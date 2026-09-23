package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"

	"github.com/Aliancn/mdlive/internal/ignore"
	"github.com/bmatcuk/doublestar/v4"
)

// ReloadRequest is the body of POST /_/api/reload: one entry per pattern to
// reload, carrying the freshly resolved discovery rules.
type ReloadRequest struct {
	Filters []PatternFilterData `json:"filters"`
}

// ReloadResult summarizes what one pattern reload changed.
type ReloadResult struct {
	Pattern      string   `json:"pattern"`
	Group        string   `json:"group,omitempty"`
	Added        int      `json:"added"`
	Removed      int      `json:"removed"`
	Unchanged    int      `json:"unchanged"`
	Excluded     int      `json:"excluded"`
	RemovedPaths []string `json:"removedPaths,omitempty"`
}

// ReloadResponse aggregates the per-pattern results for the CLI summary.
type ReloadResponse struct {
	Patterns  int `json:"patterns"`
	Added     int `json:"added"`
	Removed   int `json:"removed"`
	Unchanged int `json:"unchanged"`
	Excluded  int `json:"excluded"`
	Skipped   int `json:"skipped"`
	Files     int `json:"files"`
	// RemovedPaths lists every entry dropped because the new rules no longer
	// admit it.
	RemovedPaths []string `json:"removedPaths,omitempty"`
}

// ReloadPattern applies new discovery rules to one registered pattern
// without touching its watches. The pattern element is swapped for a fresh
// pointer — matchAndAddFile and handleCreateForGlobs read gp.pattern without
// holding the lock, so the old element must never be mutated in place. The
// base directory is unchanged, so the root and directory registrations stay
// valid: a reload creates and destroys no watcher streams.
//
// Entries the new rules no longer admit are removed, but only when the old
// rules used to admit them: a file explicitly added while excluded (or
// admitted by a different pattern) is not pattern-owned and survives. The
// expansion afterwards picks up files the new rules admit.
func (s *State) ReloadPattern(absPattern, groupName string, rules ignore.Rules) (ReloadResult, error) {
	filter, err := rules.Filter()
	if err != nil {
		return ReloadResult{}, fmt.Errorf("invalid filter: %w", err)
	}

	// Snapshot the pattern and the affected group under the lock, then swap
	// the pattern element for the recompiled one.
	s.mu.Lock()
	var gp *GlobPattern
	for _, p := range s.patterns {
		if p.Pattern == absPattern && p.Group == groupName {
			gp = p
			break
		}
	}
	if gp == nil {
		s.mu.Unlock()
		return ReloadResult{}, fmt.Errorf("pattern %q is not registered", absPattern)
	}
	newGp := &GlobPattern{
		Pattern:      gp.Pattern,
		PatternSlash: gp.PatternSlash,
		BaseDir:      gp.BaseDir,
		Group:        gp.Group,
		rules:        rules,
		pattern:      filter.ForPattern(gp.PatternSlash, filepath.ToSlash(gp.BaseDir)),
	}
	for i, p := range s.patterns {
		if p == gp {
			s.patterns[i] = newGp
			break
		}
	}
	s.mu.Unlock()

	matches, stats, err := expandPatternMatches(newGp)
	if err != nil {
		// Expansion failure restores the previous rules; the watches never
		// depended on the rules' identity, so nothing to re-register.
		s.mu.Lock()
		for i, p := range s.patterns {
			if p == newGp {
				s.patterns[i] = gp
				break
			}
		}
		s.mu.Unlock()
		return ReloadResult{}, fmt.Errorf("glob expansion failed: %w", err)
	}

	// Entries present before the reload, so "unchanged" and "added" can be
	// told apart (AddFile deduplicates and would silently report nothing).
	before := make(map[string]string, 8) // id → path
	s.mu.RLock()
	if g, ok := s.groups[groupName]; ok {
		for _, f := range g.Files {
			before[f.ID] = f.Path
		}
	}
	s.mu.RUnlock()

	res := ReloadResult{Pattern: absPattern, Group: groupName, Excluded: stats.Excluded}

	// Drop entries the new rules reject that the old rules used to admit:
	// the pattern matched them, they sit under its base, and the old filter
	// let them through. Everything else was never pattern-owned.
	for id, p := range before {
		if !pathWithinBase(p, gp.BaseDir) {
			continue
		}
		matched, err := doublestar.Match(gp.PatternSlash, filepath.ToSlash(p))
		if err != nil || !matched {
			continue
		}
		if !gp.pattern.AdmitsFile(p) || newGp.pattern.AdmitsFile(p) {
			continue
		}
		if !s.RemoveFile(id, groupName) {
			slog.Warn("failed to remove entry no longer admitted by reload", "path", p)
			continue
		}
		res.Removed++
		res.RemovedPaths = append(res.RemovedPaths, p)
	}

	// Re-expand so newly admitted files appear.
	for _, m := range matches {
		abs := filepath.Join(gp.BaseDir, m)
		if !newGp.pattern.AdmitsFile(abs) {
			continue
		}
		if _, err := s.AddFile(abs, groupName); err != nil {
			slog.Warn("skipping file", "path", abs, "error", err)
			continue
		}
		if _, ok := before[FileID(abs)]; !ok {
			res.Added++
		}
	}

	res.Unchanged = len(before) - res.Removed

	s.sendEvent(sseEvent{Name: eventUpdate, Data: "{}"})
	return res, nil
}

// handleReload applies fresh discovery rules to registered patterns. Unknown
// patterns and invalid filters are counted as skipped instead of failing the
// whole request, so one stale pattern cannot block the rest.
func handleReload(state *State) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req ReloadRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		resp := ReloadResponse{}
		for _, f := range req.Filters {
			res, err := state.ReloadPattern(f.Pattern, f.Group, f.Rules)
			if err != nil {
				slog.Warn("skipping pattern reload", "pattern", f.Pattern, "error", err)
				resp.Skipped++
				continue
			}
			resp.Patterns++
			resp.Added += res.Added
			resp.Removed += res.Removed
			resp.Unchanged += res.Unchanged
			resp.Excluded += res.Excluded
			resp.RemovedPaths = append(resp.RemovedPaths, res.RemovedPaths...)
		}
		resp.Files = state.FileCount()

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			slog.Error("failed to encode reload response", "error", err)
		}
	}
}

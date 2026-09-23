package server

import (
	"errors"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/fswatcher/fswatcher"
)

// watchRootsEnabled mirrors rootWatchCoversFiles. It is a variable so tests can
// force the root model (or the per-entry model) on any platform; tests that
// flip it must restore the old value via t.Cleanup.
var watchRootsEnabled = rootWatchCoversFiles

// addRootWatch registers one recursive watch for dir with the watcher, so a
// recursive pattern costs a single registration instead of one per directory
// and file. Logical roots (the base as spelled) and their canonical watch
// targets are counted separately: on macOS, /tmp/... watches resolve to
// /private/tmp/..., and events arrive in canonical form.
//
// A failed AddRecursive keeps the counts in place. Registration failures are
// the OS watch limit being hit; rolling back here would desync the pattern's
// bookkeeping, and the retry path reconciles the registration instead.
func (s *State) addRootWatch(dir string) {
	if s.watcher == nil {
		return
	}
	canonical := resolvePathAlias(dir)
	target := dir
	if canonical != "" {
		target = canonical
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.roots == nil {
		s.roots = make(map[string]int)
	}
	if s.rootTargets == nil {
		s.rootTargets = make(map[string]int)
	}
	if s.retainedRoots == nil {
		s.retainedRoots = make(map[string]bool)
	}
	s.roots[dir]++
	if s.roots[dir] > 1 {
		return
	}
	s.rootTargets[target]++
	// The root is properly referenced again, so a retention left over from a
	// removed pattern no longer has to keep the stream alive on its own.
	delete(s.retainedRoots, target)
	if s.rootTargets[target] > 1 {
		s.registerPathAlias(dir, canonical)
		return
	}
	if err := s.watcher.AddRecursive(target, watchOps); err != nil && !errors.Is(err, fswatcher.ErrAlreadyAdded) {
		slog.Warn("failed to watch directory tree", "root", dir, "target", target, "error", err)
	}
	s.registerPathAlias(dir, canonical)
}

// removeRootWatch releases one logical reference to a root. The physical watch
// is dropped only once no reference remains — and even then it is retained
// while tracked files still live below it, because those files carry no watch
// of their own under the root model and would silently lose live-reload
// otherwise (see physicallyReleaseRootLocked).
func (s *State) removeRootWatch(dir string) {
	if s.watcher == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	count, ok := s.roots[dir]
	if !ok {
		return
	}
	if count > 1 {
		s.roots[dir] = count - 1
		return
	}
	delete(s.roots, dir)

	target := dir
	if canonical, ok := s.aliasReverse[dir]; ok {
		target = canonical
	}
	s.unregisterPathAlias(dir)

	targetCount := s.rootTargets[target]
	if targetCount > 1 {
		s.rootTargets[target] = targetCount - 1
		return
	}
	delete(s.rootTargets, target)
	s.physicallyReleaseRootLocked(target)
}

// physicallyReleaseRootLocked removes the watcher registration for a root that
// lost its last reference — or retains it, when tracked files remain below the
// root. Entries added while the root was live have no watches of their own, so
// releasing the stream immediately would silently kill live-reload for them;
// sweepRetainedRootsLocked releases the stream once the last such entry goes.
// Caller must hold s.mu for write.
func (s *State) physicallyReleaseRootLocked(target string) {
	if s.countTrackedFilesUnderLocked(target) > 0 {
		s.retainedRoots[target] = true
		return
	}
	if s.watcher == nil {
		return
	}
	if err := s.watcher.Remove(target); err != nil && !errors.Is(err, fswatcher.ErrNotAdded) {
		slog.Warn("failed to remove directory-tree watch", "target", target, "error", err)
	}
}

// sweepRetainedRootsLocked releases retained roots that no longer have any
// tracked file below them. Caller must hold s.mu for write.
func (s *State) sweepRetainedRootsLocked() {
	if len(s.retainedRoots) == 0 {
		return
	}
	for target := range s.retainedRoots {
		if s.countTrackedFilesUnderLocked(target) > 0 {
			continue
		}
		delete(s.retainedRoots, target)
		if s.watcher == nil {
			continue
		}
		if err := s.watcher.Remove(target); err != nil && !errors.Is(err, fswatcher.ErrNotAdded) {
			slog.Warn("failed to remove directory-tree watch", "target", target, "error", err)
		}
	}
}

// countTrackedFilesUnderLocked counts file entries stored under target. Paths
// are compared in canonical form: file entries keep user-facing spellings
// (e.g. /tmp/... on macOS) while watch targets are symlink-resolved
// (/private/tmp/...), and aliasReverse carries each entry's resolution.
// Caller must hold s.mu.
func (s *State) countTrackedFilesUnderLocked(target string) int {
	count := 0
	prefix := target + string(filepath.Separator)
	for _, g := range s.groups {
		for _, f := range g.Files {
			p := f.Path
			if canonical, ok := s.aliasReverse[p]; ok {
				p = canonical
			}
			if p == target || strings.HasPrefix(p, prefix) {
				count++
			}
		}
	}
	return count
}

// rootCoversLocked reports whether p sits under a registered or retained root
// watch, in which case the file needs no watch of its own. Caller must hold
// s.mu.
func (s *State) rootCoversLocked(p string) bool {
	if !s.rootWatch {
		return false
	}
	if canonical, ok := s.aliasReverse[p]; ok {
		p = canonical
	}
	for root := range s.rootTargets {
		if pathWithinBase(p, root) {
			return true
		}
	}
	for root := range s.retainedRoots {
		if pathWithinBase(p, root) {
			return true
		}
	}
	return false
}

// dirWatchCoversLocked reports whether the parent directory of p has its own
// watch, whose depth-1 events already report the file. Caller must hold s.mu.
func (s *State) dirWatchCoversLocked(p string) bool {
	if !s.rootWatch {
		return false
	}
	if canonical, ok := s.aliasReverse[p]; ok {
		p = canonical
	}
	_, ok := s.watchTargets[filepath.Dir(p)]
	return ok
}

// fileCoveredLocked reports whether a covering watch makes a per-file watch
// for p redundant. Caller must hold s.mu.
func (s *State) fileCoveredLocked(p string) bool {
	return s.rootCoversLocked(p) || s.dirWatchCoversLocked(p)
}

// fileCovered is fileCoveredLocked for callers that hold no lock (the watch
// loop's deferred timers).
func (s *State) fileCovered(p string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.fileCoveredLocked(p)
}

// rootCovers is rootCoversLocked for callers that hold no lock.
func (s *State) rootCovers(p string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.rootCoversLocked(p)
}

// hasRoots reports whether any root watch (registered or retained) is live.
func (s *State) hasRoots() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.roots) > 0 || len(s.retainedRoots) > 0
}

// isRootPath reports whether p is a registered root, either as spelled or in
// canonical form. Events for a root arrive translated to its spelled form, so
// both keys have to be checked.
func (s *State) isRootPath(p string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.roots[p]; ok {
		return true
	}
	if _, ok := s.rootTargets[p]; ok {
		return true
	}
	if canonical, ok := s.aliasReverse[p]; ok {
		if _, ok := s.rootTargets[canonical]; ok {
			return true
		}
		return s.retainedRoots[canonical]
	}
	return false
}

// noteRootLoss records that the watcher reported a root as renamed or removed.
// The darwin backend deletes the stream itself in that case. The bookkeeping
// stays in place so the pattern still owns its registration and a later retry
// can re-establish the stream once the directory exists again.
func (s *State) noteRootLoss(path string) {
	slog.Warn("watch root moved or removed; live-reload for its tree may be degraded", "root", path)
}

// dirMoveUnderRoot reports whether a renamed or removed path is a directory
// tracked only by a root watch: no per-directory entry exists, no file entry
// sits at the path itself, but tracked files live below it.
func (s *State) dirMoveUnderRoot(eventPath string, refs []fileRef) bool {
	if !s.rootWatch || len(refs) > 0 || !s.hasRoots() {
		return false
	}
	return len(s.findRefsByPathPrefix(eventPath)) > 0
}

// registerPatternWatches sets up watching for a freshly registered pattern: a
// single recursive root on platforms whose directory registration reports the
// whole subtree, the per-directory walk everywhere else.
func (s *State) registerPatternWatches(gp *GlobPattern) {
	if gp.IsRecursive() && s.rootWatch {
		if s.watcher == nil {
			return
		}
		// Register the aliases before the root, so events that arrive the
		// moment the stream exists can already be translated; the walk itself
		// registers no watches.
		s.retainDirAliases(gp)
		s.addRootWatch(gp.BaseDir)
		return
	}
	s.walkDirsForPattern(gp, s.addDirWatch)
}

// releasePatternWatches undoes registerPatternWatches for a removed or
// replaced pattern.
func (s *State) releasePatternWatches(gp *GlobPattern) {
	if gp.IsRecursive() && s.rootWatch {
		if s.watcher == nil {
			return
		}
		// The root must be removed while the base's alias is still in place:
		// removeRootWatch resolves the watch target through it.
		s.removeRootWatch(gp.BaseDir)
		s.releaseDirAliases(gp)
		return
	}
	s.walkDirsForPattern(gp, s.removeDirWatch)
}

// retainDirAliases walks the pattern's tree and keeps a path alias for every
// symlinked directory, so watcher events reported in canonical form translate
// back to the spelled paths file entries are stored under. Without this, new
// files created inside a symlinked external tree would arrive under their
// canonical path, miss the pattern, and never appear in the sidebar. The
// per-directory watch model registered these aliases as a side effect of
// watching each directory; with a single root watch, this walk is what keeps
// the table filled.
func (s *State) retainDirAliases(gp *GlobPattern) {
	if _, err := walkSymlinkTree(gp.BaseDir, s.retainDirAlias, nil, gp.pruneFunc()); err != nil {
		slog.Warn("failed to walk directories for path aliases", "pattern", gp.Pattern, "base", gp.BaseDir, "error", err)
	}
}

// releaseDirAliases releases the alias references a pattern's tree holds. The
// walk mirrors retainDirAliases, and unresolved entries (directories that
// vanished or stopped resolving since) are released too, mirroring
// walkDirsForPattern's teardown handling.
func (s *State) releaseDirAliases(gp *GlobPattern) {
	stats, err := walkSymlinkTree(gp.BaseDir, s.releaseDirAlias, nil, gp.pruneFunc())
	for _, path := range stats.Unresolved {
		s.releaseDirAlias(path)
	}
	if err != nil {
		slog.Warn("failed to walk directories for path aliases", "pattern", gp.Pattern, "base", gp.BaseDir, "error", err)
	}
}

// retainDirAlias keeps one alias reference for a symlinked directory. Caller
// must not hold s.mu (it resolves the symlink on the filesystem).
func (s *State) retainDirAlias(dir string) {
	canonical := resolvePathAlias(dir)
	if canonical == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dirAliases == nil {
		s.dirAliases = make(map[string]int)
	}
	s.dirAliases[dir]++
	s.registerPathAlias(dir, canonical)
}

// releaseDirAlias drops one alias reference for dir, removing the path alias
// once the last reference goes. Dirs that never held a reference are a no-op.
func (s *State) releaseDirAlias(dir string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	count, ok := s.dirAliases[dir]
	if !ok {
		return
	}
	if count > 1 {
		s.dirAliases[dir] = count - 1
		return
	}
	delete(s.dirAliases, dir)
	s.unregisterPathAlias(dir)
}

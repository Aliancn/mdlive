package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

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
		s.recordWatchFailureLocked(watchKindRoot, target, err)
	} else {
		s.clearWatchFailureLocked(watchKindRoot, target)
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
	s.cancelWatchRetryLocked(watchKindRoot, target)
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
		s.cancelWatchRetryLocked(watchKindRoot, target)
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

// errRootLost is recorded as the failure reason when the watcher reports a
// root watch as renamed or removed.
var errRootLost = errors.New("watch root moved or removed")

// noteRootLoss records that the watcher reported a root as renamed or removed.
// The darwin backend deletes the stream itself in that case. The bookkeeping
// stays in place so the pattern still owns its registration and the retry
// loop can re-establish the stream once the directory exists again. The
// failure is keyed on the canonical target so removeRootWatch's cancellation
// (which resolves through aliasReverse) matches it; resolvePathAlias cannot
// be used here because the directory may already be gone.
func (s *State) noteRootLoss(path string) {
	slog.Warn("watch root moved or removed; live-reload for its tree may be degraded", "root", path)
	target := path
	s.mu.RLock()
	if canonical, ok := s.aliasReverse[path]; ok {
		target = canonical
	}
	s.mu.RUnlock()
	s.recordWatchFailure(watchKindRoot, target, errRootLost)
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

// watchKind classifies which registration a health record belongs to.
type watchKind int

const (
	watchKindFile watchKind = iota
	watchKindDir
	watchKindRoot
)

// watchKindName names the kind for logs and diagnostics.
func watchKindName(k watchKind) string {
	switch k {
	case watchKindFile:
		return "file"
	case watchKindDir:
		return "dir"
	case watchKindRoot:
		return "root"
	}
	return "watch"
}

func watchKey(kind watchKind, target string) string {
	return fmt.Sprintf("%d|%s", int(kind), target)
}

// watchFailure is one currently-failing watch registration.
type watchFailure struct {
	kind     watchKind
	target   string
	err      string
	at       time.Time
	attempts int
}

// watchRetry is one pending re-registration attempt. The retry loop pops
// entries when they come due and hands them to rewatchTarget, which
// re-creates the entry (with a longer backoff) if the attempt fails again.
type watchRetry struct {
	kind     watchKind
	target   string
	attempts int
	nextAt   time.Time
}

// watchHealth is the watcher's self-reported bookkeeping. All fields are
// guarded by State.mu; the API projection is built by State.WatcherStatus.
type watchHealth struct {
	// failed holds one record per currently-failing watch target, keyed by
	// watchKey(kind, target).
	failed map[string]*watchFailure
	// retries holds registrations waiting to be re-attempted.
	retries map[string]*watchRetry

	totalFailures  int
	totalRecovered int

	lastError     string
	lastErrorPath string
	lastErrorAt   time.Time

	// consecutive counts registration failures since the last success; at
	// watchBreakerAfter the circuit breaker opens.
	consecutive int
	// circuitUntil is the moment the breaker half-opens again; zero while
	// closed. circuitOpens counts how often the breaker has opened in a row
	// and doubles the open duration each time.
	circuitUntil time.Time
	circuitOpens int

	// warnings counts errors reported on the watcher's error channel (e.g.
	// the darwin backend dropping events). They do not degrade the watcher
	// by themselves: nothing failed to register, so there is nothing to
	// retry.
	warnings    int
	lastWarning string
}

// WatcherStatus is the JSON projection of watcher health: the shape of the
// "watcher" field in the status API response and the payload of the
// "watcher" SSE event.
type WatcherStatus struct {
	Status         string `json:"status"`
	Roots          int    `json:"roots"`
	DirWatches     int    `json:"dirWatches"`
	FileWatches    int    `json:"fileWatches"`
	Failed         int    `json:"failed"`
	PendingRetries int    `json:"pendingRetries"`
	CircuitOpen    bool   `json:"circuitOpen"`
	TotalFailures  int    `json:"totalFailures"`
	TotalRecovered int    `json:"totalRecovered"`
	LastError      string `json:"lastError,omitempty"`
	LastErrorPath  string `json:"lastErrorPath,omitempty"`
	LastErrorAt    string `json:"lastErrorAt,omitempty"`
	Warnings       int    `json:"warnings,omitempty"`
	LastWarning    string `json:"lastWarning,omitempty"`
}

// Tunables are atomics so tests can shrink the backoff and breaker thresholds
// without racing the retry loop of a state whose test has already returned.
// All values are nanoseconds except watchBreakerAfter, which is a count.
var (
	watchRetryBaseDelay  atomic.Int64
	watchRetryMaxDelay   atomic.Int64
	watchBreakerAfter    atomic.Int64
	watchBreakerOpenFor  atomic.Int64
	watchBreakerMaxOpen  atomic.Int64
	watchHealthEmitDelay atomic.Int64
	watchRetryIdlePoll   atomic.Int64
)

func init() {
	watchRetryBaseDelay.Store(int64(500 * time.Millisecond))
	watchRetryMaxDelay.Store(int64(30 * time.Second))
	watchBreakerAfter.Store(12)
	watchBreakerOpenFor.Store(int64(5 * time.Minute))
	watchBreakerMaxOpen.Store(int64(30 * time.Minute))
	// watchHealthEmitDelay coalesces health transitions into one SSE event.
	watchHealthEmitDelay.Store(int64(50 * time.Millisecond))
	// watchRetryIdlePoll is how often the retry loop wakes while there is
	// nothing to retry (it mainly re-checks the circuit breaker).
	watchRetryIdlePoll.Store(int64(2 * time.Second))
}

// watchDur reads one of the duration tunables.
func watchDur(v *atomic.Int64) time.Duration {
	return time.Duration(v.Load())
}

// RetryFailedWatches clears the circuit breaker and re-enqueues every failed
// registration once. It backs the manual "retry now" path behind
// POST /_/api/watcher/retry and returns the status after the attempt.
func (s *State) RetryFailedWatches() WatcherStatus {
	now := time.Now()
	s.mu.Lock()
	s.health.consecutive = 0
	s.health.circuitUntil = time.Time{}
	s.health.circuitOpens = 0
	for key, f := range s.health.failed {
		if _, ok := s.health.retries[key]; !ok {
			s.health.retries[key] = &watchRetry{
				kind:     f.kind,
				target:   f.target,
				attempts: f.attempts,
				nextAt:   now,
			}
		}
	}
	s.mu.Unlock()
	s.retryDueWatches()
	return s.WatcherStatus()
}

// WatcherStatus projects the health bookkeeping for the status API and SSE.
// The watch counts are physical: entries covered by a root (or a parent
// directory) hold only bookkeeping references, not their own streams, and
// must not inflate the numbers.
func (s *State) WatcherStatus() WatcherStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now()
	st := WatcherStatus{
		Roots:          len(s.rootTargets),
		Failed:         len(s.health.failed),
		PendingRetries: len(s.health.retries),
		CircuitOpen:    s.circuitOpenLocked(now),
		TotalFailures:  s.health.totalFailures,
		TotalRecovered: s.health.totalRecovered,
		LastError:      s.health.lastError,
		LastErrorPath:  s.health.lastErrorPath,
		Warnings:       s.health.warnings,
		LastWarning:    s.health.lastWarning,
	}
	for target := range s.watchTargets {
		if !s.rootCoversLocked(target) {
			st.DirWatches++
		}
	}
	for target := range s.fileWatchTargets {
		if !s.fileCoveredLocked(target) {
			st.FileWatches++
		}
	}
	if !s.health.lastErrorAt.IsZero() {
		st.LastErrorAt = s.health.lastErrorAt.Format(time.RFC3339)
	}
	if st.Failed > 0 || st.PendingRetries > 0 || st.CircuitOpen {
		st.Status = "degraded"
	} else {
		st.Status = "healthy"
	}
	return st
}

// recordWatchFailure books a failed registration and schedules a retry. It is
// the entry point for callers that hold no lock; the add*Watch helpers call
// recordWatchFailureLocked directly.
func (s *State) recordWatchFailure(kind watchKind, target string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordWatchFailureLocked(kind, target, err)
}

// recordWatchFailureLocked records a failed registration, arms the retry with
// exponential backoff, and opens the circuit breaker once failures stack up
// consecutively. Callers only reach this with a live watcher (every call site
// sits behind a s.watcher != nil check), so it performs bookkeeping only.
// Caller must hold s.mu.
func (s *State) recordWatchFailureLocked(kind watchKind, target string, err error) {
	if err == nil {
		return
	}
	if s.health.failed == nil {
		s.health.failed = make(map[string]*watchFailure)
	}
	if s.health.retries == nil {
		s.health.retries = make(map[string]*watchRetry)
	}
	key := watchKey(kind, target)
	now := time.Now()
	f, ok := s.health.failed[key]
	if !ok {
		f = &watchFailure{kind: kind, target: target}
		s.health.failed[key] = f
	}
	f.err = err.Error()
	f.at = now
	f.attempts++
	s.health.totalFailures++
	s.health.lastError = f.err
	s.health.lastErrorPath = target
	s.health.lastErrorAt = now
	s.health.consecutive++

	if s.health.consecutive >= int(watchBreakerAfter.Load()) {
		s.openCircuitLocked(now)
	}
	if !s.circuitOpenLocked(now) {
		s.health.retries[key] = &watchRetry{
			kind:     kind,
			target:   target,
			attempts: f.attempts,
			nextAt:   now.Add(watchBackoffDelay(f.attempts)),
		}
		s.wakeRetryLoopLocked()
	}
	s.scheduleHealthEmit()
}

// openCircuitLocked arms the circuit breaker: further failures are not
// enqueued for retry until circuitUntil passes. Each re-open doubles the open
// duration, so a persistently overloaded OS watch limit is probed at an ever
// lower frequency. Caller must hold s.mu.
func (s *State) openCircuitLocked(now time.Time) {
	d := watchDur(&watchBreakerOpenFor)
	maxOpen := watchDur(&watchBreakerMaxOpen)
	for i := 0; i < s.health.circuitOpens && d < maxOpen; i++ {
		d *= 2
	}
	if d > maxOpen {
		d = maxOpen
	}
	s.health.circuitUntil = now.Add(d)
	s.health.circuitOpens++
	slog.Warn("watch registrations keep failing; pausing retries", "resumeAt", s.health.circuitUntil.Format(time.RFC3339))
}

// circuitOpenLocked reports whether the breaker currently blocks retries.
// Caller must hold s.mu (or RLock).
func (s *State) circuitOpenLocked(now time.Time) bool {
	return !s.health.circuitUntil.IsZero() && now.Before(s.health.circuitUntil)
}

// clearWatchFailure forgets a failed registration after a successful one, for
// callers that hold no lock. It reports whether a failure record was cleared.
func (s *State) clearWatchFailure(kind watchKind, target string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.clearWatchFailureLocked(kind, target)
}

// clearWatchFailureLocked drops the failure and retry records for a target
// that registered successfully. Any success also resets the consecutive
// counter and the breaker: one healthy registration is enough evidence that
// the OS limit has room again. It reports whether a failure record was
// actually cleared, so a health event is emitted only on a real transition.
// Caller must hold s.mu.
func (s *State) clearWatchFailureLocked(kind watchKind, target string) bool {
	key := watchKey(kind, target)
	_, had := s.health.failed[key]
	delete(s.health.failed, key)
	_, hadRetry := s.health.retries[key]
	delete(s.health.retries, key)
	s.health.consecutive = 0
	if !s.health.circuitUntil.IsZero() {
		s.health.circuitUntil = time.Time{}
		s.health.circuitOpens = 0
	}
	if had || hadRetry {
		s.health.totalRecovered++
		s.scheduleHealthEmit()
		return true
	}
	return false
}

// cancelWatchRetryLocked drops failure and retry bookkeeping for a watch that
// is being removed. Unlike a recovered registration, a removal is not a
// recovery: neither totalRecovered nor the consecutive count is touched.
// Caller must hold s.mu.
func (s *State) cancelWatchRetryLocked(kind watchKind, target string) {
	key := watchKey(kind, target)
	_, had := s.health.failed[key]
	delete(s.health.failed, key)
	_, hadRetry := s.health.retries[key]
	delete(s.health.retries, key)
	if had || hadRetry {
		s.scheduleHealthEmit()
	}
}

// recordWatcherWarning books an error reported on the watcher's error
// channel. Warnings are surfaced through the status API but do not degrade
// the watcher: nothing failed to register, so there is nothing to retry.
func (s *State) recordWatcherWarning(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	s.health.warnings++
	s.health.lastWarning = err.Error()
	s.mu.Unlock()
}

// watchBackoffDelay returns the delay before attempt n of a failed
// registration: exponential from watchRetryBaseDelay, capped at
// watchRetryMaxDelay, with ±20% jitter so a storm of failures does not retry
// in lockstep.
func watchBackoffDelay(attempts int) time.Duration {
	d := watchDur(&watchRetryBaseDelay)
	maxDelay := watchDur(&watchRetryMaxDelay)
	for i := 1; i < attempts && d < maxDelay; i++ {
		d *= 2
	}
	if d > maxDelay {
		d = maxDelay
	}
	return time.Duration(float64(d) * (0.8 + 0.4*rand.Float64())) //nolint:gosec // jitter for retry timing, not a security-sensitive value
}

// watchRetryLoop re-registers failed watches in the background. Lock
// discipline: it never holds s.mu while waiting, and rewatchTarget performs
// watcher I/O without the lock — scheduleFileChanged and the event handlers
// take s.mu, so holding it across a retry would risk deadlock.
func (s *State) watchRetryLoop(ctx context.Context) {
	for {
		next := s.nextRetryDeadline()
		var timer *time.Timer
		var wake <-chan struct{}
		if next.IsZero() {
			timer = time.NewTimer(watchDur(&watchRetryIdlePoll))
		} else {
			wait := max(time.Until(next), 0)
			timer = time.NewTimer(wait)
			wake = s.healthWake
		}
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		case <-wake:
			timer.Stop()
		}
		s.retryDueWatches()
	}
}

// nextRetryDeadline returns when the retry loop should wake next: the earliest
// pending retry, the breaker's expiry (so a half-open probe happens on time),
// or zero for "nothing scheduled". Caller must hold no lock.
func (s *State) nextRetryDeadline() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var next time.Time
	for _, r := range s.health.retries {
		if next.IsZero() || r.nextAt.Before(next) {
			next = r.nextAt
		}
	}
	if cu := s.health.circuitUntil; !cu.IsZero() && (next.IsZero() || cu.Before(next)) {
		next = cu
	}
	return next
}

// wakeRetryLoopLocked interrupts the retry loop's wait because a retry was
// scheduled earlier than the deadline it is sleeping on. Caller must hold
// s.mu.
func (s *State) wakeRetryLoopLocked() {
	select {
	case s.healthWake <- struct{}{}:
	default:
	}
}

// retryDueWatches re-registers every retry whose deadline has passed. While
// the circuit breaker is open nothing is attempted; when it expires, all
// still-failing registrations are probed once (half-open).
func (s *State) retryDueWatches() {
	now := time.Now()
	s.mu.Lock()
	if s.circuitOpenLocked(now) {
		s.mu.Unlock()
		return
	}
	if !s.health.circuitUntil.IsZero() {
		// The breaker just expired: probe every failed registration once.
		s.health.circuitUntil = time.Time{}
		for key, f := range s.health.failed {
			if _, ok := s.health.retries[key]; !ok {
				s.health.retries[key] = &watchRetry{
					kind:     f.kind,
					target:   f.target,
					attempts: f.attempts,
					nextAt:   now,
				}
			}
		}
	}
	var due []watchRetry
	for key, r := range s.health.retries {
		if r.nextAt.After(now) {
			continue
		}
		delete(s.health.retries, key)
		due = append(due, *r)
	}
	s.mu.Unlock()

	for _, r := range due {
		if !s.watchRetryStillWanted(r.kind, r.target) {
			// The pattern, directory, or file went away while the retry was
			// pending; drop its failure record too so it stops counting as
			// degraded.
			s.mu.Lock()
			delete(s.health.failed, watchKey(r.kind, r.target))
			s.mu.Unlock()
			s.scheduleHealthEmit()
			continue
		}
		s.rewatchTarget(r)
	}
}

// watchRetryStillWanted reports whether the logical reference a retry was
// created for still exists, so removed patterns and files are not resurrected.
func (s *State) watchRetryStillWanted(kind watchKind, target string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	switch kind {
	case watchKindRoot:
		return s.rootTargets[target] > 0
	case watchKindDir:
		return s.watchTargets[target] > 0
	case watchKindFile:
		return s.fileWatchTargets[target] > 0
	}
	return false
}

// rewatchTarget re-registers one failed watch. It performs watcher I/O
// without holding s.mu, then records the outcome.
func (s *State) rewatchTarget(r watchRetry) {
	if s.watcher == nil {
		return
	}
	s.mu.RLock()
	var covered bool
	switch r.kind {
	case watchKindFile:
		covered = s.fileCoveredLocked(r.target)
	case watchKindDir:
		covered = s.rootCoversLocked(r.target)
	}
	s.mu.RUnlock()
	if covered {
		// The target gained a covering watch since the failure; the retry is
		// no longer needed.
		s.clearWatchFailure(r.kind, r.target)
		return
	}
	var err error
	switch r.kind {
	case watchKindRoot:
		err = s.watcher.AddRecursive(r.target, watchOps)
	case watchKindFile:
		err = s.watcher.Add(fileWatchPath(r.target), watchOps)
	default:
		err = s.watcher.Add(r.target, watchOps)
	}
	if err == nil || errors.Is(err, fswatcher.ErrAlreadyAdded) {
		s.clearWatchFailure(r.kind, r.target)
		return
	}
	s.recordWatchFailure(r.kind, r.target, err)
}

// scheduleHealthEmit coalesces watcher health transitions into one SSE event:
// maybeEmitHealth runs on a short timer because it takes s.mu (via
// WatcherStatus) and must therefore not run while a caller still holds it.
func (s *State) scheduleHealthEmit() {
	if s.watcher == nil {
		return
	}
	time.AfterFunc(watchDur(&watchHealthEmitDelay), s.maybeEmitHealth)
}

// maybeEmitHealth pushes the current watcher status to SSE subscribers when
// its health status changed. Runs on its own goroutine via
// scheduleHealthEmit; must not be called while holding s.mu. healthMu is held
// across the dedup check and the send so concurrent emitters cannot deliver
// an older status after a newer one.
func (s *State) maybeEmitHealth() {
	status := s.WatcherStatus()
	s.healthMu.Lock()
	defer s.healthMu.Unlock()
	if status.Status == s.lastHealthStatus {
		return
	}
	s.lastHealthStatus = status.Status
	b, err := json.Marshal(status)
	if err != nil {
		return
	}
	s.sendEvent(sseEvent{Name: eventWatcher, Data: string(b)})
}

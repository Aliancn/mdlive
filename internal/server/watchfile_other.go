//go:build !windows

package server

// fileWatchPath returns the physical watch path for a file's canonical
// target: the file itself on platforms whose watcher backend can watch
// files directly.
func fileWatchPath(target string) string {
	return target
}

//go:build windows

package server

import "path/filepath"

// fileWatchPath returns the physical watch path for a file's canonical
// target. Windows' ReadDirectoryChangesW only accepts directory handles, so
// per-file watches register the file's parent directory instead; the watcher
// backend joins the watched directory with each changed item's name, so
// events still arrive as per-file paths.
func fileWatchPath(target string) string {
	return filepath.Dir(target)
}

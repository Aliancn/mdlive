//go:build windows

package server

// rootWatchCoversFiles reports whether a directory-level registration on this
// platform also reports changes to files inside the subtree, so files under a
// covering root or directory watch need no watch of their own.
// ReadDirectoryChangesW is opened with bWatchSubtree, so a recursive root
// covers every file below it.
const rootWatchCoversFiles = true

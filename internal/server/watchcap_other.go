//go:build !darwin && !windows

package server

// rootWatchCoversFiles reports whether a directory-level registration on this
// platform also reports changes to files inside the subtree. inotify (linux)
// and kqueue (freebsd) report only direct children of a watched directory —
// kqueue watches the directory inode itself — so files keep their individual
// watches on those platforms and the per-directory + per-file model stays as
// it was.
const rootWatchCoversFiles = false

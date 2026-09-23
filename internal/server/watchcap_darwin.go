//go:build darwin

package server

// rootWatchCoversFiles reports whether a directory-level registration on this
// platform also reports changes to files inside the subtree, so files under a
// covering root or directory watch need no watch of their own. FSEvents is
// path-based and reports events for the whole tree behind a recursive root.
const rootWatchCoversFiles = true

# TODO

Backlog of browser (SPA) feature ideas and improvements. Nothing here is
committed work — pick items from this list when planning a release, then
remove them here as they land.

## Sidebar / file list

Convenience gaps in the current file list design.

- [ ] **Search in tree view filters the tree.** While searching, tree view
  is replaced by a flat filtered list (`Sidebar.tsx`); a tree-shaped result
  (matches with their ancestor chain expanded) would preserve context.
- [ ] **Close others / close all** in the file context menu (kebab).
- [ ] **Multi-select with bulk actions** (close, move to group). Today
  every operation is one file at a time.
- [ ] **Sort options for flat view.** Currently only manual drag order;
  add sort by name / modification time (server would need to send mtime).
- [ ] **Pin files to the top** of the list.
- [ ] **New-file indicator.** Files added by watch patterns get no visual
  distinction; a dot until first viewed would make "what just appeared"
  legible.

## Viewer

- [ ] **Keyboard shortcuts.** Zero global shortcuts today (only Escape in
  the search box and zoom modal). Suggested: `/` focus search, `t` ToC,
  `r` raw view, `[` / `]` previous / next file, `?` shortcut help overlay.
- [ ] **Precise search-result jump.** `SearchMatch.anchor` (kind/value,
  returned by the server) is never used — result clicks jump to the
  *heading text* (`MarkdownViewer` matches `scrollToHeading` by text), so
  duplicate or missing headings jump wrong. Jump by anchor/line.
- [ ] **In-document find navigation.** The search-hit marker rail exists
  but there is no next/previous match jump within the current document.
- [ ] **Verify print output**; the `@media print` rules hide the chrome
  but the result has never been checked end to end.
- [ ] **Export standalone HTML** (single self-contained file, inlined
  styles) for sharing rendered docs.
- [ ] **GFM task-list checkboxes** are read-only. Interactive toggling
  needs a server-side write API — decide whether that is in scope at all.

## Search

- [ ] **Cross-group search.** Search only covers the active group; add an
  "all groups" option.
- [ ] **Pagination / show more.** Server caps at `limit=50` with no way to
  request the next page from the UI.
- [ ] **Keyboard navigation of results** (arrow keys + Enter).

## Groups

- [ ] **Create / rename groups from the UI.** Requires new server APIs;
  groups can only be created via CLI `-t` today.

## Bigger features (defer)

- [ ] Split view: two files side by side for comparison.
- [ ] Touch/mobile: resize handles are mouse-event only
  (`Sidebar.tsx`, `TocPanel.tsx`); no narrow-screen layout.

## Engineering / performance

- [ ] **Content cache.** Every file switch refetches content; an in-memory
  LRU keyed by `(fileId, revision)` makes back/forward instant.
- [ ] **Refactor the render-time state adjustment in `App.tsx`** (the
  `prevGroups` / `prevActiveGroup` pattern) into explicit derivation —
  fragile as branches grow.
- [ ] **Favicon.** Deliberately removed with the mo branding; design an
  `ml` one to fill the gap.

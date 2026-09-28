# mdlive

[![build](https://github.com/Aliancn/mdlive/actions/workflows/ci.yml/badge.svg)](https://github.com/Aliancn/mdlive/actions/workflows/ci.yml)

`ml` is a **M**arkdown **L**ive viewer: it serves Markdown files in a browser and refreshes the page the moment a file is saved.

## Contents

- [Features](#features)
- [Install](#install)
- [Quick start](#quick-start)
- [How ml works](#how-ml-works)
- [Opening files](#opening-files)
- [Groups](#groups)
- [Watch mode](#watch-mode)
  - [Removing watch patterns](#removing-watch-patterns)
  - [Symlinked directories](#symlinked-directories)
- [Excluding files](#excluding-files)
  - [Applying .mlignore changes](#applying-mlignore-changes)
- [Sessions and server lifecycle](#sessions-and-server-lifecycle)
  - [Status, shutdown, restart](#status-shutdown-restart)
  - [Backup and restore](#backup-and-restore)
  - [Cleaning up logs and sessions](#cleaning-up-logs-and-sessions)
- [Scripting and automation](#scripting-and-automation)
- [Troubleshooting](#troubleshooting)
- [Flags](#flags)
- [Build](#build)

## Features

**Rendering**

- GitHub-flavored Markdown (tables, task lists, footnotes, etc.)
- Syntax highlighting ([Shiki](https://shiki.style/)), following the light/dark theme
- [Mermaid](https://mermaid.js.org/) diagram rendering
- LaTeX math rendering ([KaTeX](https://katex.org/))
- [GitHub Alerts](https://docs.github.com/en/get-started/writing-on-github/getting-started-with-writing-and-formatting-on-github/basic-writing-and-formatting-syntax#alerts) (admonitions)
- YAML frontmatter display (collapsible metadata block)
- MDX file support (renders as Markdown, strips `import`/`export`, escapes JSX tags)
- Raw HTML, raw Markdown view, copy as Markdown / Text / HTML

**Browsing**

- File grouping (named tabs, each with its own URL path)
- Flat / tree sidebar view with drag-and-drop reorder
- Recently viewed section, per group
- Table of contents panel, active-heading tracking
- Full-text search across file names and content
- Add files from the browser ("+" button or drag-and-drop from the OS file manager)
- File counts per group; fullscreen zoom modal for images and Mermaid diagrams
- Dark / light theme, content font size and width toggles
- Error feedback with retry (load failures surface inline and as toasts)

**Server**

- Live-reload on save (for files opened via the CLI)
- Single background server per port; later invocations attach to it
- Watch mode for directories and glob patterns (new files appear automatically)
- Watcher health reporting: failed registrations are retried automatically and surfaced in the UI banner, `ml --status`, and the startup summary
- Auto session backup and restore; server restart with session preservation
- Stdin pipe support (`cat file.md | ml`)
- Machine-readable stdout and `--json` output for scripting

## Install

**Homebrew:**

``` console
$ brew install Aliancn/tap/ml
```

**Install script** (macOS and Linux; picks the right binary, verifies the checksum, installs to `/usr/local/bin` or `~/.local/bin`):

``` console
$ curl -sfL https://raw.githubusercontent.com/Aliancn/mdlive/main/install.sh | sh
```

**Manually:**

Download a binary or package from [releases page](https://github.com/Aliancn/mdlive/releases): archives (`ml` / `ml.exe`) for macOS, Linux, and Windows; `deb`/`rpm`/`apk` packages for Linux distributions.

## Quick start

``` console
$ ml README.md                 # open one file (starts a server in the background)
$ ml -w 'docs/**/*.md'         # watch the whole docs/ tree; new files appear automatically
$ ml --status                  # list running servers, groups, and watcher health
$ ml --shutdown                # stop the server
```

That covers most day-to-day use. The sections below explain each part in detail.

## How ml works

`ml` has **no subcommands**. Every invocation performs exactly one task, selected by flags; `FILE`, `DIR`, and glob arguments select which files it applies to.

- **One server per port.** The first invocation starts a server on port `6275` (change with `--port` / `-p`) and backgrounds itself — the shell returns immediately. Later invocations on the same port **attach** to the running server and add their files to the same session. A different port is a completely separate session.
- **Groups.** Files live in named groups (`--target` / `-t`). Each group gets its own URL path (e.g. `/design`) and its own sidebar. Without `-t`, files go to the `default` group at `/`.
- **Live-reload.** Opened files are watched with filesystem notifications; saving a file re-renders it in the browser immediately. No polling, no manual refresh.
- **Sessions.** The set of open files, groups, and watch patterns is saved on every change and restored when a server starts again.
- **Output contract.** stdout stays machine-readable (the server URL plus one deeplink per opened file, or one JSON document with `--json`); human-readable summaries go to stderr prefixed with `ml:`. See [Scripting and automation](#scripting-and-automation).

## Opening files

``` console
$ ml README.md                          # One file
$ ml README.md CHANGELOG.md docs/*.md   # Several files (globs expand once)
$ ml docs/                              # Every .md in docs/ (add -R to recurse)
$ ml -R docs/                           # Every .md under docs/, recursively
```

Without `--watch`, globs and directories are expanded once; new files are not picked up automatically (see [Watch mode](#watch-mode) for that).

### Reading from stdin

When no positional arguments are given and stdin is redirected (not a terminal), `ml` reads Markdown content from stdin.

``` console
$ cat notes.md | ml
$ some-command | ml --target output
$ ml < notes.md
```

The content is loaded in-memory with a generated name (`stdin-<hash>.md`). Piping the same content again reuses the existing entry (deduplicated by content hash).

## Groups

``` console
$ ml spec.md --target design      # Opens at http://localhost:6275/design
$ ml api.md --target design       # Adds to the "design" group
$ ml notes.md --target notes      # Opens at http://localhost:6275/notes
```

Files can be organized into named groups using the `--target` (`-t`) flag. Each group gets its own URL path and sidebar. Files can also be moved between groups later, from the file's context menu in the browser.

## Watch mode

`--watch` (`-w`) turns on watch mode. Directory and glob positional arguments are registered as watch patterns, matching files are opened, and **new matching files are picked up automatically** as they are created.

``` console
$ ml -w '**/*.md'                              # Watch and open all .md files recursively
$ ml -w 'docs/**/*.md' --target docs           # Watch docs/ tree in "docs" group
$ ml -w '*.md' 'docs/**/*.md'                  # Multiple patterns (positional)
$ ml -w docs/                                  # Watch docs/*.md
```

Combine with `--recursive` (`-R`) to descend into subdirectories. Short flags can be combined:

``` console
$ ml -w -R docs/                               # Watch docs/**/*.md
$ ml -wR docs/                                 # Same, short-combined
```

### Removing watch patterns

`--unwatch` removes previously registered patterns. Pass glob patterns or directories as positional arguments to specify which patterns to remove. Regular file paths are not accepted (use `--close` to remove individual files from the sidebar). Files already added by a pattern remain in the sidebar.

``` console
$ ml --unwatch '**/*.md'                              # Stop watching a pattern (default group)
$ ml --unwatch docs/                                  # Stop watching docs/*.md
$ ml --unwatch 'docs/**/*.md' --target docs            # Stop watching in a specific group
```

With `-R`, a directory argument removes **all** registered patterns under that directory at once. For example, if `docs/*.md`, `docs/sub/*.md`, and `docs/**/*.md` are all registered, a single command removes them all:

``` console
$ ml --unwatch -R docs/                               # Removes docs/*.md, docs/sub/*.md, docs/**/*.md, etc.
```

Patterns are resolved to absolute paths before matching, so you can specify either a relative glob or the full path shown by `--status`.

### Symlinked directories

Watch mode and `-R` discovery **follow symlinked directories**. If your tree links into other repositories (docs vendored from elsewhere, a `notes` symlink into a second checkout), everything behind the links is discovered and watched too — which can multiply watcher registrations quickly.

The startup summary reports how much of the scan came through symlinks (on stderr):

``` console
$ ml -wR docs/
ml: scanned 4700 dir(s) / 742 file(s) — 4383 reached through symlinks
```

When a single pattern expands past the safety thresholds — more than 1000 directories or 5000 files, or more than 2000 entries when symlinks are involved — ml prints a warning while there is still time to narrow the discovery:

``` console
$ ml -w '**/*.md'
ml: WARNING: '**/*.md' expands to 5442 entries (4700 dirs, 742 files), 4383 of them reached through symlinks.
    Large trees multiply watcher registrations and can exhaust OS limits (on macOS: "FSEventStreamStart failed").
    Add a .mlignore with one line per external tree (e.g. "external-repo/**") and re-run, or watch a narrower pattern.
```

One-time expansion without `--watch` (e.g. `ml -R docs/`) uses the shell-style glob and does not follow directory symlinks, so its file count can differ from watch mode's.

## Excluding files

By default, `ml` does not open or watch dot-prefixed files and directories (`.git/`, `.cache/`, `.hidden.md`, ...). Use `--include-hidden` to turn that off.

`--exclude` (repeatable) and the `.mlignore` file in the working directory filter what directory and glob arguments discover. Both use gitignore-style syntax, with rules anchored at the working directory:

| Pattern | Meaning |
|---------|---------|
| `vendor` | No separator: matches the base name at any depth |
| `vendor/**` | Contains a separator: anchored at the working directory |
| `/docs/drafts/**` | Leading `/` also anchors at the working directory |
| `build/` | Trailing `/`: matches directories only |
| `!keep.md` | `!` re-includes files; the last matching line wins |
| `# comment` | Comment lines (and blank lines) are ignored |

``` console
$ ml -R . --exclude 'vendor/**'                  # Skip everything under vendor/
$ ml -w '**/*.md' --exclude '**/node_modules/**' # Watch all .md except node_modules
$ ml -R . --include-hidden                       # Open hidden files too
```

Rules from `.mlignore` are read from the current working directory only (no nesting); `--exclude` values are appended after the file's lines, so they win on conflicts. Invalid `--exclude` values abort the command; invalid `.mlignore` lines are skipped with a warning. The rules of each watch pattern are shown by `--status`, and can be re-applied to already-registered patterns with `ml --reload` (see [Applying .mlignore changes](#applying-mlignore-changes)).

Notes:

- **Explicit file arguments are never filtered**: `ml vendor/a.md --exclude 'vendor/**'` still opens `a.md`, and `ml '.git/**/*.md'` opens files under `.git/` (naming the dotted component literally in the pattern is treated as explicit).
- **Excludes never remove files already shown in the sidebar**; they only affect what future discovery picks up.
- A pattern anchored at the working directory does not apply to paths outside it: `ml /other -R --exclude 'vendor/**'` needs `vendor/` (base name) or a path relative to the current directory.
- Nesting needs `**/`: use `**/node_modules/**` to exclude at any depth.

### Applying .mlignore changes

Editing `.mlignore` (or passing new `--exclude` / `--include-hidden` flags) does not retroactively change what is already in the sidebar. To apply new rules to the watch patterns registered from the current directory, run:

``` console
$ ml --reload
ml: reloaded 1 pattern(s): added 12, removed 5, unchanged 725 (30 excluded)
  removed vendor/generated.md
  removed vendor/old-notes.md
  … and 3 more removed file(s) — use --json to list every path
```

`--reload` re-reads `.mlignore` and swaps in any `--exclude` / `--include-hidden` flags given on the same invocation; flags not given keep the values the pattern was registered with. Files the new rules no longer admit are removed from the sidebar and newly admitted files are added, while the watch registrations themselves stay untouched. Explicitly named files (and uploads, and stdin pipes) are never removed, even when the new rules exclude their path.

Notes:

- Patterns registered from a different working directory are skipped — run `ml --reload` from that directory.
- Patterns registered by ml ≤ 0.1.0 cannot be reloaded (their `.mlignore` lines and flag values cannot be told apart); re-register them to apply the new rules.
- Patterns the server skips (no longer registered, or not reloadable) are reported with a warning line; the remaining patterns still reload.
- Without a running ml server there is nothing to reload, and the command says so.

## Sessions and server lifecycle

### Status, shutdown, restart

`ml` runs in the background by default — the command returns immediately, leaving the shell free for other work. This makes it easy to incorporate into scripts, tool chains, or LLM-driven workflows.

``` console
$ ml -w 'docs/**/*.md'
http://localhost:6275
  http://localhost:6275/?file=a1b2c3d4  docs/README.md
  http://localhost:6275/?file=e5f6a7b8  docs/setup.md
ml: serving at http://localhost:6275 (pid 4821)
ml: 1 group(s), 12 file(s), 1 pattern(s); watcher: 1 root, 0 dir, 0 file
$ # shell is available immediately
```

The file links go to **stdout** (one per opened file, safe to pipe into other tools); the `ml:` summary lines go to **stderr**. When more than 10 files are opened, the link list is cut to the first 3 with a hint — pass `--verbose` to list every link.

Use `--status` to check all running ml servers, and `--shutdown` / `--restart` to stop or restart one (restart preserves the session — useful after upgrading the binary):

``` console
$ ml --status              # Show all running ml servers
http://localhost:6275 (pid 4821, v0.2.0 abc1234)
  default: 5 file(s)
    watching: /Users/you/project/src/**/*.md, /Users/you/project/*.md
  docs: 2 file(s)
    watching: /Users/you/project/docs/**/*.md
    excluding: drafts/**
    watcher: healthy — 2 root, 0 dir, 0 file

$ ml --shutdown            # Shut down the ml server on the default port
$ ml --shutdown -p 6276    # Shut down the ml server on a specific port
$ ml --restart             # Restart the ml server on the default port
```

`--status` also marks ports that are not a healthy ml server:

``` console
$ ml --status
http://localhost:6275 (stale: no server; log file left over — run ml --prune)

http://localhost:6280 (not ml: app=mo version=1.6.8 — use --port)

http://localhost:6275 (pid 12345, 0.1.0) (no app field; ml <=0.1.0 or upstream mo)
```

If you need the ml server to run in the foreground (e.g. for debugging), use `--foreground`:

``` console
$ ml --foreground README.md
```

### Backup and restore

`ml` automatically saves session state (open files and watch patterns per group) when files are added or removed. When starting a new server, the previous session is automatically restored and merged with any files specified on the command line. Restored session entries appear first, followed by newly specified files.

``` console
$ ml README.md CHANGELOG.md       # Start with two files
$ ml --shutdown                   # Shut down the server
$ ml                              # Restores README.md and CHANGELOG.md
$ ml TODO.md                      # Restores previous session + adds TODO.md
```

Use `--close` to remove specific files from the running server:

``` console
$ ml --close README.md            # Close a file from the default group
$ ml --close docs/*.md -t docs    # Close files from the "docs" group
```

Use `--clear` to remove a saved session. If a server is running, it is automatically restarted with an empty state:

``` console
$ ml --clear                      # Clear saved session for the default port
$ ml --clear -p 6276              # Clear saved session for a specific port
```

### Cleaning up logs and sessions

ml keeps a rotating log per port under `$XDG_STATE_HOME/ml/log/` and a saved session per port under `$XDG_STATE_HOME/ml/backup/`. A server that exited without `--shutdown` (crash, reboot) leaves both behind, and `ml --status` lists such ports as stale. Client-only commands (`--status`, `--shutdown`, `--restart`, `--prune`, ...) no longer create a log file as a side effect.

`ml --prune` removes the log files of ports that no longer answer, keeping everything that belongs to a running server:

``` console
$ ml --prune
ml: pruned 2 stale log file(s); kept 1 running server
ml: kept saved session for port 6275 (use --prune-backups to remove)
```

Saved sessions are only reported by default and are removed with `--prune-backups`, which asks for confirmation per port just like `--clear`:

``` console
$ ml --prune --prune-backups
ml: remove saved session for port 6275? [Y/n]
```

Both commands accept `--json` for scripting.

## Scripting and automation

`ml` is designed to be driven by scripts, tool chains, and AI agents. Its output contract is stable:

- **stdout is machine-readable.** The server URL first, then one deeplink per opened file (first column: URL, second column: file path). With `--json`, stdout carries a single JSON document instead.
- **stderr is for humans.** Summaries and warnings are prefixed with `ml:` and never mix into stdout.
- **The command returns immediately.** The server runs in the background; there is no need for `&`, `nohup`, or waiting. Use `--foreground` when you want it attached.
- **Deeplinks are stable.** Each file gets an ID derived from its path, so `http://localhost:6275/?file=a1b2c3d4` keeps working across server restarts.

``` console
$ ml README.md --json
{
  "url": "http://localhost:6275",
  "files": [
    {
      "url": "http://localhost:6275/?file=a1b2c3d4",
      "name": "README.md",
      "path": "/Users/you/project/README.md"
    }
  ]
}
```

`--status` also supports `--json`:

``` console
$ ml --status --json
[
  {
    "url": "http://localhost:6275",
    "status": "running",
    "pid": 12345,
    "version": "0.2.0",
    "revision": "abc1234",
    "app": "ml",
    "watcher": { "status": "healthy", "roots": 1, "dirWatches": 0, "fileWatches": 0, "failed": 0, "pendingRetries": 0, "circuitOpen": false, "totalFailures": 0, "totalRecovered": 0 },
    "groups": [
      {
        "name": "default",
        "files": 3,
        "patterns": ["**/*.md"],
        "patternFilters": [
          {
            "pattern": "**/*.md",
            "rules": { "base": "/Users/you/project", "excludes": ["**/node_modules/**"] }
          }
        ]
      }
    ]
  }
]
```

Client-only verbs (`--status`, `--shutdown`, `--restart`, `--reload`, `--unwatch`, `--close`, `--clear`, `--prune`) talk to the running server over HTTP and exit; they never start one. All of them target the port selected by `--port`.

## Troubleshooting

**Live-reload stopped working, or new files stopped appearing in the sidebar.**

Check the watcher's health:

``` console
$ ml --status
http://localhost:6275 (pid 4821, v0.2.0 abc1234)
  default: 742 file(s)
    watching: /Users/you/project/**/*.md
    watcher: degraded — 2 failed registration(s), 2 retry(s) pending (last: FSEventStreamStart failed at /Users/you/project/link)
```

`degraded` means some watch registrations failed — usually because the pattern expanded past the OS watch limit (on macOS this surfaces as `FSEventStreamStart failed`). ml keeps retrying in the background with exponential backoff, and the web UI shows a yellow banner until it recovers. To fix it faster:

- Narrow the discovery: add a `.mlignore` (one gitignore-style line per tree to skip, e.g. `external-repo/**`) and run `ml --reload`.
- Remove patterns you no longer need: `ml --unwatch <pattern>` (see [Removing watch patterns](#removing-watch-patterns)).
- Restart with a clean slate: `ml --restart`.

The browser tab shows the same information: a degraded watcher raises a banner with a **Retry now** button, which asks the server to retry the failed registrations immediately.

**A huge `lsof` output for the ml process.**

On macOS, FSEvents does not keep one file descriptor per watched path, so ml holding thousands of watch targets does not mean thousands of open files. If `lsof -p <pid> | wc -l` is large, the pid is probably another program — for example a different watcher-based tool that was started on the same port. `ml --status` tells the two apart (see below).

**The port is occupied by another tool.**

`ml` probes the status API before attaching to a running server, and refuses servers that report a different identity. `ml --status` marks them:

``` console
$ ml --status
http://localhost:6275 (not ml: app=mo version=1.6.8 — use --port)
```

Start ml on a different port with `-p`, or stop the other program first. Servers without an identity field (ml ≤ 0.1.0, or a compatible program) are still accepted, with a note.

## Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--target` | `-t` | `default` | Tab group for this invocation's files (a group maps to a URL path, e.g. `-t design` serves at `/design`) |
| `--port` | `-p` | `6275` | Server port; each port hosts one independent session |
| `--bind` | `-b` | `localhost` | Address the server binds to (non-loopback addresses expose it without authentication) |
| `--open` | | | Always open the browser, even when only attaching to a running server |
| `--no-open` | | | Never open the browser automatically |
| `--watch` | `-w` | `false` | Register directory and glob arguments as watch patterns; new matching files open automatically |
| `--recursive` | `-R` | `false` | Recurse into subdirectories when a directory argument is given |
| `--exclude` | | | Gitignore-style pattern of paths to skip during discovery (repeatable) |
| `--include-hidden` | | `false` | Also discover dot-prefixed files and directories |
| `--ignore-file` | | `.mlignore` | Ignore file read from the working directory (`''` disables) |
| `--reload` | | | Re-apply `.mlignore` and `--exclude` rules to patterns registered from the current directory |
| `--unwatch` | | `false` | Remove the watch patterns matching the given directory or glob arguments |
| `--close` | | | Close the given files instead of opening them |
| `--status` | | | Show status of every running ml server (groups, patterns, watcher health) |
| `--shutdown` | | | Shut down the running ml server on the target port |
| `--restart` | | | Restart the running ml server on the target port, preserving the session |
| `--clear` | | | Discard the saved session for the target port (restarts a running server empty) |
| `--prune` | | | Remove log files of servers that are no longer running |
| `--prune-backups` | | | With `--prune`, also remove saved sessions of stopped servers (asks for confirmation) |
| `--foreground` | | | Run the server in the foreground instead of backgrounding it |
| `--verbose` | | | List every deeplink and print the full startup summary |
| `--json` | | | Output structured JSON on stdout instead of URLs (also with `--status` and `--prune`) |
| `--version` | | | Show version |
| `--dangerously-allow-remote-access` | | | Skip the confirmation prompt for a non-loopback `--bind` (unauthenticated remote access; trusted networks only) |

> [!WARNING]
> Binding to a non-localhost address exposes ml to the network **without any authentication**. Remote clients can read any file accessible by the user, browse the filesystem via glob patterns, and shut down the server. A confirmation prompt is shown when `--bind` is set to a non-loopback address.

## Build

Requires Go 1.26+ and [pnpm](https://pnpm.io/).

``` console
$ make build
```

## License

- [MIT License](LICENSE)

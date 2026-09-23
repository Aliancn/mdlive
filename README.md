# mdlive

[![build](https://github.com/Aliancn/mdlive/actions/workflows/ci.yml/badge.svg)](https://github.com/Aliancn/mdlive/actions/workflows/ci.yml)

`ml` is a **M**arkdown **L**ive viewer that opens `.md` files in a browser.

## Features

- GitHub-flavored Markdown (tables, task lists, footnotes, etc.)
- Syntax highlighting ([Shiki](https://shiki.style/))
- [Mermaid](https://mermaid.js.org/) diagram rendering
- LaTeX math rendering ([KaTeX](https://katex.org/))
- [GitHub Alerts](https://docs.github.com/en/get-started/writing-on-github/getting-started-with-writing-and-formatting-on-github/basic-writing-and-formatting-syntax#alerts) (admonitions)
- Fullscreen zoom modal for images and Mermaid diagrams
- Dark / light theme
- File grouping
- Table of contents panel
- Flat / tree sidebar view with drag-and-drop reorder
- File name / heading title sidebar display toggle (per-group)
- Full-text search across file names and content
- YAML frontmatter display (collapsible metadata block)
- MDX file support (renders as Markdown, strips `import`/`export`, escapes JSX tags)
- Content font size toggle (small / medium / large / extra large)
- Wide / narrow content width toggle
- Raw markdown view
- Copy content (Markdown / Text / HTML)
- Server restart with session preservation
- Auto session backup and restore
- Drag-and-drop file addition from the OS file manager (content is loaded in-memory; live-reload is not supported for dropped files)
- Stdin pipe support (`cat file.md | ml`)
- Live-reload on save (for files opened via CLI)
- Watcher health reporting: failed watch registrations are retried automatically and surfaced in the UI banner, `ml --status`, and the startup summary

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

## Usage

``` console
$ ml README.md                          # Open a single file
$ ml README.md CHANGELOG.md docs/*.md   # Open multiple files
$ ml docs/                              # Open all .md files in a directory
$ ml spec.md --target design            # Open in a named group
$ cat notes.md | ml                     # Read Markdown from stdin
```

`ml` opens Markdown files in a browser with live-reload. When you save a file, the browser automatically reflects the changes.

### Reading from stdin

When no positional arguments are given and stdin is redirected (not a terminal), `ml` reads Markdown content from stdin.

``` console
$ cat notes.md | ml
$ some-command | ml --target output
$ ml < notes.md
```

The content is loaded in-memory with a generated name (`stdin-<hash>.md`). Piping the same content again reuses the existing entry (deduplicated by content hash).

### Single server, multiple files

By default, `ml` runs a single server on port `6275`. If a server is already running on the same port, subsequent `ml` invocations add files to the existing session instead of starting a new one.

``` console
$ ml README.md          # Starts an ml server in the background
$ ml CHANGELOG.md       # Adds the file to the running ml server
```

To run a completely separate session, use a different port:

``` console
$ ml draft.md -p 6276
```

### Groups

Files can be organized into named groups using the `--target` (`-t`) flag. Each group gets its own URL path and sidebar.

``` console
$ ml spec.md --target design      # Opens at http://localhost:6275/design
$ ml api.md --target design       # Adds to the "design" group
$ ml notes.md --target notes      # Opens at http://localhost:6275/notes
```

### Watch mode and glob patterns

`--watch` (`-w`) turns on watch mode. Directory and glob positional arguments are registered as watch patterns, matching files are opened, and new matching files are picked up automatically.

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

Without `--watch`, globs are expanded once and directory arguments open matching files without live-watching new additions:

``` console
$ ml docs/                                     # Open every .md directly in docs/
$ ml -R docs/                                  # Open every .md under docs/ (recursive)
$ ml 'docs/*.md'                               # Expand and open matching .md files
```

#### Removing watch patterns

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

#### Excluding files

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

#### Symlinked directories

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

### Sidebar view modes

The sidebar supports flat and tree view modes. Flat view shows file names only, while tree view displays the directory hierarchy.

### Starting and stopping

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

The file links go to **stdout** (one per opened file, safe to pipe into other tools); the `ml:` summary lines go to **stderr**. When more than 10 files are opened, the link list is cut to the first 3 with a hint — pass `--verbose` to list every link. With `--json`, stdout carries the full JSON object instead.

Use `--status` to check all running ml servers, and `--shutdown` to stop one:

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

### Server restart

Click the restart button (bottom-right corner) or run `ml --restart` to restart the `ml` server process. The current session — all open files and groups — is preserved across the restart. This is useful when you have updated the `ml` binary and want to pick up the new version without re-opening your files.

### Session backup and restore

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

### Troubleshooting

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

### JSON output

Use `--json` to get structured JSON output on stdout, useful for scripting and integration with other tools.

``` console
$ ml --json README.md
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

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--target` | `-t` | `default` | Group name |
| `--port` | `-p` | `6275` | Server port |
| `--bind` | `-b` | `localhost` | Bind address (e.g. `0.0.0.0`) |
| `--open` | | | Always open browser |
| `--no-open` | | | Never open browser |
| `--status` | | | Show all running ml servers |
| `--watch` | `-w` | `false` | Treat directory and glob arguments as watch patterns |
| `--unwatch` | | `false` | Remove watched patterns for the given directory or glob arguments |
| `--recursive` | `-R` | `false` | Recurse into subdirectories when a directory is given |
| `--exclude` | | | Glob pattern of files to exclude from discovery (repeatable) |
| `--include-hidden` | | `false` | Include dot-prefixed hidden files and directories |
| `--ignore-file` | | `.mlignore` | Ignore file read from the working directory (`''` disables) |
| `--close` | | | Close files instead of opening them |
| `--shutdown` | | | Shut down the running ml server |
| `--restart` | | | Restart the running ml server |
| `--clear` | | | Clear saved session (restarts server if running) |
| `--reload` | | | Re-apply `.mlignore` and `--exclude` rules to patterns registered from the current directory |
| `--prune` | | | Remove log files of servers that are no longer running |
| `--prune-backups` | | | With `--prune`, also remove saved sessions of stopped servers (asks for confirmation) |
| `--foreground` | | | Run ml server in foreground |
| `--json` | | | Output structured data as JSON to stdout |
| `--verbose` | | | List every deeplink and print the full startup summary |
| `--version` | | | Show version |
| `--dangerously-allow-remote-access` | | | Allow remote access without authentication (trusted networks only) |

> [!WARNING]
> Binding to a non-localhost address exposes ml to the network **without any authentication**. Remote clients can read any file accessible by the user, browse the filesystem via glob patterns, and shut down the server. A confirmation prompt is shown when `--bind` is set to a non-loopback address.

## Build

Requires Go 1.26+ and [pnpm](https://pnpm.io/).

``` console
$ make build
```

## License

- [MIT License](LICENSE)

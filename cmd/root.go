package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/k1LoW/errors"

	"github.com/Aliancn/mdlive/internal/backup"
	"github.com/Aliancn/mdlive/internal/ignore"
	"github.com/Aliancn/mdlive/internal/logfile"
	"github.com/Aliancn/mdlive/internal/server"
	"github.com/Aliancn/mdlive/version"
	"github.com/k1LoW/donegroup"
	"github.com/muesli/termenv"
	"github.com/pkg/browser"
	"github.com/spf13/cobra"
	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

const (
	// probeTimeoutFast is used when a missing server is the normal case (e.g. first launch).
	probeTimeoutFast = 500 * time.Millisecond
	// probeTimeoutDefault is used when the server is expected to be running.
	probeTimeoutDefault = 2 * time.Second
	// deadChildGrace is how long waitForReady keeps polling after the spawned
	// child dies, giving a race-winning server a chance to respond before the
	// failure is reported.
	deadChildGrace = 1 * time.Second

	markdownGlob          = "*.md"
	markdownGlobRecursive = "**/*.md"
)

// errServerConflict is returned by waitForReady when a ml server other than
// the spawned child owns the port (e.g. lost a concurrent startup race).
var errServerConflict = errors.New("another ml server is already running")

var (
	target                       string
	port                         int
	bind                         string
	open                         bool
	noOpen                       bool
	restore                      string
	shutdownServer               bool
	restartServer                bool
	foreground                   bool
	statusServer                 bool
	watchMode                    bool
	unwatchMode                  bool
	recursive                    bool
	closeFiles                   bool
	clearBackup                  bool
	jsonOutput                   bool
	dangerouslyAllowRemoteAccess bool
	excludes                     []string
	includeHidden                bool
	ignoreFile                   string
)

// patternSpec pairs a watch pattern with the discovery rules it carries.
type patternSpec struct {
	Pattern string
	Rules   ignore.Rules
}

var rootCmd = &cobra.Command{
	Use:   "ml [flags] [FILE|DIR ...]",
	Short: "ml is a Markdown viewer that opens .md files in a browser.",
	Long: `ml is a Markdown viewer that opens .md files in a browser with live-reload.

It runs in the background, serving Markdown files using a built-in React SPA,
and automatically refreshes the browser when files are saved.

Examples:
  ml README.md                          Open a single file
  ml README.md CHANGELOG.md docs/*.md   Open multiple files
  ml spec.md --target design            Open in a named group
  ml draft.md --port 6276               Use a different port
  cat notes.md | ml                     Read Markdown from stdin
  cmd | ml --target output              Pipe command output into a group

Single Server, Multiple Files:
  By default, ml runs a single server on port 6275.
  If a ml server is already running on the same port, subsequent ml
  invocations add files to the existing session instead of starting a new one.

  $ ml README.md          # Starts a ml server in the background
  $ ml CHANGELOG.md       # Adds the file to the running ml server

  To run a completely separate session, use a different port:

  $ ml draft.md -p 6276

Groups:
  Files can be organized into named groups using the --target (-t) flag.
  Each group gets its own URL path (e.g., http://localhost:6275/design)
  and its own sidebar in the browser.

  $ ml spec.md --target design      # Opens at /design
  $ ml api.md --target design       # Adds to the "design" group
  $ ml notes.md --target notes      # Opens at /notes

  If no --target is specified, files are added to the "default" group.

Starting and Stopping:
  ml runs in the background by default. The command returns
  immediately, leaving the shell free for other work.

  $ ml README.md            # Starts ml in the background
  $ ml --status             # Shows all running ml servers
  $ ml --shutdown           # Shuts it down
  $ ml --restart            # Restarts it (preserving session)

  Use --foreground to keep the ml server in the foreground.

Session Restore:
  ml automatically saves session state. When starting a new server,
  the previous session is restored and merged with any specified files.

  $ ml README.md CHANGELOG.md    # Start with two files
  $ ml --shutdown                # Shut down the server
  $ ml                           # Restores README.md and CHANGELOG.md
  $ ml TODO.md                   # Restores previous session + adds TODO.md

  Use --clear to remove a saved session.

Live-Reload:
  ml watches all opened files for changes using filesystem notifications.
  When a file is saved, the browser automatically re-renders the content.

Supported Markdown Features:
  - GitHub Flavored Markdown (tables, task lists, strikethrough, autolinks)
  - Syntax-highlighted code blocks (via Shiki)
  - Mermaid diagrams
  - LaTeX math rendering (via KaTeX)
  - GitHub Alerts (admonitions)
  - Fullscreen zoom modal for images and Mermaid diagrams
  - YAML frontmatter (displayed as a collapsible metadata block)
  - MDX files (rendered as Markdown with import/export stripped and JSX tags escaped)
  - Raw HTML

Watch mode and glob patterns:
  --watch (-w) turns on watch mode. Directory and glob positional
  arguments are then registered as watch patterns; matching files are
  opened and new files are picked up automatically. Combine with
  --recursive (-R) to descend into subdirectories.

  $ ml -w '**/*.md'                   Watch all .md files recursively
  $ ml -w 'docs/**/*.md' -t docs      Watch docs/ tree in "docs" group
  $ ml -w '*.md' 'docs/**/*.md'       Multiple patterns (positional)
  $ ml -w docs/                       Watch docs/*.md
  $ ml -w -R docs/                    Watch docs/**/*.md
  $ ml -wR docs/                      Same (short-combined form)
  $ ml --unwatch '**/*.md'            Stop watching a pattern
  $ ml --unwatch docs/                Stop watching docs/*.md
  $ ml --unwatch -R docs/             Stop watching all patterns under docs/

  Without --watch, globs are expanded once and a directory argument
  opens the matching files without live-watching new additions.

  $ ml -R docs/                       Open every .md under docs/ once

Excluding files:
  By default, dot-prefixed files and directories (e.g. .git/) are not
  opened or watched. --include-hidden turns that off. --exclude values
  (repeatable) and the .mlignore file in the working directory filter
  what directory and glob arguments discover; they use gitignore-style
  syntax with rules anchored at the working directory.

  $ ml -R . --exclude 'vendor/**'     Skip everything under vendor/
  $ ml -w '**/*.md' --exclude '**/node_modules/**'
  $ ml .git/README.md                 Explicit file arguments are never filtered
  $ ml -w '**/*.md' --include-hidden  Watch hidden files too

  A pattern without a separator (vendor) matches the base name at any
  depth; a pattern with a separator (vendor/**, /docs/drafts/**) is
  anchored at the working directory. '!'-prefixed lines re-include
  files, and the last matching line wins. Files already shown in the
  sidebar are never removed by excludes. Registered rules are shown by
  --status and updated by re-running ml with the new flags (or via
  ml --clear).

WARNING: --bind with a non-loopback address:
  Binding to a non-localhost address (e.g. 0.0.0.0) exposes ml to the
  network without any authentication. Remote clients can read any file
  accessible by this user, browse the filesystem via glob patterns, and
  shut down the server. A confirmation prompt is shown before starting.`,
	Args:    cobra.ArbitraryArgs,
	RunE:    run,
	Version: version.Version,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.Flags().StringVarP(&target, "target", "t", server.DefaultGroup, "Tab group name")
	rootCmd.Flags().IntVarP(&port, "port", "p", 6275, "Server port")
	rootCmd.Flags().StringVarP(&bind, "bind", "b", "localhost", "Bind address (e.g. localhost, 0.0.0.0)")
	rootCmd.Flags().BoolVar(&open, "open", false, "Always open browser (even when adding to existing group)")
	rootCmd.Flags().BoolVar(&noOpen, "no-open", false, "Do not open browser automatically")
	rootCmd.MarkFlagsMutuallyExclusive("open", "no-open")
	rootCmd.Flags().BoolVar(&shutdownServer, "shutdown", false, "Shut down the running ml server on the specified port")
	rootCmd.Flags().BoolVar(&restartServer, "restart", false, "Restart the running ml server on the specified port")
	rootCmd.MarkFlagsMutuallyExclusive("shutdown", "restart")
	rootCmd.Flags().StringVar(&restore, "restore", "", "Restore state from file (internal use)")
	rootCmd.Flags().MarkHidden("restore") //nolint:errcheck
	rootCmd.Flags().BoolVar(&foreground, "foreground", false, "Run ml server in foreground (do not background)")
	rootCmd.Flags().BoolVar(&statusServer, "status", false, "Show status of all running ml servers")
	rootCmd.Flags().BoolVarP(&watchMode, "watch", "w", false, "Treat directory and glob arguments as watch patterns")
	rootCmd.Flags().BoolVar(&unwatchMode, "unwatch", false, "Remove watched patterns for the given directory or glob arguments")
	rootCmd.Flags().BoolVarP(&recursive, "recursive", "R", false, "Recurse into subdirectories when a directory is given")
	rootCmd.Flags().BoolVar(&closeFiles, "close", false, "Close files instead of opening them")
	rootCmd.Flags().BoolVar(&clearBackup, "clear", false, "Clear saved session for the specified port")
	rootCmd.Flags().BoolVar(&jsonOutput, "json", false, "Output structured data as JSON to stdout")
	rootCmd.Flags().BoolVar(&dangerouslyAllowRemoteAccess, "dangerously-allow-remote-access", false, "Allow remote access without authentication. Recommended only for trusted networks.")
	rootCmd.Flags().StringArrayVar(&excludes, "exclude", nil, "Glob pattern of files to exclude from discovery (repeatable)")
	rootCmd.Flags().BoolVar(&includeHidden, "include-hidden", false, "Include dot-prefixed hidden files and directories in discovery")
	rootCmd.Flags().StringVar(&ignoreFile, "ignore-file", ".mlignore", "Name of the ignore file read from the working directory ('' disables)")
}

func run(cmd *cobra.Command, args []string) (retErr error) {
	if !foreground || restore != "" {
		logCleanup, err := logfile.Setup(port)
		if err != nil {
			slog.Warn("failed to setup log file, using stderr", "error", err)
		} else {
			defer logCleanup()
			// Cobra prints RunE errors to stderr, which is /dev/null for
			// background children. Record fatal errors in the log file too
			// (registered after logCleanup so it runs before the file is
			// closed). Skipped when slog still writes to stderr, where cobra
			// prints the error anyway.
			defer func() {
				if retErr != nil {
					slog.Error("ml exited with error", "error", retErr)
				}
			}()
		}
	}

	bind = strings.Trim(bind, "[]")
	addr := net.JoinHostPort(bind, strconv.Itoa(port))

	if clearBackup {
		wasServerRunning := false
		if _, err := probeServer(addr, probeTimeoutFast); err == nil {
			wasServerRunning = true
		} else if errors.Is(err, errForeignServer) {
			// Clearing the session of another application's server is out of
			// scope; refuse instead of silently ignoring it.
			return err
		}
		hasBackup := backup.Exists(port)

		if !wasServerRunning && !hasBackup {
			fmt.Fprintf(os.Stderr, "ml: no saved session for port %d\n", port)
			return nil
		}
		fmt.Fprintf(os.Stderr, "ml: clear saved session for port %d? [Y/n] ", port)
		scanner := bufio.NewScanner(os.Stdin)
		if !scanner.Scan() {
			fmt.Fprintln(os.Stderr, "ml: canceled")
			return nil
		}
		ans := strings.TrimSpace(scanner.Text())
		if ans != "" && strings.ToLower(ans) != "y" && strings.ToLower(ans) != "yes" {
			fmt.Fprintln(os.Stderr, "ml: canceled")
			return nil
		}

		if wasServerRunning {
			// Shut down the running server, wait for it to stop,
			// then restart with an empty state.
			if err := doShutdown(addr); err != nil {
				return err
			}
			if err := waitForServerDown(addr); err != nil {
				return err
			}
		}

		if hasBackup {
			if err := backup.Remove(port); err != nil {
				return err
			}
		}

		if wasServerRunning {
			// Restart the server with an empty state.
			if _, err := spawnNewProcess(addr, ""); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "ml: cleared session and restarted server on port %d\n", port)
		} else {
			fmt.Fprintf(os.Stderr, "ml: cleared saved session for port %d\n", port)
		}
		return nil
	}

	if statusServer {
		return doStatus()
	}

	if shutdownServer {
		return doShutdown(addr)
	}

	if restartServer {
		return doRestart(addr)
	}

	if unwatchMode {
		if watchMode {
			return fmt.Errorf("cannot use --unwatch with --watch")
		}
		if len(args) == 0 {
			return fmt.Errorf("--unwatch requires a glob pattern or directory argument")
		}

		resolvedTarget, err := server.ResolveGroupName(target)
		if err != nil {
			return fmt.Errorf("invalid target group name %q: %w", target, err)
		}

		patterns, err := resolveUnwatchArgs(args, recursive, addr, resolvedTarget)
		if err != nil {
			return err
		}

		return doUnwatch(addr, patterns, resolvedTarget)
	}

	if closeFiles {
		if watchMode {
			return fmt.Errorf("cannot use --close with --watch")
		}
		if len(args) == 0 {
			return fmt.Errorf("--close requires at least one file argument")
		}

		resolvedTarget, err := server.ResolveGroupName(target)
		if err != nil {
			return fmt.Errorf("invalid target group name %q: %w", target, err)
		}

		closedPaths, err := doClose(addr, args, resolvedTarget)
		if len(closedPaths) > 0 {
			names := displayNames(closedPaths)
			for _, name := range names {
				fmt.Printf("  %s\n", name)
			}
			fmt.Fprintf(os.Stderr, "ml: closed %d file(s) from http://%s\n", len(closedPaths), addr)
		}
		return err
	}

	if restore != "" {
		filesByGroup, patternsByGroup, patternFilters, uploadedFiles, err := loadRestoreData(restore)
		if err != nil {
			return fmt.Errorf("failed to restore state: %w", err)
		}
		return startServer(cmd.Context(), addr, filesByGroup, restorePatternSpecs(patternsByGroup, patternFilters), uploadedFiles)
	}

	resolved, err := server.ResolveGroupName(target)
	if err != nil {
		return fmt.Errorf("invalid target group name %q: %w", target, err)
	}
	target = resolved

	if recursive && len(args) == 0 {
		return fmt.Errorf("--recursive (-R) requires a directory argument")
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("cannot get working directory: %w", err)
	}
	filter, err := resolveFilter(cwd)
	if err != nil {
		return err
	}

	files, patternSpecs, err := resolveArgs(args, watchMode, recursive, filter)
	if err != nil {
		return err
	}

	if watchMode && len(patternSpecs) == 0 {
		if len(files) > 0 {
			return fmt.Errorf("--watch (-w) requires a glob pattern or directory argument\n(hint: the shell may have expanded the glob pattern; quote it, e.g. -w '**/*.md')")
		}
		return fmt.Errorf("--watch (-w) requires a glob pattern or directory argument")
	}

	// Detect redirected stdin when no positional arguments are given.
	var stdinData *server.UploadedFileData
	if isStdinRedirected() {
		if len(args) > 0 {
			return fmt.Errorf("cannot use redirected stdin with positional arguments")
		}
		if watchMode {
			return fmt.Errorf("cannot use --watch (-w) with redirected stdin")
		}
		name, content, err := readStdin(os.Stdin)
		if err != nil {
			return err
		}
		stdinData = &server.UploadedFileData{
			Name:    name,
			Content: content,
			Group:   target,
		}
	}

	// When no files, patterns, or stdin are specified and a server is already
	// running, just open the browser and exit.
	if len(files) == 0 && len(patternSpecs) == 0 && stdinData == nil {
		if _, err := probeServer(addr, probeTimeoutDefault); err == nil {
			openBrowser(addr)
			return nil
		} else if errors.Is(err, errForeignServer) {
			return err
		}
	}

	// Try adding to an existing server.
	if stdinData != nil || len(files) > 0 || len(patternSpecs) > 0 {
		result, probeErr := probeServer(addr, probeTimeoutFast)
		if probeErr == nil {
			noteLegacyServer(addr, result)
			isNewGroup := !slices.Contains(result.groups, target)

			var deeplinks []deeplinkEntry
			fileEntries := postFiles(result.client, addr, target, files)
			deeplinks = append(deeplinks, fileEntries...)
			patternEntries, patternsAdded := postPatterns(result.client, addr, target, patternSpecs)
			deeplinks = append(deeplinks, patternEntries...)

			var stdinUploadErr error
			if stdinData != nil {
				entry, err := postUploadedFile(result.client, addr, target, stdinData.Name, stdinData.Content)
				if err != nil {
					stdinUploadErr = err
					slog.Warn("failed to upload stdin content", "error", err)
				} else {
					deeplinks = append(deeplinks, entry)
				}
			}

			if stdinData != nil && len(files) == 0 && len(patternSpecs) == 0 && stdinUploadErr != nil {
				return stdinUploadErr
			}

			// Count only what was actually accepted by the running server so
			// the "added N item(s)" line does not overstate on partial POST
			// failures. postFiles appends exactly one entry per accepted file.
			added := len(fileEntries) + patternsAdded
			if stdinData != nil && stdinUploadErr == nil {
				added++
			}
			slog.Info("added to existing server", "files", len(files), "patterns", len(patternSpecs), "stdin", stdinData != nil, "addr", addr)
			emitServeOutput(addr, deeplinks, false)
			fmt.Fprintf(os.Stderr, "ml: added %d item(s) to http://%s\n", added, addr)

			if isNewGroup || open {
				openBrowser(addr)
			}
			return nil
		} else if errors.Is(probeErr, errForeignServer) {
			// A live server identified itself as another application; pushing
			// files into it would silently corrupt its state.
			return probeErr
		}
	}

	filesByGroup := map[string][]string{target: files}
	specsByGroup := map[string][]patternSpec{target: patternSpecs}

	// Restore backup and merge with specified files/patterns
	var rd server.RestoreData
	if err := backup.Load(port, &rd); err != nil {
		slog.Warn("failed to load backup", "error", err)
	}
	restoredFiles, restoredPatterns, restoredFilters, restoredUploads := filterValidRestoreData(&rd)
	restoredSpecs := restorePatternSpecs(restoredPatterns, restoredFilters)
	var uploadedFiles []server.UploadedFileData
	if len(restoredFiles) > 0 || len(restoredSpecs) > 0 || len(restoredUploads) > 0 {
		slog.Info("restoring session from backup", "port", port)
		fmt.Fprintf(os.Stderr, "ml: restoring previous session for port %d\n", port)
		filesByGroup = mergeGroups(restoredFiles, filesByGroup)
		specsByGroup = mergePatternSpecGroups(restoredSpecs, specsByGroup)
		uploadedFiles = restoredUploads
	}

	// Append stdin content to uploaded files for the new server.
	if stdinData != nil {
		uploadedFiles = append(uploadedFiles, *stdinData)
	}

	// Prompt only when actually starting a new server (not adding to existing one).
	if !isLoopbackBind(bind) {
		slog.Warn("binding to non-loopback address", "bind", bind, "dangerously-allow-remote-access", dangerouslyAllowRemoteAccess)
	}
	if !isLoopbackBind(bind) && !dangerouslyAllowRemoteAccess {
		if stdinData != nil {
			return fmt.Errorf("cannot use redirected stdin with non-loopback bind without --dangerously-allow-remote-access")
		}
		o := termenv.NewOutput(os.Stderr)
		c := func(s string) termenv.Style { return o.String(s).Foreground(o.Color("208")) }
		fmt.Fprintln(os.Stderr, c("SECURITY WARNING:").Bold(),
			c(fmt.Sprintf("Binding to %s instead of localhost. ml has no authentication -- remote clients can:", bind)))
		fmt.Fprintln(os.Stderr, c("  - Read any file accessible by this user"))
		fmt.Fprintln(os.Stderr, c("  - Browse the filesystem via glob patterns"))
		fmt.Fprintln(os.Stderr, c("  - Shut down or restart the server"))
		fmt.Fprintf(os.Stderr, "Continue? [y/N] ")
		scanner := bufio.NewScanner(os.Stdin)
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return err
			}
			fmt.Fprintln(os.Stderr, "ml: canceled")
			return nil
		}
		ans := strings.ToLower(strings.TrimSpace(scanner.Text()))
		if ans != "y" && ans != "yes" {
			fmt.Fprintln(os.Stderr, "ml: canceled")
			return nil
		}
	}

	if foreground {
		return startServer(cmd.Context(), addr, filesByGroup, specsByGroup, uploadedFiles)
	}
	return startBackground(addr, filesByGroup, specsByGroup, uploadedFiles)
}

// mergeGroups merges base and additional group maps, with base entries first.
// Entries from additional that already exist in base for the same group are skipped.
func mergeGroups(base, additional map[string][]string) map[string][]string {
	if len(base) == 0 && len(additional) == 0 {
		return nil
	}
	merged := make(map[string][]string)
	for group, items := range base {
		merged[group] = append(merged[group], items...)
	}
	for group, items := range additional {
		seen := make(map[string]struct{}, len(merged[group]))
		for _, v := range merged[group] {
			seen[v] = struct{}{}
		}
		for _, v := range items {
			if _, ok := seen[v]; !ok {
				merged[group] = append(merged[group], v)
				seen[v] = struct{}{}
			}
		}
	}
	return merged
}

// filterValidRestoreData validates restore data by checking that file paths still exist.
func filterValidRestoreData(rd *server.RestoreData) (map[string][]string, map[string][]string, []server.PatternFilterData, []server.UploadedFileData) {
	filesByGroup := make(map[string][]string)
	for group, paths := range rd.Groups {
		for _, p := range paths {
			if _, err := os.Stat(p); err != nil {
				slog.Info("skipping missing file from backup", "path", p)
				continue
			}
			filesByGroup[group] = append(filesByGroup[group], p)
		}
	}

	patternsByGroup := make(map[string][]string)
	maps.Copy(patternsByGroup, rd.Patterns)

	return filesByGroup, patternsByGroup, rd.PatternFilters, rd.UploadedFiles
}

// restorePatternSpecs builds pattern specs from restored patterns and their
// persisted discovery filters. Patterns without a stored filter get zero
// rules, which restores the default hidden-path behavior.
func restorePatternSpecs(patternsByGroup map[string][]string, filters []server.PatternFilterData) map[string][]patternSpec {
	if len(patternsByGroup) == 0 {
		return nil
	}
	rulesByPattern := make(map[string]ignore.Rules, len(filters))
	for _, f := range filters {
		rulesByPattern[f.Pattern] = f.Rules
	}
	specs := make(map[string][]patternSpec, len(patternsByGroup))
	for group, pats := range patternsByGroup {
		for _, p := range pats {
			specs[group] = append(specs[group], patternSpec{Pattern: p, Rules: rulesByPattern[p]})
		}
	}
	return specs
}

// mergePatternSpecs merges base and additional specs, base entries first. A
// pattern present in both keeps its base position but takes the additional
// rules: the latest invocation wins on rule conflicts, matching how a running
// server treats re-registration.
func mergePatternSpecs(base, additional []patternSpec) []patternSpec {
	if len(additional) == 0 {
		return base
	}
	if len(base) == 0 {
		return additional
	}
	cliRules := make(map[string]ignore.Rules, len(additional))
	for _, s := range additional {
		cliRules[s.Pattern] = s.Rules
	}
	merged := make([]patternSpec, 0, len(base)+len(additional))
	seen := make(map[string]struct{}, len(base)+len(additional))
	for _, s := range base {
		if _, ok := seen[s.Pattern]; ok {
			continue
		}
		if rules, ok := cliRules[s.Pattern]; ok {
			s.Rules = rules
		}
		merged = append(merged, s)
		seen[s.Pattern] = struct{}{}
	}
	for _, s := range additional {
		if _, ok := seen[s.Pattern]; ok {
			continue
		}
		merged = append(merged, s)
		seen[s.Pattern] = struct{}{}
	}
	return merged
}

// mergePatternSpecGroups merges base and additional per-group spec maps, with
// base entries first.
func mergePatternSpecGroups(base, additional map[string][]patternSpec) map[string][]patternSpec {
	if len(base) == 0 {
		return additional
	}
	if len(additional) == 0 {
		return base
	}
	merged := make(map[string][]patternSpec, len(base)+len(additional))
	maps.Copy(merged, base)
	for group, specs := range additional {
		merged[group] = mergePatternSpecs(merged[group], specs)
	}
	return merged
}

func loadRestoreData(path string) (map[string][]string, map[string][]string, []server.PatternFilterData, []server.UploadedFileData, error) {
	data, err := os.ReadFile(path) //nolint:gosec
	if err != nil {
		return nil, nil, nil, nil, err
	}
	os.Remove(path)

	var rd server.RestoreData
	if err := json.Unmarshal(data, &rd); err != nil {
		return nil, nil, nil, nil, err
	}
	return rd.Groups, rd.Patterns, rd.PatternFilters, rd.UploadedFiles, nil
}

func isLoopbackBind(bind string) bool {
	if bind == "localhost" {
		return true
	}
	ip := net.ParseIP(bind)
	return ip != nil && ip.IsLoopback()
}

func hasGlobChars(s string) bool {
	return strings.ContainsAny(s, "*?[")
}

func resolveUnwatchArgs(args []string, recursive bool, addr, groupName string) ([]string, error) {
	var patterns []string
	var registered []string
	var registeredFetched bool
	for _, arg := range args {
		abs, err := filepath.Abs(arg)
		if err != nil {
			return nil, fmt.Errorf("cannot resolve path %s: %w", arg, err)
		}

		if hasGlobChars(arg) {
			patterns = append(patterns, abs)
			continue
		}

		stat, err := os.Stat(abs)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				if !recursive {
					return nil, fmt.Errorf("path not found: %s", abs)
				}
				// Allow deleted directories in recursive mode so users can
				// clean up patterns that remain after the directory is removed.
			} else {
				return nil, fmt.Errorf("cannot stat path %s: %w", abs, err)
			}
		} else if !stat.IsDir() {
			return nil, fmt.Errorf("--unwatch requires glob patterns or directories, not individual files (hint: use --close to remove individual files)")
		}

		if recursive {
			if !registeredFetched {
				registered, err = fetchRegisteredPatterns(addr, groupName)
				if err != nil {
					return nil, err
				}
				registeredFetched = true
			}
			prefix := abs + string(filepath.Separator)
			var matched []string
			for _, p := range registered {
				if strings.HasPrefix(p, prefix) {
					matched = append(matched, p)
				}
			}
			if len(matched) == 0 {
				return nil, fmt.Errorf("no watched patterns found under %s in group %q (use --status to see registered patterns)", abs, groupName)
			}
			patterns = append(patterns, matched...)
		} else {
			patterns = append(patterns, filepath.Join(abs, markdownGlob))
		}
	}
	seen := make(map[string]struct{}, len(patterns))
	unique := make([]string, 0, len(patterns))
	for _, p := range patterns {
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		unique = append(unique, p)
	}

	return unique, nil
}

func fetchRegisteredPatterns(addr, groupName string) ([]string, error) {
	client := &http.Client{Timeout: probeTimeoutDefault}
	resp, err := client.Get(fmt.Sprintf("http://%s/_/api/status", addr))
	if err != nil {
		return nil, fmt.Errorf("failed to query server status: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to query server status: %s", resp.Status)
	}

	var status statusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("failed to decode server status: %w", err)
	}

	for _, g := range status.Groups {
		if g.Name == groupName {
			return g.Patterns, nil
		}
	}
	return nil, fmt.Errorf("group %q not found (use --status to see registered groups)", groupName)
}

// resolveFilter builds the discovery filter for this invocation: the lines of
// the ignore file in the working directory come first, then the --exclude
// values, so that the flags win under last-match-wins. The returned filter is
// never nil: even with no rules it enforces the default hidden-path rule.
func resolveFilter(cwd string) (*ignore.Filter, error) {
	rules := ignore.Rules{Base: filepath.ToSlash(cwd)}
	if ignoreFile != "" {
		lines, err := ignore.ReadLines(filepath.Join(cwd, ignoreFile))
		if err != nil {
			return nil, fmt.Errorf("cannot read %s: %w", ignoreFile, err)
		}
		for _, line := range lines {
			if err := ignore.ValidateLine(line); err != nil {
				fmt.Fprintf(os.Stderr, "ml: warning: ignoring invalid line in %s: %v\n", ignoreFile, err)
				continue
			}
			rules.Excludes = append(rules.Excludes, line)
		}
	}
	for _, value := range excludes {
		if err := ignore.ValidateLine(value); err != nil {
			return nil, fmt.Errorf("invalid --exclude value %q: %w", value, err)
		}
		rules.Excludes = append(rules.Excludes, value)
	}
	if includeHidden {
		rules.IncludeHidden = true
	}
	f, err := rules.Filter()
	if err != nil {
		return nil, err
	}
	return f, nil
}

func resolveArgs(args []string, watchMode, recursive bool, filter *ignore.Filter) (files []string, specs []patternSpec, err error) {
	if filter == nil {
		// A zero filter still enforces the default hidden-path rule.
		filter = &ignore.Filter{}
	}
	for _, arg := range args {
		abs, err := filepath.Abs(arg)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot resolve path %s: %w", arg, err)
		}

		if hasGlobChars(arg) {
			if watchMode {
				specs = append(specs, patternSpec{Pattern: abs, Rules: filter.Rules()})
				continue
			}
			matches, excluded, err := expandGlobPattern(abs, filter)
			if err != nil {
				return nil, nil, err
			}
			if len(matches) == 0 {
				if excluded > 0 {
					return nil, nil, fmt.Errorf("no files matched %s (%d excluded by --exclude/%s)", arg, excluded, ignoreFile)
				}
				return nil, nil, fmt.Errorf("no files matched %s", arg)
			}
			files = append(files, matches...)
			continue
		}

		stat, err := os.Stat(abs)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, nil, fmt.Errorf("file not found: %s", abs)
			}
			return nil, nil, fmt.Errorf("cannot stat path %s: %w", abs, err)
		}
		if stat.IsDir() {
			pat := filepath.Join(abs, markdownGlobFor(recursive))
			if watchMode {
				specs = append(specs, patternSpec{Pattern: pat, Rules: filter.Rules()})
				continue
			}
			matches, excluded, err := expandGlobPattern(pat, filter)
			if err != nil {
				return nil, nil, err
			}
			if len(matches) == 0 {
				if excluded > 0 {
					return nil, nil, fmt.Errorf("no .md files in %s (%d excluded by --exclude/%s)", abs, excluded, ignoreFile)
				}
				return nil, nil, fmt.Errorf("no .md files in %s", abs)
			}
			files = append(files, matches...)
			continue
		}
		// Explicitly named files are never filtered.
		files = append(files, abs)
	}
	return files, specs, nil
}

func markdownGlobFor(recursive bool) string {
	if recursive {
		return markdownGlobRecursive
	}
	return markdownGlob
}

// expandGlobPattern expands one pattern and filters the matches through the
// discovery filter. It returns how many matches were excluded so callers can
// distinguish "nothing matched" from "everything was excluded".
func expandGlobPattern(absPattern string, filter *ignore.Filter) (matches []string, excluded int, err error) {
	base, rel := doublestar.SplitPattern(filepath.ToSlash(absPattern))
	rels, err := doublestar.Glob(os.DirFS(base), rel, doublestar.WithFilesOnly())
	if err != nil {
		return nil, 0, fmt.Errorf("failed to expand glob %s: %w", absPattern, err)
	}
	pf := filter.ForPattern(filepath.ToSlash(absPattern), filepath.ToSlash(base))
	for _, r := range rels {
		abs := filepath.Join(base, r)
		if !pf.AdmitsFile(abs) {
			excluded++
			continue
		}
		matches = append(matches, abs)
	}
	collate.New(language.Und, collate.Numeric).SortStrings(matches)
	return matches, excluded, nil
}

func postFiles(client *http.Client, addr, group string, files []string) []deeplinkEntry {
	var entries []deeplinkEntry
	for _, f := range files {
		body, err := json.Marshal(map[string]string{
			"path": f,
		})
		if err != nil {
			slog.Warn("failed to marshal request", "path", f, "error", err)
			continue
		}
		resp, err := client.Post(
			fmt.Sprintf("http://%s/_/api/groups/%s/files", addr, url.PathEscape(group)),
			"application/json",
			bytes.NewReader(body),
		)
		if err != nil {
			slog.Warn("failed to post file", "path", f, "error", err)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			slog.Warn("failed to add file", "path", f, "status", resp.StatusCode)
			resp.Body.Close()
			continue
		}
		var entry server.FileEntry
		if err := json.NewDecoder(resp.Body).Decode(&entry); err != nil {
			slog.Warn("failed to decode file response", "error", err)
			resp.Body.Close()
			continue
		}
		resp.Body.Close()
		entries = append(entries, deeplinkEntry{
			URL:  buildDeeplink(addr, group, entry.ID),
			Path: entry.Path,
		})
	}
	return entries
}

// postPatterns registers each pattern with the running server, together with
// its discovery rules. It returns the deeplink entries for every file matched
// by the successful registrations, plus the number of patterns that were
// actually registered (which is not derivable from len(entries) because a
// valid pattern may legitimately match zero files).
func postPatterns(client *http.Client, addr, group string, specs []patternSpec) ([]deeplinkEntry, int) {
	var entries []deeplinkEntry
	added := 0
	for _, spec := range specs {
		body, err := json.Marshal(map[string]any{
			"pattern": spec.Pattern,
			"group":   group,
			"filter":  spec.Rules,
		})
		if err != nil {
			slog.Warn("failed to marshal request", "pattern", spec.Pattern, "error", err)
			continue
		}
		resp, err := client.Post(
			fmt.Sprintf("http://%s/_/api/patterns", addr),
			"application/json",
			bytes.NewReader(body),
		)
		if err != nil {
			slog.Warn("failed to post pattern", "pattern", spec.Pattern, "error", err)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			slog.Warn("failed to add pattern", "pattern", spec.Pattern, "status", resp.StatusCode)
			resp.Body.Close()
			continue
		}
		var patResp server.AddPatternResponse
		if err := json.NewDecoder(resp.Body).Decode(&patResp); err != nil {
			slog.Warn("failed to decode pattern response", "error", err)
			resp.Body.Close()
			continue
		}
		resp.Body.Close()
		added++
		if len(patResp.Files) == 0 && patResp.Excluded > 0 {
			fmt.Fprintf(os.Stderr, "ml: %s matched no files (%d excluded by --exclude/%s)\n", spec.Pattern, patResp.Excluded, ignoreFile)
		}
		for _, f := range patResp.Files {
			entries = append(entries, deeplinkEntry{
				URL:  buildDeeplink(addr, group, f.ID),
				Path: f.Path,
			})
		}
	}
	return entries, added
}

type deeplinkEntry struct {
	URL  string
	Path string // absolute file path (empty for uploaded files)
	Name string // display name fallback when Path is empty
}

// JSON output types

type jsonFileEntry struct {
	URL  string `json:"url"`
	Name string `json:"name"`
	Path string `json:"path"`
}

type jsonServeOutput struct {
	URL   string          `json:"url"`
	Files []jsonFileEntry `json:"files"`
}

type jsonStatusGroupEntry struct {
	Name           string                     `json:"name"`
	Files          int                        `json:"files"`
	Patterns       []string                   `json:"patterns,omitempty"`
	PatternFilters []server.PatternFilterData `json:"patternFilters,omitempty"`
}

type jsonStatusEntry struct {
	URL      string                 `json:"url"`
	Status   string                 `json:"status"`
	PID      int                    `json:"pid,omitempty"`
	Version  string                 `json:"version,omitempty"`
	Revision string                 `json:"revision,omitempty"`
	Groups   []jsonStatusGroupEntry `json:"groups,omitempty"`
}

func writeJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		slog.Warn("failed to write JSON output", "error", err)
	}
}

func deeplinksToJSON(entries []deeplinkEntry) []jsonFileEntry {
	if len(entries) == 0 {
		return []jsonFileEntry{}
	}
	names := deeplinkDisplayNames(entries)
	result := make([]jsonFileEntry, len(entries))
	for i, e := range entries {
		result[i] = jsonFileEntry{URL: e.URL, Name: names[i], Path: e.Path}
	}
	return result
}

func buildDeeplink(addr, groupName, fileID string) string {
	if groupName == server.DefaultGroup {
		return fmt.Sprintf("http://%s/?file=%s", addr, fileID)
	}
	return fmt.Sprintf("http://%s/%s?file=%s", addr, groupName, fileID)
}

// displayNames computes short display names for file paths, adding parent
// directory components as needed to distinguish files with the same base name.
func displayNames(paths []string) []string {
	names := make([]string, len(paths))
	// Track remaining parent path for each entry
	dirs := make([]string, len(paths))
	for i, p := range paths {
		names[i] = filepath.Base(p)
		dirs[i] = filepath.Dir(p)
	}

	for {
		dupes := make(map[string][]int)
		for i, n := range names {
			dupes[n] = append(dupes[n], i)
		}
		changed := false
		for _, indices := range dupes {
			if len(indices) <= 1 {
				continue
			}
			for _, idx := range indices {
				// Stop expanding when we've reached the filesystem root
				if dirs[idx] == filepath.Dir(dirs[idx]) {
					continue
				}
				parent := filepath.Base(dirs[idx])
				names[idx] = filepath.Join(parent, names[idx])
				dirs[idx] = filepath.Dir(dirs[idx])
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return names
}

// deeplinkDisplayNames computes display names for deeplink entries,
// using Name as fallback when Path is empty (uploaded files).
func deeplinkDisplayNames(entries []deeplinkEntry) []string {
	var pathEntries []string
	for _, e := range entries {
		if e.Path != "" {
			pathEntries = append(pathEntries, e.Path)
		} else {
			pathEntries = append(pathEntries, e.Name)
		}
	}
	return displayNames(pathEntries)
}

func printDeeplinks(entries []deeplinkEntry) {
	if len(entries) == 0 {
		return
	}
	names := deeplinkDisplayNames(entries)
	for i, e := range entries {
		fmt.Printf("  %s  %s\n", e.URL, names[i])
	}
}

// emitServeOutput writes the serve result (server URL + deeplinks) to stdout.
// In JSON mode it emits a single JSON object; in text mode it prints the URL and deeplinks.
func emitServeOutput(addr string, deeplinks []deeplinkEntry, printURL bool) {
	if jsonOutput {
		writeJSON(jsonServeOutput{
			URL:   fmt.Sprintf("http://%s", addr),
			Files: deeplinksToJSON(deeplinks),
		})
	} else {
		if printURL {
			fmt.Fprintf(os.Stdout, "http://%s\n", addr)
		}
		printDeeplinks(deeplinks)
	}
}

type probeResult struct {
	client  *http.Client
	groups  []string
	app     string
	version string
}

// errForeignServer marks a probe that reached a healthy server which
// identified itself as another application. Callers surface it instead of
// falling through to starting (or acting on) the wrong server.
var errForeignServer = errors.New("foreign server on port")

// probeServer checks that a ml server is running on addr by calling
// GET /_/api/status and validating the response contains a version field.
// Servers reporting an app identity other than ml are rejected; servers
// without one (ml ≤ 0.1.0, or a compatible program) are accepted as legacy.
func probeServer(addr string, timeout ...time.Duration) (*probeResult, error) {
	t := probeTimeoutDefault
	if len(timeout) > 0 {
		t = timeout[0]
	}
	client := &http.Client{Timeout: t}
	resp, err := client.Get(fmt.Sprintf("http://%s/_/api/status", addr))
	if err != nil {
		return nil, fmt.Errorf("no ml server found on %s", addr)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server on %s returned %s", addr, resp.Status)
	}

	var status struct {
		Version string `json:"version"`
		App     string `json:"app"`
		PID     int    `json:"pid"`
		Groups  []struct {
			Name string `json:"name"`
		} `json:"groups"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil || status.Version == "" {
		return nil, fmt.Errorf("server on %s is not a ml instance", addr)
	}
	if status.App != "" && status.App != server.AppName {
		return nil, fmt.Errorf("%w %s: server is a %s instance (version %s), not ml; use --port to target a different port", errForeignServer, addr, status.App, status.Version)
	}

	groups := make([]string, len(status.Groups))
	for i, g := range status.Groups {
		groups[i] = g.Name
	}
	return &probeResult{client: client, groups: groups, app: status.App, version: status.Version}, nil
}

// noteLegacyServer prints a one-time warning when the server we are about to
// attach to did not report an app identity. ml ≥ 0.2.0 always sends it, so
// the server is either an older ml or a program with a compatible status API.
// A version at or above 1.0.0 cannot be an ml release (ml starts at 0.1.0)
// and is called out as likely upstream mo.
func noteLegacyServer(addr string, res *probeResult) {
	if res.app != "" {
		return
	}
	if major, ok := majorVersion(res.version); ok && major >= 1 {
		fmt.Fprintf(os.Stderr, "ml: warning: server on http://%s did not identify itself; version %s is not an ml release and may be an upstream mo instance\n", addr, res.version)
		return
	}
	fmt.Fprintf(os.Stderr, "ml: note: server on http://%s did not identify itself (ml ≤ 0.1.0 or a compatible program)\n", addr)
}

// majorVersion returns the leading major component of a semver-ish version
// string ("1.6.8" → 1, true).
func majorVersion(v string) (int, bool) {
	v = strings.TrimPrefix(v, "v")
	major, _, ok := strings.Cut(v, ".")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(major)
	if err != nil {
		return 0, false
	}
	return n, true
}

// waitForServerDownTimeout is the maximum time to wait for a server to stop.
// Overridable in tests.
var waitForServerDownTimeout = 5 * time.Second

// waitForServerDown polls until the server on addr stops responding.
func waitForServerDown(addr string) error {
	const (
		pollInterval = 100 * time.Millisecond
		probeTimeout = 500 * time.Millisecond
	)
	deadline := time.Now().Add(waitForServerDownTimeout)
	for time.Now().Before(deadline) {
		if _, err := probeServer(addr, probeTimeout); err != nil {
			return nil
		}
		time.Sleep(pollInterval)
	}
	return fmt.Errorf("server on %s did not shut down within %s", addr, waitForServerDownTimeout)
}

func doShutdown(addr string) error {
	result, err := probeServer(addr)
	if err != nil {
		return err
	}

	resp, err := result.client.Post(fmt.Sprintf("http://%s/_/api/shutdown", addr), "application/json", nil)
	if err != nil {
		return fmt.Errorf("failed to send shutdown request: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("unexpected response from server: %s", resp.Status)
	}

	slog.Info("shutdown request sent", "addr", addr)
	fmt.Fprintf(os.Stderr, "ml: shutdown request sent to http://%s\n", addr)
	return nil
}

func doRestart(addr string) error {
	result, err := probeServer(addr)
	if err != nil {
		return err
	}

	resp, err := result.client.Post(fmt.Sprintf("http://%s/_/api/restart", addr), "application/json", nil)
	if err != nil {
		return fmt.Errorf("failed to send restart request: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("unexpected response from server: %s", resp.Status)
	}

	slog.Info("restart request sent", "addr", addr)
	fmt.Fprintf(os.Stderr, "ml: restart request sent to http://%s\n", addr)
	return nil
}

func doUnwatch(addr string, patterns []string, groupName string) error {
	result, err := probeServer(addr)
	if err != nil {
		return err
	}

	for _, pat := range patterns {
		body, err := json.Marshal(map[string]string{
			"pattern": pat,
			"group":   groupName,
		})
		if err != nil {
			return fmt.Errorf("failed to marshal request: %w", err)
		}

		req, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("http://%s/_/api/patterns", addr), bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := result.client.Do(req) //nolint:gosec // URL is constructed from local addr, not user-supplied
		if err != nil {
			return fmt.Errorf("failed to send unwatch request: %w", err)
		}
		resp.Body.Close()

		if resp.StatusCode == http.StatusNotFound {
			return fmt.Errorf("watch pattern %q not found in group %q (use --status to see registered patterns)", pat, groupName)
		}
		if resp.StatusCode != http.StatusNoContent {
			return fmt.Errorf("unexpected response from server: %s", resp.Status)
		}

		slog.Info("pattern removed", "pattern", pat, "group", groupName)
		fmt.Fprintf(os.Stderr, "ml: unwatched %s\n", pat)
	}

	return nil
}

func doClose(addr string, paths []string, groupName string) ([]string, error) {
	result, err := probeServer(addr)
	if err != nil {
		return nil, err
	}

	resp, err := result.client.Get(fmt.Sprintf("http://%s/_/api/status", addr))
	if err != nil {
		return nil, fmt.Errorf("failed to get server status: %w", err)
	}
	defer resp.Body.Close()

	var status statusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("failed to decode status: %w", err)
	}

	pathToID := make(map[string]string)
	for _, g := range status.Groups {
		if g.Name == groupName {
			for _, f := range g.Files {
				pathToID[f.Path] = f.ID
			}
			break
		}
	}

	var closedPaths []string
	var joinedErr error
	for _, p := range paths {
		absPath, err := filepath.Abs(p)
		if err != nil {
			joinedErr = errors.Join(joinedErr, fmt.Errorf("cannot resolve path %s: %w", p, err))
			continue
		}

		id, ok := pathToID[absPath]
		if !ok {
			joinedErr = errors.Join(joinedErr, fmt.Errorf("file %q not found in group %q (use --status to see files)", absPath, groupName))
			continue
		}

		req, err := http.NewRequest(http.MethodDelete,
			fmt.Sprintf("http://%s/_/api/groups/%s/files/%s", addr, url.PathEscape(groupName), id), nil)
		if err != nil {
			joinedErr = errors.Join(joinedErr, fmt.Errorf("failed to create request for %q: %w", absPath, err))
			continue
		}

		closeResp, err := result.client.Do(req)
		if err != nil {
			joinedErr = errors.Join(joinedErr, fmt.Errorf("failed to close %q: %w", absPath, err))
			continue
		}
		closeResp.Body.Close()

		if closeResp.StatusCode == http.StatusNotFound {
			joinedErr = errors.Join(joinedErr, fmt.Errorf("file %q not found", absPath))
			continue
		}
		if closeResp.StatusCode != http.StatusNoContent {
			joinedErr = errors.Join(joinedErr, fmt.Errorf("unexpected response for %q: %s", absPath, closeResp.Status))
			continue
		}

		slog.Info("file closed", "path", absPath, "id", id, "group", groupName)
		closedPaths = append(closedPaths, absPath)
	}

	return closedPaths, joinedErr
}

// statusGroupEntry is one group entry in the /_/api/status response.
type statusGroupEntry struct {
	Name  string `json:"name"`
	Files []struct {
		Name string `json:"name"`
		ID   string `json:"id"`
		Path string `json:"path"`
	} `json:"files"`
	Patterns       []string                   `json:"patterns,omitempty"`
	PatternFilters []server.PatternFilterData `json:"patternFilters,omitempty"`
}

type statusResponse struct {
	Version  string             `json:"version"`
	Revision string             `json:"revision"`
	PID      int                `json:"pid"`
	Groups   []statusGroupEntry `json:"groups"`
}

func doStatus() error {
	ports := discoverPorts()
	if len(ports) == 0 {
		if jsonOutput {
			writeJSON([]jsonStatusEntry{})
		} else {
			fmt.Fprintln(os.Stderr, "ml: no ml server found")
		}
		return nil
	}

	client := &http.Client{Timeout: 2 * time.Second}
	found := false
	var jsonEntries []jsonStatusEntry

	for i, p := range ports {
		addr := fmt.Sprintf("localhost:%d", p)
		resp, err := client.Get(fmt.Sprintf("http://%s/_/api/status", addr))
		if err != nil {
			found = true
			if jsonOutput {
				jsonEntries = append(jsonEntries, jsonStatusEntry{
					URL:    fmt.Sprintf("http://%s", addr),
					Status: "stopped",
				})
			} else {
				fmt.Fprintf(os.Stdout, "http://%s (stopped)\n", addr)
				if i < len(ports)-1 {
					fmt.Fprintln(os.Stdout)
				}
			}
			continue
		}

		var status statusResponse
		if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
			resp.Body.Close()
			continue
		}
		resp.Body.Close()
		found = true

		if jsonOutput {
			entry := jsonStatusEntry{
				URL:      fmt.Sprintf("http://%s", addr),
				Status:   "running",
				PID:      status.PID,
				Version:  status.Version,
				Revision: status.Revision,
			}
			for _, g := range status.Groups {
				entry.Groups = append(entry.Groups, jsonStatusGroupEntry{
					Name:           g.Name,
					Files:          len(g.Files),
					Patterns:       g.Patterns,
					PatternFilters: g.PatternFilters,
				})
			}
			jsonEntries = append(jsonEntries, entry)
		} else {
			ver := status.Version
			if status.Revision != "" {
				ver += " " + status.Revision
			}
			fmt.Fprintf(os.Stdout, "http://%s (pid %d, %s)\n", addr, status.PID, ver)
			for _, g := range status.Groups {
				fmt.Fprintf(os.Stdout, "  %s: %d file(s)\n", g.Name, len(g.Files))
				if len(g.Patterns) > 0 {
					fmt.Fprintf(os.Stdout, "    watching: %s\n", strings.Join(g.Patterns, ", "))
				}
				for _, pf := range g.PatternFilters {
					if len(pf.Rules.Excludes) == 0 && !pf.Rules.IncludeHidden {
						continue
					}
					detail := strings.Join(pf.Rules.Excludes, ", ")
					if pf.Rules.IncludeHidden {
						if detail != "" {
							detail += ", "
						}
						detail += "(hidden files included)"
					}
					fmt.Fprintf(os.Stdout, "    excluding: %s\n", detail)
				}
			}
			if i < len(ports)-1 {
				fmt.Fprintln(os.Stdout)
			}
		}
	}

	if jsonOutput {
		if !found {
			jsonEntries = []jsonStatusEntry{}
		}
		writeJSON(jsonEntries)
	} else if !found {
		fmt.Fprintln(os.Stderr, "ml: no ml server found")
	}

	return nil
}

func discoverPorts() []int {
	dir, err := logfile.Dir()
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var ports []int
	for _, e := range entries {
		name := e.Name()
		// Match "ml-{port}.log"
		if !strings.HasPrefix(name, "ml-") || !strings.HasSuffix(name, ".log") {
			continue
		}
		// Exclude rotated backups like "ml-6275.log.1"
		raw := strings.TrimSuffix(strings.TrimPrefix(name, "ml-"), ".log")
		p, err := strconv.Atoi(raw)
		if err != nil {
			continue
		}
		ports = append(ports, p)
	}
	sort.Ints(ports)
	return ports
}

func startServer(ctx context.Context, addr string, filesByGroup map[string][]string, specsByGroup map[string][]patternSpec, uploadedFiles []server.UploadedFileData) error {
	sigCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	ctx, cancel := donegroup.WithCancel(sigCtx)
	cleanedUp := false
	cleanup := func() {
		if cleanedUp {
			return
		}
		cleanedUp = true
		cancel()
		if err := donegroup.WaitWithTimeout(ctx, 5*time.Second); err != nil {
			slog.Error("shutdown error", "error", err)
		}
	}
	defer cleanup()

	state := server.NewState(ctx)

	state.EnableBackup(ctx, func(data server.RestoreData) {
		if err := backup.Save(port, data); err != nil {
			slog.Warn("failed to save backup", "error", err)
		}
	})

	var deeplinks []deeplinkEntry
	var totalFiles, skippedFiles int
	for group, files := range filesByGroup {
		for _, f := range files {
			totalFiles++
			entry, err := state.AddFile(f, group)
			if err != nil {
				skippedFiles++
				slog.Warn("skipping file", "path", f, "error", err)
				continue
			}
			deeplinks = append(deeplinks, deeplinkEntry{
				URL:  buildDeeplink(addr, group, entry.ID),
				Path: entry.Path,
			})
		}
	}
	var patternsAdded int
	for group, specs := range specsByGroup {
		for _, spec := range specs {
			entries, _, err := state.AddPatternWithRules(spec.Pattern, group, spec.Rules)
			if err != nil {
				slog.Warn("failed to add pattern", "pattern", spec.Pattern, "error", err)
				continue
			}
			patternsAdded++
			for _, entry := range entries {
				deeplinks = append(deeplinks, deeplinkEntry{
					URL:  buildDeeplink(addr, group, entry.ID),
					Path: entry.Path,
				})
			}
		}
	}

	for _, uf := range uploadedFiles {
		state.AddUploadedFile(uf.Name, uf.Content, uf.Group)
	}

	if totalFiles > 0 && skippedFiles == totalFiles && patternsAdded == 0 && len(uploadedFiles) == 0 {
		return fmt.Errorf("all %d file(s) were skipped", totalFiles)
	}

	handler := server.NewHandler(state)

	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		// The CloseAllSubscribers cleanup below is not yet registered; close the
		// watcher here so watchLoop exits and donegroup does not hit its 5s timeout.
		state.CloseAllSubscribers()
		return fmt.Errorf("cannot listen on %s: %w", addr, err)
	}

	emitServeOutput(addr, deeplinks, true)

	if err := donegroup.Cleanup(ctx, func() error {
		state.CloseAllSubscribers()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		return srv.Shutdown(shutdownCtx)
	}); err != nil {
		return fmt.Errorf("failed to register cleanup: %w", err)
	}

	go func() {
		slog.Info("serving", "url", fmt.Sprintf("http://%s", addr))
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
		}
	}()

	openBrowser(addr)

	select {
	case <-ctx.Done():
		slog.Info("shutting down")
	case restoreFile := <-state.RestartCh():
		slog.Info("restarting")
		// Cleanup releases the port (CloseAllSubscribers + srv.Shutdown)
		// before we spawn the new process.
		cleanup()
		_, err := spawnNewProcess(addr, restoreFile)
		return err
	case <-state.ShutdownCh():
		slog.Info("shutting down (requested via API)")
	}

	return nil
}

func spawnNewProcess(addr string, restoreFile string) (*os.Process, error) {
	binPath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("cannot find binary: %w", err)
	}

	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("cannot parse addr: %w", err)
	}

	args := []string{"--port", p, "--bind", h, "--no-open", "--foreground"}
	if restoreFile != "" {
		args = append(args, "--restore", restoreFile)
	}
	if dangerouslyAllowRemoteAccess {
		args = append(args, "--dangerously-allow-remote-access")
	}
	cmd := exec.Command(binPath, args...) //nolint:gosec
	setSysProcAttr(cmd)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start new process: %w", err)
	}

	slog.Info("new process started", "pid", cmd.Process.Pid) //nolint:gosec // PID is from our own child process
	return cmd.Process, nil
}

func startBackground(addr string, filesByGroup map[string][]string, specsByGroup map[string][]patternSpec, uploadedFiles []server.UploadedFileData) error {
	patternsByGroup := make(map[string][]string, len(specsByGroup))
	var patternFilters []server.PatternFilterData
	for group, specs := range specsByGroup {
		for _, spec := range specs {
			patternsByGroup[group] = append(patternsByGroup[group], spec.Pattern)
			if !spec.Rules.Empty() {
				patternFilters = append(patternFilters, server.PatternFilterData{
					Pattern: spec.Pattern,
					Group:   group,
					Rules:   spec.Rules,
				})
			}
		}
	}
	restoreFile, err := server.WriteRestoreFile(server.RestoreData{
		Groups:         filesByGroup,
		Patterns:       patternsByGroup,
		PatternFilters: patternFilters,
		UploadedFiles:  uploadedFiles,
	})
	if err != nil {
		return err
	}

	proc, err := spawnNewProcess(addr, restoreFile)
	if err != nil {
		os.Remove(restoreFile)
		return err
	}
	pid := proc.Pid
	// Detach so the child survives parent exit.
	if err := proc.Release(); err != nil {
		slog.Warn("failed to release process", "error", err)
	}

	status, err := waitForReady(addr, pid, 10*time.Second)
	if err != nil {
		// Remove the restore file only once the child is confirmed dead: a
		// slow-but-alive child may not have consumed it yet, and an alive
		// child removes it itself after loading.
		if !processAlive(pid) {
			os.Remove(restoreFile) //nolint:errcheck // best-effort; the child may have consumed it already
		}
		if errors.Is(err, errServerConflict) {
			// Lost a concurrent startup race: another ml server owns the
			// port. Add our files to the winner instead of reporting a
			// false success.
			return addToRunningServer(addr, status, filesByGroup, specsByGroup, uploadedFiles)
		}
		return fmt.Errorf("%w (spawned pid %d)", err, pid)
	}

	var deeplinks []deeplinkEntry
	if status != nil {
		for _, g := range status.Groups {
			for _, f := range g.Files {
				deeplinks = append(deeplinks, deeplinkEntry{
					URL:  buildDeeplink(addr, g.Name, f.ID),
					Path: f.Path,
					Name: f.Name,
				})
			}
		}
	}
	emitServeOutput(addr, deeplinks, true)
	fmt.Fprintf(os.Stderr, "ml: serving at http://%s (pid %d)\n", addr, pid)

	openBrowser(addr)

	return nil
}

// addToRunningServer posts files, patterns, and uploaded files to a ml server
// that is already running on addr. Used when a background start loses the
// port to another ml instance (concurrent startup race).
func addToRunningServer(addr string, status *statusResponse, filesByGroup map[string][]string, specsByGroup map[string][]patternSpec, uploadedFiles []server.UploadedFileData) error {
	slog.Info("port already served by another ml instance; adding to it", "addr", addr, "pid", status.PID)
	client := &http.Client{Timeout: probeTimeoutDefault}
	var deeplinks []deeplinkEntry
	added := 0
	attempted := 0
	for group, files := range filesByGroup {
		attempted += len(files)
		entries := postFiles(client, addr, group, files)
		deeplinks = append(deeplinks, entries...)
		added += len(entries)
	}
	for group, specs := range specsByGroup {
		attempted += len(specs)
		entries, patternsAdded := postPatterns(client, addr, group, specs)
		deeplinks = append(deeplinks, entries...)
		added += patternsAdded
	}
	for _, uf := range uploadedFiles {
		attempted++
		entry, err := postUploadedFile(client, addr, uf.Group, uf.Name, uf.Content)
		if err != nil {
			slog.Warn("failed to upload file", "name", uf.Name, "error", err)
			continue
		}
		deeplinks = append(deeplinks, entry)
		added++
	}
	if attempted > 0 && added == 0 {
		return fmt.Errorf("failed to add any items to the ml server at http://%s (check log file for details)", addr)
	}
	emitServeOutput(addr, deeplinks, true)
	fmt.Fprintf(os.Stderr, "ml: another ml server is already running at http://%s (pid %d); added %d item(s) to it\n", addr, status.PID, added)

	isNewGroup := true
	for _, g := range status.Groups {
		if g.Name == target {
			isNewGroup = false
			break
		}
	}
	if isNewGroup || open {
		openBrowser(addr)
	}
	return nil
}

func openBrowser(addr string) {
	if noOpen {
		return
	}
	url := fmt.Sprintf("http://%s", addr)
	if target != server.DefaultGroup {
		url = fmt.Sprintf("%s/%s", url, target)
	}
	if err := browser.OpenURL(url); err != nil {
		slog.Warn("could not open browser", "error", err)
	}
}

// waitForReady polls addr until a ml server responds as ready, the spawned
// child (childPID) dies, or timeout elapses.
//
// If a ml server responds but its PID does not match childPID, the spawned
// child lost a concurrent startup race for the port; errServerConflict is
// returned along with the winning server's status.
func waitForReady(addr string, childPID int, timeout time.Duration) (*statusResponse, error) {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	deadline := time.Now().Add(timeout)

	var childDeadSince time.Time
	for time.Now().Before(deadline) {
		resp, err := client.Get(fmt.Sprintf("http://%s/_/api/status", addr))
		if err == nil {
			if resp.StatusCode == http.StatusOK {
				var status statusResponse
				if decErr := json.NewDecoder(resp.Body).Decode(&status); decErr == nil && status.Version != "" {
					resp.Body.Close()
					if childPID > 0 && status.PID != childPID {
						return &status, errServerConflict
					}
					return &status, nil
				}
			}
			resp.Body.Close()
		}

		// Not ready yet: if the spawned child has died, keep polling briefly —
		// in a lost startup race the winner may not be serving yet — then fail
		// fast instead of waiting out the full timeout.
		if childPID > 0 && childDeadSince.IsZero() && !processAlive(childPID) {
			childDeadSince = time.Now()
		}
		if !childDeadSince.IsZero() && time.Since(childDeadSince) >= deadChildGrace {
			return nil, errors.New("server process exited unexpectedly; the port may be in use by another server (check log file for details)")
		}

		time.Sleep(50 * time.Millisecond)
	}

	return nil, fmt.Errorf("server did not become ready within %s; the port may be in use by another (non-ml) server (check log file for details)", timeout)
}

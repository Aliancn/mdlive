package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Aliancn/mdlive/internal/ignore"
	"github.com/Aliancn/mdlive/internal/server"
)

func TestRun_UnwatchWithWatch(t *testing.T) {
	unwatchMode = true
	watchMode = true
	defer func() {
		unwatchMode = false
		watchMode = false
	}()

	err := run(rootCmd, []string{"**/*.md"})
	if err == nil {
		t.Fatal("run should return error when --unwatch and --watch are both specified")
	}
	want := "cannot use --unwatch with --watch"
	if err.Error() != want {
		t.Fatalf("got error %q, want %q", err.Error(), want)
	}
}

func TestRun_UnwatchWithoutArgs(t *testing.T) {
	unwatchMode = true
	defer func() { unwatchMode = false }()

	err := run(rootCmd, nil)
	if err == nil {
		t.Fatal("run should return error when --unwatch has no arguments")
	}
	want := "--unwatch requires a glob pattern or directory argument"
	if err.Error() != want {
		t.Fatalf("got error %q, want %q", err.Error(), want)
	}
}

func TestRun_UnwatchWithFileArgs(t *testing.T) {
	f := filepath.Join(t.TempDir(), "test.md")
	writeTestFile(t, f, []byte("# Test"))

	unwatchMode = true
	defer func() { unwatchMode = false }()

	err := run(rootCmd, []string{f})
	if err == nil {
		t.Fatal("run should return error when --unwatch is given file arguments")
	}
	if !strings.Contains(err.Error(), "not individual files") {
		t.Fatalf("got error %q, want hint about individual files", err.Error())
	}
}

func TestRun_Close(t *testing.T) {
	t.Run("without args returns error", func(t *testing.T) {
		closeFiles = true
		defer func() { closeFiles = false }()

		err := run(rootCmd, nil)
		if err == nil {
			t.Fatal("run should return error when --close is specified without file arguments")
		}
		want := "--close requires at least one file argument"
		if err.Error() != want {
			t.Fatalf("got error %q, want %q", err.Error(), want)
		}
	})

	t.Run("with watch returns error", func(t *testing.T) {
		closeFiles = true
		watchMode = true
		defer func() {
			closeFiles = false
			watchMode = false
		}()

		err := run(rootCmd, []string{"README.md"})
		if err == nil {
			t.Fatal("run should return error when --close and --watch are both specified")
		}
		want := "cannot use --close with --watch"
		if err.Error() != want {
			t.Fatalf("got error %q, want %q", err.Error(), want)
		}
	})
}

func TestRun_Watch(t *testing.T) {
	t.Run("no args errors", func(t *testing.T) {
		watchMode = true
		defer func() { watchMode = false }()

		err := run(rootCmd, nil)
		if err == nil {
			t.Fatal("run should return error when --watch has no pattern or directory argument")
		}
		if !strings.Contains(err.Error(), "requires a glob pattern or directory argument") {
			t.Fatalf("got error %q, want 'requires a glob pattern or directory argument'", err.Error())
		}
	})

	t.Run("only file args hints shell expansion", func(t *testing.T) {
		f1 := filepath.Join(t.TempDir(), "a.md")
		writeTestFile(t, f1, []byte("# A"))
		f2 := filepath.Join(t.TempDir(), "b.md")
		writeTestFile(t, f2, []byte("# B"))

		watchMode = true
		defer func() { watchMode = false }()

		err := run(rootCmd, []string{f1, f2})
		if err == nil {
			t.Fatal("run should return error when --watch is given only regular file arguments")
		}
		if !strings.Contains(err.Error(), "shell may have expanded") {
			t.Fatalf("error should hint shell expansion, got %q", err.Error())
		}
	})

	t.Run("non-existent arg returns file not found", func(t *testing.T) {
		watchMode = true
		defer func() { watchMode = false }()

		err := run(rootCmd, []string{"nonexistent.md"})
		if err == nil {
			t.Fatal("run should return error for non-existent file")
		}
		if !strings.Contains(err.Error(), "file not found") {
			t.Fatalf("got error %q, want file not found error", err.Error())
		}
	})
}

func TestRun_RecursiveRequiresArgs(t *testing.T) {
	recursive = true
	defer func() { recursive = false }()

	err := run(rootCmd, nil)
	if err == nil {
		t.Fatal("run should return error when --recursive is used without any argument")
	}
	want := "--recursive (-R) requires a directory argument"
	if err.Error() != want {
		t.Fatalf("got error %q, want %q", err.Error(), want)
	}
}

func TestResolveUnwatchArgs_GlobPattern(t *testing.T) {
	patterns, err := resolveUnwatchArgs([]string{"**/*.md", "docs/*.md"}, false, "", "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(patterns) != 2 {
		t.Fatalf("got %d patterns, want 2", len(patterns))
	}
	for _, p := range patterns {
		if !filepath.IsAbs(p) {
			t.Errorf("pattern %q is not absolute", p)
		}
	}
}

func TestResolveUnwatchArgs_Directory(t *testing.T) {
	dir := t.TempDir()

	patterns, err := resolveUnwatchArgs([]string{dir}, false, "", "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(patterns) != 1 {
		t.Fatalf("got %d patterns, want 1", len(patterns))
	}
	want := filepath.Join(dir, "*.md")
	if patterns[0] != want {
		t.Errorf("got pattern %q, want %q", patterns[0], want)
	}
}

func TestResolveUnwatchArgs_FileReturnsError(t *testing.T) {
	f := filepath.Join(t.TempDir(), "test.md")
	writeTestFile(t, f, []byte("# Test"))

	_, err := resolveUnwatchArgs([]string{f}, false, "", "default")
	if err == nil {
		t.Fatal("expected error for file argument")
	}
	if !strings.Contains(err.Error(), "not individual files") {
		t.Fatalf("got error %q, want hint about individual files", err.Error())
	}
}

func TestResolveUnwatchArgs_RecursiveDirectory(t *testing.T) {
	dir := t.TempDir()

	// Set up a mock server that returns patterns for the group.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := statusResponse{
			Groups: []statusGroupEntry{
				{
					Name: "default",
					Patterns: []string{
						filepath.Join(dir, "*.md"),
						filepath.Join(dir, "sub", "*.md"),
						filepath.Join(dir, "**/*.md"),
						"/other/path/*.md",
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	})
	ts := httptest.NewServer(handler)
	defer ts.Close()
	addr := strings.TrimPrefix(ts.URL, "http://")

	patterns, err := resolveUnwatchArgs([]string{dir}, true, addr, "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(patterns) != 3 {
		t.Fatalf("got %d patterns, want 3: %v", len(patterns), patterns)
	}
	// Should NOT include /other/path/*.md
	for _, p := range patterns {
		if !strings.HasPrefix(p, dir) {
			t.Errorf("unexpected pattern %q not under %s", p, dir)
		}
	}
}

func TestResolveUnwatchArgs_RecursiveNoMatch(t *testing.T) {
	dir := t.TempDir()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := statusResponse{
			Groups: []statusGroupEntry{
				{
					Name:     "default",
					Patterns: []string{"/other/path/*.md"},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	})
	ts := httptest.NewServer(handler)
	defer ts.Close()
	addr := strings.TrimPrefix(ts.URL, "http://")

	_, err := resolveUnwatchArgs([]string{dir}, true, addr, "default")
	if err == nil {
		t.Fatal("expected error when no patterns match under directory")
	}
	if !strings.Contains(err.Error(), "no watched patterns found under") {
		t.Fatalf("got error %q, want 'no watched patterns found under'", err.Error())
	}
}

func TestResolveUnwatchArgs_RecursiveDeletedDirectory(t *testing.T) {
	// Use a path that does not exist on disk.
	deletedDir := filepath.Join(t.TempDir(), "deleted")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := statusResponse{
			Groups: []statusGroupEntry{
				{
					Name: "default",
					Patterns: []string{
						filepath.Join(deletedDir, "*.md"),
						filepath.Join(deletedDir, "**/*.md"),
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	})
	ts := httptest.NewServer(handler)
	defer ts.Close()
	addr := strings.TrimPrefix(ts.URL, "http://")

	patterns, err := resolveUnwatchArgs([]string{deletedDir}, true, addr, "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(patterns) != 2 {
		t.Fatalf("got %d patterns, want 2: %v", len(patterns), patterns)
	}
}

func TestResolveUnwatchArgs_NonRecursiveDeletedDirectory(t *testing.T) {
	deletedDir := filepath.Join(t.TempDir(), "deleted")

	_, err := resolveUnwatchArgs([]string{deletedDir}, false, "", "default")
	if err == nil {
		t.Fatal("expected error for non-existent directory without -R")
	}
	if !strings.Contains(err.Error(), "path not found") {
		t.Fatalf("got error %q, want 'path not found'", err.Error())
	}
}

func TestResolveUnwatchArgs_RecursiveGroupNotFound(t *testing.T) {
	dir := t.TempDir()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := statusResponse{
			Groups: []statusGroupEntry{
				{
					Name:     "other",
					Patterns: []string{"/other/*.md"},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	})
	ts := httptest.NewServer(handler)
	defer ts.Close()
	addr := strings.TrimPrefix(ts.URL, "http://")

	_, err := resolveUnwatchArgs([]string{dir}, true, addr, "default")
	if err == nil {
		t.Fatal("expected error when group does not exist")
	}
	if !strings.Contains(err.Error(), "group") || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("got error %q, want group not found error", err.Error())
	}
}

func TestMergeGroups(t *testing.T) {
	t.Run("restored files come first, CLI files appended after", func(t *testing.T) {
		base := map[string][]string{"default": {"/a.md", "/b.md"}}
		additional := map[string][]string{"default": {"/c.md"}}
		got := mergeGroups(base, additional)
		want := []string{"/a.md", "/b.md", "/c.md"}
		if len(got["default"]) != len(want) {
			t.Fatalf("got %v, want %v", got["default"], want)
		}
		for i, v := range want {
			if got["default"][i] != v {
				t.Fatalf("got[%d] = %s, want %s", i, got["default"][i], v)
			}
		}
	})

	t.Run("deduplicates files in the same group", func(t *testing.T) {
		base := map[string][]string{"default": {"/a.md", "/b.md"}}
		additional := map[string][]string{"default": {"/b.md", "/c.md"}}
		got := mergeGroups(base, additional)
		want := []string{"/a.md", "/b.md", "/c.md"}
		if len(got["default"]) != len(want) {
			t.Fatalf("got %v, want %v", got["default"], want)
		}
		for i, v := range want {
			if got["default"][i] != v {
				t.Fatalf("got[%d] = %s, want %s", i, got["default"][i], v)
			}
		}
	})

	t.Run("merges different groups", func(t *testing.T) {
		base := map[string][]string{"docs": {"/doc.md"}}
		additional := map[string][]string{"default": {"/a.md"}}
		got := mergeGroups(base, additional)
		if len(got["docs"]) != 1 || got["docs"][0] != "/doc.md" {
			t.Fatalf("got docs=%v, want [/doc.md]", got["docs"])
		}
		if len(got["default"]) != 1 || got["default"][0] != "/a.md" {
			t.Fatalf("got default=%v, want [/a.md]", got["default"])
		}
	})

	t.Run("nil base returns additional only", func(t *testing.T) {
		additional := map[string][]string{"default": {"/a.md"}}
		got := mergeGroups(nil, additional)
		if len(got["default"]) != 1 || got["default"][0] != "/a.md" {
			t.Fatalf("got %v, want [/a.md]", got["default"])
		}
	})

	t.Run("nil additional returns base only", func(t *testing.T) {
		base := map[string][]string{"default": {"/a.md"}}
		got := mergeGroups(base, nil)
		if len(got["default"]) != 1 || got["default"][0] != "/a.md" {
			t.Fatalf("got %v, want [/a.md]", got["default"])
		}
	})

	t.Run("both nil returns nil", func(t *testing.T) {
		got := mergeGroups(nil, nil)
		if got != nil {
			t.Fatalf("got %v, want nil", got)
		}
	})
}

func TestBuildDeeplink(t *testing.T) {
	tests := []struct {
		addr      string
		groupName string
		fileID    string
		want      string
	}{
		{"localhost:6275", server.DefaultGroup, "abc12345", "http://localhost:6275/?file=abc12345"},
		{"localhost:6275", "design", "def67890", "http://localhost:6275/design?file=def67890"},
	}
	for _, tt := range tests {
		got := buildDeeplink(tt.addr, tt.groupName, tt.fileID)
		if got != tt.want {
			t.Errorf("buildDeeplink(%q, %q, %q) = %q, want %q", tt.addr, tt.groupName, tt.fileID, got, tt.want)
		}
	}
}

func TestDisplayNames(t *testing.T) {
	t.Run("unique basenames stay short", func(t *testing.T) {
		paths := []string{"/a/README.md", "/b/CHANGELOG.md"}
		got := displayNames(paths)
		if got[0] != "README.md" || got[1] != "CHANGELOG.md" {
			t.Fatalf("got %v, want [README.md CHANGELOG.md]", got)
		}
	})

	t.Run("duplicate basenames get parent dir", func(t *testing.T) {
		paths := []string{"/project/docs/README.md", "/project/api/README.md"}
		got := displayNames(paths)
		want0 := filepath.Join("docs", "README.md")
		want1 := filepath.Join("api", "README.md")
		if got[0] != want0 || got[1] != want1 {
			t.Fatalf("got %v, want [%s %s]", got, want0, want1)
		}
	})

	t.Run("deeply nested duplicates get enough context", func(t *testing.T) {
		paths := []string{"/a/x/README.md", "/b/x/README.md"}
		got := displayNames(paths)
		want0 := filepath.Join("a", "x", "README.md")
		want1 := filepath.Join("b", "x", "README.md")
		if got[0] != want0 || got[1] != want1 {
			t.Fatalf("got %v, want [%s %s]", got, want0, want1)
		}
	})

	t.Run("identical paths do not loop forever", func(t *testing.T) {
		paths := []string{"/a/b/README.md", "/a/b/README.md"}
		got := displayNames(paths)
		if len(got) != 2 {
			t.Fatalf("got %d names, want 2", len(got))
		}
	})

	t.Run("single entry stays short", func(t *testing.T) {
		paths := []string{"/a/b/c/README.md"}
		got := displayNames(paths)
		if got[0] != "README.md" {
			t.Fatalf("got %v, want [README.md]", got)
		}
	})
}

func TestFilterValidRestoreData(t *testing.T) {
	t.Run("keeps only existing files", func(t *testing.T) {
		dir := t.TempDir()
		existing := filepath.Join(dir, "a.md")
		os.WriteFile(existing, []byte("# A"), 0o600) //nolint:errcheck
		missing := filepath.Join(dir, "missing.md")

		rd := &server.RestoreData{
			Groups: map[string][]string{
				"default": {existing, missing},
			},
		}

		filesByGroup, _, _, _ := filterValidRestoreData(rd)
		if len(filesByGroup["default"]) != 1 {
			t.Fatalf("got %d files, want 1", len(filesByGroup["default"]))
		}
		if filesByGroup["default"][0] != existing {
			t.Fatalf("got %s, want %s", filesByGroup["default"][0], existing)
		}
	})

	t.Run("omits group when all files missing", func(t *testing.T) {
		rd := &server.RestoreData{
			Groups: map[string][]string{
				"docs": {"/nonexistent/a.md", "/nonexistent/b.md"},
			},
		}

		filesByGroup, _, _, _ := filterValidRestoreData(rd)
		if _, ok := filesByGroup["docs"]; ok {
			t.Fatal("group with all missing files should not appear in result")
		}
	})

	t.Run("passes patterns through unchanged", func(t *testing.T) {
		rd := &server.RestoreData{
			Groups: map[string][]string{},
			Patterns: map[string][]string{
				"default": {"/some/path/*.md"},
			},
		}

		_, patternsByGroup, _, _ := filterValidRestoreData(rd)
		if len(patternsByGroup["default"]) != 1 {
			t.Fatalf("got %d patterns, want 1", len(patternsByGroup["default"]))
		}
		if patternsByGroup["default"][0] != "/some/path/*.md" {
			t.Fatalf("got %s, want /some/path/*.md", patternsByGroup["default"][0])
		}
	})

	t.Run("empty restore data returns empty results", func(t *testing.T) {
		rd := &server.RestoreData{}

		filesByGroup, patternsByGroup, _, _ := filterValidRestoreData(rd)
		if len(filesByGroup) != 0 {
			t.Fatalf("got %d groups, want 0", len(filesByGroup))
		}
		if len(patternsByGroup) != 0 {
			t.Fatalf("got %d pattern groups, want 0", len(patternsByGroup))
		}
	})
}

func TestDeeplinksToJSON(t *testing.T) {
	t.Run("empty entries returns empty slice", func(t *testing.T) {
		got := deeplinksToJSON(nil)
		if len(got) != 0 {
			t.Fatalf("got %d entries, want 0", len(got))
		}
	})

	t.Run("file entries with paths", func(t *testing.T) {
		entries := []deeplinkEntry{
			{URL: "http://localhost:6275/?file=abc", Path: "/home/user/README.md"},
			{URL: "http://localhost:6275/?file=def", Path: "/home/user/CHANGELOG.md"},
		}
		got := deeplinksToJSON(entries)
		if len(got) != 2 {
			t.Fatalf("got %d entries, want 2", len(got))
		}
		if got[0].URL != entries[0].URL {
			t.Errorf("got URL %q, want %q", got[0].URL, entries[0].URL)
		}
		if got[0].Name != "README.md" {
			t.Errorf("got Name %q, want %q", got[0].Name, "README.md")
		}
		if got[0].Path != entries[0].Path {
			t.Errorf("got Path %q, want %q", got[0].Path, entries[0].Path)
		}
	})

	t.Run("uploaded files with empty path use Name for display", func(t *testing.T) {
		entries := []deeplinkEntry{
			{URL: "http://localhost:6275/?file=abc", Path: "", Name: "uploaded.md"},
		}
		got := deeplinksToJSON(entries)
		if got[0].Name != "uploaded.md" {
			t.Errorf("got Name %q, want %q", got[0].Name, "uploaded.md")
		}
		if got[0].Path != "" {
			t.Errorf("got Path %q, want empty string", got[0].Path)
		}
	})
}

func TestDeeplinkDisplayNames(t *testing.T) {
	t.Run("uses Path when available", func(t *testing.T) {
		entries := []deeplinkEntry{
			{Path: "/a/README.md"},
			{Path: "/b/CHANGELOG.md"},
		}
		got := deeplinkDisplayNames(entries)
		if got[0] != "README.md" || got[1] != "CHANGELOG.md" {
			t.Fatalf("got %v, want [README.md CHANGELOG.md]", got)
		}
	})

	t.Run("falls back to Name when Path is empty", func(t *testing.T) {
		entries := []deeplinkEntry{
			{Path: "/a/README.md"},
			{Path: "", Name: "uploaded.md"},
		}
		got := deeplinkDisplayNames(entries)
		if got[0] != "README.md" || got[1] != "uploaded.md" {
			t.Fatalf("got %v, want [README.md uploaded.md]", got)
		}
	})

	t.Run("disambiguates duplicate names across path and uploaded", func(t *testing.T) {
		entries := []deeplinkEntry{
			{Path: "/a/docs/README.md"},
			{Path: "", Name: "README.md"},
		}
		got := deeplinkDisplayNames(entries)
		if got[0] == got[1] {
			t.Fatalf("names should differ but both are %q", got[0])
		}
	})
}

func TestEmitServeOutput(t *testing.T) {
	entries := []deeplinkEntry{
		{URL: "http://localhost:6275/?file=abc", Path: "/home/user/README.md"},
	}

	t.Run("json mode outputs valid JSON", func(t *testing.T) {
		jsonOutput = true
		defer func() { jsonOutput = false }()

		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		oldStdout := os.Stdout
		os.Stdout = w

		emitServeOutput("localhost:6275", entries, true)

		w.Close()
		os.Stdout = oldStdout

		var buf bytes.Buffer
		buf.ReadFrom(r) //nolint:errcheck

		var output jsonServeOutput
		if err := json.Unmarshal(buf.Bytes(), &output); err != nil {
			t.Fatalf("invalid JSON: %v\noutput: %s", err, buf.String())
		}
		if output.URL != "http://localhost:6275" {
			t.Errorf("got URL %q, want %q", output.URL, "http://localhost:6275")
		}
		if len(output.Files) != 1 {
			t.Fatalf("got %d files, want 1", len(output.Files))
		}
		if output.Files[0].Name != "README.md" {
			t.Errorf("got file name %q, want %q", output.Files[0].Name, "README.md")
		}
	})

	t.Run("text mode with printURL prints URL line", func(t *testing.T) {
		jsonOutput = false

		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		oldStdout := os.Stdout
		os.Stdout = w

		emitServeOutput("localhost:6275", entries, true)

		w.Close()
		os.Stdout = oldStdout

		var buf bytes.Buffer
		buf.ReadFrom(r) //nolint:errcheck

		output := buf.String()
		if !strings.Contains(output, "http://localhost:6275\n") {
			t.Errorf("expected URL line in output, got %q", output)
		}
		if !strings.Contains(output, "README.md") {
			t.Errorf("expected deeplink in output, got %q", output)
		}
	})

	t.Run("text mode without printURL omits URL line", func(t *testing.T) {
		jsonOutput = false

		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		oldStdout := os.Stdout
		os.Stdout = w

		emitServeOutput("localhost:6275", entries, false)

		w.Close()
		os.Stdout = oldStdout

		var buf bytes.Buffer
		buf.ReadFrom(r) //nolint:errcheck

		output := buf.String()
		if strings.Contains(output, "http://localhost:6275\n") {
			t.Errorf("URL line should not appear, got %q", output)
		}
		if !strings.Contains(output, "README.md") {
			t.Errorf("expected deeplink in output, got %q", output)
		}
	})

	t.Run("json mode with uploaded file keeps path empty", func(t *testing.T) {
		jsonOutput = true
		defer func() { jsonOutput = false }()

		uploaded := []deeplinkEntry{
			{URL: "http://localhost:6275/?file=xyz", Path: "", Name: "upload.md"},
		}

		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		oldStdout := os.Stdout
		os.Stdout = w

		emitServeOutput("localhost:6275", uploaded, true)

		w.Close()
		os.Stdout = oldStdout

		var buf bytes.Buffer
		buf.ReadFrom(r) //nolint:errcheck

		var output jsonServeOutput
		if err := json.Unmarshal(buf.Bytes(), &output); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		if output.Files[0].Path != "" {
			t.Errorf("got Path %q, want empty string", output.Files[0].Path)
		}
		if output.Files[0].Name != "upload.md" {
			t.Errorf("got Name %q, want %q", output.Files[0].Name, "upload.md")
		}
	})
}

func TestWaitForServerDown(t *testing.T) {
	// Use a short timeout for tests.
	orig := waitForServerDownTimeout
	waitForServerDownTimeout = 500 * time.Millisecond
	t.Cleanup(func() { waitForServerDownTimeout = orig })

	t.Run("returns nil when server actually stops", func(t *testing.T) {
		callCount := 0
		stopCh := make(chan struct{}, 1)

		var srv *httptest.Server //nolint:staticcheck // declared before assignment so the closure can reference srv
		srv = newFakeMoServer(t, func(w http.ResponseWriter, r *http.Request) {
			callCount++
			if callCount >= 3 {
				select {
				case stopCh <- struct{}{}:
				default:
				}
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"version": "test", "pid": 1, "groups": []any{}}) //nolint:errcheck
		})

		go func() {
			<-stopCh
			srv.Close()
		}()

		addr := strings.TrimPrefix(srv.URL, "http://")
		err := waitForServerDown(addr)
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	})

	t.Run("returns error on timeout", func(t *testing.T) {
		srv := newFakeMoServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"version": "test", "pid": 1, "groups": []any{}}) //nolint:errcheck
		})

		addr := strings.TrimPrefix(srv.URL, "http://")
		err := waitForServerDown(addr)
		if err == nil {
			t.Fatal("expected timeout error, got nil")
		}
		if !strings.Contains(err.Error(), "did not shut down") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

// newFakeMoServer creates an httptest server that handles /_/api/status with
// the provided handler, and /_/api/shutdown with a 202 response.
func newFakeMoServer(t *testing.T, statusHandler http.HandlerFunc) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /_/api/status", statusHandler)
	mux.HandleFunc("POST /_/api/shutdown", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestWaitForReady_Success(t *testing.T) {
	pid := os.Getpid()
	srv := newFakeMoServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"version": "test", "pid": pid, "groups": []any{}}) //nolint:errcheck
	})
	addr := strings.TrimPrefix(srv.URL, "http://")

	status, err := waitForReady(addr, pid, 2*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status == nil {
		t.Fatal("expected non-nil status")
	}
	if status.PID != pid {
		t.Fatalf("got PID %d, want %d", status.PID, pid)
	}
}

func TestWaitForReady_PIDMismatch(t *testing.T) {
	childPID := os.Getpid()
	otherPID := childPID + 1
	srv := newFakeMoServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"version": "test", "pid": otherPID, "groups": []any{}}) //nolint:errcheck
	})
	addr := strings.TrimPrefix(srv.URL, "http://")

	status, err := waitForReady(addr, childPID, 2*time.Second)
	if !errors.Is(err, errServerConflict) {
		t.Fatalf("got error %v, want errServerConflict", err)
	}
	if status == nil {
		t.Fatal("expected non-nil status")
	}
}

func TestWaitForReady_NonMoServer(t *testing.T) {
	childPID := os.Getpid()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /_/api/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html>not ml</html>")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	addr := strings.TrimPrefix(srv.URL, "http://")

	_, err := waitForReady(addr, childPID, 300*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "did not become ready") {
		t.Fatalf("got error %q, want 'did not become ready'", err.Error())
	}
}

func TestWaitForReady_ChildExited(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires the Unix 'true' binary")
	}
	// Start a child without waiting: it exits immediately but stays a zombie,
	// so its pid cannot be reused and processAlive's non-blocking reap is
	// what detects the death (same as a real spawned server child).
	exited := exec.Command("true")
	if err := exited.Start(); err != nil {
		t.Fatalf("failed to start helper process: %v", err)
	}
	deadPID := exited.Process.Pid

	mux := http.NewServeMux()
	mux.HandleFunc("GET /_/api/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html>not ml</html>")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	addr := strings.TrimPrefix(srv.URL, "http://")

	start := time.Now()
	_, err := waitForReady(addr, deadPID, 5*time.Second)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "exited unexpectedly") {
		t.Fatalf("got error %q, want 'exited unexpectedly'", err.Error())
	}
	// Death detection plus the deadChildGrace window should still finish
	// well before the 5s timeout.
	if elapsed > 3*time.Second {
		t.Fatalf("waitForReady took %s, want fail-fast (<3s)", elapsed)
	}
}

func TestProcessAlive_ZombieChild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("zombie processes are a Unix concept")
	}
	// Start a child and do not Wait: after exit it stays a zombie, for which
	// kill(pid, 0) still succeeds. processAlive must detect it as dead.
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start helper process: %v", err)
	}
	pid := cmd.Process.Pid

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("processAlive still reports true for exited (zombie) child after 2s")
}

func TestAddToRunningServer(t *testing.T) {
	origNoOpen := noOpen
	noOpen = true
	t.Cleanup(func() { noOpen = origNoOpen })

	var postCount int
	mux := http.NewServeMux()
	mux.HandleFunc("GET /_/api/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"version": "test", "pid": 12345, "groups": []any{}}) //nolint:errcheck
	})
	mux.HandleFunc("POST /_/api/groups/{group}/files", func(w http.ResponseWriter, r *http.Request) {
		postCount++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(server.FileEntry{ID: "abc12345", Path: "/tmp/x.md", Name: "x.md"}) //nolint:errcheck
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	addr := strings.TrimPrefix(srv.URL, "http://")

	status := &statusResponse{PID: 12345}
	err := addToRunningServer(addr, status, map[string][]string{"default": {"/tmp/x.md"}}, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if postCount != 1 {
		t.Fatalf("got %d POSTs, want 1", postCount)
	}
}

func TestAddToRunningServer_AllPostsFail(t *testing.T) {
	origNoOpen := noOpen
	noOpen = true
	t.Cleanup(func() { noOpen = origNoOpen })

	mux := http.NewServeMux()
	mux.HandleFunc("POST /_/api/groups/{group}/files", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "shutting down", http.StatusServiceUnavailable)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	addr := strings.TrimPrefix(srv.URL, "http://")

	status := &statusResponse{PID: 12345}
	err := addToRunningServer(addr, status, map[string][]string{"default": {"/tmp/x.md"}}, nil, nil)
	if err == nil {
		t.Fatal("expected error when every POST fails, got nil")
	}
	if !strings.Contains(err.Error(), "failed to add any items") {
		t.Fatalf("got error %q, want 'failed to add any items'", err.Error())
	}
}

// A pattern-only invocation must not report success when every pattern POST
// fails on the winning server. Previously len(patterns) was added to the
// counter unconditionally, defeating the "attempted > 0 && added == 0" guard.
func TestAddToRunningServer_PatternsAllFail(t *testing.T) {
	origNoOpen := noOpen
	noOpen = true
	t.Cleanup(func() { noOpen = origNoOpen })

	mux := http.NewServeMux()
	mux.HandleFunc("POST /_/api/patterns", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "shutting down", http.StatusServiceUnavailable)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	addr := strings.TrimPrefix(srv.URL, "http://")

	status := &statusResponse{PID: 12345}
	err := addToRunningServer(addr, status, nil, map[string][]patternSpec{"default": {{Pattern: "*.md"}}}, nil)
	if err == nil {
		t.Fatal("expected error when every pattern POST fails, got nil")
	}
	if !strings.Contains(err.Error(), "failed to add any items") {
		t.Fatalf("got error %q, want 'failed to add any items'", err.Error())
	}
}

// A pattern that legitimately matches zero files must still count as added,
// so the "added N item(s)" line does not misreport it as a failure.
func TestAddToRunningServer_PatternWithZeroMatches(t *testing.T) {
	origNoOpen := noOpen
	noOpen = true
	t.Cleanup(func() { noOpen = origNoOpen })

	mux := http.NewServeMux()
	mux.HandleFunc("POST /_/api/patterns", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(server.AddPatternResponse{Files: nil}) //nolint:errcheck
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	addr := strings.TrimPrefix(srv.URL, "http://")

	status := &statusResponse{PID: 12345}
	err := addToRunningServer(addr, status, nil, map[string][]patternSpec{"default": {{Pattern: "*.md"}}}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIsLoopbackBind(t *testing.T) {
	tests := []struct {
		name string
		bind string
		want bool
	}{
		{"localhost", "localhost", true},
		{"127.0.0.1", "127.0.0.1", true},
		{"::1", "::1", true},
		{"127.0.0.2", "127.0.0.2", true},
		{"0.0.0.0", "0.0.0.0", false},
		{"::", "::", false},
		{"192.168.1.1", "192.168.1.1", false},
		{"10.0.0.1", "10.0.0.1", false},
		{"example.com", "example.com", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isLoopbackBind(tt.bind)
			if got != tt.want {
				t.Errorf("isLoopbackBind(%q) = %v, want %v", tt.bind, got, tt.want)
			}
		})
	}
}

func writeTestFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("failed to write test file %s: %v", path, err)
	}
}

func TestResolveArgs_Directory(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.md"), []byte("# A"))
	writeTestFile(t, filepath.Join(dir, "b.md"), []byte("# B"))
	writeTestFile(t, filepath.Join(dir, "c.txt"), []byte("text"))

	files, patterns, err := resolveArgs([]string{dir}, false, false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(patterns) != 0 {
		t.Fatalf("got %d patterns, want 0", len(patterns))
	}
	if len(files) != 2 {
		t.Fatalf("got %d files, want 2: %v", len(files), files)
	}
	for _, f := range files {
		if !strings.HasSuffix(f, ".md") {
			t.Errorf("unexpected non-.md file: %s", f)
		}
	}
}

func TestResolveArgs_DirectoryNaturalOrder(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"i1.md", "i2.md", "i10.md", "i11.md"} {
		writeTestFile(t, filepath.Join(dir, name), []byte("# "+name))
	}

	files, _, err := resolveArgs([]string{dir}, false, false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{
		filepath.Join(dir, "i1.md"),
		filepath.Join(dir, "i2.md"),
		filepath.Join(dir, "i10.md"),
		filepath.Join(dir, "i11.md"),
	}
	if len(files) != len(want) {
		t.Fatalf("got %d files, want %d: %v", len(files), len(want), files)
	}
	for i := range want {
		if files[i] != want[i] {
			t.Errorf("files[%d] = %q, want %q", i, files[i], want[i])
		}
	}
}

func TestResolveArgs_DirectoryWithWatch(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.md"), []byte("# A"))

	files, specs, err := resolveArgs([]string{dir}, true, false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("got %d files, want 0", len(files))
	}
	if len(specs) != 1 {
		t.Fatalf("got %d specs, want 1", len(specs))
	}
	want := filepath.Join(dir, "*.md")
	if specs[0].Pattern != want {
		t.Errorf("got pattern %q, want %q", specs[0].Pattern, want)
	}
}

func TestResolveArgs_DirectoryWithWatchRecursive(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.md"), []byte("# A"))

	files, specs, err := resolveArgs([]string{dir}, true, true, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("got %d files, want 0", len(files))
	}
	if len(specs) != 1 {
		t.Fatalf("got %d specs, want 1", len(specs))
	}
	want := filepath.Join(dir, "**/*.md")
	if specs[0].Pattern != want {
		t.Errorf("got pattern %q, want %q", specs[0].Pattern, want)
	}
}

func TestResolveArgs_DirectoryRecursive(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.md"), []byte("# A"))
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(sub, "b.md"), []byte("# B"))
	writeTestFile(t, filepath.Join(sub, "c.txt"), []byte("text"))

	files, patterns, err := resolveArgs([]string{dir}, false, true, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(patterns) != 0 {
		t.Fatalf("got %d patterns, want 0", len(patterns))
	}
	if len(files) != 2 {
		t.Fatalf("got %d files, want 2: %v", len(files), files)
	}
	wantNested := filepath.Join(sub, "b.md")
	if !slices.Contains(files, wantNested) {
		t.Errorf("recursive expansion missed nested file %q in %v", wantNested, files)
	}
}

func TestResolveArgs_GlobPositional_WatchMode(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.md"), []byte("# A"))
	pattern := filepath.Join(dir, "*.md")

	files, specs, err := resolveArgs([]string{pattern}, true, false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("got %d files, want 0", len(files))
	}
	if len(specs) != 1 {
		t.Fatalf("got %d specs, want 1: %v", len(specs), specs)
	}
	if !filepath.IsAbs(specs[0].Pattern) {
		t.Errorf("pattern %q is not absolute", specs[0].Pattern)
	}
}

func TestResolveArgs_GlobPositional_NonWatch(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.md"), []byte("# A"))
	writeTestFile(t, filepath.Join(dir, "b.md"), []byte("# B"))
	pattern := filepath.Join(dir, "*.md")

	files, patterns, err := resolveArgs([]string{pattern}, false, false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(patterns) != 0 {
		t.Fatalf("got %d patterns, want 0", len(patterns))
	}
	if len(files) != 2 {
		t.Fatalf("got %d files, want 2: %v", len(files), files)
	}
}

func TestResolveArgs_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()

	_, _, err := resolveArgs([]string{dir}, false, false, nil)
	if err == nil {
		t.Fatal("expected error for empty directory")
	}
	if !strings.Contains(err.Error(), "no .md files") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestStdinName(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"simple content", "# Hello World"},
		{"empty content", ""},
		{"japanese content", "# 日本語テスト"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stdinName(tt.content)
			if !strings.HasPrefix(got, "stdin-") {
				t.Errorf("stdinName(%q) = %q, want prefix 'stdin-'", tt.content, got)
			}
			if !strings.HasSuffix(got, ".md") {
				t.Errorf("stdinName(%q) = %q, want suffix '.md'", tt.content, got)
			}
			// "stdin-" (6) + hash (7) + ".md" (3) = 16
			if len(got) != 16 {
				t.Errorf("stdinName(%q) = %q (len %d), want len 16", tt.content, got, len(got))
			}
		})
	}

	t.Run("same content produces same name", func(t *testing.T) {
		a := stdinName("# Hello")
		b := stdinName("# Hello")
		if a != b {
			t.Errorf("same content gave different names: %q vs %q", a, b)
		}
	})

	t.Run("different content produces different name", func(t *testing.T) {
		a := stdinName("# A")
		b := stdinName("# B")
		if a == b {
			t.Errorf("different content gave same name: %q", a)
		}
	})
}

func TestReadStdin(t *testing.T) {
	t.Run("reads piped content", func(t *testing.T) {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { r.Close() })
		content := "# Test Document\n\nHello world."
		go func() {
			defer w.Close()
			if _, err := w.Write([]byte(content)); err != nil {
				t.Errorf("failed to write to pipe: %v", err)
			}
		}()

		name, got, err := readStdin(r)
		if err != nil {
			t.Fatal(err)
		}
		if got != content {
			t.Errorf("got content %q, want %q", got, content)
		}
		if !strings.HasPrefix(name, "stdin-") || !strings.HasSuffix(name, ".md") {
			t.Errorf("got name %q, want stdin-<hash>.md format", name)
		}
	})

	t.Run("exceeds max size", func(t *testing.T) {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { r.Close() })
		go func() {
			defer w.Close()
			// Write just over the limit
			buf := make([]byte, maxStdinSize+1)
			if _, err := w.Write(buf); err != nil {
				t.Errorf("failed to write to pipe: %v", err)
			}
		}()

		_, _, err = readStdin(r)
		if err == nil {
			t.Fatal("expected error for oversized stdin")
		}
		if !strings.Contains(err.Error(), "too large") {
			t.Errorf("got error %q, want 'too large' message", err.Error())
		}
	})

	t.Run("empty stdin", func(t *testing.T) {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { r.Close() })
		w.Close()

		name, got, err := readStdin(r)
		if err != nil {
			t.Fatal(err)
		}
		if got != "" {
			t.Errorf("got content %q, want empty", got)
		}
		if !strings.HasPrefix(name, "stdin-") {
			t.Errorf("got name %q, want stdin- prefix", name)
		}
	})
}

func TestResolveArgs_EmptyDirectoryWithWatch(t *testing.T) {
	dir := t.TempDir()

	files, patterns, err := resolveArgs([]string{dir}, true, false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("got %d files, want 0", len(files))
	}
	if len(patterns) != 1 {
		t.Fatalf("got %d patterns, want 1", len(patterns))
	}
}

func TestResolveArgs_MixedFilesAndDirs(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "a.md"), []byte("# A"))

	singleFile := filepath.Join(t.TempDir(), "standalone.md")
	writeTestFile(t, singleFile, []byte("# Standalone"))

	files, patterns, err := resolveArgs([]string{dir, singleFile}, false, false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(patterns) != 0 {
		t.Fatalf("got %d patterns, want 0", len(patterns))
	}
	if len(files) != 2 {
		t.Fatalf("got %d files, want 2: %v", len(files), files)
	}
}

// withFilterFlags sets the discovery-filter flags and restores them afterwards.
func withFilterFlags(t *testing.T, excl []string, hidden bool, ignoreFileName string) {
	t.Helper()
	origExcludes, origIncludeHidden, origIgnoreFile := excludes, includeHidden, ignoreFile
	excludes = excl
	includeHidden = hidden
	ignoreFile = ignoreFileName
	t.Cleanup(func() {
		excludes = origExcludes
		includeHidden = origIncludeHidden
		ignoreFile = origIgnoreFile
	})
}

func TestResolveFilter(t *testing.T) {
	cwd := t.TempDir()

	t.Run("missing ignore file is not an error", func(t *testing.T) {
		withFilterFlags(t, nil, false, ".mlignore")
		f, err := resolveFilter(cwd)
		if err != nil {
			t.Fatalf("resolveFilter returned error: %v", err)
		}
		if f.Rules().Base != filepath.ToSlash(cwd) {
			t.Fatalf("rules base = %q, want %q", f.Rules().Base, filepath.ToSlash(cwd))
		}
		if len(f.Rules().Excludes) != 0 {
			t.Fatalf("rules excludes = %v, want empty", f.Rules().Excludes)
		}
	})

	t.Run("ignore file lines precede exclude flags", func(t *testing.T) {
		os.WriteFile(filepath.Join(cwd, ".mlignore"), []byte("vendor/**\n*.tmp\n"), 0o600) //nolint:errcheck
		withFilterFlags(t, []string{"drafts/**"}, false, ".mlignore")
		f, err := resolveFilter(cwd)
		if err != nil {
			t.Fatalf("resolveFilter returned error: %v", err)
		}
		want := []string{"vendor/**", "*.tmp", "drafts/**"}
		if !slices.Equal(f.Rules().Excludes, want) {
			t.Fatalf("excludes = %v, want %v", f.Rules().Excludes, want)
		}
	})

	t.Run("invalid exclude flag is an error", func(t *testing.T) {
		withFilterFlags(t, []string{"bad["}, false, "")
		if _, err := resolveFilter(cwd); err == nil {
			t.Fatal("resolveFilter returned nil error for invalid --exclude")
		}
	})

	t.Run("invalid ignore line is skipped with the rest kept", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, ".mlignore"), []byte("vendor/**\nbad[\n"), 0o600) //nolint:errcheck
		withFilterFlags(t, nil, false, ".mlignore")
		f, err := resolveFilter(dir)
		if err != nil {
			t.Fatalf("resolveFilter returned error: %v", err)
		}
		if !slices.Equal(f.Rules().Excludes, []string{"vendor/**"}) {
			t.Fatalf("excludes = %v, want [vendor/**]", f.Rules().Excludes)
		}
	})

	t.Run("empty ignore-file name disables the file", func(t *testing.T) {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, ".mlignore"), []byte("vendor/**\n"), 0o600) //nolint:errcheck
		withFilterFlags(t, nil, false, "")
		f, err := resolveFilter(dir)
		if err != nil {
			t.Fatalf("resolveFilter returned error: %v", err)
		}
		if len(f.Rules().Excludes) != 0 {
			t.Fatalf("excludes = %v, want empty with empty ignore-file name", f.Rules().Excludes)
		}
	})
}

func TestResolveArgs_Filtering(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{
		filepath.Join(dir, "a.md"),
		filepath.Join(dir, ".hidden.md"),
		filepath.Join(dir, "vendor", "v.md"),
		filepath.Join(dir, "docs", "d.md"),
	} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("# "+filepath.Base(p)), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("hidden files excluded by default", func(t *testing.T) {
		withFilterFlags(t, nil, false, "")
		filter, ferr := resolveFilter(dir)
		if ferr != nil {
			t.Fatalf("resolveFilter: %v", ferr)
		}
		files, _, err := resolveArgs([]string{dir}, false, true, filter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if slices.Contains(files, filepath.Join(dir, ".hidden.md")) {
			t.Errorf("hidden file was not excluded: %v", files)
		}
		if !slices.Contains(files, filepath.Join(dir, "a.md")) {
			t.Errorf("plain file missing: %v", files)
		}
	})

	t.Run("exclude flag filters expansion", func(t *testing.T) {
		withFilterFlags(t, []string{"vendor/**"}, false, "")
		filter, ferr := resolveFilter(dir)
		if ferr != nil {
			t.Fatalf("resolveFilter: %v", ferr)
		}
		files, _, err := resolveArgs([]string{dir}, false, true, filter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if slices.Contains(files, filepath.Join(dir, "vendor", "v.md")) {
			t.Errorf("excluded file was not filtered: %v", files)
		}
	})

	t.Run("exclude counts appear in error message", func(t *testing.T) {
		withFilterFlags(t, []string{"**"}, false, "")
		filter, ferr := resolveFilter(dir)
		if ferr != nil {
			t.Fatalf("resolveFilter: %v", ferr)
		}
		_, _, err := resolveArgs([]string{dir}, false, true, filter)
		if err == nil {
			t.Fatal("expected error when everything is excluded")
		}
		if !strings.Contains(err.Error(), "excluded by --exclude") {
			t.Fatalf("error %q does not mention the exclusion count", err.Error())
		}
	})

	t.Run("explicit file arguments bypass filtering", func(t *testing.T) {
		withFilterFlags(t, []string{"**"}, false, "")
		filter, ferr := resolveFilter(dir)
		if ferr != nil {
			t.Fatalf("resolveFilter: %v", ferr)
		}
		files, _, err := resolveArgs([]string{filepath.Join(dir, "vendor", "v.md")}, false, true, filter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(files) != 1 || files[0] != filepath.Join(dir, "vendor", "v.md") {
			t.Fatalf("explicit file was filtered: %v", files)
		}
	})

	t.Run("includeHidden admits hidden files", func(t *testing.T) {
		withFilterFlags(t, nil, true, "")
		filter, ferr := resolveFilter(dir)
		if ferr != nil {
			t.Fatalf("resolveFilter: %v", ferr)
		}
		files, _, err := resolveArgs([]string{dir}, false, true, filter)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !slices.Contains(files, filepath.Join(dir, ".hidden.md")) {
			t.Errorf("hidden file missing with --include-hidden: %v", files)
		}
	})
}

func TestPostPatterns_SendsFilter(t *testing.T) {
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("POST /_/api/patterns", func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read body: %v", err)
		}
		if err := json.Unmarshal(data, &gotBody); err != nil {
			t.Errorf("failed to decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(server.AddPatternResponse{PatternStats: server.PatternStats{Matched: 0}}) //nolint:errcheck
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()
	addr := strings.TrimPrefix(ts.URL, "http://")
	client := ts.Client()

	specs := []patternSpec{{Pattern: "/repo/**/*.md", Rules: ignore.Rules{Base: "/repo", Excludes: []string{"vendor/**"}}}}
	entries, added := postPatterns(client, addr, "default", specs)
	if added != 1 || len(entries) != 0 {
		t.Fatalf("got added=%d entries=%d, want added=1 entries=0", added, len(entries))
	}
	filter, ok := gotBody["filter"].(map[string]any)
	if !ok {
		t.Fatalf("request body has no filter object: %v", gotBody)
	}
	if filter["base"] != "/repo" || !slices.Equal(stringSlice(filter["excludes"]), []string{"vendor/**"}) {
		t.Fatalf("got filter %v, want base=/repo excludes=[vendor/**]", filter)
	}
}

func stringSlice(v any) []string {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func TestMergePatternSpecs(t *testing.T) {
	t.Run("restored first, new appended, duplicates skipped", func(t *testing.T) {
		base := []patternSpec{{Pattern: "a"}, {Pattern: "b"}}
		additional := []patternSpec{{Pattern: "b", Rules: ignore.Rules{Base: "/repo", Excludes: []string{"x/**"}}}, {Pattern: "c"}}
		got := mergePatternSpecs(base, additional)
		if len(got) != 3 || got[0].Pattern != "a" || got[1].Pattern != "b" || got[2].Pattern != "c" {
			t.Fatalf("got %v", got)
		}
		// Conflicting rules take the latest invocation's.
		if got[1].Rules.Base != "/repo" {
			t.Fatalf("conflicting spec kept old rules: %+v", got[1])
		}
	})

	t.Run("empty additional keeps base", func(t *testing.T) {
		base := []patternSpec{{Pattern: "a"}}
		if got := mergePatternSpecs(base, nil); len(got) != 1 || got[0].Pattern != "a" {
			t.Fatalf("got %v", got)
		}
	})
}

func TestRestorePatternSpecs(t *testing.T) {
	t.Run("filters are attached to their patterns", func(t *testing.T) {
		patterns := map[string][]string{"default": {"/x/*.md", "/y/*.md"}}
		filters := []server.PatternFilterData{{
			Pattern: "/x/*.md",
			Rules:   ignore.Rules{Base: "/repo", Excludes: []string{"vendor/**"}},
		}}
		specs := restorePatternSpecs(patterns, filters)
		if len(specs["default"]) != 2 {
			t.Fatalf("got %d specs, want 2", len(specs["default"]))
		}
		if specs["default"][0].Pattern != "/x/*.md" || len(specs["default"][0].Rules.Excludes) != 1 {
			t.Fatalf("first spec should carry the persisted rules: %+v", specs["default"][0])
		}
		if specs["default"][1].Pattern != "/y/*.md" || !specs["default"][1].Rules.Empty() {
			t.Fatalf("pattern without filter should get zero rules: %+v", specs["default"][1])
		}
	})

	t.Run("legacy backup without filters yields zero rules", func(t *testing.T) {
		specs := restorePatternSpecs(map[string][]string{"default": {"/x/*.md"}}, nil)
		if !specs["default"][0].Rules.Empty() {
			t.Fatalf("legacy pattern should get zero rules: %+v", specs["default"][0])
		}
	})
}

// captureStderr runs fn while capturing what it writes to os.Stderr.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStderr := os.Stderr
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		defer func() {
			if err := r.Close(); err != nil {
				t.Error(err)
			}
		}()
		b, err := io.ReadAll(r)
		if err != nil {
			t.Error(err)
		}
		done <- string(b)
	}()
	fn()
	os.Stderr = oldStderr
	w.Close()
	return <-done
}

func TestProbeServer_AppField(t *testing.T) {
	tests := []struct {
		name     string
		status   map[string]any
		wantErr  bool
		wantNote string // empty: no stderr note expected
		wantApp  string
		wantVer  string
	}{
		{
			name:    "ml server passes",
			status:  map[string]any{"version": "0.2.0", "app": "ml", "pid": 1, "groups": []any{}},
			wantApp: "ml",
			wantVer: "0.2.0",
		},
		{
			name:     "legacy server without app passes with note",
			status:   map[string]any{"version": "0.1.0", "pid": 1, "groups": []any{}},
			wantApp:  "",
			wantVer:  "0.1.0",
			wantNote: "did not identify itself (ml ≤ 0.1.0",
		},
		{
			name:     "legacy server with 1.x version hints at mo",
			status:   map[string]any{"version": "1.6.8", "pid": 1, "groups": []any{}},
			wantApp:  "",
			wantVer:  "1.6.8",
			wantNote: "may be an upstream mo instance",
		},
		{
			name:    "foreign app is rejected",
			status:  map[string]any{"version": "1.6.8", "app": "mo", "pid": 1, "groups": []any{}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/_/api/status" {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(tt.status) //nolint:errcheck
			}))
			defer ts.Close()
			addr := strings.TrimPrefix(ts.URL, "http://")

			res, err := probeServer(addr)
			if tt.wantErr {
				if err == nil {
					t.Fatal("probeServer succeeded, want foreign-server error")
				}
				if !errors.Is(err, errForeignServer) {
					t.Fatalf("error %v does not mark a foreign server", err)
				}
				if !strings.Contains(err.Error(), "is a mo instance") || !strings.Contains(err.Error(), "use --port") {
					t.Fatalf("error %v lacks the identity details", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("probeServer returned error: %v", err)
			}
			if res.app != tt.wantApp || res.version != tt.wantVer {
				t.Fatalf("got app=%q version=%q, want %q/%q", res.app, res.version, tt.wantApp, tt.wantVer)
			}

			// The legacy note is printed exactly once per attach decision.
			captured := captureStderr(t, func() {
				noteLegacyServer(addr, res)
			})
			if tt.wantNote == "" && captured != "" {
				t.Fatalf("unexpected stderr note: %q", captured)
			}
			if tt.wantNote != "" && !strings.Contains(captured, tt.wantNote) {
				t.Fatalf("stderr note %q does not contain %q", captured, tt.wantNote)
			}
		})
	}
}

func TestReload_RulesResolution(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	cwdSlash := filepath.ToSlash(cwd)
	if err := os.WriteFile(filepath.Join(dir, ".mlignore"), []byte("docs/**\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pattern := cwdSlash + "/**/*.md"

	var reloadBodies []map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("GET /_/api/status", func(w http.ResponseWriter, _ *http.Request) {
		status := map[string]any{
			"version": "0.2.0",
			"pid":     1,
			"app":     "ml",
			"groups": []map[string]any{{
				"name":  "default",
				"files": []map[string]any{},
				"patterns": []string{
					pattern,              // reloadable
					"/other/dir/**/*.md", // other base, skipped client-side
				},
				"patternFilters": []map[string]any{
					{
						"pattern": pattern,
						"group":   "default",
						"rules": map[string]any{
							"base":         cwdSlash,
							"ignoreFile":   ".mlignore",
							"flagExcludes": []string{"vendor/**"},
						},
					},
					{
						"pattern": "/other/dir/**/*.md",
						"group":   "default",
						"rules":   map[string]any{"base": "/other/dir", "ignoreFile": ".mlignore", "flagExcludes": []string{}},
					},
					{
						// ml ≤ 0.1.0 shape: no provenance, cannot be reloaded.
						"pattern": cwdSlash + "/legacy/**/*.md",
						"group":   "default",
						"rules":   map[string]any{"base": cwdSlash},
					},
				},
			}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status) //nolint:errcheck
	})
	mux.HandleFunc("POST /_/api/reload", func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read body: %v", err)
		}
		var body map[string]any
		if err := json.Unmarshal(data, &body); err != nil {
			t.Errorf("failed to decode body: %v", err)
		}
		reloadBodies = append(reloadBodies, body)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(server.ReloadResponse{Patterns: 1}) //nolint:errcheck
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()
	addr := strings.TrimPrefix(ts.URL, "http://")

	t.Run("keeps stored flags, rereads ignore file", func(t *testing.T) {
		reloadBodies = nil
		oldExcludes := excludes
		oldHidden := includeHidden
		defer func() { excludes, includeHidden = oldExcludes, oldHidden }()
		excludes = []string{"vendor/**"}
		includeHidden = false

		if err := doReload(addr, false, false); err != nil {
			t.Fatal(err)
		}
		if len(reloadBodies) != 1 {
			t.Fatalf("got %d reload requests, want 1", len(reloadBodies))
		}
		filters, ok := reloadBodies[0]["filters"].([]any)
		if !ok || len(filters) != 1 {
			t.Fatalf("body filters = %v, want exactly the reloadable pattern", reloadBodies[0]["filters"])
		}
		filter, ok := filters[0].(map[string]any)
		if !ok {
			t.Fatalf("filter entry is not an object: %v", filters[0])
		}
		if filter["pattern"] != pattern || filter["group"] != "default" {
			t.Fatalf("got filter %v, want pattern %s in group default", filter, pattern)
		}
		rules, ok := filter["rules"].(map[string]any)
		if !ok {
			t.Fatalf("filter rules are not an object: %v", filter["rules"])
		}
		if rules["base"] != cwdSlash {
			t.Fatalf("rules base = %v, want %s", rules["base"], cwdSlash)
		}
		// File lines first, then the stored --exclude values.
		wantExcludes := []any{"docs/**", "vendor/**"}
		gotExcludes, ok := rules["excludes"].([]any)
		if !ok || !slices.Equal(gotExcludes, wantExcludes) {
			t.Fatalf("rules excludes = %v, want %v", rules["excludes"], wantExcludes)
		}
		if rules["ignoreFile"] != ".mlignore" {
			t.Fatalf("rules ignoreFile = %v, want .mlignore", rules["ignoreFile"])
		}
		if !slices.Equal(stringSlice(rules["flagExcludes"]), []string{"vendor/**"}) {
			t.Fatalf("rules flagExcludes = %v, want [vendor/**]", rules["flagExcludes"])
		}
		// includeHidden is omitempty, so false arrives as JSON-absent.
		if v, ok := rules["includeHidden"].(bool); ok && v {
			t.Fatalf("rules includeHidden = %v, want false (unchanged)", rules["includeHidden"])
		}
	})

	t.Run("exclude flag replaces stored values only when changed", func(t *testing.T) {
		reloadBodies = nil
		oldExcludes := excludes
		oldHidden := includeHidden
		defer func() { excludes, includeHidden = oldExcludes, oldHidden }()
		excludes = []string{"other/**"}
		includeHidden = true

		if err := doReload(addr, true, true); err != nil {
			t.Fatal(err)
		}
		rules, ok := reloadBodies[0]["filters"].([]any)[0].(map[string]any)["rules"].(map[string]any)
		if !ok {
			t.Fatal("filter rules are not an object")
		}
		wantExcludes := []any{"docs/**", "other/**"}
		gotExcludes, ok := rules["excludes"].([]any)
		if !ok || !slices.Equal(gotExcludes, wantExcludes) {
			t.Fatalf("rules excludes = %v, want %v", rules["excludes"], wantExcludes)
		}
		if !slices.Equal(stringSlice(rules["flagExcludes"]), []string{"other/**"}) {
			t.Fatalf("rules flagExcludes = %v, want [other/**]", rules["flagExcludes"])
		}
		if rules["includeHidden"] != true {
			t.Fatalf("rules includeHidden = %v, want true (flag given)", rules["includeHidden"])
		}
	})
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdout := os.Stdout
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		defer func() {
			if err := r.Close(); err != nil {
				t.Error(err)
			}
		}()
		b, err := io.ReadAll(r)
		if err != nil {
			t.Error(err)
		}
		done <- string(b)
	}()
	fn()
	os.Stdout = oldStdout
	w.Close()
	return <-done
}

// fakeLogDir points XDG_STATE_HOME at a fresh directory and returns the log
// directory inside it plus the state root (for the backup directory).
func fakeLogDir(t *testing.T) (string, string) {
	t.Helper()
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	dir := filepath.Join(state, "ml", "log")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir, state
}

func TestRun_ClientCommandsDoNotCreateLogFile(t *testing.T) {
	tests := []struct {
		name  string
		setup func()
		args  []string
	}{
		{"status", func() { statusServer = true }, nil},
		{"prune", func() { pruneMode = true }, nil},
		{"reload without server", func() { reloadMode = true }, nil},
		{"shutdown dead port", func() { shutdownServer = true }, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logDir, _ := fakeLogDir(t)
			tt.setup()
			defer func() {
				statusServer, pruneMode, reloadMode, shutdownServer = false, false, false, false
			}()

			// The command may fail (nothing to talk to) — that is fine; the
			// point is that no log file is created along the way.
			if runErr := run(rootCmd, tt.args); runErr != nil {
				t.Logf("run failed as expected without a server: %v", runErr)
			}

			entries, err := os.ReadDir(logDir)
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return
				}
				t.Fatal(err)
			}
			if len(entries) > 0 {
				t.Fatalf("client-only command created log files: %v", entries)
			}
		})
	}
}

func TestPrune_StaleLogsRemoved(t *testing.T) {
	logDir, stateDir := fakeLogDir(t)

	// A running server keeps its logs.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /_/api/status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"version": "0.2.0", "app": "ml", "pid": 1}) //nolint:errcheck
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()
	alivePort, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(ts.URL, "http://127.0.0.1:"), ""))
	if err != nil {
		t.Fatal(err)
	}
	write := func(name string) {
		if err := os.WriteFile(filepath.Join(logDir, name), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(fmt.Sprintf("ml-%d.log", alivePort))
	write("ml-26999.log")
	write("ml-26998.log.1") // rotation of a dead port

	// 26997 has a saved session that must only be reported, not removed.
	backupDir := filepath.Join(stateDir, "ml", "backup")
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeBackup := filepath.Join(backupDir, "ml-26997.json")
	if err := os.WriteFile(writeBackup, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	// And its log, so the port is visited at all.
	write("ml-26997.log")

	out := captureStderr(t, func() {
		if err := doPrune(); err != nil {
			t.Error(err)
		}
	})

	if _, err := os.Stat(filepath.Join(logDir, fmt.Sprintf("ml-%d.log", alivePort))); err != nil {
		t.Fatalf("log of running server was removed: %v", err)
	}
	for _, name := range []string{"ml-26999.log", "ml-26998.log.1"} {
		if _, err := os.Stat(filepath.Join(logDir, name)); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("%s was not removed: %v", name, err)
		}
	}
	if _, err := os.Stat(writeBackup); err != nil {
		t.Fatalf("saved session removed without --prune-backups: %v", err)
	}
	if !strings.Contains(out, "use --prune-backups to remove") {
		t.Fatalf("stderr %q lacks the saved-session hint", out)
	}
	if !strings.Contains(out, "pruned 3 stale log file(s)") {
		t.Fatalf("stderr %q lacks the summary", out)
	}
}

func TestPrune_BackupsWithConfirmation(t *testing.T) {
	logDir, stateDir := fakeLogDir(t)
	backupDir := filepath.Join(stateDir, "ml", "backup")
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(backupDir, "ml-26997.json")
	if err := os.WriteFile(backupPath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logDir, "ml-26997.log"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Feed "y" to the confirmation prompt.
	pruneBackups = true
	defer func() { pruneBackups = false }()
	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = r
	if _, err := w.WriteString("y\n"); err != nil {
		t.Fatal(err)
	}
	w.Close()
	defer func() {
		os.Stdin = oldStdin
		r.Close() //nolint:errcheck
	}()

	out := captureStderr(t, func() {
		if err := doPrune(); err != nil {
			t.Error(err)
		}
	})
	if _, err := os.Stat(backupPath); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("saved session not removed despite confirmation: %v", err)
	}
	if !strings.Contains(out, "remove saved session for port 26997") {
		t.Fatalf("stderr %q lacks the confirmation prompt", out)
	}
}

func TestDoStatus_MarksStaleForeignAndWatcher(t *testing.T) {
	logDir, _ := fakeLogDir(t)

	healthy := httptest.NewServer(newStatusMux(map[string]any{
		"version": "0.2.0", "app": "ml", "pid": 11,
		"watcher": map[string]any{"status": "degraded", "failed": 2, "pendingRetries": 2,
			"lastError": "FSEventStreamStart failed", "lastErrorPath": "/repo/link"},
	}))
	defer healthy.Close()
	foreign := httptest.NewServer(newStatusMux(map[string]any{
		"version": "1.6.8", "app": "mo", "pid": 12,
	}))
	defer foreign.Close()
	legacy := httptest.NewServer(newStatusMux(map[string]any{
		"version": "0.1.0", "pid": 13,
	}))
	defer legacy.Close()

	portOf := func(url string) int {
		t.Helper()
		p, err := strconv.Atoi(url[strings.LastIndex(url, ":")+1:])
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	// A stale port: a log file with no listener behind it.
	stalePort := 26995

	for _, tc := range []struct {
		port  int
		label string
	}{
		{portOf(healthy.URL), "ml-%d.log"},
		{portOf(foreign.URL), "ml-%d.log"},
		{portOf(legacy.URL), "ml-%d.log"},
		{stalePort, "ml-%d.log"},
	} {
		if err := os.WriteFile(filepath.Join(logDir, fmt.Sprintf(tc.label, tc.port)), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	out := captureStdout(t, func() {
		if err := doStatus(); err != nil {
			t.Error(err)
		}
	})

	if !strings.Contains(out, "stale: no server; log file left over") {
		t.Fatalf("output lacks the stale marking:\n%s", out)
	}
	if !strings.Contains(out, "not ml: app=mo version=1.6.8") {
		t.Fatalf("output lacks the foreign marking:\n%s", out)
	}
	if !strings.Contains(out, "no app field; ml <=0.1.0 or upstream mo") {
		t.Fatalf("output lacks the legacy marking:\n%s", out)
	}
	if !strings.Contains(out, "watcher: degraded — 2 failed registration(s), 2 retry(s) pending (last: FSEventStreamStart failed at /repo/link)") {
		t.Fatalf("output lacks the degraded watcher line:\n%s", out)
	}
}

// newStatusMux serves GET /_/api/status with the given JSON body.
func newStatusMux(status map[string]any) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /_/api/status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status) //nolint:errcheck
	})
	return mux
}

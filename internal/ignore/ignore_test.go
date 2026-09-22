package ignore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseLine(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		wantOK   bool
		wantErr  bool
		wantRule rule
	}{
		{name: "empty", line: "", wantOK: false},
		{name: "blank spaces", line: "   \t ", wantOK: false},
		{name: "comment", line: "# a comment", wantOK: false},
		{name: "indented comment", line: "  # comment", wantOK: false},
		{name: "literal hash", line: `\#hash`, wantOK: true, wantRule: rule{pattern: "#hash"}},
		{name: "literal bang", line: `\!bang`, wantOK: true, wantRule: rule{pattern: "!bang"}},
		{name: "negate", line: "!keep.md", wantOK: true, wantRule: rule{pattern: "keep.md", negate: true}},
		{name: "dir only", line: "build/", wantOK: true, wantRule: rule{pattern: "build", dirOnly: true}},
		{name: "leading slash", line: "/docs/drafts", wantOK: true, wantRule: rule{pattern: "docs/drafts", anchored: true}},
		{name: "no slash", line: "vendor", wantOK: true, wantRule: rule{pattern: "vendor"}},
		{name: "doublestar", line: "**/node_modules/**", wantOK: true, wantRule: rule{pattern: "**/node_modules/**", anchored: true}},
		{name: "invalid pattern", line: "bad[", wantErr: true},
		{name: "dot", line: ".", wantErr: true},
		{name: "dot dot", line: "..", wantErr: true},
		{name: "negate nothing", line: "!", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok, err := parseLine(tt.line)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseLine(%q) error = nil, want error", tt.line)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseLine(%q) unexpected error: %v", tt.line, err)
			}
			if ok != tt.wantOK {
				t.Fatalf("parseLine(%q) ok = %v, want %v", tt.line, ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if got != tt.wantRule {
				t.Fatalf("parseLine(%q) = %+v, want %+v", tt.line, got, tt.wantRule)
			}
		})
	}
}

// A leading separator anchors the pattern at the base directory (gitignore
// semantics); it is not treated as a filesystem root on Unix.
func TestParseLine_LeadingSlashAnchors(t *testing.T) {
	got, ok, err := parseLine("/tmp/x.md")
	if err != nil || !ok {
		t.Fatalf("parseLine(%q) ok=%v err=%v", "/tmp/x.md", ok, err)
	}
	if got.absolute {
		t.Fatalf("parseLine(%q) = %+v, want absolute=false", "/tmp/x.md", got)
	}
	if !got.anchored || got.pattern != "tmp/x.md" {
		t.Fatalf("parseLine(%q) = %+v, want anchored pattern tmp/x.md", "/tmp/x.md", got)
	}
}

func TestRulesFilter_InvalidPattern(t *testing.T) {
	rules := Rules{Base: "/repo", Excludes: []string{"valid", "bad["}}
	if _, err := rules.Filter(); err == nil {
		t.Fatal("Rules.Filter() error = nil, want error for malformed line")
	} else if !strings.Contains(err.Error(), "bad[") {
		t.Fatalf("Rules.Filter() error = %v, want it to name the offending line", err)
	}
}

func TestValidateLine(t *testing.T) {
	if err := ValidateLine("# comment"); err != nil {
		t.Fatalf("ValidateLine(comment) = %v, want nil", err)
	}
	if err := ValidateLine("vendor/**"); err != nil {
		t.Fatalf("ValidateLine(vendor/**) = %v, want nil", err)
	}
	if err := ValidateLine("bad["); err == nil {
		t.Fatal("ValidateLine(bad[) = nil, want error")
	}
}

// filterFor builds a Filter whose anchored patterns resolve against base.
func filterFor(t *testing.T, base string, lines []string, includeHidden bool) *Filter {
	t.Helper()
	rules := Rules{Base: base, Excludes: lines, IncludeHidden: includeHidden}
	f, err := rules.Filter()
	if err != nil {
		t.Fatalf("Rules.Filter() unexpected error: %v", err)
	}
	return f
}

func TestRuleMatching(t *testing.T) {
	const base = "/repo"
	tests := []struct {
		name    string
		lines   []string
		path    string
		isDir   bool
		want    bool // true = admitted
		pattern string
	}{
		{
			name: "basename rule excludes at any depth", lines: []string{"vendor"},
			path: "/repo/vendor", isDir: true, want: false, pattern: "**",
		},
		{
			name: "basename rule excludes nested", lines: []string{"vendor"},
			path: "/repo/docs/vendor/x.md", isDir: false, want: false, pattern: "**",
		},
		{
			name: "basename rule does not hit similar name", lines: []string{"vendor"},
			path: "/repo/vendor.md", isDir: false, want: true, pattern: "**",
		},
		{
			name: "anchored rule excludes subtree", lines: []string{"vendor/**"},
			path: "/repo/vendor/x.md", isDir: false, want: false, pattern: "**",
		},
		{
			name: "anchored rule matches the dir itself", lines: []string{"vendor/**"},
			path: "/repo/vendor", isDir: true, want: false, pattern: "**",
		},
		{
			name: "anchored rule does not match other depth", lines: []string{"vendor/**"},
			path: "/repo/docs/vendor/x.md", isDir: false, want: true, pattern: "**",
		},
		{
			name: "leading slash anchors at base", lines: []string{"/docs/drafts/**"},
			path: "/repo/docs/drafts/a.md", isDir: false, want: false, pattern: "**",
		},
		{
			name: "leading slash does not match elsewhere", lines: []string{"/docs/drafts/**"},
			path: "/repo/other/drafts/a.md", isDir: false, want: true, pattern: "**",
		},
		{
			name: "anchored rule does not match outside base", lines: []string{"/somewhere/else/x.md"},
			path: "/somewhere/else/x.md", isDir: false, want: true, pattern: "**",
		},
		{
			name: "dir only rule excludes directory", lines: []string{"build/"},
			path: "/repo/build", isDir: true, want: false, pattern: "**",
		},
		{
			name: "dir only rule does not exclude file", lines: []string{"build/"},
			path: "/repo/build", isDir: false, want: true, pattern: "**",
		},
		{
			name: "dir only rule excludes contents via ancestor", lines: []string{"build/"},
			path: "/repo/build/sub/x.md", isDir: false, want: false, pattern: "**",
		},
		{
			name: "doublestar nested pattern", lines: []string{"**/node_modules/**"},
			path: "/repo/a/node_modules/x.md", isDir: false, want: false, pattern: "**",
		},
		{
			name: "last match negates", lines: []string{"*.md", "!keep.md"},
			path: "/repo/keep.md", isDir: false, want: true, pattern: "**",
		},
		{
			name: "last match excludes", lines: []string{"!keep.md", "*.md"},
			path: "/repo/keep.md", isDir: false, want: false, pattern: "**",
		},
		{
			name: "negation blocked under excluded ancestor", lines: []string{"vendor/**", "!vendor/keep.md"},
			path: "/repo/vendor/keep.md", isDir: false, want: false, pattern: "**",
		},
		{
			name: "unmatched path admitted", lines: []string{"vendor/**"},
			path: "/repo/docs/a.md", isDir: false, want: true, pattern: "**",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := filterFor(t, base, tt.lines, false)
			p := f.ForPattern(tt.pattern, base)
			if got := p.AdmitsPath(tt.path, tt.isDir); got != tt.want {
				t.Fatalf("AdmitsPath(%q, %v) = %v, want %v", tt.path, tt.isDir, got, tt.want)
			}
		})
	}
}

func TestHiddenRule(t *testing.T) {
	const base = "/repo"
	tests := []struct {
		name    string
		pattern string
		pbase   string
		path    string
		want    bool
	}{
		{name: "plain file admitted", pattern: "**/*.md", pbase: base, path: "/repo/a.md", want: true},
		{name: "nested file admitted", pattern: "**/*.md", pbase: base, path: "/repo/a/b.md", want: true},
		{name: "dot dir rejected", pattern: "**/*.md", pbase: base, path: "/repo/.git/x.md", want: false},
		{name: "nested dot dir rejected", pattern: "**/*.md", pbase: base, path: "/repo/a/.h/b.md", want: false},
		{name: "dot file rejected", pattern: "**/*.md", pbase: base, path: "/repo/.hidden.md", want: false},
		{name: "literal dot segment admits", pattern: ".*.md", pbase: base, path: "/repo/.notes.md", want: true},
		{name: "dot prefix pattern admits dot dir file", pattern: ".config/*.md", pbase: base, path: "/repo/.config/notes.md", want: true},
		{name: "explicit dot after doublestar admits", pattern: "**/.config/*.md", pbase: base, path: "/repo/a/.config/notes.md", want: true},
		{name: "explicit dot base admits", pattern: ".git/**/*.md", pbase: "/repo/.git", path: "/repo/.git/notes.md", want: true},
		{name: "question mark does not admit dot file", pattern: "?.md", pbase: base, path: "/repo/.md", want: false},
		{name: "question mark admits plain file", pattern: "?.md", pbase: base, path: "/repo/a.md", want: true},
		{name: "doublestar alone rejects dot dir", pattern: "**", pbase: base, path: "/repo/.git/x.md", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := filterFor(t, base, nil, false)
			p := f.ForPattern(tt.pattern, tt.pbase)
			if got := p.AdmitsFile(tt.path); got != tt.want {
				t.Fatalf("AdmitsFile(%q) with pattern %q = %v, want %v", tt.path, tt.pattern, got, tt.want)
			}
		})
	}
}

func TestHiddenRule_IncludeHidden(t *testing.T) {
	const base = "/repo"
	f := filterFor(t, base, nil, true)
	p := f.ForPattern("**/*.md", base)
	for _, path := range []string{"/repo/.git/x.md", "/repo/.hidden.md", "/repo/a/.h/b.md"} {
		if !p.AdmitsFile(path) {
			t.Fatalf("AdmitsFile(%q) = false with IncludeHidden, want true", path)
		}
	}
}

func TestPatternAdmitsDir(t *testing.T) {
	const base = "/repo"
	f := filterFor(t, base, []string{"vendor/**"}, false)

	p := f.ForPattern("**/*.md", base)
	if !p.AdmitsDir("/repo/a") {
		t.Fatal("AdmitsDir(/repo/a) = false, want true")
	}
	if !p.AdmitsDir("/repo/a/b") {
		t.Fatal("AdmitsDir(/repo/a/b) = false, want true")
	}
	if p.AdmitsDir("/repo/.git") {
		t.Fatal("AdmitsDir(/repo/.git) = true, want false")
	}
	if p.AdmitsDir("/repo/vendor") {
		t.Fatal("AdmitsDir(/repo/vendor) = true, want false (user rule)")
	}

	explicit := f.ForPattern("**/.config/*.md", base)
	if !explicit.AdmitsDir("/repo/.config") {
		t.Fatal("AdmitsDir(/repo/.config) with explicit pattern = false, want true")
	}
}

func TestPatternAdmitsPath_Ancestors(t *testing.T) {
	const base = "/repo"
	f := filterFor(t, base, []string{"vendor/"}, false)
	p := f.ForPattern("**/*.md", base)

	if p.AdmitsFile("/repo/vendor/readme.md") {
		t.Fatal("AdmitsFile under excluded ancestor = true, want false")
	}
	if !p.AdmitsFile("/repo/a/b.md") {
		t.Fatal("AdmitsFile(/repo/a/b.md) = false, want true")
	}

	// A dir-only rule must also cover a pattern rooted inside the excluded
	// directory: the base directory is exempt from the hidden layer but not
	// from the user rules.
	vendorPattern := f.ForPattern("/repo/vendor/**/*.md", "/repo/vendor")
	if vendorPattern.AdmitsFile("/repo/vendor/x.md") {
		t.Fatal("AdmitsFile under pattern rooted in excluded dir = true, want false")
	}
}

func TestFilter_SymlinkSpelling(t *testing.T) {
	const base = "/repo"
	f := filterFor(t, base, nil, false)
	p := f.ForPattern("**/*.md", base)
	// Filtering is by path spelling: a path that goes through a dot
	// directory is filtered even if it is a symlink alias to a visible file.
	if p.AdmitsFile("/repo/.git/secret.md") {
		t.Fatal("AdmitsFile(/repo/.git/secret.md) = true, want false")
	}
	if !p.AdmitsFile("/repo/link.md") {
		t.Fatal("AdmitsFile(/repo/link.md) = false, want true")
	}
}

func TestRulesEmptyEqualRoundTrip(t *testing.T) {
	empty := Rules{}
	if !empty.Empty() {
		t.Fatal("Rules{}.Empty() = false, want true")
	}
	sameA := Rules{Excludes: []string{"a"}}
	sameB := Rules{Excludes: []string{"a"}}
	if !sameA.Equal(sameB) {
		t.Fatal("Equal() = false for identical rules")
	}
	diffA := Rules{Excludes: []string{"a"}}
	diffB := Rules{Excludes: []string{"b"}}
	if diffA.Equal(diffB) {
		t.Fatal("Equal() = true for different rules")
	}
	includeHidden := Rules{IncludeHidden: true}
	if includeHidden.Empty() {
		t.Fatal("Rules{IncludeHidden:true}.Empty() = true, want false")
	}

	rules := Rules{Base: "/repo", Excludes: []string{"vendor/**", "!keep.md"}, IncludeHidden: true}
	f, err := rules.Filter()
	if err != nil {
		t.Fatalf("Filter() unexpected error: %v", err)
	}
	if got := f.Rules(); !got.Equal(rules) {
		t.Fatalf("Rules() round-trip = %+v, want %+v", got, rules)
	}
}

func TestReadLines(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		lines, err := ReadLines(filepath.Join(t.TempDir(), "absent"))
		if err != nil {
			t.Fatalf("ReadLines(missing) error = %v, want nil", err)
		}
		if lines != nil {
			t.Fatalf("ReadLines(missing) = %v, want nil", lines)
		}
	})

	t.Run("BOM and CRLF normalized", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), ".mlignore")
		content := "\ufeffvendor/**\r\n# comment\r\n\r\nkeep.md\r\n"
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		lines, err := ReadLines(path)
		if err != nil {
			t.Fatalf("ReadLines error = %v", err)
		}
		want := []string{"vendor/**", "# comment", "", "keep.md", ""}
		if len(lines) != len(want) {
			t.Fatalf("ReadLines = %q, want %q", lines, want)
		}
		for i := range want {
			if lines[i] != want[i] {
				t.Fatalf("ReadLines[%d] = %q, want %q", i, lines[i], want[i])
			}
		}
	})
}

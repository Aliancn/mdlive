// Package ignore implements ml's file-discovery filters: the patterns given
// with --exclude, the lines read from a .mlignore file, and the default rule
// that hides dot-prefixed files and directories.
package ignore

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// Rules is the wire and persistence form of a discovery filter: the raw,
// unparsed rule lines together with the absolute directory they are anchored
// to. Anchored patterns (those containing a separator) resolve against Base;
// patterns without a separator match the base name of a path at any depth.
type Rules struct {
	// Base is the absolute directory the anchored patterns resolve against,
	// typically the working directory of the ml invocation.
	Base string `json:"base,omitempty"`
	// Excludes holds .mlignore lines and --exclude values in evaluation
	// order; the last matching line decides.
	Excludes []string `json:"excludes,omitempty"`
	// IncludeHidden admits dot-prefixed files and directories when true.
	// The default (false) hides them unless the pattern names the dotted
	// component literally.
	IncludeHidden bool `json:"includeHidden,omitempty"`
}

// Empty reports whether the rules carry no user configuration, in which case
// only the default hidden-path rule applies.
func (r *Rules) Empty() bool {
	return len(r.Excludes) == 0 && !r.IncludeHidden
}

// Equal reports whether the two rule sets are identical.
func (r *Rules) Equal(other Rules) bool {
	return r.Base == other.Base && r.IncludeHidden == other.IncludeHidden && slices.Equal(r.Excludes, other.Excludes)
}

// Filter parses the rule lines and returns an immutable filter. A malformed
// line is reported as an error so that a typo in a hand-written rule fails
// loudly.
func (r *Rules) Filter() (*Filter, error) {
	f := &Filter{base: filepath.ToSlash(filepath.Clean(r.Base)), include: r.IncludeHidden}
	for _, line := range r.Excludes {
		parsed, ok, err := parseLine(line)
		if err != nil {
			return nil, err
		}
		if ok {
			f.rules = append(f.rules, parsed)
			f.lines = append(f.lines, line)
		}
	}
	return f, nil
}

// Filter is an immutable, concurrency-safe set of exclusion rules.
type Filter struct {
	base    string
	lines   []string
	rules   []rule
	include bool
}

// Rules returns the raw form of the filter, for status output and persistence.
func (f *Filter) Rules() Rules {
	return Rules{
		Base:          f.base,
		Excludes:      slices.Clone(f.lines),
		IncludeHidden: f.include,
	}
}

// ForPattern returns the admission matcher for one discovery pattern.
// patternSlash is the pattern in slash form and baseDir is the absolute base
// directory that doublestar.SplitPattern reported for it.
func (f *Filter) ForPattern(patternSlash, baseDir string) *Pattern {
	baseSlash := filepath.ToSlash(filepath.Clean(baseDir))
	return &Pattern{
		filter:  f,
		baseDir: filepath.Clean(baseDir),
		patSegs: patternSegments(patternSlash, baseSlash),
	}
}

// Pattern decides admission for the files and directories one discovery
// pattern covers. It is immutable once built and safe for concurrent use.
type Pattern struct {
	filter  *Filter
	baseDir string
	patSegs []string
}

// AdmitsDir reports whether the directory at absPath may be traversed and
// watched. Callers must only pass directories whose ancestors were
// themselves admitted: the recursive walks prune as they descend, so this
// checks the directory alone.
func (p *Pattern) AdmitsDir(absPath string) bool {
	return p.admits(absPath, true, false)
}

// AdmitsFile reports whether the file at absPath may be added. It re-checks
// every ancestor directory between the pattern base and absPath, so it is
// safe to call on a flat list of matches such as glob results.
func (p *Pattern) AdmitsFile(absPath string) bool {
	return p.admits(absPath, false, true)
}

// AdmitsPath reports whether the path at absPath may be discovered, where
// isDir tells whether it is a directory. Ancestors are checked as in
// AdmitsFile.
func (p *Pattern) AdmitsPath(absPath string, isDir bool) bool {
	return p.admits(absPath, isDir, true)
}

// admits evaluates the hidden layer and the user rules for absPath and, when
// withAncestors is set, for every directory between the pattern base and the
// path. The pattern base itself is exempt from the hidden layer (naming it
// in the pattern is what makes it explicit), but the user rules still apply
// to it as an ancestor so that a dir-only exclude such as "vendor/" also
// covers a pattern rooted inside vendor.
func (p *Pattern) admits(absPath string, isDir, withAncestors bool) bool {
	relToBase, err := filepath.Rel(p.baseDir, absPath)
	if err != nil {
		return true // outside our jurisdiction; no opinion
	}
	if relToBase == "." {
		return true // the pattern names its own base
	}
	under := relToBase != ".." && !strings.HasPrefix(relToBase, ".."+string(filepath.Separator))
	if withAncestors && under {
		for dir := filepath.Dir(absPath); ; dir = filepath.Dir(dir) {
			if dir == p.baseDir {
				if !p.ruleAdmits(dir, true) {
					return false
				}
				break
			}
			if !p.admits(dir, true, false) {
				return false
			}
		}
	}
	if under && !p.hiddenAdmits(relToBase, isDir) {
		return false
	}
	return p.ruleAdmits(absPath, isDir)
}

// hiddenAdmits applies the dot-path rule to the path below the pattern base.
// For directories the pattern may be longer than the path: a directory is
// admitted when its dotted components align with a prefix of the pattern,
// because matching files may live deeper inside it.
func (p *Pattern) hiddenAdmits(relToBase string, isDir bool) bool {
	if p.filter.include {
		return true
	}
	relSegs := strings.Split(filepath.ToSlash(relToBase), "/")
	if !slices.ContainsFunc(relSegs, isDotSegment) {
		return true
	}
	return hiddenAlign(p.patSegs, relSegs, isDir)
}

// ruleAdmits applies the user rules. Anchored patterns resolve against the
// filter base; patterns without a separator match the base name at any
// depth; absolute patterns match the absolute path.
func (p *Pattern) ruleAdmits(absPath string, isDir bool) bool {
	f := p.filter
	absSlash := filepath.ToSlash(absPath)
	relToFilter := ""
	if f.base != "" {
		if rel, err := filepath.Rel(f.base, absPath); err == nil &&
			rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			relToFilter = filepath.ToSlash(rel)
		}
	}
	return !f.excluded(absSlash, relToFilter, isDir)
}

func (f *Filter) excluded(absSlash, relSlash string, isDir bool) bool {
	excluded := false
	for i := range f.rules {
		if f.rules[i].matches(absSlash, relSlash, isDir) {
			excluded = !f.rules[i].negate
		}
	}
	return excluded
}

// ReadLines reads a .mlignore-style file and returns its raw lines, with a
// leading UTF-8 BOM and CRLF line endings removed. A missing file is not an
// error and yields no lines.
func ReadLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	text := strings.TrimPrefix(string(data), "\ufeff")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.Split(text, "\n"), nil
}

// ValidateLine checks a single ignore-rule line without building a Filter,
// so callers can warn about malformed lines in .mlignore instead of failing.
func ValidateLine(line string) error {
	_, _, err := parseLine(line)
	return err
}

// rule is one parsed ignore-rule line.
type rule struct {
	pattern  string // slash form, no leading or trailing separator
	anchored bool   // contains a separator, so it must match from the base
	absolute bool   // started with a filesystem root
	dirOnly  bool   // ended with a separator
	negate   bool   // started with '!'
}

func (r *rule) matches(absSlash, relSlash string, isDir bool) bool {
	if r.dirOnly && !isDir {
		return false
	}
	candidate := absSlash
	switch {
	case r.anchored && !r.absolute:
		if relSlash == "" {
			return false
		}
		candidate = relSlash
	case !r.anchored:
		candidate = path.Base(absSlash)
	}
	matched, err := doublestar.Match(r.pattern, candidate)
	return err == nil && matched
}

// parseLine parses one ignore-rule line. ok is false for blank lines and
// comments, which are skipped rather than errors.
func parseLine(line string) (rule, bool, error) {
	s := strings.Trim(line, " \t")
	if s == "" || strings.HasPrefix(s, "#") {
		return rule{}, false, nil
	}
	var r rule
	// A leading backslash escapes a literal '#' or '!'; it also opts the rest
	// of the line out of the negation check below.
	escaped := false
	if strings.HasPrefix(s, `\#`) || strings.HasPrefix(s, `\!`) {
		s = s[1:]
		escaped = true
	}
	if !escaped && strings.HasPrefix(s, "!") {
		r.negate = true
		s = s[1:]
	}
	if strings.HasSuffix(s, "/") {
		r.dirOnly = true
		s = strings.TrimRight(s, "/")
	}
	if s == "" || s == "." || s == ".." {
		return rule{}, false, fmt.Errorf("invalid ignore pattern %q", line)
	}
	// A leading separator anchors the pattern at the base directory
	// (gitignore semantics). Only Windows-style volume paths such as C:\...,
	// which carry no gitignore anchor meaning, are matched against the
	// absolute filesystem path.
	r.absolute = filepath.VolumeName(s) != ""
	if r.absolute {
		r.anchored = true
		r.pattern = filepath.ToSlash(filepath.Clean(s))
	} else {
		s = strings.TrimPrefix(s, "/")
		r.anchored = strings.Contains(s, "/")
		r.pattern = filepath.ToSlash(filepath.Clean(s))
	}
	if !doublestar.ValidatePattern(r.pattern) {
		return rule{}, false, fmt.Errorf("invalid ignore pattern %q", line)
	}
	return r, true, nil
}

// patternSegments returns the pattern's segments relative to its base
// directory, which is what the hidden layer aligns path components against.
func patternSegments(patternSlash, baseSlash string) []string {
	if baseSlash != "" && baseSlash != "/" && strings.HasPrefix(patternSlash, baseSlash+"/") {
		patternSlash = patternSlash[len(baseSlash)+1:]
	}
	return strings.Split(patternSlash, "/")
}

// hiddenAlign reports whether the dotted components of relSegs can be
// aligned with literal dot segments of the pattern: every dot-prefixed path
// component must line up with a pattern segment that itself starts with a
// dot, and ** may stand in for any number of non-dotted components.
//
// In full mode (files) both the path and the pattern must be consumed. In
// prefix mode (directories) the path must be consumed but the pattern may
// have segments left, because matching files can live deeper inside the
// directory.
func hiddenAlign(patSegs, relSegs []string, prefix bool) bool {
	memo := make(map[[2]int]bool)
	var align func(i, j int) bool
	align = func(i, j int) bool {
		key := [2]int{i, j}
		if v, ok := memo[key]; ok {
			return v
		}
		var result bool
		switch {
		case i == len(patSegs):
			result = j == len(relSegs)
		case patSegs[i] == "**":
			if align(i+1, j) {
				result = true
			} else if j < len(relSegs) && !isDotSegment(relSegs[j]) && align(i, j+1) {
				result = true
			}
		case j == len(relSegs):
			result = prefix
		case isDotSegment(relSegs[j]) && !isDotSegment(patSegs[i]):
			result = false
		default:
			result = align(i+1, j+1)
		}
		memo[key] = result
		return result
	}
	return align(0, 0)
}

func isDotSegment(seg string) bool {
	return seg != "" && seg[0] == '.'
}

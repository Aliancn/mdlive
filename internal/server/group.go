package server

import (
	"fmt"
	"strings"
	"unicode"
)

// DefaultGroup is the name used when no explicit group is specified.
const DefaultGroup = "default"

// normalizeGroupName trims leading and trailing slashes from a group name.
func normalizeGroupName(name string) string {
	return strings.Trim(name, "/")
}

// validateGroupName checks that a group name is safe to use as a URL path.
func validateGroupName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("group name must not be empty")
	}

	for _, r := range name {
		if unicode.IsControl(r) {
			return fmt.Errorf("group name must not contain control characters")
		}
	}

	if strings.ContainsAny(name, "?#\\") {
		return fmt.Errorf("group name must not contain '?', '#', or '\\'")
	}

	if strings.HasPrefix(name, "_/") || name == "_" {
		return fmt.Errorf("group name must not start with '_/' (reserved for internal API)")
	}

	if strings.Contains(name, "//") {
		return fmt.Errorf("group name must not contain consecutive slashes")
	}

	for seg := range strings.SplitSeq(name, "/") {
		if seg == "." || seg == ".." {
			return fmt.Errorf("group name must not contain '.' or '..' path segments")
		}
	}

	return nil
}

// ResolveGroupName normalizes the name, falls back to DefaultGroup when empty,
// and validates non-default names. It is the single entry point that all
// call sites should use for group-name handling.
func ResolveGroupName(name string) (string, error) {
	name = normalizeGroupName(name)
	if name == "" {
		return DefaultGroup, nil
	}
	if name != DefaultGroup {
		if err := validateGroupName(name); err != nil {
			return "", err
		}
	}
	return name, nil
}

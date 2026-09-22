package server

import "testing"

func Test_normalizeGroupName(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"design", "design"},
		{"/design", "design"},
		{"design/", "design"},
		{"/design/", "design"},
		{"api/docs", "api/docs"},
		{"/api/docs/", "api/docs"},
		{"///a///", "a"},
		{"", ""},
		{"/", ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := normalizeGroupName(tt.in)
			if got != tt.want {
				t.Errorf("normalizeGroupName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func Test_validateGroupName(t *testing.T) {
	valid := []string{
		"design",
		"api/docs",
		"a/b/c",
		"my-group",
		"my_group",
		"v1.0",
		"a/b.c/d",
	}
	for _, name := range valid {
		t.Run("valid:"+name, func(t *testing.T) {
			if err := validateGroupName(name); err != nil {
				t.Errorf("validateGroupName(%q) returned error: %v", name, err)
			}
		})
	}

	invalid := []struct {
		name string
		desc string
	}{
		{"", "empty"},
		{"   ", "whitespace only"},
		{"bad?name", "question mark"},
		{"bad#name", "hash"},
		{"bad\\name", "backslash"},
		{"hello\x00world", "null byte"},
		{"hello\nworld", "newline"},
		{"_/internal", "underscore-slash prefix"},
		{"_", "bare underscore"},
		{"a//b", "consecutive slashes"},
		{".", "dot segment"},
		{"..", "dot-dot segment"},
		{"a/./b", "dot in middle"},
		{"a/../b", "dot-dot in middle"},
		{"../a", "dot-dot at start"},
		{"a/..", "dot-dot at end"},
	}
	for _, tt := range invalid {
		t.Run("invalid:"+tt.desc, func(t *testing.T) {
			if err := validateGroupName(tt.name); err == nil {
				t.Errorf("validateGroupName(%q) should return error for %s", tt.name, tt.desc)
			}
		})
	}
}

func TestResolveGroupName(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"design", "design", false},
		{"/design/", "design", false},
		{"api/docs", "api/docs", false},
		{"/api/docs/", "api/docs", false},
		{"", DefaultGroup, false},
		{"/", DefaultGroup, false},
		{"default", DefaultGroup, false},
		{"bad?name", "", true},
		{"a/../b", "", true},
		{"_/internal", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ResolveGroupName(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ResolveGroupName(%q) should return error", tt.in)
				}
				return
			}
			if err != nil {
				t.Errorf("ResolveGroupName(%q) returned error: %v", tt.in, err)
				return
			}
			if got != tt.want {
				t.Errorf("ResolveGroupName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

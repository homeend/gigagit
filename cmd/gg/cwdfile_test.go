package main

import (
	"reflect"
	"testing"
)

func TestExtractCwdFile(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantPath string
		wantRest []string
	}{
		{"absent", []string{"status"}, "", []string{"status"}},
		{"space form", []string{"--cwd-file", "/tmp/x", "status"}, "/tmp/x", []string{"status"}},
		{"equals form", []string{"--cwd-file=/tmp/y"}, "/tmp/y", []string{}},
		{"before subcommand", []string{"--cwd-file", "/tmp/z", "worktree", "add"}, "/tmp/z", []string{"worktree", "add"}},
		{"after subcommand", []string{"status", "--cwd-file", "/tmp/a"}, "/tmp/a", []string{"status"}},
		{"no value is dropped safely", []string{"--cwd-file"}, "", []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path, rest := extractCwdFile(tc.args)
			if path != tc.wantPath {
				t.Errorf("path = %q, want %q", path, tc.wantPath)
			}
			if !reflect.DeepEqual(rest, tc.wantRest) {
				t.Errorf("rest = %v, want %v", rest, tc.wantRest)
			}
		})
	}
}

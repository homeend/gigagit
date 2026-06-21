package main

import (
	"errors"
	"strings"
	"testing"
)

func TestFriendlyGitError(t *testing.T) {
	cases := []struct {
		name       string
		in         string
		wantSubstr string
		noRawNoise bool // the "failed (exit N)" runner noise must be gone
	}{
		{
			name:       "not a git repository",
			in:         "status failed (exit 128): fatal: not a git repository (or any of the parent directories): .git",
			wantSubstr: "not a git repository",
			noRawNoise: true,
		},
		{
			name:       "git missing from PATH",
			in:         `exec: "git": executable file not found in $PATH`,
			wantSubstr: "PATH",
		},
		{
			name:       "dubious ownership",
			in:         "status failed (exit 128): fatal: detected dubious ownership in repository at '/repo'",
			wantSubstr: "safe.directory",
			noRawNoise: true,
		},
		{
			name:       "unknown error keeps git message, drops runner noise",
			in:         "status failed (exit 1): fatal: something unusual went wrong here",
			wantSubstr: "something unusual went wrong here",
			noRawNoise: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := friendlyGitError(errors.New(c.in))
			if !strings.Contains(got, c.wantSubstr) {
				t.Errorf("friendlyGitError(%q) = %q, want substring %q", c.in, got, c.wantSubstr)
			}
			if !strings.HasPrefix(got, "gg: ") {
				t.Errorf("message should be prefixed 'gg: ', got %q", got)
			}
			if c.noRawNoise && strings.Contains(got, "failed (exit") {
				t.Errorf("raw runner noise should be stripped, got %q", got)
			}
		})
	}
}

func TestFriendlyGitErrorNil(t *testing.T) {
	if got := friendlyGitError(nil); got != "" {
		t.Errorf("nil error should map to empty string, got %q", got)
	}
}

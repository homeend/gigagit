package template

import (
	"strings"
	"testing"
	"time"
)

func fixedCtx() Ctx {
	return Ctx{
		ParentBranch: "main",
		Repo:         "aaa",
		Seqs:         map[string]int{"issue": 42},
		Now:          func() time.Time { return time.Date(2026, 6, 11, 14, 5, 9, 0, time.UTC) },
	}
}

func TestResolveSubstitutionTokens(t *testing.T) {
	tests := []struct {
		name   string
		tmpl   string
		inputs map[string]string
		ctx    Ctx
		want   string
	}{
		{"parent and repo", "<repo>/from-<parent-branch>", nil, fixedCtx(), "aaa/from-main"},
		{"date", "d-<date:yyyy-MM-dd HH:mm>", nil, fixedCtx(), "d-2026-06-11 14:05"},
		{"seq padded", "i-<seq:issue:4>", nil, fixedCtx(), "i-0042"},
		{"seq unpadded", "i-<seq:issue>", nil, fixedCtx(), "i-42"},
		// Resolver only substitutes the supplied number; an absent key => 0. The
		// 1-based start ("first worktree gets 1") lives in config.PeekSeq, not here.
		{"seq missing is zero", "i-<seq:unknown:3>", nil, fixedCtx(), "i-000"},
		{"user input", "issue/<user:issue-id>", map[string]string{"issue-id": "777"}, fixedCtx(), "issue/777"},
		{"user reused once", "<user:id>-<user:id>", map[string]string{"id": "x"}, fixedCtx(), "x-x"},
		{"literal passthrough", "no-tokens-here", nil, fixedCtx(), "no-tokens-here"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Resolve(tc.tmpl, tc.inputs, tc.ctx)
			if err != nil {
				t.Fatalf("Resolve(%q) error: %v", tc.tmpl, err)
			}
			if got != tc.want {
				t.Fatalf("Resolve(%q) = %q, want %q", tc.tmpl, got, tc.want)
			}
		})
	}
}

func TestResolveBranchToken(t *testing.T) {
	// <branch> substitutes the sanitized resolved branch, path-template only.
	ctx := fixedCtx()
	ctx.Branch = "issue/123"
	got, err := Resolve("../<repo>.worktrees/<branch>", nil, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "../aaa.worktrees/issue-123" {
		t.Fatalf("got %q, want ../aaa.worktrees/issue-123", got)
	}
}

func TestResolveErrors(t *testing.T) {
	t.Run("unknown token", func(t *testing.T) {
		_, err := Resolve("x-<bogus>", nil, fixedCtx())
		if err == nil || !strings.Contains(err.Error(), "bogus") {
			t.Fatalf("want unknown-token error mentioning the token, got %v", err)
		}
	})
	t.Run("branch without ctx.Branch", func(t *testing.T) {
		_, err := Resolve("p/<branch>", nil, fixedCtx()) // ctx.Branch == ""
		if err == nil || !strings.Contains(err.Error(), "path") {
			t.Fatalf("want path-only error for <branch>, got %v", err)
		}
	})
	t.Run("missing user input", func(t *testing.T) {
		_, err := Resolve("<user:missing>", nil, fixedCtx())
		if err == nil || !strings.Contains(err.Error(), "missing") {
			t.Fatalf("want missing-input error, got %v", err)
		}
	})
	t.Run("bad seq pad", func(t *testing.T) {
		_, err := Resolve("<seq:issue:x>", nil, fixedCtx())
		if err == nil {
			t.Fatalf("want error for non-numeric seq padding")
		}
	})
}

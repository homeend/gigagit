package web

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsLoopbackHost(t *testing.T) {
	t.Parallel()
	cases := []struct {
		host string
		ok   bool
	}{
		{"localhost", true},
		{"127.0.0.1", true},
		{"::1", true},
		{"0.0.0.0", false},
		{"", false},
		{"192.168.1.5", false},
		{"example.com", false},
	}
	for _, c := range cases {
		if got := isLoopbackHost(c.host); got != c.ok {
			t.Errorf("isLoopbackHost(%q) = %v, want %v", c.host, got, c.ok)
		}
	}
}

func TestListenRefusesPublicAddr(t *testing.T) {
	t.Parallel()
	if _, _, err := listen("0.0.0.0:0"); err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("err = %v, want loopback refusal", err)
	}
	if _, _, err := listen(":8080"); err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("err = %v, want loopback refusal for empty host", err)
	}
}

func TestListenDefaultLoopback(t *testing.T) {
	t.Parallel()
	ln, url, err := listen("")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if !strings.HasPrefix(url, "http://127.0.0.1:") {
		t.Fatalf("url = %q, want http://127.0.0.1:<port>", url)
	}
}

func TestServePreflightNonRepo(t *testing.T) {
	t.Parallel()
	err := Serve(context.Background(), t.TempDir(), "127.0.0.1:0", false)
	if err == nil {
		t.Fatal("Serve on a non-repo dir returned nil")
	}
	if !strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("error = %v, want friendly not-a-repo message", err)
	}
}

func TestServePreflightForeignWorktreeLink(t *testing.T) {
	t.Parallel()
	// A worktree link whose gitdir this environment cannot resolve — the
	// WSL↔Windows cross-notation case.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: /nonexistent/repo/.git/worktrees/x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Serve(context.Background(), dir, "127.0.0.1:0", false)
	if err == nil {
		t.Fatal("Serve on a broken worktree link returned nil")
	}
	if !strings.Contains(err.Error(), "linked worktree") || !strings.Contains(err.Error(), "WSL") {
		t.Errorf("error = %v, want the cross-environment worktree hint", err)
	}
}

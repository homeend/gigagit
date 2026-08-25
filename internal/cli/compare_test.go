package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestParseEndpoint(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want model.Endpoint
	}{
		{"@worktree", model.Endpoint{Kind: model.EndpointWorkTree}},
		{"@staged", model.Endpoint{Kind: model.EndpointIndex}},
		{"@index", model.Endpoint{Kind: model.EndpointIndex}},
		{"HEAD~2", model.Endpoint{Kind: model.EndpointCommit, Hash: "HEAD~2"}},
		{"abc123", model.Endpoint{Kind: model.EndpointCommit, Hash: "abc123"}},
	}
	for _, c := range cases {
		if got := parseEndpoint(c.in); got != c.want {
			t.Errorf("parseEndpoint(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestCompareCommitRange(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t) // one commit: README.md, on main
	// second commit adds b.txt
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "c2")

	code, out, errb := runCLI(t, dir, "compare", "HEAD~1", "HEAD")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, errb)
	}
	if !strings.Contains(out, "b.txt") {
		t.Fatalf("compare HEAD~1 HEAD must list b.txt:\n%s", out)
	}
}

func TestCompareDefaultsToWorktree(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("dirtied\n"), 0o644) // unstaged change

	code, out, errb := runCLI(t, dir, "compare", "HEAD")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, errb)
	}
	if !strings.Contains(out, "README.md") {
		t.Fatalf("compare HEAD (vs working tree) must list README.md:\n%s", out)
	}
}

func TestCompareReversePairFriendlyError(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	code, _, errb := runCLI(t, dir, "compare", "@worktree", "HEAD") // reverse order
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if strings.Contains(errb, "DiffTreeFiles") || strings.Contains(errb, "endpoint pair") {
		t.Fatalf("error leaks internals: %s", errb)
	}
	if !strings.Contains(errb, "oldest") {
		t.Fatalf("expected an ordering hint:\n%s", errb)
	}
}

func TestCompareNoArgsUsage(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	code, _, errb := runCLI(t, dir, "compare")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (usage)", code)
	}
	if !strings.Contains(errb, "usage") {
		t.Fatalf("missing usage on stderr:\n%s", errb)
	}
}

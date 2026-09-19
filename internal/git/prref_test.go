package git

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestPRRefRoundTrip(t *testing.T) {
	t.Parallel()
	if PRRef(42) != "refs/gg/pr/42" {
		t.Fatalf("PRRef = %q", PRRef(42))
	}
	for ref, want := range map[string]int{"refs/gg/pr/42": 42, "refs/gg/pr/0": -1, "refs/gg/pr/x": -1,
		"refs/gg/pr/4/2": -1, "refs/gg/pr/007": -1, "refs/heads/42": -1} {
		n, ok := ParsePRRef(ref)
		if (want < 0) == ok || (ok && n != want) {
			t.Errorf("ParsePRRef(%q) = %d,%v", ref, n, ok)
		}
	}
}

func TestFetchRefspecWritesPrivateRef(t *testing.T) {
	t.Parallel()
	run := func(dir string, args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	upDir, _ := newTestRepo(t) // the "base repository"
	run(upDir, "commit", "--allow-empty", "-m", "pr head")
	head := run(upDir, "rev-parse", "HEAD")
	run(upDir, "update-ref", "refs/pull/7/head", head)
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	if err := repo.FetchRefspec(context.Background(), upDir, "refs/pull/7/head", PRRef(7)); err != nil {
		t.Fatal(err)
	}
	if got := run(dir, "rev-parse", PRRef(7)); got != head {
		t.Errorf("ref = %s, want %s", got, head)
	}
	if _, err := exec.Command("git", "-C", dir, "rev-parse", "--verify", "-q", "FETCH_HEAD").Output(); err == nil {
		t.Error("FETCH_HEAD was written; want --no-write-fetch-head")
	}
}

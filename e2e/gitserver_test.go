package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitServerCloneAndPush(t *testing.T) {
	root := t.TempDir()
	origin := filepath.Join(root, "origin")

	g := func(dir string, args ...string) string { return rawGit(t, dir, args...) }
	g(root, "init", "-b", "main", "origin")
	os.WriteFile(filepath.Join(origin, "a.txt"), []byte("v1\n"), 0o644)
	g(origin, "add", "-A")
	g(origin, "commit", "-m", "initial")
	g(origin, "config", "http.receivepack", "true")
	g(origin, "config", "receive.denyCurrentBranch", "ignore")

	srv := startGitServer(t, root)
	url := srv.URL + "/origin"

	g(root, "clone", url, "clone")
	clone := filepath.Join(root, "clone")
	if data, err := os.ReadFile(filepath.Join(clone, "a.txt")); err != nil || string(data) != "v1\n" {
		t.Fatalf("clone over http failed: %q %v", data, err)
	}

	os.WriteFile(filepath.Join(clone, "b.txt"), []byte("new\n"), 0o644)
	g(clone, "add", "-A")
	g(clone, "commit", "-m", "pushed change")
	g(clone, "push", "origin", "main")

	if log := g(origin, "log", "--format=%s", "main"); !strings.Contains(log, "pushed change") {
		t.Fatalf("push over http did not reach origin:\n%s", log)
	}
}

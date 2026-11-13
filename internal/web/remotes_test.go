package web

import (
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

func TestRemotesEndpoint(t *testing.T) {
	t.Parallel()
	_, clone := cloneWithOrigin(t)
	ts := serve(t, New(domain.Open(clone)))

	var got struct {
		Remotes []struct {
			Name   string `json:"name"`
			Remote string `json:"remote"`
			Branch string `json:"branch"`
			Hash   string `json:"hash"`
		} `json:"remotes"`
		Truncated bool `json:"truncated"`
	}
	if code := getJSON(t, ts, "/api/remotes", &got); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	var main *struct {
		Name   string `json:"name"`
		Remote string `json:"remote"`
		Branch string `json:"branch"`
		Hash   string `json:"hash"`
	}
	for i := range got.Remotes {
		if got.Remotes[i].Name == "origin/main" {
			main = &got.Remotes[i]
		}
	}
	if main == nil {
		t.Fatalf("no origin/main in %+v", got.Remotes)
	}
	if main.Remote != "origin" || main.Branch != "main" || main.Hash == "" {
		t.Errorf("origin/main = %+v", *main)
	}
	if got.Truncated {
		t.Error("truncated on a one-remote fixture")
	}
}

// The Remotes section pages: ?limit grows the window past the first 100, and
// truncated stays true only while rows remain (user report: clicking
// "… more" never loaded anything).
func TestRemotesLimitPages(t *testing.T) {
	t.Parallel()
	_, clone := cloneWithOrigin(t)
	head := gitRun(t, clone, "rev-parse", "HEAD")
	var refs strings.Builder
	for i := range 130 {
		fmt.Fprintf(&refs, "create refs/remotes/origin/b%03d %s\n", i, head)
	}
	gitStdin(t, clone, refs.String(), "update-ref", "--stdin")
	ts := serve(t, New(domain.Open(clone)))
	type page struct {
		Remotes   []struct{ Name string } `json:"remotes"`
		Truncated bool                    `json:"truncated"`
	}
	var first, grown, all page
	getJSON(t, ts, "/api/remotes", &first)
	if len(first.Remotes) != 100 || !first.Truncated {
		t.Fatalf("first page: %d rows, truncated=%v; want 100, true", len(first.Remotes), first.Truncated)
	}
	getJSON(t, ts, "/api/remotes?limit=120", &grown)
	if len(grown.Remotes) != 120 || !grown.Truncated {
		t.Fatalf("limit=120: %d rows, truncated=%v; want 120, true", len(grown.Remotes), grown.Truncated)
	}
	getJSON(t, ts, "/api/remotes?limit=200", &all)
	if n := len(all.Remotes); n < 131 || all.Truncated {
		t.Fatalf("limit=200: %d rows, truncated=%v; want every row (131+), false", n, all.Truncated)
	}
}

// gitStdin runs one git command in dir with stdin fed in (gitRun has none).
func gitStdin(t *testing.T, dir, stdin string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(stdin)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

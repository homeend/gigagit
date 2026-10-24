package web

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/repos"
)

// The repo switcher's ctrl+d (the TUI's forget): a registry entry is
// removed; a path the registry does not hold is refused.
func TestReposForget(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	other := newRepoDir(t, 1)
	srv := New(domain.Open(dir))
	srv.reposPath = filepath.Join(t.TempDir(), "repos.toml")
	if err := repos.Touch(srv.reposPath, other, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, srv)
	body, _ := json.Marshal(map[string]string{"path": other})
	if code := postJSON(t, ts, "/api/repos/forget", string(body), "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("forget = %d", code)
	}
	for _, e := range repos.Load(srv.reposPath) {
		if e.Path == other {
			t.Fatal("the forgotten repository is still listed")
		}
	}
	if code := postJSON(t, ts, "/api/repos/forget", `{"path":"/no/such/repo"}`, "application/json", "", nil); code != http.StatusNotFound {
		t.Errorf("forget unknown = %d, want 404", code)
	}
	if code := postJSON(t, ts, "/api/repos/forget", string(body), "application/json", "http://evil.example", nil); code != http.StatusForbidden {
		t.Errorf("cross-origin forget = %d, want 403", code)
	}
}

// Tags' "Refresh remote status": a one-shot listing, answered with how many
// tags the remote has; the rows then carry their ▲ verdict.
func TestRemoteTagsRefresh(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	remote := t.TempDir()
	gitRun(t, remote, "init", "--bare", "-q")
	gitRun(t, dir, "remote", "add", "origin", remote)
	gitRun(t, dir, "tag", "pushed")
	gitRun(t, dir, "tag", "local-only")
	gitRun(t, dir, "push", "-q", "origin", "pushed")
	ts := serve(t, New(domain.Open(dir)))
	var got struct {
		Remote int `json:"remote"`
	}
	if code := postJSON(t, ts, "/api/remote-tags/refresh", `{}`, "application/json", "", &got); code != http.StatusOK || got.Remote != 1 {
		t.Fatalf("refresh = %d %+v, want 200 with 1 remote tag", code, got)
	}
	var tags struct {
		RemoteKnown bool `json:"remote_known"`
	}
	getJSON(t, ts, "/api/tags", &tags)
	if !tags.RemoteKnown {
		t.Error("the tags carry no verdict after the refresh")
	}
	// No remote: an empty listing (domain's contract), not an error.
	noRemote := serve(t, New(domain.Open(newRepoDir(t, 1))))
	got.Remote = -1
	if code := postJSON(t, noRemote, "/api/remote-tags/refresh", `{}`, "application/json", "", &got); code != http.StatusOK || got.Remote != 0 {
		t.Errorf("no remote = %d %+v, want 200 with 0", code, got)
	}
}

// The page side of the F1 rows.
func TestListRowsClientWired(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ file, want, why string }{
		{"palette.js", `forgetRepo(row.path)`, "ctrl+d forgets the switcher's row"},
		{"palette.js", `copyText(row.path, "path " + row.path)`, "ctrl+p copies its path"},
		{"palette.js", `<span>ctrl+p copy path</span><span>ctrl+d forget</span>`, "the hint advertises both"},
		{"palette.js", `{ label: "go to current worktree"`, "the palette finds the current worktree"},
		{"sidebar.js", `{ label: "go to its worktree", act: () => doReroot(wt) }`, "a branch checked out elsewhere jumps there"},
		{"sidebar.js", `{ label: "copy stash ref"`, "a stash copies its ref"},
		{"sidebar.js", `" as a new worktree…"`, "a tag checks out as a new worktree"},
		{"sidebar.js", `"/api/remote-tags/refresh"`, "Refresh remote status lists the remote now"},
		{"sidebar.js", `{ label: "copy absolute path"`, "an entry copies its absolute path"},
		{"prefixes.js", `data-act="copy-prefix"`, "a prefix copies its template"},
	} {
		if !strings.Contains(readStatic(t, c.file), c.want) {
			t.Errorf("%s lacks %q: %s", c.file, c.want, c.why)
		}
	}
}

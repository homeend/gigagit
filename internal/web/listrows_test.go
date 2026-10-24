package web

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
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
		{"sidebar.js", `(e.worktree || state.worktree || "")`, "…in the worktree it came from"},
		{"sidebar.js", `const name = e.path.split("/").pop();`, "copy file name is the file's name, not the entry label"},
		{"sidebar.js", `{ op: "create-worktree", tag: tg.name, name, path }`, "a tag's worktree is cut at the tag by name"},
		{"palette.js", `if ((e.ctrlKey || e.metaKey) && !e.altKey && e.key.toLowerCase() === "p") {`, "ctrl+p never opens print over the palette"},
		{"index.html", "copy stash ref</span>", "the stash help row lists copy stash ref"},
		{"prefixes.js", `data-act="copy-prefix"`, "a prefix copies its template"},
	} {
		if !strings.Contains(readStatic(t, c.file), c.want) {
			t.Errorf("%s lacks %q: %s", c.file, c.want, c.why)
		}
	}
}

// A shelf row names the worktree its file came from: the page's "copy
// absolute path" joins that, never the worktree on screen (the TUI's
// copyFilePrompt(e.Origin.Worktree, …)).
func TestShelfRowCarriesItsWorktree(t *testing.T) {
	t.Parallel()
	r := shelfRowFrom(model.ShelfEntry{ID: "x", Origin: model.FileAddress{Worktree: "/w/a", Path: "f.go", State: model.StateUnstaged}})
	if r.Worktree != "/w/a" {
		t.Fatalf("shelf row worktree = %q, want /w/a", r.Worktree)
	}
}

// A tag checks out as a new worktree by NAME (the TUI's openWorktreeAt(name,
// name)): a tag the repo does not have is refused.
func TestCreateWorktreeAtTag(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	gitRun(t, dir, "tag", "v1")
	ts := serve(t, New(domain.Open(dir)))
	wt := filepath.Join(t.TempDir(), "from-v1")
	opID := startOpBody(t, ts, `{"op":"create-worktree","tag":"v1","name":"from-v1","path":"`+filepath.ToSlash(wt)+`"}`)
	events := readSSE(t, ts, opID, 30*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	if out := gitRun(t, dir, "worktree", "list"); !strings.Contains(out, "from-v1") {
		t.Fatalf("worktrees = %s", out)
	}
	if code := postJSON(t, ts, "/api/op", `{"op":"create-worktree","tag":"nope","name":"x","path":"/tmp/x"}`, "application/json", "", nil); code != http.StatusBadRequest {
		t.Errorf("an unknown tag = %d, want 400", code)
	}
}

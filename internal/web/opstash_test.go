package web

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

type stashesResp struct {
	Stashes []struct {
		Ref     string `json:"ref"`
		Subject string `json:"subject"`
		Sha     string `json:"sha"`
	} `json:"stashes"`
}

// dirtyFile rewrites f.txt so the tree has an unstaged tracked change.
func dirtyFile(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestStashesEndpoint(t *testing.T) {
	dir := newRepoDir(t, 1)
	dirtyFile(t, dir, "one\n")
	gitRun(t, dir, "stash", "push", "-m", "first")
	dirtyFile(t, dir, "two\n")
	gitRun(t, dir, "stash", "push", "-m", "second")
	ts := serve(t, New(domain.Open(dir)))

	var body stashesResp
	if code := getJSON(t, ts, "/api/stashes", &body); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if len(body.Stashes) != 2 {
		t.Fatalf("stashes = %+v", body.Stashes)
	}
	// newest first: stash@{0} is "second"
	if body.Stashes[0].Ref != "stash@{0}" || !strings.Contains(body.Stashes[0].Subject, "second") {
		t.Errorf("row 0 = %+v", body.Stashes[0])
	}
	if want := gitRun(t, dir, "rev-parse", "stash@{0}"); body.Stashes[0].Sha != want {
		t.Errorf("sha = %q, want %q", body.Stashes[0].Sha, want)
	}
}

func TestOpHTTPStashCreate(t *testing.T) {
	dir := newRepoDir(t, 1)
	dirtyFile(t, dir, "wip\n")
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"stash","message":"wip x"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if out := gitRun(t, dir, "status", "--porcelain"); strings.TrimSpace(out) != "" {
		t.Errorf("tree not clean after stash: %q", out)
	}
	var body stashesResp
	getJSON(t, ts, "/api/stashes", &body)
	if len(body.Stashes) != 1 || !strings.Contains(body.Stashes[0].Subject, "wip x") {
		t.Fatalf("stashes after create = %+v", body.Stashes)
	}
}

func TestOpHTTPStashApply(t *testing.T) {
	dir := newRepoDir(t, 1)
	dirtyFile(t, dir, "wip\n")
	gitRun(t, dir, "stash", "push", "-m", "keepme")
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"stash-apply","ref":"stash@{0}"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "f.txt")); string(got) != "wip\n" {
		t.Errorf("f.txt = %q after apply", got)
	}
	var body stashesResp
	getJSON(t, ts, "/api/stashes", &body)
	if len(body.Stashes) != 1 {
		t.Errorf("apply must keep the stash, got %+v", body.Stashes)
	}
}

func TestOpHTTPStashPop(t *testing.T) {
	dir := newRepoDir(t, 1)
	dirtyFile(t, dir, "wip\n")
	gitRun(t, dir, "stash", "push", "-m", "popme")
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"stash-pop","ref":"stash@{0}"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "f.txt")); string(got) != "wip\n" {
		t.Errorf("f.txt = %q after pop", got)
	}
	var body stashesResp
	getJSON(t, ts, "/api/stashes", &body)
	if len(body.Stashes) != 0 {
		t.Errorf("pop must drop the stash, got %+v", body.Stashes)
	}
}

func TestOpHTTPStashDrop(t *testing.T) {
	dir := newRepoDir(t, 1)
	dirtyFile(t, dir, "wip\n")
	gitRun(t, dir, "stash", "push", "-m", "dropme")
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"stash-drop","ref":"stash@{0}"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	var body stashesResp
	getJSON(t, ts, "/api/stashes", &body)
	if len(body.Stashes) != 0 {
		t.Errorf("drop must remove the stash, got %+v", body.Stashes)
	}
	// the working tree stays untouched (still the clean c1 content)
	if got, _ := os.ReadFile(filepath.Join(dir, "f.txt")); string(got) != "content 1\n" {
		t.Errorf("f.txt = %q after drop", got)
	}
}

func TestOpHTTPStashPopConflict(t *testing.T) {
	dir := newRepoDir(t, 1)
	dirtyFile(t, dir, "stashed\n")
	gitRun(t, dir, "stash", "push", "-m", "will conflict")
	dirtyFile(t, dir, "committed\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "conflicting change")
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"stash-pop","ref":"stash@{0}"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != false {
		t.Fatalf("done = %v, want ok=false (pop conflict)", done)
	}
	var st statusResp
	if code := getJSON(t, ts, "/api/status", &st); code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if i, ok := findFile(t, st, "f.txt"); !ok || st.Files[i].Kind != "conflicted" {
		t.Errorf("f.txt not conflicted after pop conflict: %+v", st.Files)
	}
}

func TestOpHTTPStashBadRef(t *testing.T) {
	dir := newRepoDir(t, 1)
	dirtyFile(t, dir, "wip\n")
	gitRun(t, dir, "stash", "push", "-m", "only")
	ts := serve(t, New(domain.Open(dir)))

	if code := postJSON(t, ts, "/api/op", `{"op":"stash-apply"}`, "application/json", "", nil); code != http.StatusBadRequest {
		t.Errorf("empty ref code = %d, want 400", code)
	}
	for _, ref := range []string{"stash@{9}", "--all"} {
		body := `{"op":"stash-apply","ref":"` + ref + `"}`
		if code := postJSON(t, ts, "/api/op", body, "application/json", "", nil); code != http.StatusNotFound {
			t.Errorf("ref %q code = %d, want 404", ref, code)
		}
	}
}

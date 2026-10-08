package web

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// storedPreview is the scope a stored note recorded.
func storedPreview(t *testing.T, srv *Server, id string) string {
	t.Helper()
	n, err := srv.service().NoteGet(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return n.Preview
}

// Spec §3: a note the page writes in PR #7's diff records PR #7.
// Serial: sendServerFull → prFixture isolates XDG state with t.Setenv.
func TestWebPRNoteIsStamped(t *testing.T) {
	ts, _, srv, head, _ := sendServerFull(t)
	id := addWebNote(t, ts, head, "pr7.txt", 1, "for the PR")
	if n, ok := domain.PRScopeNumber(storedPreview(t, srv, id)); !ok || n != 7 {
		t.Fatalf("preview %q", storedPreview(t, srv, id))
	}
}

// A2: a commit outside the PR (its head was force-pushed under the page) is
// refused — the note would otherwise be stored plain and leave the PR.
func TestWebPRNoteOffThePRIsRefused(t *testing.T) {
	ts, _, srv, _, dir := sendServerFull(t)
	parent := strings.TrimSpace(gitRun(t, dir, "rev-parse", "main"))
	code, out := postJSONAny(t, ts, "/api/notes/add",
		fmt.Sprintf(`{"path":"f.txt","rev":%q,"state":"commit","side":"new","line":1,"summary":"x","pr":7}`, parent))
	if code != 409 || !strings.Contains(fmt.Sprint(out["error"]), "#7") {
		t.Fatalf("add = %d %v", code, out)
	}
	if c, err := srv.service().NoteCounts(context.Background()); err == nil && c.ByCommit[parent] != 0 {
		t.Fatal("the refused note was stored")
	}
}

// A1 + ruling A: a PR the page does not know is never read from the forge
// for a note, and the note is refused — stored plain it would leave the PR.
func TestWebNoteForAnUnknownPRReadsNoForge(t *testing.T) {
	ts, wf, srv, head, _ := sendServerFull(t)
	reads := func() int {
		wf.mu.Lock()
		defer wf.mu.Unlock()
		return wf.prReads
	}
	before := reads()
	code, out := postJSONAny(t, ts, "/api/notes/add",
		fmt.Sprintf(`{"path":"pr7.txt","rev":%q,"state":"commit","side":"new","line":1,"summary":"x","pr":99}`, head))
	if code != 409 || !strings.Contains(fmt.Sprint(out["error"]), "#99") {
		t.Fatalf("add = %d %v", code, out)
	}
	if n := reads() - before; n != 0 {
		t.Fatalf("the note add read the forge %d times", n)
	}
	if c, err := srv.service().NoteCounts(context.Background()); err == nil && c.ByCommit[head] != 0 {
		t.Fatal("the refused note was stored")
	}
}

// Ruling A: the PR's diff is gone (gg pr forget elsewhere) — refused.
func TestWebPRNoteWithTheDiffGoneIsRefused(t *testing.T) {
	ts, _, srv, head, dir := sendServerFull(t)
	gitRun(t, dir, "update-ref", "-d", "refs/gg/pr/7")
	code, out := postJSONAny(t, ts, "/api/notes/add",
		fmt.Sprintf(`{"path":"pr7.txt","rev":%q,"state":"commit","side":"new","line":1,"summary":"x","pr":7}`, head))
	if code != 409 || !strings.Contains(fmt.Sprint(out["error"]), "#7") {
		t.Fatalf("add = %d %v", code, out)
	}
	if c, err := srv.service().NoteCounts(context.Background()); err == nil && c.ByCommit[head] != 0 {
		t.Fatal("the refused note was stored")
	}
}

// The page's note add names the PR it was written in.
func TestPageSendsThePRWithANote(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "files.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "pr: (ad.ctx.preview && ad.ctx.preview.pr) || 0,") {
		t.Fatal("files.js's note add does not send the PR number")
	}
}

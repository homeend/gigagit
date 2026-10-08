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

// Review Focus 4: a commit that is not the PR's tip takes no PR stamp.
func TestWebPRNoteOffTheTipIsNotStamped(t *testing.T) {
	ts, _, srv, _, dir := sendServerFull(t)
	parent := strings.TrimSpace(gitRun(t, dir, "rev-parse", "main"))
	code, out := postJSONAny(t, ts, "/api/notes/add",
		fmt.Sprintf(`{"path":"f.txt","rev":%q,"state":"commit","side":"new","line":1,"summary":"x","pr":7}`, parent))
	id, _ := out["id"].(string)
	if code != 200 || id == "" {
		t.Fatalf("add = %d %v", code, out)
	}
	if p := storedPreview(t, srv, id); p != "" {
		t.Fatalf("an off-tip note recorded %q", p)
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

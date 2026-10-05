package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// An agent answers another agent's review over MCP: reply in a remark's
// thread (with a link to the fix), resolve it, read it all back.
func TestNoteReplyAndResolveToolsAnswerAReview(t *testing.T) {
	e := newTestEnv(t)
	gitRun(t, e.dir, "config", "user.name", "t")
	gitRun(t, e.dir, "config", "user.email", "t@t")
	if err := os.WriteFile(filepath.Join(e.dir, "a.txt"), []byte("HELLO\nWORLD\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, e.dir, "commit", "-qam", "upper")
	head := gitRun(t, e.dir, "rev-parse", "HEAD")
	doc := `{"version":1,"summary":"ok","files":[{"path":"a.txt","annotations":[{"newRange":[1,2],"summary":"shouty"}]}]}`
	id, _, err := e.svc.SaveReview(context.Background(), domain.SaveReview{Target: domain.ReviewTarget{Kind: domain.ReviewRange, Range: head + "^.." + head, Commit: head}, Agent: "Claude", Text: doc})
	if err != nil {
		t.Fatal(err)
	}
	remark := "review:" + id + ":0"
	out := e.call(t, "gg_note_reply", map[string]any{"id": remark, "summary": "agreed", "link": "HEAD"})
	note, _ := out["note"].(map[string]any)
	if note["parent_id"] != remark || len(note["link"].(string)) != 40 {
		t.Fatalf("gg_note_reply = %v", out)
	}
	if out := e.call(t, "gg_note_resolve", map[string]any{"id": remark, "resolved": true}); out["ok"] != true {
		t.Fatalf("gg_note_resolve = %v", out)
	}
	shown := e.call(t, "gg_review_show", map[string]any{"link": id})
	raw, _ := json.Marshal(shown)
	if !strings.Contains(string(raw), `"resolved":true`) || !strings.Contains(string(raw), `"summary":"agreed"`) {
		t.Fatalf("gg_review_show = %s", raw)
	}
	if msg := e.callErr(t, "gg_note_resolve", map[string]any{"id": "forge:1", "resolved": true}); !strings.Contains(msg, "GitHub") {
		t.Fatalf("forge resolve: %s", msg)
	}
}

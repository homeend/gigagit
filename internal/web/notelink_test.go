package web

import (
	"strings"
	"testing"
)

// R13 / §7.2 web: a note's link (gg://…?note=<id>) comes from the domain's
// one builder; a reply gets its thread's link with its own id; a gone note
// is 404; a GitHub comment or a shelf note has no link (400).
// Serial: sendServer.
func TestNoteLinkEndpoint(t *testing.T) {
	ts, _, head := sendServer(t)
	id := addWebNote(t, ts, head, "pr7.txt", 1, "here")
	var got struct {
		Link string `json:"link"`
	}
	if code := getJSON(t, ts, "/api/notes/link?id="+id, &got); code != 200 || !strings.HasSuffix(got.Link, "/pr7.txt@main...refs/gg/pr/7:1?note="+id) {
		t.Fatalf("= %d %q", code, got.Link)
	}
	code, rep := postJSONAny(t, ts, "/api/notes/reply", `{"id":"`+id+`","summary":"and"}`)
	rid, _ := rep["id"].(string)
	if code != 200 || rid == "" {
		t.Fatalf("reply = %d %v", code, rep)
	}
	if code := getJSON(t, ts, "/api/notes/link?id="+rid, &got); code != 200 || !strings.HasSuffix(got.Link, "?note="+rid) {
		t.Fatalf("reply link = %d %q", code, got.Link)
	}
	if code := getJSON(t, ts, "/api/notes/link?id=forge:C1", nil); code != 400 {
		t.Errorf("a GitHub comment = %d, want 400", code)
	}
	if code := getJSON(t, ts, "/api/notes/link?id=deadbeef", nil); code != 404 {
		t.Errorf("a gone note = %d, want 404", code)
	}
	if code := getJSON(t, ts, "/api/notes/link", nil); code != 400 {
		t.Errorf("no id = %d, want 400", code)
	}
}

package mcp

import "testing"

func TestNoteAddAndMetaTools(t *testing.T) {
	e := newTestEnv(t)
	seedNoteFile(t, e)
	res := e.call(t, "gg_note_add", map[string]any{"file": "a.txt", "new_line": 1, "summary": "s",
		"meta": map[string]string{"kind": "issue"}, "display": map[string]any{"kind": map[string]any{"show": []string{"badge"}}}})
	note, _ := res["note"].(map[string]any)
	id, _ := note["id"].(string)
	disp, _ := note["display"].(map[string]any)
	if badges, _ := disp["badges"].([]any); len(badges) != 1 {
		t.Fatalf("display = %v", note)
	}
	out := e.call(t, "gg_note_meta", map[string]any{"id": id, "action": "set", "meta": map[string]string{"status": "agreed"}, "author": "MR"})
	if out["ok"] != true {
		t.Fatalf("meta = %v", out)
	}
	if msg := e.callErr(t, "gg_note_meta", map[string]any{"id": id, "action": "flip"}); msg == "" {
		t.Fatal("an unknown action must fail")
	}
}

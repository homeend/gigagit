package web

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

type symEntry struct {
	ID, Kind, Label, Left, Right string
	Symmetric                    *struct{ A, B, Base string }
}

func TestSavedCompareSymmetric(t *testing.T) {
	dir, _, _ := linkRepo(t) // main, feat/x
	gitRun(t, dir, "checkout", "-q", "-b", "feat/y", "main")
	gitRun(t, dir, "commit", "-q", "--allow-empty", "-m", "y")
	gitRun(t, dir, "checkout", "-q", "main")
	srv, svc := linkServer(t, dir)
	ts := serve(t, srv)
	const route = "/api/saved-compares/symmetric"
	body := jsonBody(t, map[string]string{"a": "feat/x", "b": "feat/y", "base": "main"})

	var added struct{ Entry symEntry }
	if code := postDecode(t, ts, route, body, "application/json", &added); code != http.StatusOK {
		t.Fatalf("add: %d", code)
	}
	e := added.Entry
	if e.Kind != "compare" || e.Symmetric == nil || e.Symmetric.A != "feat/x" || e.Symmetric.B != "feat/y" || e.Symmetric.Base != "main" {
		t.Fatalf("entry = %+v", e)
	}
	if !strings.HasSuffix(e.Left, "@main...feat/x") || !strings.HasSuffix(e.Right, "@main...feat/y") {
		t.Fatalf("links = %q | %q", e.Left, e.Right)
	}

	var dup savedResp
	if code := postDecode(t, ts, route, body, "application/json", &dup); code != http.StatusConflict || dup.ID != e.ID {
		t.Fatalf("duplicate: %d %+v, want 409 naming the existing row", code, dup)
	}
	for name, b := range map[string]string{
		"no base":   jsonBody(t, map[string]string{"a": "feat/x", "b": "feat/y"}),
		"base == a": jsonBody(t, map[string]string{"a": "feat/x", "b": "feat/y", "base": "feat/x"}),
		"unknown":   jsonBody(t, map[string]string{"a": "feat/x", "b": "nope", "base": "main"}),
		"not json":  `{`,
	} {
		if code := postDecode(t, ts, route, b, "application/json", nil); code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", name, code)
		}
	}
	if code := postDecode(t, ts, route, body, "text/plain", nil); code != http.StatusUnsupportedMediaType {
		t.Errorf("writeGuard: %d, want 415", code)
	}

	// The list carries the recognised branches too: the page parses no links.
	var list struct{ Entries []symEntry }
	if code := getJSON(t, ts, "/api/saved-compares", &list); code != http.StatusOK || len(list.Entries) != 1 || list.Entries[0].Symmetric == nil {
		t.Fatalf("list: %d %+v", code, list.Entries)
	}
	if cs, _ := svc.SavedCompareList(context.Background()); len(cs) != 1 {
		t.Fatalf("stored %+v", cs)
	}
}

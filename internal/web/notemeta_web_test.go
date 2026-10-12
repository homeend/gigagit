package web

import (
	"bytes"
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/notemeta"
)

// metaReviewDoc declares kind as a filled badge counted per value, and
// audience through `as`; the first remark is placed, the zzz.go one is not.
const metaReviewDoc = `{"version":1,"summary":"s",
 "display":{"kind":{"show":["badge","count"],"values":{"issue":{"color":"red"}}},
            "audience":{"as":"audience","show":["count"]},
            "confidence":{"show":["line"]}},
 "files":[
 {"path":"f.txt","annotations":[{"newRange":[1,1],"summary":"A is unused","meta":{"kind":"issue","audience":"author","confidence":"high"}}]},
 {"path":"zzz.go","annotations":[{"newRange":[1,1],"summary":"elsewhere","meta":{"kind":"issue"}}]}]}`

// The review body carries the header's counts (every remark) and each
// file's counts (placed remarks only — what ◆N counts).
func TestReviewBodyCarriesMetaCounts(t *testing.T) {
	t.Parallel()
	ts, _, _, id := reviewServer(t, metaReviewDoc)
	var got struct {
		Counts     map[string]int             `json:"counts"`
		MetaCounts notemeta.Counts            `json:"meta_counts"`
		MetaByFile map[string]notemeta.Counts `json:"meta_by_file"`
	}
	if code := getJSON(t, ts, "/api/review/"+id, &got); code != http.StatusOK {
		t.Fatalf("GET review = %d", code)
	}
	kind := -1
	for i, g := range got.MetaCounts.Groups {
		if g.Key == "kind" {
			kind = i
		}
	}
	if kind < 0 || got.MetaCounts.Groups[kind].Values[0].N != 2 || got.MetaCounts.Author != 1 {
		t.Fatalf("meta_counts = %+v, want kind issue 2 + 1 for author", got.MetaCounts)
	}
	f := got.MetaByFile["f.txt"]
	if len(f.Groups) != 2 || f.Groups[1].Key != "kind" || f.Groups[1].Values[0].N != 1 || f.Groups[1].Values[0].Color != "red" {
		t.Fatalf("meta_by_file[f.txt] = %+v, want issue 1 red", f)
	}
	if got.Counts["f.txt"] != 1 {
		t.Fatalf("counts = %v, ◆N must keep its meaning", got.Counts)
	}
}

// A remark the view could not place still shows its badges in Other notes.
func TestReviewOtherRowsCarryDisplay(t *testing.T) {
	t.Parallel()
	ts, _, _, id := reviewServer(t, metaReviewDoc)
	var got struct {
		Other []struct {
			Summary string             `json:"summary"`
			Display *notemeta.Resolved `json:"display"`
		} `json:"other"`
	}
	getJSON(t, ts, "/api/review/"+id, &got)
	if len(got.Other) != 1 || got.Other[0].Display == nil || len(got.Other[0].Display.Badges) != 1 ||
		got.Other[0].Display.Badges[0].Value != "issue" {
		t.Fatalf("other = %+v, want the zzz.go remark with its issue badge", got.Other)
	}
}

// The viewer's remark boxes carry the resolved display (lines included).
func TestRemarkRowsCarryDisplay(t *testing.T) {
	t.Parallel()
	_, svc, _, id := reviewServer(t, metaReviewDoc)
	ns, err := svc.ReviewFileNotes(context.Background(), id, "f.txt", []string{"content 2"})
	if err != nil || len(ns) != 1 {
		t.Fatalf("ReviewFileNotes = %v %v", ns, err)
	}
	row := remarkRows(ns)[0]
	if row.Display == nil || len(row.Display.Badges) != 1 || row.Display.Audience != "author" ||
		len(row.Display.Lines) != 1 || row.Display.Lines[0].Value != "high" {
		t.Fatalf("remark row display = %+v", row.Display)
	}
}

// Mark tags a remark with status; clear removes it; a forge thread, a
// free-form status, a missing id and an unknown remark are refused.
func TestWebMarkTagsAndClears(t *testing.T) {
	t.Parallel()
	ts, _, _, id := reviewServer(t, webReviewDoc)
	remark := "review:" + id + ":0"
	status := func() string {
		var got struct {
			Notes []struct {
				Display *notemeta.Resolved `json:"display"`
			} `json:"notes"`
		}
		getJSON(t, ts, "/api/review/notes?id="+id+"&path=f.txt&status=M", &got)
		if len(got.Notes) != 1 || got.Notes[0].Display == nil || len(got.Notes[0].Display.Badges) == 0 {
			return ""
		}
		return got.Notes[0].Display.Badges[0].Value
	}
	if code, out := postJSONAny(t, ts, "/api/notes/mark", `{"id":"`+remark+`","status":"disagreed"}`); code != http.StatusOK {
		t.Fatalf("mark = %d %v", code, out)
	}
	if s := status(); s != "disagreed" {
		t.Fatalf("after mark the remark's badge = %q, want disagreed (the built-in status declaration)", s)
	}
	if code, out := postJSONAny(t, ts, "/api/notes/mark", `{"id":"`+remark+`","status":""}`); code != http.StatusOK {
		t.Fatalf("clear = %d %v", code, out)
	}
	if s := status(); s != "" {
		t.Fatalf("after clear the badge = %q, want none", s)
	}
	for _, c := range []struct {
		body string
		want int
	}{
		{`{"id":"forge:C1","status":"agreed"}`, http.StatusConflict},
		{`{"id":"` + remark + `","status":"lgtm"}`, http.StatusBadRequest},
		{`{"status":"agreed"}`, http.StatusBadRequest},
		{`{"id":"review:` + id + `:9","status":"agreed"}`, http.StatusNotFound},
	} {
		if code, out := postJSONAny(t, ts, "/api/notes/mark", c.body); code != c.want {
			t.Errorf("mark %s = %d %v, want %d", c.body, code, out, c.want)
		}
	}
}

// newerNotesServer serves a repo with one stored note whose notes file a
// newer gg wrote (format above this build's): readable, never written.
func newerNotesServer(t *testing.T) (*httptest.Server, *domain.Service, string) {
	t.Helper()
	dir := t.TempDir()
	svc := domain.Open(newRepoDir(t, 2))
	svc.UseNotesDir(dir)
	srv := New(svc)
	srv.reposPath = filepath.Join(t.TempDir(), "repos.toml") // the prompt store for a dismissal
	ts := serve(t, srv)
	code, out := postJSONAny(t, ts, "/api/notes/add",
		`{"path":"f.txt","rev":"","state":"unstaged","side":"new","line":1,"summary":"tighten this"}`)
	id, _ := out["id"].(string)
	if code != http.StatusOK || id == "" {
		t.Fatalf("add = %d %v", code, out)
	}
	n := 0
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".toml") {
			return nil
		}
		b, _ := os.ReadFile(p)
		if nb := regexp.MustCompile(`(?m)^format = 3$`).ReplaceAll(b, []byte("format = 4")); !bytes.Equal(nb, b) {
			os.WriteFile(p, nb, 0o644)
			n++
		}
		return nil
	})
	if n == 0 {
		t.Fatal("no notes file carried format = 3")
	}
	return ts, svc, id
}

// A notes file a newer gg wrote refuses the mark with the store's word.
func TestWebMarkRefusesANewerNotesFile(t *testing.T) {
	t.Parallel()
	ts, _, id := newerNotesServer(t)
	if code, out := postJSONAny(t, ts, "/api/notes/mark", `{"id":"`+id+`","status":"agreed"}`); code != http.StatusConflict {
		t.Fatalf("mark on a newer notes file = %d %v, want 409", code, out)
	}
}

// A notes file a newer gg wrote: the centre says note-edits is unavailable
// (the TUI's own notice id) and /api/preflight reports the feature off, so
// the page hides Mark.
func TestNoteEditsUnavailableNotice(t *testing.T) {
	t.Parallel()
	ts, _, _ := newerNotesServer(t)
	var got noticesResp
	if code := getJSON(t, ts, "/api/notifications", &got); code != http.StatusOK {
		t.Fatalf("notifications = %d", code)
	}
	var found *noticeWire
	for i := range got.Notices {
		if got.Notices[i].ID == "feature_disabled_note-edits" {
			found = &got.Notices[i]
		}
	}
	if found == nil || found.Title != "note-edits is unavailable in this repository" || len(found.Detail) == 0 {
		t.Fatalf("notices = %+v, want the note-edits notice", got.Notices)
	}
	var pf struct {
		Disabled []string `json:"disabled"`
	}
	getJSON(t, ts, "/api/preflight", &pf)
	if !slices.Contains(pf.Disabled, "note-edits") {
		t.Fatalf("disabled = %v, want note-edits", pf.Disabled)
	}
	if code, out := postJSONAny(t, ts, "/api/notifications/dismiss", `{"id":"feature_disabled_note-edits"}`); code != http.StatusOK {
		t.Fatalf("dismiss = %d %v", code, out)
	}
	got = noticesResp{}
	getJSON(t, ts, "/api/notifications", &got)
	for _, n := range got.Notices {
		if n.ID == "feature_disabled_note-edits" {
			t.Fatalf("a dismissed notice came back: %+v", n)
		}
	}
}

package web

import (
	"net/http"
	"strings"
	"testing"
)

type answeredNote struct {
	ID         string `json:"id"`
	Summary    string `json:"summary"`
	ReadOnly   bool   `json:"read_only"`
	Replyable  bool   `json:"replyable"`
	Resolved   bool   `json:"resolved"`
	ResolvedBy string `json:"resolved_by"`
	Link       string `json:"link"`
	Replies    []struct {
		Summary string `json:"summary"`
		Link    string `json:"link"`
	} `json:"replies"`
}

func reviewNotesOf(t *testing.T, ts interface{ Close() }, get func(string, any) int, id string) answeredNote {
	t.Helper()
	var got struct {
		Notes []answeredNote `json:"notes"`
	}
	if code := get("/api/review/notes?id="+id+"&path=f.txt&status=M", &got); code != http.StatusOK || len(got.Notes) != 1 {
		t.Fatalf("GET /api/review/notes = %d %+v", code, got)
	}
	return got.Notes[0]
}

// The page answers a review: a reply (with a link) on a remark, then
// Resolve; refusals carry their own status.
func TestWebRepliesToAndResolvesARemark(t *testing.T) {
	t.Parallel()
	ts, _, _, id := reviewServer(t, webReviewDoc)
	get := func(p string, out any) int { return getJSON(t, ts, p, out) }
	remark := "review:" + id + ":0"
	if n := reviewNotesOf(t, ts, get, id); !n.ReadOnly || !n.Replyable {
		t.Fatalf("a remark is read-only yet replyable: %+v", n)
	}
	if code, out := postJSONAny(t, ts, "/api/notes/reply", `{"id":"`+remark+`","summary":"agreed","link":"HEAD"}`); code != http.StatusOK {
		t.Fatalf("reply = %d %v", code, out)
	}
	if code, out := postJSONAny(t, ts, "/api/notes/resolve", `{"id":"`+remark+`","resolved":true}`); code != http.StatusOK {
		t.Fatalf("resolve = %d %v", code, out)
	}
	n := reviewNotesOf(t, ts, get, id)
	if !n.Resolved || n.ResolvedBy == "" || len(n.Replies) != 1 || len(n.Replies[0].Link) != 40 {
		t.Fatalf("answered remark = %+v", n)
	}
	for _, c := range []struct {
		path, body string
		want       int
	}{
		{"/api/notes/resolve", `{"id":"forge:1","resolved":true}`, http.StatusConflict},
		{"/api/notes/resolve", `{"id":"` + remark + `","resolved":false}`, http.StatusOK},
		{"/api/notes/resolve", `{"id":"` + remark + `","resolved":false}`, http.StatusConflict},
		{"/api/notes/resolve", `{"id":"` + remark + `"}`, http.StatusBadRequest},
		{"/api/notes/reply", `{"id":"review:` + id + `:9","summary":"x"}`, http.StatusNotFound},
		{"/api/notes/reply", `{"id":"` + remark + `","summary":"x","link":"no-such-rev"}`, http.StatusBadRequest},
	} {
		if code, out := postJSONAny(t, ts, c.path, c.body); code != c.want {
			t.Errorf("POST %s %s = %d %v, want %d", c.path, c.body, code, out, c.want)
		}
	}
}

// Review rows on the wire carry the tally.
func TestReviewHeadsCarryTheTally(t *testing.T) {
	t.Parallel()
	ts, _, _, id := reviewServer(t, webReviewDoc)
	if code, _ := postJSONAny(t, ts, "/api/notes/resolve", `{"id":"review:`+id+`:1","resolved":true}`); code != http.StatusOK {
		t.Fatalf("resolve = %d", code)
	}
	var got struct {
		Reviews []struct {
			Remarks  int `json:"remarks"`
			Resolved int `json:"resolved"`
		} `json:"reviews"`
	}
	if code := getJSON(t, ts, "/api/notes/counts", &got); code != http.StatusOK || len(got.Reviews) != 1 {
		t.Fatalf("counts = %d %+v", code, got)
	}
	if r := got.Reviews[0]; r.Remarks != 2 || r.Resolved != 1 {
		t.Fatalf("tally = %+v", r)
	}
}

// The page's side, pinned at the source: a remark takes Reply (replyable),
// every non-forge thread offers Resolve, links open, rows carry the tally.
func TestReviewAnswersJSPins(t *testing.T) {
	t.Parallel()
	files, reviews, allnotes, notebox := readStatic(t, "files.js"), readStatic(t, "reviews.js"), readStatic(t, "allnotes.js"), readStatic(t, "notebox.js")
	for _, c := range []struct{ src, want, why string }{
		{files, `if (!n.read_only || n.replyable) {`, "a review remark takes replies"},
		{files, `"/api/notes/resolve"`, "Resolve / Reopen writes through the resolve endpoint"},
		{files, `if (n.source !== "forge" && !String(n.id).startsWith("forge:"))`, "a forge thread is resolved on GitHub"},
		{files, `const nl = e.target.closest(".notelink[data-link]");`, "a note's link line opens"},
		{reviews, `function tallyText(r) {`, "review rows carry the tally"},
		{reviews, `return withTally("└ " + reviewWords(r), r);`, "the commit's review row carries the tally"},
		{allnotes, `" resolved" : "";`, "View all notes carries the tally"},
		{notebox, `(n.resolved ? " · resolved" : "")`, "a stored resolved thread's title says so"},
	} {
		if !strings.Contains(c.src, c.want) {
			t.Errorf("%s — missing %q", c.why, c.want)
		}
	}
	for _, g := range []string{"✓", "✗", "✔"} {
		if strings.Contains(reviews, g) || strings.Contains(allnotes, g) {
			t.Errorf("a tally must be plain words (user ruling): found %q", g)
		}
	}
}

// The review view's header counts the resolved remarks (the TUI's meta line).
func TestReviewPayloadCountsResolved(t *testing.T) {
	t.Parallel()
	ts, _, _, id := reviewServer(t, webReviewDoc)
	if code, _ := postJSONAny(t, ts, "/api/notes/resolve", `{"id":"review:`+id+`:0","resolved":true}`); code != http.StatusOK {
		t.Fatalf("resolve = %d", code)
	}
	var got struct {
		Resolved int `json:"resolved"`
	}
	if code := getJSON(t, ts, "/api/review/"+id, &got); code != http.StatusOK || got.Resolved != 1 {
		t.Fatalf("GET /api/review/%s = %d %+v", id, code, got)
	}
}

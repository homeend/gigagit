package web

import (
	"context"
	"net/http"
	"testing"

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

package web

import (
	"net/http"
	"net/url"
	"testing"
)

type reviewDiffWire struct {
	Rows []struct {
		Kind  string `json:"kind"`
		Left  string `json:"left"`
		Right string `json:"right"`
	} `json:"rows"`
	Changed    bool   `json:"changed"`
	Status     string `json:"status"`
	LeftLabel  string `json:"left_label"`
	RightLabel string `json:"right_label"`
	Remarks    []struct {
		remarkWire
		Side string `json:"side"`
	} `json:"remarks"`
}

// Spec 2026-10-11-overview-split-diff §8: the review lane answers the
// review's change to one file, its side labels and the remarks on BOTH
// sides — the one on the deleted line marked old.
func TestReviewDiffLaneAnswersTheChangeAndBothSidesRemarks(t *testing.T) {
	t.Parallel()
	doc := `{"version":1,"summary":"s","files":[{"path":"f.txt","annotations":[` +
		`{"newRange":[1,1],"summary":"kept"},{"oldRange":[1,1],"summary":"on a deleted line"}]}]}`
	ts, _, sha, id := reviewServer(t, doc)
	var got reviewDiffWire
	q := "/api/diff?review=" + url.QueryEscape(id) + "&path=f.txt"
	if code := getJSON(t, ts, q, &got); code != http.StatusOK {
		t.Fatalf("GET %s = %d", q, code)
	}
	if !got.Changed || got.Status != "M" || len(got.Rows) != 1 || got.Rows[0].Kind != "change" || got.Rows[0].Left != "content 1" || got.Rows[0].Right != "content 2" {
		t.Fatalf("diff = %+v", got)
	}
	if got.RightLabel != sha[:7] || len(got.LeftLabel) != 7 || got.LeftLabel == got.RightLabel {
		t.Fatalf("labels %q → %q", got.LeftLabel, got.RightLabel)
	}
	sides := map[string]string{}
	for _, r := range got.Remarks {
		if !r.ReadOnly {
			t.Errorf("remark %+v is not read-only", r)
		}
		sides[r.Summary] = r.Side
	}
	if len(got.Remarks) != 2 || sides["kept"] != "" || sides["on a deleted line"] != "old" {
		t.Fatalf("remarks = %+v", got.Remarks)
	}
}

// A file the review leaves alone answers changed=false and no rows; a gone
// review is a 404 (the page says "the review is gone"); a bad path a 400.
func TestReviewDiffLaneRefusals(t *testing.T) {
	t.Parallel()
	ts, _, _, id := reviewServer(t, `{"version":1,"summary":"s","files":[]}`)
	var got reviewDiffWire
	if code := getJSON(t, ts, "/api/diff?review="+url.QueryEscape(id)+"&path=other.txt", &got); code != http.StatusOK || got.Changed || len(got.Rows) != 0 {
		t.Fatalf("unchanged file = %d %+v", code, got)
	}
	for q, want := range map[string]int{
		"/api/diff?review=nope&path=f.txt":                     http.StatusNotFound,
		"/api/diff?review=" + url.QueryEscape(id) + "&path=-x": http.StatusBadRequest,
		"/api/diff?review=" + url.QueryEscape(id):              http.StatusBadRequest,
	} {
		if code := getJSON(t, ts, q, nil); code != want {
			t.Errorf("GET %s = %d, want %d", q, code, want)
		}
	}
}

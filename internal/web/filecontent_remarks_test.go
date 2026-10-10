package web

import (
	"net/http"
	"net/url"
	"testing"
)

type remarkWire struct {
	ID        string `json:"id"`
	Start     int    `json:"start"`
	End       int    `json:"end"`
	Summary   string `json:"summary"`
	Rationale string `json:"rationale"`
	Author    string `json:"author"`
	ReadOnly  bool   `json:"readonly"`
}

type contentWithRemarks struct {
	Lines []struct {
		Text string `json:"text"`
	} `json:"lines"`
	Remarks []remarkWire `json:"remarks"`
}

// Spec 2026-10-11 §6: a file a stored overview opened carries its review's
// remarks, placed on the file at the reviewed tip — new-side only, so a
// remark on a line the commit deleted is left out (W6).
func TestFileContentCarriesReviewRemarks(t *testing.T) {
	t.Parallel()
	doc := `{"version":1,"summary":"s","files":[{"path":"f.txt","annotations":[` +
		`{"newRange":[1,1],"summary":"kept","rationale":"why"},{"oldRange":[1,1],"summary":"on a deleted line"}]}]}`
	ts, _, sha, id := reviewServer(t, doc)
	var got contentWithRemarks
	q := "/api/file-content?src=commit&rev=" + sha + "&path=f.txt&review=" + url.QueryEscape(id)
	if code := getJSON(t, ts, q, &got); code != http.StatusOK {
		t.Fatalf("GET %s = %d", q, code)
	}
	if len(got.Lines) != 1 || got.Lines[0].Text != "content 2" {
		t.Fatalf("lines = %+v, want the tip's one line", got.Lines)
	}
	if len(got.Remarks) != 1 {
		t.Fatalf("remarks = %+v, want only the kept one", got.Remarks)
	}
	r := got.Remarks[0]
	if r.Summary != "kept" || r.Rationale != "why" || r.Start != 1 || r.End != 1 || r.Author != "Claude" || !r.ReadOnly || r.ID == "" {
		t.Errorf("remark = %+v", r)
	}
}

// Without review= the body is unchanged; an unknown review answers the file
// with no remarks (the viewer still shows it).
func TestFileContentRemarksNeedAKnownReview(t *testing.T) {
	t.Parallel()
	doc := `{"version":1,"summary":"s","files":[{"path":"f.txt","annotations":[{"newRange":[1,1],"summary":"kept"}]}]}`
	ts, _, sha, _ := reviewServer(t, doc)
	for _, q := range []string{
		"/api/file-content?src=commit&rev=" + sha + "&path=f.txt",
		"/api/file-content?src=commit&rev=" + sha + "&path=f.txt&review=nope",
	} {
		var got contentWithRemarks
		if code := getJSON(t, ts, q, &got); code != http.StatusOK {
			t.Fatalf("GET %s = %d", q, code)
		}
		if len(got.Lines) != 1 || len(got.Remarks) != 0 {
			t.Errorf("GET %s = %+v, want the file and no remarks", q, got)
		}
	}
}

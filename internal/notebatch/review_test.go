package notebatch

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestParseReviewFullDocument(t *testing.T) {
	doc, err := ParseReview([]byte(`{"version":1,"summary":"## Overview\nok","meta":{"verdict":"approve"},
	 "files":[{"path":"a/b.go","summary":"one line","annotations":[
	  {"newRange":[3,4],"summary":"S","rationale":"R","meta":{"severity":"bug","confidence":"high"}},
	  {"oldRange":[7,7],"summary":"gone"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Summary != "## Overview\nok" || len(doc.Files) != 1 || len(doc.Files[0].Notes) != 2 {
		t.Fatalf("%+v", doc)
	}
	if doc.Files[0].Summary != "one line" {
		t.Fatalf("file summary %q", doc.Files[0].Summary)
	}
	n := doc.Files[0].Notes[0]
	if n.Side != "new" || n.Range != [2]int{3, 4} || n.Summary != "S" || n.Rationale != "R" {
		t.Fatalf("%+v", n)
	}
	if got := n.Meta; len(got) != 2 || got[0] != (MetaKV{"confidence", "high"}) || got[1] != (MetaKV{"severity", "bug"}) {
		t.Fatalf("meta %+v (want sorted by key)", got)
	}
	if doc.Files[0].Notes[1].Side != "old" {
		t.Fatal("oldRange must be the old side")
	}
	if len(doc.Meta) != 1 || doc.Meta[0] != (MetaKV{"verdict", "approve"}) {
		t.Fatalf("%+v", doc.Meta)
	}
	if notes, files := doc.NoteCount(); notes != 2 || files != 1 {
		t.Fatalf("NoteCount = %d, %d", notes, files)
	}
}

func TestParseReviewFoldsOldTagsAndConfidence(t *testing.T) {
	doc, err := ParseReview([]byte(`{"version":1,"summary":"x","files":[{"path":"a","annotations":[{"newRange":[1,1],"summary":"s","tags":["bug","nit"],"confidence":"low"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	m := doc.Files[0].Notes[0].Meta
	if len(m) != 2 || m[0] != (MetaKV{"confidence", "low"}) || m[1] != (MetaKV{"tags", "bug, nit"}) {
		t.Fatalf("%+v", m)
	}
}

func TestParseReviewMetaKeyWinsOverFoldedField(t *testing.T) {
	doc, err := ParseReview([]byte(`{"version":1,"summary":"x","files":[{"path":"a","annotations":[{"newRange":[1,1],"summary":"s","confidence":"low","meta":{"confidence":"high"}}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if m := doc.Files[0].Notes[0].Meta; len(m) != 1 || m[0] != (MetaKV{"confidence", "high"}) {
		t.Fatalf("%+v", m)
	}
}

func TestParseReviewMetaValues(t *testing.T) {
	doc, err := ParseReview([]byte(`{"version":1,"summary":"x","meta":{"n":3,"ok":true,"o":{"a":1}}}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []MetaKV{{"n", "3"}, {"o", `{"a":1}`}, {"ok", "true"}}
	if !reflect.DeepEqual(doc.Meta, want) {
		t.Fatalf("%+v", doc.Meta)
	}
}

func TestParseReviewRejects(t *testing.T) {
	for name, in := range map[string]string{
		"prose":       "The advisor confirms four findings.",
		"empty":       "",
		"no summary":  `{"version":1,"files":[]}`,
		"bad version": `{"version":2,"summary":"x"}`,
		"both ranges": `{"version":1,"summary":"x","files":[{"path":"a","annotations":[{"newRange":[1,1],"oldRange":[1,1],"summary":"s"}]}]}`,
		"no range":    `{"version":1,"summary":"x","files":[{"path":"a","annotations":[{"summary":"s"}]}]}`,
		"empty path":  `{"version":1,"summary":"x","files":[{"path":"","annotations":[]}]}`,
		"bad range":   `{"version":1,"summary":"x","files":[{"path":"a","annotations":[{"newRange":[5,2],"summary":"s"}]}]}`,
		"zero range":  `{"version":1,"summary":"x","files":[{"path":"a","annotations":[{"newRange":[0,2],"summary":"s"}]}]}`,
		"no note":     `{"version":1,"summary":"x","files":[{"path":"a","annotations":[{"newRange":[1,2],"summary":" "}]}]}`,
	} {
		if _, err := ParseReview([]byte(in)); !errors.Is(err, ErrNotReviewDoc) {
			t.Errorf("%s: err %v, want ErrNotReviewDoc", name, err)
		}
	}
}

func TestParseReviewUnwrapsFenceAndEnvelope(t *testing.T) {
	body := `{"version":1,"summary":"ok"}`
	for name, in := range map[string]string{
		"fence":               "Here it is:\n```json\n" + body + "\n```\n",
		"bare":                "```\n" + body + "\n```",
		"envelope":            `{"type":"result","result":` + strconv.Quote(body) + `}`,
		"envelope with fence": `{"type":"result","result":` + strconv.Quote("```json\n"+body+"\n```") + `}`,
	} {
		if doc, err := ParseReview([]byte(in)); err != nil || doc.Summary != "ok" {
			t.Errorf("%s: %+v %v", name, doc, err)
		}
	}
}

func TestParseReviewCanonicalRoundTrips(t *testing.T) {
	in := `{"version":1,"summary":"o","meta":{"verdict":"ok"},"files":[{"path":"a","summary":"fs","meta":{"risk":"low"},"annotations":[{"newRange":[1,2],"summary":"s","rationale":"r","meta":{"k":"v"}},{"oldRange":[4,4],"summary":"t"}]}]}`
	doc, err := ParseReview([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseReview(doc.Canonical())
	if err != nil || !reflect.DeepEqual(doc, back) {
		t.Fatalf("%v\n%+v\n%+v", err, doc, back)
	}
}

func TestReviewDocOverviewRoundTrips(t *testing.T) {
	in := `{"version":1,"summary":"s","overview":"The result.\n\n[the parser](a.go:3-4)","files":[]}`
	doc, err := ParseReview([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Overview != "The result.\n\n[the parser](a.go:3-4)" {
		t.Fatalf("overview = %q", doc.Overview)
	}
	again, err := ParseReview(doc.Canonical())
	if err != nil {
		t.Fatal(err)
	}
	if again.Overview != doc.Overview {
		t.Fatalf("Canonical dropped the overview: %q", again.Overview)
	}
}

// Without the key nothing changes: the canonical bytes are what they were.
func TestReviewDocWithoutOverviewIsUnchanged(t *testing.T) {
	doc, err := ParseReview([]byte(`{"version":1,"summary":"s","files":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Overview != "" {
		t.Fatalf("overview = %q, want none", doc.Overview)
	}
	if strings.Contains(string(doc.Canonical()), "overview") {
		t.Fatalf("Canonical wrote an empty overview:\n%s", doc.Canonical())
	}
}

func TestReviewDocOverviewTooLongIsRefused(t *testing.T) {
	big := strings.Repeat("x", MaxOverviewBytes+1)
	_, err := ParseReview([]byte(`{"version":1,"summary":"s","overview":"` + big + `","files":[]}`))
	if !errors.Is(err, ErrNotReviewDoc) || !strings.Contains(err.Error(), "overview exceeds 64 KiB") {
		t.Fatalf("err = %v", err)
	}
}

package web

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// An overview row carries the files its anchors use, in first-named order,
// each with the first anchor naming it: a note anchor by its note's file,
// a gone note and a repeated path adding nothing (spec
// 2026-10-19-documents-tree §6). gg session files stays flat.
func TestPageListGivesAnOverviewRowItsChildPaths(t *testing.T) {
	t.Parallel()
	s, root := noteSrv(t)
	wt := s.service().Root()
	for _, p := range []string{"a.txt", "b.txt", "c.txt"} {
		if err := os.WriteFile(filepath.Join(wt, p), []byte("x\ny\nz\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.docs.AddNote(root, "c.txt", []string{"x", "y", "z"}, 1, 1, "s", "", "")
	if err != nil {
		t.Fatal(err)
	}
	o, err := s.docs.AddOverview(root, wt, "Tour",
		"[a](a.txt:1) [b](b.txt:2) [c](note:"+n.ID+") [a again](a.txt:3) [gone](note:t999)")
	if err != nil {
		t.Fatal(err)
	}
	s.followDocs()
	var ov *ofRow
	for _, r := range s.pageList(wt) {
		if r.ID == o.ID {
			ov = &r
		} else if len(r.Children) != 0 {
			t.Fatalf("a %s row has children %+v", r.Source, r.Children)
		}
	}
	if ov == nil {
		t.Fatal("the overview is not listed")
	}
	want := []ofChild{{"a.txt", 0}, {"b.txt", 1}, {"c.txt", 2}}
	if !reflect.DeepEqual(ov.Children, want) {
		t.Fatalf("children = %+v, want %+v", ov.Children, want)
	}
	b, _ := json.Marshal(s.pageList(wt))
	if !strings.Contains(string(b), `"children":[{"path":"a.txt","anchor":0}`) || !strings.Contains(string(b), `"id":"`+o.ID+`"`) {
		t.Fatalf("wire = %s", b)
	}
	sb, _ := json.Marshal(s.ofList(wt))
	if strings.Contains(string(sb), "children") {
		t.Fatalf("the steer rows carry children: %s", sb)
	}
}

// enter on an overview's file opens the overview at the first anchor that
// names the file in the overview as fetched (it may have changed since the
// list), else the list's index when still in range, else none.
func TestAnchorForPathJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "viewer.js", voPureStart, voPureEnd, `
const as = [{dest: "a.txt:1", path: "a.txt"}, {dest: "note:n1", note: "n1", path: "c.txt"}, {dest: "a.txt:3", path: "a.txt"}, {dest: "b.txt", path: "b.txt"}];
console.log([anchorForPath(as, "a.txt", 0), anchorForPath(as, "c.txt", 2), anchorForPath(as, "b.txt", 0),
  anchorForPath(as, "z.txt", 2), anchorForPath(as, "z.txt", 9), anchorForPath([], "a.txt", 0)].join(","));
`)
	if out != "0,1,3,2,-1,-1" {
		t.Fatalf("got %s", out)
	}
}

// The switcher's child opens through openViewer's anchor option, and a
// split already showing that overview re-steers to the picked anchor
// (Review Focus 5) — the wiring is DOM; this pins it.
func TestOpenAtAnchorIsWiredJS(t *testing.T) {
	t.Parallel()
	vb, _ := os.ReadFile(filepath.Join("static", "viewer.js"))
	ob, _ := os.ReadFile(filepath.Join("static", "openfiles.js"))
	for _, want := range []string{"anchor = null", "anchorForPath(", "{ pick: pick >= 0 }", "if (same && pick) steerSplit()"} {
		if !strings.Contains(string(vb), want) {
			t.Errorf("viewer.js lacks %q", want)
		}
	}
	if !strings.Contains(string(ob), "anchor: { path: n.path, index: n.anchor }") {
		t.Error("openfiles.js: a child does not open its overview at its anchor")
	}
}

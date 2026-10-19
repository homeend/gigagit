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

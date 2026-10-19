package tui

import (
	"strings"
	"testing"
)

// treeModel is a worktree with one session overview naming a.txt:1,
// b.txt:2, the note on c.txt, a.txt:3 again and a gone note; a noted d.txt
// no overview names; and e.txt opened at a commit.
func treeModel(t *testing.T) (Model, *openFile) {
	t.Helper()
	m := privateDocsModel(t)
	wt := fileSource{kind: srcWorktree}
	c := bgDoc(m, wt, "c.txt", 5)
	n, err := c.addNote(2, 2, "on c", "", "")
	if err != nil {
		t.Fatal(err)
	}
	d := bgDoc(m, wt, "d.txt", 5)
	if _, err := d.addNote(1, 1, "on d", "", ""); err != nil {
		t.Fatal(err)
	}
	bgDoc(m, fileSource{kind: srcCommit, rev: "1a2b3c4d5e6f"}, "e.txt", 3)
	ov := storeOverview(t, m, "Tour", "[a](a.txt:1) [b](b.txt:2) [c](note:"+n.ID+") [a again](a.txt:3) [gone](note:t999)")
	rows, inner := m.viewerGeom()
	ov.layOut(rows, m.overviewWidth(inner)) // as an overview joining the list is
	m = m.registerDoc(ov)
	return m, ov
}

func nodeKinds(ns []docNode) []docNodeKind {
	out := make([]docNodeKind, len(ns))
	for i, n := range ns {
		out[i] = n.kind
	}
	return out
}

func TestDocTreeCollapsedByDefault(t *testing.T) {
	t.Parallel()
	m, ov := treeModel(t)
	ns := m.docTree("")
	if got := nodeKinds(ns); len(got) != 3 || got[0] != nodeOverview || got[1] != nodeGroup || got[2] != nodeGroup {
		t.Fatalf("kinds = %v", got)
	}
	if ns[0].doc != ov || ns[0].expanded {
		t.Fatalf("overview node = %+v", ns[0])
	}
	if ns[1].group != groupNotes || ns[1].count != 1 || ns[2].group != groupOpened || ns[2].count != 1 {
		t.Fatalf("groups = %+v / %+v", ns[1], ns[2])
	}
}

func TestDocTreeOverviewChildrenInAnchorOrder(t *testing.T) {
	t.Parallel()
	m, ov := treeModel(t)
	m.openFiles.setExpanded(m.currentWorktree, docNode{kind: nodeOverview, doc: ov}.expandKey(), true)
	ns := m.docTree("")
	var paths []string
	var anchors []int
	for _, n := range ns {
		if n.kind == nodeChild {
			paths, anchors = append(paths, n.path), append(anchors, n.anchor)
			if n.ov != ov {
				t.Fatalf("child %s belongs to %v", n.path, n.ov)
			}
		}
	}
	if strings.Join(paths, ",") != "a.txt,b.txt,c.txt" || anchors[0] != 0 || anchors[1] != 1 || anchors[2] != 2 {
		t.Fatalf("children = %v anchors = %v", paths, anchors)
	}
	for _, n := range ns {
		if n.kind == nodeChild && n.path == "c.txt" && n.doc == nil {
			t.Fatal("c.txt is open: its child row carries the document")
		}
		if n.kind == nodeChild && n.path == "a.txt" && n.doc != nil {
			t.Fatal("a.txt is not open: no document")
		}
	}
}

// A noted file an overview names lives under the overview only; Notes keeps
// the rest; a file named by two overviews shows under both.
func TestDocTreeClaimsAndSharedChildren(t *testing.T) {
	t.Parallel()
	m, ov := treeModel(t)
	ov2 := storeOverview(t, m, "Other", "[c](c.txt:1)")
	m = m.registerDoc(ov2)
	for _, o := range []*openFile{ov, ov2} {
		m.openFiles.setExpanded(m.currentWorktree, docNode{kind: nodeOverview, doc: o}.expandKey(), true)
	}
	m.openFiles.setExpanded(m.currentWorktree, docNode{kind: nodeGroup, group: groupNotes}.expandKey(), true)
	under := map[string]int{}
	var notes []string
	for _, n := range m.docTree("") {
		switch {
		case n.kind == nodeChild && n.path == "c.txt":
			under[n.ov.title]++
		case n.kind == nodeGroupFile && n.group == groupNotes:
			notes = append(notes, n.doc.path)
		}
	}
	if under["Tour"] != 1 || under["Other"] != 1 {
		t.Fatalf("c.txt under %v, want both overviews once", under)
	}
	if strings.Join(notes, ",") != "d.txt" {
		t.Fatalf("Notes = %v, want d.txt only", notes)
	}
}

// A stored overview's children open at its commit.
func TestDocTreeStoredOverviewChildCarriesTheCommit(t *testing.T) {
	t.Parallel()
	m := privateDocsModel(t)
	ov := storeOverview(t, m, "Stored", "[x](x.go:1)")
	ov.src.kind, ov.ov.tip = srcCommitOverview, "abcdef0123456"
	m = m.registerDoc(ov)
	m.openFiles.setExpanded(m.currentWorktree, docNode{kind: nodeOverview, doc: ov}.expandKey(), true)
	ns := m.docTree("")
	if len(ns) != 2 || ns[1].kind != nodeChild || ns[1].path != "x.go" || ns[1].rev != "abcdef0123456" {
		t.Fatalf("nodes = %+v", ns)
	}
}

// The filter opens what it matches for itself only: the remembered state
// stays collapsed.
func TestDocTreeFilterExpandsForItselfOnly(t *testing.T) {
	t.Parallel()
	m, ov := treeModel(t)
	ns := m.docTree("b.tx")
	if len(ns) != 2 || ns[0].doc != ov || !ns[0].expanded || ns[1].path != "b.txt" {
		t.Fatalf("filtered = %+v", ns)
	}
	if m.openFiles.expanded(m.currentWorktree, docNode{kind: nodeOverview, doc: ov}.expandKey()) {
		t.Fatal("the filter saved its expansion")
	}
	ns = m.docTree("e.tx")
	if len(ns) != 2 || ns[0].kind != nodeGroup || ns[0].group != groupOpened || ns[1].doc == nil || ns[1].doc.path != "e.txt" {
		t.Fatalf("filtered = %+v", ns)
	}
	if ns = m.docTree("zzz"); len(ns) != 0 {
		t.Fatalf("no match = %+v", ns)
	}
}

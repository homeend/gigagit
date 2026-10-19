package tui

import (
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/i18n"
)

// The ctrl+\ "Overviews & notes" tab is a tree (spec
// 2026-10-19-documents-tree): every overview on top, the files it uses
// underneath; then the Notes group (noted files no overview names); then
// the Opened group (every other document). docTree flattens it into the
// rows the popup draws; what is expanded lives on openFilesReg.

type docNodeKind int

const (
	nodeOverview  docNodeKind = iota // an overview document
	nodeChild                        // a file an overview uses
	nodeGroup                        // the Notes / Opened group row
	nodeGroupFile                    // a document under a group
)

const (
	groupNotes  = "notes"
	groupOpened = "opened"
)

// docNode is one row of the tree.
type docNode struct {
	kind     docNodeKind
	doc      *openFile // the overview, a group's document, an open child; nil: a group row, a child not open
	ov       *openFile // a child's overview
	path     string    // a child's path
	anchor   int       // a child's first anchor index naming path
	rev      string    // a child of a stored overview: the commit its anchors open at
	group    string    // a group row's / group file's group
	count    int       // a group row's document count
	expanded bool      // an overview's / a group row's
}

// expandKey is what an overview's or a group's expand state is kept under.
func (n docNode) expandKey() string {
	if n.kind == nodeGroup {
		return "group:" + n.group
	}
	return "doc:" + n.doc.key()
}

// id is the node's identity across rebuilds (the cursor keeps it).
func (n docNode) id() string {
	switch n.kind {
	case nodeChild:
		return "child:" + n.ov.key() + "\x00" + n.path
	case nodeGroup:
		return "group:" + n.group
	case nodeGroupFile:
		return "file:" + n.doc.key()
	}
	return "doc:" + n.doc.key()
}

// parentID is the id of the row a child or a group file sits under ("" at
// the top level).
func (n docNode) parentID() string {
	switch n.kind {
	case nodeChild:
		return "doc:" + n.ov.key()
	case nodeGroupFile:
		return "group:" + n.group
	}
	return ""
}

// overviewAnchors is d's anchors as written: the laid-out ones when the
// overview was laid out, else parsed from its text (the same order).
func overviewAnchors(d *openFile) []agentdocs.Anchor {
	if len(d.ov.anchors) > 0 {
		out := make([]agentdocs.Anchor, len(d.ov.anchors))
		for i, a := range d.ov.anchors {
			out[i] = a.target
		}
		return out
	}
	_, as := agentdocs.ParseOverview(d.ov.text)
	return as
}

// overviewChildren is the files overview ov uses: one per distinct path its
// anchors name — a path anchor's, a note anchor's note's (a gone note names
// nothing) — in the order first named, each with the document it is open
// as (nil when it is not open).
func (m Model) overviewChildren(ov *openFile) []docNode {
	src, rev := fileSource{kind: srcWorktree}, ""
	if ov.src.kind.storedOverview() && ov.ov.tip != "" {
		src, rev = fileSource{kind: srcCommit, rev: ov.ov.tip}, ov.ov.tip
	}
	seen := map[string]bool{}
	var out []docNode
	for i, a := range overviewAnchors(ov) {
		path := a.Path
		if a.Note != "" {
			path = ""
			if m.docs != nil && ov.src.kind == srcOverview {
				if n, ok := m.docs.FindNoteIn(ov.root, a.Note); ok {
					path = n.Path
				}
			}
		}
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, docNode{kind: nodeChild, ov: ov, path: path, anchor: i, rev: rev,
			doc: m.openFiles.find(m.currentWorktree, docKey(src, path))})
	}
	return out
}

// docTree is the tab's rows for query ("" = no filter): every overview with
// its children when expanded, then the Notes and Opened groups (a group
// with nothing in it is not drawn). A filter shows what matches — an
// overview with all its children, a child under its overview, a document
// under its group — expanded for the filter only.
func (m Model) docTree(query string) []docNode {
	wt := m.currentWorktree
	list := m.openFiles.list(wt)
	q := strings.ToLower(query)
	match := func(s string) bool { return q == "" || strings.Contains(strings.ToLower(s), q) }
	now := time.Time{}
	var out []docNode
	claimed := map[*openFile]bool{}
	for _, d := range list {
		if d.ov == nil {
			continue
		}
		node := docNode{kind: nodeOverview, doc: d}
		kids := m.overviewChildren(d)
		for _, k := range kids {
			if k.doc != nil {
				claimed[k.doc] = true
			}
		}
		node.expanded = m.openFiles.expanded(wt, node.expandKey())
		if q != "" {
			if !match(openFileRowText(d, false, now)) {
				var hits []docNode
				for _, k := range kids {
					if match(k.path) {
						hits = append(hits, k)
					}
				}
				if len(hits) == 0 {
					continue
				}
				kids = hits
			}
			node.expanded = true
		}
		out = append(out, node)
		if node.expanded {
			out = append(out, kids...)
		}
	}
	var notes, opened []*openFile
	for _, d := range list {
		switch {
		case d.ov != nil || claimed[d]:
		case d.src.kind == srcWorktree && d.hasNotes():
			notes = append(notes, d)
		default:
			opened = append(opened, d)
		}
	}
	for _, g := range []struct {
		name string
		docs []*openFile
	}{{groupNotes, notes}, {groupOpened, opened}} {
		docs := g.docs
		if q != "" {
			var hits []*openFile
			for _, d := range docs {
				if match(openFileRowText(d, false, now)) {
					hits = append(hits, d)
				}
			}
			docs = hits
		}
		if len(docs) == 0 {
			continue
		}
		node := docNode{kind: nodeGroup, group: g.name, count: len(docs)}
		node.expanded = q != "" || m.openFiles.expanded(wt, node.expandKey())
		out = append(out, node)
		if node.expanded {
			for _, d := range docs {
				out = append(out, docNode{kind: nodeGroupFile, doc: d, group: g.name})
			}
		}
	}
	return out
}

// groupLabel is a group's name as drawn.
func groupLabel(group string) string {
	if group == groupNotes {
		return i18n.T("Notes")
	}
	return i18n.T("Opened")
}

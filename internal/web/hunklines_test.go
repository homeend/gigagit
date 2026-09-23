package web

import (
	"testing"

	"github.com/homeend/gigagit/internal/hunkpick"
	"github.com/homeend/gigagit/internal/textdiff"
)

// A block's lines are pickable one by one (the TUI's picker does it; the web
// could not, because a row never said WHICH line of its block it is). The tags
// must mirror hunkpick.FromDiff exactly: Current takes the left of Changed/Del
// rows in order, Incoming the right of Changed/Add rows.
func TestHunkTagsCarryPerSideLineIndexes(t *testing.T) {
	t.Parallel()
	left := []byte("a\nold1\nold2\nb\nc\nold3\n")
	right := []byte("a\nnew1\nb\nc\nnew3\nnew4\n")

	rows := textdiff.Compare(left, right, textdiff.Options{}).Rows
	tags, count := diffHunkTags(rows)
	doc := hunkpick.FromDiff(left, right)
	blocks := doc.Blocks()
	if count != len(blocks) {
		t.Fatalf("fixture: %d tagged hunks vs %d blocks", count, len(blocks))
	}

	for i, r := range rows {
		if tags.hunk[i] < 0 {
			if tags.index[i] >= 0 || tags.work[i] >= 0 {
				t.Fatalf("row %d is context yet carries line indexes %d/%d", i, tags.index[i], tags.work[i])
			}
			continue
		}
		b := blocks[tags.hunk[i]]
		if r.Kind == textdiff.Changed || r.Kind == textdiff.Del {
			li := tags.index[i]
			if li < 0 || li >= len(b.Current) || b.Current[li] != r.Left {
				t.Fatalf("row %d: index line %d does not name %q in block %d (%q)", i, li, r.Left, tags.hunk[i], b.Current)
			}
		} else if tags.index[i] >= 0 {
			t.Fatalf("row %d has no index side yet carries index line %d", i, tags.index[i])
		}
		if r.Kind == textdiff.Changed || r.Kind == textdiff.Add {
			ri := tags.work[i]
			if ri < 0 || ri >= len(b.Incoming) || b.Incoming[ri] != r.Right {
				t.Fatalf("row %d: work line %d does not name %q in block %d (%q)", i, ri, r.Right, tags.hunk[i], b.Incoming)
			}
		} else if tags.work[i] >= 0 {
			t.Fatalf("row %d has no working side yet carries work line %d", i, tags.work[i])
		}
	}
}

// The endpoint takes picks the way the TUI's picker holds them: a block can
// contribute individual lines from either side. The old shape (a bare list of
// block ordinals = "that block's working side") keeps working.
func TestStageHunksAcceptsPerLinePicks(t *testing.T) {
	t.Parallel()
	var req stageHunksRequest
	if err := decodeStagePicks(&req, []byte(`{"path":"f.txt","hash":"h","picks":[2,4]}`)); err != nil {
		t.Fatalf("the old shape must still parse: %v", err)
	}
	if len(req.Blocks) != 2 || req.Blocks[0].Block != 2 || req.Blocks[1].Block != 4 {
		t.Fatalf("old shape parsed as %+v", req.Blocks)
	}
	if req.Blocks[0].Work != nil {
		t.Fatal("a bare ordinal means the WHOLE working side, which is nil lines + wholeWork")
	}
	if !req.Blocks[0].WholeWork {
		t.Fatal("a bare ordinal must be marked as a whole-side take")
	}

	req = stageHunksRequest{}
	if err := decodeStagePicks(&req, []byte(`{"path":"f.txt","hash":"h","picks":[{"block":1,"work":[0,2],"index":[1]}]}`)); err != nil {
		t.Fatalf("the per-line shape must parse: %v", err)
	}
	if len(req.Blocks) != 1 || req.Blocks[0].Block != 1 {
		t.Fatalf("per-line shape parsed as %+v", req.Blocks)
	}
	if got := req.Blocks[0].Work; len(got) != 2 || got[0] != 0 || got[1] != 2 {
		t.Fatalf("work lines = %v, want [0 2]", got)
	}
	if got := req.Blocks[0].Index; len(got) != 1 || got[0] != 1 {
		t.Fatalf("index lines = %v, want [1]", got)
	}
	if req.Blocks[0].WholeWork {
		t.Fatal("a per-line pick is not a whole-side take")
	}
}

// Per-line picks really stage only those lines: the end-to-end check, through
// the same engine the TUI picker drives.
func TestPerLinePicksStageOnlyThoseLines(t *testing.T) {
	t.Parallel()
	left := []byte("keep\nold1\nold2\ntail\n")
	right := []byte("keep\nnew1\nnew2\ntail\n")
	doc := hunkpick.FromDiff(left, right)
	doc.SetAll(hunkpick.TakeCurrent)
	b := doc.Blocks()[0]
	// take the first changed line's WORKING side, leave the second on the index
	b.Mode = hunkpick.LineByLine
	b.Picks = []hunkpick.Pick{{Side: hunkpick.Incoming, Line: 0}, {Side: hunkpick.Current, Line: 1}}
	got, ok := doc.Resolved()
	if !ok {
		t.Fatal("the doc must resolve with explicit picks")
	}
	if string(got) != "keep\nnew1\nold2\ntail\n" {
		t.Fatalf("staged content = %q, want the first line new and the second old", got)
	}
}

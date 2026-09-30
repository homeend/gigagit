package markdown

import (
	"reflect"
	"strings"
	"testing"
)

// anchors walks a doc in document order and returns its InAnchor nodes.
func anchors(d Doc) []Inline {
	var out []Inline
	var inl func([]Inline)
	inl = func(in []Inline) {
		for _, n := range in {
			if n.Kind == InAnchor {
				out = append(out, n)
			}
			inl(n.In)
		}
	}
	var blk func([]Block)
	blk = func(bs []Block) {
		for _, b := range bs {
			inl(b.Inline)
			blk(b.Blocks)
			for _, it := range b.Items {
				blk(it.Blocks)
			}
			for _, c := range b.Head {
				inl(c)
			}
			for _, r := range b.Rows {
				for _, c := range r {
					inl(c)
				}
			}
		}
	}
	blk(d.Blocks)
	return out
}

func acceptGoAndNotes(d string) bool {
	return strings.HasSuffix(d, ".go") || strings.HasPrefix(d, "note:")
}

func TestParseNeverEmitsAnchors(t *testing.T) {
	t.Parallel()
	d := Parse("[a](x.go:3)")
	if got := anchors(d); len(got) != 0 {
		t.Fatalf("Parse emitted anchors: %+v", got)
	}
	if got := flatText(d.Blocks[0].Inline); got != "a (x.go:3)" {
		t.Fatalf("text = %q, want today's label + dim destination", got)
	}
}

func TestParseWithKeepsAcceptedDestinations(t *testing.T) {
	t.Parallel()
	d := ParseWith("see [the loader](a/b.go) and [n](note:t7)", Options{Anchor: acceptGoAndNotes})
	got := anchors(d)
	if len(got) != 2 {
		t.Fatalf("anchors = %+v, want 2", got)
	}
	if got[0].URL != "a/b.go" || flatText(got[0].In) != "the loader" || got[1].URL != "note:t7" || flatText(got[1].In) != "n" {
		t.Fatalf("anchors = %+v", got)
	}
}

func TestParseWithEmptyLabelShowsTheDestination(t *testing.T) {
	t.Parallel()
	got := anchors(ParseWith("[](a.go)", Options{Anchor: acceptGoAndNotes}))
	if len(got) != 1 || flatText(got[0].In) != "a.go" {
		t.Fatalf("anchors = %+v, want the destination as the label", got)
	}
}

func TestParseWithLeavesHTTPAlone(t *testing.T) {
	t.Parallel()
	calls := 0
	d := ParseWith("[w](https://x.y/a.go)", Options{Anchor: func(string) bool { calls++; return true }})
	if calls != 0 || len(anchors(d)) != 0 || d.Blocks[0].Inline[0].Kind != InLink {
		t.Fatalf("calls=%d doc=%+v, want an ordinary link", calls, d)
	}
}

func TestParseWithRefusedKeepsTodaysText(t *testing.T) {
	t.Parallel()
	src := "x [a](b.txt) **y** [c](d.go)"
	got := ParseWith(src, Options{Anchor: func(string) bool { return false }})
	if !reflect.DeepEqual(got, Parse(src)) {
		t.Fatalf("refusing everything changed the tree:\n%+v\n%+v", got, Parse(src))
	}
}

func TestParseWithReachesTablesListsQuotes(t *testing.T) {
	t.Parallel()
	src := "| h |\n|---|\n| [t](t.go) |\n\n- [l](l.go)\n\n> [q](q.go)\n"
	got := anchors(ParseWith(src, Options{Anchor: acceptGoAndNotes}))
	if len(got) != 3 || got[0].URL != "t.go" || got[1].URL != "l.go" || got[2].URL != "q.go" {
		t.Fatalf("anchors = %+v, want t.go, l.go, q.go", got)
	}
}

func TestParseWithDangerousSchemeNeverAsked(t *testing.T) {
	t.Parallel()
	calls := 0
	d := ParseWith("[x](javascript:alert(1).go)", Options{Anchor: func(string) bool { calls++; return true }})
	if calls != 0 || len(anchors(d)) != 0 {
		t.Fatalf("calls=%d anchors=%+v, want the scheme dropped unasked", calls, anchors(d))
	}
}

package hunkpick

import (
	"bytes"
	"testing"
)

func TestFromDiffSplitsLiteralAndBlocks(t *testing.T) {
	// left (index) vs right (working tree): line 2 changed, a line appended.
	left := []byte("a\nb\nc\n")
	right := []byte("a\nB\nc\nd\n")
	d := FromDiff(left, right)
	if !d.FinalNewline {
		t.Fatal("FinalNewline should follow the right side")
	}
	// Default is undecided until the caller sets modes; take-incoming = the
	// working-tree content reproduced exactly.
	d.SetAll(TakeIncoming)
	out, ok := d.Resolved()
	if !ok || string(out) != "a\nB\nc\nd\n" {
		t.Fatalf("take-incoming = %q ok=%v, want the working tree", out, ok)
	}
	// take-current (index) = the original index content reproduced exactly.
	d.SetAll(TakeCurrent)
	out, _ = d.Resolved()
	if string(out) != "a\nb\nc\n" {
		t.Fatalf("take-current = %q, want the index", out)
	}
	if len(d.Blocks()) == 0 {
		t.Fatal("a changed file must yield at least one block")
	}
}

func TestFromDiffNoChangeHasNoBlocks(t *testing.T) {
	d := FromDiff([]byte("x\ny\n"), []byte("x\ny\n"))
	if len(d.Blocks()) != 0 {
		t.Fatalf("identical sides → 0 blocks, got %d", len(d.Blocks()))
	}
}

func TestFromDiffAllAdd(t *testing.T) {
	// empty index (untracked-like) → one block, all incoming.
	d := FromDiff(nil, []byte("new1\nnew2\n"))
	bs := d.Blocks()
	if len(bs) != 1 || len(bs[0].Current) != 0 || len(bs[0].Incoming) != 2 {
		t.Fatalf("all-add block wrong: %+v", bs)
	}
}

func TestFromDiffCRLFRoundTrip(t *testing.T) {
	left := []byte("a\r\nb\r\nc\r\n")
	right := []byte("a\r\nB\r\nc\r\nd\r\n")
	d := FromDiff(left, right)
	d.SetAll(TakeIncoming)
	got, ok := d.Resolved()
	if !ok || !bytes.Equal(got, right) {
		t.Fatalf("TakeIncoming = %q ok=%v, want %q", got, ok, right)
	}
	d.SetAll(TakeCurrent)
	got, ok = d.Resolved()
	if !ok || !bytes.Equal(got, left) {
		t.Fatalf("TakeCurrent = %q ok=%v, want %q", got, ok, left)
	}
}

func TestFromDiffMixedEOLStaysLF(t *testing.T) {
	// mixed endings: dominant-EOL detection must NOT fire; today's
	// normalize-to-LF behavior is kept and documented
	left := []byte("a\nb\r\nc\n")
	right := []byte("a\nB\r\nc\n")
	d := FromDiff(left, right)
	d.SetAll(TakeIncoming)
	got, ok := d.Resolved()
	want := []byte("a\nB\nc\n")
	if !ok || !bytes.Equal(got, want) {
		t.Fatalf("mixed = %q ok=%v, want %q", got, ok, want)
	}
}

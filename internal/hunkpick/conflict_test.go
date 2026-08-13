package hunkpick

import (
	"strings"
	"testing"
)

func TestParseConflictTwoWay(t *testing.T) {
	src := "top\n<<<<<<< HEAD\nfoo\nlog\n=======\nbar\nlog\n>>>>>>> feature\nend\n"
	d, err := ParseConflict([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if !d.FinalNewline {
		t.Fatal("FinalNewline should be true")
	}
	bs := d.Blocks()
	if len(bs) != 1 {
		t.Fatalf("got %d blocks, want 1", len(bs))
	}
	if len(bs[0].Current) != 2 || bs[0].Current[0] != "foo" || bs[0].Current[1] != "log" {
		t.Fatalf("current = %v", bs[0].Current)
	}
	if len(bs[0].Incoming) != 2 || bs[0].Incoming[0] != "bar" {
		t.Fatalf("incoming = %v", bs[0].Incoming)
	}
	// literal passthrough preserved: take current → top/foo/log/end
	d.SetAll(TakeCurrent)
	out, _ := d.Resolved()
	if string(out) != "top\nfoo\nlog\nend\n" {
		t.Fatalf("resolved = %q", out)
	}
}

func TestParseConflictDiff3SkipsBase(t *testing.T) {
	src := "<<<<<<< HEAD\nours\n||||||| base\nbasetext\n=======\ntheirs\n>>>>>>> x\n"
	d, err := ParseConflict([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	b := d.Blocks()[0]
	if len(b.Current) != 1 || b.Current[0] != "ours" {
		t.Fatalf("current = %v", b.Current)
	}
	if len(b.Incoming) != 1 || b.Incoming[0] != "theirs" {
		t.Fatalf("incoming = %v (base must be skipped)", b.Incoming)
	}
}

func TestParseConflictMalformed(t *testing.T) {
	// unterminated region
	if _, err := ParseConflict([]byte("<<<<<<< x\nours\n=======\ntheirs\n")); err == nil {
		t.Fatal("missing >>>>>>> should error")
	}
	// separator with no start
	if _, err := ParseConflict([]byte("=======\n")); err == nil {
		t.Fatal("stray ======= should error")
	}
}

func TestParseConflictNoFinalNewline(t *testing.T) {
	d, err := ParseConflict([]byte("plain line"))
	if err != nil {
		t.Fatal(err)
	}
	if d.FinalNewline {
		t.Fatal("FinalNewline should be false")
	}
	if len(d.Blocks()) != 0 {
		t.Fatal("no markers → no blocks")
	}
}

func TestParseConflictSizedNestedMarkersAsContent(t *testing.T) {
	src := "top\n" +
		strings.Repeat("<", 31) + " current\n" +
		"<<<<<<< HEAD\n" +
		"=======\n" +
		">>>>>>> old (v2)\n" +
		strings.Repeat("=", 31) + "\n" +
		"theirs\n" +
		strings.Repeat(">", 31) + " incoming\n"
	d, err := ParseConflictSized([]byte(src), 31)
	if err != nil {
		t.Fatal(err)
	}
	bs := d.Blocks()
	if len(bs) != 1 {
		t.Fatalf("blocks = %d, want 1", len(bs))
	}
	if strings.Join(bs[0].Current, ",") != "<<<<<<< HEAD,=======,>>>>>>> old (v2)" {
		t.Fatalf("current = %v — old 7-char markers must be plain content", bs[0].Current)
	}
	if strings.Join(bs[0].Incoming, ",") != "theirs" {
		t.Fatalf("incoming = %v", bs[0].Incoming)
	}
}

func TestParseConflictSizedClampsToSeven(t *testing.T) {
	src := "<<<<<<< HEAD\nx\n=======\ny\n>>>>>>> b\n"
	d, err := ParseConflictSized([]byte(src), 0)
	if err != nil || len(d.Blocks()) != 1 {
		t.Fatalf("size<7 must clamp to git's default: err=%v", err)
	}
}

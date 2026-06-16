package clipboard

import (
	"strings"
	"testing"
)

func TestSequencePlain(t *testing.T) {
	// base64("hello") == "aGVsbG8=", default clipboard buffer 'c', BEL terminator.
	got := Sequence("hello", NoMux)
	want := "\x1b]52;c;aGVsbG8=\x07"
	if got != want {
		t.Errorf("Sequence(plain) = %q, want %q", got, want)
	}
}

func TestSequenceTmuxWrapped(t *testing.T) {
	got := Sequence("hello", Tmux)
	if !strings.HasPrefix(got, "\x1bPtmux;") {
		t.Errorf("tmux sequence must start with the tmux DCS passthrough, got %q", got)
	}
	if !strings.HasSuffix(got, "\x1b\\") {
		t.Errorf("tmux sequence must end with ST (\\x1b\\\\), got %q", got)
	}
}

// countWriter records how many Write calls it received.
type countWriter struct {
	n   int
	buf strings.Builder
}

func (c *countWriter) Write(p []byte) (int, error) {
	c.n++
	return c.buf.Write(p)
}

func TestCopyWritesSequenceInOneWrite(t *testing.T) {
	var w countWriter
	if err := Copy(&w, "hi"); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if w.n != 1 {
		t.Errorf("Copy made %d Write calls, want exactly 1 (contiguous OSC 52)", w.n)
	}
	if got, want := w.buf.String(), Sequence("hi", detectMux()); got != want {
		t.Errorf("Copy wrote %q, want %q", got, want)
	}
}

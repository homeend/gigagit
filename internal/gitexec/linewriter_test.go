package gitexec

import (
	"reflect"
	"testing"
)

func TestLineWriterSplitsAndNormalizes(t *testing.T) {
	var got []string
	w := &lineWriter{onLine: func(s string) { got = append(got, s) }}

	// Two complete lines in one write; second uses CRLF (the '\r' must be stripped).
	if _, err := w.Write([]byte("alpha\nbeta\r\n")); err != nil {
		t.Fatal(err)
	}
	// A line split across two writes emits only once the '\n' arrives.
	if _, err := w.Write([]byte("gam")); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("after partial write, want 2 lines, got %v", got)
	}
	if _, err := w.Write([]byte("ma\n")); err != nil {
		t.Fatal(err)
	}
	// A final line with no trailing '\n' is emitted only by flush().
	if _, err := w.Write([]byte("delta")); err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("before flush, want 3 lines, got %v", got)
	}
	w.flush()

	want := []string{"alpha", "beta", "gamma", "delta"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("onLine lines = %v, want %v", got, want)
	}
	// text() is the normalized stdout: every emitted line + '\n' (CR stripped).
	if w.text() != "alpha\nbeta\ngamma\ndelta\n" {
		t.Errorf("text() = %q", w.text())
	}
}

func TestLineWriterEmptyLine(t *testing.T) {
	var got []string
	w := &lineWriter{onLine: func(s string) { got = append(got, s) }}
	if _, err := w.Write([]byte("\n")); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "" {
		t.Errorf("a bare newline should emit one empty line, got %v", got)
	}
}

func TestLineWriterFlushNoTrailingDataIsNoop(t *testing.T) {
	var calls int
	w := &lineWriter{onLine: func(string) { calls++ }}
	if _, err := w.Write([]byte("done\n")); err != nil {
		t.Fatal(err)
	}
	w.flush() // buf empty → must not emit a spurious blank line
	if calls != 1 {
		t.Errorf("flush with no pending data should not emit; calls=%d", calls)
	}
}

func TestLineWriterNilOnLine(t *testing.T) {
	w := &lineWriter{} // onLine nil: must not panic, still accumulates text
	if _, err := w.Write([]byte("x\ny")); err != nil {
		t.Fatal(err)
	}
	w.flush()
	if w.text() != "x\ny\n" {
		t.Errorf("text() = %q", w.text())
	}
}

package tui

import (
	"bytes"
	"errors"
	"testing"
)

func TestAutowrapOffBracketsTheBody(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	want := errors.New("done")
	err := autowrapOff(&buf, func() error {
		buf.WriteString("frame")
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want the body's", err)
	}
	if got := buf.String(); got != "\x1b[?7lframe\x1b[?7h" {
		t.Fatalf("stream = %q: wrap must be off around the body and on again after it", got)
	}
}

func TestAutowrapOffRestoresWrapOnPanic(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	func() {
		defer func() {
			if recover() == nil {
				t.Errorf("the body's panic must propagate")
			}
		}()
		_ = autowrapOff(&buf, func() error { panic("boom") })
	}()
	if got := buf.String(); got != "\x1b[?7l\x1b[?7h" {
		t.Fatalf("stream = %q: wrap must be restored even when the body panics", got)
	}
}

package syntax

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"
)

// LexCtx is Lex with a way out: same runs on a live context, nothing (and
// the context's error) once it is done — the diff's deferred colouring
// cancels a lex the reader has moved past.
func TestLexCtxMatchesLex(t *testing.T) {
	t.Parallel()
	src := []byte("package p\n\nfunc f() int { return 1 } // c\n")
	got, err := LexCtx(context.Background(), "go", src)
	if err != nil || !reflect.DeepEqual(got, Lex("go", src)) {
		t.Fatalf("LexCtx = %v, %v; want Lex's runs", got, err)
	}
}

func TestLexCtxCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := LexCtx(ctx, "go", bytes.Repeat([]byte("x := 1\n"), 50000))
	if !errors.Is(err, context.Canceled) || got != nil {
		t.Fatalf("LexCtx(cancelled) = %v, %v; want nil, context.Canceled", got, err)
	}
}

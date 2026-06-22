package domain

import (
	"context"
	"errors"
	"testing"

	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
)

func TestRevParseResolvesFullSHA(t *testing.T) {
	const full = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef" // 40 chars
	f := gitexec.NewFakeRunner()
	f.SetResponse("git rev-parse", gitexec.Result{Stdout: full + "\n"})
	svc := New(&git.Repo{Runner: f})

	got, err := svc.RevParse(context.Background(), "origin/foo")
	if err != nil {
		t.Fatalf("RevParse: %v", err)
	}
	if got != full {
		t.Fatalf("RevParse = %q, want %q", got, full)
	}
}

func TestRevParsePropagatesError(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetError("git rev-parse", errors.New("unknown revision"))
	svc := New(&git.Repo{Runner: f})

	if _, err := svc.RevParse(context.Background(), "no-such-ref"); err == nil {
		t.Fatal("RevParse(bogus) = nil error, want error")
	}
}

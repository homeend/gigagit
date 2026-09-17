package domain

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/linkhist"
)

func TestRecordLinkAndLinkHistory(t *testing.T) {
	t.Parallel()
	s := New(nil)
	s.SetLinkHistStore(linkhist.NewFileStore(t.TempDir()))
	ctx := context.Background()

	s.RecordLink(ctx, "gg://gigagit@ref:feat/x", "branch: feat/x")
	s.RecordLink(ctx, "gg://gigagit@abc123", "commit: abc123 fix auth")

	got := s.LinkHistory(ctx)
	if len(got) != 2 {
		t.Fatalf("LinkHistory = %v, want 2 entries", got)
	}
	// Newest-first: the commit link was recorded second.
	if got[0].Link != "gg://gigagit@abc123" || got[0].Desc != "commit: abc123 fix auth" {
		t.Fatalf("got[0] = %+v, want the commit entry", got[0])
	}
	if got[1].Link != "gg://gigagit@ref:feat/x" {
		t.Fatalf("got[1] = %+v, want the branch entry", got[1])
	}
}

func TestRecordLinkDedupsToTop(t *testing.T) {
	t.Parallel()
	s := New(nil)
	s.SetLinkHistStore(linkhist.NewFileStore(t.TempDir()))
	ctx := context.Background()

	s.RecordLink(ctx, "gg://gigagit@ref:feat/x", "branch: feat/x")
	s.RecordLink(ctx, "gg://gigagit@abc123", "commit: abc123 fix auth")
	// Re-copying the SAME link moves it to the top rather than adding a row.
	s.RecordLink(ctx, "gg://gigagit@ref:feat/x", "branch: feat/x")

	got := s.LinkHistory(ctx)
	if len(got) != 2 {
		t.Fatalf("LinkHistory = %v, want 2 entries (dedup-to-top, not 3)", got)
	}
	if got[0].Link != "gg://gigagit@ref:feat/x" {
		t.Fatalf("got[0] = %+v, want the re-copied branch entry on top", got[0])
	}
}

func TestLinkHistoryNilWhenNothingRecorded(t *testing.T) {
	t.Parallel()
	old := LinkHistStatePath
	LinkHistStatePath = t.TempDir()
	defer func() { LinkHistStatePath = old }()

	s := New(nil)
	got := s.LinkHistory(context.Background())
	if got != nil {
		t.Fatalf("LinkHistory = %v, want nil for a fresh store", got)
	}
}

func TestLinkHistoryNilWhenDisabled(t *testing.T) {
	// No injected store and no resolvable state dir → history disabled.
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("LocalAppData", "")
	old := LinkHistStatePath
	LinkHistStatePath = ""
	defer func() { LinkHistStatePath = old }()

	f := gitexec.NewFakeRunner()
	s := New(&git.Repo{Runner: f})

	// RecordLink must be a silent no-op: it must never crash and must never
	// make the copy the user asked for fail.
	s.RecordLink(context.Background(), "gg://gigagit@ref:feat/x", "branch: feat/x")

	if got := s.LinkHistory(context.Background()); got != nil {
		t.Fatalf("LinkHistory = %v, want nil when history is disabled", got)
	}
}

// TestLinkHistStateDirHonoursXDGStateHome pins the resolver's directory shape
// against an explicitly-set $XDG_STATE_HOME — the rule whose breach once
// wrote into a developer's real AppData\Local\gg.
func TestLinkHistStateDirHonoursXDGStateHome(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	old := LinkHistStatePath
	LinkHistStatePath = ""
	defer func() { LinkHistStatePath = old }()

	f := gitexec.NewFakeRunner()
	s := New(&git.Repo{Runner: f})
	ctx := context.Background()
	s.RecordLink(ctx, "gg://gigagit@ref:feat/x", "branch: feat/x")

	got := s.LinkHistory(ctx)
	if len(got) != 1 {
		t.Fatalf("LinkHistory = %v, want 1 entry", got)
	}

	wantBase := filepath.Join(stateHome, "gg", "linkhist")
	if got := stateBaseDir("linkhist"); got != wantBase {
		t.Fatalf("stateBaseDir(\"linkhist\") = %q, want %q", got, wantBase)
	}
}

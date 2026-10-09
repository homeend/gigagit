package domain

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/sessionreg"
	"github.com/homeend/gigagit/internal/wtguard"
)

// A worktree a TUI merely SHOWS (the fast switch) is taken, but the guard
// says so in those words — it is not where that TUI runs.
func TestTUIGuardNamesAShownWorktree(t *testing.T) {
	t.Parallel()
	own, shown := t.TempDir(), t.TempDir()
	g := tuiGuard{lv: liveView{tuis: []string{own}, viewed: []string{shown}}}
	r, _ := g.Check(context.Background(), wtguard.Target{Dir: own})
	if r.Blocker == nil || r.Blocker.Detail != "a gg TUI is open here" {
		t.Fatalf("own: %+v", r.Blocker)
	}
	r, _ = g.Check(context.Background(), wtguard.Target{Dir: shown})
	if r.Blocker == nil || !strings.Contains(r.Blocker.Detail, "shows") {
		t.Fatalf("shown: %+v, want the detail to say the TUI shows it", r.Blocker)
	}
}

// TUIViewing finds the TUI whose panels show dir, by the registry.
func TestTUIViewingFindsTheShowingTUI(t *testing.T) {
	t.Parallel()
	reg := t.TempDir()
	writeRegistry(t, reg, "p1", "/w/home", "/w/shown")
	if home, ok := tuiViewingIn(reg, "/w/shown"); !ok || home != "/w/home" {
		t.Fatalf("got %q %v, want /w/home", home, ok)
	}
	if _, ok := tuiViewingIn(reg, "/w/elsewhere"); ok {
		t.Fatal("a worktree no TUI shows was found")
	}
}

// writeRegistry publishes one TUI's registry file as PublishSessions would.
func writeRegistry(t *testing.T, dir, proc, worktree, viewed string) {
	t.Helper()
	if err := sessionreg.Write(dir, proc, sessionreg.Registry{PID: os.Getpid(), Worktree: worktree, Viewed: viewed}); err != nil {
		t.Fatal(err)
	}
}

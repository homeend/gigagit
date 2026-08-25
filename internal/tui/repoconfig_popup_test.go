package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRepoConfigActions(t *testing.T) {
	t.Parallel()
	// committed present, private absent → offer copy/move to private only.
	if got := repoConfigActions(true, false, true, true); len(got) != 2 ||
		got[0] != actCopyToPrivate || got[1] != actMoveToPrivate {
		t.Errorf("committed-only actions = %v", got)
	}
	// private present, committed absent → offer copy/move to committed only.
	if got := repoConfigActions(false, true, true, true); len(got) != 2 ||
		got[0] != actCopyToCommitted || got[1] != actMoveToCommitted {
		t.Errorf("private-only actions = %v", got)
	}
	// both present → all four.
	if got := repoConfigActions(true, true, true, true); len(got) != 4 {
		t.Errorf("both-present should offer 4 actions, got %d", len(got))
	}
	// neither present → none.
	if got := repoConfigActions(false, false, true, true); len(got) != 0 {
		t.Errorf("nothing present should offer no actions, got %v", got)
	}
	// no private path available (no anchor) → no to-private actions even if committed exists.
	if got := repoConfigActions(true, false, true, false); len(got) != 0 {
		t.Errorf("no private path should offer nothing, got %v", got)
	}
}

func TestRepoCfgEndpoints(t *testing.T) {
	t.Parallel()
	c, p := "/repo/.gg.toml", "/priv/config.toml"
	cases := []struct {
		act      repoCfgAction
		src, dst string
		isMove   bool
	}{
		{actCopyToPrivate, c, p, false},
		{actMoveToPrivate, c, p, true},
		{actCopyToCommitted, p, c, false},
		{actMoveToCommitted, p, c, true},
	}
	for _, tc := range cases {
		src, dst, isMove := repoCfgEndpoints(tc.act, c, p)
		if src != tc.src || dst != tc.dst || isMove != tc.isMove {
			t.Errorf("endpoints(%v) = (%q,%q,%v), want (%q,%q,%v)",
				tc.act, src, dst, isMove, tc.src, tc.dst, tc.isMove)
		}
	}
}

// TestRepoConfigRunRebindsWriteTargetSynchronously guards against the
// stale-write-target window: run must rebind m.repoConfigPath to the
// post-relocation active file the instant it returns, NOT rely on the async
// dataLoadedMsg (loadCmd) to do it later. Without the synchronous rebind, a
// per-repo Settings write (Show graph, Commit sort, a refresh rate, the hook)
// made during the reload window goes to the stale pre-relocation path — after
// a move-to-private that's the just-deleted committed file, which
// setScalarLine tolerantly recreates, silently reintroducing it.
func TestRepoConfigRunRebindsWriteTargetSynchronously(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		act           repoCfgAction
		seedCommitted bool
		seedPrivate   bool
		want          func(committed, private string) string // expected repoConfigPath right after run returns
	}{
		{
			name:          "copy to private",
			act:           actCopyToPrivate,
			seedCommitted: true,
			want:          func(c, p string) string { return p }, // private now exists -> active
		},
		{
			name:          "move to private",
			act:           actMoveToPrivate,
			seedCommitted: true,
			want:          func(c, p string) string { return p }, // committed deleted -> private active
		},
		{
			name:        "copy to committed",
			act:         actCopyToCommitted,
			seedPrivate: true,
			want:        func(c, p string) string { return p }, // private still exists -> stays active
		},
		{
			name:        "move to committed",
			act:         actMoveToCommitted,
			seedPrivate: true,
			want:        func(c, p string) string { return c }, // private deleted -> committed active
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			committed := filepath.Join(dir, "committed.gg.toml")
			private := filepath.Join(dir, "private.toml")
			if tc.seedCommitted {
				if err := os.WriteFile(committed, []byte("[ui]\ncommit_sort=\"plain\"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if tc.seedPrivate {
				if err := os.WriteFile(private, []byte("[ui]\ncommit_sort=\"plain\"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			pop := &repoConfigPopup{committedPath: committed, privatePath: private}
			pop.refresh()

			m := Model{}
			m = m.pushLayer(pop)
			// Seed a deliberately WRONG path first so the assertion below
			// proves run() actively rebinds it, rather than just observing an
			// already-correct zero value.
			m.repoConfigPath = "stale-pre-relocation-path"

			m, _ = pop.run(m, tc.act)

			want := tc.want(committed, private)
			if m.repoConfigPath != want {
				t.Fatalf("%s: repoConfigPath = %q, want %q (must be rebound synchronously, before any dataLoadedMsg)",
					tc.name, m.repoConfigPath, want)
			}
			if layerOf[*repoConfigPopup](m) != nil {
				t.Fatalf("%s: popup should have popped after run()", tc.name)
			}
		})
	}
}

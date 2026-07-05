package tui

import "testing"

func TestRepoConfigActions(t *testing.T) {
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

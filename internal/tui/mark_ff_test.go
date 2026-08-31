package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

// pairOpsMsg with a resolved fast-forward: the popup opens with the extra
// row, labelled in probe direction (behind → ahead), building a branch-lane
// FastForward regardless of which branch was marked first.
func TestPairOpsMsgAddsFastForwardRow(t *testing.T) {
	t.Parallel()
	m := markModel()
	m.mark = &markState{panel: panelBranches, key: "feat/a", display: "feat/a"}
	m.pairProbe = &pairProbeReq{marked: "feat/a", selected: "main"}
	updated, _ := m.Update(pairOpsMsg{
		marked: "feat/a", selected: "main",
		ff: domain.FFPair{Behind: "feat/a", Ahead: "main", OK: true},
	})
	m = updated.(Model)
	pp := layerOf[*pairOpPopup](m)
	if pp == nil {
		t.Fatal("expected the pair-op popup")
	}
	if len(pp.ops) != 5 {
		t.Fatalf("expected 5 ops with the fast-forward row, got %d", len(pp.ops))
	}
	ffRow := pp.ops[2]
	if got := ffRow.label("feat/a", "main"); got != "Fast-forward feat/a to main" {
		t.Fatalf("label = %q, want %q", got, "Fast-forward feat/a to main")
	}
	op, ok := ffRow.build("feat/a", "main").(engine.FastForward)
	if !ok {
		t.Fatalf("build = %T, want engine.FastForward", ffRow.build("feat/a", "main"))
	}
	if op.Branch != "feat/a" || op.Commit != "main" {
		t.Fatalf("op = %+v, want Branch=feat/a Commit=main", op)
	}
}

// No fast-forward possible (equal tips or diverged): the popup opens with
// just the four standard rows.
func TestPairOpsMsgWithoutFastForward(t *testing.T) {
	t.Parallel()
	m := markModel()
	m.mark = &markState{panel: panelBranches, key: "feat/a", display: "feat/a"}
	m.pairProbe = &pairProbeReq{marked: "feat/a", selected: "main"}
	updated, _ := m.Update(pairOpsMsg{marked: "feat/a", selected: "main"})
	m = updated.(Model)
	pp := layerOf[*pairOpPopup](m)
	if pp == nil {
		t.Fatal("expected the pair-op popup")
	}
	if len(pp.ops) != 4 {
		t.Fatalf("expected the 4 standard ops, got %d", len(pp.ops))
	}
}

// The mark died (or moved) while the probe ran: the late msg must not open a
// popup for a pair the user no longer has marked.
func TestPairOpsMsgStaleMarkIgnored(t *testing.T) {
	t.Parallel()
	m := markModel()
	m.pairProbe = &pairProbeReq{marked: "feat/a", selected: "main"}
	updated, _ := m.Update(pairOpsMsg{marked: "feat/a", selected: "main"})
	m = updated.(Model)
	if layerOf[*pairOpPopup](m) != nil {
		t.Fatal("a stale pairOpsMsg must not open the popup")
	}
}

// A newer pairing superseded the probe (the user moved the cursor and pressed
// m again): the older msg no longer matches pairProbe and must not open a
// popup for a pair the user abandoned.
func TestPairOpsMsgSupersededProbeIgnored(t *testing.T) {
	t.Parallel()
	m := markModel()
	m.mark = &markState{panel: panelBranches, key: "main", display: "main"}
	m.pairProbe = &pairProbeReq{marked: "main", selected: "feat/b"}
	updated, _ := m.Update(pairOpsMsg{marked: "main", selected: "feat/a"})
	m = updated.(Model)
	if layerOf[*pairOpPopup](m) != nil {
		t.Fatal("a superseded probe msg must not open the popup")
	}
	if m.pairProbe == nil {
		t.Fatal("the pending probe must survive an older msg")
	}
}

// An operation started while the probe ran: the popup must not open over it
// (its enter would startOp mid-op).
func TestPairOpsMsgWhileRunningIgnored(t *testing.T) {
	t.Parallel()
	m := markModel()
	m.mark = &markState{panel: panelBranches, key: "main", display: "main"}
	m.pairProbe = &pairProbeReq{marked: "main", selected: "feat/a"}
	m.running = true
	updated, _ := m.Update(pairOpsMsg{marked: "main", selected: "feat/a"})
	m = updated.(Model)
	if layerOf[*pairOpPopup](m) != nil {
		t.Fatal("the popup must not open while an operation runs")
	}
}

// End to end against a real repo: branch "old" is strictly behind main, so
// pairing them surfaces the fast-forward row in behind → ahead direction.
func TestMarkPairProbesFastForward(t *testing.T) {
	t.Parallel()
	dir, repo := newRepoDir(t)
	runGit(t, dir, "branch", "old")
	if err := os.WriteFile(filepath.Join(dir, "ahead.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "main ahead")

	m := New(domain.New(repo))
	updated, _ := m.Update(m.loadCmd()())
	m = updated.(Model)
	m.focus = panelBranches
	for i, b := range m.branches {
		if b.Name == "old" {
			m.sel[panelBranches] = i
		}
	}
	m = pressRune(t, m, "m") // mark old
	for i, b := range m.branches {
		if b.Name == "main" {
			m.sel[panelBranches] = i
		}
	}
	updated, cmd := m.Update(keyMsg("m")) // pair with main: probes first
	m = updated.(Model)
	if layerOf[*pairOpPopup](m) != nil {
		t.Fatal("the popup must wait for the fast-forward probe")
	}
	if cmd == nil {
		t.Fatal("pairing must return the probe cmd")
	}
	msg, ok := cmd().(pairOpsMsg)
	if !ok {
		t.Fatalf("probe must deliver a pairOpsMsg, got %T", cmd())
	}
	if !msg.ff.OK || msg.ff.Behind != "old" || msg.ff.Ahead != "main" {
		t.Fatalf("ff = %+v, want behind=old ahead=main", msg.ff)
	}
	updated, _ = m.Update(msg)
	m = updated.(Model)
	pp := layerOf[*pairOpPopup](m)
	if pp == nil {
		t.Fatal("expected the pair-op popup after the probe")
	}
	if len(pp.ops) != 5 {
		t.Fatalf("expected 5 ops with the fast-forward row, got %d", len(pp.ops))
	}
}

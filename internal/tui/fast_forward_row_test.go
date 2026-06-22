package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/model"
)

// ffModel: current branch "main" tip = C1; child "feat" ahead at C2.
func ffModel() Model {
	commits := []model.Commit{
		{Hash: "c2c2c2c", Parents: []string{"c1c1c1c"}, UnixTime: 30,
			Refs: []model.Ref{{Name: "feat", Kind: model.RefLocal}}},
		{Hash: "c1c1c1c", Parents: []string{"c0c0c0c"}, UnixTime: 20,
			Refs: []model.Ref{{Name: "main", Kind: model.RefLocal, Head: true}}},
		{Hash: "c0c0c0c", Parents: nil, UnixTime: 10},
	}
	m := Model{
		commits:   commits,
		sel:       map[panel]int{panelCommits: 0}, // select C2 (the ahead commit)
		sortModes: map[panel]sortMode{},
		focus:     panelCommits,
	}
	m.status.Branch = "main"
	return m
}

func TestFastForwardRowShownOnAheadCommit(t *testing.T) {
	m := ffModel() // selected = C2, ahead of main's tip C1
	row, ok := m.commitFastForwardRow()
	if !ok {
		t.Fatal("row must be offered when the selected commit is ahead of the current branch")
	}
	if row.label != "Fast-forward main to here" {
		t.Fatalf("label = %q", row.label)
	}
}

func TestFastForwardRowHiddenOnTip(t *testing.T) {
	m := ffModel()
	m.sel[panelCommits] = 1 // select C1 = main's own tip
	if _, ok := m.commitFastForwardRow(); ok {
		t.Fatal("row must be hidden on the current branch tip itself")
	}
}

func TestFastForwardRowHiddenOnBehindCommit(t *testing.T) {
	m := ffModel()
	m.sel[panelCommits] = 2 // select C0 = behind main's tip
	if _, ok := m.commitFastForwardRow(); ok {
		t.Fatal("row must be hidden on a commit behind the current branch")
	}
}

func TestFastForwardRowHiddenWhenDetached(t *testing.T) {
	m := ffModel()
	// porcelain v2 "# branch.head (detached)" → status.Branch == "(detached)",
	// NOT "" (that's the engine-side symbolic-ref representation). The TUI guard
	// must hide on the real value the status parser produces.
	m.status.Branch = "(detached)"
	if _, ok := m.commitFastForwardRow(); ok {
		t.Fatal("row must be hidden when HEAD is detached")
	}
}

func TestFastForwardRowWiredToOp(t *testing.T) {
	m := ffModel()
	row, ok := m.commitFastForwardRow()
	if !ok {
		t.Fatal("row expected")
	}
	// Assert the row is wired to a run handler (the FastForward op's behavior is
	// covered by the engine tests). Invoking run() here would spawn startOp's
	// svc.Execute goroutine against this fixture's nil svc and panic.
	if row.run == nil {
		t.Fatal("row must carry a run handler")
	}
	if row.id != "commit-fast-forward" {
		t.Fatalf("row id = %q", row.id)
	}
}

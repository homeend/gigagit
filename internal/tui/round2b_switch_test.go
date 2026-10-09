package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// --- H: a failed slot status read is said, and the slot stops "loading" ---

func TestFailedSlotStatusReadSaysSoAndStopsLoading(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	m, ok := m.switchView(other)
	if !ok {
		t.Fatalf("refused: %s", m.statusMsg)
	}
	if !m.viewLoading() {
		t.Fatal("precondition: the arriving slot is loading")
	}
	nm, _ := m.Update(dataAvailableMsg{source: srcStatus, gen: m.srcGen[srcStatus], err: errors.New("boom")})
	m = nm.(Model)
	if m.viewLoading() {
		t.Fatal("⏳ loading… stays forever after a failed status read")
	}
	if !strings.Contains(m.statusMsg, "boom") {
		t.Fatalf("status = %q, want the read's error", m.statusMsg)
	}
}

// --- I: alt+w skips a worktree that cannot be reached ---

func TestAltWSkipsAnUnreachableWorktree(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m, _ = addWorktree(t, m, "wtA")
	m, _ = addWorktree(t, m, "wtB")
	order := m.worktreeOrder()
	at := m.worktreeIndex(m.homeWorktree())
	next := m.worktrees[order[(at+1)%3]].Path
	after := m.worktrees[order[(at+2)%3]].Path
	if err := os.RemoveAll(next); err != nil {
		t.Fatal(err)
	}
	m = pressAlt(t, m, 'w')
	if m.viewed != model.KeyOf(after) {
		t.Fatalf("viewed = %q, want %q (the unreachable %q skipped); status %q", m.viewed, after, next, m.statusMsg)
	}
	if !strings.Contains(m.statusMsg, shortWorktreeName(next)) {
		t.Fatalf("status = %q, want the skipped worktree named", m.statusMsg)
	}
}

// --- J: the bare repository is no ring stop and no switch target ---

func TestBareRepositoryIsNotAWorktreeToShow(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m, _ = addWorktree(t, m, "wt2")
	bare := filepath.Join(t.TempDir(), "repo.git")
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatal(err)
	}
	m.worktrees = append([]model.Worktree{{Path: bare, Bare: true}}, slices.Clone(m.worktrees)...)
	for _, i := range m.worktreeOrder() {
		if m.worktrees[i].Bare {
			t.Fatal("the bare repository is in the alt+w ring")
		}
	}
	if nm, ok := m.switchView(bare); ok || nm.viewed != m.home || nm.statusMsg == "" {
		t.Fatalf("switchView(bare): ok=%v viewed=%q status=%q; want refused with a reason", ok, nm.viewed, nm.statusMsg)
	}
	m.focus = panelWorktrees
	seen := false
	for d := range m.panelLen(panelWorktrees) {
		m.sel[panelWorktrees] = d
		if wt, ok := m.selectedWorktree(); ok && wt.Bare {
			seen = true
			if m.canEnterWorktree() {
				t.Fatal("enter is offered on the bare repository's row")
			}
		}
	}
	if !seen {
		t.Fatal("precondition: the bare row is listed")
	}
}

// --- K: a queued return to a slot that is gone goes home ---

func TestQueuedReturnToAGoneSlotGoesHome(t *testing.T) {
	m := loadedModel(t)
	m, _ = viewedOther(t, m)
	m.pendingReturnView = model.KeyOf(filepath.Join(t.TempDir(), "gone"))
	m = m.takeQueuedReturn()
	if m.viewed != m.home || m.pendingReturnView != "" {
		t.Fatalf("viewed=%q pending=%q status=%q; want home", m.viewed, m.pendingReturnView, m.statusMsg)
	}
	if strings.Contains(m.statusMsg, "is not a worktree") {
		t.Fatalf("status = %q: an empty name was reported", m.statusMsg)
	}
}

func TestPruneViewsRetargetsAQueuedReturn(t *testing.T) {
	m := loadedModel(t)
	m, a := addWorktree(t, m, "wtA")
	m, b := addWorktree(t, m, "wtB")
	m, _ = m.switchView(a)
	m = landView(t, m)
	m, _ = m.switchView(b)
	m = landView(t, m)
	m.pendingReturnView = model.KeyOf(a)
	m = dropWorktreeFromList(m, a)
	m = m.pruneViews()
	if m.pendingReturnView != m.home {
		t.Fatalf("pending = %q after the slot it named was pruned; want home %q", m.pendingReturnView, m.home)
	}
}

// --- L: the WIP rows are the arriving tree's even when no head mark moves ---

func TestSwapRederivesTheWipRows(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	if err := os.WriteFile(filepath.Join(m.homeWorktree(), "dirty.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	nm, _ := m.Update(m.readSourceCmd(context.Background(), srcStatus, reloadOpts{manual: true})())
	m = nm.(Model)
	if len(m.wipRows) == 0 {
		t.Fatal("precondition: home shows a WIP row")
	}
	// No head mark moves on the swap: the lists say every tree is detached.
	m.worktrees = slices.Clone(m.worktrees)
	for i := range m.worktrees {
		m.worktrees[i].Branch = ""
	}
	m.branches = slices.Clone(m.branches)
	for i := range m.branches {
		m.branches[i].IsHead = false
	}
	m.commits = slices.Clone(m.commits)
	for i := range m.commits {
		m.commits[i].Refs = slices.Clone(m.commits[i].Refs)
		for j := range m.commits[i].Refs {
			m.commits[i].Refs[j].Head = false
		}
	}
	m, ok := m.switchView(other)
	if !ok {
		t.Fatalf("refused: %s", m.statusMsg)
	}
	if len(m.wipRows) != 0 {
		t.Fatalf("%d WIP rows over the clean worktree: home's rows survived the swap", len(m.wipRows))
	}
}

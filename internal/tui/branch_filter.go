package tui

import (
	"strconv"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/branchfilter"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/promptstate"
)

// Branch filters (alt+1…5): the five [[branches.filter]] slots applied to the
// Branches and Remotes panels, one active slot per panel (radio), remembered
// per repo in prompts.toml. Evaluation runs inside displayIndices BEFORE the
// / text filter, so / stacks on top; verdicts are memoised because
// displayIndices fires many times per keystroke.

// branchFilterMemo caches the verdicts for one panel. Shared pointer across
// Model copies (the commitFilterMemo discipline). Self-validating key: the
// slot, the list length, and the IDENTITY of the backing slice (a pointer to
// its first element). Every refresh path assigns a NEW slice (m.branches =
// msg.branches; sortRemoteBranchesLocalFirst returns a fresh one), so the
// pointer changes whenever the data can; nothing mutates a row in place.
type branchFilterMemo struct {
	slot           int
	n              int
	head           any // *model.Branch or *model.RemoteBranch of element 0
	hidden, exempt []bool
	count          int
}

// branchFilterMemos holds one memo per filtered panel behind one pointer.
type branchFilterMemos struct{ branches, remotes branchFilterMemo }

// invalidate drops both memos through the shared pointer. Called wherever
// m.branches, m.remoteBranches or m.worktrees are assigned: the key above
// catches a replaced LIST, but exemptions also read the worktree list (the
// Branches panel) and HEAD's upstream (the Remotes panel), neither of which
// moves the filtered list's pointer. It is also called after the slots are
// recompiled from a fresh config — the key carries the slot NUMBER, not the
// rule, so an edited rule under the same number would otherwise serve stale
// verdicts. nil-safe for zero-value test Models.
func (c *branchFilterMemos) invalidate() {
	if c != nil {
		c.branches = branchFilterMemo{}
		c.remotes = branchFilterMemo{}
	}
}

// branchFilterListName maps a panel to its promptstate list name.
func branchFilterListName(p panel) string {
	switch p {
	case panelBranches:
		return promptstate.BranchFilterListBranches
	case panelRemotes:
		return promptstate.BranchFilterListRemotes
	}
	return ""
}

// activeBranchFilter returns the usable Compiled slot active on p, or nil.
func (m Model) activeBranchFilter(p panel) *branchfilter.Compiled {
	slot := m.branchFilterSlot[p]
	if slot < 1 || slot > branchfilter.MaxSlots {
		return nil
	}
	c := m.branchFilters[slot-1]
	if !c.Usable() {
		return nil
	}
	return &c
}

// branchFilterHidden returns per-backing-index verdicts for p under its
// active slot (nil, nil, 0 when none). Memoised per panel.
func (m Model) branchFilterHidden(p panel) (hidden, exempt []bool, count int) {
	c := m.activeBranchFilter(p)
	if c == nil {
		return nil, nil, 0
	}
	var head any
	var n int
	switch p {
	case panelBranches:
		n = len(m.branches)
		if n > 0 {
			head = &m.branches[0]
		}
	case panelRemotes:
		n = len(m.remoteBranches)
		if n > 0 {
			head = &m.remoteBranches[0]
		}
	default:
		return nil, nil, 0
	}
	memo := m.bfMemoFor(p)
	if memo != nil && n > 0 && memo.slot == c.Slot.Slot && memo.n == n && memo.head == head {
		return memo.hidden, memo.exempt, memo.count
	}
	var rows []branchfilter.Row
	var ex []bool
	if p == panelBranches {
		rows = domain.BranchRows(m.branches)
		ex = domain.ExemptBranches(m.branches, m.worktrees)
	} else {
		rows = domain.RemoteBranchRows(m.remoteBranches)
		ex = domain.ExemptRemoteBranches(m.remoteBranches, m.branches)
	}
	v, cnt := branchfilter.Apply(*c, rows, ex, time.Now())
	hidden = make([]bool, len(v))
	exempt = make([]bool, len(v))
	for i, vv := range v {
		hidden[i], exempt[i] = vv.Hidden, vv.Exempt
	}
	if memo != nil && n > 0 {
		*memo = branchFilterMemo{slot: c.Slot.Slot, n: n, head: head, hidden: hidden, exempt: exempt, count: cnt}
	}
	return hidden, exempt, cnt
}

// bfMemoFor returns the panel's memo pointer (nil in zero-value test Models).
func (m Model) bfMemoFor(p panel) *branchFilterMemo {
	if m.bfMemo == nil {
		return nil
	}
	switch p {
	case panelBranches:
		return &m.bfMemo.branches
	case panelRemotes:
		return &m.bfMemo.remotes
	}
	return nil
}

// toggleBranchFilter is alt+N: on Branches/Remotes it activates slot N, or
// clears it when N is already active (radio). Elsewhere it explains itself.
// An inert or empty slot is refused with its reason and the active slot is
// left alone. The choice is persisted per repo; a failed write still applies
// for the session.
func (m Model) toggleBranchFilter(slot int) Model {
	p := m.focus
	list := branchFilterListName(p)
	if list == "" {
		m.statusMsg = i18n.T("alt+1…5 filter the Branches or Remotes panel — focus one first")
		return m
	}
	if slot < 1 || slot > branchfilter.MaxSlots {
		return m
	}
	next := slot
	if m.branchFilterSlot[p] == slot {
		next = 0
	} else {
		c := m.branchFilters[slot-1]
		if !c.Usable() {
			m.statusMsg = i18n.T("slot %d: %s", slot, bfSummary(c))
			return m
		}
	}
	if m.branchFilterSlot == nil {
		m.branchFilterSlot = map[panel]int{}
	}
	m.branchFilterSlot[p] = next
	if n := m.panelLen(p); m.sel[p] >= n && n > 0 {
		m.sel[p] = n - 1
	}
	if next == 0 {
		m.statusMsg = i18n.T("branch filter off")
	} else {
		m.statusMsg = i18n.T("branch filter %d: %s", next, bfLabel(m.branchFilters[next-1]))
	}
	switch key := m.bfRepoKey(); {
	case m.promptStore == nil:
		m.statusMsg += " " + i18n.T("(not remembered: no state dir)")
	case key == "":
		m.statusMsg += " " + i18n.T("(not remembered: repo not resolved yet)")
	default:
		if err := m.promptStore.SetBranchFilterSlot(key, list, next); err != nil {
			m.statusMsg += " " + i18n.T("(not remembered: %s)", err.Error())
		}
	}
	return m
}

// applyBranchFilterConfig recompiles the five slots from the config just
// written to m.cfg and re-reads the remembered active slots. Every m.cfg
// assignment routes through this one helper so no path can compile the slots
// and forget the memo (whose key carries the slot number, not the rule).
func (m Model) applyBranchFilterConfig() Model {
	m.branchFilters, m.branchFilterWarnings = branchfilter.CompileAll(m.cfg.Branches.Filter)
	m.bfCfgApplied = true
	m.bfMemo.invalidate()
	return m.loadBranchFilterSlots()
}

// bfRepoKey is the promptstate scope for this feature: the git common dir
// the health probe resolved, and ONLY that — the web keys the same record by
// svc.GitCommonDir, so the worktree-path fallback toolRepoKey uses would
// split the two frontends' memory. "" until the probe has run.
//
// The repoHealthKnown gate is load-bearing, not belt-and-braces: reRoot does
// NOT clear m.repoHealth (only the flag), so between a repo switch and the
// new probe the struct still holds the OLD repo's common dir. Without the
// gate, the switch's own configReadyMsg/dataLoadedMsg would load repo A's
// remembered slots into repo B, latch bfSlotsLoaded, and make B's real probe
// a no-op — and an alt+N pressed in that window would write into A's record.
func (m Model) bfRepoKey() string {
	if !m.repoHealthKnown {
		return ""
	}
	return m.repoHealth.GitCommonDir
}

// loadBranchFilterSlots reads the remembered slots for this repo once BOTH of
// its inputs have landed: the repo key (the health probe) and this repo's own
// compiled rules (its config). A slot that no longer exists or is unusable
// loads as none (nothing is rewritten). The load then latches (bfSlotsLoaded)
// so a later probe cannot clobber an alt+N made since.
//
// Both gates are load-bearing after a repo switch: reRoot batches loadCmd and
// repoHealthCmd together, and the probe wins that race essentially every
// time. Without bfCfgApplied the probe's load would validate the NEW repo's
// remembered slot against the OLD repo's compiled rules — silently dropping
// it, or keeping a number whose rule is inert — and latch, leaving the
// config's own load a no-op. Whichever of the two lands second completes it.
// Startup is unaffected: run.go compiles the config synchronously before Init
// dispatches the first probe.
func (m Model) loadBranchFilterSlots() Model {
	if m.branchFilterSlot == nil {
		m.branchFilterSlot = map[panel]int{}
	}
	key := m.bfRepoKey()
	if m.promptStore == nil || key == "" || !m.bfCfgApplied || m.bfSlotsLoaded {
		return m
	}
	for _, p := range []panel{panelBranches, panelRemotes} {
		s := m.promptStore.BranchFilterSlot(key, branchFilterListName(p))
		if s < 1 || s > branchfilter.MaxSlots || !m.branchFilters[s-1].Usable() {
			s = 0
		}
		m.branchFilterSlot[p] = s
	}
	m.bfSlotsLoaded = true
	return m
}

// bfSummary is Compiled.Summary rebuilt from translated pieces: the leaf's
// English is for the web and logs; every string a TUI row shows goes
// through i18n.T. Regex/age parse errors keep their technical detail
// (the same way git's stderr is shown verbatim on the status line).
func bfSummary(c branchfilter.Compiled) string {
	if c.Err != nil {
		return i18n.T("invalid — %s", c.Err.Error())
	}
	if c.Empty {
		return i18n.T("empty (no clause set)")
	}
	var parts []string
	if c.OlderThan != "" {
		parts = append(parts, i18n.T("older than %s", strings.TrimSpace(c.OlderThan)))
	}
	if c.YoungerThan != "" {
		parts = append(parts, i18n.T("younger than %s", strings.TrimSpace(c.YoungerThan)))
	}
	if c.Prefix != "" {
		parts = append(parts, i18n.T("prefix %s", c.Prefix))
	}
	if c.Suffix != "" {
		parts = append(parts, i18n.T("suffix %s", c.Suffix))
	}
	if c.Contains != "" {
		parts = append(parts, i18n.T("contains %s", c.Contains))
	}
	if c.Regex != "" {
		parts = append(parts, i18n.T("regex %s", c.Regex))
	}
	mode := i18n.T("hide")
	if c.Mode == branchfilter.ModeShow {
		mode = i18n.T("show only")
	}
	return mode + " · " + strings.Join(parts, ", ")
}

// bfLabel is Compiled.Label translated: the user's name when the slot has
// one, otherwise the "slot N" fallback. The leaf's English Label() is for the
// web and the logs; every string a TUI header or status line shows goes
// through i18n.T.
func bfLabel(c branchfilter.Compiled) string {
	if n := strings.TrimSpace(c.Name); n != "" {
		return n
	}
	return i18n.T("slot %d", c.Slot.Slot)
}

// branchFilterDecoration is the panel-header suffix: " ▽2 stale · 12 hidden",
// or " ▽2 stale" when nothing is hidden, or "" with no active slot.
func (m Model) branchFilterDecoration(p panel) string {
	c := m.activeBranchFilter(p)
	if c == nil {
		return ""
	}
	_, _, n := m.branchFilterHidden(p)
	s := " ▽" + strconv.Itoa(c.Slot.Slot) + " " + bfLabel(*c)
	if n > 0 {
		s += " · " + i18n.T("%d hidden", n)
	}
	return s
}

// branchFilterExemptMark is the marker appended to an exempt row's name
// (HEAD / checked out / upstream that the rule would otherwise hide).
const branchFilterExemptMark = " ∗"

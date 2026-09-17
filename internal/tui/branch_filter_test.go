package tui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/branchfilter"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/promptstate"
)

func altKey(digit rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{digit}, Alt: true}
}

func bfModel(t *testing.T) Model {
	t.Helper()
	m := newTestModel(t)
	// New() starts loading; every alt+digit is gated on !m.running && !m.loading
	// (the same gate as o), and opsIdle() decides whether the footer advertises
	// the key. A model that never finished loading would fail these tests for
	// the wrong reason.
	m.loading = false
	// New() resolves the machine's REAL prompts.toml (TestMain pins
	// XDG_CONFIG_HOME, not XDG_STATE_HOME), and toggling a slot persists it.
	// Point the store at a scratch file so the suite never writes "/r/.git"
	// records into the developer's state dir.
	m.promptStore = promptstate.NewFileStore(filepath.Join(t.TempDir(), "prompts.toml"))
	now := time.Now().Unix()
	m.branches = []model.Branch{
		{Name: "main", IsHead: true, UnixTime: now},
		{Name: "feat/a", UnixTime: now},
		{Name: "feat/old", UnixTime: now - 200*24*3600},
		{Name: "fix/b", UnixTime: now},
		{Name: "feat/wt", UnixTime: now - 200*24*3600},
	}
	m.worktrees = []model.Worktree{{Path: "/r", Branch: "main"}, {Path: "/r2", Branch: "feat/wt"}}
	m.remoteBranches = []model.RemoteBranch{
		{Name: "origin/main", Remote: "origin", Branch: "main", UnixTime: now},
		{Name: "origin/feat/a", Remote: "origin", Branch: "feat/a", UnixTime: now},
	}
	m.branches[0].Upstream = "origin/main"
	// The promptstate key, and the flag that says the probe resolved it; tests
	// that want "not resolved" blank one of them.
	m.repoHealth.GitCommonDir = "/r/.git"
	m.repoHealthKnown = true
	m.branchFilters, _ = branchfilter.CompileAll([]branchfilter.Slot{
		{Slot: 1, Name: "feat", Prefix: "feat/"},
		{Slot: 2, Name: "stale", OlderThan: "90d"},
		{Slot: 3, Name: "only-fix", Mode: branchfilter.ModeShow, Prefix: "fix/"},
		{Slot: 4, Regex: "("}, // inert
	})
	m.focus = panelBranches
	m.sortModes[panelBranches] = sortDefault
	return m
}

func names(m Model, p panel) []string {
	_, idx := m.panelView(p)
	l := m.listFor(p)
	out := make([]string, len(idx))
	for i, b := range idx {
		switch t := l.(type) {
		case branchList:
			out[i] = t.Name(b)
		case remoteBranchList:
			out[i] = t.Name(b)
		}
	}
	return out
}

func TestAltDigitSelectsSlotAndHidesRows(t *testing.T) {
	t.Parallel()
	m := bfModel(t)
	mm, _ := m.Update(altKey('1'))
	m = mm.(Model)
	got := strings.Join(names(m, panelBranches), ",")
	// feat/a and feat/old hidden; main (HEAD) and feat/wt (worktree) exempt.
	if got != "main,fix/b,feat/wt" {
		t.Errorf("slot 1: %s", got)
	}
	if m.branchFilterSlot[panelBranches] != 1 {
		t.Errorf("active = %d", m.branchFilterSlot[panelBranches])
	}
	if d := m.branchFilterDecoration(panelBranches); d != " ▽1 feat · 2 hidden" {
		t.Errorf("decoration = %q", d)
	}
}

func TestAltSameDigitClearsAndOtherDigitSwitches(t *testing.T) {
	t.Parallel()
	m := bfModel(t)
	mm, _ := m.Update(altKey('2'))
	m = mm.(Model)
	if got := strings.Join(names(m, panelBranches), ","); got != "main,feat/a,fix/b,feat/wt" {
		t.Errorf("slot 2: %s", got)
	}
	mm, _ = m.Update(altKey('3')) // radio: switches, does not stack
	m = mm.(Model)
	if got := strings.Join(names(m, panelBranches), ","); got != "main,fix/b,feat/wt" {
		t.Errorf("slot 3 (show only fix/): %s", got)
	}
	mm, _ = m.Update(altKey('3'))
	m = mm.(Model)
	if m.branchFilterSlot[panelBranches] != 0 || len(names(m, panelBranches)) != 5 {
		t.Errorf("same key should clear: slot=%d rows=%v", m.branchFilterSlot[panelBranches], names(m, panelBranches))
	}
	if m.branchFilterDecoration(panelBranches) != "" {
		t.Errorf("decoration after clear = %q", m.branchFilterDecoration(panelBranches))
	}
}

func TestAltDigitInertSlotRefusedWithReason(t *testing.T) {
	t.Parallel()
	m := bfModel(t)
	mm, _ := m.Update(altKey('4'))
	m = mm.(Model)
	if m.branchFilterSlot[panelBranches] != 0 {
		t.Errorf("inert slot activated")
	}
	if !strings.Contains(m.statusMsg, "slot 4") || !strings.Contains(m.statusMsg, "regex") {
		t.Errorf("status = %q", m.statusMsg)
	}
	mm, _ = m.Update(altKey('5')) // empty slot
	m = mm.(Model)
	if m.branchFilterSlot[panelBranches] != 0 || !strings.Contains(m.statusMsg, "slot 5") {
		t.Errorf("empty slot: active=%d status=%q", m.branchFilterSlot[panelBranches], m.statusMsg)
	}
}

func TestAltDigitOnOtherPanelExplains(t *testing.T) {
	t.Parallel()
	m := bfModel(t)
	m.focus = panelCommits
	mm, _ := m.Update(altKey('1'))
	m = mm.(Model)
	if m.branchFilterSlot[panelBranches] != 0 || m.branchFilterSlot[panelRemotes] != 0 {
		t.Error("a filter was activated from the Commits panel")
	}
	if !strings.Contains(m.statusMsg, "Branches") {
		t.Errorf("status = %q", m.statusMsg)
	}
}

func TestRemotesSlotIsIndependentAndUpstreamExempt(t *testing.T) {
	t.Parallel()
	m := bfModel(t)
	m.focus = panelRemotes
	m.activeLeftTab = panelRemotes
	mm, _ := m.Update(altKey('1'))
	m = mm.(Model)
	if m.branchFilterSlot[panelBranches] != 0 || m.branchFilterSlot[panelRemotes] != 1 {
		t.Errorf("slots = %v", m.branchFilterSlot)
	}
	if got := strings.Join(names(m, panelRemotes), ","); got != "origin/main" {
		t.Errorf("remotes under slot 1: %s (origin/feat/a hidden, origin/main is HEAD's upstream)", got)
	}
	if !strings.Contains(m.branchFilterDecoration(panelRemotes), "1 hidden") {
		t.Errorf("decoration = %q", m.branchFilterDecoration(panelRemotes))
	}
}

func TestSlashFilterStacksOnBranchFilter(t *testing.T) {
	t.Parallel()
	m := bfModel(t)
	m.branchFilterSlot[panelBranches] = 3 // show only fix/ (+ exempt)
	m.filterPanel = panelBranches
	m.filterQuery = "wt"
	if got := strings.Join(names(m, panelBranches), ","); got != "feat/wt" {
		t.Errorf("stacked: %s", got)
	}
}

func TestBranchFilterMemoTracksListChanges(t *testing.T) {
	t.Parallel()
	m := bfModel(t)
	m.branchFilterSlot[panelBranches] = 1
	if n := len(names(m, panelBranches)); n != 3 {
		t.Fatalf("before: %d rows", n)
	}
	m.branches = append(m.branches, model.Branch{Name: "feat/new", UnixTime: time.Now().Unix()})
	if got := strings.Join(names(m, panelBranches), ","); got != "main,fix/b,feat/wt" {
		t.Errorf("after append: %s", got)
	}
	// A refresh always assigns a NEW slice; a same-length replacement with a
	// different middle row must re-evaluate (the memo keys on slice identity).
	fresh := append([]model.Branch(nil), m.branches...)
	fresh[1].Name = "zzz/renamed"
	m.branches = fresh
	if got := strings.Join(names(m, panelBranches), ","); !strings.Contains(got, "zzz/renamed") {
		t.Errorf("after replacement: %s", got)
	}
	// Exemptions depend on OTHER inputs (worktrees; HEAD's upstream for
	// remotes) that never change the list pointer: the refresh paths call
	// invalidate() — prove the memo honours it.
	m.worktrees = []model.Worktree{{Path: "/r", Branch: "main"}} // feat/wt no longer checked out
	m.bfMemo.invalidate()
	if got := strings.Join(names(m, panelBranches), ","); strings.Contains(got, "feat/wt") {
		t.Errorf("after worktree removal + invalidate, feat/wt should hide: %s", got)
	}
}

func TestAltDigitWhileTypingSlashFilterIsIgnored(t *testing.T) {
	t.Parallel()
	m := bfModel(t)
	m.filterTyping = true
	m.filterPanel = panelBranches
	m.filterQuery = "fe"
	mm, _ := m.Update(altKey('1'))
	m = mm.(Model)
	if m.filterQuery != "fe" {
		t.Errorf("alt+1 leaked into the / query: %q", m.filterQuery)
	}
	if m.branchFilterSlot[panelBranches] != 0 {
		t.Errorf("alt+1 toggled a slot while typing")
	}
}

func TestToggleWithoutRepoKeyAppliesButIsNotRemembered(t *testing.T) {
	t.Parallel()
	m := bfModel(t)
	// The lifecycle's real "not resolved yet": reRoot clears the flag and
	// leaves the (now stale) snapshot in place — it never blanks GitCommonDir.
	m.repoHealthKnown = false
	mm, _ := m.Update(altKey('1'))
	m = mm.(Model)
	if m.branchFilterSlot[panelBranches] != 1 {
		t.Errorf("slot should apply for the session")
	}
	if !strings.Contains(m.statusMsg, "not remembered") {
		t.Errorf("status = %q", m.statusMsg)
	}
}

func TestRepoSwitchDropsTheRememberedSlotsUntilTheNewProbeLands(t *testing.T) {
	t.Parallel()
	m := bfModel(t)
	m.branchFilterSlot[panelBranches] = 1
	m.bfSlotsLoaded = true
	nm, _ := m.reRoot(t.TempDir())
	m = nm.(Model)
	// reRoot leaves the OLD repo's snapshot in m.repoHealth and only clears
	// repoHealthKnown, so the key must read as unresolved — otherwise the
	// switch's own config arrival loads the old repo's slots into the new one.
	if m.bfRepoKey() != "" {
		t.Errorf("bfRepoKey after a repo switch = %q, want \"\" until the new probe lands", m.bfRepoKey())
	}
	if m.branchFilterSlot[panelBranches] != 0 || m.bfSlotsLoaded {
		t.Errorf("slots survived the switch: slot=%d loaded=%v", m.branchFilterSlot[panelBranches], m.bfSlotsLoaded)
	}
	// The config arrival that follows a switch must NOT latch a load off the
	// stale key: the real probe (applyRepoHealth) is what resolves it.
	m = m.applyBranchFilterConfig()
	if m.bfSlotsLoaded {
		t.Error("loadBranchFilterSlots latched on an unresolved repo key")
	}
}

// bfSwitchedModel is a model in reRoot's post-switch state: the new repo's
// slot is remembered in promptstate and its config names slot 5, but neither
// the health probe nor the config has landed — branchFilters still holds the
// OLD repo's rules, under which slot 5 is empty (and so unusable).
func bfSwitchedModel(t *testing.T) (Model, string) {
	t.Helper()
	const newKey = "/new/.git"
	m := bfModel(t)
	if err := m.promptStore.SetBranchFilterSlot(newKey, promptstate.BranchFilterListBranches, 5); err != nil {
		t.Fatal(err)
	}
	m.branchFilterSlot = map[panel]int{}
	m.bfSlotsLoaded = false
	m.bfCfgApplied = false
	m.repoHealthKnown = false
	m.cfg.Branches.Filter = []branchfilter.Slot{{Slot: 5, Name: "new", Prefix: "feat/"}}
	return m, newKey
}

// bfProbeLands is applyRepoHealth's branch-filter half.
func bfProbeLands(m Model, key string) Model {
	m.repoHealth.GitCommonDir = key
	m.repoHealthKnown = true
	return m.loadBranchFilterSlots()
}

// reRoot batches loadCmd and repoHealthCmd, so the config and the probe race.
// Whichever lands second must complete the load — a probe that latched first
// would have validated slot 5 against the OLD repo's rules and dropped it.
func TestRepoSwitchLoadCompletesInEitherArrivalOrder(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		drive func(Model, string) Model
	}{
		{"probe first (the real race winner)", func(m Model, k string) Model {
			m = bfProbeLands(m, k)
			if m.bfSlotsLoaded {
				t.Error("the probe latched the load before this repo's rules were compiled")
			}
			return m.applyBranchFilterConfig()
		}},
		{"config first", func(m Model, k string) Model {
			m = m.applyBranchFilterConfig()
			if m.bfSlotsLoaded {
				t.Error("the config latched the load before the repo key resolved")
			}
			return bfProbeLands(m, k)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m, key := bfSwitchedModel(t)
			m = tc.drive(m, key)
			if !m.bfSlotsLoaded {
				t.Fatal("the load never completed")
			}
			if got := m.branchFilterSlot[panelBranches]; got != 5 {
				t.Errorf("remembered slot = %d, want 5 (validated against the OLD repo's rules?)", got)
			}
		})
	}
}

func TestUnnamedSlotLabelIsTranslatable(t *testing.T) {
	t.Parallel()
	m := bfModel(t)
	// Slot 4 carries no name, so the header falls back to "slot N" — which
	// must come from the bundle, not branchfilter's English Label().
	m.branchFilters, _ = branchfilter.CompileAll([]branchfilter.Slot{{Slot: 4, Prefix: "feat/"}})
	m.branchFilterSlot[panelBranches] = 4
	if got := m.branchFilterDecoration(panelBranches); got != " ▽4 slot 4 · 2 hidden" {
		t.Errorf("decoration = %q", got)
	}
	if got := bfLabel(m.branchFilters[3]); got != i18n.T("slot %d", 4) {
		t.Errorf("bfLabel = %q, want the translated fallback", got)
	}
}

func TestFooterAdvertisesBranchFilterKey(t *testing.T) {
	t.Parallel()
	m := bfModel(t)
	if !strings.Contains(m.footerLine(), "[alt+1-5] filter") {
		t.Errorf("footer = %q", m.footerLine())
	}
	m.focus = panelCommits
	if strings.Contains(m.footerLine(), "[alt+1-5]") {
		t.Errorf("footer on Commits should not advertise it: %q", m.footerLine())
	}
}

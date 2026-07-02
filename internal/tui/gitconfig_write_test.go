package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/model"
)

// isolateGlobalGitConfig points git's global/system config at throwaway
// locations so these tests never read or write the developer's real
// ~/.gitconfig. ExecRunner builds subprocess env from os.Environ(), so
// t.Setenv propagates to every git the model runs. Called before
// settingsModel in EVERY test in this file (even local-scope ones: the
// post-write GitConfigRows re-read scans global scope too).
func isolateGlobalGitConfig(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
}

// selectExplorerRow moves the explorer selection to key (must be visible).
func selectExplorerRow(t *testing.T, m Model, key string) Model {
	t.Helper()
	p := layerOf[*gitConfigPopup](m)
	if p == nil {
		t.Fatal("explorer not open")
	}
	for i, r := range p.visible() {
		if r.Key == key {
			p.sel = i
			return m
		}
	}
	t.Fatalf("row %q not visible", key)
	return m
}

// drainCmd executes cmds until msgs stop, feeding each back into Update —
// enough to drive the synchronous write cmd + the rows refresh it returns.
// tea.BatchMsg (the write + health batch) is flattened into its sub-commands,
// since the runtime — not Update — normally unpacks it.
func drainCmd(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for i := 0; i < 40 && len(queue) > 0; i++ {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		msg := c()
		if msg == nil {
			continue
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, batch...)
			continue
		}
		u, next := m.Update(msg)
		m = u.(Model)
		if next != nil {
			queue = append(queue, next)
		}
	}
	return m
}

func TestExplorerSetLocalBoolWritesConfig(t *testing.T) {
	isolateGlobalGitConfig(t)
	m, dir := settingsModel(t)
	m = openExplorer(t, m)
	if m.repoHealthKnown {
		t.Fatal("repoHealthKnown must be false before any write lands (openExplorer's `,` discards its own health cmd)")
	}
	m = selectExplorerRow(t, m, "fetch.writeCommitGraph")
	u, _ := m.Update(keyMsg("l"))
	m = u.(Model)
	p := layerOf[*gitConfigPopup](m)
	if p.edit == nil {
		t.Fatal("l on a curated bool must open the option editor")
	}
	// Option list: pick "true" (bool options are [true false]; optSel starts on
	// the current/default — move to be explicit).
	p.edit.optSel = 0
	u, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = drainCmd(t, u.(Model), cmd)

	out, err := exec.Command("git", "-C", dir, "config", "--local", "fetch.writeCommitGraph").Output()
	if err != nil || strings.TrimSpace(string(out)) != "true" {
		t.Fatalf("local value = %q, %v; want true", out, err)
	}
	if p := layerOf[*gitConfigPopup](m); p == nil || p.edit != nil {
		t.Fatal("after writing, the explorer stays open in browsing mode")
	}
	// The write cmd chains a post-write repo-health re-read (Finding 2):
	// prove it actually landed, not just that the write succeeded.
	if !m.repoHealthKnown {
		t.Fatal("repoHealthKnown must be true after the write — the chained health re-read must have applied")
	}
}

func TestExplorerSetGlobalStringWritesGlobalOnly(t *testing.T) {
	isolateGlobalGitConfig(t)
	m, dir := settingsModel(t)
	m = openExplorer(t, m)
	m = selectExplorerRow(t, m, "user.name")
	u, _ := m.Update(keyMsg("g"))
	m = u.(Model)
	p := layerOf[*gitConfigPopup](m)
	if p.edit == nil || p.edit.doc.Key != "user.name" || !p.edit.global {
		t.Fatalf("g must open a global editor, got %+v", p.edit)
	}
	// The field pre-fills with the current global value (the fixture's
	// "Ada L") — verify, then clear it before typing the new name.
	if got := p.edit.field.Value(); got != "Ada L" {
		t.Fatalf("field must pre-fill with the current global value, got %q", got)
	}
	for range "Ada L" {
		u, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		m = u.(Model)
	}
	for _, r := range "Test Person" {
		u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = u.(Model)
	}
	u, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = drainCmd(t, u.(Model), cmd)

	// isolateGlobalGitConfig points GIT_CONFIG_GLOBAL at a temp file —
	// verify the write really landed at global scope, not local.
	out, _ := exec.Command("git", "-C", dir, "config", "--global", "user.name").Output()
	if strings.TrimSpace(string(out)) != "Test Person" {
		t.Fatalf("global user.name = %q, want 'Test Person'", out)
	}
	if out, err := exec.Command("git", "-C", dir, "config", "--local", "user.name").Output(); err == nil && strings.TrimSpace(string(out)) != "" {
		t.Fatal("local scope must stay untouched")
	}
}

func TestExplorerUnsetOffersOnlySetScopes(t *testing.T) {
	isolateGlobalGitConfig(t)
	m, dir := settingsModel(t)
	if err := exec.Command("git", "-C", dir, "config", "--local", "fetch.writeCommitGraph", "true").Run(); err != nil {
		t.Fatal(err)
	}
	m = openExplorer(t, m)
	// Reload rows so the local value is visible to the popup state.
	p := layerOf[*gitConfigPopup](m)
	for i := range p.rows {
		if p.rows[i].Key == "fetch.writeCommitGraph" {
			p.rows[i].LocalSet, p.rows[i].LocalValue = true, "true"
		}
	}
	m = selectExplorerRow(t, m, "fetch.writeCommitGraph")
	u, _ := m.Update(keyMsg("u"))
	m = u.(Model)
	if p.edit == nil || !p.edit.unset {
		t.Fatal("u on a set curated row must open the unset chooser")
	}
	// Only the set scope is offered: local (set) + Cancel, no global.
	if got := strings.Join(p.edit.options, "|"); got != "Unset local|Cancel" {
		t.Fatalf("chooser options = %q, want only the set scope + Cancel", got)
	}
	u, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // first option = Unset local
	m = drainCmd(t, u.(Model), cmd)
	if err := exec.Command("git", "-C", dir, "config", "--local", "fetch.writeCommitGraph").Run(); err == nil {
		t.Fatal("local key must be unset")
	}
}

func TestExplorerUnsetOnFullyUnsetRowRefuses(t *testing.T) {
	isolateGlobalGitConfig(t)
	m, _ := settingsModel(t)
	m = openExplorer(t, m)
	m = selectExplorerRow(t, m, "add.ignoreErrors") // neither scope set
	u, _ := m.Update(keyMsg("u"))
	m = u.(Model)
	if p := layerOf[*gitConfigPopup](m); p.edit != nil {
		t.Fatal("u with nothing set must not open a chooser")
	}
	if !strings.Contains(m.statusMsg, "nothing to unset") {
		t.Fatalf("statusMsg = %q, want 'nothing to unset'", m.statusMsg)
	}
}

func TestExplorerWriteOnNonCuratedIsReadOnly(t *testing.T) {
	isolateGlobalGitConfig(t)
	m, _ := settingsModel(t)
	m = openExplorer(t, m)
	m = selectExplorerRow(t, m, "alias.lg")
	u, _ := m.Update(keyMsg("l"))
	m = u.(Model)
	if p := layerOf[*gitConfigPopup](m); p.edit != nil {
		t.Fatal("non-curated rows are read-only")
	}
	if !strings.Contains(m.statusMsg, "read-only") {
		t.Fatalf("statusMsg = %q, want a read-only explanation", m.statusMsg)
	}
}

func TestExplorerEscCancelsEditor(t *testing.T) {
	isolateGlobalGitConfig(t)
	m, _ := settingsModel(t)
	m = openExplorer(t, m)
	m = selectExplorerRow(t, m, "user.name")
	u, _ := m.Update(keyMsg("l"))
	m = u.(Model)
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = u.(Model)
	p := layerOf[*gitConfigPopup](m)
	if p == nil || p.edit != nil {
		t.Fatal("esc must cancel the editor back to browsing, not close the popup")
	}
}

func TestExplorerIntEditorFiltersRunes(t *testing.T) {
	isolateGlobalGitConfig(t)
	m, _ := settingsModel(t)
	m = openExplorer(t, m)
	p := layerOf[*gitConfigPopup](m)
	p.rows = append(p.rows, model.GitConfigRow{Key: "gc.auto"}) // KindInt, unset
	m = selectExplorerRow(t, m, "gc.auto")
	u, _ := m.Update(keyMsg("l"))
	m = u.(Model)
	if p.edit == nil || !p.edit.useField {
		t.Fatal("l on a curated int must open the text-field editor")
	}
	for _, r := range "-1a2b3" {
		u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = u.(Model)
	}
	if got := p.edit.field.Value(); got != "-123" {
		t.Fatalf("int field = %q, want digits + leading '-' only (-123)", got)
	}
}

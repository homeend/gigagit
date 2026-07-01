package tui

import (
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/promptstate"
)

// The related-prompt registry: flipping a Settings option may ask ONE
// follow-up about a related option. Trigger = setting id + a precondition
// against the live config; a globally suppressed id never fires.

// promptTestModel returns a loaded model with a temp-file prompt store, so
// tests never read or write the developer's real <state>/gg/prompts.toml.
func promptTestModel(t *testing.T) (Model, promptstate.Store) {
	t.Helper()
	m, dir := settingsModel(t)
	m.repoConfigPath = filepath.Join(dir, ".gg.toml")
	st := promptstate.NewFileStore(filepath.Join(t.TempDir(), "prompts.toml"))
	m.promptStore = st
	return m, st
}

func TestPromptFiresOnGraphOffWhenSortIsDateOrder(t *testing.T) {
	m, _ := promptTestModel(t)
	m.cfg.UI.CommitSort = "date-order"
	rp := m.relatedPromptFor(settingShowGraph, "off")
	if rp == nil {
		t.Fatal("show_graph→off with commit_sort=date-order must offer the plain prompt")
	}
	if rp.id != "show_graph_off.commit_sort_plain" {
		t.Fatalf("prompt id = %q, want show_graph_off.commit_sort_plain", rp.id)
	}
}

func TestPromptSilentWhenSortAlreadyPlain(t *testing.T) {
	m, _ := promptTestModel(t)
	m.cfg.UI.CommitSort = "plain"
	if rp := m.relatedPromptFor(settingShowGraph, "off"); rp != nil {
		t.Fatalf("commit_sort already plain: nothing to offer, got %q", rp.id)
	}
}

func TestPromptFiresOnGraphOnWhenSortIsPlain(t *testing.T) {
	m, _ := promptTestModel(t)
	m.cfg.UI.CommitSort = "plain"
	rp := m.relatedPromptFor(settingShowGraph, "on")
	if rp == nil {
		t.Fatal("show_graph→on with commit_sort=plain must offer the date-order prompt")
	}
	if rp.id != "show_graph_on.commit_sort_dateorder" {
		t.Fatalf("prompt id = %q, want show_graph_on.commit_sort_dateorder", rp.id)
	}
}

func TestPromptSilentWhenSortUnset(t *testing.T) {
	// Unset commit_sort resolves to date-order (commitSort()), so switching the
	// graph ON has nothing to offer — the effective mode is already date-order.
	m, _ := promptTestModel(t)
	m.cfg.UI.CommitSort = ""
	if rp := m.relatedPromptFor(settingShowGraph, "on"); rp != nil {
		t.Fatalf("effective date-order: nothing to offer on graph-on, got %q", rp.id)
	}
}

func TestSuppressedPromptNeverFires(t *testing.T) {
	m, st := promptTestModel(t)
	m.cfg.UI.CommitSort = "date-order"
	if err := st.SuppressPrompt("show_graph_off.commit_sort_plain"); err != nil {
		t.Fatal(err)
	}
	if rp := m.relatedPromptFor(settingShowGraph, "off"); rp != nil {
		t.Fatalf("suppressed prompt must never fire, got %q", rp.id)
	}
}

func TestNilStoreStillPrompts(t *testing.T) {
	// No state dir → nil store → prompts still work (suppression just can't
	// persist). The registry must not panic on a nil store.
	m, _ := promptTestModel(t)
	m.promptStore = nil
	m.cfg.UI.CommitSort = "date-order"
	if m.relatedPromptFor(settingShowGraph, "off") == nil {
		t.Fatal("nil store must not disable prompts")
	}
}

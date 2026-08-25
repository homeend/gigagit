package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
)

const gotoFullHash = "1234567890abcdef1234567890abcdef12345678"

// gotoModel builds an idle model whose svc resolves rev-parse to resolveTo. An
// empty resolveTo leaves rev-parse unconfigured, so the FakeRunner errors and
// RevParse fails — the unknown-revision path.
func gotoModel(t *testing.T, resolveTo string) Model {
	t.Helper()
	f := gitexec.NewFakeRunner()
	if resolveTo != "" {
		f.SetResponse("git rev-parse", gitexec.Result{Stdout: resolveTo + "\n"})
	}
	m := footerModel()
	m.svc = domain.New(&git.Repo{Runner: f})
	return m
}

// send routes any tea.Msg (key or async result) through Update, keeping the cmd.
func send(m Model, msg tea.Msg) (Model, tea.Cmd) {
	u, cmd := m.Update(msg)
	return u.(Model), cmd
}

// # opens the show-commit popup from any panel (it is a global base-layout key).
func TestGotoCommitOpensFromAnyPanel(t *testing.T) {
	t.Parallel()
	for _, p := range []panel{panelBranches, panelCommits, panelFiles} {
		m := gotoModel(t, gotoFullHash)
		m.focus = p
		m, _ = send(m, key("#"))
		if layerOf[*gotoCommitPopup](m) == nil {
			t.Errorf("# on panel %v should push the show-commit popup", p)
		}
	}
}

// A valid commit-ish resolves and opens its files on the tree, closing the popup.
func TestGotoCommitGoodSHAOpensFiles(t *testing.T) {
	t.Parallel()
	m := gotoModel(t, gotoFullHash)
	m, _ = send(m, key("#"))
	m = typeRunes(t, m, "abc")
	m, cmd := send(m, keyType(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("enter on a non-empty input should fire the resolve cmd")
	}
	if layerOf[*gotoCommitPopup](m) == nil {
		t.Fatal("popup should stay open while resolving")
	}
	// Run the resolve through Update (a cmd-non-nil check would miss the tag-gate).
	m, _ = send(m, cmd())
	if layerOf[*gotoCommitPopup](m) != nil {
		t.Fatal("a successful resolve should close the popup")
	}
	if m.filesView == nil {
		t.Fatal("a successful resolve should open the files view")
	}
	if m.filesHash != gotoFullHash {
		t.Errorf("files view hash = %q, want the resolved full hash", m.filesHash)
	}
	if m.focus != panelCommits || !m.filesTreeFocused {
		t.Errorf("by-hash open should focus the Commits panel on the tree; focus=%v tree=%v", m.focus, m.filesTreeFocused)
	}
}

// An unresolved ref shows an inline error and keeps the popup open — no files view.
func TestGotoCommitBadSHAInlineError(t *testing.T) {
	t.Parallel()
	m := gotoModel(t, "") // rev-parse errors
	m, _ = send(m, key("#"))
	m = typeRunes(t, m, "bogus")
	m, cmd := send(m, keyType(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("enter should fire the resolve cmd")
	}
	m, _ = send(m, cmd())
	p := layerOf[*gotoCommitPopup](m)
	if p == nil {
		t.Fatal("a failed resolve must keep the popup open")
	}
	if p.err == "" {
		t.Error("a failed resolve must set an inline error")
	}
	if p.resolving {
		t.Error("resolving should be cleared after the result lands")
	}
	if m.filesView != nil {
		t.Error("a failed resolve must not open the files view")
	}
}

// A resolve result for stale text (the field was edited after submit) is dropped.
func TestGotoCommitStaleResolveRejected(t *testing.T) {
	t.Parallel()
	m := gotoModel(t, gotoFullHash)
	m, _ = send(m, key("#"))
	m = typeRunes(t, m, "abc")
	m, _ = send(m, keyType(tea.KeyEnter)) // fires resolve for "abc"
	m = typeRunes(t, m, "d")              // input is now "abcd"
	m, _ = send(m, gotoCommitResolvedMsg{rev: "abc", hash: gotoFullHash})
	if layerOf[*gotoCommitPopup](m) == nil {
		t.Fatal("a stale resolve (input edited) must not close the popup")
	}
	if m.filesView != nil {
		t.Fatal("a stale resolve must not open the files view")
	}
}

// esc closes the popup without resolving.
func TestGotoCommitEscCloses(t *testing.T) {
	t.Parallel()
	m := gotoModel(t, gotoFullHash)
	m, _ = send(m, key("#"))
	m, _ = send(m, keyType(tea.KeyEsc))
	if layerOf[*gotoCommitPopup](m) != nil {
		t.Fatal("esc should close the show-commit popup")
	}
}

// An ANNOTATED tag resolves to the commit it points at (not the tag object) and
// the files actually LOAD (run the load cmd through Update — "landed", not just
// "requested"). Real repo: the bug only shows against a true annotated tag.
func TestGotoCommitAnnotatedTagLoadsFiles(t *testing.T) {
	t.Parallel()
	dir, repo := newRepoDir(t)
	gitIn(t, dir, "tag", "-a", "v1.0", "-m", "release one")
	wantHash := gitOut(t, dir, "rev-parse", "v1.0^{commit}")
	tagObj := gitOut(t, dir, "rev-parse", "v1.0")
	if tagObj == wantHash {
		t.Fatal("setup: v1.0 should be an annotated tag (tag object != commit)")
	}

	m := footerModel()
	m.svc = domain.New(repo)
	m, _ = send(m, key("#"))
	m = typeRunes(t, m, "v1.0")
	m, cmd := send(m, keyType(tea.KeyEnter))
	m, loadCmd := send(m, cmd()) // run the real rev-parse resolve
	if layerOf[*gotoCommitPopup](m) != nil {
		t.Fatal("an annotated tag should resolve and close the popup")
	}
	if m.filesHash != wantHash {
		t.Fatalf("filesHash = %q, want the peeled commit %q (got the tag object %q)", m.filesHash, wantHash, tagObj)
	}
	if loadCmd == nil {
		t.Fatal("opening the files view should return a load cmd")
	}
	m, _ = send(m, loadCmd()) // run the files load — assert it LANDS
	var sb strings.Builder
	for _, ln := range m.filesView.lines {
		sb.WriteString(ln.text + "\n")
	}
	got := sb.String()
	if strings.Contains(got, "(loading…)") || !strings.Contains(got, "README") {
		t.Fatalf("files view should list the commit's files, got %q", got)
	}
}

// The render path draws the popup and surfaces the inline error after a failed
// resolve (guards the green-unit/broken-render class).
func TestGotoCommitRendersWithError(t *testing.T) {
	t.Parallel()
	m := gotoModel(t, "") // rev-parse errors
	m, _ = send(m, key("#"))
	m = typeRunes(t, m, "bogus")
	m, cmd := send(m, keyType(tea.KeyEnter))
	m, _ = send(m, cmd())
	out := m.View()
	for _, want := range []string{"Show commit", "no such commit: bogus"} {
		if !strings.Contains(out, want) {
			t.Errorf("goto-commit render missing %q", want)
		}
	}
}

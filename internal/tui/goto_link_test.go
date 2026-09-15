package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/repos"
)

// gotoLinkModel is a loaded nav model (a.txt with an unstaged edit on line 18)
// whose repo registry is an injected temp file, plus its checkout's top level.
func gotoLinkModel(t *testing.T) (Model, string) {
	t.Helper()
	m := loadedNavModel(t)
	m.statePath = filepath.Join(t.TempDir(), "repos.toml")
	top, err := m.svc.TopLevel(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return m, top
}

// linkTo spells the local (absolute-path) link form for dir plus rest.
func linkTo(dir, rest string) string { return "gg://" + filepath.ToSlash(dir) + rest }

// pasteLink opens the # prompt, pastes text as ONE KeyRunes message (what a
// terminal paste delivers) and presses enter; it returns the resolve cmd.
func pasteLink(t *testing.T, m Model, text string) (Model, tea.Cmd) {
	t.Helper()
	m, _ = send(m, key("#"))
	m, _ = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
	p := layerOf[*gotoCommitPopup](m)
	if p == nil {
		t.Fatal("# must open the prompt")
	}
	if got := p.input.Value(); got != text {
		t.Fatalf("pasted value = %q, want %q", got, text)
	}
	m, cmd := send(m, keyType(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("enter on a link must fire the resolve cmd")
	}
	if !p.resolving {
		t.Fatal("the prompt must mark the resolve in flight")
	}
	return m, cmd
}

// A link into THIS checkout closes the prompt and lands on the line through
// the steering pipeline — the same landing a `gg session navigate` takes.
func TestGotoLinkSameCheckoutLandsOnTheLine(t *testing.T) {
	t.Parallel()
	m, top := gotoLinkModel(t)
	m, cmd := pasteLink(t, m, linkTo(top, "/a.txt:18"))
	m, cmd = send(m, cmd())
	if layerOf[*gotoCommitPopup](m) != nil {
		t.Fatal("a resolved link must close the prompt")
	}
	if m.pendingSteer == nil {
		t.Fatal("the navigate must park a pendingSteer until the diff loads")
	}
	m = pumpDiff(t, m, cmd)
	v := m.diffLayer()
	if v == nil {
		t.Fatal("no diff view after the landing")
	}
	row, ok := v.cursorRow()
	if !ok || row.RightNo != 18 {
		t.Fatalf("cursor row = %+v ok=%v, want new line 18", row, ok)
	}
	if strings.Contains(m.diffNotice, "agent") || m.diffNotice == "" {
		t.Errorf("a pasted link is the user's own doing; notice = %q", m.diffNotice)
	}
}

// A link that resolves to nothing keeps the prompt open with an inline error.
func TestGotoLinkUnresolvableStaysWithAnInlineError(t *testing.T) {
	t.Parallel()
	m, _ := gotoLinkModel(t)
	m, cmd := pasteLink(t, m, "gg://no-such-repo-anywhere/a.txt:1")
	m, _ = send(m, cmd())
	p := layerOf[*gotoCommitPopup](m)
	if p == nil {
		t.Fatal("an unresolvable link must keep the prompt open")
	}
	if p.resolving || !strings.HasPrefix(p.err, i18n.T("cannot open link: %s", "")) {
		t.Errorf("resolving=%v err=%q", p.resolving, p.err)
	}
	if !strings.Contains(m.View(), "cannot open link") {
		t.Error("the inline error must render")
	}
}

// A bare link to THIS checkout has nowhere to go: the prompt closes with a
// notice, and nothing else moves.
func TestGotoLinkBareSameCheckoutIsANotice(t *testing.T) {
	t.Parallel()
	m, top := gotoLinkModel(t)
	m, cmd := pasteLink(t, m, linkTo(top, ""))
	m, _ = send(m, cmd())
	if layerOf[*gotoCommitPopup](m) != nil || m.diffLayer() != nil || m.pendingSteer != nil {
		t.Fatal("a bare same-checkout link must only close the prompt")
	}
	if m.statusMsg != i18n.T("that link names this checkout") {
		t.Errorf("status = %q", m.statusMsg)
	}
}

// otherCheckout builds a second nav repo and registers it in m's registry, so
// the resolver can find it the way it finds any repo gg opened before.
func otherCheckout(t *testing.T, m Model) string {
	t.Helper()
	other := navRepo(t)
	if err := repos.Touch(m.statePath, other, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	return other
}

// A link into ANOTHER checkout turns the prompt into a confirm; enter switches
// the session there and arms the --at gate with the resolved landing, so the
// navigate fires only once the new repo has loaded (and, for a preview link,
// its previews have been read).
func TestGotoLinkOtherCheckoutAsksThenSwitchesAndArmsTheLanding(t *testing.T) {
	t.Parallel()
	m, top := gotoLinkModel(t)
	other := otherCheckout(t, m)
	m.startAtPreviewsSeen = true // stale from this repo's startup; the switch must reset it
	m, cmd := pasteLink(t, m, linkTo(other, "/a.txt:18"))
	m, _ = send(m, cmd())
	p := layerOf[*gotoCommitPopup](m)
	if p == nil || p.pending == nil {
		t.Fatal("a link into another checkout must ask before switching")
	}
	if !domain.SamePath(p.pending.checkout, other) || p.pending.bare {
		t.Fatalf("pending = %+v", p.pending)
	}
	if out := m.View(); !strings.Contains(out, "names another checkout") || !strings.Contains(out, "[enter] switch there") {
		t.Error("the confirm must render the checkout and its keys")
	}
	m, _ = send(m, keyType(tea.KeyEnter))
	if layerOf[*gotoCommitPopup](m) != nil {
		t.Fatal("enter on the confirm must close the prompt")
	}
	if !domain.SamePath(m.svc.Root(), other) || domain.SamePath(m.svc.Root(), top) {
		t.Fatalf("svc root = %q, want the other checkout %q", m.svc.Root(), other)
	}
	if !m.loading {
		t.Error("the switch must reload the new repo")
	}
	if !m.startAtPending || m.startAt.Path != "a.txt" || m.startAt.Line != 18 || !domain.SamePath(m.startAt.Repo.Abs, other) {
		t.Errorf("startAt = %+v pending=%v, want the landing armed", m.startAt, m.startAtPending)
	}
	if m.startAtPreviewsSeen {
		t.Error("the switch must forget the OLD repo's previews read")
	}
	if m.startAtReady() {
		t.Error("the landing must not fire before the new repo has loaded")
	}
}

// Esc on the confirm stays put; editing the text withdraws the confirm.
func TestGotoLinkOtherCheckoutEscStaysAndEditingWithdraws(t *testing.T) {
	t.Parallel()
	m, top := gotoLinkModel(t)
	other := otherCheckout(t, m)
	m, cmd := pasteLink(t, m, linkTo(other, "/a.txt:18"))
	m, _ = send(m, cmd())
	p := layerOf[*gotoCommitPopup](m)
	if p == nil || p.pending == nil {
		t.Fatal("expected the confirm")
	}
	m, _ = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if p.pending != nil {
		t.Error("editing the text must withdraw the pending switch")
	}
	m, _ = send(m, keyType(tea.KeyEnter)) // re-resolve the edited (now bogus) text…
	m, _ = send(m, keyType(tea.KeyEsc))   // …and leave
	if layerOf[*gotoCommitPopup](m) != nil {
		t.Fatal("esc must close the prompt")
	}
	if !domain.SamePath(m.svc.Root(), top) || m.startAtPending {
		t.Errorf("esc must not switch: root=%q pending=%v", m.svc.Root(), m.startAtPending)
	}
}

// A bare link into another checkout switches there with NO landing armed.
func TestGotoLinkBareOtherCheckoutSwitchesWithoutALanding(t *testing.T) {
	t.Parallel()
	m, _ := gotoLinkModel(t)
	other := otherCheckout(t, m)
	m, cmd := pasteLink(t, m, linkTo(other, ""))
	m, _ = send(m, cmd())
	p := layerOf[*gotoCommitPopup](m)
	if p == nil || p.pending == nil || !p.pending.bare {
		t.Fatal("a bare link into another checkout must ask before switching")
	}
	m, _ = send(m, keyType(tea.KeyEnter))
	if !domain.SamePath(m.svc.Root(), other) {
		t.Fatalf("svc root = %q, want %q", m.svc.Root(), other)
	}
	if m.startAtPending {
		t.Error("a bare link arms no landing")
	}
}

// Launched from the command palette's "Open gg:// link…", a resolved link
// unwinds the palette too, so the landing opens over the base.
func TestGotoLinkFromThePaletteUnwindsThePalette(t *testing.T) {
	t.Parallel()
	m, top := gotoLinkModel(t)
	m, _ = send(m, key("ctrl+p"))
	cp := layerOf[*commandPalette](m)
	if cp == nil {
		t.Fatal("ctrl+p must open the palette")
	}
	var entry *paletteCommand
	for i := range cp.cmds {
		if cp.cmds[i].label == i18n.T("Open gg:// link…") {
			entry = &cp.cmds[i]
		}
	}
	if entry == nil {
		t.Fatal("the palette must list Open gg:// link…")
	}
	m, _ = entry.run(m)
	if layerOf[*gotoCommitPopup](m) == nil {
		t.Fatal("the palette entry must open the # prompt")
	}
	m, _ = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(linkTo(top, ""))})
	m, cmd := send(m, keyType(tea.KeyEnter))
	m, _ = send(m, cmd())
	if m.topLayer() != nil {
		t.Errorf("both the prompt and the palette must be gone, top = %T", m.topLayer())
	}
}

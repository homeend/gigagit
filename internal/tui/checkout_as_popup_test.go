package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

func TestSuggestLocalNameFirstFree(t *testing.T) {
	t.Parallel()
	bs := []model.Branch{{Name: "foo"}, {Name: "main"}}
	if got := suggestLocalName(bs, "foo"); got != "foo-2" {
		t.Fatalf("suggest = %q, want foo-2", got)
	}
}

func TestSuggestLocalNameSkipsTaken(t *testing.T) {
	t.Parallel()
	bs := []model.Branch{{Name: "foo"}, {Name: "foo-2"}, {Name: "foo-3"}}
	if got := suggestLocalName(bs, "foo"); got != "foo-4" {
		t.Fatalf("suggest = %q, want foo-4", got)
	}
}

func TestCheckoutAsPopupEnterDispatchesAndArmsPending(t *testing.T) {
	t.Parallel()
	m := remoteModel()
	m = m.openCheckoutAsPopup("origin/foo", "foo", engine.CheckoutSwitch)
	p, ok := m.topLayer().(*checkoutAsPopup)
	if !ok {
		t.Fatalf("expected checkoutAsPopup on top; got %T", m.topLayer())
	}
	nm, cmd := p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter must dispatch the checkout op")
	}
	if nm.pendingCheckout.remoteRef != "origin/foo" || nm.pendingCheckout.base != "foo" ||
		nm.pendingCheckout.intent != engine.CheckoutSwitch {
		t.Fatalf("pendingCheckout = %+v, want origin/foo/foo/switch", nm.pendingCheckout)
	}
	if _, still := nm.topLayer().(*checkoutAsPopup); still {
		t.Fatal("popup must close on enter")
	}
}

func TestCheckoutAsPopupEmptyNameRefuses(t *testing.T) {
	t.Parallel()
	m := remoteModel()
	m = m.openCheckoutAsPopup("origin/foo", "", engine.CheckoutStay)
	p := m.topLayer().(*checkoutAsPopup)
	nm, cmd := p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("empty name must not dispatch")
	}
	if _, still := nm.topLayer().(*checkoutAsPopup); !still {
		t.Fatal("popup must stay open on empty name")
	}
}

func TestCheckoutAsPopupEscCancels(t *testing.T) {
	t.Parallel()
	m := remoteModel()
	m = m.openCheckoutAsPopup("origin/foo", "foo", engine.CheckoutStay)
	p := m.topLayer().(*checkoutAsPopup)
	nm, _ := p.update(m, tea.KeyMsg{Type: tea.KeyEsc})
	if _, still := nm.topLayer().(*checkoutAsPopup); still {
		t.Fatal("esc must close the popup")
	}
	if nm.pendingCheckout.remoteRef != "" {
		t.Fatal("esc must not arm pendingCheckout")
	}
}

func TestDivergedCheckoutOpensRecoveryModal(t *testing.T) {
	t.Parallel()
	m := remoteModel()
	m.branches = []model.Branch{{Name: "foo"}, {Name: "foo-2"}}
	m.pendingCheckout = pendingCheckout{remoteRef: "origin/foo", base: "foo", intent: engine.CheckoutSwitch}
	nm, _ := m.Update(opFinishedMsg{err: engine.CheckoutDivergedError{Local: "foo", RemoteRef: "origin/foo"}})
	rm := nm.(Model)
	if rm.modal == nil || rm.modal.req.ID != "checkout-diverged" {
		t.Fatalf("expected checkout-diverged modal; modal=%+v", rm.modal)
	}
	if rm.pendingCheckout.remoteRef != "" {
		t.Fatal("pendingCheckout must be cleared unconditionally at opFinishedMsg")
	}
	nm2, _ := rm.resolveModal("check out as different name…")
	rm2 := nm2.(Model)
	p, ok := rm2.topLayer().(*checkoutAsPopup)
	if !ok {
		t.Fatalf("expected checkoutAsPopup after rename choice; got %T", rm2.topLayer())
	}
	if got := p.name.Value(); got != "foo-3" {
		t.Fatalf("prefill = %q, want foo-3 (foo and foo-2 taken)", got)
	}
	if p.intent != engine.CheckoutSwitch {
		t.Fatal("recovery must keep the original intent")
	}
}

func TestDivergedRecoveryCancelDoesNothing(t *testing.T) {
	t.Parallel()
	m := remoteModel()
	m.pendingCheckout = pendingCheckout{remoteRef: "origin/foo", base: "foo", intent: engine.CheckoutStay}
	nm, _ := m.Update(opFinishedMsg{err: engine.CheckoutDivergedError{Local: "foo", RemoteRef: "origin/foo"}})
	rm := nm.(Model)
	nm2, cmd := rm.resolveModal("cancel")
	if cmd != nil {
		t.Fatal("cancel must not start an op")
	}
	if _, isPopup := nm2.(Model).topLayer().(*checkoutAsPopup); isPopup {
		t.Fatal("cancel must not open the popup")
	}
}

func TestNonDivergedErrorSkipsRecoveryModal(t *testing.T) {
	t.Parallel()
	m := remoteModel()
	m.pendingCheckout = pendingCheckout{remoteRef: "origin/foo", base: "foo", intent: engine.CheckoutStay}
	nm, _ := m.Update(opFinishedMsg{err: errors.New("boom")})
	rm := nm.(Model)
	if rm.modal != nil {
		t.Fatalf("plain error must not open the recovery modal; modal=%+v", rm.modal)
	}
	if rm.pendingCheckout.remoteRef != "" {
		t.Fatal("pendingCheckout must still be cleared")
	}
}

func TestDivergedErrorWithoutPendingSkipsModal(t *testing.T) {
	t.Parallel()
	m := remoteModel() // pendingCheckout zero — e.g. error surfaced by a CLI-driven repo
	nm, _ := m.Update(opFinishedMsg{err: engine.CheckoutDivergedError{Local: "foo", RemoteRef: "origin/foo"}})
	if rm := nm.(Model); rm.modal != nil {
		t.Fatalf("no pending checkout → no modal; modal=%+v", rm.modal)
	}
}

func TestReRootClearsPendingCheckout(t *testing.T) {
	t.Parallel()
	m := footerModel()
	m.pendingCheckout = pendingCheckout{remoteRef: "origin/foo", base: "foo", intent: engine.CheckoutStay}
	updated, _ := m.reRoot(t.TempDir())
	if got := updated.(Model); got.pendingCheckout.remoteRef != "" {
		t.Fatalf("pendingCheckout = %+v after reRoot, want zero", got.pendingCheckout)
	}
}

func TestRemoteCheckoutKeyArmsPending(t *testing.T) {
	t.Parallel()
	m := remoteModel()
	m.cfg.UI.DisableSlowOpConfirm = true // dispatch directly; pending arming is what's under test
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	rm := nm.(Model)
	if rm.pendingCheckout.remoteRef != "origin/foo" || rm.pendingCheckout.base != "foo" ||
		rm.pendingCheckout.intent != engine.CheckoutStay {
		t.Fatalf("pendingCheckout = %+v, want origin/foo/foo/stay", rm.pendingCheckout)
	}
}

func TestRemoteSwitchKeyArmsPending(t *testing.T) {
	t.Parallel()
	m := remoteModel()
	m.cfg.UI.DisableSlowOpConfirm = true // dispatch directly; pending arming is what's under test
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	rm := nm.(Model)
	if rm.pendingCheckout.remoteRef != "origin/foo" || rm.pendingCheckout.base != "foo" ||
		rm.pendingCheckout.intent != engine.CheckoutSwitch {
		t.Fatalf("pendingCheckout = %+v, want origin/foo/foo/switch", rm.pendingCheckout)
	}
}

func TestCurrentBranchCheckoutOpensModalWithPull(t *testing.T) {
	t.Parallel()
	m := remoteModel() // rb = origin/foo
	m.status.Branch = "foo"
	m.status.Upstream = "origin/foo"
	m.status.Behind = 3
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	rm := nm.(Model)
	if cmd != nil {
		t.Fatal("opening the modal must not dispatch an op")
	}
	if rm.modal == nil || rm.modal.req.ID != "checkout-current-branch" {
		t.Fatalf("expected checkout-current-branch modal; modal=%+v", rm.modal)
	}
	if got := rm.modal.req.Options; len(got) != 3 || got[0] != "pull now" || got[2] != "cancel" {
		t.Fatalf("options = %v, want [pull now, check out as different name…, cancel]", got)
	}
	if !strings.Contains(rm.modal.req.Prompt, "behind origin/foo by 3") {
		t.Fatalf("prompt = %q, want behind-by-3 wording", rm.modal.req.Prompt)
	}
	if rm.pendingCheckout.remoteRef != "" {
		t.Fatal("the modal path must not arm pendingCheckout")
	}
	if _, cmd2 := rm.resolveModal("pull now"); cmd2 == nil {
		t.Fatal("pull now must dispatch the pull op")
	}
}

func TestCurrentBranchModalNoPullWhenNotBehind(t *testing.T) {
	t.Parallel()
	m := remoteModel()
	m.status.Branch = "foo"
	m.status.Upstream = "origin/foo"
	m.status.Behind = 0
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	rm := nm.(Model)
	if rm.modal == nil || rm.modal.req.ID != "checkout-current-branch" {
		t.Fatalf("expected checkout-current-branch modal; modal=%+v", rm.modal)
	}
	for _, o := range rm.modal.req.Options {
		if o == "pull now" {
			t.Fatal("pull now must be absent when the branch is not behind")
		}
	}
	if !strings.Contains(rm.modal.req.Prompt, "already contains origin/foo") {
		t.Fatalf("prompt = %q, want already-contains wording", rm.modal.req.Prompt)
	}
}

func TestCurrentBranchModalNoPullOnNonUpstreamRemote(t *testing.T) {
	t.Parallel()
	m := remoteModel() // rb = origin/foo …
	m.status.Branch = "foo"
	m.status.Upstream = "upstream/foo" // …but foo tracks a DIFFERENT remote
	m.status.Behind = 5
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	rm := nm.(Model)
	if rm.modal == nil || rm.modal.req.ID != "checkout-current-branch" {
		t.Fatalf("expected checkout-current-branch modal; modal=%+v", rm.modal)
	}
	for _, o := range rm.modal.req.Options {
		if o == "pull now" {
			t.Fatal("pull now must be absent when the selected remote is not the upstream")
		}
	}
	if strings.Contains(rm.modal.req.Prompt, "already contains") {
		t.Fatalf("prompt = %q must not claim containment for a non-upstream remote", rm.modal.req.Prompt)
	}
}

func TestCurrentBranchModalRenameOpensPopupWithIntent(t *testing.T) {
	t.Parallel()
	m := remoteModel()
	m.branches = []model.Branch{{Name: "foo"}}
	m.status.Branch = "foo"
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}}) // s → switch intent
	rm := nm.(Model)
	if rm.modal == nil || rm.modal.req.ID != "checkout-current-branch" {
		t.Fatalf("expected checkout-current-branch modal; modal=%+v", rm.modal)
	}
	nm2, _ := rm.resolveModal("check out as different name…")
	p, ok := nm2.(Model).topLayer().(*checkoutAsPopup)
	if !ok {
		t.Fatalf("expected checkoutAsPopup after rename choice; got %T", nm2.(Model).topLayer())
	}
	if got := p.name.Value(); got != "foo-2" {
		t.Fatalf("prefill = %q, want foo-2", got)
	}
	if p.intent != engine.CheckoutSwitch {
		t.Fatal("s must carry CheckoutSwitch into the popup")
	}
}

func TestCurrentBranchModalCancelInert(t *testing.T) {
	t.Parallel()
	m := remoteModel()
	m.status.Branch = "foo"
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	rm := nm.(Model)
	nm2, cmd := rm.resolveModal("cancel")
	if cmd != nil {
		t.Fatal("cancel must not start an op")
	}
	if _, isPopup := nm2.(Model).topLayer().(*checkoutAsPopup); isPopup {
		t.Fatal("cancel must not open the popup")
	}
}

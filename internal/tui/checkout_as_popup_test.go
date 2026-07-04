package tui

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

func TestSuggestLocalNameFirstFree(t *testing.T) {
	bs := []model.Branch{{Name: "foo"}, {Name: "main"}}
	if got := suggestLocalName(bs, "foo"); got != "foo-2" {
		t.Fatalf("suggest = %q, want foo-2", got)
	}
}

func TestSuggestLocalNameSkipsTaken(t *testing.T) {
	bs := []model.Branch{{Name: "foo"}, {Name: "foo-2"}, {Name: "foo-3"}}
	if got := suggestLocalName(bs, "foo"); got != "foo-4" {
		t.Fatalf("suggest = %q, want foo-4", got)
	}
}

func TestCheckoutAsPopupEnterDispatchesAndArmsPending(t *testing.T) {
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
	m := remoteModel() // pendingCheckout zero — e.g. error surfaced by a CLI-driven repo
	nm, _ := m.Update(opFinishedMsg{err: engine.CheckoutDivergedError{Local: "foo", RemoteRef: "origin/foo"}})
	if rm := nm.(Model); rm.modal != nil {
		t.Fatalf("no pending checkout → no modal; modal=%+v", rm.modal)
	}
}

func TestReRootClearsPendingCheckout(t *testing.T) {
	m := footerModel()
	m.pendingCheckout = pendingCheckout{remoteRef: "origin/foo", base: "foo", intent: engine.CheckoutStay}
	updated, _ := m.reRoot(t.TempDir())
	if got := updated.(Model); got.pendingCheckout.remoteRef != "" {
		t.Fatalf("pendingCheckout = %+v after reRoot, want zero", got.pendingCheckout)
	}
}

func TestRemoteCheckoutKeyArmsPending(t *testing.T) {
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
	m := remoteModel()
	m.cfg.UI.DisableSlowOpConfirm = true // dispatch directly; pending arming is what's under test
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	rm := nm.(Model)
	if rm.pendingCheckout.remoteRef != "origin/foo" || rm.pendingCheckout.base != "foo" ||
		rm.pendingCheckout.intent != engine.CheckoutSwitch {
		t.Fatalf("pendingCheckout = %+v, want origin/foo/foo/switch", rm.pendingCheckout)
	}
}

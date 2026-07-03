package tui

import (
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

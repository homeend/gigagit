package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

// checkoutAsPopup collects the local branch name for materializing a remote
// ref (SmartCheckout with a caller-chosen Local). Enter dispatches directly —
// this popup IS the confirmation; esc cancels. Mirrors commitNamePopup.
type checkoutAsPopup struct {
	remoteRef string // short remote ref, e.g. "origin/foo"
	intent    engine.CheckoutIntent
	name      textfield
}

func (p *checkoutAsPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyEnter:
		name := strings.TrimSpace(p.name.Value())
		if name == "" {
			return m, nil // a local branch needs a name; esc cancels
		}
		remoteRef, intent := p.remoteRef, p.intent
		m = m.popLayer()
		// Arm the diverged-recovery hook for THIS dispatch: base is the typed
		// name, so a re-collision suggests name-2, not the original branch-2.
		m.pendingCheckout = pendingCheckout{remoteRef: remoteRef, base: name, intent: intent}
		return m.startOp(engine.SmartCheckout{RemoteRef: remoteRef, Local: name, Intent: intent})
	default:
		p.name.HandleEditKey(msg)
	}
	return m, nil
}

func (p *checkoutAsPopup) render(m Model, below string) string {
	verb := "check out"
	if p.intent == engine.CheckoutSwitch {
		verb = "switch"
	}
	w, h := m.overlayDims()
	var b strings.Builder
	b.WriteString("Check out " + p.remoteRef + " as\n\n")
	b.WriteString(viewField("name: ", p.name, true, popupContentWidth(w)) + "\n\n")
	b.WriteString("[enter] " + verb + "   [esc] cancel")
	box := modalStyle.Width(popupInnerWidth(w)).Render(b.String()) + "\n"
	return overlayCenter(clipToHeight(below, h), box, w, h)
}

// openCheckoutAsPopup pushes the name popup for materializing remoteRef under
// a user-chosen local name; prefill seeds the text field.
func (m Model) openCheckoutAsPopup(remoteRef, prefill string, intent engine.CheckoutIntent) Model {
	return m.pushLayer(&checkoutAsPopup{remoteRef: remoteRef, intent: intent, name: newTextField(prefill)})
}

// suggestLocalName returns the first of base-2, base-3, … that is not a
// loaded local branch name. base itself is always treated as taken — this is
// only called after base failed to check out.
func suggestLocalName(branches []model.Branch, base string) string {
	taken := make(map[string]bool, len(branches)+1)
	taken[base] = true
	for _, b := range branches {
		taken[b.Name] = true
	}
	for i := 2; ; i++ {
		if cand := fmt.Sprintf("%s-%d", base, i); !taken[cand] {
			return cand
		}
	}
}

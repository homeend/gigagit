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
	popupMax
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
	box := modalStyle.Width(popupResolveWidth(w, p.maximized, popupInnerWidth(w))).Render(b.String()) + "\n"
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

// checkoutDivergedModal offers recovery after a CheckoutDivergedError: check
// the remote ref out under a fresh local name (popup pre-filled with the
// first free base-2/-3… suggestion, so enter never re-collides) or cancel.
// The intent (stay vs switch) of the failed dispatch carries over.
func (m Model) checkoutDivergedModal(pc pendingCheckout) *decisionState {
	return &decisionState{
		req: engine.DecisionRequest{
			ID:      "checkout-diverged",
			Prompt:  pc.base + " has diverged from " + pc.remoteRef + " and cannot fast-forward.",
			Options: []string{"check out as different name…", "cancel"},
		},
		onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
			if opt == "check out as different name…" {
				return m.openCheckoutAsPopup(pc.remoteRef, suggestLocalName(m.branches, pc.base), pc.intent), nil
			}
			return m, nil
		},
	}
}

// checkoutCurrentBranchModal replaces the doomed dispatch when c/s targets
// the remote counterpart of the checked-out branch — updating it is a pull,
// not a checkout. State-aware: "pull now" is offered only when the selected
// ref IS the branch's upstream and the branch is behind it (pulling would
// otherwise be a no-op, or fetch from a different remote than the one
// selected); "already contains" is only claimed when provable (upstream,
// not behind). The rename option reuses the checkout-as popup with a free
// suggestion, keeping the pressed key's stay/switch intent.
func (m Model) checkoutCurrentBranchModal(rb model.RemoteBranch, intent engine.CheckoutIntent) *decisionState {
	const rename = "check out as different name…"
	prompt := m.status.Branch + " is the current branch."
	opts := []string{rename, "cancel"}
	if rb.Name == m.status.Upstream {
		if m.status.Behind > 0 {
			prompt = fmt.Sprintf("%s is the current branch (behind %s by %d).", m.status.Branch, rb.Name, m.status.Behind)
			opts = []string{"pull now", rename, "cancel"}
		} else {
			prompt = m.status.Branch + " is the current branch and already contains " + rb.Name + " (nothing to pull)."
		}
	}
	return &decisionState{
		req: engine.DecisionRequest{
			ID:      "checkout-current-branch",
			Prompt:  prompt,
			Options: opts,
		},
		onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
			switch opt {
			case "pull now":
				// The same op the p key runs for the current branch; SmartPull's
				// own decision ladder handles merge/rebase/dirty trees.
				return m.startOp(engine.SmartPull{Intent: engine.PullAndStay})
			case rename:
				return m.openCheckoutAsPopup(rb.Name, suggestLocalName(m.branches, rb.Branch), intent), nil
			}
			return m, nil
		},
	}
}

package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/linknav"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// The # prompt accepts a pasted gg:// link as well as a commit-ish. A link is
// resolved off-thread through linknav — the SAME resolver and navigate
// builder `gg open` and `gg session navigate` use — and then takes one of
// three exits: it names a place in THIS checkout (the navigate is applied
// through applySteer, exactly like a --at landing), it names this checkout
// and nothing in it (a notice), or it names ANOTHER checkout (the prompt
// turns into a confirm; enter switches the repo and lands there through the
// --at gate once the new repo has loaded).

// isLinkText reports whether the prompt's text is a gg link rather than a
// commit-ish.
func isLinkText(s string) bool { return strings.HasPrefix(s, model.LinkScheme) }

// gotoLinkResolvedMsg carries the off-thread resolve of a pasted link. text is
// the exact submitted text (the tag-gate key).
type gotoLinkResolvedMsg struct {
	text     string
	svc      *domain.Service // the session's service at dispatch: a repo switch replaces it, which retires this resolve
	checkout string          // the link's checkout: its absolute top level
	same     bool            // it is THIS session's checkout
	bare     bool            // a repository link: nothing in it to land on
	cmd      steer.Command   // the navigate to apply (same && !bare; also built, unused, for a switch)
	at       model.Link      // the landing link for a repo switch (!same && !bare)
	err      error
}

// gotoLinkSwitch is the prompt's confirm state: the link names another
// checkout, and enter switches there.
type gotoLinkSwitch struct {
	checkout string
	bare     bool
	at       model.Link
}

// resolveLinkCmd resolves text against this machine (the repo registry, this
// session's checkout, live-session presence) and, for a link with a place in
// it, builds its navigate — hunk lowering runs on the LINK's checkout, which
// may not be this one.
func (m Model) resolveLinkCmd(text string) tea.Cmd {
	svc, statePath := m.svc, m.statePath
	return func() tea.Msg {
		ctx := context.Background()
		res, err := linknav.Resolve(ctx, statePath, svc, text)
		if err != nil {
			return gotoLinkResolvedMsg{text: text, svc: svc, err: err}
		}
		msg := gotoLinkResolvedMsg{text: text, svc: svc, checkout: res.Checkout}
		// A TopLevel failure (the checkout vanished mid-session) reads as
		// "another checkout": the confirm then names this very path, and the
		// switch re-opens it — the honest recovery, not a silent no-op.
		top, err := svc.TopLevel(ctx)
		msg.same = err == nil && domain.SamePath(top, res.Checkout)
		if linknav.RepoOnly(res) {
			msg.bare = true
			return msg
		}
		target := svc
		if !msg.same {
			// The same per-service setup the session would get after the
			// switch (reRoot opens the new repo with OpenTUI too), so the hunk
			// lowered here is the hunk the switched-to session would show.
			target = domain.OpenTUI(res.Checkout)
		}
		c, err := linknav.Command(ctx, target, res)
		if err != nil {
			msg.err = err
			return msg
		}
		msg.cmd, msg.at = c, linknav.AtLink(res, c)
		return msg
	}
}

// resolvedGotoLink applies a link resolve. Tag-gated by the caller: acts only
// when the prompt is on top and its input still equals msg.text.
func (m Model) resolvedGotoLink(p *gotoCommitPopup, msg gotoLinkResolvedMsg) (Model, tea.Cmd) {
	p.resolving = false
	if msg.err != nil {
		p.err = i18n.T("cannot open link: %s", msg.err.Error())
		return m, nil
	}
	if !msg.same {
		p.pending = &gotoLinkSwitch{checkout: msg.checkout, bare: msg.bare, at: msg.at}
		return m, nil
	}
	m = m.popGotoPrompt()
	if msg.bare {
		m.statusMsg = i18n.T("that link names this checkout")
		return m, nil
	}
	// applySteer, not steerNavigate: the landing must obey the SAME refusals a
	// steered navigate does. The command carries no id and no wait, so a
	// refusal reaches the status bar through answerSteer's id-less arm and the
	// landing notice says "opened", not "agent" (startAtOrigin) — the user
	// pasted this link themselves.
	return m.applySteer(msg.cmd)
}

// popGotoPrompt closes the prompt and, when it was launched from the command
// palette, the palette beneath it — so whatever opens next opens over the
// base, not over a stale palette. (Direct # opens leave no palette
// underneath.)
func (m Model) popGotoPrompt() Model {
	m = m.popLayer()
	if _, ok := m.topLayer().(*commandPalette); ok {
		m = m.popLayer()
	}
	return m
}

// switchToLink is the confirm's enter: switch this session to the link's
// checkout (the repo switcher's own path) and, unless the link was bare, arm
// the --at gate with the resolved landing so the navigate fires once the new
// repo has loaded — never earlier, and for a preview link not before its
// previews have been read. The switch target is checked FIRST, exactly as
// guardedReRoot does for every other switch site: the resolver matches a
// local link's checkout by path prefix against the registry, so a checkout
// recorded under the other environment's notation resolves fine and would
// otherwise tear the session down (and poison --cwd-file). Such a link is
// refused in place; no repair is offered from here.
func (m Model) switchToLink(p *gotoCommitPopup, sw gotoLinkSwitch) (Model, tea.Cmd) {
	if verdict, _ := checkSwitchTarget(guardStat, guardGOOS, sw.checkout); verdict != switchOK {
		p.pending = nil
		p.err = i18n.T("cannot switch: %s is not reachable from here", sw.checkout)
		return m, nil
	}
	m = m.popGotoPrompt()
	nm, cmd := m.reRoot(sw.checkout)
	m = nm.(Model)
	if !sw.bare {
		m.startAt, m.startAtPending, m.startAtPreviewsSeen = sw.at, true, false
	}
	return m, cmd
}

package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// gotoCommitPopup takes a commit SHA (or any commit-ish ref) and opens that
// commit's files in the files-view — or a pasted gg:// link, and lands on it
// (goto_link.go). Reachable directly via `#` and from the command palette's
// "Show commit" / "Open gg:// link…". The text is resolved before anything
// opens: a ref or link that does not resolve shows an inline error and keeps
// the popup open (no half-opened files-view on a typo).
type gotoCommitPopup struct {
	popupMax
	input     textfield       // the SHA / ref / gg:// link to resolve (no spaces)
	err       string          // inline error from the last failed resolve; "" = none
	resolving bool            // a resolve cmd is in flight
	pending   *gotoLinkSwitch // a link into another checkout, awaiting enter
	hist      linkHistPicker  // the copied-link history under the input (↓ enters it)
}

func (p *gotoCommitPopup) histPicker() *linkHistPicker { return &p.hist }

// openGotoCommitPopup pushes a fresh show-commit input. The shared seam called
// by both the `#` key and the palette command, so the two paths never diverge.
func (m Model) openGotoCommitPopup() (Model, tea.Cmd) {
	m.linkHistGen++
	return m.pushLayer(&gotoCommitPopup{input: newTextField("")}), m.linkHistCmd(m.linkHistGen)
}

// submit resolves text — the ONE path both enter in the field and a pick from
// the history take, so a picked link cannot land differently from a typed one.
func (p *gotoCommitPopup) submit(m Model, text string) (Model, tea.Cmd) {
	text = strings.TrimSpace(text)
	if text == "" { // nothing to resolve; keep the popup open
		return m, nil
	}
	p.resolving = true
	p.err = ""
	if isLinkText(text) {
		return m, m.resolveLinkCmd(text)
	}
	return m, m.resolveCommitCmd(text)
}

// gotoCommitResolvedMsg carries the result of resolving the typed ref. rev is
// the exact text that was submitted (the tag-gate key); hash is the resolved
// full object id on success; err is non-nil when the ref does not resolve.
type gotoCommitResolvedMsg struct {
	rev  string
	hash string
	err  error
}

func (m Model) resolveCommitCmd(rev string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		// Peel to the commit: an annotated tag's name resolves to the tag OBJECT,
		// not the commit it points at, so the files view would get a non-commit.
		// `^{commit}` is a no-op for SHAs/branches/lightweight tags/HEAD~3 and
		// dereferences an annotated tag (same trick the annotate-tag CLI uses).
		hash, err := svc.RevParse(context.Background(), rev+"^{commit}")
		return gotoCommitResolvedMsg{rev: rev, hash: hash, err: err}
	}
}

func (p *gotoCommitPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	// The history list, while it has the keyboard. A pick fills the field AND
	// submits: this is a navigation prompt, a second enter would buy nothing.
	if picked, handled := p.hist.key(msg); handled {
		if picked == "" {
			return m, nil
		}
		p.input = newTextField(picked)
		p.pending = nil
		return p.submit(m, picked)
	}
	switch msg.Type {
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyDown:
		p.hist.enter()
		return m, nil
	case tea.KeyEnter:
		if p.pending != nil {
			return m.switchToLink(p, *p.pending)
		}
		return p.submit(m, p.input.Value())
	case tea.KeySpace:
		// neither a commit-ish nor a link has spaces; swallow the key
		return m, nil
	default:
		if p.input.HandleEditKey(msg) {
			p.err = ""      // editing clears the stale error…
			p.pending = nil // …and withdraws a switch the user did not confirm
		}
	}
	return m, nil
}

// resolvedGotoCommit applies a resolve result. Tag-gated by the caller: acts only
// when this popup is on top and its input still equals msg.rev.
func (m Model) resolvedGotoCommit(p *gotoCommitPopup, msg gotoCommitResolvedMsg) (Model, tea.Cmd) {
	p.resolving = false
	if msg.err != nil {
		p.err = i18n.T("no such commit: %s", msg.rev)
		return m, nil
	}
	m = m.popGotoPrompt()
	m, cmd := m.openChangedFiles(model.Commit{Hash: msg.hash})
	// Open on the TREE: the resolved commit may not be in the loaded feed, so the
	// right column (the Commits feed) is unrelated — walk this commit's files.
	// focus is the Commits panel (the files-view commit-list side), mirroring
	// openReflogFiles' by-hash open.
	m.focus = panelCommits
	m = m.focusTree()
	return m, cmd
}

func (p *gotoCommitPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

func (p *gotoCommitPopup) box(m Model) string {
	w, _ := m.overlayDims()
	var b strings.Builder
	b.WriteString(i18n.T("Go to commit or gg:// link") + "\n\n")
	b.WriteString(viewField(i18n.T("commit or link: "), p.input, true, popupContentWidth(w)) + "\n")
	if p.err != "" {
		b.WriteString("\n" + st().errorText.Render(p.err) + "\n")
	}
	// The copied-link history sits between the field and the key hints, so
	// the hints stay the last line whatever the list's length.
	if h := p.hist.view(popupContentWidth(w)); h != "" && p.pending == nil && !p.resolving {
		b.WriteString("\n" + h + "\n")
	}
	if p.pending != nil {
		b.WriteString("\n" + i18n.T("that link names another checkout: %s", p.pending.checkout) + "\n")
		b.WriteString("\n" + i18n.T("[enter] switch there  [esc] stay"))
	} else if p.resolving {
		// A link into another repository can cost seconds (its service opens
		// and lowers a hunk there); say so rather than look frozen.
		b.WriteString("\n" + i18n.T("resolving…") + "\n")
		b.WriteString("\n" + i18n.T("[enter] go  [esc] cancel"))
	} else {
		if p.hist.active {
			b.WriteString("\n" + i18n.T("[enter] go to this link  [↑] back to the field  [esc] back"))
		} else if len(p.hist.rows) > 0 {
			b.WriteString("\n" + i18n.T("[enter] go  [↓] copied links  [esc] cancel"))
		} else {
			b.WriteString("\n" + i18n.T("[enter] go  [esc] cancel"))
		}
	}
	return st().modalStyle.Width(popupResolveWidth(w, p.maximized, popupInnerWidth(w))).Render(b.String()) + "\n"
}

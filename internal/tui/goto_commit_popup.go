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
}

// openGotoCommitPopup pushes a fresh show-commit input. The shared seam called
// by both the `#` key and the palette command, so the two paths never diverge.
func (m Model) openGotoCommitPopup() (Model, tea.Cmd) {
	return m.pushLayer(&gotoCommitPopup{input: newTextField("")}), nil
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
	switch msg.Type {
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyEnter:
		if p.pending != nil {
			return m.switchToLink(p, *p.pending)
		}
		text := strings.TrimSpace(p.input.Value())
		if text == "" { // nothing to resolve; keep the popup open
			return m, nil
		}
		p.resolving = true
		p.err = ""
		if isLinkText(text) {
			return m, m.resolveLinkCmd(text)
		}
		return m, m.resolveCommitCmd(text)
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
	if p.pending != nil {
		b.WriteString("\n" + i18n.T("that link names another checkout: %s", p.pending.checkout) + "\n")
		b.WriteString("\n" + i18n.T("[enter] switch there  [esc] stay"))
	} else if p.resolving {
		// A link into another repository can cost seconds (its service opens
		// and lowers a hunk there); say so rather than look frozen.
		b.WriteString("\n" + i18n.T("resolving…") + "\n")
		b.WriteString("\n" + i18n.T("[enter] go  [esc] cancel"))
	} else {
		b.WriteString("\n" + i18n.T("[enter] go  [esc] cancel"))
	}
	return st().modalStyle.Width(popupResolveWidth(w, p.maximized, popupInnerWidth(w))).Render(b.String()) + "\n"
}

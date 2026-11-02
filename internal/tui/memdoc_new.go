package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/clock"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// memdocPopup is ctrl+\'s n: a title and a text become a memory, plain document
// of the worktree on screen (spec 2026-11-02-memory-docs-design). It is the
// commit box's two fields.
type memdocPopup struct {
	popupMax
	edit       commitPopup
	err        string // why ctrl+s refused, shown in place
	discardAsk bool   // esc with text typed: the next esc leaves
}

func (p *memdocPopup) typed() bool {
	return strings.TrimSpace(p.edit.title.Value()) != "" || strings.TrimSpace(p.edit.desc.Value()) != ""
}

func (p *memdocPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	submit, cancel := p.edit.applyEditKey(msg)
	switch {
	case cancel:
		if p.typed() && !p.discardAsk {
			p.discardAsk = true
			return m, nil
		}
		return m.popLayer(), nil
	case submit:
		return p.save(m)
	}
	p.discardAsk, p.err = false, ""
	return m, nil
}

// save files the document and opens it; a refusal keeps the window and its text.
func (p *memdocPopup) save(m Model) (Model, tea.Cmd) {
	title, text := strings.TrimSpace(p.edit.title.Value()), p.edit.desc.Value()
	p.discardAsk = false
	switch {
	case m.docs == nil:
		p.err = i18n.T("no document store in this gg")
		return m, nil
	case title == "":
		p.err = i18n.T("give it a title")
		return m, nil
	case strings.TrimSpace(text) == "":
		p.err = i18n.T("give it a text")
		return m, nil
	}
	root := domain.CheckoutKey(m.currentWorktree)
	o, err := m.docs.AddMemoryOverview(root, m.currentWorktree, agentdocs.MintID(clock.Now(), nil), title, text, "", true)
	if err != nil {
		p.err = err.Error()
		return m, nil
	}
	m = m.popLayer().syncOverviews()
	for _, d := range m.openFiles.list(m.currentWorktree) {
		if d.ov != nil && d.id() == o.ID {
			return m.bringToFront(d)
		}
	}
	return m, nil
}

func (p *memdocPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

func (p *memdocPopup) box(m Model) string {
	w, termH := m.overlayDims()
	innerW := popupResolveWidth(w, p.maximized, commitNormalWidth(w))
	contentW := innerW - st().modalStyle.GetHorizontalPadding()
	if contentW < 1 {
		contentW = 1
	}
	save := i18n.T("[ctrl+s] save")
	esc := i18n.T("[esc] cancel")
	if p.discardAsk {
		esc = i18n.T("[esc] again: discard what you typed")
	}
	footer := packHints([]string{i18n.T("[tab] switch field"), i18n.T("[enter] newline/next"), i18n.T("[ctrl+t] fullscreen"), save, esc}, contentW)
	if p.err != "" {
		footer = p.err + "\n" + footer
	}
	var b strings.Builder
	b.WriteString(i18n.T("New text (in memory, gone when gg exits)") + "\n\n")
	b.WriteString(renderCommitFields(&p.edit, contentW, commitDescBudget(termH, &p.edit, contentW, footer)))
	b.WriteString("\n")
	b.WriteString(footer)
	return st().modalStyle.Width(innerW).Render(b.String()) + "\n"
}

// plainOverviewLines lays a plain document out: its lines as written, control
// characters made safe, wrapped at width. No markdown, no anchors.
func plainOverviewLines(text string, width int, mode dispMode) []contentLine {
	var out []contentLine
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		s := sanitizeLine(line)
		parts := []string{s}
		if width > 0 && mode != modeScroll && s != "" {
			parts = wrapWords(s, width)
		}
		for i, part := range parts {
			out = append(out, contentLine{text: part, raw: part, src: true, noWrap: true, cont: i > 0})
		}
	}
	if len(out) == 0 {
		return []contentLine{{text: i18n.T("(empty overview)")}}
	}
	return out
}

// memdocWorktree is the worktree overview d's link names: a memory document's
// own, whether or not its root is attached.
func (m Model) memdocWorktree(d *openFile) string {
	if d.docs != nil {
		if wt := d.docs.AttachedWorktree(d.root); wt != "" {
			return wt
		}
	}
	if m.docs != nil && d.ov != nil && d.ov.memory {
		return m.docs.OverviewDir(d.root, d.id())
	}
	return ""
}

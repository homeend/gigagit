package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/fuzzy"
	"github.com/homeend/gigagit/internal/i18n"
)

// previewAddPopup collects a (source, target) pair. Each field completes
// against the local + remote-tracking branch names (fuzzy); tab/enter accept
// the top suggestion when the typed text is not itself a branch. ctrl+s swaps
// the two fields. Enter on the target submits.
type previewAddPopup struct {
	popupMax
	source, target textfield
	onTarget       bool
}

// previewSuggestLimit caps the completion strip; five names fit one line in a
// popup narrow enough for the smallest terminal gg supports.
const previewSuggestLimit = 5

// branchNameCandidates is every name a preview side may be: local branches
// then remote-tracking ones (origin/main, …). A remote target is typed like
// any other name — that is the whole "preview against origin/main" story.
func (m Model) branchNameCandidates() []string {
	out := make([]string, 0, len(m.branches)+len(m.remoteBranches))
	for _, b := range m.branches {
		out = append(out, b.Name)
	}
	for _, r := range m.remoteBranches {
		out = append(out, r.Name)
	}
	return out
}

// field is the focused text field (source until tab moves to target).
func (p *previewAddPopup) field() *textfield {
	if p.onTarget {
		return &p.target
	}
	return &p.source
}

// suggestions ranks the candidates against the focused field's text.
func (p *previewAddPopup) suggestions(m Model) []string {
	q := strings.TrimSpace(p.field().Value())
	if q == "" {
		return nil // an empty field would list every branch; say nothing instead
	}
	ms := fuzzy.Rank(q, m.branchNameCandidates(), previewSuggestLimit)
	out := make([]string, 0, len(ms))
	for _, x := range ms {
		out = append(out, x.S)
	}
	return out
}

// accept replaces the focused field with the top suggestion unless the typed
// text already names a branch exactly. Text that matches nothing is left
// alone: the add itself refuses it, naming what the user typed.
func (p *previewAddPopup) accept(m Model) {
	v := strings.TrimSpace(p.field().Value())
	for _, c := range m.branchNameCandidates() {
		if c == v {
			return
		}
	}
	if s := p.suggestions(m); len(s) > 0 {
		*p.field() = newTextField(s[0])
	}
}

// update handles one key while the form is open. It swallows every key;
// ctrl+c still quits so the user is never trapped.
func (p *previewAddPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyCtrlS:
		p.source, p.target = p.target, p.source
	case tea.KeyTab:
		p.accept(m)
		p.onTarget = !p.onTarget
	case tea.KeyUp, tea.KeyDown, tea.KeyShiftTab:
		p.onTarget = !p.onTarget
	case tea.KeyEnter:
		p.accept(m)
		if !p.onTarget {
			p.onTarget = true
			return m, nil
		}
		src, tgt := strings.TrimSpace(p.source.Value()), strings.TrimSpace(p.target.Value())
		if src == "" || tgt == "" {
			return m, nil
		}
		m = m.popLayer()
		return m, m.previewAddCmd(src, tgt, "", true, true)
	case tea.KeySpace:
		// branch names cannot contain spaces — drop it
	default:
		p.field().HandleEditKey(msg)
	}
	return m, nil
}

// render composites the add form over the layer beneath.
func (p *previewAddPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

// box draws the add form (modal box only).
func (p *previewAddPopup) box(m Model) string {
	w, _ := m.overlayDims()
	cw := popupContentWidth(w)
	srcMark, tgtMark := "> ", "  "
	if p.onTarget {
		srcMark, tgtMark = "  ", "> "
	}
	var b strings.Builder
	b.WriteString(i18n.T("New merge preview") + "\n\n")
	b.WriteString(viewField(srcMark+i18n.T("source: "), p.source, !p.onTarget, cw) + "\n")
	b.WriteString(viewField(tgtMark+i18n.T("target: "), p.target, p.onTarget, cw) + "\n")
	if s := p.suggestions(m); len(s) > 0 {
		b.WriteString("\n" + i18n.T("matches: ") + strings.Join(s, "  ") + "\n")
	}
	b.WriteString("\n" + i18n.T("[tab] next field  [ctrl+s] swap  [enter] save & open  [esc] cancel"))
	return modalStyle.Width(popupResolveWidth(w, p.maximized, popupInnerWidth(w))).Render(b.String()) + "\n"
}

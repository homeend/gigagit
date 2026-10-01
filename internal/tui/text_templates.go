package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// textTemplateListRows is how many titles the window shows before the list
// scrolls.
const textTemplateListRows = 8

// textTemplatesView is the text templates window (alt+x): a scrolling title
// list over the selected template's text and its variables. enter asks for
// the <user:…> values and shows the rendered text, which y copies. n/e/d add,
// edit and delete; the text itself is written in $EDITOR.
type textTemplatesView struct {
	popupMax
	loading    bool
	items      []model.TextTemplate
	sel        int
	bodyScroll int // first shown display line of the body pane
	mode       ttMode

	// fill / rendered
	fill      templateFill
	rendered  string
	renderErr string
	seqNames  []string // counters the rendered text consumes when taken
	rScroll   int
}

type ttMode int

const (
	ttBrowse ttMode = iota
	ttFill
	ttRendered
	ttForm
	ttConfirmDelete
)

// textTemplatesDataMsg is a (re)loaded template list. selectID names the row
// to land on after a save; status is the status line to show.
type textTemplatesDataMsg struct {
	items    []model.TextTemplate
	err      error
	selectID string
	status   string
}

func (m Model) openTextTemplates() (Model, tea.Cmd) {
	m = m.pushLayer(&textTemplatesView{loading: true})
	return m, m.loadTextTemplatesCmd("", "")
}

func (m Model) loadTextTemplatesCmd(selectID, status string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		ts, err := svc.TextTemplates(context.Background())
		return textTemplatesDataMsg{items: ts, err: err, selectID: selectID, status: status}
	}
}

// onData lands a loaded list in the view.
func (v *textTemplatesView) onData(msg textTemplatesDataMsg) {
	v.loading = false
	if msg.err != nil {
		return
	}
	v.items = msg.items
	for i, t := range v.items {
		if msg.selectID != "" && t.ID == msg.selectID {
			v.sel = i
		}
	}
	v.sel = max(0, min(v.sel, len(v.items)-1))
	v.bodyScroll = 0
}

func (v *textTemplatesView) selected() (model.TextTemplate, bool) {
	if v.sel < 0 || v.sel >= len(v.items) {
		return model.TextTemplate{}, false
	}
	return v.items[v.sel], true
}

func (v *textTemplatesView) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch v.mode {
	case ttFill:
		return v.updateFill(m, msg)
	case ttRendered:
		return v.updateRendered(m, msg)
	}
	return v.updateBrowse(m, msg)
}

func (v *textTemplatesView) updateBrowse(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyUp:
		v.moveSel(-1)
		return m, nil
	case tea.KeyDown:
		v.moveSel(1)
		return m, nil
	case tea.KeyEnter:
		t, ok := v.selected()
		if !ok {
			return m, nil
		}
		if labels, _ := domain.TextTemplateTokens(t.Body); len(labels) > 0 {
			v.fill, v.mode = newTemplateFillLabels(labels), ttFill
			return m, nil
		}
		return m, m.renderTextTemplateCmd(t.Body, map[string]string{})
	case tea.KeyPgDown:
		v.bodyScroll += v.geometry(m).bodyH // box() clamps
		return m, nil
	case tea.KeyPgUp:
		v.bodyScroll = max(0, v.bodyScroll-v.geometry(m).bodyH)
		return m, nil
	}
	switch msg.String() {
	case "j":
		v.moveSel(1)
	case "k":
		v.moveSel(-1)
	}
	return m, nil
}

// moveSel moves the list cursor; a new selection shows its text from the top.
func (v *textTemplatesView) moveSel(d int) {
	next := max(0, min(v.sel+d, len(v.items)-1))
	if next != v.sel {
		v.sel, v.bodyScroll = next, 0
	}
}

// ttGeometry is the window's layout for one terminal size.
type ttGeometry struct {
	inner, textW int
	listH        int // title rows shown
	bodyH        int // text rows shown
	varLines     []string
	hintLines    []string
}

// browseHints are the browse mode's key hints.
func (v *textTemplatesView) browseHints() string {
	if len(v.items) == 0 {
		return i18n.T("[n] add  [esc] close")
	}
	return i18n.T("[enter] fill  [n] add  [e] edit  [d] delete  [ctrl+t] maximize  [esc] close")
}

// geometry sizes the browse layout: the list takes up to 8 rows, the text
// pane what is left (12 rows unless maximized); on a short terminal the list
// gives rows up so the text keeps at least 3.
func (v *textTemplatesView) geometry(m Model) ttGeometry {
	w, termH := m.overlayDims()
	g := ttGeometry{inner: popupResolveWidth(w, v.maximized, popupWideInnerWidth(w))}
	g.textW = popupTextWidth(g.inner)
	if t, ok := v.selected(); ok {
		labels, auto := domain.TextTemplateTokens(t.Body)
		if len(labels) > 0 {
			g.varLines = append(g.varLines, wrapWidth(i18n.T("Variables: %s", strings.Join(labels, " · ")), g.textW, 2)...)
		}
		if len(auto) > 0 {
			g.varLines = append(g.varLines, wrapWidth(i18n.T("Automatic: %s", strings.Join(auto, " · ")), g.textW, 2)...)
		}
	}
	g.hintLines = wrapParts(strings.Split(v.browseHints(), "  "), g.textW, "  ")
	g.listH = min(len(v.items), textTemplateListRows)
	// title + blank, the two rules, the variables, blank + hints, the frame.
	chrome := 2 + 2 + len(g.varLines) + 1 + len(g.hintLines) + st().modalStyle.GetVerticalFrameSize() + 2
	g.bodyH = termH - chrome - g.listH
	if g.bodyH < 3 {
		g.listH = max(1, g.listH-(3-g.bodyH))
		g.bodyH = 3
	}
	if !v.maximized {
		g.bodyH = min(g.bodyH, 12)
	}
	return g
}

// ttWrapText lays text out for a pane textW wide: each line soft-wrapped at
// the last column, tabs expanded.
func ttWrapText(text string, textW int) []string {
	var out []string
	for _, line := range strings.Split(strings.ReplaceAll(text, "\t", "    "), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			out = append(out, "")
			continue
		}
		out = append(out, wrapWidth(line, textW, len(line)+1)...)
	}
	return out
}

// ttTextPane returns h rows of text starting at *scroll (clamped in place),
// padded so the pane keeps its height, plus the rule that closes it — which
// carries the shown range when the text does not fit.
func ttTextPane(text string, textW, h int, scroll *int) (rows []string, rule string) {
	lines := ttWrapText(text, textW)
	*scroll = max(0, min(*scroll, len(lines)-h))
	for i := 0; i < h; i++ {
		row := ""
		if at := *scroll + i; at < len(lines) {
			row = lines[at]
		}
		rows = append(rows, row)
	}
	rule = strings.Repeat("─", textW)
	if len(lines) > h {
		pos := fmt.Sprintf(" %d-%d/%d ", *scroll+1, *scroll+h, len(lines))
		if pw := lipgloss.Width(pos); pw+4 < textW {
			rule = strings.Repeat("─", textW-pw-2) + pos + "──"
		}
	}
	return rows, st().dim.Render(rule)
}

func (v *textTemplatesView) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), v.box(m), w, h)
}

func (v *textTemplatesView) box(m Model) string {
	switch v.mode {
	case ttFill:
		return v.fillBox(m)
	case ttRendered:
		return v.renderedBox(m)
	}
	return v.browseBox(m)
}

// textTemplateRenderedMsg is a template resolved for the rendered step.
type textTemplateRenderedMsg struct {
	text     string
	seqNames []string
	err      error
}

func (m Model) renderTextTemplateCmd(body string, inputs map[string]string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		text, seqs, err := svc.RenderTextTemplate(context.Background(), body, inputs)
		return textTemplateRenderedMsg{text: text, seqNames: seqs, err: err}
	}
}

// onRendered shows the resolved text (or why it could not be resolved).
func (v *textTemplatesView) onRendered(msg textTemplateRenderedMsg) {
	v.mode, v.rScroll = ttRendered, 0
	v.rendered, v.seqNames, v.renderErr = msg.text, msg.seqNames, ""
	if msg.err != nil {
		// The user filled a text template, not called the template package.
		v.rendered, v.seqNames = "", nil
		v.renderErr = strings.Replace(msg.err.Error(), "template: ", "", 1)
	}
}

func (v *textTemplatesView) updateFill(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	done, cancel := v.fill.handleKey(msg)
	switch {
	case cancel:
		v.mode = ttBrowse
	case done:
		if t, ok := v.selected(); ok {
			return m, m.renderTextTemplateCmd(t.Body, v.fill.inputs())
		}
		v.mode = ttBrowse
	}
	return m, nil
}

func (v *textTemplatesView) updateRendered(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	page := v.renderedRows(m)
	switch msg.Type {
	case tea.KeyEsc:
		v.mode = ttBrowse
		return m, nil
	case tea.KeyUp:
		v.rScroll = max(0, v.rScroll-1)
		return m, nil
	case tea.KeyDown:
		v.rScroll++ // box() clamps
		return m, nil
	case tea.KeyPgUp:
		v.rScroll = max(0, v.rScroll-page)
		return m, nil
	case tea.KeyPgDown:
		v.rScroll += page
		return m, nil
	}
	switch msg.String() {
	case "k":
		v.rScroll = max(0, v.rScroll-1)
	case "j":
		v.rScroll++
	case "y":
		if v.renderErr != "" {
			return m, nil
		}
		// The text is taken: copy it, consume its counters, close the window.
		svc, names := m.svc, v.seqNames
		bump := func() tea.Msg {
			if svc != nil && len(names) > 0 {
				_ = svc.BumpPrefixSeqs(context.Background(), names)
			}
			return nil
		}
		return m.popLayer(), tea.Batch(m.copyToClipboardCmd(i18n.T("copied the rendered text"), v.rendered), bump)
	}
	return m, nil
}

// selTitle is the selected template's title ("" with nothing selected).
func (v *textTemplatesView) selTitle() string {
	t, _ := v.selected()
	return t.Title
}

func (v *textTemplatesView) fillBox(m Model) string {
	g := v.geometry(m)
	parts := []string{i18n.T("%s — fill variables (%d/%d)", v.selTitle(), v.fill.idx+1, len(v.fill.labels)), ""}
	parts = append(parts, v.fill.view(g.textW)...)
	parts = append(parts, "", i18n.T("[enter/tab] next  [esc] back"))
	return popupBox(g.inner, strings.Join(parts, "\n"))
}

// renderedRows is the rendered step's text height: the whole text when it
// fits, else what the terminal leaves (16 rows unless maximized).
func (v *textTemplatesView) renderedRows(m Model) int {
	g := v.geometry(m)
	_, termH := m.overlayDims()
	// title + blank, the rule, blank + one hint line, the frame.
	room := max(3, termH-2-1-2-st().modalStyle.GetVerticalFrameSize()-2)
	if !v.maximized {
		room = min(room, 16)
	}
	return max(1, min(room, len(ttWrapText(v.rendered, g.textW))))
}

func (v *textTemplatesView) renderedBox(m Model) string {
	g := v.geometry(m)
	parts := []string{i18n.T("%s — rendered", v.selTitle()), ""}
	if v.renderErr != "" {
		parts = append(parts, wrapWidth(v.renderErr, g.textW, 6)...)
		for i := 2; i < len(parts); i++ {
			parts[i] = st().errorText.Render(parts[i])
		}
		parts = append(parts, "", i18n.T("[esc] back to templates"))
		return popupBox(g.inner, strings.Join(parts, "\n"))
	}
	rows, rule := ttTextPane(v.rendered, g.textW, v.renderedRows(m), &v.rScroll)
	parts = append(parts, rows...)
	parts = append(parts, rule, "", i18n.T("[y] copy and close  [↑/↓] scroll  [esc] back to templates"))
	return popupBox(g.inner, strings.Join(parts, "\n"))
}

func (v *textTemplatesView) browseBox(m Model) string {
	g := v.geometry(m)
	parts := []string{i18n.T("Text templates"), ""}
	if v.loading {
		parts = append(parts, i18n.T("  (loading…)"))
		return popupBox(g.inner, strings.Join(parts, "\n"))
	}
	if len(v.items) == 0 {
		parts = append(parts, i18n.T("  (none yet — [n] to add)"), "")
		parts = append(parts, g.hintLines...)
		return popupBox(g.inner, strings.Join(parts, "\n"))
	}
	s := st()
	rows := make([]winRow, len(v.items))
	for i, t := range v.items {
		cursor := "  "
		var style lipgloss.Style
		if i == v.sel {
			cursor, style = "> ", s.selectedRow
		}
		tag := i18n.T("[global]")
		if t.Scope == model.ProfileScopeRepo {
			tag = i18n.T("[this repo]")
		}
		// The tag is a right-hand column; the title takes the rest.
		titleW := max(1, g.textW-lipgloss.Width(cursor)-lipgloss.Width(tag)-2)
		rows[i] = winRow{text: cursor + padRight(truncate(t.Title, titleW), titleW) + "  " + tag, style: style}
	}
	parts = append(parts, renderWindow(rows, winOpts{w: g.textW, h: g.listH, anchor: v.sel})...)
	parts = append(parts, s.dim.Render(strings.Repeat("─", g.textW)))
	body, rule := ttTextPane(v.items[v.sel].Body, g.textW, g.bodyH, &v.bodyScroll)
	parts = append(parts, body...)
	parts = append(parts, rule)
	parts = append(parts, g.varLines...)
	parts = append(parts, "")
	parts = append(parts, g.hintLines...)
	return popupBox(g.inner, strings.Join(parts, "\n"))
}

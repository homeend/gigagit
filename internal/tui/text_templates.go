package tui

import (
	"context"
	"fmt"
	"os"
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
	fill templateFill
	// rendering: a render is in flight. Keys are swallowed (esc gives up on
	// it) and only the result of request renderGen may land.
	rendering bool
	renderGen int
	copying   bool // y's copy is running; the window closes when it worked
	rendered  string
	renderErr string
	seqNames  []string // counters the rendered text consumes when taken
	rScroll   int

	// form (add / edit)
	fTitle  textfield
	scope   model.ProfileScope
	field   int    // 0 = title, 1 = scope
	formErr string // inline validation error; "" = none
	editID  string // the template being edited; "" = adding
	// handingOff: enter was pressed and the text is on its way to the editor
	// (the temp file is being written). Keys are swallowed until the editor
	// returns or the handoff fails, so a second enter queues no second editor.
	handingOff bool
	// draft is text the editor returned that could not be saved (a malformed
	// token): the next editor run starts from it, not from the stored text.
	draft string

	// tipFull is the selected title when the list had to cut it: render shows
	// it whole on the bottom bar. Set by browseBox. The bottom bar is also the
	// status row, so tipHold keeps the title off it from an outcome this
	// window reported until the next key.
	tipFull string
	tipHold bool
}

type ttMode int

const (
	ttBrowse ttMode = iota
	ttFill
	ttRendered
	ttForm
	ttConfirmDelete
)

// textTemplatesDataMsg is a (re)loaded template list. selectID and
// selectScope name the row to land on after a save; status is the status line to show.
type textTemplatesDataMsg struct {
	items       []model.TextTemplate
	err         error
	selectID    string
	selectScope model.ProfileScope // the scope selectID lives in
	status      string
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
	v.tipHold = msg.err != nil || msg.status != ""
	if msg.err != nil && len(msg.items) == 0 {
		return // nothing could be read: keep what is shown
	}
	// With an error AND rows, one scope's file is damaged: show the other.
	v.items = msg.items
	for i, t := range v.items {
		if msg.selectID != "" && t.ID == msg.selectID && t.Scope == msg.selectScope {
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
	if v.handingOff {
		return m, nil
	}
	if v.rendering {
		if msg.Type == tea.KeyEsc {
			v.rendering = false
		}
		return m, nil
	}
	if v.loading && v.mode == ttBrowse && msg.Type != tea.KeyEsc {
		return m, nil // the list is being replaced: nothing to act on yet
	}
	switch v.mode {
	case ttFill:
		return v.updateFill(m, msg)
	case ttRendered:
		return v.updateRendered(m, msg)
	case ttForm:
		return v.updateForm(m, msg)
	case ttConfirmDelete:
		return v.updateConfirm(m, msg)
	}
	return v.updateBrowse(m, msg)
}

func (v *textTemplatesView) updateBrowse(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	v.tipHold = false
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
		return m, v.startRender(m, t.Body, map[string]string{})
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
	case "n", "a":
		v.openForm(model.TextTemplate{Scope: model.ProfileScopeGlobal})
	case "e":
		if t, ok := v.selected(); ok {
			v.openForm(t)
		}
	case "d":
		if _, ok := v.selected(); ok {
			v.mode = ttConfirmDelete
		}
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
	out := overlayCenter(clipToHeight(below, h), v.box(m), w, h)
	if v.tipFull != "" && !v.tipHold {
		// The selected title was cut: it takes the bottom bar, which the
		// window never covers, in the terminal's own colours.
		out = overlayAt(out, padRight(truncate(" "+v.tipFull, w), w), 0, h-1, w, h)
	}
	return out
}

func (v *textTemplatesView) box(m Model) string {
	v.tipFull = ""
	switch v.mode {
	case ttFill:
		return v.fillBox(m)
	case ttRendered:
		return v.renderedBox(m)
	case ttForm:
		return v.formBox(m)
	case ttConfirmDelete:
		g := v.geometry(m)
		return popupBox(g.inner, strings.Join([]string{
			i18n.T("Delete text template %s?", v.selTitle()), "",
			i18n.T("[y] delete  [n/esc] keep"),
		}, "\n"))
	}
	return v.browseBox(m)
}

// textTemplateRenderedMsg is a template resolved for the rendered step.
type textTemplateRenderedMsg struct {
	gen      int // the request it answers (textTemplatesView.renderGen)
	text     string
	seqNames []string
	err      error
}

// startRender asks for body resolved with inputs and waits for the answer.
func (v *textTemplatesView) startRender(m Model, body string, inputs map[string]string) tea.Cmd {
	v.renderGen++
	v.rendering = true
	svc, gen := m.svc, v.renderGen
	return func() tea.Msg {
		text, seqs, err := svc.RenderTextTemplate(context.Background(), body, inputs)
		return textTemplateRenderedMsg{gen: gen, text: text, seqNames: seqs, err: err}
	}
}

// onRendered shows the resolved text (or why it could not be resolved).
func (v *textTemplatesView) onRendered(msg textTemplateRenderedMsg) {
	if !v.rendering || msg.gen != v.renderGen {
		return // the user gave up on that render, or asked for another
	}
	v.rendering = false
	v.mode, v.rScroll = ttRendered, 0
	v.rendered, v.seqNames, v.renderErr = msg.text, msg.seqNames, ""
	if msg.err != nil {
		// The user filled a text template, not called the template package.
		v.rendered, v.seqNames = "", nil
		v.renderErr = strings.Replace(msg.err.Error(), "template: ", "", 1)
	}
}

func (v *textTemplatesView) updateFill(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if v.fill.scrollKey(msg, v.fillRoom(m)-1) {
		return m, nil
	}
	done, cancel := v.fill.handleKey(msg)
	switch {
	case cancel:
		v.mode = ttBrowse
	case done:
		if t, ok := v.selected(); ok {
			return m, v.startRender(m, t.Body, v.fill.inputs())
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
		if v.renderErr != "" || v.copying {
			return m, nil
		}
		// The text is taken once it is on the clipboard: only then are its
		// counters consumed and the window closed. A failed copy (no
		// clipboard) keeps the text on screen.
		v.copying = true
		svc, names := m.svc, v.seqNames
		copyCmd := m.copyToClipboardCmd(i18n.T("copied the rendered text"), v.rendered)
		return m, func() tea.Msg {
			res, _ := copyCmd().(clipboardCopiedMsg)
			out := textTemplateCopiedMsg{clipboardCopiedMsg: res}
			if res.err == nil && svc != nil && len(names) > 0 {
				out.seqErr = svc.TakeTextTemplateSeqs(context.Background(), names)
			}
			return out
		}
	}
	return m, nil
}

// textTemplateCopiedMsg is the outcome of y's copy of the rendered text.
// seqErr: the text was copied but its <seq:…> counters could not be consumed
// (the next render hands out the same numbers).
type textTemplateCopiedMsg struct {
	clipboardCopiedMsg
	seqErr error
}

// selTitle is the selected template's title ("" with nothing selected).
func (v *textTemplatesView) selTitle() string {
	t, _ := v.selected()
	return t.Title
}

// ttHints lays a step's key hints out for the window's text width.
func ttHints(hints string, textW int) []string {
	return wrapParts(strings.Split(hints, "  "), textW, "  ")
}

// ttRoom is how many content rows a step has on this terminal, given the
// rows its own chrome takes (title, rules, blank lines, hints).
func ttRoom(m Model, chrome int) int {
	_, termH := m.overlayDims()
	return termH - chrome - st().modalStyle.GetVerticalFrameSize() - 2
}

// fillHints are the fill step's key hints: ↑/↓ shows up once the focused
// value has lines to walk.
func (v *textTemplatesView) fillHints(textW int) []string {
	if v.fill.multiLine() {
		return ttHints(i18n.T("[enter/tab] next  [↑/↓] scroll  [esc] back"), textW)
	}
	return ttHints(i18n.T("[enter/tab] next  [esc] back"), textW)
}

// fillRoom is how many rows the fill step's fields get: what the terminal
// leaves under the title + blank and blank + hints, 16 unless maximized.
func (v *textTemplatesView) fillRoom(m Model) int {
	g := v.geometry(m)
	room := max(1, ttRoom(m, 2+1+len(v.fillHints(g.textW))))
	if !v.maximized {
		room = min(room, 16)
	}
	return room
}

func (v *textTemplatesView) fillBox(m Model) string {
	g := v.geometry(m)
	hints := v.fillHints(g.textW)
	// More rows than room: the shown ones follow the focused field (the
	// title carries its number) and a long value is windowed to its cursor.
	fields := v.fill.viewWindow(g.textW, v.fillRoom(m))
	// The step's position must stay readable: a title too long for the line
	// is cut to what the rest leaves, and shown whole on the bottom bar.
	title, at, n := v.selTitle(), v.fill.idx+1, len(v.fill.labels)
	if titleW := max(1, g.textW-lipgloss.Width(i18n.T("%s — fill variables (%d/%d)", "", at, n))); rowTruncated(title, titleW) {
		v.tipFull, title = title, truncate(title, titleW)
	}
	parts := []string{i18n.T("%s — fill variables (%d/%d)", title, at, n), ""}
	parts = append(parts, fields...)
	parts = append(parts, "")
	parts = append(parts, hints...)
	return popupBox(g.inner, strings.Join(parts, "\n"))
}

// renderedHints are the rendered step's key hints.
func renderedHints(textW int) []string {
	return ttHints(i18n.T("[y] copy and close  [↑/↓] scroll  [esc] back to templates"), textW)
}

// renderedRows is the rendered step's text height: the whole text when it
// fits, else what the terminal leaves (16 rows unless maximized).
func (v *textTemplatesView) renderedRows(m Model) int {
	g := v.geometry(m)
	// title + blank, the rule, blank + hints.
	room := max(3, ttRoom(m, 2+1+1+len(renderedHints(g.textW))))
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
	parts = append(parts, rule, "")
	parts = append(parts, renderedHints(g.textW)...)
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
		if i == v.sel && rowTruncated(t.Title, titleW) {
			v.tipFull = t.Title
		}
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

// openForm opens the add form (t.ID == "") or the edit form seeded from t.
func (v *textTemplatesView) openForm(t model.TextTemplate) {
	v.fTitle = newTextField(t.Title)
	v.scope, v.editID = t.Scope, t.ID
	v.field, v.formErr, v.draft = 0, "", ""
	v.mode = ttForm
}

func (v *textTemplatesView) updateForm(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		v.mode, v.draft, v.formErr = ttBrowse, "", ""
		return m, nil
	case tea.KeyUp:
		v.field = 0
		return m, nil
	case tea.KeyDown, tea.KeyTab:
		v.field = 1
		return m, nil
	case tea.KeyEnter:
		title := strings.TrimSpace(v.fTitle.Value())
		// The title alone: a placeholder text keeps the text checks quiet.
		if err := domain.ValidateTextTemplate(title, "x"); err != nil {
			v.formErr = ttErrText(err)
			return m, nil
		}
		v.formErr = ""
		before := ""
		for _, t := range v.items {
			if t.Scope != v.scope {
				continue
			}
			if v.editID != "" && t.ID == v.editID {
				before = t.Body
			} else if t.ID == domain.TextTemplateID(title) {
				// Refused here, before the editor: a text written for a
				// title that cannot be saved would be wasted work.
				v.formErr = i18n.T("the id %s is already taken in that scope (by %s)", t.ID, t.Title)
				return m, nil
			}
		}
		seed := before
		if v.draft != "" {
			seed = v.draft
		}
		v.handingOff = true
		return m, textTemplateDraftCmd(textTemplateDraftMsg{title: title, scope: v.scope, editID: v.editID, before: before}, seed)
	}
	if v.field == 1 {
		// A template's scope is where it is stored: fixed once it exists.
		if v.editID == "" {
			switch msg.String() {
			case "left", "right", " ", "h", "l":
				if v.scope == model.ProfileScopeGlobal {
					v.scope = model.ProfileScopeRepo
				} else {
					v.scope = model.ProfileScopeGlobal
				}
			}
		}
		return m, nil
	}
	v.fTitle.HandleEditKey(msg)
	return m, nil
}

// ttErrText is a validation error as the form shows it: the user is editing
// a text template, not calling the domain or template packages.
func ttErrText(err error) string {
	s := strings.TrimPrefix(err.Error(), "invalid text template: ")
	s = strings.TrimPrefix(s, "text template: ")
	return strings.Replace(s, "template: ", "", 1)
}

func (v *textTemplatesView) formBox(m Model) string {
	g := v.geometry(m)
	title := i18n.T("Add text template")
	if v.editID != "" {
		title = i18n.T("Edit text template")
	}
	cur, scopeCur := "  ", "  "
	if v.field == 0 {
		cur = "> "
	} else {
		scopeCur = "> "
	}
	scopeVal := i18n.T("global (every repo)")
	if v.scope == model.ProfileScopeRepo {
		scopeVal = i18n.T("this repo only")
	}
	scopeLine := scopeCur + i18n.T("scope: ") + scopeVal
	hint := i18n.T("[↑/↓] field  [←/→] scope  [enter] edit text in $EDITOR  [esc] back")
	if v.editID != "" {
		scopeLine = st().dim.Render(scopeLine)
		hint = i18n.T("[↑/↓] field  [enter] edit text in $EDITOR  [esc] back")
	}
	parts := []string{title, "", viewField(cur+i18n.T("title: "), v.fTitle, v.field == 0, g.textW), scopeLine}
	if v.formErr != "" {
		parts = append(parts, "")
		for _, l := range wrapWidth(v.formErr, g.textW, 4) {
			parts = append(parts, st().errorText.Render(l))
		}
	}
	parts = append(parts, "")
	parts = append(parts, wrapParts(strings.Split(i18n.T("Tokens: <user:LABEL> <date> <date:FMT> <branch> <parent-branch> <repo> <seq:NAME:N> <random-*>"), " "), g.textW, " ")...)
	parts = append(parts, "")
	parts = append(parts, wrapParts(strings.Split(hint, "  "), g.textW, "  ")...)
	return popupBox(g.inner, strings.Join(parts, "\n"))
}

// textTemplateDraftMsg is a text handed to the editor: the temp file that
// holds it and what the form said about the template it belongs to.
type textTemplateDraftMsg struct {
	path   string
	title  string
	scope  model.ProfileScope
	editID string // "" = a new template
	before string // the stored text ("" for a new template)
	err    error
}

// textTemplateEditedMsg: the editor closed on the draft's file (err is the
// editor's exit error).
type textTemplateEditedMsg struct{ textTemplateDraftMsg }

// textTemplateDraftCmd writes seed to a private temp file ending in .md (so
// the editor highlights it) and yields the draft; the Model then hands the
// file to the editor.
func textTemplateDraftCmd(d textTemplateDraftMsg, seed string) tea.Cmd {
	return func() tea.Msg {
		f, err := os.CreateTemp("", "gg-*-template.md")
		if err != nil {
			d.err = err
			return d
		}
		d.path = f.Name()
		_, werr := f.WriteString(seed)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			removeTempFile(d.path)
			d.path, d.err = "", werr
		}
		return d
	}
}

// editTextTemplateCmd suspends the TUI on the draft's file in the user's
// editor; on exit it yields a textTemplateEditedMsg.
func editTextTemplateCmd(d textTemplateDraftMsg) tea.Cmd {
	cmd := editorCommandAt(resolveEditor(), d.path, 0)
	return handover(cmd, func(err error) tea.Msg {
		d.err = err
		return textTemplateEditedMsg{d}
	})
}

// onEdited reads the text the editor left and saves it — or says why not.
// The caller has already removed the temp file. A text that cannot be saved
// (a malformed token) keeps the form open with the text as the next draft.
func (v *textTemplatesView) onEdited(m Model, msg textTemplateEditedMsg, data []byte, readErr error) (Model, tea.Cmd) {
	body := strings.TrimRight(string(data), " \t\r\n")
	oldTitle := ""
	for _, t := range v.items {
		if msg.editID != "" && t.ID == msg.editID && t.Scope == msg.scope {
			oldTitle = t.Title
		}
	}
	done := func(status string) (Model, tea.Cmd) {
		v.mode, v.draft, v.formErr = ttBrowse, "", ""
		m.statusMsg, v.tipHold = status, true
		return m, nil
	}
	switch {
	case msg.err != nil:
		return done(i18n.T("text template not saved: %s", msg.err.Error()))
	case readErr != nil:
		return done(i18n.T("text template not saved: %s", readErr.Error()))
	case strings.TrimSpace(body) == "":
		return done(i18n.T("text template not saved: the text is empty"))
	case msg.editID != "" && body == strings.TrimRight(msg.before, " \t\r\n") && msg.title == oldTitle:
		return done(i18n.T("text template unchanged"))
	}
	if err := domain.ValidateTextTemplate(msg.title, body); err != nil {
		v.mode, v.formErr, v.draft = ttForm, ttErrText(err), body
		return m, nil
	}
	v.mode, v.draft, v.loading = ttBrowse, "", true
	return m, m.saveTextTemplateCmd(msg.textTemplateDraftMsg, body)
}

func (m Model) saveTextTemplateCmd(d textTemplateDraftMsg, body string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		ctx := context.Background()
		t := model.TextTemplate{Title: d.title, Body: body, Scope: d.scope}
		var saved model.TextTemplate
		var err error
		if d.editID == "" {
			saved, err = svc.AddTextTemplate(ctx, t)
		} else {
			saved, err = svc.UpdateTextTemplate(ctx, d.scope, d.editID, t)
		}
		if err != nil {
			return textTemplateSaveFailedMsg{err: err, body: body}
		}
		items, lerr := svc.TextTemplates(ctx)
		return textTemplatesDataMsg{items: items, err: lerr, selectID: saved.ID, selectScope: saved.Scope, status: i18n.T("saved text template %s", saved.Title)}
	}
}

// textTemplateSaveFailedMsg: the store refused a text the editor returned
// (a title taken meanwhile, a row removed by another gg).
type textTemplateSaveFailedMsg struct {
	err  error
	body string
}

// onSaveFailed reopens the form — it still holds the title and scope — with
// the reason and the written text as the next draft, so nothing typed in the
// editor is lost.
func (v *textTemplatesView) onSaveFailed(msg textTemplateSaveFailedMsg) {
	v.loading = false
	v.mode, v.formErr, v.draft = ttForm, ttErrText(msg.err), msg.body
}

// updateConfirm answers the delete question: y deletes, n or esc keeps. Any
// other key leaves the question open — a stray key must not answer it.
func (v *textTemplatesView) updateConfirm(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyEsc || msg.String() == "n":
		v.mode = ttBrowse
	case msg.String() == "y":
		v.mode = ttBrowse
		if t, ok := v.selected(); ok {
			return m, m.removeTextTemplateCmd(t)
		}
	}
	return m, nil
}

func (m Model) removeTextTemplateCmd(t model.TextTemplate) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		ctx := context.Background()
		if err := svc.RemoveTextTemplate(ctx, t.Scope, t.ID); err != nil {
			return textTemplatesDataMsg{err: err}
		}
		items, err := svc.TextTemplates(ctx)
		return textTemplatesDataMsg{items: items, err: err, status: i18n.T("deleted text template %s", t.Title)}
	}
}

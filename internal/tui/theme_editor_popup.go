package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/theme"
)

// Column layout of one editor row, in display columns:
//
//	mark(2) key(20) swatch(3) value(10) default(13) doc(rest)
//
// The mark column carries ONE glyph: `*` when the global config overrides this
// role, else `>` for the cursor. The override marker wins deliberately — the
// selected row is already reverse-video, so the cursor is never in doubt, while
// losing the `*` would hide the one fact the row exists to report.
const (
	themeEdMarkW   = 2
	themeEdKeyW    = 20
	themeEdSwatchW = 3 // two painted cells + a separating space
	themeEdValueW  = 10
	themeEdDefW    = 13

	// themeEdSwatchAt is the first of the two painted swatch cells.
	themeEdSwatchAt = themeEdMarkW + themeEdKeyW
	// themeEdValueAt is the first column of the value cell — the slot the
	// inline editor types into.
	themeEdValueAt = themeEdSwatchAt + themeEdSwatchW

	// themeEditorRows is the unmaximized visible-row budget; ctrl+t (popupMax)
	// trades it for the terminal-derived cap.
	themeEditorRows = 14
)

// themeUnsetMark is what an unset ("inherit") role shows in the value column.
const themeUnsetMark = "–"

// themeEditorPopup is the Settings → "Theme colours…" surface: every editable
// role of the ACTIVE theme, with a live-painted sample, the built-in default it
// would fall back to, and an inline editor that previews the typed colour on the
// whole screen before enter writes it to the global config's [themes.<name>].
//
// It reads the global and repo override layers SEPARATELY (m.cfg.Themes is the
// merged view) because the two behave differently: the global one is what this
// editor writes, and a role the repo .gg.toml pins is shown but read-only —
// editing it here would write a line the repo file immediately shadows.
type themeEditorPopup struct {
	popupMax

	base   theme.Theme    // the built-in palette of the active theme
	global theme.Override // [themes.<name>] in the GLOBAL config — what we write
	repo   theme.Override // [themes.<name>] in the repo config — read-only here

	globalPath string
	roles      []theme.RoleRef
	sel        int

	filter    textfield
	filtering bool

	editing bool
	field   textfield
	// previewBase is the resolved theme as it stood when editing began: esc
	// (and every invalid keystroke) puts the screen back to exactly it.
	previewBase theme.Theme

	status    string
	statusErr bool
}

// newThemeEditor builds the editor for the model's active theme. The two
// override layers are re-read from disk rather than taken from m.cfg, which
// only carries their merge.
func newThemeEditor(m Model) *themeEditorPopup {
	base, ok := theme.Lookup(m.cfg.UI.Theme)
	if !ok {
		base = theme.Terminal
	}
	globalPath := config.DefaultGlobalPath()
	p := &themeEditorPopup{
		base:       base,
		globalPath: globalPath,
		roles:      theme.Roles(),
		filter:     newTextField(""),
	}
	p.global = themeOverrideIn(globalPath, "", base.Name)
	p.repo = themeOverrideIn("", m.repoConfigPath, base.Name)
	return p
}

// themeOverrideIn reads one config layer's [themes.<name>] table. A missing or
// unreadable file yields the empty (no-op) override — the editor must open even
// when the global config does not exist yet.
func themeOverrideIn(globalPath, repoPath, name string) theme.Override {
	cfg, err := config.Load(globalPath, repoPath)
	if err != nil {
		return theme.Override{}
	}
	return cfg.Themes[name]
}

// effective is the theme the rows report: the built-in palette with both
// override layers on top, repo winning — the same resolution applyTheme does.
func (p *themeEditorPopup) effective() theme.Theme {
	th, _ := theme.Overlay(p.base, theme.Merge(p.global, p.repo))
	return th
}

// shown is the theme the value column and the swatches sample. While the inline
// editor is open that is the LIVE preview (already installed by setTheme), so
// the row under the cursor shows the colour being typed.
func (p *themeEditorPopup) shown() theme.Theme {
	if p.editing {
		return activeTheme()
	}
	return p.effective()
}

// previewFor resolves the theme that setting r to v would produce.
func (p *themeEditorPopup) previewFor(r theme.RoleRef, v string) theme.Theme {
	th, _ := theme.Overlay(p.base, theme.Merge(r.OverrideSet(p.global, p.base, v), p.repo))
	return th
}

// repoShadows reports whether the repo config pins this role, making it
// read-only here. A list is set as a unit, so a repo `lanes = […]` shadows all
// seven lane rows.
func (p *themeEditorPopup) repoShadows(r theme.RoleRef) bool {
	switch r.ListKey {
	case "lanes":
		return p.repo.Lanes != nil
	case "syntax":
		return p.repo.Syntax != nil
	}
	return p.repo.Value(r.Key) != ""
}

// visible is the roles the current filter keeps (case-insensitive substring of
// the key or the description).
func (p *themeEditorPopup) visible() []theme.RoleRef {
	q := strings.ToLower(strings.TrimSpace(p.filter.Value()))
	if q == "" {
		return p.roles
	}
	out := make([]theme.RoleRef, 0, len(p.roles))
	for _, r := range p.roles {
		if strings.Contains(strings.ToLower(r.Key+" "+r.Doc), q) {
			out = append(out, r)
		}
	}
	return out
}

// current is the role under the cursor in the filtered view.
func (p *themeEditorPopup) current() (theme.RoleRef, bool) {
	vis := p.visible()
	if p.sel < 0 || p.sel >= len(vis) {
		return theme.RoleRef{}, false
	}
	return vis[p.sel], true
}

// move steps the selection by d, clamped to the filtered list.
func (p *themeEditorPopup) move(d int) {
	n := len(p.visible())
	if n == 0 {
		p.sel = 0
		return
	}
	p.sel = min(max(p.sel+d, 0), n-1)
}

// openThemeEditor pushes the editor over the Settings popup, so esc returns to
// the menu the user came from.
func (m Model) openThemeEditor() (Model, tea.Cmd) {
	return m.pushLayer(newThemeEditor(m)), nil
}

func (p *themeEditorPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if p.editing {
		return p.updateEditing(m, msg)
	}
	if p.filtering {
		return p.updateFilter(m, msg)
	}
	return p.updateBrowse(m, msg)
}

// updateFilter is the `/` sub-mode: arrows still move the selection live, every
// other editing key types into the query. Letters are query text here and
// bindings in browse mode — the same split the git-config explorer uses.
func (p *themeEditorPopup) updateFilter(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if filterMotion(msg, p.move, popupFilterPage) {
		return m, nil
	}
	switch msg.Type {
	case tea.KeyEsc:
		p.filtering, p.sel = false, 0
		p.filter = newTextField("")
		return m, nil
	case tea.KeyEnter:
		p.filtering = false // commit: keep the query, leave input mode
		return m, nil
	}
	before := p.filter.Value()
	p.filter.HandleEditKey(msg)
	if p.filter.Value() != before {
		p.sel = 0 // the filtered list changed under the cursor
	}
	return m, nil
}

func (p *themeEditorPopup) updateBrowse(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyUp:
		p.move(-1)
		return m, nil
	case tea.KeyDown:
		p.move(1)
		return m, nil
	case tea.KeyPgUp:
		p.move(-popupFilterPage)
		return m, nil
	case tea.KeyPgDown:
		p.move(popupFilterPage)
		return m, nil
	case tea.KeyHome:
		p.sel = 0
		return m, nil
	case tea.KeyEnd:
		p.move(len(p.visible()))
		return m, nil
	case tea.KeyEnter:
		return p.startEditing(m), nil
	case tea.KeyRunes:
		switch msg.String() {
		case "/":
			p.filtering = true
		case "j":
			p.move(1)
		case "k":
			p.move(-1)
		case "d":
			return p.resetRole(m)
		case "t":
			return p.cycleTheme(m)
		}
		return m, nil
	}
	return m, nil
}

// startEditing opens the inline editor on the selected row, prefilled with the
// effective colour. A repo-pinned role refuses and says where to change it.
func (p *themeEditorPopup) startEditing(m Model) Model {
	r, ok := p.current()
	if !ok {
		return m
	}
	if p.repoShadows(r) {
		p.setStatus(true, i18n.T("set by the repo .gg.toml — edit it there"))
		return m
	}
	p.previewBase = activeTheme()
	p.field = newTextField(r.Get(p.effective()))
	p.editing = true
	p.setStatus(false, i18n.T("type a colour — the whole screen previews it"))
	return m
}

// updateEditing routes keys to the inline field. Every keystroke re-previews:
// a valid (or empty) value repaints the screen through it, an invalid one puts
// the screen straight back to previewBase so a typo never leaves the UI in a
// half-applied state.
func (p *themeEditorPopup) updateEditing(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		p.editing = false
		p.status, p.statusErr = "", false
		setTheme(p.previewBase)
		return m, tea.ClearScreen
	case tea.KeyEnter:
		return p.save(m)
	}
	if !p.field.HandleEditKey(msg) {
		return m, nil
	}
	return m, p.preview()
}

// preview installs the styles for the current field value and sets the status.
func (p *themeEditorPopup) preview() tea.Cmd {
	r, ok := p.current()
	if !ok {
		return nil
	}
	v := strings.TrimSpace(p.field.Value())
	switch {
	case v == "":
		setTheme(p.previewFor(r, ""))
		p.setStatus(false, i18n.T("empty — previewing the built-in default; enter clears [themes.%s] %s", p.base.Name, themeConfigKey(r)))
	case theme.ValidColour(v):
		setTheme(p.previewFor(r, v))
		p.setStatus(false, i18n.T("%s valid — previewing; enter saves to [themes.%s] %s, esc reverts", v, p.base.Name, themeConfigKey(r)))
	default:
		setTheme(p.previewBase)
		p.setStatus(true, i18n.T("not a colour (#rrggbb or 0–255)"))
	}
	return tea.ClearScreen
}

// save writes the edited role to the global config and re-applies the theme
// from the merged config, so what stays on screen is what a restart would show.
func (p *themeEditorPopup) save(m Model) (Model, tea.Cmd) {
	r, ok := p.current()
	if !ok {
		return m, nil
	}
	v := strings.TrimSpace(p.field.Value())
	if v != "" && !theme.ValidColour(v) {
		p.setStatus(true, i18n.T("not a colour (#rrggbb or 0–255)"))
		return m, nil // stay in the editor: nothing is written, nothing is lost
	}
	next := r.OverrideSet(p.global, p.base, v)
	if err := config.SetThemeRole(p.globalPath, p.base.Name, themeConfigKey(r), themeWriteValues(next, r, v)...); err != nil {
		setTheme(p.previewBase)
		p.setStatus(true, i18n.T("not saved: %s", err.Error()))
		return m, tea.ClearScreen
	}
	p.global = next
	p.editing = false
	m, cmd := p.reapply(m)
	if v == "" {
		p.setStatus(false, i18n.T("%s restored to the built-in default", r.Key))
	} else {
		p.setStatus(false, i18n.T("saved %s = %s to [themes.%s]", themeConfigKey(r), v, p.base.Name))
	}
	return m, cmd
}

// resetRole is the `d` key: drop this role's global override so the built-in
// value paints again.
func (p *themeEditorPopup) resetRole(m Model) (Model, tea.Cmd) {
	r, ok := p.current()
	if !ok {
		return m, nil
	}
	if p.repoShadows(r) {
		p.setStatus(true, i18n.T("set by the repo .gg.toml — edit it there"))
		return m, nil
	}
	next, changed := themeClearRole(p.global, r)
	if !changed {
		p.setStatus(false, i18n.T("%s is already the built-in default", r.Key))
		return m, nil
	}
	if err := config.SetThemeRole(p.globalPath, p.base.Name, themeConfigKey(r), themeWriteValues(next, r, "")...); err != nil {
		p.setStatus(true, i18n.T("not saved: %s", err.Error()))
		return m, nil
	}
	p.global = next
	m, cmd := p.reapply(m)
	p.setStatus(false, i18n.T("%s restored to the built-in default", r.Key))
	return m, cmd
}

// cycleTheme is the `t` key: hand off to the Settings cycle, then rebuild the
// editor around the theme that is now active.
func (p *themeEditorPopup) cycleTheme(m Model) (Model, tea.Cmd) {
	m, cmd := m.cycleTheme()
	base, ok := theme.Lookup(m.cfg.UI.Theme)
	if !ok {
		base = theme.Terminal
	}
	p.base = base
	p.global = themeOverrideIn(p.globalPath, "", base.Name)
	p.repo = themeOverrideIn("", m.repoConfigPath, base.Name)
	p.status, p.statusErr = "", false
	return m, cmd
}

// reapply folds the popup's layers back into the model's config and re-applies
// the theme, so the screen shows the SAVED state rather than the preview.
func (p *themeEditorPopup) reapply(m Model) (Model, tea.Cmd) {
	themes := make(map[string]theme.Override, len(m.cfg.Themes)+1)
	for k, v := range m.cfg.Themes {
		themes[k] = v
	}
	themes[p.base.Name] = theme.Merge(p.global, p.repo)
	m.cfg.Themes = themes
	m, cmd := m.applyTheme()
	if cmd == nil {
		// The resolved theme did not change (re-saving the same colour), but the
		// popup's own rows did — repaint anyway.
		cmd = tea.ClearScreen
	}
	return m, cmd
}

func (p *themeEditorPopup) setStatus(isErr bool, s string) {
	p.status, p.statusErr = s, isErr
}

// themeConfigKey is the TOML key a role writes to: a list entry's whole array
// key ("lanes"), a scalar's own key.
func themeConfigKey(r theme.RoleRef) string {
	if r.ListKey != "" {
		return r.ListKey
	}
	return r.Key
}

// themeWriteValues is what SetThemeRole must write for r after o was updated: a
// scalar's single value (v "" removes the key), or a list's WHOLE array — which
// collapses to a removal once every entry is empty again.
func themeWriteValues(o theme.Override, r theme.RoleRef, v string) []string {
	var list []string
	switch r.ListKey {
	case "lanes":
		list = o.Lanes
	case "syntax":
		list = o.Syntax
	default:
		return []string{v}
	}
	for _, e := range list {
		if e != "" {
			return list
		}
	}
	return []string{""} // every entry inherits again: drop the line
}

// themeClearRole removes r from o, reporting whether anything was set. A list
// entry is blanked IN PLACE (never expanded from the base), so resetting one
// lane cannot pin the other six.
func themeClearRole(o theme.Override, r theme.RoleRef) (theme.Override, bool) {
	clear := func(list []string) ([]string, bool) {
		if list == nil || r.Index < 0 || r.Index >= len(list) || list[r.Index] == "" {
			return list, false
		}
		out := append([]string(nil), list...)
		out[r.Index] = ""
		return out, true
	}
	out := o
	switch r.ListKey {
	case "lanes":
		var ok bool
		out.Lanes, ok = clear(o.Lanes)
		return out, ok
	case "syntax":
		var ok bool
		out.Syntax, ok = clear(o.Syntax)
		return out, ok
	}
	if o.Value(r.Key) == "" {
		return o, false
	}
	return r.OverrideSet(o, theme.Theme{}, ""), true
}

func (p *themeEditorPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

func (p *themeEditorPopup) box(m Model) string {
	w, termH := m.overlayDims()
	inner := popupResolveWidth(w, p.maximized, popupWideInnerWidth(w))
	textW := popupTextWidth(inner)
	s := st()

	parts := []string{p.headerLine(textW), ""}
	if p.filtering || p.filter.Value() != "" {
		parts = append(parts, viewField("/ ", p.filter, p.filtering, textW), "")
	}
	parts = append(parts, s.dim.Render(themeColumnHeader(textW)))

	vis := p.visible()
	if len(vis) == 0 {
		parts = append(parts, padRight("  "+i18n.T("(no matching colours)"), textW))
	} else {
		rows := p.rows(vis, textW)
		parts = append(parts, renderWindow(rows, winOpts{
			w: textW, anchor: p.sel,
			h: min(len(rows), popupResolveRowCap(p.maximized, termH, themeEditorRows)),
		})...)
	}

	parts = append(parts, "", s.dim.Render(truncate(
		i18n.T("%d of %d rows · * = overridden here · (repo) = set by .gg.toml, read-only", len(vis), len(p.roles)), textW)))
	if p.status != "" {
		style, mark := s.reviewDim, "✓ "
		if p.statusErr {
			style, mark = s.errorText, "✗ "
		}
		parts = append(parts, style.Render(truncate(mark+p.status, textW)))
	}
	parts = append(parts, "")
	parts = append(parts, p.hints(textW)...)
	return popupBox(inner, strings.Join(parts, "\n"))
}

// headerLine is the title on the left and the write target (plus the theme
// switch hint) pushed to the right edge.
func (p *themeEditorPopup) headerLine(textW int) string {
	s := st()
	left := s.titleStyle.Render(i18n.T("Theme colours — %s", themeDisplayName(p.base.Name)))
	path := p.globalPath
	if path == "" {
		path = i18n.T("(no global config path)")
	}
	right := i18n.T("global: %s", elidePath(path, 40)) + "  " + i18n.T("[t] theme")
	gap := textW - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 2 {
		return left
	}
	return left + strings.Repeat(" ", gap) + s.dim.Render(right)
}

// themeColumnHeader labels the fixed columns; `paints` heads the description.
func themeColumnHeader(textW int) string {
	line := strings.Repeat(" ", themeEdMarkW) +
		padCell(i18n.T("key"), themeEdKeyW) +
		padCell(i18n.T("value"), themeEdSwatchW+themeEdValueW) +
		padCell(i18n.T("default"), themeEdDefW) +
		i18n.T("paints")
	return truncate(line, textW)
}

// rows builds one winRow per visible role. Every row is laid out as PLAIN text
// (so the window's width maths stays honest) and painted by a decorator: the
// row style, the two swatch cells in the role's own colour, and — while the
// inline editor is open — the value cell on the field background. The row style
// cannot do that job: renderWindow wraps the whole line in it AFTER the
// decorator runs, and a reverse-video row would swallow the sample.
func (p *themeEditorPopup) rows(vis []theme.RoleRef, textW int) []winRow {
	shown := p.shown()
	docW := max(textW-themeEdValueAt-themeEdValueW-themeEdDefW, 8)
	out := make([]winRow, len(vis))
	for i, r := range vis {
		selected := i == p.sel
		value := r.Get(shown)
		editing := selected && p.editing

		mark := " "
		switch {
		case r.OverrideGet(p.global) != "":
			mark = "*"
		case selected:
			mark = ">"
		}

		valCell := value
		if valCell == "" {
			valCell = themeUnsetMark
		}
		if editing {
			valCell = p.field.Value() + "█"
		}

		def := r.Get(p.base)
		if def == "" {
			def = themeUnsetMark
		}
		doc := r.Doc
		if p.repoShadows(r) {
			doc += "  " + i18n.T("(repo)")
		}

		// The swatch is two cells: a filled pair for a background role, ■■ for a
		// foreground one, blank when the role inherits (nothing to sample).
		swatch := "  "
		if value != "" && !r.IsBg {
			swatch = "■■"
		}

		text := padCell(mark, themeEdMarkW) +
			padCell(truncate(r.Key, themeEdKeyW-1), themeEdKeyW) +
			padCell(swatch, themeEdSwatchW) +
			padCell(truncate(valCell, themeEdValueW-1), themeEdValueW) +
			padCell(truncate(def, themeEdDefW-1), themeEdDefW) +
			truncate(doc, docW)

		rowStyle := lipgloss.Style{}
		if selected {
			rowStyle = st().selectedRow
		}
		var spans []rowSpan
		if value != "" {
			spans = append(spans, rowSpan{start: themeEdSwatchAt, width: 2, style: swatchStyle(value, r.IsBg)})
		}
		if editing {
			spans = append(spans, rowSpan{start: themeEdValueAt, width: themeEdValueW - 1, style: st().field})
		}
		out[i] = winRow{text: text, decorate: themeRowDecorator(rowStyle, spans)}
	}
	return out
}

// rowSpan is a run of display columns painted with its own style, independent of
// the row style around it.
type rowSpan struct {
	start, width int
	style        lipgloss.Style
}

// themeRowDecorator paints a laid-out row: everything outside the spans under
// rowStyle, each span under its own. Returning a fully styled line is why the
// caller leaves winRow.style zero — see rows().
func themeRowDecorator(rowStyle lipgloss.Style, spans []rowSpan) rowDecorator {
	return func(visible string, hscroll, visualLine int) string {
		r := []rune(visible)
		var b strings.Builder
		at := 0
		for _, sp := range spans {
			if sp.start < at || sp.start+sp.width > len(r) {
				continue // the row was truncated past this span
			}
			b.WriteString(rowStyle.Render(string(r[at:sp.start])))
			b.WriteString(sp.style.Render(string(r[sp.start : sp.start+sp.width])))
			at = sp.start + sp.width
		}
		b.WriteString(rowStyle.Render(string(r[at:])))
		return b.String()
	}
}

// hints is the footer: the browse bindings, or the editor's save/revert pair
// plus the accepted colour syntax.
func (p *themeEditorPopup) hints(textW int) []string {
	if p.editing {
		return wrapParts([]string{
			i18n.T("[enter] save"),
			i18n.T("[esc] revert"),
			i18n.T("#rrggbb or 0–255, empty = default"),
		}, textW, "  ")
	}
	return wrapParts([]string{
		i18n.T("[↑/↓] select"),
		i18n.T("[enter] edit"),
		i18n.T("[d] default"),
		i18n.T("[t] theme"),
		i18n.T("[/] filter"),
		i18n.T("[ctrl+t] fullscreen"),
		i18n.T("[esc] close"),
	}, textW, "  ")
}

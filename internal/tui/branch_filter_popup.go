package tui

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/branchfilter"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/i18n"
)

// branchFilterView is Settings → Branch filters…: the five slots as rows
// (browse), and a form editing one slot (edit). Saves go through the scoped
// block writers and reload m.cfg so the open panels re-evaluate at once.
// Mirrors prefixSettingsView's browse/form split.
type branchFilterView struct {
	popupMax
	sel     int // 0..4 = slot-1
	mode    bfvMode
	fields  [bfFieldCount]textfield
	fmode   branchfilter.Mode
	scope   bfScope
	field   int    // focused form row (one of the row* constants)
	formErr string // inline validation error; "" = none
}

type bfvMode int

const (
	bfvBrowse bfvMode = iota
	bfvForm
)

type bfScope int

const (
	bfScopeGlobal bfScope = iota
	bfScopeRepo
)

// Text fields in the form. Indexes into branchFilterView.fields; the two
// toggles (mode, scope) are not text and have no entry here.
const (
	bfName = iota
	bfOlder
	bfYounger
	bfPrefix
	bfSuffix
	bfContains
	bfRegex
	bfFieldCount
)

// The form's row order: the text fields interleaved with the two toggles.
// branchFilterView.field is one of these; textField maps it back to a field.
const (
	rowName = iota
	rowMode
	rowOlder
	rowYounger
	rowPrefix
	rowSuffix
	rowContains
	rowRegex
	rowScope
	rowCount
)

func (m Model) openBranchFilterSettings() (Model, tea.Cmd) {
	return m.pushLayer(&branchFilterView{}), nil
}

// bfRowLabel translates a form row's label at render time (a package var
// would freeze the English text before any language loads). The labels carry
// no padding: box aligns the column with padCell/maxLabelWidth, because a
// translation's display width is not the English literal's (a CJK label
// padded to the English length clips).
func bfRowLabel(row int) string {
	switch row {
	case rowName:
		return i18n.T("name:")
	case rowMode:
		return i18n.T("mode:")
	case rowOlder:
		return i18n.T("older than:")
	case rowYounger:
		return i18n.T("younger than:")
	case rowPrefix:
		return i18n.T("prefix:")
	case rowSuffix:
		return i18n.T("suffix:")
	case rowContains:
		return i18n.T("contains:")
	case rowRegex:
		return i18n.T("regex:")
	case rowScope:
		return i18n.T("scope:")
	}
	return ""
}

// textField returns the text field a form row edits, or nil for the toggles.
func (v *branchFilterView) textField(row int) *textfield {
	switch row {
	case rowName:
		return &v.fields[bfName]
	case rowOlder:
		return &v.fields[bfOlder]
	case rowYounger:
		return &v.fields[bfYounger]
	case rowPrefix:
		return &v.fields[bfPrefix]
	case rowSuffix:
		return &v.fields[bfSuffix]
	case rowContains:
		return &v.fields[bfContains]
	case rowRegex:
		return &v.fields[bfRegex]
	}
	return nil
}

// openForm prefills the form from the compiled slot (an empty slot starts
// blank, mode hide). inRepo prefills the scope: editing a rule this repo
// defines must default to rewriting THAT block, not writing a global copy of
// it — a global copy would be invisible here (the repo block still overrides
// it) while silently activating the repo's rule in every other repo.
func (v *branchFilterView) openForm(c branchfilter.Compiled, inRepo bool) {
	s := c.Slot
	v.fields[bfName] = newTextField(s.Name)
	v.fields[bfOlder] = newTextField(strings.TrimSpace(s.OlderThan))
	v.fields[bfYounger] = newTextField(strings.TrimSpace(s.YoungerThan))
	v.fields[bfPrefix] = newTextField(s.Prefix)
	v.fields[bfSuffix] = newTextField(s.Suffix)
	v.fields[bfContains] = newTextField(s.Contains)
	v.fields[bfRegex] = newTextField(s.Regex)
	v.fmode = s.Mode
	if v.fmode == "" {
		v.fmode = branchfilter.ModeHide
	}
	v.scope = bfScopeGlobal
	if inRepo {
		v.scope = bfScopeRepo
	}
	v.field = rowName
	v.formErr = ""
	v.mode = bfvForm
}

// formSlot assembles the Slot the form describes for slot number n.
func (v *branchFilterView) formSlot(n int) branchfilter.Slot {
	return branchfilter.Slot{
		Slot:        n,
		Name:        strings.TrimSpace(v.fields[bfName].Value()),
		Mode:        v.fmode,
		OlderThan:   strings.TrimSpace(v.fields[bfOlder].Value()),
		YoungerThan: strings.TrimSpace(v.fields[bfYounger].Value()),
		Prefix:      v.fields[bfPrefix].Value(),
		Suffix:      v.fields[bfSuffix].Value(),
		Contains:    v.fields[bfContains].Value(),
		Regex:       v.fields[bfRegex].Value(),
	}
}

// writePath is the config file the form's scope targets. "" when the repo
// scope has no path (no repo config resolved yet).
func (v *branchFilterView) writePath(m Model) string {
	if v.scope == bfScopeRepo {
		return m.repoConfigPath
	}
	return config.DefaultGlobalPath()
}

func (v *branchFilterView) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if v.mode == bfvForm {
		return v.updateForm(m, msg)
	}
	return v.updateBrowse(m, msg)
}

func (v *branchFilterView) updateBrowse(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyUp:
		if v.sel > 0 {
			v.sel--
		}
		return m, nil
	case tea.KeyDown:
		if v.sel < branchfilter.MaxSlots-1 {
			v.sel++
		}
		return m, nil
	case tea.KeyEnter:
		// The EFFECTIVE layer is the repo one whenever the repo file defines
		// the slot (including when both files do) — the same precedence [d]
		// peels by.
		_, inRepo := config.BranchFilterScopes(config.DefaultGlobalPath(), m.repoConfigPath, v.sel+1)
		v.openForm(m.branchFilters[v.sel], inRepo)
		return m, nil
	}
	switch msg.String() {
	case "j":
		if v.sel < branchfilter.MaxSlots-1 {
			v.sel++
		}
	case "k":
		if v.sel > 0 {
			v.sel--
		}
	case "d":
		return v.removeSelected(m)
	}
	return m, nil
}

// removeSelected is [d]: it peels the EFFECTIVE layer only — the repo
// definition when there is one, else the global one. A global rule is never
// removed from a repo's Settings as a side effect (the user would lose it in
// every other repo without ever being told); pressing d again removes it
// explicitly, and the status line says so.
func (v *branchFilterView) removeSelected(m Model) (Model, tea.Cmd) {
	n := v.sel + 1
	inGlobal, inRepo := config.BranchFilterScopes(config.DefaultGlobalPath(), m.repoConfigPath, n)
	var path string
	switch {
	case inRepo:
		path = m.repoConfigPath
	case inGlobal:
		path = config.DefaultGlobalPath()
	default:
		m.statusMsg = i18n.T("slot %d is not defined in this repo's or the global config", n)
		return m, nil
	}
	if err := config.RemoveBranchFilter(path, n); err != nil {
		m.statusMsg = i18n.T("branch filter %d not removed: %s", n, err.Error())
		return m, nil
	}
	m = m.reloadConfigAfterBranchFilterWrite()
	switch {
	case inRepo && inGlobal:
		m.statusMsg = i18n.T("branch filter %d: repo definition removed — the global one now applies (d again removes it too)", n)
	case inRepo:
		m.statusMsg = i18n.T("branch filter %d removed from this repo's config", n)
	default:
		m.statusMsg = i18n.T("branch filter %d removed from the global config", n)
	}
	return m, nil
}

func (v *branchFilterView) updateForm(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		v.mode = bfvBrowse
		return m, nil
	case tea.KeyUp:
		if v.field > 0 {
			v.field--
		}
		return m, nil
	case tea.KeyDown, tea.KeyTab:
		if v.field < rowCount-1 {
			v.field++
		}
		return m, nil
	case tea.KeyEnter:
		return v.save(m)
	}
	switch v.field {
	case rowMode:
		switch msg.String() {
		case "left", "right", " ", "h", "l":
			if v.fmode == branchfilter.ModeHide {
				v.fmode = branchfilter.ModeShow
			} else {
				v.fmode = branchfilter.ModeHide
			}
		}
		return m, nil
	case rowScope:
		switch msg.String() {
		case "left", "right", " ", "h", "l":
			if v.scope == bfScopeGlobal {
				v.scope = bfScopeRepo
			} else {
				v.scope = bfScopeGlobal
			}
		}
		return m, nil
	}
	if f := v.textField(v.field); f != nil {
		f.HandleEditKey(msg)
	}
	return m, nil
}

// save validates the form and writes it into the scope's config file. Every
// refusal sets formErr and writes NOTHING — the form stays open on the value
// the user typed, so a bad regex or a clause-less rule is fixed in place
// rather than landing in the file as an inert block.
func (v *branchFilterView) save(m Model) (Model, tea.Cmd) {
	n := v.sel + 1
	s := v.formSlot(n)
	c := branchfilter.Compile(s)
	if c.Err != nil {
		v.formErr = c.Err.Error()
		return m, nil
	}
	if c.Empty {
		v.formErr = i18n.T("set at least one clause (age, prefix, suffix, contains or regex)")
		return m, nil
	}
	path := v.writePath(m)
	if path == "" {
		v.formErr = i18n.T("no repo config path — choose global, or open a repo first")
		return m, nil
	}
	if err := config.SetBranchFilter(path, s); err != nil {
		v.formErr = err.Error()
		return m, nil
	}
	v.formErr = ""
	v.mode = bfvBrowse
	m = m.reloadConfigAfterBranchFilterWrite()
	m.statusMsg = i18n.T("branch filter %d saved: %s", n, bfLabel(m.branchFilters[n-1]))
	return m, nil
}

func (v *branchFilterView) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), v.box(m), w, h)
}

func (v *branchFilterView) box(m Model) string {
	w, _ := m.overlayDims()
	inner := popupResolveWidth(w, v.maximized, popupWideInnerWidth(w))
	textW := popupTextWidth(inner)
	s := st()

	if v.mode == bfvForm {
		return popupBox(inner, strings.Join(v.formLines(textW, s), "\n"))
	}
	return popupBox(inner, strings.Join(v.browseLines(m, textW, s), "\n"))
}

// formLines renders the nine form rows, the inline error and the hints.
func (v *branchFilterView) formLines(textW int, s *styles) []string {
	labels := make([]string, rowCount)
	for row := range labels {
		labels[row] = bfRowLabel(row)
	}
	lw := maxLabelWidth(0, labels...)
	parts := []string{i18n.T("Branch filter %d", v.sel+1), ""}
	for row := 0; row < rowCount; row++ {
		cur := "  "
		if row == v.field {
			cur = "> "
		}
		prefix := cur + padCell(labels[row], lw) + " "
		switch row {
		case rowMode:
			val := i18n.T("hide matching branches")
			if v.fmode == branchfilter.ModeShow {
				val = i18n.T("show ONLY matching branches")
			}
			parts = append(parts, prefix+val)
		case rowScope:
			val := i18n.T("global (every repo)")
			if v.scope == bfScopeRepo {
				val = i18n.T("this repo only")
			}
			parts = append(parts, prefix+val)
		default:
			parts = append(parts, viewField(prefix, *v.textField(row), row == v.field, textW))
		}
	}
	if v.formErr != "" {
		parts = append(parts, "", s.errorText.Render(v.formErr))
	}
	// The help sentence is ONE key (a sentence never concatenates fragments),
	// re-wrapped here on its own " · " separators so a translation longer than
	// the box is laid out over two lines instead of truncated by popupBox. A
	// translation that drops the separators degrades to one long line, exactly
	// as it renders today.
	parts = append(parts, "")
	parts = append(parts, wrapParts(strings.Split(i18n.T("ages: 90d 12w 6m 1y · set clauses AND together · regex is Go RE2 · names are the branch part (no origin/)"), " · "), textW, " · ")...)
	hint := []string{i18n.T("[↑/↓/tab] field"), i18n.T("[←/→] toggle"), i18n.T("[enter] save"), i18n.T("[esc] back")}
	return append(append(parts, ""), wrapParts(hint, textW, "  ")...)
}

// browseLines renders the five slot rows with their provenance tags, any
// config warnings, and the hints.
func (v *branchFilterView) browseLines(m Model, textW int, s *styles) []string {
	parts := []string{i18n.T("Branch filters (alt+1…5 on the Branches / Remotes panel)"), ""}
	wr := make([]winRow, branchfilter.MaxSlots)
	for i := 0; i < branchfilter.MaxSlots; i++ {
		c := m.branchFilters[i]
		prefix := "  "
		var style lipgloss.Style
		if i == v.sel {
			prefix, style = "> ", s.selectedRow
		}
		text := prefix + strconv.Itoa(i+1) + "  "
		switch {
		case c.Err != nil:
			text += bfSummary(c)
		case c.Empty:
			// An undefined slot reads as a dash, not "empty (no clause set)":
			// that phrase is for a slot that IS defined but matches nothing.
			text += "—"
		default:
			text += padRight(bfLabel(c), 14) + bfSummary(c)
		}
		inGlobal, inRepo := config.BranchFilterScopes(config.DefaultGlobalPath(), m.repoConfigPath, i+1)
		switch {
		case inRepo && inGlobal:
			text += "  " + i18n.T("[this repo, overrides global]")
		case inRepo:
			text += "  " + i18n.T("[this repo]")
		case inGlobal:
			text += "  " + i18n.T("[global]")
		}
		wr[i] = winRow{text: text, style: style}
	}
	parts = append(parts, renderWindow(wr, winOpts{w: textW, h: branchfilter.MaxSlots, anchor: v.sel})...)
	for _, warn := range m.branchFilterWarnings {
		parts = append(parts, s.errorText.Render(i18n.T("config: %s", warn)))
	}
	hint := []string{i18n.T("[enter] edit"), i18n.T("[d] remove (repo definition first, then global)"), i18n.T("[esc] back")}
	return append(append(parts, ""), wrapParts(hint, textW, "  ")...)
}

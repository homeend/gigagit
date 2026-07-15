package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// identityView is the Settings sub-surface for the git user identity and the
// named app-profiles. It is a layer (pushed over the settings menu): browse the
// current identity + profiles, apply a profile or an edited identity to git's
// local/global scope, and create/rename/delete profiles. The single git write
// goes through engine.SetIdentity; profile CRUD goes through domain.
type identityView struct {
	popupMax
	loading  bool
	id       model.Identity
	profiles []model.Profile
	sel      int // index into profiles (browse mode)
	mode     idMode

	// edit-identity (user.name/user.email) and new/rename-profile form fields.
	fName  textfield // user.name (edit) or git name (profile)
	fEmail textfield // user.email (edit) or git email (profile)
	fLabel textfield // profile display name (new/rename only)
	field  int       // focused field index within the active form
	scope  model.ProfileScope

	// rename bookkeeping: the original id/scope being replaced ("" = create).
	renameFrom  string
	renameScope model.ProfileScope

	// apply sub-state: the values about to be written, and a label for display.
	applyName  string
	applyEmail string
	applyLabel string
}

type idMode int

const (
	idBrowse idMode = iota
	idEditIdentity
	idForm // create or rename a profile
	idApply
)

// applyOp is the pure seam from a chosen (name,email,scope) to the engine op,
// so the apply path is unit-testable without driving the TUI.
func applyOp(name, email string, global bool) engine.SetIdentity {
	return engine.SetIdentity{Name: name, Email: email, Global: global}
}

// openIdentityView pushes the surface (in a loading state) and kicks off the
// async read of the current identity + profiles.
func (m Model) openIdentityView() (Model, tea.Cmd) {
	m = m.pushLayer(&identityView{loading: true})
	return m, m.loadIdentityDataCmd()
}

// identityDataMsg carries the identity + profile list (or a load/mutate error)
// back to the open identityView.
type identityDataMsg struct {
	id       model.Identity
	profiles []model.Profile
	err      error
}

// loadIdentityDataCmd reads the identity and profiles off the UI thread.
func (m Model) loadIdentityDataCmd() tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		ctx := context.Background()
		id, err := svc.Identity(ctx)
		if err != nil {
			return identityDataMsg{err: err}
		}
		ps, perr := svc.Profiles(ctx)
		return identityDataMsg{id: id, profiles: ps, err: perr}
	}
}

// addProfileCmd writes a profile (optionally removing a renamed-from original)
// then re-reads the data, all off the UI thread.
func (m Model) addProfileCmd(p model.Profile, renameFrom string, renameScope model.ProfileScope) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		ctx := context.Background()
		added, err := svc.AddProfile(ctx, p)
		if err != nil {
			return identityDataMsg{err: err}
		}
		if renameFrom != "" && renameFrom != added.ID {
			_ = svc.RemoveProfile(ctx, renameScope, renameFrom)
		}
		id, _ := svc.Identity(ctx)
		ps, err := svc.Profiles(ctx)
		return identityDataMsg{id: id, profiles: ps, err: err}
	}
}

// removeProfileCmd deletes a profile then re-reads the data.
func (m Model) removeProfileCmd(scope model.ProfileScope, id string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		ctx := context.Background()
		_ = svc.RemoveProfile(ctx, scope, id)
		idv, _ := svc.Identity(ctx)
		ps, err := svc.Profiles(ctx)
		return identityDataMsg{id: idv, profiles: ps, err: err}
	}
}

// update handles all keys while the surface is open. It swallows everything
// (no fallthrough to global handlers).
func (v *identityView) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch v.mode {
	case idApply:
		return v.updateApply(m, msg)
	case idEditIdentity, idForm:
		return v.updateForm(m, msg)
	default:
		return v.updateBrowse(m, msg)
	}
}

func (v *identityView) updateBrowse(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m = m.popLayer()
		return m, nil
	case tea.KeyUp:
		if v.sel > 0 {
			v.sel--
		}
		return m, nil
	case tea.KeyDown:
		if v.sel < len(v.profiles)-1 {
			v.sel++
		}
		return m, nil
	case tea.KeyEnter:
		if v.sel < 0 || v.sel >= len(v.profiles) {
			return m, nil
		}
		p := v.profiles[v.sel]
		v.applyName, v.applyEmail, v.applyLabel = p.GitName, p.GitEmail, p.Name
		v.mode = idApply
		return m, nil
	}
	switch msg.String() {
	case "e": // edit the live identity
		v.fName = newTextField(v.id.EffectiveName)
		v.fEmail = newTextField(v.id.EffectiveEmail)
		v.field = 0
		v.mode = idEditIdentity
		return m, nil
	case "n": // new profile
		v.fLabel = newTextField("")
		v.fName = newTextField("")
		v.fEmail = newTextField("")
		v.field = 0
		v.scope = model.ProfileScopeGlobal
		v.renameFrom = ""
		v.mode = idForm
		return m, nil
	case "r": // rename/edit the selected profile
		if v.sel < 0 || v.sel >= len(v.profiles) {
			return m, nil
		}
		p := v.profiles[v.sel]
		v.fLabel = newTextField(p.Name)
		v.fName = newTextField(p.GitName)
		v.fEmail = newTextField(p.GitEmail)
		v.field = 0
		v.scope = p.Scope
		v.renameFrom = p.ID
		v.renameScope = p.Scope
		v.mode = idForm
		return m, nil
	case "d": // delete the selected profile
		if v.sel < 0 || v.sel >= len(v.profiles) {
			return m, nil
		}
		p := v.profiles[v.sel]
		if v.sel > 0 {
			v.sel--
		}
		return m, m.removeProfileCmd(p.Scope, p.ID)
	}
	return m, nil
}

// formFields returns the focusable fields of the active form: the edit-identity
// form has 2 text fields; the profile form has 3 text fields + a scope toggle.
func (v *identityView) formFieldCount() int {
	if v.mode == idForm {
		return 4 // label, git name, git email, scope
	}
	return 2 // user.name, user.email
}

func (v *identityView) updateForm(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	scopeField := v.mode == idForm && v.field == 3
	switch msg.Type {
	case tea.KeyEsc:
		v.mode = idBrowse
		return m, nil
	case tea.KeyUp:
		if v.field > 0 {
			v.field--
		}
		return m, nil
	case tea.KeyDown, tea.KeyTab:
		if v.field < v.formFieldCount()-1 {
			v.field++
		}
		return m, nil
	case tea.KeyEnter:
		return v.submitForm(m)
	}
	if scopeField {
		switch msg.String() {
		case "left", "right", " ", "h", "l":
			if v.scope == model.ProfileScopeGlobal {
				v.scope = model.ProfileScopeRepo
			} else {
				v.scope = model.ProfileScopeGlobal
			}
		}
		return m, nil
	}
	// Delegate editing to the focused text field.
	if f := v.focusedField(); f != nil {
		f.HandleEditKey(msg)
	}
	return m, nil
}

// focusedField returns a pointer to the currently focused text field, or nil
// for the scope toggle.
func (v *identityView) focusedField() *textfield {
	if v.mode == idEditIdentity {
		if v.field == 0 {
			return &v.fName
		}
		return &v.fEmail
	}
	switch v.field {
	case 0:
		return &v.fLabel
	case 1:
		return &v.fName
	case 2:
		return &v.fEmail
	}
	return nil
}

func (v *identityView) submitForm(m Model) (Model, tea.Cmd) {
	if v.mode == idEditIdentity {
		name, email := strings.TrimSpace(v.fName.Value()), strings.TrimSpace(v.fEmail.Value())
		if name == "" || email == "" {
			m.statusMsg = i18n.T("name and email are required")
			return m, nil
		}
		v.applyName, v.applyEmail, v.applyLabel = name, email, i18n.T("edited identity")
		v.mode = idApply
		return m, nil
	}
	// profile form
	label := strings.TrimSpace(v.fLabel.Value())
	name := strings.TrimSpace(v.fName.Value())
	email := strings.TrimSpace(v.fEmail.Value())
	if label == "" || name == "" || email == "" {
		m.statusMsg = i18n.T("profile name, git name and email are required")
		return m, nil
	}
	p := model.Profile{Name: label, GitName: name, GitEmail: email, Scope: v.scope}
	cmd := m.addProfileCmd(p, v.renameFrom, v.renameScope)
	v.mode = idBrowse
	return m, cmd
}

func (v *identityView) updateApply(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		v.mode = idBrowse
		return m, nil
	}
	switch msg.String() {
	case "r", "g":
		global := msg.String() == "g"
		op := applyOp(v.applyName, v.applyEmail, global)
		m = m.clearLayers()
		return m.startOp(op)
	}
	return m, nil
}

// render composites the surface box over the layer beneath.
func (v *identityView) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), v.box(m), w, h)
}

func (v *identityView) box(m Model) string {
	// Wide popup (up to 96 cols) + a wrapped footer: the action hints are longer
	// than the standard 56-col prose popup, so popupInnerWidth would truncate
	// them ("actions cut off"). Mirrors the bookmark/shelf switchers.
	w, _ := m.overlayDims()
	inner := popupResolveWidth(w, v.maximized, popupWideInnerWidth(w))
	textW := popupTextWidth(inner)

	var body, footer []string
	switch v.mode {
	case idEditIdentity:
		body, footer = v.editLines(textW)
	case idForm:
		body, footer = v.formLines(textW)
	case idApply:
		body, footer = v.applyLines()
	default:
		body, footer = v.browseLines(m, textW)
	}
	parts := append(body, "")
	parts = append(parts, wrapParts(footer, textW, "  ")...)
	return popupBox(inner, strings.Join(parts, "\n"))
}

func identityLine(label, name, email string, set bool, inheritNote string) string {
	val := i18n.T("(not set)")
	if inheritNote != "" {
		val = inheritNote
	}
	if set {
		val = fmt.Sprintf("%s <%s>", name, email)
	}
	return "  " + padCell(label, 9) + " " + val
}

func (v *identityView) browseLines(m Model, textW int) (body, footer []string) {
	parts := []string{i18n.T("Identity & profiles"), ""}
	if v.loading {
		return append(parts, "  "+i18n.T("(loading…)")), []string{i18n.T("[esc]")}
	}
	parts = append(parts, i18n.T("Current identity"))
	parts = append(parts, identityLine(i18n.T("Global"), v.id.GlobalName, v.id.GlobalEmail, v.id.GlobalSet, ""))
	repoNote := ""
	if !v.id.LocalSet && v.id.GlobalSet {
		repoNote = i18n.T("(not set — inherits global)")
	}
	parts = append(parts, identityLine(i18n.T("Repo"), v.id.LocalName, v.id.LocalEmail, v.id.LocalSet, repoNote))
	effSet := v.id.EffectiveName != "" || v.id.EffectiveEmail != ""
	parts = append(parts, identityLine(i18n.T("Effective"), v.id.EffectiveName, v.id.EffectiveEmail, effSet, ""))
	parts = append(parts, "", i18n.T("Profiles"))
	if len(v.profiles) == 0 {
		parts = append(parts, "  "+i18n.T("(none yet — [n] to create)"))
	} else {
		wr := make([]winRow, len(v.profiles))
		for i, p := range v.profiles {
			prefix := "  "
			var st lipgloss.Style
			if i == v.sel {
				prefix, st = "> ", selectedRow
			}
			row := fmt.Sprintf("%s%s — %s <%s>  %s", prefix, p.Name, p.GitName, p.GitEmail, profileScopeTag(p.Scope))
			wr[i] = winRow{text: row, style: st}
		}
		h := len(v.profiles)
		_, termH := m.overlayDims()
		capRows := popupResolveRowCap(v.maximized, termH, 8)
		if h > capRows {
			h = capRows
		}
		parts = append(parts, renderWindow(wr, winOpts{w: textW, h: h, anchor: v.sel})...)
	}
	footer = []string{i18n.T("[enter] apply"), i18n.T("[e] edit identity"), i18n.T("[n] new"), i18n.T("[r] rename"), i18n.T("[d] delete"), i18n.T("[esc]")}
	return parts, footer
}

// profileScopeTag translates the compact scope tag shown next to a profile
// row in the browse list. p.Scope stays the protocol enum throughout — only
// this render-time switch (the agentStatusDisplay pattern) turns it into
// display text.
func profileScopeTag(s model.ProfileScope) string {
	switch s {
	case model.ProfileScopeRepo:
		return i18n.T("[this repo]")
	default:
		return i18n.T("[global]")
	}
}

// profileScopeLabel translates the scope value shown in the profile
// create/rename form's own Scope field.
func profileScopeLabel(s model.ProfileScope) string {
	switch s {
	case model.ProfileScopeRepo:
		return i18n.T("this repo only")
	default:
		return i18n.T("global (every repo)")
	}
}

func (v *identityView) fieldLine(label string, f textfield, focused bool, contentWidth int) string {
	cursor := "  "
	if focused {
		cursor = "> "
	}
	return viewField(cursor+padCell(label, 10)+" ", f, focused, contentWidth)
}

func (v *identityView) editLines(textW int) (body, footer []string) {
	return []string{
		i18n.T("Edit identity"), "",
		v.fieldLine(i18n.T("Name"), v.fName, v.field == 0, textW),
		v.fieldLine(i18n.T("Email"), v.fEmail, v.field == 1, textW),
	}, []string{i18n.T("[↑/↓] field"), i18n.T("[enter] choose scope"), i18n.T("[esc] back")}
}

func (v *identityView) formLines(textW int) (body, footer []string) {
	title := i18n.T("New profile")
	if v.renameFrom != "" {
		title = i18n.T("Edit profile")
	}
	scopeCursor := "  "
	if v.field == 3 {
		scopeCursor = "> "
	}
	scopeVal := profileScopeLabel(v.scope)
	return []string{
		title, "",
		v.fieldLine(i18n.T("Name"), v.fLabel, v.field == 0, textW),
		v.fieldLine(i18n.T("Git name"), v.fName, v.field == 1, textW),
		v.fieldLine(i18n.T("Git email"), v.fEmail, v.field == 2, textW),
		scopeCursor + padCell(i18n.T("Scope"), 10) + " " + scopeVal,
	}, []string{i18n.T("[↑/↓] field"), i18n.T("[←/→] scope"), i18n.T("[enter] save"), i18n.T("[esc] back")}
}

func (v *identityView) applyLines() (body, footer []string) {
	return []string{
		i18n.T("Apply identity"), "",
		fmt.Sprintf("  %s <%s>", v.applyName, v.applyEmail),
		i18n.T("  from: %s", v.applyLabel),
		"", i18n.T("Apply to:"),
	}, []string{i18n.T("[r] this repo"), i18n.T("[g] globally"), i18n.T("[esc] back")}
}

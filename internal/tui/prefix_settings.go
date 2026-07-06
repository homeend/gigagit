package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/model"
)

// prefixSettingsView is the Settings sub-surface that manages branch prefixes
// (browse + add/edit + delete, Global|Repo). Mirrors identityView's structure.
type prefixSettingsView struct {
	popupMax
	loading bool
	items   []model.Prefix
	sel     int
	mode    pfMode

	fValue textfield
	scope  model.ProfileScope
	field  int // 0 = value, 1 = scope
}

type pfMode int

const (
	pfBrowse pfMode = iota
	pfForm
)

type prefixDataMsg struct {
	items []model.Prefix
	err   error
}

func (m Model) openPrefixSettings() (Model, tea.Cmd) {
	m = m.pushLayer(&prefixSettingsView{loading: true})
	return m, m.loadPrefixDataCmd()
}

func (m Model) loadPrefixDataCmd() tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		ps, err := svc.Prefixes(context.Background())
		return prefixDataMsg{items: ps, err: err}
	}
}

func (m Model) addPrefixCmd(p model.Prefix) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		if _, err := svc.AddPrefix(context.Background(), p); err != nil {
			return prefixDataMsg{err: err}
		}
		ps, err := svc.Prefixes(context.Background())
		return prefixDataMsg{items: ps, err: err}
	}
}

func (m Model) removePrefixCmd(scope model.ProfileScope, id string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		_ = svc.RemovePrefix(context.Background(), scope, id)
		ps, err := svc.Prefixes(context.Background())
		return prefixDataMsg{items: ps, err: err}
	}
}

func (v *prefixSettingsView) deleteTarget() (id string, scope model.ProfileScope, ok bool) {
	if v.sel < 0 || v.sel >= len(v.items) {
		return "", 0, false
	}
	p := v.items[v.sel]
	return p.ID, p.Scope, true
}

func (v *prefixSettingsView) formPrefix() (model.Prefix, bool) {
	val := strings.TrimSpace(v.fValue.Value())
	if val == "" {
		return model.Prefix{}, false
	}
	return model.Prefix{Value: val, Scope: v.scope}, true
}

func (v *prefixSettingsView) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	if v.mode == pfForm {
		return v.updateForm(m, msg)
	}
	return v.updateBrowse(m, msg)
}

func (v *prefixSettingsView) updateBrowse(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyUp:
		if v.sel > 0 {
			v.sel--
		}
		return m, nil
	case tea.KeyDown:
		if v.sel < len(v.items)-1 {
			v.sel++
		}
		return m, nil
	}
	switch msg.String() {
	case "n", "a":
		v.fValue = newTextField("")
		v.scope = model.ProfileScopeGlobal
		v.field = 0
		v.mode = pfForm
		return m, nil
	case "d":
		id, scope, ok := v.deleteTarget()
		if !ok {
			return m, nil
		}
		if v.sel > 0 {
			v.sel--
		}
		return m, m.removePrefixCmd(scope, id)
	}
	return m, nil
}

func (v *prefixSettingsView) updateForm(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		v.mode = pfBrowse
		return m, nil
	case tea.KeyUp:
		if v.field > 0 {
			v.field--
		}
		return m, nil
	case tea.KeyDown, tea.KeyTab:
		if v.field < 1 {
			v.field++
		}
		return m, nil
	case tea.KeyEnter:
		p, ok := v.formPrefix()
		if !ok {
			m.statusMsg = "prefix value is required"
			return m, nil
		}
		v.mode = pfBrowse
		return m, m.addPrefixCmd(p)
	}
	if v.field == 1 { // scope toggle
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
	v.fValue.HandleEditKey(msg)
	return m, nil
}

func (v *prefixSettingsView) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), v.box(m), w, h)
}

func (v *prefixSettingsView) box(m Model) string {
	w, termH := m.overlayDims()
	inner := popupResolveWidth(w, v.maximized, popupWideInnerWidth(w))
	textW := popupTextWidth(inner)

	if v.mode == pfForm {
		scopeCursor := "  "
		if v.field == 1 {
			scopeCursor = "> "
		}
		scopeVal := "global (every repo)"
		if v.scope == model.ProfileScopeRepo {
			scopeVal = "this repo only"
		}
		cur := "  "
		if v.field == 0 {
			cur = "> "
		}
		parts := []string{
			"Add branch prefix", "",
			viewField(cur+"value: ", v.fValue, v.field == 0, textW),
			scopeCursor + "scope: " + scopeVal,
			"",
			"Tokens: <user:LABEL> <seq:NAME:N> <date:FMT> <parent-branch> <repo> <random-*>",
			"",
			"[↑/↓] field  [←/→] scope  [enter] save  [esc] back",
		}
		return popupBox(inner, strings.Join(parts, "\n"))
	}

	parts := []string{"Branch prefixes", ""}
	if v.loading {
		parts = append(parts, "  (loading…)")
		return popupBox(inner, strings.Join(parts, "\n"))
	}
	if len(v.items) == 0 {
		parts = append(parts, "  (none yet — [n] to add)")
	} else {
		wr := make([]winRow, len(v.items))
		for i, p := range v.items {
			prefix := "  "
			var st lipgloss.Style
			if i == v.sel {
				prefix, st = "> ", selectedRow
			}
			tag := "[global]"
			if p.Scope == model.ProfileScopeRepo {
				tag = "[this repo]"
			}
			wr[i] = winRow{text: prefix + p.Value + "  " + tag, style: st}
		}
		h := len(v.items)
		capRows := popupResolveRowCap(v.maximized, termH, 10)
		if h > capRows {
			h = capRows
		}
		parts = append(parts, renderWindow(wr, winOpts{w: textW, h: h, anchor: v.sel})...)
	}
	parts = append(parts, "", "[n] add  [d] delete  [esc] back")
	return popupBox(inner, strings.Join(parts, "\n"))
}

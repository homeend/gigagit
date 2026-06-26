package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/model"
)

// prefixPicker is a select-only quick-switcher of branch-name prefixes (global
// + repo). On select it runs the templateFill step (if the prefix has <user:>
// labels) then hands the resolved string to onPick. resolve returns the
// resolved string plus the prefix's <seq> names so the opener can bump them.
type prefixPicker struct {
	items     []model.Prefix
	rows      []string // display values, parallel to items
	sel       int
	filter    string
	filtering bool

	resolve func(value string, inputs map[string]string) (string, []string, error)
	onPick  func(m Model, resolved string, seqNames []string) (Model, tea.Cmd)

	fill      *templateFill // non-nil while collecting labels
	fillValue string        // the prefix value being filled
}

type prefixesLoadedMsg struct {
	items   []model.Prefix
	resolve func(value string, inputs map[string]string) (string, []string, error)
	onPick  func(m Model, resolved string, seqNames []string) (Model, tea.Cmd)
	err     error
}

// openPrefixPicker loads prefixes off-thread; the resulting prefixesLoadedMsg
// (handled in Update) pushes the picker. The opener supplies resolve+onPick.
func (m Model) openPrefixPicker(
	resolve func(string, map[string]string) (string, []string, error),
	onPick func(Model, string, []string) (Model, tea.Cmd),
) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		ps, err := svc.Prefixes(context.Background())
		return prefixesLoadedMsg{items: ps, resolve: resolve, onPick: onPick, err: err}
	}
}

func newPrefixPicker(msg prefixesLoadedMsg) *prefixPicker {
	p := &prefixPicker{items: msg.items, resolve: msg.resolve, onPick: msg.onPick}
	for _, it := range msg.items {
		p.rows = append(p.rows, it.Value)
	}
	return p
}

func (p *prefixPicker) visibleIdx() []int {
	var idx []int
	q := strings.ToLower(p.filter)
	for i, row := range p.rows {
		if q == "" || strings.Contains(strings.ToLower(row), q) {
			idx = append(idx, i)
		}
	}
	return idx
}

func (p *prefixPicker) selected() (model.Prefix, bool) {
	vis := p.visibleIdx()
	if p.sel < 0 || p.sel >= len(vis) {
		return model.Prefix{}, false
	}
	return p.items[vis[p.sel]], true
}

func (p *prefixPicker) moveSel(d int) {
	n := p.sel + d
	if hi := len(p.visibleIdx()) - 1; n > hi {
		n = hi
	}
	if n < 0 {
		n = 0
	}
	p.sel = n
}

func (p *prefixPicker) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	// Fill sub-mode: collect <user> labels, then resolve + hand off.
	if p.fill != nil {
		done, cancel := p.fill.handleKey(msg)
		if cancel {
			p.fill = nil
			return m, nil
		}
		if done {
			return p.finish(m, p.fillValue, p.fill.inputs())
		}
		return m, nil
	}
	if p.filtering {
		switch msg.Type {
		case tea.KeyEsc:
			p.filtering, p.filter, p.sel = false, "", 0
		case tea.KeyEnter:
			p.filtering = false
		case tea.KeyBackspace, tea.KeyCtrlH:
			if r := []rune(p.filter); len(r) > 0 {
				p.filter, p.sel = string(r[:len(r)-1]), 0
			}
		case tea.KeyRunes:
			p.filter += string(msg.Runes)
			p.sel = 0
		}
		return m, nil
	}
	switch msg.Type {
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyUp:
		p.moveSel(-1)
	case tea.KeyDown:
		p.moveSel(1)
	case tea.KeyEnter:
		it, ok := p.selected()
		if !ok {
			return m, nil
		}
		f := newTemplateFill(it.Value)
		if f.needsInput() {
			p.fill, p.fillValue = &f, it.Value
			return m, nil
		}
		return p.finish(m, it.Value, map[string]string{})
	case tea.KeyRunes:
		switch msg.String() {
		case "/":
			p.filtering = true
		case "k":
			p.moveSel(-1)
		case "j":
			p.moveSel(1)
		}
	}
	return m, nil
}

// finish resolves the prefix and calls onPick with the resolved string + the
// prefix's <seq> names.
func (p *prefixPicker) finish(m Model, value string, inputs map[string]string) (Model, tea.Cmd) {
	resolved, seqNames, err := p.resolve(value, inputs)
	if err != nil {
		m.statusMsg = "prefix: " + err.Error()
		return m.popLayer(), nil
	}
	return p.onPick(m, resolved, seqNames)
}

func (p *prefixPicker) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

func (p *prefixPicker) box(m Model) string {
	w, _ := m.overlayDims()
	inner := popupWideInnerWidth(w)
	textW := popupTextWidth(inner)

	if p.fill != nil {
		parts := []string{"Fill " + p.fillValue, ""}
		parts = append(parts, p.fill.view(textW)...)
		parts = append(parts, "", "[tab] next  [enter] done  [esc] back")
		return popupBox(inner, strings.Join(parts, "\n"))
	}

	header := "Branch prefixes"
	if p.filtering {
		header += "  /" + p.filter + "█"
	} else if p.filter != "" {
		header += "  /" + p.filter
	}
	vis := p.visibleIdx()
	var body []string
	if len(vis) == 0 {
		body = []string{padRight("  (none — add in Settings → Branch prefixes)", textW)}
	} else {
		wr := make([]winRow, len(vis))
		for n, i := range vis {
			prefix := "  "
			var st lipgloss.Style
			if n == p.sel {
				prefix, st = "> ", selectedRow
			}
			tag := "[global]"
			if p.items[i].Scope == model.ProfileScopeRepo {
				tag = "[this repo]"
			}
			wr[n] = winRow{text: prefix + p.rows[i] + "  " + tag, style: st}
		}
		h := len(vis)
		if h > 12 {
			h = 12
		}
		body = renderWindow(wr, winOpts{w: textW, h: h, anchor: p.sel})
	}
	parts := append([]string{header, ""}, body...)
	parts = append(parts, "", "[enter] use  [/] filter  [esc] cancel")
	return popupBox(inner, strings.Join(parts, "\n"))
}

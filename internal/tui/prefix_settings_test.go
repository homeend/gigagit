package tui

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/model"
)

// 'e' (in-place edit) is intentionally not offered — editing is remove + re-add
// (spec out-of-scope). A stray 'e' must not open the form (which would, on a
// changed value/scope, create an orphan duplicate).
func TestPrefixSettingsNoInPlaceEdit(t *testing.T) {
	v := &prefixSettingsView{
		items: []model.Prefix{{ID: "feat", Value: "feat/", Scope: model.ProfileScopeRepo}},
		mode:  pfBrowse,
	}
	v.update(Model{}, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	if v.mode != pfBrowse {
		t.Fatalf("'e' opened mode %v; in-place edit must not exist", v.mode)
	}
}

func TestPrefixSettingsFormBuildsValidEntry(t *testing.T) {
	v := &prefixSettingsView{mode: pfForm}
	v.fValue = newTextField("feat/")
	v.scope = model.ProfileScopeGlobal
	p, ok := v.formPrefix()
	if !ok {
		t.Fatal("want ok")
	}
	if p.Value != "feat/" || p.Scope != model.ProfileScopeGlobal {
		t.Fatalf("p = %+v", p)
	}
}

func TestPrefixSettingsEmptyValueRejected(t *testing.T) {
	v := &prefixSettingsView{mode: pfForm}
	v.fValue = newTextField("   ")
	if _, ok := v.formPrefix(); ok {
		t.Fatal("blank value should not build a prefix")
	}
}

func TestPrefixSettingsDeleteTarget(t *testing.T) {
	v := &prefixSettingsView{
		items: []model.Prefix{{ID: "feat", Value: "feat/", Scope: model.ProfileScopeRepo}},
		mode:  pfBrowse,
	}
	id, scope, ok := v.deleteTarget()
	if !ok || id != "feat" || scope != model.ProfileScopeRepo {
		t.Fatalf("id=%q scope=%v ok=%v", id, scope, ok)
	}
}

func TestPrefixSettingsMaximizeWidensAndLiftsRowCap(t *testing.T) {
	m := Model{}
	m.width, m.height = 200, 50
	var items []model.Prefix
	for i := 0; i < 20; i++ { // more than the fixed cap of 10
		items = append(items, model.Prefix{ID: fmt.Sprintf("p%d", i), Value: fmt.Sprintf("feat/%d-", i)})
	}
	v := &prefixSettingsView{items: items, mode: pfBrowse}

	normal := v.box(m)
	v.maximized = true
	maxed := v.box(m)

	if lipgloss.Width(maxed) <= lipgloss.Width(normal) {
		t.Fatalf("maximized width %d must exceed normal %d", lipgloss.Width(maxed), lipgloss.Width(normal))
	}
	if lipgloss.Height(maxed) <= lipgloss.Height(normal) {
		t.Fatalf("maximized must show more rows: height %d vs %d", lipgloss.Height(maxed), lipgloss.Height(normal))
	}
}

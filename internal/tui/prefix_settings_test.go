package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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

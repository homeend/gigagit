package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

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

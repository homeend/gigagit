package tui

import (
	"fmt"
	"strings"
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

// An invalid value must NOT close the form or dispatch the add — the error
// shows inline and the typed value survives so the user can fix it.
func TestPrefixSettingsInvalidValueKeepsFormOpen(t *testing.T) {
	v := &prefixSettingsView{mode: pfForm}
	v.fValue = newTextField("x-<bogus:1>-y")
	_, cmd := v.update(Model{}, tea.KeyMsg{Type: tea.KeyEnter})
	if v.mode != pfForm {
		t.Fatal("invalid value must keep the form open")
	}
	if cmd != nil {
		t.Fatal("invalid value must not dispatch an add")
	}
	if !strings.Contains(v.formErr, "<bogus:1>") {
		t.Fatalf("formErr = %q, want it to name the bad token", v.formErr)
	}
	if v.fValue.Value() != "x-<bogus:1>-y" {
		t.Fatal("typed value must be preserved")
	}
}

// The empty-value message moves inline too (it used to be a bottom-bar
// statusMsg).
func TestPrefixSettingsEmptyValueInlineError(t *testing.T) {
	v := &prefixSettingsView{mode: pfForm}
	v.fValue = newTextField("   ")
	m2, cmd := v.update(Model{}, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || v.mode != pfForm {
		t.Fatal("empty value must keep the form open without dispatching")
	}
	if v.formErr != "prefix value is required" {
		t.Fatalf("formErr = %q", v.formErr)
	}
	if m2.statusMsg != "" {
		t.Fatalf("statusMsg = %q, want the error inline instead", m2.statusMsg)
	}
}

func TestPrefixSettingsValidValueClosesFormAndDispatches(t *testing.T) {
	v := &prefixSettingsView{mode: pfForm}
	v.fValue = newTextField("feat/<date>") // bare <date> is valid since Task 1
	_, cmd := v.update(Model{}, tea.KeyMsg{Type: tea.KeyEnter})
	if v.mode != pfBrowse {
		t.Fatal("valid value must close the form")
	}
	if cmd == nil {
		t.Fatal("valid value must dispatch the add")
	}
	if v.formErr != "" {
		t.Fatalf("formErr = %q, want empty", v.formErr)
	}
}

// Reopening the form must not show a stale error from the previous attempt.
func TestPrefixSettingsReopenClearsInlineError(t *testing.T) {
	v := &prefixSettingsView{mode: pfBrowse, formErr: "stale"}
	v.update(Model{}, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if v.formErr != "" {
		t.Fatalf("formErr = %q, want cleared on open", v.formErr)
	}
}

func TestPrefixSettingsFormRendersInlineError(t *testing.T) {
	m := Model{}
	m.width, m.height = 120, 40
	v := &prefixSettingsView{mode: pfForm, formErr: "invalid prefix: template: unknown token <bogus>"}
	v.fValue = newTextField("<bogus>")
	if !strings.Contains(v.box(m), "unknown token <bogus>") {
		t.Fatal("form box must render the inline error line")
	}
}

// ctrl+d works even while typing (the textfield doesn't consume it) and must
// not disturb the form's state.
func TestPrefixSettingsCtrlDOpensFormatHelp(t *testing.T) {
	v := &prefixSettingsView{mode: pfForm}
	v.fValue = newTextField("feat/")
	m2, _ := v.update(Model{}, tea.KeyMsg{Type: tea.KeyCtrlD})
	if layerOf[*contentPopup](m2) == nil {
		t.Fatal("ctrl+d must push the token/date-format help sheet")
	}
	if v.mode != pfForm || v.fValue.Value() != "feat/" {
		t.Fatal("form state must survive opening the help sheet")
	}
}

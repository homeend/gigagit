package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/model"
)

func sampleIdentityView() *identityView {
	return &identityView{
		id: model.Identity{
			GlobalName: "Glob", GlobalEmail: "g@x", GlobalSet: true,
			LocalSet:      false,
			EffectiveName: "Glob", EffectiveEmail: "g@x",
		},
		profiles: []model.Profile{
			{Name: "Work", GitName: "W", GitEmail: "w@x", Scope: model.ProfileScopeGlobal},
			{Name: "OSS", GitName: "O", GitEmail: "o@x", Scope: model.ProfileScopeRepo},
		},
	}
}

func TestIdentityViewRendersCurrentAndProfiles(t *testing.T) {
	m := Model{width: 100, height: 40}
	v := sampleIdentityView()
	out := v.box(m)
	for _, want := range []string{"Glob", "g@x", "Work", "OSS", "global", "this repo"} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered output missing %q:\n%s", want, out)
		}
	}
}

// Regression: the action hints were truncated because the popup capped at 56
// cols and popupBox truncates (never wraps). They must all render now.
func TestIdentityViewRendersAllActions(t *testing.T) {
	for _, w := range []int{120, 60} { // wide (one line) and narrow (wraps)
		m := Model{width: w, height: 40}
		out := sampleIdentityView().box(m)
		for _, want := range []string{"[enter] apply", "[e] edit identity", "[n] new", "[r] rename", "[d] delete", "[esc]"} {
			if !strings.Contains(out, want) {
				t.Fatalf("width %d: action %q missing (truncated?):\n%s", w, want, out)
			}
		}
	}
}

func TestIdentityViewRendersUnsetLocalDistinctly(t *testing.T) {
	m := Model{width: 100, height: 40}
	v := sampleIdentityView() // LocalSet false, GlobalSet true
	out := v.box(m)
	// The repo line must read as "not set"/"inherits", never echo a fake local value.
	if !strings.Contains(strings.ToLower(out), "not set") && !strings.Contains(strings.ToLower(out), "inherit") {
		t.Fatalf("unset local should render as not-set/inherits:\n%s", out)
	}
}

// applyOp is the pure seam translating a (name,email,global) choice into the
// engine op, so the apply path is unit-testable without driving the TUI.
func TestApplyOpBuildsSetIdentity(t *testing.T) {
	op := applyOp("Ada", "ada@x", true)
	want := engine.SetIdentity{Name: "Ada", Email: "ada@x", Global: true}
	if op != want {
		t.Fatalf("applyOp = %+v, want %+v", op, want)
	}
}

// Applying to "this repo" (local) writes the temp repo's own .git/config — no
// global isolation needed since it never touches ~/.gitconfig.
func TestApplyWritesLocalIdentity(t *testing.T) {
	_, repo := newRepoDir(t)
	m := New(domain.New(repo))
	m.width, m.height = 100, 40
	m = m.pushLayer(&identityView{mode: idApply, applyName: "Apply Me", applyEmail: "apply@me"})

	updated, cmd := m.Update(keyMsg("r")) // [r] this repo
	m = driveOp(t, updated.(Model), cmd)

	id, err := m.svc.Identity(context.Background())
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	if !id.LocalSet || id.LocalName != "Apply Me" || id.LocalEmail != "apply@me" {
		t.Fatalf("local identity = %q <%q> set=%v", id.LocalName, id.LocalEmail, id.LocalSet)
	}
}

func TestIdentityViewSwallowsGlobalKeys(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 100, 40
	m = m.pushLayer(&identityView{})

	updated, _ := m.Update(keyMsg("p")) // p = global pull; must not leak
	m = updated.(Model)
	if m.running {
		t.Fatal("a global key leaked through and started an op")
	}
	if layerOf[*identityView](m) == nil {
		t.Fatal("popup should still be open")
	}
}

func TestIdentityEscReturnsToSettings(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 100, 40
	m = m.pushLayer(&settingsPopup{})
	mm, _ := m.openIdentityView()
	m = mm
	if layerOf[*identityView](m) == nil {
		t.Fatal("identity view not open")
	}
	updated, _ := m.Update(keyMsg("esc"))
	m = updated.(Model)
	if layerOf[*identityView](m) != nil {
		t.Fatal("esc should close the identity view")
	}
	if layerOf[*settingsPopup](m) == nil {
		t.Fatal("esc should reveal the settings menu beneath")
	}
}

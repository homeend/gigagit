package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
)

// versionsGateService builds a Service whose meta-marker probe reports the
// versions store at markerFormat (0 = no marker at all, i.e. a store this
// build can read). A marker ahead of domain.VersionsFormat makes the versions
// feature Unsatisfiable, which is the state the spec says must leave the
// feature's TUI entry points INERT.
func versionsGateService(markerFormat int) *domain.Service {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git version", gitexec.Result{Stdout: "git version 2.45.0\n"})
	out := ""
	if markerFormat > 0 {
		out = git.MetaRef(domain.StoreVersions, markerFormat) + "\x00deadbeef\x00\n"
	}
	f.SetResponse("git for-each-ref (gg)", gitexec.Result{Stdout: out})
	return domain.New(&git.Repo{Runner: f})
}

// TestBranchVersionsRowAbsentWhenTheFeatureIsDisabled is the inertness guard.
// A disabled feature that still offers "Previous versions…" and then prints
// ErrFeatureDisabled inside the popup is exactly the behaviour the spec's
// frontend table rules out ("feature's keys inert"), and what the web
// frontend already avoids by hiding its entry points.
func TestBranchVersionsRowAbsentWhenTheFeatureIsDisabled(t *testing.T) {
	t.Parallel()

	newModel := func(markerFormat int) Model {
		m := branchMergeModel()
		m.svc = versionsGateService(markerFormat)
		return m
	}

	// Control: a readable store keeps the row, so the assertion below is about
	// the gate and not about some unrelated precondition.
	if _, ok := newModel(0).branchVersionsRow(); !ok {
		t.Fatal("an enabled versions feature must still offer the branch-versions row")
	}

	if row, ok := newModel(domain.VersionsFormat + 1).branchVersionsRow(); ok {
		t.Errorf("a disabled versions feature still offers the menu row %q", row.label)
	}
}

// TestVersionsPaletteEntryAbsentWhenTheFeatureIsDisabled covers the second
// entry point. The registry itself stays static (paletteCommands has no
// Service); the filtering happens when a palette is opened.
func TestVersionsPaletteEntryAbsentWhenTheFeatureIsDisabled(t *testing.T) {
	t.Parallel()

	has := func(cmds []paletteCommand) bool {
		for _, c := range cmds {
			if c.feature == domain.FeatureVersions {
				return true
			}
		}
		return false
	}

	if !has(paletteCommands()) {
		t.Fatal("no palette entry declares the versions feature — the filter has gone blind")
	}

	m := branchMergeModel()
	m.svc = versionsGateService(0)
	if !has(m.availablePaletteCommands()) {
		t.Error("an enabled versions feature must keep its palette entry")
	}

	m.svc = versionsGateService(domain.VersionsFormat + 1)
	if has(m.availablePaletteCommands()) {
		t.Error("a disabled versions feature still offers its palette entry")
	}
}

// TestOpenVersionsIsInertWhenTheFeatureIsDisabled proves the openers
// themselves are no-ops, so a future caller that bypasses the menu/palette
// cannot open a popup that can only fail.
func TestOpenVersionsIsInertWhenTheFeatureIsDisabled(t *testing.T) {
	t.Parallel()
	m := branchMergeModel()
	m.svc = versionsGateService(domain.VersionsFormat + 1)

	if got, cmd := m.openBranchVersions("main", false, false); cmd != nil || layerOf[*versionsPopup](got) != nil {
		t.Error("openBranchVersions must be inert when the feature is disabled")
	}
	if got, cmd := m.openVersionBranchList(); cmd != nil || layerOf[*versionsPopup](got) != nil {
		t.Error("openVersionBranchList must be inert when the feature is disabled")
	}
}

// TestRenderFeatureDisabledIsLocalized proves an ErrFeatureDisabled that does
// reach a TUI surface renders through i18n.T — both the wrapper sentence and
// the preflight reason inside it — rather than the English Error() string the
// CLI prints by design. NOT parallel: it switches the process-global active
// language.
func TestRenderFeatureDisabledIsLocalized(t *testing.T) {
	withXXLanguage(t, map[string]string{
		"the %s feature is unavailable in this repository: %s": "XX-unavailable %s: %s",
		"the %s store is at format %d; this build needs %d-%d": "XX-format %s %d %d %d",
	})

	err := &domain.ErrFeatureDisabled{
		ID:           domain.FeatureVersions,
		Reason:       "the versions store is at format 2; this build needs 1-1",
		ReasonFormat: "the %s store is at format %d; this build needs %d-%d",
		ReasonArgs:   []any{"versions", 2, 1, 1},
	}
	got := renderFeatureDisabled(err)
	if want := "XX-unavailable versions: XX-format versions 2 1 1"; got != want {
		t.Errorf("renderFeatureDisabled = %q, want %q", got, want)
	}
	if got := renderLoadError(err); !strings.Contains(got, "XX-unavailable") {
		t.Errorf("renderLoadError = %q, want the localized feature-disabled prose", got)
	}
}

package domain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/preflight"
)

const legacyPreviewsTOML = `[[previews]]
id = "abcd1234"
source = "feat/x"
target = "main"
label = "login work"
created = 2026-09-01T10:00:00Z
`

// seedLegacyPreviews points svc at its own state dir and writes the
// superseded previews.toml into it. Per-Service, so these tests stay parallel.
func seedLegacyPreviews(t *testing.T, svc *Service, body string) string {
	t.Helper()
	dir := t.TempDir()
	svc.UseSavedCompareDir(dir)
	if body != "" {
		if err := os.WriteFile(filepath.Join(dir, "previews.toml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// RunAutoMigrations converts a legacy previews file WITHOUT asking, because
// the conversion is lossless. Afterwards the legacy file is gone and the
// converted entry is in the new store under its ORIGINAL id.
func TestRunAutoMigrationsConvertsPreviewsWithoutConsent(t *testing.T) {
	t.Parallel()
	_, svc := previewRepo(t)
	dir := seedLegacyPreviews(t, svc, legacyPreviewsTOML)
	ctx := context.Background()

	if err := svc.RunAutoMigrations(ctx); err != nil {
		t.Fatalf("RunAutoMigrations: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "previews.toml")); !os.IsNotExist(err) {
		t.Fatalf("previews.toml survived the conversion (stat err = %v)", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "savedcompare.toml"))
	if err != nil {
		t.Fatalf("savedcompare.toml: %v", err)
	}
	// TARGET FIRST, and the id carried over verbatim — the two things the
	// conversion promises. Asserted on the FILE so this sees the same bytes a
	// second gg process would.
	for _, want := range []string{"main...feat/x", `id = 'abcd1234'`, "login work"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("savedcompare.toml is missing %q:\n%s", want, data)
		}
	}
	if strings.Contains(string(data), "feat/x...main") {
		t.Fatalf("the converted pair is REVERSED:\n%s", data)
	}
}

// A LOSSLESS migration never appears in PendingMigrations: that list feeds
// the three consent screens, and asking about a conversion that loses nothing
// is a prompt with no decision in it.
func TestPendingMigrationsExcludesLosslessOnes(t *testing.T) {
	t.Parallel()
	_, svc := previewRepo(t)
	seedLegacyPreviews(t, svc, legacyPreviewsTOML)

	pending, err := svc.PendingMigrations(context.Background())
	if err != nil {
		t.Fatalf("PendingMigrations: %v", err)
	}
	for _, m := range pending {
		if m.Store == StorePreviews {
			t.Fatalf("a lossless migration was offered for consent: %+v", m)
		}
	}
}

// The legacy store IS detected, though — otherwise the previous test would
// pass against a build that never probes at all. Two arms of one fixture:
// Preflight sees it, PendingMigrations hides it.
func TestPreflightSeesTheLegacyPreviewsStore(t *testing.T) {
	t.Parallel()
	_, svc := previewRepo(t)
	seedLegacyPreviews(t, svc, legacyPreviewsTOML)

	vs, err := svc.Preflight(context.Background())
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	for _, v := range vs {
		if v.Feature.ID == FeaturePreviews {
			if v.State == 0 { // preflight.Satisfied
				t.Fatalf("the legacy previews store was not detected: %+v", v)
			}
			return
		}
	}
	t.Fatal("no verdict for the previews feature")
}

// Nothing to convert costs nothing and is not an error — this runs at every
// startup.
func TestRunAutoMigrationsWithNothingToDoIsANoOp(t *testing.T) {
	t.Parallel()
	_, svc := previewRepo(t)
	dir := seedLegacyPreviews(t, svc, "")

	if err := svc.RunAutoMigrations(context.Background()); err != nil {
		t.Fatalf("RunAutoMigrations: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "savedcompare.toml")); !os.IsNotExist(err) {
		t.Fatalf("a no-op run wrote savedcompare.toml (stat err = %v)", err)
	}
}

// An unknown action name is refused rather than silently doing nothing: a
// migration declaring a body this build has no code for must not report
// success and stamp the marker.
func TestMigrationActionRefusesAnUnknownName(t *testing.T) {
	t.Parallel()
	_, svc := previewRepo(t)
	if _, err := svc.migrationAction(context.Background(), "previews", "no-such-action"); err == nil {
		t.Fatal("migrationAction accepted an unknown action name")
	}
}

// RunAutoMigrations resolves against a Probes carrying ONLY the legacy map,
// so it must consider only features whose every requirement reads that map.
//
// The trap is specific and it is already in the tree: ForgeUsable reports
// NeedsStoreProbes() == false while reading Probes.Forge, so the obvious
// predicate would wave a forge-gated feature through to be judged on a
// channel this path never fills. The guard names what it supports instead.
func TestOnlyLegacyRequirementsRefusesEveryOtherKind(t *testing.T) {
	t.Parallel()
	legacy := preflight.Feature{Requires: []preflight.Requirement{preflight.LegacyStore{Store: "previews"}}}
	if !onlyLegacyRequirements(legacy) {
		t.Fatal("a LegacyStore-only feature was refused")
	}
	for name, f := range map[string]preflight.Feature{
		"forge":    {Requires: []preflight.Requirement{preflight.ForgeUsable{}}},
		"format":   {Requires: []preflight.Requirement{preflight.DataFormat{Store: "versions", Min: 2, Max: 2}}},
		"gitver":   {Requires: []preflight.Requirement{preflight.GitVersion{Min: [3]int{2, 30, 0}}}},
		"mixed":    {Requires: []preflight.Requirement{preflight.LegacyStore{Store: "previews"}, preflight.ForgeUsable{}}},
		"norequir": {},
	} {
		if onlyLegacyRequirements(f) {
			t.Fatalf("%s: accepted a feature this path cannot probe for", name)
		}
	}
}

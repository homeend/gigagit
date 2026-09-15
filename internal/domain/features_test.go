package domain

import (
	"testing"

	"github.com/homeend/gigagit/internal/preflight"
)

func TestFeaturesDeclaresCoreAndVersions(t *testing.T) {
	t.Parallel()
	fs := Features()

	byID := map[string]preflight.Feature{}
	for _, f := range fs {
		byID[f.ID] = f
	}

	core, ok := byID[FeatureCore]
	if !ok {
		t.Fatalf("Features() has no %q entry", FeatureCore)
	}
	if core.Criticality != preflight.Required {
		t.Errorf("%q criticality = %v, want Required", FeatureCore, core.Criticality)
	}
	if core.Migrate != nil {
		t.Errorf("%q must not declare a migration", FeatureCore)
	}

	versions, ok := byID[FeatureVersions]
	if !ok {
		t.Fatalf("Features() has no %q entry", FeatureVersions)
	}
	if versions.Criticality != preflight.Optional {
		t.Errorf("%q criticality = %v, want Optional", FeatureVersions, versions.Criticality)
	}
	if versions.Migrate == nil {
		t.Errorf("%q must declare the format 1->2 migration", FeatureVersions)
	}
}

func TestFeaturesAreSatisfiedOnACurrentRepo(t *testing.T) {
	t.Parallel()
	p := preflight.Probes{
		Stores:     map[string]preflight.StoreProbe{StoreVersions: {Format: VersionsFormat, HasData: true}},
		GitVersion: [3]int{2, 45, 0},
	}
	for _, v := range preflight.Resolve(Features(), p) {
		if v.State != preflight.Satisfied {
			t.Errorf("feature %q = %v on a current repo, want Satisfied (%s)", v.Feature.ID, v.State, v.Reason.Format)
		}
	}
}

func TestVersionsDeclaresTheFormatTwoMigration(t *testing.T) {
	t.Parallel()
	if VersionsFormat != 2 {
		t.Fatalf("VersionsFormat = %d, want 2", VersionsFormat)
	}
	var versions preflight.Feature
	for _, f := range Features() {
		if f.ID == FeatureVersions {
			versions = f
		}
	}
	if versions.Migrate == nil {
		t.Fatal("versions must declare a migration now that format 2 exists")
	}
	m := versions.Migrate
	if m.Store != StoreVersions || m.From != 1 || m.To != 2 {
		t.Errorf("migration = %+v, want versions 1->2", m)
	}
	if m.Describe == nil {
		t.Fatal("Describe is required: the consent screen has nothing to show without it")
	}
	txt := m.Describe()
	if txt.Format == "" {
		t.Error("Describe returned an empty format")
	}
}

func TestLegacyVersionsResolveAsRepairable(t *testing.T) {
	t.Parallel()
	p := preflight.Probes{
		// Data present, no marker: the pre-marker era, i.e. format 1.
		Stores:     map[string]preflight.StoreProbe{StoreVersions: {Format: 0, HasData: true}},
		GitVersion: [3]int{2, 45, 0},
	}
	for _, v := range preflight.Resolve(Features(), p) {
		if v.Feature.ID != FeatureVersions {
			continue
		}
		if v.State != preflight.Repairable {
			t.Errorf("versions on a legacy repo = %v, want Repairable", v.State)
		}
	}
}

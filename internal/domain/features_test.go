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
	if versions.Migrate != nil {
		t.Errorf("%q must not declare a migration yet (format 2 is the follow-up spec)", FeatureVersions)
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

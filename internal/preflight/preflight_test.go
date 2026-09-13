package preflight

import "testing"

func probes(format int, hasData bool, git [3]int) Probes {
	return Probes{
		Stores:     map[string]StoreProbe{"versions": {Format: format, HasData: hasData}},
		GitVersion: git,
	}
}

func versionsFeature(migrate *Migration) Feature {
	return Feature{
		ID:          "versions",
		Criticality: Optional,
		Requires:    []Requirement{DataFormat{Store: "versions", Min: 2, Max: 2}},
		Migrate:     migrate,
	}
}

func TestResolveStates(t *testing.T) {
	t.Parallel()
	mig := &Migration{Store: "versions", From: 1, To: 2,
		Describe: func() Text { return Text{Format: "discards %d snapshots", Args: []any{3}} }}

	cases := []struct {
		name    string
		feature Feature
		probes  Probes
		want    State
	}{
		{"in range", versionsFeature(mig), probes(2, true, [3]int{2, 40, 0}), Satisfied},
		{"no data, no marker", versionsFeature(mig), probes(0, false, [3]int{2, 40, 0}), Satisfied},
		{"legacy: data without marker is format 1", versionsFeature(mig), probes(0, true, [3]int{2, 40, 0}), Repairable},
		{"below range with migration", versionsFeature(mig), probes(1, true, [3]int{2, 40, 0}), Repairable},
		{"below range without migration", versionsFeature(nil), probes(1, true, [3]int{2, 40, 0}), Unsatisfiable},
		{"above range is never repairable", versionsFeature(mig), probes(3, true, [3]int{2, 40, 0}), Unsatisfiable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Resolve([]Feature{tc.feature}, tc.probes)
			if len(got) != 1 {
				t.Fatalf("Resolve returned %d verdicts, want 1", len(got))
			}
			if got[0].State != tc.want {
				t.Errorf("state = %v, want %v", got[0].State, tc.want)
			}
		})
	}
}

func TestResolveGitVersionIsNeverRepairable(t *testing.T) {
	t.Parallel()
	f := Feature{
		ID:          "core",
		Criticality: Required,
		Requires:    []Requirement{GitVersion{Min: [3]int{2, 40, 0}}},
		Migrate:     &Migration{Store: "versions", From: 1, To: 2, Describe: func() Text { return Text{} }},
	}
	got := Resolve([]Feature{f}, probes(2, true, [3]int{2, 30, 0}))
	if got[0].State != Unsatisfiable {
		t.Errorf("state = %v, want Unsatisfiable (a migration for another store must not repair git)", got[0].State)
	}
}

func TestResolveTakesWorstRequirement(t *testing.T) {
	t.Parallel()
	f := Feature{
		ID:          "versions",
		Criticality: Optional,
		Requires: []Requirement{
			DataFormat{Store: "versions", Min: 1, Max: 9}, // satisfied
			GitVersion{Min: [3]int{9, 0, 0}},              // unsatisfiable
		},
	}
	got := Resolve([]Feature{f}, probes(1, true, [3]int{2, 40, 0}))
	if got[0].State != Unsatisfiable {
		t.Errorf("state = %v, want Unsatisfiable", got[0].State)
	}
	if got[0].Reason.Format == "" {
		t.Error("Reason must describe the failing requirement")
	}
}

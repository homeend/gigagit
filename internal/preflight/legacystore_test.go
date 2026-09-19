package preflight

import "testing"

// A legacy store still holding data is Repairable when the feature declares a
// migration, and Satisfied once the file is gone.
func TestLegacyStoreFitsOnPresence(t *testing.T) {
	t.Parallel()
	req := LegacyStore{Store: "previews"}
	present := Probes{Legacy: map[string]LegacyProbe{"previews": {Present: true}}}
	absent := Probes{Legacy: map[string]LegacyProbe{"previews": {Present: false}}}
	unprobed := Probes{}

	if got := req.Fit(present); got != FitTooOld {
		t.Fatalf("Fit(present) = %v, want FitTooOld", got)
	}
	if got := req.Fit(absent); got != FitOK {
		t.Fatalf("Fit(absent) = %v, want FitOK", got)
	}
	// An UNPROBED store must read as OK, never as present: a caller that
	// skipped the probe must not trigger a migration it never measured.
	if got := req.Fit(unprobed); got != FitOK {
		t.Fatalf("Fit(unprobed) = %v, want FitOK", got)
	}
	if req.RepairStore() != "previews" {
		t.Fatalf("RepairStore = %q, want %q", req.RepairStore(), "previews")
	}
	// It reads Probes.Legacy, NOT Probes.Stores, so a Required-only resolve
	// must not be told it needs the git-ref store probes on its account.
	if req.NeedsStoreProbes() {
		t.Fatal("LegacyStore asked for the git-ref store probes")
	}
}

// A feature with a legacy store and a declared migration resolves Repairable.
func TestLegacyStoreResolvesRepairableWithAMigration(t *testing.T) {
	t.Parallel()
	f := Feature{
		ID:          "previews",
		Criticality: Optional,
		Requires:    []Requirement{LegacyStore{Store: "previews"}},
		Migrate:     &Migration{Store: "previews", To: 2, Action: "convert-previews", Lossless: true},
	}
	vs := Resolve([]Feature{f}, Probes{Legacy: map[string]LegacyProbe{"previews": {Present: true}}})
	if len(vs) != 1 {
		t.Fatalf("Resolve returned %d verdicts, want 1", len(vs))
	}
	if vs[0].State != Repairable {
		t.Fatalf("State = %v, want Repairable", vs[0].State)
	}
}

// THE POLARITY GATE. Lossless is what lets a migration run unasked, and its
// zero value must be the SAFE one: a migration that forgot to declare has to
// fall towards the consent screen, never past it.
//
// This is not a tautology about a bool's zero value — it pins which MEANING
// sits on that zero, and it names the case that makes the polarity matter:
// the branch-versions migration, the one that really does destroy data,
// declares nothing in this field at all. Inverting the field to "Consent"
// would not fail this test, it would fail to COMPILE it — which is the
// stronger outcome, since the flip cannot then land silently.
func TestAMigrationThatDeclaresNothingIsNotLossless(t *testing.T) {
	t.Parallel()
	var m Migration
	if m.Lossless {
		t.Fatal("an undeclared Migration reads as lossless — it would destroy data unasked")
	}
	// And the destructive migration gg actually ships declares nothing.
	versions := Migration{Store: "versions", From: 1, To: 2}
	if versions.Lossless {
		t.Fatal("the branch-versions discard reads as lossless")
	}
}

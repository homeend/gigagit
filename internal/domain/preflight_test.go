package domain

import (
	"context"
	"errors"
	"testing"

	"github.com/homeend/gigagit/internal/preflight"
)

func TestPreflightSatisfiedOnAFreshRepo(t *testing.T) {
	t.Parallel()
	svc := svcAt(cleanDir(t))

	vs, err := svc.Preflight(context.Background())
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	if len(vs) == 0 {
		t.Fatal("Preflight returned no verdicts")
	}
	for _, v := range vs {
		if v.State != preflight.Satisfied {
			t.Errorf("feature %q = %v, want Satisfied (%s)", v.Feature.ID, v.State, v.Reason.Format)
		}
	}
}

func TestPreflightTreatsUnmarkedVersionRefsAsLegacyFormatOne(t *testing.T) {
	t.Parallel()
	svc := svcAt(cleanDir(t))
	ctx := context.Background()

	// A repo from before markers existed: a version ref, no marker.
	head, err := svc.Repo().RevParse(ctx, "HEAD")
	if err != nil {
		t.Fatalf("RevParse: %v", err)
	}
	if err := svc.Repo().UpdateRef(ctx, "refs/gg/versions/main/1700000000-rebase", head); err != nil {
		t.Fatalf("UpdateRef: %v", err)
	}

	p, err := svc.preflightProbes(ctx)
	if err != nil {
		t.Fatalf("preflightProbes: %v", err)
	}
	got := p.Stores[StoreVersions]
	if got.Format != 0 || !got.HasData {
		t.Errorf("versions probe = %+v, want {Format:0 HasData:true}", got)
	}

	// With this build at format 1, legacy data resolves as Satisfied. The
	// follow-up spec bumps the range to 2, which turns this into Repairable.
	if fit := (preflight.DataFormat{Store: StoreVersions, Min: 2, Max: 2}).Fit(p); fit != preflight.FitTooOld {
		t.Errorf("fit against a format-2 build = %v, want FitTooOld", fit)
	}
}

func TestPreflightIsCached(t *testing.T) {
	t.Parallel()
	svc := svcAt(cleanDir(t))
	ctx := context.Background()

	a, err := svc.Preflight(ctx)
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	b, err := svc.Preflight(ctx)
	if err != nil {
		t.Fatalf("Preflight (second call): %v", err)
	}
	if len(a) != len(b) {
		t.Fatalf("cached verdicts differ: %d then %d", len(a), len(b))
	}
	if !svc.preflightDone {
		t.Error("preflightDone = false after Preflight; the result was not cached")
	}
}

func TestFeatureEnabledAndErrFeatureDisabled(t *testing.T) {
	t.Parallel()
	svc := svcAt(cleanDir(t))
	ctx := context.Background()

	if !svc.FeatureEnabled(ctx, FeatureVersions) {
		t.Error("FeatureEnabled(versions) = false on a fresh repo, want true")
	}
	if svc.FeatureEnabled(ctx, "no-such-feature") {
		t.Error("FeatureEnabled of an unknown id = true, want false")
	}

	err := error(&ErrFeatureDisabled{ID: FeatureVersions, Reason: "the versions store is at format 9"})
	var target *ErrFeatureDisabled
	if !errors.As(err, &target) || target.ID != FeatureVersions {
		t.Errorf("errors.As did not unwrap ErrFeatureDisabled: %v", err)
	}
	if got := err.Error(); got == "" {
		t.Error("ErrFeatureDisabled.Error() is empty")
	}
}

func TestPendingMigrationsIsEmptyWhenNothingIsRepairable(t *testing.T) {
	t.Parallel()
	svc := svcAt(cleanDir(t))

	got, err := svc.PendingMigrations(context.Background())
	if err != nil {
		t.Fatalf("PendingMigrations: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("PendingMigrations = %v, want none on a current repo", got)
	}
}

func TestPendingMigrationsListsRepairableFeaturesWithTheirRefs(t *testing.T) {
	t.Parallel()
	svc := svcAt(cleanDir(t))
	ctx := context.Background()

	head, err := svc.Repo().RevParse(ctx, "HEAD")
	if err != nil {
		t.Fatalf("RevParse: %v", err)
	}
	ref := "refs/gg/versions/main/1700000000-rebase"
	if err := svc.Repo().UpdateRef(ctx, ref, head); err != nil {
		t.Fatalf("UpdateRef: %v", err)
	}

	// Stand in for a future build: versions requires format 2 and can repair 1.
	svc.preflightMu.Lock()
	svc.preflightDone = true
	svc.preflightOut = []preflight.Verdict{{
		Feature: preflight.Feature{
			ID:          FeatureVersions,
			Criticality: preflight.Optional,
			Migrate: &preflight.Migration{Store: StoreVersions, From: 1, To: 2,
				Describe: func() preflight.Text {
					return preflight.Text{Format: "discards every recorded branch version in %s", Args: []any{"this repository"}}
				}},
		},
		State:  preflight.Repairable,
		Reason: preflight.Text{Format: "the %s store is at format %d", Args: []any{StoreVersions, 1}},
	}}
	svc.preflightMu.Unlock()

	got, err := svc.PendingMigrations(ctx)
	if err != nil {
		t.Fatalf("PendingMigrations: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("PendingMigrations returned %d entries, want 1", len(got))
	}
	m := got[0]
	if m.Feature != FeatureVersions || m.From != 1 || m.To != 2 {
		t.Errorf("migration = %+v, want versions 1→2", m)
	}
	if m.Consequence == "" {
		t.Error("Consequence is empty; consent has nothing to show")
	}
	if len(m.Refs) != 1 || m.Refs[0] != ref {
		t.Errorf("Refs = %v, want [%s]", m.Refs, ref)
	}

	if err := svc.RunMigration(ctx, m); err != nil {
		t.Fatalf("RunMigration: %v", err)
	}
	left, err := svc.Repo().ForEachRef(ctx, "refs/gg/versions/")
	if err != nil {
		t.Fatalf("ForEachRef: %v", err)
	}
	if len(left) != 0 {
		t.Errorf("%d version refs survived RunMigration, want 0", len(left))
	}
}

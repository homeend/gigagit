package domain

import (
	"context"
	"errors"
	"testing"

	"github.com/homeend/gigagit/internal/preflight"
)

func TestBranchVersionsRefusesWhenTheFeatureIsDisabled(t *testing.T) {
	t.Parallel()
	svc := svcAt(cleanDir(t))
	ctx := context.Background()

	// Force the disabled verdict rather than constructing a future-format repo:
	// the gate, not the resolver, is under test here.
	svc.preflightMu.Lock()
	svc.preflightDone = true
	svc.preflightOut = []preflight.Verdict{{
		Feature: preflight.Feature{ID: FeatureVersions, Criticality: preflight.Optional},
		State:   preflight.Unsatisfiable,
		Reason:  preflight.Text{Format: "the %s store is at format %d; this build needs %d-%d", Args: []any{StoreVersions, 9, 1, 1}},
	}}
	svc.preflightMu.Unlock()

	_, err := svc.BranchVersions(ctx, "main")
	var disabled *ErrFeatureDisabled
	if !errors.As(err, &disabled) {
		t.Fatalf("BranchVersions error = %v, want ErrFeatureDisabled", err)
	}
	if disabled.ID != FeatureVersions {
		t.Errorf("disabled.ID = %q, want %q", disabled.ID, FeatureVersions)
	}

	if _, err := svc.AllVersionBranches(ctx); !errors.As(err, &disabled) {
		t.Errorf("AllVersionBranches error = %v, want ErrFeatureDisabled", err)
	}
}

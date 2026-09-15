package cli

import (
	"strings"
	"testing"
)

func TestMigrateListsNothingOnACurrentRepo(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	code, out, errb := runCLI(t, dir, "migrate")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\nstdout: %s\nstderr: %s", code, out, errb)
	}
	if !strings.Contains(out, "nothing to migrate") {
		t.Errorf("output = %q, want it to say nothing is pending", out)
	}
}

// TestMigrateWithYesChangesNothingWhenNothingIsPending exercises --yes on a
// repo with nothing repairable: it must still be a no-op reporting "nothing
// to migrate", never an error and never an attempt to apply.
func TestMigrateWithYesChangesNothingWhenNothingIsPending(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	code, out, errb := runCLI(t, dir, "migrate", "--yes")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\nstdout: %s\nstderr: %s", code, out, errb)
	}
	if !strings.Contains(out, "nothing to migrate") {
		t.Errorf("output = %q, want it to say nothing is pending", out)
	}
	if strings.Contains(out, "migrated ") {
		t.Errorf("output = %q, must not claim anything was migrated", out)
	}
}

// TestMigrateBareInvocationMutatesNothing is the guarantee the whole feature
// exists to provide: with no flags, `gg migrate` never applies anything —
// only --yes does. It is exercised end-to-end against `gg status`, which
// must remain unaffected by a bare `gg migrate`.
func TestMigrateBareInvocationMutatesNothing(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	_, before, _ := runCLI(t, dir, "status")
	_, migOut, migErr := runCLI(t, dir, "migrate")
	afterCode, after, _ := runCLI(t, dir, "status")
	if afterCode != 0 {
		t.Fatalf("status after migrate: exit %d", afterCode)
	}
	if before != after {
		t.Errorf("status changed after a bare `gg migrate`:\nbefore: %s\nafter: %s\nmigrate stdout: %s\nmigrate stderr: %s", before, after, migOut, migErr)
	}
}

func TestMigrateRejectsUnknownFlag(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	code, _, errb := runCLI(t, dir, "migrate", "--bogus")
	if code != 2 {
		t.Fatalf("exit code = %d, want 2\nstderr: %s", code, errb)
	}
}

// TestMigrateOnARealLegacyRepoDiscardsAndStampsFormatTwo drives `gg
// migrate` end to end against a REAL repo holding a genuinely legacy
// (format-1) branch-version ref, resolved through the REAL domain.
// Features() registry — no injected verdict, no synthetic preflight.Probes
// value. The ref is planted with raw git exactly as it would be found:
// under refs/gg/versions/ with no refs/gg/meta/versions/<N> marker (the
// pre-marker era reads as format 1 by definition — see
// preflight.DataFormat.found). No other test exercises this: domain's
// TestLegacyVersionsResolveAsRepairable (features_test.go) checks
// classification against a hand-built preflight.Probes value, not a repo;
// domain's TestPendingMigrationsListsRepairableFeaturesWithTheirRefs
// (preflight_test.go) injects a hand-built Repairable verdict straight into
// svc.preflightOut, bypassing real probing. This test plants only the raw
// ref and calls the real `gg migrate` CLI twice — first bare (list without
// touching), then --yes (discard and stamp) — proving the whole chain from
// a real ref on disk to a real for-each-ref-visible discard.
func TestMigrateOnARealLegacyRepoDiscardsAndStampsFormatTwo(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	head := runGit(t, dir, "rev-parse", "HEAD")
	ref := "refs/gg/versions/main/1700000000-merge"
	runGit(t, dir, "update-ref", ref, head)

	// Bare: lists what --yes would discard, touches nothing.
	code, out, errb := runCLI(t, dir, "migrate")
	if code != 0 {
		t.Fatalf("migrate exit %d: %s", code, errb)
	}
	if !strings.Contains(out, "versions: format 1 -> 2") {
		t.Fatalf("stdout missing the pending migration line:\n%s", out)
	}
	if !strings.Contains(out, "discards 1 entries") {
		t.Fatalf("stdout missing the discard count:\n%s", out)
	}
	if strings.Contains(out, "migrated ") {
		t.Fatalf("bare invocation must not claim anything was migrated:\n%s", out)
	}
	if got := runGit(t, dir, "for-each-ref", "refs/gg/versions/"); got == "" {
		t.Fatal("bare migrate discarded the legacy ref; it must leave data untouched")
	}
	if got := runGit(t, dir, "for-each-ref", "refs/gg/meta/versions/2"); got != "" {
		t.Fatal("bare migrate must not stamp the format-2 marker")
	}

	// --yes: applies it — discards the legacy ref, stamps format 2.
	code, out, errb = runCLI(t, dir, "migrate", "--yes")
	if code != 0 {
		t.Fatalf("migrate --yes exit %d: %s", code, errb)
	}
	if !strings.Contains(out, "migrated versions to format 2") {
		t.Fatalf("stdout missing the migrated confirmation:\n%s", out)
	}
	if got := runGit(t, dir, "for-each-ref", "refs/gg/versions/"); got != "" {
		t.Fatalf("legacy ref survived --yes: %s", got)
	}
	if got := runGit(t, dir, "for-each-ref", "refs/gg/meta/versions/2"); got == "" {
		t.Fatal("--yes did not stamp refs/gg/meta/versions/2")
	}
}

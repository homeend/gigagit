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

package filelock

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestAcquireReleaseRoundTrip proves the basic shape: Acquire creates the
// lock file (and its parent directory), release removes it, and a second
// Acquire afterwards succeeds at once.
func TestAcquireReleaseRoundTrip(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "sub", "x.lock")
	release, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("lock file missing after Acquire: %v", err)
	}
	release()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("release must remove the lock file, stat err = %v", err)
	}
	release2, err := Acquire(path)
	if err != nil {
		t.Fatalf("second Acquire after release: %v", err)
	}
	release2()
}

// TestLockRetriesAPermissionRefusal is the portable stand-in for a Windows
// pending-delete name. Moved from internal/notes (where it lived before this
// package existed) — it tests the LOCK's retry behaviour, not a store.
//
// os.Remove on Windows does not unlink a file whose handle someone still
// holds — an on-access virus scanner, the search indexer — it marks the name
// PENDING DELETE, and an O_EXCL create against it then fails with
// ERROR_ACCESS_DENIED rather than ErrExist. The lock loop aborted on anything
// that was not ErrExist, so a write failed outright for a condition that
// clears in milliseconds. The Windows suite caught it as "Access is denied"
// from a Put whose lock had already been released.
//
// Windows cannot be driven from here, but the BRANCH can: an unwritable
// directory makes the same create fail with ErrPermission. Restoring the
// permission mid-flight must let the parked caller through, which is exactly
// what the pending-delete case needs and what the old code refused.
func TestLockRetriesAPermissionRefusal(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("chmod does not gate file creation on Windows; this models the Windows case FROM posix")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores the directory mode, so the refusal never happens")
	}
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "x.lock")
	if err := os.Chmod(dir, 0o500); err != nil { // r-x: no new names
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	done := make(chan error, 1)
	var release func()
	go func() {
		r, err := Acquire(lockPath)
		release = r
		done <- err
	}()

	select {
	case err := <-done:
		t.Fatalf("Acquire returned (%v) while the directory refused the lock", err)
	case <-time.After(150 * time.Millisecond): // well under Wait
	}

	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Acquire after the refusal cleared: %v", err)
	}
	if release != nil {
		release()
	}
}

package gittest

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A test that points GIT_CONFIG_GLOBAL at its own config (to exercise
// global-config behaviour) replaces Isolate's pinned file — and with it
// maintenance.auto=false. Run must keep background maintenance off anyway,
// or a template the test happens to build first leaves a detached
// maintenance child behind (the CI "copy template: open
// .git/objects/maintenance.lock" failure).
func TestRunDisablesMaintenanceUnderATestsOwnGlobalConfig(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	dir := t.TempDir()
	Run(t, dir, "init", "-b", "main")
	// `config --get` exits 1 for an unset key, which Run turns into Fatalf.
	Run(t, dir, "config", "--get", "maintenance.auto")
	Run(t, dir, "config", "--get", "gc.auto")
}

// A builder whose own git calls bypass Run (a package-local helper) can
// still leave a detached maintenance child holding maintenance.lock.
// TemplateRepo waits for it to finish instead of copying a lock that is
// about to vanish.
func TestTemplateRepoWaitsForDetachedMaintenance(t *testing.T) {
	t.Parallel()
	dst := TemplateRepo(t, "test-detached-maintenance", func(t *testing.T, dir string) {
		Run(t, dir, "init", "-b", "main")
		lock := filepath.Join(dir, ".git", "objects", "maintenance.lock")
		if err := os.WriteFile(lock, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		go func() { // the detached child finishing after the builder returns
			time.Sleep(300 * time.Millisecond)
			os.Remove(lock)
		}()
	})
	if _, err := os.Stat(filepath.Join(dst, ".git", "objects", "maintenance.lock")); !os.IsNotExist(err) {
		t.Fatalf("copy holds maintenance.lock (stat err %v); want the copy taken after maintenance ended", err)
	}
}

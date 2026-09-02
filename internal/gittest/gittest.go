// Package gittest pins the ambient git environment for test processes: a
// minimal deterministic global config (test identity, core.autocrlf off,
// commit signing off, main as the default branch) and no system config, so a
// developer machine's gitconfig can never leak into test repos — Git for
// Windows ships core.autocrlf=true, which turns every checked-out "alpha\n"
// into "alpha\r\n" and fails hundreds of content assertions. Imported for
// effect from a gitenv_test.go in every package that runs real git.
package gittest

import (
	"os"
	"path/filepath"
	"sync"
)

var once sync.Once

// Isolate points GIT_CONFIG_GLOBAL at a minimal fixed config and disables the
// system config for this process and every subprocess it spawns (the test's
// own git commands AND gg's ExecRunner inherit process env). It is called
// from test-file init() funcs — before any test or parallel goroutine runs —
// so plain os.Setenv is safe. Tests that exercise global-config behavior keep
// working: their t.Setenv("GIT_CONFIG_GLOBAL", …) overrides this for the
// test's duration and restores it afterwards.
func Isolate() {
	once.Do(func() {
		dir, err := os.MkdirTemp("", "gg-test-gitconfig-*")
		if err != nil {
			return // fail-open: the suite then runs against ambient config
		}
		cfg := filepath.Join(dir, "gitconfig")
		content := "[user]\n" +
			"\tname = gg-test\n" +
			"\temail = gg-test@test.invalid\n" +
			"[core]\n" +
			"\tautocrlf = false\n" +
			"[commit]\n" +
			"\tgpgsign = false\n" +
			"[tag]\n" +
			"\tgpgSign = false\n" +
			"[init]\n" +
			"\tdefaultBranch = main\n" +
			// No background maintenance under tests. git ≥ 2.46 DETACHES the
			// auto maintenance `git commit` kicks off (maintenance.autoDetach
			// defaults to true), so a fixture's last commit returns while a
			// child still holds .git/objects/maintenance.lock — and
			// TemplateRepo's CopyFS then lists a lock file that is gone by the
			// time it opens it ("copy template: open .git/objects/
			// maintenance.lock: no such file or directory", seen on CI only:
			// a git 2.43 dev box never detaches). gc.auto = 0 covers the older
			// `git gc --auto` lane the same way.
			"[maintenance]\n" +
			"\tauto = false\n" +
			"[gc]\n" +
			"\tauto = 0\n"
		if err := os.WriteFile(cfg, []byte(content), 0o644); err != nil {
			return
		}
		os.Setenv("GIT_CONFIG_GLOBAL", cfg)
		os.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	})
}

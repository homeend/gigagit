package gittest

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Run executes one git command in dir with the standard test identity,
// failing the test on a non-zero exit. Shared by template builders and any
// helper that still needs a bespoke git call.
//
// Background maintenance is switched off on the command line, not only in
// Isolate's global config: a test that sets its own GIT_CONFIG_GLOBAL and
// then builds a template would otherwise commit with maintenance on, and
// git ≥ 2.46 detaches it (see Isolate).
func Run(t *testing.T, dir string, args ...string) {
	t.Helper()
	args = append([]string{"-c", "maintenance.auto=false", "-c", "gc.auto=0"}, args...)
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

var (
	tmplMu   sync.Mutex
	tmplDir  string // parent holding all templates; removed by the OS temp cleaner
	tmplRepo = map[string]string{}
	tmplErr  = map[string]error{}
)

// TemplateRepo returns a fresh copy of the template repo named key, building
// it ONCE per test process via build (which receives an empty directory and
// must leave a git repository in it). Every later caller pays only a file
// copy — no processes. That matters most on Windows, where the 4–8 git
// spawns of per-test repo setup (CreateProcess + antivirus per spawn)
// dominated the suite's wall clock.
//
// Copies share the template's commit SHAs and timestamps within one test
// process; a test that adds commits on top diverges normally. build runs
// under the pinned gittest environment like any other test git invocation.
func TemplateRepo(t *testing.T, key string, build func(t *testing.T, dir string)) string {
	t.Helper()
	tmplMu.Lock()
	if err, bad := tmplErr[key]; bad {
		tmplMu.Unlock()
		t.Fatalf("template %q failed to build earlier: %v", key, err)
	}
	src, ok := tmplRepo[key]
	if !ok {
		if tmplDir == "" {
			d, err := os.MkdirTemp("", "gg-test-templates-*")
			if err != nil {
				tmplMu.Unlock()
				t.Fatalf("template root: %v", err)
			}
			tmplDir = d
		}
		src = filepath.Join(tmplDir, sanitizeKey(key))
		if err := os.MkdirAll(src, 0o755); err != nil {
			tmplErr[key] = err
			tmplMu.Unlock()
			t.Fatalf("template dir: %v", err)
		}
		build(t, src) // Fatalf on failure fails THIS test; the flag below stops reuse
		if _, err := os.Stat(filepath.Join(src, ".git")); err != nil {
			tmplErr[key] = err
			tmplMu.Unlock()
			t.Fatalf("template %q did not produce a git repo: %v", key, err)
		}
		waitMaintenance(src)
		tmplRepo[key] = src
	}
	tmplMu.Unlock()

	dst := t.TempDir()
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		t.Fatalf("copy template %q: %v", key, err)
	}
	return dst
}

// maintenanceLocks are the files a detached `git maintenance run --auto` or
// `git gc --auto` holds while it works on a repository.
var maintenanceLocks = []string{
	filepath.Join(".git", "objects", "maintenance.lock"),
	filepath.Join(".git", "gc.pid"),
}

// waitMaintenance waits (bounded) until no background maintenance holds the
// template at dir. A builder whose git calls bypass Run can still leave a
// detached child behind; copying while it runs lists a lock file that is
// gone by the time CopyFS opens it. After the bound the copy goes ahead and
// fails loudly if the child is still there.
func waitMaintenance(dir string) {
	deadline := time.Now().Add(30 * time.Second)
	for _, rel := range maintenanceLocks {
		for time.Now().Before(deadline) {
			if _, err := os.Stat(filepath.Join(dir, rel)); os.IsNotExist(err) {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
}

// BasicRepo is the canonical fixture nearly every package uses: branch main
// with one commit ("initial") containing README.md with the given content.
func BasicRepo(t *testing.T, readme string) string {
	t.Helper()
	return TemplateRepo(t, "basic:"+readme, func(t *testing.T, dir string) {
		Run(t, dir, "init", "-b", "main")
		if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(readme), 0o644); err != nil {
			t.Fatal(err)
		}
		Run(t, dir, "add", ".")
		Run(t, dir, "commit", "-m", "initial")
	})
}

// sanitizeKey turns a template key into a safe directory name.
func sanitizeKey(key string) string {
	out := make([]rune, 0, len(key))
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			out = append(out, r)
		default:
			out = append(out, '_')
		}
	}
	return string(out)
}

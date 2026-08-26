package gittest

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

// Run executes one git command in dir with the standard test identity,
// failing the test on a non-zero exit. Shared by template builders and any
// helper that still needs a bespoke git call.
func Run(t *testing.T, dir string, args ...string) {
	t.Helper()
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
		tmplRepo[key] = src
	}
	tmplMu.Unlock()

	dst := t.TempDir()
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		t.Fatalf("copy template %q: %v", key, err)
	}
	return dst
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

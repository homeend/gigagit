package web

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Every static module must parse as the browser parses it: an ES module.
// `node --check x.js` passes a broken .js file silently (node guesses the
// module kind), so each file is checked under a .mjs name. One syntax error
// in a module the page imports takes the whole page down.
func TestStaticModulesParse(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	files, err := filepath.Glob(filepath.Join("static", "*.js"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no static modules: %v", err)
	}
	dir := t.TempDir()
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			t.Parallel()
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			m := filepath.Join(dir, strings.TrimSuffix(filepath.Base(f), ".js")+".mjs")
			if err := os.WriteFile(m, src, 0o644); err != nil {
				t.Fatal(err)
			}
			if out, err := exec.Command(node, "--check", m).CombinedOutput(); err != nil {
				t.Errorf("%s does not parse as a module: %v\n%s", f, err, out)
			}
		})
	}
}

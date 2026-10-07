// Package forgetest builds the fake gh binary once per test process and seeds
// its fixture directory. Test support only; it imports nothing from forge.
package forgetest

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

const (
	EnvBin      = "GG_GH_BIN"     // forge.EnvBin
	EnvFixtures = "GG_FAKEGH_DIR" // read by the fake; unset = <cwd>/.git/fakegh
)

var (
	once    sync.Once
	binPath string
	binErr  error
)

func build() {
	_, self, _, _ := runtime.Caller(0)
	src := filepath.Join(filepath.Dir(self), "..", "testdata", "fakegh")
	dir, err := os.MkdirTemp("", "gg-fakegh")
	if err != nil {
		binErr = err
		return
	}
	binPath = filepath.Join(dir, "gh")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", binPath, ".")
	cmd.Dir = src
	if out, err := cmd.CombinedOutput(); err != nil {
		binErr = fmt.Errorf("%w\n%s", err, out)
	}
}

// BuildFakeGH returns the fake gh's absolute path, building it on first use.
func BuildFakeGH(t testing.TB) string {
	t.Helper()
	once.Do(build)
	if binErr != nil {
		t.Fatalf("build fakegh: %v", binErr)
	}
	return binPath
}

// BuildFakeGHMain is BuildFakeGH for a TestMain, which has no testing.TB.
func BuildFakeGHMain() (string, error) {
	once.Do(build)
	return binPath, binErr
}

// Seed writes name→content fixture files into dir (created if missing).
func Seed(t testing.TB, dir string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// Write is one mutation the fake gh recorded.
type Write struct {
	Op     string         `json:"op"`
	Vars   map[string]any `json:"variables"`
	Failed bool           `json:"failed"` // a fail-<Op> file made it fail
}

// Writes reads the fake gh's writes.jsonl in dir (none = empty).
func Writes(t testing.TB, dir string) []Write {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "writes.jsonl"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []Write
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var w Write
		if err := json.Unmarshal([]byte(line), &w); err != nil {
			t.Fatalf("writes.jsonl: %v", err)
		}
		out = append(out, w)
	}
	return out
}

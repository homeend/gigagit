package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOverviewExport(t *testing.T) {
	t.Parallel()
	dir := overviewCLIRepo(t)
	code, out, errb := runCLIStdin(t, dir, ovCLIDoc, "overview", "save", "--agent", "Claude Code", "--stdin")
	if code != 0 {
		t.Fatalf("save = %d %q", code, errb)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	link := strings.TrimSpace(lines[len(lines)-1])
	_, out, _ = runCLI(t, dir, "overview", "list")
	id := strings.Fields(out)[0]

	code, page, errb := runCLI(t, dir, "overview", "export", "--out", "-", id)
	if code != 0 || !strings.Contains(page, `id="gg-data"`) || !strings.Contains(page, `"path":"a.go"`) || strings.Contains(page, "the gate") {
		t.Fatalf("stdout export = %d %q", code, errb)
	}
	if code, page, errb = runCLI(t, dir, "overview", "export", "--out", "-", link); code != 0 || !strings.Contains(page, "How a works") {
		t.Fatalf("export by link = %d %q", code, errb)
	}
	target := filepath.Join(t.TempDir(), "x.html")
	if code, _, errb := runCLI(t, dir, "overview", "export", "--out", target, id); code != 0 {
		t.Fatalf("file export = %d %q", code, errb)
	}
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, errb := runCLI(t, dir, "overview", "export", "--out", target, id); code != 2 || !strings.Contains(errb, "--force") {
		t.Fatalf("no --force = %d %q", code, errb)
	}
	if code, _, errb := runCLI(t, dir, "overview", "export", "--force", "--out", target, id); code != 0 {
		t.Fatalf("--force = %d %q", code, errb)
	}
	if b, _ := os.ReadFile(target); string(b) == "old" {
		t.Fatal("--force did not overwrite")
	}
	if code, _, errb := runCLI(t, dir, "overview", "export", "--out", "-", "nope"); code != 1 || !strings.Contains(errb, "overview not found") {
		t.Fatalf("unknown id = %d %q", code, errb)
	}
	if code, _, errb := runCLI(t, dir, "overview", "export"); code != 2 || !strings.Contains(errb, "usage") {
		t.Fatalf("no id = %d %q", code, errb)
	}
}

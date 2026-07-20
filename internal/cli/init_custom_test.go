package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/agentskill"
)

// customFixture: an empty project (no detectable agents), a temp home, and a
// temp custom-targets state file wired into the package seams.
func customFixture(t *testing.T) (proj, targets string) {
	t.Helper()
	proj = t.TempDir()
	oldHome, oldTargets := InitHomeDir, InitTargetsPath
	InitHomeDir = t.TempDir()
	targets = filepath.Join(t.TempDir(), "agent-targets.toml")
	InitTargetsPath = targets
	t.Cleanup(func() { InitHomeDir, InitTargetsPath = oldHome, oldTargets })
	return proj, targets
}

func TestInitToFileInstallsManagedBlockAndRecords(t *testing.T) {
	proj, targets := customFixture(t)
	dest := filepath.Join(t.TempDir(), "myagent-instructions.md")
	os.WriteFile(dest, []byte("existing prose\n"), 0o644)

	code, out, errb := runInitCmd(t, proj, "", "--to", dest)
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, errb)
	}
	data, err := os.ReadFile(dest)
	if err != nil || agentskill.InstalledVersion(data) != agentskill.Version {
		t.Fatalf("skill block not installed at %s: %v", dest, err)
	}
	if !strings.Contains(string(data), "existing prose") {
		t.Fatal("surrounding content must be preserved")
	}
	reg, err := os.ReadFile(targets)
	if err != nil || !strings.Contains(string(reg), dest) {
		t.Fatalf("custom target not recorded in %s: %v", targets, err)
	}
	if !strings.Contains(out, dest) {
		t.Errorf("output should name the target:\n%s", out)
	}
}

func TestInitToDirectoryInstallsSkillFile(t *testing.T) {
	proj, _ := customFixture(t)
	dest := t.TempDir()

	code, _, errb := runInitCmd(t, proj, "", "--to", dest)
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, errb)
	}
	data, err := os.ReadFile(filepath.Join(dest, "using-gg", "SKILL.md"))
	if err != nil || agentskill.InstalledVersion(data) != agentskill.Version {
		t.Fatalf("skill file not installed under dir: %v", err)
	}
}

func TestInitUpdateRefreshesRecordedCustomTarget(t *testing.T) {
	proj, _ := customFixture(t)
	dest := filepath.Join(t.TempDir(), "AGENTS.md")
	if code, _, errb := runInitCmd(t, proj, "", "--to", dest); code != 0 {
		t.Fatalf("seed install failed: %s", errb)
	}
	// Age the installed copy: rewrite its version marker to v1.
	data, _ := os.ReadFile(dest)
	aged := regexp.MustCompile(`using-gg:v\d+`).ReplaceAllString(string(data), "using-gg:v1")
	os.WriteFile(dest, []byte(aged), 0o644)

	code, out, errb := runInitCmd(t, proj, "", "--update")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, errb)
	}
	fresh, _ := os.ReadFile(dest)
	if agentskill.InstalledVersion(fresh) != agentskill.Version {
		t.Fatalf("--update should refresh the custom target, still at: %d", agentskill.InstalledVersion(fresh))
	}
	if !strings.Contains(out, "Custom") {
		t.Errorf("output should mention the Custom target:\n%s", out)
	}
}

func TestInitListShowsCustomRow(t *testing.T) {
	proj, _ := customFixture(t)
	dest := filepath.Join(t.TempDir(), "AGENTS.md")
	if code, _, _ := runInitCmd(t, proj, "", "--to", dest); code != 0 {
		t.Fatal("seed install failed")
	}
	code, out, _ := runInitCmd(t, proj, "", "--list")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out, "Custom") || !strings.Contains(out, dest) || !strings.Contains(out, "up to date") {
		t.Errorf("list should show the recorded custom target with status:\n%s", out)
	}
}

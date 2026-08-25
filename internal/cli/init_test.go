package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/agentskill"
)

// initFixture: a project dir with .claude and AGENTS.md, plus a temp "home"
// with ~/.claude, wired into the package seam for one test.
func initFixture(t *testing.T) (string, string) {
	t.Helper()
	proj := t.TempDir()
	os.MkdirAll(filepath.Join(proj, ".claude"), 0o755)
	os.WriteFile(filepath.Join(proj, "AGENTS.md"), []byte("mine\n"), 0o644)
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	old := InitHomeDir
	InitHomeDir = home
	t.Cleanup(func() { InitHomeDir = old })
	isolateTargets(t)
	return proj, home
}

// isolateTargets points the custom-targets registry seam at a per-test file so
// no init test ever reads or writes the developer's real agent-targets.toml
// (a machine-state leak that once let one test's recorded target surface in
// another's "Detected agents" output).
func isolateTargets(t *testing.T) {
	t.Helper()
	old := InitTargetsPath
	InitTargetsPath = filepath.Join(t.TempDir(), "agent-targets.toml")
	t.Cleanup(func() { InitTargetsPath = old })
}

func runInitCmd(t *testing.T, proj, stdinStr string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(proj, append([]string{"init"}, args...), strings.NewReader(stdinStr), &out, &errb, "")
	return code, out.String(), errb.String()
}

func TestInitListShowsCheckboxesAndStatus(t *testing.T) {
	proj, _ := initFixture(t)
	code, out, _ := runInitCmd(t, proj, "", "--list")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	for _, want := range []string{"[ ]", "Claude Code (project)", "Claude Code (global)", "AGENTS.md", "new"} {
		if !strings.Contains(out, want) {
			t.Errorf("list missing %q:\n%s", want, out)
		}
	}
}

func TestInitAllInstallsEverythingDetected(t *testing.T) {
	proj, home := initFixture(t)
	code, out, errb := runInitCmd(t, proj, "", "--all")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, errb)
	}
	for _, p := range []string{
		filepath.Join(proj, ".claude", "skills", "using-gg", "SKILL.md"),
		filepath.Join(home, ".claude", "skills", "using-gg", "SKILL.md"),
		filepath.Join(proj, "AGENTS.md"),
	} {
		data, err := os.ReadFile(p)
		if err != nil || agentskill.InstalledVersion(data) != agentskill.Version {
			t.Errorf("not installed at %s (%v)", p, err)
		}
	}
	if !strings.Contains(out, "installed") {
		t.Errorf("output should report installs:\n%s", out)
	}
}

func TestInitUpdateRefreshesOnlyInstalled(t *testing.T) {
	proj, _ := initFixture(t)
	// Pre-install ONLY agents-md, with an old version marker.
	target := filepath.Join(proj, "AGENTS.md")
	os.WriteFile(target, []byte("mine\n\n<!-- gg:using-gg:v0:begin -->\nold\n<!-- gg:using-gg:end -->\n"), 0o644)
	code, _, errb := runInitCmd(t, proj, "", "--update")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, errb)
	}
	data, _ := os.ReadFile(target)
	if agentskill.InstalledVersion(data) != agentskill.Version {
		t.Error("--update should refresh the outdated block")
	}
	if _, err := os.Stat(filepath.Join(proj, ".claude", "skills", "using-gg", "SKILL.md")); !os.IsNotExist(err) {
		t.Error("--update must NOT install into new targets")
	}
}

func TestInitAgentsByIDAndUnknownID(t *testing.T) {
	proj, _ := initFixture(t)
	code, _, _ := runInitCmd(t, proj, "", "--agents", "claude-project")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if _, err := os.Stat(filepath.Join(proj, ".claude", "skills", "using-gg", "SKILL.md")); err != nil {
		t.Error("claude-project not installed")
	}
	code, _, errb := runInitCmd(t, proj, "", "--agents", "claude-project,bogus")
	if code != 2 || !strings.Contains(errb, "bogus") {
		t.Fatalf("unknown ID should exit 2 naming it, got %d / %q", code, errb)
	}
}

func TestInitInteractiveNumberInstallsExactlyOne(t *testing.T) {
	proj, home := initFixture(t)
	code, _, errb := runInitCmd(t, proj, "1\n")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, errb)
	}
	// Entry 1 is claude-project (registry order). Exactly one install.
	if _, err := os.Stat(filepath.Join(proj, ".claude", "skills", "using-gg", "SKILL.md")); err != nil {
		t.Error("entry 1 (claude-project) not installed")
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "using-gg", "SKILL.md")); !os.IsNotExist(err) {
		t.Error("entry 2 must not be installed")
	}
}

func TestInitEmptyEnterAppliesCheckedDefaults(t *testing.T) {
	proj, _ := initFixture(t)
	// Nothing installed yet: empty enter is a clean no-op.
	code, out, _ := runInitCmd(t, proj, "\n")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if _, err := os.Stat(filepath.Join(proj, ".claude", "skills", "using-gg", "SKILL.md")); !os.IsNotExist(err) {
		t.Error("nothing should be installed on empty-enter with no defaults")
	}
	if !strings.Contains(out, "nothing") {
		t.Errorf("should say nothing to do:\n%s", out)
	}
	// Install one, then empty-enter refreshes it (and only it).
	if code, _, _ := runInitCmd(t, proj, "", "--agents", "agents-md"); code != 0 {
		t.Fatal("seed install failed")
	}
	code, out, _ = runInitCmd(t, proj, "\n")
	if code != 0 || !strings.Contains(out, "refreshed") {
		t.Fatalf("empty-enter should refresh installed targets: %d\n%s", code, out)
	}
}

func TestInitNoInputExitsOneWithHint(t *testing.T) {
	proj, _ := initFixture(t)
	code, _, errb := runInitCmd(t, proj, "") // EOF immediately: non-interactive
	if code != 1 {
		t.Fatalf("EOF without selection should exit 1, got %d", code)
	}
	if !strings.Contains(errb, "--all") {
		t.Errorf("hint should mention --all:\n%s", errb)
	}
}

func TestInitQuitAborts(t *testing.T) {
	proj, _ := initFixture(t)
	code, _, _ := runInitCmd(t, proj, "q\n")
	if code != 0 {
		t.Fatalf("q should exit 0, got %d", code)
	}
	if _, err := os.Stat(filepath.Join(proj, ".claude", "skills", "using-gg", "SKILL.md")); !os.IsNotExist(err) {
		t.Error("q must not install anything")
	}
}

func TestInitNothingDetected(t *testing.T) {
	proj := t.TempDir() // no agent dirs at all
	old := InitHomeDir
	InitHomeDir = t.TempDir()
	t.Cleanup(func() { InitHomeDir = old })
	isolateTargets(t)
	var out, errb bytes.Buffer
	code := Run(proj, []string{"init"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 || !strings.Contains(out.String(), "no") {
		t.Fatalf("nothing detected should exit 0 and say so: %d\n%s", code, out.String())
	}
}

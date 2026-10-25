package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/agentskill"
	"github.com/homeend/gigagit/internal/promptstate"
)

// firstRunFixture is initFixture plus a temp state dir behind RepoStatePath
// (the prompts.toml marker lives beside it) and a stdin that reads as a
// terminal. It returns the project dir, the home dir and the prompts store.
func firstRunFixture(t *testing.T) (string, string, *promptstate.FileStore) {
	t.Helper()
	proj, home := initFixture(t)
	state := filepath.Join(t.TempDir(), "gg")
	oldState, oldTTY := RepoStatePath, firstRunTTY
	RepoStatePath = filepath.Join(state, "repos.toml")
	firstRunTTY = func() bool { return true }
	t.Cleanup(func() { RepoStatePath, firstRunTTY = oldState, oldTTY })
	return proj, home, promptstate.NewFileStore(filepath.Join(state, "prompts.toml"))
}

func runFirstRun(proj, stdin string) (string, string) {
	var out, errb bytes.Buffer
	FirstRunInit(proj, strings.NewReader(stdin), &out, &errb)
	return out.String(), errb.String()
}

func usingGGInstalled(home string) bool {
	data, err := os.ReadFile(filepath.Join(home, ".claude", "skills", "using-gg", "SKILL.md"))
	return err == nil && agentskill.InstalledVersion(data) == agentskill.Version
}

func TestFirstRunYesShowsInitDialogAndInstalls(t *testing.T) {
	proj, home, ps := firstRunFixture(t)
	out, errb := runFirstRun(proj, "y\na\n")
	if !strings.Contains(errb, "Install gg's skills") {
		t.Errorf("no first-run question:\n%s", errb)
	}
	if !strings.Contains(out, "Detected agents:") || !strings.Contains(errb, "Apply?") {
		t.Errorf("yes should show the gg init dialog:\nout=%s\nerr=%s", out, errb)
	}
	if !usingGGInstalled(home) {
		t.Error("a=all should have installed the skills")
	}
	if !ps.SuppressedPrompts()[firstRunPromptID] {
		t.Error("the question must be asked only once")
	}
}

func TestFirstRunEnterMeansYes(t *testing.T) {
	proj, _, _ := firstRunFixture(t)
	out, _ := runFirstRun(proj, "\nq\n")
	if !strings.Contains(out, "Detected agents:") {
		t.Errorf("enter should show the gg init dialog:\n%s", out)
	}
}

func TestFirstRunNoInstallsNothingAndRemembers(t *testing.T) {
	proj, home, ps := firstRunFixture(t)
	out, errb := runFirstRun(proj, "n\n")
	if strings.Contains(out, "Detected agents:") {
		t.Errorf("no should skip the dialog:\n%s", out)
	}
	if !strings.Contains(errb, "gg init") {
		t.Errorf("no should name gg init for later:\n%s", errb)
	}
	if usingGGInstalled(home) {
		t.Error("no must not install anything")
	}
	if !ps.SuppressedPrompts()[firstRunPromptID] {
		t.Error("a no must be remembered too")
	}
}

func TestFirstRunAskedOnlyOnce(t *testing.T) {
	proj, _, ps := firstRunFixture(t)
	if err := ps.SuppressPrompt(firstRunPromptID); err != nil {
		t.Fatal(err)
	}
	out, errb := runFirstRun(proj, "y\na\n")
	if out != "" || errb != "" {
		t.Errorf("an answered question must stay quiet:\nout=%s\nerr=%s", out, errb)
	}
}

func TestFirstRunEOFAsksAgainNextTime(t *testing.T) {
	proj, _, ps := firstRunFixture(t)
	runFirstRun(proj, "")
	if ps.SuppressedPrompts()[firstRunPromptID] {
		t.Error("no answer must not be remembered as one")
	}
}

func TestFirstRunSkillsAlreadyInstalledStaysQuiet(t *testing.T) {
	proj, _, ps := firstRunFixture(t)
	if code, _, errb := runInitCmd(t, proj, "", "--agents", "claude-global"); code != 0 {
		t.Fatalf("seed install: %d %s", code, errb)
	}
	out, errb := runFirstRun(proj, "y\na\n")
	if out != "" || errb != "" {
		t.Errorf("a user with skills installed is not new:\nout=%s\nerr=%s", out, errb)
	}
	if !ps.SuppressedPrompts()[firstRunPromptID] {
		t.Error("an already-set-up user should be marked as asked")
	}
}

func TestFirstRunNoAgentsDetectedWaits(t *testing.T) {
	_, _, ps := firstRunFixture(t)
	InitHomeDir = t.TempDir() // a home with no agents; the cleanup restores it
	out, errb := runFirstRun(t.TempDir(), "y\na\n")
	if out != "" || errb != "" {
		t.Errorf("nothing to install should stay quiet:\nout=%s\nerr=%s", out, errb)
	}
	if ps.SuppressedPrompts()[firstRunPromptID] {
		t.Error("with no agents yet, the question must wait for one")
	}
}

func TestFirstRunNotATerminalStaysQuiet(t *testing.T) {
	proj, _, ps := firstRunFixture(t)
	firstRunTTY = func() bool { return false }
	out, errb := runFirstRun(proj, "y\na\n")
	if out != "" || errb != "" || ps.SuppressedPrompts()[firstRunPromptID] {
		t.Errorf("piped stdin must neither ask nor remember:\nout=%s\nerr=%s", out, errb)
	}
}

func TestFirstRunNoStateDirStaysQuiet(t *testing.T) {
	proj, _, _ := firstRunFixture(t)
	RepoStatePath = ""
	out, errb := runFirstRun(proj, "y\na\n")
	if out != "" || errb != "" {
		t.Errorf("without a place to remember the answer, never ask:\nout=%s\nerr=%s", out, errb)
	}
}

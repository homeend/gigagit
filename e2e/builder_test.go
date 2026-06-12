package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// rawGit runs git for test verification (no date pinning).
func rawGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func localScenario(steps []Step) *Scenario {
	exit := 0
	return &Scenario{
		Name:  "t",
		Input: Input{Steps: steps},
		Runs:  []Run{{Cmd: []string{"status"}, Exit: &exit}},
	}
}

func TestBuildLocalRepo(t *testing.T) {
	sb := buildSandbox(t, localScenario([]Step{
		{Write: "README.md", Content: "hello\n"},
		{Commit: "initial"},
		{Branch: "feature/x"},
		{Write: "README.md", Content: "hello\nwip\n"},
	}))
	if got := strings.TrimSpace(rawGit(t, sb.LocalDir, "branch", "--show-current")); got != "main" {
		t.Errorf("current branch = %q, want main", got)
	}
	if !strings.Contains(rawGit(t, sb.LocalDir, "branch"), "feature/x") {
		t.Error("branch feature/x missing")
	}
	data, err := os.ReadFile(filepath.Join(sb.LocalDir, "README.md"))
	if err != nil || string(data) != "hello\nwip\n" {
		t.Errorf("README.md = %q, %v", data, err)
	}
	if !strings.Contains(rawGit(t, sb.LocalDir, "status", "--porcelain"), " M README.md") {
		t.Error("README.md should be modified/unstaged")
	}
	if _, err := os.Stat(filepath.Join(sb.LocalDir, ".gg.toml")); err != nil {
		t.Error(".gg.toml not injected")
	}
}

func TestBuildIsDeterministic(t *testing.T) {
	// Determinism holds because ticks starts at 0 per Sandbox and dateBase is fixed, with identity pinned in TestMain.
	steps := []Step{
		{Write: "a.txt", Content: "1\n"},
		{Commit: "one"},
		{Write: "a.txt", Content: "2\n"},
		{Commit: "two"},
	}
	a := buildSandbox(t, localScenario(steps))
	b := buildSandbox(t, localScenario(steps))
	ha := rawGit(t, a.LocalDir, "log", "--format=%H")
	hb := rawGit(t, b.LocalDir, "log", "--format=%H")
	if ha != hb {
		t.Errorf("two builds differ:\n%s\nvs\n%s", ha, hb)
	}
}

func TestBuildStashAndSwitchSteps(t *testing.T) {
	sb := buildSandbox(t, localScenario([]Step{
		{Write: "a.txt", Content: "base\n"},
		{Commit: "initial"},
		{Branch: "other"},
		{Write: "a.txt", Content: "edit\n"},
		{Stash: "wip"},
		{Switch: "other"},
	}))
	if got := strings.TrimSpace(rawGit(t, sb.LocalDir, "branch", "--show-current")); got != "other" {
		t.Errorf("current branch = %q, want other", got)
	}
	if got := strings.TrimSpace(rawGit(t, sb.LocalDir, "stash", "list")); !strings.Contains(got, "wip") {
		t.Errorf("stash list = %q, want a wip entry", got)
	}
}

func TestBuildSnapshotsInputSums(t *testing.T) {
	sb := buildSandbox(t, localScenario([]Step{
		{Write: "a.txt", Content: "v1\n"},
		{Commit: "initial"},
		{Write: "dirty.txt", Content: "uncommitted\n"},
	}))
	if _, ok := sb.InputSums["a.txt"]; !ok {
		t.Errorf("InputSums missing a.txt: %v", sb.InputSums)
	}
	if _, ok := sb.InputSums["dirty.txt"]; !ok {
		t.Errorf("InputSums missing dirty.txt (uncommitted file): %v", sb.InputSums)
	}
	if _, ok := sb.InputSums[".git/HEAD"]; ok {
		t.Error("InputSums must not include .git internals")
	}
}

func originScenario(transport string, originSteps, localSteps, after []Step) *Scenario {
	exit := 0
	return &Scenario{
		Name: "t",
		Input: Input{
			Steps:  localSteps,
			Origin: &Origin{Transport: transport, Steps: originSteps, After: after},
		},
		Runs: []Run{{Cmd: []string{"status"}, Exit: &exit}},
	}
}

func TestBuildOriginCloneOverHTTP(t *testing.T) {
	sb := buildSandbox(t, originScenario("", // default = http
		[]Step{{Write: "a.txt", Content: "v1\n"}, {Commit: "initial"}},
		nil,
		[]Step{{Write: "a.txt", Content: "v2\n"}, {Commit: "upstream change"}},
	))
	if !strings.HasPrefix(sb.OriginURL, "http://") {
		t.Fatalf("OriginURL = %q, want http://…", sb.OriginURL)
	}
	// local clone is at "initial"; origin advanced after the clone
	if log := rawGit(t, sb.LocalDir, "log", "--format=%s"); strings.Contains(log, "upstream change") {
		t.Errorf("local clone should not have post-clone upstream commits:\n%s", log)
	}
	if log := rawGit(t, sb.OriginDir, "log", "--format=%s"); !strings.Contains(log, "upstream change") {
		t.Errorf("origin missing after-steps commit:\n%s", log)
	}
	// the local repo's remote must be the http URL (gg pull/push hit the server)
	if remote := strings.TrimSpace(rawGit(t, sb.LocalDir, "remote", "get-url", "origin")); remote != sb.OriginURL {
		t.Errorf("origin url = %q, want %q", remote, sb.OriginURL)
	}
	// .gg.toml traveled via the origin's first commit
	if _, err := os.Stat(filepath.Join(sb.LocalDir, ".gg.toml")); err != nil {
		t.Error(".gg.toml not in clone")
	}
	// and the clone starts clean
	if out := strings.TrimSpace(rawGit(t, sb.LocalDir, "status", "--porcelain")); out != "" {
		t.Errorf("clone not clean:\n%s", out)
	}
}

func TestBuildOriginPathTransport(t *testing.T) {
	sb := buildSandbox(t, originScenario("path",
		[]Step{{Write: "a.txt", Content: "v1\n"}, {Commit: "initial"}},
		nil, nil,
	))
	if strings.HasPrefix(sb.OriginURL, "http") {
		t.Fatalf("OriginURL = %q, want a filesystem path", sb.OriginURL)
	}
}

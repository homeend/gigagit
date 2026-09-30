package domain

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/exttool"
)

// Like agentver_test.go, these swap the probe seam: no t.Parallel.

func fakeDet(version int, variants ...exttool.CommandTemplate) exttool.Detection {
	for i := range variants {
		variants[i].Category, variants[i].Name, variants[i].Mode, variants[i].Version = exttool.CatReview, "Fake", exttool.ModeCapture, version
	}
	return exttool.Detection{Tool: exttool.Tool{ID: "fake", Label: "Fake", VersionArgs: []string{"--version"}, Commands: variants}, Bin: "fake"}
}

func stubVersion(t *testing.T, out string) {
	t.Helper()
	resetAgentVersionCache()
	old := agentVersionRun
	agentVersionRun = func(context.Context, string, []string) ([]byte, error) {
		if out == "" {
			return nil, context.DeadlineExceeded
		}
		return []byte(out), nil
	}
	t.Cleanup(func() { agentVersionRun = old; resetAgentVersionCache() })
}

func writeBlocks(t *testing.T, blocks ...config.ToolCommand) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := config.AppendToolCommands(path, blocks); err != nil {
		t.Fatal(err)
	}
	return path
}

// userEdit simulates the user editing the file AFTER gg wrote it (writing a
// stamped block always computes a fresh fingerprint, so an "edited" block
// can only be made this way).
func userEdit(t *testing.T, path, from, to string) {
	t.Helper()
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), from) {
		t.Fatalf("userEdit: %q not in file", from)
	}
	os.WriteFile(path, []byte(strings.Replace(string(raw), from, to, 1)), 0o644)
}

func TestToolTemplateStatusTable(t *testing.T) {
	v1 := fakeDet(1, exttool.CommandTemplate{Command: "<bin> one"})
	v2 := fakeDet(2, exttool.CommandTemplate{Command: "<bin> two"})
	stamped := NewToolBlock(v1, v1.Tool.Commands[0])
	unstampedEqual := NewToolBlock(v2, v2.Tool.Commands[0])
	unstampedEqual.TemplateVersion, unstampedEqual.AgentRange = 0, ""
	unstampedDiff := unstampedEqual
	unstampedDiff.Command = "fake two --mine"

	cases := []struct {
		name   string
		block  config.ToolCommand
		edited bool // apply a user edit to the written file
		det    exttool.Detection
		want   ToolStatusKind
		edit   bool
	}{
		{"stamped current", stamped, false, v1, ToolCurrent, false},
		{"stamped unedited behind", stamped, false, v2, ToolUpdateAvailable, false},
		{"stamped edited current", stamped, true, v1, ToolCustomised, true},
		{"stamped edited behind", stamped, true, v2, ToolUpdateAvailable, true},
		{"unstamped equal to target", unstampedEqual, false, v2, ToolCurrent, true},
		{"unstamped differing", unstampedDiff, false, v2, ToolUpdateAvailable, true},
	}
	stubVersion(t, "")
	for _, c := range cases {
		path := writeBlocks(t, c.block)
		if c.edited {
			userEdit(t, path, "fake one", "fake one --mine")
		}
		sts := ToolTemplateStatuses(context.Background(), []string{path}, []exttool.Detection{c.det})
		if len(sts) != 1 || sts[0].Kind != c.want || sts[0].Edited != c.edit {
			t.Errorf("%s: got %+v", c.name, sts)
		}
	}
}

func TestToolTemplateStatusAgentRanges(t *testing.T) {
	det := fakeDet(1,
		exttool.CommandTemplate{Range: ">=2.1", Command: "<bin> new"},
		exttool.CommandTemplate{Range: ">=1.8 <2.1", Command: "<bin> old"})
	oldBlock := NewToolBlock(det, det.Tool.Commands[1]) // written for <2.1
	path := writeBlocks(t, oldBlock)

	stubVersion(t, "fake 2.3.0")
	st := ToolTemplateStatuses(context.Background(), []string{path}, []exttool.Detection{det})
	if len(st) != 1 || st[0].Kind != ToolUpdateAvailable || st[0].ToRange != ">=2.1" || st[0].AgentVersion != "2.3.0" {
		t.Fatalf("agent upgrade: %+v", st)
	}

	stubVersion(t, "fake 1.0.0")
	st = ToolTemplateStatuses(context.Background(), []string{path}, []exttool.Detection{det})
	if len(st) != 1 || st[0].Kind != ToolUnsupported {
		t.Fatalf("out of range: %+v", st)
	}

	stubVersion(t, "") // unknown: keep the stamped variant, no offer
	st = ToolTemplateStatuses(context.Background(), []string{path}, []exttool.Detection{det})
	if len(st) != 1 || st[0].Kind != ToolCurrent {
		t.Fatalf("unknown version: %+v", st)
	}
}

func TestToolTemplateStatusRepoShadowsGlobal(t *testing.T) {
	v2 := fakeDet(2, exttool.CommandTemplate{Command: "<bin> two"})
	v1 := fakeDet(1, exttool.CommandTemplate{Command: "<bin> one"})
	stubVersion(t, "")
	global := writeBlocks(t, NewToolBlock(v1, v1.Tool.Commands[0]))
	repo := writeBlocks(t, NewToolBlock(v2, v2.Tool.Commands[0]))
	sts := ToolTemplateStatuses(context.Background(), []string{global, repo}, []exttool.Detection{v2})
	if len(sts) != 1 || sts[0].Path != repo || sts[0].Kind != ToolCurrent {
		t.Fatalf("only the effective (repo) block may get a status: %+v", sts)
	}
}

func TestToolTemplateStatusIgnoresUserAuthored(t *testing.T) {
	stubVersion(t, "")
	path := writeBlocks(t, config.ToolCommand{Category: "review", Name: "Mine", Mode: "capture", Command: "x"})
	if sts := ToolTemplateStatuses(context.Background(), []string{path}, []exttool.Detection{fakeDet(1, exttool.CommandTemplate{Command: "<bin>"})}); len(sts) != 0 {
		t.Fatalf("a non-catalog block got a status: %+v", sts)
	}
}

func TestApplyToolUpdateWritesAndRefusesStale(t *testing.T) {
	stubVersion(t, "")
	v1 := fakeDet(1, exttool.CommandTemplate{Command: "<bin> one"})
	v2 := fakeDet(2, exttool.CommandTemplate{Command: "<bin> two"})
	path := writeBlocks(t, NewToolBlock(v1, v1.Tool.Commands[0]))
	st := ToolTemplateStatuses(context.Background(), []string{path}, []exttool.Detection{v2})[0]

	stale := st
	stale.Block.Command = "something the file no longer holds"
	if err := ApplyToolUpdate(stale); err == nil {
		t.Fatal("a block that changed since the status was computed must be refused")
	}

	raw, _ := os.ReadFile(path)
	os.WriteFile(path, []byte(string(raw)+"\n# touched\n"), 0o644) // formatting only — still allowed
	if err := ApplyToolUpdate(st); err != nil {
		t.Fatalf("formatting-only change must not block: %v", err)
	}
	got, _ := config.ToolCommandsIn(path)
	if strings.TrimSpace(got[0].Command) != "fake two" || got[0].TemplateVersion != 2 || got[0].Edited() {
		t.Fatalf("after apply: %+v", got[0])
	}
}

// Spec: the agent's --version is read only for a tool with ranged variants —
// a catalog without ranges must never spawn the agent.
func TestToolTemplateStatusNoProbeWithoutRanges(t *testing.T) {
	resetAgentVersionCache()
	calls := 0
	old := agentVersionRun
	agentVersionRun = func(context.Context, string, []string) ([]byte, error) {
		calls++
		return []byte("fake 9.9.9"), nil
	}
	t.Cleanup(func() { agentVersionRun = old; resetAgentVersionCache() })
	v1 := fakeDet(1, exttool.CommandTemplate{Command: "<bin> one"})
	path := writeBlocks(t, NewToolBlock(v1, v1.Tool.Commands[0]))
	sts := ToolTemplateStatuses(context.Background(), []string{path}, []exttool.Detection{v1})
	if len(sts) != 1 || sts[0].Kind != ToolCurrent {
		t.Fatalf("status: %+v", sts)
	}
	if calls != 0 {
		t.Fatalf("probed the agent %d time(s) with no ranged variant", calls)
	}
}

// A block the writer cannot locate is an error, never a reported success.
func TestApplyToolUpdateReportsUnlocatableBlock(t *testing.T) {
	stubVersion(t, "")
	v2 := fakeDet(2, exttool.CommandTemplate{Command: "<bin> two"})
	path := filepath.Join(t.TempDir(), "config.toml")
	// A valid, unstamped block gg's writer cannot address: its header is
	// quoted-key spelled (TOML-legal, never written by gg).
	os.WriteFile(path, []byte("[[\"tools\".\"command\"]]\ncategory = \"review\"\nname = \"Fake\"\nmode = \"capture\"\ncommand = \"fake one\"\n"), 0o644)
	sts := ToolTemplateStatuses(context.Background(), []string{path}, []exttool.Detection{v2})
	if len(sts) != 1 || sts[0].Kind != ToolUpdateAvailable {
		t.Fatalf("precondition: %+v", sts)
	}
	if err := ApplyToolUpdate(sts[0]); err == nil {
		t.Fatal("an unlocatable block must be an error")
	}
}

// A file holding the same block twice: the effective (last) one is offered
// and taking the offer rewrites it — never a "changed since" loop.
func TestApplyToolUpdateTakesTheEffectiveDuplicate(t *testing.T) {
	stubVersion(t, "")
	v2 := fakeDet(2, exttool.CommandTemplate{Command: "<bin> two"})
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte("[[tools.command]]\ncategory = \"review\"\nname = \"Fake\"\nmode = \"capture\"\ncommand = \"fake first\"\n\n[[tools.command]]\ncategory = \"review\"\nname = \"Fake\"\nmode = \"capture\"\ncommand = \"fake second\"\n"), 0o644)
	sts := ToolTemplateStatuses(context.Background(), []string{path}, []exttool.Detection{v2})
	if len(sts) != 1 || strings.TrimSpace(sts[0].Block.Command) != "fake second" {
		t.Fatalf("status must describe the effective block: %+v", sts)
	}
	if err := ApplyToolUpdate(sts[0]); err != nil {
		t.Fatalf("take: %v", err)
	}
	got, _ := config.ToolCommandsIn(path)
	if len(got) != 2 || strings.TrimSpace(got[0].Command) != "fake first" || strings.TrimSpace(got[1].Command) != "fake two" {
		t.Fatalf("after take: %+v", got)
	}
}

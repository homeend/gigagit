package domain

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/template"
)

func TestTaskModeOf(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		mode    string
		perFile bool
		want    TaskMode
		ok      bool
	}{
		{"capture", false, TaskHeadless, true},
		{"interactive", false, TaskInteractive, true},
		{"terminal", false, TaskInteractive, true},
		{"terminal", true, "", false},
		{"session", false, "", false},
	} {
		got, ok := TaskModeOf(config.ToolCommand{Mode: tc.mode, PerFile: tc.perFile})
		if got != tc.want || ok != tc.ok {
			t.Errorf("TaskModeOf(%s, perFile=%v) = %q %v", tc.mode, tc.perFile, got, ok)
		}
	}
}

func TestTaskChoicesGroupByAgent(t *testing.T) {
	t.Parallel()
	var cfg config.Config
	cfg.Tools.Command = []config.ToolCommand{
		{Category: "commit_message", Name: "Claude", Mode: "capture", Command: "claude -p x"},
		{Category: "commit_message", Name: "Claude (interactive)", Mode: "interactive", Command: "claude x"},
		{Category: "commit_message", Name: "Kimi", Mode: "capture", Command: "kimi -p x"},
		{Category: "commit_message", Name: "Mine", Mode: "capture", Command: "./my-tool"},
		{Category: "review", Name: "Claude", Mode: "capture", Command: "claude -p x"},
	}
	got := TaskChoices(cfg, exttool.CatCommitMessage, "tui")
	if len(got) != 3 {
		t.Fatalf("choices = %+v, want claude, kimi, Mine", got)
	}
	if got[0].AgentID != "claude" || got[0].Agent != "Claude Code" || len(got[0].Headless) != 1 || len(got[0].Interactive) != 1 {
		t.Errorf("claude choice = %+v", got[0])
	}
	if got[1].AgentID != "kimi" || len(got[1].Interactive) != 0 {
		t.Errorf("kimi choice = %+v", got[1])
	}
	if got[2].AgentID != "" || got[2].Agent != "Mine" {
		t.Errorf("custom choice = %+v", got[2])
	}
}

func TestCatalogueInteractiveRows(t *testing.T) {
	t.Parallel()
	want := map[string]bool{"claude": true, "codex": true, "junie": true, "antigravity": true}
	for _, tl := range exttool.Builtins() {
		for _, ct := range tl.Commands {
			if ct.Mode != exttool.ModeInteractive {
				continue
			}
			if !want[tl.ID] {
				t.Errorf("%s ships an interactive row, but it has no interactive-with-prompt mode", tl.ID)
			}
			gen := exttool.GenerateCommand(ct, tl.Bins[0])
			tc := config.ToolCommand{Category: string(ct.Category), Name: ct.Name, Mode: string(ct.Mode), Command: gen}
			if err := config.ValidateToolCommand(tc); err != nil {
				t.Errorf("%s/%s: %v", tl.ID, ct.Name, err)
			}
			if err := template.ValidateCommandTokens(gen, false); err != nil {
				t.Errorf("%s/%s tokens: %v", tl.ID, ct.Name, err)
			}
			if !strings.Contains(ct.Command, "<env:GG_MESSAGE_FILE>") || !strings.Contains(ct.Command, "wait for further instructions") {
				t.Errorf("%s/%s: an interactive prompt must write $GG_MESSAGE_FILE and then wait", tl.ID, ct.Name)
			}
		}
	}
	for id := range want {
		for _, cat := range []exttool.Category{exttool.CatCommitMessage, exttool.CatReview} {
			if !catalogueHas(id, cat, exttool.ModeInteractive, false) {
				t.Errorf("%s: no safe interactive %s row", id, cat)
			}
		}
	}
}

func catalogueHas(id string, cat exttool.Category, mode exttool.Mode, optIn bool) bool {
	for _, tl := range exttool.Builtins() {
		if tl.ID != id {
			continue
		}
		for _, ct := range tl.Commands {
			if ct.Category == cat && ct.Mode == mode && ct.OptIn == optIn {
				return true
			}
		}
	}
	return false
}

func TestEnsureInteractiveCommands(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	detect := func() []exttool.Detection {
		for _, tl := range exttool.Builtins() {
			if tl.ID == "claude" {
				return []exttool.Detection{{Tool: tl, Bin: "claude"}}
			}
		}
		return nil
	}
	var cfg config.Config
	added, err := EnsureInteractiveCommands(cfg, path, detect)
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 2 { // commit_message + review, never the OptIn yolo rows
		t.Fatalf("added = %v", added)
	}
	cfg, err = config.Load(path, "")
	if err != nil {
		t.Fatal(err)
	}
	again, _ := EnsureInteractiveCommands(cfg, path, detect)
	if len(again) != 0 {
		t.Fatalf("second run added %v, want nothing", again)
	}
}

package config

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var screenLists = ToolCommand{
	Category: "session", Name: "Claude", Mode: "session", Command: "claude",
	ScreenWorking:  []string{`^[·✢✳] [^\n]*… \(`, `Working \(\d+`},
	ScreenWaiting:  []string{`^─{8,}\n❯`, `say "hi"`},
	ScreenQuestion: []string{"🌒 \\[Y/n\\]", "tab\there"},
}

func TestScreenRuleListsRoundTrip(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := AppendToolCommands(path, []ToolCommand{screenLists}); err != nil {
		t.Fatal(err)
	}
	got, err := ToolCommandsIn(path)
	if err != nil || len(got) != 1 {
		t.Fatalf("read back: %v %+v", err, got)
	}
	for name, pair := range map[string][2][]string{
		"working":  {got[0].ScreenWorking, screenLists.ScreenWorking},
		"waiting":  {got[0].ScreenWaiting, screenLists.ScreenWaiting},
		"question": {got[0].ScreenQuestion, screenLists.ScreenQuestion},
	} {
		if !reflect.DeepEqual(pair[0], pair[1]) {
			t.Errorf("%s: got %q want %q", name, pair[0], pair[1])
		}
	}
	if !got[0].HasScreenRules() {
		t.Error("HasScreenRules = false")
	}
}

func TestBlockWithoutScreenRulesWritesNoKeys(t *testing.T) {
	t.Parallel()
	tc := ToolCommand{Category: "session", Name: "X", Mode: "session", Command: "x"}
	if out := RenderToolCommand(tc); strings.Contains(out, "screen_") {
		t.Fatalf("empty lists written:\n%s", out)
	}
	if tc.HasScreenRules() {
		t.Error("HasScreenRules = true")
	}
}

// Tuning the screen rules is not editing the template.
func TestFingerprintIgnoresScreenRules(t *testing.T) {
	t.Parallel()
	plain := screenLists
	plain.ScreenWorking, plain.ScreenWaiting, plain.ScreenQuestion = nil, nil, nil
	if ToolFingerprint(plain) != ToolFingerprint(screenLists) {
		t.Fatal("screen lists changed the fingerprint")
	}
}

// A template upgrade hands over a block without lists: the user's survive.
func TestReplaceKeepsTunedScreenRules(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := AppendToolCommands(path, []ToolCommand{screenLists}); err != nil {
		t.Fatal(err)
	}
	nb := ToolCommand{Category: "session", Name: "Claude", Mode: "session", Command: "claude --new", TemplateVersion: 2}
	if ok, err := ReplaceToolCommand(path, nb.Key(), nb); err != nil || !ok {
		t.Fatalf("replace: %v %v", ok, err)
	}
	got, _ := ToolCommandsIn(path)
	if strings.TrimSpace(got[0].Command) != "claude --new" || !reflect.DeepEqual(got[0].ScreenWaiting, screenLists.ScreenWaiting) {
		t.Fatalf("after upgrade: %+v", got[0])
	}
	// A block that brings its own lists wins.
	nb.ScreenWaiting = []string{`^READY$`}
	if ok, err := ReplaceToolCommand(path, nb.Key(), nb); err != nil || !ok {
		t.Fatalf("replace 2: %v %v", ok, err)
	}
	got, _ = ToolCommandsIn(path)
	if !reflect.DeepEqual(got[0].ScreenWaiting, []string{`^READY$`}) || len(got[0].ScreenWorking) != 0 {
		t.Fatalf("own lists: %+v", got[0])
	}
}

func TestToolsDocNamesTheScreenRuleKeys(t *testing.T) {
	t.Parallel()
	for _, d := range settingDocs {
		if d.section == "tools" && d.key == "command" {
			for _, k := range []string{"screen_working", "screen_waiting", "screen_question"} {
				if !strings.Contains(d.comment, k) {
					t.Errorf("the tools.command doc does not mention %s", k)
				}
			}
			return
		}
	}
	t.Fatal("no tools.command doc")
}

package exttool

import (
	"strings"
	"testing"
)

func TestBuiltInReviewTemplatesAskForTheDocument(t *testing.T) {
	n := 0
	for _, tl := range Builtins() {
		for _, c := range tl.Commands {
			if c.Category != CatReview {
				continue
			}
			n++
			if strings.Contains(c.Command, "/code-review") {
				t.Errorf("%s/%s still runs /code-review", tl.ID, c.Name)
			}
			if !strings.Contains(c.Command, "Review output") {
				t.Errorf("%s/%s: prompt does not point at the Review output section", tl.ID, c.Name)
			}
			if !strings.Contains(c.Command, "<env:GG_CONTEXT_FILE>") {
				t.Errorf("%s/%s: prompt does not name the review brief", tl.ID, c.Name)
			}
		}
	}
	if n == 0 {
		t.Fatal("no review templates")
	}
}

func TestEveryOldReviewTemplateUpgradesToACurrentOne(t *testing.T) {
	current := map[string]bool{}
	for _, tl := range Builtins() {
		for _, c := range tl.Commands {
			if c.Category == CatReview {
				current[c.Command] = true
			}
		}
	}
	for _, p := range SupersededReviewCommands {
		if !current[p.New] {
			t.Errorf("superseded %q upgrades to a command that is not a built-in", p.Old[:40])
		}
		if current[p.Old] {
			t.Errorf("%q is still a built-in", p.Old[:40])
		}
	}
}

func TestUpgradeReviewCommandKeepsTheBinary(t *testing.T) {
	old := strings.Replace(oldClaudeReviewCommand, "<bin>", "/usr/local/bin/claude", 1)
	got, ok := UpgradeReviewCommand(old)
	if !ok || !strings.HasPrefix(got, "/usr/local/bin/claude -p ") || strings.Contains(got, "/code-review") {
		t.Fatalf("%v %q", ok, got)
	}
	if _, ok := UpgradeReviewCommand(old + " --extra"); ok {
		t.Fatal("an edited command must not match")
	}
	if _, ok := UpgradeReviewCommand(strings.ReplaceAll(old, "\n", "\r\n")); !ok {
		t.Fatal("CRLF line ends must still match")
	}
	if _, ok := UpgradeReviewCommand("\n" + old + "\n"); !ok {
		t.Fatal("surrounding blank lines (a TOML ''' block) must still match")
	}
}

// Stored commands are GENERATED: <env:X> is rendered per OS and a binary path
// with a space is quoted. The upgrade must see through both and render the
// new command the same way.
func TestUpgradeReviewCommandMatchesGeneratedForms(t *testing.T) {
	junie := CommandTemplate{Command: oldJunieReviewCommand}
	for _, tc := range []struct{ bin, goos, env string }{
		{"junie", "linux", "${GG_MESSAGE_FILE}"},
		{`C:\Program Files\Junie\junie.exe`, "windows", "%GG_MESSAGE_FILE%"},
	} {
		stored := GenerateCommandFor(junie, tc.bin, tc.goos)
		got, ok := UpgradeReviewCommand(stored)
		if !ok {
			t.Fatalf("%s: generated old command not recognised:\n%s", tc.goos, stored)
		}
		want := GenerateCommandFor(CommandTemplate{Command: junieReviewCommand}, tc.bin, tc.goos)
		if got != want {
			t.Fatalf("%s:\n got %q\nwant %q", tc.goos, got, want)
		}
		if !strings.Contains(got, tc.env) || !strings.Contains(got, "Review output") {
			t.Fatalf("%s: %q", tc.goos, got)
		}
	}
	if _, ok := UpgradeReviewCommand(GenerateCommandFor(CommandTemplate{Command: junieReviewCommand}, "junie", "linux")); ok {
		t.Fatal("a current command is not superseded")
	}
}

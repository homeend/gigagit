package e2e

import (
	"strings"
	"testing"
)

const tuiHead = "name = \"x\"\n[input]\nsteps = [{ write = \"a\", content = \"a\\n\" }, { commit = \"c\" }]\n"

func TestParseTUIBlock(t *testing.T) {
	t.Parallel()
	sc, err := parseScenario([]byte(tuiHead+`
[tui]
size = "100x30"
[[tui.step]]
name = "commits"
keys = ["right"]
screen_excludes = ["◆ 1"]
[[tui.step]]
keys = ["down"]
wait = true
`), "x.toml")
	if err != nil {
		t.Fatal(err)
	}
	if sc.TUI == nil || sc.TUI.Size != "100x30" || len(sc.TUI.Steps) != 2 || sc.TUI.Steps[0].Name != "commits" || !sc.TUI.Steps[1].Wait {
		t.Fatalf("parsed %+v", sc.TUI)
	}
}

func TestTUIValidation(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{
		"[tui]\nsize = \"wide\"\n[[tui.step]]\nkeys = [\"down\"]\n",                                       // bad size
		"[tui]\n[[tui.step]]\nname = \"A B\"\nkeys = [\"down\"]\n",                                        // bad name
		"[tui]\n[[tui.step]]\nname = \"a\"\nkeys = [\"x\"]\n[[tui.step]]\nname = \"a\"\nkeys = [\"y\"]\n", // duplicate
		"[tui]\n[[tui.step]]\nname = \"a\"\n",                                                             // no keys, no wait
		"[tui]\n",                                                                                         // no steps
	} {
		if _, err := parseScenario([]byte(tuiHead+bad), "x.toml"); err == nil {
			t.Errorf("want an error for:\n%s", bad)
		}
	}
}

func TestRootPlaceholderKeepsWidth(t *testing.T) {
	t.Parallel()
	root := "/tmp/gg-tui/abc"
	in := "x " + root + "/local y"
	got := normalizeRoot(in, root)
	if !strings.Contains(got, "{{root}}") || len([]rune(got)) != len([]rune(in)) {
		t.Fatalf("normalizeRoot = %q (width must not change)", got)
	}
}

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

// Two e2e processes never share a TUI sandbox root (each RemoveAlls its own
// on entry), yet the root's width — what a golden's layout depends on — is
// the same for every process (final review, Important 3).
func TestTUIRootIsPerProcessAndFixedWidth(t *testing.T) {
	t.Parallel()
	a, b := tuiRoot(1, "s"), tuiRoot(99999999, "s")
	if a == b {
		t.Fatal("two processes must get different roots")
	}
	if len([]rune(a)) != len([]rune(b)) {
		t.Fatalf("root width varies with the pid: %q vs %q", a, b)
	}
}

// A path cell elided through the root still shows the pid segment; it is
// masked at the same width so every process renders the same golden.
func TestRootPlaceholderMasksACutRoot(t *testing.T) {
	t.Parallel()
	root := tuiRoot(2826841, "tui_smoke")
	cut := "* main (" + root[:len(root)-6] + "… ││"
	got := normalizeRoot(cut, root)
	if strings.Contains(got, "2826841") || len([]rune(got)) != len([]rune(cut)) {
		t.Fatalf("normalizeRoot(cut) = %q: the pid must be masked at the same width", got)
	}
}

// -update on Windows would write goldens with Windows paths (and goldens
// are never compared there): it is refused, everywhere else allowed.
func TestUpdateRefusedOnWindows(t *testing.T) {
	if err := updateRefused("windows"); err == nil {
		t.Error("-update on windows must be refused")
	}
	for _, goos := range []string{"linux", "darwin"} {
		if err := updateRefused(goos); err != nil {
			t.Errorf("-update on %s refused: %v", goos, err)
		}
	}
}

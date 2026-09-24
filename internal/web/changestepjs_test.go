package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Change stepping reasons from the VIEWPORT, the TUI's reseatFromViewport:
// when the change last stepped to is off screen (a free scroll moved away
// from it) › lands on the first change at or below the pane's top and ‹ on
// the last one above it; on screen, the step is the shipped ±1 (clamped —
// the web does not wrap). tops are each change's first-row top, the pane is
// [100, 500).
const changeStepHarness = `
import { changeStepTarget } from "./changestep.mjs";
const tops = [-900, -300, 120, 700, 1500];
console.log(JSON.stringify({
  onScreenNext: changeStepTarget(tops, 2, 100, 500, 1),
  onScreenPrev: changeStepTarget(tops, 2, 100, 500, -1),
  offNext: changeStepTarget(tops, 0, 100, 500, 1),
  offPrev: changeStepTarget(tops, 4, 100, 500, -1),
  gapNext: changeStepTarget([-900, -300, 700, 1500], 0, 100, 500, 1),
  gapPrev: changeStepTarget([-900, -300, 700, 1500], 0, 100, 500, -1),
  nothingBelow: changeStepTarget([-900, -300], 0, 100, 500, 1),
  nothingAbove: changeStepTarget([700, 1500], 1, 100, 500, -1),
  unsteppedNext: changeStepTarget([150, 700], -1, 100, 500, 1),
  unsteppedScrolled: changeStepTarget([-150, 700], -1, 100, 500, 1),
  staleIndex: changeStepTarget([150, 700], 9, 100, 500, 1),
  lastOnScreen: changeStepTarget([-300, 150], 1, 100, 500, 1),
  none: changeStepTarget([], 0, 100, 500, 1),
}));
`

func TestChangeStepTargetFollowsTheViewport(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	mod := jsFunc(t, "files.js", "changeStepTarget") + "\nexport { changeStepTarget };\n"
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "changestep.mjs"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(changeStepHarness), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, filepath.Join(dir, "run.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got map[string]int
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &got); err != nil {
		t.Fatalf("not the harness JSON: %v\n%s", err, out)
	}
	want := map[string]int{
		"onScreenNext":      3,  // on screen: the plain step
		"onScreenPrev":      1,  //   (even to a change off screen)
		"offNext":           2,  // off screen: the first change at/below the top
		"offPrev":           1,  //   the last one above it
		"gapNext":           2,  // nothing on screen: the next one down
		"gapPrev":           1,  //   the nearest one up
		"nothingBelow":      1,  // no wrap: the last change
		"nothingAbove":      0,  //   the first change
		"unsteppedNext":     0,  // never stepped: the first change on screen
		"unsteppedScrolled": 1,  //   below the top, not the one above it
		"staleIndex":        0,  // an index past the end re-seats
		"lastOnScreen":      1,  // on the last change: stays (clamped)
		"none":              -1, // no changes at all
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s: got %d, want %d", k, got[k], w)
		}
	}
}

// The change step has keys: , and . (the TUI's p/n — p is pull here and -
// folds a stacked file), in the diff layout only, advertised in help and on
// the footer.
func TestChangeStepKeysWired(t *testing.T) {
	t.Parallel()
	keys := readStatic(t, "keys.js")
	for _, want := range []string{`e.key === ","`, `e.key === "."`, "stepChange(e.key === \".\" ? 1 : -1)", `case "nextchange": stepChange(1); break;`} {
		if !strings.Contains(keys, want) {
			t.Errorf("keys.js is missing %q", want)
		}
	}
	html := readStatic(t, "index.html")
	for _, want := range []string{`data-act="nextchange"`, `<span class="hkey">, / .</span>`} {
		if !strings.Contains(html, want) {
			t.Errorf("index.html is missing %q", want)
		}
	}
	if !strings.Contains(readStatic(t, "files.js"), "changeStepTarget(") {
		t.Error("stepChange does not go through changeStepTarget")
	}
}

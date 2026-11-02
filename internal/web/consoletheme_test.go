package web

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// A basic-colour foreground on the default background takes the console's own
// palette slot; a pairing the program chose itself (an explicit background, a
// reverse run) and any other colour keep the program's value.
func TestConsoleRunTakesTheTerminalPalette(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "console.js", consolePureStart, consolePureEnd, activitySection(t)+`
const r = [runHTML({ t: "x", fg: "#800000" })];
setTermPalette(["#000000", "#800000", "#008000", "#808000", "#000080"]);
r.push(runHTML({ t: "x", fg: "#800000" }));
r.push(runHTML({ t: "x", fg: "#000080", b: true }));
r.push(runHTML({ t: "x", fg: "#000000" }));
r.push(runHTML({ t: "x", fg: "#123456" }));
r.push(runHTML({ t: "x", fg: "#000080", bg: "#c0c0c0" }));
r.push(runHTML({ t: "x", fg: "#000080", r: true }));
r.push(runHTML({ t: "x", bg: "#000080" }));
console.log(r.join("|"));
`)
	want := `<span style="color:#800000">x</span>|` + // no hello yet: the program's value
		`<span style="color:var(--term-1)">x</span>|` +
		`<span style="color:var(--term-4)" class="b">x</span>|` +
		`<span style="color:var(--term-0)">x</span>|` +
		`<span style="color:#123456">x</span>|` +
		`<span style="color:#000080;background:#c0c0c0">x</span>|` +
		`<span style="color:#000080" class="r">x</span>|` +
		`<span style="background:#000080">x</span>`
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

// The palette arrives with hello, before the first frame is folded into rows.
func TestConsoleHelloSetsThePaletteBeforeTheFirstFrame(t *testing.T) {
	t.Parallel()
	src := readStatic(t, "console.js")
	set, fold := strings.Index(src, "setTermPalette(h.palette)"), strings.Index(src, "applyFrame([], h.frame)")
	if set < 0 || fold < 0 || set > fold {
		t.Fatalf("hello must set the palette before folding its frame (set %d, fold %d)", set, fold)
	}
}

func luminance(t *testing.T, hex string) float64 {
	t.Helper()
	v, err := strconv.ParseUint(strings.TrimPrefix(hex, "#"), 16, 32)
	if err != nil || len(hex) != 7 {
		t.Fatalf("colour %q: %v", hex, err)
	}
	ch := func(shift uint) float64 {
		c := float64(v>>shift&0xff) / 255
		if c <= 0.03928 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(16) + 0.7152*ch(8) + 0.0722*ch(0)
}

// Every slot is readable (WCAG AA) on the panel the console sits on, and the
// sub-theme stays scoped to the console grid.
func TestConsolePaletteIsReadableOnTheConsoleBackground(t *testing.T) {
	t.Parallel()
	css := readStatic(t, "style.css")
	bgm := regexp.MustCompile(`--bg-alt: (#[0-9a-fA-F]{6})`).FindStringSubmatch(css)
	if bgm == nil {
		t.Fatal("no --bg-alt in style.css")
	}
	if !strings.Contains(css, "#console { position: fixed; background: var(--bg-alt);") {
		t.Fatal("the console no longer sits on --bg-alt: re-pin this test")
	}
	rule := regexp.MustCompile(`(?s)#console-grid \{\s*/\* terminal palette.*?\}`).FindString(css)
	if rule == "" {
		t.Fatal("no terminal palette rule on #console-grid")
	}
	bg := luminance(t, bgm[1])
	slots := regexp.MustCompile(`--term-(\d+): (#[0-9a-fA-F]{6})`).FindAllStringSubmatch(rule, -1)
	if len(slots) != 16 {
		t.Fatalf("%d palette slots, want 16", len(slots))
	}
	for i, s := range slots {
		if s[1] != strconv.Itoa(i) {
			t.Errorf("slot %d is --term-%s", i, s[1])
		}
		ratio := (luminance(t, s[2]) + 0.05) / (bg + 0.05)
		if ratio < 4.5 {
			t.Errorf("--term-%s %s contrast %.2f on %s, want at least 4.5", s[1], s[2], ratio, bgm[1])
		}
	}
	if n := strings.Count(css+readStatic(t, "polish.css"), "--term-0:"); n != 1 {
		t.Errorf("--term-0 defined %d times: the sub-theme has one home", n)
	}
}

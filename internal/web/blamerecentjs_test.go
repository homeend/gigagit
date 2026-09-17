package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The blame overlay's recent-lines highlight (d asks for a span, D turns it
// off) touches markup, CSS, the state literal and the overlay module; a
// missed edit half-works silently (rows stamped but never tinted, a key that
// opens nothing, a tint the sticky gutter paints over). This pins the wiring;
// timespanjs_test.go pins the span grammar against the Go package.
func TestBlameRecentIsWiredEverywhere(t *testing.T) {
	t.Parallel()
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("static", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	checks := []struct{ file, want, why string }{
		{"filehist.js", `data-t="${Number(l.time) || 0}"`, "every row must carry its author time so the tint re-applies without a refetch"},
		{"filehist.js", `' data-u="1"'`, "an uncommitted row is the newest change of all and must be marked"},
		{"filehist.js", `function applyBlameRecent(`, "the one put-it-in-this-state step"},
		{"filehist.js", `classList.toggle("brecent"`, "the class the CSS keys on"},
		{"filehist.js", `now - t * 1000 <= br.span * 60000`, "the recency rule: boundary inclusive, span in minutes"},
		{"filehist.js", `" · ≤" + formatSpan(br.span)`, "the title badge while on"},
		{"filehist.js", `e.key === "d"`, "d opens the span prompt"},
		{"filehist.js", `e.key === "D"`, "D turns the highlight off"},
		{"filehist.js", `key: "d / D · blame recent lines"`, "the ? help must advertise the keys, not only the overlay's hint line"},
		{"filehist.js", `onKey: blameKey`, "the blame layer must route keys through the named handler"},
		{"filehist.js", `state.blameRecent = { on: true, span: mins, last: text }`, "a parsed span turns the highlight on and is remembered for the next prompt"},
		{"filehist.js", `openBlameRecentPrompt(text, msg)`, "a bad span re-opens the prompt prefilled with the offending text"},
		{"filehist.js", `"Highlight lines changed within the last…"`, "the spec's dialog title"},
		{"filehist.js", `"e.g. 7d, 1d 3h 5m, 36h, 90m"`, "the spec's placeholder"},
		{"filehist.js", `openPrompt, pushLayer } from "./layers.js"`, "the prompt is the shared box, pushed on top of the blame layer"},
		{"core.js", `blameRecent: { on: false, span: 0, last: "7d" }`, "the session-only state literal, prefilling 7d"},
		{"index.html", `· d recent lines · D off ·`, "the overlay's hint advertises both keys"},
		{"style.css", `--recent-bg:`, "the tint colour lives next to the other theme tokens"},
		{"style.css", `#blame-body .bline.brecent { background: var(--recent-bg`, "the tint, scoped under the blame body"},
		{"style.css", `body.lm-scroll #blame-body .bline.brecent .bgut`, "scroll mode's sticky gutter paints --bg-alt and would hide the tint"},
	}
	for _, c := range checks {
		if !strings.Contains(read(c.file), c.want) {
			t.Errorf("%s: missing %q — %s", c.file, c.want, c.why)
		}
	}
	// Session-only, by ruling: the span never reaches the stored UI state.
	if ui := read("uistate.js"); strings.Contains(ui, "blame_recent") || strings.Contains(ui, "blameRecent") {
		t.Errorf("uistate.js: the blame recent-lines span is session-only and must not be persisted")
	}
	// The d/D handler must be the blame layer's, not the history overlay's:
	// both live in filehist.js, and the history layer keeps its own historyKey.
	fh := read("filehist.js")
	i := strings.Index(fh, "function blameKey(")
	j := strings.Index(fh, `pushLayer("blame"`)
	if i < 0 || j < 0 {
		t.Fatal("filehist.js: blameKey and the blame layer's pushLayer must both exist")
	}
	if strings.Contains(fh, `pushLayer("history", $("history"), { onKey: blameKey })`) {
		t.Error("filehist.js: the history layer must keep historyKey, not blameKey")
	}
}

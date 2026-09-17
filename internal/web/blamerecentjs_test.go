package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The blame overlay's age highlight (d asks for a signed age filter, D turns
// it off) touches markup, CSS, the state literal and the overlay module; a
// missed edit half-works silently (rows stamped but never tinted, a key that
// opens nothing, a tint the sticky gutter paints over, a highlight that
// survives a reopen). This pins the wiring; timespanjs_test.go pins the
// span and filter grammars against the Go package.
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
		{"filehist.js", `function parseFilter(`, "the signed-grammar port the prompt parses with"},
		{"filehist.js", `function filterMatches(`, "the one age rule (inclusive both sides, uncommitted = age 0)"},
		{"filehist.js", `filterMatches(br.f, `, "rows are tinted through the shared matcher, not an inline comparison"},
		{"filehist.js", `" · " + formatFilter(br.f)`, "the title badge while on: the canonical signed form, no ≤"},
		{"filehist.js", `e.key === "d"`, "d opens the age prompt"},
		{"filehist.js", `e.key === "D"`, "D turns the highlight off"},
		{"filehist.js", `key: "d / D · blame age highlight"`, "the ? help must advertise the keys, not only the overlay's hint line"},
		{"filehist.js", `onKey: blameKey`, "the blame layer must route keys through the named handler"},
		{"filehist.js", `state.blameRecent = { on: true, f, last: text }`, "a parsed filter turns the highlight on and its text is remembered for the next prompt"},
		{"filehist.js", `openBlameRecentPrompt(text, msg)`, "a bad filter re-opens the prompt prefilled with the offending text"},
		{"filehist.js", `"not an age filter: “" + text + "”`, "the error names the grammar, not a bare span"},
		{"filehist.js", `"Highlight lines by age…"`, "the spec's dialog title"},
		{"filehist.js", `"-7d younger · +30d older · +1d -7d between · +w"`, "the spec's placeholder: the signed grammar at a glance"},
		{"filehist.js", `openPrompt, pushLayer } from "./layers.js"`, "the prompt is the shared box, pushed on top of the blame layer"},
		{"core.js", `blameRecent: { on: false, f: null, last: "7d" }`, "the session-only state literal, prefilling 7d"},
		{"index.html", `· d age highlight · D off ·`, "the overlay's hint advertises both keys"},
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
	// Off on open (revision 2): opening blame resets the highlight BEFORE the
	// first apply, so a reopened overlay never inherits the last one's tint.
	// The same reset also lives in the D handler, so scope the search to
	// openFileBlame's body (up to its pushLayer).
	o := strings.Index(fh, "function openFileBlame(")
	if o < 0 || o > j {
		t.Fatal("filehist.js: openFileBlame must exist and push the blame layer")
	}
	open := fh[o:j]
	reset := strings.Index(open, "state.blameRecent.on = false")
	apply := strings.Index(open, "applyBlameRecent()")
	if reset < 0 || apply < 0 || reset > apply {
		t.Errorf("filehist.js: openFileBlame must set state.blameRecent.on = false before applyBlameRecent() (reset=%d apply=%d)", reset, apply)
	}
	// The ≤ badge is revision 1: the signed form ("+1d -7d") replaces it, so
	// nothing in the blame section may print or describe it.
	b := strings.Index(fh, "// --- blame overlay")
	if b < 0 {
		t.Fatal("filehist.js: the blame overlay section marker is gone")
	}
	if strings.Contains(fh[b:], "≤") {
		t.Error("filehist.js: the blame section still carries a ≤ badge; the title shows formatFilter's signed form")
	}
}

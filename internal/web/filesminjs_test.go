package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The file-list minimize control and the sticky back bar touch markup, CSS,
// three modules and the stored layout; a missed edit half-works silently
// (a toggle that forgets itself on restart, a strip with no way back).
func TestFilesMinimizeIsWiredEverywhere(t *testing.T) {
	t.Parallel()
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("static", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	checks := []struct{ file, want, why string }{
		{"index.html", `id="back-bar"`, "the back button and the minimize control share one sticky bar"},
		{"index.html", `id="files-min"`, "the minimize/restore control"},
		{"index.html", `id="files-top"`, "the bar and the commit header share ONE sticky wrapper"},
		{"style.css", `#files-top { position: sticky;`, "the wrapper must stay on screen while the list scrolls"},
		{"style.css", `#files-top #files-header { position: static; }`, "two sticky siblings both pin at 0 and overlap"},
		{"style.css", `#panes.nofiles`, "the minimized layout variant"},
		{"style.css", `#panes.files.nofiles`, "the files stage must have a minimized template"},
		{"style.css", `#panes.detail.nofiles`, "the diff stage must have a minimized template"},
		{"style.css", `#panes.nofiles #rs-detail { display: none; }`, "no drag handle for a strip"},
		{"files.js", `function applyFilesHidden(`, "the shared put-it-in-this-state step"},
		{"files.js", `function toggleFilesHidden(`, "the user-facing flip"},
		{"files.js", `saveUI({ files_hidden:`, "the flip must persist (random port: localStorage is useless)"},
		{"files.js", `classList.toggle("nofiles"`, "the class the CSS keys on"},
		{"app.js", `applyFilesHidden(true)`, "boot must restore the stored state"},
		{"uistate.js", `files_hidden: false`, "saveUI's base must carry the field — the endpoint REPLACES the record"},
		{"palette.js", `"toggle file list"`, "the ☰ UI group offers the same switch"},
	}
	for _, c := range checks {
		if !strings.Contains(read(c.file), c.want) {
			t.Errorf("%s: missing %q — %s", c.file, c.want, c.why)
		}
	}
	// The ☰ UI group is alphabetical (a user ruling); the new row must not
	// break it.
	pal := read("palette.js")
	i := strings.Index(pal, `{ header: "UI" }`)
	j := strings.Index(pal[i:], `{ header: "Config" }`)
	if i < 0 || j < 0 {
		t.Fatal("palette.js: the UI group must sit before Config")
	}
	var labels []string
	for _, line := range strings.Split(pal[i:i+j], "\n") {
		if k := strings.Index(line, `label: "`); k >= 0 {
			rest := line[k+len(`label: "`):]
			labels = append(labels, rest[:strings.Index(rest, `"`)])
		}
	}
	for k := 1; k < len(labels); k++ {
		if labels[k-1] > labels[k] {
			t.Errorf("palette.js: UI group not alphabetical at %q > %q", labels[k-1], labels[k])
		}
	}
}

// Every long-line viewer wraps: the diff table already did; blame drew
// `white-space: pre` and scrolled seven thousand pixels sideways on a
// laptop. The diff toolbar wraps too, or its seven buttons scroll the whole
// pane sideways on a narrow window and the wrapped table LOOKS unwrapped.
func TestLongLineViewersWrap(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "style.css"))
	if err != nil {
		t.Fatal(err)
	}
	css := string(b)
	for _, want := range []string{
		`.bline { display: flex; white-space: pre-wrap; }`,
		`.btext { flex: 1; min-width: 0; overflow-wrap: anywhere; }`,
		`#diff-header { display: flex; flex-wrap: wrap;`,
		`#diff-nav { flex: 0 1 auto; display: flex; flex-wrap: wrap;`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("style.css: missing %q", want)
		}
	}
}

// The changes-only toggle and the fold rows agree: unfolding the last run IS
// the full file (the chip flips off), and flipping the chip back on starts
// from every run folded again — in the diff pane and the history overlay.
func TestChangesOnlyAndFoldsAgree(t *testing.T) {
	t.Parallel()
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("static", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	checks := []struct{ file, want, why string }{
		{"files.js", `if (on) state.diffFolds = new Set();`, "flipping ON must forget the runs opened before the flip OFF"},
		{"files.js", `function toggleDiffView(keepScroll = false)`, "an unfold-all flip must keep the scroll position"},
		{"files.js", `if (!$("diff-body").querySelector("tr.fold")) toggleDiffView(true);`, "unfolding the last run flips the chip off"},
		{"filehist.js", `if (state.diffPartial) hist.folds = new Set();`, "the overlay's f follows the same rule"},
		{"filehist.js", `if (!$("history-diff").querySelector("tr.fold")) toggleDiffView(true);`, "the overlay's last unfold flips the shared chip off"},
	}
	for _, c := range checks {
		if !strings.Contains(read(c.file), c.want) {
			t.Errorf("%s: missing %q — %s", c.file, c.want, c.why)
		}
	}
}

// The long-line mode (the TUI's ctrl+w) is one switch for three viewers and
// a stored preference; every hop is a place it can silently half-work.
func TestTextModeIsWiredEverywhere(t *testing.T) {
	t.Parallel()
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("static", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	checks := []struct{ file, want, why string }{
		{"index.html", `id="diff-mode"`, "the toolbar chip"},
		{"index.html", `data-act="textmode"`, "the footer chip"},
		{"index.html", `w long lines: scroll / wrap / cutoff`, "both overlays' hints name the key"},
		{"style.css", `body.lm-scroll table.diff td.side { white-space: pre; overflow: hidden; }`, "scroll mode clips each side cell"},
		{"style.css", `body.lm-scroll table.diff td.side.l > .pan { transform: translateX(calc(-1 * var(--pan-l, 0px))); }`, "the old side pans on its own variable"},
		{"files.js", `function mountPanBars(host, bars)`, "the per-side scrollbars"},
		{"files.js", `<span class="pan">`, "side cells wrap their content for the pan"},
		{"index.html", `id="diff-hbars"`, "the diff pane's bars element"},
		{"style.css", `#diff-header { display: flex; flex-wrap: wrap; align-items: center; gap: 4px 8px; z-index: 2; }`, "the sticky header must stack above transformed (panned) cells"},
		{"filehist.js", `mountPanBars(host, bars);`, "the history overlay mounts its own bars"},
		{"style.css", `body.lm-cut table.diff td.side { white-space: pre; overflow: hidden; text-overflow: ellipsis; }`, "cutoff mode ellipsizes"},
		{"style.css", `body.lm-scroll .bline { white-space: pre; width: max-content; min-width: 100%; }`, "blame scroll mode"},
		{"style.css", `body.lm-cut .btext { overflow: hidden; text-overflow: ellipsis; }`, "blame cutoff mode"},
		{"files.js", `const TEXT_MODES = ["scroll", "wrap", "cutoff"];`, "the TUI's ctrl+w order"},
		{"files.js", `saveUI({ text_mode: state.textMode });`, "the cycle must persist"},
		{"keys.js", `cycleTextMode(); // the TUI's ctrl+w`, "the w key"},
		{"keys.js", `case "textmode": cycleTextMode(); break;`, "the footer chip acts"},
		{"filehist.js", "if (e.key === \"w\" && !e.ctrlKey && !e.metaKey && !e.altKey) {\n    cycleTextMode();", "w inside the history overlay"},
		{"filehist.js", `pushLayer("blame", $("blame"), { onKey: (e) => { if (e.key === "w"`, "w inside the blame overlay"},
		{"app.js", `applyTextMode(ui.text_mode)`, "boot restores the stored mode"},
		{"uistate.js", `text_mode: "wrap"`, "saveUI's base must carry the field — the endpoint REPLACES the record"},
	}
	for _, c := range checks {
		if !strings.Contains(read(c.file), c.want) {
			t.Errorf("%s: missing %q — %s", c.file, c.want, c.why)
		}
	}
}

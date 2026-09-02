package web

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The settings panel's button path (setOpt) used to re-render only the
// panel: a show_graph or commit_sort click was stored and then invisible on
// the page that had just asked for it (the 2026-09-02 audit). These pins
// keep the two web-consumed keys wired to the page they change, and keep
// the captions honest about which keys the web itself does NOT consume.
func TestSettingsJSAppliesWebConsumedKeys(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join("static", "settings.js"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	for _, want := range []string{
		"applyGraphMode(", // show_graph must redraw the open page
		"loadCommits(",    // commit_sort must re-fetch the feed the server just reset
	} {
		if !strings.Contains(s, want) {
			t.Errorf("settings.js: %s is never called — the setting is stored but the page does not change", want)
		}
	}
	// remote_tags_auto has no web consumer (▲ markers are a TUI surface): its
	// row must carry the same (TUI) caption as the operation log.
	row := regexp.MustCompile(`(?m)^.*toggleBtn\("remote_tags_auto".*$`).FindString(s)
	if row == "" {
		t.Fatal("settings.js: remote_tags_auto row not found")
	}
	if !strings.Contains(row, `class="stui"`) {
		t.Errorf("settings.js: remote_tags_auto row lacks the (TUI) caption: %s", row)
	}
	// Intervals below the hub's floor are stored as typed but tick at the
	// floor; the note under the rates must say so.
	if !strings.Contains(s, "min 10") {
		t.Error("settings.js: the intervals note does not mention the 10 s floor")
	}
}

package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/buildinfo"
	"github.com/homeend/gigagit/internal/i18n"
)

func TestPaletteVersionShowsBuildAndBinary(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m, _ = m.openCommandPalette()
	var row *paletteCommand
	for _, c := range layerOf[*commandPalette](m).cmds {
		if c.label == i18n.T("Version") {
			row = &c
		}
	}
	if row == nil {
		t.Fatal("no Version row in the palette")
	}
	m, _ = row.run(m)
	if layerOf[*commandPalette](m) != nil {
		t.Fatal("the palette stays open under the popup")
	}
	cp := layerOf[*contentPopup](m)
	if cp == nil {
		t.Fatal("no version popup")
	}
	var text strings.Builder
	for _, l := range cp.lines {
		text.WriteString(l.text + "\n")
		if !l.src || l.raw != l.text {
			t.Errorf("line %q cannot be copied", l.text)
		}
	}
	in := buildinfo.Current()
	for _, w := range []string{in.Version, in.Commit, in.Built, in.Path, in.Platform} {
		if !strings.Contains(text.String(), w) {
			t.Errorf("popup lacks %q:\n%s", w, text.String())
		}
	}
}

func TestPaletteFullNameLeadsTheFilter(t *testing.T) {
	t.Parallel()
	p := &commandPalette{cmds: paletteCommands(), query: "version"}
	got := p.visible()
	if len(got) != 2 || got[0].label != i18n.T("Version") || got[1].label != i18n.T("Branch versions…") {
		t.Fatalf("visible = %+v", got)
	}
}

func TestVersionLinesSayWhenTheTimeIsTheFiles(t *testing.T) {
	t.Parallel()
	in := buildinfo.Info{Version: "v1", Commit: "abc", Built: "2026-11-02 13:00:00 +0100", BuiltFromFile: true, Path: "/x/gg", Platform: "linux/amd64"}
	if joined := joinContentLines(versionLines(in)); !strings.Contains(joined, i18n.T("(binary file time)")) {
		t.Fatalf("no file-time mark:\n%s", joined)
	}
	in.BuiltFromFile = false
	if joined := joinContentLines(versionLines(in)); strings.Contains(joined, i18n.T("(binary file time)")) {
		t.Fatalf("file-time mark on a stamped build:\n%s", joined)
	}
}

func joinContentLines(ls []contentLine) string {
	var b strings.Builder
	for _, l := range ls {
		b.WriteString(l.text + "\n")
	}
	return b.String()
}

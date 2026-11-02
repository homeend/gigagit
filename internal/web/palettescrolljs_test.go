package web

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The list scrolls and the keys move the selected row, so every key that moves
// it has to bring it into view: past the visible rows the selection was simply
// gone. Geometry belongs to a browser; this pins that each path asks for it.
func TestPaletteKeysBringTheSelectedRowIntoView(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	mod := `
let revealed = 0, opts = null;
const POPUP_PAGE = 10;
const list = { querySelector: (q) => q === "li.sel" ? { scrollIntoView: (o) => { revealed++; opts = o; } } : null };
const $ = (id) => id === "palette-list" ? list : { value: "" };
const renderPalette = () => {};
const closePalette = () => {};
const runPaletteRow = () => {};
let pal = { mode: "cmd", sel: 0, filtered: Array.from({ length: 40 }, (_, i) => ({ label: "r" + i })) };
const press = (key) => { revealed = 0; paletteKey({ key, preventDefault() {} }); return [pal.sel, revealed]; };
` + jsFunc(t, "palette.js", "revealPaletteSel") + jsFunc(t, "palette.js", "paletteKey") + `
console.log(JSON.stringify([press("ArrowDown"), press("PageDown"), press("ArrowUp"), press("PageUp"), opts.block]));
`
	p := filepath.Join(t.TempDir(), "m.mjs")
	if err := os.WriteFile(p, []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, p).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	want := `[[1,1],[11,1],[10,1],[0,1],"nearest"]`
	if got := strings.TrimSpace(string(out)); got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

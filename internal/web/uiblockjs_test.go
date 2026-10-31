package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func uiStatic(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("static", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestUIBlockJSBootWaitsForActive(t *testing.T) {
	t.Parallel()
	app := uiStatic(t, "app.js")
	boot := app[strings.Index(app, "async function boot()"):]
	wait, gate := strings.Index(boot, "await uiWaitActive()"), strings.Index(boot, "preflightGate()")
	if wait < 0 || gate < 0 || wait > gate {
		t.Fatalf("boot must wait for the active flag before anything touches the repo (wait %d, gate %d)", wait, gate)
	}
}

func TestUIBlockJSLiveTakesTheFlipFirst(t *testing.T) {
	t.Parallel()
	live := uiStatic(t, "live.js")
	flip := strings.Index(live, `msg.reason === "ui_active"`)
	blocked := strings.Index(live, "if (state.uiBlocked")
	hello := strings.Index(live, `if (msg.reason === "hello")`)
	if flip < 0 || blocked < 0 || flip > blocked || blocked > hello {
		t.Fatalf("flip %d, blocked drop %d, hello %d: the flip first, then the drop, before anything refreshes", flip, blocked, hello)
	}
}

func TestUIBlockJSFlushDropsWhileBlocked(t *testing.T) {
	t.Parallel()
	live := uiStatic(t, "live.js")
	flush := live[strings.Index(live, "function flush() {"):]
	guard := strings.Index(flush, "if (state.uiBlocked) {")
	if guard < 0 || guard > strings.Index(flush, "isServerDown()") {
		t.Fatal("a refresh armed before the flip must not reach a blocked server")
	}
}

func TestUIBlockJSCommandsHostedOnly(t *testing.T) {
	t.Parallel()
	pal := uiStatic(t, "palette.js")
	if n := strings.Count(pal, `hostedOnly({ label: "Switch to TUI"`); n != 2 {
		t.Fatalf("Switch to TUI rows gated by hostedOnly: %d, want palette + ☰", n)
	}
	if !strings.Contains(uiStatic(t, "uiblock.js"), "return state.hosted ? [row] : [];") {
		t.Fatal("hostedOnly must gate on state.hosted")
	}
}

func TestUIBlockJSKeysDeaf(t *testing.T) {
	t.Parallel()
	keys := uiStatic(t, "keys.js")
	at := strings.Index(keys, "if (state.uiBlocked)")
	top := strings.Index(keys, "if (top) {\n    if (WINDOW_LAYERS")
	if at < 0 || top < 0 || at < top {
		t.Fatalf("after the layer stack (the quit confirm), before the page's keys: guard %d, stack %d", at, top)
	}
}

func TestUIBlockJSCopy(t *testing.T) {
	t.Parallel()
	js := uiStatic(t, "uiblock.js")
	if !strings.Contains(js, "Switch to the TUI? This page stays blocked until you switch back.") {
		t.Fatal("the confirmation's exact text")
	}
	html := uiStatic(t, "index.html")
	for _, want := range []string{`id="ui-block"`, "TUI is active", `id="ui-block-activate"`, "Activate web UI", `id="ui-block-quit"`, `id="ui-block-err"`} {
		if !strings.Contains(html, want) {
			t.Errorf("index.html: no %q", want)
		}
	}
	if !strings.Contains(uiStatic(t, "style.css"), "#ui-block.hidden { display: none; }") {
		t.Error("style.css: no #ui-block.hidden rule")
	}
}

// The notice must sit under the quit confirm (modal 30) and over everything a
// switch could leave behind.
func TestUIBlockCSSBelowTheModal(t *testing.T) {
	t.Parallel()
	if !strings.Contains(uiStatic(t, "style.css"), "#ui-block { position: fixed; inset: 0; z-index: 29;") {
		t.Fatal("#ui-block z-index")
	}
}

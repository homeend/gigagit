package web

import (
	"strings"
	"testing"
)

// A TUI-hosted page keeps its four repo-switch affordances (the server asks
// the terminal, which switches too): no `state.hosted` gate may hide them.
func TestHostedPageKeepsRepoSwitching(t *testing.T) {
	t.Parallel()
	for _, f := range []string{"palette.js", "sidebar.js", "locks.js"} {
		if src := readStatic(t, f); strings.Contains(src, "state.hosted") {
			t.Errorf("%s: a state.hosted gate is back — the hosted page must keep its switch affordances", f)
		}
	}
	cases := []struct{ file, want string }{
		{"palette.js", `{ label: "switch repo…", act: () => openPalette("repo") },`},
		{"sidebar.js", `items.unshift({ label: "switch here", act: () => doReroot(w.path) });`},
		{"locks.js", "if (served) doReroot(to);"},
	}
	for _, c := range cases {
		if !strings.Contains(readStatic(t, c.file), c.want) {
			t.Errorf("%s: missing %q", c.file, c.want)
		}
	}
}

// A switch veils the page from the request until the reload, and a failed
// switch takes the veil down again (a refusal must leave the page usable).
func TestRepoSwitchVeilWiring(t *testing.T) {
	t.Parallel()
	cases := []struct{ file, want string }{
		{"index.html", `<div id="switching" class="hidden" role="status" aria-live="polite">`},
		{"style.css", "#switching.hidden { display: none; }"},
		{"ops.js", "  showSwitching(path);\n  try {\n    await postJSON(\"/api/reroot\", { path });"},
		{"ops.js", "  } catch (e) {\n    hideSwitching();"},
		{"ops.js", "              hideSwitching();\n              opLine("},
		{"ops.js", "if (isSwitching()) { e.preventDefault(); e.stopImmediatePropagation(); }"},
	}
	for _, c := range cases {
		if !strings.Contains(readStatic(t, c.file), c.want) {
			t.Errorf("%s: missing %q", c.file, c.want)
		}
	}
}

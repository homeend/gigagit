package web

import (
	"strings"
	"testing"
)

// y on an overview row of ctrl+\'s tree copies its link (the TUI's treeKey
// y); on any other row it does nothing.
func TestTreeKeyActionCopyLinkJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "openfiles.js", ofPureStart, ofPureEnd, `
const a = (n) => treeKeyAction(n, "y", false);
console.log([a({kind: "overview"}), a({kind: "child"}), a({kind: "group"}), a({kind: "groupfile"}), treeKeyAction({kind: "overview"}, "y", true)].join(" "));
`)
	if out != "copy-link none none none copy-link" {
		t.Fatalf("got %s", out)
	}
}

// Every web overview surface copies the link the TUI's overviewLinkRow does
// (spec 2026-10-28-web-overview-links §1), through one helper that asks the
// server for it.
func TestOverviewCopyLinkIsWiredEverywhere(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ file, want, why string }{
		{"links.js", `"/api/session-overview/link?id=" + encodeURIComponent(ov.id)`, "a session overview's link is built by the server"},
		{"links.js", `"/api/review/" + encodeURIComponent(ov.stored.id) + "/link"`, "a review's overview copies the review link"},
		{"links.js", `"/api/overview/" + encodeURIComponent(ov.stored.id) + "/link"`, "a permanent overview copies its own link"},
		{"links.js", `copyOverviewLink`, "the helper is exported"},
		{"openfiles.js", `<button data-oact="enter">enter open</button><button data-oact="y">y copy link</button>`, "the tree's foot shows y after enter (the TUI's footer)"},
		{"openfiles.js", `case "copy-link": return copyOverviewLink(`, "y on an overview row copies"},
		{"openfiles.js", `b.dataset.oact === "y"`, "the foot button copies"},
		{"openfiles.js", `{ label: "Copy gg link", act: () => copyOverviewLink(`, "the row's right-click menu copies"},
		{"overviews.js", `{ label: "Export as HTML", act: () => exportOverview("overview=" + encodeURIComponent(id)) },
      { label: "Copy gg link", act: () => copyOverviewLink(`, "the commit's Overviews row menu: after Export, before Delete"},
		{"viewer.js", `case "L": copyOverviewLink(ovLinkOf(split.ov, split.id)); break;`, "L on the split's overview"},
		{"viewer.js", `case "L": copyOverviewLink(ovLinkOf(view.ov, view.id)); break;`, "L on the document-mode overview"},
		{"viewer.js", "<b>L</b> the overview's gg link", "the viewer help names L"},
		{"index.html", "/ copy gg link / delete", "the help names the overview row's copy"},
		{"openfiles.js", "<b>y</b> on an overview copies its gg link", "the switcher help names y"},
	} {
		if !strings.Contains(staticSrc(t, c.file), c.want) {
			t.Errorf("%s: missing %q — %s", c.file, c.want, c.why)
		}
	}
	if n := strings.Count(staticSrc(t, "viewer.js"), `<button data-vact="olink">L copy link</button>`); n != 3 {
		t.Errorf("viewer.js: %d L copy link chips, want 3 (split, stored, session)", n)
	}
}

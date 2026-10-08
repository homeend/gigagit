package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Task 12: the page asks the server for PR n's link pair — its base against
// gg's private head ref; an unknown PR is a 404.
// Serial: sendServerFull → prFixture isolates XDG state with t.Setenv.
func TestPRLinkPairOverTheWire(t *testing.T) {
	ts, _, _, _, _ := sendServerFull(t)
	var out struct{ Source, Target string }
	if code := getJSON(t, ts, "/api/pr/link?n=7", &out); code != 200 || out.Source != "refs/gg/pr/7" || out.Target != "main" {
		t.Fatalf("link pair = %d %+v", code, out)
	}
	if code := getJSON(t, ts, "/api/pr/link?n=99", nil); code != 404 {
		t.Fatalf("unknown PR = %d", code)
	}
}

// Task 12: the PR menu (row, header, compare bar) offers the link.
func TestPRMenuCopiesTheLink(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "links.js"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`registerRows("pr",`, `"/api/pr/link?n="`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("links.js lacks %q", want)
		}
	}
}

// Task 13: a navigate whose preview source is refs/gg/pr/<n> opens the PR's
// view on the page (live.js → prs.js openPRLanding); a PR the page does not
// list is refused in one line (ruling B3, TestUnlistedPRLinkSaysOneLine).
func TestPRLinkLandsInThePRView(t *testing.T) {
	t.Parallel()
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("static", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	live, prs := read("live.js"), read("prs.js")
	for _, want := range []string{`refs\/gg\/pr\/(\d+)`, "openPRLanding(", "is not in the pull request list"} {
		if !strings.Contains(live, want) {
			t.Errorf("live.js lacks %q", want)
		}
	}
	if !strings.Contains(prs, "export async function openPRLanding(") {
		t.Error("prs.js does not export openPRLanding")
	}
}

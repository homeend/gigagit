package cli

import (
	"strings"
	"testing"
)

// TestLinkPreviewCarriesTheHintOnlyForASavedEntry: `gg link --preview <x>`
// says WHERE the link came from only when <x> named a saved entry (its id or
// label). A typed <target>...<source> or <a>..<b> names no entry, and a
// file inside the preview lands on the file — both stay bare.
func TestLinkPreviewCarriesTheHintOnlyForASavedEntry(t *testing.T) {
	dir := previewRepo(t)
	code, out, errb := runCLI(t, dir, "preview", "add", "--label", "my preview", "feat/x", "main")
	if code != 0 {
		t.Fatalf("preview add: %d %s", code, errb)
	}
	pvID := strings.TrimSpace(out)
	code, out, errb = runCLI(t, dir, "preview", "add", "--label", "my pair", "main..feat/x")
	if code != 0 {
		t.Fatalf("pair add: %d %s", code, errb)
	}
	prID := strings.TrimSpace(out)
	// A pair saved WITHOUT a label gets "<a7>..<b7>" — text that is also a
	// valid typed range. Typing it is typing a range: no entry was named.
	a, b := strings.TrimSpace(gitOut(t, dir, "rev-parse", "main")), strings.TrimSpace(gitOut(t, dir, "rev-parse", "feat/x"))
	if code, _, errb := runCLI(t, dir, "preview", "add", b+".."+a); code != 0 {
		t.Fatalf("default-label pair add: %d %s", code, errb)
	}
	defaultLabel := b[:7] + ".." + a[:7]

	for _, c := range []struct {
		name string
		args []string
		hint string // "" = the link must carry no hint
	}{
		{"saved preview by label", []string{"link", "--preview", "my preview"}, "?preview=" + pvID},
		{"saved preview by id", []string{"link", "--preview", pvID}, "?preview=" + pvID},
		{"saved pair by label", []string{"link", "--preview", "my pair"}, "?preview=" + prID},
		{"typed preview", []string{"link", "--preview", "main...feat/x"}, ""},
		{"typed pair", []string{"link", "--preview", "main..feat/x"}, ""},
		{"a range that spells a saved pair's default label", []string{"link", "--preview", defaultLabel}, ""},
		{"a file inside a saved preview", []string{"link", "--preview", "my preview", "a.txt"}, ""},
	} {
		code, out, errb := runCLI(t, dir, c.args...)
		if code != 0 {
			t.Errorf("%s: exit %d (%s)", c.name, code, errb)
			continue
		}
		link := strings.TrimSpace(out)
		if c.hint == "" {
			if strings.Contains(link, "?") {
				t.Errorf("%s: link = %q, want no hint", c.name, link)
			}
			continue
		}
		if !strings.HasSuffix(link, c.hint) {
			t.Errorf("%s: link = %q, want it to end %q", c.name, link, c.hint)
		}
	}
}

package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// gg web hides by ID: a bare class="hidden" with no #id.hidden rule is ALWAYS
// visible (it shipped once). Every element the commit-description block adds
// starts hidden and must have its own rule, and the viewer hides the prompt's
// ok button the same way.
func TestCommitDescStaticHasPerIDHiddenRules(t *testing.T) {
	t.Parallel()
	css, err := os.ReadFile(filepath.Join("static", "style.css"))
	if err != nil {
		t.Fatal(err)
	}
	html, err := os.ReadFile(filepath.Join("static", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"files-desc", "files-desc-more", "prompt-ok"} {
		if !strings.Contains(string(html), `id="`+id+`"`) {
			t.Errorf("index.html: no element #%s", id)
		}
		if !strings.Contains(string(css), "#"+id+".hidden { display: none; }") {
			t.Errorf("style.css: no `#%s.hidden { display: none; }` rule — the element would never hide", id)
		}
	}
	// The clamp is the guarantee the header stays a header: three lines, no more.
	if !strings.Contains(string(css), "-webkit-line-clamp: 3") {
		t.Error("style.css: #files-desc is not clamped to three lines")
	}
}

package static

import "testing"

func TestFSHoldsTheSharedAssets(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"index.html", "style.css", "markdown.js", "splitdiff.js", "app.js"} {
		if b, err := FS.ReadFile(name); err != nil || len(b) == 0 {
			t.Errorf("%s: %v (%d bytes)", name, err, len(b))
		}
	}
	if _, err := FS.ReadFile("embed.go"); err == nil {
		t.Error("the Go source must not be embedded")
	}
}

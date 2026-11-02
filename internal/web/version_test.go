package web

import (
	"net/http"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/buildinfo"
	"github.com/homeend/gigagit/internal/domain"
)

func TestVersionEndpointNamesThisBinary(t *testing.T) {
	t.Parallel()
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	var got buildinfo.Info
	if code := getJSON(t, ts, "/api/version", &got); code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if got != buildinfo.Current() {
		t.Fatalf("got %+v, want %+v", got, buildinfo.Current())
	}
}

func TestPaletteHasVersionRow(t *testing.T) {
	t.Parallel()
	src := readStatic(t, "palette.js")
	for _, w := range []string{`label: "version"`, `/api/version`, `readonly: true`, `(b.label.toLowerCase() === q) - (a.label.toLowerCase() === q)`} {
		if !strings.Contains(src, w) {
			t.Errorf("palette.js lacks %q", w)
		}
	}
}

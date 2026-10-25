package web

import (
	"net/http"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
)

// Settings → file width: GET shows the width in effect, POST writes the
// GLOBAL [ui] file_width (refusing what the views cannot use) and tells a
// hosting terminal to re-read its config; health then carries the new width.
func TestSettingsFileWidth(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	srv := New(domain.Open(newRepoDir(t, 1)))
	notified := 0
	srv.SetConfigChanged(func(relocated bool) {
		if !relocated {
			notified++
		}
	})
	ts := serve(t, srv)
	var got struct {
		FileWidth int `json:"file_width"`
	}
	if code := getJSON(t, ts, "/api/settings", &got); code != http.StatusOK || got.FileWidth != 200 {
		t.Fatalf("GET = %d, file_width %d; want 200", code, got.FileWidth)
	}
	for _, bad := range []string{"0", "7", "-2"} {
		if code := postJSON(t, ts, "/api/settings", `{"file_width":`+bad+`}`, "application/json", "", nil); code != http.StatusBadRequest {
			t.Errorf("file_width %s: POST = %d, want 400", bad, code)
		}
	}
	if code := postJSON(t, ts, "/api/settings", `{"file_width":120}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("POST 120 = %d", code)
	}
	if cfg, err := config.Load(config.DefaultGlobalPath(), ""); err != nil || cfg.UI.FileWidthCols() != 120 {
		t.Fatalf("global file_width = %d (%v), want 120", cfg.UI.FileWidthCols(), err)
	}
	if notified != 1 {
		t.Errorf("the hosting terminal was told %d times, want once", notified)
	}
	var h healthOut
	if getJSON(t, ts, "/api/health", &h); h.FileWidth != 120 {
		t.Errorf("health file_width = %d, want 120", h.FileWidth)
	}
	if code := getJSON(t, ts, "/api/settings", &got); code != http.StatusOK || got.FileWidth != 120 {
		t.Fatalf("GET after = %d, file_width %d; want 120", code, got.FileWidth)
	}
}

func TestSettingsFileWidthPanel(t *testing.T) {
	t.Parallel()
	js := readStatic(t, "settings.js")
	for _, want := range []string{
		`id="s-file-width" value="${d.file_width}"`,
		`if (Number.isInteger(v) && v !== d.file_width) patch.file_width = v;`,
		`if ("file_width" in patch) fetchHealth();`,
		`inp.id === "s-retention" || inp.id === "s-file-width"`,
		"file width saved globally; this repo's settings set ${state.settings.file_width}",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("settings.js: missing %q", want)
		}
	}
}

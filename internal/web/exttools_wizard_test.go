package web

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/exttool"
)

type wizardRowWire struct {
	Tool       string `json:"tool"`
	Label      string `json:"label"`
	Category   string `json:"category"`
	Name       string `json:"name"`
	Configured bool   `json:"configured"`
	Checked    bool   `json:"checked"`
	Command    string `json:"command"`
	Status     string `json:"status"`
	OfferID    string `json:"offer_id"`
	Declined   bool   `json:"declined"`
}

// wizardDetections: one tool with a plain row, an opt-in row and a
// conflict_complete row without OptIn (the Kimi case).
func wizardDetections() []exttool.Detection {
	return []exttool.Detection{{
		Tool: exttool.Tool{
			ID: "claude", Label: "Claude Code",
			Commands: []exttool.CommandTemplate{
				{Category: exttool.CatReview, Name: "Claude", Mode: "capture", Command: "<bin> -p review"},
				{Category: exttool.CatConflict, Name: "Claude (yolo)", Mode: "terminal", OptIn: true, Command: "<bin> --yolo"},
				{Category: exttool.CatConflictComplete, Name: "Claude — resolve & complete", Mode: "terminal", Command: "<bin> --complete"},
			},
		},
		Bin: "/usr/bin/claude",
	}}
}

func wizardServer(t *testing.T, globalToml string) (string, *Server) {
	t.Helper()
	global := isolateGlobal(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if globalToml != "" {
		if err := os.MkdirAll(filepath.Dir(global), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(global, []byte(globalToml), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	srv := New(domain.Open(newRepoDir(t, 1)))
	srv.detectTools = wizardDetections
	return global, srv
}

func wizardRows(t *testing.T, srv *Server) []wizardRowWire {
	t.Helper()
	var got struct {
		Wizard []wizardRowWire `json:"wizard"`
	}
	if code := getJSON(t, serve(t, srv), "/api/exttools", &got); code != http.StatusOK {
		t.Fatalf("GET /api/exttools = %d", code)
	}
	return got.Wizard
}

func TestExtToolsWizardRows(t *testing.T) {
	_, srv := wizardServer(t, "[[tools.command]]\ncategory = \"review\"\nname = \"Claude\"\nmode = \"capture\"\ncommand = \"claude -p mine\"\n")
	srv.toolStatuses = func(context.Context) []domain.ToolTemplateStatus {
		return []domain.ToolTemplateStatus{{
			Path: "/x", Kind: domain.ToolUpdateAvailable,
			Block: config.ToolCommand{Category: "review", Name: "Claude"},
			New:   config.ToolCommand{Category: "review", Name: "Claude", Command: "claude -p new"},
		}}
	}
	rows := wizardRows(t, srv)
	if len(rows) != 3 {
		t.Fatalf("rows = %+v", rows)
	}
	r0, r1, r2 := rows[0], rows[1], rows[2]
	if r0.Tool != "claude" || r0.Label != "Claude Code" || r0.Category != "review" || r0.Name != "Claude" {
		t.Errorf("row 0 = %+v", r0)
	}
	if !r0.Configured || !r0.Checked || r0.Status != "update" || r0.OfferID == "" {
		t.Errorf("a configured row with an offer: %+v", r0)
	}
	if r0.Command != "/usr/bin/claude -p review" {
		t.Errorf("command preview = %q", r0.Command)
	}
	if r1.Checked || r1.Configured || r2.Checked || r2.Configured {
		t.Errorf("opt-in and conflict_complete start unchecked: %+v %+v", r1, r2)
	}
}

func TestExtToolsWizardAdd(t *testing.T) {
	global, srv := wizardServer(t, "")
	var changed []bool
	srv.SetConfigChanged(func(relocated bool) { changed = append(changed, relocated) })
	ts := serve(t, srv)
	body := `{"rows":[{"tool":"claude","category":"review","name":"Claude"},{"tool":"claude","category":"conflict","name":"Claude (yolo)"}]}`
	code, got := postJSONAny(t, ts, "/api/exttools/add", body)
	if code != http.StatusOK || got["written"] != float64(2) || got["path"] != global {
		t.Fatalf("add = %d %v", code, got)
	}
	cfg, err := config.Load(global, "")
	if err != nil {
		t.Fatal(err)
	}
	if n := len(cfg.Tools.Command); n != 2 {
		t.Fatalf("blocks written = %d", n)
	}
	if !strings.Contains(cfg.Tools.Command[0].Command, "/usr/bin/claude -p review") {
		t.Errorf("block 0 = %+v", cfg.Tools.Command[0])
	}
	if len(changed) != 1 || changed[0] {
		t.Errorf("the terminal is told (tools, not relocated): %v", changed)
	}
	// Again: both are configured now — nothing written, never a duplicate.
	code, got = postJSONAny(t, ts, "/api/exttools/add", body)
	if code != http.StatusOK || got["written"] != float64(0) {
		t.Fatalf("second add = %d %v", code, got)
	}
	if cfg, _ = config.Load(global, ""); len(cfg.Tools.Command) != 2 {
		t.Errorf("a duplicate was written: %d blocks", len(cfg.Tools.Command))
	}
	if len(changed) != 1 {
		t.Errorf("nothing written, nothing to tell: %v", changed)
	}
}

func TestExtToolsWizardAddRefusesUnknownRows(t *testing.T) {
	global, srv := wizardServer(t, "")
	ts := serve(t, srv)
	for _, body := range []string{
		`{"rows":[{"tool":"claude","category":"review","name":"Claude"},{"tool":"claude","category":"review","name":"nope"}]}`,
		`{"rows":[{"tool":"aider","category":"review","name":"Claude"}]}`,
		`{"rows":[{"tool":"claude","category":"review","name":"Claude","command":"rm -rf /"}]}`,
		`not json`,
	} {
		code, _ := postJSONAny(t, ts, "/api/exttools/add", body)
		if code != http.StatusBadRequest {
			t.Errorf("%s → %d, want 400", body, code)
		}
	}
	if _, err := os.Stat(global); !os.IsNotExist(err) {
		t.Errorf("a refused add wrote the global config: %v", err)
	}
	// Nothing checked is the TUI's "nothing to write", not a refusal.
	if code, got := postJSONAny(t, ts, "/api/exttools/add", `{"rows":[]}`); code != http.StatusOK || got["written"] != float64(0) {
		t.Errorf("empty add = %d %v", code, got)
	}
}

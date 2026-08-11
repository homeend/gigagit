package web

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/promptstate"
)

type extToolsWire struct {
	Commands []struct {
		Category  string   `json:"category"`
		Name      string   `json:"name"`
		Mode      string   `json:"mode"`
		PerFile   bool     `json:"per_file"`
		WhenOp    string   `json:"when_op"`
		Frontends []string `json:"frontends"`
		Command   string   `json:"command"`
		Valid     bool     `json:"valid"`
		Problem   string   `json:"problem"`
		Approved  bool     `json:"approved"`
	} `json:"commands"`
	Detected []struct {
		ID        string `json:"id"`
		Label     string `json:"label"`
		Bin       string `json:"bin"`
		Templates []struct {
			Category   string `json:"category"`
			Name       string `json:"name"`
			OptIn      bool   `json:"opt_in"`
			Configured bool   `json:"configured"`
		} `json:"templates"`
	} `json:"detected"`
	GlobalConfigPath string `json:"global_config_path"`
}

// fakeDetections is the detection seam's test double: one catalog tool with
// two templates, one of which matches a configured (category,name) block.
func fakeDetections() []exttool.Detection {
	return []exttool.Detection{{
		Tool: exttool.Tool{
			ID: "claude", Label: "Claude Code",
			Commands: []exttool.CommandTemplate{
				{Category: exttool.CatReview, Name: "claude", Mode: "capture", Command: "claude -p x"},
				{Category: exttool.CatConflictComplete, Name: "claude", Mode: "capture", OptIn: true, Command: "claude --yolo"},
			},
		},
		Bin: "/usr/bin/claude",
	}}
}

func TestExtToolsView(t *testing.T) {
	global := isolateGlobal(t) // XDG_CONFIG_HOME → temp; returns the global config path
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Dir(global), 0o755); err != nil {
		t.Fatal(err)
	}
	// one valid block matching a catalog template, one structurally broken one
	cfgToml := `[[tools.command]]
category = "review"
name = "claude"
mode = "capture"
command = "claude -p review"

[[tools.command]]
category = "review"
name = "broken"
mode = "wat"
command = "x"
`
	if err := os.WriteFile(global, []byte(cfgToml), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := newRepoDir(t, 1)
	srv := New(domain.Open(dir))
	srv.detectTools = fakeDetections
	ts := serve(t, srv)

	var got extToolsWire
	if code := getJSON(t, ts, "/api/exttools", &got); code != http.StatusOK {
		t.Fatalf("GET /api/exttools code = %d", code)
	}
	if len(got.Commands) != 2 {
		t.Fatalf("commands = %+v", got.Commands)
	}
	byName := map[string]int{}
	for i, c := range got.Commands {
		byName[c.Name] = i
	}
	ok := got.Commands[byName["claude"]]
	if !ok.Valid || ok.Problem != "" || ok.Approved || ok.Category != "review" || ok.Mode != "capture" {
		t.Errorf("valid row = %+v", ok)
	}
	bad := got.Commands[byName["broken"]]
	if bad.Valid || bad.Problem == "" {
		t.Errorf("broken row must report its problem: %+v", bad)
	}
	if got.GlobalConfigPath != global {
		t.Errorf("global_config_path = %q, want %q", got.GlobalConfigPath, global)
	}

	// detection: the seam's rows come through, with configured computed
	// against the effective config's (category,name) set
	if len(got.Detected) != 1 || got.Detected[0].ID != "claude" || got.Detected[0].Bin != "/usr/bin/claude" {
		t.Fatalf("detected = %+v", got.Detected)
	}
	tmpls := got.Detected[0].Templates
	if len(tmpls) != 2 {
		t.Fatalf("templates = %+v", tmpls)
	}
	if !tmpls[0].Configured || tmpls[0].Category != "review" {
		t.Errorf("review template should be configured: %+v", tmpls[0])
	}
	if tmpls[1].Configured || !tmpls[1].OptIn {
		t.Errorf("conflict_complete template = %+v", tmpls[1])
	}

	// approving the command (the promptstate store the review lane writes)
	// flips approved on the next read — same store, same key, same hash
	store := promptstate.NewFileStore(filepath.Join(filepath.Dir(srv.reposStatePath()), "prompts.toml"))
	key := srv.toolRepoKey(t.Context(), srv.service())
	if err := store.ApproveToolCommand(key, promptstate.CommandHash("claude -p review")); err != nil {
		t.Fatal(err)
	}
	if code := getJSON(t, ts, "/api/exttools", &got); code != http.StatusOK {
		t.Fatal("second GET failed")
	}
	if !got.Commands[byName["claude"]].Approved {
		t.Errorf("approved flag did not flip: %+v", got.Commands)
	}
}

// TestExtToolsEmpty: no config, no detections — the payload is empty arrays,
// not nulls or errors.
func TestExtToolsEmpty(t *testing.T) {
	isolateGlobal(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := newRepoDir(t, 1)
	srv := New(domain.Open(dir))
	srv.detectTools = func() []exttool.Detection { return nil }
	ts := serve(t, srv)

	var got extToolsWire
	if code := getJSON(t, ts, "/api/exttools", &got); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if len(got.Commands) != 0 || len(got.Detected) != 0 {
		t.Errorf("payload = %+v", got)
	}
}

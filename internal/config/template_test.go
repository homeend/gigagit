package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Every toml field on the config structs must be registered in settingDocs, so
// the generated template can never silently fall behind the code. THIS is the
// "update the registry when you add a setting" enforcement.
func TestSettingDocsCoverAllFields(t *testing.T) {
	registered := map[string]bool{}
	for _, d := range settingDocs {
		registered[d.section+"."+d.key] = true
	}
	check := func(section string, rt reflect.Type) {
		for i := 0; i < rt.NumField(); i++ {
			key := rt.Field(i).Tag.Get("toml")
			if key == "" {
				continue
			}
			if !registered[section+"."+key] {
				t.Errorf("config field %s.%s has no settingDocs entry — add one in template.go", section, key)
			}
		}
	}
	check("worktree", reflect.TypeOf(WorktreeConfig{}))
	check("ui", reflect.TypeOf(UIConfig{}))
	check("debug", reflect.TypeOf(DebugConfig{}))
	check("refresh", reflect.TypeOf(RefreshConfig{}))
}

// For settings whose default lives in Defaults(), the registry value must match
// it (no drift). Use-site defaults (reflog_limit, search_history_size) and the
// commitgraph ceiling are literals not covered here.
func TestSettingDocsMatchDefaults(t *testing.T) {
	d := Defaults()
	want := map[string]any{
		"worktree.path_template":           d.Worktree.PathTemplate,
		"worktree.default_branch_template": d.Worktree.DefaultBranchTemplate,
		"ui.wheel_step":                    d.UI.WheelStep,
		"ui.hscroll_step":                  d.UI.HScrollStep,
		"ui.commit_graph_lanes":            d.UI.CommitGraphLanes,
		"ui.commit_graph_min_lanes":        d.UI.CommitGraphMinLanes,
		"ui.commit_graph_step":             d.UI.CommitGraphStep,
	}
	got := map[string]any{}
	for _, doc := range settingDocs {
		got[doc.section+"."+doc.key] = doc.value
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("settingDocs[%s].value = %v, want Defaults() %v", k, got[k], v)
		}
	}
}

// The template must be valid TOML and, since every line is commented, decode to
// a zero Config — proving `config init` is inert until a line is uncommented.
func TestTemplateRoundTripsToZeroConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(Template()), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, ok, err := decodeFile(path)
	if err != nil || !ok {
		t.Fatalf("template did not decode: ok=%v err=%v", ok, err)
	}
	if !reflect.DeepEqual(cfg, Config{}) {
		t.Fatalf("commented template must decode to a zero Config, got %+v", cfg)
	}
}

// A sampling of keys must appear so a gutted registry is caught.
func TestTemplateMentionsKeySettings(t *testing.T) {
	out := Template()
	for _, k := range []string{"[worktree]", "[ui]", "reflog_limit", "search_history_size", "wheel_step", "commit_graph_pan_step"} {
		if !strings.Contains(out, k) {
			t.Errorf("template missing %q", k)
		}
	}
}

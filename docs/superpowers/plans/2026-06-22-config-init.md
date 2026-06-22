# `gg config init` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `gg config init (--repo | --global) [--force]`, which writes a commented config template listing every gg setting with its default + description, plus tests that keep the listing complete.

**Architecture:** A `settingDocs` registry in `internal/config` is the single source of truth; `config.Template()` renders it as a fully-commented `.gg.toml`. A new `gg config` CLI command (with an `init` subcommand) writes it to the repo or global path. Reflection/value-sync/round-trip tests guard the registry against drift.

**Tech Stack:** Go 1.26, `github.com/pelletier/go-toml/v2`, standard `flag`/`os`.

## Global Constraints

- Module `github.com/gigagit/gg`, Go 1.26.
- `internal/config` is a leaf package (imports only `os`, `path/filepath`, `fmt`, `strconv`, `reflect` in tests, and go-toml). Do NOT make it import `domain`/`searchhist`/`commitgraph` — centralizing the scattered defaults is out of scope (`reflog_limit`'s default must stay in `domain` because Snapshot runs before config loads).
- The generated file is **fully commented** — writing it changes nothing until a line is uncommented.
- `gg config` is its own command namespace with an `init` subcommand; it must NOT collide with the existing top-level `gg init` (agent-skill installer).
- New CLI surface ⇒ bump `agentskill.Version` AND run `gg init --update` in the same commit (`TestDogfoodSkillCopyInSync` fails on either alone).
- `main` is the trunk; branch off `main`. Run `./test.sh race` before merge.
- Every commit ends with these two trailers verbatim:
  ```
  Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro
  ```

---

### Task 1: settings registry + `Template()` + guard tests

**Files:**
- Create: `internal/config/template.go`
- Test: `internal/config/template_test.go`

**Interfaces:**
- Consumes: `config.Config`, `config.Defaults()`, `config.UIConfig`, `config.WorktreeConfig`, the unexported `decodeFile`.
- Produces: `func config.Template() string`; unexported `settingDocs []settingDoc`, `type settingDoc struct{ section, key string; value any; comment string }`, `func tomlScalar(v any) string`.

- [ ] **Step 1: Write the failing tests**

Create `internal/config/template_test.go`:

```go
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
}

// For settings whose default lives in Defaults(), the registry value must match
// it (no drift). Use-site defaults (reflog_limit, search_history_size) and the
// commitgraph ceiling are literals not covered here.
func TestSettingDocsMatchDefaults(t *testing.T) {
	d := Defaults()
	want := map[string]any{
		"worktree.path_template":          d.Worktree.PathTemplate,
		"worktree.default_branch_template": d.Worktree.DefaultBranchTemplate,
		"ui.wheel_step":             d.UI.WheelStep,
		"ui.hscroll_step":           d.UI.HScrollStep,
		"ui.commit_graph_lanes":     d.UI.CommitGraphLanes,
		"ui.commit_graph_min_lanes": d.UI.CommitGraphMinLanes,
		"ui.commit_graph_step":      d.UI.CommitGraphStep,
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
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/config/ -run "SettingDocs|Template"`
Expected: FAIL — `settingDocs`/`Template` undefined.

- [ ] **Step 3: Implement the registry + renderer**

Create `internal/config/template.go`:

```go
package config

import (
	"fmt"
	"strconv"
	"strings"
)

// settingDoc documents one configuration setting for the generated template
// (`gg config init`). value is the default rendered into the file: an int or
// string for a concrete default, or nil when the setting has no honest scalar
// default (derived, or "empty = all"), in which case it renders comment-only.
//
// settingDocs is the SINGLE SOURCE OF TRUTH for the generated config file. When
// you add a [ui]/[worktree] setting, add its entry here — TestSettingDocsCoverAllFields
// (config/template_test.go) FAILS until you do.
type settingDoc struct {
	section string // "worktree" or "ui"
	key     string // toml key
	value   any    // int | string | nil (nil ⇒ comment-only, no "= value")
	comment string // one-line description; states the default when value is nil
}

var settingDocs = []settingDoc{
	{"worktree", "path_template", "../<repo>.worktrees/<branch>", "where gg worktree creates new worktrees (tokens: <repo> <branch> <parent-branch> <date:…> <seq:…>)"},
	{"worktree", "default_branch_template", "b/from-<parent-branch>-<random-alpha:4>", "auto branch name for a new worktree"},
	{"worktree", "branch_templates", nil, "extra branch-name templates offered in the worktree popup (default: none)"},

	{"ui", "wheel_step", 3, "mouse-wheel scroll step, in rows"},
	{"ui", "hscroll_step", 8, "diff scroll-mode horizontal pan step, in columns"},
	{"ui", "footer_actions", nil, "action ids shown in the footer bar (default: empty = show all)"},
	{"ui", "menu_actions", nil, "action ids shown in the . menu (default: empty = show all)"},
	{"ui", "search_history_size", 20, "phrases kept per search-history ring (max 1000)"},
	{"ui", "reflog_limit", 200, "max HEAD reflog entries the Reflog tab loads"},
	{"ui", "commit_graph_lanes", 8, "default commit-graph window width, in lanes"},
	{"ui", "commit_graph_min_lanes", 2, "minimum commit-graph window width (narrow floor)"},
	{"ui", "commit_graph_step", 4, "commit-graph widen/narrow increment, in lanes"},
	{"ui", "commit_graph_pan_step", nil, "commit-graph pan increment, in lanes (default: derived, max(1, cols/2))"},
	{"ui", "commit_graph_max_lanes", 320, "commit-graph plane cap, in lanes (config can only lower the 320 ceiling)"},
}

// tomlScalar renders a registry value as it appears in TOML.
func tomlScalar(v any) string {
	switch t := v.(type) {
	case int:
		return strconv.Itoa(t)
	case string:
		return `"` + t + `"`
	}
	return ""
}

// Template renders the commented config file: a header, then [worktree] and
// [ui] sections in settingDocs order. Every line is commented, so writing the
// file changes nothing until a line is uncommented.
func Template() string {
	var b strings.Builder
	b.WriteString("# gg configuration — every setting with its default.\n")
	b.WriteString("# Uncomment a line to override the default. Values shown are gg's built-in\n")
	b.WriteString("# defaults; leaving a line commented keeps tracking the default across versions.\n")
	for _, section := range []string{"worktree", "ui"} {
		b.WriteString("\n[" + section + "]\n")
		for _, d := range settingDocs {
			if d.section != section {
				continue
			}
			if d.value == nil {
				fmt.Fprintf(&b, "# %s   # %s\n", d.key, d.comment)
			} else {
				fmt.Fprintf(&b, "# %s = %s   # %s\n", d.key, tomlScalar(d.value), d.comment)
			}
		}
	}
	return b.String()
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/config/ -run "SettingDocs|Template" -v`
Expected: PASS (all four).

- [ ] **Step 5: Commit**

```bash
git add internal/config/template.go internal/config/template_test.go
git commit -m "feat(config): settings registry + Template() with coverage guard

settingDocs is the single source for the generated config file; a reflection
test fails if a struct field is not registered, a value-sync test pins the
Defaults()-backed values, and a round-trip test proves the template is valid,
inert TOML.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

### Task 2: `gg config init` CLI command

**Files:**
- Create: `internal/cli/config.go`
- Modify: `internal/cli/cli.go` (the `commands` map + `Run` switch)
- Test: `internal/cli/config_test.go`

**Interfaces:**
- Consumes: `config.Template()`, `config.DefaultGlobalPath()`, `flag`, `os`, `filepath`.
- Produces: `func cmdConfig(workdir string, args []string, stdout, stderr io.Writer) int`; `commands["config"] = true`; `case "config"` in `Run`.

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/config_test.go`:

```go
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigInitRepoWritesTemplate(t *testing.T) {
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	if rc := cmdConfig(dir, []string{"init", "--repo"}, &out, &errOut); rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errOut.String())
	}
	data, err := os.ReadFile(filepath.Join(dir, ".gg.toml"))
	if err != nil {
		t.Fatalf("config not written: %v", err)
	}
	if !strings.Contains(string(data), "[ui]") || !strings.Contains(string(data), "reflog_limit") {
		t.Fatalf("template content missing:\n%s", data)
	}
}

func TestConfigInitRefusesExistingWithoutForce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gg.toml")
	if err := os.WriteFile(path, []byte("# mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if rc := cmdConfig(dir, []string{"init", "--repo"}, &out, &errOut); rc == 0 {
		t.Fatal("must refuse to overwrite without --force")
	}
	if !strings.Contains(errOut.String(), path) {
		t.Fatalf("refuse message must name the path, got %q", errOut.String())
	}
	if b, _ := os.ReadFile(path); string(b) != "# mine\n" {
		t.Fatal("existing file must be left untouched")
	}
}

func TestConfigInitForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gg.toml")
	if err := os.WriteFile(path, []byte("# mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if rc := cmdConfig(dir, []string{"init", "--repo", "--force"}, &out, &errOut); rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errOut.String())
	}
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), "[ui]") {
		t.Fatal("--force must overwrite with the template")
	}
}

func TestConfigInitGlobalUsesXDG(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	var out, errOut bytes.Buffer
	if rc := cmdConfig(t.TempDir(), []string{"init", "--global"}, &out, &errOut); rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errOut.String())
	}
	if _, err := os.Stat(filepath.Join(home, "gg", "config.toml")); err != nil {
		t.Fatalf("global config not written under XDG dir: %v", err)
	}
}

func TestConfigInitRequiresExactlyOneScope(t *testing.T) {
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	if rc := cmdConfig(dir, []string{"init"}, &out, &errOut); rc == 0 {
		t.Fatal("neither --repo nor --global must error")
	}
	if rc := cmdConfig(dir, []string{"init", "--repo", "--global"}, &out, &errOut); rc == 0 {
		t.Fatal("both --repo and --global must error")
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/cli/ -run TestConfigInit`
Expected: FAIL — `cmdConfig` undefined.

- [ ] **Step 3: Implement `cmdConfig`**

Create `internal/cli/config.go`:

```go
package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/gigagit/gg/internal/config"
)

// cmdConfig implements `gg config <subcommand>`. Currently only `init`, which
// scaffolds a fully-commented config file. Pure file I/O — no git, no repo.
func cmdConfig(workdir string, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: gg config init (--repo | --global) [--force]")
		return 2
	}
	switch args[0] {
	case "init":
		return cmdConfigInit(workdir, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown config subcommand %q\n", args[0])
		return 2
	}
}

func cmdConfigInit(workdir string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("config init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.Bool("repo", false, "write ./.gg.toml for this repository")
	global := fs.Bool("global", false, "write the global config (~/.config/gg/config.toml)")
	force := fs.Bool("force", false, "overwrite an existing file")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *repo == *global { // neither or both
		fmt.Fprintln(stderr, "config init: pass exactly one of --repo or --global")
		return 2
	}

	var path string
	if *repo {
		path = filepath.Join(workdir, ".gg.toml")
	} else {
		path = config.DefaultGlobalPath()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			fmt.Fprintf(stderr, "config init: %v\n", err)
			return 1
		}
	}

	if _, err := os.Stat(path); err == nil && !*force {
		fmt.Fprintf(stderr, "config init: %s already exists (use --force to overwrite)\n", path)
		return 1
	}
	if err := os.WriteFile(path, []byte(config.Template()), 0o644); err != nil {
		fmt.Fprintf(stderr, "config init: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "wrote", path)
	return 0
}
```

- [ ] **Step 4: Register the command**

In `internal/cli/cli.go`, add `"config"` to the `commands` map (next to `"init"`):

```go
	"inspect": true, "repo": true, "init": true, "config": true,
```

And add a case in the `Run` switch (next to `case "init":`):

```go
	case "config":
		return cmdConfig(workdir, rest, stdout, stderr)
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/cli/ -run TestConfigInit -v`
Expected: PASS (all five).

- [ ] **Step 6: Verify routing doesn't collide with `gg init`**

Run: `grep -n '"config"\|"init"' internal/cli/cli.go`
Expected: both present in the `commands` map and the switch; `config` and `init` are separate cases. Also confirm the registration guard test passes:
Run: `go test ./internal/cli/ -run TestEverySwitchCaseIsRegistered`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/config.go internal/cli/config_test.go internal/cli/cli.go
git commit -m "feat(cli): gg config init (--repo|--global) [--force]

Writes a fully-commented config template from config.Template(). Own 'config'
command namespace with an init subcommand; refuses to overwrite without --force
(message names the path); --global mkdir -p's ~/.config/gg.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

### Task 3: docs + agentskill

**Files:**
- Modify: `README.md` (Configuration section)
- Modify: `CHANGELOG.md`
- Modify: `internal/agentskill/using-gg.md` + `internal/agentskill/agentskill.go` (`Version`)
- Regenerate: `.claude/skills/using-gg/SKILL.md` via `gg init --update`

**Interfaces:** none (docs).

- [ ] **Step 1: README**

In `README.md`, in the `## Configuration` section, after the paragraph
describing `.gg.toml`, add:

```markdown
Run `gg config init --repo` (writes `./.gg.toml`) or `gg config init --global`
(writes `~/.config/gg/config.toml`) to scaffold a config file listing every
setting commented-out with its default and a description — uncomment what you
want to change. It refuses to overwrite an existing file without `--force`.
```

- [ ] **Step 2: CHANGELOG**

In `CHANGELOG.md` under `## [Unreleased]` → `### Added`:

```markdown
- **`gg config init`.** Scaffolds a documented config file (`--repo` for
  `./.gg.toml`, `--global` for `~/.config/gg/config.toml`) with every setting
  commented-out alongside its default and a one-line description. Refuses to
  overwrite without `--force`.
```

- [ ] **Step 3: agentskill — document the command + bump Version**

In `internal/agentskill/using-gg.md`, in the `## Commands` list, add (near the
other non-git utility commands):

```markdown
- `gg config init (--repo | --global) [--force]` — write a documented config
  file (every setting commented with its default); `--repo` → `./.gg.toml`,
  `--global` → `~/.config/gg/config.toml`. Refuses to overwrite without `--force`.
```

In `internal/agentskill/agentskill.go`, bump:

```go
const Version = 25
```

- [ ] **Step 4: Regenerate the dogfood skill copy**

Run:
```bash
go build -o /tmp/gg ./cmd/gg && /tmp/gg init --update
```
Expected: refreshes `.claude/skills/using-gg/SKILL.md` (stamped `v25`).

- [ ] **Step 5: Verify the dogfood sync test**

Run: `go test ./internal/agentskill/ -run TestDogfoodSkillCopyInSync`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add README.md CHANGELOG.md internal/agentskill/using-gg.md internal/agentskill/agentskill.go .claude/skills/using-gg/SKILL.md
git commit -m "docs: gg config init — readme, changelog, agentskill v25

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

### Final verification (before merge)

- [ ] `./test.sh race` — all green.
- [ ] Manual smoke: `go build -o /tmp/gg ./cmd/gg`; in a scratch dir
  `/tmp/gg config init --repo` then `cat .gg.toml` (every setting commented with
  defaults); re-run → refuses; `--force` → overwrites; `/tmp/gg config init
  --global` writes under `~/.config/gg/`.
- [ ] Write the maintenance memory note (registry is the single source; the
  reflection test enforces coverage).
- [ ] Use superpowers:finishing-a-development-branch to complete (merge to `main`).
```

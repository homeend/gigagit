# `gg config populate` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `gg config populate (--repo | --global)`, a purely additive merge that tops up an existing config file with every supported setting not already present, written as commented lines marked `[populated]`.

**Architecture:** A pure `populate(raw string) string` core in `internal/config` drives a line-preserving additive merge from the existing `settingDocs` registry; a `PopulateFile(path)` wrapper does the file I/O via the existing `atomicWriteFile`; `cmdConfigPopulate` in `internal/cli/config.go` mirrors `cmdConfigInit` and routes `--repo`/`--global` to the right path.

**Tech Stack:** Go 1.26, standard library only (`strings`, `os`, `path/filepath`), `github.com/pelletier/go-toml/v2` (already a dep, not needed for the line-oriented core), gigagit's existing `internal/config` and `internal/cli` packages.

## Global Constraints

- Module `github.com/homeend/gigagit`, Go 1.26.
- `internal/config` is read-only at runtime **except** narrow non-destructive line-edit writers (`SetGlobalDebugLogOperations`, `SetGlobalRefreshEnabled`); `populate` joins them as another non-destructive writer. Writes go through the existing `atomicWriteFile`.
- The `settingDocs` slice in `internal/config/template.go` is the SINGLE SOURCE OF TRUTH for documented settings; `populate` reads it, never a second list. `TestSettingDocsCoverAllFields` already guards its completeness.
- `internal/cli` must NOT import `internal/git` (archtest-guarded); reach git only through `internal/domain` (`svc.TopLevel`).
- Marker text is the literal ` [populated]` appended to the trailing description comment.
- Section order matches `Template()`: `worktree`, `ui`, `debug`, `refresh`.
- Run `./test.sh unit` (and `go vet ./... && gofmt -l .`) before each commit; the repo enforces gofmt + vet in the unit stage.
- Work happens in the worktree `.claude/worktrees/config-populate` on branch `feat/config-populate`. The human merges to `main`.

---

### Task 1: `populate` core + `PopulateFile` writer

**Files:**
- Create: `internal/config/populate.go`
- Test: `internal/config/populate_test.go`
- Reference (do not modify): `internal/config/template.go` (`settingDocs`, `tomlScalar`, `Template`), `internal/config/write.go` (`lineAssignsKey`, `atomicWriteFile`)

**Interfaces:**
- Consumes: `settingDocs []settingDoc` (fields `section`, `key`, `value any`, `comment string`); `tomlScalar(v any) string`; `lineAssignsKey(trimmed, key string) bool`; `atomicWriteFile(path string, data []byte) error` — all already defined in package `config`.
- Produces:
  - `func populate(raw string) string` — pure; given current file content (possibly `""`), returns the new content with every absent `settingDocs` key added as a commented line. Existing lines preserved verbatim. Idempotent.
  - `func PopulateFile(path string) (added int, err error)` — reads `path` (missing → empty), runs `populate`, writes back via `atomicWriteFile` ONLY if content changed; returns the number of keys added.

**Design notes for the implementer (read before writing code):**
- A setting is "present" if any line — active (`key = …`) or commented (`# key = …`) — assigns it **while the walker is inside that setting's `[section]`**. Track the current section header as you scan lines; reuse `lineAssignsKey(strings.TrimSpace(line), d.key)`.
- Rendering a setting's commented line (match `Template()`'s column style):
  - scalar default (`d.value != nil`): `"# " + d.key + " = " + tomlScalar(d.value) + "   # " + d.comment + " [populated]"`
  - no scalar default (`d.value == nil`): `"# " + d.key + "   # " + d.comment + " [populated]"`
- Insertion: group absent docs by section in `settingDocs` order. For a section whose `[section]` header already exists, insert its absent lines immediately after the header line. For a section with no header, append a new block: a blank line (only if the file is currently non-empty), the `[section]` header, then its lines. Append new sections in canonical order `worktree, ui, debug, refresh`.
- `populate("")` must produce the full set: one `[section]` block per section (no leading blank line before the first), every key commented + marked.
- Idempotency falls out: on a second run every key is present, so nothing is inserted and the output equals the input.
- `added` count = number of `settingDocs` entries that were absent and got inserted.

- [ ] **Step 1: Write the failing tests**

```go
package config

import (
	"strings"
	"testing"
)

// Every settingDocs key must appear in the populated output of an empty file.
func TestPopulateEmptyAddsAllKeys(t *testing.T) {
	out := populate("")
	for _, d := range settingDocs {
		if !strings.Contains(out, d.key) {
			t.Errorf("populate(\"\") missing key %q\n%s", d.key, out)
		}
		if d.value != nil {
			want := "# " + d.key + " = " + tomlScalar(d.value)
			if !strings.Contains(out, want) {
				t.Errorf("scalar key %q not rendered as %q\n%s", d.key, want, out)
			}
		}
	}
	if !strings.Contains(out, "[populated]") {
		t.Errorf("added lines must carry the [populated] marker:\n%s", out)
	}
	if strings.Contains(out, "wheel_step = 3\n") {
		t.Errorf("added lines must be COMMENTED, found an active line:\n%s", out)
	}
}

// An active override is preserved verbatim and never re-added.
func TestPopulateKeepsActiveOverride(t *testing.T) {
	in := "[ui]\nwheel_step = 5\n"
	out := populate(in)
	if !strings.Contains(out, "wheel_step = 5") {
		t.Errorf("active override dropped:\n%s", out)
	}
	if strings.Contains(out, "wheel_step = 3") {
		t.Errorf("present key must not be re-added with its default:\n%s", out)
	}
	if strings.Count(out, "wheel_step") != 1 {
		t.Errorf("wheel_step should appear exactly once, got:\n%s", out)
	}
}

// A key already present as a commented line is left exactly as-is (no marker,
// no duplicate).
func TestPopulateLeavesExistingCommentedKey(t *testing.T) {
	in := "[ui]\n# hscroll_step = 8   # mine\n"
	out := populate(in)
	if !strings.Contains(out, "# hscroll_step = 8   # mine") {
		t.Errorf("existing commented line altered:\n%s", out)
	}
	if strings.Count(out, "hscroll_step") != 1 {
		t.Errorf("hscroll_step must not be duplicated:\n%s", out)
	}
}

// Idempotent: populating twice yields the same content as once.
func TestPopulateIdempotent(t *testing.T) {
	once := populate("[ui]\nwheel_step = 5\n")
	twice := populate(once)
	if once != twice {
		t.Errorf("populate not idempotent:\nonce:\n%s\ntwice:\n%s", once, twice)
	}
}

// A missing section header is created and its keys added under it.
func TestPopulateCreatesMissingSection(t *testing.T) {
	in := "[ui]\nwheel_step = 5\n"
	out := populate(in)
	if !strings.Contains(out, "[refresh]") {
		t.Errorf("missing [refresh] section not created:\n%s", out)
	}
	if !strings.Contains(out, "# enabled = false") {
		t.Errorf("refresh.enabled not added under [refresh]:\n%s", out)
	}
}

// Unknown user keys are preserved.
func TestPopulatePreservesUnknownKeys(t *testing.T) {
	in := "[ui]\nmy_custom_key = 1\n"
	out := populate(in)
	if !strings.Contains(out, "my_custom_key = 1") {
		t.Errorf("unknown key dropped:\n%s", out)
	}
}

// A nil-default key (no honest scalar) is added commented and value-less.
func TestPopulateNilDefaultKeyValueless(t *testing.T) {
	out := populate("")
	if !strings.Contains(out, "# branch_templates   #") {
		t.Errorf("nil-default key must be commented + value-less:\n%s", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd .claude/worktrees/config-populate && go test ./internal/config/ -run TestPopulate -v`
Expected: FAIL — `undefined: populate`.

- [ ] **Step 3: Implement `populate.go`**

```go
package config

import "strings"

// populate returns raw with every settingDocs key not already present added as
// a commented documentation line (default + description + " [populated]"). A
// key counts as present if any active or commented assignment for it exists
// within its [section]. Existing lines are preserved verbatim; the result is
// idempotent. An empty raw yields the full set, one [section] block per section.
func populate(raw string) string {
	var lines []string
	if len(raw) > 0 {
		lines = strings.Split(strings.TrimRight(raw, "\n"), "\n")
	}

	// present[section][key] = true if an assignment line (active or commented)
	// exists under that section.
	present := map[string]map[string]bool{}
	headerAt := map[string]int{} // section -> line index of its [section] header
	section := ""
	for i, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section = strings.TrimSuffix(strings.TrimPrefix(trimmed, "["), "]")
			if _, ok := headerAt[section]; !ok {
				headerAt[section] = i
			}
			continue
		}
		for _, d := range settingDocs {
			if d.section == section && lineAssignsKey(trimmed, d.key) {
				if present[section] == nil {
					present[section] = map[string]bool{}
				}
				present[section][d.key] = true
			}
		}
	}

	render := func(d settingDoc) string {
		if d.value == nil {
			return "# " + d.key + "   # " + d.comment + " [populated]"
		}
		return "# " + d.key + " = " + tomlScalar(d.value) + "   # " + d.comment + " [populated]"
	}

	// Absent docs grouped by section, in settingDocs order.
	order := []string{"worktree", "ui", "debug", "refresh"}
	missing := map[string][]string{}
	for _, d := range settingDocs {
		if present[d.section][d.key] {
			continue
		}
		missing[d.section] = append(missing[d.section], render(d))
	}

	// Insert into existing sections (back-to-front so earlier indices stay valid).
	type insertion struct {
		at   int
		body []string
	}
	var inserts []insertion
	for sec, body := range missing {
		if at, ok := headerAt[sec]; ok {
			inserts = append(inserts, insertion{at: at + 1, body: body})
		}
	}
	// Sort descending by index without importing sort: simple selection.
	for i := 0; i < len(inserts); i++ {
		max := i
		for j := i + 1; j < len(inserts); j++ {
			if inserts[j].at > inserts[max].at {
				max = j
			}
		}
		inserts[i], inserts[max] = inserts[max], inserts[i]
	}
	for _, ins := range inserts {
		lines = append(lines[:ins.at], append(append([]string{}, ins.body...), lines[ins.at:]...)...)
	}

	// Append brand-new sections in canonical order.
	for _, sec := range order {
		body, ok := missing[sec]
		if !ok {
			continue
		}
		if _, exists := headerAt[sec]; exists {
			continue // already inserted above
		}
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, "["+sec+"]")
		lines = append(lines, body...)
	}

	return strings.Join(lines, "\n") + "\n"
}

// PopulateFile reads path (a missing file is treated as empty), adds every
// settingDocs key not already present as a commented line, and writes the
// result back atomically — only when something changed. It returns the number
// of keys added.
func PopulateFile(path string) (added int, err error) {
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return 0, err
	}
	before := string(raw)
	after := populate(before)
	if after == before {
		return 0, nil
	}
	added = countAdded(before)
	if err := atomicWriteFile(path, []byte(after)); err != nil {
		return 0, err
	}
	return added, nil
}

// countAdded reports how many settingDocs keys are absent from raw (i.e. how
// many populate would add).
func countAdded(raw string) int {
	var lines []string
	if len(raw) > 0 {
		lines = strings.Split(strings.TrimRight(raw, "\n"), "\n")
	}
	present := map[string]map[string]bool{}
	section := ""
	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section = strings.TrimSuffix(strings.TrimPrefix(trimmed, "["), "]")
			continue
		}
		for _, d := range settingDocs {
			if d.section == section && lineAssignsKey(trimmed, d.key) {
				if present[section] == nil {
					present[section] = map[string]bool{}
				}
				present[section][d.key] = true
			}
		}
	}
	n := 0
	for _, d := range settingDocs {
		if !present[d.section][d.key] {
			n++
		}
	}
	return n
}
```

Add the `os` import to the file header (`import ("os"; "strings")`).

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd .claude/worktrees/config-populate && go test ./internal/config/ -run TestPopulate -v && gofmt -l internal/config/populate.go`
Expected: all `TestPopulate*` PASS; `gofmt -l` prints nothing.

- [ ] **Step 5: Add a PopulateFile round-trip test**

Append to `internal/config/populate_test.go`:

```go
import (
	"os"
	"path/filepath"
)

func TestPopulateFileTopsUpAndCounts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gg.toml")
	if err := os.WriteFile(path, []byte("[ui]\nwheel_step = 5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	added, err := PopulateFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if added == 0 {
		t.Fatal("expected keys to be added")
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "wheel_step = 5") {
		t.Fatalf("override clobbered:\n%s", b)
	}
	// Second run is a no-op.
	added2, err := PopulateFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if added2 != 0 {
		t.Fatalf("second populate should add nothing, added %d", added2)
	}
}

func TestPopulateFileMissingFileCreatesIt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gg.toml")
	added, err := PopulateFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if added != len(settingDocs) {
		t.Fatalf("fresh file should add all %d keys, added %d", len(settingDocs), added)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}
```

Merge the `os`/`path/filepath` imports into the existing import block (don't add a second `import (...)`).

- [ ] **Step 6: Run the full config package test**

Run: `cd .claude/worktrees/config-populate && go test ./internal/config/ && go vet ./internal/config/`
Expected: PASS, no vet complaints.

- [ ] **Step 7: Commit**

```bash
cd .claude/worktrees/config-populate
git add internal/config/populate.go internal/config/populate_test.go
git commit -m "feat(config): populate core — additive merge from settingDocs

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01Y28iVYm7u1P2qdoa4CdK9G"
```

---

### Task 2: `gg config populate` CLI subcommand

**Files:**
- Modify: `internal/cli/config.go` (add `case "populate"` to `cmdConfig`, update the usage line, add `cmdConfigPopulate`)
- Test: `internal/cli/config_test.go` (append cases)
- Reference (do not modify): `internal/cli/init_test.go` (fixture style), `internal/cli/config_test.go` existing cases (`newRepoDir`, `domain.Open`, `cmdConfig` signature)

**Interfaces:**
- Consumes: `config.PopulateFile(path string) (int, error)`; `config.DefaultGlobalPath() string`; `svc.TopLevel(context.Context) (string, error)`; existing helpers `newRepoDir(t)` and `domain.Open(dir)` from the cli test package.
- Produces: `gg config populate (--repo | --global)` exit codes — 0 on success, 2 on usage error, 1 on I/O error.

- [ ] **Step 1: Write the failing tests**

Append to `internal/cli/config_test.go`:

```go
func TestConfigPopulateRepoTopsUpExisting(t *testing.T) {
	dir := newRepoDir(t)
	svc := domain.Open(dir)
	path := filepath.Join(dir, ".gg.toml")
	if err := os.WriteFile(path, []byte("[ui]\nwheel_step = 5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if rc := cmdConfig(svc, dir, []string{"populate", "--repo"}, &out, &errOut); rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errOut.String())
	}
	b, _ := os.ReadFile(path)
	s := string(b)
	if !strings.Contains(s, "wheel_step = 5") {
		t.Fatalf("override clobbered:\n%s", s)
	}
	if !strings.Contains(s, "[refresh]") || !strings.Contains(s, "[populated]") {
		t.Fatalf("missing settings not populated:\n%s", s)
	}
}

func TestConfigPopulateGlobalUsesXDG(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	svc := domain.Open(t.TempDir())
	var out, errOut bytes.Buffer
	if rc := cmdConfig(svc, t.TempDir(), []string{"populate", "--global"}, &out, &errOut); rc != 0 {
		t.Fatalf("rc=%d stderr=%s", rc, errOut.String())
	}
	if _, err := os.Stat(filepath.Join(home, "gg", "config.toml")); err != nil {
		t.Fatalf("global config not written under XDG dir: %v", err)
	}
}

func TestConfigPopulateRequiresExactlyOneScope(t *testing.T) {
	dir := t.TempDir()
	svc := domain.Open(dir)
	var out, errOut bytes.Buffer
	if rc := cmdConfig(svc, dir, []string{"populate"}, &out, &errOut); rc == 0 {
		t.Fatal("neither --repo nor --global must error")
	}
	if rc := cmdConfig(svc, dir, []string{"populate", "--repo", "--global"}, &out, &errOut); rc == 0 {
		t.Fatal("both --repo and --global must error")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd .claude/worktrees/config-populate && go test ./internal/cli/ -run TestConfigPopulate -v`
Expected: FAIL — `unknown config subcommand "populate"` (rc=2 where 0 expected).

- [ ] **Step 3: Wire the subcommand**

In `internal/cli/config.go`, update the usage line and add the route + handler:

```go
// in cmdConfig: replace the bare-usage line
fmt.Fprintln(stderr, "usage: gg config (init | populate) (--repo | --global) [--force]")
```

```go
// in cmdConfig's switch, add:
	case "populate":
		return cmdConfigPopulate(svc, workdir, args[1:], stdout, stderr)
```

```go
// new function, after cmdConfigInit:

// cmdConfigPopulate implements `gg config populate`. It adds every supported
// setting not already present to the target file as a commented, [populated]-
// marked line, leaving existing content untouched. Pure file I/O — no git
// writes (beyond the TopLevel read for --repo).
func cmdConfigPopulate(svc *domain.Service, workdir string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("config populate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	repo := fs.Bool("repo", false, "top up ./.gg.toml for this repository")
	global := fs.Bool("global", false, "top up the global config (~/.config/gg/config.toml)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *repo == *global { // neither or both
		fmt.Fprintln(stderr, "config populate: pass exactly one of --repo or --global")
		return 2
	}

	var path string
	if *repo {
		root := workdir
		if top, err := svc.TopLevel(context.Background()); err == nil && top != "" {
			root = top
		}
		path = filepath.Join(root, ".gg.toml")
	} else {
		path = config.DefaultGlobalPath()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			fmt.Fprintf(stderr, "config populate: %v\n", err)
			return 1
		}
	}

	added, err := config.PopulateFile(path)
	if err != nil {
		fmt.Fprintf(stderr, "config populate: %v\n", err)
		return 1
	}
	if added == 0 {
		fmt.Fprintln(stdout, path, "already complete")
	} else {
		fmt.Fprintf(stdout, "populated %s (%d added)\n", path, added)
	}
	return 0
}
```

(No new imports needed — `context`, `flag`, `fmt`, `io`, `os`, `path/filepath`, `config`, `domain` are already imported by `config.go`.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd .claude/worktrees/config-populate && go test ./internal/cli/ -run TestConfig -v`
Expected: all `TestConfigInit*` and `TestConfigPopulate*` PASS.

- [ ] **Step 5: Commit**

```bash
cd .claude/worktrees/config-populate
git add internal/cli/config.go internal/cli/config_test.go
git commit -m "feat(cli): gg config populate subcommand

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01Y28iVYm7u1P2qdoa4CdK9G"
```

---

### Task 3: Docs, agentskill, skill checklist, CLAUDE.md, memory

**Files:**
- Modify: `CHANGELOG.md`
- Modify: `README.md` (the `## Configuration` section)
- Modify: `internal/agentskill/using-gg.md` (config section)
- Modify: `internal/agentskill/agentskill.go` (`const Version`)
- Modify: `.claude/skills/adding-config-entries/SKILL.md` (checklist)
- Modify: `CLAUDE.md` (the `config` package-map row)
- Create: a project memory file under the auto-memory dir + index line

**Interfaces:**
- Consumes: nothing in code. Produces: documentation only. No tests beyond `agentskill`'s existing version-marker assertions, which key off `const Version`.

- [ ] **Step 1: CHANGELOG entry**

Add under the top/unreleased section of `CHANGELOG.md` (match the file's existing bullet style):

```markdown
- `gg config populate (--repo | --global)` — tops up an existing config file
  with every supported setting not yet present, added as commented `[populated]`
  lines; never touches existing overrides; idempotent. Complements
  `gg config init`.
```

- [ ] **Step 2: README Configuration section**

In `README.md`'s `## Configuration` section, next to the `gg config init`
description, add:

```markdown
Run `gg config populate (--repo | --global)` to top up an existing config file
with settings added in newer gg versions. Unlike `init`, it never overwrites:
it only appends the keys you don't have yet, as commented lines marked
`[populated]`, leaving your existing values and comments intact. Safe to re-run.
```

- [ ] **Step 3: agentskill doc + version bump**

In `internal/agentskill/using-gg.md`, find the line documenting
`gg config init (--repo | --global) [--force]` (around line 170) and add right
after its paragraph:

```markdown
- `gg config populate (--repo | --global)` — add any settings missing from an
  existing config file as commented `[populated]` lines; never overwrites your
  values; idempotent. Use after upgrading gg to pick up new settings.
```

Then bump the version in `internal/agentskill/agentskill.go`:

```go
const Version = 34
```

- [ ] **Step 4: adding-config-entries checklist**

In `.claude/skills/adding-config-entries/SKILL.md`, insert a new checklist step
between the current step 6 (Consume) and step 7 (Document). Renumber the
following steps:

```markdown
7. **Template registry**: add a `settingDoc` entry in
   `internal/config/template.go` — the SINGLE registry feeding both
   `gg config init` and `gg config populate`. `TestSettingDocsCoverAllFields`
   fails until you do.
```

- [ ] **Step 5: CLAUDE.md package-map note**

In `CLAUDE.md`, in the `config` package-map row, append a sentence noting that
`gg config init` and `gg config populate` are both generated from the
`settingDocs` registry (the single source of truth), and that `populate` is an
additive, comments-only top-up that never overwrites user values.

- [ ] **Step 6: Project memory**

Create `/home/homeend/.claude/projects/-mnt-t-others-gigagit/memory/config-populate-feature.md`:

```markdown
---
name: config-populate-feature
description: gg config populate — additive config top-up; settingDocs is the one registry feeding both init and populate
metadata:
  type: project
---

`feat/config-populate` (2026-06-27): added `gg config populate (--repo | --global)`.

Additive, comments-only merge: adds every `settingDocs` key not already present
(active OR commented) to the target file as a commented line + ` [populated]`
marker; leaves existing overrides/comments untouched; idempotent. Pure core
`config.populate(raw string) string` + `config.PopulateFile(path)` in
`internal/config/populate.go`; `cmdConfigPopulate` in `internal/cli/config.go`
mirrors `cmdConfigInit`.

KEY: `settingDocs` (`internal/config/template.go`) is the SINGLE registry
feeding BOTH `gg config init` and `gg config populate`. Adding a config entry
needs only a `settingDoc` there (guarded by `TestSettingDocsCoverAllFields`) —
no command-specific maintenance. See [[config-settings-registry]].
```

Then add one line to `/home/homeend/.claude/projects/-mnt-t-others-gigagit/memory/MEMORY.md` under the Features section:

```markdown
- [gg config populate](config-populate-feature.md) — additive config top-up; `settingDocs` is the one registry feeding both init and populate; comments-only, never overwrites, idempotent
```

- [ ] **Step 7: Verify agentskill tests still pass**

Run: `cd .claude/worktrees/config-populate && go test ./internal/agentskill/`
Expected: PASS (version-marker assertions read `const Version`, now 34).

- [ ] **Step 8: Commit**

```bash
cd .claude/worktrees/config-populate
git add CHANGELOG.md README.md internal/agentskill/using-gg.md internal/agentskill/agentskill.go .claude/skills/adding-config-entries/SKILL.md CLAUDE.md
git commit -m "docs: document gg config populate; note settingDocs registry feeds both commands

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01Y28iVYm7u1P2qdoa4CdK9G"
```

(The memory files live outside the repo; they are not part of the git commit.)

---

### Task 4: Full verification

**Files:** none (verification only).

- [ ] **Step 1: Full unit + vet + gofmt stage**

Run: `cd .claude/worktrees/config-populate && ./test.sh unit`
Expected: PASS (vet + gofmt clean, all unit tests green).

- [ ] **Step 2: Race + e2e before handing off for merge**

Run: `cd .claude/worktrees/config-populate && ./test.sh race`
Expected: PASS.

- [ ] **Step 3: Manual smoke test**

```bash
cd .claude/worktrees/config-populate
go build -o ./gg ./cmd/gg
tmp=$(mktemp -d)
printf '[ui]\nwheel_step = 5\n' > "$tmp/.gg.toml"
# populate uses repo toplevel; point at a throwaway repo instead:
( cd "$tmp" && git init -q && /mnt/t/others/gigagit/.claude/worktrees/config-populate/gg config populate --repo )
cat "$tmp/.gg.toml"
```
Expected: `wheel_step = 5` preserved; `[refresh]`, `[debug]`, and all other
`settingDocs` keys present as `# key = default   # … [populated]` lines;
stdout `populated <path> (N added)`. Re-running prints `<path> already complete`.

- [ ] **Step 4: Report the built binary path to the user for hands-on testing**

Give the user the absolute path: `/mnt/t/others/gigagit/.claude/worktrees/config-populate/gg`.

---

## Self-Review

**Spec coverage:**
- Surface (`--repo`/`--global`, exactly-one, TopLevel resolution, no `--force`, missing file → full set) → Task 2 (+ Task 1 `PopulateFile` for missing-file behavior).
- Per-setting rule (present active/commented → untouched; absent scalar → commented+marked; absent nil → value-less commented; section creation; idempotent) → Task 1 core + tests.
- Source of truth = `settingDocs` → Task 1 (consumed directly).
- Implementation shape (`populate`, `PopulateFile`, `cmdConfigPopulate`) → Tasks 1–2.
- Testing list → Tasks 1–2 test steps.
- Docs & maintenance reminder (CHANGELOG, README, using-gg+version, skill checklist, CLAUDE.md, memory) → Task 3.
- Open questions resolved per spec defaults: no-op write skipped (`PopulateFile` returns early when unchanged); message `populated <path> (N added)` / `already complete` (Task 2 Step 3).

**Placeholder scan:** none — every code/test step shows complete code; commands have expected output.

**Type consistency:** `populate(raw string) string`, `PopulateFile(path string) (added int, err error)`, `countAdded(raw string) int`, `cmdConfigPopulate(svc, workdir, args, stdout, stderr)` used identically across tasks; `const Version = 34` referenced by Task 3 Steps 3 and 7.

# Agent Init (`gg init`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `gg init` (CLI) and a `,` Settings popup (TUI) that detect installed AI agents and install/refresh an embedded "using-gg" skill teaching them to drive git through the gg CLI.

**Architecture:** `internal/agentskill` embeds the skill markdown (`go:embed`) with a version marker and renders the three target formats; `internal/agentinit` holds the hardcoded agent registry plus detect/status/install; the CLI command and TUI popup are thin presentations of the same engine. Checkbox rule everywhere: already-installed targets default checked (apply = refresh), `new` targets default unchecked. Spec: `docs/superpowers/specs/2026-06-12-agent-init-design.md`.

**Tech Stack:** Go 1.26, `go:embed`, `regexp` for marker parsing, Bubble Tea popup (worktree/repo popups are the exemplars).

**Branch:** Create `feat/agent-init` off `main` before Task 1.

## File Structure

- `internal/agentskill/using-gg.md` (create) — the canonical skill body (verbatim in Task 1).
- `internal/agentskill/agentskill.go` (create) — embed, `Version`, `Body`, `SkillFile`, `PlainFile`, `Block`, `InstalledVersion`.
- `internal/agentinit/agentinit.go` (create) — `Agent`/`Mode`/`Status`/`Detection`, `Builtins`, `Detect`, `Install`.
- `internal/cli/init.go` (create) — `cmdInit` (list/pick/install + flags).
- `internal/cli/cli.go` (modify) — `InitHomeDir` var, registration, dispatch.
- `cmd/gg/main.go` (modify) — wire `InitHomeDir`, help string.
- `internal/tui/settings_popup.go` (create) — two-level popup (menu → checkbox picker).
- `internal/tui/model.go`, `view.go`, `run.go` (modify) — `,` key, routing, overlay, footer, `initHomeDir` wiring.
- `.claude/skills/adding-features/SKILL.md`, `CLAUDE.md`, `CHANGELOG.md`, `README.md` (modify) — convention + docs.

**Hermeticity rule (load-bearing):** home-scoped detection is driven by an injected home dir — `cli.InitHomeDir` (package var, default `""`) and `Model.initHomeDir` (field, default `""`). Empty = home-scoped agents are skipped entirely, so no test can ever write the developer's real `~/.claude` etc. Only `cmd/gg/main.go` and `tui.Run` wire `os.UserHomeDir()`.

---

### Task 1: `internal/agentskill` — embedded skill + renderers

**Files:**
- Create: `internal/agentskill/using-gg.md`, `internal/agentskill/agentskill.go`
- Test: `internal/agentskill/agentskill_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/agentskill/agentskill_test.go`:

```go
package agentskill

import (
	"fmt"
	"strings"
	"testing"
)

func TestBodyCoversTheCLISurface(t *testing.T) {
	b := Body()
	for _, want := range []string{
		"gg status", "gg commit", "gg pull", "gg push", "gg switch",
		"gg stash", "gg undo", "gg worktree", "gg repo", "gg inspect",
		"--on-conflict", "--with-branch", "--force",
		"non-interactive", "exit 1", "stderr",
	} {
		if !strings.Contains(b, want) {
			t.Errorf("body missing %q", want)
		}
	}
	if strings.Contains(b, "gg:using-gg") {
		t.Error("body must not contain markers (renderers add them)")
	}
}

func TestSkillFileHasFrontmatterAndMarker(t *testing.T) {
	s := SkillFile()
	if !strings.HasPrefix(s, "---\n") {
		t.Fatal("SkillFile must start with YAML frontmatter")
	}
	for _, want := range []string{"name: using-gg", "description: Use when",
		fmt.Sprintf("gg:using-gg:v%d", Version)} {
		if !strings.Contains(s, want) {
			t.Errorf("SkillFile missing %q", want)
		}
	}
	if !strings.Contains(s, Body()) {
		t.Error("SkillFile must contain the body verbatim")
	}
}

func TestPlainFileHasMarkerNoFrontmatter(t *testing.T) {
	s := PlainFile()
	if strings.HasPrefix(s, "---\n") {
		t.Fatal("PlainFile must not have YAML frontmatter")
	}
	if !strings.Contains(s, fmt.Sprintf("gg:using-gg:v%d", Version)) {
		t.Error("PlainFile missing version marker")
	}
}

func TestBlockIsDelimited(t *testing.T) {
	b := Block()
	if !strings.HasPrefix(b, fmt.Sprintf("<!-- gg:using-gg:v%d:begin -->", Version)) {
		t.Errorf("block begin marker wrong:\n%s", b[:80])
	}
	if !strings.HasSuffix(strings.TrimRight(b, "\n"), "<!-- gg:using-gg:end -->") {
		t.Error("block end marker missing")
	}
}

func TestInstalledVersionParsesAllForms(t *testing.T) {
	if got := InstalledVersion([]byte(SkillFile())); got != Version {
		t.Errorf("SkillFile version = %d, want %d", got, Version)
	}
	if got := InstalledVersion([]byte("x\n" + Block() + "\ny")); got != Version {
		t.Errorf("Block version = %d, want %d", got, Version)
	}
	if got := InstalledVersion([]byte("<!-- gg:using-gg:v3:begin -->")); got != 3 {
		t.Errorf("explicit v3 = %d, want 3", got)
	}
	if got := InstalledVersion([]byte("no marker here")); got != 0 {
		t.Errorf("no marker should be 0, got %d", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/agentskill/ -v`
Expected: FAIL — package missing.

- [ ] **Step 3: Create the skill body**

Create `internal/agentskill/using-gg.md` with EXACTLY this content:

```markdown
# Using gg (gigagit)

gg is a git client CLI built for very large repositories. When it is available
(`gg` on PATH, or a `./gg` binary in the repo), prefer it over raw git for the
operations below — its smart commands carry safety rails: automatic
stash/restore around switches, a never-drop-the-stash rule on conflicts, and
guards against removing the worktree you are standing in.

## Commands

- `gg status` — branch, upstream ahead/behind, changed files.
- `gg commit -m <msg> [-a]` — commit (`-a` also stages tracked modifications).
- `gg pull [--background] [--on-conflict rebase|merge|abort]` — smart pull;
  `--background` fast-forwards another branch's ref without checking it out.
- `gg push` — push the current branch (sets upstream when missing).
- `gg switch <branch>` — switch branches, auto-stashing and restoring local
  changes; on a restore conflict the stash is preserved, never dropped.
- `gg stash [-m <msg>]` — stash the working tree.
- `gg undo` — undo the last commit, keeping its changes (ref-only soft reset).
- `gg worktree list` / `gg worktree add [<start-point>]` /
  `gg worktree remove [--with-branch] [--force] <path>` — linked worktrees;
  `add` resolves branch/path templates from `.gg.toml` and may prompt on stdin
  for `<user:...>` fields.
- `gg repo list` / `gg repo switch <query>` — the known-repository registry
  (MRU); `switch` prints the path of the unique match.
- `gg inspect` — one-shot repo summary (scriptable health check).
- `gg init` — install/refresh this skill for detected AI agents.

## The rule that matters for agents

gg never hangs waiting for input mid-operation. When an operation hits a fork
(diverged branch, dirty worktree, unmerged branch), it needs a decision:

- Interactive terminals get a prompt; **non-interactive runs fail with exit 1
  and print the decision and its options to stderr** instead of blocking.
- Pre-answer decisions with the matching flag: `--on-conflict` for pull
  divergence; `--with-branch` / `--force` for worktree removal.
- On a non-zero exit, read stderr: it names the decision and the valid
  options; re-run with the matching flag.

Exit codes: 0 = success, 1 = operation failed or needs a decision,
2 = usage error.

## Shell following

Worktree and repo switches write the target directory to `--cwd-file <path>`
(human shells follow via `gg shell-init`). As an agent, just `cd` to the path
printed on stdout.
```

- [ ] **Step 4: Implement the package**

Create `internal/agentskill/agentskill.go`:

```go
// Package agentskill carries the "using-gg" skill that teaches AI coding
// agents to drive git through the gg CLI. The content is compiled into the
// binary (go:embed); installed copies are derived artifacts that change only
// when a newer binary's init runs.
package agentskill

import (
	_ "embed"
	"fmt"
	"regexp"
	"strconv"
)

//go:embed using-gg.md
var body string

// Version is bumped whenever using-gg.md (or the rendered wrappers) change.
// Installed copies carry it so init can tell new/outdated/up-to-date apart.
const Version = 1

// Body is the canonical markdown body — no frontmatter, no markers.
func Body() string { return body }

// marker is the version stamp embedded in every rendered form.
func marker() string { return fmt.Sprintf("<!-- gg:using-gg:v%d -->", Version) }

// SkillFile renders the Claude Code SKILL.md form: YAML frontmatter + version
// marker + body. The whole file is gg-owned and safe to overwrite.
func SkillFile() string {
	return "---\n" +
		"name: using-gg\n" +
		"description: Use when performing git operations (status, commit, pull, push, branch switch, stash, worktrees) in a repository where the gg CLI is available.\n" +
		"---\n\n" +
		marker() + "\n\n" + body
}

// PlainFile renders a frontmatter-free whole file (e.g. Cursor rules).
func PlainFile() string { return marker() + "\n\n" + body }

// Block renders the managed-block form for shared files (AGENTS.md, …): the
// body wrapped in begin/end markers so init can replace it without touching
// surrounding content.
func Block() string {
	return fmt.Sprintf("<!-- gg:using-gg:v%d:begin -->\n\n%s\n<!-- gg:using-gg:end -->", Version, body)
}

var versionRe = regexp.MustCompile(`gg:using-gg:v(\d+)`)

// InstalledVersion extracts the version stamped into previously installed
// content (any rendered form). 0 means no gg marker present.
func InstalledVersion(content []byte) int {
	m := versionRe.FindSubmatch(content)
	if m == nil {
		return 0
	}
	n, err := strconv.Atoi(string(m[1]))
	if err != nil {
		return 0
	}
	return n
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/agentskill/ -v`
Expected: PASS (5 tests).

- [ ] **Step 6: Commit**

```bash
gofmt -w internal/agentskill/
go vet ./internal/agentskill/
git add internal/agentskill/
git commit -m "feat(agentskill): embedded using-gg skill with version marker"
```

---

### Task 2: `internal/agentinit` — registry + detect/status/install

**Files:**
- Create: `internal/agentinit/agentinit.go`
- Test: `internal/agentinit/agentinit_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/agentinit/agentinit_test.go`:

```go
package agentinit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/agentskill"
)

// fixture creates proj+home dirs with the named agent-detect paths present.
func fixture(t *testing.T, projPaths, homePaths []string) (string, string) {
	t.Helper()
	proj, home := t.TempDir(), t.TempDir()
	for _, p := range projPaths {
		full := filepath.Join(proj, p)
		if filepath.Ext(p) == ".md" || strings.HasSuffix(p, "rules") {
			os.MkdirAll(filepath.Dir(full), 0o755)
			os.WriteFile(full, []byte("existing\n"), 0o644)
		} else {
			os.MkdirAll(full, 0o755)
		}
	}
	for _, p := range homePaths {
		full := filepath.Join(home, p)
		os.MkdirAll(full, 0o755)
	}
	return proj, home
}

func byID(dets []Detection, id string) (Detection, bool) {
	for _, d := range dets {
		if d.Agent.ID == id {
			return d, true
		}
	}
	return Detection{}, false
}

func TestDetectFindsOnlyPresentAgents(t *testing.T) {
	proj, home := fixture(t, []string{".claude", ".junie"}, []string{".claude"})
	dets := Detect(proj, home)
	for _, want := range []string{"claude-project", "claude-global", "junie"} {
		if _, ok := byID(dets, want); !ok {
			t.Errorf("missing detection %q in %+v", want, dets)
		}
	}
	if _, ok := byID(dets, "codex"); ok {
		t.Error("codex must not be detected without ~/.codex")
	}
	if _, ok := byID(dets, "cursor"); ok {
		t.Error("cursor must not be detected without .cursor")
	}
}

func TestDetectEmptyHomeSkipsHomeAgents(t *testing.T) {
	proj, _ := fixture(t, []string{".claude"}, nil)
	dets := Detect(proj, "")
	if _, ok := byID(dets, "claude-project"); !ok {
		t.Error("project agent should be detected")
	}
	if _, ok := byID(dets, "claude-global"); ok {
		t.Error("home agents must be skipped when homeDir is empty (hermeticity)")
	}
}

func TestStatusLifecycle(t *testing.T) {
	proj, home := fixture(t, []string{".claude"}, nil)
	dets := Detect(proj, home)
	d, ok := byID(dets, "claude-project")
	if !ok {
		t.Fatal("claude-project not detected")
	}
	if d.Status != StatusNew {
		t.Fatalf("fresh target should be StatusNew, got %v", d.Status)
	}
	if err := Install(d); err != nil {
		t.Fatal(err)
	}
	d2, _ := byID(Detect(proj, home), "claude-project")
	if d2.Status != StatusUpToDate {
		t.Fatalf("after install: %v, want StatusUpToDate", d2.Status)
	}
	// Simulate an older install: stamp v0… by writing an old marker.
	if err := os.WriteFile(d.Target, []byte("<!-- gg:using-gg:v0 -->\nold"), 0o644); err != nil {
		t.Fatal(err)
	}
	d3, _ := byID(Detect(proj, home), "claude-project")
	if d3.Status != StatusOutdated {
		t.Fatalf("old marker: %v, want StatusOutdated", d3.Status)
	}
}

func TestInstallWholeFileCreatesParents(t *testing.T) {
	proj, home := fixture(t, []string{".claude"}, nil)
	d, _ := byID(Detect(proj, home), "claude-project")
	if err := Install(d); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(proj, ".claude", "skills", "using-gg", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "name: using-gg") {
		t.Error("SKILL.md missing frontmatter")
	}
}

func TestInstallBlockPreservesSurroundingContent(t *testing.T) {
	proj, home := fixture(t, []string{"AGENTS.md"}, nil)
	target := filepath.Join(proj, "AGENTS.md")
	if err := os.WriteFile(target, []byte("# My rules\n\nkeep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d, ok := byID(Detect(proj, home), "agents-md")
	if !ok {
		t.Fatal("agents-md not detected")
	}
	if err := Install(d); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(target)
	s := string(data)
	if !strings.Contains(s, "keep me") {
		t.Error("surrounding content lost")
	}
	if agentskill.InstalledVersion(data) != agentskill.Version {
		t.Error("block not stamped with current version")
	}

	// Replace an OLD block in place: surrounding bytes stay identical.
	old := "# My rules\n\nkeep me\n\n<!-- gg:using-gg:v0:begin -->\n\nancient\n<!-- gg:using-gg:end -->\n\ntail stays\n"
	if err := os.WriteFile(target, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	d2, _ := byID(Detect(proj, home), "agents-md")
	if d2.Status != StatusOutdated {
		t.Fatalf("status = %v, want outdated", d2.Status)
	}
	if err := Install(d2); err != nil {
		t.Fatal(err)
	}
	data2, _ := os.ReadFile(target)
	s2 := string(data2)
	if !strings.HasPrefix(s2, "# My rules\n\nkeep me\n\n") || !strings.HasSuffix(s2, "\n\ntail stays\n") {
		t.Errorf("surrounding bytes changed:\n%s", s2)
	}
	if strings.Contains(s2, "ancient") {
		t.Error("old block content not replaced")
	}
	if strings.Count(s2, "gg:using-gg") != 2 { // one begin + one end marker
		t.Errorf("expected exactly one block, got:\n%s", s2)
	}
}

func TestInstallBlockCreatesMissingFile(t *testing.T) {
	proj, home := fixture(t, []string{".junie"}, nil)
	d, ok := byID(Detect(proj, home), "junie")
	if !ok {
		t.Fatal("junie not detected")
	}
	if err := Install(d); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(proj, ".junie", "guidelines.md"))
	if err != nil {
		t.Fatal(err)
	}
	if agentskill.InstalledVersion(data) != agentskill.Version {
		t.Error("created file missing block")
	}
}

func TestInstallIdempotent(t *testing.T) {
	proj, home := fixture(t, []string{"AGENTS.md"}, nil)
	d, _ := byID(Detect(proj, home), "agents-md")
	if err := Install(d); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(d.Target)
	d2, _ := byID(Detect(proj, home), "agents-md")
	if err := Install(d2); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(d.Target)
	if string(first) != string(second) {
		t.Error("double install must be byte-identical")
	}
}

func TestCheckedDefaults(t *testing.T) {
	if StatusNew.Checked() {
		t.Error("new targets must default unchecked")
	}
	if !StatusUpToDate.Checked() || !StatusOutdated.Checked() {
		t.Error("installed targets must default checked")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/agentinit/ -v`
Expected: FAIL — package missing.

- [ ] **Step 3: Implement**

Create `internal/agentinit/agentinit.go`:

```go
// Package agentinit detects installed AI coding agents and installs the
// embedded using-gg skill into their instruction locations. The agent
// registry is hardcoded — supporting a new agent is a code change (one
// Builtins entry), never a runtime definition.
package agentinit

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gigagit/gg/internal/agentskill"
)

// Mode is how the skill lands in a target file.
type Mode int

const (
	ModeSkillFile Mode = iota // whole gg-owned file with Claude frontmatter
	ModePlainFile             // whole gg-owned file, no frontmatter (Cursor)
	ModeBlock                 // marker-delimited block inside a shared file
)

// Agent is one registry entry. Detect/Target paths are relative to the
// project dir, or to the home dir when prefixed "~/".
type Agent struct {
	ID     string
	Label  string
	Detect string
	Target string
	Mode   Mode
}

// Builtins is the hardcoded agent registry. Adding support for a new agent is
// exactly one entry here.
func Builtins() []Agent {
	return []Agent{
		{ID: "claude-project", Label: "Claude Code (project)", Detect: ".claude", Target: ".claude/skills/using-gg/SKILL.md", Mode: ModeSkillFile},
		{ID: "claude-global", Label: "Claude Code (global)", Detect: "~/.claude", Target: "~/.claude/skills/using-gg/SKILL.md", Mode: ModeSkillFile},
		{ID: "junie", Label: "Junie (JetBrains)", Detect: ".junie", Target: ".junie/guidelines.md", Mode: ModeBlock},
		{ID: "codex", Label: "Codex (global)", Detect: "~/.codex", Target: "~/.codex/AGENTS.md", Mode: ModeBlock},
		{ID: "opencode", Label: "OpenCode (global)", Detect: "~/.config/opencode", Target: "~/.config/opencode/AGENTS.md", Mode: ModeBlock},
		{ID: "agents-md", Label: "AGENTS.md (generic)", Detect: "AGENTS.md", Target: "AGENTS.md", Mode: ModeBlock},
		{ID: "cursor", Label: "Cursor (project)", Detect: ".cursor", Target: ".cursor/rules/using-gg.mdc", Mode: ModePlainFile},
		{ID: "gemini", Label: "Gemini CLI (project)", Detect: "GEMINI.md", Target: "GEMINI.md", Mode: ModeBlock},
		{ID: "copilot", Label: "GitHub Copilot (project)", Detect: ".github", Target: ".github/copilot-instructions.md", Mode: ModeBlock},
		{ID: "windsurf", Label: "Windsurf (project)", Detect: ".windsurfrules", Target: ".windsurfrules", Mode: ModeBlock},
	}
}

// Status of a target relative to the binary's embedded skill version.
type Status int

const (
	StatusNew Status = iota
	StatusOutdated
	StatusUpToDate
)

func (s Status) String() string {
	switch s {
	case StatusOutdated:
		return "outdated"
	case StatusUpToDate:
		return "up to date"
	}
	return "new"
}

// Checked is the default checkbox state: targets that already have the skill
// (any version) default to checked — applying refreshes them; first-time
// installs are explicit opt-in.
func (s Status) Checked() bool { return s != StatusNew }

// Detection is one detected agent with its resolved target and status.
type Detection struct {
	Agent  Agent
	Target string // absolute
	Status Status
}

// resolve maps a registry path to an absolute path; "" means "not resolvable
// in this run" (home-scoped path with no homeDir — the hermeticity rule).
func resolve(p, projDir, homeDir string) string {
	if strings.HasPrefix(p, "~/") {
		if homeDir == "" {
			return ""
		}
		return filepath.Join(homeDir, p[2:])
	}
	return filepath.Join(projDir, p)
}

// Detect returns the registry entries whose Detect path exists, with each
// target's install status. An empty homeDir skips home-scoped agents entirely
// (tests must never see the developer's real home).
func Detect(projDir, homeDir string) []Detection {
	var out []Detection
	for _, a := range Builtins() {
		probe := resolve(a.Detect, projDir, homeDir)
		if probe == "" {
			continue
		}
		if _, err := os.Stat(probe); err != nil {
			continue
		}
		target := resolve(a.Target, projDir, homeDir)
		out = append(out, Detection{Agent: a, Target: target, Status: status(target)})
	}
	return out
}

// status reads the target and classifies it against the embedded version.
func status(target string) Status {
	data, err := os.ReadFile(target)
	if err != nil {
		return StatusNew
	}
	v := agentskill.InstalledVersion(data)
	switch {
	case v == 0:
		return StatusNew // file exists but has no gg block yet
	case v < agentskill.Version:
		return StatusOutdated
	default:
		return StatusUpToDate
	}
}

// blockRe matches a previously installed managed block, any version.
var blockRe = regexp.MustCompile(`(?s)<!-- gg:using-gg:v\d+:begin -->.*?<!-- gg:using-gg:end -->`)

// Install writes the embedded skill into d.Target according to the agent's
// mode, creating parent directories as needed. Shared files keep all
// surrounding content byte-for-byte. Idempotent.
func Install(d Detection) error {
	if err := os.MkdirAll(filepath.Dir(d.Target), 0o755); err != nil {
		return err
	}
	switch d.Agent.Mode {
	case ModeSkillFile:
		return os.WriteFile(d.Target, []byte(agentskill.SkillFile()), 0o644)
	case ModePlainFile:
		return os.WriteFile(d.Target, []byte(agentskill.PlainFile()), 0o644)
	case ModeBlock:
		block := agentskill.Block()
		existing, err := os.ReadFile(d.Target)
		if os.IsNotExist(err) {
			return os.WriteFile(d.Target, []byte(block+"\n"), 0o644)
		}
		if err != nil {
			return err
		}
		if blockRe.Match(existing) {
			return os.WriteFile(d.Target, blockRe.ReplaceAll(existing, []byte(block)), 0o644)
		}
		sep := "\n\n"
		if len(existing) == 0 || strings.HasSuffix(string(existing), "\n\n") {
			sep = ""
		} else if strings.HasSuffix(string(existing), "\n") {
			sep = "\n"
		}
		return os.WriteFile(d.Target, []byte(string(existing)+sep+block+"\n"), 0o644)
	}
	return fmt.Errorf("agentinit: unknown mode %d", d.Agent.Mode)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/agentinit/ -v`
Expected: PASS (8 tests).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/agentinit/
go vet ./internal/agentinit/
git add internal/agentinit/
git commit -m "feat(agentinit): hardcoded agent registry with detect/status/install"
```

---

### Task 3: CLI — `gg init`

**Files:**
- Create: `internal/cli/init.go`
- Modify: `internal/cli/cli.go`, `cmd/gg/main.go`
- Test: `internal/cli/init_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/init_test.go`:

```go
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/agentskill"
)

// initFixture: a project dir with .claude and AGENTS.md, plus a temp "home"
// with ~/.claude, wired into the package seam for one test.
func initFixture(t *testing.T) (string, string) {
	t.Helper()
	proj := t.TempDir()
	os.MkdirAll(filepath.Join(proj, ".claude"), 0o755)
	os.WriteFile(filepath.Join(proj, "AGENTS.md"), []byte("mine\n"), 0o644)
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	old := InitHomeDir
	InitHomeDir = home
	t.Cleanup(func() { InitHomeDir = old })
	return proj, home
}

func runInitCmd(t *testing.T, proj, stdinStr string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(proj, append([]string{"init"}, args...), strings.NewReader(stdinStr), &out, &errb, "")
	return code, out.String(), errb.String()
}

func TestInitListShowsCheckboxesAndStatus(t *testing.T) {
	proj, _ := initFixture(t)
	code, out, _ := runInitCmd(t, proj, "", "--list")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	for _, want := range []string{"[ ]", "Claude Code (project)", "Claude Code (global)", "AGENTS.md", "new"} {
		if !strings.Contains(out, want) {
			t.Errorf("list missing %q:\n%s", want, out)
		}
	}
}

func TestInitAllInstallsEverythingDetected(t *testing.T) {
	proj, home := initFixture(t)
	code, out, errb := runInitCmd(t, proj, "", "--all")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, errb)
	}
	for _, p := range []string{
		filepath.Join(proj, ".claude", "skills", "using-gg", "SKILL.md"),
		filepath.Join(home, ".claude", "skills", "using-gg", "SKILL.md"),
		filepath.Join(proj, "AGENTS.md"),
	} {
		data, err := os.ReadFile(p)
		if err != nil || agentskill.InstalledVersion(data) != agentskill.Version {
			t.Errorf("not installed at %s (%v)", p, err)
		}
	}
	if !strings.Contains(out, "installed") {
		t.Errorf("output should report installs:\n%s", out)
	}
}

func TestInitUpdateRefreshesOnlyInstalled(t *testing.T) {
	proj, _ := initFixture(t)
	// Pre-install ONLY agents-md, with an old version marker.
	target := filepath.Join(proj, "AGENTS.md")
	os.WriteFile(target, []byte("mine\n\n<!-- gg:using-gg:v0:begin -->\nold\n<!-- gg:using-gg:end -->\n"), 0o644)
	code, _, errb := runInitCmd(t, proj, "", "--update")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, errb)
	}
	data, _ := os.ReadFile(target)
	if agentskill.InstalledVersion(data) != agentskill.Version {
		t.Error("--update should refresh the outdated block")
	}
	if _, err := os.Stat(filepath.Join(proj, ".claude", "skills", "using-gg", "SKILL.md")); !os.IsNotExist(err) {
		t.Error("--update must NOT install into new targets")
	}
}

func TestInitAgentsByIDAndUnknownID(t *testing.T) {
	proj, _ := initFixture(t)
	code, _, _ := runInitCmd(t, proj, "", "--agents", "claude-project")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if _, err := os.Stat(filepath.Join(proj, ".claude", "skills", "using-gg", "SKILL.md")); err != nil {
		t.Error("claude-project not installed")
	}
	if _, err := os.Stat(filepath.Join(proj, "AGENTS.md.bak")); err == nil {
		t.Error("unexpected file")
	}
	code, _, errb := runInitCmd(t, proj, "", "--agents", "claude-project,bogus")
	if code != 2 || !strings.Contains(errb, "bogus") {
		t.Fatalf("unknown ID should exit 2 naming it, got %d / %q", code, errb)
	}
}

func TestInitInteractiveNumberInstallsExactlyOne(t *testing.T) {
	proj, home := initFixture(t)
	code, _, errb := runInitCmd(t, proj, "1\n")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, errb)
	}
	// Entry 1 is claude-project (registry order). Exactly one install.
	if _, err := os.Stat(filepath.Join(proj, ".claude", "skills", "using-gg", "SKILL.md")); err != nil {
		t.Error("entry 1 (claude-project) not installed")
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "using-gg", "SKILL.md")); !os.IsNotExist(err) {
		t.Error("entry 2 must not be installed")
	}
}

func TestInitEmptyEnterAppliesCheckedDefaults(t *testing.T) {
	proj, _ := initFixture(t)
	// Nothing installed yet: empty enter is a clean no-op.
	code, out, _ := runInitCmd(t, proj, "\n")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if _, err := os.Stat(filepath.Join(proj, ".claude", "skills", "using-gg", "SKILL.md")); !os.IsNotExist(err) {
		t.Error("nothing should be installed on empty-enter with no defaults")
	}
	if !strings.Contains(out, "nothing") {
		t.Errorf("should say nothing to do:\n%s", out)
	}
	// Install one, then empty-enter refreshes it (and only it).
	if code, _, _ := runInitCmd(t, proj, "", "--agents", "agents-md"); code != 0 {
		t.Fatal("seed install failed")
	}
	code, out, _ = runInitCmd(t, proj, "\n")
	if code != 0 || !strings.Contains(out, "refreshed") {
		t.Fatalf("empty-enter should refresh installed targets: %d\n%s", code, out)
	}
}

func TestInitNoInputExitsOneWithHint(t *testing.T) {
	proj, _ := initFixture(t)
	code, _, errb := runInitCmd(t, proj, "") // EOF immediately: non-interactive
	if code != 1 {
		t.Fatalf("EOF without selection should exit 1, got %d", code)
	}
	if !strings.Contains(errb, "--all") {
		t.Errorf("hint should mention --all:\n%s", errb)
	}
}

func TestInitQuitAborts(t *testing.T) {
	proj, _ := initFixture(t)
	code, _, _ := runInitCmd(t, proj, "q\n")
	if code != 0 {
		t.Fatalf("q should exit 0, got %d", code)
	}
	if _, err := os.Stat(filepath.Join(proj, ".claude", "skills", "using-gg", "SKILL.md")); !os.IsNotExist(err) {
		t.Error("q must not install anything")
	}
}

func TestInitNothingDetected(t *testing.T) {
	proj := t.TempDir() // no agent dirs at all
	old := InitHomeDir
	InitHomeDir = t.TempDir()
	t.Cleanup(func() { InitHomeDir = old })
	var out, errb bytes.Buffer
	code := Run(proj, []string{"init"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 || !strings.Contains(out.String(), "no") {
		t.Fatalf("nothing detected should exit 0 and say so: %d\n%s", code, out.String())
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli/ -run Init -v`
Expected: FAIL — `InitHomeDir` undefined.

- [ ] **Step 3: Implement**

(a) Create `internal/cli/init.go`:

```go
package cli

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/gigagit/gg/internal/agentinit"
)

// cmdInit implements `gg init`: detect AI agents, ask which to set up, and
// install/refresh the embedded using-gg skill. Pure file I/O — no git, no
// engine, works outside a repository.
func cmdInit(workdir string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	all := fs.Bool("all", false, "install for every detected agent")
	update := fs.Bool("update", false, "refresh every already-installed target (the checked defaults)")
	agents := fs.String("agents", "", "comma-separated agent IDs to install for")
	list := fs.Bool("list", false, "print detected agents and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	dets := agentinit.Detect(workdir, InitHomeDir)
	if len(dets) == 0 {
		fmt.Fprintln(stdout, "no supported agents detected here")
		return 0
	}

	printList(stdout, dets)
	if *list {
		return 0
	}

	var chosen []agentinit.Detection
	switch {
	case *all:
		chosen = dets
	case *update:
		chosen = checkedDefaults(dets)
	case *agents != "":
		for _, id := range strings.FieldsFunc(*agents, func(r rune) bool { return r == ',' || r == ' ' }) {
			d, ok := byAgentID(dets, id)
			if !ok {
				fmt.Fprintf(stderr, "init: unknown or undetected agent ID %q\n", id)
				return 2
			}
			chosen = append(chosen, d)
		}
	default:
		// Interactive: one selection line. EOF with no input = non-interactive
		// invocation without a selection flag — never guess, never hang.
		fmt.Fprint(stderr, "Apply? [enter]=checked / a=all / numbers (e.g. 1,3) / [q]uit: ")
		line, err := bufio.NewReader(stdin).ReadString('\n')
		if err != nil && line == "" {
			fmt.Fprintln(stderr, "init: no selection (non-interactive?); use --all, --update, or --agents")
			return 1
		}
		sel := strings.TrimSpace(line)
		switch {
		case sel == "q":
			return 0
		case sel == "a":
			chosen = dets
		case sel == "":
			chosen = checkedDefaults(dets)
			if len(chosen) == 0 {
				fmt.Fprintln(stdout, "nothing installed yet — nothing to refresh (pick numbers or use a/--all)")
				return 0
			}
		default:
			for _, tok := range strings.FieldsFunc(sel, func(r rune) bool { return r == ',' || r == ' ' }) {
				n, convErr := strconv.Atoi(tok)
				if convErr != nil || n < 1 || n > len(dets) {
					fmt.Fprintf(stderr, "init: invalid selection %q\n", tok)
					return 2
				}
				chosen = append(chosen, dets[n-1])
			}
		}
	}

	for _, d := range chosen {
		if err := agentinit.Install(d); err != nil {
			fmt.Fprintf(stderr, "init: %s: %v\n", d.Agent.Label, err)
			return 1
		}
		if d.Status == agentinit.StatusNew {
			fmt.Fprintf(stdout, "✓ installed %s → %s\n", d.Agent.Label, d.Target)
		} else {
			fmt.Fprintf(stdout, "✓ refreshed %s → %s\n", d.Agent.Label, d.Target)
		}
	}
	return 0
}

// printList renders the numbered checkbox listing.
func printList(w io.Writer, dets []agentinit.Detection) {
	fmt.Fprintln(w, "Detected agents:")
	for i, d := range dets {
		box := "[ ]"
		if d.Status.Checked() {
			box = "[x]"
		}
		fmt.Fprintf(w, "  %d. %s %-26s %s  %s\n", i+1, box, d.Agent.Label, d.Target, d.Status)
	}
}

// checkedDefaults returns the targets that already have the skill installed.
func checkedDefaults(dets []agentinit.Detection) []agentinit.Detection {
	var out []agentinit.Detection
	for _, d := range dets {
		if d.Status.Checked() {
			out = append(out, d)
		}
	}
	return out
}

func byAgentID(dets []agentinit.Detection, id string) (agentinit.Detection, bool) {
	for _, d := range dets {
		if d.Agent.ID == id {
			return d, true
		}
	}
	return agentinit.Detection{}, false
}
```

(b) In `internal/cli/cli.go`:

- Add next to `RepoStatePath`:

```go
// InitHomeDir is the home directory used for `gg init`'s home-scoped agent
// detection. "" skips home-scoped agents — cmd/gg wires the real home; tests
// stay hermetic by default.
var InitHomeDir string
```

- Add the dispatch case (before `default:`):

```go
	case "init":
		return cmdInit(workdir, rest, stdin, stdout, stderr)
```

- Add `"init": true` to the `commands` map.

(c) In `cmd/gg/main.go`:

- In `main()`, right after the `cli.RepoStatePath` line, add:

```go
	if home, err := os.UserHomeDir(); err == nil {
		cli.InitHomeDir = home
	}
```

- Update the help string to include `init`:

```go
		fmt.Fprintln(os.Stderr, "commands: status commit pull push switch stash undo worktree repo init inspect (run `gg` with no arguments for the TUI)")
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/cli/ -run Init -v` then `go test ./internal/cli/ ./cmd/gg/`
Expected: PASS (9 new tests), no regressions.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/cli/ cmd/gg/
go vet ./internal/cli/ ./cmd/gg/
git add internal/cli/ cmd/gg/
git commit -m "feat(cli): gg init installs the using-gg skill for detected agents"
```

---

### Task 4: TUI — `,` Settings popup

**Files:**
- Create: `internal/tui/settings_popup.go`
- Modify: `internal/tui/model.go`, `internal/tui/view.go`, `internal/tui/run.go`
- Test: `internal/tui/settings_popup_test.go`

Background: popup contract per the worktree/repo popups — pointer field on Model, key handler swallows everything, `overlayCenter` in `render()`. Routing precedence: modal → `m.popup` → `m.repoPopup` → **`m.settings` (new)** → filter input → normal keys. `keyMsg` helper already supports "esc", "enter", "up", "down", "ctrl+d"; space arrives as `tea.KeyMsg{Type: tea.KeySpace}` (add a `"space"` case).

- [ ] **Step 1: Extend the `keyMsg` helper**

In `internal/tui/model_test.go`, add:

```go
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace}
```

- [ ] **Step 2: Write the failing tests**

Create `internal/tui/settings_popup_test.go`:

```go
package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/agentskill"
)

// settingsModel: loaded model whose project dir contains .claude and an
// AGENTS.md that already has an OLD installed block (so defaults differ).
func settingsModel(t *testing.T) (Model, string) {
	t.Helper()
	dir, repo := newRepoDir(t)
	os.MkdirAll(filepath.Join(dir, ".claude"), 0o755)
	os.WriteFile(filepath.Join(dir, "AGENTS.md"),
		[]byte("mine\n\n<!-- gg:using-gg:v0:begin -->\nold\n<!-- gg:using-gg:end -->\n"), 0o644)
	m := New(repo)
	u, _ := m.Update(m.loadCmd()())
	m = u.(Model)
	return m, dir
}

func TestCommaOpensSettingsMenu(t *testing.T) {
	m, _ := settingsModel(t)
	u, _ := m.Update(keyMsg(","))
	m = u.(Model)
	if m.settings == nil {
		t.Fatal(", should open the settings popup")
	}
	if m.settings.picker {
		t.Fatal("should open on the menu screen, not the picker")
	}
	out := m.View()
	if !strings.Contains(out, "Settings") || !strings.Contains(out, "agent") {
		t.Fatalf("menu content missing:\n%s", out)
	}
}

func TestPickerCheckboxDefaults(t *testing.T) {
	m, _ := settingsModel(t)
	u, _ := m.Update(keyMsg(","))
	m = u.(Model)
	u, _ = m.Update(keyMsg("enter")) // menu entry -> picker
	m = u.(Model)
	if !m.settings.picker {
		t.Fatal("enter on the menu entry should open the picker")
	}
	// claude-project (.claude, new) unchecked; agents-md (old block) checked.
	byID := map[string]bool{}
	for i, d := range m.settings.dets {
		byID[d.Agent.ID] = m.settings.checked[i]
	}
	if byID["claude-project"] {
		t.Error("new target must default unchecked")
	}
	if !byID["agents-md"] {
		t.Error("already-installed target must default checked")
	}
}

func TestPickerToggleAndApply(t *testing.T) {
	m, dir := settingsModel(t)
	u, _ := m.Update(keyMsg(","))
	m = u.(Model)
	u, _ = m.Update(keyMsg("enter"))
	m = u.(Model)
	// Move to the claude-project row and check it.
	idx := -1
	for i, d := range m.settings.dets {
		if d.Agent.ID == "claude-project" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatal("claude-project not in picker")
	}
	m.settings.sel = idx
	u, _ = m.Update(keyMsg("space"))
	m = u.(Model)
	if !m.settings.checked[idx] {
		t.Fatal("space should toggle the checkbox")
	}
	u, _ = m.Update(keyMsg("enter")) // apply
	m = u.(Model)
	if m.settings != nil {
		t.Fatal("apply should close the popup")
	}
	// claude-project installed AND agents-md refreshed (was checked by default).
	skill, err := os.ReadFile(filepath.Join(dir, ".claude", "skills", "using-gg", "SKILL.md"))
	if err != nil || agentskill.InstalledVersion(skill) != agentskill.Version {
		t.Errorf("claude skill not installed: %v", err)
	}
	agents, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if agentskill.InstalledVersion(agents) != agentskill.Version {
		t.Error("agents-md not refreshed")
	}
	if !strings.Contains(string(agents), "mine") {
		t.Error("surrounding AGENTS.md content lost")
	}
	if m.statusMsg == "" {
		t.Error("apply should report in the status line")
	}
}

func TestSettingsEscBackThenClose(t *testing.T) {
	m, _ := settingsModel(t)
	u, _ := m.Update(keyMsg(","))
	m = u.(Model)
	u, _ = m.Update(keyMsg("enter"))
	m = u.(Model)
	u, _ = m.Update(keyMsg("esc")) // picker -> menu
	m = u.(Model)
	if m.settings == nil || m.settings.picker {
		t.Fatal("esc in the picker should go back to the menu")
	}
	u, _ = m.Update(keyMsg("esc")) // menu -> closed
	m = u.(Model)
	if m.settings != nil {
		t.Fatal("esc on the menu should close the popup")
	}
}

func TestSettingsSwallowsGlobalKeys(t *testing.T) {
	m, _ := settingsModel(t)
	u, _ := m.Update(keyMsg(","))
	m = u.(Model)
	u, _ = m.Update(keyMsg("p"))
	m = u.(Model)
	if m.running {
		t.Fatal("settings popup leaked a global key")
	}
	if m.settings == nil {
		t.Fatal("popup should still be open")
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run 'Settings|Picker|Comma' -v`
Expected: FAIL — `m.settings` undefined.

- [ ] **Step 4: Implement the popup**

Create `internal/tui/settings_popup.go`:

```go
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/agentinit"
)

// settingsPopup is the generic Settings surface opened with `,`. v1 has a
// single menu entry (agent-skill setup); the menu/picker split exists so
// future options have a home.
type settingsPopup struct {
	picker  bool // false = menu screen, true = agent picker
	dets    []agentinit.Detection
	checked []bool
	sel     int
}

const settingsMenuAgents = "Set up agent skills (using-gg)"

// openSettings opens the menu screen.
func (m Model) openSettings() Model {
	m.settings = &settingsPopup{}
	return m
}

// openAgentPicker populates the picker from a fresh detection pass. The
// checkbox defaults encode the core rule: already-installed targets start
// checked (apply = refresh); new targets start unchecked (explicit opt-in).
func (m Model) openAgentPicker() Model {
	p := m.settings
	p.dets = agentinit.Detect(m.currentWorktree, m.initHomeDir)
	p.checked = make([]bool, len(p.dets))
	for i, d := range p.dets {
		p.checked[i] = d.Status.Checked()
	}
	p.sel = 0
	p.picker = true
	return m
}

// updateSettingsKey handles all keys while the settings popup is open.
func (m Model) updateSettingsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.settings
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		if p.picker {
			p.picker = false
			return m, nil
		}
		m.settings = nil
		return m, nil
	}
	if !p.picker {
		switch msg.Type {
		case tea.KeyEnter:
			return m.openAgentPicker(), nil
		}
		return m, nil // single menu entry: up/down are no-ops in v1
	}
	switch msg.Type {
	case tea.KeyUp:
		if p.sel > 0 {
			p.sel--
		}
	case tea.KeyDown:
		if p.sel < len(p.dets)-1 {
			p.sel++
		}
	case tea.KeySpace:
		if p.sel >= 0 && p.sel < len(p.checked) {
			p.checked[p.sel] = !p.checked[p.sel]
		}
	case tea.KeyEnter:
		installed, refreshed, failed := 0, 0, 0
		for i, d := range p.dets {
			if !p.checked[i] {
				continue
			}
			if err := agentinit.Install(d); err != nil {
				failed++
				continue
			}
			if d.Status == agentinit.StatusNew {
				installed++
			} else {
				refreshed++
			}
		}
		m.settings = nil
		m.statusMsg = fmt.Sprintf("agent skills: %d installed, %d refreshed", installed, refreshed)
		if failed > 0 {
			m.statusMsg += fmt.Sprintf(", %d failed", failed)
		}
		return m, nil
	}
	return m, nil
}

// renderSettingsPopup draws whichever screen is active.
func (m Model) renderSettingsPopup() string {
	p := m.settings
	var b strings.Builder
	if !p.picker {
		b.WriteString("Settings\n\n")
		b.WriteString("> " + settingsMenuAgents + "\n")
		b.WriteString("\n[enter] open  [esc] close")
	} else {
		b.WriteString("Set up agent skills\n\n")
		if len(p.dets) == 0 {
			b.WriteString("  no supported agents detected\n")
		}
		for i, d := range p.dets {
			cursor := "  "
			if i == p.sel {
				cursor = "> "
			}
			box := "[ ]"
			if p.checked[i] {
				box = "[x]"
			}
			b.WriteString(fmt.Sprintf("%s%s %s — %s\n", cursor, box, d.Agent.Label, d.Status))
		}
		b.WriteString("\n[space] toggle  [enter] apply  [esc] back")
	}
	w := m.width
	if w <= 0 {
		w = 80
	}
	inner := 56
	if max := w - 8; inner > max {
		inner = max
	}
	if inner < 20 {
		inner = 20
	}
	return modalStyle.Width(inner).Render(strings.TrimRight(b.String(), "\n")) + "\n"
}
```

- [ ] **Step 5: Wire Model, routing, view, run**

(a) `internal/tui/model.go` — add fields next to `repoPopup`:

```go
	settings    *settingsPopup
	initHomeDir string // home dir for agent detection; "" skips home-scoped agents (tests)
```

(b) Routing: after the `if m.repoPopup != nil { ... }` dispatch and before `if m.filterTyping {`, add:

```go
		if m.settings != nil {
			return m.updateSettingsKey(msg)
		}
```

(c) `,` key in the normal-key switch, after `case "R":`:

```go
		case ",":
			if !m.running && !m.loading {
				return m.openSettings(), nil
			}
```

(d) `internal/tui/view.go` — in `render()`, after the `repoPopup` overlay branch:

```go
	if m.settings != nil {
		w, h := m.width, m.height
		if w <= 0 {
			w = 80
		}
		if h <= 0 {
			h = 24
		}
		return overlayCenter(bg, m.renderSettingsPopup(), w, h)
	}
```

(e) `internal/tui/view.go` — footer gains `[,] settings` before the `•`:

```go
	footer := truncate("[p]ull [P]ush [s]witch [S]tash [u]ndo [w]orktree [d]elete [o]rder [/]filter [R]epo [,] settings  •  [tab] focus  [r] reload  [q] quit", g.w)
```

(f) `internal/tui/run.go` — in `Run`, next to the `statePath` wiring:

```go
	if home, err := os.UserHomeDir(); err == nil {
		m.initHomeDir = home
	}
```

(add `os` to run.go's imports).

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run 'Settings|Picker|Comma' -v` then full `go test ./internal/tui/`.
Expected: PASS (5 new tests), no regressions (existing tests leave `initHomeDir` empty → home-scoped agents never appear → nothing outside temp dirs is written).

- [ ] **Step 7: Commit**

```bash
gofmt -w internal/tui/
go vet ./internal/tui/
git add internal/tui/
git commit -m "feat(tui): , opens Settings with the agent-skill picker"
```

---

### Task 5: Convention + docs + dogfood + final gate

**Files:**
- Modify: `.claude/skills/adding-features/SKILL.md`, `CLAUDE.md`, `CHANGELOG.md`, `README.md`
- Create (generated): `.claude/skills/using-gg/SKILL.md`

- [ ] **Step 1: The convention, in the adding-features skill**

In `.claude/skills/adding-features/SKILL.md`, in the wiring-checklist table, add a row after row 7 (`cmd/gg/main.go`):

```markdown
| 7b | `internal/agentskill/using-gg.md` | **CLI surface changed (commands/flags/decision IDs)?** Update the embedded using-gg skill, bump `agentskill.Version`, and re-run `gg init` (or `gg init --update`) wherever it's installed. |
```

- [ ] **Step 2: The convention, in CLAUDE.md**

In `CLAUDE.md`, extend the docs-update paragraph:

```markdown
**After each completed stage/feature, update the project docs:**
`CHANGELOG.md` (always), `README.md` (if user-facing surface changed), this
`CLAUDE.md` (if the architecture/package map/conventions changed), and — when
the CLI surface changed — `internal/agentskill/using-gg.md` (bump
`agentskill.Version`, then `gg init --update` to refresh installed copies).
```

And add two package-map rows (near `repos`):

```markdown
| `agentskill` | The embedded "using-gg" skill (go:embed + version marker) that teaches AI agents the gg CLI. |
| `agentinit`  | Hardcoded agent registry + detect/status/install behind `gg init` and the TUI Settings popup. |
```

- [ ] **Step 3: CHANGELOG + README**

`CHANGELOG.md` under `### Added` (after `#### Repo switcher`):

```markdown
#### Agent init
- `gg init` detects installed AI coding agents (Claude Code, Junie, Codex,
  OpenCode, Cursor, AGENTS.md, …) and installs an embedded "using-gg" skill
  teaching them to drive git through the gg CLI. Already-installed targets are
  checked by default (applying refreshes them); new agents are explicit
  opt-in. `--all`, `--update`, `--agents <ids>`, `--list` for scripting.
- TUI: `,` opens a Settings popup with the same agent-skill picker.
- The skill ships inside the gg binary (version-marked); installed copies
  change only when a newer binary's init runs.
```

`README.md`: key-table row after `R`:

```markdown
| `,` | settings (set up agent skills) |
```

CLI block after `gg repo switch <query>`:

```
gg init [--all | --update | --agents <ids> | --list]
```

- [ ] **Step 4: Dogfood — install the skill into this repo**

```bash
go build -o ./gg ./cmd/gg
./gg init --agents claude-project
git add .claude/skills/using-gg/SKILL.md
```

Verify the generated file starts with the `name: using-gg` frontmatter and
carries the `gg:using-gg:v1` marker.

- [ ] **Step 5: Full verification**

```bash
gofmt -l internal/ cmd/        # must print nothing
go vet ./...
go test ./... -race
```

Expected: all clean / PASS.

- [ ] **Step 6: Commit**

```bash
git add .claude/ CLAUDE.md CHANGELOG.md README.md
git commit -m "docs: agent-init convention + dogfood the using-gg skill"
```

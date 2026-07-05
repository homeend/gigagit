# Relocatable Per-Repo Settings Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a third config tier — per-repo settings stored in the user config directory (`~/.config/gg/projects/<encoded-repo-path>/config.toml`) instead of the committed `.gg.toml` — plus a Settings action to copy/move the whole per-repo config between the two locations.

**Architecture:** `internal/config` gains a path resolver (`EncodeRepoKey`/`PrivateRepoPath`), whole-file relocation writers (`CopyRepoConfig`/`RemoveRepoConfig`), and an active-file resolver (`ActiveRepoConfigPath` — the private file if it exists, else committed). `Load` is **unchanged** (2-arg): the two TUI load paths and the CLI worktree path resolve the active path and pass it as `Load`'s `repoPath`, and set the per-repo write target to that same path — one active file is read AND written (pure replace, not layering). A new `repoConfigPopup` in the Settings menu drives the copy/move.

**Tech Stack:** Go 1.26, `github.com/pelletier/go-toml/v2`, Bubble Tea (Elm-style value-receiver `Model`), lipgloss.

## Global Constraints

- Module `github.com/homeend/gigagit`; Go 1.26. Shell out to system `git` only via `gitcmd`/`gitexec` (not relevant to this feature — no new git verbs).
- `internal/config` stays free of `internal/git`/`internal/domain` imports (pure config + fs I/O). Callers resolve the main-worktree path and pass it in.
- Config is read-only at runtime **except** the existing narrow line-edit writers; this feature adds whole-file copy/remove writers in the same package.
- Field-level overlay rule is unchanged (zero-is-unset / inverted-polarity per field). The private tier reuses `overlayWorktree`/`overlayUI`/`overlayDebug`/`overlayRefresh` verbatim.
- **Private file path (exact):** `$XDG_CONFIG_HOME/gg/projects/<EncodeRepoKey(mainWorktreePath)>/config.toml`, fallback base `~/.config` when `$XDG_CONFIG_HOME` is empty (identical base logic to `DefaultGlobalPath`).
- **Key encoding (exact):** `filepath.Clean` then replace every `/`, `\`, `:` with `-`. `/mnt/t/others/gigagit` → `-mnt-t-others-gigagit`; `C:\src\repo` → `C--src-repo`; `""` → `""`.
- **Precedence (exact):** `defaults → global → active repo file`, where the active repo file is the private user-dir file if it exists on disk, else the committed `.gg.toml`. Read path and write target use the SAME active path (`ActiveRepoConfigPath`); two repo files are never layered.
- **Anchor:** the private file is keyed on the **main** worktree (`Worktrees()[0].Path`), so all linked worktrees share one private config. The committed `.gg.toml` stays anchored on the **current** worktree (`filepath.Join(top, ".gg.toml")`), unchanged.
- Tests use `t.TempDir()` + the existing `writeFile` helper in `config_test.go`; TUI pure-logic tests need no `*domain.Service` (pass `nil`).

---

### Task 1: Config path helpers (`EncodeRepoKey` / `PrivateRepoPath`)

**Files:**
- Modify: `internal/config/config.go` (add `strings` import; add `configHome`, `EncodeRepoKey`, `PrivateRepoPath`; refactor `DefaultGlobalPath`)
- Test: `internal/config/config_test.go`

> **Note:** `Load` is NOT changed. The per-repo tier is read from ONE active file
> (private if it exists, else committed) — resolved by `ActiveRepoConfigPath`
> (Task 2) and passed as `Load`'s existing `repoPath` arg (Task 3). There is no
> layering of two repo files (that would let a committed inverted-polarity key
> shadow a private "off"; see spec §2).

**Interfaces:**
- Produces:
  - `func EncodeRepoKey(repoPath string) string`
  - `func PrivateRepoPath(mainWorktreePath string) string`

- [ ] **Step 1: Write the failing tests**

Add to `internal/config/config_test.go`:

```go
func TestEncodeRepoKey(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/mnt/t/others/gigagit", "-mnt-t-others-gigagit"},
		{"/mnt/t/others/gigagit/", "-mnt-t-others-gigagit"}, // trailing slash cleaned
		{`C:\src\repo`, "C--src-repo"},                       // drive colon + backslashes
		{"", ""},                                             // empty in, empty out
	}
	for _, c := range cases {
		if got := EncodeRepoKey(c.in); got != c.want {
			t.Errorf("EncodeRepoKey(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPrivateRepoPathXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	got := PrivateRepoPath("/mnt/t/others/gigagit")
	want := filepath.Join("/xdg", "gg", "projects", "-mnt-t-others-gigagit", "config.toml")
	if got != want {
		t.Errorf("PrivateRepoPath = %q, want %q", got, want)
	}
}

func TestPrivateRepoPathEmptyAnchor(t *testing.T) {
	if got := PrivateRepoPath(""); got != "" {
		t.Errorf("empty anchor should yield empty path, got %q", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/relocatable-repo-config && go test ./internal/config/ -run 'TestEncodeRepoKey|TestPrivateRepoPath' -v`
Expected: FAIL — `undefined: EncodeRepoKey`, `undefined: PrivateRepoPath`.

- [ ] **Step 3: Implement**

In `internal/config/config.go`, add `"strings"` to the import block. Add the `configHome` helper and refactor `DefaultGlobalPath` to use it (output unchanged, so `TestDefaultGlobalPathXDG`/`TestDefaultGlobalPathHome` keep passing):

```go
// configHome returns the base config directory: $XDG_CONFIG_HOME, else
// ~/.config (empty home ⇒ ".config" relative). Shared by DefaultGlobalPath and
// PrivateRepoPath so the two paths always live under the same root.
func configHome() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = ""
		}
		base = filepath.Join(home, ".config")
	}
	return base
}
```

Replace the body of `DefaultGlobalPath`:

```go
// DefaultGlobalPath returns the global config path, honoring $XDG_CONFIG_HOME
// and falling back to ~/.config/gg/config.toml.
func DefaultGlobalPath() string {
	return filepath.Join(configHome(), "gg", "config.toml")
}
```

Add the two new helpers (put them next to `DefaultGlobalPath`):

```go
// EncodeRepoKey turns an absolute repo path into a filesystem-safe, readable
// directory name by replacing every path separator and drive colon (/, \, :)
// with '-'. So /mnt/t/others/gigagit -> -mnt-t-others-gigagit and C:\src\repo
// -> C--src-repo. Empty in yields empty out (the caller must guard).
func EncodeRepoKey(repoPath string) string {
	if repoPath == "" {
		return ""
	}
	cleaned := filepath.Clean(repoPath)
	return strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':':
			return '-'
		}
		return r
	}, cleaned)
}

// PrivateRepoPath returns the machine-local per-repo config path for a repo
// whose MAIN worktree is at mainWorktreePath:
// $XDG_CONFIG_HOME/gg/projects/<encoded>/config.toml. Returns "" if
// mainWorktreePath is "" (no anchor ⇒ no private path). Anchored on the main
// worktree so every linked worktree of a repo shares one private config.
func PrivateRepoPath(mainWorktreePath string) string {
	if mainWorktreePath == "" {
		return ""
	}
	return filepath.Join(configHome(), "gg", "projects", EncodeRepoKey(mainWorktreePath), "config.toml")
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/config/ -v`
Expected: PASS (new tests + all existing config tests unchanged, including the 2-arg `Load` tests and `TestDefaultGlobalPath*`).

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): private per-repo config path helpers

EncodeRepoKey/PrivateRepoPath resolve a machine-local per-repo config
under ~/.config/gg/projects/<encoded>/config.toml, keyed on the main
worktree path (Claude-style readable key).

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01BSUWe3VPzxFgnqmZKQ9chg"
```

---

### Task 2: Relocation writers + active-target resolver

**Files:**
- Modify: `internal/config/write.go` (add `CopyRepoConfig`, `RemoveRepoConfig`, `ActiveRepoConfigPath`)
- Test: `internal/config/write_test.go`

**Interfaces:**
- Consumes: `atomicWriteFile` (existing, `write.go`).
- Produces:
  - `func CopyRepoConfig(src, dst string) error`
  - `func RemoveRepoConfig(path string) error`
  - `func ActiveRepoConfigPath(committedPath, privatePath string) string`

- [ ] **Step 1: Write the failing tests**

Add to `internal/config/write_test.go`:

```go
func TestCopyRepoConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.toml")
	dst := filepath.Join(dir, "sub", "b.toml") // parent dir must be created
	writeFile(t, src, "[ui]\ncommit_sort = \"plain\"\n")
	if err := CopyRepoConfig(src, dst); err != nil {
		t.Fatalf("CopyRepoConfig: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(got) != "[ui]\ncommit_sort = \"plain\"\n" {
		t.Errorf("copy mismatch: %q", got)
	}
}

func TestCopyRepoConfigMissingSrc(t *testing.T) {
	dir := t.TempDir()
	if err := CopyRepoConfig(filepath.Join(dir, "nope.toml"), filepath.Join(dir, "b.toml")); err == nil {
		t.Error("expected error copying a missing source")
	}
}

func TestRemoveRepoConfigAbsentIsNoop(t *testing.T) {
	dir := t.TempDir()
	if err := RemoveRepoConfig(filepath.Join(dir, "nope.toml")); err != nil {
		t.Errorf("removing an absent file should be a no-op, got %v", err)
	}
}

func TestActiveRepoConfigPath(t *testing.T) {
	dir := t.TempDir()
	committed := filepath.Join(dir, ".gg.toml")
	private := filepath.Join(dir, "private.toml")
	writeFile(t, committed, "")
	// private absent → committed
	if got := ActiveRepoConfigPath(committed, private); got != committed {
		t.Errorf("private absent: want committed %q, got %q", committed, got)
	}
	// private present → private
	writeFile(t, private, "")
	if got := ActiveRepoConfigPath(committed, private); got != private {
		t.Errorf("private present: want private %q, got %q", private, got)
	}
	// empty private path → committed
	if got := ActiveRepoConfigPath(committed, ""); got != committed {
		t.Errorf("empty private: want committed %q, got %q", committed, got)
	}
}
```

Confirm `write_test.go` already imports `os` and `path/filepath` (it uses `t.TempDir` + writes); add them if missing. The `writeFile` helper lives in `config_test.go` (same package) and is reused.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/config/ -run 'TestCopyRepoConfig|TestRemoveRepoConfig|TestActiveRepoConfigPath' -v`
Expected: FAIL — `undefined: CopyRepoConfig`, `undefined: RemoveRepoConfig`, `undefined: ActiveRepoConfigPath`.

- [ ] **Step 3: Implement**

Append to `internal/config/write.go`:

```go
// CopyRepoConfig copies the whole config file src → dst, creating dst's parent
// directories and writing atomically (temp file + rename, via atomicWriteFile).
// A missing src is an error the caller surfaces ("nothing to move"). Because the
// committed .gg.toml and the private file share the exact schema, this is a
// verbatim byte copy — no merge.
func CopyRepoConfig(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return atomicWriteFile(dst, data)
}

// RemoveRepoConfig deletes path. An absent path is not an error, so a move
// (copy + remove-source) is idempotent.
func RemoveRepoConfig(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ActiveRepoConfigPath resolves the active per-repo write target: the private
// user-dir file when it exists on disk, else the committed path. An empty
// privatePath (no main-worktree anchor) always yields committedPath. This is the
// rule that keeps a Settings toggle after "move to private" from recreating a
// committed .gg.toml.
func ActiveRepoConfigPath(committedPath, privatePath string) string {
	if privatePath != "" {
		if _, err := os.Stat(privatePath); err == nil {
			return privatePath
		}
	}
	return committedPath
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/config/ -v`
Expected: PASS (all config tests).

- [ ] **Step 5: Commit**

```bash
git add internal/config/write.go internal/config/write_test.go
git commit -m "feat(config): whole-file relocation writers + active-target resolver

CopyRepoConfig/RemoveRepoConfig move a per-repo config file between the
committed and private locations; ActiveRepoConfigPath picks the private
file when present so the per-repo write target follows the relocation.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01BSUWe3VPzxFgnqmZKQ9chg"
```

---

### Task 3: Wire the private tier into the TUI load paths and the CLI

**Files:**
- Modify: `internal/tui/source.go` (`bootstrapCmd` + `configReadyMsg` doc)
- Modify: `internal/tui/load.go` (`loadCmd` + `dataLoadedMsg.repoTOML` doc)
- Modify: `internal/cli/worktree.go` (the `config.Load` call ~line 111)

**Interfaces:**
- Consumes: `config.PrivateRepoPath`, `config.ActiveRepoConfigPath` (Tasks 1–2); the **unchanged** 2-arg `config.Load(global, active)`; `svc.Worktrees(ctx) ([]model.Worktree, error)` (existing; `wts[0].Path` is the main worktree, same anchor `TempExportBase` uses).
- Produces: `configReadyMsg.repoTOML` / `dataLoadedMsg.repoTOML` now carry the **active per-repo file** (private if it exists, else committed) — used for BOTH the `config.Load` read and `m.repoConfigPath` (write target). Both handlers already do `m.repoConfigPath = msg.repoTOML` — unchanged.

- [ ] **Step 1: Update `bootstrapCmd` (startup path)**

In `internal/tui/source.go`, replace the repo-config block inside `bootstrapCmd`'s returned closure:

```go
		cfg := config.Defaults()
		repoTOML, root := "", ""
		if top, err := svc.TopLevel(ctx); err == nil && top != "" {
			root = top
			committed := filepath.Join(top, ".gg.toml")
			privatePath := ""
			if wts, werr := svc.Worktrees(ctx); werr == nil && len(wts) > 0 && wts[0].Path != "" {
				privatePath = config.PrivateRepoPath(wts[0].Path)
			}
			// One active per-repo file: private if it exists, else committed. The
			// read path and the write target are the SAME path — no layering (a
			// committed inverted-polarity key must not shadow a private "off").
			repoTOML = config.ActiveRepoConfigPath(committed, privatePath)
			if c, cerr := config.Load(config.DefaultGlobalPath(), repoTOML); cerr == nil {
				cfg = c
			}
			if statePath != "" {
				_ = repos.Touch(statePath, top, time.Now())
			}
		}
```

Update the `configReadyMsg.repoTOML` field comment (in the `type configReadyMsg struct` just above `bootstrapCmd`):

```go
	repoTOML string // active per-repo write target: private user-dir file if present, else <repo-top>/.gg.toml; "" if not in a repo
```

- [ ] **Step 2: Update `loadCmd` (reRoot / legacy path)**

In `internal/tui/load.go`, replace the config-resolution block inside `loadCmd`'s returned closure (the lines computing `repoTOML`/`cfg` from `top`):

```go
		cfg := config.Defaults()
		repoTOML := ""
		top, topErr := svc.TopLevel(ctx)
		if topErr == nil && top != "" {
			committed := filepath.Join(top, ".gg.toml")
			privatePath := ""
			if wts, werr := svc.Worktrees(ctx); werr == nil && len(wts) > 0 && wts[0].Path != "" {
				privatePath = config.PrivateRepoPath(wts[0].Path)
			}
			// Same active-file resolution as bootstrapCmd: read + write target
			// are one path, private if present, else committed.
			repoTOML = config.ActiveRepoConfigPath(committed, privatePath)
			if c, cfgErr := config.Load(config.DefaultGlobalPath(), repoTOML); cfgErr == nil {
				cfg = c
			}
		}
```

Update the `dataLoadedMsg.repoTOML` field comment:

```go
	repoTOML string // active per-repo write target (private user-dir file if present, else <repo-top>/.gg.toml); rebinds repoConfigPath so per-repo Settings writes follow a repo switch AND a relocation
```

- [ ] **Step 3: Update the CLI worktree caller**

In `internal/cli/worktree.go`, replace the single `config.Load(...)` call (~line 111) with:

```go
	privatePath := ""
	if wts, werr := svc.Worktrees(ctxBg); werr == nil && len(wts) > 0 && wts[0].Path != "" {
		privatePath = config.PrivateRepoPath(wts[0].Path)
	}
	active := config.ActiveRepoConfigPath(filepath.Join(top, ".gg.toml"), privatePath)
	cfg, err := config.Load(config.DefaultGlobalPath(), active)
	if err != nil {
		fmt.Fprintln(stderr, "error: loading config:", err)
		return 1
	}
```

- [ ] **Step 4: Build + vet + full config tests**

Run: `go build ./... && go vet ./internal/tui/ ./internal/cli/ ./internal/config/ && go test ./internal/config/ ./internal/cli/ ./internal/tui/`
Expected: builds clean; PASS. (The wiring is thin; behavior is covered by `ActiveRepoConfigPath`'s unit test. No new TUI unit test here — the off-thread `svc.Worktrees` stat is impractical to isolate; Task 5 includes a live smoke test.)

- [ ] **Step 5: Commit**

```bash
git add internal/tui/source.go internal/tui/load.go internal/cli/worktree.go
git commit -m "feat(config): read private per-repo tier; write target follows it

bootstrapCmd/loadCmd/worktree.go now resolve the main-worktree-anchored
private config path, overlay it in config.Load, and set the per-repo
write target via ActiveRepoConfigPath so a Settings toggle after moving
to private stays in the private file.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01BSUWe3VPzxFgnqmZKQ9chg"
```

---

### Task 4: Settings "Repo settings location" popup

**Files:**
- Create: `internal/tui/repoconfig_popup.go`
- Create: `internal/tui/repoconfig_popup_test.go`
- Modify: `internal/tui/settings_popup.go` (menu const + slice + enter dispatch)

**Interfaces:**
- Consumes: `config.PrivateRepoPath`, `config.CopyRepoConfig`, `config.RemoveRepoConfig` (Tasks 1–2); `m.currentWorktree`, `m.worktrees` (model state, populated after load); `m.pushLayer`/`m.popLayer`, `m.loadCmd`, `m.loadGen`; render helpers `overlayCenter`/`clipToHeight`/`popupWideInnerWidth`/`popupTextWidth`/`popupBox`/`renderWindow`/`winRow`/`winOpts`/`selectedRow`/`wrapWidth`; the `layer` interface (`update(Model, tea.KeyMsg) (Model, tea.Cmd)`, `render(Model, string) string`).
- Produces: `func (m Model) openRepoConfigLocation() Model`; pure `func repoConfigActions(committedExists, privateExists, haveCommitted, havePrivate bool) []repoCfgAction`; `func repoCfgEndpoints(act repoCfgAction, committed, private string) (src, dst string, isMove bool)`.

- [ ] **Step 1: Write the failing pure-logic tests**

Create `internal/tui/repoconfig_popup_test.go`:

```go
package tui

import "testing"

func TestRepoConfigActions(t *testing.T) {
	// committed present, private absent → offer copy/move to private only.
	if got := repoConfigActions(true, false, true, true); len(got) != 2 ||
		got[0] != actCopyToPrivate || got[1] != actMoveToPrivate {
		t.Errorf("committed-only actions = %v", got)
	}
	// private present, committed absent → offer copy/move to committed only.
	if got := repoConfigActions(false, true, true, true); len(got) != 2 ||
		got[0] != actCopyToCommitted || got[1] != actMoveToCommitted {
		t.Errorf("private-only actions = %v", got)
	}
	// both present → all four.
	if got := repoConfigActions(true, true, true, true); len(got) != 4 {
		t.Errorf("both-present should offer 4 actions, got %d", len(got))
	}
	// neither present → none.
	if got := repoConfigActions(false, false, true, true); len(got) != 0 {
		t.Errorf("nothing present should offer no actions, got %v", got)
	}
	// no private path available (no anchor) → no to-private actions even if committed exists.
	if got := repoConfigActions(true, false, true, false); len(got) != 0 {
		t.Errorf("no private path should offer nothing, got %v", got)
	}
}

func TestRepoCfgEndpoints(t *testing.T) {
	c, p := "/repo/.gg.toml", "/priv/config.toml"
	cases := []struct {
		act        repoCfgAction
		src, dst   string
		isMove     bool
	}{
		{actCopyToPrivate, c, p, false},
		{actMoveToPrivate, c, p, true},
		{actCopyToCommitted, p, c, false},
		{actMoveToCommitted, p, c, true},
	}
	for _, tc := range cases {
		src, dst, isMove := repoCfgEndpoints(tc.act, c, p)
		if src != tc.src || dst != tc.dst || isMove != tc.isMove {
			t.Errorf("endpoints(%v) = (%q,%q,%v), want (%q,%q,%v)",
				tc.act, src, dst, isMove, tc.src, tc.dst, tc.isMove)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestRepoConfigActions|TestRepoCfgEndpoints' -v`
Expected: FAIL — `undefined: repoConfigActions`, `undefined: actCopyToPrivate`, `undefined: repoCfgEndpoints`.

- [ ] **Step 3: Create the popup**

Create `internal/tui/repoconfig_popup.go`:

```go
package tui

import (
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/config"
)

// repoCfgAction is one whole-file relocation of the per-repo config between the
// committed .gg.toml and the private user-dir file.
type repoCfgAction int

const (
	actCopyToPrivate repoCfgAction = iota
	actMoveToPrivate
	actCopyToCommitted
	actMoveToCommitted
)

func repoCfgActionLabel(a repoCfgAction) string {
	switch a {
	case actCopyToPrivate:
		return "Copy to private (user dir)"
	case actMoveToPrivate:
		return "Move to private (user dir)"
	case actCopyToCommitted:
		return "Copy to committed (.gg.toml)"
	case actMoveToCommitted:
		return "Move to committed (.gg.toml)"
	}
	return "?"
}

// repoConfigActions lists the applicable relocation actions given which files
// exist and which paths are available. To-private needs the committed file to
// exist and a private path to write to; to-committed is the mirror.
func repoConfigActions(committedExists, privateExists, haveCommitted, havePrivate bool) []repoCfgAction {
	var a []repoCfgAction
	if committedExists && havePrivate {
		a = append(a, actCopyToPrivate, actMoveToPrivate)
	}
	if privateExists && haveCommitted {
		a = append(a, actCopyToCommitted, actMoveToCommitted)
	}
	return a
}

// repoCfgEndpoints maps an action to its (source, destination, isMove).
func repoCfgEndpoints(act repoCfgAction, committed, private string) (src, dst string, isMove bool) {
	switch act {
	case actCopyToPrivate:
		return committed, private, false
	case actMoveToPrivate:
		return committed, private, true
	case actCopyToCommitted:
		return private, committed, false
	case actMoveToCommitted:
		return private, committed, true
	}
	return "", "", false
}

// repoConfigPopup lets the user copy/move the whole per-repo config between the
// committed .gg.toml and the private user-dir file.
type repoConfigPopup struct {
	committedPath string
	privatePath   string
	committedEx   bool
	privateEx     bool
	actions       []repoCfgAction
	sel           int
	confirm       bool          // overwrite confirmation is showing
	pending       repoCfgAction // action awaiting confirmation
}

// openRepoConfigLocation builds the popup from current model state. The
// committed path is anchored on the CURRENT worktree; the private path on the
// MAIN worktree (worktrees[0]) so all worktrees of the repo share it.
func (m Model) openRepoConfigLocation() Model {
	committed := ""
	if m.currentWorktree != "" {
		committed = filepath.Join(m.currentWorktree, ".gg.toml")
	}
	private := ""
	if len(m.worktrees) > 0 && m.worktrees[0].Path != "" {
		private = config.PrivateRepoPath(m.worktrees[0].Path)
	}
	p := &repoConfigPopup{committedPath: committed, privatePath: private}
	p.refresh()
	return m.pushLayer(p)
}

func repoCfgFileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func repoCfgFileNonEmpty(path string) bool {
	if path == "" {
		return false
	}
	st, err := os.Stat(path)
	return err == nil && st.Size() > 0
}

// refresh recomputes existence + the applicable action list.
func (p *repoConfigPopup) refresh() {
	p.committedEx = repoCfgFileExists(p.committedPath)
	p.privateEx = repoCfgFileExists(p.privatePath)
	p.actions = repoConfigActions(p.committedEx, p.privateEx, p.committedPath != "", p.privatePath != "")
	if p.sel >= len(p.actions) {
		p.sel = 0
	}
}

func (p *repoConfigPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if p.confirm {
		switch msg.String() {
		case "y", "Y", "enter":
			p.confirm = false
			return p.run(m, p.pending)
		default: // n / esc / anything else cancels
			p.confirm = false
			return m, nil
		}
	}
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyUp:
		if p.sel > 0 {
			p.sel--
		}
		return m, nil
	case tea.KeyDown:
		if p.sel < len(p.actions)-1 {
			p.sel++
		}
		return m, nil
	case tea.KeyEnter:
		if len(p.actions) == 0 {
			return m, nil
		}
		act := p.actions[p.sel]
		_, dst, _ := repoCfgEndpoints(act, p.committedPath, p.privatePath)
		if repoCfgFileNonEmpty(dst) { // overwriting real content → confirm first
			p.confirm = true
			p.pending = act
			return m, nil
		}
		return p.run(m, act)
	}
	return m, nil
}

// run performs the file op then closes the popup and triggers a full reload so
// the new file layout re-applies (config re-read, write target rebound via
// ActiveRepoConfigPath, feed/status re-walked in any new sort).
func (p *repoConfigPopup) run(m Model, act repoCfgAction) (Model, tea.Cmd) {
	src, dst, isMove := repoCfgEndpoints(act, p.committedPath, p.privatePath)
	if err := config.CopyRepoConfig(src, dst); err != nil {
		m.statusMsg = "repo settings: " + err.Error()
		return m, nil
	}
	if isMove {
		if err := config.RemoveRepoConfig(src); err != nil {
			m.statusMsg = "repo settings: copied but source not removed: " + err.Error()
			m = m.popLayer()
			m.loadGen++
			return m, m.loadCmd()
		}
	}
	m.statusMsg = "repo settings: " + repoCfgActionLabel(act) + " done"
	m = m.popLayer()
	m.loadGen++
	return m, m.loadCmd()
}

func (p *repoConfigPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

func slotDisplay(path string, exists bool) string {
	if path == "" {
		return "(unavailable)"
	}
	if exists {
		return "present  " + path
	}
	return "absent   " + path
}

func (p *repoConfigPopup) box(m Model) string {
	w, _ := m.overlayDims()
	inner := popupWideInnerWidth(w) // paths are long
	textW := popupTextWidth(inner)
	var b strings.Builder
	b.WriteString("Repo settings location\n\n")
	b.WriteString("  committed  " + slotDisplay(p.committedPath, p.committedEx) + "\n")
	b.WriteString("  private    " + slotDisplay(p.privatePath, p.privateEx) + "\n\n")

	if p.confirm {
		_, dst, _ := repoCfgEndpoints(p.pending, p.committedPath, p.privatePath)
		for _, seg := range wrapWidth("Overwrite "+dst+" ?", textW, 1<<20) {
			b.WriteString(seg + "\n")
		}
		b.WriteString("\n[y] overwrite  [n/esc] cancel")
		return popupBox(inner, strings.TrimRight(b.String(), "\n"))
	}

	if len(p.actions) == 0 {
		b.WriteString("  (nothing to move — no per-repo config here, or not in a repo)\n")
		b.WriteString("\n[esc] close")
		return popupBox(inner, strings.TrimRight(b.String(), "\n"))
	}

	wr := make([]winRow, len(p.actions))
	for i, a := range p.actions {
		prefix := "  "
		var st lipgloss.Style
		if i == p.sel {
			prefix, st = "> ", selectedRow
		}
		wr[i] = winRow{text: prefix + repoCfgActionLabel(a), style: st}
	}
	for _, line := range renderWindow(wr, winOpts{w: textW, h: len(p.actions), mode: modeCutoff, anchor: p.sel}) {
		b.WriteString(line + "\n")
	}
	b.WriteString("\nactive file = private if present; move deletes the source (may dirty a tracked .gg.toml)")
	b.WriteString("\n[↑/↓] select  [enter] do  [esc] close")
	return popupBox(inner, strings.TrimRight(b.String(), "\n"))
}
```

- [ ] **Step 4: Wire the Settings menu row**

In `internal/tui/settings_popup.go`:

Add the const (in the `const (...)` block with the other `settingsMenu*`):

```go
	settingsMenuRepoLoc     = "Repo settings location"
```

Add it to the `settingsMenu` slice (place it right after `settingsMenuShowGraph`):

```go
var settingsMenu = []string{settingsMenuAgents, settingsMenuIdentity, settingsMenuPrefixes, settingsMenuHook, settingsMenuOpLog, settingsMenuErrors, settingsMenuAutoRefresh, settingsMenuRemoteTags, settingsMenuRates, settingsMenuCommitSort, settingsMenuShowGraph, settingsMenuRepoLoc, settingsMenuCommitGraph, settingsMenuGitConfig}
```

Add the dispatch case in the enter handler `switch settingsMenu[p.menuSel]` (next to `settingsMenuShowGraph`):

```go
			case settingsMenuRepoLoc:
				return m.openRepoConfigLocation(), nil
```

(No `settingsMenuLabel` change — the row is a static opener like Identity/Git config explorer.)

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestRepoConfigActions|TestRepoCfgEndpoints' -v && go build ./...`
Expected: PASS; builds clean.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/repoconfig_popup.go internal/tui/repoconfig_popup_test.go internal/tui/settings_popup.go
git commit -m "feat(tui): Settings 'Repo settings location' copy/move popup

New settings row opens a popup that copies or moves the whole per-repo
config between the committed .gg.toml and the private user-dir file, then
full-reloads so the write target and effective config re-resolve.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01BSUWe3VPzxFgnqmZKQ9chg"
```

---

### Task 5: Docs + live smoke verification

**Files:**
- Modify: `CHANGELOG.md`
- Modify: `README.md`
- Modify: `CLAUDE.md`
- Modify: `.claude/skills/adding-config-entries/SKILL.md`

- [ ] **Step 1: CHANGELOG**

Add under the `## [Unreleased]` → `### Added` list (top):

```markdown
- **Relocatable per-repo settings.** Per-repo settings can now live in a private
  machine-local file (`~/.config/gg/projects/<encoded-repo-path>/config.toml`)
  instead of the committed `.gg.toml`, so personal preferences on a shared repo
  are never committed. gg reads ONE active per-repo file — the private file when
  it exists, else the committed `.gg.toml` — layered over global
  (`defaults → global → active repo file`); per-repo Settings writes target the
  same active file. Settings → **"Repo settings location"** copies or moves the
  whole config between the two locations.
```

- [ ] **Step 2: README**

Add a subsection to the configuration section documenting the three tiers and the private path. Draft:

```markdown
### Config precedence

gg merges configuration field-by-field, later wins:

1. Built-in defaults
2. Global — `~/.config/gg/config.toml`
3. Active per-repo file — **one** file, whichever exists:
   - `~/.config/gg/projects/<encoded-repo-path>/config.toml` (machine-local
     private file; used when present), else
   - `<repo>/.gg.toml` (committed; tracked and shared with everyone who clones)

The private per-repo file lets you keep personal preferences on a shared repo
without committing them. It is keyed on the repo's main-worktree path, so every
linked worktree shares one private config, and when it exists it *replaces* the
committed `.gg.toml` for that repo (per-repo Settings writes also target it).
Settings (`,`) → **Repo settings location** copies or moves the whole config
between the committed and private locations.

On a shared repo the committed `.gg.toml` is git-tracked, so prefer **Copy to
private** — it keeps the committed team baseline in place while your private
file takes effect. **Move to private** deletes `.gg.toml`, which leaves a pending
git deletion in a shared repo.
```

- [ ] **Step 3: CLAUDE.md**

In the `config` package row of the package map, update the two-tier description to three tiers and note the new API. Add this sentence to that row:

```
Per-repo config now has TWO possible homes: the committed `<repo>/.gg.toml` and a machine-local private file `PrivateRepoPath(mainWorktree)` = `$XDG_CONFIG_HOME/gg/projects/<EncodeRepoKey(mainWorktree)>/config.toml` (keyed on the MAIN worktree so all linked worktrees share it). `Load` is unchanged (2-arg): the caller resolves `ActiveRepoConfigPath(committed, private)` — the private file if it exists on disk, else committed — and passes THAT single path as `repoPath`, so gg reads AND writes one active per-repo file (pure replace, not layering — a committed inverted-polarity/zero-is-unset key must never shadow a private "off"). `CopyRepoConfig`/`RemoveRepoConfig` back the whole-file copy/move. TUI `repoconfig_popup.go` (Settings "Repo settings location") drives it.
```

- [ ] **Step 4: adding-config-entries skill**

Open `.claude/skills/adding-config-entries/SKILL.md`; wherever it states the overlay is "defaults → global → repo", update the repo tier to note it is **one active file** — `PrivateRepoPath(mainWorktree)` when it exists on disk, else `<repo>/.gg.toml` (resolved by `config.ActiveRepoConfigPath`). `Load` itself is unchanged (still 2-arg); the caller passes the active path. Keep the change minimal — one or two sentences in the precedence explanation.

- [ ] **Step 5: Full test suite (with race)**

Run: `./test.sh race`
Expected: vet+gofmt clean; all unit + e2e tests PASS.

- [ ] **Step 6: Live smoke test (build + drive the real feature)**

```bash
go build -o /mnt/t/others/gigagit/.claude/worktrees/relocatable-repo-config/gg ./cmd/gg
```

Then, in the worktree:
1. Ensure a repo `.gg.toml` exists (e.g. toggle "Show graph" once, or `./gg config init --repo`).
2. Launch `./gg`, open Settings (`,`), choose **Repo settings location**, pick **Move to private**.
3. Verify: `ls ~/.config/gg/projects/*/config.toml` exists and `<repo>/.gg.toml` is gone.
4. In the running TUI, toggle a per-repo setting (e.g. Commit sort); confirm no `<repo>/.gg.toml` reappears (`ls <repo>/.gg.toml` → absent) and the private file's mtime updated.
5. Reopen Settings → Repo settings location → **Move to committed**; verify `<repo>/.gg.toml` returns and the private file is gone.

- [ ] **Step 7: Commit**

```bash
git add CHANGELOG.md README.md CLAUDE.md .claude/skills/adding-config-entries/SKILL.md
git commit -m "docs: relocatable per-repo settings (private user-dir config)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01BSUWe3VPzxFgnqmZKQ9chg"
```

---

## Notes for the executor

- **Anchor asymmetry is intentional:** committed `.gg.toml` on the current worktree, private file on the main worktree. Do not "fix" one to match the other.
- **`Load` stays 2-arg:** the caller resolves the active path (`ActiveRepoConfigPath`) and passes it as `repoPath`. Do NOT add a private-path parameter, a variadic, or a second load function, and do NOT layer two repo files (a committed inverted-polarity key would shadow a private "off").
- **The popup closes and full-reloads after any action** — that is what re-resolves the write target and re-applies sort/EOL. Do not try to mutate `m.cfg` in place in the popup instead; the full reload is the correctness guarantee.
- **`gofmt`** every file before committing (`test.sh` enforces it).

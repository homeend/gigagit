# Related-Option Prompts Implementation Plan (Stage 1)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When the user flips a Settings option, gg may ask ONE follow-up question about a related option (first entries: Show graph ↔ Commit sort), with a permanent "don't ask again" persisted in a new state-dir store.

**Architecture:** A data-driven registry in `internal/tui/related_prompts.go` is consulted at the Settings-toggle chokepoint; a matching, non-suppressed entry pushes a `relatedPromptPopup` (layer stack). Suppression lives in a new tiny package `internal/promptstate` (atomic-rewrite TOML at `<state>/gg/prompts.toml`, beside `operations.log`), TUI-owned like `opLog` — no domain/git involvement. The store carries TWO record kinds (globally suppressed prompt ids now; per-repo dismissed notice ids for stage 2's notification center).

**Tech Stack:** Go 1.26, Bubble Tea (Elm-style value-receiver `Model`), `github.com/pelletier/go-toml/v2`, real-git `t.TempDir()` tests + direct `Model.Update` driving.

**Spec:** `docs/superpowers/specs/2026-07-02-related-prompts-notifications-git-config-design.md` (Stage 1 section).

## Global Constraints

- Work on branch `feat/related-prompts` in the worktree `.claude/worktrees/related-prompts` — never the shared checkout. Use worktree-relative paths.
- TDD: write the failing test first, watch it fail, implement, watch it pass, commit.
- `internal/tui` must NOT import `internal/git` (archtest-guarded). `internal/promptstate` is TUI-owned (pure UX, no git semantics) — the TUI MAY import it directly.
- "Yes" on a prompt runs EXACTLY the code path of the corresponding Settings row (here `cycleCommitSort()`), never a parallel implementation.
- Never trap the user: esc on the prompt popup = "Not now" (close, no write).
- The popup footer must name the state file so the choice is discoverable and resettable.
- Prompt suppression is GLOBAL (all repos) — a prompt you never want is never wanted anywhere.
- `Model` is a value receiver; popups live on the layer stack (`pushLayer`/`popLayer`), NOT as new Model fields.
- Every commit message ends with:
  ```
  Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01TpkcdEnsSZtSEDC7GyHf7n
  ```
- Run `gofmt -w` on every file you create/edit before committing (test.sh fails on unformatted files).
- Before the final task: `./test.sh race` must be green.

---

### Task 1: `internal/promptstate` — the suppression/dismissal store

**Files:**
- Create: `internal/promptstate/store.go`
- Create: `internal/promptstate/file_store.go`
- Test: `internal/promptstate/file_store_test.go`

**Interfaces:**
- Consumes: nothing (leaf package; only stdlib + `github.com/pelletier/go-toml/v2`).
- Produces: `promptstate.Store` interface and `promptstate.NewFileStore(path string) *FileStore`, used by Task 2. Methods (exact):
  - `SuppressedPrompts() map[string]bool`
  - `SuppressPrompt(id string) error`
  - `DismissedNotices(repoKey string) map[string]bool`
  - `DismissNotice(repoKey, id string) error`

**Background for the implementer:** gigagit keeps machine-local UX memory in small TOML files under the XDG state dir, written via temp-file + `os.Rename` (atomic rewrite) with a read-merge before each write so a sibling process's records are not lost. The exemplar is `internal/searchhist/file_store.go` — read it before starting. `promptstate` differs in two ways: it is a single machine-global file (not per-repo rooted), so the constructor takes the full file path; and it stores two record kinds — globally suppressed prompt ids (used by this stage) and per-repo dismissed notice ids keyed by an opaque repo key (consumed by stage 2's notification center; ships now so the file format is settled once).

- [ ] **Step 1: Write the failing test**

Create `internal/promptstate/file_store_test.go`:

```go
package promptstate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tempStore(t *testing.T) *FileStore {
	t.Helper()
	return NewFileStore(filepath.Join(t.TempDir(), "prompts.toml"))
}

func TestEmptyStoreHasNoRecords(t *testing.T) {
	st := tempStore(t)
	if got := st.SuppressedPrompts(); len(got) != 0 {
		t.Fatalf("fresh store: SuppressedPrompts = %v, want empty", got)
	}
	if got := st.DismissedNotices("repo-a"); len(got) != 0 {
		t.Fatalf("fresh store: DismissedNotices = %v, want empty", got)
	}
}

func TestSuppressPromptRoundTrip(t *testing.T) {
	st := tempStore(t)
	if err := st.SuppressPrompt("show_graph_off.commit_sort_plain"); err != nil {
		t.Fatalf("SuppressPrompt: %v", err)
	}
	if !st.SuppressedPrompts()["show_graph_off.commit_sort_plain"] {
		t.Fatal("suppressed id must be reported by the same store")
	}
	// A fresh store over the same file sees the persisted record.
	st2 := NewFileStore(st.path)
	if !st2.SuppressedPrompts()["show_graph_off.commit_sort_plain"] {
		t.Fatal("suppressed id must survive a reload from disk")
	}
	if st2.SuppressedPrompts()["other.prompt"] {
		t.Fatal("an id never suppressed must not be reported")
	}
}

func TestSuppressPromptIsIdempotent(t *testing.T) {
	st := tempStore(t)
	for i := 0; i < 3; i++ {
		if err := st.SuppressPrompt("p1"); err != nil {
			t.Fatalf("SuppressPrompt #%d: %v", i, err)
		}
	}
	raw, err := os.ReadFile(st.path)
	if err != nil {
		t.Fatalf("reading store file: %v", err)
	}
	if strings.Count(string(raw), "p1") != 1 {
		t.Fatalf("id must be stored once, file:\n%s", raw)
	}
}

func TestDismissNoticePerRepo(t *testing.T) {
	st := tempStore(t)
	if err := st.DismissNotice("repo-a", "commit_graph"); err != nil {
		t.Fatalf("DismissNotice: %v", err)
	}
	if !st.DismissedNotices("repo-a")["commit_graph"] {
		t.Fatal("dismissed notice must be reported for its repo")
	}
	if st.DismissedNotices("repo-b")["commit_graph"] {
		t.Fatal("a dismissal is per-repo: repo-b must not see repo-a's record")
	}
	st2 := NewFileStore(st.path)
	if !st2.DismissedNotices("repo-a")["commit_graph"] {
		t.Fatal("dismissal must survive a reload from disk")
	}
}

func TestRecordKindsCoexistInOneFile(t *testing.T) {
	st := tempStore(t)
	if err := st.SuppressPrompt("p1"); err != nil {
		t.Fatal(err)
	}
	if err := st.DismissNotice("repo-a", "n1"); err != nil {
		t.Fatal(err)
	}
	// Writing one kind must not clobber the other (read-merge before write).
	if !st.SuppressedPrompts()["p1"] || !st.DismissedNotices("repo-a")["n1"] {
		t.Fatal("both record kinds must coexist after interleaved writes")
	}
}

func TestCorruptFileTreatedAsEmpty(t *testing.T) {
	st := tempStore(t)
	if err := os.WriteFile(st.path, []byte("not [ toml"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := st.SuppressedPrompts(); len(got) != 0 {
		t.Fatalf("corrupt file must read as empty, got %v", got)
	}
	// And a write from that state still succeeds (rewrites clean).
	if err := st.SuppressPrompt("p1"); err != nil {
		t.Fatalf("SuppressPrompt over corrupt file: %v", err)
	}
	if !NewFileStore(st.path).SuppressedPrompts()["p1"] {
		t.Fatal("write after corruption must persist")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/promptstate/`
Expected: FAIL to build — `NewFileStore` / `FileStore` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/promptstate/store.go`:

```go
// Package promptstate is gigagit's machine-local memory of dismissed UX
// prompts: which related-option follow-up prompts the user never wants to see
// again (global — a prompt you never want is never wanted in any repo), and
// which health notices they dismissed per repo (consumed by the notification
// center). It is pure UX state with no git semantics: the TUI owns it directly
// (like the operation log), it is NOT config (no .gg.toml / settingDocs
// plumbing), and it lives in one TOML file under the gg state dir.
package promptstate

// Store persists prompt suppressions and notice dismissals. Safe for
// sequential use by one process; writes read-merge then atomically rewrite,
// so the common interleaved case does not lose a sibling's records.
type Store interface {
	// SuppressedPrompts returns the globally suppressed prompt ids.
	SuppressedPrompts() map[string]bool
	// SuppressPrompt records id as never-ask-again (idempotent) and persists.
	SuppressPrompt(id string) error
	// DismissedNotices returns the notice ids dismissed for repoKey.
	DismissedNotices(repoKey string) map[string]bool
	// DismissNotice records a per-repo notice dismissal (idempotent) and persists.
	DismissNotice(repoKey, id string) error
}
```

Create `internal/promptstate/file_store.go`:

```go
package promptstate

import (
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// FileStore keeps an atomic-rewrite TOML file at a caller-supplied path
// (one machine-global file, unlike searchhist's per-repo root).
type FileStore struct{ path string }

// NewFileStore points a store at the full file path (e.g. <state>/gg/prompts.toml).
func NewFileStore(path string) *FileStore { return &FileStore{path: path} }

// records is the on-disk shape.
type records struct {
	SuppressedPrompts []string            `toml:"suppressed_prompts"`
	DismissedNotices  map[string][]string `toml:"dismissed_notices"`
}

// read loads the file; a missing or malformed file reads as empty (UX memory
// is best-effort — never block the TUI on it).
func (fs *FileStore) read() records {
	empty := records{DismissedNotices: map[string][]string{}}
	data, err := os.ReadFile(fs.path)
	if err != nil {
		return empty
	}
	var r records
	if err := toml.Unmarshal(data, &r); err != nil {
		return empty
	}
	if r.DismissedNotices == nil {
		r.DismissedNotices = map[string][]string{}
	}
	return r
}

// SuppressedPrompts returns the globally suppressed prompt ids.
func (fs *FileStore) SuppressedPrompts() map[string]bool {
	return toSet(fs.read().SuppressedPrompts)
}

// SuppressPrompt records id as never-ask-again (idempotent) and persists.
func (fs *FileStore) SuppressPrompt(id string) error {
	r := fs.read() // read-merge: pick up any sibling writes first
	if toSet(r.SuppressedPrompts)[id] {
		return nil
	}
	r.SuppressedPrompts = append(r.SuppressedPrompts, id)
	return fs.write(r)
}

// DismissedNotices returns the notice ids dismissed for repoKey.
func (fs *FileStore) DismissedNotices(repoKey string) map[string]bool {
	return toSet(fs.read().DismissedNotices[repoKey])
}

// DismissNotice records a per-repo notice dismissal (idempotent) and persists.
func (fs *FileStore) DismissNotice(repoKey, id string) error {
	r := fs.read()
	if toSet(r.DismissedNotices[repoKey])[id] {
		return nil
	}
	r.DismissedNotices[repoKey] = append(r.DismissedNotices[repoKey], id)
	return fs.write(r)
}

func toSet(ids []string) map[string]bool {
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}

// write persists r via temp-file + rename (the searchhist / seq-state pattern).
func (fs *FileStore) write(r records) error {
	dir := filepath.Dir(fs.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := toml.Marshal(r)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "prompts-*.toml")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, fs.path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/promptstate/`
Expected: PASS (all 6 tests).

- [ ] **Step 5: Register the new package in the layering DAG**

`internal/archtest/import_guard_test.go` has a `TestLayeringDAG` map guarding that low-layer packages never import upward. Add `promptstate` as a leaf (it may import nothing above it). In the `cases` map, after the `"commitgraph"` line, add:

```go
		"promptstate": {"git", "engine", "domain", "tui", "cli", "app"},
```

Do NOT add `promptstate` to `TestFrontendsDoNotImportGit`'s forbidden map — unlike shelf/bookmark/searchhist it is TUI-owned by design (pure UX state, no git semantics), so `internal/tui` importing it is the intended edge.

Run: `go test ./internal/archtest/`
Expected: PASS.

- [ ] **Step 6: gofmt and commit**

```bash
gofmt -w internal/promptstate/ internal/archtest/
git add internal/promptstate/ internal/archtest/import_guard_test.go
git commit -m "feat(promptstate): machine-local store for prompt suppressions + notice dismissals

One TOML file (<state>/gg/prompts.toml, atomic rewrite, read-merge) holding
two record kinds: globally suppressed related-option prompt ids (stage 1)
and per-repo dismissed notice ids (consumed by the stage-2 notification
center). TUI-owned UX memory like the operation log — not config, no git
semantics. Registered as a leaf in the archtest layering DAG.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01TpkcdEnsSZtSEDC7GyHf7n"
```

---

### Task 2: registry + trigger evaluation (`related_prompts.go`)

**Files:**
- Create: `internal/tui/related_prompts.go`
- Modify: `internal/tui/model.go` (Model struct ~line 58 area; `New()` at line 207)
- Test: `internal/tui/related_prompts_test.go`

**Interfaces:**
- Consumes: `promptstate.Store` / `promptstate.NewFileStore(path)` (Task 1); `repos.DefaultStatePath() string` (existing, `internal/repos/repos.go:33`); `Model.commitSort() string` and `Model.cycleCommitSort() (Model, tea.Cmd)` (existing, `internal/tui/settings_popup.go:140,150`).
- Produces (used by Tasks 3–4):
  - `type relatedPrompt struct { id, setting, question, yesLabel string; when func(Model, string) bool; apply func(Model) (Model, tea.Cmd) }`
  - `const settingShowGraph = "show_graph"`
  - `var relatedPrompts []relatedPrompt` (the two show_graph entries)
  - `func (m Model) relatedPromptFor(setting, newValue string) *relatedPrompt` (nil when nothing should fire)
  - `func defaultPromptStatePath() string` and Model field `promptStore promptstate.Store`
  - Test helper `promptTestModel(t *testing.T) (Model, promptstate.Store)` in the test file — Task 3's tests reuse it (same package).

**Background:** `[ui] commit_sort` has exactly two modes (`commitSortModes = []string{"date-order", "plain"}` in `settings_popup.go:55`), so when a prompt's `when` precondition pins the CURRENT mode, calling the existing `cycleCommitSort()` lands exactly on the other mode — that is how "Yes" reuses the Settings row's code path verbatim instead of a parallel setter.

- [ ] **Step 1: Write the failing test**

Create `internal/tui/related_prompts_test.go`:

```go
package tui

import (
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/promptstate"
)

// The related-prompt registry: flipping a Settings option may ask ONE
// follow-up about a related option. Trigger = setting id + a precondition
// against the live config; a globally suppressed id never fires.

// promptTestModel returns a loaded model with a temp-file prompt store, so
// tests never read or write the developer's real <state>/gg/prompts.toml.
func promptTestModel(t *testing.T) (Model, promptstate.Store) {
	t.Helper()
	m, dir := settingsModel(t)
	m.repoConfigPath = filepath.Join(dir, ".gg.toml")
	st := promptstate.NewFileStore(filepath.Join(t.TempDir(), "prompts.toml"))
	m.promptStore = st
	return m, st
}

func TestPromptFiresOnGraphOffWhenSortIsDateOrder(t *testing.T) {
	m, _ := promptTestModel(t)
	m.cfg.UI.CommitSort = "date-order"
	rp := m.relatedPromptFor(settingShowGraph, "off")
	if rp == nil {
		t.Fatal("show_graph→off with commit_sort=date-order must offer the plain prompt")
	}
	if rp.id != "show_graph_off.commit_sort_plain" {
		t.Fatalf("prompt id = %q, want show_graph_off.commit_sort_plain", rp.id)
	}
}

func TestPromptSilentWhenSortAlreadyPlain(t *testing.T) {
	m, _ := promptTestModel(t)
	m.cfg.UI.CommitSort = "plain"
	if rp := m.relatedPromptFor(settingShowGraph, "off"); rp != nil {
		t.Fatalf("commit_sort already plain: nothing to offer, got %q", rp.id)
	}
}

func TestPromptFiresOnGraphOnWhenSortIsPlain(t *testing.T) {
	m, _ := promptTestModel(t)
	m.cfg.UI.CommitSort = "plain"
	rp := m.relatedPromptFor(settingShowGraph, "on")
	if rp == nil {
		t.Fatal("show_graph→on with commit_sort=plain must offer the date-order prompt")
	}
	if rp.id != "show_graph_on.commit_sort_dateorder" {
		t.Fatalf("prompt id = %q, want show_graph_on.commit_sort_dateorder", rp.id)
	}
}

func TestPromptSilentWhenSortUnset(t *testing.T) {
	// Unset commit_sort resolves to date-order (commitSort()), so switching the
	// graph ON has nothing to offer — the effective mode is already date-order.
	m, _ := promptTestModel(t)
	m.cfg.UI.CommitSort = ""
	if rp := m.relatedPromptFor(settingShowGraph, "on"); rp != nil {
		t.Fatalf("effective date-order: nothing to offer on graph-on, got %q", rp.id)
	}
}

func TestSuppressedPromptNeverFires(t *testing.T) {
	m, st := promptTestModel(t)
	m.cfg.UI.CommitSort = "date-order"
	if err := st.SuppressPrompt("show_graph_off.commit_sort_plain"); err != nil {
		t.Fatal(err)
	}
	if rp := m.relatedPromptFor(settingShowGraph, "off"); rp != nil {
		t.Fatalf("suppressed prompt must never fire, got %q", rp.id)
	}
}

func TestNilStoreStillPrompts(t *testing.T) {
	// No state dir → nil store → prompts still work (suppression just can't
	// persist). The registry must not panic on a nil store.
	m, _ := promptTestModel(t)
	m.promptStore = nil
	m.cfg.UI.CommitSort = "date-order"
	if m.relatedPromptFor(settingShowGraph, "off") == nil {
		t.Fatal("nil store must not disable prompts")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/tui/ -run 'TestPrompt|TestSuppressedPrompt|TestNilStore'`
Expected: FAIL to build — `promptStore`, `relatedPromptFor`, `settingShowGraph` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/tui/related_prompts.go`:

```go
package tui

import (
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/promptstate"
	"github.com/homeend/gigagit/internal/repos"
)

// relatedPrompt is one entry of the related-option registry: when a Settings
// toggle lands on a value that makes a RELATED option worth reconsidering, gg
// asks one follow-up question. Options are always Yes / Not now / No — don't
// ask again; "Yes" must reuse the related Settings row's exact code path.
type relatedPrompt struct {
	id       string // stable suppression key (persisted in prompts.toml)
	setting  string // which Settings toggle triggers evaluation
	question string
	yesLabel string
	// when gates the prompt on the LIVE config after the toggle applied:
	// newValue is the setting's fresh value; check the related option's
	// current state so an already-aligned config asks nothing.
	when func(m Model, newValue string) bool
	// apply runs the "Yes" action. It must be the same code path as the
	// related option's own Settings row — never a parallel setter.
	apply func(m Model) (Model, tea.Cmd)
}

// setting ids consulted by relatedPromptFor (one per registered trigger).
const settingShowGraph = "show_graph"

// relatedPrompts is the registry. Both show_graph entries reuse
// cycleCommitSort() for "Yes": commit_sort has exactly two modes and each
// entry's `when` pins the current one, so one cycle lands on the target mode
// via the Settings row's own code path (persist + feed re-walk included).
var relatedPrompts = []relatedPrompt{
	{
		id:       "show_graph_off.commit_sort_plain",
		setting:  settingShowGraph,
		question: "Ordering only matters for graph lanes — also switch Commit sort to plain (much faster on big repos)?",
		yesLabel: "Yes, set plain",
		when: func(m Model, newValue string) bool {
			return newValue == "off" && m.commitSort() == "date-order"
		},
		apply: func(m Model) (Model, tea.Cmd) { return m.cycleCommitSort() },
	},
	{
		id:       "show_graph_on.commit_sort_dateorder",
		setting:  settingShowGraph,
		question: "The graph draws correct lanes only with date-order — switch Commit sort back to date-order?",
		yesLabel: "Yes, set date-order",
		when: func(m Model, newValue string) bool {
			return newValue == "on" && m.commitSort() == "plain"
		},
		apply: func(m Model) (Model, tea.Cmd) { return m.cycleCommitSort() },
	},
}

// relatedPromptFor returns the first registered, non-suppressed prompt whose
// trigger matches this setting change, or nil when nothing should fire. A nil
// prompt store only disables suppression persistence, never the prompts.
func (m Model) relatedPromptFor(setting, newValue string) *relatedPrompt {
	var suppressed map[string]bool
	if m.promptStore != nil {
		suppressed = m.promptStore.SuppressedPrompts()
	}
	for i := range relatedPrompts {
		rp := &relatedPrompts[i]
		if rp.setting != setting || suppressed[rp.id] || !rp.when(m, newValue) {
			continue
		}
		return rp
	}
	return nil
}

// defaultPromptStatePath puts prompts.toml beside operations.log in the gg
// state dir, reusing the repo registry's platform-appropriate resolution.
// "" when no home/state dir exists.
func defaultPromptStatePath() string {
	sp := repos.DefaultStatePath()
	if sp == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(sp), "prompts.toml")
}

// defaultPromptStore opens the machine-global prompt store, or nil when there
// is no state dir (prompts still show; "don't ask again" just can't persist).
func defaultPromptStore() promptstate.Store {
	path := defaultPromptStatePath()
	if path == "" {
		return nil
	}
	return promptstate.NewFileStore(path)
}
```

In `internal/tui/model.go`, add the field to the `Model` struct (near `opLog`; search for `opLog` in the struct definition):

```go
	promptStore promptstate.Store // related-prompt suppressions; nil = no state dir
```

and add the import `"github.com/homeend/gigagit/internal/promptstate"` to model.go's import block. In `New()` (line 207), add to the literal after `opLog: newOpLog(),`:

```go
		promptStore:    defaultPromptStore(),
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestPrompt|TestSuppressedPrompt|TestNilStore'`
Expected: PASS (6 tests). Then run `go test ./internal/tui/` to confirm no existing test broke.

- [ ] **Step 5: gofmt and commit**

```bash
gofmt -w internal/tui/related_prompts.go internal/tui/related_prompts_test.go internal/tui/model.go
git add internal/tui/related_prompts.go internal/tui/related_prompts_test.go internal/tui/model.go
git commit -m "feat(tui): related-option prompt registry + trigger evaluation

Data-driven registry: a Settings toggle may offer ONE follow-up about a
related option, gated on the live config (an already-aligned config asks
nothing) and on the promptstate suppression store (global don't-ask-again).
First entries: show_graph off→offer commit_sort=plain, on→offer date-order
back; both reuse cycleCommitSort() so Yes is the Settings row's own path.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01TpkcdEnsSZtSEDC7GyHf7n"
```

---

### Task 3: `relatedPromptPopup` + Settings hook

**Files:**
- Create: `internal/tui/related_prompt_popup.go`
- Modify: `internal/tui/settings_popup.go` (the `settingsMenuShowGraph` case in `(*settingsPopup).update`, line ~334)
- Test: `internal/tui/related_prompt_popup_test.go`

**Interfaces:**
- Consumes: `relatedPrompt` / `Model.relatedPromptFor` / `Model.promptStore` / `defaultPromptStatePath()` and the `promptTestModel(t)` test helper (all Task 2); layer stack (`pushLayer`/`popLayer`/`layerOf`, `internal/tui/layer_stack.go`); `overlayCenter`, `clipToHeight`, `modalStyle`, `popupInnerWidth`, `popupTextWidth`, `wrapWidth(s string, w, maxLines int) []string`, `selectedRow`, `m.overlayDims()` (existing popup plumbing — exemplar: `internal/tui/commit_name_popup.go` and `settings_popup.go`).
- Produces: `type relatedPromptPopup struct { prompt *relatedPrompt; sel int }` implementing `layer`; `func (m Model) maybeRelatedPrompt(setting, newValue string) (Model, tea.Cmd)` — called from the Settings enter handler (and by future toggles that register prompts).

**Popup contract (from the spec + house rules):** three options — `<yesLabel>` / `Not now` / `No — don't ask again`; ↑/↓ moves (wrapping, like the Settings menu); enter chooses; **esc = Not now** (close, no write — never trap); ctrl+c quits; every other key is swallowed. The footer names the state file so the choice is discoverable/resettable. It pushes ON TOP of the Settings popup, so closing it returns to Settings with the flipped toggle visible.

- [ ] **Step 1: Write the failing test**

Create `internal/tui/related_prompt_popup_test.go`:

```go
package tui

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// openSettingsAndToggleShowGraph drives the real key path: open Settings with
// ',', move the menu selection to "Show graph", press enter. Returns the model
// after the toggle (and possibly with a related prompt pushed).
func openSettingsAndToggleShowGraph(t *testing.T, m Model) Model {
	t.Helper()
	u, _ := m.Update(keyMsg(","))
	m = u.(Model)
	p := layerOf[*settingsPopup](m)
	if p == nil {
		t.Fatal("settings popup did not open")
	}
	for i, entry := range settingsMenu {
		if entry == settingsMenuShowGraph {
			p.menuSel = i
		}
	}
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return u.(Model)
}

func TestToggleShowGraphOffPushesPrompt(t *testing.T) {
	m, _ := promptTestModel(t)
	m.cfg.UI.CommitSort = "date-order"
	m = openSettingsAndToggleShowGraph(t, m) // on → off
	pp := layerOf[*relatedPromptPopup](m)
	if pp == nil {
		t.Fatal("toggling show_graph off with date-order must push the related prompt")
	}
	if pp.prompt.id != "show_graph_off.commit_sort_plain" {
		t.Fatalf("wrong prompt: %q", pp.prompt.id)
	}
	out := m.View()
	if !strings.Contains(out, "Commit sort") {
		t.Fatalf("prompt question must be visible, view:\n%s", out)
	}
	if !strings.Contains(out, "Not now") || !strings.Contains(out, "don't ask again") {
		t.Fatalf("all three options must be visible, view:\n%s", out)
	}
}

func TestToggleShowGraphOffNoPromptWhenAlreadyPlain(t *testing.T) {
	m, _ := promptTestModel(t)
	m.cfg.UI.CommitSort = "plain"
	m = openSettingsAndToggleShowGraph(t, m)
	if layerOf[*relatedPromptPopup](m) != nil {
		t.Fatal("commit_sort already plain: no prompt")
	}
	if layerOf[*settingsPopup](m) == nil {
		t.Fatal("settings must stay open after a promptless toggle")
	}
}

func TestPromptEscMeansNotNow(t *testing.T) {
	m, _ := promptTestModel(t)
	m.cfg.UI.CommitSort = "date-order"
	m = openSettingsAndToggleShowGraph(t, m)
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = u.(Model)
	if layerOf[*relatedPromptPopup](m) != nil {
		t.Fatal("esc must close the prompt")
	}
	if layerOf[*settingsPopup](m) == nil {
		t.Fatal("esc must return to the Settings popup beneath")
	}
	if m.cfg.UI.CommitSort != "date-order" {
		t.Fatal("Not now must not touch commit_sort")
	}
	// Not now is session-only: the next toggle round-trip asks again.
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // Settings enter: off → on (no prompt: sort is date-order)
	m = u.(Model)
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // on → off again
	m = u.(Model)
	if layerOf[*relatedPromptPopup](m) == nil {
		t.Fatal("Not now must not suppress the prompt permanently")
	}
}

func TestPromptYesAppliesCommitSortPlain(t *testing.T) {
	m, _ := promptTestModel(t)
	m.cfg.UI.CommitSort = "date-order"
	m = openSettingsAndToggleShowGraph(t, m)
	// sel starts on Yes; enter applies.
	u, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = u.(Model)
	if layerOf[*relatedPromptPopup](m) != nil {
		t.Fatal("Yes must close the prompt")
	}
	if m.cfg.UI.CommitSort != "plain" {
		t.Fatalf("Yes must set commit_sort=plain via cycleCommitSort, got %q", m.cfg.UI.CommitSort)
	}
	if cmd == nil {
		t.Fatal("cycleCommitSort re-walks the feed — a command must be returned")
	}
	raw, err := os.ReadFile(m.repoConfigPath)
	if err != nil {
		t.Fatalf("Yes must persist to the repo .gg.toml: %v", err)
	}
	if !strings.Contains(string(raw), `commit_sort = "plain"`) {
		t.Fatalf(".gg.toml missing commit_sort = \"plain\":\n%s", raw)
	}
}

func TestPromptDontAskAgainSuppressesForever(t *testing.T) {
	m, st := promptTestModel(t)
	m.cfg.UI.CommitSort = "date-order"
	m = openSettingsAndToggleShowGraph(t, m)
	// Move to the third option (Yes → Not now → don't ask again) and choose it.
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	u, _ = u.(Model).Update(tea.KeyMsg{Type: tea.KeyDown})
	u, _ = u.(Model).Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = u.(Model)
	if layerOf[*relatedPromptPopup](m) != nil {
		t.Fatal("don't-ask-again must close the prompt")
	}
	if m.cfg.UI.CommitSort != "date-order" {
		t.Fatal("don't-ask-again must not touch commit_sort")
	}
	if !st.SuppressedPrompts()["show_graph_off.commit_sort_plain"] {
		t.Fatal("don't-ask-again must persist the suppression")
	}
	// Toggle on then off again: the suppressed prompt never comes back.
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // off → on
	m = u.(Model)
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // on → off
	m = u.(Model)
	if layerOf[*relatedPromptPopup](m) != nil {
		t.Fatal("suppressed prompt must never fire again")
	}
}

func TestPromptSwallowsGlobalKeys(t *testing.T) {
	m, _ := promptTestModel(t)
	m.cfg.UI.CommitSort = "date-order"
	m = openSettingsAndToggleShowGraph(t, m)
	before := len(m.layers.entries)
	for _, k := range []string{"p", "g", "G", ",", "r", "?"} {
		u, _ := m.Update(keyMsg(k))
		m = u.(Model)
	}
	if len(m.layers.entries) != before {
		t.Fatal("global keys must be swallowed while the prompt is open")
	}
	if layerOf[*relatedPromptPopup](m) == nil {
		t.Fatal("prompt must still be open")
	}
}

func TestPromptFooterNamesStateFile(t *testing.T) {
	m, _ := promptTestModel(t)
	m.cfg.UI.CommitSort = "date-order"
	m = openSettingsAndToggleShowGraph(t, m)
	pp := layerOf[*relatedPromptPopup](m)
	if pp == nil {
		t.Fatal("prompt must be open")
	}
	if p := defaultPromptStatePath(); p != "" && !strings.Contains(m.View(), "prompts.toml") {
		t.Fatal("the popup must name the prompts.toml state file so the choice is resettable")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/tui/ -run 'TestToggleShowGraph(Off|On)|TestPrompt(Esc|Yes|Dont|Swallows|Footer)'`
Expected: FAIL to build — `relatedPromptPopup` undefined.

- [ ] **Step 3: Write the popup + hook**

Create `internal/tui/related_prompt_popup.go`:

```go
package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// relatedPromptPopup asks the ONE follow-up question a Settings toggle can
// trigger (see related_prompts.go). It pushes on top of the Settings popup;
// closing it (any choice, or esc = Not now) returns to Settings with the
// flipped toggle still visible.
type relatedPromptPopup struct {
	prompt *relatedPrompt
	sel    int // 0 = yes, 1 = not now, 2 = don't ask again
}

// maybeRelatedPrompt consults the registry after a Settings toggle applied and
// pushes the follow-up popup when a trigger matches. Call it with the
// setting's FRESH value (after the toggle mutated cfg).
func (m Model) maybeRelatedPrompt(setting, newValue string) (Model, tea.Cmd) {
	rp := m.relatedPromptFor(setting, newValue)
	if rp == nil {
		return m, nil
	}
	return m.pushLayer(&relatedPromptPopup{prompt: rp}), nil
}

// options returns the fixed three-choice list, yes-label first.
func (p *relatedPromptPopup) options() []string {
	return []string{p.prompt.yesLabel, "Not now", "No — don't ask again"}
}

func (p *relatedPromptPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	n := len(p.options())
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc: // esc = Not now — never trap, never write
		return m.popLayer(), nil
	case tea.KeyUp:
		p.sel = (p.sel - 1 + n) % n
		return m, nil
	case tea.KeyDown:
		p.sel = (p.sel + 1) % n
		return m, nil
	case tea.KeyEnter:
		sel, rp := p.sel, p.prompt
		m = m.popLayer()
		switch sel {
		case 0:
			return rp.apply(m)
		case 2:
			if m.promptStore == nil {
				m.statusMsg = "won't ask again this session (not saved: no state dir)"
			} else if err := m.promptStore.SuppressPrompt(rp.id); err != nil {
				m.statusMsg = "won't ask again this session (not saved: " + err.Error() + ")"
			} else {
				m.statusMsg = "won't ask again — saved to " + defaultPromptStatePath()
			}
		}
		return m, nil
	}
	return m, nil // swallow everything else — no fallthrough to global keys
}

func (p *relatedPromptPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	textW := popupTextWidth(popupInnerWidth(w))
	var b strings.Builder
	b.WriteString("Related option\n\n")
	for _, line := range wrapWidth(p.prompt.question, textW, 1<<20) {
		b.WriteString(line + "\n")
	}
	b.WriteString("\n")
	for i, opt := range p.options() {
		prefix := "  "
		if i == p.sel {
			prefix = "> "
		}
		row := prefix + opt
		if i == p.sel {
			row = selectedRow.Render(row)
		}
		b.WriteString(row + "\n")
	}
	b.WriteString("\n[↑/↓] select  [enter] choose  [esc] not now")
	// Name the state file so a persisted "don't ask again" is discoverable
	// and resettable (delete or edit prompts.toml to bring prompts back).
	if path := defaultPromptStatePath(); path != "" {
		b.WriteString("\n")
		for _, seg := range wrapWidth("don't-ask-again choices: "+path, textW, 1<<20) {
			b.WriteString(seg + "\n")
		}
	}
	box := modalStyle.Width(popupInnerWidth(w)).Render(strings.TrimRight(b.String(), "\n")) + "\n"
	return overlayCenter(clipToHeight(below, h), box, w, h)
}
```

Note for the implementer: `wrapWidth`, `popupTextWidth`, `popupInnerWidth`, `selectedRow`, `modalStyle`, `overlayCenter`, `clipToHeight` all already exist in `internal/tui` (see `settings_popup.go` for usage of each). If `wrapWidth`'s signature differs from `(s string, w, max int) []string`, match the existing call sites (e.g. `settings_popup.go:531`).

In `internal/tui/settings_popup.go`, change the `settingsMenuShowGraph` case (line ~334) from:

```go
			case settingsMenuShowGraph:
				return m.toggleShowGraph(), nil // stays open so the state flip is visible
```

to:

```go
			case settingsMenuShowGraph:
				m = m.toggleShowGraph() // stays open so the state flip is visible
				// A related option may be worth reconsidering now (e.g. commit
				// sort buys nothing with the graph hidden) — one follow-up, max.
				return m.maybeRelatedPrompt(settingShowGraph, m.cfg.UI.ShowGraph)
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestToggleShowGraph|TestPrompt'`
Expected: PASS (all new tests plus the existing `TestToggleShowGraphPersistsAndFlips` — that one drives `toggleShowGraph()` directly, not the menu, so it is unaffected).

Then run the full package: `go test ./internal/tui/`
Expected: PASS. If `TestToggleShowGraphPersistsAndFlips` or any settings test fails, the hook changed behavior it shouldn't have — the prompt must only be pushed from the MENU path, never inside `toggleShowGraph()` itself.

- [ ] **Step 5: gofmt and commit**

```bash
gofmt -w internal/tui/related_prompt_popup.go internal/tui/related_prompt_popup_test.go internal/tui/settings_popup.go
git add internal/tui/related_prompt_popup.go internal/tui/related_prompt_popup_test.go internal/tui/settings_popup.go
git commit -m "feat(tui): related-option follow-up popup on Settings toggles

Toggling Show graph now asks the one relevant follow-up (switch Commit sort
to plain / back to date-order) when the live config makes it worthwhile.
Options: Yes (runs the Settings row's own code path) / Not now (esc; session
only) / No — don't ask again (persisted globally in prompts.toml, which the
popup footer names so the choice is resettable).

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01TpkcdEnsSZtSEDC7GyHf7n"
```

---

### Task 4: the `adding-related-option-prompts` project skill

**Files:**
- Create: `.claude/skills/adding-related-option-prompts/SKILL.md`

**Interfaces:**
- Consumes: the shipped shapes from Tasks 1–3 (verify names against the actual code before writing — the skill documents reality, not the plan).
- Produces: a project skill that future sessions load when wiring a new related-option prompt.

- [ ] **Step 1: Write the skill**

Create `.claude/skills/adding-related-option-prompts/SKILL.md`:

```markdown
---
name: adding-related-option-prompts
description: Use when a gigagit Settings toggle should offer a one-question follow-up about a RELATED option (e.g. Show graph off → offer Commit sort plain), or when changing the related-prompt registry, popup, or promptstate suppression store.
---

# Adding a related-option prompt

## What this is

When a Settings toggle lands on a value that makes a *different* option worth
reconsidering, gg asks ONE follow-up question — options are always
`<Yes, do it>` / `Not now` / `No — don't ask again`. The machinery is a
data-driven registry (`internal/tui/related_prompts.go`), a generic popup
(`internal/tui/related_prompt_popup.go`), and a machine-global suppression
store (`internal/promptstate`, one TOML file at `<state>/gg/prompts.toml`).
Adding a prompt = adding ONE registry entry + one hook line; the popup and
store are already generic.

## Checklist for a new prompt

1. **Registry entry** in `relatedPrompts` (`internal/tui/related_prompts.go`):

   ```go
   {
       id:       "<setting>_<newvalue>.<related>_<target>", // e.g. show_graph_off.commit_sort_plain
       setting:  setting<X>,                                 // const naming the triggering toggle
       question: "…one sentence, states the WHY…?",
       yesLabel: "Yes, <verb> <target>",
       when: func(m Model, newValue string) bool {
           return newValue == "<trigger-value>" && /* related option's CURRENT state makes it worthwhile */
       },
       apply: func(m Model) (Model, tea.Cmd) { return m.<theRelatedSettingsRowFunc>() },
   }
   ```

   - **id is forever.** It is the persisted suppression key — never rename one
     that shipped (a rename un-suppresses the prompt for everyone).
   - **`when` must check the live config**, not just newValue: an
     already-aligned config must ask nothing. Mind unset-vs-default resolution
     (use the resolving helper, e.g. `m.commitSort()`, never the raw cfg field).
   - **`apply` must reuse the related Settings row's exact code path**
     (toggle/cycle func) — persist + side effects (feed re-walks etc.) come
     free and stay consistent. Never write a parallel setter. If the row func
     is a cycle over two modes and `when` pins the current one, one cycle IS
     the targeted set.

2. **Hook the triggering toggle** (if it isn't hooked yet). In the Settings
   enter handler (`settings_popup.go`), after the toggle applies:

   ```go
   m = m.toggle<X>()
   return m.maybeRelatedPrompt(setting<X>, m.cfg.<Section>.<Field>)
   ```

   Pass the setting's FRESH value (after the toggle mutated cfg). One
   follow-up max — `relatedPromptFor` returns the first match only.

3. **Suppression lifecycle** — nothing to do. `relatedPromptFor` filters
   suppressed ids; "No — don't ask again" persists via
   `promptstate.Store.SuppressPrompt`. Suppression is GLOBAL (all repos) by
   design. "Not now" is session-only (no record anywhere).

## Rules

- **Never trap:** esc = Not now. The popup swallows every key.
- Prompts are UX memory, NOT config: no `.gg.toml` key, no `settingDocs`
  entry, no `internal/config` writer.
- The popup footer names `prompts.toml` — keep it that way (discoverable,
  resettable by deleting the file or the id line).
- `internal/promptstate` is TUI-owned (archtest DAG leaf); do NOT move it
  behind `domain` and do NOT let it import anything above itself.

## Tests to write (see `related_prompt_popup_test.go` for exemplars)

- Trigger unit tests on `relatedPromptFor`: fires on the trigger value +
  precondition; silent when the related option is already aligned; silent
  when suppressed; nil store still prompts.
- Popup wiring through the REAL key path (open Settings, select row, enter):
  prompt pushes; Yes applies via the row's code path AND persists its config;
  esc returns to Settings and changes nothing; don't-ask-again writes the
  store and the prompt never fires again; global keys are swallowed.
- Always inject a temp-file store (`m.promptStore =
  promptstate.NewFileStore(filepath.Join(t.TempDir(), "prompts.toml"))`) —
  a test must never touch the developer's real prompts.toml.
```

- [ ] **Step 2: Verify the skill matches reality**

Re-read the skill and check every named identifier (`relatedPrompts`, `relatedPromptFor`, `maybeRelatedPrompt`, `SuppressPrompt`, file paths) against the code shipped in Tasks 1–3:

Run: `grep -n "relatedPromptFor\|maybeRelatedPrompt\|SuppressPrompt" internal/tui/related_prompts.go internal/tui/related_prompt_popup.go internal/promptstate/*.go`
Expected: every identifier the skill names appears in the output. Fix the skill (not the code) on any mismatch.

- [ ] **Step 3: Commit**

```bash
git add .claude/skills/adding-related-option-prompts/
git commit -m "docs(skills): adding-related-option-prompts project skill

How to wire a new related-option follow-up: registry entry anatomy (stable
suppression id, live-config precondition, apply = the Settings row's own
code path), the one hook line, suppression semantics, and the test matrix.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01TpkcdEnsSZtSEDC7GyHf7n"
```

---

### Task 5: docs + full race gate

**Files:**
- Modify: `CHANGELOG.md` (top of the `### Added` list under `## [Unreleased]`)
- Modify: `README.md` (the Settings/commit-sort area, ~line 201–210)
- Modify: `CLAUDE.md` (package map: new `promptstate` row; extend the `tui` row)

**Interfaces:** none — documentation of what Tasks 1–4 shipped. Verify wording against the code, not this plan.

- [ ] **Step 1: CHANGELOG entry**

Add at the TOP of the `### Added` list under `## [Unreleased]`:

```markdown
- **Related-option prompts.** Flipping a Settings option can now ask one
  follow-up about a related option: turning "Show graph" off offers to set
  Commit sort to `plain` (ordering only matters for graph lanes — plain is
  much faster on big repos); turning it back on offers `date-order` back.
  Options are Yes / Not now / **No — don't ask again**; the last is persisted
  machine-globally in `<state>/gg/prompts.toml` (named in the popup, delete a
  line to bring a prompt back). The registry is generic
  (`internal/tui/related_prompts.go`); the new `internal/promptstate` store
  also carries per-repo notice dismissals for the upcoming notification
  center.
```

- [ ] **Step 2: README**

In the Settings/commit-sort section (around lines 201–210, where "Commit sort" and "Show graph" are documented), append a short paragraph after the Show graph text:

```markdown
The two settings know about each other: toggling "Show graph" asks (once)
whether to align "Commit sort" with it — `plain` when the graph goes off
(ordering only matters for lanes; plain is much faster on big repos),
`date-order` when it comes back. Answer "No — don't ask again" to silence a
prompt permanently; those choices live in `<state>/gg/prompts.toml`, which
the prompt names — delete the line (or the file) to get prompts back.
```

- [ ] **Step 3: CLAUDE.md package map**

Add a `promptstate` row to the package map table (alphabetically it fits after `profile`):

```markdown
| `promptstate` | Machine-local UX-memory store (`<state>/gg/prompts.toml`, atomic-rewrite TOML beside `operations.log`): globally suppressed related-option prompt ids + per-repo dismissed notice ids (stage-2 notification center). TUI-owned by design — pure UX, no git semantics, NOT config (no `.gg.toml`/settingDocs); a leaf in the archtest layering DAG. |
```

And append to the `tui` row (it is one long cell; add before the closing `|`):

```markdown
 **Related-option prompts** (`related_prompts.go`, `related_prompt_popup.go`): a Settings toggle may push ONE follow-up popup about a related option — registry entries carry a stable suppression id, a live-config `when` precondition, and an `apply` that reuses the related Settings row's own code path (both show_graph↔commit_sort entries call `cycleCommitSort()`); options Yes / Not now (esc) / don't-ask-again (persisted via `promptstate`); hook = `maybeRelatedPrompt(setting, freshValue)` after the toggle in the Settings enter handler.
```

- [ ] **Step 4: Full verification**

```bash
./test.sh race
```

Expected: all stages green (vet+gofmt → unit → e2e). Fix anything that fails before committing.

- [ ] **Step 5: Commit**

```bash
git add CHANGELOG.md README.md CLAUDE.md
git commit -m "docs: record related-option prompts (stage 1)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01TpkcdEnsSZtSEDC7GyHf7n"
```

- [ ] **Step 6: Build a test binary for the user**

```bash
go build -o ./gg ./cmd/gg
```

Report the ABSOLUTE path `/mnt/t/others/gigagit/.claude/worktrees/related-prompts/gg` to the user for manual verification (toggle Show graph in a repo with commit_sort unset/date-order → prompt appears; verify Yes / Not now / don't-ask-again; check `~/.local/state/gg/prompts.toml`). The user merges — do not merge.

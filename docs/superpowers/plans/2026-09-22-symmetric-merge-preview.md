# Symmetric Merge Preview Implementation Plan

> **For agentic workers:** executed by THIS session (CLAUDE.md: NEVER USE SUB AGENTS), task by task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Pick branches A and B plus an explicit base, and gg saves ONE comparison `@base...A` vs `@base...B`, shows it as a symmetric row in Previews and opens it, in the TUI, web and CLI.

**Architecture:** There is no new storage. A symmetric preview is an ordinary two-link saved comparison (`rowCompare` / web `kind:"compare"`), recognised by its SHAPE through one pure domain function, `SymmetricOf`. Creating one is `domain.SymmetricPreviewAdd` → `SavedCompareAdd`. Opening it is the existing `CompareLinks` door.

**Tech Stack:** Go 1.26, Bubble Tea TUI, vanilla-JS web SPA, TOML e2e scenarios.

**Spec:** `docs/superpowers/specs/2026-09-22-symmetric-merge-preview-design.md` (amended by Task 0 below).

## Global Constraints

- Worktree: `/mnt/t/others/gigagit/.claude/worktrees/symmetric-merge-preview`, branch `feat/symmetric-merge-preview`. Every shell command uses this absolute path (the shell cwd resets).
- `internal/tui` / `internal/cli` never import `internal/git`. Web and MCP are domain-only.
- Every user-visible TUI string goes through `i18n.T` with a literal key present in `internal/i18n/lang/{ja,ko,ru,zh}.toml`.
- New TUI glyphs must be EAW-neutral. Use the ASCII badge `sym`. No `↔` in any NEW label.
- The base has NO default in any frontend.
- The link text is TARGET (base) first: `@<base>...<A>` on the left, `@<base>...<B>` on the right.
- There is no JS link parser: the server hands `{a, b, base}` to the page.
- New tests call `t.Parallel()` unless they touch global state.
- Commit trailers:
  ```
  Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01Vr2Uj8JZAbvVr2ip6ZEzwN
  ```

---

### Task 0: Amend the spec to match the code

These were found while planning.

- **TUI base picker:** a one-field popup that starts EMPTY, with fuzzy branch completion (the `previewAddPopup` idiom: `branchSuggestions` / `namesABranch`). It is not a cursor list.
- **Esc in the base popup:** returns to the pair menu. `pairOpPopup` pops itself before `open`, so a new `pairOp.stacked` flag keeps it underneath.
- **Enter on ANY saved comparison row** in Previews also gets the `compareLoadingPopup` (today only the marks flow shows it).
- **Web route:** `POST /api/saved-compares/symmetric {a, b, base, label}`. It returns `{entry}` on success, **409** `{id, label}` for a duplicate (the existing web convention), 400 for a refusal and 503 when the store is off. The busy state is `runOnce("symmetric-preview")` plus opLine text, and `runLinkCompare` then says "comparing…".
- **CLI:** the id goes on **stdout** (the `gg preview add` convention). A duplicate prints the existing id on stdout and a note on stderr, and exits 0. `gg compare --list` / `gg preview list` stay UNCHANGED: `--list` already prints both link texts, which spell the base.
- **`SymmetricOf`** also requires both links to name the same repo.

- [x] **Step 1:** Edit the spec's TUI, Web and CLI sections to these rulings.
- [x] **Step 2:** Commit (together with this plan).

---

### Task 1: Domain — `SymmetricPreviewAdd` + `SymmetricOf`

**Files:**
- Create: `internal/domain/symmetric.go`
- Test: `internal/domain/symmetric_test.go`

**Interfaces:**
- Produces:
  - `type Symmetric struct{ A, B, Base string }`
  - `func SymmetricOf(c SavedCompare) (Symmetric, bool)`
  - `func (s *Service) SymmetricPreviewAdd(ctx context.Context, a, b, base, label string) (SavedCompare, error)`. A duplicate returns the existing entry AND `ErrSavedCompareExists`.
  - `func (Symmetric) DefaultLabel() string` → `"A vs B (base: base)"`

- [ ] **Step 1: Write the failing tests** (`symmetric_test.go`)

```go
package domain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/gittest"
)

// symRepo is previewRepo (main, feat/x) plus feat/y off main's FIRST commit
// touching y.txt, so all three names differ and each side's three-dot set is
// distinct: base...feat/x = {a.txt, b.txt}, base...feat/y = {y.txt}.
func symRepo(t *testing.T) (string, *Service) {
	t.Helper()
	dir, svc := previewRepo(t)
	gittest.Run(t, dir, "checkout", "-q", "-b", "feat/y", "main~1")
	os.WriteFile(filepath.Join(dir, "y.txt"), []byte("y\n"), 0o644)
	gittest.Run(t, dir, "add", "y.txt")
	gittest.Run(t, dir, "commit", "-q", "-m", "add y")
	gittest.Run(t, dir, "checkout", "-q", "main")
	return dir, svc
}

func TestSymmetricPreviewAddStoresBaseFirstALeft(t *testing.T) {
	t.Parallel()
	_, svc := symRepo(t)
	ctx := context.Background()
	c, err := svc.SymmetricPreviewAdd(ctx, "feat/x", "feat/y", "main", "")
	if err != nil {
		t.Fatal(err)
	}
	// Assert the TEXT: a double swap would round-trip through SymmetricOf.
	if !strings.Contains(c.Left, "@main...feat/x") || !strings.Contains(c.Right, "@main...feat/y") {
		t.Fatalf("links = %q | %q", c.Left, c.Right)
	}
	if c.Label != "feat/x vs feat/y (base: main)" {
		t.Fatalf("label = %q", c.Label)
	}
	sym, ok := SymmetricOf(c)
	if !ok || sym != (Symmetric{A: "feat/x", B: "feat/y", Base: "main"}) {
		t.Fatalf("SymmetricOf = %+v, %v", sym, ok)
	}
	dup, err := svc.SymmetricPreviewAdd(ctx, "feat/x", "feat/y", "main", "other")
	if !errors.Is(err, ErrSavedCompareExists) || dup.ID != c.ID {
		t.Fatalf("duplicate = %+v, %v; want the existing row + ErrSavedCompareExists", dup, err)
	}
}

func TestSymmetricPreviewAddRefusals(t *testing.T) {
	t.Parallel()
	_, svc := symRepo(t)
	ctx := context.Background()
	for name, args := range map[string][3]string{
		"a == b":      {"feat/x", "feat/x", "main"},
		"base == a":   {"feat/x", "feat/y", "feat/x"},
		"base == b":   {"feat/x", "feat/y", "feat/y"},
		"empty base":  {"feat/x", "feat/y", ""},
		"empty a":     {"", "feat/y", "main"},
		"unknown":     {"feat/x", "nope", "main"},
		"link-unsafe": {"feat/x", "feat/y", "ma?in"},
	} {
		if _, err := svc.SymmetricPreviewAdd(ctx, args[0], args[1], args[2], ""); err == nil {
			t.Errorf("%s: want a refusal", name)
		}
	}
	if cs, _ := svc.SavedCompareList(ctx); len(cs) != 0 {
		t.Fatalf("a refusal stored %+v", cs)
	}
}

// Live: the entry holds NAMES, so a moved tip shows up on the next open.
func TestSymmetricPreviewIsLive(t *testing.T) {
	t.Parallel()
	dir, svc := symRepo(t)
	ctx := context.Background()
	c, err := svc.SymmetricPreviewAdd(ctx, "feat/x", "feat/y", "main", "")
	if err != nil {
		t.Fatal(err)
	}
	gittest.Run(t, dir, "checkout", "-q", "feat/y")
	os.WriteFile(filepath.Join(dir, "z.txt"), []byte("z\n"), 0o644)
	gittest.Run(t, dir, "add", "z.txt")
	gittest.Run(t, dir, "commit", "-q", "-m", "add z")
	gittest.Run(t, dir, "checkout", "-q", "main")
	cmp, err := svc.CompareLinks(ctx, c.Left, c.Right, ResolveOpts{Cwd: svc})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fmtComparison(cmp), "z.txt") {
		t.Fatalf("the reopened comparison misses feat/y's new commit: %s", fmtComparison(cmp))
	}
}

func TestSymmetricOfShapes(t *testing.T) {
	t.Parallel()
	const r = "gg:///repo"
	for name, tc := range map[string]struct {
		left, right string
		ok          bool
	}{
		"symmetric":      {r + "@main...a", r + "@main...b", true},
		"different base": {r + "@main...a", r + "@dev...b", false},
		"same source":    {r + "@main...a", r + "@main...a", false},
		"path-bearing":   {r + "/x.txt@main...a", r + "@main...b", false},
		"commit pair":    {r + "@main...a", r + "@aaaaaaa..bbbbbbb", false},
		"other repo":     {r + "@main...a", "gg:///other@main...b", false},
		"a set":          {r + "@main...a", "", false},
	} {
		_, ok := SymmetricOf(SavedCompare{Left: tc.left, Right: tc.right})
		if ok != tc.ok {
			t.Errorf("%s: ok = %v, want %v", name, ok, tc.ok)
		}
	}
}
```

`fmtComparison` renders the comparison's paths into one string. Before writing it, check `comparelinks.go` for an existing helper that lists a `LinkComparison`'s file paths; `grep -n "func.*LinkComparison" internal/domain/*.go` will find it. If there is none, add this to the test file:

```go
func fmtComparison(c LinkComparison) string { return fmt.Sprintf("%+v", c) }
```

It works because `%+v` prints every path held in the file sets; import `fmt`. Also check the local-link spelling that `SymmetricOf`'s shape test uses: run `grep -n 'gg:///' internal/model/link_test.go | head` and copy a real local form if `gg:///repo@…` does not parse.

- [ ] **Step 2: Run the tests and watch them fail**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/symmetric-merge-preview && go test ./internal/domain -run 'Symmetric' -count=1`
Expected: a build failure, because `SymmetricPreviewAdd`, `SymmetricOf` and `Symmetric` are undefined.

- [ ] **Step 3: Implement** `internal/domain/symmetric.go`

```go
package domain

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/homeend/gigagit/internal/model"
)

// A SYMMETRIC merge preview is two merge previews against one base, compared:
// "what A would bring into base" vs "what B would bring into base" — two
// branches that fix the same thing. It is not a store kind. It is an ordinary
// two-link saved comparison whose halves are both whole-tree three-dot
// preview links with the SAME target, and SymmetricOf is the one place that
// shape is recognised. A comparison saved by hand from two such previews is
// symmetric too — it is the same data.

// Symmetric names a symmetric preview's three branches.
type Symmetric struct{ A, B, Base string }

// DefaultLabel is the row name when none is given. No arrow glyph: ↔ reads as
// a left arrow in a monospace cell.
func (s Symmetric) DefaultLabel() string {
	return fmt.Sprintf("%s vs %s (base: %s)", s.A, s.B, s.Base)
}

// SymmetricOf reports whether c is a symmetric preview and names its branches.
func SymmetricOf(c SavedCompare) (Symmetric, bool) {
	if c.IsSet() {
		return Symmetric{}, false
	}
	l, errL := model.ParseLink(c.Left)
	r, errR := model.ParseLink(c.Right)
	if errL != nil || errR != nil {
		return Symmetric{}, false
	}
	lp, rp := wholePreview(l), wholePreview(r)
	if lp == nil || rp == nil || l.Repo != r.Repo || lp.Target != rp.Target || lp.Source == rp.Source {
		return Symmetric{}, false
	}
	return Symmetric{A: lp.Source, B: rp.Source, Base: lp.Target}, true
}

// wholePreview is l's preview when l addresses a whole merge preview: no
// path, line, hunk or hint.
func wholePreview(l model.Link) *model.LinkPreview {
	if l.Path != "" || l.Line != 0 || l.Hunk != 0 || l.Hint.Kind != "" {
		return nil
	}
	return l.Target.Preview
}

// SymmetricPreviewAdd saves the comparison @base...a vs @base...b. A
// duplicate hands back the stored entry with ErrSavedCompareExists.
func (s *Service) SymmetricPreviewAdd(ctx context.Context, a, b, base, label string) (SavedCompare, error) {
	a, b, base = strings.TrimSpace(a), strings.TrimSpace(b), strings.TrimSpace(base)
	switch {
	case a == "" || b == "" || base == "":
		return SavedCompare{}, errors.New("symmetric preview: needs two branches and a base")
	case a == b:
		return SavedCompare{}, errors.New("symmetric preview: the two branches are the same")
	case base == a || base == b:
		return SavedCompare{}, errors.New("symmetric preview: the base must differ from both branches")
	}
	for _, name := range []string{a, b, base} {
		if !model.LinkRefOK(name) {
			return SavedCompare{}, fmt.Errorf("symmetric preview: %q cannot be written in a gg:// link", name)
		}
		if _, ok, err := s.ResolveRev(ctx, name); err != nil {
			return SavedCompare{}, err
		} else if !ok {
			return SavedCompare{}, fmt.Errorf("symmetric preview: %q is not a branch or commit", name)
		}
	}
	repo, err := s.LinkRepo(ctx)
	if err != nil {
		return SavedCompare{}, err
	}
	sym := Symmetric{A: a, B: b, Base: base}
	if label == "" {
		label = sym.DefaultLabel()
	}
	return s.SavedCompareAdd(ctx, previewLinkText(repo, a, base), previewLinkText(repo, b, base), label)
}

// previewLinkText is the whole-tree link of the merge preview "source into
// target" — the spelling entryFromPreview stores, target first.
func previewLinkText(repo model.LinkRepo, source, target string) string {
	return model.Link{Repo: repo, Target: model.LinkTarget{
		State:   model.StateCommitted,
		Preview: &model.LinkPreview{Source: source, Target: target},
	}}.String()
}
```

Also refactor `entryFromPreview` (in `preview.go`) to build its link with `previewLinkText`, so there is one construction:

```go
l, err := model.ParseLink(previewLinkText(repo, p.Source, p.Target))
```

Before relying on these, check two things:
- `model.LinkRepo` is comparable (`l.Repo != r.Repo`). Run `grep -n "type LinkRepo struct" -A6 internal/model/link.go`. If it holds a slice, compare `.String()` instead.
- `model.LinkHint` has a `Kind` field (confirmed: `LinkHint{Kind, ID}`).

- [ ] **Step 4: Run the tests and check they pass**

Run: `go test ./internal/domain -run 'Symmetric|Preview' -count=1`
Expected: PASS. The existing `Preview*` tests must also stay green after the refactor.

- [ ] **Step 5: Commit** `feat(domain): symmetric merge preview — SymmetricPreviewAdd + SymmetricOf`

---

### Task 2: TUI — create one from the pair menu (base popup + loading)

**Files:**
- Create: `internal/tui/symmetric_popup.go`
- Modify: `internal/tui/mark.go` (a new pairOp and the `stacked` field), `internal/tui/pairop_popup.go` (the enter path honours `stacked`), `internal/tui/preview_marks.go` (extract `openCompareWithLoading`), `internal/tui/model.go` (the `symmetricSavedMsg` case in Update), `internal/tui/opaffected*.go` if saving is an op (it is NOT an op, it is a tea.Cmd, so no mapping is needed)
- Modify: `internal/i18n/lang/{ja,ko,ru,zh}.toml`
- Test: `internal/tui/symmetric_popup_test.go`

**Interfaces:**
- Consumes: `domain.SymmetricPreviewAdd`, `domain.ErrSavedCompareExists`, `domain.Symmetric.DefaultLabel`
- Produces:
  - `func (m Model) openCompareWithLoading(left, right, subject string) (Model, tea.Cmd)`, used by Task 3
  - `type symmetricSavedMsg struct{ c domain.SavedCompare; existed bool; err error }`

- [ ] **Step 1: Write the failing tests**

```go
package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// symModel: mergePreviewModel (main, feat/x) plus feat/y off main.
func symModel(t *testing.T) Model {
	t.Helper()
	m, dir, _ := mergePreviewModel(t)
	runGit(t, dir, "branch", "feat/y", "main")
	runGit(t, dir, "checkout", "-q", "feat/y")
	writeFile(t, dir, "y.txt", "y\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-q", "-m", "add y")
	runGit(t, dir, "checkout", "-q", "main")
	updated, _ := m.Update(m.loadCmd()())
	m = updated.(Model)
	m.width, m.height = 160, 40
	if len(m.worktrees) > 0 {
		m.currentWorktree = m.worktrees[0].Path
	}
	return m
}

func typeText(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		m, _ = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func TestSymmetricPairRowOpensAnEmptyBasePopup(t *testing.T) {
	t.Parallel()
	m := symModel(t)
	m, _ = m.openSymmetricBase("feat/x", "feat/y")
	p, ok := m.topLayer().(*symmetricBasePopup)
	if !ok || p.base.Value() != "" {
		t.Fatalf("top layer = %T, base %q; want an EMPTY base popup", m.topLayer(), p.base.Value())
	}
	// Enter on an empty base does nothing: there is no default.
	m, cmd := send(m, keyType(tea.KeyEnter))
	if cmd != nil {
		t.Fatal("enter with no base must not save")
	}
	if _, ok := m.topLayer().(*symmetricBasePopup); !ok {
		t.Fatal("the popup must stay open")
	}
}

func TestSymmetricBaseRefusesAOrB(t *testing.T) {
	t.Parallel()
	m := symModel(t)
	m, _ = m.openSymmetricBase("feat/x", "feat/y")
	m = typeText(t, m, "feat/x")
	m, cmd := send(m, keyType(tea.KeyEnter))
	if cmd != nil || !strings.Contains(m.statusMsg, "base") {
		t.Fatalf("base == A must be refused in place: cmd=%v status=%q", cmd, m.statusMsg)
	}
}

func TestSymmetricSaveOpensTheComparisonWithLoading(t *testing.T) {
	t.Parallel()
	m := symModel(t)
	m, _ = m.openSymmetricBase("feat/x", "feat/y")
	m = typeText(t, m, "main")
	m, cmd := send(m, keyType(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("enter with a base must save")
	}
	m, cmd = send(m, cmd()) // symmetricSavedMsg
	if _, ok := m.topLayer().(*compareLoadingPopup); !ok {
		t.Fatalf("the save must open the comparison behind a loading popup, top = %T", m.topLayer())
	}
	m = pumpAll(t, m, cmd)
	if m.filesSets == nil || !strings.Contains(m.filesSets.LeftText, "@main...feat/x") || !strings.Contains(m.filesSets.RightText, "@main...feat/y") {
		t.Fatalf("comparison did not open: %+v (status %q)", m.filesSets, m.statusMsg)
	}
	cs, _ := m.svc.SavedCompareList(context.Background())
	if len(cs) != 2 { // the fixture's merge preview + the symmetric entry
		t.Fatalf("stored = %+v", cs)
	}
}

func TestSymmetricBaseEscReturnsToThePairMenu(t *testing.T) {
	t.Parallel()
	m := symModel(t)
	m = m.pushLayer(&pairOpPopup{marked: "feat/x", selected: "feat/y", ops: pairOpsFor(panelBranches)})
	m.layerTop().(*pairOpPopup).sel = len(pairOpsFor(panelBranches)) - 1 // the symmetric row is last
	m, _ = send(m, keyType(tea.KeyEnter))
	if _, ok := m.topLayer().(*symmetricBasePopup); !ok {
		t.Fatalf("top = %T", m.topLayer())
	}
	m, _ = send(m, keyType(tea.KeyEsc))
	if _, ok := m.topLayer().(*pairOpPopup); !ok {
		t.Fatalf("esc must return to the pair menu, top = %T", m.topLayer())
	}
}
```

Check the helper names before running: `send`, `keyType`, `pumpAll`, `runGit` and `writeFile` exist in tui tests. Run `grep -rn "^func writeFile\|^func send(\|^func keyType\|^func pumpAll\|func (m Model) topLayer\|^func runGit" internal/tui/*.go`. Replace `writeFile` with `os.WriteFile` if it is absent. Use the real `pairOpPopup` field names (`marked`, `selected`, `ops`, `sel`) and the layer accessor as they appear in `pairop_popup.go`; use `topLayer()` if there is no `layerTop`.

- [ ] **Step 2: Run the tests and watch them fail**

Run: `go test ./internal/tui -run Symmetric -count=1`
Expected: a build failure, because `openSymmetricBase` and `symmetricBasePopup` are undefined.

- [ ] **Step 3: Implement**

In `mark.go`, add a field to `pairOp`:

```go
	// stacked keeps the pair menu under what open pushes, so esc there returns
	// to it (a window opened from a popup returns to that popup). The mark is
	// kept too; the pushed window clears it when it hands off.
	stacked bool
```

and a last entry in `pairOpsFor`:

```go
		{
			// Two branches that fix the same thing, each measured against a
			// base the user names (no default): saved as one comparison and
			// opened at once.
			label:   func(marked, selected string) string { return i18n.T("Symmetric merge preview %s, %s…", marked, selected) },
			enabled: true,
			stacked: true,
			open: func(m Model, marked, selected string) (Model, tea.Cmd) {
				return m.openSymmetricBase(marked, selected)
			},
		},
```

In `pairop_popup.go`'s enter path, before `m = m.popLayer()`:

```go
		if op.open != nil && op.stacked {
			return op.open(m, marked, selected)
		}
```

In `preview_marks.go`, extract the loading push from `compareMarkedPreviews`:

```go
// openCompareWithLoading starts the link comparison and, while it loads,
// shows the "comparing…" popup (esc cancels). A no-op start — the same
// comparison already on screen — pushes nothing.
func (m Model) openCompareWithLoading(left, right, subject string) (Model, tea.Cmd) {
	m, cmd := m.startLinkCompare(left, right)
	if cmd == nil {
		return m, nil
	}
	return m.pushLayer(&compareLoadingPopup{tag: m.linkCompareWant, subject: subject}), cmd
}
```

Make `compareMarkedPreviews` call `return m.openCompareWithLoading(links[0], links[1], subject)`.

Create `symmetric_popup.go`:

```go
package tui

import (
	"context"
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// symmetricBasePopup asks for the BASE of a symmetric merge preview of (a, b).
// The field starts EMPTY and stays so until the user types: the base is never
// guessed. Completion is the preview form's (branchSuggestions); tab accepts
// the top suggestion. It sits ON the pair menu, so esc returns there.
type symmetricBasePopup struct {
	popupMax
	a, b string
	base textfield
}

type symmetricSavedMsg struct {
	c       domain.SavedCompare
	existed bool
	err     error
}

func (m Model) openSymmetricBase(a, b string) (Model, tea.Cmd) {
	return m.pushLayer(&symmetricBasePopup{a: a, b: b, base: newTextField("")}), nil
}

func (p *symmetricBasePopup) accept(m Model) {
	if m.namesABranch(p.base.Value()) {
		return
	}
	if s := m.branchSuggestions(p.base.Value()); len(s) > 0 {
		p.base = newTextField(s[0])
	}
}

func (p *symmetricBasePopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyTab:
		p.accept(m)
	case tea.KeyEnter:
		p.accept(m)
		base := strings.TrimSpace(p.base.Value())
		if base == "" {
			return m, nil
		}
		if base == p.a || base == p.b {
			m.statusMsg = i18n.T("the base must differ from %s and %s", p.a, p.b)
			return m, nil
		}
		// Handing off to a save + open: drop the popup AND the pair menu
		// under it, and the mark they were opened from.
		m = m.clearLayers()
		m.mark = nil
		m.statusMsg = i18n.T("saving symmetric merge preview…")
		return m, m.symmetricAddCmd(p.a, p.b, base)
	case tea.KeySpace:
		// branch names cannot contain spaces
	default:
		p.base.HandleEditKey(msg)
	}
	return m, nil
}

func (m Model) symmetricAddCmd(a, b, base string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		c, err := svc.SymmetricPreviewAdd(context.Background(), a, b, base, "")
		existed := errors.Is(err, domain.ErrSavedCompareExists)
		if existed {
			err = nil
		}
		return symmetricSavedMsg{c: c, existed: existed, err: err}
	}
}

// symmetricSaved lands the save: an error is said, otherwise the row is
// refreshed into Previews and the comparison opens behind the loading popup.
func (m Model) symmetricSaved(msg symmetricSavedMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		m.statusMsg = i18n.T("symmetric merge preview: %s", msg.err.Error())
		return m, nil
	}
	m.statusMsg = i18n.T("saved to previews: %s", msg.c.Label)
	if msg.existed {
		m.statusMsg = i18n.T("already saved as %s", msg.c.Label)
	}
	m, open := m.openCompareWithLoading(msg.c.Left, msg.c.Right, msg.c.Label)
	m, reload := m.chainPreviewsRead()
	return m, tea.Batch(open, reload)
}

func (p *symmetricBasePopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

func (p *symmetricBasePopup) box(m Model) string {
	w, _ := m.overlayDims()
	var b strings.Builder
	b.WriteString(i18n.T("Symmetric merge preview") + "\n\n")
	b.WriteString(i18n.T("compare what %s and %s would each bring into a base", p.a, p.b) + "\n\n")
	b.WriteString(i18n.T("base:") + " " + p.base.View() + "\n")
	if s := m.branchSuggestions(p.base.Value()); len(s) > 0 {
		b.WriteString("  " + strings.Join(s, "  ") + "\n")
	}
	b.WriteString("\n" + i18n.T("[enter] save and open  [tab] complete  [esc] back"))
	return popupBox(popupInnerWidth(w), b.String())
}
```

Before writing this, check a few names:
- `clearLayers`, `chainPreviewsRead` (its signature is `(Model, tea.Cmd)` in `savedCompare`), `textfield.View` and `popupBox`. Run `grep -n "func (m Model) clearLayers\|func (m Model) chainPreviewsRead\|func (f \*\?textfield) View\|func popupBox" internal/tui/*.go`.
- Copy how `previewAddPopup.box` renders its field (it may use a cursor helper rather than `View()`) and use the same.

In `model.go` Update, next to the `compareSavedMsg` case:

```go
	case symmetricSavedMsg:
		return m.symmetricSaved(msg)
```

- [ ] **Step 4: Add the i18n keys to all four bundles**

The keys are: `"Symmetric merge preview %s, %s…"`, `"Symmetric merge preview"`, `"compare what %s and %s would each bring into a base"`, `"base:"`, `"[enter] save and open  [tab] complete  [esc] back"`, `"the base must differ from %s and %s"`, `"saving symmetric merge preview…"` and `"symmetric merge preview: %s"`. Check first whether `"saved to previews: %s"` and `"already saved as %s"` already exist.

Follow the `adding-translations` skill: every `%s` stays in order unless the bundle uses the reordered-verb form. Example `ja`: `"Symmetric merge preview %s, %s…" = "%s と %s の対称マージプレビュー…"`.

- [ ] **Step 5: Run the tests and check they pass**

Run: `go test ./internal/tui -run 'Symmetric|PreviewSecondSpace|I18n|Vocab|MenuLabel|RenderOrder' -count=1`
Expected: PASS, i18n gates included.

- [ ] **Step 6: Commit** `feat(tui): symmetric merge preview from the branch pair menu`

---

### Task 3: TUI — the Previews row (badge, subject, `.` rows, enter loading, help)

**Files:**
- Modify: `internal/tui/preview_panel.go` (`previewRow.sym`, `subject`, readPreviews fills `sym`), `internal/tui/model.go` (enter on a compare row → `openCompareWithLoading`), `internal/tui/action_menu.go` (append `previewSymmetricSideRows`), `internal/tui/help.go`
- Create: `internal/tui/preview_symmetric.go`
- Modify: the four i18n bundles
- Test: `internal/tui/preview_symmetric_test.go`

**Interfaces:**
- Consumes: `domain.SymmetricOf`, `openCompareWithLoading` (Task 2), `openPreviewCmd(id, source, target, path string) tea.Cmd` (existing)
- Produces: `previewRow.sym *domain.Symmetric`

- [ ] **Step 1: Write the failing tests**

```go
package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func symRowModel(t *testing.T) Model {
	t.Helper()
	m := symModel(t)
	if _, err := m.svc.SymmetricPreviewAdd(context.Background(), "feat/x", "feat/y", "main", ""); err != nil {
		t.Fatal(err)
	}
	m, cmd := m.reloadSourcesCmd([]sourceKey{srcPreviews}, reloadOpts{manual: true})
	updated, _ := m.Update(cmd())
	m = updated.(Model).activateTab(panelPreviews)
	for i, r := range m.previews {
		if r.sym != nil {
			m.sel[panelPreviews] = i
			return m
		}
	}
	t.Fatalf("no symmetric row among %+v", m.previews)
	return m
}

func TestSymmetricRowWearsBadgeAndBase(t *testing.T) {
	t.Parallel()
	out := symRowModel(t).View()
	if !strings.Contains(out, "sym") || !strings.Contains(out, "feat/x vs feat/y") || !strings.Contains(out, "base: main") {
		t.Fatalf("row must show sym, A vs B and the base:\n%s", out)
	}
	if strings.Contains(out, "↔") {
		t.Fatalf("a symmetric row draws no ↔:\n%s", out)
	}
}

func TestSymmetricRowEnterShowsLoading(t *testing.T) {
	t.Parallel()
	m := symRowModel(t)
	m, cmd := send(m, keyType(tea.KeyEnter))
	if _, ok := m.topLayer().(*compareLoadingPopup); !ok || cmd == nil {
		t.Fatalf("enter must open behind the loading popup, top = %T", m.topLayer())
	}
}

func TestSymmetricRowDotMenuOpensEachSide(t *testing.T) {
	t.Parallel()
	m := symRowModel(t)
	rows := m.previewSymmetricSideRows()
	if len(rows) != 2 {
		t.Fatalf("want 2 side rows, got %d", len(rows))
	}
	if !strings.Contains(rows[0].label, "feat/x → main") || !strings.Contains(rows[1].label, "feat/y → main") {
		t.Fatalf("labels = %q, %q", rows[0].label, rows[1].label)
	}
	_, cmd := rows[0].run(m)
	if cmd == nil {
		t.Fatal("opening a side must start the preview open")
	}
}
```

- [ ] **Step 2: Run the tests and watch them fail** with `go test ./internal/tui -run SymmetricRow -count=1`. They fail because `sym` and `previewSymmetricSideRows` are undefined.

- [ ] **Step 3: Implement**

`preview_panel.go`: add the field `sym *domain.Symmetric // rowCompare: non-nil when the comparison is a symmetric merge preview`. In readPreviews' comparisons loop:

```go
		row := previewRow{kind: rowCompare, cmp: c,
			cmpDesc: [2]string{describeLinkText(ctx, svc, c.Left), describeLinkText(ctx, svc, c.Right)}}
		if s, ok := domain.SymmetricOf(c); ok {
			row.sym = &s
		}
		rows = append(rows, row)
```

In `subject()`, inside `case rowCompare:`:

```go
		if r.sym != nil {
			return "sym  " + i18n.T("%s vs %s", r.sym.A, r.sym.B) + "  " + i18n.T("base: %s", r.sym.Base)
		}
```

Also check the `.` menu entry "Save reversed preview" (`previewSwapCmd`) for a symmetric row. It saves the reversed pair (B vs A, same base), which is a valid entry that is also symmetric. Leave it as is.

`model.go` (enter on Previews), replace `return m.startLinkCompare(c.Left, c.Right)` with:

```go
						return m.openCompareWithLoading(c.Left, c.Right, r.label())
```

Create `preview_symmetric.go`:

```go
package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
)

// previewSymmetricSideRows are the `.` menu's two extra rows on a symmetric
// row: open either side as the ordinary merge preview it is made of.
func (m Model) previewSymmetricSideRows() []actionRow {
	if m.focus != panelPreviews || !m.opsIdle() {
		return nil
	}
	r, ok := m.selectedPreview()
	if !ok || r.sym == nil {
		return nil
	}
	s := *r.sym
	side := func(id, source string) actionRow {
		return actionRow{
			id:    id,
			label: i18n.T("Open merge preview %s → %s", source, s.Base),
			run: func(m Model) (tea.Model, tea.Cmd) {
				return m, m.openPreviewCmd("", source, s.Base, "")
			},
		}
	}
	return []actionRow{side("preview-sym-a", s.A), side("preview-sym-b", s.B)}
}
```

In `action_menu.go`, after the `previewMarksClearRow` block, add `out = append(out, m.previewSymmetricSideRows()...)`.

In `help.go`, in the Previews section, add:

```go
r("", i18n.T("symmetric merge preview: mark branch A (m), move to B, m → \"Symmetric merge preview…\", type the base — saved as one Previews row (sym) comparing what A and B would each bring into the base; . opens either side")),
```

Also add a Branches-section line if pair ops are listed there; check with `grep -n "Merge preview" internal/tui/help.go`.

- [ ] **Step 4: Add the i18n keys** `"%s vs %s"`, `"base: %s"`, `"Open merge preview %s → %s"` and the help line to all four bundles. Check first whether `"Open merge preview %s → %s"` already exists (`grep -n '"Open merge preview' internal/i18n/lang/ja.toml`).

- [ ] **Step 5: Run the tests** with `go test ./internal/tui -count=1`. The whole package must pass, including the i18n and footer gates.

- [ ] **Step 6: Headless check.** Build `go build -o /tmp/claude-1000/gg-sym ./cmd/gg`. In a scratch repo with main, feat/x and feat/y, run `./tui-capture.sh` with a keyscript that marks feat/x, moves to feat/y, presses `m`, picks the last row, types `main` and presses enter. Assert that the snapshot shows the "comparing…" popup, then the compare view, then the Previews row with `sym`. Follow the `driving-tui-headless` skill for the exact keyscript syntax.

- [ ] **Step 7: Commit** `feat(tui): symmetric rows in Previews — badge, side opens, loading on enter`

---

### Task 4: Web server — the route + the `symmetric` field

**Files:**
- Modify: `internal/web/savedcompares.go`
- Test: `internal/web/savedcompares_symmetric_test.go`

**Interfaces:**
- Consumes: `domain.SymmetricPreviewAdd`, `domain.SymmetricOf`
- Produces:
  - The wire route `POST /api/saved-compares/symmetric {a, b, base, label}` → `200 {entry}` | `409 {error, id, label}` | `400 {error}` | `503`
  - `savedCompareRow.Symmetric *symWire` (JSON `symmetric: {a, b, base}`) on every comparison row that is symmetric

- [ ] **Step 1: Write the failing test**

```go
package web

import (
	"context"
	"net/http"
	"testing"
)

func TestSavedCompareSymmetric(t *testing.T) {
	dir, _, _ := linkRepo(t) // main, feat/x
	gitRun(t, dir, "branch", "feat/y", "main")
	gitRun(t, dir, "checkout", "-q", "feat/y")
	gitRun(t, dir, "commit", "-q", "--allow-empty", "-m", "y")
	gitRun(t, dir, "checkout", "-q", "main")
	srv, svc := linkServer(t, dir)
	ts := serve(t, srv)
	body := jsonBody(t, map[string]string{"a": "feat/x", "b": "feat/y", "base": "main"})

	var added struct {
		Entry struct {
			ID, Kind, Label, Left, Right string
			Symmetric                    *struct{ A, B, Base string }
		}
	}
	if code := postDecode(t, ts, "/api/saved-compares/symmetric", body, "application/json", &added); code != http.StatusOK {
		t.Fatalf("add: %d", code)
	}
	if added.Entry.Kind != "compare" || added.Entry.Symmetric == nil || added.Entry.Symmetric.Base != "main" || added.Entry.Symmetric.A != "feat/x" {
		t.Fatalf("entry = %+v", added.Entry)
	}
	var dup savedResp
	if code := postDecode(t, ts, "/api/saved-compares/symmetric", body, "application/json", &dup); code != http.StatusConflict || dup.ID != added.Entry.ID {
		t.Fatalf("duplicate: %d %+v", code, dup)
	}
	for name, b := range map[string]string{
		"no base":   jsonBody(t, map[string]string{"a": "feat/x", "b": "feat/y"}),
		"base == a": jsonBody(t, map[string]string{"a": "feat/x", "b": "feat/y", "base": "feat/x"}),
		"unknown":   jsonBody(t, map[string]string{"a": "feat/x", "b": "nope", "base": "main"}),
		"not json":  `{`,
	} {
		if code := postDecode(t, ts, "/api/saved-compares/symmetric", b, "application/json", nil); code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", name, code)
		}
	}
	if code := postDecode(t, ts, "/api/saved-compares/symmetric", body, "text/plain", nil); code != http.StatusUnsupportedMediaType {
		t.Errorf("writeGuard: %d", code)
	}
	// The list carries the recognised branches too — the page parses no links.
	var list savedResp
	getDecode(t, ts, "/api/saved-compares", &list)
	if cs, _ := svc.SavedCompareList(context.Background()); len(cs) != 1 {
		t.Fatalf("stored %+v", cs)
	}
}
```

Find the GET helper with `grep -n "func getDecode\|func getJSON" internal/web/*_test.go`. If there is none, decode `/api/saved-compares` inline with `http.Get`. Assert on the RAW JSON that the entry has `"symmetric":{"a":"feat/x","b":"feat/y","base":"main"}`.

- [ ] **Step 2: Watch it fail** with `go test ./internal/web -run TestSavedCompareSymmetric -count=1`. It returns 404 or 405.

- [ ] **Step 3: Implement** in `savedcompares.go`:

```go
		mux.HandleFunc("POST /api/saved-compares/symmetric", writeGuard(s.handleSymmetricAdd))
```

```go
// symWire is a symmetric merge preview's three branches, recognised by domain
// so the page never parses a link.
type symWire struct {
	A    string `json:"a"`
	B    string `json:"b"`
	Base string `json:"base"`
}
```

Add a field `Symmetric *symWire `json:"symmetric,omitempty"`` to `savedCompareRow`, and change `comparisonRow`:

```go
func comparisonRow(c domain.SavedCompare) savedCompareRow {
	row := savedCompareRow{ID: c.ID, Label: c.Label, Kind: "compare", Left: c.Left, Right: c.Right}
	if s, ok := domain.SymmetricOf(c); ok {
		row.Symmetric = &symWire{A: s.A, B: s.B, Base: s.Base}
	}
	return row
}
```

```go
// handleSymmetricAdd saves a symmetric merge preview ({a, b, base, label}).
// Every refusal domain makes before storing (a missing or repeated name, an
// unknown one) is the caller's input, so it is a 400.
func (s *Server) handleSymmetricAdd(w http.ResponseWriter, r *http.Request) {
	var req struct{ A, B, Base, Label string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSavedCompareBody)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad request body: %w", err))
		return
	}
	c, err := s.service().SymmetricPreviewAdd(readCtx(r), req.A, req.B, req.Base, req.Label)
	switch {
	case errors.Is(err, domain.ErrSavedCompareExists):
		writeAlreadySaved(w, c.ID, c.Label)
	case errors.Is(err, domain.ErrSavedComparesDisabled):
		writeErr(w, http.StatusServiceUnavailable, err)
	case err != nil:
		writeErr(w, http.StatusBadRequest, err)
	default:
		s.emitPreviews()
		writeJSON(w, map[string]any{"entry": comparisonRow(c)})
	}
}
```

Check that the GET list handler builds its comparison rows through `comparisonRow`: `grep -n "comparisonRow(" internal/web/savedcompares.go`. If it builds them inline, route it through `comparisonRow`.

- [ ] **Step 4: Run the tests** with `go test ./internal/web -run 'SavedCompare' -count=1`. Expected: PASS.

- [ ] **Step 5: Commit** `feat(web): POST /api/saved-compares/symmetric + symmetric on comparison rows`

---

### Task 5: Web client — the drop menu, the Previews menu flow, row badge + menu, busy

**Files:**
- Modify: `internal/web/static/previews.js`, `internal/web/static/sidebar.js`, and the stylesheet only if `.psub` does not suit the badge (reuse existing classes; a new class needs a visible style)
- Test: a Playwright probe in the scratchpad (not committed unless the repo keeps probes; check `ls internal/web/*probe* e2e/web* 2>/dev/null`)

**Interfaces:**
- Consumes: the Task 4 route and `row.symmetric`, plus `runLinkCompare`, `runOnce`, `opLine`, `openPrompt`, `knownName`, `openPreviewForPair(source, target)` (existing, previews.js:339)
- Produces: `window.__ggSymmetricPreview(a, b)`

- [ ] **Step 1: Implement in `previews.js`**

```js
// A SYMMETRIC merge preview: what a and b would each bring into a base the
// user names — no default, the prompt starts empty. Saved as one comparison
// (the server recognises the shape) and opened at once. runOnce: a save + a
// two-sided compare can take a minute on a big repo, and a second click
// must not stack a second one.
function symmetricPreviewFlow(a, b) {
  openPrompt({
    title: "Symmetric merge preview — " + a + " and " + b + " — base branch:",
    value: "",
    onSubmit: (base) => {
      base = (base || "").trim();
      if (!base) {
        opLine("name a base branch", true);
        return false;
      }
      if (!knownName(base)) {
        opLine("unknown branch " + base, true);
        return false;
      }
      if (base === a || base === b) {
        opLine("the base must differ from " + a + " and " + b, true);
        return false;
      }
      const run = runOnce("symmetric-preview", () => saveSymmetric(a, b, base));
      if (!run) opLine("a symmetric merge preview is already being saved…");
    },
  });
}

async function saveSymmetric(a, b, base) {
  opLine("saving symmetric merge preview " + a + " vs " + b + " (base: " + base + ")…");
  let entry;
  try {
    entry = (await postJSON("/api/saved-compares/symmetric", { a, b, base, label: "" })).entry;
  } catch (err) {
    if (!(err.data && err.data.id)) {
      opLine("symmetric merge preview: " + (err.message || err), true);
      return;
    }
    entry = { id: err.data.id, label: err.data.label };
    opLine("already saved as " + err.data.label);
  }
  refresh();
  await runLinkCompare("id=" + encodeURIComponent(entry.id));
}

// The Previews menu entry: prompt a, then b, then the base.
function newSymmetricFlow() {
  openPrompt({
    title: "Symmetric merge preview — first branch:",
    value: "",
    onSubmit: (a) => {
      if (!knownName(a)) return void opLine("unknown branch " + a, true);
      openPrompt({
        title: "Symmetric merge preview — second branch:",
        value: "",
        onSubmit: (b) => {
          if (!knownName(b)) return void opLine("unknown branch " + b, true);
          if (a === b) return void opLine("the two branches are the same", true);
          symmetricPreviewFlow(a, b);
        },
      });
    },
  });
}
window.__ggSymmetricPreview = symmetricPreviewFlow;
```

Check `openPrompt`'s contract before relying on it. Does returning `false` from `onSubmit` keep the prompt open? Run `grep -n "function openPrompt" -A40 internal/web/static/*.js`. If it does not, re-open the prompt on refusal with the typed value, as `addPreviewFlow` does. Check whether `postJSON` rejects with `err.data`; `saveSaved` relies on it, so it does. Import `runOnce` and `postJSON` from `./core.js` if they are not imported already.

Change `registerRows("menu", …)` to:

```js
registerRows("menu", () => [
  { label: "new merge preview…", act: addPreviewFlow },
  { label: "new symmetric merge preview…", act: newSymmetricFlow },
]);
```

In `savedRowHTML`, for a symmetric row:

```js
  const sym = e.kind === "compare" && e.symmetric;
  const sub = e.kind === "pair"
    ? e.a.slice(0, 8) + ".." + e.b.slice(0, 8)
    : sym ? e.symmetric.a + " vs " + e.symmetric.b + " · base: " + e.symmetric.base
    : e.left_desc + " ↔ " + e.right_desc;
```

Then paint the badge before the label as `(sym ? '<span class="psub">sym</span>' : "")`. It reuses `.psub`, which is visible in both themes. Do NOT invent a class that has no rule (see the memory "a new class with no #id.hidden rule").

In `showComparisonMenu`, when `e.symmetric`, insert after "open comparison":

```js
      ...(e.symmetric
        ? [
            { label: "open merge preview " + e.symmetric.a + " → " + e.symmetric.base, act: () => openPreviewForPair(e.symmetric.a, e.symmetric.base) },
            { label: "open merge preview " + e.symmetric.b + " → " + e.symmetric.base, act: () => openPreviewForPair(e.symmetric.b, e.symmetric.base) },
          ]
        : []),
```

Check `openPreviewForPair`'s argument order (source, target) at previews.js:339.

- [ ] **Step 2: Implement in `sidebar.js`**, `showBranchPairMenu`, after the compare row:

```js
  // Two branches that fix the same thing, each against a base the user names.
  items.push({ label: "symmetric merge preview " + src + ", " + dst + "…", act: () => window.__ggSymmetricPreview && window.__ggSymmetricPreview(src, dst) });
```

- [ ] **Step 3: Run the JS unit tests if the repo has them.** Find them with `ls internal/web/static/*.test.* internal/web/*_js_test.go 2>/dev/null`, then run `go test ./internal/web -count=1`.

- [ ] **Step 4: Playwright probe.** Follow the `playwright-web-verification` memory: rebuild, restart, hard-reload, and prove that the port is yours via `/api/repo`.
  - Build `go build -o <scratch>/gg-sym ./cmd/gg`.
  - In a scratch repo with main, feat/x and feat/y, run `gg-sym web --addr 127.0.0.1:<port>`.
  - The probe has to:
    1. Open the Previews menu → "new symmetric merge preview…", type feat/x, then feat/y. Assert the base prompt's input value is `""`.
    2. Submit an empty base → the prompt stays and the opLine shows an error.
    3. Type `main` and submit. Assert that the opLine shows "saving symmetric…" or "comparing…" and that the compare view becomes **visible** (bounding box > 0 and not `.hidden`).
    4. Assert that the Previews list has a VISIBLE `li[data-kind=compare]` containing `sym` and `base: main`.
    5. Right-click it → assert the two "open merge preview … → main" rows are visible.
    6. Drag the branch feat/x onto feat/y → assert the menu row "symmetric merge preview feat/x, feat/y…" is visible.
  - Run the probe against `main`'s binary first. It must FAIL at step 1.

- [ ] **Step 5: Commit** `feat(web): symmetric merge preview — drop menu, Previews flow, sym rows`

---

### Task 6: CLI — `gg preview add --symmetric --base`

**Files:**
- Modify: `internal/cli/preview.go`
- Create: `e2e/scenarios/s96_symmetric_preview.toml` (check that the number is free: `ls e2e/scenarios | sort | tail -3`)
- Modify: `internal/agentskill/using-gg.md`, `internal/agentskill/agentskill.go` (`Version = 91`)

**Interfaces:**
- Consumes: `domain.SymmetricPreviewAdd`, `domain.ErrSavedCompareExists`

- [ ] **Step 1: Write the failing e2e scenario**

```toml
name = "symmetric merge preview: gg preview add --symmetric --base"

[input]
steps = [
  { write = "a.txt", content = "alpha\n" },
  { commit = "seed" },
  { branch = "feat/x" },
  { branch = "feat/y" },
]

# No --base is a usage error: the base is never guessed.
[[run]]
cmd  = ["preview", "add", "--symmetric", "feat/x", "feat/y"]
exit = 2

# base == one of the branches is refused, and nothing is stored.
[[run]]
cmd  = ["preview", "add", "--symmetric", "--base", "feat/x", "feat/x", "feat/y"]
exit = 1

# The save prints the new id on stdout.
[[run]]
cmd             = ["preview", "add", "--symmetric", "--base", "main", "feat/x", "feat/y"]
exit            = 0

# --list shows ONE comparison whose two links spell the base first.
[[run]]
cmd             = ["compare", "--list"]
exit            = 0
stdout_contains = ["feat/x vs feat/y (base: main)", "main...feat/x", "main...feat/y"]

# A duplicate is reused, not an error.
[[run]]
cmd             = ["preview", "add", "--symmetric", "--base", "main", "feat/x", "feat/y"]
exit            = 0
stderr_contains = ["already saved"]

# It runs like any saved comparison.
[[run]]
cmd  = ["compare", "--saved", "feat/x vs feat/y (base: main)"]
exit = 0
```

Before writing, check the step vocabulary: does `{ branch = … }` create a branch without switching, and is the default branch `main`? Read the top of `e2e/scenarios/s88_merge_preview.toml` and copy its input shape. Check that `stderr_contains` exists: `grep -rn "stderr_contains" e2e/*.go | head -2`.

- [ ] **Step 2: Watch it fail** with `go test ./e2e -run 'TestScenarios/s96' -count=1`. The scenario-name filter depends on the harness; check with `grep -n "t.Run(" e2e/*.go`. Expected: FAIL, because `--symmetric` is an unknown flag and the command exits 2 on the save step.

- [ ] **Step 3: Implement** in `previewAdd`:

```go
	symmetric := fs.Bool("symmetric", false, "save a symmetric merge preview of <a> <b> against --base")
	base := fs.String("base", "", "the base branch of a --symmetric preview (required, no default)")
```

After `fs.Parse`, add:

```go
	if *symmetric {
		if fs.NArg() != 2 || strings.TrimSpace(*base) == "" {
			fmt.Fprintln(stderr, "usage: gg preview add --symmetric --base <base> [--label <text>] <a> <b>")
			return 2
		}
		c, err := svc.SymmetricPreviewAdd(context.Background(), fs.Arg(0), fs.Arg(1), *base, *label)
		if errors.Is(err, domain.ErrSavedCompareExists) {
			fmt.Fprintf(stderr, "preview add: already saved as %s (%s)\n", c.ID, c.Label)
			fmt.Fprintln(stdout, c.ID)
			return 0
		}
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		fmt.Fprintln(stdout, c.ID)
		return 0
	}
	if *base != "" {
		fmt.Fprintln(stderr, "preview add: --base is only for --symmetric")
		return 2
	}
```

Update the usage line to add `| --symmetric --base <base> <a> <b>`. The `--label` help text says the default is `"<source> → <target>"`; append `(symmetric: "<a> vs <b> (base: <base>)")`.

- [ ] **Step 4: Run** `go test ./e2e -count=1 -run s96` and `go test ./internal/cli -count=1`. Expected: PASS.

- [ ] **Step 5: Update the agent skill.** In `using-gg.md`, in the preview section, add a line about `gg preview add --symmetric --base <base> <a> <b>`: it saves one comparison `@base...a` vs `@base...b` (live), prints the id, reuses a duplicate, and is opened with `gg compare --saved <id>`. Bump `Version` to 91. Run `go test ./internal/agentskill -count=1`.

- [ ] **Step 6: Commit** `feat(cli): gg preview add --symmetric --base`

---

### Task 7: Docs, gates, delivery

- [ ] **Step 1: `CHANGELOG.md`.** Add an Unreleased entry: "Symmetric merge preview — pick two branches and a base (no default); gg saves one live comparison of what each would bring into the base and opens it (TUI pair menu, web drop menu + Previews menu, `gg preview add --symmetric --base`). Enter on any saved comparison now shows the comparing… popup."
- [ ] **Step 2: `README.md`.** Add one line in the previews section.
- [ ] **Step 3: `docs/CLAUDE-details.md`.** Under previews / savedcompare, note that a symmetric entry is a two-link comparison recognised only by `domain.SymmetricOf` (same repo, same target, whole-tree, different sources), with the target first in the link text. Leave `CLAUDE.md` unchanged (no new package, no new convention).
- [ ] **Step 4: Gates.** Run `./test.sh` (vet + gofmt → unit → e2e), then `./test.sh race`. Start the race run now; don't wait for a quiet machine. Report the failures verbatim if any.
- [ ] **Step 5: Commit the docs**, then build the verify binaries: `go build -o <scratch>/gg ./cmd/gg` and `./build.sh web` (Windows exe). Send the paths to the user.
- [ ] **Step 6: Ask the user before merging.** The merge is `gg merge` into `main` with `--no-ff` plus trailers, then `./build.sh install`, then remove the worktree.

# Interactive Rebase — Slice 4b: the TUI editor — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The GitKraken-style interactive-rebase editor in the TUI: mark two branches → pair-op picker → **"Interactive rebase {marked} onto {selected}"** opens a full-screen view-stack surface listing the range's commits with per-row **Pick/Reword/Squash/Drop**, reorder, and **Reset/Cancel/Start**, building a `rebaseplan.Plan` and running the (already-merged) `engine.InteractiveRebase` op.

**Architecture:** (1) a range-log query `domain.CommitRange(onto, branch)` returning each commit's hash/subject/**full message** (needed for reword pre-fill + squash compose); (2) extract F2's message-field editing into a reusable `(*commitPopup).applyEditKey` so the editor can host reword input **inside the surface** (the view-stack short-circuits Model-level popups — both routing and render return before the popup branches); (3) the `irebaseEditor` surface; (4) a `pairOp.open` hook so the picker can open a view instead of running an op, plus the third Branches pair-op; (5) docs.

**Tech Stack:** Go 1.26, Bubble Tea, `internal/rebaseplan` (Slice 2), `engine.InteractiveRebase` (Slice 3).

**Spec:** `docs/superpowers/specs/2026-06-15-interactive-rebase-design.md` (Slice 4, TUI half).

**Key facts (verified in the codebase):**
- View-stack: `surface{ render(m Model) string; update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) }`; `m.pushSurface(s)` / `m.popSurface()` / `m.stackTop()`. The top surface owns all keys (`model.go`: `if s := m.stackTop(); s != nil { return s.update(m, msg) }`) and the whole screen (`view.go`: `if s := m.stackTop(); s != nil { return clipToHeight(s.render(m), h) }`). Pointer-receiver surfaces (like `*historyView`) so mutations persist.
- `pairOp{ label, build, enabled, note }` in `mark.go`; the picker's `enter` runs `m.startOp(op.build(marked, selected))` (`pairop_popup.go`).
- F2 `commitPopup{title, desc, field}` + `updateCommitPopupKey` + `message()` in `commit_popup.go`.
- Frontend sets `GGBin = os.Executable()`.

**Conventions:** TDD; `internal/tui` reaches git only through `internal/domain`; new keybindings land in BOTH `help.go` and the footer (drift guard `TestHelpFooterCoverage`); gate `./test.sh race`; commits end `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.

---

### Task 1: range-log query (commits with full messages)

**Files:**
- Modify: `internal/model/model.go` (new `RangeCommit` type)
- Create: `internal/git/logrange.go` (+ test `internal/git/logrange_test.go`)
- Modify: `internal/engine/gitops.go` (interface), `internal/domain/query.go` (gated query)

- [ ] **Step 1: Write the failing verb test**

Create `internal/git/logrange_test.go`:

```go
package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLogRangeMessages(t *testing.T) {
	dir, runner := newTestRepo(t) // commit "initial" on main
	repo := &Repo{Runner: runner}
	git := func(args ...string) {
		c := exec.Command("git", append([]string{"-C", dir}, args...)...)
		c.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("checkout", "-b", "work")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644)
	git("add", ".")
	git("commit", "-m", "first")
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644)
	git("add", ".")
	git("commit", "-m", "second line subject", "-m", "body para")

	cs, err := repo.LogRangeMessages(context.Background(), "main", "work")
	if err != nil {
		t.Fatalf("log range: %v", err)
	}
	if len(cs) != 2 {
		t.Fatalf("got %d commits, want 2", len(cs))
	}
	// oldest-first (git todo order)
	if cs[0].Subject != "first" {
		t.Fatalf("cs[0].Subject = %q, want first", cs[0].Subject)
	}
	if cs[1].Subject != "second line subject" {
		t.Fatalf("cs[1].Subject = %q", cs[1].Subject)
	}
	if cs[1].Message != "second line subject\n\nbody para\n" {
		t.Fatalf("cs[1].Message = %q", cs[1].Message)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/git/ -run TestLogRangeMessages -v`
Expected: FAIL — `LogRangeMessages` / `model.RangeCommit` undefined.

- [ ] **Step 3: Add the model type**

In `internal/model/model.go`, add:

```go
// RangeCommit is a commit in a rev-range with its full message, used by the
// interactive-rebase editor (reword pre-fill + squash compose need the body).
type RangeCommit struct {
	Hash    string
	Subject string
	Message string // full message (subject + body), trailing newline preserved
}
```

- [ ] **Step 4: Implement the verb**

Create `internal/git/logrange.go`:

```go
package git

import (
	"context"
	"strings"

	"github.com/gigagit/gg/internal/gitcmd"
	"github.com/gigagit/gg/internal/model"
)

// LogRangeMessages lists onto..branch oldest-first (git todo order) with each
// commit's full message. Records are NUL-separated (-z), fields by 0x1f, so a
// multi-line %B body parses unambiguously.
func (r *Repo) LogRangeMessages(ctx context.Context, onto, branch string) ([]model.RangeCommit, error) {
	argv := gitcmd.New("log").Arg("--reverse", "-z", "--format=%H%x1f%s%x1f%B").
		Arg(onto + ".." + branch).ToArgv()
	res, err := r.Runner.Run(ctx, "git log range", argv)
	if err != nil {
		return nil, err
	}
	var out []model.RangeCommit
	for _, rec := range strings.Split(res.Stdout, "\x00") {
		if strings.TrimSpace(rec) == "" {
			continue
		}
		f := strings.SplitN(rec, "\x1f", 3)
		if len(f) != 3 {
			continue
		}
		out = append(out, model.RangeCommit{Hash: f[0], Subject: f[1], Message: f[2]})
	}
	return out, nil
}
```

> `%B` ends with a trailing newline; with `-z` the record terminator is NUL, so
> the `Message` field keeps git's own trailing `\n` (matched by the test).

- [ ] **Step 5: Add to `GitOps` + a gated domain query**

In `internal/engine/gitops.go`, add to the read-verbs group:

```go
	LogRangeMessages(ctx context.Context, onto, branch string) ([]model.RangeCommit, error)
```

In `internal/domain/query.go`, next to `FileLog`:

```go
// CommitRange lists onto..branch oldest-first with full messages, under a Read
// reservation. Backs the interactive-rebase editor.
func (s *Service) CommitRange(ctx context.Context, onto, branch string) ([]model.RangeCommit, error) {
	return query(ctx, s, "commit-range", func(c context.Context) ([]model.RangeCommit, error) {
		return s.repo.LogRangeMessages(c, onto, branch)
	})
}
```

- [ ] **Step 6: Run + build**

Run: `go build ./... && go test ./internal/git/ -run TestLogRangeMessages -v`
Expected: build clean (`*git.Repo` satisfies the grown interface); test PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/model/model.go internal/git/logrange.go internal/git/logrange_test.go internal/engine/gitops.go internal/domain/query.go
git commit -m "feat(domain): CommitRange — range commits with full messages

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: extract reusable message-field editing from `commitPopup`

**Files:**
- Modify: `internal/tui/commit_popup.go`
- Test: `internal/tui/commit_popup_test.go` (existing F2 tests must stay green)

The editor hosts reword input inside its surface, so the title/description
keystroke handling must be callable without F2's commit-submit. Extract it; F2's
behavior is unchanged.

- [ ] **Step 1: Add `applyEditKey`, refactor `updateCommitPopupKey` to use it**

In `internal/tui/commit_popup.go`, add:

```go
// applyEditKey applies one key to the popup's title/description fields and
// reports control outcomes: submit=true on ctrl+s, cancel=true on esc. Editing
// keys (tab/enter/backspace/space/runes) mutate in place and return false,false.
// ctrl+c is handled by the caller (it quits the program). Reused by F2's commit
// popup and the interactive-rebase editor's reword sub-mode.
func (p *commitPopup) applyEditKey(msg tea.KeyMsg) (submit, cancel bool) {
	switch msg.Type {
	case tea.KeyEsc:
		return false, true
	case tea.KeyCtrlS:
		return true, false
	case tea.KeyTab, tea.KeyShiftTab:
		p.field = (p.field + 1) % 2
	case tea.KeyEnter:
		if p.field == 0 {
			p.field = 1
		} else {
			p.desc += "\n"
		}
	case tea.KeyBackspace:
		if p.field == 0 {
			if r := []rune(p.title); len(r) > 0 {
				p.title = string(r[:len(r)-1])
			}
		} else {
			if r := []rune(p.desc); len(r) > 0 {
				p.desc = string(r[:len(r)-1])
			}
		}
	case tea.KeySpace:
		if p.field == 0 {
			p.title += " "
		} else {
			p.desc += " "
		}
	case tea.KeyRunes:
		if p.field == 0 {
			p.title += string(msg.Runes)
		} else {
			p.desc += string(msg.Runes)
		}
	}
	return false, false
}
```

Then rewrite `updateCommitPopupKey` to delegate (behavior identical to today):

```go
func (m Model) updateCommitPopupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	p := m.commitPopup
	submit, cancel := p.applyEditKey(msg)
	switch {
	case cancel:
		m.commitPopup = nil
	case submit:
		if strings.TrimSpace(p.title) == "" {
			m.statusMsg = "title required"
			return m, nil
		}
		op := engine.Commit{Message: p.message(), Amend: p.amend}
		m.commitPopup = nil
		return m.startOp(op)
	}
	return m, nil
}
```

- [ ] **Step 2: Run the existing F2 popup tests**

Run: `go test ./internal/tui/ -run 'TestCommitPopup|TestCKey|TestCapCKey|TestSplitMessage' -v`
Expected: PASS — the refactor preserves F2 behavior (typing, field switch,
empty-title refusal, commit/amend).

- [ ] **Step 3: Commit**

```bash
git add internal/tui/commit_popup.go
git commit -m "refactor(tui): extract commitPopup.applyEditKey for reuse

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: the `irebaseEditor` surface

**Files:**
- Create: `internal/tui/irebase_view.go`
- Test: `internal/tui/irebase_view_test.go`
- Modify: `internal/tui/model.go` (only if a msg type is needed; the editor is self-contained)

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/irebase_view_test.go`:

```go
package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/model"
	"github.com/gigagit/gg/internal/rebaseplan"
)

// edRows: oldest-first input [c1,c2,c3] → editor shows newest-first [c3,c2,c1].
func edRows() []model.RangeCommit {
	return []model.RangeCommit{
		{Hash: "h1", Subject: "wip1", Message: "wip1\n"},
		{Hash: "h2", Subject: "wip2", Message: "wip2\n"},
		{Hash: "h3", Subject: "wip3", Message: "wip3\n"},
	}
}

func TestIrebaseEditorNewestFirstAndPlanOrder(t *testing.T) {
	e := newIrebaseEditor("work", "main", edRows(), "/bin/gg")
	if e.rows[0].sha != "h3" {
		t.Fatalf("top row = %q, want h3 (newest-first)", e.rows[0].sha)
	}
	// default plan is all-pick, oldest-first
	plan := e.plan()
	if len(plan.Entries) != 3 || plan.Entries[0].Sha != "h1" || plan.Entries[2].Sha != "h3" {
		t.Fatalf("plan order wrong: %+v", plan.Entries)
	}
	for _, en := range plan.Entries {
		if en.Action != rebaseplan.Pick {
			t.Fatalf("default action = %q, want pick", en.Action)
		}
	}
}

func TestIrebaseEditorActionsAndReorder(t *testing.T) {
	e := newIrebaseEditor("work", "main", edRows(), "/bin/gg")
	m := Model{stack: &viewStack{entries: []surface{e}}}
	// focus top row (h3, newest), drop it
	m, _ = e.update(m, key("d"))
	if e.rows[0].action != rebaseplan.Drop {
		t.Fatalf("top action = %q, want drop", e.rows[0].action)
	}
	// move focus down to h2, squash it
	m, _ = e.update(m, key("j"))
	m, _ = e.update(m, key("s"))
	if e.rows[1].action != rebaseplan.Squash {
		t.Fatalf("row1 action = %q, want squash", e.rows[1].action)
	}
	// reorder: move focused row up
	before := e.rows[0].sha
	m, _ = e.update(m, keyType(tea.KeyCtrlUp))
	if e.rows[0].sha == before && len(e.rows) > 1 {
		// row1 should have swapped to top
		t.Fatalf("ctrl+up did not reorder")
	}
	// reset restores all-pick, original order
	m, _ = e.update(m, key("R"))
	if e.rows[0].sha != "h3" || e.rows[0].action != rebaseplan.Pick {
		t.Fatalf("reset did not restore: %+v", e.rows[0])
	}
}

func TestIrebaseEditorSquashOnOldestRefused(t *testing.T) {
	e := newIrebaseEditor("work", "main", edRows(), "/bin/gg")
	m := Model{stack: &viewStack{entries: []surface{e}}}
	// move to the bottom (oldest) row and squash → refused
	m, _ = e.update(m, key("j"))
	m, _ = e.update(m, key("j")) // now on h1 (oldest, last row)
	m, _ = e.update(m, key("s"))
	if e.rows[len(e.rows)-1].action == rebaseplan.Squash {
		t.Fatal("squash on the oldest row must be refused")
	}
}

func TestIrebaseEditorReword(t *testing.T) {
	e := newIrebaseEditor("work", "main", edRows(), "/bin/gg")
	m := Model{stack: &viewStack{entries: []surface{e}}}
	m, _ = e.update(m, key("r")) // open reword for h3
	if e.reword == nil {
		t.Fatal("r must open reword input")
	}
	m, _ = e.update(m, keyRunes("X"))
	m, _ = e.update(m, keyType(tea.KeyCtrlS)) // submit
	if e.reword != nil {
		t.Fatal("ctrl+s must close reword input")
	}
	if e.rows[0].action != rebaseplan.Reword || e.rows[0].newMsg == "" {
		t.Fatalf("reword not stored: %+v", e.rows[0])
	}
}

// key/keyType/keyRunes helpers (reuse if already present in the tui test pkg).
func key(s string) tea.KeyMsg       { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
func keyType(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }
func keyRunes(s string) tea.KeyMsg  { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
```

> If `key`/`keyRunes` collide with existing tui-test helpers (e.g. `pressRune`),
> drop the local definitions and use the existing ones. Keep `keyType` for the
> ctrl+up / ctrl+s messages.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui/ -run TestIrebaseEditor -v`
Expected: FAIL — `newIrebaseEditor`/`irebaseEditor` undefined.

- [ ] **Step 3: Implement the surface**

Create `internal/tui/irebase_view.go`:

```go
package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/model"
	"github.com/gigagit/gg/internal/rebaseplan"
)

// irebaseRow is one editable commit in the editor (displayed newest-first).
type irebaseRow struct {
	sha     string
	subject string
	orig    string // original full message
	action  rebaseplan.Action
	newMsg  string // reword: the new message
}

// irebaseEditor is the GitKraken-style interactive-rebase surface. Rows are
// newest-first for display; plan() reverses to git todo order (oldest-first).
type irebaseEditor struct {
	branch, onto string
	ggBin        string
	rows         []irebaseRow
	orig         []irebaseRow // for Reset
	sel          int
	reword       *commitPopup // non-nil while editing a reword message
}

// newIrebaseEditor builds the editor from oldest-first range commits.
func newIrebaseEditor(branch, onto string, commits []model.RangeCommit, ggBin string) *irebaseEditor {
	rows := make([]irebaseRow, 0, len(commits))
	for i := len(commits) - 1; i >= 0; i-- { // reverse → newest-first
		c := commits[i]
		rows = append(rows, irebaseRow{sha: c.Hash, subject: c.Subject, orig: c.Message, action: rebaseplan.Pick})
	}
	orig := append([]irebaseRow(nil), rows...)
	return &irebaseEditor{branch: branch, onto: onto, ggBin: ggBin, rows: rows, orig: orig}
}

// plan reverses the newest-first rows back to git todo order (oldest-first).
func (e *irebaseEditor) plan() rebaseplan.Plan {
	entries := make([]rebaseplan.Entry, 0, len(e.rows))
	for i := len(e.rows) - 1; i >= 0; i-- {
		r := e.rows[i]
		entries = append(entries, rebaseplan.Entry{Sha: r.sha, Action: r.action, Orig: r.orig, NewMsg: r.newMsg})
	}
	return rebaseplan.Plan{Entries: entries}
}

func (e *irebaseEditor) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	// Reword sub-mode owns input while open.
	if e.reword != nil {
		submit, cancel := e.reword.applyEditKey(msg)
		switch {
		case cancel:
			e.reword = nil
		case submit:
			if strings.TrimSpace(e.reword.title) == "" {
				m.statusMsg = "title required"
				return m, nil
			}
			e.rows[e.sel].action = rebaseplan.Reword
			e.rows[e.sel].newMsg = e.reword.message()
			e.reword = nil
		}
		return m, nil
	}
	switch msg.String() {
	case "esc":
		return m.popSurface(), nil
	case "down", "j":
		if e.sel < len(e.rows)-1 {
			e.sel++
		}
	case "up", "k":
		if e.sel > 0 {
			e.sel--
		}
	case "p":
		e.rows[e.sel].action = rebaseplan.Pick
	case "d":
		e.rows[e.sel].action = rebaseplan.Drop
	case "s":
		// Squash melds into the older neighbor (the row below, newest-first).
		// The oldest row (last) has nothing older — refuse.
		if e.sel == len(e.rows)-1 {
			m.statusMsg = "squash: the oldest commit has nothing to squash into"
			return m, nil
		}
		e.rows[e.sel].action = rebaseplan.Squash
	case "r":
		t, d := splitMessage(e.rows[e.sel].orig)
		if e.rows[e.sel].action == rebaseplan.Reword && e.rows[e.sel].newMsg != "" {
			t, d = splitMessage(e.rows[e.sel].newMsg)
		}
		e.reword = &commitPopup{title: t, desc: d}
	case "ctrl+up":
		if e.sel > 0 {
			e.rows[e.sel-1], e.rows[e.sel] = e.rows[e.sel], e.rows[e.sel-1]
			e.sel--
		}
	case "ctrl+down":
		if e.sel < len(e.rows)-1 {
			e.rows[e.sel+1], e.rows[e.sel] = e.rows[e.sel], e.rows[e.sel+1]
			e.sel++
		}
	case "R":
		e.rows = append([]irebaseRow(nil), e.orig...)
		e.sel = 0
	case "enter":
		op := engine.InteractiveRebase{Branch: e.branch, Onto: e.onto, Plan: e.plan(), GGBin: e.ggBin}
		m = m.popSurface()
		return m.startOp(op)
	}
	return m, nil
}

func (e *irebaseEditor) render(m Model) string {
	w, h := m.width, m.height
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}
	var b strings.Builder
	b.WriteString("Interactive rebase: " + e.branch + " onto " + e.onto + "\n\n")
	for i, r := range e.rows {
		cur := "  "
		if i == e.sel {
			cur = "> "
		}
		action := padRight("["+string(r.action)+"]", 10)
		subj := r.subject
		if r.action == rebaseplan.Reword && r.newMsg != "" {
			first, _ := splitMessage(r.newMsg)
			subj = first + "  (reworded)"
		}
		line := cur + action + " " + shortHash(r.sha) + "  " + subj
		if i == e.sel {
			b.WriteString(selectedRow.Render(truncate(line, w)))
		} else {
			b.WriteString(truncate(line, w))
		}
		b.WriteString("\n")
	}
	if e.reword != nil {
		b.WriteString("\nReword:\n")
		b.WriteString(renderCommitFields(e.reword))
		b.WriteString("\n[tab] switch field  [enter] newline/next  [ctrl+s] set  [esc] cancel")
	} else {
		b.WriteString("\n[p]ick [r]eword [s]quash [d]rop  [ctrl+↑/↓] move  [enter] start  [R]eset  [esc] cancel")
	}
	return b.String()
}
```

Add the small shared render helper to `commit_popup.go` and use it from
`renderCommitPopup` (DRY) and the editor:

```go
// renderCommitFields draws the title/description fields with the focus cursor.
func renderCommitFields(p *commitPopup) string {
	var b strings.Builder
	titleCur, descCur := "  ", "  "
	if p.field == 0 {
		titleCur = "> "
	} else {
		descCur = "> "
	}
	b.WriteString(titleCur + "title:       " + p.title + "\n")
	descLines := strings.Split(p.desc, "\n")
	b.WriteString(descCur + "description: " + descLines[0] + "\n")
	for _, l := range descLines[1:] {
		b.WriteString("             " + l + "\n")
	}
	return b.String()
}
```

(Update `renderCommitPopup` to call `renderCommitFields(p)` for the field block,
keeping its heading + hint — a pure DRY refactor; F2 render tests stay green.)

- [ ] **Step 4: Run the editor tests**

Run: `go test ./internal/tui/ -run TestIrebaseEditor -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/irebase_view.go internal/tui/irebase_view_test.go internal/tui/commit_popup.go
git commit -m "feat(tui): interactive-rebase editor surface (pick/reword/squash/drop, reorder)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 4: third pair-op opens the editor

**Files:**
- Modify: `internal/tui/mark.go` (pairOp gains `open`; add the 3rd Branches op)
- Modify: `internal/tui/pairop_popup.go` (enter calls `open` when set)
- Modify: `internal/tui/op.go` (a cmd to load the range, then push the editor)
- Modify: `internal/tui/footer.go`, `internal/tui/help.go`
- Test: `internal/tui/irebase_view_test.go` (the open hook builds the editor)

- [ ] **Step 1: Add the `open` hook to `pairOp` and the picker**

In `internal/tui/mark.go`, add a field to `pairOp`:

```go
	// open, when non-nil, is used instead of build+startOp: the picker calls it
	// to open a view (e.g. the interactive-rebase editor) for (marked, selected).
	open func(m Model, marked, selected string) (Model, tea.Cmd)
```

Add the third Branches pair-op (after the existing Rebase entry):

```go
		{
			label:   func(marked, selected string) string { return "Interactive rebase " + marked + " onto " + selected },
			enabled: true,
			open: func(m Model, marked, selected string) (Model, tea.Cmd) {
				return m, m.loadIrebaseCmd(marked, selected)
			},
		},
```

In `internal/tui/pairop_popup.go`, in the `enter` case, branch on `open`:

```go
	case "enter":
		op := p.ops[p.sel]
		if !op.enabled {
			m.statusMsg = op.label(p.marked, p.selected) + ": " + op.note
			return m, nil
		}
		marked, selected := p.marked, p.selected
		m.pairPopup = nil
		m.mark = nil
		if op.open != nil {
			return op.open(m, marked, selected)
		}
		return m.startOp(op.build(marked, selected))
```

- [ ] **Step 2: Add the load cmd + msg in `op.go`**

In `internal/tui/op.go`:

```go
// irebaseLoadedMsg carries the range commits for the interactive-rebase editor.
type irebaseLoadedMsg struct {
	branch, onto string
	commits      []model.RangeCommit
	err          error
}

// loadIrebaseCmd fetches branch's commits since onto (oldest-first) off the UI
// thread; the resulting msg opens the editor.
func (m Model) loadIrebaseCmd(branch, onto string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		cs, err := svc.CommitRange(context.Background(), onto, branch)
		return irebaseLoadedMsg{branch: branch, onto: onto, commits: cs, err: err}
	}
}
```

In `internal/tui/model.go`'s message `switch`, handle it (push the editor; empty
range is a no-op with a statusMsg). `os.Executable()` gives `GGBin`:

```go
	case irebaseLoadedMsg:
		if msg.err != nil {
			m.statusMsg = "interactive rebase: " + msg.err.Error()
			return m, nil
		}
		if len(msg.commits) == 0 {
			m.statusMsg = "interactive rebase: no commits in range"
			return m, nil
		}
		ggBin, err := os.Executable()
		if err != nil {
			m.statusMsg = "interactive rebase: " + err.Error()
			return m, nil
		}
		m = m.pushSurface(newIrebaseEditor(msg.branch, msg.onto, msg.commits, ggBin))
		return m, nil
```

Add `"os"` to `model.go`'s imports if missing, and ensure `op.go` imports
`internal/model`.

- [ ] **Step 3: Footer + help**

The editor is a view-stack surface that owns the keyboard; the footer is
overridden while it's open (like the files view). In `internal/tui/footer.go`'s
`footerLine`, add a branch (after the `filesView` one):

```go
	if _, ok := m.stackTop().(*irebaseEditor); ok {
		return "irebase: [p]ick [r]eword [s]quash [d]rop  [ctrl+↑/↓] move  [enter] start  [R]eset  [esc] cancel"
	}
```

In `internal/tui/help.go`, add a section:

```go
		h("Interactive rebase editor"),
		r("p / r / s / d", "set the row action: pick / reword / squash / drop"),
		r("ctrl+↑/↓", "move the focused commit up / down"),
		r("↑/k ↓/j", "move the cursor"),
		r("enter", "start the rebase with the current plan"),
		r("R", "reset every row to pick, original order"),
		r("esc", "cancel (close without rebasing)"),
```

- [ ] **Step 4: Test the open hook builds the editor**

Append to `internal/tui/irebase_view_test.go`:

```go
func TestIrebaseLoadedMsgPushesEditor(t *testing.T) {
	m := Model{width: 80, height: 24}
	updated, _ := m.Update(irebaseLoadedMsg{branch: "work", onto: "main", commits: edRows()})
	m = updated.(Model)
	if _, ok := m.stackTop().(*irebaseEditor); !ok {
		t.Fatal("irebaseLoadedMsg should push the editor surface")
	}
}

func TestIrebaseLoadedEmptyRangeNoOp(t *testing.T) {
	m := Model{width: 80, height: 24}
	updated, _ := m.Update(irebaseLoadedMsg{branch: "work", onto: "main", commits: nil})
	m = updated.(Model)
	if m.stackTop() != nil {
		t.Fatal("empty range must not push a surface")
	}
}
```

- [ ] **Step 5: Run the TUI package**

Run: `go test ./internal/tui/`
Expected: PASS, including `TestHelpFooterCoverage` (the editor keys are
documented in help; the footer override is exempt like the files view — confirm
the drift guard passes, and adjust the help rows if it flags a missing key).

- [ ] **Step 6: Commit**

```bash
git add internal/tui/mark.go internal/tui/pairop_popup.go internal/tui/op.go internal/tui/model.go internal/tui/footer.go internal/tui/help.go internal/tui/irebase_view_test.go
git commit -m "feat(tui): pair-op opens the interactive-rebase editor

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 5: docs

**Files:**
- Modify: `README.md`, `CHANGELOG.md`

- [ ] **Step 1: README**

In the TUI key table, document the editor under the `m` (pair-op) row or add a
row: marking two branches and choosing **Interactive rebase** opens the editor
(`p/r/s/d` set the action, `ctrl+↑/↓` reorder, `enter` start, `R` reset, `esc`
cancel).

- [ ] **Step 2: CHANGELOG**

Under `## [Unreleased]` → `### Added`, append to the interactive-rebase entry (or
add a sub-bullet):

```markdown
- TUI: mark two branches (`m`, `m`) on the Branches panel and choose
  **Interactive rebase {marked} onto {selected}** to open a GitKraken-style
  editor — per-row pick/reword/squash/drop, reorder with `ctrl+↑/↓`, `enter`
  starts, `R` resets, `esc` cancels. Reword opens an inline title+description
  editor; squash composes the combined message; the working tree is preserved.
```

- [ ] **Step 3: Commit**

```bash
git add README.md CHANGELOG.md
git commit -m "docs: interactive-rebase TUI editor in README/CHANGELOG

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Final verification (after all tasks)

- [ ] `./test.sh race` — vet+gofmt clean, all unit + e2e green.
- [ ] Manual smoke (optional): `go build ./cmd/gg && ./gg`, mark two branches, open the editor, reword/drop, start.
- [ ] `superpowers:finishing-a-development-branch`.
- [ ] **After merge, RE-RUN `./test.sh race` on merged `main`** — drift discipline.

---

## Self-Review

**1. Spec coverage (Slice 4, TUI half):**
- Pair-op entry "Interactive rebase {marked} onto {selected}", range `selected..marked` → Task 1 (`CommitRange`) + Task 4 (3rd op + `open` hook). ✓
- View-stack surface, newest-first display, plan reversed to oldest-first → Task 3 (`newIrebaseEditor`/`plan`). ✓
- `p/r/s/d`, `ctrl+↑/↓`, `enter`/`esc`/`R` → Task 3 `update`. ✓
- Squash refused on the oldest row → Task 3. ✓
- Reword reuses F2's message editing (via `applyEditKey`), hosted inside the surface because the view-stack short-circuits Model popups → Tasks 2–3. ✓
- Runs `engine.InteractiveRebase` with `GGBin = os.Executable()` via `startOp` → Tasks 3–4. ✓
- Footer + help (drift guard) → Task 4. ✓

**2. Placeholder scan:** complete code throughout; the two conditional notes (reuse existing `key`/`pressRune` helpers if they collide; adjust help rows if the drift guard flags one) are concrete, not vague.

**3. Type consistency:** `rebaseplan.Plan`/`Entry{Sha,Action,Orig,NewMsg}` match Slice 2; `engine.InteractiveRebase{Branch,Onto,Plan,GGBin}` matches Slice 3; `model.RangeCommit{Hash,Subject,Message}` consistent across verb (T1), `GitOps`, `CommitRange`, and the editor (T3). `(*commitPopup).applyEditKey`/`message`/`splitMessage`/`renderCommitFields` consistent between F2 (T2) and the editor (T3). `irebaseLoadedMsg`/`loadIrebaseCmd` consistent between op.go (T4 step 2) and model.go (T4 step 2). `pairOp.open` defined in T4 and used by the picker (T4) and the 3rd op (T4).

**This completes the interactive-rebase feature** (slices 1–4). Deferred items remain as recorded in the spec's "Out of scope" (combined-message-squash editing, merge commits, mid-conflict UI, pushed-history warning, Windows exec-quoting verification).

# Name a commit on shelf/bookmark — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a user give a human name to a commit when shelving it ("Shelf this commit" / `gg shelf commit`) or bookmarking it ("Bookmark this commit"), so it's distinguishable later in the `G`/`g` switchers.

**Architecture:** Add a `Label` field to `ShelfEntry` (bookmarks already have one) threaded through `PutCommit`/`ShelfAddCommit` and a `--name` CLI flag; in the TUI, the four "Shelf/Bookmark this commit" rows open a shared single-line **name popup** (pre-filled with the commit subject, `ctrl+s` inserts the short sha) that dispatches the create command with the entered label. Name captured at creation only.

**Tech Stack:** Go 1.26, Bubble Tea (TUI), `go-toml/v2` (shelf index), existing `textfield`/popup-layer infrastructure.

## Global Constraints

- Module `github.com/homeend/gigagit`, Go 1.26.
- `internal/tui` and `internal/cli` NEVER import `internal/git` — git only via `m.svc` (domain); archtest-guarded.
- Name is captured at **creation time only** (no rename-later).
- The derived shelf-entry ID stays unchanged (`commit-<shortsha>-<blobsha8>`); `Label` is display-only.
- `ctrl+s` inserts `sha[:7]` (full sha when < 7 chars) at the cursor.
- Empty name → bookmark falls back to the commit **subject**; shelf leaves `Label=""` (switcher then shows `Origin.Display()`).
- Follow TDD; gofmt is a hard gate; `./test.sh` (vet+gofmt → unit → e2e) must pass before done.

---

### Task 1: Shelf label backend — `ShelfEntry.Label` through `PutCommit`/`ShelfAddCommit` + `gg shelf commit --name`

**Files:**
- Modify: `internal/model/model.go` (ShelfEntry struct, ~257-265)
- Modify: `internal/shelf/store.go` (Store.PutCommit signature, line 33)
- Modify: `internal/shelf/file_store.go` (PutCommit impl, ~172-197)
- Modify: `internal/domain/shelf.go` (ShelfAddCommit, ~55-77)
- Modify: `internal/tui/shelf.go` (shelfAddCommitCmd caller, line 75 — pass `""` for now)
- Modify: `internal/cli/shelf.go` (shelfCommit — add `--name`, line ~165-179)
- Test: `internal/shelf/file_store_test.go`, `internal/cli/shelf_test.go`

**Interfaces:**
- Produces:
  - `model.ShelfEntry.Label string`
  - `shelf.Store.PutCommit(bucket string, addr model.FileAddress, tar []byte, label string) (model.ShelfEntry, error)`
  - `domain.(*Service).ShelfAddCommit(ctx context.Context, sha, label string) (model.ShelfEntry, error)`

- [ ] **Step 1: Write the failing test (store persists Label)**

Append to `internal/shelf/file_store_test.go`:

```go
func TestPutCommitPersistsLabel(t *testing.T) {
	fs := NewFileStore(t.TempDir())
	addr := model.FileAddress{State: model.StateCommitted, Commit: "a1b2c3d4e5f6", Path: ""}
	e, err := fs.PutCommit("", addr, []byte("tarbytes"), "my fix")
	if err != nil {
		t.Fatalf("PutCommit: %v", err)
	}
	if e.Label != "my fix" {
		t.Fatalf("Label = %q, want %q", e.Label, "my fix")
	}
	// Survives the TOML index round-trip (reopen the store, list).
	fs2 := NewFileStore(fsRoot(fs))
	page, err := fs2.List("", 0, 10)
	if err != nil || len(page) != 1 {
		t.Fatalf("List: %v (n=%d)", err, len(page))
	}
	if page[0].Label != "my fix" {
		t.Fatalf("reloaded Label = %q, want %q", page[0].Label, "my fix")
	}
}
```

Add this helper near the top of the test file if no equivalent exists (the store's root is unexported):

```go
// fsRoot exposes a FileStore's root for reopen-round-trip tests.
func fsRoot(fs *FileStore) string { return fs.root }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/shelf/ -run TestPutCommitPersistsLabel -v`
Expected: FAIL — `too many arguments in call to fs.PutCommit` / `e.Label undefined`.

- [ ] **Step 3: Implement the label field + plumbing**

In `internal/model/model.go`, add `Label` to `ShelfEntry` (after `Origin`):

```go
type ShelfEntry struct {
	ID      string
	Bucket  string
	Kind    ShelfKind
	Origin  FileAddress
	Label   string // human name (commit entries); "" = none. Display-only, not in ID.
	SHA     string
	Size    int64
	Created time.Time
}
```

In `internal/shelf/store.go`, update the interface method:

```go
	PutCommit(bucket string, addr model.FileAddress, tar []byte, label string) (model.ShelfEntry, error)
```

In `internal/shelf/file_store.go`, update `PutCommit` to take and set `label`:

```go
func (fs *FileStore) PutCommit(bucket string, addr model.FileAddress, tar []byte, label string) (model.ShelfEntry, error) {
	if len(tar) > MaxCommitArchiveBytes {
		return model.ShelfEntry{}, ErrTooLarge
	}
	bucket = normalizeBucket(bucket)
	sha, err := fs.writeBlob(tar)
	if err != nil {
		return model.ShelfEntry{}, err
	}
	short := addr.Commit
	if len(short) > 7 {
		short = short[:7]
	}
	return fs.putEntry(model.ShelfEntry{
		ID:      fmt.Sprintf("commit-%s-%s", short, sha[:8]),
		Bucket:  bucket,
		Kind:    model.ShelfKindCommit,
		Origin:  addr,
		Label:   label,
		SHA:     sha,
		Size:    int64(len(tar)),
		Created: time.Now(),
	})
}
```

In `internal/domain/shelf.go`, thread `label` through `ShelfAddCommit`:

```go
func (s *Service) ShelfAddCommit(ctx context.Context, sha, label string) (model.ShelfEntry, error) {
	st := s.shelfStore(ctx)
	if st == nil {
		return model.ShelfEntry{}, ErrShelfDisabled
	}
	paths, err := s.commitChangedPaths(ctx, sha)
	if err != nil {
		return model.ShelfEntry{}, err
	}
	if len(paths) == 0 {
		return model.ShelfEntry{}, fmt.Errorf("shelf: commit %s changes no files", sha)
	}
	tar, err := s.archiveFiles(ctx, sha, paths)
	if err != nil {
		return model.ShelfEntry{}, err
	}
	addr := model.FileAddress{State: model.StateCommitted, Commit: sha, Path: ""}
	return st.PutCommit("", addr, tar, label)
}
```

In `internal/tui/shelf.go`, keep `shelfAddCommitCmd(sha string)` compiling by passing an empty label for now (Task 2 changes its signature):

```go
		e, err := svc.ShelfAddCommit(context.Background(), sha, "")
```

- [ ] **Step 4: Run the store test**

Run: `go test ./internal/shelf/ -run TestPutCommitPersistsLabel -v`
Expected: PASS. Then `go build ./...` — confirm the whole tree still compiles.

- [ ] **Step 5: Write the failing CLI test (`--name`)**

Append to `internal/cli/shelf_test.go` (mirror the existing `TestShelfCommitThenExport` harness — real repo, in-process CLI):

```go
func TestShelfCommitName(t *testing.T) {
	env := newShelfCLIEnv(t) // reuse the harness TestShelfCommitThenExport uses
	env.writeCommit("a.txt", "alpha\n", "add a")
	sha := env.head()
	if code := env.run("shelf", "commit", "--name", "my fix", sha); code != 0 {
		t.Fatalf("shelf commit --name exit=%d", code)
	}
	e := env.newestShelfEntry(t) // list newest-first, take [0]
	if e.Label != "my fix" {
		t.Fatalf("Label = %q, want %q", e.Label, "my fix")
	}
}
```

> NOTE: adapt `newShelfCLIEnv`/`writeCommit`/`head`/`run`/`newestShelfEntry` to the real CLI test helpers in `internal/cli/shelf_test.go`; keep the flow and the `Label` assertion.

- [ ] **Step 6: Run it (fails: unknown flag)**

Run: `go test ./internal/cli/ -run TestShelfCommitName -v`
Expected: FAIL — `flag provided but not defined: -name` (or Label empty).

- [ ] **Step 7: Add `--name` to `shelfCommit`**

In `internal/cli/shelf.go`, update `shelfCommit`:

```go
func shelfCommit(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("shelf commit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	name := fs.String("name", "", "human name for the shelved commit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: gg shelf commit [--name <name>] <sha>")
		return 2
	}
	e, err := svc.ShelfAddCommit(context.Background(), fs.Arg(0), *name)
	if err != nil {
		fmt.Fprintf(stderr, "shelf commit: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "shelved commit as %s\n", e.ID)
	return 0
}
```

- [ ] **Step 8: Run both + build**

Run: `go test ./internal/shelf/ ./internal/cli/ -run 'PutCommitPersistsLabel|ShelfCommitName' -v` → PASS.
Run: `go build ./...` and `gofmt -l internal/model internal/shelf internal/domain internal/cli internal/tui` (empty).

- [ ] **Step 9: Commit**

```bash
git add internal/model/model.go internal/shelf/ internal/domain/shelf.go internal/cli/shelf.go internal/tui/shelf.go
git commit -m "shelf: thread a human Label through PutCommit/ShelfAddCommit + gg shelf commit --name"
```

---

### Task 2: TUI name popup + rewire the four commit rows

**Files:**
- Create: `internal/tui/commit_name_popup.go`
- Modify: `internal/tui/shelf.go` (`shelfAddCommitCmd` signature + `commitShelfRow`/`reflogShelfRow` run handlers)
- Modify: `internal/tui/bookmark.go` (`commitBookmark` signature + `commitBookmarkRow`/`reflogBookmarkRow` run handlers)
- Modify: `internal/tui/help.go` (mention the naming prompt on the Shelf/Bookmark-this-commit line)
- Test: `internal/tui/commit_name_popup_test.go`

**Interfaces:**
- Consumes: `model.ShelfEntry.Label` (Task 1); existing `textfield` (`newTextField(s)`, `Value()`, `HandleEditKey(msg)`, unexported `insert([]rune)`); `m.pushLayer`/`m.popLayer`; `m.bookmarkAddCmd(b)`; `shelfAddedMsg`/`bookmarkAddedMsg` handlers; the `layer` interface (`update(m Model, msg tea.KeyMsg)(Model, tea.Cmd)` + `render(m Model, below string) string`).
- Produces:
  - `type commitNamePopup struct { commit model.Commit; forShelf bool; name textfield }` implementing `layer`.
  - `shelfAddCommitCmd(sha, label string) tea.Cmd`
  - `commitBookmark(c model.Commit, label string) model.Bookmark`

- [ ] **Step 1: Write the failing test**

Create `internal/tui/commit_name_popup_test.go` (mirror the helpers used in `internal/tui/temp_export_test.go` — grep it for `keyMsg`, `layerOf`, and the minimal Model constructor):

```go
package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/homeend/gigagit/internal/model"
)

func TestCommitNamePopupEnterDispatches(t *testing.T) {
	m := loadedModelLinearCommits(t, 1)
	p := &commitNamePopup{commit: model.Commit{Hash: "a1b2c3d4e5", Subject: "subj"}, forShelf: true, name: newTextField("subj")}
	_, cmd := p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter should dispatch a create command")
	}
}

func TestCommitNamePopupCtrlSInsertsShortSha(t *testing.T) {
	m := loadedModelLinearCommits(t, 1)
	p := &commitNamePopup{commit: model.Commit{Hash: "a1b2c3d4e5", Subject: ""}, forShelf: false, name: newTextField("")}
	p.update(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if got := p.name.Value(); got != "a1b2c3d" {
		t.Fatalf("after ctrl+s name = %q, want the 7-char short sha", got)
	}
}

func TestCommitNamePopupEscNoDispatch(t *testing.T) {
	m := loadedModelLinearCommits(t, 1)
	p := &commitNamePopup{commit: model.Commit{Hash: "a1b2c3d4e5"}, forShelf: true, name: newTextField("x")}
	_, cmd := p.update(m, tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Fatal("esc should not dispatch a create command")
	}
}

func TestCommitBookmarkLabelFallsBackToSubject(t *testing.T) {
	if b := commitBookmark(model.Commit{Hash: "h", Subject: "the subject"}, ""); b.Label != "the subject" {
		t.Fatalf("empty label should fall back to subject, got %q", b.Label)
	}
	if b := commitBookmark(model.Commit{Hash: "h", Subject: "the subject"}, "custom"); b.Label != "custom" {
		t.Fatalf("label = %q, want custom", b.Label)
	}
}
```

> NOTE: if `loadedModelLinearCommits` needs a non-zero arg or a different minimal-Model helper is the norm in popup tests, adapt to the real one (grep `internal/tui/*_test.go`). Keep the four assertions.

- [ ] **Step 2: Run it (fails)**

Run: `go test ./internal/tui/ -run 'TestCommitNamePopup|TestCommitBookmarkLabel' -v`
Expected: FAIL — `commitNamePopup` / `commitBookmark` arity undefined.

- [ ] **Step 3: Create the popup**

Create `internal/tui/commit_name_popup.go`:

```go
package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/model"
)

// commitNamePopup collects a human name when shelving or bookmarking a commit.
// The field is pre-filled with the commit subject; ctrl+s inserts the short sha
// at the cursor; enter creates the shelf entry / bookmark with the name; esc
// cancels. forShelf routes to the shelf vs bookmark create command.
type commitNamePopup struct {
	commit   model.Commit
	forShelf bool
	name     textfield
}

func (p *commitNamePopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyCtrlS:
		sha := p.commit.Hash
		if len(sha) > 7 {
			sha = sha[:7]
		}
		p.name.insert([]rune(sha))
		return m, nil
	case tea.KeyEnter:
		label := strings.TrimSpace(p.name.Value())
		c, forShelf := p.commit, p.forShelf
		m = m.popLayer()
		if forShelf {
			return m, m.shelfAddCommitCmd(c.Hash, label)
		}
		return m, m.bookmarkAddCmd(commitBookmark(c, label))
	default:
		p.name.HandleEditKey(msg)
	}
	return m, nil
}

func (p *commitNamePopup) render(m Model, below string) string {
	title := "Bookmark this commit"
	verb := "bookmark"
	if p.forShelf {
		title, verb = "Shelf this commit", "shelf"
	}
	w, _ := m.overlayDims()
	var b strings.Builder
	b.WriteString(title + "\n\n")
	b.WriteString(viewField("name: ", p.name, true, popupContentWidth(w)) + "\n\n")
	b.WriteString("[ctrl+s] insert sha   [enter] " + verb + "   [esc] cancel")
	return popupBoxLikeSibling(m, b.String()) // see NOTE below
}
```

> IMPORTANT (render compositing): the `layer.render` return value must be the
> composited overlay string, not a bare body. Do NOT invent the wrappers —
> mirror an existing single-field popup's `render` EXACTLY, changing only the
> title/field/hint. Use `internal/tui/temp_export.go`'s `tempExportPopup.render`
> as the template (it was the most recently reviewed one): copy its
> `overlayDims`/`popupContentWidth`/box/center/clip wrapper calls verbatim and
> drop in the title/`viewField("name: ", …)`/hint above. Replace the
> `popupBoxLikeSibling(...)` placeholder with those real calls.

- [ ] **Step 4: Change `shelfAddCommitCmd` to take a label + rewire the shelf rows**

In `internal/tui/shelf.go`, change the command signature and both rows:

```go
func (m Model) shelfAddCommitCmd(sha, label string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		e, err := svc.ShelfAddCommit(context.Background(), sha, label)
		return shelfAddedMsg{entry: e, err: err}
	}
}
```

`commitShelfRow` run handler (replace the `sha := …` + run body):

```go
	c := m.commits[bi]
	return actionRow{
		id:    "commit-shelf",
		label: "Shelf this commit",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.pushLayer(&commitNamePopup{commit: c, forShelf: true, name: newTextField(c.Subject)}), nil
		},
	}, true
```

`reflogShelfRow` run handler:

```go
	e := m.reflog[bi]
	c := model.Commit{Hash: e.Hash, Subject: e.Subject}
	return actionRow{
		id:    "reflog-shelf",
		label: "Shelf this commit",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.pushLayer(&commitNamePopup{commit: c, forShelf: true, name: newTextField(c.Subject)}), nil
		},
	}, true
```

(Ensure `internal/tui/shelf.go` imports `github.com/homeend/gigagit/internal/model` — it already uses `model` elsewhere.)

- [ ] **Step 5: Change `commitBookmark` to take a label + rewire the bookmark rows**

In `internal/tui/bookmark.go`:

```go
// commitBookmark builds the path-less bookmark for commit c with a human label
// (falls back to the commit subject when label is empty).
func commitBookmark(c model.Commit, label string) model.Bookmark {
	if label == "" {
		label = c.Subject
	}
	return model.Bookmark{
		State:  model.StateCommitted,
		Commit: c.Hash,
		Branch: firstLocalRef(c),
		Path:   "",
		Label:  label,
	}
}
```

`commitBookmarkRow` run handler (replace the `b := commitBookmark(...)` + run body):

```go
	c := m.commits[bi]
	return actionRow{
		id:    "commit-bookmark",
		label: "Bookmark this commit",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.pushLayer(&commitNamePopup{commit: c, forShelf: false, name: newTextField(c.Subject)}), nil
		},
	}, true
```

`reflogBookmarkRow` run handler:

```go
	e := m.reflog[bi]
	c := model.Commit{Hash: e.Hash, Subject: e.Subject}
	return actionRow{
		id:    "reflog-bookmark",
		label: "Bookmark this commit",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.pushLayer(&commitNamePopup{commit: c, forShelf: false, name: newTextField(c.Subject)}), nil
		},
	}, true
```

- [ ] **Step 6: Help text**

In `internal/tui/help.go`, the `.`-menu line that already lists "Shelf this commit / Bookmark this commit": append " (name it)" so it reads e.g. `Bookmark this commit / Shelf this commit (name it)`. Keep it one line.

- [ ] **Step 7: Run tests + build**

Run: `go test ./internal/tui/ -run 'TestCommitNamePopup|TestCommitBookmarkLabel' -v` → PASS.
Run: `go test ./internal/tui/` (whole package), `go build ./...`, `gofmt -l internal/tui` (empty).

- [ ] **Step 8: Commit**

```bash
git add internal/tui/commit_name_popup.go internal/tui/commit_name_popup_test.go internal/tui/shelf.go internal/tui/bookmark.go internal/tui/help.go
git commit -m "tui: name popup for Shelf/Bookmark this commit (ctrl+s inserts short sha)"
```

---

### Task 3: Shelf switcher shows the label

**Files:**
- Modify: `internal/tui/shelf_popup.go` (row text build, ~line 42)
- Test: `internal/tui/shelf_popup_test.go`

**Interfaces:**
- Consumes: `model.ShelfEntry.Label` (Task 1), `FileAddress.Display()`.
- Produces: `shelfEntryDisplay(e model.ShelfEntry) string`.

- [ ] **Step 1: Write the failing test**

Append to `internal/tui/shelf_popup_test.go` (create if absent):

```go
package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestShelfEntryDisplayLabel(t *testing.T) {
	labeled := model.ShelfEntry{
		Kind:   model.ShelfKindCommit,
		Origin: model.FileAddress{State: model.StateCommitted, Commit: "a1b2c3d4e5"},
		Label:  "my fix",
	}
	got := shelfEntryDisplay(labeled)
	if !strings.Contains(got, "my fix") || !strings.HasSuffix(got, "— my fix") {
		t.Fatalf("labeled display = %q, want it to end with '— my fix'", got)
	}
	plain := model.ShelfEntry{
		Kind:   model.ShelfKindCommit,
		Origin: model.FileAddress{State: model.StateCommitted, Commit: "a1b2c3d4e5"},
	}
	if shelfEntryDisplay(plain) != plain.Origin.Display() {
		t.Fatalf("unlabeled display should equal Origin.Display()")
	}
}
```

- [ ] **Step 2: Run it (fails)**

Run: `go test ./internal/tui/ -run TestShelfEntryDisplayLabel -v`
Expected: FAIL — `shelfEntryDisplay` undefined.

- [ ] **Step 3: Add the helper + use it**

In `internal/tui/shelf_popup.go`, add:

```go
// shelfEntryDisplay is the switcher row text: the address, plus " — <label>"
// when the entry carries a human name (mirrors bookmarkDisplay).
func shelfEntryDisplay(e model.ShelfEntry) string {
	s := e.Origin.Display()
	if e.Label != "" {
		s += " — " + e.Label
	}
	return s
}
```

Change the row build (currently `p.rows = append(p.rows, e.Origin.Display())`, ~line 42) to:

```go
		p.rows = append(p.rows, shelfEntryDisplay(e))
```

- [ ] **Step 4: Run it + build**

Run: `go test ./internal/tui/ -run TestShelfEntryDisplayLabel -v` → PASS.
Run: `go test ./internal/tui/`, `go build ./...`, `gofmt -l internal/tui` (empty).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/shelf_popup.go internal/tui/shelf_popup_test.go
git commit -m "tui: shelf switcher shows a labeled commit entry's name"
```

---

### Task 4: Docs + full test sweep

**Files:** `CHANGELOG.md`, `README.md`, `CLAUDE.md`, `internal/agentskill/using-gg.md` + `internal/agentskill` version const.

- [ ] **Step 1: CHANGELOG.md** — add an entry: naming a commit when shelving/bookmarking it (TUI name popup, `ctrl+s` inserts short sha; `gg shelf commit --name`; shelf switcher shows the name). Match the file's format.

- [ ] **Step 2: README.md** — document the name prompt on "Shelf/Bookmark this commit" and `gg shelf commit [--name <name>] <sha>`. Match the README's structure/voice.

- [ ] **Step 3: CLAUDE.md** — shelf row: note `ShelfEntry.Label` + `PutCommit(...,label)` + `ShelfAddCommit(sha,label)`; tui row: the `commitNamePopup` naming prompt. Keep the terse table style.

- [ ] **Step 4: agentskill** — add `--name` to `gg shelf commit` in `internal/agentskill/using-gg.md`; bump the `agentskill.Version` const (grep it). Do NOT run `gg init --update` (machine-local; leave to the user). If a dogfood sync test (`TestDogfoodSkillCopyInSync`) fails after the version bump, regenerate the repo-tracked `.claude/skills/using-gg/SKILL.md` to be byte-identical to the embedded doc (build a throwaway dumper of `agentskill.SkillFile()`, write it, delete the dumper) and commit it too.

- [ ] **Step 5: Full sweep** — `./test.sh` (vet+gofmt → unit → e2e). Must be fully green; paste the summary into the report. Then `go build -o ./gg ./cmd/gg`.

- [ ] **Step 6: Commit**

```bash
git add CHANGELOG.md README.md CLAUDE.md internal/agentskill/ .claude/skills/using-gg/SKILL.md
git commit -m "docs: name a commit on shelf/bookmark (CHANGELOG/README/CLAUDE/agentskill)"
```

---

## Self-Review

**Spec coverage:**
- TUI name popup pre-filled with subject, ctrl+s inserts short sha, enter creates, esc cancels → Task 2. ✓
- Both Commits + Reflog, both Shelf + Bookmark → Task 2 rewires all four rows. ✓
- Bookmark empty→subject fallback → `commitBookmark` (Task 2). ✓
- Shelf `Label` field + PutCommit + ShelfAddCommit + CLI `--name` + empty→no label → Task 1. ✓
- Shelf switcher shows the label → Task 3. ✓
- Reuse `textfield.insert` (no new method) → Task 2 (matches spec intent; simpler than a new `InsertString`). ✓
- Creation-only, ID unchanged, docs → Global Constraints + Task 4. ✓

**Type consistency:** `ShelfEntry.Label`, `PutCommit(...,label string)`, `ShelfAddCommit(ctx,sha,label string)`, `shelfAddCommitCmd(sha,label string)`, `commitBookmark(c model.Commit,label string)`, `commitNamePopup{commit,forShelf,name}`, `shelfEntryDisplay(e)` — consistent across tasks. ✓

**Adapt-to-real flags:** Task 1's CLI test helpers, Task 2's popup-test helpers, and the popup `render` compositing must be matched to the real neighboring code (called out inline); the public signatures and assertions are the fixed contract.

# ctrl+\ "Overviews & notes" tree — plan 1 of 2: the TUI

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task, inline (this repo forbids implementer subagents; one read-only review subagent at the end). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The `ctrl+\` popup's Open files tab becomes the **Overviews & notes** tree: overviews on top (collapsed) with the files they use underneath, then a **Notes** group (noted files no overview names), then an **Opened** group (everything else); `enter` opens the row's own thing, `→`/`←` expand and collapse, the expand state lives while gg runs.

**Architecture:** A pure builder (`internal/tui/doc_tree.go`) turns the worktree's open-files list, the agent-docs store and the remembered expand state into a flat slice of `docNode`s (kind, depth, document, the child's overview/path/anchor, group). The popup's Files tab renders those nodes instead of `openFileRowText` rows and dispatches its keys on the node kind. Expand state is a map on `openFilesReg` (already a pointer on the Model, per worktree). A child's `enter` selects the overview's first anchor in that file and opens the overview (`showOverview` → the split steers to the selected anchor).

**Tech Stack:** Go 1.26, Bubble Tea, `internal/tui` (sessions_popup.go, open_files.go, split_overview.go), `internal/i18n/lang/*.toml`, the e2e TUI harness (`-update`).

**Spec:** `docs/superpowers/specs/2026-10-19-documents-tree-design.md` (mock: `2026-10-19-documents-tree-mock.txt`).

## Global Constraints

- What is in the open-files list, when documents join or leave it, close (`x`) and delete (`D`) semantics, `gg session files` and the `●`/`○` marks do not change (spec §7).
- Overviews keep the list's existing order (most recently shown first); an overview's children are in the order its anchors first name them; a path appears under every overview naming it; a document shown as an overview's child is not repeated under Notes or Opened.
- Every user-visible string goes through `i18n.T` with a literal key present in all four bundles (`internal/i18n/lang/{ja,ko,ru,zh}.toml`), translated.
- Paths are cut in the middle (`winRow.elide`), never at the end.
- `./test.sh race` must print `all green` before merge; `gg add <files>` + `git commit -F <msgfile>`; never `git add -A`. Worktree `/work/gigagit/.claude/worktrees/documents-tree`, branch `feat/documents-tree`.

## Review Focus

1. A note anchor whose note is gone, an anchor with an empty path, a stored overview at a commit: children never panic, never list "", and a stored overview's child shows `@ <sha7>` — Task 1.
2. Filtering then clearing the filter: expand state set before the filter is what remains after it (the filter's own expansion is not saved) — Task 1.
3. Cursor stability: after `→`/`←`/`x`/`D` and after a store change refreshes the list, the cursor stays on the same node (or its parent when the node left) — Task 2.
4. `enter` on a child of an overview already shown as the split re-steers the split to that file — Task 3.
5. The popup opening on this tab when it is the only one with rows, and quit mode never showing it — Task 2 (existing tests keep passing with the new label).

---

### Task 1: the pure tree builder

**Files:** Create `internal/tui/doc_tree.go`, `internal/tui/doc_tree_test.go`; modify `internal/tui/open_files.go` (expand-state map on `openFilesReg`).

**Interfaces — Produces:**

```go
type docNodeKind int
const (
	nodeOverview docNodeKind = iota // an overview document
	nodeChild                       // a file an overview uses
	nodeGroup                       // the Notes / Opened group row
	nodeGroupFile                   // a document under a group
)
const groupNotes, groupOpened = "notes", "opened"

type docNode struct {
	kind     docNodeKind
	doc      *openFile // overview / group file / an open child; nil: group row, child not open
	ov       *openFile // a child's overview
	path     string    // a child's path
	anchor   int       // a child's first anchor index naming path
	rev      string    // a child of a stored overview: the commit its anchors open at
	group    string    // a group row's / group file's group
	count    int       // a group row's document count
	expanded bool      // an overview's / a group row's
}
func (n docNode) id() string          // stable node identity (cursor keeping)
func (n docNode) expandKey() string   // "doc:<key>" / "group:<name>"
func (m Model) docTree(query string) []docNode
func (r *openFilesReg) expanded(wt, key string) bool
func (r *openFilesReg) setExpanded(wt, key string, on bool)
```

- [ ] **Step 1: failing tests** — `doc_tree_test.go` builds a model with `attachedDocsModel(t)`: a session overview naming `a.txt:1`, `b.txt:2`, `note:<n on c.txt>`, `a.txt:3` (dup), `note:gone`; a noted `d.txt` named by no overview; an opened commit version of `e.txt` (`newOpenFile(fileSource{kind: srcCommit, rev: "1a2b3c4d…"}, "e.txt")`). Assert:
  - collapsed: kinds `[overview, group(notes, count 1), group(opened, count 1)]`, no child rows;
  - `setExpanded(… "doc:"+ov.key(), true)` → children `a.txt, b.txt, c.txt` in that order, each `nodeChild` with `anchor` 0, 1, 2; `c.txt`'s doc is the listed noted doc; the gone note adds nothing;
  - `c.txt` (noted, named) is not under Notes; `d.txt` is;
  - a second overview naming `a.txt` lists it under both;
  - a stored overview (`srcCommitOverview` doc with `ov.tip = "1a2b3c4d…"`, anchor `x.go:1`) child has `rev` set;
  - `docTree("b.tx")`: overview shown expanded with ONLY `b.txt`; the registry's state for it stays collapsed; `docTree("e.tx")`: the Opened group expanded with `e.txt` only.
- [ ] **Step 2: run** `go test ./internal/tui -run TestDocTree` — FAIL (undefined).
- [ ] **Step 3: implement** `doc_tree.go`: `children(ov)` walks `ov.ov.anchors`, a note anchor resolves through `m.docs.FindNoteIn(ov.root, id)` (session overviews only), dedupes by path keeping the first index; the child's document is `m.openFiles.find(wt, docKey(src, path))` with `src` = worktree, or `{srcCommit, tip}` for a stored overview with a tip. Claimed = the keys of every child's document. Notes = worktree docs with `hasNotes()` not claimed; Opened = every other non-overview doc not claimed. Filter: lower-case `strings.Contains` on the row text (`openFileRowText` / the child's path / the group label); a matching overview shows all its children. `openFilesReg.open map[string]bool` keyed `KeyOf(wt) + "\x00" + key`.
- [ ] **Step 4: run** — PASS.
- [ ] **Step 5: commit** `tui: the documents tree builder (overviews, their files, Notes, Opened)`.

### Task 2: the popup renders the tree and its keys

**Files:** Modify `internal/tui/sessions_popup.go`; test `internal/tui/doc_tree_popup_test.go`; update `open_files_test.go`, `task_tab_test.go`, `session_docs_test.go` expectations of the label.

**Interfaces — Consumes:** Task 1. **Produces:** `p.nodes []docNode` parallel to `p.rows` on the Files tab; `docTreeRowText(m Model, n docNode, now time.Time) string`.

- [ ] **Step 1: failing tests** — on the Task 1 model, open the popup on the Files tab: rows read `▸ ○ <overview row>`, `▸ Notes  1 file`, `▸ Opened  1 file`; `right` on the overview → its children drawn indented `    ○ a.txt …`; cursor on `b.txt`, `left` → cursor on the overview, collapsed; `right` on Notes then `down` → on `d.txt`, `left` → back on Notes collapsed; `enter` on the overview → the overview comes up (top layer is the split), `enter` on a group toggles it; `x` on a child that is open closes that document; the tab label reads `Overviews & notes N`; a store change (`onAgentDocsChanged`) keeps the cursor on the same node.
- [ ] **Step 2: run** — FAIL.
- [ ] **Step 3: implement** — `refresh` builds `p.nodes = m.docTree(p.query)` and `p.rows` from `docTreeRowText` (overview: `▸ `/`▾ ` + `openFileRowText`; child: four-space indent + the document's row or `○ <path>  @ <sha7>` / `○ <path>  K notes  working tree`; group: arrow + `Notes`/`Opened` + `N file(s)`; group file: indent + `openFileRowText`); `selectable` is every node on this tab; `currentFile` = `n.doc`; keys `right`/`left`/`enter`/`x`/`D` per spec §4, each followed by a refresh that re-finds the selected node by `id()` (or its parent's); the elide head is the row's prefix width; label `Overviews & notes %d`, empty text `(no overviews, notes or open files)`, hint per spec §4.
- [ ] **Step 4: run** the popup tests and `go test ./internal/tui -run 'TestDocTree|TestSwitcher|TestSessionsPopup|TestCtrlBackslash|OpenFiles|SessionDocs'` — PASS.
- [ ] **Step 5: commit** `tui: ctrl+\ shows the Overviews & notes tree`.

### Task 3: a child opens its overview at that file

**Files:** Modify `internal/tui/sessions_popup.go` (child `enter`), `internal/tui/split_overview.go` (`openOverviewAt`); test in `doc_tree_popup_test.go`.

- [ ] **Step 1: failing test** — `enter` on child `b.txt` (anchor 1): the top layer is a `*splitView` whose left is the overview with `ov.ov.sel == 1` and whose right document is `b.txt`; with the split already showing the overview at anchor 0, `enter` on `b.txt` leaves the right pane on `b.txt`.
- [ ] **Step 2: run** — FAIL.
- [ ] **Step 3: implement** `func (m Model) openOverviewAt(ov *openFile, i int) (Model, tea.Cmd)`: set `ov.ov.sel = i`; if `m.splitShowing(ov)` is non-nil, raise it as `openOverviewSplit` does and `steerSplit`; else `showOverview(ov)`.
- [ ] **Step 4: run** — PASS. **Step 5: commit** `tui: enter on an overview's file opens the overview beside it`.

### Task 4: words, help, docs, e2e, gate

**Files:** `internal/i18n/lang/{ja,ko,ru,zh}.toml`, `internal/tui/help.go` (the `ctrl+\` row), `README.md`, `CHANGELOG.md`, `docs/CLAUDE-details.md`, `e2e/scenarios/tui_session_docs.toml` (+ screens), `tui_overview_anchor_bands` screens, new `e2e/scenarios/tui_documents_tree.toml`.

- [ ] **Step 1:** every new key translated in all four bundles; `go test ./internal/tui -run 'I18n|Vocab|MenuLabels|EngineProse'` PASS.
- [ ] **Step 2:** the e2e scenarios: `tui_session_docs` follows the tree (the label, Notes/Opened groups); new `tui_documents_tree`: an overview naming two files with notes plus one plain noted file → collapsed screen, `right` expanded screen, `enter` on a child opens the split. `go test ./e2e -run 'TestScenarios/tui_' -update`, then READ every changed screen.
- [ ] **Step 3:** help row, README `ctrl+\` passage, CHANGELOG entry, CLAUDE-details paragraph.
- [ ] **Step 4:** `./test.sh race` → `all green`. **Step 5: commit** `docs + e2e: the Overviews & notes tree`.

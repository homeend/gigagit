# Review Answers (review links stage 2) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans.
> This repo forbids implementer subagents (CLAUDE.md): THIS session executes
> every task in order; only the final whole-branch review may be a read-only
> subagent. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Anyone (agent or user) can reply to a stored review's remarks and
resolve / unresolve any note thread; resolved threads start folded; review
rows show `12 remarks · 4 resolved`; agents read it all back through
`gg review show`.

**Architecture:** A reply to remark `review:<id>:<n>` is an ordinary stored
note at the REVIEW's address whose stored parent is the review
(`model.StoredRootID`), so the store's orphan prune, cap and cascade keep it
with its review. Resolutions are a `[[resolved]]` table in each note part,
keyed by thread root id, pruned with their root. Domain joins replies and
resolutions onto remarks by fingerprint at read time; every thread reader
stamps a `Resolution`; TUI, web, CLI and MCP render it.

**Tech Stack:** Go 1.26, Bubble Tea TUI, loopback web (vanilla JS), MCP
go-sdk, go-toml/v2, TOML e2e harness.

**Spec:** `docs/superpowers/specs/2026-10-05-review-answers-design.md`

## Global Constraints

- Worktree `/work/gigagit/.claude/worktrees/review-answers`, branch
  `feat/review-answers`; every shell command uses absolute paths (the shell
  cwd resets to the main checkout).
- Frontends never import `internal/git` (archtest); TUI/web reach git only
  through `domain`. Frontends never import `internal/notes`.
- Every user-visible TUI string: `i18n.T("<literal>")` + an entry in ALL FOUR
  bundles `internal/i18n/lang/{ja,ko,zh,ru}.toml`; plurals = two keys.
  CLI/engine prose stays English.
- Row tallies use plain words — never ✓ ✗ ✔ or other symbols (user ruling).
- No store or git work on the TUI Update thread: every domain call runs in a
  `tea.Cmd`.
- Collapsed state is never stored; Resolved is.
- Forge (`forge:`) threads: no local reply, no local resolve.
- Tests use real git in `t.TempDir()`; new TUI tests call `t.Parallel()`;
  tests that swap `notes.Now` run serially.
- Staging: `gg add <explicit paths>` then `git commit -F <scratchpad msg>`
  (gg commit has no -F); never `git add -A`.
- Commit trailer: `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`
  + `Claude-Session: https://claude.ai/code/session_01GaVoNSXah5Ti1LcfmvUoXc`.
- Race gate is green only when `./test.sh race` prints `all green`.

## Deviations from the spec (decided while planning)

- **Name of the thread state in Go:** `model.ThreadResolution` and
  `ResolvedNote.Resolution *model.ThreadResolution` (nil = open), not
  `Resolved bool / ResolvedBy / ResolvedAt`: domain already uses "resolved"
  for "resolved against a diff" (`ResolvedNote`, `keepResolved`), and a
  pointer carries who and when in one field. The wire keeps the spec's names
  (`resolved`, `resolved_by`, `resolved_at`, snake case like the rest of
  `WireNote`).
- **Remark replies bypass `NoteAdd`:** they are stored by
  `replyToRemark` straight through `st.Put` — `NoteAdd` would try to read a
  file side for a path-less note and fail.
- **Batch `resolve` is allowed only on `replyTo` items** (a new root with
  `resolve` is refused at parse time) and is applied after every reply of
  the batch is stored; a failed resolve rolls back the batch's replies and
  restores the resolutions it already changed.
- **TUI E / Delete keep today's target rule** (the thread's visible root):
  on a remark root they refuse; replies — remark replies included — are
  edited or removed in gg web or with `gg note rm`, as stored replies are
  today (spec §2.1 said "E and Delete on a reply work as on any reply" —
  in the TUI no reply is targetable today).
- **A note's link opens from the `.` menu** ("Open link: …"), not by enter
  on a link row: the diff cursor walks diff lines, never note rows
  (spec §2.2).
- **Link validation lives in domain** (`Service.NoteLink`, called inside
  `NoteAdd` / `NoteReply`), so CLI, MCP, web and TUI share one rule.
- **`WireNote.Replyable`** (new, `replyable`) tells the page a read-only
  root still takes replies (a review remark); `read_only` keeps meaning
  "never edited or removed".

## Review Focus

1. A note write ANYWHERE in the part after B replied (e.g. the user adds a
   line note on another commit) must not delete B's remark replies — the
   orphan prune runs on every write. (Task 1 test
   `TestRemarkReplySurvivesOtherWrites`.)
2. A review re-saved under the same id with remarks re-ordered: replies and
   the resolved flag follow the remark by fingerprint, not by index; a
   remark that vanished shows its thread as outdated, never on the wrong
   remark. (Task 2 tests `TestRemarkRepliesFollowAMovedRemark`,
   `TestRemarkRepliesOfAVanishedRemarkAreOutdated`.)
3. Deleting the review (TUI Delete, `gg note rm <review id>`,
   `NotesClearAtCommit`) leaves no remark reply and no `[[resolved]]` entry
   behind in the part file. (Task 1 `TestRemovingAReviewTakesItsRemarkReplies`,
   Task 1 `TestResolutionsArePrunedWithTheirRoot`.)
4. A remark reply never surfaces as a stray note: no ✎ marker, no Notes row,
   no entry in View all notes / `gg note list`, no thread in a preview's
   path-less listing. (Task 3 `TestRemarkRepliesAreNeverListedOnTheirOwn`.)
5. Two gg processes resolving different threads in one part at once — each
   mutation re-reads under the part lock, so both survive. (Task 1
   `TestConcurrentResolvesBothLand`.)

---

### Task 1: Model + store — remark replies are the review's children, `[[resolved]]` table

**Files:**
- Modify: `internal/model/note.go` (Note fields; `StoredRootID`,
  `ParseReviewNoteID`, `Note.StoredParent`, `Note.IsRemarkReply`,
  `ThreadResolution`)
- Modify: `internal/notes/part_file.go` (index, read/write, mutate, Remove,
  dropOrphanReplies, capOldestFirst, resolution mutations)
- Modify: `internal/notes/file_store.go` (Put routing, resolution API)
- Modify: `internal/notes/store.go` (interface)
- Test: `internal/model/note_test.go`, `internal/notes/part_file_test.go`,
  `internal/notes/file_store_test.go`

**Interfaces:**
- Produces:
  - `model.Note.Link, RemarkFP, RemarkSummary string`
  - `func model.StoredRootID(id string) string`
  - `func model.ParseReviewNoteID(id string) (reviewID string, n int, ok bool)`
  - `func (model.Note) StoredParent() string`, `func (model.Note) IsRemarkReply() bool`
  - `type model.ThreadResolution struct{ Root, By string; At time.Time; RemarkFP string }`
  - `notes.Store` gains `LoadResolved(p Part) ([]model.ThreadResolution, error)`,
    `LoadAllResolved() ([]model.ThreadResolution, error)`,
    `Resolve(r model.ThreadResolution) error`, `Unresolve(root string) error`
    (`ErrNotFound` when the root or the entry is absent).

- [ ] **Step 1: Write the failing model tests** (append to `internal/model/note_test.go`)

```go
func TestStoredRootIDMapsARemarkToItsReview(t *testing.T) {
	cases := map[string]string{
		"review:ab12cd34:3": "ab12cd34",
		"review:ab12cd34:0": "ab12cd34",
		"ab12cd34":          "ab12cd34",
		"forge:991":         "forge:991",
		"review:bad":        "review:bad", // no index: not a remark id
		"":                  "",
	}
	for in, want := range cases {
		if got := StoredRootID(in); got != want {
			t.Errorf("StoredRootID(%q) = %q, want %q", in, got, want)
		}
	}
	r := Note{ID: "r1", ParentID: "review:ab12cd34:2"}
	if r.StoredParent() != "ab12cd34" || !r.IsRemarkReply() {
		t.Fatalf("remark reply: StoredParent=%q IsRemarkReply=%v", r.StoredParent(), r.IsRemarkReply())
	}
	p := Note{ID: "r2", ParentID: "11223344"}
	if p.StoredParent() != "11223344" || p.IsRemarkReply() {
		t.Fatalf("plain reply: StoredParent=%q IsRemarkReply=%v", p.StoredParent(), p.IsRemarkReply())
	}
	if id, n, ok := ParseReviewNoteID("review:ab12cd34:7"); !ok || id != "ab12cd34" || n != 7 {
		t.Fatalf("ParseReviewNoteID = %q %d %v", id, n, ok)
	}
	for _, bad := range []string{"review:ab12cd34", "review::3", "review:ab:-1", "review:ab:x", "ab:3"} {
		if _, _, ok := ParseReviewNoteID(bad); ok {
			t.Errorf("ParseReviewNoteID(%q) accepted", bad)
		}
	}
}
```

- [ ] **Step 2: Run it — expect a compile failure**

Run: `cd /work/gigagit/.claude/worktrees/review-answers && go test ./internal/model/ -run TestStoredRootID`
Expected: FAIL — `undefined: StoredRootID`.

- [ ] **Step 3: Implement in `internal/model/note.go`**

Add to `Note`, just before `Created`:

```go
	// Link is an address the note points at — a gg:// link or a full commit
	// sha (a reply's "here is the fix"). Empty on most notes.
	Link string `toml:"link,omitempty"`
	// RemarkFP and RemarkSummary are set only on a reply to a review remark
	// (ParentID "review:<id>:<n>"): the remark's fingerprint, which finds it
	// again when the review is re-saved, and its summary, which an outdated
	// thread still shows.
	RemarkFP      string `toml:"remark_fp,omitempty"`
	RemarkSummary string `toml:"remark_summary,omitempty"`
```

After `IsReadOnlyNoteID`:

```go
// ParseReviewNoteID splits a review remark id "review:<id>:<n>".
func ParseReviewNoteID(id string) (reviewID string, n int, ok bool) {
	rest, found := strings.CutPrefix(id, ReviewNoteIDPrefix)
	if !found {
		return "", 0, false
	}
	i := strings.LastIndexByte(rest, ':')
	if i <= 0 {
		return "", 0, false
	}
	n, err := strconv.Atoi(rest[i+1:])
	if err != nil || n < 0 {
		return "", 0, false
	}
	return rest[:i], n, true
}

// StoredRootID is the STORED note a thread root id lives under: the review
// for a review remark ("review:<id>:<n>" → "<id>"), the id itself otherwise.
// A reply to a remark is a stored child of its review: the store's orphan
// prune, cap and cascade all key on this.
func StoredRootID(id string) string {
	if rid, _, ok := ParseReviewNoteID(id); ok {
		return rid
	}
	return id
}

// ThreadResolution marks one note thread resolved (GitHub's "Resolve
// conversation"). Root is the thread's root id — a stored note id or a
// review remark id; RemarkFP is set for a remark root (it follows the remark
// when the review is re-saved). It is stored beside the notes, in the
// root's part, and goes when the root goes.
type ThreadResolution struct {
	Root     string    `toml:"root"`
	By       string    `toml:"by,omitempty"`
	At       time.Time `toml:"at"`
	RemarkFP string    `toml:"remark_fp,omitempty"`
}
```

After `IsReply`:

```go
// StoredParent is the stored note n hangs off: the review for a reply to a
// review remark, ParentID otherwise ("" for a root).
func (n Note) StoredParent() string { return StoredRootID(n.ParentID) }

// IsRemarkReply reports a reply to a review remark: it is shown only inside
// its review, never as a note of its own.
func (n Note) IsRemarkReply() bool { return IsReviewNoteID(n.ParentID) }
```

Add `"strconv"` to the imports.

- [ ] **Step 4: Run it — expect PASS**

Run: `cd /work/gigagit/.claude/worktrees/review-answers && go test ./internal/model/`
Expected: `ok`.

- [ ] **Step 5: Write the failing store tests** (append to `internal/notes/file_store_test.go`; reuse the file's existing helpers for a temp-root `FileStore` — read the top of the file and use the same constructor it uses, `NewFileStore(t.TempDir())`)

```go
func reviewRoot(id, commit string) model.Note {
	return model.Note{ID: id, Source: model.NoteSourceAgent, Tags: []string{model.ReviewTag},
		Address: model.FileAddress{State: model.StateCommitted, Commit: commit},
		Side:    model.NoteSideNew, Summary: "Review: x", Created: time.Unix(100, 0).UTC()}
}

func remarkReply(id, reviewID string, n int, commit string) model.Note {
	return model.Note{ID: id, ParentID: fmt.Sprintf("review:%s:%d", reviewID, n), Source: model.NoteSourceAgent,
		Address: model.FileAddress{State: model.StateCommitted, Commit: commit},
		Side:    model.NoteSideNew, Summary: "agreed", RemarkFP: "fp", RemarkSummary: "s",
		Created: time.Unix(200, 0).UTC()}
}

func lineNote(id, commit, path string) model.Note {
	return model.Note{ID: id, Source: model.NoteSourceUser,
		Address: model.FileAddress{State: model.StateCommitted, Commit: commit, Path: path},
		Side:    model.NoteSideNew, Range: [2]int{1, 1}, ContextHash: "h", Summary: "line",
		Created: time.Unix(300, 0).UTC()}
}

func TestRemarkReplySurvivesOtherWrites(t *testing.T) {
	st := NewFileStore(t.TempDir())
	c := strings.Repeat("a", 40)
	for _, n := range []model.Note{reviewRoot("rev00001", c), remarkReply("rep00001", "rev00001", 0, c)} {
		if err := st.Put(n); err != nil {
			t.Fatal(err)
		}
	}
	// Any later write in the part runs the orphan prune.
	if err := st.Put(lineNote("line0001", strings.Repeat("b", 40), "x.go")); err != nil {
		t.Fatal(err)
	}
	ns, err := st.Load(PartCommits)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(ns, func(n model.Note) bool { return n.ID == "rep00001" }) {
		t.Fatalf("the remark reply was pruned: %+v", ns)
	}
}

func TestRemovingAReviewTakesItsRemarkReplies(t *testing.T) {
	st := NewFileStore(t.TempDir())
	c := strings.Repeat("a", 40)
	for _, n := range []model.Note{reviewRoot("rev00001", c), remarkReply("rep00001", "rev00001", 0, c), lineNote("line0001", c, "x.go")} {
		if err := st.Put(n); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Remove("rev00001"); err != nil {
		t.Fatal(err)
	}
	ns, _ := st.Load(PartCommits)
	if len(ns) != 1 || ns[0].ID != "line0001" {
		t.Fatalf("after removing the review: %+v", ns)
	}
}

func TestResolutionsRoundTripAndRoute(t *testing.T) {
	root := t.TempDir()
	st := NewFileStore(root)
	c := strings.Repeat("a", 40)
	for _, n := range []model.Note{reviewRoot("rev00001", c), lineNote("line0001", c, "x.go")} {
		if err := st.Put(n); err != nil {
			t.Fatal(err)
		}
	}
	at := time.Unix(500, 0).UTC()
	if err := st.Resolve(model.ThreadResolution{Root: "review:rev00001:2", By: "B", At: at, RemarkFP: "fp"}); err != nil {
		t.Fatal(err)
	}
	if err := st.Resolve(model.ThreadResolution{Root: "line0001", By: "me", At: at}); err != nil {
		t.Fatal(err)
	}
	// Replace, not duplicate.
	if err := st.Resolve(model.ThreadResolution{Root: "line0001", By: "you", At: at}); err != nil {
		t.Fatal(err)
	}
	rs, err := st.LoadResolved(PartCommits)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 2 || rs[1].By != "you" {
		t.Fatalf("resolutions = %+v", rs)
	}
	if err := st.Resolve(model.ThreadResolution{Root: "nope0000", At: at}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("resolving an unknown root: %v", err)
	}
	if err := st.Unresolve("line0001"); err != nil {
		t.Fatal(err)
	}
	if err := st.Unresolve("line0001"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second unresolve: %v", err)
	}
	all, _ := st.LoadAllResolved()
	if len(all) != 1 || all[0].Root != "review:rev00001:2" {
		t.Fatalf("LoadAllResolved = %+v", all)
	}
	// The table is in the part file itself.
	data, _ := os.ReadFile(filepath.Join(root, "commits.toml"))
	if !strings.Contains(string(data), "[[resolved]]") {
		t.Fatalf("no [[resolved]] table:\n%s", data)
	}
}

func TestResolutionsArePrunedWithTheirRoot(t *testing.T) {
	st := NewFileStore(t.TempDir())
	c := strings.Repeat("a", 40)
	for _, n := range []model.Note{reviewRoot("rev00001", c), lineNote("line0001", c, "x.go")} {
		if err := st.Put(n); err != nil {
			t.Fatal(err)
		}
	}
	at := time.Unix(500, 0).UTC()
	_ = st.Resolve(model.ThreadResolution{Root: "review:rev00001:0", At: at, RemarkFP: "fp"})
	_ = st.Resolve(model.ThreadResolution{Root: "line0001", At: at})
	if err := st.Remove("rev00001"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Sweep(func(n model.Note) bool { return n.ID != "line0001" }); err != nil {
		t.Fatal(err)
	}
	if rs, _ := st.LoadAllResolved(); len(rs) != 0 {
		t.Fatalf("orphaned resolutions kept: %+v", rs)
	}
}

func TestConcurrentResolvesBothLand(t *testing.T) {
	root := t.TempDir()
	c := strings.Repeat("a", 40)
	seed := NewFileStore(root)
	for _, n := range []model.Note{lineNote("line0001", c, "x.go"), lineNote("line0002", c, "y.go")} {
		if err := seed.Put(n); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	for _, id := range []string{"line0001", "line0002"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Two stores = two processes' views of one directory.
			if err := NewFileStore(root).Resolve(model.ThreadResolution{Root: id, At: time.Unix(1, 0).UTC()}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if rs, _ := NewFileStore(root).LoadAllResolved(); len(rs) != 2 {
		t.Fatalf("resolutions = %+v", rs)
	}
}
```

Add the imports the file lacks (`errors`, `fmt`, `os`, `path/filepath`,
`slices`, `strings`, `sync`, `time`, `model`).

- [ ] **Step 6: Run them — expect compile failure**

Run: `cd /work/gigagit/.claude/worktrees/review-answers && go test ./internal/notes/ -run 'RemarkReply|RemovingAReview|Resolutions|ConcurrentResolves'`
Expected: FAIL — `st.Resolve undefined`.

- [ ] **Step 7: Implement the part file** (`internal/notes/part_file.go`)

Replace `index`, `read`, `write` and `mutateCap`, and add the resolution
mutations:

```go
type index struct {
	Notes    []model.Note             `toml:"notes"`
	Resolved []model.ThreadResolution `toml:"resolved,omitempty"`
}

// readIndex parses the file (see read for the missing-file rule).
func (fs *partFile) readIndex() (index, error) {
	data, err := os.ReadFile(fs.path)
	if err != nil {
		if os.IsNotExist(err) {
			return index{}, nil
		}
		return index{}, err
	}
	var idx index
	if err := toml.Unmarshal(data, &idx); err != nil {
		return index{}, fmt.Errorf("%w: %s: %v", ErrCorrupt, fs.path, err)
	}
	return idx, nil
}

// read parses the file's notes. ... (keep the existing doc comment)
func (fs *partFile) read() ([]model.Note, error) {
	idx, err := fs.readIndex()
	return idx.Notes, err
}

// LoadResolved returns this part's thread resolutions. Never writes.
func (fs *partFile) LoadResolved() ([]model.ThreadResolution, error) {
	idx, err := fs.readIndex()
	return idx.Resolved, err
}
```

`write` takes the whole index (a part with no notes is removed — a
resolution cannot outlive its root, so it never keeps a file alive):

```go
func (fs *partFile) write(idx index) error {
	if len(idx.Notes) == 0 {
		if err := os.Remove(fs.path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	data, err := toml.Marshal(idx)
	// … the rest of the existing body unchanged …
}
```

`mutateCap` reads the index, applies to the notes, prunes, caps, then keeps
only the resolutions whose root is still here:

```go
	before, err := fs.readIndex()
	if err != nil {
		return 0, err
	}
	ns, err := apply(slices.Clone(before.Notes))
	if err != nil {
		return 0, err
	}
	ns = dropOrphanReplies(ns)
	if capped {
		ns = capOldestFirst(ns, fs.pol.MaxEntries)
	}
	rs := keepRootedResolutions(before.Resolved, ns)
	if sameNotes(before.Notes, ns) && sameResolutions(before.Resolved, rs) {
		return len(ns), nil
	}
	if werr := fs.write(index{Notes: ns, Resolved: rs}); werr != nil {
		return 0, werr
	}
	return len(ns), nil
```

New helpers and mutations:

```go
// keepRootedResolutions drops a resolution whose thread root is gone from
// the part: a stored root by id, a review remark by its review.
func keepRootedResolutions(rs []model.ThreadResolution, ns []model.Note) []model.ThreadResolution {
	if len(rs) == 0 {
		return rs
	}
	roots := make(map[string]bool, len(ns))
	for _, n := range ns {
		if !n.IsReply() {
			roots[n.ID] = true
		}
	}
	kept := make([]model.ThreadResolution, 0, len(rs))
	for _, r := range rs {
		if roots[model.StoredRootID(r.Root)] {
			kept = append(kept, r)
		}
	}
	return kept
}

func sameResolutions(a, b []model.ThreadResolution) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// mutateResolved is mutate for the resolutions: same locks, fresh read,
// atomic rewrite; the notes are untouched (apply sees them to check a
// root). A resolution whose root is not in this part is dropped.
func (fs *partFile) mutateResolved(apply func(ns []model.Note, rs []model.ThreadResolution) ([]model.ThreadResolution, error)) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	unlock, err := fs.lock()
	if err != nil {
		return err
	}
	defer unlock()
	before, err := fs.readIndex()
	if err != nil {
		return err
	}
	rs, err := apply(before.Notes, slices.Clone(before.Resolved))
	if err != nil {
		return err
	}
	rs = keepRootedResolutions(rs, before.Notes)
	if sameResolutions(before.Resolved, rs) {
		return nil
	}
	return fs.write(index{Notes: before.Notes, Resolved: rs})
}

// Resolve adds r, or replaces the entry with the same Root. ErrNotFound when
// the root is not in this part.
func (fs *partFile) Resolve(r model.ThreadResolution) error {
	return fs.mutateResolved(func(ns []model.Note, rs []model.ThreadResolution) ([]model.ThreadResolution, error) {
		if !hasRoot(ns, model.StoredRootID(r.Root)) {
			return nil, ErrNotFound
		}
		for i := range rs {
			if rs[i].Root == r.Root {
				rs[i] = r
				return rs, nil
			}
		}
		return append(rs, r), nil
	})
}

// Unresolve removes root's entry; ErrNotFound when there is none.
func (fs *partFile) Unresolve(root string) error {
	return fs.mutateResolved(func(_ []model.Note, rs []model.ThreadResolution) ([]model.ThreadResolution, error) {
		i := slices.IndexFunc(rs, func(r model.ThreadResolution) bool { return r.Root == root })
		if i < 0 {
			return nil, ErrNotFound
		}
		return slices.Delete(rs, i, i+1), nil
	})
}

func hasRoot(ns []model.Note, id string) bool {
	return slices.ContainsFunc(ns, func(n model.Note) bool { return n.ID == id && !n.IsReply() })
}
```

Switch the three stored-parent sites to `StoredParent()`:

```go
// Remove: a root takes its replies — a review takes its remark replies too.
			if n.ID == id || n.StoredParent() == id {

// dropOrphanReplies
		if n.IsReply() && !roots[n.StoredParent()] {

// capOldestFirst (three sites)
		if !exempt[n.ID] && !exempt[n.StoredParent()] {
			if n.StoredParent() == r.ID {
		if doomed[n.ID] || doomed[n.StoredParent()] {
```

Check `legacy.go` and every other `fs.read()`/`fs.write(` caller in the
package compiles with the new `write(index)` signature
(`grep -n 'fs.write(\|\.write(' internal/notes/*.go`); a legacy merge
writes `index{Notes: ns}`.

- [ ] **Step 8: Implement the FileStore + interface** (`file_store.go`, `store.go`)

```go
// store.go — add to Store:
	LoadResolved(p Part) ([]model.ThreadResolution, error)
	LoadAllResolved() ([]model.ThreadResolution, error)
	// Resolve records a thread resolved in its root's part (a review remark's
	// review); ErrNotFound when no part holds the root. Unresolve removes the
	// entry; ErrNotFound when there is none.
	Resolve(r model.ThreadResolution) error
	Unresolve(root string) error
```

```go
// file_store.go
func (fs *FileStore) LoadResolved(p Part) ([]model.ThreadResolution, error) {
	return fs.file(p).LoadResolved()
}

func (fs *FileStore) LoadAllResolved() ([]model.ThreadResolution, error) {
	parts, err := fs.Parts()
	if err != nil {
		return nil, err
	}
	var all []model.ThreadResolution
	var errs []error
	for _, p := range parts {
		rs, lerr := fs.LoadResolved(p)
		if lerr != nil {
			errs = append(errs, lerr)
			continue
		}
		all = append(all, rs...)
	}
	return all, errors.Join(errs...)
}

// rootPart is the part holding the stored note a thread root id lives under.
func (fs *FileStore) rootPart(root string) (Part, error) {
	at, unread, err := fs.where()
	if err != nil {
		return "", err
	}
	if p, ok := at[model.StoredRootID(root)]; ok {
		return p, nil
	}
	if unread != nil {
		return "", unread
	}
	return "", ErrNotFound
}

func (fs *FileStore) Resolve(r model.ThreadResolution) error {
	p, err := fs.rootPart(r.Root)
	if err != nil {
		return err
	}
	return fs.file(p).Resolve(r)
}

func (fs *FileStore) Unresolve(root string) error {
	p, err := fs.rootPart(root)
	if err != nil {
		return err
	}
	return fs.file(p).Unresolve(root)
}
```

In `Put`, route a reply by its stored parent:

```go
	if n.IsReply() {
		p, ok := at[n.StoredParent()]
```

Any test type in `internal/domain` that implements `notes.Store` by
embedding it keeps compiling; a type that implements it method-by-method
(grep `func (.*) Sweep(keep func(model.Note) bool)` under `internal/`)
gets the four methods delegating to its inner store.

- [ ] **Step 9: Run the package — expect PASS**

Run: `cd /work/gigagit/.claude/worktrees/review-answers && go test ./internal/notes/ ./internal/model/ && go build ./...`
Expected: `ok` ×2, build clean.

- [ ] **Step 10: Commit**

```bash
cd /work/gigagit/.claude/worktrees/review-answers && gg add internal/model/note.go internal/model/note_test.go internal/notes/part_file.go internal/notes/file_store.go internal/notes/store.go internal/notes/file_store_test.go && git commit -F <scratchpad>/msg.txt
```
Message: `feat(notes): remark replies are their review's children; [[resolved]] thread table`

---

### Task 2: Domain — reply to a remark, join replies and resolutions onto remarks

**Files:**
- Create: `internal/domain/review_threads.go`
- Modify: `internal/domain/notes.go` (`NoteReply` routes `review:` parents)
- Modify: `internal/domain/review_notes.go` (`Review` gains threads;
  `reviewNotes` callers load them)
- Modify: `internal/domain/review_doc.go` (`reviewDocNotes` attaches;
  `ReviewOtherNote` gains outdated threads; `reviewSplit`)
- Modify: `internal/domain/working_review.go` (`WorkingReviews` loads threads)
- Modify: `internal/domain/forge_notes.go` (error vars)
- Test: `internal/domain/review_threads_test.go`

**Interfaces:**
- Consumes (Task 1): `model.StoredRootID`, `ParseReviewNoteID`,
  `Note.IsRemarkReply`, `Note.RemarkFP/RemarkSummary/Link`,
  `model.ThreadResolution`, `notes.Store.LoadResolved`.
- Produces:
  - `var ErrNoSuchRemark`, `var ErrForgeReadOnly` (forge reply), `var ErrForgeResolved`
  - `func remarkFP(path string, side model.NoteSide, rng [2]int, summary string) string`
  - `Review.Replies []model.Note` (its remark replies, creation order),
    `Review.Resolutions []model.ThreadResolution` (its `review:<id>:*` entries)
  - `ResolvedNote.Resolution *model.ThreadResolution`
  - `type RemarkThread struct{ Remark int; Replies []model.Note; Resolution *model.ThreadResolution }`
  - `func (r Review) RemarkThreads() (byRemark []RemarkThread, outdated []OutdatedThread)`
    — one entry per document remark (index = remark n), plus the threads
    whose remark is gone
  - `type OutdatedThread struct{ Root, Summary string; Replies []model.Note; Resolution *model.ThreadResolution }`
  - `func (r Review) Tally() (remarks, resolved int)`

- [ ] **Step 1: Write the failing tests** (`internal/domain/review_threads_test.go`)

The fixture builds on `reviewRepo` + `commitFile` + `revParse` (the helpers
`structuredReview` in `review_doc_test.go` uses):

```go
package domain

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/model"
)

// threadDoc has three remarks, all on lines of g.txt (3 lines).
const threadDoc = `{"version":1,"summary":"ok","files":[{"path":"g.txt","annotations":[
 {"newRange":[1,1],"summary":"first"},
 {"newRange":[2,2],"summary":"second"},
 {"newRange":[3,3],"summary":"third"}]}]}`

const threadDocReversed = `{"version":1,"summary":"ok","files":[{"path":"g.txt","annotations":[
 {"newRange":[3,3],"summary":"third"},
 {"newRange":[2,2],"summary":"second"},
 {"newRange":[1,1],"summary":"first"}]}]}`

const threadDocWithoutThird = `{"version":1,"summary":"ok","files":[{"path":"g.txt","annotations":[
 {"newRange":[1,1],"summary":"first"},
 {"newRange":[2,2],"summary":"second"}]}]}`

// threadReview stores threadDoc on a commit adding g.txt; it returns the
// service, the review id and its target (to re-save under the same id).
func threadReview(t *testing.T) (*Service, string, ReviewTarget) {
	t.Helper()
	dir, svc, _ := reviewRepo(t)
	commitFile(t, dir, "g.txt", "one\ntwo\nthree\n", "add g")
	tip := revParse(t, dir, "HEAD")
	tg := ReviewTarget{Kind: ReviewRange, Range: tip + "^.." + tip, Label: "g"}
	id, _, err := svc.SaveReview(context.Background(), SaveReview{Target: tg, Agent: "A", Text: threadDoc})
	if err != nil {
		t.Fatal(err)
	}
	return svc, id, tg
}

func resave(t *testing.T, svc *Service, id string, tg ReviewTarget, doc string) {
	t.Helper()
	if _, _, err := svc.SaveReview(context.Background(), SaveReview{Target: tg, Agent: "A", Text: doc, NoteID: id}); err != nil {
		t.Fatal(err)
	}
}

func mustReview(t *testing.T, svc *Service, id string) Review {
	t.Helper()
	r, err := svc.Review(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func remarkID(rid string, n int) string { return model.ReviewNoteIDPrefix + rid + ":" + itoa(n) }

func itoa(n int) string { return string(rune('0' + n)) } // indices < 10 here

func TestReplyToARemarkIsStoredWithItsReview(t *testing.T) {
	t.Parallel()
	svc, rid, _ := threadReview(t)
	ctx := context.Background()
	rep, err := svc.NoteReply(ctx, remarkID(rid, 1), model.Note{Source: model.NoteSourceAgent, Author: "B", Summary: "agreed"})
	if err != nil {
		t.Fatal(err)
	}
	r := mustReview(t, svc, rid)
	if rep.Address.Path != "" || rep.Address.Commit != r.Commit || rep.RemarkFP == "" || rep.RemarkSummary != "second" {
		t.Fatalf("reply = %+v", rep)
	}
	if _, err := svc.NoteReply(ctx, remarkID(rid, 9), model.Note{Summary: "x"}); !errors.Is(err, ErrNoSuchRemark) {
		t.Fatalf("out of range: %v", err)
	}
	if _, err := svc.NoteReply(ctx, model.ReviewNoteIDPrefix+"deadbeef:0", model.Note{Summary: "x"}); !errors.Is(err, ErrReviewNotFound) {
		t.Fatalf("gone review: %v", err)
	}
	if _, err := svc.NoteReply(ctx, "forge:12", model.Note{Summary: "x"}); !errors.Is(err, ErrReadOnlyNote) {
		t.Fatalf("forge: %v", err)
	}
	// A reply to B's reply stays flat under the remark.
	if _, err := svc.NoteReply(ctx, rep.ID, model.Note{Author: "A", Summary: "thanks"}); err != nil {
		t.Fatal(err)
	}
	th, _ := mustReview(t, svc, rid).RemarkThreads()
	if len(th) != 3 || len(th[1].Replies) != 2 || len(th[0].Replies) != 0 {
		t.Fatalf("threads = %+v", th)
	}
}

func TestRemarkRepliesFollowAMovedRemark(t *testing.T) {
	t.Parallel()
	svc, rid, tg := threadReview(t)
	ctx := context.Background()
	if _, err := svc.NoteReply(ctx, remarkID(rid, 2), model.Note{Author: "B", Summary: "on third"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.NoteResolve(ctx, remarkID(rid, 2), true, "B"); err != nil {
		t.Fatal(err)
	}
	resave(t, svc, rid, tg, threadDocReversed)
	th, outdated := mustReview(t, svc, rid).RemarkThreads()
	if len(outdated) != 0 || len(th[0].Replies) != 1 || th[0].Replies[0].Summary != "on third" || th[0].Resolution == nil || th[2].Resolution != nil {
		t.Fatalf("threads=%+v outdated=%+v", th, outdated)
	}
}

func TestRemarkRepliesOfAVanishedRemarkAreOutdated(t *testing.T) {
	t.Parallel()
	svc, rid, tg := threadReview(t)
	ctx := context.Background()
	if _, err := svc.NoteReply(ctx, remarkID(rid, 2), model.Note{Author: "B", Summary: "on third"}); err != nil {
		t.Fatal(err)
	}
	resave(t, svc, rid, tg, threadDocWithoutThird)
	th, outdated := mustReview(t, svc, rid).RemarkThreads()
	if len(th) != 2 || len(outdated) != 1 || outdated[0].Summary != "third" || outdated[0].Replies[0].Summary != "on third" {
		t.Fatalf("threads=%+v outdated=%+v", th, outdated)
	}
	other, err := svc.ReviewOtherNotes(ctx, rid)
	if err != nil || !slices.ContainsFunc(other, func(o ReviewOtherNote) bool { return o.Outdated && o.Summary == "third" }) {
		t.Fatalf("ReviewOtherNotes = %+v, %v", other, err)
	}
}

func TestReviewNotesForCarriesRepliesAndResolution(t *testing.T) {
	t.Parallel()
	svc, rid, _ := threadReview(t)
	ctx := context.Background()
	id := remarkID(rid, 0)
	if _, err := svc.NoteReply(ctx, id, model.Note{Author: "B", Summary: "ok"}); err != nil {
		t.Fatal(err)
	}
	st := svc.notesStore(ctx)
	fp := mustReview(t, svc, rid).remarkFPs()[0]
	if err := st.Resolve(model.ThreadResolution{Root: id, By: "B", At: time.Unix(9, 0).UTC(), RemarkFP: fp}); err != nil {
		t.Fatal(err)
	}
	r := mustReview(t, svc, rid)
	got, err := svc.ReviewNotesFor(ctx, rid, "g.txt", sideDiff("one", "two", "three")) // review_doc_test.go helper
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(got, func(n ResolvedNote) bool { return n.Note.ID == id })
	if i < 0 || len(got[i].Replies) != 1 || got[i].Resolution == nil || got[i].Resolution.By != "B" {
		t.Fatalf("remark 0 = %+v", got)
	}
	if rem, res := r.Tally(); rem != 3 || res != 1 {
		t.Fatalf("Tally = %d %d", rem, res)
	}
}
```

`TestRemarkRepliesFollowAMovedRemark` uses `NoteResolve` from Task 3: write
it now, expect it to fail to compile until Task 3, and mark it
`t.Skip("Task 3")` at the top until then (remove the skip in Task 3 Step 4).

- [ ] **Step 2: Run them — expect compile failure**

Run: `cd /work/gigagit/.claude/worktrees/review-answers && go test ./internal/domain/ -run 'RemarkReplies|ReplyToARemark|ReviewNotesForCarries'`
Expected: FAIL — `undefined: ErrNoSuchRemark` (and friends).

- [ ] **Step 3: Implement `internal/domain/review_threads.go`**

```go
package domain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notes"
)

// Review remarks are threads (spec 2026-10-05 review answers): a reply to
// remark "review:<id>:<n>" is a stored note at the REVIEW's address whose
// stored parent is the review; a resolution is a [[resolved]] entry keyed by
// the remark id. Both find their remark again by fingerprint, so a re-saved
// review never moves an answer onto another remark.

// ErrNoSuchRemark is a remark index the review does not have.
var ErrNoSuchRemark = errors.New("no such remark")

// remarkFP fingerprints one remark: what it says and where.
func remarkFP(path string, side model.NoteSide, rng [2]int, summary string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d\x00%d\x00%s", reviewPath(path), side, rng[0], rng[1], summary)))
	return hex.EncodeToString(h[:8])
}

// docRemark is one document remark in document order (index = n).
type docRemark struct {
	path    string
	side    model.NoteSide
	rng     [2]int
	summary string
	fp      string
}

func (r Review) docRemarks() []docRemark {
	if r.Doc == nil {
		return nil
	}
	var out []docRemark
	for _, f := range r.Doc.Files {
		for _, dn := range f.Notes {
			side := reviewSide(dn.Side)
			out = append(out, docRemark{path: reviewPath(f.Path), side: side, rng: dn.Range,
				summary: dn.Summary, fp: remarkFP(f.Path, side, dn.Range, dn.Summary)})
		}
	}
	return out
}

// remarkFPs is every remark's fingerprint, by index.
func (r Review) remarkFPs() []string {
	var out []string
	for _, d := range r.docRemarks() {
		out = append(out, d.fp)
	}
	return out
}

// RemarkThread is what hangs off one remark: its replies (creation order)
// and its resolution (nil = open).
type RemarkThread struct {
	Remark     int
	Replies    []model.Note
	Resolution *model.ThreadResolution
}

// OutdatedThread is a thread whose remark the review no longer has: Root is
// the remark id it was made under, Summary the remark's summary as the
// first reply recorded it.
type OutdatedThread struct {
	Root, Summary string
	Replies       []model.Note
	Resolution    *model.ThreadResolution
}

// remarkFor finds the remark a thread made under root with fingerprint fp
// belongs to now: the same index when it still carries fp, else the remark
// that does, else -1 (outdated).
func remarkFor(rs []docRemark, root, fp string) int {
	if _, n, ok := model.ParseReviewNoteID(root); ok && n < len(rs) && rs[n].fp == fp {
		return n
	}
	for i, d := range rs {
		if d.fp == fp {
			return i
		}
	}
	return -1
}

// RemarkThreads joins r.Replies and r.Resolutions onto r's remarks.
func (r Review) RemarkThreads() (byRemark []RemarkThread, outdated []OutdatedThread) {
	rs := r.docRemarks()
	byRemark = make([]RemarkThread, len(rs))
	for i := range byRemark {
		byRemark[i].Remark = i
	}
	gone := map[string]*OutdatedThread{} // by fingerprint
	var order []string
	lost := func(root, fp, summary string) *OutdatedThread {
		if o, ok := gone[fp]; ok {
			return o
		}
		o := &OutdatedThread{Root: root, Summary: summary}
		gone[fp] = o
		order = append(order, fp)
		return o
	}
	for _, n := range r.Replies {
		if i := remarkFor(rs, n.ParentID, n.RemarkFP); i >= 0 {
			byRemark[i].Replies = append(byRemark[i].Replies, n)
			continue
		}
		o := lost(n.ParentID, n.RemarkFP, n.RemarkSummary)
		o.Replies = append(o.Replies, n)
	}
	for i := range r.Resolutions {
		res := r.Resolutions[i]
		if j := remarkFor(rs, res.Root, res.RemarkFP); j >= 0 {
			byRemark[j].Resolution = &res
			continue
		}
		if o, ok := gone[res.RemarkFP]; ok {
			o.Resolution = &res
		} // a resolution with no remark and no reply left: nothing to show
	}
	for _, fp := range order {
		outdated = append(outdated, *gone[fp])
	}
	return byRemark, outdated
}

// Tally is the review's remark count and how many of them are resolved.
func (r Review) Tally() (remarks, resolved int) {
	th, _ := r.RemarkThreads()
	for _, t := range th {
		if t.Resolution != nil {
			resolved++
		}
	}
	return len(th), resolved
}

// reviewThreads is every remark reply and remark resolution in parts, by
// review id: one read per part, shared by all the reviews built from it.
type reviewThreads struct {
	replies     map[string][]model.Note
	resolutions map[string][]model.ThreadResolution
}

func (s *Service) loadReviewThreads(st notes.Store, all []model.Note, parts []notes.Part) reviewThreads {
	th := reviewThreads{replies: map[string][]model.Note{}, resolutions: map[string][]model.ThreadResolution{}}
	for _, n := range all {
		if n.IsRemarkReply() {
			id := n.StoredParent()
			th.replies[id] = append(th.replies[id], n)
		}
	}
	for id := range th.replies {
		sort.SliceStable(th.replies[id], func(a, b int) bool { return th.replies[id][a].Created.Before(th.replies[id][b].Created) })
	}
	for _, p := range parts {
		rs, err := st.LoadResolved(p)
		if err != nil {
			continue // an unreadable table hides resolutions, never fails a read
		}
		for _, res := range rs {
			if model.IsReviewNoteID(res.Root) {
				id := model.StoredRootID(res.Root)
				th.resolutions[id] = append(th.resolutions[id], res)
			}
		}
	}
	return th
}

// withThreads attaches review r's replies and resolutions.
func (th reviewThreads) attach(r Review) Review {
	r.Replies, r.Resolutions = th.replies[r.ID], th.resolutions[r.ID]
	return r
}

// replyToRemark stores a reply to remark id ("review:<rid>:<n>") at the
// review's own address, stamped with the remark's fingerprint and summary.
func (s *Service) replyToRemark(ctx context.Context, id string, n model.Note) (model.Note, error) {
	rid, idx, ok := model.ParseReviewNoteID(id)
	if !ok {
		return model.Note{}, ErrNoteNotFound
	}
	st := s.notesStore(ctx)
	if st == nil {
		return model.Note{}, ErrNotesDisabled
	}
	all, err := st.LoadAll()
	if err != nil {
		return model.Note{}, err
	}
	var root *model.Note
	for i := range all {
		if all[i].ID == rid && !all[i].IsReply() && (all[i].IsReviewNote() || all[i].IsWorkingReview()) {
			root = &all[i]
			break
		}
	}
	if root == nil {
		return model.Note{}, fmt.Errorf("%w: %s", ErrReviewNotFound, rid)
	}
	r := s.reviewOf(ctx, *root, map[string]string{})
	rs := r.docRemarks()
	if idx >= len(rs) {
		return model.Note{}, fmt.Errorf("%w: review %s has no remark %d", ErrNoSuchRemark, rid, idx)
	}
	now := notes.Now().UTC()
	n.ID = notes.NewID(all)
	n.ParentID = id
	if n.Source == "" {
		n.Source = model.NoteSourceUser
	}
	n.Address, n.Side, n.Range, n.ContextHash = root.Address, model.NoteSideNew, [2]int{}, ""
	n.RemarkFP, n.RemarkSummary = rs[idx].fp, rs[idx].summary
	n.Created, n.Updated = now, now
	if err := st.Put(n); err != nil {
		return model.Note{}, err
	}
	s.invalidateNoteCounts()
	return n, nil
}

```

In `notes.go` `NoteReply`, route before the read-only refusal:

```go
func (s *Service) NoteReply(ctx context.Context, parentID string, n model.Note) (model.Note, error) {
	if model.IsReviewNoteID(parentID) {
		return s.replyToRemark(ctx, parentID, n)
	}
	if model.IsReadOnlyNoteID(parentID) {
		return model.Note{}, ErrReadOnlyNote
	}
	// … unchanged …
```

and in the stored-root branch, when the root found is itself a remark reply
(`root.IsRemarkReply()` after the walk — a reply to B's reply), hand off to
`replyToRemark(ctx, root.ParentID, n)` so the thread stays flat under the
remark.

`Review` (in `review_notes.go`) gains:

```go
	// Replies are the review's remark replies (creation order) and
	// Resolutions its remark resolutions; RemarkThreads joins them.
	Replies     []model.Note
	Resolutions []model.ThreadResolution
```

`reviewNotes` returns the threads too: change it to
`func (s *Service) reviewNotes(ctx) ([]model.Note, reviewThreads, error)`
(load `parts` once, build `th := s.loadReviewThreads(st, all, parts)`), and
in `ReviewsForBranch`, `Review`, `ReviewsForCommit` wrap every
`s.reviewOf(...)` as `th.attach(s.reviewOf(...))`. Do the same in
`workingReviewNotes` / `WorkingReviews` (its part is the worktree part).

`ReviewOtherNote` gains:

```go
	// Outdated: a thread whose remark the re-saved review no longer has;
	// Summary is the remark's summary as its first reply recorded it.
	Outdated bool
	Root     string
	Replies  []model.Note
	Resolution *model.ThreadResolution
```

`reviewSplit` appends one `ReviewOtherNote{Outdated: true, Root: o.Root,
Summary: o.Summary, Replies: o.Replies, Resolution: o.Resolution}` per
`outdated` thread after the existing other notes (Path empty — the
renderer shows them under an "Outdated" heading, Task 7).

`reviewDocNotes` attaches each remark's thread: compute
`th, _ := r.RemarkThreads()` once at the top; for remark index `i` set

```go
			rn := ResolvedNote{Note: n, Status: model.NoteActive, Range: dn.Range,
				Resolution: th[i].Resolution}
			for _, rep := range th[i].Replies {
				rep.Side, rep.Range = side, dn.Range // a reply draws at its remark
				rep.Address = addr
				rn.Replies = append(rn.Replies, ResolvedNote{Note: rep, Status: model.NoteActive, Range: dn.Range})
			}
			out = append(out, rn)
```

(`i` is the existing running index — compute it BEFORE `i++` and before
the path filter `continue`.) `ResolvedNote` (notes.go) gains:

```go
	// Resolution is the thread's resolved state (nil = open): stored for a
	// stored root or a review remark, GitHub's for a forge thread.
	Resolution *model.ThreadResolution
```

- [ ] **Step 4: Run — expect PASS; then the domain package**

Run: `cd /work/gigagit/.claude/worktrees/review-answers && go test ./internal/domain/ -run 'RemarkReplies|ReplyToARemark|ReviewNotesForCarries' && go test ./internal/domain/ 2>&1 | tail -5`
Expected: the new tests PASS; the package `ok`.

- [ ] **Step 5: Commit**

Message: `feat(domain): replies to review remarks, joined by fingerprint; review tally`

---

### Task 3: Domain — resolve any thread; every reader stamps the resolution; remark replies stay hidden

**Files:**
- Create: `internal/domain/note_resolve.go`
- Modify: `internal/domain/notes.go` (`NotesFor`, `NotesAt`, sweep predicate
  at `NotesClearAtCommit`), `notes_shelf_entry.go`, `previewnotes.go`,
  `notes_overview.go`, `forge_notes.go`, `notewire.go`
- Test: `internal/domain/note_resolve_test.go`

**Interfaces:**
- Consumes: Task 1 store API, Task 2 `ResolvedNote.Resolution`,
  `Review.RemarkThreads`, `remarkFor`, `docRemarks`.
- Produces:
  - `func (s *Service) NoteResolve(ctx context.Context, id string, resolved bool, by string) (model.ThreadResolution, error)`
    (returns the entry written, or the zero value after an unresolve)
  - `var ErrForgeResolved`, `var ErrNotResolved`
  - `func (s *Service) withResolutions(ctx context.Context, rns []ResolvedNote) []ResolvedNote`
  - `WireNote.Replyable bool`, `ResolvedBy`, `ResolvedAt string`, `Link string`

- [ ] **Step 1: Write the failing tests** (`internal/domain/note_resolve_test.go`; fixtures from Task 2's test file)

```go
func TestNoteResolveOnEveryKindOfRoot(t *testing.T) {
	t.Parallel()
	svc, rid, _ := threadReview(t)
	ctx := context.Background()
	r := mustReview(t, svc, rid)
	addr := model.FileAddress{State: model.StateCommitted, Commit: r.Commit, Path: "g.txt"}
	line, err := svc.NoteAdd(ctx, model.Note{Address: addr, Side: model.NoteSideNew, Range: [2]int{2, 2}, Summary: "mine"})
	if err != nil {
		t.Fatal(err)
	}
	rep, err := svc.NoteReply(ctx, line.ID, model.Note{Summary: "r"})
	if err != nil {
		t.Fatal(err)
	}
	// A reply id resolves its thread.
	if _, err := svc.NoteResolve(ctx, rep.ID, true, "me"); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.NotesAt(ctx, addr)
	if len(got) != 1 || got[0].Resolution == nil || got[0].Resolution.By != "me" {
		t.Fatalf("stored root = %+v", got)
	}
	remark := remarkID(rid, 1)
	if _, err := svc.NoteResolve(ctx, remark, true, "B"); err != nil {
		t.Fatal(err)
	}
	if th, _ := mustReview(t, svc, rid).RemarkThreads(); th[1].Resolution == nil {
		t.Fatalf("remark not resolved: %+v", th)
	}
	if _, err := svc.NoteResolve(ctx, remark, false, "B"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.NoteResolve(ctx, remark, false, "B"); !errors.Is(err, ErrNotResolved) {
		t.Fatalf("second unresolve: %v", err)
	}
	if _, err := svc.NoteResolve(ctx, "forge:5", true, "x"); !errors.Is(err, ErrForgeResolved) {
		t.Fatalf("forge: %v", err)
	}
	if _, err := svc.NoteResolve(ctx, "nope0000", true, "x"); !errors.Is(err, ErrNoteNotFound) {
		t.Fatalf("unknown: %v", err)
	}
	if _, err := svc.NoteResolve(ctx, remarkID(rid, 7), true, "x"); !errors.Is(err, ErrNoSuchRemark) {
		t.Fatalf("remark out of range: %v", err)
	}
	// Removing the stored root takes its resolution.
	if err := svc.NoteRemove(ctx, line.ID); err != nil {
		t.Fatal(err)
	}
	if rs, _ := svc.notesStore(ctx).LoadAllResolved(); slices.ContainsFunc(rs, func(x model.ThreadResolution) bool { return x.Root == line.ID }) {
		t.Fatalf("resolution outlived its root: %+v", rs)
	}
}

func TestRemarkRepliesAreNeverListedOnTheirOwn(t *testing.T) {
	t.Parallel()
	svc, rid, _ := threadReview(t)
	ctx := context.Background()
	before, err := svc.NoteCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.NoteReply(ctx, remarkID(rid, 0), model.Note{Summary: "x"}); err != nil {
		t.Fatal(err)
	}
	after, _ := svc.NoteCounts(ctx) // NoteReply invalidated the cache
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("counts changed: %+v → %+v", before, after)
	}
	ov, _ := svc.NotesOverview(ctx)
	var files []NoteFileNotes
	files = append(append(append(files, ov.Unstaged...), ov.Staged...), ov.Untracked...)
	for _, c := range ov.Commits {
		files = append(files, c.Files...)
	}
	for _, f := range files {
		for _, n := range f.Notes {
			for _, x := range append([]ResolvedNote{n}, n.Replies...) {
				if x.Note.IsRemarkReply() {
					t.Fatalf("overview lists a remark reply: %+v", x)
				}
			}
		}
	}
	addrs, _ := svc.NoteAddresses(ctx)
	for _, a := range addrs {
		ns, _ := svc.NotesAt(ctx, a)
		for _, n := range ns {
			if n.Note.IsRemarkReply() {
				t.Fatalf("NotesAt lists a remark reply: %+v", n)
			}
		}
	}
}
```

Imports: `reflect`, `strings` besides Task 2's. If a forge test
helper exists (`forge_notes_test.go`), add one case: a forge comment
tagged `resolved` comes out of `PreviewNotesAt` with a non-nil
`Resolution`.

- [ ] **Step 2: Run — expect compile failure** (`NoteResolve` undefined)

Run: `cd /work/gigagit/.claude/worktrees/review-answers && go test ./internal/domain/ -run 'NoteResolveOn|NeverListedOnTheirOwn'`

- [ ] **Step 3: Implement `internal/domain/note_resolve.go`**

```go
package domain

import (
	"context"
	"errors"
	"fmt"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notes"
)

// ErrForgeResolved refuses a local resolve of a forge thread: GitHub owns
// that state until gg can publish (notes ↔ forge strategy).
var ErrForgeResolved = errors.New("forge threads are resolved on GitHub")

// ErrNotResolved is an unresolve of an open thread.
var ErrNotResolved = errors.New("thread is not resolved")

// NoteResolve marks the thread of id resolved (or open again). id may be a
// thread root, a reply (its thread), or a review remark.
func (s *Service) NoteResolve(ctx context.Context, id string, resolved bool, by string) (model.ThreadResolution, error) {
	if model.IsForgeNoteID(id) {
		return model.ThreadResolution{}, ErrForgeResolved
	}
	st := s.notesStore(ctx)
	if st == nil {
		return model.ThreadResolution{}, ErrNotesDisabled
	}
	root, fp, err := s.threadRoot(ctx, st, id)
	if err != nil {
		return model.ThreadResolution{}, err
	}
	if !resolved {
		if err := st.Unresolve(root); err != nil {
			if errors.Is(err, notes.ErrNotFound) {
				return model.ThreadResolution{}, ErrNotResolved
			}
			return model.ThreadResolution{}, err
		}
		s.invalidateNoteCounts()
		return model.ThreadResolution{}, nil
	}
	e := model.ThreadResolution{Root: root, By: by, At: notes.Now().UTC(), RemarkFP: fp}
	if err := st.Resolve(e); err != nil {
		if errors.Is(err, notes.ErrNotFound) {
			return model.ThreadResolution{}, ErrNoteNotFound
		}
		return model.ThreadResolution{}, err
	}
	s.invalidateNoteCounts()
	return e, nil
}

// threadRoot is the root id a resolution is keyed by, and the remark
// fingerprint for a remark thread.
func (s *Service) threadRoot(ctx context.Context, st notes.Store, id string) (string, string, error) {
	if rid, n, ok := model.ParseReviewNoteID(id); ok {
		r, err := s.Review(ctx, rid)
		if err != nil {
			return "", "", err
		}
		fps := r.remarkFPs()
		if n >= len(fps) {
			return "", "", fmt.Errorf("%w: review %s has no remark %d", ErrNoSuchRemark, rid, n)
		}
		return id, fps[n], nil
	}
	all, err := st.LoadAll()
	if err != nil {
		return "", "", err
	}
	for _, n := range all {
		if n.ID != id {
			continue
		}
		switch {
		case n.IsRemarkReply(): // its remark's thread, keyed as made
			return n.ParentID, n.RemarkFP, nil
		case n.IsReply():
			return n.ParentID, "", nil
		}
		return n.ID, "", nil
	}
	return "", "", ErrNoteNotFound
}

// withResolutions stamps every stored root of rns with its resolution and
// every forge root with GitHub's flag. Remark roots carry theirs already
// (reviewDocNotes). One store read per call.
func (s *Service) withResolutions(ctx context.Context, rns []ResolvedNote) []ResolvedNote {
	if len(rns) == 0 {
		return rns
	}
	var byRoot map[string]model.ThreadResolution
	if st := s.notesStore(ctx); st != nil {
		if rs, err := st.LoadAllResolved(); err == nil || len(rs) > 0 {
			byRoot = make(map[string]model.ThreadResolution, len(rs))
			for _, r := range rs {
				byRoot[r.Root] = r
			}
		}
	}
	for i := range rns {
		n := rns[i].Note
		switch {
		case n.Source == model.NoteSourceForge:
			if model.NoteHasTag(n, model.NoteTagResolved) {
				rns[i].Resolution = &model.ThreadResolution{Root: n.ID}
			}
		case rns[i].Resolution == nil:
			if r, ok := byRoot[n.ID]; ok {
				rns[i].Resolution = &r
			}
		}
	}
	return rns
}
```

Wire it at every public thread reader's return: `NotesFor`, `NotesAt`,
`ShelfNotes`, `PreviewNotesFor`, `PreviewNotesAt`, `PreviewNotesAll` (each
map value), `NotesOverview` (each `NoteFileNotes.Notes`). Pattern:
`return s.withResolutions(ctx, out), nil`.

Hide remark replies from the stored-notes readers:
- `notes_overview.go`: `if review[n.ID] || review[n.StoredParent()] {`
- `previewnotes.go` (the `mine` loop): first line of the loop body
  `if n.IsRemarkReply() { continue }`
- `notes.go` `NotesClearAtCommit` sweep predicate:
  `return !roots[n.ID] && !roots[n.StoredParent()]`
- `NoteCounts`: replies are already skipped — the test pins it.

`notewire.go` — add fields and fill them:

```go
	// Replyable: a read-only root that still takes replies (a review remark).
	Replyable  bool   `json:"replyable,omitempty"`
	ResolvedBy string `json:"resolved_by,omitempty"`
	ResolvedAt string `json:"resolved_at,omitempty"` // RFC 3339
	// Link: an address the note points at (a reply's fix), gg:// or a sha.
	Link string `json:"link,omitempty"`
```

In `ToWireNote`, after the forge block:

```go
	if r.Resolution != nil || (r.Note.Source == model.NoteSourceForge && model.NoteHasTag(r.Note, model.NoteTagResolved)) {
		w.Resolved = true
	}
	if r.Resolution != nil {
		w.ResolvedBy = r.Resolution.By
		if !r.Resolution.At.IsZero() {
			w.ResolvedAt = r.Resolution.At.UTC().Format(time.RFC3339)
		}
	}
	if model.IsReviewNoteID(r.Note.ID) {
		w.ReadOnly, w.Replyable = true, true
	}
	w.Link = r.Note.Link
```

(remove the old forge-only `w.Resolved = …` line — `withResolutions` now
stamps forge threads, so the flag comes from `Resolution` for every kind;
keep a regression test that a forge `resolved` tag still yields
`"resolved":true` on the wire — `forge_notes_test.go` / `notewire_test.go`
has the existing assertion, it must still pass.)

- [ ] **Step 4: Remove the `t.Skip("Task 3")` from
`TestRemarkRepliesFollowAMovedRemark`; run the package — expect PASS**

Run: `cd /work/gigagit/.claude/worktrees/review-answers && go test ./internal/domain/ 2>&1 | tail -5`
Expected: `ok`.

- [ ] **Step 5: Commit** — `feat(domain): resolve any note thread; readers stamp resolutions; remark replies stay in their review`

---

### Task 4: Domain + notebatch — batch replies with `link` and `resolve`

**Files:**
- Modify: `internal/notebatch/notebatch.go` (`rawComment`, `Item`)
- Modify: `internal/domain/notebatch_plan.go`
- Test: `internal/notebatch/notebatch_test.go`, `internal/domain/notebatch_plan_test.go`

**Interfaces:**
- Consumes: `NoteReply` (Task 2), `NoteResolve` (Task 3).
- Produces: `notebatch.Item.Link string`, `notebatch.Item.Resolve *bool`;
  `PlannedNote.Resolve *bool` (Link rides on `PlannedNote.Note.Link`).

- [ ] **Step 1: Failing parser test** (`notebatch_test.go`)

```go
func TestCommentReplyCarriesLinkAndResolve(t *testing.T) {
	b, err := Parse([]byte(`{"comments":[{"replyTo":"review:ab12cd34:1","summary":"fixed","link":"gg://r@abc:a.go:3","resolve":true}]}`))
	if err != nil {
		t.Fatal(err)
	}
	it := b.Items[0]
	if it.Link != "gg://r@abc:a.go:3" || it.Resolve == nil || !*it.Resolve {
		t.Fatalf("item = %+v", it)
	}
	if _, err := Parse([]byte(`{"comments":[{"filePath":"a.go","newLine":1,"summary":"x","resolve":true}]}`)); err == nil ||
		!strings.Contains(err.Error(), "resolve") {
		t.Fatalf("resolve on a root comment: %v", err)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/notebatch/ -run CommentReplyCarries` — Expected FAIL (`it.Link undefined`).

- [ ] **Step 3: Implement** — `rawComment` gains
`Link *string \`json:"link"\`` and `Resolve *bool \`json:"resolve"\``;
`Item` gains `Link string` and `Resolve *bool` (doc: "set only on a reply:
the thread's state after the reply"). In the comment loop: `it.Link =
trimPtr(c.Link)`, `it.Resolve = c.Resolve`; in the root branch (no
`replyTo`), `if c.Resolve != nil { return Batch{}, fmt.Errorf("%s: resolve
applies to a replyTo comment only", where) }`. A root comment may carry
`link`.

- [ ] **Step 4: Run** the notebatch package — Expected `ok`.

- [ ] **Step 5: Failing domain test** (`notebatch_plan_test.go`)

```go
func TestBatchAnswersAReview(t *testing.T) {
	t.Parallel()
	svc, rid, _ := threadReview(t)
	ctx := context.Background()
	yes := true
	b := notebatch.Batch{Items: []notebatch.Item{
		{ReplyTo: remarkID(rid, 0), Summary: "agreed", Resolve: &yes},
		{ReplyTo: remarkID(rid, 1), Summary: "fixed", Link: "HEAD"},
	}}
	planned, _, err := svc.PlanNoteBatch(ctx, b, false, "", "B", NoteSideBoth)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApplyNoteBatch(ctx, planned); err != nil {
		t.Fatal(err)
	}
	r, _ := svc.Review(ctx, rid)
	th, _ := r.RemarkThreads()
	if th[0].Resolution == nil || len(th[1].Replies) != 1 || len(th[1].Replies[0].Link) != 40 {
		t.Fatalf("threads = %+v", th)
	}
	// A bad replyTo fails the PLAN, before any write.
	bad := notebatch.Batch{Items: []notebatch.Item{{ReplyTo: remarkID(rid, 9), Summary: "x"}}}
	if _, _, err := svc.PlanNoteBatch(ctx, bad, false, "", "B", NoteSideBoth); !errors.Is(err, ErrNoSuchRemark) {
		t.Fatalf("plan: %v", err)
	}
}

// failResolveStore fails Resolve for one root: the seam that reaches the
// resolve-failure rollback (a reply failure never gets that far).
type failResolveStore struct {
	notes.Store
	failRoot string
}

func (f *failResolveStore) Resolve(r model.ThreadResolution) error {
	if r.Root == f.failRoot {
		return errors.New("disk full")
	}
	return f.Store.Resolve(r)
}

func TestBatchResolveFailureRestoresAndRollsBack(t *testing.T) {
	t.Parallel()
	svc, rid, _ := threadReview(t)
	ctx := context.Background()
	// Remark 0 is already resolved by A before the batch.
	if _, err := svc.NoteResolve(ctx, remarkID(rid, 0), true, "A"); err != nil {
		t.Fatal(err)
	}
	yes, no := true, false
	planned, _, err := svc.PlanNoteBatch(ctx, notebatch.Batch{Items: []notebatch.Item{
		{ReplyTo: remarkID(rid, 0), Summary: "reopen", Resolve: &no},
		{ReplyTo: remarkID(rid, 1), Summary: "done", Resolve: &yes},
	}}, false, "", "B", NoteSideBoth)
	if err != nil {
		t.Fatal(err)
	}
	svc.SetNotesStore(&failResolveStore{Store: svc.notesStore(ctx), failRoot: remarkID(rid, 1)})
	if _, err := svc.ApplyNoteBatch(ctx, planned); err == nil || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("expected a rolled-back failure, got %v", err)
	}
	th, _ := mustReview(t, svc, rid).RemarkThreads()
	if len(th[0].Replies) != 0 || len(th[1].Replies) != 0 {
		t.Fatalf("the batch's replies survived: %+v", th)
	}
	if th[0].Resolution == nil || th[0].Resolution.By != "A" {
		t.Fatalf("remark 0 must be resolved by A again: %+v", th[0].Resolution)
	}
	if th[1].Resolution != nil {
		t.Fatalf("remark 1 must stay open: %+v", th[1].Resolution)
	}
}
```

The test file uses `threadReview`, `remarkID`, `mustReview` from Task 2's
`review_threads_test.go` (same package).

- [ ] **Step 6: Run** `go test ./internal/domain/ -run 'BatchAnswers|BatchResolveFailure'` — Expected FAIL (`Resolve` field undefined on PlannedNote).

- [ ] **Step 7: Implement** in `notebatch_plan.go`:

```go
type PlannedNote struct {
	Note    model.Note
	ReplyTo string
	// Resolve, on a reply, sets its thread's state once every reply of the
	// batch is stored.
	Resolve *bool
}
```

In `PlanNoteBatchIn`, set `n.Link = it.Link` for every item, and replace the
reply-parent validation with:

```go
		if it.ReplyTo != "" {
			if err := s.checkReplyParent(ctx, it.ReplyTo); err != nil {
				return nil, 0, fmt.Errorf("item %d: replyTo %s: %w", i, it.ReplyTo, err)
			}
			planned = append(planned, PlannedNote{Note: n, ReplyTo: it.ReplyTo, Resolve: it.Resolve})
			continue
		}
```

```go
// checkReplyParent validates a reply's parent without writing: a stored
// note, or a remark of a review this store holds; forge ids are refused.
func (s *Service) checkReplyParent(ctx context.Context, id string) error {
	if rid, n, ok := model.ParseReviewNoteID(id); ok {
		r, err := s.Review(ctx, rid)
		if err != nil {
			return err
		}
		if n >= len(r.remarkFPs()) {
			return fmt.Errorf("%w: review %s has no remark %d", ErrNoSuchRemark, rid, n)
		}
		return nil
	}
	if model.IsReadOnlyNoteID(id) {
		return ErrReadOnlyNote
	}
	_, err := s.NoteGet(ctx, id)
	return err
}
```

`ApplyNoteBatch`: after the store loop, apply resolutions, remembering
what each changed so a failure can restore it:

```go
	type undo struct {
		id   string
		prev *model.ThreadResolution // nil = was open
	}
	var undos []undo
	for i, p := range planned {
		if p.Resolve == nil {
			continue
		}
		prev := s.resolutionOf(ctx, out[i].ID)
		if _, err := s.NoteResolve(ctx, out[i].ID, *p.Resolve, out[i].Author); err != nil && !errors.Is(err, ErrNotResolved) {
			for j := len(undos) - 1; j >= 0; j-- {
				s.restoreResolution(ctx, undos[j].id, undos[j].prev)
			}
			removed, failed := s.rollbackNoteBatch(ctx, out)
			if failed > 0 {
				return nil, fmt.Errorf("item %d: %w (rollback incomplete: %d could not be removed)", i, err, failed)
			}
			return nil, fmt.Errorf("item %d: %w (rolled back %d notes)", i, err, removed)
		}
		undos = append(undos, undo{out[i].ID, prev})
	}
	return out, nil
```

with, in `note_resolve.go`:

```go
// resolutionOf is the thread of id's current resolution (nil = open or
// unknown).
func (s *Service) resolutionOf(ctx context.Context, id string) *model.ThreadResolution {
	st := s.notesStore(ctx)
	if st == nil {
		return nil
	}
	root, _, err := s.threadRoot(ctx, st, id)
	if err != nil {
		return nil
	}
	rs, _ := st.LoadAllResolved()
	for _, r := range rs {
		if r.Root == root {
			return &r
		}
	}
	return nil
}

// restoreResolution puts a thread back to prev (nil = open). Best effort.
func (s *Service) restoreResolution(ctx context.Context, id string, prev *model.ThreadResolution) {
	st := s.notesStore(ctx)
	if st == nil {
		return
	}
	root, _, err := s.threadRoot(ctx, st, id)
	if err != nil {
		return
	}
	if prev == nil {
		_ = st.Unresolve(root)
		return
	}
	_ = st.Resolve(*prev)
}
```

`ApplyNoteBatch` indexes `out[i]` by planned index — it already appends one
stored note per planned item in order, so `out[i]` is item `i`.

- [ ] **Step 8: Run** `go test ./internal/notebatch/ ./internal/domain/ 2>&1 | tail -5` — Expected `ok` ×2.

- [ ] **Step 9: Commit** — `feat(notes): a note batch answers a review — replyTo a remark, link, resolve`

---

### Task 5: CLI — `gg note reply --link`, `gg note resolve|unresolve`, `gg review show` threads; `--review --no-fingerprint`

**Files:**
- Modify: `internal/cli/note.go` (dispatch, `noteReply`, new `noteResolve`,
  `noteMutations`, usage lines)
- Modify: `internal/cli/review_show.go` (`printReviewShow`), `internal/cli/link*.go` (the `--review` flag check)
- Modify: `internal/domain/note_resolve.go` (`NoteLink`, `ErrNoteLink`), `internal/domain/notes.go` (`NoteAdd`/`NoteReply` normalise `Link`)
- Modify: `internal/domain/review_link.go` (`ReviewShow`/`ReviewShowRemark` carry threads)
- Test: `internal/cli/note_resolve_test.go`, `internal/cli/review_show_test.go`

**Interfaces:**
- Consumes: `NoteReply`, `NoteResolve`, `Review.RemarkThreads`, `Tally`.
- Produces:
  - `ReviewShowRemark.ID string` (`json:"id"`), `.Resolved bool`,
    `.ResolvedBy string`, `.Replies []ReviewShowReply`
  - `type ReviewShowReply struct{ ID, Author, Summary, Rationale, Link string; Created time.Time }` (json snake: `id, author, summary, rationale, link, created`)
  - `ReviewShow.Outdated []ReviewShowOutdated` (`json:"outdated,omitempty"`),
    `ReviewShow.Resolved int` (`json:"resolved"`)
  - `type ReviewShowOutdated struct{ Root, Summary string; Resolved bool; Replies []ReviewShowReply }`

- [ ] **Step 1: Failing CLI tests** (`internal/cli/note_resolve_test.go`; read
`review_show_test.go` first and reuse its setup that stores a review with
remarks and yields its id `rid` in a temp repo `dir`)

```go
func TestNoteResolveAndReplyOnARemark(t *testing.T) {
	dir, rid := reviewedRepo(t) // review_show_test.go: two remarks on a.txt
	remark := "review:" + rid + ":0"
	if code, _, errb := runCLI(t, dir, "note", "reply", remark, "--summary", "fixed", "--link", "HEAD"); code != 0 {
		t.Fatalf("reply: %s", errb)
	}
	if code, out, errb := runCLI(t, dir, "note", "resolve", remark, "--json"); code != 0 || !strings.Contains(out, `"root"`) {
		t.Fatalf("resolve: %d %s %s", code, out, errb)
	}
	code, out, _ := runCLI(t, dir, "review", "show", rid)
	if code != 0 || !strings.Contains(out, "resolved by") || !strings.Contains(out, "fixed") ||
		!strings.Contains(out, "1 of 2 resolved") || !strings.Contains(out, remark) {
		t.Fatalf("show:\n%s", out)
	}
	code, out, _ = runCLI(t, dir, "review", "show", "--json", rid)
	var rs domain.ReviewShow
	if code != 0 || json.Unmarshal([]byte(out), &rs) != nil || !rs.Remarks[0].Resolved ||
		len(rs.Remarks[0].Replies) != 1 || len(rs.Remarks[0].Replies[0].Link) != 40 || rs.Resolved != 1 {
		t.Fatalf("json: %s", out)
	}
	if code, _, errb := runCLI(t, dir, "note", "unresolve", remark); code != 0 {
		t.Fatalf("unresolve: %s", errb)
	}
	if code, _, errb := runCLI(t, dir, "note", "unresolve", remark); code != 1 || !strings.Contains(errb, "not resolved") {
		t.Fatalf("second unresolve: %d %s", code, errb)
	}
	if code, _, errb := runCLI(t, dir, "note", "reply", remark, "--summary", "x", "--link", "not a rev"); code == 0 || !strings.Contains(errb, "link") {
		t.Fatalf("bad link: %d %s", code, errb)
	}
	if code, _, errb := runCLI(t, dir, "note", "resolve", "forge:3"); code != 1 || !strings.Contains(errb, "GitHub") {
		t.Fatalf("forge: %d %s", code, errb)
	}
}

func TestLinkReviewRefusesNoFingerprint(t *testing.T) {
	dir, rid := reviewedRepo(t)
	if code, _, errb := runCLI(t, dir, "link", "--review", rid, "--no-fingerprint"); code != 2 || !strings.Contains(errb, "--no-fingerprint") {
		t.Fatalf("got %d %s", code, errb)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/cli/ -run 'NoteResolveAndReply|LinkReviewRefuses'` — Expected FAIL (unknown subcommand "resolve").

- [ ] **Step 3: Implement**

`note.go`:
- usage line → `gg note <add|reply|resolve|unresolve|rm|list|clear|apply> ...`
- dispatch: `case "resolve", "unresolve": return noteResolve(svc, sub == "resolve", rest, stdout, stderr)`
- `noteLinkShape`: `case "reply", "rm", "resolve", "unresolve":`
- `noteMutations` gains `"resolve": true, "unresolve": true`
- `noteReply` gains `link := fs.String("link", "", "a gg:// link or a commit the reply points at (e.g. the fix)")`
  and passes `Link: strings.TrimSpace(*link)` in the `model.Note`; a bad
  link comes back from `NoteReply` as `domain.ErrNoteLink` → print
  `note reply: <err>` and exit 2 (check it before `noteIDExit`).
- **Domain (this task):** in `internal/domain/note_resolve.go` add

```go
// ErrNoteLink is a note link that is neither a gg:// link nor a commit.
var ErrNoteLink = errors.New("link is neither a gg:// link nor a commit")

// NoteLink normalises a note's link: a gg:// link that parses stays as
// written, a revision becomes its full commit sha. "" stays "".
func (s *Service) NoteLink(ctx context.Context, v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", nil
	}
	if strings.HasPrefix(v, "gg://") {
		if _, err := model.ParseLink(v); err != nil {
			return "", fmt.Errorf("%w: %v", ErrNoteLink, err)
		}
		return v, nil
	}
	full, ok, err := s.ResolveRev(ctx, v+"^{commit}")
	if err != nil || !ok {
		return "", fmt.Errorf("%w: %q", ErrNoteLink, v)
	}
	return strings.TrimSpace(full), nil
}
```

  and call it at the top of `NoteAdd`, `NoteReply` (before the remark
  branch) when `n.Link != ""`: `if n.Link, err = s.NoteLink(ctx, n.Link); err != nil { return model.Note{}, err }`.
  A domain test: `NoteReply(…, Link: "HEAD")` stores 40 hex;
  `Link: "nope"` → `errors.Is(err, ErrNoteLink)`. (`ResolveRev` with
  `^{commit}`: check its signature handles the suffix — `resolveCommitCmd`
  in the TUI peels the same way via `RevParse`; use whichever domain call
  returns a full sha for `HEAD^{commit}`.)

```go
func noteResolve(svc *domain.Service, resolve bool, args []string, stdout, stderr io.Writer) int {
	sub := "note unresolve"
	if resolve {
		sub = "note resolve"
	}
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		fmt.Fprintf(stderr, "usage: gg %s [<repo-link>] <note-id|review:<id>:<n>> [--json]\n", sub)
		return 2
	}
	id, rest := args[0], args[1:]
	fs := flag.NewFlagSet(sub, flag.ContinueOnError)
	fs.SetOutput(stderr)
	author := fs.String("author", "", "who resolves (default: $GG_AGENT, else agent)")
	asJSON := fs.Bool("json", false, "print the thread's state as JSON")
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "usage: gg %s [<repo-link>] <note-id> [--json]\n", sub)
		return 2
	}
	e, err := svc.NoteResolve(context.Background(), id, resolve, noteAuthorDefault(*author))
	switch {
	case errors.Is(err, domain.ErrNotResolved):
		fmt.Fprintf(stderr, "%s: thread %s is not resolved\n", sub, id)
		return 1
	case errors.Is(err, domain.ErrForgeResolved):
		fmt.Fprintf(stderr, "%s: %s is a pull-request thread: it is resolved on GitHub\n", sub, id)
		return 1
	case err != nil:
		return noteIDExit(sub, id, svc, err, stderr)
	}
	if *asJSON {
		return printJSON(stdout, stderr, map[string]any{"id": id, "resolved": resolve, "root": e.Root, "by": e.By})
	}
	if resolve {
		fmt.Fprintf(stdout, "resolved %s\n", id)
	} else {
		fmt.Fprintf(stdout, "reopened %s\n", id)
	}
	return 0
}
```

(Use the package's existing JSON printer — grep `func printJSON\|json.NewEncoder(stdout)` in `internal/cli` and call that; if none matches the signature above, inline `json.NewEncoder(stdout).Encode(...)`.) `noteIDExit` must also map `domain.ErrReviewNotFound` / `domain.ErrNoSuchRemark` to plain exit-1 messages (they fall through to `error: …` today — acceptable; the test only checks forge + unresolve).

`review_link.go` (`ReviewShow`) — new types per Interfaces, filled from
`r.RemarkThreads()`:

```go
	th, outdated := r.RemarkThreads()
	for _, x := range reviewRemarksIn(r, t, repo) {
		rm := ReviewShowRemark{ID: fmt.Sprintf("%s%s:%d", model.ReviewNoteIDPrefix, r.ID, x.N), N: x.N, /* … existing … */}
		if x.N < len(th) {
			if res := th[x.N].Resolution; res != nil {
				rm.Resolved, rm.ResolvedBy = true, res.By
				out.Resolved++
			}
			rm.Replies = showReplies(th[x.N].Replies)
		}
		out.Remarks = append(out.Remarks, rm)
	}
	for _, o := range outdated {
		out.Outdated = append(out.Outdated, ReviewShowOutdated{Root: o.Root, Summary: o.Summary,
			Resolved: o.Resolution != nil, Replies: showReplies(o.Replies)})
	}
```

```go
func showReplies(ns []model.Note) []ReviewShowReply {
	var out []ReviewShowReply
	for _, n := range ns {
		out = append(out, ReviewShowReply{ID: n.ID, Author: n.Author, Summary: n.Summary,
			Rationale: n.Rationale, Link: n.Link, Created: n.Created})
	}
	return out
}
```

JSON tags: `ID json:"id"`, `Resolved json:"resolved"`, `ResolvedBy json:"resolved_by,omitempty"`, `Replies json:"replies,omitempty"`; `ReviewShowReply` fields `id, author, summary, rationale,omitempty, link,omitempty, created`.

`printReviewShow` — after the header line add a tally line when there are
remarks (`fmt.Fprintf(w, "%d of %d resolved\n", rs.Resolved, len(rs.Remarks))`).
The remark line `[%d] %s:%s — %s` stays EXACTLY as it is (the existing
pins in `review_show_test.go` and `e2e/scenarios/s108_review_links.toml`
check it); the id goes on its own line after the link:
`fmt.Fprintln(w, "    id "+r.ID)`. Then:

```go
		if r.Resolved {
			fmt.Fprintf(w, "    resolved by %s\n", orDash(r.ResolvedBy))
		}
		for _, rep := range r.Replies {
			fmt.Fprintf(w, "    ↳ %s: %s\n", orDash(rep.Author), rep.Summary)
			if t := strings.TrimSpace(rep.Rationale); t != "" {
				fmt.Fprintln(w, "      "+strings.ReplaceAll(t, "\n", "\n      "))
			}
			if rep.Link != "" {
				fmt.Fprintln(w, "      → "+rep.Link)
			}
		}
```

and at the end an `Outdated` section (`\nOutdated threads (their remark is
gone from the re-saved review):` then the same reply lines under
`[<root>] — <summary>`). `orDash(s)` returns `"-"` for empty (add it if the
package lacks one). The `↳`/`→` arrows are CLI text, not TUI rows — the
glyph ruling is about row tallies; keep them.

`--review --no-fingerprint`: in the `gg link` flag handling (grep
`no-fingerprint` in `internal/cli/link*.go`), refuse the combination with
exit 2: `link: --no-fingerprint has no meaning with --review (a review link names no line)`.

`--json` stays the parse surface for agents; the text additions are for
humans. If any existing `TestReviewShow*` pin or s108's `stdout_contains`
breaks, the remark line changed — put it back, do not edit the pin.

- [ ] **Step 4: Run** `go test ./internal/cli/ 2>&1 | tail -5` — Expected `ok`.

- [ ] **Step 5: Commit** — `feat(cli): note reply --link, note resolve/unresolve, review show prints threads`

---

### Task 6: MCP — `gg_note_reply`, `gg_note_resolve`, threads in `gg_review_show`

**Files:**
- Modify: `internal/mcp/notes.go`
- Test: `internal/mcp/notes_test.go` (+ any tool-list golden: grep `gg_note_rm` in `internal/mcp/*_test.go`)

**Interfaces:**
- Consumes: `NoteReply`, `NoteResolve`, `ReviewShow` (Task 5 shape — `gg_review_show` returns it as-is, so it carries the threads with no change).
- Produces: tools `gg_note_reply` (in: `id`, `summary`, `rationale`, `link`, `author`) → `{repo, note: WireNote}`; `gg_note_resolve` (in: `id`, `resolved bool`, `author`) → `okOut`.

- [ ] **Step 1: Failing test** — copy the shape of the existing `gg_note_rm` test in `notes_test.go` (it builds a server over a temp repo and calls a tool by name); store a review (reuse the domain-side review setup via `svc.SaveReview` on the server's service), then:

```go
	res := callTool(t, srv, "gg_note_reply", map[string]any{"id": "review:" + rid + ":0", "summary": "agreed", "link": "HEAD"})
	// expect no error, a note whose parent_id is the remark id and link a 40-hex sha
	res = callTool(t, srv, "gg_note_resolve", map[string]any{"id": "review:" + rid + ":0", "resolved": true})
	// expect ok:true; then gg_review_show's remarks[0].resolved == true and len(replies) == 1
	res = callTool(t, srv, "gg_note_resolve", map[string]any{"id": "forge:1", "resolved": true})
	// expect an error mentioning GitHub
```

Use the helper names the file already has for calling a tool and decoding
its structured output.

- [ ] **Step 2: Run** `go test ./internal/mcp/ -run NoteReply` — Expected FAIL (unknown tool).

- [ ] **Step 3: Implement** two `sdk.AddTool` blocks after `gg_note_rm`, same
guard pattern (`repoCheck`, `mutatingAnnotations()`, `s.notifyNotesChanged()`):

```go
	sdk.AddTool(srv, &sdk.Tool{
		Name: "gg_note_reply",
		Description: "Reply to a gg review note thread: a stored note id, or a review remark id " +
			"\"review:<review id>:<n>\" as gg_review_show lists them. link (optional) is a gg:// link or a " +
			"commit the reply points at, e.g. the fix. MUTATES gg's note store.",
		Annotations: mutatingAnnotations(),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in noteReplyIn) (*sdk.CallToolResult, noteOut, error) {
		out := noteOut{Repo: s.repoInfo()}
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		if in.ID == "" || strings.TrimSpace(in.Summary) == "" {
			return nil, out, fmt.Errorf("id and summary are required")
		}
		n, err := s.svc.NoteReply(ctx, in.ID, model.Note{Source: model.NoteSourceAgent, Author: s.author(in.Author),
			Summary: strings.TrimSpace(in.Summary), Rationale: strings.TrimSpace(in.Rationale), Link: in.Link})
		if err != nil {
			return nil, out, err
		}
		out.Note = domain.ToWireNote(domain.ResolvedNote{Note: n, Status: model.NoteActive, Range: n.Range})
		s.notifyNotesChanged()
		return nil, out, nil
	})

	sdk.AddTool(srv, &sdk.Tool{
		Name: "gg_note_resolve",
		Description: "Resolve (resolved:true) or reopen (resolved:false) a gg note thread by any of its ids, " +
			"including a review remark id \"review:<review id>:<n>\". Pull-request threads are resolved on " +
			"GitHub and refuse. MUTATES gg's note store.",
		Annotations: mutatingAnnotations(),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in noteResolveIn) (*sdk.CallToolResult, okOut, error) {
		out := okOut{Repo: s.repoInfo()}
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		if in.ID == "" {
			return nil, out, fmt.Errorf("id is required")
		}
		if _, err := s.svc.NoteResolve(ctx, in.ID, in.Resolved, s.author(in.Author)); err != nil {
			return nil, out, err
		}
		out.OK = true
		s.notifyNotesChanged()
		return nil, out, nil
	})
```

Input types (next to `noteRmIn`):

```go
type noteReplyIn struct {
	ID        string `json:"id" jsonschema:"the note id or review remark id (review:<id>:<n>) to reply to"`
	Summary   string `json:"summary" jsonschema:"the reply"`
	Rationale string `json:"rationale,omitempty" jsonschema:"the why, optional"`
	Link      string `json:"link,omitempty" jsonschema:"a gg:// link or a commit the reply points at"`
	Author    string `json:"author,omitempty" jsonschema:"author label (default: the agent)"`
}

type noteResolveIn struct {
	ID       string `json:"id" jsonschema:"any id of the thread: root, reply or review remark"`
	Resolved bool   `json:"resolved" jsonschema:"true resolves, false reopens"`
	Author   string `json:"author,omitempty" jsonschema:"who resolves (default: the agent)"`
}
```

`noteOut` / `s.author`: reuse what `gg_note_add` uses for its output
struct and author default (read its block at `notes.go:152`). The link is
validated by `NoteReply` (`domain.NoteLink`). If a tool-list
golden or count exists, add the two names.

- [ ] **Step 4: Run** `go test ./internal/mcp/ 2>&1 | tail -3` — Expected `ok`.

- [ ] **Step 5: Commit** — `feat(mcp): gg_note_reply and gg_note_resolve; gg_review_show carries the threads`

---
### Task 7: TUI — reply to a remark, `x` resolve / reopen, resolved threads fold, link rows; reviewHintMsg gen guard

**Files:**
- Modify: `internal/tui/note_popup.go` (`openNotePopup` review-view gate), `internal/tui/note_keys.go`
  (`noteTarget.resolved`, `replyableNoteTargets`, `withReplyableNoteTarget`, `noteMenuRows`,
  new `toggleThreadResolved`, `threadResolvedMsg`, `noteLinkRows`), `internal/tui/diff_notes.go`
  (`seedCollapsed`, `noteBoxTitle`, `collapsedNoteLine`, `noteBodyLines` link row),
  `internal/tui/diff_view.go` (`x` key), `internal/tui/model.go` (`threadResolvedMsg` case),
  `internal/tui/help.go`, `internal/tui/review_hint.go` (+ the `reviewHintMsg` dispatch),
  `internal/i18n/lang/{ja,ko,zh,ru}.toml`
- Test: `internal/tui/note_resolve_test.go`, `internal/tui/review_hint_test.go` (or the file that tests `onReviewHint` today — grep `reviewHintMsg` in `internal/tui/*_test.go`)

**Interfaces:**
- Consumes: `domain.ResolvedNote.Resolution` (Tasks 2–3), `svc.NoteResolve`, `svc.NoteReply` (remark ids),
  `model.Note.Link`, `model.IsReviewNoteID`.
- Produces: `func (m Model) toggleThreadResolved() (tea.Model, tea.Cmd)`; `type threadResolvedMsg struct{ root string; resolved bool; err error }`.

Rulings for this task (ledger them):
- E and Delete keep today's target rule (the thread's visible ROOT): on a remark
  root they refuse with the existing read-only notice. A reply — a remark reply
  included — is edited or removed in gg web or with `gg note rm`, exactly as
  stored replies are today. Only R (reply) and `x` (resolve) are new on remarks.
- The diff cursor walks diff lines, never note rows, so "enter on the link line"
  becomes a `.` menu row **"Open link: <link>"** per link of a thread in reach
  (root or reply), which opens it through the `#` prompt (`openGotoCommitPopup`
  pre-filled + `submit`): a gg:// link navigates, a sha opens the commit — the
  prompt's own error and cross-checkout confirm come for free.

- [ ] **Step 1: Failing tests** (`internal/tui/note_resolve_test.go`). Read
`note_collapse_test.go` first: it builds a `diffView` with notes through
`setNotes` and drives keys; copy its setup helper. Tests:

```go
func TestResolvedThreadsStartFoldedOnce(t *testing.T) {
	t.Parallel()
	v := &diffView{}
	root := domain.ResolvedNote{Note: model.Note{ID: "n1", Summary: "s"}, Range: [2]int{1, 1},
		Resolution: &model.ThreadResolution{Root: "n1", By: "B"}}
	v.setNotes([]domain.ResolvedNote{root})
	if !v.collapsed["n1"] {
		t.Fatal("a resolved stored thread must start folded")
	}
	v.collapsed["n1"] = false // the user unfolded it
	v.setNotes([]domain.ResolvedNote{root})
	if v.collapsed["n1"] {
		t.Fatal("seeding is once per view: a re-read must not refold")
	}
}

func TestCollapsedResolvedThreadSaysResolved(t *testing.T) {
	t.Parallel()
	v := &diffView{}
	r := domain.ResolvedNote{Note: model.Note{ID: "n1", Author: "B", Summary: "agreed"}, Range: [2]int{1, 1},
		Resolution: &model.ThreadResolution{Root: "n1"}}
	if got := v.collapsedNoteLine(r).text; !strings.Contains(got, i18n.T("resolved")) {
		t.Fatalf("collapsed row = %q", got)
	}
}

func TestXResolvesTheThreadAtTheCursor(t *testing.T) {
	t.Parallel()
	// A diff with one stored thread on the cursor line (the note_collapse_test
	// setup), svc a real domain service over a temp repo holding that note.
	m, noteID := diffWithStoredNote(t) // write it from note_collapse_test.go's helper + a real NoteAdd
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	msg := cmd() // runs svc.NoteResolve
	rm, ok := msg.(threadResolvedMsg)
	if !ok || rm.err != nil || rm.root != noteID || !rm.resolved {
		t.Fatalf("msg = %#v", msg)
	}
	m2, _ := next.(Model).Update(rm)
	if !m2.(Model).diffLayer().collapsed[noteID] {
		t.Fatal("resolving folds the thread in place")
	}
}

func TestXOnAForgeThreadSaysGitHub(t *testing.T) {
	t.Parallel()
	m := diffWithForgeNote(t) // a forge note on the cursor line (forge_notes_test.go has a builder)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if cmd != nil {
		t.Fatal("no store call for a forge thread")
	}
	if got := next.(Model).diffNotice; !strings.Contains(got, i18n.T("resolved on GitHub")) {
		t.Fatalf("notice = %q", got)
	}
}

func TestRInTheReviewViewRepliesToARemark(t *testing.T) {
	t.Parallel()
	m := reviewDiffWithRemark(t) // dv.reviewID set, one remark "review:<id>:0" on the cursor line
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	p := layerOf[*notePopup](next.(Model))
	if p == nil || p.mode != noteReply || !model.IsReviewNoteID(p.targetID) {
		t.Fatalf("popup = %#v", p)
	}
	// c (add) and E (edit) still refuse in the review view.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if layerOf[*notePopup](next.(Model)) != nil {
		t.Fatal("c must still refuse in the review view")
	}
}
```

Write the three fixture builders (`diffWithStoredNote`, `diffWithForgeNote`,
`reviewDiffWithRemark`) in this test file from the existing helpers
(`note_collapse_test.go`, `forge_notes_test.go`, `review_note_view_test.go`);
`reviewDiffWithRemark` sets `dv.reviewID` and the remark's
`domain.ResolvedNote{Note: model.Note{ID: "review:abcd1234:0", Source: model.NoteSourceAgent, ...}}`.

- [ ] **Step 2: Run** `go test ./internal/tui/ -run 'ResolvedThreads|CollapsedResolved|XResolves|XOnAForge|RInTheReviewView'` — Expected FAIL (compile: `threadResolvedMsg` undefined).

- [ ] **Step 3: Implement**

`diff_notes.go`:

```go
// seedCollapsed folds a RESOLVED thread (stored, review remark or forge)
// the first time this view sees it; a later re-read never refolds one the
// user opened.
		if r.Resolution != nil || (r.Note.Source == model.NoteSourceForge && model.NoteHasTag(r.Note, model.NoteTagResolved)) {
			v.collapsed[r.Note.ID] = true
		}
```

`collapsedNoteLine`: after the replies count, `if r.Resolution != nil { text += " · " + i18n.T("resolved") }`.
`noteBoxTitle`: for a non-forge root with `r.Resolution != nil` append
`" · " + i18n.T("resolved")` (forge titles already say it via `forgeNoteTitle`).
`noteBodyLines`: after a note's text rows, when `n.Link != ""` append one
`noteRowText` row `indent + "  → " + sanitizeLine(n.Link)` (cut with the
row helper the function already uses for long text).

`note_keys.go`:

```go
// noteTarget gains:
	resolved bool // the thread is resolved (its Resolution is set)

// in noteTargetIn: t.resolved = r.Resolution != nil

// replyableNoteTargets: the threads R may answer — every stored thread and a
// review's remarks; forge threads stay read-only.
func replyableNoteTargets(ts []noteTarget) []noteTarget {
	out := ts[:0:0]
	for _, t := range ts {
		if t.note.Source != model.NoteSourceForge && !model.IsForgeNoteID(t.rootID) {
			out = append(out, t)
		}
	}
	return out
}

// resolvableNoteTargets: the threads x may resolve — the same set.
func resolvableNoteTargets(ts []noteTarget) []noteTarget { return replyableNoteTargets(ts) }

// toggleThreadResolved resolves the thread in reach, or reopens it.
func (m Model) toggleThreadResolved() (tea.Model, tea.Cmd) {
	all := m.notesAtCursor()
	ts := resolvableNoteTargets(all)
	if len(ts) == 0 {
		if len(all) > 0 {
			m.statusMsg = i18n.T("resolved on GitHub")
			m.diffNotice = m.statusMsg
		}
		return m, nil
	}
	return m.withNoteTargetIn(ts, func(m Model, t noteTarget) (tea.Model, tea.Cmd) {
		svc, root, want, who := m.svc, t.rootID, !t.resolved, m.noteAuthor()
		return m, func() tea.Msg {
			_, err := svc.NoteResolve(context.Background(), root, want, who)
			return threadResolvedMsg{root: root, resolved: want, err: err}
		}
	})
}

// threadResolvedMsg is a resolve / reopen's result: on success the thread
// folds (resolved) or unfolds (reopened) and the notes reload.
type threadResolvedMsg struct {
	root     string
	resolved bool
	err      error
}
```

(`m.noteAuthor()`: use whatever the note popup uses for its author — grep
`author:` in `note_popup.go`'s open path; if it is a field set from config,
call the same function.)

`openNotePopup` (`note_popup.go:55`): refuse in the review view only for
`noteAdd` and `noteEdit`; for `noteReply` route through
`m.withNoteTargetIn(replyableNoteTargets(m.notesAtCursor()), …openNotePopupFor…)`
(outside the review view too — a stored thread is in both sets, so nothing
changes there). `noteMenuRows`: when `editableNoteTargets` is empty but
`replyableNoteTargets` is not, return just the `"note-reply"` row; always
append, when `resolvableNoteTargets` is non-empty, a row
`{id: "note-resolve", key: "x", label: i18n.T("Resolve thread")}` — or
`i18n.T("Reopen thread")` when the first target is resolved — whose `run`
is `toggleThreadResolved`; then the link rows:

```go
// noteLinkRows: one "Open link: …" row per link carried by a thread in
// reach (its root or a reply), opened through the # prompt.
func (m Model) noteLinkRows() []actionRow {
	var rows []actionRow
	seen := map[string]bool{}
	v := m.diffLayer()
	for _, t := range m.notesAtCursor() {
		for _, r := range v.notes {
			if r.Note.ID != t.rootID {
				continue
			}
			for _, n := range append([]domain.ResolvedNote{r}, r.Replies...) {
				if l := n.Note.Link; l != "" && !seen[l] {
					seen[l] = true
					link := l
					rows = append(rows, actionRow{id: "note-link", label: i18n.T("Open link: %s", truncate(link, 48)),
						run: func(m Model) (tea.Model, tea.Cmd) { return m.openNoteLink(link) }})
				}
			}
		}
	}
	return rows
}

// openNoteLink opens a note's link through the # prompt: the same resolve,
// errors and cross-checkout confirm as a pasted link.
func (m Model) openNoteLink(link string) (tea.Model, tea.Cmd) {
	m, histCmd := m.openGotoCommitPopup()
	p := layerOf[*gotoCommitPopup](m)
	p.input = newTextField(link)
	m, cmd := p.submit(m, link)
	return m, tea.Batch(histCmd, cmd)
}
```

Wire `noteLinkRows()` into `action_menu.go:144` after `noteMenuRows()`.

`diff_view.go` key switch: `case "x": return m.toggleThreadResolved()`.
(Verified while planning: the model routes keys to the top layer —
`model.go` `l.update(m, msg)` — before its own `case "x"` (session row /
conflict start), so the diff's `x` never reaches it.)

**Reply popup Link field** (`note_popup.go`): `notePopup` gains
`link textfield`; in `noteReply` mode the field order is summary →
rationale → link (`field` cycles 0..2 with the keys that cycle it today —
read `update` for tab / shift+tab), drawn as a third labelled row
`i18n.T("link (optional): a commit or gg:// link")`; `p.note(...)` sets
`Link: strings.TrimSpace(p.link.Value())`. A bad link comes back from
`NoteReply` as `domain.ErrNoteLink` through `noteMutatedMsg.err` — the
existing error notice shows it. Add a test: reply mode, type a summary,
tab twice, type `HEAD`, submit → the cmd's `NoteReply` stores a 40-hex
`Link` (read the stored note back through `svc.NoteGet`).

`model.go`, next to `case noteMutatedMsg:`:

```go
	case threadResolvedMsg:
		if msg.err != nil {
			m.statusMsg = i18n.T("note: %s", msg.err.Error())
			m.diffNotice = "▸ " + m.statusMsg
			return m, nil
		}
		if v := m.diffLayer(); v != nil {
			if v.collapsed == nil {
				v.collapsed = map[string]bool{}
			}
			v.collapsed[msg.root] = msg.resolved
		}
		return m.Update(noteMutatedMsg{}) // the one notes reload path
```

`help.go` (Diff view section, after the `o/O` row):
`r("x", i18n.T("resolve / reopen the thread next to the cursor (a resolved thread folds; o unfolds)"))`.
The diff footer has no room (140-column budget) — help + `.` menu are the
advertised homes (memory: advertise features in help AND footer — the
footer is full; say so in the ledger).

**reviewHintMsg generation guard:** read `internal/tui/review_hint.go`;
`reviewHintMsg` gains `svc *domain.Service` set from `m.svc` when the cmd
is built, and `onReviewHint` drops the message when `msg.svc != m.svc` (the
`gotoLinkResolvedMsg` precedent: a repo switch replaces `m.svc`). Test:
build the msg, swap `m.svc` for another service, deliver — nothing opens.

i18n: add every new key to all four bundles — `"resolved"` (exists? grep
first; forge uses it), `"resolved on GitHub"`, `"Resolve thread"`,
`"Reopen thread"`, `"Open link: %s"`, the help row. Run the gates.

- [ ] **Step 4: Run** `go test ./internal/tui/ ./internal/i18n/ 2>&1 | tail -5` — Expected `ok` ×2 (includes the i18n AST gates).

- [ ] **Step 5: Commit** — `feat(tui): reply to review remarks, x resolves a thread, resolved threads fold, note links open`

---

### Task 8: Tallies — `12 remarks · 4 resolved` on every review row (TUI + domain heads)

**Files:**
- Modify: `internal/domain/review_threads.go` (`docTally`), `internal/domain/review_notes.go`
  (`ReviewHead.Remarks, Resolved`), `internal/domain/notes.go` (`NoteCounts` fills them)
- Modify: `internal/tui/files_view.go` (`reviewRowText`), `internal/tui/branch_reviews.go`
  (`branchReviewRowBody`), `internal/tui/working_review_row.go` (`workingReviewRowText`),
  `internal/tui/all_notes_popup.go` (`anReviewParts`), `internal/tui/review_view.go`
  (`reviewMetaLine`, `reviewOtherNoteLines` outdated threads), new `internal/tui/review_tally.go`,
  the four bundles
- Test: `internal/domain/review_threads_test.go` (tally in heads), `internal/tui/review_tally_test.go`

**Interfaces:**
- Consumes: `Review.Tally()`, `Review.RemarkThreads()`, `ReviewOtherNote.Outdated/Replies`.
- Produces: `ReviewHead.Remarks, ReviewHead.Resolved int`; `func docTally(text string, rs []model.ThreadResolution) (remarks, resolved int)`;
  TUI `func reviewTally(remarks, resolved int, room int) string`.

- [ ] **Step 1: Failing tests**

Domain (append to `review_threads_test.go`):

```go
func TestNoteCountsCarryTheReviewTally(t *testing.T) {
	t.Parallel()
	svc, rid, _ := threadReview(t)
	ctx := context.Background()
	if _, err := svc.NoteResolve(ctx, remarkID(rid, 2), true, "B"); err != nil {
		t.Fatal(err)
	}
	c, err := svc.NoteCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(c.Reviews, func(h ReviewHead) bool { return h.ID == rid })
	if i < 0 || c.Reviews[i].Remarks != 3 || c.Reviews[i].Resolved != 1 {
		t.Fatalf("heads = %+v", c.Reviews)
	}
}
```

TUI (`review_tally_test.go`):

```go
func TestReviewTallyWords(t *testing.T) {
	t.Parallel()
	cases := []struct {
		remarks, resolved, room int
		want                    string
	}{
		{0, 0, 80, ""},
		{12, 4, 80, i18n.T("%d remarks · %d resolved", 12, 4)},
		{1, 0, 80, i18n.T("1 remark · %d resolved", 0)},
		{12, 4, 10, i18n.T("%d/%d resolved", 4, 12)},
	}
	for _, c := range cases {
		if got := reviewTally(c.remarks, c.resolved, c.room); got != c.want {
			t.Errorf("reviewTally(%d,%d,%d) = %q, want %q", c.remarks, c.resolved, c.room, got, c.want)
		}
	}
	for _, r := range []string{"✓", "✗", "✔"} {
		if strings.Contains(reviewTally(5, 2, 80), r) {
			t.Fatalf("a tally must be plain words (user ruling): %q", reviewTally(5, 2, 80))
		}
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/domain/ -run NoteCountsCarry; go test ./internal/tui/ -run ReviewTallyWords` — Expected FAIL (fields / func undefined).

- [ ] **Step 3: Implement**

`review_threads.go`:

```go
// docTally counts a review document's remarks and how many of rs resolve
// one of them (by fingerprint, as RemarkThreads joins). No git: NoteCounts
// calls it for every review.
func docTally(text string, rs []model.ThreadResolution) (remarks, resolved int) {
	r := Review{Text: text}
	if doc, err := notebatch.ParseReview([]byte(text)); err == nil {
		r.Doc = &doc
	}
	r.Resolutions = rs
	return r.Tally()
}
```

(`r.ID` is not needed by `RemarkThreads` — it matches by fingerprint and
`ParseReviewNoteID` index.) `ReviewHead` gains
`Remarks, Resolved int // the document's remark count and how many are resolved`.
In `NoteCounts` (notes.go ~595–610), load the resolutions once
(`st.LoadAllResolved()`, ignore its error), group the `review:` ones by
`model.StoredRootID`, and set `Remarks, Resolved = docTally(n.Rationale, byReview[n.ID])`
on both `ReviewHead` literals.

`internal/tui/review_tally.go`:

```go
package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/i18n"
)

// reviewTally is a review row's "12 remarks · 4 resolved" — plain words
// (user ruling: no check-mark glyphs) — or "4/12 resolved" when room
// columns cannot hold the long form; "" for a review with no remarks.
func reviewTally(remarks, resolved, room int) string {
	if remarks == 0 {
		return ""
	}
	long := i18n.T("%d remarks · %d resolved", remarks, resolved)
	if remarks == 1 {
		long = i18n.T("1 remark · %d resolved", resolved)
	}
	if lipgloss.Width(long) <= room {
		return long
	}
	return i18n.T("%d/%d resolved", resolved, remarks)
}

// withTally appends a tally to a row head when there is one.
func withTally(head string, remarks, resolved, width int) string {
	t := reviewTally(remarks, resolved, width-lipgloss.Width(head)-3)
	if t == "" {
		return head
	}
	return head + " · " + t
}
```

Row call sites — each gets the width it is laid out in (read the call
site for the variable; when a site has none, pass a large room so the long
form is used and the row's existing elision keeps the head):
- `files_view.go` `reviewRowText(r)` → `withTally(head, rem, res, width)` with `rem, res := r.Tally()`.
- `branch_reviews.go` `branchReviewRowBody(h)` → `withTally(…, h.Remarks, h.Resolved, width)`.
- `working_review_row.go` `workingReviewRowText(r)` → `r.Review.Tally()`.
- `all_notes_popup.go` `anReviewParts` → put the tally in the summary
  part (`r.review.Tally()`).
- `review_view.go` `reviewMetaLine`: after `"%d notes on %d files"` add
  `" · " + i18n.T("%d resolved", res)` when `res > 0`; `reviewOtherNoteLines`:
  outdated entries (`o.Outdated`) render under one heading row
  `i18n.T("Outdated (the remark is gone from the re-saved review)")` as
  `"<summary> — N replies"` (+ `" · resolved"`), never with a `path:line`.

i18n: `"%d remarks · %d resolved"`, `"1 remark · %d resolved"`,
`"%d/%d resolved"`, `"%d resolved"`, the Outdated heading — all four bundles.

- [ ] **Step 4: Run** `go test ./internal/domain/ ./internal/tui/ ./internal/i18n/ 2>&1 | tail -5` — Expected `ok` ×3. Update any golden e2e screens this changes only in Task 10.

- [ ] **Step 5: Commit** — `feat: review rows carry the remark tally (plain words)`

---

### Task 9: gg web — reply on remarks, Resolve / Reopen, folding, links, tallies

**Files:**
- Modify: `internal/web/notes.go` (`noteReq.Link`, `handleNoteReply`, new `handleNoteResolve` + route, `noteErrStatus`)
- Modify: `internal/web/reviews.go` (`reviewHeadWire.Remarks/Resolved`), `internal/web/allnotes.go` (`overviewReviewWire`, `overviewWorkingReviewWire` same two fields)
- Modify: `internal/web/static/files.js` (menu rows, `replyNotePrompt`, `readOnlyNote`, link row in `noteBoxHTML`), `internal/web/static/notebox.js` (`noteTitle`), `internal/web/static/reviews.js` (`reviewWords` tally), `internal/web/static/allnotes.js` (`reviewCells` tail)
- Test: `internal/web/notes_test.go`, a JS pin test (grep `steerhintjs_test.go` for the existing "JS source contains" pattern)

**Interfaces:**
- Consumes: `NoteResolve`, `NoteReply`, `WireNote.Replyable/Resolved/ResolvedBy/Link`, `ReviewHead.Remarks/Resolved`, `Review.Tally`.
- Produces: `POST /api/notes/resolve {id, resolved}` → `{ok:true}`; `/api/notes/reply` accepts `link`.

- [ ] **Step 1: Failing Go tests** (`notes_test.go` — reuse its server-over-temp-repo helper and request helper):
  - reply to `review:<id>:0` → 200, and `GET` of the review's notes (the endpoint the review view uses — grep `ReviewNotesFor` in `internal/web`) shows the reply under the remark with `replyable:true` on the remark;
  - `POST /api/notes/resolve {"id":"review:<id>:0","resolved":true}` → 200; the same GET shows `"resolved":true,"resolved_by":…`;
  - resolve `forge:1` → 409; unresolve an open thread → 409; reply to `review:<id>:9` → 404;
  - `reply` with `"link":"HEAD"` stores a 40-hex link; `"link":"not a rev"` → 400.
  - `GET` of the commit's review heads (`reviewHeads` route) carries `"remarks":3,"resolved":…`.

- [ ] **Step 2: Run** `go test ./internal/web/ -run 'Resolve|RemarkReply'` — Expected FAIL (404 route).

- [ ] **Step 3: Implement**
  - `noteReq` gains `Link string \`json:"link"\`` and `Resolved *bool \`json:"resolved"\``.
  - `handleNoteReply`: pass `Link: req.Link` into the note; `noteErrStatus` maps `domain.ErrNoteLink` → 400.
  - `handleNoteResolve`: `id` required, `resolved` required (400 when nil); `NoteResolve(ctx, id, *req.Resolved, author)` with the page's author (the same value `handleNoteReply` uses); 200 `{"ok":true}`; register `mux.HandleFunc("POST /api/notes/resolve", writeGuard(s.handleNoteResolve))`.
  - `noteErrStatus`: `ErrNoteNotFound`, `ErrReviewNotFound`, `ErrNoSuchRemark` → 404; `ErrReadOnlyNote`, `ErrForgeResolved`, `ErrNotResolved` → 409.
  - Wires: `Remarks int \`json:"remarks"\``, `Resolved int \`json:"resolved"\``, filled from `ReviewHead` / `Review.Tally()`.
  - `files.js` context menu (`n.read_only ? [] : …` at ~3694): build
    ```js
    const noteRows = [];
    if (!n.read_only) noteRows.push({ label: "Edit note", act: () => editNotePrompt(n) });
    if (!n.read_only || n.replyable) noteRows.push({ label: "Reply…", act: () => replyNotePrompt(n) });
    if (n.source !== "forge" && !String(n.id).startsWith("forge:"))
      noteRows.push({ label: n.resolved ? "Reopen thread" : "Resolve thread", act: () => resolveNote(n, !n.resolved) });
    for (const l of noteLinks(n)) noteRows.push({ label: "Open link: " + l, act: () => gotoNoteLink(l) });
    ```
    with `noteLinks(n)` = the distinct `link` of the root and its `replies`,
    `gotoNoteLink(l)` = the `#` prompt's opener (`live.js` `gotoLink` — export it if it is module-private, or call the function the `#` prompt's submit calls), and
    ```js
    function resolveNote(n, resolved) {
      noteWrite(resolved ? "resolve" : "reopen", "/api/notes/resolve", { id: n.id, resolved });
    }
    ```
    `noteWrite` already reloads the notes; after a resolve, fold the thread
    (`state.noteCollapsed.add(rootId)` before the reload) and after a reopen
    unfold it (`delete`).
  - `readOnlyNote(n)` is the reply guard too: let `replyNotePrompt` skip it when `n.replyable`.
  - `noteBoxHTML`: a reply (and a root) with `link` renders a last line
    `<div class="notelink" data-link="…">→ ${esc(link)}</div>`; a click on
    `.notelink` calls `gotoNoteLink`.
  - `notebox.js` `noteTitle`: for a stored note append `" · resolved"` when
    `n.resolved` (the read-only branch already does); `seedCollapsed`
    already keys on `n.resolved` — now true for every resolved thread.
  - `reviews.js` `reviewWords(r)`: append `" · " + tallyText(r.remarks, r.resolved)` when `r.remarks > 0`, `tallyText` = `"N remarks · M resolved"` / `"1 remark · M resolved"` (the page is English; same words as the TUI).
  - `allnotes.js` `reviewCells`: put the tally in `tail` (`"  " + tallyText(...)`).
  - The review view's outdated threads: where the page renders
    `ReviewOtherNotes` (grep `other` in `reviews.js`), an `outdated` entry
    renders under "Outdated (the remark is gone from the re-saved review)"
    with its reply count — mirror the TUI.

- [ ] **Step 4: JS pin** — in the Go JS-pin test pattern, assert `files.js`
contains `"/api/notes/resolve"` and `n.replyable`, and `reviews.js` contains
`tallyText(`.

- [ ] **Step 5: Run** `go test ./internal/web/ 2>&1 | tail -3` — Expected `ok`.

- [ ] **Step 6: Manual browser check** (memory: assert VISIBILITY; describe
what the user SEES). Build `./build.sh` in the worktree, run
`<worktree>/bin/gg web` on a scratch repo with a stored review, open the
review: right-click a remark → Reply… and Resolve thread are offered; after
Resolve the thread folds and its title says resolved; the review row says
`N remarks · 1 resolved`. Use the playwright probe the memory describes.

- [ ] **Step 7: Commit** — `feat(web): reply to review remarks, resolve threads, note links, review tallies`

---

### Task 10: Docs, skills, e2e

**Files:**
- Modify: `internal/agentskill/reviewing-with-gg.md`, `internal/agentskill/using-gg.md`, the version constants (grep `Version` in `internal/agentskill/*.go`)
- Modify: `CHANGELOG.md`, `README.md` (notes section, if it lists note verbs), `docs/CLAUDE-details.md` ("Review links" section: stage 2 paragraph)
- Create: `e2e/scenarios/s109_review_answers.toml`

- [ ] **Step 1: e2e scenario** — copy `e2e/scenarios/s108_review_links.toml`'s
setup (repo + stored review via the CLI), then: `gg note apply` with
`{"comments":[{"replyTo":"review:<id>:0","summary":"agreed","resolve":true},{"replyTo":"review:<id>:1","summary":"fixed","link":"HEAD"}]}`
on stdin; assert `gg review show --json latest` has `remarks[0].resolved == true`,
`remarks[1].replies[0].summary == "fixed"`, `resolved == 1`; then
`gg note unresolve review:<id>:0` exits 0 and a second exits 1. Use the
review-id capture the s108 scenario uses (`latest`, or its capture step).

- [ ] **Step 2: Run** `cd /work/gigagit/.claude/worktrees/review-answers && ./test.sh e2e 2>&1 | tail -5` — Expected PASS (re-run with `-update` only for golden screens Task 8 changed; inspect the diff of every updated golden).

- [ ] **Step 3: Skills** — `reviewing-with-gg.md` gains the recipe:

```markdown
## Answering another agent's review

You were handed a review link (`gg://…?review=<id>`).

1. `gg review show <link>` — every remark prints with its id
   (`review:<id>:<n>`), its replies and whether it is resolved.
2. Check each remark against the code. Answer in its thread:
   `gg note reply review:<id>:<n> --summary "…" [--rationale "…"] [--link <commit|gg://…>]`
   — `--link` points at your fix.
3. Settle what is settled: `gg note resolve review:<id>:<n>` (reopen with
   `gg note unresolve`). Leave a remark open when you disagree and say why.
4. Or all at once: `gg note apply --stdin` with
   `{"comments":[{"replyTo":"review:<id>:<n>","summary":"…","link":"…","resolve":true}, …]}`.

The author of the review reads your answers back with `gg review show`.
```

`using-gg.md`: list `gg note resolve|unresolve <id>` and `--link` on
`gg note reply`. Bump both skill versions.

- [ ] **Step 4: Docs** — CHANGELOG (Unreleased: "Review remarks are threads:
reply, resolve / reopen, tallies…"), README note-verb list, CLAUDE-details
stage-2 paragraph (the StoredParent rule, `[[resolved]]`, the fingerprint
join, the forge refusal, the Task 7 rulings).

- [ ] **Step 5: Run** `go test ./internal/agentskill/ 2>&1 | tail -3` (the skill sync test) — Expected `ok`.

- [ ] **Step 6: Commit** — `docs+e2e: review answers — skills, changelog, scenario s109`

---

### Task 11: Gates and the final review

- [ ] **Step 1:** `cd /work/gigagit/.claude/worktrees/review-answers && ./test.sh race > <scratchpad>/race.log 2>&1; tail -5 <scratchpad>/race.log` — green only when it prints `all green`.
- [ ] **Step 2:** Final whole-branch review by a read-only subagent on the most capable model (executing-plans "Final Review"), with this plan's Review Focus verbatim and the ledger's `Ruling:` lines.
- [ ] **Step 3:** Fix Critical/Important findings RED→GREEN, rerun the race gate, ask the user before merging.

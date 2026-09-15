# Notes in merge previews (Feature A) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make a merge preview (`target...source`) a first-class review surface for gg's existing review notes: notes gathered along the whole branch, resolved against the source tip, shown (and addable) in the TUI preview diff, the web preview stage, and the `--preview` CLI/MCP verbs.

**Architecture:** No new note kind and no new store field. A preview note is an ordinary committed note at `FileAddress{State: StateCommitted, Commit: <source tip full sha>, Path}`, `Side: NoteSideNew`. Domain gains a `PreviewNoteSet` (tip + the `merge-base..source` commit list, cached with `PreviewSummary`) and three queries that load every note whose commit is in that set and resolve it through the SAME `resolveNotes`/`resolveOne` code the ordinary note path uses, against the tip's file content. Frontends stamp the preview diff view with that set; every existing note gate then works unchanged.

**Tech Stack:** Go 1.26, Bubble Tea (TUI), plain ES modules (web), `github.com/modelcontextprotocol/go-sdk` (MCP), TOML e2e scenarios.

**Spec:** `docs/superpowers/specs/2026-09-10-preview-notes-design.md` — **§1 only**. §2 (Feature B: preview links, steering, `gg open`) is a LATER plan; do not implement any of it.

## Global Constraints

Copied from the spec and the controller's binding rulings. Every task's requirements implicitly include this section.

1. **Hunk numbering under `--preview` comes from the PREVIEW diff** — `model.DiffSpec{Rev: "<targetHash>...<sourceHash>"}` through `HunkRange`/`DiffHunks`, never the tip commit's own parent→tip diff. `gg diff --preview --hunks` and `gg note add --preview --hunk N` must agree, and so must `gg note apply --preview` batch items and `gg review --preview --notes`.
2. **Any new-side line at the tip must be anchorable.** VERIFIED: `internal/domain/notes.go:695-700` — for `StateCommitted` the new side is `s.ShowFile(ctx, addr.Commit, addr.Path)`, i.e. the FULL FILE at the commit, not the commit's diff. No fix needed; do not change `noteSideLines`.
3. **One resolver, not two.** Factor `NotesFor` into load + resolve; `PreviewNotesFor` reuses the same per-note resolver (`resolveNotes` → `resolveOne`). Load by iterating the store ONCE and testing `commit ∈ set` (a `map[string]bool`), never one store query per commit.
4. **The old-side guard is enforced in code.** A preview diff view's note-add path (`c`, the `.`-menu rows, the popup) refuses old-side rows with the i18n notice `notes in a preview anchor on the new side`. Steering `highlight add` with `side: "old"` on a preview view is refused the same way, in the TUI consumer (the CLI need not know).
5. **One identification rule for preview diff views** (see the gate table in Task 3): `v.compare` stays `true`, `v.rev` is already the source tip, `v.noteAddr = FileAddress{StateCommitted, Tip, Path}` is stamped, and `v.previewSet *domain.PreviewNoteSet` is non-nil. Every gate is handled through `previewSet` or through the stamped `noteAddr`; no gate is left keyed on `v.compare` alone.
6. **Non-OK preview states** (`merged`, `missing-source`, `missing-target`, `no-base`): no set (`PreviewNoteSet{}` with `Tip == ""`), no badge, no notes, **no error**.
7. **Caching:** the `PreviewNotes` rev-list is cached with the summary, keyed `(srcHash, tgtHash)`. Preview note COUNTS depend on the notes store, so they invalidate with `s.noteCounts` (every note mutation) as well as when the summary cache moves.
8. **Merge-preview gotchas (binding):** never dispatch a previews read from `reRoot`'s batch (chain it off `dataLoadedMsg`); every previews reload inherits the manual flag (`chainPreviewsRead()`); a same-tag re-open must reconcile hashes; in the web, sidebar branch hashes are SHORT — compare by prefix; a failed `/api/preview` fetch keeps previous rows.
9. **`gg diff --preview` and `gg review --preview` are sugar over the existing `A...B` range paths**: resolve the pair, then call the existing code; no new diff path. `--preview` is refused with `--rev`, `--cached` or a `gg://` positional (exit 2, message contains `one target only`); `--old-line` with `--preview` is refused (exit 2).
10. **Every new user-visible TUI string is a literal `i18n.T` key present in ALL FOUR bundles** (`internal/i18n/lang/{ja,ko,zh,ru}.toml`); the AST-gate tests (`i18n_scan_test.go`, `options_vocab_test.go`, `menu_labels_test.go`, `engine_prose_test.go`, `render_order_test.go`) fail otherwise. This feature adds exactly three: `notes in a preview anchor on the new side`, `(outdated)`, `Remove all notes on %s (%d of %d).`
11. **Web:** reuse the existing note row renderer and note-add flow with `target: {state: "commit", commit: <tip>}`; new `GET /api/preview/notes?source=&target=&path=`; `/api/preview` rows gain `notes`; stale renders with the CSS class `outdated`; the add control is not offered on old-side rows. `./build.sh web` is part of the merge checklist.
12. **Skills:** both embedded skills get a paragraph (spec §1.5); bump `Version` 68→69 and `ReviewVersion` 5→6 in the same task. **No task runs `gg init --update`** (it refreshes the user's global copies).
13. **Tests:** real `git` in a `t.TempDir()`; TUI tests call `t.Parallel()` and inject state (never `t.Setenv`); tests calling `lipgloss.SetColorProfile` are NOT parallel; web JS assertions follow the source-assertion pattern used by the existing web tests; the e2e scenario is `e2e/scenarios/s91_preview_notes.toml` (spec §1.6). `internal/tui`/`cli`/`web`/`mcp` never import `internal/git` (guarded by `internal/archtest`).
14. **Commits:** every command is prefixed `cd /mnt/t/others/gigagit.worktrees/feat-preview-notes &&` (the shell cwd resets to the main checkout between calls); stage NAMED files only, never `git add -A`; every commit message ends with:
```
Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU
```
15. **`CHANGELOG.md` contains a literal `<<<<<<<` line far below** — that is content, not a conflict marker. The new entry goes directly under `## [Unreleased]` (line 9) by line number.
16. **Run everything in the FOREGROUND. No background jobs.** `go test ./internal/tui/` takes ~2 min, `./test.sh unit` ~5 min, `./test.sh race` ~10 min. Budget for it; do not background a test run and move on.

---

## File Structure

**Created**
- `internal/git/revlist.go` — the single new git verb (`RevListRange`).
- `internal/domain/previewnotes.go` — `PreviewNoteSet`, `PreviewNotes`, `PreviewResolve`, `PreviewNotesFor`, `PreviewNotesAt`, `PreviewNoteCounts`, `PreviewStatus`.
- `internal/cli/previewflag.go` — the one `--preview` flag helper shared by `diff`, `note`, `review`.
- `internal/web/previewnotes.go` — `GET /api/preview/notes`.
- `e2e/scenarios/s91_preview_notes.toml`.
- Test files mirroring each of the above.

**Modified** (major touch points only; exact anchors live in each task)
- `internal/domain/notes.go` — factor the loader out of `NotesFor`.
- `internal/domain/preview.go` — expose the cached base on `PreviewSummary`.
- `internal/domain/notebatch_plan.go` — hunk-spec override for batches.
- `internal/tui/{diff_view.go,files_view.go,preview_open.go,preview_panel.go,note_keys.go,note_popup.go,note_remove_all_popup.go,diff_notes.go,steer_attn.go,viewstate.go,model.go}`.
- `internal/cli/{diff.go,note.go,review.go,preview.go}`; `internal/mcp/notes.go`.
- `internal/web/{previews.go,static/files.js,static/previews.js,static/style.css}`.
- `internal/agentskill/{agentskill.go,using-gg.md,reviewing-with-gg.md}`; `CHANGELOG.md`, `README.md`, `docs/CLAUDE-details.md`.
- `internal/i18n/lang/{ja,ko,zh,ru}.toml`.

---

### Task 1: The commit set — `git rev-list` verb, `PreviewNotes`, `PreviewResolve`

**Files:**
- Create: `internal/git/revlist.go`
- Create: `internal/domain/previewnotes.go`
- Modify: `internal/domain/preview.go:127-134` (the `PreviewSummary` struct — expose `base`)
- Test: `internal/git/revlist_test.go`, `internal/domain/previewnotes_test.go`

**Interfaces:**
- Consumes: `(*git.Repo).MergeBase` (`internal/git/mergebase.go:14`), `(*Service).PreviewSummary` (`internal/domain/preview.go:142`), `(*Service).PreviewGet` (`internal/domain/preview.go:55`).
- Produces:
  - `func (r *git.Repo) RevListRange(ctx context.Context, base, tip string) ([]string, error)` — full shas, newest first.
  - `type domain.PreviewNoteSet struct { Source, Target, Tip, Base string; Commits []string }` — `Tip == ""` means "not previewable" (ruling 6).
  - `func (set PreviewNoteSet) OK() bool`
  - `func (s *domain.Service) PreviewNotes(ctx context.Context, source, target string) (PreviewNoteSet, error)`
  - `func (s *domain.Service) PreviewResolve(ctx context.Context, spec string) (source, target string, err error)` — accepts an id, a label, or `<target>...<source>`.
  - `func (set PreviewNoteSet) DiffSpec() model.DiffSpec` — the ONE construction of the preview's patch (`<base>..<tip>`); every hunk-numbering caller uses it (ruling 1).
  - `domain.PreviewSummary.Base() string` (the previously unexported `base`).

- [ ] **Step 1: Write the failing git-verb test**

Create `internal/git/revlist_test.go`:

```go
package git

import (
	"context"
	"testing"
)

func TestRevListRangeListsCommitsNewestFirst(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	writeFile(t, r, "a.txt", "one\n")
	commitAll(t, r, "c1")
	base, err := r.RevParse(ctx, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, r, "a.txt", "two\n")
	commitAll(t, r, "c2")
	writeFile(t, r, "a.txt", "three\n")
	commitAll(t, r, "c3")
	tip, err := r.RevParse(ctx, "HEAD")
	if err != nil {
		t.Fatal(err)
	}

	got, err := r.RevListRange(ctx, base, tip)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 commits between base and tip, got %d: %v", len(got), got)
	}
	if got[0] != tip {
		t.Fatalf("newest first: want %s, got %s", tip, got[0])
	}
	for _, c := range got {
		if len(c) != 40 {
			t.Fatalf("want full shas, got %q", c)
		}
	}
}

func TestRevListRangeEmptyWhenTipIsBase(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	writeFile(t, r, "a.txt", "one\n")
	commitAll(t, r, "c1")
	h, err := r.RevParse(ctx, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.RevListRange(ctx, h, h)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("want no commits, got %v", got)
	}
}
```

If `newRepo`/`writeFile`/`commitAll`/`RevParse` have different names in `internal/git`'s test helpers, use the existing ones — run `grep -n "func newRepo\|func writeFile\|func commitAll" internal/git/*_test.go` first and adapt the three calls; do not add new helpers.

- [ ] **Step 2: Run it to make sure it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-preview-notes && go test ./internal/git/ -run TestRevListRange -v`
Expected: FAIL — `r.RevListRange undefined`.

- [ ] **Step 3: Write the verb**

Create `internal/git/revlist.go`:

```go
package git

import (
	"context"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
)

// RevListRange lists the commits reachable from tip but not from base
// (`git rev-list <base>..<tip>`), newest first, as full shas. One invocation.
//
// First-parent is deliberately NOT used: a merge preview gathers notes along
// the WHOLE branch, and a note left on a merged side branch still belongs to
// the preview (spec §1.2).
func (r *Repo) RevListRange(ctx context.Context, base, tip string) ([]string, error) {
	argv := gitcmd.New("rev-list").Arg(base + ".." + tip).ToArgv()
	res, err := r.Runner.Run(ctx, "git rev-list (range)", argv)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(res.Stdout, "\n") {
		if s := strings.TrimSpace(line); s != "" {
			out = append(out, s)
		}
	}
	return out, nil
}
```

- [ ] **Step 4: Run it to verify it passes**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-preview-notes && go test ./internal/git/ -run TestRevListRange -v`
Expected: PASS (both tests).

- [ ] **Step 5: Write the failing domain test**

Create `internal/domain/previewnotes_test.go`:

```go
package domain

import (
	"context"
	"testing"
)

// TestPreviewNotesGathersTheBranch: main + feat (three commits). The set's tip
// is feat's tip and Commits is exactly the three commits merge-base..feat.
func TestPreviewNotesGathersTheBranch(t *testing.T) {
	svc, repoDir := newPreviewRepo(t) // helper at the bottom of this file
	ctx := context.Background()
	_ = repoDir

	set, err := svc.PreviewNotes(ctx, "feat", "main")
	if err != nil {
		t.Fatal(err)
	}
	if !set.OK() {
		t.Fatalf("want an ok set, got %+v", set)
	}
	if len(set.Commits) != 3 {
		t.Fatalf("want 3 commits on feat, got %d: %v", len(set.Commits), set.Commits)
	}
	if set.Commits[0] != set.Tip {
		t.Fatalf("newest first: Commits[0]=%s Tip=%s", set.Commits[0], set.Tip)
	}
	if set.Source != "feat" || set.Target != "main" {
		t.Fatalf("set must remember its pair, got %+v", set)
	}
}

// Ruling 6: a non-ok state yields an EMPTY set and NO error.
func TestPreviewNotesOnANonOKPairIsEmptyAndSilent(t *testing.T) {
	svc, _ := newPreviewRepo(t)
	ctx := context.Background()

	set, err := svc.PreviewNotes(ctx, "no-such-branch", "main")
	if err != nil {
		t.Fatalf("a missing source must not be an error: %v", err)
	}
	if set.OK() || set.Tip != "" {
		t.Fatalf("want an empty set, got %+v", set)
	}
}

func TestPreviewResolveAcceptsTheThreeDotForm(t *testing.T) {
	svc, _ := newPreviewRepo(t)
	src, tgt, err := svc.PreviewResolve(context.Background(), "main...feat")
	if err != nil {
		t.Fatal(err)
	}
	if src != "feat" || tgt != "main" {
		t.Fatalf("`<target>...<source>`: want source=feat target=main, got %s/%s", src, tgt)
	}
}

func TestPreviewResolveAcceptsASavedIDAndLabel(t *testing.T) {
	svc, _ := newPreviewRepo(t)
	ctx := context.Background()
	p, err := svc.PreviewAdd(ctx, "feat", "main", "login")
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range []string{p.ID, "login"} {
		src, tgt, err := svc.PreviewResolve(ctx, spec)
		if err != nil {
			t.Fatalf("%s: %v", spec, err)
		}
		if src != "feat" || tgt != "main" {
			t.Fatalf("%s: got %s/%s", spec, src, tgt)
		}
	}
}
```

Add the helper at the bottom of the same file. Follow the existing `internal/domain` test-repo pattern — run `grep -n "func newTestService\|func newRepo\|func newSvc" internal/domain/*_test.go` and reuse whatever builds a `*Service` over a real git `t.TempDir()`; the helper below shows the SHAPE it must produce, not a new mechanism:

```go
// newPreviewRepo builds a real repo: main with one commit, then feat with
// three commits, the second of which rewrites a line the first added (so a
// note on the first commit goes outdated on the tip).
func newPreviewRepo(t *testing.T) (*Service, string) {
	t.Helper()
	svc, dir := newTestService(t) // the existing domain test helper
	ctx := context.Background()
	run := func(args ...string) {
		t.Helper()
		if out, err := gitRun(t, dir, args...); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	writeRepoFile(t, dir, "a.txt", "alpha\nbravo\ncharlie\n")
	run("add", "."); run("commit", "-m", "seed")
	run("checkout", "-b", "feat")
	writeRepoFile(t, dir, "a.txt", "alpha\nbravo\ncharlie\nDELTA\n")
	run("add", "."); run("commit", "-m", "c1 adds DELTA")
	writeRepoFile(t, dir, "a.txt", "alpha\nbravo\ncharlie\nECHO\n")
	run("add", "."); run("commit", "-m", "c2 rewrites DELTA")
	writeRepoFile(t, dir, "b.txt", "bee\n")
	run("add", "."); run("commit", "-m", "c3 adds b.txt")
	run("checkout", "main")
	_ = ctx
	return svc, dir
}
```

(`gitRun`/`writeRepoFile`: use the existing helpers in `internal/domain`'s test files; if they are named differently, adapt the two call sites.)

- [ ] **Step 6: Run the domain test to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-preview-notes && go test ./internal/domain/ -run TestPreview -v`
Expected: FAIL — `svc.PreviewNotes undefined`, `svc.PreviewResolve undefined`.

- [ ] **Step 7: Expose the cached merge base**

In `internal/domain/preview.go`, directly after the `PreviewSummary` struct (which ends at line 134), add:

```go
// Base is the merge base the summary computed ("" unless PreviewOK). It rides
// the SAME cache entry as the rest of the summary, so a preview's note set
// costs no extra git call while the tips are unchanged.
func (s PreviewSummary) Base() string { return s.base }
```

- [ ] **Step 8: Write `previewnotes.go`**

Create `internal/domain/previewnotes.go`:

```go
package domain

import (
	"context"
	"strings"

	"github.com/homeend/gigagit/internal/model"
)

// Notes in merge previews (spec §1).
//
// A preview of source → target shows `git diff target...source`: the merge
// base on the old side, the source TIP on the new side. The new side is byte
// for byte the file at the tip, so a note on it is an ordinary committed note
// — FileAddress{StateCommitted, <tip>, Path}, Side new. Nothing new is stored.
//
// What IS new is the READ: when the agent pushes more commits the tip moves,
// and notes written against the old tip would vanish from the preview. So the
// preview gathers notes along the whole branch (merge-base..source) and
// resolves each one against the tip's content through the SAME resolver the
// ordinary note path uses. A note whose anchored text survived is active; one
// whose lines a later commit changed is stale — which the preview surfaces as
// "outdated", because there it is the expected case rather than an edge one.

// PreviewNoteSet is one preview's note scope: the write target (the source
// tip) and every commit whose notes the preview gathers.
//
// The zero value means "not previewable" — a merged pair, a missing name, no
// common base. That is NOT an error (spec ruling 6): callers test OK() and
// simply show no notes and no badge.
type PreviewNoteSet struct {
	Source, Target string   // the pair's NAMES, as saved/typed
	Tip            string   // full sha of the source tip: the write target
	Base           string   // merge-base(target, source)
	Commits        []string // merge-base..source, newest first (includes Tip)
}

// OK reports whether the pair resolved to a previewable range.
func (set PreviewNoteSet) OK() bool { return set.Tip != "" }

// DiffSpec is THE preview's patch: merge-base → source tip. Every surface that
// numbers hunks under --preview (the CLI's `gg diff --preview --hunks`, the
// note verbs' --hunk N, the batch planner, MCP) builds it from here and
// nowhere else, so they cannot drift apart (ruling 1). `<base>..<tip>` and
// `<targetHash>...<sourceHash>` are the same patch by construction; the base
// is already resolved here, so the two-dot form is the cheaper spelling.
func (set PreviewNoteSet) DiffSpec() model.DiffSpec {
	if !set.OK() {
		return model.DiffSpec{}
	}
	return model.DiffSpec{Rev: set.Base + ".." + set.Tip}
}

// has reports whether a commit belongs to this preview. The membership map is
// built ONCE per query (ruling 3): the store is iterated a single time and
// every note tested against this set, never one store query per commit.
func (set PreviewNoteSet) commitSet() map[string]bool {
	m := make(map[string]bool, len(set.Commits))
	for _, c := range set.Commits {
		m[c] = true
	}
	return m
}

// PreviewNotes resolves a pair into its note set. It reuses PreviewSummary's
// cached (srcHash, tgtHash) entry for the tip and the base, so an unchanged
// pair costs the two rev-parse calls the summary already makes plus one
// rev-list; a moved tip is a new cache key and recomputes everything.
func (s *Service) PreviewNotes(ctx context.Context, source, target string) (PreviewNoteSet, error) {
	sum, err := s.PreviewSummary(ctx, source, target)
	if err != nil {
		return PreviewNoteSet{}, err
	}
	if sum.State != PreviewOK {
		return PreviewNoteSet{}, nil // ruling 6: no set, no error
	}
	key := "preview-revlist:" + sum.SourceHash + ":" + sum.TargetHash
	v, err := s.factory.Cache("preview").GetOrLoad(key, func() (any, error) {
		return query(ctx, s, key, func(ctx context.Context) ([]string, error) {
			return s.repo.RevListRange(ctx, sum.Base(), sum.SourceHash)
		})
	})
	if err != nil {
		return PreviewNoteSet{}, err
	}
	return PreviewNoteSet{
		Source: source, Target: target,
		Tip: sum.SourceHash, Base: sum.Base(),
		Commits: v.([]string),
	}, nil
}

// PreviewResolve turns one --preview argument into a (source, target) pair:
// a saved record's id or label, or git's three-dot form `<target>...<source>`
// (the same order `git diff target...source` reads, and the order Feature B's
// link grammar will use). The three-dot form needs no saved record.
//
// It lives in domain, not in the CLI, because MCP resolves the same string.
func (s *Service) PreviewResolve(ctx context.Context, spec string) (source, target string, err error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", "", ErrPreviewNotFound
	}
	if i := strings.Index(spec, "..."); i >= 0 {
		target, source = strings.TrimSpace(spec[:i]), strings.TrimSpace(spec[i+3:])
		if target == "" || source == "" {
			return "", "", errPreviewPairShape
		}
		return source, target, nil
	}
	p, err := s.PreviewGet(ctx, spec)
	if err != nil {
		return "", "", err
	}
	return p.Source, p.Target, nil
}
```

Add the sentinel next to the other preview errors in `internal/domain/preview.go:16-19`:

```go
// errPreviewPairShape is a three-dot --preview argument missing one side.
var errPreviewPairShape = errors.New("preview: expected <target>...<source>")
```

- [ ] **Step 9: Run the domain test to verify it passes**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-preview-notes && go test ./internal/domain/ -run TestPreview -v`
Expected: PASS.

- [ ] **Step 10: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-preview-notes && git add internal/git/revlist.go internal/git/revlist_test.go internal/domain/previewnotes.go internal/domain/previewnotes_test.go internal/domain/preview.go && git commit -m "feat(domain): a merge preview's note set — rev-list verb, PreviewNotes, PreviewResolve

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 2: Gathering and resolving — `PreviewNotesFor`, `PreviewNotesAt`, `PreviewNoteCounts`

**Files:**
- Modify: `internal/domain/notes.go:297-328` (factor the loader out of `NotesFor`), `internal/domain/notes.go:444-449` (drop the new cache on invalidation)
- Modify: `internal/domain/previewnotes.go` (append)
- Modify: `internal/domain/service.go:50-55` (one new cached field)
- Test: `internal/domain/previewnotes_test.go` (append)

**Interfaces:**
- Consumes: `PreviewNoteSet` + `PreviewNotes` (Task 1); `resolveNotes`/`resolveOne` (`internal/domain/notes.go:626`, `:556`); `diffSideLines` (`:507`); `(*Service).ShowFile` (`internal/domain/query.go:406`); `splitLines` (`:804`).
- Produces:
  - `func (s *Service) PreviewNotesFor(ctx context.Context, set PreviewNoteSet, path string, d Diff) ([]ResolvedNote, error)` — for a frontend that already holds the compare diff.
  - `func (s *Service) PreviewNotesAt(ctx context.Context, set PreviewNoteSet, path string) ([]ResolvedNote, error)` — for the stateless callers (web, CLI, MCP).
  - `func (s *Service) PreviewNoteCounts(ctx context.Context, set PreviewNoteSet) (byPath map[string]int, total int, err error)`
  - `func PreviewStatus(st model.NoteStatus) string` — `"outdated"` for `model.NoteStale`, otherwise `string(st)`.
  - `func (s *Service) loadNotesAt(ctx context.Context, addr model.FileAddress) ([]model.Note, error)` (unexported; `NotesFor`/`NotesAt` now share it).

- [ ] **Step 1: Write the failing tests**

Append to `internal/domain/previewnotes_test.go`:

```go
// A note on the FIRST feat commit, on a line the SECOND commit rewrote, must
// still be listed on the preview — as stale (rendered "outdated"). A note on
// a line that survived is active.
func TestPreviewNotesForGathersOlderCommitsAndMarksThemStale(t *testing.T) {
	svc, dir := newPreviewRepo(t)
	ctx := context.Background()
	c1 := revParse(t, dir, "feat~2") // "c1 adds DELTA"

	// On c1, line 4 is "DELTA"; c2 rewrote it to "ECHO" → stale on the tip.
	stale, err := svc.NoteAdd(ctx, model.Note{
		Source: model.NoteSourceAgent, Author: "ada",
		Address: model.FileAddress{State: model.StateCommitted, Commit: c1, Path: "a.txt"},
		Side:    model.NoteSideNew, Range: [2]int{4, 4}, Summary: "why DELTA",
	})
	if err != nil {
		t.Fatal(err)
	}
	// On c1, line 1 is "alpha" and still is on the tip → active.
	live, err := svc.NoteAdd(ctx, model.Note{
		Source: model.NoteSourceAgent, Author: "ada",
		Address: model.FileAddress{State: model.StateCommitted, Commit: c1, Path: "a.txt"},
		Side:    model.NoteSideNew, Range: [2]int{1, 1}, Summary: "alpha stands",
	})
	if err != nil {
		t.Fatal(err)
	}

	set, err := svc.PreviewNotes(ctx, "feat", "main")
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.PreviewNotesAt(ctx, set, "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]ResolvedNote{}
	for _, r := range got {
		byID[r.Note.ID] = r
	}
	if len(byID) != 2 {
		t.Fatalf("want both notes gathered from the older commit, got %d", len(byID))
	}
	if s := byID[stale.ID].Status; s != model.NoteStale {
		t.Fatalf("a note on a rewritten line must be stale, got %q", s)
	}
	if s := byID[live.ID].Status; s != model.NoteActive {
		t.Fatalf("a note on a surviving line must be active, got %q", s)
	}
	if w := PreviewStatus(model.NoteStale); w != "outdated" {
		t.Fatalf("the preview word for stale is outdated, got %q", w)
	}
}

// A note whose commit is no longer on the branch is ORPHANED: hidden from the
// listing but still counted (spec §1.2, the "retired file" rule).
func TestPreviewNoteCountsIncludeHiddenNotes(t *testing.T) {
	svc, dir := newPreviewRepo(t)
	ctx := context.Background()
	c1 := revParse(t, dir, "feat~2")
	if _, err := svc.NoteAdd(ctx, model.Note{
		Source: model.NoteSourceAgent, Author: "ada",
		Address: model.FileAddress{State: model.StateCommitted, Commit: c1, Path: "a.txt"},
		Side:    model.NoteSideNew, Range: [2]int{1, 1}, Summary: "on the branch",
	}); err != nil {
		t.Fatal(err)
	}
	// A note on a commit that is NOT on feat at all (main's seed commit).
	seed := revParse(t, dir, "main")
	if _, err := svc.NoteAdd(ctx, model.Note{
		Source: model.NoteSourceAgent, Author: "ada",
		Address: model.FileAddress{State: model.StateCommitted, Commit: seed, Path: "a.txt"},
		Side:    model.NoteSideNew, Range: [2]int{1, 1}, Summary: "off the branch",
	}); err != nil {
		t.Fatal(err)
	}

	set, err := svc.PreviewNotes(ctx, "feat", "main")
	if err != nil {
		t.Fatal(err)
	}
	byPath, total, err := svc.PreviewNoteCounts(ctx, set)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || byPath["a.txt"] != 1 {
		t.Fatalf("only the branch's own note counts: total=%d byPath=%v", total, byPath)
	}
}

// Ruling 7: the counts follow the notes store, not only the summary cache.
func TestPreviewNoteCountsInvalidateOnAMutation(t *testing.T) {
	svc, dir := newPreviewRepo(t)
	ctx := context.Background()
	set, err := svc.PreviewNotes(ctx, "feat", "main")
	if err != nil {
		t.Fatal(err)
	}
	if _, total, err := svc.PreviewNoteCounts(ctx, set); err != nil || total != 0 {
		t.Fatalf("cold counts: total=%d err=%v", total, err)
	}
	if _, err := svc.NoteAdd(ctx, model.Note{
		Source: model.NoteSourceAgent, Author: "ada",
		Address: model.FileAddress{State: model.StateCommitted, Commit: revParse(t, dir, "feat"), Path: "b.txt"},
		Side:    model.NoteSideNew, Range: [2]int{1, 1}, Summary: "fresh",
	}); err != nil {
		t.Fatal(err)
	}
	_, total, err := svc.PreviewNoteCounts(ctx, set)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("a note mutation must drop the cached preview counts, got total=%d", total)
	}
}

// Ruling 6: a non-ok set answers empty, silently.
func TestPreviewNotesOnAnEmptySetAreSilent(t *testing.T) {
	svc, _ := newPreviewRepo(t)
	ctx := context.Background()
	got, err := svc.PreviewNotesAt(ctx, PreviewNoteSet{}, "a.txt")
	if err != nil || got != nil {
		t.Fatalf("want (nil, nil), got (%v, %v)", got, err)
	}
	byPath, total, err := svc.PreviewNoteCounts(ctx, PreviewNoteSet{})
	if err != nil || total != 0 || len(byPath) != 0 {
		t.Fatalf("want empty counts and no error, got (%v, %d, %v)", byPath, total, err)
	}
}
```

Add the import of `"github.com/homeend/gigagit/internal/model"` to the test file, and this helper beside `newPreviewRepo`:

```go
// revParse is the test's own rev resolver: full sha for a rev in dir.
func revParse(t *testing.T, dir, rev string) string {
	t.Helper()
	out, err := gitRun(t, dir, "rev-parse", rev)
	if err != nil {
		t.Fatalf("rev-parse %s: %v", rev, err)
	}
	return strings.TrimSpace(out)
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-preview-notes && go test ./internal/domain/ -run TestPreviewNote -v`
Expected: FAIL — `svc.PreviewNotesAt undefined`, `svc.PreviewNoteCounts undefined`, `PreviewStatus undefined`.

- [ ] **Step 3: Factor the loader out of `NotesFor`**

Replace the body of `NotesFor` at `internal/domain/notes.go:297-328` with a call to a new shared loader. `NotesFor` becomes:

```go
// NotesFor returns the notes that apply to addr, resolved against the OPEN
// diff d and threaded (roots carry their replies), sorted new-side-first then
// by line. Orphaned notes are omitted — they are hidden until the startup
// sweep drops them. Reads never rewrite the store.
func (s *Service) NotesFor(ctx context.Context, addr model.FileAddress, d Diff) ([]ResolvedNote, error) {
	mine, err := s.loadNotesAt(ctx, addr)
	if err != nil {
		return nil, err
	}
	oldLines, newLines := diffSideLines(d)
	return keepResolved(resolveNotes(mine, oldLines, newLines)), nil
}

// loadNotesAt is the STORE half of a note read, shared by NotesFor, NotesAt
// and (through its own membership rule) the preview queries: load once, scope
// a worktree-state query to this checkout, keep what sameNoteTarget claims.
// Splitting it out is what keeps ONE resolver in the codebase — every caller
// below this line hands its notes to resolveNotes and nothing else.
func (s *Service) loadNotesAt(ctx context.Context, addr model.FileAddress) ([]model.Note, error) {
	st := s.notesStore(ctx)
	if st == nil {
		return nil, ErrNotesDisabled
	}
	all, err := st.Load()
	if err != nil {
		return nil, err
	}
	// Scope the query to this checkout so a sibling worktree's notes on the
	// same path never surface here.
	if worktreeScopedNote(addr) {
		wt, werr := s.noteWorktree(ctx, addr)
		if werr != nil {
			return nil, werr
		}
		addr.Worktree = wt
	}
	mine := make([]model.Note, 0, len(all))
	for _, n := range all {
		if sameNoteTarget(n.Address, addr) {
			mine = append(mine, n)
		}
	}
	return mine, nil
}

// keepResolved drops the orphans every note read hides (§4.4).
func keepResolved(res []ResolvedNote) []ResolvedNote {
	kept := res[:0]
	for _, r := range res {
		if r.Status != model.NoteOrphaned {
			kept = append(kept, r)
		}
	}
	return kept
}
```

Then rewrite `NotesAt` (`internal/domain/notes.go:341-375`) to use the same two helpers, keeping its "only read the sides when there is something to resolve" behaviour:

```go
func (s *Service) NotesAt(ctx context.Context, addr model.FileAddress) ([]ResolvedNote, error) {
	mine, err := s.loadNotesAt(ctx, addr)
	if err != nil {
		return nil, err
	}
	if len(mine) == 0 {
		return nil, nil
	}
	// Only now are the two sides worth reading: the reads shell out to git,
	// and an address with no notes at all must cost nothing.
	oldLines, _ := s.noteSideLines(ctx, addr, model.NoteSideOld)
	newLines, _ := s.noteSideLines(ctx, addr, model.NoteSideNew)
	return keepResolved(resolveNotes(mine, oldLines, newLines)), nil
}
```

- [ ] **Step 4: Run the existing note tests — the refactor must change nothing**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-preview-notes && go test ./internal/domain/ -run TestNote -v`
Expected: PASS, exactly as before the refactor. If anything fails, the refactor is wrong — fix it before continuing.

- [ ] **Step 5: Add the cached-counts field**

In `internal/domain/service.go`, directly after `noteCounts *NoteCounts` (line 50), add:

```go
	// previewCounts caches PreviewNoteCounts per (tip, base) pair. It follows
	// BOTH clocks: a new tip is a new key, and every note mutation drops the
	// whole map through invalidateNoteCounts (ruling 7) — counts read the
	// notes store, which the summary cache knows nothing about.
	previewCounts map[string]previewCountEntry
```

And in `internal/domain/notes.go:444-449`, extend `invalidateNoteCounts`:

```go
func (s *Service) invalidateNoteCounts() {
	s.mu.Lock()
	s.noteCounts = nil
	s.previewCounts = nil // preview badges count the same store
	s.notesGen++
	s.mu.Unlock()
}
```

`SetNotesStore` (`internal/domain/notesstore.go:45-54`) clears `noteCounts` inline instead of calling that helper, so it needs the same line or a test that injects a store reads stale preview counts:

```go
	s.notes = st
	s.noteCounts = nil
	s.previewCounts = nil // same store, same invalidation
	s.notesGen++
```

- [ ] **Step 6: Append the queries to `previewnotes.go`**

```go
// PreviewStatus is the word a preview uses for a resolved note's status.
// "Outdated" is the preview's name for stale (spec §1.2): in a preview a note
// whose lines a later commit changed is the EXPECTED case, not an edge case,
// so the surfaces say so. model.NoteStatus gains no value — this maps at
// render/wire time only, so the store and the resolver stay untouched.
func PreviewStatus(st model.NoteStatus) string {
	if st == model.NoteStale {
		return "outdated"
	}
	return string(st)
}

// loadPreviewNotes gathers every stored note the preview covers for one path:
// a committed, NEW-side note whose commit is in the set. The store is iterated
// ONCE against a membership map (ruling 3) — never one query per commit.
//
// Old-side notes on those commits are ignored on purpose (spec §1.2): they
// belong to that commit's own parent→commit picture, not to the
// merge-base → tip one the preview draws. Replies inherit their root's side,
// so this one test covers them too.
func (s *Service) loadPreviewNotes(ctx context.Context, set PreviewNoteSet, path string) ([]model.Note, error) {
	st := s.notesStore(ctx)
	if st == nil {
		return nil, ErrNotesDisabled
	}
	all, err := st.Load()
	if err != nil {
		return nil, err
	}
	in := set.commitSet()
	mine := make([]model.Note, 0, 8)
	for _, n := range all {
		if n.Address.State != model.StateCommitted || n.Side != model.NoteSideNew {
			continue
		}
		if path != "" && n.Address.Path != path {
			continue
		}
		if in[n.Address.Commit] {
			mine = append(mine, n)
		}
	}
	return mine, nil
}

// PreviewNotesFor resolves the preview's notes for one path against a diff the
// caller already holds (the TUI's open compare view). Only the NEW side is
// used: the preview's old side is the merge base, which no stored address
// names, so nothing can anchor there.
func (s *Service) PreviewNotesFor(ctx context.Context, set PreviewNoteSet, path string, d Diff) ([]ResolvedNote, error) {
	if !set.OK() {
		return nil, nil
	}
	mine, err := s.loadPreviewNotes(ctx, set, path)
	if err != nil {
		return nil, err
	}
	_, newLines := diffSideLines(d)
	return keepResolved(resolveNotes(mine, nil, newLines)), nil
}

// PreviewNotesAt is PreviewNotesFor for a caller with no diff in hand (the web
// handler, the CLI, MCP). The preview's new side is byte for byte the file at
// the tip, so the tip's own content is the resolution text — the same bytes
// noteSideLines would read for a committed address.
func (s *Service) PreviewNotesAt(ctx context.Context, set PreviewNoteSet, path string) ([]ResolvedNote, error) {
	if !set.OK() {
		return nil, nil
	}
	mine, err := s.loadPreviewNotes(ctx, set, path)
	if err != nil {
		return nil, err
	}
	if len(mine) == 0 {
		return nil, nil
	}
	var newLines []string
	if b, ferr := s.ShowFile(ctx, set.Tip, path); ferr == nil {
		newLines = splitLines(b)
	}
	// newLines stays nil when the path is gone from the tip: resolveOne then
	// reports orphaned, and keepResolved hides those — exactly the rule the
	// ordinary note path follows for a deleted file.
	return keepResolved(resolveNotes(mine, nil, newLines)), nil
}

// previewCountEntry is one cached count result.
type previewCountEntry struct {
	byPath map[string]int
	total  int
}

// PreviewNoteCounts are the preview's badges: root notes per path and in
// total. HIDDEN notes count (spec §1.2): an orphan — the path gone from the
// tip, or the note's commit rebased off the branch — never draws a row, but
// the Previews panel still says it is there, like hunk's "retired file" count.
// So these are counted from the STORE, without resolving anything.
//
// The maps are the cached instance, shared by every caller: READ-ONLY.
func (s *Service) PreviewNoteCounts(ctx context.Context, set PreviewNoteSet) (map[string]int, int, error) {
	if !set.OK() {
		return map[string]int{}, 0, nil
	}
	key := set.Tip + ":" + set.Base
	s.mu.Lock()
	if e, ok := s.previewCounts[key]; ok {
		s.mu.Unlock()
		return e.byPath, e.total, nil
	}
	s.mu.Unlock()

	mine, err := s.loadPreviewNotes(ctx, set, "")
	if err != nil {
		return map[string]int{}, 0, err
	}
	e := previewCountEntry{byPath: map[string]int{}}
	for _, n := range mine {
		if n.IsReply() { // a badge counts THREADS
			continue
		}
		e.total++
		if n.Address.Path != "" {
			e.byPath[n.Address.Path]++
		}
	}
	s.mu.Lock()
	if s.previewCounts == nil {
		s.previewCounts = map[string]previewCountEntry{}
	}
	s.previewCounts[key] = e
	s.mu.Unlock()
	return e.byPath, e.total, nil
}
```

Add `"github.com/homeend/gigagit/internal/model"` to the file's imports.

- [ ] **Step 7: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-preview-notes && go test ./internal/domain/ -v`
Expected: PASS (the whole package — the `NotesFor`/`NotesAt` refactor must have broken nothing).

- [ ] **Step 8: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-preview-notes && git add internal/domain/previewnotes.go internal/domain/previewnotes_test.go internal/domain/notes.go internal/domain/service.go && git commit -m "feat(domain): gather and resolve a preview's notes through the one resolver

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 3: TUI — the preview diff view (stamp, gates, refusal, outdated, remove-all)

**Files:**
- Modify: `internal/tui/diff_view.go:87` (the struct), `:615-624` (the compare loader), `internal/tui/files_view.go:684-714` (`openDiffForFileLine`), `internal/tui/model.go:414-422` (`diffMsg`), `internal/tui/note_keys.go:46-63,79-96,356-361`, `internal/tui/note_popup.go:51-72`, `internal/tui/note_remove_all_popup.go:60-125`, `internal/tui/diff_notes.go:222-243`
- Modify: `internal/i18n/lang/{ja,ko,zh,ru}.toml`
- Test: `internal/tui/preview_notes_test.go` (new)

**Interfaces:**
- Consumes: `domain.PreviewNoteSet`, `(*Service).PreviewNotesFor`, `PreviewStatus` (Task 2).
- Produces:
  - `diffView.previewSet *domain.PreviewNoteSet` — non-nil ⇒ this diff is a preview's.
  - `Model.filesPreviewSet *domain.PreviewNoteSet` and `Model.filesPreviewCounts map[string]int` — the files view's copy, consumed by Task 4's badges.
  - `func (m Model) previewNoteSet() *domain.PreviewNoteSet` — the set of the diff on top, or nil.
  - `func (v *diffView) inheritIdentity(from *diffView)` — the loader's identity copy (context, rev, noteAddr, previewSet).

#### The `note_keys.go` / note-surface gate table (ruling 5)

The rule, applied everywhere: **`v.compare` stays true** (a preview IS a two-sided compare, so the edit/bookmark refusals that key on it are still correct), **`v.rev` is already the source tip** (`openCompareFiles` sets `m.filesHash = right.Hash`; `openDiffForFileLine` copies it into `rev`), and the view additionally carries **`noteAddr = {StateCommitted, Tip, Path}`** plus **`previewSet != nil`**.

| # | Gate (file:line) | Keyed on | Today | Preview behaviour |
|---|---|---|---|---|
| 1 | `diffNoteAddress` `note_keys.go:35-41` | `v.noteAddr.Path` | compare view has no address → notes inert | address is stamped → notes live. **No code change** |
| 2 | `loadNotesCmd` `note_keys.go:46-63` | `v.noteAddr` | `svc.NotesFor(addr, diff)` | `svc.PreviewNotesFor(*set, addr.Path, diff)` when `previewSet != nil` |
| 3 | `notedFilePath` `note_keys.go:356-361` (`}`/`{` file step) | `v.rev != ""` → `noteCounts.ByCommitPath[rev+":"+path]` | tip-only counts | read `m.filesPreviewCounts[path]` when `previewSet != nil` |
| 4 | `noteAnchorsAtCursor` `note_keys.go:79-96` | cursor row sides | offers new + old | drop the old-side anchor when `previewSet != nil` |
| 5 | `openNotePopup(noteAdd)` `note_popup.go:58-63` | `len(p.anchors)` | inert when empty | empty after the filter ⇒ status notice `notes in a preview anchor on the new side`. Filtering to one anchor also disarms `hasSideField` (`note_popup.go:106`), so the side toggle disappears with no extra code |
| 6 | `noteRemoveAllRow` / `openNoteRemoveAll` `note_remove_all_popup.go:44,70` | `diffHasNotes()` + `diffNoteAddress()` | `roots: len(v.notes)` | count only the notes whose commit IS the tip (that is all `NotesClear(addr)` removes) and render `Remove all notes on %s (%d of %d).` |
| 7 | `noteBoxTitle` `diff_notes.go:222-243` | `r.Status == model.NoteStale` | appends `(stale)` | appends `(outdated)` and prefixes `⊘ ` when `previewSet != nil` |
| 8 | `steerHighlight` `steer_attn.go:128-134` | `c.Side` | accepts `old` | refuse `old` on a preview view (Task 4) |
| 9 | `attnKeyFor(v.noteAddr)` `steer_attn.go:101` | `noteAddr` | keys on path+state+commit | keys on the TIP once stamped — correct. **No code change** |
| 10 | `contextLinkText` `link.go:104-112` (`L` copy link) | `diffNoteAddress()` | refuses a compare | copies the TIP's commit link once stamped — spec §1.3 says exactly this. **No code change** |
| 11 | `diff_edit.go:39`, `bookmark.go:34` | `v.compare` | refuse on a compare | still refuse. **No code change** |

- [ ] **Step 1: Write the failing test**

Create `internal/tui/preview_notes_test.go`:

```go
package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/textdiff"
)

// previewDiffModel builds a Model whose top layer is a preview diff view: a
// two-sided compare stamped with a tip address and a preview set.
func previewDiffModel(t *testing.T, notes []domain.ResolvedNote) Model {
	t.Helper()
	const tip = "1111111111111111111111111111111111111111"
	set := &domain.PreviewNoteSet{Source: "feat", Target: "main", Tip: tip, Commits: []string{tip}}
	v := &diffView{
		title: "a.txt", compare: true, rev: tip, width: 100,
		noteAddr:   model.FileAddress{State: model.StateCommitted, Commit: tip, Path: "a.txt"},
		previewSet: set,
		notes:      notes,
		full: []textdiff.Row{
			{Kind: textdiff.RowSame, Left: "alpha", Right: "alpha", LeftNo: 1, RightNo: 1},
			{Kind: textdiff.RowDel, Left: "gone", LeftNo: 2},
			{Kind: textdiff.RowAdd, Right: "added", RightNo: 2},
		},
	}
	v.rebuild()
	m := Model{width: 100, height: 40}
	m.filesPreviewSet = set
	m = m.pushLayer(v) // pushLayer returns Model, not tea.Model — no assertion
	return m
}

// Gate 4 + 5: the cursor on an old-side-only row offers no anchor at all, and
// `c` there posts the refusal notice instead of opening the form.
func TestPreviewDiffRefusesAnOldSideNote(t *testing.T) {
	t.Parallel()
	m := previewDiffModel(t, nil)
	v := m.diffLayer()
	// Put the cursor on the Del row (old side only).
	for i, ln := range v.lines {
		if ln.Row.LeftNo == 2 && ln.Row.RightNo == 0 {
			v.curLine = i
		}
	}
	if as := m.noteAnchorsAtCursor(); len(as) != 0 {
		t.Fatalf("a preview offers no old-side anchor, got %+v", as)
	}
	tm, _ := m.openNotePopup(noteAdd)
	m2 := tm.(Model)
	if _, ok := m2.topLayer().(*notePopup); ok {
		t.Fatal("the note form must not open on an old-side preview row")
	}
	if m2.statusMsg != "notes in a preview anchor on the new side" {
		t.Fatalf("want the refusal notice, got %q", m2.statusMsg)
	}
}

// Gate 4: a row that exists on both sides offers ONLY the new side.
func TestPreviewDiffOffersOnlyTheNewSide(t *testing.T) {
	t.Parallel()
	m := previewDiffModel(t, nil)
	v := m.diffLayer()
	for i, ln := range v.lines {
		if ln.Row.RightNo == 1 && ln.Row.LeftNo == 1 {
			v.curLine = i
		}
	}
	as := m.noteAnchorsAtCursor()
	if len(as) != 1 || as[0].side != model.NoteSideNew {
		t.Fatalf("want exactly one new-side anchor, got %+v", as)
	}
}

// Gate 7: a stale note renders as outdated, with the ⊘ marker, in a preview.
func TestPreviewDiffTitleSaysOutdated(t *testing.T) {
	t.Parallel()
	r := domain.ResolvedNote{
		Note:   model.Note{ID: "n1", Source: model.NoteSourceAgent, Author: "ada", Side: model.NoteSideNew, Summary: "why"},
		Status: model.NoteStale, Range: [2]int{1, 1},
	}
	m := previewDiffModel(t, []domain.ResolvedNote{r})
	title := m.diffLayer().noteBoxTitle(r)
	if !contains(title, "(outdated)") || !contains(title, "⊘") {
		t.Fatalf("a preview marks a stale note outdated with ⊘, got %q", title)
	}
	if contains(title, "(stale)") {
		t.Fatalf("a preview must not say stale, got %q", title)
	}
	// A NON-preview view keeps the old word.
	plain := &diffView{noteAddr: model.FileAddress{Path: "a.txt"}}
	if !contains(plain.noteBoxTitle(r), "(stale)") {
		t.Fatal("a plain commit diff still says (stale)")
	}
}

// The compare loader builds a FRESH diffView and diffMsg then does
// `*dv = *msg.view` — so anything the opener stamped is lost unless the loader
// inherits it. This is the one trap in the whole task, so it is tested on the
// inheritance step itself, not on a view the test already filled in.
func TestCompareLoaderInheritsThePreviewStamp(t *testing.T) {
	t.Parallel()
	opener := previewDiffModel(t, nil).diffLayer()
	fresh := &diffView{title: "a.txt"} // what loadCompareDiffCmd starts from
	fresh.inheritIdentity(opener)
	if fresh.previewSet != opener.previewSet {
		t.Fatal("the loader must carry the preview set across the rebuild")
	}
	if fresh.noteAddr != opener.noteAddr {
		t.Fatalf("the loader must carry the note address, got %+v", fresh.noteAddr)
	}
	if fresh.rev != opener.rev || fresh.context != opener.context {
		t.Fatal("the loader must keep carrying rev and context, as it did before")
	}
	// A nil opener (a fresh open, no layer yet) must be a no-op, not a panic.
	(&diffView{}).inheritIdentity(nil)
}

func contains(s, sub string) bool { return len(s) >= len(sub) && (len(sub) == 0 || indexOf(s, sub) >= 0) }

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```

If `internal/tui` already has a `contains`/`indexOf` helper (check with `grep -rn "func contains(" internal/tui/`), delete the two here and use the existing one. Check the real `textdiff.Row` field/constant names with `grep -n "RowSame\|RowAdd\|RowDel" internal/textdiff/*.go` and adapt the fixture — do not invent names.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-preview-notes && go test ./internal/tui/ -run "TestPreviewDiff|TestCompareLoaderInherits" -v`
Expected: FAIL — `previewSet`, `filesPreviewSet` and `inheritIdentity` undefined.

- [ ] **Step 3: Add the fields and the accessor**

In `internal/tui/diff_view.go`, directly after `noteAddr model.FileAddress` (line 87):

```go
	// previewSet is non-nil when this diff is one file of a MERGE PREVIEW
	// (target...source). It changes three things and nothing else: notes are
	// gathered along the branch (PreviewNotesFor) rather than read off the
	// tip alone, the old side is not addressable (the merge base is nobody's
	// stored old side), and a stale note renders as "outdated" — in a preview
	// that is the expected state, not an edge case. noteAddr still names the
	// TIP, so every other note surface works unchanged.
	previewSet *domain.PreviewNoteSet
```

Add `"github.com/homeend/gigagit/internal/domain"` to that file's imports if it is not already there.

In `internal/tui/model.go`, beside `previews []previewRow` (line 142):

```go
	// filesPreviewSet / filesPreviewCounts are the open preview's note scope
	// and its per-path badge counts; nil/empty when the files view is not
	// showing a preview. Stamped onto each diff the view opens.
	filesPreviewSet    *domain.PreviewNoteSet
	filesPreviewCounts map[string]int
```

Add to `internal/tui/note_keys.go` (after `diffNoteAddress`, line 41):

```go
// previewNoteSet is the merge-preview scope of the diff on top, or nil. Like
// diffNoteAddress it reads the field the LOADER stamped, never Model state at
// key time.
func (m Model) previewNoteSet() *domain.PreviewNoteSet {
	v := m.diffLayer()
	if v == nil {
		return nil
	}
	return v.previewSet
}
```

- [ ] **Step 4: Stamp the view, and keep the stamp**

`internal/tui/files_view.go`, in `openDiffForFileLine`'s compare branch (lines 711-714), replace:

```go
	if m.inCompareMode() {
		m.diffLayer().context = m.filesContext
		m.diffTag = "cmp:" + m.filesLeft.CacheTag() + ":" + m.filesRight.CacheTag() + ":" + l.path
		return m, m.loadCompareDiffCmd(m.filesLeft, m.filesRight, l)
	}
```

with:

```go
	if m.inCompareMode() {
		dv := m.diffLayer()
		dv.context = m.filesContext
		// A merge preview is a compare whose NEW side is the source tip, so
		// its rows ARE note-addressable at that commit — unlike every other
		// compare, whose old side no stored address names. Stamp the address
		// and the set here, where the opener knows which compare this is.
		if set := m.filesPreviewSet; set != nil {
			dv.previewSet = set
			dv.noteAddr = model.FileAddress{State: model.StateCommitted, Commit: set.Tip, Path: l.path}
		}
		m.diffTag = "cmp:" + m.filesLeft.CacheTag() + ":" + m.filesRight.CacheTag() + ":" + l.path
		return m, m.loadCompareDiffCmd(m.filesLeft, m.filesRight, l)
	}
```

`internal/tui/diff_view.go:622-624` — the compare loader builds a FRESH view and copies only `context, rev` from the layer, and `model.go:414-422` then does `*dv = *msg.view`. Without this the stamp is wiped the moment the diff lands. Extract the copy so it is testable on its own; add beside `loadCompareDiffCmd`:

```go
// inheritIdentity carries the OPENER's identity onto a freshly built view.
// The loaders construct their view with no Model to ask, and diffMsg replaces
// the whole value (`*dv = *msg.view`), so anything the opener stamped — the
// context line, the provenance rev, the note address, the merge-preview set —
// has to travel here or it is silently lost the moment the diff lands.
// A nil source is a fresh open with no layer yet: a no-op.
func (v *diffView) inheritIdentity(from *diffView) {
	if from == nil {
		return
	}
	v.context, v.rev = from.context, from.rev
	v.noteAddr, v.previewSet = from.noteAddr, from.previewSet
}
```

and replace lines 622-624 with:

```go
	v.inheritIdentity(m.diffLayer())
```

- [ ] **Step 5: Route the note load through the preview resolver (gate 2)**

`internal/tui/note_keys.go:57-62`, inside `loadNotesCmd`:

```go
	svc, tag, rows := m.svc, m.diffTag, v.full
	set := v.previewSet
	return func() tea.Msg {
		d := domain.Diff{Result: textdiff.Result{Rows: rows}}
		if set != nil {
			ns, err := svc.PreviewNotesFor(context.Background(), *set, addr.Path, d)
			return notesLoadedMsg{tag: tag, notes: ns, err: err}
		}
		ns, err := svc.NotesFor(context.Background(), addr, d)
		return notesLoadedMsg{tag: tag, notes: ns, err: err}
	}
```

- [ ] **Step 6: The old-side guard (gates 4 and 5)**

`internal/tui/note_keys.go:88-95`, at the end of `noteAnchorsAtCursor`:

```go
	var out []noteAnchor
	if r.RightNo > 0 {
		out = append(out, noteAnchor{model.NoteSideNew, r.RightNo, model.NoteContextHash([]string{r.Right})})
	}
	// A preview's old side is the MERGE BASE, which no stored address names,
	// so it offers no anchor at all — the same rule `gg review A..B` follows
	// (reviewImportTarget: ranges anchor to the tip, new side only).
	if r.LeftNo > 0 && v.previewSet == nil {
		out = append(out, noteAnchor{model.NoteSideOld, r.LeftNo, model.NoteContextHash([]string{r.Left})})
	}
	return out
```

`internal/tui/note_popup.go:58-63`, inside `openNotePopup`'s `noteAdd` case:

```go
	case noteAdd:
		p.anchors = m.noteAnchorsAtCursor()
		if len(p.anchors) == 0 {
			// On a preview the cause is knowable and worth saying: the cursor
			// is on a line that exists only on the merge-base side.
			if m.previewNoteSet() != nil {
				m.statusMsg = i18n.T("notes in a preview anchor on the new side")
			}
			return m, nil
		}
```

- [ ] **Step 7: Outdated rendering (gate 7)**

`internal/tui/diff_notes.go`, in `noteBoxTitle` — replace the trailing `(stale)` block (lines 240-243):

```go
	t += " · " + v.noteAddr.Path + " " + sideMark + strconv.Itoa(r.Range[1])
	if r.Status == model.NoteStale {
		if v.previewSet != nil {
			// In a preview a note whose lines a later commit changed is the
			// EXPECTED case, so it is named plainly and marked in the gutter
			// rather than whispered as an edge condition.
			return "⊘ " + t + " " + i18n.T("(outdated)")
		}
		t += " " + i18n.T("(stale)")
	}
	return t
```

- [ ] **Step 8: Remove-all scoping (gate 6)**

`internal/tui/note_remove_all_popup.go` — add the tip-scoped count to the struct (after `replies int`, line 37):

```go
	total   int    // every root the preview shows, tip and older commits alike
	tip     string // "" = not a preview; else the short sha the clear is scoped to
```

In `openNoteRemoveAll` (line 70), replace the counting block:

```go
	p := &noteRemoveAllPopup{field: newTextField(""), addr: addr, path: addr.Path}
	for _, r := range v.notes {
		p.total++
		// NotesClear takes ONE address: on a preview that is the tip, so a
		// note gathered from an older commit is not removed and must not be
		// counted as if it were.
		if v.previewSet == nil || r.Note.Address.Commit == addr.Commit {
			p.roots++
			p.replies += len(r.Replies)
		}
	}
	if v.previewSet != nil {
		p.tip = shortHash(addr.Commit)
	}
```

The row itself must disappear when nothing it can remove is on the tip — offering it and then doing nothing is exactly the silent no-op this file's own comments forbid. Add the tip-scoped test to `noteRemoveAllRow` (line 44):

```go
func (m Model) noteRemoveAllRow() (actionRow, bool) {
	if !m.diffHasNotes() {
		return actionRow{}, false
	}
	addr, ok := m.diffNoteAddress()
	if !ok {
		return actionRow{}, false
	}
	// On a preview the visible notes may ALL come from older commits, which
	// NotesClear(addr) — one address, the tip — cannot touch. Offering the
	// row there would be a gesture that does nothing.
	if v := m.diffLayer(); v != nil && v.previewSet != nil && !diffHasTipNotes(v, addr.Commit) {
		return actionRow{}, false
	}
	return actionRow{
		id:    "note-remove-all",
		label: i18n.T("Remove all notes…"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.openNoteRemoveAll()
		},
	}, true
}

// diffHasTipNotes reports whether any visible thread is anchored on commit.
func diffHasTipNotes(v *diffView, commit string) bool {
	for _, r := range v.notes {
		if r.Note.Address.Commit == commit {
			return true
		}
	}
	return false
}
```

In `box` (line 122), before the existing `lead` assignment:

```go
	lead := i18n.T("This deletes %d notes from %s.", p.roots, p.path)
	if p.replies > 0 {
		lead = i18n.T("This deletes %d notes (%d replies included) from %s.", p.roots+p.replies, p.replies, p.path)
	}
	if p.tip != "" {
		// A preview gathers notes along the branch but clears only the tip's,
		// so the question says which slice of the total is about to go.
		lead = i18n.T("Remove all notes on %s (%d of %d).", p.tip, p.roots+p.replies, p.total)
	}
```

- [ ] **Step 9: Add the three keys to all four bundles**

Append this block to `internal/i18n/lang/ja.toml`:

```toml

# Notes in merge previews
"notes in a preview anchor on the new side" = "プレビューのノートは新しい側にアンカーされます"
"(outdated)" = "(古い)"
"Remove all notes on %s (%d of %d)." = "%s のノートをすべて削除します (%d / %d)。"
```

`internal/i18n/lang/ko.toml`:

```toml

# Notes in merge previews
"notes in a preview anchor on the new side" = "미리보기의 노트는 새 쪽에 고정됩니다"
"(outdated)" = "(오래됨)"
"Remove all notes on %s (%d of %d)." = "%s의 노트를 모두 삭제합니다 (%d / %d)."
```

`internal/i18n/lang/zh.toml`:

```toml

# Notes in merge previews
"notes in a preview anchor on the new side" = "预览中的笔记锚定在新的一侧"
"(outdated)" = "(已过时)"
"Remove all notes on %s (%d of %d)." = "删除 %s 上的所有笔记（%d / %d）。"
```

`internal/i18n/lang/ru.toml`:

```toml

# Notes in merge previews
"notes in a preview anchor on the new side" = "заметки в предпросмотре привязываются к новой стороне"
"(outdated)" = "(устарело)"
"Remove all notes on %s (%d of %d)." = "Удалить все заметки на %s (%d из %d)."
```

All three keep their `%`-verb order, so no `%[n]s` reordering is needed (`render_order_test.go` checks exactly that).

- [ ] **Step 10: Run the TUI tests**

Run (FOREGROUND, ~2 min): `cd /mnt/t/others/gigagit.worktrees/feat-preview-notes && go test ./internal/tui/ ./internal/i18n/`
Expected: PASS, including the i18n AST gates (`i18n_scan_test.go`, `menu_labels_test.go`).

- [ ] **Step 11: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-preview-notes && git add internal/tui/diff_view.go internal/tui/files_view.go internal/tui/model.go internal/tui/note_keys.go internal/tui/note_popup.go internal/tui/note_remove_all_popup.go internal/tui/diff_notes.go internal/tui/preview_notes_test.go internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml internal/i18n/lang/zh.toml internal/i18n/lang/ru.toml && git commit -m "feat(tui): a merge-preview diff carries notes — tip address, gathered set, outdated marker, new-side-only add

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 4: TUI — badges, the refresh chain, and the steering guard

**Files:**
- Modify: `internal/tui/preview_open.go:27-34,53-62,118-148`, `internal/tui/preview_panel.go:13-36,51-70`, `internal/tui/files_view.go:40-49` (`closeFilesView` reset), `internal/tui/note_keys.go:354-361`, `internal/tui/model.go:1363-1371` (`srcNotes` arrival), `internal/tui/viewstate.go:279`, `internal/tui/steer_attn.go:123-137`
- Test: `internal/tui/preview_notes_test.go` (append)

**Interfaces:**
- Consumes: `Model.filesPreviewSet`, `Model.filesPreviewCounts`, `diffView.previewSet` (Task 3); `(*Service).PreviewNotes`, `(*Service).PreviewNoteCounts` (Task 2).
- Produces:
  - `previewRow.notes int` — the total for the Previews panel badge.
  - `previewOpenMsg.set domain.PreviewNoteSet` and `previewOpenMsg.counts map[string]int`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/preview_notes_test.go`:

```go
// The Previews panel row carries the SAME ◆N badge every other note-bearing
// row uses (never a new glyph).
func TestPreviewRowCarriesANoteBadge(t *testing.T) {
	t.Parallel()
	m := Model{previews: []previewRow{{
		rec:   model.MergePreview{ID: "p1", Label: "login", Source: "feat", Target: "main"},
		sum:   domain.PreviewSummary{State: domain.PreviewOK, Files: 2, Ahead: 3},
		notes: 4,
	}}}
	rows := m.previewRows()
	if len(rows) != 1 || !contains(rows[0], noteBadge(4)) {
		t.Fatalf("want the ◆4 badge on the preview row, got %q", rows[0])
	}
}

// Ruling 6: a non-ok pair shows no badge at all.
func TestPreviewRowWithoutNotesHasNoBadge(t *testing.T) {
	t.Parallel()
	m := Model{previews: []previewRow{{
		rec: model.MergePreview{ID: "p1", Label: "merged", Source: "feat", Target: "main"},
		sum: domain.PreviewSummary{State: domain.PreviewMerged},
	}}}
	if rows := m.previewRows(); contains(rows[0], "◆") {
		t.Fatalf("a merged pair carries no note badge, got %q", rows[0])
	}
}

// Gate 3: }/{ steps to the next file that carries PREVIEW notes.
func TestNotedFilePathUsesThePreviewCounts(t *testing.T) {
	t.Parallel()
	m := previewDiffModel(t, nil)
	m.filesPreviewCounts = map[string]int{"b.txt": 2}
	if m.notedFilePath("b.txt") != true {
		t.Fatal("a file with preview notes must be a }/{ target")
	}
	if m.notedFilePath("c.txt") != false {
		t.Fatal("a file without preview notes must not be")
	}
}

// After a note write the pair's HASHES are unchanged, so afterPreviewsRefresh
// takes its early-return branch — which must still take the fresh counts, or
// the file-list badges and }/{ go stale in the only case that matters.
func TestUnchangedPreviewRefreshStillMovesTheCounts(t *testing.T) {
	t.Parallel()
	m := previewDiffModel(t, nil)
	m.filesView = &contentPopup{}
	m.filesPreviewCounts = map[string]int{}
	m.previewOpen = &previewOpenState{id: "p1", source: "feat", target: "main",
		srcHash: "src", tgtHash: "tgt"}
	m.previews = []previewRow{{
		rec:    model.MergePreview{ID: "p1", Source: "feat", Target: "main"},
		sum:    domain.PreviewSummary{State: domain.PreviewOK, SourceHash: "src", TargetHash: "tgt"},
		notes:  1,
		byPath: map[string]int{"a.txt": 1},
	}}
	m2, _ := m.afterPreviewsRefresh()
	if m2.filesPreviewCounts["a.txt"] != 1 {
		t.Fatalf("an unchanged-hash refresh must still take the fresh counts, got %v", m2.filesPreviewCounts)
	}
}

// Ruling 4: steering may not mark the old side of a preview.
func TestSteerHighlightRefusesTheOldSideOfAPreview(t *testing.T) {
	t.Parallel()
	m := previewDiffModel(t, nil)
	m2, _ := m.steerHighlight(steer.Command{ID: "1", Verb: "highlight", Action: "add",
		File: "a.txt", Side: "old", Start: 1, Tone: "info"})
	if len(m2.attention) != 0 {
		t.Fatal("an old-side mark must be refused on a preview view")
	}
}
```

Add the `steer` import. Check the real `steer.Command` field names with `grep -n "type Command struct" -A 25 internal/steer/*.go` and adapt the literal — do not invent fields. If `steerHighlight` needs a resolvable attention key (see `resolveAttnKey`), give the fixture whatever the existing `steer_attn_test.go` cases give theirs; copy that setup rather than inventing one.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-preview-notes && go test ./internal/tui/ -run "TestPreviewRow|TestNotedFilePath|TestSteerHighlightRefuses|TestUnchangedPreviewRefresh" -v`
Expected: FAIL — `previewRow.notes`/`previewRow.byPath` undefined, `filesPreviewCounts` unread.

- [ ] **Step 3: Count notes with the summary (Previews panel)**

`internal/tui/preview_panel.go` — add the field to `previewRow` (line 13):

```go
type previewRow struct {
	rec   model.MergePreview
	sum   domain.PreviewSummary
	notes int            // root notes gathered along the branch, hidden ones included
	byPath map[string]int // the same counts per path; feeds the open preview's file list
	err   error
}
```

and fill both in `readPreviews` (lines 30-35):

```go
	rows := make([]previewRow, 0, len(ps))
	for _, p := range ps {
		sum, err := svc.PreviewSummary(ctx, p.Source, p.Target)
		row := previewRow{rec: p, sum: sum, err: err}
		// Ruling 6: a pair that is not previewable has no note scope at all,
		// so it gets no badge and costs no store read.
		if err == nil && sum.State == domain.PreviewOK {
			if set, serr := svc.PreviewNotes(ctx, p.Source, p.Target); serr == nil {
				if byPath, total, cerr := svc.PreviewNoteCounts(ctx, set); cerr == nil {
					row.notes, row.byPath = total, byPath
				}
			}
		}
		rows = append(rows, row)
	}
```

and paint it in `previewRows` (line 83) — reusing `noteBadge`, never a new glyph:

```go
		out = append(out, padCell(r.rec.Label, w)+"  "+pair+"  "+previewStateText(r)+noteBadge(r.notes))
```

- [ ] **Step 4: Carry the set onto the open preview**

`internal/tui/preview_open.go` — add to `previewOpenMsg` (line 27):

```go
	set    domain.PreviewNoteSet // the note scope; zero when the pair is not ok
	counts map[string]int        // per-path root-note counts for the file list
```

and fill both in `reopenPreviewCmd` (lines 55-61):

```go
	return func() tea.Msg {
		eps, err := svc.PreviewOpen(context.Background(), source, target)
		msg := previewOpenMsg{
			id: id, source: source, target: target,
			keepPath: keepPath, moved: moved, gen: gen, eps: eps, err: err,
		}
		// The note scope rides the SAME resolve, so the file list paints its
		// badges in the first frame rather than after a second round trip.
		if err == nil && eps.Summary.State == domain.PreviewOK {
			if set, serr := svc.PreviewNotes(context.Background(), source, target); serr == nil {
				msg.set = set
				msg.counts, _, _ = svc.PreviewNoteCounts(context.Background(), set)
			}
		}
		return msg
	}
```

In `handlePreviewOpenMsg`, arm the Model AFTER `openCompareFiles` (which runs `closeFilesView` and would clear them) — extend the block at lines 139-144:

```go
	m.previewGen = msg.gen
	if msg.set.OK() {
		set := msg.set
		m.filesPreviewSet = &set
		m.filesPreviewCounts = msg.counts
	}
	m.previewOpen = &previewOpenState{
```

And in the same-tag early return (lines 119-126), refresh the counts without reopening:

```go
		po.srcHash, po.tgtHash = msg.eps.Summary.SourceHash, msg.eps.Summary.TargetHash
		if msg.set.OK() {
			set := msg.set
			m.filesPreviewSet, m.filesPreviewCounts = &set, msg.counts
		}
		return m, nil
```

`internal/tui/files_view.go` — clear both in `closeFilesView`, beside `m.comparePair = nil` (line 49):

```go
	m.filesPreviewSet = nil
	m.filesPreviewCounts = nil
```

- [ ] **Step 5: Badge the preview's file list, and the `}`/`{` step**

`internal/tui/viewstate.go:279` reads `l.notes[path]`; fill that map from the preview counts. Find where the compare file list's `notes` map is populated (`grep -n "notes:" internal/tui/viewstate.go internal/tui/files_view.go`) and, in the compare branch, use:

```go
	// A preview's per-file badge counts the notes gathered along the branch,
	// not the tip's alone — so the file list and the diff agree.
	notes := m.noteCounts.ByPath
	if m.filesPreviewSet != nil {
		notes = m.filesPreviewCounts
	}
```

`internal/tui/note_keys.go:356-361` (gate 3):

```go
func (m Model) notedFilePath(path string) bool {
	if m.filesPreviewSet != nil {
		// A preview gathers notes from every commit on the branch, so the
		// tip-keyed ByCommitPath map would miss most of them.
		return m.filesPreviewCounts[path] > 0
	}
	if v := m.diffLayer(); v != nil && v.rev != "" {
		return m.noteCounts.ByCommitPath[v.rev+":"+path] > 0
	}
	return m.noteCounts.ByPath[path] > 0
}
```

- [ ] **Step 6: Refresh the badges on every note mutation (ruling 7 + 8)**

`internal/tui/model.go`'s `dataAvailableMsg` handler already ends with `return m, previewsChain` (line 1395) — there IS an existing chain variable for the branches/remotes arrival. Find it (`grep -n "previewsChain" internal/tui/model.go`) and add `srcNotes` to what arms it, rather than building a parallel `tea.Batch`. Preview badges count the SAME store, so a note write moves them; always through `chainPreviewsRead()`, never a plain `reloadSourcesCmd` (ruling 8: a silent read superseding a manual one strands `srcLoading[srcPreviews]`). Arm it only when a preview is actually on screen — otherwise every note write in every repo spends a git resolve per saved pair:

```go
	// where previewsChain is armed, alongside the existing branches/remotes arm:
	if msg.source == srcNotes && (m.previewOpen != nil || m.activeLeftTab == panelPreviews) {
		m, previewsChain = m.chainPreviewsRead()
	}
```

Never dispatch this from `reRoot`'s batch (ruling 8).

Then make the arrival actually move the OPEN preview's counts. `afterPreviewsRefresh` (`internal/tui/preview_open.go:187-189`) currently early-returns for a saved pair whose hashes are unchanged — which is exactly the case after a note write, so without this the file-list badges and the `}`/`{` step go stale. Replace that branch:

```go
				if r.sum.SourceHash == po.srcHash && r.sum.TargetHash == po.tgtHash {
					// The pair did not move, but its NOTES may have (this
					// refresh was chained off srcNotes). Take the fresh counts
					// without re-resolving or re-opening anything.
					m.filesPreviewCounts = r.byPath
					return m, nil
				}
```

- [ ] **Step 7: The steering guard (gate 8, ruling 4)**

`internal/tui/steer_attn.go`, in `steerHighlight` right after the `side != "new" && side != "old"` check (line 134):

```go
	// A preview's old side is the merge base: not addressable, the same
	// refusal the TUI's own `c` gives there.
	if side == "old" && m.previewNoteSet() != nil {
		return m, m.answerSteer(c, steerFail(c, "notes in a preview anchor on the new side"))
	}
```

(The steer wire is agent-facing protocol, so this message stays English — it is not routed through `i18n.T`.)

- [ ] **Step 8: Run the TUI tests**

Run (FOREGROUND, ~2 min): `cd /mnt/t/others/gigagit.worktrees/feat-preview-notes && go test ./internal/tui/`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-preview-notes && git add internal/tui/preview_open.go internal/tui/preview_panel.go internal/tui/files_view.go internal/tui/note_keys.go internal/tui/model.go internal/tui/viewstate.go internal/tui/steer_attn.go internal/tui/preview_notes_test.go && git commit -m "feat(tui): preview note badges, refresh chain and the old-side steering refusal

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 5: CLI — the `--preview` flag and the preview diff

**Files:**
- Create: `internal/cli/previewflag.go`
- Modify: `internal/cli/diff.go:21-96`, `internal/cli/preview.go:167-181` (`previewDiff`)
- Test: `internal/cli/previewflag_test.go` (new), `internal/cli/diff_test.go` (append)

**Interfaces:**
- Consumes: `(*Service).PreviewResolve`, `(*Service).PreviewNotes` (Tasks 1-2); `(*Service).PreviewSummary`/`PreviewOpen`, `HunkDiffSpec` (`internal/domain/hunks.go:61` — an explicit `A...B` rev passes through unchanged).
- Produces:
  - `type previewFlag struct{ spec *string }`
  - `func addPreviewFlag(fs *flag.FlagSet) previewFlag`
  - `func (pf previewFlag) set() bool`
  - `func resolvePreviewTarget(ctx context.Context, svc *domain.Service, spec string) (previewTarget, error)`
  - `type previewTarget struct { Source, Target string; Set domain.PreviewNoteSet; Spec model.DiffSpec }` — `Spec.Rev` is `"<targetHash>...<sourceHash>"` (ruling 1).
  - `func previewUsageErr(verb string, stderr io.Writer) int` — prints `<verb>: one target only (--preview cannot be combined with --rev, --cached or a gg:// link)` and returns 2.

- [ ] **Step 1: Write the failing test**

Create `internal/cli/previewflag_test.go`:

```go
package cli

import (
	"context"
	"strings"
	"testing"
)

// The three-dot form needs no saved record, and the resolved DiffSpec is the
// PREVIEW's patch, built by the one constructor (set.DiffSpec()) — so hunk
// numbers under --preview match `gg diff --preview --hunks` and are never the
// tip commit's own parent→tip numbering.
func TestResolvePreviewTargetThreeDotForm(t *testing.T) {
	svc, dir := newCLIPreviewRepo(t) // helper below
	tgt, err := resolvePreviewTarget(context.Background(), svc, "main...feat")
	if err != nil {
		t.Fatal(err)
	}
	if tgt.Source != "feat" || tgt.Target != "main" {
		t.Fatalf("got %s/%s", tgt.Source, tgt.Target)
	}
	if !tgt.Set.OK() || tgt.Set.Tip != revParseCLI(t, dir, "feat") {
		t.Fatalf("the set's tip must be feat's tip, got %+v", tgt.Set)
	}
	if tgt.Spec.Rev != tgt.Set.DiffSpec().Rev {
		t.Fatalf("the CLI must not build its own range: %q vs %q", tgt.Spec.Rev, tgt.Set.DiffSpec().Rev)
	}
	// The preview's patch runs merge-base → tip, NOT parent(tip) → tip.
	if !strings.HasPrefix(tgt.Spec.Rev, revParseCLI(t, dir, "main")) {
		t.Fatalf("the range must start at the merge base, got %q", tgt.Spec.Rev)
	}
}

func TestDiffPreviewRefusesASecondTarget(t *testing.T) {
	svc, _ := newCLIPreviewRepo(t)
	var out, errb strings.Builder
	code := cmdDiff(svc, []string{"--preview", "main...feat", "--cached"}, &out, &errb)
	if code != 2 {
		t.Fatalf("want exit 2, got %d", code)
	}
	if !strings.Contains(errb.String(), "one target only") {
		t.Fatalf("want the one-target message, got %q", errb.String())
	}
}

func TestDiffPreviewHunksNumberThePreviewDiff(t *testing.T) {
	svc, _ := newCLIPreviewRepo(t)
	var out, errb strings.Builder
	if code := cmdDiff(svc, []string{"--preview", "main...feat", "--hunks"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "a.txt") || !strings.Contains(out.String(), "1 @@ -") {
		t.Fatalf("want numbered hunks over the preview diff, got %q", out.String())
	}
}
```

`newCLIPreviewRepo`/`revParseCLI`: build them on the existing `internal/cli` test helper that makes a real repo + `*domain.Service` (find it with `grep -n "func newTestRepo\|func newCLISvc" internal/cli/*_test.go`), seeding the same main/feat shape `newPreviewRepo` uses in Task 1.

- [ ] **Step 2: Run it to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-preview-notes && go test ./internal/cli/ -run TestDiffPreview -v`
Expected: FAIL — `resolvePreviewTarget` undefined.

- [ ] **Step 3: Write the flag helper**

Create `internal/cli/previewflag.go`:

```go
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// --preview is the ONE way an agent addresses a merge preview from the CLI:
// a saved record's id or label, or git's three-dot form <target>...<source>
// (which needs no saved record). One parser, shared by diff, note and review,
// so the three can never disagree about what a preview's diff is.

// previewTarget is a resolved --preview argument.
type previewTarget struct {
	Source, Target string
	Set            domain.PreviewNoteSet
	// Spec is the PREVIEW's diff, straight from Set.DiffSpec() — the single
	// construction of that patch (ruling 1). Hunk numbers come from it, never
	// from the tip commit's own parent→tip diff, so `gg diff --preview
	// --hunks` and `gg note add --preview --hunk N` address the same hunk.
	// Hashes, not names: the value is spliced into a git argv and rides the
	// diff cache key.
	Spec model.DiffSpec
}

type previewFlag struct{ spec *string }

func addPreviewFlag(fs *flag.FlagSet) previewFlag {
	return previewFlag{spec: fs.String("preview", "",
		"a merge preview: <id>, <label>, or <target>...<source>")}
}

func (pf previewFlag) set() bool { return pf.spec != nil && *pf.spec != "" }

// previewUsageErr is the refusal when --preview is combined with another way
// of naming a target. It is a usage error (exit 2), never a silent override.
func previewUsageErr(verb string, stderr io.Writer) int {
	fmt.Fprintf(stderr, "%s: one target only (--preview cannot be combined with --rev, --cached or a gg:// link)\n", verb)
	return 2
}

// resolvePreviewTarget resolves the argument and the pair's live state. A pair
// that is not previewable is an ERROR here (unlike the TUI/web display path,
// where it is simply nothing to show): a CLI caller asked for that diff by
// name and must not be handed an empty one.
func resolvePreviewTarget(ctx context.Context, svc *domain.Service, spec string) (previewTarget, error) {
	source, target, err := svc.PreviewResolve(ctx, spec)
	if err != nil {
		return previewTarget{}, err
	}
	sum, err := svc.PreviewSummary(ctx, source, target)
	if err != nil {
		return previewTarget{}, err
	}
	switch sum.State {
	case domain.PreviewOK:
	case domain.PreviewMissingSource:
		return previewTarget{}, fmt.Errorf("preview: missing: %s", source)
	case domain.PreviewMissingTarget:
		return previewTarget{}, fmt.Errorf("preview: missing: %s", target)
	default:
		return previewTarget{}, fmt.Errorf("preview: %s → %s: %s", source, target, sum.State)
	}
	set, err := svc.PreviewNotes(ctx, source, target)
	if err != nil {
		return previewTarget{}, err
	}
	return previewTarget{Source: source, Target: target, Set: set, Spec: set.DiffSpec()}, nil
}

// withPaths copies the spec with -- <paths> applied.
func (t previewTarget) withPaths(paths []string) model.DiffSpec {
	s := t.Spec
	s.Paths = paths
	return s
}
```

- [ ] **Step 4: Wire `gg diff --preview`**

`internal/cli/diff.go` — register the flag after `asJSON` (line 29):

```go
	pf := addPreviewFlag(fs)
```

and, right after the `fs.NArg() > 1` check (line 48), before `rev` is read:

```go
	if pf.set() {
		// Ruling 9: --preview is sugar over the existing A...B range path —
		// resolve the pair, then run the code every other range runs.
		if *cached || fs.NArg() > 0 {
			return previewUsageErr("diff", stderr)
		}
		ctx := context.Background()
		tgt, err := resolvePreviewTarget(ctx, svc, *pf.spec)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		return renderDiffSpec(ctx, svc, tgt.withPaths(paths), *hunks, *asJSON, *stat, *nameOnly, stdout, stderr)
	}
```

(The `fs.NArg() > 0` test covers both a plain rev and a `gg://` positional in one place.)

- [ ] **Step 5: Wire `gg preview diff --hunks`**

`internal/cli/preview.go`, `previewDiff` (line 167) — add `--hunks`/`--json` and accept `<id|label>` as well as the pair, so the symmetric form of ruling 1 exists:

```go
func previewDiff(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("preview diff", flag.ContinueOnError)
	fs.SetOutput(stderr)
	patch := fs.Bool("patch", false, "print unified diffs instead of the changed-file list")
	hunks := fs.Bool("hunks", false, "list each file's numbered git @@ hunks")
	asJSON := fs.Bool("json", false, "with --hunks: emit the hunk list as JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *asJSON && !*hunks {
		fmt.Fprintln(stderr, "preview diff: --json requires --hunks")
		return 2
	}
	if *hunks && *patch {
		fmt.Fprintln(stderr, "preview diff: --hunks and --patch are mutually exclusive")
		return 2
	}
	if *hunks {
		// One id/label, or the pair, numbered over the SAME patch
		// `gg diff --preview --hunks` prints.
		if fs.NArg() < 1 || fs.NArg() > 2 {
			fmt.Fprintln(stderr, "usage: gg preview diff --hunks [--json] <id|label>|<source> <target>")
			return 2
		}
		spec := fs.Arg(0)
		if fs.NArg() == 2 {
			spec = fs.Arg(1) + "..." + fs.Arg(0) // <target>...<source>
		}
		ctx := context.Background()
		tgt, err := resolvePreviewTarget(ctx, svc, spec)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		return renderDiffSpec(ctx, svc, tgt.Spec, true, *asJSON, false, false, stdout, stderr)
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(stderr, "usage: gg preview diff [--patch] <source> <target>")
		return 2
	}
	return printPreview(svc, fs.Arg(0), fs.Arg(1), *patch, stdout, stderr)
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-preview-notes && go test ./internal/cli/ -run "TestDiffPreview|TestResolvePreview|TestPreviewDiff" -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-preview-notes && git add internal/cli/previewflag.go internal/cli/previewflag_test.go internal/cli/diff.go internal/cli/preview.go && git commit -m "feat(cli): gg diff --preview and gg preview diff --hunks over the preview range

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 6: CLI — `gg note … --preview` and `gg review --preview`

**Files:**
- Modify: `internal/domain/notebatch_plan.go:52-115` (hunk-spec override), `internal/cli/note.go:264-353` (`noteAdd`), `:515-582` (`noteList`), `internal/cli/noteapply.go:40+`, `internal/cli/review.go:33-113,207-215`
- Test: `internal/cli/note_preview_test.go` (new), `internal/domain/notebatch_plan_test.go` (append)

**Interfaces:**
- Consumes: `previewTarget`, `addPreviewFlag`, `previewUsageErr`, `resolvePreviewTarget` (Task 5); `PreviewNotesAt`, `PreviewStatus` (Task 2); `HunkRange` (`internal/domain/hunks.go:84`).
- Produces:
  - `type domain.NoteBatchTarget struct { Cached bool; Rev string; Hunks *model.DiffSpec }`
  - `func (s *Service) PlanNoteBatchIn(ctx context.Context, b notebatch.Batch, t NoteBatchTarget, author string, rule NoteSideRule) (planned []PlannedNote, skipped int, err error)`
  - `PlanNoteBatch` keeps its signature and delegates.

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/note_preview_test.go`:

```go
package cli

import (
	"strings"
	"testing"
)

// The note is stored on the TIP, so the tip's own commit view shows it too.
func TestNoteAddPreviewStoresOnTheTip(t *testing.T) {
	svc, dir := newCLIPreviewRepo(t)
	tip := revParseCLI(t, dir, "feat")
	var out, errb strings.Builder
	code := cmdNote(svc, []string{"add", "--preview", "main...feat", "--file", "a.txt",
		"--new-line", "1", "--summary", "alpha stands"}, strings.NewReader(""), &out, &errb)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	var lout, lerr strings.Builder
	if code := cmdNote(svc, []string{"list", "--rev", tip, "--file", "a.txt"}, strings.NewReader(""), &lout, &lerr); code != 0 {
		t.Fatalf("list exit %d: %s", code, lerr.String())
	}
	if !strings.Contains(lout.String(), "alpha stands") {
		t.Fatalf("the note must be stored on the tip, got %q", lout.String())
	}
}

func TestNoteAddPreviewRefusesOldLine(t *testing.T) {
	svc, _ := newCLIPreviewRepo(t)
	var out, errb strings.Builder
	code := cmdNote(svc, []string{"add", "--preview", "main...feat", "--file", "a.txt",
		"--old-line", "1", "--summary", "no"}, strings.NewReader(""), &out, &errb)
	if code != 2 {
		t.Fatalf("want exit 2, got %d (%s)", code, errb.String())
	}
	if !strings.Contains(errb.String(), "new side") {
		t.Fatalf("want the new-side message, got %q", errb.String())
	}
}

func TestNoteAddPreviewRefusesRev(t *testing.T) {
	svc, dir := newCLIPreviewRepo(t)
	var out, errb strings.Builder
	code := cmdNote(svc, []string{"add", "--preview", "main...feat", "--rev", revParseCLI(t, dir, "feat"),
		"--file", "a.txt", "--new-line", "1", "--summary", "no"}, strings.NewReader(""), &out, &errb)
	if code != 2 || !strings.Contains(errb.String(), "one target only") {
		t.Fatalf("want exit 2 + one-target message, got %d %q", code, errb.String())
	}
}

// A note on an older commit's rewritten line lists as `outdated`.
func TestNoteListPreviewReportsOutdated(t *testing.T) {
	svc, dir := newCLIPreviewRepo(t)
	c1 := revParseCLI(t, dir, "feat~2")
	var out, errb strings.Builder
	if code := cmdNote(svc, []string{"add", "--rev", c1, "--file", "a.txt",
		"--new-line", "4", "--summary", "why DELTA"}, strings.NewReader(""), &out, &errb); code != 0 {
		t.Fatalf("seed exit %d: %s", code, errb.String())
	}
	out.Reset()
	errb.Reset()
	if code := cmdNote(svc, []string{"list", "--preview", "main...feat", "--file", "a.txt"},
		strings.NewReader(""), &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "outdated") || !strings.Contains(out.String(), "why DELTA") {
		t.Fatalf("want the gathered note reported outdated, got %q", out.String())
	}
}
```

Append to `internal/domain/notebatch_plan_test.go` (adapt the existing helpers in that file):

```go
// Ruling 1: a batch's `hunk` items number the PREVIEW's patch, while the
// address stays the tip.
func TestPlanNoteBatchInUsesTheGivenHunkSpec(t *testing.T) {
	svc, dir := newPreviewRepo(t)
	ctx := context.Background()
	tip := revParse(t, dir, "feat")
	set, err := svc.PreviewNotes(ctx, "feat", "main")
	if err != nil {
		t.Fatal(err)
	}
	spec := set.DiffSpec()
	b, err := notebatch.Parse([]byte(`{"files":[{"path":"a.txt","annotations":[{"hunk":1,"summary":"first preview hunk"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	planned, skipped, err := svc.PlanNoteBatchIn(ctx, b,
		NoteBatchTarget{Rev: tip, Hunks: &spec}, "ada", NoteSideNewOnly)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 0 || len(planned) != 1 {
		t.Fatalf("planned=%d skipped=%d", len(planned), skipped)
	}
	got := planned[0].Note
	if got.Address.Commit != tip {
		t.Fatalf("the address is the tip, got %s", got.Address.Commit)
	}
	if got.Side != model.NoteSideNew {
		t.Fatalf("new side only, got %s", got.Side)
	}
	// The preview's first hunk covers the merge-base..tip change, which
	// reaches line 4 (DELTA → ECHO) — the tip's OWN first hunk would not.
	if got.Range[1] < 4 {
		t.Fatalf("the range must come from the preview patch, got %v", got.Range)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-preview-notes && go test ./internal/cli/ ./internal/domain/ -run "Preview" -v`
Expected: FAIL — `PlanNoteBatchIn` undefined, `--preview` not a `note` flag.

- [ ] **Step 3: Extend the batch planner**

`internal/domain/notebatch_plan.go` — add above `PlanNoteBatch` (line 52):

```go
// NoteBatchTarget separates the two things a batch needs from its target: the
// ADDRESS every item is stored at, and the PATCH a `hunk` item is numbered
// against. They coincide for every target that existed before merge previews
// (a commit's address and its own parent→commit diff), but a preview stores
// on the source tip while numbering merge-base → tip, so they must be
// expressible apart.
type NoteBatchTarget struct {
	Cached bool
	Rev    string          // the address target ("" = working tree)
	Hunks  *model.DiffSpec // nil = derive from Cached/Rev through HunkDiffSpec
}

// PlanNoteBatch is PlanNoteBatchIn for the targets whose address and patch
// coincide. Kept as-is so every existing caller is untouched.
func (s *Service) PlanNoteBatch(ctx context.Context, b notebatch.Batch, cached bool, rev, author string, rule NoteSideRule) ([]PlannedNote, int, error) {
	return s.PlanNoteBatchIn(ctx, b, NoteBatchTarget{Cached: cached, Rev: rev}, author, rule)
}
```

Rename the existing function to `PlanNoteBatchIn(ctx context.Context, b notebatch.Batch, t NoteBatchTarget, author string, rule NoteSideRule) (planned []PlannedNote, skipped int, err error)` and replace its two uses of `cached, rev`:

- line 75: `addr, err = s.NoteTarget(ctx, it.Path, t.Cached, t.Rev)`
- line 81: `side, rng, aerr := s.planNoteBatchAnchor(ctx, addr, t, it.Target)`

and change `planNoteBatchAnchor` (line 101) to take `t NoteBatchTarget`, with its hunk branch becoming:

```go
	case t.Hunk != 0:
		spec := model.DiffSpec{}
		if bt.Hunks != nil {
			spec = *bt.Hunks
			spec.Paths = []string{addr.Path}
		} else {
			var err error
			spec, err = s.HunkDiffSpec(ctx, bt.Cached, bt.Rev, []string{addr.Path})
			if err != nil {
				return "", [2]int{}, err
			}
		}
		return s.HunkRange(ctx, spec, addr.Path, t.Hunk)
```

(Rename the parameter carefully: the batch item is `t notebatch.Target`; call the new one `bt NoteBatchTarget`.)

- [ ] **Step 4: `gg note add --preview`**

`internal/cli/note.go`, in `noteAdd` (line 267) add `pf := addPreviewFlag(fs)`.

First extend the LINK branch's own guard (line 303) — without this, `gg note add gg://… --preview X` silently drops `--preview` and writes to the link's target instead (ruling 9 makes that exit 2):

```go
	if link != nil {
		if pf.set() {
			return previewUsageErr("note add", stderr)
		}
		if *tf.file != "" || *tf.rev != "" || *tf.cached || *hunk != 0 || *newLine != 0 || *oldLine != 0 {
```

Then, inside the `else` branch that resolves the target (line 328), branch first:

```go
	} else if pf.set() {
		if *tf.rev != "" || *tf.cached {
			return previewUsageErr("note add", stderr)
		}
		if strings.TrimSpace(*tf.file) == "" {
			fmt.Fprintln(stderr, "note add: --preview needs --file <path>")
			return 2
		}
		if *oldLine != 0 {
			// Spec §1.1: the preview's old side is the merge base, which no
			// stored address names.
			fmt.Fprintln(stderr, "note add: notes in a preview anchor on the new side (drop --old-line)")
			return 2
		}
		tgt, terr := resolvePreviewTarget(ctx, svc, *pf.spec)
		if terr != nil {
			fmt.Fprintln(stderr, "error:", terr)
			return 1
		}
		addr = model.FileAddress{State: model.StateCommitted, Commit: tgt.Set.Tip, Path: *tf.file}
		switch {
		case *newLine != 0:
			if *newLine < 1 {
				fmt.Fprintln(stderr, "note add: --new-line must be a 1-based line number")
				return 2
			}
			side, rng = model.NoteSideNew, [2]int{*newLine, *newLine}
		case *hunk != 0:
			// Ruling 1: the PREVIEW's patch, so this agrees with
			// `gg diff --preview --hunks`.
			s, r, herr := svc.HunkRange(ctx, tgt.withPaths([]string{*tf.file}), *tf.file, *hunk)
			if herr != nil {
				return noteExit(herr, stderr)
			}
			if s == model.NoteSideOld {
				// A delete-only hunk has no new side to anchor on.
				fmt.Fprintln(stderr, "note add: notes in a preview anchor on the new side (that hunk only deletes lines)")
				return 2
			}
			side, rng = s, r
		default:
			fmt.Fprintln(stderr, "note add: pass exactly one of --hunk or --new-line")
			return 2
		}
	} else {
```

- [ ] **Step 5: `gg note list --preview`**

`internal/cli/note.go`, in `noteList` (line 518) add `pf := addPreviewFlag(fs)` and, before the `link != nil` branch (line 539):

```go
	if pf.set() {
		if link != nil || *tf.rev != "" || *tf.cached {
			return previewUsageErr("note list", stderr)
		}
		tgt, terr := resolvePreviewTarget(ctx, svc, *pf.spec)
		if terr != nil {
			fmt.Fprintln(stderr, "error:", terr)
			return 1
		}
		got, gerr := svc.PreviewNotesAt(ctx, tgt.Set, strings.TrimSpace(*tf.file))
		if gerr != nil {
			return noteExit(gerr, stderr)
		}
		res = got
		previewWords = true
	}
```

Declare `previewWords := false` beside `var res []domain.ResolvedNote` (line 538) and use it in the two render paths:

- text (line 576): `renderNoteLinePreview(stdout, r, false, previewWords)`
- JSON (line 567): `wires = append(wires, domain.ToWireNotePreview(r, previewWords))`

Add the two thin wrappers. In `internal/cli/note.go`, beside `renderNoteLine` (line 495):

```go
// renderNoteLinePreview is renderNoteLine with the preview's status word.
// "Outdated" is what a merge preview calls stale (spec §1.2) — the STORE is
// unchanged, only the word printed for it.
func renderNoteLinePreview(w io.Writer, r domain.ResolvedNote, indent, preview bool) {
	if !preview {
		renderNoteLine(w, r, indent)
		return
	}
	if indent {
		fmt.Fprintf(w, "  %s [%s] reply  %s\n", r.Note.ID, r.Note.Source, r.Note.Summary)
		return
	}
	fmt.Fprintf(w, "%s [%s] %s %s:%d-%d %s  %s\n",
		r.Note.ID, r.Note.Source, noteTargetLabel(r.Note.Address),
		r.Note.Side, r.Range[0], r.Range[1], domain.PreviewStatus(r.Status), r.Note.Summary)
}
```

and in `internal/domain` beside `ToWireNote` (find it with `grep -rn "func ToWireNote" internal/domain/`):

```go
// ToWireNotePreview is ToWireNote with the preview's status word. The wire
// shape is unchanged: only the `status` string differs, and only when the
// caller is rendering a merge preview.
func ToWireNotePreview(r ResolvedNote, preview bool) WireNote {
	w := ToWireNote(r)
	if preview {
		w.Status = PreviewStatus(r.Status)
	}
	return w
}
```

(Check `WireNote`'s status field name first; if it is not `Status`, use the real one.)

- [ ] **Step 6: `gg note apply --preview`**

In `internal/cli/noteapply.go`'s flag set, add `pf := addPreviewFlag(fs)`; where it computes `cached, rev` for `PlanNoteBatch`, branch:

```go
	target := domain.NoteBatchTarget{Cached: *tf.cached, Rev: *tf.rev}
	rule := domain.NoteSideBoth
	if pf.set() {
		if *tf.rev != "" || *tf.cached || link != nil {
			return previewUsageErr("note apply", stderr)
		}
		tgt, terr := resolvePreviewTarget(ctx, svc, *pf.spec)
		if terr != nil {
			fmt.Fprintln(stderr, "error:", terr)
			return 1
		}
		spec := tgt.Spec
		// Stored on the tip, numbered over the preview's own patch, new side
		// only — old-side items are SKIPPED with a warning (the --working rule).
		target = domain.NoteBatchTarget{Rev: tgt.Set.Tip, Hunks: &spec}
		rule = domain.NoteSideNewOnly
	}
	planned, skipped, err := svc.PlanNoteBatchIn(ctx, batch, target, noteAuthorDefault(*author), rule)
```

and, where the existing code reports a skip count (mirror `internal/cli/review.go:186-188`), keep printing:

```go
	if skipped > 0 {
		fmt.Fprintf(stderr, "note: skipped %d old-side annotation(s) — notes in a preview anchor on the new side\n", skipped)
	}
```

Adapt the variable names to what `noteapply.go` actually uses — read the function first.

- [ ] **Step 7: `gg review --preview`**

`internal/cli/review.go` — add `pf := addPreviewFlag(fs)` after `wantNotes` (line 37) and a fourth case in the target switch (line 51):

```go
	arg := ""
	var target domain.ReviewTarget
	switch {
	case pf.set():
		if *working || fs.NArg() >= 1 {
			return previewUsageErr("review", stderr)
		}
		tgt, terr := resolvePreviewTarget(ctx, svc, *pf.spec)
		if terr != nil {
			fmt.Fprintln(stderr, "error:", terr)
			return 1
		}
		// Ruling 9: the range review, over the pair the preview names. Range
		// is the HASH pair (it is spliced unquoted into the tool command);
		// Label is the human pair and is never executed. The existing range
		// rule in reviewImportTarget then anchors notes on the tip, new side
		// only — exactly what a preview needs.
		arg = tgt.Spec.Rev
		target = domain.ReviewTarget{Kind: domain.ReviewRange, Range: tgt.Spec.Rev,
			Label: tgt.Target + " ... " + tgt.Source, Diff: tgt.Spec}
	case *working:
```

`reviewImportTarget` (line 122) already takes the last non-empty segment of an `A...B` range as the tip and returns `NoteSideNewOnly`, so it needs no change. But `importReviewNotes` must number hunks over the preview patch. Thread an EXPLICIT spec through rather than sniffing the range string: gating on `strings.Contains(Rev, "...")` would silently change today's `gg review A...B --notes` (its hunks would start being numbered over the range while `A..B` stayed tip-numbered) — a behaviour change this feature did not ask for.

Give `importReviewNotes` one more parameter, `hunkSpec *model.DiffSpec`, and pass it through:

```go
// importReviewNotes reads the tool's notes … hunkSpec is the patch a `hunk`
// annotation is numbered against, or nil to derive it from cached/rev as
// before. Only --preview passes one: its notes are stored on the source tip
// but numbered over merge-base → tip, and nothing else in review land splits
// those two apart.
func importReviewNotes(ctx context.Context, svc *domain.Service, target domain.ReviewTarget, arg, notesPath, report, toolName string, hunkSpec *model.DiffSpec, stderr io.Writer) int {
```

and at line 178:

```go
	planned, skipped, err := svc.PlanNoteBatchIn(ctx, batch,
		domain.NoteBatchTarget{Cached: cached, Rev: rev, Hunks: hunkSpec},
		noteAuthorDefault(toolName), rule)
```

In `cmdReview`, keep a `var hunkSpec *model.DiffSpec` beside `arg`, set it in the `pf.set()` case (`spec := tgt.Spec; hunkSpec = &spec`), and pass it at the two `importReviewNotes` call sites (line 112 is the only one; check with `grep -n importReviewNotes internal/cli/*.go`).

- [ ] **Step 8: Run the tests**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-preview-notes && go test ./internal/cli/ ./internal/domain/`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-preview-notes && git add internal/domain/notebatch_plan.go internal/domain/notebatch_plan_test.go internal/domain/wirenote.go internal/cli/note.go internal/cli/noteapply.go internal/cli/review.go internal/cli/note_preview_test.go && git commit -m "feat(cli): gg note add/list/apply --preview and gg review --preview

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

(Adjust the staged file list to whatever file `ToWireNotePreview` actually landed in — check with `git -C /mnt/t/others/gigagit.worktrees/feat-preview-notes status --short` first.)

---

### Task 7: MCP — `preview` in place of `commit`

**Files:**
- Modify: `internal/mcp/notes.go:31-37,88-200,231-281`
- Test: `internal/mcp/notes_preview_test.go` (new)

**Interfaces:**
- Consumes: `PreviewResolve`, `PreviewNotes`, `PreviewNotesAt`, `PreviewNoteCounts`, `PlanNoteBatchIn`, `PreviewStatus`.
- Produces: `noteTargetIn.Preview string` — `"<target>...<source>"`, an id or a label; mutually exclusive with `rev`/`cached`.

- [ ] **Step 1: Write the failing test**

Create `internal/mcp/notes_preview_test.go`, following the structure of the existing MCP note tests (read one first — `ls internal/mcp/*_test.go`). It must assert:

```go
// gg_note_add with preview stores on the tip and anchors on the new side.
func TestNoteAddWithAPreviewStoresOnTheTip(t *testing.T) { /* … */ }

// gg_notes_list with preview reports the gathered set with status "outdated".
func TestNotesListWithAPreviewReportsOutdated(t *testing.T) { /* … */ }

// preview + rev is a usage error.
func TestPreviewAndRevAreMutuallyExclusive(t *testing.T) { /* … */ }
```

Write the bodies against the same repo shape `newPreviewRepo` builds (main + three feat commits, the second rewriting the first's line), using whatever `*Server` constructor the existing MCP tests use.

- [ ] **Step 2: Run it to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-preview-notes && go test ./internal/mcp/ -run Preview -v`
Expected: FAIL — unknown field `preview`.

- [ ] **Step 3: Add the field and the resolver**

`internal/mcp/notes.go`, extend `noteTargetIn` (line 33):

```go
type noteTargetIn struct {
	File   string `json:"file,omitempty"`
	Cached bool   `json:"cached,omitempty"`
	Rev    string `json:"rev,omitempty"`
	// Preview addresses a MERGE PREVIEW instead of a commit:
	// "<target>...<source>", or a saved preview's id or label. The note is
	// stored on the source TIP and anchors on the new side only — the
	// preview's old side is the merge base, which no stored address names.
	Preview string `json:"preview,omitempty"`
}
```

Add, beside `notesFor` (line 231):

```go
// previewSet resolves a request's preview target. It is mutually exclusive
// with cached/rev: two targets in one call is a caller error, never a silent
// precedence rule.
func (s *Server) previewSet(ctx context.Context, t noteTargetIn) (domain.PreviewNoteSet, error) {
	if t.Cached || t.Rev != "" {
		return domain.PreviewNoteSet{}, fmt.Errorf("one target only: preview cannot be combined with rev or cached")
	}
	source, target, err := s.svc.PreviewResolve(ctx, t.Preview)
	if err != nil {
		return domain.PreviewNoteSet{}, err
	}
	set, err := s.svc.PreviewNotes(ctx, source, target)
	if err != nil {
		return domain.PreviewNoteSet{}, err
	}
	if !set.OK() {
		return domain.PreviewNoteSet{}, fmt.Errorf("preview %s → %s is not previewable", source, target)
	}
	return set, nil
}
```

MCP does **not** build its own range: every hunk spec below comes from `set.DiffSpec()` (Task 1), the one construction of the preview's patch (ruling 1).

- [ ] **Step 4: Branch the three tools**

`gg_notes_list` (line 95): before `s.notesFor`, insert:

```go
		if in.Preview != "" {
			set, perr := s.previewSet(ctx, in.noteTargetIn)
			if perr != nil {
				return nil, out, perr
			}
			res, rerr := s.svc.PreviewNotesAt(ctx, set, in.File)
			if rerr != nil {
				return nil, out, rerr
			}
			for _, r := range res {
				if in.Type != "" && in.Type != "all" && string(r.Note.Source) != in.Type {
					continue
				}
				out.Notes = append(out.Notes, domain.ToWireNotePreview(r, true))
			}
			return nil, out, nil
		}
```

`gg_note_add` (line 134): replace the `NoteTarget`/`noteAnchor` pair with:

```go
		var addr model.FileAddress
		var side model.NoteSide
		var rng [2]int
		if in.Preview != "" {
			set, perr := s.previewSet(ctx, in.noteTargetIn)
			if perr != nil {
				return nil, out, perr
			}
			if in.File == "" {
				return nil, out, fmt.Errorf("preview needs file")
			}
			if in.OldLine != 0 {
				return nil, out, fmt.Errorf("notes in a preview anchor on the new side (drop old_line)")
			}
			addr = model.FileAddress{State: model.StateCommitted, Commit: set.Tip, Path: in.File}
			switch {
			case in.NewLine != 0:
				side, rng = model.NoteSideNew, [2]int{in.NewLine, in.NewLine}
			case in.Hunk != 0:
				spec := set.DiffSpec()
				spec.Paths = []string{in.File}
				s2, r2, herr := s.svc.HunkRange(ctx, spec, in.File, in.Hunk)
				if herr != nil {
					return nil, out, herr
				}
				if s2 == model.NoteSideOld {
					return nil, out, fmt.Errorf("notes in a preview anchor on the new side (that hunk only deletes lines)")
				}
				side, rng = s2, r2
			default:
				return nil, out, fmt.Errorf("pass exactly one of hunk or new_line")
			}
		} else {
			a, aerr := s.svc.NoteTarget(ctx, in.File, in.Cached, in.Rev)
			if aerr != nil {
				return nil, out, aerr
			}
			sd, rg, nerr := s.noteAnchor(ctx, a, in)
			if nerr != nil {
				return nil, out, nerr
			}
			addr, side, rng = a, sd, rg
		}
```

`gg_notes_apply` (line 185): replace the plan call with:

```go
		target := domain.NoteBatchTarget{Cached: in.Cached, Rev: in.Rev}
		rule := domain.NoteSideBoth
		if in.Preview != "" {
			set, perr := s.previewSet(ctx, in.noteTargetIn)
			if perr != nil {
				return nil, out, perr
			}
			spec := set.DiffSpec()
			target = domain.NoteBatchTarget{Rev: set.Tip, Hunks: &spec}
			rule = domain.NoteSideNewOnly
		}
		planned, _, err := s.svc.PlanNoteBatchIn(ctx, batch, target, author, rule)
```

Update all three tool `Description` strings to mention the new argument, e.g. append to each: `preview = "<target>...<source>" (or a saved preview's id/label) addresses a MERGE PREVIEW instead: the note is stored on the source tip, new side only.`

- [ ] **Step 5: Run the tests**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-preview-notes && go test ./internal/mcp/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-preview-notes && git add internal/mcp/notes.go internal/mcp/notes_preview_test.go && git commit -m "feat(mcp): the note tools accept a preview target

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 8: Web server — `/api/preview/notes` and the `notes` count on `/api/preview`

**Files:**
- Create: `internal/web/previewnotes.go`
- Modify: `internal/web/previews.go:18-26` (route registration is in `init`), `:29-49` (`previewRow`/`previewRowFrom`), `:62-85` (`handlePreviews`)
- Test: `internal/web/previewnotes_test.go` (new)

**Interfaces:**
- Consumes: `PreviewNotes`, `PreviewNotesAt`, `PreviewNoteCounts`, `PreviewStatus`, `ToWireNotePreview`; `(*Server).knownRefName` (`internal/web/previews.go:88`).
- Produces:
  - `GET /api/preview/notes?source=&target=&path=` → `{"notes": [wireNote], "tip": "<full sha>", "counts": {"<path>": n}, "total": n}`.
  - `previewRow.Notes int` with JSON key `notes`.

- [ ] **Step 1: Write the failing test**

Create `internal/web/previewnotes_test.go`, following the pattern of `internal/web/previews_test.go` (read it first for the server/repo fixture):

```go
package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPreviewNotesEndpointReturnsTheGatheredSet(t *testing.T) {
	s, dir := newPreviewServer(t) // the fixture: main + three feat commits, one note on feat~2
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/preview/notes?source=feat&target=main&path=a.txt", nil)
	s.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Notes []struct{ Status, Summary string } `json:"notes"`
		Tip   string                             `json:"tip"`
		Total int                                `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Notes) != 1 {
		t.Fatalf("want the gathered note, got %d", len(body.Notes))
	}
	if body.Notes[0].Status != "outdated" {
		t.Fatalf("a note on a rewritten line is outdated, got %q", body.Notes[0].Status)
	}
	if len(body.Tip) != 40 {
		t.Fatalf("the payload must name the write target, got %q", body.Tip)
	}
	_ = dir
}

// An unknown branch is a 404, not an empty preview (the /api/compare posture).
func TestPreviewNotesRefusesAnUnknownBranch(t *testing.T) {
	s, _ := newPreviewServer(t)
	rec := httptest.NewRecorder()
	s.mux().ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/preview/notes?source=nope&target=main&path=a.txt", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", rec.Code)
	}
}

func TestPreviewRowsCarryTheNoteTotal(t *testing.T) {
	s, _ := newPreviewServer(t) // the fixture also saves the pair as a record
	rec := httptest.NewRecorder()
	s.mux().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/preview", nil))
	var body struct {
		Entries []struct{ Notes int `json:"notes"` } `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Entries) != 1 || body.Entries[0].Notes != 1 {
		t.Fatalf("want notes:1 on the row, got %+v", body.Entries)
	}
}
```

(`s.mux()` — use whatever handler accessor the existing web tests use.)

- [ ] **Step 2: Run it to verify it fails**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-preview-notes && go test ./internal/web/ -run TestPreviewNotes -v`
Expected: FAIL — 404 on the route.

- [ ] **Step 3: Write the handler**

Create `internal/web/previewnotes.go`:

```go
package web

import (
	"errors"
	"net/http"

	"github.com/homeend/gigagit/internal/domain"
)

// Notes inside a merge preview. The page never names a checkout or a sha: it
// passes the pair's branch NAMES, which are resolved against the live branch
// lists (the /api/compare allowlist posture) before anything reaches a git
// argv, and the server answers with the resolved tip so the page's note-ADD
// calls can target it through the ordinary /api/notes/add endpoint.

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/preview/notes", s.handlePreviewNotes)
	})
}

func (s *Server) handlePreviewNotes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	source, target, path := q.Get("source"), q.Get("target"), q.Get("path")
	for _, name := range []string{source, target} {
		if code, err := s.knownRefName(r, name); code != 0 {
			writeErr(w, code, err)
			return
		}
	}
	if path != "" && !isGitArgSafe(path) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid path"))
		return
	}
	ctx := readCtx(r)
	set, err := s.service().PreviewNotes(ctx, source, target)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// Ruling 6: a pair that is not previewable is not an error — it simply
	// has nothing to show.
	out := map[string]any{"notes": []wireNote{}, "tip": set.Tip, "counts": map[string]int{}, "total": 0}
	if !set.OK() {
		writeJSON(w, out)
		return
	}
	res, err := s.service().PreviewNotesAt(ctx, set, path)
	if err != nil && !errors.Is(err, domain.ErrNotesDisabled) {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	notes := make([]wireNote, 0, len(res))
	for _, n := range res {
		notes = append(notes, domain.ToWireNotePreview(n, true))
	}
	counts, total, cerr := s.service().PreviewNoteCounts(ctx, set)
	if cerr != nil && !errors.Is(cerr, domain.ErrNotesDisabled) {
		writeErr(w, http.StatusInternalServerError, cerr)
		return
	}
	out["notes"], out["counts"], out["total"] = notes, orEmptyCounts(counts), total
	writeJSON(w, out)
}
```

- [ ] **Step 4: Put the total on the preview rows**

`internal/web/previews.go` — add to `previewRow` (after `TargetHash`, line 37):

```go
	// Notes is the preview's root-note total, hidden ones included (the TUI's
	// ◆N badge). Zero for a pair that is not previewable.
	Notes int `json:"notes"`
```

and fill it in `handlePreviews` (lines 73-84):

```go
	for _, p := range ps {
		sum, err := svc.PreviewSummary(ctx, p.Source, p.Target)
		if err != nil {
			rows = append(rows, previewErrorRow(p, err))
			continue
		}
		row := previewRowFrom(p, sum)
		if sum.State == domain.PreviewOK {
			if set, serr := svc.PreviewNotes(ctx, p.Source, p.Target); serr == nil && set.OK() {
				if _, total, cerr := svc.PreviewNoteCounts(ctx, set); cerr == nil {
					row.Notes = total
				}
			}
		}
		rows = append(rows, row)
	}
```

- [ ] **Step 5: Run the tests**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-preview-notes && go test ./internal/web/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-preview-notes && git add internal/web/previewnotes.go internal/web/previewnotes_test.go internal/web/previews.go && git commit -m "feat(web): GET /api/preview/notes and the note total on preview rows

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 9: Web page — notes in the preview stage

**Files:**
- Modify: `internal/web/static/files.js:611-665` (`openFile`'s compare branch), `:933-960` (`notesArmed`/`noteQuery`), `:1007-1045` (`noteBoxHTML`), the `c` add path (find it with `grep -n "firstChangedRow\|case \"c\"" internal/web/static/files.js`), `:560-600` (`renderFiles` badges)
- Modify: `internal/web/static/previews.js:36-67` (row text), `:120-158` (`openPreviewBody`), `internal/web/static/live.js` (the `notes` event → refetch previews)
- Modify: `internal/web/static/style.css` (the `.outdated` rule)
- Test: `internal/web/static_js_test.go` or the existing web JS source-assertion test file (find it with `grep -rln "static/files.js" internal/web/*_test.go`)

**Interfaces:**
- Consumes: `GET /api/preview/notes` and `previewRow.notes` (Task 8); the existing `/api/notes/add|edit|reply|remove` endpoints unchanged.
- Produces: `state.previewOpen.tip`, `state.previewCounts`, and `state.diffCtx.preview = {source, target}` for the rest of the page.

- [ ] **Step 1: Write the failing source assertions**

Append to the existing web JS source-assertion test (same file, same style — read one case first):

```go
func TestPreviewDiffArmsNotesInJS(t *testing.T) {
	src := readStatic(t, "files.js")
	for _, want := range []string{
		"/api/preview/notes",             // the preview note query exists
		"state.diffCtx.preview",          // the diff context carries the pair
		"notes in a preview anchor on the new side", // the old-side refusal
		"outdated",                       // the stale class/word
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("files.js must contain %q", want)
		}
	}
}

func TestPreviewRowShowsTheNoteBadgeInJS(t *testing.T) {
	src := readStatic(t, "previews.js")
	if !strings.Contains(src, "e.notes") {
		t.Fatal("previews.js must paint the row's note total")
	}
}

func TestOutdatedClassIsStyled(t *testing.T) {
	css := readStatic(t, "style.css")
	if !strings.Contains(css, ".outdated") {
		t.Fatal("style.css must style the outdated note class")
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-preview-notes && go test ./internal/web/ -run "TestPreview.*JS|TestOutdated" -v`
Expected: FAIL.

- [ ] **Step 3: Arm the preview diff context**

`internal/web/static/previews.js`, in `armPreview` (line 104), record the tip explicitly and fetch the counts:

```js
function armPreview(body) {
  state.previewOpen = {
    id: body.id || "",
    source: body.source,
    target: body.target,
    sourceHash: body.source_hash,
    targetHash: body.target_hash,
    // The source tip IS the note write target: a preview note is an ordinary
    // committed note on it (spec §1.1). The page never computes this itself.
    tip: body.source_hash,
  };
}
```

and in `stateText` / `renderPreviews` (line 58) append the badge:

```js
        `<span class="psub">${esc(stateText(e))}</span>` +
        (e.notes > 0 ? `<span class="notebadge">◆${e.notes}</span>` : "") +
```

`internal/web/static/files.js`, in `openFile`'s compare branch (lines 626-641), replace the `cmp`-gated `notes: !cmp` with a preview-aware context:

```js
  const cmp = state.filesMode === "compare";
  // A merge preview is the ONE compare whose new side is a real commit's
  // content (the source tip), so its rows ARE note-addressable — at the tip,
  // new side only. Every other compare stays notes:false, because its old
  // side is the compared revision, not bHash^.
  const po = state.previewOpen;
  const prev = cmp && po && po.tip && state.compare && state.compare.bHash === po.tip ? po : null;
  state.diffCtx = {
    path: f.path,
    rev: prev ? prev.tip : cmp ? state.compare.bHash : f.sha || state.fileSha,
    state: "commit",
    notes: !cmp || !!prev,
    preview: prev ? { source: prev.source, target: prev.target } : null,
    compare: cmp,
  };
```

- [ ] **Step 4: Query the preview's notes**

`internal/web/static/files.js`, `noteQuery` (line 939):

```js
function noteQuery() {
  if (!notesArmed()) return null;
  const q = new URLSearchParams({ path: state.diffCtx.path });
  if (state.diffCtx.preview) {
    // The preview gathers notes along the branch, so the query is the PAIR,
    // not the tip: a note written against an older commit still belongs here.
    q.set("source", state.diffCtx.preview.source);
    q.set("target", state.diffCtx.preview.target);
    return q;
  }
  if (state.diffCtx.state === "commit") {
    if (!state.diffCtx.rev) return null;
    q.set("rev", state.diffCtx.rev);
    q.set("state", "commit");
  } else {
    q.set("state", state.diffCtx.state || "unstaged");
  }
  return q;
}
```

and in `fetchNotes` (line 964) pick the endpoint:

```js
  try {
    const url = (state.diffCtx.preview ? "/api/preview/notes?" : "/api/notes?") + q;
    const d = await getJSON(url);
    state.notes = d.notes || [];
    if (state.diffCtx.preview) state.previewCounts = d.counts || {};
  } catch {
    return;
  }
```

Mutations need NO change: `state.diffCtx.rev` is the tip and `state: "commit"`, so the existing `/api/notes/add|edit|reply|remove` posts already carry `target: {state: "commit", commit: <tip>}`.

- [ ] **Step 5: Outdated rendering and the old-side refusal**

`internal/web/static/files.js`, `noteBoxHTML` (line 1021):

```js
  const stale = n.status === "stale" || n.status === "outdated";
  // A preview names it "outdated": there, a note whose lines a later commit
  // changed is the expected case, not an edge one.
  const word = state.diffCtx.preview ? " (outdated)" : " (stale)";
  const cls = state.diffCtx.preview ? "outdated" : "stale";
  const agent = n.source === "agent";
  const title = (agent ? "agent note" : "note") + (n.author ? " · " + n.author : "") +
    " · " + state.diffCtx.path + " " + (n.side === "old" ? "L" : "R") + n.line + (stale ? word : "");
```

then use `cls` in place of the two literal `"stale"` class fragments in the same function (`notebox … ${stale ? " " + cls : ""}` and `tr class="note${stale ? " " + cls : ""}…`).

In the `c` add path (the handler that reads `state.diffRow` and opens the note prompt), refuse the old side first:

```js
  if (state.diffCtx && state.diffCtx.preview && row.side === "old") {
    opLine("notes in a preview anchor on the new side", true);
    return;
  }
```

`internal/web/static/style.css` — add beside the existing `.stale` note rules:

```css
/* A preview's outdated note: the same dimming as a stale one, under the name
   a merge preview uses for it. */
tr.note.outdated .notebox,
.notebox.outdated {
  opacity: 0.6;
}
```

(Match the actual existing `.stale` declarations — copy their properties rather than guessing; find them with `grep -n "stale" internal/web/static/style.css`.)

- [ ] **Step 6: Per-file badges and the live refresh**

`internal/web/static/files.js`, in the compare branch of `renderFiles` (line 526), use the preview counts instead of the `cmp ? ""` suppression:

```js
      state.previewOpen && state.previewOpen.tip && state.previewCounts
        ? noteBadgeHTML(state.previewCounts[f.path])
        : cmp
        ? ""
        : noteBadgeHTML(state.noteCounts.by_commit_path[(f.sha || state.fileSha) + ":" + f.path]);
```

`internal/web/static/live.js` — where the `notes` SSE source already triggers `refreshNoteCounts()`/`fetchNotes()`, also call `fetchPreviews()` so the sidebar row badge follows a write (the same reason the TUI chains `chainPreviewsRead` off `srcNotes`).

- [ ] **Step 7: Run the tests**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-preview-notes && go test ./internal/web/`
Expected: PASS.

- [ ] **Step 8: Verify in a real browser**

Build and run, then check by hand (the JS source assertions prove the strings exist, not that the page works):

```bash
cd /mnt/t/others/gigagit.worktrees/feat-preview-notes && go build -o /tmp/gg-preview ./cmd/gg && /tmp/gg-preview web
```

In the page: open a merge preview, open a file, add a note on a new-side row, confirm it appears; open the source tip's own commit diff and confirm the SAME note is there; click an old-side row and confirm `c` refuses with the notice.

- [ ] **Step 9: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-preview-notes && git add internal/web/static/files.js internal/web/static/previews.js internal/web/static/live.js internal/web/static/style.css internal/web/*_test.go && git commit -m "feat(web): notes inside the merge-preview stage — gathered set, outdated rows, new-side-only add

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

---

### Task 10: Skills, docs, e2e, and the full gate

**Files:**
- Modify: `internal/agentskill/agentskill.go:22,26`, `internal/agentskill/using-gg.md:252-264`, `internal/agentskill/reviewing-with-gg.md` (the `## Workflow` section, line 12)
- Create: `e2e/scenarios/s91_preview_notes.toml`
- Modify: `CHANGELOG.md:9`, `README.md`, `docs/CLAUDE-details.md`

**Interfaces:**
- Consumes: every verb Tasks 5-7 shipped.
- Produces: nothing code depends on.

- [ ] **Step 1: Write the e2e scenario**

Create `e2e/scenarios/s91_preview_notes.toml`:

The schema (`.claude/skills/writing-e2e-scenarios/SKILL.md`) has exactly ONE `[input]` block and no run-step that writes a file, so the "a later commit moved the lines" case is built in `[input] steps` up front and the older commit is addressed with an ordinary rev-ish (`HEAD~1`) — `NoteAdd` resolves it to a full sha, which is precisely what makes the note a preview note on an older commit.

```toml
name = "preview notes: stored on the tip, gathered along the branch, outdated when a later commit moved the lines"

[input]
steps = [
  { write = "a.txt", content = "alpha\nbravo\ncharlie\n" },
  { commit = "seed" },
  { branch = "feat/x" },
  { switch = "feat/x" },
  { write = "a.txt", content = "alpha\nbravo\ncharlie\nDELTA\n" },
  { commit = "add DELTA" },
  { write = "a.txt", content = "alpha\nbravo\ncharlie\nECHO\n" },
  { commit = "rewrite DELTA as ECHO" },
]

[[run]]
cmd  = ["preview", "add", "--label", "login", "feat/x", "main"]
exit = 0

# The preview's own numbered hunks — what --hunk N resolves against.
[[run]]
cmd             = ["diff", "--preview", "login", "--hunks"]
exit            = 0
stdout_contains = ["a.txt", "1 @@ -"]

# A note addressed to the PREVIEW: stored on the source tip, active there.
[[run]]
cmd             = ["note", "add", "--preview", "login", "--file", "a.txt", "--new-line", "4", "--summary", "why ECHO"]
exit            = 0

# A note on the EARLIER commit, on the line that commit added and the next one
# rewrote. It is not on the tip at all, yet the preview gathers it.
[[run]]
cmd             = ["note", "add", "--rev", "HEAD~1", "--file", "a.txt", "--new-line", "4", "--summary", "why DELTA"]
exit            = 0

[[run]]
cmd             = ["note", "list", "--preview", "login", "--file", "a.txt"]
exit            = 0
stdout_contains = ["why ECHO", "active", "why DELTA", "outdated"]

# The old side is the merge base: not addressable.
[[run]]
cmd  = ["note", "add", "--preview", "login", "--file", "a.txt", "--old-line", "1", "--summary", "no"]
exit = 2

# One target only.
[[run]]
cmd  = ["note", "add", "--preview", "login", "--rev", "HEAD", "--file", "a.txt", "--new-line", "1", "--summary", "no"]
exit = 2

# The three-dot form needs no saved record.
[[run]]
cmd             = ["note", "list", "--preview", "main...feat/x", "--file", "a.txt"]
exit            = 0
stdout_contains = ["why ECHO", "why DELTA"]

[expect]
branch = "feat/x"
clean  = true
```

Checklist from the skill, applied: flags before positionals in every `cmd`; no origin, so the required commit step lives in `input.steps`; no decision is reachable, so no flag answers one; exit codes are success 0 / usage 2, as the verbs define them.

- [ ] **Step 2: Run the scenario**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-preview-notes && ./test.sh e2e`
Expected: PASS, `s91_preview_notes` included.

- [ ] **Step 3: Update both skills and bump both versions**

`internal/agentskill/using-gg.md` — append to the `gg preview` bullet (after line 264):

```markdown
  Notes live inside a preview: `gg diff --preview <id|label|<target>...<source>>
  [--hunks [--json]]` prints the preview's own patch and numbers its hunks,
  and `gg note add --preview P --file F (--new-line N | --hunk H) --summary …`
  anchors a note on it. A preview note is stored on the SOURCE TIP and shows
  on that commit's own view too; the old side (the merge base) is not
  addressable, so `--old-line` is refused. `gg note list --preview P [--file F]
  [--json]` lists the notes gathered along the whole branch — a note written
  against an earlier commit whose lines a later commit changed is reported
  `outdated` rather than dropped. `gg note apply --preview P --stdin` imports
  a batch onto the tip (old-side items skipped), and `gg review --preview P
  [--notes]` reviews the pair.
```

`internal/agentskill/reviewing-with-gg.md` — add to the `## Workflow` section:

```markdown
When you changed code on a branch, review it the way the user will: open a
merge preview onto its target (`gg preview add <source> <target>`), then
annotate every change you made with `gg note add --preview <id|label> --file
<path> --new-line <n> --summary "…" --rationale "…"`. The notes are stored on
the source tip and travel with it; when you push further commits the preview
keeps showing them, marking the ones whose lines moved `outdated`. Report the
preview's name back to the user so they can open it in their gg.
```

`internal/agentskill/agentskill.go` — line 22 `const Version = 69`, line 26 `const ReviewVersion = 6`. **Do NOT run `gg init --update`.**

- [ ] **Step 4: Update the project docs**

`CHANGELOG.md` — insert directly under `## [Unreleased]` (line 9):

```markdown
- **Notes inside merge previews.** A preview (`Previews` tab, `gg preview`)
  is now a review surface: its diff rows carry review notes, gathered along
  the whole branch (merge-base → source tip) rather than read off the tip
  alone, so a note survives the agent pushing more commits. A note whose
  lines a later commit changed stays listed and is marked **outdated** (`⊘`
  in the TUI gutter, the `outdated` class in the web page); one whose commit
  left the branch is hidden but still counted in the panel badge. A preview
  note is an ordinary committed note on the source tip — the same note shows
  on that commit's own view — and the old side (the merge base) is not
  addressable: `c` and `gg note add --old-line` are refused there. New CLI
  surface: `gg diff --preview`, `gg preview diff --hunks`, `gg note
  add|list|apply --preview`, `gg review --preview`; hunk numbers under
  `--preview` come from the preview's own patch. MCP's three note tools take
  a `preview` argument, and the web preview stage shows, adds and replies to
  notes through `GET /api/preview/notes`.
```

`README.md` — extend the Previews paragraph and the notes paragraph with one sentence each, saying that previews carry notes and that a preview note is stored on the source tip.

`docs/CLAUDE-details.md` — add a "notes in merge previews" paragraph to the notes section: the `PreviewNoteSet` shape, the gather-and-resolve rule, that `PreviewStatus` maps stale→outdated at render time only, the `previewSet` stamp on `diffView`, and the old-side refusal.

`CLAUDE.md` is **not** touched: no package responsibility changed (the row for `domain` already covers "review + conflict-complete report wrappers; branch-version and repo-health queries").

- [ ] **Step 5: The full gate**

Run these in the FOREGROUND, in order, and read the output:

```bash
cd /mnt/t/others/gigagit.worktrees/feat-preview-notes && ./test.sh            # ~5 min
cd /mnt/t/others/gigagit.worktrees/feat-preview-notes && ./test.sh race       # ~10 min
cd /mnt/t/others/gigagit.worktrees/feat-preview-notes && ./build.sh web       # the web exe (merge checklist, ruling 11)
```

Expected: all green. `./test.sh` covers vet + gofmt + the archtest DAG guard (`internal/tui|cli|web|mcp` must still not import `internal/git`) + the i18n AST gates + e2e.

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-preview-notes && git add internal/agentskill/agentskill.go internal/agentskill/using-gg.md internal/agentskill/reviewing-with-gg.md e2e/scenarios/s91_preview_notes.toml CHANGELOG.md README.md docs/CLAUDE-details.md && git commit -m "docs(skills,e2e): teach previews-with-notes; s91 scenario; changelog

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU"
```

- [ ] **Step 7: Deliver a verify binary**

Build it in the worktree and hand the user the absolute path, unprompted:

```bash
cd /mnt/t/others/gigagit.worktrees/feat-preview-notes && ./build.sh && ls -l ./gg
```

Then ask before merging. Do not merge, and do not push.

---

## Rulings made while planning

Things the spec left open, decided here. Each is implemented at the anchor named.

1. **`PreviewNoteSet` carries `Source`, `Target` and `Base` too**, not just `Tip`/`Commits` (spec §1.2 shows two fields). The frontends need the pair to re-query and the base to key the count cache; adding them costs nothing and keeps every caller from re-deriving them. (Task 1.)
2. **A non-previewable pair returns the ZERO set with a nil error from `PreviewNotes`, but is an ERROR in the CLI** (`resolvePreviewTarget`). Display surfaces show nothing (ruling 6); a CLI caller who named that diff explicitly must be told, not handed an empty patch. (Tasks 1, 5.)
3. **`outdated` is a render-time word, not a `NoteStatus` value** — `domain.PreviewStatus(st)` applied in `renderNoteLinePreview`, `ToWireNotePreview`, the MCP tools, `/api/preview/notes` and the two `noteBoxTitle`s. The spec says `model.NoteStatus` gains no value; this is how. (Task 2.)
4. **Two preview note readers, one resolver:** `PreviewNotesFor` (caller holds the diff, TUI) and `PreviewNotesAt` (no diff — web/CLI/MCP; reads the tip's file content). Both funnel into `resolveNotes(mine, nil, newLines)`. The alternative — making every stateless caller build a `domain.Diff` — would have meant a second resolution path.
5. **`PreviewNoteCounts` counts from the STORE, unresolved**, like `NoteCounts` does. That is what makes hidden (orphaned) notes countable, which §1.2 requires, and it keeps the badge painters off git.
6. **The preview counts cache is a plain `map[string]previewCountEntry` on `Service`, keyed `tip+":"+base`**, dropped whole by `invalidateNoteCounts`. It does not go through `factory.Cache("preview")`, because that cache is keyed only on hashes and would survive a note write (ruling 7 forbids that).
7. **`PlanNoteBatch` keeps its signature; `PlanNoteBatchIn` + `NoteBatchTarget` is the new door.** Every existing caller (MCP apply, review import, `gg note apply`) is untouched unless it needs the split. (Task 6.)
8. **`gg review --preview` sets `Range` to the HASH pair and `Label` to the NAME pair.** `ReviewTarget.Range` is spliced unquoted into the tool command (a branch name there is a command-injection vector, per its own doc comment at `internal/domain/review.go:31-36`), so names never reach it.
9. **`gg preview diff --hunks` accepts one id/label OR the two-name pair**, so it is symmetric with `gg diff --preview` without a second flag. (Task 5.)
10. **`gg note add --preview` requires `--file`** (and rejects a bare `--hunk` with no file). The preview has many files; an address needs one.
11. **A delete-only hunk under `--preview` is refused (exit 2 / an MCP error)**, because `HunkRange` returns the OLD side for it and the preview's old side is not addressable. The message reuses the same new-side wording.
12. **`L` (copy link) and the bookmark/shelf/edit refusals are unchanged.** `contextLinkText` (`internal/tui/link.go:104`) gates on `diffNoteAddress()`, not on `v.compare`, so stamping the address makes `L` copy the TIP's commit link — exactly what spec §1.3 asks for in Feature A. `diff_edit.go:39` and `bookmark.go:34` gate on `v.compare`, which stays true, so they still refuse. Both are "no code change" rows in the gate table.
13. **The steering refusal message is English, not `i18n.T`.** `answerSteer`/`steerFail` is agent-facing protocol; only the TUI's own notice is translated. (Task 4, ruling 4's TUI half.)
14. **`srcNotes` chains a previews read only when a preview is open or the Previews tab is active.** Chaining it unconditionally would spend a git resolve per saved pair on every note write in every repo. (Task 4.)
15. **The web page learns the tip from `state.previewOpen.tip`** (set in `armPreview` from `source_hash`), and gates the preview diff context on `state.compare.bHash === po.tip`, matching `previewShowing()`'s own test. Sidebar branch hashes are SHORT (ruling 8) — but `source_hash` from `/api/preview` is full, and both sides of this comparison come from that payload, so no prefix comparison is needed here.
16. **One construction of the preview's patch: `PreviewNoteSet.DiffSpec()`** (`<base>..<tip>`). The CLI, the batch planner, MCP and the tests all call it; nobody assembles a range string themselves. Ruling 1 says the numbering must agree — this is the mechanism that makes disagreement impossible. (Task 1.)
17. **The loader's identity copy is an extracted method, `(*diffView).inheritIdentity`**, not four inline assignments — so the one trap in Task 3 (a fresh view + `*dv = *msg.view` silently dropping the stamp) has a test that actually exercises it. (Task 3.)
18. **`gg review A..B` / `A...B` hunk numbering is UNCHANGED by this feature.** `importReviewNotes` takes an explicit `hunkSpec *model.DiffSpec` that only `--preview` fills; sniffing the range string would have quietly changed today's `gg review A...B --notes`. The latent inconsistency (`A...B` notes numbered over the tip's own diff) is noted here, not fixed. (Task 6.)
19. **`afterPreviewsRefresh`'s unchanged-hash branch takes the fresh per-path counts.** A note write does not move the tips, so that branch is the one every preview-badge refresh goes through; without it the file-list `◆N` and the `}`/`{` step go stale in exactly the case the feature is for. `previewRow` therefore carries `byPath` as well as `notes`. (Task 4.)
20. **"Remove all notes…" disappears when nothing on the TIP is removable.** `NotesClear` takes one address; on a preview whose visible notes all come from older commits the row would be a gesture that does nothing, which that file's own comments forbid. (Task 3.)
21. **The e2e scenario builds the "lines moved" case in `[input]` and addresses the older commit as `HEAD~1`.** The harness has one `[input]` block and no file-writing run step, so a mid-scenario commit is not expressible; `NoteAdd` resolves `HEAD~1` to a full sha, which is all the older-commit case needs. (Task 10.)
22. **The web note-ADD path needs no new endpoint**: `state.diffCtx.rev` is already the tip with `state: "commit"`, so the existing `/api/notes/*` posts carry the right target (spec §1.4).

## Self-review

**1. Spec coverage (§1 only).**

| Spec | Task |
|---|---|
| §1.1 preview note = committed note on the tip, new side | 2 (address), 3 (TUI gate 4/5), 6 (CLI), 7 (MCP) |
| §1.1 old side refused, `notes in a preview anchor on the new side` | 3 (TUI `c`/menu), 4 (steering), 6 (CLI exit 2), 7 (MCP), 9 (web) |
| §1.2 `PreviewNotes` / `PreviewNotesFor` / `PreviewNoteCounts` | 1, 2 |
| §1.2 rev-list not first-parent, cached with the summary | 1 (`RevListRange` doc + cache key) |
| §1.2 outdated = stale, listed, `⊘` + header word | 2 (`PreviewStatus`), 3 (TUI), 9 (web) |
| §1.2 orphaned hidden but counted | 2 (`PreviewNoteCounts` counts from the store) |
| §1.2 old-side notes on those commits ignored | 2 (`loadPreviewNotes` filters `Side == New`) |
| §1.3 Previews panel `✎ N` badge | 4 — **shipped as `◆N` (`noteBadge`)**, per the controller's "REUSE it, never a new glyph" |
| §1.3 preview file list per-file counts | 4 (TUI), 9 (web) |
| §1.3 preview diff view: `noteAddr`, `previewSet`, all note keys | 3 (the gate table) |
| §1.3 `Remove all notes on <tip> (N of M)` | 3 |
| §1.3 `L` unchanged | ruling 12 (no code change, asserted) |
| §1.3 live steering `reload notes` re-resolves; marks key on the tip | 3 (`loadNotesCmd` branch), ruling 12 row 9 |
| §1.4 `/api/preview` rows gain `notes`; `GET /api/preview/notes`; add/reply on the tip; no new mutation endpoint; old side offers no control; SSE | 8, 9 |
| §1.5 `--preview` helper, `gg diff`, `gg preview diff --hunks`, `gg note add/list/apply`, `gg review`, MCP, both skills + version bumps | 5, 6, 7, 10 |
| §1.6 domain/CLI/TUI/web/e2e tests | 1, 2, 3, 4, 5, 6, 8, 9, 10 |

No §1 requirement is unassigned. §2 (Feature B) appears nowhere, as instructed.

**2. Placeholder scan.** No "TBD", no "add error handling", no "similar to Task N". A few places tell the implementer to *read an existing file first and adapt names* (the `internal/git` and `internal/domain` test helpers, `textdiff.Row`'s constants, `steer.Command`'s fields, `noteapply.go`'s locals, `WireNote`'s status field, the `previewsChain` arm site). Those are deliberate: inventing a helper name that does not exist is worse than an explicit "check this, then adapt", and each one names the exact grep to run. The e2e schema question is now CLOSED — the scenario is written against the schema in `.claude/skills/writing-e2e-scenarios/SKILL.md`, which has one `[input]` block and no file-writing run step (ruling 21).

**4. Post-review fixes (second pass).** Five things in the first draft would have broken or false-passed an implementer and are fixed above: the stamp test that passed either way (now `inheritIdentity`, ruling 17); `m.pushLayer(v).(Model)`, which does not compile (`pushLayer` returns `Model`); stale preview counts after a note write, because `afterPreviewsRefresh` early-returns on unchanged hashes (ruling 19); `gg note add <gg://link> --preview X` silently dropping `--preview` (the link branch's guard now tests `pf.set()`); and the invented `[input.more]` e2e block (ruling 21). Three smaller ones too: one construction of the preview patch instead of two spellings (ruling 16), an explicit `hunkSpec` instead of range-string sniffing (ruling 18), `SetNotesStore` clearing `previewCounts`, and the remove-all row hiding rather than no-opping (ruling 20).

**3. Type consistency.** Checked across tasks: `PreviewNoteSet{Source,Target,Tip,Base,Commits}` + `OK()` (T1) is used verbatim in T2/T3/T4/T5/T7/T8. `PreviewNotesFor(ctx, set, path, d)` / `PreviewNotesAt(ctx, set, path)` / `PreviewNoteCounts(ctx, set) (map[string]int, int, error)` match every call site. `PreviewStatus(model.NoteStatus) string` and `ToWireNotePreview(ResolvedNote, bool)` are used identically in T6/T7/T8. `NoteBatchTarget{Cached,Rev,Hunks}` + `PlanNoteBatchIn(ctx, b, t, author, rule)` match T6's CLI and T7's MCP. `previewTarget{Source,Target,Set,Spec}` + `withPaths` match T5 and T6. `diffView.previewSet` / `Model.filesPreviewSet` / `Model.filesPreviewCounts` / `Model.previewNoteSet()` match T3 and T4.

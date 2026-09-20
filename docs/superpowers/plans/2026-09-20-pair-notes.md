# Pair Notes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans,
> INLINE in the session that wrote this plan. **NEVER subagents** (project
> rule). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** review notes work on a commit pair — written through a pair link or
`--preview <a>..<b>|<pair id>`, read inside the saved pair (and the
link-opened pair) in the TUI.

**Architecture:** a pair's note scope is a `PreviewNoteSet{Tip:B, Base:A,
Commits:A..B}` with empty names; the existing resolver/loader/counts are
reused untouched. One domain constructor (`PairNotes`), one domain parser
(`NoteScopeResolve`) shared by CLI and MCP, one CLI adapter
(`noteScopeFromLink`), one opener-independent TUI arming message
(`pairNotesMsg`).

**Tech stack:** Go 1.26, Bubble Tea, real `git` in `t.TempDir()`.

**Spec:** `docs/superpowers/specs/2026-09-20-pair-notes-design.md`

## Global constraints

- Worktree: `/mnt/t/others/gigagit/.claude/worktrees/pair-notes`, branch
  `feat/pair-notes`. Every shell command starts with `cd` to it; Write/Edit use
  absolute worktree paths. `rtk` prefix. Never `git add -A`. Never push.
- New logic lives in NEW files; shared files get dispatch-line edits only
  (`feat/links-compare` is rewriting `steerNavigatePair` and `files_view.go`).
- `domain.Resolved.Preview` is NEVER set for a pair link.
- `gg show <pair link>` stays exit 2.
- Old side of a pair is not addressable: `ErrPreviewOldSide`, exit 2.
- New tests call `t.Parallel()` unless they use `t.Setenv`. TUI repos via
  `testRepo(t, dir)`. Every guard is watched failing with the GUARD removed.
- TUI strings through `i18n.T`, literal keys in all four bundles.
- Commit trailers: `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>`
  and `Claude-Session: https://claude.ai/code/session_01L78m6vwaZ9iTeQMZ7PYTrb`.

## File map

| File | Role |
|---|---|
| `internal/domain/pairnotes.go` (new) | `IsPair`, `PairNotes`, `NoteScopeResolve` |
| `internal/domain/pairnotes_test.go` (new) | domain tests |
| `internal/cli/notescope.go` (new) | `noteScopeFromLink`, `scopeName` |
| `internal/cli/note_pair_test.go` (new) | CLI tests |
| `internal/cli/previewflag.go` | `resolvePreviewTarget` → `NoteScopeResolve`; help text |
| `internal/cli/note.go`, `noteapply.go`, `session.go`, `linkconsume.go` | dispatch-line swaps, `Pair: true` |
| `internal/mcp/notes.go` | `previewSet` → `NoteScopeResolve` |
| `internal/tui/saved_pair_notes.go` (new) | `pairNotesMsg`, `pairNotesCmd`, handler, `scopeLinkFor` |
| `internal/tui/saved_pair_notes_test.go` (new) | TUI tests |
| `internal/tui/saved_pair_open.go`, `steer_nav.go`, `model.go`, `preview_panel.go`, `link.go`, `stash_link.go` | one-line dispatches |
| `e2e/scenarios/s97_pair_notes.toml` (new) | e2e |

---

### Task 1: domain — `PairNotes` + `IsPair`

**Files:** create `internal/domain/pairnotes.go`, `internal/domain/pairnotes_test.go`.

**Produces:** `func (set PreviewNoteSet) IsPair() bool`,
`func (s *Service) PairNotes(ctx, a, b string) (PreviewNoteSet, error)`.

- [ ] **Step 1 — failing tests** (fixture: `newPreviewRepo` — main + feat with
  three commits; `revParse`). Tests:
  - `TestPairNotesMatchesThePreviewSetButIsAPair`: `A = merge-base(main,feat)`,
    `B = feat`. `PairNotes(A,B)` and `PreviewNotes("feat","main")` have equal
    `Tip`, `Base`, `Commits`; `pair.IsPair() && !preview.IsPair()`;
    `pair.Source == "" && pair.Target == ""`; `pair.DiffSpec().Rev == A+".."+B`.
  - `TestPairNotesAcceptsNamesAndFreezesThem`: `PairNotes("main","feat")` →
    `Tip == revParse(feat)` (full 40).
  - `TestPairNotesReversedPairStillHoldsItsTip`: `PairNotes(featTip, A)` (B is
    an ancestor of A) → `Commits == []string{A}` (the new B), `OK()`. Then add a
    note on that tip (`svc.NoteAdd…` as `TestPreviewNoteCountsIncludeHiddenNotes`
    does) and assert `PreviewNoteCounts` total == 1.
  - `TestPairNotesMissingCommitIsEmptyAndSilent`: a 40-hex that does not exist
    → zero set, nil error.
  - `TestPairNotesGathersNewSideOnly`: notes on B new, on the middle commit
    new, on B old side, on A → `PreviewNotesAll` returns exactly the first two.
  - `TestForgeNotesIgnoreAPairSet`: `svc.forgeNotesFor(pairSet, "")` is empty.
- [ ] **Step 2 — run, expect compile failure:**
  `cd <wt> && rtk go test ./internal/domain -run 'PairNotes|ForgeNotesIgnore' -count=1`
- [ ] **Step 3 — implement**

```go
package domain

// IsPair reports a scope built from two COMMITS (PairNotes) rather than from a
// branch pair (PreviewNotes). The names are what differ: a pair has none, so
// every surface that would print or link them must ask this first.
func (set PreviewNoteSet) IsPair() bool { return set.OK() && set.Source == "" }

// PairNotes is a commit pair's note scope: notes are written to b (new side)
// and gathered along a..b. It reads no saved entry, so an unsaved pair link
// gets the very same scope as a saved one. A commit that is not here yields
// the ZERO set and no error (the preview lane's ruling 6).
func (s *Service) PairNotes(ctx context.Context, a, b string) (PreviewNoteSet, error) {
	fa, aok, err := s.ResolveRev(ctx, a)
	if err != nil { return PreviewNoteSet{}, err }
	fb, bok, err := s.ResolveRev(ctx, b)
	if err != nil { return PreviewNoteSet{}, err }
	if !aok || !bok { return PreviewNoteSet{}, nil }
	fa, fb = strings.TrimSpace(fa), strings.TrimSpace(fb)
	key := "pair-revlist:" + fa + ":" + fb
	v, err := s.factory.Cache("preview").GetOrLoad(key, func() (any, error) {
		return query(ctx, s, key, func(ctx context.Context) ([]string, error) {
			return s.repo.RevListRange(ctx, fa, fb)
		})
	})
	if err != nil { return PreviewNoteSet{}, err }
	commits := v.([]string)
	// a..b is EMPTY when b is an ancestor of a (a reversed save). The write
	// target must still be gathered, or a note just written would not show.
	if !slices.Contains(commits, fb) {
		commits = append([]string{fb}, commits...) // fresh slice: the cached one is shared
	}
	return PreviewNoteSet{Tip: fb, Base: fa, Commits: commits}, nil
}
```

  (Check `ResolveRev`'s exact signature in `pair.go`'s `PairAdd` and mirror it.)
- [ ] **Step 4 — green**, then **remove the `slices.Contains` guard**, confirm
  `TestPairNotesReversedPairStillHoldsItsTip` FAILS, restore.
- [ ] **Step 5 — commit** `feat(domain): PairNotes — a commit pair's note scope`.

### Task 2: domain — `NoteScopeResolve`

**Files:** `pairnotes.go`, `pairnotes_test.go`.

**Produces:** `func (s *Service) NoteScopeResolve(ctx, spec string) (PreviewNoteSet, error)`
— always an OK set or an error.

- [ ] **Step 1 — failing tests** `TestNoteScopeResolve` (table): `main...feat`
  → preview set (`Source=="feat"`); `main..feat` → pair set; saved preview id
  and label → preview; saved pair id and label → pair; both kinds sharing a
  label → preview; `nope` → `errors.Is(err, ErrPreviewNotFound)`; `...feat` →
  `errPreviewPairShape`; `..feat` / `main..` → `errPreviewPairShape`;
  `main...gone` → error text `preview: missing: gone`; `main..<absent 40hex>`
  → error text `preview: missing commit: <that sha>`.
- [ ] **Step 2 — red.**
- [ ] **Step 3 — implement.** Order: contains `...` → `PreviewResolve` +
  `PreviewSummary` state switch (move the exact messages from
  `cli/previewflag.go:60-68`) + `PreviewNotes` (+ `!OK()` →
  `preview %s → %s is not previewable`); else contains `..` → split, both
  halves non-empty else `errPreviewPairShape`, `PairNotes`, zero set → find
  which half fails `ResolveRev` → `fmt.Errorf("preview: missing commit: %s", half)`;
  else `PreviewGet` → on `ErrPreviewNotFound` → `PairGet` →
  `ErrPairNotFound` maps back to `ErrPreviewNotFound`; pair → `PairNotes(p.A,p.B)`
  with the same missing-commit error.
- [ ] **Step 4 — green. Step 5 — commit** `feat(domain): NoteScopeResolve — one parser for --preview and MCP`.

### Task 3: CLI + MCP collapse onto `NoteScopeResolve`

**Files:** `internal/cli/previewflag.go`, `internal/mcp/notes.go`, new
`internal/cli/note_pair_test.go`, MCP test file beside the existing preview
note tests.

- [ ] **Step 1 — failing tests.** CLI (fixture `previewRepo` — serial, it uses
  `t.Setenv`): `gg note add --preview main..feat --file a.txt --new-line 1 -m x`
  exits 0 and `gg note list --rev <featTip> --file a.txt` shows it;
  `gg preview add main..feat` then `gg note list --preview <id>` lists it;
  `gg preview diff <pair id>` prints the a..b patch (exit 0);
  `gg diff --preview main..feat --stat` equals `gg diff main..feat --stat`.
  MCP: note add + list with `preview: "main..feat"`.
- [ ] **Step 2 — red.**
- [ ] **Step 3 — implement.** `resolvePreviewTarget` body becomes:

```go
set, err := svc.NoteScopeResolve(ctx, spec)
if err != nil { return previewTarget{}, err }
return previewTarget{Source: set.Source, Target: set.Target, Set: set, Spec: set.DiffSpec()}, nil
```

  Flag help: `"a merge preview or a commit pair: <id>, <label>, <target>...<source> or <a>..<b>"`.
  `mcp.previewSet`: keep the cached/rev refusal, then
  `return s.svc.NoteScopeResolve(ctx, t.Preview)`; extend the `preview` arg
  description with the pair forms. Check `cli/preview.go:242` (`gg preview
  diff`) prints nothing keyed on `tgt.Source`; grep `tgt.Source|tgt.Target|pv.Source|pv.Target`
  across `internal/cli` and give each printing site a pair arm via
  `scopeName(set)` (Task 4 file; create it here if needed):

```go
// scopeName is how prose names a note scope: the branch pair, or <a7>..<b7>.
func scopeName(set domain.PreviewNoteSet) string {
	if set.IsPair() { return set.Base[:7] + ".." + set.Tip[:7] }
	return set.Target + "..." + set.Source
}
```
- [ ] **Step 4 — green; run the existing preview CLI + MCP tests** (messages
  must be byte-identical): `rtk go test ./internal/cli ./internal/mcp -run 'Preview|Note' -count=1`.
- [ ] **Step 5 — commit** `feat(cli,mcp): --preview and the MCP preview arg take a commit pair`.

### Task 4: CLI — pair LINKS on note verbs and highlight

**Files:** create `internal/cli/notescope.go`; edit `note.go:46,100-129,331,658`,
`noteapply.go:114`, `session.go:645-690`, `linkconsume.go:47-57`; tests in
`note_pair_test.go`.

- [ ] **Step 1 — failing tests:**
  - `note add gg://…/a.txt@A..B:1 -m x` → stored on B, new side.
  - `note list gg://…@A..B` (bare) lists all paths; with a path lists one.
  - `note add …@A..B#1`: fixture where `B^..B` and `A..B` number hunks
    differently (two commits touching the same file in different places) — the
    note lands on `A..B`'s hunk 1.
  - delete-only hunk `#N` → exit 2 + `ErrPreviewOldSide` text; `:old:3` → exit 2.
  - `note apply gg://…@A..B` with a batch → notes on B; old-side item skipped as for a preview.
  - `gg show gg://…@A..B` → exit 2 (unchanged). Watch it fail by temporarily
    passing `Pair: true` in `show.go`, then restore.
  - `session highlight add gg://…/a.txt@A..B:1` no longer exits 2 at the shape
    gate (assert with `--no-wait` against a temp steer dir, as the existing
    highlight link tests do).
  - link form == flag form: `note list <pair link>/a.txt` output equals
    `note list --preview A..B --file a.txt`.
- [ ] **Step 2 — red.**
- [ ] **Step 3 — implement** `notescope.go`:

```go
// noteScopeFromLink is previewTargetFromLink for BOTH bounded kinds: a merge
// preview link (the resolver already built its set) or a change-set link
// (built here — Resolved.Preview stays nil for a pair on purpose: linknav and
// the steer layers dispatch on it and would refuse a nameless preview).
func noteScopeFromLink(ctx context.Context, svc *domain.Service, res domain.Resolved) (previewTarget, bool, error) {
	if tgt, ok := previewTargetFromLink(res); ok { return tgt, true, nil }
	p := res.Pair
	if p == nil { return previewTarget{}, false, nil }
	set, err := svc.PairNotes(ctx, p.A, p.B)
	if err != nil { return previewTarget{}, false, err }
	if !set.OK() { return previewTarget{}, false, fmt.Errorf("preview: missing commit in %s..%s", p.A, p.B) }
	return previewTarget{Set: set, Spec: set.DiffSpec()}, true, nil
}
```

  Swap each `previewTargetFromLink(*link)` site for it (handle `err` with the
  site's existing error exit). `linkconsume.go`: `linkDiffSpec` calls the
  adapter and the separate `res.Pair` arm is deleted — then run the existing
  `gg diff <pair link>` tests to prove the one lane still yields `A..B`
  (**caveat:** `HunkDiffSpec` may normalise something `DiffSpec()` does not —
  diff both specs in a test; if they differ, keep `HunkDiffSpec` as the single
  constructor and have the adapter's `Spec` come from it for pairs).
  `note.go:46` and `session.go:645` → `linkShapes{Ref: true, Pair: true}`;
  rewrite both comments. `noteLinkShape` `list` arm: `!hasPath && res.Preview == nil && res.Pair == nil`.
  Old side: in `noteAdd`'s link arm and highlight, when the scope is set and
  `link.Side == model.NoteSideOld` → print `domain.ErrPreviewOldSide`, return 2
  (check how the preview link lane refuses it today and share that line).
  Highlight: replace `res.Preview != nil` with the adapter's `ok`.
- [ ] **Step 4 — green + whole package:** `rtk go test ./internal/cli -count=1`.
- [ ] **Step 5 — commit** `feat(cli): gg note and session highlight accept a change-set link`.

### Task 5: TUI — arm the scope (saved row + link landing), refresh, stamp

**Files:** create `internal/tui/saved_pair_notes.go`, `saved_pair_notes_test.go`;
one line each in `saved_pair_open.go` (`handlePairOpenMsg`), `steer_nav.go`
(`steerNavigatePair`), `model.go` (msg arm + `srcNotes` arm).

- [ ] **Step 1 — failing tests** (`savedPairModel` fixture + a note added on B
  via `m.svc`):
  - `TestPairOpenArmsTheNoteScope`: enter on the pair row, pump msgs →
    `m.filesPreviewSet != nil && IsPair()`, `filesPreviewCounts["a.txt"] == 1`,
    file row shows `◆1`; enter on the file → `diffLayer().noteAddr.Commit == B`,
    `previewSet != nil`, the note box text is in `View()`.
  - `TestPairNotesMsgForAnotherViewIsDropped`: hand-built `pairNotesMsg` with
    the right gen but other shas, and one with the right shas but
    `gen-1` → scope stays nil. Watch it fail with the endpoint gate removed.
  - `TestPairNotesMsgStampsAnAlreadyOpenDiff`: open the file BEFORE delivering
    the msg → after delivery the diff layer is stamped and a `loadNotesCmd`
    equivalent is returned (non-nil cmd).
  - `TestPairCountsRefreshAfterANoteWrite`: armed view, add a second note via
    svc, deliver the `srcNotes` dataAvailable path → counts == 2.
  - `TestSteeredPairLandingArmsTheNoteScope`: `applySteer` navigate
    `State:"pair"` → scope armed.
  - `TestClosingThePairViewClearsTheScope`.
- [ ] **Step 2 — red.**
- [ ] **Step 3 — implement:**

```go
type pairNotesMsg struct {
	a, b   string
	set    domain.PreviewNoteSet
	counts map[string]int
	gen    int
}

func (m Model) pairNotesCmd(a, b string) tea.Cmd {
	svc, gen := m.svc, m.previewGen
	if svc == nil { return nil }
	return func() tea.Msg {
		ctx := context.Background()
		set, err := svc.PairNotes(ctx, a, b)
		if err != nil || !set.OK() { return pairNotesMsg{a: a, b: b, gen: gen} }
		counts, _, _ := svc.PreviewNoteCounts(ctx, set)
		return pairNotesMsg{a: a, b: b, set: set, counts: counts, gen: gen}
	}
}

// showsCommitPair: the compare on screen is commit a against commit b. The
// gate reads the ENDPOINTS, never compareTag — another opener (a link-shaped
// compare) tags the same two commits differently.
func (m Model) showsCommitPair(a, b string) bool { … filesView != nil, inCompareMode,
	both Kind()==EndpointCommit, Hash()==a / ==b … }
```

  Handler: gen + `showsCommitPair(set.Base,set.Tip)` + `set.OK()` else drop;
  never overwrite a MERGE scope (`m.previewOpen != nil` → drop); arm both
  fields (nil counts on a refresh keep the old map, as `preview_open.go:289`
  does); if a diff layer is open over this view and unstamped, stamp
  `previewSet`/`noteAddr{Committed, set.Tip, path}` and return `m.loadNotesCmd()`.
  **Gen caveat:** `openCompareFiles` runs `closeFilesView`, which bumps
  `previewGen` — so build the cmd AFTER `openCompareFiles` returns
  (`handlePairOpenMsg`: `m, cmd = m.openCompareFiles(...)`; then
  `cmd = tea.Batch(cmd, m.pairNotesCmd(A, B))`). Same order in
  `steerNavigatePair` (use the resolved `ahash`/`bhash`, on the `nm` model).
  `model.go` `srcNotes` arm: `if s := m.filesPreviewSet; s != nil && s.IsPair() { batch m.pairNotesCmd(s.Base, s.Tip) }`.
  Update the stale comment on `handlePairOpenMsg`.
- [ ] **Step 4 — green + `rtk go test ./internal/tui -run 'Pair|Preview|Note|Steer' -count=1`.**
- [ ] **Step 5 — commit** `feat(tui): a commit pair's diff carries its review notes`.

### Task 6: TUI — pair row badge and scope links

**Files:** `preview_panel.go` (`readPreviews` pair loop), `link.go:215,229,231`,
`stash_link.go` (`pairLinkFor`), `saved_pair_notes.go` (`scopeLinkFor`), tests.

- [ ] **Step 1 — failing tests:** pair row renders `◆1` and `Haystack` strips
  it; `TestScopeLinkDisagreesOnOneFixture` — the SAME fixture opened as the
  merge preview yields `@main...feat/x` links and opened as the pair yields
  `/a.txt@<A40>..<B40>:<line>` (diff row), `/a.txt@A..B` (file row), bare pair
  link (heading row); each parses with `model.ParseLink`.
- [ ] **Step 2 — red. Step 3 — implement:** pair loop:
  `if err == nil && psum.State == domain.PairOK { set → counts → row.notes,row.byPath }`.
  `pairLinkFor(a, b)` keeps its signature and delegates to a new
  `pairFileLinkFor(a, b, path string, line int)` (sets `Link.Path`, `Line`).

```go
func (m Model) scopeLinkFor(set *domain.PreviewNoteSet, path string, line int) (string, bool) {
	if set.IsPair() { return m.pairFileLinkFor(set.Base, set.Tip, path, line) }
	return m.previewLinkFor(set.Source, set.Target, path, line)
}
```

  Replace the three `previewLinkFor(set.Source, set.Target, …)` calls in
  `link.go`. The `.rec` AST gate must stay green.
- [ ] **Step 4 — green. Step 5 — commit** `feat(tui): ◆N on a saved pair and pair links from its diff`.

### Task 7: e2e, help, docs, skills

- [ ] `e2e/scenarios/s97_pair_notes.toml` (use the `writing-e2e-scenarios`
  skill; model on `s91_preview_notes` + `s96_saved_pairs`): preview add a..b →
  note add via pair link → `note list --preview <id>` → `preview rm <id>` →
  `note list --rev <B> --file …` still shows it → `gg show <pair link>` exit 2.
- [ ] Help: one Previews-section row "notes work in a saved diff (new side)" —
  i18n key in all four bundles (`adding-translations` skill).
- [ ] `CHANGELOG.md`, `README.md`, `docs/CLAUDE-details.md` ("Pair notes":
  IsPair, B-in-Commits guard, endpoint gate, Resolved.Preview stays nil).
- [ ] `internal/agentskill/using-gg.md` + `reviewing-with-gg.md`; bump
  `agentskill.Version` and `ReviewVersion`; regenerate BOTH `.claude/skills`
  copies with a throwaway test writing `SkillFile()`; delete the test.
- [ ] Headless smoke (`driving-tui-headless`): scratch repo, saved pair with a
  note → Previews tab (`C-Right` ×3) → enter → enter on file → the note box is
  in the snapshot; `}` lands on it. Check the selected-row overflow; fix only
  if it is a one-line clamp.
- [ ] Commit `docs: pair notes — CHANGELOG, README, details, skills`.

### Task 8: gate and hand-over

- [ ] `git log feat/pair-notes..main` — merge `main` in, resolve, **build the
  merged tree** (`rtk go build ./...`), re-run affected packages.
- [ ] `./test.sh race` on the merged tree (start it immediately; ~17 min).
- [ ] Build a stripped verify binary in the worktree; SendUserFile + absolute path.
- [ ] Advisor review; then ASK before merging. After merge:
  `./build.sh install`, `./build.sh web`, remove worktree + branch, update memory.

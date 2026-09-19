# Links compare dialog (plan 3b-2) Implementation Plan

> **For agentic workers:** this plan is executed **inline by the one session
> that wrote it** (CLAUDE.md: NEVER USE SUB AGENTS). Steps use checkbox
> (`- [ ]`) syntax for tracking.

**Goal:** any two `gg://` links can be compared from inside the TUI — typed or
picked in a dialog, bounded with a base picker, saved, listed and re-opened —
and every frontend answers a link comparison through ONE domain door.

**Architecture:** `domain.CompareLinks` (parse → locate → same-checkout →
`EvalLink` ×2 → `CompareSets`) becomes the only place a link text turns into a
file set; CLI and MCP move onto it. The TUI gains a SET-shaped compare view
(`openLinkCompare`) that is handed its file list and reads bytes through
`FileSet.Source(path)`. Pair-link landing, the "Compare with link…" dialog,
the Previews panel's comparison rows and the bookmark↔shelf cross flow are
four OPENERS of that one view.

**Tech stack:** Go 1.26, Bubble Tea, real `git` in `t.TempDir()`.

**Spec:** `docs/superpowers/specs/2026-09-19-links-tui-design.md` §3.5–3.9, §4,
§5 (D6, D7, D8 accepted). Parent:
`docs/superpowers/specs/2026-09-16-unified-links-design.md` §5.1, §8.

## Global constraints

- A pair is BOUNDED, a point is UNBOUNDED; `CompareSets` is total. No frontend
  screens a pair.
- TARGET FIRST: base `B` for ref `A` rewrites the field to `@B...A`. Gates
  assert the link TEXT.
- Every link is built with `model.Link{…}.String()`, never by concatenation.
  A pair half is a FULL sha (≥ 40). `stash@{N}` never appears in a link.
- `internal/tui` never imports `internal/git`, `linkhist` or `savedcompare`.
  `Model` is a value receiver.
- Every TUI string goes through `i18n.T` with a literal key present in
  ja/ko/zh/ru. CLI/MCP/engine prose stays English.
- Every task carries a **break table**: each row is applied with the GUARD
  removed, the named test must go RED, the file is restored. A break that
  fails to BUILD is not a red test — redo it.
- Link fixtures always include a LOCAL-form row (`gg:///abs/…`).
- A comparison only READS a member present on BOTH sides (an A/D row is
  decided without bytes in `CompareSets`). The TUI diff loader is different:
  an `A` row still reads its NEW side — Task 4 uses exactly that.
- Commit per task. Trailers on every commit:
  `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>` and
  `Claude-Session: https://claude.ai/code/session_01XuwzeyRENXkAu7UGZ5J3Up`.
- Worktree: `/mnt/t/others/gigagit/.claude/worktrees/links-compare`, branch
  `feat/links-compare`. Every shell command starts with `cd` to it; every
  Write/Edit uses the worktree's absolute path.

## Rulings made while planning (each: what · why · cost if wrong)

| # | ruling |
|---|---|
| **P1** | **The view's first opener is pair LANDING, not the dialog** (Task 4), and the dialog follows in Task 5. §3.6 says "dialog and view in the same task" *because* a door with no caller and a view with no opener are not mergeable. Landing is an opener, so the reason is met and the task halves in size. Cost if wrong: none at merge — both land in this one branch. |
| **P2** | **The door has a one-sided half, `EvalLinkText`.** The CLI's mixed case (`gg compare HEAD <link>`) needs parse → locate → same-checkout → `EvalLink` for ONE token. `CompareLinks` is `EvalLinkText` ×2 + `CompareSets`. After this plan `EvalLink(` has no caller outside `internal/domain` (archtest gate). |
| **P3** | **Cross-repo is its own sentinel, `domain.ErrLinkCrossRepo`**, not a wrapped `model.ErrLink`: wrapping would prefix "bad gg link:" onto a link that is well-formed. The CLI maps both to exit 2. |
| **P4** | **`BaseSuggestion` carries `Self`** — the point's own full sha — beyond the spec's `{Base, Why}`. A user types `@abc1234`; `@p..sha` needs 40 chars on both halves. Without `Self` the rewrite would need a second git call or would concatenate a short sha. |
| **P5** | **The rewrite is `model.Link.WithBase(base, self)`** — pure, in `model`, so 3c's JS twin is gated against the rewrite as well as against `BoundKind`. |
| **P6** | **`linkHistHost` returns ALL of a host's pickers** (`histPickers() []*linkHistPicker`); the load fills each. The dialog has two fields. A pick in the dialog FILLS its field; only `#` submits on pick. |
| **P7** | **`ctrl+enter` is not bound.** Terminals do not deliver it distinctly. Submit is `enter` on link 2 (and `enter` on link 1 moves to the next row). |
| **P8** | **`gg session state` keeps reporting a links compare as its two ENDPOINTS.** `session_snapshot.go` is a report, not a restore; nothing re-opens from it. Recorded, not built. |
| **P9** | **Save comparison's label field starts EMPTY**; the store fills `DefaultLabel` for an empty label (`savedcompare/file_store.go:134`). `tui` cannot import `savedcompare`, and a prefilled default would need a domain twin of a one-line rule. |
| **P10** | **Naming: a two-link saved entry is a "comparison row"** in code and UI (`previewRow.cmp`). `feat/saved-pairs` (another session, unmerged at planning time) calls a SET entry holding `@A..B` a "pair". Two meanings of "pair row" in one panel is the look-alike-arms defect waiting to happen. |
| **P11** | **`using-gg` is NOT bumped** unless Task 3 changes a CLI message or flag. The door is a refactor for the CLI. If `feat/saved-pairs` merges first it takes v85; re-check the number at the docs task. |

## The other session (read before Task 7)

`feat/saved-pairs` rewrites `internal/tui/preview_panel.go`,
`preview_actions.go`, touches `link.go` and `steer_nav.go`, adds
`domain/pair.go`, and makes `previewRow` two-kind behind an AST gate that
forbids `.rec` outside `merge()`. **Before starting Task 7** run
`git -C /mnt/t/others/gigagit log --oneline main..feat/saved-pairs | head -1`.
Empty output means it merged: merge `main` into this branch first, re-read
`preview_panel.go`, and add the comparison row as the THIRD kind through that
file's accessor pattern instead of the bare `cmp` field below. Tasks 1–6 are
disjoint from that branch except `steer_nav.go` (Task 4) — re-read the merge
there too.

## File map

| file | responsibility |
|---|---|
| `internal/domain/comparelinks.go` (new) | `LinkSide`, `LinkSideError`, `ErrLinkCrossRepo`, `LinkComparison`, `EvalLinkText`, `CompareLinks` |
| `internal/domain/suggestbase.go` (new) | `BaseSuggestion`, `SuggestBase`, trunk detection |
| `internal/git/remotehead.go` (new) | `RemoteDefaultBranch` — one `symbolic-ref` |
| `internal/model/linkbound.go` (new) | `LinkBoundKind`, `Link.BoundKind`, `Link.WithBase` |
| `internal/cli/compare.go` | `compareLinkSet` shrinks to `EvalLinkText`; link×link goes through `CompareLinks` |
| `internal/mcp/links.go` | `gg_compare_links` body shrinks to the door; `sameLinkRepo` leaves |
| `internal/archtest/evallink_door_test.go` (new) | no `EvalLink(` outside domain |
| `internal/tui/link_compare.go` (new) | tag, load cmd, `openLinkCompare`, `compareSides`, `pointLinkFor` |
| `internal/tui/link_compare_popup.go` (new) | the dialog: two fields, pickers, swap, inline errors |
| `internal/tui/link_compare_base.go` (new) | base rows: kind, suggestion, completion, rewrite |
| `internal/tui/save_compare_popup.go` (new) | the label prompt + `SavedCompareAdd` |
| `internal/tui/preview_panel.go`, `preview_actions.go` | comparison rows |
| `internal/tui/bookmark_popup.go`, `shelf_popup.go`, `bookmark_compare.go` | cross arms |

## The break harness (recreate once; never committed)

`$SCRATCH/brk.py <file> <pkg> <run-regex>` reads `OLD\n===\nNEW` on stdin,
asserts OLD occurs exactly once, writes NEW, runs
`go test <pkg> -run <regex> -count=1`, prints `RED` (non-zero AND output lacks
`[build failed]`/`cannot use`/`undefined:`), `GREEN-BAD` or `BUILD-BAD`, then
restores the file byte-for-byte in a `finally`.

---

### Task 1: the door — `EvalLinkText` and `CompareLinks`

**Files:** create `internal/domain/comparelinks.go`,
`internal/domain/comparelinks_test.go`.

**Produces:**

```go
type LinkSide int
const ( LinkSideLeft LinkSide = iota; LinkSideRight )
func (s LinkSide) String() string // "left" | "right"

type LinkSideError struct { Side LinkSide; Err error }
func (e *LinkSideError) Error() string // Side.String() + ": " + Err.Error()
func (e *LinkSideError) Unwrap() error

var ErrLinkCrossRepo = errors.New("cross-repository compare is not supported yet")

type LinkComparison struct {
    Left, Right         FileSet
    LeftText, RightText string
    Files               []model.CommitFile
}

func (s *Service) EvalLinkText(ctx context.Context, text string, o ResolveOpts) (FileSet, error)
func (s *Service) CompareLinks(ctx context.Context, leftText, rightText string, o ResolveOpts) (LinkComparison, error)
```

`EvalLinkText`, in order: `model.ParseLink` → `s.TopLevel` → default
`o.Cwd = s` when nil → `LocateLink(ctx, l, o)` → `SameCheckout(checkout, top)`
else `fmt.Errorf("%s names a different repository (%s): %w", text, checkout, ErrLinkCrossRepo)`
→ `l.Path = rel` → `s.EvalLink`. Move `compareLinkSet`'s doc comment (WHY the
locate comes first) here; it is the rule's home now.

`CompareLinks`: `EvalLinkText` left (wrap in `&LinkSideError{LinkSideLeft, err}`),
right likewise, then `CompareSets` (its error returned bare — it belongs to
neither side). Texts are stored as given, untrimmed input trimmed once with
`strings.TrimSpace`.

- [ ] **Step 1 — fixture + failing tests.** `compareLinksFixture(t)` returns
  `(svc, abs, c1, c2)`: `newRealRepo`, write `sub/a.go` + `sub/b.go`, commit
  (`c1`), change BOTH files, commit (`c2`); `abs` is the `gg://`-ready
  checkout path exactly as `TestALocalFormFileLinkDescribesAsItsFile` builds
  it (Windows drive rule included). Add `git remote add origin
  https://example.invalid/x/r.git` so the NAME form `gg://r/…` also locates.

  | test | asserts |
  |---|---|
  | `TestCompareLinksLocalFormFileLinkIsOneFile` | up front: `CompareLinks(gg://abs@c1, gg://abs@c2)` has **2** files (the arms differ). Then `gg://abs/sub/a.go@c1` ↔ `gg://abs/sub/a.go@c2` → exactly `M sub/a.go`. |
  | `TestCompareLinksNameFormAgreesWithLocalForm` | `gg://r/sub/a.go@c1 ↔ gg://r/sub/a.go@c2` gives the same `Files` as the local-form row. |
  | `TestCompareLinksTwoLocalFileLinksAreOneCheckout` | `gg://abs/sub/a.go@c1 ↔ gg://abs/sub/b.go@c2` is NOT refused; result is `D sub/a.go` + `A sub/b.go` (key-based algebra, D8). |
  | `TestCompareLinksRefusesAnotherCheckout` | right = a second real repo registered through `ResolveOpts.OpenFn`/registry as the existing cross-repo resolve tests do → `errors.Is(err, ErrLinkCrossRepo)`, `errors.As` → `Side == LinkSideRight`, and `!errors.Is(err, model.ErrLink)`. |
  | `TestCompareLinksGrammarErrorNamesItsSide` | left `"gg://"`-garbage → `errors.Is(err, model.ErrLink)` THROUGH the wrapper and `Side == LinkSideLeft`; swap the arguments → `LinkSideRight`. |
  | `TestCompareLinksKeepsTheTextsAsGiven` | `LeftText/RightText` equal the inputs (a name-form text is not rewritten to local form). |
  | `TestCompareLinksStashSetReadsItsOwnSource` | `stashFixture` (3b-1): `gg://abs@<parent> ↔ gg://abs@<parent>..<sha>` lists the untracked file as `A`, and `c.Right.Source("scratch.txt") != c.Right.Endpoint()`. |

- [ ] **Step 2 — run:** `go test ./internal/domain -run 'TestCompareLinks' -count=1` → FAIL (undefined).
- [ ] **Step 3 — implement** as specified above.
- [ ] **Step 4 — run** the same command → PASS; then `go test ./internal/domain -count=1`.
- [ ] **Step 5 — break table.**

  | break | must go red |
  |---|---|
  | delete `l.Path = rel` | `…LocalFormFileLinkIsOneFile` |
  | replace the `SameCheckout` check with `true` | `…RefusesAnotherCheckout` |
  | delete `Unwrap` | `…GrammarErrorNamesItsSide` |
  | wrap the right side with `LinkSideLeft` | `…GrammarErrorNamesItsSide` (swapped half) |
  | store `l.String()` instead of the given text | `…KeepsTheTextsAsGiven` |

- [ ] **Step 6 — commit:** `feat(domain): CompareLinks — the one door from two link texts to a comparison`.

---

### Task 2: MCP onto the door (prove the defects first)

**Files:** `internal/mcp/links.go`, `internal/mcp/links_test.go`.

- [ ] **Step 1 — the proving tests, written against UNCHANGED code.**

  ```go
  // F4: gg_compare_links evaluated a parsed local-form FILE link without
  // locating it, so the file half stayed inside Repo.Abs.
  func TestCompareLinksNarrowsALocalFormFileLink(t *testing.T) {
      e := newTestEnv(t)
      c1 := gitRun(t, e.dir, "rev-parse", "HEAD")
      for _, f := range []string{"a.txt", "b.txt"} {
          os.WriteFile(filepath.Join(e.dir, f), []byte("changed "+f+"\n"), 0o644)
      }
      gitRun(t, e.dir, "add", "-A"); gitRun(t, e.dir, "commit", "-m", "both")
      c2 := gitRun(t, e.dir, "rev-parse", "HEAD")
      whole := e.call(t, "gg_compare_links", map[string]any{"left": linkTo(e, "@"+c1), "right": linkTo(e, "@"+c2)})
      if n := len(whole["files"].([]any)); n != 2 {
          t.Fatalf("fixture: whole-tree compare lists %d files, want 2 — the arms must differ", n)
      }
      out := e.call(t, "gg_compare_links", map[string]any{"left": linkTo(e, "/a.txt@"+c1), "right": linkTo(e, "/a.txt@"+c2)})
      files := out["files"].([]any)
      if len(files) != 1 || files[0].(map[string]any)["path"] != "a.txt" {
          t.Fatalf("files = %v, want exactly a.txt", files)
      }
  }

  // The same missing locate made sameLinkRepo compare two UNSPLIT Repo.Abs
  // values, so two files of ONE checkout read as two repositories.
  func TestCompareLinksTwoFilesOfOneCheckoutAreNotTwoRepositories(t *testing.T) { … a.txt@c1 vs b.txt@c2 → no error, D a.txt + A b.txt }
  ```

  (`b.txt` must exist at `c1`: if `newTestEnv` does not create it, the fixture
  adds and commits it before taking `c1`.)

- [ ] **Step 2 — run** `go test ./internal/mcp -run 'TestCompareLinks' -count=1`
  and **record the outcome in the commit message**: each test either FAILS
  (defect proven) or PASSES (refuted — then say so, keep the test, and the
  move is a pure de-duplication).
- [ ] **Step 3 — move.** The tool body becomes: `repoCheck` →
  `s.svc.CompareLinks(ctx, in.Left, in.Right, domain.ResolveOpts{Cwd: s.svc})`
  → rows. Errors pass through (`LinkSideError` already says `left: …`;
  a bare error keeps the `comparing: ` prefix). Delete `sameLinkRepo`; delete
  `linkRepoLabel` only if `grep -n linkRepoLabel internal/mcp` shows no other
  caller. `TestCompareLinksRefusesTwoRepositories` asserts the substring
  `different repositor` so both the old and new wording satisfy it — then
  tighten it to `cross-repository`.
- [ ] **Step 4 — run** `go test ./internal/mcp -count=1` → PASS.
- [ ] **Step 5 — break table.**

  | break | must go red |
  |---|---|
  | in `domain/comparelinks.go` delete `l.Path = rel` | `…NarrowsALocalFormFileLink` (MCP) |
  | restore the old `sameLinkRepo` pre-check in the tool body | `…TwoFilesOfOneCheckout…` |

- [ ] **Step 6 — commit:** `fix(mcp): gg_compare_links goes through domain.CompareLinks` (or `refactor(mcp): …` if Step 2 refuted both).

---

### Task 3: CLI onto the door + the archtest gate

**Files:** `internal/cli/compare.go`, `internal/cli/compare_test.go`,
`internal/archtest/evallink_door_test.go` (new).

**Consumes:** `EvalLinkText`, `CompareLinks`, `ErrLinkCrossRepo`.

- [ ] **Step 1 — failing tests.**
  - `TestCompareTwoLocalFormFileLinksThroughTheDoor` (cli): the Task 2 fixture
    shape; `gg compare gg://abs/a.txt@c1 gg://abs/a.txt@c2` → stdout exactly
    `M\ta.txt\n`, exit 0. Same expectation as MCP's and (Task 4) the TUI's —
    that triple IS §5's "one door" row.
  - `TestCompareCrossRepoLinkStillExitsTwo` and
    `TestCompareBadLinkStillExitsTwo`, `TestCompareUnknownRepoLinkExitsOne`:
    pin today's exit codes BEFORE the move (they should pass now; they are the
    net for the move).
  - archtest: walk `internal/{cli,mcp,tui,web}` non-test `.go` files, fail on
    any `.EvalLink(` selector call (AST: `*ast.SelectorExpr` with
    `Sel.Name == "EvalLink"`). RED now (cli + mcp… mcp already moved → cli only).
- [ ] **Step 2 — run** → the archtest fails naming `cli/compare.go`.
- [ ] **Step 3 — move.**
  - `compareLinkSet` body → `fs, err := svc.EvalLinkText(ctx, tok, linkResolveOpts(statePath, svc))`;
    error → `compareLinkExit(err, stderr)`: prints `compare: <err>`, returns 2
    for `model.ErrLink` or `domain.ErrLinkCrossRepo`, else 1.
  - In `cmdCompare`, when BOTH tokens satisfy `isLinkArg`: call
    `svc.CompareLinks`; on `*LinkSideError` print the INNER error (messages
    stay byte-identical to today's) through `compareLinkExit`; feed
    `c.Left/c.Right/c.Files` into the existing output + `--patch` guards
    (`sideLosesItsKeySet` needs the sets). Everything else keeps
    `resolveCompareSpec` (s95 pins `# frozen compare:` and its codes).
  - `recordCompareLinks` is untouched.
- [ ] **Step 4 — run** `go test ./internal/cli ./internal/archtest -count=1` and
  `go test ./e2e -run 'S95|s95' -count=1` → PASS.
- [ ] **Step 5 — break table.**

  | break | must go red |
  |---|---|
  | `compareLinkExit` returns 1 for `ErrLinkCrossRepo` | `…CrossRepoLinkStillExitsTwo` |
  | print the wrapped error (`left: …`) instead of the inner | whichever existing test pins a link error message; if none does, add `TestCompareLinkErrorTextHasNoSidePrefix` and break again |
  | add `svc.EvalLink(ctx, l)` back into `compareLinkSet` | the archtest |

- [ ] **Step 6 — commit:** `refactor(cli): gg compare reaches links through the door; archtest forbids EvalLink outside domain`.

---

### Task 4: the set-shaped view, opened by pair landing

**Files:** create `internal/tui/link_compare.go`, `link_compare_test.go`;
modify `model.go` (fields + msg route), `files_view.go` (`beginFilesView`
clears, diff open uses `compareSides`), `diff_view.go`
(`loadCompareDiffCmd` call sites), `steer_nav.go` (`steerNavigatePair`).

**Produces:**

```go
func linkCompareTag(left, right string) string // "links:" + left + "\x00" + right
type linkCompareLoadedMsg struct {
    tag                 string
    c                   domain.LinkComparison
    leftDesc, rightDesc string
    err                 error
}
func (m Model) startLinkCompare(left, right string) (Model, tea.Cmd)
func (m Model) openLinkCompare(msg linkCompareLoadedMsg) (Model, tea.Cmd)
func (m Model) compareSides(l contentLine) (left, right model.Endpoint)
func (m Model) pointLinkFor(sha string) (string, bool) // full sha only; mirrors pairLinkFor
// Model: filesSets *domain.LinkComparison; linkCompareWant string
```

- `startLinkCompare`: tag; if the view is open in compare mode with
  `m.compareTag == tag` → no-op (the "already showing" rule, now keyed on
  TEXT); else `m.linkCompareWant = tag` and return a cmd running
  `svc.CompareLinks(ctx, left, right, linknav.Opts(m.statePath, svc))` plus
  `svc.DescribeLink` for each parsed side.
- Route `linkCompareLoadedMsg`: `msg.tag != m.linkCompareWant` → drop. Error →
  `m.linkCompareWant = ""` (retryable), then: the dialog on top → Task 5
  handles it; a parked steer with this tag → `failPending("the compare failed: …")`;
  else `statusMsg = i18n.T("compare: %s", …)`.
- `openLinkCompare`: `beginFilesView`; `filesMode = filesModeCompare`;
  `filesLeft/Right = c.Left.Endpoint(), c.Right.Endpoint()` (refuse through
  the existing `endpointComparable` status line if either is not);
  `filesSets = &c`; `compareTag = msg.tag`; title
  `leftDesc + " ↔ " + rightDesc`; `filesHash` by `openCompareFiles`' own
  switch (extract it to `compareFilesHash(left, right)` and call it from
  both); lines = `commitFileLines(c.Files)`; tree focused. Then
  `drainPendingCompare()`.
- `compareSides(l)`: `filesSets == nil` → `m.filesLeft, m.filesRight`; else
  `filesSets.Left.Source(oldP), filesSets.Right.Source(l.path)` (`oldP` =
  `l.oldPath` when set). The compare arm at `files_view.go:837` becomes
  `left, right := m.compareSides(l)` feeding BOTH `m.diffTag` and
  `loadCompareDiffCmd` — so the Differ cache key (`compareDiffKey`) is built
  from the endpoints actually read (a third-parent read can never be served a
  diff cached under `b`).
- `beginFilesView` sets `m.filesSets = nil`.
- `steerNavigatePair`: after both halves resolve, `left, lok := m.pointLinkFor(ahash)`,
  `right, rok := m.pairLinkFor(ahash, bhash)`. Both ok → `m.steerToPanels()`,
  `startLinkCompare(left, right)`, and ALWAYS park
  `pendingSteer{stage: steerStageCompare, tag: linkCompareTag(left, right)}` —
  the view is not open yet, so the reply is sent from the loaded handler:
  `c.File == ""` → `navigateLanded(c, "opened "+pair)`, else
  `drainPendingCompare`. The `▸ opened a..b` notice moves there too. Not ok
  (no link form for this checkout) → today's `openCompareFiles` path,
  unchanged.
- `drainPendingCompare` handles `c.File == ""` by calling `navigateLanded`.

- [ ] **Step 1 — failing tests** (`stashLinkModel`-style fixture with a `-u`
  stash: one tracked edit `tracked.txt`, one untracked `scratch.txt`; helper
  `landPair(t, m, a, b, file)` sends the steer navigate the way
  `steer_nav_test.go` already does and pumps cmds with `runCmds`).

  | test | asserts |
  |---|---|
  | `TestLandingAStashPairListsItsUntrackedFile` | up front: `svc.CompareFiles(commit a, commit b)` does NOT list `scratch.txt` (the arms differ). After landing: rows contain `A scratch.txt` and `M tracked.txt`; `m.filesSets != nil`. |
  | `TestAnAddedUntrackedRowReadsItsOwnSource` | open `scratch.txt`'s diff from that view, pump → `diffLayer().err == nil` and the new side holds the file's text. (With `FileRef` of `b` the read fails — the file is not in `b`.) |
  | `TestLinkCompareTagIsTheTextsNotTheEndpoints` | `startLinkCompare(gg://abs@c1, X)` then `startLinkCompare(gg://abs/sub/a.go@c1, X)` → the second REPLACES the first (row count 2 → 1). Then the same pair again → returned cmd is nil. |
  | `TestAStaleLinkCompareLoadIsDropped` | start A, start B, deliver A's msg → view still not showing A's title. |
  | `TestAFailedLinkCompareIsRetryable` | right = unparseable → status line set, `linkCompareWant == ""`; starting the same pair again returns a non-nil cmd. |
  | `TestLandingAPairRepliesAfterTheViewOpens` | no reply is queued before `linkCompareLoadedMsg` is delivered; after it, the reply says `opened <a7>..<b7>`. With `File: "scratch.txt"` the cursor lands on that row. |
  | `TestLocalFormFileLinksCompareAsOneFileInTheTUI` | §5 "one door", TUI leg: the Task 1 fixture through `startLinkCompare` → exactly `M sub/a.go`. |
  | `TestPointLinkRefusesAShortSha` | `pointLinkFor("abc1234")` → `ok == false`. |

- [ ] **Step 2 — run** `go test ./internal/tui -run 'TestLanding|TestLinkCompare|TestAnAdded|TestAStaleLink|TestAFailedLink|TestLocalFormFileLinks|TestPointLink' -count=1` → FAIL.
- [ ] **Step 3 — implement.** New tests call `t.Parallel()` and build repos via `testRepo(t, dir)`/`newRepo(t)` (never a raw `NewExecRunner`).
- [ ] **Step 4 — run** the regex, then `go test ./internal/tui -count=1`.
- [ ] **Step 5 — break table.**

  | break | must go red |
  |---|---|
  | `compareSides` returns `m.filesLeft, m.filesRight` always | `TestAnAddedUntrackedRowReadsItsOwnSource` |
  | `steerNavigatePair` takes the `openCompareFiles` path always | `TestLandingAStashPairListsItsUntrackedFile` |
  | `linkCompareTag` built from `c.Left.Endpoint().CacheTag()` (move it into the loaded handler to compile) | `TestLinkCompareTagIsTheTextsNotTheEndpoints` |
  | drop the `msg.tag != m.linkCompareWant` gate | `TestAStaleLinkCompareLoadIsDropped` |
  | keep `linkCompareWant` on error | `TestAFailedLinkCompareIsRetryable` |
  | reply from `steerNavigatePair` immediately | `TestLandingAPairRepliesAfterTheViewOpens` |
  | `beginFilesView` stops clearing `filesSets` | add `TestAnOrdinaryCompareAfterALinkCompareForgetsTheSets` (open a link compare, then `openCompareFiles(c1, c2)`, assert `filesSets == nil`), break again |

- [ ] **Step 6 — i18n:** new keys (at least `compare: %s` exists already — reuse; add only what is new) into all four bundles; run `go test ./internal/tui -run 'I18n|i18n' -count=1`.
- [ ] **Step 7 — commit:** `feat(tui): a set-shaped compare view; pair links land through domain.CompareLinks`.

---

### Task 5: the "Compare with link…" dialog

**Files:** create `internal/tui/link_compare_popup.go`,
`link_compare_popup_test.go`; modify `linkhist_picker.go` (P6),
`goto_commit_popup.go`, `command_palette.go`, `help.go`/`popup_help.go`,
`model.go` (msg route), four i18n bundles.

**Consumes:** `startLinkCompare`, `linkCompareLoadedMsg`, `linkHistPicker`.

**Produces:**

```go
type linkCompareSide struct {
    input textfield
    hist  linkHistPicker
    err   string
    // Task 6 adds the base row here.
}
type linkComparePopup struct {
    popupMax
    side  [2]linkCompareSide
    focus int  // row index into rows(); Task 6 makes rows() dynamic
    busy  bool // a compare is in flight
    err   string // an error that belongs to neither side
}
func (m Model) openLinkComparePopup() (Model, tea.Cmd) // bumps linkHistGen, returns linkHistCmd
type linkHistHost interface{ histPickers() []*linkHistPicker }
```

Behaviour:
- Palette row `i18n.T("Compare with link…")` between *Browse remote branches*
  and *File blame*; it pops the palette, then pushes the dialog.
- `tab`/`shift+tab` walk the rows; `↓` on a link row enters ITS picker;
  a pick FILLS that field, clears its `err`, stays in the dialog.
- `ctrl+s` swaps the two `input` values (and, from Task 6, their base rows).
- `enter` on link 1 → focus the next row. `enter` on link 2 → submit when
  both fields are non-empty after `TrimSpace`; `busy = true`.
- Submit = `m.startLinkCompare(l, r)`. The loaded handler, when the top layer
  is this popup: error → `busy = false`; `errors.As(*domain.LinkSideError)` →
  `side[Side].err`, else `p.err`; success → `handOffToFilesView` wrapping
  `openLinkCompare` (the dialog is parked, and esc on the view returns to it
  with both fields intact).
- An "already showing" no-op submit (nil cmd) just hands off to the open view.
- Editing a field clears that side's `err` and `p.err`. `space` is dropped
  (a link holds none). `esc` closes; `ctrl+c` quits. `ctrl+t` maximizes
  (`popupMax`).
- Box laid out with `popupBox` at `popupTextWidth`; link texts elided with
  `elideMiddle`; the footer names `tab`, `↓ history`, `ctrl+s swap`, `enter`,
  `esc`.
- Help: the palette section of `help.go` gains the command; the dialog has a
  `?`-less footer (it is a text form — `?` is input).

- [ ] **Step 1 — P6 first, test-first:** `TestTheHistoryLoadFillsEveryPickerOfItsHost`
  (open the dialog with two recorded links, pump → both `side[i].hist.rows`
  have 2 rows). RED: interface still singular. Change the interface, make
  `gotoCommitPopup.histPickers()` return its one picker, `loadedLinkHist`
  loop. The 3b-1 picker tests must still pass.
- [ ] **Step 2 — failing dialog tests** (helper `linkDialog(t, m)` asserts the
  top layer's type; `typeText(m, s)` sends runes).

  | test | asserts |
  |---|---|
  | `TestThePaletteListsCompareWithLinkAlphabetically` | the label sits between the two neighbours (index order in the palette slice). |
  | `TestAPickFillsItsOwnFieldAndDoesNotSubmit` | focus link 2, `↓`, `enter` → `side[1].input` = the link, `side[0].input` still empty, `busy == false`, the dialog is still on top. |
  | `TestCtrlSSwapsTheTwoSides` | left = a REF link, right = a COMMIT link (different kinds, so a no-op swap cannot pass) → texts exchanged. |
  | `TestSubmitOpensTheSetView` | Task 1 fixture, local-form file links → after pumping, the files view shows `M sub/a.go`, `filesSets != nil`. |
  | `TestEscOnTheViewReturnsToTheDialogWithItsFields` | after the above, esc → top layer is the dialog, both inputs unchanged. |
  | `TestASideErrorRendersUnderItsOwnField` | right unparseable → `side[1].err != ""`, `side[0].err == ""`, `busy == false`; the rendered box shows the message BELOW field 2's line (index of the message line > index of field 2's line, and field 1's next line is not it). Then mirror it for the left. |
  | `TestEditingClearsTheError` | type a rune into field 2 → `side[1].err == ""`. |
  | `TestEnterOnLinkOneMovesOnAndNeverSubmits` | both fields filled, focus 0, enter → focus 1 (link 2 while no base rows), `busy == false`. |
  | `TestAnEmptySideDoesNotSubmit` | right empty, enter on link 2 → no cmd, not busy. |
  | `TestTheDialogNeverExceedsItsWidth` | an 80-col model with a 300-char link → every rendered line's display width ≤ the box width. |

- [ ] **Step 3 — run** → FAIL. **Step 4 — implement. Step 5 — run** the package.
- [ ] **Step 6 — break table.**

  | break | must go red |
  |---|---|
  | a pick calls submit (copy `#`'s behaviour) | `…FillsItsOwnFieldAndDoesNotSubmit` |
  | a pick always fills `side[0]` | same test (it focuses link 2) |
  | the error handler writes `side[0].err` regardless of `Side` | `…RendersUnderItsOwnField` |
  | success uses `popLayer` instead of `handOffToFilesView` | `TestEscOnTheViewReturnsToTheDialog…` |
  | `loadedLinkHist` fills only `histPickers()[0]` | `TestTheHistoryLoadFillsEveryPickerOfItsHost` |
  | move the palette row to the end | `…Alphabetically` |

- [ ] **Step 7 — i18n + the AST gates** (`go test ./internal/tui -run 'I18n|i18n|Menu|Vocab|RenderOrder' -count=1`).
- [ ] **Step 8 — commit:** `feat(tui): Compare with link… — two link fields over the copied-link history`.

---

### Task 6: `BoundKind`, `WithBase`, `SuggestBase`, and the base rows

**Files:** create `internal/model/linkbound.go` (+ test),
`internal/git/remotehead.go` (+ test), `internal/domain/suggestbase.go`
(+ test), `internal/tui/link_compare_base.go` (+ test); modify
`link_compare_popup.go`, `preview_add_popup.go` (extract the completion).

**Produces:**

```go
// model
type LinkBoundKind int
const ( LinkBoundNone LinkBoundKind = iota; LinkBoundRef; LinkBoundCommit )
func (l Link) BoundKind() LinkBoundKind
func (l Link) WithBase(base, self string) (Link, bool)

// git — `git symbolic-ref --quiet --short refs/remotes/<remote>/HEAD`; "" + nil when unset
func (r *Repo) RemoteDefaultBranch(ctx context.Context, remote string) (string, error)

// domain
type BaseSuggestion struct{ Base, Why, Self string } // Why: "upstream" | "trunk" | "parent" | ""
func (s *Service) SuggestBase(ctx context.Context, l model.Link) (BaseSuggestion, error)
```

Rules:
- `BoundKind`: `Path != ""` → None. `Target.Ref != ""` → Ref.
  `State == StateCommitted && Commit != "" && Pair == nil && Preview == nil` →
  Commit. Else None (worktree, staged, pair, preview, a hint-only link).
- `WithBase`: Ref kind → requires `LinkRefOK(base) && base != l.Target.Ref`;
  result target `Preview{Source: l.Target.Ref, Target: base}` (renders
  `@base...ref` — TARGET FIRST). Commit kind → requires both `base` and `self`
  to be ≥ 40 hex; result `Pair{A: base, B: self}`. `Repo`, `Side` and `Hint`
  carry over; anything else → `false`.
- `SuggestBase`, Ref: upstream (`s.repo.UpstreamRef`; any error = none) →
  `RemoteDefaultBranch("origin")` → local `main` → local `master` (each probed
  with `ResolveRev("refs/heads/<n>")`); a candidate EQUAL to the ref is skipped
  and the walk continues; nothing left → `{Why: ""}`. `Why` is `"upstream"`
  for the first, `"trunk"` for the other three. Commit: `Self` = the resolved
  full sha (`ResolveRev`; not ok → error), `Base` = first of
  `s.commitParents(Self)`; a root commit → `Base == ""`, `Why == ""`.
  None kind → zero value, nil. Reads ride the same query wrapper
  `commitParents` uses.

Dialog:
- Each side computes `kind` on EVERY edit (pure: `ParseLink` + `BoundKind`;
  unparseable → None). `rows()` = link 1, [base 1], link 2, [base 2].
- When a side's kind becomes non-None for a text it has no suggestion for, it
  returns `suggestBaseCmd(side, text)` → `baseSuggestedMsg{side, text, sug, err}`;
  the handler drops it unless the field's trimmed text still equals `text`.
  The base field is prefilled with `sug.Base`; its label is
  `i18n.T("base (upstream)")` / `("base (trunk)")` / `("base (parent)")` /
  `("base")`.
- Ref kind only: the base field fuzzy-completes against
  `m.branchNameCandidates()`. Extract `previewAddPopup.suggestions/accept`'s
  core to `branchSuggestions(m, q) []string` and use it from both.
- **`enter` on a base row is the ONLY thing that rewrites a link field:**
  `l.WithBase(baseValue, sug.Self)` → `input = newTextField(newLink.String())`;
  the field is now a preview/pair → kind None → the row disappears; focus goes
  to that side's link field. Refused (`false`) → the side's `err` =
  `i18n.T("that base cannot bound this link")`.
- `ctrl+s` swaps the whole `linkCompareSide` values.
- A `LinkSideError` wrapping `domain.ErrNoMergeBase` renders under its side
  like any other (§4) — covered by a test, no special code.

- [ ] **Step 1 — model tests (table, pure).** Rows: `@ref:feat/x` → Ref;
  `@<40hex>` → Commit; `@abc1234` → Commit; `/f.go@ref:feat/x` → None;
  `@a..b` → None; `@main...feat/x` → None; worktree / `@staged` → None;
  local-form `gg:///abs@ref:feat/x` → Ref (the LOCAL-form row).
  `WithBase`: ref+`main` → `String()` ends `@main...feat/x`; ref+itself →
  false; ref+`bad name` → false; commit + short base → false; commit + full →
  `@<p>..<sha>`; a hint survives; `ParseLink(result.String())` round-trips and
  its `BoundKind()` is None.
- [ ] **Step 2 — git + domain tests.** One repo: `main`, `feat/x` branched off
  it, bare `origin` remote cloned-from so `origin/HEAD` exists.
  `TestSuggestBasePrefersTheUpstream` (set `feat/x`'s upstream to
  `origin/feat/x` → `{origin/feat/x, upstream}`); `…FallsBackToOriginHEAD`
  (no upstream → `{origin/main, trunk}`); `…FallsBackToLocalMainThenMaster`
  (no remote; then rename main→master); `…SkipsTheRefItself` (ref = `main`,
  no remote, a `master` exists → `master`; no `master` → empty);
  `…ForACommitIsItsFirstParentAndItsOwnFullSha` (asked with a 7-char sha →
  `Self` is 40 chars, `Base` = parent, `Why == "parent"`);
  `…ForARootCommitIsEmpty`. End-to-end:
  `TestABoundedRefLinkComparesThroughTheDoor` — `WithBase` of `@ref:feat/x`
  with `origin/main`, its `String()` fed to `CompareLinks` against
  `@ref:main` → lists exactly feat/x's changed file (proves a remote-tracking
  TARGET evaluates).
- [ ] **Step 3 — dialog tests.**

  | test | asserts |
  |---|---|
  | `TestABaseRowShowsOnlyForAnUnboundedPoint` | type a ref link → `rows()` has 3; type a pair link → 2; clear → 2. |
  | `TestTabbingThroughABaseRowRewritesNothing` | ref link in field 1, pump the suggestion, `tab` ×4 → `side[0].input.Value()` byte-identical. |
  | `TestEnterOnTheBaseRowRewritesTargetFirst` | left = `@ref:feat/x` (ref), right = `@<sha>` (commit): the fixture has an `origin` remote and no upstream, so the suggestion is `origin/main`: enter on base 1 → the field TEXT has suffix `@origin/main...feat/x`; enter on base 2 → ends `@<parent40>..<sha40>`. Rows shrink back to 2. |
  | `TestAStaleSuggestionIsDropped` | deliver a `baseSuggestedMsg` whose `text` no longer matches → base field untouched. |
  | `TestEditingBackToAPointBringsTheRowBack` | after a rewrite, replace the field with a ref link → 3 rows again. |
  | `TestSwapCarriesTheBaseRows` | ref left, pair right, `ctrl+s` → the base row now belongs to side 1. |
  | `TestNoMergeBaseRendersUnderItsSide` | two unrelated histories (orphan branch), bounded left → submit → `side[0].err` contains `domain.ErrNoMergeBase.Error()`. |

- [ ] **Step 4 — implement; run `go test ./internal/model ./internal/git ./internal/domain ./internal/tui -count=1`.**
- [ ] **Step 5 — break table.**

  | break | must go red |
  |---|---|
  | `WithBase` swaps Source/Target | `TestEnterOnTheBaseRowRewritesTargetFirst` AND the model table |
  | `WithBase` uses `l.Target.Commit` instead of `self` | model table (short-sha row) |
  | `BoundKind` ignores `Path` | model table |
  | `tab` on a base row calls the rewrite | `TestTabbingThroughABaseRowRewritesNothing` |
  | `SuggestBase` returns the ref itself | `…SkipsTheRefItself` |
  | upstream and trunk probes swapped | `…PrefersTheUpstream` |
  | drop the text gate on `baseSuggestedMsg` | `TestAStaleSuggestionIsDropped` |

- [ ] **Step 6 — i18n, AST gates, commit:** `feat: the base picker — BoundKind, SuggestBase, and a target-first rewrite on enter`.

---

### Task 7: Save comparison… and the Previews panel's comparison rows

> Run the **other-session checkpoint** above first.

**Files:** create `internal/tui/save_compare_popup.go` (+ test); modify
`action_menu.go`, `preview_panel.go`, `preview_actions.go`,
`preview_rename_popup.go`, `model.go`, help + four bundles.

**Save:**
- `.` in the files view when `m.filesSets != nil` gains row
  `{id: "save-comparison", label: i18n.T("Save comparison…")}` (add its label
  to `actionLabel`'s switch). It opens `saveComparePopup{input}`; `enter` runs
  `svc.SavedCompareAdd(ctx, sets.LeftText, sets.RightText, TrimSpace(input))`
  in a cmd → `compareSavedMsg{c, err}`.
- `errors.Is(err, domain.ErrSavedCompareExists)` → status
  `i18n.T("already saved as %s", c.Label)`, NOT an error. Success →
  `i18n.T("saved comparison: %s", c.Label)` and a `srcPreviews` refresh.

**Panel:**
- `previewRow` gains `cmp *domain.SavedCompare` and `cmpDesc [2]string`.
  `readPreviews` appends, after the SET rows, every `SavedCompareList` entry
  with `!IsSet()`; each side's description is `svc.DescribeLink` of the parsed
  text (parse failure → the elided text itself).
- Row text: `<label>  <left desc> ↔ <right desc>`; no state cell, no note
  badge. `previewList.Name/Date/Key` read `cmp` when it is set.
- `enter` / `preview-open` on a comparison row → `startLinkCompare(cmp.Left,
  cmp.Right)`; a failure is the ordinary status line and the row STAYS.
- Rename / remove route by shape: comparison → `SavedCompareRename/Remove(cmp.ID, …)`;
  SET rows keep `PreviewRename/Remove`. The rename popup and the remove
  confirm take the id + a `isCmp bool`, never a second copy of themselves.
- Preview-only actions on a comparison row (`preview-swap`, the notes list,
  copy link) decline with `i18n.T("not available for a saved comparison")`.
  The row's `.` menu offers **Copy left link** / **Copy right link**
  (`copyToClipboardCmd`, so both record).

- [ ] **Step 1 — failing tests.**

  | test | asserts |
  |---|---|
  | `TestSaveComparisonStoresTheTwoTexts` | open a link compare, `.` → Save comparison…, enter with an empty label → `SavedCompareList` holds one non-set entry whose `Left/Right` equal the view's TEXTS (name-form in, name-form stored) and whose label is non-empty. |
  | `TestSavingTwiceIsANoticeNotAnError` | second save → status contains "already saved as", list still has 1. |
  | `TestSaveComparisonIsAbsentFromAnOrdinaryCompare` | `openCompareFiles(c1, c2)` → the menu has no `save-comparison` row. |
  | `TestThePanelListsBothShapes` | one merge preview + one saved comparison → 2 rows; the comparison row contains ` ↔ `. |
  | `TestRenamingAComparisonLeavesThePreviewAlone` | §5's row: rename the comparison through the panel → its label changed, the SET's label byte-identical. Then the mirror: rename the SET, the comparison does not move. |
  | `TestRemovingAComparisonLeavesThePreviewAlone` | likewise for remove. |
  | `TestEnterOnAComparisonRowReopensIt` | enter → files view with `filesSets != nil` and `compareTag == linkCompareTag(cmp.Left, cmp.Right)`. |
  | `TestADeadComparisonStaysListedAndCanBeRemoved` | save a comparison whose right is `@ref:gone`, delete the branch → enter sets a status line, the row is still there, remove works. |
  | `TestPreviewOnlyActionsDeclineOnAComparisonRow` | `preview-swap` on a comparison row → the status line; store unchanged. |
  | `TestCopyLeftAndRightLinkCopyTheirOwnHalf` | the two rows write `cmp.Left` and `cmp.Right` respectively to the fake clipboard (left ≠ right in the fixture). |

- [ ] **Step 2–4 — run red, implement, run `go test ./internal/tui -count=1`.**
- [ ] **Step 5 — break table.**

  | break | must go red |
  |---|---|
  | save passes `filesLeft.Display()` texts / `c.Left`-derived strings instead of `LeftText` | `TestSaveComparisonStoresTheTwoTexts` |
  | rename routes every row to `PreviewRename` | `TestRenamingAComparison…` |
  | rename routes every row to `SavedCompareRename` | the mirrored half of the same test |
  | `readPreviews` drops the `!IsSet()` filter | `TestThePanelListsBothShapes` (the SET appears twice → 3 rows) |
  | "Copy right link" copies `cmp.Left` | `TestCopyLeftAndRightLink…` |
  | the menu row is unconditional | `TestSaveComparisonIsAbsent…` |

- [ ] **Step 6 — i18n, gates, commit:** `feat(tui): Save comparison… and saved comparisons in the Previews panel`.

---

### Task 8: bookmark↔shelf cross arms

**Files:** `internal/tui/bookmark_compare.go`, `bookmark_popup.go`,
`shelf_popup.go`, `link.go` (two small builders), tests in
`entry_cross_compare_test.go` (new).

- `bookmarkLink(b) (string, bool)` and `shelfEntryLink(e) (string, bool)`:
  extract the two `L`-key bodies from 3b-1 (`hintedLinkFor(b.Address(),
  {bookmark, b.ID})`; the shelf twin over `e.Origin` with `{shelf, e.ID}`) and
  call them from `L` too — one producer per entry kind.
- `pendingCompare` gains `link string` and `fromShelf bool`; the two
  switcher→switcher starts (`bookmark_popup.go:362-365`,
  `shelf_popup.go:478-484`) fill them. The consuming popup copies them to
  `compareLink` / `compareCross` (cross = the first pick came from the OTHER
  switcher).
- On enter, **only when `compareCross`**:

  | first | second | action |
  |---|---|---|
  | commit | commit | both links ok → `handOffToFilesView(startLinkCompare(first, second))`; either missing → today's `startEntryCompare` |
  | commit | file / file | commit | both links ok → the same door; missing → `▸ no gg link for this place` |
  | file | file | unchanged two-ref diff |

  Same-kind flows (bookmark→bookmark, shelf→shelf) are untouched, refusals
  included.
- The door's failure while a switcher is parked: the ordinary status line; a
  gone bookmark commit keeps `entryGoneText`'s sticky notice (check
  `errors.As` through `LinkSideError`).
- After the change, `grep -n` each refusal string: a string with no remaining
  caller leaves all four bundles.

- [ ] **Step 1 — failing tests.**

  | test | asserts |
  |---|---|
  | `TestCrossCommitCommitGoesThroughTheDoor` | bookmark(commit c1) → shelf switcher → shelved commit c2: view opens with `filesSets != nil`. Same fixture bookmark→bookmark: `filesSets == nil` (the arms DISAGREE on one fixture). Both list the same paths. |
  | `TestCrossCommitAgainstAFileIsLegal` | bookmark(commit c1) → shelf FILE entry of `sub/a.go` (shelved from a dirty working tree): exactly one row, `sub/a.go`, no refusal text in `statusMsg`. Then file → commit. |
  | `TestSameKindCommitAgainstAFileIsStillRefused` | bookmark(commit) → bookmark(file) → the old refusal string. |
  | `TestCrossFileFileKeepsItsTwoRefDiff` | bookmark file `sub/a.go` → shelf file `sub/b.go` (DIFFERENT paths): a diff layer opens titled `sub/a.go ↔ sub/b.go`; no files view. |
  | `TestACrossCompareOfAGoneShelvedCommitOpensFrozen` | shelve a commit, make its sha unreachable (`git update-ref -d` + `reflog expire` + `gc --prune=now` as the existing frozen-shelf tests do) → the cross compare still opens (the `?shelf=` hint's exception 2). |
  | `TestLStillCopiesTheSameLinks` | 3b-1's `TestLCopiesABookmarksLink` family passes unchanged (run, don't rewrite). |

- [ ] **Step 2–4 — red, implement, green (`go test ./internal/tui -count=1`).**
- [ ] **Step 5 — break table.**

  | break | must go red |
  |---|---|
  | `compareCross` is always true | `TestSameKindCommitAgainstAFileIsStillRefused` and the `filesSets == nil` half of the first test |
  | `compareCross` is always false | `TestCrossCommitAgainstAFileIsLegal` |
  | route cross file↔file through the door | `TestCrossFileFileKeepsItsTwoRefDiff` |
  | `shelfEntryLink` drops the hint | `TestACrossCompareOfAGoneShelvedCommitOpensFrozen` |

- [ ] **Step 6 — commit:** `feat(tui): bookmark↔shelf — commit arms through the door, commit↔file made legal, file↔file untouched`.

---

### Task 9: docs, headless round trip, the gate

- [ ] **Docs.** `CHANGELOG.md` section "Compare with link…";
  `README.md` (the palette command, the base picker, saved comparisons in the
  Previews tab, the closed stash window); `docs/CLAUDE-details.md` "Links in
  the TUI" grows: the door, the set-shaped view + text tag, P1–P10 in one
  paragraph each where they are gotchas (P3, P4, P6, P8). `CLAUDE.md`: the
  `domain` row already names the algebra — add `CompareLinks` in ≤ 1 line only
  if it still fits one row. `using-gg`: per P11.
- [ ] **Help/footer audit:** the palette help lists the command; the Previews
  tab help names comparison rows; nothing advertises what does not exist.
  `go test ./internal/tui -run 'Help|Footer|I18n|i18n' -count=1`.
- [ ] **Headless, against the BUILT binary** (`go build -o $SCRATCH/gg ./cmd/gg`;
  `--gg` wrapper sets `XDG_STATE_HOME=$SCRATCH/state`, unsets
  `WAYLAND_DISPLAY XDG_RUNTIME_DIR DISPLAY`, `PATH=/usr/bin:/bin`), in a
  scratch repo with a `-u` stash, a `feat/x` branch and a remote:
  1. copy the stash link (3b-1), `#`, `↓`, enter → the view lists
     `A scratch.txt` (**the window is closed**);
  2. palette → Compare with link… → `↓` pick the branch link → the base row
     shows `(upstream|trunk)` → enter → the field reads `@…...feat/x` → second
     field: a commit link → submit → the view;
  3. `.` → Save comparison… → enter → Previews tab shows the row → enter
     re-opens it → rename → remove;
  4. `gg compare --list` from the shell (same `XDG_STATE_HOME`) shows and then
     no longer shows the row — TUI and CLI see ONE store.
  Keep the snapshots in `$SCRATCH`; quote the decisive lines in the final report.
- [ ] **Repo-local skill copy** only if `using-gg` changed:
  `HOME=$SCRATCH/home gg init --update`.
- [ ] **Merge main into the branch** if it moved; re-read the merge in
  `steer_nav.go`, `preview_panel.go`, `preview_actions.go`, `link.go`,
  `internal/i18n/lang/*.toml` for guards it invalidated.
- [ ] **Gate the merged tree:**
  `./test.sh race > $SCRATCH/race.log 2>&1; echo EXIT=$?` — start it
  immediately, do not wait for other sessions.
- [ ] **Advisor review, then ASK before merging.** After the merge:
  `./build.sh install`, `gg init --update`, remove the worktree and branch,
  update `memory/links-tui-plan-3b.md` + the `MEMORY.md` line.

---

## Spec coverage

| spec | task |
|---|---|
| §3.5 door, typed per-side errors, cross-repo refused | 1 |
| §3.5 "CLI and MCP shrink to a call"; F4 proven or refuted | 2, 3 |
| §3.6 view, text tag, `Source(path)`, title | 4 |
| "known window" — pair landing via `#`, `gg open`, `gg session navigate` (all reach `steerNavigatePair`) | 4 |
| §3.7 palette, two fields, picker, swap, inline errors | 5 |
| §3.7 `BoundKind`, `SuggestBase`, rewrite table, "nothing rewritten until enter" | 6 |
| §3.8 save; panel rows; rename/remove by shape; Copy left/right | 7 |
| §3.9 cross arms | 8 |
| §4 errors table | 4 (retry), 5 (inline), 6 (no merge base), 7 (dead row) |
| §5 gates | each task's table; "one door" = Tasks 2+3+4 sharing one expectation |
| §7 what 3c inherits | `CompareLinks`, `SuggestBase`, `BoundKind`, `WithBase` all exported and pure where promised |

Deviations from the spec's letter, all ruled above: P1 (task split), P4
(`Self`), P5 (`WithBase` in model), P7 (no `ctrl+enter`), P9 (empty label).

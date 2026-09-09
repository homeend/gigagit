# Merge preview — design

**Date:** 2026-09-09 · **Branch:** `feat/merge-preview` · **Status:** approved in chat
(user chose: GitHub three-dot semantics; a new left-panel tab; machine-local
per-repo store; TUI + CLI + web in this stage; local + remote-tracking
branches as sides; row summary = label, source → target, files, commits ahead)

## Problem

The existing "Compare A ↔ B" (Branches tab, `m` + `m`) is a plain tip-to-tip
tree diff (`git diff A B`). It shows every difference between the two tips,
i.e. changes made on BOTH sides since they diverged. A pull-request review
shows something else: only what the source branch would bring into the
target, which GitHub computes as the three-dot diff `git diff target...source`
(merge-base to source tip).

The user wants:

1. that PR-style view for any (source, target) pair, and
2. to **save** a pair so it can be reopened with one keypress, from a tab in
   the top-left panel, with the view always reflecting the CURRENT tips
   (recomputed when either branch moves, without re-selecting anything).

## Definitions

- **Source** — the branch whose changes are being previewed (GitHub's
  "compare" branch). **Target** — the branch it would be merged into
  (GitHub's "base"). Written `source → target` everywhere: titles, rows,
  dialogs, CLI output, web.
- **Merge preview of `source → target`** — the file set and diffs of
  `git diff target...source`: from `M = merge-base(target, source)` to the
  tip of `source`. Commits only; the working tree and index never take part.
  It is NOT a trial merge: nothing is said about conflicts.
- **Preview record** — a saved pair. Stores branch NAMES, never hashes; every
  open resolves the current tips. That is the whole "dynamic" mechanism.
- A side may be a **local branch** (`feat/x`) or a **remote-tracking branch**
  (`origin/main`). Tags and raw shas are not offered by the pickers (the CLI
  passes any name through `ResolveRev`; a name that no longer resolves is the
  "missing" state, so a tag typed at the CLI simply works until deleted).

## Non-goals (follow-ups, listed not built)

- MCP tools for previews (the read surface can add `gg_previews_list` /
  `gg_preview_files` later; the domain API below is shaped for it).
- A "commits ahead" list per preview (the PR *Commits* tab). The data is one
  `git log target..source` away; a later stage can open the scoped commit view.
- Per-row `+/-` line counts (a numstat pass per pair per tip change).
- A conflict probe (`git merge-tree --write-tree`) marking "would conflict".
- Sharing previews through `.gg.toml`. The store is machine-local only.

## Architecture

```
  TUI Previews tab / pair dialog   CLI `gg preview …`   web sidebar group + /api/preview
                 \                        |                        /
                  └──────────── internal/domain (preview queries) ─┘
                                   |                 |
                        internal/preview (store)   internal/git verbs
                        XDG state, TOML + lock     ResolveCommit, rev-list, diff --name-only
```

Every frontend reaches previews through `domain.Service`; none imports
`internal/preview` (archtest, like `notes`/`bookmark`/`shelf`).

### `internal/preview` — the store (new leaf package)

Mirrors `internal/notes`: one TOML file per repo under the XDG state dir,
keyed the way notes and bookmarks key the repo (git common dir), written under
the same two locks (process-local mutex + `previews.toml.lock` O_EXCL file
with the stale-lock takeover) because the TUI, the CLI and `gg web` may write
concurrently. No `Sweep`/cap: the list is short and user-curated.

```go
type Record struct {
    ID      string    // derived: short hash of "source\x00target" — direction-sensitive
    Source  string    // branch name, e.g. "feat/login" or "origin/feat/login"
    Target  string    // branch name, e.g. "main" or "origin/main"
    Label   string    // human label; defaults to "<source> → <target>"
    Created time.Time
}

type Store interface {
    Add(r Record) (Record, error)       // ErrExists when the (source,target) id is present
    Get(id string) (Record, error)      // ErrNotFound
    List() ([]Record, error)            // Created order, oldest first
    Rename(id, label string) error
    Remove(id string) error
}
```

`model.MergePreview` (a plain data mirror of `Record`) is what domain and the
frontends pass around; the store's own type stays inside the package.

Test seams follow notes: `domain.PreviewsDisabled` for tests that never want
the store touched, `svc.UsePreviewsDir(dir)` to point it at a temp dir.
Windows: the store checks `XDG_STATE_HOME` first ([[windows-test-suite]]).

### `internal/git` — verbs (one invocation each)

- `ResolveCommit(ctx, ref)` — exists; used for both sides.
- `CountLeftRight(ctx, target, source) (behind, ahead int, err)` — NEW:
  `git rev-list --left-right --count target...source`. `ahead` is the
  right-side count = commits on source not in target. `behind` is reported
  but only `ahead` is shown in this stage.
- `DiffNameOnlyRange(ctx, target, source) ([]string, error)` — NEW:
  `git diff --name-only -z target...source`.
- `MergeBase(ctx, a, b)` — exists; run as part of the summary (see below)
  and kept in the cached summary so opening a preview needs no extra call.

All three summary/open verbs take HASHES, not names: the domain resolves names
first so nothing mutable reaches a cache key or the diff cache (the rule from
the compare-branches gotcha: resolve a ref to hex before it touches a cache).

### `internal/domain` — queries

```go
// Store CRUD (no git; a Read reservation is not needed — the store is not the repo).
func (s *Service) PreviewAdd(ctx, source, target, label string) (model.MergePreview, error)
func (s *Service) PreviewList(ctx) ([]model.MergePreview, error)
func (s *Service) PreviewGet(ctx, idOrLabel string) (model.MergePreview, error)
func (s *Service) PreviewRename(ctx, id, label string) error
func (s *Service) PreviewRemove(ctx, id string) error

// Live state of one pair; the row summary. Two rev-parse calls always (name →
// hash is how movement is detected); the summary itself is cached by (srcHash, tgtHash).
type PreviewState int // PreviewOK | PreviewMerged | PreviewMissingSource | PreviewMissingTarget | PreviewNoBase
type PreviewSummary struct {
    State      PreviewState
    SourceHash string // "" when missing
    TargetHash string // "" when missing
    Files      int    // len(diff --name-only target...source); 0 unless PreviewOK
    Ahead      int    // commits on source not in target; 0 unless PreviewOK
}
func (s *Service) PreviewSummary(ctx, source, target string) (PreviewSummary, error)

// The two compare endpoints for the view: left = merge-base, right = source tip.
type PreviewEndpoints struct {
    Summary     PreviewSummary
    Left, Right model.Endpoint // both EndpointCommit; zero when State != PreviewOK
}
func (s *Service) PreviewOpen(ctx, source, target string) (PreviewEndpoints, error)
```

Rules:

- `PreviewAdd` validates each side resolves at add time (`ResolveRev`);
  a non-resolving name is refused with a clear error ("`foo` is not a
  branch"). Adding the same `(source, target)` again returns the existing
  record and `ErrExists`; frontends treat that as "focus the existing row".
- `PreviewSummary`: resolve both sides (`queryQuiet`, no session-error noise
  for a missing branch); missing → the matching Missing state. Then
  `CountLeftRight`; a merge-base failure (no common ancestor) → `PreviewNoBase`.
  `Ahead == 0` → `PreviewMerged` (files not computed). Otherwise
  `DiffNameOnlyRange` → `Files`, `PreviewOK`. The base is probed FIRST
  (`MergeBase`; unrelated histories make rev-list report every commit on
  both sides rather than fail), so the OK case is three git calls under one
  Read reservation, single-flight-coalesced (key
  `preview-summary:<srcHash>:<tgtHash>`) and cached in a session-lived LRU
  (`internal/cache`) keyed by the hash pair. Resolving the two NAMES costs
  two `rev-parse` calls on every refresh regardless — that is how tip
  movement is detected — so an unchanged pair is two cheap calls, never
  zero; only a moved pair pays the three summary calls.
- `PreviewOpen`: `PreviewSummary` + `MergeBase(targetHash, sourceHash)` →
  `Left = {Commit, M}`, `Right = {Commit, sourceHash}`. From here the existing
  `CompareFiles(left, right)` / `Differ` path serves the view unchanged, and
  its diff cache stays correct because both endpoints are hashes.
- Untracked/working-tree logic in `CompareFiles` is not triggered (both
  endpoints are commits).

### TUI

**Previews tab.** A fourth entry in `leftTabs` (`panelPreviews`) after
Worktrees, with its own `sourceKey` `srcPreviews` (consumer: `panelPreviews`).
The list is `model.MergePreview` rows joined with their `PreviewSummary`:

```
 login-fix        feat/login → main           12 files  ↑3
 docs on origin   docs → origin/main          merged
 old-feature      feat/old → main             missing: feat/old
 import           vendor/lib → main           no common base
```

Layout: label column (elided), `source → target` column (elided with the
shared middle-elision), then state/counts right-aligned. Row states carry a
distinct style: merged dim, missing/no-base in the warning colour.

Keys in the tab (all in the footer and the `.` action menu, each with an
`actionMenuLabel` case, all i18n'd in four bundles):

| key   | action |
|-------|--------|
| enter | open the preview (`PreviewOpen` off-thread → `openCompareFiles`) |
| a     | add: a two-field form (Source, Target) with the fuzzy branch picker over local + remote-tracking names; `ctrl+s` swaps the fields; enter on the second field saves and opens; label defaults to `source → target` and can be renamed later |
| e     | rename the label (single text field prefilled; `r` is the global reload key, `e` is the Worktrees-tab rename convention) |
| d     | delete (typed-free confirm popup: "Remove preview `<label>`?") |
| s     | swap direction: saves a NEW record `target → source` (the id is direction-sensitive) and focuses it; the original stays |

Enter on a merged / missing / no-base row shows the state as a notice instead
of opening an empty view.

**Pair-picker row.** In the Branches tab, the `m` + `m` pair popup gains a
row **"Merge preview %s → %s…"** (marked = source, selected = target, the same
operand order as the existing "Merge %s into %s" row so the two never
disagree). It opens a small option dialog whose prompt states the direction
in full:

```
 Preview merging feat/login into main
   Show once
   Show and save          ← adds the record (label = default), then opens
   Swap direction          ← re-renders the dialog as main → feat/login
   Cancel
```

Because the pair picker only exists in the Branches tab, a preview whose
target is remote-tracking (`origin/main`) is created via the tab's `a` form;
this is stated in the tab's help text.

**Compare view in preview mode.** `openCompareFiles(left, right)` as today,
then: `m.filesTitle` overridden to `Merge preview: <source> → <target>`
(`Endpoint.Display()` truncates), `m.comparePair` left nil so the `f` origin
filter is inert and its footer entry hidden, and a new `m.previewOpen
*previewOpenState{id, source, target, srcHash, tgtHash, tag}` records what is
showing. Everything below the file list (per-file side-by-side diff, line
cursor, notes anchored to `FileAddress{Commit: sourceHash, Path}`, N/P
stepping, `/` search, ctrl+t maximize) is untouched.

**Dynamic refresh.**

- `srcPreviews` is NOT mapped per-op in `opAffectedSources` (nearly every op
  moves a ref). Instead it is chained: whenever `srcBranches` or `srcRemotes`
  completes (interval lane, file-watch on refs, post-op partial refresh, `r`),
  the previews source runs. Its loader lists records and calls
  `PreviewSummary` for each off the UI thread; unchanged pairs hit the domain
  cache and cost no git call. Git work for changed pairs runs under the
  existing `LimitRunner`.
- **Open view follows the tips.** After each previews refresh, if
  `m.previewOpen != nil` and the refreshed summary's `(srcHash, tgtHash)`
  differs from the recorded pair, the view re-arms: `PreviewOpen` again →
  `openCompareFiles` with the new endpoints (the compare tag changes, so the
  same-tag guard does not swallow it), then the previously selected path is
  re-selected when it still exists (else cursor to the top), and a one-line
  notice "preview updated: <source> moved" (or "<target> moved") is shown. If
  the new state is not `PreviewOK`, the compare view closes back to the tab
  with the state notice.
- Deleting the record that is currently open closes the view.

**Refresh registry plumbing checklist** (from `adding-tui-windows`): `panel`
enum + `panelPreviews` in `leftTabs`, `srcConsumers`, `sourceNames`/
`sourceDisplayName`, `viewstate` geometry (the left slot already sizes by
`activeLeftTab`), `tab_click_test`, focus cycling, `T`/maximize-left,
`Model.tabTitle` and the footer bindings.

### CLI

```
gg preview list                              # <id>\t<label>\t<source>\t<target>\t<state>\t<files>\t<ahead>
gg preview add [--label <text>] <source> <target>
gg preview rm <id|label>
gg preview rename <id|label> <text>
gg preview show [--patch] <id|label>         # like `gg compare`: <status>\t<path> lines, or unified diffs
gg preview diff <source> <target> [--patch]  # one-off, no record (the "show once" of the CLI)
```

`show` on a non-OK state prints the state to stderr and exits 1 (`merged`,
`missing: <name>`, `no common base`). `preview` is added to the verb table in
`cli.go`. `using-gg.md` gains a "Merge previews" paragraph; `agentskill.Version`
bumps; `gg init --update` after merge.

### Web

- `GET /api/preview` → `{entries:[{id,label,source,target,state,files,ahead,source_hash,target_hash}]}`
  (summaries via `PreviewSummary`; the same cache serves all tabs).
- `POST /api/preview {source,target,label?}` → the record (409 on `ErrExists`
  with the existing id); `POST /api/preview/rename {id,label}`;
  `DELETE /api/preview?id=` (all three behind `writeGuard`, so the client
  sends the JSON content type; the bookmark-endpoint shape, not REST paths).
- `GET /api/preview/open?id=` → `{state, left, right, source, target, label, source_hash, target_hash}`; the
  client then opens the EXISTING compare page with those two hashes, exactly
  as the sidebar's compare rows do. No new diff surface.
- `GET /api/preview/diff?source=&target=` → the one-off form for the pair
  picker's "Show once" (names resolved server-side, allowlist-validated
  against the live branch lists like `/api/versions`).
- Sidebar group **Previews** (`previews.js`, added to `COLLAPSED_DEFAULT` on a
  first run like bookmarks), rows mirror the TUI columns; row menu: open,
  rename, swap direction, remove. A `+` header button opens the add form
  (two selects over local + remote-tracking names, swap button). The branch
  context menu's pair section gains "Merge preview … →" with the same
  once/save dialog. Visibility uses per-id hidden rules (a bare
  `class="hidden"` is always visible in `gg web`).
- Live: the server-side refresh hub already emits "branches/remotes changed"
  events; the client re-fetches `/api/preview` on those. An open preview
  page compares the returned hashes with the ones it opened and re-opens
  itself when they moved, keeping the selected file where possible (the TUI
  rule).

### i18n

Every new TUI string (tab title, row states, footer labels, menu rows, the
dialog prompt and options, form labels, notices) is a literal `i18n.T` key
present in ja/ko/zh/ru. Decision-option VALUES in the dialog stay English.
Engine has no new prose (no new ops: previews are queries only).

### Error handling

- Add with a non-resolving side: refused with the name in the message.
- Duplicate add: focuses the existing row (TUI/web), `ErrExists` message +
  exit 1 with the existing id on stderr (CLI).
- Store I/O or lock failures surface through the session error ring like
  notes; the tab shows "previews unavailable: <reason>" and stays usable.
- A side deleted while a preview is open: handled by the refresh path above.
- Repo re-root / repo switch: the tab reloads from the new repo's store (the
  bookmark/notes precedent).

## Testing

- **store:** TDD in a temp XDG dir — add/get/list/rename/remove, duplicate id,
  direction-sensitivity, lock contention (two writers), stale-lock takeover,
  `XDG_STATE_HOME` precedence.
- **git verbs:** `FakeRunner` argv assertions for the two new verbs; a real
  repo for the parse paths.
- **domain:** real git in `t.TempDir()`: the five states, ahead/files counts,
  the cache hit (FakeRunner call count stays flat on unchanged tips),
  `PreviewOpen` endpoints equal `merge-base` + source tip, missing-name refusal.
- **TUI:** model tests for tab rendering (four states), `a` form + swap,
  pair-dialog once/save/swap, enter opens with the right title and no `f`
  entry, refresh re-arm keeps the selected path, delete-while-open closes;
  the i18n AST gates, `tab_click_test`, and a `tui-capture.sh` headless
  snapshot of the tab.
- **CLI + e2e:** unit tests per verb; one `e2e/scenarios/merge_preview.toml`
  (add, list shows `↑N`, show lists the files, merge → state `merged`).
- **web:** handler tests (CRUD, 409, allowlist refusal, open hashes) and a
  browser probe on a fresh port against a fixture with a diverged pair:
  add via the form, row counts visible, open shows only the source's files,
  commit on the source in a shell → row and open page update without a reload.
  Probe must assert VISIBILITY and identify the binary (memory rules).

## Docs after merge

`CHANGELOG.md`, `README.md` (new tab + verbs), `internal/agentskill/using-gg.md`
+ version bump, `docs/CLAUDE-details.md` (preview package row + the
"names never reach a cache key" note), `docs/web-tui-parity.md`. `CLAUDE.md`
gains one package-map row for `preview`.

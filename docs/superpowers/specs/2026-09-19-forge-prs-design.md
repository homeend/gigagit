# Forge pull requests (read-only) — design

Date: 2026-09-19 · Branch: `feat/forge-prs` · Status: awaiting review

## Goal

Show the current repository's open pull requests inside gg and let the user
read a PR — its diff, its inline review comments, its description and its
conversation — without leaving the TUI. **Read-only:** gg never posts, edits,
resolves or submits anything on the forge. The first (and only) provider is
GitHub through the system `gh` CLI; the design keeps everything above one seam
forge-neutral so GitLab (`glab`) and Gitea (`tea`) are one more implementation.

## User rulings (2026-09-19 — do not re-ask)

1. The PR head is fetched into a gg-private local ref; writing local refs is
   inside "read-only" (nothing reaches the forge).
2. Outdated inline comments (no current position) go to the hub popup's
   "Outdated" section with their original hunk snippet.
3. Resolved threads are marked and collapsed; collapsing is built as a generic
   note-box mechanism so local notes get it too.
4. Only open PRs are *discovered*. Browsing/searching closed/merged ones is a
   follow-up.
5. A read-only CLI ships in this cut; the web UI is a follow-up.
6. Which repository a checkout maps to is `gh`'s answer; no remote picker.
7. Bodies render as wrapped plain text; markdown is a follow-up.
8. Poll interval defaults to 5 minutes, plus a manual refresh key.
9. No forge tool, or the tool cannot read this repo's PRs → the feature is
   not shown at all: no tab, no notice.
10. Detection runs ONCE, at startup. Not available then → the feature is off
    for the whole session (no mid-session re-detection).
11. A PR gg already knows never disappears because it was closed or merged:
    it stays in the list, marked `closed` / `merged`.

## Approach

Chosen: a forge-neutral leaf package `internal/forge` (types + `Provider`
interface + the `gh` implementation), owned by `domain`. Comments are fetched
live and held in memory; they are never written to the `notes` store.

Rejected:
- *Import comments into the `notes` store* — invents sync state, staleness and
  a cleanup sweep for data the forge already owns; pointless while read-only.
- *Render `gh pr diff` text* — loses the whole preview surface (side-by-side,
  syntax colouring, blame, in-view search, note boxes).

## 1. `internal/forge` (new leaf package)

Imports: stdlib + `gitexec` (for the `Runner` seam) only. `domain` is the only
importer; `archtest` gains the rule (as for `notes`/`preview`/`shelf`).

```go
type PullRequest struct {
    Number       int
    Title, Body  string
    Author       string
    State        string    // "open" | "closed" | "merged"
    Draft        bool
    ReviewState  string    // "", "approved", "changes_requested", "review_required"
    Source       string    // head branch name (may live in a fork)
    SourceRepo   string    // "owner/name" when the head is in a fork, else ""
    Target       string    // base branch name
    HeadSHA      string
    BaseSHA      string    // baseRefOid: the diff's left side once closed/merged
    Comments     int       // total comment count, for the row badge
    URL          string
    Created, Updated time.Time
}

type CommentKind int // Inline | FileLevel | General | ReviewSummary

type Comment struct {
    ID, ParentID string    // forge ids, opaque; ParentID threads replies
    Kind         CommentKind
    Author, Body string
    Path         string    // Inline/FileLevel
    Side         string    // "old" | "new"
    Line, StartLine int    // 1-based; StartLine == Line for one-line comments
    Outdated     bool      // the forge reports no current position
    Resolved     bool      // thread resolved (set on every comment of the thread)
    Hunk         string    // original diff hunk snippet (for Outdated)
    Verdict      string    // ReviewSummary only: approved/changes_requested/commented
    Created, Updated time.Time
}

type Provider interface {
    Name() string                                       // "github"
    Detect(ctx context.Context) error                    // nil = usable for THIS repo
    ListOpen(ctx context.Context) ([]PullRequest, error)
    PR(ctx context.Context, n int) (PullRequest, error)
    Comments(ctx context.Context, n int) ([]Comment, error)
    HeadRefspec(n int) string                           // "pull/<n>/head" on GitHub
}
```

`HeadRefspec` is the one forge-specific fact the fetch op needs (GitLab is
`merge-requests/<n>/head`), so it sits on the provider, not in the engine.

### The `gh` provider (`forge/gh.go`)

- Runs through a `gitexec.Runner` built with `NewExecRunner(ghPath, workDir,
  rec)` — the constructor already takes any binary path. `ghPath` resolves
  from `$GG_GH_BIN` (tests/e2e), else `gh` on `PATH`.
- `Detect`: `exec.LookPath`, then `gh pr list --limit 1 --json number`. Exit 0
  ⇒ usable (binary present, repo resolves to a GitHub repo, auth works). Any
  failure ⇒ not usable; the error is kept for `gg pr` diagnostics only.
- `ListOpen`: `gh pr list --state open --limit 100 --json
  number,title,author,isDraft,reviewDecision,headRefName,headRepositoryOwner,
  headRepository,baseRefName,baseRefOid,headRefOid,state,comments,url,createdAt,updatedAt`.
  Cap 100, newest first; the tab shows a "100+ — showing newest" row at the cap.
- `PR`: `gh pr view <n> --json …` (same fields + `body`).
- `Comments`: one `gh api graphql` call for `reviewThreads` (path, line,
  startLine, diffSide, isResolved, isOutdated, comments{id, replyTo, author,
  body, diffHunk, createdAt, updatedAt}, `subjectType` for file-level) plus the
  PR's `comments` (General) and `reviews` with a non-empty body or a verdict
  (ReviewSummary). GraphQL because REST does not expose `isResolved`.
  Paginated to a hard cap of 500 comments; beyond it the hub shows a
  truncation line.
- All parsing is pure functions over the JSON bytes (`parseList`, `parsePR`,
  `parseThreads`), unit-tested from captured fixtures.

`forge.Default(workDir, rec) []Provider` returns the provider list (one entry
today); domain tries them in order and keeps the first whose `Detect` passes.

## 2. Detection (preflight)

`preflight` gains:
- `Feature.Silent bool` — an unsatisfied silent feature yields a verdict the
  TUI notice builder skips (`notify.go` filters `v.Silent`); CLI `gg pr`
  still reports the reason. Today every non-satisfied Optional verdict raises
  a notice, which ruling 9 forbids.
- `ForgeUsable` requirement + `Probes.Forge *ForgeProbe{Provider string; Err
  string}`. `Fit` = Satisfied when `Provider != ""`, else Unsatisfiable.
  `NeedsStoreProbes` = false.

`domain` owns the probe: `Service.ForgeStatus(ctx)` runs `Detect` across
`forge.Default` exactly once per session and caches the verdict — there is no
re-detection (ruling 10); a user who fixes `gh` restarts gg. The probe is
*started* at startup but is not part of the blocking preflight: the TUI fires
it alongside its initial loads and the tab appears when the verdict lands, so
the network call cannot delay first paint. On a repo switch (`R`) the new
repo's service probes once, the same way.

## 3. `FetchPRHead` engine op

`engine.FetchPRHead{Remote, Refspec string; Number int; HeadSHA string}`:
- `LockMode()` = RefWrite.
- Idempotent: if `refs/gg/pr/<n>` already resolves to `HeadSHA`, emit
  `Done` with `Changed=false` and run no git.
- Else one verb: `git fetch --no-tags <remote> +<refspec>:refs/gg/pr/<n>`
  (new `git.FetchRefspec`). `Remote` is the configured git remote whose URL
  names the base repo `gh` reports (`gh repo view --json nameWithOwner`,
  matched host/owner/name across ssh and https spellings) — so the user's own
  transport and credentials are used. Only when no remote matches does it fall
  back to the base repo's URL in the protocol `gh` is configured for
  (`sshUrl`/`url`). Fork PRs work either way: GitHub serves `pull/N/head` from
  the base repo.
- No Decider forks. Failure surfaces as the op error (TUI toast / CLI stderr).
- `git.PRRefPrefix = "refs/gg/pr/"` sits beside `VersionRefPrefix`; the
  existing `--decorate-refs-exclude=refs/gg/*` already keeps it out of the
  graph. Refs are never pruned automatically — a fetched ref is what makes a
  PR "known" across sessions (§4a); the row's *Forget* action deletes it.
- TUI: mapped in `opAffectedSources` to nothing but `srcPRs` (no branch,
  status or feed source changes).

## 4. Domain queries

```go
func (s *Service) ForgeStatus(ctx) ForgeStatus            // {Provider string; Err error}
func (s *Service) PullRequests(ctx) ([]forge.PullRequest, error)   // singleflight
func (s *Service) PullRequest(ctx, n int) (forge.PullRequest, error)
func (s *Service) PRComments(ctx, n int) (PRComments, error)

type PRComments struct {
    Inline   []ResolvedNote   // anchored, threaded; per file via .Note.Address
    Hub      []forge.Comment  // General + ReviewSummary, chronological
    Outdated []forge.Comment  // inline comments with no current position
    Truncated bool
}
```

Inline/FileLevel comments convert to `model.Note`:
- new `NoteSource` `NoteSourceForge = "forge"`; `Author` = forge login;
  `ID` = `forge:<id>` (cannot collide with store ids); `ParentID` likewise.
- `Address` = the PR pair's file address on the preview surface, `Side`/`Range`
  from the comment; FileLevel uses `Range{0,0}` — the renderer's "above the
  first hunk" sentinel.
- `ResolvedNote.Status` = active (the forge already said the position is
  current; no context-hash re-resolution). A new `ResolvedNote.Collapsed bool`
  seeds true for resolved threads.
- `Note.Tags` carries `resolved` for the marker.

`domain.NotesFor…` (the diff view's loader) merges forge notes into the slice
it already returns **when the open diff is a PR pair**; store notes on the same
pair still load and render alongside. Every store mutation path
(`EditNote`/`DeleteNote`/`RemoveAllNotes`/reply) rejects `forge:` ids with a
typed `ErrReadOnlyNote`; `RemoveAllNotes` skips them and counts only store
notes in its typed-confirm prompt.

### 4a. Known PRs — closed/merged rows stay (ruling 11)

`PullRequests` returns the union of:
- **open** — `ListOpen`;
- **known, no longer open** — every n that was in a previous `ListOpen`
  result this session, or that has a local `refs/gg/pr/<n>` (the user opened
  it, in any session), and is absent from `ListOpen`. Each is re-read with
  `PR(n)` once to learn `closed`/`merged`; a terminal state is cached for the
  session and not re-polled (it cannot change back often enough to matter; the
  manual refresh re-reads it).

So a PR the user merely saw stays for the session; a PR the user opened stays
across restarts, because its ref is the durable record — no new state file.
Known rows sort after open ones, newest first. `engine.ForgetPR{Number}`
(RefWrite) deletes the ref and drops the row; it is refused for an open PR
that has no ref (nothing to forget). A merged PR's diff still opens
(`target...refs/gg/pr/<n>` is empty once merged with a merge commit, so for a
`merged`/`closed` row the pair's left side is the PR's recorded base sha —
`baseRefOid` — instead of the moving target tip).

The preview pair for PR n is `target...refs/gg/pr/<n>`. A test pins that
`EvalEndpoint`/the preview summary accept a non-branch full refname; if any
step insists on `refs/heads/`, that step is widened to "any ref that
rev-parses to a commit" (the pair is never *saved* to the `preview` store, so
the store's branch-name shape is untouched).

## 5. TUI

### PRs tab
- `panelPRs`, a fifth left tab. `leftTabs` becomes a method
  `m.leftTabs()` that appends `panelPRs` only when `ForgeStatus.Provider !=
  ""`; every consumer of the slice (tab cycling, number keys, header paint,
  `link.go`, help) goes through it. Once shown, the tab never disappears in a
  session: later provider failures render an error row ("gh: <first line>"),
  with the refresh key to retry — focus is never yanked.
- Row: `#123 Title…  author  source→target  [draft] [✓|✗|…]  💬4`. Width
  elision follows the Previews row painter. Wide glyphs are avoided in favour
  of the existing marker set (see wide-glyph overflow memory).
- Source `srcPRs` in the refresh registry: interval `[refresh] prs_seconds`,
  default **300**, `0` = never, floor `MinSeconds`. No gitwatch trigger (the
  data is remote). `r` on the tab = manual refresh (list + the open PR's
  comments); the global refresh key includes it.
- Closed/merged/unavailable rows are dimmed and carry the state word in place
  of the review badge (§4a).
- Keys on a row: `enter` open the PR diff · `i` PR hub popup · `r` refresh ·
  *Forget* (`.` menu + a key; known non-open rows only, confirm-free since it
  only drops a private ref) ·
  `y`-family copy: PR URL, and the `gg://` pair link · `.` menu with the same
  actions. Footer and help advertise all of them.
- `enter`: run `FetchPRHead` via `domain.Execute`, then open the pair through
  the same entry the Previews tab uses (files view over `target...ref`).
  Title bar shows `PR #123 · title`.

### Inline comments
- Rendered by the existing note-box renderer. Header line: `author · 2d ·
  github` and `· resolved` when tagged. Replies indent as today.
- Read-only: note edit/delete/reply keys on a `forge` note do nothing and
  flash "forge comments are read-only". `}`/`{`, `/` search and "List notes…"
  include them; the list marks the source.
- FileLevel (`Range{0,0}`) boxes render above the file's first hunk, in both
  unified and side-by-side layouts (full width in side-by-side).
- **Collapse (generic):** a collapsed note box is one line:
  `▸ author: first line of summary… (N replies)`. `z`-style toggle key on the
  note under the cursor (exact key chosen in the plan against the diff view's
  key table; must not shadow wrap `z`), plus "collapse/expand all notes" in
  the `.` menu. Collapse state is per open view, in memory; forge-resolved
  threads start collapsed, everything else expanded. Works for store notes
  too (ruling 3).
- While a PR diff is open, its comments re-poll on the `srcPRs` interval; a
  changed result re-delivers `notesLoadedMsg` (cursor and scroll preserved,
  collapse state kept by note id).

### PR hub popup
- `prHubPopup` embedding `popupMax` (ctrl+t). Opened by `i` on a tab row and
  from the open PR diff (`.` menu + same key). Content, top to bottom:
  1. `#123 Title`, then `open · draft? · author · source → target ·
     review state · updated 3h ago`, then the URL.
  2. Description: body as plain text wrapped at `popupTextWidth` (CR
     stripped, tabs expanded, control chars sanitized — same sanitizer as the
     commit-message viewer).
  3. **Conversation**: General + ReviewSummary chronologically; a review shows
     its verdict badge (`approved` / `changes requested` / `commented`).
  4. **Outdated** (only if any): `path:line · author`, the hunk snippet
     (last ≤6 lines, diff-coloured), the body.
- Scroll, `/ @ ] [` in-view search (fourth host of the shared search), `y`
  copies the URL, `esc` returns to whatever opened it (layer-stack rule).
- Loads asynchronously with a spinner row; an error renders in place with the
  retry key.

### i18n
Every new string goes through `i18n.T` with literal keys in all four bundles;
forge-provided text (titles, bodies, logins) is never translated.

## 6. CLI

```
gg pr list [--json]            # open PRs + known closed/merged (state field)
gg pr view <n> [--json]        # header + description + conversation + outdated
gg pr comments <n> [--json]    # inline threads (path:line, side, resolved), then general
gg pr fetch <n>                # FetchPRHead; prints refs/gg/pr/<n>
gg pr forget <n>               # ForgetPR
```

- No usable provider → exit 1 with the `Detect` reason on stderr (the CLI is
  explicit, so it is *not* silent).
- `--json` shapes are the `forge` types with snake_case keys; documented in
  `using-gg.md` (bump `agentskill.Version`). `gg pr fetch` + `gg diff
  <target>...refs/gg/pr/<n>` gives agents the diff.

## 7. Config

`[refresh] prs_seconds = 300` — added per the `adding-config-entries`
checklist (defaults → global → repo overlay, a `settingDoc`, the Settings
popup row next to the other intervals).

## 8. Error handling

| Situation | Behaviour |
|---|---|
| `gh` missing / unauthenticated / not a GitHub repo | no tab, no notice; `gg pr` explains |
| network or rate-limit failure after the tab is shown | error row in the tab / in-place error in hub; previous list kept until a read succeeds |
| fetch of the PR head fails | op error toast; the diff does not open |
| PR closed/merged while listed | row stays, re-marked `closed`/`merged` on the next refresh; it still opens |
| known PR deleted on the forge / `PR(n)` 404 | row stays marked `unavailable`; *Forget* removes it |
| comment on a path absent from the diff | treated as Outdated (hub), never dropped |
| >100 PRs / >500 comments | capped, with a visible truncation row/line |
| `gh` hangs | every call has a 30 s context timeout; SIGTERM via the Runner |

## 9. Testing (TDD)

- `forge`: parse tests from captured JSON fixtures (threads, replies,
  file-level, outdated, resolved, fork heads); argv assertions with
  `FakeRunner`; `Detect` failure modes.
- A Go fixture binary (`internal/forge/testdata/fakegh`, built once per test
  process like engine's `buildGG`) that answers the `gh` invocations from a
  directory of canned JSON — used via `$GG_GH_BIN` by domain, CLI and e2e
  tests on all three OSes (no shell scripts).
- `preflight`: `ForgeUsable` fit + `Silent` filtered from notices.
- `engine`: `FetchPRHead` against a real bare "base" repo with a
  `refs/pull/1/head`; idempotence (countingRunner: zero git calls when
  current); RefWrite lock mode.
- `domain`: comment → note conversion, merge with store notes, mutation
  paths reject `forge:` ids, `RemoveAllNotes` leaves them, stale-ref pruning,
  non-branch refname through the preview path.
- `tui`: tab hidden/shown by `ForgeStatus`; tab never vanishes after shown;
  read-only keys; collapse toggle on forge and store notes; file-level box
  placement; hub popup layout, maximize, search, esc-returns-to-opener;
  refresh source registration; i18n gates. Headless check with
  `tui-capture.sh` against the fake `gh` before calling it done.
- `e2e`: one scenario — list, view, comments, fetch.
- `./test.sh race` before merge.

## 10. Docs on ship

`CHANGELOG.md`, `README.md`, `internal/agentskill/using-gg.md` (+ version
bump, `gg init --update`), one-line `forge` row + the `preflight`/`domain`/
`cli`/`tui` row touch-ups in `CLAUDE.md`, per-feature detail in
`docs/CLAUDE-details.md`, config settings registry.

## Follow-ups (recorded, not built)

1. Web UI: PRs sidebar group + hub overlay + inline comments in the web diff.
2. Markdown rendering for descriptions and comments (TUI + web).
3. Closed/merged PRs and search, modelled on remote-branch exploring.
4. GitLab (`glab`) and Gitea (`tea`) providers behind `forge.Provider`.
5. Write-back: reply, new inline comment, edit own comment, pending review +
   verdict, resolve thread.
6. MCP read surface for PRs/comments.
7. PR checks/CI status in the row and hub.

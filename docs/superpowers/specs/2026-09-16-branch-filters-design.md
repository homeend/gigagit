# Branch filters (five configurable slots, TUI + web) — design

Date: 2026-09-16 · Branch: `feat/branch-filters` · Status: agreed, awaiting plan

## Problem

A monorepo's Branches and Remotes lists hold hundreds of refs, most of them
stale or someone else's. The `/` filter is a transient substring match that
must be retyped every session and cannot express "older than 90 days" or
"not `release/*`". Users want a handful of named, reusable filters they can
flip on and off with one key, defined once per machine or per repo.

## Goals

- Five numbered **filter slots**, defined in `.gg.toml` (global or repo),
  each a named rule that either **hides** matching branches or **shows only**
  matching branches.
- A rule can test the tip commit's **age** (older/younger than a duration)
  and the branch **name** (prefix, suffix, substring, regex). Set clauses
  AND together.
- One key (alt+1…5) activates a slot; the active slot is **remembered per
  repo** and shared by the TUI and `gg web`.
- Applies to **local branches and remote branches**, each list with its own
  active slot.
- Editable from the TUI Settings popup and the web Settings view, not only by
  hand.
- One evaluator in Go so the TUI and the web never disagree on a pattern.

## Non-goals

- No AND/OR composition of slots: exactly one slot is active per list (radio).
- No tags, no worktree list, no branch pickers, no merge/preview popups: the
  filter changes what the two lists SHOW, nothing else reads it.
- No engine operation, no CLI flag, no MCP surface. (Reading a filtered list
  from the CLI is a separate feature if ever wanted.)
- No JavaScript reimplementation of the matcher.
- ctrl+1…5 as requested is not deliverable: terminals send ctrl+1 as `1`,
  ctrl+2 as NUL, ctrl+3 as ESC, ctrl+4 as `ctrl+\`, ctrl+5 as `ctrl+]`
  (Bubble Tea's key table confirms), and browsers reserve ctrl+digit for tab
  switching. Ruling: alt+1…5.

## Config: the slot definition

Array-of-tables in `.gg.toml`, global (`~/.config/gg/config.toml`) or repo
(committed `.gg.toml` or the machine-private repo file, whichever "Repo
settings location" selects):

```toml
[[branch_filters]]
slot = 2                 # 1..5, required, the alt+N key
name = "stale"           # label shown in the panel header / list chip; default "slot N"
mode = "hide"            # "hide" (default) | "show"  — show = show ONLY matching branches
older_than = "90d"       # tip commit age; units d w m y (m = 30d, y = 365d); "" = unset
younger_than = ""        # same syntax
prefix = "feat/"         # name starts with
suffix = ""              # name ends with
contains = ""            # name contains
regex = ""               # Go RE2 (regexp.Compile), unanchored; "" = unset
```

Semantics:

- A branch **matches** a slot when every SET clause holds (AND). A slot with
  no clauses set matches nothing (and is reported as "empty" in Settings).
- `mode = "hide"`: matching branches are hidden. `mode = "show"`: only
  matching branches are shown.
- Name clauses see the **branch part**: for a remote branch `origin/feat/x`
  the name tested is `feat/x` (`model.RemoteBranch.Branch`). Local branches
  test `Name`. Matching is case-sensitive (git ref names are).
- Age clauses compare `now - UnixTime` of the tip. **Unknown tip time
  (`UnixTime == 0`) fails any age clause**, in both modes: an unknown-age
  branch is never hidden by an age rule and never shown by a show-only age
  rule.
- `now` is taken once per evaluation (a list refresh), never per row.

Validation (`branchfilter.Validate`): slot outside 1..5, unknown mode, an
unparsable duration, an uncompilable regex, or a duplicate slot in ONE file
makes the block **inert**, with a one-line reason. An inert slot loads as
"slot N: invalid — <reason>" in Settings and alt+N answers with the reason in
the status line. Never a startup error, never a silently empty list (same
rule as `[[tools.command]]` and templates).

Overlay (`overlayBranchFilters`): keyed by slot number. A repo file's slot N
**replaces** global slot N whole; slots the repo omits fall through from
global. This is the second deliberate exception to the field-level overlay
(the first is `[[tools.command]]`) and is documented in `settingDocs`.
Within one file, a duplicate slot makes the LATER block inert (the first
wins), so hand-editing never silently flips a rule.

`populate.go` / `template.go`: the template gains a commented
`[[branch_filters]]` example block (array blocks are inserted the way theme
blocks are); `PopulateFile` counts a missing example as added.

## The `branchfilter` package (DAG leaf)

`internal/branchfilter` — stdlib only, no git/config/TUI imports.

```go
type Mode string // "hide" | "show"

type Slot struct {
    Slot        int
    Name        string
    Mode        Mode
    OlderThan   time.Duration // 0 = unset
    YoungerThan time.Duration
    Prefix, Suffix, Contains string
    Regex       string
}

// Compiled is a validated slot ready to evaluate; Err != nil ⇒ inert.
type Compiled struct { Slot; re *regexp.Regexp; Err error; Empty bool }

func Compile(s Slot) Compiled
func ParseAge(s string) (time.Duration, error)   // "90d" "12w" "6m" "1y"
func (c Compiled) Matches(name string, unixTime int64, now time.Time) bool

// Verdict for one row under an active slot.
type Verdict struct{ Hidden, Exempt bool }

// Apply evaluates rows[i] = (name, unixTime) under c; exempt[i] rows are
// never hidden but still reported as Exempt when the rule would hide them.
func Apply(c Compiled, rows []Row, exempt []bool, now time.Time) (v []Verdict, hidden int)
```

`config` decodes the TOML into `[]branchfilter.Slot` (config imports the leaf
for the type only, as it imports `theme`); `Compile` runs in domain.

## Exemptions

A row that is HEAD, or a branch checked out in ANY worktree, is **always
shown**, rendered dim-marked (`∗` after the name in the TUI, a `.exempt`
class in the web) when the active rule would have hidden it. In the Remotes
list the exempt row is the current branch's upstream. Consequences: `f`
find-current and the web ⌖ locate always land, and switching never strands
you on an invisible row.

## Active slot: per repo, per list

`promptstate` gains a repo-keyed record (key = git common dir, the scope
`DismissedNotices` already uses):

```toml
[branch_filter."<repoKey>"]
branches = 2   # 0 = none
remotes  = 0
```

`FileStore.BranchFilterSlots(repoKey) (branches, remotes int)` and
`SetBranchFilterSlot(repoKey, list string, slot int) error`. Both frontends
already open the same `prompts.toml` (beside `operations.log` in the gg state
dir), so alt+2 in the TUI is what the web shows on its next fetch. A stored
slot that no longer exists, is inert, or is empty loads as **none** (the
list shows everything, the header says nothing); it is not rewritten.

## Domain

```go
func (s *Service) BranchFilters(ctx) ([5]branchfilter.Compiled, error)      // from EffectiveConfig, compiled once per config load
func (s *Service) ActiveBranchFilter(list string) int                         // "branches" | "remotes"; 0 = none
func (s *Service) SetActiveBranchFilter(list string, slot int) error          // validates 0..5, persists
func (s *Service) FilterBranches(ctx, bs []model.Branch, ws []model.Worktree) (verdicts []branchfilter.Verdict, hidden int, active *branchfilter.Compiled, err error)
func (s *Service) FilterRemoteBranches(ctx, rbs []model.RemoteBranch, head model.Branch) (…same…)
```

The two `Filter*` queries compute exemptions (HEAD + worktree branches;
HEAD's upstream) and call `branchfilter.Apply`. They are pure over their
inputs so the TUI can memoise and the web can call them per request.
Reads stay under the existing read reservation; `SetActiveBranchFilter` is a
state-file write, not a git write, and takes no reservation.

## TUI

- **Keys:** `alt+1`…`alt+5` on the focused Branches or Remotes panel select
  that slot; pressing the ACTIVE slot's key clears it. On any other panel the
  key is a no-op with a status line: "alt+1…5 filter the Branches/Remotes
  panel (focus one first)". An inert/empty slot answers "slot N: <reason>"
  and leaves the active slot unchanged.
- **Evaluation:** `displayIndices` drops hidden rows BEFORE the `/` text
  check, so `/` stacks on top. Verdicts are memoised per (panel, list
  version, active slot) in a `branchFilterMemo` modelled on
  `filter_memo.go`; the memo is invalidated on every branches/remotes source
  write and on config reload.
- **Header:** the panel label becomes `Branches ▽2 stale · 12 hidden`
  (`▽N name` when nothing is hidden). Exempt rows carry a dim `∗`.
- **Footer/help:** footer pin `[alt+1-5] filter` on the two panels, a help
  row. `find_current.go` needs no new case: the row it looks for (HEAD, or
  HEAD's upstream on Remotes) is exempt, so a slot can never hide it.
- **Settings → "Branch filters…":** a popup listing slots 1–5, one line
  each: `2  stale      hide  older than 90d, prefix feat/   (repo)`; an
  inert slot shows its reason, an unset slot shows `—`. `enter` edits,
  `d` clears (asks scope when the slot exists in both files). The editor is a
  form modelled on `commit_filter_popup.go`: Name, Mode (toggle), Older than,
  Younger than, Prefix, Suffix, Contains, Regex, Scope (global | repo; repo
  honours "Repo settings location"). `enter` validates via
  `branchfilter.Compile` (a bad regex is refused inline, never saved), writes,
  reloads config, and re-evaluates the open lists. New strings go through
  `i18n.T` with all four bundles.
- **Writer:** `config.SetBranchFilter(path string, s branchfilter.Slot) error`
  and `config.RemoveBranchFilter(path string, slot int) error` — scoped
  line-edit writers that replace or remove the ONE `[[branch_filters]]` block
  whose `slot = N`, preserving everything else (precedent: the theme-role and
  tools writers). No block for N ⇒ append.
- **Verification before wiring:** confirm with `tui-capture.sh` that `alt+1`
  arrives in Bubble Tea as the single key `"alt+1"` under tmux (ESC-prefixed
  alt can split on escape-time); if it does not, the plan adds the tmux
  `escape-time 0` note to the headless skill and treats a bare ESC+digit
  pair inside 50 ms as alt+digit.

## Web

- **Wire:** `GET /api/branches` rows gain `hidden` and `exempt` booleans and
  the response gains `filter: {slot, name, mode, hidden}` (null when none).
  The client keeps the FULL array in `state.branches` (pickers, drop targets,
  `is_head` lookups unchanged) and `renderBranches` skips `hidden` rows.
  `GET /api/remotes` DROPS hidden rows server-side BEFORE the 100-row cap
  (sort → filter → cap), so the section still means "the 100 most recent
  remote branches you asked to see"; nothing client-side addresses an
  unrendered remote row. Its header carries the same `filter` object.
- **Set:** `PUT /api/branch-filter {"list":"branches"|"remotes","slot":0..5}`
  under `writeGuard`, allowlisted values only; answers the new header so the
  client re-renders without a refetch, then `live.js` refetches on the next
  branches/remotes event as usual.
- **Chip:** each of the two list headers gets a `▽` chip next to the sort
  chip. Click → menu `none / 1 name / 2 name / …`; inert or empty slots are
  listed disabled with their reason. Active: `▽2 stale · 12 hidden`.
- **Keys:** `alt+1…5` → Branches, `alt+shift+1…5` → Remotes, matched on
  `e.code === "DigitN"` (with alt held `e.key` is a dead or accented character
  on several layouts), ignored while focus is in an input/textarea or a
  prompt is open. Same radio rule: the active slot's key clears.
- **Settings view:** a *branch filters* section listing the five slots with
  an inline edit form (same fields as the TUI, scope select global/repo) and
  a clear button. Extends `settingsWriteRequest` with
  `branch_filters: [{slot, name, mode, older_than, younger_than, prefix,
  suffix, contains, regex, scope, remove}]`; the server validates every item
  with `branchfilter.Compile` before writing any (the handler's existing
  "refuse the whole request first" rule). Regex text crosses the wire as
  data only; it is compiled, never executed as anything else.
- Web state (`/api/uistate`) is untouched: the active slot is per repo, which
  uistate is not.

## Error handling

| Situation | Behaviour |
|---|---|
| Invalid regex / duration / mode / slot in TOML | slot inert with reason; Settings shows it; alt+N says it; list unfiltered |
| Duplicate slot in one file | later block inert ("duplicate of slot N") |
| Stored active slot no longer valid | loads as none; nothing rewritten |
| Editor save fails (no repo config path, write error) | status line "branch filter 2 not saved: <err>", popup stays open |
| `prompts.toml` unwritable | alt+N still applies for the session; status line notes "not remembered: <err>" |
| Filter hides the row the cursor was on | cursor clamps to the nearest visible row (existing `panelView` behaviour) |

## Testing

- **`branchfilter`:** `ParseAge` table (d/w/m/y, garbage, negative); `Compile`
  inert cases; `Matches` for each clause, AND composition, remote branch-part
  naming, unknown time under each mode; `Apply` exemptions and hidden count.
- **`config`:** decode, overlay by slot (repo replaces, omitted falls through,
  duplicate-in-file inert), template/populate contain the example block,
  `SetBranchFilter`/`RemoveBranchFilter` replace exactly one block and leave
  the rest byte-identical.
- **`promptstate`:** round-trip, unknown repo ⇒ zeros, missing file ⇒ zeros.
- **`domain`:** compiled-once-per-config, `Filter*` verdicts with HEAD /
  worktree / upstream exemptions, stale stored slot ⇒ none.
- **`tui`:** `displayIndices` hides under a slot and `/` stacks; exempt HEAD
  stays; alt+N select / same-key clear / other-panel status; inert slot
  refused; header text and footer pin exact strings; find-current message;
  Settings popup rows and editor save path against a temp config file; the four AST i18n gates.
- **`web`:** handler verdict flags, remotes sort→filter→cap order with a
  250-row fixture, `PUT /api/branch-filter` allowlist + persistence, settings
  POST refuses a bad regex without writing, `e.code` key routing unit-tested
  in the JS harness. Playwright probe (skill `playwright-web-verification`)
  against the UNFIXED build first, then the worktree build, on an isolated
  `XDG_STATE_HOME`, asserting row VISIBILITY.
- **Headless TUI capture** proving alt+1 toggles and the header reads
  `Branches ▽1 …`.

## Rulings already made — do not re-ask

- Keys are **alt+1…5** (ctrl+digit is unreachable on both surfaces).
- **Radio**: one active slot per list, same key clears.
- Scope: **local + remote branches**, active slot **independent per list**.
- Definitions in **TOML + editor UI** (TUI Settings popup, web Settings).
- Active slot **persisted per repo** in `prompts.toml`, shared by TUI and web.
- **One evaluator in Go**; web receives verdicts.
- **HEAD / worktree-checked-out / upstream rows are always shown**, marked.
- Within a slot, set clauses **AND**; `mode` picks hide vs show-only.
- Unknown tip time **fails** any age clause.

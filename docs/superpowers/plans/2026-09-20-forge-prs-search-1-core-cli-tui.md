# Forge PR search — plan 1: seam, domain, CLI, TUI

> **Execution:** inline, by the session that wrote it (project rule: NEVER
> subagents), task by task, TDD, one commit per task. Steps use `- [ ]`.

**Goal:** find closed / merged / any-state pull requests from `gg pr list`
and from a TUI search popup (`A` in the PRs tab).

**Architecture:** `forge.Provider` gains one read, `Search(PRQuery)`. `domain`
wraps it (`PRSearch` — number lookup, single-flight, a session-only last
result) and re-exports the query type. The CLI adds flags to `gg pr list`;
the TUI adds a two-zone popup layer whose rows reuse the PRs-tab painter and
whose opens reuse `openPRCmd` / `openPRHub`.

**Spec:** `docs/superpowers/specs/2026-09-20-forge-prs-search-design.md`
(plan 2 = the web half).

## Global constraints

- Read-only: no gh verb that mutates (the argv test's token list applies).
- Frontends never import `internal/forge`; they use `domain.PRQuery` etc.
- States `all|open|closed|merged`; default `all`; limit default 50, 1…200.
- gh ALWAYS gets `--search`, with `sort:updated-desc` appended unless the text
  holds a `sort:` qualifier; `--limit` is `Limit+1` (the `more` probe).
- A search never adds to `forgeSeen` (the normal list does not grow).
- Every TUI string through `i18n.T`, literal key, in ja/ko/ru/zh.
- New tests call `t.Parallel()` unless they touch process-wide state.
- Worktree: `/mnt/t/others/gigagit/.claude/worktrees/forge-prs-closed-search`;
  every command uses absolute paths / `git -C`.

---

### Task 1: the seam — `forge.PRQuery`, `Provider.Search`, gh, fakegh

**Files:** modify `internal/forge/forge.go`, `internal/forge/gh.go`,
`internal/forge/testdata/fakegh/main.go`, `internal/forge/gh_test.go`;
create `internal/forge/query.go`, `internal/forge/query_test.go`,
`internal/forge/testdata/pr-search.json`; modify the two provider fakes
`internal/domain/forge_test.go`, `internal/web/prs_test.go` (compile).

**Produces:**

```go
// query.go
type PRQuery struct{ State, Text string; Limit int }
const (StateAll="all"; StateOpen="open"; StateClosed="closed"; StateMerged="merged"
       DefaultSearchLimit=50; MaxSearchLimit=200)
func (q PRQuery) Normalize() (PRQuery, error) // trims Text, ""→all, 0→50; bad state / limit → error
func PRStates() []string                        // all, closed, merged, open — the cycle order
// Provider
Search(ctx context.Context, q PRQuery) (prs []model.PullRequest, more bool, err error)
```

- [ ] Test `TestPRQueryNormalize`: `{}`→`{all,"",50}`; `" x "` trimmed;
  `State:"MERGED"` → error (exact lowercase only); limit `-1`, `201` → error;
  `200` ok.
- [ ] Test `TestGHSearchArgv` (FakeRunner handler name `"gh pr search"`):
  - `{closed,"fix login",50}` → argv has `pr list --state closed --limit 51
    --search "fix login sort:updated-desc" --json <prFields>`;
  - empty text → `--search sort:updated-desc`;
  - text `-label:bug sort:created-asc` → passed verbatim (own `sort:` wins),
    and it is the element right after `--search`;
  - no mutating token (reuse the list from `TestGHArgvIsReadOnly`);
  - handler answers 3 rows with Limit 2 → 2 rows, `more=true`; 2 rows → false.
- [ ] Run → FAIL (undefined). Implement:

```go
func (g *GH) Search(ctx context.Context, q PRQuery) ([]model.PullRequest, bool, error) {
	text := q.Text
	if !hasSortQualifier(text) {
		text = strings.TrimSpace(text + " sort:updated-desc")
	}
	res, err := g.run(ctx, "gh pr search", "pr", "list", "--state", q.State,
		"--limit", strconv.Itoa(q.Limit+1), "--search", text, "--json", prFields)
	if err != nil { return nil, false, err }
	prs, err := parsePRList([]byte(res.Stdout))
	if err != nil { return nil, false, err }
	if len(prs) > q.Limit { return prs[:q.Limit], true, nil }
	return prs, false, nil
}
// hasSortQualifier: any whitespace-separated field with the prefix "sort:".
```

- [ ] fakegh: `pr list` whose args contain `--search` → `pr-search.json`
  (Detect and ListOpen never pass it, so they keep `pr-list.json`). Add
  `testdata/pr-search.json`: #7 open, #5 MERGED, #3 CLOSED.
- [ ] Add `Search` to both fakes. Domain fake fields:
  `search []model.PullRequest; searchMore bool; searchErr error;
  searchCalls []forge.PRQuery`. Web fake: same shape, minimal.
- [ ] `go test ./internal/forge/ && go vet ./...` → PASS. Commit
  `feat(forge): Provider.Search — a forge-neutral pull-request query`.

### Task 2: domain — `PRSearch`, `PRSearchLast`

**Files:** create `internal/domain/forge_search.go`,
`internal/domain/forge_search_test.go`; modify `internal/domain/service.go`
(or wherever `forgeTerminal` is declared) for the new fields.

**Produces:**

```go
type PRQuery = forge.PRQuery
const (PRStateFilterAll = forge.StateAll; … Open, Closed, Merged)
func PRStateFilters() []string { return forge.PRStates() }
const PRSearchDefaultLimit, PRSearchMaxLimit = forge.DefaultSearchLimit, forge.MaxSearchLimit
type PRSearchResult struct{ Query PRQuery; PRs []model.PullRequest; More bool; At time.Time }
func (s *Service) PRSearch(ctx context.Context, q PRQuery) (PRSearchResult, error)
func (s *Service) PRSearchLast() (PRSearchResult, bool)
func NormalizePRQuery(q PRQuery) (PRQuery, error)
```

(Check the existing names first: `model.PRState*` constants exist — the new
ones are named `PRStateFilter*` to stay distinct.)

- [ ] Tests (fakeForge via `SetForgeProviders`):
  - `TestPRSearchAsksTheProviderNormalized` — `{Text:" x "}` → one call
    `{all,"x",50}`; result rows sorted newest-updated first; `More` carried.
  - `TestPRSearchNumberLookup` — `"#5"` and `"5"`: zero `searchCalls`, one
    `PR(5)`; unknown number → empty result, nil error; `"0"` and `"#"` are
    ordinary text.
  - `TestPRSearchDoesNotGrowTheList` — after a search, `PullRequests` has no
    row for a found closed PR (no ref, never seen).
  - `TestPRSearchSeedsTheCache` — `PRDetailsCached`/cache lookup (use the same
    probe the existing cache tests use) knows the found PR.
  - `TestPRSearchLast` — none before; set after success; a failing search
    keeps the previous; returned slice is a clone (mutating it is harmless).
  - `TestPRSearchBadQuery` — bad state → error, zero provider calls.
  - `TestPRSearchNoForge` — `ErrForgeUnavailable`.
- [ ] Implement: `provider(ctx)` → `Normalize` → number regexp
  `^#?([0-9]+)$` with n>0 → `s.PullRequest(ctx, n)` (ErrNotFound → empty) →
  else `flight.Do("forge-search:"+state+"\x00"+limit+"\x00"+text)` →
  `p.Search`; under `forgeMu`: `putPRLocked(pr,false)` for each row (NOT for
  the number path — `PullRequest` already stored it with its body), store
  `forgeSearchLast`. Sort with the comparator from `pullRequests` (extract it
  to `prByUpdated`). Drop `forgeSearchLast` wherever the forge state is reset
  (grep `forgeTerminal = ` / the re-root reset).
- [ ] `go test ./internal/domain/ -run 'PRSearch|PullRequests'` → PASS. Commit
  `feat(domain): PRSearch — number lookup, session memory, no list growth`.

### Task 3: CLI — `gg pr list --state/--search/--limit`

**Files:** modify `internal/cli/pr.go`, `internal/cli/pr_test.go`; create
`e2e/scenarios/pr_search.toml`.

- [ ] Tests: `--state closed` lists search rows; `--search "-label:bug"`
  (value starting with `-`) accepted; `--search=x`, `--limit=5` forms;
  `--state bogus` and `--limit 0|201|x` → exit 2 + usage; a value flag with
  no value → exit 2; search flags with `view` → exit 2; `More` →
  stderr has `more results — narrow the search`, stdout does not; `--json`
  is a bare array; plain `gg pr list` unchanged (no `Search` call).
- [ ] Implement the arg loop: value flags `--state|--search|--limit` take the
  NEXT arg unconditionally (or the `=` form); set `searching=true`. `list` +
  searching → `svc.PRSearch`. Usage line:
  `usage: gg pr list [--state all|open|closed|merged] [--search <text>] [--limit <n>] [--json]`.
- [ ] e2e `pr_search.toml`: seed `pr-list.json` (open #7) + `pr-search.json`
  (#7 open, #5 merged, #3 closed) + `pr-view-5.json`; runs:
  `pr list` excludes `#5`; `pr list --state all` contains `#5`, `merged`,
  `#3`, `closed`; `pr list --search "#5"` contains `#5` only
  (`stdout_excludes = ["#3"]`); `pr list --state nope` exit 2.
- [ ] `go test ./internal/cli/ -run PR && go test ./e2e/ -run 'TestScenarios/pr_'`
  → PASS. Commit `feat(cli): gg pr list --state/--search/--limit`.

### Task 4: TUI — the search popup

**Files:** create `internal/tui/pr_search_popup.go`,
`internal/tui/pr_search_popup_test.go`; modify `internal/tui/pr_panel.go`
(`prRows` → `prRowsFor`), `internal/tui/model.go` (`case "A"`, the
`prSearchMsg` case), `internal/tui/preview_open.go` (hand-off).

**Produces:**

```go
type prSearchPopup struct {
	popupMax
	input   textfield
	state   string            // a domain.PRStateFilters() value
	asked   domain.PRQuery    // the query in flight / last answered
	busy    bool
	err     string
	res     domain.PRSearchResult
	has     bool              // res holds an answer
	inRows  bool              // focus zone
	sel     int
	mode    dispMode; hscroll int
}
type prSearchMsg struct{ q domain.PRQuery; res domain.PRSearchResult; err error }
func (m Model) canSearchPRs() bool      // focus==panelPRs && forgeShown && opsIdle && svc!=nil
func (m Model) openPRSearch() (Model, tea.Cmd)
func (m Model) handlePRSearchMsg(msg prSearchMsg) (Model, tea.Cmd)
func prRowsFor(prs []model.PullRequest) []string
```

Behaviour (spec §3.4):

- open: seed from `svc.PRSearchLast()` (text, state, rows) — no forge call.
- prompt zone: `tab` cycles state; `enter` → normalize
  `{state,text,PRSearchDefaultLimit}`, `busy=true`, `asked=q`, cmd →
  `prSearchMsg`; `↓` → rows when `len(res.PRs)>0`; `esc` → `popLayer`;
  otherwise `input.HandleEditKey`.
- rows zone: `↑/k` (at 0 → prompt), `↓/j`, `pgup/pgdn/home/end`, `z` mode,
  `shift+←/→` pan; `enter` → `m.openPRCmd(pr)` when `m.opsIdle()`; `i` →
  `m.openPRHub(pr)`; `y` → copy URL (same messages as `copyPRURL`); `esc` →
  prompt. While `m.running` every key but `esc` is swallowed.
- `handlePRSearchMsg`: `p := layerOf[*prSearchPopup](m)`; drop when nil or
  `msg.q != p.asked`; error → `p.err = firstLine`, rows kept; success →
  `res/has`, `sel=0`, `inRows = len(PRs)>0`.
- render: `popupBox`; line 1 = `[state]  ` + `viewField`; a rule; then
  `searching…` / error / `no pull requests match` / rows through
  `renderWindow` (selected row styled only in the rows zone, non-open rows
  dim) + the `more results — narrow the search` line; footer hint per zone.
- hand-off: in `handlePreviewOpenMsg`, replace the bare
  `m, cmd = m.openCompareFiles(...)` with

```go
open := func(m Model) (Model, tea.Cmd) { return m.openCompareFiles(msg.eps.Left, msg.eps.Right) }
if layerOf[*prSearchPopup](m) != nil { // only the search popup parks: a moved-tip re-open under a hub must not
	m, cmd = m.handOffToFilesView(open)
} else {
	m, cmd = open(m)
}
```

- [ ] Tests (`loadedModel`, messages injected, no forge):
  open needs the PRs tab + forge (`A` elsewhere does nothing / not stash);
  typing + `tab` cycle order all→closed→merged→open→all; `enter` emits a cmd
  and sets busy; a stale `prSearchMsg` (other query) is dropped; success
  moves to rows; `↑` at row 0 and `esc` return to the prompt, second `esc`
  closes; key-swallow (`p`, `S`, `q` in the prompt become text / nothing
  global happens); `i` pushes `*prHubPopup` and `esc` reveals the search
  popup again; `enter` on a row returns a cmd and leaves the popup on the
  stack; a `previewOpenMsg` for that PR parks the popup
  (`filesReturnLayers` has it, stack empty) and the files view's `esc`
  restores it; the same msg WITHOUT the popup parks nothing; error keeps old
  rows; `more` line shows; fit test at 60×16 and 40×10 (every line ≤ width);
  `prRowsFor` parity: `m.prRows()` equals `prRowsFor(m.prs)`.
- [ ] Run → FAIL, implement, run `go test ./internal/tui/ -run 'PRSearch|PRRows|PRPanel|PROpen'`
  → PASS. Commit `feat(tui): pull-request search popup (A in the PRs tab)`.

### Task 5: registration, i18n, help

**Files:** `internal/tui/footer.go` (binding `pr-search`, key `A`, label
`[A] search`, predicate `canSearchPRs`, scope matching its neighbours — pick
the panel scope, not `scopeRow`: it works on an empty list),
`internal/tui/action_menu.go` (`"pr-search"` → `Search pull requests…`),
`internal/tui/command_palette.go` (entry + availability),
`internal/tui/help.go` (row `A` in the Pull requests block),
`internal/i18n/lang/{ja,ko,ru,zh}.toml`.

- [ ] Run the gates first to collect every missing key:
  `go test ./internal/tui/ -run 'I18n|Vocab|MenuLabels|HelpFooter|Palette|Footer'`.
- [ ] Add translations (use the `adding-translations` skill), fix pins
  (footer-width tests are exact strings — check `TestFooter*`).
- [ ] Full `go test ./internal/tui/ ./internal/i18n/` → PASS. Commit
  `feat(tui): advertise PR search — footer, menu, palette, help, translations`.

### Task 6: headless verification

- [ ] Build the worktree binary; fixture = scratchpad `prtui/` (memory
  recipe): add `pr-search.json` (open #7, merged #5 with head ≠ base and a
  REAL fetchable head, closed #3) and `pr-view-5.json`.
- [ ] `tui-capture.sh`: `C-Left` → PRs tab → `A` → type `login` → `tab` →
  `enter` → rows → `i` → `esc` → `enter` (diff opens) → `esc` (popup back) →
  `esc esc`; then the PRs tab lists #5 dimmed. Read every snapshot; widths
  80 and 50.
- [ ] Mutation check: remove the `layerOf[*prSearchPopup]` hand-off → the
  "popup back" snapshot must differ (watch the unit test fail too).

### Task 7: docs

- [ ] `CHANGELOG.md`, `README.md` (key `A`, the new flags),
  `internal/agentskill/using-gg.md` + bump `agentskill.Version`,
  `docs/CLAUDE-details.md` (forge section: Search seam, sort qualifier,
  number lookup, no list growth, the hand-off gate), spec §3.1 invocation line
  (`--search` is always present). Commit `docs: pull-request search`.

### Task 8: gates

- [ ] `./test.sh` then, from a CLEAN tree, `./test.sh race`.
- [ ] Stripped, version-stamped verify binary → SendUserFile + path.
- [ ] Ask before merging; after the merge `./build.sh install`
  (+ `gg init --update` note for the user: the skill version moved).

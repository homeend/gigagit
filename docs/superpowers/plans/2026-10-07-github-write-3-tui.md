# GitHub write-back — Plan 3: TUI

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
> **gigagit rule (CLAUDE.md): NO implementer subagents** — this plan is executed by the session that wrote it (executing-plans); only the final whole-branch review may be a read-only subagent.
> Every commit message below ends with the session's attribution trailer lines (`Co-Authored-By: …` / `Claude-Session: …`); the steps show the subject only. `gg commit` has no `-F`: stage with `gg add <paths>`, commit with `git commit -F <msgfile>`.

**Goal:** In gg's terminal UI, a pull request's notes show where they live (local / sending / failed / on GitHub) and which group they belong to (a colour bar), and the user sends them — one note, a whole group, a verdict, a reply — resolves threads, approves an agent's queued send, and finishes or discards an interrupted send, all without leaving the PR view.

**Architecture:** Plan 2 built the send core: `domain.PRSendOp(req)` plans outside the repo gate and returns an `engine.SendToForge` whose one `forge.send` decision is the confirm. The TUI builds that op off the Update goroutine (a `forgeSendReadyMsg`, the `prFetchReadyMsg` shape), runs it with `startOp`, and replaces the engine's English confirm text with its own rendering of `op.Plan` (translated, height-capped). Two small domain additions make the TUI's job possible: a PR's AI reviews draw their unsent remarks in the PR's diff (so a review is a visible, coloured group with a "Send review" row), and frontend-facing helpers (send groups, an editable review body, reason constants, the pending-send path and outcome). Pending agent sends and interrupted sends become notice SOURCES re-derived in `rebuildNotices`, like drift findings.

**Tech Stack:** Go 1.26, Bubble Tea, lipgloss, `internal/filewatch`, the fake gh (`internal/forge/testdata/fakegh`, `forgetest`), the e2e harness (`e2e/`).

**Spec:** `docs/superpowers/specs/2026-10-07-github-write-design.md` — §1.1 (marks), §1.3 (groups and colour), §1.4 (carried notes), §2.4 (freshness), §3.3–§3.5 (op, confirm, gestures), §3.7 (pending-send queue), §4.1 (TUI), §5 (errors), §6 (testing). Plan 1 (`49e37a6e`), plan 2 (`14dccb1f`) and the plan-2 follow-ups (`56538a2d`) are on `main`; plan 2's code layout and rulings R1–R12 are in `docs/superpowers/plans/2026-10-07-github-write-2-send-core-cli.md`.

## Global Constraints

- Nothing in this plan (code or tests) posts to real GitHub. Tests use `FakeRunner`, an in-memory forge, or the fake gh with `GG_GH_BIN` pointed at it. The empty-body COMMENT probe stays unrun.
- The repo gate is not re-entrant: `PRSendOp` (it reads under its own reservations) is ALWAYS called inside a `tea.Cmd`, never on the Update goroutine and never inside a running op; only its returned op goes to `startOp`.
- `internal/tui` never imports `internal/git`, `forge`, `notes`, `prcache` (archtest). Everything forge-shaped reaches the TUI through `domain` or `engine` types.
- Every user-visible TUI string goes through `i18n.T` with a literal key present in all four bundles (`internal/i18n/lang/{ja,ko,zh,ru}.toml`); prose lives in the format, never in an arg; two keys for singular/plural; width math with `lipgloss.Width`. Engine/CLI prose and decision option VALUES stay English.
- Every popup's key hints sit below a blank line; a popup opened from a popup returns to it on close (`pushLayer`/`popLayer`); every new key/row is advertised in help AND a footer.
- New never-ending TUI commands (the pending-sends watcher) are gated by `m.quiet` (the e2e Headless driver).
- Stored/drawn times use `internal/clock`; tests freeze them where they draw ages.
- Tests: real git in `t.TempDir()`; new tests call `t.Parallel()` unless they set env (fake gh: `t.Setenv`) — those are serial, with a comment saying why. Every TestMain that can reach the state dir already pins `XDG_STATE_HOME`/`XDG_CONFIG_HOME`; a new test package does too.
- A "watch it fail" check means the guard removed, not the feature; never `git checkout -- <file>` to undo a probe — copy a backup and restore it.
- Race gate: `./test.sh race > <log> 2>&1`; green ONLY when the log says `all green`.

## Review Focus

1. **A send whose confirm is open when the PR refresh is due** → the comment poll stands down while a modal is up (`prCommentsTick` already checks `m.modal`), so nothing re-renders under the confirm; after the op the view reloads once. (Task 6 test pins it.)
2. **An agent's queued send approved while the user's own send op is running** → `startOp` refuses a second op (`opsIdle` false); the notice stays and says so; nothing is lost from the queue. (Task 10 test.)
3. **A pending send whose PR is not the one on screen, or not fetched** → approve builds the op for the entry's PR; when its diff is not available the domain error reaches the status line and the entry stays pending (not failed). (Task 6 test.)
4. **A long review** (30 remarks, a 40-line body) → the confirm modal fits the screen: at most 12 item rows plus "+ N more", the body cut to 6 lines. (Task 6 test.)
5. **A repo switch (`R`) with notices from pending/interrupted sends** → the old repo's sources are dropped, the watcher is closed and re-armed for the new repo's file, nothing from the old repo can be approved. (Task 10 test.)
6. **Group colour stability** → the same group id always gets the same slot (no map-iteration order), across processes and themes; a forge review sent from a local group wears that group's colour (the domain maps it — plan 2's `TestASentNoteKeepsItsGroup` — and the slot is a pure function of the id). (Task 1 test.)
7. **Marks outside a PR** → a plain commit/working-tree diff draws no `○`/`●` marks and no group bars (golden screens unchanged) but still shows `◌` sending and `○!` failed. (Task 4 test.)

## Rulings this plan makes on the spec (for the user's review)

- **T1 — a PR's AI reviews draw in its diff.** Today a review's remarks render only in the review view; a PR's diff never shows them, so "Send review (its group)" and the group colours (§1.3, §4.1) would have nothing to sit on. A review belongs to PR n when its reviewed tip is one of the PR's commits or it was saved on the PR's preview link; its unsent remarks draw in the PR diff, re-anchored on the PR head by the same hash rule the send uses (a remark whose lines changed is not drawn there; it stays in the review view). Cost if wrong: the PR view shows remarks the user expected only in the review view.
- **T2 — carried notes count in the Files badges** (closes the plan-2 open item). The `}`/`{` file steps and the stacked-diff filter read those counts; a carried note left out would make the step skip a file that visibly shows a box.
- **T3 — sync marks and group bars show in a PR's diff only.** Elsewhere a stored note is always local and every one would be in "my draft review", so a mark and a bar on every box everywhere would say nothing and change every golden screen. Outside a PR only the two states that need attention show: `◌` sending and `○!` failed (with the error). Spec ruling 4 says "every note shows its sync state"; cost if wrong: one condition to drop.
- **T4 — the TUI renders the confirm itself.** The engine's confirm text carries English item labels and skip reasons as arguments; the TUI holds the op's `Plan` before running it and renders the `forge.send` prompt from it (translated, capped to the screen). The CLI keeps the engine's English text.
- **T5 — "PR popup" = the pull request details popup (`i`) and the open PR's `.` menu.** The details popup gains `[s] send review` and `[v] verdict`; the open PR's files view and diff `.` menus gain "Send review…" and "Verdict…". The Pull requests tab row gets no send rows (the diff must be fetched first; enter does that).
- **T6 — "Later" keeps a pending/interrupted notice.** An agent is waiting on it: closing the dialog must not dismiss it for the session the way health notices' "Not now" does. Approve, reject, finish and discard remove it; the queue file is the truth on the next poll.
- **T7 — an AI review's body is editable** (§3.3): a non-empty `PRSendRequest.Body` replaces the review's summary as the GitHub review body (still signed with the agent and carrying the send marker). Today `--review` ignores `--body`; the CLI gains the same behaviour.
- **T8 — interrupted sends are raised only for PRs read this session** (`PRInterrupted` needs the PR's cached details); the notice appears after that PR's first refresh, not at startup.
- **T9 — fold the eight plan-2 follow-up minors in as the last task** (Task 14), since they touch the same files. Strike Task 14 at review if you want them separate.

---

## File structure

| File | Responsibility |
|---|---|
| `internal/theme/theme.go`, `override.go` | 6 scalar roles `NoteGroup1..6` (`note_group_1..6`), Dark/Light values, docs |
| `internal/tui/styles.go` | `noteGroups [6]lipgloss.Style`, legacy literals |
| `internal/tui/note_group.go` (new) | `groupSlot(group) int` (stable fnv32a % 6 + 1), `groupBar(slot) string` |
| `internal/domain/pr_reviews.go` (new) | a PR's reviews (`prReviews`), their remarks re-anchored on the PR head (`prReviewNotes`), shared anchoring with the planner (`remarkAnchor`) |
| `internal/domain/previewnotes.go` | remarks + carried notes in `PreviewNotesFor` / `PreviewNotesAll` / `PreviewNoteCounts`; `PreviewNoteGroups` |
| `internal/domain/forge_send_front.go` (new) | `SendGroup`, `PRSendGroups`, `ReviewBodyText`, `PendingSendsPath`, `PendingOutcome`, skip reason constants |
| `internal/domain/forge_send.go` | an edited review body (T7), reason constants, `Summary`/`Path`/`Line` on items and skips |
| `internal/engine/send_forge.go` | `SendItem.Summary`, `SendSkip.{Path,Line,Summary}`, `SendPlan.BodyText()` |
| `internal/tui/diff_notes.go`, `diff_render.go` | marks, carried origin, failed row, group bar in note boxes |
| `internal/tui/files_view.go`, `pr_comments.go` | badge group bars, `prCountsMsg.groups` |
| `internal/tui/forge_send.go` (new) | `forgeSendCmd`, `forgeSendReadyMsg`, `forgeSendState`, the localized confirm, post-op follow-up |
| `internal/tui/forge_send_menu.go` (new) | the `.` rows on notes in a PR's diff; R and x on GitHub threads |
| `internal/tui/send_review_popup.go` (new) | Send review… (group pick → body) and Verdict… |
| `internal/tui/pr_revalidate.go` | the "updated" freshness state |
| `internal/tui/pending_sends.go` (new) | pending agent sends: watcher, poll, notice source, approve/reject |
| `internal/tui/interrupted_sends.go` (new) | interrupted sends: notice source, finish/discard |
| `internal/tui/source.go` | `SendToForge` → `srcNotes` |
| `internal/tui/help.go`, `footer.go`, `pr_hub.go` | advertise everything |
| `e2e/scenario.go`, `e2e/builder.go` | `ref` step, `{{rev:<name>}}` in written content |
| `e2e/scenarios/tui_pr_send.toml` (+ `.screens/`) | golden screens: marks, menu, confirm, sent |

---
### Task 1: six group-colour roles and a stable group → slot map

**Files:**
- Modify: `internal/theme/theme.go` (struct, `roles()`, `Dark`, `Light`)
- Modify: `internal/theme/override.go` (`Override` fields, `roleFields` rows)
- Modify: `internal/tui/styles.go` (`legacy` literals, `noteGroups [6]lipgloss.Style`)
- Create: `internal/tui/note_group.go`
- Test: `internal/theme/theme_test.go`, `internal/tui/note_group_test.go`

**Interfaces:**
- Produces: `theme.Theme.NoteGroup1 … NoteGroup6 string` (TOML `note_group_1 … note_group_6`); `styleSet.noteGroups [6]lipgloss.Style` (index 0 = slot 1); `func groupSlot(group string) int` (0 for `""`, else 1–6, stable); `func groupBarStyle(slot int) (lipgloss.Style, bool)`.

- [ ] **Step 1: Write the failing tests**

`internal/theme/theme_test.go` — append:

```go
func TestNoteGroupRolesAreConfigurable(t *testing.T) {
	t.Parallel()
	keys := map[string]bool{}
	for _, d := range RoleDocs() {
		keys[d.Key] = true
	}
	for i := 1; i <= 6; i++ {
		k := fmt.Sprintf("note_group_%d", i)
		if !keys[k] {
			t.Errorf("RoleDocs lacks %s", k)
		}
	}
	// Six DISTINCT colours per built-in theme that paints colours: two groups
	// sharing a slot colour would read as one group.
	for _, th := range []Theme{Dark, Light} {
		seen := map[string]bool{}
		for _, c := range []string{th.NoteGroup1, th.NoteGroup2, th.NoteGroup3, th.NoteGroup4, th.NoteGroup5, th.NoteGroup6} {
			if c == "" || seen[c] {
				t.Errorf("%s: group colour %q empty or repeated", th.Name, c)
			}
			seen[c] = true
		}
	}
}
```

(add `"fmt"` to the imports if missing).

`internal/tui/note_group_test.go`:

```go
package tui

import "testing"

func TestGroupSlotIsStableAndInRange(t *testing.T) {
	t.Parallel()
	if groupSlot("") != 0 {
		t.Fatal("no group, no slot")
	}
	ids := []string{"mine", "review:r-1", "review:r-2", "github:PRR_9", "review:abc", "github:x"}
	for _, id := range ids {
		s := groupSlot(id)
		if s < 1 || s > 6 {
			t.Fatalf("groupSlot(%q) = %d, want 1..6", id, s)
		}
		for i := 0; i < 50; i++ { // no map order, no randomness
			if groupSlot(id) != s {
				t.Fatalf("groupSlot(%q) is not stable", id)
			}
		}
	}
	// Pinned values: a change of hash would recolour every user's groups.
	if got := groupSlot("mine"); got != groupSlotMine {
		t.Fatalf("groupSlot(mine) = %d, want %d", got, groupSlotMine)
	}
}

func TestGroupBarStyleUsesTheSlotRole(t *testing.T) {
	t.Parallel()
	for slot := 1; slot <= 6; slot++ {
		if _, ok := groupBarStyle(slot); !ok {
			t.Fatalf("slot %d has no style", slot)
		}
	}
	if _, ok := groupBarStyle(0); ok {
		t.Fatal("slot 0 is no group: no bar")
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/theme/ ./internal/tui/ -run 'NoteGroupRoles|GroupSlot|GroupBarStyle|RoleFieldsCoverEveryRole' 2>&1 | tail -20`
Expected: FAIL — `undefined: NoteGroup1` / `undefined: groupSlot`.

- [ ] **Step 3: Implement**

`internal/theme/theme.go`: in `Theme`, after `PickerLeft, PickerRight string` add

```go
	// Note groups (spec 2026-10-07 §1.3): the colour bar a note box wears in a
	// pull request's view — one per review group, picked stably from the
	// group id (the TUI's groupSlot), so a review keeps its colour on GitHub.
	NoteGroup1, NoteGroup2, NoteGroup3, NoteGroup4, NoteGroup5, NoteGroup6 string
```

In `roles()` append `t.NoteGroup1, t.NoteGroup2, t.NoteGroup3, t.NoteGroup4, t.NoteGroup5, t.NoteGroup6,` after `t.PickerLeft, t.PickerRight,`. In `Dark` add
`NoteGroup1: "#E06C75", NoteGroup2: "#E5C07B", NoteGroup3: "#98C379", NoteGroup4: "#56B6C2", NoteGroup5: "#C678DD", NoteGroup6: "#D19A66",`
and in `Light`
`NoteGroup1: "#B23A48", NoteGroup2: "#A07400", NoteGroup3: "#3E7D2E", NoteGroup4: "#1F7A8C", NoteGroup5: "#7D3C98", NoteGroup6: "#B35C1E",`.

`internal/theme/override.go`: in `Override` after `PickerRight`:

```go
	NoteGroup1 string `toml:"note_group_1"`
	NoteGroup2 string `toml:"note_group_2"`
	NoteGroup3 string `toml:"note_group_3"`
	NoteGroup4 string `toml:"note_group_4"`
	NoteGroup5 string `toml:"note_group_5"`
	NoteGroup6 string `toml:"note_group_6"`
```

and in `roleFields` after the `picker_right` row:

```go
	{"note_group_1", "note group colour bar 1 (a review's notes in a pull request view)", func(t *Theme) *string { return &t.NoteGroup1 }, func(o *Override) *string { return &o.NoteGroup1 }},
	{"note_group_2", "note group colour bar 2", func(t *Theme) *string { return &t.NoteGroup2 }, func(o *Override) *string { return &o.NoteGroup2 }},
	{"note_group_3", "note group colour bar 3", func(t *Theme) *string { return &t.NoteGroup3 }, func(o *Override) *string { return &o.NoteGroup3 }},
	{"note_group_4", "note group colour bar 4", func(t *Theme) *string { return &t.NoteGroup4 }, func(o *Override) *string { return &o.NoteGroup4 }},
	{"note_group_5", "note group colour bar 5", func(t *Theme) *string { return &t.NoteGroup5 }, func(o *Override) *string { return &o.NoteGroup5 }},
	{"note_group_6", "note group colour bar 6", func(t *Theme) *string { return &t.NoteGroup6 }, func(o *Override) *string { return &o.NoteGroup6 }},
```

`internal/tui/styles.go`: in `legacy` add
`NoteGroup1: "167", NoteGroup2: "179", NoteGroup3: "107", NoteGroup4: "73", NoteGroup5: "140", NoteGroup6: "173",`;
in `styleSet` add `noteGroups [6]lipgloss.Style // note group bars, slot 1..6 at 0..5`; after the `noteFrameStale` line in the builder:

```go
	for i, c := range [6][2]string{
		{th.NoteGroup1, legacy.NoteGroup1}, {th.NoteGroup2, legacy.NoteGroup2}, {th.NoteGroup3, legacy.NoteGroup3},
		{th.NoteGroup4, legacy.NoteGroup4}, {th.NoteGroup5, legacy.NoteGroup5}, {th.NoteGroup6, legacy.NoteGroup6},
	} {
		s.noteGroups[i] = ns().Foreground(pick(c[0], c[1]))
	}
```

`internal/tui/note_group.go`:

```go
package tui

import (
	"hash/fnv"

	"github.com/charmbracelet/lipgloss"
)

// groupSlot is a note group's colour slot (spec 2026-10-07 §1.3): 0 for no
// group, else 1–6 from the group id's FNV-1a hash — stable across runs and
// processes, so a review and the GitHub review it became share a colour.
func groupSlot(group string) int {
	if group == "" {
		return 0
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(group))
	return int(h.Sum32()%6) + 1
}

// groupSlotMine pins "my draft review"'s slot: a hash change would recolour
// every user's groups, and the test catches it.
const groupSlotMine = 4

// groupBarStyle is the style a slot's bar is painted with (false for 0).
func groupBarStyle(slot int) (lipgloss.Style, bool) {
	if slot < 1 || slot > 6 {
		return lipgloss.Style{}, false
	}
	return st().noteGroups[slot-1], true
}
```

Run the test once; if `groupSlot("mine")` is not 4, set `groupSlotMine` to the value it prints (the constant pins whatever FNV-1a gives; it is not a design choice).

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/theme/ ./internal/tui/ -run 'NoteGroupRoles|GroupSlot|GroupBarStyle|RoleFields|RoleDocs|Roles|BuiltinsComplete|Theme' 2>&1 | tail -20`
Expected: PASS. Then `go test ./internal/config/ 2>&1 | tail -5` (the theme template docs) — PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/theme internal/tui/styles.go internal/tui/note_group.go internal/tui/note_group_test.go
git commit -F <msgfile>   # "feat(theme): six note-group colour roles and a stable group slot"
```

---

### Task 2: domain — a PR's AI reviews draw in its diff; carried notes and remarks count; groups per path

**Files:**
- Create: `internal/domain/pr_reviews.go`
- Modify: `internal/domain/forge_send.go` (`remarkItem` uses the shared `remarkPlace`)
- Modify: `internal/domain/previewnotes.go` (`PreviewNotesFor`, `PreviewNotesAt`, `PreviewNotesAll`, `PreviewNoteCounts`, new `PreviewNoteGroups`)
- Modify: `internal/domain/service.go` (`prReviewCache` beside `carriedCache`), `internal/domain/notes.go` (drop it where `carriedCache` is dropped, line ~739)
- Test: `internal/domain/pr_reviews_test.go`

**Interfaces:**
- Consumes: `sendRepo(t)` (domain test helper: PR #7, big.go lines 5 and 25 changed, head sha), `addPRNote`, `SaveReview`, `PRPreview`.
- Produces: `func (s *Service) PreviewNoteGroups(ctx context.Context, set PreviewNoteSet) (map[string][]string, error)` — path → distinct group ids in line order; remark roots `ResolvedNote{Note.ID: "review:<rid>:<i>", Group: "review:<rid>"}` in `PreviewNotesFor/At/All` of a PR set; `PreviewNoteCounts` of a PR set includes carried notes and drawn remarks.

- [ ] **Step 1: Write the failing tests**

`internal/domain/pr_reviews_test.go`:

```go
package domain

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// prSet opens PR #7 of sendRepo and returns its note set.
func prSet(t *testing.T, svc *Service) PreviewNoteSet {
	t.Helper()
	pr, err := svc.PullRequest(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	prev, err := svc.PRPreview(context.Background(), pr)
	if err != nil || !prev.Set.OK() {
		t.Fatalf("PR preview: %+v %v", prev.Endpoints, err)
	}
	return prev.Set
}

const twoRemarks = `{"version":1,"summary":"looks fine","files":[{"path":"big.go","annotations":[
 {"newRange":[5,5],"summary":"check this"},{"newRange":[25,25],"summary":"and this"}]}]}`

// repoDir is sendRepo's checkout (revParse needs it).
func repoDir(t *testing.T, svc *Service) string {
	t.Helper()
	dir, err := svc.TopLevel(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func saveHeadReview(t *testing.T, svc *Service, head, doc string) string {
	t.Helper()
	rid, _, err := svc.SaveReview(context.Background(), SaveReview{
		Target: ReviewTarget{Kind: ReviewRange, Range: head + "^.." + head, Label: "feat"}, Agent: "claude", Text: doc})
	if err != nil {
		t.Fatal(err)
	}
	return rid
}

// T1: a review of one of the PR's commits draws its remarks in the PR diff,
// as review:<id>:<n> roots of the review's group.
func TestPRDiffShowsItsReviewsRemarks(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	rid := saveHeadReview(t, svc, head, twoRemarks)
	got, err := svc.PreviewNotesAt(context.Background(), prSet(t, svc), "big.go")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, r := range got {
		if r.Group != "review:"+rid {
			continue
		}
		ids = append(ids, r.Note.ID)
		if r.Sync != model.SyncLocal || r.Note.Source != model.NoteSourceAgent || r.Note.Author != "claude" {
			t.Errorf("remark root %+v", r)
		}
	}
	want := []string{model.ReviewNoteIDPrefix + rid + ":0", model.ReviewNoteIDPrefix + rid + ":1"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("remark ids = %v, want %v", ids, want)
	}
}

// A review of a commit that is not the PR's (main's own history) stays out.
func TestPRDiffLeavesOutAReviewOfAnotherCommit(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	base := revParse(t, repoDir(t, svc), head+"~1")
	saveHeadReview(t, svc, base, `{"version":1,"summary":"old","files":[{"path":"other.go","annotations":[{"newRange":[1,1],"summary":"x"}]}]}`)
	got, _ := svc.PreviewNotesAt(context.Background(), prSet(t, svc), "big.go")
	for _, r := range got {
		if strings.HasPrefix(r.Group, "review:") {
			t.Fatalf("a review of another commit drew in the PR: %+v", r)
		}
	}
}

// T2: the Files badges count carried notes and drawn remarks too.
func TestPRCountsIncludeCarriedNotesAndRemarks(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	ctx := context.Background()
	addPRNote(t, svc, head, "big.go", 5, "on the PR")
	mainTip := revParse(t, repoDir(t, svc), "main")
	addPRNote(t, svc, mainTip, "big.go", 10, "carried: line 10 is the same in the PR")
	saveHeadReview(t, svc, head, twoRemarks)
	counts, _, err := svc.PreviewNoteCounts(ctx, prSet(t, svc))
	if err != nil {
		t.Fatal(err)
	}
	if counts["big.go"] != 4 {
		t.Fatalf("big.go counts %d, want 4 (1 note + 1 carried + 2 remarks)", counts["big.go"])
	}
}

// Groups per path, in line order, each once: the Files badge's colour bars.
func TestPreviewNoteGroupsByPath(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	addPRNote(t, svc, head, "big.go", 5, "mine")
	rid := saveHeadReview(t, svc, head, twoRemarks)
	groups, err := svc.PreviewNoteGroups(context.Background(), prSet(t, svc))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{GroupMine, "review:" + rid}; !reflect.DeepEqual(groups["big.go"], want) {
		t.Fatalf("groups = %v, want %v", groups["big.go"], want)
	}
}

// The carried and remark slices are CACHED instances: the per-path answer
// is built in a fresh slice, so two reads of one path agree and the second
// carries no element the first appended.
func TestPreviewNotesAtLeavesTheCachesAlone(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	mainTip := revParse(t, repoDir(t, svc), "main")
	addPRNote(t, svc, mainTip, "big.go", 10, "carried")
	saveHeadReview(t, svc, head, twoRemarks)
	set := prSet(t, svc)
	ctx := context.Background()
	before := len(svc.carriedNotes(ctx, set)["big.go"]) + len(svc.prReviewNotes(ctx, set)["big.go"])
	a, _ := svc.PreviewNotesAt(ctx, set, "big.go")
	b, _ := svc.PreviewNotesAt(ctx, set, "big.go")
	after := len(svc.carriedNotes(ctx, set)["big.go"]) + len(svc.prReviewNotes(ctx, set)["big.go"])
	if before != 3 || after != before || len(a) != len(b) {
		t.Fatalf("caches %d → %d, reads %d / %d", before, after, len(a), len(b))
	}
}
```


- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/domain/ -run 'PRDiffShows|PRDiffLeavesOut|PRCountsInclude|PreviewNoteGroups|LeavesTheCachesAlone' 2>&1 | tail -20`
Expected: FAIL — no remark roots, counts 2, `undefined: PreviewNoteGroups`.

- [ ] **Step 3: Implement**

`internal/domain/forge_send.go` — extract the anchoring out of `remarkItem` (behaviour unchanged; `remarkItem` calls it and maps `!ok` to `skip("its lines changed")`):

```go
// remarkPlace re-finds a remark's lines in target (§3.2): the text is read
// where the review read it (the merge base for the old side, the review's
// worktree or tip for the new) and found by its hash. ok false = its lines
// changed. Shared by the send planner and the PR view (T1).
func (s *Service) remarkPlace(ctx context.Context, r Review, path string, side model.NoteSide, rng [2]int, target []string) ([2]int, []string, bool) {
	base, tip, _ := s.reviewRevs(ctx, r)
	var src []string
	switch {
	case side == model.NoteSideOld:
		src = s.revLines(ctx, base, path)
	case r.Kind == ReviewOnWorktree:
		src = s.worktreeLines(ctx, r.Worktree, path)
	default:
		src = s.revLines(ctx, tip, path)
	}
	span := rng[1] - rng[0] + 1
	want := anchorLines(src, rng)
	if span < 1 || len(want) != span {
		return [2]int{}, nil, false
	}
	start := findAnchor(target, model.NoteContextHash(want), span, rng[0])
	if start == 0 {
		return [2]int{}, nil, false
	}
	return [2]int{start, start + span - 1}, want, true
}
```

`internal/domain/pr_reviews.go`:

```go
package domain

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// prReviews are the reviews that belong to a PR's view (plan 3, T1): one of
// the PR's commits is the reviewed tip, or the review was saved on the PR's
// preview link (its scope ends in "...refs/gg/pr/<n>").
func (s *Service) prReviews(ctx context.Context, set PreviewNoteSet) []Review {
	if _, ok := git.ParsePRRef(set.Source); !ok || !set.OK() {
		return nil
	}
	c, err := s.NoteCounts(ctx)
	if err != nil {
		return nil
	}
	in := set.commitSet()
	var out []Review
	for _, h := range c.Reviews {
		if !in[h.Commit] && !strings.HasSuffix(h.Preview, "..."+set.Source) {
			continue
		}
		if r, err := s.Review(ctx, h.ID); err == nil && r.Doc != nil {
			out = append(out, r)
		}
	}
	return out
}

// prReviewNotes is every unsent remark of the PR's reviews, by path, placed
// on the PR (new side: its head; old side: the merge base). A moved remark
// lives on GitHub; one whose lines changed stays in the review view only.
// Cached per tip:base:notes generation (the carriedNotes rule): READ-ONLY.
func (s *Service) prReviewNotes(ctx context.Context, set PreviewNoteSet) map[string][]ResolvedNote {
	if _, ok := git.ParsePRRef(set.Source); !ok || !set.OK() {
		return nil
	}
	s.mu.Lock()
	key := set.Tip + ":" + set.Base + ":" + strconv.FormatUint(s.notesGen, 10)
	if c, ok := s.prReviewCache[key]; ok {
		s.mu.Unlock()
		return c
	}
	gen := s.notesGen
	s.mu.Unlock()
	lines := map[string][]string{} // "<rev>:<path>" → lines, read once
	read := func(rev, path string) []string {
		k := rev + ":" + path
		if l, ok := lines[k]; ok {
			return l
		}
		var l []string
		if b, err := s.ShowFile(ctx, rev, path); err == nil {
			l = splitLines(b)
		}
		lines[k] = l
		return l
	}
	out := map[string][]ResolvedNote{}
	for _, r := range s.prReviews(ctx, set) {
		threads, _ := r.RemarkThreads()
		i := -1
		for _, f := range r.Doc.Files {
			for _, dn := range f.Notes {
				i++
				path, side := reviewPath(f.Path), reviewSide(dn.Side)
				fp := remarkFP(f.Path, side, dn.Range, dn.Summary)
				if r.remarkMoved(fp) {
					continue
				}
				target := read(set.Tip, path)
				if side == model.NoteSideOld {
					target = read(set.Base, path)
				}
				rng, _, ok := s.remarkPlace(ctx, r, path, side, dn.Range, target)
				if !ok {
					continue
				}
				n := model.Note{ID: fmt.Sprintf("%s%s:%d", model.ReviewNoteIDPrefix, r.ID, i), Source: model.NoteSourceAgent,
					Author: r.Agent, Address: model.FileAddress{State: model.StateCommitted, Commit: set.Tip, Path: path},
					Side: side, Range: rng, Summary: dn.Summary, Rationale: dn.Rationale, Created: r.Created, Updated: r.Updated}
				for _, kv := range dn.Meta {
					n.Tags = append(n.Tags, kv.Key+": "+kv.Value)
				}
				sd := r.remarkSend(fp)
				rn := ResolvedNote{Note: n, Status: model.NoteActive, Range: rng, Group: "review:" + r.ID, Sync: sd.State()}
				if sd != nil {
					rn.SendErr = sd.Err
				}
				if i < len(threads) {
					rn.Resolution = threads[i].Resolution
					for _, rep := range threads[i].Replies {
						rep.Address, rep.Side, rep.Range = n.Address, side, rng
						rs, re := syncOf(rep)
						rn.Replies = append(rn.Replies, ResolvedNote{Note: rep, Status: model.NoteActive, Range: rng,
							Group: rn.Group, Sync: rs, SendErr: re})
					}
				}
				out[path] = append(out[path], rn)
			}
		}
	}
	s.mu.Lock()
	if s.notesGen == gen {
		if s.prReviewCache == nil {
			s.prReviewCache = map[string]map[string][]ResolvedNote{}
		}
		s.prReviewCache[key] = out
	}
	s.mu.Unlock()
	return out
}
```

`internal/domain/service.go`: beside `carriedCache` add
`prReviewCache map[string]map[string][]ResolvedNote // a PR set's drawn review remarks per tip:base:notesGen`;
`internal/domain/notes.go` (where `s.carriedCache = nil`): add `s.prReviewCache = nil // remarks read the same store`.

`internal/domain/previewnotes.go`:
- `PreviewNotesFor` and `PreviewNotesAt`: build the extras into a FRESH slice (fixes appending into the cached carried slice):

```go
	extra := append(append(append([]ResolvedNote(nil), s.carriedNotes(ctx, set)[path]...), s.prReviewNotes(ctx, set)[path]...), forge...)
	if len(mine) == 0 {
		return extra, nil
	}
	…
	return append(keepResolved(resolveNotes(mine, nil, newLines)), extra...), nil
```

(keep the `ErrNotesDisabled` early return as is: no store means no reviews either).
- `PreviewNotesAll`: after the carried loop add
  `for p, rs := range s.prReviewNotes(ctx, set) { out[p] = append(out[p], rs...) }` — `out[p]` is either a fresh slice or nil here, never the cache.
- `PreviewNoteCounts`: after the store/forge merge, for a PR set add the carried and remark roots (one fresh map; the cached one stays untouched):

```go
	extra := map[string]int{}
	for p, rs := range s.carriedNotes(ctx, set) {
		extra[p] += len(rs)
	}
	for p, rs := range s.prReviewNotes(ctx, set) {
		extra[p] += len(rs)
	}
```

  merged into the returned map and total exactly like the forge half (restructure the early return so the forge-free path merges too).
- `PreviewNoteGroups`:

```go
// PreviewNoteGroups are a preview's note groups per path, each once, in line
// order (ties: the order PreviewNotesAll gives) — the Files badge's colour
// bars (spec §1.3). A PR set's only: every other preview is all "mine".
func (s *Service) PreviewNoteGroups(ctx context.Context, set PreviewNoteSet) (map[string][]string, error) {
	if _, ok := git.ParsePRRef(set.Source); !ok || !set.OK() {
		return map[string][]string{}, nil
	}
	all, err := s.PreviewNotesAll(ctx, set)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]string, len(all))
	for p, rs := range all {
		rs = append([]ResolvedNote(nil), rs...)
		sort.SliceStable(rs, func(a, b int) bool { return rs[a].Range[0] < rs[b].Range[0] })
		seen := map[string]bool{}
		for _, r := range rs {
			if r.Group != "" && !seen[r.Group] {
				seen[r.Group] = true
				out[p] = append(out[p], r.Group)
			}
		}
	}
	return out, nil
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/domain/ -run 'PRDiff|PRCounts|PreviewNote|PlanSend|PRSendOp|Carried|Remark|Review' 2>&1 | tail -20`
Expected: PASS (the planner tests prove `remarkPlace` kept `remarkItem`'s behaviour). Then `go test ./internal/domain/ ./internal/cli/ 2>&1 | tail -5` — PASS (`gg pr notes` now also lists remark roots: if a CLI test pins its exact rows, update the expectation and ledger it).

- [ ] **Step 5: Commit**

```bash
gg add internal/domain
git commit -F <msgfile>   # "feat(domain): a PR's AI reviews draw in its diff; carried notes and remarks count; groups per path"
```

---

### Task 3: domain + engine — what a frontend needs to send

**Files:**
- Modify: `internal/engine/send_forge.go` (`SendItem.Summary`, `SendSkip.Path/Line/Summary`, `SendPlan.BodyText`)
- Modify: `internal/domain/forge_send.go` (reason constants at every skip; summaries; T7 edited review body)
- Create: `internal/domain/forge_send_front.go`
- Modify: `internal/cli/prpending.go` (`approvePending` uses `domain.PendingOutcome`)
- Test: `internal/domain/forge_send_front_test.go`, `internal/engine/send_forge_test.go`

**Interfaces:**
- Consumes: Task 2's remark roots (a review group's count).
- Produces:
  - `engine.SendItem.Summary string` (the note's summary, ≤60 runes), `engine.SendSkip{Label, Reason, Path string; Line int; Summary string}`, `func (p SendPlan) BodyText() string` (the body without the send marker).
  - `domain.SkipNotInPR = "not in this PR"`, `SkipLinesChanged = "its lines changed"`, `SkipBeingSent = "already being sent"`, `SkipOnGitHub = "already on GitHub"`, `SkipGone = "it no longer exists"`, `SkipThreadNotInPR = "its thread is not in this PR"`; `func SendSkipReasons() []string`.
  - `type SendGroup struct { ID, Agent, Summary string; Count int }`; `func (s *Service) PRSendGroups(ctx context.Context, n int) ([]SendGroup, error)`.
  - `func (s *Service) ReviewBodyText(ctx context.Context, id string) (string, error)`.
  - `func (s *Service) PendingSendsPath(ctx context.Context) (string, error)`.
  - `func PendingOutcome(res engine.Result, err error) (state, outcome string, waiting bool)`.

- [ ] **Step 1: Write the failing tests**

`internal/engine/send_forge_test.go` — append:

```go
func TestSendPlanBodyTextDropsTheMarker(t *testing.T) {
	t.Parallel()
	p := SendPlan{Body: "fine\n\n" + forge.SendMarker("n1")}
	if got := p.BodyText(); got != "fine" {
		t.Fatalf("BodyText = %q", got)
	}
}
```


`internal/domain/forge_send_front_test.go`:

```go
package domain

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

// The TUI renders the confirm from these fields (T4): reason codes are the
// exported constants, and every item and skip names its note's summary.
func TestPlanSendCarriesReasonCodesAndSummaries(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	ctx := context.Background()
	stale := addPRNote(t, svc, head, "big.go", 5, "about the old text")
	fresh := addPRNote(t, svc, head, "big.go", 25, "still here")
	if err := svc.notesStore(ctx).Edit(stale, func(n *model.Note) error { n.ContextHash = "gone"; return nil }); err != nil {
		t.Fatal(err)
	}
	svc.invalidateNoteCounts()
	p, err := svc.planSend(ctx, PRSendRequest{PR: 7, Notes: []string{stale, fresh}})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 1 || p.Items[0].Summary != "still here" {
		t.Fatalf("items = %+v", p.Items)
	}
	sk := p.Skipped
	if len(sk) != 1 || sk[0].Reason != SkipLinesChanged || sk[0].Path != "big.go" || sk[0].Line != 5 || sk[0].Summary != "about the old text" {
		t.Fatalf("skipped = %+v", sk)
	}
	for _, r := range SendSkipReasons() {
		if r == "" {
			t.Fatal("an empty reason code")
		}
	}
}

// T7: an edited body replaces the AI review's summary on GitHub; it stays
// signed by the agent and carries the send marker.
func TestPlanSendReviewHonoursAnEditedBody(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	rid := saveHeadReview(t, svc, head, twoRemarks)
	p, err := svc.planSend(context.Background(), PRSendRequest{PR: 7, Review: rid, Body: "Edited: two things to fix."})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.Body, "Edited: two things to fix.") || strings.Contains(p.Body, "looks fine") ||
		!strings.Contains(p.Body, "— claude via gg") || p.BodyText() == p.Body {
		t.Fatalf("body = %q", p.Body)
	}
}

func TestPRSendGroupsListsMineThenReviews(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	addPRNote(t, svc, head, "big.go", 5, "mine one")
	addPRNote(t, svc, head, "big.go", 25, "mine two")
	rid := saveHeadReview(t, svc, head, twoRemarks)
	gs, err := svc.PRSendGroups(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(gs) != 2 || gs[0].ID != GroupMine || gs[0].Count != 2 ||
		gs[1].ID != "review:"+rid || gs[1].Count != 2 || gs[1].Agent != "claude" || gs[1].Summary == "" {
		t.Fatalf("groups = %+v", gs)
	}
	body, err := svc.ReviewBodyText(context.Background(), rid)
	if err != nil || !strings.Contains(body, "looks fine") || strings.Contains(body, "via gg") {
		t.Fatalf("ReviewBodyText = %q, %v", body, err)
	}
}

func TestPendingOutcomeMirrorsTheApprovalRules(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		res     engine.Result
		err     error
		state   string
		waiting bool
	}{
		{engine.Result{Summary: "sent 1 comments to o/r #7"}, nil, PendingSent, false},
		{engine.Result{Summary: "aborted: sending to o/r #7"}, nil, PendingRejected, false},
		{engine.Result{}, errors.New("HTTP 502"), PendingFailed, false},
		{engine.Result{}, engine.ErrDecisionRequired, PendingWaiting, true},
	} {
		st, _, waiting := PendingOutcome(c.res, c.err)
		if st != c.state || waiting != c.waiting {
			t.Errorf("%+v %v → %q waiting=%v", c.res, c.err, st, waiting)
		}
	}
}

func TestPendingSendsPathIsTheQueueFile(t *testing.T) {
	t.Parallel()
	_, svc := newRealRepo(t)
	ctx := context.Background()
	p, err := svc.PendingSendsPath(ctx)
	if err != nil || p == "" {
		t.Fatalf("path %q, %v", p, err)
	}
	if _, err := svc.PendingSendAdd(ctx, PRSendRequest{PR: 7, Mine: true}, "claude"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("the queue file is not at %s: %v", p, err)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/engine/ ./internal/domain/ -run 'BodyTextDrops|ReasonCodesAndSummaries|EditedBody|PRSendGroups|PendingOutcome|PendingSendsPath' 2>&1 | tail -20`
Expected: FAIL — undefined `BodyText`, `SkipLinesChanged`, `PRSendGroups`, …

- [ ] **Step 3: Implement**

`internal/engine/send_forge.go`:

```go
type SendItem struct {
	…
	Summary  string // the note's summary, cut to 60 runes ("" for a resolve)
}

// SendSkip is one item the plan leaves local, and why. Path/Line/Summary
// name it for a frontend that renders its own text (the TUI); Label is the
// CLI's English line; Reason is a code (domain.Skip* constants).
type SendSkip struct {
	Label, Reason string
	Path          string
	Line          int
	Summary       string
}

// BodyText is the review body as the user reads it: the send marker dropped.
func (p SendPlan) BodyText() string { return strings.TrimSpace(forge.StripSendMarker(p.Body)) }
```

`internal/domain/forge_send.go`: replace each reason literal with its constant; give every `SendSkip` its `Path`, `Line` (the note's/remark's first line, 0 for a file or the summary) and `Summary`; give every `SendItem` its `Summary: cutLabel(<summary>)` (draft replies: `cutLabel(d.Summary)`; resolve/unresolve: none). In `planReview`'s `req.Review` case:

```go
		plan.Key, plan.Body, plan.Verdict = r.ID, reviewSendBody(r), true
		if strings.TrimSpace(req.Body) != "" { // T7: the user edited the summary
			plan.Body = sendBody(model.Note{Source: model.NoteSourceAgent, Author: r.Agent, Summary: strings.TrimSpace(req.Body)}, r.ID, "")
		}
```

(the `summarySent` branch after it still blanks the body).

`internal/domain/forge_send_front.go`:

```go
package domain

import (
	"context"
	"errors"
	"strings"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

// Skip reasons: the codes engine.SendSkip.Reason carries. English protocol
// (the CLI prints them); a frontend maps each to its own words.
const (
	SkipNotInPR       = "not in this PR"
	SkipLinesChanged  = "its lines changed"
	SkipBeingSent     = "already being sent"
	SkipOnGitHub      = "already on GitHub"
	SkipGone          = "it no longer exists"
	SkipThreadNotInPR = "its thread is not in this PR"
)

// SendSkipReasons lists every code (a frontend's translation gate).
func SendSkipReasons() []string {
	return []string{SkipNotInPR, SkipLinesChanged, SkipBeingSent, SkipOnGitHub, SkipGone, SkipThreadNotInPR}
}

// SendGroup is one group a "Send review" can pick (spec §3.5): "my draft
// review" or one AI review, with how many of its notes are still local.
type SendGroup struct {
	ID             string // GroupMine or "review:<id>"
	Agent, Summary string // a review's agent and summary line ("" for mine)
	Count          int
}

// PRSendGroups are PR n's local groups with something to send: "my draft
// review" first, then its AI reviews newest first. The PR's diff must be
// fetched (PRNotes says so otherwise).
func (s *Service) PRSendGroups(ctx context.Context, n int) ([]SendGroup, error) {
	byPath, err := s.PRNotes(ctx, n)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, rs := range byPath {
		for _, r := range rs {
			if r.Note.Source == model.NoteSourceForge || r.Note.IsForgeReply() || r.Sync == model.SyncSending || r.Sync == model.SyncForge {
				continue
			}
			counts[r.Group]++
		}
	}
	var out []SendGroup
	if c := counts[GroupMine]; c > 0 {
		out = append(out, SendGroup{ID: GroupMine, Count: c})
	}
	nc, _ := s.NoteCounts(ctx)
	for _, h := range nc.Reviews { // newest first already
		if c := counts["review:"+h.ID]; c > 0 {
			out = append(out, SendGroup{ID: "review:" + h.ID, Agent: h.Agent, Summary: h.Summary, Count: c})
		}
	}
	return out, nil
}

// ReviewBodyText is an AI review's summary as the user edits it before a
// send (the GitHub review body, unsigned and unmarked).
func (s *Service) ReviewBodyText(ctx context.Context, id string) (string, error) {
	r, err := s.Review(ctx, id)
	if err != nil {
		return "", err
	}
	if r.Doc != nil {
		return strings.TrimSpace(r.Doc.Overview), nil
	}
	return strings.TrimSpace(r.Text), nil
}

// PendingSendsPath is this repository's pending-send queue file — what a
// frontend watches (spec §3.7). The file may not exist yet.
func (s *Service) PendingSendsPath(ctx context.Context) (string, error) { return s.pendingPath(ctx) }

// PendingOutcome is what an approval writes back to the queue entry: sent,
// rejected at the confirm, failed — or still waiting when the approver could
// not answer the confirm (nothing reached the forge).
func PendingOutcome(res engine.Result, err error) (state, outcome string, waiting bool) {
	switch {
	case errors.Is(err, engine.ErrDecisionRequired):
		return PendingWaiting, "", true
	case err != nil:
		return PendingFailed, err.Error(), false
	case strings.HasPrefix(res.Summary, "aborted"):
		return PendingRejected, "rejected at the confirm", false
	}
	return PendingSent, res.Summary, false
}
```

`internal/cli/prpending.go` `approvePending`: keep the `errJoinNeedsConfirm` test (CLI-only), then

```go
	state, outcome, waiting := domain.PendingOutcome(res, err)
	if waiting || errors.Is(err, errJoinNeedsConfirm) {
		// The approver could not answer here: nothing reached GitHub, and the
		// agent's request is still good.
		fmt.Fprintln(stderr, "error:", err)
		fmt.Fprintf(stderr, "still pending: %s (approve it in a terminal, or in gg)\n", e.ID)
		return 1
	}
```

followed by the existing `PendingSendFinish` / printing (the inline `switch` goes).

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/engine/ ./internal/domain/ ./internal/cli/ 2>&1 | tail -10`
Expected: PASS (CLI pending tests prove the outcome move kept behaviour).

- [ ] **Step 5: Commit**

```bash
gg add internal/engine internal/domain internal/cli/prpending.go
git commit -F <msgfile>   # "feat(domain): send groups, an editable review body, reason codes, the queue path and outcome for frontends"
```

---

### Task 4: TUI — sync marks, group bars, a failed send's error, a carried note's origin

**Files:**
- Modify: `internal/tui/diff_view.go` (`diffView.forgePR int`)
- Modify: `internal/tui/diff_notes.go` (`noteLine.group`, `noteLine.errRow`; `noteBoxLines`, `noteBoxTitle`, `collapsedNoteLine`)
- Modify: `internal/tui/diff_render.go` (`noteBoxCell` paints the bar and the error row)
- Modify: `internal/tui/model.go` (`notesLoadedMsg` and `stackNotesMsg` handlers stamp `forgePR`)
- Test: `internal/tui/note_marks_test.go`

**Interfaces:**
- Consumes: `groupSlot`, `groupBarStyle` (Task 1); `ResolvedNote.Sync/SendErr/Group/Origin` (plan 2); `domain.OriginWorkingTree`.
- Produces: `func syncMark(s model.SyncState, inPR bool) string` (`"○"`, `"◌"`, `"○!"`, `"●"`, or `""`); `diffView.forgePR` (the PR whose diff this is, 0 = none) — Tasks 6–7 read it.

Rules (T3): inside a PR's diff (`forgePR > 0`) every root's title starts with its mark and the box wears its group's bar (the left frame column in the group colour); outside, only `◌` and `○!` show and there is no bar. A failed root gets one more row inside its box, `send failed: <error>`, in the error colour. A carried root's title ends with `· from a1b2c3d` / `· from working tree`. Replies carry no mark of their own (a thread moves whole).

- [ ] **Step 1: Write the failing tests**

`internal/tui/note_marks_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// prNotedModel is notedModel inside PR #7's view.
func prNotedModel(t *testing.T) Model {
	t.Helper()
	m := notedModel(t)
	m.diffLayer().forgePR = 7
	return m
}

func titleOf(t *testing.T, v *diffView, id string) string {
	t.Helper()
	byLine, _ := v.noteRowIndex()
	for _, rows := range byLine {
		for _, nl := range rows {
			if nl.rootID == id && nl.kind == noteRowTop {
				return nl.text
			}
		}
	}
	t.Fatalf("no box for %s", id)
	return ""
}

func TestSyncMarksInsideAPR(t *testing.T) {
	t.Parallel()
	m := prNotedModel(t)
	v := m.diffLayer()
	v.notes[0].Sync, v.notes[0].Group = model.SyncLocal, domain.GroupMine
	v.notes[1].Sync, v.notes[1].SendErr, v.notes[1].Group = model.SyncFailed, "HTTP 502: Bad Gateway", "review:r1"
	v.relayout(0)
	if got := titleOf(t, v, "n1"); !strings.HasPrefix(got, "○ ") {
		t.Fatalf("local note title %q", got)
	}
	if got := titleOf(t, v, "n2"); !strings.HasPrefix(got, "○! ") {
		t.Fatalf("failed note title %q", got)
	}
	byLine, _ := v.noteRowIndex()
	var errRow bool
	for _, rows := range byLine {
		for _, nl := range rows {
			if nl.rootID == "n2" && nl.errRow && strings.Contains(nl.text, "HTTP 502") {
				errRow = true
			}
			if nl.rootID == "n2" && nl.group != groupSlot("review:r1") {
				t.Fatalf("row %q carries group slot %d", nl.text, nl.group)
			}
		}
	}
	if !errRow {
		t.Fatal("a failed send shows its error inside the box")
	}
}

func TestMarksOutsideAPRAreOnlyTheOnesThatNeedAttention(t *testing.T) {
	t.Parallel()
	m := notedModel(t) // forgePR 0
	v := m.diffLayer()
	v.notes[0].Sync, v.notes[0].Group = model.SyncLocal, domain.GroupMine
	v.notes[1].Sync, v.notes[1].Group = model.SyncSending, domain.GroupMine
	v.relayout(0)
	if got := titleOf(t, v, "n1"); strings.HasPrefix(got, "○") {
		t.Fatalf("a plain local note outside a PR got a mark: %q", got)
	}
	if got := titleOf(t, v, "n2"); !strings.HasPrefix(got, "◌ ") {
		t.Fatalf("a note being sent shows ◌ everywhere: %q", got)
	}
	byLine, _ := v.noteRowIndex()
	for _, rows := range byLine {
		for _, nl := range rows {
			if nl.group != 0 {
				t.Fatalf("a group bar outside a PR: %+v", nl)
			}
		}
	}
}

func TestCarriedNoteNamesItsOrigin(t *testing.T) {
	t.Parallel()
	m := prNotedModel(t)
	v := m.diffLayer()
	v.notes[0].Origin = "a1b2c3d"
	v.notes[1].Origin = domain.OriginWorkingTree
	v.relayout(0)
	if got := titleOf(t, v, "n1"); !strings.HasSuffix(got, "· from a1b2c3d") {
		t.Fatalf("title %q", got)
	}
	if got := titleOf(t, v, "n2"); !strings.HasSuffix(got, "· from working tree") {
		t.Fatalf("title %q", got)
	}
}

func TestGroupBarPaintsTheLeftFrameColumn(t *testing.T) {
	t.Parallel()
	slot := groupSlot("review:r1")
	bar, _ := groupBarStyle(slot)
	cell := noteBoxCell(noteLine{kind: noteRowSummary, text: "x", group: slot}, 20)
	if !strings.HasPrefix(cell, bar.Render("│")) {
		t.Fatalf("the left frame column is not the group's colour: %q", cell)
	}
	if ansi.StringWidth(cell) != 20 {
		t.Fatalf("width %d", ansi.StringWidth(cell))
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tui/ -run 'SyncMarks|MarksOutsideAPR|CarriedNoteNames|GroupBarPaints' 2>&1 | tail -20`
Expected: FAIL — `v.forgePR undefined`, `nl.group undefined`.

- [ ] **Step 3: Implement**

`internal/tui/diff_view.go`, in `diffView` beside `reviewID`:

```go
	// forgePR is the pull request whose diff this is (0 = none): note boxes
	// wear their sync mark and group bar only there (plan 3, T3). Stamped
	// when the notes arrive (notesLoadedMsg / stackNotesMsg).
	forgePR int
```

`internal/tui/model.go`: in `case notesLoadedMsg:` before `dv.setNotes(msg.notes)` and in `case stackNotesMsg:` before `dv.setNotesFor(...)` add `dv.forgePR = m.openPRNumber()`.

`internal/tui/diff_notes.go`:
- `noteLine` gains

```go
	group  int  // the thread's group colour slot (0 = no bar): a PR view only
	errRow bool // the "send failed: …" row, painted in the error colour
```

- new helpers:

```go
// syncMark is a root's sync mark (spec §1.1). Inside a PR every state shows;
// elsewhere only the two that need attention (T3).
func syncMark(s model.SyncState, inPR bool) string {
	switch s {
	case model.SyncSending:
		return "◌"
	case model.SyncFailed:
		return "○!"
	}
	if !inPR {
		return ""
	}
	if s == model.SyncForge {
		return "●"
	}
	return "○"
}

// noteOrigin is a carried note's title tail ("" for any other note).
func noteOrigin(origin string) string {
	switch origin {
	case "":
		return ""
	case domain.OriginWorkingTree:
		return " · " + i18n.T("from working tree")
	}
	return " · " + i18n.T("from %s", origin)
}
```

- `noteBoxLines`: compute `inPR := v.forgePR > 0`, `slot := 0; if inPR { slot = groupSlot(r.Group) }`; the title becomes

```go
	title := owner.noteBoxTitle(r) + noteOrigin(r.Origin)
	if mk := syncMark(r.Sync, inPR); mk != "" {
		title = mk + " " + title
	}
```

  every row the function builds (frame rows and the body rows from `noteBodyLines`) gets `group = slot` (set it on the slice after building, before returning); when `r.Sync == model.SyncFailed && r.SendErr != ""`, insert before the closing blank+bottom rows the wrapped rows of `i18n.T("send failed: %s", firstLine(r.SendErr))` with `kind: noteRowText, errRow: true`.
- `collapsedNoteLine`: prefix the mark the same way (`syncMark(r.Sync, v.forgePR > 0)`), set `group`.

`internal/tui/diff_render.go` `noteBoxCell`: after `frame` is chosen,

```go
	left := frame
	if bar, ok := groupBarStyle(nl.group); ok && !nl.stale {
		left = bar
	}
	if nl.errRow {
		text = s.errorText
	}
```

and render the FIRST glyph of every row with `left` instead of `frame`: `╭` (top: `left.Render("╭") + frame.Render("─ "+title+" "+rule+"╮")`), `╰` (bottom), the left `│` (blank and body rows) and the collapsed row's `▸ `. Widths are unchanged.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/tui/ -run 'SyncMarks|MarksOutsideAPR|CarriedNoteNames|GroupBarPaints|Note|Forge' 2>&1 | tail -20`
Expected: PASS. Then the translation gate: add `"from working tree"`, `"from %s"`, `"send failed: %s"` to all four bundles (`internal/i18n/lang/{ja,ko,zh,ru}.toml`, near the other note strings):

| key | ja | ko | zh | ru |
|---|---|---|---|---|
| `from working tree` | `作業ツリーから` | `작업 트리에서` | `来自工作区` | `из рабочего дерева` |
| `from %s` | `%s から` | `%s에서` | `来自 %s` | `из %s` |
| `send failed: %s` | `送信失敗: %s` | `전송 실패: %s` | `发送失败：%s` | `отправка не удалась: %s` |

Run: `go test ./internal/i18n/ ./internal/tui/ -run 'I18n|CheckVerbs' 2>&1 | tail -5` — PASS. Then `go test ./e2e/ 2>&1 | tail -5` — PASS with NO golden screen changed (T3: nothing outside a PR moved).

- [ ] **Step 5: Commit**

```bash
gg add internal/tui internal/i18n/lang
git commit -F <msgfile>   # "feat(tui): note sync marks, group colour bars, a failed send's error, a carried note's origin"
```

---

### Task 5: TUI — group bars on the Files tree's note badges

**Files:**
- Modify: `internal/tui/pr_comments.go` (`prCountsMsg.groups`, `prCountsCmd`)
- Modify: `internal/tui/preview_open.go` (`previewOpenMsg.groups` filled where `counts` is, in `openPRPreviewCmd` — `pr_actions.go`)
- Modify: `internal/tui/model.go` (`filesPreviewGroups map[string][]string` beside `filesPreviewCounts`; set where counts are set, cleared where they are cleared)
- Modify: `internal/tui/files_view.go` (badge site, line ~1499), `internal/tui/diff_notes.go` (`noteBadgeGroups`)
- Test: `internal/tui/note_badge_groups_test.go`

**Interfaces:**
- Consumes: `domain.PreviewNoteGroups` (Task 2), `groupSlot`, `groupBarStyle` (Task 1).
- Produces: `func noteBadgeGroups(n int, groups []string) string` — `"  "` + one `▌` per group (at most 3, each in its slot colour) + `"◆ "` + n; `noteBadge(n)` unchanged when `groups` is empty.

- [ ] **Step 1: Write the failing test**

`internal/tui/note_badge_groups_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestNoteBadgeGroupsDrawsOneBarPerGroup(t *testing.T) {
	t.Parallel()
	if got := noteBadgeGroups(3, nil); got != noteBadge(3) {
		t.Fatalf("no groups = the plain badge, got %q", got)
	}
	got := ansi.Strip(noteBadgeGroups(4, []string{"mine", "review:r1"}))
	if got != "  ▌▌◆ 4" {
		t.Fatalf("badge = %q", got)
	}
	many := ansi.Strip(noteBadgeGroups(9, []string{"a", "b", "c", "d", "e"}))
	if strings.Count(many, "▌") != 3 {
		t.Fatalf("at most 3 bars, got %q", many)
	}
	bar, _ := groupBarStyle(groupSlot("review:r1"))
	if !strings.Contains(noteBadgeGroups(4, []string{"review:r1"}), bar.Render("▌")) {
		t.Fatal("the bar wears its group's colour")
	}
}

func TestPRCountsMsgCarriesGroups(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	nm, _ := m.Update(prCountsMsg{gen: m.previewGen, counts: map[string]int{"a.txt": 2}, groups: map[string][]string{"a.txt": {"mine"}}})
	mm := nm.(Model)
	if g := mm.filesPreviewGroups["a.txt"]; len(g) != 1 || g[0] != "mine" {
		t.Fatalf("groups = %v", mm.filesPreviewGroups)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/tui/ -run 'NoteBadgeGroups|PRCountsMsgCarriesGroups' 2>&1 | tail -10`
Expected: FAIL — undefined `noteBadgeGroups`, unknown field `groups`.

- [ ] **Step 3: Implement**

`internal/tui/diff_notes.go`:

```go
// noteBadgeGroups is a PR file row's badge: one bar per note group (at most
// three, each in its group's colour — spec §1.3) before the ◆ count.
func noteBadgeGroups(n int, groups []string) string {
	if n <= 0 || len(groups) == 0 {
		return noteBadge(n)
	}
	var b strings.Builder
	b.WriteString("  ")
	for i, g := range groups {
		if i == 3 {
			break
		}
		if bar, ok := groupBarStyle(groupSlot(g)); ok {
			b.WriteString(bar.Render("▌"))
		}
	}
	return b.String() + "◆ " + strconv.Itoa(n)
}
```

`prCountsMsg` gains `groups map[string][]string`; `prCountsCmd` also calls `svc.PreviewNoteGroups(ctx, set)` (an error leaves groups nil — the plain badge). `previewOpenMsg` gains `groups` filled in `openPRPreviewCmd` beside `msg.counts`. In `model.go` add `filesPreviewGroups map[string][]string` beside `filesPreviewCounts`; every site that assigns or clears `filesPreviewCounts` (grep it) does the same to `filesPreviewGroups`. `files_view.go:1499` becomes `text += noteBadgeGroups(m.filesPreviewCounts[l.path], m.filesPreviewGroups[l.path])`.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/tui/ -run 'NoteBadge|PRCounts|PRComments|FilesView|Preview' 2>&1 | tail -10`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/tui
git commit -F <msgfile>   # "feat(tui): group colour bars on a PR's Files badges"
```

---

### Task 6: TUI — run a send: build the op off-thread, the translated confirm, the follow-up

**Files:**
- Create: `internal/tui/forge_send.go`
- Modify: `internal/tui/model.go` (`forgeSend *forgeSendState` on `Model`; `case forgeSendReadyMsg`; `case opDecisionMsg`; the `opFinishedMsg` follow-up beside the `pendingPROpen` capture, line ~3593)
- Modify: `internal/tui/source.go` (`engine.SendToForge` → `[]sourceKey{srcNotes}`)
- Test: `internal/tui/forge_send_test.go`, `internal/tui/pr_send_serial_test.go`

**Interfaces:**
- Consumes: `domain.PRSendOp`, `domain.PendingOutcome`, `domain.SendSkipReasons`, `domain.Skip*` (Task 3); `engine.SendPlan.BodyText`, `SendItem.Summary`, `SendSkip.Path/Line/Summary`.
- Produces:
  - `type forgeSendState struct { pr int; plan engine.SendPlan; pendingID, event string }`
  - `type forgeSendReadyMsg struct { req domain.PRSendRequest; pendingID string; op engine.SendToForge; err error }`
  - `func (m Model) forgeSendCmd(req domain.PRSendRequest, pendingID string) (Model, tea.Cmd)` — THE way every TUI send starts (Tasks 7, 8, 10, 11).
  - `func sendConfirmText(p engine.SendPlan) string`; `func sendSkipReasonText(reason string) string`.
  - `func prSendModel(t *testing.T) (Model, string, string)` (serial test helper: model, repo dir, PR head) — Tasks 7, 10, 11 reuse it.

- [ ] **Step 1: Write the failing tests**

`internal/tui/forge_send_test.go`:

```go
package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/forge"
	"github.com/homeend/gigagit/internal/i18n"
)

func TestSendConfirmTextNamesEveryItemAndSkip(t *testing.T) {
	t.Parallel()
	p := engine.SendPlan{Target: "o/r #7", Mode: engine.SendReview, Body: "Two things.\n\n" + forge.SendMarker("r1"),
		Items: []engine.SendItem{
			{Kind: engine.SendThread, Thread: forge.Thread{Path: "a.go", Line: 12, StartLine: 12}, Summary: "rename this",
				Replies: []engine.SendReplyBody{{Key: "x"}}},
			{Kind: engine.SendThread, Thread: forge.Thread{Path: "b.go"}, Summary: "split this file"},
		},
		Skipped: []engine.SendSkip{{Reason: domain.SkipNotInPR, Path: "c.go", Line: 3, Summary: "elsewhere"},
			{Reason: domain.SkipOnGitHub}},
	}
	got := sendConfirmText(p)
	for _, want := range []string{"Send to o/r #7:", "review body:", "Two things.", "+ a.go:12 rename this (1 reply)",
		"+ b.go (file) split this file", "- c.go:3 elsewhere (skipped: not in this PR)", "- review summary (skipped: already on GitHub)"} {
		if !strings.Contains(got, want) {
			t.Errorf("confirm lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "gg:") {
		t.Errorf("the send marker leaked into the confirm:\n%s", got)
	}
}

// Review Focus 4: a long review fits the screen.
func TestSendConfirmTextIsCapped(t *testing.T) {
	t.Parallel()
	p := engine.SendPlan{Target: "o/r #7", Body: strings.Repeat("line\n", 40)}
	for i := 0; i < 30; i++ {
		p.Items = append(p.Items, engine.SendItem{Kind: engine.SendThread, Thread: forge.Thread{Path: "a.go", Line: i + 1}, Summary: fmt.Sprint("r", i)})
	}
	got := sendConfirmText(p)
	if n := strings.Count(got, "\n") + 1; n > 24 {
		t.Fatalf("%d rows:\n%s", n, got)
	}
	if !strings.Contains(got, "+ 18 more") {
		t.Fatalf("the cut is not said:\n%s", got)
	}
}

// Every reason code has words of its own — never the generic fallback.
func TestEverySkipReasonHasItsOwnWords(t *testing.T) {
	t.Parallel()
	for _, r := range domain.SendSkipReasons() {
		if got := sendSkipReasonText(r); got == i18n.T("(skipped: %s)", r) {
			t.Errorf("reason %q falls back to the generic text", r)
		}
	}
}

// The engine's English prompt is replaced by the TUI's own; the option the
// agent asked for is preselected.
func TestForgeSendDecisionUsesTheTUIsConfirm(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	m.forgeSend = &forgeSendState{pr: 7, plan: engine.SendPlan{Target: "o/r #7", Verdict: true}, event: engine.OptApprove}
	req := engine.PromptReq(engine.DecisionSendForge, "Send to %s:\n%s",
		[]string{engine.OptComment, engine.OptApprove, engine.OptRequestChanges, "abort"}, "o/r #7", "ENGLISH")
	nm, _ := m.Update(opDecisionMsg{req: req, reply: make(chan engine.DecisionResponse, 1)})
	mm := nm.(Model)
	if mm.modal == nil || strings.Contains(renderPrompt(mm.modal.req), "ENGLISH") || !strings.HasPrefix(renderPrompt(mm.modal.req), "Send to o/r #7:") {
		t.Fatalf("modal prompt = %q", renderPrompt(mm.modal.req))
	}
	if mm.modal.req.Options[mm.modal.sel] != engine.OptApprove {
		t.Fatalf("preselected %q", mm.modal.req.Options[mm.modal.sel])
	}
}

// Review Focus 1: while the confirm is open the comment poll stands down,
// so no refresh re-renders the view under the modal.
func TestThePRPollWaitsWhileTheConfirmIsOpen(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m.forgeSend = &forgeSendState{pr: 7}
	m.modal = &decisionState{req: engine.DecisionRequest{ID: engine.DecisionSendForge, Options: []string{"send", "abort"}}}
	if _, cmd := m.prCommentsTick(time.Now().Add(24 * time.Hour)); cmd != nil {
		t.Fatal("the PR poll ran under the send confirm")
	}
}

// Review Focus 3: a queued request whose plan fails stays queued: the error
// is said and nothing is started or finished.
func TestAFailedPlanLeavesAQueuedRequestWaiting(t *testing.T) {
	t.Parallel()
	m := notedModel(t)
	nm, cmd := m.Update(forgeSendReadyMsg{req: domain.PRSendRequest{PR: 7, Mine: true}, pendingID: "p1",
		err: errors.New("#7's diff is not available here")})
	mm := nm.(Model)
	if cmd != nil || mm.forgeSend != nil || mm.running || !strings.Contains(mm.statusMsg, "not available") {
		t.Fatalf("cmd=%v send=%v running=%v status=%q", cmd != nil, mm.forgeSend != nil, mm.running, mm.statusMsg)
	}
}

func TestSendToForgeRefreshesTheNotes(t *testing.T) {
	t.Parallel()
	srcs := opAffectedSources(engine.SendToForge{})
	if len(srcs) != 1 || srcs[0] != srcNotes {
		t.Fatalf("SendToForge refreshes %v, want [srcNotes]", srcs)
	}
}
```

`internal/tui/pr_send_serial_test.go`:

```go
package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/forge/forgetest"
	"github.com/homeend/gigagit/internal/model"
)

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// prSendModel: main holds big.go (30 lines); PR #7 (refs/gg/pr/7 = feat)
// changes its line 5; the fake gh answers the PR and records every write.
// SERIAL (every test using it): it sets process env and lifts
// domain.ForgeDisabled.
func prSendModel(t *testing.T) (Model, string, string) {
	t.Helper()
	dir, _ := newRepoDir(t)
	var lines []string
	for i := 1; i <= 30; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	write := func(body, msg string) {
		if err := os.WriteFile(filepath.Join(dir, "big.go"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		runGit(t, dir, "add", "big.go")
		runGit(t, dir, "commit", "-q", "-m", msg)
	}
	write(strings.Join(lines, "\n")+"\n", "big")
	runGit(t, dir, "checkout", "-q", "-b", "feat")
	lines[4] = "line 5 changed"
	write(strings.Join(lines, "\n")+"\n", "change")
	head := gitOut(t, dir, "rev-parse", "HEAD")
	runGit(t, dir, "update-ref", "refs/gg/pr/7", head)
	runGit(t, dir, "checkout", "-q", "main")
	fixtures := filepath.Join(dir, ".git", "fakegh")
	t.Setenv(forgetest.EnvBin, forgetest.BuildFakeGH(t))
	t.Setenv(forgetest.EnvFixtures, fixtures)
	domain.ForgeDisabled = false
	t.Cleanup(func() { domain.ForgeDisabled = true })
	forgetest.Seed(t, fixtures, prSendFixtures(head))
	return New(domain.OpenTUI(dir)), dir, head
}

// prSendFixtures are the CLI's sendPRRepo answers (internal/cli/prsend_test.go):
// the PR snapshot before and after a submit, the view, an empty list, the repo.
func prSendFixtures(head string) map[string]string {
	pr := func(threads string) string {
		return fmt.Sprintf(`{"data":{"repository":{"pullRequest":{"id":"PR_7","number":7,"title":"t","body":"","author":{"login":"ann"},
"state":"OPEN","isDraft":false,"reviewDecision":"","headRefName":"feat","isCrossRepository":false,"headRepositoryOwner":{"login":"ann"},
"headRepository":{"name":"r"},"baseRefName":"main","baseRefOid":"","headRefOid":"%s","url":"https://github.com/o/r/pull/7",
"createdAt":"2026-10-01T10:00:00Z","updatedAt":"2026-10-06T10:00:00Z","viewerDidAuthor":false,"viewerLatestReview":null,
"reviewThreads":{"pageInfo":{"hasNextPage":false},"nodes":[%s]},"comments":{"pageInfo":{"hasNextPage":false},"nodes":[]},
"reviews":{"pageInfo":{"hasNextPage":false},"nodes":[]}}}}}`, head, threads)
	}
	sent := `{"id":"PRRT_new1","path":"big.go","line":5,"startLine":null,"originalLine":5,"originalStartLine":null,"diffSide":"RIGHT",
"subjectType":"LINE","isResolved":false,"isOutdated":false,"comments":{"pageInfo":{"hasNextPage":false},"nodes":[{"id":"PRRC_new1",
"replyTo":null,"author":{"login":"me"},"body":"x","diffHunk":"","createdAt":"2026-10-07T10:00:00Z","updatedAt":"2026-10-07T10:00:00Z",
"pullRequestReview":{"id":"PRR_new"}}]}}`
	view := fmt.Sprintf(`{"number":7,"title":"t","author":{"login":"ann"},"state":"OPEN","isDraft":false,"headRefName":"feat",
"isCrossRepository":false,"baseRefName":"main","baseRefOid":"","headRefOid":"%s","url":"https://github.com/o/r/pull/7",
"createdAt":"2026-10-01T10:00:00Z","updatedAt":"2026-10-06T10:00:00Z","body":""}`, head)
	return map[string]string{"snapshot-7.json": pr(""), "snapshot-7-sent.json": pr(sent), "pr-view-7.json": view,
		"pr-list.json": "[]", "repo-view.json": `{"nameWithOwner":"o/r","url":"https://github.com/o/r","sshUrl":"git@github.com:o/r.git"}`}
}

func addTUINote(t *testing.T, m Model, head string, line int, sum string) string {
	t.Helper()
	n, err := m.svc.NoteAdd(context.Background(), model.Note{Source: model.NoteSourceUser, Summary: sum,
		Address: model.FileAddress{State: model.StateCommitted, Commit: head, Path: "big.go"},
		Side:    model.NoteSideNew, Range: [2]int{line, line}})
	if err != nil {
		t.Fatal(err)
	}
	return n.ID
}

// runToModal runs a forgeSendCmd chain until the op asks its question; it
// returns the model and the op's pending wait (the next message).
func runToModal(t *testing.T, m Model, cmd tea.Cmd) (Model, tea.Cmd) {
	t.Helper()
	for i := 0; i < 50 && m.modal == nil; i++ {
		if cmd == nil {
			t.Fatalf("no modal (status %q)", m.statusMsg)
		}
		nm, next := m.Update(cmd())
		m, cmd = nm.(Model), next
	}
	if m.modal == nil {
		t.Fatal("no modal")
	}
	return m, cmd
}

// Serial: env (prSendModel).
func TestSendANoteFromTheTUI(t *testing.T) {
	m, dir, head := prSendModel(t)
	id := addTUINote(t, m, head, 5, "look here")
	m, cmd := m.forgeSendCmd(domain.PRSendRequest{PR: 7, Notes: []string{id}}, "")
	m, wait := runToModal(t, m, cmd)
	prompt := renderPrompt(m.modal.req)
	if !strings.HasPrefix(prompt, "Send to o/r #7:") || !strings.Contains(prompt, "+ big.go:5 look here") {
		t.Fatalf("confirm:\n%s", prompt)
	}
	if ws := forgetest.Writes(t, filepath.Join(dir, ".git", "fakegh")); len(ws) != 0 {
		t.Fatalf("nothing is written before the confirm: %v", ws)
	}
	nm, _ := m.resolveModal("send")
	m = driveOp(t, nm.(Model), wait)
	var ops []string
	for _, w := range forgetest.Writes(t, filepath.Join(dir, ".git", "fakegh")) {
		ops = append(ops, w.Op)
	}
	if strings.Join(ops, ",") != "StartReview,AddThread,SubmitReview" {
		t.Fatalf("writes = %v", ops)
	}
	notes, err := m.svc.PRNotes(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range notes["big.go"] {
		if r.Note.ID == id {
			t.Fatalf("the sent note is still local: %+v", r)
		}
	}
	if m.forgeSend != nil {
		t.Fatal("the send state is cleared once the op ends")
	}
}

// Serial: env. A plan error (here: nothing to send) never starts an op.
func TestSendPlanErrorIsSaidAndNothingRuns(t *testing.T) {
	m, _, _ := prSendModel(t)
	m, cmd := m.forgeSendCmd(domain.PRSendRequest{PR: 7, Notes: []string{"no-such-note"}}, "")
	nm, _ := m.Update(cmd())
	mm := nm.(Model)
	if mm.running || mm.modal != nil || !strings.Contains(mm.statusMsg, "no-such-note") {
		t.Fatalf("running=%v modal=%v status=%q", mm.running, mm.modal != nil, mm.statusMsg)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tui/ -run 'SendConfirmText|EverySkipReason|ForgeSendDecision|SendToForgeRefreshes|PRPollWaits|FailedPlanLeaves|SendANoteFromTheTUI|SendPlanError' 2>&1 | tail -20`
Expected: FAIL — undefined `sendConfirmText`, `forgeSendState`, `forgeSendCmd`.

- [ ] **Step 3: Implement**

`internal/tui/forge_send.go`:

```go
package tui

import (
	"context"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
)

// Sending to GitHub from the TUI (spec 2026-10-07 §3.3, plan 3). Every send
// — a note, a group, a verdict, a reply, an agent's queued request — is
// planned by domain.PRSendOp OFF the Update goroutine (it reads under the
// repo gate, which is not re-entrant), then run with startOp; the op's one
// question (forge.send) is the confirm, which the TUI renders from the plan
// it holds (T4).

// forgeSendState is the send the TUI is running: the plan its confirm shows,
// the PR it writes to, the queued request it answers ("" = the user's own),
// and the verdict an agent asked for (preselected in the confirm).
type forgeSendState struct {
	pr        int
	plan      engine.SendPlan
	pendingID string
	event     string
}

// forgeSendReadyMsg carries the planned op (or why there is none) back.
type forgeSendReadyMsg struct {
	req       domain.PRSendRequest
	pendingID string
	op        engine.SendToForge
	err       error
}

// forgeSendCmd plans req off the UI thread.
func (m Model) forgeSendCmd(req domain.PRSendRequest, pendingID string) (Model, tea.Cmd) {
	svc := m.svc
	if svc == nil {
		return m, nil
	}
	if !m.opsIdle() {
		m = m.sayInDiff(i18n.T("another operation is running — send again when it ends"))
		return m, nil
	}
	m = m.sayInDiff(i18n.T("preparing the send to #%d…", req.PR))
	return m, func() tea.Msg {
		op, err := svc.PRSendOp(context.Background(), req)
		return forgeSendReadyMsg{req: req, pendingID: pendingID, op: op, err: err}
	}
}

// handleForgeSendReady starts the planned op, or says why it cannot. A
// queued request whose plan fails stays queued (it may work once the PR is
// fetched): only the op's outcome finishes it.
func (m Model) handleForgeSendReady(msg forgeSendReadyMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		return m.sayInDiff(i18n.T("send: %s", firstLine(msg.err.Error()))), nil
	}
	if !m.opsIdle() {
		return m.sayInDiff(i18n.T("another operation is running — send again when it ends")), nil
	}
	m.forgeSend = &forgeSendState{pr: msg.req.PR, plan: msg.op.Plan, pendingID: msg.pendingID, event: msg.req.Event}
	return m.startOp(msg.op)
}

// sayInDiff puts msg on the status line and, while a diff is on top (it has
// no status bar), in the diff's notice box.
func (m Model) sayInDiff(msg string) Model {
	m.statusMsg = msg
	if m.diffLayer() != nil {
		m.diffNotice = "▸ " + msg
	}
	return m
}

// The confirm's caps (Review Focus 4): body lines, then item + skip rows.
const (
	sendConfirmBodyRows = 6
	sendConfirmRows     = 12
)

// sendConfirmText is the forge.send prompt in the user's language: target,
// body, every item, every skipped item with its reason — what will be posted.
func sendConfirmText(p engine.SendPlan) string {
	var b []string
	switch p.Mode {
	case engine.SendFinish:
		b = append(b, i18n.T("Finish sending to %s:", p.Target))
	case engine.SendDiscard:
		b = append(b, i18n.T("Discard gg's pending review on %s:", p.Target))
	default:
		b = append(b, i18n.T("Send to %s:", p.Target))
	}
	if p.Pending != "" && p.Mode == engine.SendReview {
		b = append(b, i18n.T("You have a review pending on GitHub: these comments join it and it is submitted."))
	}
	if body := p.BodyText(); body != "" {
		b = append(b, i18n.T("review body:"))
		for i, l := range strings.Split(body, "\n") {
			if i == sendConfirmBodyRows {
				b = append(b, "    …")
				break
			}
			b = append(b, "    "+l)
		}
	}
	var rows []string
	for _, it := range p.Items {
		rows = append(rows, "  + "+sendItemText(p.Mode, it))
	}
	for _, sk := range p.Skipped {
		rows = append(rows, "  - "+sendSkipText(sk))
	}
	if len(rows) > sendConfirmRows {
		more := len(rows) - sendConfirmRows
		rows = append(rows[:sendConfirmRows], "  "+i18n.T("+ %d more", more))
	}
	return strings.Join(append(b, rows...), "\n")
}

// sendWhere is "path:line" or "path (file)": data plus one translated word.
func sendWhere(path string, line int) string {
	if line == 0 {
		return path + " " + i18n.T("(file)")
	}
	return path + ":" + strconv.Itoa(line)
}

func sendItemText(mode engine.SendMode, it engine.SendItem) string {
	if mode == engine.SendFinish || mode == engine.SendDiscard {
		return i18n.T("%s (waiting in the pending review)", it.Key)
	}
	switch it.Kind {
	case engine.SendReply:
		return i18n.T("reply: %s", it.Summary)
	case engine.SendResolve:
		return i18n.T("resolve thread %s", it.ThreadID)
	case engine.SendUnresolve:
		return i18n.T("reopen thread %s", it.ThreadID)
	}
	line := it.Thread.StartLine
	if line == 0 {
		line = it.Thread.Line
	}
	s := sendWhere(it.Thread.Path, line) + " " + it.Summary
	switch n := len(it.Replies); {
	case n == 1:
		s += " " + i18n.T("(1 reply)")
	case n > 1:
		s += " " + i18n.T("(%d replies)", n)
	}
	if it.Resolve {
		s += " · " + i18n.T("resolved after sending")
	}
	return s
}

func sendSkipText(sk engine.SendSkip) string {
	what := sk.Summary
	switch {
	case sk.Path != "":
		what = sendWhere(sk.Path, sk.Line) + " " + sk.Summary
	case sk.Reason == domain.SkipOnGitHub:
		what = i18n.T("review summary")
	}
	return what + " " + sendSkipReasonText(sk.Reason)
}

// sendSkipReasonText is one reason code in words; an unknown code (a newer
// domain) shows as data in a generic frame.
func sendSkipReasonText(reason string) string {
	switch reason {
	case domain.SkipNotInPR:
		return i18n.T("(skipped: not in this PR)")
	case domain.SkipLinesChanged:
		return i18n.T("(skipped: its lines changed)")
	case domain.SkipBeingSent:
		return i18n.T("(skipped: already being sent)")
	case domain.SkipOnGitHub:
		return i18n.T("(skipped: already on GitHub)")
	case domain.SkipGone:
		return i18n.T("(skipped: it no longer exists)")
	case domain.SkipThreadNotInPR:
		return i18n.T("(skipped: its thread is not in this PR)")
	}
	return i18n.T("(skipped: %s)", reason)
}

// forgeSendFinished is the op's follow-up (opFinishedMsg): answer the queued
// request, re-read the PR (its threads now hold what was sent) and recount
// the badges. The notes themselves reload through srcNotes (the op's
// opAffectedSources), whose arrival re-resolves the open diff's boxes.
func (m Model) forgeSendFinished(fs *forgeSendState, res engine.Result, err error) (Model, tea.Cmd) {
	var cmds []tea.Cmd
	if fs.pendingID != "" {
		cmds = append(cmds, m.pendingFinishCmd(fs.pendingID, res, err))
	}
	if fs.pr != 0 && fs.pr == m.openPRNumber() {
		var c tea.Cmd
		m, c = m.prRefreshCmd(fs.pr, false)
		cmds = append(cmds, c, m.prCountsCmd())
	}
	return m, tea.Batch(cmds...)
}
```

`pendingFinishCmd` is Task 10's; until then add a stub in `forge_send.go` that Task 10 replaces:

```go
// pendingFinishCmd writes a queued request's outcome back (Task 10).
func (m Model) pendingFinishCmd(id string, res engine.Result, err error) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		if st, out, waiting := domain.PendingOutcome(res, err); !waiting {
			_, _ = svc.PendingSendFinish(context.Background(), id, st, out)
		}
		return nil
	}
}
```

`internal/tui/model.go`:
- `Model` gains `forgeSend *forgeSendState // the send being run (forge_send.go); nil = none` beside `pendingPROpen`.
- `case forgeSendReadyMsg: return m.handleForgeSendReady(msg)` beside `case prFetchReadyMsg:`.
- `case opDecisionMsg:` becomes

```go
	case opDecisionMsg:
		req, sel := msg.req, 0
		if fs := m.forgeSend; fs != nil && req.ID == engine.DecisionSendForge {
			// T4: the TUI's own words for the plan it holds; the agent's wish preselected.
			req.Prompt, req.PromptMsg = sendConfirmText(fs.plan), engine.Msg{}
			if i := slices.Index(req.Options, fs.event); i >= 0 {
				sel = i
			}
		}
		m.modal = &decisionState{req: req, reply: msg.reply, sel: sel}
		return m, m.waitForOp(m.opMsgs)
```

- in the `opFinishedMsg` handler next to `prOpen := m.pendingPROpen` / `m.pendingPROpen = nil`: `fs := m.forgeSend; m.forgeSend = nil`; after the existing completion work (status line, source reload), `if fs != nil { var c tea.Cmd; m, c = m.forgeSendFinished(fs, msg.res, msg.err); cmd = tea.Batch(cmd, c) }` (adapt to the handler's local names).

`internal/tui/source.go`: split `engine.SendToForge` out of the `FetchPRHead, ForgetPR` case:

```go
	case engine.SendToForge:
		// A send deletes the local notes GitHub now holds (and stamps the
		// rest): the notes and their badges change; no git state does.
		return []sourceKey{srcNotes}
```

Bundles (all four) for every new key: `another operation is running — send again when it ends`, `preparing the send to #%d…`, `send: %s`, `Finish sending to %s:`, `Discard gg's pending review on %s:`, `Send to %s:`, `You have a review pending on GitHub: these comments join it and it is submitted.`, `review body:`, `+ %d more`, `%s (waiting in the pending review)`, `reply: %s`, `resolve thread %s`, `reopen thread %s`, `resolved after sending`, `review summary`, the six `(skipped: …)` keys and `(skipped: %s)`. Write real translations (ja/ko/zh/ru) in the bundles' existing register; keep every `%s`/`%d` in order.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/tui/ -run 'SendConfirm|EverySkipReason|ForgeSend|SendToForge|SendANote|SendPlanError|I18n|CheckVerbs|PROps' 2>&1 | tail -20`
Expected: PASS (`TestPROpsRefreshNoSources` covers only FetchPRHead/ForgetPR and stays as is).

- [ ] **Step 5: Commit**

```bash
gg add internal/tui internal/i18n/lang
git commit -F <msgfile>   # "feat(tui): run a send — plan off-thread, a translated confirm, reload after"
```

---

### Task 7: TUI — Send review… and Verdict… (group pick → body → the confirm)

**Files:**
- Create: `internal/tui/send_review_popup.go`
- Modify: `internal/tui/pr_hub.go` (`s` and `v` keys, footer), `internal/tui/action_menu.go` (two rows while a PR's diff or files view is open)
- Modify: `internal/tui/model.go` (`case sendGroupsMsg`, `case sendBodyMsg`)
- Test: `internal/tui/send_review_popup_test.go`, `internal/tui/pr_send_serial_test.go`

**Interfaces:**
- Consumes: `domain.PRSendGroups`, `domain.ReviewBodyText`, `domain.GroupMine`, `domain.SendGroup` (Task 3); `forgeSendCmd` (Task 6).
- Produces:
  - `func (m Model) openSendReview(pr int) (Model, tea.Cmd)` — loads the groups, then the picker (skipped for one group).
  - `func (m Model) openSendReviewBody(pr int, group string) (Model, tea.Cmd)` — Task 8's note row calls it with the note's group.
  - `func (m Model) openVerdict(pr int) (Model, tea.Cmd)`.
  - `type sendReviewPopup struct{…}` with `func (p *sendReviewPopup) request() domain.PRSendRequest`.

Flow (spec §3.5): **Send review…** → the groups with something to send (`PRSendGroups`) → a chooser (one group: no chooser) → the body popup, prefilled with an AI review's summary (`ReviewBodyText`; "my draft review": empty) → ctrl+s → `forgeSendCmd` → the confirm offers comment / approve / request-changes (approve and request-changes absent on your own PR). **Verdict…** → the body popup (empty, optional) → the same confirm with no threads.

- [ ] **Step 1: Write the failing tests**

`internal/tui/send_review_popup_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
)

func TestSendReviewPopupRequest(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		p    sendReviewPopup
		want domain.PRSendRequest
	}{
		{sendReviewPopup{pr: 7, group: domain.GroupMine, body: newTextField(" LGTM \n")}, domain.PRSendRequest{PR: 7, Mine: true, Body: "LGTM"}},
		{sendReviewPopup{pr: 7, group: "review:r1", body: newTextField("edited")}, domain.PRSendRequest{PR: 7, Review: "r1", Body: "edited"}},
		{sendReviewPopup{pr: 7, verdict: true}, domain.PRSendRequest{PR: 7, Verdict: true}},
	} {
		if got := c.p.request(); got.PR != c.want.PR || got.Mine != c.want.Mine || got.Review != c.want.Review ||
			got.Verdict != c.want.Verdict || got.Body != c.want.Body {
			t.Errorf("request = %+v, want %+v", got, c.want)
		}
	}
}

func TestSendGroupsOpenAChooserThenTheBody(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	nm, _ := m.Update(sendGroupsMsg{pr: 7, groups: []domain.SendGroup{
		{ID: domain.GroupMine, Count: 2}, {ID: "review:r1", Agent: "claude", Summary: "two nits", Count: 1}}})
	mm := nm.(Model)
	if mm.modal == nil || len(mm.modal.req.Options) != 3 || mm.modal.req.Options[2] != "Cancel" {
		t.Fatalf("chooser = %+v", mm.modal)
	}
	if !strings.Contains(mm.modal.req.Options[0], "2") || !strings.Contains(mm.modal.req.Options[1], "claude") {
		t.Fatalf("labels = %q", mm.modal.req.Options)
	}
	nm, _ = mm.resolveModal(mm.modal.req.Options[0]) // my draft review: no body to load
	p := layerOf[*sendReviewPopup](nm.(Model))
	if p == nil || p.group != domain.GroupMine || p.body.Value() != "" {
		t.Fatalf("body popup = %+v", p)
	}
}

func TestOneSendGroupSkipsTheChooser(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	nm, _ := m.Update(sendGroupsMsg{pr: 7, groups: []domain.SendGroup{{ID: domain.GroupMine, Count: 1}}})
	if p := layerOf[*sendReviewPopup](nm.(Model)); p == nil || nm.(Model).modal != nil {
		t.Fatal("one group goes straight to the body")
	}
	nm, _ = m.Update(sendGroupsMsg{pr: 7})
	if !strings.Contains(nm.(Model).statusMsg, "#7") {
		t.Fatalf("no group: status %q", nm.(Model).statusMsg)
	}
}

func TestSendBodyMsgPrefillsTheReviewSummary(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	nm, _ := m.Update(sendBodyMsg{pr: 7, group: "review:r1", body: "Two nits."})
	if p := layerOf[*sendReviewPopup](nm.(Model)); p == nil || p.body.Value() != "Two nits." {
		t.Fatalf("popup = %+v", p)
	}
}

func TestSendReviewPopupKeys(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m, _ = m.openVerdict(7)
	p := layerOf[*sendReviewPopup](m)
	if p == nil || !p.verdict {
		t.Fatal("Verdict… opens the body popup in verdict mode")
	}
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyEnter})
	if p.body.Value() != "\n" {
		t.Fatalf("enter is a newline in the body, got %q", p.body.Value())
	}
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyEsc})
	if layerOf[*sendReviewPopup](m) != nil {
		t.Fatal("esc closes it")
	}
}

func TestPRHubAdvertisesAndRunsSendKeys(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m, _ = m.openPRHub(m.prs[0])
	hub := layerOf[*prHubPopup](m)
	if !strings.Contains(hub.keys, "[s]") || !strings.Contains(hub.keys, "[v]") {
		t.Fatalf("hub footer %q", hub.keys)
	}
	nm, _ := hub.update(m, synthKey("v"))
	if layerOf[*sendReviewPopup](nm) == nil {
		t.Fatal("v opens Verdict…")
	}
}
```

Append to `internal/tui/pr_send_serial_test.go`:

```go
// Serial: env. "my draft review" with a verdict: one review, every local note,
// the event the user picked.
func TestSendMyDraftReviewWithAVerdict(t *testing.T) {
	m, dir, head := prSendModel(t)
	addTUINote(t, m, head, 5, "look here")
	m, _ = m.openSendReviewBody(7, domain.GroupMine)
	p := layerOf[*sendReviewPopup](m)
	p.body = newTextField("LGTM")
	m, cmd := p.update(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	m, wait := runToModal(t, m, cmd)
	if got := strings.Join(m.modal.req.Options, ","); got != "comment,approve,request-changes,abort" {
		t.Fatalf("options %s", got)
	}
	nm, _ := m.resolveModal("approve")
	m = driveOp(t, nm.(Model), wait)
	ws := forgetest.Writes(t, filepath.Join(dir, ".git", "fakegh"))
	last := ws[len(ws)-1]
	if last.Op != "SubmitReview" || last.Vars["event"] != "APPROVE" || !strings.Contains(last.Vars["body"].(string), "LGTM") {
		t.Fatalf("submit = %+v", last)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tui/ -run 'SendReviewPopup|SendGroups|OneSendGroup|SendBodyMsg|PRHubAdvertises|SendMyDraftReview' 2>&1 | tail -20`
Expected: FAIL — undefined `sendReviewPopup`, `sendGroupsMsg`.

- [ ] **Step 3: Implement**

`internal/tui/send_review_popup.go`:

```go
package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
)

// sendReviewPopup is the review body before a send (spec §3.5): Send review
// (a group's notes as one GitHub review) or Verdict (no threads). The verdict
// itself is the op's confirm.
type sendReviewPopup struct {
	popupMax
	pr      int
	group   string // domain.GroupMine or "review:<id>"; "" with verdict
	verdict bool
	body    textfield
	scroll  int
}

// sendGroupsMsg is a PR's groups with something to send.
type sendGroupsMsg struct {
	pr     int
	groups []domain.SendGroup
	err    error
}

// sendBodyMsg is an AI review's summary, read to prefill the body.
type sendBodyMsg struct {
	pr          int
	group, body string
	err         error
}

func (m Model) openSendReview(pr int) (Model, tea.Cmd) {
	svc := m.svc
	if svc == nil || pr == 0 {
		return m, nil
	}
	return m, func() tea.Msg {
		gs, err := svc.PRSendGroups(context.Background(), pr)
		return sendGroupsMsg{pr: pr, groups: gs, err: err}
	}
}

func (m Model) handleSendGroups(msg sendGroupsMsg) (Model, tea.Cmd) {
	switch {
	case msg.err != nil:
		return m.sayInDiff(i18n.T("send: %s", firstLine(msg.err.Error()))), nil
	case len(msg.groups) == 0:
		return m.sayInDiff(i18n.T("nothing to send to #%d", msg.pr)), nil
	case len(msg.groups) == 1:
		return m.openSendReviewBody(msg.pr, msg.groups[0].ID)
	}
	opts := make([]string, 0, len(msg.groups)+1)
	for _, g := range msg.groups {
		opts = append(opts, sendGroupLabel(g))
	}
	opts = append(opts, "Cancel")
	groups := msg.groups
	m.modal = &decisionState{
		req: engine.DecisionRequest{ID: "pr-send-group", Prompt: i18n.T("Send which review to #%d?", msg.pr),
			Options: opts}, // dynamic by nature: the groups' own words
		onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
			for i, g := range groups {
				if opt == opts[i] {
					return m.openSendReviewBody(msg.pr, g.ID)
				}
			}
			return m, nil
		},
	}
	return m, nil
}

// sendGroupLabel is one chooser row: "my draft review · 2 notes" or
// "claude: two nits · 3 remarks".
func sendGroupLabel(g domain.SendGroup) string {
	if g.ID == domain.GroupMine {
		if g.Count == 1 {
			return i18n.T("my draft review · 1 note")
		}
		return i18n.T("my draft review · %d notes", g.Count)
	}
	sum := truncate(sanitizeLine(g.Summary), 40)
	if g.Count == 1 {
		return i18n.T("%s: %s · 1 remark", g.Agent, sum)
	}
	return i18n.T("%s: %s · %d remarks", g.Agent, sum, g.Count)
}

func (m Model) openSendReviewBody(pr int, group string) (Model, tea.Cmd) {
	id, isReview := strings.CutPrefix(group, "review:")
	if !isReview || m.svc == nil {
		return m.pushLayer(&sendReviewPopup{pr: pr, group: group, body: newTextField("")}), nil
	}
	svc := m.svc
	return m, func() tea.Msg {
		body, err := svc.ReviewBodyText(context.Background(), id)
		return sendBodyMsg{pr: pr, group: group, body: body, err: err}
	}
}

func (m Model) handleSendBody(msg sendBodyMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		return m.sayInDiff(i18n.T("send: %s", firstLine(msg.err.Error()))), nil
	}
	return m.pushLayer(&sendReviewPopup{pr: msg.pr, group: msg.group, body: newTextField(msg.body)}), nil
}

func (m Model) openVerdict(pr int) (Model, tea.Cmd) {
	if pr == 0 {
		return m, nil
	}
	return m.pushLayer(&sendReviewPopup{pr: pr, verdict: true, body: newTextField("")}), nil
}

// request is what ctrl+s sends.
func (p *sendReviewPopup) request() domain.PRSendRequest {
	req := domain.PRSendRequest{PR: p.pr, Body: strings.TrimSpace(p.body.Value())}
	switch {
	case p.verdict:
		req.Verdict = true
	case p.group == domain.GroupMine:
		req.Mine = true
	default:
		req.Review = strings.TrimPrefix(p.group, "review:")
	}
	return req
}

func (p *sendReviewPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyCtrlS:
		m = m.popLayer()
		return m.forgeSendCmd(p.request(), "")
	case tea.KeyEnter:
		p.body.InsertNewline()
		return m, nil
	case tea.KeyUp:
		p.body.Up()
		return m, nil
	case tea.KeyDown:
		p.body.Down()
		return m, nil
	}
	p.body.HandleEditKey(msg)
	return m, nil
}

func (p *sendReviewPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

func (p *sendReviewPopup) box(m Model) string {
	w, _ := m.overlayDims()
	innerW := popupResolveWidth(w, p.maximized, commitNormalWidth(w))
	contentW := popupTextWidth(innerW)
	heading := i18n.T("Send review to #%d", p.pr)
	what := i18n.T("my draft review")
	switch {
	case p.verdict:
		heading, what = i18n.T("Verdict on #%d", p.pr), i18n.T("no comments, only the verdict")
	case p.group != domain.GroupMine:
		what = i18n.T("an AI review: its summary is the review body")
	}
	var b strings.Builder
	b.WriteString(heading + "\n" + what + "\n\n")
	b.WriteString(viewFieldWindow("> "+i18n.T("body:")+" ", p.body, true, contentW, 10, &p.scroll) + "\n")
	b.WriteString(i18n.T("the next step asks for the verdict: comment, approve or request changes") + "\n")
	b.WriteString("\n" + packHints([]string{
		i18n.T("[enter] newline"),
		i18n.T("[ctrl+s] continue"),
		i18n.T("[ctrl+t] fullscreen"),
		i18n.T("[esc] cancel"),
	}, contentW))
	return popupBox(innerW, b.String())
}
```

Register the popup with the layer machinery exactly as `notePopup` is (grep `*notePopup` in `layers.go`/`model.go` for the layer interface and the key routing — ctrl+t `popupMax` included); `model.go` gets `case sendGroupsMsg: return m.handleSendGroups(msg)` and `case sendBodyMsg: return m.handleSendBody(msg)`.

`internal/tui/pr_hub.go`: `cp.keys = i18n.T("[y] copy URL  [r] reload  [s] send review  [v] verdict")` (remove the old key `[y] copy URL  [r] reload` from all four bundles — the orphan gate), and in `update`:

```go
		case "s":
			return m.openSendReview(p.pr.Number)
		case "v":
			return m.openVerdict(p.pr.Number)
```

`internal/tui/action_menu.go`: right after the `pr-hub-diff` row (`if m.openPRNumber() > 0 {`):

```go
			n := m.openPRNumber()
			rows = append(rows,
				actionRow{id: "pr-send-review", label: i18n.T("Send review…"), run: func(m Model) (tea.Model, tea.Cmd) { return m.openSendReview(n) }},
				actionRow{id: "pr-verdict", label: i18n.T("Verdict…"), run: func(m Model) (tea.Model, tea.Cmd) { return m.openVerdict(n) }})
```

and the same two rows in the files-view branch of `availableActions` while `m.openPRNumber() > 0` (find where the PR files view's rows are built — the branch that adds `Pull request details…` for the files view, or add it beside `pr-hub-diff` if the files view shares that branch).

Bundles (all four): `nothing to send to #%d`, `Send which review to #%d?`, `my draft review · 1 note`, `my draft review · %d notes`, `%s: %s · 1 remark`, `%s: %s · %d remarks`, `Send review to #%d`, `my draft review`, `Verdict on #%d`, `no comments, only the verdict`, `an AI review: its summary is the review body`, `body:`, `the next step asks for the verdict: comment, approve or request changes`, `[enter] newline`, `[ctrl+s] continue`, `[y] copy URL  [r] reload  [s] send review  [v] verdict`, `Send review…`, `Verdict…` (reuse any key that already exists — the gate rejects duplicates only across files, never a reused key).

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/tui/ -run 'SendReview|SendGroups|OneSendGroup|SendBodyMsg|PRHub|SendMyDraftReview|I18n|ActionMenuLabels|CheckVerbs|PopupKeyHints' 2>&1 | tail -20`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/tui internal/i18n/lang
git commit -F <msgfile>   # "feat(tui): Send review… and Verdict… — pick a group, edit the body, the confirm picks the verdict"
```

---

### Task 8: TUI — the `.` menu on a note in a PR's diff; R and x on GitHub threads

**Files:**
- Create: `internal/tui/forge_send_menu.go`
- Modify: `internal/tui/note_keys.go` (`noteTarget.sync/group/drafts/forge`; `noteTargetIn`; `replyableNoteTargets(ts, inPR)`; `toggleThreadResolved`; `noteMenuRows` appends the forge rows)
- Modify: `internal/tui/note_popup.go` (`notePopup.sendPR`; `noteSubmitCmd` returns the new reply's id; the read-only refusal only outside a PR)
- Modify: `internal/tui/model.go` (`noteMutatedMsg.sendPR/sendID` → `forgeSendCmd` after a successful Reply & send)
- Test: `internal/tui/forge_send_menu_test.go`, `internal/tui/pr_send_serial_test.go`

**Interfaces:**
- Consumes: `diffView.forgePR` (Task 4), `forgeSendCmd` (Task 6), `openSendReviewBody` (Task 7).
- Produces: `func (m Model) prOfDiff() int` (the PR whose diff is on top, 0 = none: `dv.forgePR` when it equals `m.openPRNumber()`); `func noteSendRequest(pr int, t noteTarget) domain.PRSendRequest`; `func threadActionRequest(pr int, t noteTarget) domain.PRSendRequest`.

Rows, inside a PR's diff only (§4.1): on a local root in reach — **Send to GitHub** (**Retry sending to GitHub** when its last send failed; none while it is being sent), **Send my draft review…** or **Send this AI review…** (its group: Task 7's body popup, no chooser); on a GitHub thread — **Reply to note** (the existing row, now offered), **Reply & send…**, **Resolve thread / Reopen thread** (the existing row: on GitHub, no confirm — spec §3.5), **Send draft reply** / **Send N draft replies** when local drafts hang under it. Delete stays local-only (`editableNoteTargets` already refuses GitHub threads and remarks). Outside a PR nothing changes: GitHub threads stay read-only (`TestForgeNotesAreReadOnlyInTheDiff` keeps passing unchanged — it runs outside a PR).

- [ ] **Step 1: Write the failing tests**

`internal/tui/forge_send_menu_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
)

// prMenuModel is notedModel inside PR #7's diff with a service (forgeSendCmd
// needs one to dispatch; the command itself is never run here).
func prMenuModel(t *testing.T) Model {
	t.Helper()
	m := prNotedModel(t)
	m.svc = domain.New(&git.Repo{Runner: gitexec.NewFakeRunner()})
	m.previewOpen = &previewOpenState{prNumber: 7}
	m.filesView = newContentPopup("PR #7", nil)
	return m
}

func rowIDs(rows []actionRow) string {
	var ids []string
	for _, r := range rows {
		ids = append(ids, r.id)
	}
	return strings.Join(ids, ",")
}

func TestPRNoteMenuSendsALocalNote(t *testing.T) {
	t.Parallel()
	m := prMenuModel(t)
	v := m.diffLayer()
	v.notes[0].Group = domain.GroupMine
	v.setCursorLine(4, m.diffBodyRows())
	ids := rowIDs(m.noteMenuRows())
	for _, want := range []string{"note-send", "note-send-review", "note-edit", "note-reply", "note-delete"} {
		if !strings.Contains(ids, want) {
			t.Errorf("rows %s lack %s", ids, want)
		}
	}
	tg, _ := m.noteNearCursor()
	if req := noteSendRequest(7, tg); req.PR != 7 || len(req.Notes) != 1 || req.Notes[0] != "n1" {
		t.Fatalf("request %+v", req)
	}
}

func TestPRNoteMenuRetriesAFailedNote(t *testing.T) {
	t.Parallel()
	m := prMenuModel(t)
	v := m.diffLayer()
	v.notes[0].Sync, v.notes[0].SendErr = model.SyncFailed, "HTTP 502"
	v.setCursorLine(4, m.diffBodyRows())
	for _, r := range m.noteMenuRows() {
		if r.id == "note-send" && r.label != "Retry sending to GitHub" {
			t.Fatalf("label %q", r.label)
		}
	}
	v.notes[0].Sync = model.SyncSending
	if strings.Contains(rowIDs(m.noteMenuRows()), "note-send,") {
		t.Fatal("a note being sent offers no second send")
	}
}

func TestPRNoteMenuOnAGitHubThread(t *testing.T) {
	t.Parallel()
	m := prMenuModel(t)
	v := m.diffLayer()
	root := forgeRoot("C1", 5, "rename this", false)
	draft := rootNote("d1", 5, "on it", "", model.NoteSourceUser, model.NoteActive)
	draft.Note.ParentID = model.ForgeNoteIDPrefix + "C1"
	root.Replies = []domain.ResolvedNote{draft}
	v.notes = []domain.ResolvedNote{root}
	v.relayout(0)
	v.setCursorLine(4, m.diffBodyRows())
	ids := rowIDs(m.noteMenuRows())
	for _, want := range []string{"note-reply", "note-reply-send", "note-resolve", "note-send-drafts"} {
		if !strings.Contains(ids, want) {
			t.Errorf("rows %s lack %s", ids, want)
		}
	}
	for _, not := range []string{"note-send,", "note-edit", "note-delete"} {
		if strings.Contains(ids+",", not) {
			t.Errorf("rows %s offer %s on a GitHub thread", ids, not)
		}
	}
	tg, _ := m.noteNearCursor()
	if req := threadActionRequest(7, tg); len(req.Resolve) != 1 || req.Resolve[0] != "forge:C1" {
		t.Fatalf("resolve request %+v", req)
	}
	tg.resolved = true
	if req := threadActionRequest(7, tg); len(req.Unresolve) != 1 {
		t.Fatalf("reopen request %+v", req)
	}
}

func TestRAndXOnAGitHubThreadInsideAPR(t *testing.T) {
	t.Parallel()
	m := prMenuModel(t)
	v := m.diffLayer()
	v.notes = []domain.ResolvedNote{forgeRoot("C1", 5, "rename this", false)}
	v.relayout(0)
	v.setCursorLine(4, m.diffBodyRows())
	nm, _ := v.update(m, synthKey("R"))
	if p := layerOf[*notePopup](nm); p == nil || p.targetID != "forge:C1" {
		t.Fatal("R inside a PR answers a GitHub thread")
	}
	nm, cmd := v.update(m, synthKey("x"))
	if cmd == nil || !strings.Contains(nm.statusMsg, "#7") {
		t.Fatalf("x resolves on GitHub through a send (status %q)", nm.statusMsg)
	}
}
```

(If `newContentPopup`'s signature differs, build the files view the way `prDiffModel` gets one; `openPRNumber()` needs both `previewOpen` and `filesView` non-nil.)

Append to `internal/tui/pr_send_serial_test.go`:

```go
// Serial: env. Reply & send: the draft is written, then sent as a reply.
func TestReplyAndSendFromThePRView(t *testing.T) {
	m, dir, _ := prSendModel(t)
	fixtures := filepath.Join(dir, ".git", "fakegh")
	b, _ := os.ReadFile(filepath.Join(fixtures, "snapshot-7-sent.json"))
	forgetest.Seed(t, fixtures, map[string]string{"snapshot-7.json": string(b)}) // a thread to answer
	ctx := context.Background()
	if _, err := m.svc.PRRevalidate(ctx, 7); err != nil {
		t.Fatal(err)
	}
	d, err := m.svc.NoteReply(ctx, "forge:PRRC_new1", model.Note{Source: model.NoteSourceUser, Summary: "on it"})
	if err != nil {
		t.Fatal(err)
	}
	nm, cmd := m.Update(noteMutatedMsg{sendPR: 7, sendID: d.ID})
	m, wait := runToModal(t, nm.(Model), cmd)
	if !strings.Contains(renderPrompt(m.modal.req), "reply: on it") {
		t.Fatalf("confirm:\n%s", renderPrompt(m.modal.req))
	}
	nm2, _ := m.resolveModal("send")
	driveOp(t, nm2.(Model), wait)
	ws := forgetest.Writes(t, fixtures)
	if len(ws) != 1 || ws[0].Op != "Reply" || ws[0].Vars["thread"] != "PRRT_new1" {
		t.Fatalf("writes %+v", ws)
	}
}
```

(`runToModal` must keep feeding every message the batch returns: if `m.Update(noteMutatedMsg…)` returns a `tea.Batch`, flatten it with `flattenCmd` and feed each message — adjust `runToModal` once, for every caller.)

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tui/ -run 'PRNoteMenu|RAndXOnAGitHubThread|ReplyAndSend' 2>&1 | tail -20`
Expected: FAIL — undefined `noteSendRequest`, `threadActionRequest`, unknown fields `sendPR`/`sendID`.

- [ ] **Step 3: Implement**

`internal/tui/note_keys.go`:
- `noteTarget` gains

```go
	sync   model.SyncState // where the thread's root lives (plan 3)
	group  string          // the root's group (GroupMine, "review:<id>", "github:<id>")
	forge  bool            // the root is a GitHub thread
	drafts []string        // local draft replies under a GitHub thread, oldest first
```

  set in `noteTargetIn` from `r.Sync`, `r.Group`, `r.Note.Source == model.NoteSourceForge`, and the replies with `rep.Note.IsForgeReply() && rep.Sync != model.SyncForge && rep.Sync != model.SyncSending`.
- `replyableNoteTargets(ts []noteTarget, inPR bool)` keeps forge roots when `inPR`; update its three callers to pass `m.prOfDiff() > 0`.
- `toggleThreadResolved`: inside the per-target act, a forge target goes to GitHub:

```go
		if t.forge {
			return m.forgeSendCmd(threadActionRequest(m.prOfDiff(), t), "")
		}
```

  and the "resolved on GitHub" refusal stays for the outside-a-PR case only.
- `noteMenuRows`: `rows = append(rows, m.forgeNoteRows()...)` before the Delete row.

`internal/tui/forge_send_menu.go`:

```go
package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// prOfDiff is the pull request whose diff is on top (0 = none): the view's
// own stamp, and only while that PR is still the open one.
func (m Model) prOfDiff() int {
	v := m.diffLayer()
	if v == nil || v.forgePR == 0 || v.forgePR != m.openPRNumber() {
		return 0
	}
	return v.forgePR
}

// noteSendRequest sends one local root (a note or an AI review's remark).
func noteSendRequest(pr int, t noteTarget) domain.PRSendRequest {
	return domain.PRSendRequest{PR: pr, Notes: []string{t.rootID}}
}

// threadActionRequest resolves a GitHub thread, or reopens a resolved one.
func threadActionRequest(pr int, t noteTarget) domain.PRSendRequest {
	if t.resolved {
		return domain.PRSendRequest{PR: pr, Unresolve: []string{t.rootID}}
	}
	return domain.PRSendRequest{PR: pr, Resolve: []string{t.rootID}}
}

// sendable is a local root that can go now (not on GitHub, not in flight).
func sendable(t noteTarget) bool {
	return !t.forge && t.sync != model.SyncSending && t.sync != model.SyncForge
}

// forgeNoteRows are the . menu's GitHub rows for the threads in reach,
// inside a PR's diff only (spec §4.1).
func (m Model) forgeNoteRows() []actionRow {
	pr := m.prOfDiff()
	if pr == 0 {
		return nil
	}
	all := m.notesAtCursor()
	var local, threads, drafted []noteTarget
	for _, t := range all {
		switch {
		case t.forge:
			threads = append(threads, t)
			if len(t.drafts) > 0 {
				drafted = append(drafted, t)
			}
		case sendable(t):
			local = append(local, t)
		}
	}
	var rows []actionRow
	if len(local) > 0 {
		label := i18n.T("Send to GitHub")
		if local[0].sync == model.SyncFailed {
			label = i18n.T("Retry sending to GitHub")
		}
		rows = append(rows, actionRow{id: "note-send", label: label, run: func(m Model) (tea.Model, tea.Cmd) {
			return m.withNoteTargetIn(local, func(m Model, t noteTarget) (tea.Model, tea.Cmd) {
				return m.forgeSendCmd(noteSendRequest(pr, t), "")
			})
		}})
		label = i18n.T("Send my draft review…")
		if local[0].group != domain.GroupMine {
			label = i18n.T("Send this AI review…")
		}
		rows = append(rows, actionRow{id: "note-send-review", label: label, run: func(m Model) (tea.Model, tea.Cmd) {
			return m.withNoteTargetIn(local, func(m Model, t noteTarget) (tea.Model, tea.Cmd) {
				return m.openSendReviewBody(pr, t.group)
			})
		}})
	}
	if len(threads) > 0 {
		rows = append(rows, actionRow{id: "note-reply-send", label: i18n.T("Reply & send…"), run: func(m Model) (tea.Model, tea.Cmd) {
			return m.withNoteTargetIn(threads, func(m Model, t noteTarget) (tea.Model, tea.Cmd) {
				nm, cmd := m.openNotePopupFor(noteReply, t)
				if p := layerOf[*notePopup](nm.(Model)); p != nil {
					p.sendPR = pr
				}
				return nm, cmd
			})
		}})
	}
	if len(drafted) > 0 {
		label := i18n.T("Send draft reply")
		if n := len(drafted[0].drafts); n > 1 {
			label = i18n.T("Send %d draft replies", n)
		}
		rows = append(rows, actionRow{id: "note-send-drafts", label: label, run: func(m Model) (tea.Model, tea.Cmd) {
			return m.withNoteTargetIn(drafted, func(m Model, t noteTarget) (tea.Model, tea.Cmd) {
				return m.forgeSendCmd(domain.PRSendRequest{PR: pr, Notes: t.drafts}, "")
			})
		}})
	}
	return rows
}
```

`internal/tui/note_popup.go`: `notePopup` gains `sendPR int // Reply & send: the PR the reply goes to once saved`; the reply refusal in `openNotePopup` is `if len(ts) == 0 && len(all) > 0` with `replyableNoteTargets(all, m.prOfDiff() > 0)`; `noteSubmitCmd` captures `sendPR := p.sendPR` and, for `noteReply`, `d, err := svc.NoteReply(ctx, id, n)` → `return noteMutatedMsg{err: err, clearMarks: ranged, sendPR: sendPR, sendID: d.ID}`; the heading for a reply-and-send popup is `i18n.T("Reply & send")`.

`internal/tui/note_keys.go`: `noteMutatedMsg` gains `sendPR int; sendID string // Reply & send: send this draft once saved`. `model.go` `case noteMutatedMsg:` — after the reload command is built, on success:

```go
		if msg.sendPR != 0 && msg.sendID != "" {
			var send tea.Cmd
			m, send = m.forgeSendCmd(domain.PRSendRequest{PR: msg.sendPR, Notes: []string{msg.sendID}}, "")
			return m, tea.Batch(counts, send)
		}
```

Bundles (all four): `Send to GitHub`, `Retry sending to GitHub`, `Send my draft review…`, `Send this AI review…`, `Reply & send…`, `Reply & send`, `Send draft reply`, `Send %d draft replies`.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/tui/ -run 'PRNoteMenu|RAndX|ReplyAndSend|ForgeNotes|Note|I18n|ActionMenuLabels' 2>&1 | tail -20`
Expected: PASS — including the unchanged `TestForgeNotesAreReadOnlyInTheDiff` (outside a PR).

- [ ] **Step 5: Commit**

```bash
gg add internal/tui internal/i18n/lang
git commit -F <msgfile>   # "feat(tui): send, reply, reply & send, resolve from a note's . menu in a PR's diff"
```

---

### Task 9: TUI — the "updated" freshness mark

**Files:**
- Modify: `internal/tui/pr_revalidate.go` (`prUpdated` set/cleared; `prFreshnessSuffix`)
- Modify: `internal/tui/model.go` (`prUpdated bool` beside `prOfflineSince`; cleared wherever `prOfflineSince` is reset on a repo switch or a new PR)
- Test: `internal/tui/pr_freshness_test.go`

**Interfaces:**
- Produces: the title tail `· updated` after a refresh that found new comments or new commits; it stays until a refresh finds nothing new (spec §2.4: "nothing changed → the freshness mark clears").

- [ ] **Step 1: Write the failing test**

`internal/tui/pr_freshness_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestFreshnessSaysUpdatedUntilAQuietRefresh(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	pr := model.PullRequest{Number: 7, HeadSHA: m.previewOpen.srcHash}
	nm, _ := m.Update(prRevalidatedMsg{n: 7, gen: m.prsGen, pr: pr, commentsChanged: true})
	m = nm.(Model)
	if got := m.prFreshnessSuffix(); !strings.Contains(got, "updated") {
		t.Fatalf("after new comments: %q", got)
	}
	nm, _ = m.Update(prRevalidatedMsg{n: 7, gen: m.prsGen, pr: pr})
	m = nm.(Model)
	if got := m.prFreshnessSuffix(); got != "" {
		t.Fatalf("after a quiet refresh: %q", got)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/tui/ -run FreshnessSaysUpdated 2>&1 | tail -10`
Expected: FAIL — `after new comments: ""`.

- [ ] **Step 3: Implement**

`handlePRRevalidatedMsg`: on `msg.err == nil` (after the gen check): `m.prUpdated = msg.commentsChanged || msg.moved` (the "moved" judged against the head on screen too — compute after `moved` is final and OR it in). `prFreshnessSuffix` gains a last case:

```go
	case m.prUpdated:
		return " · " + i18n.T("updated")
```

(after `refreshing…` and `offline`). Clear `m.prUpdated` where a different PR opens and on `reRoot` (beside `prOfflineSince`). Bundles: `updated` (ja `更新あり`, ko `업데이트됨`, zh `已更新`, ru `обновлено`) — if the key already exists, reuse it.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/tui/ -run 'Freshness|PRRefresh|PRRevalidate|MovedHead|I18n' 2>&1 | tail -10`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/tui internal/i18n/lang
git commit -F <msgfile>   # "feat(tui): the PR view says updated after a refresh that found something"
```

---

### Task 10: TUI — an agent's queued sends in the notice centre

**Files:**
- Create: `internal/tui/pending_sends.go`
- Modify: `internal/tui/notify.go` (`noticeAction.sourced`; `rebuildNotices` appends `pendingSendNotices(m)`)
- Modify: `internal/tui/notice_popup.go` (a `sourced` action closes the dialog and leaves the notice to its source)
- Modify: `internal/tui/model.go` (fields; `Init` and `reRoot` batches; `heartbeatMsg`; the new msgs)
- Modify: `internal/tui/forge_send.go` (the real `pendingFinishCmd` replaces Task 6's stub)
- Test: `internal/tui/pending_sends_test.go`, `internal/tui/pr_send_serial_test.go`

**Interfaces:**
- Consumes: `domain.PendingSends`, `PendingSendFinish`, `PendingSendsPath`, `PendingOutcome`, `PendingSend`, `PendingRejected` (plan 2 + Task 3); `forgeSendCmd` (Task 6); `filewatch.New/Set/Events/Close`; `statDisk`/`diskStat` (`open_files_watch.go`).
- Produces: `pendingSendsMsg{gen int; list []domain.PendingSend; stat diskStat}`; `func pendingSendNoticeID(id string) string` (`"pending_send_" + id`); `noticeAction.sourced bool`.

Behaviour (spec §3.7, T6): every WAITING entry of this repository's queue is one notice — "claude wants to send 4 notes to #7" — with the age, the verdict the agent asked for, and "Nothing is posted until you confirm." Actions: **Review and send…** (plans the entry's request for ITS PR and runs the same confirm; the agent's verdict preselected), **Reject**, **Later**. None of them dismisses the notice: it leaves when the queue says the entry is no longer waiting. The queue file is watched (`filewatch`, a wake-up) and stat-polled on the heartbeat every 2 s (the truth — a missed or unsupported fsnotify event costs at most 2 s). A plan error (PR not fetched, head moved) leaves the entry waiting; only the op's outcome finishes it (`PendingOutcome`).

- [ ] **Step 1: Write the failing tests**

`internal/tui/pending_sends_test.go`:

```go
package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

func waitingSend(id string, req domain.PRSendRequest) domain.PendingSend {
	return domain.PendingSend{ID: id, Requester: "claude", Request: req, Created: time.Now().Add(-2 * time.Minute), State: domain.PendingWaiting}
}

func noticeByID(m Model, id string) *notice {
	for i := range m.notices {
		if m.notices[i].id == id {
			return &m.notices[i]
		}
	}
	return nil
}

func TestPendingSendsBecomeNotices(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	list := []domain.PendingSend{
		waitingSend("p1", domain.PRSendRequest{PR: 7, Notes: []string{"a", "b", "c", "d"}}),
		waitingSend("p2", domain.PRSendRequest{PR: 7, Review: "r1", Event: "approve"}),
	}
	nm, cmd := m.Update(pendingSendsMsg{gen: m.noticeGen, list: list})
	m = nm.(Model)
	n := noticeByID(m, pendingSendNoticeID("p1"))
	if n == nil || n.title != "claude wants to send 4 notes to #7" {
		t.Fatalf("notice = %+v", n)
	}
	n2 := noticeByID(m, pendingSendNoticeID("p2"))
	if n2 == nil || !strings.Contains(strings.Join(n2.detail, "\n"), "approve") {
		t.Fatalf("the asked verdict is shown: %+v", n2)
	}
	if !m.noticesUnread || cmd == nil {
		t.Fatal("a new queued send blinks the notice segment")
	}
	// The entry left the queue: the notice goes.
	nm, _ = m.Update(pendingSendsMsg{gen: m.noticeGen, list: list[1:]})
	if noticeByID(nm.(Model), pendingSendNoticeID("p1")) != nil {
		t.Fatal("a finished entry keeps its notice")
	}
}

func TestPendingSendsFromAnOldRepoAreDropped(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	nm, _ := m.Update(pendingSendsMsg{gen: m.noticeGen - 1, list: []domain.PendingSend{waitingSend("p1", domain.PRSendRequest{PR: 7, Mine: true})}})
	if len(nm.(Model).pendingSends) != 0 {
		t.Fatal("a read from before a repo switch must be dropped")
	}
}

// T6: no action dismisses the notice — the queue does.
func TestPendingNoticeActionsLeaveTheNoticeToTheQueue(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	nm, _ := m.Update(pendingSendsMsg{gen: m.noticeGen, list: []domain.PendingSend{waitingSend("p1", domain.PRSendRequest{PR: 7, Mine: true})}})
	m = nm.(Model)
	n := *noticeByID(m, pendingSendNoticeID("p1"))
	later := n.actions[len(n.actions)-1]
	if !later.sourced || later.run != nil {
		t.Fatalf("Later = %+v", later)
	}
	m, _ = m.applyNoticeAction(n, later)
	if noticeByID(m, pendingSendNoticeID("p1")) == nil || m.noticeSessionDismissed[n.id] {
		t.Fatal("Later must not dismiss a queued send")
	}
}

// Review Focus 2: approving while another op runs says so and sends nothing.
func TestApproveWhileAnOpRunsKeepsTheEntryWaiting(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	nm, _ := m.Update(pendingSendsMsg{gen: m.noticeGen, list: []domain.PendingSend{waitingSend("p1", domain.PRSendRequest{PR: 7, Mine: true})}})
	m = nm.(Model)
	m.running = true
	n := *noticeByID(m, pendingSendNoticeID("p1"))
	m, cmd := m.applyNoticeAction(n, n.actions[0])
	if cmd != nil || !strings.Contains(m.statusMsg, "another operation") || noticeByID(m, n.id) == nil {
		t.Fatalf("status %q cmd %v", m.statusMsg, cmd != nil)
	}
}

// Reject writes the outcome; the re-read drops the notice.
func TestRejectAnswersTheAgent(t *testing.T) {
	t.Parallel()
	dir, _ := newRepoDir(t)
	m := New(domain.OpenTUI(dir))
	ctx := context.Background()
	e, err := m.svc.PendingSendAdd(ctx, domain.PRSendRequest{PR: 7, Mine: true}, "claude")
	if err != nil {
		t.Fatal(err)
	}
	m = drainCmd(t, m, m.pendingSendsReadCmd(m.noticeGen))
	n := noticeByID(m, pendingSendNoticeID(e.ID))
	if n == nil {
		t.Fatal("no notice for the queued send")
	}
	reject := n.actions[1]
	m, cmd := m.applyNoticeAction(*n, reject)
	m = drainCmd(t, m, cmd)
	got, _ := m.svc.PendingSendGet(ctx, e.ID)
	if got.State != domain.PendingRejected {
		t.Fatalf("state %q", got.State)
	}
	if noticeByID(m, n.id) != nil {
		t.Fatal("the rejected entry's notice must go")
	}
}
```

(`newTestModel` / `drainCmd` are the package's helpers: `newTestModel` has a real repo and service; `drainCmd` feeds a command's messages through `Update`. If `newTestModel`'s `noticeGen` is 0, the "old repo" test uses `m.noticeGen = 5` first and sends `gen: 4`.)

Append to `internal/tui/pr_send_serial_test.go`:

```go
// Serial: env. An agent's queued send, approved in the TUI: the confirm
// preselects the agent's wish, the send goes, the agent's entry says sent.
func TestApprovedPendingSendIsSentAndAnswered(t *testing.T) {
	m, dir, head := prSendModel(t)
	id := addTUINote(t, m, head, 5, "look here")
	ctx := context.Background()
	e, err := m.svc.PendingSendAdd(ctx, domain.PRSendRequest{PR: 7, Notes: []string{id}}, "claude")
	if err != nil {
		t.Fatal(err)
	}
	m = drainCmd(t, m, m.pendingSendsReadCmd(m.noticeGen))
	n := noticeByID(m, pendingSendNoticeID(e.ID))
	m, cmd := m.applyNoticeAction(*n, n.actions[0])
	m, wait := runToModal(t, m, cmd)
	nm, _ := m.resolveModal("send")
	m, tail := driveOpKeepCmd(t, nm.(Model), wait)
	m = drainCmd(t, m, tail) // pendingFinishCmd + the re-read
	got, _ := m.svc.PendingSendGet(ctx, e.ID)
	if got.State != domain.PendingSent || len(forgetest.Writes(t, filepath.Join(dir, ".git", "fakegh"))) != 3 {
		t.Fatalf("entry %+v", got)
	}
	if noticeByID(m, n.id) != nil {
		t.Fatal("a sent entry keeps its notice")
	}
}
```

(`driveOpKeepCmd` exists in `internal/tui` tests: it drives the op and returns the command left after it finished; if its shape differs, use `driveOp` and collect the follow-up from the `opFinishedMsg` handler's returned command.)

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tui/ -run 'PendingSends|PendingNotice|ApproveWhile|RejectAnswers|ApprovedPendingSend' 2>&1 | tail -20`
Expected: FAIL — undefined `pendingSendsMsg`, `pendingSendNoticeID`.

- [ ] **Step 3: Implement**

`internal/tui/notify.go`: `noticeAction` gains

```go
	// sourced: the notice's own source (a queued or interrupted send) decides
	// when it goes; the action closes the dialog and never dismisses it.
	sourced bool
```

`applyNoticeAction`: `if !act.keep && !act.sourced { …remove + session-dismiss… }`. `notice_popup.go` is unchanged (`!act.keep` already closes the dialog). In `rebuildNotices`, after the steer-ask notice: `next = append(next, pendingSendNotices(m)...)` (no dismissal filter: the queue is the truth).

`internal/tui/pending_sends.go`:

```go
package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/clock"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/filewatch"
	"github.com/homeend/gigagit/internal/i18n"
)

// An agent's send waits in this repository's queue until the user answers it
// here (spec 2026-10-07 §3.7). The queue file is watched (a wake-up) and
// stat-polled on the heartbeat (the truth); every waiting entry is a notice.

const pendingPollEvery = 2 * time.Second

// pendingWatchState is the queue watcher on Model (pointer fields survive
// the value copy through the model).
type pendingWatchState struct {
	path     string
	w        *filewatch.Watcher
	stat     diskStat
	lastPoll time.Time
	polling  bool
}

type pendingSendsMsg struct {
	gen  int
	list []domain.PendingSend
	stat diskStat
}

type pendingStatMsg struct {
	gen  int
	path string
	stat diskStat
}

type pendingWatchMsg struct {
	gen  int
	path string
	w    *filewatch.Watcher // nil: no watcher here (poll only)
}

type pendingWakeMsg struct{ gen int }

func pendingSendNoticeID(id string) string { return "pending_send_" + id }

// pendingSendsReadCmd reads the queue (gen = m.noticeGen when asked).
func (m Model) pendingSendsReadCmd(gen int) tea.Cmd {
	svc := m.svc
	if svc == nil {
		return nil
	}
	return func() tea.Msg {
		ctx := context.Background()
		p, _ := svc.PendingSendsPath(ctx)
		st := statDisk(p)
		list, err := svc.PendingSends(ctx)
		if err != nil {
			return nil // best-effort, like every notice source
		}
		return pendingSendsMsg{gen: gen, list: list, stat: st}
	}
}

// pendingWatchCmd finds the queue file and, outside headless runs, watches it.
func (m Model) pendingWatchCmd(gen int) tea.Cmd {
	svc, quiet := m.svc, m.quiet
	if svc == nil {
		return nil
	}
	return func() tea.Msg {
		p, err := svc.PendingSendsPath(context.Background())
		if err != nil || p == "" {
			return nil
		}
		msg := pendingWatchMsg{gen: gen, path: p}
		if !quiet { // a never-ending listen must not run under Headless
			if w, err := filewatch.New(200 * time.Millisecond); err == nil {
				w.Set([]string{p})
				msg.w = w
			}
		}
		return msg
	}
}

func pendingListenCmd(w *filewatch.Watcher, gen int) tea.Cmd {
	return func() tea.Msg {
		if _, ok := <-w.Events(); !ok {
			return nil
		}
		return pendingWakeMsg{gen: gen}
	}
}

// pendingSendsTick is the heartbeat's stat poll.
func (m Model) pendingSendsTick(now time.Time) (Model, tea.Cmd) {
	pw := m.pendingWatch
	if pw == nil || pw.path == "" || pw.polling || now.Sub(pw.lastPoll) < pendingPollEvery {
		return m, nil
	}
	pw.polling, pw.lastPoll = true, now
	gen, path := m.noticeGen, pw.path
	return m, func() tea.Msg { return pendingStatMsg{gen: gen, path: path, stat: statDisk(path)} }
}

func (m Model) handlePendingWatch(msg pendingWatchMsg) (Model, tea.Cmd) {
	if msg.gen != m.noticeGen {
		if msg.w != nil {
			msg.w.Close()
		}
		return m, nil
	}
	if m.pendingWatch != nil && m.pendingWatch.w != nil {
		m.pendingWatch.w.Close()
	}
	m.pendingWatch = &pendingWatchState{path: msg.path, w: msg.w}
	if msg.w == nil {
		return m, nil
	}
	return m, pendingListenCmd(msg.w, msg.gen)
}

func (m Model) handlePendingStat(msg pendingStatMsg) (Model, tea.Cmd) {
	pw := m.pendingWatch
	if msg.gen != m.noticeGen || pw == nil {
		return m, nil
	}
	pw.polling = false
	if pw.stat.known && pw.stat.same(msg.stat) {
		return m, nil
	}
	return m, m.pendingSendsReadCmd(msg.gen)
}

func (m Model) handlePendingSends(msg pendingSendsMsg) (Model, tea.Cmd) {
	if msg.gen != m.noticeGen {
		return m, nil
	}
	if m.pendingWatch != nil {
		m.pendingWatch.stat = msg.stat
	}
	var waiting []domain.PendingSend
	for _, e := range msg.list {
		if e.State == domain.PendingWaiting {
			waiting = append(waiting, e)
		}
	}
	prev := m.noticeIDs()
	m.pendingSends = waiting
	m = m.rebuildNotices()
	return m.armBlinkForNew(prev)
}

// pendingSendNotices are the waiting entries as notices.
func pendingSendNotices(m Model) []notice {
	var out []notice
	for _, e := range m.pendingSends {
		e := e
		detail := []string{i18n.T("asked %s", ageString(clock.Now(), e.Created))}
		if e.Request.Event != "" {
			detail = append(detail, i18n.T("asks for: %s", optionDisplayName(e.Request.Event)))
		}
		detail = append(detail, i18n.T("Nothing is posted until you confirm."))
		out = append(out, notice{
			id: pendingSendNoticeID(e.ID), repoKey: m.repoHealth.GitCommonDir,
			title: pendingSendTitle(e), detail: detail,
			actions: []noticeAction{
				{label: i18n.T("Review and send…"), sourced: true, run: func(m Model) (Model, tea.Cmd) {
					return m.forgeSendCmd(e.Request, e.ID)
				}},
				{label: i18n.T("Reject"), sourced: true, run: func(m Model) (Model, tea.Cmd) {
					return m, m.pendingRejectCmd(e.ID)
				}},
				{label: i18n.T("Later"), sourced: true},
			},
		})
	}
	return out
}

// pendingSendTitle says who wants what sent where — one literal per shape.
func pendingSendTitle(e domain.PendingSend) string {
	r, who := e.Request, e.Requester
	switch {
	case r.Finish:
		return i18n.T("%s wants to finish the pending review on #%d", who, r.PR)
	case r.Discard:
		return i18n.T("%s wants to discard the pending review on #%d", who, r.PR)
	case r.Review != "":
		return i18n.T("%s wants to send an AI review to #%d", who, r.PR)
	case r.Mine:
		return i18n.T("%s wants to send the draft review to #%d", who, r.PR)
	case len(r.Resolve) == 1:
		return i18n.T("%s wants to resolve 1 thread on #%d", who, r.PR)
	case len(r.Resolve) > 1:
		return i18n.T("%s wants to resolve %d threads on #%d", who, len(r.Resolve), r.PR)
	case len(r.Unresolve) == 1:
		return i18n.T("%s wants to reopen 1 thread on #%d", who, r.PR)
	case len(r.Unresolve) > 1:
		return i18n.T("%s wants to reopen %d threads on #%d", who, len(r.Unresolve), r.PR)
	case len(r.Notes) == 1:
		return i18n.T("%s wants to send 1 note to #%d", who, r.PR)
	case len(r.Notes) > 1:
		return i18n.T("%s wants to send %d notes to #%d", who, len(r.Notes), r.PR)
	}
	return i18n.T("%s wants to post a verdict on #%d", who, r.PR)
}

// pendingRejectCmd answers the agent "rejected" and re-reads the queue.
func (m Model) pendingRejectCmd(id string) tea.Cmd {
	svc, gen := m.svc, m.noticeGen
	return func() tea.Msg {
		ctx := context.Background()
		_, _ = svc.PendingSendFinish(ctx, id, domain.PendingRejected, "rejected in gg")
		list, _ := svc.PendingSends(ctx)
		return pendingSendsMsg{gen: gen, list: list}
	}
}

// pendingFinishCmd writes an approved send's outcome back (sent, rejected at
// the confirm, failed) and re-reads the queue; an approver who could not
// answer leaves it waiting.
func (m Model) pendingFinishCmd(id string, res engine.Result, err error) tea.Cmd {
	svc, gen := m.svc, m.noticeGen
	return func() tea.Msg {
		ctx := context.Background()
		if st, out, waiting := domain.PendingOutcome(res, err); !waiting {
			_, _ = svc.PendingSendFinish(ctx, id, st, out)
		}
		list, _ := svc.PendingSends(ctx)
		return pendingSendsMsg{gen: gen, list: list}
	}
}
```

Delete Task 6's stub `pendingFinishCmd` from `forge_send.go`.

`internal/tui/model.go`:
- fields `pendingSends []domain.PendingSend` and `pendingWatch *pendingWatchState` (beside `driftNotices`).
- `Init`'s batch: `m.pendingWatchCmd(m.noticeGen), m.pendingSendsReadCmd(m.noticeGen)`; `reRoot`: close `m.pendingWatch.w` if set, `m.pendingWatch, m.pendingSends = nil, nil` before `noticeGen` is bumped, and add the same two commands (with the new gen) to its batch (line ~4992).
- cases: `pendingSendsMsg` → `handlePendingSends`; `pendingStatMsg` → `handlePendingStat`; `pendingWatchMsg` → `handlePendingWatch`; `pendingWakeMsg` → `if msg.gen != m.noticeGen { return m, nil }; return m, tea.Batch(m.pendingSendsReadCmd(msg.gen), pendingListenCmd(m.pendingWatch.w, msg.gen))` (guard `m.pendingWatch != nil && m.pendingWatch.w != nil`).
- `heartbeatMsg` (the non-quiet branch): `var pcmd tea.Cmd; m, pcmd = m.pendingSendsTick(time.Now())` added to the batch.
- program exit: close `m.pendingWatch.w` where the steer watcher is closed on quit (grep `steerWatch.Close`).

Bundles (all four): `asked %s`, `asks for: %s`, `Nothing is posted until you confirm.`, `Review and send…`, `Reject`, `Later`, and the eleven `%s wants to …` titles. Translations keep `%s` before `#%d` or reorder with `%[1]s`/`%[2]d` explicitly.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/tui/ -run 'Pending|Notice|ApproveWhile|RejectAnswers|ApprovedPendingSend|I18n|CheckVerbs' 2>&1 | tail -20`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/tui internal/i18n/lang
git commit -F <msgfile>   # "feat(tui): an agent's queued sends in the notice centre — review and send, reject, later"
```

---

### Task 11: TUI — interrupted sends in the notice centre

**Files:**
- Create: `internal/tui/interrupted_sends.go`
- Modify: `internal/tui/pr_revalidate.go` (after a successful refresh, ask whether that PR has an interrupted send)
- Modify: `internal/tui/notify.go` (`rebuildNotices` appends `interruptedSendNotices(m)`)
- Modify: `internal/tui/model.go` (`interrupted map[int]interruptedSend`; `case interruptedMsg`; cleared in `reRoot`)
- Test: `internal/tui/interrupted_sends_test.go`

**Interfaces:**
- Consumes: `domain.PRInterrupted(ctx, n) (review string, keys []string, joined bool)`; `forgeSendCmd`; `noticeAction.sourced` (Task 10).
- Produces: `interruptedMsg{gen, pr int; review string; keys []string; joined bool}`; notice id `"interrupted_send_<n>"`.

Behaviour (spec §3.4, T8): after every successful refresh of PR n, `PRInterrupted(n)` (gate-free, reads the cached PR) — a non-empty answer is a notice "A send to #7 was interrupted: 3 comments wait in a pending review on GitHub" with **Finish sending** (`{PR: n, Finish: true}` → the same confirm), **Discard** (`{PR: n, Discard: true}`; absent when gg joined the user's own pending review — `joined`), **Later**. An empty answer removes PR n's notice.

- [ ] **Step 1: Write the failing tests**

`internal/tui/interrupted_sends_test.go`:

```go
package tui

import (
	"strings"
	"testing"
)

func TestInterruptedSendBecomesANotice(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	nm, _ := m.Update(interruptedMsg{gen: m.noticeGen, pr: 7, review: "PRR_1", keys: []string{"n1", "n2", "n3"}})
	m = nm.(Model)
	n := noticeByID(m, "interrupted_send_7")
	if n == nil || !strings.Contains(n.title, "#7") || !strings.Contains(n.title, "3") {
		t.Fatalf("notice = %+v", n)
	}
	var labels []string
	for _, a := range n.actions {
		labels = append(labels, a.label)
		if !a.sourced {
			t.Errorf("%q dismisses the notice", a.label)
		}
	}
	if strings.Join(labels, ",") != "Finish sending,Discard,Later" {
		t.Fatalf("actions %v", labels)
	}
	// Joined the user's own pending review: never discard it.
	nm, _ = m.Update(interruptedMsg{gen: m.noticeGen, pr: 7, review: "PRR_1", keys: []string{"n1"}, joined: true})
	for _, a := range noticeByID(nm.(Model), "interrupted_send_7").actions {
		if a.label == "Discard" {
			t.Fatal("Discard offered on the user's own pending review")
		}
	}
	// Settled: the notice goes.
	nm, _ = nm.(Model).Update(interruptedMsg{gen: m.noticeGen, pr: 7})
	if noticeByID(nm.(Model), "interrupted_send_7") != nil {
		t.Fatal("a settled send keeps its notice")
	}
}

func TestARefreshAsksForInterruptedSends(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	pr := m.prs[0]
	pr.HeadSHA = m.previewOpen.srcHash
	_, cmd := m.Update(prRevalidatedMsg{n: 7, gen: m.prsGen, pr: pr})
	found := false
	for _, msg := range flattenCmd(t, cmd) {
		if _, ok := msg.(interruptedMsg); ok {
			found = true
		}
	}
	if !found {
		t.Fatal("a successful refresh asks whether a send was interrupted")
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tui/ -run 'InterruptedSend|ARefreshAsks' 2>&1 | tail -10`
Expected: FAIL — undefined `interruptedMsg`.

- [ ] **Step 3: Implement**

`internal/tui/interrupted_sends.go`:

```go
package tui

import (
	"context"
	"sort"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// An interrupted send (spec §3.4): gg stopped after GitHub opened its pending
// review. Each refresh of a PR asks; the notice offers to finish or discard.

type interruptedSend struct {
	review string
	keys   []string
	joined bool
}

type interruptedMsg struct {
	gen    int
	pr     int
	review string
	keys   []string
	joined bool
}

func (m Model) interruptedCmd(pr int) tea.Cmd {
	svc, gen := m.svc, m.noticeGen
	if svc == nil || pr == 0 {
		return nil
	}
	return func() tea.Msg {
		rev, keys, joined := svc.PRInterrupted(context.Background(), pr)
		return interruptedMsg{gen: gen, pr: pr, review: rev, keys: keys, joined: joined}
	}
}

func (m Model) handleInterrupted(msg interruptedMsg) (Model, tea.Cmd) {
	if msg.gen != m.noticeGen {
		return m, nil
	}
	prev := m.noticeIDs()
	if msg.review == "" {
		delete(m.interrupted, msg.pr)
	} else {
		if m.interrupted == nil {
			m.interrupted = map[int]interruptedSend{}
		}
		m.interrupted[msg.pr] = interruptedSend{review: msg.review, keys: msg.keys, joined: msg.joined}
	}
	m = m.rebuildNotices()
	return m.armBlinkForNew(prev)
}

func interruptedSendNotices(m Model) []notice {
	prs := make([]int, 0, len(m.interrupted))
	for n := range m.interrupted {
		prs = append(prs, n)
	}
	sort.Ints(prs)
	var out []notice
	for _, n := range prs {
		n, s := n, m.interrupted[n]
		title := i18n.T("A send to #%d was interrupted: 1 comment waits in a pending review on GitHub", n)
		if len(s.keys) != 1 {
			title = i18n.T("A send to #%d was interrupted: %d comments wait in a pending review on GitHub", n, len(s.keys))
		}
		acts := []noticeAction{{label: i18n.T("Finish sending"), sourced: true, run: func(m Model) (Model, tea.Cmd) {
			return m.forgeSendCmd(domain.PRSendRequest{PR: n, Finish: true}, "")
		}}}
		if !s.joined {
			acts = append(acts, noticeAction{label: i18n.T("Discard"), sourced: true, run: func(m Model) (Model, tea.Cmd) {
				return m.forgeSendCmd(domain.PRSendRequest{PR: n, Discard: true}, "")
			}})
		}
		acts = append(acts, noticeAction{label: i18n.T("Later"), sourced: true})
		out = append(out, notice{id: "interrupted_send_" + strconv.Itoa(n), repoKey: m.repoHealth.GitCommonDir,
			title: title, detail: []string{i18n.T("Nothing else of it is visible on GitHub until it is submitted.")}, actions: acts})
	}
	return out
}
```

`pr_revalidate.go` `handlePRRevalidatedMsg`: on `msg.err == nil`, batch `m.interruptedCmd(msg.n)` into the returned command (on every return path after the error check). `notify.go` `rebuildNotices`: `next = append(next, interruptedSendNotices(m)...)` after the pending ones. `model.go`: `interrupted map[int]interruptedSend` on Model; `case interruptedMsg: return m.handleInterrupted(msg)`; `reRoot` sets `m.interrupted = nil`. After a finish/discard op, Task 6's follow-up refresh re-asks and the notice goes.

Bundles (all four): the two `A send to #%d was interrupted…` titles, `Finish sending`, `Discard` (reuse if it exists), `Nothing else of it is visible on GitHub until it is submitted.`

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/tui/ -run 'Interrupted|ARefreshAsks|PRRefresh|PRRevalidate|Notice|I18n' 2>&1 | tail -10`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/tui internal/i18n/lang
git commit -F <msgfile>   # "feat(tui): an interrupted send in the notice centre — finish or discard"
```

---

### Task 12: help, footers, and golden screens of a send from the PR view

**Files:**
- Modify: `internal/tui/help.go` (the Pull requests section, lines ~140–148)
- Modify: `e2e/scenario.go` (`Step.Ref`; `kind()`), `e2e/builder.go` (the `ref` action; `{{rev:<name>}}` in written content)
- Create: `e2e/scenarios/tui_pr_send.toml` (+ its `.screens/` directory, written by `-update`)
- Test: `internal/tui/help_test.go` (or the file holding help-row tests), `e2e/builder_test.go`

**Interfaces:**
- Consumes: everything above.
- Produces: `Step{Ref: "refs/gg/pr/7", Value: "feat"}` (`git update-ref <ref> <value>`); `{{rev:<name>}}` in a `write` step's content (expands to `git rev-parse <name>` in the repo being built).

- [ ] **Step 1: Write the failing tests**

`e2e/builder_test.go` — append (match the file's existing sandbox construction; `TestBuildLocalRepo` shows it):

```go
func TestBuildRefStepAndRevExpansion(t *testing.T) {
	sb := newTestSandbox(t, // the helper TestBuildLocalRepo uses
		[]Step{{Write: "a.txt", Content: "v1\n"}, {Commit: "initial"}, {Branch: "feat"},
			{Ref: "refs/gg/pr/7", Value: "feat"},
			{Write: ".git/fakegh/head.txt", Content: "head={{rev:feat}}"}})
	head := strings.TrimSpace(sb.git(t, sb.LocalDir, "rev-parse", "feat"))
	if got := strings.TrimSpace(sb.git(t, sb.LocalDir, "rev-parse", "refs/gg/pr/7")); got != head {
		t.Fatalf("ref = %s, want %s", got, head)
	}
	b, _ := os.ReadFile(filepath.Join(sb.LocalDir, ".git", "fakegh", "head.txt"))
	if string(b) != "head="+head {
		t.Fatalf("expanded %q", b)
	}
}
```

Help — append to the help tests (`grep -ln 'func Test.*Help' internal/tui/*_test.go`):

```go
func TestHelpDescribesSendingFromThePRView(t *testing.T) {
	t.Parallel()
	text := plain(strings.Join(helpLines(newTestModel(t)), "\n")) // the helper the help tests already use
	for _, want := range []string{"Send to GitHub", "Reply & send", "Send review…", "Verdict…", "Review and send…", "Finish sending", "updated"} {
		if !strings.Contains(text, want) {
			t.Errorf("help lacks %q", want)
		}
	}
	if strings.Contains(text, "gg never posts to the forge") {
		t.Error("help still says gg never posts")
	}
}
```

(adapt `helpLines` to the name the help tests use to render the help text.)

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./e2e/ -run TestBuildRefStep 2>&1 | tail -5; go test ./internal/tui/ -run HelpDescribesSending 2>&1 | tail -5`
Expected: FAIL — unknown field `Ref`; help lacks the rows.

- [ ] **Step 3: Implement**

`e2e/scenario.go`: `Step` gains `Ref string \`toml:"ref"\` // a ref name; Value holds the rev it points at (git update-ref)`; `kind()` adds `if s.Ref != "" { kinds = append(kinds, "ref") }` and the `value` check becomes `if s.Value != "" && k != "git_config" && k != "ref"`.

`e2e/builder.go`: in the step switch

```go
		case "ref":
			b.git(t, dir, "update-ref", st.Ref, st.Value)
```

and the `write` case writes `expandRevs(t, b, dir, ExpandText(st.Content))`:

```go
// expandRevs replaces {{rev:<name>}} with the commit <name> names in the repo
// being built — a fixture (a fake gh answer) can then carry a real head sha.
func expandRevs(t *testing.T, b *Sandbox, dir, s string) string {
	t.Helper()
	for {
		i := strings.Index(s, "{{rev:")
		if i < 0 {
			return s
		}
		j := strings.Index(s[i:], "}}")
		if j < 0 {
			return s
		}
		name := s[i+len("{{rev:") : i+j]
		sha := strings.TrimSpace(b.git(t, dir, "rev-parse", name))
		s = s[:i] + sha + s[i+j+2:]
	}
}
```

`internal/tui/help.go` — the Pull requests section: line 141's "read-only — gg never posts to the forge" becomes "gg writes to the forge only when you send something (below)"; line 147 drops "read-only"; then add:

```go
		r("", i18n.T("inside a pull request's diff every note box says where it lives — ○ only here, ◌ being sent, ○! the last send failed (the error is in the box), ● on GitHub — and wears its group's colour bar: my draft review, each AI review of the pull request's commits, each GitHub review (a sent group keeps its colour); a note carried from another commit or the working tree says where it is from; the file list's badges show the same bars; the title says refreshing…, updated or offline")),
		r(".", i18n.T("on a note in a pull request's diff: Send to GitHub (Retry after a failure), Send my draft review… / Send this AI review…; on a GitHub thread: Reply to note, Reply & send…, Resolve / Reopen thread (on GitHub at once), Send draft reply; Delete note stays local; every send shows what will be posted and asks first")),
		r("R / x", i18n.T("on a GitHub thread inside a pull request's diff: reply (a local draft until you send it) / resolve or reopen it on GitHub")),
		r("s / v", i18n.T("in the pull request details (and the . menu of its diff): Send review… — pick my draft review or an AI review, edit the body, then the confirm picks comment, approve or request changes — and Verdict… (no comments)")),
		r("!", i18n.T("the notice centre lists an agent's queued sends (Review and send… / Reject / Later — nothing is posted until you confirm) and sends that were interrupted (Finish sending / Discard)")),
```

and translate the changed and new keys in all four bundles (remove the two replaced keys from the bundles: the orphan gate).

`e2e/scenarios/tui_pr_send.toml` (the PR head is `feat`; the fake gh answers from `.git/fakegh`):

```toml
name = "tui: a note in a pull request's diff shows its mark and is sent to GitHub from the . menu"

[input]
steps = [
  { write = "big.txt", content = "line 1\nline 2\nline 3\nline 4\nline 5\nline 6\nline 7\nline 8\nline 9\nline 10\n" },
  { commit = "big" },
  { branch = "feat" }, { switch = "feat" },
  { write = "big.txt", content = "line 1\nline 2\nline 3\nline 4\nline 5 changed\nline 6\nline 7\nline 8\nline 9\nline 10\n" },
  { commit = "change" },
  { switch = "main" },
  { ref = "refs/gg/pr/7", value = "feat" },
  { write = ".git/fakegh/pr-list.json", content = '[{"number":7,"title":"Change line 5","author":{"login":"ann"},"state":"OPEN","isDraft":false,"reviewDecision":"","headRefName":"feat","isCrossRepository":false,"baseRefName":"main","baseRefOid":"","headRefOid":"{{rev:feat}}","url":"https://github.com/o/r/pull/7","createdAt":"2026-01-01T10:00:00Z","updatedAt":"2026-01-01T11:00:00Z"}]' },
  { write = ".git/fakegh/repo-view.json", content = '{"nameWithOwner":"o/r","url":"https://github.com/o/r","sshUrl":"git@github.com:o/r.git"}' },
  # snapshot-7.json / snapshot-7-sent.json / pr-view-7.json: the same JSON as
  # prSendFixtures in internal/tui/pr_send_serial_test.go, with
  # headRefOid = {{rev:feat}} and the path big.txt.
]

[[run]]
cmd  = ["note", "add", "--rev", "feat", "--file", "big.txt", "--new-line", "5", "--summary", "why changed?", "--source", "user"]
exit = 0

[tui]
size = "160x40"

[[tui.step]]
name = "pr-tab"
keys = ["C-right", "C-right", "C-right", "C-right"]
wait = true
screen_contains = ["#7", "Change line 5"]

[[tui.step]]
name = "pr-diff-note-marked"
keys = ["enter", "enter"]
wait = true
screen_contains = ["PR #7", "○ note", "why changed?"]

[[tui.step]]
name = "note-menu"
keys = ["."]
screen_contains = ["Send to GitHub", "Send my draft review…"]

[[tui.step]]
name = "send-confirm"
keys = ["<move to Send to GitHub>", "enter"]
wait = true
screen_contains = ["Send to o/r #7:", "+ big.txt:5 why changed?", "send", "abort"]

[[tui.step]]
name = "sent"
keys = ["enter"]
wait = true
screen_contains = ["● review", "me"]
screen_excludes = ["○ note"]

[expect]
branch = "main"
```

Write the three snapshot fixtures as `write` steps (copy `prSendFixtures`, replacing `big.go` with `big.txt` and `%s` with `{{rev:feat}}`). Then run `go test ./e2e/ -run 'TestScenarios/tui_pr_send' -update` and READ every written screen: fix the keys (`<move to Send to GitHub>` = the `down` presses the menu needs; the tab count to the Pull requests tab; whether the startup forge probe runs under Headless — if it does not, a `[tui]` step that presses `r` first) until each screen shows what its name says. If the PR open tries a network fetch the sandbox cannot serve, give the scenario an `[input] origin` whose `refs/pull/7/head` is `feat` (the http transport serves any ref) and ledger the choice. The fake gh's answer to the post-submit read must be `snapshot-7-sent.json` (as in the CLI test).

- [ ] **Step 4: Run the tests**

Run: `go test ./e2e/ 2>&1 | tail -5; go test ./internal/tui/ -run 'Help|I18n|CheckVerbs|FooterRendersTranslated' 2>&1 | tail -5`
Expected: PASS; the only new golden screens are `tui_pr_send`'s; no other `.screens` file changed (`git status e2e/`).

- [ ] **Step 5: Commit**

```bash
gg add e2e internal/tui internal/i18n/lang
git commit -F <msgfile>   # "feat(tui): help for sending from a PR; golden screens of a send"
```

---

### Task 13: docs, skill, memory

**Files:**
- Modify: `CHANGELOG.md` (a "Sending to GitHub from the terminal UI" section at the top, user-facing prose, no internals)
- Modify: `README.md` (the pull requests part: sending from the TUI; the six `note_group_*` theme roles where theme roles are listed)
- Modify: `docs/CLAUDE-details.md` ("Sending to GitHub": the TUI half — `forgeSendCmd` is the one entry, the TUI confirm renders `op.Plan`, notice sources, T1–T9)
- Modify: `CLAUDE.md` only if a package row changed (the `tui` row: "sends to a forge through `forgeSendCmd`" is one clause — keep the row one line)
- Check: `internal/agentskill/using-gg.md` — no CLI surface changed except `--body` with `--review` now edits the review body (T7): one sentence, bump `agentskill.Version`, regenerate `.claude/skills/using-gg/SKILL.md` the way plan 2 did, `gg init --update` after the merge.

- [ ] **Step 1:** Write the CHANGELOG section: marks and bars, the `.` rows, Send review… / Verdict…, Reply & send, resolve with x, queued agent sends and interrupted sends in `!`, "updated" in the title, AI reviews of a PR's commits now draw in its diff, carried notes counted in badges, `gg pr send --review <id> --body` edits the review body.
- [ ] **Step 2:** README + CLAUDE-details + skill paragraph as listed.
- [ ] **Step 3:** Run: `go test ./internal/agentskill/ ./internal/config/ 2>&1 | tail -5` — PASS (the skill version marker and the theme docs in `gg config populate`).
- [ ] **Step 4:** Commit: `gg add CHANGELOG.md README.md docs/CLAUDE-details.md internal/agentskill .claude/skills/using-gg/SKILL.md` (+ `CLAUDE.md` if touched); `git commit -F <msgfile>` — "docs: sending to GitHub from the TUI (CHANGELOG, README, details, using-gg vN)".
- [ ] **Step 5:** Update memory `github-write-feature.md` (plan 3 state, rulings T1–T9, deferred items) after the final review, not before.

---

### Task 14: the plan-2 follow-up minors (strike at review if they should stay separate — T9)

**Files:** `internal/domain/forge_send.go`, `internal/domain/forge_send_ledger.go`, `internal/domain/review_threads.go`, `internal/cli/prsend.go`, `internal/git/diff_raw.go`, tests beside each.

One TDD cycle per item (test RED with the guard missing → fix → GREEN), one commit for all:

1. **A re-saved review with a new summary is re-posted.** The summary mark (`summaryFP = "summary"`) keys on a constant; key it on the summary text's fingerprint (`remarkFP("", "", [2]int{}, summary)`-style hash of the overview) so a changed summary is "not sent". Test: send a review whose remarks partly fail (summary marked), re-save it with a new overview, plan again → `plan.Body` contains the new overview and no "already on GitHub" skip.
2. **`settleReview`'s remove is atomic.** Remove inside the same `Store.Edit` transaction (or re-check under the remove's lock that no reply was added since the edit); test with `racingStore` (forge_settle_race_test.go) adding a reply between the two calls → the reply survives.
3. **`noteKinds` surfaces a store error.** Return `(drafts, other int, err error)`; `planSend` returns the error. Test: a store that fails `LoadAll` → `planSend` errors (not `ErrMixedSend`).
4. **A typo'd id beside `--resolve`** is an error again (`ErrSendRequest: no local note <id>`), and the skip label of a deleted draft is `reply: <id>` (one shape). Tests in `forge_send_actions_test.go`.
5. **`--event` with `--note`** is refused (`--event needs --review, --mine or --verdict`). CLI test.
6. **The "rerun with --yes" hint** is not printed when the PR has a pending review (where `--yes` is always refused). CLI test on the stderr.
7. **The stale-note test's "stays unstamped" leg** asserts against a SEND, not the plan (send with the fake writer, then read the stale note: still local, no stamp); the summary test adds the duplicate-entry and later-removal legs.
8. **The send diff passes `--no-ext-diff --no-textconv`** (`DiffPatch` when `Unified > 0`); the argv test pins them.

Run: `go test ./internal/domain/ ./internal/cli/ ./internal/git/ 2>&1 | tail -10` — PASS. Commit: `gg add internal/domain internal/cli internal/git && git commit -F <msgfile>` — "fix(forge): plan-2 follow-up minors (summary re-post, atomic settle remove, store errors, --event/--note, hints, diff flags)".

---

## Final steps

- [ ] `./test.sh race > <workspace>/race.log 2>&1`; green ONLY when the log says `all green` (a known flake such as `TestRankPerf` re-runs alone 3/3 and the full gate re-runs).
- [ ] `./build.sh` into the worktree (`<worktree>/bin/gg`) for the user's manual look (absolute path; never attach the binary).
- [ ] Final whole-branch review: one read-only subagent on the most capable model, with this plan, the spec, the ledger's `Ruling:` lines and the Review Focus list.
- [ ] Ask the user before merging (`gg merge -F <msgfile> --into main feat/github-write-3`); after the merge `./build.sh install` and `gg init --update`.

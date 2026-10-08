# GitHub write-back follow-ups 2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (native, this session; NO implementer subagents) to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the ten minors deferred by the PR note-scope branch (`cd28329b`) and the cache-minors branch (`565eae1b`).

**Architecture:** Small, independent fixes in `domain` (note scope, send planner, NoteCounts, listing verdict), `web` (note add, PR open, prs.js reads), `tui` (post-send re-read), `prcache` (Windows retry) and one archived plan's wording. No new package, no new wire field, no new config.

**Tech Stack:** Go 1.26, vanilla JS modules (node-run unit tests), real `git` in temp dirs.

**Spec:** `docs/superpowers/specs/2026-10-08-pr-note-scope-design.md` (A1–A4); `docs/superpowers/plans/2026-10-08-github-write-cache-minors.md` + its final review (B1–B6). The items themselves are the user's list (session starter, 2026-10-08).

## Global Constraints

- A PR shows only what was written for it: scope `"<base>...refs/gg/pr/<n>"`, matched BY NUMBER (`PRScopeNumber`, `PreviewNoteSet.owns`).
- An AI agent never sends anything to GitHub; no test or probe touches real GitHub.
- A test fixture that writes a PR note stamps it: `Preview "main...refs/gg/pr/7"`, CLI `--preview main...refs/gg/pr/7`, web `"pr":7`.
- Web tests built on `sendServerFull`/`prFixture` are SERIAL (`t.Setenv` via `isolateState`) — no `t.Parallel()`.
- New TUI strings go through `i18n.T` with all four bundles (none planned). Engine/CLI/web prose stays English.
- Tasks 1–9 leave `using-gg.md` alone (A3 only refuses a request the docs never promised). Task 14 changes it for `gg link --pr` → `agentskill.Version = 156`, `GGReviewVersion = 3`, dogfood copies regenerated in the same commit.
- `gg commit` has no `-F`: `gg add <paths>` then `git commit -F <msgfile>`. Never `git add -A` (a built `bin/gg` may sit in the worktree).

## Review Focus

1. **Force-pushed PR (A2):** the page still shows the old tip, which is no longer in `prev.Set.Commits` → the note add is REFUSED (409, names the PR, says reopen), never stored plain.
2. **A review saved on a PR tip before `cd28329b` (A3):** unstamped (spec §5), it is no longer in `prReviewHeads` → `gg pr send --review <rid>` now refuses it with "not in this PR". Intended; CHANGELOG says so.
3. **A PR opened offline / before its comments loaded (A1):** `cachedPR` (the listing row the view was opened from) still finds it → the note is stamped; no gh read either way.
4. **One PR, three base spellings on one commit (A4):** one Range review row, count summed, and opening it (any spelling) shows all three notes.
5. **A failed read with a queued post-send read (B4):** the re-read is issued once, never loops (the flag is cleared before it is issued).
6. **A PR link opened where the PR is not listed (Task 13):** no forge, or the PR list not loaded yet (`gg open <link>` at TUI start) → a merge preview with a notice, never a silent nothing; a link whose base is spelled differently from the PR view's (`origin/main` vs `main`) still lands (the gate is the number).

---

## Rulings already taken in this plan (the user's "yes" covers them)

- **A1:** the web resolves the PR with `cachedPR` (the same lookup `handlePROpen` opened the view with) and hands the row to `domain.PRNoteScope`; the domain never reads gh for a note. When the row is missing, `notePreview`'s answer stands (in a PR view that is `""`, because `noteScopeSpec` returns `""` for a PR — so in practice the note is stored plain, as today, but with no gh read).
- **A2:** any commit of the PR's range takes the stamp (reads and the TUI already accept the whole range). A commit OUTSIDE the range (force-push) is refused with HTTP 409 `pull request #7 no longer holds this commit — reopen it` (new `domain.ErrNoteOffPR`). The existing test `TestWebPRNoteOffTheTipIsNotStamped` (writes on `main`, outside the range) becomes `TestWebPRNoteOffThePRIsRefused`.
- **A4:** `ScopesByCommit` merges a PR's spellings into one bucket keyed by the first spelling seen; every allowlist that compared `sc.Scope == scope` uses `domain.SameNoteScope` instead.
- **B2:** no browser probe (a timing race); covered by node unit tests of `prfresh.js`.
- **B3:** the test drives the follower's view directly (a live ctx receiving `context.DeadlineExceeded` from the listing), mirroring `TestACancelledListingKeepsTheCachedVerdict`, rather than staging a real two-goroutine coalesce.
- **B6:** the archived plan's line is reworded, the way cache-minors F-c reworded CLAUDE-details.

---

### Task 1: A1 + A2 — a web note in a PR: cache-only, any commit of the range, refuse off-range

**Files:**
- Modify: `internal/domain/forge_send.go` (`PRNoteScope`, new `ErrNoteOffPR` beside the other `Err…` vars at the top)
- Modify: `internal/web/notes.go:286-289` (`handleNoteAdd`) and the error mapping at `:298-304`
- Test: `internal/domain/pr_note_scope_test.go`, `internal/web/pr_note_scope_test.go`

**Interfaces:**
- Produces: `func (s *Service) PRNoteScope(ctx context.Context, pr model.PullRequest, commit string) (string, error)`; `var ErrNoteOffPR = errors.New("the pull request no longer holds this commit")`.

- [ ] **Step 1: domain tests (RED)** — append to `internal/domain/pr_note_scope_test.go`:

```go
// A1/A2: the scope comes from the row handed in (no forge read), and any
// commit of the PR's range takes it — an older tip the page still shows.
func TestPRNoteScopeAcceptsAnyCommitOfThePR(t *testing.T) {
	t.Parallel()
	svc, ff, oldHead := sendRepo(t)
	dir := repoDir(t, svc)
	runGitIn(t, dir, "checkout", "-q", "feat")
	commitFile(t, dir, "other.go", "package other // moved\n", "a newer commit")
	newHead := revParse(t, dir, "HEAD")
	runGitIn(t, dir, "checkout", "-q", "main")
	runGitIn(t, dir, "update-ref", git.PRRef(7), newHead)
	pr := model.PullRequest{Number: 7, State: "open", Target: "main", HeadSHA: newHead}
	reads := ff.views.Load()
	for _, c := range []string{newHead, oldHead} {
		sc, err := svc.PRNoteScope(context.Background(), pr, c)
		if n, ok := PRScopeNumber(sc); err != nil || !ok || n != 7 {
			t.Fatalf("commit %s: scope %q err %v", c[:7], sc, err)
		}
	}
	if ff.views.Load() != reads {
		t.Fatal("PRNoteScope read the forge")
	}
}

// A2: a commit the PR no longer holds (a force-push) is refused, never plain.
func TestPRNoteScopeRefusesACommitOffThePR(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	base := revParse(t, repoDir(t, svc), "main")
	pr := model.PullRequest{Number: 7, State: "open", Target: "main", HeadSHA: head}
	if sc, err := svc.PRNoteScope(context.Background(), pr, base); !errors.Is(err, ErrNoteOffPR) || sc != "" {
		t.Fatalf("scope %q err %v", sc, err)
	}
}
```

If domain's `fakeForge` has no view counter, add `views atomic.Int64` incremented in its `View`/`PullRequest` method (whichever `svc.PullRequest` calls) — check `forge_test.go`'s fake first and reuse an existing counter if there is one. Add `"errors"` to the imports.

- [ ] **Step 2: run, expect a build failure** (`PRNoteScope` takes an `int`):
  `go test ./internal/domain/ -run 'PRNoteScope' 2>&1 | tail -5` → `cannot use pr (variable of struct type model.PullRequest) as int`.

- [ ] **Step 3: implement** — replace `PRNoteScope` in `internal/domain/forge_send.go`:

```go
// PRNoteScope is the scope a note written in PR pr's view records
// ("<base>...refs/gg/pr/<n>", spec 2026-10-08 §3): any commit of the PR's
// range takes it (the view may show an older tip than the forge's); "" when
// the PR's diff is not available here. A commit the PR no longer holds (its
// head was force-pushed under the open view) is ErrNoteOffPR — never a plain
// note, which would silently leave the PR. pr is the caller's cached row:
// no forge read.
func (s *Service) PRNoteScope(ctx context.Context, pr model.PullRequest, commit string) (string, error) {
	prev, err := s.PRPreview(ctx, pr)
	if err != nil || !prev.Set.OK() {
		return "", nil
	}
	if !slices.Contains(prev.Set.Commits, commit) {
		return "", fmt.Errorf("%w: pull request #%d no longer holds %s — reopen it", ErrNoteOffPR, pr.Number, shortSHA(commit))
	}
	return prev.Set.Pair(), nil
}
```

and add `ErrNoteOffPR = errors.New("the pull request no longer holds this commit")` to the `var (…)` block holding `ErrForgeReadOnly`. Add `"slices"` to the imports if missing.

- [ ] **Step 4: run** `go test ./internal/domain/ -run 'PRNoteScope|PRNote' 2>&1 | tail -5` → PASS.

- [ ] **Step 5: web tests (RED)** — in `internal/web/pr_note_scope_test.go` replace `TestWebPRNoteOffTheTipIsNotStamped` with:

```go
// A2: a commit outside the PR (its head was force-pushed under the page) is
// refused — the note would otherwise be stored plain and leave the PR.
func TestWebPRNoteOffThePRIsRefused(t *testing.T) {
	ts, _, srv, _, dir := sendServerFull(t)
	parent := strings.TrimSpace(gitRun(t, dir, "rev-parse", "main"))
	code, out := postJSONAny(t, ts, "/api/notes/add",
		fmt.Sprintf(`{"path":"f.txt","rev":%q,"state":"commit","side":"new","line":1,"summary":"x","pr":7}`, parent))
	if code != 409 || !strings.Contains(fmt.Sprint(out["error"]), "#7") {
		t.Fatalf("add = %d %v", code, out)
	}
	if c, err := srv.service().NoteCounts(context.Background()); err == nil && c.ByCommit[parent] != 0 {
		t.Fatal("the refused note was stored")
	}
}

// A1: a PR the page does not know is never read from the forge for a note.
func TestWebNoteForAnUnknownPRReadsNoForge(t *testing.T) {
	ts, wf, srv, head, _ := sendServerFull(t)
	wf.mu.Lock()
	before := wf.prReads
	wf.mu.Unlock()
	code, out := postJSONAny(t, ts, "/api/notes/add",
		fmt.Sprintf(`{"path":"pr7.txt","rev":%q,"state":"commit","side":"new","line":1,"summary":"x","pr":99}`, head))
	id, _ := out["id"].(string)
	if code != 200 || id == "" {
		t.Fatalf("add = %d %v", code, out)
	}
	wf.mu.Lock()
	after := wf.prReads
	wf.mu.Unlock()
	if after != before {
		t.Fatalf("the note add read the forge %d times", after-before)
	}
	if p := storedPreview(t, srv, id); p != "" {
		t.Fatalf("an unknown PR stamped %q", p)
	}
}
```

(`writerForge` embeds `*fakeForge`; `prReads` is the field its PR read bumps — confirm in `prs_test.go` and use the real name.)

- [ ] **Step 6: run, expect FAIL** — `go test ./internal/web/ -run 'WebPRNote|WebNoteForAnUnknownPR' 2>&1 | tail -8`: a build failure first (old call site passes an `int`). Fix nothing yet beyond what Step 7 does.

- [ ] **Step 7: implement** — `internal/web/notes.go` `handleNoteAdd`, replace the two-line PR override with:

```go
	preview := s.notePreview(r.Context(), req.Preview, addr)
	if req.PR > 0 && addr.State == model.StateCommitted {
		// The row the view was opened from (cachedPR): never a forge read.
		if pr, ok := s.cachedPR(s.service(), req.PR); ok {
			sc, err := s.service().PRNoteScope(r.Context(), pr, addr.Commit)
			if err != nil {
				writeErr(w, http.StatusConflict, err)
				return
			}
			if sc != "" {
				preview = sc
			}
		}
	}
```

- [ ] **Step 8: run** `go test ./internal/web/ -run 'WebPRNote|WebNoteForAnUnknownPR|PageSendsThePR|PRNotes' 2>&1 | tail -5` → PASS. Then `go test ./internal/domain/ ./internal/web/ 2>&1 | tail -5` → ok.

- [ ] **Step 9: commit**

```bash
gg add internal/domain/forge_send.go internal/domain/pr_note_scope_test.go internal/web/notes.go internal/web/pr_note_scope_test.go
git commit -F <msgfile>   # "fix(web): a PR note takes any commit of the PR, never reads gh, refuses a commit the PR dropped"
```

---

### Task 2: A3 — `gg pr send --review` / `--note review:…` only take the PR's own reviews

**Files:**
- Modify: `internal/domain/forge_send.go` (`planReview`: the `req.Review != ""` case and the `model.IsReviewNoteID(id)` case)
- Test: `internal/domain/pr_reviews_test.go`

**Interfaces:**
- Consumes: `prReviewHeads(ctx, set) []ReviewHead` (`pr_reviews.go:32`).
- Produces: `func (s *Service) prOwnsReview(ctx context.Context, set PreviewNoteSet, rid string) bool`.

- [ ] **Step 1: tests (RED)** — append to `internal/domain/pr_reviews_test.go`:

```go
// A3: a review the PR does not show is refused by both send shapes.
func TestPRSendRefusesAReviewThePRDoesNotShow(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	other, _, err := svc.SaveReview(context.Background(), SaveReview{
		Target: ReviewTarget{Kind: ReviewRange, Range: "main.." + head, Label: "main ... feat",
			Diff: model.DiffSpec{Rev: "main.." + head}, Commit: head, Preview: "main...feat"},
		Agent: "claude", Text: twoRemarks})
	if err != nil {
		t.Fatal(err)
	}
	for _, req := range []PRSendRequest{
		{Review: other},
		{Notes: []string{model.ReviewNoteID(other, 0)}},
	} {
		if _, err := svc.PRSendPlan(context.Background(), 7, req); !errors.Is(err, ErrSendRequest) ||
			!strings.Contains(err.Error(), "not in this PR") {
			t.Fatalf("%+v: err %v", req, err)
		}
	}
	mine := savePRReview(t, svc, twoRemarks)
	if _, err := svc.PRSendPlan(context.Background(), 7, PRSendRequest{Review: mine}); err != nil {
		t.Fatalf("the PR's own review: %v", err)
	}
}
```

Before writing it, confirm the planner's entry point name (`PRSendPlan` or whatever `TestPlanSendRefuses…` in `forge_send_test.go` calls) and `model.ReviewNoteID`'s real name (the inverse of `ParseReviewNoteID`); use those.

- [ ] **Step 2: run** `go test ./internal/domain/ -run TestPRSendRefusesAReviewThePRDoesNotShow 2>&1 | tail -5` → FAIL (`err <nil>`).

- [ ] **Step 3: implement** — in `pr_reviews.go` beside `prReviewHeads`:

```go
// prOwnsReview reports whether review rid is one PR set shows (its own,
// stamped for it): a send takes no other.
func (s *Service) prOwnsReview(ctx context.Context, set PreviewNoteSet, rid string) bool {
	return slices.ContainsFunc(s.prReviewHeads(ctx, set), func(h ReviewHead) bool { return h.ID == rid })
}
```

In `planReview`, right after each `r, err := s.Review(ctx, …)` + its `err` check (both the `req.Review != ""` case and the `IsReviewNoteID` case):

```go
		if !s.prOwnsReview(ctx, prev.Set, r.ID) {
			return engine.SendPlan{}, fmt.Errorf("%w: review %s is not in this PR", ErrSendRequest, r.ID)
		}
```

- [ ] **Step 4: run** `go test ./internal/domain/ -run 'PRSend|PlanSend|PRReview|PRDiffShows' 2>&1 | tail -5` → PASS; then `go test ./internal/domain/ ./internal/cli/ ./internal/web/ ./internal/tui/ -run 'Send|Review' 2>&1 | tail -5` → ok (fixtures already stamp their reviews via `ScopeReviewTarget`; a failure here is a fixture writing an unstamped review — stamp it, ledger it).

- [ ] **Step 5: commit** — `fix(pr): a send takes only the reviews the PR shows`.

---

### Task 3: A4 — one PR, one Range review row, whatever the base spelling

**Files:**
- Modify: `internal/domain/pairnotes.go` (new `SameNoteScope`; `ScopeAtCommit`'s `sc.Scope != scope`)
- Modify: `internal/domain/previewnotes.go` (`owns`)
- Modify: `internal/domain/notes.go:681-682` (`ScopesByCommit` bucket match)
- Modify: `internal/domain/review.go` (`ScopeReviewTarget` label)
- Modify: `internal/web/scoperange.go:69-71`, `internal/web/notes.go:245-249` (allowlists)
- Test: `internal/domain/pr_note_scope_test.go`, `internal/web/scoperange_test.go`

**Interfaces:**
- Produces: `func SameNoteScope(a, b string) bool` — equal, or both PR scopes of one number.

- [ ] **Step 1: tests (RED)** — append to `internal/domain/pr_note_scope_test.go`:

```go
// A4: a PR's notes written over different base spellings are one review:
// one Range review row (count summed), and opening it from any spelling
// shows them all.
func TestOnePRIsOneScopeWhateverTheBaseSpelling(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	base := revParse(t, repoDir(t, svc), "main")
	a := addScopedNote(t, svc, head, "big.go", 5, "main..."+git.PRRef(7), "by name")
	b := addScopedNote(t, svc, head, "big.go", 25, base+"..."+git.PRRef(7), "by sha")
	c, err := svc.NoteCounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if scs := c.ScopesByCommit[head]; len(scs) != 1 || scs[0].N != 2 {
		t.Fatalf("scopes %+v", scs)
	}
	from, to, err := svc.ScopeAtCommit(context.Background(), base+"..."+git.PRRef(7), head)
	if err != nil {
		t.Fatal(err)
	}
	set, err := svc.PairNotes(context.Background(), from, to)
	if err != nil {
		t.Fatal(err)
	}
	set.Only = base + "..." + git.PRRef(7) // opened from the sha spelling
	if ids := idsAt(t, svc, set, "big.go"); !ids[a] || !ids[b] {
		t.Fatalf("ids %v (a %s b %s)", ids, a, b)
	}
}

func TestScopeReviewTargetNamesThePR(t *testing.T) {
	t.Parallel()
	svc, _, _ := sendRepo(t)
	if got := ScopeReviewTarget(prNoteSetOf(t, svc)).Label; got != "PR #7" {
		t.Fatalf("label %q", got)
	}
}

func TestSameNoteScope(t *testing.T) {
	t.Parallel()
	pr7, pr8 := "main..."+git.PRRef(7), "main..."+git.PRRef(8)
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{pr7, pr7, true},
		{pr7, "abc123..." + git.PRRef(7), true},
		{pr7, pr8, false},
		{"main...feat", "main...feat", true},
		{"main...feat", "origin/main...feat", false},
		{pr7, "", false},
	} {
		if got := SameNoteScope(tc.a, tc.b); got != tc.want {
			t.Errorf("SameNoteScope(%q, %q) = %v", tc.a, tc.b, got)
		}
	}
}
```

Web allowlist test, append to `internal/web/scoperange_test.go` (follow that file's existing fixture for a commit with a scoped note; write two notes on one commit stamped `main...refs/gg/pr/7` and `<mainsha>...refs/gg/pr/7`, then `GET /api/scope/range?commit=<sha>&scope=<the sha spelling>` → 200, and `/api/notes/counts`' `scopes_by_commit[<sha>]` has ONE entry with `n: 2` and `label: "PR #7"`). Use the route and JSON names that file's tests already use.

- [ ] **Step 2: run** `go test ./internal/domain/ -run 'OnePRIsOneScope|ScopeReviewTargetNamesThePR|SameNoteScope' 2>&1 | tail -5` → build failure (`SameNoteScope` undefined); after a stub it fails on the counts (2 buckets), the ids (b missing) and the label.

- [ ] **Step 3: implement**

`internal/domain/pairnotes.go`, after `PRScopeNumber`:

```go
// SameNoteScope reports whether two note scopes (Note.Preview) are one
// review: the same name, or the same pull request whatever the base's
// spelling (spec 2026-10-08 §2).
func SameNoteScope(a, b string) bool {
	if a == b {
		return a != ""
	}
	n, ok := PRScopeNumber(a)
	m, ok2 := PRScopeNumber(b)
	return ok && ok2 && n == m
}
```

`ScopeAtCommit`: `if sc.Scope != scope || sc.Base == ""` → `if !SameNoteScope(sc.Scope, scope) || sc.Base == ""`.

`internal/domain/previewnotes.go` `owns`:

```go
func (set PreviewNoteSet) owns(preview string) bool {
	if n, ok := git.ParsePRRef(set.Source); ok {
		m, ok := PRScopeNumber(preview)
		return ok && m == n
	}
	// A PR's review opened from its commit (Only, View all notes): any
	// spelling of that PR's base.
	return SameNoteScope(preview, set.scope())
}
```

(Non-PR scopes keep exact matching: `SameNoteScope` of two non-PR strings is `a == b`.)

`internal/domain/notes.go` bucket match: `e.Scope == n.Preview` → `SameNoteScope(e.Scope, n.Preview)`.

`internal/domain/review.go` `ScopeReviewTarget`: after the `IsPair` branch add

```go
	if _, ok := git.ParsePRRef(set.Source); ok {
		label = NoteScopeLabel(set.scope()) // "PR #7", the TUI's and web's name for it
	}
```

(import `internal/git` if `review.go` lacks it; `domain` already imports it elsewhere.)

`internal/web/scoperange.go`: `known = known || sc.Scope == scope` → `known = known || domain.SameNoteScope(sc.Scope, scope)`.
`internal/web/notes.go` `notePreview`: `if sc.Scope == spec` → `if domain.SameNoteScope(sc.Scope, spec)` (still `return spec`: the note joins under the spelling the page holds; reads match by number).

- [ ] **Step 4: run** `go test ./internal/domain/ ./internal/web/ ./internal/tui/ -run 'Scope|Review|PRNote|AllNotes|RangeReview' 2>&1 | tail -5` → ok. A TUI/web golden or label test asserting `"<base> ... refs/gg/pr/7"` for a PR review is updated to `"PR #7"` (ledger it).

- [ ] **Step 5: commit** — `fix(notes): one pull request is one review scope whatever its base's spelling`.

---

### Task 4: B1 — a web PR open stamps the entry before the preview writes it

**Files:**
- Modify: `internal/web/prs.go:316-322` (`handlePROpen`)
- Test: `internal/web/prs_cache_test.go` (create if no PR-cache web test file exists; else append to the one holding the open/cache tests — `grep -l SetPRCacheStore internal/web/*_test.go`)

- [ ] **Step 1: test (RED)**

```go
// B1: on a full cache, opening a PR keeps its derived data: the open is
// stamped BEFORE PRPreview writes the entry, so the trim drops the older one.
func TestPROpenOnAFullCacheKeepsItsDerivedData(t *testing.T) {
	ts, _, srv, _, _ := sendServerFull(t)
	st := prcache.New(t.TempDir(), 1)
	srv.service().SetPRCacheStore(st)
	if err := st.Update(8, func(e *prcache.Entry) { e.OpenedAt = time.Now().Add(-time.Hour) }); err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if code := getJSON(t, ts, "/api/pr/open?n=7", &out); code != 200 {
		t.Fatalf("open = %d %v", code, out)
	}
	e, ok := st.Load(7)
	if !ok || len(e.Derived) == 0 || e.OpenedAt.IsZero() {
		t.Fatalf("entry 7 ok=%v derived=%d opened=%v", ok, len(e.Derived), e.OpenedAt)
	}
	if _, ok := st.Load(8); ok {
		t.Fatal("the older entry survived the trim")
	}
}
```

- [ ] **Step 2: run** `go test ./internal/web/ -run TestPROpenOnAFullCacheKeepsItsDerivedData 2>&1 | tail -5` → FAIL (`derived=0`). If it passes, the entry already existed with derived data from the fetch: then the new store above is not the one PRPreview writes — check `SetPRCacheStore` replaces the service's store and fix the fixture, not the assertion.

- [ ] **Step 3: implement** — in `handlePROpen` move `svc.MarkPROpened(ctx, n)` to just before `res, err := svc.PRPreview(ctx, pr)`, comment: `// stamped FIRST: on a full cache the entry PRPreview writes must not be the one trimmed`.

- [ ] **Step 4: run** the test → PASS; `go test ./internal/web/ -run 'PR' 2>&1 | tail -3` → ok.

- [ ] **Step 5: commit** — `fix(web): a PR open is stamped before its preview is cached`.

---

### Task 5: B2 — one moved-head follow at a time; the reopen's comments read waits instead of dropping

**Files:**
- Modify: `internal/web/static/prfresh.js` (new pure `oncePerKey`)
- Modify: `internal/web/static/prs.js` (`followMovedHead`, `refreshPRComments`)
- Test: `internal/web/prfreshjs_test.go`

**Interfaces:**
- Produces: `export function oncePerKey(fn)` → `(key, ...args) => Promise|null` — runs `fn(key, ...args)` unless a call for `key` is still pending (then `null`).

- [ ] **Step 1: test (RED)** — append to `internal/web/prfreshjs_test.go`:

```go
// B2: a second moved-head follow of the same PR while one runs is skipped;
// another PR's, or the same PR's after the first ended, runs.
func TestPRFreshJSOncePerKey(t *testing.T) {
	t.Parallel()
	out := runFreshModuleJS(t, `
import { oncePerKey } from "./prfresh.mjs";
const ends = [];
const runs = [];
const follow = oncePerKey((n) => { runs.push(n); return new Promise((r) => ends.push(r)); });
const first = follow(7);
const dup = follow(7) === null;
follow(8);
ends[0](); await first; await new Promise((r) => setTimeout(r, 0));
follow(7);
console.log(JSON.stringify({ runs, dup }));
`)
	var got struct {
		Runs []int `json:"runs"`
		Dup  bool  `json:"dup"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if !slices.Equal(got.Runs, []int{7, 8, 7}) || !got.Dup {
		t.Fatalf("runs=%v dup=%v", got.Runs, got.Dup)
	}
}

// B2: prs.js follows a moved head through oncePerKey, and the reopen's
// comments read waits for a running read instead of being dropped.
func TestPRsJSFollowsOnceAndQueuesTheReopenRead(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "prs.js"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, want := range []string{
		"const followMovedHead = oncePerKey(",
		`reads.soon("comments:" + n, commentsRead(n, moved))`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("prs.js lacks %q", want)
		}
	}
}
```

(Add `os`, `path/filepath`, `strings` imports if missing; `runFreshModuleJS` is the file's existing runner.)

- [ ] **Step 2: run** `go test ./internal/web/ -run 'PRFreshJSOncePerKey|PRsJSFollowsOnce' 2>&1 | tail -6` → FAIL (no export / missing strings).

- [ ] **Step 3: implement** — `prfresh.js`, after `serialReads`:

```js
// oncePerKey wraps an async fn so one call per key runs at a time: a call
// for a key whose earlier call has not settled answers null (prs.js: the
// moved-head follow — two reads seeing the same move must fetch once).
export function oncePerKey(fn) {
  const pending = new Set();
  return (key, ...args) => {
    if (pending.has(key)) return null;
    pending.add(key);
    const done = () => pending.delete(key);
    const p = Promise.resolve().then(() => fn(key, ...args));
    p.then(done, done);
    return p;
  };
}
```

`prs.js`: import `oncePerKey` beside `serialReads`; turn `async function followMovedHead(n) {…}` into

```js
const followMovedHead = oncePerKey(async (n) => {
  …same body…
});
```

(declared before its first use at `onHeadMoved(...)`: a `const` is not hoisted — move the declaration above that line). `refreshPRComments` becomes:

```js
export function refreshPRComments(n, moved = false) {
  reads.soon("comments:" + n, commentsRead(n, moved));
}
```

(no caller uses the return value: `live.js:237`, `prs.js:280`.)

- [ ] **Step 4: run** `go test ./internal/web/ -run 'PRFresh|PRsJS|PRs' 2>&1 | tail -4` → ok; `node --check internal/web/static/prs.js` → no output.

- [ ] **Step 5: commit** — `fix(web): a moved PR head is followed once; the reopen's comments read waits its turn`.

---

### Task 6: B3 — a budget's deadline is not a forge verdict

**Files:**
- Modify: `internal/domain/forge.go:185`
- Test: `internal/domain/prcache_test.go`

- [ ] **Step 1: test (RED)** — append:

```go
// B3: a coalesced caller with a live ctx gets the leader's budget
// DeadlineExceeded: that says nothing about gh — no Detect, verdict stands.
func TestADeadlinedListingKeepsTheCachedVerdict(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ok := &fakeForge{url: "u", open: []model.PullRequest{pr(3, "open", 1)}}
	if _, err := newCachedForgeSvc(t, ok, dir).PullRequests(context.Background()); err != nil {
		t.Fatal(err)
	}
	ff := &fakeForge{url: "u", listErr: fmt.Errorf("gh: %w", context.DeadlineExceeded)}
	b := newCachedForgeSvc(t, ff, dir)
	if _, err := b.PullRequests(context.Background()); err == nil {
		t.Fatal("want an error")
	}
	if n := ff.detects.Load(); n != 0 {
		t.Fatalf("a deadlined listing ran Detect %d times", n)
	}
}
```

- [ ] **Step 2: run** `go test ./internal/domain/ -run TestADeadlinedListingKeepsTheCachedVerdict 2>&1 | tail -4` → FAIL (`ran Detect 1 times`).

- [ ] **Step 3: implement**

```go
	decisive := err == nil || (ctx.Err() == nil &&
		!errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded))
```

and extend the comment above: `— a coalesced caller gets the LEADER's cancellation or deadline —`.

- [ ] **Step 4: run** `go test ./internal/domain/ -run 'Listing|Verdict|PullRequests|Forge' 2>&1 | tail -3` → ok.

- [ ] **Step 5: commit** — `fix(forge): a listing that ran out of budget keeps the cached verdict`.

---

### Task 7: B4 — a failed read still issues the queued post-send read

**Files:**
- Modify: `internal/tui/pr_revalidate.go` (`handlePRRevalidatedMsg`, the `msg.err != nil` return)
- Test: `internal/tui/pr_forge_gen_test.go`

- [ ] **Step 1: test (RED)** — append:

```go
// B4: the read a post-send re-read waited for FAILED: the re-read is still
// issued (once — the flag is cleared first).
func TestAFailedReadStillIssuesTheQueuedReRead(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m = revalidated(m, 7, 1, false)
	m.prRefreshAgain = 7
	nm, cmd := m.Update(prRevalidatedMsg{n: 7, gen: m.forgeGen, pr: model.PullRequest{Number: 7}, err: errors.New("offline")})
	mm := nm.(Model)
	if mm.prRefreshAgain != 0 || !mm.prRevalidateInflight || cmd == nil {
		t.Fatalf("again=%d inflight=%v cmd=%v", mm.prRefreshAgain, mm.prRevalidateInflight, cmd != nil)
	}
}
```

(Use `m.forgeGen` or `m.prsGen` exactly as `TestARefreshAgainDiesWithAClosedView` does; add `errors` import.)

- [ ] **Step 2: run** `go test ./internal/tui/ -run TestAFailedReadStillIssuesTheQueuedReRead 2>&1 | tail -4` → FAIL (`inflight=false`).

- [ ] **Step 3: implement** — the error return becomes:

```go
	if msg.err != nil {
		if again && m.openPRNumber() == msg.n {
			// The read the post-send read waited for failed: ask it now,
			// once (the flag is already cleared).
			var next tea.Cmd
			m, next = m.prRefreshCmd(msg.n, false)
			cmd = tea.Batch(cmd, next)
		}
		return m, cmd // offline, rate-limited: the diff on screen stands
	}
```

- [ ] **Step 4: run** `go test ./internal/tui/ -run 'Again|Revalidat|OwnSend|PRCache' 2>&1 | tail -3` → ok.

- [ ] **Step 5: commit** — `fix(tui): a failed PR read still issues the queued post-send read`.

---

### Task 8: B5 — prcache's trim and quarantine retry a transient Windows error

**Files:**
- Modify: `internal/prcache/prcache.go` (`Store` gains `remove`; `readJSON` → `(s *Store) readJSON`; a shared `retry`)
- Test: `internal/prcache/prcache_test.go`

- [ ] **Step 1: tests (RED)** — append (mirror `TestSaveRetriesATransientRename` for construction):

```go
// B5: the trim's remove retries a transient error (Windows: a reader holds
// the file) instead of leaving the cache over its bound.
func TestTrimRetriesATransientRemove(t *testing.T) {
	t.Parallel()
	s := New(t.TempDir(), 1)
	fails := 2
	s.remove = func(p string) error {
		if fails > 0 {
			fails--
			return &os.PathError{Op: "remove", Path: p, Err: fs.ErrPermission}
		}
		return os.Remove(p)
	}
	for n, at := range map[int]time.Time{1: time.Unix(100, 0), 2: time.Unix(200, 0)} {
		if err := s.Save(Entry{Number: n, OpenedAt: at}); err != nil {
			t.Fatalf("save %d: %v", n, err)
		}
	}
	if _, ok := s.Load(1); ok || fails != 0 {
		t.Fatalf("entry 1 kept=%v, fails left %d", ok, fails)
	}
}

// B5: a corrupt file's quarantine rename retries a transient error too.
func TestQuarantineRetriesATransientRename(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := New(dir, 0)
	if err := os.WriteFile(filepath.Join(dir, "pr-3.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	fails := 2
	s.rename = func(from, to string) error {
		if fails > 0 {
			fails--
			return &os.LinkError{Op: "rename", Old: from, New: to, Err: fs.ErrPermission}
		}
		return os.Rename(from, to)
	}
	if _, ok := s.Load(3); ok {
		t.Fatal("a corrupt entry loaded")
	}
	if _, err := os.Stat(filepath.Join(dir, "pr-3.json")); !errors.Is(err, fs.ErrNotExist) || fails != 0 {
		t.Fatalf("not quarantined: %v, fails left %d", err, fails)
	}
}
```

(The map iteration order does not matter: Save trims after each write and entry 2 is newer either way.)

- [ ] **Step 2: run** `go test ./internal/prcache/ 2>&1 | tail -4` → build failure (`s.remove` undefined), then FAIL on the quarantine (free `readJSON` ignores `s.rename`).

- [ ] **Step 3: implement**
  - `Store` gets `remove func(string) error // os.Remove; tests inject a blocked one`; `New` sets `remove: os.Remove`.
  - Generalise the retry: `renameRetry` becomes

```go
// retry runs op until it succeeds, fails for good, or filelock.Wait passes.
// On Windows a rename over, a rename of, or a removal of a file another gg
// process is reading (os.ReadFile holds it without FILE_SHARE_DELETE) fails
// with access denied or a sharing violation — a state that clears in
// milliseconds.
func retry(op func() error) error {
	deadline := time.Now().Add(filelock.Wait)
	for {
		err := op()
		if err == nil || !transientRename(err) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(filelock.Poll)
	}
}

func (s *Store) renameRetry(from, to string) error {
	return retry(func() error { return s.rename(from, to) })
}
```

  - `readJSON` becomes a method; the quarantine line: `_ = s.renameRetry(path, path+".corrupt-"+strconv.FormatInt(time.Now().Unix(), 10))`. Update every call (`LoadRepo`, `LoadList`, `Load`, `trimLocked`, and any other in the file).
  - `trimLocked` and `Remove`: `os.Remove(x)` → `retry(func() error { return s.remove(x) })`, keeping the `fs.ErrNotExist` tolerance.

- [ ] **Step 4: run** `go test ./internal/prcache/ ./internal/domain/ -run '.' 2>&1 | tail -3` → ok; `GOOS=windows go vet ./internal/prcache/` → clean.

- [ ] **Step 5: commit** — `fix(prcache): the trim and the quarantine retry a transient Windows error`.

---

### Task 9: B6 + docs

**Files:**
- Modify: `docs/superpowers/plans/2026-10-08-github-write-followups.md:850`
- Modify: `CHANGELOG.md` (new top section), `docs/CLAUDE-details.md` (the PR note-scope paragraph: any range commit takes the stamp, off-range refused, spellings merged; the PR-reviews paragraph: a send takes only the PR's reviews)

- [ ] **Step 1:** line 850 `// flight when the send ended neither shows nor absorbs it.` → `// flight when the send ended does not absorb it (it still shows "updated" when it reports a change).`
- [ ] **Step 2:** CHANGELOG top section "Pull request follow-ups":
  - A note written in a PR's diff joins the PR on any of its commits — an older head the page still shows included — and is refused, with "reopen it", when the PR no longer holds the commit. Writing it never asks GitHub.
  - One PR's notes are one Range review row whatever base they were written over; a PR's review is titled "PR #7".
  - `gg pr send --review`/`--note review:…` take only a review the PR shows (a review saved on a PR tip before PR note scope is no longer sendable from the PR).
  - A web PR open on a full cache keeps its diff data; a moved head is followed once; the reopen's comments read waits instead of dropping; a listing that ran out of time keeps the forge verdict; the TUI's post-send re-read survives a failed read; the PR cache retries transient Windows file errors on trim and quarantine.
- [ ] **Step 3:** `docs/CLAUDE-details.md` — update the two paragraphs named above (grep `refs/gg/pr/<n>` and `prReviewHeads`).
- [ ] **Step 4: commit** — `docs: PR follow-ups 2`.

---

### Task 10: `domain.PRLinkPair` + `gg link --pr <n>`

**Why (user, 2026-10-08):** "There is no option to get a gg link to a PR, so how can I ask the agent to do a review?" → "create gg links menus where applicable" → "add link opening too". A PR's link is the merge-preview link `gg://<repo>[/<path>[:<line>]]@<base>...refs/gg/pr/<n>`; `/gg-review` over it stores a review stamped for the PR (probed 2026-10-08 in a scratch repo: `preview: "main...refs/gg/pr/7"`). No grammar change.

**Files:**
- Modify: `internal/domain/forge.go` (new `PRLinkPair` beside `PRPair`)
- Modify: `internal/cli/link.go` (`--pr` flag, usage line)
- Test: `internal/domain/pr_link_test.go` (create), `internal/cli/link_pr_test.go` (create)

**Interfaces:**
- Produces: `func (s *Service) PRLinkPair(ctx context.Context, n int) (PRPair, error)` — the pair PR n's link names: PRPair of the cached row (listing, else the full-read cache), else ONE `PullRequest` read; `ErrPRNotFetched` (new, `errors.New("the pull request is not fetched here")`, wrapped with `gg pr fetch <n>`) when `refs/gg/pr/<n>` is missing.

- [ ] **Step 1: domain test (RED)** — `internal/domain/pr_link_test.go`:

```go
package domain

import (
	"context"
	"errors"
	"testing"

	"github.com/homeend/gigagit/internal/git"
)

// Task 10: PR 7's link pair is its base against gg's private head ref; a PR
// that was never fetched has no link (it would not resolve).
func TestPRLinkPair(t *testing.T) {
	t.Parallel()
	svc, ff, _ := sendRepo(t)
	p, err := svc.PRLinkPair(context.Background(), 7)
	if err != nil || p.Head != git.PRRef(7) || p.Base != "main" {
		t.Fatalf("pair %+v err %v", p, err)
	}
	ff.byNum[8] = ff.byNum[7]
	pr8 := ff.byNum[8]
	pr8.Number = 8
	ff.byNum[8] = pr8
	if _, err := svc.PRLinkPair(context.Background(), 8); !errors.Is(err, ErrPRNotFetched) {
		t.Fatalf("unfetched: %v", err)
	}
}
```

- [ ] **Step 2: run** `go test ./internal/domain/ -run TestPRLinkPair 2>&1 | tail -3` → build failure (`PRLinkPair` undefined).

- [ ] **Step 3: implement** in `forge.go` after `PRPair`:

```go
// ErrPRNotFetched: a PR's link names gg's private refs/gg/pr/<n>, which only
// a fetched PR has — a link to an unfetched one would not resolve.
var ErrPRNotFetched = errors.New("the pull request is not fetched here")

// PRLinkPair is the pair PR n's gg:// link names (<base>...refs/gg/pr/<n>):
// the base the PR's own view uses (PRPair) and the fetched head. The row
// comes from the cache; only a PR this session never saw costs a forge read.
func (s *Service) PRLinkPair(ctx context.Context, n int) (PRPair, error) {
	if !s.PRFetched(ctx)[n] {
		return PRPair{}, fmt.Errorf("%w: gg pr fetch %d", ErrPRNotFetched, n)
	}
	pr, ok := model.PullRequest{}, false
	if rows, rok := s.PullRequestsCached(); rok {
		for _, p := range rows {
			if p.Number == n {
				pr, ok = p, true
			}
		}
	}
	if !ok {
		if p, _, dok := s.PRDetailsCached(n); dok {
			pr, ok = p, true
		}
	}
	if !ok {
		var err error
		if pr, err = s.PullRequest(ctx, n); err != nil {
			return PRPair{}, err
		}
	}
	return s.PRPair(ctx, pr), nil
}
```

- [ ] **Step 4: run** the test → PASS.

- [ ] **Step 5: CLI test (RED)** — `internal/cli/link_pr_test.go`: build the repo the way `internal/cli/pr_note_scope_test.go` does (it has a fetched PR 7 with base `main`); run `runLink(<temp state path>, svc, dir, []string{"--pr", "7"}, &out, &errb)` → exit 0, stdout ends with `@main...refs/gg/pr/7\n`; `[]string{"--pr", "7", "big.go:5"}` → contains `/big.go:5@main...refs/gg/pr/7`; `[]string{"--pr", "7", "--rev", "HEAD"}` → exit 2 (one target); `[]string{"--pr", "x"}` → exit 2; an unfetched PR → exit 1 with `gg pr fetch` on stderr. Use the exact helper names that file uses.

- [ ] **Step 6: run** `go test ./internal/cli/ -run LinkPR 2>&1 | tail -3` → FAIL (`flag provided but not defined: -pr`).

- [ ] **Step 7: implement** — in `runLink`: `prN := fs.Int("pr", 0, "a pull request's link: <base>...refs/gg/pr/<n> (fetched PRs; gg pr fetch <n>)")`; add `*prN != 0` to the target count (`set`) and to the `--review`/`--version` exclusion lists; a negative `*prN` → usage exit 2. After the `pf.set()` block:

```go
	if *prN != 0 {
		pp, perr := svc.PRLinkPair(ctx, *prN)
		if perr != nil {
			fmt.Fprintln(stderr, "error:", perr)
			return 1
		}
		prev = &model.LinkPreview{Source: pp.Head, Target: pp.Base}
	}
```

Usage first line gains `| --pr <n>` beside `--preview …`; the "name the target; use one" message lists `--pr`.

- [ ] **Step 8: run** `go test ./internal/cli/ -run 'Link' 2>&1 | tail -3` → ok.

- [ ] **Step 9: commit** — `feat(link): gg link --pr <n> prints a pull request's gg:// link`.

---

### Task 11: TUI — copy a PR's link

**Files:**
- Modify: `internal/tui/footer.go` (binding `pr-link`, key `L`), `internal/tui/action_menu.go` (label case `pr-link`; open-PR `.` rows), the Pull requests list key handler (where `pr-copy`/`y` runs — `grep -n '"pr-copy"' internal/tui/*.go`), `internal/tui/pr_hub.go` (`L` in the hub, keys line), `internal/tui/help.go` (one row)
- Create: `internal/tui/pr_link.go`
- Modify: `internal/i18n/lang/{ja,ko,zh,ru}.toml`
- Test: `internal/tui/pr_link_test.go` (create)

**Interfaces:**
- Consumes: `domain.PRLinkPair`, `Model.previewLinkFor(source, target, path string, line int) (string, bool)` (`link.go:125`), `Model.copyToClipboardCmd(desc, text string) tea.Cmd`.
- Produces: `func (m Model) prLinkCmd(n int) tea.Cmd` → `prLinkMsg{n int; source, target string; err error}`; `handlePRLinkMsg` copies `previewLinkFor(source, target, "", 0)` with the status `copied link to PR #%d`, an `ErrPRNotFetched` → `PR #%d is not fetched: press enter to open it first`, any other error → `error: %s`.

- [ ] **Step 1: tests (RED)** — `internal/tui/pr_link_test.go` on `prDiffModel(t)` / the PR list fixture the other `pr_*_test.go` files use:
  - `TestPRListLCopiesThePRLink`: Pull requests panel focused on #7, key `L` → returns a cmd; run it → `prLinkMsg{n:7, source:"refs/gg/pr/7", target:"main"}`; `Update` with it → the clipboard text (the fixture's clipboard seam the other copy tests read — see `stash_link_test.go`/`link_test.go`) ends with `@main...refs/gg/pr/7`, status `copied link to PR #7`.
  - `TestUnfetchedPRLinkSaysOpenItFirst`: `Update(prLinkMsg{n: 8, err: fmt.Errorf("%w: gg pr fetch 8", domain.ErrPRNotFetched)})` → status `PR #8 is not fetched: press enter to open it first`, nothing copied.
  - `TestOpenPRMenuHasCopyPRLink`: the `.` menu over an open PR's diff has a row `pr-link-open` labelled `Copy pull request link`; running it copies `…@<po.target>...<po.source>` synchronously (no cmd round trip — the pair is on screen).
  - `TestPRHubLCopiesTheLink`: in the PR hub popup, `L` returns `prLinkCmd(7)`'s msg.

- [ ] **Step 2: run** `go test ./internal/tui/ -run 'PRLink|PRListL|PRHubL|OpenPRMenuHasCopy' 2>&1 | tail -5` → build failure.

- [ ] **Step 3: implement**
  - `pr_link.go`: `prLinkMsg`, `prLinkCmd` (`svc.PRLinkPair(context.Background(), n)` off the Update thread), `handlePRLinkMsg` (wire in `Update`'s msg switch beside the other PR msgs).
  - footer binding after `pr-copy`: `{"pr-link", "L", i18n.T("[L] copy link"), func(m Model) bool { return m.canOpenPR() }, scopeRow}`; `action_menu.go` label case `"pr-link": i18n.T("Copy pull request link")`; the list key handler maps `pr-link` → `prLinkCmd(<row's number>)`.
  - open-PR `.` rows (the `m.openPRNumber() > 0` block): `actionRow{id: "pr-link-open", label: i18n.T("Copy pull request link"), run: …}` — `po := m.previewOpen`; `link, ok := m.previewLinkFor(po.source, po.target, "", 0)`; `!ok` → status `▸ no gg link for this place`; else `copyToClipboardCmd(i18n.T("copied link to PR #%d", n), link)`.
  - hub: `case "L": return m, m.prLinkCmd(p.pr.Number)`; keys line `[y] copy URL  [L] copy link  [r] reload  [s] send review  [v] verdict`.
  - help row in the Pull requests section: `r("L", i18n.T("copy the pull request's gg:// link — paste it to an agent (/gg-review <link>) or open it with gg open; it lands in the PR's view"))`.
  - Bundles: every new key in ja/ko/zh/ru (replace the hub keys line key; remove the old one from all four — the orphan gate).

- [ ] **Step 4: run** the tests → PASS; then `go test ./internal/i18n/ ./internal/tui/ -run 'I18n|EngineProse|DecisionOptionValues|ActionMenuLabels|CheckVerbs|FooterRendersTranslated|PR' 2>&1 | tail -3` → ok.

- [ ] **Step 5: commit** — `feat(tui): copy a pull request's gg:// link (L, the . menu, the PR details)`.

---

### Task 12: web — copy a PR's link

**Files:**
- Modify: `internal/web/prs.go` (route `GET /api/pr/link`), `internal/web/static/links.js` (`registerRows("pr", …)`), `internal/web/static/prs.js` (help text if the PR menu is listed in `registerHelp`)
- Test: `internal/web/pr_link_test.go` (create)

**Interfaces:**
- Produces: `GET /api/pr/link?n=<n>` → `{"source": "refs/gg/pr/<n>", "target": "<base>"}`; 404 an unknown PR (`knownPR`), 409 `ErrPRNotFetched` (message names `gg pr fetch`).

- [ ] **Step 1: tests (RED)** — `internal/web/pr_link_test.go` (SERIAL, `sendServerFull`):
  - `TestPRLinkPairOverTheWire`: `GET /api/pr/link?n=7` → 200 `{source: "refs/gg/pr/7", target: "main"}`; `n=99` → 404.
  - `TestPRMenuCopiesTheLink` (static, parallel): `links.js` contains `registerRows("pr",` and `"/api/pr/link?n="`.

- [ ] **Step 2: run** `go test ./internal/web/ -run 'PRLinkPair|PRMenuCopies' 2>&1 | tail -3` → FAIL (404 / missing strings).

- [ ] **Step 3: implement**
  - `prs.go`: `mux.HandleFunc("GET /api/pr/link", s.handlePRLink)`; handler: `svc, pr, ok := s.knownPR(w, r)`; `p, err := svc.PRLinkPair(readCtx(r), pr.Number)`; `errors.Is(err, domain.ErrPRNotFetched)` → 409, other err → 500; else `writeJSON(w, map[string]any{"source": p.Head, "target": p.Base})`.
  - `links.js`, beside `registerRows("preview", …)`:

```js
// A pull request's link: its base against gg's private head ref, the pair the
// PR view opens (the server names it — the row's own names are for DISPLAY).
// Opening it lands in the PR's view (live.js). An unfetched PR has none.
registerRows("pr", (pr) =>
  !pr.fetched
    ? []
    : [{
        label: "copy gg:// link",
        act: async () => {
          let p;
          try {
            p = await getJSON("/api/pr/link?n=" + pr.number);
          } catch (e) {
            opLine("pull request #" + pr.number + ": " + (e.message || e), true);
            return;
          }
          const link = linkFor(state.repo, state.worktree, { preview: { source: p.source, target: p.target } });
          if (link) copyLink(link, linkDesc("preview", "PR #" + pr.number, ""));
          else opLine("pull request #" + pr.number + ": no gg link for this pair", true);
        },
      }]
);
```

  (Match `getJSON`/`opLine`/`linkFor`'s real signatures and imports in `links.js`; the preview row's own `copyLinkRow` shows the call shape.)

- [ ] **Step 4: run** the tests → PASS; `node --check internal/web/static/links.js`.

- [ ] **Step 5: commit** — `feat(web): copy a pull request's gg:// link from its menu`.

---

### Task 13: opening a PR link lands in the PR's view (TUI + web)

**Rule:** a navigate whose preview SOURCE is `refs/gg/pr/<n>` (`domain.PRScopeNumber(target+"..."+source)` in Go; `/^refs\/gg\/pr\/(\d+)$/` in JS) opens PR n's view — GitHub threads, its own notes, send — when PR n is in the frontend's PR list; with a path it then opens that file (and line) inside it. A PR the list does not hold (no forge here, not listed) opens as the plain merge preview it always did, with a notice `PR #<n> is not in the pull request list here — opened as a merge preview`. Covers `gg open`, `gg session navigate`, the TUI `#` paste prompt and the web page (all ride the steer navigate).

**Files:**
- Modify: `internal/tui/steer_nav.go` (`pendingSteer.prNumber`; `steerNavigatePreview`; `drainPendingPreview`'s gate)
- Modify: `internal/web/static/live.js` (`steerNavigateLand`'s preview arm), `internal/web/static/prs.js` (export `openPRLanding(n)` → resolves true when the PR view is on screen)
- Modify: i18n bundles (the notice)
- Test: `internal/tui/pr_link_test.go`, `internal/web/pr_link_test.go`

- [ ] **Step 1: TUI tests (RED)** — in `pr_link_test.go`, mirror the existing preview-landing tests (`grep -ln steerNavigatePreview internal/tui/*_test.go`):
  - `TestAPRLinkOpensThePRView`: model with #7 in `m.prs`; `steerNavigate(steer.Command{Cmd: "navigate", Target: &steer.Target{State: "preview", Source: "refs/gg/pr/7", Target: "main"}, File: "big.go"})` → the pending stage is `steerStagePreview` with `prNumber == 7`, and the returned cmd yields a `previewOpenMsg` with `prNumber == 7`; feeding that msg + the compare list lands on `big.go`'s diff (the same drain assertions the merge-preview landing test makes).
  - `TestAPRLinkWithABaseSpellingOfItsOwnStillDrains`: the link's target is `origin/main` while the PR view opens with `main` → still drains (the gate is the number).
  - `TestAnUnlistedPRLinkOpensAMergePreview`: `m.prs` empty → `openPreviewCmd` path (msg `prNumber == 0`) and the notice.

- [ ] **Step 2: run** `go test ./internal/tui/ -run 'PRLink' 2>&1 | tail -4` → FAIL.

- [ ] **Step 3: implement (TUI)**
  - `pendingSteer` gains `prNumber int // steerStagePreview: a PR link — the PR view's number is the gate, not the pair's spelling`.
  - `steerNavigatePreview` head:

```go
	if n, ok := domain.PRScopeNumber(tgt + "..." + src); ok {
		if p, ok := m.listedPR(n); ok {
			if m.width > 0 && m.width < 60 && c.File != "" {
				return m, m.answerSteer(c, steerFail(c, "the terminal is too narrow for the diff view"))
			}
			m = m.steerToPanels()
			if c.File == "" {
				m = m.steerNotice(i18n.T("▸ opened PR #%d", n))
				nm, reply := m.navigateLanded(c, fmt.Sprintf("opened pull request #%d", n))
				return nm, tea.Batch(reply, nm.openPRPreviewCmd(p))
			}
			m.pendingSteer = &pendingSteer{cmd: c, stage: steerStagePreview, prNumber: n, at: time.Now()}
			return m, m.openPRPreviewCmd(p)
		}
		m.statusMsg = i18n.T("PR #%d is not in the pull request list here — opened as a merge preview", n)
	}
```

  (`listedPR(n)` = the row in `m.prs` with that number — add it in `pr_link.go` if no such helper exists; match `navigateLanded`'s real return shape.)
  - `drainPendingPreview`'s gate: `po.source != ps.source || po.target != ps.target` → `!(ps.prNumber != 0 && po.prNumber == ps.prNumber) && (po.source != ps.source || po.target != ps.target)`; the landed/failed wording uses `pull request #<n>` when `ps.prNumber != 0`.
  - If a start-at landing (`gg open <link>` launching the TUI) can arrive before the PR list loads, it would take the fallback: check `startAtOrigin` ordering against the PR listing; if the listing is not part of the start-at wait, park the landing until the first listing (or its failure) arrives — Ruling, ledgered.

- [ ] **Step 4: web test (RED)** — `pr_link_test.go`, static (parallel): `live.js` contains `refs\/gg\/pr\/(\d+)` and `openPRLanding(`; `prs.js` contains `export async function openPRLanding(`. The behaviour is proven by the browser probe below.

- [ ] **Step 5: implement (web)**
  - `prs.js`: make `openPR` return whether the view is shown (`true` after `showPR` answered `"shown"` or after a successful fetch+show; `false` otherwise), and

```js
// openPRLanding opens PR n's view for a gg:// link (live.js): the same open
// as a row click. false = the page does not list PR n.
export async function openPRLanding(n) {
  const pr = knownPR(n);
  return pr ? await openPR(pr) : false;
}
```

  - `live.js` preview arm:

```js
  if (s.state === "preview") {
    const m = /^refs\/gg\/pr\/(\d+)$/.exec(s.source || "");
    const n = m ? Number(m[1]) : 0;
    if (n && (await openPRLanding(n))) {
      if (!s.file) return;
      if (!(await openNamedFile(state.files, s, "pull request #" + n))) return;
    } else {
      if (n) opLine("PR #" + n + " is not in the pull request list here — opened as a merge preview");
      await openPreviewForPair(s.source, s.target);
      if (!s.file) return;
      if (!(await openNamedFile(state.files, s, "preview " + s.target + "..." + s.source))) return;
    }
  } else if …
```

- [ ] **Step 6: run** `go test ./internal/tui/ ./internal/web/ -run 'PRLink|Steer|Navigate|Landing' 2>&1 | tail -4` → ok; `node --check` both JS files.

- [ ] **Step 7: commit** — `feat(links): a pull request's gg:// link opens in its PR view`.

---

### Task 14: skill text + docs for PR links

**Files:**
- Modify: `internal/agentskill/using-gg.md` (`gg link --pr`), `internal/agentskill/gg-review.md` (a PR link reviews the whole PR and stores the review in the PR's view), `internal/agentskill/agentskill.go` (`Version = 156`, `GGReviewVersion = 3`), regenerate `.claude/skills/using-gg/SKILL.md` and `.claude/skills/gg-review/SKILL.md` with a throwaway `go run` of `agentskill` (NOT `gg init --update` before merge)
- Modify: `README.md` (`gg link --pr`, the `L` key), `CHANGELOG.md` (the Task 9 section gains "Links to pull requests"), `docs/CLAUDE-details.md` (link section: the PR link and its landing)

- [ ] **Step 1:** using-gg, in the `gg link` block: `` - `gg link --pr <n> [<path>[:<line>]]` — a pull request's link (`…@<base>...refs/gg/pr/<n>`; fetched PRs only, `gg pr fetch <n>`). Opening it lands in the PR's view; `/gg-review <link>` stores a review the PR's view shows. `` In the `gg pr notes` paragraph: a review FOR a PR is `gg review save "$(gg link --pr <n>)"`.
- [ ] **Step 2:** gg-review.md step 1's refusal line names a pull request link among the accepted kinds; one sentence: "A pull request's link (`@<base>...refs/gg/pr/<n>`) reviews the whole PR; the review shows in its view."
- [ ] **Step 3:** bump the two versions; regenerate the dogfood copies; `go test ./internal/agentskill/ ./internal/agentinit/ 2>&1 | tail -3` → ok (`TestDogfoodSkillCopyInSync` included).
- [ ] **Step 4:** README/CHANGELOG/CLAUDE-details lines.
- [ ] **Step 5: commit** — `docs: pull request links (skill v156, gg-review v3)`.

---

## After the tasks

1. **Browser probe (web-visible: A2, A4 label).** Scratchpad `probe/fixtures.sh <dir> <bin> <port>` with `SRC=` the worktree + a `pscope.mjs` variant: open PR #7, move the fake head (force-push: reset `refs/pull/7/head` to an unrelated commit in the bare repo + `revalidate` blocked so the page keeps the old diff), add a note (`#prompt-input`). OLD build (`~/go/bin/gg`) first: the note is stored plain (count shows on the commit, not in the PR). NEW build (`<worktree>/bin/gg`): the op line shows the red "no longer holds … reopen it", nothing stored. Assert VISIBILITY of the line. A1/B1/B2/B3/B4/B5 get no probe (no visible surface or a race).
   **PR links (Tasks 11–13):** web — right-click the PR row → "copy gg:// link" (assert the menu row is VISIBLE, read the clipboard text via the page's link history `/api/links`), then `gg session navigate <that link with :pr7.txt:1>` against the probe page → the PR view (compare bar names PR #7) with pr7.txt open. TUI — `./tui-capture.sh` on the fixture: Pull requests panel, `L` → status `copied link to PR #7`; then `gg open <link>` lands in the PR view. OLD build first: no menu row, no `L`, and the link opens a plain merge preview.
2. **Race gate:** `./test.sh race > .superpowers/sdd/<plan>/race.log 2>&1` in the background; green only when the log says `all green`.
3. **Final review:** one read-only subagent on the most capable model over `git merge-base main HEAD..HEAD`, with this plan's Review Focus.
4. Ask before merging (`gg merge -F <msgfile> --into main fix/pr-followups-2`). After: `./build.sh install`, `./build.sh web`, `gg init --update`, remove worktree + branch, check main clean, update memory `github-write-feature.md`.

# Review Links (stage 1: link and read) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans.
> This repo forbids implementer subagents (CLAUDE.md): THIS session executes
> every task in order; only the final whole-branch review may be a read-only
> subagent. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A stored AI review gets a `gg://…?review=<id>` link that agents can
construct, read (`gg review show`) and open, and that the user can copy and
open in the TUI and gg web; the commit's Range review and Notes rows get
Copy gg link too.

**Architecture:** One new hint kind (`review`) in `model`; domain builds the
links (`review_link.go`), lists a review's remarks, and checks a review hint
against its address inside `ResolveLink`, so every entry point (CLI, `#`
paste, start-at, steer navigate, gg web) shares one resolution. The TUI and
the page each map the hint to their existing review opener.

**Tech Stack:** Go 1.26, Bubble Tea TUI, loopback web (vanilla JS), MCP
go-sdk, TOML e2e harness.

**Spec:** `docs/superpowers/specs/2026-10-05-review-links-design.md`

## Global Constraints

- Worktree `/work/gigagit/.claude/worktrees/review-links`, branch
  `feat/review-links`; every shell command uses absolute paths (the shell
  cwd resets to the main checkout).
- Frontends never import `internal/git` (archtest); TUI/web reach git only
  through `domain`.
- Every user-visible TUI string: `i18n.T("<literal>")` + an entry in ALL FOUR
  bundles `internal/i18n/lang/{ja,ko,zh,ru}.toml` (adding-translations
  skill). CLI/engine prose stays English.
- No git work on the TUI Update thread: link building runs in a `tea.Cmd`.
- Hint ids reject `@:#?/` and whitespace (`model.LinkHintIDOK`); review ids
  are 8 hex.
- Tests use real git in `t.TempDir()`; new TUI tests call `t.Parallel()`.
- Staging: `gg add <explicit paths>` then `git commit -F <scratchpad msg>`
  (gg commit has no -F); never `git add -A` (a built `bin/gg` may exist).
- Commit trailer: `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`
  + `Claude-Session: https://claude.ai/code/session_01GaVoNSXah5Ti1LcfmvUoXc`.
- Race gate is green only when `./test.sh race` prints `all green`.

## Deviations from the spec (decided while planning)

- The address-less `gg://<repo>?review=<id>` is refused at RESOLVE time in
  `finishLink` (the existing pattern for `preview`/`version`), not at parse
  time — `ParseLink` stays address-agnostic for hints.
- gg web asks the server for the Range review / Notes row links too
  (`GET /api/notes/row-link`) instead of building them with `linkFor`: one
  builder (domain) for all three row kinds, no JS twin to keep in step.
- `latest` is accepted wherever a review id is (`gg link --review latest`,
  `gg review show latest`), mirroring `gg link --version … latest`; it lets
  the e2e scenario and an agent name "the review just made".
- gg web's View all notes has no row menu today; it gets none here (the TUI's
  All notes gets `ctrl+l`).

## Review Focus

1. A branch literally named `show` — `gg review show` must still mean the
   subcommand; `gg review refs/heads/show` reviews the branch (Task 3 test).
2. A review link whose review was deleted — the address still opens, with a
   notice, in the TUI and web; `gg review show` exits 1 (Tasks 3, 5, 7).
3. A review link edited to another commit (`@<other>?review=<id>`) — refused
   everywhere, never opens the wrong review (Task 2 test).
4. A prose review (no review document) — `gg review show` prints the text,
   `remarks: []`; Copy gg link still works (Tasks 2, 3).
5. An old-side remark and a multi-line remark — the remark's own link is
   `:old:<n>` / `:<a>-<b>` and reparses (Task 2 test).

---

### Task 1: The `review` hint kind

**Files:**
- Modify: `internal/model/link.go` (linkHintKinds ~line 123, its comment, the
  parseLinkHint error text ~line 685, `LinkHint.Kind` comment ~line 92)
- Modify: `internal/web/static/links.js:62` (`linkHintKindOK` twin)
- Modify: `internal/web/steer.go` comment listing the kinds (~line 244)
- Test: `internal/model/link_test.go`

**Interfaces:**
- Produces: `model.LinkHintKindOK("review") == true`; `model.ReviewHintKind = "review"`.

- [ ] **Step 1: Failing test** — append to `internal/model/link_test.go`:

```go
// A review hint rides a commit or a pair link and round-trips; its kind is
// in the closed set the parse error names.
func TestReviewHintRoundTrips(t *testing.T) {
	for _, s := range []string{
		"gg://github.com/o/r@0123456789abcdef0123456789abcdef01234567?review=1a2b3c4d",
		"gg://github.com/o/r@0123456789abcdef0123456789abcdef01234567..89abcdef0123456789abcdef0123456789abcdef?review=1a2b3c4d",
	} {
		l, err := ParseLink(s)
		if err != nil {
			t.Fatalf("ParseLink(%q): %v", s, err)
		}
		if l.Hint != (LinkHint{Kind: ReviewHintKind, ID: "1a2b3c4d"}) || l.String() != s {
			t.Fatalf("%q → %+v → %q", s, l.Hint, l.String())
		}
	}
	_, err := ParseLink("gg://github.com/o/r@abc1234?nope=1")
	if err == nil || !strings.Contains(err.Error(), "review") {
		t.Fatalf("the unknown-kind error must list review: %v", err)
	}
}
```

- [ ] **Step 2: Run** `go test /work/gigagit/.claude/worktrees/review-links/internal/model -run TestReviewHintRoundTrips -count=1` (from the worktree: `cd <wt> && go test ./internal/model -run …`). Expected: FAIL (`ReviewHintKind` undefined).

- [ ] **Step 3: Implement** in `internal/model/link.go`:

```go
// "review" names a stored AI REVIEW (a review note's id). The link's address
// is the change the review compared — @<commit> for one commit, @<base>..<tip>
// for a range or a branch — so every hint-blind verb sees the reviewed
// change; the consumer opens the review itself. A lookup key, machine-local
// like "version": reviews live in the local note store.
var linkHintKinds = map[string]bool{"bookmark": true, "shelf": true, "stash": true, "preview": true, "view": true, "version": true, "review": true}

// ReviewHintKind is the hint a review link carries.
const ReviewHintKind = "review"
```

Error text: `"%w: unknown hint kind %q (want bookmark, shelf, stash, preview, version, review or view)"`. `LinkHint.Kind` comment: list every kind. `links.js`: add `|| kind === "review"`. `web/steer.go` comment: add "review".

- [ ] **Step 4: Run** `go test ./internal/model ./internal/web -count=1` → PASS (a JS/Go twin test, if any, now agrees).

- [ ] **Step 5: Commit** `feat(model): a review hint kind for gg:// links` (paths: the four files).

---

### Task 2: Domain — build, describe, check, list remarks

**Files:**
- Create: `internal/domain/review_link.go`
- Modify: `internal/domain/linkresolve.go` (address-less arm in `finishLink` ~line 523; the mismatch check after `finishLink` returns in `ResolveLink`)
- Modify: `internal/domain/linkdesc.go` (~line 110 switch: a `review` arm)
- Test: `internal/domain/review_link_test.go`

**Interfaces:**
- Consumes: `model.ReviewHintKind` (Task 1); existing `Review(ctx,id)`, `Reviews(ctx)`, `ReviewRevs`, `ScopeAtCommit`, `LinkRepo`, `ResolveRev`, `ErrReviewNotFound`.
- Produces:
  - `func (s *Service) ReviewID(ctx context.Context, idOrLatest string) (string, error)` — `latest` = newest `Created`; `ErrReviewNotFound` when none.
  - `func (s *Service) ReviewLink(ctx context.Context, id string) (string, error)` — link text.
  - `func (s *Service) ScopeLinkText(ctx context.Context, scope, commit string) (string, error)` — `gg://<repo>@<a>..<b>`.
  - `func (s *Service) CommitFileLinkText(ctx context.Context, commit, path string) (string, error)` — `gg://<repo>/<path>@<commit>`.
  - `type ReviewRemark struct { N int; Path string; Side model.NoteSide; Start, End int; Summary, Rationale string; Meta []notebatch.MetaKV; Link string }`
  - `func (s *Service) ReviewRemarks(ctx context.Context, r Review) ([]ReviewRemark, error)`
  - `var ErrReviewLinkMismatch = errors.New("the link does not match the review")`
  - `ResolveLink` refuses an address-less review hint (`model.ErrLink`) and a mismatched one (`ErrReviewLinkMismatch` wrapped in `model.ErrLink`); a deleted review passes (consumers notice).

- [ ] **Step 1: Failing tests** — `internal/domain/review_link_test.go`. Fixture: a real repo with three commits (`newRepoDir`-style helper already in domain tests; use `domain.Open(dir)` + `svc.UseNotesDir(t.TempDir())`), a single-commit review of HEAD and a range review `HEAD~2..HEAD`, saved with `SaveReview` and a document text holding a new-side multi-line note and an old-side note:

```go
const linkReviewDoc = `{"version":1,"summary":"ok","files":[{"path":"a.txt","annotations":[
 {"newRange":[1,2],"summary":"two lines","rationale":"why"},
 {"oldRange":[1,1],"summary":"removed line"}]}]}`

func TestReviewLinkSingleAndRange(t *testing.T) {
	t.Parallel()
	svc, dir, single, rng := reviewLinkFixture(t) // returns the two review ids
	ctx := context.Background()
	head := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))
	base := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD~2"))
	l1, err := svc.ReviewLink(ctx, single)
	if err != nil || !strings.HasSuffix(l1, "@"+head+"?review="+single) {
		t.Fatalf("single = %q, %v", l1, err)
	}
	l2, err := svc.ReviewLink(ctx, rng)
	if err != nil || !strings.HasSuffix(l2, "@"+base+".."+head+"?review="+rng) {
		t.Fatalf("range = %q, %v", l2, err)
	}
	if id, err := svc.ReviewID(ctx, "latest"); err != nil || id != rng {
		t.Fatalf("latest = %q, %v, want the range review (saved last)", id, err)
	}
}

func TestReviewRemarksCarryTheirOwnLinks(t *testing.T) {
	t.Parallel()
	svc, _, _, rng := reviewLinkFixture(t)
	ctx := context.Background()
	r, _ := svc.Review(ctx, rng)
	rs, err := svc.ReviewRemarks(ctx, r)
	if err != nil || len(rs) != 2 {
		t.Fatalf("remarks = %+v, %v", rs, err)
	}
	if rs[0].N != 0 || rs[0].Start != 1 || rs[0].End != 2 || !strings.Contains(rs[0].Link, "/a.txt@") || !strings.HasSuffix(rs[0].Link, ":1-2") {
		t.Fatalf("remark 0 = %+v", rs[0])
	}
	if rs[1].N != 1 || rs[1].Side != model.NoteSideOld || !strings.Contains(rs[1].Link, ":old:1") {
		t.Fatalf("remark 1 = %+v", rs[1])
	}
	for _, x := range rs {
		if _, err := model.ParseLink(x.Link); err != nil {
			t.Fatalf("remark link %q does not reparse: %v", x.Link, err)
		}
	}
}

func TestReviewHintIsChecked(t *testing.T) {
	t.Parallel()
	svc, dir, single, _ := reviewLinkFixture(t)
	ctx := context.Background()
	opts := ResolveOpts{Cwd: svc}
	good, _ := svc.ReviewLink(ctx, single)
	if res, err := ResolveLink(ctx, mustParse(t, good), opts); err != nil || res.Hint.Kind != model.ReviewHintKind {
		t.Fatalf("good link: %+v, %v", res, err)
	}
	other := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD~1"))
	bad := good[:strings.LastIndex(good, "@")+1] + other + "?review=" + single
	if _, err := ResolveLink(ctx, mustParse(t, bad), opts); !errors.Is(err, ErrReviewLinkMismatch) {
		t.Fatalf("a link moved to another commit must be refused: %v", err)
	}
	bare := good[:strings.LastIndex(good, "@")] + "?review=" + single
	if _, err := ResolveLink(ctx, mustParse(t, bare), opts); !errors.Is(err, model.ErrLink) {
		t.Fatalf("an address-less review link must be refused: %v", err)
	}
	if err := svc.NoteRemove(ctx, single); err != nil {
		t.Fatal(err)
	}
	if res, err := ResolveLink(ctx, mustParse(t, good), opts); err != nil || res.Hint.ID != single {
		t.Fatalf("a deleted review still resolves its address (consumers notice): %+v, %v", res, err)
	}
}

// A prose review (no document) has no remarks and still links.
func TestProseReviewLinksWithNoRemarks(t *testing.T) {
	t.Parallel()
	svc, dir, _, _ := reviewLinkFixture(t)
	ctx := context.Background()
	head := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))
	tg := ReviewTarget{Kind: ReviewRange, Range: head + "^.." + head, Commit: head}
	id, _, err := svc.SaveReview(ctx, SaveReview{Target: tg, Agent: "Claude", Text: "# Verdict\nfine"})
	if err != nil {
		t.Fatal(err)
	}
	r, _ := svc.Review(ctx, id)
	if rs, err := svc.ReviewRemarks(ctx, r); err != nil || rs == nil || len(rs) != 0 {
		t.Fatalf("prose remarks = %#v, %v, want an empty non-nil list", rs, err)
	}
	if l, err := svc.ReviewLink(ctx, id); err != nil || !strings.Contains(l, "?review="+id) {
		t.Fatalf("prose link = %q, %v", l, err)
	}
}
```

Write `reviewLinkFixture(t) (svc *Service, dir, singleID, rangeID string)` and `mustParse(t, s) model.Link` in the same file: a repo via `git init -b main`, three commits on `a.txt` (each rewriting its 3 lines); `SaveReview` with `ReviewTarget{Kind: ReviewRange, Range: head+"^.."+head, Commit: head}` and text `linkReviewDoc`, then `{Kind: ReviewRange, Range: base+".."+head, Commit: head}` with the same text (saved second, so it is `latest`). `ResolveOpts`: copy the opts helper the existing `linkresolve` tests use (grep `ResolveOpts{` in `internal/domain/*_test.go`) so the repo resolves by path.

- [ ] **Step 2: Run** `go test ./internal/domain -run 'ReviewLink|ReviewRemarks|ReviewHint|ProseReview' -count=1` → FAIL (undefined).

- [ ] **Step 3: Implement** `internal/domain/review_link.go`:

```go
package domain

// Review links (spec 2026-10-05-review-links): a stored review's gg:// link
// is the change it compared plus ?review=<id>; its remarks each carry their
// own line link so an agent can read the exact code (`gg link text`).

var ErrReviewLinkMismatch = errors.New("the link does not match the review")

func (s *Service) ReviewID(ctx context.Context, idOrLatest string) (string, error) {
	if idOrLatest != "latest" {
		if _, err := s.Review(ctx, idOrLatest); err != nil {
			return "", err
		}
		return idOrLatest, nil
	}
	rs, err := s.Reviews(ctx)
	if err != nil {
		return "", err
	}
	var best Review
	for _, r := range rs {
		if best.ID == "" || r.Created.After(best.Created) {
			best = r
		}
	}
	if best.ID == "" {
		return "", ErrReviewNotFound
	}
	return best.ID, nil
}

// reviewTarget is the link target of review r: its commit, or its range as
// two full shas.
func (s *Service) reviewTarget(ctx context.Context, r Review) (model.LinkTarget, error) {
	base, tip, isRange := s.ReviewRevs(ctx, r)
	full := func(rev string) (string, error) {
		h, ok, err := s.ResolveRev(ctx, rev)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", fmt.Errorf("review %s: %s does not resolve here", r.ID, rev)
		}
		return strings.TrimSpace(h), nil
	}
	t, err := full(tip)
	if err != nil {
		return model.LinkTarget{}, err
	}
	if !isRange {
		return model.LinkTarget{State: model.StateCommitted, Commit: t}, nil
	}
	b, err := full(base)
	if err != nil {
		return model.LinkTarget{}, err
	}
	return model.LinkTarget{State: model.StateCommitted, Pair: &model.LinkPair{A: b, B: t}}, nil
}

func (s *Service) linkText(ctx context.Context, l model.Link) (string, error) {
	repo, err := s.LinkRepo(ctx)
	if err != nil {
		return "", err
	}
	l.Repo = repo
	if l.Side == "" {
		l.Side = model.NoteSideNew
	}
	text := l.String()
	if _, err := model.ParseLink(text); err != nil {
		return "", fmt.Errorf("cannot express %s as a gg link: %w", text, err)
	}
	return text, nil
}

func (s *Service) ReviewLink(ctx context.Context, id string) (string, error) {
	r, err := s.Review(ctx, id)
	if err != nil {
		return "", err
	}
	t, err := s.reviewTarget(ctx, r)
	if err != nil {
		return "", err
	}
	return s.linkText(ctx, model.Link{Target: t, Hint: model.LinkHint{Kind: model.ReviewHintKind, ID: r.ID}})
}

func (s *Service) ScopeLinkText(ctx context.Context, scope, commit string) (string, error) {
	a, b, err := s.ScopeAtCommit(ctx, scope, commit)
	if err != nil {
		return "", err
	}
	return s.linkText(ctx, model.Link{Target: model.LinkTarget{State: model.StateCommitted, Pair: &model.LinkPair{A: a, B: b}}})
}

func (s *Service) CommitFileLinkText(ctx context.Context, commit, path string) (string, error) {
	return s.linkText(ctx, model.Link{Path: path, Target: model.LinkTarget{State: model.StateCommitted, Commit: commit}})
}

type ReviewRemark struct {
	N                  int
	Path               string
	Side               model.NoteSide
	Start, End         int
	Summary, Rationale string
	Meta               []notebatch.MetaKV
	Link               string
}

// ReviewRemarks are r's document notes in document order. N is the index the
// read-time note ids use (<ReviewNoteIDPrefix><id>:<n>), stable for answers.
func (s *Service) ReviewRemarks(ctx context.Context, r Review) ([]ReviewRemark, error) {
	if r.Doc == nil {
		return []ReviewRemark{}, nil
	}
	t, err := s.reviewTarget(ctx, r)
	if err != nil {
		return nil, err
	}
	out := []ReviewRemark{}
	n := 0
	for _, f := range r.Doc.Files {
		for _, dn := range f.Notes {
			side := model.NoteSideNew
			if dn.Side == "old" {
				side = model.NoteSideOld
			}
			rm := ReviewRemark{N: n, Path: f.Path, Side: side, Start: dn.Range[0], End: dn.Range[1],
				Summary: dn.Summary, Rationale: dn.Rationale, Meta: dn.Meta}
			n++
			l := model.Link{Path: f.Path, Target: t, Side: side, Line: rm.Start}
			if rm.End > rm.Start {
				l.End = rm.End
			}
			if text, err := s.linkText(ctx, l); err == nil {
				rm.Link = text // a path a link cannot spell keeps no link, never fails the list
			}
			out = append(out, rm)
		}
	}
	return out, nil
}

// checkReviewHint refuses a resolved review link whose address is not the
// change its review compared. A review this store no longer holds passes:
// the address still means something and the consumer says the review is gone.
func checkReviewHint(ctx context.Context, svc *Service, res Resolved) error {
	r, err := svc.Review(ctx, res.Hint.ID)
	if errors.Is(err, ErrReviewNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	t, err := svc.reviewTarget(ctx, r)
	if err != nil {
		return err
	}
	match := t.Pair == nil && res.Pair == nil && res.Commit == t.Commit ||
		t.Pair != nil && res.Pair != nil && *res.Pair == *t.Pair
	if !match {
		return fmt.Errorf("%w: %w (review %s compared %s)", model.ErrLink, ErrReviewLinkMismatch, r.ID, targetText(t))
	}
	return nil
}

func targetText(t model.LinkTarget) string {
	if t.Pair != nil {
		return t.Pair.A[:7] + ".." + t.Pair.B[:7]
	}
	return t.Commit[:7]
}
```

(Before writing, confirm the exact `model.Link` field names for path/line/end — `Path`, `Line`, `End`, `Side` per `Resolved`'s copy in `finishLink` — and that `Review.Doc.Files[].Notes[]` is `notebatch.ReviewNote`.)

In `finishLink`'s address-less switch add:

```go
		case model.ReviewHintKind:
			// A review IS the change it compared — the hint only says which
			// review — so without an address the link names nothing.
			return Resolved{}, fmt.Errorf("%w: a review hint needs the change it reviewed (@<commit> or @<base>..<tip>); %q alone names nothing", model.ErrLink, l.Hint.ID)
```

In `ResolveLink`, right after the successful `finishLink(...)` call:

```go
	if res.Hint.Kind == model.ReviewHintKind {
		if err := checkReviewHint(ctx, opts.OpenFn(res.Checkout), res); err != nil {
			return Resolved{}, err
		}
	}
```

`linkdesc.go` switch, a `review` arm: `if r, err := s.Review(ctx, l.Hint.ID); err == nil { return "review", reviewDescLabel(r), "" }` where `reviewDescLabel` = `r.Agent + " review of " + short(r.Commit)` (branch-qualified when `r.Branch != ""`); a miss falls through like `version`.

- [ ] **Step 4: Run** the Step 2 command → PASS; then `go test ./internal/domain -count=1` → PASS.

- [ ] **Step 5: Commit** `feat(domain): review links, remarks and the review-hint check`.

---

### Task 3: CLI — `gg link --review`, `gg review show`, resolve output

**Files:**
- Modify: `internal/cli/link.go` (flag + branch next to `--version` ~line 57/66; `linkUsage`; resolve text output ~line 575)
- Create: `internal/cli/review_show.go`
- Modify: `internal/cli/review.go:30` (dispatch `show` before flag parsing)
- Test: `internal/cli/review_show_test.go`

**Interfaces:**
- Consumes: Task 2's `ReviewID`, `ReviewLink`, `ReviewRemarks`, `ResolveLink` (via `linkResolveOpts(RepoStatePath, svc)`), `ErrReviewNotFound`.
- Produces: `wireReviewShow` JSON (used by MCP in Task 4):

```go
type wireReviewRemark struct {
	N         int               `json:"n"`
	Path      string            `json:"path"`
	Side      string            `json:"side"`
	Start     int               `json:"start"`
	End       int               `json:"end"`
	Summary   string            `json:"summary"`
	Rationale string            `json:"rationale,omitempty"`
	Meta      map[string]string `json:"meta,omitempty"`
	Link      string            `json:"link,omitempty"`
}
type wireReviewShow struct {
	ID       string             `json:"id"`
	Agent    string             `json:"agent"`
	Created  time.Time          `json:"created"`
	Branch   string             `json:"branch,omitempty"`
	Base     string             `json:"base"`
	Tip      string             `json:"tip"`
	Link     string             `json:"link"`
	Overview string             `json:"overview"`
	Meta     map[string]string  `json:"meta,omitempty"`
	Remarks  []wireReviewRemark `json:"remarks"`
}
```

The shape above is written in DOMAIN as `domain.ReviewShow` / `domain.ReviewShowRemark` (same fields and JSON tags — `mcp` is domain-only and cannot import `cli`), with `func (s *Service) ReviewShow(ctx context.Context, id string) (ReviewShow, error)` in `internal/domain/review_link.go`; CLI and MCP both marshal it. The `wire…` names above are only the field listing.

- [ ] **Step 1: Failing tests** (`runCLI(t, dir, args...)` is the existing helper; fixture = a repo with one structured review saved through `svc.SaveReview` as in Task 2):

```go
func TestLinkReviewPrintsTheReviewLink(t *testing.T) {
	t.Parallel()
	dir, id := reviewedRepo(t)
	code, out, errb := runCLI(t, dir, "link", "--review", id)
	if code != 0 || !strings.Contains(out, "?review="+id) {
		t.Fatalf("link --review = %d %q %q", code, out, errb)
	}
	if code, out, _ = runCLI(t, dir, "link", "--review", "latest"); code != 0 || !strings.Contains(out, "?review="+id) {
		t.Fatalf("latest = %d %q", code, out)
	}
	if code, _, _ = runCLI(t, dir, "link", "--review", "deadbeef"); code != 1 {
		t.Fatalf("unknown id = %d, want 1", code)
	}
}

func TestReviewShowTextAndJSON(t *testing.T) {
	t.Parallel()
	dir, id := reviewedRepo(t)
	_, link, _ := runCLI(t, dir, "link", "--review", id)
	link = strings.TrimSpace(link)
	for _, arg := range []string{id, link, "latest"} {
		code, out, errb := runCLI(t, dir, "review", "show", arg)
		if code != 0 || !strings.Contains(out, "review "+id) || !strings.Contains(out, "[0] a.txt:1-2 — two lines") ||
			!strings.Contains(out, "    why") || !strings.Contains(out, "a.txt@") {
			t.Fatalf("review show %s = %d\n%s\n%s", arg, code, out, errb)
		}
	}
	code, out, _ := runCLI(t, dir, "review", "show", "--json", id)
	var w domain.ReviewShow
	if code != 0 || json.Unmarshal([]byte(out), &w) != nil || w.ID != id || len(w.Remarks) != 2 || w.Remarks[1].Side != "old" {
		t.Fatalf("json = %d %s", code, out)
	}
	if code, _, _ = runCLI(t, dir, "review", "show", "deadbeef"); code != 1 {
		t.Fatalf("unknown = %d, want 1", code)
	}
	if code, _, _ = runCLI(t, dir, "review", "show", "gg://nope@@"); code != 2 {
		t.Fatalf("malformed = %d, want 2", code)
	}
}

// `show` is the subcommand; a branch named show is reviewed by its full ref.
func TestReviewShowIsASubcommand(t *testing.T) {
	t.Parallel()
	dir, _ := reviewedRepo(t)
	runGit(t, dir, "branch", "show")
	if code, _, errb := runCLI(t, dir, "review", "show"); code != 2 || !strings.Contains(errb, "usage: gg review show") {
		t.Fatalf("bare show = %d %q (want the show usage)", code, errb)
	}
}

func TestLinkResolvePrintsTheReview(t *testing.T) {
	t.Parallel()
	dir, id := reviewedRepo(t)
	_, link, _ := runCLI(t, dir, "link", "--review", id)
	_, out, _ := runCLI(t, dir, "link", "resolve", strings.TrimSpace(link))
	if !strings.Contains(out, "review "+id) {
		t.Fatalf("resolve = %q", out)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/cli -run 'LinkReview|ReviewShow|LinkResolvePrintsTheReview' -count=1` → FAIL.

- [ ] **Step 3: Implement.**
  - domain (`review_link.go`): `type ReviewShow` (fields + JSON tags as above; Meta as `map[string]string` built from `[]MetaKV`), `func (s *Service) ReviewShow(ctx context.Context, id string) (ReviewShow, error)` = Review + `ReviewRevs` (full shas via `reviewTarget`) + `ReviewLink` + `ReviewRemarks`; `Overview` = `Doc.Overview`, or `Text` for a prose review; `Remarks` never nil.
  - `internal/cli/review.go` top of `cmdReview`: `if len(rest) > 0 && rest[0] == "show" { return reviewShow(svc, rest[1:], stdout, stderr) }`.
  - `internal/cli/review_show.go`:

```go
const reviewShowUsage = "usage: gg review show [--json] <review-link|id|latest>"

func reviewShow(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("review show", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print the review as JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, reviewShowUsage)
		return 2
	}
	ctx := context.Background()
	id, code := reviewArgID(ctx, svc, fs.Arg(0), stderr)
	if code != 0 {
		return code
	}
	rs, err := svc.ReviewShow(ctx, id)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if *asJSON {
		return writeJSON(stdout, stderr, rs) // the package's existing JSON writer; else json.NewEncoder
	}
	printReviewShow(stdout, rs)
	return 0
}

// reviewArgID turns a review link, an id or "latest" into a stored review's id.
func reviewArgID(ctx context.Context, svc *domain.Service, arg string, stderr io.Writer) (string, int) {
	if strings.HasPrefix(arg, "gg://") {
		l, err := model.ParseLink(arg)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return "", 2
		}
		if l.Hint.Kind != model.ReviewHintKind {
			fmt.Fprintln(stderr, "error: that link names no review (no ?review=<id>)")
			return "", 2
		}
		if _, err := domain.ResolveLink(ctx, l, linkResolveOpts(RepoStatePath, svc)); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			if errors.Is(err, model.ErrLink) && !errors.Is(err, domain.ErrReviewLinkMismatch) {
				return "", 2
			}
			return "", 1
		}
		arg = l.Hint.ID
	}
	id, err := svc.ReviewID(ctx, arg)
	if err != nil {
		if errors.Is(err, domain.ErrReviewNotFound) {
			fmt.Fprintf(stderr, "error: review %s not found\n", arg)
		} else {
			fmt.Fprintln(stderr, "error:", err)
		}
		return "", 1
	}
	return id, 0
}

func printReviewShow(w io.Writer, rs domain.ReviewShow) {
	what := shortSha(rs.Tip)
	if rs.Base != "" {
		what = shortSha(rs.Base) + ".." + shortSha(rs.Tip)
	}
	if rs.Branch != "" {
		what += " (" + rs.Branch + ")"
	}
	fmt.Fprintf(w, "review %s · %s · %s · %s\n", rs.ID, rs.Agent, rs.Created.Local().Format("2006-01-02 15:04"), what)
	fmt.Fprintln(w, strings.TrimRight(rs.Overview, "\n"))
	if len(rs.Meta) > 0 {
		fmt.Fprintf(w, "\n%s\n", metaMapText(rs.Meta))
	}
	if len(rs.Remarks) > 0 {
		fmt.Fprintln(w)
	}
	for _, r := range rs.Remarks {
		line := fmt.Sprint(r.Start)
		if r.End > r.Start {
			line += fmt.Sprintf("-%d", r.End)
		}
		if r.Side == "old" {
			line = "-" + line
		}
		fmt.Fprintf(w, "[%d] %s:%s — %s", r.N, r.Path, line, r.Summary)
		if len(r.Meta) > 0 {
			fmt.Fprintf(w, " (%s)", metaMapText(r.Meta))
		}
		fmt.Fprintln(w)
		if r.Rationale != "" {
			fmt.Fprintln(w, "    "+strings.ReplaceAll(strings.TrimSpace(r.Rationale), "\n", "\n    "))
		}
		if r.Link != "" {
			fmt.Fprintln(w, "    "+r.Link)
		}
	}
}
```

  (`shortSha`: reuse the package's existing short-hash helper — grep `func short` in `internal/cli`; `metaMapText` = sorted `key: value` joined by `, `, matching `metaText`'s format. `Base` is empty for a single-commit review.)
  - `internal/cli/link.go`: `review := fs.String("review", "", "a stored AI REVIEW's link: --review <id|latest> (ids from gg review / gg note list)")`; a branch mirroring `--version` (no other flag, no positional) → `linkReview(svc, *review, stdout, stderr)`: `ReviewID` → `ReviewLink` → `svc.RecordLink(ctx, text, "review: "+id)` → print; unknown → exit 1 `error: review <x> not found`. Add the flag to `linkUsage`.
  - resolve text output: after the existing `line` is printed, `if res.Hint.Kind == model.ReviewHintKind { fmt.Fprintln(stdout, "review "+res.Hint.ID) }`.

- [ ] **Step 4: Run** Step 2's command → PASS; `go test ./internal/cli -count=1` → PASS.

- [ ] **Step 5: Commit** `feat(cli): gg link --review and gg review show`.

---

### Task 4: MCP — `gg_review_show`

**Files:**
- Modify: `internal/mcp/links.go` (a tool after `gg_link_text`, ~line 190)
- Modify: `internal/mcp/server_test.go:146` (the expected tool set)
- Test: `internal/mcp/links_test.go`

**Interfaces:** Consumes `domain.ReviewShow` (Task 3) and `s.resolveLinkArg`.

- [ ] **Step 1: Failing test** in `links_test.go`, following the file's existing `gg_link_text` test (same server helper): save a structured review, call `gg_review_show` with `{"link": <ReviewLink>}` and with `{"link": <id>}`; assert `id`, two remarks, `remarks[0].link` non-empty. Add `"gg_review_show": true` to the tool set in `server_test.go`.

- [ ] **Step 2: Run** `go test ./internal/mcp -run 'ReviewShow|Tools' -count=1` → FAIL.

- [ ] **Step 3: Implement:**

```go
	sdk.AddTool(srv, &sdk.Tool{
		Name: "gg_review_show",
		Description: "A stored AI review: its overview and every remark (path, side, lines, summary, rationale) with the remark's own gg:// line link. " +
			"Pass a review link (gg://…?review=<id>), a review id, or \"latest\". Use it to check another agent's review.",
		Annotations: readOnlyAnnotations(),
	}, func(ctx context.Context, _ *sdk.CallToolRequest, in linkResolveIn) (*sdk.CallToolResult, domain.ReviewShow, error) {
		var out domain.ReviewShow
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		id := strings.TrimSpace(in.Link)
		if strings.HasPrefix(id, "gg://") {
			res, err := s.resolveLinkArg(ctx, id)
			if err != nil {
				return nil, out, err
			}
			if res.Hint.Kind != model.ReviewHintKind {
				return nil, out, errors.New("that link names no review (no ?review=<id>)")
			}
			id = res.Hint.ID
		}
		id, err := s.svc.ReviewID(ctx, id)
		if err != nil {
			return nil, out, err
		}
		out, err = s.svc.ReviewShow(ctx, id)
		return nil, out, err
	})
```

- [ ] **Step 4: Run** Step 2 → PASS; `go test ./internal/mcp -count=1` → PASS.
- [ ] **Step 5: Commit** `feat(mcp): gg_review_show`.

---

### Task 5: TUI — a review link opens the review

**Files:**
- Modify: `internal/tui/steer_nav.go` (`steerNavigate` ~line 182: an early `review` case; `navigateLanded`'s hint switch: `review` never reaches it)
- Create: `internal/tui/review_hint.go`
- Modify: `internal/tui/model.go` (dispatch `reviewHintMsg`)
- i18n: 1 new key in the four bundles
- Test: `internal/tui/review_hint_test.go`

**Interfaces:**
- Consumes: `model.ReviewHintKind`; existing `openReviewFrom(id, title, back)`, `reviewTitle`, `steerToPanels`, `answerSteer`, `steerOK`, `startAtOrigin`.
- Produces: `type reviewHintMsg struct { cmd steer.Command; commit string; found bool; err error }`.

- [ ] **Step 1: Failing tests** — reuse `reviewViewModel(t, reviewViewDoc)` (review_view_test.go) for a model + review id:

```go
// A navigate carrying ?review= opens the review view (the user's paste,
// gg open, an agent's navigate — one steer path), back to its commit.
func TestReviewLinkOpensTheReview(t *testing.T) {
	t.Parallel()
	m, id := reviewViewModel(t, reviewViewDoc)
	sha := m.commits[0].Hash
	c := steer.Command{Cmd: "navigate", Commit: sha, HintKind: model.ReviewHintKind, HintID: id, Origin: "start-at"}
	m, cmd := m.steerNavigate(c)
	m = drainCmds(t, m, cmd)
	if m.filesReview == nil || m.filesReview.id != id {
		t.Fatalf("the review did not open: %+v", m.filesReview)
	}
	if m.filesReview.back.Hash != sha {
		t.Fatalf("back = %q, want the review's commit", m.filesReview.back.Hash)
	}
}

// A deleted review: a notice, then the commit opens as a plain link would.
func TestDeletedReviewLinkLandsTheCommit(t *testing.T) {
	t.Parallel()
	m, id := reviewViewModel(t, reviewViewDoc)
	sha := m.commits[0].Hash
	if err := m.svc.NoteRemove(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	c := steer.Command{Cmd: "navigate", Commit: sha, HintKind: model.ReviewHintKind, HintID: id, Origin: "start-at"}
	m, cmd := m.steerNavigate(c)
	m = drainCmds(t, m, cmd)
	if m.filesReview != nil || !strings.Contains(m.statusMsg, "no longer exists") {
		t.Fatalf("review=%v status=%q", m.filesReview, m.statusMsg)
	}
}
```

(Check `steer.Command`'s exact origin field name for a user-started landing — `startAtOrigin(c)` reads it — and use it. Add a third test through the `#` paste path: find the paste handler that calls `linknav.Command` (grep `linknav.Command` in `internal/tui`) and drive it with `m.svc.ReviewLink(ctx, id)`'s text; assert `m.filesReview.id == id`.)

- [ ] **Step 2: Run** `go test ./internal/tui -run 'ReviewLinkOpens|DeletedReviewLink|PastedReviewLink' -count=1` → FAIL.

- [ ] **Step 3: Implement** `internal/tui/review_hint.go`:

```go
// A review link (?review=<id>) lands on the REVIEW, not its commit: every
// entry point (the # paste, gg open's start-at, an agent's navigate) arrives
// here through steerNavigate. The review is looked up off the Update thread;
// a deleted one degrades to the plain landing of its address with a notice.

type reviewHintMsg struct {
	cmd    steer.Command
	commit string
	found  bool
	err    error
}

func (m Model) steerNavigateReview(c steer.Command) (Model, tea.Cmd) {
	svc := m.svc
	if svc == nil {
		return m, m.answerSteer(c, steerFail(c, "no repository"))
	}
	return m, func() tea.Msg {
		r, err := svc.Review(context.Background(), c.HintID)
		if errors.Is(err, domain.ErrReviewNotFound) {
			return reviewHintMsg{cmd: c}
		}
		return reviewHintMsg{cmd: c, commit: r.Commit, found: err == nil, err: err}
	}
}

func (m Model) onReviewHint(msg reviewHintMsg) (Model, tea.Cmd) {
	c := msg.cmd
	if !msg.found {
		plain := c
		plain.HintKind, plain.HintID = "", ""
		nm, cmd := m.steerNavigate(plain)
		if msg.err == nil {
			nm.statusMsg = i18n.T("That review no longer exists; opened its change")
		} else {
			nm.statusMsg = i18n.T("review: %s", msg.err.Error())
		}
		return nm, cmd
	}
	nm := m.steerToPanels()
	nm, open := nm.openReviewFrom(c.HintID, reviewTitle(shortHash(msg.commit)), model.Commit{Hash: msg.commit})
	return nm, tea.Batch(open, nm.answerSteer(c, steerOK(c, "opened review "+c.HintID)))
}
```

In `steerNavigate`, first case after `c.Step != ""`: `case c.HintKind == model.ReviewHintKind: return m.steerNavigateReview(c)`. In `model.go` Update: `case reviewHintMsg: return m.onReviewHint(msg)`. Bundle key: `"That review no longer exists; opened its change"` (ja/ko/zh/ru; `"review: %s"` likely exists — check, add if not).

- [ ] **Step 4: Run** Step 2 → PASS; `go test ./internal/tui -count=1` → PASS (i18n gates included).
- [ ] **Step 5: Commit** `feat(tui): a review link opens the review`.

---

### Task 6: TUI — Copy gg link on note rows, the review view, Branches, All notes

**Files:**
- Modify: `internal/tui/note_row_menu.go` (three items per row; `asyncCopyLinkRow`)
- Modify: `internal/tui/review_delete.go` / `action_menu.go` (review view `.` menu: Copy review link; Branches review sub-row: Copy gg link — next to `showBranchReviewRow`, ~action_menu.go:305)
- Modify: `internal/tui/all_notes_popup.go` (`ctrl+l` on an `anReview` row; hint line ~647)
- i18n: `"Copy gg link"`, `"Copy review link"`, `"Copied link: %s"` (exists — reuse), `"[ctrl+l] copy link"`
- Test: `internal/tui/note_row_menu_test.go` (extend), `internal/tui/review_link_copy_test.go`

**Interfaces:**
- Consumes: `ReviewLink`, `ScopeLinkText`, `CommitFileLinkText` (Task 2); `copyToClipboardCmd(okMsg, text)` (records the link in `gg links` itself).
- Produces: `func (m Model) asyncCopyLinkRow(id, label string, build func(context.Context) (string, error)) actionRow`.

- [ ] **Step 1: Failing tests.** Update `TestNoteRowMenusAreOpenAndDelete` wants to:

```go
		{"review", isReviewRow, []string{"Open review", "Copy gg link", "Delete review"}},
		{"range review", isScopeRow, []string{"Open range review", "Copy gg link", "Delete range review"}},
		{"notes", isNotedRow, []string{"Open notes", "Copy gg link", "Delete notes"}},
```

New tests (a fake clipboard writer: the existing tests set `m.clipWrite = func(_ io.Writer, s string) (string, error) { got = s; return s, nil }` — grep `clipWrite =` in tests for the exact signature):

```go
func TestNoteRowCopyLinks(t *testing.T) {
	t.Parallel()
	m, id := noteRowsModel(t)
	var got string
	m.clipWrite = /* capture into got */
	for _, c := range []struct {
		row  func(contentLine) bool
		want string
	}{
		{isReviewRow, "?review=" + id},
		{isScopeRow, ".."},          // the pair link
		{isNotedRow, "/a.txt@" + m.filesHash},
	} {
		got = ""
		mm := selectRow(t, m, c.row)
		r, ok := menuRowByID(t, mm, "copy-gg-link")
		if !ok {
			t.Fatal("no Copy gg link")
		}
		u, cmd := r.run(mm)
		drainCmds(t, u.(Model), cmd)
		if !strings.HasPrefix(got, "gg://") || !strings.Contains(got, c.want) {
			t.Fatalf("copied %q, want …%s…", got, c.want)
		}
	}
}
```

plus: review view `.` menu has `copy-review-link` copying `?review=<id>` (use `openedReviewView`); Branches review sub-row (use `reviewBranchesModel`, `m.sel[panelBranches] = 2`) has `copy-gg-link`; All notes (`m.openAllNotes()`, cursor on the `anReview` row, `tea.KeyMsg{Type: tea.KeyCtrlL}`) copies the review link.

- [ ] **Step 2: Run** `go test ./internal/tui -run 'NoteRowMenus|NoteRowCopyLinks|ReviewViewCopy|BranchReviewCopy|AllNotesCopy' -count=1` → FAIL.

- [ ] **Step 3: Implement.**

```go
// asyncCopyLinkRow is a copy row whose link needs git (a review's revs, a
// pair's full shas): it is built when the row RUNS, off the Update thread,
// then copied — and recorded in gg links — like every copy.
func (m Model) asyncCopyLinkRow(id, label string, build func(context.Context) (string, error)) actionRow {
	return actionRow{id: id, label: label, run: func(m Model) (tea.Model, tea.Cmd) {
		cp := m.copyToClipboardCmd
		return m, func() tea.Msg {
			text, err := build(context.Background())
			if err != nil {
				return clipboardCopiedMsg{err: err}
			}
			return cp(i18n.T("Copied link: %s", text), text)()
		}
	}}
}
```

(`cp(...)` captures `m`'s svc/clipWrite at run time — `copyToClipboardCmd` is a value method; capturing the method value is fine. `i18n.T` inside the Cmd is allowed: it is a literal key.)

`noteRowMenu`: insert between Open and Delete —
- review: `m.asyncCopyLinkRow("copy-gg-link", i18n.T("Copy gg link"), func(ctx context.Context) (string, error) { return svc.ReviewLink(ctx, id) })`
- range review: `svc.ScopeLinkText(ctx, scope, hash)`
- notes: `svc.CommitFileLinkText(ctx, hash, path)`
(`svc := m.svc`, captured; a nil svc → no copy row.)

Review view: in the content-window branch of `availableActions`, right before `deleteReviewRow` is appended, when `m.filesReview != nil` (same gate `deleteReviewRow` uses), append `m.asyncCopyLinkRow("copy-review-link", i18n.T("Copy review link"), …ReviewLink(ctx, st.id))`. Branches: next to `showBranchReviewRow`, when `selectedBranchReview()` is ok, append `copy-gg-link` for `h.ID`. All notes: `case tea.KeyCtrlL:` on an `anReview` row → run the same async copy (call `m.asyncCopyLinkRow(...).run(m)`); add `i18n.T("[ctrl+l] copy link")` to the key-hint list only when the cursor is on a review row (mirror how `[ctrl+d] delete` is added at ~647).

- [ ] **Step 4: Run** Step 2 → PASS; i18n gates: `go test ./internal/i18n/ ./internal/tui/ -run 'I18n|EngineProse|DecisionOptionValues|ActionMenuLabels|CheckVerbs|FooterRendersTranslated' -count=1` → PASS; `go test ./internal/tui -count=1` → PASS.
- [ ] **Step 5: Commit** `feat(tui): copy gg link on note rows, the review view, Branches and All notes`.

---

### Task 7: gg web — copy and open

**Files:**
- Modify: `internal/web/reviews.go` (`GET /api/review/{id}/link`)
- Modify: `internal/web/notes.go` (`GET /api/notes/row-link?commit=&path=|scope=`)
- Modify: `internal/web/static/reviews.js` (menus: Copy gg link in `reviewMenu`, `scopeRowMenu`, `notedRowMenu`)
- Modify: `internal/web/static/live.js` (`steerNavigate`: a review hint opens the review)
- Modify: `internal/web/static/sidebar.js` (nothing new beyond `reviewMenu` gaining the item)
- Test: `internal/web/reviews_test.go` / `notes_test.go`; a playwright probe in the scratchpad (not committed)

**Interfaces:**
- Consumes: `ReviewLink`, `ScopeLinkText`, `CommitFileLinkText`; JS `copyLink(link, desc)` (links.js:282 — records via `/api/linkhist`), `openReview(id, back)`, `steerNavigateLand(s)`, `navMiss`/`opLine`.
- Produces: `GET /api/review/{id}/link` → `{"link": "<text>"}` (404 unknown); `GET /api/notes/row-link` → `{"link": "<text>"}` (400 on a non-hex commit or not exactly one of path/scope).

- [ ] **Step 1: Failing Go tests:**

```go
func TestReviewLinkEndpoint(t *testing.T) {
	t.Parallel()
	ts, svc, id := reviewServer(t) // existing review test fixture in reviews_test.go — reuse/extend
	var got struct{ Link string }
	if code := getJSONCode(t, ts, "/api/review/"+id+"/link", &got); code != 200 || !strings.Contains(got.Link, "?review="+id) {
		t.Fatalf("link = %d %q", code, got.Link)
	}
	if code := getJSONCode(t, ts, "/api/review/deadbeef/link", &got); code != 404 {
		t.Fatalf("unknown = %d", code)
	}
	_ = svc
}

func TestNoteRowLinkEndpoint(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 3)
	svc := domain.Open(dir)
	svc.UseNotesDir(t.TempDir())
	ts := serve(t, New(svc))
	head := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))
	base := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD~1"))
	var got struct{ Link string }
	q := func(v url.Values) int { got.Link = ""; return getJSONCode(t, ts, "/api/notes/row-link?"+v.Encode(), &got) }
	if code := q(url.Values{"commit": {head}, "path": {"f.txt"}}); code != 200 || !strings.HasSuffix(got.Link, "/f.txt@"+head) {
		t.Fatalf("path = %d %q", code, got.Link)
	}
	if code := q(url.Values{"commit": {head}, "scope": {base[:7] + ".." + head[:7]}}); code != 200 || !strings.Contains(got.Link, "@"+base+".."+head) {
		t.Fatalf("scope = %d %q", code, got.Link)
	}
	for _, v := range []url.Values{
		{"commit": {"HEAD~1"}, "path": {"f.txt"}},
		{"commit": {head}},
		{"commit": {head}, "path": {"f.txt"}, "scope": {"a..b"}},
	} {
		if code := q(v); code != 400 {
			t.Errorf("%v = %d, want 400", v, code)
		}
	}
}
```

(`getJSONCode(t, ts, path string, out any) int` — a GET that decodes into out and returns the status; reuse the package's helper if one with that shape exists (grep `func getJSON` in `internal/web/*_test.go`), else add it beside `getJSON`. `reviewServer(t) (*httptest.Server, *domain.Service, string)` — reuse the review fixture in `internal/web/reviews_test.go`, extracting one if the tests build it inline.)

- [ ] **Step 2: Run** `go test ./internal/web -run 'ReviewLinkEndpoint|NoteRowLinkEndpoint' -count=1` → FAIL.

- [ ] **Step 3: Implement** handlers (`knownReview` for the 404; `isHexSha` for the commit; `writeJSON(w, map[string]any{"link": text})`), then JS:

```js
// reviews.js
function copyServerLink(url, desc) {
  getJSON(url)
    .then((d) => copyLink(d.link, desc))
    .catch((e) => opLine("copy link: " + (e.message || e), true));
}
// reviewMenu: rows = open ? [Open review] : []; then
//   { label: "Copy gg link", act: () => copyServerLink("/api/review/" + encodeURIComponent(id) + "/link", "review") }
//   then Delete review.
// scopeRowMenu: between Open and Delete:
//   { label: "Copy gg link", act: () => copyServerLink("/api/notes/row-link?" + new URLSearchParams({ commit: state.fileSha, scope }), "range review") }
// notedRowMenu: likewise with { commit: state.fileSha, path }, desc "notes: " + path.
```

(import `copyLink` from `./links.js` in reviews.js; check `copyLink`'s signature at links.js:282 first.)

`live.js` `steerNavigate`, before `closeFinder()`:

```js
  if (s.hint_kind === "review") return steerNavigateReview(s);
```

```js
// steerNavigateReview: a review link opens the REVIEW (the TUI's
// steerNavigateReview); a deleted one says so and lands its address.
async function steerNavigateReview(s) {
  try {
    await getJSON("/api/review/" + encodeURIComponent(s.hint_id));
  } catch (e) {
    opLine(e.status === 404 ? "that review no longer exists; opened its change" : "review: " + (e.message || e), true);
    closeFinder();
    return steerNavigateLand({ ...s, hint_kind: "", hint_id: "" });
  }
  closeFinder();
  await openReview(s.hint_id, { kind: "list" });
}
```

(import `openReview` from `./reviews.js` in live.js if not already; `node --check` every edited JS file.)

- [ ] **Step 4: Run** Step 2 → PASS; `go test ./internal/web -count=1` → PASS; `node --check` on reviews.js, live.js.
- [ ] **Step 5: Browser check** (scratchpad, uncommitted): rebuild `gg` to the scratchpad, fixture repo with a review + range note + plain note (isolated `XDG_STATE_HOME`), playwright (`executablePath` = `~/.cache/ms-playwright/chromium_headless_shell-1243/chrome-headless-shell-linux64/chrome-headless-shell`): right-click each row → menu shows `Open … / Copy gg link / Delete …` (assert visible); click Copy gg link → `GET /api/linkhist` lists the link; `#` prompt with the review link → the review view opens (assert `#files-title` reads "Review…"); delete the review then paste again → the commit opens and the op line says it no longer exists. Read one screenshot.
- [ ] **Step 6: Commit** `feat(web): copy a review's gg link; a review link opens the review`.

---

### Task 8: Docs, skill, e2e

**Files:**
- Create: `e2e/scenarios/s108_review_links.toml`
- Modify: `internal/agentskill/using-gg.md` (link table row, `gg review show`, `gg link --review`, machine-local note, MCP tool), `internal/agentskill/reviewing-with-gg.md` (a "checking another agent's review" recipe), the skill version constant (`agentskill.Version` — grep it)
- Modify: `CHANGELOG.md`, `README.md` (review paragraph ~line 1300: Copy gg link + opening a review link), `docs/CLAUDE-details.md` (a "Review links (2026-10-05)" section: hint kind, domain functions, the one landing path, deviations)
- Modify: `docs/superpowers/specs/2026-10-05-review-links-design.md` (the four deviations above, one line each under a "Changed while planning" heading)

- [ ] **Step 1: e2e scenario** (writing-e2e-scenarios skill; model on `s80_cli_review.toml` — the tool must print a review DOCUMENT so remarks exist):

```toml
name = "a review link: gg link --review latest, gg review show latest, gg diff on the link"

[input]
steps = [
  { write = ".gg.toml", content = '''
[[tools.command]]
category = "review"
name = "Doc"
mode = "capture"
command = 'printf "%s" "{\"version\":1,\"summary\":\"looks fine\",\"files\":[{\"path\":\"a.txt\",\"annotations\":[{\"newRange\":[1,1],\"summary\":\"check v2\"}]}]}"'
''' },
  { write = "a.txt", content = "v1\n" },
  { commit = "initial" },
  { write = "a.txt", content = "v2\n" },
  { commit = "second" },
]

[[run]]
cmd = ["review", "--tool", "Doc", "HEAD"]
exit = 0

[[run]]
cmd = ["link", "--review", "latest"]
exit = 0
stdout_contains = ["?review="]

[[run]]
cmd = ["review", "show", "latest"]
exit = 0
stdout_contains = ["looks fine", "[0] a.txt:1 — check v2", "a.txt@"]

[expect]
branch = "main"
clean = true
```

(Verify the capture tool's quoting against how `s80` passes `<range>`; adjust the printf so the stored text is exactly the JSON. If the harness cannot express the JSON through printf, write it to a file in `steps` and `cat` it.)

- [ ] **Step 2: Run** `./test.sh e2e` → the new scenario passes (and nothing else regresses).
- [ ] **Step 3: Docs + skill** as listed; bump the skill version.
- [ ] **Step 4: Run** `./test.sh` → `all green`.
- [ ] **Step 5: Commit** `docs: review links — skill, README, CHANGELOG, details; e2e scenario`.

---

### Task 9: Gates and review

- [ ] **Step 1:** `./test.sh race > <scratchpad>/race.log 2>&1`; green only on `all green` in the log.
- [ ] **Step 2:** Build the verify binary `go build -o /work/gigagit/.claude/worktrees/review-links/bin/gg ./cmd/gg` (never staged).
- [ ] **Step 3:** One read-only review subagent (most capable model) over `git diff main...HEAD`: correctness of the hint check (mismatch vs deleted), every §4a entry point, async copy, i18n, the `show` subcommand collision. Fix confirmed findings test-first, rerun the race gate.
- [ ] **Step 4:** Report to the user with the verify binary's absolute path; ask before merging.

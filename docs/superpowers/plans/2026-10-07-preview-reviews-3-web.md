# Preview reviews — plan 3: web Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. (gigagit: executing-plans, inline — NO implementer subagents.)

**Goal:** gg web shows a merge preview's / commit pair's AI reviews where the preview lives, matching the TUI (plan 2): sub-rows in the sidebar's Previews list, "review (AI)…" and "show review" in the preview and pair right-click menus, a Reviews block at the top of the opened preview, and a review view header that names the preview and says "older tip".

**Architecture:** The server carries each row's classified review heads on the wire it already serves (`/api/preview` entries, the pair entries of `/api/saved-compares`, and the opened scope's `/api/preview/notes` and `/api/pair/notes`). It also learns a review target `preview` keyed by a saved row ID that it looks up itself, so no ref name comes off the wire. The page renders sub-rows from those lists, opens reviews through the existing `openReview`, and returns to the preview through a `back` kind of its own. A stored review emits `notes` on the live hub, so every open page re-reads.

**Tech Stack:** Go `net/http` handlers + embedded ES-module SPA (`internal/web/static/*.js`), Go tests with `httptest`, node-run pure-JS guards, a playwright visibility probe in the scratchpad.

**Spec:** `docs/superpowers/specs/2026-10-07-preview-reviews-design.md` (§7 web; R2, R3, R5, R6; §8; §9 web testing). Plan 1 (`20b2fc2b`) gave the domain; plan 2 (`03c4a173`) gave `PreviewReviewsByScope`, `ReviewOlder` and the TUI to mirror.

## Global Constraints

- NO implementer subagents; work in `.claude/worktrees/preview-reviews-web` on `feat/preview-reviews-web`; commit with `gg add <files>` + `git commit -F <msgfile>`; never `git add -A`.
- `internal/web` is domain-only (never imports `internal/git`). Wire values are allowlisted: a review of a preview is started by a saved row's ID that the server looks up in `PreviewList`/`PairList`. A branch name or range from the wire never reaches `ReviewTarget.Range`.
- Web strings are English (the web has no i18n layer).
- R2 classification comes from domain (`PreviewReviews`, `PreviewReviewsByScope`, `ReviewOlder`); the page never classifies.
- R6: a review row's right-click menu is `reviewMenu` (Open review / Copy gg link / Delete review) and nothing else.
- Hidden elements are hidden BY ID in this page (memory: web-hidden-class-is-per-id). The new rows reuse existing classes (`brev`, `rev`, `sect`), so no new hidden rule is needed.
- A browser check must assert VISIBILITY, run against the build under test (memory: browser-check-must-assert-visibility, playwright-web-verification: rebuild, restart, hard reload).
- Race gate: `./test.sh race` is green ONLY when its log says "all green". After the merge: `./build.sh install` and `./build.sh web` (memory: windows-web-exe-delivery).

## Rulings made while planning

- **P3-R1 (wire shape):** `reviewHeadWire` gains `Older bool \`json:"older,omitempty"\``. The preview/pair `reviews` arrays reuse it, giving spec §7's `{id, agent, created, remarks, resolved, older}` plus the fields the existing rows already carry (summary, commit). Cost if wrong: a few spare JSON fields.
- **P3-R2 (start route):** `reviewStartRequest` and `GET /api/review/tools` take `target=preview&preview=<saved row id>`. The server finds the ID in `PreviewList` (merge) or `PairList` (pair), refuses an unknown ID (400), and refuses a set that is not OK (400 "… not previewable"). Spec §7 says "looked up server-side (`NoteScopeResolve`)". The plan looks the ID up in the two lists directly, because `NoteScopeResolve` also accepts labels and literal `a...b` text, which would let a ref name through. Cost if wrong: none (stricter).
- **P3-R3 (refresh):** the review lane's run calls `s.emitNotes()` once a review is stored. live.js's existing `notes` branch then re-reads the counts AND the previews, so every page (the TUI-hosted one too) shows the new sub-row. The steer review hint (`live.js`) re-reads both before opening, the TUI's parity. Cost if wrong: one extra refresh per review.
- **P3-R4 (back to the preview):** `openReview`'s `back` gains `{kind: "preview", source, target}` (re-open through `window.__ggOpenPreviewForPair`, a previews.js hook, because reviews.js cannot import previews.js without closing a cycle) and `{kind: "pair", a, b}` (`runLinkCompare("a=…&b=…")`; reviews.js already imports linkcompare.js). After the reopen, `state.reviewSel = id`, so the review's row is selected.

## Review Focus

1. **Sub-rows break the list's row handlers.** A review sub-row has no `data-id`, so the click, contextmenu and drag handlers must route it to the review, never to a preview. Expect: click opens the review, right-click shows `reviewMenu`, and a drag never starts from it. Pinned by Task 3's `TestPreviewsJSReviewSubRowsWired` and the playwright probe.
2. **A wire ID that is not a saved row.** Expect: 400 and nothing runs, including for a label, a `main...feat/x` literal, or an ID with shell metacharacters. Pinned by Task 2's `TestReviewPreviewTargetRefusesNonIDs`.
3. **A stale row (merged / source deleted) still lists reviews but cannot start one.** Expect: `reviews` present with `older: true`; the start route answers 400; the menu row is absent (state ≠ ok). Pinned by Task 1's `TestPreviewEntriesCarryOlderReviewsWhenMerged` and Task 2's merged-preview case.
4. **The opened preview's block on a PR diff or a range opened from a commit's Range review row.** Expect: no Reviews block (a PR has no scope; a range shows one review's notes). Pinned by Task 5's `TestReviewRowsHTMLSkipsPRAndScopedPair` (wiring guard) and a manual check.
5. **Esc from a review opened in the block when the preview can no longer open.** Expect: an op-line notice and the commit list, never a stuck review view. Pinned by Task 5's goBack branch (`openPreviewForPair` reports the state and returns; goBack falls through to the list).

---

### Task 1: Server — `reviews` on preview entries, pair entries and the opened scope's notes

**Files:**
- Modify: `internal/web/reviews.go` (`reviewHeadWire.Older`, `reviewHeads` copies it)
- Modify: `internal/web/previews.go` (`previewRow.Reviews`, `handlePreviews`)
- Modify: `internal/web/savedcompares.go` (`savedCompareRow.Reviews`, `handleSavedCompares`)
- Modify: `internal/web/previewnotes.go`, `internal/web/pairnotes.go` (`reviews` on the response)
- Test: `internal/web/preview_reviews_test.go` (new)

**Interfaces:**
- Consumes: `svc.PreviewReviews(ctx, set)`, `svc.PreviewReviewsByScope(ctx, scope)`, `domain.ScopeReviewTarget`, `svc.SaveReview`.
- Produces: JSON `reviews: [reviewHeadWire…]` (always an array, never null) on every `/api/preview` entry, every pair entry of `/api/saved-compares`, and the `/api/preview/notes` + `/api/pair/notes` responses (empty for a set that is not OK, a PR, or a scoped pair read).

- [ ] **Step 1: Write the failing tests** (`internal/web/preview_reviews_test.go`)

```go
package web

import (
	"context"
	"net/http"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

const previewReviewWebDoc = `{"version":1,"summary":"## Summary\nok","files":[{"path":"f.txt","annotations":[{"newRange":[1,1],"summary":"one"}]}]}`

// previewReviewWebRepo: main + feat/x one commit ahead, a saved preview
// "login" and a saved pair main..feat/x, each with one stored review.
func previewReviewWebRepo(t *testing.T) (string, *domain.Service) {
	t.Helper()
	dir := newRepoDir(t, 2)
	gitRun(t, dir, "checkout", "-b", "feat/x")
	gitRun(t, dir, "commit", "--allow-empty", "-m", "feature")
	gitRun(t, dir, "checkout", "main")
	svc := domain.Open(dir)
	svc.UseNotesDir(t.TempDir())
	svc.UsePreviewsDir(t.TempDir())
	ctx := context.Background()
	if _, err := svc.PreviewAdd(ctx, "feat/x", "main", "login"); err != nil {
		t.Fatal(err)
	}
	set, err := svc.PreviewNotes(ctx, "feat/x", "main")
	if err != nil || !set.OK() {
		t.Fatalf("PreviewNotes %+v %v", set, err)
	}
	save := func(s domain.PreviewNoteSet, agent string) {
		if _, _, err := svc.SaveReview(ctx, domain.SaveReview{Target: domain.ScopeReviewTarget(s), Agent: agent, Text: previewReviewWebDoc}); err != nil {
			t.Fatal(err)
		}
	}
	save(set, "Claude Code")
	if _, err := svc.PairAdd(ctx, set.Base, set.Tip, "pair one"); err != nil {
		t.Fatal(err)
	}
	pair, err := svc.PairNotes(ctx, set.Base, set.Tip)
	if err != nil || !pair.OK() {
		t.Fatalf("PairNotes %+v %v", pair, err)
	}
	save(pair, "Codex")
	return dir, svc
}

type wireReview struct {
	ID    string `json:"id"`
	Agent string `json:"agent"`
	Older bool   `json:"older"`
}

func TestPreviewEntriesCarryTheirReviews(t *testing.T) {
	t.Parallel()
	_, svc := previewReviewWebRepo(t)
	ts := serve(t, New(svc))
	var pv struct {
		Entries []struct {
			ID      string       `json:"id"`
			Reviews []wireReview `json:"reviews"`
		} `json:"entries"`
	}
	if code := getJSON(t, ts, "/api/preview", &pv); code != http.StatusOK || len(pv.Entries) != 1 {
		t.Fatalf("GET /api/preview = %d %+v", code, pv)
	}
	if r := pv.Entries[0].Reviews; len(r) != 1 || r[0].Agent != "Claude Code" || r[0].Older {
		t.Fatalf("preview reviews = %+v", r)
	}
	var sc struct {
		Entries []struct {
			Kind    string       `json:"kind"`
			Reviews []wireReview `json:"reviews"`
		} `json:"entries"`
	}
	if code := getJSON(t, ts, "/api/saved-compares", &sc); code != http.StatusOK {
		t.Fatalf("GET /api/saved-compares = %d", code)
	}
	var pairRows int
	for _, e := range sc.Entries {
		if e.Kind != "pair" {
			continue
		}
		pairRows++
		if len(e.Reviews) != 1 || e.Reviews[0].Agent != "Codex" {
			t.Fatalf("pair reviews = %+v", e.Reviews)
		}
	}
	if pairRows != 1 {
		t.Fatalf("pair rows = %d", pairRows)
	}
}

func TestPreviewEntriesCarryOlderReviewsWhenMerged(t *testing.T) {
	t.Parallel()
	dir, svc := previewReviewWebRepo(t)
	gitRun(t, dir, "merge", "--ff-only", "feat/x")
	ts := serve(t, New(svc))
	var pv struct {
		Entries []struct {
			State   string       `json:"state"`
			Reviews []wireReview `json:"reviews"`
		} `json:"entries"`
	}
	getJSON(t, ts, "/api/preview", &pv)
	if len(pv.Entries) != 1 || pv.Entries[0].State != "merged" {
		t.Fatalf("entries %+v, want the merged preview", pv.Entries)
	}
	if r := pv.Entries[0].Reviews; len(r) != 1 || !r[0].Older {
		t.Fatalf("merged preview reviews = %+v, want one older", r)
	}
}

func TestOpenedScopeNotesCarryReviews(t *testing.T) {
	t.Parallel()
	_, svc := previewReviewWebRepo(t)
	ts := serve(t, New(svc))
	var pn struct {
		Reviews []wireReview `json:"reviews"`
	}
	if code := getJSON(t, ts, "/api/preview/notes?source=feat/x&target=main", &pn); code != http.StatusOK || len(pn.Reviews) != 1 {
		t.Fatalf("/api/preview/notes = %d %+v", code, pn)
	}
	set, _ := svc.PreviewNotes(context.Background(), "feat/x", "main")
	var pr struct {
		Reviews []wireReview `json:"reviews"`
	}
	if code := getJSON(t, ts, "/api/pair/notes?a="+set.Base+"&b="+set.Tip, &pr); code != http.StatusOK || len(pr.Reviews) != 1 || pr.Reviews[0].Agent != "Codex" {
		t.Fatalf("/api/pair/notes = %d %+v", code, pr)
	}
	// A range opened from a commit's Range review row shows ONE review's
	// notes: no Reviews block.
	if code := getJSON(t, ts, "/api/pair/notes?a="+set.Base+"&b="+set.Tip+"&scope=x", &pr); code != http.StatusOK || len(pr.Reviews) != 0 {
		t.Fatalf("scoped /api/pair/notes = %d %+v, want no reviews", code, pr)
	}
}
```

(Check the helper names `newRepoDir(t, n)`, `gitRun`, `serve`, `getJSON` in the package's `_test.go` files first — `review_test.go` uses all four.)

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/web -run 'TestPreviewEntriesCarry|TestOpenedScopeNotesCarryReviews' -count=1`
Expected: FAIL — `reviews` absent (zero-length decode), e.g. "preview reviews = []".

- [ ] **Step 3: Implement**

`reviews.go`: add to `reviewHeadWire`

```go
	// Older marks a preview review of a tip its preview has since moved
	// past (domain classifies it; spec R2). Absent on every other review.
	Older bool `json:"older,omitempty"`
```

and set `Older: h.Older` in `reviewHeads`.

`previews.go`: `previewRow` gains

```go
	// Reviews are the preview's AI reviews (spec R2: current, then older
	// tips whose commits still exist), newest first. Never null.
	Reviews []reviewHeadWire `json:"reviews"`
```

In `previewRowFrom` and `previewErrorRow` initialise `Reviews: []reviewHeadWire{}`. In `handlePreviews`: inside the `PreviewOK` block, after the counts, `if hs, herr := svc.PreviewReviews(ctx, set); herr == nil { row.Reviews = reviewHeads(hs) }`; add an `else` arm for `sum.State != domain.PreviewOK`: `if hs, herr := svc.PreviewReviewsByScope(ctx, p.Target+"..."+p.Source); herr == nil { row.Reviews = reviewHeads(hs) }` (spec §8).

`savedcompares.go`: `savedCompareRow` gains `Reviews []reviewHeadWire \`json:"reviews,omitempty"\`` (a comparison row has none, so omitempty keeps its shape). In `handleSavedCompares`, a pair row starts with `row.Reviews = []reviewHeadWire{}` and, inside the `PairOK` block where `set` is resolved, `if hs, herr := svc.PreviewReviews(ctx, set); herr == nil { row.Reviews = reviewHeads(hs) }`.

`previewnotes.go` / `pairnotes.go`: the `out` map gains `"reviews": []reviewHeadWire{}`. After the counts, `if set.OK() && set.Only == "" { if hs, herr := svc.PreviewReviews(ctx, set); herr == nil { out["reviews"] = reviewHeads(hs) } }`. A PR set has no scope, so `PreviewReviews` returns none.

- [ ] **Step 4: Run them to verify they pass, plus the existing preview / pair / notes handler tests**

Run: `go test ./internal/web -run 'Preview|Pair|SavedCompare|Notes|Review' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/web/reviews.go internal/web/previews.go internal/web/savedcompares.go internal/web/previewnotes.go internal/web/pairnotes.go internal/web/preview_reviews_test.go
git commit -F <msgfile>   # "feat(web): preview and pair entries carry their AI reviews"
```

---

### Task 2: Server — start a review of a saved preview/pair by ID; preview label + older; emit notes

**Files:**
- Modify: `internal/web/review.go` (`reviewStartRequest.Preview`, `reviewTarget(kind="preview")`, `handleReviewTools` query, the run's `emitNotes`)
- Modify: `internal/web/reviews.go` (`handleReview`: preview label + `older`)
- Test: `internal/web/preview_reviews_test.go`

**Interfaces:**
- Consumes: `svc.PreviewList`, `svc.PairList`, `svc.PreviewSummary`, `svc.PreviewNotes`, `svc.PairNotes`, `domain.ScopeReviewTarget`, `svc.ReviewOlder`, `domain.NoteScopeLabel`, `s.emitNotes()`.
- Produces:
  - `func (s *Server) previewReviewTarget(ctx context.Context, svc *domain.Service, id string) (domain.ReviewTarget, error)`.
  - `reviewTarget(ctx, svc, kind, branch, sha, preview string)` (new trailing param). `handleReviewTools` reads `preview` from the query; `handleReviewStart` reads `req.Preview`.
  - `GET /api/review/{id}` body gains `"older": bool`, and `label` = `NoteScopeLabel(Preview)` for a preview review.

- [ ] **Step 1: Write the failing tests** (append)

```go
func TestReviewPreviewTargetByID(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir, svc := previewReviewWebRepo(t)
	if err := os.WriteFile(filepath.Join(dir, ".gg.toml"), []byte(echoReviewTool), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, New(svc))
	ps, _ := svc.PreviewList(context.Background())
	pairs, _ := svc.PairList(context.Background())
	got := reviewTools(t, ts, "?target=preview&preview="+ps[0].ID)
	set, _ := svc.PreviewNotes(context.Background(), "feat/x", "main")
	if got.Range != set.Base+".."+set.Tip || got.Label != "main ... feat/x" {
		t.Fatalf("preview target = %+v", got)
	}
	if got := reviewTools(t, ts, "?target=preview&preview="+pairs[0].ID); got.Range != set.Base+".."+set.Tip {
		t.Fatalf("pair target = %+v", got)
	}
}

func TestReviewPreviewTargetRefusesNonIDs(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir, svc := previewReviewWebRepo(t)
	if err := os.WriteFile(filepath.Join(dir, ".gg.toml"), []byte(echoReviewTool), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, New(svc))
	for _, bad := range []string{"", "login", "main...feat/x", "x;rm%20-rf", "nope"} {
		var body map[string]any
		if code := getJSON(t, ts, "/api/review/tools?target=preview&preview="+bad, &body); code != http.StatusBadRequest {
			t.Errorf("preview=%q → %d, want 400", bad, code)
		}
	}
	gitRun(t, dir, "merge", "--ff-only", "feat/x") // the preview is no longer previewable
	ps, _ := svc.PreviewList(context.Background())
	if code, body := startReview(t, ts, `{"target":"preview","preview":"`+ps[0].ID+`","tool":"Echo","approve":true}`); code != http.StatusBadRequest {
		t.Fatalf("merged preview start = %d (%v), want 400", code, body)
	}
}

// A stored review (any kind) tells every open page: their sub-rows and ✎
// re-read on the live hub's "notes".
func TestReviewRunEmitsNotes(t *testing.T) {
	dir := reviewRepo(t, echoReviewTool)
	svc := reviewSvc(t, dir)
	srv := New(svc)
	ts := serve(t, srv)
	srv.liveMu.Lock()
	srv.live = newLiveHub(config.RefreshConfig{}, false, srv.opInFlight)
	srv.liveMu.Unlock()
	t.Cleanup(srv.stopLive)
	ch, cancel := srv.liveHubRef().subscribe()
	t.Cleanup(cancel)
	code, body := startReview(t, ts, `{"target":"branch","branch":"feature","tool":"Echo","approve":true}`)
	if code != http.StatusAccepted {
		t.Fatalf("start = %d (%v)", code, body)
	}
	opID, _ := body["op_id"].(string)
	readSSE(t, ts, opID, 30*time.Second)
	deadline := time.After(5 * time.Second)
	for {
		select {
		case msg := <-ch:
			for _, c := range msg.Changed {
				if c == "notes" {
					return
				}
			}
		case <-deadline:
			t.Fatal("a stored review emitted no notes event")
		}
	}
}

func TestReviewEndpointNamesThePreviewAndOlder(t *testing.T) {
	t.Parallel()
	dir, svc := previewReviewWebRepo(t)
	ts := serve(t, New(svc))
	set, _ := svc.PreviewNotes(context.Background(), "feat/x", "main")
	hs, _ := svc.PreviewReviews(context.Background(), set)
	var r struct {
		Label string `json:"label"`
		Older bool   `json:"older"`
	}
	if code := getJSON(t, ts, "/api/review/"+hs[0].ID, &r); code != http.StatusOK || r.Label != "feat/x → main" || r.Older {
		t.Fatalf("review = %d %+v", code, r)
	}
	gitRun(t, dir, "checkout", "feat/x")
	gitRun(t, dir, "commit", "--allow-empty", "-m", "more")
	gitRun(t, dir, "checkout", "main")
	if getJSON(t, ts, "/api/review/"+hs[0].ID, &r); !r.Older {
		t.Fatalf("review after the source moved = %+v, want older", r)
	}
}
```

(imports: `os`, `path/filepath`, `time`, `config`.)

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/web -run 'TestReviewPreviewTarget|TestReviewRunEmitsNotes|TestReviewEndpointNamesThePreview' -count=1`
Expected: FAIL. The tools route treats `target=preview` as a branch named HEAD (wrong range); no notes event arrives; the label is a short sha.

- [ ] **Step 3: Implement** (`review.go`)

```go
type reviewStartRequest struct {
	Target  string `json:"target"` // "branch" (default) | "working" | "commit" | "preview"
	Branch  string `json:"branch"`
	Sha     string `json:"sha"`     // target "commit": the commit to review, hex only
	Preview string `json:"preview"` // target "preview": a saved merge preview's or pair's ID
	Tool    string `json:"tool"`
	Approve bool   `json:"approve"`
}
```

`reviewTarget` gains a trailing `preview string`: `if kind == "preview" { return s.previewReviewTarget(ctx, svc, preview) }`. Update both callers (`handleReviewTools` passes `r.URL.Query().Get("preview")`, `handleReviewStart` passes `req.Preview`).

```go
// previewReviewTarget is the review of a SAVED merge preview or commit pair,
// named by its row ID. The ID is looked up in the two lists — never resolved
// as a label or an a...b literal — so no ref name from the wire reaches the
// target; the range is the scope's hex pair (ScopeReviewTarget). A preview
// that is not previewable (merged, a side missing, no base) is refused like
// the CLI refuses it.
func (s *Server) previewReviewTarget(ctx context.Context, svc *domain.Service, id string) (domain.ReviewTarget, error) {
	if id == "" {
		return domain.ReviewTarget{}, errors.New("no preview named")
	}
	var set domain.PreviewNoteSet
	ps, err := svc.PreviewList(ctx)
	if err != nil {
		return domain.ReviewTarget{}, err
	}
	for _, p := range ps {
		if p.ID == id {
			if set, err = svc.PreviewNotes(ctx, p.Source, p.Target); err != nil {
				return domain.ReviewTarget{}, err
			}
			if !set.OK() {
				return domain.ReviewTarget{}, fmt.Errorf("preview %s is not previewable (merged, a side missing, or no common base)", p.Label)
			}
			return domain.ScopeReviewTarget(set), nil
		}
	}
	pairs, err := svc.PairList(ctx)
	if err != nil {
		return domain.ReviewTarget{}, err
	}
	for _, p := range pairs {
		if p.ID == id {
			if set, err = svc.PairNotes(ctx, p.A, p.B); err != nil {
				return domain.ReviewTarget{}, err
			}
			if !set.OK() {
				return domain.ReviewTarget{}, fmt.Errorf("pair %s: a commit is missing", p.Label)
			}
			return domain.ScopeReviewTarget(set), nil
		}
	}
	return domain.ReviewTarget{}, fmt.Errorf("unknown preview %q", id)
}
```

(`PreviewNotes` on a merged pair returns the zero set; confirm while implementing and keep the `!set.OK()` refusal either way.)

The run closure in `handleReviewStart`, after a successful `ReviewReport` with `res.NoteID != ""`: `s.emitNotes()`. It runs inside the closure; `s` is captured. Comment: "a stored review is a note: every open page re-reads its counts and previews (P3-R3)".

`reviews.go` `handleReview`: after the label switch, `if rv.Preview != "" { label = domain.NoteScopeLabel(rv.Preview) }`. Add `"older": svc.ReviewOlder(ctx, rv)` to the body; use the handler's existing ctx/svc names.

- [ ] **Step 4: Run them to verify they pass, plus the whole review lane**

Run: `go test ./internal/web -run 'Review|Preview|Pair' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/web/review.go internal/web/reviews.go internal/web/preview_reviews_test.go
git commit -F <msgfile>   # "feat(web): review a saved preview or pair by id; preview label and older; a stored review emits notes"
```

---

### Task 3: Page — review sub-rows in the Previews list

**Files:**
- Modify: `internal/web/static/reviews.js` (pure section: `previewReviewText`; export it)
- Modify: `internal/web/static/previews.js` (render sub-rows; click / contextmenu route them)
- Test: `internal/web/reviewsjs_test.go` (pure text), `internal/web/previewsjs_test.go` (wiring)

**Interfaces:**
- Consumes: Task 1's `reviews` arrays; `branchReviewText(r, now)`, `openReview(id, back)`, `reviewMenu(id, x, y, open)` from reviews.js.
- Produces: `previewReviewText(r, now = new Date())` = `branchReviewText(r, now) + (r.older ? " · older tip" : "")`; `<li class="brev" data-review="<id>">` rows after their preview / pair row (no `data-id`, not draggable).

- [ ] **Step 1: Write the failing tests**

`reviewsjs_test.go`:

```go
func TestPreviewReviewText(t *testing.T) {
	t.Parallel()
	got := runReviewsPure(t, `
const now = new Date("2026-10-07T12:00:00Z");
console.log(previewReviewText({ created: "2026-10-07T10:02:00Z", agent: "Claude Code", remarks: 5, resolved: 3 }, now));
console.log(previewReviewText({ created: "2026-10-01T09:00:00Z", agent: "Codex", older: true }, now));
`)
	want := "└ Review: 10-07 10:02 Claude Code · 3/5 resolved\n└ Review: 10-01 09:00 Codex · older tip"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}
```

`previewsjs_test.go`:

```go
// Review sub-rows sit under their preview / pair row and route to the review,
// never to a preview handler (Review Focus 1).
func TestPreviewsJSReviewSubRowsWired(t *testing.T) {
	t.Parallel()
	src := staticSrc(t, "previews.js")
	for _, want := range []string{
		`previewReviewText`,
		`class="brev" data-review=`,
		`li.dataset.review`,
		`openReview(li.dataset.review, { kind: "list" })`,
		`reviewMenu(`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("previews.js lacks %q", want)
		}
	}
	// The review rows carry no data-id: every preview handler's guard skips them.
	if strings.Contains(src, `class="brev" data-id=`) {
		t.Error("a review sub-row carries a data-id")
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/web -run 'TestPreviewReviewText|TestPreviewsJSReviewSubRowsWired' -count=1`
Expected: FAIL — `previewReviewText is not defined` (node) and the missing strings.

- [ ] **Step 3: Implement**

reviews.js, inside the pure section after `branchReviews`:

```js
// previewReviewText is a merge preview's (or pair's) review sub-row: the
// branch sub-row's text, plus "· older tip" for a review of a tip the preview
// has since moved past — it still opens exactly what was reviewed (R2).
function previewReviewText(r, now = new Date()) {
  return branchReviewText(r, now) + (r && r.older ? " · older tip" : "");
}
```

Add `previewReviewText` to reviews.js's export list.

previews.js: `import { openReview, previewReviewText, reviewMenu } from "./reviews.js";`. reviews.js reaches previews.js only through ops.js, the cycle previews.js already has with ops.js. Each name is a hoisted function declaration called from event handlers only, so evaluation order cannot bite. The startReview hand-off in Task 4 goes through a window hook anyway, following the `__ggAddPreview` precedent for review.js. Add:

```js
// reviewSubRows paints a row's AI reviews under it (spec R3), the Branches
// sub-row's shape: no data-id and not draggable, so every preview handler and
// drag & drop pass them by; data-review routes click and right-click.
function reviewSubRows(e) {
  return (e.reviews || [])
    .map((r) => `<li class="brev" data-review="${esc(r.id)}" title="${esc(r.summary || "")}">${esc(previewReviewText(r))}</li>`)
    .join("");
}
```

In `renderPreviews`, append `+ reviewSubRows(e)` after each merge row's closing `</li>`. In `savedRowHTML`, append `(e.kind === "pair" ? reviewSubRows(e) : "")` after its `</li>`.

The click listener on `$("previews-list")`, first lines:

```js
  const li = ev.target.closest("li");
  if (li && li.dataset.review) return openReview(li.dataset.review, { kind: "list" });
  if (!li || !li.dataset.id) return;
```

The contextmenu listener, before its `data-id` guard:

```js
  if (li && li.dataset.review) {
    ev.preventDefault();
    const rid = li.dataset.review;
    return reviewMenu(rid, ev.clientX, ev.clientY, () => openReview(rid, { kind: "list" }));
  }
```

- [ ] **Step 4: Run them to verify they pass, plus the page guards**

Run: `go test ./internal/web -run 'JS|Static|Previews|Reviews' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/web/static/reviews.js internal/web/static/previews.js internal/web/reviewsjs_test.go internal/web/previewsjs_test.go
git commit -F <msgfile>   # "feat(web): Previews list shows a preview's reviews as sub-rows"
```

---

### Task 4: Page — "review (AI)…" and "show review" on the preview and pair menus

**Files:**
- Modify: `internal/web/static/review.js` (`startReview(target, branch, sha, preview)`; the query and POST carry `preview`)
- Modify: `internal/web/static/previews.js` (`showPreviewMenu`, `showPairMenu`)
- Test: `internal/web/previewsjs_test.go`

**Interfaces:**
- Consumes: Task 2's `target=preview&preview=<id>`.
- Produces: `startReview(target, branch, sha, preview)`; menu rows `review (AI)…` (state ok only) and `show review` (newest non-older review).

- [ ] **Step 1: Write the failing test**

```go
func TestPreviewsJSReviewMenuRows(t *testing.T) {
	t.Parallel()
	src := staticSrc(t, "previews.js")
	for _, want := range []string{
		`label: "review (AI)…"`,
		`window.__ggStartReview("preview", "", "", e.id)`,
		`label: "show review"`,
		`e.state === "ok"`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("previews.js lacks %q", want)
		}
	}
	rv := staticSrc(t, "review.js")
	for _, want := range []string{`async function startReview(target, branch, sha, preview)`, `"&preview="`, `preview, tool: tool.name`, `window.__ggStartReview = startReview`} {
		if !strings.Contains(rv, want) {
			t.Errorf("review.js lacks %q", want)
		}
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/web -run TestPreviewsJSReviewMenuRows -count=1`
Expected: FAIL — the missing strings.

- [ ] **Step 3: Implement**

review.js:
- `async function startReview(target, branch, sha, preview) {`
- the tools query: `+ (preview ? "&preview=" + encodeURIComponent(preview) : "")`
- `rev = { mode: "review", target, branch, sha, preview, label: info.label, … }`
- `reviewRun`: `const { target, branch, sha, preview, tool } = rev;` and the POST body `{ target, branch, sha, preview, tool: tool.name, approve: !!approve }`.

review.js imports ops.js, which imports previews.js, so a direct import would close the cycle `previews → review → ops → previews`. Use the module's window-hook hand-off (the `__ggAddPreview` precedent): at the bottom of review.js, `window.__ggStartReview = startReview; // previews.js starts a preview review through this (an import would close a cycle through ops.js)`.

```js
// reviewRows are a row's AI-review menu rows: "review (AI)…" while the
// preview is previewable (the server refuses the rest anyway), "show review"
// when it has a review of its current tip.
function reviewRows(e) {
  const rows = [];
  if (e.state === "ok") rows.push({ label: "review (AI)…", act: () => window.__ggStartReview("preview", "", "", e.id) });
  const cur = (e.reviews || []).find((r) => !r.older);
  if (cur) rows.push({ label: "show review", act: () => openReview(cur.id, { kind: "list" }) });
  return rows;
}
```

Add `...reviewRows(e)` to `showPreviewMenu`'s `items` after "save reversed …", and to `showPairMenu`'s list after "copy gg link".

- [ ] **Step 4: Run it, plus the page guards**

Run: `go test ./internal/web -run 'JS|Static|Previews|Review' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/web/static/review.js internal/web/static/previews.js internal/web/previewsjs_test.go
git commit -F <msgfile>   # "feat(web): review (AI) and show review on a preview or pair"
```

---

### Task 5: Page — the opened preview's Reviews block, esc back, header, steer refresh

**Files:**
- Modify: `internal/web/static/previews.js` (`loadPreviewCounts` keeps `d.reviews`; `window.__ggOpenPreviewForPair`)
- Modify: `internal/web/static/files.js` (`loadPairCounts` keeps `d.reviews`; the `data-review` click and contextmenu pick a preview back)
- Modify: `internal/web/static/reviews.js` (`reviewRowsHTML` preview arm; `goBack` preview/pair kinds; `setReviewHeader` older)
- Modify: `internal/web/static/live.js` (review hint re-reads counts + previews first)
- Modify: `internal/web/static/core.js` (`state.previewReviews: []`)
- Test: `internal/web/reviewsjs_test.go` / `previewsjs_test.go` (wiring guards)

**Interfaces:**
- Consumes: Task 1's `reviews` on `/api/preview/notes` and `/api/pair/notes`; Task 2's `older` on `/api/review/{id}`.
- Produces:
  - `state.previewReviews` — the opened scope's heads (set by the counts loads, cleared when a new compare opens).
  - `previewBack(id)` in files.js: `{kind: "preview", source, target, reviewId}` for an open merge preview (not a PR); `{kind: "pair", a, b, reviewId}` for an unscoped pair; null otherwise.
  - `goBack` handles both kinds.

- [ ] **Step 1: Write the failing guards**

```go
func TestPreviewReviewsBlockWired(t *testing.T) {
	t.Parallel()
	rv := staticSrc(t, "reviews.js")
	for _, want := range []string{
		`state.previewReviews`,
		`back.kind === "preview"`,
		`window.__ggOpenPreviewForPair`,
		`back.kind === "pair"`,
		`runLinkCompare("a=" + encodeURIComponent(back.a) + "&b=" + encodeURIComponent(back.b))`,
		`d.older ? " · older tip" : ""`,
	} {
		if !strings.Contains(rv, want) {
			t.Errorf("reviews.js lacks %q", want)
		}
	}
	pv := staticSrc(t, "previews.js")
	for _, want := range []string{`window.__ggOpenPreviewForPair = openPreviewForPair`, `state.previewReviews = d.reviews || []`} {
		if !strings.Contains(pv, want) {
			t.Errorf("previews.js lacks %q", want)
		}
	}
	fs := staticSrc(t, "files.js")
	for _, want := range []string{`state.previewReviews = d.reviews || []`, `previewBack(`} {
		if !strings.Contains(fs, want) {
			t.Errorf("files.js lacks %q", want)
		}
	}
	lv := staticSrc(t, "live.js")
	if !strings.Contains(lv, `await Promise.all([refreshNoteCounts(), fetchPreviews()])`) {
		t.Error("live.js: a review hint does not re-read the counts and previews first")
	}
}

// Review Focus 4: a PR diff and a scoped pair (one review's range) get no block.
func TestReviewRowsHTMLSkipsPRAndScopedPair(t *testing.T) {
	t.Parallel()
	rv := staticSrc(t, "reviews.js")
	if !strings.Contains(rv, `function previewScopeReviews()`) || !strings.Contains(rv, `po.pr`) || !strings.Contains(rv, `p.scope`) {
		t.Error("reviews.js: previewScopeReviews must refuse a PR and a scoped pair")
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/web -run 'TestPreviewReviewsBlockWired|TestReviewRowsHTMLSkipsPRAndScopedPair' -count=1`
Expected: FAIL — the missing strings.

- [ ] **Step 3: Implement**

core.js state: `previewReviews: [], // the opened preview's / pair's AI reviews (its Reviews block)`.

previews.js:
- `loadPreviewCounts`: after `state.previewCounts = d.counts || {};` add `state.previewReviews = d.reviews || [];`.
- `armPreview`: in the `!samePair` branch, also `state.previewReviews = [];`.
- At the bottom near the other hooks: `window.__ggOpenPreviewForPair = openPreviewForPair;` with a comment ("reviews.js returns to a preview through this; importing previews.js there would close a cycle").

files.js:
- `loadPairCounts`: after `state.previewCounts = …` add `state.previewReviews = d.reviews || [];`.
- `openLinkCompare`: next to `state.previewCounts = null;` add `state.previewReviews = [];`.
- New helper (beside `pairCtx`):

```js
// previewBack is where esc from a review opened in the preview's Reviews
// block returns: the merge preview (re-opened by its names, the record path
// when saved) or the pair (re-run as a..b). null for any other screen.
function previewBack(reviewId) {
  const po = openPreviewCtx();
  if (po && !po.pr) return { kind: "preview", source: po.source, target: po.target, reviewId };
  const p = pairCtx();
  if (p && !p.scope) return { kind: "pair", a: p.a, b: p.b, reviewId };
  return null;
}
```

- The `data-review` click branch: `openReview(li.dataset.review, state.filesMode === "status" ? { kind: "list" } : previewBack(li.dataset.review) || reviewBackFromCommit(li.dataset.review));`. The contextmenu `reviewMenu(...)` open callback uses the same expression.

reviews.js:
- New (outside the pure section):

```js
// previewScopeReviews is the opened merge preview's (or unscoped pair's) AI
// reviews — its Reviews block. None for a pull request (no scope) or a range
// opened from a commit's Range review row (one review's notes: p.scope).
function previewScopeReviews() {
  if (state.filesMode !== "compare" || state.layout === "list") return [];
  const po = state.previewOpen;
  if (po && po.tip && state.compare && state.compare.bHash === po.tip) return po.pr ? [] : state.previewReviews || [];
  const p = state.compare && state.compare.pair;
  if (p && !p.scope) return state.previewReviews || [];
  return [];
}
```

- `reviewRowsHTML`: prefix the existing output with the preview block

```js
  const pv = previewScopeReviews();
  const pvHTML = pv.length
    ? `<li class="sect">Reviews</li>` +
      pv.map((r) => `<li class="rev${state.reviewSel === r.id ? " sel" : ""}" data-review="${esc(r.id)}" title="${esc(r.summary || "")}">${esc(previewReviewText(r))}</li>`).join("")
    : "";
```

and `return pvHTML + (…existing…)`. `commitReviewList()` is already empty in compare mode, so the two never double up.

- `goBack`: before the final fall-through

```js
  if (back && back.kind === "preview" && window.__ggOpenPreviewForPair) {
    window.__ggOpenPreviewForPair(back.source, back.target).then(() => {
      state.reviewSel = back.reviewId || "";
      renderFiles();
    });
    return;
  }
  if (back && back.kind === "pair") {
    runLinkCompare("a=" + encodeURIComponent(back.a) + "&b=" + encodeURIComponent(back.b)).then((ok) => {
      if (!ok) return;
      state.reviewSel = back.reviewId || "";
      renderFiles();
    });
    return;
  }
```

(A preview that no longer opens: `openPreviewBody` posts the state notice and leaves the screen as it is. Verify by hand; if the review view stays on screen, add `.then(() => { if (state.review) …fall through… })` — ledger the decision.)

- `setReviewHeader`: `$("files-title").textContent = "Review " + d.label + (d.older ? " · older tip" : "");`.
- Update the `openReview` doc comment to list the two new `back` kinds.

live.js, the review-hint landing (`await openReview(s.hint_id, { kind: "list" });`, ~446):

```js
  // An agent that just saved this review (gg review save) wrote the store
  // behind this page: re-read the counts and the previews first, so its
  // sub-row is there on arrival (the TUI's review-hint refresh).
  await Promise.all([refreshNoteCounts(), fetchPreviews()]);
  await openReview(s.hint_id, { kind: "list" });
```

(Import `refreshNoteCounts` from files.js if live.js lacks it.)

- [ ] **Step 4: Run them, plus every page guard**

Run: `go test ./internal/web -count=1 > <scratchpad>/web-t5.log 2>&1; tail -3 <scratchpad>/web-t5.log`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
gg add internal/web/static/previews.js internal/web/static/files.js internal/web/static/reviews.js internal/web/static/live.js internal/web/static/core.js internal/web/reviewsjs_test.go internal/web/previewsjs_test.go
git commit -F <msgfile>   # "feat(web): the opened preview lists its reviews; back to the preview; older tip in the header"
```

---

### Task 6: Help, playwright visibility probe, docs, race gate

**Files:**
- Modify: `internal/web/static/previews.js` (its `registerHelp({...})` block, ~925: review rows)
- Modify: `CHANGELOG.md`, `README.md`, `docs/CLAUDE-details.md`
- Scratch (never committed): `<scratchpad>/pw-preview-reviews/probe.mjs`

- [ ] **Step 1: Help text.** Add to previews.js's `registerHelp` entries, in the existing shape: "right-click a preview or pair → review (AI)… — an agent reviews the whole preview as one document, stored with the preview; show review opens the newest one", and "a preview's reviews list under it (└ Review: date agent tally · older tip when the preview has moved on); click opens the review, right-click → open / copy gg link / delete; the opened preview lists them under Reviews, and back from a review returns to the preview". Run `go test ./internal/web -run 'Help|JS' -count=1` → PASS.

- [ ] **Step 2: Playwright probe (assert VISIBILITY).** Build `go build -o bin/gg ./cmd/gg` in the worktree. Set up a scratch repo (main + feat/x, `bin/gg preview add --label login feat/x main`, then `bin/gg review save 'gg://<abs>@main...feat/x' --agent "Claude Code" --stdin` fed with a review document via `printf '%s'`). Start `bin/gg web --addr 127.0.0.1:<port>` in that repo, with `XDG_STATE_HOME` pointing at a scratch dir. The probe:
  - waits for `#previews-list li[data-id]` to be visible, then asserts `#previews-list li.brev[data-review]` is visible and its text contains `└ Review:` and `Claude Code`;
  - right-clicks the preview row and asserts a visible menu item with text `review (AI)…` and one with `show review`; Escape closes it;
  - clicks the preview row, waits for `#files-list li.sect` with text `Reviews` (visible) and a visible `#files-list li.rev[data-review]`;
  - clicks that row and asserts `#files-title` text starts with `Review feat/x → main`;
  - triggers the page's esc-back (the same key the files pane uses; check keys.js) and asserts `#files-list li.rev.sel[data-review]` is visible again (back in the preview, its row selected);
  - screenshots each step and you READ the PNGs.
  Prove it bites: run the probe once against a build with `reviewSubRows` returning `""` (a reversible one-line edit, rebuilt), then restore. The sub-row assertion must fail.

- [ ] **Step 3: Docs.**
  - CHANGELOG, in the "Preview reviews" entry's Added list: "**Web: preview reviews where the preview lives.** The sidebar's Previews list shows a merge preview's or commit pair's AI reviews as sub-rows (`· older tip` for a review of a tip the preview has moved past; hidden once its commits are gone); click opens it, right-click is Open review / Copy gg link / Delete review. A preview's or pair's right-click menu gains **review (AI)…** (started by the saved row's id; the server looks it up — no ref name from the page) and **show review**. The opened preview lists its reviews under **Reviews**; back from one returns to the preview. The review view's title names the preview and says `older tip`. A stored review emits `notes`, so every open page shows it."
  - README: in the gg web section, one line for the Previews sub-rows and the two menu rows.
  - CLAUDE-details `## Preview reviews (2026-10-07)`: a "Web (plan 3)" paragraph naming `reviewHeadWire.Older`, the `reviews` fields (preview entries, pair entries, the two notes endpoints), `previewReviewTarget` (ID lookup in PreviewList/PairList, not NoteScopeResolve: P3-R2), `emitNotes` on a stored review, `previewScopeReviews` / `previewBack` / the `preview` and `pair` back kinds through `window.__ggOpenPreviewForPair` (P3-R4), and the live.js hint refresh.

- [ ] **Step 4: Full gate**

Run: `./test.sh race > <scratchpad>/race-p3.log 2>&1; tail -3 <scratchpad>/race-p3.log`
Expected: the log ends with "all green".

- [ ] **Step 5: Commit**

```bash
gg add internal/web/static/previews.js CHANGELOG.md README.md docs/CLAUDE-details.md
git commit -F <msgfile>   # "docs(web): preview reviews in gg web"
```

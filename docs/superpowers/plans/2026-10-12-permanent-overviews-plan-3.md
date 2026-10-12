# Permanent Overviews — Plan 3 (Web) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (this repo forbids implementer subagents — CLAUDE.md). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show permanent overviews in `gg web` and the page a TUI hosts, as the TUI does since plan 2: a `≡` on commit rows, "open overview: <title>" in the commit menu, a `└ Overview` sub-row under a branch at that tip, View all notes rows, a PR view's Overviews block, and `?overview=` links. Each opens in the existing overview split, with the file at the overview's commit and its remarks.

**Architecture:** The server adds a small read/delete surface over plan 1/2's domain calls (`Overview`, `OverviewDocAt`, `OverviewFileNotes`, `OverviewFileDiff`, `OverviewLink`, `RemoveOverview`, `PreviewNoteSet.Overviews`). It also adds an `overview=<id>` twin to each `review=<id>` lane: file content, diff, export. The page already draws a review's stored overview in the viewer's "stored" mode (`openStoredOverview`). Its identity becomes kind-aware: `stored.id` is `review-overview:<id>` or `overview:<id>`, and one pure helper (`storedRef`) turns that into the `review=` / `overview=` query every lane sends. The row surfaces read the new `overviews` arrays from the counts, the all-notes payload and the PR notes.

**Tech Stack:** Go 1.26 (`internal/web`), vanilla ES modules (`internal/web/static`), node-run pure-JS tests (`runPureJS`), string-wiring tests (`readStatic`), Playwright in a scratch dir for the visible check.

**Spec:** `docs/superpowers/specs/2026-10-12-permanent-overviews-design.md` (§10; R5, R9, R10; §9 for the TUI behaviour the page mirrors). Plans 1–2 are merged (`17ddc122`, `3cff040b`).

## Global Constraints

- `web` is a domain-only frontend: never import `internal/git` (archtest).
- Every new route sits behind the existing loopback + Host/Origin guards. Mutations go through `writeGuard` (JSON Content-Type, loopback Origin). Wire values are validated before use: an overview id matches `reviewIDRE` (`^[A-Za-z0-9_.-]{1,80}$`, overview_export_http.go) and never reaches a git argv.
- R9: an overview's remarks show only beside its own split — the `overview=` lane is the only way they reach the page.
- R5: a commit overview's `≡` and menu row show from any branch. The branch sub-row shows only while the branch's tip IS the overview's commit and the overview's recorded branch is that branch.
- R10: a PR overview appears only in that PR's view (its Overviews block), never in counts rows, View all notes or commit rows.
- Row menus on an overview row: **Open overview / Delete overview** (the TUI's ruling). View all notes keeps its keys: enter / ctrl+d / ctrl+l.
- `y` in an overview copies its text (plan 2 ruling, spec §9 amended).
- A deleted overview that is open in the page closes when the counts refresh, with `the overview was deleted` (the TUI's behaviour).
- The remark box of a permanent overview says `overview · <agent> · line N` (the page's `noteBoxTitle` reads the row's kind). The TUI's box still says "review" — a deferred plan-2 minor, out of scope here.
- New tests call `t.Parallel()` unless they use a serial fixture (`sendServerFull` is serial).
- Help (`index.html` help rows) names every new click and menu row; the foot hints stay as they are (no new keys).

## Review Focus

1. **The overview is deleted elsewhere while its split is open in the page** (the CLI, the TUI, another tab). Expected: on the next counts refresh the viewer closes and `the overview was deleted` shows. A link to it then says it no longer exists and opens its commit. Pinned in Task 3's pure test (`goneStoredOverview`) and Task 8's Playwright `gone` scenario.
2. **Two overviews on one commit.** Expected: two menu rows and two branch sub-rows, each opening its own; one `≡`. Pinned in Task 4's pure test (`commitOverviews`/`branchOverviews`) and Task 8's Playwright `commits`/`branch` scenarios.
3. **A stale `review-overview:` id keeps working.** A review's stored overview must still send `review=` everywhere after the refactor: remarks, D diff, export, S stack. Pinned in Task 3 (`storedRef` pure test) and the Task 8 Playwright run of the review-overview scenario.
4. **An overview id from the URL or a link that is not one** (`../x`, empty, 200 chars). Expected: 400 from every route, no git call. Pinned in Task 1 (`TestOverviewRoutesRefuseBadIDs`).
5. **A PR overview opened from the PR view.** Expected: D diffs merge-base..head (`OverviewFileDiff`'s PR scope), the file opens at the overview's commit, and the box's remarks show. Pinned in Task 2 (`TestPRNotesListsThePRsOverviews`) and plan 2's domain test of `OverviewFileDiff` on a PR; Task 6 adds a Playwright PR check only if the fixture can serve a PR.

---

### Task 1: Server — one overview's reads (document, link, file remarks, diff, export)

**Files:**
- Create: `internal/web/overviews_http.go`, `internal/web/overviews_http_test.go`
- Modify: `internal/web/filecontent.go` (the `overview=` remarks), `internal/web/diff.go:166` (the `overview=` lane), `internal/web/reviewdiff.go` (`remarkRows` kind), `internal/web/filecontent.go` `noteRow` (`Kind`), `internal/web/overview_export_http.go` (`overview=`)

**Interfaces:**
- Consumes (domain): `Overview(ctx,id) (Overview, error)`, `OverviewDocAt(ctx,id) (OverviewDoc, error)`, `OverviewFileNotes(ctx,id,path,lines)`, `OverviewFileDiff(ctx,id,path) (ReviewFileDiff, error)`, `OverviewLink(ctx,id) (string, error)`, `ErrOverviewNotFound`, `ErrNoOverview`, `OverviewSource{OverviewID}`, `PRScopeNumber(scope) (int, bool)`.
- Produces:
  - `GET /api/overview/{id}` → `{id, title, commit, branch, pr, agent, created, blocks, anchors, text}`. `anchors` is `[]overviewAnchor` with `Plain = !OK`; `pr` is 0 for a commit overview. 404 when gone.
  - `GET /api/overview/{id}/link` → `{link}`.
  - `/api/file-content?…&overview=<id>` → `remarks` (rows with `kind:"overview"`).
  - `/api/diff?overview=<id>&path=<p>` → the review-diff payload shape (`changed, status, old_path, left_label, right_label, remarks`, the rows).
  - `/api/overview-export?overview=<id>`.
  - `noteRow.Kind string \`json:"kind,omitempty"\`` — `"review"` | `"overview"` on read-only rows.
  - `func overviewRemarkRows(ns []domain.ResolvedNote) []noteRow`.

- [ ] **Step 1: Write the failing tests** — `internal/web/overviews_http_test.go`:

```go
package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// overviewServer serves newRepoDir's two-commit repo (f.txt "content 2" at the
// tip) holding one permanent overview on HEAD: a link to f.txt:1 with a remark.
func overviewServer(t *testing.T) (ts *httptest.Server, svc *domain.Service, id string) {
	t.Helper()
	svc = domain.Open(newRepoDir(t, 2))
	svc.UseNotesDir(t.TempDir())
	ctx := context.Background()
	tg, err := svc.HeadOverviewTarget(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := svc.SaveOverview(ctx, domain.SaveOverview{Target: tg, Agent: "Claude Code", Text: []byte(
		`{"version":1,"title":"How f works","overview":"Start at [f](f.txt:1), then [gone](zzz.go:1).","files":[{"path":"f.txt","annotations":[{"newRange":[1,1],"summary":"the content"}]}]}`)})
	if err == nil {
		t.Fatal("zzz.go is not in the commit: the save must be refused")
	}
	got, _, err = svc.SaveOverview(ctx, domain.SaveOverview{Target: tg, Agent: "Claude Code", Text: []byte(
		`{"version":1,"title":"How f works","overview":"Start at [f](f.txt:1).","files":[{"path":"f.txt","annotations":[{"newRange":[1,1],"summary":"the content"}]}]}`)})
	if err != nil {
		t.Fatal(err)
	}
	return serve(t, New(svc)), svc, got.ID
}

func TestOverviewDocument(t *testing.T) {
	t.Parallel()
	ts, svc, id := overviewServer(t)
	var got struct {
		ID, Title, Commit, Agent, Text string
		PR                            int
		Blocks                        []any
		Anchors                       []overviewAnchor
	}
	if code := getJSON(t, ts, "/api/overview/"+id, &got); code != http.StatusOK {
		t.Fatalf("GET = %d", code)
	}
	head, _, _ := svc.ResolveRev(context.Background(), "HEAD")
	if got.ID != id || got.Title != "How f works" || got.Commit != strings.TrimSpace(head) || got.PR != 0 || len(got.Blocks) == 0 {
		t.Fatalf("doc = %+v", got)
	}
	if len(got.Anchors) != 1 || got.Anchors[0].Path != "f.txt" || got.Anchors[0].Plain {
		t.Fatalf("anchors = %+v", got.Anchors)
	}
	var link struct{ Link string }
	if code := getJSON(t, ts, "/api/overview/"+id+"/link", &link); code != 200 || !strings.Contains(link.Link, "?overview="+id) {
		t.Fatalf("link = %d %+v", code, link)
	}
}

func TestOverviewRemarksRideTheLanes(t *testing.T) {
	t.Parallel()
	ts, svc, id := overviewServer(t)
	head, _, _ := svc.ResolveRev(context.Background(), "HEAD")
	var fc struct {
		Remarks []noteRow `json:"remarks"`
	}
	if code := getJSON(t, ts, "/api/file-content?src=commit&rev="+strings.TrimSpace(head)+"&path=f.txt&overview="+id, &fc); code != 200 ||
		len(fc.Remarks) != 1 || fc.Remarks[0].Summary != "the content" || !fc.Remarks[0].ReadOnly || fc.Remarks[0].Kind != "overview" {
		t.Fatalf("file-content = %d %+v", code, fc.Remarks)
	}
	// R9: without the lane the same file carries none.
	if getJSON(t, ts, "/api/file-content?src=commit&rev="+strings.TrimSpace(head)+"&path=f.txt", &fc); len(fc.Remarks) != 0 {
		t.Fatalf("remarks leaked: %+v", fc.Remarks)
	}
	var d struct {
		Changed bool      `json:"changed"`
		Remarks []noteRow `json:"remarks"`
	}
	if code := getJSON(t, ts, "/api/diff?overview="+id+"&path=f.txt", &d); code != 200 || !d.Changed || len(d.Remarks) != 1 || d.Remarks[0].Kind != "overview" {
		t.Fatalf("diff = %d %+v", code, d)
	}
	resp, err := http.Get(ts.URL + "/api/overview-export?overview=" + id)
	if err != nil || resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Disposition"), "overview-how-f-works.html") {
		t.Fatalf("export = %v %+v", err, resp)
	}
	resp.Body.Close()
}

func TestOverviewRoutesRefuseBadIDs(t *testing.T) {
	t.Parallel()
	ts, _, _ := overviewServer(t)
	for _, p := range []string{
		"/api/overview/..%2Fx", "/api/overview/" + strings.Repeat("a", 200),
		"/api/diff?overview=..%2Fx&path=f.txt", "/api/overview-export?overview=..%2Fx",
		"/api/file-content?src=worktree&path=f.txt&overview=..%2Fx",
	} {
		if code := getJSON(t, ts, p, nil); code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", p, code)
		}
	}
	for _, p := range []string{"/api/overview/nope", "/api/overview/nope/link", "/api/diff?overview=nope&path=f.txt", "/api/overview-export?overview=nope"} {
		if code := getJSON(t, ts, p, nil); code != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", p, code)
		}
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/web/ -run 'OverviewDocument|OverviewRemarksRideTheLanes|OverviewRoutesRefuseBadIDs' -count=1`
Expected: FAIL — `noteRow` has no `Kind` (a compile error), then 404s on the new routes.

- [ ] **Step 3: Implement**

`filecontent.go` — `noteRow` gains, after `Side`:

```go
	// Kind names a read-only row's owner for the box title: "review" or
	// "overview" (a permanent overview's remark); "" on an agent's note.
	Kind string `json:"kind,omitempty"`
```

`reviewdiff.go` — `remarkRows` sets `Kind: "review"`. Add beside it:

```go
// overviewRemarkRows are a permanent overview's remarks on the wire: the
// review rows, owned by the overview (the page titles the box so).
func overviewRemarkRows(ns []domain.ResolvedNote) []noteRow {
	out := remarkRows(ns)
	for i := range out {
		out[i].Kind = "overview"
	}
	return out
}
```

`filecontent.go` `handleFileContent`: read `overview := q.Get("overview")`. Refuse `review != "" && overview != ""`, and `overview != "" && !reviewIDRE.MatchString(overview)`, with 400. After the `review` remarks:

```go
	if overview != "" {
		if ns, err := svc.OverviewFileNotes(ctx, overview, path, agentdocs.Lines(data)); err == nil && len(ns) > 0 {
			body.Remarks = overviewRemarkRows(ns)
		}
	}
```

`diff.go` — before the `review` lane:

```go
	if id := q.Get("overview"); id != "" { // a permanent overview's commit change to one file (its split's D)
		s.handleOverviewDiff(w, r, id)
		return
	}
```

`internal/web/overviews_http.go`:

```go
package web

import (
	"errors"
	"net/http"

	"github.com/homeend/gigagit/internal/domain"
)

// Permanent overviews in the page (spec 2026-10-12-permanent-overviews §10):
// one overview's document, link and delete; its remarks and diff ride the
// file-content and diff lanes as overview=<id>. An id only ever matches a
// stored note's id — it never reaches a git argv.
func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/overview/{id}", s.handleOverviewDoc)
		mux.HandleFunc("GET /api/overview/{id}/link", s.handleOverviewLink)
	})
}

// overviewID is the request's id, answered 400 when it is not one.
func overviewID(w http.ResponseWriter, id string) (string, bool) {
	if !reviewIDRE.MatchString(id) {
		writeErr(w, http.StatusBadRequest, errors.New("want an overview id"))
		return "", false
	}
	return id, true
}

// overviewErr answers a domain error: 404 for a gone overview.
func overviewErr(w http.ResponseWriter, err error) {
	if errors.Is(err, domain.ErrOverviewNotFound) || errors.Is(err, domain.ErrNoOverview) {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeErr(w, http.StatusInternalServerError, err)
}

func (s *Server) handleOverviewDoc(w http.ResponseWriter, r *http.Request) {
	id, ok := overviewID(w, r.PathValue("id"))
	if !ok {
		return
	}
	svc, ctx := s.service(), readCtx(r)
	o, err := svc.Overview(ctx, id)
	if err != nil {
		overviewErr(w, err)
		return
	}
	od, err := svc.OverviewDocAt(ctx, id)
	if err != nil {
		overviewErr(w, err)
		return
	}
	anchors := make([]overviewAnchor, 0, len(od.Anchors))
	for _, a := range od.Anchors {
		anchors = append(anchors, overviewAnchor{Dest: a.Dest, Path: a.Path, Start: a.Start, End: a.End, Note: a.Note,
			Label: a.Label, Missing: !a.OK, Plain: !a.OK})
	}
	pr, _ := domain.PRScopeNumber(o.Scope)
	writeJSON(w, map[string]any{
		"id": o.ID, "title": o.Title, "commit": o.Commit, "branch": o.Branch, "pr": pr,
		"agent": o.Agent, "created": wireTime(o.Created), "blocks": od.Doc.Blocks, "anchors": anchors, "text": od.Text,
	})
}

func (s *Server) handleOverviewLink(w http.ResponseWriter, r *http.Request) {
	id, ok := overviewID(w, r.PathValue("id"))
	if !ok {
		return
	}
	link, err := s.service().OverviewLink(readCtx(r), id)
	if err != nil {
		overviewErr(w, err)
		return
	}
	writeJSON(w, map[string]string{"link": link})
}

// handleOverviewDiff serves /api/diff?overview=<id>&path=<p>: the file's
// change in the overview's commit (a PR overview's: merge base..head) with
// its remarks — the review diff lane's shape.
func (s *Server) handleOverviewDiff(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := overviewID(w, id); !ok {
		return
	}
	path := r.URL.Query().Get("path")
	if !isGitArgSafe(path) {
		writeErr(w, http.StatusBadRequest, errors.New("a valid path is required"))
		return
	}
	rd, err := s.service().OverviewFileDiff(r.Context(), id, path)
	if err != nil {
		overviewErr(w, err)
		return
	}
	p := diffPayload(rd.Diff, nil)
	p["changed"], p["status"], p["old_path"] = rd.Changed, rd.Status, rd.OldPath
	p["left_label"], p["right_label"] = rd.Left, rd.Right
	p["remarks"] = overviewRemarkRows(rd.Notes)
	writeJSON(w, p)
}
```

If `OverviewLink` answers a gone overview with an error that is not `ErrOverviewNotFound`, map it the same way (check `overview_link.go`).

`overview_export_http.go`: read `ov := q.Get("overview")`, and add a case before `default`:

```go
	case ov != "" && id == "" && review == "" && reviewIDRE.MatchString(ov):
		src = domain.OverviewSource{OverviewID: ov}
```

Add `errors.Is(err, domain.ErrOverviewNotFound)` to the 404 case. Change the 400 text to `want ?id=<overview id>, ?review=<review id> or ?overview=<id>`. The `id`/`review` cases must still refuse when `overview` is also given: add `&& ov == ""` to both.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/web/ -run 'Overview|FileContent|ReviewDiff|Export' -count=1` → PASS. Then `go test ./internal/web/ -count=1` → PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/web/overviews_http.go internal/web/overviews_http_test.go internal/web/filecontent.go internal/web/diff.go internal/web/reviewdiff.go internal/web/overview_export_http.go
git commit -F <msgfile>   # "web: a permanent overview's document, link, remarks, diff and export routes"
```

---

### Task 2: Server — the lists (counts, View all notes, PR notes) and delete

**Files:**
- Modify: `internal/web/notes.go` (`handleNoteCounts`), `internal/web/allnotes.go`, `internal/web/prnotes.go`, `internal/web/overviews_http.go` (remove)
- Test: `internal/web/overviews_http_test.go` (append), `internal/web/prnotes_reviews_test.go` (append)

**Interfaces:**
- Consumes: `NoteCounts.Overviews []OverviewHead`, `NoteCounts.PreviewOverviews map[string][]OverviewHead`, `NoteCommitNotes.Overviews []Overview`, `PreviewNoteSet.Overviews(NoteCounts)`, `RemoveOverview(ctx,id)`.
- Produces:
  - `type overviewHeadWire struct{ ID, Commit, Branch, Agent, Title, Created string; Older bool }` (json: `id, commit, branch, agent, title, created, older`). `overviewHeads(hs []domain.OverviewHead) []overviewHeadWire` is never nil.
  - `/api/notes/counts` gains `"overviews"`: the commit overviews, newest first, the rows' source. It also gains `"overview_ids"`: every overview id, commit and PR, the gone-check's source.
  - `/api/notes/overview` `commits[].overviews` is `[]overviewHeadWire` (a commit's, never a PR's).
  - `/api/pr/notes` gains `"overviews"` (the PR's, `Older` set).
  - `POST /api/overview/remove {"id"}` (writeGuard) → 200 `{}`; 404 gone; 400 bad id.

- [ ] **Step 1: Write the failing tests** (append to `overviews_http_test.go`):

```go
func TestCountsCarryOverviews(t *testing.T) {
	t.Parallel()
	ts, svc, id := overviewServer(t)
	head, _, _ := svc.ResolveRev(context.Background(), "HEAD")
	var c struct {
		Overviews   []overviewHeadWire `json:"overviews"`
		OverviewIDs []string           `json:"overview_ids"`
	}
	if code := getJSON(t, ts, "/api/notes/counts", &c); code != 200 || len(c.Overviews) != 1 ||
		c.Overviews[0].ID != id || c.Overviews[0].Commit != strings.TrimSpace(head) || c.Overviews[0].Branch != "main" ||
		c.Overviews[0].Title != "How f works" || len(c.OverviewIDs) != 1 || c.OverviewIDs[0] != id {
		t.Fatalf("counts = %d %+v", code, c)
	}
	var all struct {
		Commits []struct {
			Hash      string             `json:"hash"`
			Overviews []overviewHeadWire `json:"overviews"`
		} `json:"commits"`
	}
	if code := getJSON(t, ts, "/api/notes/overview?on=some-other-branch", &all); code != 200 || len(all.Commits) != 1 || len(all.Commits[0].Overviews) != 1 {
		t.Fatalf("all notes = %d %+v (R5: any branch)", code, all)
	}
}

func TestRemoveOverview(t *testing.T) {
	t.Parallel()
	ts, _, id := overviewServer(t)
	if code := postJSON(t, ts, "/api/overview/remove", `{"id":"`+id+`"}`, "application/json", "", nil); code != 200 {
		t.Fatalf("remove = %d", code)
	}
	if code := postJSON(t, ts, "/api/overview/remove", `{"id":"`+id+`"}`, "application/json", "", nil); code != 404 {
		t.Fatalf("remove again = %d, want 404", code)
	}
	if code := postJSON(t, ts, "/api/overview/remove", `{"id":"../x"}`, "application/json", "", nil); code != 400 {
		t.Fatalf("bad id = %d, want 400", code)
	}
	if code := getJSON(t, ts, "/api/overview/"+id, nil); code != 404 {
		t.Fatalf("after remove = %d", code)
	}
}
```

Append to `prnotes_reviews_test.go`:

```go
// R10: a PR's overviews ride /api/pr/notes only — never the counts' rows.
// Serial: sendServer.
func TestPRNotesListsThePRsOverviews(t *testing.T) {
	ts, _, srv, _, _ := sendServerFull(t)
	svc, ctx := srv.service(), context.Background()
	pr, ok := srv.cachedPR(svc, 7)
	if !ok {
		t.Fatal("PR #7 is not listed")
	}
	pair := svc.PRPair(ctx, pr)
	set, err := svc.PreviewNotes(ctx, pair.Head, pair.Base)
	if err != nil {
		t.Fatal(err)
	}
	tg, err := svc.LinkOverviewTarget(ctx, domain.Resolved{Preview: &set, Commit: set.Tip,
		Addr: model.FileAddress{State: model.StateCommitted, Commit: set.Tip}})
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := svc.SaveOverview(ctx, domain.SaveOverview{Target: tg, Agent: "claude", Text: []byte(
		`{"version":1,"title":"How #7 works","overview":"[here](pr7.txt:1)","files":[]}`)})
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Overviews []overviewHeadWire `json:"overviews"`
	}
	if code := getJSON(t, ts, "/api/pr/notes?n=7", &out); code != 200 || len(out.Overviews) != 1 || out.Overviews[0].ID != got.ID || out.Overviews[0].Older {
		t.Fatalf("pr notes = %d %+v", code, out.Overviews)
	}
	var c struct {
		Overviews   []overviewHeadWire `json:"overviews"`
		OverviewIDs []string           `json:"overview_ids"`
	}
	if getJSON(t, ts, "/api/notes/counts", &c); len(c.Overviews) != 0 || len(c.OverviewIDs) != 1 {
		t.Fatalf("counts = %+v (a PR overview is not a row, but is an id)", c)
	}
}
```

(Add the `model` import. If `pr7.txt` is not the fixture's file name, use the file `prReviewDoc` names — that is the fixture's file.)

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/web/ -run 'CountsCarryOverviews|RemoveOverview|PRNotesListsThePRsOverviews' -count=1`
Expected: FAIL — `undefined: overviewHeadWire`.

- [ ] **Step 3: Implement**

`overviews_http.go` — add the wire type, the converter and the remove route:

```go
// overviewHeadWire is one overview as a row needs it.
type overviewHeadWire struct {
	ID      string `json:"id"`
	Commit  string `json:"commit"`
	Branch  string `json:"branch,omitempty"`
	Agent   string `json:"agent"`
	Title   string `json:"title"`
	Created string `json:"created"`
	Older   bool   `json:"older,omitempty"` // a PR overview of a head the PR has moved past
}

// overviewHeads is hs on the wire; never nil.
func overviewHeads(hs []domain.OverviewHead) []overviewHeadWire {
	out := make([]overviewHeadWire, 0, len(hs))
	for _, h := range hs {
		out = append(out, overviewHeadWire{ID: h.ID, Commit: h.Commit, Branch: h.Branch, Agent: h.Agent, Title: h.Title,
			Created: wireTime(h.Created), Older: h.Older})
	}
	return out
}

// overviewIDs is every overview id the counts hold — a commit's and a PR's:
// the page closes an open overview no longer among them.
func overviewIDs(c domain.NoteCounts) []string {
	out := []string{}
	for _, h := range c.Overviews {
		out = append(out, h.ID)
	}
	for _, hs := range c.PreviewOverviews {
		for _, h := range hs {
			out = append(out, h.ID)
		}
	}
	slices.Sort(out)
	return out
}

func (s *Server) handleOverviewRemove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	id, ok := overviewID(w, req.ID)
	if !ok {
		return
	}
	if err := s.service().RemoveOverview(r.Context(), id); err != nil {
		overviewErr(w, err)
		return
	}
	writeJSON(w, map[string]any{})
}
```

Register `mux.HandleFunc("POST /api/overview/remove", writeGuard(s.handleOverviewRemove))`. Use the body decoder the other POST handlers use (grep `handleNoteRemove` for its name). Spec §10 says `DELETE /api/overview/{id}`. **Ruling:** `POST …/remove`, the shape every mutation here takes — `writeGuard` wants a JSON body, and the page's `postJSON` and the tests' `postJSON` exist. *Cost if wrong:* one route rename. Record it in the ledger.

`notes.go` `handleNoteCounts` map gains:

```go
		"overviews":    overviewHeads(c.Overviews), // the commit rows' ≡, the menu rows, the branch sub-rows
		"overview_ids": overviewIDs(c),            // every overview, a PR's too: an open one no longer here closes
```

The page's `refreshNoteCounts` failure fallback must keep both keys (Task 3 does that).

`allnotes.go`: `overviewCommitWire` gains `Overviews []overviewHeadWire \`json:"overviews"\``. In the commits loop, fill it from `c.Overviews` (each `domain.Overview` → `overviewHeadWire{ID, Commit, Branch, Agent, Title, Created: wireTime(o.Created)}`, never nil).

`prnotes.go` — after the reviews block:

```go
	// The PR's permanent overviews (R10): its view's Overviews block only.
	if c, cerr := svc.NoteCounts(ctx); cerr == nil {
		out["overviews"] = overviewHeads(set.Overviews(c))
	}
```

and `"overviews": []overviewHeadWire{}` in the initial `out` map, so the field is always an array.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/web/ -run 'Overview|NoteCounts|NotesOverview|PRNotes' -count=1` → PASS. Then `go test ./internal/web/ -count=1` → PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/web/overviews_http.go internal/web/overviews_http_test.go internal/web/notes.go internal/web/allnotes.go internal/web/prnotes.go internal/web/prnotes_reviews_test.go
git commit -F <msgfile>   # "web: overviews in the counts, View all notes and the PR notes; remove"
```

---

### Task 3: The page — a kind-aware stored overview (viewer, split, diff, export, box title)

**Files:**
- Modify: `internal/web/static/viewer.js`, `internal/web/static/splitdiff.js`, `internal/web/static/files.js` (`refreshNoteCounts`), `internal/web/static/reviews.js` (the `openStoredOverview` call)
- Test: `internal/web/storedoverviewjs_test.go` (append), `internal/web/viewernotesjs_test.go` (append a pure test)

**Interfaces:**
- Consumes: Task 1's routes and `noteRow.kind`; Task 2's `overviews` / `overview_ids` counts.
- Produces (viewer.js; each named function below lives inside the `// --- overview model (pure; guarded against Go) ---` block):
  - `storedRef(stored) → {kind: "review"|"overview", id}` — parses `review-overview:<id>` / `overview:<id>`; `{kind:"", id:""}` for anything else.
  - `storedQuery(stored) → "review=<id>" | "overview=<id>" | ""`.
  - `remarksQuery(from) → storedQuery(from.stored)` while `from` is a live stored way back, else `""`. It replaces `remarksReview`.
  - `goneStoredOverview(ov, ids) → bool` — a permanent stored overview whose id is not in `ids`.
  - `openStoredOverview({ id, kind = "review", title, blocks, anchors, tip, text })` — `kind:"overview"` makes `stored.id = "overview:" + id`.
  - `export async function openPermanentOverview(id)` — GET `/api/overview/{id}`, then `openStoredOverview({kind:"overview", id, title: "Overview: " + d.title, …, tip: d.commit})`. A 404 says `that overview no longer exists`.
  - `export function closeGoneOverview(ids)` — the page-side `dropGoneOverviews`.
  - `fetchContent(src, rev, path, remarks = "", init)` — `remarks` is a query fragment (`storedQuery`'s value).
  - `noteBoxTitle(n)` uses `n.kind || "review"` for a read-only row.

- [ ] **Step 1: Write the failing tests**

Append to `viewernotesjs_test.go`:

```go
func TestStoredRefJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "viewer.js", voPureStart, voPureEnd, `
console.log([
  JSON.stringify(storedRef({ id: "review-overview:ab12" })),
  JSON.stringify(storedRef({ id: "overview:cd34" })),
  JSON.stringify(storedRef({ id: "f7" })), JSON.stringify(storedRef(null)),
  storedQuery({ id: "review-overview:ab12" }), storedQuery({ id: "overview:cd34" }), storedQuery(null) === "",
  remarksQuery({ stored: { id: "overview:cd34" } }), remarksQuery({ stored: { id: "overview:cd34" }, closed: true }) === "",
  remarksQuery({ id: "f7" }) === "", remarksQuery(null) === "",
  goneStoredOverview({ stored: { id: "overview:cd34" } }, ["ab12"]), goneStoredOverview({ stored: { id: "overview:cd34" } }, ["cd34"]),
  goneStoredOverview({ stored: { id: "review-overview:ab12" } }, []), goneStoredOverview(null, []),
].join("|"));
`)
	want := `{"kind":"review","id":"ab12"}|{"kind":"overview","id":"cd34"}|{"kind":"","id":""}|{"kind":"","id":""}|review=ab12|overview=cd34|true|overview=cd34|true|true|true|true|false|false|false`
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

func TestNoteBoxTitleOwnerJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "viewer.js", vnPureStart, vnPureEnd, `
console.log([
  noteBoxTitle({ readonly: true, author: "Claude", start: 2, end: 2 }),
  noteBoxTitle({ readonly: true, kind: "overview", author: "Claude", start: 2, end: 3 }),
].join("|"));
`)
	if want := "review · Claude · line 2|overview · Claude · lines 2–3"; out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}
```

Append to `storedoverviewjs_test.go`:

```go
// Spec 2026-10-12-permanent-overviews §10: a permanent overview opens in the
// viewer's stored mode, and every lane names its kind (review= / overview=).
func TestPermanentOverviewIsWired(t *testing.T) {
	t.Parallel()
	v := readStatic(t, "viewer.js")
	for _, want := range []string{
		`export async function openPermanentOverview(id)`,
		`getJSON("/api/overview/" + encodeURIComponent(id))`,
		`kind: "overview"`,
		`function fetchContent(src, rev, path, remarks = "", init = undefined)`,
		`(remarks ? "&" + remarks : "")`,
		`remarksQuery(from)`,
		`export function closeGoneOverview(ids)`,
		`"the overview was deleted"`,
		`"/api/overview-export?" + q`,
	} {
		if !strings.Contains(v, want) {
			t.Errorf("viewer.js lacks %q", want)
		}
	}
	for _, gone := range []string{`replace(/^review-overview:/, "")`, `function remarksReview(`} {
		if strings.Contains(v, gone) {
			t.Errorf("viewer.js still has %q (storedRef reads the kind)", gone)
		}
	}
	if sd := readStatic(t, "splitdiff.js"); strings.Contains(sd, `replace(/^review-overview:/, "")`) || !strings.Contains(sd, `storedQuery(from.stored)`) {
		t.Error("splitdiff.js does not ask storedQuery for the stored overview's lane")
	}
	if f := readStatic(t, "files.js"); !strings.Contains(f, `overviews: c.overviews || []`) || !strings.Contains(f, `closeGoneOverview(state.noteCounts.overview_ids)`) {
		t.Error("files.js does not keep the counts' overviews or close a gone one")
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/web/ -run 'StoredRefJS|NoteBoxTitleOwnerJS|PermanentOverviewIsWired' -count=1`
Expected: FAIL — `storedRef is not defined`; the wiring strings missing.

- [ ] **Step 3: Implement**

`viewer.js`, inside the overview model block (pure: no DOM, no state), replacing `remarksReview`:

```js
// storedRef is a stored overview's owner: a review's stored overview
// ("review-overview:<id>") or a permanent overview ("overview:<id>").
function storedRef(stored) {
  const s = String((stored && stored.id) || "");
  if (s.startsWith("review-overview:")) return { kind: "review", id: s.slice("review-overview:".length) };
  if (s.startsWith("overview:")) return { kind: "overview", id: s.slice("overview:".length) };
  return { kind: "", id: "" };
}

// storedQuery is the lane a stored overview's reads go through: its remarks
// on a file, its D diff, its export ("" = none).
function storedQuery(stored) {
  const r = storedRef(stored);
  return r.id ? r.kind + "=" + encodeURIComponent(r.id) : "";
}

// remarksQuery is the remarks lane of a file opened from way back from: a
// stored overview still open; "" otherwise.
function remarksQuery(from) {
  return from && from.stored && !from.closed ? storedQuery(from.stored) : "";
}

// goneStoredOverview: ov is a permanent overview no longer among ids (the
// counts' overview_ids) — deleted elsewhere, so the page closes it.
function goneStoredOverview(ov, ids) {
  const r = storedRef(ov && ov.stored);
  return r.kind === "overview" && !(ids || []).includes(r.id);
}
```

`noteBoxTitle` (note model block): `if (n.readonly) return (n.kind || "review") + " · " + n.author + " · " + at;`.

`fetchContent`: the fourth parameter becomes `remarks = ""`, appended as `(remarks ? "&" + remarks : "")`. Every caller passes `remarksQuery(from)` (lines ~572, ~2548) or `sp.ov.stored ? storedQuery(sp.ov.stored) : ""` (the stack, ~1375).

`docName`: a stored overview reads `"overview of review " + id` for a review and `"overview " + JSON.stringify(v.ov.title)` for a permanent one (via `storedRef`).

`exportOverviewHTML`: `const q = ov.stored ? storedQuery(ov.stored) : "id=" + …`.

`stackReviewFiles` compares `"review-overview:" + rv.id === st.id` — unchanged (a permanent overview has no review view; the stack falls back to the anchors' files).

`openStoredOverview({ id, kind = "review", title, blocks, anchors, tip, text })`: `id: (kind === "overview" ? "overview:" : "review-overview:") + id`.

```js
// openPermanentOverview opens permanent overview id (spec 2026-10-12 §10):
// the stored mode, its anchors opening the files at the overview's commit.
export async function openPermanentOverview(id) {
  let d;
  try {
    d = await getJSON("/api/overview/" + encodeURIComponent(id));
  } catch (e) {
    return opLine(e.status === 404 ? "that overview no longer exists" : "overview: " + (e.message || e), true);
  }
  openStoredOverview({ kind: "overview", id, title: "Overview: " + d.title, blocks: d.blocks, anchors: d.anchors || [], tip: d.commit, text: d.text || "" });
}

// closeGoneOverview closes the viewer when it shows a permanent overview the
// refreshed counts no longer hold (the TUI's dropGoneOverviews).
export function closeGoneOverview(ids) {
  const ov = split ? split.ov : view.ov;
  if (!isOpen() || !goneStoredOverview(ov, ids)) return;
  closeViewer("close");
  opLine("the overview was deleted", false);
}
```

(`isOpen`, `split`, `view`, `closeViewer` and `opLine` are already in viewer.js's scope; check `opLine`'s import and add it if missing.)

`splitdiff.js` `diffSource`: `if (from.stored) { const q = storedQuery(from.stored); return q ? "/api/diff?" + q + "&path=" + encodeURIComponent(path) : ""; }`. `storedQuery` lives in viewer.js; if splitdiff.js is imported BY viewer.js (check the import direction), move `storedRef`/`storedQuery` into a module both import, keeping the pure-block markers on the copy `runPureJS` reads. `diffNotice`'s words: `"the review is gone"` → `kind === "overview" ? "the overview is gone" : "the review is gone"`, and the no-change words likewise (`"no change to this file in the overview's commit"`). Pass the kind from `storedRef(from.stored).kind` at its caller.

`files.js` `refreshNoteCounts`: add `overviews: c.overviews || []` and `overview_ids: c.overview_ids || []` to `state.noteCounts`, and the same empty arrays to the failure fallback. After the assignment: `closeGoneOverview(state.noteCounts.overview_ids);` (import from viewer.js). On a FAILED fetch, do not close anything: the fallback's `[]` would close every open overview. Guard by calling it in the success branch only.

`reviews.js` `openReviewOverview` passes `kind: "review"` explicitly (readability).

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/web/ -run 'JS|Wired|Stored|Split|Viewer' -count=1` → PASS. Then `go test ./internal/web/ -count=1` → PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/web/static/viewer.js internal/web/static/splitdiff.js internal/web/static/files.js internal/web/static/reviews.js internal/web/storedoverviewjs_test.go internal/web/viewernotesjs_test.go
git commit -F <msgfile>   # "web: the stored overview knows its kind — a permanent overview opens through the same viewer"
```

---

### Task 4: The page — `≡` on commit rows, the commit menu, the branch sub-rows, delete

**Files:**
- Modify: `internal/web/static/commits.js` (row mark + menu), `internal/web/static/sidebar.js` (sub-rows, click, right-click), `internal/web/static/reviews.js` (or a new `internal/web/static/overviews.js` — preferred: one module for the overview row helpers), `internal/web/static/style.css`, `internal/web/static/index.html` (help rows)
- Create: `internal/web/static/overviews.js`, `internal/web/overviewsjs_test.go`

**Interfaces:**
- Consumes: Task 2's `state.noteCounts.overviews`; Task 3's `openPermanentOverview`.
- Produces (`overviews.js`):
  - pure block `// --- overview rows (pure; guarded against Go) ---` … `// --- end overview rows ---` holding `commitOverviews(ovs, hash)` (that commit's overviews, newest first), `branchOverviews(ovs, b)` (`o.branch === b.name && hash matches b's tip` — `b.hash` is git's short sha, so compare with `startsWith`), `overviewRowText(o, now)` = `"└ Overview: <title> <MM-DD HH:MM> <agent>"` (the TUI's `branchOverviewRowBody`; reuse `reviews.js`'s stamp helper for the date).
  - `export function overviewMenu(id, x, y)` — Open overview / Delete overview.
  - `export function confirmDeleteOverview(id)` — `showLocalConfirm("Delete overview by <agent>?", ["cancel","delete"], …)` → POST `/api/overview/remove` (404 = already gone), then `refreshNoteCounts()` (which closes it if open) and `opLine("overview deleted")`.

- [ ] **Step 1: Write the failing tests** — `internal/web/overviewsjs_test.go`:

```go
package web

import (
	"strings"
	"testing"
)

const orPureStart = "// --- overview rows (pure; guarded against Go) ---"
const orPureEnd = "// --- end overview rows ---"

func TestOverviewRowsJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "overviews.js", orPureStart, orPureEnd, `
const ovs = [
  { id: "o2", commit: "abc1234def", branch: "feat", agent: "Claude", title: "Parsing", created: "2026-10-12T09:40:00Z" },
  { id: "o1", commit: "abc1234def", branch: "feat", agent: "Claude", title: "Locks", created: "2026-10-11T08:00:00Z" },
  { id: "o3", commit: "fff0000aaa", branch: "feat", agent: "Claude", title: "Old tip", created: "2026-10-10T08:00:00Z" },
];
console.log([
  commitOverviews(ovs, "abc1234def").map((o) => o.id).join("+"),
  commitOverviews(ovs, "0000").length,
  branchOverviews(ovs, { name: "feat", hash: "abc1234" }).map((o) => o.id).join("+"),
  branchOverviews(ovs, { name: "main", hash: "abc1234" }).length,
  overviewRowText(ovs[1], new Date("2026-10-12T12:00:00Z")).startsWith("└ Overview: Locks "),
].join("|"));
`)
	if want := "o2+o1|0|o2+o1|0|true"; out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

func TestOverviewSurfacesAreWired(t *testing.T) {
	t.Parallel()
	c := readStatic(t, "commits.js")
	for _, want := range []string{`class="ovmark"`, `commitOverviews(`, `"open overview: " + `} {
		if !strings.Contains(c, want) {
			t.Errorf("commits.js lacks %q", want)
		}
	}
	s := readStatic(t, "sidebar.js")
	for _, want := range []string{`class="bov" data-overview=`, `li.dataset.overview`, `overviewMenu(`} {
		if !strings.Contains(s, want) {
			t.Errorf("sidebar.js lacks %q", want)
		}
	}
	o := readStatic(t, "overviews.js")
	for _, want := range []string{`"Open overview"`, `"Delete overview"`, `"/api/overview/remove"`} {
		if !strings.Contains(o, want) {
			t.Errorf("overviews.js lacks %q", want)
		}
	}
	if !strings.Contains(readStatic(t, "style.css"), `.crow .ovmark`) {
		t.Error("style.css lacks the ≡ mark's rule")
	}
	if !strings.Contains(readStatic(t, "index.html"), `open overview:`) {
		t.Error("index.html help lacks the overview rows")
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/web/ -run 'OverviewRowsJS|OverviewSurfacesAreWired' -count=1`
Expected: FAIL — `overviews.js` missing.

- [ ] **Step 3: Implement**

`overviews.js` (imports: `state`, `esc`, `postJSON` from core.js; `showCtxMenu` from layers.js; `opLine`, `showLocalConfirm` from ops.js; `refreshNoteCounts` from files.js; `openPermanentOverview` from viewer.js; the stamp helper from reviews.js). Put the pure functions between the guard markers:

```js
// --- overview rows (pure; guarded against Go) ---
// commitOverviews is commit hash's permanent overviews, newest first (R5:
// from any branch).
function commitOverviews(ovs, hash) {
  return (ovs || []).filter((o) => o.commit === hash).sort((a, b) => (b.created || "").localeCompare(a.created || ""));
}

// branchOverviews is b's overviews of its CURRENT tip, made on b (R5).
function branchOverviews(ovs, b) {
  return commitOverviewsAt(ovs, b.hash).filter((o) => o.branch === b.name);
}

function commitOverviewsAt(ovs, short) {
  return (ovs || []).filter((o) => short && o.commit.startsWith(short)).sort((a, b) => (b.created || "").localeCompare(a.created || ""));
}

// overviewRowText is a branch sub-row: "└ Overview: <title> 10-12 09:40 <agent>".
function overviewRowText(o, now = new Date()) {
  return ["└ Overview:", o.title, overviewStamp(o.created, now), o.agent].filter(Boolean).join(" ");
}
// --- end overview rows ---
```

`overviewStamp` is the review rows' date format. If `reviews.js`'s stamp helper is outside its own pure block, copy its body (a few lines) into this block as `overviewStamp`. The guard runs this block alone.

`commits.js` `rowHTML`: after the `✎` span add `(commitOverviews(state.noteCounts && state.noteCounts.overviews, row.hash).length ? '<span class="ovmark" title="a permanent overview: right-click → open overview">≡</span>' : "")`. In `showCommitMenu`, after the review row:

```js
  for (const o of commitOverviews(state.noteCounts && state.noteCounts.overviews, c.hash))
    items.push({ label: "open overview: " + o.title, act: () => openPermanentOverview(o.id) });
```

`sidebar.js` `renderBranches`: after the review sub-rows, `branchOverviews(state.noteCounts.overviews, b).map((o) => \`<li class="bov" data-overview="${esc(o.id)}" title="${esc(o.title)}">${esc(overviewRowText(o))}</li>\`).join("")`. Click: `if (li && li.dataset.overview) return openPermanentOverview(li.dataset.overview);`. Right-click: `if (li && li.dataset.overview) { e.preventDefault(); return overviewMenu(li.dataset.overview, e.clientX, e.clientY); }`. Both go before the `data-n` handling, like the review rows.

`style.css`: `.crow .ovmark { color: var(--accent); margin-right: 6px; }`. `#branches-list li.bov` takes the `li.brev` rules (grep `li.brev` and add `li.bov` to the same selectors).

`index.html` help: extend the commit right-click row's text with `<b>open overview: &lt;title&gt;</b> opens an agent's permanent overview of that commit (a ≡ marks such a commit) — the overview on the left, the file at the commit on the right with its remarks`. Add a sentence to the branch-row help naming the `└ Overview` sub-row (click opens, right-click = open / delete).

- [ ] **Step 4: Run the tests and a quick look**

Run: `go test ./internal/web/ -count=1` → PASS. Build `go build -o bin/gg ./cmd/gg`. Save an overview into a scratch repo with `bin/gg overview save`, run `bin/gg web` there, and look with Playwright (the Task 8 script's `commits` scenario, written now and reused): the `≡`, the menu row and the sub-row are VISIBLE, and a click opens the split.

- [ ] **Step 5: Commit**

```bash
gg add internal/web/static/overviews.js internal/web/static/commits.js internal/web/static/sidebar.js internal/web/static/style.css internal/web/static/index.html internal/web/overviewsjs_test.go
git commit -F <msgfile>   # "web: ≡ on commit rows, open overview in the commit menu, └ Overview under a branch at that tip"
```

---

### Task 5: The page — View all notes lists a commit's overviews

**Files:**
- Modify: `internal/web/static/allnotes.js`
- Test: `internal/web/allnotesjs_test.go` (append)

**Interfaces:**
- Consumes: Task 2's `commits[].overviews`; Task 3's `openPermanentOverview`; Task 4's `confirmDeleteOverview`.
- Produces: a row kind `"overview"` (`{kind:"overview", depth:3, overview, filter}`) under an `Overviews` dir in each commit.

- [ ] **Step 1: Write the failing test** — append to `allnotesjs_test.go`. Follow the file's existing pure-test pattern: grep its first `runPureJS` call for the block markers and how it feeds an overview payload to the row builder. Assert:

- a commit with `overviews: [{id:"o1", title:"How the gate works", agent:"Claude"}]` yields, after its `Reviews` rows, a `dir` row `"Overviews"` then a row `{kind: "overview"}` with that id;
- `anCopyLinkURL` of that row is `"/api/overview/o1/link"`;
- the leaf test (`leaf(r)` in `anVisible`) includes overview rows, so a query on the title keeps the row.

Plus a wiring test that `allnotes.js` holds `case "overview":`, `openPermanentOverview(r.overview.id)` and `confirmDeleteOverview(r.overview.id)`.

- [ ] **Step 2: Run it to verify it fails** — `go test ./internal/web/ -run AllNotes -count=1` → FAIL (no overview rows).

- [ ] **Step 3: Implement** in `allnotes.js`:

- the builder: after the `Reviews` block, `if ((c.overviews || []).length) { rows.push({ kind: "dir", depth: 2, text: "Overviews" }); for (const o of c.overviews) rows.push({ kind: "overview", depth: 3, overview: o, filter: (o.title + "\0" + (o.agent || "") + "\0" + (o.branch || "")).toLowerCase() }); }`;
- `anCopyLinkURL`: `if (r.kind === "overview") return "/api/overview/" + encodeURIComponent(r.overview.id) + "/link";`;
- every `r.kind === "note" || r.kind === "review"` leaf test gains `|| r.kind === "overview"` (the count, the hints, the render arm, delete);
- the render arm draws an overview row in the review row's columns: STATUS `overview`, WHO the agent, WHERE `commit` / `branch <b>`, WHEN the age, NOTE the title, with no tally. Factor `reviewCells` into a shared helper or add `overviewCells(r, now)` beside it;
- `activate`: `case "overview": openPermanentOverview(r.overview.id); return;`. Do NOT `hide()` first: the viewer is a layer over the popup, so esc returns to the popup (CLAUDE.md: a window opened from a popup returns to it). Verify this in Task 8's Playwright run;
- `deleteSelected`: `if (r.kind === "overview") return confirmDeleteOverview(r.overview.id);` before the review arm, and after the confirm resolves, `load()` the popup again. Give `confirmDeleteOverview` an optional `done` callback the popup passes;
- the help sentence at ~line 591 names overviews: "commits (with their AI reviews and permanent overviews)".

- [ ] **Step 4: Run** `go test ./internal/web/ -count=1` → PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/web/static/allnotes.js internal/web/static/overviews.js internal/web/allnotesjs_test.go
git commit -F <msgfile>   # "web: View all notes lists a commit's permanent overviews (enter opens, ctrl+d deletes, ctrl+l copies the link)"
```

---

### Task 6: The page — the PR view's Overviews block

**Files:**
- Modify: `internal/web/static/previews.js` (`loadPRCounts`, the pair resets), `internal/web/static/files.js` (resets at ~639 and ~753), `internal/web/static/reviews.js` (`reviewRowsHTML`, `headRowIds`, the enter on a selected head row), `internal/web/static/files.js` (the file-list click / right-click routing at ~5106 / ~5170)
- Test: `internal/web/overviewsjs_test.go` (append a wiring test)

**Interfaces:**
- Consumes: Task 2's `/api/pr/notes` `overviews`; Task 4's `overviewMenu` and `overviewRowText`.
- Produces: `state.previewOverviews` (the open PR's overviews; `[]` elsewhere); rows `<li class="rev rov" data-overview="<id>">` after the Reviews block, under an `Overviews` section; an `older tip` suffix on an `older` row.

- [ ] **Step 1: Write the failing test** — a wiring test asserting:
  - `previews.js` holds `state.previewOverviews = d.overviews || []`;
  - every place that sets `state.previewReviews = []` also sets `state.previewOverviews = []` (count both strings per file and require equal counts);
  - `reviews.js` holds `<li class="sect">Overviews</li>` and `data-overview=`;
  - `files.js`'s list click and right-click route `li.dataset.overview`.

- [ ] **Step 2: Run it to verify it fails.**

- [ ] **Step 3: Implement.**
  - `loadPRCounts`: `state.previewOverviews = d.overviews || [];` (a PR only: `loadPreviewCounts` and the pair counts leave it `[]`).
  - Every `state.previewReviews = []` reset gains `state.previewOverviews = [];`.
  - `reviewRowsHTML`: after `pvHTML`, `ovHTML` = `previewScopeOverviews()` rendered under `<li class="sect">Overviews</li>`. Each row is `<li class="rev rov${sel}" data-overview="${esc(o.id)}" title="${esc(o.title)}">${esc(overviewRowText(o) + (o.older ? " · older tip" : ""))}</li>`. `previewScopeOverviews()` returns `state.previewOverviews` exactly when `previewScopeReviews()` would return the PR's reviews AND `state.previewOpen && state.previewOpen.pr`; `[]` otherwise.
  - `headRowIds`: the overview rows' ids as `"overview:" + id` after the reviews, so `,`/`.` step onto them and the selection is `state.reviewSel === "overview:" + id`.
  - `openSelectedReview` (enter on a selected head row): `"overview:"`-prefixed → `openPermanentOverview(id)`.
  - `files.js` list click: `if (li && li.dataset.overview) return openPermanentOverview(li.dataset.overview);`. Right-click: `overviewMenu(...)`. Both go before the `dataset.review` arms.

- [ ] **Step 4: Run** `go test ./internal/web/ -count=1` → PASS. Run the Playwright `pr` scenario if the fixture can serve a PR. Otherwise ledger that the PR block is covered by Go + wiring tests only, and say why.

- [ ] **Step 5: Commit**

```bash
gg add internal/web/static/previews.js internal/web/static/files.js internal/web/static/reviews.js internal/web/overviewsjs_test.go
git commit -F <msgfile>   # "web: a PR view lists its permanent overviews (R10)"
```

---

### Task 7: `?overview=` links in the page

**Files:**
- Modify: `internal/web/static/live.js` (`steerNavigate`), `internal/web/static/links.js` (`linkHintKindOK`)
- Test: `internal/web/steerhintjs_test.go` (append a wiring test) and `internal/web/linksjs_test.go` (the hint twin)

**Interfaces:**
- Consumes: Task 3's `openPermanentOverview`; the server's steer wire already passes `hint_kind:"overview"` (`model.LinkHintKindOK` holds it).
- Produces: `async function steerNavigateOverview(s)` in live.js.

- [ ] **Step 1: Write the failing tests**:
  - a wiring test: `live.js` holds `if (s.hint_kind === "overview") return steerNavigateOverview(s);`, `getJSON("/api/overview/" + encodeURIComponent(s.hint_id))` and `"that overview no longer exists; opened its commit"`;
  - the JS twin of the hint kinds (the test that pins `linkHintKindOK` against `model`'s list — grep `linkHintKindOK` in `*_test.go`) must now include `overview`. If that test derives the list from `model` it fails by itself; otherwise add `"overview"` to its expectation.

- [ ] **Step 2: Run them to verify they fail.**

- [ ] **Step 3: Implement**:

```js
// steerNavigateOverview: an overview link opens the overview (the TUI's
// steerNavigateOverview). The store is asked, not the counts: an agent links
// one right after `gg overview save`. A deleted one says so and lands its
// commit.
async function steerNavigateOverview(s) {
  try {
    await getJSON("/api/overview/" + encodeURIComponent(s.hint_id));
  } catch (e) {
    opLine(e.status === 404 ? "that overview no longer exists; opened its commit" : "overview: " + (e.message || e), true);
    closeFinder();
    return steerNavigateLand({ ...s, hint_kind: "", hint_id: "" });
  }
  closeFinder();
  await refreshNoteCounts(); // just saved behind the page: its ≡ and rows show on arrival
  await openPermanentOverview(s.hint_id);
}
```

and `if (s.hint_kind === "overview") return steerNavigateOverview(s);` beside the review arm. `links.js` `linkHintKindOK` gains `|| kind === "overview"`.

- [ ] **Step 4: Run** `go test ./internal/web/ -count=1` → PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/web/static/live.js internal/web/static/links.js internal/web/steerhintjs_test.go internal/web/linksjs_test.go
git commit -F <msgfile>   # "web: ?overview= links open the overview"
```

---

### Task 8: The visible check, docs, gate

**Files:**
- Scratch (never committed): `$SCRATCH/websplit-ov/check.mjs` (Playwright; `npm i playwright@1` there)
- Modify: `CHANGELOG.md`, `README.md` (the gg web section), `docs/CLAUDE-details.md` (Permanent overviews: a "web" paragraph), `docs/superpowers/specs/2026-10-12-permanent-overviews-design.md` §10 (the remove route ruling and the dropped `GET /api/overviews?commit=` — the counts carry the rows)

- [ ] **Step 1: Playwright check (visible behaviour, memory: assert VISIBILITY)** — copy the shape of `docs/superpowers/plans/2026-10-11-web-overview-split-plan-1.md` Task 1's `check.mjs`. The fixture is a scratch repo with `a.txt`/`b.txt` and two commits, plus a permanent overview on HEAD made with `bin/gg overview save --agent Claude --stdin`, linking `a.txt:2` (remark "why two?") and `b.txt:2`. A second overview on the same commit pins two rows. Scenarios, each asserting what is VISIBLE:
  1. `commits`: the HEAD row shows `≡`; right-click → both "open overview: …" rows; clicking one opens the split — left `Overview: <title>`, right `a.txt` with the box `overview · Claude · line 2` and `why two?`.
  2. `branch`: the sidebar shows two `└ Overview:` rows under `main`; a click opens; right-click shows exactly `Open overview` / `Delete overview`.
  3. `diff`: D in the split shows the commit's diff of `a.txt`; on `b.txt` (unchanged by HEAD) the notice says `no change to this file in the overview's commit`.
  4. `allnotes`: View all notes lists both under `Overviews`; enter opens over the popup; esc returns to the popup.
  5. `gone`: with the split open, `bin/gg overview rm <id>` from the shell, then trigger a counts refresh (the `notes` live event, or call `refreshNoteCounts`). The viewer closes and `the overview was deleted` is visible.
  6. `link`: navigate `?overview=<id>` through `/api/link-command` + `steerNavigate` (or `bin/gg session navigate <link>` when steer reaches the page); the split opens.
  7. `review-overview` (regression, Review Focus 3): plan 1 of the overview split's stored review overview still opens, shows its remarks (`review · …`), D still diffs, export still downloads.
  Run against the built `bin/gg`. Prove it bites: with the `≡` span removed from `commits.js` (a reversible edit — never `git checkout` a file), scenario 1 fails.

- [ ] **Step 2: Docs**
  - CHANGELOG "Permanent overviews in gg web": the `≡`, the commit menu row, the branch sub-rows, View all notes, the PR view block, links, delete, the remark box says `overview`.
  - README's gg web section: one paragraph.
  - CLAUDE-details: the routes (`/api/overview/{id}`, `/link`, `/remove`, the `overview=` lanes), `storedRef`/`storedQuery`, `overview_ids` + `closeGoneOverview`, `overviews.js`.
  - Spec §10: amend the route list (POST remove; counts instead of `GET /api/overviews?commit=`).

- [ ] **Step 3: Gate** — `env -u GG_INBOX -u GG_MCP_URL -u GG_SESSION_TOKEN -u GG_SESSION_ID ./test.sh race > <ledger dir>/race.log 2>&1`; green only on `all green`.

- [ ] **Step 4: Commit**

```bash
gg add CHANGELOG.md README.md docs/CLAUDE-details.md docs/superpowers/specs/2026-10-12-permanent-overviews-design.md
git commit -F <msgfile>   # "docs: permanent overviews in gg web"
```

Then the final whole-branch review (a read-only subagent on the most capable model), one fix pass, and ask the user before merging. After the merge: `./build.sh install`, `./build.sh web` (the Windows web exe — memory: after every web merge), check main is clean, remove the worktree and branch.

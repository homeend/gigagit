# gg web stored overviews in the tree — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans.

**Goal:** an opened review / permanent overview is an entry of the web's shared open-files list, so ctrl+\'s tree lists it (TUI parity).
**Spec:** docs/superpowers/specs/2026-10-29-web-stored-overview-tree-design.md

## Global Constraints
- Row text: title `Overview: <label>`, meta `overview · <n> anchors`; child meta `@ <sha7>` / `working tree`; D: `nothing to delete on this row`.
- Key sources `review-overview` / `commit-overview` (never `overview`); wire steer source `overview`; page row `stored: {kind, id, tip}`.
- The server reads the document on register; nothing but `{kind, id, tab}` comes from the wire (`kind` ∈ review|overview, id validated as the existing handlers do).
- After every static/*.js edit run `go test ./internal/web`.

## Review Focus
- followDocs / overviewByID / findSessionOverview never touch a stored entry (Src differs).
- Over the cap a stored entry is never evicted; a plain close (esc) removes it unless it was backgrounded; x always removes.
- A split showing a stored overview: esc/ctrl+] send close/background for the overview's id, the right column's file as today.
- A tab whose list change names its stored overview (another tab's x) drops its viewer (the existing closed-file path), and the `!f.stored` guards still keep page-local Back copies.
- Hosted page: same Server code; the TUI's own ctrl+\ is unaffected.

### Task 1: server entry + `op: "stored"` (openfiles.go, openfiles_http.go, agentdocs_follow.go, new stored_overview_of.go)
**Produces:** `ofEntry.stored *ofStored` (`type ofStored struct{Kind, ID, Tip string; Anchors int; Children []ofChild}`), `ofEntry.backgrounded bool`; `ofChild.Rev string` (json `rev,omitempty`); `ofRow.Stored *ofStoredWire` (json `stored`: kind, id, tip); `func (r *openFiles) openStored(wt string, k ofKey, st ofStored, title, tab string) steer.OpenFile`; `func (s *Server) readStored(ctx, kind, id string) (ofStored, string /*title*/, error)`; `reviewLabel(rv domain.Review) string` factored out of handleReview.
- [ ] RED tests (stored_overview_tree_test.go): `TestStoredRegisterListsIt` (permanent overview: title, anchors count, children with rev=commit, source "overview", stored set), `TestStoredReviewOverview` (review: tip, label), `TestStoredWorkingReviewChildrenHaveNoRev`, `TestStoredSurvivesFollowDocs`, `TestStoredNeverEvicted`, `TestStoredCloseRemoves`, `TestStoredBackgroundThenCloseKeeps`, `TestStoredEverywhereCloseRemoves`, `TestStoredGoneIs404`, `TestStoredBadKindIs400`.
- [ ] Implement: wireLocked maps a stored Src to "overview"; frontLocked skips stored entries when evicting; background sets backgrounded; close: stored+backgrounded+!everywhere → background; pageList fills Stored/Children from the entry. Commit.

### Task 2: page viewer registers (viewer.js)
- [ ] RED pins (storedoverviewjs_test.go updated): openStoredOverview posts `op: "stored"` and keeps the id (`doc.fid`); viewerFileId answers it; closeViewer / backgroundViewer / the split's close send close / background for it; the old "never registered" pins replaced.
- [ ] Implement; `go test ./internal/web` PASS; commit.

### Task 3: page tree (openfiles.js)
- [ ] RED pins: docTree matches a stored child to the commit row (tip, path) or the worktree row (no tip); treeRow child meta `@ ` + sha7 / `working tree`; open / open-at on a stored row re-read via `/api/review/` / `/api/overview/` and show (anchor for open-at); 404 → close + "that overview no longer exists"; deleteAction "" for stored; y copies with `stored`; closeGoneOverview closes list rows of permanent overviews not in ids.
- [ ] Implement; `go test ./internal/web` PASS; commit.

### Task 4: docs, probe, gate
- [ ] CHANGELOG (new entry; the old "web list holds session overviews only" sentence superseded), docs/web-tui-parity.md row 112 → parity, README if the tree is described; browser probe (Playwright, scratch XDG, kill by pid); `./test.sh race` "all green"; final review subagent; ask before merge.

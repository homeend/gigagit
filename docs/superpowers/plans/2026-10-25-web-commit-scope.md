# gg web — multi-branch commit view + all files — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans (inline; CLAUDE.md forbids implementer subagents). Steps use checkbox (`- [ ]`) syntax.

**Goal:** the web's commit-list scope becomes the TUI's ordered set (Add / Remove from commit view, Show all branches, upstreams in the walk, ◉ marks, segments over the set), and a commit's files view gets the TUI's `a` all-files toggle.

**Architecture:** server state `s.scope []string` replaces `s.solo`; `/api/solo` gains explicit add/remove ops; `/api/commits` computes upstreams per request and answers `scope`. Client keeps `state.solo` as the one-entry view and adds `state.scope`. All files = `/api/commit/{sha}?all=1` (TreeFiles) + `state.filesAll`, whose rows diff commit ↔ working tree through `/api/entry-diff`.

**Tech Stack:** Go (internal/web), ES modules (internal/web/static), Go tests with source pins + node-run pure functions, Playwright probe.

**Spec:** docs/superpowers/specs/2026-10-25-web-commit-scope-design.md

## Global Constraints

- TUI words in the web's lowercase menu convention: `add to commit view`, `remove from commit view`, `show all branches`; chip `solo: X ✕` / `<n> branches ✕`; title `(all files)`; diff context `<short> ↔ working tree`.
- `go test ./internal/web` after every static edit; gofmt before the race gate.
- Old request shapes of `/api/solo` keep working.

## Review Focus

1. Removing a member branch that was deleted since it was added must succeed (no ref lookup on remove).
2. A second tab sees the scope via `scope` on every `/api/commits` answer; `solo` empty when the list has several.
3. Upstream list changes between requests (a push set an upstream) re-walk the feed instead of answering the old one.
4. `a` on a stash detail or a review does nothing; a mode left on never leaks into a compare/status/stash screen.
5. An all-files row of a file deleted in the working tree opens (working side lenient, reads empty).

---

### Task 1: server scope list + add/remove ops

**Files:** Modify `internal/web/solo.go`, `internal/web/commits.go`, `internal/web/search.go`, `internal/web/server.go`; Test `internal/web/solo_test.go`.

**Interfaces — Produces:** `s.scope []string` (encoded entries); `(*Server).scopeEntries() []scopeEntry` (`scopeEntry{Kind, Ref string}` json `kind`,`ref`); `scopeFor(entries []string, ups []string, f feedFilter) domain.LogScope`; answers carry `scope`.

- [ ] Tests (fail first): `TestSoloAddRemoveBuildsSet` (add feature + main → `scope` has both, `solo` ""; remove one → `solo` = the other; remove last → all), `TestSoloRemoveDeletedBranch` (add, delete the branch with git, remove → 200, scope empty), `TestSoloAddRefusesTagAndUnknown` (tag → 404, unknown → 404, unsafe → 400), `TestSoloSetReplacesSet` (`{branch}` after two adds → one entry), `TestSoloAddCommitScopeKeepsSha` (commit solo then add branch → 2 entries, first kind commit).
- [ ] Implement; old `{branch}` / `{kind,ref}` shapes unchanged.
- [ ] `go test ./internal/web -run Solo` green; commit.

### Task 2: upstreams in the walk

**Files:** `internal/web/commits.go`, `internal/web/search.go`; Test `internal/web/scope_upstream_test.go`.

**Interfaces — Produces:** `(*Server).feedUpstreams(ctx, entries []string) []string`; `feedFor(r, scope domain.LogScope)`; `handleCommits` computes the scope once per request.

- [ ] Tests: `TestFeedWalksUpstreams` — origin with a commit ahead of local main (fetched, local not pulled): unscoped feed lists it; scope [feature] (no upstream) does not; scope [main] does. `TestFeedUpstreamChangeRewalks` — first request without an upstream, then `git branch -u` → next plain request lists the upstream-only commit.
- [ ] Implement (TUI `feedUpstreams` rule: in-scope local branches, Upstream exists in RemoteBranches, dedup, branch order).
- [ ] Green; commit.

### Task 3: segments over the set

**Files:** `internal/web/commits.go`; Test in `internal/web/scope_upstream_test.go` (or a new `scope_segments_test.go`).

- [ ] Test `TestScopeSegmentsForSet`: scope [a, b] answers rows whose `seg` field is set on every row (len == rows) and a's commits and b's commits do not share a segment.
- [ ] `scopeSegments(r, entries, commits)`: TUI `segBoundary` over the set.
- [ ] Green; commit.

### Task 4: client scope

**Files:** `static/commits.js` (chip, `scopeLabel`, `setSolo`, `scopeOp`, commit menu row), `static/core.js` (`state.scope`), `static/sidebar.js` (menu rows, ◉ mark), `static/reviews.js` (`viewBranches`), `static/style.css` (`.scope-mark`), help; Test `internal/web/scopejs_test.go`.

- [ ] Tests: node `scopeLabel` ([]→"", one branch → `solo: x`, one commit → `solo: <8>`, two → `2 branches`); pins for the three menu words, the ◉ span in renderBranches, viewBranches reading state.scope, chip using scopeLabel, `op: "add"` / `op: "remove"` posts.
- [ ] Implement; green; commit.

### Task 5: server all files

**Files:** `internal/web/commits.go`; Test `internal/web/allfiles_test.go`.

- [ ] Test `TestCommitAllFilesListsWholeTree`: a commit touching one of three files → plain answer 1 file, `?all=1` answers 3 with empty status and `"all": true`.
- [ ] Implement; green; commit.

### Task 6: client all files

**Files:** `static/commits.js` (openCommit/openCommitByHash/openStashDetail), `static/files.js` (`allFilesOK`, `toggleAllFiles`, `fileDiffURL` arm, openFile stack skip, title, ctx), `static/keys.js` (`a`), `static/index.html` (foot chip), `static/style.css` (`.on`), help; Test `internal/web/allfilesjs_test.go`.

- [ ] Tests: pins — `a` routed before `noteKey(e, "a")` gated on `allFilesOK()`; `fileDiffURL` all arm builds `left: "commit:"` + `right: "worktree"`; openFile skips `openStack` in all mode; `?all=1` in both openers; stash sets `fileStash`; title `(all files)`; foot chip `data-act="allfiles"`.
- [ ] Implement; green; commit.

### Task 7: docs, probe, review, gate

- [ ] CHANGELOG section; parity doc rows later on the docs branch.
- [ ] Playwright probe (Chromium + Firefox), fixture with an origin + tracking branch.
- [ ] Read-only review subagent; fix Critical/Important test-first.
- [ ] gofmt, `./test.sh race` → "all green"; build bin/gg.

# Memory documents Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans. Per CLAUDE.md this session implements every task itself, no implementer subagents; one read-only review subagent at the end. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Overviews backed by nothing (process memory), optionally plain text, created by an agent from the command line or by the user in ctrl+\, with a `gg://` link and whole-text copy.

**Architecture:** `agentdocs.Store` decides backing per overview instead of per root. The CLI carries text to the live gg through new `memdoc_*` steer verbs, answered without touching the screen. A separate `memdocs.json` presence keeps a link reachable after gg moves off that worktree, without changing how any other verb routes.

**Tech Stack:** Go 1.26, Bubble Tea TUI, embedded JS in `internal/web/static`.

**Spec:** `docs/superpowers/specs/2026-11-02-memory-docs-design.md`

## Global Constraints

- Worktree `/work/gigagit/.claude/worktrees/memory-docs`, branch `feat/memory-docs`; use absolute paths.
- Limits: 64 KiB text, 200-character one-line title, 20 memory overviews per worktree.
- Memory documents never enter the session-docs directory, session snapshots or state dumps.
- File-backed behaviour is unchanged: file rows byte-identical; `link:` line and JSON fields are the only additions.
- Exact answers: `no gg session for this worktree: a memory document needs one` (exit 1); `gg did not answer; it may still add it` (exit 1, plus id and link); `overview <id> no longer exists`; `overview <id> has no export: read it with gg overview show`.
- Text listings append `\tmemory` to memory rows only. JSON: `"backing": "file" | "memory"`, `"plain"`.
- Every TUI string through `i18n.T` in all four bundles; every new key in `help.go` and the footer; new tests `t.Parallel()` unless process-global state.
- Internal packages: `tui`/`cli` never import `internal/git`. Commit with `gg add` + `git commit -F`.

## Review Focus

- A verb from a worktree not on screen: document lands in the sender's worktree, nothing on screen moves, no switch notice.
- gg moved off the worktree, then `gg overview show <link>` from elsewhere: the text is returned; after the last document is removed, `memdocs.json` is gone and other verbs in that worktree still say "no gg session".
- Hosted page active (terminal blocked): `memdoc_add` from a console agent still lands and shows on the page.
- Text with HTML/markup/ANSI escapes in a plain document: shown literally in web and TUI, whole-text copy byte-exact.
- `n` with text typed then `esc`: asks first; empty title or text: refused in place with the window kept open.

---

### Task 1: Per-document backing in the store

**Files:**
- Modify: `internal/agentdocs/overviews.go`, `internal/agentdocs/attach.go`, `internal/agentdocs/docfile.go`
- Test: `internal/agentdocs/memory_test.go` (new)

**Interfaces:**
- Produces: `Overview.Memory bool`, `Overview.Plain bool`; `(*Store).AddMemoryOverview(root, dir, id, title, text, author string, plain bool) (Overview, error)`; `(*Store).MemoryOverviews(root string) []Overview`; `(*Store).MemoryRoots() []string`; `(*Store).OverviewDir(root, id string) string`; `(*Store).AddPlainOverview(root, dir, title, text string) (Overview, error)` for a file-backed plain document (front matter `plain = true`); `(*Store).SetOverviewPlain(root, id string, plain bool) error` for `set --plain`.

- [ ] **Step 1: Write failing tests**: `TestMemorySurvivesReload`, `TestMemorySurvivesAttachDetach`, `TestMemoryRemoveTouchesNoFile` (directory listing unchanged), `TestLegacyUnattachedStillDrops`, `TestMemoryCapPerRoot` (21st refused, files not counted), `TestMemorySameIDReplaces`, `TestMemoryIDHeldByFileRefused`, `TestPlainHasNoAnchors` (text `[a](x.go:1)` → `Anchors` empty, `Text` byte-identical), `TestPlainFrontMatterRoundTrip` (and a file without the key reads `Plain == false`, an unknown front-matter key does not make a file unreadable).
- [ ] **Step 2: Run** `cd <worktree> && go test ./internal/agentdocs/ -run 'Memory|Plain|Legacy'` → FAIL (undefined).
- [ ] **Step 3: Implement.** `AddMemoryOverview` requires `ValidID(id)`, uses `overviewRefusal`. `dropRootLocked` skips `e.Memory`. `removeOverview` and `SetOverviewIn` check `e.Memory` before `s.roots[e.Root]`. `ParseOverview` skipped when `Plain`. If an unknown front-matter key makes old readers refuse a file, bump the format as the spec says; otherwise leave it.
- [ ] **Step 4: Run** the same → PASS; `go test ./internal/agentdocs/` → PASS.
- [ ] **Step 5: Commit** `feat: agentdocs: backing per overview, plain overviews`.

### Task 2: Steer verbs and memdocs presence in the TUI

**Files:**
- Modify: `internal/steer/steer.go`, `internal/tui/steer.go`, `internal/tui/steer_kept.go`, `internal/tui/ui_switch.go`
- Create: `internal/tui/steer_memdoc.go`
- Test: `internal/tui/steer_memdoc_test.go`

**Interfaces:**
- Consumes: Task 1 store API.
- Produces: `steer.MemdocsPresence = "memdocs.json"`; `steer.IsMemdocVerb(cmd string) bool`; `Command.Plain bool`; `steer.Overview.Plain bool`; verbs `memdoc_add|set|list|show|rm` (`FileID` = minted id, `Worktree` = root's checkout); `domain.SessionInboxOf(ctx, worktree string) (string, error)` resolving via that worktree's own common dir.

- [ ] **Step 1: Write failing tests**: `TestMemdocAddFromOtherWorktreeNoScreenChange` (same `View()` before and after, no notice, doc under the sender's root); `TestMemdocAnsweredWhileBlocked` (active side = page); `TestMemdocPresenceWrittenBeforeReply`; `TestMemdocPresenceRemovedWithLastDoc`; `TestMemdocOnlyInboxRefusesOtherVerbs` (navigate → `no gg session for this worktree`); `TestMemdocNotWrittenToDocsDir`; `TestAnotherLiveGGWinsMemdocInbox`.
- [ ] **Step 2: Run** `go test ./internal/tui/ -run Memdoc` → FAIL.
- [ ] **Step 3: Implement.** Answer `IsMemdocVerb` first in the steer dispatch, before `steerShownMismatch`, off-thread for the inbox resolve; `tendKeptInboxes` touches `MemdocsPresence` for each `MemoryRoots()` inbox and drains it; removes it (and stops draining) with the last document. `doc.memInbox` recorded on the TUI's side map keyed by root. Reuse `answerSteer`.
- [ ] **Step 4: Run** → PASS; `go test ./internal/steer/ ./internal/tui/` → PASS.
- [ ] **Step 5: Commit** `feat: memdoc steer verbs and memdocs presence (TUI)`.

### Task 3: CLI

**Files:**
- Modify: `internal/cli/session_overview.go`, `internal/cli/session.go`, `internal/cli/session_docs.go`, `internal/cli/overview_show_links.go`, `internal/cli/overview.go`, `internal/cli/open.go`, `internal/domain/session_overview_link.go`, `internal/domain/linkresolve.go`, `internal/domain/linkdesc.go`
- Test: `internal/cli/session_overview_memory_test.go`

**Interfaces:**
- Consumes: verbs from Task 2.
- Produces: `routeMemdoc(inbox string) sessionRoute` (TUI/web only, then `MemdocsPresence`; IDE never); `ResolveOpts.MemoryDoc func(checkout, id string) bool`; `askMemdoc(inbox string, c steer.Command) (steer.Reply, error)`.

- [ ] **Step 1: Write failing tests** with a fake consumer goroutine draining the inbox:
  `TestAddMemoryNoLiveGG` (exact answer, exit 1, empty docs dir); `TestAddMemoryNoReply` (exact answer, exit 1, id and link printed); `TestAddMemoryIDEOnlyRefused`; `TestAddTextExclusive` (`--text` + `--file`, exit 2; `--memory` on `set`, exit 2); `TestAddPrintsLinkBothBackings`; `TestPlainNoAnchorWarning`; `TestListFileRowsByteIdentical` + memory row ends `\tmemory`; `TestListNoLiveFilesOnly`; `TestShowLinkFallsBackToLive` from another cwd; `TestShowLinkGoneAfterQuit`; `TestOpenMemoryLink`; `TestMemdocsPresenceDoesNotRouteNavigate`; `TestExportMemoryRefused`.
- [ ] **Step 2: Run** `go test ./internal/cli/ -run 'Memory|TextExclusive|PrintsLink|Plain|ListFileRows|ListNoLive|ShowLink|OpenMemory|MemdocsPresence'` → FAIL.
- [ ] **Step 3: Implement** the flags, minted id on the CLI, merging in list/show/set/rm and `docs ls`, the fallback before `sessionOverviewGone` at every site (`linkresolve.go:123,194`, `linkdesc.go:228`), `overview show` printing from the reply.
- [ ] **Step 4: Run** → PASS; `go test ./internal/cli/ ./internal/domain/` → PASS.
- [ ] **Step 5: Commit** `feat: gg session overview --memory --plain --text`.

### Task 4: TUI screens

**Files:**
- Modify: `internal/tui/overview.go`, `internal/tui/overview_keys.go`, `internal/tui/open_files.go`, `internal/tui/overview_link.go`, `internal/tui/doc_tree.go`, `internal/tui/sessions_popup.go`, `internal/tui/help.go`, `internal/tui/footer.go`, translation bundles
- Create: `internal/tui/memdoc_new.go`
- Test: `internal/tui/memdoc_screen_test.go`

**Interfaces:**
- Consumes: Task 1 store API; `OverviewDir` for the link.
- Produces: `newMemdocPopup` (layer-stack window, `pushLayer`/`popLayer`).

- [ ] **Step 1: Write failing tests**: `TestTreeMemoryTag`; `TestNNewMemdocCreatesAndOpens` (memory, plain, worktree on screen); `TestNEscAsksWhenTyped`; `TestNEmptyRefusedStaysOpen`; `TestPlainOpensFullScreenNoSplit` on a wide window; `TestPlainShowsMarkupAndEscapesLiterally`; `TestPlainMenuHasCopyAndLinkNoExport`; `TestCopyOverviewByteExact`; `TestMemoryMarkdownSplitsNoExport`; `TestMemoryLinkOnUnattachedRoot`; `TestDeleteMemoryAsksAndRemoves`; `TestRepoSwitchKeepsMemoryDoc`.
- [ ] **Step 2: Run** `go test ./internal/tui/ -run 'Memory|Memdoc|NNew|NEsc|NEmpty|Plain|CopyOverview|RepoSwitchKeeps'` → FAIL.
- [ ] **Step 3: Implement.** `n` in `Overviews & notes`; reuse the `text_templates.go` text area; plain rows bypass `md_render.go`, sanitized through the viewer's existing control-char handling; `bringToFront` skips the split for `Plain`.
- [ ] **Step 4: Run** → PASS; `go test ./internal/tui/` (AST gates included) → PASS.
- [ ] **Step 5: Commit** `feat(tui): memory documents in ctrl+\ and the plain view`.

### Task 5: gg web

**Files:**
- Modify: `internal/web/steer.go`, `internal/web/session_overview.go`, `internal/web/openfiles_http.go`, `internal/web/agentdocs_follow.go`, `internal/web/static/openfiles.js`, `internal/web/static/viewer.js`, `internal/web/static/keys.js` (help text where `?` lives)
- Test: `internal/web/memdoc_test.go`

**Interfaces:**
- Consumes: Task 1 store API; verbs from Task 2.
- Produces: `POST /api/session-overview/memory` (`{title,text}` → `{id,link}`) under `writeGuard`; `memory`, `plain` on `/api/open-files` rows.

- [ ] **Step 1: Write failing tests**: `TestWebMemdocVerbs` (answered before `toSteerWire`, hosted and standalone); `TestWebCreateRoute` (limits, guard); `TestWebOpenFilesFlags`; `TestWebLinkOnUnattachedRoot`; `TestWebStandaloneSwitchLinkGoneThenBack`.
- [ ] **Step 2: Run** `go test ./internal/web/ -run 'WebMemdoc|WebCreate|WebOpenFiles|WebLink|WebStandalone'` → FAIL.
- [ ] **Step 3: Implement**; plain text via `textContent`; `n` dialog; tag; hide export for memory and plain; new elements get their own `#id.hidden`.
- [ ] **Step 4: Run** → PASS; run the browser probe from `web.md` in Firefox and Chromium, hosted and standalone: `n`, tag, `y`, `L`, `<script>` shown literally. Compare with a TUI capture.
- [ ] **Step 5: Commit** `feat(web): memory documents`.

### Task 6: Docs, e2e, final gate

**Files:**
- Modify: `CHANGELOG.md`, `README.md`, `internal/agentskill/using-gg.md` + `agentskill.Version` + dogfood copy, `CLAUDE.md` rows (`agentdocs`, `steer`, `cli`), `docs/CLAUDE-details.md`, the web parity doc
- Create: `e2e/scenarios/s_memory_docs.toml`, `e2e/scenarios/tui_memory_docs.toml` + `.screens`

- [ ] **Step 1: Write the scenarios** (use `writing-e2e-scenarios`): agent adds, another worktree reads through the link; TUI golden of the tagged row.
- [ ] **Step 2: Run** `go test ./e2e/ -run 'memory_docs' -update` once, read the golden, then without `-update` → PASS.
- [ ] **Step 3: Update the docs**; `gg init --update` (mention global copies in the report); commit the dogfood copy.
- [ ] **Step 4: Run** `./test.sh race > <scratchpad>/race.log 2>&1`, read the verdict in a separate call → "all green".
- [ ] **Step 5: Commit**, build `<worktree>/bin/gg`, then a read-only whole-branch review subagent on the most capable model; fix findings; merge main in if it moved; rerun the race gate.

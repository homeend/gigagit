# Overview Links Implementation Plan

> **For agentic workers:** this repo forbids implementer subagents (CLAUDE.md). Execute with superpowers:executing-plans in the session that wrote it. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every overview (session and permanent) has a `gg://` link. The TUI copies it, and another agent reads it with `gg overview show <link>` from any worktree or repository.

**Architecture:** The domain gets a new link shape: a local-form, address-less `?overview=<id>` link. It names one worktree's session overview. It resolves only when the caller passes a session-docs seam (`ResolveOpts.SessionDocs`), which domain needs because it may not import `steer`. Navigation and `gg overview show` pass the seam. Every other resolver call refuses the link, so the refusal lives in one place. The CLI reads the overview through `agentdocs` attached to that worktree's docs directory. The TUI builds the link from the open document: the session form, the permanent `OverviewLink`, or the review's `ReviewLink`.

**Tech Stack:** Go 1.26, Bubble Tea, existing `agentdocs` / `linknav` / `domain` packages.

**Spec:** `docs/superpowers/specs/2026-10-28-overview-links-design.md`

## Global Constraints

- Session overview link text: `gg://<abs worktree top, slash form>?overview=<id>`, built through `linkTextIn` (it must re-parse). A path containing `@`, `#` or `?` fails `model.LinkAbsOK` and gets no link.
- Permanent links are unchanged (`OverviewLink`). A review's overview copies `ReviewLink`.
- A gone session overview: error text `overview <id> no longer exists`. CLI exit 1.
- Hint-blind refusal text: `an overview link names no files — read it with gg overview show <link>`, wrapping `model.ErrLink`.
- Web/IDE refusal text: `a session overview link opens only in the gg terminal for now`.
- Memory-only overview: TUI status `this overview is not saved to a worktree — no link`. Nothing is copied.
- `domain` must not import `steer` (archtest). `tui`/`cli` never import `git`. Every TUI string goes through `i18n.T` with all four bundles filled in. Every new key appears in both the `?` help and the footer.
- New tests call `t.Parallel()` unless they touch process-global state.

## Review Focus

1. **Same overview id in two worktrees.** The link must resolve to the worktree it names, never the reader's cwd. Pinned in Task 1 (`TestSessionOverviewLinkPicksTheNamedWorktree`) and Task 2 (`TestOverviewShowSessionLinkFromAnotherWorktree`).
2. **The worktree was removed after the link was copied.** Expect a clear "not a checkout here" error, not a panic and not a wrong repository. Pinned in Task 1 (`TestSessionOverviewLinkRemovedWorktree`).
3. **The link is pasted into a verb that needs files** (`gg diff`, `gg link text`, `gg note add`, compare). Expect the refusal that names `gg overview show`, never "the whole working tree". Pinned in Task 1 (`TestSessionOverviewLinkRefusedWithoutSeam`) and Task 2 (`TestHintBlindVerbsRefuseSessionOverviewLink`).
4. **The checkout path is a symlink or differs only in case.** `TopLevel` vs the link's path is compared with `domain.SamePath` after `filepath.EvalSymlinks` on both. Pinned in Task 1 (`TestSessionOverviewLinkThroughSymlink`, skipped on Windows).
5. **A memory-only overview or a tour.** Copying a memory-only overview refuses. A tour (`tour-<key>`) copies a link that keeps working after the tour is rewritten. Pinned in Task 4 (`TestCopyLinkMemoryOverviewRefuses`) and Task 1 (`TestSessionOverviewLinkTourID`).

---

### Task 1: Domain link shape + resolver + linknav seam

**Files:**
- Modify: `internal/agentdocs/attach.go` (add `AttachedWorktree`)
- Create: `internal/domain/session_overview_link.go`
- Modify: `internal/domain/linkresolve.go` (`ResolveOpts`, `ResolveLink` early branch)
- Modify: `internal/domain/linkdesc.go` (overview arm)
- Modify: `internal/linknav/linknav.go` (`SessionDocsDir`, `WithSessionOverviews`, `Resolve` uses it)
- Test: `internal/domain/session_overview_link_test.go`, `internal/agentdocs/attach_test.go`, `internal/linknav/linknav_test.go`

**Interfaces:**
- Produces:
  - `func (s *agentdocs.Store) AttachedWorktree(root string) string` returns the `worktreeDir` passed to `Attach`, or "" (memory only).
  - `ResolveOpts.SessionDocs func(commonDir, checkout string) string`: the checkout's session-docs dir, or "" for none. nil means the caller cannot take a session overview link.
  - `func SessionOverviewLink(worktree, id string) (string, error)`
  - `func IsSessionOverviewLink(l model.Link) bool`: `l.IsLocal() && l.Hint.Kind == model.OverviewHintKind && l.Path == "" && hintOnlyTarget(l.Target) && l.Target.State == model.StateUnstaged && l.Line == 0`
  - `func (r Resolved) SessionOverview() bool`: `r.Hint.Kind == model.OverviewHintKind && r.Commit == ""`
  - `var ErrOverviewNamesNoFiles` (wraps `model.ErrLink`) and `var ErrSessionOverviewGone`
  - `func SessionOverviewDocsDir(ctx, svc *Service, opts ResolveOpts) (string, error)`: the dir for svc's checkout. CLI Task 2 uses it.
  - `func linknav.SessionDocsDir(commonDir, checkout string) string` = `steer.DocsDir(config.SessionSteerDir(commonDir, checkout))`
  - `func linknav.WithSessionOverviews(o domain.ResolveOpts) domain.ResolveOpts`

- [ ] **Step 1: Write the failing tests** in `internal/domain/session_overview_link_test.go`. Each one writes a real overview file with `agentdocs.WriteDoc(dir, id, agentdocs.KindOverview, data, false)`, using data from `agentdocs.EncodeOverviewFile`, into a `t.TempDir()` docs dir. `opts.SessionDocs` returns that dir.
  - `TestSessionOverviewLinkRoundTrip`: `SessionOverviewLink(top, id)` == `"gg://"+top+"?overview="+id` (POSIX). `ParseLink` → `IsSessionOverviewLink` true. `ResolveLink` gives `res.Checkout` SamePath `top`, `res.SessionOverview()` true, `res.Hint.ID == id`.
  - `TestSessionOverviewLinkPicksTheNamedWorktree`: main checkout + `git worktree add` second; the same id exists in both docs dirs (the seam maps per checkout). `Cwd` = main, link names the second, so `res.Checkout` is the second.
  - `TestSessionOverviewLinkGone`: no file, so `errors.Is(err, ErrSessionOverviewGone)` and the message contains `overview <id> no longer exists`.
  - `TestSessionOverviewLinkRefusedWithoutSeam`: `opts.SessionDocs == nil`, so `errors.Is(err, ErrOverviewNamesNoFiles)` and `errors.Is(err, model.ErrLink)`.
  - `TestSessionOverviewLinkRemovedWorktree`: link to a deleted dir gives `errors.Is(err, ErrLinkUnknownRepo)`.
  - `TestSessionOverviewLinkNotATopLevel`: link to `<top>/sub` (a subdirectory) gives `ErrLinkUnknownRepo`.
  - `TestSessionOverviewLinkThroughSymlink` (skip on windows): symlink → top; a link via the symlink resolves.
  - `TestSessionOverviewLinkTourID`: id `tour-brief` resolves.
  - `TestSessionOverviewLinkBadPath`: `SessionOverviewLink("/x/a@b", "id")` errors with `model.ErrLink`.
  - `TestPermanentOverviewLinkUnchanged`: the existing `TestOverviewLinkRoundTrip` stays green. No edit.
  - `TestLinkDescSessionOverview`: `svc.LinkDesc` of the link gives kind `overview`, id `<id>`, subject = base name of the worktree.

- [ ] **Step 2: Run them to make sure they fail**
  Run: `go test ./internal/domain -run 'SessionOverviewLink|LinkDescSessionOverview' -count=1`
  Expected: FAIL (undefined: `SessionOverviewLink`).

- [ ] **Step 3: Implement.**
  - `SessionOverviewLink`: `filepath.ToSlash(filepath.Clean(worktree))`. Refuse `!model.LinkAbsOK`. Refuse `!agentdocs.ValidID(id)`. `linkTextIn(model.LinkRepo{Abs: abs}, model.Link{Target: model.LinkTarget{State: model.StateUnstaged}, Hint: model.LinkHint{Kind: model.OverviewHintKind, ID: id}})`.
  - In `ResolveLink`, right after the ctx check and before the committed-without-sha guard: `if IsSessionOverviewLink(l) { return resolveSessionOverview(ctx, l, opts) }`.
  - `resolveSessionOverview`:
    1. nil seam → `ErrOverviewNamesNoFiles`.
    2. `opts.OpenFn(abs)` (default `Open`). Its `TopLevel` must equal `abs` under `SamePath` after `EvalSymlinks` on both, else `ErrLinkUnknownRepo` "%s is not a checkout here".
    3. `dir := opts.SessionDocs(commonDir, top)`. Empty → `ErrSessionOverviewGone`.
    4. `agentdocs.ReadDoc(dir, id, agentdocs.KindOverview)`. `fs.ErrNotExist` → `ErrSessionOverviewGone` ("overview %s no longer exists"). Any other error → return it.
    5. Return `Resolved{Checkout: top, Addr: model.FileAddress{State: model.StateUnstaged, Worktree: top}, Side: model.NoteSideNew, Hint: l.Hint}`.
  - `SessionOverviewDocsDir` = `opts.SessionDocs(svc.GitCommonDir, svc.TopLevel)`.
  - `linkDescFields`: in the overview arm, before the stored lookup, `if IsSessionOverviewLink(l) { return "overview", l.Hint.ID, path.Base(l.Repo.Abs) }`.
  - `linknav.Resolve` uses `WithSessionOverviews(Opts(...))`. `Opts` itself stays seam-less: compare and the base suggestion use it.

- [ ] **Step 4: Add `TestAttachedWorktree`** in `internal/agentdocs`: after `Attach(root, wt, dir)` it returns `wt`, and "" after `Detach` and for an unknown root. Add `TestResolveTakesSessionOverviewLinks` in `linknav`: `Resolve` on such a link succeeds with an XDG-pinned docs dir (not parallel: uses Setenv). Implement `AttachedWorktree`.

- [ ] **Step 5: Run the tests to make sure they pass**
  Run: `go test ./internal/domain ./internal/agentdocs ./internal/linknav ./internal/archtest -count=1`
  Expected: PASS.

- [ ] **Step 6: Commit** `feat(domain): session overview links (gg://<worktree>?overview=<id>)`

### Task 2: CLI `gg overview show <link>` for both kinds, anchors as links

**Files:**
- Modify: `internal/cli/link.go` (`linkShapes.SessionOverview`, `resolveLinkArg`, `gg link resolve`)
- Modify: `internal/cli/session_docs_store.go` (`openDocsAt`)
- Modify: `internal/cli/overview.go` (`overviewShow` session branch, anchors section for both kinds)
- Create: `internal/domain/anchor_link.go`
- Test: `internal/cli/overview_link_test.go`, `internal/domain/anchor_link_test.go`

**Interfaces:**
- Consumes: Task 1 (`Resolved.SessionOverview`, `linknav.WithSessionOverviews`, `SessionOverviewDocsDir`).
- Produces:
  - `func AnchorLink(repo model.LinkRepo, commit string, a agentdocs.Anchor) (string, error)`: commit "" means the working tree (`StateUnstaged`), else `StateCommitted`. `Line = a.Start`, `End = a.End` when `a.End > a.Start`. Path = `a.Path`. A note anchor gets no link: the caller prints the note.
  - `linkShapes.SessionOverview bool`. When set, `resolveLinkArg` resolves with `linknav.WithSessionOverviews(opts)`.
  - `func openDocsAt(verb, top, inbox string, svc *domain.Service, stderr io.Writer) (*docsSession, bool)`. `openDocs` delegates to it.

- [ ] **Step 1: Write the failing tests.** Docs go in through `runOverview(...,"add",...)` against the inbox `config.SessionSteerDir(cd, top)`. CLI's TestMain pins XDG.
  - `TestOverviewShowSessionLinkFromAnotherWorktree`: repo + `git worktree add wt2`. Add an overview in wt2 whose text is `See [gate](a.go:3-4) and [why](note:<noteID>)`, after `gg session note add a.go:3 …` in wt2. `gg overview show <link>` run with svc = main gives exit 0. The output contains the title, the text, a line with `gg://<wt2>/a.go:3-4`, and the note's summary. `--json` has `"kind":"session"`, `"link"`, and `"anchors"`, where `[0].link` has the `gg://<wt2>/a.go` prefix and `[1].note.summary` is set.
  - `TestOverviewShowSessionLinkGone`: rm it, then show gives exit 1 and stderr `overview <id> no longer exists`.
  - `TestOverviewShowPermanentListsAnchorLinks`: the existing permanent show output also gains `anchors:` with `gg://…@<sha>…/a.go:3`.
  - `TestHintBlindVerbsRefuseSessionOverviewLink`: `gg diff <link>`, `gg note add <link> …`, `gg link text <link>` and `gg overview export <link>` each give a non-zero exit with stderr containing `gg overview show`.
  - `TestLinkResolveSessionOverview`: `gg link resolve <link>` prints the checkout and `overview <id>`.
  - Domain `TestAnchorLink`: a working-tree range, a committed single line, and an `End == Start` case without a range.

- [ ] **Step 2: Run them to make sure they fail**
  Run: `go test ./internal/cli -run 'OverviewShowSession|OverviewShowPermanentListsAnchorLinks|HintBlindVerbsRefuse|LinkResolveSessionOverview' -count=1 && go test ./internal/domain -run TestAnchorLink -count=1`
  Expected: FAIL.

- [ ] **Step 3: Implement.**
  - `overviewShow`: for a `gg://` arg, resolve with `linkShapes{SessionOverview: true}`. On `res.SessionOverview()`, use `openDocsAt("overview show", res.Checkout, config.SessionSteerDir(cd, res.Checkout), …)`, then `checkedOverview`. A miss prints `overview <id> no longer exists`.
  - Text form: `<id> — <title> (session overview, <author>, <created>)`, then the link, a blank line, the text, then `anchors:` with one line per anchor. A file anchor prints `  <label> → <AnchorLink(LinkRepo{Abs: top}, "", a)>`. A note anchor prints `  <label> → note <id> <path>:<start[-end]>  <summary>` and an indented rationale.
  - Permanent: the same `anchors:` section from `agentdocs.ParseOverview(o.Doc.Overview)`, using `tsvc.LinkRepo` and `o.Commit`.
  - JSON: session `{"id","kind":"session","link","title","author","created","text","anchors":[{"label","path","start","end","link"}|{"label","note":{"id","path","start","end","summary","rationale"}}]}`. Permanent JSON gains the same `"anchors"`.
  - `overviewByArg` (export/rm) keeps `linkShapes{}`, so domain refuses a session link with the shared text. `gg link resolve`'s `domain.ResolveLink` call uses `linknav.WithSessionOverviews(linkResolveOpts(...))` and prints `overview <id>` for it. That line already exists.

- [ ] **Step 4: Run the tests to make sure they pass**
  Run: `go test ./internal/cli ./internal/domain -count=1`
  Expected: PASS.

- [ ] **Step 5: Commit** `feat(cli): gg overview show reads a session overview link from any checkout`

### Task 3: CLI navigate/open — TUI only

**Files:**
- Modify: `internal/cli/session.go` (navigate arm), `internal/cli/open.go`
- Test: `internal/cli/overview_link_nav_test.go`

**Interfaces:**
- Consumes: Task 1/2 (`linkShapes.SessionOverview`, `Resolved.SessionOverview`).
- Produces: a navigate `steer.Command{Cmd:"navigate", HintKind:"overview", HintID:id, Worktree: res.Checkout}` posted to the link checkout's inbox with `to = "tui"`.

- [ ] **Step 1: Write the failing tests**
  - `TestSessionNavigateSessionOverviewPostsToTUI`: fake TUI presence in the wt2 inbox (the pattern in `session_test.go`). `gg session navigate --no-wait <link>` posts a command with `HintKind=="overview"`, `HintID==id`, `Worktree==wt2` and no Commit.
  - `TestSessionNavigateSessionOverviewRefusesWebIDE`: `--to web` and `--to ide` give exit 2 with stderr `a session overview link opens only in the gg terminal for now`.
  - `TestOpenSessionOverviewWebRefused`: `gg open --web <link>` gives exit 2 with the same text.

- [ ] **Step 2: Run them to make sure they fail**
  Run: `go test ./internal/cli -run 'SessionOverviewPostsToTUI|SessionOverviewRefusesWebIDE|OpenSessionOverviewWeb' -count=1`
  Expected: FAIL.

- [ ] **Step 3: Implement.** Add `SessionOverview: true` to both `resolveLinkArg` calls. After the resolve, on `res.SessionOverview()`: refuse `--web` or `dir.to` web/ide (exit 2), set `c.Worktree = res.Checkout`, and force `to = "tui"` so a live web page alone is never steered.

- [ ] **Step 4: Run the tests to make sure they pass**
  Run: `go test ./internal/cli -count=1`
  Expected: PASS.

- [ ] **Step 5: Commit** `feat(cli): gg open / session navigate take a session overview link (TUI only)`

### Task 4: TUI copy actions

**Files:**
- Create: `internal/tui/overview_link.go`
- Modify: `internal/tui/sessions_popup.go` (`treeKey` `y` on `nodeOverview`; footer at the `[D] delete` hint line ~561)
- Modify: `internal/tui/commit_overview_rows.go:111-113`, `internal/tui/note_row_menu.go:62-64`, `internal/tui/overview_keys.go` (`overviewRows`, `splitOverviewRows`)
- Modify: `internal/tui/help.go`, `internal/i18n/*.toml` (ja/ko/zh/ru)
- Test: `internal/tui/overview_link_test.go`

**Interfaces:**
- Consumes: Task 1 (`domain.SessionOverviewLink`, `agentdocs.Store.AttachedWorktree`), existing `svc.OverviewLink` / `svc.ReviewLink`, `asyncCopyLinkRow`.
- Produces: `func (m Model) overviewLinkRow(d *openFile) actionRow`, which has id `overview-copy-link` and label `i18n.T("Copy gg link")`. It builds:
  - `srcOverview`: `wt := d.docs.AttachedWorktree(d.root)`. If `wt == ""`, return error `i18n.T("this overview is not saved to a worktree — no link")`. Else `domain.SessionOverviewLink(wt, d.id())`.
  - `srcCommitOverview`: `svc.OverviewLink(ctx, d.src.rev)`.
  - `srcReviewOverview`: `svc.ReviewLink(ctx, d.src.rev)`.

- [ ] **Step 1: Write the failing tests.** Use `recordingModel`, so the clipboard is a slice and link history is a temp dir. Session docs are attached with the existing fixtures in `agentdocs_fixtures_test.go`.
  - `TestCtrlBackslashYCopiesSessionOverviewLink`: open the ctrl+\ popup on the Overviews tab, cursor on the overview row, press `y`, drain cmds. The clipboard is `gg://<worktree>?overview=<id>` and link history has one entry with kind `overview`.
  - `TestCtrlBackslashYOnChildRowDoesNothing`: `y` on a child row means nothing is copied and nothing is killed.
  - `TestCommitOverviewsRowMenuHasCopyLink`: the row menu ids are `open-overview`, `overview-export`, `overview-copy-link`, `delete-overview` in that order. Running it copies the `OverviewLink`.
  - `TestOpenOverviewMenuHasCopyLink`: `overviewRows()` and `splitOverviewRows` both contain `overview-copy-link`.
  - `TestCopyLinkReviewOverviewCopiesReviewLink`: a `srcReviewOverview` doc copies `?review=<id>`.
  - `TestCopyLinkMemoryOverviewRefuses`: an unattached root means the clipboard is empty and the status contains `not saved to a worktree`.
  - The existing i18n/help/footer gates (`i18n_scan_test.go` etc.) must stay green.

- [ ] **Step 2: Run them to make sure they fail**
  Run: `go test ./internal/tui -run 'CopiesSessionOverviewLink|YOnChildRow|CopyLink' -count=1`
  Expected: FAIL.

- [ ] **Step 3: Implement.**
  - `treeKey`: `case "y"` when `n.kind == nodeOverview` runs `m.overviewLinkRow(n.doc).run(m)` and returns done=true. Other nodes return done=true and do nothing, so `y` never reaches the sessions-tab kill path.
  - Add the row after Export in both Overviews-row menus, and in both open-overview menus.
  - Footer: `[enter] open  [y] copy link  [→/←] expand/collapse  [x] close  [D] delete  …`. This is a new key, so translate it in all four bundles.
  - Help: add a row `r("y", i18n.T("ctrl+\\ Overviews & notes: copy the overview's gg:// link — paste it to another agent, which reads it with gg overview show <link>; also Copy gg link in an overview's . menu and on a commit's Overviews entry"))`.

- [ ] **Step 4: Run the tests to make sure they pass**
  Run: `go test ./internal/tui -count=1`
  Expected: PASS.

- [ ] **Step 5: Commit** `feat(tui): copy an overview's gg link (ctrl+\ y, Overviews and overview . menus)`

### Task 5: TUI navigate opens a session overview

**Files:**
- Modify: `internal/tui/steer_nav.go` (`steerNavigate` overview arm)
- Test: `internal/tui/overview_hint_test.go`

**Interfaces:**
- Consumes: `steerFileFocus` (it reloads and syncs the store, then `bringToFront`).

- [ ] **Step 1: Write the failing tests**
  - `TestSteerNavigateOpensSessionOverview`: an attached session overview in the current worktree, then `steer.Command{Cmd:"navigate", HintKind: model.OverviewHintKind, HintID: id}` (no Commit). The focused doc's `id() == id` and the steer reply is OK.
  - `TestSteerNavigateSessionOverviewClosedReopens`: close it with `closeDoc` first. It still opens, because `syncOverviews` re-registers it.
  - `TestSteerNavigateSessionOverviewGone`: an unknown id gives a failed reply containing `no longer exists`.
  - `TestGotoLinkSessionOverviewSameCheckout`: the `#` prompt with the link, through `resolveLinkCmd` and then `resolvedGotoLink`, focuses the overview. Use the existing goto_link test pattern.

- [ ] **Step 2: Run them to make sure they fail**
  Run: `go test ./internal/tui -run 'SessionOverview' -count=1`
  Expected: FAIL.

- [ ] **Step 3: Implement.** In `steerNavigate`, put a new arm BEFORE the existing overview arm: `case c.HintKind == model.OverviewHintKind && c.Commit == "" && c.Target == nil:`. It copies c with `Cmd: "file_focus"` and `FileID: c.HintID`, calls `steerFileFocus`, and rewrites a `no open file` failure to `overview <id> no longer exists`. Another worktree or repository needs nothing new: `#` confirms the switch and `AtLink` carries the hint.

- [ ] **Step 4: Run the tests to make sure they pass**
  Run: `go test ./internal/tui -count=1`
  Expected: PASS.

- [ ] **Step 5: Commit** `feat(tui): a session overview link opens the overview`

### Task 6: Docs, skills, gate

**Files:**
- Modify: `internal/agentskill/using-gg.md` (next to the review/note link lines ~361-369), `internal/agentskill/gg-overview.md`, `internal/agentskill/agentskill.go` (`Version` 181 → 182; bump gg-overview's own marker if it has one)
- Modify: `CHANGELOG.md`, `README.md` (if it lists overview CLI verbs), `docs/CLAUDE-details.md` (the link-hint section: the address-less local `?overview=` form + the `SessionDocs` seam)

- [ ] **Step 1:** using-gg: "An overview link (`gg://…?overview=<id>`), pasted by the user or another agent, holds another agent's analysis. Read it with `gg overview show <link>`, then each anchor with `gg link text <anchor link>`." gg-overview: "Your overview's link (ctrl+\ `y`) can be handed to another agent."
- [ ] **Step 2:** CHANGELOG entry: "Overview links". It covers what is copied where, `gg overview show <link>` for both kinds, the anchors list, and the TUI-only navigate. It names gg web as the follow-up.
- [ ] **Step 3:** Run `go test ./internal/agentskill -count=1` (version/golden guards), then `./build.sh install && gg init --update`.
- [ ] **Step 4:** Run the full gate. `./test.sh race > /tmp/claude-1000/-work-gigagit/b096b659-fbe3-466f-bdcc-00425577c928/scratchpad/race.log 2>&1`, then read the verdict: green only when the log says "all green".
- [ ] **Step 5: Commit** `docs: overview links (using-gg v182, CHANGELOG)`
- [ ] **Step 6:** Run a read-only whole-branch review subagent on the most capable model. Fix what it confirms, re-run the gate, merge `main` into the branch if it moved, then ask the user to merge.

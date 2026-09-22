# Web ↔ TUI parity

What the browser frontend (`gg web`) can do compared with the TUI, and what it
still cannot. Snapshot taken **2026-08-15** against `web-dev` `e7263ab0` — two
waves of commit-menu work and both bookmark/shelf stages are merged there,
ahead of `main`.

**How this was derived** (repeat it to refresh the list rather than trusting the
prose): the `engine.X{}` operations each frontend constructs are the ground
truth for *what can be run*.

```bash
grep -rhoE "engine\.[A-Z][A-Za-z]+\{" internal/tui/*.go | grep -v _test | sort -u
grep -rhoE "engine\.[A-Z][A-Za-z]+\{" internal/web/*.go | grep -v _test | sort -u
```

Everything else — bookmarks, the shelf, the notification centre, searching —
is not an engine op, so those are listed separately below.

## Shared surface

Pull · push · force-push · smart switch · merge · rebase · fast-forward ·
interactive rebase · branch create/rename/delete · remote checkout (stay or
switch) · browse & checkout unfetched remote branches (per-branch fetch
mapping) · prune · delete remote branch · worktree create + remove · tags
(create, annotate, push, delete, delete from remote) · stash create/apply/pop/
drop · commit · staging incl. the region/line hunk picker · discard ·
`.gitignore` rows · reset (incl. reset-to-remote-tip) · conflict resolution
through the hunk picker · AI conflict-complete · diff (incl. the `f` changed-lines-only fold) · file history · blame ·
compare two branches · solo scope · branch versions · AI review · identity &
profiles · branch prefixes · external tools · session errors · repo switcher ·
refresh settings · live refresh (file watch + intervals) · commit graph · command palette · cherry-pick · revert ·
reword · undo last commit · worktree from a commit · fast-forward to a commit ·
reset to a commit · annotated tags from a commit · review one commit (AI) ·
**bookmarks and the shelf** (list, add a file or commit, name, remove, open,
restore to a path, restore one file from a shelved commit, cherry-pick a
shelved commit — live or from its frozen patch) · **merge previews** (saved
`source → target` branch-name pairs, previewed as the merge-base..source
diff rather than a tip-to-tip compare: list, add, rename, remove, save the
reversed pair, open, and live re-open when a tracked tip moves) · **commit
pairs and saved comparisons** in the same Previews section (list, open, rename,
remove, save reversed, copy gg link — either half of a comparison; a commit
pair carries its **review notes**: the ◆N total on its row, badges, note rows,
`c`/`E`/`R` on the new side, the line copy row as a `@a..b` link; its files'
right-click menu has history / blame / bookmark / shelf at `b`) ·
**compare with link…** (two `gg://` links through `domain.CompareLinks`: the
copied-link history, side swap, per-side errors, the base row, save
comparison…; a `@a..b` link lands through the same door; **web only:** the
⇄ symmetric view of two sets — two row-aligned file lists around one diff, a
flippable direction arrow, differ / all / one side / same filters; and drag a
Previews row onto another to start the comparison — the TUI's twin is space /
`m` marks on two Previews rows; and the **stacked diff** (`S`) — every file
of a change set in one scroll, header + diff per file, the symmetric
comparison included — which the TUI now has too, with its own keys: `n`/`p`
step file to file where the web uses `j`/`k`, home/end are the whole stack's
ends, `J` lists the files, and `-`/`_` fold, as on the web) · **pull
requests** (read-only: list, fetch + open the PR diff, copy URL, forget,
`[refresh] prs` poll and ⟳, review threads inside the diff as read-only note
boxes, the details overlay — the TUI's PR hub — the comment re-poll, and the
any-state **search** — the TUI's `A` — as a field + state chip in the section;
PR text is rendered as markdown in both — the web paints the parsed tree
as HTML, the TUI lays it out in rows) ·
**note folding** (every note box: click the title, `z` / `Z` — the TUI's
`o` / `O`).

## Missing from the web

### Operations the TUI wires and the web does not

| Engine op | What it gives the user |
|---|---|
| `ApplyPatch` | import a `.patch` / `format-patch` mailbox |
| `ExportFile`, `ExportToDir` | export a commit (or one file's diff) as a patch; also the `t` copy-to-temp-dir lane |
| `MoveWorktree` | rename or relocate a worktree (the web can only create and remove) |
| `RemoveGitLocks` | clean up stranded `*.lock` files (`gg unlock`) |
| `AddFetchMappings` | repair a clone's fetch refspec after a push that can't be tracked |
| `PushTags` | the branch-tip tag prompt when pushing |
| `GenerateMessage` | AI-written commit message from the staged diff (AI *review* is wired; this is not) |
| `CreateWorktree` | the keep-changes modes (worktree *from a commit* now works) |
| `MarkAllResolved` | resolve every conflicted file at once |
| `ResolveConflict` | whole-file keep-current / keep-incoming (the web has only the hunk picker) |

Commit amend is TUI-only too (the web's commit op has no amend lane).

### Subsystems absent from the web entirely

- **Notification centre (`!`)** and the related-option prompts that feed it.
- **Git config explorer** — the web writes git config only for identity.
- **Agent-skill setup** (`gg init` / Settings → Set up agent skills).

### Weaker in the web (present, but not equivalent)

- **Live refresh.** A ref-family change (branches, remotes, worktrees, tags,
  reflog) reloads the whole sidebar in one go rather than the one list; the
  notification center still does not auto-refresh.
- **Bookmarks and the shelf.** Both stores are in the sidebar now and can be
  filled, opened, restored and (for a shelved commit) cherry-picked. What is
  still missing is **comparing against** an entry — `CompareFiles` /
  `ComparePatch` already take a `model.Endpoint` that can name a shelf entry;
  the compare view only ever passes commits — and the `t` copy-to-temp-dir
  lane.

- **Searching commits.** The web's `/` filters the rows already loaded, client
  side (subject, author, sha prefix). The TUI adds `ctrl+f` eager paging into
  unloaded history, the `\` server-side feed filter (path / author / message /
  date range), and the `F` fuzzy file finder.
- **Searching text in a view.** Both have the in-view search (`/`, `@`
  backward, `]`/`[` step with wrap, `esc` clears) in the diff view and in
  blame, with the same rules (case-insensitive, visible lines only, a same
  row counted once). The TUI also searches the View-file preview and the
  hunk picker; the web's file-history overlay and conflict picker have no
  search.
- **Stash create.** The web stashes the whole tree with a message; the TUI
  popup offers a per-file checklist.
- **Commit context menu.** Mostly closed. What the TUI still has that the web
  does not: **squash marked commits**, **export patch**, **solo from this
  commit**, and the compare-selection rows (mark two commits, compare the
  range).
- **Solo scope.** The server stores solo as a *branch name*, so
  solo-from-a-commit (the TUI's `ctrl+g` on the Commits panel) has no wire
  shape yet. Territory dot colors in a soloed feed are at parity (per-row
  `seg` on `/api/commits`); the TUI's multi-branch "Add to commit view" scope
  has no web equivalent, so the web colors only the single-ref solo.

## Neither frontend

Sparse-checkout (roadmapped alongside the remaining MCP surface).

## Working on this

The remaining gap is split into seven task files under
`docs/superpowers/plans/` — see `README-web-parallel-tasks.md`. Task 0 adds the
registries that let the other six run in parallel without sharing a file; tasks
1–6 each own an area outright. **Do not edit this document from a task
branch** — report what you closed and it is updated after the merge, so the
parity snapshot never appears in a merge conflict.

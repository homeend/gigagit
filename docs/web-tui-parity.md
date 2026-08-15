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
switch) · prune · delete remote branch · worktree create + remove · tags
(create, annotate, push, delete, delete from remote) · stash create/apply/pop/
drop · commit · staging incl. the region/line hunk picker · discard ·
`.gitignore` rows · reset (incl. reset-to-remote-tip) · conflict resolution
through the hunk picker · AI conflict-complete · diff · file history · blame ·
compare two branches · solo scope · branch versions · AI review · identity &
profiles · branch prefixes · external tools · session errors · repo switcher ·
refresh settings · commit graph · command palette · cherry-pick · revert ·
reword · undo last commit · worktree from a commit · fast-forward to a commit ·
reset to a commit · annotated tags from a commit · review one commit (AI) ·
**bookmarks and the shelf** (list, add a file or commit, name, remove, open,
restore to a path, restore one file from a shelved commit, cherry-pick a
shelved commit — live or from its frozen patch).

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
- **Stash create.** The web stashes the whole tree with a message; the TUI
  popup offers a per-file checklist.
- **Commit context menu.** Mostly closed. What the TUI still has that the web
  does not: **squash marked commits**, **export patch**, **solo from this
  commit**, and the compare-selection rows (mark two commits, compare the
  range).
- **Solo scope.** The server stores solo as a *branch name*, so
  solo-from-a-commit (the TUI's `ctrl+g` on the Commits panel) has no wire
  shape yet.

## Neither frontend

Sparse-checkout (roadmapped alongside the remaining MCP surface).

## Working on this

The remaining gap is split into seven task files under
`docs/superpowers/plans/` — see `README-web-parallel-tasks.md`. Task 0 adds the
registries that let the other six run in parallel without sharing a file; tasks
1–6 each own an area outright. **Do not edit this document from a task
branch** — report what you closed and it is updated after the merge, so the
parity snapshot never appears in a merge conflict.

# Web ↔ TUI parity

What the browser frontend (`gg web`) can do compared with the TUI, and what it
still cannot. Snapshot taken **2026-08-15** against `main` `1dad9dcb`.

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
refresh settings · commit graph · command palette.

## Missing from the web

### Operations the TUI wires and the web does not

| Engine op | What it gives the user |
|---|---|
| `CherryPick` | cherry-pick a commit onto the current branch |
| `Revert` | revert a commit |
| `Reword` | rename/reword an existing commit's message |
| `UndoLastCommit` | `u` — ref-only soft reset of the last commit |
| `ApplyPatch` | import a `.patch` / `format-patch` mailbox |
| `ExportFile`, `ExportToDir` | export a commit (or one file's diff) as a patch; also the `t` copy-to-temp-dir lane |
| `MoveWorktree` | rename or relocate a worktree (the web can only create and remove) |
| `RemoveGitLocks` | clean up stranded `*.lock` files (`gg unlock`) |
| `AddFetchMappings` | repair a clone's fetch refspec after a push that can't be tracked |
| `PushTags` | the branch-tip tag prompt when pushing |
| `GenerateMessage` | AI-written commit message from the staged diff (AI *review* is wired; this is not) |
| `CreateWorktree` | worktree from a commit, and the keep-changes modes |
| `MarkAllResolved` | resolve every conflicted file at once |
| `ResolveConflict` | whole-file keep-current / keep-incoming (the web has only the hunk picker) |
| `WriteFile` | write content into the worktree |

Commit amend is TUI-only too (the web's commit op has no amend lane).

### Subsystems absent from the web entirely

- **Bookmarks (`g`)** — live, richly-addressed references to files and commits:
  compare-against, paste to a path, cherry-pick a bookmarked commit.
- **The shelf (`G`)** — the non-git frozen store: shelve a file or a whole
  commit, browse a shelved commit's files, restore, cherry-pick from the
  frozen patch after a `gc`, export to a temp dir.
  Together these are the single largest gap; `internal/web` has zero
  references to either.
- **Notification centre (`!`)** and the related-option prompts that feed it.
- **Git config explorer** — the web writes git config only for identity.
- **Agent-skill setup** (`gg init` / Settings → Set up agent skills).

### Weaker in the web (present, but not equivalent)

- **Searching commits.** The web's `/` filters the rows already loaded, client
  side (subject, author, sha prefix). The TUI adds `ctrl+f` eager paging into
  unloaded history, the `\` server-side feed filter (path / author / message /
  date range), and the `F` fuzzy file finder.
- **Stash create.** The web stashes the whole tree with a message; the TUI
  popup offers a per-file checklist.
- **Commit context menu.** The web offers show / copy id / copy subject /
  create tag / move up / move down / drop. The TUI adds reword, create branch
  here, create worktree here, fast-forward to here, reset to here,
  cherry-pick, revert, squash marked, export patch, review this commit,
  bookmark, shelf, solo from this commit, and the compare-selection rows.
- **Solo scope.** The server stores solo as a *branch name*, so
  solo-from-a-commit (the TUI's `ctrl+g` on the Commits panel) has no wire
  shape yet.

## Neither frontend

Sparse-checkout (roadmapped alongside the remaining MCP surface).

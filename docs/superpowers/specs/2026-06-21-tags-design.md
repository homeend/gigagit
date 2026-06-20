# Tags support — design

## Goal

Bring git **tags** into `gg` as a first-class surface: view tags, create them
(lightweight or annotated), delete them, check them out, and push them — from
the TUI and the CLI. Delivered in **stages, read-first**, mirroring the
Remotes-tab arc. Each stage is its own feature branch through the full
brainstorm→spec→plan→execute→merge cycle.

## Placement & navigation (TUI)

Tags live in the **middle-left window** (today the **Files** box) as a **tab**,
using the *same* tab-slot mechanism as the top-left Branches/Remotes/Worktrees
slot.

Today there is one tab slot, hard-wired:

- `leftTabs = []panel{panelBranches, panelRemotes, panelWorktrees}` — the
  display/cycle order.
- `m.activeLeftTab` — which member shows.
- `tabBarLabel(active)` — renders `[Branches] R W`.
- `ctrl+←/→` — cycles `leftTabs`.

This is generalized to **two independent slots**:

| Slot | Members | Active field | Label fn | Cycle |
|------|---------|--------------|----------|-------|
| Top (refs) | Branches, Remotes, Worktrees | `activeLeftTab` | `tabBarLabel` | `ctrl+←/→` while a top-slot tab is focused |
| Middle (files) | **Files, Tags** | `activeFilesTab` (new) | new `filesTabLabel` | `ctrl+←/→` while the middle box is focused |

```
┌────────────────────────┐
│ [Branches] R  W        │  top slot — unchanged
│  main                  │
├────────────────────────┤
│ [Files 3]  Tags        │  middle slot — NEW: Files ⇄ Tags
│  M model.go            │
│  A CHANGELOG.md        │
├────────────────────────┤
│ Staged 1               │  Staged stays its own always-on box
└────────────────────────┘
```

Decisions:

- **Files ⇄ Tags only** in the middle slot. **Staged stays a separate,
  always-on box** so the stage/unstage workflow keeps Files and Staged both
  visible. (Folding Staged into the slot was considered and rejected for this
  reason.)
- `ctrl+←/→` is **focus-aware**: it cycles whichever slot currently owns focus
  (top slot vs. middle box). This is the one behavioral change to the existing
  shortcut — today it always cycles the top slot. The narrow-terminal single
  column path is unaffected (it shows only Commits).
- New `panelTags` panel id, registered with the panel infrastructure
  (`panelView`/`panelLen`/`sel`/`sortModes`/filtering) like the other list
  panels.

## Data model

Reuse the existing ref vocabulary where possible:

- `model.RefTag` already exists and commit decorations already parse `tag: v1`
  from `%D` (`internal/git/log.go`). **No change** to commit decoration.
- New `model.Tag` plain type for the Tags list:

  ```go
  type Tag struct {
      Name       string // "v2.1.0"
      Target     string // commit hash the tag resolves to (peeled for annotated)
      Annotated  bool   // annotated (has its own object) vs lightweight
      Subject    string // annotated: tag message subject; lightweight: target commit subject
  }
  ```

## Stage 1 — Read (first feature branch)

### Git verb (`internal/git/tag.go`)

One invocation, `gitcmd`-built:

- `Tags(ctx) ([]model.Tag, error)` →
  `git tag --list --sort=-creatordate --format=<fmt>` where `<fmt>` emits, per
  tag, the name, `%(objecttype)` (→ `Annotated = objecttype=="tag"`),
  `%(*objectname)`/`%(objectname)` (peeled target for annotated, direct for
  lightweight), and `%(contents:subject)`/`%(*contents:subject)` for the
  message/subject. Parsed with a NUL/tab field split (real-git tested).

### Domain query (`internal/domain`)

- `Tags(ctx) ([]model.Tag, error)` — a Read-reservation query (parallel +
  singleflight-coalesced, like `Worktrees`/`Status`). Loaded into the model on
  startup `Snapshot` and refreshed when the Tags tab is shown / after a
  tag-mutating op.

### TUI surface

- `panelTags` panel; `m.tags []model.Tag`; rows rendered as
  `<kind> <name>  <short-target>  <subject>` where `<kind>` is `●` (annotated)
  / `○` (lightweight). Lane-style single-source row string (filter haystack =
  name + target + subject).
- Middle slot tab bar `[Files] Tags`; `ctrl+←/→` (focus-aware) switches.
- `/`-filter and sort modes via the shared panel machinery.
- **Enter on a tag** jumps to the tag's target commit in the Commits panel,
  reusing the goto-tip helper (`commitHasLocalRef` analog for tags / direct
  hash match): find the loaded commit with that hash, set `sel[panelCommits]` +
  `focus=panelCommits`; `statusMsg` notice when the target isn't loaded.
- Read-only: no `.`-menu mutating actions yet (Stage 1 ships a usable viewer).

### CLI

- `gg tag ls` (alias `gg tag list`) — prints the tag list (name, kind, short
  target, subject), mirroring `gg remote ls`. e2e `stdout_contains`.

### Tests

- Real-git `git/tag_test.go`: lightweight + annotated tags, target peeling,
  sort order.
- `domain` query test.
- TUI: tab switch renders the Tags panel; row format; enter-jumps-to-commit;
  `/`-filter narrows.
- e2e scenario: repo with tags → `gg tag ls` asserts names/kinds.

## Stage 2 — Create (second feature branch)

- `engine.CreateTag{Name, Commit, Message}` — `Message == ""` → lightweight
  (`git tag <name> <commit>`); else annotated (`git tag -a -m <msg> <name>
  <commit>`). Emits `Done`; no Decider (a name clash is a surfaced error).
  `LockMode` RefWrite.
- Git verb `CreateTag(ctx, name, commit, message string)`.
- TUI: Commits-panel `.`-menu `commitCreateTagRow` → tag popup (name field;
  optional message field → annotated). Reuses the commit-row hook
  (`focus==panelCommits` + `backingIndex`) and the text-popup pattern.
- CLI: `gg tag create <name> [<commit>] [-m <msg>]` (defaults to HEAD).
- e2e: create lightweight + annotated; assert presence/kind.

## Stage 3 — Delete (third feature branch)

- `engine.DeleteTag{Name}` → `git tag -d <name>`. RefWrite.
- TUI: Tags-tab `.`-menu **Delete tag** → **confirm modal** (the existing
  decision modal; destructive, never-trap: Cancel option). On success refresh
  the list.
- CLI: `gg tag rm <name>` (alias `delete`).
- e2e: delete removes the tag.

## Stage 4 — Checkout + Push (fourth feature branch)

### Checkout (Decider fork — "ask each time")

- `engine.CheckoutTag{Name}` resolves the tag, then **forks via a Decider**
  (`tag-checkout`): option list = `detached` (check out the tag's commit,
  detached HEAD) / `branch` (create a new branch at the tag, then switch). The
  branch path reuses the create-branch infra; the switch reuses SmartSwitch.
  TUI answers from the modal; CLI from a flag (`--as=detached|branch
  [--branch <name>]`). Dirty-tree handling matches SmartSwitch.
- TUI: Tags-tab `.`-menu **Check out tag**.
- CLI: `gg tag checkout <name> [--as=… --branch=…]`.

### Push

- `engine.PushTag{Remote, Name}` → `git push <remote> <tag>`. RefWrite.
  Remote selection: if exactly one remote, use it; otherwise a remote picker
  (reuse the Remotes infra / a pair-op style picker).
- TUI: Tags-tab `.`-menu **Push tag** (→ remote picker when >1 remote).
- CLI: `gg tag push <name> [<remote>]`.
- e2e: push to the scenario git server; assert the tag exists on the remote.

## Cross-cutting

- Each stage bumps `agentskill.Version` and runs `gg init --update` to refresh
  the dogfood `SKILL.md` (the `TestDogfoodSkillCopyInSync` gate fails the suite
  otherwise) — only the stages that change the CLI surface (all but the pure
  layout generalization, which Stage 1 carries anyway).
- Docs per stage: `CHANGELOG.md` always; `README.md` when the user-facing
  surface changes; `CLAUDE.md` package map only if a new package appears (none
  expected — tags reuse `git`/`domain`/`engine`/`tui`/`cli`).
- `internal/tui` and `internal/cli` never import `internal/git` (archtest);
  tags go through `domain`.

## Out of scope (v1)

- Tag ranges / bulk operations.
- Signing (`-s`) and verification.
- Editing/moving an existing tag (delete + recreate instead).
- Folding Staged into the middle tab slot.
- Pushing/deleting tags on the remote in one sweep (`--tags`); per-tag only.

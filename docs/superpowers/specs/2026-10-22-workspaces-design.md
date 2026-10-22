# Workspaces — Design Spec

**Date:** 2026-10-22
**Status:** Draft (awaiting approval)
**Scope:** Several repositories open in one gg TUI at once, as one named
*workspace*, like a VS Code multi-root workspace. The follow-up the repo
switcher spec (2026-06-12) named. Group sync stays out of scope (§Non-goals).

---

## Intent

The user works on systems split over several repositories (a monorepo plus
satellites, or a few services). Today gg shows one repository; moving to
another is the `R` Repositories popup and a full re-root, which drops every
open window, console and walk ring. The user wants the repositories of one
system to be *open together*: their worktrees, branches and agent sessions in
the same lists, the worktree walks (alt+w, alt+a, alt+q) running across all of
them, every repository watched the whole time, and one keystroke moving the
whole interface from a worktree of repository A to a worktree of repository B
exactly as it moves between two worktrees of one repository today.

## Goals

- A **workspace** is a named, ordered set of 1–5 repositories, defined by the
  user in a **Workspaces** settings popup opened from the command palette.
- Opening gg in a repository that belongs to one or more workspaces asks which
  to open: one of those workspaces, or the repository alone.
- In a workspace, the **Worktrees** and **Branches** tabs list every member
  repository's rows under a top-level heading per repository (its directory
  name), in workspace order.
- **alt+w, alt+a / alt+A and alt+q / alt+Q work over the whole workspace**:
  the ring they walk is the union of all member repositories' worktrees (and
  their sessions), in the order the Branches tab shows them.
- Every member repository is **loaded and watched** for the lifetime of the
  workspace; results route to the repository and worktree they belong to,
  whether or not it is on screen.
- One repository is **on screen at a time**; switching between repositories
  inside the workspace is a slot swap (windows, consoles and walk state kept),
  never a re-root.

## Non-goals

- Several repositories visible **side by side** on one screen.
- **Group sync** / any operation that runs in several repositories at once;
  gg stays one-operation-at-a-time per TUI.
- A committed workspace file (`.code-workspace` style). Workspaces are
  machine-local state (§Storage). This can follow if asked.
- A `gg workspace` CLI, MCP exposure, and standalone `gg web` workspaces. The
  hosted web page follows the terminal's repository as it does today.
- Cross-repository compare, cherry-pick or any op taking rows from two
  member repositories at once.

## Anti-goals (a failure even if it works)

- A workspace that quietly re-roots: any switch inside the workspace that
  loses an open window, a parked walk, a console, or a half-typed box.
- A launch dialog the user cannot leave with esc into a working single
  repository.
- Member repositories whose lists go stale because only the viewed one is
  watched.
- Scriptable verbs (`gg status`, `gg mcp`, `gg web`, `gg session`, …) changing
  behaviour because a workspace exists.

---

## Terms

- **Member**: a repository in a workspace, identified by its **main checkout
  path** (the parent of its common git dir; the bare directory for a bare main
  repository). Opening gg in *any* worktree of a member counts as opening that
  member.
- **Home**: the worktree gg was launched in, as today. The home repository is
  the member it belongs to.
- **Viewed repository**: the member whose worktree is on screen.
- **Repository group**: a member's rows in the Worktrees or Branches tab,
  under its heading.

---

## 1. Storage

`workspaces.toml` beside `repos.toml` under the state dir
(`$XDG_STATE_HOME/gg/`, `%LocalAppData%\gg\`, `~/.local/state/gg/`); written
temp + rename under the shared file lock (`filelock`), corrupt file → empty
list, as `repos` does. A new DAG-leaf package `internal/workspace`:

```toml
format = 1

[[workspace]]
name  = "shop"
repos = ["/work/shop-api", "/work/shop-web", "/work/shop-infra"]  # ≤ 5, ordered
settings_repo = "/work/shop-web"  # optional: whose theme/process-wide config the workspace uses (§6)
```

- `name` is unique (case-insensitive), 1–40 runes, no `/`, `?` or control
  characters; `repos` are absolute main-checkout paths, compared by
  `model.KeyOf`. A path may appear in several workspaces. A workspace may
  hold one repository (a named way back to it is harmless).
- A member path that no longer exists is kept on disk (a drive may be
  unmounted) and shown greyed with "(missing)" in the settings popup; it is
  skipped at launch and never pruned silently.
- The package exposes `Load`, `Save`, `Containing(key)` (the workspaces a
  checkout key belongs to), `Name` validation and the cap `MaxRepos = 5`.
  Owned by `domain` (`domain.Workspaces()` returns the loaded list, a save
  verb beside it); frontends never import the package. The state path is
  injectable (a field on `tui.Headless` and the domain accessor) so tests
  never touch user state.

## 2. The Workspaces settings popup (command palette)

Palette entry **"Workspaces…"** in the settings group. A popup (embeds
`popupMax`, ctrl+t maximises) with two levels:

**Workspace list.** One row per workspace: name, member count, and the member
directory names in order. Rows for workspaces the viewed repository belongs
to carry a ● marker. Keys (all in the footer and `?`):

- `n` new workspace (text box for the name; validation errors shown inline,
  the box stays open until valid or esc).
- `enter` open the workspace's member list (below).
- `r` rename, `d` delete (one confirm: "Delete workspace <name>? Its
  repositories stay on disk."), `o` **open this workspace now** (§4), esc
  closes.

**Member list.** One row per member: directory name, full path (middle-cut),
"(missing)" when absent. Keys:

- `a` add a repository: a picker with three sources, top to bottom — the
  repositories **open in this gg right now** (the viewed one, and the members
  of the open workspace), the Repositories registry's **recent** entries (the
  `R` list, MRU order, same fuzzy filter), and a last row **"Path…"** that
  opens the existing repository path popup (`repo_path_popup.go`) for a
  typed path. Whatever is picked is resolved to its main checkout path before
  it is stored. Refused with a status line when the workspace already has 5
  members or the path is already a member; a typed path that is not a git
  repository is refused the way `R`'s typed path is.
- `d` remove the member (no confirm; the repository is untouched), `K`/`J`
  move it up/down (this is the walk order), `s` make this member the
  **settings repository** (§6; a ✎ marker shows which one it is; `s` on it
  again clears the choice back to the default), esc back to the workspace
  list.

Every edit saves at once. The popup is available in a single repository and
inside a workspace alike; edits to the **open** workspace take effect live:
an added member loads (§5), a removed member's slots are dropped (its
consoles are asked about first, see §6), a reorder re-sorts the groups.

## 3. Launch

Only the TUI launch path (`tui.Run`) asks. The first snapshot resolves the
common dir; once it has (the same point `applyRepoHealth` runs), gg looks up
the workspaces containing the home repository:

- **none** → nothing changes; gg is the single-repository gg of today.
- **one or more** → a popup over the loaded single-repository screen: one
  row per workspace, "Open workspace **shop** — shop-api, shop-web,
  shop-infra", then a last row "Open **shop-api** alone". Enter picks; esc is
  "alone". The dialog never blocks the first load: the home repository is
  already on screen behind it, so esc costs nothing.
- Picking a workspace loads the other members (§5) and the status line says
  "workspace shop: 3 repositories".

`gg --workspace <name>` opens that workspace without the dialog (the home
repository must be a member, else the flag is refused with a message and gg
exits non-zero — nothing opens half-way); it exists for tmux/scripts and the
e2e harness. No other CLI surface.

Headless e2e (`tui.Headless`) takes the workspaces state path like the
repositories one, so a scenario can pre-write `workspaces.toml`.

## 4. Entering, switching and leaving a workspace at run time

- The Workspaces popup's `o` opens a workspace from inside a running gg. If
  the viewed repository is a member, it becomes the home repository's slot
  and nothing on screen is lost; if not, the switch is a re-root into the
  workspace's first member (today's repository-switch rules: refused while an
  operation runs; the user is told).
- Opening a *different* workspace while one is open: the members in both stay
  as they are; the rest are dropped (§6) and the new ones load.
- The palette gains **"Leave workspace"** (visible only inside one): keeps
  the viewed repository on screen as a single repository, drops the other
  members' slots (§6).
- The `R` popup switches between **workspaces and repositories**. Above the
  repository list it gains a **Workspaces** group: one row per defined
  workspace (name, member directory names), the open one marked ●; the fuzzy
  filter matches workspace names and member names too. Enter on a workspace
  row opens it (the rules two bullets up); enter on the open one is a no-op.
  Below, the repository list is today's MRU list; inside a workspace the
  member repositories carry the ● marker. Picking a member switches to it
  inside the workspace (its last-viewed worktree, else its main worktree).
  Picking a non-member re-roots as today **and leaves the workspace** — the
  status line says so ("left workspace shop"). The group is absent when no
  workspace is defined, so `R` looks as today for everyone else.
- **Every other way into a repository obeys the same rule**: ctrl+\ opening
  a session that runs in a member, a `gg://` link, `gg session navigate`, the
  web page's own switch. A target inside a member is a member switch; only a
  target outside every member re-roots and leaves the workspace. No path
  leaves the workspace by accident.

### Look versus adopt

Today there are two kinds of worktree switch and the workspace keeps both,
unchanged:

- a **look** (alt+w, alt+a, alt+q) shows another worktree; gg's own worktree
  (home) stays where it is, the ring comes back to it;
- an **adopt** (enter on a worktree row, `R`, a link, ctrl+\) moves home
  there: the snapshot file, the steer inbox, the Repositories recent list and
  the hosted web page follow, exactly through today's `adoptView` path.

In a workspace both kinds may cross a member boundary. An adopt into another
member moves home into that member: the home repository is simply "the member
home is in" at any moment, and whatever is tied to home (the published
identity, the snapshot, the web page) moves with it — the same thing `R` does
today, minus the re-root and minus the config re-read (§6: the process-wide
config and the theme belong to the workspace's settings repository, not to
home). A look never moves any of it. The heading click (§5) is an adopt.

## 5. What is on screen

One repository is viewed at a time. **Commits, Stashes, Tags, Files, Staged,
the header line, the status line, notices and the commit graph show the
viewed repository's viewed worktree**, exactly as today.

**Worktrees tab.** Groups in workspace order. A group is a top-level heading
row — the member's directory name, bold, not selectable, no gutter marks —
followed by that member's worktree rows and their session/◆ sub-rows exactly
as the tab renders them today. The viewed repository's group is where the
cursor is after a switch; moving the cursor into another group does **not**
switch the view. Enter / click on a worktree row of another group switches
to it (the slot swap of §6). A click on a heading (it takes no cursor, so
no enter) switches to that member's last-viewed worktree, else its main
worktree. When two members share
a directory name the heading adds the parent directory ("api (shop)", "api
(billing)"); this closes the "same-name repos not disambiguated" open item
for this tab.

**Branches tab.** The same grouping: a heading per member, then that
member's branch list *exactly as today* — its own sort, its own active
branch-filter slot, its own session/review/overview sub-rows. Moving the
cursor into another group does not switch the view. **Any action on a row of
another member first switches the view to that member** (to the worktree the
branch is checked out in, else the member's last-viewed worktree) and then
runs there — the rule today's consoles already follow ("the panels show THAT
worktree and what you start runs there"). A compare (`f`) between rows of two
members is refused with a status line.

The **header line** names the viewed worktree's path as today; when it
belongs to another member than home, nothing extra is added — the user chose
the worktree path over a repository name.

**The footer** keeps today's hints; in a workspace the `[alt+w] next
worktree` hint is shown whenever the workspace has more than one worktree in
total.

## 6. State: repository slots over worktree slots

Today the Model keeps per-worktree slots (`worktreeView`) under one
repository; repository-wide state (branches, commits, stashes, tags, remotes,
worktree list, forge detection, repo health, config, branch filters, the
Repositories registry entry, the observability ring) is Model-global and a
switch to another repository is a `reRoot`.

A workspace adds a **repository slot** (`repoView`) per member: the
repository-wide state above, its `domain.Service` rooted at the main checkout
(`OpenTUISharing` — one process-wide git limiter, one observability ring),
its repository-level **watcher** (`gitwatch` over refs, worktrees, reflog,
remotes; polling where unsupported), its background-refresh lane entries,
and its map of worktree slots. The Model's own fields stay the LIVE copy, as
with worktree slots: a switch between members copies out the leaving
repository's state and in the arriving one's, then the existing worktree
slot swap lands on the target worktree. Nothing is reloaded; results carry
the repository (its service pointer, as handlers already check `msg.svc`)
and land in the right slot; results for a sleeping worktree keep queuing as
today.

**Watching.** Every member's repository-level watcher runs for the lifetime
of the workspace (this is what keeps the other groups' branch and worktree
rows current). Working-tree **status** of a sleeping worktree is refreshed on
switch, as today — the panels cannot show it while asleep, and five
repositories on WSL2 9p would otherwise poll five trees for nothing. The
background auto-refresh lane runs the sleeping members' *repository* sources
on their normal cadence under the one `LimitRunner`. A member whose watcher
cannot start falls back to polling for that member only.

**Operations.** One at a time per TUI, unchanged. Today every worktree
switch — enter, alt+w, alt+q — is **refused while an operation runs** (the
operation's slot stays on screen; `switchRefusalBy`); a member switch is
refused the same way, with the same message. So an operation always runs in
the viewed member and the "which repository is asking?" problem of group sync
does not arise. A member's background load (§10) is not `m.loading`: it must
never make `opsIdle()` false, or every switch would be refused while a member
loads. A switch into a member whose load has not finished shows that member's
panels as loading (`viewLoading`), never empty-as-clean.

**Per-repository config.** `Config` splits in two. **Per member** (read from
that member's own `.gg.toml` overlay, applied to its slot's service and
lists): everything `applyServicePolicies` hands a service, branch and
worktree templates, `[[tools.command]]`, `[[branches.filter]]`, `[refresh]`,
`[versions]`, `[notes]`. **Process-wide** (one terminal, one keyboard, one
scheduler): `[ui]` whole (theme, graph, counts, diff options, language,
steering), `[console]`, `[tasks]`, `[agents]`, `[web]`, `[forge]`, `[debug]`.
The process-wide part comes from the workspace's **settings repository**:
the member named by `settings_repo` (§1, chosen with `s` in the Workspaces
popup), else the member gg was opened in. It is fixed for as long as the
workspace is open: **one theme for the whole workspace**, and neither a look
nor an adopt into another member changes it (unlike `R` today, which re-reads
config on a re-root). It is re-read when the user changes `settings_repo` or
opens another workspace. The Settings popup edits the settings repository's
config, and inside a workspace its title names that repository so an edit
made while looking at another member is never a surprise.

**Per-panel versus per-group keys.** The sort mode and the `/` text filter
stay **per panel**: they apply to every group as one list (one key, one
visible effect, and `worktreeRing()` follows what the tab displays). The
alt+1…5 branch-filter slot stays **per repository**: the keys act on the
group the cursor is in, and each group shows its own active slot.

**Dropping a member** (leave, remove, open another workspace): a member with
a running agent or terminal console asks once ("shop-web has 2 running
sessions — leave them running (hidden), stop them, or cancel?"); the first
option is the safe one. Its worktree slots, parked windows and queued
results go; its watcher closes; its sessions, if kept, stay in the
process-global manager and the ctrl+\ popup.

**Presence and publishing.** `sessionreg` keeps publishing one worktree per
TUI (home) plus the viewed worktree, whichever member it is in; the agent
channel stays one per TUI. The steer inbox and session-docs directory move
with home and the view exactly as today's adopt and look move them. The
hosted web page follows **adopts only**, as today (never a look, never a
console's view), through the existing follow in `webhost.go`: an adopt into
another member hands it that member's service the way `R` does; the page's
own repository switch asks the terminal as today and the terminal answers
under §4's rules.

## 7. The walks

The **ring** is the Branches tab's checkout rows top to bottom, over all
groups in workspace order — the one list already drives alt+w; it only
becomes longer and crosses headings. Concretely:

- **alt+w**: the next worktree in the ring, around again past the last. When
  the next worktree belongs to another member, the step is a member switch
  (§6) followed by the worktree switch; the cursor lands on that worktree's
  branch row inside its group. Everything else in alt+w's help text holds
  (first press closes windows / shows the current worktree, "a look, not a
  switch of gg's own worktree": the ring comes back to home).
- **alt+a**: walks every member's running agent sessions in the ring's
  order and binds the one shown; crossing into another member switches the
  view to that session's worktree. **alt+A** (shift) stays "the viewed
  worktree only" — unchanged. alt+t / alt+T likewise for terminals.
- **alt+q / alt+Q**: unchanged — they walk the *shown* worktree's screens;
  which worktree is shown is whatever alt+w / alt+a left on screen.
- The consoles' key tables (`console.go`'s pass-through lists) need no new
  keys; the ring they call into is simply the workspace ring.

Exactly one ring function (`worktreeRing()`, today's Branches-order list
generalised over groups) is the source for alt+w, alt+a and the Worktrees
tab's enter/click targets, so the three can never disagree about order.

## 8. Translation, help, footer, e2e

- Every new string goes through `i18n.T` with keys in all four bundles
  (the AST gates). New keys/entries are listed in `?` and the footer.
- Golden screens (`e2e/scenarios/`): the launch dialog (one and two
  workspaces), the grouped Worktrees and Branches tabs, alt+w crossing a
  heading, the Workspaces popup (both levels), and "alone" via esc.
- `CHANGELOG.md`, `README.md` (new palette entry, launch dialog, flag),
  `docs/CLAUDE-details.md` (the repository-slot design, ids) are updated;
  `CLAUDE.md`'s package map gains one row for `internal/workspace`.

## 9. Testing

- `internal/workspace`: load/save round trip, name validation, cap,
  `Containing` by checkout key (case folding on Windows/macOS), corrupt file,
  missing member kept, lock contention.
- TUI unit: the launch dialog appears only when the home repository is a
  member and esc opens alone; `--workspace` refusal; grouped entries for both
  tabs (headings not selectable, cursor skips them, disambiguated headings);
  a member switch keeps the leaving worktree's windows and the parked walk;
  results tagged with another member's service land in that member's slot,
  never the live one; alt+w order across a heading equals `worktreeRing()`;
  alt+A stays scoped; a member's watcher stays open while another member is
  viewed; dropping a member with running sessions asks first; `R` picking a
  non-member leaves the workspace; Leave workspace keeps the viewed one.
- Race gate (`./test.sh race`) before merging: five services, five watchers
  and the shared limiter are exactly where a data race would hide.

## 10. Left to the builder

- The exact `repoView` field list: everything `reRoot` resets today is the
  starting inventory (`reRoot` + `dataLoadedMsg` are the two places that
  enumerate it).
- Whether a member's first load happens eagerly at workspace open (all at
  once, under the limiter) or on first view; eager is the default — the
  groups must show rows without being visited — with the Branches/Worktrees
  sources first and commits last.
- How the launch dialog and the existing startup prompts (resume-paused-op,
  slow-fs warning, notices) order themselves: the workspace question comes
  after a repo-health blocker and before anything that needs a choice of
  worktree.
- The open item "create/repair/move-home reRoot wipes sleeping slots' parked
  windows" becomes more visible with members; a member switch must not go
  through that path.

## 11. Decisions made in the conversation

- One repository on screen at a time; the walks span the workspace (user).
- Worktrees **and** Branches tabs show all members' rows under a top-level
  heading = repository directory name (user).
- Header/status line stays the worktree path; no repository name added
  (user).
- Cap of 5 members (user). Dialog on launch when the opened repository is in
  ≥ 1 workspace; plain open otherwise (user).
- Machine-local storage, `--workspace` flag, the slot-based (non re-root)
  switch, status-of-sleeping-worktrees-on-switch, actions on another
  member's rows switch first, headings clickable, Leave workspace entry, the
  drop-a-member prompt: my calls, open to change.
- After the builder check (settled by me, from how today's code behaves):
  look/adopt kept as today and both may cross members; switches stay refused
  while an operation runs; the config split; per-panel sort and `/`,
  per-group alt+1…5; every way into a member stays inside the workspace.
- Second round (user): alt+A stays scoped to the viewed worktree's
  repository; `R` switches between workspaces and repositories; the theme
  comes from one chosen member, selectable in the workspace's settings
  (my extension: that member supplies the whole process-wide config block,
  not only the theme, so the session has one set of UI settings); the add
  picker offers the repositories open now, the registry's recents and a typed
  path.

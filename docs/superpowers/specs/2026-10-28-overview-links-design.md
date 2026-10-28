# Overview links — every overview is addressable

Date: 2026-10-28 · Branch: `feat/overview-links`

## Why

The user wants to hand one agent's analysis to another agent. They open the
ctrl+\ **Overviews & notes** tab (or a commit's **Overviews** row in its
files view), copy a `gg://` link to an overview, switch to another agent's
console with alt+a and paste the link. The receiving agent reads the overview
and the notes its anchors point to, and uses them as context.

The receiving agent may be in **another worktree or another repository**:
some tasks span repositories. The agents run in the same gg process, on this
machine.

Today only permanent overviews (stored on a commit, `gg overview save`) have a
link (`?overview=<id>`, `domain.OverviewLink`); the TUI copies it only from
View all notes (ctrl+l, `all_notes_popup.go` — `asyncCopyLinkRow("copy-gg-link",
i18n.T("Copy gg link"), …)`, reuse that label and helper) and gg web serves it
(`web/overviews_http.go`). Session overviews (an agent's documents in a
worktree's session-docs directory, `gg session overview add`, and keyed tours
`tour-<key>`) have no link at all.

The TUI shows three kinds of overview document (`open_file.go`): a session
overview (`srcOverview`), a permanent one (`srcCommitOverview`) and a
review's stored overview (`srcReviewOverview`). The ctrl+\ tree lists the
OPEN documents of the current worktree (`docTree` over
`openFiles.list(m.currentWorktree)`) — every session overview, and a
permanent or review overview once it has been opened.

## Goals

1. **Every overview has a `gg://` link**, permanent and session alike.
2. **Copy it from the TUI:**
   - ctrl+\ Overviews & notes tab: a key on an overview row (`y` — my call)
     copies its link; the tab's footer and the `?` help name it.
   - A commit's (and a PR view's) **Overviews** entry: its `.` menu gains
     **Copy gg link** (beside Open overview / Export as HTML / Delete
     overview — `commit_overview_rows.go`, `note_row_menu.go`).
   - The `.` menu of an OPEN overview (`overview_keys.go` `overviewRows`)
     gains **Copy gg link** (my call: so no trip back to ctrl+\).
   - The copied link is recorded in the link history like every other
     copied link.
3. **An agent reads it:** `gg overview show <link>` prints the overview for
   either kind from any checkout on this machine — title, author/created, the
   text, then its anchors: each anchor as a `gg://` LINK into the overview's
   own checkout (its worktree for a session overview, its commit for a
   permanent one — never the reader's cwd), and for a `note:` anchor the
   note's text. `--json` gives the same as data. (My call: code lines are not
   inlined; the agent reads them with `gg link text <anchor link>`, which then
   reads the source worktree's copy, not its own.)
4. **The link opens it on screen** wherever a link opens — the `#` prompt,
   `gg open`, `gg session navigate` — the same way ctrl+\ enter opens it;
   a link to another worktree or repository follows the existing link
   behaviour for another checkout.
5. **A gone session overview fails clearly**: "overview <id> no longer
   exists" (CLI exit 1; TUI status line). (My call: a gone PERMANENT
   overview keeps today's behaviour — `onOverviewHint` opens its commit and
   says so; that is not silent and its tests stay.)
6. **Agents are told:** using-gg says a pasted overview link →
   `gg overview show <link>` (bump `agentskill.Version`), next to the
   existing review/note link lines. gg-overview mentions that an overview's
   link can be handed to another agent.

## Non-goals

- **gg web** — a REQUIRED follow-up, not dropped (the user asked for web UI
  parity: the same copy actions on its Overviews & notes list and overview
  views, and gg web opening a session overview link instead of refusing it).
- Links that travel to other machines. A session overview exists only in its
  worktree's session-docs directory, so its link is machine-local by
  construction (like shelf links).
- Converting a session overview into a permanent one, or any change to how
  overviews are written, stored or deleted.
- Copying a link to a single anchor (already: Copy anchor reference).

## Anti-goals

- A link that resolves to the WRONG overview (e.g. same id in another
  worktree) — a session overview's link must name its worktree.
- A link that only works from the worktree it was copied in.
- The receiving agent needing the human to open anything first: reading must
  work from the files on disk, with or without a gg window on that worktree.

## Decided

- **Link shape for a session overview** (my call): the LOCAL link form —
  `gg://<abs worktree checkout path>?overview=<id>` — address-less, with the
  existing `overview` hint kind. A permanent overview's link always carries a
  commit (`@<sha>`); an `overview` hint with NO address on a local-form link
  (today a hard error in `linkresolve.go` `finishLink`) now means "that
  checkout's session overview <id>". The address, not the id format, decides
  which store is asked; the branch must come BEFORE `checkOverviewHint`
  (`linkresolve.go:150`), which requires a commit. Session ids are the docs
  directory's file ids (`agentdocs.MintID`, e.g. `20261028T150405-ab12`, or
  a tour's fixed `tour-<key>` — a tour's link always shows its latest
  content).
- **Hint-blind verbs refuse it** (my call): the new form names no files, so
  `gg link text`, `gg compare` / `EvalLink`, `gg diff` etc. refuse it with
  "an overview link names no files — read it with gg overview show" rather
  than treating it as the whole working tree. One choke point in domain, not
  per-command guards. `gg link resolve` prints the checkout + `overview <id>`.
- **Where a session overview lives:** `steer.DocsDir(<inbox of that
  worktree>)` — the files `<id>.md` (overview, TOML front matter) and
  `<id>.toml` (notes); another checkout's inbox is
  `config.SessionSteerDir(commonDir, top)`. Reading goes through `agentdocs`
  attached to that directory (as `internal/cli/session_docs_store.go`
  `openDocs` does for the agent's own worktree, generalised to any
  checkout), so the reading agent's own cwd does not matter.
- **The link names the overview's own worktree** — the root its docs
  directory belongs to (a brief/report tour lives under the WORKER's
  worktree), never `m.currentWorktree` by assumption.
- **A memory-only overview** (no docs directory attached; `f<n>` ids,
  `overviews.go:239`) never reaches disk, so no other agent can read it: the
  copy says "this overview is not saved to a worktree — no link" and copies
  nothing (my call).
- **A review's stored overview** (`srcReviewOverview`): Copy gg link / `y`
  copies the REVIEW's `?review=` link (my call — the overview is the
  review's summary; agents already read it with `gg review show <link>`).
- **`gg overview show` also takes a review link** (user, 2026-10-28): when
  the review's document carries an overview (`Review.HasOverview`) it prints
  that overview — review id, agent, what it reviewed, the text and its
  anchors as links at the reviewed tip (the working tree for a working
  review) — the same layout as the other two kinds. A review WITHOUT an
  overview is refused: "review <id> has no overview — read it with gg review
  show <link>". The remarks stay `gg review show`'s. One review is one
  agent's document with at most one overview (several agents reviewing one
  change = several reviews, each with its own link; a cross-review is merged
  into one), so one link names exactly one overview.
- **Copy points:** the ctrl+\ tab's `y` acts on an overview row of any
  kind; on other rows (children, Notes/Opened groups and files) it does
  nothing (my call; no new behaviour for files here). A permanent overview
  is copyable from the commit's Overviews entry without opening it first.
- **`gg open` / `gg session navigate --to web|ide`** with a session
  overview link: the web and IDE consumers refuse it clearly ("not
  supported here yet") until the web follow-up (my call).
- **Copy feedback:** the usual "copied" status + the link history record,
  through the existing async copy-link row helpers (`asyncCopyLinkRow`).

## Left to the builder

- Exact names of new domain functions (e.g. a `SessionOverviewLink` beside
  `OverviewLink`, a resolver for the address-less form) and how
  `gg overview show`'s `overviewByArg` dispatches between the two stores.
- `gg link resolve` / `linkdesc` output for the new form (follow the
  existing `overview <id>` line / "overview: <id> <agent> · <title>" shape).
- The exact text layout of `gg overview show` for a session overview — keep
  it close to the permanent overview's output so one agent instruction
  covers both.
- Translations (all four bundles) for every new label/footer/help string;
  footer + `?` help entries for the new key; CHANGELOG, README if
  user-facing surface changed, using-gg + version bump.
- Tests: domain (link build/resolve both kinds, gone overview, wrong
  worktree), CLI (`gg overview show <link>` from ANOTHER worktree and from
  another repository), TUI (copy from ctrl+\, from the commit Overviews `.`
  menu, from an open overview's `.` menu; navigate opens it), an e2e
  scenario if the existing overview scenarios make one cheap.

---
name: using-gg
description: Use when performing git operations (status, commit, pull, push, branch switch, stash, worktrees) in a repository where the gg CLI is available.
---

<!-- gg:using-gg:v159 -->

# Using gg (gigagit)

gg is a git client CLI built for very large repositories. When it is available
(`gg` on PATH, or a `./gg` binary in the repo), prefer it over raw git for the
operations below — its smart commands carry safety rails: automatic
stash/restore around switches, a never-drop-the-stash rule on conflicts, and
guards against removing the worktree you are standing in.

## Commands

- `gg status` — branch, upstream ahead/behind, changed files.
- `gg batch [--keep-going]` — run a script of gg commands from stdin against
  ONE process (one repo discovery for the whole script). One command per
  line; blank lines and `#` comments are skipped; a leading `gg ` is
  tolerated; single/double quotes group words (`commit -m "two words"`) but
  there are NO pipes, env vars, globs, or redirection. Output framing, one
  section per command:

      #1 ok status
      on branch main (origin/main ↑0 ↓0)
      working tree clean
      #2 !1 push
      ! error: push-rejected needs a decision (options: rebase, force, abort); rerun with the matching flag
      #done 1 ok, 1 failed

  Header `#<idx> ok|!<exit> <cmdline>` precedes each command's output;
  stderr lines are prefixed `! `; the `#done` trailer summarizes. Batch
  stops at the first failure unless `--keep-going`; the trailer appends
  `(stopped)` when the stop skipped later lines (not when the failure was
  the last line). Nested `batch` inside a batch script is rejected.
  Sub-commands read an empty stdin — anything needing a decision fails
  loud with its options instead of hanging, exactly like a single
  non-interactive run. Exit: 0 all ok, 1 any failed, 2 script/usage
  error. Prefer batch whenever you would otherwise chain 2+ gg calls.
- `gg log [-n N] [<rev>|<A..B>]` — terse history, newest first: one
  `<short-sha> <subject>` line per commit. Default N=10, rev defaults to
  HEAD; ranges (`main..HEAD`) pass through.
- `gg diff [--stat|--name-only] [--cached] [<rev>|<A..B>] [-- <paths>...]` —
  working-tree diff (default), index diff (`--cached`), or commit/range
  diff. Default prints the full patch; `--stat` prints `path +A -D` lines
  plus a `N files +A -D` trailer (`path bin` for binaries; renames render as
  `old => new +A -D`); `--name-only` prints bare paths. Paths must follow
  `--`. An empty diff prints nothing.
- `gg show <commit> [--patch] [-- <file>...]` — `<short-sha> <subject>`
  header plus the commit's terse stat block (default) or full patch
  (`--patch`).
- `gg review [--tool <name>] [--working] [<rev>|<A..B>]` — runs a configured
  AI review agent headless. The agent replies with the gg review document
  (JSON: a markdown overview plus per-file line notes); stdout is the
  overview, its meta, then one `path:line — summary` line per note (`-line`
  = a removed line). A reply that is not the document prints as it came,
  with `warning: the review is not in gg review format` on stderr. The
  review is stored as a note on the reviewed commit (a range's last commit;
  a branch review's tip, carrying the branch name) and `note: <id>` is
  printed on stderr. A `--working` review is stored too, in this worktree's
  notes: it includes untracked files and records each reviewed file by its
  blob id, so `gg note list --file <path>` shows its notes on a file only
  while that file still matches what was reviewed. Its link is the working
  tree plus `?review=<id>`.
  There is no report file. Flags must precede the positional (like `gg log
  -n`). No positional reviews the current branch's work; a single `<rev>`
  reviews just that commit's own change (`rev^..rev`; a repository's first
  commit: everything it adds); an `A..B` positional
  is used as a range; `--working` reviews uncommitted changes. `--tool`
  picks among configured `review` commands when more than one is set up.
  `--model <m>` runs the tool on that model: gg adds the agent's own flag
  (Claude `--model`, Codex `-m`, Junie `--model=`, Kimi `-m`, Antigravity
  `--model`) or fills `<model:FLAG>` / `<model>` in a custom command; a
  tool with neither refuses it (exit 2), and so does one whose command goes
  on after the agent's own arguments — a `|`, `;`, `&`, `&&`, `||`, a further
  command line or a `#` comment would take the flag (use `<model:FLAG>`
  there); gg does not check the name — the
  agent's CLI does — but refuses a `"`, `%` or line break in it.
  `--focus <text>` tells the reviewer what to look at hardest: gg writes it
  into the review brief (a `## Focus` section), never into the command, so
  any text works; empty or over 2000 characters exits 2.
  `--link <gg-link>` reviews what the link names, as `gg review save` does
  (not with `--working`, `--preview`, `--notes` or a positional). `--no-save` prints the
  review and stores nothing; with `--json` stdout is the review document
  itself, and a reply that is not one exits 1 with the text on stderr
  (`--json` needs `--no-save`; `--no-save` refuses `--notes`).
  `gg review --tools [--json]` lists the review tools: `name`, `agent`
  (the built-in agent id, `""` for a custom command), `mode`, `model`
  (whether `--model` works).
  Exit 0 on a produced report, 1 on tool failure/empty report/no review tool
  configured, 2 on a usage error. A report the note store cannot keep is
  still printed, then `error: review not saved: …` on stderr, exit 1.
- `gg review show [--json] <review-link|id|latest>` — read a STORED review
  back (another agent's, or your own earlier one): a header line `review <id>
  · <agent> · <date> · <what it reviewed>`, the overview, then one numbered
  remark per line, `[n] <path>:<line> — <summary>` (`-<line>` = a removed
  line, `<a>-<b>` = a range), its rationale and the remark's own gg:// line
  link indented beneath — hand that link to `gg link text` for the exact
  code. `--json`: `{id, agent, created, branch, base, tip, link, overview,
  meta, remarks: [{n, path, side, start, end, summary, rationale, meta,
  link, review_link}]}` (`base` is empty for a one-commit review; `remarks`
  is `[]` for a review that is not in the document format). Each remark also
  prints its id `review:<id>:<n>` — what `gg note reply` / `gg note resolve`
  take — and its REVIEW link (the line link plus `?review=<id>`, which
  reopens the review at the remark). A review link WITH a path names one
  file of the review, with a line one remark: `gg review show <that link>`
  prints only that file's remarks, or the remark(s) at that line — so a
  link the user copied from a review ("Copy remark link") tells you which
  remark they mean. Narrowed, it leaves out the review's outdated threads
  (they record no file) and says how many: `N outdated threads not shown`
  (`outdated_hidden` in JSON). `n` is the remark's stable
  index. `show` is always the subcommand — review a branch named `show` as
  `gg review refs/heads/show`. Exit 1 unknown/deleted review or a moved
  link, 2 a malformed link or one with no `?review=`. MCP: `gg_review_show`
  (`link` = a review link, an id or `latest`).
- `gg review save <gg-link> --agent <name> (--stdin | --file <path>) [--json]`
  — store a review document YOU wrote (the shape in the reviewing-with-gg
  skill's "Review document") as the review of the change the link names: a
  merge preview or commit pair (the review then belongs to that preview,
  never to its commit), a commit (its own change), a `@ref:` branch (against
  the trunk) or a working/staged file link (the working changes). A path or
  line in the link narrows nothing. Prints `review: <id>` and the review's
  gg:// link; `--json` → `{id, link, warn}`. Exit 1 on input that is not a
  review document (nothing stored) or a link naming no change, 2 on a usage
  error or a malformed link.
- `gg review save <gg-link> --dry-run [--json]` — store nothing; print what
  the link would review and how to read it; `--json` → `{kind, label, range,
  diff, hunks, checkout}`. `kind` = preview | pair | commit | branch |
  working; `diff` = the `gg diff` argument for the patch/stat (the range;
  `<empty-tree>..<sha>` for a root commit; `HEAD` for working changes);
  `hunks` = the `gg diff --hunks` argument (`""` for working changes — no
  hunk numbering covers HEAD → working tree, read the patch); `checkout` =
  where to run both (a working link may name another worktree). Read the
  change through these: a `@ref:` link is the tip's own change to `gg diff`
  but the branch against the trunk to a review. A stored working review
  carries fingerprints taken at save time, like the lane's.
- `gg link --review <id|latest>` — print a stored review's link (and record
  it in `gg links`); `latest` = the newest review in this repository.
- `gg link --pr <n> [<path>[:<line>]]` — a pull request's link
  (`gg://<repo>@<base>...refs/gg/pr/<n>`, the pair its view opens on; a
  fetched PR only — `gg pr fetch <n>` first). Opening it (`gg open`, `gg
  session navigate`) lands in the PR's view — on a checkout that has not
  fetched that PR yet, the view fetches it first when the PR is in the pull
  request list (otherwise it says so: list or search for it, then open the
  link again); `gg review save` over it stores a review that view shows
  (that one needs the PR fetched). Every verb that reads the PR's commits
  (`gg review save`, `gg compare`, `gg link resolve`) refuses an unfetched
  PR's link with `pull request #<n> is not fetched here — run gg pr fetch
  <n> first`. Navigating to one waits up to 30s for the view's fetch, then
  prints `queued: the TUI is fetching pull request #<n> — the view opens
  when the fetch finishes` (exit 0: it is still on its way).

### Review notes

```bash
gg diff --hunks [--json] [--cached] [<commit>] [-- <paths>...]   # numbered git @@ hunks per file
gg note add   --file <path> (--hunk N | --new-line N | --old-line N) [--cached | --rev <c>] \
              --summary "…" [--rationale "…"] [--author <name>] [--source user|agent] [--json]
gg note reply [<repo-link>] <note-id|review:<id>:<n>> --summary "…" [--rationale "…"] [--link <commit|gg://…>] [--json]
gg note resolve   [<repo-link>] <note-id|review:<id>:<n>> [--json]   # resolve a thread (any of its ids); a resolved thread folds
gg note unresolve [<repo-link>] <note-id|review:<id>:<n>>            # reopen it (exit 1 when it is not resolved)
gg note apply [<repo-link>] --stdin [--cached | --rev <c>] [--author <name>] [--json]   # agent-context v1 or a comments batch
gg note list  [<link> | --file <path>] [--type user|agent|all] [--cached | --rev <c>] [--json]
gg note list  --shelf <entry-id>                                   # notes gg left on a whole shelf entry
gg note rm    [<repo-link>] <note-id>
gg note clear [<link>] (--file <path> | --all) [--type user|agent|all] --yes
gg review --notes [--tool <name>] [<rev>|<A..B>]                 # also keep the review's notes as permanent notes (not with --working)
gg skill path [review|using-gg|delegate]                         # print the bundled skill's path
```

Notes are machine-local review remarks anchored to a line range on one side of
one file; the user reads them inline in `gg` and `gg web`. A note targets ONE
diff: no flag = the unstaged working tree, `--cached` = the staged diff,
`--rev <commit>` = that commit's own change (a range is refused). Line numbers
are 1-based. Full guidance: `gg skill path` (the reviewing-with-gg skill).

A stored review's remarks are threads: `gg review show` lists each remark's id
(`review:<id>:<n>`; `review:latest:<n>` = the newest review's), and `gg note
reply` / `resolve` / `unresolve` take it — answer another agent's review there.
Pull-request (forge) threads are not yours to change: GitHub owns their
resolved state, and only the user resolves or sends (see `gg pr`).
MCP: `gg_note_reply`, `gg_note_resolve`.

A note written for a pull request also has a SYNC state. `gg note list`
prints it after the `[source]` column when it is not plain local:
`[sending]` (a send is under way) or `[failed: <error>]` (the last send
failed; it is still local). JSON adds `sync` (`local` | `sending` | `failed`
| `github` — a note GitHub now holds), `send_error`, and `group` (`mine` =
your loose notes, `review:<id>` = a stored review's remarks,
`github:<review id>` = what one GitHub review posted). A note in `github` lives
on GitHub now: read it with `gg pr comments <n>`.

Three gotchas worth knowing up front (the reviewing-with-gg skill covers
them in full): a path with BOTH a staged and an unstaged note needs
`gg note list --file <path>` with `--cached` (or without) to resolve each
against its own diff — a bare `note list` can show a misleading verdict; a
batch error's `item N` is 0-based; `gg note clear --all` removes this
checkout's own working-tree notes plus EVERY commit note in the store
(commit notes are not worktree-scoped).

- `gg session status` — is a gg TUI or web page open on this worktree? Exit 1
  when none is.
- `gg session navigate --file <path> (--hunk N | --new-line N | --old-line N)
  [--cached | --rev <sha>]` — put the open window on that line; `--rev <sha>`
  alone reveals a commit, `--next-comment` / `--prev-comment` step the open
  diff. Add `--no-wait` to skip the 2s wait for the window's answer.
- `gg session reload [notes|status|worktrees|all]`, `gg session focus <panel>`,
  `gg session highlight add|clear` — refresh, switch panel, or paint an
  attention band. See the `reviewing-with-gg` skill for when to use them.
- Who gets a verb: with a TUI live, the TUI answers — and every screen verb
  (navigate, reload, focus, highlight, `files focus`, a background open) also
  reaches every gg web tab; with only gg web live, the page does. `--to tui`
  or `--to web` after the verb picks one side even while both run — e.g. `gg
  session overview add --to web …` shows the tour in the browser and leaves
  the terminal alone (a TUI serving its own page still lists it). A side that is not running: exit 1 `no gg TUI for this
  worktree (gg web is live)` / `no gg web page for this worktree (a gg TUI is
  live)`. Give `--to` once, after the verb (`-to` before it or a second
  `--to` exits 2). `--` ends a session verb's flags: what follows is an
  argument even when it starts with `-` (`gg session files focus -- -x.go`).

Inside a console gg started (an agent or an Open terminal), `$GG_INBOX` names
THAT gg, and every `gg session` verb talks to it first — even when gg now
shows another worktree (with `--to`, only while that side is live there). A navigate to a file or a highlight sent from a
worktree gg is not showing is NOT applied: exit 1 `gg is showing worktree <a>;
asked the user to switch to <b>`. gg raised a notice; tell the user and wait —
do not retry in a loop.

### gg links

A `gg://` link names one place in one repository — a file, a line on one side
of one diff, a hunk, or a commit — in a form a human can paste into a chat and
you can hand straight back to gg:

```text
gg://<repo>/<path>[@<target>][:<line>]     <target> = a full/short sha, "staged", or absent = the working tree
gg://<repo>/<path>[@<target>]#<hunk>       hunk numbers are `gg diff --hunks`'s
gg://<repo>/<path>@<sha>:old:<n>           the old side of that diff
gg://<repo>/<path>[@staged]:[old:]<n>~<fp>  an UNCOMMITTED line + its 8-hex fingerprint: gg re-finds the text
gg://<repo>/<path>[@<target>]:[old:]<a>-<b>  a RANGE of lines a..b on one side (every target a line link takes)
gg://<repo>/<path>[@staged]:[old:]<a>-<b>~<fp>  an UNCOMMITTED range + the fingerprint of its whole block: only CHECKED
gg://<repo>@<sha>                          a commit, no file
gg://<repo>@ref:<branch|tag>               a branch or tag TIP: the whole tree there
gg://<repo>@<a>..<b>                       a CHANGE-SET: only what differs between a and b
gg://<repo>/<path>@<a>..<b>[:<line>]       a file (or line of b's text) in that change-set
gg://<repo>/<path>@<a>..<b>:old:<n>        a line of a's text there — e.g. one the change-set removed
gg://<repo>@<target>...<source>            a merge preview: the Previews tab entry
gg://<repo>/<path>@<target>...<source>[:<line>]   a file (or new-side line) in that preview
gg://<repo>/<path>@<target>...<source>#<hunk>     a hunk of that preview's patch
gg:///abs/checkout/path/file.go:12         a repo with no remote: its absolute path
gg://<repo>@<sha>?bookmark=<id>            a trailing ?<kind>=<id> hint: bookmark | shelf | stash | preview | version | review
gg://<repo>@<a>..<b>?preview=<id>          a SAVED pair (or, on a <target>...<source> link, a saved merge preview)
gg://<repo>@<base>..<ours>?version=<unix>-<op>   a RECORDED BRANCH VERSION's frozen preview (the id `gg versions` prints)
gg://<repo>@<sha>?review=<id>              a STORED AI REVIEW of one commit (<base>..<tip> for a range or branch review)
```

`@ref:<name>` keeps the NAME on purpose: it addresses the branch, not
whichever commit it sits on today. `@<a>..<b>` is git's two-dot range and is
the one link form that names a SET OF FILES rather than a whole tree — each
half may be a sha or a refname. The `?<hint>` suffix records which UI surface
a link was copied from; `gg compare` ignores it, so a bookmarked commit and
the same commit off the log are one endpoint. The only exception is a
`?shelf=` hint that is the sole surviving source of the BYTES: a link with no
address at all (`gg://<repo>?shelf=<id>`, a shelved working-tree file) reads
the shelved blob, and `gg://<repo>@<sha>?shelf=<id>` falls back to the frozen
tar once that sha has been gc'd. Because `?` is the hint separator, a path, a
checkout path or a ref name containing `?` cannot appear in a link at all —
`gg link` refuses to print one rather than emit something that reparses
differently. `?preview=<id>` marks a link copied off a SAVED Previews entry
(a merge preview or a commit pair): it lands exactly like the bare link and
then reveals the saved row — by id, else by the entry holding the same set,
else a "not saved here" notice. The id is a lookup key, never a checksum, and
a `?preview=` hint with no `@…` address is refused (it names nothing).
`?version=<id>` marks a link copied off a recorded BRANCH VERSION (the
snapshot gg takes before a merge/rebase/pull): the address is that version's
frozen preview `@<base>..<ours>`, so `gg diff` on it shows what the branch
contributed BEFORE the operation, and a TUI/web landing reveals the Branch
versions row it came from. The id is `<unix>-<op>` with no branch (ids reject
`/`); the consumer finds the record by id, tie-broken by the pair. These
links are MACHINE-LOCAL: version refs are never pushed and `<ours>` is a
rewritten tip, so on another checkout the pair itself will not resolve.
`?review=<id>` names a STORED AI REVIEW (the id `gg review` prints as
`note: <id>`); its address is the change the review compared, so `gg diff`
on it shows exactly what was reviewed. When a human hands you a review link
— usually "check this review" — read it with `gg review show <link>` (below)
and check each remark against the code; `gg open <link>` shows the review in
their gg. A review link moved onto another change is refused (exit 1, `the
link does not match the review`); a review deleted since the link was copied
still resolves its address, and `gg review show` says `review <id> not found`.
Review links are machine-local like version links.
A link to an UNCOMMITTED line (the working tree, `@staged`, `?view=content`)
ends `:<n>~<fp>`: a fingerprint of that line's text, added by every copy path
(`gg link` too; `--no-fingerprint` prints the plain form). The file may have
moved on since the human copied it, so gg re-finds the text and tells you on
stderr — the verb still runs, on the line where the text is NOW:
`gg: line 33 moved to 41` (follow it; `(nearest of N matching lines)` means
the text is not unique, so check the line), or `gg: line 33 has changed since
this link was copied` (the line the human meant no longer exists as written —
say so rather than answer about whatever sits on line 33 now). `gg link
resolve --json` and `gg_link_resolve` report `line` (now), `asked_line` and
`anchor` (`same` | `moved` | `changed`). A commit, pair, preview or ref link
never carries a fingerprint (it is refused): those already name fixed content.
A RANGE link (`:<a>-<b>`, `:old:<a>-<b>`) names the lines a human MARKED —
in a diff, the file viewer or a preview — and usually comes with a question
("why are these here", "explain this"). Get exactly those lines with
`gg link text <link>` (header `<path> @ <target> (<side>), lines a-b`, then
`<no>\t<text>` per line; `--json` gives `path`/`target`/`side`/`start`/
`end`/`lines`; MCP: `gg_link_text`). It works for a single-line link too.
An uncommitted range carries ONE fingerprint over its whole block and gg does
NOT re-find it: if any marked line changed, or the block moved, every verb
refuses the link (exit 1, `the link is no longer valid: lines a-b of <path>
have changed since it was copied`) — ask the human for a fresh link instead of
guessing. `gg link <path>:<a>-<b>` prints one; `gg link resolve --json`
reports `end_line`; `gg session highlight add <link>` bands the range (do not
also pass `--end`); `gg note add <link>` writes a note covering the range,
shown under its last line with the range marked beside it.
`gg link resolve` takes both: a `@ref:` link answers with `ref <name>` plus
the tip it resolves to HERE, a `@a..b` link with `pair <a>..<b>`, and
`--json` carries `ref` / `pair_a` + `pair_b` beside the address fields. The
verbs that CONSUME a link are stricter, and differ from each other:

| link shape | `gg diff`, `gg open`, `gg session navigate` | `gg show`, every `gg note` verb, `gg session highlight` |
|---|---|---|
| `@ref:<name>` — a whole tree | takes it | takes it |
| `@<a>..<b>` — a change-set | takes it | **refuses it, exit 2** |

A change-set's only single commit is its newer end, and anchoring a note or a
`gg show` there would silently widen a bounded set of files into a whole
tree — so those verbs refuse rather than guess. `gg compare` evaluates either
shape directly.

A preview link spells the branch PAIR, never a sha: the names travel between
machines, the machine-local preview id does not. Every verb that takes a link
treats a preview link exactly as `--preview` — `gg diff`, `gg note
add|list|apply|clear`, `gg session navigate`, `gg session highlight add`,
`gg link resolve`. `gg show` refuses one (use `gg diff`), and the preview's old
side is the merge base, so `:old:` is refused at parse time; a `#<hunk>`
naming a delete-only hunk (no new side) is instead refused at RUN time, on
`gg note add`, `gg session navigate` and `gg session highlight add`. Build one
with `gg link --preview <id|label|<target>...<source>> [<path>[:<line>]]`.

`gg open <link>` shows it to the user in their gg: a link with no `:<line>`
opens the file and leaves the cursor alone (the plain `gg link <path>` form
is exactly this, so a link gg itself printed is always openable). It steers
whatever gg session is live in the link's checkout (`--no-wait` skips waiting for that
session's answer), or starts the TUI there positioned on the link; a bare
repository link (`gg://<repo>`) just opens the TUI in that checkout. `--web`
names the browser instead: a live `gg web` page is steered (only the page);
a live TUI with no page is asked to serve one from its own process (the
page then shows the TUI's agent sessions) and the link goes to that page;
otherwise `gg web` is started in that checkout with the browser opened,
landing on the link — and like the TUI launch it then RUNS IN THE FOREGROUND
until the user stops it, so do not wait on its exit (steer a page the user
already has open, or start it detached). Refused inside `gg batch` (exit 2 —
unlike every other batch line it might launch the TUI or a server). Use it
instead of launching `gg` yourself. (A human can also paste any link into the
TUI's `#` prompt.)

A **content link** names a file as it is ON DISK in the worktree, not a
diff: `gg://<repo>/<path>?view=content`. `gg link --content <path>` prints
one (exit 1 when the file is not in the working tree; `--content` takes no
target flag, hint flag or `#<hunk>`). `gg link --content <path>:<line>`
focuses a line — `gg open` puts the viewer's cursor on it (exit 1 when the
file has fewer lines) and answers `opened <path> at line N`; when the file
shrank since the link was made it lands on the last line and says `(line N is
past the end, M lines)`. `gg open` / `gg session navigate` show it in the TUI's
content viewer; `gg open --web` shows it in the browser's viewer (a live
page is steered, a live TUI is asked to serve one, else one is started). Every other link-taking verb
(`gg diff`, `gg show`, `gg note …`, `gg session highlight add`) refuses it
(exit 2). When the user asks you to "open" a file for them, this is the link
to build: `gg link --content <path>[:<line>]` → `gg open <link>`.

**Open files — let the user read along.** The TUI and `gg web` each keep up
to 100 files open per worktree (the ctrl+\ switcher lists them). You can use
that list:

- `gg open <content-link> --background` (or `gg session navigate <link>
  --background`) loads the file WITHOUT touching the screen and answers
  `opened <path> in the background [at line N]`; a file already on screen is
  left alone (`<path> is already open on screen`). Needs a live TUI or `gg web`
  page (exit 1 otherwise — it never launches one); with both live it goes to
  both and the TUI's answer decides (the page's is printed `web: …`); exit 2 for a link that is not a content
  link. When a 101st file pushes one out, the answer ends
  `; closed <path> (100 files open)`.
- `gg session files [--json]` — the open files, one per line:
  `<id>\t<path>\t<source>\t<:line|->\t<shown|background>` — an overview's
  row shows its quoted title in the path column — focus it by its id, column
  1 (`--json`: `id, path, source, rev, line, state, title`). Also in
  `gg_ui_state` as `open_files`.
- `gg session files focus <id|path>[:<line>]` — bring one to the front,
  optionally at a line; exit 1 `no open file <x>`. A foreground `gg open` of
  a working-tree file already reuses its open copy; `files focus` is how you
  reach the commit/shelf versions the user opened.
- With only `gg web` live, the page's server answers from its own list; a
  focus brings the file up in every open tab (`; no gg web tab is open to
  show it` when none is).

Walking the user through several files: open A, B and C with `--background`
first (they load while you think), then `gg session files focus <id>:<line>`
each one in turn as you explain it.

**Temporary notes — explain on the code itself.** When the user says "show me
on the files", put short remarks on the lines you are talking about. The user
reads each one in a box under its lines in the gg TUI and in the gg web
page's file viewer.

- `gg session note add <path>:<start>[-<end>] --summary "…" [--rationale "…"]
  [--author <name>] [--json]` — put a note on those lines of the file AS IT IS
  ON DISK. A file that is not open is opened in the background first. Answers
  `noted <path>:<lines> as t<n>`. `<path>` is repo-relative; an open file's id
  (`f3:10-12`) works too. Limits: 50 notes per file, summary 500 characters,
  rationale 4000 — an over-limit note is refused, never cut.
- `gg session note list [<path>|<file-id>] [--json]` —
  `<id>\t<path>\t<start>-<end>\t<summary>`; `(outdated)` marks a note whose
  lines have since changed.
- `gg session note show <note-id> [--json]` — the note and the lines it sits on
  now.
- `gg session note rm <note-id>` / `gg session note clear <path>|<file-id>`.

These notes are TEMPORARY: they live in the running gg's memory, follow their
lines when the file changes, and are gone when the user closes the file (X in
the TUI, x in the web switcher) or quits gg. They are not `gg note` review
notes and never reach the notes store. They need a live gg TUI or gg web page
(exit 1 when neither is live). A gg TUI that serves its own web page shows the
SAME notes in both — a note dismissed in one is gone from the other; with a
TUI live, the TUI answers. A standalone `gg web` answers on its own (with the
TUI's reply text, except `gg web is showing worktree …` for another worktree).

The walkthrough: `gg session note add` on each file (that opens them), then
`gg session files focus <id>:<line>` one file at a time as you explain it.
Keep a summary to one sentence; put the reasoning in `--rationale`. A note
taller than half the user's window is cut in place (they press enter to read
it all), so prefer several short notes on the exact lines over one long one.
A note is placed on the file as it is on disk at that moment — add notes
AFTER your edits, not before.

When the user's message holds `gg note t<n> <path>:<lines>`, they copied a
reference to one of your notes: run `gg session note show t<n>` to see which
remark they mean (if it answers `no note t<n>`, the user closed it — the path
and lines still say where it was).

**Overviews — a guided tour through several files.** When the user asks to be
shown or walked through something that spans several files, write an
overview: a short markdown document that lives in gg's memory — the TUI's, or
a gg web page's when no TUI runs (a TUI serving its own page shares one set
with it: same ids, closing one in either closes it in both) — whose links
are ANCHORS. The user tabs from anchor to anchor, presses enter to open
one (the real file comes to the front at the place you named), and backspace
(in gg web also the browser's Back) brings them back to the overview on the
anchor they left.

- Anchor destinations: `[label](path)` (the file), `[label](path:120)` (the
  line), `[label](path:120-140)` (the range), `[label](note:t7)` (the
  file one of your temporary notes sits on, at the note). `path` is relative
  to the worktree gg is showing, in slash form; lines are 1-based and `N-M`
  needs M ≥ N; a directory does not resolve. http(s) links stay ordinary
  links. Any other destination is not an anchor: it renders as the label plus
  the destination in dim text. Links past the 100th anchor become plain label
  text with no warning.
- `gg session overview add --title "…" [--file <md>] [--background] [--json]`
  — the markdown from `--file` or piped stdin — a terminal on stdin is
  refused, never waited on (≤ 64 KiB, ≤ 100 anchors, 20 overviews
  per worktree). Shows it unless `--background`. Prints its id (`f<n>`, an
  open file's id), one `unresolved: <dest>` line per anchor whose file or
  note was not found — fix those with `set` — and a last line when there is
  news: `added f<n> in the background (<why>)` when the user was busy (it
  waits; `gg session files focus f<n>` shows it later), `; closed <path> (100
  files open)` when a file was pushed out to make room. With `--json`, read
  `state` (`shown` / `background`). On gg web, `shown` means the open tabs
  were told to show it; `gg session files` says whether a tab really shows
  it (a tab that could not load it does not count).
- `gg session overview set <id> [--title "…"] [--file <md>] [--json]` —
  replace the text; the user's selected anchor stays selected when its
  destination is still there (on gg web it is selected again when a later
  `set` brings that destination back).
- `gg session overview list [--json]` — `<id>\t<state>\t<n> anchors\t<title>`;
  `show <id> [--json]` — `<id>\t<title>`, a blank line, the text; `rm <id>` —
  `closed <id>`. `gg session files focus <id>` brings one to the front.
- Answers you may get (exit 1): `no gg session for this worktree` (start gg
  first); `no overview f<n>` (the user closed it — add a new one); `20
  overviews are open; remove one first`; `gg is showing worktree <a>, not
  <b>` (the TUI is on another checkout — add, set, show and rm all check; gg web
  says `gg web is showing worktree …`). Exit 2 is misuse: `--title is required`, `the text is empty`
  (nothing on stdin), `give the text with --file or on stdin` (a terminal on
  stdin), `the text is over 64 KiB`.

A worker agent started by gg runs in its OWN worktree, which the user's gg is
usually not showing — `gg session overview` and `gg session note` answer `gg
is showing worktree …` there. A worker's overview is its final
`agent_report`: gg files it as a tour of the worker's worktree (see the
delegate skill).

In a file an anchor opened, gg draws EVERY line and range anchor your
overview has in that file as a band (the one the user opened brighter), and
`n` / `p` step the user through them in line order — so point at each place
you want seen in a file with its own `path:N` / `path:N-M` anchor rather than
one wide range.

Combine with notes: `gg session note add` the remarks first, then link them
from the overview (`[why this lock](note:t7)`). Order the anchors in the order
you would explain them — tab walks them top to bottom. Like notes, overviews
are temporary (gone on X or quit) and need a live gg TUI or gg web page. When
the user's message holds `gg overview f<n> "<title>" → <dest>`, they copied a
reference to that anchor: that is the step they are asking about.

`<repo>` is the repository name of the repo's remote (`gigagit`), resolved
through gg's machine-local repository history — so a link made on one checkout
finds the right one here.

- `gg link [<path>[:<line>]] [--cached | --rev <commit> | --preview <id> |
  --ref <branch|tag> | --pair <a>..<b> | --content] [--bookmark <id> | --shelf <id>]` —
  print the link for a place in the current repo. Those five target flags name
  the same thing, so pass at most one (exit 2 otherwise); `--bookmark` and
  `--shelf` attach the landing hint and are mutually exclusive. `--preview`
  given a saved entry's id or label (not a typed range, and with no path)
  appends `?preview=<id>` by itself. `--pair`
  resolves both halves to FULL shas so the link travels and still means the
  same thing tomorrow; `--ref` keeps the name. E.g.:

  ```bash
  gg link --pair HEAD~3..HEAD        # gg://repo@<sha40>..<sha40> — the last 3 commits' change-set
  gg link --ref main                 # gg://repo@ref:main — the branch tip, as a name
  gg link --ref main --bookmark b1   # …with the hint that names where it was copied from
  ```

  `gg link resolve <link> [--json]` says which checkout it names here and the
  address inside it; exit 1 when the repository is unknown or ambiguous,
  exit 2 when the link itself is malformed.
- A STASH is linked as the change-set it holds: `gg://<repo>@<parent>..<stash-sha>`
  (both full shas — never `stash@{N}`, which is renumbered by every push and
  drop). For a stash made with untracked files (`git stash -u`) the set
  INCLUDES those files, as additions, although `git diff <parent> <stash>`
  does not show them: the link means "what was stashed". `gg links` describes
  such a row as `stash: <subject>`.
- `gg links [--json]` — the links copied in this repository, newest first,
  one `<desc>\t<link>` row per line. Every "copy gg link" action records here:
  `gg link`, `gg compare` on a link argument, the browser's own copy rows, and
  every link copied in the TUI (Copy link on a file, commit, branch, tag,
  preview or stash row; `L` in the bookmark and shelf switchers).
  **Use it to pick up a place the user just copied instead of asking them to
  paste it again.** Twenty rows are kept; re-copying a link moves it to the
  top rather than adding a second row. Nothing copied yet prints nothing and
  exits 0.
- **A link the user pastes is enough.** Pass it as the FIRST positional to
  `gg diff <link>`, `gg show <link>`, every `gg note` verb (`gg note add
  <link> --summary "…"`, `gg note list <link>`, `gg note reply <repo-link>
  <id> …`), `gg session navigate <link>` and
  `gg session highlight add <link>[-<end>]`. Each verb's link replaces the
  flags that name the same thing, and passing both is a usage error (exit 2),
  never a silent override:
  - `gg diff <link>` / `gg show <link>` — replaces `--cached`, the `<rev>`
    positional and `-- <paths>`. (`gg diff` uses the link's file and target
    only: to see the hunk a `#<hunk>` link names, add `--hunks`. `gg show`
    needs a link to a COMMIT and exits 2 otherwise.)
  - `gg note add <link>` — replaces `--file`, `--cached`, `--rev`, `--hunk`,
    `--new-line` and `--old-line`; the link must carry `:<line>` or `#<hunk>`.
  - `gg note list <link>` — replaces `--file`, `--cached` and `--rev`; the
    link's own line or hunk is ignored (it lists that FILE's threads). A BARE
    preview link (no path) instead lists every path the preview covers — the
    same as `--preview <P>` with no `--file`.
  - `gg note reply <repo-link> <id> …` / `gg note rm <repo-link> <id>` — note
    ids are per REPOSITORY, so from another directory put the repository's
    link first (`gg://<repo>` or `gg:///abs/path`, no file or `@target`
    part): it only picks the checkout whose store holds the id. A bare id the
    current repository does not hold exits 1 and says which store it searched.
  - `gg note clear <link>` — a file link replaces `--file`, `--cached` and
    `--rev`; a repository link picks the checkout and `--file`/`--all` apply
    as usual.
  - `gg note apply <repo-link> --stdin` — the link's `@staged`/`@<sha>`
    replaces `--cached`/`--rev`; a link carrying a file is refused, since each
    batch item names its own path.
  - `gg session navigate <link>` — the same six, plus `--next-comment` and
    `--prev-comment`.
  - `gg session highlight add <link>[-<end>]` — replaces `--file`, `--cached`,
    `--rev`, `--start` and `--side`; `--end` is still yours to pass (but not
    together with the link's own `-<end>`, and never on a `#<hunk>` link,
    which already names a range).
- The verb runs against the checkout the link names even when your working
  directory is somewhere else — including posting into that worktree's session.
- **Quote a link that carries `#<hunk>`**: `#` starts a shell comment, so
  `gg diff gg://r/a.go#3` silently loses the hunk. gg applies no heuristic.
- Read `gg session status` (or `gg_ui_state`'s `cursor.link`) to see WHERE THE
  USER IS LOOKING right now, as a link you can pass to any of the verbs above.

- `gg add [-f] (-A | <path>...)` / `gg unstage <path>...` — stage paths (or
  everything incl. untracked with `-A`) / remove paths from the index
  keeping working-tree content. `gg add` + `gg commit` fully replaces
  `git add` + `git commit` for new files. When git refuses a path because
  .gitignore excludes it, the `stage.ignored` decision offers
  `force-add`/`abort`; `-f` pre-answers it with `force-add` (git add -f), so
  a non-interactive add of an ignored path needs `-f` or it fails with git's
  refusal.
- `gg commit -m <msg> [-a] [--amend]` — commit (`-a` also stages tracked
  modifications; `--amend` rewrites the last commit, reusing its message when
  `-m` is omitted). Prints a summary naming the commit it made:
  `✓ committed <short-sha> <subject>` (`amended ...` for `--amend`).
- `gg commit reword <commit> -m <msg>` — change a commit's message. HEAD is a
  cheap amend; an older commit replays its branch onto its own parent (in
  place, later commits preserved). Refuses the repository's root commit and a
  commit not on the current branch.
- `gg commit export-patch <commit> [--out <path>] [--force] [-- <file>]` —
  write a `git am`-able patch (`git format-patch -1 --binary --stdout`) for
  the whole commit, or with `-- <file>` just that file's change within it.
  Without `--out` the target defaults to `<parent-of-repo>/<name>.patch`
  (`<shortsha>.patch`, or `<shortsha>-<basename>.patch` for a file); `--force`
  overwrites an existing target, otherwise it refuses (exit 2). Refuses a
  merge commit — `git format-patch -1` on a merge silently emits a different
  commit's patch instead of erroring.
- `gg apply [--am | --working] <path>` — apply a patch file (the inverse of
  `gg commit export-patch`; round-trips it). Default = working-tree mode:
  lands the diff as unstaged changes, nothing committed; a hunk that
  doesn't apply cleanly falls back to a 3-way merge, and conflicts stay in
  the tree as markers (exit 1) for you to resolve and commit. `--am`
  recreates commits from a `git format-patch` mailbox (author/date/message
  preserved) and is atomic: a conflicting mailbox is rolled back completely
  (exit 1, nothing changed). `--am` on a plain diff is refused.
- `gg pull [<branch>] [--background] [--on-conflict rebase|merge|reset|abort] [--on-dirty shelve|discard|abort] [--on-stale-mapping remove|abort]` —
  smart pull; with `<branch>` + `--background` it fast-forwards that branch's
  ref without checking it out (or pulls in the worktree that has it checked
  out). When git refuses the pull because uncommitted work in that worktree
  is in the way (files the pull would overwrite, or a rebase pull on a dirty
  tree), `--on-dirty` answers the `pull.dirty` fork: `shelve` stores every
  change (untracked too) as one shelf entry `WIP on <branch>` and cleans the
  tree, `discard` throws it away (ignored files kept), both then retry the
  pull; `abort` prints `pull cancelled` (exit 0, nothing touched). Without
  the flag a piped run exits 1 naming `pull.dirty`. Dirt the pull does not
  touch never asks. On a diverged current branch, `--on-conflict=reset`
  hard-resets it to the remote tip, discarding local commits and uncommitted
  changes. If a per-branch fetch refspec references a branch deleted on the
  remote, every fetch exits 128 and blocks the pull; `--on-stale-mapping=remove`
  answers the `fetch_mapping.stale` fork by removing the stale mapping(s) (and
  their dangling tracking refs) and retrying. Without the flag a piped run
  fails with the original fetch error (config is never mutated unseen).
  After a successful pull, if the branch's own recorded change set (against
  what it pulled) shows a path the pull itself added or resurrected (a
  status flip such as `M` → `A`), gg prints `! this operation changed
  <branch>'s change set:` followed by one `<status> <path>` line per
  finding; a path that only dropped out (absorbed upstream) is noted
  quietly underneath as `(absorbed upstream: <status> <path>)`. Silent when
  nothing changed, or when no version was recorded to compare against.
- `gg push [--force | --force-with-lease] [--on-reject ...] [--map | --no-map] [<branch>]`
  — push a branch (sets upstream when missing). With no positional it pushes the
  current branch; with `<branch>` it pushes that local branch **by name without
  checking it out** (git pushes any local ref) — handy for a branch never pushed
  before. With no flag it is a plain push. If the remote moved ahead the push is rejected non-fast-forward;
  `--on-reject` then recovers it non-interactively: `rebase` replays your commits
  onto the remote tip and pushes, `force`/`force-with-lease` overwrite, `abort`
  cancels. With `--on-reject` unset a rejected push **fails** (non-interactive)
  or prompts (interactive) — it never silently no-ops. `--force-with-lease`
  force-pushes only if the remote branch has not moved since your last fetch;
  `--force` overwrites the remote branch unconditionally (no lease). Use one
  after a rebase/amend/reword rewrites history. The flags answer the
  `push-force` decision, so a force push never prompts; `--on-reject` cannot be
  combined with `--force`/`--force-with-lease`. `--on-reject=rebase` applies only
  when pushing the current branch — rebasing rewrites HEAD, so a rejected push of
  a non-current `<branch>` offers only force/abort. `--map` adds a per-branch
  fetch-refspec mapping when the clone's refspec doesn't cover the pushed branch
  (single-branch/depth clones); `--no-map` declines; with neither, non-interactive
  runs skip.
- `gg switch <branch>` — switch branches, auto-stashing and restoring local
  changes; on a restore conflict the stash is preserved, never dropped.
- `gg checkout <remote>/<branch> [-s|--switch] [--as <local>]` — check out a
  remote-tracking branch as a local tracking branch (fast-forward-safe: reuses
  an existing local branch only if it fast-forwards to the remote ref, and
  refuses a diverged one — retry with `--as <name>` to materialize it under a
  different local name). `-s` also switches to it.
- `gg remote ls | fetch | prune` — `ls` lists remote-tracking branches (one
  `remote/branch` per line); `fetch` updates tracking refs for all remotes
  (`git fetch --all`); `prune` drops tracking refs for branches deleted upstream.
- `gg remote rm <remote>/<branch>` — delete a remote branch (`git push <remote> --delete`).
- `gg tag ls | create | rm | checkout | push` — `ls` lists tags newest-first
  (one name per line); `create [-m <msg>] <name> [<commit>]` creates a tag at
  `<commit>` (default HEAD): lightweight, or annotated when `-m` is given;
  `rm <name>` (alias `delete`) deletes a tag locally; `rm --remote <name> [<remote>]`
  deletes it from the remote (`git push <remote> --delete refs/tags/<name>`; the remote
  defaults to the only configured one); `checkout [--branch <name>] <tag>`
  checks out a tag — detached, or onto a new branch created at the tag when
  `--branch` is given; `push <name> [<remote>]` pushes a tag to a remote (the
  remote defaults to the only configured one; specify it when there are several);
  `annotate -m <message> <name>` sets or replaces a tag's annotation message —
  turns a lightweight tag into an annotated one, or updates the message of an
  existing annotated tag, keeping its target commit unchanged.
- `gg compare <left> [<right>]` — print the files that changed between two
  endpoints, one `<status>\t<path>` line each (renames: `old -> new`). Each
  endpoint is a commit-ish (`HEAD`, a branch, `abc123`, `HEAD~2`), `@staged`
  (the index), or `@worktree` (the working tree); `<right>` defaults to
  `@worktree`. E.g. `gg compare HEAD` (working tree vs HEAD), `gg compare
  HEAD~3 HEAD` (what changed across the last 3 commits), `gg compare main
  @staged` (the index vs `main`). Rows are sorted by path, and **the order of
  the two endpoints is free**: `gg compare @worktree main` compares just as
  `gg compare main @worktree` does, with the statuses turned round (what reads
  `A` forward reads `D` reversed).
- `gg compare [--patch] <spec> [<spec>]` — specs may also be `bookmark:<id>`
  or `shelf:<id>` (a stored commit entry; find ids via `gg bookmark list` /
  `gg shelf list`). While the entry's commit exists this is a live tree
  compare; a gc'd shelved commit falls back to its frozen snapshot (noted on
  stderr, scoped to the files that commit changed) — and a frozen entry
  compares against `@staged`/`@worktree` too, which answers "is my shelved work
  already in my tree?". `--patch` prints unified diffs instead of the file
  list — the patch of EXACTLY the rows the listing shows, read left → right as
  typed, for every comparison the listing answers: a link with a `/<path>`
  renders that one file, an `@<a>..<b>` change-set its members, a reversed
  pair (`@worktree` then a commit) the same diff the other way round. (A file
  set renders one file at a time, so a very large one is slower than two whole
  endpoints.)
- **Either side of `gg compare` may be a `gg://` link**, mixed freely with the
  rest. A link with a `/<path>`, or a `@<a>..<b>` change-set target, names a
  SET OF FILES; comparing it against a whole tree projects the tree onto that
  set, so the answer is scoped to those paths and never grows to the tree's
  size. E.g.:

  ```bash
  gg compare gg://repo@ref:main gg://repo@ref:v1.2   # two tips, whole trees
  gg compare gg://repo@abc1234..def5678 gg://repo@ref:main
                                # did main already absorb that change-set?
  gg compare gg://repo/internal/tui/model.go@ref:main main
                                # one file, one row
  ```

  A link naming a DIFFERENT checkout is refused (exit 2): cross-repository
  compare is not supported yet.
- `gg compare --save <label> <left> [<right>]` — run the comparison AND keep
  it. Both sides are stored as `gg://` links whatever you typed, so
  `bookmark:<id>`, `shelf:<id>`, `@staged`, `@worktree` and a bare commit-ish
  all become links. A token that cannot be expressed as one is refused
  (exit 2) and nothing is stored; a comparison that FAILS stores nothing
  either.

  **stdout is unchanged** — still the `<status>\t<path>` list, so it stays
  pipeable. The saved id is reported on stderr as `# saved: <id>\t<label>`.
- `gg compare --saved <id|label>` — re-run a stored comparison. It prints
  exactly what the original invocation printed.
- `gg compare --list` — the stored comparisons, one
  `<id>\t<label>\t<left>\t<right>` line each. A saved MERGE PREVIEW is a
  one-sided entry, so its `<right>` column is EMPTY — the column count is
  fixed either way, for `cut`. Nothing is printed when none are stored.
- `gg compare --remove <id|label>` — delete ONE stored comparison (a saved
  merge preview is a row of the same store, so this removes one too).
  `gg compare --rename <id|label> <new-label>` relabels one; the id is derived
  from the two links, so it does not change. Both take no endpoints, print
  nothing on stdout, and confirm on stderr (`# removed: <id>\t<label>` /
  `# renamed: <id>\t<label>`). An unknown `<id|label>` is exit 2.

  ```bash
  gg compare --save "auth refactor" main feat/auth
  gg compare --list | cut -f1,2        # ids and labels
  gg compare --saved "auth refactor"   # run it again later
  gg compare --rename "auth refactor" "auth refactor v2"
  gg compare --remove "auth refactor v2"
  ```

  Saved comparisons and saved merge previews share ONE store, so
  `gg compare --list` shows both and `gg preview list` shows the previews
  among them.
- `gg preview add [--label <text>] <source> <target>` — save a MERGE PREVIEW:
  "what would <source> bring into <target>", i.e. the GitHub pull-request
  files-changed diff (`git diff target...source`, from their merge base to
  the source tip — NOT the tip-to-tip diff `gg compare` prints). Names are
  stored, not hashes, so every later `show` reflects the current tips.
  `gg preview list` prints `<id>\t<label>\t<source>\t<target>\t<state>\t<files>\t<ahead>`
  (state: `ok`, `merged`, `missing-source`, `missing-target`, `no-base`, or
  `error` with zero counts for a row whose summary failed — the reason goes to
  stderr and the other rows still print; exit 1 only if EVERY row failed);
  `gg preview show [--patch] <id|label>` prints the file list (or unified
  diff; a non-ok state goes to stderr with exit 1); `gg preview diff
  [--patch] <source> <target>` is the one-off form with no record, and
  `gg preview diff <id|label> --hunks [--json]` numbers a saved preview's
  hunks (the same numbering as `gg diff --preview P --hunks`);
  `gg preview rename <id|label> <text>`; `gg preview rm <id|label>`.
- `gg preview add --symmetric --base <base> [--label <text>] <a> <b>` — save a
  SYMMETRIC MERGE PREVIEW: ONE saved comparison of `@<base>...<a>` against
  `@<base>...<b>` — what each branch would bring into the base, compared (two
  branches that fix the same thing). `--base` is required (no default); a base
  equal to `<a>` or `<b>` exits 1, a missing `--base` exits 2. It holds branch
  NAMES, so it stays live. Prints the id; an already-saved one prints the
  existing id and exits 0. It is listed by `gg compare --list` (not
  `gg preview list`) and run with `gg compare --saved <id|label>`; the default
  label is `<a> vs <b> (base: <base>)`.
- `gg preview add [--label <text>] <a>..<b>` — save a COMMIT PAIR: the plain
  two-dot diff between two commits, as a named entry beside the merge
  previews. ONE argument holding `..` (three dots is a merge preview, not a
  pair). Any rev works and both sides FREEZE to full shas at save time, so
  `gg preview add main..feat/x` stores today's two tips and never follows the
  branches. Prints the id; an already-saved pair exits 1 naming the existing
  id; an unknown rev or `a == b` exits 2. `gg preview list` prints a pair in
  the same seven columns — `<id>\t<label>\t<a-full-sha>\t<b-full-sha>\tpair\t<files>\t0`
  (state `missing` when a commit is gone) — after the previews;
  `show [--patch]`, `rename` and `rm` take a pair's id or label too. Its link
  is `gg://<repo>@<a>..<b>` (see `gg compare --list`), which `gg diff`,
  `gg open` and `gg compare` accept. A pair is also a NOTE SCOPE, exactly like
  a merge preview: `--preview` takes `<a>..<b>` or a saved pair's id/label on
  `gg diff`, `gg note add|list|apply`, `gg review` and `gg link`, and every
  `gg note` verb plus `gg session highlight add` accept the pair link itself
  (`gg note add gg://<repo>/<path>@<a>..<b>:<line> --summary …`, `#<hunk>`
  numbered over the pair's patch). Notes land on `b`, NEW side only — they are
  ordinary notes on that commit and outlive the saved entry; `:old:` or a
  delete-only hunk exits 2, and `gg show <pair link>` still exits 2.
  `gg preview diff [--patch] <pair id|label>` prints a saved pair.
  Notes live inside a preview: `gg diff --preview <id|label|<target>...<source>>
  [--hunks [--json]]` prints the preview's own patch and numbers its hunks,
  and `gg note add --preview P --file F (--new-line N | --hunk H) --summary …`
  anchors a note on it. A preview note is stored on the SOURCE TIP and
  remembers the preview (`--json` carries `"preview": "<target>...<source>"`,
  or `"<a7>..<b7>"` for a pair). The TUI and the web page show a PREVIEW's
  notes in that preview only, never on the commit; a PAIR's notes (a range
  review) as a Range reviews row and ✎ on the pair's newer commit — and only
  while the reader is on the branch the note was written on (the branch
  checked out when `gg note add --preview <a>..<b>` ran), so write a range
  review from the branch it reviews. The old side (the merge base) is not addressable, so `--old-line`
  is refused. `gg note list --preview P [--file F] [--json]` lists the notes
  written IN that preview or pair, on any commit of the branch — one written
  when an earlier commit was the tip, whose lines a later commit changed, is
  reported `outdated` rather than dropped. A note written with `--rev` is its
  commit's own note: `--preview` does not list it (`gg note list --rev`
  does, and `--rev` still lists every note at that address). `gg note apply --preview P --stdin` imports
  a batch onto the tip (old-side items skipped), and `gg review --preview P
  [--notes]` reviews the pair — stored as the preview's own review.
- `gg branch current` — just the branch name (HEAD's short sha when
  detached).
- `gg branch ls` — local branches, `* ` marking HEAD, `↑a ↓b` when an
  upstream exists.
- `gg branch create <name> [<start-point>]` — create a branch (no switch);
  start point defaults to HEAD.
- `gg branch rename <old> <new>` — rename a local branch (`git branch -m`);
  refuses an existing target name.
- `gg branch delete [--force] <name>` — delete a branch; refuses the
  checked-out branch and branches checked out in a worktree. An unmerged
  branch is a `branch-unmerged` fork (`force-delete`/`keep`): pass `--force`
  to pre-answer it.
- `gg pr list|view|comments|fetch|forget` — pull requests through the
  forge's own CLI (GitHub's `gh` today); the write verbs are below. Needs `gh` installed, logged in and able to read this repo;
  otherwise every verb exits 1 with `gg pr: no forge CLI can read this
  repository's pull requests: <why>`. `--json` may sit anywhere.
  `gg pr list [--json]` prints `#<n> <state> <author>  <source> → <target>
  [draft] [<review_state>]  <title>` — open PRs first, newest-updated first; a
  fork head prints `owner:branch`. PRs gg already knows stay listed after they
  close, marked `closed`/`merged` (or `unavailable` when the forge no longer
  answers): known = listed open earlier in this process, or fetched.
  `gg pr list --state all|open|closed|merged --search <text> --limit <n>`
  (any one of the three flags) SEARCHES the forge instead — the way to a
  closed/merged PR gg never fetched. Same row format and the same bare JSON
  array; state defaults to `all`, limit to 50 (1…200), newest-updated first.
  `<text>` is the forge's own search syntax (`author:kim`, `head:fix/x`,
  `-label:bug` — a value may start with a dash; `--search=<text>` works too);
  a text that is only a number (`123`, `#123`) looks that PR up directly and
  ignores `--state` (unknown number = empty list, exit 0). When the forge has
  more rows than the limit, `more results — narrow the search` goes to STDERR.
  A bad state or limit exits 2. Search results never join the plain list.
  `gg pr view <n> [--json]` prints the row, the URL, the description, then
  `── conversation ──` (general comments and review verdicts, oldest first,
  `carol [changes_requested]: …`) and `── outdated ──` (inline threads whose
  lines later pushes changed, with their original line and hunk tail).
  `gg pr comments <n> [--json]` prints the inline threads that still have a
  position: `a.go:10-12 (new) carol [resolved]: body`, replies indented two
  spaces, file-level threads as `a.go (file)`, body continuation lines indented
  four. JSON: `{inline, hub, outdated, truncated}` of `{id, parent_id, kind
  (inline|file|general|review), author, body, path, side (old|new), line,
  start_line, outdated, resolved, hunk, verdict, created, updated}`;
  `truncated` = the PR has more than gg reads (100 threads × 50 comments, 100
  conversation comments, 100 reviews — per PR).
  `gg pr fetch <n>` fetches the PR head into the private ref `refs/gg/pr/<n>`
  (a local ref only — never pushed, never shown in the graph; a no-op when
  already current) and prints the ref, so the change itself reads with
  `gg diff <target>...refs/gg/pr/<n>`. Fork PRs work. `gg pr forget <n>`
  deletes that ref and drops a closed row.
  `gg pr comments` prefixes each thread root with `[<thread id>]`.
- `gg pr notes <n> [--json]` — what the PR's view holds: the local notes
  written for it, GitHub threads, draft replies; the ids `gg pr send --note`
  takes. A note or review FOR a pull request is written with `--preview
  <base>...refs/gg/pr/<n>` (after `gg pr fetch <n>`) — a review with
  `gg review save "$(gg link --pr <n>)" --agent <name> --stdin` (the
  document on stdin); it shows only in that PR's view,
  and a note on a PR's commit written any other way does not.
- **Sending to GitHub is the user's, never yours.** You cannot send
  anything to GitHub: inside any gg session `gg pr send`, `gg pr reply
  --send` and `gg pr resolve|unresolve` refuse ("agents can't send to
  GitHub"), and outside one they post only after a human answers the confirm
  at a terminal (there is no `--yes`). Write your findings as local notes
  (`gg note add`) or a stored review (`gg review save`), and draft replies to
  GitHub threads with `gg pr reply <n> <thread> <text>` (no `--send`: a local
  draft). Then tell the user what is ready; they send it from gg's PR view,
  gg web, or their own terminal. Your notes posted that way end with
  `— <agent> via gg`. Never run `gh` to post comments, reviews or resolves
  yourself.
- `gg versions [<branch>]` — list a branch's recorded pre-operation
  snapshots (taken automatically before merges, rebases, resets, amends,
  and branch deletion), newest first: `<id> <short-sha> <time> <subject>`.
  A two-branch row is followed by one indented line holding its preview
  link (`gg://<repo>@<base>..<ours>?version=<id>`) — paste it to `gg diff`,
  or into a chat, to hand someone the pre-operation change set. One-branch
  rows have none. The post-operation drift report (`! this operation
  changed <branch>'s change set`) ends with the same link as
  `recorded version: gg://…`.
- `gg link --version <branch> <id|latest>` — print ONE version's preview
  link alone (the same line `gg versions` prints under its row), recorded in
  `gg links`. A one-branch record has none (exit 1, "records no preview");
  an unknown id exits 1; it takes no path and no other target or hint flag.
- `gg versions show <branch> <id|latest>` — print the frozen change set a
  two-branch version recorded (`<status>\t<path>` lines, its own
  Base...Ours diff at operation time — never a live diff against whatever
  the branch is at now). A one-branch record (amend, reset, undo-commit,
  delete-branch, restore) prints `<id>: records no preview (a one-branch
  operation)` instead (exit 0, not an error).
- `gg versions restore [--discard] <branch> <id|latest>` — move the branch
  back to a recorded version (its own pre-restore state is snapshotted
  first). Restoring the current branch hard-resets; `--discard` answers the
  dirty-tree prompt. Also recreates a deleted branch. Exit 0 restored,
  1 failure/unknown id, 2 usage.
- `gg unlock [--yes]` — list git lock files (`.git/index.lock` and friends)
  stranded by a git process that was killed before it could clean up. While
  one exists every git command fails with "Another git process seems to be
  running in this repository". Without `--yes` nothing is removed: exit 0 when
  clean, **exit 1 when locks are present** (so it works as a precondition
  check), 2 usage. Pass `--yes` to remove them — but only once you are sure no
  other git is running, since deleting a live git's lock corrupts its write.
- `gg migrate [--yes]` — list pending store migrations (a feature whose data
  format is behind what this build writes, but repairable) and what applying
  each would discard. Without `--yes` this changes NOTHING — it is safe to
  run just to check. With `--yes`, applies every listed migration. A feature
  gg can't satisfy and can't repair keeps running with that one capability
  disabled rather than failing the whole command; only a Required feature
  (e.g. the git version floor) blocks gg from starting at all. The
  `versions` store's migration is a straight discard: a format-1 branch
  version carries no merge-base, so it can never become a preview, only be
  thrown away. A repo that still holds format-1 data **records no new
  versions at all** until it runs — rebases/merges/pulls there proceed with
  no safety net rather than mixing formats.
- `gg merge [--into <target>] [--on-conflict=keep|abort] [--no-ff] [-m <msg> | -F <file>] <source>`
  — merge one branch into another (default target: the current branch;
  worktree-aware — merges in the worktree that has the target checked out,
  autostashes when it must switch). `--on-conflict=keep` leaves conflicts in
  the tree (exit 1), `--on-conflict=abort` restores the tree (exit 0); with
  neither and no TTY, a conflict exits 1 with the options on stderr. `-m <msg>`
  or `-F <file>` (`-` = stdin) sets the merge commit's message — a multi-line
  message with trailers goes through `-F -` — and **implies `--no-ff`** (a
  message is written for a merge commit; a silent fast-forward would drop it);
  `--no-ff` alone forces a merge commit with git's own message. Flags precede
  the positional; `-m` and `-F` together, or an empty message, exit 2 (inside
  `gg batch` stdin is empty, so use `-m` or `-F <path>` there, not `-F -`). A
  successful merge prints the same change-set-drift summary as `gg pull` (see
  above) for the branch it moved.
- `gg rebase [--branch <b>] [--on-conflict=keep|abort] <newbase>` — replay a
  branch's commits onto `<newbase>` (default branch: the current one; `--branch`
  rebases another branch, switching to it). Worktree-aware — rebases in place,
  in the worktree that has the branch checked out (you stay put), or autostashes
  and switches. A conflict pauses the rebase: `--on-conflict=keep` leaves it
  paused for `git rebase --continue` (exit 1), `--on-conflict=abort` runs
  `git rebase --abort` (exit 0); with neither and no TTY, a conflict exits 1
  with the options on stderr. A successful rebase prints the same
  change-set-drift summary as `gg pull` (see above) for the branch it moved.
- `gg rebase -i --plan <file> <newbase>` — **interactive** rebase from a plan
  file (a gg rebase-plan JSON: ordered `{sha, action: pick|reword|squash|drop,
  orig, new_msg}`); the TUI builds this plan interactively. Squash composes a
  combined message (target subject + each squashed commit's message
  line-by-line). The working tree is preserved across the rebase; conflicts
  answer to `--on-conflict`.
- `gg cherry-pick [--on-conflict=keep|abort] <commit>` — apply `<commit>` onto
  the current branch as a new commit. A dirty tree is autostashed and restored.
  A conflict: `--on-conflict=keep` leaves it paused for `git cherry-pick
  --continue` (exit 1), `--on-conflict=abort` runs `git cherry-pick --abort`
  (exit 0); with neither and no TTY, a conflict exits 1 with the options on
  stderr.
- `gg revert [--on-conflict=keep|abort] <commit>` — create a new commit on the
  current branch that undoes `<commit>`. A dirty tree is autostashed and
  restored. A conflict: `--on-conflict=keep` leaves it paused for `git revert
  --continue` (exit 1), `--on-conflict=abort` runs `git revert --abort` (exit 0);
  with neither and no TTY, a conflict exits 1 with the options on stderr.
  Reverting a merge commit is refused (it needs `-m <parent>`, out of scope).
- `gg reset [--soft|--mixed|--hard] [--force] <commit>` — move the current branch
  to `<commit>`. `--soft` keeps the changes staged, `--mixed` (the default) keeps
  them unstaged, `--hard` discards uncommitted tracked changes (untracked files
  survive; the commits reset past stay recoverable via `git reflog`). If
  `<commit>` is not on the current branch the reset is refused unless `--force`
  is given (a non-TTY run lists the options and exits 1).
- `gg fast-forward <commit>` — advance the current branch to `<commit>` when it
  is a descendant of the branch tip (a fast-forward; no merge commit, like
  `git merge --ff-only`). Refuses with a non-zero exit if `<commit>` is not
  strictly ahead. Use it to move a base branch up to a commit on a branch built
  on top of it.
- `gg stash [-m <msg>] [-u] [-- <paths>...]` — stash the working tree (or only
  the named paths; `-u` includes untracked files).
- `gg stash list` — list stashes (`stash@{N}: <subject>`, newest first).
- `gg stash apply [<ref>]` / `gg stash pop [<ref>]` / `gg stash drop [<ref>]` —
  apply (keep), pop (apply + drop), or drop a stash; `<ref>` defaults to the
  newest. A conflicting apply/pop exits non-zero and keeps the stash.
- `gg discard [--yes|-y] (--all | <path>...)` — throw away unstaged changes:
  tracked edits are reverted (staged hunks kept), untracked files deleted.
  Destructive, so `--yes` is required (or a y/N prompt on a TTY). `--all`
  discards everything unstaged and refuses while the repo is conflicted; named
  paths must appear in `gg status` and a conflicted path is rejected.
- `gg shelf add [--staged|--rev <commit>] [--bucket <name>] <path>...` /
  `gg shelf list [--bucket <name>]` / `gg shelf rm <entry>` /
  `gg shelf restore [--force] <entry> <dest>` — the **shelf**: a non-git,
  per-file content store of frozen copies that survive even permanent deletion
  of the source. `add` freezes the unstaged (default), `--staged`, or `--rev`
  version of each path and prints its entry id. `restore` writes a stored copy
  to a **required** `<dest>` as an unstaged change (`--force` to overwrite an
  existing differing file, else it refuses). Entries persist per-repo under the
  machine-local state dir; the default bucket is implicit. `list` shows each
  entry's id and a bookmark-style origin (`<container> / <state-or-commit> /
  <path>`).
- `gg shelf commit [--name <name>] <sha>` — freeze `<sha>`'s **changed files**
  (content only, via `git archive`; no message/author) as one durable,
  path-less shelf entry and print its entry id. Unlike a bare git ref, the
  content is stored, so it survives `git gc` and history rewrites — the same
  durability guarantee a file entry gets from surviving deletion of its
  source. Capped at 200MiB of archive data. `--name` (must come before the
  `<sha>` positional) attaches a human-readable label, shown after the entry's
  address in `gg shelf list` (` — <name>`) and the shelf quick-switcher;
  omitting it leaves the entry unlabeled.
- `gg shelf export [--dir <path>] [--force] <entry-id>` — write a shelf
  entry's files to a directory **outside** the working tree (flags come
  **before** the positional `<entry-id>`). Without `--dir` the target defaults
  to `<main-worktree>.tmp/<name>` — a fixed sibling directory next to the repo
  (e.g. `/a/x/repo` → `/a/x/repo.tmp`); `<name>` is `commit-<7-char-sha>` for a
  commit entry, else the entry's id. `--force` overwrites an existing target
  directory; without it an existing target refuses (exit 2).
- `gg shelf cherry-pick [--patch] [--on-conflict=keep|abort] <entry-id>` —
  re-apply a **shelved commit** onto the current branch. While the original
  commit object still exists this is a live `git cherry-pick`
  (`--on-conflict` pre-answers a conflict: `keep` leaves conflict markers in
  the tree and exits 1, `abort` rolls back cleanly and exits 0). Once the commit is
  gc'd or the history rewritten, gg replays the patch snapshot frozen at
  shelve time (`git am --3way`, atomic — all-or-nothing); `--patch` forces
  that lane even while the commit exists. An entry shelved before patch
  support, or a merge commit, has no snapshot: the gc'd case then exits 1
  with a clear message. Exit 0 = commit created, or a clean
  `--on-conflict=abort`; 1 = failure or conflicts left; 2 = usage.
- `gg bookmark add [--rev <commit>] [--staged] [--worktree <path>] <path>...` /
  `gg bookmark list` / `gg bookmark rm <id>` /
  `gg bookmark paste [--force] <id> <dest>` — **bookmarks**: a persistent registry
  of richly-addressed file references (vs the shelf's frozen copies). `add` stores
  a live pointer — default = this worktree's working file; `--staged` the index
  side; `--worktree <path>` another worktree; `--rev <commit>` a committed file
  (frozen by blob sha). `paste` resolves the bookmark's **current** bytes (live
  for working/index, the pinned blob for committed) and writes them to a
  **required** `<dest>` as an unstaged change (`--force` to overwrite). A bookmark
  to a working file reflects later edits; to freeze bytes, shelf instead.
- `gg prefix ls` / `gg prefix add [--global] <value>` / `gg prefix rm [--global] <value>`
  — **branch prefixes**: reusable, templated branch-name skeletons in a two-scope
  store (repo by default; `--global` for every repo). `<value>` may use gg tokens
  (`<user:LABEL>`, `<seq:NAME:N>`, `<date>`/`<date:…>` (bare `<date>` = today as `yyyy-MM-dd`), `<parent-branch>`, `<repo>`,
  `<random-*>`; `<branch>` is rejected). In the TUI, the create-branch popup
  (`ctrl+p`) and create-worktree popup (`p`) let you pick one, fill any
  `<user:…>` labels, and append the rest of the name.
- `gg prefix resolve <id> [--set label=value]… [--parent <branch>] [--bump]`
  — print a prefix (its id from `gg prefix ls`) resolved exactly as the TUI's
  picker does: `--set issue-number=1234` fills `<user:issue-number>` (a
  missing label exits 2 naming the flag), `<date:…>` is now, `<repo>` the
  main worktree's name, `<parent-branch>` `--parent` or the current branch.
  Read-only: a `<seq:…>` counter prints its next number every time; pass
  `--bump` ONCE, for the name you will create, to advance it. Append the rest
  of the name yourself (`me/MTHR-1234` + `-fix-login`). `--template <value>`
  resolves a raw template instead of a stored id.
- `gg template list` / `gg template show <id>` / `gg template render <id> [--set label=value]… [--peek]` /
  `gg template add --title <title> -F <file|-> [--repo]` /
  `gg template edit <id> [--title <title>] [-F <file|->]` / `gg template rm <id>`
  — **text templates**: titled, multi-line texts (a PR description, a bug
  report) with the branch-prefix tokens plus `<branch>` (the current branch),
  in a two-scope store: `add` stores GLOBALLY (every repo) unless `--repo`;
  the other verbs find either scope (the repo row wins a tie; `--global`
  narrows). `list` prints `id<TAB>scope<TAB>title`; an id may be a unique
  prefix — an unknown or ambiguous id exits 2, a store file that cannot be
  read exits 1 with the reason. `render` prints the resolved text: `--set name=Ann` fills
  `<user:name>` (a missing label, or a label the template does not ask for,
  exits 2 naming it); it CONSUMES the template's `<seq:…>` counters — pass
  `--peek` to preview without advancing them. An unknown `<…>` (`<br>`,
  `<me@x.com>`) is literal text. `-F -` reads the text from stdin. In the TUI,
  `alt+x` opens the same templates (fill, then `y` copies).
- `gg undo` — undo the last commit, keeping its changes (ref-only soft reset).
- `gg worktree list` (plain: `branch<TAB>path`) / `gg worktree add [<start-point>]` /
  `gg worktree add --branch <name> [<path>]` /
  `gg worktree remove [--with-branch] [--force] <path>` — linked worktrees;
  `add` resolves branch/path templates from `.gg.toml` and may prompt on stdin
  for `<user:...>` fields; `add --branch` checks out the EXISTING branch in
  the new worktree (no new branch; refuses a branch already checked out).
  With `--branch` an optional `<path>` is the destination (relative to the
  current directory, like `git worktree add`), bypassing the path template —
  e.g. `gg worktree add --branch feat/x .claude/worktrees/feat-x`.
  `remove` refuses a dirty or **locked** worktree (an interrupted `add` can
  leave one locked); `--force` removes a dirty tree and unlocks a locked one.
- `gg worktree add --from <commit> [--keep staged|unstaged] [<branch-name>]`
  — create a worktree on a NEW branch cut from `<commit>` rather than a
  branch tip. Default name `<current-branch>_<short-sha>` (a trailing
  positional overrides it); mutually exclusive with `--branch` (`--from`
  always makes a new branch). `--keep staged`/`--keep unstaged` instead
  land the new branch on the commit's PARENT, with the commit's own diff
  left staged (`git reset --soft`) or unstaged (`git reset --mixed`) in the
  fresh worktree — redo a commit differently without touching the branch
  it came from. `--keep` requires `--from` (exit 2 without it, or on an
  unrecognized value); a root or merge commit under `--keep` is refused
  (exit 1) before anything is created — there is no single parent to keep
  the diff against.
  If `[worktree] post_create_hook` is set in `.gg.toml` (a multi-line TOML
  literal `'''…'''` shell script), `gg worktree add` will offer to run it
  inside the new worktree. The hook requires approval before it runs (the
  script is shown and you choose run/skip). Pass `--hook` to approve without
  prompting; pass `--no-hook` to skip without prompting. With neither flag
  `gg` prompts interactively on stdin and defaults to skip when stdin is not
  a terminal (piped/scripted invocations never run an unseen script). Env:
  `GG_MAIN_WORKTREE` (the main checkout, useful as a copy source),
  `GG_WORKTREE_PATH`, `GG_BRANCH`, `GG_REPO`. Hook output streams to the
  busy log; a hook failure is reported but does not roll back the worktree.
- `gg worktree prune` — drop stale worktree admin entries left behind by an
  interrupted or manually-deleted worktree (`git worktree prune`).
- `gg worktree recycle [--on-dirty=commit|shelve|discard|abort] [--force] [--as <name>] <path> <branch>` —
  check an EXISTING local branch out in an existing worktree `<path>` (not
  the one you are in), replacing what it has checked out. `<branch>` may
  also be a remote branch (`origin/foo`, when no local branch has that
  name): it is checked out first as `foo` (or `--as <name>`) — created
  tracking it, or fast-forwarded if it exists; a diverged one fails with a
  hint to use `--as`. A dirty target
  needs `--on-dirty`: `commit` commits everything there (untracked included,
  subject `Committed changes due to worktree recycle <date>`) on the branch
  that is leaving; `shelve` stages everything and stores it as ONE shelf
  set `WIP on <branch>`, then cleans the worktree — what a set cannot hold
  (deletions, the old side of a rename; an all-deletions change stores an
  empty `delete.me`) is written to a note on the entry: read it with
  `gg note list --shelf <id>` (`gg shelf list` prints the id); `discard`
  deletes the changes (untracked files too, ignored files kept); `abort`
  does nothing. Without the flag a pipeline
  exits 1 naming `recycle.dirty`. Refused: a paused rebase/merge, a lock
  file, a branch already checked out somewhere. A worktree in use — claimed
  by another agent, reserved, an agent session running there, a gg TUI open
  there, or the main checkout — asks `recycle.blocked`; a pipeline exits 1
  naming it and the reasons, `--force` recycles anyway (your OWN claim
  never blocks you). Flags go BEFORE `<path>`.
- `gg worktree list --json` / `gg worktree list --free [--json]` — every
  worktree with its facts (`dirty` counts + `last_change`, `claim`,
  `sessions`, `tui`, `reserved`, `paused_op`, `git_lock`), a `free` verdict
  and `blocked_by` reasons (`missing`, `main`, `detached`, `paused-op`,
  `git-lock`, `reserved`, `claimed`, `tui`, `session`, `dirty-recent`,
  `status-failed`, `check-failed`); `--free` keeps the free ones, clean
  first, then stale-dirty (untouched longer than `[agents] stale_after`,
  default 14d) by oldest change. `recycle` says which `--on-dirty` to pass:
  `none` (clean — omit the flag) or `shelve`. A blocked worktree's `dirty`
  is `null` (its `git status` is skipped).
- `gg worktree claim [--note <text>] <path>` / `gg worktree release [--force] <path>`
  — take a free worktree for yourself and give it back. Only an agent
  running inside gg (it has `GG_SESSION_ID`) can claim; outside gg, or with
  a session gg is not running, claim exits 2. Atomic: of two agents racing
  for one worktree exactly one wins, the other exits 1 with the reasons —
  pick the next. Claiming a worktree you already hold succeeds (safe to
  retry). A claim ends by itself when your session ends; `release`
  exits 0 even with no claim. `--note` (e.g. the issue URL) shows in the
  user's TUI.
- `gg worktree reserve <path>` / `gg worktree unreserve <path>` — keep a
  worktree away from agents (`[agents] reserved` in the repo config).
- `gg worktree rename [--force] <worktree> <new-name>` / `gg worktree move
  [--force] <worktree> <new-path>` — relocate a linked worktree's directory
  (`git worktree move`); `rename` is a same-parent move computed from just
  the new directory name (refuses a name containing `/` or `\` — use `move`
  for that). `<worktree>` resolves by path (absolute, cwd-relative, or
  main-worktree-relative, exactly like `worktree remove`) or by branch name.
  A relative `<new-path>` resolves against the invocation directory. The
  **main worktree is refused**. A **locked** worktree (an interrupted `add`
  can leave one locked) forks the `move-worktree-locked` decision —
  `unlock-and-move` / `abort`; `--force` pre-answers `unlock-and-move`.
  Moving/renaming the worktree your shell is sitting in still succeeds: gg
  leaves the tree before the git call (required on Windows, which cannot
  rename a directory a process holds as cwd) and updates the shell-init
  cwd-handoff file so a wrapped shell's `cd` follows to the new path.
- `gg repo list` / `gg repo switch [--exact] <query>` — the known-repository
  registry (MRU); `switch` prints the path of the unique match (a
  case-insensitive substring of the name or path; `--exact` requires the whole
  name or path, for a query that prefixes sibling checkouts).
- `gg inspect` — one-shot repo summary (scriptable health check).
- `gg init` — install/refresh this skill for detected AI agents. An agent gg
doesn't know can still get it: `gg init --to <path>` installs at a custom
location (a file receives a marker-delimited managed block, surrounding
content preserved; a directory receives `<dir>/using-gg/SKILL.md`) and
remembers the target, so `gg init --update` refreshes it too. `gg init
--mcp` registers gg's MCP server with Claude Code (user scope), which is what
gives an agent in a gg console the agent tools.
- `gg config init (--repo | --global) [--force]` — write a documented config
  file (every setting commented with its default); `--repo` → `.gg.toml` at the
  repo root, `--global` → `~/.config/gg/config.toml`. Refuses to overwrite
  without `--force`.
- `gg config populate (--repo | --global)` — add any settings missing from an
  existing config file as commented `[populated]` lines; never overwrites your
  values; idempotent. Use after upgrading gg to pick up new settings.
- `--time-track <file>` (global; combine with any command) — append one JSON
  span per process start, git subprocess, and operation to `<file>` for
  performance analysis.

### Picking a worktree for another agent

1. `gg worktree list --free --json` — free worktrees, best first.
2. `gg worktree claim --note <issue-url> <path>` — exit 1 means someone got
   there first: take the next one.
3. Name the branch by the user's scheme: `gg prefix resolve <id> --set
   <label>=<value> --bump` (ids from `gg prefix ls`), append the rest, then
   `gg branch create <name> <start-point>`.
4. `gg worktree recycle [--on-dirty=shelve] <path> <branch>` (pass
   `--on-dirty` only when its `recycle` is `shelve`).
5. When done: `gg worktree release <path>`.

`git-lock` can appear for one listing while another reader's `git status`
holds `index.lock` — list again before giving up on a worktree. A `claim`
that exits 2 right after your session started can mean gg has not published
it yet: retry once after a second. A claim made by gg on the other side of
a WSL/Windows pair (one repo, two hosts) is never judged dead from this side
— only its own side or the user releases it.

### Starting another agent

To hand a task to worker agents, follow the **delegate** skill (the
overseer's playbook: plan, brief, start, the wait loop, check, finish — and
the worker protocol). This section is the tool reference.

Inside a gg console you have gg's agent tools (MCP, via `gg mcp`) and their
CLI twins. Outside a gg console they do not exist: `gg agent` exits 2,
except `gg agent list`, which lists the sessions of the running gg TUIs.

- `agent_start {worktree, tool, prompt, note?}` / `gg agent start --worktree
  <path|name|branch> --tool <name> (--prompt <text> | --prompt-file <file|->)
  [--note <url>]` — starts a worker in that worktree with your task as its
  brief (up to 256 KiB; the note up to 1 KiB); prints its id. Your claim on the worktree (or a fresh one) passes to the worker and
  returns to you when it ends. `tool` is a session command named in the
  user's `[agents] spawn`. Write the brief as markdown and link the files
  and lines the worker should start from with repo-relative anchors
  (`[the parser](internal/x/parse.go:40-60)`, the overview anchor grammar):
  the user reads it as a tour of the worker's worktree.
- `agent_list` / `gg agent list [--json]` — every session of this gg;
  `mine` marks the agents you started. `name` is the user's name for the
  session when it was started with one (Start agent's optional name;
  `tool` / `label` stay the command). `activity` is what the agent is
  doing, read off its screen: `working`, `idle` (its turn is over, it waits
  for input), `question` (it waits for a decision — read the choices from
  `agent_screen`); absent when gg cannot tell. `stalled` means it printed
  nothing for two minutes while apparently busy, or showed nothing but its
  spinner for eleven minutes (a hung model call). `activity_since` is when
  that began. `report_at` / `report_final` say the agent reported
  (`agent_report`). To wait for a worker use `agent_wait`, not a poll.
- `agent_screen {id}` / `gg agent screen <id>` — its visible console text,
  with `activity` and, at a question, `options` (`key` is the digit to
  `agent_send` as a key for a numbered choice; `pick:<i>` names a
  cursor-style choice gg cannot press for you yet — answer it with
  `up`/`down`/`enter` keys instead). `report` is its latest `agent_report`,
  `reports` all kept ones; `gg agent screen <id> --reports` prints those
  instead of the screen.
- `agent_send {id, text?, enter?, keys?}` / `gg agent send <id> [text…]
  [--no-enter] [--key esc]…` — paste text (up to 64 KiB), then Enter (default
  when there is text), then keys (`enter esc tab up down … ctrl+c 1 space`). Only agents
  you started.
- `agent_kill {id, remove?}` / `gg agent kill <id> [--remove]` — only agents
  you started.
- `agent_task` / `gg agent task` — **a worker's first act**: your task.
- `agent_wait {id?, until?, timeout_s?}` / `gg agent wait [<id>] [--until
  idle|question|exit|report|any] [--timeout <s>]` — block until a worker
  you started has news, then return ONE event: `report` (its
  `agent_report`, text included), `exit` (with `exit_code`), `question`
  (with `options` — answer with `agent_send`), `idle` (its turn ended
  without a report — read `agent_screen`). Without `id`: any worker you
  started directly. Each event comes once; an `idle` or `question` only if
  it began after the last input to that worker. An `exit` ends every wait,
  whatever `until` asked — a dead worker reports nothing more.
  `timed_out: true` (default after 45 s — keep `timeout_s` under your
  client's tool timeout and at most 90 when it moves long calls to the
  background, as Claude Code does at 2 min: a backgrounded wait still takes
  the next event; 1 … 600; leave it out for the default) just means
  call again; with an `id` it carries the worker's `state` (`running` /
  `exited`) and `activity`. A turn too short for gg to see it working
  (a fraction of a second) ends without an `idle`: a `timed_out` whose
  `activity` is `idle` after you sent something — read `agent_screen`.
  The CLI exits 0 on an event, 3 on a timeout. A wait that was killed
  outright can swallow one event — if a worker seems silent for long,
  look at `agent_list` (`report_at`, `state`) or `agent_screen`.
- `agent_report {text, final?}` / `gg agent report [--final] [--] (<text> |
  -F <file> | -F -)` — flags before the text (a `--final` inside it is
  prose) — **a worker's last act**: your result for whoever
  started you (what changed, what you skipped, what they must do; up to
  64 KiB). `final` says you are done. You stay running until killed — do
  not exit on your own: the parent may read your screen or ask more. The
  user's session row shows `done` / `reported` until someone types into
  your session. Write it as markdown: the first line a one-line summary
  (rows and notices show it), then anchors to what you changed, most
  important first (`[new check](src/a.go:10-30)`) — the user opens it as a
  tour of your worktree.

A parent's loop: `agent_wait` → on `report` read it; on `question` answer
with `agent_send`; on `idle` without a report read `agent_screen`; on
`timed_out` call again (with `activity: idle` read `agent_screen` first); `agent_kill {remove: true}` when the work is done.

Refusals and what to do: "spawning is off" / "not in [agents] spawn" — ask
the user to allow the command in the global config; "has no <prompt> slot" —
the user accepts the command's update in Settings → External tools;
"approve … once" — the user starts that command from Start agent once;
"max_spawned cap" — wait for or kill a worker; "a spawned agent may not
start agents" — you are a worker: `agent_report` back instead; "you have no
workers" — `agent_wait` without an id needs a worker you started; "is not free:
claimed/…" — pick another worktree (`gg worktree list --free`); "this gg
console has no agent channel" — tell the user (an Open terminal never has
one).

## The rule that matters for agents

gg never hangs waiting for input mid-operation. When an operation hits a fork
(diverged branch, dirty worktree, unmerged branch), it needs a decision:

- Interactive terminals get a prompt; **non-interactive runs fail with exit 1
  and print the decision and its options to stderr** instead of blocking.
- Pre-answer decisions with the matching flag: `--on-conflict` for pull
  divergence; `--with-branch` / `--force` for worktree removal; `--force`
  for a locked worktree under `worktree rename`/`worktree move`; `--force`
  for unmerged branch deletion.
- On a non-zero exit, read stderr: it names the decision and the valid
  options; re-run with the matching flag.

Exit codes: 0 = success, 1 = operation failed or needs a decision,
2 = usage error.

**Paths.** Everything gg *prints* — `gg status`, `gg diff`, `gg show --patch` —
is relative to the **worktree root**, wherever you run it from. A pathspec you
*type* is relative to **your cwd**, exactly as it is for git: in `src/`,
`gg add .` stages `src/` and `gg add xxx.txt` stages `src/xxx.txt`. So a path
copied out of `gg status` only feeds straight back in when you are at the
root — the simplest habit is to run gg from the worktree root, where the two
are the same thing. (`gg note --file` is the one exception: it is always
repo-relative, because a `gg://` link can point it at another checkout.)

## Registering yourself as a gg tool

gg can call an external AI agent for four tasks: **resolving conflicts**
(the TUI conflict window's `t` picker), **resolving-and-completing a paused
operation** (the same `t` picker — the agent resolves, stages, runs the
matching `--continue` through further rebase rounds, and writes an overview
to `$GG_MESSAGE_FILE`; never `--abort`), **generating commit messages** (the
commit popup's `ctrl+g`), and **reviewing changes** (`gg review` and the TUI
review rows). Each tool is a `[[tools.command]]` block in gg's config. The
easiest path is the human running Settings → External tools, which detects
installed agents and writes these blocks; write a block yourself only when
asked to, or for a tool the catalog doesn't know.

Where to write it — **default to the global config**
(`$XDG_CONFIG_HOME/gg/config.toml`, default `~/.config/gg/config.toml`); it
is always active and applies to every repo. A repo-level block is trickier:
per-repo settings live in ONE of two files, and gg reads whichever is
active — the machine-local private file
`$XDG_CONFIG_HOME/gg/projects/<repo-key>/config.toml` **if it exists**
(`<repo-key>` = the main worktree's absolute path with `/`, `\`, and `:`
replaced by `-`), else the committed `<repo>/.gg.toml`. Appending to
`.gg.toml` while the private file exists does nothing — check for the
private file first. Global and the active repo file concatenate, and a
repo block wins a `(category, name)` collision.

```toml
[[tools.command]]
category = "commit_message"   # conflict | commit_message | review | conflict_complete
name     = "MyAgent"          # picker label; unique per category
mode     = "capture"          # capture = headless | terminal = takes the TTY
command  = '''myagent --do-the-task'''
# per_file = true             # conflict only: run once per conflicted file
# when_op  = "merge"          # conflict/conflict_complete only: merge|rebase|cherry-pick|revert
```

A structurally invalid block (unknown category/mode, empty name/command,
`per_file` outside conflict, unknown `when_op`) is silently ignored at load.
The command body cannot contain `'''`. **First run is approval-gated**: gg
shows the human the fully resolved command (Run/Cancel), remembered per repo
keyed on the block's text — editing the block re-prompts.

Per-category contract (env vars are absolute paths; stdin is /dev/null;
capture commands run with the repo worktree as working directory; capture
runs also get `GG_TASK=commit_message` / `GG_TASK=review`, so one command
can branch on the task):

- **commit_message** (`mode = "capture"`): read `$GG_CONTEXT_FILE` (change
  summary + recent commit subjects) and `$GG_STAGED_DIFF` (full staged diff;
  replaced by a stat-only note when huge). Return the message EITHER as
  stdout — plain text, or Claude-style `--output-format json` (`.result`) —
  OR by writing it to `$GG_MESSAGE_FILE`; **non-empty file content wins over
  stdout** (use the file if your stdout is a status report, not the answer).
  Format: subject line, blank line, body. Do not run `git commit`.
- **review** (`mode = "capture"`): the `<range>` token in the command text
  resolves to the injection-safe range under review (e.g. `abc12..def34`;
  empty for a working-changes review). Read `$GG_CONTEXT_FILE` (range label
  + numstat) and `$GG_REVIEW_DIFF` (the full diff). Return a free-form
  markdown report via stdout or `$GG_MESSAGE_FILE` (same file-wins rule);
  gg persists it under the state dir and shows it in a viewer. Do not
  modify repository files.
- **conflict** (`mode = "terminal"` — gg suspends and hands you the real
  terminal in the worktree): a paused merge/rebase/cherry-pick/revert has
  conflicts. Env: `GG_OP`, `GG_SOURCE`, `GG_TARGET`, `GG_CONFLICTED_FILES`
  (space-joined), `GG_REPO`, and `GG_CONTEXT_FILE` (op header + one
  conflicted path per line). Resolve the files and stage them; do NOT
  continue or abort the operation unless asked — gg re-reads status when
  you exit and offers the continue step itself. With `per_file = true`
  (mergetool style) the command instead runs for one file with the
  `<local>`/`<base>`/`<remote>`/`<merged>` path tokens (also as `GG_LOCAL`
  etc.); editing `<merged>` offers mark-resolved on return.
- **conflict_complete** (`mode = "terminal"` or `"capture"`): same env as
  `conflict`, plus an empty `$GG_MESSAGE_FILE` and `GG_TASK=conflict_complete`.
  Unlike `conflict`, you OWN the sequencer for this run: resolve every
  conflict, stage it, run the matching continue command (`git merge
  --continue`, `git rebase --continue`, `git cherry-pick --continue`, or
  `git revert --continue`), and repeat through further rebase rounds until
  the operation finishes. Never run any `--abort` command and never push —
  if a conflict cannot be resolved safely, stop and leave the operation
  paused instead. Finally write an overview (which operation, each file and
  how you resolved it, how many continue rounds ran, the final state) to
  `$GG_MESSAGE_FILE`; gg opens it in a report viewer on a clean exit.

Tokens usable in `command`: path tokens `<repo>` `<file>` `<local>` `<base>`
`<remote>` `<merged>` `<context-file>` (gg shell-quotes them), prose tokens
`<op>` `<source>` `<target>` `<conflicted-files>` `<range>` `<user:LABEL>`
(`<user:…>` prompts the human at run time). Plain `${GG_*}` env references
work too — the vars are always exported. `<bin>`/`<env:NAME>` are
wizard-only; in a hand-written block name the binary and env vars directly.

Worked examples (adapt the binary and prompt; `$GG_*` expand at run time):

```toml
[[tools.command]]
category = "commit_message"
name     = "MyAgent"
mode     = "capture"
command  = '''myagent run -q "Write a git commit message for the staged changes. Read the summary at $GG_CONTEXT_FILE and the full diff at $GG_STAGED_DIFF. Output ONLY the message: an imperative subject (max ~72 chars), a blank line, a short body."'''

[[tools.command]]
category = "review"
name     = "MyAgent"
mode     = "capture"
command  = '''myagent run -q "Review the change <range>. The full diff is at $GG_REVIEW_DIFF, a summary at $GG_CONTEXT_FILE. Write a markdown review (findings with severity, then a summary) into the file at $GG_MESSAGE_FILE (an absolute path outside the repository). Do not modify repository files."'''

[[tools.command]]
category = "conflict"
name     = "MyAgent"
mode     = "terminal"
command  = '''myagent "A git $GG_OP is paused with conflicts. The conflicted paths are listed in $GG_CONTEXT_FILE. Resolve the conflict markers and stage the results with git add. Do not continue or abort the operation."'''
```

If your tool refuses to write outside its project root, the phrase "an
absolute path outside the repository" in the `$GG_MESSAGE_FILE` instruction
is load-bearing — keep it.

Verify a registration headlessly with the review lane: `gg review --tool
MyAgent HEAD` — exit 0 with a report on stdout proves the block loaded,
the command resolved, and your output channel works (running the CLI
yourself is its own consent; the TUI's first-run approval popup still
guards `ctrl+g`/`t`). Plain `gg review` with a wrong/missing `--tool`
errors listing the configured names — a cheap existence check. The
commit_message and conflict lanes have no headless test path; they surface
in the TUI (`ctrl+g` chooser, conflict `t` picker, Settings → External
tools).

Once configured: commit messages come from `ctrl+g` in the commit popup,
reviews from `gg review [--tool <name>]` or the TUI review rows, conflict
tools from `t` in the conflict window.

## Interacting over MCP

Besides the CLI, gg ships an MCP server: `gg mcp` (stdio) serves the repo
it starts in — one server per repo, discovered from the working directory;
every reply carries `repo{common_dir, worktree}`. Register it from the
repo directory, e.g. for Claude Code: `claude mcp add gg -- gg mcp`.

Use the CLI for git operations — that is what it is for. Use MCP for what
only gg knows: `gg_ui_state` (the live TUI session — focused panel, cursor,
◉-marked commits, open compare, conflict/running-op state; `session: null`
when no TUI is running for the repo), bookmark and shelf listing/reading,
`gg_compare_trees`/`gg_compare_file` (diff any two versions, including
shelved-commit members), `gg_export` (copy a bookmark/shelf entry to a
directory), and two consent-gated mutations — `gg_cherry_pick` (re-apply a
shelved/bookmarked commit; falls back to the shelf's stored patch when the
original was gc'd) and `gg_write_to_worktree` (restore a stored file
version as an unstaged change). The mutating tools are annotated
destructive, so your MCP client prompts before running them.

MCP also carries the `gg://` link surface, so a link a human pastes into the
chat needs no shell round-trip:

- `gg_link_resolve {link}` — take the link apart: `checkout`, `path`,
  `state`, `commit`, plus `ref` for a branch-tip link, `pair_a`/`pair_b` for
  a change-set, `preview_source`/`preview_target` for a merge preview,
  and `line`/`side`/`hunk`/`hint_kind`/`hint_id` when the link carries them.
  A field is OMITTED when the link does not name it, so an absent `line`
  means "no line", never line 0.
- `gg_link_list {}` — this repo's copied-link ring, newest first, the same
  rows `gg links` prints.
- `gg_compare_links {left, right}` — the changed files between the two places
  two links name, in either order. Two links naming different repositories
  are refused.
- `gg_compare_list {}` — the SAVED comparisons `gg compare --list` prints:
  each row has `id`, `label`, `kind` (`preview` = a merge preview, `pair` = a
  frozen commit pair, `comparison` = two links), `left`/`left_desc` and — for
  a comparison only — `right`/`right_desc`. A preview's or pair's `id` or
  `label` is what the note tools' `preview` argument takes; a comparison's
  `left` and `right` go straight into `gg_compare_links`.
- `gg_compare_file` takes `{"source":"link","link":"gg://…"}` as either side:
  a link carries its own path and state, so it needs neither `path` nor
  `locator`.

All four resolve against the repository that server was started in; a link
naming a different checkout is refused rather than answered from elsewhere.

MCP also carries the review-notes surface — the same store `gg note` writes:

- `gg_notes_list` — review notes, resolved (read-only)
- `gg_note_add` — leave one anchored note (mutates)
- `gg_notes_apply` — import a batch of notes (mutates)
- `gg_note_rm` — remove one note (mutates)

## Shell following

Worktree and repo switches write the target directory to `--cwd-file <path>`
(human shells follow via `gg shell-init`). As an agent, just `cd` to the path
printed on stdout.

---
name: gg-overview-v2
description: Experimental v2 of gg-overview — present a result in gg as notes plus an overview whose links open the code. Only when the user types /gg-overview-v2.
argument-hint: "[what to present]"
disable-model-invocation: true
---

<!-- gg:gg-overview-v2:v5 -->

# gg-overview-v2 — present a result in gg (experimental)

This is the EXPERIMENTAL second version of gg-overview, installed by
`gg init --experimental` so the user can compare the two. The user typed
`/gg-overview-v2 [what to present]` (or said "gg-overview-v2"): follow THIS
file, not gg-overview. It differs in "Read before you write" and "What the
overview says".

**gg-overview** is gg's way of showing the user what you found or changed:
an **overview** — a short markdown document whose links are anchors into the
code — with your remarks on the exact lines. The user reads the
overview in their gg window, tabs from anchor
to anchor, presses enter to open the real file at the place you named, and
backspace brings them back. Present the result this way instead of (not as
well as) a long chat answer. (A worker started by gg: see "As a worker
agent" at the end.)

An overview is **PERMANENT by default**: gg stores it on the commit it
explains, and the user opens it again any time from that commit — `≡` in
gg's Commits list, an Overviews row in the commit's files, a `└ Overview` row under the
branch while its tip is that commit, View all notes, or its
`gg://…?overview=<id>` link. Make it a **SESSION** overview (files in the
worktree's session-docs directory, shown by every gg reader of that
worktree and kept until deleted) only when the user says "session",
"temporary" or "don't keep it" — then follow "Session overview" below.
Never turn one kind into the other.

The full command grammar is in the using-gg skill (`gg overview save` for
the permanent kind; "Session notes" and "Overviews" for the session
kind); `gg skill path using-gg` prints where it is. This skill is the
recipe.

**Wording.** Write every text the user reads — the overview, the notes,
your reply — in ASD-STE100 (80% compatibility when possible): short
sentences, active voice, one meaning per word, no idioms. Code, paths,
commands and identifiers stay exactly as they are.

**The user says only WHAT to present.** Everything else below is yours to
find out: which window is open, which gg to run, whether the save is
possible. Never ask the user for a flag, a path or a target; never make
them repeat a rule that is written here.

## Where it shows — find the window yourself

Run `gg session status` once, in the checkout you present. Its first line
says if the window shows that checkout: `this worktree: <path> — shown`, or
`— not shown (the window shows <other>)`. `runs in:` is where the window's
gg runs, not what it shows. Then it lists every
live window for this worktree: `tui:`, `web:` and `ide:` (an IDE with the
gg plugin). The `gg session …` verbs reach whichever is live WITHOUT any
flag: a TUI or a web page takes them, and when only the IDE is live the IDE
takes them. Add `--to tui|web|ide` only when several are live and the user
named one.

**A window on another checkout never stops you.** Notes and a session
overview are files in YOUR worktree's session docs; a permanent overview is
stored on the commit. Write them all the same, from your own worktree.
`no gg session for this worktree`, `not shown`, `the IDE shows <root>, not
<worktree>` and `gg is showing worktree <a>, not <b>: the document is saved
…` only mean that the user does not see it yet. Finish the work, then say in
one line where it shows ("written in wt-orange; it shows when you open that
worktree in gg"). Do not ask the user to switch first.

**The window is there but did not answer.** A verb that prints `queued: no
answer from the IDE within 2s` (or `… from the TUI …`) exited 0 but shows
nothing yet: the window is live and slow, or stuck. Do not repeat the verb
(a late answer would apply it twice) and do not start gg web beside a live
window. Wait a few seconds, then check with `gg session files` (a navigate or
a focus is listed once it landed). Listed = it landed; carry on.
Not listed after two checks = tell the user in one line that the IDE (or
TUI) did not take it and what you sent, then stop; it is theirs to restart.

**The right gg.** If `gg session status` prints no `ide:` line although
the user works in an IDE, or `gg overview` / `gg session overview` is
"unknown", you are running an old gg: list the candidates (`which -a gg`;
`where gg` on Windows), run the newest by its full path (`<path> version`
≥ 0.4.3), and never a bare `gg` from a checkout that carries its own old
`gg.exe`. Say which binary you used in one line of the reply.

## Read before you write

When you present a CHANGE (a commit, a branch, a pull request, your own
work), collect these facts first. Use gg, in the checkout of the change:

1. **The range.** A commit: `gg show <sha> --patch`. A branch or a pull
   request: `gg review save <gg-link> --dry-run --json` (the link from `gg
   link --ref <branch>` or `gg link --pr <n>`; it stores nothing) prints
   `diff` — the range against the trunk — and `checkout`, where to run the
   commands below. `gg diff --stat <diff>` gives the size; a rename shows
   as `old => new`.
2. **The intent.** `gg log -n 50 <diff>` lists the commit subjects; `gg pr
   view <n>` prints a pull request's description. Read them, and what the
   user said, before the code: they tell you what the change is FOR.
3. **The before.** `gg diff <diff> -- <file>` shows the removed lines. For
   old lines outside a hunk: `gg link --rev <sha> <path>:old:<a>-<b>` (a
   commit) or `gg link --pair <diff> <path>:old:<a>-<b>` (a range), then
   `gg link text <that link>`. Without the old code you cannot say what
   changed.
4. **New terms.** In the added lines, find each new type, function,
   constant, config key, CLI flag, operation, decision id or stored field.
   In the removed lines, find each name that is gone or renamed.
5. **The callers.** Search the checkout for each new name. Find where
   execution enters the new code from code that the change did not touch.
6. **Mechanical files.** Note renames, formatting-only edits and generated
   files. They go in one line of "Not covered", not in the walk.

To read a file AS IT IS at a commit (the working tree has other changes):
`gg link --rev <sha> <path>:<a>-<b>`, then `gg link text <that link>`. A
range past the file's end is refused with the file's line count.

## What the overview says — a walkthrough, never a file list

An overview explains how something WORKS; a list of files with a sentence
each is not one. Write it in the order you would explain it to a colleague
at a whiteboard, and let the anchors follow that order — the reader walks
them top to bottom, so the walk IS the explanation. The sections, in this
order:

1. First line: what the change (or the code) makes possible, in one
   sentence.
2. **Before → after** (a change only): what the code did before, what was
   wrong or missing, what it does now. One short paragraph. Anchor the new
   lines; the old lines stay in words (an anchor names a line at the
   commit).
3. **New terms** (only when the change adds them): one line per term —
   `name` (its kind), what it IS in the words of the product, not of the
   code ("a claim holds a worktree for one agent", not "a struct with
   three fields"), with ONE anchor on its definition line. Put a term
   before every term and section that uses it. Then **Changed meaning**:
   each existing term whose behaviour changed, anchored. These cause the
   most confusion — never skip one.
4. **Core idea**: one paragraph that names the shape ("a file inbox plus a
   watch", "one gate, writer first"). Add a small diagram in a fenced text
   block only when it makes the shape clearer than the words.
5. **How it works**: one real run through the code, step by step, in
   execution order, crossing files as the control flow does — the trigger
   (key, command, event, call; start at the caller in unchanged code),
   each decision point, the data handed on, where the result lands
   (screen, clipboard, store). One sentence per step, with ONE anchor on
   the lines where that step happens.
6. **Contracts** (only when the change touches one): every change to what
   other code, users or stored data depend on — a CLI flag, a config key,
   a stored format, the `gg://` grammar, a wire shape, a skill text — and
   what happens to old data and old callers. Say "breaking" when it is.
7. **Why this shape**: the two or three design decisions and what each
   avoids, anchored. A reason the commit, the PR or the user states needs
   no mark; write `(inferred)` after a reason you concluded yourself.
8. **Edges**: failure and the unusual cases, anchored to their branches.
9. **Not covered**: what you skipped, what you could not confirm (claims of
   a commit subject or PR description you did not find in the code), open
   questions, what the user should do next, and one line naming the
   mechanical files.

**Scale it to the change.** A small change (one commit, no new term) gets
sections 1, 2 (one or two sentences), 5, 8 and 9. A branch that adds
concepts gets every section that applies. Code that is not a change (the
user asked how something works) skips 2 and 6.

An overview's text, exactly as you write it:

```markdown
When the user saves, [`SaveOverview`](internal/domain/overview_save.go:96-120)
first runs [the whole-document check](internal/domain/overview_save.go:180-200).
```

Write each link bare. Backticks AROUND a link make it plain text: it
opens nothing, and gg refuses or reports it. For a code look, put the
backticks INSIDE the brackets: [`checkOverview`](internal/domain/overview_save.go:180).

Never group by file: a file may appear five times, in the order the flow
reaches it. Do not narrate the diff line by line: say what each step does
and why; the reader sees the lines. Prefer several precise anchors
(`path:N`, `path:N-M`) over one wide range; anchor the lines that SHOW the
point, not a neighbour of them (files with the same content, such as one
script copied into sibling directories, look identical in the viewer:
anchor the file that differs). Read the code before you write an anchor
("Read before you write" above, and the functions around each hunk). Each
anchored place also gets a short remark that says what that line
contributes to the flow (the `annotations` of a permanent overview, a
session note of a session one). Before you save, read each anchored
line again and check that it shows what your sentence says. Limits:
≤ 64 KiB, ≤ 100 anchors.

## Steps (permanent — the default)

1. **Finish first.** The overview explains a commit: make every edit and
   commit it before you write a single anchor.
2. **Work in the worktree whose commit you explain.** Paths are relative to
   it, in slash form; lines are the file AT that commit (`gg link --rev
   <sha> <path>:<a>-<b>` + `gg link text` when the tree has uncommitted
   changes).
3. **Write the document** as JSON:
   `{"version":1,"title":"<short title>","overview":"<markdown>","files":[{"path":"…","annotations":[{"newRange":[a,b],"summary":"…","rationale":"…"}]}]}`.
   The markdown is the walkthrough of "What the overview says" above;
   its links are the anchors: [the parser](internal/x/parse.go:40-60),
   [this check](src/a.go:120), [the config](config.toml) — never
   `note:` links (a permanent overview has no session notes). Your line
   remarks go in `files[].annotations` (one per anchored place, on the
   exact lines; summary one sentence, reasoning in `rationale`): gg draws
   them as read-only boxes beside the file, only in this overview.
4. **Save it.** `gg overview save [<gg-link>] --agent "<your name>" --stdin`
   (the JSON on stdin). No link = HEAD; a pull request: the link from `gg
   link --pr <n>`. gg checks the WHOLE document against the commit and
   refuses the save — nothing stored, every problem listed — for a path the
   commit lacks, lines past a file's end or a `note:` link: fix the
   document and save again (the same commit and title replaces). On
   `<path>: uncommitted changes` or `<path>: differs from <sha7> on disk`
   the commit cannot carry it: switch to the session recipe below
   without asking (the user wanted to see the result, not to commit for
   it), and say in one line of the reply that it is a session overview
   because <path> is uncommitted — unless the user asked for the stored kind, then
   tell them what to commit and stop.
5. **Show it.** `gg session navigate <the printed gg://…?overview= link>`
   opens it in whichever window is live — the TUI, the web page, or the
   IDE's tool window (`opened overview <id>`). `no gg session for this
   worktree` → skip this step: the overview is stored and the user opens
   it from the commit. NEVER start gg web for a permanent overview.
6. **Reply** in chat with the link, the title and a three-line summary. Do
   not paste the overview into the chat — it is in gg.

## Session overview — notes and overview as files — only when asked

Notes and overviews made this way are FILES in the worktree's session-docs
directory (`gg session docs path` prints it). Every gg reader of that
worktree shows them — the TUI, a gg web page, the gg-notes IDE plugin —
and they stay until the user or you delete them (`rm`; closing one in a
reader hides it there only). No window needs to be live.

1. **Finish first.** A note stores the file's lines as they are on disk at
   that moment — make every edit before you add a single note or anchor.
2. **Work in the worktree you present.** `cd` to it; paths in notes and
   anchors are relative to it, in slash form.
3. **Notes on the lines.** `gg session note add <path>:<start>[-<end>]
   --summary "…" [--rationale "…"] --author "<your name>"` — one per remark,
   on the exact lines it is about. Summary: one sentence; reasoning goes in
   `--rationale`. Several short notes beat one long one. A note may carry
   `--meta k=v` and `--display <json>` (reviewing-with-gg's "Meta and
   display": `kind`, `intent`; meta never triggers anything). Each answers
   `noted … as <id>` — the id is the note's file name; keep them for step 4.
4. **The overview.** The walkthrough of "What the overview says" above, as
   markdown; its links are the anchors: [the parser](internal/x/parse.go:40-60),
   [this check](src/a.go:120), [why this lock](note:<id>) (opens the file
   at your note), [the config](config.toml). The TUI and gg web show an
   overview beside the file each anchor points to (↑/↓ walk the anchors and
   the file follows, D flips the file to the diff, a note anchor shows your
   note, tab switches sides; a narrow window shows it full-screen, where tab
   walks them); the IDE's tool window walks them with Next anchor.
   Write it: `gg session overview add --title "<short title>" --file <md>`
   (or the markdown on stdin). It prints the overview's id (its file name);
   each `unresolved: <dest>` line is an anchor whose file or note was not
   found — fix the text and run `gg session overview set <id> --file <md>`.
   It then asks the live window to show it and prints that answer
   (`focused overview <id> "<title>"`). With no window live it prints
   nothing more and exits 0: tell the user in one line that no gg window is
   open and that the files are in the directory `gg session docs path`
   prints — whichever reader opens next shows them. Never start gg web or
   the TUI for this. `--background` writes without showing.
5. **Reply** in chat with the overview's id and title and a three-line
   summary. Do not paste the overview into the chat — it is in gg (in an
   IDE: the plugin's tool window lists it under "This session").

**Writing the files yourself** (a gg too old for these verbs, or no gg on
the machine). The directory is what `gg session docs path` prints:
`<state>/gg/sessions/<key of the git common dir>/steer/<key of the
worktree>/docs/`, where `<state>` is `$XDG_STATE_HOME`, else
`%LocalAppData%` on Windows, else `~/.local/state`, and a key is the
absolute path (cleaned) with every `/`, `\` and `:` replaced by `-`
(`/work/gigagit/.git` → `-work-gigagit-.git`; the common dir is `git
rev-parse --path-format=absolute --git-common-dir`, the worktree `git
rev-parse --show-toplevel`). Create it if it is missing. An id matches
`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`; mint it as `<UTC stamp>-<4 hex>`
(`$(date -u +%Y%m%dT%H%M%S)-$(printf %04x $RANDOM)`). Write the whole
file to a temp name in that directory that starts with `.` and ends in
`.tmp`, then `mv` it over the final name — never write in place.

An overview is `<id>.md`, a TOML front matter block then the markdown:

    +++
    format = 1
    title = 'How scroll coalescing works'
    author = 'claude'
    created = 2026-10-18T14:32:01Z
    +++
    Scroll events are coalesced per frame, so [one flush](internal/tui/scroll.go:102-104) …

A note is `<id>.toml`. `lines` MUST be the text of lines `start..end`
exactly as you read them from the file after your last edit, one string
per line, no line terminator (that is how a reader places the note on the
file as it is now, and follows it when the file changes):

    format = 1
    path = 'internal/tui/scroll.go'
    start = 102
    end = 104
    summary = 'One lock around flush: the wheel handler and the tick both reach it.'
    rationale = 'Without it a tick between two deltas …'
    author = 'claude'
    created = 2026-10-18T14:32:05Z
    lines = ['func (s *scroller) flush() {', '	s.mu.Lock()', '	defer s.mu.Unlock()']

(`gg session docs ls` lists what a directory holds; `gg session note rm
<id>` / `gg session overview rm <id>` delete a file, as does removing it
by hand.)

A review can carry an overview of its own: `/gg-review-v2 <link> overview …`
writes the same document into the review's `"overview"` field
(reviewing-with-gg's "Review document"), and gg keeps it with the review,
anchored at the reviewed change.

When the user later pastes `gg overview <id> "<title>" → <dest>` or `gg note
<id> <path>:<lines>`, they copied a reference to one of your anchors or
notes: that is the step they are asking about (`gg session overview show
<id>`, `gg session note show <id>`).

## As a worker agent

A worker started by gg (see the gg-delegate skill) works in its OWN worktree,
which the user's gg is not showing. There, "gg-overview" in a brief means
your **final `agent_report`**: gg files that report as a tour of your
worktree. Write it like the markdown `overview` of step 3 — the sections
of "What the overview says", scaled to the change;
markdown only, no JSON or annotations, and no `gg overview save` — and send it with
`agent_report {text, final: true}` (`gg agent report --final -F -`), then
end your turn with the same text as your last message — the user reads your
result in your console. `gg session note` / `gg session overview` run in your
worktree write files into YOUR worktree's session-docs directory (the
user's gg shows them in your console's view, and your own IDE window sees
them) — the report is still what the user reads; never start gg web.

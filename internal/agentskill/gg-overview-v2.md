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
gg's Commits list, the commit's `.` menu, a `└ Overview` row under the
branch while its tip is that commit, View all notes, or its
`gg://…?overview=<id>` link. Make it **TEMPORARY** (in the running gg's
memory only, gone when gg closes) only when the user says "in-memory",
"temporary" or "don't keep it" — then follow "Temporary (in-memory)
overview" below. Never turn one kind into the other.

The full command grammar is in the using-gg skill (`gg overview save` for
the permanent kind; "Temporary notes" and "Overviews" for the temporary
one); `gg skill path using-gg` prints where it is. This skill is the
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

Run `gg session status` once, in the checkout you present. It lists every
live window for this worktree: `tui:`, `web:` and `ide:` (an IDE with the
gg plugin). The `gg session …` verbs reach whichever is live WITHOUT any
flag: a TUI or a web page takes them, and when only the IDE is live the IDE
takes them. Add `--to tui|web|ide` only when several are live and the user
named one. `no gg session for this worktree` means no window at all; what
to do then is in the temporary recipe (step 3). `the IDE shows <root>, not
<worktree>` / `gg is showing worktree <a>, not <b>` means the user's window
is on another checkout: present from that checkout when the files are
there, otherwise tell the user in one line and stop.

**The window is there but did not answer.** A verb that prints `queued: no
answer from the IDE within 2s` (or `… from the TUI …`) exited 0 but shows
nothing yet: the window is live and slow, or stuck. Do not repeat the verb
(a late answer would apply it twice) and do not start gg web beside a live
window. Wait a few seconds, then check with the matching list verb — `gg
session overview list` after an overview, `gg session note list` after a
note, `gg session files` after a navigate. Listed = it landed; carry on.
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
temporary note of an in-memory one). Before you save, read each anchored
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
   its links are the anchors: `[the parser](internal/x/parse.go:40-60)`,
   `[this check](src/a.go:120)`, `[the config](config.toml)` — never
   `note:` links (a permanent overview has no temporary notes). Your line
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
   the commit cannot carry it: switch to the temporary recipe below
   without asking (the user wanted to see the result, not to commit for
   it), and say in one line of the reply that it is in memory because
   <path> is uncommitted — unless the user asked for the stored kind, then
   tell them what to commit and stop.
5. **Show it.** `gg session navigate <the printed gg://…?overview= link>`
   opens it in whichever window is live — the TUI, the web page, or the
   IDE's tool window (`opened overview <id>`). `no gg session for this
   worktree` → skip this step: the overview is stored and the user opens
   it from the commit. NEVER start gg web for a permanent overview.
6. **Reply** in chat with the link, the title and a three-line summary. Do
   not paste the overview into the chat — it is in gg.

## Temporary (in-memory) overview — only when asked

Notes and overviews made this way live in the running gg's memory and are
gone when the user closes them or quits gg.

1. **Finish first.** Notes sit on a file as it is on disk at that moment —
   make every edit before you add a single note or anchor.
2. **Work in the worktree gg shows.** `cd` to the worktree that holds the
   files you present; paths in notes and anchors are relative to it, in
   slash form.
3. **Which window?** `gg session status` ("Where it shows" above). A live
   `tui:`, `web:` or `ide:` takes the verbs below as they are. `no gg
   session for this worktree` means no window at all: tell the user so in
   one line ("gg is not running in <worktree> — starting gg web for the
   overview") and start the web page yourself, DETACHED — it runs until
   the user stops it, so never wait on it: `gg web --open` in that
   worktree, in the background, its output in a log file (`nohup gg web
   --open >"${TMPDIR:-/tmp}/gg-web.log" 2>&1 &` in sh; `Start-Process gg
   -ArgumentList 'web','--open' -WindowStyle Hidden -RedirectStandardError
   $env:TEMP\gg-web.log` in PowerShell). Run `gg session overview list`
   again until it exits 0 (a few seconds; give up after ~20 s and tell the
   user). The log's `gg web: serving <url>` line is the page — put the URL
   in your reply (step 6): the browser tab may still be loading when you
   add the overview (`no gg web tab is open to show it`), and the page
   lists it once open. Never start the TUI yourself — it needs the user's
   terminal — and never start gg web while an IDE is live.
4. **Notes on the lines.** `gg session note add <path>:<start>[-<end>]
   --summary "…" [--rationale "…"] --author "<your name>"` — one per remark,
   on the exact lines it is about. Summary: one sentence; reasoning goes in
   `--rationale`. Several short notes beat one long one. A note may carry
   `--meta k=v` and `--display <json>` (reviewing-with-gg's "Meta and
   display": `kind`, `intent`; meta never triggers anything). Each answers
   `noted … as t<n>` — keep the ids for step 5.
5. **The overview.** The walkthrough of "What the overview says" above, as
   markdown; its links are the anchors: `[the parser](internal/x/parse.go:40-60)`,
   `[this check](src/a.go:120)`, `[why this lock](note:t7)` (opens the file
   at your note), `[the config](config.toml)`. The TUI and gg web show an
   overview beside the file each anchor points to (↑/↓ walk the anchors and
   the file follows, D flips the file to the diff, a note anchor shows your
   note, tab switches sides; a narrow window shows it full-screen, where tab
   walks them); the IDE's tool window walks them with Next anchor.
   Add it: `gg session overview add --title "<short title>" --file <md>` (or
   the markdown on stdin). It prints the overview's id `f<n>`; each
   `unresolved: <dest>` line is an anchor whose file or note was not found —
   fix the text and run `gg session overview set f<n> --file <md>`. When it
   ends `added f<n> in the background (<why>)`, the user was busy: say so,
   they open it with tab or you bring it up later with `gg session files
   focus f<n>`.
6. **Reply** in chat with the overview's id and title (and the gg web URL
   when you started it) and a three-line summary. Do not paste the overview
   into the chat — it is in gg (in an IDE: the plugin's tool window lists it
   under "This session").

A review can carry an overview of its own: `/gg-review-v2 <link> overview …`
writes the same document into the review's `"overview"` field
(reviewing-with-gg's "Review document"), and gg keeps it with the review,
anchored at the reviewed change.

When the user later pastes `gg overview f<n> "<title>" → <dest>` or `gg note
t<n> <path>:<lines>`, they copied a reference to one of your anchors or
notes: that is the step they are asking about (`gg session overview show
f<n>`, `gg session note show t<n>`).

## As a worker agent

A worker started by gg (see the gg-delegate skill) works in its OWN worktree,
which the user's gg is not showing. There, "gg-overview" in a brief means
your **final `agent_report`**: gg files that report as a tour of your
worktree. Write it like the markdown `overview` of step 3 — the sections
of "What the overview says", scaled to the change;
markdown only, no JSON or annotations, and no `gg overview save` — and send it with
`agent_report {text, final: true}` (`gg agent report --final -F -`), then
end your turn with the same text as your last message — the user reads your
result in your console. Do NOT run `gg session note` / `gg session overview`
and do not start gg web: they target the user's window, not your worktree.

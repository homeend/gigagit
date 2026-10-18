# gg-overview — present a result in gg

**gg-overview** is gg's way of showing the user what you found or changed:
an **overview** — a short markdown document whose links are anchors into the
code — with your remarks on the exact lines. The user reads the
overview in their gg window, tabs from anchor
to anchor, presses enter to open the real file at the place you named, and
backspace brings them back. When the user says "use gg-overview", "show it
as a gg-overview", "visualise the result in gg" or types `/gg-overview
[what to present]`, this is what they mean: present the result this way
instead of (not as well as) a long chat answer. (A worker started by gg:
see "As a worker agent" at the end.)

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

## What the overview says — a walkthrough, never a file list

An overview explains how something WORKS; a list of files with a sentence
each is not one. Write it in the order you would explain it to a colleague
at a whiteboard, and let the anchors follow that order — the reader walks
them top to bottom, so the walk IS the explanation:

1. First line: what the change (or the code) makes possible, in one
   sentence.
2. **How it works**: one real run through the code, step by step, in
   execution order, crossing files as the control flow does — the trigger
   (key, command, event, call), each decision point, the data handed on,
   where the result lands (screen, clipboard, store). One sentence per
   step, with ONE anchor on the lines where that step happens.
3. **Why this shape**: the two or three design decisions and what each
   avoids, anchored.
4. **Edges**: failure and the unusual cases, anchored to their branches.
5. **Not covered**: what you skipped, what you could not confirm (for a
   commit: claims of the commit message you did not find in the code),
   open questions, what the user should do next.

Never group by file: a file may appear five times, in the order the flow
reaches it. Prefer several precise anchors (`path:N`, `path:N-M`) over one
wide range; anchor the lines that SHOW the point, not a neighbour of them
(files with the same content, such as one script copied into sibling
directories, look identical in the viewer: anchor the file that differs).
Read the code before you write an anchor: for a commit, `gg show <sha>
--patch` and the surrounding functions. Each anchored place also gets a
short remark that says what that line contributes to the flow (the
`annotations` of a permanent overview, a temporary note of an in-memory
one). Limits: ≤ 64 KiB, ≤ 100 anchors.

## Steps (permanent — the default)

1. **Finish first.** The overview explains a commit: make every edit and
   commit it before you write a single anchor.
2. **Work in the worktree whose commit you explain.** Paths are relative to
   it, in slash form; lines are the file AT that commit (`gg show
   <sha>:<path>` when the tree has uncommitted changes).
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

A review can carry an overview of its own: `/gg-review <link> overview …`
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
worktree. Write it like the markdown `overview` of step 3 — the
one-sentence first line, then anchored points, most important first;
markdown only, no JSON or annotations, and no `gg overview save` — and send it with
`agent_report {text, final: true}` (`gg agent report --final -F -`), then
end your turn with the same text as your last message — the user reads your
result in your console. Do NOT run `gg session note` / `gg session overview`
and do not start gg web: they target the user's window, not your worktree.

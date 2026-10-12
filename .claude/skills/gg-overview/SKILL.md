---
name: gg-overview
description: Use when the user says gg-overview, or asks to visualise, present or walk them through a result in gg — temporary notes on the exact lines plus an overview whose links open the code.
argument-hint: "[what to present]"
---

<!-- gg:gg-overview:v8 -->

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

## Steps (permanent — the default)

1. **Finish first.** The overview explains a commit: make every edit and
   commit it before you write a single anchor.
2. **Work in the worktree whose commit you explain.** Paths are relative to
   it, in slash form; lines are the file AT that commit (`gg show
   <sha>:<path>` when the tree has uncommitted changes).
3. **Write the document** as JSON:
   `{"version":1,"title":"<short title>","overview":"<markdown>","files":[{"path":"…","annotations":[{"newRange":[a,b],"summary":"…","rationale":"…"}]}]}`.
   The markdown is written in the order you would explain it — the anchors
   are walked top to bottom:
   - first line: the result in one sentence;
   - then each point with its anchor: `[the parser](internal/x/parse.go:40-60)`,
     `[this check](src/a.go:120)`, `[the config](config.toml)` — never
     `note:` links (a permanent overview has no temporary notes);
   - one `path:N` or `path:N-M` anchor per place you want seen — gg draws
     every anchor in a file as a band and `n` / `p` step through them, so
     prefer several precise anchors over one wide range;
   - anchor the lines that SHOW the point, not a neighbour of them;
   - last: what you skipped, open questions, what the user should do next.
   Your line remarks go in `files[].annotations` (one per remark, on the
   exact lines; summary one sentence, reasoning in `rationale`): gg draws
   them as read-only boxes beside the file, only in this overview.
   Limits: ≤ 64 KiB, ≤ 100 anchors.
4. **Save it.** `gg overview save [<gg-link>] --agent "<your name>" --stdin`
   (the JSON on stdin). No link = HEAD; a pull request: the link from `gg
   link --pr <n>`. gg checks the WHOLE document against the commit and
   refuses the save — nothing stored, every problem listed — for a path the
   commit lacks, lines past a file's end or a `note:` link: fix the
   document and save again (the same commit and title replaces). On
   `<path>: uncommitted changes` or `<path>: differs from <sha7> on disk`
   STOP and ask the user: commit those files and save permanently, or keep
   the overview in memory only (then the temporary recipe below).
5. **Show it.** When a gg window is open on the worktree, `gg session
   navigate <the printed gg://…?overview= link>` opens it there. `no gg
   session for this worktree` → skip this step: the overview is stored and
   the user opens it from the commit. NEVER start gg web for a permanent
   overview.
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
3. **Is gg running there?** `gg session overview list`. Exit 1 with `no gg
   session for this worktree` means neither a gg TUI nor a gg web page is
   open on it. Then tell the user so in one line ("gg is not running in
   <worktree> — starting gg web for the overview") and start the web page
   yourself, DETACHED — it runs until the user stops it, so never wait on
   it: `gg web --open` in that worktree, in the background, its output in a
   log file (`nohup gg web --open >"${TMPDIR:-/tmp}/gg-web.log" 2>&1 &` in sh;
   `Start-Process gg -ArgumentList 'web','--open' -WindowStyle Hidden
   -RedirectStandardError $env:TEMP\gg-web.log` in PowerShell). Run `gg
   session overview list` again until it exits 0 (a few seconds; give up
   after ~20 s and tell the user). The log's `gg web: serving <url>` line is
   the page — put the URL in your reply (step 6): the browser tab may still
   be loading when you add the overview (`no gg web tab is open to show
   it`), and the page lists it once open. Never start the TUI yourself — it
   needs the user's terminal. `gg is showing worktree <a>, not <b>` means the user's gg
   is on another checkout: tell the user and ask them to switch, or present
   from <a> if that is where the files are.
4. **Notes on the lines.** `gg session note add <path>:<start>[-<end>]
   --summary "…" [--rationale "…"] --author "<your name>"` — one per remark,
   on the exact lines it is about. Summary: one sentence; reasoning goes in
   `--rationale`. Several short notes beat one long one. Each answers `noted
   … as t<n>` — keep the ids for step 5.
5. **The overview.** Markdown, written in the order you would explain it —
   the anchors are walked top to bottom (the TUI and gg web show an overview
   beside the file each anchor points to: ↑/↓ walk the anchors and the file follows,
   D flips the file to the review's diff of it (HEAD → disk for yours), a note
   anchor shows your note, tab switches sides; a narrow terminal or window
   shows it full-screen, where tab walks them):
   - first line: the result in one sentence;
   - then each point with its anchor: `[the parser](internal/x/parse.go:40-60)`,
     `[this check](src/a.go:120)`, `[why this lock](note:t7)` (opens the file
     at your note), `[the config](config.toml)`;
   - one `path:N` or `path:N-M` anchor per place you want seen — gg draws
     every anchor in a file as a band and `n` / `p` step through them, so
     prefer several precise anchors over one wide range;
   - anchor the lines that SHOW the point, not a neighbour of them: files
     with the same content (one script copied into sibling directories)
     look identical in the viewer, so anchor the file that differs (each
     directory's config, not the shared script);
   - last: what you skipped, open questions, what the user should do next.
   Limits: ≤ 64 KiB, ≤ 100 anchors.
   Add it: `gg session overview add --title "<short title>" --file <md>` (or
   the markdown on stdin). It prints the overview's id `f<n>`; each
   `unresolved: <dest>` line is an anchor whose file or note was not found —
   fix the text and run `gg session overview set f<n> --file <md>`. When it
   ends `added f<n> in the background (<why>)`, the user was busy: say so,
   they open it with tab or you bring it up later with `gg session files
   focus f<n>`.
6. **Reply** in chat with the overview's id and title (and the gg web URL
   when you started it) and a three-line summary. Do not paste the overview into the chat — it is in gg.

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

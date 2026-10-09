---
name: gg-overview
description: Use when the user says gg-overview, or asks to visualise, present or walk them through a result in gg — temporary notes on the exact lines plus an overview whose links open the code.
argument-hint: "[what to present]"
---

<!-- gg:gg-overview:v1 -->

# gg-overview — present a result in gg

**gg-overview** is gg's way of showing the user what you found or changed:
temporary **notes** on the exact lines, plus an **overview** — a short
markdown document whose links are anchors into the code. The user reads the
overview in their gg window (the TUI, or the gg web page), tabs from anchor
to anchor, presses enter to open the real file at the place you named, and
backspace brings them back. When the user says "use gg-overview", "show it
as a gg-overview", "visualise the result in gg" or types `/gg-overview
[what to present]`, this is what they mean: present the result this way
instead of (not as well as) a long chat answer.

The full command grammar is in the using-gg skill ("Open-file notes" and
"Overviews"); `gg skill path using-gg` prints where it is. This skill is the
recipe.

## Steps

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
   tab walks the anchors top to bottom:
   - first line: the result in one sentence;
   - then each point with its anchor: `[the parser](internal/x/parse.go:40-60)`,
     `[this check](src/a.go:120)`, `[why this lock](note:t7)` (opens the file
     at your note), `[the config](config.toml)`;
   - one `path:N` or `path:N-M` anchor per place you want seen — gg draws
     every anchor in a file as a band and `n` / `p` step through them, so
     prefer several precise anchors over one wide range;
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

Notes and overviews are TEMPORARY: they live in the running gg's memory and
are gone when the user closes them or quits gg. If the user wants the result
kept, store a review instead (reviewing-with-gg's `gg review save`, or `gg
note` review notes).

When the user later pastes `gg overview f<n> "<title>" → <dest>` or `gg note
t<n> <path>:<lines>`, they copied a reference to one of your anchors or
notes: that is the step they are asking about (`gg session overview show
f<n>`, `gg session note show t<n>`).

## As a worker agent

A worker started by gg (see the gg-delegate skill) works in its OWN worktree,
which the user's gg is not showing. There, "gg-overview" in a brief means
your **final `agent_report`**: gg files that report as a tour of your
worktree. Write it exactly like the overview in step 5 — the one-sentence
first line, then anchored points, most important first — and send it with
`agent_report {text, final: true}` (`gg agent report --final -F -`). Do NOT
run `gg session note` / `gg session overview` and do not start gg web: they
target the user's window, not your worktree.

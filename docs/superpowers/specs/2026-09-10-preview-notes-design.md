# Notes in merge previews, preview links, `gg open` (design, approved 2026-09-10)

The goal: an agent refactors on a branch, opens a merge preview of that
branch onto its target, annotates the changes it made with notes ("what was
added and why"), and hands the user one link. The user opens the link and
reads the notes inside the preview, replies, steers the agent. Every leg
exists today in isolation — merge previews (`Previews` tab, `gg preview`),
review notes (`gg note`, the diff views, the `reviewing-with-gg` skill) and
`gg://` links — and none of them meet: a preview opens compare-style diff
views that carry no note address, the link grammar has no preview form,
steering cannot navigate into a preview, and the skills do not teach
previews.

Decided in chat 2026-09-10 (do not re-ask):

1. **Anchoring is GitHub-style, not hunk-style.** hunk keeps notes only in
   the live session, keys files by path with no content hash, and on reload
   lets a note drift by line number (its anchor resolver is "permissive": a
   line the patch no longer shows is hung from the nearest hunk; nothing is
   ever marked outdated). GitHub anchors a comment to a commit and marks it
   **Outdated** when later commits change those lines. gg already has the
   GitHub model (address + side + range + `context_hash`, resolved to
   active / moved / stale / orphaned), so previews reuse it unchanged.
2. **Agents address a preview, never a sha**: one `--preview` flag on the
   note-taking verbs; gg resolves the source tip at write time.
3. **Preview links spell the branch pair**, git's three-dot form:
   `gg://<repo>@<target>...<source>`. Names travel between machines; the
   machine-local preview id does not.
4. **A link is opened with `gg open <link>`**: it steers a live gg session in
   the resolved checkout, or starts the TUI there positioned on the link. A
   paste field inside the TUI is a follow-up.
5. **The web frontend is in scope** for display, add and reply in previews
   (feature A). Web link production and `gg open` steering a web page are
   follow-ups.

The work is two features on two branches: **A** (notes in previews) needs
nothing new from links; **B** (preview links, steering, `gg open`) builds on
the merged `gg://` work (`docs/superpowers/specs/2026-09-10-gg-links-design.md`).

## 1. Feature A — notes in merge previews

### 1.1 What a preview note is

A preview of `source → target` shows `git diff target...source`: merge base
on the old side, the source tip on the new side. The new side is byte for
byte the file at the source tip, so a note on it is an ordinary committed
note: `FileAddress{State: StateCommitted, Commit: <source tip full sha>,
Path}`, `Side: NoteSideNew`. No new note kind, no new store field, and the
same note is visible in the source tip's own commit view.

The old side (the merge base) is not note-addressable in a preview: it is
not one of §4.4's addressable old sides, exactly as `gg review A..B` rules
today (`reviewImportTarget`: ranges anchor to the tip, new side only). A
`c` / `gg note add --preview` on an old-side line is refused with
`notes in a preview anchor on the new side` (TUI notice, CLI exit 2).

### 1.2 Resolution: gathering and outdated notes

When the agent pushes more commits the tip moves and notes made on the old
tip would vanish from the preview. So the preview does not read the tip's
notes only; it gathers them along the branch:

```go
// internal/domain
type PreviewNoteSet struct {
    Tip     string                       // full sha of the source tip (write target)
    Commits []string                     // merge-base..source, newest first
}
func (s *Service) PreviewNotes(ctx, source, target string) (PreviewNoteSet, error)
func (s *Service) PreviewNotesFor(ctx, set PreviewNoteSet, path string, d Diff) ([]ResolvedNote, error)
func (s *Service) PreviewNoteCounts(ctx, set PreviewNoteSet) (byPath map[string]int, total int, err error)
```

- `PreviewNotes` reuses `PreviewSummary`'s cached base/tip pair and lists
  `git rev-list <base>..<source>` (first-parent is NOT used: a note on a
  merged side branch still belongs to the preview). The list is cached with
  the summary, keyed on `(srcHash, tgtHash)`.
- `PreviewNotesFor` loads every root note whose address is
  `(StateCommitted, c, path)` for `c ∈ Commits` and resolves each against
  the preview diff's NEW side by the existing fingerprint rule
  (`resolveNote`): active (range unchanged), moved (found elsewhere), stale,
  orphaned. Replies follow their root. A note whose commit is the tip is
  resolved exactly as today; a note from an older commit is resolved
  against the tip's content, so "the lines changed since" is what makes it
  stale.
- **Outdated** is the preview's name for stale: the note stays listed,
  greyed, with an explicit marker `⊘` in the note gutter and `outdated` in
  the note popup header, because in a preview it is the expected case
  rather than an edge case. Orphaned (path gone from the preview) is hidden
  from the diff but still counted in `PreviewNoteCounts.total` and shown in
  the Previews panel row, like hunk's "retired file" count. A note whose
  commit is no longer in `merge-base..source` (the branch was rebased) cannot
  be attributed to the preview at all and is neither shown nor counted; it
  stays reachable in that commit's own view (ruling 2026-09-15). `model.NoteStatus`
  gains no value: the TUI/web map `NoteStale` to the outdated rendering
  when the view is a preview.
- Old-side notes on those commits (a note the user made on the old side of
  some commit's own diff) are ignored by the preview; they are not part of
  the merge-base → tip picture.

### 1.3 TUI

- **Previews panel** row: a `✎ N` badge (total root notes, including hidden
  ones) after the existing summary cell, painted from `PreviewNoteCounts`
  fetched with the summary (`srcPreviews` refresh; `srcNotes` also triggers
  a badge refresh for the open preview).
- **Preview file list** (`openCompareFiles` reached through
  `handlePreviewOpenMsg`): per-file note counts from `byPath`, same badge
  the commit files view paints from `NoteCounts.ByCommitPath`. The files
  view gains a `previewSet *domain.PreviewNoteSet` (nil = not a preview).
- **Preview diff view**: `noteAddr = FileAddress{StateCommitted, Tip, Path}`
  and a `previewSet` pointer; `NotesFor` is replaced by `PreviewNotesFor`
  when the set is non-nil. All note keys work unchanged: `c` add (new side
  only — old side refused with the notice above), `E` edit, `R` reply, `a`
  toggle, `}`/`{` next/prev, `.`-menu rows "List notes…" / "Remove all
  notes…" (the latter clears notes on the tip only, and says so:
  `Remove all notes on <tip short> (N of M)`). `L` copy link is unchanged in
  feature A (it copies the tip's commit link, which is valid) and switches
  to the preview form in feature B.
- Live steering `reload notes` re-resolves through the same path; attention
  marks are keyed by file+target where target = the tip commit.

### 1.4 Web

- `GET /api/preview` rows gain `notes` (total). The preview stage's file
  rows carry per-file counts; diff rows show notes through the existing
  note renderer with an `outdated` class on stale ones. Data comes from a
  new `GET /api/preview/notes?source=&target=&path=` (resolved notes for one
  file) and the counts riding on the existing preview payload.
- Add/reply/edit/delete from a preview diff post to the existing note
  endpoints with `target: {state: "commit", commit: <tip>}` — the page
  learns the tip from the preview payload; no new mutation endpoint. Old
  side: the add control is not offered.
- Live refresh: the notes SSE source already re-fetches the open file's
  notes; the preview stage subscribes the same way.

### 1.5 CLI, MCP, skill

One shared target flag, parsed by one helper (`internal/cli/previewflag.go`):

```text
--preview <id | label | <target>...<source>>
```

`PreviewGet` already resolves id-or-label; the three-dot form is parsed
into (source, target) and resolved through `PreviewSummary` without needing
a saved preview. The flag is refused together with `--rev`, `--cached` or a
`gg://` positional (exit 2, "one target only").

- `gg diff --preview P [--hunks [--json]] [-- <paths>]` — the preview diff
  (`target...source`); `--hunks` numbers hunks in git `@@` order like today.
- `gg preview diff <id|label> --hunks [--json]` — the same, for symmetry.
- `gg note add --preview P --file F (--new-line N | --hunk H) --summary …` —
  stored on the tip; `--old-line` refused (exit 2). `gg note list --preview P
  [--file F] [--json]` — the gathered, resolved set; `status` reports
  `outdated` for stale. `gg note apply --preview P --stdin` — batch import
  onto the tip, new side only (old-side items skipped with a warning, the
  `--working` rule).
- `gg review --preview P [--notes]` — the range review `target...source`
  with the range rule (tip, new side).
- MCP: `gg_notes_list`, `gg_note_add`, `gg_notes_apply` accept
  `preview: "<target>...<source>"` in place of `commit`.
- `using-gg.md`: a "Merge previews" paragraph (`gg preview add <source>
  <target>`, `gg diff --preview`, `gg note … --preview`).
  `reviewing-with-gg.md`: the workflow paragraph — "when you changed code
  on a branch, open a preview onto its target (`gg preview add`), annotate
  every change you made with `gg note add --preview … --summary … --rationale
  …`, and report the preview's name (feature B: its link) to the user".
  `Version` and `ReviewVersion` bump; `gg init --update` refreshes copies.

### 1.6 Testing (A)

- Domain: a repo with `main` and `feat` (three commits, one touching a
  file twice): notes on the first commit resolve active on the tip when the
  lines survived, outdated when the second commit changed them, orphaned
  when `feat` is rebased so the commit is gone; counts include hidden ones;
  the rev-list cache follows the summary cache (a moved tip invalidates).
- CLI: `gg note add --preview` stores on the tip (`gg note list --rev <tip>`
  shows it); `--old-line` exit 2; `--preview` + `--rev` exit 2; the
  three-dot form without a saved preview; `gg note list --preview` reports
  `outdated`; `gg diff --preview --hunks --json`; `gg review --preview
  --notes` imports onto the tip.
- TUI: preview diff shows a tip note and an older-commit note (one active,
  one outdated with `⊘`); `c` on the old side → notice; badges in the
  Previews panel and file list; `}`/`{` skip hidden notes.
- Web: `/api/preview/notes` unit test through the server; JS source
  assertion for the outdated class; a CDP probe adding a note from a
  preview diff row and seeing it on the tip commit's view.
- e2e: `s91_preview_notes.toml` — `gg preview add`, `gg note add
  --preview`, `gg note list --preview`, a further commit, `outdated`.

## 2. Feature B — preview links, steering, `gg open`

### 2.1 Grammar

```text
gg://<repo>@<target>...<source>                       the preview (Previews tab)
gg://<repo>/<path>@<target>...<source>[:<line>]       a file / new-side line in it
gg://<repo>/<path>@<target>...<source>#<hunk>         a hunk in it
```

`<target>` and `<source>` are branch names (local or remote-tracking:
`origin/main...origin/feat/login`), never shas. Git ref names cannot contain
`:`, `@{`, `#` is not allowed either, and the parser already reads the FIRST
`@` after the repo and the LAST `:` — so branch names with `/` and `.` parse
cleanly. `:old:<n>` is refused on a preview link (old side not addressable).
`model.LinkTarget` gains `Preview *LinkPreview{Source, Target string}`,
set iff the target text contains `...`; `Link.String()` writes the pair
verbatim; `Link.Address()` is not enough for a preview — callers use
`domain.ResolveLink`, whose `Resolved` gains `Preview *PreviewNoteSet`
(the tip resolved on the chosen checkout) and fills `Addr` with the tip
commit address.

### 2.2 Producers

- TUI: Previews panel row `.`-menu "Copy link" (the preview link); preview
  diff view `L` and "Copy link" produce the file/line form; `cursor.link`
  in the session snapshot is the preview form whenever a preview is open
  (`gg session status` prints it; `gg_ui_state` exposes it).
- CLI: `gg link --preview P [<path>[:<line>]]`.

### 2.3 Consumers

- Every link-taking verb (`gg diff`, `gg show` → refused for a preview link
  with "use gg diff", `gg note add|list|apply|clear`, `gg session navigate`,
  `gg session highlight add`, `gg link resolve`) accepts the preview form and
  behaves as with `--preview`.
- `gg session navigate <preview link>`: the steer wire gains
  `target.state = "preview"` with `source`/`target` names; the TUI opens the
  Previews tab entry (creating a show-once preview when none is saved),
  then the file and line; the web page switches to the preview stage the
  same way. Refusals and the parked-navigate TTL are unchanged. Attention
  marks key on the tip commit, so `highlight add` works in a preview.
- **`gg open <link>`** (`internal/cli/open.go`): resolve the link (repo →
  checkout, as `gg link resolve`); if `steer.Live` reports a TUI or web
  session in that checkout, post the corresponding `navigate` (wait ≤2 s,
  `--no-wait`) and print `steered: <checkout>`; otherwise `chdir` to the
  checkout and launch the TUI in-process with a startup navigate (a new
  `--at <link>` flag on the TUI entry, consumed once the first snapshot is
  loaded, same pipeline as a steer navigate). Exit 1 on an unresolvable
  link. `gg open` targets the TUI only; a web page is reached through
  `gg session navigate` as before (a web-only `gg open` is a follow-up).

### 2.4 Skill text

`using-gg.md`: the preview link rows join the link grammar block; "`gg open
<link>` shows it to the user in their gg". `reviewing-with-gg.md`: the
workflow's last step becomes "print `gg link --preview <P>` and hand that
link back". Version bumps.

### 2.5 Testing (B)

- `ParseLink`/`String` round trips for the three preview rows, remote
  names, refusal of `:old:` and of a sha-looking pair.
- `ResolveLink` on a two-checkout registry: the preview resolves on the
  checkout that has both branches; ambiguity/unknown as today.
- CLI: each consumer with a preview link vs `--preview` produces the same
  stored note / diff / posted steer command; `gg open` against a fake steer
  inbox posts the navigate; against no session, the launch path is
  unit-tested through the `--at` startup navigate (tui test: a Model with
  `startAt` opens the preview after `dataLoadedMsg`).
- TUI/web: copy producers through the clipboard seam; snapshot
  `cursor.link` canonical; navigate into a preview via the steer inbox.
- e2e: `s92_preview_links.toml` — `gg link --preview`, `gg note add <link>`,
  `gg link resolve --json`.

## 3. Out of scope

Old-side (merge-base) notes in previews; a per-preview top-level summary
note; a TUI paste field for links; web-side link production and `gg open`
targeting a web page (both on the todo list); registering a `gg://` OS URL
handler.

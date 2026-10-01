# Range links — design

Date: 2026-10-02 · Branch: `feat/range-links` · Status: awaiting review

## Purpose

Mark several lines in the terminal — in a diff (either side) or in a file
view — and copy one `gg://` link to them. The main use is pasting the link to
an agent: "why are these changes here", "explain this part". Opening the link
restores what was marked: the same lines, on the same side, in the same kind
of view.

An agent turns the link into text with a new verb, `gg link text <link>`.

## Rulings (user, 2026-10-02)

1. The agent reads the lines through `gg link text <link>` (plus an MCP twin).
2. Terminal first. The web page is a follow-up; this round it only lands on
   the first line and names the range.
3. An uncommitted range carries ONE fingerprint of the whole marked block.
4. A range whose block no longer matches is INVALID. gg says so and does not
   guess: no search for where the block went, no landing on the old numbers.
5. `gg note add <range link>` writes a note that sits under the LAST line,
   keeps the range in the note, and the view draws the range above the note.

## 1. Grammar (`internal/model`)

```
gg://repo/path@<target>:<a>-<b>            new side, lines a..b
gg://repo/path@<target>:old:<a>-<b>        old side
gg://repo/path:<a>-<b>~<8hex>              uncommitted, with a block fingerprint
```

- `Link` gains `End int` (0 = a single line). `Line` stays the first line.
- `b < a`, `a < 1` are parse errors. `a == b` IS a single-line link: it parses
  to `End == 0`, prints as `:<a>`, and keeps today's single-line behaviour
  (section 3).
- A range and `#<hunk>` in one link is a parse error, as a line and a hunk
  already are.
- `splitLinkLine` is the one place that reads the suffix (it owns the Windows
  drive-colon rule); it returns the end as well.
- `Link.String()` writes `:<a>-<b>`, then `~<fp>` when set.

**Block fingerprint** (`model.BlockFingerprint(lines []string) string`): FNV-1a
32-bit over the lines, each trimmed of leading and trailing whitespace, joined
with `\n`; 8 lowercase hex. It fills the same `~<8hex>` slot, so
`LinkFingerprintOK` is unchanged. A block whose lines are all blank after
trimming has no fingerprint — the link is then the plain range. As today, a
fingerprint on a committed target is a parse error: a commit already names
fixed content.

## 2. Resolving a range (`internal/domain`)

`Resolved` gains `End int`. All consumers go through `domain.ResolveLink`, so
the rule below reaches the `#` paste prompt, `gg open`, `gg session navigate`,
`gg session highlight add`, `gg note add`, `gg link resolve`, `gg link text`
and the MCP tools at once.

| Link | What the resolver does |
|---|---|
| committed target (sha, `@a..b`, preview) | nothing to verify; `End` is passed through |
| uncommitted, no fingerprint | passed through unverified |
| uncommitted, fingerprint | read the side's text (`linkSideLines`), hash lines `a..b`, compare |

For a fingerprinted range:

- **match** → resolved, `Anchor.State = "same"`.
- **no match**, a range past the end of the file, or a file that cannot be
  read (deleted, binary, over the diff size cap) → `ResolveLink` returns the
  new error `domain.ErrLinkStale`, with the message
  `the link is no longer valid: lines <a>-<b> of <path> have changed since it was copied`.

There is no "moved" state for a range.

## 3. Single lines are unchanged

A one-line link (`:<n>~<fp>`, including a selection of one line) keeps the
behaviour merged on 2026-10-01: it follows the text when it moved, warns when
it is gone, and never refuses. This spec does not change that. (If the strict
rule should apply to single lines too, that is a separate, small change.)

## 4. Copying a range link in the TUI

With a line selection active (space, move, optionally space again), `L` and
the menu row copy the RANGE link. The row reads
"Copy link to selected lines (N)". Without a selection both do what they do
today. Copying the link keeps the selection (esc clears it), so the text can
still be copied with enter.

Hosts:

- **Diff view, single file and stacked.** The side is the cursor's side. The
  range is the first and the last selected row that has a line number on that
  side; gap rows at the edges are trimmed. A selection with no numbered row on
  that side copies nothing and says "nothing to link on this side". The target
  is whatever a single-line link from that view names today (working tree,
  index, a commit, a commit pair `@a..b`, a merge preview — whose old side
  still has no link).
- **Stacked view:** a selection that crosses a file boundary copies nothing
  and says "a link marks lines of one file".
- **File viewer and View-file preview.** The range is the selected file lines;
  the link is the content link (`?view=content`) with `:<a>-<b>`.
- **Blame** stays without links.

An uncommitted range gets its fingerprint from the text of the side it names,
through one domain call (`Service.LinkBlockFingerprint(ctx, link)`), which
reads with `linkSideLines` — the same reader the resolver uses, so producer
and resolver cannot disagree about line endings or what is text.

The copy confirmation and the link history (`gg links`) work as for any link.

## 5. Opening a range link in the TUI

- `steer.Line` gains `End int` (`json:"end,omitempty"`); `linknav.Command`
  and `linknav.AtLink` carry it.
- `navigateLanded` — the one landing funnel — restores the selection on the
  host the link opens: rows from the one holding line `a` to the one holding
  line `b` on the link's side, frozen (as after the second space), cursor on
  the first line, in the old pane for `:old:`. Folded context that hides part
  of the range is opened.
- The notice reads `opened <path>:<a>-<b>`.
- A frozen selection means the cursor can move without losing the marks; esc
  clears it; enter copies the text; `L` copies the same link again.
- A stale range (section 2) opens nothing. The notice shows the resolver's
  message, translated. This holds for the `#` prompt, `gg open` and
  `gg session navigate` alike; the CLI verbs print the message and exit 1.

## 6. `gg link text <link>` and `gg_link_text`

Prints exactly the lines a link names.

- Accepts a link with a line or a range. A link with only a file, or a
  `#<hunk>`, is a usage error this round (exit 2).
- Text source by target: working tree / index / HEAD through `linkSideLines`;
  a commit's file at that sha (`:old:` = its parent's); a pair's `a` side for
  `:old:` and `b` side otherwise; a merge preview's source tip (new side
  only).
- Output: one header line, then one line per source line, `<no>\t<text>`:

  ```
  internal/x.go @ working tree (new), lines 33-41
  33	func f() {
  …
  ```

- `--json`: `{"path","target","side","start","end","lines":[…]}`.
- A stale range prints the resolver's error and no text (exit 1). A committed
  range past the end of the file is an error naming the file's line count.
- A single fingerprinted line that moved prints the line from where it is now
  and the existing note on stderr.
- MCP twin `gg_link_text` returns the JSON shape.

## 7. The rest of the CLI and MCP surface

- `gg link <path>:<a>-<b>` (with the existing side/target flags) builds a
  range link, fingerprinted when uncommitted unless `--no-fingerprint`.
- `gg link resolve --json` and `gg_link_resolve` gain `end_line`.
- `gg diff <link>`, `gg show <link>`: unchanged output; a stale range is the
  error.
- `gg session highlight add <link>` bands `a..b`. The CLI's private
  `splitLinkRange` shim is deleted — the grammar now owns `-<end>` (the shim
  also breaks on `~<fp>`). `--end` together with a range link stays a usage
  error.
- `gg session navigate <link>` and `gg open <link>` land with the selection
  (section 5).

## 8. Notes on a range

The store already has what ruling 5 asks for: `model.Note.Range` is a
`[first, last]` pair, the context hash already covers the whole range, and the
diff view already places a note under `Range[1]`, the last line.

- `gg note add <range link>` writes `Range = [a, b]` (today a link always
  gives `[line, line]`). MCP note-add through a link likewise.
- **New drawing:** a note whose range spans more than one line marks rows
  `Range[0]..Range[1]` on the note's side with a thin bar in the gutter, in
  the note's colour, so the marked area sits directly above the note. This
  applies to every ranged note, including the hunk notes agents already
  write, in the single and the stacked diff. It is a gutter bar, not the
  selection background, so it is never mistaken for a live selection.
- Out of scope: writing a ranged note from the TUI's own note key with a
  selection active, and the web's band — both follow-ups.

## 9. Web (this round)

- The server-side resolver handles ranges, so a pasted or steered range link
  lands on the first line and the op line names the range
  (`opened <path>:<a>-<b>`). A stale range shows the error.
- `links.js` must parse and re-print a range without mangling it.
- No selection restore and no range copy in the browser: the follow-up.

## 10. Errors and edges

- Range past the end, uncommitted + fingerprint → stale. Unfingerprinted or
  committed → the TUI lands and clamps the selection to the file's end;
  `gg link text` errors.
- CRLF and a trailing newline are normalised by `linkSideLines`; trimming
  makes re-indentation not a change, as for single lines.
- A selection across folded context links first..last; the hidden lines are
  part of the range (and of the fingerprint).
- A 32-bit hash can collide; a collision makes a changed block read as valid.
  Accepted, as for single lines.

## 11. Testing

- `model`: parse/print round trips (`:a-b`, `:old:a-b`, `~fp`, `a==b`
  collapse, `b<a`, range + hunk, fingerprint on a commit, Windows drive path).
- `domain`: block match, edit inside the block, block moved (stale, not
  followed), range past EOF, unreadable file, staged old side, all-blank block.
- `cli`: `gg link` range producer, `link resolve --json` `end_line`,
  `link text` for each target kind and the stale error, `note add` range,
  `highlight add` range with and without `~fp`.
- `tui`: `L` with a selection in the diff (both sides, stacked, cross-file
  refusal, gap trimming), file viewer and preview; landing restores the frozen
  selection on the right side; stale notice; the note gutter bar.
- e2e: one scenario — copy a range link, `gg link text` it, edit a line
  inside, see the stale error — plus a golden screen of a landed range.
- i18n: every new TUI string in the four bundles.

## 12. Docs

`CHANGELOG.md`, `docs/CLAUDE-details.md` (link grammar), the `model` row in
`CLAUDE.md`, `internal/agentskill/using-gg.md` (+ version bump), `README.md`
where links are described.

## Calls of mine to check

- A one-line selection keeps the lenient single-line rule (section 3).
- A stale range opens nothing at all, rather than opening the file unmarked.
- The note band is a gutter bar and is always drawn for every ranged note.
- `L` keeps the selection after copying.
- A cross-file stacked selection refuses instead of linking the cursor's file.

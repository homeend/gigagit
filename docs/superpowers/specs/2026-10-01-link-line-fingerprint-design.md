# Line fingerprints on uncommitted `gg://` links — design

Date: 2026-10-01 · Status: awaiting review

## 1. Why

A link to a committed line (`@<sha>`, `@<a>..<b>`) names fixed content. A link
to an uncommitted line does not: `gg://repo/path:33` and
`gg://repo/path@staged:33` mean "line 33 of whatever is there when the link is
opened". The user copies such a link, pastes it to an agent ("why did you
change this?"), and by the time the agent opens it the file has been edited
and line 33 is something else. Nothing says so.

The user's ruling (2026-10-01): **follow the text**. The link carries a
fingerprint of the line; gg re-finds the line if it moved and says when it
has changed. No content is stored. A link cannot show what a line used to
say — only that it no longer says it.

## 2. Scope

In scope — links that name a **line** on an **uncommitted** target:

| link | text the fingerprint is taken from |
|------|------------------------------------|
| `gg://repo/path:N` | the working file (also an untracked file) |
| `gg://repo/path:old:N` | the index (the old side of the unstaged diff) |
| `gg://repo/path@staged:N` | the index |
| `gg://repo/path@staged:old:N` | `HEAD` |
| `gg://repo/path:N?view=content` | the working file |

Out of scope (user ruling: uncommitted only): commit, pair, merge-preview and
`@ref:` links; hunk links (`#N`); file links with no line. Out of scope
altogether: snapshotting content.

## 3. Grammar

A fingerprint follows the line number, separated by `~`:

```
gg://<repo>/<path>[@staged]:[old:]<line>~<fp>[?view=content]
gg://gigagit/e2e/env_test.go:33~9f2c41aa
```

- `<fp>` is exactly 8 lowercase hex characters.
- It is parsed out of the text after the LAST `:` — the same place the line
  number is read — so `~` needs no new path restriction: a path may still
  contain `~`.
- `model.Link` gains `Fingerprint string` ("" = none). `String()` writes it
  only when `Line > 0`; `ParseLink` and `String` stay inverses.
- `ParseLink` refuses, with a message naming the fix:
  - a fingerprint that is not 8 lowercase hex characters;
  - a fingerprint on a target outside §2 ("a commit already names fixed
    content; drop `~<fp>`");
  - a fingerprint on a hunk link or with no line.
- A link without a fingerprint parses and behaves exactly as today.
- An older gg build refuses a fingerprinted link as malformed. Accepted: the
  link is still readable by a human, and every agent runs the installed gg.

### The fingerprint

`fp = hex(FNV-1a 32-bit(utf8(trim(line))))`, 8 characters, where `trim`
strips leading and trailing whitespace (`strings.TrimSpace` /
`String.prototype.trim` — the tests pin that the two agree on the cases that
occur in source text: spaces, tabs, CR).

- Trimming makes re-indentation a non-event — the rule
  `model.NoteContextHash` already follows. The HASH is deliberately not that
  function's sha256: the web builds links synchronously in the browser, where
  sha256 is async-only, and 32 bits are enough to tell "this line" from "some
  other line" inside one file (a collision makes a changed line look
  unchanged; it cannot open the wrong file).
- One Go function (`model.LineFingerprint`) and one JS twin in `links.js`'s
  guarded pure section, pinned to each other by a test.
- A line that is empty after trimming gets **no** fingerprint: it would match
  every blank line. The producer emits the plain `:N` form.

## 4. Producers

Every producer of an in-scope line link adds the fingerprint; there is no
separate action and no setting.

- **TUI** — `contextLinkText` (L, the `.` menu's Copy link, the session
  snapshot's `cursor.link`): the cursor row's own text on the cursor side;
  `compareLinkText`'s working-tree / index sides; the file viewer's and the
  View-file preview's Copy file link with a cursor line.
- **Web** — `linkFor` takes the row's text (the clicked cell) for the
  unstaged / staged / untracked states and for `cmpSides`' working-tree and
  index sides.
- **CLI** — `gg link <path>:<line>` (working tree, `--cached`, `--content`)
  reads the line and adds the fingerprint. `gg link --no-fingerprint` emits
  the plain form (for scripts that want a stable string).

A producer that cannot read the line (file gone, line past the end where
that is already an error) behaves as today; it never invents a fingerprint.

## 5. Resolution

All consumers resolve through `domain.ResolveLink`, so the check lives there,
once. For a link with a fingerprint the resolver reads the text §2 names and
sets the resolved line:

| outcome | condition | `Resolved.Line` |
|---------|-----------|-----------------|
| `same` | line N has the fingerprint | N |
| `moved` | N does not, exactly one other line does | that line |
| `moved`, ambiguous | N does not, several do | the one nearest N (ties: the lower number) |
| `changed` | no line has it, or the text cannot be read | N (clamped as today) |

`Resolved` gains `Anchor`:

```go
type LineAnchor struct {
    Asked   int    // the line the link named
    State   string // "", "same", "moved", "changed"
    Matches int    // how many lines carry the fingerprint (moved)
}
```

`State == ""` means the link carried no fingerprint. Nothing refuses: a stale
link still opens where it pointed.

The read is one `ResolveBytes` (working file / index / `HEAD`) per
fingerprinted link, and none for any other link. A binary or over-cap file
resolves as `changed`.

### What each consumer says

One prose builder in `domain` (`Resolved.AnchorNote() string`, English — it
is agent-facing protocol), reused everywhere:

- `moved`: `line 33 moved to 41` · ambiguous: `line 33 moved to 41 (nearest of 3 matching lines)`
- `changed`: `line 33 has changed since this link was copied`
- `same`: nothing.

| consumer | where the note goes |
|----------|---------------------|
| `gg diff`, `gg show`, `gg note add`, `gg session highlight add` | one line on stderr, prefixed `gg: `; the verb then acts on the resolved line |
| `gg link --json`, MCP `gg_link_resolve` | fields `asked_line`, `anchor` (the state), `anchor_matches`; `line` is the resolved line |
| `gg open`, `gg session navigate` | the navigate lands on the resolved line; the reply detail ends with the note |
| TUI `#` prompt / a steered landing | the diff / viewer notice box shows the opened notice plus the note, translated through `i18n.T` like the opened notice itself (four bundles); the steer REPLY detail carries the English `AnchorNote` |
| Web landing | the op line shows the note |

`gg note add <link>` anchors the note at the RESOLVED line. For `changed` it
still adds at N — the caller was warned on stderr.

## 6. Units

- `internal/model/link.go` — `Fingerprint` field, parse/print, refusals;
  `LineFingerprint(line string) string` (pure).
- `internal/domain/linkresolve.go` — `LineAnchor`, the read, `anchorLine`
  (pure: lines + asked + fp → line, state, matches), `AnchorNote`.
- `internal/linknav` — carries the note into the steer command's reply
  detail; no decision of its own.
- `internal/tui` — producers pass the row text; the landing notice appends
  the note.
- `internal/web/static/links.js` — `lineFingerprint` twin; `linkFor` takes an
  optional row text. `files.js` passes the clicked cell's text.
- `internal/cli/link.go` — producer + `--no-fingerprint`; JSON fields.
- `internal/mcp/links.go` — JSON fields.

`anchorLine` and `LineFingerprint` are pure and tested without git; the
resolver is tested against a real repo.

## 7. Testing

- **model** — round trip for every §2 shape; each refusal; a path containing
  `~`; a Windows drive path; `LineFingerprint` vectors (indent-insensitive,
  blank → "").
- **JS ↔ Go** — the fingerprint vectors and the `linkFor` outputs are pinned
  to the Go ones (the existing node-driven test pattern).
- **domain** — `anchorLine` table (same / moved / ambiguous nearest / tie /
  changed / past end); `ResolveLink` on a real repo for the working tree, the
  index and `HEAD` sides.
- **cli** — `gg link` emits the fingerprint; `--no-fingerprint`; `gg diff
  <link>` prints the note on stderr; `gg link --json` fields.
- **tui** — `L` on a working-tree diff, both sides, and a blank line; a
  steered landing on a moved line lands there and says so.
- **e2e** — one scenario: copy a working-tree line link, edit the file so the
  line moves, `gg diff <link>` reports the move; a second edit changes the
  line, and it reports the change.
- **Prove it bites** — with the resolver's re-find removed, the moved-line
  test must fail.

## 8. Docs

`CHANGELOG.md`; `internal/agentskill/using-gg.md` (the `~<fp>` form, the
three outcomes, the JSON fields; bump `agentskill.Version`, `gg init
--update`); the `model` row of `CLAUDE.md`'s package map (one clause);
`docs/CLAUDE-details.md` (the link grammar section); `README.md`'s link
section if it lists the shapes.

## 9. Not doing

- Fingerprints on commit / pair / preview / ref links (user ruling).
- Snapshotting content, or showing what a changed line used to say.
- Multi-line or context fingerprints: one line, nearest match on ambiguity.
- Re-anchoring links already in the copied-link history: they are stored
  text and resolve like any pasted link.

# Text templates — design

Date: 2026-10-02 · Branch: `feat/text-templates`

## Goal

Reusable, longer pieces of text (a PR description, a bug report, a standup
note) that the user keeps in gg, fills with a few values, and copies to the
clipboard. The model is the branch-name prefix feature: same token grammar,
same two scopes, same kind of store — but the value is a titled, multi-line
body instead of a one-line skeleton.

## Agreed rulings (user, 2026-10-02)

- Key: `alt+x` (`alt+t` stays the last-terminal cycle).
- The body is written and edited in `$EDITOR`; no in-TUI multi-line editor.
- Storage like branch prefixes: machine-local, global + this-repo scopes.
- Frontends: TUI + CLI + web.
- Variables are listed BELOW the template text, not beside it.
- `y` on the rendered text copies it and closes the WHOLE window.
- Variable values are single-line for now.

## Data

`model.TextTemplate{ID, Title, Body, Scope, Created}`.

New package `internal/texttmpl`: a `Store` interface (`Add`, `Get`, `List`,
`Update`, `Remove`) and a `FileStore` over `texttemplates.toml`, placed and
written exactly like `prefix.FileStore` (same root per scope, same atomic
rewrite). DAG position as `prefix`: owned by `domain`, frontends never
import it.

- The ID is derived from the title (as `PrefixID` derives from the value). A
  title is unique within a scope; adding a duplicate title is an error.
  `Update` keeps the ID when only the body changes; a title change re-derives
  it. Moving a template between scopes is remove + add.
- Limits: title ≤ 80 runes, single line; body ≤ 64 KiB, non-empty after
  trimming trailing whitespace.

## Tokens

The grammar is the branch-prefix one: `<user:LABEL>`, `<date>`,
`<date:FMT>`, `<seq:NAME>`, `<seq:NAME:N>`, `<parent-branch>`, `<repo>`,
`<random-alpha:N>`, `<random-num:N>`, plus `<branch>` = the current branch
(raw, not path-sanitized).

Text differs from a branch name in one way: prose and markdown contain
angle brackets (`<br>`, `<me@example.com>`). So `internal/template` gains a
text entry point, `ResolveText(tmpl, inputs, ctx)`:

- a `<…>` whose prefix is not a known token kind stays in the output as
  literal text;
- a KNOWN token with malformed arguments (`<seq>`, `<date:…>` with no
  clock) is still an error;
- `<branch>` resolves to `Ctx.Branch` verbatim; an unset branch (detached
  HEAD) resolves to the empty string.

`template.TextTokens(tmpl)` returns the user labels (first-appearance
order) and the distinct automatic tokens used, for the "Variables" /
"Automatic" lines. `Resolve` (branch names, paths) is unchanged.

`<seq:NAME>`: a preview peeks the counter; the counters are consumed only
when the rendered text is taken (TUI `y`, web Copy, CLI `render`).

## Domain

On `*domain.Service`, mirroring `prefixstore.go` / `prefixresolve.go`:

- `TextTemplates(ctx) ([]model.TextTemplate, error)` — global rows, then
  repo rows.
- `AddTextTemplate`, `UpdateTextTemplate`, `RemoveTextTemplate`.
- `ValidateTextTemplate(title, body) error`.
- `TextTemplateTokens(body) (userLabels, automatic []string)`.
- `RenderTextTemplate(ctx, body, inputs) (text string, seqNames []string, err error)`
  — peeks sequences; the caller bumps them through the existing
  `BumpPrefixSeqs` when the text is taken.
- `SetTextTemplateStores(global, repo)` for tests.

Store reads/writes are not git operations and take no repo reservation
(the prefix precedent).

## TUI

One layer-stack window, `textTemplatesView` (`popupMax` embedded, so
`ctrl+t` maximizes), opened by `alt+x` when no layer is open and no filter
is being typed (the `alt+a`/`alt+t` gate). Inside a focused agent console
the key goes to the agent. Also: a command-palette entry, a help row, a
global footer hint.

### Browse

```
╭──────────────────────────────────────────────────────────────────────────╮
│ Text templates                                                           │
│                                                                          │
│ > PR description                                           [global]      │
│   Release announcement                                     [global]      │
│   Bug report                                               [this repo]   │
│   …                                                                    ▼ │
│ ──────────────────────────────────────────────────────────────────────── │
│ ## <user:title>                                                          │
│                                                                          │
│ Branch: <branch>  ·  <date>                                              │
│ Ticket: <user:ticket>                                                    │
│ …                                                                      ▼ │
│ ──────────────────────────────────────────────────────────────────────── │
│ Variables: title · ticket · summary                                      │
│ Automatic: <branch> · <date>                                             │
│                                                                          │
│ [enter] fill  [n] add  [e] edit  [d] delete  [ctrl+t] maximize  [esc]    │
╰──────────────────────────────────────────────────────────────────────────╯
```

- The list shows at most 8 rows (`renderWindow`, anchored on the
  selection); a long title is cut with the full text in the bottom bar (the
  popup cut-row rule).
- The body pane shows the selected template's text, soft-wrapped, sized to
  the remaining popup height; `pgup`/`pgdn` scroll it, `↑`/`↓` move the
  list selection and reset the body scroll.
- "Variables" lists the `<user:…>` labels; "Automatic" the other known
  tokens used. A line with nothing to list is omitted; both lines wrap.
- Empty store: `(none yet — [n] to add)`.

### Fill → rendered

`enter` opens the fill step, reusing `templateFill` (one single-line field
per label, `enter`/`tab` next, `esc` back to browse). A template with no
`<user:…>` token skips it.

```
│ PR description — fill variables (2/3)                                    │
│                                                                          │
│   title:   Text templates window                                         │
│ > ticket:  GG-412█                                                       │
│   summary:                                                               │
│                                                                          │
│ [enter/tab] next  [esc] back                                             │
```

The rendered step shows the resolved text, soft-wrapped and scrollable:

```
│ PR description — rendered                                                │
│                                                                          │
│ ## Text templates window                                                 │
│ …                                                                      ▼ │
│                                                                          │
│ [y] copy and close  [↑/↓] scroll  [esc] back to templates                │
```

- `y` copies through the existing clipboard path, bumps the consumed
  sequences, pops the whole window, and sets a "copied" status message. A
  failed copy keeps the window open and shows the clipboard error.
- `esc` returns to browse (the fill values are dropped).
- A resolve error is shown in place of the text; only `esc` is offered.

### Add / edit / delete

`n` and `e` open a small form: title field + scope (`←/→`, global / this
repo). In edit mode the scope is fixed. `enter` validates the title and
hands the body to `$EDITOR`: a temp file (`.md`, seeded with the current
body, empty for a new one) opened through `handover()`; on exit the file is
read back, trailing whitespace trimmed, and the template saved. An empty
body, or an unchanged body+title on edit, saves nothing and says so. The
temp file is removed either way. `d` deletes after a yes/no confirm.

All strings go through `i18n.T` with keys in the four bundles.

## CLI

`gg template` (alias `gg templates` for `list`), mirroring `cli/prefix.go`:

- `list` — `id  [scope]  title`, one per line.
- `show <id>` — the raw body, then the variables.
- `render <id> [--var label=value …]` — the resolved text on stdout. A
  missing `--var` is an error naming the label; consumes sequences.
- `add --title <t> [--repo] [-F <file>]` — body from the file, or stdin.
- `edit <id> [--title <t>] [-F <file>]` — body from the file, or stdin when
  piped; with neither, only the title changes.
- `remove <id> [--repo]`.

An id may be given as a unique prefix; the repo scope wins a tie between
scopes unless `--global` is passed. `internal/agentskill/using-gg.md` gains
the verbs (bump `agentskill.Version`).

## Web

A "Text templates" view matching the TUI: title list, body, variables
lines, a fill dialog, the rendered text with a Copy button
(`navigator.clipboard`, the existing copy helper). Add and edit use a
dialog with a title field, scope select and a `<textarea>`. Endpoints,
beside `web/prefixes.go`: `GET /api/text-templates`, `POST …/add`,
`POST …/update`, `POST …/remove`, `POST …/render` (`{id, scope, inputs,
take}` — `take: true` bumps sequences; the page calls it on Copy). Mutating
routes carry the same guards as the prefix routes. Opened from the palette
and `alt+x`.

## Testing

- `texttmpl`: store round-trip, duplicate title, update, scope files.
- `template`: `ResolveText` — literal unknown tokens, known-token errors,
  `<branch>`, `TextTokens` order.
- `domain`: render peeks, bump consumes, validation limits.
- `cli`: verb tests + one e2e scenario (add → list → render).
- `tui`: unit tests for browse / fill / rendered / form state, the editor
  round-trip through a fake editor, `y` closing the window; one golden
  screen of the browse window.
- `web`: handler tests; a playwright probe asserting the view is VISIBLE.

## Build order

One branch, three commits' worth of stages: (1) `template.ResolveText` +
store + domain + CLI, (2) the TUI window, (3) the web view. Docs after each:
CHANGELOG, README, `docs/CLAUDE-details.md`, one package-map row in
CLAUDE.md, the config/settings registry untouched (no new config entry).

## Out of scope

Multi-line variable values; an in-TUI multi-line editor; templates in
`.gg.toml` (team sharing); MCP tools; inserting a rendered text straight
into a commit message or PR.

# gg links — portable file/line addresses (design, approved 2026-09-10)

A `gg://` link names one place in one repository — a file, a line on one
side of one diff, a hunk, or a commit — in a form a human can copy from the
TUI, paste into a chat on another machine, and an agent can hand straight
back to the gg CLI. It is the communication shortcut for the review loop
that phases 1–3 of the hunk-parity roadmap built (notes, the agent lane,
live steering): "look at `gg://gigagit/internal/tui/steer.go@eb759989:42`"
is enough for `gg session navigate`, `gg note add`, `gg diff` and
`gg show`.

Decided in chat 2026-09-10 (do not re-ask):

- **Repo identity**: a repo that has a remote is named by the remote's
  repository name; a repo with no remote is named by its absolute checkout
  path. Both resolve through gg's machine-local repository history (the
  `internal/repos` registry behind the repo switcher).
- A link with no path (`gg://gigagit@eb759989`) navigates to the commit in
  the Commits panel.

## 1. Link grammar

```text
gg://<repo>/<path>[@<target>][:<line>]        file / line
gg://<repo>/<path>[@<target>]#<hunk>          hunk (git @@ order, 1-based)
gg://<repo>@<commit>                          commit, no path
gg://<repo>                                   the repository itself

<repo>    = <name>                    remote-named: origin's repository name
          | /<absolute checkout path> local-only: gg:///mnt/t/others/test-1/…
<target>  = <commit sha, full or ≥7 hex>      committed (parent → commit)
          | staged                            HEAD → index
          | (absent)                          working tree (index → file)
<line>    = <n> | old:<n>                     new side by default
```

Examples:

```text
gg://gigagit/internal/tui/steer.go@eb759989:42
gg://gigagit/internal/tui/steer.go:42              working tree, new side
gg://gigagit/internal/tui/steer.go@staged:42
gg://gigagit/internal/tui/steer.go@eb759989:old:38
gg://gigagit/internal/tui/steer.go@eb759989#3
gg://gigagit@eb759989
gg:///mnt/t/others/test-1/src/PipelineConfig.kt@d0ff6830:42
```

Rules:

- `<path>` is the git slash form, relative to the checkout top level, not
  URL-encoded. A path containing `@`, `:` or `#` cannot be expressed: the
  producers refuse to copy it ("path contains @, : or # — no gg link") and
  the parser reads the FIRST `@` after the repo segment as the target
  separator, so such a path never parses as something else by accident.
- The remote-named form takes the LAST path segment of the `origin` URL
  with a trailing `.git` stripped (`git@github.com:homeend/gigagit.git` and
  `https://github.com/homeend/gigagit` both give `gigagit`). When the repo
  has remotes but no `origin`, the first remote in `git remote` order is
  used. Case is preserved; matching is case-insensitive on Windows only.
- The local form is `gg:///` followed by the absolute checkout path with
  forward slashes (`gg:///C:/src/repo/...` on Windows). It is machine-bound
  by nature; the resolver still consults the registry so a moved checkout
  can be found by name as a fallback.
- Short shas are accepted on input (≥ 7 hex); producers always write the
  FULL sha, so a link never becomes ambiguous as history grows.
- `#<hunk>` and `:<line>` are mutually exclusive.

The parsed form is one value type:

```go
// internal/model
type Link struct {
    Repo     LinkRepo   // Name ("gigagit") or Path ("/abs/checkout"); exactly one set
    Path     string     // "" = the repository / a commit link
    Target   LinkTarget // StateUnstaged (default), StateStaged, StateCommitted + Commit
    Side     NoteSide   // NoteSideNew unless "old:"
    Line     int        // 0 = none
    Hunk     int        // 0 = none
}
func ParseLink(s string) (Link, error)       // strict grammar, English errors
func (l Link) String() string                // canonical form (full sha, slash path)
func (l Link) Address() FileAddress          // Path/State/Commit; Worktree filled by the resolver
```

`model.FileAddress` already carries Path/State/Commit/Worktree, so a link is
a FileAddress plus a repo identity plus an optional line/hunk; the note
store, the steer wire and the diff loaders all consume FileAddress today.

## 2. Resolution

`domain.ResolveLink(ctx, link, opts) (Resolved, error)` turns a link into a
checkout on THIS machine:

```go
type Resolved struct {
    Checkout string          // absolute top level of the chosen checkout
    Addr     model.FileAddress // Worktree = Checkout for working-tree states
    Line     int
    Side     model.NoteSide
    Hunk     int
    Commit   string          // full sha when the link names a commit
}
```

Candidate order:

1. **The cwd's repo**, when the caller runs inside a checkout whose
   identity matches (same remote name, or the same absolute path).
2. **The registry** (`internal/repos`, MRU order): every registered
   checkout whose identity matches. The registry gains a per-entry `remote`
   field (`toml:"remote"`, the remote repository name computed at
   `Touch` time; entries written by older gg have it empty and are
   computed lazily on first lookup, then rewritten). A local-path link
   matches an entry by path; when the path no longer exists it falls back
   to entries whose directory name equals the path's base name (a moved
   checkout).
3. **Disambiguation** when several checkouts match (several worktrees or
   clones of one repo): keep those that CONTAIN the target commit
   (`git cat-file -e <sha>^{commit}` in each; working-tree links skip this
   step); then prefer a checkout with a live gg session (`steer.Live` on
   its TUI/web presence); then the most recently opened. If two remain
   equally ranked, the resolver returns `ErrLinkAmbiguous` listing them,
   and the CLI prints them with an exit 1 — it never guesses between two
   worktrees, because working-tree links would land in the wrong files.
4. **Not found** → `repo <name> is not in this machine's gg history; open
   it once in gg` (exit 1). The registry is the only source; gg never scans
   the filesystem.

The resolver is a domain query (`Read` reservation on the chosen repo only
for the commit probe); it needs a `Service` per candidate checkout, opened
through the same path the repo switcher uses.

## 3. Producers

- **TUI**: in the diff view, `.` menu row "Copy link" (and the key `L`,
  verified free in the diff view — the plan must re-check) copies the
  cursor line's link: target from `diffView.noteAddr`, side/line from the
  cursor row (new side when the row has one, else old), full sha. In the
  Files/Staged panels the row's file link (no line); in the Commits panel
  the commit link (`gg://<repo>@<sha>`); in a commit's files view the file
  at that commit. The clipboard path is the existing `clipboard` writer
  with its notices ("copied gg://… "). A repo with no remote produces the
  local form.
- **Session snapshot** (`ui-state.json`, `gg session status`, MCP
  `gg_ui_state`): a new `cursor.link` string — the same link the TUI would
  copy for the current cursor (diff line when a diff is open, else the
  focused panel's row). This is how an agent learns "where the user is
  looking" without the user copying anything.
- **Web**: the diff row's context menu gets "Copy link"; the same producer
  logic ported to JS (`linkFor(ctx, side, no)`), full sha from
  `state.diffCtx.rev`.
- **CLI**: `gg link [<path>[:<line>]] [--cached | --rev <c>]` prints the
  link for a path in the cwd's repo (default: the repo link), for scripts
  and for agents that want to hand a link back to the user.

## 4. Consumers (CLI positionals)

Every verb below accepts a link as its FIRST positional and then behaves as
if the corresponding flags had been given, resolving the repo first and
running against that checkout even when the cwd is elsewhere:

```text
gg session navigate <link>                 # file+line / hunk / commit reveal; --no-wait as today
gg session highlight add <link>[-<end>] [--tone t]   # start from the link's line, --end or "-<n>" suffix
gg note add <link> --summary … [--rationale …]
gg note list <link>                        # notes on that file/target (line ignored)
gg diff <link>                             # the file's diff for that target (line ignored)
gg show <link>                             # the commit (path optional → --patch limited to it)
gg link resolve <link> [--json]            # prints the chosen checkout + FileAddress; exit 1 when ambiguous/unknown
```

Mixing a link with the flags it replaces (`--file`, `--rev`, `--cached`,
`--hunk`, `--new-line`, `--old-line`) is a usage error (exit 2). A link
whose target state does not fit the verb (a commit link to
`gg session highlight add` on a page showing the working tree) is not
special-cased: the verb behaves exactly as with explicit flags.

Steering from another checkout: `gg session navigate gg://gigagit/…` run
from `/tmp` resolves the checkout, then posts into THAT checkout's inbox
(the steer dir is per worktree, `config.SessionSteerDir(commonDir,
checkout)`), so the live session in that worktree moves. The routing table
(none / TUI / web / both) is unchanged.

MCP: `gg_ui_state` exposes `cursor.link`; no new tools (MCP agents shell
out, as for steering).

## 5. Skill text

`using-gg.md` gains a "gg links" paragraph: the grammar in five lines, "a
link the user pastes is enough — pass it as the first positional to `gg
diff`, `gg show`, `gg note add`, `gg session navigate`", and "read
`gg session status` for `cursor.link` to see where the user is looking".
`reviewing-with-gg.md` gains one line: "quote a link (`gg link <path>:<n>`)
in your chat reply for every finding, so the user can jump to it".
`Version` and `ReviewVersion` bump; dogfood copies regenerate.

## 6. Out of scope (v1)

Links to notes (`gg://…/note/<id>`), links to branches or tags, a
`gg://` URL handler in the OS, web-page consumption of a pasted link (the
page has no paste surface; the CLI steers it).

## 7. Testing

- `model.ParseLink`/`String` round-trip table: every grammar row, short
  and full shas, paths with `@`/`:` when a target is present, Windows
  local form, refusals (both `#` and `:`, bad sha, empty repo).
- `domain.ResolveLink` on real repos in `t.TempDir()`: cwd match; registry
  match by remote name (two clones, only one containing the commit); local
  path match and moved-checkout fallback; ambiguity error listing both;
  unknown repo error; the lazy `remote` fill of an old registry entry.
- CLI: each consumer verb with a link vs the equivalent flags produces
  identical behaviour (compare the posted steer command / the stored note
  / the diff output); link + flag → exit 2; `gg link` output round-trips
  through `ParseLink`; `gg link resolve --json`.
- TUI: the copy producers (diff line new/old side, files row, commit row,
  commit-files row) through the clipboard seam; snapshot `cursor.link`
  present and canonical; the key is free (i18n/menu gates).
- Web: `linkFor` unit-tested via the JS source assertion pattern used by
  the steering tests; a CDP probe copying a link from a diff row.
- e2e: `s90_links.toml` — `gg link`, `gg diff <link>`, `gg note add
  <link>` in the sandbox repo (local-path form, since the sandbox has no
  remote).

# Task 0 — registries, so six agents never share a file

**Run this alone, before tasks 1–6.** It adds no user-visible behaviour: it
changes *where* a feature is allowed to plug in, so six parallel branches stop
colliding in the same four files.

Read `README-web-parallel-tasks.md` first — ground rules, ownership table, and
the browser-check recipe live there.

## Why

Every remaining web feature wants to edit the same places:

| File | Today | Collisions |
|------|-------|-----------|
| `internal/web/ophttp.go` | one `switch` with **43 `case`s**, 1054 lines | every new operation |
| `internal/web/server.go` | one route table | every new endpoint |
| `static/commits.js` / `files.js` / `sidebar.js` | menu rows built inline | every new menu row |
| `static/index.html` | 50 static help rows, static overlay markup | every user-visible feature |

Six agents adding a `case` to one switch conflict on adjacent lines, every
time. After this task they each write their own file and register into it.

## What to build

### 1. Operation registry (`internal/web/opreg.go`, new)

```go
// builder returns the op, an optional cleanup to run after it finishes (the
// shelf's patch lane leaves a temp file), an HTTP status for a refusal, and
// the error. Mirrors the existing buildRestore / buildShelfCherryPick shape.
type OpBuilder func(*Server, *http.Request, opStartRequest) (engine.Operation, func(), int, error)

func RegisterOp(name string, b OpBuilder)   // panics on a duplicate name
func lookupOp(name string) (OpBuilder, bool)
```

- `handleOpStart` consults `lookupOp(req.Op)` **before** its existing switch;
  the switch stays exactly as it is for the 43 ops that already work. Do not
  migrate them — a mass move is a merge hazard for the very branches this
  task exists to protect, and it buys nothing.
- Honour the cleanup: when a builder returns one, run the op through
  `startRun` with `defer cleanup()` (copy the `shelf-cherry-pick` arm, which
  already does this).
- Register from an `init()` in the feature's own file.

### 2. Route registry (`internal/web/routereg.go`, new)

```go
func RegisterRoutes(fn func(mux *http.ServeMux, s *Server))
```

Collected at init; `Server.Handler()` calls each after its own `mux.HandleFunc`
lines. A feature adds `func init() { RegisterRoutes(func(mux *http.ServeMux, s *Server) { mux.HandleFunc("GET /api/mine", s.handleMine) }) }`.

### 3. Menu-row registry (`static/menus.js`, new)

```js
registerRows("commit", (ctx) => [ {label: "…", act: () => …} ]);
extraRows("commit", ctx)   // used by the menu builders
```

Menus and their context object:

| Menu | key | ctx |
|------|-----|-----|
| commit list row | `"commit"` | the commit row (`hash`, `short`, `subject`, `parents`) |
| files pane row | `"file"` | `{path, section, sha}` |
| branch row | `"branch"` | the branch |
| worktree row | `"worktree"` | the worktree |
| tag / stash / reflog rows | `"tag"` / `"stash"` / `"reflog"` | the entry |

In `commits.js`, `files.js` and `sidebar.js`, append `...extraRows(key, ctx)`
to each menu's item list **once**, just before `showCtxMenu`. Separators keep
working: `showCtxMenu` already collapses doubles and trims stray ones, and
gives an ungrouped red row its own line.

### 4. Help registry (`static/help.js`, new)

`registerHelp({key, html})` appends a `.hrow` to `#help-box` at boot, after the
static rows. A feature documents itself from its own module; nobody edits
`index.html`.

### 5. Overlay helper (`static/layers.js`)

`mountOverlay(id)` creates and returns a hidden `<div id=…>` appended to
`document.body` if it does not exist, so a new full-screen surface needs no
markup. Keep it a few lines; the layer stack already handles the rest.

## Acceptance

- `RegisterOp` + `RegisterRoutes` have tests: a registered op runs through
  `POST /api/op`, a registered route answers, a duplicate name panics.
- `registerRows` has a browser check: a row registered from a scratch module
  appears in the commit menu, in registration order, after the built-ins.
- Nothing user-visible changes. The existing suite passes untouched — that IS
  the acceptance criterion for the refactor half.
- `./test.sh race` green.
- CHANGELOG bullet: internal, one line, "so features can register instead of
  editing shared files".

## Verification

The full recipe is in `README-web-parallel-tasks.md`. For this task the browser
check is small but not optional: register a throwaway row from a scratch
module, open the commit menu, assert it is there and that the console is
clean. An ES-module mistake here breaks every downstream task at once, and
`node --check` cannot see it.

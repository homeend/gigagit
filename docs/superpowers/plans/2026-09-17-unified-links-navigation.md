# Unified `gg://` Links — Plan 2: Navigation, History and the Agent Surface

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make every link shape the grammar can express *navigable*, give links a
20-entry history that survives the process, and put both on the MCP surface — so a
link an agent produces can be handed straight back to a human, and a link a human
copies can be handed straight to an agent.

**Architecture:** Plan 1b made every link *evaluable* (`EvalEndpoint` →
`FileSet` → `CompareSets`). It deliberately left every link *unnavigable* beyond
the shapes that shipped before it. This plan closes that gap in the one seam that
owns it — `domain.ResolveLink` → `linknav.Command` → `steer.Command` → the two
consumers — then adds the machine-local `internal/linkhist` store behind
`domain`, the three MCP tools, and the `agentskill` bump. No new compare
algebra, no new TUI surfaces (plan 3), no `savedcompare` (plan 3).

**Tech Stack:** Go 1.26, `github.com/pelletier/go-toml/v2`,
`github.com/modelcontextprotocol/go-sdk/mcp`, Bubble Tea, vanilla ES modules.

**Spec:** `docs/superpowers/specs/2026-09-16-unified-links-design.md` (§3.3, §4.3,
§4.4, §5.3, §5.4, §5.5, §6, §8 row "2").

---

## Global Constraints

- **`internal/tui`, `internal/cli`, `internal/mcp` and `internal/web` never
  import `internal/git`** — they reach git through `internal/domain`.
  `internal/archtest` fails the build otherwise. `internal/linkhist` is a DAG
  leaf owned by `domain`; **no frontend may import it** (the
  `notes`/`shelf`/`preview`/`bookmark` convention).
- **`domain` must not import `internal/steer`.** The `ResolveOpts.LiveFn` seam
  exists for exactly that. `linknav` sits between them and may import both.
- **Every user-visible TUI string goes through `i18n.T` with a LITERAL key
  present in all four bundles** — `ja`, `ko`, `zh`, `ru`; the English text IS
  the key, so there is no `en` bundle. Add the key to every file under
  `internal/i18n`. Four AST gates fail otherwise: `i18n_scan_test.go`, `options_vocab_test.go`,
  `menu_labels_test.go`, `engine_prose_test.go`. **Engine/CLI prose and every
  steer protocol VALUE stay English** — they are an agent-facing protocol.
- **`model.Endpoint` has unexported fields.** Build one only through a
  constructor. Adding an `EndpointKind` without a row in
  `internal/model/endpoint_exhaustive_test.go` fails the build, by design.
  *This plan adds no endpoint kind.*
- **Every fix carries a test proven to fail without it.** Write the test, run
  it, watch it fail, then implement. Three vacuous tests were caught in plan 1b
  by reverting the fix and re-running.
- **A git verb is one invocation**, built with `gitcmd`, run through
  `r.Runner.Run`/`.Stream`. Never shell out directly.
- **State isolation:** an explicitly-set `$XDG_STATE_HOME` wins over
  `%LocalAppData%` on every platform. A new store that does not check it in
  that order will write the developer's real `AppData\Local\gg` on Windows.
- **Windows:** `filepath.IsAbs("/x")` is FALSE on Windows. Never build a link
  string as `"gg://" + path`; use `model.Link{...}.String()`. Tests comparing
  raw git output use `filepath.ToSlash` on BOTH sides.
- **Run tests in the FOREGROUND** on your own packages only. Never pipe a test
  command through `tail` — you would read tail's exit code. A shell hook
  rewrites `go test` output into a summary line; use `rtk proxy go test …` for
  raw output.

---

## Rulings

These deviate from, or decide what the spec leaves open. They are binding; do
not re-litigate them during execution.

**R1 — A file link with no line opens the file, positioned at the top.**
`gg link internal/web/static/sidebar.js` emits `gg://gigagit/internal/web/static/sidebar.js`
and `gg open` on it returns `linknav.ErrNoLine`. The producer and the navigator
disagree about what a valid link is — one layer below where `linkRoundTrips`
was added to prevent exactly that. The protocol rule becomes: **`File` set and
`Line` nil means "open that file's diff; do not move the cursor".**
`linknav.ErrNoLine` is deleted — after this nothing can produce it.
*Cost if wrong:* an agent that meant line 1 lands on line 1 anyway.

**R2 — A `@ref:<name>` link navigates to its tip; a `@<a>..<b>` link opens as a
compare.** Both ride the wire as NAMES, never as a resolved sha, and the
consumer resolves — the pattern `Target{State:"preview"}` already set, so a tip
that moved between post and apply is honoured. A ref is a POINT (the tree
there); a pair is BOUNDED (what it changed), which is a compare, not a commit.
*Cost if wrong:* a ref link could instead have frozen the tip at post time; the
preview lane already made the opposite call and nothing has argued against it.

**Correction (after Task 4's review, 2026-09-17).** R2 as written above is
true of the REF arm and NOT of the pair arm, and the difference is by design,
not by accident:

- `@ref:<name>` — `domain.finishLink` keeps the NAME in `Resolved.Ref`, so
  every producer puts a name on the wire and every consumer re-resolves it. A
  tip that moves between post and apply IS honoured.
- `@<a>..<b>` — `finishLink` resolves BOTH halves to full shas before the
  command is composed, so every real producer (`gg open`, `gg session
  navigate`, `gg link --pair`) puts SHAS on the wire. That is correct and
  deliberate: a change-set is a fixed pair of commits — that is what makes it
  BOUNDED, re-comparable and portable between machines, and `gg link --pair`
  already advertises "both halves resolved to full shas so the link travels".
  A pair does not track a moving branch.

So the consumers' name-handling for a pair (`steerNavigatePair`'s
`ResolveRev`, and the web's `openCompareForPair`) is DEFENSIVE, not the main
path: it is reachable by a hand-composed steer post — which an agent can
make — and by the TUI's `--at` converter, which passes a link's halves
verbatim. It must work, and Task 4's fix round made it work; but a
browser-verified name-pair scenario is not representative of what `gg open`
actually sends, and nobody should conclude from it that the sha path is
untested.

**R3 — `locateLink` filters ref and pair candidates by name containment,
ALWAYS.** A ref link is `StateCommitted` with an EMPTY `Commit`, so today it
skips the commit-containment filter (`l.Target.Commit != ""` is load-bearing
there) and falls to MRU order with no ambiguity check: a checkout that does not
hold the branch wins. `previewCandidates` is the shape to copy — it runs
unconditionally, not only on ties, because "the cwd has the repo but not the
branch" is the common case, not the tie-break case.
*Cost if wrong:* an extra `ResolveRev` per candidate on links that resolve fine
today. Cheap; the filter is what makes a ref link honest.

**R4 — Per-verb acceptance of the two new shapes.** `resolveLinkArg` is shared
by six verbs and each has a different notion of "a place":

| verb | `@ref:<name>` | `@<a>..<b>` |
|---|---|---|
| `gg open` | accept (navigate to the tip) | accept (open the compare) |
| `gg session navigate` | accept | accept |
| `gg diff` | accept (a tip is a rev) | accept (`a..b` *is* a diff) |
| `gg show` | accept (show the tip commit) | **refuse** |
| `gg note add/list/apply/clear` | accept (the tip is the write target) | **refuse** |
| `gg session highlight` | accept | **refuse** |

A pair's only single commit is `b`, and anchoring a note or a `show` there would
silently widen a BOUNDED change-set into the whole tree at `b` — the same
mistake `sideLosesItsKeySet` refuses per side in `gg compare --patch`. The
refusal is explicit prose naming `gg compare`, never a fall-through.

**Accepting a pair is not enough — `gg diff` must diff the RANGE.** The
allow-list alone is a silent wrong answer: `cli/linkconsume.go`'s
`linkDiffSpec` falls through to `case model.StateCommitted:` and asks
`HunkDiffSpec(ctx, false, res.Addr.Commit, paths)`, and Task 2 sets
`Addr.Commit` to **B** — so `gg diff gg://repo@a..b` would pass the gate and
print `B^..B`, B's own change, at exit 0. `HunkDiffSpec` already passes a `rev`
containing `..` straight through (`domain/hunks.go:58`), so the fix is one
branch; Task 5 step 3b is where it goes.

**And a `@ref:` link in `gg diff` means the tip's OWN change** (`tip^..tip`),
exactly as a commit link does — not `git diff <tip>`. `@ref:main` and
`@<that sha>` are two spellings of the same kind of target (both unbounded
points), and two spellings of one thing must not diff differently. The bare-rev
flag path's `gg diff <rev>` (working tree vs rev) is a different vocabulary and
does not bind the link path; `linkDiffSpec` has always routed every link
through `HunkDiffSpec` for that reason.
*Cost if wrong:* `gg diff <ref link>` shows the tip commit instead of the
uncommitted drift; the user asks `gg diff` with no link.
A verb that names no allowance gets `{Ref: true, Pair: false}`: forgetting to
think about a bounded shape must REFUSE, never widen. That is the same
direction as the algebra itself — you can only ever scale down — and a test
cannot catch a seventh verb that does not exist yet.
*Cost if wrong:* a user who wanted `gg show` on a pair runs `gg compare`; a
future verb that should take pairs is refused until someone passes the flag,
which is a compile-time-visible one-line fix rather than a silent wrong answer.

**R5 — The hint is honoured on navigate and degrades with a notice.** `?bookmark=<id>`
reveals the bookmark row, `?shelf=<id>` the shelf row; the address is navigated
either way. A hint naming an entry this machine does not have is DROPPED with a
notice, never an error (spec §3.3 rule 3) — **except** the address-less
`gg://<repo>?shelf=<id>` form, where the hint is the only content source and a
miss is a hard error (spec §6).

**R6 — `linkhist` is PER REPOSITORY**, keyed by `repoKey(commonDir)` under
`<state>/gg/linkhist/<key>/links.toml`, exactly as `shelf`, `notes`, `search`
and `bookmark` are. Cross-repository compare is refused in phase 1 (spec §9), so
a global list would offer rows nothing in this build can consume, and every
surface that reads the history (`gg links`, `gg_link_list`, the `#` prompt's
picker in plan 3, the compare dialog in plan 3) is a per-repo surface.
*Cost if wrong:* a link copied in repo A is not offered in repo B. The store's
location is private to `domain`, so moving it later changes no API.

**R7 — `Desc` is captured at creation and never derived at read time** (spec
§4.3). The describing context — which row the user was on — is gone by the time
anything lists the history. A stash's `Desc` ("WIP on main") is the only thing
that makes its row recognisable, because `stash@{N}` is deliberately absent from
the link.

**R8 — `gg compare <left> <right>` appends a LINK argument to the history on
success.** Spec §4.3: "A link the user *pastes* into a compare field and
successfully compares is also appended." The CLI positional is that field. Only
arguments that were links, only on exit 0, and each through the same
move-to-top rule.

**R9 — Explicitly OUT of scope.** A reviewer must not flag these as gaps:
`sideLosesItsKeySet` moving from `internal/cli` into `domain` (and with it
un-exporting `FileSet.Narrowed`); projected `--patch` rendering and the
`model.DiffSpec.Reverse` field it needs; the parked 1a exit-code gap
(`domain.ResolveRev` returns `ok=false, err=nil` for git failures, so a corrupt
or locked repo reports "unknown revision"); `internal/savedcompare` and the
file-backed migration; every new TUI surface (copy rows, the compare palette,
the shared base picker) and every new web surface beyond `/api/linkhist`. Those
are plan 3.

---

## File Structure

| file | responsibility |
|---|---|
| `internal/steer/steer.go` | **modify** — `Target` grows `Ref`, `A`, `B` and `Hint*`; `Command.Line`'s doc records the nil rule (R1) |
| `internal/domain/linkresolve.go` | **modify** — `Resolved` grows `Ref`, `Pair`, `Hint`; `refCandidates`/`pairCandidates` (R3); `ResolveLink` stops refusing ref/pair |
| `internal/linknav/linknav.go` | **modify** — `ErrNoLine` deleted (R1); `Command` grows the ref, pair and hint arms; `AtLink` carries them |
| `internal/tui/steer_nav.go` | **modify** — `steerCommandForLink` stops refusing a line-less link; `steerNavigateRef`, `steerNavigatePair`, hint reveals |
| `internal/web/steer.go` | **modify** — the wire validator learns `ref`/`pair` states and the hint fields |
| `internal/web/static/live.js` | **modify** — `steerNavigate` grows the ref and pair cases + hint reveals |
| `internal/cli/link.go` | **modify** — `resolveLinkArg` grows a per-verb allow-list (R4); `gg link` appends to the history |
| `internal/cli/links.go` | **create** — `gg links`: the 20-row history, terse (spec §5.3) |
| `internal/linkhist/store.go` | **create** — the `Store` interface + `Entry`; DAG leaf, stdlib + toml only |
| `internal/linkhist/file_store.go` | **create** — TOML + `O_EXCL` lock, read-merge, dedup-to-top, cap 20 |
| `internal/domain/linkhiststore.go` | **create** — the per-repo store resolver + `RecordLink`/`LinkHistory` (the `searchstore.go` shape) |
| `internal/web/linkhist.go` | **create** — `GET`/`POST /api/linkhist` |
| `internal/mcp/links.go` | **create** — `gg_link_resolve`, `gg_link_list`, `gg_compare_links` |
| `internal/mcp/compare.go` | **modify** — `gg_compare_file` accepts a link on either side |
| `internal/agentskill/using-gg.md` | **modify** — the new verbs, the two navigable shapes, the MCP tools |
| `internal/agentskill/skill.go` | **modify** — `Version` 80 → 81 |
| `CHANGELOG.md`, `README.md`, `docs/CLAUDE-details.md`, `CLAUDE.md` | **modify** — one map row for `linkhist`; detail in CLAUDE-details |

---

### Task 1: A file link with no line opens the file

Plain "this file at this commit" is the most natural thing to copy and the one
shape that falls through: a repo-only link launches gg in the checkout, a line
link steers, and a line-LESS file link is refused by the two PRODUCERS — even
though **both consumers already implement it**. Verify that before writing
anything: `internal/tui/steer_nav.go:246` (`if c.Line == nil { … "opened
"+c.File }`), `drainPendingLoad` at `:319` (the same for the commit and preview
lanes), and `internal/web/static/live.js:276` (`if (!s.line) return;`). The
protocol already means what R1 says; nothing but the refusals is missing.

**Files:**
- Modify: `internal/linknav/linknav.go` — delete `ErrNoLine` and both `line.No < 1` refusals (≈`:127`, `:154`)
- Modify: `internal/tui/steer_nav.go` — `steerCommandForLink`, the two `l.Line < 1` refusals
- Modify: `internal/steer/steer.go` — `Command.Line`'s doc comment
- Modify: `internal/cli/open.go` / wherever `navExit` maps `linknav.ErrNoLine`
- Test: `internal/linknav/linknav_test.go`, `internal/tui/steer_nav_test.go` (or the file holding `steerCommandForLink`'s tests), `internal/cli/open_test.go`

**Interfaces:**
- Consumes: nothing from a prior task — this is first.
- Produces: `linknav.Command` may now return a `steer.Command` whose `File` is
  set and whose `Line` is nil. Task 2's ref/pair arms must preserve that: a
  ref or pair link with a path and no line is the same shape.

- [ ] **Step 1: Write the failing tests**

In `internal/linknav/linknav_test.go`, beside the existing `TestCommandShapes`:

```go
// TestFileLinkWithNoLineOpensTheFile pins R1: `gg link <path>` emits a link
// with no line, and Command must turn it into a navigate that names the file
// and leaves the cursor alone — not the old ErrNoLine refusal, which made
// `gg open` reject a link `gg link` had just produced.
func TestFileLinkWithNoLineOpensTheFile(t *testing.T) {
	t.Parallel()
	dir, svc, sha := navRepo(t) // the helper TestCommandShapes uses
	_ = sha
	res := resolve(t, svc, model.Link{
		Repo: model.LinkRepo{Abs: abs(dir)}, Path: "a.txt",
		Target: model.LinkTarget{State: model.StateUnstaged},
		Side:   model.NoteSideNew,
	}.String())
	c, err := linknav.Command(context.Background(), svc, res)
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	if c.Cmd != "navigate" || c.File != "a.txt" {
		t.Fatalf("command = %+v, want a navigate naming a.txt", c)
	}
	if c.Line != nil {
		t.Errorf("Line = %+v, want nil (open the file, do not move the cursor)", c.Line)
	}
	if c.Target == nil || c.Target.State != "unstaged" {
		t.Errorf("target = %+v, want the working tree", c.Target)
	}
}
```

Add the commit-target twin in the same test (`@<sha>` with a path and no line →
`Target.State == "commit"`, `Line == nil`), because that is the shape
`gg link <path> --rev <sha>` emits and the one the user actually hit.

In the TUI's `steerCommandForLink` test file:

```go
// TestSteerCommandForLinkAcceptsALineLessFile pins the OTHER producer: the
// --at startup gate (gg open with no live session launches the TUI on the
// link). It refused l.Line < 1, so a line-less link launched gg nowhere in
// particular instead of on the file.
func TestSteerCommandForLinkAcceptsALineLessFile(t *testing.T) {
	t.Parallel()
	c, ok := steerCommandForLink(model.Link{
		Repo: model.LinkRepo{Abs: "/repo"}, Path: "a.txt",
		Target: model.LinkTarget{State: model.StateUnstaged},
		Side:   model.NoteSideNew,
	})
	if !ok {
		t.Fatal("steerCommandForLink refused a line-less file link")
	}
	if c.File != "a.txt" || c.Line != nil {
		t.Fatalf("command = %+v, want File a.txt and no Line", c)
	}
}
```

And an end-to-end one in `internal/cli/open_test.go` asserting `gg open` on the
link `gg link` itself prints exits 0 and posts a navigate naming the file with
no line — the round trip whose absence is the bug.

- [ ] **Step 2: Run them and watch each fail**

```
rtk proxy go test ./internal/linknav/ -run TestFileLinkWithNoLineOpensTheFile -v
rtk proxy go test ./internal/tui/ -run TestSteerCommandForLinkAcceptsALineLessFile -v
rtk proxy go test ./internal/cli/ -run TestOpen -v
```
Expected: the linknav one fails with `that link names a file but no line`, the
TUI one with `steerCommandForLink refused`, the CLI one with exit 2.

- [ ] **Step 3: Delete the two refusals in `linknav.Command`**

Both arms end the same way today:

```go
	if line.No < 1 {
		return steer.Command{}, ErrNoLine
	}
	c.Line = &line
	return c, nil
```

Replace each with:

```go
	// A link with no line is a link to the FILE: the consumer opens its diff
	// and leaves the cursor where it was (steer.Command.Line's contract). It
	// used to be ErrNoLine, which made `gg open` refuse the very link
	// `gg link <path>` prints — the producer and the navigator disagreeing
	// about what a valid link is.
	if line.No > 0 {
		c.Line = &line
	}
	return c, nil
```

Then delete `ErrNoLine` from the `var` block and its doc sentence, and fix
every reference the compiler finds (`navExit`'s mapping in `internal/cli`, any
`errors.Is` in a test). Leave `ErrRepoOnly` exactly as it is: a repo-only link
genuinely names no place, and `gg open` already handles it by launching gg
there.

- [ ] **Step 4: Drop the `l.Line < 1` refusals in `steerCommandForLink`**

Two sites: the preview arm and the general arm. In the preview arm, keep the
`l.Hunk > 0` refusal at the top of the function (a hunk must already have been
lowered) and replace

```go
		if l.Line < 1 {
			return steer.Command{}, false
		}
		c.File = l.Path
		c.Line = &steer.Line{Side: "new", No: l.Line} // a preview has no old side
```

with

```go
		c.File = l.Path
		if l.Line > 0 {
			c.Line = &steer.Line{Side: "new", No: l.Line} // a preview has no old side
		}
```

and, in the general arm, set `c.Line` only when `l.Line > 0`, keeping the side
computation inside that branch.

- [ ] **Step 5: Record the rule on the protocol**

In `internal/steer/steer.go`, on `Command`:

```go
	// Line is where to land inside the file. NIL means "open File and leave
	// the cursor alone" — the shape `gg link <path>` produces, which every
	// consumer already honours (tui.steerNavigateStatusFile,
	// tui.drainPendingLoad, live.js steerNavigate). A navigate naming a file
	// is never refused for want of a line.
	Line    *Line    `json:"line,omitempty"`
```

- [ ] **Step 6: Run them and watch them pass**

```
rtk proxy go test ./internal/linknav/ ./internal/tui/ ./internal/cli/ ./internal/steer/
```

- [ ] **Step 7: Prove the tests are not vacuous**

Re-insert one `return steer.Command{}, ErrNoLine`, re-run the linknav test,
confirm it fails, remove it again. Do the same for one `l.Line < 1`.

- [ ] **Step 8: Commit**

```bash
git add internal/linknav internal/tui internal/steer internal/cli
git commit -m "fix(linknav): a file link with no line opens the file, it does not refuse

gg link <path> emits gg://repo/path with no line and gg open refused it —
the producer and the navigator disagreeing about what a valid link is, one
layer below where linkRoundTrips guards the same class. Both consumers
already implemented 'File set, Line nil = open it and do not move the
cursor'; only the two producers refused. ErrNoLine is deleted: after this
nothing can produce it."
```

---

### Task 2: `ref:` and `<a>..<b>` become resolvable places

`domain.ResolveLink` refuses both shapes outright today (`linkresolve.go`, the
guard on `l.Target.Ref != "" || l.Target.Pair != nil`). That refusal was a
deliberate plan-1b deferral; this task lifts it and carries the two shapes all
the way to a `steer.Command`. **No consumer changes here** — Tasks 3 and 4 own
those, and the wire must exist before they can read it.

**Files:**
- Modify: `internal/steer/steer.go` — `Target` grows `Ref`, `A`, `B`
- Modify: `internal/domain/linkresolve.go` — `Resolved.Ref`, `Resolved.Pair`; `refCandidates`; the guard replaced by real handling in `finishLink`
- Modify: `internal/linknav/linknav.go` — `Command`'s ref and pair arms; `AtLink`
- Test: `internal/domain/linkresolve_test.go`, `internal/linknav/linknav_test.go`, `internal/steer/steer_test.go`

**Interfaces:**
- Consumes: Task 1's rule that `Command` may return `Line == nil`.
- Produces, for Tasks 3–5:
  ```go
  // internal/steer
  type Target struct {
      State  string // … | "ref" | "pair"
      Commit string
      Source string // preview only
      Target string // preview only
      Ref    string // set iff State == "ref": the branch or tag NAME
      A, B   string // set iff State == "pair": the change-set's two halves
  }

  // internal/domain
  type Resolved struct {
      // … existing fields …
      // Ref is the branch or tag NAME when the link named a tip
      // (@ref:<name>). Commit and Addr.Commit carry the tip as it resolved
      // HERE, so a caller that wants an address has one — but the NAME is
      // what travels, and every consumer re-resolves it (ruling R2).
      Ref string
      // Pair is the change-set when the link named one (@<a>..<b>), each half
      // resolved to a FULL sha on the chosen checkout. Commit and Addr.Commit
      // carry B: a change-set's newer end is the only single commit it has,
      // and a consumer that needs one (a note, a `gg show`) is refused by
      // ruling R4 rather than silently handed it.
      Pair *model.LinkPair
  }
  ```

- [ ] **Step 1: Write the failing resolver tests**

In `internal/domain/linkresolve_test.go`:

```go
// TestResolveLinkRefYieldsTheTipAndKeepsTheName pins R2: a @ref: link resolves
// so a consumer can navigate it, and the NAME survives resolution — freezing
// the tip at resolve time and dropping the name is exactly what the preview
// lane refuses to do.
func TestResolveLinkRefYieldsTheTipAndKeepsTheName(t *testing.T) {
	t.Parallel()
	dir := gittest.BasicRepo(t, "hello\n")
	svc := Open(dir)
	head, _, err := svc.ResolveRev(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}
	l, err := model.ParseLink(model.Link{
		Repo:   model.LinkRepo{Abs: filepath.ToSlash(dir)},
		Target: model.LinkTarget{State: model.StateCommitted, Ref: "main"},
		Side:   model.NoteSideNew,
	}.String())
	if err != nil {
		t.Fatal(err)
	}
	res, err := ResolveLink(context.Background(), l, ResolveOpts{Cwd: svc})
	if err != nil {
		t.Fatalf("ResolveLink: %v", err)
	}
	if res.Ref != "main" {
		t.Errorf("Ref = %q, want main", res.Ref)
	}
	if res.Commit != strings.TrimSpace(head) {
		t.Errorf("Commit = %q, want the tip %q", res.Commit, head)
	}
}
```

Plus three more, each of which must be seen failing:

1. `TestResolveLinkPairResolvesBothHalves` — `@main..<sha>` yields
   `Pair.A`/`Pair.B` as FULL shas and `Commit == Pair.B`.
2. `TestResolveLinkRefSkipsACheckoutWithoutTheBranch` (R3) — two registry
   checkouts of one repo, the branch only in the SECOND, the first more
   recently opened. The resolve must land on the second. **This is the test
   that earns Task 2**: without `refCandidates` it picks the first and reports
   a tip from the wrong checkout.
3. `TestResolveLinkPairRefusedWhenNoCheckoutHoldsBothHalves` — the error names
   the repo and both halves, mirroring the preview refusal's words.

- [ ] **Step 2: Run them, watch all four fail**

```
rtk proxy go test ./internal/domain/ -run 'TestResolveLink(Ref|Pair)' -v
```
Expected: `a branch-tip or change-set link cannot be navigated yet` for each.

- [ ] **Step 3: Add the candidate filters (R3)**

In `locateLink`, immediately after the existing preview filter and **before**
the cwd short-circuit, add the ref/pair filter. It runs unconditionally for
the same reason `previewCandidates` does: "the cwd has the repo but not the
branch" is the common case, not the tie-break case.

```go
	// A ref or pair link names REFS, so a checkout that cannot resolve them
	// cannot show it. Like previewCandidates this runs ALWAYS, not only on a
	// tie: a ref link is StateCommitted with an EMPTY Commit, so it skips the
	// containment filter below (Commit != "" is load-bearing there) and would
	// otherwise take the MRU checkout whether or not it holds the branch.
	if names := linkTargetRefs(l); len(names) > 0 {
		kept := resolvingAll(ctx, cands, names, opts)
		if len(kept) == 0 {
			return linkCandidate{}, fmt.Errorf("%w: no checkout of %s resolves %s", ErrLinkUnknownRepo, linkRepoLabel(l), strings.Join(names, " and "))
		}
		cands = kept
	}
```

with two helpers beside `previewCandidates`:

```go
// linkTargetRefs lists the ref names a link's TARGET needs resolved on the
// checkout that shows it: the tip for @ref:<name>, both halves for @<a>..<b>.
// A preview has its own filter (its halves are branch names by definition);
// every other target names at most a sha, which containment already covers.
func linkTargetRefs(l model.Link) []string {
	if l.Target.Ref != "" {
		return []string{l.Target.Ref}
	}
	if p := l.Target.Pair; p != nil {
		return []string{p.A, p.B}
	}
	return nil
}

// resolvingAll keeps the candidates on which EVERY name resolves. ResolveRev
// is the same probe containing() and previewCandidates() use, and a candidate
// whose probe errors is simply not a candidate, exactly as there.
func resolvingAll(ctx context.Context, cands []linkCandidate, names []string, opts ResolveOpts) []linkCandidate {
	var kept []linkCandidate
	for _, c := range cands {
		svc := opts.OpenFn(c.checkout)
		ok := true
		for _, n := range names {
			if _, found, err := svc.ResolveRev(ctx, n); err != nil || !found {
				ok = false
				break
			}
		}
		if ok {
			kept = append(kept, c)
		}
	}
	return kept
}
```

- [ ] **Step 4: Replace the refusal with resolution**

Delete the `l.Target.Ref != "" || l.Target.Pair != nil` guard from
`ResolveLink` (keep its comment's reasoning in `finishLink`'s new arms), and in
`finishLink`'s `case model.StateCommitted:` add, before the preview arm:

```go
		if r := l.Target.Ref; r != "" {
			// The NAME is what travels and what every consumer re-resolves
			// (ruling R2); the tip is resolved here only so a caller wanting
			// an ADDRESS has one. ResolveRev peels to ^{commit} and expands to
			// the full sha — the same call the commit arm makes.
			full, found, err := opts.OpenFn(c.checkout).ResolveRev(ctx, r)
			if err != nil {
				return Resolved{}, err
			}
			if !found {
				return Resolved{}, fmt.Errorf("%w: %s does not resolve %s", ErrLinkUnknownRepo, c.checkout, r)
			}
			r2 := strings.TrimSpace(full)
			res.Ref, res.Commit, res.Addr.Commit = r, r2, r2
			return res, nil
		}
		if p := l.Target.Pair; p != nil {
			svc := opts.OpenFn(c.checkout)
			a, aok, aerr := svc.ResolveRev(ctx, p.A)
			b, bok, berr := svc.ResolveRev(ctx, p.B)
			if aerr != nil {
				return Resolved{}, aerr
			}
			if berr != nil {
				return Resolved{}, berr
			}
			if !aok || !bok {
				return Resolved{}, fmt.Errorf("%w: %s does not resolve %s..%s", ErrLinkUnknownRepo, c.checkout, p.A, p.B)
			}
			// Commit carries B — the change-set's newer end and its only
			// single commit. Ruling R4 refuses the verbs that would anchor on
			// it rather than let a bounded set widen into the tree at B.
			res.Pair = &model.LinkPair{A: strings.TrimSpace(a), B: strings.TrimSpace(b)}
			res.Commit, res.Addr.Commit = res.Pair.B, res.Pair.B
			return res, nil
		}
```

(Rename the local `r := Resolved{…}` to `res` in `finishLink` if it collides —
do it in one mechanical pass so the diff stays readable.)

- [ ] **Step 5: Run the resolver tests, watch them pass**

- [ ] **Step 6: Extend `steer.Target` and `linknav.Command`**

Add `Ref`, `A`, `B` to `steer.Target` with `json:",omitempty"` tags and the doc
sentences from the Interfaces block. Then, in `linknav.Command`, add the two
arms **before** the `res.Addr.Path == ""` test, mirroring the preview arm's
shape exactly (the pair rides as NAMES, the consumer resolves):

```go
	if res.Ref != "" {
		// The NAME rides the wire, never the tip: the consumer resolves it
		// itself, so a tip that moved between post and apply is honoured —
		// the rule the preview arm above already follows.
		c.Target = &steer.Target{State: "ref", Ref: res.Ref}
		if res.Addr.Path == "" {
			return c, nil // reveal the tree at that tip
		}
		c.File = res.Addr.Path
		// A ref is a POINT, so its file diff is the commit lane's: lower a
		// hunk against the resolved tip, exactly as the commit arm does.
		…
	}
	if p := res.Pair; p != nil {
		c.Target = &steer.Target{State: "pair", A: p.A, B: p.B}
		if res.Addr.Path == "" {
			return c, nil // open the compare
		}
		c.File = res.Addr.Path
		…
	}
```

For the hunk lowering in both arms, reuse `HunkLine` with the resolved rev
(`res.Commit`) rather than inventing a third lowering path; the pair arm's rev
is `p.B`, which is what `res.Commit` already holds.

- [ ] **Step 7: Teach `AtLink` the two shapes**

`AtLink` rebuilds a `model.Link` for the TUI launcher. Add:

```go
	case res.Ref != "":
		l.Target = model.LinkTarget{State: model.StateCommitted, Ref: res.Ref}
	case res.Pair != nil:
		l.Target = model.LinkTarget{State: model.StateCommitted, Pair: res.Pair}
```

as branches of the existing preview/else decision, keeping the `Side`/`Line`
handling below untouched.

- [ ] **Step 8: Tests for the wire shapes, then commit**

`internal/linknav/linknav_test.go`: a table asserting the four commands — ref
with and without a path, pair with and without a path — and that `AtLink`
round-trips each back to a link whose `String()` equals the input.
`internal/steer/steer_test.go`: the new fields survive `Post`/`Drain`.

```bash
git add internal/steer internal/domain internal/linknav
git commit -m "feat(domain): a @ref: tip and a @a..b change-set resolve and reach the wire

ResolveLink refused both by design in plan 1b; navigation is this plan's
seam. The NAME travels and the consumer re-resolves it (the rule the
preview lane set), and locateLink now filters candidates by whether they
resolve the names at all — a ref link is StateCommitted with an empty
Commit, so it skipped the containment filter and would have taken the MRU
checkout whether or not it held the branch."
```

---

### Task 3: the TUI navigates a tip and a change-set

**Files:**
- Modify: `internal/tui/steer_nav.go` — `steerNavigate`'s switch, `steerNavigateRef`, `steerNavigatePair`
- Modify: `internal/tui/steer_nav.go` — `steerCommandForLink` grows the ref/pair arms (the `--at` launcher path)
- Modify: `internal/i18n/*.toml` (all four bundles) — the new status strings
- Test: `internal/tui/steer_nav_test.go` (or the file holding the existing navigate tests)

**Interfaces:**
- Consumes: `steer.Target{State:"ref", Ref}` and `{State:"pair", A, B}` from Task 2.
- Produces: nothing later tasks consume.

**The two landings, and why:**

- **`ref`** — a tip is a POINT: the whole tree there. The tip is resolved HERE,
  in the consumer (`domain.ResolveRev` through the service), never taken from
  the wire. With a file it is the commit-file lane verbatim.

  **With NO file it opens the files view BY HASH for both origins** — do not
  delegate to `steerNavigate`'s `c.Commit` arm as written. That arm opens the
  files view only when `startAtOrigin(c)` and answers an agent's navigate with
  `"commit not loaded in the feed"`, a deliberate shipped ruling: the agent
  asked for a feed ROW, and moving it into a files view is not that. **A branch
  tip is precisely the commit least likely to be paged in**, so inheriting that
  refusal would make `gg session navigate gg://repo@ref:feat/x` fail most of
  the time. The reasoning does not transfer: a ref names a TREE, not a feed
  row, so `openChangedFiles(model.Commit{Hash: hash})` is the honest landing
  for either origin.
  *Cost if wrong:* an agent that wanted the feed row gets the files view — the
  same content, a different panel.
- **`pair`** — a change-set is BOUNDED: it is a comparison.
  `openCompareFiles(model.CommitEndpoint(a), model.CommitEndpoint(b))` is
  exactly `git diff a b`, and `endpointComparable` already admits
  `EndpointCommit`. **Never `model.PairEndpoint`** here: an endpoint pair is one
  bounded SET, and the compare view wants the two sides that produce it.

- [ ] **Step 1: Write the failing tests**

Four, in the TUI's navigate test file, each driving `applySteer` on a real
fixture repo and asserting the resulting model:

```go
// TestSteerNavigateRefOpensTheTipsFiles pins R2's consumer half: the wire
// carries the NAME, and the TUI resolves it — so a branch that moved between
// post and apply lands on the NEW tip, not a stale sha the poster captured.
func TestSteerNavigateRefOpensTheTipsFiles(t *testing.T) { … }

// TestSteerNavigateRefResolvesAtApplyTime advances the branch AFTER building
// the command and asserts the landing is the new tip. This is the test that
// earns the name-on-the-wire ruling; freezing the sha passes every other test
// in this file.
func TestSteerNavigateRefResolvesAtApplyTime(t *testing.T) { … }

// TestSteerNavigatePairOpensACompare asserts filesModeCompare and the two
// endpoints, and that a pair NEVER reaches openCompareFiles as a PairEndpoint
// (CacheTag would key the cache on one side of a two-sided view).
func TestSteerNavigatePairOpensACompare(t *testing.T) { … }

// TestSteerNavigateRefUnknownBranchRefuses asserts an English protocol
// refusal naming the branch, and that nothing in the model moved.
func TestSteerNavigateRefUnknownBranchRefuses(t *testing.T) { … }
```

Fill each body against the existing tests' fixture helpers — do not invent a
new harness. Every refusal must be asserted to leave the view untouched:
`steerNavigate`'s own rule is that a refusal is decided BEFORE anything moves.

- [ ] **Step 2: Run them, watch all four fail** (`navigate needs a file, a commit or a step`)

- [ ] **Step 3: Add the two cases to `steerNavigate`'s switch**

Insert them beside the preview case, before the `c.File != ""` cases:

```go
	case c.Target != nil && c.Target.State == "ref":
		return m.steerNavigateRef(c)
	case c.Target != nil && c.Target.State == "pair":
		return m.steerNavigatePair(c)
```

- [ ] **Step 4: Implement `steerNavigateRef`**

Resolve, then delegate — no new landing code:

```go
// steerNavigateRef lands a @ref:<name> navigate. The wire carries the NAME
// (ruling R2), so the tip is resolved HERE: a branch that moved between post
// and apply lands on the new tip. Once resolved it is an ordinary commit
// navigate, so it delegates rather than duplicating either landing.
func (m Model) steerNavigateRef(c steer.Command) (Model, tea.Cmd) {
	name := c.Target.Ref
	if name == "" {
		return m, m.answerSteer(c, steerFail(c, "target.state \"ref\" needs target.ref"))
	}
	// A DEADLINED read on the Update thread: the ff-pull lane does not set
	// m.running, so a background fetch can hold the gate with opsIdle() still
	// true. updateThreadCtx is the seam plan 1b added for exactly this.
	ctx, cancel := updateThreadCtx(updateThreadGitTimeout)
	defer cancel()
	sha, ok, err := m.svc.ResolveRev(ctx, name)
	if err := busyOr(err); err != nil {
		return m, m.answerSteer(c, steerFail(c, "resolving "+name+": "+err.Error()))
	}
	if !ok {
		return m, m.answerSteer(c, steerFail(c, name+" does not resolve here"))
	}
	hash := strings.TrimSpace(sha)
	// Reuse the commit lanes verbatim: the ref is spent.
	nc := c
	nc.Target = &steer.Target{State: "commit", Commit: hash}
	if nc.File != "" {
		return m.steerNavigateCommitFile(nc)
	}
	// NOT steerNavigate(nc) with nc.Commit set: that arm refuses an agent's
	// navigate for a commit the feed has not paged in, and a branch tip is the
	// commit least likely to be paged in. A ref names a TREE, so open it by
	// hash for either origin (see the landing note above).
	nm := m.steerToPanels()
	nm, cmd := nm.openChangedFiles(model.Commit{Hash: hash})
	nm.focus = panelCommits
	nm = nm.focusTree()
	return nm, tea.Batch(cmd, nm.answerSteer(c, steerOK(c, "opened "+name+" at "+shortHash(hash))))
}
```

Note the deadline: this is a synchronous git call on the Bubble Tea Update
thread, the exact hazard `internal/tui/update_thread_git.go` was created for in
plan 1b. Use `updateThreadCtx` + `busyOr`; do not use `context.Background()`.

- [ ] **Step 5: Implement `steerNavigatePair`**

```go
// steerNavigatePair lands a @<a>..<b> navigate: a change-set is BOUNDED, so it
// is a COMPARISON, and the compare files view is where a comparison lives. The
// halves arrive as names or shas and are resolved here for the same reason a
// ref is (ruling R2).
//
// CommitEndpoint on each half, never PairEndpoint: an endpoint pair is one
// bounded SET, and openCompareFiles wants the two sides that produce it.
func (m Model) steerNavigatePair(c steer.Command) (Model, tea.Cmd) { … }
```

Resolve both halves under one `updateThreadCtx`, refuse with an English
protocol message naming the unresolved half, build the two endpoints with
`model.CommitEndpoint` (which validates ≥7 hex — a resolved full sha always
passes), then `m.steerToPanels()` and `openCompareFiles`. When `c.File != ""`,
park a `pendingSteer` at `steerStageFiles` the way `steerNavigateCommitFile`
does so `drainPendingLoad` opens the file — and note that `drainPendingLoad`
already honours a nil `c.Line` (Task 1), so a line-less pair-plus-file link
needs nothing extra.

- [ ] **Step 6: `steerCommandForLink` — the `--at` launcher path**

`gg open` with no live session launches the TUI positioned on the link, and the
converter is PURE (no repository). A ref or pair link therefore cannot be
lowered there. Add both arms returning the command with the NAMES on it — the
resolution happens in `steerNavigateRef`/`Pair` when the started TUI applies
it, which is the same place a live session resolves.

- [ ] **Step 7: i18n**

Every new user-visible string gets a literal key in ALL FOUR bundles. Candidates:
`"▸ opened %s"` already exists and covers the ref landing; the pair landing
needs `"▸ opened %s..%s"`. Run the gates:

```
rtk proxy go test ./internal/tui/ -run 'TestI18n|TestOptionsVocab|TestMenuLabels|TestEngineProse|Bundle'
```
(then the full `./internal/tui/` package, which is where the four AST gates live).

- [ ] **Step 8: Run, prove non-vacuous, commit**

Revert the resolve-at-apply-time call to a wire-carried sha and confirm
`TestSteerNavigateRefResolvesAtApplyTime` fails — that is the one test the
whole ruling rests on.

---

### Task 4: the web navigates a tip and a change-set

**Files:**
- Modify: `internal/web/steer.go` — the wire validator and `toSteerWire`
- Modify: `internal/web/static/live.js` — `steerNavigate`'s branch chain
- Test: `internal/web/steer_test.go`, `internal/web/static/*_test` gate (whichever pins the JS)

**Interfaces:** consumes Task 2's `steer.Target` fields; produces nothing.

The web's wire is a FLATTENED shape (`toSteerWire` lifts `Target.State` to
`w.State`), so both halves change together: the validator must admit
`state: "ref"` with a non-empty `ref`, and `state: "pair"` with non-empty `a`
and `b`, and must keep refusing each with its field missing. Mirror the existing
`"a commit target needs a commit"` refusal's wording.

- [ ] **Step 1: Write the failing tests** — `steerPost` with each new state,
  asserting 200 and the flattened body; plus the two missing-field refusals and
  an argument-injection case (`ref: "--upload-pack=evil"`) matching the existing
  `"navigate","commit":"-x"` row. A ref name starting with `-` must be refused
  by the SAME guard the commit case uses; if that guard is commit-specific,
  widen it and say so in its comment.
- [ ] **Step 2: Run, watch them fail** (`unknown state "ref"`).
- [ ] **Step 3: Extend the validator** — add the two states to the `w.State`
  allow-list and their field checks to the `case "navigate":` block, beside
  `if w.State == "commit" && w.Commit == ""`.
- [ ] **Step 4: Extend `live.js`'s `steerNavigate`** — two branches before the
  `!s.file` one:
  ```js
  } else if (s.state === "ref") {
    // The NAME, never a sha: the tip is resolved here, so a branch that moved
    // between post and apply is honoured (the TUI consumer does the same).
    const sha = await resolveRefTip(s.ref);
    if (!sha) return;
    await openCommitByHash(sha, s.ref);
    …
  } else if (s.state === "pair") {
    await openCompareForPair(s.a, s.b);
    …
  }
  ```
  `resolveRefTip` and `openCompareForPair` do not exist yet. Find the existing
  API the branch list already uses to learn a tip (`/api/branches` carries
  them) and reuse it rather than adding an endpoint; for the compare, reuse
  whatever `openPreviewForPair` calls underneath — a preview IS a
  three-dot pair, so the compare renderer is already there and a two-dot pair
  differs only in the range it asks for. If it turns out `/api/compare` cannot
  express a two-dot range, add the parameter to the EXISTING handler; do not
  create a second compare endpoint.
- [ ] **Step 5: Keep the line-less rule** — `if (!s.line) return;` at the end
  already covers both new branches. Assert it in a test rather than assume it.
- [ ] **Step 6: Run the package, prove non-vacuous, commit.**

**Verification that is NOT a Node gate (carry into the task review):** this task
changes the browser path, and plan 1b shipped `links.js` twice with only a
Node-level gate behind it. The task is not done until a real browser has
steered a live `gg web` page with a `@ref:` link and a `@a..b` link and the
page has been asserted to show the right thing — per
[[browser-check-must-assert-visibility]], run it against the UNFIXED build
first and watch it fail.

---

### Task 5: the six CLI verbs decide, each for itself

`resolveLinkArg` is shared by `gg diff`, `gg show`, every `gg note` verb,
`gg open` and `gg session navigate`/`highlight`. After Task 2 it starts
returning ref and pair resolutions to all six at once, and three of them must
refuse (ruling R4). A shared resolver with a per-verb allow-list, so the
decision is in ONE table rather than six scattered guards.

**Files:**
- Modify: `internal/cli/link.go` — `resolveLinkArg` grows a shape allow-list
- Modify: `internal/cli/linkconsume.go` — `linkDiffSpec` grows the PAIR branch
- Modify: `internal/cli/diff.go`, `show.go`, `note.go`, `open.go`, `session.go` — pass their allowance
- Test: `internal/cli/link_shapes_test.go` (**create**), `internal/cli/diff_test.go`

**Interfaces:**
```go
// linkShapes is what one verb accepts. A verb names its own allowance so the
// refusal is decided in one table (ruling R4) rather than in six guards that
// can drift apart — which is exactly how gg shipped a preview link that four
// verbs read and two did not.
type linkShapes struct {
	Ref  bool // @ref:<name> — a tip is a single commit, so most verbs take it
	Pair bool // @<a>..<b> — BOUNDED; a verb needing one commit must refuse it
}

func resolveLinkArgShapes(ctx context.Context, svc *domain.Service, s string, allow linkShapes) (domain.Resolved, error)
```

`resolveLinkArg` keeps its signature and calls the new one with
`linkShapes{Ref: true}` — the REFUSING default (R4). A verb that wants pairs
opts UP explicitly, at its own call site, so a seventh verb added later refuses
a bounded shape rather than silently widening it. No test can catch a verb that
does not exist yet; the default can.

- [ ] **Step 1: Write the failing table test**

One table over all six verbs × both new shapes, run against a real repo, each
row asserting the exit code AND that a refusal's prose names `gg compare`:

```go
// TestLinkShapesPerVerb is ruling R4 as a table. A pair's only single commit
// is B, and anchoring a note or a `gg show` there would silently widen a
// BOUNDED change-set into the whole tree at B — the same mistake
// sideLosesItsKeySet refuses per side in `gg compare --patch`.
func TestLinkShapesPerVerb(t *testing.T) {
	cases := []struct {
		verb     []string // argv after the link is substituted
		refOK    bool
		pairOK   bool
	}{
		{verb: []string{"diff"}, refOK: true, pairOK: true},
		{verb: []string{"show"}, refOK: true, pairOK: false},
		{verb: []string{"note", "list"}, refOK: true, pairOK: false},
		{verb: []string{"open"}, refOK: true, pairOK: true},
		{verb: []string{"session", "navigate"}, refOK: true, pairOK: true},
		{verb: []string{"session", "highlight", "add"}, refOK: true, pairOK: false},
	}
	…
}
```

A refused shape exits **2** (a caller mistake, the shipped convention), not 1.

- [ ] **Step 1b: Write the failing RANGE test** — the defect the allow-list
  hides. Against a repo with three commits `c1 → c2 → c3`, each touching a
  DIFFERENT file, assert `gg diff gg://<abs>@<c1>..<c3>` names the files c2 and
  c3 changed and NOT only c3's:

```go
// TestDiffPairLinkDiffsTheRangeNotJustB is the silent-wrong-answer guard for
// ruling R4's diff row. domain.Resolved.Pair carries both halves, but
// Resolved.Commit and Addr.Commit carry B alone — so a pair link that merely
// PASSES the shape gate falls through linkDiffSpec's StateCommitted arm and
// prints B's own change (B^..B) at exit 0. Three commits, three files: the
// wrong answer names one file, the right answer names two.
func TestDiffPairLinkDiffsTheRangeNotJustB(t *testing.T) { … }
```

  Also assert the `--hunks` form over the same range, since that is the path a
  note's numbering would inherit.

- [ ] **Step 2: Run them, watch every `pairOK: false` row and the range test
  fail.** The range test is the one that fails with exit 0 and wrong output —
  see it do exactly that before fixing it.
- [ ] **Step 3: Implement `resolveLinkArgShapes`** — after `domain.ResolveLink`
  returns, check the resolution's shape against `allow` and return a wrapped
  `model.ErrLink` naming the verb's own limit and pointing at `gg compare`:
  ```go
	if res.Pair != nil && !allow.Pair {
		return domain.Resolved{}, fmt.Errorf("%w: a change-set link names what changed between two commits, not one place to %s; hand it to `gg compare`", model.ErrLink, verb)
	}
  ```
  Thread the verb's own name in so the message is the verb's, not the
  resolver's. `linkExit` already maps `model.ErrLink` to exit 2.
- [ ] **Step 3b: Give `linkDiffSpec` the pair branch** — immediately after the
  preview branch, before the `switch res.Addr.State`:

```go
	// A CHANGE-SET link diffs its RANGE. Addr.Commit carries B alone (the only
	// single commit a pair has), so falling through to the StateCommitted arm
	// below would print B^..B — B's own change — at exit 0. HunkDiffSpec
	// passes a rev containing ".." straight through, which is the same lane
	// `gg diff <a>..<b>` already takes.
	if p := res.Pair; p != nil {
		return svc.HunkDiffSpec(ctx, false, p.A+".."+p.B, paths)
	}
```

  A `@ref:` link needs NO branch: `Addr.Commit` is the resolved tip and the
  `StateCommitted` arm gives `tip^..tip`, which is the ruling (R4's last
  paragraph). Say so in a comment there so the next reader does not "fix" it.

- [ ] **Step 4: Pass each verb's allowance at its call site**, one line each.
  Only `diff`, `open` and `session navigate` pass `Pair: true`; the other three
  pass nothing and inherit the refusing default.
- [ ] **Step 5: Run, prove non-vacuous** (flip one row's allowance and watch the
  table catch it; revert step 3b's branch and watch the range test print one
  file), **commit.**

---

### Task 6: the hint is honoured, and degrades with a notice

Spec §3.3's three rules, of which only the first is implemented: compare
already ignores the hint (plan 1b). This task does rules 2 and 3.

**Files:**
- Modify: `internal/steer/steer.go` — `Command` grows `HintKind`, `HintID`
- Modify: `internal/domain/linkresolve.go` — `Resolved.Hint model.LinkHint`
- Modify: `internal/linknav/linknav.go` — `Command` carries the hint through
- Modify: `internal/tui/steer_nav.go` — reveal the bookmark or shelf row
- Modify: `internal/web/steer.go`, `internal/web/static/live.js` — the same
- Test: all four packages

**Interfaces:**
```go
// internal/steer
// HintKind/HintID name the UI surface the link was copied from ("bookmark" or
// "shelf"). They never change WHERE the command lands — only which surface is
// revealed there — and a consumer that cannot find the entry lands anyway
// (spec §3.3 rule 3: the hint degrades, it never fails).
HintKind string `json:"hint_kind,omitempty"`
HintID   string `json:"hint_id,omitempty"`
```

- [ ] **Step 1: Write the failing tests** — for each consumer: a hint naming a
  PRESENT entry reveals its row; a hint naming an ABSENT one still lands on the
  address and reports a notice (never `steerFail`); and — the case worth its own
  test — the address-less `gg://<repo>?shelf=<id>` form with the entry absent is
  a HARD error (spec §6: the hint was the only content source). That last one
  needs `domain` to distinguish "no address" from "no hint", so assert the
  domain error first and the consumer behaviour second.
- [ ] **Step 2: Run, watch them fail.**
- [ ] **Step 3–5:** carry `Hint` into `Resolved` in `finishLink` (it is a pure
  copy from `l.Hint`), onto the wire in `linknav.Command`, and into each
  consumer's landing as a post-landing reveal — **after** the navigate has
  landed, so a missing entry cannot prevent the landing. Use the existing
  bookmark/shelf reveal paths (the sidebar row selection the `.` menu already
  uses); do not add a second way to select those rows.
- [ ] **Step 6: i18n for the notice in all four bundles; run the gates; commit.**

---

### Task 7: `internal/linkhist` — the 20-entry MRU

A DAG leaf owned by `domain`. **No frontend may import it** (archtest enforces
the `notes`/`shelf`/`preview`/`bookmark` convention). `internal/searchhist` is
the closest precedent — a per-repo TOML ring with dedup-to-top, read-merge and
temp+rename — and `internal/preview/file_store.go` carries the `O_EXCL` lock
helpers the spec asks for. Copy the ring from the first and the lock from the
second: three processes (TUI, CLI, `gg web`) write this file, and read-merge
alone can still lose a sibling's entry when two rewrites interleave.

**Files:**
- Create: `internal/linkhist/store.go` — the interface + `Entry` + `Max`
- Create: `internal/linkhist/file_store.go` — TOML + lock + ring
- Create: `internal/linkhist/file_store_test.go`

**Interfaces (produced, consumed by Tasks 8–10):**
```go
// Package linkhist is gigagit's per-repo MRU of gg:// links the user has
// COPIED: records only, no blobs. Owned by internal/domain — frontends reach
// it only through domain queries (the notes/shelf/preview/bookmark
// convention).
package linkhist

// Max is the hard ceiling on kept entries. Twenty is a list a human can read
// without scrolling and is the spec's figure (§4.3).
const Max = 20

// Entry is one copied link.
type Entry struct {
	// Link is the canonical link text (model.Link.String()). It is the
	// identity: re-copying the same text moves the row to the top rather than
	// adding one.
	Link string `toml:"link"`
	// Desc is the human label, captured AT CREATION and never derived later
	// (ruling R7): the describing context — which row the user was on — is
	// gone by the time anything lists this. A stash's subject is the only
	// thing that makes its row recognisable, because stash@{N} is
	// deliberately absent from the link.
	Desc string `toml:"desc"`
	// Created is RFC3339. Display-only; order is the slice's.
	Created string `toml:"created"`
}

// Store persists one repo's ring, newest first.
type Store interface {
	// List returns the ring newest-first. Empty when there is none.
	List() ([]Entry, error)
	// Record prepends e (dedup-to-top on Link), trims to Max, and persists
	// atomically under a cross-process lock. A blank Link is a no-op, not an
	// error. An existing row's Desc is REPLACED by the new one: the surface
	// the user copied from this time is the one they will recognise.
	Record(e Entry) error
}
```

- [ ] **Step 1: Write the failing tests** (all `t.Parallel()`, all in `t.TempDir()`):
  1. `List` on a missing file is empty with a nil error.
  2. `Record` then `List` returns the entry.
  3. **Dedup-to-top**: record A, B, A → `[A, B]`, and A's `Desc` is the second one's.
  4. **Cap**: record 25 distinct links → `len == 20` and the OLDEST five are gone.
  5. **Blank link** is a silent no-op.
  6. **Corrupt file** → `List` returns an ERROR, never empty. (Swallowing it
     would let the next `Record` destroy the store — the rule
     `preview.FileStore.read` already states.)
  7. **Concurrent writers**: two goroutines each recording 20 distinct links;
     afterwards exactly 20 remain and every one is well-formed. Run this test
     under `-race`.
- [ ] **Step 2: Run, watch them fail to compile** (the package does not exist).
- [ ] **Step 3: Write `store.go`** exactly as in the Interfaces block.
- [ ] **Step 4: Write `file_store.go`** — `links.toml` under `root`, an
  `index{Links []Entry `toml:"links"`}` wrapper, the lock constants and
  `lock`/`unlock` helpers copied from `internal/preview/file_store.go`
  (`lockWait` 2s, `lockPoll` 20ms, `lockStale` 30s), a process-local
  `sync.Mutex`, read-merge INSIDE the lock, dedup-to-top, `Max` trim, and
  temp+rename via `os.CreateTemp(fs.root, "links-*.toml")`.
- [ ] **Step 5: Run the package, then under `-race`.**
- [ ] **Step 6: Prove non-vacuous** — remove the `Max` trim and watch test 4
  fail; remove the lock and watch test 7 fail under `-race`.
- [ ] **Step 7: Commit.**

---

### Task 8: domain wiring, the producers, and `gg links`

**Files:**
- Create: `internal/domain/linkhiststore.go` — the resolver + the two queries
- Modify: `internal/cli/link.go` — `gg link` records what it prints
- Modify: `internal/cli/compare.go` — a LINK positional is recorded on success (R8)
- Create: `internal/cli/links.go` — `gg links`
- Modify: `cmd/gg/main.go` — route `links`
- Modify: `internal/archtest/*_test.go` — `linkhist` joins the domain-owned-store list
- Test: `internal/domain/linkhiststore_test.go`, `internal/cli/links_test.go`

**Interfaces (produced, consumed by Tasks 9–10):**
```go
// LinkHistStatePath overrides the link-history root. "" uses the default XDG
// location. Tests set it; it is the searchstore.go seam.
var LinkHistStatePath string

func (s *Service) SetLinkHistStore(st linkhist.Store)   // test seam
// RecordLink appends a copied link. BEST-EFFORT: a nil store (no state home)
// or a write error is silently ignored, exactly as RecordSearch is — a
// history that cannot be written must never fail the copy the user asked for.
func (s *Service) RecordLink(ctx context.Context, link, desc string)
// LinkHistory returns the ring newest-first; nil on any failure.
func (s *Service) LinkHistory(ctx context.Context) []linkhist.Entry
```

- [ ] **Step 1: Write the failing tests** — the store resolver keyed by
  `repoKey(commonDir)` under `<state>/gg/linkhist/<key>` (assert
  `$XDG_STATE_HOME` WINS, per the Global Constraints — this is the rule whose
  breach wrote into a developer's real `AppData\Local\gg` once);
  `gg link <path>` records one row whose `Desc` matches the spec §4.3 table for
  that kind; `gg link` printing the SAME link twice leaves one row;
  `gg compare <link> <rev>` records the link on exit 0 and records NOTHING on a
  failure; `gg links` prints the ring newest-first, terse, and prints nothing
  (exit 0) when empty.
- [ ] **Step 2: Run, watch them fail.**
- [ ] **Step 3: Write `linkhiststore.go`** — copy `searchstore.go`'s shape
  verbatim, substituting `"linkhist"` for `"search"` in the base dir and
  `linkhist.NewFileStore` for `searchhist.NewFileStore`. Keep the
  `$XDG_STATE_HOME` → `%LocalAppData%` → `~/.local/state` order and the
  memoising `s.mu` guard.
- [ ] **Step 4: Compose `Desc` at the producer** — a small pure function in
  `internal/cli/link.go` so its table is testable on its own:
  ```go
  // linkDesc is the human label stored with a copied link (ruling R7: captured
  // at creation, never derived at read time). The forms are spec §4.3's table.
  // A commit's subject is truncated to descMax runes so one row stays one line.
  func linkDesc(kind, id, subject string) string
  ```
  with `descMax = 60`. The kinds and their forms: `branch: <name>`,
  `bookmark: <label>`, `shelf: <label>`, `preview: <target>...<source>`,
  `commit: <short> <subject>`, `stash: <subject>`, and for a plain path
  `file: <path>`.
- [ ] **Step 5: Record at both producers** — after `gg link` prints, and in
  `gg compare` for each positional that `isLinkArg` said was a link, only on
  exit 0 (R8). Both calls are best-effort and must not change the exit code.
- [ ] **Step 6: Write `gg links`** — terse, one row per line, `<desc>\t<link>`,
  `--json` for the structured form (matching `gg link resolve --json`'s
  convention). Add it to `linkUsage`'s sibling usage text and to the verb
  router in `cmd/gg/main.go`.
- [ ] **Step 7: Extend archtest** — add `internal/linkhist` to whatever list
  pins "no frontend imports a domain-owned store", and assert it FAILS when a
  frontend imports it (add the import temporarily, run, remove).
- [ ] **Step 8: Write the e2e round trip** — a TOML scenario under
  `e2e/scenarios/`, per the `writing-e2e-scenarios` skill: `gg link <path>` →
  `gg links` (the row is listed) → `gg open <that link>` with no live session
  and the launcher stubbed (exit 0, the launcher received the link). This is
  spec §7's `e2e` row and the round trip whose two halves disagreed — the
  reason Task 1 exists.
- [ ] **Step 9: Run all four packages plus `./e2e/`, prove non-vacuous, commit.**

---

### Task 9: `/api/linkhist`

**The web builds its link strings CLIENT-SIDE** (`internal/web/static/links.js`
`linkFor`/`repoSegment`), so the copy handler must POST the link it just wrote
to the clipboard — there is no server-side moment at which the link exists.
And the history must be SERVED, never stored in the browser: `gg web` binds a
random port every run, which empties `localStorage`
([[web-random-port-kills-localstorage]]).

**Files:**
- Create: `internal/web/linkhist.go` — `GET`/`POST /api/linkhist`
- Modify: `internal/web/static/links.js` — `copyLinkRow` POSTs after a successful copy
- Modify: wherever the web registers routes
- Test: `internal/web/linkhist_test.go`

- [ ] **Step 1: Write the failing tests** — `GET` on an empty store returns
  `{"links":[]}` (an EMPTY ARRAY, not null: the JS iterates it); `POST
  {"link":"gg://…","desc":"…"}` then `GET` returns the row; a POST whose link
  does not parse is **400**, not 500 (run it through `model.ParseLink` — the
  body is wire input); a POST with a blank link is 400; and the loopback +
  Host/Origin guards apply exactly as they do to every other mutating handler
  (assert a cross-origin POST is refused — copy the existing guard test's shape).
- [ ] **Step 2: Run, watch them fail (404).**
- [ ] **Step 3: Implement the handler** — `GET` calls `svc.LinkHistory`, `POST`
  validates with `model.ParseLink` then calls `svc.RecordLink`. Method not
  allowed → 405. **Wrap the body read with the same size cap the other POST
  handlers use**; do not invent a new one.
- [ ] **Step 4: POST from `copyLinkRow`** — after the clipboard write resolves,
  fire-and-forget the POST (`.catch(() => {})`): a history that cannot be
  recorded must never make the copy look failed. Compose `desc` on the client
  with the same forms Task 8's `linkDesc` produces — and pin that agreement in
  a test, the way `model.CommitDateLayout` pins the JS date port.
- [ ] **Step 5: Run the package, prove non-vacuous, commit.**
- [ ] **Step 6: Browser verification** (carry into the review): with a real
  `gg web`, copy a link from a commit row and from a bookmark row, then reload
  the page on a DIFFERENT port and confirm both rows are still listed — that
  last step is the whole reason this is an API and not `localStorage`.

---

### Task 10: the MCP tools

**Files:**
- Create: `internal/mcp/links.go` — the three tools
- Modify: `internal/mcp/server.go` — `s.registerLinkTools(srv)` in `sdkServer`
- Modify: `internal/mcp/compare.go` — `gg_compare_file` accepts a link side
- Modify: `internal/mcp/types.go` — the in/out payload types
- Test: `internal/mcp/links_test.go`

**The three tools:**

| tool | in | out |
|---|---|---|
| `gg_link_resolve` | `{link}` | the resolved place: checkout, path, state, commit, ref, pair, line, side, hunk, hint — the `domain.Resolved` fields, flattened |
| `gg_link_list` | `{}` | this repo's ring, newest first: `[{link, desc, created}]` |
| `gg_compare_links` | `{left, right}` (both `gg://…`) | the changed-file rows, exactly `gg_compare_trees`'s `Files` shape |

- [ ] **Step 1: Write the failing tests** — one per tool against a real repo,
  plus: `gg_link_resolve` on an unparseable link returns a tool ERROR whose
  text wraps the parse refusal (not a 500 and not a panic — plan 1b found the
  web arms promising a 500 for `EndpointInvalid` and panicking instead);
  `gg_compare_links` with two links naming DIFFERENT repositories is refused
  with the spec §9 message (cross-repo compare is deferred); `gg_link_list` on
  an empty store returns an empty array.
- [ ] **Step 2: Run, watch them fail.**
- [ ] **Step 3: Implement** — follow `registerCompareTools`'s shape exactly:
  `sdk.AddTool` with `readOnlyAnnotations()`, `s.repoCheck()` first, an `out`
  value pre-seeded with `Repo: s.repoInfo()` and `Files: []commitFileRow{}` so
  a JSON consumer never sees `null`.
  `gg_compare_links` is `domain.EvalLink` on each side then `CompareSets` —
  **never `CompareFiles`**, for the reason `gg_compare_trees`' comment already
  gives (the algebra is total; `git diff` supports only four forward pairs, and
  one compare frontend disagreeing with another about what a user may ask is
  the bug plan 1b fixed).
- [ ] **Step 4: Link sides on `gg_compare_file`** — extend `fileSideIn` with
  `source: "link"` + `link`, resolved through the same `domain.EvalLink` path.
  Add its row to whatever table pins `fileSideIn`'s sources.
- [ ] **Step 5: Run the package, prove non-vacuous, commit.**

---

### Task 11: the agent skill, and the docs

**Files:**
- Modify: `internal/agentskill/using-gg.md`
- Modify: `internal/agentskill/skill.go` — `Version` 80 → 81
- Modify: `CHANGELOG.md` (always), `README.md` (the CLI surface changed)
- Modify: `CLAUDE.md` — ONE map row for `linkhist`; one clause on the `linknav` row
- Modify: `docs/CLAUDE-details.md` — the per-feature detail, the rulings, the gotchas

- [ ] **Step 1** — `using-gg.md`: `gg links`; `gg open` on a line-less file
  link (it works now); which verbs take `@ref:` and which refuse `@<a>..<b>`
  (R4's table, in prose); the three MCP tools; the link side on
  `gg_compare_file`. **Write what an agent needs to DO, not what changed** —
  this file is a skill, not a changelog.
- [ ] **Step 2** — bump `Version`. The `agentskill` package has a test that
  fails when the markdown changes without the version moving; run it.
- [ ] **Step 3** — CHANGELOG: one entry per user-visible change, in gg's own
  voice. **Every claim must cite the line that makes it true.** Plan 1b shipped
  a CHANGELOG bullet asserting a regression that had never existed on main, and
  the implementer who refused to write it was right; three such errors shipped
  in plan 1a. If you cannot point at the code, cut the sentence.
- [ ] **Step 4** — `CLAUDE.md`: keep the new row to ONE line. Per-feature
  detail goes to `docs/CLAUDE-details.md`, never into `CLAUDE.md`.
- [ ] **Step 5** — Commit. Then tell the human to run `gg init --update`: the
  global skill copy is a FILE ON THEIR MACHINE and nothing in this repo can
  refresh it. They are already two versions behind (77 installed, 80 on main).

---

## Self-review

Run before dispatching Task 1.

**Spec coverage.** §3.3 → Task 6 (rules 2 and 3; rule 1 shipped in 1b).
§4.3 `linkhist` → Tasks 7–9; `savedcompare` → NOT this plan (R9, spec §8 puts
it in plan 3). §4.4 `linkresolve`/`linknav`/`steer`/`agentskill`/`links.js` →
Tasks 2, 3, 4, 6, 9, 11. §5.3 `gg links` → Task 8; `gg compare --save` → plan 3
(it needs `savedcompare`). §5.4 all four MCP items → Task 10. §5.5
`/api/linkhist` → Task 9; the two-field dialog and the Previews tab → plan 3.
§6 the two hint rows → Task 6. §7's `e2e` row → Task 8 step 8
(`gg link` → `gg links` → `gg open`, under `e2e/scenarios/`).

**Placeholder scan.** Tasks 3, 4 and 6 name test functions whose bodies say
"fill against the existing fixture helpers" rather than carrying full code.
That is deliberate and bounded: each of those packages has an established
harness (`applySteer` fixtures, `steerPost`, the four i18n gates) and a made-up
one would be worse than none. Every such step names the helper to use and the
exact assertion. No step says "add error handling", "handle edge cases" or
"similar to Task N".

**Type consistency.** `steer.Target.Ref`/`A`/`B` (Task 2) are the fields Tasks
3 and 4 read. `domain.Resolved.Ref`/`Pair`/`Hint` (Tasks 2, 6) are what Task 5
inspects and Task 10 flattens. `linkhist.Entry{Link,Desc,Created}` (Task 7) is
what Tasks 8, 9 and 10 carry — one shape, three surfaces.
`linkShapes{Ref,Pair}` exists only in `internal/cli`. `linkDesc` (Task 8) is
the Go side of the agreement Task 9 pins in JS.

**Two things to carry in the DISPATCH, not the task text.**

1. *Task 3, the pair-plus-file lane.* The step says to park a `pendingSteer` at
   `steerStageFiles` "the way `steerNavigateCommitFile` does" — but
   `drainPendingFiles` gates on `m.filesHash == ps.hash`, and
   `openCompareFiles` sets `m.compareTag`, not `m.filesHash`. **Verify the
   compare view's load actually drains a pending before relying on it.** If it
   does not, the lane needs its OWN stage (`steerStageCompare` + a
   `drainPendingCompare` keyed on `compareTag`), not a borrowed gate — a
   pending parked on a gate nothing satisfies is a command that silently never
   lands.
2. *Task 4's browser step is not optional.* Plan 1b shipped `links.js` twice
   with only a Node-level gate behind it, and the whole-branch review found the
   predicates stale. The task review must see evidence a real browser steered a
   live page.

**One ordering hazard.** Task 5 must land before Task 10: `gg_compare_links`
takes any link shape, and if `resolveLinkArgShapes` does not exist yet an
implementer will invent a second refusal table in `internal/mcp`. The
dependency is noted in Task 10's dispatch.

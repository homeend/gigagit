# Web discard op + shared CRLF hunk fix — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire the existing `engine.Discard` into the web op switch with red-confirm RMB entries, and fix hunkpick's CRLF→LF rewrite so pure-CRLF files round-trip byte-faithfully (TUI fixed for free; web's blanket CRLF 422 narrows to mixed-EOL only).

**Architecture:** Part A is pure wiring (new `case "discard"` resolving the path against a fresh status read — the allowlist pattern — plus client RMB items + local confirm). Part B adds a `Doc.EOL` terminator chosen by `FromDiff` when both inputs are consistently CRLF; `Resolved()` joins with it.

**Tech Stack:** Go (`internal/web`, `internal/hunkpick`), vanilla JS.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-25-web-discard-crlf-design.md` (committed in this worktree) — binding.
- Work ONLY in `/mnt/t/others/gigagit.worktrees/feat-web-discard` on branch `feat/web-discard`.
- The ONLY `app.js` edit is one appended item block at the END of the `#files-list` contextmenu items list (other wave-3 tracks own everything else client-side). No keydown/footer/diff-pane edits.
- `engine.Discard` and `hunkpick.ParseConflict` behavior must NOT change (`ParseConflict` docs keep `EOL == ""`).
- Exact new server strings: 404 `unknown path`, 422 `conflicted — resolve instead`, 422 `file mixes CRLF and LF line endings — stage the whole file instead`.
- Gates: `go test ./internal/hunkpick ./internal/web ./internal/engine`, `gofmt -l` clean on touched files, `node --check internal/web/static/app.js`.
- Commit messages end with:
  `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>` and
  `Claude-Session: https://claude.ai/code/session_01HJ4EsSe6QUvrEADAwdC9HG`

---

### Task 1: CRLF fix in hunkpick (TDD)

**Files:**
- Modify: `internal/hunkpick/hunkpick.go` (Doc field + `Resolved`)
- Modify: `internal/hunkpick/fromdiff.go` (EOL detection)
- Test: `internal/hunkpick/fromdiff_test.go`

**Interfaces:**
- Produces: `Doc.EOL string` (zero value `""` ≡ `"\n"` — every existing constructor/test unaffected). Task 2 relies on `FromDiff` CRLF inputs → `Resolved` CRLF output.

- [ ] **Step 1: failing tests** — append to `internal/hunkpick/fromdiff_test.go` (add `"bytes"` to imports if absent):

```go
func TestFromDiffCRLFRoundTrip(t *testing.T) {
	left := []byte("a\r\nb\r\nc\r\n")
	right := []byte("a\r\nB\r\nc\r\nd\r\n")
	d := FromDiff(left, right)
	d.SetAll(TakeIncoming)
	got, ok := d.Resolved()
	if !ok || !bytes.Equal(got, right) {
		t.Fatalf("TakeIncoming = %q ok=%v, want %q", got, ok, right)
	}
	d.SetAll(TakeCurrent)
	got, ok = d.Resolved()
	if !ok || !bytes.Equal(got, left) {
		t.Fatalf("TakeCurrent = %q ok=%v, want %q", got, ok, left)
	}
}

func TestFromDiffMixedEOLStaysLF(t *testing.T) {
	// mixed endings: dominant-EOL detection must NOT fire; today's
	// normalize-to-LF behavior is kept and documented
	left := []byte("a\nb\r\nc\n")
	right := []byte("a\nB\r\nc\n")
	d := FromDiff(left, right)
	d.SetAll(TakeIncoming)
	got, ok := d.Resolved()
	want := []byte("a\nB\nc\n")
	if !ok || !bytes.Equal(got, want) {
		t.Fatalf("mixed = %q ok=%v, want %q", got, ok, want)
	}
}
```

- [ ] **Step 2: run** `go test ./internal/hunkpick -run TestFromDiff -v` — expect `TestFromDiffCRLFRoundTrip` FAIL (LF output), mixed test may already pass.

- [ ] **Step 3: implement**

In `internal/hunkpick/hunkpick.go`, add to the `Doc` struct:

```go
	// EOL is the terminator Resolved joins lines with; "" means "\n".
	// FromDiff sets "\r\n" when both inputs are consistently CRLF —
	// textdiff strips the \r from every line for alignment identity, so
	// the terminator must be re-applied at join time or a CRLF file
	// comes back entirely LF (the silent-rewrite bug the TUI H picker
	// shipped with). ParseConflict leaves it "" on purpose: its lines
	// keep their own \r (it splits on \n without trimming).
	EOL string
```

In `Resolved()`, replace the two hardcoded join/append lines:

```go
	buf := bytes.Join(toBytes(lines), []byte("\n"))
	if d.FinalNewline && len(lines) > 0 {
		buf = append(buf, '\n')
	}
```

with:

```go
	sep := d.EOL
	if sep == "" {
		sep = "\n"
	}
	buf := bytes.Join(toBytes(lines), []byte(sep))
	if d.FinalNewline && len(lines) > 0 {
		buf = append(buf, sep...)
	}
```

In `internal/hunkpick/fromdiff.go`, add the helper and set `EOL` on the doc
`FromDiff` builds, immediately before it is returned (adapt the doc's local
variable name):

```go
// eolOf reports whether b ends every line with CRLF, and whether it has any
// newlines at all (a side with none — or a nil side — expresses no opinion).
func eolOf(b []byte) (crlf, any bool) {
	nl := bytes.Count(b, []byte("\n"))
	if nl == 0 {
		return false, false
	}
	return bytes.Count(b, []byte("\r\n")) == nl, true
}
```

```go
	lc, la := eolOf(left)
	rc, ra := eolOf(right)
	// CRLF only when at least one side has newlines and every side that
	// does is consistently CRLF; anything mixed stays LF (documented).
	if (la || ra) && (!la || lc) && (!ra || rc) && (lc || rc) {
		d.EOL = "\r\n"
	}
```

(Add `"bytes"` to fromdiff.go's imports if absent.)

- [ ] **Step 4: run** `go test ./internal/hunkpick -v` — ALL tests pass (the existing LF fixtures must be untouched by the zero-value default).

- [ ] **Step 5: commit** `git add internal/hunkpick && git commit -m "fix(hunkpick): preserve CRLF through FromDiff→Resolved (dominant-EOL rejoin)"` (with trailers).

---

### Task 2: web guard narrows to mixed-EOL; CRLF round-trips over HTTP (TDD)

**Files:**
- Modify: `internal/web/hunks.go`
- Test: `internal/web/hunks_test.go`

**Interfaces:**
- Consumes: Task 1's `Doc.EOL` behavior.
- Produces: `GET/POST /api/hunks*` now 200s on pure-CRLF files; 422 only for mixed EOL (message above).

- [ ] **Step 1: flip the tests.** In `internal/web/hunks_test.go`, find the existing CRLF 422 test (grep `CRLF`). Replace it with a round-trip test and add a mixed-EOL test, reusing the file's existing server/fixture helpers (mirror the neighboring hunk tests' setup style — write the file, `git add` via the harness, edit the worktree copy):

```go
func TestHunksCRLFRoundTrip(t *testing.T) {
	// index: a,b,c (CRLF)  worktree: a,B,c,d (CRLF) → blocks stage-able,
	// and the staged blob keeps its \r\n bytes (the hunkpick EOL fix).
	// 1. seed a committed CRLF file, then edit it (still CRLF)
	// 2. GET /api/hunks → 200, count >= 1, capture hash
	// 3. POST /api/stage-hunks picks=[0] with the hash → 200
	// 4. read the staged blob (svc.ShowFile(ctx, "", path) or the
	//    harness's git helper `git show :<path>`) → contains "\r\n"
	//    and contains the picked hunk's new content
}
```

```go
func TestHunksMixedEOL422(t *testing.T) {
	// worktree file "a\nb\r\nc\n" (mixed) → GET /api/hunks returns 422 with
	// "file mixes CRLF and LF line endings — stage the whole file instead"
}
```

Write these as real tests (full bodies) against the existing harness; the comments above are the required assertions, not placeholders to leave in.

- [ ] **Step 2: run** `go test ./internal/web -run TestHunks -v` — CRLF round-trip FAILS (422 today), mixed passes or fails depending on message.

- [ ] **Step 3: implement.** In `internal/web/hunks.go` replace the CRLF guard block

```go
	if bytes.Contains(work, []byte("\r\n")) || bytes.Contains(index, []byte("\r\n")) {
		writeErr(w, http.StatusUnprocessableEntity, errors.New("file uses CRLF line endings; hunk staging would rewrite them — stage the whole file instead"))
		return nil, "", false
	}
```

with

```go
	if mixedEOL(work) || mixedEOL(index) {
		writeErr(w, http.StatusUnprocessableEntity, errors.New("file mixes CRLF and LF line endings — stage the whole file instead"))
		return nil, "", false
	}
```

add the helper:

```go
// mixedEOL reports whether b mixes CRLF and bare-LF line endings — the one
// case hunkpick's dominant-EOL rejoin would still silently normalize.
// Consistent CRLF round-trips byte-faithfully since the hunkpick EOL fix.
func mixedEOL(b []byte) bool {
	crlf := bytes.Count(b, []byte("\r\n"))
	return crlf > 0 && bytes.Count(b, []byte("\n")) > crlf
}
```

and update `loadHunkDoc`'s doc comment: the "CRLF is refused deliberately…" sentence becomes "Mixed EOL is refused (dominant-EOL rejoin would silently normalize the minority); consistent CRLF round-trips since hunkpick's EOL fix."

- [ ] **Step 4: run** `go test ./internal/web -v` — all pass.

- [ ] **Step 5: commit** `git add internal/web && git commit -m "feat(web): hunk staging accepts pure-CRLF files; 422 only for mixed EOL"` (with trailers).

---

### Task 3: discard op over the web (TDD)

**Files:**
- Modify: `internal/web/ophttp.go` (new switch case + doc-comment op list)
- Modify: `internal/web/static/app.js` (one appended contextmenu item block)
- Test: `internal/web/opdiscard_test.go` (new; mirror an existing op test file's harness, e.g. the delete-branch one)

**Interfaces:**
- Consumes: `engine.Discard{Restore, Remove []string}` (exists; decision-free), `svc.Status`, `model.Kind*` constants, the op transport (`startOp`/SSE).
- Produces: wire op `{op: "discard", path}`.

- [ ] **Step 1: failing test** — `internal/web/opdiscard_test.go`, using the same fixture + op-transport helpers as the existing op tests (start op via POST `/api/op`, follow `/api/op/{id}/events` to `done`):

```go
func TestOpDiscard(t *testing.T) {
	// fixture: commit f.txt "clean\n"; then worktree edits:
	//   f.txt → "dirty\n" (tracked, unstaged)   new.txt → "x\n" (untracked)
	// 1. {op:"discard", path:"f.txt"}  → done ok, changed:true;
	//    on disk f.txt == "clean\n"; new.txt still exists
	// 2. {op:"discard", path:"new.txt"} → done ok; new.txt gone from disk
	// 3. {op:"discard", path:"nope.txt"} → HTTP 404 "unknown path"
	// 4. {op:"discard", path:""} and {op:"discard", path:"-x"} → 400 "invalid path"
}

func TestOpDiscardConflicted(t *testing.T) {
	// fixture: create a real merge conflict in f.txt (two branches editing
	// the same line, git merge → conflict) via the harness git helper
	// {op:"discard", path:"f.txt"} → HTTP 422 "conflicted — resolve instead"
}
```

Write full bodies against the existing harness; the comments are the required assertions.

- [ ] **Step 2: run** `go test ./internal/web -run TestOpDiscard -v` — FAIL with 400 `unknown op "discard"`.

- [ ] **Step 3: implement.** In `internal/web/ophttp.go`, add before `default:` (and add `"github.com/homeend/gigagit/internal/model"` to imports if absent):

```go
	case "discard":
		// Per-file discard. The path resolves against a fresh status read
		// (the remove-worktree/stash allowlist pattern): a stale client row
		// 404s instead of discarding the wrong thing. Decision-free — the
		// client confirms before POSTing (the delete-tag convention).
		if req.Path == "" || !isGitArgSafe(req.Path) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid path"))
			return
		}
		st, err := svc.Status(r.Context())
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		var discard engine.Operation
		for _, f := range st.Files {
			if f.Path != req.Path {
				continue
			}
			switch f.Kind {
			case model.KindUnmerged:
				writeErr(w, http.StatusUnprocessableEntity, errors.New("conflicted — resolve instead"))
				return
			case model.KindUntracked:
				discard = engine.Discard{Remove: []string{req.Path}}
			default:
				discard = engine.Discard{Restore: []string{req.Path}}
			}
			break
		}
		if discard == nil {
			writeErr(w, http.StatusNotFound, errors.New("unknown path"))
			return
		}
		op = discard
```

Extend the handler's doc-comment op list with `discard`. (`svc` is already in scope — the push/remove-worktree cases use it.)

- [ ] **Step 4: run** `go test ./internal/web -v` — all pass.

- [ ] **Step 5: client entries.** In `internal/web/static/app.js`, in the `#files-list` contextmenu handler, AFTER the `unstage all` items block and immediately BEFORE the `showCtxMenu(items, e.clientX, e.clientY);` call, insert:

```js
  if (f.section === "changes") {
    items.push({
      label: "discard changes", danger: true,
      act: () => showLocalConfirm(
        "Discard changes to " + f.path + "? This cannot be undone.",
        ["discard", "abort"],
        (o) => { if (o === "discard") startOp({ op: "discard", path: f.path }, "discard " + f.path); }
      ),
    });
  } else if (f.section === "untracked") {
    items.push({
      label: "delete untracked file", danger: true,
      act: () => showLocalConfirm(
        "Delete untracked " + f.path + "? This cannot be undone.",
        ["discard", "abort"],
        (o) => { if (o === "discard") startOp({ op: "discard", path: f.path }, "discard " + f.path); }
      ),
    });
  }
```

(`"discard"` is already in `DANGER_OPTIONS` → red confirm button for free; `danger: true` renders the menu row red. Staged-only and conflicted rows get no entry — restore would no-op / the server 422s.)

- [ ] **Step 6: verify** `node --check internal/web/static/app.js` and `go build ./cmd/gg`.

- [ ] **Step 7: CHANGELOG + commit.** Add under `## [Unreleased]`:

```markdown
- web: right-click a changed file → **discard changes** (untracked →
  **delete untracked file**), behind a red confirm. The op resolves the
  path server-side against a fresh status read; conflicted files are
  refused (resolve instead).

- Hunk staging no longer rewrites CRLF files to LF: `hunkpick` now re-applies
  the file's own line terminator on resolve, so the TUI `H` picker and the
  web hunk view round-trip pure-CRLF files byte-faithfully. The web guard
  narrows to refusing only mixed-EOL files.
```

```bash
git add -A && git commit -m "feat(web): per-file discard op with red confirm (RMB)"
```
(with trailers)

## Self-review notes

- Task ordering is deliberate: the hunkpick fix lands first so Task 2's
  round-trip test exercises it; Task 3 is independent of both.
- `engine.Discard` runs both restore and clean even on partial failure —
  no web-side compensation needed; op errors surface via `done.error`.
- Full `./test.sh race` is run at the wave gate by the controller, not here.

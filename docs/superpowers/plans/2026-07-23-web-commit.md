# Web Track A (commit from status pane) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Commit staged changes from the web's working-tree screen — the op transport's second operation.

**Architecture:** One new switch case in `handleOpStart` (`op:"commit"` → `engine.Commit{Message}`), and a commit box on the status pane whose client rides the existing transport via a generic `startOp(body, label)` refactor of `startSwitch`. A form-field keyboard guard stops the global j/k/s/u handler from firing while typing.

**Tech Stack:** Go stdlib + `internal/engine`; hand-written JS.

**Spec:** `docs/superpowers/specs/2026-07-23-web-commit-design.md`. Read it first.

## Global Constraints

- Branch `feat/web-commit` (worktree `/mnt/t/others/gigagit.worktrees/feat-web-commit`), off `web-dev`. Merge target = web-dev (controller does it). NEVER main; never push.
- **Parallel-track discipline:** track B is concurrently editing the sidebar region, `fetchBranches`, `loadRepo`, `server.go` routes, and new endpoint files. Touch ONLY: `internal/web/ophttp.go`, a NEW test file, `static/index.html` (files-pane region + nothing else), `static/style.css` (append only), `static/app.js` (keydown top, the `startSwitch` region, `renderFiles`, `handleOpEvent` — NOT `fetchBranches`/`loadRepo`/`boot`/sidebar code), `CHANGELOG.md`, `CLAUDE.md`.
- `op:"commit"` requires non-empty `strings.TrimSpace(Message)` → else 400. No `All`, no `Amend`.
- `internal/web` never imports `internal/git`; stdlib only; mutating route already behind writeGuard (no new routes).
- Tests: real git via existing helpers (`newRepoDir`, `gitRun`, `serve`, `postJSON`, `readSSE`, `findEvent`). JS untested-by-design.
- Commands run from the worktree root.

## File Structure

| File | Responsibility |
|---|---|
| `internal/web/ophttp.go` (modify) | `Message` field + `case "commit"` |
| `internal/web/opcommit_test.go` (new) | commit round-trip + validation tests |
| `static/index.html` (modify) | `#commit-box` in the files pane |
| `static/app.js` (modify) | form-field guard, `startOp` refactor, `doCommit`, box visibility |
| `static/style.css` (modify) | commit-box styles (append) |
| `CHANGELOG.md`, `CLAUDE.md` (modify) | docs |

---

### Task 1: `op:"commit"` on the transport

**Files:**
- Modify: `internal/web/ophttp.go`
- Create: `internal/web/opcommit_test.go`

**Interfaces:**
- Consumes: `engine.Commit{Message string; All, Amend bool}` (`internal/engine/ops_basic.go:13` — only `Message` is set); existing `opStartRequest`/`handleOpStart` switch; test helpers `newRepoDir`, `gitRun`, `serve`, `postJSON`, `readSSE` (from `ophttp_test.go`).
- Produces: `POST /api/op {"op":"commit","message":...}` → 202/400, wired for Task 2's JS.

- [ ] **Step 1: Write the failing tests**

Create `internal/web/opcommit_test.go`:

```go
package web

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

func TestOpHTTPCommitRoundTrip(t *testing.T) {
	dir := newRepoDir(t, 1)
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "new.txt")
	ts := serve(t, New(domain.Open(dir)))

	var out struct {
		OpID string `json:"op_id"`
	}
	code := postJSON(t, ts, "/api/op", `{"op":"commit","message":"web commit\n\nbody line"}`, "application/json", "", &out)
	if code != http.StatusAccepted {
		t.Fatalf("commit start code = %d", code)
	}
	events := readSSE(t, ts, out.OpID, 20*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	sum, _ := done["summary"].(string)
	if !strings.Contains(sum, "committed") || !strings.Contains(sum, "web commit") {
		t.Errorf("summary = %q", sum)
	}
	if subj := gitRun(t, dir, "log", "-1", "--format=%s"); subj != "web commit" {
		t.Errorf("committed subject = %q", subj)
	}
	if body := gitRun(t, dir, "log", "-1", "--format=%b"); !strings.Contains(body, "body line") {
		t.Errorf("committed body = %q", body)
	}
	if out := gitRun(t, dir, "status", "--porcelain"); strings.Contains(out, "new.txt") {
		t.Errorf("new.txt still pending after commit:\n%s", out)
	}
}

func TestOpHTTPCommitValidation(t *testing.T) {
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))
	for _, body := range []string{
		`{"op":"commit","message":""}`,
		`{"op":"commit","message":"   \n\t "}`,
		`{"op":"commit"}`,
	} {
		if code := postJSON(t, ts, "/api/op", body, "application/json", "", nil); code != http.StatusBadRequest {
			t.Errorf("%s: code = %d, want 400", body, code)
		}
	}
}

func TestOpHTTPCommitNothingStaged(t *testing.T) {
	dir := newRepoDir(t, 1) // clean tree
	ts := serve(t, New(domain.Open(dir)))
	var out struct {
		OpID string `json:"op_id"`
	}
	if code := postJSON(t, ts, "/api/op", `{"op":"commit","message":"nope"}`, "application/json", "", &out); code != http.StatusAccepted {
		t.Fatalf("start code = %d", code)
	}
	events := readSSE(t, ts, out.OpID, 20*time.Second)
	done := events[len(events)-1]
	if done["ok"] != false {
		t.Fatalf("done = %v, want ok=false (git refuses an empty commit)", done)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/web/ -run 'TestOpHTTPCommit' 2>&1 | tail -5`
Expected: FAIL — `"unknown op \"commit\""` → the round-trip gets 400 where 202 expected.

- [ ] **Step 3: Implement**

In `internal/web/ophttp.go`:

Add `"strings"` to the import block.

Extend `opStartRequest`:

```go
type opStartRequest struct {
	Op      string `json:"op"`
	Branch  string `json:"branch"`
	Message string `json:"message"`
}
```

Add a case to `handleOpStart`'s switch, after `case "switch":`'s block:

```go
	case "commit":
		if strings.TrimSpace(req.Message) == "" {
			writeErr(w, http.StatusBadRequest, errors.New("message required"))
			return
		}
		op = engine.Commit{Message: req.Message}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/web/ -run 'TestOpHTTPCommit' -v 2>&1 | tail -8`
Expected: PASS (3 tests).

- [ ] **Step 5: Package + archtest + gofmt**

Run: `go test ./internal/web/ ./internal/archtest/ 2>&1 | tail -3 && gofmt -l internal/web/`
Expected: ok, ok, no output.

- [ ] **Step 6: Commit**

```bash
git add internal/web/ophttp.go internal/web/opcommit_test.go
git commit -m "feat(web): op:commit — engine.Commit on the transport"
```

---

### Task 2: commit box UI + form-field keyboard guard; docs

**Files:**
- Modify: `static/index.html`, `static/app.js`, `static/style.css`, `CHANGELOG.md`, `CLAUDE.md`

JS/HTML/CSS + docs only. Go change needed → BLOCKED.

- [ ] **Step 1: index.html — the commit box**

In the `files-pane` section, directly AFTER the `#files-actions` div:

```html
    <div id="commit-box" class="hidden">
      <textarea id="commit-msg" rows="3" placeholder="commit message — first line is the subject"></textarea>
      <button id="commit-btn">commit</button>
    </div>
```

- [ ] **Step 2: style.css — append**

```css
#commit-box { padding: 4px 8px; border-bottom: 1px solid var(--border); display: flex; gap: 8px; align-items: flex-start; }
#commit-box.hidden { display: none; }
#commit-box textarea {
  flex: 1; background: var(--bg-alt); color: var(--fg); border: 1px solid var(--border);
  border-radius: 3px; font: inherit; padding: 3px 6px; resize: vertical;
}
#commit-box textarea:focus { outline: none; border-color: var(--accent); }
#commit-box button {
  background: var(--bg-alt); color: var(--fg); border: 1px solid var(--border);
  border-radius: 3px; padding: 2px 12px; font: inherit; cursor: pointer;
}
#commit-box button:hover:not(:disabled) { border-color: var(--accent); }
#commit-box button:disabled { opacity: .45; cursor: default; }
```

- [ ] **Step 3: app.js — generic startOp + doCommit**

Replace the whole `startSwitch` function (app.js:96-110 on this branch's base) with:

```js
// startOp is the transport client, op-agnostic: POST /api/op, then follow
// the SSE stream. state.op.kind lets done-handling react per op (a commit
// clears the message box; a switch must not eat a draft).
async function startOp(body, label) {
  if (state.op) return; // one live op; the server would 409 anyway
  let resp;
  try {
    resp = await postJSON("/api/op", body);
  } catch (e) {
    opLine("error: " + (e.message || e), true);
    return;
  }
  opLine("⟳ " + label + "…");
  const es = new EventSource("/api/op/" + resp.op_id + "/events");
  state.op = { id: resp.op_id, es, kind: body.op };
  es.onmessage = (m) => handleOpEvent(JSON.parse(m.data));
  es.onerror = () => {}; // transient; done handling closes the source
}

function startSwitch(branch) {
  startOp({ op: "switch", branch }, "switching " + branch);
}

function doCommit() {
  const message = $("commit-msg").value;
  if (!message.trim()) return;
  startOp({ op: "commit", message }, "committing");
}
```

In `handleOpEvent`'s `done` branch, capture the kind FIRST and clear the box on a successful commit. The branch currently starts with:

```js
  } else if (ev.type === "done") {
```

Make its body begin:

```js
  } else if (ev.type === "done") {
    const kind = state.op && state.op.kind;
```

and directly after the existing `hideModal();` line add:

```js
    if (ev.ok && kind === "commit") $("commit-msg").value = "";
```

- [ ] **Step 4: app.js — box visibility + button state + wiring**

In `renderFiles`: the non-status branch already does `$("files-actions").classList.add("hidden");` — add beside it:

```js
    $("commit-box").classList.add("hidden");
```

The status branch already does `$("files-actions").classList.remove("hidden");` — add beside it:

```js
  $("commit-box").classList.remove("hidden");
  $("commit-btn").disabled = !(state.wt && state.wt.counts.staged > 0) || !!state.op;
```

Also in the tree-went-clean branch of `stage()` (it already hides `files-actions`): add

```js
    $("commit-box").classList.add("hidden");
```

Wire the button, next to the other listeners at the bottom:

```js
$("commit-btn").addEventListener("click", doCommit);
```

- [ ] **Step 5: app.js — the form-field keyboard guard**

At the VERY TOP of the `keydown` listener body — BEFORE the modal block (`if (!$("modal").classList.contains("hidden")) {`) — insert:

```js
  // Form fields own the keyboard: without this, typing a commit message
  // triggers j/k navigation and s/u staging. Ctrl/Cmd+Enter commits.
  if (e.target.closest && e.target.closest("input,textarea")) {
    if (e.key === "Enter" && (e.ctrlKey || e.metaKey) && e.target.id === "commit-msg") {
      e.preventDefault();
      doCommit();
    }
    return;
  }
```

- [ ] **Step 6: Build + smoke**

Run: `node --check internal/web/static/app.js && go build -o ./gg ./cmd/gg && go test ./internal/web/ 2>&1 | tail -2 && gofmt -l internal/web/`
Expected: all clean.

Smoke (scratch repo only — commits mutate):

```bash
rm -rf /tmp/ggweb-ci && git init -q -b main /tmp/ggweb-ci && cd /tmp/ggweb-ci
git -c user.email=t@t -c user.name=t commit -q --allow-empty -m init
echo x > a.txt && git add a.txt
(cd /tmp/ggweb-ci && /mnt/t/others/gigagit.worktrees/feat-web-commit/gg web --addr 127.0.0.1:8126 &) && sleep 1
OP=$(curl -s -X POST -H 'Content-Type: application/json' -d '{"op":"commit","message":"from web"}' http://127.0.0.1:8126/api/op | python3 -c 'import json,sys;print(json.load(sys.stdin)["op_id"])')
curl -s -N --max-time 5 http://127.0.0.1:8126/api/op/$OP/events | tail -2
git -C /tmp/ggweb-ci log -1 --format=%s    # expect: from web
curl -s -o /dev/null -w '%{http_code}\n' -X POST -H 'Content-Type: application/json' -d '{"op":"commit","message":"  "}' http://127.0.0.1:8126/api/op   # expect 400
pkill -f 'gg web --addr 127.0.0.1:8126'; true
```

Playwright visual pass: fresh scratch repo with one untracked file; adapt the pattern from `/tmp/claude-1000/-mnt-t-others-gigagit/125bc0e5-080f-41d1-8b68-33968f63bf24/scratchpad/pw/shoot4.js` (run with `node` from that pw directory; GG = this worktree's binary): open the working-tree row, click the file's `s` button (stages it), type a message into `#commit-msg`, click `#commit-btn`, wait ~1.5s, screenshot as `shot-9-committed.png` — the list screen should be back (clean tree) with the op line reading `committed …`. Read the PNG to verify before reporting.

- [ ] **Step 7: docs**

`CHANGELOG.md`, top of `## [Unreleased]`:

```markdown
- `gg web`: commit from the working-tree screen — a message box + commit
  button (Ctrl+Enter) on the status pane, wired as the op transport's second
  operation (`op:"commit"` → `engine.Commit`; empty message → 400). Typing
  in form fields no longer triggers the j/k/s/u keyboard shortcuts.
```

`CLAUDE.md` web row: after the increment-2 sentence, append:

```
`op:"commit"` (engine.Commit, message required) is the transport's second
op — the status pane's commit box submits it (Ctrl+Enter; a form-field
keydown guard keeps j/k/s/u from firing while typing).
```

- [ ] **Step 8: Commit**

```bash
git add internal/web/static/ CHANGELOG.md CLAUDE.md
git commit -m "feat(web): commit box on the status pane (Ctrl+Enter), form-field key guard"
```

---

### Final verification

- [ ] `go build ./... && go test ./internal/web/ ./internal/archtest/` green; `./test.sh unit` if time allows (controller runs the full suite before merge).
- [ ] Merge target = web-dev, controller-owned. Never main.

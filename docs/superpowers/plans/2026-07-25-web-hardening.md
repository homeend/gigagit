# Web Transport Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the web op transport's reliability gaps: a `resolved` wire event (replay-idempotent modals, live second-tab close), client SSE-drop recovery, red destructive modal options, persisted sidebar state, stash ref+sha guard, gen-guarded detail opens, diff-nav disabled on notices.

**Architecture:** Server half in `internal/web/oprun.go` (publish the `resolved` marker inside `decide`'s critical section) and `ophttp.go` (sha freshness guard on the stash allowlist). Client half entirely in the embedded SPA (`internal/web/static/app.js` + `style.css`). No new endpoints, no engine/domain changes.

**Tech Stack:** Go 1.26 stdlib (net/http, httptest, real-git test fixtures), vanilla ES JavaScript (no framework), CSS.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-25-web-hardening-design.md` (committed in this worktree).
- Decision options are English protocol values — never translated, never renamed. The client `DANGER_OPTIONS` set is exactly: `force`, `force-with-lease`, `force-delete`, `reset`, `delete`, `drop`, `unlock-and-remove`, `discard`, `overwrite`, `hard`.
- Client-sent identifiers resolve server-side by allowlist; the sha guard is a freshness check ON TOP of the existing ref allowlist, never a replacement. A StashCommit resolve **error** must NOT block the op (best-effort guard); only a successful resolve that mismatches refuses.
- The `resolved` event must be appended to history atomically with consuming the pending decision (same `r.mu` critical section) — `publish` locks `r.mu` itself, so `decide` must use a locked helper, not call `publish`.
- localStorage access always wrapped in try/catch (private-mode safety); failure degrades to non-persistent behavior.
- Working dir: `/mnt/t/others/gigagit.worktrees/feat-web-hardening`, branch `feat/web-hardening`. Verify with `git branch --show-current` before any edit.
- Existing test helpers to reuse (do not re-implement): `newRepoDir`, `gitRun` (returns trimmed output), `serve`, `getJSON`, `postJSON(t, ts, path, body, contentType, origin, out)`, `startOpBody`, `readSSE`, `waitDecision`, `dirtyFile`.

---

### Task 1: Server — `resolved` event + stash sha guard

**Files:**
- Modify: `internal/web/oprun.go` (publish/decide, ~lines 133–208)
- Modify: `internal/web/ophttp.go` (opStartRequest, stash allowlist loop)
- Test: `internal/web/opharden_test.go` (new file)

**Interfaces:**
- Consumes: existing `opRun`, `wireEvent`, web test helpers (Global Constraints list).
- Produces: wire event `{"type":"resolved"}` (no other fields) published when a decision is answered; `opStartRequest.Sha string` with JSON tag `sha`, honored by stash-apply/stash-pop/stash-drop. Task 2's client relies on both.

- [ ] **Step 1: Write the failing tests**

Create `internal/web/opharden_test.go`:

```go
package web

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// A decided fork must leave a "resolved" marker in the replay history:
// a reconnecting client re-shows then re-hides the modal (idempotent
// replay), and a second tab's modal closes live off the same event.
func TestDecidePublishesResolved(t *testing.T) {
	dir := newRepoDir(t, 2)
	gitRun(t, dir, "branch", "feature")
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	opID := startOpBody(t, ts, `{"op":"delete-branch","branch":"feature"}`)
	run := srv.opByID(opID)
	if run == nil {
		t.Fatal("run not found")
	}
	waitDecision(t, run)
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"delete"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide code = %d", code)
	}
	events := readSSE(t, ts, opID, 30*time.Second) // fresh subscribe: full replay
	di, ri := -1, -1
	for i, we := range events {
		if we["type"] == "decision" && di == -1 {
			di = i
		}
		if we["type"] == "resolved" && ri == -1 {
			ri = i
		}
	}
	if di == -1 || ri == -1 || ri < di {
		t.Fatalf("replay must contain decision then resolved, got %v", events)
	}
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
}

// The stash ops' optional sha is a freshness guard on top of the ref
// allowlist: a mismatch means the stash list changed under the client.
func TestOpHTTPStashShaGuard(t *testing.T) {
	dir := newRepoDir(t, 1)
	dirtyFile(t, dir, "wip\n")
	gitRun(t, dir, "stash", "push", "-m", "keepme")
	ts := serve(t, New(domain.Open(dir)))

	code := postJSON(t, ts, "/api/op", `{"op":"stash-drop","ref":"stash@{0}","sha":"`+strings.Repeat("0", 40)+`"}`, "application/json", "", nil)
	if code != http.StatusConflict {
		t.Fatalf("mismatched sha code = %d, want 409", code)
	}
	if out := gitRun(t, dir, "stash", "list"); !strings.Contains(out, "keepme") {
		t.Fatalf("stash gone after refused drop: %q", out)
	}

	// matching sha dispatches; the empty-sha path is covered by the
	// existing apply/pop/drop tests
	sha := gitRun(t, dir, "rev-parse", "stash@{0}")
	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"stash-apply","ref":"stash@{0}","sha":"`+sha+`"}`), 30*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-hardening && go test ./internal/web/ -run 'TestDecidePublishesResolved|TestOpHTTPStashShaGuard' -count=1`
Expected: FAIL — `TestDecidePublishesResolved` fails with "replay must contain decision then resolved" (no resolved event exists); `TestOpHTTPStashShaGuard` fails with "mismatched sha code = 202, want 409" (the `sha` field is ignored today).

- [ ] **Step 3: Implement the server changes**

In `internal/web/oprun.go`, replace the existing `publish` func with:

```go
// publish appends to the replay buffer and fans out to live subscribers.
// A subscriber whose buffer is full drops the event (probe-tier; the
// replay-on-attach path is the correctness backstop).
func (r *opRun) publish(we wireEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.publishLocked(we)
}

// publishLocked is publish for callers already holding r.mu — decide must
// append its resolved marker atomically with consuming the pending fork.
func (r *opRun) publishLocked(we wireEvent) {
	r.history = append(r.history, we)
	for ch := range r.subs {
		select {
		case ch <- we:
		default:
		}
	}
}
```

In the same file's `decide` method, extend the successful-answer case (the `case r.answer <- option:` branch) from:

```go
	select {
	case r.answer <- option:
		r.pending = nil // consumed: a second decide must 409, and no stale answer can outlive its fork
		return nil
	default:
		return errNotWaiting // answer already queued
	}
```

to:

```go
	select {
	case r.answer <- option:
		r.pending = nil // consumed: a second decide must 409, and no stale answer can outlive its fork
		// The resolved marker makes replay idempotent (a reconnecting
		// client re-hides the modal it just re-showed) and closes a second
		// tab's modal live.
		r.publishLocked(wireEvent{"type": "resolved"})
		return nil
	default:
		return errNotWaiting // answer already queued
	}
```

In `internal/web/ophttp.go`, add the `Sha` field to `opStartRequest`:

```go
type opStartRequest struct {
	Op      string `json:"op"`
	Branch  string `json:"branch"`
	Message string `json:"message"`
	Tag     string `json:"tag"`
	Path    string `json:"path"`
	Ref     string `json:"ref"`
	Sha     string `json:"sha"`
}
```

and in the `case "stash-apply", "stash-pop", "stash-drop":` allowlist loop, insert the freshness guard right after the ref match, before the inner op switch:

```go
		for _, e := range entries {
			if e.Ref == req.Ref {
				// Optional freshness guard: the client sends the sha it
				// listed; a successful resolve that mismatches means the
				// stash list changed under it (stash@{N} is positional).
				// A resolve error does not block — best-effort only.
				if req.Sha != "" {
					if cs, cerr := s.svc.StashCommit(r.Context(), e.Ref); cerr == nil && cs != req.Sha {
						writeErr(w, http.StatusConflict, errors.New("stash list changed; refresh"))
						return
					}
				}
				switch req.Op {
```

(the rest of the loop body is unchanged).

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-hardening && go test ./internal/web/ -run 'TestDecidePublishesResolved|TestOpHTTPStashShaGuard' -count=1`
Expected: PASS (both).

- [ ] **Step 5: Run the full web package + vet + gofmt**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-hardening && go test ./internal/web/ -count=1 && go vet ./internal/web/ && gofmt -l internal/web/`
Expected: all web tests PASS, vet clean, gofmt prints nothing.

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-web-hardening
git add internal/web/oprun.go internal/web/ophttp.go internal/web/opharden_test.go
git commit -m "feat(web): resolved wire event + stash sha freshness guard"
```

---

### Task 2: Client — recovery, danger styling, persistence, minors + docs

**Files:**
- Modify: `internal/web/static/app.js`
- Modify: `internal/web/static/style.css`
- Modify: `CHANGELOG.md`
- Modify: `CLAUDE.md` (the `web` package-map row)

**Interfaces:**
- Consumes: the `{"type":"resolved"}` wire event and the `sha` field on stash op bodies (Task 1).
- Produces: nothing later tasks rely on (final task).

All app.js edits below are exact old→new replacements; the surrounding code must not otherwise change. **CSS caution (a prior task deleted a base rule during an insertion): every style.css edit is an APPEND of new lines next to the quoted anchor — re-read the diff after editing and confirm no existing rule was removed.**

- [ ] **Step 1: state + shared helpers**

In `app.js`, in the `const state = {` literal, after the line `diffBlockIdx: -1,` add:

```js
  detailGen: 0,
```

After the line `const $ = (id) => document.getElementById(id);` add:

```js
// Destructive decision options render red in the modal (the ctx-menu
// danger precedent). Options are English protocol values — i18n never
// translates them — so a client-side set is reliable.
const DANGER_OPTIONS = new Set([
  "force", "force-with-lease", "force-delete", "reset", "delete", "drop",
  "unlock-and-remove", "discard", "overwrite", "hard",
]);

const SECTIONS = ["branches", "worktrees", "tags", "stashes"];

// localStorage can throw (private mode); persistence is best-effort.
function lsGet(k) { try { return localStorage.getItem(k); } catch { return null; } }
function lsSet(k, v) { try { localStorage.setItem(k, v); } catch {} }
```

- [ ] **Step 2: SSE-drop recovery in startOp**

Replace:

```js
  es.onmessage = (m) => handleOpEvent(JSON.parse(m.data));
  es.onerror = () => {}; // transient; done handling closes the source
```

with:

```js
  es.onmessage = (m) => handleOpEvent(JSON.parse(m.data));
  // EventSource auto-retries transient drops (readyState CONNECTING) and
  // the server replays full history on reconnect. A permanent failure
  // (readyState CLOSED — e.g. the server restarted and the op id is gone)
  // or 5 straight failed retries declares the op lost: unlock the UI and
  // refresh so panels show whatever the op actually did.
  let errCount = 0;
  es.onopen = () => { errCount = 0; };
  es.onerror = () => {
    if (!state.op || state.op.es !== es) return; // stale source after done
    errCount++;
    if (es.readyState === EventSource.CLOSED || errCount >= 5) {
      es.close();
      state.op = null;
      $("pull-btn").disabled = false;
      $("push-btn").disabled = false;
      hideModal();
      opLine("error: lost connection to operation — repo state refreshed", true);
      refreshAfterOp();
    } else {
      opLine("⟳ reconnecting…");
    }
  };
```

- [ ] **Step 3: resolved handling + danger styling**

In `handleOpEvent`, between the `decision` branch and the `done` branch, insert:

```js
  } else if (ev.type === "resolved") {
    hideModal(); // this decision was answered (another tab, or a replay)
```

so the chain reads `progress` / `decision` / `resolved` / `done`.

In `showModal`, replace:

```js
  $("modal-options").innerHTML = (ev.options || [])
    .map((o) => `<button data-o="${esc(o)}">${esc(o)}</button>`)
    .join("");
```

with:

```js
  $("modal-options").innerHTML = (ev.options || [])
    .map((o) => `<button data-o="${esc(o)}"${DANGER_OPTIONS.has(o) ? ' class="danger"' : ""}>${esc(o)}</button>`)
    .join("");
```

- [ ] **Step 4: sidebar persistence**

Replace `toggleSection` and the section wiring:

```js
function toggleSection(name) {
  const collapsed = $(name + "-list").classList.toggle("collapsed");
  $(name + "-header").textContent = (collapsed ? "\u25b8 " : "") + name;
}
["branches", "worktrees", "tags", "stashes"].forEach((n) => {
  $(n + "-header").addEventListener("dblclick", () => toggleSection(n));
});
```

with:

```js
function toggleSection(name) {
  const collapsed = $(name + "-list").classList.toggle("collapsed");
  $(name + "-header").textContent = (collapsed ? "\u25b8 " : "") + name;
  lsSet("gg.sidebar.collapsed", JSON.stringify(SECTIONS.filter((n) => $(n + "-list").classList.contains("collapsed"))));
}
SECTIONS.forEach((n) => {
  $(n + "-header").addEventListener("dblclick", () => toggleSection(n));
});

// Restore persisted sidebar state (b-key visibility + per-section
// collapse). The collapsed class lives on the persistent <ul> containers,
// so a one-time boot restore survives every re-render.
(function restoreSidebar() {
  let names = [];
  try { names = JSON.parse(lsGet("gg.sidebar.collapsed") || "[]"); } catch {}
  SECTIONS.forEach((n) => {
    if (names.includes(n)) {
      $(n + "-list").classList.add("collapsed");
      $(n + "-header").textContent = "\u25b8 " + n;
    }
  });
  if (lsGet("gg.sidebar.hidden") === "1") {
    state.sidebar = false;
    $("panes").classList.add("nosb");
  }
})();
```

In the keydown handler, replace:

```js
  } else if (e.key === "b" && state.layout === "list") {
    state.sidebar = !state.sidebar;
    $("panes").classList.toggle("nosb", !state.sidebar);
    renderCommits(); // list width changed
```

with:

```js
  } else if (e.key === "b" && state.layout === "list") {
    state.sidebar = !state.sidebar;
    lsSet("gg.sidebar.hidden", state.sidebar ? "0" : "1");
    $("panes").classList.toggle("nosb", !state.sidebar);
    renderCommits(); // list width changed
```

- [ ] **Step 5: detail-open generation counter**

In `openCommit`, after `const row = state.rows[i - wtCount()];` and around the fetch, replace:

```js
  const body = await getJSON("/api/commit/" + row.hash);
  state.files = body.files || [];
```

with:

```js
  const gen = ++state.detailGen;
  const body = await getJSON("/api/commit/" + row.hash);
  if (gen !== state.detailGen) return; // superseded by a newer open or esc
  state.files = body.files || [];
```

In `openCommitByHash`, replace:

```js
  const body = await getJSON("/api/commit/" + hash);
  state.files = body.files || [];
```

with:

```js
  const gen = ++state.detailGen;
  const body = await getJSON("/api/commit/" + hash);
  if (gen !== state.detailGen) return; // superseded by a newer open or esc
  state.files = body.files || [];
```

In `openStashDetail`, replace:

```js
  const body = await getJSON("/api/commit/" + st.sha);
  let files = body.files || [];
  if (st.untracked_sha) {
    const u = await getJSON("/api/commit/" + st.untracked_sha).catch(() => ({ files: [] }));
    files = files.concat((u.files || []).map((f) => ({ ...f, sha: st.untracked_sha })));
  }
```

with:

```js
  const gen = ++state.detailGen;
  const body = await getJSON("/api/commit/" + st.sha);
  if (gen !== state.detailGen) return; // superseded by a newer open or esc
  let files = body.files || [];
  if (st.untracked_sha) {
    const u = await getJSON("/api/commit/" + st.untracked_sha).catch(() => ({ files: [] }));
    if (gen !== state.detailGen) return;
    files = files.concat((u.files || []).map((f) => ({ ...f, sha: st.untracked_sha })));
  }
```

In `drillOut`, replace:

```js
function drillOut() {
  if (state.layout !== "detail") return;
  state.pane = "commits";
```

with:

```js
function drillOut() {
  if (state.layout !== "detail") return;
  state.detailGen++; // invalidate any in-flight detail fetch
  state.pane = "commits";
```

- [ ] **Step 6: diff-nav on notice paths + stash sha in op bodies**

In `openFile`, replace:

```js
  $("diff-title").textContent = f.path;
  $("diff-body").innerHTML = `<div class="notice">loading…</div>`;
  try {
    renderDiff(await getJSON("/api/diff?" + q));
  } catch (e) {
    $("diff-body").innerHTML = `<div class="notice">error: ${esc(e.message || e)}</div>`;
  }
```

with:

```js
  $("diff-title").textContent = f.path;
  $("diff-body").innerHTML = `<div class="notice">loading…</div>`;
  updateDiffNav();
  try {
    renderDiff(await getJSON("/api/diff?" + q));
  } catch (e) {
    $("diff-body").innerHTML = `<div class="notice">error: ${esc(e.message || e)}</div>`;
    updateDiffNav();
  }
```

In `openStatusDiff`, replace:

```js
  if (f.section === "conflicts") {
    $("diff-body").innerHTML = `<div class="notice">conflicted — resolve in the TUI</div>`;
    return;
  }
```

with:

```js
  if (f.section === "conflicts") {
    $("diff-body").innerHTML = `<div class="notice">conflicted — resolve in the TUI</div>`;
    updateDiffNav();
    return;
  }
```

and later in the same function replace:

```js
  $("diff-body").innerHTML = `<div class="notice">loading…</div>`;
  try {
    renderDiff(await getJSON("/api/diff?" + q));
  } catch (e) {
    $("diff-body").innerHTML = `<div class="notice">error: ${esc(e.message || e)}</div>`;
  }
```

with:

```js
  $("diff-body").innerHTML = `<div class="notice">loading…</div>`;
  updateDiffNav();
  try {
    renderDiff(await getJSON("/api/diff?" + q));
  } catch (e) {
    $("diff-body").innerHTML = `<div class="notice">error: ${esc(e.message || e)}</div>`;
    updateDiffNav();
  }
```

In `showStashMenu`, replace the three startOp bodies:

```js
  items.push({ label: "apply", act: () => startOp({ op: "stash-apply", ref: st.ref }, "applying " + st.ref) });
  items.push({ label: "pop", act: () => startOp({ op: "stash-pop", ref: st.ref }, "popping " + st.ref) });
```

with:

```js
  items.push({ label: "apply", act: () => startOp({ op: "stash-apply", ref: st.ref, sha: st.sha || "" }, "applying " + st.ref) });
  items.push({ label: "pop", act: () => startOp({ op: "stash-pop", ref: st.ref, sha: st.sha || "" }, "popping " + st.ref) });
```

and inside the drop confirm, replace:

```js
            if (o === "drop") startOp({ op: "stash-drop", ref: st.ref }, "dropping " + st.ref);
```

with:

```js
            if (o === "drop") startOp({ op: "stash-drop", ref: st.ref, sha: st.sha || "" }, "dropping " + st.ref);
```

- [ ] **Step 7: style.css danger rule (append-only)**

Directly AFTER the existing line

```css
#modal-options button:hover { border-color: var(--accent); }
```

append (do not modify or remove any existing line):

```css
#modal-options button.danger { color: #f27a6a; }
#modal-options button.danger:hover { border-color: #f27a6a; }
```

- [ ] **Step 8: syntax + build + test gate**

Run: `cd /mnt/t/others/gigagit.worktrees/feat-web-hardening && node --check internal/web/static/app.js && go build ./... && go test ./internal/web/ -count=1`
Expected: node prints nothing (syntax OK), build succeeds (assets embed), web tests PASS.

- [ ] **Step 9: docs**

`CHANGELOG.md`: read the first 30 lines and add, following the file's existing newest-first convention and formatting, this entry:

```
- web: transport hardening — SSE-drop recovery (reconnecting hint; a lost
  op unlocks the UI and refreshes), `resolved` wire event closes answered
  decision modals on replay and in second tabs, destructive modal options
  render red, sidebar collapse/visibility persist across reloads, stash
  apply/pop/drop guard ref+sha (409 when the list changed), gen-guarded
  detail opens, diff arrows disable on notice panes.
```

`CLAUDE.md`: append to the END of the `web` package-map row (the table row starting `| \`web\` |`), before its closing ` |`, this sentence:

```
 Transport hardening: `decide` publishes a `resolved` wire event (replay-idempotent modals; a second tab's modal closes live); the client's `es.onerror` distinguishes transient (CONNECTING → reconnecting hint) from permanent (CLOSED or 5 straight failures → op declared lost: UI unlocked, modal closed, `refreshAfterOp`); destructive modal options render red via a client `DANGER_OPTIONS` set (options are English protocol values); sidebar visibility + per-section collapse persist in localStorage (`gg.sidebar.*`); stash apply/pop/drop send the listed `sha` and the server 409s on mismatch (freshness guard atop the ref allowlist; resolve errors never block); detail opens are gen-guarded (`state.detailGen`, bumped by `drillOut`) and the diff-nav arrows disable on notice renders.
```

- [ ] **Step 10: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-web-hardening
git add internal/web/static/app.js internal/web/static/style.css CHANGELOG.md CLAUDE.md
git commit -m "feat(web): SSE-drop recovery, danger modal options, sidebar persistence, detail-gen + diff-nav guards"
```

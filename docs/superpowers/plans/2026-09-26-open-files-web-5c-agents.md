# Open files on the web — 5c: agent verbs Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (inline, NO subagents — CLAUDE.md) to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `gg open --background`, `gg session files [--json]` and `gg session files focus` work against a live `gg web` page, answered by the web server's own open-files list.

**Architecture:** The web server answers the three open-files verbs SYNCHRONOUSLY in the body of `POST /api/session/steer` (200 + a `steer.Reply`), from the 5b registry `s.ofs`; every other verb keeps its 202. A focus is also emitted to every tab on the live hub, and each tab brings the file up through the 5b `openViewer({id})` path. The CLI's TUI-only `steerTUI` becomes `steerLive`: the TUI when live (its reply decides), else the web; a background open and a focus go to BOTH when both are live.

**Tech Stack:** Go 1.26 (`internal/web`, `internal/steer`, `internal/cli`, `e2e`), vanilla ES modules (`internal/web/static/live.js`).

**Spec:** `docs/superpowers/specs/2026-09-26-open-files-web-design.md` — "Agents (5c)" + the 5c row. TUI reference: `internal/tui/steer_files.go`, `docs/superpowers/plans/2026-09-25-open-files-plan-4-agent-verbs.md`.

## Global Constraints

- The reply words are the TUI's, verbatim: `opened <path> in the background`, `<path> is already open on screen`, `focused <path>`, ` at line N`, ` at line M (line N is past the end, M lines)`, `; closed <path> (20 files open)`, `no open file <x>`, `<path> is not in the working tree`, `no open files`.
- `files` is answered synchronously in the steer POST's response body from `s.ofs` (spec).
- `--background` loads into the list (`s.ofs.open` with tab `""`); never launches a page.
- `file_focus` is broadcast to the tabs (every tab brings it up); unknown → exit 1 `no open file <x>`.
- CLI: TUI when live (its reply decides), else the web; a background open and a focus go to BOTH when both are live; `files` answers from the TUI when live, else from the web. `gg web does not keep open files yet` goes.
- Paths are cut in the MIDDLE (elidePath), never end-cut; key hints only in `#foot`, never in a toast or a box.
- Run the FULL web package tests at every task-done (`go test ./internal/web/`).
- Browser checks ASSERT VISIBILITY and run against the unfixed installed build first.

## Review Focus

1. **A focus with no tab open** — the server has a presence but no browser tab streams: an agent must not be told the file is on screen. The reply appends `; no gg web tab is open to show it` (Task 4 test).
2. **A focus by PATH** (`files focus a.txt:3`) — the tabs must receive the ID, not the path, or two versions of one path (worktree + commit) are ambiguous on the page. The server resolves the path and puts `file_id` on the wire (Task 4 test).
3. **A background open of a file a tab is showing** — must be left alone (`already open on screen`), not moved or re-lined (Task 3 test).
4. **`files` while an op is in flight** — read-only, must answer instead of 409 (Task 2 test); a background open likewise (no steer emitted, only `fanOut`) (Task 3 test).
5. **Both TUI and web live** — the web's answer must not decide the exit code or be mistaken for the TUI's: it prints prefixed `web: …`, the TUI's reply decides (Task 6 test).

---

## File Structure

- `internal/steer/notify.go` — add `PostHTTPReply` (a steer POST that returns the server's `Reply`).
- `internal/web/steer.go` — `toSteerWire` accepts the verbs (+ `FileID` on the wire); `handleSteer` routes the three verbs to the synchronous answers.
- `internal/web/steer_files.go` (new) — `steerFiles`, `steerBackground`, `steerFileFocus`, `steerLanded`, `versionLines`, `steerOK`/`steerFail`.
- `internal/web/openfiles.go` — registry helpers `lookup`, `resolve`, `liveTabs`.
- `internal/web/openfiles_http.go` — `broadcastOpened` (the `opened` name rides `open_files`).
- `internal/web/filecontent.go` — extract `readVersion` (shared with `versionLines`).
- `internal/web/live.go` — `liveMsg.Opened`.
- `internal/web/static/live.js` — the `file_focus` arm + the background toast.
- `internal/cli/session.go`, `internal/cli/session_files.go` — `steerLive` routing.
- `e2e/scenario.go`, `e2e/builder.go`, `e2e/web.go` (new), `e2e/scenarios/s100_session_files_web.toml` (new), `e2e/scenarios/s89_session_cli.toml`, `e2e/scenario_test.go`.
- Docs: `internal/agentskill/using-gg.md` + `agentskill.go` (Version 98 → 99), `README.md`, `CHANGELOG.md`, `docs/CLAUDE-details.md`.

---

### Task 1: `steer.PostHTTPReply`

**Files:**
- Modify: `internal/steer/notify.go`
- Test: `internal/steer/notify_test.go`

**Interfaces:**
- Produces: `func PostHTTPReply(base string, c Command) (Reply, error)` — 2xx: the decoded `Reply`; 409: error `operation in flight`; other non-2xx: the body's `{"error": …}` text as the error, else `gg web answered <status>`.

- [ ] **Step 1: Write the failing test** (append to `internal/steer/notify_test.go`; add imports `net/http`, `net/http/httptest` if missing)

```go
func TestPostHTTPReplyDecodesTheAnswer(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		status  int
		body    string
		wantErr string
		want    Reply
	}{
		{200, `{"id":"1-1","ok":true,"detail":"focused a.txt","files":[{"id":"f1","path":"a.txt","source":"worktree","state":"shown"}]}`, "",
			Reply{ID: "1-1", OK: true, Detail: "focused a.txt", Files: []OpenFile{{ID: "f1", Path: "a.txt", Source: "worktree", State: "shown"}}}},
		{200, `{"id":"1-1","ok":false,"error":"no open file f9"}`, "", Reply{ID: "1-1", Error: "no open file f9"}},
		{400, `{"error":"unknown file id \"x\""}`, `unknown file id "x"`, Reply{}},
		{409, `{"error":"operation in flight"}`, "operation in flight", Reply{}},
		{500, `oops`, "gg web answered 500 Internal Server Error", Reply{}},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/session/steer" || r.Header.Get("Content-Type") != "application/json" {
				http.Error(w, "bad request shape", http.StatusTeapot)
				return
			}
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		}))
		got, err := PostHTTPReply(srv.URL, Command{ID: "1-1", Cmd: "files"})
		srv.Close()
		if tc.wantErr != "" {
			if err == nil || err.Error() != tc.wantErr {
				t.Errorf("%d: err = %v, want %q", tc.status, err, tc.wantErr)
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%d: got %+v, %v; want %+v", tc.status, got, err, tc.want)
		}
	}
	if _, err := PostHTTPReply("", Command{Cmd: "files"}); err == nil {
		t.Error("an empty base must be refused")
	}
}
```

- [ ] **Step 2: Run it — expect FAIL**

Run: `go test ./internal/steer/ -run TestPostHTTPReplyDecodesTheAnswer`
Expected: FAIL — `undefined: PostHTTPReply`.

- [ ] **Step 3: Implement** — in `notify.go`, split the request out of `PostHTTP` and add `PostHTTPReply`:

```go
// postSteerHTTP sends c to an open gg web page's steer endpoint.
func postSteerHTTP(base string, c Command) (*http.Response, error) {
	if base == "" {
		return nil, errors.New("the gg web presence carries no URL")
	}
	body, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(base, "/")+"/api/session/steer", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return (&http.Client{Timeout: httpTimeout}).Do(req)
}

// PostHTTP (body unchanged apart from using postSteerHTTP):
func PostHTTP(base string, c Command) error {
	resp, err := postSteerHTTP(base, c)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusConflict {
		return errors.New("operation in flight")
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("gg web answered %s", resp.Status)
	}
	return nil
}

// PostHTTPReply is PostHTTP for the verbs the gg web SERVER answers itself
// (files, file_focus, a background open — it owns the open-files list): the
// 200's body is the Reply. A refusal the endpoint validated comes back as an
// error carrying the endpoint's own words.
func PostHTTPReply(base string, c Command) (Reply, error) {
	var rep Reply
	resp, err := postSteerHTTP(base, c)
	if err != nil {
		return rep, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusConflict {
		return rep, errors.New("operation in flight")
	}
	if resp.StatusCode/100 != 2 {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &e) == nil && e.Error != "" {
			return rep, errors.New(e.Error)
		}
		return rep, fmt.Errorf("gg web answered %s", resp.Status)
	}
	if err := json.Unmarshal(data, &rep); err != nil {
		return rep, fmt.Errorf("gg web's answer: %w", err)
	}
	return rep, nil
}
```

Add `io` to the imports. Update the `httpTimeout` comment ("answers 202 at once" → "answers at once: 202, or the open-files verbs' reply").

- [ ] **Step 4: Run — expect PASS**

Run: `go test ./internal/steer/`
Expected: PASS (the new test and the existing PostHTTP/NotifyReload tests).

- [ ] **Step 5: Commit**

```bash
git add internal/steer/notify.go internal/steer/notify_test.go
git commit -m "feat(steer): PostHTTPReply — a steer POST that returns the web server's answer"
```

---

### Task 2: the wire accepts the verbs; `files` answers from the list

**Files:**
- Modify: `internal/web/steer.go`
- Create: `internal/web/steer_files.go`
- Test: `internal/web/steer_test.go` (flip `TestToSteerWireRefusesOpenFilesVerbs`), `internal/web/steer_files_test.go` (new)

**Interfaces:**
- Produces: `steerWire.FileID string \`json:"file_id,omitempty"\``; `func steerOK(c steer.Command, detail string) steer.Reply`; `func steerFail(c steer.Command, msg string) steer.Reply`; `func (s *Server) steerFiles(c steer.Command) steer.Reply`.
- Consumes: `s.ofs.list(wt) []steer.OpenFile` (5b).

- [ ] **Step 1: Write the failing tests**

Replace `TestToSteerWireRefusesOpenFilesVerbs` in `steer_test.go` with:

```go
// The open-files verbs reach the web (plan 5c): the stage-4 refusals are gone,
// and what is left is the verbs' own shape rules.
func TestToSteerWireAcceptsOpenFilesVerbs(t *testing.T) {
	t.Parallel()
	for _, c := range []steer.Command{
		{Cmd: "files"},
		{Cmd: "file_focus", FileID: "f1"},
		{Cmd: "file_focus", File: "a.txt", Line: &steer.Line{No: 3}},
		{Cmd: "navigate", File: "a.txt", Background: true, HintKind: "view", HintID: "content"},
	} {
		if _, err := toSteerWire(c); err != nil {
			t.Errorf("toSteerWire(%+v) = %v, want accepted", c, err)
		}
	}
	for _, tc := range []struct {
		c    steer.Command
		want string
	}{
		{steer.Command{Cmd: "file_focus"}, "file_focus needs a file id or a path"},
		{steer.Command{Cmd: "file_focus", FileID: "x1"}, `unknown file id "x1"`},
		{steer.Command{Cmd: "navigate", File: "a.txt", Background: true}, "a background open needs a content link"},
		{steer.Command{Cmd: "navigate", Commit: "HEAD", Background: true, HintKind: "view", HintID: "content"}, "a background open needs a content link"},
	} {
		if _, err := toSteerWire(tc.c); err == nil || err.Error() != tc.want {
			t.Errorf("toSteerWire(%+v) = %v, want %q", tc.c, err, tc.want)
		}
	}
}
```

Create `steer_files_test.go`:

```go
package web

import (
	"net/http"
	"testing"

	"github.com/homeend/gigagit/internal/steer"
)

// steerAsk posts one command and decodes the synchronous answer.
func steerAsk(t *testing.T, s *Server, body string) (int, steer.Reply) {
	t.Helper()
	var rep steer.Reply
	code := postJSON(t, serve(t, s), "/api/session/steer", body, "application/json", "", &rep)
	return code, rep
}

func TestSteerFilesAnswersFromTheList(t *testing.T) {
	t.Parallel()
	s := newSteerServer(t)
	code, rep := steerAsk(t, s, `{"id":"1-1","cmd":"files"}`)
	if code != http.StatusOK || !rep.OK || rep.ID != "1-1" || rep.Detail != "no open files" || len(rep.Files) != 0 {
		t.Fatalf("empty: code=%d rep=%+v", code, rep)
	}
	s.ofs.open(s.service().Root(), ofKey{Src: "worktree", Path: "f.txt"}, "", 3)
	code, rep = steerAsk(t, s, `{"id":"1-2","cmd":"files"}`)
	if code != http.StatusOK || !rep.OK || len(rep.Files) != 1 {
		t.Fatalf("code=%d rep=%+v", code, rep)
	}
	if f := rep.Files[0]; f.ID != "f1" || f.Path != "f.txt" || f.Source != "worktree" || f.Line != 3 || f.State != "background" {
		t.Fatalf("file = %+v", f)
	}
}

// files is read-only: an op in flight does not stop it (only a steer the hub
// must deliver is refused with 409).
func TestSteerFilesAnswersWhileAnOpIsInFlight(t *testing.T) {
	t.Parallel()
	s := newSteerServer(t)
	s.cur = &opRun{} // opInFlight() is true
	if code, rep := steerAsk(t, s, `{"id":"1-1","cmd":"files"}`); code != http.StatusOK || !rep.OK {
		t.Fatalf("code=%d rep=%+v", code, rep)
	}
}
```

- [ ] **Step 2: Run — expect FAIL**

Run: `go test ./internal/web/ -run 'TestToSteerWireAcceptsOpenFilesVerbs|TestSteerFiles'`
Expected: FAIL — the stage-4 refusal (`open files are not supported in gg web yet`), a 400 on `files`.

- [ ] **Step 3: Implement**

In `steer.go`: add `FileID string \`json:"file_id,omitempty"\`` to `steerWire` (after `HintID`, commented "the open file a file_focus brings up (the server resolves a path to it)"). In `toSteerWire`:
- set `w.FileID = c.FileID` in the initial literal; after the `c.Commit` safety check add:

```go
	if c.FileID != "" && !openFileID.MatchString(c.FileID) {
		return w, fmt.Errorf("unknown file id %q", c.FileID)
	}
```

with, beside `steerPanels`:

```go
// openFileID is an open file's id — the registry's "f<n>".
var openFileID = regexp.MustCompile(`^f[0-9]+$`)
```

- delete the `if c.Background { … not supported … }` block; in the `navigate` arm, first thing:

```go
		// A background open loads a WORKING-TREE file into the list (the
		// TUI's rule): only a content link names one.
		if c.Background && (c.File == "" || c.Commit != "" || c.HintKind != model.ContentHintKind || c.HintID != model.ContentHintID) {
			return w, errors.New("a background open needs a content link")
		}
```

- replace the `case "files", "file_focus":` refusal with:

```go
	case "files":
	case "file_focus":
		if c.FileID == "" && c.File == "" {
			return w, errors.New("file_focus needs a file id or a path")
		}
```

In `handleSteer`, right after `s.freezePair(readCtx(r), &wire)` and BEFORE the op-in-flight check:

```go
	// The open-files verbs are answered HERE, synchronously (plan 5c): the
	// server owns the list. files and a background open touch no screen and
	// never ride the hub's steer lane, so an op in flight does not stop them.
	if c.Cmd == "files" {
		writeJSON(w, s.steerFiles(c))
		return
	}
```

Create `steer_files.go`:

```go
package web

import "github.com/homeend/gigagit/internal/steer"

// The open-files steer verbs on the web (plan 5c): gg session files, files
// focus and open --background, answered by the server from its own list —
// the TUI's steer_files.go, word for word in every reply.

func steerOK(c steer.Command, detail string) steer.Reply {
	return steer.Reply{ID: c.ID, OK: true, Detail: detail}
}

func steerFail(c steer.Command, msg string) steer.Reply {
	return steer.Reply{ID: c.ID, Error: msg}
}

// steerFiles answers `gg session files`: the served worktree's open files,
// most recently shown first.
func (s *Server) steerFiles(c steer.Command) steer.Reply {
	r := steerOK(c, "")
	r.Files = s.ofs.list(s.service().Root())
	if len(r.Files) == 0 {
		r.Detail = "no open files"
	}
	return r
}
```

Add `regexp` to `steer.go`'s imports.

- [ ] **Step 4: Run — expect PASS**

Run: `go test ./internal/web/ -run 'TestToSteerWire|TestSteer'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/web/steer.go internal/web/steer_files.go internal/web/steer_test.go internal/web/steer_files_test.go
git commit -m "feat(web): the steer endpoint accepts the open-files verbs; files answers from the list"
```

---

### Task 3: `--background` loads into the list

**Files:**
- Modify: `internal/web/steer_files.go`, `internal/web/steer.go` (`handleSteer`), `internal/web/openfiles.go`, `internal/web/openfiles_http.go`, `internal/web/filecontent.go`, `internal/web/live.go`
- Test: `internal/web/steer_files_test.go`, `internal/web/openfiles_test.go` (registry)

**Interfaces:**
- Produces: `func (r *openFiles) lookup(wt string, k ofKey) (steer.OpenFile, bool)`; `func readVersion(ctx context.Context, svc *domain.Service, src, rev, path string) ([]byte, error)`; `func (s *Server) versionLines(ctx context.Context, k ofKey) (n int, known bool)`; `func steerLanded(lead string, line, n int, known bool, evicted string) string`; `func (s *Server) broadcastOpened(wt, evicted, opened string)`; `liveMsg.Opened string \`json:"opened,omitempty"\``.
- Consumes: `s.ofs.open`, `s.baseline`, `svc.WorktreeFilesPresent`, `domain.SameCheckout`, `s.steerWorktree` (under `s.steerMu`).

- [ ] **Step 1: Write the failing tests**

Registry (append to `openfiles_test.go`):

```go
func TestOpenFilesLookup(t *testing.T) {
	t.Parallel()
	r := newOpenFiles()
	k := ofKey{Src: "worktree", Path: "a.txt"}
	if _, ok := r.lookup("/w", k); ok {
		t.Fatal("lookup found a file never opened")
	}
	r.open("/w", k, "t1", 0)
	f, ok := r.lookup("/w", k)
	if !ok || f.ID != "f1" || f.State != "shown" {
		t.Fatalf("lookup = %+v, %v", f, ok)
	}
	if _, ok := r.lookup("/w", ofKey{Src: "commit", Rev: "abc", Path: "a.txt"}); ok {
		t.Fatal("another version of the path matched")
	}
}
```

Endpoint (append to `steer_files_test.go`; `newRepoDir(t, 1)` holds `f.txt` = `content 1\n`, one line):

```go
func TestSteerBackgroundOpensIntoTheList(t *testing.T) {
	isolateGlobal(t)
	s := newSteerServer(t)
	ts := serve(t, s)
	next, stop := eventsFor(t, ts, "t1")
	defer stop()
	next() // hello
	var rep steer.Reply
	code := postJSON(t, ts, "/api/session/steer",
		`{"id":"1-1","cmd":"navigate","file":"f.txt","hint_kind":"view","hint_id":"content","background":true,"line":{"no":1}}`,
		"application/json", "", &rep)
	if code != http.StatusOK || !rep.OK || rep.Detail != "opened f.txt in the background at line 1" {
		t.Fatalf("code=%d rep=%+v", code, rep)
	}
	m := next()
	if m.Reason != "open_files" || m.Opened != "f.txt" || len(m.Files) != 1 || m.Files[0].State != "background" || m.Files[0].Line != 1 {
		t.Fatalf("event = %+v", m)
	}
	// Past the end: the server's own read says so, in the TUI's words.
	_ = postJSON(t, ts, "/api/session/steer",
		`{"id":"1-2","cmd":"navigate","file":"f.txt","hint_kind":"view","hint_id":"content","background":true,"line":{"no":9}}`,
		"application/json", "", &rep)
	if rep.Detail != "opened f.txt in the background at line 1 (line 9 is past the end, 1 lines)" {
		t.Fatalf("past the end: %+v", rep)
	}
}

func TestSteerBackgroundRefusals(t *testing.T) {
	t.Parallel()
	s := newSteerServer(t)
	s.steerWorktree = s.service().Root()
	for _, tc := range []struct{ body, want string }{
		{`{"id":"1","cmd":"navigate","file":"nope.txt","hint_kind":"view","hint_id":"content","background":true}`, "nope.txt is not in the working tree"},
		{`{"id":"2","cmd":"navigate","file":"f.txt","hint_kind":"view","hint_id":"content","background":true,"worktree":"/elsewhere"}`, "gg web is showing worktree " + s.service().Root() + ", not /elsewhere"},
	} {
		code, rep := steerAsk(t, s, tc.body)
		if code != http.StatusOK || rep.OK || rep.Error != tc.want {
			t.Errorf("%s: code=%d rep=%+v, want error %q", tc.body, code, rep, tc.want)
		}
	}
}

// A file a tab is looking at is left exactly as the user has it.
func TestSteerBackgroundLeavesAShownFileAlone(t *testing.T) {
	t.Parallel()
	s := newSteerServer(t)
	wt := s.service().Root()
	s.ofs.open(wt, ofKey{Src: "worktree", Path: "f.txt"}, "t1", 1)
	code, rep := steerAsk(t, s, `{"id":"1","cmd":"navigate","file":"f.txt","hint_kind":"view","hint_id":"content","background":true,"line":{"no":1}}`)
	if code != http.StatusOK || !rep.OK || rep.Detail != "f.txt is already open on screen" {
		t.Fatalf("code=%d rep=%+v", code, rep)
	}
}

// A background open touches no screen: an op in flight does not stop it.
func TestSteerBackgroundAnswersWhileAnOpIsInFlight(t *testing.T) {
	t.Parallel()
	s := newSteerServer(t)
	s.cur = &opRun{}
	code, rep := steerAsk(t, s, `{"id":"1","cmd":"navigate","file":"f.txt","hint_kind":"view","hint_id":"content","background":true}`)
	if code != http.StatusOK || !rep.OK || rep.Detail != "opened f.txt in the background" {
		t.Fatalf("code=%d rep=%+v", code, rep)
	}
}

func TestSteerLandedWords(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		line, n int
		known   bool
		ev      string
		want    string
	}{
		{0, 5, true, "", "focused a"},
		{3, 5, true, "", "focused a at line 3"},
		{9, 5, true, "", "focused a at line 5 (line 9 is past the end, 5 lines)"},
		{3, 0, false, "", "focused a"},
		{0, 5, true, "b.txt", "focused a; closed b.txt (20 files open)"},
	} {
		if got := steerLanded("focused a", tc.line, tc.n, tc.known, tc.ev); got != tc.want {
			t.Errorf("steerLanded(%d,%d,%v,%q) = %q, want %q", tc.line, tc.n, tc.known, tc.ev, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run — expect FAIL**

Run: `go test ./internal/web/ -run 'TestOpenFilesLookup|TestSteerBackground|TestSteerLandedWords'`
Expected: FAIL — `undefined: lookup`, `steerLanded`, `m.Opened`.

- [ ] **Step 3: Implement**

`openfiles.go`:

```go
// lookup is the entry for version k, if one is open.
func (r *openFiles) lookup(wt string, k ofKey) (steer.OpenFile, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.byWT[wt] {
		if e.key == k {
			return r.wireLocked(e), true
		}
	}
	return steer.OpenFile{}, false
}
```

`live.go` — in `liveMsg`, after `FileID`: `Opened string \`json:"opened,omitempty"\` // open_files: the path an agent just opened in the background`.

`openfiles_http.go` — turn `broadcastOpenFiles` into a wrapper:

```go
func (s *Server) broadcastOpenFiles(wt, evicted string) { s.broadcastOpened(wt, evicted, "") }

// broadcastOpened is broadcastOpenFiles naming the file an agent's
// background open just added, so every tab can say so.
func (s *Server) broadcastOpened(wt, evicted, opened string) {
	if h := s.liveHubRef(); h != nil {
		h.fanOut(liveMsg{Changed: []string{}, Reason: "open_files", Files: s.ofs.list(wt), Evicted: evicted, Opened: opened})
	}
}
```

(keep `broadcastOpenFiles`'s existing comment on `broadcastOpened`).

`filecontent.go` — extract the read so the steer answers count the same lines the page shows:

```go
// readVersion reads path at one version: the bytes ON DISK (a missing file
// is fs.ErrNotExist, passed through), a commit's blob, or a shelf entry's.
// src and rev are the caller's to validate.
func readVersion(ctx context.Context, svc *domain.Service, src, rev, path string) ([]byte, error) {
	switch src {
	case "commit":
		return svc.ShowFile(ctx, rev, path)
	case "shelf":
		return svc.ShelfBlob(ctx, rev)
	}
	return svc.WorktreeFile(ctx, path)
}
```

and in `handleFileContent` keep the switch's validation arms but make each read go through `readVersion(ctx, svc, src, rev, path)` (the `"", "worktree"` arm keeps its `fs.ErrNotExist` → `Missing` answer). Add the `context` import. `go test ./internal/web/ -run TestFileContent` must stay green.

`steer_files.go` — add (imports: `context`, `fmt`, `strings`, `domain`):

```go
// versionLines counts k's lines as the viewer would show them. known is
// false when there are none to land on (missing, empty, too large, a read
// that failed) — the reply then names no line, as the TUI's does.
func (s *Server) versionLines(ctx context.Context, k ofKey) (int, bool) {
	data, err := readVersion(ctx, s.service(), k.Src, k.Rev, k.Path)
	if err != nil || len(data) == 0 || len(data) > domain.MaxDiffBytes {
		return 0, false
	}
	return strings.Count(strings.TrimSuffix(string(data), "\n"), "\n") + 1, true
}

// steerLanded is the TUI's landedDetail over a line count.
func steerLanded(lead string, line, n int, known bool, evicted string) string {
	detail := lead
	if line > 0 && known {
		if line > n {
			detail = fmt.Sprintf("%s at line %d (line %d is past the end, %d lines)", detail, n, line, n)
		} else {
			detail = fmt.Sprintf("%s at line %d", detail, line)
		}
	}
	if evicted != "" {
		detail += fmt.Sprintf("; closed %s (%d files open)", evicted, maxOpenFiles)
	}
	return detail
}

// steerBackground loads a content link's file into the list without
// showing it (gg open --background): no tab moves. A file a tab shows is
// left exactly as the user has it.
func (s *Server) steerBackground(ctx context.Context, c steer.Command) steer.Reply {
	s.steerMu.Lock()
	shown := s.steerWorktree
	s.steerMu.Unlock()
	if c.Worktree != "" && shown != "" && !domain.SameCheckout(c.Worktree, shown) {
		return steerFail(c, "gg web is showing worktree "+shown+", not "+c.Worktree)
	}
	svc := s.service()
	present, err := svc.WorktreeFilesPresent(ctx, []string{c.File})
	if err != nil {
		return steerFail(c, "checking "+c.File+": "+err.Error())
	}
	if !present[c.File] {
		return steerFail(c, c.File+" is not in the working tree")
	}
	wt := svc.Root()
	k := ofKey{Src: "worktree", Path: c.File}
	if f, ok := s.ofs.lookup(wt, k); ok && f.State == "shown" {
		return steerOK(c, c.File+" is already open on screen")
	}
	line := 0
	if c.Line != nil {
		line = c.Line.No
	}
	f, ev := s.ofs.open(wt, k, "", line)
	s.baseline(wt, f.ID, k)
	s.broadcastOpened(wt, ev, f.Path)
	n, known := s.versionLines(ctx, k)
	return steerOK(c, steerLanded("opened "+c.File+" in the background", line, n, known, ev))
}
```

`steer.go` `handleSteer` — extend the synchronous block:

```go
	if c.Background {
		writeJSON(w, s.steerBackground(readCtx(r), c))
		return
	}
```

- [ ] **Step 4: Run — expect PASS**

Run: `go test ./internal/web/ -run 'TestOpenFiles|TestSteer|TestFileContent'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/web/
git commit -m "feat(web): gg open --background loads into the web's open-files list"
```

---

### Task 4: `files focus` — every tab brings it up

**Files:**
- Modify: `internal/web/steer_files.go`, `internal/web/steer.go` (`handleSteer`), `internal/web/openfiles.go`
- Test: `internal/web/steer_files_test.go`, `internal/web/openfiles_test.go`

**Interfaces:**
- Produces: `func (r *openFiles) resolve(wt, id, path string) (steer.OpenFile, bool)` (an id that matches nothing is tried as a path — the TUI's `findOpenFile`); `func (r *openFiles) liveTabs() int`; `func (s *Server) steerFileFocus(ctx context.Context, c steer.Command, wire steerWire) steer.Reply`. On the hub: a `steer` message whose wire is `{cmd:"file_focus", file_id, line}` (never a path).
- Consumes: `s.ofs.cursor`, `s.ofs.focus(wt, id, "")`, `s.ofs.entryKey`, `versionLines`, `steerLanded` (Task 3).

- [ ] **Step 1: Write the failing tests**

Registry (append to `openfiles_test.go`):

```go
func TestOpenFilesResolveAndLiveTabs(t *testing.T) {
	t.Parallel()
	r := newOpenFiles()
	r.open("/w", ofKey{Src: "worktree", Path: "a.txt"}, "", 0)
	r.open("/w", ofKey{Src: "worktree", Path: "b.txt"}, "", 0)
	for _, tc := range []struct{ id, path, want string }{
		{"f1", "", "f1"}, {"", "b.txt", "f2"}, {"b.txt", "", "f2"}, {"f9", "", ""},
	} {
		f, ok := r.resolve("/w", tc.id, tc.path)
		if ok != (tc.want != "") || f.ID != tc.want {
			t.Errorf("resolve(%q,%q) = %+v,%v; want %q", tc.id, tc.path, f, ok, tc.want)
		}
	}
	if r.liveTabs() != 0 {
		t.Fatal("no stream is open")
	}
	r.streamOpened("t1")
	if r.liveTabs() != 1 {
		t.Fatal("liveTabs misses t1")
	}
}
```

Endpoint (append to `steer_files_test.go`):

```go
func TestSteerFileFocusBroadcastsTheIDToEveryTab(t *testing.T) {
	isolateGlobal(t)
	s := newSteerServer(t)
	ts := serve(t, s)
	wt := s.service().Root()
	s.ofs.open(wt, ofKey{Src: "worktree", Path: "f.txt"}, "", 0)
	next, stop := eventsFor(t, ts, "t1")
	defer stop()
	next() // hello
	var rep steer.Reply
	code := postJSON(t, ts, "/api/session/steer", `{"id":"1-1","cmd":"file_focus","file":"f.txt","line":{"no":1}}`, "application/json", "", &rep)
	if code != http.StatusOK || !rep.OK || rep.Detail != "focused f.txt at line 1" {
		t.Fatalf("code=%d rep=%+v", code, rep)
	}
	var sawList, sawSteer bool
	for i := 0; i < 2; i++ {
		m := next()
		switch m.Reason {
		case "open_files":
			sawList = len(m.Files) == 1 && m.Files[0].Line == 1
		case "steer":
			sawSteer = m.Steer != nil && m.Steer.Cmd == "file_focus" && m.Steer.FileID == "f1" && m.Steer.File == "" && m.Steer.Line == 1
		}
	}
	if !sawList || !sawSteer {
		t.Fatalf("list=%v steer=%v — the tabs need the id, never a path", sawList, sawSteer)
	}
}

func TestSteerFileFocusUnknownAndNoTab(t *testing.T) {
	t.Parallel()
	s := newSteerServer(t)
	s.ofs.open(s.service().Root(), ofKey{Src: "worktree", Path: "f.txt"}, "", 0)
	if code, rep := steerAsk(t, s, `{"id":"1","cmd":"file_focus","file_id":"f9"}`); code != http.StatusOK || rep.OK || rep.Error != "no open file f9" {
		t.Fatalf("unknown: code=%d rep=%+v", code, rep)
	}
	// No browser tab streams: the agent must not be told it is on screen.
	if _, rep := steerAsk(t, s, `{"id":"2","cmd":"file_focus","file_id":"f1"}`); !rep.OK || rep.Detail != "focused f.txt; no gg web tab is open to show it" {
		t.Fatalf("no tab: %+v", rep)
	}
}

// A focus DOES ride the hub's steer lane, so it keeps the 409 while an op runs.
func TestSteerFileFocusIs409WhileAnOpIsInFlight(t *testing.T) {
	t.Parallel()
	s := newSteerServer(t)
	s.ofs.open(s.service().Root(), ofKey{Src: "worktree", Path: "f.txt"}, "", 0)
	s.cur = &opRun{}
	if code := steerPost(t, s, `{"id":"1","cmd":"file_focus","file_id":"f1"}`, "application/json"); code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", code)
	}
}
```

- [ ] **Step 2: Run — expect FAIL**

Run: `go test ./internal/web/ -run 'TestOpenFilesResolve|TestSteerFileFocus'`
Expected: FAIL — `undefined: resolve`, `liveTabs`; the endpoint answers 202.

- [ ] **Step 3: Implement**

`openfiles.go`:

```go
// resolve finds an open file by id, else by path (the first — most recently
// shown — version of it). An id that matches nothing is tried as a path, as
// the TUI's findOpenFile does.
func (r *openFiles) resolve(wt, id, path string) (steer.OpenFile, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if id != "" {
		if _, e := r.findLocked(wt, id); e != nil {
			return r.wireLocked(e), true
		}
		path = id
	}
	for _, e := range r.byWT[wt] {
		if e.key.Path == path {
			return r.wireLocked(e), true
		}
	}
	return steer.OpenFile{}, false
}

// liveTabs is how many tabs have a live event stream.
func (r *openFiles) liveTabs() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.streams)
}
```

`steer_files.go`:

```go
// steerFileFocus brings an open file to the front (gg session files focus):
// every tab brings it up, as the switcher's enter would, at the agent's line
// when it named one. The reply's line is counted from the server's own read.
func (s *Server) steerFileFocus(ctx context.Context, c steer.Command, wire steerWire) steer.Reply {
	wt := s.service().Root()
	f, ok := s.ofs.resolve(wt, c.FileID, c.File)
	if !ok {
		name := c.FileID
		if name == "" {
			name = c.File
		}
		return steerFail(c, "no open file "+name)
	}
	line := 0
	if c.Line != nil {
		line = c.Line.No
	}
	s.ofs.cursor(wt, f.ID, line) // a line > 0 becomes the server's line
	s.ofs.focus(wt, f.ID, "")
	s.broadcastOpenFiles(wt, "")
	// The tabs get the ID: a path may name two versions (working tree and a
	// commit), and the page brings files back by id.
	wire.FileID, wire.File = f.ID, ""
	if h := s.liveHubRef(); h != nil {
		h.emitSteer(liveMsg{Changed: []string{}, Reason: "steer", Steer: &wire})
	}
	k, _ := s.ofs.entryKey(wt, f.ID)
	n, known := s.versionLines(ctx, k)
	detail := steerLanded("focused "+f.Path, line, n, known, "")
	if s.ofs.liveTabs() == 0 {
		detail += "; no gg web tab is open to show it"
	}
	return steerOK(c, detail)
}
```

`steer.go` `handleSteer` — after the op-in-flight 409 check and before the generic `emitSteer`:

```go
	if c.Cmd == "file_focus" {
		writeJSON(w, s.steerFileFocus(readCtx(r), c, wire))
		return
	}
```

Update `handleSteer`'s doc comment: "It answers 202 at once — … — except the open-files verbs, which the server answers itself with a 200 and a steer.Reply."

- [ ] **Step 4: Run — expect PASS**

Run: `go test ./internal/web/ -run 'TestOpenFiles|TestSteer'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/web/
git commit -m "feat(web): gg session files focus brings an open file up in every tab"
```

---

### Task 5: the page — the focus arm and the background toast

**Files:**
- Modify: `internal/web/static/live.js`
- Test: `internal/web/steer_files_test.go` (source pins, the suite's live.js style)

**Interfaces:**
- Consumes: the `file_focus` steer wire (`file_id`, `line`) from Task 4; `liveMsg.opened` from Task 3; `openViewer({ id, line })` (5b — `focus` op, then `pickLine`/`landLine`, which already handle a line past the end with the viewer's notice); `opLine` (already imported from `./ops.js`).

- [ ] **Step 1: Write the failing test**

```go
func TestLiveJSRoutesTheOpenFilesVerbs(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "live.js"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, want := range []string{
		`case "file_focus":`,
		`openViewer({ id: s.file_id, line: s.line || 0 })`,
		`if (msg.opened) opLine(msg.opened + " opened in the background", false);`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("live.js: missing %q", want)
		}
	}
	// Key hints live in the footer only (ruling): the toast names no key.
	if strings.Contains(src, `opened in the background — ctrl`) {
		t.Error("live.js: the background toast advertises a key")
	}
}
```

(add `os`, `path/filepath`, `strings` to the test file's imports.)

- [ ] **Step 2: Run — expect FAIL**

Run: `go test ./internal/web/ -run TestLiveJSRoutesTheOpenFilesVerbs`
Expected: FAIL — missing `case "file_focus":`.

- [ ] **Step 3: Implement** — in `live.js`:

`applySteer`'s switch, after the `navigate` arm:

```js
      case "file_focus":
        return await steerFileFocus(s);
```

below `steerNavigateContent`:

```js
// steerFileFocus brings an open file up in THIS tab — every tab gets the
// command (gg session files focus): the switcher's enter, at the agent's
// line when it named one. The server resolved a path to the id.
async function steerFileFocus(s) {
  await openViewer({ id: s.file_id, line: s.line || 0 });
}
```

in the `open_files` branch of the stream handler, before `viewerOpenFiles`:

```js
      if (msg.opened) opLine(msg.opened + " opened in the background", false);
```

- [ ] **Step 4: Run — expect PASS**

Run: `go test ./internal/web/`
Expected: PASS (full web package).

- [ ] **Step 5: Commit**

```bash
git add internal/web/static/live.js internal/web/steer_files_test.go
git commit -m "feat(web): the page brings a focused file up and says when an agent opens one"
```

---

### Task 6: CLI routing — `steerLive`

**Files:**
- Modify: `internal/cli/session.go`, `internal/cli/session_files.go`
- Test: `internal/cli/session_files_test.go`

**Interfaces:**
- Produces: `func steerLive(dir string, c steer.Command, both, noWait bool, stdout, stderr io.Writer) (rep steer.Reply, code int, ok bool)` — replaces `steerTUI`. `both=false` (`files`): the TUI when live, else the web. `both=true` (a background open, a focus): the web too when both are live — its answer printed `web: <detail>` (stdout) / `web: <error>` (stderr) and the TUI's reply decides; web alone → its reply is THE reply. Neither live → stderr `no gg session for this worktree`, exit 1.
- Consumes: `steer.PostHTTPReply` (Task 1); `routeFor`, `preferredInbox`, `steer.Post`, `steer.AwaitReply` (existing).

- [ ] **Step 1: Write the failing tests** — in `session_files_test.go`, replace `TestSessionFilesNeedsALiveTUI` with:

```go
func TestSessionFilesNeedsALiveSession(t *testing.T) {
	t.Parallel()
	var out, errb bytes.Buffer
	if code := runSession(t.TempDir(), nil, []string{"files"}, &out, &errb); code != 1 || !strings.Contains(errb.String(), "no gg session for this worktree") {
		t.Fatalf("exit=%d stderr=%q", code, errb.String())
	}
}

// Web only: the page's server answers from its own list.
func TestSessionFilesAnswersFromTheWeb(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	web, srv := newSteerServer(t, 200, `{"id":"x","ok":true,"files":[{"id":"f1","path":"a.txt","source":"worktree","line":3,"state":"background"}]}`)
	liveWebPresence(t, dir, srv.URL)
	var out, errb bytes.Buffer
	if code := runSession(dir, nil, []string{"files"}, &out, &errb); code != 0 || out.String() != "f1\ta.txt\tworktree\t:3\tbackground\n" {
		t.Fatalf("exit=%d out=%q err=%q", code, out.String(), errb.String())
	}
	if got := web.commands(); len(got) != 1 || got[0].Cmd != "files" {
		t.Fatalf("posted %+v", got)
	}
}

// TUI and web both live: files answers from the TUI alone.
func TestSessionFilesPrefersTheTUI(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	web, srv := newSteerServer(t, 200, `{"ok":true}`)
	liveWebPresence(t, dir, srv.URL)
	answer(t, dir, func(c steer.Command) steer.Reply {
		return steer.Reply{ID: c.ID, OK: true, Files: []steer.OpenFile{{ID: "f4", Path: "t.txt", Source: "worktree", State: "shown"}}}
	})
	var out, errb bytes.Buffer
	if code := runSession(dir, nil, []string{"files"}, &out, &errb); code != 0 || !strings.HasPrefix(out.String(), "f4\tt.txt") {
		t.Fatalf("exit=%d out=%q err=%q", code, out.String(), errb.String())
	}
	if len(web.commands()) != 0 {
		t.Error("files was posted to the web page while a TUI was live")
	}
}

func TestSessionFilesFocusWebOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, srv := newSteerServer(t, 200, `{"ok":false,"error":"no open file f9"}`)
	liveWebPresence(t, dir, srv.URL)
	var out, errb bytes.Buffer
	if code := runSession(dir, nil, []string{"files", "focus", "f9"}, &out, &errb); code != 1 || errb.String() != "no open file f9\n" {
		t.Fatalf("exit=%d out=%q err=%q", code, out.String(), errb.String())
	}
}

// Both live: the focus reaches both; the web's answer is labelled, the
// TUI's reply decides the exit code.
func TestSessionFilesFocusGoesToBoth(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	web, srv := newSteerServer(t, 200, `{"ok":false,"error":"no open file f2"}`)
	liveWebPresence(t, dir, srv.URL)
	answer(t, dir, func(c steer.Command) steer.Reply { return steer.Reply{ID: c.ID, OK: true, Detail: "focused a.txt at line 2"} })
	var out, errb bytes.Buffer
	code := runSession(dir, nil, []string{"files", "focus", "f2:2"}, &out, &errb)
	if code != 0 || out.String() != "focused a.txt at line 2\n" || errb.String() != "web: no open file f2\n" {
		t.Fatalf("exit=%d out=%q err=%q", code, out.String(), errb.String())
	}
	if got := web.commands(); len(got) != 1 || got[0].Cmd != "file_focus" || got[0].FileID != "f2" || got[0].Line == nil || got[0].Line.No != 2 {
		t.Fatalf("web got %+v", got)
	}
}

// A web that cannot be reached, alone: exit 1 with its error.
func TestSessionFilesWebErrorExitsOne(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, srv := newSteerServer(t, 409, `{"error":"operation in flight"}`)
	liveWebPresence(t, dir, srv.URL)
	var out, errb bytes.Buffer
	if code := runSession(dir, nil, []string{"files", "focus", "f1"}, &out, &errb); code != 1 || !strings.Contains(errb.String(), "web: operation in flight") {
		t.Fatalf("exit=%d err=%q", code, errb.String())
	}
}
```

Also update the other `"no gg TUI session for this worktree"` expectation in this file (line ~187, the background-open test) to `"no gg session for this worktree"`.

- [ ] **Step 2: Run — expect FAIL**

Run: `go test ./internal/cli/ -run 'TestSessionFiles|TestOpenBackground'`
Expected: FAIL — web-only runs print `gg web does not keep open files yet`.

- [ ] **Step 3: Implement** — in `session.go`, replace `steerTUI` with:

```go
// steerLive posts c to the live session and waits for its answer: the TUI
// when it is live (its reply decides), else the gg web page's server, which
// answers the open-files verbs itself. With both set (a background open, a
// focus) a live page gets it TOO — as navigate does — and its answer is
// printed labelled "web:". ok is true when a reply came; otherwise code is
// the exit status, the reason already printed (nothing live, --no-wait's id,
// a timeout's "queued", which is exit 0 as in sendSteer).
func steerLive(dir string, c steer.Command, both, noWait bool, stdout, stderr io.Writer) (rep steer.Reply, code int, ok bool) {
	dir = preferredInbox(dir)
	r := routeFor(dir)
	if !r.tuiOK && !r.webOK {
		fmt.Fprintln(stderr, "no gg session for this worktree")
		return rep, 1, false
	}
	if c.ID == "" {
		c.ID = steer.NewID() // one id for both deliveries, as sendSteer
	}
	if r.webOK && (both || !r.tuiOK) {
		// The web answers synchronously, so even --no-wait gets its reply.
		wrep, err := steer.PostHTTPReply(r.web.URL, c)
		if !r.tuiOK {
			if err != nil {
				fmt.Fprintln(stderr, "web:", err)
				return rep, 1, false
			}
			return wrep, 0, true
		}
		switch {
		case err != nil:
			fmt.Fprintln(stderr, "web:", err)
		case !wrep.OK:
			fmt.Fprintln(stderr, "web:", wrep.Error)
		case wrep.Detail != "":
			fmt.Fprintln(stdout, "web:", wrep.Detail)
		}
	}
	c.Wait = !noWait
	id, err := steer.Post(dir, c)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return rep, 1, false
	}
	if noWait {
		fmt.Fprintln(stdout, id)
		return rep, 0, false
	}
	rep, got := steer.AwaitReply(dir, id, steerReplyWaitForTest)
	if !got {
		fmt.Fprintln(stdout, "queued: no answer from the TUI within 2s")
		return rep, 0, false
	}
	return rep, 0, true
}
```

`sendBackground`: `rep, code, ok := steerLive(dir, c, true, noWait, stdout, stderr)`; its comment: "posts a background open to the live session(s): it loads the file into the open-files list without touching the screen. With nothing live it never launches one — a launch IS the screen."

`session_files.go`: `sessionFiles` → `steerLive(dir, steer.Command{Cmd: "files"}, false, false, stdout, stderr)`; `sessionFilesFocus` → `steerLive(dir, c, true, *noWait, stdout, stderr)`; `sessionFiles`'s comment "the live TUI's open files" → "the live session's open files (the TUI's, else gg web's)".

- [ ] **Step 4: Run — expect PASS**

Run: `go test ./internal/cli/`
Expected: PASS (grep for any other `no gg TUI session` / `does not keep open files` expectation in `internal/cli` and update it to the new words — `rtk grep -n "no gg TUI session\|does not keep open files" internal/cli` must print nothing afterwards).

- [ ] **Step 5: Commit**

```bash
git add internal/cli/
git commit -m "feat(cli): the open-files verbs reach gg web — steerLive routes TUI, web or both"
```

---

### Task 7: e2e — a real `gg web` in a scenario

**Files:**
- Modify: `e2e/scenario.go` (`Input.Web`), `e2e/builder.go` (start it), `e2e/scenarios/s89_session_cli.toml`, `e2e/scenario_test.go`
- Create: `e2e/web.go`, `e2e/scenarios/s100_session_files_web.toml`

**Interfaces:**
- Produces: `[input] web = true` — the sandbox's `local` repo is served by an in-process `web.Serve(ctx, dir, "127.0.0.1:0", false, nil)` whose steering presence is live before the first run; stopped (and awaited) at cleanup.

- [ ] **Step 1: Write the failing scenario** — `e2e/scenarios/s100_session_files_web.toml`:

```toml
name = "session files with only gg web live: the web server answers from its list"

[input]
web = true
steps = [
  { write = "a.txt", content = "alpha\nbravo\ncharlie\n" },
  { commit = "seed" },
]

[[run]]
cmd             = ["session", "files"]
exit            = 0
stdout_excludes = ["a.txt"]

[[run]]
cmd             = ["open", "--background", "gg://{{cwd}}/a.txt?view=content"]
exit            = 0
stdout_contains = ["opened a.txt in the background"]

[[run]]
cmd             = ["session", "files"]
exit            = 0
stdout_contains = ["f1\ta.txt\tworktree\t-\tbackground"]

[[run]]
cmd             = ["session", "files", "--json"]
exit            = 0
stdout_contains = ['"id": "f1"', '"path": "a.txt"']

[[run]]
cmd             = ["session", "files", "focus", "f1:2"]
exit            = 0
stdout_contains = ["focused a.txt at line 2; no gg web tab is open to show it"]

[[run]]
cmd             = ["session", "files"]
exit            = 0
stdout_contains = ["f1\ta.txt\tworktree\t:2\tbackground"]

[[run]]
cmd             = ["session", "files", "focus", "f9"]
exit            = 1
stderr_contains = ["no open file f9"]

[[run]]
cmd             = ["open", "--background", "gg://{{cwd}}/nope.txt?view=content"]
exit            = 1

[expect]
branch = "main"
```

(If `gg link` refuses a content link to a missing file at resolve time and the exit is 2, rule the row to that exit in the ledger — the point is "not 0".)

In `s89_session_cli.toml` change the four `"no gg TUI session for this worktree"` to `"no gg session for this worktree"`, and the comment above them to "with nothing live every open-files verb says so — and a background open never launches a session." In `e2e/scenario_test.go` `TestRunMissingStderr`, use `"no gg session"` / `"no gg session for this worktree\n"`.

- [ ] **Step 2: Run — expect FAIL**

Run: `go test ./e2e/ -run 'TestScenarios/s100_session_files_web|TestScenarios/s89'`
Expected: FAIL — s100: `strict mode: fields in the document are missing in the target struct` (or the equivalent unknown-field error for `web`); s89: stderr missing the new words.

- [ ] **Step 3: Implement**

`scenario.go` `Input`: `Web bool \`toml:"web"\` // serve local with an in-process gg web (live steering presence) before the runs`.

`builder.go` `buildSandbox`, after `sb.snapshotInput(t)`:

```go
	if sc.Input.Web {
		startWeb(t, sb)
	}
```

`e2e/web.go`:

```go
package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
	"github.com/homeend/gigagit/internal/web"
)

// startWeb serves the sandbox's local repo with a real in-process gg web
// ([input] web = true) and waits until its steering presence is live, so a
// scenario's `gg session …` runs reach it exactly as they reach a user's page.
// The server is stopped, and awaited, when the scenario ends.
func startWeb(t *testing.T, sb *Sandbox) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	exited := make(chan struct{})
	var serveErr error
	go func() {
		defer close(exited)
		serveErr = web.Serve(ctx, sb.LocalDir, "127.0.0.1:0", false, nil)
	}()
	t.Cleanup(func() {
		cancel()
		<-exited
	})
	svc := domain.Open(sb.LocalDir)
	cd, err := svc.GitCommonDir(ctx)
	if err != nil {
		t.Fatal(err)
	}
	top, err := svc.TopLevel(ctx)
	if err != nil {
		t.Fatal(err)
	}
	dir := config.SessionSteerDir(cd, top)
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); {
		if _, ok := steer.Live(dir, steer.WebPresence); ok {
			return
		}
		select {
		case <-exited:
			t.Fatalf("gg web exited before claiming its presence: %v", serveErr)
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Fatal("gg web never claimed its steering presence")
}
```

- [ ] **Step 4: Run — expect PASS**

Run: `go test ./e2e/ -run 'TestScenarios/s100_session_files_web|TestScenarios/s89|TestRunMissingStderr'` then `go test ./internal/archtest/`
Expected: PASS (archtest: e2e is a test harness outside `internal/`; if a rule forbids importing `internal/web` from `e2e`, ledger a ruling and add `e2e` to that rule's allowlist beside `cmd/gg`).

- [ ] **Step 5: Commit**

```bash
git add e2e/
git commit -m "test(e2e): [input] web = true serves a real gg web; web-only session files rows"
```

---

### Task 8: docs + skill

**Files:**
- Modify: `internal/agentskill/using-gg.md`, `internal/agentskill/agentskill.go` (Version 98 → 99), `README.md` (~line 1231–1242), `CHANGELOG.md`, `docs/CLAUDE-details.md` (the "Content links" section: a "Web agent verbs (5c)" paragraph after "Web open files (5b)")

- [ ] **Step 1: using-gg.md** — in "Open files — let the user read along": first sentence → "The TUI and `gg web` each keep up to 20 files open per worktree (the ctrl+\ switcher lists them). You can use that list:"; "Needs a live TUI (exit 1 otherwise — it never launches one)" → "Needs a live TUI or `gg web` page (exit 1 otherwise — it never launches one); with both live it goes to both and the TUI's answer decides (the page's is printed `web: …`)"; after the `files focus` bullet add: "With only `gg web` live, the page's server answers from its own list; a focus brings the file up in every open tab (`; no gg web tab is open to show it` when none is)." Bump `Version = 99` in `agentskill.go`.

- [ ] **Step 2: README** — replace "These need a running TUI (they never start one); `gg web` does not keep open files yet." with "These work against a running TUI or `gg web` (they never start one): the web page's server answers from its shared list, and a focus brings the file up in every open tab."

- [ ] **Step 3: CHANGELOG** — a new top entry "Open files on the web — agent verbs (5c)": `gg open --background`, `gg session files [--json]` and `gg session files focus` work with `gg web`; the server answers from its list; a focus brings the file up in every tab; with TUI and web both live a background open and a focus go to both (TUI decides); the page says "<path> opened in the background"; e2e `[input] web = true`.

- [ ] **Step 4: CLAUDE-details.md** — "Web agent verbs (5c)" paragraph: synchronous 200 + `steer.Reply` for `files`/background/`file_focus` on `/api/session/steer` (every other verb 202); `files` and a background open skip the op-in-flight 409 (no hub steer), `file_focus` keeps it; the wire carries `file_id` (never a path); `steerLanded` = the TUI's `landedDetail`; `readVersion` shared with `/api/file-content`; `liveMsg.opened`; CLI `steerLive(both)`; `steer.PostHTTPReply`; e2e `[input] web = true` (`e2e/web.go`).

- [ ] **Step 5: Verify + commit**

Run: `go test ./internal/agentskill/ ./internal/web/`
Expected: PASS (agentskill has a version/content drift test).

```bash
git add internal/agentskill/ README.md CHANGELOG.md docs/CLAUDE-details.md
git commit -m "docs: the open-files agent verbs work with gg web (skill v99)"
```

---

### Task 9: gates, verify binary, browser check

- [ ] **Step 1:** `./test.sh > $WS/test.log 2>&1` then `./test.sh race > $WS/race.log 2>&1` (≈30 min on /mnt; never edit the tree meanwhile). Expected: all stages green; read the tails.

- [ ] **Step 2: verify binary** — `go build -o $SCRATCH/gg-5c ./cmd/gg`.

- [ ] **Step 3: browser check (playwright, ASSERTS VISIBILITY)** — first against the installed `~/go/bin/gg` (unfixed: step b must fail with `gg web does not keep open files yet`), then against `$SCRATCH/gg-5c`. Isolated `XDG_STATE_HOME`/`XDG_CONFIG_HOME` in the scratchpad; a throwaway repo with `a.txt` (5 lines) and `b.txt`; `gg web --addr 127.0.0.1:0`, URL read from its log; kill only that PID.
  a. Load the page (one tab); wait for the SSE hello.
  b. `gg open --background gg://<repo>/a.txt?view=content` → exit 0, stdout `opened a.txt in the background`; the page's status line visibly shows `a.txt opened in the background`; the viewer is NOT visible.
  c. `ctrl+\` → the switcher is visible and lists `a.txt` with `○`; esc.
  d. `gg session files` → `f1\ta.txt\tworktree\t-\tbackground`.
  e. `gg session files focus f1:3` → stdout `focused a.txt at line 3`; the viewer is visible, its title contains `a.txt`, the cursor row (`.vline.cur` or the 5a cursor class) is line 3 and in the viewport.
  f. `gg session files` → the row reads `:3\tshown`.
  g. `gg session files focus f9` → exit 1, `no open file f9`.
  Record pass/fail per step for both builds in the ledger.

- [ ] **Step 4:** final whole-branch self-review (no subagents — CLAUDE.md), ledger it; then ask the user before merging.

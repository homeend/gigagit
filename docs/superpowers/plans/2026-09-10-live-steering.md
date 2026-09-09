# Phase 3: live steering (`gg session` → the open TUI / web page) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an agent that has just left review notes put the human's open gg window on the line it means — `gg session navigate|reload|focus|highlight|status` posts a small command into a per-worktree file inbox, the running TUI (and an open `gg web` page) applies it and answers, and every `gg note` mutation posts a free `reload notes` so CLI-written notes reach a live TUI without a manual refresh.

**Architecture:** A new stdlib-plus-fsnotify DAG leaf `internal/steer` owns the on-disk protocol: presence files (liveness = a fresh mtime, not a pid probe), command files (temp+rename, drained and unlinked by the consumer), and reply files. `internal/config` grows the path math (`SessionSteerDir`) and one `[ui]` key. `internal/tui` grows a consumer (`tui/steer.go`) that rides the existing 1 s `heartbeatMsg` tick plus an fsnotify watch, with a `pendingSteer` state drained by the same load-complete handlers `pendingGotoTip` uses. `internal/cli` grows `gg session`, which resolves `--hunk N` through phase 2's `HunkDiffSpec`/`DiffHunks` so the consumer only ever sees file + target + side + line. `internal/web` writes its own presence and takes the same command over `POST /api/session/steer` onto the live hub. No new engine op.

**Tech Stack:** Go 1.26, standard library plus `github.com/fsnotify/fsnotify` (already a direct dependency, used by `internal/gitwatch`), Bubble Tea for the TUI consumer, `github.com/pelletier/go-toml/v2` for the config key, the existing `gittest` / `newTestRepo` helpers, plain ES modules for the web page.

**Spec:** `docs/superpowers/specs/2026-09-08-hunk-parity-roadmap.md` — **§4.6 "Phase 3 design: live steering" is the binding authority**; §4.5 (phase 2, the agent lane) and §4.4 (phase 1, notes core) describe the seams this consumes and are NOT re-planned here.

## Global Constraints

- **Worktree:** all work happens in `/mnt/t/others/gigagit.worktrees/feat-live-steering` on branch `feat/live-steering`. Prefix every shell command with `cd /mnt/t/others/gigagit.worktrees/feat-live-steering &&`; use absolute paths under that directory for every file read or write. Never touch the main checkout at `/mnt/t/others/gigagit`.
- **Go 1.26.** Module `github.com/homeend/gigagit`.
- **Layering:** `internal/tui` and `internal/cli` NEVER import `internal/git` (nor `internal/notes`/`shelf`/`bookmark`/`profile`/`prefix`); they reach git and the stores through `internal/domain`. `internal/mcp` and `internal/web` are domain-only too. `internal/steer` imports **stdlib + `github.com/fsnotify/fsnotify` only** — never `config`, `domain`, `model` or any gg package. `internal/archtest/import_guard_test.go` gains the `internal/steer` row in Task 1.
- **Tests call `t.Parallel()`** and **MUST NOT use `t.Setenv`** — `t.Setenv` panics in a parallel test. The steer directory is **injected**, never read from the environment in a test:
  - TUI: the `Model` field `m.steerDir string` (set by tests right after `New(svc)`, exactly as `session_snapshot_test.go:144` sets `m.snapshotPath`).
  - Web: the `Server` field `srv.steerDir string`.
  - CLI: the internal entry point `runSession(dir string, svc *domain.Service, args []string, stdout, stderr io.Writer) int` takes the directory as its first parameter; `cmdSession` resolves it from config, tests pass `t.TempDir()`.
  - MCP: the `Server` field `s.steerDir string`.
  - `internal/steer` itself takes `dir string` in every exported function — it has no notion of a state home.

  The ONE exception: a CLI test that must exercise the real `cli.Run` →
  `config.SessionSteerDir` path may call `t.Setenv("XDG_STATE_HOME", t.TempDir())`
  and must then NOT call `t.Parallel()`, mirroring the existing
  `internal/cli` note tests. Every other test injects.
- **Real git in `t.TempDir()`** via the existing helpers (`internal/cli/worktree_test.go:16 newCLIRepo`, the `internal/tui` `newTestRepo`-style helpers, `internal/gittest`), or `gitexec.FakeRunner` for argv assertions. Follow TDD: failing test first, watch it fail, minimal implementation, watch it pass.
- **Every user-visible TUI string goes through `i18n.T` with a string LITERAL key**, and the same key must be added to all four bundles `internal/i18n/lang/{ja,ko,zh,ru}.toml` under `[strings]` in the SAME change. AST gates (`i18n_scan_test.go`, `options_vocab_test.go`, `menu_labels_test.go`, `engine_prose_test.go`, `render_order_test.go`) fail otherwise. Never re-sort a bundle; insert near related keys. Format verbs must match between key and translation.
- **Protocol strings stay English** and are NEVER routed through `i18n.T`: every `Reply.Error` / `Reply.Detail` string, every command JSON value (`"navigate"`, `"unstaged"`, `"new"`, `"warn"`, `"next_note"`, …), every CLI stdout/stderr line, every `settingDoc` comment.
- **Commit trailers:** every commit message ends with these two lines, in this order:
  ```
  Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU
  ```
- **No `git commit --amend`** — concurrent agents may share this worktree; always make a new commit.
- **Stage by explicit path** (`git add <path> <path>`), never `git add -A` / `git add .`.
- **gofmt:** run `gofmt -l <changed .go files>` before each commit; the output must be empty.
- **Each task ends green** on `go test ./internal/<pkg>/...` for every package it touched (the exact commands are in the task's last steps). `./test.sh unit` must be green before the final task's commit; `./test.sh race` before the human merges.
- **`CHANGELOG.md` contains a literal `<<<<<<<` line inside an old entry.** It is real content, not a conflict marker. Edit CHANGELOG only with `Edit` anchored on nearby prose; never run a "conflict marker cleanup" over it, never rewrite the file wholesale.
- **Windows path notation:** never string-compare `/` against `\`. Compare filesystem paths with `filepath.Clean`/`filepath.Rel`; repo-relative paths in a command are git slash form (`filepath.ToSlash`).
- **Liveness is mtime only.** `steer.Live` reports a presence whose mtime is under 5 s old. There is no pid probe anywhere in this plan; `Presence.PID` is display-only. (§4.6's "Live notes for free" paragraph says "pid alive" — that phrasing predates the 2026-09-10 mtime ruling in the same section and is superseded by it.)
- **`opAffectedSources` is untouched** — this plan adds no engine operation.

---

### Task 1: `internal/steer` — the on-disk steering protocol

The DAG leaf every later task consumes. Nothing else in this plan can start
until it exists.

**Files:**
- Create: `internal/steer/steer.go` (types, `NewID`, `Post`, `Drain`, `PostReply`, `AwaitReply`, `Discard`)
- Create: `internal/steer/presence.go` (`Touch`, `Live`, `Remove`)
- Create: `internal/steer/watch.go` (`Watch`, `*Watcher`)
- Create: `internal/steer/steer_test.go`
- Create: `internal/steer/presence_test.go`
- Create: `internal/steer/watch_test.go`
- Modify: `internal/archtest/import_guard_test.go` (add the `steer` DAG row + a leaf test)
- Modify: `CLAUDE.md` (one package-map row)

**Interfaces:**
- Consumes: nothing from this plan. Stdlib (`encoding/json`, `os`, `path/filepath`, `sort`, `strconv`, `strings`, `sync`, `time`) plus `github.com/fsnotify/fsnotify` (already a direct module dependency — see `internal/gitwatch/watcher.go:11`).
- Produces:
  ```go
  // internal/steer/steer.go
  const (
      TUIPresence     = "tui.json"
      WebPresence     = "web.json"
      MaxCommandBytes = 16 << 10
      LiveWindow      = 5 * time.Second
  )

  type Target struct {
      State  string `json:"state,omitempty"`  // "unstaged" | "staged" | "untracked" | "commit"
      Commit string `json:"commit,omitempty"` // full 40-hex sha when State == "commit"
  }
  type Line struct {
      Side string `json:"side"` // "new" | "old"
      No   int    `json:"no"`   // 1-based line number on that side
  }
  type Command struct {
      ID      string   `json:"id"`
      Cmd     string   `json:"cmd"`               // "navigate" | "reload" | "focus" | "highlight" | "highlight_clear"
      File    string   `json:"file,omitempty"`    // repo-relative, git slash form
      Target  *Target  `json:"target,omitempty"`
      Commit  string   `json:"commit,omitempty"`  // navigate: reveal this commit, no file
      Line    *Line    `json:"line,omitempty"`
      Step    string   `json:"step,omitempty"`    // "next_note" | "prev_note"
      Sources []string `json:"sources,omitempty"` // reload: "notes" | "status" | "all"
      Panel   string   `json:"panel,omitempty"`   // focus: a panelProtoName
      Side    string   `json:"side,omitempty"`    // highlight: "new" | "old"
      Start   int      `json:"start,omitempty"`
      End     int      `json:"end,omitempty"`
      Tone    string   `json:"tone,omitempty"`    // "info" | "warn" | "error"
      Wait    bool     `json:"wait,omitempty"`
  }
  type Reply struct {
      ID     string `json:"id"`
      OK     bool   `json:"ok"`
      Detail string `json:"detail,omitempty"`
      Error  string `json:"error,omitempty"`
  }
  type Presence struct {
      PID      int    `json:"pid"`
      Worktree string `json:"worktree"`
      Started  string `json:"started,omitempty"` // RFC3339
      URL      string `json:"url,omitempty"`     // web.json only
  }

  func NewID() string
  func Post(dir string, c Command) (string, error)
  func Drain(dir string) []Command
  func PostReply(dir string, r Reply) error
  func AwaitReply(dir, id string, timeout time.Duration) (Reply, bool)
  func Discard(dir string)

  // internal/steer/presence.go
  func Touch(dir, name string, p Presence) error
  func Live(dir, name string) (Presence, bool)
  func Remove(dir, name string)

  // internal/steer/watch.go
  type Watcher struct{ /* unexported */ }
  func Watch(dir string) (*Watcher, error)
  func (w *Watcher) Events() <-chan struct{}
  func (w *Watcher) Close()
  ```

- [ ] **Step 1: Write the failing command round-trip test**

Create `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/steer/steer_test.go`:

```go
package steer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPostDrainRoundTripAndOrder(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "inbox") // Post must create the dir
	ids := make([]string, 0, 3)
	for i, cmd := range []string{"navigate", "reload", "focus"} {
		id, err := Post(dir, Command{ID: "100000000000000000" + string(rune('0'+i)) + "-7", Cmd: cmd, Wait: true})
		if err != nil {
			t.Fatalf("Post %s: %v", cmd, err)
		}
		ids = append(ids, id)
	}
	got := Drain(dir)
	if len(got) != 3 {
		t.Fatalf("Drain = %d commands, want 3", len(got))
	}
	for i, want := range []string{"navigate", "reload", "focus"} {
		if got[i].Cmd != want || got[i].ID != ids[i] {
			t.Errorf("command %d = %+v, want cmd %q id %q (name order = post order)", i, got[i], want, ids[i])
		}
		if !got[i].Wait {
			t.Errorf("command %d lost Wait", i)
		}
	}
	if again := Drain(dir); len(again) != 0 {
		t.Errorf("second Drain = %+v, want empty — Drain unlinks what it returns", again)
	}
}

func TestPostFillsAnEmptyID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	id, err := Post(dir, Command{Cmd: "reload", Sources: []string{"notes"}})
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	if !strings.Contains(id, "-") {
		t.Fatalf("generated id = %q, want <unixnano>-<pid>", id)
	}
	got := Drain(dir)
	if len(got) != 1 || got[0].ID != id {
		t.Fatalf("Drain = %+v, want the generated id %q", got, id)
	}
	if len(got[0].Sources) != 1 || got[0].Sources[0] != "notes" {
		t.Errorf("Sources = %+v, want [notes]", got[0].Sources)
	}
}

func TestDrainNeverSeesAHalfWrittenFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// A temp file mid-write must not match the cmd-*.json glob.
	if err := os.WriteFile(filepath.Join(dir, "steer-halfway.tmp"), []byte(`{"id":"x","cmd":"nav`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Drain(dir); len(got) != 0 {
		t.Fatalf("Drain = %+v, want empty — a *.tmp is invisible to the glob", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "steer-halfway.tmp")); err != nil {
		t.Errorf("Drain removed a temp file it does not own: %v", err)
	}
}

func TestDrainDropsOversizeUnparsableAndIDLess(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	big := `{"id":"big-1","cmd":"navigate","file":"` + strings.Repeat("x", MaxCommandBytes) + `"}`
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("cmd-1-1.json", big)
	write("cmd-2-1.json", "{not json")
	write("cmd-3-1.json", `{"cmd":"navigate"}`) // no id: nothing could be replied to
	write("cmd-4-1.json", `{"id":"4-1","cmd":"navigate","file":"a.go"}`)
	got := Drain(dir)
	if len(got) != 1 || got[0].ID != "4-1" {
		t.Fatalf("Drain = %+v, want only the well-formed 4-1", got)
	}
	left, _ := os.ReadDir(dir)
	if len(left) != 0 {
		t.Errorf("Drain left %d files behind; every cmd-*.json it looked at must be unlinked", len(left))
	}
}

func TestReplyRoundTripAndAwaitTimeout(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := PostReply(dir, Reply{ID: "abc-1", OK: true, Detail: "opened src/x.go:18"}); err != nil {
		t.Fatalf("PostReply: %v", err)
	}
	r, ok := AwaitReply(dir, "abc-1", time.Second)
	if !ok || !r.OK || r.Detail != "opened src/x.go:18" {
		t.Fatalf("AwaitReply = %+v ok=%v, want the posted reply", r, ok)
	}
	if _, err := os.Stat(filepath.Join(dir, "reply-abc-1.json")); !os.IsNotExist(err) {
		t.Errorf("AwaitReply must delete the reply it read (err = %v)", err)
	}
	start := time.Now()
	if _, ok := AwaitReply(dir, "missing-1", 120*time.Millisecond); ok {
		t.Error("AwaitReply on a missing reply must report ok=false")
	}
	if d := time.Since(start); d < 100*time.Millisecond {
		t.Errorf("AwaitReply returned after %v, want it to poll until the timeout", d)
	}
}

func TestDiscardSweepsCommandsAndRepliesOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, n := range []string{"cmd-1-1.json", "cmd-2-1.json", "reply-1-1.json"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(`{"id":"x"}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, TUIPresence), []byte(`{"pid":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	Discard(dir)
	left, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 || left[0].Name() != TUIPresence {
		names := make([]string, len(left))
		for i, e := range left {
			names[i] = e.Name()
		}
		t.Fatalf("after Discard: %v, want only %s — presence files are not leftovers", names, TUIPresence)
	}
}

func TestCommandJSONOmitsEmptyFields(t *testing.T) {
	t.Parallel()
	data, err := json.Marshal(Command{ID: "1-1", Cmd: "reload", Sources: []string{"notes"}})
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	want := `{"id":"1-1","cmd":"reload","sources":["notes"]}`
	if got != want {
		t.Errorf("Command JSON = %s, want %s (an agent reads these files)", got, want)
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && go test ./internal/steer/ 2>&1 | head -20
```

Expected: `no Go files in .../internal/steer` (the package does not exist yet).

- [ ] **Step 3: Write `internal/steer/steer.go`**

Create `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/steer/steer.go`:

```go
// Package steer carries gg's live-steering protocol: the on-disk inbox a
// `gg session …` command uses to reach a running TUI or `gg web` page.
//
// The channel is deliberately a directory of small JSON files, not a loopback
// control server: no port to discover, no handshake, and a crashed session
// leaves nothing but files the next session sweeps. One inbox per WORKTREE
// (the caller computes the path — this package never touches the environment)
// holds three kinds of file:
//
//	tui.json / web.json     presence; liveness is a FRESH MTIME, not a pid probe
//	cmd-<id>.json           one command, written temp+rename, unlinked by the consumer
//	reply-<id>.json         the consumer's answer, unlinked by the CLI that read it
//
// Every exported function takes the inbox dir explicitly, which is also the
// test seam: a test passes t.TempDir() and needs no environment variable.
package steer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// TUIPresence and WebPresence name the two presence files in an inbox.
	TUIPresence = "tui.json"
	WebPresence = "web.json"

	// MaxCommandBytes caps one command file. A larger file is deleted unread:
	// nothing legitimate approaches 16 KiB, and parsing an unbounded file a
	// stranger dropped in the state dir is not worth the risk.
	MaxCommandBytes = 16 << 10

	// LiveWindow is how fresh a presence file's mtime must be to count as a
	// live session. Sessions re-touch theirs on a 1 s tick, so five seconds is
	// four missed ticks of slack.
	LiveWindow = 5 * time.Second

	cmdPrefix   = "cmd-"
	replyPrefix = "reply-"
	jsonSuffix  = ".json"
	tmpPattern  = "steer-*.tmp" // never matches cmd-*.json / reply-*.json
)

// Target names where a file lives: the working tree (unstaged/untracked), the
// index (staged), or one commit. Values are protocol strings, always English.
type Target struct {
	State  string `json:"state,omitempty"`  // "unstaged" | "staged" | "untracked" | "commit"
	Commit string `json:"commit,omitempty"` // full 40-hex sha when State == "commit"
}

// Line is a landing point in a diff: a 1-based number on one of its two sides.
type Line struct {
	Side string `json:"side"` // "new" | "old"
	No   int    `json:"no"`
}

// Command is one steering command. The consumer sees only file + target +
// side + line — the CLI has already resolved --hunk N into a line number, so
// there is exactly one landing path.
type Command struct {
	ID      string   `json:"id"`
	Cmd     string   `json:"cmd"`               // "navigate" | "reload" | "focus" | "highlight" | "highlight_clear"
	File    string   `json:"file,omitempty"`    // repo-relative, git slash form
	Target  *Target  `json:"target,omitempty"`
	Commit  string   `json:"commit,omitempty"`  // navigate: reveal this commit, no file
	Line    *Line    `json:"line,omitempty"`
	Step    string   `json:"step,omitempty"`    // "next_note" | "prev_note"
	Sources []string `json:"sources,omitempty"` // reload: "notes" | "status" | "all"
	Panel   string   `json:"panel,omitempty"`   // focus: a protocol panel name
	Side    string   `json:"side,omitempty"`    // highlight: "new" | "old"
	Start   int      `json:"start,omitempty"`
	End     int      `json:"end,omitempty"`
	Tone    string   `json:"tone,omitempty"` // "info" | "warn" | "error"
	Wait    bool     `json:"wait,omitempty"`
}

// Reply is the consumer's answer to one command. Detail and Error are English
// protocol prose an agent parses — never translated.
type Reply struct {
	ID     string `json:"id"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Presence records a live session. PID is display-only: liveness is decided by
// the file's mtime, which costs one stat, needs no per-OS process API and
// cannot be fooled by pid reuse.
type Presence struct {
	PID      int    `json:"pid"`
	Worktree string `json:"worktree"`
	Started  string `json:"started,omitempty"` // RFC3339
	URL      string `json:"url,omitempty"`     // web.json only
}

// NewID returns a command id that sorts by post time: <unixnano>-<pid>.
func NewID() string {
	return strconv.FormatInt(time.Now().UnixNano(), 10) + "-" + strconv.Itoa(os.Getpid())
}

// Post writes c into dir's inbox and returns its id, filling in a generated one
// when c.ID is empty. The write is temp+rename, so a consumer draining the dir
// mid-write sees either nothing or the whole file.
func Post(dir string, c Command) (string, error) {
	if c.ID == "" {
		c.ID = NewID()
	}
	data, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	if err := writeAtomic(dir, cmdPrefix+c.ID+jsonSuffix, data); err != nil {
		return "", err
	}
	return c.ID, nil
}

// Drain returns every well-formed command in dir, in name (= post) order, and
// unlinks every cmd-*.json it looked at. A file that is oversize, unparsable or
// carries no id is deleted and dropped: a reply file is named after the id, so
// an id-less command is unanswerable by construction.
func Drain(dir string) []Command {
	names := listPrefixed(dir, cmdPrefix)
	out := make([]Command, 0, len(names))
	for _, n := range names {
		p := filepath.Join(dir, n)
		st, err := os.Stat(p)
		if err != nil || st.Size() > MaxCommandBytes {
			os.Remove(p)
			continue
		}
		data, err := os.ReadFile(p)
		os.Remove(p)
		if err != nil {
			continue
		}
		var c Command
		if json.Unmarshal(data, &c) != nil || c.ID == "" {
			continue
		}
		out = append(out, c)
	}
	return out
}

// PostReply writes r as reply-<id>.json. A command posted with Wait false gets
// no reply — nothing would ever read it.
func PostReply(dir string, r Reply) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return writeAtomic(dir, replyPrefix+r.ID+jsonSuffix, data)
}

// AwaitReply polls for reply-<id>.json every 25 ms until timeout, deleting the
// file it read. ok is false when the timeout expired with no answer (the
// command is still in the inbox and may yet be applied) or the file was
// unreadable.
func AwaitReply(dir, id string, timeout time.Duration) (Reply, bool) {
	p := filepath.Join(dir, replyPrefix+id+jsonSuffix)
	deadline := time.Now().Add(timeout)
	for {
		if data, err := os.ReadFile(p); err == nil {
			os.Remove(p)
			var r Reply
			if json.Unmarshal(data, &r) != nil {
				return Reply{}, false
			}
			return r, true
		}
		if !time.Now().Before(deadline) {
			return Reply{}, false
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// Discard removes every command and reply file in dir. A session sweeps its
// inbox at startup so a crashed session's commands never replay, and a reply
// the CLI abandoned on timeout never outlives the session that wrote it.
// Presence files are NOT leftovers and are left alone.
func Discard(dir string) {
	for _, pfx := range []string{cmdPrefix, replyPrefix} {
		for _, n := range listPrefixed(dir, pfx) {
			os.Remove(filepath.Join(dir, n))
		}
	}
}

// listPrefixed returns the sorted names of dir's <prefix>*.json files.
func listPrefixed(dir, prefix string) []string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(ents))
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || !strings.HasPrefix(n, prefix) || !strings.HasSuffix(n, jsonSuffix) {
			continue
		}
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// writeAtomic creates dir if needed and lands data at dir/name via a temp file
// and a rename, so no reader ever sees a partial file.
func writeAtomic(dir, name string, data []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, tmpPattern)
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, filepath.Join(dir, name)); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
```

- [ ] **Step 4: Run the command tests and watch them pass**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && go test ./internal/steer/ -run 'TestPost|TestDrain|TestReply|TestDiscard|TestCommandJSON' -v 2>&1 | tail -30
```

Expected: all seven PASS.

- [ ] **Step 5: Write the failing presence test**

Create `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/steer/presence_test.go`:

```go
package steer

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTouchThenLive(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "inbox")
	want := Presence{PID: 4242, Worktree: "/abs/wt", Started: "2026-09-10T09:00:00Z"}
	if err := Touch(dir, TUIPresence, want); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	got, ok := Live(dir, TUIPresence)
	if !ok {
		t.Fatal("Live = false right after Touch")
	}
	if got.PID != want.PID || got.Worktree != want.Worktree || got.Started != want.Started {
		t.Errorf("Live = %+v, want %+v", got, want)
	}
}

func TestTouchRefreshesMtimeWithoutRewriting(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := Touch(dir, TUIPresence, Presence{PID: 1, Worktree: "/a"}); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, TUIPresence)
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	// The re-touch carries a DIFFERENT payload; the file on disk keeps the
	// original because a live session only refreshes the clock.
	if err := Touch(dir, TUIPresence, Presence{PID: 2, Worktree: "/b"}); err != nil {
		t.Fatalf("re-Touch: %v", err)
	}
	got, ok := Live(dir, TUIPresence)
	if !ok {
		t.Fatal("Live = false after a re-Touch")
	}
	if got.PID != 1 {
		t.Errorf("PID = %d, want the original 1 — a re-touch is os.Chtimes, not a rewrite", got.PID)
	}
}

func TestTouchRecreatesAMissingPresence(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// A CLI Live check swept the file while this session stalled past the
	// window; the next tick must bring it back, not fail on os.Chtimes.
	if err := Touch(dir, TUIPresence, Presence{PID: 9, Worktree: "/w"}); err != nil {
		t.Fatalf("Touch on an empty dir: %v", err)
	}
	os.Remove(filepath.Join(dir, TUIPresence))
	if err := Touch(dir, TUIPresence, Presence{PID: 9, Worktree: "/w"}); err != nil {
		t.Fatalf("Touch after the file vanished: %v", err)
	}
	if _, ok := Live(dir, TUIPresence); !ok {
		t.Error("Live = false: Touch must recreate a presence that is gone")
	}
}

func TestLiveIsFalseAndSweepsAStalePresence(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := Touch(dir, WebPresence, Presence{PID: 5, Worktree: "/w", URL: "http://127.0.0.1:7777"}); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, WebPresence)
	old := time.Now().Add(-LiveWindow - time.Second)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	if got, ok := Live(dir, WebPresence); ok {
		t.Fatalf("Live = %+v true, want false for an mtime older than %v", got, LiveWindow)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("a stale presence must be swept by the Live check that finds it (err = %v)", err)
	}
}

func TestLiveOnAMissingDirAndRemove(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, ok := Live(filepath.Join(dir, "nope"), TUIPresence); ok {
		t.Error("Live on a missing dir must be false, not a panic")
	}
	if err := Touch(dir, TUIPresence, Presence{PID: 3, Worktree: "/w"}); err != nil {
		t.Fatal(err)
	}
	Remove(dir, TUIPresence)
	if _, ok := Live(dir, TUIPresence); ok {
		t.Error("Live = true after Remove")
	}
	Remove(dir, TUIPresence) // a second Remove is a silent no-op
}
```

- [ ] **Step 6: Run it and watch it fail**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && go test ./internal/steer/ -run TestTouch 2>&1 | head -10
```

Expected: `undefined: Touch`.

- [ ] **Step 7: Write `internal/steer/presence.go`**

Create `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/steer/presence.go`:

```go
package steer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Touch records (or refreshes) a live session's presence. The common case —
// the session's 1 s tick — is a single os.Chtimes on an existing file: liveness
// is the mtime, so re-marshalling the same payload every second would be pure
// waste. The file is (re)written only when it is missing, which also covers the
// case where a CLI Live check swept it while this session stalled past the
// window.
func Touch(dir, name string, p Presence) error {
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); err == nil {
		now := time.Now()
		if err := os.Chtimes(path, now, now); err == nil {
			return nil
		}
		// Fall through: the file exists but its clock could not be set (a
		// read-only remount, a racing sweep) — rewrite it instead.
	}
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return writeAtomic(dir, name, data)
}

// Live reports the presence recorded in dir/name when its mtime is under
// LiveWindow old. A presence older than that belonged to a session that
// crashed or was SIGKILLed; Live removes it, so the next caller does not even
// stat it.
func Live(dir, name string) (Presence, bool) {
	path := filepath.Join(dir, name)
	st, err := os.Stat(path)
	if err != nil {
		return Presence{}, false
	}
	if time.Since(st.ModTime()) > LiveWindow {
		os.Remove(path)
		return Presence{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Presence{}, false
	}
	var p Presence
	if json.Unmarshal(data, &p) != nil {
		return Presence{}, false
	}
	return p, true
}

// Remove deletes a presence file. A session calls it on clean exit and on a
// repo switch; a second call is a silent no-op.
func Remove(dir, name string) { os.Remove(filepath.Join(dir, name)) }
```

- [ ] **Step 8: Run the presence tests and watch them pass**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && go test ./internal/steer/ -run 'TestTouch|TestLive' -v 2>&1 | tail -20
```

Expected: all five PASS.

- [ ] **Step 9: Write the failing watch test**

Create `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/steer/watch_test.go`:

```go
package steer

import (
	"path/filepath"
	"testing"
	"time"
)

func TestWatchWakesOnAPostedCommand(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "inbox")
	w, err := Watch(dir) // must create the dir it watches
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer w.Close()
	if _, err := Post(dir, Command{Cmd: "reload", Sources: []string{"notes"}}); err != nil {
		t.Fatalf("Post: %v", err)
	}
	select {
	case <-w.Events():
	case <-time.After(3 * time.Second):
		t.Fatal("no wake within 3s of a posted command")
	}
}

func TestWatchIgnoresPresenceTouches(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "inbox")
	w, err := Watch(dir)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer w.Close()
	// A presence re-touch happens every second in every session sharing this
	// inbox; waking the consumer for it would defeat the whole point.
	if err := Touch(dir, WebPresence, Presence{PID: 1, Worktree: "/w"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-w.Events():
		t.Fatal("a presence write must not wake the consumer")
	case <-time.After(300 * time.Millisecond):
	}
}

func TestWatchCloseIsIdempotentAndClosesEvents(t *testing.T) {
	t.Parallel()
	w, err := Watch(t.TempDir())
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	w.Close()
	w.Close() // a second Close must not panic on a closed channel
	select {
	case _, open := <-w.Events():
		if open {
			t.Error("Events delivered a value after Close")
		}
	case <-time.After(time.Second):
		t.Error("Events was not closed by Close")
	}
}
```

- [ ] **Step 10: Run it and watch it fail**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && go test ./internal/steer/ -run TestWatch 2>&1 | head -10
```

Expected: `undefined: Watch`.

- [ ] **Step 11: Write `internal/steer/watch.go`**

Create `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/steer/watch.go`:

```go
package steer

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// Watcher wakes a consumer when a command file lands in its inbox. It is the
// FAST path only — every consumer also polls the dir on its own tick, so a
// Watch that fails (no inotify watches left, an exotic filesystem) degrades to
// a one-second latency rather than to a broken feature.
type Watcher struct {
	fsw  *fsnotify.Watcher
	out  chan struct{}
	done chan struct{}

	mu     sync.Mutex
	closed bool
}

// Watch creates dir if needed and starts watching it for command files.
func Watch(dir string) (*Watcher, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := fsw.Add(dir); err != nil {
		fsw.Close()
		return nil, err
	}
	w := &Watcher{fsw: fsw, out: make(chan struct{}, 1), done: make(chan struct{})}
	go w.loop()
	return w, nil
}

// Events delivers coalesced wakes: one buffered slot, because "there is work in
// the inbox" is not a countable fact — the consumer drains everything it finds.
// The channel is closed by Close.
func (w *Watcher) Events() <-chan struct{} { return w.out }

func (w *Watcher) loop() {
	for {
		select {
		case <-w.done:
			return
		case ev, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			if ev.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename) == 0 {
				continue
			}
			// Presence files are re-touched once a second by every session on
			// this inbox; only a command is worth a wake.
			if !strings.HasPrefix(filepath.Base(ev.Name), cmdPrefix) {
				continue
			}
			w.wake()
		case _, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
		}
	}
}

// wake delivers one notification, dropping it when a wake is already pending.
// It takes the mutex so it can never send on a channel Close has closed.
func (w *Watcher) wake() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	select {
	case w.out <- struct{}{}:
	default:
	}
}

// Close stops the watcher and closes Events. Calling it twice is a no-op.
func (w *Watcher) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	w.closed = true
	close(w.done)
	w.fsw.Close()
	close(w.out)
}
```

- [ ] **Step 12: Run the whole package and watch it pass**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && go test ./internal/steer/... -v 2>&1 | tail -40
```

Expected: every test PASS.

- [ ] **Step 13: Add the archtest rows**

In `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/archtest/import_guard_test.go`, inside the `TestLayeringDAG` `cases` map, add this row right after the `"syntax":` row:

```go
		"steer":       {"config", "model", "git", "engine", "domain", "tui", "cli", "mcp", "web", "app", "gitwatch", "i18n", "theme"},
```

Then append this test at the end of the same file:

```go
// TestSteerIsAStdlibLeaf pins internal/steer's dependency budget: the steering
// protocol is imported by all four frontends AND by the CLI that talks to them,
// so any gg package it pulled in would become a dependency of everything.
// fsnotify is the one exception (the inbox wake).
func TestSteerIsAStdlibLeaf(t *testing.T) {
	for _, imp := range directImports(t, "github.com/homeend/gigagit/internal/steer") {
		if imp == "github.com/fsnotify/fsnotify" {
			continue
		}
		if strings.Contains(imp, ".") { // a dot in the first path element = a module path
			if first := strings.SplitN(imp, "/", 2)[0]; strings.Contains(first, ".") {
				t.Errorf("internal/steer imports %s — it must stay stdlib + fsnotify only", imp)
			}
		}
	}
}
```

- [ ] **Step 14: Run the arch tests**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && go test ./internal/archtest/ -run 'TestLayeringDAG|TestSteerIsAStdlibLeaf' -v 2>&1 | tail -15
```

Expected: both PASS.

- [ ] **Step 15: Add the CLAUDE.md package row**

In `/mnt/t/others/gigagit.worktrees/feat-live-steering/CLAUDE.md`, in the `### Package map (internal/)` table, insert this row immediately after the `gitwatch` row (keep it to ONE line — per-feature detail belongs in `docs/CLAUDE-details.md`):

```markdown
| `steer`      | Live-steering protocol leaf: a per-worktree file inbox (presence with mtime liveness, temp+rename command/reply files, an fsnotify wake) that `gg session` uses to drive a running TUI or `gg web` page. stdlib + fsnotify only; `tui`/`cli`/`web`/`mcp` import it directly. |
```

- [ ] **Step 16: gofmt and commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && gofmt -l internal/steer internal/archtest
```

Expected: empty output.

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && git add internal/steer/steer.go internal/steer/presence.go internal/steer/watch.go internal/steer/steer_test.go internal/steer/presence_test.go internal/steer/watch_test.go internal/archtest/import_guard_test.go CLAUDE.md && git commit -m "$(cat <<'EOF'
feat(steer): the live-steering file inbox protocol

A stdlib+fsnotify DAG leaf: presence files whose liveness is a fresh mtime
(no pid probe, immune to pid reuse), temp+rename command and reply files,
a startup Discard sweep, and a best-effort fsnotify wake. Every function
takes the inbox dir explicitly, which is also the test seam.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU
EOF
)"
```

---

### Task 2: `config.SessionSteerDir` and the `[ui] agent_steering` key

**Files:**
- Modify: `internal/config/config.go` (the `UIConfig` struct ~l.44-105, `Defaults()`, `overlayUI`, and a new `SessionSteerDir` beside `SessionSnapshotPath` at l.550)
- Modify: `internal/config/template.go` (one `settingDocs` row, after the `theme` row at l.52)
- Modify: `internal/config/config_test.go` (layer test)
- Create: `internal/config/steerdir_test.go`
- Modify: `README.md` (the `## Configuration` section)

**Interfaces:**
- Consumes: `config.EncodeRepoKey(repoPath string) string` (`internal/config/config.go:497`), the unexported `stateHome() string` (`:526`), `config.SessionSnapshotPath(commonDir string) string` (`:550`).
- Produces:
  ```go
  // internal/config/config.go
  func SessionSteerDir(commonDir, worktree string) string
  type UIConfig struct {
      // …existing fields…
      AgentSteering string `toml:"agent_steering"` // "on" (default) | "off"
  }
  func (c UIConfig) SteeringOn() bool // c.AgentSteering != "off"
  ```
  `SessionSteerDir` returns `<state>/gg/sessions/<EncodeRepoKey(commonDir)>/steer/<EncodeRepoKey(worktree)>`, or `""` when either argument is empty or no state root exists (which the consumers read as "steering disabled").

**Ruling recorded here so no executor re-litigates it:** §4.6 writes the key as
`[ui] agent_steering = false`. A plain `bool` cannot express that under gg's
zero-is-unset overlay rule (default `true` ⇒ a file's `false` is
indistinguishable from "key absent" and would be silently ignored), and
`go-toml/v2` hard-errors decoding a TOML bool into a Go string field — a user
who typed the spec's literal could not start gg. The key is therefore a
**string** with values `"on"` / `"off"`, exactly like the shipped `show_graph`
and `diff_cursor` keys, which are strings for this same documented reason
(`internal/config/config.go:78-80`). `agent_steering = "off"` is the way to
turn steering off.

- [ ] **Step 1: Write the failing path test**

Create `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/config/steerdir_test.go`:

```go
package config

import (
	"path/filepath"
	"testing"
)

// The steer inbox is keyed by WORKTREE inside the repo's session dir: the
// snapshot is shared by every worktree of a repo, but a `gg session navigate`
// run in worktree A must reach the TUI showing A.
func TestSessionSteerDirIsPerWorktreeUnderTheSessionDir(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir()) // no t.Parallel: this test sets an env var
	common := filepath.Join(string(filepath.Separator), "repo", ".git")
	a := SessionSteerDir(common, filepath.Join(string(filepath.Separator), "repo"))
	b := SessionSteerDir(common, filepath.Join(string(filepath.Separator), "wt", "feat"))
	if a == "" || b == "" {
		t.Fatalf("SessionSteerDir = %q / %q, want real paths", a, b)
	}
	if a == b {
		t.Fatalf("two worktrees of one repo share the inbox %q", a)
	}
	snapDir := filepath.Dir(SessionSnapshotPath(common))
	for _, got := range []string{a, b} {
		if filepath.Dir(filepath.Dir(got)) != snapDir {
			t.Errorf("SessionSteerDir = %q, want <sessionDir>/steer/<worktree key> under %q", got, snapDir)
		}
		if filepath.Base(filepath.Dir(got)) != "steer" {
			t.Errorf("SessionSteerDir = %q, want its parent named \"steer\"", got)
		}
	}
	if filepath.Base(a) != EncodeRepoKey(filepath.Join(string(filepath.Separator), "repo")) {
		t.Errorf("leaf = %q, want EncodeRepoKey of the worktree", filepath.Base(a))
	}
}

func TestSessionSteerDirEmptyArgs(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if got := SessionSteerDir("", "/repo"); got != "" {
		t.Errorf("no common dir: got %q, want \"\" (steering off)", got)
	}
	if got := SessionSteerDir("/repo/.git", ""); got != "" {
		t.Errorf("no worktree: got %q, want \"\" (steering off)", got)
	}
}

func TestSteeringOnDefaultsToOn(t *testing.T) {
	t.Parallel()
	if !Defaults().UI.SteeringOn() {
		t.Error("agent steering must be ON by default")
	}
	if got := Defaults().UI.AgentSteering; got != "on" {
		t.Errorf("default agent_steering = %q, want \"on\"", got)
	}
	if (UIConfig{AgentSteering: "off"}).SteeringOn() {
		t.Error(`agent_steering = "off" must turn steering off`)
	}
	if !(UIConfig{}).SteeringOn() {
		t.Error("an unset agent_steering must read as on (zero-is-unset)")
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && go test ./internal/config/ -run 'TestSessionSteerDir|TestSteeringOn' 2>&1 | head -10
```

Expected: `undefined: SessionSteerDir`, `c.AgentSteering undefined`.

- [ ] **Step 3: Add the field, the default, the overlay and the path helper**

In `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/config/config.go`, add to `UIConfig` immediately after the `Theme string` field (l.96):

```go
	// AgentSteering governs the live-steering inbox both interactive frontends
	// expose to `gg session …`:
	//   "on"  — the TUI writes its presence and drains commands; `gg web`
	//           writes web.json and serves POST /api/session/steer. THE DEFAULT.
	//   "off" — neither frontend writes a presence or accepts a command, so
	//           `gg session` reports "no gg session for this worktree".
	// A string (not a bool) on purpose, for the same reason as ShowGraph: under
	// the zero-is-unset overlay rule a bool's `false` is indistinguishable from
	// unset, so a repo could never turn steering back on over a global off.
	// Empty = unset; resolved to "on".
	AgentSteering string `toml:"agent_steering"`
```

Add the accessor next to `SyntaxOn` (after l.110):

```go
// SteeringOn reports whether the live-steering inbox is enabled: everything but
// the literal "off" (including the default "on" and any unrecognized value) is
// on. Only the TUI and `gg web` consult it — with steering off they write no
// presence, and `gg session` then finds nothing live.
func (c UIConfig) SteeringOn() bool { return c.AgentSteering != "off" }
```

In `Defaults()`, in the `UI:` literal, add next to the existing `Theme:` entry:

```go
			AgentSteering: "on",
```

In `overlayUI`, add next to the `Theme` clause:

```go
	if src.AgentSteering != "" {
		dst.AgentSteering = src.AgentSteering
	}
```

Add after `SessionSnapshotPath` (l.559):

```go
// SessionSteerDir is the live-steering inbox for ONE worktree of the repo whose
// git common dir is commonDir:
// <state>/gg/sessions/<EncodeRepoKey(commonDir)>/steer/<EncodeRepoKey(worktree)>.
//
// The snapshot one level up is keyed by common dir and shared by every worktree
// of a repo; the inbox must NOT inherit that, because `gg session navigate` run
// in worktree A has to reach the session showing A. "" (steering disabled) when
// either path is empty or no state root exists.
func SessionSteerDir(commonDir, worktree string) string {
	if commonDir == "" || worktree == "" {
		return ""
	}
	root := stateHome()
	if root == "" {
		return ""
	}
	return filepath.Join(root, "gg", "sessions", EncodeRepoKey(commonDir), "steer", EncodeRepoKey(worktree))
}
```

- [ ] **Step 4: Run the new tests and watch them pass**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && go test ./internal/config/ -run 'TestSessionSteerDir|TestSteeringOn' -v 2>&1 | tail -15
```

Expected: three PASS.

- [ ] **Step 5: Add the overlay layer test**

Append to `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/config/config_test.go`:

```go
func TestUIAgentSteeringLayers(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.toml")

	cfg, err := Load(missing, missing)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.AgentSteering != "on" || !cfg.UI.SteeringOn() {
		t.Errorf("default agent_steering = %q, want on", cfg.UI.AgentSteering)
	}

	g := filepath.Join(dir, "global.toml")
	writeFile(t, g, "[ui]\nagent_steering = \"off\"\n")
	cfg, err = Load(g, missing)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.SteeringOn() {
		t.Error("a global off must turn steering off")
	}

	// A repo can turn it back ON over a global off — the whole reason the key
	// is a string and not a bool.
	r := filepath.Join(dir, "repo.toml")
	writeFile(t, r, "[ui]\nagent_steering = \"on\"\n")
	cfg, err = Load(g, r)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.UI.SteeringOn() {
		t.Error("a repo on must beat a global off")
	}

	// An empty value is unset and cannot reset the global.
	writeFile(t, r, "[ui]\nagent_steering = \"\"\n")
	cfg, err = Load(g, r)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.SteeringOn() {
		t.Error("an empty agent_steering must be ignored, leaving the global off")
	}
}
```

- [ ] **Step 6: Add the settingDoc row and run the coverage gate**

In `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/config/template.go`, in `settingDocs`, insert directly after the `{"ui", "theme", …}` row:

```go
	{"ui", "agent_steering", "on", "accept live-steering commands from `gg session` (an AI agent putting your window on the line it just annotated): on (default) or off; with off the TUI and gg web write no session presence and gg session reports no session"},
```

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && go test ./internal/config/... -v -run 'TestSettingDocs|TestUIAgentSteering' 2>&1 | tail -15
```

Expected: `TestSettingDocsCoverAllFields` and `TestUIAgentSteeringLayers` PASS.

- [ ] **Step 7: Document the key in the README**

In `/mnt/t/others/gigagit.worktrees/feat-live-steering/README.md`, find the `## Configuration` section's `[ui]` example block (search for `show_graph`) and add, immediately after the `theme` line:

```toml
agent_steering = "on"   # accept `gg session` steering commands from an AI agent ("off" disables the inbox)
```

- [ ] **Step 8: Run the whole config package**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && go test ./internal/config/... 2>&1 | tail -5
```

Expected: `ok`.

- [ ] **Step 9: gofmt and commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && gofmt -l internal/config
```

Expected: empty output.

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && git add internal/config/config.go internal/config/template.go internal/config/config_test.go internal/config/steerdir_test.go README.md && git commit -m "$(cat <<'EOF'
feat(config): SessionSteerDir and the [ui] agent_steering key

The steering inbox is keyed by WORKTREE inside the repo's session dir, so a
command posted in one worktree reaches the session showing that worktree —
unlike the snapshot one level up, which every worktree shares.

agent_steering is a string ("on"/"off"), not a bool: under the zero-is-unset
overlay rule a bool's false is indistinguishable from unset, so a repo could
never turn steering back on over a global off. Same reason as show_graph.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU
EOF
)"
```

---

### Task 3: three attention theme roles

The band an agent's `highlight` paints needs a colour in every theme before the
TUI can draw it. This task is pure `internal/theme` + `internal/tui/styles.go`;
nothing renders yet.

**Files:**
- Modify: `internal/theme/theme.go` (the `Theme` struct l.38-41, `roles()` l.53-62, `Dark` l.74-84, `Light` l.95-105)
- Modify: `internal/theme/override.go` (the `Override` struct ~l.46-49, the `roleFields` table after the `note_stale` row at l.107)
- Modify: `internal/theme/override_test.go` (extend the round-trip assertions)
- Modify: `internal/tui/styles.go` (the `styles` struct l.19+, `legacy` l.95, `buildStyles` l.109+)
- Create: `internal/tui/attention_style_test.go`

**Interfaces:**
- Consumes: `theme.Theme`, `theme.Override`, `theme.RoleDocs()`, `internal/tui`'s `st() *styles` and `pick(v, fallback string) lipgloss.TerminalColor`.
- Produces:
  ```go
  // internal/theme/theme.go — three new Theme fields (backgrounds)
  AttentionInfo, AttentionWarn, AttentionError string
  // internal/theme/override.go — the matching Override fields
  AttentionInfo  string `toml:"attention_info"`
  AttentionWarn  string `toml:"attention_warn"`
  AttentionError string `toml:"attention_error"`

  // internal/tui/styles.go — three new styles fields
  attnInfo, attnWarn, attnError lipgloss.Style
  // and the resolver every renderer calls:
  func attnStyle(tone string) (lipgloss.Style, bool) // "info"|"warn"|"error"; false for anything else
  ```

**Ruling:** §4.6 writes the roles as `attention.info` / `attention.warn` /
`attention.error`. `roleFields` keys are flat snake_case TOML keys under one
`[themes.<name>]` table (`cursor_row_bg`, `note_user`, `diff_del_cursor_bg`) —
a dotted key would open a sub-table there. The keys are therefore
`attention_info` / `attention_warn` / `attention_error`.

- [ ] **Step 1: Write the failing style test**

Create `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/tui/attention_style_test.go`:

```go
package tui

import (
	"fmt"
	"testing"

	"github.com/homeend/gigagit/internal/theme"
)

func TestAttentionStyleResolvesEveryTone(t *testing.T) {
	t.Parallel()
	for _, tone := range []string{"info", "warn", "error"} {
		if _, ok := attnStyle(tone); !ok {
			t.Errorf("attnStyle(%q) = not ok, want a style", tone)
		}
	}
	if _, ok := attnStyle("shout"); ok {
		t.Error("attnStyle must refuse a tone outside the allowlist")
	}
	if _, ok := attnStyle(""); ok {
		t.Error("attnStyle(\"\") must be refused — the CLI always sends a tone")
	}
}

// The three tones must be visually distinct in every built-in theme, and none
// of them may be the plain frame background (an invisible band is a bug the
// user reports as "highlight does nothing").
func TestAttentionRolesAreDistinctInEveryTheme(t *testing.T) {
	for _, th := range []theme.Theme{theme.Terminal, theme.Dark, theme.Light} {
		th := th
		t.Run(th.Name, func(t *testing.T) {
			s := buildStyles(th)
			// lipgloss.Color is `type Color string` with no String() method
			// (lipgloss@v1.1.0 color.go:45), so a `interface{ String() string }`
			// assertion would panic — fmt.Sprint renders any TerminalColor.
			got := []string{
				fmt.Sprint(s.attnInfo.GetBackground()),
				fmt.Sprint(s.attnWarn.GetBackground()),
				fmt.Sprint(s.attnError.GetBackground()),
			}
			seen := map[string]bool{}
			for i, g := range got {
				if g == "" {
					t.Errorf("tone %d has an empty background in %s — the band would be invisible", i, th.Name)
				}
				if seen[g] {
					t.Errorf("tone %d reuses background %q in %s", i, g, th.Name)
				}
				seen[g] = true
			}
		})
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && go test ./internal/tui/ -run TestAttention 2>&1 | head -10
```

Expected: `undefined: attnStyle`, `s.attnInfo undefined`.

- [ ] **Step 3: Add the three roles to `internal/theme`**

In `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/theme/theme.go`, extend the `// Signals.` block (l.39-41):

```go
	// Signals.
	NoticeHot, NoticeDim, ReviewHot, ReviewDim  string
	NoteUser, NoteAgent, NoteStale, PickerLabel string

	// Attention bands: the background an agent's `gg session highlight` paints
	// over the marked rows on one side of the diff (a band, like the note band
	// — never a box).
	AttentionInfo, AttentionWarn, AttentionError string
```

In `roles()`, append after the `NoteUser…` line:

```go
		t.AttentionInfo, t.AttentionWarn, t.AttentionError,
```

In `var Dark`, after the `NoteUser:` line:

```go
	AttentionInfo: "24", AttentionWarn: "94", AttentionError: "89",
```

In `var Light`, after its `NoteUser:` line:

```go
	AttentionInfo: "#D3E2F2", AttentionWarn: "#F2E8CC", AttentionError: "#EFD3DC",
```

`Terminal` stays all-empty by design (it inherits the terminal scheme); the TUI
supplies the fallback in Step 4.

In `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/theme/override.go`, add to the `Override` struct after `PickerLabel`:

```go
	AttentionInfo  string `toml:"attention_info"`
	AttentionWarn  string `toml:"attention_warn"`
	AttentionError string `toml:"attention_error"`
```

and add three rows to `roleFields` immediately after the `picker_label` row:

```go
	{"attention_info", "agent attention band, info tone (gg session highlight)", func(t *Theme) *string { return &t.AttentionInfo }, func(o *Override) *string { return &o.AttentionInfo }},
	{"attention_warn", "agent attention band, warn tone", func(t *Theme) *string { return &t.AttentionWarn }, func(o *Override) *string { return &o.AttentionWarn }},
	{"attention_error", "agent attention band, error tone", func(t *Theme) *string { return &t.AttentionError }, func(o *Override) *string { return &o.AttentionError }},
```

- [ ] **Step 4: Run the theme gate**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && go test ./internal/theme/... -v 2>&1 | tail -25
```

Expected: `TestRoleFieldsCoverEveryRole` PASSes (it compares `len(roleFields)` with `len(Theme{}.roles())` — both grew by three) and the whole package is green. If it still fails, one of the two edits above is missing a line.

- [ ] **Step 5: Build the three styles in the TUI**

In `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/tui/styles.go`, add to the `styles` struct (next to the `noteFrame*` fields):

```go
	attnInfo  lipgloss.Style
	attnWarn  lipgloss.Style
	attnError lipgloss.Style
```

Add to the `legacy` theme literal, next to its `NoteUser:` entry — the Terminal
theme leaves every role empty and `pick` falls back to `legacy`, so THIS is what
makes an attention band visible under `theme = "terminal"`:

```go
	AttentionInfo: "24", AttentionWarn: "94", AttentionError: "89",
```

Add to `buildStyles`, next to the `s.noteFrame*` assignments:

```go
	s.attnInfo = ns().Background(pick(th.AttentionInfo, legacy.AttentionInfo))
	s.attnWarn = ns().Background(pick(th.AttentionWarn, legacy.AttentionWarn))
	s.attnError = ns().Background(pick(th.AttentionError, legacy.AttentionError))
```

Add the resolver at the end of `styles.go`:

```go
// attnStyle maps a steering command's tone onto the attention band style.
// The tone is an agent-supplied protocol value, so the allowlist lives here:
// anything outside it paints nothing rather than defaulting to a colour the
// agent did not ask for.
func attnStyle(tone string) (lipgloss.Style, bool) {
	s := st()
	switch tone {
	case "info":
		return s.attnInfo, true
	case "warn":
		return s.attnWarn, true
	case "error":
		return s.attnError, true
	}
	return lipgloss.Style{}, false
}
```

- [ ] **Step 6: Run the style tests and watch them pass**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && go test ./internal/tui/ -run 'TestAttention|TestTheme|TestStyles' -v 2>&1 | tail -25
```

Expected: `TestAttentionStyleResolvesEveryTone` and all three `TestAttentionRolesAreDistinctInEveryTheme` subtests PASS.

- [ ] **Step 7: Run the packages this task touched**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && go test ./internal/theme/... ./internal/tui/... ./internal/config/... 2>&1 | tail -10
```

Expected: three `ok` lines. (`internal/config`'s `gg config populate` golden output renders `RoleDocs()`; if a golden test fails, regenerate it exactly as its own failure message instructs — the three new documented keys are expected content.)

- [ ] **Step 8: gofmt and commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && gofmt -l internal/theme internal/tui
```

Expected: empty output.

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && git add internal/theme/theme.go internal/theme/override.go internal/theme/override_test.go internal/tui/styles.go internal/tui/attention_style_test.go && git commit -m "$(cat <<'EOF'
feat(theme): attention_info / attention_warn / attention_error roles

The band `gg session highlight` will paint over marked diff rows, with
dark-cube and light-hex values plus a legacy fallback so the terminal theme
shows something rather than nothing.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU
EOF
)"
```

---

### Task 4: the TUI consumer — presence, drain, refusal rules, reply

After this task a running TUI writes `tui.json` every second, sweeps stale
commands at startup, drains its inbox on both the fsnotify wake and the 1 s
heartbeat, and answers every command. No command DOES anything yet: the
dispatch switch has only its `default`, which replies
`ok:false "unknown command \"navigate\""`. Tasks 5 and 6 add the real cases.

**Files:**
- Create: `internal/tui/steer.go`
- Create: `internal/tui/steer_test.go`
- Modify: `internal/tui/model.go` (Model fields ~l.211-218; `Init()` l.321; the `heartbeatMsg` case l.2409; the `snapshotTargetMsg` case l.454; `reRoot` l.3644 and l.3733)
- Modify: `internal/tui/run.go` (l.51 startup, l.69 exit)

**Interfaces:**
- Consumes: `steer.Touch/Live/Remove/Drain/Discard/PostReply/Watch` and `steer.Command`/`Reply`/`Presence`/`TUIPresence` (Task 1); `config.SessionSteerDir(commonDir, worktree string) string` and `(config.UIConfig).SteeringOn() bool` (Task 2); the existing `m.snapshotCommonDir` / `m.snapshotWorktree` (`internal/tui/model.go:211-217`), `m.opsIdle()` (`avail.go:13`), `m.topLayer()` (`layer_stack.go:43`), `m.cfg`.
- Produces:
  ```go
  // internal/tui/model.go — new Model fields
  steerDir   string          // "" = steering off for this session
  steerGen   int             // bumped on reRoot; stale watcher msgs are dropped
  steerWatch *steer.Watcher  // pointer: survives the Model value copy

  // internal/tui/steer.go
  type steerStartedMsg struct { gen int; w *steer.Watcher }
  type steerWakeMsg    struct { gen int }

  func (m Model) steerActive() bool
  func (m Model) initSteerInbox() Model
  func (m Model) closeSteerInbox() Model
  func (m Model) startSteerCmd(gen int) tea.Cmd
  func steerListenCmd(w *steer.Watcher, gen int) tea.Cmd
  func (m Model) touchSteerPresence()
  func (m Model) drainSteer() (Model, tea.Cmd)
  func (m Model) steerRefusal() string          // "" = the model can act
  func (m Model) applySteer(c steer.Command) (Model, tea.Cmd)
  func (m Model) answerSteer(c steer.Command, r steer.Reply) tea.Cmd
  func steerOK(c steer.Command, detail string) steer.Reply
  func steerFail(c steer.Command, reason string) steer.Reply
  ```
  Tasks 5 and 6 add `case` arms to `applySteer` and consume `answerSteer`,
  `steerOK`, `steerFail` unchanged.

- [ ] **Step 1: Write the failing refusal + drain tests**

Create `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/tui/steer_test.go`:

```go
package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/steer"
)

// steerModel is a Model wired to a real temp repo with its inbox injected —
// the seam that lets every steering test run with t.Parallel() and no
// t.Setenv (which panics in a parallel test).
func steerModel(t *testing.T) (Model, string) {
	t.Helper()
	m := newTestModel(t)
	dir := filepath.Join(t.TempDir(), "steer")
	m.steerDir = dir
	m.cfg = config.Defaults()
	// New() sets loading:true (model.go:301) and opsIdle() is
	// !running && !loading — without this every steering command would be
	// refused with "an operation is running" and no ok:true test could pass.
	m.loading = false
	return m, dir
}

func TestSteerActiveFollowsDirAndConfig(t *testing.T) {
	t.Parallel()
	m, _ := steerModel(t)
	if !m.steerActive() {
		t.Fatal("a resolved dir plus the default config must be active")
	}
	off := m
	off.cfg.UI.AgentSteering = "off"
	if off.steerActive() {
		t.Error(`agent_steering = "off" must disable the consumer`)
	}
	none := m
	none.steerDir = ""
	if none.steerActive() {
		t.Error("an unresolved dir must disable the consumer")
	}
}

func TestInitSteerInboxWritesPresenceAndDiscardsLeftovers(t *testing.T) {
	t.Parallel()
	m, dir := steerModel(t)
	if _, err := steer.Post(dir, steer.Command{Cmd: "focus", Panel: "files"}); err != nil {
		t.Fatal(err)
	}
	m = m.initSteerInbox()
	if got := steer.Drain(dir); len(got) != 0 {
		t.Errorf("startup left %d command(s) behind; a crashed session's inbox must be swept", len(got))
	}
	p, ok := steer.Live(dir, steer.TUIPresence)
	if !ok {
		t.Fatal("no live presence after initSteerInbox")
	}
	if p.PID != os.Getpid() || p.Worktree == "" {
		t.Errorf("presence = %+v, want this pid and a worktree", p)
	}
	if p.Started == "" {
		t.Error("presence must carry an RFC3339 start time")
	}
	if _, err := time.Parse(time.RFC3339, p.Started); err != nil {
		t.Errorf("started = %q, not RFC3339: %v", p.Started, err)
	}
}

func TestInitSteerInboxIsANoOpWhenSteeringIsOff(t *testing.T) {
	t.Parallel()
	m, dir := steerModel(t)
	m.cfg.UI.AgentSteering = "off"
	m = m.initSteerInbox()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("steering off must not even create the inbox dir (err = %v)", err)
	}
}

func TestCloseSteerInboxRemovesPresence(t *testing.T) {
	t.Parallel()
	m, dir := steerModel(t)
	m = m.initSteerInbox()
	m = m.closeSteerInbox()
	if _, ok := steer.Live(dir, steer.TUIPresence); ok {
		t.Error("presence survived closeSteerInbox — a dead session must not look live")
	}
	if m.steerDir != "" || m.steerWatch != nil {
		t.Errorf("closeSteerInbox left steerDir=%q watch=%v", m.steerDir, m.steerWatch)
	}
}

func TestSteerRefusalRules(t *testing.T) {
	t.Parallel()
	base, _ := steerModel(t)
	base.ready = true

	cases := []struct {
		name string
		mut  func(m Model) Model
	}{
		{"op running", func(m Model) Model { m.running = true; return m }},
		{"loading", func(m Model) Model { m.loading = true; return m }},
		{"decision modal", func(m Model) Model { m.modal = &decisionState{}; return m }},
		{"action menu", func(m Model) Model { m.actionMenu = &actionMenu{}; return m }},
		{"panel / filter", func(m Model) Model { m.filterTyping = true; return m }},
		{"panel @ highlight", func(m Model) Model { m.highlightTyping = true; return m }},
		{"search-history dropdown", func(m Model) Model { m.recallOpen = true; return m }},
		{"files-view filter", func(m Model) Model { m.filesView = &contentPopup{typing: true}; return m }},
		{"stash-view filter", func(m Model) Model { m.stashView = &stashView{typing: true}; return m }},
		{"hunk picker", func(m Model) Model { return m.pushLayer(&hunkPicker{}) }},
		{"rebase editor", func(m Model) Model { return m.pushLayer(&irebaseEditor{}) }},
		{"repo switcher", func(m Model) Model { return m.pushLayer(&repoPopup{}) }},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := c.mut(base).steerRefusal(); got == "" {
				t.Errorf("%s must refuse a steering command", c.name)
			}
		})
	}

	if got := base.steerRefusal(); got != "" {
		t.Errorf("an idle model on the panels refused with %q", got)
	}
	// A diff view is POPPED to reach the panels, never a refusal.
	if got := base.pushLayer(&diffView{}).steerRefusal(); got != "" {
		t.Errorf("an open diff must not refuse a command, got %q", got)
	}
}

func TestDrainAnswersAnUnknownCommand(t *testing.T) {
	t.Parallel()
	m, dir := steerModel(t)
	m = m.initSteerInbox()
	id, err := steer.Post(dir, steer.Command{Cmd: "teleport", Wait: true})
	if err != nil {
		t.Fatal(err)
	}
	m, cmd := m.drainSteer()
	runSteerCmd(t, cmd)
	r, ok := steer.AwaitReply(dir, id, time.Second)
	if !ok {
		t.Fatal("no reply for an unknown command")
	}
	if r.OK || !strings.Contains(r.Error, "unknown command") {
		t.Fatalf("reply = %+v, want ok:false naming the unknown command (not a refusal for some other reason)", r)
	}
	_ = m
}

func TestDrainRefusesWhileAnOpRuns(t *testing.T) {
	t.Parallel()
	m, dir := steerModel(t)
	m = m.initSteerInbox()
	m.running = true
	id, err := steer.Post(dir, steer.Command{Cmd: "focus", Panel: "files", Wait: true})
	if err != nil {
		t.Fatal(err)
	}
	m, cmd := m.drainSteer()
	runSteerCmd(t, cmd)
	r, ok := steer.AwaitReply(dir, id, time.Second)
	if !ok {
		t.Fatal("a refusal must still be answered — never leave the CLI hanging")
	}
	if r.OK {
		t.Fatalf("reply = %+v, want ok:false while an op runs", r)
	}
	_ = m
}

func TestDrainWritesNoReplyForANoWaitCommand(t *testing.T) {
	t.Parallel()
	m, dir := steerModel(t)
	m = m.initSteerInbox()
	id, err := steer.Post(dir, steer.Command{Cmd: "teleport"}) // Wait false
	if err != nil {
		t.Fatal(err)
	}
	m, cmd := m.drainSteer()
	runSteerCmd(t, cmd)
	if _, err := os.Stat(filepath.Join(dir, "reply-"+id+".json")); !os.IsNotExist(err) {
		t.Errorf("a no-wait command must get no reply file (err = %v)", err)
	}
	_ = m
}

func TestHeartbeatTouchesPresenceAndDrains(t *testing.T) {
	t.Parallel()
	m, dir := steerModel(t)
	m = m.initSteerInbox()
	p := filepath.Join(dir, steer.TUIPresence)
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	id, err := steer.Post(dir, steer.Command{Cmd: "teleport", Wait: true})
	if err != nil {
		t.Fatal(err)
	}
	tm, cmd := m.Update(heartbeatMsg{})
	if _, ok := tm.(Model); !ok {
		t.Fatalf("Update returned %T", tm)
	}
	runSteerCmd(t, cmd)
	if _, ok := steer.Live(dir, steer.TUIPresence); !ok {
		t.Error("the heartbeat must re-touch the presence file")
	}
	if _, ok := steer.AwaitReply(dir, id, 2*time.Second); !ok {
		t.Error("the heartbeat must drain the inbox (the poll is the watcher's safety net)")
	}
}

// runSteerCmd executes a tea.Cmd tree far enough to run the reply writes,
// which are ordinary off-thread commands.
func runSteerCmd(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		for _, c := range b {
			runSteerCmd(t, c)
		}
	}
}

func TestSteerPresenceJSONShape(t *testing.T) {
	t.Parallel()
	m, dir := steerModel(t)
	m = m.initSteerInbox()
	data, err := os.ReadFile(filepath.Join(dir, steer.TUIPresence))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("presence is not valid JSON: %v", err)
	}
	for _, k := range []string{"pid", "worktree", "started"} {
		if _, ok := raw[k]; !ok {
			t.Errorf("presence missing %q: %s", k, data)
		}
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && go test ./internal/tui/ -run TestSteer 2>&1 | head -20
```

Expected: `m.steerDir undefined`, `undefined: steerModel`'s helpers, etc.

- [ ] **Step 3: Add the Model fields**

In `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/tui/model.go`, immediately after the snapshot fields (l.211-217, `lastSnapshot []byte`), add:

```go
	// Live steering (steer.go). steerDir is "" when steering is off (config,
	// or an unresolved repo). steerWatch is a POINTER so it survives the Model
	// value copy, like modal and popup; steerGen makes a repo switch drop the
	// old inbox's in-flight watcher messages.
	steerDir   string
	steerGen   int
	steerWatch *steer.Watcher
```

Add `"github.com/homeend/gigagit/internal/steer"` to model.go's import block.

- [ ] **Step 4: Write `internal/tui/steer.go`**

Create `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/tui/steer.go`:

```go
package tui

import (
	"os"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/steer"
)

// steerStartedMsg carries a freshly opened inbox watcher back onto the Update
// goroutine; gen drops one that a repo switch superseded.
type steerStartedMsg struct {
	gen int
	w   *steer.Watcher
}

// steerWakeMsg says a command file landed in the inbox.
type steerWakeMsg struct{ gen int }

// steerActive reports whether this session participates in live steering: it
// needs a resolved inbox AND the [ui] agent_steering key left on. With it off
// the TUI writes no presence, so `gg session` reports "no gg session" and
// nothing here ever runs.
func (m Model) steerActive() bool {
	return m.steerDir != "" && m.cfg.UI.SteeringOn()
}

// steerDirFor resolves this session's inbox from the identity the snapshot
// already resolved. "" means steering is off for this session.
func steerDirFor(commonDir, worktree string) string {
	return config.SessionSteerDir(commonDir, worktree)
}

// initSteerInbox claims the inbox for this session: write the presence file,
// then DISCARD every command and reply already in it. Those belong to a
// session that crashed — replaying them would move a window the agent asked
// about minutes or days ago.
func (m Model) initSteerInbox() Model {
	if !m.steerActive() {
		return m
	}
	steer.Discard(m.steerDir)
	_ = steer.Touch(m.steerDir, steer.TUIPresence, steer.Presence{
		PID:      os.Getpid(),
		Worktree: m.snapshotWorktree,
		Started:  time.Now().UTC().Format(time.RFC3339),
	})
	return m
}

// closeSteerInbox ends this session's claim: the presence goes away at once
// (rather than aging out over five seconds) and the watcher is stopped.
// Called on clean exit and on every repo switch.
func (m Model) closeSteerInbox() Model {
	if m.steerDir != "" {
		steer.Remove(m.steerDir, steer.TUIPresence)
	}
	if m.steerWatch != nil {
		m.steerWatch.Close()
		m.steerWatch = nil
	}
	m.steerDir = ""
	return m
}

// startSteerCmd opens the inbox watcher off-thread. A failure is silent: the
// heartbeat poll already drains the inbox once a second, so a missing watcher
// costs latency, not the feature.
func (m Model) startSteerCmd(gen int) tea.Cmd {
	if !m.steerActive() {
		return nil
	}
	dir := m.steerDir
	return func() tea.Msg {
		w, err := steer.Watch(dir)
		if err != nil {
			return nil
		}
		return steerStartedMsg{gen: gen, w: w}
	}
}

// steerListenCmd blocks on the watcher until a command lands, then hands the
// Update goroutine a wake. A closed channel ends the loop by returning nil.
func steerListenCmd(w *steer.Watcher, gen int) tea.Cmd {
	if w == nil {
		return nil
	}
	return func() tea.Msg {
		if _, ok := <-w.Events(); !ok {
			return nil
		}
		return steerWakeMsg{gen: gen}
	}
}

// touchSteerPresence refreshes this session's mtime. Liveness IS the mtime, so
// this one syscall per second is the whole "is a TUI running" protocol.
func (m Model) touchSteerPresence() {
	if !m.steerActive() {
		return
	}
	_ = steer.Touch(m.steerDir, steer.TUIPresence, steer.Presence{
		PID:      os.Getpid(),
		Worktree: m.snapshotWorktree,
		Started:  time.Now().UTC().Format(time.RFC3339),
	})
}

// drainSteer applies every command waiting in the inbox, in post order, and
// batches the replies. It runs on the Update goroutine (so it may touch the
// Model freely); only the reply writes go off-thread.
func (m Model) drainSteer() (Model, tea.Cmd) {
	if !m.steerActive() {
		return m, nil
	}
	cmds := make([]tea.Cmd, 0, 4)
	for _, c := range steer.Drain(m.steerDir) {
		var cmd tea.Cmd
		m, cmd = m.applySteer(c)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if len(cmds) == 0 {
		return m, nil
	}
	return m, tea.Batch(cmds...)
}

// steerRefusal reports, in English protocol prose, why the model cannot act on
// a steering command right now — or "" when it can. Two principles: never
// answer a decision on the user's behalf, and never throw away what the user is
// in the middle of. The layer whitelist is the same idea as paletteReachable's:
// a diff, history, blame or plain content popup is something the pipeline pops
// to reach the panels (a content popup only when its own filter is not being
// typed into); anything else owns the keyboard and its own text state.
func (m Model) steerRefusal() string {
	switch {
	case !m.opsIdle():
		return "an operation is running"
	case m.modal != nil:
		return "a decision is waiting for the user"
	case m.proc != nil:
		return "an interactive process owns the screen"
	case m.actionMenu != nil:
		return "the action menu is open"
	case m.filterTyping, m.highlightTyping, m.recallOpen:
		return "the user is typing"
	case m.filesView != nil && m.filesView.typing:
		return "the user is typing"
	case m.filesPreview != nil && m.filesPreview.typing:
		return "the user is typing"
	case m.stashView != nil && m.stashView.typing:
		return "the user is typing"
	}
	switch l := m.topLayer().(type) {
	case nil, *diffView, *historyView, *blameView:
		return ""
	case *contentPopup:
		// A content popup is poppable — unless its own / filter has focus, in
		// which case popping it would throw away what the user is typing.
		if l.typing {
			return "the user is typing"
		}
		return ""
	}
	return "a window is open that owns the keyboard"
}

// steerOK / steerFail build the two reply shapes. Detail and Error are English
// protocol prose an agent parses — never i18n keys.
func steerOK(c steer.Command, detail string) steer.Reply {
	return steer.Reply{ID: c.ID, OK: true, Detail: detail}
}

func steerFail(c steer.Command, reason string) steer.Reply {
	return steer.Reply{ID: c.ID, OK: false, Error: reason}
}

// answerSteer writes a reply off-thread. A command posted with wait:false gets
// none — nothing would ever read it, and the file would only have to be swept.
func (m Model) answerSteer(c steer.Command, r steer.Reply) tea.Cmd {
	if !c.Wait || m.steerDir == "" {
		return nil
	}
	dir := m.steerDir
	return func() tea.Msg {
		_ = steer.PostReply(dir, r)
		return nil
	}
}

// applySteer runs one command. Refusals are checked once, before dispatch, so
// every verb inherits them.
func (m Model) applySteer(c steer.Command) (Model, tea.Cmd) {
	if why := m.steerRefusal(); why != "" {
		return m, m.answerSteer(c, steerFail(c, why))
	}
	switch c.Cmd {
	default:
		return m, m.answerSteer(c, steerFail(c, "unknown command "+strconv.Quote(c.Cmd)))
	}
}
```

- [ ] **Step 5: Wire the messages into `Update`**

In `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/tui/model.go`, replace the `heartbeatMsg` case (l.2409-2417) with:

```go
	case heartbeatMsg:
		// A single perpetual tick (started in Init): re-render so the busy line's
		// elapsed time advances while an op runs. View only shows it when running,
		// so an idle tick just repaints identical content (bubbletea diffs frames).
		// Also drives the background auto-refresh scheduler, the session snapshot,
		// and live steering — the presence touch (liveness IS the mtime) plus an
		// unconditional inbox poll. The poll is the safety net for a watcher that
		// failed to start; it is NOT gated on gitwatch.Supported, which probes the
		// REPO's filesystem while the inbox lives in the state dir.
		var cmd tea.Cmd
		m, cmd = m.refreshTick(time.Now())
		m = m.maybeWriteSnapshot()
		m.touchSteerPresence()
		var scmd tea.Cmd
		m, scmd = m.drainSteer()
		return m, tea.Batch(cmd, scmd, heartbeatCmd())
```

Add these two cases next to it (anywhere in the same `switch msg := msg.(type)`):

```go
	case steerStartedMsg:
		if msg.gen != m.steerGen || !m.steerActive() {
			msg.w.Close() // superseded by a repo switch, or steering went away
			return m, nil
		}
		m.steerWatch = msg.w
		return m, steerListenCmd(msg.w, msg.gen)

	case steerWakeMsg:
		if msg.gen != m.steerGen {
			return m, nil
		}
		var cmd tea.Cmd
		m, cmd = m.drainSteer()
		return m, tea.Batch(cmd, steerListenCmd(m.steerWatch, msg.gen))
```

Extend `Init()` (l.321) to start the watcher:

```go
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.bootstrapCmd(), loadSearchHistCmd(m.svc), heartbeatCmd(), m.repoHealthCmd(m.noticeGen), m.startSteerCmd(m.steerGen))
}
```

In the `dataLoadedMsg` handler, right after `m.cfg = msg.cfg` (l.986), reconcile
the inbox with the config that just landed — a repo switch (or a config edit
picked up by the settings writer) can turn steering off under a session that
already claimed an inbox:

```go
		if !m.steerActive() {
			m = m.closeSteerInbox() // config says off: drop the presence at once
		}
```

`closeSteerInbox` clears `steerDir` too, so turning `agent_steering` back ON
under a running session takes effect at the next repo switch or restart. That is
deliberate: §4.6 describes the key as a session-level switch, and a half-armed
inbox is worse than a clearly dormant one. Make the same addition at the sibling
`m.cfg = msg.cfg` site (l.1054, the config-reload handler) so an edited config
behaves the same way.

Extend the `snapshotTargetMsg` case (l.454) — the async re-home after a repo
switch — so the inbox follows the new repo:

```go
	case snapshotTargetMsg:
		if msg.svc != m.svc {
			return m, nil // stale: a later repo switch superseded this resolve
		}
		m.snapshotCommonDir = msg.commonDir
		m.snapshotWorktree = msg.worktree
		m.snapshotPath = config.SessionSnapshotPath(msg.commonDir)
		m.lastSnapshot = nil
		// The steer inbox is keyed by WORKTREE under the same session dir, so it
		// re-homes on exactly the same beat.
		m.steerDir = steerDirFor(msg.commonDir, msg.worktree)
		m = m.initSteerInbox()
		return m, m.startSteerCmd(m.steerGen)
```

In `reRoot` (l.3643), next to the snapshot teardown at l.3644, add the inbox
teardown and a generation bump:

```go
	removeSnapshotFile(m.snapshotPath) // the old repo's session ends here
	m = m.closeSteerInbox()            // …and so does its steering inbox
	m.steerGen++                       // drop the old watcher's in-flight msgs
```

(The `m.snapshotPath, m.snapshotCommonDir, … = "", "", "", nil` line at l.3655
already runs; `closeSteerInbox` has cleared `steerDir` alongside it. No change
is needed at l.3733 — `snapshotTargetCmd(m.svc)` is already batched there and
its handler now re-arms steering.)

- [ ] **Step 6: Wire startup and exit in `run.go`**

`run.go` loads the config into a LOCAL `cfg` (l.32-37) and never stores it on the
model — `m.cfg` stays the zero value until the first `dataLoadedMsg`. That would
make `steerActive()` read an empty `AgentSteering` (which resolves to "on") and
write a presence at startup even for a user who turned steering off. So store it
first. Immediately after the `setTheme(th)` line (l.50) and BEFORE
`m = m.initSnapshotTarget()` (l.51), add:

```go
	// The startup path's config, on the model: the live-steering gate below
	// reads it, and dataLoadedMsg overwrites it with the same value moments
	// later. Every pre-load fallback helper (Model.wheelStep and friends)
	// treats a defaults-filled cfg exactly as it treats the zero value.
	m.cfg = cfg
```

then, after `m = m.initSnapshotTarget()` (l.51):

```go
	// The inbox is keyed by worktree under the session dir the snapshot just
	// resolved; the watcher itself starts from Init().
	m.steerDir = steerDirFor(m.snapshotCommonDir, m.snapshotWorktree)
	m = m.initSteerInbox()
```

In the post-`p.Run()` block (l.64-71), add the teardown next to
`removeSnapshotFile(fm.snapshotPath)`. `closeSteerInbox` is a value receiver and
the returned Model is discarded at exit — the point is its side effects
(unlinking `tui.json`, closing the watcher), so assign it back to `fm`, which the
block already holds:

```go
		fm = fm.closeSteerInbox()
```

- [ ] **Step 7: Run the steering tests and watch them pass**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && go test ./internal/tui/ -run 'TestSteer|TestInitSteer|TestCloseSteer|TestDrain|TestHeartbeat' -v 2>&1 | tail -40
```

Expected: every test PASS, including all twelve `TestSteerRefusalRules` subtests.

- [ ] **Step 8: Run the whole TUI package**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && go test ./internal/tui/... 2>&1 | tail -10
```

Expected: `ok`. (`TestMaybeWriteSnapshotOnlyOnChange` and the other snapshot
tests must still pass — `newTestModel` leaves `steerDir` empty, so every
steering hook is inert for them.)

- [ ] **Step 9: gofmt and commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && gofmt -l internal/tui
```

Expected: empty output.

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && git add internal/tui/steer.go internal/tui/steer_test.go internal/tui/model.go internal/tui/run.go && git commit -m "$(cat <<'EOF'
feat(tui): the live-steering consumer — presence, drain, refusal, reply

The TUI claims its worktree's inbox at startup (writing tui.json and
discarding a crashed session's leftovers), re-touches the presence on the
1s heartbeat, and drains on both the fsnotify wake and that same tick —
the poll is the safety net for a watcher that failed to start.

Refusals never trap the user: an op in flight, a decision modal, a process,
the action menu, any typing surface, or a layer outside the poppable
whitelist all answer ok:false with a reason instead of acting. Every verb
still answers "unknown command"; the real cases land next.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU
EOF
)"
```

---

### Task 5: `navigate`, `focus` and `step`

**Files:**
- Create: `internal/tui/steer_nav.go`
- Create: `internal/tui/steer_nav_test.go`
- Modify: `internal/tui/steer.go` (`applySteer`'s switch; a pending expiry in `drainSteer`)
- Modify: `internal/tui/model.go` (a `pendingSteer` field; drains in the `diffMsg` case l.387, the `commitFilesMsg` case l.549, the `srcStatus` arm of `dataAvailableMsg` ~l.1183-1201; clear on `reRoot` next to `pendingGotoTip` at l.3685)
- Modify: `internal/tui/diff_notes.go` (`noteAnchorLine` l.124 → delegates to a new `lineAnchor`)
- Modify: `internal/tui/commit_scope.go` (`gotoCommitByHash` l.335 → delegates to a new `gotoLoadedCommit`)
- Modify: `internal/tui/session_snapshot.go` (add `panelFromProtoName`, the inverse of `panelProtoName` at l.107)

**Interfaces:**
- Consumes: `steer.Command`/`Line`/`Target` (Task 1); `m.steerRefusal()`, `m.answerSteer`, `steerOK`, `steerFail`, `m.applySteer` (Task 4); `(*diffView).setCursorLine(li, body int)` and `alignCursor(mode cursorAlign, body int)` (`diff_cursor.go:90`/`:189`), `m.expandFoldFor(v *diffView, li int, find func() (int, bool)) (Model, int, bool)` (`note_keys.go:304`), `m.diffBodyRows() int` (`diff_view.go:372`), `m.openStatusDiff(f model.FileStatus, staged bool) (tea.Model, tea.Cmd)` (`diff_view.go:432`), `m.openChangedFiles(c model.Commit) (Model, tea.Cmd)` (`files_view.go:69`), `m.openDiffForFileLine(l contentLine) (tea.Model, tea.Cmd)` (`files_view.go:680`), `m.jumpNote(dir int) (Model, bool)` (`note_keys.go:277`), `m.displayIndices(p panel) []int` (`viewstate.go:511`), `m.clearFilteringForFocus() (Model, bool)` (`commit_scope.go:42`), `m.activateTab(p panel) Model` (`model.go:3212`), `m.focusCommitsPanel() Model` (`model.go:3244`), `m.closeFilesView() Model`, `m.closeStashView() Model`, `m.clearLayers() Model` (`layer_stack.go:74`), `commitIsHash(c model.Commit, short string) bool` (`commit_scope.go:956`), `m.commitAtUnified(bi int) (model.Commit, bool)`, `m.reloadSourcesCmd([]sourceKey{srcStatus}, reloadOpts{})` (`source.go:256`).
- Produces:
  ```go
  // internal/tui/model.go
  pendingSteer *pendingSteer // parked navigate, drained by the load it waits on

  // internal/tui/steer_nav.go
  type steerStage int
  const (
      steerStageStatusRetry steerStage = iota
      steerStageFiles
      steerStageDiff
  )
  type pendingSteer struct {
      cmd   steer.Command
      stage steerStage
      tag   string    // steerStageDiff: the m.diffTag this landing belongs to
      hash  string    // steerStageFiles: the commit whose file list is loading
      at    time.Time // parked at; expired by the heartbeat
  }
  const steerPendingTTL = 5 * time.Second

  func (m Model) steerNavigate(c steer.Command) (Model, tea.Cmd)
  func (m Model) steerFocus(c steer.Command) (Model, tea.Cmd)
  func (m Model) steerStep(c steer.Command) (Model, tea.Cmd)
  func (m Model) steerToPanels() Model
  func (m Model) landSteer(v *diffView, c steer.Command) (Model, tea.Cmd)
  func (m Model) failPending(reason string) (Model, tea.Cmd)
  func (m Model) expirePendingSteer(now time.Time) (Model, tea.Cmd)
  func (m Model) drainPendingStatus() (Model, tea.Cmd)
  func (m Model) drainPendingFiles() (Model, tea.Cmd)
  func (m Model) drainPendingDiff(v *diffView) (Model, tea.Cmd)

  // internal/tui/diff_notes.go
  func (v *diffView) lineAnchor(no int, old bool) (int, bool)
  func (v *diffView) lastLineNo(old bool) int

  // internal/tui/commit_scope.go
  func (m Model) gotoLoadedCommit(hash string) (Model, model.Commit, bool)

  // internal/tui/session_snapshot.go
  func panelFromProtoName(s string) (panel, bool)
  ```

**Rulings recorded here:**
1. `step: next_note|prev_note` calls `m.jumpNote(±1)`, not the full `}`/`{`
   switch arm. The arm's second behaviour — arming `fileArmNextNote` so a
   SECOND press steps to the next noted file — is a two-keystroke interaction
   that has no meaning for a one-shot command; a `step` that finds no further
   note in the open diff answers `ok:false "no next note in this diff"`.
2. A clamped landing answers `ok:true` with
   `detail:"opened src/x.go:120; clamped to line 120"` — the spec's "clamped to
   line N" phrase, appended to the normal detail so one parse rule serves both.
3. A parked navigate is bounded by `steerPendingTTL` (5 s) and expired by the
   heartbeat with `ok:false "the view did not load in time"`. Without it a
   command parked on a load that never arrives (the user pressed esc) would
   land on an unrelated later diff.

- [ ] **Step 1: Write the failing anchor + goto extraction tests**

Create `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/tui/steer_nav_test.go`:

```go
package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
	"github.com/homeend/gigagit/internal/textdiff"
)

func TestLineAnchorFindsBothSidesAndFolds(t *testing.T) {
	t.Parallel()
	v := &diffView{lines: []textdiff.Line{
		{Row: textdiff.Row{LeftNo: 1, RightNo: 1}},
		{Fold: 8}, // hides old 2-9 / new 2-9
		{Row: textdiff.Row{LeftNo: 10, RightNo: 10}},
		{Row: textdiff.Row{LeftNo: 0, RightNo: 11}}, // an added line: no old number
	}}
	if li, vis := v.lineAnchor(10, false); li != 2 || !vis {
		t.Errorf("new:10 = (%d,%v), want (2,true)", li, vis)
	}
	if li, vis := v.lineAnchor(10, true); li != 2 || !vis {
		t.Errorf("old:10 = (%d,%v), want (2,true)", li, vis)
	}
	if li, vis := v.lineAnchor(5, false); li != 1 || vis {
		t.Errorf("a folded number must return its FOLD entry, got (%d,%v), want (1,false)", li, vis)
	}
	if li, _ := v.lineAnchor(99, false); li != -1 {
		t.Errorf("a number past the end = %d, want -1", li)
	}
	if li, _ := v.lineAnchor(11, true); li != -1 {
		t.Errorf("new-only line 11 must not resolve on the old side, got %d", li)
	}
	if got := v.lastLineNo(false); got != 11 {
		t.Errorf("lastLineNo(new) = %d, want 11", got)
	}
	if got := v.lastLineNo(true); got != 10 {
		t.Errorf("lastLineNo(old) = %d, want 10", got)
	}
}

func TestPanelFromProtoNameRoundTrips(t *testing.T) {
	t.Parallel()
	for _, p := range []panel{panelBranches, panelWorktrees, panelRemotes, panelFiles, panelStaged, panelCommits, panelTags, panelReflog, panelPreviews} {
		name := panelProtoName(p)
		if name == "" {
			t.Fatalf("panel %v has no protocol name", p)
		}
		got, ok := panelFromProtoName(name)
		if !ok || got != p {
			t.Errorf("panelFromProtoName(%q) = (%v,%v), want (%v,true)", name, got, ok, p)
		}
	}
	if _, ok := panelFromProtoName("nowhere"); ok {
		t.Error("an unknown panel name must be refused")
	}
	if _, ok := panelFromProtoName(""); ok {
		t.Error("an empty panel name must be refused")
	}
}

func TestSteerFocusSwitchesPanelsAndAnswers(t *testing.T) {
	t.Parallel()
	m, dir := steerModel(t)
	m = m.initSteerInbox()
	m.ready = true
	m = m.pushLayer(&diffView{}) // popped to reach the panels
	c := steer.Command{ID: "f-1", Cmd: "focus", Panel: "branches", Wait: true}
	m, cmd := m.applySteer(c)
	runSteerCmd(t, cmd)
	if m.focus != panelBranches {
		t.Errorf("focus = %v, want panelBranches", m.focus)
	}
	if m.topLayer() != nil {
		t.Error("focus must pop the layer stack to reach the panels")
	}
	r, ok := steer.AwaitReply(dir, "f-1", time.Second)
	if !ok || !r.OK {
		t.Fatalf("reply = %+v ok=%v, want ok:true", r, ok)
	}
}

func TestSteerFocusRefusesAnUnknownPanel(t *testing.T) {
	t.Parallel()
	m, dir := steerModel(t)
	m = m.initSteerInbox()
	m.ready = true
	m, cmd := m.applySteer(steer.Command{ID: "f-2", Cmd: "focus", Panel: "nowhere", Wait: true})
	runSteerCmd(t, cmd)
	r, _ := steer.AwaitReply(dir, "f-2", time.Second)
	if r.OK || !strings.Contains(r.Error, "nowhere") {
		t.Fatalf("reply = %+v, want ok:false naming the panel", r)
	}
	_ = m
}

func TestSteerStepWithoutAnOpenDiffIsRefused(t *testing.T) {
	t.Parallel()
	m, dir := steerModel(t)
	m = m.initSteerInbox()
	m.ready = true
	m, cmd := m.applySteer(steer.Command{ID: "s-1", Cmd: "navigate", Step: "next_note", Wait: true})
	runSteerCmd(t, cmd)
	r, _ := steer.AwaitReply(dir, "s-1", time.Second)
	if r.OK {
		t.Fatalf("reply = %+v, want ok:false with no diff open", r)
	}
	_ = m
}

// navRepo is a real repo with one committed 40-line file and one unstaged edit
// on line 18, so the Files panel carries exactly one diffable row and new line
// 18 is a real landing target. It returns the repo's path.
//
// NOTE the suite's two shapes: `newRepo(t)` (load_test.go:18) returns a
// *git.Repo for domain.New; this helper returns a PATH for domain.Open, which
// is the same service either way. `gitRun(t, dir, args...)` is load_test.go:87.
func navRepo(t *testing.T) string {
	t.Helper()
	dir := gittest.BasicRepo(t, "hi\n")
	lines := make([]string, 0, 40)
	for i := 1; i <= 40; i++ {
		lines = append(lines, "line "+strconv.Itoa(i))
	}
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "a.txt")
	gitRun(t, dir, "commit", "-m", "seed")
	lines[17] = "EDITED" // new line 18
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestSteerNavigateLandsTheCursorOnANewSideLine(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	dir := m.steerDir
	c := steer.Command{
		ID: "n-1", Cmd: "navigate", File: "a.txt",
		Target: &steer.Target{State: "unstaged"},
		Line:   &steer.Line{Side: "new", No: 18},
		Wait:   true,
	}
	m, cmd := m.applySteer(c)
	if m.pendingSteer == nil {
		t.Fatal("navigate must park a pendingSteer until the diff loads")
	}
	m = pumpDiff(t, m, cmd)
	v := m.diffLayer()
	if v == nil {
		t.Fatal("no diff view after navigate")
	}
	row, ok := v.cursorRow()
	if !ok {
		t.Fatal("no cursor row after the landing")
	}
	if row.RightNo != 18 {
		t.Errorf("cursor landed on new line %d, want 18", row.RightNo)
	}
	if m.pendingSteer != nil {
		t.Error("the pending steer must be cleared once it lands")
	}
	r, ok := steer.AwaitReply(dir, "n-1", 2*time.Second)
	if !ok || !r.OK || !strings.Contains(r.Detail, "a.txt:18") {
		t.Fatalf("reply = %+v ok=%v, want ok:true detailing a.txt:18", r, ok)
	}
}

func TestSteerNavigateClampsPastEndOfFile(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	dir := m.steerDir
	m, cmd := m.applySteer(steer.Command{
		ID: "n-2", Cmd: "navigate", File: "a.txt",
		Target: &steer.Target{State: "unstaged"},
		Line:   &steer.Line{Side: "new", No: 9999},
		Wait:   true,
	})
	m = pumpDiff(t, m, cmd)
	r, ok := steer.AwaitReply(dir, "n-2", 2*time.Second)
	if !ok || !r.OK {
		t.Fatalf("reply = %+v ok=%v, want ok:true — a too-large number clamps", r, ok)
	}
	if !strings.Contains(r.Detail, "clamped to line") {
		t.Errorf("detail = %q, want it to say the landing was clamped", r.Detail)
	}
	_ = m
}

func TestSteerNavigateRefusesAPathNotInTheDiff(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	dir := m.steerDir
	m, cmd := m.applySteer(steer.Command{
		ID: "n-3", Cmd: "navigate", File: "nope.txt",
		Target: &steer.Target{State: "unstaged"},
		Line:   &steer.Line{Side: "new", No: 1},
		Wait:   true,
	})
	// The miss triggers ONE status reload and a retry before it gives up.
	m = pumpAll(t, m, cmd)
	r, ok := steer.AwaitReply(dir, "n-3", 3*time.Second)
	if !ok || r.OK {
		t.Fatalf("reply = %+v ok=%v, want ok:false", r, ok)
	}
	if !strings.Contains(r.Error, "nope.txt") {
		t.Errorf("error = %q, want it to name the path", r.Error)
	}
	_ = m
}

func TestSteerNavigateRefusesAConflictedRow(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	dir := m.steerDir
	// Force the row to read as conflicted without building a real merge: the
	// refusal is a property of the row's kind, not of how it got that way.
	for i := range m.status.Files {
		if m.status.Files[i].Path == "a.txt" {
			m.status.Files[i].Kind = model.KindUnmerged
		}
	}
	m, cmd := m.applySteer(steer.Command{
		ID: "n-4", Cmd: "navigate", File: "a.txt",
		Target: &steer.Target{State: "unstaged"},
		Line:   &steer.Line{Side: "new", No: 1},
		Wait:   true,
	})
	runSteerCmd(t, cmd)
	r, _ := steer.AwaitReply(dir, "n-4", time.Second)
	if r.OK || !strings.Contains(r.Error, "conflict") {
		t.Fatalf("reply = %+v, want ok:false mentioning the conflict editor", r)
	}
	_ = m
}

func TestSteerNavigateRefusesAnUnloadedCommit(t *testing.T) {
	t.Parallel()
	m := loadedNavModel(t)
	dir := m.steerDir
	m, cmd := m.applySteer(steer.Command{
		ID: "n-5", Cmd: "navigate", Commit: "0123456789abcdef0123456789abcdef01234567", Wait: true,
	})
	runSteerCmd(t, cmd)
	r, _ := steer.AwaitReply(dir, "n-5", time.Second)
	if r.OK || !strings.Contains(r.Error, "not loaded") {
		t.Fatalf("reply = %+v, want ok:false \"commit not loaded in the feed\" — an agent must never raise the eager-search prompt", r)
	}
	_ = m
}

func TestExpirePendingSteerAnswersAndClears(t *testing.T) {
	t.Parallel()
	m, dir := steerModel(t)
	m = m.initSteerInbox()
	m.pendingSteer = &pendingSteer{
		cmd:   steer.Command{ID: "x-1", Cmd: "navigate", Wait: true},
		stage: steerStageDiff,
		at:    time.Now().Add(-2 * steerPendingTTL),
	}
	m, cmd := m.expirePendingSteer(time.Now())
	runSteerCmd(t, cmd)
	if m.pendingSteer != nil {
		t.Error("an expired pending steer must be cleared")
	}
	r, ok := steer.AwaitReply(dir, "x-1", time.Second)
	if !ok || r.OK {
		t.Fatalf("reply = %+v ok=%v, want ok:false — the CLI must never hang on a load that never arrived", r, ok)
	}
}
```

- [ ] **Step 2: Write the test pumps**

Append to the same file — these drive the async loads the way a real Bubble Tea
program would, without a program:

```go
// loadedNavModel is a Model over navRepo with its status already loaded and its
// steering inbox injected: the starting point for every navigate test.
func loadedNavModel(t *testing.T) Model {
	t.Helper()
	m := New(domain.Open(navRepo(t)))
	m.cfg = config.Defaults()
	m.steerDir = filepath.Join(t.TempDir(), "steer")
	m = m.initSteerInbox()
	m.ready = true
	m.loading = false // New() sets it true; opsIdle() would refuse every command
	m.width, m.height = 200, 60
	st, err := m.svc.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	m = m.withStatus(st)
	return m
}

// pumpDiff runs cmd, feeding every diffMsg (and any batched sibling) back into
// Update, which is where a parked steer lands.
func pumpDiff(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for _, msg := range flattenCmd(t, cmd) {
		tm, next := m.Update(msg)
		m = tm.(Model)
		if next != nil {
			m = pumpDiff(t, m, next)
		}
	}
	return m
}

// pumpAll is pumpDiff with a bounded number of rounds, for chains that go
// through a source reload (status miss → reload → retry).
func pumpAll(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for i := 0; i < 8 && cmd != nil; i++ {
		msgs := flattenCmd(t, cmd)
		cmd = nil
		for _, msg := range msgs {
			tm, next := m.Update(msg)
			m = tm.(Model)
			if next != nil {
				cmd = tea.Batch(cmd, next)
			}
		}
	}
	return m
}

// flattenCmd runs one tea.Cmd tree and returns every non-nil message it
// produced, batches expanded.
func flattenCmd(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if msg == nil {
		return nil
	}
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, flattenCmd(t, c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}
```

This file's imports are: `context`, `os`, `path/filepath`, `strconv`, `strings`,
`testing`, `time`, `tea "github.com/charmbracelet/bubbletea"`, and
`github.com/homeend/gigagit/internal/{config,domain,gittest,model,steer,textdiff}`.
`gitRun(t, dir, args...)` (`internal/tui/load_test.go:87`) and
`gittest.BasicRepo(t, readme)` (`internal/gittest/template.go:85`) are the
suite's existing helpers.

- [ ] **Step 3: Run the tests and watch them fail**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && go test ./internal/tui/ -run 'TestLineAnchor|TestPanelFromProto|TestSteerFocus|TestSteerStep|TestSteerNavigate|TestExpirePending' 2>&1 | head -20
```

Expected: `undefined: lineAnchor`, `undefined: panelFromProtoName`, `m.pendingSteer undefined`.

- [ ] **Step 4: Extract `lineAnchor` from `noteAnchorLine`**

In `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/tui/diff_notes.go`, replace `noteAnchorLine` (l.124) with a two-line delegate plus the extracted core:

```go
// noteAnchorLine finds the logical line a note hangs off: the line carrying
// the END of its range on its side (§4.4 phase-2 hunks anchor at the range
// end). See lineAnchor for the return contract.
func (v *diffView) noteAnchorLine(r domain.ResolvedNote) (int, bool) {
	return v.lineAnchor(r.Range[1], r.Note.Side == model.NoteSideOld)
}

// lineAnchor finds the logical line carrying number no on the old (old=true) or
// new side. visible=false means the number exists in the file but is folded
// away, and the returned index is the FOLD entry that hides it. (-1, false)
// means the number is not in this view at all. Shared by the note anchors and
// by live steering's landing, which knows only a side and a number.
func (v *diffView) lineAnchor(no int, old bool) (int, bool) {
	if no <= 0 {
		return -1, false
	}
	numOf := func(ln textdiff.Line) int {
		if old {
			return ln.Row.LeftNo
		}
		return ln.Row.RightNo
	}
	prev := 0
	for i, ln := range v.lines {
		if ln.Fold > 0 {
			// The hidden run spans (prev, next): find the next real number.
			next := 0
			for j := i + 1; j < len(v.lines); j++ {
				if v.lines[j].Fold == 0 && numOf(v.lines[j]) > 0 {
					next = numOf(v.lines[j])
					break
				}
			}
			if no > prev && (next == 0 || no < next) {
				return i, false
			}
			continue
		}
		if numOf(ln) == no {
			return i, true
		}
		if n := numOf(ln); n > 0 {
			prev = n
		}
	}
	return -1, false
}

// lastLineNo is the highest line number present on one side, or 0 when the side
// has none (a pure addition has no old side). Live steering clamps a landing to
// it rather than refusing an agent whose line number drifted past the end.
func (v *diffView) lastLineNo(old bool) int {
	last := 0
	for _, ln := range v.lines {
		if ln.Fold > 0 {
			continue
		}
		n := ln.Row.RightNo
		if old {
			n = ln.Row.LeftNo
		}
		if n > last {
			last = n
		}
	}
	return last
}
```

Note: `lastLineNo` skips folds, so a file whose tail is folded reports the last
VISIBLE number — which is exactly what the clamp then lands on, and
`expandFoldFor` still handles a fold in the middle.

- [ ] **Step 5: Extract `gotoLoadedCommit` and add `panelFromProtoName`**

In `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/tui/commit_scope.go`, replace `gotoCommitByHash` (l.335) with:

```go
// gotoLoadedCommit moves the Commits cursor to the LOADED row matching hash
// (display-index space; hash compare, not decoration parsing), focuses the
// Commits panel and returns the commit it landed on. It never starts the eager
// deep search — a caller that must not raise a prompt (live steering: never
// raise a decision on an agent's behalf) uses this half directly.
func (m Model) gotoLoadedCommit(hash string) (Model, model.Commit, bool) {
	// Landing in the feed is the point — close a covering stash list up front
	// so BOTH branches (direct hit here, and gotoCommitByHash's eager-search
	// fallback, whose scan/prompt/miss paths never reach it) land visible.
	if m.stashView != nil {
		m = m.closeStashView()
	}
	idx := m.displayIndices(panelCommits)
	for di, bi := range idx {
		if c, ok := m.commitAtUnified(bi); ok && commitIsHash(c, hash) {
			m.sel[panelCommits] = di
			m = m.focusCommitsPanel()
			return m, c, true
		}
	}
	return m, model.Commit{}, false
}

// gotoCommitByHash lands on a loaded row, falling back to the ctrl+f eager
// deep-search — it clears any /-filter ("go to" semantics), pages history under
// the search budget, and prompts before scanning deeper. Shared by the goto-tip
// row (enter / .-menu) and the ctrl+g pendingGotoTip drain in the
// commitsReloadedMsg handler.
func (m Model) gotoCommitByHash(hash string) (Model, tea.Cmd) {
	m, _, ok := m.gotoLoadedCommit(hash)
	if ok {
		return m, nil
	}
	return m.startEagerSearch(hash)
}
```

In `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/tui/session_snapshot.go`, add after `panelProtoName` (l.129):

```go
// panelFromProtoName is panelProtoName's inverse: it decodes the protocol name
// a session snapshot writes and a `gg session focus` command carries. Unknown
// names are refused rather than defaulted — an agent that named a panel gg does
// not have should hear so.
func panelFromProtoName(s string) (panel, bool) {
	for _, p := range []panel{panelBranches, panelWorktrees, panelRemotes, panelFiles, panelStaged, panelCommits, panelTags, panelReflog, panelPreviews} {
		if panelProtoName(p) == s {
			return p, true
		}
	}
	return 0, false
}
```

(The empty string can never match, because `panelProtoName` returns `""` only
for panels outside that list.)

- [ ] **Step 6: Write `internal/tui/steer_nav.go`**

Create `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/tui/steer_nav.go`:

```go
package tui

import (
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// steerStage says which load a parked navigate is waiting on. Each stage is
// drained by the SAME handler that already drains pendingGotoTip's cousin, so
// an async load is never raced.
type steerStage int

const (
	steerStageStatusRetry steerStage = iota // one status reload, then retry the lookup
	steerStageFiles                         // a commit's changed-file list
	steerStageDiff                          // the diff itself; land the cursor
)

// steerPendingTTL bounds a parked navigate. The user can close the view the
// command was waiting for, in which case its load never arrives; without a
// bound the pending would then land on an unrelated later diff.
const steerPendingTTL = 5 * time.Second

// pendingSteer is a navigate command parked until the load it needs arrives.
type pendingSteer struct {
	cmd   steer.Command
	stage steerStage
	tag   string // steerStageDiff: the m.diffTag this landing belongs to
	hash  string // steerStageFiles: the commit whose file list is loading
	at    time.Time
}

// steerToPanels pops everything the refusal whitelist lets it pop, so the
// panel selection the pipeline is about to make is the one the user sees. The
// whitelist has already been enforced by steerRefusal: anything still on the
// stack here is a diff, history, blame view or plain content popup.
func (m Model) steerToPanels() Model {
	if m.filesView != nil {
		m = m.closeFilesView()
	}
	if m.stashView != nil {
		m = m.closeStashView()
	}
	return m.clearLayers()
}

// steerFocus switches to a panel by its protocol name.
func (m Model) steerFocus(c steer.Command) (Model, tea.Cmd) {
	p, ok := panelFromProtoName(c.Panel)
	if !ok {
		return m, m.answerSteer(c, steerFail(c, "unknown panel "+strconv.Quote(c.Panel)))
	}
	m = m.steerToPanels()
	if p == panelCommits {
		m = m.focusCommitsPanel()
	} else {
		m = m.activateTab(p)
	}
	return m, m.answerSteer(c, steerOK(c, "focused "+c.Panel))
}

// steerStep moves the open diff's cursor to the next/previous note. It is
// jumpNote, not the whole }/{ key arm: that arm's second behaviour (arm, then a
// SECOND press steps to the next noted FILE) is a two-keystroke interaction
// with no meaning for a one-shot command.
func (m Model) steerStep(c steer.Command) (Model, tea.Cmd) {
	dir := 1
	if c.Step == "prev_note" {
		dir = -1
	}
	if m.diffLayer() == nil {
		return m, m.answerSteer(c, steerFail(c, "no diff is open"))
	}
	var moved bool
	m, moved = m.jumpNote(dir)
	if !moved {
		if dir > 0 {
			return m, m.answerSteer(c, steerFail(c, "no next note in this diff"))
		}
		return m, m.answerSteer(c, steerFail(c, "no previous note in this diff"))
	}
	return m, m.answerSteer(c, steerOK(c, "stepped to the "+map[bool]string{true: "next", false: "previous"}[dir > 0]+" note"))
}

// steerNavigate is the whole navigate verb. It resolves what it can
// synchronously and parks the rest.
func (m Model) steerNavigate(c steer.Command) (Model, tea.Cmd) {
	switch {
	case c.Step != "":
		return m.steerStep(c)
	case c.File != "" && c.Target != nil && c.Target.State == "commit":
		return m.steerNavigateCommitFile(c)
	case c.File != "":
		return m.steerNavigateStatusFile(c, false)
	case c.Commit != "":
		m2, _, ok := m.steerToPanels().gotoLoadedCommit(c.Commit)
		if !ok {
			return m, m.answerSteer(c, steerFail(c, "commit not loaded in the feed"))
		}
		return m2, m2.answerSteer(c, steerOK(c, "revealed commit "+shortHash(c.Commit)))
	}
	return m, m.answerSteer(c, steerFail(c, "navigate needs a file, a commit or a step"))
}

// steerNavigateStatusFile selects a working-tree/index row by path and opens
// its diff. retried says the one status reload has already happened, so a miss
// this time is final.
func (m Model) steerNavigateStatusFile(c steer.Command, retried bool) (Model, tea.Cmd) {
	staged := c.Target != nil && c.Target.State == "staged"
	p := panelFiles
	if staged {
		p = panelStaged
	}
	m = m.steerToPanels()
	m.focus = p
	// "Go to" semantics, exactly as gotoCommitByHash has them: a /-filter that
	// hides the row would make the landing invisible.
	m, _ = m.clearFilteringForFocus()

	for di, bi := range m.displayIndices(p) {
		f := m.status.Files[bi]
		if f.Path != c.File {
			continue
		}
		if f.Kind == model.KindUnmerged {
			return m, m.answerSteer(c, steerFail(c, c.File+" is conflicted; open the conflict editor yourself"))
		}
		m.sel[p] = di
		tm, cmd := m.openStatusDiff(f, staged)
		m = tm.(Model)
		if c.Line == nil {
			return m, tea.Batch(cmd, m.answerSteer(c, steerOK(c, "opened "+c.File)))
		}
		m.pendingSteer = &pendingSteer{cmd: c, stage: steerStageDiff, tag: m.diffTag, at: time.Now()}
		return m, cmd
	}

	if retried {
		return m, m.answerSteer(c, steerFail(c, c.File+" is not in the working-tree diff"))
	}
	// The agent may have written the file a moment ago and this repo may have
	// no usable file-watch (drvfs): re-read status once before giving up.
	m.pendingSteer = &pendingSteer{cmd: c, stage: steerStageStatusRetry, at: time.Now()}
	var cmd tea.Cmd
	m, cmd = m.reloadSourcesCmd([]sourceKey{srcStatus}, reloadOpts{})
	return m, cmd
}

// steerNavigateCommitFile lands on a loaded commit, opens its changed-file list
// and parks until that list arrives.
func (m Model) steerNavigateCommitFile(c steer.Command) (Model, tea.Cmd) {
	hash := c.Target.Commit
	if hash == "" {
		return m, m.answerSteer(c, steerFail(c, "target.state \"commit\" needs target.commit"))
	}
	m = m.steerToPanels()
	m2, commit, ok := m.gotoLoadedCommit(hash)
	if !ok {
		return m, m.answerSteer(c, steerFail(c, "commit not loaded in the feed"))
	}
	m = m2
	var cmd tea.Cmd
	m, cmd = m.openChangedFiles(commit)
	// Park the FEED's hash: openChangedFiles writes commit.Hash into
	// m.filesHash, and drainPendingFiles gates on m.filesHash == ps.hash. The
	// CLI's value resolved to the same commit but need not be the same string.
	m.pendingSteer = &pendingSteer{cmd: c, stage: steerStageFiles, hash: commit.Hash, at: time.Now()}
	return m, cmd
}

// drainPendingStatus retries the path lookup after the one status reload.
func (m Model) drainPendingStatus() (Model, tea.Cmd) {
	ps := m.pendingSteer
	if ps == nil || ps.stage != steerStageStatusRetry {
		return m, nil
	}
	m.pendingSteer = nil
	return m.steerNavigateStatusFile(ps.cmd, true)
}

// drainPendingFiles selects the commanded path in the freshly loaded file list
// and opens its diff, advancing the pending to the diff stage.
func (m Model) drainPendingFiles() (Model, tea.Cmd) {
	ps := m.pendingSteer
	if ps == nil || ps.stage != steerStageFiles || m.filesView == nil || m.filesHash != ps.hash {
		return m, nil
	}
	c := ps.cmd
	for i, l := range m.filesView.lines {
		if l.heading || l.path != c.File {
			continue
		}
		m.filesView.sel = i
		tm, cmd := m.openDiffForFileLine(l)
		m = tm.(Model)
		if c.Line == nil {
			m.pendingSteer = nil
			return m, tea.Batch(cmd, m.answerSteer(c, steerOK(c, "opened "+c.File+" in "+shortHash(ps.hash))))
		}
		m.pendingSteer = &pendingSteer{cmd: c, stage: steerStageDiff, tag: m.diffTag, at: time.Now()}
		return m, cmd
	}
	return m.failPending(c.File + " is not in commit " + shortHash(ps.hash))
}

// drainPendingDiff lands the cursor once the diff it was parked on has arrived.
// v is the view the diffMsg handler has already filled in.
func (m Model) drainPendingDiff(v *diffView) (Model, tea.Cmd) {
	ps := m.pendingSteer
	if ps == nil || ps.stage != steerStageDiff || ps.tag != m.diffTag {
		return m, nil
	}
	c := ps.cmd
	m.pendingSteer = nil
	if v.err != nil {
		return m, m.answerSteer(c, steerFail(c, "the diff failed to load: "+v.err.Error()))
	}
	return m.landSteer(v, c)
}

// landSteer puts the diff cursor on the commanded line: resolve the anchor
// (clamping past the end of the file rather than refusing), expand a fold that
// hides it, then centre. This is gotoNote's recipe with a {side,no} in place of
// a note id.
func (m Model) landSteer(v *diffView, c steer.Command) (Model, tea.Cmd) {
	old := c.Line.Side == "old"
	no := c.Line.No
	clamped := false
	find := func() (int, bool) { return v.lineAnchor(no, old) }

	li, ok := find()
	if !ok {
		last := v.lastLineNo(old)
		if last == 0 {
			side := "new"
			if old {
				side = "old"
			}
			return m, m.answerSteer(c, steerFail(c, c.File+" has no "+side+" side in this diff"))
		}
		if no > last {
			no, clamped = last, true
			li, ok = find()
		}
		if !ok {
			return m, m.answerSteer(c, steerFail(c, "line "+strconv.Itoa(c.Line.No)+" is not in "+c.File+"'s diff"))
		}
	}
	if m, li, ok = m.expandFoldFor(v, li, find); !ok {
		return m, m.answerSteer(c, steerFail(c, "line "+strconv.Itoa(no)+" could not be revealed"))
	}
	body := m.diffBodyRows()
	v.setCursorLine(li, body)
	v.alignCursor(alignCenter, body)

	detail := "opened " + c.File + ":" + strconv.Itoa(no)
	if clamped {
		detail += "; clamped to line " + strconv.Itoa(no)
	}
	return m, m.answerSteer(c, steerOK(c, detail))
}

// failPending answers and clears whatever is parked. Every failure branch of
// every load the pipeline waits on routes through here — a pending that is
// dropped without an answer leaves the CLI waiting out its two seconds and
// leaves the next unrelated load to land the cursor somewhere random.
func (m Model) failPending(reason string) (Model, tea.Cmd) {
	ps := m.pendingSteer
	if ps == nil {
		return m, nil
	}
	m.pendingSteer = nil
	return m, m.answerSteer(ps.cmd, steerFail(ps.cmd, reason))
}

// expirePendingSteer gives up on a parked command whose load never arrived
// (the user closed the view). Called from the heartbeat.
func (m Model) expirePendingSteer(now time.Time) (Model, tea.Cmd) {
	if m.pendingSteer == nil || now.Sub(m.pendingSteer.at) < steerPendingTTL {
		return m, nil
	}
	return m.failPending("the view did not load in time")
}
```

- [ ] **Step 7: Wire the verb into `applySteer` and the expiry into `drainSteer`**

In `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/tui/steer.go`, change `applySteer`'s switch to:

```go
	switch c.Cmd {
	case "navigate":
		return m.steerNavigate(c)
	case "focus":
		return m.steerFocus(c)
	default:
		return m, m.answerSteer(c, steerFail(c, "unknown command "+strconv.Quote(c.Cmd)))
	}
```

and give `drainSteer` the expiry, as its first act (an expired pending must be
answered before a new command can park over it):

```go
func (m Model) drainSteer() (Model, tea.Cmd) {
	if !m.steerActive() {
		return m, nil
	}
	cmds := make([]tea.Cmd, 0, 4)
	var exp tea.Cmd
	m, exp = m.expirePendingSteer(time.Now())
	if exp != nil {
		cmds = append(cmds, exp)
	}
	for _, c := range steer.Drain(m.steerDir) {
		var cmd tea.Cmd
		m, cmd = m.applySteer(c)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if len(cmds) == 0 {
		return m, nil
	}
	return m, tea.Batch(cmds...)
}
```

Also refuse a NAVIGATE that would park over an already-parked one — add this as
the second check in `applySteer`, right after the refusal. It is scoped to
`navigate` on purpose: `reload`, `focus` and `highlight` park nothing, and the
automatic `reload notes` a concurrent `gg note add` posts (Task 8) must still be
applied while a navigate is loading.

```go
	if c.Cmd == "navigate" && m.pendingSteer != nil {
		return m, m.answerSteer(c, steerFail(c, "a previous navigate is still loading"))
	}
```

- [ ] **Step 8: Add the field and the three drains in `model.go`**

Add the field next to `pendingGotoTip` (l.58):

```go
	pendingSteer *pendingSteer // parked navigate (steer_nav.go); drained by the load it waits on
```

In the `diffMsg` case (l.387), append the drain to the tail — after
`dv.hideAgent = m.notesAgentOff`, replacing the final `return`:

```go
		dv.hideAgent = m.notesAgentOff
		// A parked navigate lands HERE, not at open time: *dv = *msg.view above
		// has just overwritten curLine and offset with the loader's values.
		var scmd tea.Cmd
		m, scmd = m.drainPendingDiff(dv)
		return m, tea.Batch(m.loadNotesCmd(), scmd)
```

In the `commitFilesMsg` case (l.549), add the drain to the success tail and a
failure to the error branch:

```go
		if msg.err != nil {
			m.statusMsg = i18n.T("files: %s", msg.err.Error())
			if len(m.filesView.lines) == 1 && isLoadingPlaceholder(m.filesView.lines[0].text) {
				m.filesView.lines = []contentLine{{text: i18n.T("(load failed)")}}
			}
			return m.failPending("the commit's file list failed to load: " + msg.err.Error())
		}
```

and, replacing the final `return m, nil` of that case:

```go
		m.filesCommit = msg.commit // authoritative: also the follow-live j/k repaint
		return m.drainPendingFiles()
```

In the `srcStatus` arm of `dataAvailableMsg`, at the very end of the arm (after
`m = m.maybeResumePrompt()` at l.1201) add:

```go
			// The one status reload a steering navigate asked for has landed:
			// retry the path lookup against the fresh list.
			if m.pendingSteer != nil && m.pendingSteer.stage == steerStageStatusRetry {
				return m.drainPendingStatus()
			}
```

In `reRoot`, next to the `m.pendingGotoTip = ""` clear (l.3685), add:

```go
	if m.pendingSteer != nil {
		m.pendingSteer = nil // the repo it referred to is gone; its inbox went with it
	}
```

- [ ] **Step 9: Run the navigation tests and watch them pass**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && go test ./internal/tui/ -run 'TestLineAnchor|TestPanelFromProto|TestSteerFocus|TestSteerStep|TestSteerNavigate|TestExpirePending' -v 2>&1 | tail -40
```

Expected: every test PASS.

- [ ] **Step 10: Run the whole TUI package**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && go test ./internal/tui/... 2>&1 | tail -10
```

Expected: `ok`. The note tests exercise `noteAnchorLine`, which now delegates —
if any fails, `lineAnchor`'s body drifted from the original during the extraction.

- [ ] **Step 11: gofmt and commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && gofmt -l internal/tui
```

Expected: empty output.

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && git add internal/tui/steer_nav.go internal/tui/steer_nav_test.go internal/tui/steer.go internal/tui/model.go internal/tui/diff_notes.go internal/tui/commit_scope.go internal/tui/session_snapshot.go && git commit -m "$(cat <<'EOF'
feat(tui): steering navigate, focus and step

navigate selects the row, opens the diff and lands the cursor on {side,no}
via the notes' own recipe — lineAnchor (extracted from noteAnchorLine),
expandFoldFor, setCursorLine, alignCursor. Async loads are never raced: a
pendingSteer is parked and drained by the very handler that owns the load,
the same shape pendingGotoTip uses, and every failure branch answers rather
than leaking the pending onto the next diff.

A commit that is not loaded is refused instead of starting the eager search,
which can raise a prompt — never raise a decision on an agent's behalf.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU
EOF
)"
```

---

### Task 6: `reload`, attention marks, notices and translations

**Files:**
- Create: `internal/tui/steer_attn.go`
- Create: `internal/tui/steer_attn_test.go`
- Modify: `internal/tui/steer.go` (`applySteer`'s switch)
- Modify: `internal/tui/steer_nav.go` (post a notice on a landing and on focus)
- Modify: `internal/tui/model.go` (the `attention` map field + init in `New` l.297; clear on `reRoot`)
- Modify: `internal/tui/diff_render.go` (`diffPaneLines` l.339 — one `else if`)
- Modify: `internal/i18n/lang/ja.toml`, `ko.toml`, `zh.toml`, `ru.toml` (five keys each)

**Interfaces:**
- Consumes: `attnStyle(tone string) (lipgloss.Style, bool)` (Task 3); `steer.Command` (Task 1); `m.answerSteer`, `steerOK`, `steerFail` (Task 4); `m.reloadSourcesCmd([]sourceKey, reloadOpts) (Model, tea.Cmd)` and `m.reloadAllCmd(reloadOpts) (Model, tea.Cmd)` (`source.go:256`/`:297`), `srcNotes`/`srcStatus` (`source.go:26-37`); `cellMark{row bool, base, gut lipgloss.Style}` and `noMark()` / `cursorMark(style string)` (`diff_render.go:19-40`); `model.FileAddress{State, Commit, Path}`; `i18n.T`.
- Produces:
  ```go
  // internal/tui/model.go
  attention map[attentionKey][]steerMark // agent attention bands; nil-safe reads

  // internal/tui/steer_attn.go
  type attentionKey struct{ path, state, commit string }
  type steerMark struct {
      side       string // "new" | "old"
      start, end int    // 1-based inclusive
      tone       string // "info" | "warn" | "error"
  }
  func fileStateProto(s model.FileState) string
  func attnKeyFor(a model.FileAddress) (attentionKey, bool)
  func steerAttnKey(c steer.Command) (attentionKey, bool)
  func (m Model) attnMarkFor(v *diffView, r textdiff.Row) (lipgloss.Style, bool)
  func (m Model) steerHighlight(c steer.Command) (Model, tea.Cmd)
  func (m Model) steerHighlightClear(c steer.Command) (Model, tea.Cmd)
  func (m Model) steerReload(c steer.Command) (Model, tea.Cmd)
  func (m Model) steerNotice(s string) Model
  ```

- [ ] **Step 1: Write the failing attention + reload tests**

Create `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/tui/steer_attn_test.go`:

```go
package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
	"github.com/homeend/gigagit/internal/textdiff"
)

func markCmd(id string, tone string, start, end int) steer.Command {
	return steer.Command{
		ID: id, Cmd: "highlight", File: "a.txt",
		Target: &steer.Target{State: "unstaged"},
		Side:   "new", Start: start, End: end, Tone: tone, Wait: true,
	}
}

func TestSteerHighlightStoresAMarkAndPaintsTheRows(t *testing.T) {
	t.Parallel()
	m, dir := steerModel(t)
	m = m.initSteerInbox()
	m.ready = true

	m, cmd := m.applySteer(markCmd("h-1", "warn", 10, 12))
	runSteerCmd(t, cmd)
	r, ok := steer.AwaitReply(dir, "h-1", time.Second)
	if !ok || !r.OK {
		t.Fatalf("reply = %+v ok=%v, want ok:true", r, ok)
	}

	v := &diffView{noteAddr: model.FileAddress{State: model.StateUnstaged, Path: "a.txt"}}
	for _, tc := range []struct {
		no   int
		want bool
	}{{9, false}, {10, true}, {11, true}, {12, true}, {13, false}} {
		_, got := m.attnMarkFor(v, textdiff.Row{RightNo: tc.no})
		if got != tc.want {
			t.Errorf("new line %d painted = %v, want %v (range is 1-based INCLUSIVE)", tc.no, got, tc.want)
		}
	}
	// A mark on the new side must not paint the old side's numbers.
	if _, got := m.attnMarkFor(v, textdiff.Row{LeftNo: 11}); got {
		t.Error("a new-side mark painted an old-side row")
	}
	// …and not another file's diff.
	other := &diffView{noteAddr: model.FileAddress{State: model.StateUnstaged, Path: "b.txt"}}
	if _, got := m.attnMarkFor(other, textdiff.Row{RightNo: 11}); got {
		t.Error("a mark leaked into another file's diff")
	}
}

func TestSteerHighlightDefaultsAndRefusals(t *testing.T) {
	t.Parallel()
	m, dir := steerModel(t)
	m = m.initSteerInbox()
	m.ready = true

	// end omitted = a single line; side omitted = new.
	one := markCmd("h-2", "info", 5, 0)
	one.Side = ""
	m, cmd := m.applySteer(one)
	runSteerCmd(t, cmd)
	if r, _ := steer.AwaitReply(dir, "h-2", time.Second); !r.OK {
		t.Fatalf("reply = %+v, want ok:true for a one-line mark", r)
	}
	v := &diffView{noteAddr: model.FileAddress{State: model.StateUnstaged, Path: "a.txt"}}
	if _, got := m.attnMarkFor(v, textdiff.Row{RightNo: 5}); !got {
		t.Error("an omitted end must mark exactly the start line on the new side")
	}

	for _, bad := range []struct {
		name string
		cmd  steer.Command
	}{
		{"tone", func() steer.Command { c := markCmd("h-3", "shout", 1, 1); return c }()},
		{"side", func() steer.Command { c := markCmd("h-4", "info", 1, 1); c.Side = "middle"; return c }()},
		{"start", func() steer.Command { c := markCmd("h-5", "info", 0, 3); return c }()},
		{"backwards range", func() steer.Command { c := markCmd("h-6", "info", 9, 4); return c }()},
		{"no file", func() steer.Command { c := markCmd("h-7", "info", 1, 1); c.File = ""; return c }()},
	} {
		bad := bad
		t.Run(bad.name, func(t *testing.T) {
			t.Parallel()
			mm, cmd := m.applySteer(bad.cmd)
			runSteerCmd(t, cmd)
			r, _ := steer.AwaitReply(dir, bad.cmd.ID, time.Second)
			if r.OK {
				t.Errorf("a bad %s was accepted: %+v", bad.name, r)
			}
			_ = mm
		})
	}
}

func TestSteerHighlightClearScopes(t *testing.T) {
	t.Parallel()
	m, dir := steerModel(t)
	m = m.initSteerInbox()
	m.ready = true
	m, c1 := m.applySteer(markCmd("c-1", "info", 1, 2))
	runSteerCmd(t, c1)
	two := markCmd("c-2", "error", 3, 4)
	two.File = "b.txt"
	m, c2 := m.applySteer(two)
	runSteerCmd(t, c2)

	va := &diffView{noteAddr: model.FileAddress{State: model.StateUnstaged, Path: "a.txt"}}
	vb := &diffView{noteAddr: model.FileAddress{State: model.StateUnstaged, Path: "b.txt"}}

	m, c3 := m.applySteer(steer.Command{ID: "c-3", Cmd: "highlight_clear", File: "a.txt", Target: &steer.Target{State: "unstaged"}, Wait: true})
	runSteerCmd(t, c3)
	if _, got := m.attnMarkFor(va, textdiff.Row{RightNo: 1}); got {
		t.Error("clear with a file must drop that file's marks")
	}
	if _, got := m.attnMarkFor(vb, textdiff.Row{RightNo: 3}); !got {
		t.Error("clear with a file must leave other files' marks alone")
	}

	m, c4 := m.applySteer(steer.Command{ID: "c-4", Cmd: "highlight_clear", Wait: true})
	runSteerCmd(t, c4)
	if _, got := m.attnMarkFor(vb, textdiff.Row{RightNo: 3}); got {
		t.Error("clear with no file must drop every mark")
	}
}

func TestSteerReloadSourcesAndMarkLifetime(t *testing.T) {
	t.Parallel()
	m, dir := steerModel(t)
	m = m.initSteerInbox()
	m.ready = true
	m, hc := m.applySteer(markCmd("r-0", "info", 1, 2))
	runSteerCmd(t, hc)

	m, cmd := m.applySteer(steer.Command{ID: "r-1", Cmd: "reload", Sources: []string{"notes"}, Wait: true})
	runSteerCmd(t, cmd)
	r, ok := steer.AwaitReply(dir, "r-1", time.Second)
	if !ok || !r.OK {
		t.Fatalf("reply = %+v ok=%v, want ok:true", r, ok)
	}
	v := &diffView{noteAddr: model.FileAddress{State: model.StateUnstaged, Path: "a.txt"}}
	if _, got := m.attnMarkFor(v, textdiff.Row{RightNo: 1}); got {
		t.Error("an EXPLICIT reload must drop the attention marks (the interval refresh must not)")
	}

	m2, bad := m.applySteer(steer.Command{ID: "r-2", Cmd: "reload", Sources: []string{"weather"}, Wait: true})
	runSteerCmd(t, bad)
	if rr, _ := steer.AwaitReply(dir, "r-2", time.Second); rr.OK || !strings.Contains(rr.Error, "weather") {
		t.Errorf("reply = %+v, want ok:false naming the unknown source", rr)
	}
	_ = m2
}

func TestSteerReloadDefaultsToNotes(t *testing.T) {
	t.Parallel()
	m, dir := steerModel(t)
	m = m.initSteerInbox()
	m.ready = true
	m, cmd := m.applySteer(steer.Command{ID: "r-3", Cmd: "reload", Wait: true})
	runSteerCmd(t, cmd)
	r, _ := steer.AwaitReply(dir, "r-3", time.Second)
	if !r.OK || !strings.Contains(r.Detail, "notes") {
		t.Fatalf("reply = %+v, want ok:true naming notes", r)
	}
	_ = m
}

func TestSteerPostsATransientNotice(t *testing.T) {
	t.Parallel()
	m, _ := steerModel(t)
	m = m.initSteerInbox()
	m.ready = true
	m, cmd := m.applySteer(steer.Command{ID: "n-9", Cmd: "reload", Sources: []string{"notes"}})
	runSteerCmd(t, cmd)
	if m.statusMsg == "" {
		t.Fatal("a reload with no diff open must post a status-bar notice")
	}
	withDiff := m.pushLayer(&diffView{})
	withDiff, cmd2 := withDiff.applySteer(steer.Command{ID: "n-10", Cmd: "reload", Sources: []string{"notes"}})
	runSteerCmd(t, cmd2)
	if withDiff.diffNotice == "" {
		t.Error("with a diff open the notice belongs on the diff notice line")
	}
}

func TestFileStateProtoCoversEveryTargetState(t *testing.T) {
	t.Parallel()
	want := map[model.FileState]string{
		model.StateUnstaged:  "unstaged",
		model.StateStaged:    "staged",
		model.StateUntracked: "untracked",
		model.StateCommitted: "commit",
	}
	for st, s := range want {
		if got := fileStateProto(st); got != s {
			t.Errorf("fileStateProto(%v) = %q, want %q", st, got, s)
		}
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && go test ./internal/tui/ -run 'TestSteerHighlight|TestSteerReload|TestSteerPostsA|TestFileStateProto' 2>&1 | head -15
```

Expected: `undefined: fileStateProto`, `m.attnMarkFor undefined`.

- [ ] **Step 3: Add the Model field**

In `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/tui/model.go`, next to the steering fields added in Task 4:

```go
	// attention holds the bands `gg session highlight` painted, keyed by file
	// address. A map, so it survives the Model value copy. Marks live until
	// highlight_clear, an explicit reload command, or session end — NOT the
	// interval auto-refresh rebuilding a working-tree diff, which would wipe
	// them without any agent action.
	attention map[attentionKey][]steerMark
```

In `New` (l.297), initialize it beside the other maps:

```go
		attention: map[attentionKey][]steerMark{},
```

In `reRoot`, next to the `m.pendingSteer = nil` clear added in Task 5:

```go
	m.attention = map[attentionKey][]steerMark{} // the marks referred to the old repo's files
```

- [ ] **Step 4: Write `internal/tui/steer_attn.go`**

Create `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/tui/steer_attn.go`:

```go
package tui

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
	"github.com/homeend/gigagit/internal/textdiff"
)

// attentionKey addresses one file's marks the way a note address does: the
// path plus the target it lives in, so the same path in the index, the working
// tree and a commit keep separate bands.
type attentionKey struct{ path, state, commit string }

// steerMark is one band: an inclusive 1-based range on one side, in one tone.
type steerMark struct {
	side       string
	start, end int
	tone       string
}

// fileStateProto maps a file state onto the protocol string a steering command
// carries. It is the inverse of the web's noteState allowlist.
func fileStateProto(s model.FileState) string {
	switch s {
	case model.StateStaged:
		return "staged"
	case model.StateUntracked:
		return "untracked"
	case model.StateCommitted:
		return "commit"
	default:
		return "unstaged"
	}
}

// attnKeyFor derives a mark key from an open diff's note address. A view with
// no address (a compare, a shelf file) can carry no marks — it has no stable
// identity to key them by.
func attnKeyFor(a model.FileAddress) (attentionKey, bool) {
	if a.Path == "" {
		return attentionKey{}, false
	}
	return attentionKey{path: a.Path, state: fileStateProto(a.State), commit: a.Commit}, true
}

// steerAttnKey derives a mark key from a command's file + target.
func steerAttnKey(c steer.Command) (attentionKey, bool) {
	if c.File == "" {
		return attentionKey{}, false
	}
	k := attentionKey{path: c.File, state: "unstaged"}
	if c.Target != nil {
		if c.Target.State != "" {
			k.state = c.Target.State
		}
		k.commit = c.Target.Commit
	}
	return k, true
}

// attnMarkFor reports the band style row r wears in view v, if any. The first
// matching mark wins: two overlapping bands from one agent are its own business,
// and blending them would produce a colour it never asked for.
func (m Model) attnMarkFor(v *diffView, r textdiff.Row) (lipgloss.Style, bool) {
	if len(m.attention) == 0 || v == nil {
		return lipgloss.Style{}, false
	}
	k, ok := attnKeyFor(v.noteAddr)
	if !ok {
		return lipgloss.Style{}, false
	}
	for _, mk := range m.attention[k] {
		no := r.RightNo
		if mk.side == "old" {
			no = r.LeftNo
		}
		if no == 0 || no < mk.start || no > mk.end {
			continue
		}
		if s, ok := attnStyle(mk.tone); ok {
			return s, true
		}
	}
	return lipgloss.Style{}, false
}

// steerHighlight records one band. Every field is validated here — the values
// come from an agent, and an unrecognized tone must paint nothing rather than
// default to a colour nobody asked for.
func (m Model) steerHighlight(c steer.Command) (Model, tea.Cmd) {
	k, ok := steerAttnKey(c)
	if !ok {
		return m, m.answerSteer(c, steerFail(c, "highlight needs a file"))
	}
	side := c.Side
	if side == "" {
		side = "new"
	}
	if side != "new" && side != "old" {
		return m, m.answerSteer(c, steerFail(c, "unknown side "+strconv.Quote(c.Side)))
	}
	if _, ok := attnStyle(c.Tone); !ok {
		return m, m.answerSteer(c, steerFail(c, "unknown tone "+strconv.Quote(c.Tone)))
	}
	if c.Start < 1 {
		return m, m.answerSteer(c, steerFail(c, "start must be a 1-based line number"))
	}
	end := c.End
	if end == 0 {
		end = c.Start
	}
	if end < c.Start {
		return m, m.answerSteer(c, steerFail(c, "end is before start"))
	}
	if m.attention == nil {
		m.attention = map[attentionKey][]steerMark{}
	}
	m.attention[k] = append(m.attention[k], steerMark{side: side, start: c.Start, end: end, tone: c.Tone})
	m = m.steerNotice(i18n.T("▸ agent marked lines in %s", c.File))
	return m, m.answerSteer(c, steerOK(c, "marked "+c.File+" "+side+":"+strconv.Itoa(c.Start)+"-"+strconv.Itoa(end)+" as "+c.Tone))
}

// steerHighlightClear drops one file's marks, or every mark when the command
// names no file.
func (m Model) steerHighlightClear(c steer.Command) (Model, tea.Cmd) {
	if c.File == "" {
		m.attention = map[attentionKey][]steerMark{}
		m = m.steerNotice(i18n.T("▸ agent cleared its marks"))
		return m, m.answerSteer(c, steerOK(c, "cleared every mark"))
	}
	k, _ := steerAttnKey(c)
	next := make(map[attentionKey][]steerMark, len(m.attention))
	for key, marks := range m.attention {
		if key != k {
			next[key] = marks
		}
	}
	m.attention = next
	m = m.steerNotice(i18n.T("▸ agent cleared its marks"))
	return m, m.answerSteer(c, steerOK(c, "cleared the marks on "+c.File))
}

// steerReload re-reads the named sources, which re-resolves the open diff's
// notes exactly as the .-menu mutations do. An explicit reload also drops the
// attention marks: the agent is saying "look again", and a band whose range
// drifted under an edit is its own to re-post.
func (m Model) steerReload(c steer.Command) (Model, tea.Cmd) {
	names := c.Sources
	if len(names) == 0 {
		names = []string{"notes"}
	}
	var srcs []sourceKey
	all := false
	for _, n := range names {
		switch n {
		case "notes":
			srcs = append(srcs, srcNotes)
		case "status":
			srcs = append(srcs, srcStatus)
		case "all":
			all = true
		default:
			return m, m.answerSteer(c, steerFail(c, "unknown reload source "+strconv.Quote(n)))
		}
	}
	m.attention = map[attentionKey][]steerMark{}
	var cmd tea.Cmd
	if all {
		m, cmd = m.reloadAllCmd(reloadOpts{manual: true, hardFeed: true})
	} else {
		m, cmd = m.reloadSourcesCmd(srcs, reloadOpts{})
	}
	m = m.steerNotice(i18n.T("▸ agent asked for a reload"))
	return m, tea.Batch(cmd, m.answerSteer(c, steerOK(c, "reloaded "+strings.Join(names, ", "))))
}

// steerNotice posts a transient "an agent did this" line — on the diff view's
// notice line when one is open, otherwise on the status bar. Both clear on the
// next key, like every other notice: the user should see WHY the window moved,
// and then never think about it again.
func (m Model) steerNotice(s string) Model {
	if m.diffLayer() != nil {
		m.diffNotice = s
		return m
	}
	m.statusMsg = s
	return m
}
```

- [ ] **Step 5: Wire the three verbs and the two landing notices**

In `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/tui/steer.go`, extend `applySteer`'s switch:

```go
	switch c.Cmd {
	case "navigate":
		return m.steerNavigate(c)
	case "focus":
		return m.steerFocus(c)
	case "reload":
		return m.steerReload(c)
	case "highlight":
		return m.steerHighlight(c)
	case "highlight_clear":
		return m.steerHighlightClear(c)
	default:
		return m, m.answerSteer(c, steerFail(c, "unknown command "+strconv.Quote(c.Cmd)))
	}
```

In `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/tui/steer_nav.go`, post the
notices — in `steerFocus`, before its `return`:

```go
	m = m.steerNotice(i18n.T("agent moved the focus"))
```

and in `landSteer`, immediately before building `detail`:

```go
	m.diffNotice = i18n.T("▸ agent opened %s", c.File+":"+strconv.Itoa(no))
```

(`landSteer` always runs with a diff open, so it writes `diffNotice` directly
rather than going through `steerNotice`.) Add `"github.com/homeend/gigagit/internal/i18n"`
to `steer_nav.go`'s imports.

- [ ] **Step 6: Paint the band in the diff renderer**

In `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/tui/diff_render.go`, in
`diffPaneLines` (l.339), replace:

```go
		mk := noMark()
		if i >= curStart && i < curEnd {
			mk = cursorMark(style)
		}
		r := dr.row
```

with:

```go
		mk := noMark()
		r := dr.row
		switch {
		case i >= curStart && i < curEnd:
			// The cursor outranks an attention band: the user must always be
			// able to see where they are.
			mk = cursorMark(style)
		default:
			if bg, ok := m.attnMarkFor(v, r); ok {
				mk = cellMark{row: true, base: bg, gut: s.diffGutter}
			}
		}
```

(`s` is the `st()` local already bound at the top of `diffPaneLines`.)

- [ ] **Step 7: Add the five keys to all four bundles**

In each of `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/i18n/lang/{ja,ko,zh,ru}.toml`,
insert the block below under `[strings]`, immediately after the
`"agent note" = …` line (near line 319) — the bundles are NOT key-sorted; insert
in place near the related note keys and never re-sort the file.

`ja.toml`:
```toml
"▸ agent opened %s" = "▸ エージェントが %s を開きました"
"▸ agent asked for a reload" = "▸ エージェントが再読み込みを要求しました"
"▸ agent marked lines in %s" = "▸ エージェントが %s の行にマークを付けました"
"▸ agent cleared its marks" = "▸ エージェントがマークを消去しました"
"agent moved the focus" = "エージェントがフォーカスを移動しました"
```

`ko.toml`:
```toml
"▸ agent opened %s" = "▸ 에이전트가 %s 을(를) 열었습니다"
"▸ agent asked for a reload" = "▸ 에이전트가 새로 고침을 요청했습니다"
"▸ agent marked lines in %s" = "▸ 에이전트가 %s 의 줄을 표시했습니다"
"▸ agent cleared its marks" = "▸ 에이전트가 표시를 지웠습니다"
"agent moved the focus" = "에이전트가 포커스를 이동했습니다"
```

`zh.toml`:
```toml
"▸ agent opened %s" = "▸ 代理已打开 %s"
"▸ agent asked for a reload" = "▸ 代理请求了刷新"
"▸ agent marked lines in %s" = "▸ 代理标记了 %s 中的行"
"▸ agent cleared its marks" = "▸ 代理清除了标记"
"agent moved the focus" = "代理移动了焦点"
```

`ru.toml`:
```toml
"▸ agent opened %s" = "▸ агент открыл %s"
"▸ agent asked for a reload" = "▸ агент запросил обновление"
"▸ agent marked lines in %s" = "▸ агент отметил строки в %s"
"▸ agent cleared its marks" = "▸ агент снял свои отметки"
"agent moved the focus" = "агент переместил фокус"
```

Each translation carries exactly the same format verbs as its key (`%s` once,
or none) — `CheckVerbs` fails otherwise, and a mismatch silently falls back to
English for that one key.

- [ ] **Step 8: Run the i18n gates**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && go test ./internal/i18n/... ./internal/tui/ -run 'TestI18n|TestBundle|TestVerbs|TestRenderOrder|TestOptionsVocab|TestMenuLabels|TestEngineProse' -v 2>&1 | tail -25
```

Expected: every gate PASSes. A failure naming one of the five keys means a
bundle is missing it or its verbs drifted.

- [ ] **Step 9: Run the new tests and watch them pass**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && go test ./internal/tui/ -run 'TestSteerHighlight|TestSteerReload|TestSteerPostsA|TestFileStateProto' -v 2>&1 | tail -35
```

Expected: every test PASS.

- [ ] **Step 10: Run the whole TUI package**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && go test ./internal/tui/... 2>&1 | tail -10
```

Expected: `ok`. `internal/tui/diff_cursor_test.go:544-577` pins the cursor
band's exact shades — the renderer change must not have altered any row that
carries no attention mark.

- [ ] **Step 11: gofmt and commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && gofmt -l internal/tui
```

Expected: empty output.

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && git add internal/tui/steer_attn.go internal/tui/steer_attn_test.go internal/tui/steer.go internal/tui/steer_nav.go internal/tui/model.go internal/tui/diff_render.go internal/i18n/lang/ja.toml internal/i18n/lang/ko.toml internal/i18n/lang/zh.toml internal/i18n/lang/ru.toml && git commit -m "$(cat <<'EOF'
feat(tui): steering reload, attention bands and notices

reload re-reads notes/status/all, which re-resolves the open diff's notes
exactly as the .-menu mutations do. highlight paints a background band over
a range on one side; the cursor row still outranks it. Marks live until
highlight_clear, an EXPLICIT reload, or session end — never wiped by the
interval auto-refresh, which no agent asked for.

Every landing, reload, mark and focus posts a transient translated notice so
the user sees why the window moved; the replies stay English protocol prose.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU
EOF
)"
```

---

### Task 7: `gg session` — status, navigate, reload, focus, highlight

**Files:**
- Create: `internal/cli/session.go`
- Create: `internal/cli/session_test.go`
- Modify: `internal/cli/cli.go` (a `case "session":` in `runOne` l.73+, an entry in the `commands` map l.160)
- Modify: `cmd/gg/main.go` (the hardcoded usage string at l.103)

**Interfaces:**
- Consumes: `steer.Command`/`Target`/`Line`/`Presence`/`Post`/`AwaitReply`/`Live`/`TUIPresence`/`WebPresence` (Task 1); `config.SessionSteerDir` and `config.SessionSnapshotPath` (Task 2 / `internal/config/config.go:550`); `(*domain.Service).GitCommonDir(ctx) (string, error)` (`query.go:574`), `TopLevel(ctx) (string, error)` (`query.go:478`), `NoteTarget(ctx, path string, cached bool, rev string) (model.FileAddress, error)` (`internal/domain/notetarget.go:50`) and `domain.ErrNoteTargetUsage` (`:44`), `RevParse(ctx, rev string) (string, error)` (`internal/domain/query.go:490`), `HunkDiffSpec(ctx, cached bool, rev string, paths []string) (model.DiffSpec, error)` (`internal/domain/hunks.go:54`), `DiffHunks(ctx, spec) ([]model.FileHunks, error)` (`:75`); `model.FileHunks{Path, OldPath, Hunks}` / `model.Hunk{N, Old, New [2]int, Header}` (`internal/model/hunk.go`); `addTargetFlags(fs *flag.FlagSet) noteTargetFlags` (`internal/cli/note.go:79`).
- Produces:
  ```go
  // internal/cli/session.go
  func cmdSession(svc *domain.Service, args []string, stdout, stderr io.Writer) int
  func runSession(dir string, svc *domain.Service, args []string, stdout, stderr io.Writer) int // TEST SEAM
  func sessionInboxDir(svc *domain.Service) (string, error)
  type sessionRoute struct { tui, web steer.Presence; tuiOK, webOK bool }
  func routeFor(dir string) sessionRoute
  func sendSteer(dir string, c steer.Command, noWait bool, stdout, stderr io.Writer) int
  func postWebSteer(base string, c steer.Command) error
  func targetOf(a model.FileAddress) *steer.Target
  func resolveHunkLine(ctx context.Context, svc *domain.Service, cached bool, rev, path string, n int) (steer.Line, error)
  var steerReplyWaitForTest = 2 * time.Second // a var so a test can shorten it
  const steerHTTPTimeout = 2 * time.Second
  ```

**Test seam ruling:** `internal/cli` has no per-run options struct — `cli.Run`
constructs everything from the working directory. `runSession(dir, …)` taking
the inbox as its FIRST parameter is therefore the seam: `cmdSession` resolves
`dir` from `config.SessionSteerDir` and delegates; every CLI test calls
`runSession(t.TempDir(), …)` and so runs with `t.Parallel()` and no `t.Setenv`.
The `cli.Run` → `cmdSession` path (which does read the real state home) is
covered by the e2e scenario in Task 12, which sets `XDG_STATE_HOME` once for the
whole harness process.

- [ ] **Step 1: Write the failing CLI tests**

Create `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/cli/session_test.go`:

```go
package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

// livePresence writes a fresh tui.json so the routing table's "TUI only" row
// applies. Liveness is the mtime, so simply writing the file makes it live.
func livePresence(t *testing.T, dir string) {
	t.Helper()
	if err := steer.Touch(dir, steer.TUIPresence, steer.Presence{PID: 4242, Worktree: "/w", Started: time.Now().UTC().Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
}

// answer runs a fake consumer: it drains one command and replies. Returns the
// command it saw.
func answer(t *testing.T, dir string, reply func(steer.Command) steer.Reply) <-chan steer.Command {
	t.Helper()
	out := make(chan steer.Command, 1)
	go func() {
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			cmds := steer.Drain(dir)
			if len(cmds) == 0 {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			c := cmds[0]
			if c.Wait {
				_ = steer.PostReply(dir, reply(c))
			}
			out <- c
			return
		}
		close(out)
	}()
	return out
}

func TestSessionStatusExitsOneWithNoPresence(t *testing.T) {
	t.Parallel()
	var out, errb bytes.Buffer
	svc := domain.Open(newCLIRepo(t))
	if code := runSession(t.TempDir(), svc, []string{"status"}, &out, &errb); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errb.String(), "no gg session") {
		t.Errorf("stderr = %q, want it to say there is no session", errb.String())
	}
}

func TestSessionStatusPrintsTheLiveTUI(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	var out, errb bytes.Buffer
	svc := domain.Open(newCLIRepo(t))
	if code := runSession(dir, svc, []string{"status"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d (stderr %q), want 0", code, errb.String())
	}
	if !strings.Contains(out.String(), "4242") {
		t.Errorf("stdout = %q, want the TUI pid", out.String())
	}
}

func TestSessionStatusJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	var out, errb bytes.Buffer
	svc := domain.Open(newCLIRepo(t))
	if code := runSession(dir, svc, []string{"status", "--json"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("--json is not valid JSON (%v): %s", err, out.String())
	}
	if _, ok := got["tui"]; !ok {
		t.Errorf("json = %s, want a \"tui\" member", out.String())
	}
}

func TestSessionNavigateNoWaitPrintsTheIDAndExitsZero(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	var out, errb bytes.Buffer
	svc := domain.Open(newCLIRepo(t))
	code := runSession(dir, svc, []string{"navigate", "--file", "a.txt", "--new-line", "3", "--no-wait"}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit = %d (stderr %q), want 0", code, errb.String())
	}
	id := strings.TrimSpace(out.String())
	if id == "" {
		t.Fatal("--no-wait must print the command id")
	}
	got := steer.Drain(dir)
	if len(got) != 1 {
		t.Fatalf("inbox = %+v, want one command", got)
	}
	c := got[0]
	if c.ID != id || c.Cmd != "navigate" || c.File != "a.txt" || c.Wait {
		t.Fatalf("posted %+v, want navigate a.txt with wait:false and id %q", c, id)
	}
	if c.Line == nil || c.Line.Side != "new" || c.Line.No != 3 {
		t.Fatalf("line = %+v, want new:3", c.Line)
	}
	if c.Target == nil || c.Target.State != "unstaged" {
		t.Fatalf("target = %+v, want the unstaged working tree", c.Target)
	}
}

func TestSessionNavigateWaitsForTheReply(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	seen := answer(t, dir, func(c steer.Command) steer.Reply {
		return steer.Reply{ID: c.ID, OK: true, Detail: "opened a.txt:3"}
	})
	var out, errb bytes.Buffer
	svc := domain.Open(newCLIRepo(t))
	code := runSession(dir, svc, []string{"navigate", "--file", "a.txt", "--new-line", "3"}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit = %d (stderr %q), want 0", code, errb.String())
	}
	if !strings.Contains(out.String(), "opened a.txt:3") {
		t.Errorf("stdout = %q, want the reply's detail", out.String())
	}
	c, ok := <-seen
	if !ok || !c.Wait {
		t.Fatalf("consumer saw %+v ok=%v, want wait:true", c, ok)
	}
}

func TestSessionNavigateFailedReplyExitsOne(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	answer(t, dir, func(c steer.Command) steer.Reply {
		return steer.Reply{ID: c.ID, OK: false, Error: "a.txt is not in the working-tree diff"}
	})
	var out, errb bytes.Buffer
	svc := domain.Open(newCLIRepo(t))
	code := runSession(dir, svc, []string{"navigate", "--file", "a.txt", "--new-line", "3"}, &out, &errb)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errb.String(), "not in the working-tree diff") {
		t.Errorf("stderr = %q, want the reply's error", errb.String())
	}
}

// NO t.Parallel(): this is the one test that writes the package-level
// steerReplyWaitForTest, and every other session test reads it. Under
// t.Parallel() that is a data race ./test.sh race would catch.
func TestSessionNavigateTimeoutIsQueuedNotAFailure(t *testing.T) {
	dir := t.TempDir()
	livePresence(t, dir)
	var out, errb bytes.Buffer
	svc := domain.Open(newCLIRepo(t))
	old := steerReplyWaitForTest
	steerReplyWaitForTest = 150 * time.Millisecond
	defer func() { steerReplyWaitForTest = old }()
	code := runSession(dir, svc, []string{"navigate", "--file", "a.txt", "--new-line", "3"}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 — the command is still in the inbox", code)
	}
	if !strings.Contains(out.String(), "queued") {
		t.Errorf("stdout = %q, want a \"queued\" line", out.String())
	}
}

func TestSessionHunkResolvesToTheFirstLineOfTheHunk(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	repo := hunkCLIRepo(t)
	var out, errb bytes.Buffer
	svc := domain.Open(repo)
	code := runSession(dir, svc, []string{"navigate", "--file", "a.txt", "--hunk", "2", "--no-wait"}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit = %d (stderr %q), want 0", code, errb.String())
	}
	got := steer.Drain(dir)
	if len(got) != 1 || got[0].Line == nil {
		t.Fatalf("posted %+v, want a command carrying a line", got)
	}
	if got[0].Line.Side != "new" {
		t.Errorf("side = %q, want new", got[0].Line.Side)
	}
	// A hunk's New span INCLUDES git's three lines of leading context, so an
	// edit on line 30 yields `@@ -27,7 +27,7 @@` and New[0] == 27. Landing on
	// the hunk's first line means its first line, context and all — that is
	// what phase 2's numbering hands out, and the change is then on screen.
	if got[0].Line.No != 27 {
		t.Errorf("line = %d, want 27 — --hunk lands on the FIRST line of hunk 2's new span, which starts 3 context lines above the edit", got[0].Line.No)
	}
}

func TestSessionHunkOnAnUntrackedPathIsRefused(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	repo := newCLIRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "brand-new.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	code := runSession(dir, domain.Open(repo), []string{"navigate", "--file", "brand-new.txt", "--hunk", "1", "--no-wait"}, &out, &errb)
	if code == 0 {
		t.Fatal("exit = 0, want a refusal")
	}
	if !strings.Contains(errb.String(), "--new-line") {
		t.Errorf("stderr = %q, want it to point at --new-line", errb.String())
	}
	if got := steer.Drain(dir); len(got) != 0 {
		t.Errorf("a refused command must never be posted, got %+v", got)
	}
}

func TestSessionNavigateBareRevPostsAFullSha(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	var out, errb bytes.Buffer
	code := runSession(dir, domain.Open(newCLIRepo(t)), []string{"navigate", "--rev", "HEAD", "--no-wait"}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit = %d (stderr %q), want 0", code, errb.String())
	}
	got := steer.Drain(dir)
	if len(got) != 1 {
		t.Fatalf("inbox = %+v, want one command", got)
	}
	// The consumer compares HASHES, so "HEAD" on the wire could never match.
	if len(got[0].Commit) != 40 {
		t.Fatalf("commit = %q, want a full 40-hex sha", got[0].Commit)
	}
}

func TestSessionNavigateBadRevExitsOne(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	var out, errb bytes.Buffer
	if code := runSession(dir, domain.Open(newCLIRepo(t)), []string{"navigate", "--rev", "no-such-rev", "--no-wait"}, &out, &errb); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if got := steer.Drain(dir); len(got) != 0 {
		t.Errorf("an unresolvable rev must never be posted, got %+v", got)
	}
}

func TestSessionFlagUsageErrorsExitTwo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	svc := domain.Open(newCLIRepo(t))
	for _, args := range [][]string{
		{"navigate", "--file", "a.txt", "--cached", "--rev", "HEAD", "--new-line", "1"},
		{"navigate", "--file", "a.txt"},                       // no line, no step
		{"navigate", "--file", "a.txt", "--new-line", "1", "--old-line", "2"},
		{"focus"},                                             // no panel
		{"highlight"},                                         // no add/clear
		{"nonsense"},
	} {
		args := args
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Parallel()
			var out, errb bytes.Buffer
			if code := runSession(dir, svc, args, &out, &errb); code != 2 {
				t.Errorf("exit = %d, want 2 (usage error); stderr %q", code, errb.String())
			}
		})
	}
}

func TestSessionReloadFocusAndHighlightPostTheRightCommand(t *testing.T) {
	t.Parallel()
	svc := domain.Open(newCLIRepo(t))
	cases := []struct {
		name string
		args []string
		want func(t *testing.T, c steer.Command)
	}{
		{"reload defaults to notes", []string{"reload", "--no-wait"}, func(t *testing.T, c steer.Command) {
			if c.Cmd != "reload" || len(c.Sources) != 1 || c.Sources[0] != "notes" {
				t.Errorf("posted %+v, want reload notes", c)
			}
		}},
		{"reload all", []string{"reload", "all", "--no-wait"}, func(t *testing.T, c steer.Command) {
			if len(c.Sources) != 1 || c.Sources[0] != "all" {
				t.Errorf("posted %+v, want reload all", c)
			}
		}},
		{"focus", []string{"focus", "commits", "--no-wait"}, func(t *testing.T, c steer.Command) {
			if c.Cmd != "focus" || c.Panel != "commits" {
				t.Errorf("posted %+v, want focus commits", c)
			}
		}},
		{"highlight add", []string{"highlight", "add", "--file", "a.txt", "--start", "4", "--end", "9", "--tone", "warn", "--no-wait"}, func(t *testing.T, c steer.Command) {
			if c.Cmd != "highlight" || c.Start != 4 || c.End != 9 || c.Tone != "warn" || c.Side != "new" {
				t.Errorf("posted %+v, want highlight a.txt new:4-9 warn", c)
			}
		}},
		{"highlight clear all", []string{"highlight", "clear", "--no-wait"}, func(t *testing.T, c steer.Command) {
			if c.Cmd != "highlight_clear" || c.File != "" {
				t.Errorf("posted %+v, want a file-less highlight_clear", c)
			}
		}},
		{"next comment", []string{"navigate", "--next-comment", "--no-wait"}, func(t *testing.T, c steer.Command) {
			if c.Cmd != "navigate" || c.Step != "next_note" {
				t.Errorf("posted %+v, want navigate step next_note", c)
			}
		}},
		{"prev comment", []string{"navigate", "--prev-comment", "--no-wait"}, func(t *testing.T, c steer.Command) {
			if c.Step != "prev_note" {
				t.Errorf("posted %+v, want step prev_note", c)
			}
		}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			livePresence(t, dir)
			var out, errb bytes.Buffer
			if code := runSession(dir, svc, tc.args, &out, &errb); code != 0 {
				t.Fatalf("exit = %d (stderr %q), want 0", code, errb.String())
			}
			got := steer.Drain(dir)
			if len(got) != 1 {
				t.Fatalf("inbox = %+v, want one command", got)
			}
			tc.want(t, got[0])
		})
	}
}

// hunkCLIRepo commits 60 numbered lines then edits two far-apart regions, so
// `gg diff --hunks` reports exactly two hunks (git's default 3 lines of context
// cannot bridge lines 5 and 30) and hunk 2's new span starts at line 30.
func hunkCLIRepo(t *testing.T) string {
	t.Helper()
	dir := newCLIRepo(t)
	lines := make([]string, 0, 60)
	for i := 1; i <= 60; i++ {
		lines = append(lines, "line "+strconv.Itoa(i))
	}
	write := func(ls []string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(strings.Join(ls, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(lines)
	gittest.Run(t, dir, "add", "a.txt")
	gittest.Run(t, dir, "commit", "-m", "seed")
	lines[4] = "EDITED-A"  // new line 5  → hunk 1
	lines[29] = "EDITED-B" // new line 30 → hunk 2
	write(lines)
	return dir
}
```

`newCLIRepo(t) string` is `internal/cli/worktree_test.go:16`;
`gittest.Run(t, dir, args...)` is `internal/gittest/template.go:14`. This file's
imports are: `bytes`, `encoding/json`, `os`, `path/filepath`, `strconv`,
`strings`, `testing`, `time`, and
`github.com/homeend/gigagit/internal/{domain,gittest,steer}`. `domain.Open(dir)`
(`internal/domain/service.go:149`) is how `internal/cli` tests build a service —
`domain.New` takes a `*git.Repo` and belongs to the TUI suite.

- [ ] **Step 2: Run it and watch it fail**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && go test ./internal/cli/ -run TestSession 2>&1 | head -15
```

Expected: `undefined: runSession`, `undefined: steerReplyWaitForTest`.

- [ ] **Step 3: Write `internal/cli/session.go`**

Create `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/cli/session.go`:

```go
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// steerReplyWaitForTest bounds the wait for a TUI's answer. A variable rather
// than a const so a test can shorten it; production never changes it.
var steerReplyWaitForTest = 2 * time.Second

// steerHTTPTimeout bounds the POST to an open gg web page. The endpoint answers
// 202 immediately, so anything slower is a page that is gone.
const steerHTTPTimeout = 2 * time.Second

// cmdSession is `gg session`: post a steering command to whatever gg session is
// showing this worktree.
func cmdSession(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	dir, err := sessionInboxDir(svc)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return runSession(dir, svc, args, stdout, stderr)
}

// sessionInboxDir resolves this worktree's inbox. "" (no state home) is not an
// error: it simply means nothing can be live, and the verbs then report so.
func sessionInboxDir(svc *domain.Service) (string, error) {
	ctx := context.Background()
	cd, err := svc.GitCommonDir(ctx)
	if err != nil {
		return "", err
	}
	top, err := svc.TopLevel(ctx)
	if err != nil {
		return "", err
	}
	return config.SessionSteerDir(cd, top), nil
}

// runSession dispatches one session subcommand against an explicit inbox dir.
// The dir is a parameter so tests can point it at t.TempDir() and stay parallel.
func runSession(dir string, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: gg session <status|navigate|reload|focus|highlight> [flags]")
		return 2
	}
	switch args[0] {
	case "status":
		return sessionStatus(dir, svc, args[1:], stdout, stderr)
	case "navigate":
		return sessionNavigate(dir, svc, args[1:], stdout, stderr)
	case "reload":
		return sessionReload(dir, args[1:], stdout, stderr)
	case "focus":
		return sessionFocus(dir, args[1:], stdout, stderr)
	case "highlight":
		return sessionHighlight(dir, svc, args[1:], stdout, stderr)
	}
	fmt.Fprintf(stderr, "session: unknown subcommand %q\n", args[0])
	return 2
}

// sessionRoute is who is live for this worktree right now.
type sessionRoute struct {
	tui, web     steer.Presence
	tuiOK, webOK bool
}

func routeFor(dir string) sessionRoute {
	if dir == "" {
		return sessionRoute{}
	}
	var r sessionRoute
	r.tui, r.tuiOK = steer.Live(dir, steer.TUIPresence)
	r.web, r.webOK = steer.Live(dir, steer.WebPresence)
	return r
}

// sendSteer applies the routing table: no live session is exit 1, a live web
// page gets a POST, a live TUI gets the inbox file, and when both are live the
// TUI's reply decides the exit code.
func sendSteer(dir string, c steer.Command, noWait bool, stdout, stderr io.Writer) int {
	r := routeFor(dir)
	if !r.tuiOK && !r.webOK {
		fmt.Fprintln(stderr, "no gg session for this worktree")
		return 1
	}
	webFailed := false
	if r.webOK {
		if err := postWebSteer(r.web.URL, c); err != nil {
			fmt.Fprintln(stderr, "web:", err)
			webFailed = true
		} else {
			fmt.Fprintln(stdout, "web: sent")
		}
	}
	if !r.tuiOK {
		if webFailed {
			return 1
		}
		return 0
	}
	c.Wait = !noWait
	id, err := steer.Post(dir, c)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if noWait {
		fmt.Fprintln(stdout, id)
		return 0
	}
	rep, ok := steer.AwaitReply(dir, id, steerReplyWaitForTest)
	if !ok {
		// The command is still in the inbox and may yet be applied — a slow
		// answer is not a failure, and reporting one would make an agent retry
		// and move the window twice.
		fmt.Fprintln(stdout, "queued: no answer from the TUI within 2s")
		return 0
	}
	if rep.OK {
		if rep.Detail != "" {
			fmt.Fprintln(stdout, rep.Detail)
		}
		return 0
	}
	fmt.Fprintln(stderr, rep.Error)
	return 1
}

// postWebSteer hands the command to an open gg web page. Content-Type is JSON
// and no Origin header is sent, which the server's writeGuard accepts from a
// non-browser client.
func postWebSteer(base string, c steer.Command) error {
	if base == "" {
		return errors.New("the gg web presence carries no URL")
	}
	body, err := json.Marshal(c)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(base, "/")+"/api/session/steer", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: steerHTTPTimeout}
	resp, err := client.Do(req)
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

// targetOf maps a resolved note address onto the wire target. It is the CLI's
// half of the same allowlist the web's noteState enforces.
func targetOf(a model.FileAddress) *steer.Target {
	t := &steer.Target{State: "unstaged"}
	switch a.State {
	case model.StateStaged:
		t.State = "staged"
	case model.StateUntracked:
		t.State = "untracked"
	case model.StateCommitted:
		t.State, t.Commit = "commit", a.Commit
	}
	return t
}

// resolveHunkLine turns --hunk N into the {side,no} the consumer lands on: the
// FIRST line of the hunk's new span, or its old span for a pure deletion. (A
// NOTE anchors at a range END; a landing wants the top of the region.) Resolved
// HERE and never in the TUI, so one landing path serves --hunk, --new-line and
// --old-line alike.
func resolveHunkLine(ctx context.Context, svc *domain.Service, cached bool, rev, path string, n int) (steer.Line, error) {
	spec, err := svc.HunkDiffSpec(ctx, cached, rev, []string{path})
	if err != nil {
		return steer.Line{}, err
	}
	files, err := svc.DiffHunks(ctx, spec)
	if err != nil {
		return steer.Line{}, err
	}
	for _, f := range files {
		if f.Path != path && f.OldPath != path {
			continue
		}
		for _, h := range f.Hunks {
			if h.N != n {
				continue
			}
			if h.New[0] > 0 {
				return steer.Line{Side: "new", No: h.New[0]}, nil
			}
			if h.Old[0] > 0 {
				return steer.Line{Side: "old", No: h.Old[0]}, nil
			}
			return steer.Line{}, fmt.Errorf("hunk %d of %s has no lines", n, path)
		}
		return steer.Line{}, fmt.Errorf("%s has %d hunks", path, len(f.Hunks))
	}
	return steer.Line{}, fmt.Errorf("%s has no hunks in this diff", path)
}

// sessionStatus prints the routing for this worktree.
func sessionStatus(dir string, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print the routing as JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	r := routeFor(dir)
	view := sessionOpenView(svc)
	if *asJSON {
		out := map[string]any{"worktree": r.worktree(), "view": view}
		if r.tuiOK {
			out["tui"] = map[string]any{"pid": r.tui.PID, "started": r.tui.Started}
		} else {
			out["tui"] = nil
		}
		if r.webOK {
			out["web"] = map[string]any{"pid": r.web.PID, "url": r.web.URL}
		} else {
			out["web"] = nil
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		if !r.tuiOK && !r.webOK {
			return 1
		}
		return 0
	}
	if !r.tuiOK && !r.webOK {
		fmt.Fprintln(stderr, "no gg session for this worktree")
		return 1
	}
	fmt.Fprintln(stdout, "worktree:", r.worktree())
	if r.tuiOK {
		fmt.Fprintf(stdout, "tui: pid %d (since %s)\n", r.tui.PID, r.tui.Started)
	}
	if r.webOK {
		fmt.Fprintf(stdout, "web: pid %d at %s\n", r.web.PID, r.web.URL)
	}
	if view != "" {
		fmt.Fprintln(stdout, "view:", view)
	}
	return 0
}

// worktree is whichever live presence knows it (both record the same path).
func (r sessionRoute) worktree() string {
	if r.tuiOK && r.tui.Worktree != "" {
		return r.tui.Worktree
	}
	return r.web.Worktree
}

// sessionOpenView reads the phase-0 session snapshot for a one-line "what is on
// screen" summary. Best-effort: no snapshot, or an unreadable one, is "".
func sessionOpenView(svc *domain.Service) string {
	cd, err := svc.GitCommonDir(context.Background())
	if err != nil {
		return ""
	}
	path := config.SessionSnapshotPath(cd)
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var snap struct {
		Focus struct {
			Panel string `json:"panel"`
		} `json:"focus"`
		Cursor struct {
			File   string `json:"file"`
			Commit string `json:"commit"`
		} `json:"cursor"`
	}
	if json.Unmarshal(data, &snap) != nil {
		return ""
	}
	parts := make([]string, 0, 2)
	if snap.Focus.Panel != "" {
		parts = append(parts, snap.Focus.Panel)
	}
	switch {
	case snap.Cursor.File != "":
		parts = append(parts, snap.Cursor.File)
	case snap.Cursor.Commit != "":
		parts = append(parts, snap.Cursor.Commit)
	}
	return strings.Join(parts, " / ")
}

// sessionNavigate is the navigate verb: a file + a line, a commit to reveal, or
// a note step.
func sessionNavigate(dir string, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session navigate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tf := addTargetFlags(fs)
	hunk := fs.Int("hunk", 0, "land on the Nth @@ hunk of --file (`gg diff --hunks` numbers them)")
	newLine := fs.Int("new-line", 0, "land on this line of the new side")
	oldLine := fs.Int("old-line", 0, "land on this line of the old side")
	next := fs.Bool("next-comment", false, "step the open diff to the next note")
	prev := fs.Bool("prev-comment", false, "step the open diff to the previous note")
	noWait := fs.Bool("no-wait", false, "post the command and exit without waiting for an answer")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *next || *prev {
		if *next && *prev {
			fmt.Fprintln(stderr, "session navigate: --next-comment and --prev-comment are mutually exclusive")
			return 2
		}
		step := "next_note"
		if *prev {
			step = "prev_note"
		}
		return sendSteer(dir, steer.Command{Cmd: "navigate", Step: step}, *noWait, stdout, stderr)
	}

	// A bare --rev with no --file reveals the commit. NoteTarget cannot be used
	// here — it refuses a target with no path (ErrNoteTargetUsage, "a note needs
	// a file path") — and the raw rev must NOT be posted either: the consumer
	// compares hashes (commitIsHash), so "HEAD" would match nothing and every
	// such command would come back "commit not loaded in the feed". Resolve it
	// to a full sha up front, and refuse anything that does not resolve.
	if *tf.file == "" {
		if *tf.rev == "" {
			fmt.Fprintln(stderr, "session navigate: pass --file, --rev, or --next-comment/--prev-comment")
			return 2
		}
		full, err := svc.RevParse(context.Background(), *tf.rev)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		if len(full) != 40 {
			fmt.Fprintf(stderr, "session navigate: %q did not resolve to a commit\n", *tf.rev)
			return 1
		}
		return sendSteer(dir, steer.Command{Cmd: "navigate", Commit: full}, *noWait, stdout, stderr)
	}

	set := 0
	for _, n := range []int{*hunk, *newLine, *oldLine} {
		if n != 0 {
			set++
		}
	}
	if set != 1 {
		fmt.Fprintln(stderr, "session navigate: pass exactly one of --hunk, --new-line, --old-line")
		return 2
	}

	ctx := context.Background()
	addr, err := svc.NoteTarget(ctx, *tf.file, *tf.cached, *tf.rev)
	if err != nil {
		if errors.Is(err, domain.ErrNoteTargetUsage) {
			fmt.Fprintln(stderr, "session navigate:", err)
			return 2
		}
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}

	var line steer.Line
	switch {
	case *hunk != 0:
		if addr.State == model.StateUntracked {
			fmt.Fprintln(stderr, "session navigate: untracked files have no hunks; use --new-line")
			return 1
		}
		line, err = resolveHunkLine(ctx, svc, *tf.cached, *tf.rev, addr.Path, *hunk)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
	case *newLine != 0:
		line = steer.Line{Side: "new", No: *newLine}
	default:
		line = steer.Line{Side: "old", No: *oldLine}
	}

	return sendSteer(dir, steer.Command{
		Cmd:    "navigate",
		File:   addr.Path,
		Target: targetOf(addr),
		Line:   &line,
	}, *noWait, stdout, stderr)
}

// sessionReload re-reads one or more of the TUI's sources.
func sessionReload(dir string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session reload", flag.ContinueOnError)
	fs.SetOutput(stderr)
	noWait := fs.Bool("no-wait", false, "post the command and exit without waiting for an answer")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	src := "notes"
	switch fs.NArg() {
	case 0:
	case 1:
		src = fs.Arg(0)
	default:
		fmt.Fprintln(stderr, "usage: gg session reload [notes|status|all] [--no-wait]")
		return 2
	}
	switch src {
	case "notes", "status", "all":
	default:
		fmt.Fprintf(stderr, "session reload: unknown source %q (notes, status or all)\n", src)
		return 2
	}
	return sendSteer(dir, steer.Command{Cmd: "reload", Sources: []string{src}}, *noWait, stdout, stderr)
}

// sessionFocus switches the TUI (or the web page) to a named panel.
func sessionFocus(dir string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session focus", flag.ContinueOnError)
	fs.SetOutput(stderr)
	noWait := fs.Bool("no-wait", false, "post the command and exit without waiting for an answer")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: gg session focus <files|staged|commits|branches|worktrees|remotes|tags|reflog|previews> [--no-wait]")
		return 2
	}
	return sendSteer(dir, steer.Command{Cmd: "focus", Panel: fs.Arg(0)}, *noWait, stdout, stderr)
}

// sessionHighlight adds or clears the attention bands an agent paints.
func sessionHighlight(dir string, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: gg session highlight <add|clear> [flags]")
		return 2
	}
	switch args[0] {
	case "add":
		return sessionHighlightAdd(dir, svc, args[1:], stdout, stderr)
	case "clear":
		return sessionHighlightClear(dir, svc, args[1:], stdout, stderr)
	}
	fmt.Fprintf(stderr, "session highlight: unknown subcommand %q (add or clear)\n", args[0])
	return 2
}

func sessionHighlightAdd(dir string, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session highlight add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tf := addTargetFlags(fs)
	start := fs.Int("start", 0, "first line of the band (1-based)")
	end := fs.Int("end", 0, "last line of the band; defaults to --start")
	side := fs.String("side", "new", "which side of the diff to paint: new or old")
	tone := fs.String("tone", "info", "band colour: info, warn or error")
	noWait := fs.Bool("no-wait", false, "post the command and exit without waiting for an answer")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *tf.file == "" || *start < 1 {
		fmt.Fprintln(stderr, "session highlight add: --file and a 1-based --start are required")
		return 2
	}
	switch *side {
	case "new", "old":
	default:
		fmt.Fprintf(stderr, "session highlight add: unknown side %q (new or old)\n", *side)
		return 2
	}
	switch *tone {
	case "info", "warn", "error":
	default:
		fmt.Fprintf(stderr, "session highlight add: unknown tone %q (info, warn or error)\n", *tone)
		return 2
	}
	if *end != 0 && *end < *start {
		fmt.Fprintln(stderr, "session highlight add: --end is before --start")
		return 2
	}
	addr, err := svc.NoteTarget(context.Background(), *tf.file, *tf.cached, *tf.rev)
	if err != nil {
		if errors.Is(err, domain.ErrNoteTargetUsage) {
			fmt.Fprintln(stderr, "session highlight add:", err)
			return 2
		}
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return sendSteer(dir, steer.Command{
		Cmd: "highlight", File: addr.Path, Target: targetOf(addr),
		Side: *side, Start: *start, End: *end, Tone: *tone,
	}, *noWait, stdout, stderr)
}

func sessionHighlightClear(dir string, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session highlight clear", flag.ContinueOnError)
	fs.SetOutput(stderr)
	tf := addTargetFlags(fs)
	noWait := fs.Bool("no-wait", false, "post the command and exit without waiting for an answer")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	c := steer.Command{Cmd: "highlight_clear"}
	if *tf.file != "" {
		addr, err := svc.NoteTarget(context.Background(), *tf.file, *tf.cached, *tf.rev)
		if err != nil {
			if errors.Is(err, domain.ErrNoteTargetUsage) {
				fmt.Fprintln(stderr, "session highlight clear:", err)
				return 2
			}
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		c.File, c.Target = addr.Path, targetOf(addr)
	}
	return sendSteer(dir, c, *noWait, stdout, stderr)
}
```

- [ ] **Step 4: Register the verb**

In `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/cli/cli.go`, add to `runOne`'s
switch, next to `case "note":`:

```go
	case "session":
		return cmdSession(svc, rest, stdout, stderr)
```

and add `"session": true,` to the `commands` map (this is also what makes
`gg batch` admit it — `cmdBatch` calls `runOne` directly, and `commands` is what
`cmd/gg` uses to choose the CLI over launching the TUI).

In `/mnt/t/others/gigagit.worktrees/feat-live-steering/cmd/gg/main.go`, add `session`
to the hardcoded usage list at l.103 (the comment there says it is kept in sync
with `cli.commands`).

- [ ] **Step 5: Run the CLI tests and watch them pass**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && go test ./internal/cli/ -run TestSession -v 2>&1 | tail -50
```

Expected: every test PASS. The expectation comes from phase 2's contract, not
from running it: `model.Hunk.New` is git's `@@` span, which carries three lines
of leading context, so an edit on new line 30 gives `@@ -27,7 +27,7 @@` and
`New[0] == 27`. If the test reports something else, print the patch
(`cd <fixture> && git diff`) and check whether the two edits merged into ONE
hunk — with fewer than seven lines between them git emits a single `@@`, and the
fixture, not the expectation, is what must change.

- [ ] **Step 6: Run the whole CLI package**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && go test ./internal/cli/... 2>&1 | tail -10
```

Expected: `ok`.

- [ ] **Step 7: gofmt and commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && gofmt -l internal/cli cmd/gg
```

Expected: empty output.

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && git add internal/cli/session.go internal/cli/session_test.go internal/cli/cli.go cmd/gg/main.go && git commit -m "$(cat <<'EOF'
feat(cli): gg session — status, navigate, reload, focus, highlight

The agent-facing half of live steering. Target flags reuse gg note's table
verbatim, and --hunk N is resolved HERE through phase 2's HunkDiffSpec +
DiffHunks into the FIRST line of the hunk's span, so the consumer only ever
sees file + target + side + line and one landing path serves all three flags.

Routing follows which presences are fresh: none is exit 1, a live web page
gets a POST, a live TUI gets the inbox file, and when both are live the TUI's
reply decides. A reply that does not arrive within 2s prints "queued" and
exits 0 — the command is still in the inbox, and reporting a failure would
make an agent retry and move the window twice.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU
EOF
)"
```

---

### Task 8: live notes for free — `reload notes` posts from every note mutation

Today `srcNotes` is never polled, so a note written by `gg note add` reaches a
running TUI only on a manual `r`. This task closes that gap without the agent
doing anything.

**Files:**
- Create: `internal/steer/notify.go` (`PostHTTP`, `NotifyReload`)
- Create: `internal/steer/notify_test.go`
- Modify: `internal/cli/session.go` (`postWebSteer` delegates to `steer.PostHTTP`; add `steerDirFor`)
- Modify: `internal/cli/note.go` (`withNotesHousekeeping` posts after a MUTATING sub-command)
- Modify: `internal/cli/note_test.go` (one end-to-end wiring test)
- Modify: `internal/cli/review.go` (`importReviewNotes` posts after `ApplyNoteBatch`, l.183-191)
- Modify: `internal/mcp/server.go` (a `steerDir` field, set in `New`)
- Modify: `internal/mcp/notes.go` (the three mutating tool handlers post)
- Create: `internal/mcp/steer_test.go`

**Interfaces:**
- Consumes: `steer.Command`, `steer.Live`, `steer.Post`, `steer.NewID`, `steer.TUIPresence`, `steer.WebPresence` (Task 1); `config.SessionSteerDir` (Task 2); `sessionInboxDir(svc)` (Task 7); `(*mcp.Server).commonDir` / `.worktree` (`internal/mcp/server.go:37`/`:39`).
- Produces:
  ```go
  // internal/steer/notify.go
  func PostHTTP(base string, c Command) error
  func NotifyReload(dir string, sources ...string)

  // internal/cli/session.go
  func steerDirFor(svc *domain.Service) string // "" on any failure; never an error to a note verb
  ```

**Ruling:** the post is best-effort, no-wait and silent. A note verb must never
fail, block, or print anything because of a window it does not own — §4.6's
"failures are silent". `NotifyReload` writes `wait:false`, so no reply file is
ever created for it.

- [ ] **Step 1: Write the failing notify test**

Create `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/steer/notify_test.go`:

```go
package steer

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNotifyReloadPostsOnlyForALivePresence(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	NotifyReload(dir, "notes")
	if got := Drain(dir); len(got) != 0 {
		t.Fatalf("posted %+v with no live presence, want nothing", got)
	}

	if err := Touch(dir, TUIPresence, Presence{PID: 1, Worktree: "/w"}); err != nil {
		t.Fatal(err)
	}
	NotifyReload(dir, "notes")
	got := Drain(dir)
	if len(got) != 1 {
		t.Fatalf("posted %+v, want one reload", got)
	}
	if got[0].Cmd != "reload" || len(got[0].Sources) != 1 || got[0].Sources[0] != "notes" {
		t.Errorf("posted %+v, want reload notes", got[0])
	}
	if got[0].Wait {
		t.Error("an automatic reload must be wait:false — nothing would read its reply")
	}
	if _, err := os.Stat(filepath.Join(dir, "reply-"+got[0].ID+".json")); !os.IsNotExist(err) {
		t.Errorf("a wait:false post must leave no reply file (err = %v)", err)
	}
}

func TestNotifyReloadSkipsAStalePresence(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := Touch(dir, TUIPresence, Presence{PID: 1, Worktree: "/w"}); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-LiveWindow - time.Second)
	if err := os.Chtimes(filepath.Join(dir, TUIPresence), old, old); err != nil {
		t.Fatal(err)
	}
	NotifyReload(dir, "notes")
	if got := Drain(dir); len(got) != 0 {
		t.Fatalf("posted %+v for a stale presence, want nothing", got)
	}
}

func TestNotifyReloadEmptyDirIsANoOp(t *testing.T) {
	t.Parallel()
	NotifyReload("", "notes") // must not panic
}

func TestNotifyReloadReachesALiveWebPage(t *testing.T) {
	t.Parallel()
	seen := make(chan Command, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/session/steer" {
			http.NotFound(w, r)
			return
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json (the server's writeGuard requires it)", ct)
		}
		if o := r.Header.Get("Origin"); o != "" {
			t.Errorf("Origin = %q, want none from a non-browser client", o)
		}
		body, _ := io.ReadAll(r.Body)
		var c Command
		_ = json.Unmarshal(body, &c)
		seen <- c
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	dir := t.TempDir()
	if err := Touch(dir, WebPresence, Presence{PID: 2, Worktree: "/w", URL: srv.URL}); err != nil {
		t.Fatal(err)
	}
	NotifyReload(dir, "notes")
	select {
	case c := <-seen:
		if c.Cmd != "reload" || c.ID == "" {
			t.Errorf("web got %+v, want a reload with an id", c)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the live web page was never told")
	}
}

func TestPostHTTPMapsTheGateConflict(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
	}))
	defer srv.Close()
	err := PostHTTP(srv.URL, Command{ID: "1-1", Cmd: "reload"})
	if err == nil {
		t.Fatal("a 409 must be an error")
	}
	if err.Error() != "operation in flight" {
		t.Errorf("err = %q, want \"operation in flight\"", err)
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && go test ./internal/steer/ -run 'TestNotify|TestPostHTTP' 2>&1 | head -10
```

Expected: `undefined: NotifyReload`, `undefined: PostHTTP`.

- [ ] **Step 3: Write `internal/steer/notify.go`**

Create `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/steer/notify.go`:

```go
package steer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// httpTimeout bounds a POST to an open gg web page. The endpoint answers 202
// immediately, so anything slower is a page that is already gone.
const httpTimeout = 2 * time.Second

// PostHTTP hands one command to an open gg web page. Content-Type is JSON and
// no Origin header is sent, which the server's writeGuard accepts from a
// non-browser client. A 409 means an operation is in flight and the page
// refused the command outright.
func PostHTTP(base string, c Command) error {
	if base == "" {
		return errors.New("the gg web presence carries no URL")
	}
	body, err := json.Marshal(c)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(base, "/")+"/api/session/steer", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: httpTimeout}).Do(req)
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

// NotifyReload tells whatever sessions are live for this inbox that the named
// sources changed. It is BEST-EFFORT and no-wait in every sense: no reply is
// requested (nothing would read one), every failure is swallowed, and a caller
// with no inbox, no live session or no network passes through untouched. A note
// verb must never fail, block, or print because of a window it does not own.
func NotifyReload(dir string, sources ...string) {
	if dir == "" || len(sources) == 0 {
		return
	}
	c := Command{Cmd: "reload", Sources: sources}
	if _, ok := Live(dir, TUIPresence); ok {
		_, _ = Post(dir, c)
	}
	if p, ok := Live(dir, WebPresence); ok {
		wc := c
		wc.ID = NewID()
		_ = PostHTTP(p.URL, wc)
	}
}
```

- [ ] **Step 4: Run the notify tests and watch them pass**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && go test ./internal/steer/... -v 2>&1 | tail -30
```

Expected: every test PASS.

- [ ] **Step 5: Wire the CLI note verbs and the review importer**

In `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/cli/session.go`, replace
`postWebSteer`'s body with a delegate (and drop the now-unused `bytes`,
`net/http` and `errors`-only imports if `gofmt`/`go vet` flags them — `errors`
is still used by the `errors.Is` calls):

```go
// postWebSteer hands the command to an open gg web page.
func postWebSteer(base string, c steer.Command) error { return steer.PostHTTP(base, c) }
```

and add:

```go
// steerDirFor resolves this worktree's inbox for a caller that must never fail
// because of it: any error is "" (no inbox), which every consumer treats as
// "nothing is live".
func steerDirFor(svc *domain.Service) string {
	dir, err := sessionInboxDir(svc)
	if err != nil {
		return ""
	}
	return dir
}
```

In `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/cli/note.go`, change
`withNotesHousekeeping` (l.57-70) to take the sub-command and post after a
mutation:

```go
// noteMutations are the sub-commands that CHANGE the store; only they are worth
// waking a live session for. `list` reads.
var noteMutations = map[string]bool{"add": true, "reply": true, "rm": true, "clear": true, "apply": true}

// withNotesHousekeeping applies [notes] from the effective config, runs fn, and
// only THEN starts and briefly waits for the sweep — the caller's work must
// never queue behind maintenance. A successful mutation also posts a
// best-effort `reload notes` to any live gg session for this worktree, so a
// note an agent just wrote appears in the human's open window without a manual
// refresh (srcNotes is never polled).
func withNotesHousekeeping(svc *domain.Service, sub string, fn func() int) int {
	if cfg, err := svc.EffectiveConfig(context.Background()); err == nil {
		svc.SetNotesPolicy(cfg.Notes.MaxAgeDays, cfg.Notes.MaxEntries)
	}
	code := fn()
	if code == 0 && noteMutations[sub] {
		steer.NotifyReload(steerDirFor(svc), "notes")
	}
	svc.StartNotesSweep()
	ctx, cancel := context.WithTimeout(context.Background(), notesSweepBudget)
	defer cancel()
	svc.WaitNotesSweep(ctx)
	return code
}
```

Update its single call site in `cmdNote` (l.30-55) to pass the sub-command it
already parsed — the dispatcher reads `args[0]` to choose the verb; pass that
same value (`""` when `args` is empty, which then matches no mutation). Add
`"github.com/homeend/gigagit/internal/steer"` to `note.go`'s imports.

In `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/cli/review.go`, in
`importReviewNotes` (l.151), after the successful `svc.ApplyNoteBatch` and
before the `fmt.Fprintln(stderr, "notes:", …)` line:

```go
	steer.NotifyReload(steerDirFor(svc), "notes")
```

Add `"github.com/homeend/gigagit/internal/steer"` to `review.go`'s imports.

- [ ] **Step 6: Add the CLI wiring test**

Append to `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/cli/note_test.go`
(this one exercises the REAL `cli.Run` path, so it sets the state home and does
NOT call `t.Parallel()` — the same shape as the note tests already in this file):

```go
// TestNoteAddPostsAReloadToALiveSession proves the wiring end to end: a note
// mutation run through cli.Run posts a `reload notes` into the inbox of a live
// session for this worktree.
func TestNoteAddPostsAReloadToALiveSession(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state) // no t.Parallel: t.Setenv panics in one
	repo := newCLIRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := domain.Open(repo)
	dir, err := sessionInboxDir(svc)
	if err != nil || dir == "" {
		t.Fatalf("sessionInboxDir = %q, %v", dir, err)
	}
	if err := steer.Touch(dir, steer.TUIPresence, steer.Presence{PID: 1, Worktree: repo}); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := Run(repo, []string{"note", "add", "--file", "a.txt", "--new-line", "1", "--summary", "hi"}, strings.NewReader(""), &out, &errb, ""); code != 0 {
		t.Fatalf("note add exit = %d (stderr %q)", code, errb.String())
	}
	got := steer.Drain(dir)
	if len(got) != 1 || got[0].Cmd != "reload" {
		t.Fatalf("inbox = %+v, want one reload notes", got)
	}
	if got[0].Wait {
		t.Error("the automatic reload must be wait:false")
	}
}

// TestNoteListPostsNothing: a read must not wake anybody.
func TestNoteListPostsNothing(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	repo := newCLIRepo(t)
	svc := domain.Open(repo)
	dir, _ := sessionInboxDir(svc)
	if err := steer.Touch(dir, steer.TUIPresence, steer.Presence{PID: 1, Worktree: repo}); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := Run(repo, []string{"note", "list"}, strings.NewReader(""), &out, &errb, ""); code != 0 {
		t.Fatalf("note list exit = %d (stderr %q)", code, errb.String())
	}
	if got := steer.Drain(dir); len(got) != 0 {
		t.Fatalf("a read posted %+v, want nothing", got)
	}
}
```

Make sure `note_test.go` imports `bytes`, `os`, `path/filepath`, `strings`,
`github.com/homeend/gigagit/internal/domain` and
`github.com/homeend/gigagit/internal/steer`.

- [ ] **Step 7: Wire MCP**

In `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/mcp/server.go`, add to the
`Server` struct:

```go
	// steerDir is this worktree's live-steering inbox; a mutating note tool
	// posts a best-effort `reload notes` into it so the human's open window
	// updates. "" = no inbox (no state home, or repo resolution failed).
	// Overridable by tests.
	steerDir string
```

and set it in `New`, immediately after `s.worktree = top`:

```go
		s.steerDir = config.SessionSteerDir(cd, top)
```

Add `"github.com/homeend/gigagit/internal/config"` to `server.go`'s imports.

Create `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/mcp/steer.go`:

```go
package mcp

import "github.com/homeend/gigagit/internal/steer"

// notifyNotesChanged tells a live gg session for this worktree that the note
// store changed. Best-effort and silent — an MCP tool's result must never
// depend on whether a human happens to have a window open.
func (s *Server) notifyNotesChanged() {
	steer.NotifyReload(s.steerDir, "notes")
}
```

In `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/mcp/notes.go`, call it at
the end of each of the three MUTATING handlers' success paths — `gg_note_add`
(registered l.120), `gg_notes_apply` (l.155) and `gg_note_rm` (l.197) — right
before each returns its result:

```go
	s.notifyNotesChanged()
```

`gg_notes_list` (l.92) is a read and gets none.

- [ ] **Step 8: Add the MCP test**

Create `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/mcp/steer_test.go`:

```go
package mcp

import (
	"testing"

	"github.com/homeend/gigagit/internal/steer"
)

func TestNotifyNotesChangedPostsForALiveSession(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := &Server{steerDir: dir} // the injected seam; no env, so this stays parallel
	s.notifyNotesChanged()
	if got := steer.Drain(dir); len(got) != 0 {
		t.Fatalf("posted %+v with no live presence, want nothing", got)
	}
	if err := steer.Touch(dir, steer.TUIPresence, steer.Presence{PID: 1, Worktree: "/w"}); err != nil {
		t.Fatal(err)
	}
	s.notifyNotesChanged()
	got := steer.Drain(dir)
	if len(got) != 1 || got[0].Cmd != "reload" || got[0].Sources[0] != "notes" {
		t.Fatalf("posted %+v, want one reload notes", got)
	}
}

func TestNotifyNotesChangedWithNoInboxIsANoOp(t *testing.T) {
	t.Parallel()
	(&Server{}).notifyNotesChanged() // must not panic
}
```

- [ ] **Step 9: Run every touched package**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && go test ./internal/steer/... ./internal/cli/... ./internal/mcp/... 2>&1 | tail -10
```

Expected: three `ok` lines.

- [ ] **Step 10: gofmt and commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && gofmt -l internal/steer internal/cli internal/mcp
```

Expected: empty output.

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && git add internal/steer/notify.go internal/steer/notify_test.go internal/cli/session.go internal/cli/note.go internal/cli/note_test.go internal/cli/review.go internal/mcp/server.go internal/mcp/steer.go internal/mcp/notes.go internal/mcp/steer_test.go && git commit -m "$(cat <<'EOF'
feat: live notes for free — every note mutation posts a reload

gg note add|reply|apply|rm|clear, gg review --notes and the three mutating
MCP note tools now post a best-effort, no-wait `reload notes` to any live
session for the caller's worktree. srcNotes is never polled, so before this
a CLI-written note reached a running TUI only on a manual refresh.

Best-effort in every sense: no reply requested, every failure swallowed, and
a caller with no inbox or no live session passes through untouched. A note
verb must never fail because of a window it does not own.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU
EOF
)"
```

---

### Task 9: the web half — presence, `POST /api/session/steer`, hub event, page

**Files:**
- Create: `internal/web/steer.go`
- Create: `internal/web/steer_test.go`
- Modify: `internal/web/server.go` (two `Server` fields)
- Modify: `internal/web/serve.go` (write the presence after `listen`, start the touch ticker, remove on shutdown)
- Modify: `internal/web/reroot.go` (re-home the presence, l.187 next to `touchMRU`)
- Modify: `internal/web/live.go` (`liveMsg` gains `Steer`; `emit` splits into `emit` + `fanOut`; add `emitSteer`)
- Modify: `internal/web/static/core.js` (one `state` field)
- Modify: `internal/web/static/live.js` (dispatch `reason === "steer"`)
- Modify: `internal/web/static/files.js` (attention classes in `diffHTML`; export `markDiffRow`)
- Modify: `internal/web/static/style.css` (three row classes)

**Interfaces:**
- Consumes: `steer.Command`/`Target`/`Line`/`Presence`/`Touch`/`Remove`/`WebPresence` (Task 1); `config.SessionSteerDir` and `(config.UIConfig).SteeringOn()` (Task 2); `writeGuard(next http.HandlerFunc) http.HandlerFunc` (`internal/web/server.go:224`), `isGitArgSafe(s string) bool` (`:248`), `writeJSON`/`writeErr` (`:194`/`:199`), `noteState(s string) (model.FileState, bool)` / `noteSide` (`internal/web/notes.go:36`/`:52`), `RegisterRoutes` (`internal/web/routereg.go`), `(*Server).opInFlight() bool` (`internal/web/live.go:201`), `(*Server).liveHubRef() *liveHub` (`:252`), `listen(addr) (net.Listener, string, error)` (`internal/web/serve.go:43`, URL built at `:116`).
- Produces:
  ```go
  // internal/web/server.go
  steerDir string // this worktree's inbox; "" = steering off (TEST SEAM)
  steerURL string // this server's own http://127.0.0.1:PORT

  // internal/web/steer.go
  type steerWire struct {
      Cmd     string   `json:"cmd"`
      File    string   `json:"file,omitempty"`
      State   string   `json:"state,omitempty"`
      Commit  string   `json:"commit,omitempty"`
      Side    string   `json:"side,omitempty"`
      Line    int      `json:"line,omitempty"`
      Step    string   `json:"step,omitempty"`
      Sources []string `json:"sources,omitempty"`
      Panel   string   `json:"panel,omitempty"`
      Start   int      `json:"start,omitempty"`
      End     int      `json:"end,omitempty"`
      Tone    string   `json:"tone,omitempty"`
  }
  func toSteerWire(c steer.Command) (steerWire, error)
  func (s *Server) handleSteer(w http.ResponseWriter, r *http.Request)
  func (s *Server) initSteerPresence(ctx context.Context, url string)
  func (s *Server) touchSteerPresence()
  func (s *Server) removeSteerPresence()
  func resolveSteerDir(ctx context.Context, svc *domain.Service) (dir, worktree string)
  func (s *Server) rehomeSteerPresence(ctx context.Context, svc *domain.Service)

  // internal/web/live.go
  func (h *liveHub) fanOut(msg liveMsg)
  func (h *liveHub) emitSteer(msg liveMsg)
  // liveMsg gains: Steer *steerWire `json:"steer,omitempty"`

  // internal/web/static/files.js — new export
  markDiffRow
  ```

**Rulings:**
1. The web's panes are `state.pane` = `commits | files` and `state.layout` =
   `list | files | diff` — NOT the TUI's nine panels. (`core.js:16`'s comment
   says `list | detail`; it is stale — `setLayout` at `files.js:60` proves the
   three real values, and `diff` maps to the `detail` CSS class.) `focus commits`
   shows the commit list, `focus files` / `staged` shows the file column, and
   every other protocol panel name is a no-op ON THE PAGE: the endpoint
   validated it, and it answers 202 with no page acknowledgement, so nothing is
   misreported. The TUI is the surface where `focus` means all nine.
2. The SPA has **one** theme (a single `:root` at `style.css:1-9`; no
   `prefers-color-scheme`, no `data-theme`), so the attention bands get one set
   of values, not a light and a dark pair.

- [ ] **Step 1: Write the failing endpoint tests**

Create `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/web/steer_test.go`:

```go
package web

import (
	"net/http"
	"testing"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

// newSteerServer builds a Server with its inbox injected — the seam that keeps
// these tests parallel and out of the real state home.
func newSteerServer(t *testing.T) *Server {
	t.Helper()
	s := New(domain.Open(newRepoDir(t, 1)))
	s.steerDir = t.TempDir()
	return s
}

// steerPost drives one request through the real handler chain (hostGuard +
// writeGuard included) using the suite's own helpers.
func steerPost(t *testing.T, s *Server, body, contentType string) int {
	t.Helper()
	ts := serve(t, s)
	var out map[string]any
	return postJSON(t, ts, "/api/session/steer", body, contentType, "", &out)
}

func TestSteerEndpointAccepts(t *testing.T) {
	t.Parallel()
	code := steerPost(t, newSteerServer(t), `{"id":"1-1","cmd":"navigate","file":"a.txt","target":{"state":"unstaged"},"line":{"side":"new","no":12}}`, "application/json")
	if code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", code)
	}
}

func TestSteerEndpointRejectsBadValues(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"id":"1-1","cmd":"teleport"}`,
		`{"id":"1-1","cmd":"navigate","file":"a.txt","target":{"state":"elsewhere"}}`,
		`{"id":"1-1","cmd":"navigate","file":"a.txt","line":{"side":"middle","no":1}}`,
		`{"id":"1-1","cmd":"navigate","file":"--upload-pack=evil","target":{"state":"unstaged"}}`,
		`{"id":"1-1","cmd":"navigate","commit":"-x"}`,
		`{"id":"1-1","cmd":"highlight","file":"a.txt","start":1,"tone":"shout"}`,
		`{"id":"1-1","cmd":"reload","sources":["weather"]}`,
		`not json`,
	} {
		body := body
		t.Run(body, func(t *testing.T) {
			t.Parallel()
			if code := steerPost(t, newSteerServer(t), body, "application/json"); code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 for %s", code, body)
			}
		})
	}
}

func TestSteerEndpointIs404WhenSteeringIsOff(t *testing.T) {
	t.Parallel()
	s := New(domain.Open(newRepoDir(t, 1))) // steerDir "" = steering off
	if code := steerPost(t, s, `{"id":"1-1","cmd":"reload","sources":["notes"]}`, "application/json"); code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 — with steering off the CLI must see no session", code)
	}
}

func TestSteerEndpointRequiresJSONContentType(t *testing.T) {
	t.Parallel()
	if code := steerPost(t, newSteerServer(t), `{"id":"1-1","cmd":"reload"}`, "text/plain"); code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415 (writeGuard)", code)
	}
}

func TestSteerEndpointAnswers409WhileAnOpIsInFlight(t *testing.T) {
	t.Parallel()
	s := newSteerServer(t)
	s.cur = &opRun{} // zero value: done is false, so opInFlight() is true
	if code := steerPost(t, s, `{"id":"1-1","cmd":"reload","sources":["notes"]}`, "application/json"); code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 — a steer must never be dropped silently by the hub gate", code)
	}
}

func TestSteerEmissionBypassesTheHubGate(t *testing.T) {
	t.Parallel()
	h := newLiveHub(config.RefreshConfig{}, false, func() bool { return true }) // gate always closed
	defer h.close()
	ch := h.subscribe()
	h.emit(liveMsg{Changed: []string{"notes"}, Reason: "notes"})
	select {
	case got := <-ch:
		t.Fatalf("emit delivered %+v through a closed gate", got)
	default:
	}
	h.emitSteer(liveMsg{Changed: []string{}, Reason: "steer", Steer: &steerWire{Cmd: "reload", Sources: []string{"notes"}}})
	select {
	case got := <-ch:
		if got.Reason != "steer" || got.Steer == nil {
			t.Fatalf("got %+v, want a steer message", got)
		}
	default:
		t.Fatal("emitSteer did not deliver: a one-off instruction must not be dropped by the op gate")
	}
}

func TestWebPresenceLifecycle(t *testing.T) {
	t.Parallel()
	s := newSteerServer(t)
	s.steerURL = "http://127.0.0.1:7777"
	s.touchSteerPresence()
	p, ok := steer.Live(s.steerDir, steer.WebPresence)
	if !ok {
		t.Fatal("no live web presence after a touch")
	}
	if p.URL != "http://127.0.0.1:7777" {
		t.Errorf("url = %q, want the server's own address — the CLI POSTs to it", p.URL)
	}
	s.removeSteerPresence()
	if _, ok := steer.Live(s.steerDir, steer.WebPresence); ok {
		t.Error("presence survived removeSteerPresence")
	}
}
```

Helpers used above are the suite's own: `newRepoDir(t, n)`
(`internal/web/server_test.go:37`), `serve(t, srv)` (`:51`) and
`postJSON(t, ts, path, body, contentType, origin, out)`
(`internal/web/status_test.go:41`, which returns the status code).
`newLiveHub(cfg config.RefreshConfig, watchOK bool, gate func() bool)` is
`internal/web/live.go:77`; the config is irrelevant here, only the gate matters.
`opRun`'s zero value has `done == false`, which is exactly what
`(*Server).opInFlight()` (`live.go:201`) reads as "in flight".

- [ ] **Step 2: Run it and watch it fail**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && go test ./internal/web/ -run 'TestSteer|TestWebPresence' 2>&1 | head -15
```

Expected: `s.steerDir undefined`, `undefined: steerWire`, `h.emitSteer undefined`.

- [ ] **Step 3: Add the `Server` fields**

In `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/web/server.go`, add to the
`Server` struct (next to `reposPath`, the other test seam):

```go
	// steerDir is this worktree's live-steering inbox and steerURL this
	// server's own loopback address, written into web.json so `gg session`
	// knows where to POST. steerDir "" = steering off: no presence is written
	// and POST /api/session/steer answers 404, so the CLI reports no session.
	// Both are test seams (set directly; nothing reads the environment).
	steerDir      string
	steerURL      string
	steerWorktree string // what `gg session status` prints for this session
```

- [ ] **Step 4: Split the hub's gate and widen `liveMsg`**

In `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/web/live.go`, add the
field to `liveMsg` (l.53-59):

```go
	Steer   *steerWire `json:"steer,omitempty"`
```

and replace `emit` (l.119-131) with:

```go
// emit fans msg out to every subscriber, non-blocking. Dropped whole while
// the gate reports an op in flight.
func (h *liveHub) emit(msg liveMsg) {
	if h.gate != nil && h.gate() {
		return
	}
	h.fanOut(msg)
}

// emitSteer fans a steering event out BYPASSING the gate. emit drops messages
// while an op runs because a refresh would show a half-applied tree and the
// post-op refresh covers it anyway; a steer is a one-off instruction from an
// agent, and dropping it would simply lose it. The endpoint checks the gate
// itself and answers 409 instead, so nothing reaches here mid-operation.
func (h *liveHub) emitSteer(msg liveMsg) { h.fanOut(msg) }

// fanOut is the delivery half both emitters share.
func (h *liveHub) fanOut(msg liveMsg) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stopped {
		return
	}
	for ch := range h.subs {
		select {
		case ch <- msg:
		default: // this tab's buffer is full — drop; its reconnect refreshes in full
		}
	}
}
```

- [ ] **Step 5: Write `internal/web/steer.go`**

Create `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/web/steer.go`:

```go
package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("POST /api/session/steer", writeGuard(s.handleSteer))
	})
}

// steerPresenceTick is how often the page's presence mtime is refreshed.
// Liveness IS the mtime, and steer.LiveWindow is five seconds.
const steerPresenceTick = time.Second

// steerWire is the validated command the page receives on the live hub. Every
// field has passed the same allowlists the notes lane uses, so the page can
// hand these values straight to its openers.
type steerWire struct {
	Cmd     string   `json:"cmd"`
	File    string   `json:"file,omitempty"`
	State   string   `json:"state,omitempty"`
	Commit  string   `json:"commit,omitempty"`
	Side    string   `json:"side,omitempty"`
	Line    int      `json:"line,omitempty"`
	Step    string   `json:"step,omitempty"`
	Sources []string `json:"sources,omitempty"`
	Panel   string   `json:"panel,omitempty"`
	Start   int      `json:"start,omitempty"`
	End     int      `json:"end,omitempty"`
	Tone    string   `json:"tone,omitempty"`
}

// toSteerWire validates one posted command. Untrusted values reach git argv
// through the page's own fetches, so `file` and `commit` go through
// isGitArgSafe and every enum through its allowlist — an unknown value is a
// 400, never a silent default.
func toSteerWire(c steer.Command) (steerWire, error) {
	w := steerWire{Cmd: c.Cmd, File: c.File, Commit: c.Commit, Step: c.Step,
		Sources: c.Sources, Panel: c.Panel, Start: c.Start, End: c.End, Tone: c.Tone}
	if c.File != "" && !isGitArgSafe(c.File) {
		return w, errors.New("unsafe file")
	}
	if c.Commit != "" && !isGitArgSafe(c.Commit) {
		return w, errors.New("unsafe commit")
	}
	if c.Target != nil {
		if _, ok := noteState(c.Target.State); !ok {
			return w, fmt.Errorf("unknown state %q", c.Target.State)
		}
		if c.Target.Commit != "" && !isGitArgSafe(c.Target.Commit) {
			return w, errors.New("unsafe target commit")
		}
		w.State = c.Target.State
		if w.State == "" {
			w.State = "unstaged"
		}
		if c.Target.Commit != "" {
			w.Commit = c.Target.Commit
		}
	}
	if c.Line != nil {
		if _, ok := noteSide(c.Line.Side); !ok {
			return w, fmt.Errorf("unknown side %q", c.Line.Side)
		}
		if c.Line.No < 1 {
			return w, errors.New("line must be 1-based")
		}
		w.Side, w.Line = c.Line.Side, c.Line.No
		if w.Side == "" {
			w.Side = "new"
		}
	}
	switch c.Cmd {
	case "navigate":
		switch c.Step {
		case "", "next_note", "prev_note":
		default:
			return w, fmt.Errorf("unknown step %q", c.Step)
		}
		if c.File == "" && c.Commit == "" && c.Step == "" {
			return w, errors.New("navigate needs a file, a commit or a step")
		}
	case "reload":
		if len(w.Sources) == 0 {
			w.Sources = []string{"notes"}
		}
		for _, n := range w.Sources {
			switch n {
			case "notes", "status", "all":
			default:
				return w, fmt.Errorf("unknown reload source %q", n)
			}
		}
	case "focus":
		if c.Panel == "" {
			return w, errors.New("focus needs a panel")
		}
	case "highlight":
		if c.File == "" {
			return w, errors.New("highlight needs a file")
		}
		if c.Start < 1 || (c.End != 0 && c.End < c.Start) {
			return w, errors.New("highlight needs a 1-based start and an end at or after it")
		}
		if _, ok := noteSide(c.Side); !ok {
			return w, fmt.Errorf("unknown side %q", c.Side)
		}
		w.Side = c.Side
		if w.Side == "" {
			w.Side = "new"
		}
		switch c.Tone {
		case "info", "warn", "error":
		default:
			return w, fmt.Errorf("unknown tone %q", c.Tone)
		}
		if w.End == 0 {
			w.End = w.Start
		}
	case "highlight_clear":
	default:
		return w, fmt.Errorf("unknown command %q", c.Cmd)
	}
	return w, nil
}

// handleSteer takes one command from `gg session` and hands it to every open
// page. It answers 202 at once — there is no page acknowledgement, and the CLI
// prints "web: sent".
func (s *Server) handleSteer(w http.ResponseWriter, r *http.Request) {
	if s.steerDir == "" {
		http.NotFound(w, r) // steering is off: the CLI must see no session here
		return
	}
	var c steer.Command
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, steer.MaxCommandBytes)).Decode(&c); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	wire, err := toSteerWire(c)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	// The hub drops everything while an op is in flight. A steer must not
	// vanish that way, so refuse it out loud instead — the CLI prints
	// "operation in flight" and exits 1.
	if s.opInFlight() {
		writeErr(w, http.StatusConflict, errors.New("operation in flight"))
		return
	}
	if h := s.liveHubRef(); h != nil {
		h.emitSteer(liveMsg{Changed: []string{}, Reason: "steer", Steer: &wire})
	}
	w.WriteHeader(http.StatusAccepted)
}

// resolveSteerDir resolves a service's inbox and its worktree. (Named apart
// from internal/cli's steerDirFor(svc) and internal/tui's
// steerDirFor(commonDir, worktree): three packages, three shapes, one concept.) A "" dir means
// steering is off for this server (not a repo, no state home, or the
// [ui] agent_steering key turned off) — every consumer reads it that way.
func resolveSteerDir(ctx context.Context, svc *domain.Service) (dir, worktree string) {
	cd, err := svc.GitCommonDir(ctx)
	if err != nil {
		return "", ""
	}
	top, err := svc.TopLevel(ctx)
	if err != nil {
		return "", ""
	}
	if cfg, cerr := svc.EffectiveConfig(ctx); cerr == nil && !cfg.UI.SteeringOn() {
		return "", ""
	}
	return config.SessionSteerDir(cd, top), top
}

// initSteerPresence claims this worktree's web presence once the listener's URL
// is known, and keeps its mtime fresh until ctx ends.
func (s *Server) initSteerPresence(ctx context.Context, url string) {
	s.steerURL = url
	s.steerDir, s.steerWorktree = resolveSteerDir(ctx, s.service())
	if s.steerDir == "" {
		return
	}
	s.touchSteerPresence()
	go func() {
		t := time.NewTicker(steerPresenceTick)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.touchSteerPresence()
			}
		}
	}()
}

// touchSteerPresence refreshes the page's presence: one os.Chtimes per second,
// which is the whole "is a gg web page open" protocol.
func (s *Server) touchSteerPresence() {
	if s.steerDir == "" {
		return
	}
	_ = steer.Touch(s.steerDir, steer.WebPresence, steer.Presence{
		PID:      os.Getpid(),
		Worktree: s.steerWorktree, // what `gg session status` prints
		Started:  time.Now().UTC().Format(time.RFC3339),
		URL:      s.steerURL,
	})
}

// removeSteerPresence ends the claim at once rather than letting it age out.
func (s *Server) removeSteerPresence() {
	if s.steerDir == "" {
		return
	}
	steer.Remove(s.steerDir, steer.WebPresence)
}

// rehomeSteerPresence moves the presence to the new repo on POST /api/reroot.
func (s *Server) rehomeSteerPresence(ctx context.Context, svc *domain.Service) {
	s.removeSteerPresence()
	s.steerDir, s.steerWorktree = resolveSteerDir(ctx, svc)
	s.touchSteerPresence()
}
```

This file's imports are `context`, `encoding/json`, `errors`, `fmt`,
`net/http`, `os`, `time`, and
`github.com/homeend/gigagit/internal/{config,domain,steer}`.

- [ ] **Step 6: Wire startup, shutdown and reroot**

In `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/web/serve.go`, in `Serve`,
after `srv.startLive(ctx)` (l.48) and its `defer srv.Close()` (l.49):

```go
	srv.initSteerPresence(ctx, url)
	defer srv.removeSteerPresence()
```

In `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/web/reroot.go`, in
`handleReroot`, next to `touchMRU(...)` (l.187):

```go
	s.rehomeSteerPresence(r.Context(), cand)
```

- [ ] **Step 7: Run the Go side and watch it pass**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && go test ./internal/web/ -run 'TestSteer|TestWebPresence' -v 2>&1 | tail -35
```

Expected: every test PASS.

- [ ] **Step 8: Add `state.attention` and the page's dispatch**

In `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/web/static/core.js`, add
to the `state` literal next to `diffRow`:

```js
  // Attention bands an agent painted (gg session highlight), keyed
  // "<state>\0<commit>\0<path>". Each value is a list of {side, start, end,
  // tone}. Cleared by highlight_clear and by an EXPLICIT steer reload — never
  // by the interval refresh, which no agent asked for.
  attention: new Map(),
```

In `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/web/static/files.js`,
add the band helper next to `curCls` in `diffHTML` (l.706) and use it in all
three layout branches' `<tr class="…">`:

```js
// attnKey/attnCls: the row classes a `gg session highlight` paints. A CLASS on
// the tr, not an inline background: tr.add/tr.del paint their own cells at
// higher specificity, so the CSS uses an inset box-shadow that layers over
// whatever background the cell already has.
function attnKey(ctx) {
  if (!ctx || !ctx.path) return "";
  return `${ctx.state || "unstaged"} ${ctx.rev || ""} ${ctx.path}`;
}
function attnCls(side, no) {
  if (!no || !state.attention.size) return "";
  const marks = state.attention.get(attnKey(state.diffCtx));
  if (!marks) return "";
  for (const m of marks) {
    if (m.side === side && no >= m.start && no <= m.end) return " attn-" + m.tone;
  }
  return "";
}
```

`diffHTML` composes the row class in **five** `<tr class="…">` templates
(files.js:666, 679, 687, 693, 711). Add the same one expression to each,
alongside `curCls`/`curClsBoth`; the side-by-side one (l.711) becomes:

```js
      html +=
        `<tr class="${r.kind}${hunkCls(r)}${curClsBoth(r)}${attnCls("new", r.right_no) || attnCls("old", r.left_no)}"${hunkAttr(r)}${anchor(aside, ano)}${both}>` +
```

and each single-side template uses that side's number, e.g.
`${attnCls("new", r.right_no)}` in a pure-add row.

**This must be derived inside `diffHTML`, never toggled after render.**
`renderDiff` re-runs from `state.lastDiff` on a window resize and on every notes
refresh (files.js:726, 789-795), so a class added post-hoc the way
`markDiffRow` adds `cur` would silently vanish. That is why `attnCls` reads
`state.attention` the same way `curCls` reads `state.diffRow`.

Add `markDiffRow` to `files.js`'s export list (the `export { … }` statement at
files.js:1863) — it is module-local today and `live.js` needs it. `attnCls`
stays module-local; only `diffHTML` calls it.

- [ ] **Step 9: Apply the command in `live.js`**

In `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/web/static/live.js`, extend
the imports:

```js
import { fetchNotes, markDiffRow, openFile, openStatusDiff, reconcileStatusView, refreshNoteCounts, renderDiff, setLayout, stepNote } from "./files.js";
import { loadCommits, openCommitByHash, renderCommits } from "./commits.js";
```

and add the dispatch inside `es.onmessage`, immediately after the `hello` arm:

```js
    if (msg.reason === "steer" && msg.steer) {
      applySteer(msg.steer);
      return;
    }
```

then add, next to `refreshSources`:

```js
// applySteer runs one command an agent posted through
// POST /api/session/steer. Every value arrived through the server's
// allowlists, so the openers can be handed it directly. Failures are silent:
// the endpoint answered 202 long ago and the CLI has already printed
// "web: sent" — there is nothing to report a failure to.
async function applySteer(s) {
  try {
    switch (s.cmd) {
      case "reload": {
        // Through the same single-flight gate manual `r` and the watcher use:
        // the server serialises reads under the per-repo gate, so overlapping
        // fans stack. A null return means one is already running, which is a
        // perfectly good answer to "reload".
        const want = new Set(s.sources && s.sources.length ? s.sources : ["notes"]);
        await runOnce("refresh", () => refreshSources(want));
        state.attention.clear();
        return;
      }
      case "focus":
        return steerFocus(s.panel);
      case "highlight":
        return steerHighlight(s);
      case "highlight_clear":
        return steerHighlightClear(s);
      case "navigate":
        return await steerNavigate(s);
    }
  } catch {
    // a page that moved on under us is not an error worth surfacing
  }
}

// steerFocus maps the protocol panel names onto the web's two panes. The page
// has `commits` and `files`, not the TUI's nine panels; a name it has no pane
// for is a no-op (the endpoint already validated it, and the CLI is told
// "web: sent", not what the page did with it).
function steerFocus(panel) {
  if (panel === "commits") {
    state.pane = "commits";
    setLayout("list");
    return;
  }
  if (panel === "files" || panel === "staged") {
    state.pane = "files";
  }
}

function steerAttnKey(s) {
  return `${s.state || "unstaged"} ${s.commit || ""} ${s.file}`;
}

function steerHighlight(s) {
  const k = steerAttnKey(s);
  const marks = state.attention.get(k) || [];
  marks.push({ side: s.side || "new", start: s.start, end: s.end || s.start, tone: s.tone });
  state.attention.set(k, marks);
  if (state.lastDiff) renderDiff(state.lastDiff);
}

function steerHighlightClear(s) {
  if (!s.file) state.attention.clear();
  else state.attention.delete(steerAttnKey(s));
  if (state.lastDiff) renderDiff(state.lastDiff);
}

// steerNavigate opens what the command names and marks the landed row. It
// reuses the very openers the .-menu rows use, so a steered landing is
// indistinguishable from a clicked one.
async function steerNavigate(s) {
  if (s.step) {
    stepNote(s.step === "next_note" ? 1 : -1);
    return;
  }
  if (!s.file) {
    if (s.commit) await openCommitByHash(s.commit);
    return;
  }
  if (s.state === "commit") {
    await openCommitByHash(s.commit);
    const i = state.files.findIndex((f) => f.path === s.file);
    if (i < 0) return;
    await openFile(i);
  } else {
    await fetchStatus(); // the agent may have written the file a moment ago
    const i = state.statusEntries.findIndex((f) => f.path === s.file);
    if (i < 0) return;
    await openStatusDiff(i);
  }
  if (!s.line) return;
  const side = s.side || "new";
  const tr = document.querySelector(`#diff-body tr[data-side="${side}"][data-no="${s.line}"]`);
  if (!tr) return;
  markDiffRow(tr, side, s.line);
  tr.scrollIntoView({ block: "center" });
}
```

(`fetchStatus` is already imported by `live.js` from `./status.js`; `state` and
`runOnce` from `./core.js`.)

- [ ] **Step 10: Add the three row classes**

The SPA has ONE theme: a single `:root` block at `style.css:1-9`, no
`prefers-color-scheme`, no `data-theme`. So there is one set of values, not two.

Add the tokens to that `:root` block, next to `--add`/`--del`:

```css
  --attn-info: #1e3350; --attn-warn: #3d3520; --attn-error: #43242a;
```

Then add the row rules AFTER the per-cell add/del rules at `style.css:119-124`
(`tr.add td.side.r` and friends are three-class selectors, so a two-class rule
placed earlier would lose to them) and BEFORE `table.diff tr.cur > td:first-child`
(l.738), whose inset accent bar must still win on the first cell:

```css
/* Attention bands an agent painted (gg session highlight). These must beat the
   per-cell tr.add/tr.del backgrounds at l.119-124, which are three-class
   selectors — hence td.side/td.no rather than a bare td. tr.cur adds no
   background of its own (it is an inset accent bar), so the two compose. */
table.diff tr.attn-info  td.side, table.diff tr.attn-info  td.no { background: var(--attn-info); }
table.diff tr.attn-warn  td.side, table.diff tr.attn-warn  td.no { background: var(--attn-warn); }
table.diff tr.attn-error td.side, table.diff tr.attn-error td.no { background: var(--attn-error); }
```

Specificity check before you move on: `table.diff tr.attn-warn td.side` is
(0,3,3) and `tr.add td.side.r` is (0,3,2) — the band wins on class count tie by
element count, and on an `attn` row the band is what the reader must see.

- [ ] **Step 11: Run the web package and verify in a browser**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && go test ./internal/web/... 2>&1 | tail -10
```

Expected: `ok`.

Then a live check against a REBUILT binary — the SPA is embedded, so a stale
binary silently serves the old JS:

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && go build -o /tmp/gg-steer ./cmd/gg && md5sum /tmp/gg-steer && (/tmp/gg-steer web --addr 127.0.0.1:0 &) && sleep 2
```

Read the `gg web: serving http://127.0.0.1:PORT` line from stderr, confirm the
port answers YOUR binary (`curl -s http://127.0.0.1:PORT/api/repo` and check the
path is this worktree — other agents may have a `gg web` running), then, from
the same worktree:

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && /tmp/gg-steer session status && /tmp/gg-steer session navigate --file <a changed file> --new-line 3
```

Expected: `status` lists `web: pid N at http://127.0.0.1:PORT`, and `navigate`
prints `web: sent` and exits 0 (there is no TUI, so no reply is waited for). In
the browser the page should open that file's diff with the row marked. Stop the
server when done (`kill` the pid `session status` printed — never a broad
`pkill`, other agents share this machine).

- [ ] **Step 12: gofmt and commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && gofmt -l internal/web
```

Expected: empty output.

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && git add internal/web/steer.go internal/web/steer_test.go internal/web/server.go internal/web/serve.go internal/web/reroot.go internal/web/live.go internal/web/static/core.js internal/web/static/live.js internal/web/static/files.js internal/web/static/style.css && git commit -m "$(cat <<'EOF'
feat(web): gg web takes steering commands too

gg web writes web.json with its own URL beside the TUI presence and refreshes
the mtime on a 1s ticker. POST /api/session/steer validates through the same
allowlists as the notes lane and emits on the live hub BYPASSING emit's op
gate — that gate exists to drop refreshes mid-operation, and a one-off
instruction dropped that way would simply be lost. The endpoint checks the
gate itself and answers 409 instead, which the CLI prints.

The page applies a command through the very openers its own menu rows use, so
a steered landing is indistinguishable from a clicked one. Attention bands are
an inset box-shadow, not a background: tr.add/tr.del paint their cells at
higher specificity, the same trap the note anchor already documents.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU
EOF
)"
```

---

### Task 10: the skills — `reviewing-with-gg`, `using-gg`, version bumps, dogfood copies

**Files:**
- Modify: `internal/agentskill/reviewing-with-gg.md` (a new `## Steering the user's window` section, after `## Guiding a review` at l.116 and before `## Caveats`)
- Modify: `internal/agentskill/using-gg.md` (a `gg session` entry in its command list)
- Modify: `internal/agentskill/agentskill.go` (`Version` 64 → 65, `ReviewVersion` 2 → 3)
- Modify: `internal/agentskill/agentskill_test.go` (`TestBodyCoversTheCLISurface`'s assertion list)
- Modify: `.claude/skills/reviewing-with-gg/SKILL.md` (regenerated dogfood copy)
- Modify: `.claude/skills/using-gg/SKILL.md` (regenerated dogfood copy)

**Interfaces:**
- Consumes: `agentskill.SkillFile() string`, `agentskill.ReviewingWithGG.SkillFile() string` (`internal/agentskill/agentskill.go:52`/`:62`), the `gg session` surface from Task 7.
- Produces: nothing other code calls. The two `TestDogfood*SkillCopyInSync` tests (`internal/agentskill/agentskill_test.go:14` and `:147`) byte-compare the committed copies against the rendered skills and FAIL until Step 5 regenerates them.

**Ruling:** §4.6 says `Version` 64 and `ReviewVersion` 3. `Version` is already
64 on this branch's base (phase 2 landed it), so this task takes it to **65** —
the spec's number was written against phase 2's starting point. `ReviewVersion`
is 2 and becomes 3, exactly as the spec says.

- [ ] **Step 1: Write the failing surface assertion**

In `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/agentskill/agentskill_test.go`,
add `"gg session navigate"` and `"gg session status"` to the string list
`TestBodyCoversTheCLISurface` (l.25-40) asserts is present in `using-gg.md`.

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && go test ./internal/agentskill/ -run TestBodyCoversTheCLISurface 2>&1 | head -10
```

Expected: FAIL — `using-gg.md` does not mention `gg session` yet.

- [ ] **Step 2: Add the steering section to `reviewing-with-gg.md`**

Insert this section into
`/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/agentskill/reviewing-with-gg.md`
between `## Guiding a review` and `## Caveats`:

```markdown
## Steering the user's window

If the human has gg open on this worktree, you can put their window on the
line you are talking about. Check first:

```bash
gg session status          # exit 1 = nothing open; do nothing more
```

When a session is live, navigate to a hunk BEFORE you leave its note, and
again after `gg note apply` so the reader lands on the first finding:

```bash
gg session navigate --file src/search.ts --hunk 2
gg session navigate --file src/search.ts --new-line 42 --cached
gg session navigate --rev <sha>                     # reveal a commit
gg session navigate --next-comment                  # step the open diff
gg session reload notes                             # after writing notes
gg session focus files
gg session highlight add --file src/search.ts --start 40 --end 46 --tone warn
gg session highlight clear --file src/search.ts
```

- `--file` + one of `--hunk N` / `--new-line N` / `--old-line N`; the target
  flags are `gg note`'s (`--cached`, `--rev <sha>`, neither = the working tree).
- A command WAITS up to 2s for the window's answer and prints what happened.
  Use `--no-wait` for fire-and-forget; it prints the command id and exits 0.
- Exit 1 means the window refused: an operation is running, a decision is
  waiting for the user, they are typing, or a picker owns the screen. That is
  not an error to work around — say what you found and move on.
- `gg note add|reply|apply|rm` already posts a reload on its own; you only need
  `gg session reload` after changing notes some other way.
- NEVER launch the TUI or `gg web` yourself. Steering drives a window the human
  chose to open; if none is open, your notes are still waiting for them.
```

- [ ] **Step 3: Add `gg session` to `using-gg.md`**

In `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/agentskill/using-gg.md`,
in the command list (the section `TestBodyCoversTheCLISurface` scans — the same
place `gg note add` and `gg review --notes` are listed), add:

```markdown
- `gg session status` — is a gg TUI or web page open on this worktree? Exit 1
  when none is.
- `gg session navigate --file <path> (--hunk N | --new-line N | --old-line N)
  [--cached | --rev <sha>]` — put the open window on that line; `--rev <sha>`
  alone reveals a commit, `--next-comment` / `--prev-comment` step the open
  diff. Add `--no-wait` to skip the 2s wait for the window's answer.
- `gg session reload [notes|status|all]`, `gg session focus <panel>`,
  `gg session highlight add|clear` — refresh, switch panel, or paint an
  attention band. See the `reviewing-with-gg` skill for when to use them.
```

- [ ] **Step 4: Bump both versions**

In `/mnt/t/others/gigagit.worktrees/feat-live-steering/internal/agentskill/agentskill.go`:

```go
// Version is bumped whenever using-gg.md (or the rendered wrappers) change.
// Installed copies carry it so init can tell new/outdated/up-to-date apart.
const Version = 65

// ReviewVersion is the same counter for reviewing-with-gg, which starts at 1
// and moves independently of Version.
const ReviewVersion = 3
```

- [ ] **Step 5: Regenerate the two dogfood copies**

The version markers are rendered INTO the committed `SKILL.md` files, so the
byte-compare tests fail until both are refreshed. Build the branch's own binary
and let it write them:

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && go build -o /tmp/gg-steering ./cmd/gg && /tmp/gg-steering init --update && /tmp/gg-steering init --agents claude-project
```

Then confirm the two files changed and contain the new versions:

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && git diff --stat .claude/skills/ && grep -c "gg session" .claude/skills/using-gg/SKILL.md .claude/skills/reviewing-with-gg/SKILL.md
```

Expected: both `SKILL.md` files modified; both grep counts non-zero.

- [ ] **Step 6: Run the agentskill gates**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && go test ./internal/agentskill/... ./internal/agentinit/... -v 2>&1 | tail -30
```

Expected: every test PASS, including `TestDogfoodSkillCopyInSync`,
`TestDogfoodReviewSkillCopyInSync`, `TestBodyCoversTheCLISurface` and
`TestRenderedFrontmatterIsPlainScalarSafe` (which forbids `": "` and `" #"` in a
skill's frontmatter `Description` — neither skill's description changed here, so
it should stay green; if it fails, a description was edited by mistake).

- [ ] **Step 7: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && gofmt -l internal/agentskill
```

Expected: empty output.

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && git add internal/agentskill/reviewing-with-gg.md internal/agentskill/using-gg.md internal/agentskill/agentskill.go internal/agentskill/agentskill_test.go .claude/skills/using-gg/SKILL.md .claude/skills/reviewing-with-gg/SKILL.md && git commit -m "$(cat <<'EOF'
docs(skill): teach agents gg session (ReviewVersion 3, Version 65)

reviewing-with-gg gains a "Steering the user's window" section: check
gg session status first, navigate to a hunk BEFORE leaving its note and
after note apply, treat an exit-1 refusal as a fact rather than something to
work around, and never launch a window the human did not open. using-gg
lists the verbs. Both dogfood copies regenerated — the byte-compare tests
fail until they are.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU
EOF
)"
```

---

### Task 11: the e2e scenario

**Files:**
- Create: `e2e/scenarios/s89_session_cli.toml`

**Interfaces:**
- Consumes: the `gg session` verbs from Task 7 and their exit codes; the harness schema in `.claude/skills/writing-e2e-scenarios/SKILL.md`.
- Produces: nothing other code calls.

**Ruling (why this scenario is thin):** the harness points `XDG_STATE_HOME` at
`<sandbox>/xdgstate` (`e2e/env_test.go:32`), and the inbox path underneath it is
`gg/sessions/<EncodeRepoKey(commonDir)>/steer/<EncodeRepoKey(worktree)>/` —
keys derived from the sandbox's own absolute path, which a static TOML `write`
step cannot name. The scenario therefore covers the paths that need NO presence
file (every verb's "nothing is live" behaviour and every usage error); the
live-presence paths (`--no-wait` printing an id, the reply wait, the routing
table) are covered by the Go CLI tests in Task 7, which inject the dir. This
mirrors phase 2's ruling that `note rm <id>` is a Go test because the runner
cannot feed one run's stdout into a later run's argv.

- [ ] **Step 1: Write the scenario**

Create `/mnt/t/others/gigagit.worktrees/feat-live-steering/e2e/scenarios/s89_session_cli.toml`:

The harness's `[[run]]` struct has **`stdout_contains` and `stdout_excludes`
only** — there is no `stderr_contains` key (`e2e/scenario.go:116-117`), and an
unknown key is a schema error. Every "no gg session" message goes to stderr, so
this scenario asserts on **exit codes**, plus `stdout_excludes` to prove the
verbs printed no command id (which would mean something was posted).

```toml
name = "session CLI: no live session, and the usage errors"

[input]
steps = [
  { write = "a.txt", content = "alpha\nbravo\ncharlie\ndelta\n" },
  { commit = "seed" },
  { write = "a.txt", content = "alpha\nBRAVO\ncharlie\nDELTA\n" },
]

# Nothing is open: status exits 1 and prints nothing to stdout.
[[run]]
cmd             = ["session", "status"]
exit            = 1
stdout_excludes = ["tui:", "web:"]

# Every steering verb reports the same thing rather than silently doing nothing.
# With no session, --no-wait never reaches the "print the id" branch: no
# "web: sent" line either, which is what distinguishes refused from posted.
[[run]]
cmd             = ["session", "navigate", "--file", "a.txt", "--new-line", "2", "--no-wait"]
exit            = 1
stdout_excludes = ["web: sent"]

[[run]]
cmd  = ["session", "navigate", "--rev", "HEAD", "--no-wait"]
exit = 1

[[run]]
cmd  = ["session", "navigate", "--next-comment", "--no-wait"]
exit = 1

[[run]]
cmd  = ["session", "reload", "notes", "--no-wait"]
exit = 1

[[run]]
cmd  = ["session", "focus", "files", "--no-wait"]
exit = 1

[[run]]
cmd  = ["session", "highlight", "add", "--file", "a.txt", "--start", "2", "--tone", "warn", "--no-wait"]
exit = 1

[[run]]
cmd  = ["session", "highlight", "clear", "--no-wait"]
exit = 1

# Usage errors are exit 2 and are caught BEFORE the routing check, so a mistake
# reports the mistake rather than "no gg session".
[[run]]
cmd  = ["session", "navigate", "--file", "a.txt"]
exit = 2

[[run]]
cmd  = ["session", "navigate", "--file", "a.txt", "--new-line", "1", "--old-line", "2"]
exit = 2

[[run]]
cmd  = ["session", "navigate", "--file", "a.txt", "--cached", "--rev", "HEAD", "--new-line", "1"]
exit = 2

[[run]]
cmd  = ["session", "reload", "weather", "--no-wait"]
exit = 2

[[run]]
cmd  = ["session", "focus"]
exit = 2

[[run]]
cmd  = ["session", "highlight"]
exit = 2

[[run]]
cmd  = ["session", "nonsense"]
exit = 2

# Steering never touches the repo.
[expect]
branch = "main"

[expect.status]
unstaged = ["a.txt"]
```

- [ ] **Step 2: Verify each assertion against the contract, then run it**

The harness rule is that expectations come from the operation's contract, never
from copying what happened. Re-read Task 7's `sendSteer` and confirm: it prints
`no gg session for this worktree` to STDERR and returns 1 when neither presence
is live, and it returns before the `--no-wait` branch that prints an id; every
flag-validation branch returns 2 and is reached before `sendSteer`; and no verb
writes to the repo, so the working tree still shows `a.txt` unstaged.

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && go test ./e2e -run 'TestScenarios/s89_session_cli' -v 2>&1 | tail -25
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && git add e2e/scenarios/s89_session_cli.toml && git commit -m "$(cat <<'EOF'
test(e2e): s89 — gg session with no live session, and its usage errors

Covers every verb's "nothing is open" path (exit 1, one message) and the
flag validation that must be caught before the routing check (exit 2). The
live-presence paths need an inbox whose name is derived from the sandbox's
own absolute path, which a static TOML step cannot write — those are Go CLI
tests with an injected dir.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU
EOF
)"
```

---

### Task 12: the docs tax and the full green run

**Files:**
- Modify: `CHANGELOG.md`
- Modify: `README.md` (the agent-lane paragraph)
- Modify: `docs/CLAUDE-details.md` (the long-form entry: command ids, refusal rules, layout)
- Modify: `CLAUDE.md` (the `Status` roadmap line only — the `steer` package row landed in Task 1)

**Interfaces:**
- Consumes: everything. This task adds no code.
- Produces: nothing other code calls.

- [ ] **Step 1: Add the CHANGELOG entry**

`CHANGELOG.md` contains a literal `<<<<<<<` line inside an older entry. It is
real content, not a conflict marker. Use `Edit` anchored on the prose of the
newest entry's heading; NEVER run a "conflict marker cleanup" and never rewrite
the file.

Find the topmost entry heading and insert above it:

```markdown
### Live steering — an agent can put your window on the line it means

`gg session` posts a small command to whatever gg session is showing the
current worktree: a running TUI, an open `gg web` page, or both.

```bash
gg session status                                   # exit 1 when nothing is open
gg session navigate --file src/x.go --hunk 2        # land the cursor there
gg session navigate --rev <sha>                     # reveal a commit
gg session navigate --next-comment                  # step the open diff's notes
gg session reload [notes|status|all]
gg session focus commits
gg session highlight add --file src/x.go --start 40 --end 46 --tone warn
gg session highlight clear [--file src/x.go]
```

- The channel is a file inbox beside the session snapshot, one per WORKTREE, so
  a command run in worktree A reaches the window showing A. Liveness is a fresh
  presence mtime, not a pid probe.
- `navigate` waits up to 2s for the window's answer and prints it; `--no-wait`
  prints the command id and exits at once.
- The window refuses (exit 1, with a reason) rather than acting while an
  operation is running, a decision is waiting, the user is typing, or a picker,
  the conflict editor, the rebase editor or the repo switcher is open.
- `highlight` paints an attention band over a line range, in three new theme
  roles `attention_info` / `attention_warn` / `attention_error`.
- **Live notes for free:** `gg note add|reply|apply|rm|clear`, `gg review
  --notes` and the mutating MCP note tools now post a `reload notes` on their
  own, so a note an agent just wrote appears in your open window without a
  manual refresh.
- Off with `[ui] agent_steering = "off"`; the frontends then write no presence
  and `gg session` reports no session.
```

- [ ] **Step 2: Update the README**

In `/mnt/t/others/gigagit.worktrees/feat-live-steering/README.md`, in the agent-lane
paragraph (search for `gg note` / `reviewing-with-gg`), add:

```markdown
An agent can also steer the window you already have open: `gg session status`
says whether one is running, and `gg session navigate --file <path> --hunk N`
puts your cursor on the hunk it is about to annotate. Every `gg note` mutation
posts a refresh on its own, so notes an agent writes appear without a manual
`r`. Turn the whole channel off with `[ui] agent_steering = "off"`.
```

(The `[ui] agent_steering` line itself was added to the `## Configuration`
section in Task 2.)

- [ ] **Step 3: Write the long-form entry**

Append to `/mnt/t/others/gigagit.worktrees/feat-live-steering/docs/CLAUDE-details.md`
(the archive CLAUDE.md points at for per-feature detail):

```markdown
### Live steering (`internal/steer`, phase 3)

**Layout**, under the phase-0 session dir
`<state>/gg/sessions/<EncodeRepoKey(commonDir)>/`:

```text
ui-state.json                      # the phase-0 snapshot, repo-level, untouched
steer/<EncodeRepoKey(worktree)>/   # ONE inbox per WORKTREE
  tui.json                         # {"pid","worktree","started"}; removed on exit and on reRoot
  web.json                         # + {"url"}
  cmd-<unixnano>-<pid>.json        # one command, temp+rename, unlinked by the consumer
  reply-<id>.json                  # the answer, unlinked by the CLI that read it
```

The snapshot is keyed by the git COMMON dir and shared by every worktree; the
inbox deliberately is not, so `gg session navigate` run in worktree A steers the
window showing A. Both re-home on `reRoot` through the same
`snapshotTargetMsg`.

**Liveness is a fresh mtime**, never a pid probe: each session re-touches its
presence on the 1 s tick (`heartbeatMsg` in the TUI, a ticker goroutine in
`gg web`), and `steer.Live` reports a presence under 5 s old, sweeping an older
one. One `os.Chtimes`/`os.Stat` per second, no per-OS process API, immune to pid
reuse. `Presence.PID` is display-only.

**Command ids** are `<unixnano>-<pid>`, which sorts by post time — `Drain`
returns commands in name order. A command over 16 KiB, unparsable, or carrying
no `id` is deleted and dropped: a reply file is named after the id, so an
id-less command is unanswerable by construction. `wait:false` gets no reply.
`Discard` sweeps `cmd-*` AND `reply-*` at startup, so a crashed session's
commands never replay and a reply the CLI abandoned on timeout never outlives
its session.

**Two sessions on the same worktree both drain the same inbox**, and whichever
unlinks a command file first applies it. This is rare (a second TUI on one
worktree), deliberately not guarded, and recorded here rather than arbitrated:
any lock would have to survive a SIGKILL, which is the exact problem mtime
liveness exists to avoid.

**Refusal rules** (`Model.steerRefusal`, `internal/tui/steer.go`) — a command is
answered `ok:false` with a reason and applied to nothing when: an op is running
or a load is in flight (`!m.opsIdle()`), a decision modal is up (`m.modal`), a
process owns the screen (`m.proc`, which is where the conflict resolver lives),
the action menu is open, any typing surface has focus (`filterTyping`,
`highlightTyping`, `recallOpen`, `filesView.typing`, `filesPreview.typing`,
`stashView.typing`), or `topLayer()` is outside the poppable whitelist
(`nil`, `*diffView`, `*historyView`, `*blameView`, `*contentPopup`) — which is
how the hunk/stage picker, the rebase editor and the repo switcher are refused
without enumerating all ~58 layer types.

**The navigate pipeline** parks a `pendingSteer` and drains it in the very
handler that owns the load it waits on — `dataAvailableMsg`/`srcStatus` for the
one status retry, `commitFilesMsg` for a commit's file list, `diffMsg` for the
landing. The landing must happen AFTER `*dv = *msg.view` in the `diffMsg` arm,
which overwrites `curLine`/`offset` with the loader's values. Every failure
branch routes through `failPending`, and a pending older than
`steerPendingTTL` (5 s) is expired by the heartbeat — a leaked pending would
land the cursor on the next unrelated diff.

**Two deliberate refusals.** A commit that is not loaded in the feed is refused
(`gotoLoadedCommit`, the half of `gotoCommitByHash` that never starts the eager
deep search) rather than triggering a search that can raise a prompt: never
raise a decision on an agent's behalf. A conflicted row is refused rather than
opened: the conflict editor is the user's to open.

**`--hunk N` is resolved by the CLI, never the TUI** — phase 2's `HunkDiffSpec`
+ `DiffHunks` turn it into the FIRST line of the hunk's new span (old side for a
pure deletion), so the consumer only ever sees file + target + side + line and
one landing path serves `--hunk`, `--new-line` and `--old-line`. An untracked
path has no git hunks and is refused with "untracked files have no hunks; use
--new-line".

**Attention marks** live in `m.attention map[attentionKey][]steerMark`, keyed by
path + target state + commit. They survive the interval auto-refresh on purpose:
a rebuild nobody asked for must not wipe an agent's bands. They die on
`highlight_clear`, an EXPLICIT `reload`, and `reRoot`. The cursor row outranks a
band in `diffPaneLines`.

**Web.** `gg web` writes `web.json` with its URL after `listen` and removes it on
shutdown and `reRoot`. `POST /api/session/steer` validates through the same
allowlists as the notes lane, checks the op gate itself (409
`operation in flight` — the CLI prints it and exits 1) and emits on the live hub
BYPASSING `liveHub.emit`'s gate, which would silently drop a one-off instruction.
The endpoint answers 202; there is no page acknowledgement, and the CLI prints
`web: sent`.

**Protocol prose stays English** — every `Reply.Detail`/`Error`, every command
JSON value, every CLI line. Only the on-screen notices the TUI posts
(`"▸ agent opened %s"`, `"▸ agent asked for a reload"`,
`"▸ agent marked lines in %s"`, `"▸ agent cleared its marks"`,
`"agent moved the focus"`) go through `i18n.T` and live in all four bundles.

**Test seams:** the inbox dir is a field (`m.steerDir`, `Server.steerDir`,
`mcp.Server.steerDir`) or a parameter (`cli.runSession(dir, …)`), never read from
the environment in a test — the tui suite runs `t.Parallel()` and `t.Setenv`
panics there.
```

- [ ] **Step 4: Update CLAUDE.md's Status line**

In `/mnt/t/others/gigagit.worktrees/feat-live-steering/CLAUDE.md`, in the `## Status`
section, the roadmap sentence lists what is next. Live steering has now shipped,
so it must not read as pending; leave the rest of the roadmap unchanged. (The
`steer` package-map row was added in Task 1 — do not add a second one.)

- [ ] **Step 5: Run the full staged suite**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && ./test.sh 2>&1 | tail -30
```

Expected: vet + gofmt clean, unit tests green, e2e green (including `s89`).

- [ ] **Step 6: Run the race gate**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && ./test.sh race 2>&1 | tail -30
```

Expected: green. `internal/steer`'s `Watcher` (a goroutine writing to a channel
under a mutex) and the TUI's presence touch are the two places a data race would
show up.

- [ ] **Step 7: Build a verify binary for the human**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && go build -o /mnt/t/others/gigagit.worktrees/feat-live-steering/gg-live-steering ./cmd/gg && ls -l /mnt/t/others/gigagit.worktrees/feat-live-steering/gg-live-steering
```

Hand the human that absolute path so they can try it before the merge. Do NOT
commit the binary.

- [ ] **Step 8: Commit the docs**

```bash
cd /mnt/t/others/gigagit.worktrees/feat-live-steering && git add CHANGELOG.md README.md docs/CLAUDE-details.md CLAUDE.md && git commit -m "$(cat <<'EOF'
docs: live steering — CHANGELOG, README, CLAUDE-details

The long-form entry records what an executor must not re-derive: the inbox
layout, mtime liveness, the refusal rules and why they are a layer whitelist,
where each pendingSteer stage drains and why the landing must follow
*dv = *msg.view, the two deliberate refusals (an unloaded commit, a
conflicted row), and the test seams.

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU
EOF
)"
```

---

## Rulings on spec ambiguities

Recorded so an executor does not re-litigate them:

1. **`[ui] agent_steering` is a STRING (`"on"`/`"off"`), not a bool.** §4.6 writes
   `= false`, which cannot work: under gg's zero-is-unset overlay rule a
   default-true bool's `false` is indistinguishable from "key absent", and
   `go-toml/v2` hard-errors decoding a TOML bool into a Go string field, so a
   user who typed the spec's literal could not start gg. Same reasoning, and
   the same shape, as the shipped `show_graph` (`internal/config/config.go:78-80`).
2. **Theme role keys are `attention_info` / `attention_warn` /
   `attention_error`.** §4.6 writes them dotted; `roleFields` keys are flat
   snake_case inside one `[themes.<name>]` table, where a dot would open a
   sub-table.
3. **`agentskill.Version` goes 64 → 65**, not "to 64". The spec's number was
   written against phase 2's starting point and phase 2 already landed 64.
   `ReviewVersion` 2 → 3 is exactly as specified.
4. **`steer.PostReply` / `steer.AwaitReply`, not `steer.Reply`** as a function.
   §4.6 says replies go "through `steer.Reply`", but `Reply` is the type; Go
   forbids a func and a type sharing a name in one package.
5. **A command with no `id` is deleted and dropped, with no reply.** §4.6 says
   it is "answered `ok:false`", which is impossible — the reply file is named
   after the id. Oversize files are likewise deleted unread. An unknown `cmd`
   DOES get `ok:false`: it has an id, so `Drain` returns it and `applySteer`'s
   `default` answers it.
6. **`step: next_note|prev_note` calls `m.jumpNote(±1)`**, not the whole `}`/`{`
   key arm. That arm's second behaviour — arm, then a SECOND press steps to the
   next noted FILE — is a two-keystroke interaction with no meaning for a
   one-shot command.
7. **`--hunk N` lands on the FIRST line of the hunk's span**, via `DiffHunks`
   rather than `HunkRange`. A NOTE anchors at a range END (phase 2's rule); a
   LANDING wants the top of the region, which is what §4.6's "`{side:new,
   no:<first new line>}`" says. That first line is git's own `@@` start, so it
   sits three context lines above the first CHANGED line — `model.Hunk` carries
   no per-line data to do better, and the change is on screen either way.
8. **A clamped landing answers `ok:true` with
   `detail:"opened <file>:<N>; clamped to line <N>"`** — §4.6's "clamped to line
   N" phrase appended to the normal detail, so one parse rule serves both.
9. **A parked navigate expires after 5 s** (`steerPendingTTL`, swept by the
   heartbeat). §4.6 does not bound it; without a bound, a command parked on a
   load the user cancelled would land the cursor on the next unrelated diff.
10. **Liveness is mtime ONLY.** §4.6's "Live notes for free" paragraph says
    "whenever a presence file … is live (pid alive)"; that phrasing predates the
    2026-09-10 mtime ruling in the same section and is superseded by it. No pid
    probe appears anywhere in this plan.
11. **The TUI tests inject the inbox dir; they do not set `XDG_STATE_HOME`.**
    §4.6's Testing paragraph says "parallel tests with `XDG_STATE_HOME` per
    test", which predates the test-seam ruling three paragraphs above it —
    `t.Setenv` panics in a parallel test. The ONE exception is a CLI test that
    must exercise the real `cli.Run` path (Task 8 Step 6), which sets the env
    and does not call `t.Parallel()`.
12. **The e2e scenario covers only the "nothing is live" paths.** The inbox path
    is `<state>/gg/sessions/<EncodeRepoKey(commonDir)>/steer/<EncodeRepoKey(worktree)>/`,
    keys derived from the sandbox's own absolute path, which a static TOML
    `write` step cannot name. The live-presence paths are Go CLI tests with an
    injected dir. Mirrors phase 2's ruling #7 about `note rm <id>`.
13. **`stderr_contains` does not exist** in the e2e harness (`e2e/scenario.go:116-117`
    has `stdout_contains` / `stdout_excludes` only), so s89 asserts exit codes.
14. **The web page's `focus` handles two names.** `state.pane` is
    `commits | files`; a protocol panel the page has no pane for is a silent
    no-op there. The endpoint still validates the name, and it answers 202 with
    no page acknowledgement, so nothing is misreported.
15. **The web SPA has one theme** — a single `:root` block, no
    `prefers-color-scheme` — so the attention bands get one set of values.
    They are per-cell `background` rules on `td.side`/`td.no`, high enough in
    specificity to beat `tr.add td.side.r`; `tr.cur` adds no background of its
    own, so the two compose.
16. **MCP gains no steering TOOL in v1** (§4.6 is explicit: MCP agents shell out
    to the CLI). Task 8 gives MCP only the outbound `reload notes` post.
17. **Two sessions on the same worktree both drain the inbox**; whichever
    unlinks a command file first applies it. §4.6 calls this rare, documented
    and deliberately not guarded — it is recorded in `docs/CLAUDE-details.md`
    (Task 12) and nothing tries to arbitrate.
18. **A bare `gg session navigate --rev <c>` resolves through `svc.RevParse`,
    not `NoteTarget`.** `NoteTarget` refuses a target with no path, and posting
    the raw rev would be worse than useless: the consumer compares hashes
    (`commitIsHash`), so `"HEAD"` on the wire matches nothing and every such
    command would answer "commit not loaded in the feed". Anything that does not
    resolve to 40 hex is refused before the command is written.
19. **`m.cfg` is stored at startup** (`run.go`, Task 4 Step 6). It is otherwise
    the zero value until the first `dataLoadedMsg`, which would make the
    steering gate read an empty `AgentSteering` (= on) and write a presence for
    a user who turned steering off.

## Self-review

**Spec coverage (§4.6 → task):**

| §4.6 requirement | Task |
|---|---|
| Inbox layout under the session dir; one dir per WORKTREE | 1, 2 |
| Presence files, liveness = fresh mtime under 5 s, stale swept, pid display-only | 1, 4, 9 |
| Re-home on `reRoot` (both frontends) | 4, 9 |
| Command file JSON: all seven shapes; temp+rename | 1 |
| Reply shape; unknown cmd / no id / >16 KiB; `wait:false` gets none | 1, 4 |
| Startup discard sweeps `cmd-*` AND `reply-*` | 1, 4 |
| `error`/`detail` English protocol prose; on-screen notices i18n in four bundles | 1, 4, 6 |
| `--hunk` resolved by the CLI; untracked refusal string | 7 |
| TUI consumer: presence, discard, fsnotify watch (best-effort), unconditional 1 s poll NOT gated on `gitwatch.Supported` | 4 |
| `Drain` in name order; `applySteer` on Update; `Reply` off-thread | 1, 4 |
| Refusal rules (op, modal, text field, picker/conflict/rebase/switcher); popping the poppable | 4 |
| `[ui] agent_steering` + `settingDoc`, disables BOTH frontends, web 404 | 2, 4, 9 |
| Test seam `m.steerDir` / `Server.steerDir` (+ MCP, + the CLI parameter) | header, 2, 4, 8, 9 |
| Navigate step 1: panels, filter clear, row by path, `openStatusDiff`, conflicted refusal, ONE status reload + retry, commit = loaded rows only | 5 |
| Navigate step 2: `{side,no}` anchor, `expandFoldFor`, `setCursorLine`, `alignCursor(alignCenter)`, clamp | 5 |
| Navigate step 3: `commit` alone; `step` with no open diff | 5 |
| Reply carries the landing detail; transient on-screen notice | 5, 6 |
| `reload` = `reloadSourcesCmd` over notes/status/all | 6 |
| Live notes for free: every `gg note` mutation, `gg review --notes`, the MCP note tools | 8 |
| Attention marks: map, three theme roles, band not box, lifetime rules, web `state.attention` | 3, 6, 9 |
| `focus` by `panelProtoName`, unknown → `ok:false` | 5 |
| CLI verb list, `gg batch` admits `session`, target flags = `gg note`'s | 7 |
| Routing table (none / TUI / web / both), POST with no Origin, 25 ms poll to 2 s, `queued` on timeout, `status` output | 7 |
| Web: `web.json`, endpoint + allowlists, gate check → 409, emit bypasses the gate, 202, page applies | 9 |
| Skill sections, `Version` / `ReviewVersion`, dogfood copies | 10 |
| CHANGELOG / README / CLAUDE.md row / `docs/CLAUDE-details.md` | 1 (row), 2 (README config), 12 |
| Testing list: steer package, TUI Update-driven, CLI, web, e2e | 1, 4, 5, 6, 7, 8, 9, 11 |

Nothing in §4.6 is unassigned. The one requirement that is deliberately
narrowed is the e2e scenario (ruling 12).

**Placeholder scan:** every code step carries real Go, JS, CSS, TOML or
Markdown. No "TBD", no "similar to Task N", no "add error handling". The two
places a step says "read the file first" name the exact function and line
(Task 10 Step 3's `using-gg.md` command list; Task 12 Step 1's CHANGELOG
anchor) and give the text to insert.

**Type consistency:** `steer.Command` / `Target` / `Line` / `Reply` /
`Presence` (Task 1) are used with identical field names in Tasks 4–9.
`steerOK(c, detail)` / `steerFail(c, reason)` / `m.answerSteer(c, r)` (Task 4)
keep one signature through Tasks 5 and 6. `attnStyle(tone) (lipgloss.Style, bool)`
(Task 3) is called only by `attnMarkFor` and `steerHighlight` (Task 6).
`(*diffView).lineAnchor(no int, old bool) (int, bool)` (Task 5) is the single
anchor used by both `noteAnchorLine` and `landSteer`.
`gotoLoadedCommit(hash) (Model, model.Commit, bool)` (Task 5) is called by
`gotoCommitByHash` and by both navigate branches. Three same-named-but-distinct
helpers were deliberately separated: `internal/tui`'s
`steerDirFor(commonDir, worktree) string`, `internal/cli`'s
`steerDirFor(svc) string`, and `internal/web`'s
`resolveSteerDir(ctx, svc) (dir, worktree string)` — renamed so no executor
cross-wires them. `steerReplyWaitForTest` is a `var` (a test shortens it), not a
const.

<!-- END -->

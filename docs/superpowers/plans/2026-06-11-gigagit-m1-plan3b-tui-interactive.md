# gigagit M1 — Plan 3B: Interactive TUI (operations + modal Decider) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the TUI drive the engine's smart operations — bind keys to SmartPull / Push / SmartSwitch / Stash / UndoLastCommit, stream live progress, resolve mid-flight forks through a modal Decider, and write a debug dump on panic — completing the M1 milestone.

**Architecture:** A channel bridge connects a synchronous `engine.Operation` (running in its own goroutine) to the Bubble Tea loop: events flow over a `chan tea.Msg` surfaced by a `waitForOp` command; a `uiDecider` implementing `engine.Decider` sends a decision request (with a reply channel) over the same channel and blocks until the modal answers — so the op goroutine blocks on a real human choice while `Update` never blocks. A reusable `app.DumpRepo` powers both `gg inspect --debug-dump` and a panic-recovery dump in `cmd/gg`.

**Tech Stack:** Go 1.26; bubbletea/lipgloss (already deps); existing internal packages. TUI logic tested via message dispatch and a real-git operation driven through the bridge; the modal tested by feeding keys and asserting the reply.

---

## Shared interfaces

```go
// internal/tui — message types and bridge
type opEventMsg struct{ event engine.Event }
type opDecisionMsg struct {
	req   engine.DecisionRequest
	reply chan engine.DecisionResponse
}
type opFinishedMsg struct {
	res engine.Result
	err error
}
func (m Model) startOp(op engine.Operation) (Model, tea.Cmd) // launches goroutines, returns waitForOp cmd
func waitForOp(msgs chan tea.Msg) tea.Cmd

type uiDecider struct{ msgs chan tea.Msg }
func (d uiDecider) Decide(ctx context.Context, req engine.DecisionRequest) (engine.DecisionResponse, error)

type decisionState struct {
	req   engine.DecisionRequest
	reply chan engine.DecisionResponse
	sel   int
}

// internal/app — reusable dump assembler
func DumpRepo(ctx context.Context, path string, repo *git.Repo, ring *observ.Ring, errs []string) error
```

New `Model` fields (added in Task 1): `running bool`, `statusMsg string`, `opMsgs chan tea.Msg`, `modal *decisionState`.

---

## Task 1: Operation bridge — run an engine op from the TUI

**Files:**
- Create: `internal/tui/op.go`
- Modify: `internal/tui/model.go` (fields + Update handling for op messages)
- Test: `internal/tui/op_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/tui/op_test.go`:
```go
package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/observ"
)

// newRepoDir is like newRepo but also returns the working directory.
func newRepoDir(t *testing.T) (string, *git.Repo) {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644)
	run("add", ".")
	run("commit", "-m", "initial")
	return dir, &git.Repo{Runner: gitexec.NewExecRunner("git", dir, observ.NewRing(50))}
}

// drives the op message loop until the operation finishes, returning the model.
func driveOp(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for i := 0; i < 50 && m.running; i++ {
		if cmd == nil {
			t.Fatal("ran out of commands before the operation finished")
		}
		msg := cmd()
		updated, next := m.Update(msg)
		m = updated.(Model)
		cmd = next
	}
	if m.running {
		t.Fatal("operation did not finish")
	}
	return m
}

func TestRunCommitOperationFinishesAndClearsRunning(t *testing.T) {
	dir, repo := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644)

	m := New(repo)
	// Apply an initial data load so the model is in the loaded state.
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)

	m, cmd := m.startOp(engine.Commit{Message: "second", All: true})
	if !m.running {
		t.Fatal("expected running=true right after startOp")
	}
	m = driveOp(t, m, cmd)
	if m.statusMsg == "" {
		t.Fatal("expected a status message after the operation")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestRunCommitOperation`
Expected: FAIL — undefined `startOp`, `running`, op message types.

- [ ] **Step 3: Implement the bridge**

Create `internal/tui/op.go`:
```go
package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/engine"
)

// opEventMsg carries one engine event (progress/done/gitline) to the UI.
type opEventMsg struct{ event engine.Event }

// opDecisionMsg asks the UI to resolve a fork; the op goroutine blocks on reply.
type opDecisionMsg struct {
	req   engine.DecisionRequest
	reply chan engine.DecisionResponse
}

// opFinishedMsg is sent once when the operation returns.
type opFinishedMsg struct {
	res engine.Result
	err error
}

// uiDecider bridges engine decisions to the UI over the msgs channel.
type uiDecider struct{ msgs chan tea.Msg }

func (d uiDecider) Decide(ctx context.Context, req engine.DecisionRequest) (engine.DecisionResponse, error) {
	reply := make(chan engine.DecisionResponse, 1)
	select {
	case d.msgs <- opDecisionMsg{req: req, reply: reply}:
	case <-ctx.Done():
		return engine.DecisionResponse{}, ctx.Err()
	}
	select {
	case resp := <-reply:
		return resp, nil
	case <-ctx.Done():
		return engine.DecisionResponse{}, ctx.Err()
	}
}

// startOp launches op in a goroutine, forwarding its events and completion onto
// a fresh message channel, and returns the command that waits for the next msg.
func (m Model) startOp(op engine.Operation) (Model, tea.Cmd) {
	msgs := make(chan tea.Msg, 32)
	events := make(chan engine.Event, 32)
	repo := m.repo
	go func() {
		res, err := op.Run(context.Background(), engine.OpDeps{
			Repo:    repo,
			Events:  events,
			Decider: uiDecider{msgs: msgs},
		})
		close(events)
		msgs <- opFinishedMsg{res: res, err: err}
	}()
	go func() {
		for e := range events {
			msgs <- opEventMsg{event: e}
		}
	}()
	m.running = true
	m.statusMsg = "working…"
	m.opMsgs = msgs
	return m, waitForOp(msgs)
}

// waitForOp blocks (off the UI thread) for the next op message.
func waitForOp(msgs chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-msgs }
}
```

Modify `internal/tui/model.go`:
1. Add fields to the `Model` struct (after `commits`):
```go
	running   bool
	statusMsg string
	opMsgs    chan tea.Msg
	modal     *decisionState
```
2. Add the message-handling cases to `Update`'s top-level `switch msg := msg.(type)` (alongside the existing `tea.WindowSizeMsg`, `dataLoadedMsg`, `tea.KeyMsg` cases):
```go
	case opEventMsg:
		switch e := msg.event.(type) {
		case engine.Progress:
			m.statusMsg = e.Step
			if e.Detail != "" {
				m.statusMsg += ": " + e.Detail
			}
		case engine.Done:
			m.statusMsg = e.Result.Summary
		}
		return m, waitForOp(m.opMsgs)
	case opDecisionMsg:
		m.modal = &decisionState{req: msg.req, reply: msg.reply}
		return m, waitForOp(m.opMsgs)
	case opFinishedMsg:
		m.running = false
		m.opMsgs = nil
		if msg.err != nil {
			m.statusMsg = "error: " + msg.err.Error()
		} else if msg.res.Summary != "" {
			m.statusMsg = msg.res.Summary
		}
		return m, m.loadCmd() // refresh panels after a mutating op
```
3. Add the `engine` import to model.go: `"github.com/gigagit/gg/internal/engine"`.
4. The `decisionState` type is defined in Task 2; for this task add a minimal definition at the top of `op.go` so the package compiles:
```go
type decisionState struct {
	req   engine.DecisionRequest
	reply chan engine.DecisionResponse
	sel   int
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ && go vet ./internal/tui/ && gofmt -l internal/tui`
Expected: PASS (op bridge + all prior tests); gofmt clean.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/op.go internal/tui/model.go internal/tui/op_test.go
git commit -m "feat: bridge engine operations into the TUI event loop"
```

---

## Task 2: Decision modal

**Files:**
- Modify: `internal/tui/model.go` (modal key handling)
- Modify: `internal/tui/view.go` (modal rendering)
- Test: `internal/tui/modal_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/tui/modal_test.go`:
```go
package tui

import (
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/engine"
)

func modalModel() (Model, chan engine.DecisionResponse) {
	m := New(nil)
	reply := make(chan engine.DecisionResponse, 1)
	m.modal = &decisionState{
		req:   engine.DecisionRequest{ID: "non-fast-forward", Prompt: "diverged", Options: []string{"rebase", "merge", "abort"}},
		reply: reply,
	}
	return m, reply
}

func TestModalEnterSendsSelectedOption(t *testing.T) {
	m, reply := modalModel()
	// move down once → "merge", then enter.
	updated, _ := m.Update(keyMsg("down"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("enter"))
	m = updated.(Model)

	if m.modal != nil {
		t.Fatal("modal should be cleared after a choice")
	}
	select {
	case resp := <-reply:
		if resp.Option != "merge" {
			t.Fatalf("option = %q, want merge", resp.Option)
		}
	default:
		t.Fatal("no response sent on the reply channel")
	}
}

func TestModalRendersPromptAndOptions(t *testing.T) {
	m, _ := modalModel()
	m.width, m.height = 80, 24
	out := m.View()
	if !strings.Contains(out, "diverged") {
		t.Fatalf("modal view missing prompt:\n%s", out)
	}
	for _, opt := range []string{"rebase", "merge", "abort"} {
		if !strings.Contains(out, opt) {
			t.Fatalf("modal view missing option %q:\n%s", opt, out)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestModal`
Expected: FAIL — modal key handling / rendering not implemented (Enter won't send; View won't show options).

- [ ] **Step 3: Implement**

In `internal/tui/model.go`, at the very top of the `tea.KeyMsg` case (before the existing `switch msg.String()`), intercept keys while a modal is open:
```go
	case tea.KeyMsg:
		if m.modal != nil {
			switch msg.String() {
			case "up", "k":
				if m.modal.sel > 0 {
					m.modal.sel--
				}
			case "down", "j":
				if m.modal.sel < len(m.modal.req.Options)-1 {
					m.modal.sel++
				}
			case "enter":
				m.modal.reply <- engine.DecisionResponse{Option: m.modal.req.Options[m.modal.sel]}
				m.modal = nil
			case "esc":
				m.modal.reply <- engine.DecisionResponse{Option: abortOption(m.modal.req.Options)}
				m.modal = nil
			}
			return m, nil
		}
		switch msg.String() {
		// ... existing q / r / tab / up / down cases unchanged ...
		}
```
(Keep the existing inner `switch msg.String()` block exactly as it is; only the `if m.modal != nil { … return m, nil }` guard is added above it.)

Add the helper (anywhere in model.go):
```go
// abortOption returns "abort" if offered, else the last option (safe default).
func abortOption(opts []string) string {
	for _, o := range opts {
		if o == "abort" {
			return o
		}
	}
	if len(opts) > 0 {
		return opts[len(opts)-1]
	}
	return ""
}
```

In `internal/tui/view.go`, render the modal when present. Change `View` delegation in model.go is already `m.render()`; in `render()` (view.go), at the very start, if `m.modal != nil`, return the modal overlay instead of the panels:
```go
func (m Model) render() string {
	if m.modal != nil {
		return m.renderModal()
	}
	// ... existing panel rendering ...
```
Add `renderModal` to view.go:
```go
var modalStyle = lipgloss.NewStyle().Border(lipgloss.DoubleBorder()).BorderForeground(lipgloss.Color("11")).Padding(1, 2)

func (m Model) renderModal() string {
	var b strings.Builder
	b.WriteString(m.modal.req.Prompt)
	b.WriteString("\n\n")
	for i, opt := range m.modal.req.Options {
		if i == m.modal.sel {
			b.WriteString(selectedRow.Render("> " + opt))
		} else {
			b.WriteString("  " + opt)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n[↑/↓] choose  [enter] confirm  [esc] abort")
	return modalStyle.Render(b.String()) + "\n"
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ && go vet ./internal/tui/ && gofmt -l internal/tui`
Expected: PASS; gofmt clean.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go internal/tui/view.go internal/tui/modal_test.go
git commit -m "feat: add decision modal for mid-operation forks"
```

---

## Task 3: Bind operation keys

**Files:**
- Modify: `internal/tui/model.go` (key → operation)
- Modify: `internal/tui/view.go` (footer + status line)
- Test: `internal/tui/keys_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/tui/keys_test.go`:
```go
package tui

import (
	"testing"
)

func TestPullKeyStartsOperation(t *testing.T) {
	m := loadedModel(t) // from nav_test.go; loads a single-commit repo on main
	updated, cmd := m.Update(keyMsg("p"))
	mm := updated.(Model)
	if !mm.running {
		t.Fatal("pressing p should start an operation (running=true)")
	}
	if cmd == nil {
		t.Fatal("expected a waitForOp command")
	}
	// Drive it to completion so the goroutine doesn't leak.
	driveOp(t, mm, cmd)
}

func TestKeysIgnoredWhileRunning(t *testing.T) {
	m := loadedModel(t)
	m.running = true // pretend an op is in flight
	updated, _ := m.Update(keyMsg("u"))
	mm := updated.(Model)
	// 'u' must not start a second operation while one is running.
	if mm.opMsgs != nil {
		t.Fatal("operation keys must be ignored while another op is running")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run 'TestPullKey|TestKeysIgnored'`
Expected: FAIL — 'p' does nothing yet.

- [ ] **Step 3: Implement**

In `internal/tui/model.go`, inside the non-modal `switch msg.String()` of the `tea.KeyMsg` case, add a guard and operation keys. First, immediately after entering the non-modal switch, ignore op keys while running by handling only navigation/quit when `m.running` — simplest: add the op cases but gate them on `!m.running`. Add these cases:
```go
		case "p":
			if !m.running && !m.loading {
				return m.startOp(engine.SmartPull{Intent: engine.PullAndStay})
			}
		case "P":
			if !m.running && !m.loading && m.status.Branch != "" {
				return m.startOp(engine.Push{Remote: "origin", Branch: m.status.Branch, SetUpstream: true})
			}
		case "s":
			if !m.running && !m.loading && len(m.branches) > 0 {
				target := m.branches[m.sel[panelBranches]].Name
				return m.startOp(engine.SmartSwitch{Branch: target})
			}
		case "S":
			if !m.running && !m.loading {
				return m.startOp(engine.Stash{Message: "gg stash"})
			}
		case "u":
			if !m.running && !m.loading {
				return m.startOp(engine.UndoLastCommit{})
			}
```
(Place these inside the existing `switch msg.String()` that already has `q`/`r`/`tab`/`up`/`down`. The `r` reload should also be gated: change its case to `case "r": if !m.running { m.loading = true; return m, m.loadCmd() }`.)

In `internal/tui/view.go`, show the status line and op keys in the footer. Replace the `footer` line in `render()`:
```go
	footer := "[p]ull [P]ush [s]witch [S]tash [u]ndo  •  [tab] focus  [r] reload  [q] quit"
	status := m.statusMsg
	if m.running {
		status = "⏳ " + status
	}
	return strings.Join([]string{header, body, footer, status}, "\n") + "\n"
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ && go vet ./internal/tui/ && gofmt -l internal/tui`
Expected: PASS; gofmt clean.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go internal/tui/view.go internal/tui/keys_test.go
git commit -m "feat: bind pull/push/switch/stash/undo keys in the TUI"
```

---

## Task 4: Reusable debug dump + panic recovery

**Files:**
- Modify: `internal/app/inspect.go` (extract exported `DumpRepo`, delegate)
- Modify: `cmd/gg/main.go` (panic-recovery dump around the TUI)
- Test: `internal/app/dump_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/app/dump_test.go`:
```go
package app

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/observ"
)

func TestDumpRepoWritesValidJSON(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644)
	run("add", ".")
	run("commit", "-m", "initial")

	ring := observ.NewRing(50)
	repo := &git.Repo{Runner: gitexec.NewExecRunner("git", dir, ring)}
	// Populate the ring with at least one span.
	_, _ = repo.Status(context.Background())

	path := filepath.Join(t.TempDir(), "dump.json")
	if err := DumpRepo(context.Background(), path, repo, ring, []string{"panic: boom"}); err != nil {
		t.Fatalf("DumpRepo: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var d observ.Dump
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatalf("dump not valid JSON: %v", err)
	}
	if d.Repo.Branch != "main" {
		t.Fatalf("dump branch = %q, want main", d.Repo.Branch)
	}
	if len(d.Errors) == 0 {
		t.Fatal("dump should include the provided errors")
	}
	if len(d.Recent) == 0 {
		t.Fatal("dump should include recent spans")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/app/ -run TestDumpRepo`
Expected: FAIL — undefined `DumpRepo`.

- [ ] **Step 3: Implement**

In `internal/app/inspect.go`, add the exported `DumpRepo` and make the existing `writeDump` delegate to it. Add:
```go
// DumpRepo assembles and writes a debug dump for repo using ring's recent spans.
// It is best-effort: git failures degrade gracefully into the dump's fields.
func DumpRepo(ctx context.Context, path string, repo *git.Repo, ring *observ.Ring, errs []string) error {
	st, err := repo.Status(ctx)
	if err != nil {
		errs = append(errs, err.Error())
	}
	gitVer := ""
	if res, verr := repo.Runner.Run(ctx, "git version", gitcmd.New("version").ToArgv()); verr == nil {
		gitVer = strings.TrimSpace(res.Stdout)
	}
	c := st.Counts()
	d := observ.Dump{
		GeneratedAt: time.Now(),
		GGVersion:   buildinfo.Version,
		GitVersion:  gitVer,
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
		Repo: observ.RepoInfo{
			Branch:   st.Branch,
			Upstream: st.Upstream,
		},
		WorkingTree: observ.DumpCounts{
			Staged: c.Staged, Unstaged: c.Unstaged,
			Untracked: c.Untracked, Conflicted: c.Conflicted,
		},
		Recent: ring.Snapshot(),
		Errors: errs,
	}
	return observ.WriteDump(path, d)
}
```
Then replace the body of the existing unexported `writeDump(ctx, path, runner, ring, st, errs)` so the `Inspect` path reuses `DumpRepo`. The simplest change: in `Inspect`, replace the `writeDump(...)` call with a `DumpRepo(...)` call. Since `Inspect` already has `runner` and `ring` and `repo`, change its dump block to:
```go
	if opts.DumpPath != "" {
		if derr := DumpRepo(ctx, opts.DumpPath, repo, ring, errs); derr != nil {
			return fmt.Errorf("write debug dump: %w", derr)
		}
		fmt.Fprintf(opts.Stdout, "debug dump written: %s\n", opts.DumpPath)
	}
```
and DELETE the now-unused `writeDump` function and the `gitStatus` alias if it becomes unused. Ensure imports (`runtime`, `strings`, `time`, `buildinfo`, `gitcmd`, `observ`, `git`) are present; remove any that become unused. Run `goimports`/`gofmt` and `go build` to confirm.

In `cmd/gg/main.go`, create the ring explicitly, keep a reference, and wrap the TUI launch with a panic-recovery dump:
```go
func main() {
	if len(os.Args) > 1 && os.Args[1] == "inspect" {
		runInspect(os.Args[2:])
		return
	}
	ring := observ.NewRing(200)
	repo := &git.Repo{Runner: gitexec.NewExecRunner("git", ".", ring)}
	defer func() {
		if r := recover(); r != nil {
			path := filepath.Join(os.TempDir(), fmt.Sprintf("gg-panic-%d.json", time.Now().Unix()))
			_ = app.DumpRepo(context.Background(), path, repo, ring, []string{fmt.Sprintf("panic: %v", r)})
			fmt.Fprintf(os.Stderr, "gg panicked; debug dump written to %s\n", path)
			panic(r)
		}
	}()
	if err := tui.Run(repo); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
```
Add imports `"path/filepath"` and `"time"` to main.go.

- [ ] **Step 4: Run test + full suite**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l internal cmd`
Expected: build OK; all PASS (including the existing `internal/app` inspect tests, now routed through `DumpRepo`); vet clean; gofmt prints nothing.

- [ ] **Step 5: Smoke check (inspect dump still works)**

Run:
```bash
go build -o /tmp/gg ./cmd/gg && /tmp/gg inspect --debug-dump /tmp/d.json && head -c 200 /tmp/d.json
```
Expected: dump file written and valid JSON. (Do NOT run the bare `gg` TUI headless — it needs a terminal.)

- [ ] **Step 6: Commit**

```bash
git add internal/app/inspect.go internal/app/dump_test.go cmd/gg/main.go
git commit -m "feat: reusable DumpRepo and panic-recovery debug dump"
```

---

## Self-Review

**Spec coverage (§4, §7, §9.3):**
- Operations driven from the TUI, async, non-blocking, with live progress → Tasks 1, 3 (`startOp`/`waitForOp`, status line).
- Modal Decider resolving mid-flight forks; the op goroutine blocks on a human choice while `Update` never blocks → Tasks 1–2 (`uiDecider` + modal).
- Smart-sync + undo keys (pull/push/switch/stash/undo) → Task 3.
- Panic-triggered debug dump (counts only, redacted, no diffs — inherited from `observ.WriteDump`) → Task 4.

**Deferred (note):** **credential-prompt routing** (git asking for HTTPS/SSH credentials) needs PTY/`GIT_ASKPASS` handling in `gitexec` and is a dedicated follow-up plan; headless/MCP already resolves via policy, and the operations here work for already-authenticated remotes. **Commit from the TUI** needs a text-input mode (message entry) — a small follow-up. The diff/context panel content remains a later enhancement.

**Placeholder scan:** none — every step contains complete code.

**Type consistency:** `opEventMsg`/`opDecisionMsg`/`opFinishedMsg`/`uiDecider`/`decisionState` defined in Tasks 1–2 and used consistently; `startOp` returns `(Model, tea.Cmd)` and is called that way in Task 3; `engine.SmartPull{Intent: engine.PullAndStay}`, `engine.Push{Remote,Branch,SetUpstream}`, `engine.SmartSwitch{Branch}`, `engine.Stash{Message}`, `engine.UndoLastCommit{}` match the engine signatures; `app.DumpRepo` signature matches both the test and the panic handler.

---

## Plan sequence (M1) — COMPLETE after this plan

1. Plan 1 — Foundation ✅  2. Plan 2A — Engine ✅  3. Plan 2B — Smart ops ✅  4. Plan 2C — Undo ✅
5. Plan 3A — Read-only TUI ✅  6. **Plan 3B — Interactive TUI** (this document) → **M1 done.**

Post-M1 follow-ups (not M1 scope): credential-prompt routing, TUI commit (text input), diff/context panel, viewport scrolling, then M2 (CLI + Workspaces) and M3 (MCP + heavy ops).

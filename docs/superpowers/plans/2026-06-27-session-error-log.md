# Session Error Log Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Record every git operation that fails (returns an error to a frontend) to an always-on durable global `errors.log` in the gg state dir, and add a Settings-menu viewer for the current session's failures.

**Architecture:** A new process-global "failure seam" in `internal/observ` (`NoteFailure` / `SetFailureSink` / `SessionFailures`) mirrors the existing span-sink seam. `internal/domain` feeds it at the two boundaries where a genuine failure surfaces to a frontend — the generic read-query helper and `Service.Execute`. The TUI opens `errors.log` always-on at startup (registering it as the sink) and adds a read-only Settings viewer over the in-memory ring.

**Tech Stack:** Go 1.26, Bubble Tea TUI, standard library only (no new deps).

## Global Constraints

- Module path: `github.com/homeend/gigagit`.
- `internal/observ` has no other-package dependencies — keep it that way (no `repos`/`domain`/`git` imports). Path resolution for the file lives in `internal/tui`.
- `internal/tui` and `internal/cli` never import `internal/git` directly (archtest-guarded) — these tasks don't add such imports.
- Capture point is the **domain boundary only**. Do NOT filter spans in `internal/gitexec`; tolerated non-zero probes must never be recorded.
- A failure = a non-nil `error` returned to a frontend, excluding `context.Canceled` / `context.DeadlineExceeded`.
- Durable file is **always-on** — no config knob, no toggle.
- Follow TDD: failing test first, minimal code, passing test, commit.

---

### Task 1: `observ` failure seam

**Files:**
- Create: `internal/observ/failures.go`
- Test: `internal/observ/failures_test.go`

**Interfaces:**
- Produces:
  - `type FailureEntry struct { Time time.Time; Source string; Detail string }`
  - `func NoteFailure(source string, err error)`
  - `func SetFailureSink(w io.Writer)`
  - `func SessionFailures() []FailureEntry` (newest-first)
  - `func ResetFailures()` (test isolation)

- [ ] **Step 1: Write the failing test**

Create `internal/observ/failures_test.go`:

```go
package observ

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestNoteFailureRecordsAndWrites(t *testing.T) {
	ResetFailures()
	var buf bytes.Buffer
	SetFailureSink(&buf)
	NoteFailure("query status", errors.New("git status failed (exit 128):\nfatal: boom"))

	fs := SessionFailures()
	if len(fs) != 1 {
		t.Fatalf("want 1 entry, got %d", len(fs))
	}
	if fs[0].Source != "query status" {
		t.Fatalf("source = %q", fs[0].Source)
	}
	// Detail is collapsed to one line.
	if strings.Contains(fs[0].Detail, "\n") {
		t.Fatalf("detail not collapsed: %q", fs[0].Detail)
	}
	if !strings.Contains(fs[0].Detail, "fatal: boom") {
		t.Fatalf("detail missing stderr: %q", fs[0].Detail)
	}
	line := buf.String()
	if !strings.Contains(line, "query status") || !strings.Contains(line, "fatal: boom") {
		t.Fatalf("sink line missing fields: %q", line)
	}
	if strings.Count(line, "\n") != 1 {
		t.Fatalf("want exactly one line written: %q", line)
	}
}

func TestNoteFailureIgnoresNilAndCancellation(t *testing.T) {
	ResetFailures()
	NoteFailure("x", nil)
	NoteFailure("x", context.Canceled)
	NoteFailure("x", fmt.Errorf("wrap: %w", context.DeadlineExceeded))
	if fs := SessionFailures(); len(fs) != 0 {
		t.Fatalf("want 0 entries, got %d: %+v", len(fs), fs)
	}
}

func TestSessionFailuresNewestFirst(t *testing.T) {
	ResetFailures()
	NoteFailure("a", errors.New("1"))
	NoteFailure("b", errors.New("2"))
	fs := SessionFailures()
	if len(fs) != 2 || fs[0].Source != "b" || fs[1].Source != "a" {
		t.Fatalf("want newest-first [b,a], got %+v", fs)
	}
}

func TestFailureRingEviction(t *testing.T) {
	ResetFailures()
	for i := 0; i < failureRingCap+50; i++ {
		NoteFailure(fmt.Sprintf("s%d", i), errors.New("e"))
	}
	fs := SessionFailures()
	if len(fs) != failureRingCap {
		t.Fatalf("want cap %d, got %d", failureRingCap, len(fs))
	}
	// Newest first: the last recorded source leads.
	if fs[0].Source != fmt.Sprintf("s%d", failureRingCap+49) {
		t.Fatalf("oldest not evicted; head = %q", fs[0].Source)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/observ/ -run TestNoteFailure -v`
Expected: FAIL — `undefined: ResetFailures` / `SetFailureSink` / `NoteFailure` / `SessionFailures` / `failureRingCap`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/observ/failures.go`:

```go
package observ

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// FailureEntry is one genuine, frontend-surfaced failure: a git operation or
// read query that returned an error to a frontend.
type FailureEntry struct {
	Time   time.Time `json:"time"`
	Source string    `json:"source"`
	Detail string    `json:"detail"`
}

// failureRingCap bounds the in-memory session ring read by SessionFailures.
const failureRingCap = 500

var (
	failMu   sync.Mutex
	failRing []FailureEntry
	failSink io.Writer
)

// SetFailureSink registers (nil clears) the durable writer that every
// subsequent NoteFailure appends one line to. Ring collection is independent
// and always on. Mirrors SetSpanSink: nil (the default) means ring-only, so
// the CLI/library/tests see no file side effect unless they opt in.
func SetFailureSink(w io.Writer) {
	failMu.Lock()
	defer failMu.Unlock()
	failSink = w
}

// NoteFailure records a genuine failure. A nil error or a context
// cancellation/deadline (a user abort, not a git failure) is ignored. The
// entry joins a bounded process-global ring and, when a sink is set, is
// appended there as one tab-separated line.
func NoteFailure(source string, err error) {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return
	}
	e := FailureEntry{Time: time.Now(), Source: source, Detail: oneLine(err.Error())}
	failMu.Lock()
	defer failMu.Unlock()
	failRing = append(failRing, e)
	if len(failRing) > failureRingCap {
		failRing = failRing[len(failRing)-failureRingCap:]
	}
	if failSink != nil {
		fmt.Fprintf(failSink, "%s\t%s\t%s\n", e.Time.UTC().Format(time.RFC3339), e.Source, e.Detail)
	}
}

// SessionFailures returns a newest-first copy of the failure ring.
func SessionFailures() []FailureEntry {
	failMu.Lock()
	defer failMu.Unlock()
	out := make([]FailureEntry, len(failRing))
	for i, e := range failRing {
		out[len(failRing)-1-i] = e
	}
	return out
}

// ResetFailures clears the ring and sink. Tests use it for isolation.
func ResetFailures() {
	failMu.Lock()
	defer failMu.Unlock()
	failRing = nil
	failSink = nil
}

// oneLine collapses all runs of whitespace (including newlines) to single
// spaces so a multi-line git stderr renders as one log line / one list row.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/observ/ -v`
Expected: PASS (all failure tests plus the existing observ tests).

- [ ] **Step 5: Commit**

```bash
git add internal/observ/failures.go internal/observ/failures_test.go
git commit -m "feat(observ): session failure seam (NoteFailure/SetFailureSink/SessionFailures)"
```

---

### Task 2: Capture genuine failures at the domain boundary

**Files:**
- Modify: `internal/domain/query.go` (the generic `query[T]` helper, ~line 39-53; add `observ` import)
- Modify: `internal/domain/service.go` (`Execute`, after the span emit ~line 180)
- Test: `internal/domain/failures_test.go`

**Interfaces:**
- Consumes: `observ.NoteFailure`, `observ.SessionFailures`, `observ.ResetFailures` (Task 1).
- Produces: failure sources `"query <key>"` (e.g. `query status`, `query snapshot`) and `"op <Name>"` (e.g. `op SmartPull`, matching `engine.OpName`).

- [ ] **Step 1: Write the failing test**

Create `internal/domain/failures_test.go`:

```go
package domain_test

import (
	"context"
	"errors"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/observ"
)

// failOp returns a plain error; engine.OpName(failOp{}) == "failOp".
type failOp struct{}

func (failOp) Run(ctx context.Context, deps engine.OpDeps) (engine.Result, error) {
	return engine.Result{}, errors.New("kaboom")
}

// cancelOp returns context.Canceled (a user abort, not a failure).
type cancelOp struct{}

func (cancelOp) Run(ctx context.Context, deps engine.OpDeps) (engine.Result, error) {
	return engine.Result{}, context.Canceled
}

func newFakeService() *domain.Service {
	// A bare FakeRunner errors for every span ("fake: no response configured"),
	// so any read query fails.
	return domain.New(&git.Repo{Runner: gitexec.NewFakeRunner()})
}

func TestQueryFailureRecorded(t *testing.T) {
	observ.ResetFailures()
	s := newFakeService()
	if _, err := s.Status(context.Background()); err == nil {
		t.Fatal("expected Status to fail against a bare fake runner")
	}
	fs := observ.SessionFailures()
	if len(fs) != 1 || fs[0].Source != "query status" {
		t.Fatalf("want one 'query status' failure, got %+v", fs)
	}
}

func TestExecuteFailureRecorded(t *testing.T) {
	observ.ResetFailures()
	s := newFakeService()
	if _, err := s.Execute(context.Background(), failOp{}, nil, nil); err == nil {
		t.Fatal("expected Execute(failOp) to return the op error")
	}
	fs := observ.SessionFailures()
	if len(fs) != 1 || fs[0].Source != "op failOp" {
		t.Fatalf("want one 'op failOp' failure, got %+v", fs)
	}
}

func TestExecuteCancellationNotRecorded(t *testing.T) {
	observ.ResetFailures()
	s := newFakeService()
	_, _ = s.Execute(context.Background(), cancelOp{}, nil, nil)
	if fs := observ.SessionFailures(); len(fs) != 0 {
		t.Fatalf("cancellation must not be recorded, got %+v", fs)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/domain/ -run "TestQueryFailureRecorded|TestExecuteFailureRecorded|TestExecuteCancellationNotRecorded" -v`
Expected: FAIL — `TestQueryFailureRecorded` and `TestExecuteFailureRecorded` find 0 entries (the hook is not wired yet).

- [ ] **Step 3a: Wire the query helper**

In `internal/domain/query.go`, add `observ` to imports:

```go
import (
	"context"
	"strconv"
	"strings"
	"sync"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/observ"
	"github.com/homeend/gigagit/internal/repogate"
)
```

Replace the `flight.Do` closure body in `query[T]` (record the leader's `fn` error only, so coalesced followers don't double-count):

```go
func query[T any](ctx context.Context, s *Service, key string, fn func(context.Context) (T, error)) (T, error) {
	v, err := s.flight.Do(key, func() (any, error) {
		res, e := s.gateFor(ctx).Acquire(ctx, repogate.Read, "read "+key)
		if e != nil {
			return nil, e
		}
		defer res.Release()
		out, ferr := fn(ctx)
		if ferr != nil {
			observ.NoteFailure("query "+key, ferr)
		}
		return out, ferr
	})
	if err != nil {
		var zero T
		return zero, err
	}
	return v.(T), nil
}
```

- [ ] **Step 3b: Wire Execute**

In `internal/domain/service.go`, in `Execute`, after the existing `observ.EmitSpan(span)` and before `return out, opErr`, add the `NoteFailure` call (it no-ops on nil and on cancellation):

```go
	observ.EmitSpan(span)
	observ.NoteFailure(label, opErr)
	return out, opErr
```

(`label` is already `"op " + engine.OpName(op)` from earlier in the method.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/domain/ -run "TestQueryFailureRecorded|TestExecuteFailureRecorded|TestExecuteCancellationNotRecorded" -v`
Expected: PASS.

Then run the full domain package to confirm no regression (other tests may leave failure entries; that is harmless — `ResetFailures` is only called by the new tests):

Run: `go test ./internal/domain/`
Expected: ok.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/query.go internal/domain/service.go internal/domain/failures_test.go
git commit -m "feat(domain): record genuine failures at query + Execute boundaries"
```

---

### Task 3: Always-on durable `errors.log` + startup wiring

**Files:**
- Create: `internal/tui/errlog.go`
- Test: `internal/tui/errlog_test.go`
- Modify: `cmd/gg/main.go` (TUI launch path, before `tui.Run(svc)` ~line 90)

**Interfaces:**
- Consumes: `observ.SetFailureSink` (Task 1), `repos.DefaultStatePath`.
- Produces:
  - `func defaultErrLogPath() string` (unexported; used by the Settings label in Task 4)
  - `func OpenErrorLog() (*os.File, string, error)` (exported; called by `cmd/gg`)

- [ ] **Step 1: Write the failing test**

Create `internal/tui/errlog_test.go`:

```go
package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultErrLogPathName(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	p := defaultErrLogPath()
	if p == "" {
		t.Fatal("expected a path with XDG_STATE_HOME set")
	}
	if filepath.Base(p) != "errors.log" {
		t.Fatalf("want basename errors.log, got %q", p)
	}
}

func TestOpenErrorLogCreatesAppendable(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	f, path, err := OpenErrorLog()
	if err != nil {
		t.Fatalf("OpenErrorLog: %v", err)
	}
	if f == nil {
		t.Fatal("expected a file handle with a state dir set")
	}
	defer f.Close()
	if _, err := f.WriteString("x\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("errors.log not created: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run "TestDefaultErrLogPathName|TestOpenErrorLogCreatesAppendable" -v`
Expected: FAIL — `undefined: defaultErrLogPath` / `OpenErrorLog`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/tui/errlog.go`:

```go
package tui

import (
	"os"
	"path/filepath"

	"github.com/homeend/gigagit/internal/repos"
)

// defaultErrLogPath puts the always-on error log beside operations.log in the
// gg state dir, reusing the repo registry's platform-appropriate resolution.
// "" when no home/state dir exists.
func defaultErrLogPath() string {
	sp := repos.DefaultStatePath()
	if sp == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(sp), "errors.log")
}

// OpenErrorLog opens (creating as needed) the always-on error log for
// appending and returns the handle plus its path. Unlike the operation log it
// has no on/off toggle: every genuine failure is recorded for the whole
// session. Returns (nil, "", nil) when there is no state dir — nothing to open,
// and not an error worth blocking TUI launch.
func OpenErrorLog() (*os.File, string, error) {
	path := defaultErrLogPath()
	if path == "" {
		return nil, "", nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, path, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, path, err
	}
	return f, path, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run "TestDefaultErrLogPathName|TestOpenErrorLogCreatesAppendable" -v`
Expected: PASS.

- [ ] **Step 5: Wire startup in `cmd/gg/main.go`**

In the TUI launch path, immediately before `cwd, err := tui.Run(svc)` (~line 90), register the always-on sink (best-effort — a failed open must not block the TUI):

```go
	if ef, _, eerr := tui.OpenErrorLog(); eerr == nil && ef != nil {
		observ.SetFailureSink(ef)
		defer func() { observ.SetFailureSink(nil); _ = ef.Close() }()
	}
	cwd, err := tui.Run(svc)
```

(`cmd/gg/main.go` already imports `internal/tui` and `internal/observ`.)

- [ ] **Step 6: Verify build + package tests**

Run: `go build ./cmd/gg && go test ./internal/tui/ -run "ErrLog|ErrorLog"`
Expected: build succeeds; tests PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/errlog.go internal/tui/errlog_test.go cmd/gg/main.go
git commit -m "feat(tui): open always-on errors.log and register failure sink at startup"
```

---

### Task 4: Settings menu row + session-errors viewer

**Files:**
- Modify: `internal/tui/settings_popup.go` (struct field, menu const/slice, `settingsMenuLabel`, `update`, `box`; add `observ` import)
- Test: `internal/tui/settings_errors_test.go`

**Interfaces:**
- Consumes: `observ.SessionFailures` (Task 1), `defaultErrLogPath` (Task 3), existing `renderWindow`/`winRow`/`winOpts`/`selectedRow`/`dispMode`.
- Produces: `settingsMenuErrors` menu entry; `settingsPopup.errorsView` screen.

- [ ] **Step 1: Write the failing test**

Create `internal/tui/settings_errors_test.go`:

```go
package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/observ"
)

func errorsMenuIndex(t *testing.T) int {
	t.Helper()
	for i := range settingsMenu {
		if settingsMenu[i] == settingsMenuErrors {
			return i
		}
	}
	t.Fatal("session-errors entry missing from settings menu")
	return -1
}

func settingsErrModel() Model {
	return New(domain.New(&git.Repo{Runner: gitexec.NewFakeRunner()}))
}

func TestSettingsErrorsLabelShowsCount(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	observ.ResetFailures()
	m := settingsErrModel()
	idx := errorsMenuIndex(t)

	if got := settingsMenuLabel(m, idx); !strings.Contains(got, "none") {
		t.Fatalf("empty-state label should say 'none': %q", got)
	}
	observ.NoteFailure("op SmartPull", errors.New("git pull failed (exit 1): rejected"))
	if got := settingsMenuLabel(m, idx); !strings.Contains(got, "1") {
		t.Fatalf("label should reflect count 1: %q", got)
	}
}

func TestSettingsErrorsViewerOpensAndCloses(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	observ.ResetFailures()
	observ.NoteFailure("op SmartPull", errors.New("git pull failed (exit 1): rejected"))

	m := settingsErrModel()
	u, _ := m.Update(keyMsg(","))
	m = u.(Model)
	// Select the Session errors row, then open it.
	layerOf[*settingsPopup](m).menuSel = errorsMenuIndex(t)
	u, _ = m.Update(keyMsg("enter"))
	m = u.(Model)

	p := layerOf[*settingsPopup](m)
	if p == nil || !p.errorsView {
		t.Fatal("enter on the Session errors row should open the viewer")
	}
	if out := m.View(); !strings.Contains(out, "Session errors") || !strings.Contains(out, "rejected") {
		t.Fatalf("viewer should list the failure:\n%s", out)
	}
	// esc returns to the menu (does not close the popup).
	u, _ = m.Update(keyMsg("esc"))
	m = u.(Model)
	p = layerOf[*settingsPopup](m)
	if p == nil || p.errorsView {
		t.Fatal("esc should return from the viewer to the menu, keeping the popup open")
	}
}

func TestSettingsErrorsEmptyState(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	observ.ResetFailures()
	m := settingsErrModel()
	u, _ := m.Update(keyMsg(","))
	m = u.(Model)
	layerOf[*settingsPopup](m).menuSel = errorsMenuIndex(t)
	u, _ = m.Update(keyMsg("enter"))
	m = u.(Model)
	if out := m.View(); !strings.Contains(out, "no errors this session") {
		t.Fatalf("empty viewer should show the empty state:\n%s", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run "TestSettingsErrors" -v`
Expected: FAIL — `settingsMenuErrors` undefined / `errorsView` undefined.

- [ ] **Step 3a: Struct field + menu entry**

In `internal/tui/settings_popup.go`, add `observ` to the import block:

```go
	"github.com/homeend/gigagit/internal/agentinit"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/observ"
```

Add the screen field to `settingsPopup` (alongside `picker bool`):

```go
	picker     bool // false = menu screen, true = agent picker
	errorsView bool // true = session-errors viewer screen
```

Add the menu constant and extend the menu slice:

```go
	settingsMenuOpLog    = "Operation log"
	settingsMenuErrors   = "Session errors"
)

// settingsMenu is the top-level menu order.
var settingsMenu = []string{settingsMenuAgents, settingsMenuIdentity, settingsMenuPrefixes, settingsMenuOpLog, settingsMenuErrors}
```

- [ ] **Step 3b: Dynamic label**

In `settingsMenuLabel`, before the final `return settingsMenu[i]`, add the errors branch:

```go
	if settingsMenu[i] == settingsMenuErrors {
		path := defaultErrLogPath()
		if path == "" {
			path = "(no state dir)"
		}
		n := len(observ.SessionFailures())
		if n == 0 {
			return settingsMenuErrors + ": none — " + path
		}
		return fmt.Sprintf("%s: %d — %s", settingsMenuErrors, n, path)
	}
```

- [ ] **Step 3c: Key handling in `update`**

Replace the `tea.KeyEsc` case (currently handling `picker`) with one that also handles the viewer:

```go
	case tea.KeyEsc:
		if p.errorsView {
			p.errorsView = false
			return m, nil
		}
		if p.picker {
			p.picker = false
			return m, nil
		}
		m = m.popLayer()
		return m, nil
```

Change the menu-navigation guard from `if !p.picker {` to exclude the viewer too:

```go
	if !p.picker && !p.errorsView {
```

Inside that block's `tea.KeyEnter` switch, add the new case:

```go
			case settingsMenuErrors:
				p.errorsView = true
				p.sel = 0
				p.hscroll = 0
				return m, nil
```

Immediately AFTER that menu block (after its closing `}` that ends `if !p.picker && !p.errorsView { ... }`), add a viewer-navigation block before the picker switch:

```go
	if p.errorsView {
		fs := observ.SessionFailures()
		switch msg.Type {
		case tea.KeyUp:
			if p.sel > 0 {
				p.sel--
			}
		case tea.KeyDown:
			if p.sel < len(fs)-1 {
				p.sel++
			}
		}
		return m, nil
	}
```

- [ ] **Step 3d: Rendering in `box`**

In `box`, replace the `if !p.picker { ... } else { ... }` block with a three-way form. The menu and picker branches keep their existing bodies; add the `errorsView` branch first:

```go
	if p.errorsView {
		b.WriteString("Session errors\n\n")
		fs := observ.SessionFailures()
		if len(fs) == 0 {
			b.WriteString("  no errors this session\n")
		} else {
			wr := make([]winRow, len(fs))
			for i, e := range fs {
				prefix := "  "
				var st lipgloss.Style
				if i == p.sel {
					prefix, st = "> ", selectedRow
				}
				wr[i] = winRow{
					text:  fmt.Sprintf("%s%s  %s — %s", prefix, e.Time.Format("15:04:05"), e.Source, e.Detail),
					style: st,
				}
			}
			h := len(fs)
			if h > 12 {
				h = 12
			}
			for _, line := range renderWindow(wr, winOpts{w: textW, h: h, mode: p.mode, anchor: p.sel, hscroll: p.hscroll}) {
				b.WriteString(line + "\n")
			}
		}
		if path := defaultErrLogPath(); path != "" {
			b.WriteString("\nfull history: " + path + "\n")
		}
		b.WriteString("\n[z] mode  [esc] back")
	} else if !p.picker {
		b.WriteString("Settings\n\n")
		for i := range settingsMenu {
			prefix := "  "
			if i == p.menuSel {
				prefix = "> "
			}
			b.WriteString(prefix + settingsMenuLabel(m, i) + "\n")
		}
		b.WriteString("\n[↑/↓] select  [enter] open/toggle  [esc] close")
	} else {
		b.WriteString("Set up agent skills\n\n")
		// ... unchanged existing picker body ...
	}
```

(Keep the existing picker body verbatim inside the final `else`.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run "TestSettingsErrors" -v`
Expected: PASS.

Then the whole TUI package: `go test ./internal/tui/`
Expected: ok (existing settings tests still pass — the menu gained one row but no existing index assertions are hardcoded against length).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/settings_popup.go internal/tui/settings_errors_test.go
git commit -m "feat(tui): Settings 'Session errors' row + read-only viewer"
```

---

### Task 5: Docs + full verification

**Files:**
- Modify: `CHANGELOG.md`
- Modify: `CLAUDE.md` (package map row for `observ`)

- [ ] **Step 1: Update CHANGELOG.md**

Add an entry under the current unreleased/top section (match the file's existing style):

```markdown
- Session error log: every git operation that fails (returns an error to a
  frontend) is recorded to an always-on `errors.log` in the gg state dir, and a
  new Settings (`,`) → "Session errors" entry shows the current session's
  failures. Control-flow probes (e.g. `merge-base --is-ancestor`) and user
  cancellations are not recorded.
```

- [ ] **Step 2: Update CLAUDE.md package map**

In the `observ` row of the package-map table, append a sentence noting the new seam:

```
Also a process-global **failure seam** (`NoteFailure`/`SetFailureSink`/`SessionFailures`): `domain` records every genuine, frontend-surfaced failure (each `query` + `Execute` error path, excluding cancellation) to a bounded session ring and an always-on `errors.log` (TUI-wired), surfaced by the Settings → "Session errors" viewer.
```

- [ ] **Step 3: Full verification**

Run: `./test.sh`
Expected: vet + gofmt clean, unit tests pass, e2e passes.

If `gofmt` flags anything: `gofmt -w internal/observ/ internal/domain/ internal/tui/ cmd/gg/`

- [ ] **Step 4: Commit**

```bash
git add CHANGELOG.md CLAUDE.md
git commit -m "docs: changelog + package map for session error log"
```

- [ ] **Step 5: Build a test binary for manual check**

Run: `go build -o ./gg ./cmd/gg`
Then report the absolute worktree path of `./gg` to the user for manual TUI testing (trigger a failing op — e.g. a push with no remote — then open `,` → Session errors).

---

## Self-Review

**Spec coverage:**
- AC#1 (failed pull/push/merge/read recorded): Task 2 (`Execute` + `query` hooks) + Task 2 tests (`TestExecuteFailureRecorded`, `TestQueryFailureRecorded`). ✓
- AC#2 (control-flow probes not recorded): structurally guaranteed — capture is at the domain boundary, tolerant verbs return nil error so `NoteFailure` is never reached; `gitexec` is untouched (Global Constraints). ✓
- AC#3 (cancellation not recorded): `NoteFailure` filters `context.Canceled`/`DeadlineExceeded`; tested in Task 1 (`TestNoteFailureIgnoresNilAndCancellation`) and Task 2 (`TestExecuteCancellationNotRecorded`). ✓
- AC#4 (durable global file in user dir): Task 3 (`errors.log` beside `operations.log`, always-on sink). ✓
- AC#5 (Settings viewer of current session): Task 4 (menu row + viewer). ✓

**Placeholder scan:** No TBD/TODO; every code step has complete code. The one "unchanged existing picker body" reference points at code the engineer is editing in-place (not asked to reproduce), and is explicitly marked keep-verbatim. ✓

**Type consistency:** `FailureEntry{Time,Source,Detail}`, `NoteFailure(string,error)`, `SetFailureSink(io.Writer)`, `SessionFailures() []FailureEntry`, `ResetFailures()`, `defaultErrLogPath() string`, `OpenErrorLog() (*os.File,string,error)`, `settingsMenuErrors`, `settingsPopup.errorsView` — names identical across all tasks and tests. Sources `"query "+key` and `label` (`"op "+OpName`) match the test expectations `"query status"` / `"op failOp"`. ✓

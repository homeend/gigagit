# Agent Names Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
> **gigagit rule:** NO implementer subagents — this session executes every task itself (CLAUDE.md). A read-only review subagent at the end is fine.

**Goal:** An optional name typed after picking an agent in Start agent (TUI + gg web), shown as `Claude (yolo) [viewer]` wherever the session is shown, with alt+↓ recall of names used before in this repo.

**Architecture:** `agentsession.Info` gains `Name` beside `Label`, with one `Title()` for display. The name travels on `domain.SpawnRecord.Name` (TUI, hosted web) / `domain.SessionStartRequest.Name` (web) into `StartSpec.Name`, cleaned once by `domain.CleanAgentName`. Remembered names are a new ring `"agentname"` in the existing per-repo search-history store; the TUI reuses `recallUpdate`/`recordSearch`, the web reads the ring through `/api/session-commands` and fills a `<datalist>`.

**Tech Stack:** Go 1.26, Bubble Tea, vanilla JS (gg web static), TOML i18n bundles.

**Spec:** `docs/superpowers/specs/2026-10-07-agent-names-design.md`

**Worktree:** `/work/gigagit/.claude/worktrees/agent-names` (branch `feat/agent-names`). Every command below runs there (`cd` first — the shell cwd resets to the main checkout).

## Global Constraints

- Row format: `Label [Name]` when named, `Label` alone when unnamed — `Claude (yolo) [viewer]`.
- Name cleaning: trim; control characters (incl. `\n`, `\r`, `\t`) removed; at most 40 runes, cut with no ellipsis; whitespace-only → `""`.
- Remembered names: per repository, ring scope `"agentname"`, size cap = `[ui] search_history_size` (existing).
- `Label` keeps meaning the command (tool identity): `agent_list`'s `tool`, task rows `Label · key`, the registry's fallback agent name are NOT changed to the title.
- No `name` input on `agent_start` / `gg agent start`; terminals are never named; no rename after start.
- Every new user-visible TUI string goes through `i18n.T` with a literal key present in ja/ko/zh/ru bundles.
- `internal/tui` and `internal/cli` never import `internal/git`; display helpers they need come from `domain`.
- Popup key hints have a blank line above them.

## Review Focus

1. A pasted multi-line or tab-laden name — expect one clean line on the row, never a broken row (Task 2 table + Task 4 paste test).
2. One configured, unapproved command: approve → name → start; esc on the name step closes the popup, nothing starts (Task 4).
3. Web name phase: typing `j`, `k`, `1`–`9` goes into the input, never moves/picks a row (Task 7 `dialogStep` table).
4. Web start that returns `needs_approval` after the name step re-posts WITH the typed name (Task 7: `run` carries `d.name`; wiring check).
5. The recall dropdown never outlives the popup: starting from the name step resets recall (Task 4 asserts `!m.recallOpen` after start).

---

### Task 1: `agentsession` — `Name` and `Title()`

**Files:**
- Modify: `internal/agentsession/types.go` (Info, StartSpec)
- Modify: `internal/agentsession/session.go:103` (Info construction)
- Test: `internal/agentsession/title_test.go` (create)

**Interfaces:**
- Produces: `agentsession.Info.Name string`, `agentsession.StartSpec.Name string`, `func Title(label, name string) string`, `func (i Info) Title() string`.

- [ ] **Step 1: Write the failing test** — `internal/agentsession/title_test.go`

```go
package agentsession

import "testing"

func TestTitle(t *testing.T) {
	t.Parallel()
	cases := []struct{ label, name, want string }{
		{"Claude (yolo)", "viewer", "Claude (yolo) [viewer]"},
		{"Claude", "", "Claude"},
		{"Junie", "worker 2", "Junie [worker 2]"},
	}
	for _, c := range cases {
		if got := Title(c.label, c.name); got != c.want {
			t.Errorf("Title(%q, %q) = %q, want %q", c.label, c.name, got, c.want)
		}
		if got := (Info{Label: c.label, Name: c.name}).Title(); got != c.want {
			t.Errorf("Info.Title() = %q, want %q", got, c.want)
		}
	}
}
```

Add to the same file a start test proving the spec's name reaches `Info` (sh-based; skip on Windows like the package's other start tests — copy their skip/cleanup idiom from `session_test.go`):

```go
func TestStartCarriesName(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	m := NewManager()
	t.Cleanup(func() { m.KillAll(context.Background()) })
	s, err := m.Start(StartSpec{Label: "Sh", Name: "viewer", Dir: t.TempDir(), Argv: []string{"sh", "-c", "sleep 30"}, Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	if in := s.Info(); in.Name != "viewer" || in.Title() != "Sh [viewer]" {
		t.Fatalf("info %+v", in)
	}
}
```
(imports: `context`, `runtime`, `testing`.)

- [ ] **Step 2: Run to verify it fails**

Run: `cd /work/gigagit/.claude/worktrees/agent-names && go test ./internal/agentsession/ -run 'TestTitle|TestStartCarriesName'`
Expected: FAIL — `undefined: Title`, `unknown field Name`.

- [ ] **Step 3: Implement**

`types.go` — in `Info`, after `Label`:
```go
	Name    string    // the user's name for this session ("" = unnamed), e.g. "viewer"
```
In `StartSpec`, change `Label, AgentID, Repo, Dir string` to keep it and add below:
```go
	Name                      string // the user's name for the session; "" = unnamed
```
Append to `types.go`:
```go
// Title is how a session is shown: its label, then the user's name in
// brackets when it has one — "Claude (yolo) [viewer]".
func Title(label, name string) string {
	if name == "" {
		return label
	}
	return label + " [" + name + "]"
}

// Title is the session's display title (see Title).
func (i Info) Title() string { return Title(i.Label, i.Name) }
```
`session.go:103` — add `Name: spec.Name,` to the `Info{...}` literal after `Label: spec.Label,`.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/agentsession/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/agentsession/types.go internal/agentsession/session.go internal/agentsession/title_test.go
git commit -F <msgfile>   # "feat(agentsession): a session's Name and its display Title"
```
(`gg commit` has no `-F`; write the message + the Co-Authored-By / Claude-Session trailers to a file in the scratchpad.)

---

### Task 2: `domain` — clean, carry and remember the name

**Files:**
- Create: `internal/domain/agentname.go`
- Modify: `internal/domain/agentspawn.go:18-23` (SpawnRecord.Name)
- Modify: `internal/domain/sessions.go` (`StartSession` 169, `startSessionPrompt` 172-187, `StartAgentSession` 194-213, `startLine` 247-255, `SessionStartRequest` 327-332)
- Modify: `internal/domain/tasks.go:570` (pass `""` to `startLine`)
- Test: `internal/domain/agentname_test.go` (create)

**Interfaces:**
- Consumes: `agentsession.StartSpec.Name`, `agentsession.Title` (Task 1).
- Produces:
  - `const AgentNameHistoryScope = "agentname"`
  - `func CleanAgentName(s string) string`
  - `func SessionTitle(label, name string) string` (re-export of `agentsession.Title` for tui/web/cli)
  - `SpawnRecord.Name string`
  - `SessionStartRequest.Name string`
  - `startSessionPrompt(ctx, tc, worktreeDir, cwd, cols, rows, env, prompt, name string)`, `startLine(ctx, mgr, label, name, agentID, line, worktreeDir, cwd, cols, rows, env)` (unexported).

- [ ] **Step 1: Write the failing tests** — `internal/domain/agentname_test.go`

```go
package domain

import (
	"context"
	"strings"
	"testing"
)

func TestCleanAgentName(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("é", 45)
	cases := []struct{ in, want string }{
		{"  viewer  ", "viewer"},
		{"", ""},
		{" \t \n ", ""},
		{"line one\nline two", "line oneline two"},
		{"tab\there", "tabhere"},
		{"bell\x07\x1b[31mred", "bell[31mred"},
		{long, strings.Repeat("é", 40)},
		{"  " + strings.Repeat("a", 39) + "  b", strings.Repeat("a", 39) + " "},
	}
	for _, c := range cases {
		if got := CleanAgentName(c.in); got != c.want {
			t.Errorf("CleanAgentName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
```
The last case pins the order: trim → drop controls → cut to 40 runes (the cut may leave a trailing space; it is NOT re-trimmed — simple and stable). If you prefer re-trimming after the cut, change the expectation to `strings.Repeat("a", 39)` and implement accordingly — pick one and keep test and code in agreement.

```go
func TestStartAgentSessionCarriesTheName(t *testing.T) {
	useSessions(t)
	main, svc := newRealRepo(t)
	s, _, err := svc.StartAgentSession(context.Background(), sleeper(), main, "", 80, 24, nil, "", SpawnRecord{Name: "  viewer\n"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if in := s.Info(); in.Name != "viewer" || in.Title() != "Sleeper [viewer]" {
		t.Fatalf("info %+v", in)
	}
	s2, _, err := svc.StartAgentSession(context.Background(), sleeper(), main, "", 80, 24, nil, "", SpawnRecord{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if in := s2.Info(); in.Name != "" || in.Title() != "Sleeper" {
		t.Fatalf("unnamed info %+v", in)
	}
}

func TestSessionTitleReexport(t *testing.T) {
	t.Parallel()
	if got := SessionTitle("Junie", "worker"); got != "Junie [worker]" {
		t.Fatal(got)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/domain/ -run 'TestCleanAgentName|TestStartAgentSessionCarriesTheName|TestSessionTitleReexport'`
Expected: FAIL — undefined `CleanAgentName`, `SessionTitle`, unknown field `Name` in `SpawnRecord`.

- [ ] **Step 3: Implement**

`internal/domain/agentname.go`:
```go
package domain

import (
	"strings"
	"unicode"

	"github.com/homeend/gigagit/internal/agentsession"
)

// AgentNameHistoryScope is the per-repo search-history ring that remembers
// the names typed in Start agent (TUI alt+↓ recall, the web's datalist).
const AgentNameHistoryScope = "agentname"

// maxAgentNameRunes caps a session name: a row label, not a description.
const maxAgentNameRunes = 40

// CleanAgentName is the one normalisation of a user-typed session name:
// trimmed, control characters (newlines and tabs included) dropped, cut to
// 40 runes. Whitespace only is no name.
func CleanAgentName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	if r := []rune(s); len(r) > maxAgentNameRunes {
		s = string(r[:maxAgentNameRunes])
	}
	return s
}

// SessionTitle is how a session is shown: "Claude (yolo) [viewer]", or the
// bare label when unnamed (agentsession.Title, for frontends).
func SessionTitle(label, name string) string { return agentsession.Title(label, name) }
```

`agentspawn.go` — `SpawnRecord` gains:
```go
	Name     string // the user's name for the session ("" = unnamed); cleaned by StartAgentSession
```

`sessions.go`:
- `StartSession` (line 169): `return s.startSessionPrompt(ctx, tc, worktreeDir, cwd, cols, rows, env, "", "")`
- `startSessionPrompt` signature gains a trailing `name string`; its `startLine` call becomes `s.startLine(ctx, Sessions(), tc.Name, name, agentIDFor(tc), resolved, worktreeDir, cwd, cols, rows, env)`.
- `StartAgentSession`: before the start, `rec.Name = CleanAgentName(rec.Name)`; call `s.startSessionPrompt(ctx, tc, dir, cwd, cols, rows, env, prompt, rec.Name)`. Update its doc comment: "rec.Name is the user's name for the session (cleaned here)."
- `startLine(ctx, mgr, label, name, agentID, line, worktreeDir, cwd string, cols, rows int, env []string)`; add `Name: name,` to its `StartSpec` literal.
- `SessionStartRequest` gains `Name string // the user's name for an agent ("" = unnamed); ignored for a terminal`.

`tasks.go:570`: `t.spec.Svc.startLine(ctx, m.sessionMgr(), label, "", t.spec.AgentID, in.Command, …)` (insert `""` after `label`).

- [ ] **Step 4: Run to verify it passes**

Run: `go build ./... && go test ./internal/domain/ -run 'TestCleanAgentName|TestStartAgentSession|TestSessionTitle|TestSpawn'`
Expected: PASS (build proves every `startLine` caller is updated).

- [ ] **Step 5: Commit** — "feat(domain): clean and carry an agent session's name".

---

### Task 3: agent-facing and cross-process surfaces carry the name

**Files:**
- Modify: `internal/domain/session_tracker.go:66`, `internal/domain/agentreport.go:107` (notices use the title)
- Modify: `internal/domain/agenttour.go:58` (tour title)
- Modify: `internal/domain/agentverbs.go:25-40,67` (`AgentEntry.Name`)
- Modify: `internal/sessionreg/sessionreg.go:21-28` (`Entry.Name`)
- Modify: `internal/domain/sessionpublish.go:65-68,157-163,176` (registry snapshot + `AgentHostSession.Name`)
- Modify: `internal/cli/agent.go:186-191` (list tail) and `:219-226` (hosts)
- Modify: `internal/agentskill/using-gg.md:1121` + `internal/agentskill/agentskill.go:25` (`Version = 142`)
- Test: `internal/domain/agentname_test.go` (extend), `internal/cli/agent_test.go` (extend)

**Interfaces:**
- Consumes: `Info.Title()`, `SpawnRecord.Name` (Tasks 1-2).
- Produces: `AgentEntry.Name` (`json:"name,omitempty"`), `sessionreg.Entry.Name` (`json:"name,omitempty"`), `AgentHostSession.Name` (`json:"name,omitempty"`); `ActivityNotice.Label` now holds the title.

- [ ] **Step 1: Write the failing tests** — append to `internal/domain/agentname_test.go`:

```go
func TestNameReachesAgentListAndRegistry(t *testing.T) {
	useSessions(t)
	main, svc := newRealRepo(t)
	s, _, err := svc.StartAgentSession(context.Background(), sleeper(), main, "", 80, 24, nil, "", SpawnRecord{Name: "viewer"}, "")
	if err != nil {
		t.Fatal(err)
	}
	full := FullSessionID(s.Info().ID)
	var found bool
	for _, e := range AgentList("") {
		if e.ID == full {
			found = true
			if e.Name != "viewer" || e.Tool != "Sleeper" || e.Label != "Sleeper" {
				t.Fatalf("entry %+v — tool/label stay the command, name is separate", e)
			}
		}
	}
	if !found {
		t.Fatal("session not listed")
	}
	reg := snapshotRegistry("", "", "")
	if len(reg.Sessions) != 1 || reg.Sessions[0].Name != "viewer" || reg.Sessions[0].Label != "Sleeper" {
		t.Fatalf("registry %+v", reg.Sessions)
	}
}
```

Append to `internal/cli/agent_test.go` — reuse `agentEnvFor` but with a named session. Add a variant start right in the test:

```go
func TestAgentListShowsTheName(t *testing.T) {
	dir, _ := agentEnvFor(t, nil)
	tc := config.ToolCommand{Category: "session", Name: "Sh", Mode: "session", Command: "sh -c 'sleep 600'"}
	s, _, err := domain.Open(dir).StartAgentSession(context.Background(), tc, dir, "", 80, 24, nil, "", domain.SpawnRecord{Name: "viewer"}, "")
	if err != nil {
		t.Fatal(err)
	}
	code, out, _ := runAgentCLI(t, dir, "", "list")
	full := domain.FullSessionID(s.Info().ID)
	var line string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, full) {
			line = l
		}
	}
	if code != 0 || !strings.Contains(line, "name viewer") {
		t.Fatalf("list = %d, row %q", code, line)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/domain/ -run TestNameReachesAgentListAndRegistry && go test ./internal/cli/ -run TestAgentListShowsTheName`
Expected: FAIL — unknown field `Name` on `AgentEntry` / `sessionreg.Entry`; CLI row lacks `name viewer`.

- [ ] **Step 3: Implement**

- `session_tracker.go:66` and `agentreport.go:107`: `Label: info.Title(),` (update the `ActivityNotice.Label` field comment in `session_states.go:58` to `// the session's title (label + [name])`).
- `agenttour.go:58`: `tourTitle(kind, info.Title(), …)`.
- `agentverbs.go`: add to `AgentEntry` after `Label`: `Name     string    \`json:"name,omitempty"\``; in `AgentList` add `Name: in.Name,` to the literal.
- `sessionreg.go` `Entry`: after `Label`: `Name    string \`json:"name,omitempty"\` // the user's name for the session`.
- `sessionpublish.go`: `snapshotRegistry` literal gains `Name: in.Name,`; `AgentHostSession` gains `Name  string \`json:"name,omitempty"\``; `liveAgentHostsIn` copies `Name: e.Name`. Leave the `agent = e.Label` fallback (line 124) alone — identity, not display.
- `cli/agent.go` list (around 186): after the `Parent` tail entry add
  ```go
  		if a.Name != "" {
  			tail = append(tail, "name "+a.Name)
  		}
  ```
  (place it FIRST in `tail`, before `parent`, so it reads `name viewer  parent …  mine`).
  Hosts (around 222): after computing `name`, `if s.Name != "" { name += " [" + s.Name + "]" }`.
- `using-gg.md` in the `agent_list` bullet (line ~1121), after "`mine` marks the agents you started.": add "`name` is the user's name for the session when it was started with one (Start agent's optional name; `tool`/`label` stay the command)." Bump `agentskill.Version` to `142`.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/domain/ ./internal/cli/ ./internal/sessionreg/ ./internal/agentskill/`
Expected: PASS (the agentskill golden/version tests may need the bump only).

- [ ] **Step 5: Commit** — "feat(agents): agent_list, the session registry and notices carry the name".

---

### Task 4: TUI — the name step in Start agent

**Files:**
- Modify: `internal/tui/agent_start_popup.go` (stage, struct, choose, approve enter, start, update, render)
- Modify: `internal/tui/search_history.go:19-27` (`scopeAgentName`)
- Modify: `internal/tui/websession.go:140` (hosted start passes the name, records it)
- Modify: `internal/i18n/lang/{ja,ko,zh,ru}.toml` (4 new keys, appended after the `"Start this agent?  (%s)"` line)
- Test: `internal/tui/agent_start_popup_test.go` (update `TestStartAgentApproveThenStartsConsole`, add new tests)

**Interfaces:**
- Consumes: `domain.CleanAgentName`, `domain.SessionTitle`, `domain.AgentNameHistoryScope`, `domain.SpawnRecord.Name`, `domain.SessionStartRequest.Name` (Task 2); `m.recallUpdate`, `m.recordSearch`, `m.recallReset` (existing, `search_history.go`); `newTextField`, `viewField`, `textfield.HandleEditKey` (existing).
- Produces: `stageName` stage; `scopeAgentName` const; `(*agentStartPopup).askName(m Model) (tea.Model, tea.Cmd)`; `(*agentStartPopup).start(m Model, name string)`.

- [ ] **Step 1: Write the failing tests** — in `agent_start_popup_test.go`

Update `TestStartAgentApproveThenStartsConsole`: after the approving `enter`, the popup is now on the name step; type a name and press enter:
```go
	mm, cmd := m.Update(keyMsg("enter"))
	m = mm.(Model)
	if p, _ := m.topLayer().(*agentStartPopup); p == nil || p.stage != stageName || cmd != nil {
		t.Fatal("approving must move to the name step, starting nothing yet")
	}
	m = typeText(t, m, "viewer")
	mm, cmd = m.Update(keyMsg("enter"))
	m = mm.(Model)
	if m.topLayer() != nil || cmd == nil {
		t.Fatal("enter on the name must close the popup and start the session")
	}
	// cmd is a tea.Batch(record, start): run it to completion.
	m = pumpAll(t, m, cmd)
	…
	s, _ := m.consoleSession()
	if in := s.Info(); in.Label != "B" || in.Name != "viewer" {
		t.Fatalf("started %+v, want B named viewer", in)
	}
```
(`pumpAll(t, m, cmd) Model` lives in `steer_nav_test.go:683`; it runs batched cmds and feeds their msgs back.)

New tests (same file):
```go
// twoCmdModel: a model with two approved session commands.
func twoCmdModel(t *testing.T) Model {
	t.Helper()
	m := loadedModel(t)
	m.width, m.height = 120, 40
	startTestSession(t, m, `sleep 5`)
	m.cfg.Tools.Command = []config.ToolCommand{
		{Category: "session", Name: "A", Mode: "session", Command: "sleep 5"},
		{Category: "session", Name: "B", Mode: "session", Command: "sleep 6"},
	}
	m.rememberToolApproval("sleep 5")
	m.rememberToolApproval("sleep 6")
	return m
}

func TestStartAgentNameStepEmptyEnterStartsUnnamed(t *testing.T) {
	m := twoCmdModel(t)
	m, _ = m.startAgentFor(m.currentWorktree)
	mm, _ := m.Update(keyMsg("1"))
	m = mm.(Model)
	p, _ := m.topLayer().(*agentStartPopup)
	if p == nil || p.stage != stageName || p.pick.Name != "A" {
		t.Fatalf("an approved pick goes to the name step: %+v", p)
	}
	if v := m.View(); !strings.Contains(v, "Name this agent (optional)") || !strings.Contains(v, "[alt+↓] recent names") {
		t.Fatalf("name step not on screen:\n%s", v)
	}
	mm, cmd := m.Update(keyMsg("enter"))
	m = pumpAll(t, mm.(Model), cmd)
	s, _ := m.consoleSession()
	if in := s.Info(); in.Label != "A" || in.Name != "" {
		t.Fatalf("started %+v, want A unnamed", in)
	}
	if len(m.searchHist[scopeAgentName]) != 0 {
		t.Fatal("an empty name is not remembered")
	}
}

func TestStartAgentNameStepEsc(t *testing.T) {
	m := twoCmdModel(t)
	m, _ = m.startAgentFor(m.currentWorktree)
	mm, _ := m.Update(keyMsg("2"))
	m = mm.(Model)
	mm, _ = m.Update(keyMsg("esc"))
	m = mm.(Model)
	if p, _ := m.topLayer().(*agentStartPopup); p == nil || p.stage != stageChoose {
		t.Fatal("esc on the name step returns to the chooser when there are several commands")
	}
	// One command (unapproved): approve → name; esc closes, nothing starts.
	m.cfg.Tools.Command = []config.ToolCommand{{Category: "session", Name: "Solo", Mode: "session", Command: "sleep 7"}}
	m = m.popLayer()
	m, _ = m.startAgentFor(m.currentWorktree)
	if p, _ := m.topLayer().(*agentStartPopup); p == nil || p.stage != stageApprove {
		t.Fatalf("a single unapproved command asks approval first: %+v", p)
	}
	mm, _ = m.Update(keyMsg("enter"))
	m = mm.(Model)
	if p, _ := m.topLayer().(*agentStartPopup); p == nil || p.stage != stageName {
		t.Fatal("approval leads to the name step")
	}
	before := len(domain.Sessions().List())
	mm, cmd := m.Update(keyMsg("esc"))
	m = mm.(Model)
	if m.topLayer() != nil || cmd != nil || len(domain.Sessions().List()) != before {
		t.Fatal("esc on a single command's name step closes the popup and starts nothing")
	}
}

func TestStartAgentNameRecallAndCleaning(t *testing.T) {
	m := twoCmdModel(t)
	m.searchHist = map[string][]string{scopeAgentName: {"worker", "viewer"}}
	m, _ = m.startAgentFor(m.currentWorktree)
	mm, _ := m.Update(keyMsg("1"))
	m = mm.(Model)
	mm, _ = m.Update(keyMsg("alt+down"))
	m = mm.(Model)
	mm, _ = m.Update(keyMsg("alt+down"))
	m = mm.(Model)
	if !m.recallOpen || !strings.Contains(m.View(), "viewer") {
		t.Fatal("alt+↓ opens the recent names")
	}
	mm, _ = m.Update(keyMsg("enter")) // fills the field
	m = mm.(Model)
	p, _ := m.topLayer().(*agentStartPopup)
	if p == nil || p.name.Value() != "viewer" || m.recallOpen {
		t.Fatalf("enter on a recalled name fills the field: %+v", p)
	}
	mm, cmd := m.Update(keyMsg("enter"))
	m = pumpAll(t, mm.(Model), cmd)
	if m.recallOpen {
		t.Fatal("the recall dropdown must not outlive the popup")
	}
	s, _ := m.consoleSession()
	if s.Info().Name != "viewer" || m.searchHist[scopeAgentName][0] != "viewer" {
		t.Fatalf("named %q, ring %v", s.Info().Name, m.searchHist[scopeAgentName])
	}
}

func TestStartAgentNamePasteIsOneLine(t *testing.T) {
	m := twoCmdModel(t)
	m, _ = m.startAgentFor(m.currentWorktree)
	mm, _ := m.Update(keyMsg("1"))
	m = mm.(Model)
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("rev\niewer\t"), Paste: true})
	m = mm.(Model)
	mm, cmd := m.Update(keyMsg("enter"))
	m = pumpAll(t, mm.(Model), cmd)
	s, _ := m.consoleSession()
	if got := s.Info().Name; got != "reviewer" {
		t.Fatalf("name %q, want reviewer", got)
	}
}
```
(Imports to add: `github.com/homeend/gigagit/internal/domain`.)

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/tui/ -run 'TestStartAgent'`
Expected: FAIL — undefined `stageName`, `scopeAgentName`, `p.name`.

- [ ] **Step 3: Implement**

`search_history.go` const block: `scopeAgentName = domain.AgentNameHistoryScope // Start agent's optional session name`.

`agent_start_popup.go`:
```go
const (
	stageDetecting agentStage = iota
	stageChoose
	stageApprove
	stageName // optional session name, alt+↓ recalls names used before
)

type agentStartPopup struct {
	stage    agentStage
	worktree string
	cmds     []config.ToolCommand
	sel      int
	pick     config.ToolCommand
	name     textfield
}
```
`choose`: replace `return p.start(m)` with `return p.askName(m)`.
Add:
```go
// askName moves to the optional name step for the picked command.
func (p *agentStartPopup) askName(m Model) (tea.Model, tea.Cmd) {
	p.stage, p.name = stageName, newTextField("")
	return m.recallReset(), nil
}
```
`start` becomes `start(m Model, name string)`:
```go
func (p *agentStartPopup) start(m Model, name string) (tea.Model, tea.Cmd) {
	m = m.recallReset().popLayer()
	…
	title := domain.SessionTitle(tc.Name, name)
	m.statusMsg = i18n.T("starting %s…", title)
	return m, func() tea.Msg {
		s, _, err := svc.StartAgentSession(context.Background(), tc, dir, cwd, cols, rows, env, url, domain.SpawnRecord{Name: name}, "")
		if err != nil {
			return agentStartedMsg{name: title, err: err}
		}
		return agentStartedMsg{id: s.Info().ID, name: title, inbox: inbox, note: note}
	}
}
```
`update` — `stageApprove` enter: `m.rememberToolApproval(p.pick.Command); nm, cmd := p.askName(m); return nm.(Model), cmd`. Add a case before `stageApprove`'s:
```go
	case stageName:
		if nm, nq, handled, _ := m.recallUpdate(scopeAgentName, msg, p.name.Value()); handled {
			p.name = newTextField(nq)
			return nm, nil
		} else {
			m = nm
		}
		switch msg.Type {
		case tea.KeyEsc:
			m = m.recallReset()
			if len(p.cmds) > 1 {
				p.stage = stageChoose
				return m, nil
			}
			return m.popLayer(), nil
		case tea.KeyEnter:
			name := domain.CleanAgentName(p.name.Value())
			var record tea.Cmd
			if name != "" {
				m, record = m.recordSearch(scopeAgentName, name)
			}
			nm, cmd := p.start(m, name)
			return nm.(Model), tea.Batch(record, cmd)
		default:
			p.name.HandleEditKey(msg) // spaces included
		}
		return m, nil
```
(Note the existing `key == "ctrl+c"` guard at the top of `update` stays first.)

`render` — add:
```go
	case stageName:
		esc := i18n.T("[enter] start  [alt+↓] recent names  [esc] back")
		if len(p.cmds) <= 1 {
			esc = i18n.T("[enter] start  [alt+↓] recent names  [esc] cancel")
		}
		lines = []string{
			i18n.T("Name this agent (optional)"), "",
			i18n.T("Start %s in %s", p.pick.Name, shortWorktreeName(p.worktree)), "",
			viewField("> ", p.name, true, textW), "",
			esc,
		}
```

`websession.go:140` (hosted page start): pass `domain.SpawnRecord{Name: req.Name}`; set `name = domain.SessionTitle(req.Command.Name, domain.CleanAgentName(req.Name))` for an agent (the `name` var feeds the status line). Where the handler builds the start cmd, also record a non-empty cleaned name in the TUI ring: `if n := domain.CleanAgentName(req.Name); n != "" && !req.Terminal { m, rec = m.recordSearch(scopeAgentName, n) }` and batch `rec` with the returned cmd (read the surrounding function to see how its cmd is returned — it returns `(m, start, "")`; return `tea.Batch(rec, start)` if the return type is `tea.Cmd`).

i18n — append after `"Start this agent?  (%s)" = …` in each bundle:

ja.toml
```toml
"Name this agent (optional)" = "このエージェントに名前を付ける（任意）"
"Start %s in %s" = "%s を %s で開始"
"[enter] start  [alt+↓] recent names  [esc] back" = "[enter] 開始  [alt+↓] 最近の名前  [esc] 戻る"
"[enter] start  [alt+↓] recent names  [esc] cancel" = "[enter] 開始  [alt+↓] 最近の名前  [esc] キャンセル"
```
ko.toml
```toml
"Name this agent (optional)" = "이 에이전트 이름 지정 (선택)"
"Start %s in %s" = "%s 시작 (%s)"
"[enter] start  [alt+↓] recent names  [esc] back" = "[enter] 시작  [alt+↓] 최근 이름  [esc] 뒤로"
"[enter] start  [alt+↓] recent names  [esc] cancel" = "[enter] 시작  [alt+↓] 최근 이름  [esc] 취소"
```
zh.toml
```toml
"Name this agent (optional)" = "为此代理命名（可选）"
"Start %s in %s" = "启动 %s（位于 %s）"
"[enter] start  [alt+↓] recent names  [esc] back" = "[enter] 启动  [alt+↓] 最近名称  [esc] 返回"
"[enter] start  [alt+↓] recent names  [esc] cancel" = "[enter] 启动  [alt+↓] 最近名称  [esc] 取消"
```
ru.toml
```toml
"Name this agent (optional)" = "Имя для этого агента (необязательно)"
"Start %s in %s" = "Запуск %s в %s"
"[enter] start  [alt+↓] recent names  [esc] back" = "[enter] запустить  [alt+↓] недавние имена  [esc] назад"
"[enter] start  [alt+↓] recent names  [esc] cancel" = "[enter] запустить  [alt+↓] недавние имена  [esc] отмена"
```
If a key already exists in a bundle (grep first: `grep -n '"Start %s in %s"' internal/i18n/lang/ja.toml`), do not duplicate it.

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/tui/ -run 'TestStartAgent|I18n|Vocab|MenuLabel|EngineProse' && go test ./internal/i18n/`
Expected: PASS (the AST gates prove the four bundles carry the keys).

- [ ] **Step 5: Commit** — "feat(tui): optional agent name in Start agent with alt+↓ recall".

---

### Task 5: TUI — show the title wherever a session is shown

**Files:**
- Modify: `internal/tui/worktree_sessions.go:53,57,59`
- Modify: `internal/tui/console.go:396,521,604,852`
- Modify: `internal/tui/console_scope.go:110`
- Modify: `internal/tui/model.go:2757`
- Modify: `internal/tui/steer_switch_ask.go:58`
- Modify: `internal/tui/agent_start_popup.go:346,360`
- Modify: `internal/tui/sessions_popup.go:53,89,91,338`
- Test: `internal/tui/agent_names_display_test.go` (create)

**Interfaces:**
- Consumes: `domain.SessionInfo.Title()` (Task 1 — `SessionInfo` aliases `agentsession.Info`).

- [ ] **Step 1: Write the failing test** — `internal/tui/agent_names_display_test.go`

```go
package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

func TestSessionRowsShowTheName(t *testing.T) {
	t.Parallel()
	named := domain.SessionInfo{ID: "zz-named", Label: "Claude (yolo)", Name: "viewer", Dir: "/r/wt", Repo: "r", State: domain.SessionRunning, Started: time.Now()}
	plain := domain.SessionInfo{ID: "zz-plain", Label: "Junie", Dir: "/r/wt", Repo: "r", State: domain.SessionExited}
	if got := sessionRowBody(named); !strings.HasPrefix(got, "└ ● Claude (yolo) [viewer]  ") {
		t.Fatalf("named row %q", got)
	}
	if got := sessionRowBody(plain); !strings.HasPrefix(got, "└ ○ Junie  ") {
		t.Fatalf("unnamed row %q", got)
	}
	if label, _, _ := consoleTitleParts(named); label != "Claude (yolo) [viewer]" {
		t.Fatalf("console title %q", label)
	}
	rows, _ := sessionsPopupRows([]domain.SessionInfo{named, plain}, "viewer")
	joined := strings.Join(rows, "\n")
	if !strings.Contains(joined, "Claude (yolo) [viewer]") || strings.Contains(joined, "Junie") {
		t.Fatalf("filtering on the name finds only the named session:\n%s", joined)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui/ -run TestSessionRowsShowTheName`
Expected: FAIL — rows show `Claude (yolo)` without `[viewer]`; the filter finds nothing.

- [ ] **Step 3: Implement** — replace `info.Label` with `info.Title()` at every listed line (and `s.Info().Label` → `s.Info().Title()` at `sessions_popup.go:338`). In `sessions_popup.go:53` the filter haystack becomes `info.Title()+" "+info.Dir+" "+info.Repo`; update the `sessionsPopupRows` doc comment ("on its title (label and name), worktree or repo"). Do NOT touch `terminal.go` (terminals are unnamed) or task rows.

Run `grep -n 'info.Label\|Info().Label' internal/tui/*.go | grep -v _test` afterwards: the only remaining hits must be identity uses (none expected in `tui`; if one appears, decide display vs identity and note it in the commit message).

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/tui/ -run 'TestSessionRows|Session|Console'`
Expected: PASS.

- [ ] **Step 5: Commit** — "feat(tui): session rows, console title and popups show the agent's name".

---

### Task 6: gg web server — names in, names out

**Files:**
- Modify: `internal/web/session_lifecycle.go` (`sessionStartReq` 121-128, `handleSessionStart` ~205, `handleSessionCommands` ~103-119)
- Modify: `internal/web/sessions_http.go:28-31,98-105` (`sessionWire.Name`, label = title)
- Test: `internal/web/session_lifecycle_test.go` (extend `sessionCommandsBody`, new tests)

**Interfaces:**
- Consumes: `domain.CleanAgentName`, `domain.AgentNameHistoryScope`, `domain.SessionStartRequest.Name`, `SpawnRecord.Name`, `Service.RecordSearch`, `Service.SearchHistoryAll`, `Info.Title()`.
- Produces: `GET /api/session-commands` → `"names": [string]` (newest first, `[]` when none); `POST /api/session-start` accepts `"name"`; session JSON `"label"` = title, `"name"` = raw name.

- [ ] **Step 1: Write the failing tests** — `session_lifecycle_test.go`

Extend `sessionCommandsBody` with `Names []string`. Add:
```go
func TestSessionStartWithANameRecordsIt(t *testing.T) {
	srv, root := lifecycleServer(t, shSessionTool)
	ts := serve(t, srv)
	code, body := postJSONAny(t, ts, "/api/session-start", startBody(root, `,"tool":"Shell","approve":true,"name":"  viewer\n"`))
	sess, _ := body["session"].(map[string]any)
	if code != http.StatusOK || sess["label"] != "Shell [viewer]" || sess["name"] != "viewer" {
		t.Fatalf("%d %v", code, body)
	}
	var cmds sessionCommandsBody
	if code := getJSON(t, ts, "/api/session-commands?worktree="+url.QueryEscape(root), &cmds); code != http.StatusOK || len(cmds.Names) == 0 || cmds.Names[0] != "viewer" {
		t.Fatalf("names = %d %v", code, cmds.Names)
	}
	// An unnamed start records nothing and keeps the bare label.
	code, body = postJSONAny(t, ts, "/api/session-start", startBody(root, `,"tool":"Shell"`))
	sess, _ = body["session"].(map[string]any)
	if code != http.StatusOK || sess["label"] != "Shell" || sess["name"] != nil {
		t.Fatalf("%d %v", code, body)
	}
}
```
In `TestSessionStartDelegatesToTheStarter`, add `,"name":"worker"` to the first post's body and assert `got.Name == "worker"`.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/web/ -run 'TestSessionStart|TestSessionCommands'`
Expected: FAIL — label `Shell`, no `name`, `names` empty, `got.Name` empty.

- [ ] **Step 3: Implement**

- `sessionStartReq` gains `Name string \`json:"name"\``.
- `handleSessionStart`: after `req := domain.SessionStartRequest{…}` set `if !q.Terminal { req.Name = domain.CleanAgentName(q.Name) }`. After a SUCCESSFUL start (where the handler writes the session back), `if req.Name != "" { svc.RecordSearch(r.Context(), domain.AgentNameHistoryScope, req.Name, cfg.UI.SearchHistorySize) }`.
- `startSessionHere` (line 176): pass `domain.SpawnRecord{Name: req.Name}`.
- `handleSessionCommands`: add `"names": names` to the `writeJSON` map where `names := svc.SearchHistoryAll(r.Context())[domain.AgentNameHistoryScope]; if names == nil { names = []string{} }`.
- `sessions_http.go`: `sessionWire` gains `Name string \`json:"name,omitempty"\`` after `Label`; in `sessionsWireWith` set `Label: info.Title(), Name: info.Name`; the task case becomes `w.Label = info.Title() + " · " + tk.Key`.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/web/ ./internal/tui/ -run 'Session|WebSession'`
Expected: PASS.

- [ ] **Step 5: Commit** — "feat(web): session start takes a name; session JSON shows the title".

---

### Task 7: gg web page — the name phase in the Start agent dialog

**Files:**
- Modify: `internal/web/static/sessions.js` (model `dialogStep`, dialog state, render, `run`, `onKey`, FOOT)
- Modify: `internal/web/static/style.css` (input/datalist look inside `#sessstart-box`, only if needed)
- Test: `internal/web/sessionsjs_test.go`

**Interfaces:**
- Consumes: `/api/session-commands` `names`, `/api/session-start` `name` (Task 6).
- Produces: dialog phase `"name"`; `dialogStep` returns `{phase:"name", sel}` for a pick, `{start: sel, approve, named: true}` from the name phase.

Flow: choose → (approved) **name** → start; choose → (unapproved) approve → **name** → start; a 403 `needs_approval` after the name step → approve → start with the stored name (`d.name`).

- [ ] **Step 1: Write the failing test** — replace the expectations in `TestSessionsModelJS` and add name-phase cases. New driver lines (append before `console.log`):

```js
const n = { phase: "name", cmds, sel: 0 };
r.push(JSON.stringify(dialogStep(n, "Enter")));              // start, approved
r.push(JSON.stringify(dialogStep({ ...n, sel: 1, approved: true }, "Enter"))); // approved in this dialog
r.push(JSON.stringify(dialogStep(n, "Escape")));             // back to the list
r.push(JSON.stringify(dialogStep({ ...n, cmds: [cmds[0]] }, "Escape"))); // one command: close
r.push(JSON.stringify(dialogStep(n, "j")), JSON.stringify(dialogStep(n, "2")), JSON.stringify(dialogStep(n, "ArrowDown"))); // typing is the input's
r.push(JSON.stringify(dialogStep(n, "Enter", true)));        // a held enter starts nothing
```
Existing expectation changes: `dialogStep(d, "Enter")` on an approved row → `{"sel":0,"phase":"name"}`; `dialogStep({...d, sel: 2}, "Enter")` → `{"sel":2,"phase":"name"}`; approve-phase Enter → `{"phase":"name","approved":true}`.
Name-phase expectations, in order: `{"start":0,"approve":false}` · `{"start":1,"approve":true}` · `{"phase":"choose"}` · `{"close":true}` · `{}` · `{}` · `{}` · `{}`.

Add wiring checks to `sessionsWiring`:
```go
	{"sessions.js", `<datalist id="sessstart-names">`, "the name input suggests the repo's remembered names"},
	{"sessions.js", "body.names", "the names come with the command list"},
	{"sessions.js", "name: d.name", "the start (and its re-post after approval) carries the typed name"},
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/web/ -run 'TestSessionsModelJS|TestSessionsJSIsWired'`
Expected: FAIL.

- [ ] **Step 3: Implement** — `sessions.js`

`dialogStep`:
```js
function dialogStep(d, key, repeat) {
  if (d.phase === "starting" || (repeat && key === "Enter")) return {};
  if (d.phase === "detecting") return key === "Escape" ? { close: true } : {};
  if (d.phase === "name") {
    if (key === "Enter") return { start: d.sel, approve: !!d.approved };
    if (key === "Escape") return d.cmds.length > 1 ? { phase: "choose" } : { close: true };
    return {}; // every other key is typing: the input has it
  }
  if (d.phase === "approve") {
    if (key === "Enter") return { phase: "name", approved: true };
    if (key === "Escape") return d.cmds.length > 1 ? { phase: "choose" } : { close: true };
    return {};
  }
  const pick = (i) => (d.cmds[i].approved ? { sel: i, phase: "name" } : { sel: i, phase: "approve" });
  …unchanged…
}
```
Dialog state gains `name: ""`, `approved: false` (reset `approved` and `name` when re-entering `choose`: in `apply`, `if (step.phase === "choose") Object.assign(dlg, { name: "", approved: false })`).

`render` — a `name` branch:
```js
  else if (dlg.phase === "name")
    body.innerHTML =
      `<div class="rnote">Start ${esc(cur.name)} in ${esc(wtLeaf(dlg.path))}</div>` +
      `<input id="sessstart-name" list="sessstart-names" maxlength="40" placeholder="optional — alt+↓ recent names" autocomplete="off">` +
      `<datalist id="sessstart-names">${(dlg.names || []).map((n) => `<option value="${esc(n)}">`).join("")}</datalist>`;
```
Title for the name phase: `"Name this agent (optional)"`. After setting innerHTML in the name phase: `const inp = $("sessstart-name"); inp.value = dlg.name; inp.addEventListener("input", () => (dlg.name = inp.value)); inp.focus();`.
FOOT gains `name: \`<span>enter start · alt+↓ recent names · esc back</span>\``.

`onKey` in `startAgent`:
```js
    onKey: (e) => {
      if (!dlg) return true;
      if (dlg.phase === "name" && e.key !== "Enter" && e.key !== "Escape") return false; // typing (and alt+↓) belongs to the input
      apply(dialogStep(dlg, e.key, e.repeat));
      e.preventDefault();
      return true;
    },
```
(Contract, `keys.js:118-122`: `onKey` returning `false` falls through to "a non-empty stack owns the keyboard" — no `preventDefault`, so the keystroke types into the focused input and reaches no global shortcut. Enter/Escape stay the dialog's.)

`startAgent`: after loading `body`, `d.names = body.names || []`.
`run(i, approve)`: post `{ worktree: d.path, tool: d.cmds[i].name, approve, name: d.name }`. On `needs_approval`: keep `d.name`, set `d.phase = "approve"` (unchanged) — its Enter now goes to the name phase with the typed name prefilled, then starts with approve. Keep the literal `name: d.name` in the post body (the wiring check greps it).

`apply`: unchanged except the `choose` reset above; `step.start !== undefined` still runs `run(step.start, step.approve)`.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/web/ -run 'Sessions'`
Expected: PASS.

- [ ] **Step 5: Browser check** (playwright per memory `playwright-web-verification`): rebuild `go build -o bin/gg ./cmd/gg` in the worktree, run `bin/gg web` against a scratch repo with a `sh -c 'sleep 600'` session command, hard-reload, open Start agent, assert the name input is **visible and focused**, type `j2viewer` (proves keys reach the input), press Enter, assert the session row text shows `Shell [j2viewer]`; reopen the dialog, focus the input, press alt+↓ and assert the datalist offers `j2viewer` (Chrome). Run the same against the UNFIXED main binary to see the check fail.

- [ ] **Step 6: Commit** — "feat(web): optional agent name in the Start agent dialog".

---

### Task 8: Docs, golden check, gates

**Files:**
- Modify: `CHANGELOG.md` (new top section "Agent names")
- Modify: `README.md` (~line 1490 Agent sessions: a paragraph on the name step + alt+↓)
- Modify: `docs/CLAUDE-details.md` (Agent sessions core section ~3872: `Name` vs `Label`, `Title()`, `SpawnRecord.Name`, ring `agentname`)

- [ ] **Step 1: Write docs**

CHANGELOG top section:
```markdown
## Agent names

### Added

- **Name an agent when you start it.** After picking a command in Start
  agent (TUI and gg web), an optional name step: type `viewer`, `worker`, …
  and the session shows as `Claude (yolo) [viewer]` on its Worktrees /
  Branches row, in the console title, the sessions popup (filterable by the
  name), alt+a and the activity notices. Enter on an empty field starts it
  unnamed, as before. alt+↓ recalls the names used before in this
  repository (the web's input offers them as suggestions). `agent_list` /
  `gg agent list` and the session registry carry `name`.
```
README: in "Agent sessions (embedded consoles)" after the first paragraph:
```markdown
After you pick the command, Start agent asks for an **optional name** — say
`viewer` or `worker` — shown after the label on the session's row
(`Claude (yolo) [viewer]`), in its console title and in the agent-sessions
popup. Enter on an empty field skips it; `alt+↓` lists names you used
before in this repository.
```

- [ ] **Step 2: Full gates**

Run: `./test.sh` then `./test.sh race` (race gate green ONLY on "all green" in its log — memory `race-gate-check-all-green`).
Expected: all green. Any e2e golden screen that renders the agent popup will change only if a scenario starts an agent; if a golden diff appears, inspect it, and `-update` only when the new screen is the intended one.

- [ ] **Step 3: Headless TUI look** (driving-tui-headless skill): `./tui-capture.sh` with a keyscript that opens a worktree row's `.` menu → Start agent → picks a command → types `viewer` → enter; read the snapshot: the name step shows the title, the field, a blank line, the hints; the sub-row reads `└ ● <label> [viewer]`.

- [ ] **Step 4: Commit** — "docs: agent names".

- [ ] **Step 5: Final review** — a read-only review subagent over `git diff main...feat/agent-names` (no edits, no test runs that write into the worktree). Then ASK the user before `gg merge`.

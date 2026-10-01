# Agent Spawn — Plan A (core) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans (this repo forbids implementer subagents — CLAUDE.md). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A running TUI hosts an MCP agent channel: an agent session gg started can, over authenticated loopback MCP, start a worker agent in a worktree (normal Start agent path, claim handed over), list/read/type into/kill agents, and a worker can read its brief.

**Architecture:** Domain owns a process-global spawn registry (tokens, parents, briefs, a slot counter), the agent verbs, `SpawnAgent` (checks → claim → `StartAgentSession` → handover) and the claim handover/revert. `internal/mcp` gets an `AgentHost` (streamable HTTP + go-sdk bearer middleware) with the six tools. The TUI owns the host through an `AgentHost` seam set by `cmd/gg`; `agent_start` enters the Bubble Tea loop by a request/reply channel (the `webhost.go` switch precedent) only to pick up the console size, child env and status line.

**Tech Stack:** Go 1.26, `github.com/modelcontextprotocol/go-sdk v1.6.1` (`mcp`, `auth`), Bubble Tea v1, real `git`/`sh` in tests.

**Spec:** `docs/superpowers/specs/2026-10-01-agent-spawn-design.md` (plan A = §9 "Plan A — core").

## Global Constraints

- Frontends (`tui`, `cli`, `mcp`, `web`) never import `agentsession`, `sessionreg`, `wtclaim`, `git` in non-test code (archtest `import_guard_test.go`).
- Every user-visible TUI string goes through `i18n.T` with a literal key present in `internal/i18n/lang/{ja,ko,zh,ru}.toml`.
- Engine/CLI/MCP prose is English protocol text, never translated.
- `[agents] spawn` and `[agents] max_spawned` are read from the GLOBAL config only (a repo value is ignored); `max_spawned` default 4, clamp 1..16.
- The kick-off line is the constant `domain.AgentKickoff = "You were started by gg as a worker agent. Call the gg MCP tool agent_task to read your task, then do it."` — never caller text on a command line.
- Tokens: 32 random bytes hex (`crypto/rand`), never written to disk; only AGENT sessions get one (never an Open-terminal shell).
- Any catalog template change bumps that family's `Version` and regenerates `internal/exttool/testdata/catalog_versions.golden` with `-update`.
- Tests that swap `domain.UseSessionManager` or call `t.Setenv` are NOT `t.Parallel()`.
- Commit with `git -C /work/gigagit/.claude/worktrees/agent-spawn …`; never `git add -A` (a built `bin/gg` may exist); end messages with the session's Co-Authored-By/Claude-Session lines.

## Spec corrections made while planning (ledger them as rulings at execution start)

- R1: the session-row upgrade rides the existing template-update offer (family `Version` bump) instead of a new preflight migration — the offer already covers stamped and unstamped built-in blocks (`domain/tooltemplates.go` `blockStatus`). Spec §3.1 updated.
- R2: the spawn registry prunes lazily on every read instead of a Subscribe goroutine. Spec §3.1 updated.
- R3: only manual Start agent sessions and spawned workers get a token; interactive AI-task sessions (`domain.Tasks()`) do not — they are not overseers. Cost if wrong: an AI task cannot spawn (add the env in `domain/tasks` later).
- R4: the approval lookup uses the CALLER repo's git common dir as the promptstate key (the TUI's own `toolRepoKey` would be wrong after a repo switch).

## Review Focus

1. Two overseers calling `agent_start` at once with one slot left — exactly one starts (slot counter, Task 5 test `TestSpawnSlotsAreRaceFree`).
2. A worker killed while its claim is fresh and another gg process lists worktrees — the claim reverts to the live parent, never vanishes (Task 4 `TestDeadWorkerClaimRevertsToParent` + `TestYoungClaimGraceKeepsUnlistedSession`).
3. A killed worker's token — every later tool call gets 401 (Task 7 `TestKilledSessionTokenIs401`).
4. `agent_send` text followed by Enter on a real shell — the text runs as one command line (Task 6 `TestAgentSendTypesAndEnters`).
5. A repo `.gg.toml` redefining an allow-listed session command — refused as unapproved (Task 6 `TestSpawnRefusesUnapprovedCommand`).

## File Structure

| File | Responsibility |
|------|----------------|
| `internal/config/config.go`, `template.go`, `config_test.go` | `[agents] spawn`, `max_spawned` (global-only) + setting docs |
| `internal/template/command.go`, `command_test.go` | `<prompt>`/`<prompt:FLAG>` token, `CmdCtx.Prompt`, `HasPromptSlot` |
| `internal/exttool/exttool.go`, `testdata/catalog_versions.golden` | session rows with `<prompt>`, family bumps |
| `internal/wtclaim/wtclaim.go` | `Claim.Parent` |
| `internal/domain/wtclaim.go`, `wtguards.go`, `wtclaim_test.go`, `wtguards_test.go` | handover, revert, young-claim grace, caller exemption |
| `internal/agentsession/session.go` | strip inherited agent-channel env |
| `internal/domain/agentspawn.go`, `agentspawn_test.go` | registry: tokens, records, reach, slots; `StartAgentSession` |
| `internal/domain/agentverbs.go`, `agentverbs_test.go` | list/screen/send/kill/task, key names, worktree resolution, `SpawnAgent` |
| `internal/mcp/agenthost.go`, `agenttools.go`, `agenthost_test.go` | the MCP host + tools |
| `internal/tui/agenthost.go`, `agenthost_test.go`, `agent_start_popup.go`, `run.go`, `model.go`, `terminal.go` | seam, request/reply, manual start gets a token, status line |
| `cmd/gg/main.go` | `tui.NewAgentHost = …` |
| `internal/i18n/lang/*.toml` | new status strings |

---

### Task 1: `[agents] spawn` and `max_spawned` (global only)

**Files:**
- Modify: `internal/config/config.go` (AgentsConfig ~line 700, Load loop ~line 273, Defaults ~line 246)
- Modify: `internal/config/template.go` (settingDocs, after `allow_main`)
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces: `config.AgentsConfig.Spawn []string` (`toml:"spawn"`), `config.AgentsConfig.MaxSpawned int` (`toml:"max_spawned"`), `func (c AgentsConfig) SpawnCap() int` (clamped 1..16, 0 → 4).

- [ ] **Step 1: Write the failing test**

```go
func TestAgentsSpawnIsGlobalOnly(t *testing.T) {
	dir := t.TempDir()
	global := filepath.Join(dir, "global.toml")
	repo := filepath.Join(dir, "repo.toml")
	os.WriteFile(global, []byte("[agents]\nspawn = [\"Claude\"]\nmax_spawned = 2\n"), 0o644)
	os.WriteFile(repo, []byte("[agents]\nspawn = [\"Evil\"]\nmax_spawned = 16\nstale_after = \"7d\"\n"), 0o644)
	cfg, err := Load(global, repo)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cfg.Agents.Spawn, []string{"Claude"}) || cfg.Agents.MaxSpawned != 2 {
		t.Fatalf("repo must not change spawn/max_spawned: %+v", cfg.Agents)
	}
	if cfg.Agents.StaleAfter != "7d" {
		t.Fatalf("other [agents] keys still overlay: %+v", cfg.Agents)
	}
	d, _ := Load("", "")
	if d.Agents.SpawnCap() != 4 || len(d.Agents.Spawn) != 0 {
		t.Fatalf("defaults: %+v cap %d", d.Agents, d.Agents.SpawnCap())
	}
	for in, want := range map[int]int{-3: 1, 0: 4, 1: 1, 9: 9, 99: 16} {
		if got := (AgentsConfig{MaxSpawned: in}).SpawnCap(); got != want {
			t.Errorf("SpawnCap(%d) = %d, want %d", in, got, want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /work/gigagit/.claude/worktrees/agent-spawn && go test ./internal/config -run TestAgentsSpawnIsGlobalOnly`
Expected: FAIL — `cfg.Agents.Spawn undefined`.

- [ ] **Step 3: Implement**

In `AgentsConfig` add:

```go
	// Spawn names the session commands (by their `name`) an agent running
	// inside gg may start as a worker (`agent_start`). Empty = spawning off.
	// Read from the GLOBAL file only: a cloned repo must not enable it.
	Spawn []string `toml:"spawn"`
	// MaxSpawned caps live agent-spawned sessions per TUI (default 4,
	// clamped 1..16). Global file only.
	MaxSpawned int `toml:"max_spawned"`
```

and below the struct:

```go
// SpawnCap is MaxSpawned clamped to 1..16; unset (0) is the default 4.
func (c AgentsConfig) SpawnCap() int {
	switch {
	case c.MaxSpawned == 0:
		return 4
	case c.MaxSpawned < 1:
		return 1
	case c.MaxSpawned > 16:
		return 16
	}
	return c.MaxSpawned
}
```

In `overlayAgents` add:

```go
	if len(src.Spawn) > 0 {
		dst.Spawn = src.Spawn
	}
	if src.MaxSpawned != 0 {
		dst.MaxSpawned = src.MaxSpawned
	}
```

In `Load`, replace the agents lines with:

```go
			spawn, maxSpawned := cfg.Agents.Spawn, cfg.Agents.MaxSpawned
			overlayAgents(&cfg.Agents, layer.Agents)
			if i == 0 {
				// reserved is repo-only: paths belong to one repo, and a
				// global entry could never be unreserved from it.
				cfg.Agents.Reserved = nil
			} else {
				// spawn and max_spawned are global-only: a cloned repo's
				// .gg.toml must never let agents start agents.
				cfg.Agents.Spawn, cfg.Agents.MaxSpawned = spawn, maxSpawned
			}
```

In `template.go` settingDocs after `allow_main`:

```go
	{"agents", "spawn", nil, "session command names (Settings → External tools, category session) an agent running inside gg may start as a worker through the agent_start MCP tool / gg agent start; empty = spawning off; each command must also have been approved once by starting it from the TUI and must contain <prompt>; GLOBAL file only (a repo value is ignored)"},
	{"agents", "max_spawned", 4, "live agent-spawned sessions per TUI (agents you start yourself are not counted); clamped to 1..16; GLOBAL file only"},
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/config`
Expected: PASS (including `TestSettingDocsCoverAllFields`).

- [ ] **Step 5: Commit**

```bash
git -C /work/gigagit/.claude/worktrees/agent-spawn add internal/config
git -C /work/gigagit/.claude/worktrees/agent-spawn commit -m "feat(config): [agents] spawn allow-list and max_spawned, global file only"
```

---

### Task 2: the `<prompt>` command token

**Files:**
- Modify: `internal/template/command.go`
- Test: `internal/template/command_test.go`

**Interfaces:**
- Produces: `template.CmdCtx.Prompt string`; token `<prompt>` / `<prompt:FLAG>`; `func HasPromptSlot(tmpl string) bool`.

- [ ] **Step 1: Write the failing test**

```go
func TestPromptToken(t *testing.T) {
	cases := []struct{ tmpl, prompt, goos, want string }{
		{"claude <prompt>", "", "linux", "claude "},
		{"claude <prompt>", "do it", "linux", "claude 'do it'"},
		{"junie <prompt:--prompt>", "", "linux", "junie "},
		{"junie <prompt:--prompt> --brave", "do it", "linux", "junie --prompt 'do it' --brave"},
		{"agy <prompt:--prompt-interactive>", "do it", "windows", `agy --prompt-interactive "do it"`},
	}
	for _, c := range cases {
		got, err := resolveCommandFor(c.tmpl, nil, CmdCtx{Prompt: c.prompt}, c.goos)
		if err != nil || got != c.want {
			t.Errorf("%q/%q/%s = %q, %v; want %q", c.tmpl, c.prompt, c.goos, got, err, c.want)
		}
	}
	if err := ValidateCommandTokens("junie <prompt:--prompt>", false); err != nil {
		t.Fatalf("<prompt> must validate: %v", err)
	}
	if !HasPromptSlot("x <prompt:--p> y") || !HasPromptSlot("x <prompt>") || HasPromptSlot("x <repo>") {
		t.Fatal("HasPromptSlot")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/template -run TestPromptToken`
Expected: FAIL — `unknown field Prompt in struct literal`.

- [ ] **Step 3: Implement**

Add to `CmdCtx` (and a sentence to its doc comment: "Prompt is the agent kick-off line (`<prompt>`); "" leaves the slot empty."):

```go
	Prompt                            string
```

In `resolveCommandToken`, before `default:`:

```go
	case "prompt":
		if ctx.Prompt == "" {
			return "", nil // a manual start: the slot vanishes
		}
		q := quoteArgFor(ctx.Prompt, goos)
		if hasColon && rest != "" {
			return rest + " " + q, nil
		}
		return q, nil
```

In `commandTokens` add `"prompt": false,`. Add:

```go
// HasPromptSlot reports whether a command template carries a <prompt> slot —
// the one way an agent's kick-off line reaches a spawned worker.
func HasPromptSlot(tmpl string) bool {
	for _, m := range tokenRe.FindAllStringSubmatch(tmpl, -1) {
		if p, _, _ := cutColon(m[1]); p == "prompt" {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/template`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git -C /work/gigagit/.claude/worktrees/agent-spawn add internal/template
git -C /work/gigagit/.claude/worktrees/agent-spawn commit -m "feat(template): <prompt>/<prompt:FLAG> token — empty on a manual start, the kick-off line on a spawn"
```

---

### Task 3: session rows carry `<prompt>`

**Files:**
- Modify: `internal/exttool/exttool.go` (the `CatSession` rows of claude, junie, codex, antigravity — lines ~481-552)
- Modify: `internal/exttool/testdata/catalog_versions.golden` (regenerated)
- Test: `internal/exttool/exttool_test.go`

**Interfaces:**
- Consumes: Task 2's `template.HasPromptSlot`.

- [ ] **Step 1: Write the failing test** (in `exttool_test.go`)

```go
func TestSessionRowsHavePromptSlot(t *testing.T) {
	want := map[string]bool{"claude": true, "junie": true, "codex": true, "antigravity": true, "kimi": false}
	for _, tl := range Builtins() {
		w, ok := want[tl.ID]
		if !ok {
			continue
		}
		for _, ct := range tl.Commands {
			if ct.Category != CatSession {
				continue
			}
			if got := template.HasPromptSlot(ct.Command); got != w {
				t.Errorf("%s / %s: prompt slot = %t, want %t (%q)", tl.ID, ct.Name, got, w, ct.Command)
			}
		}
	}
}
```

(import `github.com/homeend/gigagit/internal/template` if the file lacks it.)

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/exttool -run TestSessionRowsHavePromptSlot`
Expected: FAIL — 8 rows without a slot.

- [ ] **Step 3: Implement** — replace the eight session rows:

```go
				{Category: CatSession, Name: "Claude", Mode: ModeSession, Version: 2, Command: "<bin> <prompt>"},
				{Category: CatSession, Name: "Claude (yolo)", Mode: ModeSession, OptIn: true, Version: 2, Command: "<bin> --dangerously-skip-permissions <prompt>"},
```
```go
				{Category: CatSession, Name: "Junie", Mode: ModeSession, Version: 2, Command: "<bin> <prompt:--prompt>"},
				{Category: CatSession, Name: "Junie (yolo)", Mode: ModeSession, OptIn: true, Version: 2, Command: "<bin> --brave <prompt:--prompt>"},
```
```go
				{Category: CatSession, Name: "Codex", Mode: ModeSession, Version: 2, Command: "<bin> <prompt>"},
				{Category: CatSession, Name: "Codex (yolo)", Mode: ModeSession, OptIn: true, Version: 2, Command: "<bin> --dangerously-bypass-approvals-and-sandbox <prompt>"},
```
```go
				{Category: CatSession, Name: "Antigravity", Mode: ModeSession, Version: 2, Command: "<bin> <prompt:--prompt-interactive>"},
				{Category: CatSession, Name: "Antigravity (yolo)", Mode: ModeSession, OptIn: true, Version: 2, Command: "<bin> --dangerously-skip-permissions <prompt:--prompt-interactive>"},
```

(The positional prompt goes LAST so flags stay flags for every agent's parser. If a family already carries `Version: N > 1`, use N+1 instead of 2 — check each row before editing.)

- [ ] **Step 4: Regenerate the golden and run the package**

Run: `go test ./internal/exttool -run TestCatalogVersionBumpGuard -update && go test ./internal/exttool ./internal/domain -run 'Session|Tool'`
Expected: PASS. The existing template-update flow (`domain.ToolTemplateStatuses` → `ToolUpdateAvailable`) now offers the new session commands to every config holding the old ones; no extra migration (see ledger ruling R1).

- [ ] **Step 5: Commit**

```bash
git -C /work/gigagit/.claude/worktrees/agent-spawn add internal/exttool
git -C /work/gigagit/.claude/worktrees/agent-spawn commit -m "feat(exttool): session commands take a <prompt> slot (claude, junie, codex, antigravity; families bumped)"
```

---

### Task 4: claim handover, revert to parent, young-claim grace, caller exemption

**Files:**
- Modify: `internal/wtclaim/wtclaim.go` (Claim)
- Modify: `internal/domain/wtclaim.go`, `internal/domain/wtguards.go` (sessionGuard)
- Test: `internal/domain/wtclaim_test.go`, `internal/domain/wtguards_test.go`

**Interfaces:**
- Produces: `wtclaim.Claim.Parent string` (`toml:"parent"`); `func (s *Service) HandOverWorktree(ctx context.Context, path, from, to string) error` (ErrNotHolder when `from` does not hold it); `func (s *Service) claimHeldBy(ctx context.Context, path, session string) (bool, error)`; `const youngClaimGrace = 2 * sessionreg.LiveWindow`; `ClaimInfo.Parent string`.

- [ ] **Step 1: Write the failing tests** (append to `wtclaim_test.go`)

```go
func TestHandOverAndRevertToParent(t *testing.T) {
	t.Parallel()
	main, svc, reg := inventoryRepo(t)
	wt := addWT(t, main, "ho")
	sessionreg.Write(reg, "p", sessionreg.Registry{PID: 1, Sessions: []sessionreg.Entry{
		{ID: "p/s1", Dir: main, Agent: "claude", State: "running"},
		{ID: "p/s2", Dir: main, Agent: "codex", State: "running"}, // not in wt: a session there blocks the claim
	}})
	ctx := context.Background()
	if err := svc.ClaimWorktree(ctx, wt, "p/s1", "issue 7", pol()); err != nil {
		t.Fatal(err)
	}
	if err := svc.HandOverWorktree(ctx, wt, "p/s9", "p/s2"); !errors.Is(err, ErrNotHolder) {
		t.Fatalf("handover by a non-holder = %v", err)
	}
	if err := svc.HandOverWorktree(ctx, wt, "p/s1", "p/s2"); err != nil {
		t.Fatal(err)
	}
	c, ok, _ := wtclaim.Read(git.GitDirAt(wt))
	if !ok || c.Session != "p/s2" || c.Parent != "p/s1" || c.Agent != "codex" || c.Note != "issue 7" {
		t.Fatalf("after handover: %+v", c)
	}
	// The worker exits; the parent lives: the claim goes back to the parent.
	sessionreg.Write(reg, "p", sessionreg.Registry{PID: 1, Sessions: []sessionreg.Entry{
		{ID: "p/s1", Dir: main, Agent: "claude", State: "running"},
		{ID: "p/s2", Dir: main, Agent: "codex", State: "exited"},
	}})
	infos, _ := svc.WorktreeInventory(ctx, pol(), false)
	if w := find(t, infos, wt); w.Claim() == nil || w.Claim().Session != "p/s1" || w.Claim().Agent != "claude" {
		t.Fatalf("claim must revert to the live parent: %+v", w.Claim())
	}
	c, _, _ = wtclaim.Read(git.GitDirAt(wt))
	if c.Parent != "" || c.Note != "issue 7" {
		t.Fatalf("reverted claim: %+v", c)
	}
	// Now the parent exits too: swept.
	sessionreg.Write(reg, "p", sessionreg.Registry{PID: 1, Sessions: []sessionreg.Entry{
		{ID: "p/s1", Dir: main, State: "exited"}, {ID: "p/s2", Dir: main, State: "exited"},
	}})
	infos, _ = svc.WorktreeInventory(ctx, pol(), false)
	if w := find(t, infos, wt); w.Claim() != nil {
		t.Fatalf("both dead: claim must be swept, got %+v", w.Claim())
	}
}

func TestDeadWorkerClaimRevertsToParent(t *testing.T) {
	t.Parallel()
	main, svc, reg := inventoryRepo(t)
	wt := addWT(t, main, "rv")
	gd := git.GitDirAt(wt)
	if err := wtclaim.Create(gd, wtclaim.Claim{Session: "p/s2", Parent: "p/s1", Agent: "codex",
		Since: time.Now().Add(-time.Hour).UTC().Truncate(time.Second), Host: localHost()}); err != nil {
		t.Fatal(err)
	}
	sessionreg.Write(reg, "p", sessionreg.Registry{PID: 1, Sessions: []sessionreg.Entry{
		{ID: "p/s1", Dir: main, Agent: "claude", State: "running"},
	}})
	ok, err := svc.ReleaseWorktree(context.Background(), wt, "p/s1", false)
	if err != nil || !ok {
		t.Fatalf("the parent owns the reverted claim, so it can release it: %v %v", ok, err)
	}
}

func TestYoungClaimGraceKeepsUnlistedSession(t *testing.T) {
	t.Parallel()
	main, svc, reg := inventoryRepo(t)
	wt := addWT(t, main, "young")
	proc := fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
	sessionreg.Write(reg, proc, sessionreg.Registry{PID: os.Getpid(), Sessions: []sessionreg.Entry{
		{ID: proc + "/s1", Dir: main, State: "running"},
	}})
	// A worker claim written before its TUI's registry lists the worker.
	if err := wtclaim.Create(git.GitDirAt(wt), wtclaim.Claim{Session: proc + "/s2", Parent: proc + "/s1",
		Since: time.Now().UTC().Truncate(time.Second), Host: localHost()}); err != nil {
		t.Fatal(err)
	}
	infos, _ := svc.WorktreeInventory(context.Background(), pol(), false)
	if w := find(t, infos, wt); w.Claim() == nil || w.Claim().Session != proc+"/s2" {
		t.Fatalf("a fresh claim of an unlisted session in a live registry must survive: %+v", w.Claim())
	}
}
```

Append to `wtguards_test.go`:

```go
func TestSessionGuardExemptsTheCaller(t *testing.T) {
	t.Parallel()
	lv := liveView{byDir: map[string][]SessionRef{filepath.Clean("/w"): {{ID: "p/s1", Agent: "claude", State: "running"}}}}
	g := sessionGuard{lv: lv}
	r, _ := g.Check(context.Background(), wtguard.Target{Dir: "/w", CallerSession: "p/s1"})
	if r.Blocker != nil {
		t.Fatalf("the caller's own session must not block: %+v", r.Blocker)
	}
	r, _ = g.Check(context.Background(), wtguard.Target{Dir: "/w", CallerSession: "p/s9"})
	if r.Blocker == nil {
		t.Fatal("another session must still block")
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/domain -run 'HandOver|RevertsToParent|YoungClaim|ExemptsTheCaller'`
Expected: FAIL — `unknown field Parent`, `svc.HandOverWorktree undefined`.

- [ ] **Step 3: Implement**

`internal/wtclaim/wtclaim.go`, in `Claim` after `Host`:

```go
	// Parent is the session that handed this claim to its holder (an agent
	// that spawned a worker); when the holder dies and the parent lives, the
	// claim reverts to the parent instead of being swept. "" = none.
	Parent string `toml:"parent"`
```

`internal/domain/wtinventory.go` (or wherever `ClaimInfo` is declared — `grep -n "type ClaimInfo" internal/domain`): add `Parent string \`json:"parent,omitempty"\``.

`internal/domain/wtclaim.go`:

```go
// youngClaimGrace: a claim this fresh whose session its LIVE process does not
// list yet is a handover racing the registry write (sessionpublish writes
// asynchronously) — alive, not dead.
const youngClaimGrace = 2 * sessionreg.LiveWindow
```

(import `sessionreg`). `sameClaim` gains `&& a.Parent == b.Parent`. In `claimDead`, before `return sessionDead(...)`:

```go
	if _, listed := lv.running[c.Session]; !listed && lv.procs[sessionreg.ProcOf(c.Session)] &&
		!c.Since.IsZero() && time.Since(c.Since) < youngClaimGrace {
		return false
	}
```

Add the settle helper and use it in both sweep sites:

```go
// settleDeadClaim runs under the claim lock on a claim already judged dead:
// it reverts to a live Parent (ruling 6) or removes it. ok reports a claim
// that is still there (reverted).
func settleDeadClaim(gitDir string, c wtclaim.Claim, lv liveView) (ClaimInfo, bool) {
	if c.Parent != "" && !sessionDead(c.Parent, lv) {
		back := wtclaim.Claim{Session: c.Parent, Agent: lv.agents[c.Parent],
			Since: time.Now().UTC().Truncate(time.Second), Note: c.Note, Host: c.Host}
		if wtclaim.Remove(gitDir) == nil && wtclaim.Create(gitDir, back) == nil {
			return claimInfoOf(back), true
		}
		return ClaimInfo{}, false
	}
	_ = wtclaim.Remove(gitDir)
	return ClaimInfo{}, false
}

func claimInfoOf(c wtclaim.Claim) ClaimInfo {
	return ClaimInfo{Session: c.Session, Agent: c.Agent, Since: c.Since, Note: c.Note, Parent: c.Parent}
}
```

`liveClaim` becomes:

```go
func liveClaim(gitDir string, lv liveView) (ClaimInfo, bool) {
	if gitDir == "" {
		return ClaimInfo{}, false
	}
	c, ok := readClaim(gitDir)
	if !ok {
		return ClaimInfo{}, false
	}
	if !claimDead(c, gitDir, lv) {
		return claimInfoOf(c), true
	}
	var info ClaimInfo
	var kept bool
	_ = wtclaim.WithLock(gitDir, func() error {
		again, ok := readClaim(gitDir)
		switch {
		case !ok:
		case !sameClaim(again, c) || !claimDead(again, gitDir, lv):
			info, kept = claimInfoOf(again), true // someone re-claimed meanwhile
		default:
			info, kept = settleDeadClaim(gitDir, again, lv)
		}
		return nil
	})
	return info, kept
}
```

`liveClaimLocked` becomes:

```go
func liveClaimLocked(gitDir string, lv liveView) (ClaimInfo, bool) {
	c, ok := readClaim(gitDir)
	if !ok {
		return ClaimInfo{}, false
	}
	if claimDead(c, gitDir, lv) {
		return settleDeadClaim(gitDir, c, lv)
	}
	return claimInfoOf(c), true
}
```

In `ClaimWorktree`'s locked section, replace the dead-claim `wtclaim.Remove` with `if info, kept := settleDeadClaim(gitDir, c, lv); kept { if info.Session == sessionID { return nil }; return &NotFreeError{Path: target.Path, BlockedBy: []string{"claimed"}} }` so a reverted claim is honoured.

Add the handover:

```go
// HandOverWorktree moves path's claim from the session holding it to `to`,
// recording `from` as the claim's Parent (an overseer handing a worktree to
// the worker it spawned). ErrNotHolder when from does not hold a live claim.
func (s *Service) HandOverWorktree(ctx context.Context, path, from, to string) error {
	w, err := s.worktreeAt(ctx, path)
	if err != nil {
		return err
	}
	gitDir := git.GitDirAt(w.Path)
	if gitDir == "" {
		return ErrUnknownWorktree
	}
	lv := readLive(s.registryDir())
	return wtclaim.WithLock(gitDir, func() error {
		c, ok := liveClaimLocked(gitDir, lv)
		if !ok || c.Session != from {
			return ErrNotHolder
		}
		if err := wtclaim.Remove(gitDir); err != nil {
			return err
		}
		return wtclaim.Create(gitDir, wtclaim.Claim{Session: to, Agent: lv.agents[to], Parent: from,
			Since: time.Now().UTC().Truncate(time.Second), Note: c.Note, Host: localHost()})
	})
}

// claimHeldBy reports whether session holds path's live claim.
func (s *Service) claimHeldBy(ctx context.Context, path, session string) (bool, error) {
	w, err := s.worktreeAt(ctx, path)
	if err != nil {
		return false, err
	}
	c, ok := liveClaim(git.GitDirAt(w.Path), readLive(s.registryDir()))
	return ok && c.Session == session, nil
}
```

Replace every remaining `ClaimInfo{Session: c.Session, Agent: c.Agent, Since: c.Since, Note: c.Note}` literal in the file with `claimInfoOf(c)`.

`internal/domain/wtguards.go` `sessionGuard.Check`: change the inner loop to

```go
		for _, r := range rs {
			if r.State == "running" && (t.CallerSession == "" || r.ID != t.CallerSession) {
				running = append(running, r.Agent)
			}
		}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/domain ./internal/wtclaim ./internal/cli -run 'Claim|Guard|Inventory|Recycle|Worktree'`
Expected: PASS, including every stage-1 claim test (`TestExitedSessionClaimIsDead` must still sweep: an EXITED listing is not "unlisted").

- [ ] **Step 5: Commit**

```bash
git -C /work/gigagit/.claude/worktrees/agent-spawn add internal/wtclaim internal/domain
git -C /work/gigagit/.claude/worktrees/agent-spawn commit -m "feat(domain): claim handover with Parent, dead worker's claim reverts to a live parent, young-claim grace, the caller's own session never blocks it"
```

---

### Task 5: spawn registry and `StartAgentSession`

**Files:**
- Create: `internal/domain/agentspawn.go`, `internal/domain/agentspawn_test.go`
- Modify: `internal/domain/sessions.go` (StartSession → shared helper with a prompt)
- Modify: `internal/agentsession/session.go` (`childEnv` strip list)

**Interfaces:**
- Produces:
  - `const AgentKickoff string` (the Global Constraints text)
  - `type SpawnRecord struct { Parent, Brief, Worktree string; Spawned bool }`
  - `func FullSessionID(id SessionID) string` (= `ProcTag()+"/"+id`)
  - `func VerifyAgentToken(token string) (string, bool)` — full id of a RUNNING session
  - `func AgentRecord(fullID string) (SpawnRecord, bool)`
  - `func AgentDescends(target, caller string) bool`
  - `func reserveSpawnSlot(cap int) (release func(), ok bool)` / `commitSpawnSlot()` semantics via the returned func: `release(started bool)`
  - `func (s *Service) StartAgentSession(ctx context.Context, tc config.ToolCommand, dir, cwd string, cols, rows int, env []string, mcpURL string, rec SpawnRecord, prompt string) (*AgentSession, string, error)` — returns the session and its token ("" when mcpURL is "")
  - `func (s *Service) StartSession(...)` unchanged signature, now `= startSessionPrompt(..., "")`

- [ ] **Step 1: Write the failing tests** (`agentspawn_test.go`)

```go
package domain

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/config"
)

// useSessions installs a fresh manager and registry for one serial test.
func useSessions(t *testing.T) {
	t.Helper()
	restoreMgr := UseSessionManager(agentsession.NewManager())
	restoreReg := useSpawnRegistry()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		Sessions().KillAll(ctx)
		restoreReg()
		restoreMgr()
	})
}

func sleeper() config.ToolCommand {
	return config.ToolCommand{Category: "session", Name: "Sleeper", Mode: "session", Command: "sh -c 'sleep 600' <prompt>"}
}

func TestStartAgentSessionMintsAndBindsAToken(t *testing.T) {
	useSessions(t)
	main, svc := newRealRepo(t)
	s, tok, err := svc.StartAgentSession(context.Background(), sleeper(), main, "", 80, 24, nil, "http://127.0.0.1:1/mcp", SpawnRecord{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) != 64 {
		t.Fatalf("token %q", tok)
	}
	id, ok := VerifyAgentToken(tok)
	if !ok || id != FullSessionID(s.Info().ID) {
		t.Fatalf("verify = %q %v", id, ok)
	}
	if _, ok := VerifyAgentToken(strings.Repeat("0", 64)); ok {
		t.Fatal("an unknown token verified")
	}
	Sessions().Kill(s.Info().ID)
	<-s.Done()
	if _, ok := VerifyAgentToken(tok); ok {
		t.Fatal("an exited session's token must stop working")
	}
}

func TestStartAgentSessionWithoutURLHasNoToken(t *testing.T) {
	useSessions(t)
	main, svc := newRealRepo(t)
	_, tok, err := svc.StartAgentSession(context.Background(), sleeper(), main, "", 80, 24, nil, "", SpawnRecord{}, "")
	if err != nil || tok != "" {
		t.Fatalf("tok %q err %v", tok, err)
	}
}

func TestAgentDescends(t *testing.T) {
	useSessions(t)
	bindRecord("p/s2", SpawnRecord{Parent: "p/s1", Spawned: true})
	bindRecord("p/s3", SpawnRecord{Parent: "p/s2", Spawned: true})
	if !AgentDescends("p/s2", "p/s1") || !AgentDescends("p/s3", "p/s1") {
		t.Fatal("children and grandchildren descend")
	}
	if AgentDescends("p/s1", "p/s2") || AgentDescends("p/s4", "p/s1") || AgentDescends("p/s1", "p/s1") {
		t.Fatal("parents, strangers and self do not")
	}
}

func TestSpawnSlotsAreRaceFree(t *testing.T) {
	useSessions(t)
	var mu sync.Mutex
	got := 0
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, ok := reserveSpawnSlot(3); ok {
				mu.Lock()
				got++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if got != 3 {
		t.Fatalf("%d of 8 racing reservations won 3 slots", got)
	}
}

func TestKickoffIsShellSafe(t *testing.T) {
	if strings.ContainsAny(AgentKickoff, "\"'%!^&|<>$`\\") {
		t.Fatalf("the kick-off must be safe in sh and cmd.exe quoting: %q", AgentKickoff)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/domain -run 'StartAgentSession|AgentDescends|SpawnSlots|Kickoff'`
Expected: FAIL — undefined `StartAgentSession`, `useSpawnRegistry`, `bindRecord`, `reserveSpawnSlot`, `AgentKickoff`.

- [ ] **Step 3: Implement `agentspawn.go`**

```go
package domain

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"

	"github.com/homeend/gigagit/internal/agentsession"
)

// AgentKickoff is the one line a spawned worker's <prompt> slot carries: the
// brief itself never rides a command line (spec §2 ruling 5).
const AgentKickoff = "You were started by gg as a worker agent. Call the gg MCP tool agent_task to read your task, then do it."

// SpawnRecord is what this process knows about an agent session it started.
type SpawnRecord struct {
	Parent   string // full session id of the agent that spawned it ("" = the user)
	Brief    string // the free-form task (agent_task)
	Worktree string
	Spawned  bool // started by an agent (counts against max_spawned; may not spawn)
}

// spawnRegistry is process-global, like Sessions(): it survives reRoot.
type spawnRegistry struct {
	mu      sync.Mutex
	tokens  map[string]string // token -> full session id
	records map[string]SpawnRecord
	pending int // reserved slots whose start has not finished
}

var (
	spawnMu  sync.Mutex
	spawnReg = newSpawnRegistry()
)

func newSpawnRegistry() *spawnRegistry {
	return &spawnRegistry{tokens: map[string]string{}, records: map[string]SpawnRecord{}}
}

func registry() *spawnRegistry { spawnMu.Lock(); defer spawnMu.Unlock(); return spawnReg }

// useSpawnRegistry installs a fresh registry (tests) and returns the restore.
func useSpawnRegistry() func() {
	spawnMu.Lock()
	prev := spawnReg
	spawnReg = newSpawnRegistry()
	spawnMu.Unlock()
	return func() { spawnMu.Lock(); spawnReg = prev; spawnMu.Unlock() }
}

// FullSessionID is a session's GG_SESSION_ID: <ProcTag>/<id>.
func FullSessionID(id SessionID) string { return agentsession.ProcTag() + "/" + string(id) }

// localID maps a full id back to this process's session id ("" = not ours).
func localID(full string) SessionID {
	id, ok := strings.CutPrefix(full, agentsession.ProcTag()+"/")
	if !ok || id == "" {
		return ""
	}
	return SessionID(id)
}

// runningLocal reports a full id naming a RUNNING session of this process.
func runningLocal(full string) (*AgentSession, bool) {
	id := localID(full)
	if id == "" {
		return nil, false
	}
	s, ok := Sessions().Get(id)
	if !ok || s.Info().State != SessionRunning {
		return nil, false
	}
	return s, true
}

// prune drops records and tokens of sessions no longer listed (removed);
// called under r.mu on every read — the Manager has no removal hook.
func (r *spawnRegistry) prune() {
	listed := map[string]bool{}
	for _, in := range Sessions().List() {
		listed[FullSessionID(in.ID)] = true
	}
	for id := range r.records {
		if !listed[id] {
			delete(r.records, id)
		}
	}
	for tok, id := range r.tokens {
		if !listed[id] {
			delete(r.tokens, tok)
		}
	}
}

func mintToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("crypto/rand: " + err.Error()) // never on a supported OS
	}
	return hex.EncodeToString(b[:])
}

func bindRecord(full string, rec SpawnRecord) {
	r := registry()
	r.mu.Lock()
	r.records[full] = rec
	r.mu.Unlock()
}

func bindToken(tok, full string) {
	r := registry()
	r.mu.Lock()
	r.tokens[tok] = full
	r.mu.Unlock()
}

// VerifyAgentToken maps a bearer token to the full id of the RUNNING agent
// session it was minted for; false for an unknown token or a session that
// exited (a killed worker's token stops working at once).
func VerifyAgentToken(tok string) (string, bool) {
	r := registry()
	r.mu.Lock()
	r.prune()
	full, ok := r.tokens[tok]
	r.mu.Unlock()
	if !ok {
		return "", false
	}
	if _, live := runningLocal(full); !live {
		return "", false
	}
	return full, true
}

// AgentRecord is the spawn record of a session of this process.
func AgentRecord(full string) (SpawnRecord, bool) {
	r := registry()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prune()
	rec, ok := r.records[full]
	return rec, ok
}

// AgentDescends reports target being caller's child, grandchild, …
func AgentDescends(target, caller string) bool {
	r := registry()
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := map[string]bool{}
	for cur := target; !seen[cur]; {
		seen[cur] = true
		rec, ok := r.records[cur]
		if !ok || rec.Parent == "" {
			return false
		}
		if rec.Parent == caller {
			return true
		}
		cur = rec.Parent
	}
	return false
}

// reserveSpawnSlot takes one of cap slots (live spawned + pending). The
// returned release(started) frees the pending slot; a started session then
// counts through its record instead.
func reserveSpawnSlot(cap int) (func(started bool), bool) {
	r := registry()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prune()
	live := 0
	for id, rec := range r.records {
		if _, ok := runningLocal(id); ok && rec.Spawned {
			live++
		}
	}
	if live+r.pending >= cap {
		return nil, false
	}
	r.pending++
	var once sync.Once
	return func(bool) { once.Do(func() { r.mu.Lock(); r.pending--; r.mu.Unlock() }) }, true
}
```

(`release`'s argument is kept for readability at call sites; the record written before release is what keeps a started session counted.)

In `sessions.go`, change `StartSession` and add the prompt-aware path plus `StartAgentSession`:

```go
func (s *Service) StartSession(ctx context.Context, tc config.ToolCommand, worktreeDir, cwd string, cols, rows int, env []string) (*AgentSession, error) {
	return s.startSessionPrompt(ctx, tc, worktreeDir, cwd, cols, rows, env, "")
}

func (s *Service) startSessionPrompt(ctx context.Context, tc config.ToolCommand, worktreeDir, cwd string, cols, rows int, env []string, prompt string) (*AgentSession, error) {
	resolved, err := template.ResolveCommand(tc.Command, nil, template.CmdCtx{Repo: worktreeDir, Prompt: prompt})
	if err != nil {
		return nil, err
	}
	return s.startLine(ctx, Sessions(), tc.Name, agentIDFor(tc), resolved, worktreeDir, cwd, cols, rows, env)
}

// StartAgentSession is StartSession for an AGENT the agent channel can
// reach: with mcpURL set the child gets GG_MCP_URL and a fresh
// GG_SESSION_TOKEN, bound to the new session with rec. prompt fills the
// command's <prompt> slot (AgentKickoff for a spawned worker, "" for a
// manual start). Returns the token ("" without a URL).
func (s *Service) StartAgentSession(ctx context.Context, tc config.ToolCommand, dir, cwd string, cols, rows int, env []string, mcpURL string, rec SpawnRecord, prompt string) (*AgentSession, string, error) {
	tok := ""
	if mcpURL != "" {
		tok = mintToken()
		env = append(append([]string(nil), env...), "GG_MCP_URL="+mcpURL, "GG_SESSION_TOKEN="+tok)
	}
	if rec.Parent != "" {
		env = append(env, "GG_PARENT_SESSION="+rec.Parent)
	}
	sess, err := s.startSessionPrompt(ctx, tc, dir, cwd, cols, rows, env, prompt)
	if err != nil {
		return nil, "", err
	}
	full := FullSessionID(sess.Info().ID)
	bindRecord(full, rec)
	if tok != "" {
		bindToken(tok, full)
	}
	return sess, tok, nil
}
```

`internal/agentsession/session.go` `childEnv`: extend the case list to
`case "TMUX", "TMUX_PANE", "TERM", "GG_SESSION_ID", "GG_MCP_URL", "GG_SESSION_TOKEN", "GG_PARENT_SESSION":` and add one sentence to its comment ("a gg started inside a gg console must not hand its own agent channel identity to its children").

- [ ] **Step 4: Run tests**

Run: `go test ./internal/domain ./internal/agentsession -run 'StartAgentSession|AgentDescends|SpawnSlots|Kickoff|StartSession|ChildEnv'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git -C /work/gigagit/.claude/worktrees/agent-spawn add internal/domain/agentspawn.go internal/domain/agentspawn_test.go internal/domain/sessions.go internal/agentsession/session.go
git -C /work/gigagit/.claude/worktrees/agent-spawn commit -m "feat(domain): agent spawn registry — per-session tokens, spawn records, reach, race-free slots; StartAgentSession"
```

---

### Task 6: agent verbs and `SpawnAgent`

**Files:**
- Create: `internal/domain/agentverbs.go`, `internal/domain/agentverbs_test.go`

**Interfaces:**
- Consumes: Task 1 `AgentsConfig.Spawn/SpawnCap`, Task 2 `template.HasPromptSlot`, Task 4 `HandOverWorktree`, `claimHeldBy`, `ClaimWorktree`, `ReleaseWorktree`, Task 5 registry + `StartAgentSession`.
- Produces:
  - `type AgentEntry struct { ID, Parent, Tool, Label, Worktree, State string; ExitCode int; Started time.Time; Spawned, Mine bool }` (json tags snake_case)
  - `func AgentList(caller string) []AgentEntry`
  - `func AgentScreen(target string) (state, text string, err error)`
  - `func AgentSend(caller, target, text string, enter bool, keys []string) error`
  - `func AgentKill(caller, target string, remove bool) error`
  - `func AgentTask(caller string) (SpawnRecord, error)`
  - `func ParseConsoleKeyName(s string) (ConsoleKey, error)`
  - `type AgentStartRequest struct { Caller, Worktree, Tool, Prompt, Note string }`
  - `type AgentStartResult struct { ID, Worktree, Tool, Warning string }`
  - `type SpawnSpec struct { Req AgentStartRequest; Cols, Rows int; Env []string; MCPURL string; Approved func(repoKey, command string) bool }`
  - `func SpawnAgent(ctx context.Context, sp SpawnSpec) (AgentStartResult, *AgentSession, error)`
  - `func ServiceForDir(dir string) *Service` (cached per dir)
  - `func (s *Service) ResolveWorktreeArg(ctx context.Context, arg string) (string, error)`
  - `const MaxBriefBytes = 256 << 10`

- [ ] **Step 1: Write the failing tests** (`agentverbs_test.go`)

```go
package domain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/wtclaim"
)

// spawnFixture: a repo, a worktree, a global config allowing "Sleeper", and
// a manually started overseer session in the main worktree.
func spawnFixture(t *testing.T, cap int) (main, wt string, svc *Service, overseer string) {
	t.Helper()
	useSessions(t)
	cfgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	os.MkdirAll(filepath.Join(cfgDir, "gg"), 0o755)
	os.WriteFile(filepath.Join(cfgDir, "gg", "config.toml"), []byte(
		"[agents]\nspawn = [\"Sleeper\"]\nmax_spawned = "+strconv.Itoa(cap)+"\n\n"+
			"[[tools.command]]\ncategory = \"session\"\nname = \"Sleeper\"\nmode = \"session\"\n"+
			"command = \"sh -c 'sleep 600' <prompt>\"\n\n"+
			"[[tools.command]]\ncategory = \"session\"\nname = \"Bare\"\nmode = \"session\"\ncommand = \"sh -c 'sleep 600'\"\n"), 0o644)
	main, svc = newRealRepo(t)
	svc.UseSessionRegistryDir(t.TempDir())
	wt = addWT(t, main, "job")
	s, _, err := svc.StartAgentSession(context.Background(), sleeper(), main, "", 80, 24, nil, "http://x", SpawnRecord{}, "")
	if err != nil {
		t.Fatal(err)
	}
	cacheService(main, svc)
	return main, wt, svc, FullSessionID(s.Info().ID)
}

func approveAll(string, string) bool { return true }

func TestSpawnAgentStartsHandsOverAndRecords(t *testing.T) {
	_, wt, _, ov := spawnFixture(t, 4)
	res, sess, err := SpawnAgent(context.Background(), SpawnSpec{Req: AgentStartRequest{Caller: ov, Worktree: wt,
		Tool: "sleeper", Prompt: "fix issue 7", Note: "issue 7"}, Cols: 80, Rows: 24, MCPURL: "http://x", Approved: approveAll})
	if err != nil {
		t.Fatal(err)
	}
	if res.ID != FullSessionID(sess.Info().ID) || res.Warning != "" {
		t.Fatalf("result %+v", res)
	}
	rec, ok := AgentRecord(res.ID)
	if !ok || rec.Parent != ov || rec.Brief != "fix issue 7" || !rec.Spawned {
		t.Fatalf("record %+v", rec)
	}
	c, ok, _ := wtclaim.Read(git.GitDirAt(wt))
	if !ok || c.Session != res.ID || c.Parent != ov || c.Note != "issue 7" {
		t.Fatalf("claim %+v", c)
	}
	if b, err := AgentTask(res.ID); err != nil || b.Brief != "fix issue 7" {
		t.Fatalf("agent_task = %+v %v", b, err)
	}
	if _, err := AgentTask(ov); err == nil {
		t.Fatal("a manual session has no task")
	}
}

func TestSpawnRefusals(t *testing.T) {
	_, wt, _, ov := spawnFixture(t, 1)
	base := SpawnSpec{Req: AgentStartRequest{Caller: ov, Worktree: wt, Tool: "Sleeper", Prompt: "x"}, Cols: 80, Rows: 24, MCPURL: "http://x", Approved: approveAll}
	try := func(mut func(*SpawnSpec)) error {
		sp := base
		mut(&sp)
		_, _, err := SpawnAgent(context.Background(), sp)
		return err
	}
	cases := map[string]struct {
		mut  func(*SpawnSpec)
		want string
	}{
		"dead caller":   {func(sp *SpawnSpec) { sp.Req.Caller = "0-1/s1" }, "not a running agent of this gg"},
		"not allowed":   {func(sp *SpawnSpec) { sp.Req.Tool = "Bare" }, "not in [agents] spawn"},
		"unknown tool":  {func(sp *SpawnSpec) { sp.Req.Tool = "Nope" }, "no session command named"},
		"unapproved":    {func(sp *SpawnSpec) { sp.Approved = func(string, string) bool { return false } }, "approve Sleeper once"},
		"empty prompt":  {func(sp *SpawnSpec) { sp.Req.Prompt = "" }, "prompt is empty"},
		"huge prompt":   {func(sp *SpawnSpec) { sp.Req.Prompt = strings.Repeat("x", MaxBriefBytes+1) }, "prompt is larger than"},
		"bad worktree":  {func(sp *SpawnSpec) { sp.Req.Worktree = "nowhere" }, "no worktree"},
	}
	for name, c := range cases {
		if err := try(c.mut); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", name, err, c.want)
		}
	}
	// Cap 1: the first start wins, the second is refused.
	res, _, err := SpawnAgent(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	if err := try(func(sp *SpawnSpec) {}); err == nil || !strings.Contains(err.Error(), "max_spawned") {
		t.Fatalf("over the cap = %v", err)
	}
	// A worker may not spawn.
	if err := try(func(sp *SpawnSpec) { sp.Req.Caller = res.ID }); err == nil || !strings.Contains(err.Error(), "may not start agents") {
		t.Fatalf("nested = %v", err)
	}
}

func TestSpawnRefusesUnapprovedCommand(t *testing.T) {
	_, wt, _, ov := spawnFixture(t, 4)
	var asked []string
	_, _, err := SpawnAgent(context.Background(), SpawnSpec{Req: AgentStartRequest{Caller: ov, Worktree: wt, Tool: "Sleeper", Prompt: "x"},
		Cols: 80, Rows: 24, Approved: func(key, cmd string) bool { asked = append(asked, cmd); return false }})
	if err == nil || len(asked) != 1 || asked[0] != "sh -c 'sleep 600' <prompt>" {
		t.Fatalf("approval must be asked for the resolved block's command TEXT: %v %v", asked, err)
	}
	if _, ok, _ := wtclaim.Read(git.GitDirAt(wt)); ok {
		t.Fatal("a refused spawn leaves no claim")
	}
}

func TestFailedWorkerClaimRevertsToOverseer(t *testing.T) {
	main, wt, _, ov := spawnFixture(t, 4)
	os.WriteFile(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "gg", "config.toml"), []byte(
		"[agents]\nspawn = [\"Quick\"]\n\n[[tools.command]]\ncategory = \"session\"\nname = \"Quick\"\nmode = \"session\"\ncommand = \"sh -c 'exit 3' <prompt>\"\n"), 0o644)
	_, sess, err := SpawnAgent(context.Background(), SpawnSpec{Req: AgentStartRequest{Caller: ov, Worktree: wt, Tool: "Quick", Prompt: "x"}, Cols: 80, Rows: 24, Approved: approveAll})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-sess.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the worker never exited")
	}
	infos, _ := ServiceForDir(main).WorktreeInventory(context.Background(), pol(), false)
	if w := find(t, infos, wt); w.Claim() == nil || w.Claim().Session != ov {
		t.Fatalf("a dead worker's claim must revert to the live overseer: %+v", w.Claim())
	}
}

func TestAgentSendTypesAndEnters(t *testing.T) {
	_, wt, svc, ov := spawnFixture(t, 4)
	shTool := sleeper()
	shTool.Command = "sh -i" // a plain interactive shell: typed text runs as a command line
	child, _, err := svc.StartAgentSession(context.Background(), shTool, wt, "", 80, 24, nil, "http://x", SpawnRecord{Parent: ov, Spawned: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	target := FullSessionID(child.Info().ID)
	if err := AgentSend(ov, target, "echo gg-$((6*7))", true, nil); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, text, err := AgentScreen(target)
		if err == nil && strings.Contains(text, "gg-42") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("screen never showed gg-42:\n%s", text)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := AgentSend(target, ov, "x", true, nil); err == nil || !strings.Contains(err.Error(), "not an agent you started") {
		t.Fatalf("send to a non-descendant = %v", err)
	}
	if err := AgentKill(ov, target, true); err != nil {
		t.Fatal(err)
	}
	if err := AgentKill(target, ov, false); err == nil {
		t.Fatal("kill of a non-descendant must refuse")
	}
}

func TestAgentListMarksMine(t *testing.T) {
	_, wt, _, ov := spawnFixture(t, 4)
	res, _, err := SpawnAgent(context.Background(), SpawnSpec{Req: AgentStartRequest{Caller: ov, Worktree: wt, Tool: "Sleeper", Prompt: "x"}, Cols: 80, Rows: 24, Approved: approveAll})
	if err != nil {
		t.Fatal(err)
	}
	var mine, theirs int
	for _, e := range AgentList(ov) {
		switch e.ID {
		case res.ID:
			if !e.Mine || e.Parent != ov || !e.Spawned || e.Tool != "Sleeper" || e.State != "running" {
				t.Fatalf("worker row %+v", e)
			}
			mine++
		case ov:
			if e.Mine {
				t.Fatal("the caller is not its own descendant")
			}
			theirs++
		}
	}
	if mine != 1 || theirs != 1 {
		t.Fatalf("list %+v", AgentList(ov))
	}
}

func TestParseConsoleKeyName(t *testing.T) {
	for in, want := range map[string]ConsoleKey{
		"enter": {K: "enter"}, "esc": {K: "esc"}, "ctrl+c": {K: "char", Mod: ModCtrl, Text: "c"},
		"1": {K: "char", Text: "1"}, "space": {K: "char", Text: " "}, "shift+tab": {K: "tab", Mod: ModShift},
	} {
		got, err := ParseConsoleKeyName(in)
		if err != nil || got != want {
			t.Errorf("%q = %+v %v, want %+v", in, got, err, want)
		}
	}
	if _, err := ParseConsoleKeyName("ctrl+nope"); err == nil {
		t.Error("unknown key accepted")
	}
}

func TestResolveWorktreeArg(t *testing.T) {
	t.Parallel()
	main, svc := newRealRepo(t)
	wt := addWT(t, main, "feature-x")
	for _, arg := range []string{wt, "feature-x"} {
		got, err := svc.ResolveWorktreeArg(context.Background(), arg)
		if err != nil || !SameCheckout(got, wt) {
			t.Errorf("%q = %q %v", arg, got, err)
		}
	}
	if _, err := svc.ResolveWorktreeArg(context.Background(), "nope"); err == nil {
		t.Error("an unknown name resolved")
	}
}
```

(Add `"strconv"` to the imports; drop `"errors"` if unused. A start that fails inside `Manager.Start` cannot be provoked portably — `sessionShell` always runs the user's shell — so the created-claim release on that path is covered by code review, and the observable case, a worker that dies at once, by `TestFailedWorkerClaimRevertsToOverseer`.)

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/domain -run 'Spawn|FailedWorker|AgentSend|AgentList|ConsoleKeyName|ResolveWorktreeArg'`
Expected: FAIL — undefined `SpawnAgent`, `AgentSend`, `ServiceForDir`, …

- [ ] **Step 3: Implement `agentverbs.go`**

```go
package domain

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/homeend/gigagit/internal/template"
)

// MaxBriefBytes caps an agent_start prompt (the brief).
const MaxBriefBytes = 256 << 10

type AgentEntry struct {
	ID       string    `json:"id"`
	Parent   string    `json:"parent,omitempty"`
	Tool     string    `json:"tool"`
	Label    string    `json:"label"`
	Worktree string    `json:"worktree"`
	State    string    `json:"state"`
	ExitCode int       `json:"exit_code"`
	Started  time.Time `json:"started"`
	Spawned  bool      `json:"spawned"`
	Mine     bool      `json:"mine"`
}

// AgentList is every session of this process; Mine marks caller's descendants.
func AgentList(caller string) []AgentEntry {
	var out []AgentEntry
	for _, in := range Sessions().List() {
		full := FullSessionID(in.ID)
		rec, _ := AgentRecord(full)
		out = append(out, AgentEntry{ID: full, Parent: rec.Parent, Tool: in.Label, Label: in.Label,
			Worktree: in.Dir, State: sessionStateName(in.State), ExitCode: in.ExitCode,
			Started: in.Started, Spawned: rec.Spawned, Mine: AgentDescends(full, caller)})
	}
	return out
}

func sessionOf(full string) (*AgentSession, error) {
	id := localID(full)
	if id == "" {
		return nil, fmt.Errorf("%s is not a session of this gg", full)
	}
	s, ok := Sessions().Get(id)
	if !ok {
		return nil, fmt.Errorf("no session %s", full)
	}
	return s, nil
}

// AgentScreen is target's visible screen as plain text (trailing blanks trimmed).
func AgentScreen(target string) (string, string, error) {
	s, err := sessionOf(target)
	if err != nil {
		return "", "", err
	}
	lines := strings.Split(s.Text(), "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return sessionStateName(s.Info().State), strings.TrimRight(strings.Join(lines, "\n"), "\n"), nil
}

// reach refuses a target that is not caller's descendant (spec §5).
func reach(caller, target string) error {
	if AgentDescends(target, caller) {
		return nil
	}
	by := "the user"
	if rec, ok := AgentRecord(target); ok && rec.Parent != "" {
		by = rec.Parent
	}
	return fmt.Errorf("%s is not an agent you started (started by %s)", target, by)
}

// AgentSend pastes text (bracketed when the child asked for it), then Enter
// when enter is set, then each key in keys.
func AgentSend(caller, target, text string, enter bool, keys []string) error {
	if err := reach(caller, target); err != nil {
		return err
	}
	s, err := sessionOf(target)
	if err != nil {
		return err
	}
	if s.Info().State != SessionRunning {
		return fmt.Errorf("%s has exited", target)
	}
	var evs []ConsoleInput
	for _, k := range keys {
		ck, err := ParseConsoleKeyName(k)
		if err != nil {
			return err
		}
		in, err := ConsoleKeyEvent(ck)
		if err != nil {
			return err
		}
		evs = append(evs, in)
	}
	if text != "" {
		s.Paste(text)
	}
	if enter {
		s.SendKey(uv.KeyPressEvent{Code: uv.KeyEnter})
	}
	for _, in := range evs {
		if in.IsKey {
			s.SendKey(in.Key)
		} else {
			s.SendText(in.Text)
		}
	}
	return nil
}

// AgentKill ends target (and forgets it with remove).
func AgentKill(caller, target string, remove bool) error {
	if err := reach(caller, target); err != nil {
		return err
	}
	id := localID(target)
	if remove {
		return Sessions().KillAndRemove(id)
	}
	return Sessions().Kill(id)
}

// AgentTask is the caller's own brief.
func AgentTask(caller string) (SpawnRecord, error) {
	rec, ok := AgentRecord(caller)
	if !ok || !rec.Spawned {
		return SpawnRecord{}, errors.New("you were not started by an agent: there is no task")
	}
	return rec, nil
}

// ParseConsoleKeyName reads "enter", "esc", "ctrl+c", "shift+tab", "1",
// "space" into the web console's wire key.
func ParseConsoleKeyName(s string) (ConsoleKey, error) {
	orig, mod := s, 0
	for {
		switch {
		case strings.HasPrefix(s, "ctrl+"):
			mod, s = mod|ModCtrl, s[5:]
			continue
		case strings.HasPrefix(s, "alt+"):
			mod, s = mod|ModAlt, s[4:]
			continue
		case strings.HasPrefix(s, "shift+"):
			mod, s = mod|ModShift, s[6:]
			continue
		}
		break
	}
	if s == "space" {
		return ConsoleKey{K: "char", Mod: mod, Text: " "}, nil
	}
	if _, ok := consoleKeyNames[s]; ok {
		return ConsoleKey{K: s, Mod: mod}, nil
	}
	if utf8.RuneCountInString(s) == 1 {
		return ConsoleKey{K: "char", Mod: mod, Text: s}, nil
	}
	return ConsoleKey{}, fmt.Errorf("unknown key %q", orig)
}

var (
	svcCacheMu sync.Mutex
	svcCache   = map[string]*Service{}
)

// ServiceForDir is a Service rooted at dir, cached: the agent channel works
// in the CALLER's repository, whatever the TUI shows now (spec §7 reRoot).
func ServiceForDir(dir string) *Service {
	key := filepath.Clean(dir)
	svcCacheMu.Lock()
	defer svcCacheMu.Unlock()
	if s, ok := svcCache[key]; ok {
		return s
	}
	s := Open(key)
	svcCache[key] = s
	return s
}

// cacheService pins dir's Service (tests: one with a test registry dir).
func cacheService(dir string, s *Service) {
	svcCacheMu.Lock()
	svcCache[filepath.Clean(dir)] = s
	svcCacheMu.Unlock()
}

// ResolveWorktreeArg maps a path, a worktree directory name or a branch name
// to a worktree path of this repository.
func (s *Service) ResolveWorktreeArg(ctx context.Context, arg string) (string, error) {
	wts, err := s.Worktrees(ctx)
	if err != nil {
		return "", err
	}
	var hits []string
	for _, w := range wts {
		if SameCheckout(w.Path, arg) {
			return w.Path, nil
		}
		if filepath.Base(w.Path) == arg || w.Branch == arg {
			hits = append(hits, w.Path)
		}
	}
	switch len(hits) {
	case 0:
		return "", fmt.Errorf("no worktree %q in this repository", arg)
	case 1:
		return hits[0], nil
	}
	return "", fmt.Errorf("%q names several worktrees: %s", arg, strings.Join(hits, ", "))
}

type AgentStartRequest struct{ Caller, Worktree, Tool, Prompt, Note string }

type AgentStartResult struct {
	ID       string `json:"id"`
	Worktree string `json:"worktree"`
	Tool     string `json:"tool"`
	Warning  string `json:"warning,omitempty"`
}

// SpawnSpec is what the hosting frontend adds to a request: the console
// size, its child env (GG_INBOX), the channel URL and the approval lookup.
type SpawnSpec struct {
	Req        AgentStartRequest
	Cols, Rows int
	Env        []string
	MCPURL     string
	Approved   func(repoKey, command string) bool
}

// SpawnAgent is agent_start (spec §4.1): checks, claim, the normal session
// start with the kick-off prompt, the record, the claim handover.
func SpawnAgent(ctx context.Context, sp SpawnSpec) (AgentStartResult, *AgentSession, error) {
	req := sp.Req
	callerSess, live := runningLocal(req.Caller)
	if !live {
		return AgentStartResult{}, nil, fmt.Errorf("caller %s is not a running agent of this gg", req.Caller)
	}
	if rec, ok := AgentRecord(req.Caller); ok && rec.Spawned {
		return AgentStartResult{}, nil, errors.New("a spawned agent may not start agents")
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return AgentStartResult{}, nil, errors.New("the prompt is empty: pass the worker's task")
	}
	if len(req.Prompt) > MaxBriefBytes {
		return AgentStartResult{}, nil, fmt.Errorf("the prompt is larger than %d bytes", MaxBriefBytes)
	}
	svc := ServiceForDir(callerSess.Info().Dir)
	cfg, err := svc.EffectiveConfig(ctx)
	if err != nil {
		return AgentStartResult{}, nil, err
	}
	var tc *config.ToolCommand
	for _, c := range SessionCommands(cfg, "tui") {
		if strings.EqualFold(c.Name, req.Tool) {
			c := c
			tc = &c
			break
		}
	}
	if tc == nil {
		return AgentStartResult{}, nil, fmt.Errorf("no session command named %q", req.Tool)
	}
	if len(cfg.Agents.Spawn) == 0 {
		return AgentStartResult{}, nil, errors.New("spawning is off — add the command name to [agents] spawn in the global config")
	}
	allowed := false
	for _, n := range cfg.Agents.Spawn {
		allowed = allowed || strings.EqualFold(n, tc.Name)
	}
	if !allowed {
		return AgentStartResult{}, nil, fmt.Errorf("%s is not in [agents] spawn", tc.Name)
	}
	if !template.HasPromptSlot(tc.Command) {
		return AgentStartResult{}, nil, fmt.Errorf("%s has no <prompt> slot — accept its template update in Settings → External tools, or add <prompt> to its command", tc.Name)
	}
	repoKey, _ := svc.GitCommonDir(ctx)
	if sp.Approved == nil || !sp.Approved(repoKey, tc.Command) {
		return AgentStartResult{}, nil, fmt.Errorf("approve %s once by starting it from the TUI (Start agent), then retry", tc.Name)
	}
	release, ok := reserveSpawnSlot(cfg.Agents.SpawnCap())
	if !ok {
		return AgentStartResult{}, nil, fmt.Errorf("%d spawned agents are running — the max_spawned cap; wait or kill one", cfg.Agents.SpawnCap())
	}
	started := false
	defer func() { release(started) }()
	path, err := svc.ResolveWorktreeArg(ctx, req.Worktree)
	if err != nil {
		return AgentStartResult{}, nil, err
	}
	held, err := svc.claimHeldBy(ctx, path, req.Caller)
	if err != nil {
		return AgentStartResult{}, nil, err
	}
	created := false
	if !held {
		ac, _, aerr := svc.AgentsConfig(ctx)
		if aerr != nil {
			return AgentStartResult{}, nil, aerr
		}
		pol, perr := PolicyFromConfig(ac)
		if perr != nil {
			return AgentStartResult{}, nil, perr
		}
		if err := svc.ClaimWorktree(ctx, path, req.Caller, req.Note, pol); err != nil {
			return AgentStartResult{}, nil, err
		}
		created = true
	}
	sess, _, err := svc.StartAgentSession(ctx, *tc, path, "", sp.Cols, sp.Rows, sp.Env, sp.MCPURL,
		SpawnRecord{Parent: req.Caller, Brief: req.Prompt, Worktree: path, Spawned: true}, AgentKickoff)
	if err != nil {
		if created {
			_, _ = svc.ReleaseWorktree(ctx, path, req.Caller, false)
		}
		return AgentStartResult{}, nil, err
	}
	started = true
	res := AgentStartResult{ID: FullSessionID(sess.Info().ID), Worktree: path, Tool: tc.Name}
	if err := svc.HandOverWorktree(ctx, path, req.Caller, res.ID); err != nil {
		res.Warning = "the worker runs, but its claim stayed with you: " + err.Error()
	}
	return res, sess, nil
}
```

Add `"github.com/homeend/gigagit/internal/config"` to the imports; the uv import line is the one `console_keys.go` uses. `Reserved`/`StaleAfter` come from `svc.AgentsConfig` (anchored on the main worktree, stage 1's rule) even when the caller runs in a linked worktree; `Spawn`/`MaxSpawned` are global-only so `cfg` carries them either way.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/domain -run 'Spawn|Agent|ConsoleKeyName|ResolveWorktreeArg|Claim|Session'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git -C /work/gigagit/.claude/worktrees/agent-spawn add internal/domain/agentverbs.go internal/domain/agentverbs_test.go
git -C /work/gigagit/.claude/worktrees/agent-spawn commit -m "feat(domain): agent verbs (list/screen/send/kill/task) and SpawnAgent — allow-list, prompt slot, one-time approval, cap, no nesting, claim handover"
```

---

### Task 7: the MCP agent host and tools

**Files:**
- Create: `internal/mcp/agenthost.go`, `internal/mcp/agenttools.go`, `internal/mcp/agenthost_test.go`

**Interfaces:**
- Consumes: Task 5/6 domain API.
- Produces:
  - `type Starter func(ctx context.Context, req domain.AgentStartRequest) (domain.AgentStartResult, error)`
  - `func NewAgentHost() *AgentHost`; `(*AgentHost) Start(starter Starter) (string, error)` (listens on `127.0.0.1:0`, returns `http://127.0.0.1:<port>/mcp`); `URL() string`; `Close()`; `Handler(starter Starter) http.Handler` (tests)
  - `func RegisterAgentTools(srv *sdk.Server, caller func(*sdk.CallToolRequest) (string, error), starter Starter)` (plan B reuses it in the forwarder)
  - Tool names: `agent_start`, `agent_list`, `agent_screen`, `agent_send`, `agent_kill`, `agent_task`

- [ ] **Step 1: Write the failing tests** (`agenthost_test.go`)

```go
package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
)

type bearer struct{ tok string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.tok)
	return http.DefaultTransport.RoundTrip(r)
}

func agentClient(t *testing.T, url, tok string) (*sdk.ClientSession, error) {
	t.Helper()
	tr := &sdk.StreamableClientTransport{Endpoint: url, HTTPClient: &http.Client{Transport: bearer{tok}}, MaxRetries: -1, DisableStandaloneSSE: true}
	return sdk.NewClient(&sdk.Implementation{Name: "t", Version: "0"}, nil).Connect(context.Background(), tr, nil)
}

// hostEnv: a real repo, a fresh session manager, one agent session with a
// token, and the host's handler behind httptest.
func hostEnv(t *testing.T, starter Starter) (url, tok, full string) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	restore := domain.UseSessionManager(agentsession.NewManager())
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	gitRun(t, dir, "commit", "--allow-empty", "-m", "seed")
	svc := domain.Open(dir)
	ts := httptest.NewServer(NewAgentHost().Handler(starter))
	tc := config.ToolCommand{Category: "session", Name: "Sh", Mode: "session", Command: "sh -c 'sleep 600'"}
	s, tok, err := svc.StartAgentSession(context.Background(), tc, dir, "", 80, 24, nil, ts.URL+"/mcp", domain.SpawnRecord{}, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ts.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		domain.Sessions().KillAll(ctx)
		restore()
	})
	return ts.URL + "/mcp", tok, domain.FullSessionID(s.Info().ID)
}

func TestAgentToolsOverHTTP(t *testing.T) {
	var got domain.AgentStartRequest
	url, tok, full := hostEnv(t, func(_ context.Context, r domain.AgentStartRequest) (domain.AgentStartResult, error) {
		got = r
		return domain.AgentStartResult{ID: "p/s9", Worktree: "/w", Tool: r.Tool}, nil
	})
	cs, err := agentClient(t, url, tok)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "agent_start",
		Arguments: map[string]any{"worktree": "/w", "tool": "Claude", "prompt": "do it", "note": "n"}})
	if err != nil || res.IsError {
		t.Fatalf("agent_start = %v %+v", err, res)
	}
	if got.Caller != full || got.Tool != "Claude" || got.Prompt != "do it" || got.Note != "n" {
		t.Fatalf("the starter must receive the AUTHENTICATED caller: %+v", got)
	}
	res, err = cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "agent_list", Arguments: map[string]any{}})
	if err != nil || res.IsError || !strings.Contains(resultText(res), full) {
		t.Fatalf("agent_list = %v %s", err, resultText(res))
	}
	res, _ = cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "agent_task", Arguments: map[string]any{}})
	if !res.IsError || !strings.Contains(resultText(res), "not started by an agent") {
		t.Fatalf("agent_task for a manual session = %s", resultText(res))
	}
}

func TestMissingOrUnknownTokenIs401(t *testing.T) {
	url, _, _ := hostEnv(t, nil)
	for _, tok := range []string{"", strings.Repeat("a", 64)} {
		if _, err := agentClient(t, url, tok); err == nil {
			t.Fatalf("token %q connected", tok)
		}
	}
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader("{}"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no bearer = %v %v", resp.StatusCode, err)
	}
}

func TestKilledSessionTokenIs401(t *testing.T) {
	url, tok, full := hostEnv(t, nil)
	cs, err := agentClient(t, url, tok)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	s, _ := domain.Sessions().Get(domain.SessionID(full[strings.LastIndex(full, "/")+1:]))
	domain.Sessions().Kill(s.Info().ID)
	<-s.Done()
	if res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "agent_list", Arguments: map[string]any{}}); err == nil && !res.IsError {
		t.Fatal("a killed session's token still works")
	}
}

func TestOriginHeaderRefused(t *testing.T) {
	url, tok, _ := hostEnv(t, nil)
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Origin", "http://evil.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a browser Origin = %v %v", resp.StatusCode, err)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/mcp -run 'AgentTools|Token|Origin'`
Expected: FAIL — `undefined: NewAgentHost`, `Starter`.

- [ ] **Step 3: Implement `agenthost.go`**

```go
package mcp

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/homeend/gigagit/internal/buildinfo"
	"github.com/homeend/gigagit/internal/domain"
)

// Starter runs agent_start inside the hosting TUI (it owns the console
// size, the child env and the status line).
type Starter func(ctx context.Context, req domain.AgentStartRequest) (domain.AgentStartResult, error)

// AgentHost is the agent channel a TUI serves: MCP over streamable HTTP on
// loopback; every request authenticated by the calling session's token.
type AgentHost struct {
	srv *http.Server
	url string
}

func NewAgentHost() *AgentHost { return &AgentHost{} }

const agentInstructions = "gg agent channel. These tools exist only for an agent running inside a gg console. " +
	"A worker agent's first act is agent_task (its brief). agent_start starts a worker in a worktree " +
	"(the gg [agents] spawn allow-list governs which session commands); agent_send/agent_kill reach only agents you started."

// Handler is the authenticated MCP endpoint (tests mount it on httptest).
func (h *AgentHost) Handler(starter Starter) http.Handler {
	srv := sdk.NewServer(&sdk.Implementation{Name: "gg-agents", Version: buildinfo.Version},
		&sdk.ServerOptions{Instructions: agentInstructions})
	RegisterAgentTools(srv, callerFromToken, starter)
	mcpH := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return srv }, nil)
	authed := auth.RequireBearerToken(verifyToken, nil)(mcpH)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != "" {
			http.Error(w, "browsers may not call the gg agent channel", http.StatusForbidden)
			return
		}
		authed.ServeHTTP(w, r)
	})
}

func verifyToken(_ context.Context, tok string, _ *http.Request) (*auth.TokenInfo, error) {
	id, ok := domain.VerifyAgentToken(tok)
	if !ok {
		return nil, auth.ErrInvalidToken
	}
	return &auth.TokenInfo{UserID: id, Expiration: time.Now().Add(24 * time.Hour)}, nil
}

func callerFromToken(req *sdk.CallToolRequest) (string, error) {
	if req.Extra == nil || req.Extra.TokenInfo == nil || req.Extra.TokenInfo.UserID == "" {
		return "", errors.New("not running inside a gg console (no session token)")
	}
	return req.Extra.TokenInfo.UserID, nil
}

// Start listens on a random loopback port and serves until Close.
func (h *AgentHost) Start(starter Starter) (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	mux := http.NewServeMux()
	mux.Handle("/mcp", h.Handler(starter))
	h.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	h.url = "http://" + ln.Addr().String() + "/mcp"
	go func() { _ = h.srv.Serve(ln) }()
	return h.url, nil
}

func (h *AgentHost) URL() string { return h.url }

func (h *AgentHost) Close() {
	if h.srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = h.srv.Shutdown(ctx)
	}
}
```

`agenttools.go`:

```go
package mcp

import (
	"context"
	"errors"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/homeend/gigagit/internal/domain"
)

type agentStartIn struct {
	Worktree string `json:"worktree" jsonschema:"worktree path, directory name or branch"`
	Tool     string `json:"tool" jsonschema:"session command name, as in [agents] spawn"`
	Prompt   string `json:"prompt" jsonschema:"the worker's task (its brief), up to 256 KiB"`
	Note     string `json:"note,omitempty" jsonschema:"note on the worktree claim, e.g. the issue URL"`
}
type agentIDIn struct {
	ID string `json:"id" jsonschema:"a session id as agent_list prints it"`
}
type agentSendIn struct {
	ID    string   `json:"id"`
	Text  string   `json:"text,omitempty" jsonschema:"text to paste"`
	Enter *bool    `json:"enter,omitempty" jsonschema:"press Enter after the text (default true)"`
	Keys  []string `json:"keys,omitempty" jsonschema:"keys after the text: enter, esc, tab, up, down, ctrl+c, 1, space…"`
}
type agentKillIn struct {
	ID     string `json:"id"`
	Remove bool   `json:"remove,omitempty" jsonschema:"also forget the session once it exited"`
}
type agentListOut struct {
	Agents []domain.AgentEntry `json:"agents"`
}
type agentScreenOut struct {
	ID    string `json:"id"`
	State string `json:"state"`
	Text  string `json:"text"`
}
type agentTaskOut struct {
	Brief    string `json:"brief"`
	Parent   string `json:"parent"`
	Worktree string `json:"worktree"`
}
type empty struct{}

// RegisterAgentTools adds the six agent tools; caller names the
// authenticated session of a request, starter runs agent_start.
func RegisterAgentTools(srv *sdk.Server, caller func(*sdk.CallToolRequest) (string, error), starter Starter) {
	sdk.AddTool(srv, &sdk.Tool{Name: "agent_start", Description: "Start a worker agent in a worktree with a task; the worktree claim passes to the worker."},
		func(ctx context.Context, req *sdk.CallToolRequest, in agentStartIn) (*sdk.CallToolResult, domain.AgentStartResult, error) {
			who, err := caller(req)
			if err != nil {
				return nil, domain.AgentStartResult{}, err
			}
			if starter == nil {
				return nil, domain.AgentStartResult{}, errors.New("this gg cannot start agents")
			}
			res, err := starter(ctx, domain.AgentStartRequest{Caller: who, Worktree: in.Worktree, Tool: in.Tool, Prompt: in.Prompt, Note: in.Note})
			return nil, res, err
		})
	sdk.AddTool(srv, &sdk.Tool{Name: "agent_list", Description: "Every agent session of this gg; mine = started by you.", Annotations: readOnlyAnnotations()},
		func(_ context.Context, req *sdk.CallToolRequest, _ empty) (*sdk.CallToolResult, agentListOut, error) {
			who, err := caller(req)
			if err != nil {
				return nil, agentListOut{}, err
			}
			return nil, agentListOut{Agents: domain.AgentList(who)}, nil
		})
	sdk.AddTool(srv, &sdk.Tool{Name: "agent_screen", Description: "A session's visible console text.", Annotations: readOnlyAnnotations()},
		func(_ context.Context, req *sdk.CallToolRequest, in agentIDIn) (*sdk.CallToolResult, agentScreenOut, error) {
			if _, err := caller(req); err != nil {
				return nil, agentScreenOut{}, err
			}
			st, text, err := domain.AgentScreen(in.ID)
			return nil, agentScreenOut{ID: in.ID, State: st, Text: text}, err
		})
	sdk.AddTool(srv, &sdk.Tool{Name: "agent_send", Description: "Type into an agent you started: paste text, Enter (default), then keys."},
		func(_ context.Context, req *sdk.CallToolRequest, in agentSendIn) (*sdk.CallToolResult, empty, error) {
			who, err := caller(req)
			if err != nil {
				return nil, empty{}, err
			}
			enter := in.Enter == nil || *in.Enter
			return nil, empty{}, domain.AgentSend(who, in.ID, in.Text, enter, in.Keys)
		})
	sdk.AddTool(srv, &sdk.Tool{Name: "agent_kill", Description: "End an agent you started."},
		func(_ context.Context, req *sdk.CallToolRequest, in agentKillIn) (*sdk.CallToolResult, empty, error) {
			who, err := caller(req)
			if err != nil {
				return nil, empty{}, err
			}
			return nil, empty{}, domain.AgentKill(who, in.ID, in.Remove)
		})
	sdk.AddTool(srv, &sdk.Tool{Name: "agent_task", Description: "Your own task, when an agent started you.", Annotations: readOnlyAnnotations()},
		func(_ context.Context, req *sdk.CallToolRequest, _ empty) (*sdk.CallToolResult, agentTaskOut, error) {
			who, err := caller(req)
			if err != nil {
				return nil, agentTaskOut{}, err
			}
			rec, err := domain.AgentTask(who)
			return nil, agentTaskOut{Brief: rec.Brief, Parent: rec.Parent, Worktree: rec.Worktree}, err
		})
}
```

(A handler returning a non-nil error becomes an `IsError` tool result in go-sdk — that is the protocol's refusal channel. If `readOnlyAnnotations` lives in another file of the package, reuse it; it exists — `grep -n "func readOnlyAnnotations" internal/mcp`.)

- [ ] **Step 4: Run tests**

Run: `go test ./internal/mcp ./internal/archtest`
Expected: PASS (archtest: `mcp` non-test imports stay domain-only; the test file's `agentsession` import is a test import, which `go list .Imports` excludes).

- [ ] **Step 5: Commit**

```bash
git -C /work/gigagit/.claude/worktrees/agent-spawn add internal/mcp/agenthost.go internal/mcp/agenttools.go internal/mcp/agenthost_test.go
git -C /work/gigagit/.claude/worktrees/agent-spawn commit -m "feat(mcp): the agent channel host — streamable HTTP on loopback, per-request bearer verification, six agent tools"
```

---

### Task 8: the TUI owns the host; manual and spawned starts get the channel

**Files:**
- Create: `internal/tui/agenthost.go`, `internal/tui/agenthost_test.go`
- Modify: `internal/tui/model.go` (field `agentHost *agentHostState`, Init batch, dispatch cases)
- Modify: `internal/tui/run.go` (start/close the host)
- Modify: `internal/tui/agent_start_popup.go` (`start` uses `StartAgentSession`)
- Modify: `cmd/gg/main.go` (`tui.NewAgentHost`)
- Modify: `internal/i18n/lang/{ja,ko,zh,ru}.toml`

**Interfaces:**
- Consumes: Task 6 `domain.SpawnAgent`, `SpawnSpec`; Task 7 `mcp.NewAgentHost`, `mcp.Starter` (only in `cmd/gg`).
- Produces:
  - `type AgentHost interface { Start(starter func(context.Context, domain.AgentStartRequest) (domain.AgentStartResult, error)) (string, error); Close() }`
  - `var NewAgentHost func() AgentHost` (set by cmd/gg)
  - `func (m Model) agentURL() string`
  - messages `agentSpawnRequestMsg{req domain.AgentStartRequest; reply chan agentSpawnReply}`, `agentSpawnedMsg{res domain.AgentStartResult; err error; reply chan agentSpawnReply; inbox string; id domain.SessionID}`

- [ ] **Step 1: Write the failing tests** (`agenthost_test.go`, serial)

```go
package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

func TestAgentSpawnRequestRoundTrip(t *testing.T) {
	st := newAgentHostState()
	st.url = "http://127.0.0.1:1/mcp"
	m := sizedModel(t, 120, 40)
	m.agentHost = st
	focus := m.focus
	var spawned domain.SpawnSpec
	agentSpawn = func(_ context.Context, sp domain.SpawnSpec) (domain.AgentStartResult, *domain.AgentSession, error) {
		spawned = sp
		return domain.AgentStartResult{ID: "p/s7", Worktree: "/w/job", Tool: "Claude"}, nil, nil
	}
	t.Cleanup(func() { agentSpawn = domain.SpawnAgent })
	starter := starterFor(st)
	done := make(chan domain.AgentStartResult, 1)
	go func() {
		res, err := starter(context.Background(), domain.AgentStartRequest{Caller: "p/s1", Worktree: "/w/job", Tool: "Claude", Prompt: "x"})
		if err != nil {
			t.Error(err)
		}
		done <- res
	}()
	req := (waitAgentSpawnCmd(st)()).(agentSpawnRequestMsg)
	nm, cmd := m.onAgentSpawnRequest(req)
	msg := cmd().(tea.BatchMsg)[0]().(agentSpawnedMsg) // the batch's first cmd is the spawn
	nm2, _ := nm.onAgentSpawned(msg)
	select {
	case res := <-done:
		if res.ID != "p/s7" {
			t.Fatalf("reply %+v", res)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the starter never got its reply")
	}
	if spawned.MCPURL != st.url || spawned.Cols < 20 || spawned.Approved == nil {
		t.Fatalf("spec %+v", spawned)
	}
	if !strings.Contains(nm2.statusMsg, "Claude") || nm2.focus != focus {
		t.Fatalf("status %q, focus %v→%v: a spawned worker never takes focus", nm2.statusMsg, focus, nm2.focus)
	}
}

func TestStarterRefusesWhenClosing(t *testing.T) {
	st := newAgentHostState()
	close(st.stop)
	if _, err := starterFor(st)(context.Background(), domain.AgentStartRequest{}); err == nil {
		t.Fatal("a closing TUI must refuse")
	}
}
```

(Imports: add `tea "github.com/charmbracelet/bubbletea"`.)

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/tui -run 'AgentSpawnRequest|StarterRefuses'`
Expected: FAIL — undefined `newAgentHostState`, `starterFor`, …

- [ ] **Step 3: Implement `internal/tui/agenthost.go`**

```go
package tui

import (
	"context"
	"errors"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// AgentHost is the agent MCP channel served from THIS process (cmd/gg sets
// NewAgentHost to internal/mcp's host; the frontends never import each
// other). nil = unavailable: sessions then get no GG_MCP_URL.
type AgentHost interface {
	Start(starter func(context.Context, domain.AgentStartRequest) (domain.AgentStartResult, error)) (string, error)
	Close()
}

var NewAgentHost func() AgentHost

// agentSpawn is a test seam over domain.SpawnAgent.
var agentSpawn = domain.SpawnAgent

type agentHostState struct {
	host     AgentHost
	url      string
	requests chan agentSpawnRequestMsg
	stop     chan struct{}
}

type agentSpawnReply struct {
	res domain.AgentStartResult
	err error
}

type agentSpawnRequestMsg struct {
	req   domain.AgentStartRequest
	reply chan agentSpawnReply
}

type agentSpawnedMsg struct {
	res   domain.AgentStartResult
	id    domain.SessionID
	err   error
	reply chan agentSpawnReply
	inbox string
}

func newAgentHostState() *agentHostState {
	return &agentHostState{requests: make(chan agentSpawnRequestMsg), stop: make(chan struct{})}
}

// starterFor hands an agent_start into Update and waits for the answer.
func starterFor(st *agentHostState) func(context.Context, domain.AgentStartRequest) (domain.AgentStartResult, error) {
	return func(ctx context.Context, req domain.AgentStartRequest) (domain.AgentStartResult, error) {
		reply := make(chan agentSpawnReply, 1)
		select {
		case st.requests <- agentSpawnRequestMsg{req: req, reply: reply}:
		case <-st.stop:
			return domain.AgentStartResult{}, errors.New("gg is closing")
		case <-ctx.Done():
			return domain.AgentStartResult{}, ctx.Err()
		}
		select {
		case r := <-reply:
			return r.res, r.err
		case <-st.stop:
			return domain.AgentStartResult{}, errors.New("gg is closing")
		case <-ctx.Done():
			return domain.AgentStartResult{}, ctx.Err()
		}
	}
}

func waitAgentSpawnCmd(st *agentHostState) tea.Cmd {
	if st == nil {
		return nil
	}
	return func() tea.Msg {
		select {
		case r := <-st.requests:
			return r
		case <-st.stop:
			return nil
		}
	}
}

// startAgentHost serves the channel before the program starts (Run).
func (m Model) startAgentHost() Model {
	if NewAgentHost == nil {
		return m
	}
	st := newAgentHostState()
	h := NewAgentHost()
	url, err := h.Start(starterFor(st))
	if err != nil {
		m.statusMsg = i18n.T("agent channel unavailable: %s", err.Error())
		return m
	}
	st.host, st.url = h, url
	m.agentHost = st
	return m
}

func (m Model) closeAgentHost() {
	if m.agentHost != nil && m.agentHost.host != nil {
		close(m.agentHost.stop)
		m.agentHost.host.Close()
	}
}

func (m Model) agentURL() string {
	if m.agentHost == nil {
		return ""
	}
	return m.agentHost.url
}

// onAgentSpawnRequest supplies what only the TUI knows — the console size,
// its child env, the approval store — and runs the spawn off the UI thread;
// the wait re-arms at once so a second request is never stuck behind this one.
func (m Model) onAgentSpawnRequest(msg agentSpawnRequestMsg) (Model, tea.Cmd) {
	g := m.layout()
	cols, rows := consoleInner(g.rightW, g.boxH[panelCommits])
	store := m.promptStore
	sp := domain.SpawnSpec{Req: msg.req, Cols: cols, Rows: rows, Env: m.childEnv(), MCPURL: m.agentURL(),
		Approved: func(repoKey, command string) bool {
			return store != nil && store.ApprovedToolCommands(repoKey)[toolCommandHash(command)]
		}}
	inbox := m.childInboxDir()
	spawn := func() tea.Msg {
		res, sess, err := agentSpawn(context.Background(), sp)
		out := agentSpawnedMsg{res: res, err: err, reply: msg.reply, inbox: inbox}
		if sess != nil {
			out.id = sess.Info().ID
		}
		return out
	}
	return m, tea.Batch(spawn, waitAgentSpawnCmd(m.agentHost))
}

// onAgentSpawned answers the agent, keeps the child's inbox answered, and
// says so on the status line — never opening the console (ruling 11).
func (m Model) onAgentSpawned(msg agentSpawnedMsg) (Model, tea.Cmd) {
	msg.reply <- agentSpawnReply{res: msg.res, err: msg.err}
	if msg.err != nil {
		return m, nil
	}
	if msg.inbox != "" && msg.id != "" {
		m.childInbox[msg.id] = msg.inbox
	}
	m.statusMsg = i18n.T("%s started in %s by an agent", msg.res.Tool, shortWorktreeName(msg.res.Worktree))
	if msg.res.Warning != "" {
		m.statusMsg += " — " + i18n.T("its worktree claim stayed with the agent that started it")
	}
	var cmd tea.Cmd
	m, cmd = m.reloadSourcesCmd([]sourceKey{srcWorktrees}, reloadOpts{})
	return m, cmd
}
```

Notes for this step:
- `m.childInbox` may be nil in a literal Model: guard with `if m.childInbox == nil { m.childInbox = map[domain.SessionID]string{} }` before the write (check its declared type in model.go:342 and match it).

`model.go`: add the field `agentHost *agentHostState // the agent MCP channel (agenthost.go); pointer: survives the value copy`; append `waitAgentSpawnCmd(m.agentHost)` to the `Init` batch; in `dispatch` add

```go
	case agentSpawnRequestMsg:
		return m.onAgentSpawnRequest(msg)
	case agentSpawnedMsg:
		return m.onAgentSpawned(msg)
```

`run.go`: after `m = m.initSteerInbox()` add `m = m.startAgentHost()`; in the tail after `fm.closeWeb()` add `fm.closeAgentHost()`. If the program errors before `final` is a Model, close via the pre-run `m`: add `defer m.closeAgentHost()` right after starting it instead, and drop the tail call (one close path; `close(st.stop)` must run once — guard with a `sync.Once` field `closed` in `agentHostState`).

`agent_start_popup.go` `start`: capture `url := m.agentURL()` and replace the `StartSession` call with

```go
		s, _, err := svc.StartAgentSession(context.Background(), tc, dir, cwd, cols, rows, env, url, domain.SpawnRecord{}, "")
```

`cmd/gg/main.go` next to `tui.NewWebHost`:

```go
	tui.NewAgentHost = func() tui.AgentHost { return agentHostAdapter{mcp.NewAgentHost()} }
```

and at file end:

```go
// agentHostAdapter converts the TUI's starter func to mcp.Starter.
type agentHostAdapter struct{ h *mcp.AgentHost }

func (a agentHostAdapter) Start(s func(context.Context, domain.AgentStartRequest) (domain.AgentStartResult, error)) (string, error) {
	return a.h.Start(mcp.Starter(s))
}
func (a agentHostAdapter) Close() { a.h.Close() }
```

i18n — add to each of `internal/i18n/lang/{ja,ko,zh,ru}.toml`:

```toml
"agent channel unavailable: %s" = "<translation>"
"%s started in %s by an agent" = "<translation>"
"its worktree claim stayed with the agent that started it" = "<translation>"
```

with real translations, e.g. ja: `"エージェントチャネルを利用できません: %s"`, `"%s をエージェントが %s で起動しました"`, `"ワークツリーのクレームは起動元のエージェントに残りました"`; ko: `"에이전트 채널을 사용할 수 없음: %s"`, `"에이전트가 %[2]s에서 %[1]s을(를) 시작함"`, `"워크트리 클레임은 시작한 에이전트에 남았습니다"`; zh: `"代理通道不可用：%s"`, `"代理在 %[2]s 中启动了 %[1]s"`, `"工作树认领仍归启动它的代理所有"`; ru: `"канал агентов недоступен: %s"`, `"агент запустил %s в %s"`, `"заявка на рабочее дерево осталась у запустившего агента"`. (Check the bundles accept `%[n]s` reordering — `grep -n '%\[2\]' internal/i18n/lang/ko.toml | head -1`; if none exists, keep the source order.)

- [ ] **Step 4: Run tests**

Run: `go test ./internal/tui -run 'AgentSpawn|StarterRefuses|I18n|Vocab|MenuLabels|EngineProse|Session|AgentStart' && go build ./cmd/gg && go test ./internal/archtest`
Expected: PASS; the binary builds.

- [ ] **Step 5: Commit**

```bash
git -C /work/gigagit/.claude/worktrees/agent-spawn add internal/tui internal/i18n cmd/gg
git -C /work/gigagit/.claude/worktrees/agent-spawn commit -m "feat(tui): the TUI serves the agent channel — agent_start runs through the Update loop on the normal session path; every agent session gets GG_MCP_URL and its token"
```

---

### Task 9: full gate

- [ ] **Step 1:** Run `cd /work/gigagit/.claude/worktrees/agent-spawn && ./test.sh race > /tmp/claude-1000/-work-gigagit/63e7bf78-bedf-425b-857e-5933228d7cb7/scratchpad/race-a.log 2>&1; grep -c "all green" /tmp/claude-1000/-work-gigagit/63e7bf78-bedf-425b-857e-5933228d7cb7/scratchpad/race-a.log`
  Expected: `1` (the log says "all green"; a `| tail` exit code is NOT evidence — memory race-gate-check-all-green).
- [ ] **Step 2:** Live smoke (headless, throwaway repo): build `bin/gg`, start it under `tui-capture.sh` in a scratch repo with a global config allowing a `sh -c 'sleep 600' <prompt>` command, open a terminal-free agent session via the Branches `.` menu, then from a second shell `curl` the URL with that session's token is NOT possible (token only in the child env) — instead verify `GG_MCP_URL` appears in the session's environment by starting the agent command `sh -c 'env | grep GG_MCP_URL; sleep 600' <prompt>` and reading the console snapshot. Record the result in the ledger.
- [ ] **Step 3:** Commit any fixes with their tests; ledger the gate result.

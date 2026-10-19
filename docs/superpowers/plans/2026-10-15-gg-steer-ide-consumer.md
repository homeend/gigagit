# gg steer — IDE consumer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let `gg session <verb> --to ide` (or `GG_SESSION_TO=ide`) reach an IDE that publishes `ide.json` and drains `<inbox>/ide/`, exactly as the spec says, with conformance material the plugin is built against — and change nothing for TUI/web users.

**Architecture:** Two constants and one helper in `internal/steer` (presence name, sub-inbox dir); the CLI's routing table (`sessionRoute`) gains a third side that is selected only by `--to ide`/`GG_SESSION_TO`, and the file-delivery path (post + await) is parameterised by the side's directory and name; `gg session status` reports it. No TUI/web code changes. Conformance files under `docs/specs/gg-steer/` with tests in `steer`, `agentdocs`, `config` and `tui`.

**Tech Stack:** Go 1.26; `github.com/google/jsonschema-go` (direct, MIT) for the schema tests; the existing CLI test harness (`runCLI`, `livePresence`, `answer` in `internal/cli/session_test.go`).

**Spec:** `docs/superpowers/specs/2026-10-15-gg-steer-ide-consumer-design.md`.

## Global Constraints

- Worktree `/work/gigagit/.claude/worktrees/steer-ide`, branch `feat/steer-ide` off `main`; every path relative to it. The notes-store branch is unrelated and unmerged; do not touch it.
- No new dependency. `internal/steer` production code stays stdlib + fsnotify; test files may import jsonschema-go.
- **Never implicit:** with no `--to` and no `GG_SESSION_TO`, routing and output are byte-for-byte what they are today; every existing `internal/cli` session test must pass unchanged.
- Engine/CLI prose English; the TUI gains no strings (no i18n work).
- Tests: real `git` in `t.TempDir()`; `t.Parallel()` unless `t.Setenv`.
- `gg add` + raw `git commit -F`; never `git add -A`; attribution per the session reminder.
- `./test.sh race` before done; "all green" only.
- Docs after: CHANGELOG, `using-gg.md` + `gg-overview.md`, `agentskill.Version` bump (main is 173 → 174; if the notes-store branch merges first with 174, resolve to 175), dogfood copy via `./bin/gg init --update`, `docs/CLAUDE-details.md` steer paragraph; README has no `gg session` block (checked) — no README change.

## Review Focus

1. `--to ide` while a TUI is live and no IDE: must refuse with `no IDE for this worktree` and post NOTHING to the TUI's inbox (a silent fallback would move the user's terminal). → Task 2 (`TestSessionToIDERefusesWhenOnlyTUIIsLive`).
2. `GG_SESSION_TO=ide` with an explicit `--to tui`: the flag wins. → Task 2 (`TestSessionEnvDefaultLosesToExplicitTo`).
3. An IDE command file left in `<inbox>/ide/` when a TUI starts: the TUI's `Discard` must not delete it. → Task 1 (`TestDiscardLeavesTheIDESubInbox`).
4. `gg session status` with only `ide.json` live: exit 0, `worktree:` from the IDE presence, `ide:` line, JSON `ide` object — and `tui`/`web` null. → Task 3 (`TestSessionStatusPrintsTheLiveIDE`).
5. A reanchor case where the note's text appears twice after an edit: stays outdated (never adopts one of two). → Task 4 (`reanchor-cases.json`, `TestReanchorCases`).

---

### Task 1: `steer` constants + sub-inbox isolation

**Files:**
- Modify: `internal/steer/steer.go:28-47` (constants), add `IDEDir`
- Test: `internal/steer/ide_test.go`

**Interfaces:**
- Produces: `steer.IDEPresence = "ide.json"`, `steer.IDEInbox = "ide"`, `func IDEDir(inbox string) string` (= `filepath.Join(inbox, IDEInbox)`). `Post`/`Drain`/`PostReply`/`AwaitReply`/`Discard` are unchanged and take `IDEDir(inbox)` as their dir.

- [ ] **Step 1: Write the failing test**

`internal/steer/ide_test.go`:

```go
package steer

import (
	"os"
	"path/filepath"
	"testing"
)

// Review focus 3 + spec §2.1: the IDE's sub-inbox is invisible to the TUI's
// Drain and survives its Discard; its own Drain sees only its own files.
func TestDiscardLeavesTheIDESubInbox(t *testing.T) {
	t.Parallel()
	inbox := t.TempDir()
	ide := IDEDir(inbox)
	if ide != filepath.Join(inbox, "ide") {
		t.Fatalf("IDEDir = %q", ide)
	}
	if _, err := Post(ide, Command{ID: "1-1", Cmd: "navigate", File: "a.go"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Post(inbox, Command{ID: "2-2", Cmd: "navigate", File: "b.go"}); err != nil {
		t.Fatal(err)
	}
	if got := Drain(inbox); len(got) != 1 || got[0].ID != "2-2" {
		t.Fatalf("top-level Drain = %+v, want only the TUI's command", got)
	}
	Discard(inbox)
	if _, err := os.Stat(filepath.Join(ide, "cmd-1-1.json")); err != nil {
		t.Fatalf("Discard of the inbox removed the IDE's command: %v", err)
	}
	if got := Drain(ide); len(got) != 1 || got[0].ID != "1-1" || got[0].From != ide {
		t.Fatalf("IDE Drain = %+v", got)
	}
	// Presence names: three distinct files at the top level.
	if IDEPresence != "ide.json" || IDEPresence == TUIPresence || IDEPresence == WebPresence {
		t.Fatalf("IDEPresence = %q", IDEPresence)
	}
	if err := Touch(inbox, IDEPresence, Presence{PID: 1, Worktree: "/w"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := Live(inbox, IDEPresence); !ok {
		t.Fatal("ide.json not live right after Touch")
	}
	if _, ok := Live(inbox, TUIPresence); ok {
		t.Fatal("an ide.json made the TUI look live")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd /work/gigagit/.claude/worktrees/steer-ide && go test ./internal/steer -run TestDiscardLeavesTheIDESubInbox 2>&1 | tail -3`
Expected: compile FAIL — `IDEDir`, `IDEPresence` undefined.

- [ ] **Step 3: Implement**

In `internal/steer/steer.go`, in the `const (` block after `WebPresence`:

```go
	// IDEPresence names an IDE plugin's presence file (spec
	// 2026-10-15-gg-steer-ide-consumer §2.2). Its commands and replies do
	// NOT share the TUI's top-level files: the TUI drains and unlinks every
	// cmd-*.json it sees, so the IDE's live in the IDEInbox sub-directory,
	// which listPrefixed skips and Discard never enters.
	IDEPresence = "ide.json"
	IDEInbox    = "ide"
```

After `NewID`:

```go
// IDEDir is the IDE's own command/reply directory inside inbox: what a
// `gg session … --to ide` posts to and awaits in (spec §2.1).
func IDEDir(inbox string) string { return filepath.Join(inbox, IDEInbox) }
```

Update the package doc comment's file list to add `ide.json` and `ide/`.

- [ ] **Step 4: Run to verify it passes**

Run: `cd /work/gigagit/.claude/worktrees/steer-ide && go test ./internal/steer`
Expected: PASS (whole package).

- [ ] **Step 5: Commit**

```bash
cd /work/gigagit/.claude/worktrees/steer-ide
gg add internal/steer/steer.go internal/steer/ide_test.go
git commit -F <msgfile>   # "steer: IDEPresence + the ide/ sub-inbox (IDEDir)"
```

---

### Task 2: CLI routing — `--to ide` and `GG_SESSION_TO`

**Files:**
- Modify: `internal/cli/session.go` (`cutTo` ~112-150, `sessDir.target` 152-171, `sessionRoute`/`routeFor` 173-187, `sendSteerWaiting` 218-262, `steerLive` 303-345, `preferredInboxFor` 358-385, `sendBackground`, the usage line 65)
- Test: `internal/cli/session_ide_test.go`

**Interfaces:**
- Consumes: `steer.IDEPresence`, `steer.IDEDir` (Task 1); test helpers `livePresence`, `keepPresenceAlive`, `answer`, `runCLI`, `newRepoDir` (existing).
- Produces: `sessionRoute{ide steer.Presence; ideOK bool}`, `(sessionRoute).fileDir(inbox string) string` (the directory file delivery uses: `steer.IDEDir(inbox)` when `ideOK`, else `inbox`), `(sessionRoute).fileSide() string` (`"IDE"` / `"TUI"` for the queued message), `sessionGetenv("GG_SESSION_TO")` as the `--to` default; `cutTo` accepts `ide`.

- [ ] **Step 1: Write the failing tests**

`internal/cli/session_ide_test.go`. It reuses `session_test.go`'s helpers as they are: `livePresence(t, dir)`, `keepPresenceAlive(t, dir, name)`, `answer(t, dir, reply)` (drains the dir it is given — pass `steer.IDEDir(dir)` for the IDE), `runSession(dir, svc, args, &out, &errb)` and `newCLIRepo(t)`; `sessionGetenv` is a package variable tests swap (see `session_inbox_test.go:55`).

```go
package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

func runSessionAt(t *testing.T, dir string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	svc := domain.Open(newCLIRepo(t))
	code := runSession(dir, svc, args, &out, &errb)
	return code, out.String(), errb.String()
}

// withSessionEnv runs fn with sessionGetenv answering from env (and "" for
// every other key). Not parallel-safe: callers must not t.Parallel().
func withSessionEnv(t *testing.T, env map[string]string, fn func()) {
	t.Helper()
	old := sessionGetenv
	sessionGetenv = func(k string) string { return env[k] }
	defer func() { sessionGetenv = old }()
	fn()
}

func liveIDEPresence(t *testing.T, dir string) {
	t.Helper()
	if err := steer.Touch(dir, steer.IDEPresence, steer.Presence{PID: 99, Worktree: "/w", Started: time.Now().UTC().Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	keepPresenceAlive(t, dir, steer.IDEPresence)
}

func answerIDE(t *testing.T, dir string, reply func(steer.Command) steer.Reply) <-chan steer.Command {
	t.Helper()
	return answer(t, steer.IDEDir(dir), reply)
}

// --to ide posts to <inbox>/ide/ and awaits the reply there.
func TestSessionToIDEPostsToTheSubInbox(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	liveIDEPresence(t, dir)
	got := answerIDE(t, dir, func(c steer.Command) steer.Reply {
		return steer.Reply{ID: c.ID, OK: true, Detail: "opened a.go at line 3"}
	})
	code, out, errs := runSessionAt(t, dir, "navigate", "--file", "a.go", "--new-line", "3", "--to", "ide")
	if code != 0 || !strings.Contains(out, "opened a.go at line 3") {
		t.Fatalf("exit %d out %q err %q", code, out, errs)
	}
	c := <-got
	if c.Cmd != "navigate" || c.File != "a.go" || c.Line == nil || c.Line.No != 3 || !c.Wait {
		t.Fatalf("command = %+v", c)
	}
	if entries, _ := filepath.Glob(filepath.Join(dir, "cmd-*.json")); len(entries) != 0 {
		t.Fatalf("a --to ide command reached the TUI's inbox: %v", entries)
	}
}

// Review focus 1: --to ide with only a TUI live refuses and posts nothing.
func TestSessionToIDERefusesWhenOnlyTUIIsLive(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	livePresence(t, dir)
	code, _, errs := runSessionAt(t, dir, "navigate", "--file", "a.go", "--to", "ide")
	if code != 1 || !strings.Contains(errs, "no IDE for this worktree") {
		t.Fatalf("exit %d err %q", code, errs)
	}
	if entries, _ := filepath.Glob(filepath.Join(dir, "cmd-*.json")); len(entries) != 0 {
		t.Fatalf("refused command still reached the TUI: %v", entries)
	}
}

// Never implicit: an ide.json alone makes no verb go anywhere without --to.
func TestSessionIgnoresIDEWithoutTo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	liveIDEPresence(t, dir)
	code, _, errs := runSessionAt(t, dir, "navigate", "--file", "a.go")
	if code != 1 || !strings.Contains(errs, "no gg session for this worktree") {
		t.Fatalf("exit %d err %q", code, errs)
	}
}

// GG_SESSION_TO=ide is the default --to; an explicit --to wins (review focus 2).
func TestSessionEnvDefaultAndExplicitTo(t *testing.T) {
	dir := t.TempDir()
	liveIDEPresence(t, dir)
	livePresence(t, dir)
	tui := answer(t, dir, func(c steer.Command) steer.Reply { return steer.Reply{ID: c.ID, OK: true, Detail: "tui"} })
	ide := answerIDE(t, dir, func(c steer.Command) steer.Reply { return steer.Reply{ID: c.ID, OK: true, Detail: "ide"} })
	withSessionEnv(t, map[string]string{"GG_SESSION_TO": "ide"}, func() {
		if code, out, _ := runSessionAt(t, dir, "navigate", "--file", "a.go"); code != 0 || !strings.Contains(out, "ide") {
			t.Fatalf("env default: exit %d out %q", code, out)
		}
		<-ide
		if code, out, _ := runSessionAt(t, dir, "navigate", "--file", "a.go", "--to", "tui"); code != 0 || !strings.Contains(out, "tui") {
			t.Fatalf("explicit --to tui: exit %d out %q", code, out)
		}
		<-tui
	})
	// Review focus 2 twin: a bogus value is a usage error, not silently ignored.
	withSessionEnv(t, map[string]string{"GG_SESSION_TO": "vim"}, func() {
		if code, _, errs := runSessionAt(t, dir, "navigate", "--file", "a.go"); code != 2 || !strings.Contains(errs, "GG_SESSION_TO") {
			t.Fatalf("bogus env: exit %d err %q", code, errs)
		}
	})
}

// --to takes ide now; the usage names it.
func TestCutToAcceptsIDE(t *testing.T) {
	t.Parallel()
	rest, to, err := cutTo([]string{"navigate", "--to", "ide", "--file", "a.go"})
	if err != nil || to != "ide" || len(rest) != 3 {
		t.Fatalf("cutTo = %v %q %v", rest, to, err)
	}
	if _, _, err := cutTo([]string{"navigate", "--to", "vim"}); err == nil || !strings.Contains(err.Error(), "ide") {
		t.Fatalf("bogus --to: %v", err)
	}
}
```

`TestSessionEnvDefaultAndExplicitTo` swaps the package variable, so it must NOT call `t.Parallel()` (the other tests in the file do).

- [ ] **Step 2: Run to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/steer-ide && go test ./internal/cli -run 'TestSessionToIDE|TestSessionIgnoresIDE|TestSessionEnvDefault|TestCutToAcceptsIDE' 2>&1 | tail -8`
Expected: FAIL — `cutTo` rejects `ide` ("--to needs tui or web"), the others see "no gg session for this worktree" or usage exits.

- [ ] **Step 3: Implement**

In `internal/cli/session.go`:

1. `cutTo`: accept `ide` wherever `tui`/`web` are accepted; messages become `--to needs tui, web or ide` and `--to goes after the verb: gg session <verb> --to tui|web|ide …`; the usage line (65) says `[--to tui|web|ide]`.
2. After `cutTo` in `cmdSession`, before `sessDir{}`:
   ```go
   if to == "" {
       if env := sessionGetenv("GG_SESSION_TO"); env != "" {
           switch env {
           case "tui", "web", "ide":
               to = env
           default:
               fmt.Fprintf(stderr, "GG_SESSION_TO=%q: use tui, web or ide\n", env)
               return 2
           }
       }
   }
   ```
   (`status` keeps refusing an explicit `--to`; an env default is ignored for `status` — it reports every side.)
3. `sessionRoute`: add `ide steer.Presence; ideOK bool`; `routeFor` reads `steer.Live(dir, steer.IDEPresence)`.
4. `target()`:
   ```go
   case d.to == "ide" && !r.ideOK:
       why = "no IDE for this worktree"
   ```
   then in the `switch d.to`: `case "ide": r.tuiOK, r.webOK = false, false`; `default` (no `--to`): `r.ideOK = false` — the IDE is never implicit. The final "no gg session" check stays `!r.tuiOK && !r.webOK` (ide already handled).
5. Two helpers on `sessionRoute`:
   ```go
   // fileDir is where file delivery goes: the IDE's sub-inbox when --to ide
   // selected it, else the TUI's top-level inbox.
   func (r sessionRoute) fileDir(inbox string) string {
       if r.ideOK { return steer.IDEDir(inbox) }
       return inbox
   }
   func (r sessionRoute) fileOK() bool   { return r.tuiOK || r.ideOK }
   func (r sessionRoute) fileSide() string { if r.ideOK { return "IDE" }; return "TUI" }
   ```
6. `sendSteerWaiting`, `steerLive`, `sendBackground` (and any other place that tests `r.tuiOK` to decide file delivery and calls `steer.Post(dir, …)` / `steer.AwaitReply(dir, …)`): replace `r.tuiOK` with `r.fileOK()` and `dir` with `r.fileDir(dir)` for Post/AwaitReply; the queued message `"queued: no answer from the TUI within 2s"` becomes `fmt.Sprintf("queued: no answer from the %s within 2s", r.fileSide())` (keep the TUI text byte-identical when the side is TUI). `grep -n 'r.tuiOK\|steer.Post(\|steer.AwaitReply(' internal/cli/session*.go` lists every site.
7. `preferredInboxFor(dir, to)`: for `to == "ide"`, `$GG_INBOX` wins while `ide.json` is live there; skip the `TUIViewing` fallback.
8. `(sessionRoute).worktree()`: fall through to `r.ide.Worktree` when neither TUI nor web.

- [ ] **Step 4: Run to verify they pass — and that nothing else moved**

Run: `cd /work/gigagit/.claude/worktrees/steer-ide && go test ./internal/cli -run 'TestSession|TestCutTo' 2>&1 | tail -3 && go test ./internal/cli 2>&1 | tail -1`
Expected: PASS; the whole `cli` package green (every pre-existing session test unchanged).

- [ ] **Step 5: Commit**

```bash
cd /work/gigagit/.claude/worktrees/steer-ide
gg add internal/cli/session.go internal/cli/session_ide_test.go internal/cli/session_test.go
git commit -F <msgfile>   # "cli: gg session --to ide (and GG_SESSION_TO) — the IDE's sub-inbox, never implicit"
```

---

### Task 3: `gg session status` reports the IDE; skills and docs

**Files:**
- Modify: `internal/cli/session.go` (`sessionStatusAt` 434-498)
- Modify: `internal/agentskill/using-gg.md` (the `gg session` paragraph ~259-281), `internal/agentskill/gg-overview.md` (where it chooses TUI vs web), `internal/agentskill/agentskill.go:38`
- Modify: `.claude/skills/using-gg/SKILL.md` (via `./bin/gg init --update`), `CHANGELOG.md`, `docs/CLAUDE-details.md`
- Test: `internal/cli/session_ide_test.go` (append)

- [ ] **Step 1: Write the failing test**

```go
// Review focus 4: status with only the IDE live.
func TestSessionStatusPrintsTheLiveIDE(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := steer.Touch(dir, steer.IDEPresence, steer.Presence{PID: 99, Worktree: "/w", Started: "2026-10-15T12:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	code, out, _ := runSessionAt(t, dir, "status")
	if code != 0 || !strings.Contains(out, "worktree: /w") || !strings.Contains(out, "ide: pid 99 (since 2026-10-15T12:00:00Z)") {
		t.Fatalf("exit %d out %q", code, out)
	}
	code, out, _ = runSessionAt(t, dir, "status", "--json")
	if code != 0 || !strings.Contains(out, `"ide": {`) || !strings.Contains(out, `"tui": null`) || !strings.Contains(out, `"web": null`) {
		t.Fatalf("json: exit %d out %s", code, out)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd /work/gigagit/.claude/worktrees/steer-ide && go test ./internal/cli -run TestSessionStatusPrintsTheLiveIDE 2>&1 | tail -3`
Expected: FAIL — exit 1 "no gg session for this worktree".

- [ ] **Step 3: Implement**

In `sessionStatusAt`: the two "nothing live" checks become `!r.tuiOK && !r.webOK && !r.ideOK` (status is the one place the IDE counts without `--to`); JSON adds `out["ide"] = map[string]any{"pid": r.ide.PID, "started": r.ide.Started}` or `nil`; text adds, after the web line, `fmt.Fprintf(stdout, "ide: pid %d (since %s)\n", r.ide.PID, r.ide.Started)`. `r.worktree()` already falls through to the IDE (Task 2).

- [ ] **Step 4: Run to verify it passes**

Run: `cd /work/gigagit/.claude/worktrees/steer-ide && go test ./internal/cli -run 'TestSessionStatus' 2>&1 | tail -2`
Expected: PASS (new + the three existing status tests).

- [ ] **Step 5: Skills, version, dogfood copy, docs**

- `using-gg.md`: every `[--to tui|web]` → `[--to tui|web|ide]`; in the paragraph that explains `--to` (~lines 262–270) add: "`--to ide` reaches an IDE plugin that publishes itself for this worktree (`gg session status` lists it as `ide:`). The IDE is a target only when asked: pass `--to ide` when the user works in the IDE, or rely on `GG_SESSION_TO=ide` set by a terminal started from it — an explicit `--to` always wins. Verbs the IDE does not answer (`focus`, `reload`, `serve`; `preview`/`ref`/`pair` targets; old-side lines without a diff) come back as `error: …`."
- `gg-overview.md`: where it says "check gg runs — start `gg web --open` if not", add "(or, when the user works in the IDE, `--to ide` on every `gg session` verb — `gg session status` shows `ide:`)".
- `agentskill.go`: `Version = 174`.
- Build and refresh: `go build -o bin/gg ./cmd/gg && ./bin/gg init --update` (refreshes the dogfood copy and the global agent copies — the documented post-CLI-change step).
- `CHANGELOG.md`, new section at the top of Unreleased:
  ```markdown
  ## gg session --to ide: an IDE as a third steering target

  ### Added
  - `gg session <verb> --to ide` (or `GG_SESSION_TO=ide` as the default `--to`)
    reaches an IDE plugin that publishes `ide.json` in the worktree's steering
    inbox and drains its own `ide/` sub-inbox — the TUI drains and unlinks every
    top-level command, so the IDE's files live beside, not among, them. Never
    implicit: without `--to`/the env var, routing is unchanged. `gg session
    status` lists the IDE. Spec:
    `docs/superpowers/specs/2026-10-15-gg-steer-ide-consumer-design.md`;
    conformance material (command/reply/presence schemas, a replay corpus,
    reanchor cases, EncodeRepoKey vectors) under `docs/specs/gg-steer/`.
  ```
- `docs/CLAUDE-details.md`: in the live-steering section (grep `Live steering` / `steer`), one sentence: "A third consumer, the IDE, publishes `ide.json` and drains `<inbox>/ide/` (`steer.IDEDir`); selected only by `--to ide` / `GG_SESSION_TO` (spec 2026-10-15-gg-steer-ide-consumer)."

Run: `go test ./internal/agentskill ./internal/cli 2>&1 | tail -2` → PASS.

- [ ] **Step 6: Commit**

```bash
cd /work/gigagit/.claude/worktrees/steer-ide
gg add internal/cli/session.go internal/cli/session_ide_test.go internal/agentskill/using-gg.md internal/agentskill/gg-overview.md internal/agentskill/agentskill.go .claude/skills/using-gg/SKILL.md CHANGELOG.md docs/CLAUDE-details.md
git commit -F <msgfile>   # "cli: gg session status lists the IDE; skills v174 say when to --to ide"
```

---

### Task 4: Conformance material under `docs/specs/gg-steer/`

**Files:**
- Create: `docs/specs/gg-steer/README.md`, `command.schema.json`, `reply.schema.json`, `presence.schema.json`, `steer-cases.json`, `reanchor-cases.json`, `repokey-vectors.json`
- Test: `internal/steer/schema_test.go`, `internal/agentdocs/reanchor_cases_test.go`, `internal/config/repokey_vectors_test.go`, `internal/tui/steer_cases_test.go`

**Interfaces:**
- Consumes: `steer.Command/Reply/Presence` (JSON tags), `agentdocs.reanchor(notes []*note, old, cur []string)` + the unexported `note` type (test in package `agentdocs`), `config.EncodeRepoKey`, `tui` test helpers used by `TestSteerRefusalRules` (`steer_test.go:146-250`: a base `Model` + `applySteer`).

- [ ] **Step 1: Schemas + steer schema test (RED first)**

`internal/steer/schema_test.go`:

```go
package steer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

func loadSchema(t *testing.T, name string) *jsonschema.Resolved {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "specs", "gg-steer", name))
	if err != nil {
		t.Fatal(err)
	}
	var s jsonschema.Schema
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	rs, err := s.Resolve(nil)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return rs
}

func validate(t *testing.T, rs *jsonschema.Resolved, v any) error {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var inst any
	if err := json.Unmarshal(raw, &inst); err != nil {
		t.Fatal(err)
	}
	return rs.Validate(inst)
}

// Every wire shape gg writes validates against the published schemas
// (spec 2026-10-15-gg-steer-ide-consumer §7).
func TestWireShapesMatchSchemas(t *testing.T) {
	t.Parallel()
	cmd, rep, pres := loadSchema(t, "command.schema.json"), loadSchema(t, "reply.schema.json"), loadSchema(t, "presence.schema.json")
	cmds := []Command{
		{ID: "1-1", Cmd: "navigate", File: "a.go", Line: &Line{Side: "new", No: 40, End: 60}, Worktree: "/w", Wait: true},
		{ID: "1-2", Cmd: "navigate", Commit: "d5efa035df1f3c4c949040bebb924a35d0ea61a0"},
		{ID: "1-3", Cmd: "navigate", File: "a.go", Target: &Target{State: "commit", Commit: "d5efa035df1f3c4c949040bebb924a35d0ea61a0"}, Background: true},
		{ID: "1-4", Cmd: "highlight", File: "a.go", Side: "new", Start: 3, End: 4, Tone: "warn"},
		{ID: "1-5", Cmd: "file_focus", FileID: "f3", Line: &Line{No: 7}},
		{ID: "1-6", Cmd: "note_add", File: "a.go", Start: 3, End: 4, Summary: "s", Rationale: "r", Author: "ada", Meta: map[string]string{"kind": "question"}, Display: json.RawMessage(`{"kind":{"show":["badge"]}}`)},
		{ID: "1-7", Cmd: "overview_add", Title: "Tour", Text: "# t\n[a](a.go:3)", Background: true},
		{ID: "1-8", Cmd: "overview_show", FileID: "f9"},
	}
	for _, c := range cmds {
		if err := validate(t, cmd, c); err != nil {
			t.Errorf("command %s: %v", c.ID, err)
		}
	}
	reps := []Reply{
		{ID: "1-1", OK: true, Detail: "opened a.go:40-60 at line 40"},
		{ID: "1-2", OK: false, Error: "no IDE for this worktree"},
		{ID: "1-5", OK: true, Files: []OpenFile{{ID: "f3", Path: "a.go", Source: "worktree", Line: 7, State: "shown", Notes: 1}}},
		{ID: "1-6", OK: true, Notes: []FileNote{{ID: "t1", FileID: "f3", Path: "a.go", Start: 3, End: 4, Summary: "s"}}},
		{ID: "1-7", OK: true, Overviews: []Overview{{ID: "f4", Title: "Tour", State: "background", Anchors: 1}}},
	}
	for _, r := range reps {
		if err := validate(t, rep, r); err != nil {
			t.Errorf("reply %s: %v", r.ID, err)
		}
	}
	for _, p := range []Presence{{PID: 1, Worktree: "/w"}, {PID: 2, Worktree: "/w", Started: "2026-10-15T12:00:00Z", URL: "http://127.0.0.1:1"}} {
		if err := validate(t, pres, p); err != nil {
			t.Errorf("presence %+v: %v", p, err)
		}
	}
	// Unknown keys are tolerated (the IDE adds "ide"/"plugin" to its presence).
	var inst any
	_ = json.Unmarshal([]byte(`{"pid":3,"worktree":"/w","ide":"IntelliJ IDEA 2026.2","plugin":"0.2.0"}`), &inst)
	if err := pres.Validate(inst); err != nil {
		t.Errorf("presence with IDE keys: %v", err)
	}
	// What a reader must refuse: no id / no cmd / a line without a side.
	for _, bad := range []string{`{"cmd":"navigate"}`, `{"id":"1-9"}`, `{"id":"1-9","cmd":"navigate","line":{"no":3}}`} {
		var inst any
		_ = json.Unmarshal([]byte(bad), &inst)
		if err := cmd.Validate(inst); err == nil {
			t.Errorf("command schema accepted %s", bad)
		}
	}
}
```

Write the three schemas (JSON Schema 2020-12, `additionalProperties: true` everywhere — these are reader schemas; `required`: command `id`, `cmd`; line `side`, `no`; reply `id`, `ok`; presence `pid`, `worktree`; enums: `cmd` (the full verb list from §3.1), `target.state`, `line.side`, `side`, `tone`, `step`, `state` of OpenFile/Overview, `source` of OpenFile). Run the test: FAIL (files missing) → write → PASS.

- [ ] **Step 2: `reanchor-cases.json` + `agentdocs` test**

Cases (`old` = the lines the note was aligned to, `cur` = the current text, `note` = `{start, end, outdated}` before, `expect` = after):

```json
[
  {"name":"unchanged","old":["a","b","c"],"cur":["a","b","c"],"note":{"start":2,"end":2},"expect":{"start":2,"end":2,"outdated":false}},
  {"name":"line inserted above: follows","old":["a","b","c"],"cur":["x","a","b","c"],"note":{"start":2,"end":3},"expect":{"start":3,"end":4,"outdated":false}},
  {"name":"a line of the note edited: outdated, numbers kept","old":["a","b","c"],"cur":["a","B","c"],"note":{"start":2,"end":2},"expect":{"start":2,"end":2,"outdated":true}},
  {"name":"note's lines split apart: outdated","old":["a","b","c","d"],"cur":["a","b","x","c","d"],"note":{"start":2,"end":3},"expect":{"start":2,"end":3,"outdated":true}},
  {"name":"outdated, text back at its old place: returns","old":["a","B","c"],"cur":["a","b","c"],"note":{"start":2,"end":2,"outdated":true,"anchor":["b"]},"expect":{"start":2,"end":2,"outdated":false}},
  {"name":"outdated, text at exactly one other place: returns there","old":["a","B","c"],"cur":["b","a","B","c"],"note":{"start":2,"end":2,"outdated":true,"anchor":["b"]},"expect":{"start":1,"end":1,"outdated":false}},
  {"name":"outdated, text at two places: stays outdated (review focus 5)","old":["a","B","c"],"cur":["b","a","B","b"],"note":{"start":2,"end":2,"outdated":true,"anchor":["b"]},"expect":{"start":2,"end":2,"outdated":true}},
  {"name":"file shrank under an outdated note: clamped","old":["a","B","c","d"],"cur":["a"],"note":{"start":3,"end":4,"outdated":true,"anchor":["c","d"]},"expect":{"start":1,"end":1,"outdated":true}}
]
```

`internal/agentdocs/reanchor_cases_test.go` (package `agentdocs`): load the file; for each case build `n := &note{Start, End, Outdated, anchor: case.note.anchor}` (when `anchor` is absent, set it to `old[start-1:end]` — what `AddNote` records); call `reanchor([]*note{n}, old, cur)`; compare `Start/End/Outdated` with `expect`. Expect-values are the plan author's reading of `reanchor`; when a case fails, read the function and fix the CASE (and the spec §5 wording if it was wrong), never the code — ledger each.

- [ ] **Step 3: `repokey-vectors.json` + config test**

```json
[
  {"in":"/home/homeend/.claude/jobs/2620a6cb/tmp/cmprepo/.git","out":"-home-homeend-.claude-jobs-2620a6cb-tmp-cmprepo-.git","note":"real, confirmed on disk 2026-10-15"},
  {"in":"/work/gigagit/.git","out":"-work-gigagit-.git"},
  {"in":"/home/dev/src/gate","out":"-home-dev-src-gate"},
  {"in":"C:\\src\\x\\.git","out":"C--src-x-.git","os":"windows","note":"derived: Clean keeps backslashes on Windows, then \\ and : become -"},
  {"in":"C:/src/x/.git","out":"C--src-x-.git","os":"windows","note":"Clean turns / into \\ on Windows first"}
]
```

`internal/config/repokey_vectors_test.go`: run each case whose `os` is empty or equals `runtime.GOOS` through `EncodeRepoKey`. (On Linux the Windows rows are skipped; the plugin agent confirms them with `gg session status` on Windows.)

- [ ] **Step 4: `steer-cases.json` + TUI replay test**

The corpus has two kinds of entry: `"sync": true` refusals the TUI answers without touching git (run by gg's test), and landings whose expected `detail` documents the prose for the plugin (`"sync": false`, skipped by gg's test — the TUI lands asynchronously). Shape:

```json
{"files": {"a.go": "package a\n\nfunc gate() {}\n"},
 "cases": [
  {"name":"unknown verb","sync":true,"command":{"id":"c-1","cmd":"dance","wait":true},"reply":{"ok":false,"error_prefix":"unknown command"}},
  {"name":"target state nobody knows","sync":true,"command":{"id":"c-2","cmd":"navigate","file":"a.go","target":{"state":"shelf"},"wait":true},"reply":{"ok":false}},
  {"name":"line side nobody knows","sync":true,"command":{"id":"c-3","cmd":"navigate","file":"a.go","line":{"side":"left","no":1},"wait":true},"reply":{"ok":false}},
  {"name":"another worktree","sync":true,"command":{"id":"c-4","cmd":"navigate","file":"a.go","worktree":"/elsewhere","wait":true},"reply":{"ok":false,"error_prefix":"gg is showing worktree"}},
  {"name":"open a line","sync":false,"command":{"id":"c-5","cmd":"navigate","file":"a.go","line":{"side":"new","no":3},"wait":true},"reply":{"ok":true,"detail":"opened a.go:3 at line 3"}},
  {"name":"past the end lands on the last line","sync":false,"command":{"id":"c-6","cmd":"navigate","file":"a.go","line":{"side":"new","no":9},"wait":true},"reply":{"ok":true,"detail":"opened a.go:9 at line 3 (line 9 is past the end, 3 lines)"}},
  {"name":"file_focus of nothing","sync":true,"command":{"id":"c-7","cmd":"file_focus","file_id":"f9","wait":true},"reply":{"ok":false,"error":"no open file f9"}},
  {"name":"note on nothing","sync":true,"command":{"id":"c-8","cmd":"note_show","note_id":"t9","wait":true},"reply":{"ok":false,"error":"no note t9"}},
  {"name":"TUI-only verbs the IDE refuses","sync":false,"ide_only":true,"command":{"id":"c-9","cmd":"reload","wait":true},"reply":{"ok":false,"error":"reload is a gg TUI command"}}
 ]}
```

`internal/tui/steer_cases_test.go`: build the base `Model` the way `TestSteerRefusalRules` does (read `steer_test.go:146-250` and reuse its helper verbatim — its name is the one to use; the repo gets the corpus's `files`), then for each `sync: true` case without `ide_only`, run `applySteer` and compare `ok`, and `error`/`error_prefix`/`detail` when given. If a sync case turns out asynchronous in the TUI (the reply comes from a `tea.Cmd`), flip it to `"sync": false` and ledger it — the corpus must say what gg does, not what the plan guessed.

- [ ] **Step 5: README**

`docs/specs/gg-steer/README.md`: one paragraph per file, the spec path, and "the plugin runs `steer-cases.json` (every case, `ide_only` included) and `reanchor-cases.json` against its own handler; gg runs the `sync` cases against its TUI".

- [ ] **Step 6: Run everything touched, commit**

Run: `cd /work/gigagit/.claude/worktrees/steer-ide && go test ./internal/steer ./internal/agentdocs ./internal/config ./internal/tui -run 'TestWireShapes|TestReanchorCases|TestRepoKeyVectors|TestSteerCases' 2>&1 | tail -4`
Expected: PASS ×4.

```bash
gg add docs/specs/gg-steer internal/steer/schema_test.go internal/agentdocs/reanchor_cases_test.go internal/config/repokey_vectors_test.go internal/tui/steer_cases_test.go
git commit -F <msgfile>   # "spec conformance: gg-steer schemas, replay corpus, reanchor cases, repo-key vectors"
```

---

### Task 5: Race gate, verify binary, the plugin handoff

- [ ] **Step 1: Race gate**

Run: `cd /work/gigagit/.claude/worktrees/steer-ide && ./test.sh race > <scratch>/race-steer.log 2>&1; grep -n 'all green\|^FAIL\|--- FAIL' <scratch>/race-steer.log | tail -4`
Expected: `all green`.

- [ ] **Step 2: Verify binary + a live smoke**

`go build -o bin/gg ./cmd/gg`, then in a temp repo: `./bin/gg session status` → exit 1 "no gg session"; create the inbox dir (`./bin/gg session status --json` shows nothing; compute it with `EncodeRepoKey`), write an `ide.json`, run `./bin/gg session navigate --file a.go --new-line 1 --to ide --no-wait` → prints an id and `ls <inbox>/ide/` shows `cmd-<id>.json`; without `--to` → "no gg session for this worktree". Record the exact inbox path in the handoff.

- [ ] **Step 3: Hand the plugin repo its copy (no commit there)**

```bash
W=/work/gigagit/.claude/worktrees/steer-ide; G=/work/gg-notes/docs/gg-steer; mkdir -p $G
cp $W/docs/superpowers/specs/2026-10-15-gg-steer-ide-consumer-design.md $G/SPEC.md   # + the provenance header
cp $W/docs/specs/gg-steer/*.json $W/docs/specs/gg-steer/README.md $G/
```

Append to `/work/gg-notes/AGENTS.md` a section "## 7. Live steering — be a `gg session` target" with: the inbox path rule and the real example, the presence loop (1 s touch, 5 s window, `ide.json` fields), the `ide/` sub-inbox drain rules (size cap, id check, unlink-then-apply, reply only when `wait`, sweep at open), the verb table reference (SPEC §3.3), the in-memory model (SPEC §5), the three conformance files, the acceptance of SPEC §8, and the binary that has `--to ide` (`$W/bin/gg` until merged). Report the paths.

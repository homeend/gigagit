# Interactive Rebase — Slice 2: gg-as-editor protocol — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A pure `internal/rebaseplan` package that turns an interactive-rebase plan into a git todo, plus two hidden `gg` subcommands git invokes as its sequence editor / reword step — so a later slice can drive `git rebase -i` non-interactively and cross-platform (no `sed`, no `GIT_EDITOR`).

**Architecture:** `internal/rebaseplan` (pure: types + JSON + grouping + message-compose + todo generation, no git/os-exec). `cmd/gg` gains hidden routes `__rebase-seq <plan> <todofile>` (rewrite git's todo from the plan) and `__rebase-message <plan> <index>` (amend HEAD with the plan's message via `git commit --amend -F -`). Reword and Squash share the one `__rebase-message` mechanism; Squash composes a combined message (target subject as title, each squashed commit's message as a blank-line-separated body block).

**Tech Stack:** Go 1.26, `encoding/json`, `os/exec` (cmd/gg only).

**Spec:** `docs/superpowers/specs/2026-06-15-interactive-rebase-design.md` (Slice 2). Builds on Slice 1 (`gitexec.RunEnv`, merged).

**Conventions:** TDD; `internal/rebaseplan` stays pure (the git amend lives in `cmd/gg`); tests use a real `git` for the amend test; gate `./test.sh race`; commits end `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.

**Plan file format (JSON):** `{"entries":[{"sha","action","orig","new_msg"}]}`, entries in **git todo order (oldest-first)**. `action` ∈ `pick|reword|squash|drop`. `orig` = the commit's original full message (always set by the builder). `new_msg` = the user's new message (reword only).

**Grouping rule:** drops are omitted; each `squash` melds into the nearest preceding non-squash target. The editor forbids squash on the oldest row, so the first non-drop entry is never a squash (`Groups` errors if it is). `__rebase-message <index>` is always invoked with a **target's** original index.

---

### Task 1: `rebaseplan` plan types + JSON

**Files:**
- Create: `internal/rebaseplan/plan.go`
- Test: `internal/rebaseplan/plan_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/rebaseplan/plan_test.go`:

```go
package rebaseplan

import (
	"reflect"
	"testing"
)

func TestMarshalUnmarshalRoundTrip(t *testing.T) {
	p := Plan{Entries: []Entry{
		{Sha: "aaa", Action: Pick, Orig: "first\n"},
		{Sha: "bbb", Action: Reword, Orig: "second\n", NewMsg: "second reworded"},
		{Sha: "ccc", Action: Squash, Orig: "third\n"},
		{Sha: "ddd", Action: Drop, Orig: "fourth\n"},
	}}
	b, err := Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := Unmarshal(b)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got, p) {
		t.Fatalf("round trip mismatch:\n got %+v\nwant %+v", got, p)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/rebaseplan/ -run TestMarshal -v`
Expected: FAIL — package/`Plan`/`Marshal` undefined.

- [ ] **Step 3: Create the types**

Create `internal/rebaseplan/plan.go`:

```go
// Package rebaseplan models an interactive-rebase plan and turns it into the
// git rebase todo + per-commit messages. It is pure: no git, no os/exec — the
// gg-as-editor subcommands in cmd/gg do the I/O and run git.
package rebaseplan

import "encoding/json"

// Action is one interactive-rebase row action.
type Action string

const (
	Pick   Action = "pick"
	Reword Action = "reword"
	Squash Action = "squash"
	Drop   Action = "drop"
)

// Entry is one commit in the plan. Orig is the commit's original full message
// (always populated by the builder); NewMsg holds the user's new message for a
// Reword. Entries are stored in git todo order (oldest-first).
type Entry struct {
	Sha    string `json:"sha"`
	Action Action `json:"action"`
	Orig   string `json:"orig"`
	NewMsg string `json:"new_msg,omitempty"`
}

// Plan is the ordered set of rebase entries.
type Plan struct {
	Entries []Entry `json:"entries"`
}

// Marshal serializes the plan to JSON for the plan file.
func Marshal(p Plan) ([]byte, error) { return json.Marshal(p) }

// Unmarshal parses a plan file's JSON.
func Unmarshal(b []byte) (Plan, error) {
	var p Plan
	err := json.Unmarshal(b, &p)
	return p, err
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/rebaseplan/ -run TestMarshal -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/rebaseplan/plan.go internal/rebaseplan/plan_test.go
git commit -m "feat(rebaseplan): plan types + JSON serialization

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: grouping + composed message

**Files:**
- Create: `internal/rebaseplan/group.go`
- Test: `internal/rebaseplan/group_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/rebaseplan/group_test.go`:

```go
package rebaseplan

import (
	"reflect"
	"testing"
)

func TestGroups(t *testing.T) {
	p := Plan{Entries: []Entry{
		{Sha: "a", Action: Pick, Orig: "A"},
		{Sha: "b", Action: Squash, Orig: "B"},
		{Sha: "c", Action: Drop, Orig: "C"},
		{Sha: "d", Action: Reword, Orig: "D", NewMsg: "D2"},
	}}
	groups, err := p.Groups()
	if err != nil {
		t.Fatalf("groups: %v", err)
	}
	want := []Group{
		{Target: 0, Squash: []int{1}}, // a with b squashed in (c dropped, skipped)
		{Target: 3, Squash: nil},      // d reworded
	}
	if !reflect.DeepEqual(groups, want) {
		t.Fatalf("groups = %+v, want %+v", groups, want)
	}
}

func TestGroupsSquashFirstIsError(t *testing.T) {
	p := Plan{Entries: []Entry{{Sha: "a", Action: Squash, Orig: "A"}}}
	if _, err := p.Groups(); err == nil {
		t.Fatal("a leading squash (nothing older to meld into) must error")
	}
}

func TestMessageComposesSquash(t *testing.T) {
	p := Plan{Entries: []Entry{
		{Sha: "a", Action: Pick, Orig: "title A\n\nbody A\n"},
		{Sha: "b", Action: Squash, Orig: "msg B\n"},
		{Sha: "c", Action: Squash, Orig: "msg C\n"},
	}}
	got := p.Message(0)
	want := "title A\n\nbody A\n\nmsg B\n\nmsg C"
	if got != want {
		t.Fatalf("Message(0) = %q, want %q", got, want)
	}
}

func TestMessageRewordWins(t *testing.T) {
	p := Plan{Entries: []Entry{
		{Sha: "a", Action: Reword, Orig: "old\n", NewMsg: "new title\n\nnew body"},
		{Sha: "b", Action: Squash, Orig: "squashed\n"},
	}}
	got := p.Message(0)
	want := "new title\n\nnew body\n\nsquashed"
	if got != want {
		t.Fatalf("Message(0) = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/rebaseplan/ -run 'TestGroups|TestMessage' -v`
Expected: FAIL — `Group`/`Groups`/`Message` undefined.

- [ ] **Step 3: Implement grouping + message**

Create `internal/rebaseplan/group.go`:

```go
package rebaseplan

import (
	"fmt"
	"strings"
)

// Group is a target commit plus the squash entries melded into it, by index
// into Plan.Entries.
type Group struct {
	Target int
	Squash []int
}

// Groups returns the squash-groups in todo order, skipping Drop entries. Each
// Squash entry attaches to the nearest preceding non-squash target. It errors
// if a squash has no preceding target (the editor forbids squash on the oldest
// row, so this only happens on a malformed plan).
func (p Plan) Groups() ([]Group, error) {
	var groups []Group
	cur := -1 // index into groups of the current target's group
	for i, e := range p.Entries {
		switch e.Action {
		case Drop:
			continue
		case Squash:
			if cur < 0 {
				return nil, fmt.Errorf("rebaseplan: squash at index %d has no preceding commit", i)
			}
			groups[cur].Squash = append(groups[cur].Squash, i)
		default: // Pick, Reword
			groups = append(groups, Group{Target: i})
			cur = len(groups) - 1
		}
	}
	return groups, nil
}

// Message returns the commit message for the group whose target is at index ti:
// the target's new message (if reworded) else its original, followed by each
// squashed commit's original message as a blank-line-separated block.
func (p Plan) Message(ti int) string {
	t := p.Entries[ti]
	base := t.Orig
	if t.Action == Reword && t.NewMsg != "" {
		base = t.NewMsg
	}
	parts := []string{strings.TrimRight(base, "\n")}
	for i := ti + 1; i < len(p.Entries); i++ {
		switch p.Entries[i].Action {
		case Squash:
			parts = append(parts, strings.TrimRight(p.Entries[i].Orig, "\n"))
		case Drop:
			continue
		default:
			i = len(p.Entries) // next target ends the group
		}
	}
	return strings.Join(parts, "\n\n")
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/rebaseplan/ -run 'TestGroups|TestMessage' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/rebaseplan/group.go internal/rebaseplan/group_test.go
git commit -m "feat(rebaseplan): squash grouping + composed message

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: todo generation

**Files:**
- Create: `internal/rebaseplan/todo.go`
- Test: `internal/rebaseplan/todo_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/rebaseplan/todo_test.go`:

```go
package rebaseplan

import "testing"

func TestRewriteTodo(t *testing.T) {
	p := Plan{Entries: []Entry{
		{Sha: "aaaaaaa", Action: Pick, Orig: "A"},
		{Sha: "bbbbbbb", Action: Reword, Orig: "B", NewMsg: "B2"},
		{Sha: "ccccccc", Action: Squash, Orig: "C"}, // melds into B
		{Sha: "ddddddd", Action: Drop, Orig: "D"},   // omitted
		{Sha: "eeeeeee", Action: Pick, Orig: "E"},   // plain pick, no exec
	}}
	got, err := p.RewriteTodo("/usr/bin/gg", "/tmp/plan.json")
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	want := "pick aaaaaaa\n" +
		"pick bbbbbbb\n" +
		"fixup ccccccc\n" +
		`exec "/usr/bin/gg" __rebase-message "/tmp/plan.json" 1` + "\n" +
		"pick eeeeeee\n"
	if got != want {
		t.Fatalf("todo =\n%q\nwant\n%q", got, want)
	}
}

func TestRewriteTodoSquashFirstErrors(t *testing.T) {
	p := Plan{Entries: []Entry{{Sha: "a", Action: Squash, Orig: "A"}}}
	if _, err := p.RewriteTodo("gg", "/tmp/p"); err == nil {
		t.Fatal("leading squash must error")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/rebaseplan/ -run TestRewriteTodo -v`
Expected: FAIL — `RewriteTodo` undefined.

- [ ] **Step 3: Implement todo generation**

Create `internal/rebaseplan/todo.go`:

```go
package rebaseplan

import (
	"fmt"
	"strings"
)

// RewriteTodo produces the git interactive-rebase todo text for the plan.
// ggBin is the gg binary path and planPath the plan file path; together they
// form the `exec` lines that apply reword/squash messages. A reworded target or
// a target with squashed commits gets one exec line (referencing the target's
// original index) after its pick (+ fixups).
func (p Plan) RewriteTodo(ggBin, planPath string) (string, error) {
	groups, err := p.Groups()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, g := range groups {
		fmt.Fprintf(&b, "pick %s\n", p.Entries[g.Target].Sha)
		for _, si := range g.Squash {
			fmt.Fprintf(&b, "fixup %s\n", p.Entries[si].Sha)
		}
		if p.Entries[g.Target].Action == Reword || len(g.Squash) > 0 {
			fmt.Fprintf(&b, "exec %q __rebase-message %q %d\n", ggBin, planPath, g.Target)
		}
	}
	return b.String(), nil
}
```

> `%q` quotes the binary and plan paths so spaces survive git's `sh -c` exec.
> Paths with embedded double-quotes are not supported (temp paths never have
> them) — a deliberate v1 limitation.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/rebaseplan/ -run TestRewriteTodo -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/rebaseplan/todo.go internal/rebaseplan/todo_test.go
git commit -m "feat(rebaseplan): generate the git rebase todo from a plan

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 4: `gg __rebase-seq` hidden subcommand

**Files:**
- Create: `cmd/gg/rebase_editor.go`
- Modify: `cmd/gg/main.go` (route `__rebase-seq` before the `cli.IsCommand` check)
- Test: `cmd/gg/rebase_editor_test.go`

- [ ] **Step 1: Write the failing test**

Create `cmd/gg/rebase_editor_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gigagit/gg/internal/rebaseplan"
)

func TestRunRebaseSeqRewritesTodo(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.json")
	todoPath := filepath.Join(dir, "git-rebase-todo")

	p := rebaseplan.Plan{Entries: []rebaseplan.Entry{
		{Sha: "aaaaaaa", Action: rebaseplan.Pick, Orig: "A"},
		{Sha: "bbbbbbb", Action: rebaseplan.Drop, Orig: "B"},
	}}
	b, _ := rebaseplan.Marshal(p)
	if err := os.WriteFile(planPath, b, 0o644); err != nil {
		t.Fatal(err)
	}
	// git would write the original todo here; content is irrelevant — we overwrite.
	if err := os.WriteFile(todoPath, []byte("pick aaaaaaa A\npick bbbbbbb B\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runRebaseSeq([]string{planPath, todoPath}); err != nil {
		t.Fatalf("runRebaseSeq: %v", err)
	}
	got, _ := os.ReadFile(todoPath)
	if string(got) != "pick aaaaaaa\n" {
		t.Fatalf("todo = %q, want %q", string(got), "pick aaaaaaa\n")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./cmd/gg/ -run TestRunRebaseSeq -v`
Expected: FAIL — `runRebaseSeq` undefined.

- [ ] **Step 3: Implement the subcommand body**

Create `cmd/gg/rebase_editor.go`:

```go
package main

import (
	"fmt"
	"os"

	"github.com/gigagit/gg/internal/rebaseplan"
)

// runRebaseSeq is the GIT_SEQUENCE_EDITOR hook: it reads the gg plan and
// overwrites git's rebase todo file to match. args = [planPath, todoPath].
func runRebaseSeq(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("__rebase-seq: want <plan> <todofile>, got %v", args)
	}
	planPath, todoPath := args[0], args[1]
	raw, err := os.ReadFile(planPath)
	if err != nil {
		return err
	}
	p, err := rebaseplan.Unmarshal(raw)
	if err != nil {
		return err
	}
	ggBin, err := os.Executable()
	if err != nil {
		return err
	}
	todo, err := p.RewriteTodo(ggBin, planPath)
	if err != nil {
		return err
	}
	return os.WriteFile(todoPath, []byte(todo), 0o644)
}
```

- [ ] **Step 4: Route it in `main.go`**

In `cmd/gg/main.go`, add this **before** the `cli.IsCommand` check (after the
`inspect` route, around line 44), so the hidden command isn't caught by the
unknown-command guard:

```go
	if len(args) > 0 && args[0] == "__rebase-seq" {
		if err := runRebaseSeq(args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "gg __rebase-seq:", err)
			os.Exit(1)
		}
		return
	}
```

- [ ] **Step 5: Run to verify it passes**

Run: `go test ./cmd/gg/ -run TestRunRebaseSeq -v && go build ./cmd/gg`
Expected: PASS; build clean.

- [ ] **Step 6: Commit**

```bash
git add cmd/gg/rebase_editor.go cmd/gg/rebase_editor_test.go cmd/gg/main.go
git commit -m "feat(gg): __rebase-seq writes the git todo from a plan

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 5: `gg __rebase-message` hidden subcommand

**Files:**
- Modify: `cmd/gg/rebase_editor.go` (add `runRebaseMessage`)
- Modify: `cmd/gg/main.go` (route `__rebase-message`)
- Test: `cmd/gg/rebase_editor_test.go` (real-repo amend)

- [ ] **Step 1: Write the failing test**

Append to `cmd/gg/rebase_editor_test.go` (add imports `os/exec`, `strings`):

```go
func TestRunRebaseMessageAmendsHead(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-b", "main")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\n"), 0o644)
	git("add", ".")
	git("commit", "-m", "original")

	planPath := filepath.Join(dir, "plan.json")
	p := rebaseplan.Plan{Entries: []rebaseplan.Entry{
		{Sha: "head", Action: rebaseplan.Reword, Orig: "original", NewMsg: "reworded title\n\nbody"},
	}}
	b, _ := rebaseplan.Marshal(p)
	os.WriteFile(planPath, b, 0o644)

	// runRebaseMessage runs `git commit --amend` in the current directory, so
	// run it from dir.
	cwd, _ := os.Getwd()
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := runRebaseMessage([]string{planPath, "0"}); err != nil {
		t.Fatalf("runRebaseMessage: %v", err)
	}
	out, _ := exec.Command("git", "-C", dir, "log", "-1", "--pretty=%B").Output()
	if got := strings.TrimSpace(string(out)); got != "reworded title\n\nbody" {
		t.Fatalf("HEAD message = %q, want %q", got, "reworded title\n\nbody")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./cmd/gg/ -run TestRunRebaseMessage -v`
Expected: FAIL — `runRebaseMessage` undefined.

- [ ] **Step 3: Implement the subcommand body**

Add to `cmd/gg/rebase_editor.go` (add imports `os/exec`, `strconv`, `strings`):

```go
// runRebaseMessage is the rebase `exec` hook for reword/squash: it amends HEAD's
// message to the plan's composed message for the target at the given index.
// args = [planPath, index]. It runs in the rebase working directory (git's cwd
// for exec steps), so the bare `git commit` targets the right repo.
func runRebaseMessage(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("__rebase-message: want <plan> <index>, got %v", args)
	}
	raw, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	p, err := rebaseplan.Unmarshal(raw)
	if err != nil {
		return err
	}
	idx, err := strconv.Atoi(args[1])
	if err != nil {
		return fmt.Errorf("__rebase-message: bad index %q: %w", args[1], err)
	}
	if idx < 0 || idx >= len(p.Entries) {
		return fmt.Errorf("__rebase-message: index %d out of range", idx)
	}
	cmd := exec.Command("git", "commit", "--amend", "-F", "-")
	cmd.Stdin = strings.NewReader(p.Message(idx))
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}
```

(Merge the import additions into the file's existing import block: it then
imports `fmt`, `os`, `os/exec`, `strconv`, `strings`, and the rebaseplan
package.)

- [ ] **Step 4: Route it in `main.go`**

In `cmd/gg/main.go`, next to the `__rebase-seq` route:

```go
	if len(args) > 0 && args[0] == "__rebase-message" {
		if err := runRebaseMessage(args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "gg __rebase-message:", err)
			os.Exit(1)
		}
		return
	}
```

- [ ] **Step 5: Run to verify it passes**

Run: `go test ./cmd/gg/ -run TestRunRebaseMessage -v && go build ./cmd/gg`
Expected: PASS; build clean.

- [ ] **Step 6: Commit**

```bash
git add cmd/gg/rebase_editor.go cmd/gg/rebase_editor_test.go cmd/gg/main.go
git commit -m "feat(gg): __rebase-message amends HEAD from the plan

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Final verification (after all tasks)

- [ ] `./test.sh race` — vet+gofmt clean, all unit + e2e green.
- [ ] `superpowers:finishing-a-development-branch`.
- [ ] **After merge, RE-RUN `./test.sh race` on merged `main`** — drift discipline (main is moving fast; expect to rebase).

---

## Self-Review

**1. Spec coverage (Slice 2):**
- Plan file JSON `{Sha,Action,Message}` → Task 1 (`Entry` with `Orig`+`NewMsg`; richer than the spec's single `Message`, needed to compose squash bodies). ✓
- `__rebase-seq` rewrites todo: pick / reword→pick+exec / squash→fixup / drop→omit / reorder→line order → Tasks 3–4. ✓
- `__rebase-message` amends HEAD via `git commit --amend -F` → Task 5. ✓
- `GIT_SEQUENCE_EDITOR` only, no `GIT_EDITOR` → reword/squash both done with `exec` lines (Task 3). ✓
- Squash composes a combined message (target subject title + squashed messages line-by-line) → Task 2 (`Message`). ✓
- Hidden routes in `cmd/gg/main.go`, not user-facing → Tasks 4–5 (before the `cli.IsCommand`/unknown-command guard). ✓
- `internal/rebaseplan` pure (no git/os-exec) → Tasks 1–3; git lives only in `cmd/gg` (Task 5). ✓

**2. Placeholder scan:** every code step shows complete code; the one limitation note (paths with embedded double-quotes unsupported) is an explicit v1 boundary, not a vague gap. ✓

**3. Type consistency:** `Action`/`Pick`/`Reword`/`Squash`/`Drop`, `Entry{Sha,Action,Orig,NewMsg}`, `Plan{Entries}`, `Marshal`/`Unmarshal`, `Group{Target,Squash}`, `Plan.Groups()`, `Plan.Message(int)`, `Plan.RewriteTodo(ggBin, planPath)` are identical across Tasks 1–3 and used consistently by `runRebaseSeq`/`runRebaseMessage` in Tasks 4–5. The `exec` line index (`g.Target`, original plan index) matches `Message(ti)`'s parameter. ✓

**Deferred to Slice 3:** setting `GIT_SEQUENCE_EDITOR` and running `git rebase -i` end-to-end (the engine op), which exercises both subcommands through a real driven rebase.

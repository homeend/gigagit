# Compare Trees — Stage 5 Implementation Plan (`gg compare` CLI)

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans. Steps use `- [ ]`.

**Goal:** `gg compare <left> [<right>]` prints the changed-file list between two endpoints (a commit-ish, `@staged`, or `@worktree`; `right` defaults to `@worktree`), mirroring `gg status`'s shape.

**Architecture:** Reuse `domain.CompareFiles` (Stage 1). Parse endpoint strings → `model.Endpoint` (commit-ish passes through as `Hash`; git resolves it). Register `compare` in the CLI `commands` map, the `Run` switch, and the `cmd/gg` help string. No engine/git/domain change.

**Tech Stack:** Go 1.26, scriptable CLI frontend.

## Global Constraints

- Reuse `svc.CompareFiles(ctx, left, right)` — DiffTreeFiles runs `git diff --name-status`; the `Hash` field carries a commit-ish (`HEAD`, `main`, `abc123`, `HEAD~2`) and git resolves it.
- DiffTreeFiles supports only the four forward forms (commit→commit/index/worktree, index→worktree); an unsupported pair errors — surface it with a clear message.
- **Register `compare` in BOTH** `internal/cli/cli.go` `commands` map (guard test `TestEverySwitchCaseIsRegistered` enforces switch⊆map) **AND** the `cmd/gg/main.go` help string (line ~65 — the guard test does NOT cover this; update by hand). This is the exact known-bugs #1 drift.
- CLI surface changed → update `internal/agentskill/using-gg.md`, bump `agentskill.Version`, run `gg init --update` (or the test `TestDogfoodSkillCopyInSync` fails).
- TDD, real git (`newCLIRepo`/`runCLI`). Verify test exit explicitly (no `| tail`). Branch `compare-cli`; human merges.

---

### Task 1: `cmdCompare` + endpoint parsing + registration

**Files:**
- Create: `internal/cli/compare.go`
- Modify: `internal/cli/cli.go` (`commands` map + `case "compare"` in `Run`)
- Modify: `cmd/gg/main.go` (help string line ~65: add `compare`)
- Test: `internal/cli/compare_test.go`

**Interfaces:**
- Produces: `func cmdCompare(svc *domain.Service, args []string, stdout, stderr io.Writer) int`; `func parseEndpoint(s string) model.Endpoint`.

- [ ] **Step 1: Write the failing test**

Create `internal/cli/compare_test.go`:

```go
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func TestParseEndpoint(t *testing.T) {
	cases := []struct {
		in   string
		want model.Endpoint
	}{
		{"@worktree", model.Endpoint{Kind: model.EndpointWorkTree}},
		{"@staged", model.Endpoint{Kind: model.EndpointIndex}},
		{"@index", model.Endpoint{Kind: model.EndpointIndex}},
		{"HEAD~2", model.Endpoint{Kind: model.EndpointCommit, Hash: "HEAD~2"}},
		{"abc123", model.Endpoint{Kind: model.EndpointCommit, Hash: "abc123"}},
	}
	for _, c := range cases {
		if got := parseEndpoint(c.in); got != c.want {
			t.Errorf("parseEndpoint(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestCompareCommitRange(t *testing.T) {
	dir := newCLIRepo(t) // one commit: README.md
	gitRun(t, dir, "checkout", "-q", "-b", "main") // ensure on main (no-op if already)
	// second commit adds b.txt
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "c2")

	code, out, errb := runCLI(t, dir, "compare", "HEAD~1", "HEAD")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, errb)
	}
	if !strings.Contains(out, "b.txt") {
		t.Fatalf("compare HEAD~1 HEAD must list b.txt:\n%s", out)
	}
}

func TestCompareDefaultsToWorktree(t *testing.T) {
	dir := newCLIRepo(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("dirtied\n"), 0o644) // unstaged change

	code, out, errb := runCLI(t, dir, "compare", "HEAD")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, errb)
	}
	if !strings.Contains(out, "README.md") {
		t.Fatalf("compare HEAD (vs working tree) must list README.md:\n%s", out)
	}
}

func TestCompareNoArgsUsage(t *testing.T) {
	dir := newCLIRepo(t)
	code, _, errb := runCLI(t, dir, "compare")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (usage)", code)
	}
	if !strings.Contains(errb, "usage") {
		t.Fatalf("missing usage on stderr:\n%s", errb)
	}
}
```

(`gitRun` already exists in `internal/cli/branch_test.go`. If the `checkout -b main` line fails because the repo is already on `main`, drop that line — `newCLIRepo` inits with `-b main`.)

- [ ] **Step 2: Run, verify it fails**

Run: `cd /mnt/t/others/gg-compare-cli && go test ./internal/cli/ -run TestCompare -v`
Expected: FAIL — `parseEndpoint`/`cmdCompare` undefined (and `compare` not routed).

- [ ] **Step 3a: Implement `cmdCompare`**

Create `internal/cli/compare.go`:

```go
package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/model"
)

// parseEndpoint maps a CLI token to a comparison endpoint: "@worktree" (the
// working tree), "@staged"/"@index" (the index), or any other token as a
// commit-ish (git resolves HEAD, branch names, abc123, HEAD~2, …).
func parseEndpoint(s string) model.Endpoint {
	switch s {
	case "@worktree":
		return model.Endpoint{Kind: model.EndpointWorkTree}
	case "@staged", "@index":
		return model.Endpoint{Kind: model.EndpointIndex}
	default:
		return model.Endpoint{Kind: model.EndpointCommit, Hash: s}
	}
}

// cmdCompare prints the changed-file list between two endpoints:
//
//	gg compare <left> [<right>]
//
// where each endpoint is a commit-ish, @staged, or @worktree. <right> defaults
// to @worktree. Output is one "<status>\t<path>" line per changed file.
func cmdCompare(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: gg compare <left> [<right>]   (endpoints: a commit, @staged, @worktree; right defaults to @worktree)")
		return 2
	}
	left := parseEndpoint(args[0])
	right := model.Endpoint{Kind: model.EndpointWorkTree}
	if len(args) > 1 {
		right = parseEndpoint(args[1])
	}
	files, err := svc.CompareFiles(context.Background(), left, right)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	for _, f := range files {
		if f.OldPath != "" {
			fmt.Fprintf(stdout, "%s\t%s -> %s\n", f.Status, f.OldPath, f.Path)
			continue
		}
		fmt.Fprintf(stdout, "%s\t%s\n", f.Status, f.Path)
	}
	return 0
}
```

- [ ] **Step 3b: Register in `cli.go`**

In `internal/cli/cli.go`, add the switch case (next to `tag`):

```go
	case "compare":
		return cmdCompare(svc, rest, stdout, stderr)
```

and add `compare` to the `commands` map:

```go
	"remote": true, "tag": true, "compare": true,
```

- [ ] **Step 3c: `cmd/gg` help string**

In `cmd/gg/main.go` (~line 65), add `compare` to the commands list string (e.g. after `tag`):

```go
	fmt.Fprintln(os.Stderr, "commands: status commit pull push switch checkout branch stash undo discard shelf bookmark merge rebase cherry-pick revert reset worktree remote tag compare repo init inspect (run `gg` with no arguments for the TUI)")
```

- [ ] **Step 4: Run, verify it passes**

Run: `cd /mnt/t/others/gg-compare-cli && go test ./internal/cli/ -run TestCompare -v`
Expected: PASS (all four).

- [ ] **Step 5: Verify the real binary routes it (known-bugs #1 lesson)**

```bash
cd /mnt/t/others/gg-compare-cli && go build -o /tmp/gg-compare ./cmd/gg
cd "$(mktemp -d)" && git init -q -b main . && echo hi > a.txt && git add . && git -c user.email=t@t -c user.name=t commit -qm c1
echo changed > a.txt
/tmp/gg-compare compare HEAD ; echo "exit=$?"   # expect: M\ta.txt, exit 0 — NOT "unknown command"
```
Expected: lists `a.txt`, exit 0. (`cli.Run` tests bypass `IsCommand`; only the built binary proves routing.)

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gg-compare-cli
git add internal/cli/compare.go internal/cli/cli.go cmd/gg/main.go internal/cli/compare_test.go
git commit -m "feat(cli): gg compare <left> [<right>] — changed files between endpoints

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01KdNVc8a85eb3E9VMwxdYZi"
```

---

### Task 2: e2e scenario

**Files:**
- Create: `e2e/scenarios/s68_compare.toml`

- [ ] **Step 1: Write the scenario**

Create `e2e/scenarios/s68_compare.toml` (mirror `s48_remote_ls.toml`'s schema; the next free number is s68):

```toml
name = "compare: changed files between commits and vs the working tree"

[input]
steps = [
  { write = "f1.txt", content = "v1\n" },
  { commit = "c1" },
  { write = "f2.txt", content = "v2\n" },
  { commit = "c2" },
  { write = "f1.txt", content = "dirty\n" },
]

[[run]]
cmd             = ["compare", "HEAD~1", "HEAD"]
exit            = 0
stdout_contains = ["f2.txt"]

[[run]]
cmd             = ["compare", "HEAD"]
exit            = 0
stdout_contains = ["f1.txt"]

[expect]
branch = "main"
```

(The trailing `write` to `f1.txt` with no `commit` leaves the working tree dirty, so `compare HEAD` lists `f1.txt`. `compare HEAD~1 HEAD` lists `f2.txt` added by c2. Drop the `[expect] clean` line — the tree is intentionally dirty.)

- [ ] **Step 2: Run the e2e suite**

Run: `cd /mnt/t/others/gg-compare-cli && go test ./e2e/ -run 'TestScenarios|Scenario' 2>&1 | tail -5`
(If the e2e runner uses a different `-run` name, run `go test ./e2e/` plainly.)
Expected: PASS, including the new scenario.

> If the scenario step schema differs (e.g. no bare `write`/`commit` ops), inspect `e2e/` harness types and an existing dirty-tree scenario, and adapt the steps — keep the two `run` blocks and their `stdout_contains`.

- [ ] **Step 3: Commit**

```bash
cd /mnt/t/others/gg-compare-cli
git add e2e/scenarios/s68_compare.toml
git commit -m "test(e2e): gg compare scenario (commit range + vs working tree)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01KdNVc8a85eb3E9VMwxdYZi"
```

---

### Task 3: agentskill + CHANGELOG + gate

**Files:**
- Modify: `internal/agentskill/using-gg.md` (document `gg compare`)
- Modify: `internal/agentskill/agentskill.go` (`Version` 22 → 23)
- Modify: `CHANGELOG.md`
- Then: `gg init --update` to refresh the installed copy (or `TestDogfoodSkillCopyInSync` fails)

- [ ] **Step 1: Document `gg compare` in the skill**

In `internal/agentskill/using-gg.md`, in the command reference (near `gg tag` / `gg status`), add:

```markdown
### `gg compare <left> [<right>]`

Print the files that changed between two endpoints. Each endpoint is a
commit-ish (`HEAD`, a branch, `abc123`, `HEAD~2`), `@staged` (the index), or
`@worktree` (the working tree). `<right>` defaults to `@worktree`.

```
gg compare HEAD            # working tree vs HEAD
gg compare HEAD~3 HEAD     # what changed across the last 3 commits
gg compare main @staged    # the index vs main
```

Output is one `<status>\t<path>` line per changed file (renames: `old -> new`).
```

- [ ] **Step 2: Bump the version**

In `internal/agentskill/agentskill.go`: `const Version = 23`.

- [ ] **Step 3: Refresh the installed copy**

```bash
cd /mnt/t/others/gg-compare-cli && go run ./cmd/gg init --update
```
(Then `git status` should show the regenerated installed skill copy if one is tracked; include it in the commit.)

- [ ] **Step 4: CHANGELOG**

Under `## [Unreleased]` → `### Added`, after the Stage 4 line:

```markdown
- **`gg compare <left> [<right>]` (CLI).** Print the files that changed between
  two endpoints — a commit-ish, `@staged`, or `@worktree` (right defaults to the
  working tree). Stage 5 (final) of commit comparison.
```

- [ ] **Step 5: Format + vet + full race gate**

```bash
cd /mnt/t/others/gg-compare-cli
gofmt -l internal/ cmd/
go vet ./...
./test.sh race
```
Expected: `gofmt` silent, `vet` exit 0, `./test.sh race` → `all green` exit 0 (read the status directly).

- [ ] **Step 6: Commit**

```bash
cd /mnt/t/others/gg-compare-cli
git add internal/agentskill/ CHANGELOG.md
git commit -m "docs(cli): document gg compare in the agent skill (v23) + changelog

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01KdNVc8a85eb3E9VMwxdYZi"
```

## Self-Review

- Spec CLI surface (`gg compare`, endpoints, default right=@worktree) → Task 1. ✅
- Registration in BOTH commands map + cmd/gg help (known-bugs #1) + real-binary verify → Task 1 Steps 3b/3c/5. ✅
- e2e: commit-range + vs-working-tree → Task 2. ✅
- agentskill bump + dogfood sync → Task 3. ✅
- Names consistent: `cmdCompare`, `parseEndpoint`, `model.Endpoint`, `CompareFiles`. ✅

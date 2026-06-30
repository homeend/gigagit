# Post-worktree hook — approval gate (addendum)

> Addendum to `2026-06-30-post-worktree-hook.md`. Adds an explicit see-it-then-approve gate so a repo-configured (clone-borne) `.gg.toml` hook never runs silently. Decided after the final whole-branch review flagged the default-on execution as a supply-chain surface.

**Goal:** Before running a configured post-create hook, show the script and require approval. Architecturally this is a mid-op **Decision** (`deps.decide`), so the TUI modal and CLI decider both resolve it.

**User decisions (locked):** prompt every time a hook would run (no persistent trust store); safe default = **skip** when no decider can answer; CLI when non-interactive (no tty) **skips unless `--hook`**.

## Global Constraints

- Module github.com/homeend/gigagit; Go 1.26. `./test.sh` is the gate; gofmt-clean.
- Decision ID: exported `engine.HookDecisionID = "post_create_hook.run"`. Options: `["run","skip"]`. The hook runs ONLY on `"run"`.
- Safe default: any decider error (including `ErrDecisionRequired` = no decider) ⇒ skip, never run.
- The hook is still non-fatal and still streams output (unchanged from the base plan).
- TUI needs NO new code for the prompt — the existing `uiDecider`+`renderModal` render `req.Prompt`+`req.Options`. The engine bounds the script shown so a long script can't overflow the modal.
- CLI flags on `gg worktree add`: `--no-hook` (skip, no prompt), `--hook` (run, no prompt), mutually exclusive; neither + interactive ⇒ prompt on stdin; neither + non-interactive ⇒ skip.

---

### Task 11: Engine approval gate

**Files:**
- Modify: `internal/engine/post_create_hook.go` (add the decision + bounded preview)
- Test: `internal/engine/create_worktree_test.go`, `internal/engine/create_worktree_for_branch_test.go`

**Interfaces:**
- Produces: `const engine.HookDecisionID = "post_create_hook.run"`. `runPostCreateHook` now calls `deps.decide` before running; runs only on `"run"`.
- Consumes: existing `DecisionRequest`/`deps.decide`/`MapDecider`.

- [ ] **Step 1: Update existing tests + add the skip test (RED)**

In `create_worktree_test.go`, the two tests that expect the hook to RUN must now supply an approving decider. Change the `OpDeps` in `TestCreateWorktreeRunsHook` and `TestCreateWorktreeHookFailureNonFatal` to include `Decider: MapDecider{HookDecisionID: "run"}`:

```go
	// TestCreateWorktreeRunsHook:
	res, err := CreateWorktree{StartPoint: "main", Branch: "f/h", Path: wt, PostCreateHook: "echo hi"}.Run(
		context.Background(), OpDeps{Repo: repo, Events: ch, HookRunner: fh, Decider: MapDecider{HookDecisionID: "run"}})
```
```go
	// TestCreateWorktreeHookFailureNonFatal:
	res, err := CreateWorktree{StartPoint: "main", Branch: "f/f", Path: wt, PostCreateHook: "false"}.Run(
		context.Background(), OpDeps{Repo: repo, HookRunner: fh, Decider: MapDecider{HookDecisionID: "run"}})
```

Add a new test asserting the gate skips without approval:

```go
func TestCreateWorktreeHookSkippedWithoutApproval(t *testing.T) {
	dir, repo := newRepo(t)
	wt := filepath.Join(filepath.Dir(dir), "wt-skip")
	fh := &fakeHookRunner{}
	ch := make(chan Event, 64)
	res, err := CreateWorktree{StartPoint: "main", Branch: "f/s", Path: wt, PostCreateHook: "echo hi"}.Run(
		context.Background(), OpDeps{Repo: repo, Events: ch, HookRunner: fh, Decider: MapDecider{HookDecisionID: "skip"}})
	close(ch)
	if err != nil {
		t.Fatal(err)
	}
	if fh.called {
		t.Fatal("hook must not run when not approved")
	}
	if !res.Changed {
		t.Fatal("worktree should still be created")
	}
	var sawSkip bool
	for _, e := range drain(ch) {
		if g, ok := e.(GitLine); ok && g.Raw == "post-create hook skipped" {
			sawSkip = true
		}
	}
	if !sawSkip {
		t.Fatal("expected a 'post-create hook skipped' line")
	}
}

func TestCreateWorktreeHookSkippedWithNoDecider(t *testing.T) {
	dir, repo := newRepo(t)
	wt := filepath.Join(filepath.Dir(dir), "wt-nodecider")
	fh := &fakeHookRunner{}
	_, err := CreateWorktree{StartPoint: "main", Branch: "f/nd", Path: wt, PostCreateHook: "echo hi"}.Run(
		context.Background(), OpDeps{Repo: repo, HookRunner: fh}) // no Decider ⇒ safe skip
	if err != nil {
		t.Fatal(err)
	}
	if fh.called {
		t.Fatal("hook must not run when no decider can approve")
	}
}
```

In `create_worktree_for_branch_test.go`, add the approving decider to `TestCreateWorktreeForBranchRunsHook` and `TestCreateWorktreeForBranchHookFailureNonFatal`:

```go
		context.Background(), OpDeps{Repo: repo, HookRunner: fh, Decider: MapDecider{HookDecisionID: "run"}})
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/engine/ -run 'CreateWorktree' -v`
Expected: FAIL — `HookDecisionID` undefined; the new skip tests fail (hook still runs unconditionally).

- [ ] **Step 3: Implement the gate**

In `internal/engine/post_create_hook.go`, add the constant + preview helper and insert the decision at the top of `runPostCreateHook` (after the empty-script guard, before building env). Ensure `fmt` and `strings` are imported.

```go
// HookDecisionID is the DecisionRequest ID for approving a post-create hook.
// The hook runs only when the decider answers "run"; any other answer — or no
// decider at all — skips it (a repo .gg.toml hook can be clone-borne, so the
// safe default is never to run unattended).
const HookDecisionID = "post_create_hook.run"

// hookPromptPreview bounds the script shown in the approval prompt so a long
// script cannot overflow the TUI modal; the full script always lives in .gg.toml.
func hookPromptPreview(script string) string {
	const maxLines = 40
	lines := strings.Split(strings.TrimRight(script, "\n"), "\n")
	if len(lines) <= maxLines {
		return strings.TrimRight(script, "\n")
	}
	return strings.Join(lines[:maxLines], "\n") +
		fmt.Sprintf("\n… (%d more lines — see .gg.toml)", len(lines)-maxLines)
}
```

Insert at the top of `runPostCreateHook`, right after the `strings.TrimSpace(script) == ""` return:

```go
	resp, derr := deps.decide(ctx, DecisionRequest{
		ID:      HookDecisionID,
		Prompt:  "Run this post-create hook?\n\n" + hookPromptPreview(script),
		Options: []string{"run", "skip"},
	})
	if derr != nil || resp.Option != "run" {
		deps.emit(ctx, GitLine{Raw: "post-create hook skipped"})
		return ""
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/engine/ -v`
Expected: PASS (whole package).

- [ ] **Step 5: Commit**

```bash
git add internal/engine/post_create_hook.go internal/engine/create_worktree_test.go internal/engine/create_worktree_for_branch_test.go
git commit -m "feat(engine): require approval before running a post-create hook"
```

---

### Task 12: CLI approval policy (`--hook`/`--no-hook`, non-interactive skip)

**Files:**
- Modify: `internal/cli/worktree.go` (`cmdWorktreeAdd`)
- Test: `internal/cli/worktree_test.go`

**Interfaces:**
- Consumes: `engine.HookDecisionID`, `cliDecider{policy,in,out,interactive}`, `stdinIsTerminal()`, `runOperation`.

- [ ] **Step 1: Update CLI tests for the new default (RED)**

The base default flipped: a non-interactive `gg worktree add` now SKIPS the hook unless `--hook`. Update `internal/cli/worktree_test.go`:
- In `TestWorktreeAddRunsConfiguredHook` and `TestWorktreeAddBranchRunsConfiguredHook`, add `--hook` to the args so the hook runs (these tests run non-interactively):
  - plain: args become `[]string{"worktree", "add", "--hook", <startpoint>}`
  - branch: `[]string{"worktree", "add", "--hook", "--branch", "hook-branch"}`
- `TestWorktreeAddNoHookFlag` / `TestWorktreeAddBranchNoHookFlag` keep `--no-hook` and still assert the marker is absent.
- Add a new test asserting the non-interactive default skips:

```go
func TestWorktreeAddHookSkippedNonInteractiveByDefault(t *testing.T) {
	dir := newCLIRepo(t) // mirror the existing hook tests' setup helper
	cfgPath := filepath.Join(dir, ".gg.toml")
	if err := config.SetWorktreePostCreateHook(cfgPath, "touch hook-ran\n"); err != nil {
		t.Fatal(err)
	}
	var out, errBuf bytes.Buffer
	// No --hook/--no-hook, stdin is non-interactive (empty reader) ⇒ default skip.
	code := Run(dir, []string{"worktree", "add", "main"}, strings.NewReader(""), &out, &errBuf)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errBuf.String())
	}
	marker := filepath.Join(filepath.Dir(dir), "wt-cli-default-skip", "hook-ran")
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("non-interactive default must skip the hook")
	}
}
```

> The exact harness (`newCLIRepo`/`Run` signature, the path template used, how the worktree dir is derived) MUST mirror the existing hook tests in this file — read them first and copy their idioms. The worktree path name (`wt-cli-default-skip`) must match whatever path template the test configures.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run Worktree -v`
Expected: FAIL — `--hook` unknown flag; default-skip test fails (hook still runs).

- [ ] **Step 3: Implement**

In `cmdWorktreeAdd` (`internal/cli/worktree.go`), add the `--hook` flag beside `--no-hook`, enforce mutual exclusion, and build the decider policy. Replace the current op-construction + `runOperation(... cliDecider{} ...)` block:

```go
	runHookFlag := fs.Bool("hook", false, "run the configured [worktree] post_create_hook without prompting")
	// (keep the existing) noHook := fs.Bool("no-hook", false, "skip the configured [worktree] post_create_hook")
```

After parsing (near the existing `--branch`+startpoint mutual-exclusion check), add:

```go
	if *noHook && *runHookFlag {
		fmt.Fprintln(stderr, "worktree add: --hook and --no-hook are mutually exclusive")
		return 2
	}
```

Where the op is built and run, replace with:

```go
	hook := cfg.Worktree.PostCreateHook
	policy := map[string]string{}
	switch {
	case *noHook:
		policy[engine.HookDecisionID] = "skip"
	case *runHookFlag:
		policy[engine.HookDecisionID] = "run"
	case !stdinIsTerminal():
		policy[engine.HookDecisionID] = "skip" // never run an unseen script in a pipeline
	}
	dec := cliDecider{policy: policy, in: stdin, out: stderr, interactive: stdinIsTerminal()}

	var op engine.Operation = engine.CreateWorktree{StartPoint: startPoint, Branch: branch, Path: path, PostCreateHook: hook}
	if *forBranch != "" {
		op = engine.CreateWorktreeForBranch{Branch: branch, Path: path, PostCreateHook: hook}
	}
	res, err := runOperation(ctxBg, svc, op, dec, stderr)
```

(Remove the old `hook := cfg.Worktree.PostCreateHook; if *noHook { hook = "" }` lines and the old `cliDecider{}` call — the hook string is now always passed; the policy/decider decides whether it runs.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run Worktree -v` then `go test ./internal/cli/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/worktree.go internal/cli/worktree_test.go
git commit -m "feat(cli): approve post-create hook via --hook/--no-hook; skip when non-interactive"
```

---

### Task 13: Docs, doc-bug fixes, cosmetics, full suite

**Files:**
- Modify: `CHANGELOG.md`, `README.md`, `CLAUDE.md`, `docs/superpowers/specs/2026-06-30-post-worktree-hook-design.md`, `internal/agentskill/using-gg.md`, `internal/agentskill/agentskill.go`, `.claude/skills/using-gg/SKILL.md` (regenerated), `internal/config/write.go` (doc comments), `internal/engine/hook_runner.go` (\r strip)

- [ ] **Step 1: Document the approval gate everywhere**

- CHANGELOG: amend the post-worktree-hook entry to state the hook now requires approval — the script is shown and you choose run/skip (TUI modal; CLI `--hook`/`--no-hook`, prompts interactively, skips non-interactively).
- README "Post-worktree hook" subsection: add an **approval/security** note — "the hook is read from the repo's `.gg.toml`, which travels on clone; gg never runs it without showing it and asking. In the TUI a modal shows the script (run/skip); on the CLI pass `--hook` to run or `--no-hook` to skip, otherwise gg prompts (and skips when not attached to a terminal)."
- Design spec: add a short "Security / trust" section recording the accepted boundary and the approval gate (this closes the final-review gap).
- using-gg.md: document `gg worktree add --hook` / `--no-hook`, the interactive prompt, and the non-interactive default (skip). Note the hook only runs after approval.

- [ ] **Step 2: Fix the CLAUDE.md field-name doc bug (final-review #2)**

In `CLAUDE.md`, the `tui` row currently says the `[h]` toggle "sets a `skipHook` flag". Correct it: the field is `runHook bool` (default true; a pre-skip that suppresses even the prompt). Also update the `engine` row to mention the `HookDecisionID` approval gate (run/skip, safe-default skip) and the `cli` row to mention `--hook`/`--no-hook` + non-interactive skip.

- [ ] **Step 3: Safe cosmetic cleanups (from reviews)**

- `internal/config/write.go`: fix the garbled `'''` notation in the `SetWorktreePostCreateHook` / `setMultilineLiteral` doc comments (use plain prose, e.g. "a TOML multi-line literal string (triple-single-quote delimited)").
- `internal/engine/hook_runner.go`: in `hookLineWriter`, strip a trailing `\r` so Windows (`\r\n`) hook output doesn't emit `GitLine`s with a trailing carriage return. In `Write`, change the emit to `w.onLine(strings.TrimSuffix(line[:len(line)-1], "\r"))` (add `strings` import) and the same `TrimSuffix(rest, "\r")` in `flush`. Keep the existing line-splitting tests green; add one assertion that a `\r\n`-terminated line yields a clean line.

- [ ] **Step 4: Version bump + regenerate the dogfood skill**

Bump `agentskill.Version` (35 → 36) in `internal/agentskill/agentskill.go`, then regenerate `.claude/skills/using-gg/SKILL.md` in sync (the same way Task 10 did — programmatically within the module, NOT `gg init --update`) so `TestDogfoodSkillCopyInSync` passes.

- [ ] **Step 5: Full suite + commit**

Run: `go build ./cmd/gg && ./test.sh`
Expected: all green (vet+gofmt, unit, e2e). Do not commit a red tree.

```bash
git add CHANGELOG.md README.md CLAUDE.md docs/superpowers/specs/2026-06-30-post-worktree-hook-design.md internal/agentskill/using-gg.md internal/agentskill/agentskill.go .claude/skills/using-gg/SKILL.md internal/config/write.go internal/engine/hook_runner.go
git commit -m "docs+polish: hook approval gate (docs, CLAUDE field fix, \r strip, version bump)"
```

---

## Self-Review

- Approval gate in engine (safe-default skip) → Task 11. ✓
- TUI prompt: free via existing uiDecider+renderModal; engine bounds the script preview → Task 11. ✓
- CLI `--hook`/`--no-hook`, interactive prompt, non-interactive skip → Task 12. ✓
- Base-plan tests that assumed silent run are updated (engine Task 11, CLI Task 12). ✓
- Final-review Important #2 (CLAUDE skipHook→runHook) → Task 13 Step 2. ✓
- Safe cosmetics (write.go doc comments, \r strip) → Task 13 Step 3. ✓
- Docs + security note + version/dogfood sync → Task 13. ✓

**Type consistency:** `HookDecisionID` defined in Task 11, consumed by Task 11 tests and Task 12 CLI. `cliDecider`/`stdinIsTerminal`/`runOperation` are existing. Options `["run","skip"]` are the same strings used by the policy map, the engine branch (`!= "run"`), and the CLI flags.

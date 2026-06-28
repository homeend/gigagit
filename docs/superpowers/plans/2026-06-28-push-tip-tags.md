# Push branch-tip tags with the branch (P) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`).

**Goal:** When `P` pushes the current branch and the branch-tip commit has local tags not on the remote, offer to push them too — verified by a fresh `ls-remote` capped at 5s (skip the check on timeout so `P` never hangs).

**Architecture:** New `engine.PushTags` (one `git push` of many tag refspecs) + `git.Repo.PushTags`. New non-coalescing `domain.RemoteTagsFresh(ctx)` so a 5s-timeout context fully governs the check. The `P` handler runs the check only when the tip has tags, then either pushes directly or shows a modal; "push both" sets `pendingPushTags`, which `opFinishedMsg` chains as a `PushTags` op after the branch push succeeds (keeping push-rejection recovery branch-only), with optimistic `remoteTagNames` updates.

**Tech Stack:** Go 1.26, Bubble Tea TUI. `internal/{git,engine,domain,tui}`.

## Global Constraints

- A git verb is ONE git invocation, built with `gitcmd`, run via `r.Runner.Run`.
- `internal/tui`/`internal/cli` MUST NOT import `internal/git` (archtest-guarded).
- `domain.RemoteTagsFresh` must NOT route through the failure seam (`observ.NoteFailure`) — a 5s timeout/offline must not spam `errors.log`. It must NOT coalesce via singleflight (so the caller's context governs the 5s budget).
- `model.Branch.Hash` and `model.Tag.Target` are BOTH `objectname:short` from the same repo → a direct `==` compares the same commit correctly.
- The tag check fires ONLY when the tip has local tags; the no-tag case stays instant (no network).
- TDD: failing test → see fail → implement → see pass → commit.

---

### Task 1: `engine.PushTags` + `git.Repo.PushTags`

**Files:**
- Modify: `internal/git/sync.go` (add `PushTags`)
- Create: `internal/engine/push_tags.go` (the op)
- Test: `internal/git/sync_test.go` (verb), `internal/engine/push_tags_test.go` (op)

**Interfaces:**
- `func (r *Repo) PushTags(ctx context.Context, remote string, names []string) error` — `git push <remote> refs/tags/<n>…` in one invocation; empty `names` → no-op (nil error, no git call).
- `engine.PushTags{Remote string; Names []string}` implementing `Operation`.

- [ ] **Step 1: Failing verb test** — in `internal/git/sync_test.go`, mirror `TestPushTag`/the existing push tests. Two cases: (a) FakeRunner argv assertion that `PushTags(ctx,"origin",[]string{"a","b"})` runs `git push origin refs/tags/a refs/tags/b`; (b) empty names → no Runner call, nil error. (Grep `PushTag` in sync_test.go for the pattern.)

- [ ] **Step 2: Run, verify fail** — `go test ./internal/git/ -run TestPushTags`.

- [ ] **Step 3: Implement the verb** (mirror `PushTag` at sync.go:138):
```go
// PushTags pushes the named tags to a remote in one invocation:
// `git push <remote> refs/tags/<n>…`. Empty names is a no-op.
func (r *Repo) PushTags(ctx context.Context, remote string, names []string) error {
	if len(names) == 0 {
		return nil
	}
	b := gitcmd.New("push").Arg(remote)
	for _, n := range names {
		b = b.Arg("refs/tags/" + n)
	}
	_, err := r.Runner.Run(ctx, "git push (tags)", b.ToArgv())
	return err
}
```
(Verify `gitcmd.Builder.Arg` is variadic-or-chainable as used here; mirror `PushTag`'s exact call style.)

- [ ] **Step 4: Failing op test** — `internal/engine/push_tags_test.go`, mirror `push_tag_test.go`. Assert `PushTags{Remote:"origin", Names:[]string{"a","b"}}.Run` calls `deps.Repo.PushTags(ctx,"origin",["a","b"])`, emits a Progress + Done, returns `Result{Summary:"pushed tags", Changed:true}`. Empty Names → `Result{Changed:false}` no push. (Grep how `push_tag_test.go` builds `OpDeps` with a fake repo.)

- [ ] **Step 5: Run, verify fail** — `go test ./internal/engine/ -run TestPushTags`.

- [ ] **Step 6: Implement the op** (mirror `push_tag.go`). PushTags needs the `GitOps` interface to expose `PushTags` — add `PushTags(ctx, remote string, names []string) error` to the `GitOps` interface (find it; `PushTag` is already there — add alongside). Then:
```go
package engine

import "context"

// PushTags pushes several tags to a remote in one invocation. Remote must be set
// by the caller (the TUI passes "origin"); if empty, it resolves like PushTag.
type PushTags struct {
	Remote string
	Names  []string
}

func (op PushTags) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if len(op.Names) == 0 {
		return Result{Summary: "no tags to push", Changed: false}, nil
	}
	remote := op.Remote
	if remote == "" {
		// reuse PushTag's remote resolution (find its helper, e.g. resolveRemote)
		r, err := resolvePushRemote(ctx, deps) // NAME PER push_tag.go's actual helper
		if err != nil {
			return Result{}, err
		}
		remote = r
	}
	deps.emit(ctx, Progress{Step: "pushing tags", Detail: remote})
	if err := deps.Repo.PushTags(ctx, remote, op.Names); err != nil {
		return Result{}, err
	}
	res := Result{Summary: "pushed tags", Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}
```
Read `push_tag.go` first and mirror its remote-resolution + emit style exactly (the `resolvePushRemote` name above is a placeholder — use the real helper, or inline "origin" handling if PushTag does). Also wire `PushTags` into any `FakeGitOps`/test double used by engine tests.

- [ ] **Step 7: Run, verify pass; packages** — `go test ./internal/git/ ./internal/engine/`.

- [ ] **Step 8: Commit**
```bash
git add internal/git/sync.go internal/git/sync_test.go internal/engine/push_tags.go internal/engine/push_tags_test.go internal/engine/*.go
git commit -m "feat(engine,git): PushTags — push multiple tags in one git push"
```

---

### Task 2: `domain.RemoteTagsFresh` (non-coalescing, context-governed)

**Files:**
- Modify: `internal/domain/query.go` (add `RemoteTagsFresh`)
- Test: `internal/domain/remote_tags_test.go`

**Interfaces:**
- `func (s *Service) RemoteTagsFresh(ctx context.Context) (map[string]bool, error)` — like `RemoteTags` (origin-or-first remote, no failure recording) but WITHOUT singleflight coalescing, so the caller's context (a 5s timeout) fully governs it.

- [ ] **Step 1: Failing test** — in `internal/domain/remote_tags_test.go` add: (a) `RemoteTagsFresh` returns the pushed-tag set for a repo with a bare remote + a pushed tag (mirror the existing `RemoteTags` origin test); (b) a cancelled context returns promptly with an error and records NO session failure (mirror the existing `queryQuiet`-no-record test — drain `observ.SessionFailures()` before/after, assert unchanged); (c) no remote → empty set, nil error.

- [ ] **Step 2: Run, verify fail** — `go test ./internal/domain/ -run RemoteTagsFresh`.

- [ ] **Step 3: Implement** — add to `query.go`, next to `RemoteTags`:
```go
// RemoteTagsFresh is RemoteTags WITHOUT singleflight coalescing: it always issues
// its own ls-remote under a Read reservation, so the caller's context (e.g. a 5s
// timeout) fully governs it — a coalesced follower of the background remote-tags
// lookup could not be cancelled by its own context. Like RemoteTags it resolves
// origin-or-first and records NO failure (a timeout/offline must not spam the
// session error log).
func (s *Service) RemoteTagsFresh(ctx context.Context) (map[string]bool, error) {
	res, err := s.gateFor(ctx).Acquire(ctx, repogate.Read, "read remote-tags-fresh")
	if err != nil {
		return nil, err
	}
	defer res.Release()
	names, err := s.repo.RemoteNames(ctx)
	if err != nil {
		return nil, err
	}
	remote := pickDefaultRemote(names)
	if remote == "" {
		return map[string]bool{}, nil
	}
	return s.repo.RemoteTags(ctx, remote)
}
```
(Reuse the existing `pickDefaultRemote`. No `s.flight.Do`, no `observ.NoteFailure`.)

- [ ] **Step 4: Run, verify pass; package** — `go test ./internal/domain/`.

- [ ] **Step 5: Commit**
```bash
git add internal/domain/query.go internal/domain/remote_tags_test.go
git commit -m "feat(domain): RemoteTagsFresh — non-coalescing, context-governed remote-tag lookup"
```

---

### Task 3: TUI — P flow (5s check), modal, chain, optimistic

**Files:**
- Modify: `internal/tui/model.go` (P handler → `startPush`; `pushTagCheckMsg` handler; `opFinishedMsg` chain + clears; new fields)
- Modify: `internal/tui/remote_tags.go` (helpers + `pushTagCheckCmd`; extend `applyPendingRemoteTag`)
- Test: `internal/tui/push_tip_tags_test.go`

**Interfaces / new Model fields:**
```go
pendingPushTags     []string // tip tags to push after a successful branch Push
pendingRemoteTagAdds []string // tags to mark on-remote (optimistic) on next op success
pushCheckGen        int      // generation guard for the async pre-push tag check
```

- [ ] **Step 1: Failing tests** — `internal/tui/push_tip_tags_test.go`. Use the package's test-model helper + construct `m.branches` (with `Hash`), `m.status.Branch`, `m.tags`. Cases (drive via `m.Update(...)` and the handlers):
  - `unpushedTipTags`/`tagsAtCommit` + tip-hash helper: tags at the tip hash, filtered by a remote set.
  - `pushTagCheckMsg` with an unpushed tip tag → opens the `push-with-tags` modal (assert `m.modal != nil`, options present).
  - `pushTagCheckMsg` with all tip tags already in `remoteSet` → no modal, starts a push (assert running / a Push cmd).
  - `pushTagCheckMsg` with `err != nil` (timeout) or `remoteSet == nil` → no modal, straight push.
  - stale gen: `pushTagCheckMsg{gen: old}` after `pushCheckGen` advanced → ignored (no modal, no push).
  - chain: simulate a branch-`Push` `opFinishedMsg` success with `m.pendingPushTags=["v1"]` → next op is `engine.PushTags{Names:["v1"]}` and `pendingPushTags` cleared; an `opFinishedMsg` ERROR with pendingPushTags set → cleared, no chain.
  - optimistic: after a `PushTags` success with `pendingRemoteTagAdds=["v1"]`, `m.remoteTagNames["v1"]` is true.

  Grep existing tests for how a Model is built with branches/tags and how `opFinishedMsg`/modal are exercised (e.g. `confirm_op_test.go`, `tags_actions_test.go`, `remote_tags_test.go`).

- [ ] **Step 2: Run, verify fail.**

- [ ] **Step 3: Implement helpers + P flow** (in `remote_tags.go` or `model.go` as fits):
```go
// tagsAtCommit returns the local tags whose target commit is hash.
func tagsAtCommit(tags []model.Tag, hash string) []model.Tag {
	if hash == "" {
		return nil
	}
	var out []model.Tag
	for _, t := range tags {
		if t.Target == hash { // both are objectname:short from the same repo
			out = append(out, t)
		}
	}
	return out
}

// currentBranchTipHash is the short commit hash at the current branch's tip.
func (m Model) currentBranchTipHash() string {
	for _, b := range m.branches {
		if b.Name == m.status.Branch {
			return b.Hash
		}
	}
	return ""
}

func (m Model) pushCurrentOp() engine.Operation {
	return engine.Push{Remote: "origin", Branch: m.status.Branch, SetUpstream: true}
}

// pushTagCheckMsg carries the 5s-budgeted pre-push remote-tag check result.
type pushTagCheckMsg struct {
	gen       int
	tipTags   []model.Tag
	remoteSet map[string]bool // nil on timeout/error → skip the tag check
	err       error
}

// pushTagCheckCmd runs a fresh, 5s-bounded remote-tag lookup off the UI thread.
func (m Model) pushTagCheckCmd(gen int, tipTags []model.Tag) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		set, err := svc.RemoteTagsFresh(ctx)
		return pushTagCheckMsg{gen: gen, tipTags: tipTags, remoteSet: set, err: err}
	}
}

// startPush begins a current-branch push. If the tip has local tags, it first
// runs a 5s-budgeted remote check to offer pushing unpushed tip tags; otherwise
// it pushes immediately.
func (m Model) startPush() (tea.Model, tea.Cmd) {
	tipTags := tagsAtCommit(m.tags, m.currentBranchTipHash())
	if len(tipTags) == 0 {
		return m.startOp(m.pushCurrentOp())
	}
	m.pushCheckGen++
	m.statusMsg = "checking remote tags…"
	return m, m.pushTagCheckCmd(m.pushCheckGen, tipTags)
}
```
P handler (model.go ~924): replace the body with `return m.startPush()` (keep the `!m.running && !m.loading && m.status.Branch != ""` guard).

`pushTagCheckMsg` handler (add to the message switch):
```go
case pushTagCheckMsg:
	if msg.gen != m.pushCheckGen {
		return m, nil // superseded (another P / op / repo switch)
	}
	m.statusMsg = ""
	if msg.err == nil && msg.remoteSet != nil {
		m.remoteTagNames = msg.remoteSet // free cache refresh
	}
	var unpushed []string
	if msg.err == nil && msg.remoteSet != nil {
		for _, t := range msg.tipTags {
			if !msg.remoteSet[t.Name] {
				unpushed = append(unpushed, t.Name)
			}
		}
	}
	if len(unpushed) == 0 {
		return m.startOp(m.pushCurrentOp()) // nothing to offer (or timed out) → just push
	}
	noun := "tag " + strings.Join(unpushed, ", ")
	if len(unpushed) > 1 {
		noun = "tags " + strings.Join(unpushed, ", ")
	}
	m.modal = &decisionState{
		req: engine.DecisionRequest{
			ID:      "push-with-tags",
			Prompt:  "Branch tip has " + noun + " not on the remote. Push too?",
			Options: []string{"Push branch + tags", "Push branch only", "Cancel"},
		},
		sel: 0,
		onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
			switch opt {
			case "Push branch + tags":
				m.pendingPushTags = unpushed
				return m.startOp(m.pushCurrentOp())
			case "Push branch only":
				return m.startOp(m.pushCurrentOp())
			default:
				return m, nil
			}
		},
	}
	return m, nil
```

- [ ] **Step 4: Implement the chain + optimistic** in the `opFinishedMsg` handler (model.go ~1567):
  - ERROR path: also clear `m.pendingPushTags = nil` and `m.pendingRemoteTagAdds = nil` (beside the existing `pendingRemoteTagSet`/`Unset` clears).
  - SUCCESS path: capture and clear before the chain returns:
    ```go
    pushTags := m.pendingPushTags
    m.pendingPushTags = nil
    ```
    Place this with the other pending captures/clears. Then AFTER the existing `switchTo`/`chainSwitch` early returns (a branch Push sets neither, so control reaches here), add:
    ```go
    if len(pushTags) > 0 {
        m.pendingRemoteTagAdds = pushTags // optimistic: mark on-remote when PushTags succeeds
        return m.startOp(engine.PushTags{Remote: "origin", Names: pushTags})
    }
    ```
  - Extend `applyPendingRemoteTag` (remote_tags.go) to also drain the slice (so the chained `PushTags` success marks them on-remote — `applyPendingRemoteTag` already runs on every op success at ~line 1601):
    ```go
    for _, n := range m.pendingRemoteTagAdds {
        if m.remoteTagNames == nil {
            m.remoteTagNames = map[string]bool{}
        }
        m.remoteTagNames[n] = true
    }
    m.pendingRemoteTagAdds = nil
    ```
  CRITICAL ordering: the branch-Push success runs `applyPendingRemoteTag` (adds nothing yet), THEN sets `pendingRemoteTagAdds` and chains `PushTags`; the `PushTags` success then runs `applyPendingRemoteTag` and adds them. The chained `PushTags` op carries `pendingPushTags==nil`, so it never re-chains. Verify this flow in a test.

- [ ] **Step 5: Run, verify pass; build + package** — `go test ./internal/tui/ -run "PushTipTags|TagsAtCommit|StartPush" && go test ./internal/tui/ && go build ./cmd/gg`. Imports: `context`, `time`, `strings`, `engine`, `model` (add as needed).

- [ ] **Step 6: Commit**
```bash
git add internal/tui/model.go internal/tui/remote_tags.go internal/tui/push_tip_tags_test.go
git commit -m "feat(tui): P offers to push unpushed branch-tip tags (5s-checked, chained)"
```

---

### Task 4: docs + memory

- [ ] **Step 1: CHANGELOG** — `P` now offers to push an unpushed tag at the branch tip (verified with a 5s-budgeted ls-remote; skipped on timeout); pushes branch then the tag(s).
- [ ] **Step 2: README** — document the prompt + the 5s-skip behavior + that only branch-tip tags are considered, in the push/tags section.
- [ ] **Step 3: CLAUDE.md** — `engine` row: add `PushTags`. `domain` row: add `RemoteTagsFresh` (non-coalescing, context-governed, no failure-record). `tui` row: note the `P` pre-push tip-tag check (`startPush`/`pushTagCheckMsg`, 5s budget, `pendingPushTags` chain).
- [ ] **Step 4: memory** — `push-tip-tags-feature.md` (type project): P→`startPush` checks tip tags via `RemoteTagsFresh` (5s ctx, skip on timeout); `pushCheckGen` drops stale results; modal → `pendingPushTags` → chained `engine.PushTags` after branch push; optimistic `pendingRemoteTagAdds`. Link `[[tag-remote-indicator-feature]]`, `[[auto-remote-tags-feature]]`. Add a `MEMORY.md` index line.
- [ ] **Step 5: Build + test** — `go build ./cmd/gg && ./test.sh unit`.
- [ ] **Step 6: Commit**
```bash
git add CHANGELOG.md README.md CLAUDE.md
git commit -m "docs: P pushes unpushed branch-tip tags"
```

---

## Self-review notes

- **5s guarantee:** relies on `RemoteTagsFresh` being non-coalescing + the caller's `context.WithTimeout(5s)`; the gate `Acquire(ctx)` and `LimitRunner`/`ExecRunner` all honor ctx, so the cmd returns within ~5s. The `pushCheckGen` guard drops a late result regardless.
- **Hash compare:** `Branch.Hash` and `Tag.Target` are both `objectname:short` (same repo) → `==` is correct.
- **No re-chain loop:** `pendingPushTags` is captured-and-cleared on the branch-push success before chaining; the `PushTags` op has none.
- **Failure seam:** `RemoteTagsFresh` records nothing → an offline `P` doesn't spam `errors.log`.
- **Never-trap:** the modal always has `Cancel`.
- **No-tag fast path:** the network check only fires when the tip has local tags.

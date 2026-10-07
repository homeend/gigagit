# GitHub write-back — Plan 2: send core + CLI

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
> **gigagit rule (CLAUDE.md): NO implementer subagents** — this plan is executed by the session that wrote it (executing-plans); only the final whole-branch review may be a read-only subagent.
> Every commit message below ends with the session's attribution trailer lines (`Co-Authored-By: …` / `Claude-Session: …`); the steps show the subject only.

**Goal:** Local notes, single review remarks, whole AI reviews and verdicts go to a GitHub pull request from gg — all-or-nothing per review — replies and resolve/unresolve work on GitHub threads, and a sent item is deleted locally so GitHub is its only home; agents can only queue a send that the user approves.

**Architecture:** `forge` gains a `Writer` (GraphQL mutations through `gh api graphql --input <temp file>`). `engine.SendToForge` is a thin op in the `ApplyMigration` style: it holds the confirm decision and the write ORDER, and is handed a domain-built `Plan` value (refresh + re-anchor + rendered bodies, computed outside the repo gate right before the op runs — R12), the `forge.Writer`, and a domain `SendLedger` (stamp / fail / settle; gate-free). Domain stamps local items before the forge sees them, and ONE settle pass — run on every PR snapshot read — turns stamps into deletions (sent), keeps them (still pending on GitHub) or clears them (gone): that pass is write-through, crash recovery and the echo-marker match at once. A file-backed pending-send queue (domain + filelock) is how an agent's send waits for the user. The CLI gets `gg pr send|reply|resolve|unresolve|notes|pending`.

**Tech Stack:** Go 1.26, `gh` CLI (`gh api graphql --input`), `pelletier/go-toml/v2`, `internal/filelock`, the fake gh (`internal/forge/testdata/fakegh`).

**Spec:** `docs/superpowers/specs/2026-10-07-github-write-design.md` — §1 (data model), §3 (sending), §4.3 (CLI). Plan 1 (PR cache, §2) is merged on `main` (`49e37a6e`).

## Global Constraints

- Nothing in this plan (code or tests) posts to real GitHub. Every test runs against `FakeRunner` or the fake gh. The one manual probe (Task 12) runs only against a scratch repository of the user's and only after the user says yes.
- All writes go through `gh api graphql --input <file>`; the file is `{"query": "...", "variables": {...}}`, created with `os.CreateTemp` and removed after the call. gg's runner has no stdin.
- Every thread is created by `addPullRequestReviewThread(pullRequestReviewId, …)` — line threads too — because its payload returns `thread{id}` and `addPullRequestReview`'s does not (verified by introspection 2026-10-07). `addPullRequestReviewThreadReply` takes `pullRequestReviewId`, so a moved root's local replies join the same pending review.
- A review is all or nothing: any failure before `submitPullRequestReview` succeeds deletes the pending review (`deletePullRequestReview`) — except when gg joined the user's OWN pending review (`submit-with-pending`), which gg never deletes.
- Resolve runs only AFTER submit (a pending thread cannot be resolved), one call per thread; a failure is reported, not rolled back.
- Every posted body ends with the invisible marker `<!-- gg:<key> -->` (key = a stored note id, or `remark:<review id>:<remark fp>`).
- AI-written bodies end with `— <agent> via gg`; their rationale and meta fold into one `<details>` block.
- Decision ids and option VALUES are English protocol: `forge.send` (`comment` / `approve` / `request-changes` / `send` / `submit-with-pending` / `discard` / `abort`). `approve` and `request-changes` are never offered on the user's own PR.
- An agent never posts: inside a gg session (`GG_INBOX` set) every write verb queues a pending send; `--yes` there is ignored with a stderr line; `gg pr pending approve|reject` is refused there.
- Pending sends: `stateBaseDir("pending-sends")/<repoKey>.toml`, lock `<file>.lock`; long-poll default **10 min**; entries older than **24 h** expire.
- `internal/tui`, `internal/cli`, `internal/web`, `internal/mcp` never import `forge`, `notes`, `prcache` or `internal/git` (archtest). `engine` may import `forge` (types + the `Writer` interface); `forge` never imports `engine`.
- Engine prose via `msg.go` helpers only; every new format / option value in all four i18n bundles; new option values get `optionDisplayName` cases.
- Stored/drawn times use `internal/clock` / `notes.Now()`; tests freeze them.
- Tests use real git in `t.TempDir()` or `FakeRunner`; new tests call `t.Parallel()` unless they set env (then serial, commented); every TestMain that can reach the state dir pins `XDG_STATE_HOME`/`XDG_CONFIG_HOME`.

## Review Focus

1. **A send killed after the pending review exists** (gh killed, gg crashed, network gone) → at the next snapshot read the stamped items stay `◌`, `gg pr send <n> --finish` submits them, `--discard` deletes the pending review only when gg's stamps name it (never the user's own browser draft). (Task 5 + Task 7 tests.)
2. **A settle pass racing a send in another gg process**: a snapshot read that STARTED before an item was stamped must never clear that stamp. (Task 5 test.)
3. **A review re-saved after one of its remarks moved to GitHub** (`gg review save` with the same id, a re-imported reply) → the moved remark stays hidden; it does not come back as local. (Task 3 test.)
4. **A local draft reply to a GitHub thread** survives every store mutation — another note's removal, the orphan prune, the cap. (Task 2 test.)
5. **A mixed group** — one note inside a diff hunk, one in a changed file outside every hunk, one in a file the PR does not touch, one stale → line thread, file-level thread with a quote, skipped "not in this PR", skipped "its lines changed"; the skipped notes stay local and untouched. (Task 6 test.)
6. **A writer queued behind the send** (a commit started while the confirm is open) → the op's settle step never blocks on the repo gate. (Task 5 test.)
7. **`diff.context` set in the user's git config** → hunk membership still matches GitHub's 3-line context. (Task 6 test.)

## Rulings this plan makes on the spec (for the user's review)

- **R1 — every thread via `AddThread`.** The spec table gives `StartReview` the line threads; its payload has no thread ids, so stamps could not be written per item. `StartReview` creates an empty pending review; one `AddThread` per item. Cost: one `gh` call per thread.
- **R2 — write-through is a re-read.** After submit the op calls the ledger's `Settle`, which is one snapshot read (`PRRevalidate`) plus the settle pass; no cache entry is hand-built from mutation payloads (§2.6). Cost: one extra call per send.
- **R3 — carried notes are not cached with derived data** (§1.4 says they are). They depend on the note store, which changes far more often than a PR's git data; they are memory-cached per (tip, base, notes generation) like `previewCounts`. A disk cache would show a just-added note hours late.
- **R4 — a whole review with skipped remarks is kept.** §1.2 deletes the review note on send; when some remarks could not be sent (file not in the PR, stale), the note stays with the sent remarks marked moved, so nothing is lost. Its summary is then on GitHub and its unsent remarks local.
- **R5 — a moved head is fetched by the CLI before the op.** The op runs under a Read reservation and cannot fetch; `gg pr send` revalidates first and runs `PRFetchOp` when the head moved. The op's plan refuses a moved head (`ErrPRHeadMoved`) as a guard.
- **R6 — joining the user's pending review submits with `COMMENT`**, and `--yes` never answers `submit-with-pending` (it needs a human).
- **R7 — `gg pr send <n> --finish | --discard`** is plan 2's gesture for an interrupted send (the notice centre is plan 3). `--discard` is refused when no local stamp names the pending review.
- **R8 — `gg pr notes <n> [--json]`** is added: the CLI's way to see what a PR's view holds (every local note, carried ones with their origin, GitHub threads, each with its sync state) — the items `gg pr send --note` takes.
- **R9 — `gg note list` prints a sync token only when it is not `local`** (`[sending]`, `[failed: <error>]`), and `(from <origin>)` on a carried note, so the existing line shape agents parse is unchanged for every plain note; `--json` carries `sync`, `send_error`, `group`, `origin`.
- **R10 — empty-body `COMMENT`** is sent as is; when GitHub refuses a blank body, the submit is retried once with "1 comment" / "N comments" (§3.4's fallback), so the unverified API answer cannot fail a send.
- **R12 — the plan is computed before the op runs, outside the repo gate.** Spec §3.3 has the op refresh and re-anchor; but every domain read takes its own Read reservation and the gate is not re-entrant (a writer queued behind the op's Read would deadlock a nested read). `PRSendOp` builds the plan — refresh, settle, re-anchor, bodies — immediately before handing the op over; the op holds it as a value. The confirm may sit open for a while; threads then anchor on the head the plan read (`commitOID`), which GitHub accepts (an older line just shows as outdated if it changed). The op's only domain call afterwards, the ledger's `Settle`, must be gate-free (Task 5 tests it under a queued writer).
- **R11 — skills.** CLAUDE.md asks for a `using-gg.md` update on any CLI change; spec §7 defers skills to plan 5. This plan adds one short "sending to GitHub" paragraph (version bump, `gg init --update`); plan 5 writes the full agent guidance.

---
### Task 1: `forge.Writer` over `gh api graphql --input`, the send marker, a recording fake gh

**Files:**
- Create: `internal/forge/writer.go` (types, `Writer`, marker helpers, `IsBlankBodyError`)
- Create: `internal/forge/gh_write.go` (`*GH` implements `Writer`)
- Create: `internal/forge/gh_write_test.go`
- Modify: `internal/forge/gh_parse.go` (a review whose body is only a marker counts as empty)
- Modify: `internal/forge/gh_test.go` (`TestGHArgvIsReadOnly` → the READ methods stay read-only)
- Modify: `internal/forge/forge.go` (package doc: read seam + optional writer)
- Modify: `internal/forge/testdata/fakegh/main.go` (mutations from `--input`, `writes.jsonl`, `fail-<Op>`, `snapshot-<n>-sent.json`)
- Modify: `internal/forge/forgetest/forgetest.go` (`Writes`)

**Interfaces:**
- Consumes: `gitexec.Runner.RunEnv`, `GH.run`, `ghEnv`.
- Produces:
  ```go
  type Event string
  const (EventComment Event = "COMMENT"; EventApprove Event = "APPROVE"; EventRequestChanges Event = "REQUEST_CHANGES")
  type Thread struct {
      Path      string
      Line      int            // 0 = a file-level thread
      StartLine int            // 0, or == Line: one line
      Side      model.NoteSide // new = RIGHT, old = LEFT (ignored for a file-level thread)
      Body      string
  }
  type ThreadRef struct{ ID, CommentID, URL string }
  type CommentRef struct{ ID, URL string }
  type Writer interface {
      StartReview(ctx context.Context, prID, commit string) (reviewID string, err error)
      AddThread(ctx context.Context, reviewID string, t Thread) (ThreadRef, error)
      Reply(ctx context.Context, reviewID, threadID, body string) (CommentRef, error) // reviewID "" = a standalone reply
      SubmitReview(ctx context.Context, reviewID string, ev Event, body string) error
      DeletePendingReview(ctx context.Context, reviewID string) error
      Resolve(ctx context.Context, threadID string) error
      Unresolve(ctx context.Context, threadID string) error
  }
  func SendMarker(key string) string                 // "<!-- gg:" + key + " -->"
  func SendMarkerKey(body string) (string, bool)     // the key of the LAST marker in body
  func StripSendMarker(body string) string           // body without any trailing marker line
  func IsBlankBodyError(err error) bool
  // forgetest:
  type Write struct{ Op string; Vars map[string]any; Failed bool }
  func Writes(t testing.TB, dir string) []Write
  ```

- [ ] **Step 1: Write the failing tests** (`internal/forge/gh_write_test.go`)

```go
package forge

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/forge/forgetest"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
)

// inputOf reads the --input file a mutation was handed (the handler runs
// before the GH method removes it).
func inputOf(t *testing.T, argv []string) map[string]any {
	t.Helper()
	i := slices.Index(argv, "--input")
	if i < 0 || i+1 >= len(argv) {
		t.Fatalf("argv %v has no --input file", argv)
	}
	b, err := os.ReadFile(argv[i+1])
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestGHWriterSendsEachMutationThroughAnInputFile(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	var docs []map[string]any
	var files []string
	answer := func(out string) func(context.Context, []string) (gitexec.Result, error) {
		return func(_ context.Context, argv []string) (gitexec.Result, error) {
			if argv[0] != "api" || argv[1] != "graphql" {
				t.Errorf("argv = %v", argv)
			}
			docs = append(docs, inputOf(t, argv))
			files = append(files, argv[slices.Index(argv, "--input")+1])
			return gitexec.Result{Stdout: out}, nil
		}
	}
	f.SetHandler("gh api graphql (StartReview)", answer(`{"data":{"addPullRequestReview":{"pullRequestReview":{"id":"PRR_1"}}}}`))
	f.SetHandler("gh api graphql (AddThread)", answer(`{"data":{"addPullRequestReviewThread":{"thread":{"id":"PRRT_1","comments":{"nodes":[{"id":"PRRC_1","url":"https://x/1"}]}}}}}`))
	f.SetHandler("gh api graphql (Reply)", answer(`{"data":{"addPullRequestReviewThreadReply":{"comment":{"id":"PRRC_2","url":"https://x/2"}}}}`))
	f.SetHandler("gh api graphql (SubmitReview)", answer(`{"data":{"submitPullRequestReview":{"pullRequestReview":{"id":"PRR_1","state":"COMMENTED"}}}}`))
	g := NewGHWithRunner(f)
	ctx := context.Background()
	id, err := g.StartReview(ctx, "PR_kw7", "h1h1")
	if err != nil || id != "PRR_1" {
		t.Fatalf("StartReview = %q, %v", id, err)
	}
	ref, err := g.AddThread(ctx, id, Thread{Path: "a.go", Line: 12, StartLine: 10, Side: model.NoteSideNew, Body: "why?"})
	if err != nil || ref != (ThreadRef{ID: "PRRT_1", CommentID: "PRRC_1", URL: "https://x/1"}) {
		t.Fatalf("AddThread = %+v, %v", ref, err)
	}
	if _, err := g.AddThread(ctx, id, Thread{Path: "b.go", Body: "whole file"}); err != nil {
		t.Fatal(err)
	}
	c, err := g.Reply(ctx, id, "PRRT_1", "and also")
	if err != nil || c.ID != "PRRC_2" {
		t.Fatalf("Reply = %+v, %v", c, err)
	}
	if err := g.SubmitReview(ctx, id, EventApprove, "lgtm"); err != nil {
		t.Fatal(err)
	}
	vars := func(i int) map[string]any { return docs[i]["variables"].(map[string]any) }
	if q := docs[0]["query"].(string); !strings.HasPrefix(q, "mutation StartReview(") || vars(0)["pr"] != "PR_kw7" || vars(0)["commit"] != "h1h1" {
		t.Errorf("StartReview doc = %v", docs[0])
	}
	if v := vars(1); v["review"] != "PRR_1" || v["path"] != "a.go" || v["line"] != 12.0 || v["startLine"] != 10.0 ||
		v["side"] != "RIGHT" || v["startSide"] != "RIGHT" || v["subject"] != "LINE" {
		t.Errorf("line thread vars = %v", v)
	}
	if v := vars(2); v["subject"] != "FILE" || v["line"] != nil || v["side"] != nil {
		t.Errorf("file thread vars = %v (a file-level thread carries no line or side)", v)
	}
	if v := vars(3); v["thread"] != "PRRT_1" || v["review"] != "PRR_1" || v["body"] != "and also" {
		t.Errorf("reply vars = %v", v)
	}
	if v := vars(4); v["event"] != "APPROVE" || v["body"] != "lgtm" {
		t.Errorf("submit vars = %v", v)
	}
	for _, p := range files {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("input file %s left behind", p)
		}
	}
}

func TestGHWriterOneLineThreadSendsNoStartLine(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	var doc map[string]any
	f.SetHandler("gh api graphql (AddThread)", func(_ context.Context, argv []string) (gitexec.Result, error) {
		doc = inputOf(t, argv)
		return gitexec.Result{Stdout: `{"data":{"addPullRequestReviewThread":{"thread":{"id":"T","comments":{"nodes":[]}}}}}`}, nil
	})
	if _, err := NewGHWithRunner(f).AddThread(context.Background(), "R",
		Thread{Path: "a.go", Line: 3, StartLine: 3, Side: model.NoteSideOld, Body: "x"}); err != nil {
		t.Fatal(err)
	}
	v := doc["variables"].(map[string]any)
	if v["side"] != "LEFT" || v["startLine"] != nil || v["startSide"] != nil {
		t.Errorf("vars = %v", v)
	}
}

func TestGHWriterSurfacesGraphQLErrors(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	f.SetHandler("gh api graphql (SubmitReview)", func(context.Context, []string) (gitexec.Result, error) {
		return gitexec.Result{Stdout: `{"errors":[{"message":"Body can't be blank"}]}`, Stderr: "gh: Body can't be blank"},
			errors.New("exit status 1")
	})
	err := NewGHWithRunner(f).SubmitReview(context.Background(), "R", EventComment, "")
	if err == nil || !strings.Contains(err.Error(), "Body can't be blank") || !IsBlankBodyError(err) {
		t.Fatalf("err = %v", err)
	}
	f.SetHandler("gh api graphql (Resolve)", func(context.Context, []string) (gitexec.Result, error) {
		return gitexec.Result{Stdout: `{"data":null,"errors":[{"message":"Resource not accessible by integration"}]}`}, nil
	})
	if err := NewGHWithRunner(f).Resolve(context.Background(), "T"); err == nil || IsBlankBodyError(err) {
		t.Fatalf("a GraphQL error on exit 0 must still fail: %v", err)
	}
}

func TestSendMarker(t *testing.T) {
	t.Parallel()
	body := "why?\n\n" + SendMarker("a1b2c3d4")
	if k, ok := SendMarkerKey(body); !ok || k != "a1b2c3d4" {
		t.Fatalf("SendMarkerKey = %q, %v", k, ok)
	}
	if got := StripSendMarker(body); got != "why?" {
		t.Fatalf("StripSendMarker = %q", got)
	}
	if k, ok := SendMarkerKey("plain <!-- note -->"); ok {
		t.Fatalf("a foreign HTML comment is not a marker: %q", k)
	}
	if got := StripSendMarker("no marker"); got != "no marker" {
		t.Fatalf("StripSendMarker changed an unmarked body: %q", got)
	}
}

func TestReviewWithOnlyAMarkerIsAnEmptyEnvelope(t *testing.T) {
	t.Parallel()
	doc := `{"data":{"repository":{"pullRequest":{"reviewThreads":{"pageInfo":{},"nodes":[]},"comments":{"pageInfo":{},"nodes":[]},
"reviews":{"pageInfo":{},"nodes":[{"id":"R1","author":{"login":"me"},"body":"<!-- gg:n1 -->","state":"COMMENTED","submittedAt":"2026-10-07T10:00:00Z"}]}}}}}`
	cs, _, err := parseThreads([]byte(doc))
	if err != nil || len(cs) != 0 {
		t.Fatalf("parseThreads = %+v, %v (a marker-only COMMENTED review is the bare envelope)", cs, err)
	}
}

// Serial: t.Setenv. The real subprocess path against the fake binary: every
// mutation is recorded, a fail-<Op> file fails it.
func TestGHWriterAgainstFakeBinary(t *testing.T) {
	bin := forgetest.BuildFakeGH(t)
	dir := t.TempDir()
	t.Setenv(forgetest.EnvFixtures, dir)
	t.Setenv(EnvBin, bin)
	g := NewGH(t.TempDir(), nil)
	ctx := context.Background()
	id, err := g.StartReview(ctx, "PR_kw7", "h1h1")
	if err != nil || id == "" {
		t.Fatalf("StartReview = %q, %v", id, err)
	}
	ref, err := g.AddThread(ctx, id, Thread{Path: "a.go", Line: 4, Side: model.NoteSideNew, Body: "x"})
	if err != nil || ref.ID == "" || ref.CommentID == "" {
		t.Fatalf("AddThread = %+v, %v", ref, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fail-SubmitReview"), []byte("GraphQL: Body can't be blank"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := g.SubmitReview(ctx, id, EventComment, ""); !IsBlankBodyError(err) {
		t.Fatalf("SubmitReview err = %v", err)
	}
	ws := forgetest.Writes(t, dir)
	var ops []string
	for _, w := range ws {
		ops = append(ops, w.Op)
	}
	if strings.Join(ops, ",") != "StartReview,AddThread,SubmitReview" {
		t.Fatalf("writes = %v", ops)
	}
	if ws[1].Vars["path"] != "a.go" {
		t.Errorf("AddThread vars = %v", ws[1].Vars)
	}
}
```

Also change `TestGHArgvIsReadOnly`'s comment and assertion scope: it drives only the `Provider` methods (as today); add one line asserting that none of those calls used `--input` (the write path's tell):

```go
	for _, argv := range seen {
		if slices.Contains(argv, "--input") {
			t.Errorf("read argv %v uses --input (the write path)", argv)
		}
	}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/forge/ -run 'GHWriter|SendMarker|OnlyAMarker' -count=1`
Expected: FAIL — `undefined: Thread`, `undefined: SendMarker`, … (compile errors).

- [ ] **Step 3: Implement `internal/forge/writer.go`**

```go
package forge

import (
	"context"
	"regexp"
	"strings"

	"github.com/homeend/gigagit/internal/model"
)

// Event is a submitted review's verdict.
type Event string

const (
	EventComment        Event = "COMMENT"
	EventApprove        Event = "APPROVE"
	EventRequestChanges Event = "REQUEST_CHANGES"
)

// Thread is one review thread to create. Line 0 makes it file-level.
type Thread struct {
	Path      string
	Line      int
	StartLine int
	Side      model.NoteSide
	Body      string
}

// ThreadRef is a created thread: its id, its first comment's id and URL.
type ThreadRef struct{ ID, CommentID, URL string }

// CommentRef is a created reply.
type CommentRef struct{ ID, URL string }

// Writer posts to a forge. It is optional beside Provider (which stays
// read-only); domain reaches it only through engine.SendToForge. v2's PR
// lifecycle and metadata methods join this interface.
type Writer interface {
	// StartReview opens the viewer's pending (unsubmitted) review on commit.
	StartReview(ctx context.Context, prID, commit string) (reviewID string, err error)
	AddThread(ctx context.Context, reviewID string, t Thread) (ThreadRef, error)
	// Reply answers a thread; reviewID "" posts it on its own (submitted at
	// once), otherwise it joins that pending review.
	Reply(ctx context.Context, reviewID, threadID, body string) (CommentRef, error)
	SubmitReview(ctx context.Context, reviewID string, ev Event, body string) error
	DeletePendingReview(ctx context.Context, reviewID string) error
	Resolve(ctx context.Context, threadID string) error
	Unresolve(ctx context.Context, threadID string) error
}

const markerOpen, markerClose = "<!-- gg:", " -->"

// markerRe matches a gg send marker on a line of its own.
var markerRe = regexp.MustCompile(`(?m)^<!-- gg:([A-Za-z0-9:._-]+) -->[ \t]*$`)

// SendMarker is the invisible last line of every body gg posts: the local
// item's key, so an echo is matched to its local copy even when the send's
// stamp was lost.
func SendMarker(key string) string { return markerOpen + key + markerClose }

// SendMarkerKey is the key of body's LAST marker.
func SendMarkerKey(body string) (string, bool) {
	all := markerRe.FindAllStringSubmatch(body, -1)
	if len(all) == 0 {
		return "", false
	}
	return all[len(all)-1][1], true
}

// StripSendMarker drops every marker line and the blank lines it leaves at
// the end.
func StripSendMarker(body string) string {
	if !strings.Contains(body, markerOpen) {
		return body
	}
	return strings.TrimRight(markerRe.ReplaceAllString(body, ""), " \t\r\n")
}

// IsBlankBodyError reports the forge refusing a review because its body is
// empty (the REST rule; whether GraphQL applies it to COMMENT is unverified).
func IsBlankBodyError(err error) bool {
	if err == nil {
		return false
	}
	m := strings.ToLower(err.Error())
	return strings.Contains(m, "body") && (strings.Contains(m, "blank") || strings.Contains(m, "empty"))
}
```

- [ ] **Step 4: Implement `internal/forge/gh_write.go`**

```go
package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/homeend/gigagit/internal/model"
)

// The mutations. Each operation name is what the fake gh records and what
// the runner's span is named after.
const (
	mStartReview = `mutation StartReview($pr:ID!,$commit:GitObjectID){addPullRequestReview(input:{pullRequestId:$pr,commitOID:$commit}){pullRequestReview{id}}}`
	mAddThread   = `mutation AddThread($review:ID!,$path:String!,$body:String!,$line:Int,$startLine:Int,$side:DiffSide,$startSide:DiffSide,$subject:PullRequestReviewThreadSubjectType){addPullRequestReviewThread(input:{pullRequestReviewId:$review,path:$path,body:$body,line:$line,startLine:$startLine,side:$side,startSide:$startSide,subjectType:$subject}){thread{id comments(first:1){nodes{id url}}}}}`
	mReply       = `mutation Reply($thread:ID!,$review:ID,$body:String!){addPullRequestReviewThreadReply(input:{pullRequestReviewThreadId:$thread,pullRequestReviewId:$review,body:$body}){comment{id url}}}`
	mSubmit      = `mutation SubmitReview($review:ID!,$event:PullRequestReviewEvent!,$body:String){submitPullRequestReview(input:{pullRequestReviewId:$review,event:$event,body:$body}){pullRequestReview{id state}}}`
	mDelete      = `mutation DeleteReview($review:ID!){deletePullRequestReview(input:{pullRequestReviewId:$review}){pullRequestReview{id}}}`
	mResolve     = `mutation Resolve($thread:ID!){resolveReviewThread(input:{threadId:$thread}){thread{id isResolved}}}`
	mUnresolve   = `mutation Unresolve($thread:ID!){unresolveReviewThread(input:{threadId:$thread}){thread{id isResolved}}}`
)

var _ Writer = (*GH)(nil)

// mutate posts one mutation: the query and variables go in a temp file
// (gg's runner has no stdin), out receives the "data" object. A GraphQL
// error fails the call even when gh exits 0.
func (g *GH) mutate(ctx context.Context, op, query string, vars map[string]any, out any) error {
	f, err := os.CreateTemp("", "gg-gh-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := json.NewEncoder(f).Encode(map[string]any{"query": query, "variables": vars}); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	res, runErr := g.run(ctx, "gh api graphql ("+op+")", "api", "graphql", "--input", f.Name())
	var doc struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	_ = json.Unmarshal([]byte(res.Stdout), &doc)
	if len(doc.Errors) > 0 {
		var msgs []string
		for _, e := range doc.Errors {
			msgs = append(msgs, e.Message)
		}
		return fmt.Errorf("%s: %s", op, strings.Join(msgs, "; "))
	}
	if runErr != nil {
		if msg := strings.TrimSpace(res.Stderr); msg != "" {
			return fmt.Errorf("%s: %s", op, msg)
		}
		return fmt.Errorf("%s: %w", op, runErr)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(doc.Data, out)
}

func ghSide(s model.NoteSide) string {
	if s == model.NoteSideOld {
		return "LEFT"
	}
	return "RIGHT"
}

func (g *GH) StartReview(ctx context.Context, prID, commit string) (string, error) {
	var out struct {
		R struct {
			Review struct{ ID string } `json:"pullRequestReview"`
		} `json:"addPullRequestReview"`
	}
	vars := map[string]any{"pr": prID}
	if commit != "" {
		vars["commit"] = commit
	}
	if err := g.mutate(ctx, "StartReview", mStartReview, vars, &out); err != nil {
		return "", err
	}
	return out.R.Review.ID, nil
}

func (g *GH) AddThread(ctx context.Context, reviewID string, t Thread) (ThreadRef, error) {
	vars := map[string]any{"review": reviewID, "path": t.Path, "body": t.Body, "subject": "FILE"}
	if t.Line > 0 {
		vars["subject"], vars["line"], vars["side"] = "LINE", t.Line, ghSide(t.Side)
		if t.StartLine > 0 && t.StartLine < t.Line {
			vars["startLine"], vars["startSide"] = t.StartLine, ghSide(t.Side)
		}
	}
	var out struct {
		R struct {
			Thread struct {
				ID       string
				Comments struct {
					Nodes []struct{ ID, URL string }
				}
			}
		} `json:"addPullRequestReviewThread"`
	}
	if err := g.mutate(ctx, "AddThread", mAddThread, vars, &out); err != nil {
		return ThreadRef{}, err
	}
	ref := ThreadRef{ID: out.R.Thread.ID}
	if n := out.R.Thread.Comments.Nodes; len(n) > 0 {
		ref.CommentID, ref.URL = n[0].ID, n[0].URL
	}
	return ref, nil
}

func (g *GH) Reply(ctx context.Context, reviewID, threadID, body string) (CommentRef, error) {
	vars := map[string]any{"thread": threadID, "body": body}
	if reviewID != "" {
		vars["review"] = reviewID
	}
	var out struct {
		R struct {
			Comment struct{ ID, URL string }
		} `json:"addPullRequestReviewThreadReply"`
	}
	if err := g.mutate(ctx, "Reply", mReply, vars, &out); err != nil {
		return CommentRef{}, err
	}
	return CommentRef{ID: out.R.Comment.ID, URL: out.R.Comment.URL}, nil
}

func (g *GH) SubmitReview(ctx context.Context, reviewID string, ev Event, body string) error {
	return g.mutate(ctx, "SubmitReview", mSubmit, map[string]any{"review": reviewID, "event": string(ev), "body": body}, nil)
}

func (g *GH) DeletePendingReview(ctx context.Context, reviewID string) error {
	return g.mutate(ctx, "DeleteReview", mDelete, map[string]any{"review": reviewID}, nil)
}

func (g *GH) Resolve(ctx context.Context, threadID string) error {
	return g.mutate(ctx, "Resolve", mResolve, map[string]any{"thread": threadID}, nil)
}

func (g *GH) Unresolve(ctx context.Context, threadID string) error {
	return g.mutate(ctx, "Unresolve", mUnresolve, map[string]any{"thread": threadID}, nil)
}
```

In `gh_parse.go`'s review loop, judge emptiness on the body without its marker:

```go
		if verdict == "pending" || (verdict == "commented" && strings.TrimSpace(StripSendMarker(r.Body)) == "") {
```

Update `forge.go`'s package doc: "gg's window onto a code forge's pull requests: `Provider` reads (never mutates); the optional `Writer` posts, reached only through `engine.SendToForge`."

- [ ] **Step 5: Extend the fake gh and `forgetest`**

In `fakegh/main.go`, before the existing `api graphql` case, dispatch `--input`:

```go
	case len(a) >= 4 && a[0] == "api" && a[1] == "graphql" && a[2] == "--input":
		os.Exit(mutation(dir, a[3]))
```

```go
// mutation answers one write: it appends {"op","variables"} to writes.jsonl
// (what tests assert on), fails with fail-<Op>'s text on stderr when that
// file exists, else prints mutation-<Op>.json or a built-in answer.
func mutation(dir, input string) int {
	b, err := os.ReadFile(input)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakegh:", err)
		return 1
	}
	var doc struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		fmt.Fprintln(os.Stderr, "fakegh:", err)
		return 1
	}
	op := strings.TrimPrefix(doc.Query, "mutation ")
	if i := strings.IndexAny(op, "({ "); i >= 0 {
		op = op[:i]
	}
	_ = os.MkdirAll(dir, 0o755)
	failMsg, failErr := os.ReadFile(filepath.Join(dir, "fail-"+op))
	line, _ := json.Marshal(map[string]any{"op": op, "variables": doc.Variables, "failed": failErr == nil})
	if f, err := os.OpenFile(filepath.Join(dir, "writes.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
		f.Write(append(line, '\n'))
		f.Close()
	}
	if failErr == nil {
		fmt.Fprintln(os.Stderr, strings.TrimSpace(string(failMsg)))
		return 1
	}
	if out, err := os.ReadFile(filepath.Join(dir, "mutation-"+op+".json")); err == nil {
		os.Stdout.Write(out)
		return 0
	}
	k := countWrites(dir, op)
	answers := map[string]string{
		"StartReview":  `{"data":{"addPullRequestReview":{"pullRequestReview":{"id":"PRR_new"}}}}`,
		"AddThread":    fmt.Sprintf(`{"data":{"addPullRequestReviewThread":{"thread":{"id":"PRRT_new%d","comments":{"nodes":[{"id":"PRRC_new%d","url":"https://github.com/o/r/pull/7#discussion_new%d"}]}}}}}`, k, k, k),
		"Reply":        fmt.Sprintf(`{"data":{"addPullRequestReviewThreadReply":{"comment":{"id":"PRRC_reply%d","url":"https://github.com/o/r/pull/7#reply%d"}}}}`, k, k),
		"SubmitReview": `{"data":{"submitPullRequestReview":{"pullRequestReview":{"id":"PRR_new","state":"COMMENTED"}}}}`,
		"DeleteReview": `{"data":{"deletePullRequestReview":{"pullRequestReview":{"id":"PRR_new"}}}}`,
		"Resolve":      `{"data":{"resolveReviewThread":{"thread":{"id":"x","isResolved":true}}}}`,
		"Unresolve":    `{"data":{"unresolveReviewThread":{"thread":{"id":"x","isResolved":false}}}}`,
	}
	out, ok := answers[op]
	if !ok {
		fmt.Fprintln(os.Stderr, "fakegh: unsupported mutation:", op)
		return 2
	}
	fmt.Print(out)
	return 0
}

// shownWrite reports a recorded, successful SubmitReview / Reply / Resolve /
// Unresolve.
func shownWrite(dir string) bool {
	b, _ := os.ReadFile(filepath.Join(dir, "writes.jsonl"))
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var w struct {
			Op     string `json:"op"`
			Failed bool   `json:"failed"`
		}
		if json.Unmarshal([]byte(line), &w) == nil && !w.Failed {
			switch w.Op {
			case "SubmitReview", "Reply", "Resolve", "Unresolve":
				return true
			}
		}
	}
	return false
}

// countWrites is how many times op has been recorded (this call included):
// the k in a created id, so two threads get two ids.
func countWrites(dir, op string) int {
	b, _ := os.ReadFile(filepath.Join(dir, "writes.jsonl"))
	return strings.Count(string(b), `"op":"`+op+`"`)
}
```

And in the snapshot branch: when `writes.jsonl` records a `SubmitReview` (or a `Reply`/`Resolve`/`Unresolve`) and `snapshot-<n>-sent.json` exists, serve that file — the PR as GitHub shows it after the send:

```go
	if strings.HasPrefix(name, "snapshot-") {
		// Only a SUCCESSFUL write GitHub would show — a submitted review, a
		// reply, a resolve — switches to the sent snapshot; a failed send
		// (StartReview … DeleteReview) shows nothing.
		if shownWrite(dir) {
			sent := strings.TrimSuffix(name, ".json") + "-sent.json"
			if b2, err2 := os.ReadFile(filepath.Join(dir, sent)); err2 == nil {
				os.Stdout.Write(b2)
				return
			}
		}
	}
```

`forgetest.go`:

```go
// Write is one mutation the fake gh recorded.
type Write struct {
	Op     string         `json:"op"`
	Vars   map[string]any `json:"variables"`
	Failed bool           `json:"failed"` // a fail-<Op> file made it fail
}

// Writes reads the fake gh's writes.jsonl in dir (none = empty).
func Writes(t testing.TB, dir string) []Write {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "writes.jsonl"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []Write
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var w Write
		if err := json.Unmarshal([]byte(line), &w); err != nil {
			t.Fatalf("writes.jsonl: %v", err)
		}
		out = append(out, w)
	}
	return out
}
```

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/forge/... -count=1`
Expected: PASS (all forge tests, the old ones included).

- [ ] **Step 7: Commit**

```bash
gg add internal/forge
git commit -m "feat(forge): Writer over gh api graphql --input, the send marker, a recording fake gh"
```

---

### Task 2: model — send stamps and sync state; notes store — replies rooted on GitHub, atomic edit

**Files:**
- Modify: `internal/model/note.go` (`NoteSend`, `RemarkSend`, `Note.Send`, `Note.RemarkSends`, `SyncState`, `IsForgeReply`)
- Modify: `internal/notes/part_file.go` (`dropOrphanReplies`, `capOldestFirst`, `edit`)
- Modify: `internal/notes/file_store.go` (`Edit`)
- Modify: `internal/notes/store.go` (`Store.Edit`)
- Test: `internal/model/note_test.go`, `internal/notes/forge_reply_test.go` (new)

**Interfaces:**
- Consumes: —
- Produces:
  ```go
  // model
  type NoteSend struct {
      PR      int       `toml:"pr"`
      Review  string    `toml:"review,omitempty"`  // the forge review it rides in (pending until submitted)
      Thread  string    `toml:"thread,omitempty"`
      Comment string    `toml:"comment,omitempty"`
      URL     string    `toml:"url,omitempty"`
      At      time.Time `toml:"at"`
      Err     string    `toml:"err,omitempty"`     // the last attempt failed: no ids, just this
  }
  type RemarkSend struct {
      RemarkFP string   `toml:"remark_fp"`
      Send     NoteSend `toml:"send"`
      Moved    bool     `toml:"moved,omitempty"`   // GitHub has it: hidden locally
  }
  // Note gains:
  //   Send        *NoteSend    `toml:"send,omitempty"`
  //   RemarkSends []RemarkSend `toml:"remark_sends,omitempty"`
  type SyncState string
  const (SyncLocal SyncState = "local"; SyncSending SyncState = "sending"; SyncFailed SyncState = "failed"; SyncForge SyncState = "github")
  func (s *NoteSend) State() SyncState            // nil → local; Err → failed; else sending
  func (n Note) IsForgeReply() bool               // a local reply whose root is a GitHub thread
  func (n Note) RemarkSend(fp string) (RemarkSend, bool)
  // notes
  // Store gains: Edit(id string, fn func(*model.Note) error) error — load+edit+save of ONE record under its part's lock; ErrNotFound when no part has it.
  ```

- [ ] **Step 1: Write the failing tests**

`internal/model/note_test.go` (append):

```go
func TestNoteSendState(t *testing.T) {
	t.Parallel()
	var none *NoteSend
	if none.State() != SyncLocal {
		t.Errorf("nil send = %q", none.State())
	}
	if s := (&NoteSend{PR: 7, Review: "R"}); s.State() != SyncSending {
		t.Errorf("stamped = %q", s.State())
	}
	if s := (&NoteSend{PR: 7, Err: "403"}); s.State() != SyncFailed {
		t.Errorf("failed = %q", s.State())
	}
	if !(Note{ParentID: ForgeNoteIDPrefix + "PRRC_1"}).IsForgeReply() || (Note{ParentID: "n1"}).IsForgeReply() {
		t.Error("IsForgeReply")
	}
	n := Note{RemarkSends: []RemarkSend{{RemarkFP: "fp1", Moved: true}}}
	if rs, ok := n.RemarkSend("fp1"); !ok || !rs.Moved {
		t.Errorf("RemarkSend = %+v, %v", rs, ok)
	}
}
```

`internal/notes/forge_reply_test.go`:

```go
package notes

import (
	"errors"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/model"
)

// forgeReply is a local draft answer to GitHub thread root PRRC_1.
func forgeReply(id string, sec int) model.Note {
	n := commitNote(id, "", sec)
	n.ParentID = model.ForgeNoteIDPrefix + "PRRC_1"
	return n
}

func TestForgeRootedReplySurvivesMutations(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir()) // uncapped: only the orphan prune is under test
	for _, n := range []model.Note{forgeReply("r0000001", 1), commitNote("c0000001", "", 2), commitNote("c0000002", "", 3)} {
		if err := fs.Put(n); err != nil {
			t.Fatal(err)
		}
	}
	if err := fs.Remove("c0000001"); err != nil { // another note's removal runs the orphan prune
		t.Fatal(err)
	}
	all, err := fs.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range all {
		found = found || n.ID == "r0000001"
	}
	if !found {
		t.Fatalf("the draft reply to a GitHub thread was pruned: %+v", all)
	}
}

func TestForgeRootedReplyCountsAsItsOwnThreadUnderTheCap(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	fs.SetPolicy(Policy{MaxEntries: 2})
	for _, n := range []model.Note{forgeReply("r0000001", 1), commitNote("c0000001", "", 2), commitNote("c0000002", "", 3)} {
		if err := fs.Put(n); err != nil {
			t.Fatal(err)
		}
	}
	all, _ := fs.LoadAll()
	if len(all) != 2 {
		t.Fatalf("cap kept %d records, want 2", len(all))
	}
	for _, n := range all {
		if n.ID == "r0000001" {
			t.Fatalf("the OLDEST thread (the draft reply) should have gone first: %+v", all)
		}
	}
}

func TestEditIsAtomicPerRecord(t *testing.T) {
	t.Parallel()
	fs := NewFileStore(t.TempDir())
	if err := fs.Put(commitNote("c0000001", "", 1)); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	if err := fs.Edit("c0000001", func(n *model.Note) error {
		n.Send = &model.NoteSend{PR: 7, Review: "PRR_1", At: at}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	all, _ := fs.LoadAll()
	if len(all) != 1 || all[0].Send == nil || all[0].Send.Review != "PRR_1" || !all[0].Send.At.Equal(at) {
		t.Fatalf("after Edit: %+v", all)
	}
	if err := fs.Edit("nope", func(*model.Note) error { return nil }); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Edit of a missing id = %v", err)
	}
	boom := errors.New("boom")
	if err := fs.Edit("c0000001", func(n *model.Note) error { n.Summary = "x"; return boom }); !errors.Is(err, boom) {
		t.Fatalf("Edit's own error = %v", err)
	}
	if all, _ := fs.LoadAll(); all[0].Summary == "x" {
		t.Fatal("a failed edit was written")
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/model/ ./internal/notes/ -run 'NoteSendState|ForgeRooted|EditIsAtomic' -count=1`
Expected: FAIL — `undefined: NoteSend`, `fs.Edit undefined`.

- [ ] **Step 3: Implement the model**

In `internal/model/note.go`, add to `Note` (after `RemarkSummary`):

```go
	// Send is this note's trip to a forge: stamped with the forge's ids while
	// it is being sent (a crash leaves them for the next refresh to settle),
	// or holding the last attempt's error. nil = never sent.
	Send *NoteSend `toml:"send,omitempty"`
	// RemarkSends is, on a review note, each remark that has gone (or is
	// going) to a forge, keyed by remark fingerprint so a re-saved review
	// keeps them.
	RemarkSends []RemarkSend `toml:"remark_sends,omitempty"`
```

and the types:

```go
// NoteSend is one note's (or one review remark's) send to a forge.
type NoteSend struct {
	PR      int       `toml:"pr"`
	Review  string    `toml:"review,omitempty"`
	Thread  string    `toml:"thread,omitempty"`
	Comment string    `toml:"comment,omitempty"`
	URL     string    `toml:"url,omitempty"`
	At      time.Time `toml:"at"`
	Err     string    `toml:"err,omitempty"`
}

// RemarkSend is one remark of a stored review on its way to a forge;
// Moved: the forge has it and the remark is hidden locally.
type RemarkSend struct {
	RemarkFP string   `toml:"remark_fp"`
	Send     NoteSend `toml:"send"`
	Moved    bool     `toml:"moved,omitempty"`
}

// SyncState is where a note lives (spec 2026-10-07 §1.1); English protocol
// values (CLI --json, MCP).
type SyncState string

const (
	SyncLocal   SyncState = "local"
	SyncSending SyncState = "sending"
	SyncFailed  SyncState = "failed"
	SyncForge   SyncState = "github"
)

// State is the sync state a send stamp gives its note.
func (s *NoteSend) State() SyncState {
	switch {
	case s == nil:
		return SyncLocal
	case s.Err != "":
		return SyncFailed
	}
	return SyncSending
}

// IsForgeReply reports a stored reply whose thread root is a forge comment:
// a local draft answer to a GitHub thread.
func (n Note) IsForgeReply() bool { return n.IsReply() && IsForgeNoteID(n.ParentID) }

// RemarkSend is the send entry of the remark with fingerprint fp.
func (n Note) RemarkSend(fp string) (RemarkSend, bool) {
	for _, r := range n.RemarkSends {
		if r.RemarkFP == fp {
			return r, true
		}
	}
	return RemarkSend{}, false
}
```

- [ ] **Step 4: Implement the store**

`part_file.go` — a reply rooted on the forge is a thread of its own for the prune and the cap:

```go
func dropOrphanReplies(ns []model.Note) []model.Note {
	roots := make(map[string]bool, len(ns))
	for _, n := range ns {
		if !n.IsReply() {
			roots[n.ID] = true
		}
	}
	kept := make([]model.Note, 0, len(ns))
	for _, n := range ns {
		// A draft answer to a GitHub thread has its root on the forge, never
		// in the store: it is not an orphan.
		if n.IsReply() && !n.IsForgeReply() && !roots[n.StoredParent()] {
			continue
		}
		kept = append(kept, n)
	}
	return kept
}
```

In `capOldestFirst`, the `roots` collection becomes `if (!n.IsReply() || n.IsForgeReply()) && !exempt[n.ID]` (a forge-rooted reply is dropped as its own thread; nothing hangs off it).

`edit` on `partFile` and `Edit` on `FileStore`:

```go
// edit rewrites the one record id under the part's lock; ErrNotFound when
// this part does not hold it. fn's error aborts with nothing written.
func (fs *partFile) edit(id string, fn func(*model.Note) error) error {
	_, err := fs.mutate(func(ns []model.Note) ([]model.Note, error) {
		for i := range ns {
			if ns[i].ID == id {
				if err := fn(&ns[i]); err != nil {
					return nil, err
				}
				return ns, nil
			}
		}
		return nil, ErrNotFound
	})
	return err
}
```

```go
// Edit loads, edits and saves ONE record wherever it is stored, under its
// part's lock — the read-modify-write a send's stamps need (two gg
// processes may stamp one note). ErrNotFound when no part holds id.
func (fs *FileStore) Edit(id string, fn func(*model.Note) error) error {
	parts, err := fs.Parts()
	if err != nil {
		return err
	}
	for _, p := range parts {
		if err := fs.file(p).edit(id, fn); !errors.Is(err, ErrNotFound) {
			return err
		}
	}
	return ErrNotFound
}
```

Add `Edit(id string, fn func(*model.Note) error) error` to the `Store` interface with a one-line doc. (`mutate`'s clone makes the record `fn` edits a copy; a slice field like `RemarkSends` must be re-sliced, never appended in place — `fn` callers in Task 5 build a new slice.)

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/model/ ./internal/notes/ ./internal/domain/ -count=1`
Expected: PASS (domain included: its store wrappers embed `notes.Store`).

- [ ] **Step 6: Commit**

```bash
gg add internal/model internal/notes
git commit -m "feat(notes): send stamps, sync state, draft replies rooted on GitHub, atomic Edit"
```

---

### Task 3: domain read side — draft replies under GitHub threads, sync state and group on every note, moved remarks hidden

**Files:**
- Modify: `internal/domain/notes.go` (`ResolvedNote` gains `Sync`, `SendErr`, `Group`, `Origin`; `NoteReply` branches on a forge parent; `resolveNotes` fills sync + group)
- Create: `internal/domain/forge_drafts.go` (`forgeReplyDraft`, `forgeDrafts`, `syncOf`, group constants)
- Modify: `internal/domain/forge_notes.go` (`forgeNotesFor(ctx, …)` attaches drafts; `forgeNote` strips the marker and sets `Sync`/`Group`; `ErrReadOnlyNote` doc)
- Modify: `internal/domain/previewnotes.go` (`loadPreviewNotes` skips forge-rooted replies; callers pass ctx to `forgeNotesFor`/`forgeNoteCounts`)
- Modify: `internal/domain/review_notes.go` (`Review.RemarkSends`; `reviewOf` copies it; `putReview` keeps `Send` + `RemarkSends` on a re-save)
- Modify: `internal/domain/review_doc.go` (`reviewDocNotes`, `reviewSplit` skip moved remarks; remark sync + group)
- Modify: `internal/domain/notewire.go` (`sync`, `send_error`, `group`, `origin`)
- Test: `internal/domain/forge_drafts_test.go` (new), `internal/domain/review_moved_test.go` (new)

**Interfaces:**
- Consumes: `model.NoteSend.State`, `model.Note.IsForgeReply`, `notes.Store.Edit` (Task 2); `forge.StripSendMarker` (Task 1).
- Produces:
  ```go
  // ResolvedNote gains:
  //   Sync    model.SyncState // local / sending / failed / github
  //   SendErr string          // the last send's error (Sync == failed)
  //   Group   string          // GroupMine, "review:<id>", "github:<review id>"
  //   Origin  string          // a carried note's home: a short sha or "working tree" (Task 8)
  const GroupMine = "mine"
  func syncOf(n model.Note) (model.SyncState, string)
  func (r Review) remarkMoved(fp string) bool
  // WireNote gains: Sync string `json:"sync,omitempty"`, SendErr `json:"send_error,omitempty"`,
  //   Group `json:"group,omitempty"`, Origin `json:"origin,omitempty"`
  // NoteReply("forge:<comment id>", n) stores a draft reply: ParentID = "forge:<thread ROOT comment id>",
  //   Address = the PR head commit + the thread's path, Side/Range = the thread's.
  ```

- [ ] **Step 1: Write the failing tests**

`internal/domain/forge_drafts_test.go`:

```go
package domain

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/forge"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// prThreadSvc: the prPreviewRepo PR (#7, a.go changed) with one GitHub
// thread on a.go:3 (root PRRC_c1, reply PRRC_c2) cached, and a note store.
func prThreadSvc(t *testing.T) (*Service, PreviewNoteSet, string) {
	t.Helper()
	dir, head := prPreviewRepo(t)
	_, svc := newRealRepoAt(t, dir)
	svc.UseNotesDir(t.TempDir())
	at := time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC)
	ff := &fakeForge{
		byNum: map[int]model.PullRequest{7: {Number: 7, State: "open", Target: "main", HeadSHA: head}},
		comments: []model.ForgeComment{
			{ID: "PRRC_c1", Kind: model.ForgeCommentInline, Author: "bob", Body: "why?\n\n" + forge.SendMarker("zz"), Path: "a.go",
				Side: model.NoteSideNew, Line: 3, StartLine: 3, ThreadID: "PRRT_t1", ReviewID: "PRR_r1", Created: at},
			{ID: "PRRC_c2", ParentID: "PRRC_c1", Kind: model.ForgeCommentInline, Author: "ann", Body: "because", Path: "a.go",
				Side: model.NoteSideNew, Line: 3, StartLine: 3, ThreadID: "PRRT_t1", ReviewID: "PRR_r2", Created: at},
		},
	}
	svc.SetForgeProviders([]forge.Provider{ff})
	ctx := context.Background()
	if _, err := svc.PRCommentsRefresh(ctx, 7); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PullRequest(ctx, 7); err != nil {
		t.Fatal(err)
	}
	set, err := svc.PreviewNotes(ctx, git.PRRef(7), "main")
	if err != nil || !set.OK() {
		t.Fatalf("set = %+v, %v", set, err)
	}
	return svc, set, head
}

func TestReplyToAGitHubThreadIsALocalDraftUnderIt(t *testing.T) {
	t.Parallel()
	svc, set, head := prThreadSvc(t)
	ctx := context.Background()
	// Answering the REPLY still hangs the draft off the thread's root.
	d, err := svc.NoteReply(ctx, model.ForgeNoteIDPrefix+"PRRC_c2", model.Note{Source: model.NoteSourceUser, Summary: "on it"})
	if err != nil {
		t.Fatal(err)
	}
	if d.ParentID != model.ForgeNoteIDPrefix+"PRRC_c1" || d.Address.Commit != head || d.Address.Path != "a.go" || d.Range != [2]int{3, 3} {
		t.Fatalf("draft = %+v", d)
	}
	got, err := svc.PreviewNotesAt(ctx, set, "a.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("roots = %d, want the one GitHub thread (the draft is not a root)", len(got))
	}
	root := got[0]
	if root.Sync != model.SyncForge || root.Group != "github:PRR_r1" {
		t.Errorf("root sync/group = %q/%q", root.Sync, root.Group)
	}
	if strings.Contains(root.Note.Summary+root.Note.Rationale, "gg:") {
		t.Errorf("the send marker shows: %q / %q", root.Note.Summary, root.Note.Rationale)
	}
	last := root.Replies[len(root.Replies)-1]
	if last.Note.ID != d.ID || last.Sync != model.SyncLocal || last.Range != [2]int{3, 3} {
		t.Fatalf("draft reply = %+v (want local, last, at the thread's line)", last)
	}
	// It is a stored note: removable like any other.
	if err := svc.NoteRemove(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
}

func TestReplyToAnUnknownForgeCommentFails(t *testing.T) {
	t.Parallel()
	svc, _, _ := prThreadSvc(t)
	if _, err := svc.NoteReply(context.Background(), model.ForgeNoteIDPrefix+"PRRC_nope",
		model.Note{Summary: "x"}); err == nil {
		t.Fatal("a reply to a comment gg has not read must fail")
	}
}

func TestStoredNoteCarriesItsSyncState(t *testing.T) {
	t.Parallel()
	svc, set, head := prThreadSvc(t)
	ctx := context.Background()
	n, err := svc.NoteAdd(ctx, model.Note{Source: model.NoteSourceUser, Summary: "local",
		Address: model.FileAddress{State: model.StateCommitted, Commit: head, Path: "a.go"},
		Side:    model.NoteSideNew, Range: [2]int{1, 1}})
	if err != nil {
		t.Fatal(err)
	}
	st := svc.notesStore(ctx)
	if err := st.Edit(n.ID, func(x *model.Note) error {
		x.Send = &model.NoteSend{PR: 7, Err: "HTTP 403"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	svc.invalidateNoteCounts()
	got, _ := svc.PreviewNotesAt(ctx, set, "a.go")
	for _, r := range got {
		if r.Note.ID == n.ID {
			if r.Sync != model.SyncFailed || r.SendErr != "HTTP 403" || r.Group != GroupMine {
				t.Fatalf("stored note = sync %q err %q group %q", r.Sync, r.SendErr, r.Group)
			}
			w := ToWireNote(r)
			if w.Sync != "failed" || w.SendErr != "HTTP 403" || w.Group != GroupMine {
				t.Fatalf("wire = %+v", w)
			}
			return
		}
	}
	t.Fatal("the stored note is not in the PR's notes")
}
```

(`newRealRepoAt(t, dir)` is a small helper next to `newRealRepo`: a `Service` over an existing dir. Add it to `compare_test.go` if no equivalent exists — check `prpreview_test.go`'s `countingService` first and reuse it when it fits.)

`internal/domain/review_moved_test.go`:

```go
package domain

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestMovedRemarkStaysHiddenAcrossAReSave(t *testing.T) {
	t.Parallel()
	svc, r := structuredReview(t) // remark 0 = g.txt:2 "line two"
	ctx := context.Background()
	fps := r.remarkFPs()
	st := svc.notesStore(ctx)
	if err := st.Edit(r.ID, func(n *model.Note) error {
		n.RemarkSends = []model.RemarkSend{{RemarkFP: fps[0], Moved: true,
			Send: model.NoteSend{PR: 7, Thread: "PRRT_x", URL: "https://x"}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	svc.invalidateNoteCounts()
	d := diffOfCommitFile(t, svc, r.Commit, "g.txt")
	if got, _ := svc.ReviewNotesFor(ctx, r.ID, "g.txt", d); len(got) != 0 {
		t.Fatalf("a moved remark still shows: %+v", got)
	}
	// Re-save the same review in place (an import, a retry): still hidden.
	if _, _, err := svc.SaveReview(ctx, SaveReview{Target: ReviewTarget{Kind: ReviewRange, Range: r.Commit + "^.." + r.Commit, Label: "g"},
		Agent: "Claude", Text: r.Text, NoteID: r.ID}); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.ReviewNotesFor(ctx, r.ID, "g.txt", d); len(got) != 0 {
		t.Fatalf("the re-save brought the moved remark back: %+v", got)
	}
	r2, _ := svc.Review(ctx, r.ID)
	if len(r2.RemarkSends) != 1 || !r2.RemarkSends[0].Moved {
		t.Fatalf("RemarkSends after the re-save = %+v", r2.RemarkSends)
	}
}
```

(`diffOfCommitFile` = the `Diff` of `<commit>^..<commit>` for a path, as `review_doc_test.go` already builds for `ReviewNotesFor`; reuse that test's construction verbatim.)

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/domain/ -run 'GitHubThreadIsALocalDraft|UnknownForgeComment|CarriesItsSyncState|MovedRemarkStaysHidden' -count=1`
Expected: FAIL — `r.Sync undefined`, `r.RemarkSends undefined`, and `NoteReply` returns `ErrReadOnlyNote` for the forge parent.

- [ ] **Step 3: Implement `forge_drafts.go`**

```go
package domain

import (
	"context"
	"errors"
	"strings"

	"github.com/homeend/gigagit/internal/markdown"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notes"
)

// GroupMine is the implicit group of a PR's hand-written local notes
// ("my draft review", spec §1.3). A review's group is "review:<id>", a
// GitHub review's "github:<review id>".
const GroupMine = "mine"

// ErrUnknownForgeComment: a reply names a forge comment no cached PR holds.
var ErrUnknownForgeComment = errors.New("that GitHub comment is not in any pull request gg has read (open the PR first)")

// syncOf is a stored note's sync state and its last send error.
func syncOf(n model.Note) (model.SyncState, string) {
	st := n.Send.State()
	if st == model.SyncFailed {
		return st, n.Send.Err
	}
	return st, ""
}

// forgeCommentByID finds a cached forge comment and its thread's root (the
// thread's comment with no parent) across every PR read this session.
func (s *Service) forgeCommentByID(id string) (pr int, root model.ForgeComment, ok bool) {
	s.forgeMu.Lock()
	defer s.forgeMu.Unlock()
	for n, e := range s.forgeComments {
		for _, bucket := range [][]model.ForgeComment{e.c.Inline, e.c.Outdated} {
			var hit *model.ForgeComment
			for i := range bucket {
				if bucket[i].ID == id {
					hit = &bucket[i]
				}
			}
			if hit == nil {
				continue
			}
			for _, c := range bucket {
				if c.ThreadID == hit.ThreadID && c.ParentID == "" {
					return n, c, true
				}
			}
			return n, *hit, true
		}
	}
	return 0, model.ForgeComment{}, false
}

// forgeReplyDraft stores n as a local draft answer to the GitHub thread
// holding comment parentID: rooted on the thread's FIRST comment, addressed
// at the PR's head and the thread's path and lines, so it is drawn and sent
// with that thread.
func (s *Service) forgeReplyDraft(ctx context.Context, parentID string, n model.Note) (model.Note, error) {
	pr, root, ok := s.forgeCommentByID(strings.TrimPrefix(parentID, model.ForgeNoteIDPrefix))
	if !ok {
		return model.Note{}, ErrUnknownForgeComment
	}
	p, _, ok := s.PRDetailsCached(pr)
	if !ok || p.HeadSHA == "" {
		return model.Note{}, ErrUnknownForgeComment
	}
	st := s.notesStore(ctx)
	if st == nil {
		return model.Note{}, ErrNotesDisabled
	}
	all, err := st.LoadAll()
	if err != nil {
		return model.Note{}, err
	}
	rn := forgeNote(root, p.HeadSHA)
	now := notes.Now().UTC()
	n.ID = notes.NewID(all)
	n.ParentID = model.ForgeNoteIDPrefix + root.ID
	n.Address = model.FileAddress{State: model.StateCommitted, Commit: p.HeadSHA, Path: root.Path}
	n.Side, n.Range = rn.Note.Side, rn.Note.Range
	if n.Source == "" {
		n.Source = model.NoteSourceUser
	}
	n.Created, n.Updated = now, now
	if err := st.Put(n); err != nil {
		return model.Note{}, err
	}
	s.invalidateNoteCounts()
	return n, nil
}

// forgeDrafts is every stored draft reply, by the forge root comment id it
// answers, in creation order.
func (s *Service) forgeDrafts(ctx context.Context) map[string][]model.Note {
	st := s.notesStore(ctx)
	if st == nil {
		return nil
	}
	all, err := st.Load(notes.PartCommits)
	if err != nil {
		return nil
	}
	out := map[string][]model.Note{}
	for _, n := range all {
		if n.IsForgeReply() {
			id := strings.TrimPrefix(n.ParentID, model.ForgeNoteIDPrefix)
			out[id] = append(out[id], n)
		}
	}
	return out
}

var _ = markdown.Summary // forgeNote's split; keeps the import honest if unused here
```

(Drop the last line if `markdown` ends up unused in this file.)

- [ ] **Step 4: Wire it in**

- `NoteReply`: before the `IsReadOnlyNoteID` refusal, `if model.IsForgeNoteID(parentID) { return s.forgeReplyDraft(ctx, parentID, n) }` (after the `NoteLink` normalisation, like the remark branch).
- `ErrReadOnlyNote`'s doc: "every note mutation but a REPLY handed the id of a note built at read time: a forge comment (edited and deleted only on GitHub) or a review document's note (the review is the one stored note)".
- `forgeNotesFor(ctx context.Context, set PreviewNoteSet, path string)`: after the roots are built, append each root's drafts (`s.forgeDrafts(ctx)[strings.TrimPrefix(root.Note.ID, model.ForgeNoteIDPrefix)]`) as `ResolvedNote{Note: d, Status: model.NoteActive, Range: root.Range, Sync: …syncOf(d), Group: GroupMine}`. Load the drafts once per call (only when `inline` is non-empty). Update the four callers (`PreviewNotesFor`, `PreviewNotesAt`, `PreviewNotesAll`, `forgeNoteCounts`) to pass ctx — `forgeNoteCounts` counts roots, so drafts do not change a badge.
- `forgeNote`: `body := forge.StripSendMarker(c.Body)` before `markdown.Summary(body)`; set `Sync: model.SyncForge, Group: "github:" + c.ReviewID` (Group "github" when `ReviewID` is empty).
- `loadPreviewNotes`: `if n.IsForgeReply() { continue } // drawn under its GitHub thread (forgeNotesFor)`.
- `resolveNotes` (and `NotesAt`'s resolved build): every `ResolvedNote` built from a stored note gets `Sync, SendErr = syncOf(n)` and `Group: GroupMine` (a review note keeps `Group: "review:" + n.ID`).
- `Review` gains `RemarkSends []model.RemarkSend`; `reviewOf` copies `n.RemarkSends` in both branches; add

```go
// remarkMoved reports a remark the forge now owns (hidden locally).
func (r Review) remarkMoved(fp string) bool {
	for _, x := range r.RemarkSends {
		if x.RemarkFP == fp && x.Moved {
			return true
		}
	}
	return false
}

// remarkSend is the send entry of remark fp (sync state of a remark row).
func (r Review) remarkSend(fp string) *model.NoteSend {
	for _, x := range r.RemarkSends {
		if x.RemarkFP == fp && !x.Moved {
			s := x.Send
			return &s
		}
	}
	return nil
}
```

- `reviewDocNotes` and `reviewSplit`: compute `fp := remarkFP(f.Path, side, dn.Range, dn.Summary)` per remark; `continue` (after `i++`, so indices stay the document's) when `r.remarkMoved(fp)`; a shown remark's `ResolvedNote` gets `Group: "review:" + r.ID` and `Sync, SendErr` from `r.remarkSend(fp).State()` / `.Err`.
- `ReviewHead.Remarks` (the row tally): subtract the moved remarks where the head is built.
- `putReview`: in the `for _, old := range all` loop also carry `n.Send, n.RemarkSends = old.Send, old.RemarkSends`.
- `WireNote`: the four fields; `ToWireNote` fills them from the `ResolvedNote` (`string(r.Sync)`).

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/domain/ -count=1`
Expected: PASS (the whole package: `forgeNotesFor`'s signature change touches every PR-note test).

- [ ] **Step 6: Commit**

```bash
gg add internal/domain
git commit -m "feat(notes): draft replies under GitHub threads, sync state and group on every note, moved remarks hidden"
```

---

### Task 4: engine — `SendToForge`: the confirm, the write order, all-or-nothing

**Files:**
- Create: `internal/engine/send_forge.go`
- Create: `internal/engine/send_forge_test.go`
- Modify: `internal/tui/source.go` (`opAffectedSources`: `engine.SendToForge` → `[]sourceKey{}`)
- Modify: `internal/tui/i18n_display.go` (`optionDisplayName`: `comment`, `approve`, `request-changes`, `send`, `submit-with-pending`, `discard` where missing)
- Modify: `internal/i18n/lang/{ja,ko,zh,ru}.toml` (the prompt formats, summaries and option labels)

**Interfaces:**
- Consumes: `forge.Writer`, `forge.Thread`, `forge.Event`, `forge.IsBlankBodyError` (Task 1); `model.NoteSend` (Task 2).
- Produces:
  ```go
  type SendKind int
  const (SendThread SendKind = iota; SendReply; SendResolve; SendUnresolve)
  type SendReplyBody struct{ Key, Body string }
  type SendItem struct {
      Key      string          // the local item ("" = none, e.g. a resolve)
      Label    string          // one confirm line: "a.go:12 rename this"
      Kind     SendKind
      Thread   forge.Thread    // SendThread
      Replies  []SendReplyBody // SendThread: local replies, posted into the new thread in order
      Resolve  bool            // SendThread: resolve once submitted
      ThreadID string          // SendReply / SendResolve / SendUnresolve
      Body     string          // SendReply
  }
  type SendSkip struct{ Label, Reason string }
  type SendMode int
  const (SendReview SendMode = iota; SendActions; SendFinish; SendDiscard)
  type SendPlan struct {
      Target  string // "owner/repo #7"
      PR      int
      PRID    string // the forge's node id
      Head    string // the head the threads anchor on
      Mode    SendMode
      Verdict bool   // SendReview: the user picks comment/approve/request-changes (false: send/abort, COMMENT)
      Key     string // SendReview of a whole stored review: the review note (stamped too)
      Body    string // the review body (SendReview, SendFinish)
      OwnPR   bool   // the viewer opened the PR: no approve / request-changes
      Pending string // the viewer's pending review on the forge ("" = none)
      Items   []SendItem
      Skipped []SendSkip
  }
  type SendLedger interface {
      Stamp(ctx context.Context, key string, s model.NoteSend) error // ◌: ids known so far
      Fail(ctx context.Context, keys []string, err error)            // ○!: the error, no ids
      Settle(ctx context.Context) error                              // re-read the PR, settle every stamp
  }
  type SendToForge struct {
      Plan   SendPlan
      Writer forge.Writer
      Ledger SendLedger
      Now    func() time.Time // stamp clock; nil = time.Now (domain passes clock.Now)
  }
  // Plan is a VALUE (R12): domain computed it outside the gate just before.
  const DecisionSendForge = "forge.send"
  // Options: OptComment "comment", OptApprove "approve", OptRequestChanges "request-changes",
  //          OptSend "send", OptSubmitWithPending "submit-with-pending", OptDiscard "discard", "abort".
  ```

- [ ] **Step 1: Write the failing tests** (`internal/engine/send_forge_test.go`)

```go
package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/forge"
	"github.com/homeend/gigagit/internal/model"
)

// fakeWriter records every call as "Op arg…" and fails the ops in fail.
type fakeWriter struct {
	mu    sync.Mutex
	calls []string
	fail  map[string]error
	n     int
}

func (w *fakeWriter) rec(op string, args ...any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	parts := []string{op} // fmt.Sprint puts no space between string operands
	for _, a := range args {
		parts = append(parts, fmt.Sprint(a))
	}
	w.calls = append(w.calls, strings.Join(parts, " "))
	if err := w.fail[op]; err != nil {
		delete(w.fail, op) // fail once
		return err
	}
	return nil
}
func (w *fakeWriter) StartReview(_ context.Context, pr, commit string) (string, error) {
	return "R1", w.rec("StartReview", pr, commit)
}
func (w *fakeWriter) AddThread(_ context.Context, review string, t forge.Thread) (forge.ThreadRef, error) {
	w.n++
	id := fmt.Sprintf("T%d", w.n)
	return forge.ThreadRef{ID: id, CommentID: "C" + id, URL: "u/" + id}, w.rec("AddThread", review, t.Path, t.Line)
}
func (w *fakeWriter) Reply(_ context.Context, review, thread, body string) (forge.CommentRef, error) {
	return forge.CommentRef{ID: "RC" + thread}, w.rec("Reply", review, thread, body)
}
func (w *fakeWriter) SubmitReview(_ context.Context, review string, ev forge.Event, body string) error {
	return w.rec("SubmitReview", review, ev, body)
}
func (w *fakeWriter) DeletePendingReview(_ context.Context, review string) error {
	return w.rec("DeletePendingReview", review)
}
func (w *fakeWriter) Resolve(_ context.Context, th string) error   { return w.rec("Resolve", th) }
func (w *fakeWriter) Unresolve(_ context.Context, th string) error { return w.rec("Unresolve", th) }

type fakeLedger struct {
	stamps  map[string]model.NoteSend
	failed  map[string]string
	settled int
}

func newLedger() *fakeLedger {
	return &fakeLedger{stamps: map[string]model.NoteSend{}, failed: map[string]string{}}
}
func (l *fakeLedger) Stamp(_ context.Context, key string, s model.NoteSend) error {
	l.stamps[key] = s
	return nil
}
func (l *fakeLedger) Fail(_ context.Context, keys []string, err error) {
	for _, k := range keys {
		delete(l.stamps, k)
		l.failed[k] = err.Error()
	}
}
func (l *fakeLedger) Settle(context.Context) error { l.settled++; return nil }

func plan2() SendPlan {
	return SendPlan{Target: "o/r #7", PR: 7, PRID: "PR_7", Head: "h1", Mode: SendReview, Verdict: true, Body: "summary",
		Items: []SendItem{
			{Key: "n1", Label: "a.go:3 x", Kind: SendThread, Thread: forge.Thread{Path: "a.go", Line: 3, Body: "x"},
				Replies: []SendReplyBody{{Key: "n1r", Body: "and y"}}, Resolve: true},
			{Key: "n2", Label: "b.go (file) z", Kind: SendThread, Thread: forge.Thread{Path: "b.go", Body: "z"}},
		},
		Skipped: []SendSkip{{Label: "c.go:1 q", Reason: "not in this PR"}}}
}

func runSend(t *testing.T, p SendPlan, w *fakeWriter, l *fakeLedger, answer string) (Result, error, []DecisionRequest) {
	t.Helper()
	var asked []DecisionRequest
	dec := deciderFunc(func(_ context.Context, r DecisionRequest) (DecisionResponse, error) {
		asked = append(asked, r)
		return DecisionResponse{Option: answer}, nil
	})
	op := SendToForge{Plan: p, Writer: w, Ledger: l,
		Now: func() time.Time { return time.Unix(100, 0) }}
	res, err := op.Run(context.Background(), OpDeps{Decider: dec})
	return res, err, asked
}

func TestSendReviewWritesInOrderAndSettles(t *testing.T) {
	t.Parallel()
	w, l := &fakeWriter{}, newLedger()
	res, err, asked := runSend(t, plan2(), w, l, OptApprove)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"StartReview PR_7 h1", "AddThread R1 a.go 3", "Reply R1 T1 and y", "AddThread R1 b.go 0",
		"SubmitReview R1 APPROVE summary", "Resolve T1"}
	if !slices.Equal(w.calls, want) {
		t.Fatalf("calls =\n%s\nwant\n%s", strings.Join(w.calls, "\n"), strings.Join(want, "\n"))
	}
	if len(asked) != 1 || asked[0].ID != DecisionSendForge ||
		!slices.Equal(asked[0].Options, []string{OptComment, OptApprove, OptRequestChanges, "abort"}) {
		t.Fatalf("asked = %+v", asked)
	}
	for _, s := range []string{"o/r #7", "a.go:3 x", "b.go (file) z", "c.go:1 q", "not in this PR", "summary"} {
		if !strings.Contains(asked[0].Prompt, s) {
			t.Errorf("confirm lacks %q:\n%s", s, asked[0].Prompt)
		}
	}
	if l.stamps["n1"].Thread != "T1" || l.stamps["n1"].Review != "R1" || l.stamps["n1r"].Comment != "RCT1" || l.stamps["n2"].Thread != "T2" {
		t.Errorf("stamps = %+v", l.stamps)
	}
	if l.settled != 1 || !res.Changed {
		t.Errorf("settled %d, res %+v", l.settled, res)
	}
}

func TestSendReviewOnOwnPROffersNoVerdicts(t *testing.T) {
	t.Parallel()
	p := plan2()
	p.OwnPR = true
	_, _, asked := runSend(t, p, &fakeWriter{}, newLedger(), "abort")
	if !slices.Equal(asked[0].Options, []string{OptComment, "abort"}) {
		t.Fatalf("own PR options = %v", asked[0].Options)
	}
}

func TestSendAbortWritesNothing(t *testing.T) {
	t.Parallel()
	w, l := &fakeWriter{}, newLedger()
	res, err, _ := runSend(t, plan2(), w, l, "abort")
	if err != nil || res.Changed || len(w.calls) != 0 || len(l.stamps) != 0 || !strings.HasPrefix(res.Summary, "aborted") {
		t.Fatalf("res %+v err %v calls %v stamps %v", res, err, w.calls, l.stamps)
	}
}

func TestSendRefusesAnAnswerNotOffered(t *testing.T) {
	t.Parallel()
	p := plan2()
	p.OwnPR = true
	w := &fakeWriter{}
	if _, err, _ := runSend(t, p, w, newLedger(), OptApprove); err == nil || len(w.calls) != 0 {
		t.Fatalf("approve on own PR: err %v calls %v", err, w.calls)
	}
}

func TestSendFailureBeforeSubmitDeletesThePendingReview(t *testing.T) {
	t.Parallel()
	w := &fakeWriter{fail: map[string]error{"AddThread": nil, "SubmitReview": errors.New("HTTP 502")}}
	l := newLedger()
	_, err, _ := runSend(t, plan2(), w, l, OptComment)
	if err == nil || !strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("err = %v", err)
	}
	if w.calls[len(w.calls)-1] != "DeletePendingReview R1" {
		t.Fatalf("last call = %q, want the pending review deleted", w.calls[len(w.calls)-1])
	}
	for _, k := range []string{"n1", "n1r", "n2"} {
		if l.failed[k] != "SubmitReview: HTTP 502" && !strings.Contains(l.failed[k], "HTTP 502") {
			t.Errorf("%s failed = %q", k, l.failed[k])
		}
	}
	if len(l.stamps) != 0 || l.settled != 0 {
		t.Errorf("stamps %v settled %d", l.stamps, l.settled)
	}
}

func TestSendJoiningThePendingReviewNeverDeletesIt(t *testing.T) {
	t.Parallel()
	p := plan2()
	p.Pending = "MINE"
	w := &fakeWriter{fail: map[string]error{"SubmitReview": errors.New("HTTP 502")}}
	l := newLedger()
	_, err, asked := runSend(t, p, w, l, OptSubmitWithPending)
	if !slices.Equal(asked[0].Options, []string{OptSubmitWithPending, "abort"}) {
		t.Fatalf("pending options = %v", asked[0].Options)
	}
	if err == nil {
		t.Fatal("want the submit error")
	}
	for _, c := range w.calls {
		if strings.HasPrefix(c, "StartReview") || strings.HasPrefix(c, "DeletePendingReview") {
			t.Fatalf("joined review: %q must not run (calls %v)", c, w.calls)
		}
	}
	if l.stamps["n1"].Review != "MINE" || len(l.failed) != 0 {
		t.Fatalf("joined failure keeps the stamps for --finish: stamps %v failed %v", l.stamps, l.failed)
	}
}

func TestSendRetriesABlankBody(t *testing.T) {
	t.Parallel()
	p := plan2()
	p.Verdict, p.Body = false, ""
	p.Items = p.Items[:1]
	w := &fakeWriter{fail: map[string]error{"SubmitReview": errors.New("SubmitReview: Body can't be blank")}}
	if _, err, _ := runSend(t, p, w, newLedger(), OptSend); err != nil {
		t.Fatal(err)
	}
	subs := slices.DeleteFunc(slices.Clone(w.calls), func(c string) bool { return !strings.HasPrefix(c, "SubmitReview") })
	if !slices.Equal(subs, []string{"SubmitReview R1 COMMENT ", "SubmitReview R1 COMMENT 1 comment"}) {
		t.Fatalf("submits = %q", subs)
	}
}

func TestSendActionsGoOneByOne(t *testing.T) {
	t.Parallel()
	p := SendPlan{Target: "o/r #7", PR: 7, Mode: SendActions, Items: []SendItem{
		{Key: "d1", Label: "reply 1", Kind: SendReply, ThreadID: "TA", Body: "one"},
		{Key: "d2", Label: "reply 2", Kind: SendReply, ThreadID: "TB", Body: "two"},
		{Label: "resolve TC", Kind: SendResolve, ThreadID: "TC"},
	}}
	w := &fakeWriter{fail: map[string]error{"Reply": errors.New("HTTP 404")}}
	l := newLedger()
	res, err, asked := runSend(t, p, w, l, OptSend)
	if err == nil || !strings.Contains(err.Error(), "1 of 3") {
		t.Fatalf("err = %v (a partial failure reports how many failed)", err)
	}
	if !slices.Equal(asked[0].Options, []string{OptSend, "abort"}) {
		t.Fatalf("options = %v", asked[0].Options)
	}
	if l.failed["d1"] == "" || l.stamps["d2"].Comment != "RCTB" || !slices.Contains(w.calls, "Resolve TC") || l.settled != 1 {
		t.Fatalf("failed %v stamps %v calls %v settled %d res %+v", l.failed, l.stamps, w.calls, l.settled, res)
	}
}

func TestResolveOnlyNeedsNoConfirm(t *testing.T) {
	t.Parallel()
	p := SendPlan{Target: "o/r #7", PR: 7, Mode: SendActions, Items: []SendItem{{Label: "resolve", Kind: SendResolve, ThreadID: "T"}}}
	w := &fakeWriter{}
	_, err, asked := runSend(t, p, w, newLedger(), "never")
	if err != nil || len(asked) != 0 || !slices.Equal(w.calls, []string{"Resolve T"}) {
		t.Fatalf("err %v asked %v calls %v", err, asked, w.calls)
	}
}

func TestFinishAndDiscard(t *testing.T) {
	t.Parallel()
	w, l := &fakeWriter{}, newLedger()
	if _, err, _ := runSend(t, SendPlan{Target: "o/r #7", PR: 7, Mode: SendFinish, Pending: "P"}, w, l, OptSend); err != nil {
		t.Fatal(err)
	}
	if _, err, _ := runSend(t, SendPlan{Target: "o/r #7", PR: 7, Mode: SendDiscard, Pending: "P"}, w, l, OptDiscard); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(w.calls, []string{"SubmitReview P COMMENT ", "DeletePendingReview P"}) || l.settled != 2 {
		t.Fatalf("calls %v settled %d", w.calls, l.settled)
	}
}

func TestSendWithNothingToSendIsAnError(t *testing.T) {
	t.Parallel()
	p := SendPlan{Target: "o/r #7", PR: 7, Mode: SendReview, Skipped: []SendSkip{{Label: "x", Reason: "not in this PR"}}}
	if _, err, asked := runSend(t, p, &fakeWriter{}, newLedger(), OptSend); err == nil || len(asked) != 0 {
		t.Fatalf("err %v asked %d", err, len(asked))
	}
}
```

(`deciderFunc` — add to the test file if the package has no such adapter: `type deciderFunc func(context.Context, DecisionRequest) (DecisionResponse, error)` with a `Decide` method.)

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/engine/ -run 'Send|ResolveOnly|FinishAndDiscard' -count=1`
Expected: FAIL — `undefined: SendToForge`.

- [ ] **Step 3: Implement `internal/engine/send_forge.go`**

```go
package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/forge"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/repogate"
)

// (types from the Interfaces block above)

const DecisionSendForge = "forge.send"

const (
	OptComment           = "comment"
	OptApprove           = "approve"
	OptRequestChanges    = "request-changes"
	OptSend              = "send"
	OptSubmitWithPending = "submit-with-pending"
	OptDiscard           = "discard"
)

// ErrNothingToSend: every item was skipped (or none was given).
var ErrNothingToSend = errors.New("nothing to send: every item was skipped")

var _ Operation = SendToForge{}

// LockMode: the op touches no git state; the note store has its own lock.
func (op SendToForge) LockMode() repogate.Mode { return repogate.Read }

func (op SendToForge) now() time.Time {
	if op.Now != nil {
		return op.Now()
	}
	return time.Now()
}

func (op SendToForge) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Writer == nil || op.Ledger == nil {
		return Result{}, fmt.Errorf("send to forge: Writer and Ledger are required")
	}
	p := op.Plan
	switch p.Mode {
	case SendFinish:
		return op.finish(ctx, deps, p)
	case SendDiscard:
		return op.discard(ctx, deps, p)
	case SendActions:
		return op.actions(ctx, deps, p)
	}
	return op.review(ctx, deps, p)
}

// confirm asks once and checks the answer is one of the options.
func confirm(ctx context.Context, deps OpDeps, p SendPlan, options []string) (string, error) {
	req := PromptReq(DecisionSendForge, "Send to %s:\n%s", options, p.Target, describePlan(p))
	resp, err := deps.decide(ctx, req)
	if err != nil {
		return "", err
	}
	if !slices.Contains(options, resp.Option) {
		return "", fmt.Errorf("%s: %q is not one of %s", DecisionSendForge, resp.Option, strings.Join(options, ", "))
	}
	return resp.Option, nil
}

// describePlan is the confirm's body: the review body, every item and every
// skipped item with its reason — exactly what will be posted.
func describePlan(p SendPlan) string {
	var b strings.Builder
	if strings.TrimSpace(p.Body) != "" {
		fmt.Fprintf(&b, "review body:\n%s\n", indent(forge.StripSendMarker(p.Body)))
	}
	for _, it := range p.Items {
		fmt.Fprintf(&b, "  + %s\n", it.Label)
		for range it.Replies {
			fmt.Fprintf(&b, "      + reply\n")
		}
	}
	for _, s := range p.Skipped {
		fmt.Fprintf(&b, "  - %s (skipped: %s)\n", s.Label, s.Reason)
	}
	return strings.TrimRight(b.String(), "\n")
}

func indent(s string) string { return "    " + strings.ReplaceAll(s, "\n", "\n    ") }

func (op SendToForge) review(ctx context.Context, deps OpDeps, p SendPlan) (Result, error) {
	if len(p.Items) == 0 && !p.Verdict {
		return Result{}, ErrNothingToSend
	}
	options := []string{OptSend, "abort"}
	if p.Verdict {
		options = []string{OptComment, OptApprove, OptRequestChanges, "abort"}
		if p.OwnPR {
			options = []string{OptComment, "abort"}
		}
	}
	if p.Pending != "" {
		options = []string{OptSubmitWithPending, "abort"}
	}
	choice, err := confirm(ctx, deps, p, options)
	if err != nil {
		return Result{}, err
	}
	if choice == "abort" {
		return Result{}.WithSummary("aborted: sending to %s", p.Target), nil
	}
	ev := forge.EventComment
	switch choice {
	case OptApprove:
		ev = forge.EventApprove
	case OptRequestChanges:
		ev = forge.EventRequestChanges
	}
	joined := p.Pending != ""
	review := p.Pending
	if !joined {
		if review, err = op.Writer.StartReview(ctx, p.PRID, p.Head); err != nil {
			op.Ledger.Fail(ctx, planKeys(p), err)
			return Result{}, err
		}
	}
	stamp := func(key string, s model.NoteSend) {
		if key != "" {
			s.PR, s.Review, s.At = p.PR, review, op.now()
			_ = op.Ledger.Stamp(ctx, key, s)
		}
	}
	stamp(p.Key, model.NoteSend{})
	for _, it := range p.Items {
		stamp(it.Key, model.NoteSend{})
		for _, r := range it.Replies {
			stamp(r.Key, model.NoteSend{})
		}
	}
	fail := func(err error) (Result, error) {
		if joined {
			// The user's own pending review: never deleted. The stamps stay, so
			// `--finish` (or the browser) can submit what gg added.
			return Result{}, fmt.Errorf("%w (your pending review on GitHub now holds gg's comments: finish or discard it)", err)
		}
		if derr := op.Writer.DeletePendingReview(ctx, review); derr != nil {
			err = errors.Join(err, fmt.Errorf("deleting the pending review: %w", derr))
		}
		op.Ledger.Fail(ctx, planKeys(p), err)
		return Result{}, err
	}
	threads := map[string]string{} // item key → thread id
	for _, it := range p.Items {
		deps.emit(ctx, Progress{Step: "sending", Detail: it.Label})
		ref, err := op.Writer.AddThread(ctx, review, it.Thread)
		if err != nil {
			return fail(err)
		}
		threads[it.Key] = ref.ID
		stamp(it.Key, model.NoteSend{Thread: ref.ID, Comment: ref.CommentID, URL: ref.URL})
		for _, r := range it.Replies {
			c, err := op.Writer.Reply(ctx, review, ref.ID, r.Body)
			if err != nil {
				return fail(err)
			}
			stamp(r.Key, model.NoteSend{Thread: ref.ID, Comment: c.ID, URL: c.URL})
		}
	}
	if err := op.submit(ctx, review, ev, p.Body, len(p.Items)); err != nil {
		return fail(err)
	}
	res := Result{Changed: true}.WithSummary("sent %d comments to %s", len(p.Items), p.Target)
	if err := op.Ledger.Settle(ctx); err != nil {
		res = res.AppendSummary("; local copies are removed at the next refresh (%v)", err)
	}
	var unresolved int
	for _, it := range p.Items {
		if it.Resolve {
			if err := op.Writer.Resolve(ctx, threads[it.Key]); err != nil {
				unresolved++
			}
		}
	}
	if unresolved > 0 {
		res = res.AppendSummary("; %d threads could not be resolved", unresolved)
	}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

// submit submits, retrying ONCE with a count body when the forge refuses a
// blank one (whether GraphQL does is unverified — spec §3.4).
func (op SendToForge) submit(ctx context.Context, review string, ev forge.Event, body string, n int) error {
	err := op.Writer.SubmitReview(ctx, review, ev, body)
	if err == nil || strings.TrimSpace(body) != "" || !forge.IsBlankBodyError(err) {
		return err
	}
	fallback := "1 comment"
	if n != 1 {
		fallback = fmt.Sprintf("%d comments", n)
	}
	return op.Writer.SubmitReview(ctx, review, ev, fallback)
}

func planKeys(p SendPlan) []string {
	var keys []string
	if p.Key != "" {
		keys = append(keys, p.Key)
	}
	for _, it := range p.Items {
		if it.Key != "" {
			keys = append(keys, it.Key)
		}
		for _, r := range it.Replies {
			keys = append(keys, r.Key)
		}
	}
	return keys
}

func (op SendToForge) actions(ctx context.Context, deps OpDeps, p SendPlan) (Result, error) {
	if len(p.Items) == 0 {
		return Result{}, ErrNothingToSend
	}
	needsConfirm := slices.ContainsFunc(p.Items, func(it SendItem) bool { return it.Kind == SendReply })
	if needsConfirm { // resolve / unresolve alone are immediate (spec §3.5)
		choice, err := confirm(ctx, deps, p, []string{OptSend, "abort"})
		if err != nil {
			return Result{}, err
		}
		if choice == "abort" {
			return Result{}.WithSummary("aborted: sending to %s", p.Target), nil
		}
	}
	var failed int
	var firstErr error
	for _, it := range p.Items {
		var err error
		switch it.Kind {
		case SendReply:
			if it.Key != "" {
				_ = op.Ledger.Stamp(ctx, it.Key, model.NoteSend{PR: p.PR, Thread: it.ThreadID, At: op.now()})
			}
			var c forge.CommentRef
			if c, err = op.Writer.Reply(ctx, "", it.ThreadID, it.Body); err == nil && it.Key != "" {
				_ = op.Ledger.Stamp(ctx, it.Key, model.NoteSend{PR: p.PR, Thread: it.ThreadID, Comment: c.ID, URL: c.URL, At: op.now()})
			}
		case SendResolve:
			err = op.Writer.Resolve(ctx, it.ThreadID)
		case SendUnresolve:
			err = op.Writer.Unresolve(ctx, it.ThreadID)
		}
		if err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
			if it.Key != "" {
				op.Ledger.Fail(ctx, []string{it.Key}, err)
			}
		}
	}
	settleErr := op.Ledger.Settle(ctx)
	if failed > 0 {
		return Result{Changed: failed < len(p.Items)}, fmt.Errorf("%d of %d failed: %w", failed, len(p.Items), firstErr)
	}
	res := Result{Changed: true}.WithSummary("sent %d actions to %s", len(p.Items), p.Target)
	if settleErr != nil {
		res = res.AppendSummary("; local copies are removed at the next refresh (%v)", settleErr)
	}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

func (op SendToForge) finish(ctx context.Context, deps OpDeps, p SendPlan) (Result, error) {
	choice, err := confirm(ctx, deps, p, []string{OptSend, "abort"})
	if err != nil {
		return Result{}, err
	}
	if choice == "abort" {
		return Result{}.WithSummary("aborted: sending to %s", p.Target), nil
	}
	if err := op.Writer.SubmitReview(ctx, p.Pending, forge.EventComment, p.Body); err != nil {
		return Result{}, err
	}
	res := Result{Changed: true}.WithSummary("submitted the pending review on %s", p.Target)
	if err := op.Ledger.Settle(ctx); err != nil {
		res = res.AppendSummary("; local copies are removed at the next refresh (%v)", err)
	}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

func (op SendToForge) discard(ctx context.Context, deps OpDeps, p SendPlan) (Result, error) {
	choice, err := confirm(ctx, deps, p, []string{OptDiscard, "abort"})
	if err != nil {
		return Result{}, err
	}
	if choice == "abort" {
		return Result{}.WithSummary("aborted: sending to %s", p.Target), nil
	}
	if err := op.Writer.DeletePendingReview(ctx, p.Pending); err != nil {
		return Result{}, err
	}
	res := Result{Changed: true}.WithSummary("discarded the pending review on %s", p.Target)
	_ = op.Ledger.Settle(ctx) // the stamps naming it are cleared: the items are local again
	deps.emit(ctx, Done{Result: res})
	return res, nil
}
```

Note the blank-body test: the plan's `Verdict` false + body "" means the single-note review sends `COMMENT` with an empty body; the fake fails it once with "Body can't be blank" and the retry carries "1 comment".

- [ ] **Step 4: Wire the gates**

- `internal/tui/source.go`: add `engine.SendToForge` to the `FetchPRHead, ForgetPR` case (touches no panel; empty, not nil).
- `optionDisplayName`: add the missing cases among `comment`, `approve`, `request-changes`, `send`, `submit-with-pending`, `discard`, each `return i18n.T("<value>")` (check first which exist).
- Bundles: add every new key to all four files — the prompt `"Send to %s:\n%s"`, the step `"sending"`, the summaries `"aborted: sending to %s"`, `"sent %d comments to %s"`, `"sent %d actions to %s"`, `"; local copies are removed at the next refresh (%v)"`, `"; %d threads could not be resolved"`, `"submitted the pending review on %s"`, `"discarded the pending review on %s"`, and the option labels. Translate each (ja/ko/zh/ru), keeping verbs.

- [ ] **Step 5: Run the tests and the i18n gates**

Run: `go test ./internal/engine/ -count=1 && go test ./internal/i18n/ ./internal/tui/ -run 'I18n|EngineProse|DecisionOptionValues|ActionMenuLabels|CheckVerbs|OpAffected' -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
gg add internal/engine internal/tui/source.go internal/tui/i18n_display.go internal/i18n/lang
git commit -m "feat(engine): SendToForge — one confirm, all-or-nothing reviews, actions one by one"
```

---

### Task 5: domain — the send ledger: stamps, failures, one settle pass on every PR read

**Files:**
- Create: `internal/domain/forge_send_ledger.go`
- Create: `internal/domain/forge_send_ledger_test.go`
- Modify: `internal/domain/forge_cache.go` (`PRRevalidate` notes its read start and runs `settleSends`)
- Modify: `internal/domain/service.go` (`sendIdx *sendIndexT`, `sendIdxGen` beside `previewCounts`)

**Interfaces:**
- Consumes: `notes.Store.Edit`, `model.NoteSend`, `model.RemarkSend` (Task 2); `forge.SendMarkerKey` (Task 1); `Review.RemarkSends` (Task 3); `engine.SendLedger` (Task 4).
- Produces:
  ```go
  func RemarkKey(reviewID, fp string) string                  // "remark:<review id>:<fp>"
  func parseRemarkKey(key string) (reviewID, fp string, ok bool)
  type prLedger struct{ s *Service; pr int }                 // implements engine.SendLedger
  func (s *Service) sendLedger(pr int) engine.SendLedger
  func (s *Service) settleSends(ctx context.Context, pr model.PullRequest, cs []model.ForgeComment, readStart time.Time)
  // PRInterrupted is what an interrupted send left on GitHub: the pending review
  // gg's stamps name (it must equal the PR's ViewerPendingReview) and the keys
  // waiting in it; "" when nothing is pending.
  func (s *Service) PRInterrupted(ctx context.Context, n int) (reviewID string, keys []string)
  ```

The settle rules (spec §3.4), for every stamped item of PR n whose stamp is OLDER than the read's start (a stamp written after the read began is never judged by it — another process may be mid-send):

| What the read shows | Stored note / reply | Remark entry | Whole-review note |
|---|---|---|---|
| its comment (by stamp comment id, its marker, or — for an item of a review — the thread gg created) in a SUBMITTED review | delete the note | `Moved = true` (keep thread + URL) | review submitted → every remark settled moved? delete the note : clear its own stamp (R4) |
| its stamp's review is the viewer's pending review | keep (interrupted) | keep | keep |
| neither | clear the stamp (local again) | drop the entry | clear the stamp |

A comment is in a submitted review when its `ReviewID` is not the PR's `ViewerPendingReview`. A note with NO stamp whose id is the marker key of a submitted comment of this PR is deleted too (a lost stamp). Failed stamps (`Err != ""`) are never touched.

- [ ] **Step 1: Write the failing tests** (`internal/domain/forge_send_ledger_test.go`)

```go
package domain

import (
	"context"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/forge"
	"github.com/homeend/gigagit/internal/model"
)

var settleT0 = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

// settleSvc: prThreadSvc's PR with the clock at settleT0 and two stored
// notes on a.go; it returns the fake forge to script the next read.
func settleSvc(t *testing.T) (*Service, *fakeForge, string, string) {
	t.Helper()
	svc, _, head := prThreadSvc(t)
	svc.forgeNow = func() time.Time { return settleT0 }
	ctx := context.Background()
	add := func(sum string) string {
		n, err := svc.NoteAdd(ctx, model.Note{Source: model.NoteSourceUser, Summary: sum,
			Address: model.FileAddress{State: model.StateCommitted, Commit: head, Path: "a.go"},
			Side:    model.NoteSideNew, Range: [2]int{1, 1}})
		if err != nil {
			t.Fatal(err)
		}
		return n.ID
	}
	ff := svc.forgeProviders()[0].(*fakeForge) // or keep the pointer from prThreadSvc
	return svc, ff, add("one"), add("two")
}

func stampNote(t *testing.T, svc *Service, id string, s model.NoteSend) {
	t.Helper()
	if err := svc.notesStore(context.Background()).Edit(id, func(n *model.Note) error { n.Send = &s; return nil }); err != nil {
		t.Fatal(err)
	}
}

func noteByID(t *testing.T, svc *Service, id string) (model.Note, bool) {
	t.Helper()
	all, err := svc.notesStore(context.Background()).LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range all {
		if n.ID == id {
			return n, true
		}
	}
	return model.Note{}, false
}

func TestSettleDeletesWhatGitHubHas(t *testing.T) {
	t.Parallel()
	svc, ff, a, b := settleSvc(t)
	stampNote(t, svc, a, model.NoteSend{PR: 7, Review: "PRR_new", Thread: "PRRT_new1", At: settleT0.Add(-time.Minute)})
	stampNote(t, svc, b, model.NoteSend{PR: 7, Review: "PRR_gone", At: settleT0.Add(-time.Minute)})
	ff.mu.Lock()
	ff.comments = append(ff.comments, model.ForgeComment{ID: "PRRC_new1", Kind: model.ForgeCommentInline, Path: "a.go",
		Line: 1, Body: "one\n\n" + forge.SendMarker(a), ThreadID: "PRRT_new1", ReviewID: "PRR_new"})
	ff.mu.Unlock()
	if _, err := svc.PRRevalidate(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if _, ok := noteByID(t, svc, a); ok {
		t.Error("a sent note must be deleted locally (GitHub owns it now)")
	}
	n, ok := noteByID(t, svc, b)
	if !ok || n.Send != nil {
		t.Errorf("a stamp whose review is gone must be cleared: %+v %v", n.Send, ok)
	}
}

func TestSettleKeepsAPendingReviewsItems(t *testing.T) {
	t.Parallel()
	svc, ff, a, _ := settleSvc(t)
	stampNote(t, svc, a, model.NoteSend{PR: 7, Review: "PRR_p", Thread: "PRRT_p1", At: settleT0.Add(-time.Minute)})
	ff.mu.Lock()
	pr := ff.byNum[7]
	pr.ViewerPendingReview = "PRR_p"
	ff.byNum[7] = pr
	ff.comments = append(ff.comments, model.ForgeComment{ID: "PRRC_p1", Kind: model.ForgeCommentInline, Path: "a.go",
		Line: 1, Body: "one", ThreadID: "PRRT_p1", ReviewID: "PRR_p"})
	ff.mu.Unlock()
	ctx := context.Background()
	if _, err := svc.PRRevalidate(ctx, 7); err != nil {
		t.Fatal(err)
	}
	n, ok := noteByID(t, svc, a)
	if !ok || n.Send == nil || n.Send.Review != "PRR_p" {
		t.Fatalf("an interrupted send stays stamped: %+v %v", n.Send, ok)
	}
	if rev, keys := svc.PRInterrupted(ctx, 7); rev != "PRR_p" || len(keys) != 1 || keys[0] != a {
		t.Fatalf("PRInterrupted = %q %v", rev, keys)
	}
}

func TestSettleNeverJudgesAStampNewerThanTheRead(t *testing.T) {
	t.Parallel()
	svc, _, a, _ := settleSvc(t)
	// Stamped by another process after this read began: the read cannot see
	// its pending review yet, so "gone" would be a lie.
	stampNote(t, svc, a, model.NoteSend{PR: 7, Review: "PRR_racing", At: settleT0.Add(time.Second)})
	if _, err := svc.PRRevalidate(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if n, _ := noteByID(t, svc, a); n.Send == nil || n.Send.Review != "PRR_racing" {
		t.Fatalf("a newer stamp was judged: %+v", n.Send)
	}
}

func TestSettleClearsAWholeReviewWhoseReviewIsGone(t *testing.T) {
	t.Parallel()
	svc, r := structuredReview(t)
	svc.forgeNow = func() time.Time { return settleT0 }
	ctx := context.Background()
	if err := svc.notesStore(ctx).Edit(r.ID, func(n *model.Note) error {
		n.Send = &model.NoteSend{PR: 7, Review: "PRR_deleted", At: settleT0.Add(-time.Minute)}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	svc.invalidateNoteCounts()
	svc.settleSends(ctx, model.PullRequest{Number: 7}, nil, settleT0)
	r2, _ := svc.Review(ctx, r.ID)
	n, _ := noteByID(t, svc, r2.ID)
	if n.Send != nil {
		t.Fatalf("a whole review whose GitHub review is gone stays sending forever: %+v", n.Send)
	}
}

// The op calls Settle while holding a Read reservation: a writer queued
// behind it must not deadlock a gate-taking read inside Settle (R12).
func TestSettleNeverTakesTheGate(t *testing.T) {
	t.Parallel()
	svc, _, a, _ := settleSvc(t)
	stampNote(t, svc, a, model.NoteSend{PR: 7, Review: "PRR_gone", At: settleT0.Add(-time.Minute)})
	ctx := context.Background()
	gate := svc.gateFor(ctx)
	held, err := gate.Acquire(ctx, repogate.Read, "op SendToForge")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	go func() { // a writer queues behind the op
		if w, err := gate.Acquire(ctx, repogate.TreeWrite, "op Commit"); err == nil {
			w.Release()
		}
	}()
	for len(gate.Queue()) < 2 { // wait until the writer is queued
		time.Sleep(time.Millisecond)
	}
	done := make(chan error, 1)
	go func() { done <- svc.sendLedger(7).Settle(ctx) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Settle blocked on the repo gate under a queued writer")
	}
}

func TestSettleNeverTakesAnExistingThreadForASentReply(t *testing.T) {
	t.Parallel()
	svc, ff, a, _ := settleSvc(t)
	// A standalone reply stamped before its Reply call answered (a crash):
	// its thread exists on GitHub, its comment does not.
	stampNote(t, svc, a, model.NoteSend{PR: 7, Thread: "PRRT_t1", At: settleT0.Add(-time.Minute)})
	_ = ff
	if _, err := svc.PRRevalidate(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	n, ok := noteByID(t, svc, a)
	if !ok || n.Send != nil {
		t.Fatalf("the reply was never posted: keep it, local again (%+v, %v)", n.Send, ok)
	}
}

func TestSettleLeavesFailuresAndMatchesALostStamp(t *testing.T) {
	t.Parallel()
	svc, ff, a, b := settleSvc(t)
	stampNote(t, svc, a, model.NoteSend{PR: 7, Err: "HTTP 403", At: settleT0.Add(-time.Minute)})
	ff.mu.Lock()
	ff.comments = append(ff.comments, model.ForgeComment{ID: "PRRC_x", Kind: model.ForgeCommentInline, Path: "a.go",
		Line: 1, Body: "two\n\n" + forge.SendMarker(b), ThreadID: "PRRT_x", ReviewID: "PRR_x"})
	ff.mu.Unlock()
	if _, err := svc.PRRevalidate(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if n, ok := noteByID(t, svc, a); !ok || n.Send == nil || n.Send.Err != "HTTP 403" {
		t.Errorf("a failed stamp must stay: %+v", n.Send)
	}
	if _, ok := noteByID(t, svc, b); ok {
		t.Error("an unstamped note whose marker GitHub echoes was sent: delete it")
	}
}

func TestLedgerStampsAndFailsRemarks(t *testing.T) {
	t.Parallel()
	svc, r := structuredReview(t)
	ctx := context.Background()
	fp := r.remarkFPs()[0]
	l := svc.sendLedger(7)
	key := RemarkKey(r.ID, fp)
	if err := l.Stamp(ctx, key, model.NoteSend{PR: 7, Review: "R1", Thread: "T1", At: settleT0}); err != nil {
		t.Fatal(err)
	}
	r2, _ := svc.Review(ctx, r.ID)
	if len(r2.RemarkSends) != 1 || r2.RemarkSends[0].Send.Thread != "T1" || r2.RemarkSends[0].Moved {
		t.Fatalf("after Stamp: %+v", r2.RemarkSends)
	}
	l.Fail(ctx, []string{key}, errFake("HTTP 500"))
	r3, _ := svc.Review(ctx, r.ID)
	if s := r3.RemarkSends[0].Send; s.Err != "HTTP 500" || s.Thread != "" {
		t.Fatalf("after Fail: %+v", s)
	}
}

type errFake string

func (e errFake) Error() string { return string(e) }
```

(Adapt the `ff` plumbing to how `prThreadSvc` exposes its fake forge — returning it from `prThreadSvc` is simplest; `svc.forgeProviders()` above is illustrative.)

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/domain/ -run 'Settle|LedgerStamps' -count=1`
Expected: FAIL — `svc.sendLedger undefined`, `undefined: RemarkKey`.

- [ ] **Step 3: Implement `forge_send_ledger.go`**

```go
package domain

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/forge"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notes"
)

const remarkKeyPrefix = "remark:"

// RemarkKey names one review remark as a send item (and in its marker).
func RemarkKey(reviewID, fp string) string { return remarkKeyPrefix + reviewID + ":" + fp }

func parseRemarkKey(key string) (string, string, bool) {
	rest, ok := strings.CutPrefix(key, remarkKeyPrefix)
	if !ok {
		return "", "", false
	}
	i := strings.LastIndexByte(rest, ':')
	if i <= 0 {
		return "", "", false
	}
	return rest[:i], rest[i+1:], true
}

type prLedger struct {
	s  *Service
	pr int
}

func (s *Service) sendLedger(pr int) engine.SendLedger { return prLedger{s: s, pr: pr} }

// editKey applies fn to the stamp of one item: a stored note's Send, or a
// remark's entry on its review note (created when missing).
func (l prLedger) editKey(ctx context.Context, key string, fn func(*model.NoteSend) bool) error {
	st := l.s.notesStore(ctx)
	if st == nil {
		return ErrNotesDisabled
	}
	defer l.s.invalidateNoteCounts()
	if rid, fp, ok := parseRemarkKey(key); ok {
		return st.Edit(rid, func(n *model.Note) error {
			out := slices.Clone(n.RemarkSends)
			i := slices.IndexFunc(out, func(r model.RemarkSend) bool { return r.RemarkFP == fp })
			if i < 0 {
				out, i = append(out, model.RemarkSend{RemarkFP: fp}), len(out)
			}
			if !fn(&out[i].Send) { // false = drop the entry
				out = slices.Delete(out, i, i+1)
			}
			n.RemarkSends = out
			return nil
		})
	}
	return st.Edit(key, func(n *model.Note) error {
		s := model.NoteSend{}
		if n.Send != nil {
			s = *n.Send
		}
		if fn(&s) {
			n.Send = &s
		} else {
			n.Send = nil
		}
		return nil
	})
}

func (l prLedger) Stamp(ctx context.Context, key string, s model.NoteSend) error {
	return l.editKey(ctx, key, func(cur *model.NoteSend) bool {
		// Ids only grow during one send: a later stamp never forgets one.
		if s.Thread == "" {
			s.Thread = cur.Thread
		}
		if s.Comment == "" {
			s.Comment = cur.Comment
		}
		if s.URL == "" {
			s.URL = cur.URL
		}
		s.Err = ""
		*cur = s
		return true
	})
}

func (l prLedger) Fail(ctx context.Context, keys []string, err error) {
	for _, k := range keys {
		_ = l.editKey(ctx, k, func(cur *model.NoteSend) bool {
			*cur = model.NoteSend{PR: l.pr, Err: err.Error(), At: l.s.forgeClock()}
			return true
		})
	}
}

// Settle is write-through and crash recovery at once: one read of the PR,
// whose snapshot store runs the settle pass.
func (l prLedger) Settle(ctx context.Context) error {
	_, err := l.s.PRRevalidate(ctx, l.pr)
	return err
}

// settleSends applies the settle table (plan Task 5) to PR pr's stamped
// items, judging only stamps older than readStart. It runs on EVERY
// PRRevalidate (the TUI heartbeat, the web poll), so the common case —
// nothing of this PR is stamped and no echoed marker names a local note —
// costs no store read: sendIndex answers it from a per-notes-generation memo.
func (s *Service) settleSends(ctx context.Context, pr model.PullRequest, cs []model.ForgeComment, readStart time.Time) {
	idx := s.sendIndex(ctx)
	if !idx.prs[pr.Number] && !idx.echoed(cs) {
		return
	}
	st := s.notesStore(ctx)
	if st == nil {
		return
	}
	all, err := st.LoadAll()
	if err != nil {
		return
	}
	pending := pr.ViewerPendingReview
	submitted := func(c model.ForgeComment) bool { return c.ReviewID == "" || c.ReviewID != pending }
	byComment, byThread, byMarker := map[string]bool{}, map[string]bool{}, map[string]bool{}
	reviewDone := map[string]bool{}
	for _, c := range cs {
		if !submitted(c) {
			continue
		}
		byComment[c.ID] = true
		if c.ThreadID != "" {
			byThread[c.ThreadID] = true
		}
		if c.ReviewID != "" {
			reviewDone[c.ReviewID] = true
		}
		if c.Kind == model.ForgeCommentReview {
			reviewDone[c.ID] = true
		}
		if k, ok := forge.SendMarkerKey(c.Body); ok {
			byMarker[k] = true
		}
	}
	judge := func(key string, sd *model.NoteSend) (sent, keep bool) {
		switch {
		// A thread proves a send only for a REVIEW item (gg created that
		// thread in that review); a standalone reply's thread existed before
		// it, so only its own comment id or marker does.
		case byMarker[key] || (sd != nil && (byComment[sd.Comment] || (sd.Review != "" && byThread[sd.Thread]))):
			return true, false
		case sd == nil:
			return false, true // unstamped and not echoed: plain local
		case sd.Err != "" || sd.PR != pr.Number || !sd.At.Before(readStart):
			return false, true
		case sd.Review != "" && sd.Review == pending:
			return false, true
		}
		return false, false // gone: clear
	}
	changed := false
	for _, n := range all {
		if n.IsReviewNote() || n.IsWorkingReview() {
			if s.settleReview(ctx, st, n, pr.Number, pending, judge, reviewDone, readStart) {
				changed = true
			}
			continue
		}
		if n.Send == nil && !byMarker[n.ID] {
			continue
		}
		if n.Send != nil && n.Send.PR != pr.Number && !byMarker[n.ID] {
			continue
		}
		sent, keep := judge(n.ID, n.Send)
		switch {
		case sent:
			if err := st.Remove(n.ID); err == nil || errors.Is(err, notes.ErrNotFound) {
				changed = true
			}
		case !keep:
			_ = st.Edit(n.ID, func(x *model.Note) error { x.Send = nil; return nil })
			changed = true
		}
	}
	if changed {
		s.invalidateNoteCounts()
	}
}

// settleReview settles one review note: each remark entry, then the note's
// own whole-review stamp (R4: deleted only when every remark has moved).
func (s *Service) settleReview(ctx context.Context, st notes.Store, n model.Note, pr int, pending string,
	judge func(string, *model.NoteSend) (bool, bool), reviewDone map[string]bool, readStart time.Time) bool {
	if len(n.RemarkSends) == 0 && n.Send == nil {
		return false
	}
	out := make([]model.RemarkSend, 0, len(n.RemarkSends))
	changed := false
	for _, r := range n.RemarkSends {
		if r.Moved || r.Send.PR != pr {
			out = append(out, r)
			continue
		}
		sd := r.Send
		sent, keep := judge(RemarkKey(n.ID, r.RemarkFP), &sd)
		switch {
		case sent:
			r.Moved, changed = true, true
			out = append(out, r)
		case keep:
			out = append(out, r)
		default:
			changed = true // gone: the remark is local again
		}
	}
	whole := n.Send != nil && n.Send.PR == pr && n.Send.Err == "" && n.Send.At.Before(readStart)
	switch {
	case whole && reviewDone[n.Send.Review]:
		// reviewDocOf parses the stored document only: no branch-tip
		// lookup, so the settle pass never takes the repo gate (R12).
		r := reviewDocOf(n)
		r.RemarkSends = out
		if allRemarksMoved(r) {
			return st.Remove(n.ID) == nil
		}
		_ = st.Edit(n.ID, func(x *model.Note) error { x.Send, x.RemarkSends = nil, out; return nil })
		return true
	case whole && n.Send.Review != pending:
		// Its review is neither submitted nor pending: gone. Local again.
		_ = st.Edit(n.ID, func(x *model.Note) error { x.Send, x.RemarkSends = nil, out; return nil })
		return true
	}
	if changed {
		_ = st.Edit(n.ID, func(x *model.Note) error { x.RemarkSends = out; return nil })
	}
	return changed
}

// reviewDocOf is a review note's document and send entries, nothing more:
// enough for remark fingerprints, with no git call.
func reviewDocOf(n model.Note) Review {
	r := Review{ID: n.ID, RemarkSends: n.RemarkSends}
	if doc, err := notebatch.ParseReview([]byte(n.Rationale)); err == nil {
		r.Doc = &doc
	}
	return r
}

func allRemarksMoved(r Review) bool {
	for _, fp := range r.remarkFPs() {
		if !r.remarkMoved(fp) {
			return false
		}
	}
	return true
}

// sendIndexT is what settleSends needs to know cheaply: which PRs have a
// stamped item, and which local ids (notes, reviews) exist to be echoed.
type sendIndexT struct {
	prs map[int]bool
	ids map[string]bool
}

// echoed reports a comment whose marker names a local note or a remark of
// a local review.
func (x sendIndexT) echoed(cs []model.ForgeComment) bool {
	for _, c := range cs {
		k, ok := forge.SendMarkerKey(c.Body)
		if !ok {
			continue
		}
		if rid, _, isRemark := parseRemarkKey(k); isRemark {
			k = rid
		}
		if x.ids[k] {
			return true
		}
	}
	return false
}

// sendIndex is built once per notes generation (one LoadAll per notes
// change, never per poll); Stamp/Fail invalidate it through
// invalidateNoteCounts.
func (s *Service) sendIndex(ctx context.Context) sendIndexT {
	s.mu.Lock()
	if s.sendIdx != nil && s.sendIdxGen == s.notesGen {
		x := *s.sendIdx
		s.mu.Unlock()
		return x
	}
	gen := s.notesGen
	s.mu.Unlock()
	x := sendIndexT{prs: map[int]bool{}, ids: map[string]bool{}}
	if st := s.notesStore(ctx); st != nil {
		if all, err := st.LoadAll(); err == nil {
			for _, n := range all {
				x.ids[n.ID] = true
				if n.Send != nil && n.Send.Err == "" {
					x.prs[n.Send.PR] = true
				}
				for _, r := range n.RemarkSends {
					if !r.Moved && r.Send.Err == "" {
						x.prs[r.Send.PR] = true
					}
				}
			}
		}
	}
	s.mu.Lock()
	if s.notesGen == gen {
		s.sendIdx, s.sendIdxGen = &x, gen
	}
	s.mu.Unlock()
	return x
}

// PRInterrupted (doc in the Interfaces block).
func (s *Service) PRInterrupted(ctx context.Context, n int) (string, []string) {
	p, _, ok := s.PRDetailsCached(n)
	if !ok || p.ViewerPendingReview == "" {
		return "", nil
	}
	st := s.notesStore(ctx)
	if st == nil {
		return "", nil
	}
	all, err := st.LoadAll()
	if err != nil {
		return "", nil
	}
	var keys []string
	for _, x := range all {
		if x.Send != nil && x.Send.PR == n && x.Send.Err == "" && x.Send.Review == p.ViewerPendingReview {
			keys = append(keys, x.ID)
		}
		for _, r := range x.RemarkSends {
			if !r.Moved && r.Send.PR == n && r.Send.Err == "" && r.Send.Review == p.ViewerPendingReview {
				keys = append(keys, RemarkKey(x.ID, r.RemarkFP))
			}
		}
	}
	if len(keys) == 0 {
		return "", nil
	}
	return p.ViewerPendingReview, keys
}
```

- [ ] **Step 4: Run the settle pass from `PRRevalidate`**

In `PRRevalidate`, take `readStart := s.forgeClock()` before the provider call; after `storeComments` (both branches) and `rememberPR`, call `s.settleSends(ctx, pr, raw, readStart)` where `raw` is the comment slice just read (the snapshot's or `p.Comments`'s — have `PRCommentsRefresh`'s fallback branch return the raw slice to its caller through a small internal variant, `refreshComments(ctx, n) (raw []model.ForgeComment, changed bool, err error)`, which `PRCommentsRefresh` wraps).

- [ ] **Step 4b: A sent group keeps its colour (spec §1.3)**

When `settleSends` judges an item sent, it records `forge review id → local group` (`"review:<id>"` for a remark or a whole review, `GroupMine` for a stored note) in the PR's cache entry: `prcache.Entry` gains `Groups map[string]string \`json:"groups,omitempty"\`` (written through `persistPR`, which takes `forgeMu` itself — call it with no lock held). `forgeNote` takes the map and sets `Group` to the mapped local group when the comment's `ReviewID` is in it, else `"github:<review id>"`. Test (append to `forge_send_ledger_test.go`):

```go
func TestASentNoteKeepsItsGroup(t *testing.T) {
	t.Parallel()
	svc, ff, a, _ := settleSvc(t)
	stampNote(t, svc, a, model.NoteSend{PR: 7, Review: "PRR_new", Thread: "PRRT_new1", At: settleT0.Add(-time.Minute)})
	ff.mu.Lock()
	ff.comments = append(ff.comments, model.ForgeComment{ID: "PRRC_new1", Kind: model.ForgeCommentInline, Path: "a.go",
		Line: 1, Body: "one", ThreadID: "PRRT_new1", ReviewID: "PRR_new"})
	ff.mu.Unlock()
	ctx := context.Background()
	if _, err := svc.PRRevalidate(ctx, 7); err != nil {
		t.Fatal(err)
	}
	set, _ := svc.PreviewNotes(ctx, git.PRRef(7), "main")
	got, _ := svc.PreviewNotesAt(ctx, set, "a.go")
	for _, r := range got {
		if r.Note.ID == model.ForgeNoteIDPrefix+"PRRC_new1" && r.Group != GroupMine {
			t.Fatalf("the sent note's GitHub thread lost its group: %q", r.Group)
		}
	}
}
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/domain/ -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
gg add internal/domain
git commit -m "feat(forge): send ledger — stamps, failures, and one settle pass on every PR read"
```

---

### Task 6: domain — the send planner: requests, anchoring, bodies, `PRSendOp`

**Files:**
- Create: `internal/domain/forge_send.go` (`PRSendRequest`, `PRSendOp`, `planSend`, item builders)
- Create: `internal/domain/forge_send_body.go` (`sendBody`, `reviewSendBody`, `quoteLines`)
- Create: `internal/domain/forge_send_test.go`, `internal/domain/forge_send_body_test.go`
- Modify: `internal/model/model.go` (`DiffSpec.Unified int`), `internal/git/diff_raw.go` (`-U<n>` when set), `internal/domain/query_cli.go` (the cache key carries `Unified`)
- Modify: `internal/domain/forge_test.go` (`fakeForge` implements `forge.Writer` and `forge.Snapshotter`, simulating GitHub: a submitted review's threads become comments)

**Interfaces:**
- Consumes: `engine.SendToForge`, `engine.SendPlan`, `engine.SendItem`, … (Task 4); `prLedger`, `RemarkKey`, `PRInterrupted` (Task 5); `forgeCommentByID`, `Review.remarkMoved`, `ResolvedNote.Sync` (Task 3); `forge.SendMarker`, `forge.Writer` (Task 1).
- Produces:
  ```go
  type PRSendRequest struct {
      PR        int      `toml:"pr" json:"pr"`
      Review    string   `toml:"review,omitempty" json:"review,omitempty"`       // a stored review: every unsent remark + its summary
      Mine      bool     `toml:"mine,omitempty" json:"mine,omitempty"`           // every local root note the PR shows
      Notes     []string `toml:"notes,omitempty" json:"notes,omitempty"`         // note ids, remark ids (review:<id>:<n>), draft replies
      Resolve   []string `toml:"resolve,omitempty" json:"resolve,omitempty"`     // thread ids or forge comment ids
      Unresolve []string `toml:"unresolve,omitempty" json:"unresolve,omitempty"`
      Verdict   bool     `toml:"verdict,omitempty" json:"verdict,omitempty"`     // a verdict with no comments
      Body      string   `toml:"body,omitempty" json:"body,omitempty"`
      Finish    bool     `toml:"finish,omitempty" json:"finish,omitempty"`
      Discard   bool     `toml:"discard,omitempty" json:"discard,omitempty"`
      Agent     string   `toml:"agent,omitempty" json:"agent,omitempty"`         // who asked (the body trailer for --body text)
  }
  var ErrPRHeadMoved, ErrForgeReadOnly, ErrNoInterruptedSend, ErrMixedSend, ErrSendRequest error
  func (s *Service) PRSendOp(ctx context.Context, req PRSendRequest) (engine.SendToForge, error)
  func (s *Service) planSend(ctx context.Context, req PRSendRequest) (engine.SendPlan, error)
  func placeThread(hunks []model.Hunk, side model.NoteSide, rng [2]int) (inDiff bool)
  func sendBody(n model.Note, key, quote string) string
  ```

Item rules (spec §3.2, §1.2):

| Request item | Becomes | Skipped when |
|---|---|---|
| a stored root note shown in the PR (`PreviewNotesAll`, resolved at the tip) | `SendThread`: line thread when both ends of its RESOLVED range sit in one hunk on the new side (`placeThread`); else a file-level thread whose body opens with `quoteLines`; its stored replies ride as `Replies`; a resolution sets `Resolve` | status stale ("its lines changed"); path not changed by the PR / gone from the tip ("not in this PR"); `Sync == sending` ("already being sent") |
| a review remark `review:<id>:<n>` | `SendThread` keyed `RemarkKey(id, fp)`: its lines' text (at the review's tip for the new side, its base for the old) is re-found by hash in the PR head (new) or the merge base (old), then placed as above; its remark replies ride along | text not found ("its lines changed"); path not in the PR; already moved |
| `--review <id>` | every unmoved remark as above, `Key` = the review note, `Body` = `reviewSendBody`, `Verdict` | (per remark) |
| `--mine` | every stored root of `Group == GroupMine` the PR shows, `Verdict` | (per note) |
| a draft reply (`IsForgeReply`) | `SendReply` (`SendActions`) to its thread's id | its thread is not in the cache |
| `--resolve` / `--unresolve` id | `SendResolve` / `SendUnresolve` (`SendActions`); a comment id maps to its thread | unknown id → error |
| `--finish` / `--discard` | `SendFinish` / `SendDiscard` on `PRInterrupted`'s review | none interrupted → `ErrNoInterruptedSend` |

Mixing a thread item with a reply/resolve in one request is `ErrMixedSend` (two different confirms). A forge id in `Notes` is `ErrSendRequest` ("that is a GitHub comment: answer it with gg pr reply").

- [ ] **Step 1: Write the failing tests**

`internal/domain/forge_send_body_test.go`:

```go
package domain

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/forge"
	"github.com/homeend/gigagit/internal/model"
)

func TestSendBodyUserNote(t *testing.T) {
	t.Parallel()
	got := sendBody(model.Note{Source: model.NoteSourceUser, Summary: "rename this", Rationale: "it shadows x"}, "n1", "")
	want := "rename this\n\nit shadows x\n\n" + forge.SendMarker("n1")
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestSendBodyAgentNoteFoldsDetailsAndSigns(t *testing.T) {
	t.Parallel()
	got := sendBody(model.Note{Source: model.NoteSourceAgent, Author: "claude", Summary: "nil deref",
		Rationale: "p can be nil here", Tags: []string{"severity: bug"}, Confidence: 0.8}, "remark:r1:ab12", "")
	for _, s := range []string{"nil deref\n\n<details><summary>Details</summary>\n\np can be nil here",
		"- severity: bug", "confidence: 0.8", "</details>", "— claude via gg", forge.SendMarker("remark:r1:ab12")} {
		if !strings.Contains(got, s) {
			t.Errorf("body lacks %q:\n%s", s, got)
		}
	}
	if !strings.HasSuffix(got, forge.SendMarker("remark:r1:ab12")) {
		t.Errorf("the marker must be the last line:\n%s", got)
	}
}

func TestSendBodyQuotesTheLineOfAFileLevelThread(t *testing.T) {
	t.Parallel()
	q := quoteLines(15, []string{"\tx := 1"})
	got := sendBody(model.Note{Source: model.NoteSourceUser, Summary: "why 1?"}, "n2", q)
	if !strings.HasPrefix(got, "> Line 15:\n> ```\n> \tx := 1\n> ```\n\nwhy 1?") {
		t.Fatalf("got %q", got)
	}
}
```

`internal/domain/forge_send_test.go`:

```go
package domain

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/forge"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// sendRepo: main holds big.go (30 lines) and other.go; the PR (#7) changes
// big.go line 5 and line 25 (two hunks: 2–8 and 22–28 at -U3). Returns the
// service (fake forge + note store), the fake forge and the PR head.
func sendRepo(t *testing.T) (*Service, *fakeForge, string) {
	t.Helper()
	dir, _ := newRealRepo(t)
	var lines []string
	for i := 1; i <= 30; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	big := strings.Join(lines, "\n") + "\n"
	commitFile(t, dir, "big.go", big, "big")
	commitFile(t, dir, "other.go", "package other\n", "other")
	runGitIn(t, dir, "checkout", "-q", "-b", "feat")
	lines[4], lines[24] = "line 5 changed", "line 25 changed"
	commitFile(t, dir, "big.go", strings.Join(lines, "\n")+"\n", "change")
	head := revParse(t, dir, "HEAD")
	runGitIn(t, dir, "update-ref", git.PRRef(7), head)
	runGitIn(t, dir, "checkout", "-q", "main")
	_, svc := newRealRepoAt(t, dir)
	svc.UseNotesDir(t.TempDir())
	ff := &fakeForge{slug: "github.com/o/r",
		byNum: map[int]model.PullRequest{7: {Number: 7, State: "open", Target: "main", HeadSHA: head, NodeID: "PR_7"}}}
	svc.SetForgeProviders([]forge.Provider{ff})
	return svc, ff, head
}

func addNote(t *testing.T, svc *Service, commit, path string, line int, sum string) string {
	t.Helper()
	n, err := svc.NoteAdd(context.Background(), model.Note{Source: model.NoteSourceUser, Summary: sum,
		Address: model.FileAddress{State: model.StateCommitted, Commit: commit, Path: path},
		Side:    model.NoteSideNew, Range: [2]int{line, line}})
	if err != nil {
		t.Fatal(err)
	}
	return n.ID
}

func TestPlanSendPlacesEachNote(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	inHunk := addNote(t, svc, head, "big.go", 5, "in the hunk")
	between := addNote(t, svc, head, "big.go", 15, "between the hunks")
	untouched := addNote(t, svc, head, "other.go", 1, "file not in the PR")
	p, err := svc.planSend(context.Background(), PRSendRequest{PR: 7, Notes: []string{inHunk, between, untouched}})
	if err != nil {
		t.Fatal(err)
	}
	if p.Mode != engine.SendReview || p.Verdict || p.Target != "o/r #7" || p.PRID != "PR_7" || p.Head != head {
		t.Fatalf("plan = %+v", p)
	}
	if len(p.Items) != 2 || len(p.Skipped) != 1 {
		t.Fatalf("items %+v skipped %+v", p.Items, p.Skipped)
	}
	if th := p.Items[0].Thread; p.Items[0].Key != inHunk || th.Path != "big.go" || th.Line != 5 || th.Side != model.NoteSideNew {
		t.Errorf("line thread = %+v", p.Items[0])
	}
	if th := p.Items[1].Thread; th.Line != 0 || !strings.HasPrefix(th.Body, "> Line 15:") {
		t.Errorf("file-level thread = %+v", th)
	}
	if p.Skipped[0].Reason != "not in this PR" {
		t.Errorf("skip = %+v", p.Skipped[0])
	}
}

func TestPlanSendIgnoresTheUsersDiffContext(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	runGitIn(t, svc.repoDirForTest(), "config", "diff.context", "10") // would merge the two hunks
	between := addNote(t, svc, head, "big.go", 15, "between")
	p, err := svc.planSend(context.Background(), PRSendRequest{PR: 7, Notes: []string{between}})
	if err != nil {
		t.Fatal(err)
	}
	if p.Items[0].Thread.Line != 0 {
		t.Fatalf("line 15 is outside GitHub's 3-line hunks: %+v", p.Items[0].Thread)
	}
}

func TestPlanSendRefusesAMovedHead(t *testing.T) {
	t.Parallel()
	svc, ff, _ := sendRepo(t)
	ff.mu.Lock()
	pr := ff.byNum[7]
	pr.HeadSHA = strings.Repeat("e", 40)
	ff.byNum[7] = pr
	ff.mu.Unlock()
	if _, err := svc.planSend(context.Background(), PRSendRequest{PR: 7, Mine: true}); err == nil || !strings.Contains(err.Error(), "new commits") {
		t.Fatalf("err = %v", err)
	}
}

func TestPRSendOpEndToEndDeletesTheSentNote(t *testing.T) {
	t.Parallel()
	svc, ff, head := sendRepo(t)
	id := addNote(t, svc, head, "big.go", 5, "in the hunk")
	keep := addNote(t, svc, head, "other.go", 1, "not in the PR")
	ctx := context.Background()
	op, err := svc.PRSendOp(ctx, PRSendRequest{PR: 7, Notes: []string{id, keep}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Execute(ctx, op, nil, engine.MapDecider{engine.DecisionSendForge: engine.OptSend}); err != nil {
		t.Fatal(err)
	}
	if got := ff.writeLog(); !strings.Contains(got, "StartReview") || !strings.Contains(got, "SubmitReview COMMENT") {
		t.Fatalf("writes = %s", got)
	}
	if _, ok := noteByID(t, svc, id); ok {
		t.Error("the sent note is still local")
	}
	if n, ok := noteByID(t, svc, keep); !ok || n.Send != nil {
		t.Errorf("the skipped note must stay local and unstamped: %+v %v", n.Send, ok)
	}
}

func TestPRSendOpNeedsAWritableForge(t *testing.T) {
	t.Parallel()
	svc, _, _ := sendRepo(t)
	svc.SetForgeProviders([]forge.Provider{readOnlyForge{&fakeForge{}}})
	if _, err := svc.PRSendOp(context.Background(), PRSendRequest{PR: 7, Mine: true}); err == nil {
		t.Fatal("a provider with no Writer cannot send")
	}
}

func TestPlanSendWholeReviewKeysEveryRemark(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	ctx := context.Background()
	doc := `{"version":1,"summary":"looks fine","files":[{"path":"big.go","annotations":[
 {"newRange":[5,5],"summary":"check this"},{"newRange":[25,25],"summary":"and this"}]}]}`
	rid, _, err := svc.SaveReview(ctx, SaveReview{Target: ReviewTarget{Kind: ReviewRange, Range: head + "^.." + head, Label: "feat"},
		Agent: "claude", Text: doc})
	if err != nil {
		t.Fatal(err)
	}
	p, err := svc.planSend(ctx, PRSendRequest{PR: 7, Review: rid})
	if err != nil {
		t.Fatal(err)
	}
	if p.Key != rid || !p.Verdict || len(p.Items) != 2 || !strings.HasPrefix(p.Items[0].Key, "remark:"+rid+":") ||
		!strings.Contains(p.Body, "looks fine") || !strings.Contains(p.Body, "— claude via gg") {
		t.Fatalf("plan = %+v", p)
	}
}
```

Test support in `forge_test.go`: `fakeForge` gains a writer that simulates GitHub (`StartReview` → `"PRR_w"`; `AddThread` → `PRRT_w<k>` / `PRRC_w<k>` kept pending; `Reply` likewise; `SubmitReview` moves the review's pending comments into `f.comments` with `ReviewID` set and logs `SubmitReview <EVENT>`; `DeletePendingReview` drops them; `Resolve`/`Unresolve` flip `Resolved` on the thread's comments), a `writeLog()` accessor, and `Snapshot` so `PRRevalidate` takes the one-call path. `readOnlyForge{forge.Provider}` wraps a provider to hide its `Writer`. `svc.repoDirForTest()` returns the repo's top level (`TopLevel(ctx)`), or pass `dir` back from `sendRepo` instead.

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/domain/ -run 'SendBody|PlanSend|PRSendOp' -count=1`
Expected: FAIL — `undefined: sendBody`, `undefined: PRSendRequest`.

- [ ] **Step 3: Implement `DiffSpec.Unified`**

`model.DiffSpec` gains `Unified int // > 0: -U<n> (a forge's hunks are git's default 3, whatever diff.context says)`. `git.Repo.DiffPatch` adds `.ArgIf(spec.Unified > 0, "-U"+strconv.Itoa(spec.Unified))` after `--no-color`; `domain.DiffPatch`'s cache key appends `":U"+strconv.Itoa(spec.Unified)`.

- [ ] **Step 4: Implement `forge_send_body.go`**

```go
package domain

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/forge"
	"github.com/homeend/gigagit/internal/model"
)

// sendBody is what gg posts for one note (spec §3.6): an optional quote
// (a file-level thread standing in for a line outside the diff), the
// summary, the rationale — folded into <details> with the tags and the
// confidence when an agent wrote it, which also signs it — and the marker.
func sendBody(n model.Note, key, quote string) string {
	var b strings.Builder
	b.WriteString(quote)
	b.WriteString(strings.TrimSpace(n.Summary))
	agent := n.Source == model.NoteSourceAgent
	rat := strings.TrimSpace(n.Rationale)
	switch {
	case agent && (rat != "" || len(n.Tags) > 0 || n.Confidence > 0):
		b.WriteString("\n\n<details><summary>Details</summary>\n\n")
		if rat != "" {
			b.WriteString(rat + "\n\n")
		}
		for _, t := range n.Tags {
			b.WriteString("- " + t + "\n")
		}
		if n.Confidence > 0 {
			b.WriteString("\nconfidence: " + strconv.FormatFloat(n.Confidence, 'g', -1, 64) + "\n")
		}
		b.WriteString("\n</details>")
	case rat != "":
		b.WriteString("\n\n" + rat)
	}
	if agent {
		who := strings.TrimSpace(n.Author)
		if who == "" {
			who = "agent"
		}
		b.WriteString("\n\n— " + who + " via gg")
	}
	b.WriteString("\n\n" + forge.SendMarker(key))
	return b.String()
}

// quoteLines opens a file-level thread with the lines it is really about.
func quoteLines(line int, text []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "> Line %d:\n> ```\n", line)
	for _, l := range text {
		b.WriteString("> " + l + "\n")
	}
	b.WriteString("> ```\n\n")
	return b.String()
}

// reviewSendBody is a whole review's GitHub body: its overview (or its
// prose), signed by its agent, marked with the review note's id.
func reviewSendBody(r Review) string {
	text := r.Text
	if r.Doc != nil {
		text = r.Doc.Overview
	}
	n := model.Note{Source: model.NoteSourceAgent, Author: r.Agent, Summary: strings.TrimSpace(text)}
	return sendBody(n, r.ID, "")
}
```

(The agent `Details` fold uses `Rationale` only for remarks and agent notes; a user note's rationale stays plain markdown under the summary.)

- [ ] **Step 5: Implement `forge_send.go`**

```go
package domain

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/forge"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notebatch"
)

var (
	ErrPRHeadMoved       = errors.New("the pull request has new commits on GitHub: fetch them first (gg pr fetch)")
	ErrForgeReadOnly     = errors.New("this forge cannot be written to from gg")
	ErrNoInterruptedSend = errors.New("no interrupted send: gg has nothing waiting in a pending review")
	ErrMixedSend         = errors.New("send new comments and replies / resolves separately")
	ErrSendRequest       = errors.New("send request")
)

// (PRSendRequest as in the Interfaces block)

// PRSendOp builds the one op that writes to a forge. The frontend collected
// every input already; building the op re-reads the PR, settles, re-anchors
// and renders the bodies (R12: outside the gate, right before Execute); the
// op confirms and writes.
func (s *Service) PRSendOp(ctx context.Context, req PRSendRequest) (engine.SendToForge, error) {
	p, err := s.provider(ctx)
	if err != nil {
		return engine.SendToForge{}, err
	}
	w, ok := p.(forge.Writer)
	if !ok {
		return engine.SendToForge{}, ErrForgeReadOnly
	}
	plan, err := s.planSend(ctx, req) // outside the gate (R12): every read here reserves it
	if err != nil {
		return engine.SendToForge{}, err
	}
	return engine.SendToForge{
		Plan:   plan,
		Writer: w,
		Ledger: s.sendLedger(req.PR),
		Now:    s.forgeClock,
	}, nil
}

func (s *Service) planSend(ctx context.Context, req PRSendRequest) (engine.SendPlan, error) {
	rv, err := s.PRRevalidate(ctx, req.PR) // also settles what earlier sends left
	if err != nil {
		return engine.SendPlan{}, err
	}
	if rv.Moved {
		return engine.SendPlan{}, ErrPRHeadMoved
	}
	pr := rv.PR
	plan := engine.SendPlan{Target: s.prTarget(ctx, pr.Number), PR: pr.Number, PRID: pr.NodeID, Head: pr.HeadSHA,
		OwnPR: pr.ViewerDidAuthor, Pending: pr.ViewerPendingReview, Body: req.Body}
	switch {
	case req.Finish || req.Discard:
		rev, keys := s.PRInterrupted(ctx, pr.Number)
		if rev == "" {
			return engine.SendPlan{}, ErrNoInterruptedSend
		}
		plan.Mode, plan.Pending, plan.Body = engine.SendFinish, rev, ""
		if req.Discard {
			plan.Mode = engine.SendDiscard
		}
		for _, k := range keys {
			plan.Items = append(plan.Items, engine.SendItem{Key: k, Label: k + " (waiting in the pending review)"})
		}
		return plan, nil
	case len(req.Resolve)+len(req.Unresolve) > 0 || s.onlyDrafts(ctx, req.Notes):
		if req.Review != "" || req.Mine || req.Verdict || !s.onlyDrafts(ctx, req.Notes) {
			return engine.SendPlan{}, ErrMixedSend
		}
		return s.planActions(ctx, plan, req)
	}
	return s.planReview(ctx, plan, pr, req)
}
```

with these helpers in the same file (each small; the executor writes them out in full):

- `prTarget(ctx, n) string` — `strings.TrimPrefix(slug, "github.com/") + " #" + n` from `s.baseRepo`, falling back to `"#<n>"`.
- `onlyDrafts(ctx, ids) bool` — every id names a stored `IsForgeReply` note (and there is at least one).
- `planActions` — one `SendReply` per draft (`ThreadID` = `forgeCommentByID(parent).ThreadID`, `Body` = `sendBody(note, note.ID, "")`, `Label` = `"reply: " + summary`), one `SendResolve` / `SendUnresolve` per id (`threadIDFor(n, id)`: a `PRRT_…` thread id as is, a comment id → its `ThreadID`; unknown → `fmt.Errorf("%w: %s is not a thread of #%d", ErrSendRequest, id, n)`).
- `planReview`:
  1. A verdict-only request (no `Notes`, no `Review`, not `Mine`) needs no diff: return the plan with `Verdict` and the body at once. Otherwise `prev, err := s.PRPreview(ctx, pr)`; not `PreviewOK` → `fmt.Errorf("%w: #%d's diff is not available here (gg pr fetch %d)", ErrSendRequest, …)`.
  2. `hunks := s.sendHunks(ctx, prev.Set)` — `DiffHunks(DiffSpec{Rev: set.Base+".."+set.Tip, Unified: 3})` as `map[path][]model.Hunk` plus `changed map[path]bool` from `prev.Files` (status ≠ `D`).
  3. `shown, _ := s.PreviewNotesAll(ctx, prev.Set)` → index roots by id.
  4. For each requested id (and for `--mine` every shown root with `Group == GroupMine` and `!Note.IsForgeReply()`, and for `--review` every unmoved remark): build the item with `s.noteItem(rn, hunks, changed, tip lines)` or `s.remarkItem(ctx, r, i, prev.Set, hunks, changed)`, or a `SendSkip`.
  5. `--review`: `plan.Key = r.ID`, `plan.Body = reviewSendBody(r)`, `plan.Verdict = true`; `--mine` and a verdict-only request: `plan.Verdict = true`, `plan.Body = req.Body` (+ `"\n\n— <agent> via gg"` when `req.Agent != ""`).
  6. Unknown ids → `fmt.Errorf("%w: no local note %s in #%d", ErrSendRequest, id, n)`; a forge id → `fmt.Errorf("%w: %s is a GitHub comment: answer it with gg pr reply", ErrSendRequest, id)`.

```go
// placeThread reports whether rng lies inside ONE hunk on side — the lines
// GitHub accepts a line comment on.
func placeThread(hunks []model.Hunk, side model.NoteSide, rng [2]int) bool {
	for _, h := range hunks {
		r := h.New
		if side == model.NoteSideOld {
			r = h.Old
		}
		if r != [2]int{0, 0} && rng[0] >= r[0] && rng[1] <= r[1] {
			return true
		}
	}
	return false
}

// threadFor turns an anchored range into a forge thread: a line thread in
// the diff, a file-level one quoting the lines outside it.
func threadFor(path string, side model.NoteSide, rng [2]int, text []string, hunks []model.Hunk,
	body func(quote string) string) forge.Thread {
	if placeThread(hunks, side, rng) {
		return forge.Thread{Path: path, Line: rng[1], StartLine: rng[0], Side: side, Body: body("")}
	}
	return forge.Thread{Path: path, Body: body(quoteLines(rng[0], text))}
}
```

`remarkItem` finds the remark's text: `lines := s.revLines(ctx, tip, path)` (the review's tip for the new side, `reviewRevs`' base for the old side; a working review reads `s.worktreeLines`), `want := anchorLines(lines, dn.Range)`, then `findAnchor(s.revLines(ctx, set.Tip|set.Base, path), model.NoteContextHash(want), span, dn.Range[0])` — 0 = skip "its lines changed". The remark's `model.Note` for `sendBody` is `{Source: agent, Author: r.Agent, Summary: dn.Summary, Rationale: dn.Rationale, Tags: meta "k: v"}`; its replies are `RemarkThreads()[i].Replies` (each `SendReplyBody{Key: reply.ID, Body: sendBody(reply, reply.ID, "")}`), `Resolve` = that thread's `Resolution != nil`.

`noteItem` uses the RESOLVED range (`rn.Range`) and status: `NoteStale` → skip "its lines changed"; path not in `changed` → skip "not in this PR"; `rn.Sync == model.SyncSending` → skip "already being sent". Its replies: `rn.Replies` (stored, not drafts), its resolution: `rn.Resolution != nil`. The label is `fmt.Sprintf("%s:%d %s", path, rng[0], cut(summary, 60))` (`"%s (file) %s"` for a file-level thread).

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/domain/ ./internal/git/ ./internal/model/ -count=1`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
gg add internal/domain internal/model internal/git
git commit -m "feat(forge): the send planner — anchoring at GitHub's hunks, bodies, PRSendOp"
```

---

### Task 7: CLI — `gg pr send | reply | resolve | unresolve | notes`

**Files:**
- Create: `internal/cli/prsend.go`
- Create: `internal/cli/prsend_test.go`
- Modify: `internal/cli/pr.go` (dispatch the write verbs before the read parser; usage; thread ids on `gg pr comments` roots)
- Modify: `internal/cli/cli.go` (`cmdPR(svc, rest, stdin, stdout, stderr)`)
- Modify: `internal/domain/forge_send.go` (`PRThreadRoot`, `PRNotes`)

**Interfaces:**
- Consumes: `PRSendOp`, `PRSendRequest`, `ErrPRHeadMoved` (Task 6); `PRInterrupted` (Task 5); `NoteReply` with a forge parent (Task 3); `engine.DecisionSendForge`, `engine.Opt*` (Task 4); `runOperation`, `cliDecider`, `stdinIsTerminal`, `renderNoteLine`.
- Produces:
  ```go
  // domain
  func (s *Service) PRThreadRoot(ctx context.Context, n int, id string) (rootCommentID, threadID string, err error) // id: a thread id, a comment id, or "forge:<comment id>"
  func (s *Service) PRNotes(ctx context.Context, n int) (map[string][]ResolvedNote, error)                   // PreviewNotesAll over the PR's set
  // cli
  func prSend(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int
  func prReply(…) int; func prResolve(svc, resolve bool, …) int; func prNotes(…) int
  func runPRSend(ctx context.Context, svc *domain.Service, req domain.PRSendRequest, yes bool, answer string, stdin io.Reader, stdout, stderr io.Writer) int
  ```

CLI surface (`prUsage` gains these lines):

```
       gg pr send <n> (--note <id>… | --review <id> | --mine | --verdict) [--event comment|approve|request-changes] [--body <text>] [--yes]
       gg pr send <n> --finish | --discard [--yes]
       gg pr reply <n> <thread-or-comment-id> <text> [--send [--yes]]
       gg pr resolve|unresolve <n> <thread-or-comment-id>
       gg pr notes <n> [--json]
```

- `--yes` answers the confirm with `--event` (default `comment`) for `--review`/`--mine`/`--verdict`, `send` for notes and replies, `discard` for `--discard`; never `submit-with-pending` (R6). Without `--yes` a terminal is asked; a pipe gets cliDecider's "needs a decision … rerun with the matching flag" (the hint names `--yes`).
- A moved head (`PRRevalidate(…).Moved`) is fetched first with `PRFetchOp` (R5), with a progress line.
- `--note` repeats; `--note` takes a stored note id, a draft reply id, or a remark id `review:<id>:<n>`.
- Exit codes: 0 sent; 1 an error or a partial failure (the summary still printed); 2 usage.

- [ ] **Step 1: Write the failing tests** (`internal/cli/prsend_test.go`)

```go
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/forge/forgetest"
)

// sendPRRepo: main holds big.go (30 lines); the PR #7 changes line 5;
// refs/gg/pr/7 is its head; the fake gh answers the PR, its (empty) threads
// and — once a review was submitted — the thread PRRT_new1 it created.
// Serial: env.
func sendPRRepo(t *testing.T) (dir, head, fixtures string) {
	t.Helper()
	dir = newRepoDir(t)
	var lines []string
	for i := 1; i <= 30; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	writeCommit(t, dir, "big.go", strings.Join(lines, "\n")+"\n", "big")
	gitIn(t, dir, "checkout", "-q", "-b", "feat")
	lines[4] = "line 5 changed"
	writeCommit(t, dir, "big.go", strings.Join(lines, "\n")+"\n", "change")
	head = strings.TrimSpace(gitIn(t, dir, "rev-parse", "HEAD"))
	gitIn(t, dir, "update-ref", "refs/gg/pr/7", head)
	gitIn(t, dir, "checkout", "-q", "main")
	fixtures = filepath.Join(dir, ".git", "fakegh")
	t.Setenv(forgetest.EnvBin, forgetest.BuildFakeGH(t))
	t.Setenv(forgetest.EnvFixtures, fixtures)
	pr := func(threads string) string {
		return fmt.Sprintf(`{"data":{"repository":{"pullRequest":{"id":"PR_7","number":7,"title":"t","body":"","author":{"login":"ann"},
"state":"OPEN","isDraft":false,"reviewDecision":"","headRefName":"feat","isCrossRepository":false,"headRepositoryOwner":{"login":"ann"},
"headRepository":{"name":"r"},"baseRefName":"main","baseRefOid":"","headRefOid":"%s","url":"https://github.com/o/r/pull/7",
"createdAt":"2026-10-01T10:00:00Z","updatedAt":"2026-10-06T10:00:00Z","viewerDidAuthor":false,"viewerLatestReview":null,
"reviewThreads":{"pageInfo":{"hasNextPage":false},"nodes":[%s]},"comments":{"pageInfo":{"hasNextPage":false},"nodes":[]},
"reviews":{"pageInfo":{"hasNextPage":false},"nodes":[]}}}}}`, head, threads)
	}
	sent := `{"id":"PRRT_new1","path":"big.go","line":5,"startLine":null,"originalLine":5,"originalStartLine":null,"diffSide":"RIGHT",
"subjectType":"LINE","isResolved":false,"isOutdated":false,"comments":{"pageInfo":{"hasNextPage":false},"nodes":[{"id":"PRRC_new1",
"replyTo":null,"author":{"login":"me"},"body":"x","diffHunk":"","createdAt":"2026-10-07T10:00:00Z","updatedAt":"2026-10-07T10:00:00Z",
"pullRequestReview":{"id":"PRR_new"}}]}}`
	view := fmt.Sprintf(`{"number":7,"title":"t","author":{"login":"ann"},"state":"OPEN","isDraft":false,"headRefName":"feat",
"isCrossRepository":false,"baseRefName":"main","baseRefOid":"","headRefOid":"%s","url":"https://github.com/o/r/pull/7",
"createdAt":"2026-10-01T10:00:00Z","updatedAt":"2026-10-06T10:00:00Z","body":""}`, head)
	forgetest.Seed(t, fixtures, map[string]string{
		"snapshot-7.json": pr(""), "snapshot-7-sent.json": pr(sent), "pr-view-7.json": view,
		"pr-list.json": "[]", "repo-view.json": `{"nameWithOwner":"o/r","url":"https://github.com/o/r","sshUrl":"git@github.com:o/r.git"}`,
	})
	return dir, head, fixtures
}

func addCLINote(t *testing.T, dir, head string, line int) string {
	t.Helper()
	var out, errb strings.Builder
	code := Run(dir, []string{"note", "add", "--rev", head, "--file", "big.go", "--new-line", fmt.Sprint(line),
		"--summary", "look here", "--source", "user", "--json"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("note add: %s", errb.String())
	}
	var n struct{ ID string }
	if err := json.Unmarshal([]byte(out.String()), &n); err != nil || n.ID == "" {
		t.Fatalf("note add json %q: %v", out.String(), err)
	}
	return n.ID
}

func TestPRSendNoteWithYes(t *testing.T) {
	dir, head, fixtures := sendPRRepo(t)
	id := addCLINote(t, dir, head, 5)
	out, errs, code := runPR(t, dir, "send", "7", "--note", id, "--yes")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	if !strings.Contains(out, "sent 1 comments to o/r #7") {
		t.Errorf("stdout = %q", out)
	}
	var ops []string
	for _, w := range forgetest.Writes(t, fixtures) {
		ops = append(ops, w.Op)
	}
	if strings.Join(ops, ",") != "StartReview,AddThread,SubmitReview" {
		t.Fatalf("writes = %v", ops)
	}
	notes, _, _ := runPR(t, dir, "notes", "7", "--json")
	if strings.Contains(notes, id) {
		t.Errorf("the sent note is still local:\n%s", notes)
	}
}

func TestPRSendWithoutYesInAPipeAsks(t *testing.T) {
	dir, head, fixtures := sendPRRepo(t)
	id := addCLINote(t, dir, head, 5)
	_, errs, code := runPR(t, dir, "send", "7", "--note", id)
	if code == 0 || !strings.Contains(errs, "forge.send") || !strings.Contains(errs, "--yes") {
		t.Fatalf("exit %d, stderr %q", code, errs)
	}
	if ws := forgetest.Writes(t, fixtures); len(ws) != 0 {
		t.Fatalf("nothing may be written before the confirm: %v", ws)
	}
}

func TestPRSendFailureKeepsTheNoteWithItsError(t *testing.T) {
	dir, head, fixtures := sendPRRepo(t)
	id := addCLINote(t, dir, head, 5)
	if err := os.WriteFile(filepath.Join(fixtures, "fail-SubmitReview"), []byte("HTTP 502: Bad Gateway"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, errs, code := runPR(t, dir, "send", "7", "--note", id, "--yes")
	if code != 1 || !strings.Contains(errs, "HTTP 502") {
		t.Fatalf("exit %d, stderr %q", code, errs)
	}
	ops := forgetest.Writes(t, fixtures)
	if ops[len(ops)-1].Op != "DeleteReview" {
		t.Fatalf("the pending review must be deleted: %v", ops)
	}
	notes, _, _ := runPR(t, dir, "notes", "7", "--json")
	if !strings.Contains(notes, id) || !strings.Contains(notes, `"sync":"failed"`) {
		t.Fatalf("the note stays local, failed:\n%s", notes)
	}
}

func TestPRSendUsage(t *testing.T) {
	dir, _, _ := sendPRRepo(t)
	for _, args := range [][]string{
		{"send"}, {"send", "x"}, {"send", "7"}, {"send", "7", "--mine", "--review", "r1"},
		{"send", "7", "--event", "maybe", "--mine"}, {"send", "7", "--finish", "--discard"},
	} {
		if _, _, code := runPR(t, dir, args...); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
}

func TestPRResolveIsImmediate(t *testing.T) {
	dir, _, fixtures := sendPRRepo(t)
	// Seed a thread to resolve.
	b, _ := os.ReadFile(filepath.Join(fixtures, "snapshot-7-sent.json"))
	forgetest.Seed(t, fixtures, map[string]string{"snapshot-7.json": string(b)})
	out, errs, code := runPR(t, dir, "resolve", "7", "PRRT_new1")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	ws := forgetest.Writes(t, fixtures)
	if len(ws) != 1 || ws[0].Op != "Resolve" || ws[0].Vars["thread"] != "PRRT_new1" || !strings.Contains(out, "sent 1 actions") {
		t.Fatalf("writes %v out %q", ws, out)
	}
}

func TestPRReplyDraftThenSend(t *testing.T) {
	dir, _, fixtures := sendPRRepo(t)
	b, _ := os.ReadFile(filepath.Join(fixtures, "snapshot-7-sent.json"))
	forgetest.Seed(t, fixtures, map[string]string{"snapshot-7.json": string(b)})
	out, errs, code := runPR(t, dir, "reply", "7", "PRRT_new1", "on it")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	if ws := forgetest.Writes(t, fixtures); len(ws) != 0 {
		t.Fatalf("a draft reply writes nothing: %v", ws)
	}
	id := strings.Fields(out)[0] // "<id> draft reply to PRRT_new1"
	if _, errs, code := runPR(t, dir, "send", "7", "--note", id, "--yes"); code != 0 {
		t.Fatalf("send the draft: %s", errs)
	}
	ws := forgetest.Writes(t, fixtures)
	if len(ws) != 1 || ws[0].Op != "Reply" || ws[0].Vars["thread"] != "PRRT_new1" || ws[0].Vars["review"] != nil {
		t.Fatalf("writes = %+v", ws)
	}
}

func TestPRCommentsShowsThreadIDs(t *testing.T) {
	dir, _, fixtures := sendPRRepo(t)
	b, _ := os.ReadFile(filepath.Join(fixtures, "snapshot-7-sent.json"))
	forgetest.Seed(t, fixtures, map[string]string{"snapshot-7.json": string(b), "threads-7.json": string(b)})
	out, _, _ := runPR(t, dir, "comments", "7")
	if !strings.Contains(out, "[PRRT_new1] big.go:5 (new) me: x") {
		t.Fatalf("comments = %q", out)
	}
}
```

(`newRepoDir`, `writeCommit`, `gitIn` — use the cli package's existing repo helpers; check their names in `cli_test.go`/`helpers_test.go` and adapt.)

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/cli/ -run 'PRSend|PRResolve|PRReply|PRCommentsShowsThread' -count=1`
Expected: FAIL — `gg pr send` is a usage error today (exit 2) and the comments line has no thread id.

- [ ] **Step 3: Domain helpers** (append to `forge_send.go`)

```go
// PRThreadRoot names a thread of PR n by any of its handles — a thread id,
// one of its comment ids, or "forge:<comment id>" — reading the PR's
// comments when they are not cached yet.
func (s *Service) PRThreadRoot(ctx context.Context, n int, id string) (string, string, error) {
	id = strings.TrimPrefix(strings.TrimSpace(id), model.ForgeNoteIDPrefix)
	// The PR itself too: a draft reply is addressed at its head
	// (forgeReplyDraft reads PRDetailsCached).
	if _, err := s.PullRequest(ctx, n); err != nil {
		return "", "", err
	}
	cs, err := s.PRComments(ctx, n)
	if err != nil {
		return "", "", err
	}
	for _, bucket := range [][]model.ForgeComment{cs.Inline, cs.Outdated} {
		for _, c := range bucket {
			if (c.ID == id || c.ThreadID == id) && c.ParentID == "" {
				return c.ID, c.ThreadID, nil
			}
			if c.ID == id { // a reply: its root shares its thread
				for _, r := range bucket {
					if r.ThreadID == c.ThreadID && r.ParentID == "" {
						return r.ID, r.ThreadID, nil
					}
				}
			}
		}
	}
	return "", "", fmt.Errorf("%w: %s is not a thread of #%d", ErrSendRequest, id, n)
}

// PRNotes is everything PR n's view holds, by path: local notes (carried
// ones too, Task 8), GitHub threads, draft replies — each with its sync
// state. The PR's diff must be available here (gg pr fetch).
func (s *Service) PRNotes(ctx context.Context, n int) (map[string][]ResolvedNote, error) {
	pr, err := s.PullRequest(ctx, n)
	if err != nil {
		return nil, err
	}
	prev, err := s.PRPreview(ctx, pr)
	if err != nil {
		return nil, err
	}
	if !prev.Set.OK() {
		return nil, fmt.Errorf("%w: #%d's diff is not available here (gg pr fetch %d)", ErrSendRequest, n, n)
	}
	if _, err := s.PRComments(ctx, n); err != nil {
		return nil, err
	}
	return s.PreviewNotesAll(ctx, prev.Set)
}
```

- [ ] **Step 4: Implement `internal/cli/prsend.go`**

```go
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

// multiFlag is a repeatable string flag (--note a --note b).
type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

// prNumber parses "7" or "#7".
func prNumber(s string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(s), "#"))
	return n, err == nil && n > 0
}

func prSend(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	usage := func(msg string) int {
		if msg != "" {
			fmt.Fprintln(stderr, "error:", msg)
		}
		fmt.Fprintln(stderr, prUsage)
		return 2
	}
	if len(args) == 0 {
		return usage("")
	}
	n, ok := prNumber(args[0])
	if !ok {
		return usage(fmt.Sprintf("%q is not a pull request number", args[0]))
	}
	fs := flag.NewFlagSet("pr send", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var notes multiFlag
	fs.Var(&notes, "note", "a local note, draft reply or review remark id (repeatable)")
	review := fs.String("review", "", "send a stored review: every unsent remark, its summary as the body")
	mine := fs.Bool("mine", false, "send every local note the PR shows as one review")
	verdict := fs.Bool("verdict", false, "a verdict with no comments")
	event := fs.String("event", "", "comment, approve or request-changes (with --yes)")
	body := fs.String("body", "", "the review body")
	yes := fs.Bool("yes", false, "answer the confirm (never inside a gg session)")
	finish := fs.Bool("finish", false, "submit the review an interrupted send left pending")
	discard := fs.Bool("discard", false, "delete the review an interrupted send left pending")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
		return usage("")
	}
	kinds := 0
	for _, on := range []bool{len(notes) > 0, *review != "", *mine, *verdict, *finish, *discard} {
		if on {
			kinds++
		}
	}
	if kinds != 1 {
		return usage("name exactly one of --note, --review, --mine, --verdict, --finish, --discard")
	}
	switch *event {
	case "", engine.OptComment, engine.OptApprove, engine.OptRequestChanges:
	default:
		return usage(fmt.Sprintf("--event %q: want comment, approve or request-changes", *event))
	}
	req := domain.PRSendRequest{PR: n, Review: *review, Mine: *mine, Notes: notes, Verdict: *verdict,
		Body: *body, Finish: *finish, Discard: *discard}
	answer := engine.OptSend
	switch {
	case *review != "" || *mine || *verdict:
		answer = engine.OptComment
		if *event != "" {
			answer = *event
		}
	case *discard:
		answer = engine.OptDiscard
	}
	return runPRSend(context.Background(), svc, req, *yes, answer, stdin, stdout, stderr)
}

// runPRSend fetches a moved head, then runs the op. yes answers the confirm
// with answer; otherwise a terminal is asked and a pipe gets the decision
// error.
func runPRSend(ctx context.Context, svc *domain.Service, req domain.PRSendRequest, yes bool, answer string,
	stdin io.Reader, stdout, stderr io.Writer) int {
	if st := svc.ForgeStatus(ctx); !st.Available() {
		fmt.Fprintf(stderr, "gg pr: %v\n", st.Err)
		return 1
	}
	if rv, err := svc.PRRevalidate(ctx, req.PR); err == nil && rv.Moved {
		fmt.Fprintf(stderr, "#%d has new commits: fetching them first\n", req.PR)
		fop, err := svc.PRFetchOp(ctx, req.PR)
		if err == nil {
			_, err = runOperation(ctx, svc, fop, cliDecider{}, stderr)
		}
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
	}
	op, err := svc.PRSendOp(ctx, req)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	dec := cliDecider{in: stdin, out: stderr, interactive: stdinIsTerminal()}
	if yes {
		dec.policy = map[string]string{engine.DecisionSendForge: answer}
	}
	res, err := runOperation(ctx, svc, op, dec, stderr)
	if res.Summary != "" {
		fmt.Fprintln(stdout, res.Summary)
	}
	if err != nil {
		if errors.Is(err, engine.ErrDecisionRequired) || strings.Contains(err.Error(), "needs a decision") {
			fmt.Fprintln(stderr, "(rerun with --yes to send without being asked)")
		}
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

func prReply(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pr reply", flag.ContinueOnError)
	fs.SetOutput(stderr)
	send := fs.Bool("send", false, "send the reply at once")
	yes := fs.Bool("yes", false, "answer the confirm")
	source := fs.String("source", "user", "user or agent")
	pos, flags := splitPositionals(args, 3)
	if err := fs.Parse(flags); err != nil || len(pos) != 3 {
		fmt.Fprintln(stderr, prUsage)
		return 2
	}
	n, ok := prNumber(pos[0])
	if !ok {
		fmt.Fprintln(stderr, prUsage)
		return 2
	}
	ctx := context.Background()
	root, thread, err := svc.PRThreadRoot(ctx, n, pos[1])
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	src, err := noteSourceValue(*source)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 2
	}
	summary, rationale, _ := strings.Cut(strings.TrimSpace(pos[2]), "\n")
	d, err := svc.NoteReply(ctx, "forge:"+root, noteFromText(src, summary, rationale))
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	fmt.Fprintf(stdout, "%s draft reply to %s\n", d.ID, thread)
	if !*send {
		return 0
	}
	return runPRSend(ctx, svc, domain.PRSendRequest{PR: n, Notes: []string{d.ID}}, *yes, engine.OptSend, stdin, stdout, stderr)
}

func prResolve(svc *domain.Service, resolve bool, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) != 2 {
		fmt.Fprintln(stderr, prUsage)
		return 2
	}
	n, ok := prNumber(args[0])
	if !ok {
		fmt.Fprintln(stderr, prUsage)
		return 2
	}
	req := domain.PRSendRequest{PR: n}
	if resolve {
		req.Resolve = []string{args[1]}
	} else {
		req.Unresolve = []string{args[1]}
	}
	return runPRSend(context.Background(), svc, req, false, engine.OptSend, stdin, stdout, stderr)
}

func prNotes(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pr notes", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "emit the wire notes as a JSON array")
	pos, flags := splitPositionals(args, 1)
	if err := fs.Parse(flags); err != nil || len(pos) != 1 {
		fmt.Fprintln(stderr, prUsage)
		return 2
	}
	n, ok := prNumber(pos[0])
	if !ok {
		fmt.Fprintln(stderr, prUsage)
		return 2
	}
	byPath, err := svc.PRNotes(context.Background(), n)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	var wires []domain.WireNote
	for _, p := range domain.PreviewNotePaths(byPath) {
		for _, r := range byPath[p] {
			if *asJSON {
				wires = append(wires, domain.ToWireNotePreview(r, true))
				continue
			}
			renderNoteLine(stdout, r, false, noteStatusWord(r, true))
			for _, rep := range r.Replies {
				renderNoteLine(stdout, rep, true, noteStatusWord(rep, true))
			}
		}
	}
	if *asJSON {
		if wires == nil {
			wires = []domain.WireNote{}
		}
		return jsonOut(stdout, stderr, wires)
	}
	return 0
}
```

`splitPositionals(args, k)` pulls the first k non-flag arguments (in order) and returns the rest as flags, so `gg pr reply 7 T1 "text" --send` and `gg pr reply --send 7 T1 "text"` both parse; `noteFromText(src, summary, rationale) model.Note` builds the draft; `jsonOut` encodes like `pr.go`'s `emit`. Wire the dispatch at the top of `cmdPR`:

```go
	if len(args) > 0 {
		switch args[0] {
		case "send":
			return prSend(svc, args[1:], stdin, stdout, stderr)
		case "reply":
			return prReply(svc, args[1:], stdin, stdout, stderr)
		case "resolve", "unresolve":
			return prResolve(svc, args[0] == "resolve", args[1:], stdin, stdout, stderr)
		case "notes":
			return prNotes(svc, args[1:], stdout, stderr)
		}
	}
```

`cmdPR`'s doc loses "never writes to the forge": "The read verbs list, read, fetch and forget; the write verbs (send, reply, resolve, unresolve) post through one confirmed op." `printPRComments` prefixes a thread root with `[<ThreadID>] ` when it has one.

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/cli/ -count=1`
Expected: PASS (the existing `pr` tests included).

- [ ] **Step 6: Commit**

```bash
gg add internal/cli internal/domain
git commit -m "feat(cli): gg pr send, reply, resolve, unresolve and notes"
```

---

### Task 8: domain — carried notes in a PR's view (§1.4)

**Files:**
- Create: `internal/domain/forge_carried.go`
- Create: `internal/domain/forge_carried_test.go`
- Modify: `internal/domain/previewnotes.go` (`PreviewNotesFor`, `PreviewNotesAt`, `PreviewNotesAll` append a PR set's carried notes)
- Modify: `internal/domain/service.go` (the memory cache field), `internal/domain/notes.go` (`invalidateNoteCounts` drops it)

**Interfaces:**
- Consumes: `ResolvedNote.Origin`, `syncOf`, `GroupMine` (Task 3); `DiffSpec.Unified` (Task 6); `findAnchor`, `splitLines`, `git.ParsePRRef`.
- Produces:
  ```go
  func (s *Service) carriedNotes(ctx context.Context, set PreviewNoteSet) map[string][]ResolvedNote // by path; nil unless set is a PR's
  const OriginWorkingTree = "working tree"
  ```

A note is carried into PR set S when ALL hold: it is a stored root (not a reply, not entry-level, not a review, not a forge draft) with a `ContextHash` on the NEW side; it is NOT on one of S's commits (those already show) and not on a shelf entry; its path is changed by the PR (`DiffHunks` over `Base..Tip`, `Unified: 3`); its anchored lines' hash is found in the PR head's version of the path (`findAnchor` from its stored start). It is drawn at the found range with `Origin` = its commit's short sha, or `OriginWorkingTree`; its stored replies come along. The result is memory-cached under `tip + ":" + base + ":" + notesGen` (R3), never on disk. Badges (`PreviewNoteCounts`) stay store-only in this plan (they never read file content); plan 3 decides whether the Files badge counts carried notes.

- [ ] **Step 1: Write the failing test** (`internal/domain/forge_carried_test.go`)

```go
package domain

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

func TestCarriedNotesFollowIdenticalText(t *testing.T) {
	t.Parallel()
	dir, _ := prPreviewRepo(t) // the PR's a.go ends "var X = 1"
	runGitIn(t, dir, "checkout", "-q", "-b", "other")
	commitFile(t, dir, "a.go", "package a\n\nvar X = 1\nvar Y = 2\n", "same line elsewhere")
	other := revParse(t, dir, "HEAD")
	runGitIn(t, dir, "checkout", "-q", "main")
	_, svc := newRealRepoAt(t, dir)
	svc.UseNotesDir(t.TempDir())
	ctx := context.Background()
	add := func(line int, sum string) string {
		n, err := svc.NoteAdd(ctx, model.Note{Source: model.NoteSourceUser, Summary: sum,
			Address: model.FileAddress{State: model.StateCommitted, Commit: other, Path: "a.go"},
			Side:    model.NoteSideNew, Range: [2]int{line, line}})
		if err != nil {
			t.Fatal(err)
		}
		return n.ID
	}
	carried := add(3, "X is magic")
	notHere := add(4, "Y is not in the PR")
	set, err := svc.PreviewNotes(ctx, git.PRRef(7), "main")
	if err != nil || !set.OK() {
		t.Fatalf("set %+v err %v", set, err)
	}
	got, err := svc.PreviewNotesAt(ctx, set, "a.go")
	if err != nil {
		t.Fatal(err)
	}
	var hit *ResolvedNote
	for i := range got {
		if got[i].Note.ID == notHere {
			t.Fatal("a note whose text is not in the PR head was carried")
		}
		if got[i].Note.ID == carried {
			hit = &got[i]
		}
	}
	if hit == nil || hit.Range != [2]int{3, 3} || hit.Origin != other[:7] || hit.Sync != model.SyncLocal {
		t.Fatalf("carried = %+v", hit)
	}
	// A plain (non-PR) preview of the same pair carries nothing.
	plain, _ := svc.PreviewNotes(ctx, "other", "main")
	if all, _ := svc.PreviewNotesAll(ctx, plain); len(all["a.go"]) != 2 {
		t.Fatalf("a branch preview shows its own two notes only: %+v", all)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/domain/ -run CarriedNotes -count=1`
Expected: FAIL — the carried note is missing (`hit == nil`).

- [ ] **Step 3: Implement `forge_carried.go`**

```go
package domain

import (
	"context"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// OriginWorkingTree is a carried note's origin when it lives on uncommitted
// content.
const OriginWorkingTree = "working tree"

// carriedNotes (plan Task 8 rules), by path.
func (s *Service) carriedNotes(ctx context.Context, set PreviewNoteSet) map[string][]ResolvedNote {
	if _, ok := git.ParsePRRef(set.Source); !ok || !set.OK() {
		return nil
	}
	s.mu.Lock()
	key := set.Tip + ":" + set.Base + ":" + strconv.FormatUint(s.notesGen, 10)
	if c, ok := s.carriedCache[key]; ok {
		s.mu.Unlock()
		return c
	}
	gen := s.notesGen
	s.mu.Unlock()
	st := s.notesStore(ctx)
	if st == nil {
		return nil
	}
	all, err := st.LoadAll()
	if err != nil {
		return nil
	}
	files, err := s.DiffHunks(ctx, model.DiffSpec{Rev: set.Base + ".." + set.Tip, Unified: 3})
	if err != nil {
		return nil
	}
	changed := map[string]bool{}
	for _, f := range files {
		changed[f.Path] = true
	}
	in := set.commitSet()
	replies := map[string][]model.Note{}
	for _, n := range all {
		if n.IsReply() && !n.IsForgeReply() {
			replies[n.ParentID] = append(replies[n.ParentID], n)
		}
	}
	head := map[string][]string{} // path → the PR head's lines, read once
	out := map[string][]ResolvedNote{}
	for _, n := range all {
		a := n.Address
		if n.IsReply() || n.IsEntryLevel() || n.IsReviewNote() || n.ContextHash == "" || n.Side != model.NoteSideNew ||
			a.ShelfID != "" || in[a.Commit] || !changed[a.Path] {
			continue
		}
		lines, ok := head[a.Path]
		if !ok {
			if b, ferr := s.ShowFile(ctx, set.Tip, a.Path); ferr == nil {
				lines = splitLines(b)
			}
			head[a.Path] = lines
		}
		span := n.Range[1] - n.Range[0] + 1
		start := findAnchor(lines, n.ContextHash, max(span, 1), n.Range[0])
		if start == 0 {
			continue
		}
		rng := [2]int{start, start + max(span, 1) - 1}
		origin := OriginWorkingTree
		if a.Commit != "" {
			origin = shortSHA(a.Commit)
		}
		sync, serr := syncOf(n)
		rn := ResolvedNote{Note: n, Status: model.NoteActive, Range: rng, Sync: sync, SendErr: serr, Group: GroupMine, Origin: origin}
		for _, r := range replies[n.ID] {
			rs, re := syncOf(r)
			rn.Replies = append(rn.Replies, ResolvedNote{Note: r, Status: model.NoteActive, Range: rng, Sync: rs, SendErr: re, Group: GroupMine})
		}
		out[a.Path] = append(out[a.Path], rn)
	}
	s.mu.Lock()
	if s.notesGen == gen {
		if s.carriedCache == nil {
			s.carriedCache = map[string]map[string][]ResolvedNote{}
		}
		s.carriedCache[key] = out
	}
	s.mu.Unlock()
	return out
}
```

(`shortSHA` exists in previewnotes; add `strconv` to the imports; `Service` gains `carriedCache map[string]map[string][]ResolvedNote` beside `previewCounts`, and `invalidateNoteCounts` resets it.)

In `PreviewNotesFor` / `PreviewNotesAt`: after the store notes are resolved, append `s.carriedNotes(ctx, set)[path]` (before the forge threads, so the order is: the PR's own notes, carried notes, GitHub threads). In `PreviewNotesAll`: merge every path of `carriedNotes` into `out` (a path with only carried notes gains a key). The "no store" early returns stay as they are.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/domain/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/domain
git commit -m "feat(notes): a PR's view carries notes whose lines it holds verbatim"
```

---

### Task 9: domain — the pending-send queue

**Files:**
- Create: `internal/domain/pendingsend.go`
- Create: `internal/domain/pendingsend_test.go`
- Modify: `internal/domain/statebasedir_test.go`, `internal/domain/statebasedir_callers_test.go` (the `pending-sends` kind)

**Interfaces:**
- Consumes: `PRSendRequest` (Task 6); `filelock.Acquire`, `stateBaseDir`, `repoKey`, `clock.Now`.
- Produces:
  ```go
  type PendingSend struct {
      ID        string        `toml:"id" json:"id"`
      Requester string        `toml:"requester" json:"requester"`
      Request   PRSendRequest `toml:"request" json:"request"`
      Created   time.Time     `toml:"created" json:"created"`
      State     string        `toml:"state" json:"state"`
      Outcome   string        `toml:"outcome,omitempty" json:"outcome,omitempty"`
      Done      time.Time     `toml:"done,omitempty" json:"done,omitempty"`
  }
  const (PendingWaiting = "pending"; PendingSent = "sent"; PendingRejected = "rejected"; PendingFailed = "failed";
         PendingCancelled = "cancelled"; PendingExpired = "expired")
  const PendingSendTTL = 24 * time.Hour
  const DefaultPendingWait = 10 * time.Minute
  var ErrPendingSendNotFound, ErrPendingSendClosed, ErrPendingStillWaiting error
  var pendingPoll = 250 * time.Millisecond // tests shorten it
  func (s *Service) PendingSendAdd(ctx context.Context, req PRSendRequest, requester string) (PendingSend, error)
  func (s *Service) PendingSends(ctx context.Context) ([]PendingSend, error)
  func (s *Service) PendingSendGet(ctx context.Context, id string) (PendingSend, error)
  func (s *Service) PendingSendFinish(ctx context.Context, id, state, outcome string) (PendingSend, error)
  func (s *Service) PendingSendWait(ctx context.Context, id string, timeout time.Duration) (PendingSend, error)
  ```

The file: `stateBaseDir("pending-sends")/<repoKey(git common dir)>.toml` (`[[sends]]` array), lock `<file>.lock`. Every read-modify-write runs under the lock and first applies the expiry: a `pending` entry older than 24 h becomes `expired` (Done = now, Outcome "expired: nobody approved it within 24 h"); a finished entry whose `Done` is older than 24 h is dropped. `Finish` moves only a `pending` entry (anything else → `ErrPendingSendClosed`). `Wait` polls `Get` every `pendingPoll` until the state is not `pending` (returns it), the ctx ends, or the timeout passes (returns the pending entry and `ErrPendingStillWaiting`).

- [ ] **Step 1: Write the failing tests** (`internal/domain/pendingsend_test.go`)

```go
package domain

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/clock"
)

func TestPendingSendLifecycleAcrossTwoServices(t *testing.T) {
	t.Parallel()
	dir, agent := newRealRepo(t)
	_, user := newRealRepoAt(t, dir) // a second process on the same repo
	ctx := context.Background()
	e, err := agent.PendingSendAdd(ctx, PRSendRequest{PR: 7, Notes: []string{"n1"}}, "claude")
	if err != nil || e.ID == "" || e.State != PendingWaiting {
		t.Fatalf("Add = %+v, %v", e, err)
	}
	list, err := user.PendingSends(ctx)
	if err != nil || len(list) != 1 || list[0].Request.Notes[0] != "n1" || list[0].Requester != "claude" {
		t.Fatalf("the other process lists %+v, %v", list, err)
	}
	done := make(chan PendingSend, 1)
	go func() {
		got, _ := agent.PendingSendWait(ctx, e.ID, 5*time.Second)
		done <- got
	}()
	if _, err := user.PendingSendFinish(ctx, e.ID, PendingSent, "sent 1 comments to o/r #7"); err != nil {
		t.Fatal(err)
	}
	if got := <-done; got.State != PendingSent || got.Outcome != "sent 1 comments to o/r #7" {
		t.Fatalf("the waiter got %+v", got)
	}
	if _, err := user.PendingSendFinish(ctx, e.ID, PendingRejected, ""); !errors.Is(err, ErrPendingSendClosed) {
		t.Fatalf("finishing twice = %v", err)
	}
}

func TestPendingSendWaitTimesOut(t *testing.T) {
	t.Parallel()
	_, svc := newRealRepo(t)
	ctx := context.Background()
	e, _ := svc.PendingSendAdd(ctx, PRSendRequest{PR: 7, Mine: true}, "claude")
	got, err := svc.PendingSendWait(ctx, e.ID, 30*time.Millisecond)
	if !errors.Is(err, ErrPendingStillWaiting) || got.State != PendingWaiting {
		t.Fatalf("Wait = %+v, %v", got, err)
	}
}

// Serial: freezes the process clock.
func TestPendingSendExpiresAfterADay(t *testing.T) {
	_, svc := newRealRepo(t)
	ctx := context.Background()
	t0 := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	restore := clock.Freeze(t0)
	defer restore()
	e, _ := svc.PendingSendAdd(ctx, PRSendRequest{PR: 7, Mine: true}, "claude")
	clock.Freeze(t0.Add(25 * time.Hour))
	got, err := svc.PendingSendGet(ctx, e.ID)
	if err != nil || got.State != PendingExpired {
		t.Fatalf("after 25 h: %+v, %v", got, err)
	}
	clock.Freeze(t0.Add(50 * time.Hour))
	if _, err := svc.PendingSendGet(ctx, e.ID); !errors.Is(err, ErrPendingSendNotFound) {
		t.Fatalf("a finished entry is dropped a day later: %v", err)
	}
}
```

(Use the `clock` package's actual freeze API — check `internal/clock` for its name and restore shape.)

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/domain/ -run PendingSend -count=1`
Expected: FAIL — `undefined: PendingSendAdd`.

- [ ] **Step 3: Implement `pendingsend.go`**

```go
package domain

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/homeend/gigagit/internal/clock"
	"github.com/homeend/gigagit/internal/filelock"
)

// (types, constants and errors from the Interfaces block)

var (
	ErrPendingSendNotFound = errors.New("no such pending send")
	ErrPendingSendClosed   = errors.New("that pending send is no longer waiting")
	ErrPendingStillWaiting = errors.New("still pending")
)

type pendingFile struct {
	Sends []PendingSend `toml:"sends"`
}

// pendingPath is this repository's queue file ("" when no state dir).
func (s *Service) pendingPath(ctx context.Context) (string, error) {
	base := stateBaseDir("pending-sends")
	if base == "" {
		return "", ErrNotesDisabled
	}
	cd, err := s.GitCommonDir(ctx)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, repoKey(strings.TrimSpace(cd))+".toml"), nil
}

// pendingMutate is the queue's one read-modify-write: lock, read, expire,
// apply fn, write (temp + rename) when anything changed.
func (s *Service) pendingMutate(ctx context.Context, fn func(f *pendingFile) error) error {
	path, err := s.pendingPath(ctx)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	release, err := filelock.Acquire(path + ".lock")
	if err != nil {
		return err
	}
	defer release()
	var f pendingFile
	if b, err := os.ReadFile(path); err == nil {
		if err := toml.Unmarshal(b, &f); err != nil {
			_ = os.Rename(path, path+".corrupt-"+clock.Now().Format("20060102150405"))
			f = pendingFile{}
		}
	}
	now := clock.Now()
	kept := f.Sends[:0]
	for _, e := range f.Sends {
		switch {
		case e.State == PendingWaiting && now.Sub(e.Created) > PendingSendTTL:
			e.State, e.Done, e.Outcome = PendingExpired, now, "expired: nobody approved it within 24 h"
		case e.State != PendingWaiting && now.Sub(e.Done) > PendingSendTTL:
			continue
		}
		kept = append(kept, e)
	}
	f.Sends = kept
	if err := fn(&f); err != nil {
		return err
	}
	b, err := toml.Marshal(f)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *Service) PendingSendAdd(ctx context.Context, req PRSendRequest, requester string) (PendingSend, error) {
	var b [4]byte
	rand.Read(b[:])
	e := PendingSend{ID: hex.EncodeToString(b[:]), Requester: requester, Request: req, Created: clock.Now(), State: PendingWaiting}
	return e, s.pendingMutate(ctx, func(f *pendingFile) error { f.Sends = append(f.Sends, e); return nil })
}

func (s *Service) PendingSends(ctx context.Context) ([]PendingSend, error) {
	var out []PendingSend
	err := s.pendingMutate(ctx, func(f *pendingFile) error { out = append(out, f.Sends...); return nil })
	return out, err
}

func (s *Service) PendingSendGet(ctx context.Context, id string) (PendingSend, error) {
	all, err := s.PendingSends(ctx)
	if err != nil {
		return PendingSend{}, err
	}
	for _, e := range all {
		if e.ID == id {
			return e, nil
		}
	}
	return PendingSend{}, ErrPendingSendNotFound
}

func (s *Service) PendingSendFinish(ctx context.Context, id, state, outcome string) (PendingSend, error) {
	var got PendingSend
	err := s.pendingMutate(ctx, func(f *pendingFile) error {
		for i := range f.Sends {
			if f.Sends[i].ID != id {
				continue
			}
			if f.Sends[i].State != PendingWaiting {
				return ErrPendingSendClosed
			}
			f.Sends[i].State, f.Sends[i].Outcome, f.Sends[i].Done = state, outcome, clock.Now()
			got = f.Sends[i]
			return nil
		}
		return ErrPendingSendNotFound
	})
	return got, err
}

func (s *Service) PendingSendWait(ctx context.Context, id string, timeout time.Duration) (PendingSend, error) {
	deadline := time.Now().Add(timeout)
	for {
		e, err := s.PendingSendGet(ctx, id)
		if err != nil || e.State != PendingWaiting {
			return e, err
		}
		if time.Now().After(deadline) {
			return e, ErrPendingStillWaiting
		}
		select {
		case <-ctx.Done():
			return e, ctx.Err()
		case <-time.After(pendingPoll):
		}
	}
}
```

(The mutate writes even on a pure read — expiry may have changed the file; skip the write when nothing changed by comparing the marshalled bytes with what was read.)

Add `"pending-sends"` to the kinds lists in `statebasedir_test.go` and `statebasedir_callers_test.go`.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/domain/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/domain
git commit -m "feat(forge): the pending-send queue — an agent's send waits for the user"
```

---

### Task 10: CLI — agents queue, the user approves: `gg pr pending`

**Files:**
- Modify: `internal/cli/prsend.go` (`runPRSend` queues inside a gg session)
- Create: `internal/cli/prpending.go`, `internal/cli/prpending_test.go`
- Modify: `internal/cli/pr.go` (dispatch `pending`; usage)

**Interfaces:**
- Consumes: `PendingSendAdd/Get/Finish/Wait/PendingSends`, `DefaultPendingWait`, `ErrPendingStillWaiting` (Task 9); `runPRSend` (Task 7); `sessionGetenv`.
- Produces:
  ```go
  var pendingWaitTimeout = domain.DefaultPendingWait // tests shorten it
  func inGGSession() bool                           // sessionGetenv("GG_INBOX") != ""
  func prPending(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int
  // exit codes: 0 sent / listed; 1 rejected, failed, cancelled, expired, or an error; 3 still pending.
  ```

Usage lines:

```
       gg pr pending [list] [--json]
       gg pr pending approve <id> [--yes] | reject <id> | wait <id> | cancel <id>
```

Inside a gg session every write (`send`, `reply --send`, `resolve`, `unresolve`) queues: stderr `--yes is ignored inside a gg session: the user approves sends in gg` when `--yes` was given; stdout `queued <id>: waiting for the user's approval (gg pr pending approve <id>)`; then the wait: `sent` → its outcome on stdout, exit 0; rejected / failed / cancelled / expired → the outcome on stderr, exit 1; timeout → `still pending: <id> (gg pr pending wait <id>)`, exit 3. `approve` and `reject` are refused inside a session (exit 1: `approve pending sends in your own terminal or in gg`). `approve` runs the request through `runPRSend` with the user's own confirm (`--yes` allowed: the user typed it) and finishes the entry: the op's error → `failed`; an `aborted:` summary → `rejected`; else `sent` with the summary.

- [ ] **Step 1: Write the failing tests** (`internal/cli/prpending_test.go`)

```go
package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/forge/forgetest"
)

// Serial: swaps sessionGetenv and pendingWaitTimeout.
func inSession(t *testing.T) {
	t.Helper()
	old := sessionGetenv
	sessionGetenv = func(k string) string {
		if k == "GG_INBOX" {
			return t.TempDir()
		}
		return ""
	}
	oldWait := pendingWaitTimeout
	pendingWaitTimeout = 50 * time.Millisecond
	t.Cleanup(func() { sessionGetenv, pendingWaitTimeout = old, oldWait })
}

func TestAgentSendIsQueuedThenApproved(t *testing.T) {
	dir, head, fixtures := sendPRRepo(t)
	id := addCLINote(t, dir, head, 5)
	inSession(t)
	out, errs, code := runPR(t, dir, "send", "7", "--note", id, "--yes")
	if code != 3 || !strings.Contains(errs, "--yes is ignored inside a gg session") || !strings.Contains(out, "still pending: ") {
		t.Fatalf("exit %d out %q err %q", code, out, errs)
	}
	if ws := forgetest.Writes(t, fixtures); len(ws) != 0 {
		t.Fatalf("an agent's send posted: %v", ws)
	}
	pid := strings.Fields(strings.TrimPrefix(out[strings.Index(out, "still pending: "):], "still pending: "))[0]
	if _, errs, code := runPR(t, dir, "pending", "approve", pid, "--yes"); code != 1 || !strings.Contains(errs, "own terminal") {
		t.Fatalf("approve inside a session: exit %d %q", code, errs)
	}
	// The user, in a plain terminal:
	sessionGetenv = func(string) string { return "" }
	list, _, _ := runPR(t, dir, "pending")
	if !strings.Contains(list, pid) || !strings.Contains(list, "pending") || !strings.Contains(list, "#7") {
		t.Fatalf("list = %q", list)
	}
	if _, errs, code := runPR(t, dir, "pending", "approve", pid, "--yes"); code != 0 {
		t.Fatalf("approve: exit %d %s", code, errs)
	}
	if ws := forgetest.Writes(t, fixtures); len(ws) != 3 || ws[2].Op != "SubmitReview" {
		t.Fatalf("writes after approve = %v", ws)
	}
	out, _, code = runPR(t, dir, "pending", "wait", pid)
	if code != 0 || !strings.Contains(out, "sent 1 comments") {
		t.Fatalf("wait: exit %d %q", code, out)
	}
}

func TestAgentCancelsItsSend(t *testing.T) {
	dir, head, _ := sendPRRepo(t)
	id := addCLINote(t, dir, head, 5)
	inSession(t)
	out, _, _ := runPR(t, dir, "send", "7", "--note", id)
	pid := strings.Fields(strings.TrimPrefix(out[strings.Index(out, "still pending: "):], "still pending: "))[0]
	if _, errs, code := runPR(t, dir, "pending", "cancel", pid); code != 0 {
		t.Fatalf("cancel: %s", errs)
	}
	if _, errs, code := runPR(t, dir, "pending", "wait", pid); code != 1 || !strings.Contains(errs, "cancelled") {
		t.Fatalf("wait after cancel: exit %d %q", code, errs)
	}
}

func TestUserRejects(t *testing.T) {
	dir, head, fixtures := sendPRRepo(t)
	id := addCLINote(t, dir, head, 5)
	inSession(t)
	out, _, _ := runPR(t, dir, "send", "7", "--note", id)
	pid := strings.Fields(strings.TrimPrefix(out[strings.Index(out, "still pending: "):], "still pending: "))[0]
	sessionGetenv = func(string) string { return "" }
	if _, _, code := runPR(t, dir, "pending", "reject", pid); code != 0 {
		t.Fatal("reject failed")
	}
	if ws := forgetest.Writes(t, fixtures); len(ws) != 0 {
		t.Fatalf("a rejected send posted: %v", ws)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/cli/ -run 'AgentSend|AgentCancels|UserRejects' -count=1`
Expected: FAIL — inside the stubbed session the send posts (writes ≠ 0) and `pending` is a usage error.

- [ ] **Step 3: Implement**

At the top of `runPRSend`:

```go
	if inGGSession() {
		if yes {
			fmt.Fprintln(stderr, "--yes is ignored inside a gg session: the user approves sends in gg")
		}
		return queueAndWait(ctx, svc, req, stdout, stderr)
	}
```

`prpending.go`:

```go
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/clock"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

var pendingWaitTimeout = domain.DefaultPendingWait

func inGGSession() bool { return sessionGetenv("GG_INBOX") != "" }

// queueAndWait is an agent's send: a pending entry the user approves in gg,
// and a long-poll on its outcome.
func queueAndWait(ctx context.Context, svc *domain.Service, req domain.PRSendRequest, stdout, stderr io.Writer) int {
	who := strings.TrimSpace(os.Getenv("GG_AGENT"))
	if who == "" {
		who = "agent"
	}
	req.Agent = who
	e, err := svc.PendingSendAdd(ctx, req, who)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	fmt.Fprintf(stdout, "queued %s: waiting for the user's approval (gg pr pending approve %s)\n", e.ID, e.ID)
	return waitOutcome(ctx, svc, e.ID, stdout, stderr)
}

func waitOutcome(ctx context.Context, svc *domain.Service, id string, stdout, stderr io.Writer) int {
	e, err := svc.PendingSendWait(ctx, id, pendingWaitTimeout)
	switch {
	case errors.Is(err, domain.ErrPendingStillWaiting):
		fmt.Fprintf(stdout, "still pending: %s (gg pr pending wait %s)\n", id, id)
		return 3
	case err != nil:
		fmt.Fprintln(stderr, "error:", err)
		return 1
	case e.State == domain.PendingSent:
		fmt.Fprintln(stdout, e.Outcome)
		return 0
	}
	fmt.Fprintf(stderr, "%s: %s\n", e.State, e.Outcome)
	return 1
}

func prPending(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	ctx := context.Background()
	verb := "list"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		verb, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("pr pending", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "JSON rows (list)")
	yes := fs.Bool("yes", false, "answer the confirm (approve)")
	pos, flags := splitPositionals(args, 1)
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	needID := verb != "list"
	if needID != (len(pos) == 1) {
		fmt.Fprintln(stderr, prUsage)
		return 2
	}
	switch verb {
	case "list":
		all, err := svc.PendingSends(ctx)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		if *asJSON {
			if all == nil {
				all = []domain.PendingSend{}
			}
			return jsonOut(stdout, stderr, all)
		}
		if len(all) == 0 {
			fmt.Fprintln(stdout, "(no pending sends)")
		}
		for _, e := range all {
			fmt.Fprintf(stdout, "%s  #%d  %-9s %s  %s  %s ago\n", e.ID, e.Request.PR, e.State, e.Requester,
				describeRequest(e.Request), clock.Now().Sub(e.Created).Round(time.Second))
		}
		return 0
	case "wait":
		return waitOutcome(ctx, svc, pos[0], stdout, stderr)
	case "cancel":
		if _, err := svc.PendingSendFinish(ctx, pos[0], domain.PendingCancelled, "cancelled by the agent"); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		return 0
	case "approve", "reject":
		if inGGSession() {
			fmt.Fprintln(stderr, "error: approve pending sends in your own terminal or in gg")
			return 1
		}
		e, err := svc.PendingSendGet(ctx, pos[0])
		if err == nil && e.State != domain.PendingWaiting {
			err = domain.ErrPendingSendClosed
		}
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		if verb == "reject" {
			_, err := svc.PendingSendFinish(ctx, e.ID, domain.PendingRejected, "rejected by the user")
			if err != nil {
				fmt.Fprintln(stderr, "error:", err)
				return 1
			}
			return 0
		}
		return approvePending(ctx, svc, e, *yes, stdin, stdout, stderr)
	}
	fmt.Fprintln(stderr, prUsage)
	return 2
}
```

`approvePending` runs the request (`runPRSendCapture`: `runPRSend`'s body returning the `engine.Result` and error instead of an exit code — refactor `runPRSend` into that plus a printing wrapper), then `PendingSendFinish` with `failed` (the error text), `rejected` (`aborted:` summary) or `sent` (the summary), and prints the summary. `describeRequest(req)` is one short phrase: `review r1`, `my draft review`, `3 notes`, `verdict`, `resolve PRRT_x`, `finish`, `discard`. The `answer` for `--yes` is derived exactly as in `prSend` (extract that switch into `defaultAnswer(req, event string) string` and reuse it).

Dispatch `pending` in `cmdPR` beside `send`.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/cli/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/cli
git commit -m "feat(cli): agents queue sends; gg pr pending list, approve, reject, wait, cancel"
```

---

### Task 11: CLI — `gg note list` shows the sync state and a carried note's origin

**Files:**
- Modify: `internal/cli/note.go` (`renderNoteLine`)
- Test: `internal/cli/note_sync_test.go` (new)

**Interfaces:**
- Consumes: `ResolvedNote.Sync`, `SendErr`, `Origin` (Tasks 3, 8).
- Produces: `renderNoteLine` output — unchanged for a local note (R9); `<id> [<source>] [sending] …` / `<id> [<source>] [failed: <error>] …`; a carried note ends its status word with ` (from <origin>)`.

- [ ] **Step 1: Write the failing test**

```go
package cli

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

func TestRenderNoteLineSyncTokens(t *testing.T) {
	t.Parallel()
	base := domain.ResolvedNote{Note: model.Note{ID: "n1", Source: model.NoteSourceUser, Summary: "s",
		Address: model.FileAddress{State: model.StateCommitted, Commit: strings.Repeat("a", 40), Path: "a.go"},
		Side:    model.NoteSideNew}, Range: [2]int{3, 3}, Status: model.NoteActive, Sync: model.SyncLocal}
	line := func(r domain.ResolvedNote) string {
		var b strings.Builder
		renderNoteLine(&b, r, false, "active")
		return b.String()
	}
	if got := line(base); got != "n1 [user] aaaaaaa:a.go new:3-3 active  s\n" {
		t.Fatalf("a local note's line changed: %q", got)
	}
	failed := base
	failed.Sync, failed.SendErr = model.SyncFailed, "HTTP 403"
	if got := line(failed); !strings.HasPrefix(got, "n1 [user] [failed: HTTP 403] aaaaaaa:a.go") {
		t.Errorf("failed = %q", got)
	}
	sending := base
	sending.Sync = model.SyncSending
	if got := line(sending); !strings.HasPrefix(got, "n1 [user] [sending] ") {
		t.Errorf("sending = %q", got)
	}
	carried := base
	carried.Origin = "bbbbbbb"
	if got := line(carried); !strings.Contains(got, "active (from bbbbbbb)  s") {
		t.Errorf("carried = %q", got)
	}
}
```

(Check the exact current line format first — `"%s [%s] %s %s:%d-%d %s  %s\n"` — and pin the local-note expectation to what it prints today.)

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/cli/ -run RenderNoteLineSyncTokens -count=1`
Expected: FAIL on the failed/sending/carried lines; the local line passes.

- [ ] **Step 3: Implement**

In `renderNoteLine`, build `src := "[" + string(r.Note.Source) + "]"` and append `" [sending]"` or `" [failed: " + r.SendErr + "]"` for those states; use `src` in every branch's format where `[%s]` printed the source. For the line-note branch, `status += " (from " + r.Origin + ")"` when `r.Origin != ""`.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/cli/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/cli
git commit -m "feat(cli): gg note list shows a note's sync state and where a carried note lives"
```

---

### Task 12: docs, the read-only ruling reversed, skill, gates, probe, review

**Files:**
- Modify: `CHANGELOG.md`, `README.md` (PR section: sending, replies, resolve, pending sends)
- Modify: `CLAUDE.md` (`forge` row: "Forge (pull-request) seam: the read-only `Provider` + the optional `Writer` (GraphQL mutations through `gh api graphql --input`), reached only through `engine.SendToForge`; …"; `cli` row: `gg pr` read + send/reply/resolve/pending; `engine` row: `SendToForge`)
- Modify: `docs/CLAUDE-details.md` (a "GitHub write-back (plan 2)" section: decision id + options, the write order, the settle table, stamps and markers, the queue file, the CLI exit codes, R1–R11)
- Modify: `internal/model/note.go` (`NoteSourceForge`'s comment: "a review comment read from a forge … never stored; edited and deleted only on the forge — gg sends notes TO it (`SendToForge`) and stores draft replies to it")
- Modify: `docs/superpowers/specs/2026-09-19-*forge*` (its read-only ruling: one line "Reversed 2026-10-07 by the GitHub write-back spec; gg writes only through `SendToForge`.")
- Modify: `internal/agentskill/using-gg.md` + `agentskill.Version` bump (R11: a short "Sending to GitHub" paragraph — agents queue with `gg pr send`/`reply --send`/`resolve`, exit 3 = still pending, `gg pr pending wait <id>`, never `--yes`)

- [ ] **Step 1: Write the docs** listed above. Keep CLAUDE.md rows to one line each.

- [ ] **Step 2: Update the installed skills**

Run: `go build -o /tmp/claude-1000/gg-skill ./cmd/gg && /tmp/claude-1000/gg-skill init --update`
Expected: the skill files report the new version.

- [ ] **Step 3: Run the race gate**

Run: `./test.sh race > <workspace>/race.log 2>&1; tail -5 <workspace>/race.log`
Expected: the log ends with "all green" (anything else is a failure — read the log).

- [ ] **Step 4: Commit**

```bash
gg add CHANGELOG.md README.md CLAUDE.md docs internal/model/note.go internal/agentskill
git commit -m "docs: GitHub write-back plan 2 — send core + CLI; the forge read-only ruling reversed"
```

- [ ] **Step 5: The empty-body probe (ASK THE USER FIRST)**

Ask the user whether to probe `submitPullRequestReview` with `COMMENT` and an empty body on a scratch repository of theirs. Only on a yes: build gg, open a throwaway PR in that repository, run `gg pr send <n> --note <id> --yes` once and read `writes.jsonl`-equivalent output (`gh api` stderr) for a blank-body refusal. Record the answer in `docs/CLAUDE-details.md` (and drop the retry if GitHub accepts the empty body — or keep it; it costs nothing). On a no: leave R10's retry in place and note "unverified" in the details doc.

- [ ] **Step 6: Final whole-branch review** — one read-only review subagent (most capable model) over `git merge-base main HEAD..HEAD` with this plan, the spec and the ledger's rulings; fix Critical/Important findings RED→GREEN; re-run the race gate.

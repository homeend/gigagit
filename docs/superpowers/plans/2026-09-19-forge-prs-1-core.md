# Forge PRs — Plan 1 of 3: core + CLI — Implementation Plan

> **For agentic workers:** this repo forbids subagents (CLAUDE.md). Execute with
> superpowers:executing-plans, inline, in the ONE session, in the worktree
> `/mnt/t/others/gigagit/.claude/worktrees/forge-prs`. Steps use checkbox
> (`- [ ]`) syntax for tracking.

**Goal:** a read-only, forge-neutral pull-request core (types, `gh` provider,
detection, PR-head fetch, domain queries) with a scriptable `gg pr` CLI.

**Architecture:** plain PR/comment types live in `internal/model`; a new leaf
`internal/forge` holds the `Provider` interface and the `gh` implementation
(shelling out through `gitexec.Runner`); `domain` owns detection (once per
session), the PR queries, the known-PR set and the op builders; two tiny
RefWrite engine ops fetch/forget `refs/gg/pr/<n>`; `internal/cli` prints.

**Tech Stack:** Go 1.26, system `gh` ≥ 2.x (JSON + `gh api graphql`), real git
in `t.TempDir()`, a Go-built fake `gh` for tests.

**Spec:** `docs/superpowers/specs/2026-09-19-forge-prs-design.md`

**Plan series:** 1 = this (core + CLI, shippable alone). 2 = TUI PRs tab, PR
hub popup, `[refresh] prs_seconds`. 3 = inline comments as read-only note
boxes + the generic collapse toggle. Plans 2 and 3 are written after plan 1
lands, against its real signatures.

## Deviations from the spec (decided while planning — spec is amended in Task 12)

1. **Types live in `internal/model`**, not `internal/forge`: archtest forbids
   frontends importing a domain-owned package, and the CLI/TUI must name the
   types. `forge` keeps `Provider` + `gh`.
2. **No comment count on list rows.** `gh pr list --json` has no count field
   (`comments` returns every body). `PullRequest.Comments` is dropped.
3. **Comment caps are single-page**: 100 review threads × 50 comments each,
   100 conversation comments, 100 reviews. `Truncated` is set when any
   connection reports `hasNextPage`. (Spec said "paginated to 500".)
4. **e2e fixture transport:** the fake `gh` reads canned JSON from
   `$GG_FAKEGH_DIR`, else `<cwd>/.git/fakegh/` — so a TOML scenario seeds it
   with ordinary `write` steps and needs no schema change.

## Global Constraints

- NEVER use subagents. All work in the worktree; every shell command starts
  with `cd /mnt/t/others/gigagit/.claude/worktrees/forge-prs &&`.
- Read-only toward the forge: no `gh` verb that mutates (`comment`, `review`,
  `edit`, `merge`, `close`, `api -X POST/PATCH/DELETE`) may appear anywhere.
- Every `gh` call runs under a 30 s context timeout.
- PR head refs live under `refs/gg/pr/<n>` and are never pruned automatically.
- Detection runs once per `domain.Service`; no re-detection.
- Frontends never import `internal/forge` (archtest rule added in Task 3).
- Engine/CLI prose stays English. New tests call `t.Parallel()` unless they
  use `t.Setenv` (then serial, and say why in a comment).
- One git verb = one invocation, argv via `gitcmd`.
- Use `gg` for git ops (`gg add <paths>`, `gg commit -m …`); stage named
  paths only, never `-A`. Commit trailers:
  `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>` and
  `Claude-Session: https://claude.ai/code/session_017VxidCD6HkJEKBATaLPmv5`.

## File structure

| File | Responsibility |
|---|---|
| `internal/model/forge.go` (new) | `PullRequest`, `ForgeComment`, `ForgeCommentKind`, PR state consts |
| `internal/forge/forge.go` (new) | `Provider` interface, `Default`, `ErrUnavailable` |
| `internal/forge/gh.go` (new) | `GH` provider: argv + calls |
| `internal/forge/gh_parse.go` (new) | pure JSON → model parsers |
| `internal/forge/remote.go` (new) | `RepoSlug(url)` — host/owner/name from ssh/https URLs |
| `internal/forge/testdata/*.json` (new) | captured-shape fixtures |
| `internal/forge/testdata/fakegh/main.go` (new) | fake `gh` binary |
| `internal/forge/forgetest/forgetest.go` (new) | `BuildFakeGH(t)`, `Seed(t, dir, files)` |
| `internal/preflight/preflight.go` | `Feature.Silent`, `ForgeProbe`, `ForgeUsable` |
| `internal/tui/notify.go` | skip silent verdicts |
| `internal/git/prref.go` (new) | `PRRefPrefix`, `PRRef(n)`, `ParsePRRef`, `FetchRefspec` verb |
| `internal/engine/fetch_pr_head.go`, `forget_pr.go` (new) | the two ops |
| `internal/engine/gitops.go` | `FetchRefspec` on `GitOps` |
| `internal/domain/forge.go` (new) | `ForgeStatus`, `PullRequests`, `PullRequest`, `PRComments`, `PRFetchOp`, `PRForgetOp`, `PRPair` |
| `internal/domain/features.go` | `FeatureForge` declaration |
| `internal/cli/pr.go` (new) | `gg pr list|view|comments|fetch|forget` |
| `e2e/scenarios/pr_readonly.toml` (new) | list/view/comments scenario |

---

### Task 1: model types

**Files:**
- Create: `internal/model/forge.go`
- Test: `internal/model/forge_test.go`

**Interfaces — Produces:**
```go
const (PRStateOpen="open"; PRStateClosed="closed"; PRStateMerged="merged"; PRStateUnavailable="unavailable")
type PullRequest struct{…}           // fields below
func (p PullRequest) IsOpen() bool
type ForgeCommentKind string          // "inline" | "file" | "general" | "review"
type ForgeComment struct{…}
```

- [ ] **Step 1: failing test** — `internal/model/forge_test.go`

```go
package model

import "testing"

func TestPullRequestIsOpen(t *testing.T) {
	t.Parallel()
	for state, want := range map[string]bool{
		PRStateOpen: true, PRStateClosed: false, PRStateMerged: false, PRStateUnavailable: false,
	} {
		if got := (PullRequest{State: state}).IsOpen(); got != want {
			t.Errorf("IsOpen(%q) = %v, want %v", state, got, want)
		}
	}
}
```

- [ ] **Step 2:** `cd /mnt/t/others/gigagit/.claude/worktrees/forge-prs && rtk go test ./internal/model/ -run TestPullRequestIsOpen` → FAIL (undefined).

- [ ] **Step 3: implement** — `internal/model/forge.go`

```go
package model

import "time"

// PR states are English protocol values (CLI --json, future MCP); the TUI
// localizes only their rendering.
const (
	PRStateOpen        = "open"
	PRStateClosed      = "closed"
	PRStateMerged      = "merged"
	PRStateUnavailable = "unavailable" // known locally, the forge no longer answers for it
)

// PullRequest is one forge pull/merge request, provider-neutral.
type PullRequest struct {
	Number      int       `json:"number"`
	Title       string    `json:"title"`
	Body        string    `json:"body,omitempty"`
	Author      string    `json:"author"`
	State       string    `json:"state"`
	Draft       bool      `json:"draft"`
	ReviewState string    `json:"review_state,omitempty"` // "", approved, changes_requested, review_required
	Source      string    `json:"source"`                 // head branch name
	SourceRepo  string    `json:"source_repo,omitempty"`  // owner/name when the head lives in a fork
	Target      string    `json:"target"`                 // base branch name
	HeadSHA     string    `json:"head_sha"`
	BaseSHA     string    `json:"base_sha"`
	URL         string    `json:"url"`
	Created     time.Time `json:"created"`
	Updated     time.Time `json:"updated"`
}

// IsOpen reports whether p is still open on the forge.
func (p PullRequest) IsOpen() bool { return p.State == PRStateOpen }

// ForgeCommentKind classifies a comment by where it hangs.
type ForgeCommentKind string

const (
	ForgeCommentInline  ForgeCommentKind = "inline"  // anchored to a line (range)
	ForgeCommentFile    ForgeCommentKind = "file"    // anchored to a whole file
	ForgeCommentGeneral ForgeCommentKind = "general" // the PR conversation
	ForgeCommentReview  ForgeCommentKind = "review"  // a submitted review's summary + verdict
)

// ForgeComment is one comment of any kind. IDs are the forge's, opaque.
type ForgeComment struct {
	ID        string           `json:"id"`
	ParentID  string           `json:"parent_id,omitempty"`
	Kind      ForgeCommentKind `json:"kind"`
	Author    string           `json:"author"`
	Body      string           `json:"body"`
	Path      string           `json:"path,omitempty"`
	Side      NoteSide         `json:"side,omitempty"`
	Line      int              `json:"line,omitempty"`
	StartLine int              `json:"start_line,omitempty"` // == Line for a one-line comment
	Outdated  bool             `json:"outdated,omitempty"`
	Resolved  bool             `json:"resolved,omitempty"`
	Hunk      string           `json:"hunk,omitempty"`
	Verdict   string           `json:"verdict,omitempty"` // review only: approved, changes_requested, commented
	Created   time.Time        `json:"created"`
	Updated   time.Time        `json:"updated"`
}
```

`NoteSide` is `model.NoteSideOld = "old"` / `model.NoteSideNew = "new"`
(`note.go:22-25`, verified) — Task 3's parser uses them by name.

- [ ] **Step 4:** re-run the test → PASS.
- [ ] **Step 5: commit** — `gg add internal/model/forge.go internal/model/forge_test.go && gg commit -m "feat(model): provider-neutral pull-request and forge-comment types"` (+ trailers).

---

### Task 2: `forge` package — Provider, slug parser

**Files:**
- Create: `internal/forge/forge.go`, `internal/forge/remote.go`
- Test: `internal/forge/remote_test.go`

**Interfaces — Produces:**
```go
type Provider interface {
	Name() string
	Detect(ctx context.Context) error
	ListOpen(ctx context.Context) ([]model.PullRequest, error)
	PR(ctx context.Context, n int) (model.PullRequest, error)
	Comments(ctx context.Context, n int) (cs []model.ForgeComment, truncated bool, err error)
	BaseRepo(ctx context.Context) (slug, fallbackURL string, err error) // "host/owner/name"
	HeadRefspec(n int) string
}
var ErrNotFound = errors.New("forge: pull request not found")
func RepoSlug(remoteURL string) string   // "" when unparseable
func Default(workDir string, rec observ.Recorder) []Provider
```

- [ ] **Step 1: failing test** — `internal/forge/remote_test.go`

```go
package forge

import "testing"

func TestRepoSlug(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"git@github.com:homeend/gigagit.git":         "github.com/homeend/gigagit",
		"ssh://git@github.com/homeend/gigagit.git":   "github.com/homeend/gigagit",
		"ssh://git@github.com:22/homeend/gigagit":    "github.com/homeend/gigagit",
		"https://github.com/homeend/gigagit.git":     "github.com/homeend/gigagit",
		"https://user:tok@github.com/Homeend/GigaGit/": "github.com/homeend/gigagit",
		"/srv/git/repo.git":                          "",
		"":                                           "",
	} {
		if got := RepoSlug(in); got != want {
			t.Errorf("RepoSlug(%q) = %q, want %q", in, got, want)
		}
	}
}
```

- [ ] **Step 2:** `rtk go test ./internal/forge/` → FAIL (no package).

- [ ] **Step 3: implement**

`internal/forge/remote.go`:
```go
package forge

import (
	"net/url"
	"strings"
)

// RepoSlug reduces a git remote URL to "host/owner/name" (lower-cased, no
// .git, no credentials, no port) so an ssh and an https spelling of one
// repository compare equal. "" means the URL names no forge repository.
func RepoSlug(remote string) string {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return ""
	}
	var host, path string
	if strings.Contains(remote, "://") {
		u, err := url.Parse(remote)
		if err != nil {
			return ""
		}
		host, path = u.Hostname(), u.Path
	} else if at := strings.Index(remote, "@"); at >= 0 && strings.Contains(remote[at:], ":") {
		// scp-like: user@host:owner/name
		rest := remote[at+1:]
		colon := strings.Index(rest, ":")
		host, path = rest[:colon], rest[colon+1:]
	} else {
		return ""
	}
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	parts := strings.Split(path, "/")
	if host == "" || len(parts) < 2 {
		return ""
	}
	return strings.ToLower(host + "/" + strings.Join(parts, "/"))
}
```

`internal/forge/forge.go`:
```go
// Package forge is gg's read-only window onto a code forge's pull requests.
// Everything above Provider is forge-neutral; gh.go is the only file that
// knows GitHub. Owned by domain — frontends never import it.
package forge

import (
	"context"
	"errors"
	"time"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/observ"
)

// CallTimeout bounds every provider subprocess.
const CallTimeout = 30 * time.Second

// ErrNotFound: the forge has no such pull request (deleted, or never existed).
var ErrNotFound = errors.New("forge: pull request not found")

// Provider reads one forge. Implementations never mutate the forge.
type Provider interface {
	Name() string
	// Detect returns nil only when the tool is installed, authenticated and
	// able to read THIS repository's pull requests.
	Detect(ctx context.Context) error
	ListOpen(ctx context.Context) ([]model.PullRequest, error)
	PR(ctx context.Context, n int) (model.PullRequest, error)
	Comments(ctx context.Context, n int) (cs []model.ForgeComment, truncated bool, err error)
	// BaseRepo names the repository PRs target: its RepoSlug and a clone URL
	// to fall back on when no configured remote matches.
	BaseRepo(ctx context.Context) (slug, fallbackURL string, err error)
	// HeadRefspec is the server-side ref holding PR n's head.
	HeadRefspec(n int) string
}

// Default is the provider list, in probe order.
func Default(workDir string, rec observ.Recorder) []Provider {
	return []Provider{NewGH(workDir, rec)}
}
```
(`NewGH` arrives in Task 3; until then `Default` does not compile — so in this
task leave `Default` out and add it in Task 3 Step 3.)

- [ ] **Step 4:** `rtk go test ./internal/forge/` → PASS.
- [ ] **Step 5: commit** — `feat(forge): Provider contract and remote-URL slug`.

---

### Task 3: `gh` provider + parsers + fake `gh` + archtest rule

**Files:**
- Create: `internal/forge/gh.go`, `internal/forge/gh_parse.go`,
  `internal/forge/testdata/pr-list.json`, `pr-view-7.json`, `threads-7.json`,
  `repo-view.json`, `internal/forge/testdata/fakegh/main.go`,
  `internal/forge/forgetest/forgetest.go`
- Modify: `internal/forge/forge.go` (add `Default`),
  `internal/archtest/import_guard_test.go:18-19` (add the forge row)
- Test: `internal/forge/gh_parse_test.go`, `internal/forge/gh_test.go`

**Interfaces — Produces:**
```go
func NewGH(workDir string, rec observ.Recorder) *GH
func NewGHWithRunner(r gitexec.Runner) *GH          // tests: FakeRunner
// span names (FakeRunner keys): "gh pr list (detect)", "gh pr list",
// "gh pr view", "gh api graphql (threads)", "gh repo view"
// forgetest:
func BuildFakeGH(t testing.TB) string               // abs path of the built binary, built once per process
func Seed(t testing.TB, dir string, files map[string]string)  // writes dir/<name>
const EnvBin = "GG_GH_BIN"; const EnvFixtures = "GG_FAKEGH_DIR"
```

- [ ] **Step 1: fixtures.** Write these exact files.

`internal/forge/testdata/pr-list.json`:
```json
[
  {"number":7,"title":"Add forge tab","author":{"login":"alice"},"state":"OPEN","isDraft":false,
   "reviewDecision":"CHANGES_REQUESTED","headRefName":"feat/forge","isCrossRepository":false,
   "headRepositoryOwner":{"login":"homeend"},"headRepository":{"name":"gigagit"},
   "baseRefName":"main","baseRefOid":"1111111111111111111111111111111111111111",
   "headRefOid":"2222222222222222222222222222222222222222",
   "url":"https://github.com/homeend/gigagit/pull/7",
   "createdAt":"2026-09-01T10:00:00Z","updatedAt":"2026-09-02T11:30:00Z"},
  {"number":9,"title":"Fork fix","author":{"login":"bob"},"state":"OPEN","isDraft":true,
   "reviewDecision":"","headRefName":"fix/typo","isCrossRepository":true,
   "headRepositoryOwner":{"login":"bob"},"headRepository":{"name":"gigagit"},
   "baseRefName":"main","baseRefOid":"1111111111111111111111111111111111111111",
   "headRefOid":"3333333333333333333333333333333333333333",
   "url":"https://github.com/homeend/gigagit/pull/9",
   "createdAt":"2026-09-03T10:00:00Z","updatedAt":"2026-09-03T10:00:00Z"}
]
```

`internal/forge/testdata/pr-view-7.json`: the first object above plus
`"body":"Adds the tab.\r\n\r\nSecond paragraph."` and `"state":"MERGED"`.

`internal/forge/testdata/repo-view.json`:
```json
{"nameWithOwner":"homeend/gigagit","url":"https://github.com/homeend/gigagit","sshUrl":"git@github.com:homeend/gigagit.git"}
```

`internal/forge/testdata/threads-7.json`:
```json
{"data":{"repository":{"pullRequest":{
 "reviewThreads":{"pageInfo":{"hasNextPage":false},"nodes":[
  {"path":"a.go","line":12,"startLine":10,"diffSide":"RIGHT","subjectType":"LINE","isResolved":false,"isOutdated":false,
   "comments":{"pageInfo":{"hasNextPage":false},"nodes":[
    {"id":"C1","replyTo":null,"author":{"login":"carol"},"body":"rename this","diffHunk":"@@ -1 +1 @@\n+x","createdAt":"2026-09-02T09:00:00Z","updatedAt":"2026-09-02T09:00:00Z"},
    {"id":"C2","replyTo":{"id":"C1"},"author":{"login":"alice"},"body":"done","diffHunk":"@@ -1 +1 @@\n+x","createdAt":"2026-09-02T09:30:00Z","updatedAt":"2026-09-02T09:30:00Z"}]}},
  {"path":"b.go","line":null,"startLine":null,"diffSide":"RIGHT","subjectType":"FILE","isResolved":true,"isOutdated":false,
   "comments":{"pageInfo":{"hasNextPage":false},"nodes":[
    {"id":"C3","replyTo":null,"author":{"login":"carol"},"body":"split this file","diffHunk":"","createdAt":"2026-09-02T10:00:00Z","updatedAt":"2026-09-02T10:00:00Z"}]}},
  {"path":"a.go","line":null,"startLine":null,"diffSide":"LEFT","subjectType":"LINE","isResolved":false,"isOutdated":true,
   "comments":{"pageInfo":{"hasNextPage":false},"nodes":[
    {"id":"C4","replyTo":null,"author":{"login":null},"body":"old remark","diffHunk":"@@ -5,2 +5,2 @@\n-old\n+new","createdAt":"2026-09-01T12:00:00Z","updatedAt":"2026-09-01T12:00:00Z"}]}}]},
 "comments":{"pageInfo":{"hasNextPage":false},"nodes":[
  {"id":"G1","author":{"login":"bob"},"body":"nice work","createdAt":"2026-09-02T08:00:00Z","updatedAt":"2026-09-02T08:00:00Z"}]},
 "reviews":{"pageInfo":{"hasNextPage":true},"nodes":[
  {"id":"R1","author":{"login":"carol"},"body":"","state":"COMMENTED","submittedAt":"2026-09-02T09:00:00Z"},
  {"id":"R2","author":{"login":"carol"},"body":"needs the rename","state":"CHANGES_REQUESTED","submittedAt":"2026-09-02T09:05:00Z"},
  {"id":"R3","author":{"login":"dave"},"body":"","state":"APPROVED","submittedAt":"2026-09-02T12:00:00Z"}]}
}}}}
```

- [ ] **Step 2: failing parser tests** — `internal/forge/gh_parse_test.go`

```go
package forge

import (
	"os"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParsePRList(t *testing.T) {
	t.Parallel()
	prs, err := parsePRList(fixture(t, "pr-list.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(prs) != 2 {
		t.Fatalf("len = %d, want 2", len(prs))
	}
	a, b := prs[0], prs[1]
	if a.Number != 7 || a.Author != "alice" || a.State != model.PRStateOpen ||
		a.ReviewState != "changes_requested" || a.Source != "feat/forge" || a.SourceRepo != "" ||
		a.Target != "main" || a.HeadSHA[:1] != "2" || a.BaseSHA[:1] != "1" || a.Updated.IsZero() {
		t.Errorf("pr 7 = %+v", a)
	}
	if !b.Draft || b.SourceRepo != "bob/gigagit" || b.ReviewState != "" {
		t.Errorf("pr 9 = %+v", b)
	}
}

func TestParsePRViewMergedAndBody(t *testing.T) {
	t.Parallel()
	p, err := parsePR(fixture(t, "pr-view-7.json"))
	if err != nil {
		t.Fatal(err)
	}
	if p.State != model.PRStateMerged {
		t.Errorf("state = %q", p.State)
	}
	if p.Body != "Adds the tab.\n\nSecond paragraph." { // CRLF normalised
		t.Errorf("body = %q", p.Body)
	}
}

func TestParseThreads(t *testing.T) {
	t.Parallel()
	cs, truncated, err := parseThreads(fixture(t, "threads-7.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !truncated {
		t.Error("truncated = false, want true (reviews.hasNextPage)")
	}
	by := map[string]model.ForgeComment{}
	for _, c := range cs {
		by[c.ID] = c
	}
	if c := by["C1"]; c.Kind != model.ForgeCommentInline || c.Path != "a.go" || c.Line != 12 ||
		c.StartLine != 10 || c.Side != model.NoteSideNew || c.ParentID != "" {
		t.Errorf("C1 = %+v", c)
	}
	if c := by["C2"]; c.ParentID != "C1" || c.Line != 12 {
		t.Errorf("C2 = %+v", c)
	}
	if c := by["C3"]; c.Kind != model.ForgeCommentFile || !c.Resolved || c.Line != 0 {
		t.Errorf("C3 = %+v", c)
	}
	if c := by["C4"]; !c.Outdated || c.Side != model.NoteSideOld || c.Author != "ghost" || c.Hunk == "" {
		t.Errorf("C4 = %+v", c)
	}
	if c := by["G1"]; c.Kind != model.ForgeCommentGeneral {
		t.Errorf("G1 = %+v", c)
	}
	if _, ok := by["R1"]; ok {
		t.Error("R1 (COMMENTED, empty body) must be dropped — it is only the envelope of inline comments")
	}
	if c := by["R2"]; c.Kind != model.ForgeCommentReview || c.Verdict != "changes_requested" {
		t.Errorf("R2 = %+v", c)
	}
	if c := by["R3"]; c.Verdict != "approved" { // empty body kept: a verdict is news
		t.Errorf("R3 = %+v", c)
	}
}

func TestParseThreadsNullPullRequestIsNotFound(t *testing.T) {
	t.Parallel()
	_, _, err := parseThreads([]byte(`{"data":{"repository":{"pullRequest":null}}}`))
	if err != ErrNotFound {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}
```

- [ ] **Step 3:** `rtk go test ./internal/forge/` → FAIL (undefined parsers).

- [ ] **Step 4: implement parsers** — `internal/forge/gh_parse.go`

```go
package forge

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/model"
)

type ghLogin struct {
	Login *string `json:"login"`
}

// name: GitHub nulls the author of a deleted account; it renders as "ghost".
func (l *ghLogin) name() string {
	if l == nil || l.Login == nil || *l.Login == "" {
		return "ghost"
	}
	return *l.Login
}

type ghPR struct {
	Number              int      `json:"number"`
	Title               string   `json:"title"`
	Body                string   `json:"body"`
	Author              *ghLogin `json:"author"`
	State               string   `json:"state"`
	IsDraft             bool     `json:"isDraft"`
	ReviewDecision      string   `json:"reviewDecision"`
	HeadRefName         string   `json:"headRefName"`
	IsCrossRepository   bool     `json:"isCrossRepository"`
	HeadRepositoryOwner *ghLogin `json:"headRepositoryOwner"`
	HeadRepository      *struct {
		Name string `json:"name"`
	} `json:"headRepository"`
	BaseRefName string    `json:"baseRefName"`
	BaseRefOid  string    `json:"baseRefOid"`
	HeadRefOid  string    `json:"headRefOid"`
	URL         string    `json:"url"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

func (g ghPR) model() model.PullRequest {
	p := model.PullRequest{
		Number: g.Number, Title: g.Title, Body: normText(g.Body), Author: g.Author.name(),
		State: strings.ToLower(g.State), Draft: g.IsDraft,
		ReviewState: strings.ToLower(g.ReviewDecision),
		Source:      g.HeadRefName, Target: g.BaseRefName,
		HeadSHA: g.HeadRefOid, BaseSHA: g.BaseRefOid, URL: g.URL,
		Created: g.CreatedAt, Updated: g.UpdatedAt,
	}
	if g.IsCrossRepository && g.HeadRepository != nil {
		p.SourceRepo = g.HeadRepositoryOwner.name() + "/" + g.HeadRepository.Name
	}
	return p
}

// normText normalises forge text: CRLF/CR → LF, surrounding blank space cut.
func normText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.TrimSpace(strings.ReplaceAll(s, "\r", "\n"))
}

func parsePRList(b []byte) ([]model.PullRequest, error) {
	var raw []ghPR
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	out := make([]model.PullRequest, 0, len(raw))
	for _, g := range raw {
		out = append(out, g.model())
	}
	return out, nil
}

func parsePR(b []byte) (model.PullRequest, error) {
	var g ghPR
	if err := json.Unmarshal(b, &g); err != nil {
		return model.PullRequest{}, err
	}
	return g.model(), nil
}

type ghPage struct {
	HasNextPage bool `json:"hasNextPage"`
}

type ghThreads struct {
	Data struct {
		Repository struct {
			PullRequest *struct {
				ReviewThreads struct {
					PageInfo ghPage `json:"pageInfo"`
					Nodes    []struct {
						Path        string `json:"path"`
						Line        *int   `json:"line"`
						StartLine   *int   `json:"startLine"`
						DiffSide    string `json:"diffSide"`
						SubjectType string `json:"subjectType"`
						IsResolved  bool   `json:"isResolved"`
						IsOutdated  bool   `json:"isOutdated"`
						Comments    struct {
							PageInfo ghPage `json:"pageInfo"`
							Nodes    []struct {
								ID      string `json:"id"`
								ReplyTo *struct {
									ID string `json:"id"`
								} `json:"replyTo"`
								Author    *ghLogin  `json:"author"`
								Body      string    `json:"body"`
								DiffHunk  string    `json:"diffHunk"`
								CreatedAt time.Time `json:"createdAt"`
								UpdatedAt time.Time `json:"updatedAt"`
							} `json:"nodes"`
						} `json:"comments"`
					} `json:"nodes"`
				} `json:"reviewThreads"`
				Comments struct {
					PageInfo ghPage `json:"pageInfo"`
					Nodes    []struct {
						ID        string    `json:"id"`
						Author    *ghLogin  `json:"author"`
						Body      string    `json:"body"`
						CreatedAt time.Time `json:"createdAt"`
						UpdatedAt time.Time `json:"updatedAt"`
					} `json:"nodes"`
				} `json:"comments"`
				Reviews struct {
					PageInfo ghPage `json:"pageInfo"`
					Nodes    []struct {
						ID          string    `json:"id"`
						Author      *ghLogin  `json:"author"`
						Body        string    `json:"body"`
						State       string    `json:"state"`
						SubmittedAt time.Time `json:"submittedAt"`
					} `json:"nodes"`
				} `json:"reviews"`
			} `json:"pullRequest"`
		} `json:"repository"`
	} `json:"data"`
}

func parseThreads(b []byte) ([]model.ForgeComment, bool, error) {
	var raw ghThreads
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, false, err
	}
	pr := raw.Data.Repository.PullRequest
	if pr == nil {
		return nil, false, ErrNotFound
	}
	truncated := pr.ReviewThreads.PageInfo.HasNextPage || pr.Comments.PageInfo.HasNextPage || pr.Reviews.PageInfo.HasNextPage
	var out []model.ForgeComment
	for _, th := range pr.ReviewThreads.Nodes {
		truncated = truncated || th.Comments.PageInfo.HasNextPage
		kind := model.ForgeCommentInline
		if th.SubjectType == "FILE" {
			kind = model.ForgeCommentFile
		}
		side := model.NoteSideNew
		if th.DiffSide == "LEFT" {
			side = model.NoteSideOld
		}
		line, start := 0, 0
		if th.Line != nil {
			line, start = *th.Line, *th.Line
		}
		if th.StartLine != nil {
			start = *th.StartLine
		}
		for _, c := range th.Comments.Nodes {
			fc := model.ForgeComment{
				ID: c.ID, Kind: kind, Author: c.Author.name(), Body: normText(c.Body),
				Path: th.Path, Side: side, Line: line, StartLine: start,
				Outdated: th.IsOutdated, Resolved: th.IsResolved, Hunk: c.DiffHunk,
				Created: c.CreatedAt, Updated: c.UpdatedAt,
			}
			if c.ReplyTo != nil {
				fc.ParentID = c.ReplyTo.ID
			}
			out = append(out, fc)
		}
	}
	for _, c := range pr.Comments.Nodes {
		out = append(out, model.ForgeComment{
			ID: c.ID, Kind: model.ForgeCommentGeneral, Author: c.Author.name(),
			Body: normText(c.Body), Created: c.CreatedAt, Updated: c.UpdatedAt,
		})
	}
	for _, r := range pr.Reviews.Nodes {
		verdict := strings.ToLower(r.State)
		if verdict == "pending" || (verdict == "commented" && strings.TrimSpace(r.Body) == "") {
			continue // the bare envelope of inline comments carries no news
		}
		out = append(out, model.ForgeComment{
			ID: r.ID, Kind: model.ForgeCommentReview, Author: r.Author.name(),
			Body: normText(r.Body), Verdict: verdict, Created: r.SubmittedAt, Updated: r.SubmittedAt,
		})
	}
	return out, truncated, nil
}
```

- [ ] **Step 5:** `rtk go test ./internal/forge/` → parser tests PASS.

- [ ] **Step 6: failing provider tests** — `internal/forge/gh_test.go`

```go
package forge

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
)

func TestGHArgvIsReadOnly(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	var seen [][]string
	rec := func(out string) func(context.Context, []string) (gitexec.Result, error) {
		return func(_ context.Context, argv []string) (gitexec.Result, error) {
			seen = append(seen, argv)
			return gitexec.Result{Stdout: out}, nil
		}
	}
	f.SetHandler("gh pr list (detect)", rec("[]"))
	f.SetHandler("gh pr list", rec(string(fixture(t, "pr-list.json"))))
	f.SetHandler("gh pr view", rec(string(fixture(t, "pr-view-7.json"))))
	f.SetHandler("gh api graphql (threads)", rec(string(fixture(t, "threads-7.json"))))
	f.SetHandler("gh repo view", rec(string(fixture(t, "repo-view.json"))))
	g := NewGHWithRunner(f)
	ctx := context.Background()
	if err := g.Detect(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := g.ListOpen(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := g.PR(ctx, 7); err != nil {
		t.Fatal(err)
	}
	if _, _, err := g.Comments(ctx, 7); err != nil {
		t.Fatal(err)
	}
	slug, fallback, err := g.BaseRepo(ctx)
	if err != nil || slug != "github.com/homeend/gigagit" || fallback != "git@github.com:homeend/gigagit.git" {
		t.Errorf("BaseRepo = %q %q %v", slug, fallback, err)
	}
	if len(seen) != 5 {
		t.Fatalf("calls = %d, want 5", len(seen))
	}
	for _, argv := range seen {
		for _, bad := range []string{"-X", "--method", "comment", "review", "edit", "merge", "close", "reopen", "create"} {
			if slices.Contains(argv, bad) {
				t.Errorf("argv %v contains mutating token %q", argv, bad)
			}
		}
	}
	if !slices.Contains(seen[1], "open") || !slices.Contains(seen[1], "100") {
		t.Errorf("list argv = %v", seen[1])
	}
	if !slices.Contains(seen[3], "number=7") || !slices.Contains(seen[3], "owner={owner}") {
		t.Errorf("threads argv = %v", seen[3])
	}
	if g.HeadRefspec(7) != "pull/7/head" {
		t.Errorf("refspec = %q", g.HeadRefspec(7))
	}
}

func TestGHDetectFailureIsAnError(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	f.SetError("gh pr list (detect)", errors.New("exit status 4: not logged in"))
	if err := NewGHWithRunner(f).Detect(context.Background()); err == nil {
		t.Fatal("Detect = nil, want an error")
	}
}

func TestGHViewNotFound(t *testing.T) {
	t.Parallel()
	f := gitexec.NewFakeRunner()
	f.SetHandler("gh pr view", func(context.Context, []string) (gitexec.Result, error) {
		return gitexec.Result{Stderr: "GraphQL: Could not resolve to a PullRequest with the number of 99."},
			errors.New("exit status 1")
	})
	_, err := NewGHWithRunner(f).PR(context.Background(), 99)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
	_ = strings.TrimSpace
}
```
Before relying on the last test, open `internal/gitexec/exec.go` around the
`Run` return and check whether a non-zero exit returns the `Result` (with
`Stderr`) alongside the error, or folds stderr into the error text. `PR` below
checks BOTH (`res.Stderr` and `err.Error()`), so either shape passes; adjust
the fake handler only if `FakeRunner.Run` discards the Result on error.

- [ ] **Step 7: implement provider** — `internal/forge/gh.go`

```go
package forge

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/observ"
)

// EnvBin overrides the gh binary (tests, e2e, an unusual install).
const EnvBin = "GG_GH_BIN"

const prFields = "number,title,author,state,isDraft,reviewDecision,headRefName,isCrossRepository," +
	"headRepositoryOwner,headRepository,baseRefName,baseRefOid,headRefOid,url,createdAt,updatedAt"

// threadsQuery is single-page by design (plan deviation 3): hasNextPage on any
// connection surfaces as truncated rather than a pagination loop.
const threadsQuery = `query($owner:String!,$name:String!,$number:Int!){repository(owner:$owner,name:$name){pullRequest(number:$number){
reviewThreads(first:100){pageInfo{hasNextPage} nodes{path line startLine diffSide subjectType isResolved isOutdated
comments(first:50){pageInfo{hasNextPage} nodes{id replyTo{id} author{login} body diffHunk createdAt updatedAt}}}}
comments(first:100){pageInfo{hasNextPage} nodes{id author{login} body createdAt updatedAt}}
reviews(first:100){pageInfo{hasNextPage} nodes{id author{login} body state submittedAt}}}}}`

// GH reads GitHub through the system gh CLI.
type GH struct{ r gitexec.Runner }

// NewGH builds the production provider. The runner is gitexec's: it is a
// generic subprocess runner whose binary path is a constructor argument.
func NewGH(workDir string, rec observ.Recorder) *GH {
	bin := os.Getenv(EnvBin)
	if bin == "" {
		bin = "gh"
	}
	return &GH{r: gitexec.NewExecRunner(bin, workDir, rec)}
}

func NewGHWithRunner(r gitexec.Runner) *GH { return &GH{r: r} }

func (g *GH) Name() string             { return "github" }
func (g *GH) HeadRefspec(n int) string { return "pull/" + strconv.Itoa(n) + "/head" }

func (g *GH) run(ctx context.Context, name string, argv ...string) (gitexec.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	return g.r.Run(ctx, name, argv)
}

func (g *GH) Detect(ctx context.Context) error {
	_, err := g.run(ctx, "gh pr list (detect)", "pr", "list", "--limit", "1", "--json", "number")
	return err
}

func (g *GH) ListOpen(ctx context.Context) ([]model.PullRequest, error) {
	res, err := g.run(ctx, "gh pr list", "pr", "list", "--state", "open", "--limit", "100", "--json", prFields)
	if err != nil {
		return nil, err
	}
	return parsePRList([]byte(res.Stdout))
}

func (g *GH) PR(ctx context.Context, n int) (model.PullRequest, error) {
	res, err := g.run(ctx, "gh pr view", "pr", "view", strconv.Itoa(n), "--json", prFields+",body")
	if err != nil {
		if strings.Contains(res.Stderr+err.Error(), "Could not resolve to a PullRequest") {
			return model.PullRequest{}, fmt.Errorf("#%d: %w", n, ErrNotFound)
		}
		return model.PullRequest{}, err
	}
	return parsePR([]byte(res.Stdout))
}

func (g *GH) Comments(ctx context.Context, n int) ([]model.ForgeComment, bool, error) {
	// {owner}/{repo} are gh's own placeholders, filled from its repo resolution.
	res, err := g.run(ctx, "gh api graphql (threads)", "api", "graphql",
		"-F", "owner={owner}", "-F", "name={repo}", "-F", "number="+strconv.Itoa(n),
		"-f", "query="+threadsQuery)
	if err != nil {
		return nil, false, err
	}
	return parseThreads([]byte(res.Stdout))
}

func (g *GH) BaseRepo(ctx context.Context) (string, string, error) {
	res, err := g.run(ctx, "gh repo view", "repo", "view", "--json", "nameWithOwner,url,sshUrl")
	if err != nil {
		return "", "", err
	}
	var v struct{ URL, SSHURL string }
	if err := jsonUnmarshalFold([]byte(res.Stdout), &v); err != nil {
		return "", "", err
	}
	fallback := v.SSHURL
	if fallback == "" {
		fallback = v.URL
	}
	return RepoSlug(v.URL), fallback, nil
}
```
`jsonUnmarshalFold` is just `json.Unmarshal` — encoding/json already matches
`url`/`sshUrl` to `URL`/`SSHURL` case-insensitively; write
`json.Unmarshal` directly and import `encoding/json` (do not create the
helper). The test's argv assertion `owner={owner}` pins the placeholder.

Add `Default` to `forge.go` now (code in Task 2 Step 3).

- [ ] **Step 8: fake gh** — `internal/forge/testdata/fakegh/main.go`

```go
// fakegh stands in for the gh CLI in tests: it maps an invocation to a canned
// JSON file under $GG_FAKEGH_DIR, else <cwd>/.git/fakegh. A missing fixture
// dir or file exits 1 — exactly how a box without a usable gh behaves.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	dir := os.Getenv("GG_FAKEGH_DIR")
	if dir == "" {
		dir = filepath.Join(".git", "fakegh")
	}
	a := os.Args[1:]
	name := ""
	switch {
	case len(a) >= 2 && a[0] == "pr" && a[1] == "list":
		name = "pr-list.json"
	case len(a) >= 3 && a[0] == "pr" && a[1] == "view":
		name = "pr-view-" + a[2] + ".json"
	case len(a) >= 2 && a[0] == "repo" && a[1] == "view":
		name = "repo-view.json"
	case len(a) >= 2 && a[0] == "api" && a[1] == "graphql":
		for _, s := range a {
			if n, ok := strings.CutPrefix(s, "number="); ok {
				name = "threads-" + n + ".json"
			}
		}
	}
	if name == "" {
		fmt.Fprintln(os.Stderr, "fakegh: unsupported invocation:", strings.Join(a, " "))
		os.Exit(2)
	}
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		if strings.HasPrefix(name, "pr-view-") {
			fmt.Fprintln(os.Stderr, "GraphQL: Could not resolve to a PullRequest with the number of "+a[2]+".")
		} else {
			fmt.Fprintln(os.Stderr, "fakegh:", err)
		}
		os.Exit(1)
	}
	os.Stdout.Write(b)
}
```

`internal/forge/forgetest/forgetest.go`:
```go
// Package forgetest builds the fake gh binary once per test process and seeds
// its fixture directory. Test-only; imported by forge, domain, cli and e2e tests.
package forgetest

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

const (
	EnvBin      = "GG_GH_BIN"
	EnvFixtures = "GG_FAKEGH_DIR"
)

var (
	once    sync.Once
	binPath string
	binErr  error
)

// BuildFakeGH returns the fake gh's absolute path, building it on first use.
func BuildFakeGH(t testing.TB) string {
	t.Helper()
	once.Do(func() {
		_, self, _, _ := runtime.Caller(0)
		src := filepath.Join(filepath.Dir(self), "..", "testdata", "fakegh")
		dir, err := os.MkdirTemp("", "gg-fakegh")
		if err != nil {
			binErr = err
			return
		}
		binPath = filepath.Join(dir, "gh")
		if runtime.GOOS == "windows" {
			binPath += ".exe"
		}
		cmd := exec.Command("go", "build", "-o", binPath, ".")
		cmd.Dir = src
		if out, err := cmd.CombinedOutput(); err != nil {
			binErr = &buildError{string(out), err}
		}
	})
	if binErr != nil {
		t.Fatalf("build fakegh: %v", binErr)
	}
	return binPath
}

type buildError struct {
	out string
	err error
}

func (e *buildError) Error() string { return e.err.Error() + "\n" + e.out }

// Seed writes name→content fixture files into dir (created if missing).
func Seed(t testing.TB, dir string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
```

Add to `internal/forge/gh_test.go` a real-subprocess test:
```go
// Serial: t.Setenv.
func TestGHAgainstFakeBinary(t *testing.T) {
	bin := forgetest.BuildFakeGH(t)
	dir := t.TempDir()
	forgetest.Seed(t, dir, map[string]string{
		"pr-list.json": string(fixture(t, "pr-list.json")),
		"pr-view-7.json": string(fixture(t, "pr-view-7.json")),
	})
	t.Setenv(forgetest.EnvBin, bin)
	t.Setenv(forgetest.EnvFixtures, dir)
	g := NewGH(t.TempDir(), nil)
	ctx := context.Background()
	if err := g.Detect(ctx); err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if prs, err := g.ListOpen(ctx); err != nil || len(prs) != 2 {
		t.Fatalf("ListOpen = %d, %v", len(prs), err)
	}
	if _, err := g.PR(ctx, 99); !errors.Is(err, ErrNotFound) {
		t.Errorf("PR(99) err = %v, want ErrNotFound", err)
	}
	t.Setenv(forgetest.EnvFixtures, t.TempDir()) // empty dir = gh that cannot read the repo
	if err := g.Detect(ctx); err == nil {
		t.Error("Detect against an empty fixture dir = nil, want an error")
	}
}
```
(import `github.com/homeend/gigagit/internal/forge/forgetest`; this makes it
an external-style import of a sibling — fine, `forgetest` does not import
`forge`.)

- [ ] **Step 9: archtest.** In `internal/archtest/import_guard_test.go`, add
next to the `notes`/`preview` rows:
```go
"github.com/homeend/gigagit/internal/forge":      "frontends must reach the forge through internal/domain",
```
Read the surrounding test first: if the guard walks `_test.go` files too,
CLI tests must keep using the fake binary only (they do, Task 9).

- [ ] **Step 10:** `rtk go test ./internal/forge/... ./internal/archtest/` → PASS. `rtk go vet ./internal/forge/...` clean.
- [ ] **Step 11: commit** — `feat(forge): gh provider, JSON parsers, fake gh for tests`.

---

### Task 4: preflight — `Silent` + `ForgeUsable`

**Files:**
- Modify: `internal/preflight/preflight.go` (Probes ~l.66, Feature ~l.197, after GitVersion ~l.175), `internal/tui/notify.go:229`
- Test: `internal/preflight/preflight_test.go` (append), `internal/tui/notify_test.go` (append; find the existing `featureDisabledNotices` test and mirror its setup)

**Interfaces — Produces:**
```go
type ForgeProbe struct{ Provider, Err string }
// Probes gains:  Forge *ForgeProbe   // nil = not probed (yet)
// Feature gains: Silent bool
type ForgeUsable struct{}            // implements Requirement
```

- [ ] **Step 1: failing tests** (append to `internal/preflight/preflight_test.go`)

```go
func TestForgeUsable(t *testing.T) {
	t.Parallel()
	f := []Feature{{ID: "forge", Criticality: Optional, Silent: true, Requires: []Requirement{ForgeUsable{}}}}
	for name, tc := range map[string]struct {
		p    Probes
		want State
	}{
		"unprobed":  {Probes{}, Unsatisfiable},
		"missing":   {Probes{Forge: &ForgeProbe{Err: "gh: not found"}}, Unsatisfiable},
		"available": {Probes{Forge: &ForgeProbe{Provider: "github"}}, Satisfied},
	} {
		v := Resolve(f, tc.p)[0]
		if v.State != tc.want {
			t.Errorf("%s: state = %v, want %v", name, v.State, tc.want)
		}
		if !v.Feature.Silent {
			t.Errorf("%s: Silent lost", name)
		}
	}
	if NeedsStoreProbes(f) {
		t.Error("ForgeUsable must not need store probes")
	}
}
```

- [ ] **Step 2:** `rtk go test ./internal/preflight/` → FAIL.

- [ ] **Step 3: implement.** In `preflight.go`:

```go
// ForgeProbe is what detection learned about a code forge for this repo.
// Provider is the usable provider's name ("" = none); Err says why not.
type ForgeProbe struct {
	Provider string
	Err      string
}
```
add `Forge *ForgeProbe` to `Probes` (comment: `// nil = not probed (yet)`), add
to `Feature`:
```go
	// Silent features never surface a "feature unavailable" notice: their
	// absence is the normal state of a box without the external tool.
	Silent bool
```
and after `GitVersion`'s methods:
```go
// ForgeUsable requires a forge CLI that can read this repository's pull
// requests. Unprobed counts as unusable: the feature lights up only on a
// positive verdict.
type ForgeUsable struct{}

func (ForgeUsable) Fit(p Probes) Fit {
	if p.Forge != nil && p.Forge.Provider != "" {
		return FitOK
	}
	return FitTooOld
}

func (ForgeUsable) Reason(p Probes) Text {
	if p.Forge == nil || p.Forge.Err == "" {
		return Text{Format: "no forge CLI can read this repository's pull requests"}
	}
	return Text{Format: "no forge CLI can read this repository's pull requests: %s", Args: []any{p.Forge.Err}}
}

func (ForgeUsable) Remedy(Probes) Text {
	return Text{Format: "install the GitHub CLI and run `gh auth login`, then restart gg"}
}

func (ForgeUsable) RepairStore() string    { return "" }
func (ForgeUsable) NeedsStoreProbes() bool { return false }
```
(`FitOK`/`FitTooOld`/`FitTooNew` verified at `preflight.go:43-47`.)
`FitTooOld` with no `Migrate` resolves to Unsatisfiable (see `Resolve`).

- [ ] **Step 4: notify filter.** In `internal/tui/notify.go` `featureDisabledNotices`:
```go
		if v.State == preflight.Satisfied || v.Feature.Silent {
			continue
		}
```
Add a TUI test beside the existing notices test asserting that a verdict slice
containing a non-satisfied Silent feature yields no notice with id
`feature_disabled_forge`. If `featureDisabledNotices` cannot be fed verdicts
directly (it calls `svc.Preflight`), extract the loop into
`noticesForVerdicts(vs []preflight.Verdict, repoKey string) []notice` and test
that pure function:
```go
func TestSilentFeatureRaisesNoNotice(t *testing.T) {
	t.Parallel()
	vs := []preflight.Verdict{
		{Feature: preflight.Feature{ID: "forge", Silent: true}, State: preflight.Unsatisfiable},
		{Feature: preflight.Feature{ID: "versions"}, State: preflight.Unsatisfiable},
	}
	got := noticesForVerdicts(vs, "k")
	if len(got) != 1 || got[0].id != noticeFeatureDisabledPrefix+"versions" {
		t.Fatalf("notices = %+v", got)
	}
}
```
The two new `Text.Format` strings render through `renderVerdictReason`; run
the i18n gates — if `engine_prose_test`/`i18n_scan_test` demand bundle entries
for preflight prose, add them per the `adding-translations` skill (they are
only ever *rendered* by CLI `gg pr`, in English, but the gate is the judge).

- [ ] **Step 5:** `rtk go test ./internal/preflight/ ./internal/tui/ -run 'ForgeUsable|SilentFeature|i18n|Notice'` → PASS.
- [ ] **Step 6: commit** — `feat(preflight): silent features and the ForgeUsable requirement`.

---

### Task 5: git — PR ref naming + `FetchRefspec`

**Files:**
- Create: `internal/git/prref.go`
- Modify: `internal/engine/gitops.go` (add to the `GitOps` interface beside `Fetch`, l.41)
- Test: `internal/git/prref_test.go`

**Interfaces — Produces:**
```go
const PRRefPrefix = "refs/gg/pr/"
func PRRef(n int) string
func ParsePRRef(ref string) (n int, ok bool)
func (r *Repo) FetchRefspec(ctx context.Context, remote, src, dst string) error
// GitOps gains: FetchRefspec(ctx context.Context, remote, src, dst string) error
```

- [ ] **Step 1: failing tests** — `internal/git/prref_test.go`

```go
package git

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func TestPRRefRoundTrip(t *testing.T) {
	t.Parallel()
	if PRRef(42) != "refs/gg/pr/42" {
		t.Fatalf("PRRef = %q", PRRef(42))
	}
	for ref, want := range map[string]int{"refs/gg/pr/42": 42, "refs/gg/pr/0": -1, "refs/gg/pr/x": -1, "refs/gg/pr/4/2": -1, "refs/heads/42": -1} {
		n, ok := ParsePRRef(ref)
		if (want < 0) == ok || (ok && n != want) {
			t.Errorf("ParsePRRef(%q) = %d,%v", ref, n, ok)
		}
	}
}

func TestFetchRefspecWritesPrivateRef(t *testing.T) {
	t.Parallel()
	upDir, _ := newTestRepo(t) // "base repo" with one commit
	run := func(dir string, args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	head := run(upDir, "rev-parse", "HEAD")
	run(upDir, "update-ref", "refs/pull/7/head", head)
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	if err := repo.FetchRefspec(context.Background(), upDir, "pull/7/head", PRRef(7)); err != nil {
		t.Fatal(err)
	}
	if got := run(dir, "rev-parse", PRRef(7)); got != head {
		t.Errorf("ref = %s, want %s", got, head)
	}
	if _, err := exec.Command("git", "-C", dir, "rev-parse", "--verify", "-q", "FETCH_HEAD").Output(); err == nil {
		t.Error("FETCH_HEAD was written; want --no-write-fetch-head")
	}
}
```
(`newTestRepo` is at `internal/git/repo_test.go:19`; if its repo has no
commit, make one with `run(dir, "commit", "--allow-empty", "-m", "c")`.
`pull/7/head` resolves server-side as `refs/pull/7/head` by git's ref DWIM
for fetch sources — if the fetch fails with "couldn't find remote ref", pass
`refs/pull/7/head` and change `GH.HeadRefspec` + its test to return the
`refs/`-qualified form.)

- [ ] **Step 2:** `rtk go test ./internal/git/ -run 'PRRef|FetchRefspec'` → FAIL.

- [ ] **Step 3: implement** — `internal/git/prref.go`

```go
package git

import (
	"context"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
)

// PRRefPrefix is where gg keeps fetched pull-request heads:
// refs/gg/pr/<number>. Like refs/gg/versions/ it sits outside
// refs/heads|tags|remotes — never pushed, never decorated in the graph
// (--decorate-refs-exclude=refs/gg/*). A ref here is also gg's durable record
// that the user opened that PR.
const PRRefPrefix = "refs/gg/pr/"

// PRRef names PR n's head ref.
func PRRef(n int) string { return PRRefPrefix + strconv.Itoa(n) }

// ParsePRRef is PRRef's inverse; ok=false for anything else.
func ParsePRRef(ref string) (int, bool) {
	s, found := strings.CutPrefix(ref, PRRefPrefix)
	if !found {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 || strconv.Itoa(n) != s {
		return 0, false
	}
	return n, true
}

// FetchRefspec force-fetches one server ref into one local ref
// (`git fetch --no-tags --no-write-fetch-head <remote> +<src>:<dst>`).
// remote may be a configured name or a URL.
func (r *Repo) FetchRefspec(ctx context.Context, remote, src, dst string) error {
	argv := gitcmd.New("fetch").Arg("--no-tags", "--no-write-fetch-head", remote, "+"+src+":"+dst).ToArgv()
	_, err := r.Runner.Run(ctx, "git fetch refspec", argv)
	return err
}
```
Add `FetchRefspec(ctx context.Context, remote, src, dst string) error` to
`engine.GitOps` under `Fetch`. Then `rtk go build ./...` — every hand-written
`GitOps` fake that the compiler flags gets a one-line stub
`func (f *X) FetchRefspec(context.Context, string, string, string) error { return nil }`.

- [ ] **Step 4:** `rtk go test ./internal/git/ -run 'PRRef|FetchRefspec'` and `rtk go build ./... && rtk go vet ./internal/engine/ ./internal/git/` → PASS.
- [ ] **Step 5: commit** — `feat(git): refs/gg/pr naming and the FetchRefspec verb`.

---

### Task 6: engine ops — `FetchPRHead`, `ForgetPR`

**Files:**
- Create: `internal/engine/fetch_pr_head.go`, `internal/engine/forget_pr.go`
- Test: `internal/engine/pr_ops_test.go`

**Interfaces — Consumes:** `git.PRRef`, `GitOps.FetchRefspec`,
`GitOps.RevParse` (`gitops.go:70` — `ResolveCommit` is NOT on `GitOps`; ops
resolve with `RevParse(ctx, ref+"^{commit}")`, which errors on a missing ref),
`GitOps.DeleteRef`. **Produces:**
```go
type FetchPRHead struct{ Remote, Refspec string; Number int; HeadSHA string }
type ForgetPR struct{ Number int }
```

- [ ] **Step 1: failing tests** — `internal/engine/pr_ops_test.go`

```go
package engine

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/repogate"
)

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestFetchPRHeadFetchesThenNoOps(t *testing.T) {
	t.Parallel()
	upDir, _ := newRepo(t)
	head := gitOut(t, upDir, "rev-parse", "HEAD")
	gitOut(t, upDir, "update-ref", "refs/pull/7/head", head)
	dir, repo := newRepo(t)
	op := FetchPRHead{Remote: upDir, Refspec: "pull/7/head", Number: 7, HeadSHA: head}
	if op.LockMode() != repogate.RefWrite {
		t.Fatal("LockMode != RefWrite")
	}
	res, err := op.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil || !res.Changed {
		t.Fatalf("first run = %+v, %v", res, err)
	}
	if got := gitOut(t, dir, "rev-parse", git.PRRef(7)); got != head {
		t.Fatalf("ref = %s", got)
	}
	// Second run: the ref already IS HeadSHA. Point Remote at nothing — a
	// fetch attempt would fail, so success proves no git fetch ran.
	op.Remote = "/nonexistent/remote"
	res, err = op.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil || res.Changed {
		t.Fatalf("second run = %+v, %v (want no-op)", res, err)
	}
}

func TestFetchPRHeadValidates(t *testing.T) {
	t.Parallel()
	_, repo := newRepo(t)
	for _, op := range []FetchPRHead{{Number: 0, Remote: "o", Refspec: "x"}, {Number: 1, Refspec: "x"}, {Number: 1, Remote: "o"}} {
		if _, err := op.Run(context.Background(), OpDeps{Repo: repo}); err == nil {
			t.Errorf("%+v: err = nil", op)
		}
	}
}

func TestForgetPRDeletesOnlyItsRef(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	head := gitOut(t, dir, "rev-parse", "HEAD")
	gitOut(t, dir, "update-ref", git.PRRef(7), head)
	res, err := ForgetPR{Number: 7}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil || !res.Changed {
		t.Fatalf("run = %+v, %v", res, err)
	}
	if out, err := exec.Command("git", "-C", dir, "rev-parse", "--verify", "-q", git.PRRef(7)).Output(); err == nil {
		t.Fatalf("ref survived: %s", out)
	}
	// Forgetting a PR with no ref is a clean no-op, not an error.
	res, err = ForgetPR{Number: 7}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil || res.Changed {
		t.Fatalf("second run = %+v, %v", res, err)
	}
}
```

- [ ] **Step 2:** `rtk go test ./internal/engine/ -run 'FetchPRHead|ForgetPR'` → FAIL.

- [ ] **Step 3: implement.**

`internal/engine/fetch_pr_head.go`:
```go
package engine

import (
	"context"
	"fmt"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/repogate"
)

// FetchPRHead brings pull request Number's head commit into the gg-private
// ref refs/gg/pr/<Number>. Nothing reaches the forge; no branch, index or
// worktree is touched. HeadSHA (when known) makes it idempotent: a ref already
// at that commit costs one rev-parse and no network.
type FetchPRHead struct {
	Remote  string // configured remote name or URL of the BASE repository
	Refspec string // server-side ref of the PR head (provider.HeadRefspec)
	Number  int
	HeadSHA string // optional
}

var _ Operation = FetchPRHead{}

func (op FetchPRHead) LockMode() repogate.Mode { return repogate.RefWrite }

func (op FetchPRHead) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Number <= 0 || op.Remote == "" || op.Refspec == "" {
		return Result{}, fmt.Errorf("fetch pr head: number, remote and refspec are required")
	}
	ref := git.PRRef(op.Number)
	if op.HeadSHA != "" {
		if cur, err := deps.Repo.RevParse(ctx, ref+"^{commit}"); err == nil && cur == op.HeadSHA {
			res := Result{}.WithSummary("pull request #%d is already fetched", op.Number)
			deps.emit(ctx, Done{Result: res})
			return res, nil
		}
	}
	deps.emit(ctx, Progress{Step: "fetching pull request head", Detail: fmt.Sprintf("#%d", op.Number)})
	if err := deps.Repo.FetchRefspec(ctx, op.Remote, op.Refspec, ref); err != nil {
		return Result{}, fmt.Errorf("fetch pr head #%d: %w", op.Number, err)
	}
	res := Result{Changed: true}.WithSummary("fetched pull request #%d", op.Number)
	deps.emit(ctx, Done{Result: res})
	return res, nil
}
```

`internal/engine/forget_pr.go`:
```go
package engine

import (
	"context"
	"fmt"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/repogate"
)

// ForgetPR drops gg's local record of a pull request: its refs/gg/pr/<n> ref.
// The ref name is built here from the number, so this op can never delete
// anything outside the PR namespace.
type ForgetPR struct{ Number int }

var _ Operation = ForgetPR{}

func (op ForgetPR) LockMode() repogate.Mode { return repogate.RefWrite }

func (op ForgetPR) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Number <= 0 {
		return Result{}, fmt.Errorf("forget pr: a pull request number is required")
	}
	ref := git.PRRef(op.Number)
	if _, err := deps.Repo.RevParse(ctx, ref+"^{commit}"); err != nil {
		res := Result{}.WithSummary("pull request #%d was not fetched", op.Number)
		deps.emit(ctx, Done{Result: res})
		return res, nil
	}
	if err := deps.Repo.DeleteRef(ctx, ref); err != nil {
		return Result{}, fmt.Errorf("forget pr #%d: %w", op.Number, err)
	}
	res := Result{Changed: true}.WithSummary("forgot pull request #%d", op.Number)
	deps.emit(ctx, Done{Result: res})
	return res, nil
}
```
`WithSummary` strings are engine prose: run
`rtk go test ./internal/tui/ -run 'EngineProse|i18n'`. The gate will name any
summary/progress string missing from the four bundles — add them following
the `adding-translations` skill (this is the one TUI-facing touch in plan 1).

- [ ] **Step 4:** `rtk go test ./internal/engine/ -run 'FetchPRHead|ForgetPR'` → PASS.
- [ ] **Step 5: commit** — `feat(engine): FetchPRHead and ForgetPR ref-only ops`.

---

### Task 7: domain — detection, once

**Files:**
- Create: `internal/domain/forge.go`
- Modify: `internal/domain/features.go` (add `FeatureForge`), `internal/domain/preflight.go` (`probesFrom`/`preflightProbes` fill `Probes.Forge`), `internal/domain/service.go` (fields)
- Test: `internal/domain/forge_test.go`

**Interfaces — Produces:**
```go
const FeatureForge = "forge"
type ForgeStatus struct{ Provider string; Err error }
func (st ForgeStatus) Available() bool
func (s *Service) ForgeStatus(ctx context.Context) ForgeStatus   // probes once; later calls return the cache
func (s *Service) SetForgeProviders(ps []forge.Provider)         // test seam; must precede the first ForgeStatus
var ErrForgeUnavailable = errors.New("no forge CLI can read this repository's pull requests")
```

- [ ] **Step 1: failing test** — `internal/domain/forge_test.go`

```go
package domain

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/homeend/gigagit/internal/forge"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/preflight"
)

// fakeForge is a scriptable forge.Provider.
type fakeForge struct {
	mu        sync.Mutex
	detectErr error
	detects   atomic.Int32
	open      []model.PullRequest
	byNum     map[int]model.PullRequest // PR(n); missing → forge.ErrNotFound
	prCalls   map[int]int
	comments  []model.ForgeComment
	truncated bool
	slug, url string
}

func (f *fakeForge) Name() string { return "fake" }
func (f *fakeForge) Detect(context.Context) error { f.detects.Add(1); return f.detectErr }
func (f *fakeForge) ListOpen(context.Context) ([]model.PullRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]model.PullRequest(nil), f.open...), nil
}
func (f *fakeForge) PR(_ context.Context, n int) (model.PullRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.prCalls == nil {
		f.prCalls = map[int]int{}
	}
	f.prCalls[n]++
	p, ok := f.byNum[n]
	if !ok {
		return model.PullRequest{}, forge.ErrNotFound
	}
	return p, nil
}
func (f *fakeForge) Comments(context.Context, int) ([]model.ForgeComment, bool, error) {
	return f.comments, f.truncated, nil
}
func (f *fakeForge) BaseRepo(context.Context) (string, string, error) { return f.slug, f.url, nil }
func (f *fakeForge) HeadRefspec(n int) string                          { return "pull/x/head" }

func TestForgeStatusProbesOnce(t *testing.T) {
	t.Parallel()
	svc := newForgeSvc(t, &fakeForge{detectErr: errors.New("not logged in")})
	ff := svc.forgeProviders[0].(*fakeForge)
	for range 3 {
		if st := svc.ForgeStatus(context.Background()); st.Available() || st.Err == nil {
			t.Fatalf("status = %+v", st)
		}
	}
	if n := ff.detects.Load(); n != 1 {
		t.Errorf("Detect ran %d times, want 1 (no re-detection)", n)
	}
}

func TestForgeFeatureVerdictFollowsStatus(t *testing.T) {
	t.Parallel()
	svc := newForgeSvc(t, &fakeForge{})
	verdict := func() preflight.Verdict {
		vs, err := svc.Preflight(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range vs {
			if v.Feature.ID == FeatureForge {
				return v
			}
		}
		t.Fatal("no forge verdict")
		return preflight.Verdict{}
	}
	if v := verdict(); v.State == preflight.Satisfied || !v.Feature.Silent {
		t.Errorf("before the probe: %+v (Preflight must never run the network probe itself)", v)
	}
	svc.ForgeStatus(context.Background())
	if v := verdict(); v.State != preflight.Satisfied {
		t.Errorf("after the probe: %+v", v)
	}
}
```
`newForgeSvc` (same file):
```go
func newForgeSvc(t *testing.T, ff *fakeForge) *Service {
	t.Helper()
	svc := newTestSvc(t) // see note below
	svc.SetForgeProviders([]forge.Provider{ff})
	return svc
}
```
`newTestSvc` does not exist: use `newRealRepo(t) (string, *Service)`
(`compare_test.go:20`, verified) — `_, svc := newRealRepo(t)`.

- [ ] **Step 2:** `rtk go test ./internal/domain/ -run Forge` → FAIL.

- [ ] **Step 3: implement.**

`features.go`: add `FeatureForge = "forge"` to the const block and append
```go
		{
			ID:          FeatureForge,
			Criticality: preflight.Optional,
			Silent:      true, // no gh is the normal case, not a problem to report
			Requires:    []preflight.Requirement{preflight.ForgeUsable{}},
		},
```
`service.go` — new fields on `Service`:
```go
	forgeMu        sync.Mutex
	forgeProviders []forge.Provider // nil = forge.Default; tests inject
	forgeProbed    bool
	forgeActive    forge.Provider // nil when none is usable
	forgeErr       error
	forgeSeen      map[int]bool              // PR numbers listed open this session
	forgeTerminal  map[int]model.PullRequest // cached closed/merged/unavailable reads
```
`internal/domain/forge.go`:
```go
package domain

import (
	"context"
	"errors"

	"github.com/homeend/gigagit/internal/forge"
)

// ErrForgeUnavailable: detection found no usable forge CLI for this repo.
var ErrForgeUnavailable = errors.New("no forge CLI can read this repository's pull requests")

// ForgeStatus is the once-per-session detection verdict.
type ForgeStatus struct {
	Provider string // "" = none usable
	Err      error  // why not (diagnostics; the TUI stays silent)
}

func (st ForgeStatus) Available() bool { return st.Provider != "" }

// SetForgeProviders replaces the provider list. Test seam; call before the
// first ForgeStatus.
func (s *Service) SetForgeProviders(ps []forge.Provider) {
	s.forgeMu.Lock()
	defer s.forgeMu.Unlock()
	s.forgeProviders = ps
}

// ForgeStatus probes the forge providers on its FIRST call and answers from
// that verdict for the rest of the session — there is no re-detection (user
// ruling 10): a user who installs or logs into gh restarts gg. The first call
// makes a network round trip; frontends call it off their UI thread.
func (s *Service) ForgeStatus(ctx context.Context) ForgeStatus {
	s.forgeMu.Lock()
	defer s.forgeMu.Unlock()
	if !s.forgeProbed {
		ps := s.forgeProviders
		if ps == nil {
			ps = forge.Default(s.workdir, nil)
		}
		s.forgeErr = ErrForgeUnavailable
		for _, p := range ps {
			err := p.Detect(ctx)
			if err == nil {
				s.forgeActive, s.forgeErr = p, nil
				break
			}
			s.forgeErr = errors.Join(ErrForgeUnavailable, err)
		}
		s.forgeProbed = true
		s.invalidatePreflight() // the forge verdict just changed
	}
	if s.forgeActive == nil {
		return ForgeStatus{Err: s.forgeErr}
	}
	return ForgeStatus{Provider: s.forgeActive.Name()}
}

// forgeProbe snapshots detection for the preflight resolver WITHOUT probing.
func (s *Service) forgeProbe() *preflight.ForgeProbe {
	s.forgeMu.Lock()
	defer s.forgeMu.Unlock()
	if !s.forgeProbed {
		return nil
	}
	if s.forgeActive == nil {
		return &preflight.ForgeProbe{Err: s.forgeErr.Error()}
	}
	return &preflight.ForgeProbe{Provider: s.forgeActive.Name()}
}

// provider returns the active provider, probing if nobody has yet.
func (s *Service) provider(ctx context.Context) (forge.Provider, error) {
	if st := s.ForgeStatus(ctx); !st.Available() {
		return nil, st.Err
	}
	s.forgeMu.Lock()
	defer s.forgeMu.Unlock()
	return s.forgeActive, nil
}
```
(import `preflight`.) In `preflight.go`:
- add `func (s *Service) invalidatePreflight() { s.preflightMu.Lock(); s.preflightDone, s.preflightOut, s.preflightMarks = false, nil, nil; s.preflightMu.Unlock() }`.
  **Lock order:** `ForgeStatus` holds `forgeMu` then takes `preflightMu`;
  `Preflight` holds `preflightMu` then calls `forgeProbe` (takes `forgeMu`) —
  that is an inversion. Avoid it: in `ForgeStatus`, record
  `justProbed := true`, release `forgeMu` (drop the `defer`, unlock
  explicitly before the return paths) and call `s.invalidatePreflight()`
  AFTER unlocking. Write it that way; the snippet above shows intent only.
- in both places that build `preflight.Probes{…}` (`probesFrom` return at
  ~l.70 and any sibling), add `Forge: s.forgeProbe(),`.
- if `s.workdir` is empty for services built by the test helper, use the
  repo's dir accessor the helper exposes; check `openWith` in `service.go:172-234`
  for where the ring (`observ.Recorder`) is kept and pass it instead of `nil`
  to `forge.Default` when there is a field for it.

- [ ] **Step 4:** `rtk go test ./internal/domain/ -run 'Forge|Preflight'` → PASS, and `rtk go test -race ./internal/domain/ -run Forge` → PASS (lock order).
- [ ] **Step 5: commit** — `feat(domain): detect the forge once per session; forge feature verdict`.

---

### Task 8: domain — PR queries, known-PR set, op builders

**Files:**
- Modify: `internal/domain/forge.go`
- Test: `internal/domain/forge_test.go` (append)

**Interfaces — Produces:**
```go
func (s *Service) PullRequests(ctx context.Context) ([]model.PullRequest, error) // open first, then known non-open; each group newest-updated first
func (s *Service) PullRequest(ctx context.Context, n int) (model.PullRequest, error)
type PRComments struct {
	Inline    []model.ForgeComment // inline + file-level WITH a current position, thread order
	Hub       []model.ForgeComment // general + review, chronological
	Outdated  []model.ForgeComment // inline whose thread has no current position
	Truncated bool
}
func (s *Service) PRComments(ctx context.Context, n int) (PRComments, error)
func (s *Service) PRFetchOp(ctx context.Context, n int) (engine.FetchPRHead, error)
func (s *Service) PRForgetOp(n int) engine.ForgetPR   // also drops n from the session's known set
type PRPair struct{ Base, Head string }               // revs for the diff: Base...Head
func (s *Service) PRPair(ctx context.Context, p model.PullRequest) PRPair
```
Plan 3 converts `PRComments.Inline` to note boxes; this plan stops at the
provider-neutral shape.

- [ ] **Step 1: failing tests** (append)

```go
func pr(n int, state string, upd int) model.PullRequest {
	return model.PullRequest{Number: n, State: state, Title: "t", HeadSHA: "h", BaseSHA: "b",
		Target: "main", Updated: time.Unix(int64(upd), 0)}
}

func TestPullRequestsKeepsKnownClosedOnes(t *testing.T) {
	t.Parallel()
	ff := &fakeForge{open: []model.PullRequest{pr(1, "open", 10), pr(2, "open", 20)},
		byNum: map[int]model.PullRequest{1: pr(1, "merged", 30)}}
	svc := newForgeSvc(t, ff)
	ctx := context.Background()
	got, err := svc.PullRequests(ctx)
	if err != nil || len(got) != 2 || got[0].Number != 2 {
		t.Fatalf("first = %+v, %v (want #2 then #1, newest first)", got, err)
	}
	ff.mu.Lock()
	ff.open = []model.PullRequest{pr(2, "open", 20)} // #1 got merged
	ff.mu.Unlock()
	for range 2 {
		got, err = svc.PullRequests(ctx)
		if err != nil || len(got) != 2 || got[1].Number != 1 || got[1].State != model.PRStateMerged {
			t.Fatalf("after merge = %+v, %v", got, err)
		}
	}
	if ff.prCalls[1] != 1 {
		t.Errorf("PR(1) read %d times, want 1 (terminal state is cached)", ff.prCalls[1])
	}
}

func TestPullRequestsListsFetchedRefsAcrossSessions(t *testing.T) {
	t.Parallel()
	ff := &fakeForge{byNum: map[int]model.PullRequest{5: pr(5, "closed", 5)}}
	svc := newForgeSvc(t, ff)
	ctx := context.Background()
	head, err := svc.repo.ResolveCommit(ctx, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{5, 6} { // 6 is unknown to the forge
		if err := svc.repo.UpdateRef(ctx, git.PRRef(n), head); err != nil {
			t.Fatal(err)
		}
	}
	got, err := svc.PullRequests(ctx)
	if err != nil || len(got) != 2 {
		t.Fatalf("got %+v, %v", got, err)
	}
	states := map[int]string{got[0].Number: got[0].State, got[1].Number: got[1].State}
	if states[5] != model.PRStateClosed || states[6] != model.PRStateUnavailable {
		t.Errorf("states = %v", states)
	}
}

func TestForgetDropsSessionKnownPR(t *testing.T) {
	t.Parallel()
	ff := &fakeForge{open: []model.PullRequest{pr(1, "open", 1)}, byNum: map[int]model.PullRequest{1: pr(1, "closed", 2)}}
	svc := newForgeSvc(t, ff)
	ctx := context.Background()
	svc.PullRequests(ctx)
	ff.mu.Lock()
	ff.open = nil
	ff.mu.Unlock()
	if got, _ := svc.PullRequests(ctx); len(got) != 1 {
		t.Fatalf("closed PR vanished: %+v", got)
	}
	if _, err := svc.Execute(ctx, svc.PRForgetOp(1), nil, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.PullRequests(ctx); len(got) != 0 {
		t.Errorf("after forget = %+v", got)
	}
}

func TestPRCommentsBuckets(t *testing.T) {
	t.Parallel()
	at := func(s int) time.Time { return time.Unix(int64(s), 0) }
	ff := &fakeForge{truncated: true, comments: []model.ForgeComment{
		{ID: "R", Kind: model.ForgeCommentReview, Created: at(30)},
		{ID: "I", Kind: model.ForgeCommentInline, Path: "a.go", Line: 3},
		{ID: "O", Kind: model.ForgeCommentInline, Path: "a.go", Outdated: true},
		{ID: "Z", Kind: model.ForgeCommentInline, Path: "a.go"}, // no line, not flagged: still has no position
		{ID: "F", Kind: model.ForgeCommentFile, Path: "b.go"},
		{ID: "G", Kind: model.ForgeCommentGeneral, Created: at(10)},
	}}
	c, err := newForgeSvc(t, ff).PRComments(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	ids := func(cs []model.ForgeComment) (s string) {
		for _, c := range cs {
			s += c.ID
		}
		return
	}
	if ids(c.Inline) != "IF" || ids(c.Outdated) != "OZ" || ids(c.Hub) != "GR" || !c.Truncated {
		t.Errorf("inline=%s outdated=%s hub=%s truncated=%v", ids(c.Inline), ids(c.Outdated), ids(c.Hub), c.Truncated)
	}
}

func TestPRFetchOpPrefersAMatchingRemote(t *testing.T) {
	t.Parallel()
	ff := &fakeForge{slug: "github.com/homeend/gigagit", url: "git@github.com:homeend/gigagit.git",
		byNum: map[int]model.PullRequest{7: pr(7, "open", 1)}}
	svc := newForgeSvc(t, ff)
	ctx := context.Background()
	op, err := svc.PRFetchOp(ctx, 7)
	if err != nil || op.Remote != ff.url || op.Refspec != "pull/x/head" || op.HeadSHA != "h" || op.Number != 7 {
		t.Fatalf("no remotes: op = %+v, %v (want the fallback URL)", op, err)
	}
	// add a remote spelled differently (https) that names the same repo
	if _, err := svc.repo.Runner.Run(ctx, "git remote add", []string{"remote", "add", "upstream", "https://github.com/HomeEnd/gigagit.git"}); err != nil {
		t.Fatal(err)
	}
	op, err = svc.PRFetchOp(ctx, 7)
	if err != nil || op.Remote != "upstream" {
		t.Fatalf("op = %+v, %v (want remote name upstream)", op, err)
	}
}

func TestPRPair(t *testing.T) {
	t.Parallel()
	svc := newForgeSvc(t, &fakeForge{})
	ctx := context.Background()
	if p := svc.PRPair(ctx, model.PullRequest{Number: 7, State: "open", Target: "main", BaseSHA: "bbb"}); p.Base != "main" || p.Head != "refs/gg/pr/7" {
		t.Errorf("open = %+v", p)
	}
	if p := svc.PRPair(ctx, model.PullRequest{Number: 7, State: "merged", Target: "main", BaseSHA: "bbb"}); p.Base != "bbb" {
		t.Errorf("merged = %+v (want the recorded base sha)", p)
	}
	if p := svc.PRPair(ctx, model.PullRequest{Number: 7, State: "open", Target: "no-such-branch", BaseSHA: "bbb"}); p.Base != "bbb" {
		t.Errorf("missing target = %+v (want the base sha fallback)", p)
	}
}
```
(imports: `time`, `internal/git`. Check how the runner wants argv for the
`remote add` call — `gitcmd.New("remote").Arg("add", …).ToArgv()` if `Run`
expects builder output; mirror any existing test that adds a remote.)

- [ ] **Step 2:** `rtk go test ./internal/domain/ -run 'PullRequests|Forget|PRComments|PRFetchOp|PRPair'` → FAIL.

- [ ] **Step 3: implement** (append to `internal/domain/forge.go`)

```go
// PullRequests lists the open pull requests plus every KNOWN one that is no
// longer open (user ruling 11): a PR never disappears from gg because it was
// closed or merged. "Known" = listed open earlier this session, or has a
// fetched refs/gg/pr/<n> (the user opened it, in any session — the ref is
// the durable record; there is no state file). Open ones come first; each
// group is newest-updated first.
func (s *Service) PullRequests(ctx context.Context) ([]model.PullRequest, error) {
	p, err := s.provider(ctx)
	if err != nil {
		return nil, err
	}
	v, err := s.flight.Do("forge-prs", func() (any, error) { return s.pullRequests(ctx, p) })
	if err != nil {
		return nil, err
	}
	return slices.Clone(v.([]model.PullRequest)), nil
}
```
(`flightGroup.Do(key string, fn func() (any, error)) (any, error)` — verified, `flight.go:26`.)

```go
func (s *Service) pullRequests(ctx context.Context, p forge.Provider) ([]model.PullRequest, error) {
	open, err := p.ListOpen(ctx)
	if err != nil {
		return nil, err
	}
	isOpen := make(map[int]bool, len(open))
	known := map[int]bool{}
	s.forgeMu.Lock()
	if s.forgeSeen == nil {
		s.forgeSeen = map[int]bool{}
	}
	for _, pr := range open {
		isOpen[pr.Number] = true
		s.forgeSeen[pr.Number] = true
		delete(s.forgeTerminal, pr.Number) // reopened
	}
	for n := range s.forgeSeen {
		known[n] = true
	}
	s.forgeMu.Unlock()
	if refs, err := s.repo.ForEachRef(ctx, git.PRRefPrefix); err == nil { // fail open
		for _, r := range refs {
			if n, ok := git.ParsePRRef(r.Ref); ok {
				known[n] = true
			}
		}
	}
	var rest []model.PullRequest
	for n := range known {
		if isOpen[n] {
			continue
		}
		rest = append(rest, s.terminalPR(ctx, p, n))
	}
	byUpdated := func(a, b model.PullRequest) int {
		if c := b.Updated.Compare(a.Updated); c != 0 {
			return c
		}
		return b.Number - a.Number
	}
	slices.SortFunc(open, byUpdated)
	slices.SortFunc(rest, byUpdated)
	return append(open, rest...), nil
}

// terminalPR reads a no-longer-open PR once and caches the answer: a
// closed/merged state is re-read only after Forget or a reopen.
func (s *Service) terminalPR(ctx context.Context, p forge.Provider, n int) model.PullRequest {
	s.forgeMu.Lock()
	cached, ok := s.forgeTerminal[n]
	s.forgeMu.Unlock()
	if ok {
		return cached
	}
	pr, err := p.PR(ctx, n)
	switch {
	case errors.Is(err, forge.ErrNotFound):
		pr = model.PullRequest{Number: n, State: model.PRStateUnavailable}
	case err != nil:
		// transient: show it as unavailable now, do NOT cache, retry next read
		return model.PullRequest{Number: n, State: model.PRStateUnavailable}
	case pr.IsOpen():
		return pr // raced with a reopen; the next list carries it
	}
	s.forgeMu.Lock()
	if s.forgeTerminal == nil {
		s.forgeTerminal = map[int]model.PullRequest{}
	}
	s.forgeTerminal[n] = pr
	s.forgeMu.Unlock()
	return pr
}

// PullRequest reads one PR with its body.
func (s *Service) PullRequest(ctx context.Context, n int) (model.PullRequest, error) {
	p, err := s.provider(ctx)
	if err != nil {
		return model.PullRequest{}, err
	}
	return p.PR(ctx, n)
}

// PRComments is one PR's comments, bucketed by where a frontend shows them.
type PRComments struct {
	Inline    []model.ForgeComment
	Hub       []model.ForgeComment
	Outdated  []model.ForgeComment
	Truncated bool
}

func (s *Service) PRComments(ctx context.Context, n int) (PRComments, error) {
	p, err := s.provider(ctx)
	if err != nil {
		return PRComments{}, err
	}
	cs, truncated, err := p.Comments(ctx, n)
	if err != nil {
		return PRComments{}, err
	}
	out := PRComments{Truncated: truncated}
	for _, c := range cs {
		switch {
		case c.Kind == model.ForgeCommentGeneral || c.Kind == model.ForgeCommentReview:
			out.Hub = append(out.Hub, c)
		case c.Kind == model.ForgeCommentFile && !c.Outdated:
			out.Inline = append(out.Inline, c)
		case c.Outdated || c.Line <= 0:
			out.Outdated = append(out.Outdated, c)
		default:
			out.Inline = append(out.Inline, c)
		}
	}
	slices.SortStableFunc(out.Hub, func(a, b model.ForgeComment) int { return a.Created.Compare(b.Created) })
	return out, nil
}

// PRFetchOp builds the op that brings PR n's head into refs/gg/pr/<n>. It
// fetches through the configured remote naming the base repository — the
// user's own transport and credentials — and only falls back to the
// provider's URL when no remote matches.
func (s *Service) PRFetchOp(ctx context.Context, n int) (engine.FetchPRHead, error) {
	p, err := s.provider(ctx)
	if err != nil {
		return engine.FetchPRHead{}, err
	}
	pr, err := p.PR(ctx, n)
	if err != nil {
		return engine.FetchPRHead{}, err
	}
	slug, fallback, err := p.BaseRepo(ctx)
	if err != nil {
		return engine.FetchPRHead{}, err
	}
	remote := fallback
	if names, err := s.repo.RemoteNames(ctx); err == nil {
		for _, name := range names {
			if u, err := s.repo.RemoteURL(ctx, name); err == nil && slug != "" && forge.RepoSlug(u) == slug {
				remote = name
				break
			}
		}
	}
	if remote == "" {
		return engine.FetchPRHead{}, fmt.Errorf("pull request #%d: no remote or URL for the base repository", n)
	}
	return engine.FetchPRHead{Remote: remote, Refspec: p.HeadRefspec(n), Number: n, HeadSHA: pr.HeadSHA}, nil
}

// PRForgetOp builds the op that drops PR n's ref, and forgets n for this
// session so a closed row leaves the list.
func (s *Service) PRForgetOp(n int) engine.ForgetPR {
	s.forgeMu.Lock()
	delete(s.forgeSeen, n)
	delete(s.forgeTerminal, n)
	s.forgeMu.Unlock()
	return engine.ForgetPR{Number: n}
}

// PRPair is the rev pair a PR's diff opens on: Base...Head.
type PRPair struct{ Base, Head string }

// PRPair: an open PR diffs against its moving target branch (what merging
// would do today); a closed/merged one — or one whose target branch is not
// here — against the base commit the forge recorded, since after a merge the
// target tip already contains the head and the three-dot diff would be empty.
func (s *Service) PRPair(ctx context.Context, p model.PullRequest) PRPair {
	pair := PRPair{Base: p.BaseSHA, Head: git.PRRef(p.Number)}
	if p.IsOpen() && p.Target != "" {
		if _, err := s.repo.ResolveCommit(ctx, p.Target); err == nil {
			pair.Base = p.Target
		}
	}
	return pair
}
```
(`model.RefInfo.Ref` is the full ref name — verified.) `svc.repo.UpdateRef` exists on `*git.Repo`
(it is on `GitOps`).

Caveat to verify while implementing `PRPair`: for an open PR whose target only
exists as `origin/main` (no local `main`), `ResolveCommit("main")` fails and
the pair falls back to `BaseSHA` — acceptable (the recorded base is the PR's
true base), and `BaseSHA` is reachable only if fetched; `gg pr fetch` fetches
the head, whose history contains the merge base but not necessarily
`baseRefOid`. So in `PRPair`, after choosing `BaseSHA`, verify it resolves
(`ResolveCommit(p.BaseSHA)`); if not, fall back to `origin/<Target>`-style:
iterate `RemoteNames` and take the first `<remote>/<Target>` that resolves.
Add a fourth test case for that: target branch absent locally, a
`refs/remotes/origin/main` present, `BaseSHA` = 40 zeros → `Base == "origin/main"`.

- [ ] **Step 4:** run the Step 2 command → PASS; `rtk go test -race ./internal/domain/ -run 'Forge|PullRequests|PR'` → PASS.
- [ ] **Step 5: commit** — `feat(domain): pull-request queries, known-PR set, fetch/forget op builders`.

---

### Task 9: CLI — `gg pr`

**Files:**
- Create: `internal/cli/pr.go`
- Modify: `internal/cli/cli.go` (dispatch `case "pr":` beside `"preview"` ~l.158; add `"pr": true` to the known-verbs map ~l.194-197; add a usage line wherever `versions`' one lives — `grep -n 'versionsUsage\|gg versions' internal/cli/*.go`)
- Test: `internal/cli/pr_test.go`

**Interfaces — Consumes:** Task 7/8 domain API, `runOperation` (`core.go:80`).

Output contract:
```
gg pr list        → "#7  open    alice  feat/forge → main  [changes_requested]  Add forge tab"
                    draft adds "[draft]"; fork heads print "bob:fix/typo"; none → "(no pull requests)"
gg pr view 7      → header line as above, URL line, blank, body, then "── conversation ──" and
                    "── outdated ──" sections (each only when non-empty)
gg pr comments 7  → per inline/file comment: "a.go:10-12 (new) carol: rename this"
                    replies indented two spaces; a resolved thread prints "carol [resolved]: …";
                    continuation lines of a body hang-indent 4; outdated rows keep their ORIGINAL line;
                    file-level prints "b.go (file)"
gg pr fetch 7     → prints "refs/gg/pr/7"
gg pr forget 7    → prints the op summary
--json on list/view/comments → the model structs; view = {"pr":…,"comments":PRComments}
no provider       → stderr "gg pr: <status.Err>", exit 1.   bad usage → exit 2.
```

- [ ] **Step 1: failing tests** — `internal/cli/pr_test.go`

```go
package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/forge/forgetest"
)
```
⚠ archtest (Task 3 Step 9) may forbid `internal/cli` importing
`internal/forge/...` even from tests. If `rtk go test ./internal/archtest/`
fails on this import, move `forgetest` to `internal/forgetest` (it imports
nothing from `forge`; only its `runtime.Caller` path to `testdata/fakegh`
changes to `../forge/testdata/fakegh`) and update the three importers.

```go
// prRepo builds a repo whose .git/fakegh holds the forge package's fixtures.
// Serial tests: they set process env for the fake gh.
func prRepo(t *testing.T, fixtures ...string) string {
	t.Helper()
	dir := newRepoDir(t) // core_test.go:20
	t.Setenv(forgetest.EnvBin, forgetest.BuildFakeGH(t))
	files := map[string]string{}
	for _, name := range fixtures {
		b, err := os.ReadFile(filepath.Join("..", "forge", "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		files[name] = string(b)
	}
	forgetest.Seed(t, filepath.Join(dir, ".git", "fakegh"), files)
	return dir
}

func runPR(t *testing.T, dir string, args ...string) (string, string, int) {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(dir, append([]string{"pr"}, args...), strings.NewReader(""), &out, &errb, "")
	return out.String(), errb.String(), code
}

func TestPRList(t *testing.T) {
	dir := prRepo(t, "pr-list.json")
	out, errs, code := runPR(t, dir, "list")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	for _, want := range []string{"#7", "alice", "feat/forge → main", "[changes_requested]", "Add forge tab",
		"#9", "[draft]", "bob:fix/typo → main"} {
		if !strings.Contains(out, want) {
			t.Errorf("list output lacks %q:\n%s", want, out)
		}
	}
	out, _, _ = runPR(t, dir, "list", "--json")
	var prs []map[string]any
	if err := json.Unmarshal([]byte(out), &prs); err != nil || len(prs) != 2 || prs[0]["number"] == nil || prs[0]["head_sha"] == nil {
		t.Errorf("json = %s (%v)", out, err)
	}
}

func TestPRViewAndComments(t *testing.T) {
	dir := prRepo(t, "pr-list.json", "pr-view-7.json", "threads-7.json")
	out, errs, code := runPR(t, dir, "view", "7")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	for _, want := range []string{"#7", "merged", "https://github.com/homeend/gigagit/pull/7", "Second paragraph.",
		"── conversation ──", "bob: nice work", "carol [changes_requested]: needs the rename", "dave [approved]",
		"── outdated ──", "a.go (old) ghost: old remark", "-old"} {
		if !strings.Contains(out, want) {
			t.Errorf("view lacks %q:\n%s", want, out)
		}
	}
	out, _, _ = runPR(t, dir, "comments", "7")
	for _, want := range []string{"a.go:10-12 (new) carol: rename this", "  alice: done", "b.go (file) carol: split this file [resolved]"} {
		if !strings.Contains(out, want) {
			t.Errorf("comments lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "old remark") {
		t.Error("comments must not print outdated threads inline")
	}
}

func TestPRWithoutAForgeFailsLoudly(t *testing.T) {
	dir := prRepo(t) // fake gh, NO fixtures → Detect fails
	_, errs, code := runPR(t, dir, "list")
	if code != 1 || !strings.Contains(errs, "gg pr:") || !strings.Contains(errs, "no forge CLI") {
		t.Errorf("exit %d, stderr %q", code, errs)
	}
}

func TestPRUsage(t *testing.T) {
	dir := prRepo(t, "pr-list.json")
	for _, args := range [][]string{{}, {"view"}, {"view", "x"}, {"bogus"}, {"fetch", "0"}} {
		if _, _, code := runPR(t, dir, args...); code != 2 {
			t.Errorf("gg pr %v → exit %d, want 2", args, code)
		}
	}
}
```
`gg pr fetch` end-to-end is covered by Task 6 (engine) + Task 8 (`PRFetchOp`);
the fake `gh`'s repo-view fixture names github.com, which a temp repo cannot
fetch from, so no CLI-level fetch test.

- [ ] **Step 2:** `rtk go test ./internal/cli/ -run TestPR` → FAIL.

- [ ] **Step 3: implement** — `internal/cli/pr.go`

```go
package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

const prUsage = `usage: gg pr list [--json]
       gg pr view <number> [--json]
       gg pr comments <number> [--json]
       gg pr fetch <number>
       gg pr forget <number>`

// cmdPR is the read-only pull-request surface. Unlike the TUI — which hides
// the feature silently — the CLI was ASKED, so a missing forge is an error.
func cmdPR(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, prUsage)
		return 2
	}
	verb := args[0]
	fs := flag.NewFlagSet("pr "+verb, flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "machine-readable output")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	n := 0
	switch verb {
	case "list":
		if fs.NArg() != 0 {
			fmt.Fprintln(stderr, prUsage)
			return 2
		}
	case "view", "comments", "fetch", "forget":
		var err error
		if fs.NArg() != 1 {
			fmt.Fprintln(stderr, prUsage)
			return 2
		}
		if n, err = strconv.Atoi(strings.TrimPrefix(fs.Arg(0), "#")); err != nil || n <= 0 {
			fmt.Fprintf(stderr, "error: %q is not a pull request number\n%s\n", fs.Arg(0), prUsage)
			return 2
		}
	default:
		fmt.Fprintln(stderr, prUsage)
		return 2
	}
	ctx := context.Background()
	if st := svc.ForgeStatus(ctx); !st.Available() {
		fmt.Fprintf(stderr, "gg pr: %v\n", st.Err)
		return 1
	}
	fail := func(err error) int { fmt.Fprintln(stderr, "error:", err); return 1 }
	emit := func(v any) int {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(v); err != nil {
			return fail(err)
		}
		return 0
	}
	switch verb {
	case "list":
		prs, err := svc.PullRequests(ctx)
		if err != nil {
			return fail(err)
		}
		if *asJSON {
			if prs == nil {
				prs = []model.PullRequest{}
			}
			return emit(prs)
		}
		if len(prs) == 0 {
			fmt.Fprintln(stdout, "(no pull requests)")
		}
		for _, p := range prs {
			fmt.Fprintln(stdout, prLine(p))
		}
	case "view":
		p, err := svc.PullRequest(ctx, n)
		if err != nil {
			return fail(err)
		}
		cs, err := svc.PRComments(ctx, n)
		if err != nil {
			return fail(err)
		}
		if *asJSON {
			return emit(struct {
				PR       model.PullRequest `json:"pr"`
				Comments domain.PRComments `json:"comments"`
			}{p, cs})
		}
		printPRView(stdout, p, cs)
	case "comments":
		cs, err := svc.PRComments(ctx, n)
		if err != nil {
			return fail(err)
		}
		if *asJSON {
			return emit(cs)
		}
		printPRComments(stdout, cs)
	case "fetch":
		op, err := svc.PRFetchOp(ctx, n)
		if err != nil {
			return fail(err)
		}
		if _, err := runOperation(ctx, svc, op, nil, stderr); err != nil {
			return fail(err)
		}
		fmt.Fprintln(stdout, "refs/gg/pr/"+strconv.Itoa(n))
	case "forget":
		res, err := runOperation(ctx, svc, svc.PRForgetOp(n), nil, stderr)
		if err != nil {
			return fail(err)
		}
		fmt.Fprintln(stdout, res.Summary)
	}
	return 0
}

func prLine(p model.PullRequest) string {
	src := p.Source
	if p.SourceRepo != "" {
		src = strings.SplitN(p.SourceRepo, "/", 2)[0] + ":" + src
	}
	var tags string
	if p.Draft {
		tags += " [draft]"
	}
	if p.ReviewState != "" {
		tags += " [" + p.ReviewState + "]"
	}
	return fmt.Sprintf("#%-4d %-11s %s  %s → %s%s  %s", p.Number, p.State, p.Author, src, p.Target, tags, p.Title)
}

func printPRView(w io.Writer, p model.PullRequest, cs domain.PRComments) {
	fmt.Fprintln(w, prLine(p))
	fmt.Fprintln(w, p.URL)
	if p.Body != "" {
		fmt.Fprintf(w, "\n%s\n", p.Body)
	}
	if len(cs.Hub) > 0 {
		fmt.Fprintln(w, "\n── conversation ──")
		for _, c := range cs.Hub {
			who := c.Author
			if c.Verdict != "" {
				who += " [" + c.Verdict + "]"
			}
			fmt.Fprintf(w, "%s: %s\n", who, c.Body)
		}
	}
	if len(cs.Outdated) > 0 {
		fmt.Fprintln(w, "\n── outdated ──")
		for _, c := range cs.Outdated {
			fmt.Fprintf(w, "%s (%s) %s: %s\n", c.Path, c.Side, c.Author, c.Body)
			if c.ParentID == "" && c.Hunk != "" {
				for _, l := range lastLines(c.Hunk, 6) {
					fmt.Fprintln(w, "    "+l)
				}
			}
		}
	}
	if cs.Truncated {
		fmt.Fprintln(w, "\n(comment list truncated — open the pull request on the forge for the rest)")
	}
}

func printPRComments(w io.Writer, cs domain.PRComments) {
	if len(cs.Inline) == 0 {
		fmt.Fprintln(w, "(no inline comments)")
	}
	for _, c := range cs.Inline {
		if c.ParentID != "" {
			fmt.Fprintf(w, "  %s: %s\n", c.Author, c.Body)
			continue
		}
		where := c.Path + " (file)"
		if c.Kind == model.ForgeCommentInline {
			span := strconv.Itoa(c.Line)
			if c.StartLine > 0 && c.StartLine != c.Line {
				span = strconv.Itoa(c.StartLine) + "-" + span
			}
			where = fmt.Sprintf("%s:%s (%s)", c.Path, span, c.Side)
		}
		tail := ""
		if c.Resolved {
			tail = " [resolved]"
		}
		fmt.Fprintf(w, "%s %s: %s%s\n", where, c.Author, c.Body, tail)
	}
	if cs.Truncated {
		fmt.Fprintln(w, "(comment list truncated)")
	}
}

func lastLines(s string, n int) []string {
	ls := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(ls) > n {
		ls = ls[len(ls)-n:]
	}
	return ls
}
```
Verified: `NoteSide` prints `old`/`new`; `engine.Result.Summary` is the
English string (`operation.go:9`). Do not pass a nil Decider — replace both
`nil` decider arguments above with `cliDecider{out: stderr}` (the shape
`versions.go:115` builds; these ops raise no decisions).

Wire in `cli.go`: `case "pr": return cmdPR(svc, rest, stdout, stderr)` and
`"pr": true` in the known-verbs map; add `pr` to the top-level help text next
to `preview`.

- [ ] **Step 4:** `rtk go test ./internal/cli/ -run TestPR && rtk go test ./internal/archtest/` → PASS.
- [ ] **Step 5: commit** — `feat(cli): read-only gg pr list/view/comments/fetch/forget`.

---

### Task 10: e2e scenario

**Files:**
- Create: `e2e/scenarios/pr_readonly.toml`
- Modify: `e2e/env_test.go` (TestMain: point `GG_GH_BIN` at the fake)

Use the `writing-e2e-scenarios` skill for the schema. In `TestMain`, before
`m.Run()`:
```go
	// A fake gh for every scenario: it answers only from <repo>/.git/fakegh, so
	// scenarios that seed no fixtures see "no usable forge" — and the real gh on
	// a developer's box can never leak network calls into the suite.
	os.Setenv("GG_GH_BIN", forgetest.BuildFakeGH(testingTB{}))
```
`BuildFakeGH` takes a `testing.TB`; TestMain has none. Add to `forgetest`:
```go
// BuildFakeGHMain is BuildFakeGH for a TestMain, which has no testing.TB.
func BuildFakeGHMain() (string, error) { once.Do(build); return binPath, binErr }
```
by extracting the `once.Do` body into `func build()`, and call that here
(`panic` on error, like the surrounding setup).

Scenario (adapt key names to the schema the skill documents; the `write`
step with a `.git/…` path is the fixture transport):
```toml
name = "pr: read-only list, view and comments from a forge CLI"

[input]
steps = [
  { write = "a.go", content = "package a\n" },
  { commit = "init" },
  { write = ".git/fakegh/pr-list.json", content = '[{"number":7,"title":"Add forge tab","author":{"login":"alice"},"state":"OPEN","isDraft":false,"reviewDecision":"","headRefName":"feat/forge","isCrossRepository":false,"baseRefName":"main","baseRefOid":"1111111111111111111111111111111111111111","headRefOid":"2222222222222222222222222222222222222222","url":"https://example.test/pull/7","createdAt":"2026-09-01T10:00:00Z","updatedAt":"2026-09-02T11:30:00Z"}]' },
]

[[run]]
argv = ["pr", "list"]
stdout_contains = ["#7", "alice", "feat/forge → main", "Add forge tab"]

[[run]]
argv = ["pr", "view", "8"]
exit = 1
```
If the builder's `write` step refuses paths under `.git` (check
`e2e/builder.go` for a guard), add a dedicated `forge_fixture = { name, content }`
step to `Step` + the builder that writes `<repo>/.git/fakegh/<name>`, with a
builder test beside the existing ones.

- [ ] Run: `./test.sh e2e` → PASS (all scenarios — the fake gh must not disturb any other).
- [ ] Commit — `test(e2e): read-only gg pr scenario over a fake gh`.

---

### Task 11: real-gh smoke check (manual, this machine has gh logged in)

- [ ] `cd /mnt/t/others/gigagit/.claude/worktrees/forge-prs && go build -o /tmp/claude-1000/-mnt-t-others-gigagit/1d5f6169-b84b-4df5-9a29-6b7e085e6628/scratchpad/gg-forge ./cmd/gg`
- [ ] In a clone of a public GitHub repo with open, reviewed PRs (e.g.
  `/mnt/t/others/…/lazygit` from the test-repos memory; confirm it has a
  github remote): run `gg-forge pr list`, `pr view <n>`, `pr comments <n>`,
  `pr fetch <n>` then `gg-forge diff <target>...refs/gg/pr/<n> --stat`-style
  read, and `pr forget <n>`.
- [ ] Verify against the PR's web page: thread count, a resolved thread is
  tagged, an outdated thread lands under "outdated", a fork PR fetches.
- [ ] Verify `git log --oneline --decorate -3` shows no `refs/gg/pr` decoration in gg's graph (`gg-forge log -3`).
- [ ] Any mismatch between real `gh` JSON and the fixtures → fix the parser
  AND update the fixture to the real shape, re-run Task 3 tests.

---

### Task 12: docs + gate

**Files:** `docs/superpowers/specs/2026-09-19-forge-prs-design.md`,
`CHANGELOG.md`, `README.md`, `internal/agentskill/using-gg.md`,
`internal/agentskill/agentskill.go` (`Version` 81 → 82), `CLAUDE.md`,
`docs/CLAUDE-details.md`

- [ ] Spec: apply the four deviations (types → `model`; drop `Comments`
  count and the `💬` badge; single-page caps; fixture transport) so spec and
  code agree.
- [ ] CHANGELOG: an entry under the unreleased heading — read-only `gg pr`,
  `refs/gg/pr/<n>`, detection once per session, closed/merged PRs stay listed.
- [ ] README: a short "Pull requests (read-only)" section with the five verbs.
- [ ] using-gg.md: the `gg pr` verbs + `--json` shapes + "fetch then
  `gg diff <target>...refs/gg/pr/<n>`"; bump `agentskill.Version` to 82; run
  the agentskill tests (a version-marker test exists).
- [ ] CLAUDE.md package map: one new row —
  `| forge | Read-only forge (pull-request) seam: Provider interface + the gh CLI implementation (JSON/GraphQL parsers, RepoSlug); types live in model. Owned by domain; frontends never import it. |`
  — and extend the `preflight`, `domain`, `cli` rows by a clause each. Detail
  (known-PR rule, PRPair base choice, fake gh transport) goes in
  `docs/CLAUDE-details.md`.
- [ ] `./test.sh` → PASS, then `./test.sh race` → PASS. Paste the tails into the session.
- [ ] Commit — `docs: forge PR core — changelog, readme, agent skill v82, package map`.
- [ ] STOP. Report to the user; do not merge (the human merges). Then write
  plan 2 (TUI tab + hub popup + `[refresh] prs_seconds`).

---

## Self-review (done while writing)

- **Spec coverage in this plan:** §1 provider ✔ (T2-3), §2 detection ✔ (T4, T7),
  §3 fetch op ✔ (T5-6), §4/4a queries + known PRs ✔ (T8), §6 CLI ✔ (T9),
  §8 error rows: CLI-visible ones ✔, §9 tests ✔. **Deferred to plan 2:** §5 PRs
  tab, hub popup, §7 `prs_seconds`, manual refresh, help/footer. **Deferred to
  plan 3:** inline comments → note boxes (`NoteSourceForge`, `ErrReadOnlyNote`,
  `RemoveAllNotes` skip), file-level box placement, generic collapse, comment
  re-poll.
- **Types:** `PRComments`, `PRPair`, `ForgeStatus`, `FetchPRHead`, `ForgetPR`
  names are identical across Tasks 6-9.
- **Names verified against the code:** `NoteSide` consts, `Fit` consts,
  `flightGroup.Do`, `RefInfo.Ref`, `Result.Summary`, `GitOps.RevParse`,
  `newRealRepo`, `newRepoDir`, `runOperation` + `cliDecider`.
  **Still verify-first (marked inline):** whether archtest scans `_test.go`
  imports, the e2e builder's handling of `.git/…` write paths, the
  `gitexec` Result-on-error shape, the ring/recorder field on `Service`.

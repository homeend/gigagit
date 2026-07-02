# Agent-friendly CLI verbs (stage 1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the gg CLI the read/stage verbs agents currently fall back to raw git for: `gg log`, `gg diff`, `gg show`, `gg add`/`gg unstage`, `gg branch current`, `gg branch ls`, `gg worktree prune`, and a commit summary that includes the new sha.

**Architecture:** Each read verb is a thin `internal/git` verb (one git invocation via `gitcmd`) behind a `domain` query (Read reservation, singleflight, `NoteFailure`), rendered terse by a CLI subcommand. Writes reuse/extend engine ops (`Stage`, `Commit`, new `PruneWorktrees`) run via `runOperation`/`domain.Execute`. Spec: `docs/superpowers/specs/2026-07-03-agent-cli-batch-design.md`.

**Tech Stack:** Go 1.26, stdlib `flag`, existing `gitcmd`/`gitexec` (FakeRunner) infrastructure. No new dependencies.

## Global Constraints

- **Worktree:** ALL work happens in `/mnt/t/others/gigagit/.claude/worktrees/agent-cli-verbs` on branch `feat/agent-cli-verbs`. First action of every session/subagent: `cd` there and verify with `git branch --show-current` (expect `feat/agent-cli-verbs`). Write/Edit tools: always use absolute paths that start with the worktree path.
- **Layering (archtest-enforced):** `internal/cli` NEVER imports `internal/git` — it reaches git only through `internal/domain` and uses `internal/model` types. `internal/git` verbs are one git invocation each, argv built with `gitcmd.New(...)`.
- **Output formats are the contract** (terse-first, spec §Decisions): tests assert exact bytes.
- **TDD:** write the failing test first, watch it fail, implement, watch it pass, commit. Run `gofmt -l` on touched packages before each commit (must print nothing).
- **Commit messages:** conventional prefix (`feat(cli):`, `feat(engine):`, `test(e2e):`, `docs:`) + the standard Claude trailers your harness instructions require.
- **Test commands** run from the worktree root, e.g. `go test ./internal/git/ -run TestLogLines -v`.

---

### Task 1: git verbs `LogLines` + `CommitLine` (+ `model.LogLine`)

**Files:**
- Modify: `internal/model/model.go` (add `LogLine` near `Commit`)
- Create: `internal/git/log_lines.go`
- Test: `internal/git/log_lines_test.go`

**Interfaces:**
- Consumes: `gitcmd.New`, `gitexec.Result.Stdout` (string), `Repo{Runner}`.
- Produces: `model.LogLine{Hash, Subject string}`; `(r *Repo) LogLines(ctx context.Context, rev string, n int) ([]model.LogLine, error)`; `(r *Repo) CommitLine(ctx context.Context, rev string) (model.LogLine, error)`. Both used by Task 2's domain queries and Task 7's engine change.

- [ ] **Step 1: Write the failing tests**

`internal/git/log_lines_test.go`:

```go
package git

import (
	"context"
	"reflect"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
)

func TestLogLinesArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git log", gitexec.Result{Stdout: "abc1234\x1ffirst subject\ndef5678\x1fsecond\n"})
	r := &Repo{Runner: f}
	got, err := r.LogLines(context.Background(), "main..HEAD", 5)
	if err != nil {
		t.Fatalf("LogLines: %v", err)
	}
	wantArgv := []string{"log", "--format=%h%x1f%s", "-n", "5", "main..HEAD"}
	if !reflect.DeepEqual(f.Calls[0].Argv, wantArgv) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, wantArgv)
	}
	want := []model.LogLine{{Hash: "abc1234", Subject: "first subject"}, {Hash: "def5678", Subject: "second"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("lines = %+v, want %+v", got, want)
	}
}

func TestCommitLineArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git log", gitexec.Result{Stdout: "abc1234\x1fthe subject\n"})
	r := &Repo{Runner: f}
	got, err := r.CommitLine(context.Background(), "HEAD")
	if err != nil {
		t.Fatalf("CommitLine: %v", err)
	}
	wantArgv := []string{"log", "-1", "--format=%h%x1f%s", "HEAD"}
	if !reflect.DeepEqual(f.Calls[0].Argv, wantArgv) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, wantArgv)
	}
	if got.Hash != "abc1234" || got.Subject != "the subject" {
		t.Fatalf("line = %+v", got)
	}
}

func TestCommitLineEmptyOutputErrors(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git log", gitexec.Result{Stdout: ""})
	r := &Repo{Runner: f}
	if _, err := r.CommitLine(context.Background(), "HEAD"); err == nil {
		t.Fatal("want error on empty output")
	}
}

func TestParseLogLinesSkipsMalformed(t *testing.T) {
	got := parseLogLines("abc\x1fok\nnot-a-log-line\n\n")
	if len(got) != 1 || got[0].Hash != "abc" {
		t.Fatalf("got %+v", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/git/ -run 'TestLogLines|TestCommitLine|TestParseLogLines' -v`
Expected: FAIL — `undefined: model.LogLine`, `r.LogLines undefined`.

- [ ] **Step 3: Implement**

Add to `internal/model/model.go` (below the `Commit` type):

```go
// LogLine is one terse history row (short sha + subject) — the gg log /
// gg show header unit.
type LogLine struct {
	Hash    string // short sha (%h)
	Subject string
}
```

Create `internal/git/log_lines.go`:

```go
package git

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
	"github.com/homeend/gigagit/internal/model"
)

// logLineFormat is one commit per line: short sha and subject separated by
// \x1f (unit separator), which cannot appear in either field.
const logLineFormat = "%h%x1f%s"

// LogLines returns up to n history rows reachable from rev, newest first.
// rev may be a branch, sha, or range string (A..B / A...B) — passed to git
// verbatim.
func (r *Repo) LogLines(ctx context.Context, rev string, n int) ([]model.LogLine, error) {
	b := gitcmd.New("log").Arg("--format="+logLineFormat, "-n", strconv.Itoa(n), rev)
	res, err := r.Runner.Run(ctx, "git log", b.ToArgv())
	if err != nil {
		return nil, err
	}
	return parseLogLines(res.Stdout), nil
}

// CommitLine returns rev's short sha and subject.
func (r *Repo) CommitLine(ctx context.Context, rev string) (model.LogLine, error) {
	b := gitcmd.New("log").Arg("-1", "--format="+logLineFormat, rev)
	res, err := r.Runner.Run(ctx, "git log", b.ToArgv())
	if err != nil {
		return model.LogLine{}, err
	}
	lines := parseLogLines(res.Stdout)
	if len(lines) == 0 {
		return model.LogLine{}, fmt.Errorf("no commit at %q", rev)
	}
	return lines[0], nil
}

func parseLogLines(out string) []model.LogLine {
	var lines []model.LogLine
	for _, ln := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		sha, subject, ok := strings.Cut(ln, "\x1f")
		if !ok || sha == "" {
			continue
		}
		lines = append(lines, model.LogLine{Hash: sha, Subject: subject})
	}
	return lines
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/git/ -run 'TestLogLines|TestCommitLine|TestParseLogLines' -v`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/model/model.go internal/git/log_lines.go internal/git/log_lines_test.go
git commit -m "feat(git): LogLines + CommitLine verbs for terse history reads"
```

---

### Task 2: domain queries `Log`/`CommitLine` + CLI `gg log`

**Files:**
- Create: `internal/domain/query_cli.go`
- Create: `internal/cli/log.go`
- Modify: `internal/cli/cli.go` (switch case + `commands` map)
- Test: `internal/cli/log_test.go`

**Interfaces:**
- Consumes: Task 1's `Repo.LogLines`/`Repo.CommitLine`; domain's unexported `query[T]` helper (`internal/domain/query.go:40`); CLI helpers `runCLI(t, dir, args...)` and `newRepoDir(t)` from `internal/cli/core_test.go`.
- Produces: `(s *Service) Log(ctx context.Context, rev string, n int) ([]model.LogLine, error)`; `(s *Service) CommitLine(ctx context.Context, rev string) (model.LogLine, error)` (used again by Tasks 5 and 9); `gg log [-n N] [<rev>]` printing `<hash> <subject>` lines.

- [ ] **Step 1: Write the failing tests**

`internal/cli/log_test.go`:

```go
package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitIn runs git in dir with test identity (mirrors newRepoDir's runner).
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestLogDefault(t *testing.T) {
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x\n"), 0o644)
	gitIn(t, dir, "add", "a.txt")
	gitIn(t, dir, "commit", "-m", "second commit")

	code, out, errb := runCLI(t, dir, "log")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, errb)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d:\n%s", len(lines), out)
	}
	// newest first: "<short-sha> <subject>"
	if !strings.HasSuffix(lines[0], " second commit") {
		t.Fatalf("line 0 = %q", lines[0])
	}
	sha := strings.Fields(lines[0])[0]
	if len(sha) < 7 || len(sha) > 12 {
		t.Fatalf("sha token looks wrong: %q", sha)
	}
}

func TestLogCountFlag(t *testing.T) {
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x\n"), 0o644)
	gitIn(t, dir, "add", "a.txt")
	gitIn(t, dir, "commit", "-m", "second commit")

	code, out, _ := runCLI(t, dir, "log", "-n", "1")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if got := strings.Count(out, "\n"); got != 1 {
		t.Fatalf("want exactly 1 line, got %d:\n%s", got, out)
	}
}

func TestLogRange(t *testing.T) {
	dir := newRepoDir(t)
	gitIn(t, dir, "switch", "-c", "feat")
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("y\n"), 0o644)
	gitIn(t, dir, "add", "b.txt")
	gitIn(t, dir, "commit", "-m", "on feat")

	code, out, _ := runCLI(t, dir, "log", "main..HEAD")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if strings.Count(out, "\n") != 1 || !strings.Contains(out, "on feat") {
		t.Fatalf("range output wrong:\n%s", out)
	}
}

func TestLogTooManyArgs(t *testing.T) {
	dir := newRepoDir(t)
	code, _, _ := runCLI(t, dir, "log", "a", "b")
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestLog' -v`
Expected: FAIL — `gg log` hits the `unknown command` default case (exit 2 with output mismatch on the first three tests).

- [ ] **Step 3: Implement**

Create `internal/domain/query_cli.go`:

```go
package domain

// Queries backing the agent-facing CLI read verbs (gg log / diff / show /
// branch current). Same contract as query.go: Read reservation, singleflight
// per key, NoteFailure on genuine errors.

import (
	"context"
	"strconv"

	"github.com/homeend/gigagit/internal/model"
)

// Log returns up to n terse history rows from rev (branch, sha, or A..B
// range), newest first.
func (s *Service) Log(ctx context.Context, rev string, n int) ([]model.LogLine, error) {
	return query(ctx, s, "log:"+rev+":"+strconv.Itoa(n), func(ctx context.Context) ([]model.LogLine, error) {
		return s.repo.LogLines(ctx, rev, n)
	})
}

// CommitLine returns rev's short sha and subject.
func (s *Service) CommitLine(ctx context.Context, rev string) (model.LogLine, error) {
	return query(ctx, s, "commitline:"+rev, func(ctx context.Context) (model.LogLine, error) {
		return s.repo.CommitLine(ctx, rev)
	})
}
```

Create `internal/cli/log.go`:

```go
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/homeend/gigagit/internal/domain"
)

// cmdLog implements `gg log [-n N] [<rev>|<A..B>]` — terse history:
// one "<short-sha> <subject>" line per commit, newest first.
func cmdLog(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("log", flag.ContinueOnError)
	fs.SetOutput(stderr)
	n := fs.Int("n", 10, "number of commits to show")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 1 {
		fmt.Fprintln(stderr, "usage: gg log [-n N] [<rev>|<A..B>]")
		return 2
	}
	rev := "HEAD"
	if fs.NArg() == 1 && fs.Arg(0) != "" {
		rev = fs.Arg(0)
	}
	lines, err := svc.Log(context.Background(), rev, *n)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	for _, l := range lines {
		fmt.Fprintf(stdout, "%s %s\n", l.Hash, l.Subject)
	}
	return 0
}
```

In `internal/cli/cli.go`: add a switch case (alphabetically near "checkout"):

```go
	case "log":
		return cmdLog(svc, rest, stdout, stderr)
```

and add `"log": true,` to the `commands` map (this map gates CLI-vs-TUI routing in `cmd/gg` via `IsCommand` — forgetting it makes `gg log` launch the TUI).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run 'TestLog' -v && go test ./internal/domain/ ./internal/cli/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/query_cli.go internal/cli/log.go internal/cli/log_test.go internal/cli/cli.go
git commit -m "feat(cli): gg log — terse sha+subject history"
```

---

### Task 3: git verbs `DiffNumstat`/`DiffPatch` + `ParseNumstat` (+ model types)

**Files:**
- Modify: `internal/model/model.go` (add `DiffSpec`, `DiffStat`)
- Create: `internal/git/diff_raw.go`
- Test: `internal/git/diff_raw_test.go`

**Interfaces:**
- Consumes: `gitcmd` builder (`Arg`, `ArgIf`), FakeRunner.
- Produces: `model.DiffSpec{Cached bool; Rev string; Paths []string}`; `model.DiffStat{Path, OldPath string; Added, Deleted int; Binary bool}`; `(r *Repo) DiffNumstat(ctx, spec model.DiffSpec) (string, error)`; `(r *Repo) DiffPatch(ctx, spec model.DiffSpec) (string, error)`; exported pure `ParseNumstat(out string) []model.DiffStat`. Used by Task 4 (domain+CLI diff) and Task 5 (show).

- [ ] **Step 1: Write the failing tests**

`internal/git/diff_raw_test.go`:

```go
package git

import (
	"context"
	"reflect"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
)

func TestDiffNumstatArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git diff", gitexec.Result{})
	r := &Repo{Runner: f}
	spec := model.DiffSpec{Cached: true, Rev: "main..HEAD", Paths: []string{"a.go", "b.go"}}
	if _, err := r.DiffNumstat(context.Background(), spec); err != nil {
		t.Fatalf("DiffNumstat: %v", err)
	}
	want := []string{"diff", "--numstat", "-z", "--cached", "main..HEAD", "--", "a.go", "b.go"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
}

func TestDiffPatchArgvMinimal(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git diff", gitexec.Result{Stdout: "PATCH"})
	r := &Repo{Runner: f}
	out, err := r.DiffPatch(context.Background(), model.DiffSpec{})
	if err != nil || out != "PATCH" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	want := []string{"diff"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
}

func TestParseNumstat(t *testing.T) {
	// Real -z shapes (verified empirically): ordinary "A\tD\tpath\x00",
	// rename "A\tD\t\x00old\x00new\x00", binary "-\t-\tpath\x00".
	in := "3\t1\tmain.go\x00-\t-\timg.png\x001\t0\t\x00old.go\x00new.go\x00"
	got := ParseNumstat(in)
	want := []model.DiffStat{
		{Path: "main.go", Added: 3, Deleted: 1},
		{Path: "img.png", Binary: true},
		{Path: "new.go", OldPath: "old.go", Added: 1, Deleted: 0},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestParseNumstatEmpty(t *testing.T) {
	if got := ParseNumstat(""); len(got) != 0 {
		t.Fatalf("got %+v, want empty", got)
	}
}

func TestParseNumstatTruncatedRename(t *testing.T) {
	// A rename record cut off before its two path fields must not panic.
	if got := ParseNumstat("1\t0\t\x00old.go"); len(got) != 1 || got[0].Path != "old.go" || got[0].OldPath != "" {
		// acceptable: the lone trailing field is dropped OR treated as
		// incomplete; the invariant under test is "no panic, no bogus entry
		// with empty Path".
		for _, s := range got {
			if s.Path == "" {
				t.Fatalf("entry with empty path: %+v", got)
			}
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/git/ -run 'TestDiff|TestParseNumstat' -v`
Expected: FAIL — `undefined: model.DiffSpec` etc.

- [ ] **Step 3: Implement**

Add to `internal/model/model.go` (below `LogLine`):

```go
// DiffSpec addresses a diff: working tree (zero value), the index
// (Cached), a commit or range (Rev), optionally narrowed to Paths.
type DiffSpec struct {
	Cached bool
	Rev    string // "", a commit-ish, or a range string (A..B / A...B)
	Paths  []string
}

// DiffStat is one file's terse change stat (from git --numstat).
type DiffStat struct {
	Path    string
	OldPath string // non-empty for renames; Path is then the new name
	Added   int
	Deleted int
	Binary  bool
}
```

Create `internal/git/diff_raw.go`:

```go
package git

import (
	"context"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
	"github.com/homeend/gigagit/internal/model"
)

// DiffNumstat returns `git diff --numstat -z` output for spec (raw, parse
// with ParseNumstat). -z keeps paths verbatim (no core.quotepath mangling)
// and makes rename records unambiguous.
func (r *Repo) DiffNumstat(ctx context.Context, spec model.DiffSpec) (string, error) {
	b := gitcmd.New("diff").Arg("--numstat", "-z").
		ArgIf(spec.Cached, "--cached").
		ArgIf(spec.Rev != "", spec.Rev)
	if len(spec.Paths) > 0 {
		b.Arg("--").Arg(spec.Paths...)
	}
	res, err := r.Runner.Run(ctx, "git diff", b.ToArgv())
	if err != nil {
		return "", err
	}
	return res.Stdout, nil
}

// DiffPatch returns the full patch for spec, exactly as git prints it.
func (r *Repo) DiffPatch(ctx context.Context, spec model.DiffSpec) (string, error) {
	b := gitcmd.New("diff").
		ArgIf(spec.Cached, "--cached").
		ArgIf(spec.Rev != "", spec.Rev)
	if len(spec.Paths) > 0 {
		b.Arg("--").Arg(spec.Paths...)
	}
	res, err := r.Runner.Run(ctx, "git diff", b.ToArgv())
	if err != nil {
		return "", err
	}
	return res.Stdout, nil
}

// ParseNumstat parses `--numstat -z` records: "A\tD\tpath\x00" ordinarily;
// a rename leaves the path field empty and appends old and new as the next
// two NUL fields; binary files carry "-" counts.
func ParseNumstat(out string) []model.DiffStat {
	fields := strings.Split(out, "\x00")
	var stats []model.DiffStat
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if f == "" {
			continue
		}
		parts := strings.SplitN(f, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		st := model.DiffStat{Path: parts[2]}
		if parts[0] == "-" {
			st.Binary = true
		} else {
			st.Added, _ = strconv.Atoi(parts[0])
			st.Deleted, _ = strconv.Atoi(parts[1])
		}
		if st.Path == "" { // rename: the next two fields are old, new
			if i+2 >= len(fields) {
				break
			}
			st.OldPath, st.Path = fields[i+1], fields[i+2]
			i += 2
		}
		stats = append(stats, st)
	}
	return stats
}
```

Note on `TestParseNumstatTruncatedRename`: with this implementation `"1\t0\t\x00old.go"` splits into fields `["1\t0\t", "old.go"]`; `i+2 >= len(fields)` → `break`, then `"old.go"` is never reached — result is empty, which satisfies the test's no-bogus-entry invariant.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/git/ -run 'TestDiff|TestParseNumstat' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/model/model.go internal/git/diff_raw.go internal/git/diff_raw_test.go
git commit -m "feat(git): DiffNumstat/DiffPatch verbs + numstat -z parser"
```

---

### Task 4: domain diff queries + CLI `gg diff`

**Files:**
- Modify: `internal/domain/query_cli.go` (add `DiffStat`, `DiffPatch`)
- Create: `internal/cli/diff.go`
- Modify: `internal/cli/cli.go` (switch case + `commands` map)
- Test: `internal/cli/diff_test.go`

**Interfaces:**
- Consumes: Task 3's verbs + `git.ParseNumstat` + model types; the `query` helper.
- Produces: `(s *Service) DiffStat(ctx, spec model.DiffSpec) ([]model.DiffStat, error)`; `(s *Service) DiffPatch(ctx, spec model.DiffSpec) (string, error)`; CLI `gg diff [--stat|--name-only] [--cached] [<rev>] [-- <paths>]`; unexported CLI helper `renderStat(w io.Writer, stats []model.DiffStat)` reused by Task 5's `gg show`.

- [ ] **Step 1: Write the failing tests**

`internal/cli/diff_test.go`:

```go
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestRenderStat(t *testing.T) {
	var buf bytes.Buffer
	renderStat(&buf, []model.DiffStat{
		{Path: "main.go", Added: 3, Deleted: 1},
		{Path: "img.png", Binary: true},
		{Path: "new.go", OldPath: "old.go", Added: 1},
	})
	want := "main.go +3 -1\nimg.png bin\nold.go => new.go +1 -0\n3 files +4 -1\n"
	if buf.String() != want {
		t.Fatalf("got:\n%q\nwant:\n%q", buf.String(), want)
	}
}

func TestRenderStatEmpty(t *testing.T) {
	var buf bytes.Buffer
	renderStat(&buf, nil)
	if buf.String() != "" {
		t.Fatalf("empty diff must print nothing, got %q", buf.String())
	}
}

func TestDiffStatWorkingTree(t *testing.T) {
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\nmore\nlines\n"), 0o644)
	code, out, errb := runCLI(t, dir, "diff", "--stat")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	want := "README.md +2 -0\n1 files +2 -0\n"
	if out != want {
		t.Fatalf("got %q, want %q", out, want)
	}
}

func TestDiffPatchDefault(t *testing.T) {
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644)
	code, out, _ := runCLI(t, dir, "diff")
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if !strings.Contains(out, "-hi") || !strings.Contains(out, "+changed") {
		t.Fatalf("patch missing hunks:\n%s", out)
	}
}

func TestDiffNameOnly(t *testing.T) {
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644)
	code, out, _ := runCLI(t, dir, "diff", "--name-only")
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if out != "README.md\n" {
		t.Fatalf("got %q", out)
	}
}

func TestDiffEmptyPrintsNothing(t *testing.T) {
	dir := newRepoDir(t)
	for _, mode := range [][]string{{"diff"}, {"diff", "--stat"}, {"diff", "--name-only"}} {
		code, out, _ := runCLI(t, dir, mode...)
		if code != 0 || out != "" {
			t.Fatalf("%v: exit=%d out=%q (want 0, empty)", mode, code, out)
		}
	}
}

func TestDiffPathsRequireDashDash(t *testing.T) {
	dir := newRepoDir(t)
	code, _, _ := runCLI(t, dir, "diff", "main", "README.md")
	if code != 2 {
		t.Fatalf("exit=%d, want 2 (two positionals without --)", code)
	}
}

func TestDiffStatBothFlagsRejected(t *testing.T) {
	dir := newRepoDir(t)
	code, _, _ := runCLI(t, dir, "diff", "--stat", "--name-only")
	if code != 2 {
		t.Fatalf("exit=%d, want 2", code)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run 'TestRenderStat|TestDiff' -v`
Expected: FAIL — `undefined: renderStat`, unknown command "diff".

- [ ] **Step 3: Implement**

Append to `internal/domain/query_cli.go` (add `"strings"` to imports):

```go
// DiffStat returns terse per-file change stats for spec.
func (s *Service) DiffStat(ctx context.Context, spec model.DiffSpec) ([]model.DiffStat, error) {
	key := "diffstat:" + strconv.FormatBool(spec.Cached) + ":" + spec.Rev + ":" + strings.Join(spec.Paths, "\x00")
	return query(ctx, s, key, func(ctx context.Context) ([]model.DiffStat, error) {
		out, err := s.repo.DiffNumstat(ctx, spec)
		if err != nil {
			return nil, err
		}
		return git.ParseNumstat(out), nil
	})
}

// DiffPatch returns the full patch text for spec.
func (s *Service) DiffPatch(ctx context.Context, spec model.DiffSpec) (string, error) {
	key := "diffpatch:" + strconv.FormatBool(spec.Cached) + ":" + spec.Rev + ":" + strings.Join(spec.Paths, "\x00")
	return query(ctx, s, key, func(ctx context.Context) (string, error) {
		return s.repo.DiffPatch(ctx, spec)
	})
}
```

(`internal/domain` already imports `internal/git`; add it to this file's imports as `"github.com/homeend/gigagit/internal/git"`.)

Create `internal/cli/diff.go`:

```go
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// cmdDiff implements `gg diff [--stat|--name-only] [--cached] [<rev>] [-- <paths>]`.
// Default prints the full patch; --stat prints terse "path +A -D" lines with
// an "N files +A -D" trailer; --name-only prints bare paths. Paths must
// follow a "--" separator so a rev is never ambiguous with a path.
func cmdDiff(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	fs.SetOutput(stderr)
	stat := fs.Bool("stat", false, "terse per-file change counts")
	nameOnly := fs.Bool("name-only", false, "changed paths only")
	cached := fs.Bool("cached", false, "diff the index against HEAD")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *stat && *nameOnly {
		fmt.Fprintln(stderr, "diff: --stat and --name-only are mutually exclusive")
		return 2
	}
	rev, paths, ok := splitRevAndPaths(fs.Args())
	if !ok {
		fmt.Fprintln(stderr, "usage: gg diff [--stat|--name-only] [--cached] [<rev>|<A..B>] [-- <paths>...]")
		return 2
	}
	spec := model.DiffSpec{Cached: *cached, Rev: rev, Paths: paths}
	if *stat || *nameOnly {
		stats, err := svc.DiffStat(context.Background(), spec)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		if *nameOnly {
			for _, s := range stats {
				fmt.Fprintln(stdout, s.Path)
			}
			return 0
		}
		renderStat(stdout, stats)
		return 0
	}
	patch, err := svc.DiffPatch(context.Background(), spec)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	io.WriteString(stdout, patch)
	return 0
}

// splitRevAndPaths splits positional args at "--": at most one rev before
// it, paths after. Without "--", a single arg is a rev and two or more is
// an error (ambiguous).
func splitRevAndPaths(args []string) (rev string, paths []string, ok bool) {
	for i, a := range args {
		if a == "--" {
			before := args[:i]
			if len(before) > 1 {
				return "", nil, false
			}
			if len(before) == 1 {
				rev = before[0]
			}
			return rev, args[i+1:], true
		}
	}
	switch len(args) {
	case 0:
		return "", nil, true
	case 1:
		return args[0], nil, true
	default:
		return "", nil, false
	}
}

// renderStat prints the terse stat block: "path +A -D" per file ("path bin"
// for binaries, "old => new +A -D" for renames) and an "N files +A -D"
// trailer. Empty input prints nothing.
func renderStat(w io.Writer, stats []model.DiffStat) {
	if len(stats) == 0 {
		return
	}
	add, del := 0, 0
	for _, s := range stats {
		name := s.Path
		if s.OldPath != "" {
			name = s.OldPath + " => " + s.Path
		}
		if s.Binary {
			fmt.Fprintf(w, "%s bin\n", name)
			continue
		}
		add += s.Added
		del += s.Deleted
		fmt.Fprintf(w, "%s +%d -%d\n", name, s.Added, s.Deleted)
	}
	fmt.Fprintf(w, "%d files +%d -%d\n", len(stats), add, del)
}
```

In `internal/cli/cli.go`: add the switch case and `"diff": true,` to `commands`:

```go
	case "diff":
		return cmdDiff(svc, rest, stdout, stderr)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run 'TestRenderStat|TestDiff' -v && go test ./internal/cli/ ./internal/domain/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/query_cli.go internal/cli/diff.go internal/cli/diff_test.go internal/cli/cli.go
git commit -m "feat(cli): gg diff — patch, terse --stat, --name-only"
```

---

### Task 5: `gg show` (verbs + domain + CLI)

**Files:**
- Create: `internal/git/show_verbs.go`
- Modify: `internal/domain/query_cli.go` (add `ShowStat`, `ShowPatch`)
- Create: `internal/cli/show.go`
- Modify: `internal/cli/cli.go` (switch case + `commands` map)
- Test: `internal/git/show_verbs_test.go`, `internal/cli/show_test.go`

**Interfaces:**
- Consumes: Task 1's `CommitLine` (domain), Task 3's `ParseNumstat`, Task 4's `renderStat`.
- Produces: `(r *Repo) ShowNumstat(ctx, rev string, paths []string) (string, error)`; `(r *Repo) ShowPatch(ctx, rev string, paths []string) (string, error)`; `(s *Service) ShowStat(ctx, rev string, paths []string) (model.LogLine, []model.DiffStat, error)`; `(s *Service) ShowPatch(ctx, rev string, paths []string) (model.LogLine, string, error)`; CLI `gg show <commit> [--patch] [-- <file>]`.

- [ ] **Step 1: Write the failing tests**

`internal/git/show_verbs_test.go`:

```go
package git

import (
	"context"
	"reflect"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
)

func TestShowNumstatArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git show", gitexec.Result{})
	r := &Repo{Runner: f}
	if _, err := r.ShowNumstat(context.Background(), "abc123", []string{"a.go"}); err != nil {
		t.Fatalf("ShowNumstat: %v", err)
	}
	want := []string{"show", "--numstat", "-z", "--format=", "abc123", "--", "a.go"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
}

func TestShowPatchArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git show", gitexec.Result{Stdout: "PATCH"})
	r := &Repo{Runner: f}
	out, err := r.ShowPatch(context.Background(), "abc123", nil)
	if err != nil || out != "PATCH" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	want := []string{"show", "--patch", "--format=", "abc123"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
}
```

`internal/cli/show_test.go`:

```go
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShowDefaultStat(t *testing.T) {
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\n"), 0o644)
	gitIn(t, dir, "add", "a.txt")
	gitIn(t, dir, "commit", "-m", "add a")

	code, out, errb := runCLI(t, dir, "show", "HEAD")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	// header, one stat line, trailer
	if len(lines) != 3 {
		t.Fatalf("want 3 lines, got %d:\n%s", len(lines), out)
	}
	if !strings.HasSuffix(lines[0], " add a") {
		t.Fatalf("header = %q", lines[0])
	}
	if lines[1] != "a.txt +2 -0" || lines[2] != "1 files +2 -0" {
		t.Fatalf("stat block wrong:\n%s", out)
	}
}

func TestShowPatchFlag(t *testing.T) {
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644)
	gitIn(t, dir, "add", "a.txt")
	gitIn(t, dir, "commit", "-m", "add a")

	code, out, _ := runCLI(t, dir, "show", "--patch", "HEAD")
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if !strings.Contains(out, "+one") || !strings.HasSuffix(strings.Split(out, "\n")[0], " add a") {
		t.Fatalf("patch output wrong:\n%s", out)
	}
}

func TestShowRequiresCommit(t *testing.T) {
	dir := newRepoDir(t)
	code, _, _ := runCLI(t, dir, "show")
	if code != 2 {
		t.Fatalf("exit=%d, want 2", code)
	}
}

func TestShowFileScope(t *testing.T) {
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("two\n"), 0o644)
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "add both")

	code, out, _ := runCLI(t, dir, "show", "HEAD", "--", "a.txt")
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if strings.Contains(out, "b.txt") || !strings.Contains(out, "a.txt +1 -0") {
		t.Fatalf("scope wrong:\n%s", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/git/ -run 'TestShow' -v && go test ./internal/cli/ -run 'TestShow' -v`
Expected: FAIL — undefined verbs / unknown command "show".

- [ ] **Step 3: Implement**

Create `internal/git/show_verbs.go`:

```go
package git

import (
	"context"

	"github.com/homeend/gigagit/internal/gitcmd"
)

// ShowNumstat returns `git show --numstat -z --format=` output for rev,
// optionally scoped to paths — the commit's change stats with no message
// block. On a merge commit git prints an empty combined stat, which renders
// as "no changes" downstream (matching git's own behavior).
func (r *Repo) ShowNumstat(ctx context.Context, rev string, paths []string) (string, error) {
	b := gitcmd.New("show").Arg("--numstat", "-z", "--format=", rev)
	if len(paths) > 0 {
		b.Arg("--").Arg(paths...)
	}
	res, err := r.Runner.Run(ctx, "git show", b.ToArgv())
	if err != nil {
		return "", err
	}
	return res.Stdout, nil
}

// ShowPatch returns rev's full patch (no message block), optionally scoped
// to paths.
func (r *Repo) ShowPatch(ctx context.Context, rev string, paths []string) (string, error) {
	b := gitcmd.New("show").Arg("--patch", "--format=", rev)
	if len(paths) > 0 {
		b.Arg("--").Arg(paths...)
	}
	res, err := r.Runner.Run(ctx, "git show", b.ToArgv())
	if err != nil {
		return "", err
	}
	return res.Stdout, nil
}
```

Append to `internal/domain/query_cli.go`:

```go
// ShowStat returns rev's header line plus terse per-file stats.
func (s *Service) ShowStat(ctx context.Context, rev string, paths []string) (model.LogLine, []model.DiffStat, error) {
	type showStat struct {
		line  model.LogLine
		stats []model.DiffStat
	}
	v, err := query(ctx, s, "showstat:"+rev+":"+strings.Join(paths, "\x00"), func(ctx context.Context) (showStat, error) {
		line, err := s.repo.CommitLine(ctx, rev)
		if err != nil {
			return showStat{}, err
		}
		out, err := s.repo.ShowNumstat(ctx, rev, paths)
		if err != nil {
			return showStat{}, err
		}
		return showStat{line: line, stats: git.ParseNumstat(out)}, nil
	})
	return v.line, v.stats, err
}

// ShowPatch returns rev's header line plus its full patch text.
func (s *Service) ShowPatch(ctx context.Context, rev string, paths []string) (model.LogLine, string, error) {
	type showPatch struct {
		line  model.LogLine
		patch string
	}
	v, err := query(ctx, s, "showpatch:"+rev+":"+strings.Join(paths, "\x00"), func(ctx context.Context) (showPatch, error) {
		line, err := s.repo.CommitLine(ctx, rev)
		if err != nil {
			return showPatch{}, err
		}
		patch, err := s.repo.ShowPatch(ctx, rev, paths)
		if err != nil {
			return showPatch{}, err
		}
		return showPatch{line: line, patch: patch}, nil
	})
	return v.line, v.patch, err
}
```

Create `internal/cli/show.go`:

```go
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/homeend/gigagit/internal/domain"
)

// cmdShow implements `gg show <commit> [--patch] [-- <file>...]` — a
// "<short-sha> <subject>" header followed by the terse stat block
// (default) or the full patch (--patch).
func cmdShow(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	fs.SetOutput(stderr)
	patch := fs.Bool("patch", false, "print the full patch instead of the stat block")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rev, paths, ok := splitRevAndPaths(fs.Args())
	if !ok || rev == "" {
		fmt.Fprintln(stderr, "usage: gg show <commit> [--patch] [-- <file>...]")
		return 2
	}
	ctx := context.Background()
	if *patch {
		line, text, err := svc.ShowPatch(ctx, rev, paths)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		fmt.Fprintf(stdout, "%s %s\n", line.Hash, line.Subject)
		io.WriteString(stdout, text)
		return 0
	}
	line, stats, err := svc.ShowStat(ctx, rev, paths)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	fmt.Fprintf(stdout, "%s %s\n", line.Hash, line.Subject)
	renderStat(stdout, stats)
	return 0
}
```

In `internal/cli/cli.go`: add the switch case and `"show": true,` to `commands`:

```go
	case "show":
		return cmdShow(svc, rest, stdout, stderr)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/git/ -run 'TestShow' -v && go test ./internal/cli/ -run 'TestShow' -v && go test ./internal/domain/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/git/show_verbs.go internal/git/show_verbs_test.go internal/domain/query_cli.go internal/cli/show.go internal/cli/show_test.go internal/cli/cli.go
git commit -m "feat(cli): gg show — header + terse stat, --patch"
```

---

### Task 6: `engine.Stage` gains `All`; CLI `gg add` / `gg unstage`

**Files:**
- Modify: `internal/engine/stage.go` (add `All` field + validation)
- Modify: `internal/engine/gitops.go` (add `StageAll` to the interface)
- Modify: `internal/git/stage.go` (add `StageAll` verb)
- Create: `internal/cli/add.go`
- Modify: `internal/cli/cli.go` (two switch cases + `commands` map)
- Test: `internal/engine/stage_test.go` (extend), `internal/git/stage_test.go` (create if absent — check first), `internal/cli/add_test.go`

**Interfaces:**
- Consumes: existing `engine.Stage{Paths, Unstage}`, `runOperation`, `finish`, engine test helpers `newRepo(t) (string, *git.Repo)` / `gitOut(t, dir, args...)` / `gitE(t, dir, args...)` from `internal/engine/ops_basic_test.go`.
- Produces: `engine.Stage{Paths []string; All bool; Unstage bool}` (All ⇒ `git add -A`); `(r *Repo) StageAll(ctx) error`; CLI `gg add (-A | <path>...)`, `gg unstage <path>...`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/engine/stage_test.go`:

```go
func TestStageAllStagesUntracked(t *testing.T) {
	dir, repo := newRepo(t)
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644)

	res, err := Stage{All: true}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("stage all: %v", err)
	}
	if !res.Changed || !strings.Contains(res.Summary, "staged all") {
		t.Fatalf("result = %+v", res)
	}
	if out := gitOut(t, dir, "diff", "--cached", "--name-only"); !strings.Contains(out, "new.txt") {
		t.Fatalf("new.txt not staged; cached names = %q", out)
	}
}

func TestStageAllRejectsPaths(t *testing.T) {
	_, repo := newRepo(t)
	if _, err := (Stage{All: true, Paths: []string{"x"}}).Run(context.Background(), OpDeps{Repo: repo}); err == nil {
		t.Fatal("want error for All+Paths")
	}
}

func TestStageAllRejectsUnstage(t *testing.T) {
	_, repo := newRepo(t)
	if _, err := (Stage{All: true, Unstage: true}).Run(context.Background(), OpDeps{Repo: repo}); err == nil {
		t.Fatal("want error for All+Unstage")
	}
}
```

`internal/cli/add_test.go`:

```go
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddPathThenCommitNewFile(t *testing.T) {
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644)

	code, _, errb := runCLI(t, dir, "add", "new.txt")
	if code != 0 {
		t.Fatalf("add exit=%d stderr=%s", code, errb)
	}
	// the previously-impossible flow: commit a brand-new file via gg only
	code, out, errb := runCLI(t, dir, "commit", "-m", "add new file")
	if code != 0 {
		t.Fatalf("commit exit=%d stderr=%s", code, errb)
	}
	_ = out
	code, out, _ = runCLI(t, dir, "status")
	if code != 0 || !strings.Contains(out, "clean") {
		t.Fatalf("expected clean tree:\n%s", out)
	}
}

func TestAddAllFlag(t *testing.T) {
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644)
	code, _, errb := runCLI(t, dir, "add", "-A")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	code, out, _ := runCLI(t, dir, "status")
	if code != 0 || !strings.Contains(out, "A  new.txt") {
		t.Fatalf("new.txt not staged:\n%s", out)
	}
}

func TestAddUsageErrors(t *testing.T) {
	dir := newRepoDir(t)
	if code, _, _ := runCLI(t, dir, "add"); code != 2 {
		t.Fatalf("bare add: exit=%d, want 2", code)
	}
	if code, _, _ := runCLI(t, dir, "add", "-A", "x.txt"); code != 2 {
		t.Fatalf("add -A with path: exit=%d, want 2", code)
	}
}

func TestUnstage(t *testing.T) {
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644)
	runCLI(t, dir, "add", "new.txt")

	code, _, errb := runCLI(t, dir, "unstage", "new.txt")
	if code != 0 {
		t.Fatalf("unstage exit=%d stderr=%s", code, errb)
	}
	code, out, _ := runCLI(t, dir, "status")
	if code != 0 || !strings.Contains(out, "?? new.txt") {
		t.Fatalf("new.txt should be untracked again:\n%s", out)
	}
	if code, _, _ := runCLI(t, dir, "unstage"); code != 2 {
		t.Fatal("bare unstage must be usage error")
	}
}
```

Note: check `gg status`'s exact XY rendering for an added file in `cmdStatus` (`internal/cli/cli.go:124` — `%c%c %s`, staged `A`, unstaged space → `"A  new.txt"`; untracked renders `?? new.txt`). If the assertion fails, read the actual output and match it — the invariant under test is staged-vs-untracked, not the exact glyphs.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/engine/ -run 'TestStageAll' -v && go test ./internal/cli/ -run 'TestAdd|TestUnstage' -v`
Expected: FAIL — `unknown field All`, unknown command "add".

- [ ] **Step 3: Implement**

`internal/git/stage.go` — append:

```go
// StageAll stages every change in the working tree, including untracked
// files (git add -A).
func (r *Repo) StageAll(ctx context.Context) error {
	_, err := r.Runner.Run(ctx, "git add", gitcmd.New("add").Arg("-A").ToArgv())
	return err
}
```

`internal/engine/gitops.go` — add to the `GitOps` interface next to `StagePaths`:

```go
	// StageAll stages all changes including untracked files (git add -A).
	StageAll(ctx context.Context) error
```

`internal/engine/stage.go` — replace the type and the top of `Run`:

```go
// Stage stages (or, with Unstage, unstages) the given paths in the index.
// All stages everything including untracked files (git add -A) and is
// mutually exclusive with Paths and Unstage. It takes no decisions and
// emits a single Progress; the default TreeWrite reservation applies (it
// writes .git/index).
type Stage struct {
	Paths   []string
	All     bool
	Unstage bool
}
```

and at the start of `Run`:

```go
	if op.All {
		if op.Unstage {
			return Result{}, fmt.Errorf("stage: All cannot unstage")
		}
		if len(op.Paths) > 0 {
			return Result{}, fmt.Errorf("stage: All and explicit paths are mutually exclusive")
		}
		deps.emit(ctx, Progress{Step: "staged", Detail: "all changes"})
		if err := deps.Repo.StageAll(ctx); err != nil {
			return Result{}, fmt.Errorf("stage: %w", err)
		}
		return Result{Summary: "staged all changes", Changed: true}, nil
	}
	if len(op.Paths) == 0 {
		return Result{}, fmt.Errorf("stage: no paths")
	}
```

(the rest of the existing body is unchanged).

Create `internal/cli/add.go`:

```go
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

// cmdAdd implements `gg add (-A | <path>...)` — stage named paths, or with
// -A everything including untracked files.
func cmdAdd(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	all := fs.Bool("A", false, "stage all changes, including untracked files")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *all == (fs.NArg() > 0) { // exactly one of -A / paths
		fmt.Fprintln(stderr, "usage: gg add (-A | <path>...)")
		return 2
	}
	res, err := runOperation(context.Background(), svc,
		engine.Stage{Paths: fs.Args(), All: *all}, cliDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}

// cmdUnstage implements `gg unstage <path>...` — remove paths from the
// index, keeping working-tree content.
func cmdUnstage(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: gg unstage <path>...")
		return 2
	}
	res, err := runOperation(context.Background(), svc,
		engine.Stage{Paths: args, Unstage: true}, cliDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}
```

In `internal/cli/cli.go`: add both switch cases and `"add": true, "unstage": true,` to `commands`:

```go
	case "add":
		return cmdAdd(svc, rest, stdout, stderr)
	case "unstage":
		return cmdUnstage(svc, rest, stdout, stderr)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/engine/ -run 'TestStage' -v && go test ./internal/cli/ -run 'TestAdd|TestUnstage' -v && go test ./internal/git/`
Expected: PASS (including the pre-existing Stage tests).

- [ ] **Step 5: Commit**

```bash
git add internal/git/stage.go internal/engine/stage.go internal/engine/gitops.go internal/engine/stage_test.go internal/cli/add.go internal/cli/add_test.go internal/cli/cli.go
git commit -m "feat(cli): gg add/unstage — staging incl. untracked via engine.Stage All"
```

---

### Task 7: `engine.Commit` summary includes the new sha

**Files:**
- Modify: `internal/engine/ops_basic.go` (Commit op, ~lines 10-30)
- Modify: `internal/engine/gitops.go` (add `CommitLine` to the interface; may need the `model` import)
- Test: `internal/engine/ops_basic_test.go` (extend)

**Interfaces:**
- Consumes: Task 1's `(r *Repo) CommitLine(ctx, rev) (model.LogLine, error)`.
- Produces: commit summaries of the form `committed <short-sha> <subject>` / `amended <short-sha> <subject>` (CLI prints them via `finish` as `✓ committed <sha> <subject>`; the TUI status line inherits them).

- [ ] **Step 1: Write the failing test**

Append to `internal/engine/ops_basic_test.go`:

```go
func TestCommitSummaryIncludesSha(t *testing.T) {
	dir, repo := newRepo(t)
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\n"), 0o644)

	res, err := Commit{Message: "sha in summary", All: true}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	sha := strings.TrimSpace(gitOut(t, dir, "rev-parse", "--short", "HEAD"))
	want := "committed " + sha + " sha in summary"
	if res.Summary != want {
		t.Fatalf("summary = %q, want %q", res.Summary, want)
	}
}
```

(If `ops_basic_test.go` lacks `os`/`filepath`/`strings` imports, add them.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/ -run TestCommitSummaryIncludesSha -v`
Expected: FAIL — summary is `"committed"`.

- [ ] **Step 3: Implement**

`internal/engine/gitops.go` — add to `GitOps` (and `"github.com/homeend/gigagit/internal/model"` to imports if absent):

```go
	// CommitLine returns rev's short sha and subject (git log -1).
	CommitLine(ctx context.Context, rev string) (model.LogLine, error)
```

`internal/engine/ops_basic.go` — in `Commit.Run`, replace the summary lines:

```go
	summary := "committed"
	if op.Amend {
		summary = "amended"
	}
	// Best-effort: name the commit we just made. The commit itself
	// succeeded, so a failed read only costs the sha in the summary.
	if line, lerr := deps.Repo.CommitLine(ctx, "HEAD"); lerr == nil {
		summary += " " + line.Hash + " " + line.Subject
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/engine/ -run 'TestCommit' -v && go test ./internal/cli/ -run 'TestCommit' -v`
Expected: PASS — including the pre-existing engine Commit tests and CLI commit tests (they assert exit codes/clean status, not the summary text; if any asserts the old summary literal, update it to expect the `committed <sha> <subject>` form).

- [ ] **Step 5: Commit**

```bash
git add internal/engine/ops_basic.go internal/engine/gitops.go internal/engine/ops_basic_test.go
git commit -m "feat(engine): commit summary names the new sha + subject"
```

---

### Task 8: `gg worktree prune`

**Files:**
- Create: `internal/engine/prune_worktrees.go`
- Modify: `internal/engine/gitops.go` (add `PruneWorktrees`)
- Modify: `internal/git/worktree.go` (add the verb — put it beside the existing worktree verbs; check the actual filename with `grep -rn "func (r \*Repo) AddWorktree" internal/git/`)
- Modify: `internal/cli/worktree.go` (dispatch case + usage strings)
- Test: `internal/engine/prune_worktrees_test.go`, `internal/cli/worktree_test.go` (extend)

**Interfaces:**
- Consumes: engine op pattern (`Prune` in `internal/engine/prune.go` is the model), `runOperation`/`finish`.
- Produces: `engine.PruneWorktrees{}` (default TreeWrite lock); `(r *Repo) PruneWorktrees(ctx) error`; CLI `gg worktree prune`.

- [ ] **Step 1: Write the failing tests**

`internal/engine/prune_worktrees_test.go`:

```go
package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPruneWorktreesRemovesStaleAdminDir(t *testing.T) {
	dir, repo := newRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	gitE(t, dir, "worktree", "add", wt, "-b", "tmp-branch")
	// Simulate a worktree whose directory vanished (the stale state prune cleans).
	os.RemoveAll(wt)

	res, err := PruneWorktrees{}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v", res)
	}
	admin := filepath.Join(dir, ".git", "worktrees")
	entries, _ := os.ReadDir(admin)
	if len(entries) != 0 {
		t.Fatalf("stale admin dirs remain: %v", entries)
	}
}
```

Append to `internal/cli/worktree_test.go`:

```go
func TestWorktreePruneCommand(t *testing.T) {
	dir := newRepoDir(t)
	code, out, errb := runCLI(t, dir, "worktree", "prune")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	if !strings.Contains(out, "pruned") {
		t.Fatalf("out = %q", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/engine/ -run TestPruneWorktrees -v && go test ./internal/cli/ -run TestWorktreePrune -v`
Expected: FAIL — `undefined: PruneWorktrees`, `worktree: unknown subcommand "prune"`.

- [ ] **Step 3: Implement**

Verb (in the file that holds `AddWorktree`):

```go
// PruneWorktrees drops stale $GIT_DIR/worktrees admin entries left by
// deleted worktree directories (git worktree prune).
func (r *Repo) PruneWorktrees(ctx context.Context) error {
	_, err := r.Runner.Run(ctx, "git worktree prune", gitcmd.New("worktree").Arg("prune").ToArgv())
	return err
}
```

`internal/engine/gitops.go` — add to the interface near the worktree methods:

```go
	// PruneWorktrees removes stale worktree admin entries (git worktree prune).
	PruneWorktrees(ctx context.Context) error
```

Create `internal/engine/prune_worktrees.go`:

```go
package engine

import "context"

// PruneWorktrees removes stale $GIT_DIR/worktrees administrative entries
// (git worktree prune). Default TreeWrite reservation: it mutates .git
// admin state. Not dispatched by the TUI, so no opAffectedSources mapping
// is needed.
type PruneWorktrees struct{}

var _ Operation = PruneWorktrees{}

func (op PruneWorktrees) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "pruning worktrees"})
	if err := deps.Repo.PruneWorktrees(ctx); err != nil {
		return Result{}, err
	}
	res := Result{Summary: "pruned stale worktrees", Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}
```

`internal/cli/worktree.go` — add to `cmdWorktree`'s switch and update both usage strings (`usage: gg worktree <list|add|remove|prune> [args]` and the unknown-subcommand message):

```go
	case "prune":
		res, err := runOperation(context.Background(), svc, engine.PruneWorktrees{}, cliDecider{}, stderr)
		return finish(res, err, stdout, stderr)
```

(add the `engine` import to `internal/cli/worktree.go` if it is not already there).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/engine/ -run TestPruneWorktrees -v && go test ./internal/cli/ -run TestWorktree -v`
Expected: PASS (including existing worktree CLI tests).

- [ ] **Step 5: Commit**

```bash
git add internal/git/ internal/engine/prune_worktrees.go internal/engine/prune_worktrees_test.go internal/engine/gitops.go internal/cli/worktree.go internal/cli/worktree_test.go
git commit -m "feat(cli): gg worktree prune"
```

---

### Task 9: `gg branch current` + `gg branch ls`

**Files:**
- Modify: `internal/cli/branch.go` (two subcommands + dispatch + usage string)
- Test: `internal/cli/branch_test.go` (extend)

**Interfaces:**
- Consumes: existing `domain.CurrentBranch` (returns `""` when detached), Task 2's `domain.CommitLine` (detached fallback), existing `domain.Branches` (`[]model.Branch{Name, Upstream, Ahead, Behind, IsHead, ...}`).
- Produces: `gg branch current` (one line: name, or short sha when detached); `gg branch ls` (`* <name> ↑a ↓b` rows; star = HEAD; counts only with an upstream).

- [ ] **Step 1: Write the failing tests**

Append to `internal/cli/branch_test.go`:

```go
func TestBranchCurrent(t *testing.T) {
	dir := newRepoDir(t)
	code, out, errb := runCLI(t, dir, "branch", "current")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	if out != "main\n" {
		t.Fatalf("out = %q, want %q", out, "main\n")
	}
}

func TestBranchCurrentDetached(t *testing.T) {
	dir := newRepoDir(t)
	gitIn(t, dir, "checkout", "--detach", "HEAD")
	code, out, _ := runCLI(t, dir, "branch", "current")
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	sha := strings.TrimSpace(out)
	if len(sha) < 7 || len(sha) > 12 || strings.ContainsAny(sha, " \t") {
		t.Fatalf("detached output should be a short sha, got %q", out)
	}
}

func TestBranchLs(t *testing.T) {
	dir := newRepoDir(t)
	gitIn(t, dir, "branch", "feat-x")
	code, out, _ := runCLI(t, dir, "branch", "ls")
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if !strings.Contains(out, "* main") || !strings.Contains(out, "  feat-x") {
		t.Fatalf("ls output wrong:\n%s", out)
	}
}
```

(`gitIn` comes from Task 2's `log_test.go`; `strings` may need importing.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run TestBranch -v`
Expected: FAIL — `branch: unknown subcommand "current"`.

- [ ] **Step 3: Implement**

In `internal/cli/branch.go`, extend `cmdBranch`'s switch (and its usage line to `<create|rename|delete|current|ls>`):

```go
	case "current":
		return cmdBranchCurrent(svc, stdout, stderr)
	case "ls":
		return cmdBranchLs(svc, stdout, stderr)
```

and add:

```go
// cmdBranchCurrent implements `gg branch current` — the bare branch name,
// or HEAD's short sha when detached.
func cmdBranchCurrent(svc *domain.Service, stdout, stderr io.Writer) int {
	name, err := svc.CurrentBranch(context.Background())
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if name == "" { // detached HEAD
		line, lerr := svc.CommitLine(context.Background(), "HEAD")
		if lerr != nil {
			fmt.Fprintln(stderr, "error:", lerr)
			return 1
		}
		name = line.Hash
	}
	fmt.Fprintln(stdout, name)
	return 0
}

// cmdBranchLs implements `gg branch ls` — one local branch per line,
// "* " marking HEAD, "↑a ↓b" only when an upstream exists.
func cmdBranchLs(svc *domain.Service, stdout, stderr io.Writer) int {
	branches, err := svc.Branches(context.Background())
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	for _, b := range branches {
		marker := "  "
		if b.IsHead {
			marker = "* "
		}
		if b.Upstream != "" {
			fmt.Fprintf(stdout, "%s%s ↑%d ↓%d\n", marker, b.Name, b.Ahead, b.Behind)
		} else {
			fmt.Fprintf(stdout, "%s%s\n", marker, b.Name)
		}
	}
	return 0
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ -run TestBranch -v`
Expected: PASS (including pre-existing branch tests).

- [ ] **Step 5: Commit**

```bash
git add internal/cli/branch.go internal/cli/branch_test.go
git commit -m "feat(cli): gg branch current + branch ls"
```

---

### Task 10: e2e scenarios

**Files:**
- Create: `e2e/scenarios/sNN_cli_log_show.toml`, `e2e/scenarios/sNN_cli_add_commit.toml` (pick NN: run `ls e2e/scenarios/ | sort | tail -3` and use the next free number for each)

**Interfaces:**
- Consumes: the e2e schema (`e2e/scenario.go`; `[[run]]` supports `cmd`, `exit`, `stdout_contains`, `stdout_excludes`). Before writing, read the project skill `.claude/skills/writing-e2e-scenarios/SKILL.md` — it documents the schema and the common wrong-expectation mistakes.
- Produces: regression coverage of the new verbs at the user-visible level.

- [ ] **Step 1: Read the skill, then write the scenarios**

`sNN_cli_log_show.toml`:

```toml
name = "cli: gg log and gg show print terse history and stat"

[input]
steps = [
  { write = "a.txt", content = "one\ntwo\n" },
  { commit = "first commit" },
  { write = "a.txt", content = "one\ntwo\nthree\n" },
  { commit = "second commit" },
]

[[run]]
cmd  = ["log", "-n", "1"]
exit = 0
stdout_contains = ["second commit"]
stdout_excludes = ["first commit"]

[[run]]
cmd  = ["show", "HEAD"]
exit = 0
stdout_contains = ["second commit", "a.txt +1 -0", "1 files +1 -0"]

[expect]
branch = "main"
```

`sNN_cli_add_commit.toml`:

```toml
name = "cli: gg add stages an untracked file so gg commit can commit it"

[input]
steps = [
  { write = "README.md", content = "hello\n" },
  { commit = "initial" },
  { write = "brand-new.txt", content = "x\n" },
]

[[run]]
cmd  = ["add", "brand-new.txt"]
exit = 0

[[run]]
cmd  = ["commit", "-m", "add brand-new"]
exit = 0
stdout_contains = ["committed", "add brand-new"]

[[run]]
cmd  = ["diff", "--stat"]
exit = 0

[expect]
branch = "main"

[expect.status]
# nothing staged, nothing unstaged — the tree is clean
```

Adjust the `[input]`/`[expect]` details to whatever the skill documents if they differ (e.g. whether an empty `[expect.status]` means "clean" — if the harness requires omitting the table instead, omit it).

- [ ] **Step 2: Run the e2e suite**

Run: `./test.sh e2e`
Expected: PASS, including the two new scenarios (their names print in the run log).

- [ ] **Step 3: Commit**

```bash
git add e2e/scenarios/
git commit -m "test(e2e): scenarios for gg log/show and add→commit flow"
```

---

### Task 11: docs + agent skill bump

**Files:**
- Modify: `CHANGELOG.md`, `README.md`, `CLAUDE.md`, `internal/agentskill/using-gg.md`, `internal/agentskill/agentskill.go` (Version)

**Interfaces:**
- Consumes: everything shipped in Tasks 1-10.
- Produces: user- and agent-facing documentation; a bumped skill version so `gg init --update` refreshes installed copies.

- [ ] **Step 1: CHANGELOG.md** — add an entry at the top following the existing format, covering: `gg log`, `gg diff`, `gg show`, `gg add`/`gg unstage`, `gg branch current`/`ls`, `gg worktree prune`, and commit summaries now naming the new sha.

- [ ] **Step 2: README.md** — extend the CLI section with the new verbs (one line each, mirroring the style used for existing commands).

- [ ] **Step 3: CLAUDE.md** — update the package map rows: `cli` (new read verbs + add/unstage), `engine` (Stage.All, PruneWorktrees, Commit summary includes sha), `git` (LogLines/CommitLine/DiffNumstat/DiffPatch/ShowNumstat/ShowPatch/StageAll/PruneWorktrees).

- [ ] **Step 4: using-gg.md** — insert these command docs in the Commands list (terse formats are the agent contract, so state them exactly):

```markdown
- `gg log [-n N] [<rev>|<A..B>]` — terse history, newest first: one
  `<short-sha> <subject>` line per commit. Default N=10, rev defaults to
  HEAD; ranges (`main..HEAD`) pass through.
- `gg diff [--stat|--name-only] [--cached] [<rev>|<A..B>] [-- <paths>...]` —
  working-tree diff (default), index diff (`--cached`), or commit/range
  diff. Default prints the full patch; `--stat` prints `path +A -D` lines
  plus a `N files +A -D` trailer (`path bin` for binaries); `--name-only`
  prints bare paths. Paths must follow `--`. An empty diff prints nothing.
- `gg show <commit> [--patch] [-- <file>...]` — `<short-sha> <subject>`
  header plus the commit's terse stat block (default) or full patch
  (`--patch`).
- `gg add (-A | <path>...)` / `gg unstage <path>...` — stage paths (or
  everything incl. untracked with `-A`) / remove paths from the index
  keeping working-tree content. `gg add` + `gg commit` fully replaces
  `git add` + `git commit` for new files.
- `gg branch current` — just the branch name (HEAD's short sha when
  detached).
- `gg branch ls` — local branches, `* ` marking HEAD, `↑a ↓b` when an
  upstream exists.
- `gg worktree prune` — drop stale worktree admin entries.
```

Also update the `gg commit` entry to mention the summary now names the new sha (`✓ committed <short-sha> <subject>`).

- [ ] **Step 5: bump the skill version** — in `internal/agentskill/agentskill.go`, increment `Version` by one (read the current value first: `grep -n "Version" internal/agentskill/agentskill.go`).

- [ ] **Step 6: verify + commit**

Run: `go build ./... && go test ./internal/agentskill/`
Expected: build OK, agentskill tests (version marker) pass.

```bash
git add CHANGELOG.md README.md CLAUDE.md internal/agentskill/
git commit -m "docs: document the agent-facing CLI verbs; bump using-gg skill"
```

---

### Task 12: final gate

**Files:** none (verification only)

- [ ] **Step 1: full staged suite with race detector**

Run: `./test.sh race`
Expected: vet+gofmt clean, all unit tests and e2e scenarios PASS. Fix anything that fails before proceeding (archtest will catch any accidental `internal/git` import from `internal/cli`).

- [ ] **Step 2: build and smoke-test the binary**

```bash
go build -o ./gg ./cmd/gg
./gg log -n 3
./gg diff --stat
./gg branch current
./gg show HEAD
```

Expected: terse output per the formats above, no errors. The binary lives at `/mnt/t/others/gigagit/.claude/worktrees/agent-cli-verbs/gg` (absolute path — do not commit it).

- [ ] **Step 3: report** — summarize what shipped and hand back for human review/merge (the human owns the merge to main).

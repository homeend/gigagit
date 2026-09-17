package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// linkState is a registry path inside a fresh temp dir (file not created), so
// every link test stays parallel and hermetic.
func linkState(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "repos.toml")
}

// runLinkCLI runs `gg link`/`gg link resolve` as if invoked from workdir —
// the directory the service is opened at AND the directory gg was asked to
// run in, matching how cmd/gg wires Run/runOne in the real binary. Most
// callers pass the checkout's top level (workdir == top); the rebase tests
// below pass a subdirectory to exercise cwd-relative path arguments.
func runLinkCLI(t *testing.T, workdir string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := runLink(linkState(t), domain.Open(workdir), workdir, args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestLinkPrintsTheRepoLinkWithNoArguments(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	if out, err := exec.Command("git", "-C", dir, "remote", "add", "origin", "git@github.com:homeend/gigagit.git").CombinedOutput(); err != nil {
		t.Fatalf("remote add: %v\n%s", err, out)
	}
	code, out, errb := runLinkCLI(t, dir)
	if code != 0 {
		t.Fatalf("exit = %d (stderr %q)", code, errb)
	}
	if strings.TrimSpace(out) != "gg://gigagit" {
		t.Errorf("stdout = %q, want gg://gigagit", out)
	}
}

func TestLinkFallsBackToTheLocalFormWithoutARemote(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	code, out, errb := runLinkCLI(t, dir, "README.md:4")
	if code != 0 {
		t.Fatalf("exit = %d (stderr %q)", code, errb)
	}
	got := strings.TrimSpace(out)
	if !strings.HasPrefix(got, "gg:///") || !strings.HasSuffix(got, "/README.md:4") {
		t.Fatalf("stdout = %q, want the local form", got)
	}
	// Everything gg link prints must parse back.
	if _, err := model.ParseLink(got); err != nil {
		t.Errorf("ParseLink(%q) = %v", got, err)
	}
}

func TestLinkTargetFlags(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	head := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))
	cases := []struct {
		name string
		args []string
		want string // suffix of the printed link
	}{
		{"cached", []string{"--cached", "README.md:2"}, "/README.md@staged:2"},
		{"rev", []string{"--rev", "HEAD", "README.md:2"}, "/README.md@" + head + ":2"},
		{"old side", []string{"README.md:old:2"}, "/README.md:old:2"},
		{"no line", []string{"README.md"}, "/README.md"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			code, out, errb := runLinkCLI(t, dir, tc.args...)
			if code != 0 {
				t.Fatalf("exit = %d (stderr %q)", code, errb)
			}
			got := strings.TrimSpace(out)
			if !strings.HasSuffix(got, tc.want) {
				t.Errorf("stdout = %q, want a link ending %q", got, tc.want)
			}
			if _, err := model.ParseLink(got); err != nil {
				t.Errorf("ParseLink(%q) = %v", got, err)
			}
		})
	}
}

// TestLinkPathIsRebasedToTheCheckoutTop covers ruling P20: the link
// grammar's <path> is always top-level-relative, so `gg link` must rebase a
// cwd-relative argument onto the checkout top rather than embed it verbatim.
func TestLinkPathIsRebasedToTheCheckoutTop(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "a.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	for _, args := range [][]string{{"add", "sub/a.txt"}, {"commit", "-m", "add sub/a.txt"}} {
		c := exec.Command("git", append([]string{"-C", dir}, args...)...)
		c.Env = env
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	t.Run("from a subdirectory", func(t *testing.T) {
		t.Parallel()
		code, out, errb := runLinkCLI(t, sub, "a.txt:3")
		if code != 0 {
			t.Fatalf("exit = %d (stderr %q)", code, errb)
		}
		got := strings.TrimSpace(out)
		if !strings.HasSuffix(got, "/sub/a.txt:3") {
			t.Errorf("stdout = %q, want a link ending /sub/a.txt:3", got)
		}
		if _, err := model.ParseLink(got); err != nil {
			t.Errorf("ParseLink(%q) = %v", got, err)
		}
	})

	t.Run("from the top level", func(t *testing.T) {
		t.Parallel()
		code, out, errb := runLinkCLI(t, dir, "sub/a.txt:3")
		if code != 0 {
			t.Fatalf("exit = %d (stderr %q)", code, errb)
		}
		got := strings.TrimSpace(out)
		if !strings.HasSuffix(got, "/sub/a.txt:3") {
			t.Errorf("stdout = %q, want a link ending /sub/a.txt:3", got)
		}
	})

	t.Run("escaping the checkout exits 2", func(t *testing.T) {
		t.Parallel()
		code, _, errb := runLinkCLI(t, dir, "../x")
		if code != 2 {
			t.Errorf("exit = %d, want 2; stderr %q", code, errb)
		}
	})

	t.Run("absolute path inside the checkout", func(t *testing.T) {
		t.Parallel()
		abs := filepath.ToSlash(filepath.Join(sub, "a.txt")) + ":3"
		code, out, errb := runLinkCLI(t, dir, abs)
		if code != 0 {
			t.Fatalf("exit = %d (stderr %q)", code, errb)
		}
		got := strings.TrimSpace(out)
		if !strings.HasSuffix(got, "/sub/a.txt:3") {
			t.Errorf("stdout = %q, want a link ending /sub/a.txt:3", got)
		}
	})
}

// A1 (P25): the real binary passes workdir "." to cli.Run, so `gg link
// a.txt:2` used to die in filepath.Rel — an ABSOLUTE top level against a
// RELATIVE join. The rebase makes the workdir absolute itself. No os.Chdir
// here (the suite is parallel): the workdir is expressed relative to the
// process cwd instead, which is the same shape "." has.
func TestLinkAcceptsARelativeWorkdir(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(cwd, dir)
	if err != nil {
		t.Skipf("no relative path from %q to %q: %v", cwd, dir, err)
	}
	if filepath.IsAbs(rel) {
		t.Fatalf("Rel returned an absolute path %q", rel)
	}
	code, out, errb := runLinkCLI(t, rel, "README.md:2")
	if code != 0 {
		t.Fatalf("exit = %d (stderr %q)", code, errb)
	}
	got := strings.TrimSpace(out)
	if !strings.HasSuffix(got, "/README.md:2") {
		t.Errorf("stdout = %q, want a link ending /README.md:2", got)
	}
	if _, err := model.ParseLink(got); err != nil {
		t.Errorf("ParseLink(%q) = %v", got, err)
	}
}

// A6: LinkPathOK must run on the REBASED, top-level-relative path, never on
// the raw argument — a Windows absolute argument ("C:\repo\a.txt") is all
// drive colon, and so is a POSIX checkout that merely lives under a directory
// with a ':' in its name. Both reduce to a clean relative path.
func TestLinkAbsoluteArgumentWithAColonInTheCheckoutPath(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		// A ':' is not a legal Windows path character; the DRIVE colon covers
		// the same code path there and is exercised by the absolute-argument
		// subtest of TestLinkPathIsRebasedToTheCheckoutTop.
		t.Skip("':' cannot appear in a Windows directory name")
	}
	top := filepath.Join(t.TempDir(), "odd:name", "repo")
	if err := os.MkdirAll(top, 0o755); err != nil {
		t.Fatal(err)
	}
	seedCLIRepoAt(t, top)
	abs := filepath.ToSlash(filepath.Join(top, "README.md")) + ":2"
	code, out, errb := runLinkCLI(t, top, abs)
	if code != 0 {
		t.Fatalf("exit = %d (stderr %q)", code, errb)
	}
	got := strings.TrimSpace(out)
	if !strings.HasSuffix(got, "/README.md:2") {
		t.Errorf("stdout = %q, want a link ending /README.md:2", got)
	}
	// The checkout path itself carries the colon; the link must still reparse.
	if _, err := model.ParseLink(got); err != nil {
		t.Errorf("ParseLink(%q) = %v", got, err)
	}
}

// A6: an '@' (or a '#' that is not a hunk suffix) in the path gets the
// INTENDED message — before, the probe parse ran first and complained about a
// target or a hunk the user never wrote.
func TestLinkPathSeparatorMessages(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	for _, tc := range []struct{ name, arg, want string }{
		{"at in path", "we@ird.go", "path contains @, :, # or ?"},
		{"hash in path", "we#ird.go", "path contains @, :, # or ?"},
		{"hash then junk", "a.go#x", "path contains @, :, # or ?"},
		// '?' became the hint separator (Task 1), so it is as inexpressible
		// as '@' — and MORE dangerous, because ParseLink does not fail on it:
		// it silently eats the tail as a hint and leaves a link to a
		// DIFFERENT, possibly existing, file.
		{"question in path", "a?bookmark=x.txt", "path contains @, :, # or ?"},
		{"question then junk", "a?b.txt", "path contains @, :, # or ?"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			code, _, errb := runLinkCLI(t, dir, tc.arg)
			if code != 2 {
				t.Fatalf("exit = %d, want 2; stderr %q", code, errb)
			}
			if !strings.Contains(errb, tc.want) {
				t.Errorf("stderr = %q, want it to mention %q", errb, tc.want)
			}
		})
	}
	// A real hunk suffix still works (it is part of the grammar `gg link`
	// prints, and the '#' check must not swallow it).
	code, out, errb := runLinkCLI(t, dir, "README.md#2")
	if code != 0 {
		t.Fatalf("hunk suffix: exit = %d (stderr %q)", code, errb)
	}
	if got := strings.TrimSpace(out); !strings.HasSuffix(got, "/README.md#2") {
		t.Errorf("stdout = %q, want a link ending /README.md#2", got)
	}
}

// A '?' in the path argument used to be the ONE separator that did not fail
// loudly: ParseLink eats everything from the first '?' as the hint, so the
// probe handed back the TRUNCATED path and `gg link` printed, with exit 0, a
// link to a different file that happens to exist. Two real files make that
// visible — the refusal is the only correct answer, since the grammar cannot
// hold either name.
func TestLinkRefusesAPathWithAQuestionMark(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	for _, name := range []string{"a", "a?bookmark=x.txt", "a?b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name+"\n"), 0o644); err != nil {
			t.Skipf("this filesystem cannot hold %q: %v", name, err)
		}
	}
	for _, arg := range []string{"a?bookmark=x.txt", "a?b.txt"} {
		arg := arg
		t.Run(arg, func(t *testing.T) {
			t.Parallel()
			code, out, errb := runLinkCLI(t, dir, arg)
			if code != 2 {
				t.Fatalf("exit = %d, want 2; stdout %q stderr %q", code, out, errb)
			}
			if strings.TrimSpace(out) != "" {
				t.Errorf("stdout = %q, want nothing printed", out)
			}
			if !strings.Contains(errb, "path contains @, :, # or ?") {
				t.Errorf("stderr = %q, want the path-separator refusal naming '?'", errb)
			}
		})
	}
}

// A3: a remoteless checkout whose PATH holds an '@' cannot be expressed in
// the local form — the first '@' is the grammar's target separator. `gg link`
// refuses (exit 2) rather than print a link nothing can parse.
func TestLinkRefusesACheckoutPathWithASeparator(t *testing.T) {
	t.Parallel()
	for _, seg := range []string{"user@corp", "backup#1"} {
		seg := seg
		t.Run(seg, func(t *testing.T) {
			t.Parallel()
			top := filepath.Join(t.TempDir(), seg, "repo")
			if err := os.MkdirAll(top, 0o755); err != nil {
				t.Fatal(err)
			}
			seedCLIRepoAt(t, top) // no remote → the local form
			code, out, errb := runLinkCLI(t, top, "README.md:2")
			if code != 2 {
				t.Fatalf("exit = %d, want 2; stdout %q stderr %q", code, out, errb)
			}
			if !strings.Contains(errb, "no gg link") {
				t.Errorf("stderr = %q, want the refusal", errb)
			}
		})
	}
}

// seedCLIRepoAt initialises a real repo with one commit at an EXISTING dir
// (newCLIRepo chooses its own; these tests need a specific path).
func seedCLIRepoAt(t *testing.T, dir string) {
	t.Helper()
	env := append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	run := func(args ...string) {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", dir}, args...)...)
		c.Env = env
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\nthere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "README.md")
	run("commit", "-m", "initial")
}

func TestLinkUsageErrors(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	for _, args := range [][]string{
		{"--cached", "--rev", "HEAD", "README.md"},
		{"we@ird.go"},         // a path holding a link separator
		{"README.md", "more"}, // two positionals
		{"resolve"},           // resolve with no link
		{"resolve", "not-a-link"},
	} {
		args := args
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			t.Parallel()
			if code, _, errb := runLinkCLI(t, dir, args...); code != 2 {
				t.Errorf("exit = %d, want 2; stderr %q", code, errb)
			}
		})
	}
}

func TestLinkResolvePrintsTheChosenCheckout(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	code, out, errb := runLinkCLI(t, dir, "README.md:4")
	if code != 0 {
		t.Fatalf("link: exit %d (%q)", code, errb)
	}
	link := strings.TrimSpace(out)

	code, out, errb = runLinkCLI(t, dir, "resolve", link)
	if code != 0 {
		t.Fatalf("resolve: exit %d (stderr %q)", code, errb)
	}
	if !strings.Contains(out, dir) || !strings.Contains(out, "README.md") {
		t.Errorf("stdout = %q, want the checkout and the path", out)
	}
}

func TestLinkResolveJSON(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	_, out, _ := runLinkCLI(t, dir, "README.md:4")
	link := strings.TrimSpace(out)

	code, out, errb := runLinkCLI(t, dir, "resolve", "--json", link)
	if code != 0 {
		t.Fatalf("exit = %d (stderr %q)", code, errb)
	}
	var got wireResolvedLink
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("json: %v (%q)", err, out)
	}
	if got.Path != "README.md" || got.Line != 4 || got.Side != "new" || got.State != "unstaged" {
		t.Errorf("payload = %+v", got)
	}
	if got.Checkout == "" || got.Worktree == "" {
		t.Errorf("payload = %+v, want the checkout and worktree filled", got)
	}
}

func TestLinkResolveUnknownRepoExitsOne(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	code, _, errb := runLinkCLI(t, dir, "resolve", "gg://nobody-has-this/a.go")
	if code != 1 {
		t.Errorf("exit = %d, want 1; stderr %q", code, errb)
	}
	if !strings.Contains(errb, "open it once in gg") {
		t.Errorf("stderr = %q, want the fix-it hint", errb)
	}
}

// A4: ResolveLink wraps model.ErrLink for a link the parser let through but
// the resolver cannot honour (here: a path escaping the checkout). That is a
// MALFORMED link — exit 2, the same as everywhere else — while a link that is
// merely unresolvable here stays exit 1.
func TestLinkResolveMalformedLinkExitsTwo(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	if out, err := exec.Command("git", "-C", dir, "remote", "add", "origin", "git@github.com:homeend/gigagit.git").CombinedOutput(); err != nil {
		t.Fatalf("remote add: %v\n%s", err, out)
	}
	code, _, errb := runLinkCLI(t, dir, "resolve", "gg://gigagit/../secret")
	if code != 2 {
		t.Fatalf("exit = %d, want 2; stderr %q", code, errb)
	}
	if !strings.Contains(errb, "escapes the checkout") {
		t.Errorf("stderr = %q, want the escape refusal", errb)
	}
}

// gitOut runs git in dir and returns stdout, failing the test on error.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(out)
}

// openCLI opens a service for a repo dir.
func openCLI(dir string) *domain.Service { return domain.Open(dir) }

// TestLinkRefAndPairFlags: --ref emits a branch-tip link (a POINT — the whole
// tree there, and it MOVES, which is why the NAME is kept), --pair emits a
// change-set link (BOUNDED — what it changed, with both halves resolved to
// full shas so the link is portable and self-describing).
func TestLinkRefAndPairFlags(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	c1 := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "c2")
	c2 := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))

	t.Run("ref keeps the name", func(t *testing.T) {
		t.Parallel()
		code, out, errb := runLinkCLI(t, dir, "--ref", "main")
		if code != 0 {
			t.Fatalf("exit = %d (stderr %q)", code, errb)
		}
		got := strings.TrimSpace(out)
		if !strings.HasSuffix(got, "@ref:main") {
			t.Errorf("stdout = %q, want a link ending @ref:main", got)
		}
		l, err := model.ParseLink(got)
		if err != nil {
			t.Fatalf("ParseLink(%q) = %v", got, err)
		}
		if l.Target.Ref != "main" || l.Target.State != model.StateCommitted || l.Target.Commit != "" {
			t.Errorf("target = %+v, want Ref=main, StateCommitted, no commit", l.Target)
		}
	})

	t.Run("ref with a path", func(t *testing.T) {
		t.Parallel()
		code, out, errb := runLinkCLI(t, dir, "--ref", "main", "README.md")
		if code != 0 {
			t.Fatalf("exit = %d (stderr %q)", code, errb)
		}
		got := strings.TrimSpace(out)
		if !strings.HasSuffix(got, "/README.md@ref:main") {
			t.Errorf("stdout = %q, want .../README.md@ref:main", got)
		}
	})

	t.Run("pair resolves both halves to full shas", func(t *testing.T) {
		t.Parallel()
		// Abbreviated input on one side, a name on the other: both must come
		// out as the FULL sha (`gg link` is a producer).
		code, out, errb := runLinkCLI(t, dir, "--pair", c1[:8]+"..main")
		if code != 0 {
			t.Fatalf("exit = %d (stderr %q)", code, errb)
		}
		got := strings.TrimSpace(out)
		if !strings.HasSuffix(got, "@"+c1+".."+c2) {
			t.Errorf("stdout = %q, want a link ending @%s..%s", got, c1, c2)
		}
		l, err := model.ParseLink(got)
		if err != nil {
			t.Fatalf("ParseLink(%q) = %v", got, err)
		}
		if l.Target.Pair == nil || l.Target.Pair.A != c1 || l.Target.Pair.B != c2 {
			t.Errorf("target = %+v, want Pair{%s, %s}", l.Target, c1, c2)
		}
	})

	// Each refusal asserts its EXACT exit code, not merely non-zero. The split
	// is the shipped convention and it is a deliberate ruling here: an argument
	// the GRAMMAR cannot carry is bad input (ErrLink ⇒ 2), while a name git
	// does not have is a lookup that failed (⇒ 1, the same code `--rev` has
	// always used for an unknown revision). A table that only checked
	// "non-zero" would pin neither.
	t.Run("refusals", func(t *testing.T) {
		t.Parallel()
		for _, tc := range []struct {
			name, want string
			exit       int
			args       []string
		}{
			// Malformed ARGUMENTS — usage, exit 2.
			{"three dots go to --preview", "use --preview", 2, []string{"--pair", c1 + "..." + c2}},
			{"no dots at all", "--pair takes <a>..<b>", 2, []string{"--pair", c1}},
			{"a missing half", "needs both halves", 2, []string{"--pair", c1 + ".."}},
			{"an empty first half", "needs both halves", 2, []string{"--pair", ".." + c2}},
			{"an inexpressible ref name", "cannot be expressed in a gg link", 2, []string{"--ref", "we:ird"}},
			// Well-formed, but this repository does not have the name — a
			// failed lookup, exit 1, exactly as `--rev no-such-rev` reports.
			{"an unknown half", "unknown revision", 1, []string{"--pair", c1 + "..no-such-rev"}},
			{"an unknown second half", "unknown revision", 1, []string{"--pair", "no-such-rev.." + c2}},
			{"an unknown ref", "unknown revision", 1, []string{"--ref", "no-such-branch"}},
			{"an unknown rev (the shipped precedent)", "unknown revision", 1, []string{"--rev", "no-such-rev"}},
		} {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				code, out, errb := runLinkCLI(t, dir, tc.args...)
				if code != tc.exit {
					t.Fatalf("gg link %v: exit = %d, want %d (stdout %q, stderr %q)", tc.args, code, tc.exit, out, errb)
				}
				if !strings.Contains(errb, tc.want) {
					t.Errorf("gg link %v: stderr = %q, want it to contain %q", tc.args, errb, tc.want)
				}
				if strings.TrimSpace(out) != "" {
					t.Errorf("gg link %v printed a link anyway: %q", tc.args, out)
				}
			})
		}
	})
}

// TestLinkHintFlags: --bookmark / --shelf attach the ?<kind>=<id> landing
// hint. The hint says WHICH SURFACE the link was copied from; it never changes
// what the link addresses, so it composes with any target.
func TestLinkHintFlags(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	head := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))

	for _, tc := range []struct {
		name string
		args []string
		want string // suffix of the printed link
	}{
		{"bookmark on a file link", []string{"--bookmark", "b1", "README.md"}, "/README.md?bookmark=b1"},
		{"shelf on a file link", []string{"--shelf", "commit-123-abcdef01", "README.md"}, "/README.md?shelf=commit-123-abcdef01"},
		{"bookmark on a ref link", []string{"--ref", "main", "--bookmark", "b1"}, "@ref:main?bookmark=b1"},
		{"bookmark on a commit link", []string{"--rev", head, "--bookmark", "b1"}, "@" + head + "?bookmark=b1"},
		{"bookmark with a line", []string{"--bookmark", "b1", "README.md:2"}, "/README.md:2?bookmark=b1"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			code, out, errb := runLinkCLI(t, dir, tc.args...)
			if code != 0 {
				t.Fatalf("exit = %d (stderr %q)", code, errb)
			}
			got := strings.TrimSpace(out)
			if !strings.HasSuffix(got, tc.want) {
				t.Errorf("stdout = %q, want a link ending %q", got, tc.want)
			}
			l, err := model.ParseLink(got)
			if err != nil {
				t.Fatalf("ParseLink(%q) = %v", got, err)
			}
			if l.Hint.Kind == "" || l.Hint.ID == "" {
				t.Errorf("hint = %+v, want both halves set", l.Hint)
			}
		})
	}

	t.Run("two hints is a usage error", func(t *testing.T) {
		t.Parallel()
		code, out, errb := runLinkCLI(t, dir, "--bookmark", "b1", "--shelf", "s1")
		if code != 2 {
			t.Fatalf("exit = %d, want 2 (stdout %q)", code, out)
		}
		if !strings.Contains(errb, "--bookmark and --shelf are mutually exclusive") {
			t.Errorf("stderr = %q, want the exclusion message", errb)
		}
	})

	// An id holding a grammar separator cannot round-trip: parseLinkHint
	// refuses it, so the PRODUCER must refuse it too rather than print a link
	// its own parser rejects.
	t.Run("an id with a separator is refused", func(t *testing.T) {
		t.Parallel()
		code, out, errb := runLinkCLI(t, dir, "--bookmark", "b?1", "README.md")
		if code != 2 {
			t.Fatalf("exit = %d, want 2 (stdout %q, stderr %q)", code, out, errb)
		}
		if !strings.Contains(errb, "not a readable gg link") {
			t.Errorf("stderr = %q, want the round-trip refusal", errb)
		}
		if strings.TrimSpace(out) != "" {
			t.Errorf("printed a link anyway: %q", out)
		}
	})
}

// TestLinkTargetFlagsAreExclusive: --cached, --rev, --preview, --ref and
// --pair all name the TARGET, so at most one may be set — the rule --cached
// and --rev already had, now counted rather than checked pairwise.
func TestLinkTargetFlagsAreExclusive(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	head := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"cached+rev", []string{"--cached", "--rev", head}, "name the target; use one"},
		{"cached+ref", []string{"--cached", "--ref", "main"}, "name the target; use one"},
		{"rev+ref", []string{"--rev", head, "--ref", "main"}, "name the target; use one"},
		{"ref+pair", []string{"--ref", "main", "--pair", head + ".." + head}, "name the target; use one"},
		{"cached+pair", []string{"--cached", "--pair", head + ".." + head}, "name the target; use one"},
		// --preview keeps its own shared message, the one `gg diff` prints.
		{"preview+ref", []string{"--preview", "a...b", "--ref", "main"}, "one target only"},
		{"preview+pair", []string{"--preview", "a...b", "--pair", head + ".." + head}, "one target only"},
		{"preview+cached", []string{"--preview", "a...b", "--cached"}, "one target only"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			code, out, errb := runLinkCLI(t, dir, tc.args...)
			if code != 2 {
				t.Fatalf("gg link %v: exit = %d, want 2 (stdout %q, stderr %q)", tc.args, code, out, errb)
			}
			if !strings.Contains(errb, tc.want) {
				t.Errorf("gg link %v: stderr = %q, want it to contain %q", tc.args, errb, tc.want)
			}
			if strings.TrimSpace(out) != "" {
				t.Errorf("gg link %v printed a link anyway: %q", tc.args, out)
			}
		})
	}
}

// `gg link resolve` (and every other verb that goes through resolveLinkArg —
// gg diff, gg show, gg note *, gg open, gg session navigate) used to refuse a
// branch tip or a change-set outright: model.FileAddress has no field for
// either shape, and domain.ResolveLink deliberately refused them (plan 1b).
// That refusal was lifted for navigation's sake (a later plan's Task 2):
// ResolveLink now resolves both, carrying the tip (or the newer half of the
// pair) as Resolved.Commit/Addr.Commit, alongside the NAME(s) in the new
// Resolved.Ref/Resolved.Pair fields the CLI's plain-text and JSON output do
// not read yet. So `gg link resolve` on one of these now succeeds — it just
// prints the same "commit <sha>" line a plain commit link would, without
// naming the shape. Teaching this verb's output to say "ref main" or "pair
// a..b" is not this task's job (it never reads Resolved.Ref/Resolved.Pair);
// this test only pins that the link no longer refuses and does not silently
// mis-blame a missing sha.
func TestLinkResolveNowResolvesRefAndPair(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	head := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))
	for _, tc := range []struct{ name, link string }{
		{"a branch tip", mustLink(t, dir, "--ref", "main")},
		{"a change-set", mustLink(t, dir, "--pair", head+".."+head)},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			code, out, errb := runLinkCLI(t, dir, "resolve", tc.link)
			if code != 0 {
				t.Fatalf("link resolve %s: exit = %d, want 0 (stdout %q stderr %q)", tc.link, code, out, errb)
			}
			if errb != "" {
				t.Errorf("stderr = %q, want empty", errb)
			}
			if !strings.Contains(out, head) {
				t.Errorf("stdout = %q, want it to name the resolved commit %q", out, head)
			}
		})
	}
}

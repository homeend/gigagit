package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
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

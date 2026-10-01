package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/clock"
	"github.com/homeend/gigagit/internal/forge/forgetest"
)

// TestMain pins git's environment process-wide so both the builder's git
// calls and gg's own git invocations (via cli.Run) are isolated from the
// machine: no system/user gitconfig, fixed identity, file-protocol clones
// allowed, and gg's global config redirected away from the real user's.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "gg-e2e-env")
	if err != nil {
		panic(err)
	}
	gcfg := filepath.Join(dir, "gitconfig")
	cfg := "[init]\n\tdefaultBranch = main\n" +
		"[user]\n\tname = gg-e2e\n\temail = e2e@gg\n" +
		"[protocol \"file\"]\n\tallow = always\n"
	if err := os.WriteFile(gcfg, []byte(cfg), 0o644); err != nil {
		panic(err)
	}
	os.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	// A suite run inside a gg console must not reach that console's TUI.
	os.Unsetenv("GG_MCP_URL")
	os.Unsetenv("GG_SESSION_TOKEN")
	os.Unsetenv("GG_INBOX") // every successful mutating verb would nudge it
	os.Setenv("GIT_CONFIG_GLOBAL", gcfg)
	os.Setenv("GIT_AUTHOR_NAME", "gg-e2e")
	os.Setenv("GIT_AUTHOR_EMAIL", "e2e@gg")
	os.Setenv("GIT_COMMITTER_NAME", "gg-e2e")
	os.Setenv("GIT_COMMITTER_EMAIL", "e2e@gg")
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))     // gg global config isolation
	os.Setenv("XDG_STATE_HOME", filepath.Join(dir, "xdgstate")) // gg shelf store isolation
	os.Unsetenv("GIT_DIR")                                      // ambient GIT_DIR would redirect every git call
	// A fake gh for every scenario: it answers only from <repo>/.git/fakegh, so
	// a scenario that seeds no fixtures sees "no usable forge" — and the real
	// gh on a developer's box can never leak network calls into the suite.
	fakeGH, err := forgetest.BuildFakeGHMain()
	if err != nil {
		panic(err)
	}
	os.Setenv(forgetest.EnvBin, fakeGH)
	os.Unsetenv(forgetest.EnvFixtures)
	// The fake agent (e2e/internal/ggfake), built once; scenarios name it as
	// {{ggfake}} in a capture tool's command.
	ggFakeBin = filepath.Join(dir, "ggfake"+exeSuffix())
	if out, err := exec.Command("go", "build", "-o", ggFakeBin, "./internal/ggfake").CombinedOutput(); err != nil {
		panic(fmt.Sprintf("build ggfake: %v\n%s", err, out))
	}
	review, err := filepath.Abs(filepath.Join("fixtures", "review.json"))
	if err != nil {
		panic(err)
	}
	os.Setenv("GGFAKE_REVIEW", review)
	// On PATH, so {{ggfake}} expands to the bare name: the expansion lands in
	// committed files (.gg.toml), and a temp-dir path would change the
	// commit's SHA on every run.
	os.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	// Capture commands run through $SHELL (engine/capture_runner.go): a
	// developer's zsh or fish must not decide how a scenario's command parses.
	if runtime.GOOS != "windows" {
		os.Setenv("SHELL", "/bin/sh")
	}
	// One frozen "now" for every stored or drawn time, and one date for every
	// commit gg itself makes: scenarios run in parallel in one process, so
	// neither can be per scenario (the TUI golden-screens spec, §4). The
	// builder's own git calls pass their dates per command and are unaffected.
	clock.Freeze(frozenNow)
	// Dates render in local time; the machine's zone must not reach a golden.
	time.Local = time.UTC
	os.Setenv("GIT_AUTHOR_DATE", frozenNow.Format(time.RFC3339))
	os.Setenv("GIT_COMMITTER_DATE", frozenNow.Format(time.RFC3339))
	code := func() int {
		defer os.RemoveAll(dir)
		return m.Run()
	}()
	os.Exit(code)
}

// ggFakeBin is the fake agent TestMain builds (e2e/internal/ggfake).
var ggFakeBin string

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

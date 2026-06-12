package e2e

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ggTOML pins gg's per-repo config so template-driven worktree paths and
// branch names are deterministic (the built-in default contains
// <random-alpha:4>). Injected before steps run; the first commit picks it up.
const ggTOML = "[worktree]\npath_template = \"../wt/<branch>\"\ndefault_branch_template = \"wt-<parent-branch>\"\n"

// dateBase is the frozen clock: each builder git call advances it by 1s.
var dateBase = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// Sandbox is one scenario's isolated environment.
type Sandbox struct {
	Root      string            // temp root; all relative dirs resolve against it
	LocalDir  string            // the repo gg commands run in (Root/local)
	OriginDir string            // Root/origin, or "" without [input.origin]
	OriginURL string            // clone/push URL (http://… or OriginDir)
	InputSums map[string]string // relpath → sha256 at end of input phase
	ticks     int
}

// git runs one git command for repo building, with deterministic dates.
func (b *Sandbox) git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	b.ticks++
	date := dateBase.Add(time.Duration(b.ticks) * time.Second).Format(time.RFC3339)
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	// GIT_AUTHOR_DATE/GIT_COMMITTER_DATE override any inherited value;
	// identity (NAME/EMAIL) and config isolation come from TestMain's Setenv.
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build: git %v in %s: %v\n%s", args, dir, err, out)
	}
	return string(out)
}

// dir resolves a sandbox-root-relative path ("" = the local repo).
func (b *Sandbox) dir(rel string) string {
	if rel == "" {
		return b.LocalDir
	}
	return filepath.Join(b.Root, rel)
}

// buildSandbox constructs the scenario's input state. Origin topology is
// added in a later task; until then scenarios with [input.origin] fail.
func buildSandbox(t *testing.T, sc *Scenario) *Sandbox {
	t.Helper()
	sb := &Sandbox{Root: t.TempDir()}
	sb.LocalDir = filepath.Join(sb.Root, "local")
	if sc.Input.Origin != nil {
		buildOrigin(t, sb, sc) // implemented with the remote-topology task
	} else {
		sb.git(t, sb.Root, "init", "-b", "main", "local")
		// .gg.toml is intentionally left uncommitted here; it is picked up by
		// the scenario's first `commit` step (`git add -A`), which validation
		// guarantees exists.
		writeGGToml(t, sb.LocalDir)
	}
	sb.runSteps(t, sc.Input.Steps, sb.LocalDir)
	if sc.Input.Origin != nil {
		sb.runSteps(t, sc.Input.Origin.After, sb.OriginDir)
	}
	sb.snapshotInput(t)
	return sb
}

func writeGGToml(t *testing.T, repoDir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repoDir, ".gg.toml"), []byte(ggTOML), 0o644); err != nil {
		t.Fatal(err)
	}
}

// runSteps executes build steps; defaultDir is the acting repo unless a step
// sets cwd (sandbox-root-relative).
func (b *Sandbox) runSteps(t *testing.T, steps []Step, defaultDir string) {
	t.Helper()
	for _, st := range steps {
		k, err := st.kind()
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		dir := defaultDir
		if st.Cwd != "" {
			dir = b.dir(st.Cwd)
		}
		switch k {
		case "write":
			p := filepath.Join(dir, st.Write)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(st.Content), 0o644); err != nil {
				t.Fatal(err)
			}
		case "rm":
			if err := os.Remove(filepath.Join(dir, st.Rm)); err != nil {
				t.Fatal(err)
			}
		case "commit":
			b.git(t, dir, "add", "-A")
			b.git(t, dir, "commit", "-m", st.Commit)
		case "branch":
			b.git(t, dir, "branch", st.Branch)
		case "switch":
			b.git(t, dir, "switch", st.Switch)
		case "stash":
			b.git(t, dir, "stash", "push", "-u", "-m", st.Stash)
		case "worktree":
			b.git(t, dir, "worktree", "add", b.dir(st.Worktree), st.Branch)
		}
	}
}

// snapshotInput records each working-tree file's checksum at the end of the
// input phase — the reference for { unchanged = true } expectations.
// v1 limitation: only the LOCAL repo's working tree is snapshotted; linked-
// worktree scopes have no `unchanged` support.
func (b *Sandbox) snapshotInput(t *testing.T) {
	t.Helper()
	b.InputSums = map[string]string{}
	err := filepath.WalkDir(b.LocalDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(b.LocalDir, path)
		if err != nil {
			return fmt.Errorf("rel %s -> %s: %w", b.LocalDir, path, err)
		}
		if strings.HasPrefix(rel, "..") {
			return fmt.Errorf("snapshot: path %s escaped sandbox root %s", path, b.LocalDir)
		}
		sum, err := fileSHA256(path)
		if err != nil {
			return err
		}
		b.InputSums[filepath.ToSlash(rel)] = sum
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
}

func fileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// buildOrigin creates the upstream repo, serves it (http default), and
// clones it as the local repo. Sequence: origin.steps → serve → clone →
// (caller then runs input.steps in the clone and origin.after upstream).
func buildOrigin(t *testing.T, sb *Sandbox, sc *Scenario) {
	t.Helper()
	o := sc.Input.Origin
	sb.OriginDir = filepath.Join(sb.Root, "origin")
	sb.git(t, sb.Root, "init", "-b", "main", "origin")
	writeGGToml(t, sb.OriginDir) // first origin commit carries it into clones
	sb.runSteps(t, o.Steps, sb.OriginDir)
	// Anonymous push + pushes to the checked-out branch (assertions read refs,
	// never origin's working tree, so a stale tree is fine).
	sb.git(t, sb.OriginDir, "config", "http.receivepack", "true")
	sb.git(t, sb.OriginDir, "config", "receive.denyCurrentBranch", "ignore")

	if o.Transport == "path" {
		sb.OriginURL = sb.OriginDir
	} else {
		srv := startGitServer(t, sb.Root)
		sb.OriginURL = srv.URL + "/origin"
	}
	sb.git(t, sb.Root, "clone", sb.OriginURL, "local")
}

package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/rebaseplan"
)

// runRebaseSeq is the GIT_SEQUENCE_EDITOR hook: it reads the gg plan and
// overwrites git's rebase todo file to match. args = [planPath, todoPath].
func runRebaseSeq(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("__rebase-seq: want <plan> <todofile>, got %v", args)
	}
	planPath, todoPath := args[0], args[1]
	raw, err := os.ReadFile(planPath)
	if err != nil {
		return err
	}
	p, err := rebaseplan.Unmarshal(raw)
	if err != nil {
		return err
	}
	ggBin, err := os.Executable()
	if err != nil {
		return err
	}
	todo, err := p.RewriteTodo(ggBin, planPath, runtime.GOOS)
	if err != nil {
		return err
	}
	return os.WriteFile(todoPath, []byte(todo), 0o644)
}

// runRebaseMessage is the rebase `exec` hook for reword/squash: it amends HEAD's
// message to the plan's composed message for the target at the given index.
// args = [planPath, index]. It runs in the rebase working directory (git's cwd
// for exec steps), so the bare `git commit` targets the right repo.
func runRebaseMessage(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("__rebase-message: want <plan> <index>, got %v", args)
	}
	raw, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	p, err := rebaseplan.Unmarshal(raw)
	if err != nil {
		return err
	}
	idx, err := strconv.Atoi(args[1])
	if err != nil {
		return fmt.Errorf("__rebase-message: bad index %q: %w", args[1], err)
	}
	if idx < 0 || idx >= len(p.Entries) {
		return fmt.Errorf("__rebase-message: index %d out of range", idx)
	}
	cmd := exec.Command("git", "commit", "--amend", "-F", "-")
	cmd.Stdin = strings.NewReader(p.Message(idx))
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

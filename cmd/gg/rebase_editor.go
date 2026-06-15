package main

import (
	"fmt"
	"os"

	"github.com/gigagit/gg/internal/rebaseplan"
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
	todo, err := p.RewriteTodo(ggBin, planPath)
	if err != nil {
		return err
	}
	return os.WriteFile(todoPath, []byte(todo), 0o644)
}

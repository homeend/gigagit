package cli

import (
	"bufio"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/homeend/gigagit/internal/agentinit"
	"github.com/homeend/gigagit/internal/promptstate"
)

// firstRunPromptID marks the first-run skills question as answered in the
// machine-local prompts.toml, so it is asked once per user.
const firstRunPromptID = "first_run_skills"

// firstRunTTY is the interactivity probe (a test seam): the question needs a
// human at a terminal.
var firstRunTTY = stdinIsTerminal

// FirstRunInit asks a new user, once, whether to install gg's skills for the
// AI agents it detects; yes runs the same dialog as `gg init`. It never stops
// the launch that called it: the TUI starts whatever the answer. A user is
// new when none of THEIR targets (home-scoped or remembered --to ones) has
// the skills yet — skills a repository commits say nothing about the user;
// one who already has them is marked as asked without a word. With no agent detected nothing is
// marked, so the question waits until there is something to install. No
// answer (EOF) is not remembered as one.
func FirstRunInit(workdir string, stdin io.Reader, stdout, stderr io.Writer) {
	if RepoStatePath == "" || !firstRunTTY() {
		return
	}
	ps := promptstate.NewFileStore(filepath.Join(filepath.Dir(RepoStatePath), "prompts.toml"))
	if ps.SuppressedPrompts()[firstRunPromptID] {
		return
	}
	dets := agentinit.Detect(workdir, InitHomeDir)
	if customs, err := agentinit.LoadCustomTargets(initTargetsPath()); err == nil {
		dets = append(dets, agentinit.CustomDetections(customs)...)
	}
	if len(dets) == 0 {
		return
	}
	if userSetUp(dets) {
		_ = ps.SuppressPrompt(firstRunPromptID)
		return
	}

	fmt.Fprintln(stderr, "gg can install its skills for your AI agents, so they know how to use gg.")
	fmt.Fprint(stderr, "Install gg's skills now? [Y/n]: ")
	r := bufio.NewReader(stdin)
	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		fmt.Fprintln(stderr)
		return
	}
	_ = ps.SuppressPrompt(firstRunPromptID)
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "", "y", "yes":
		cmdInit(workdir, nil, r, stdout, stderr)
	default:
		fmt.Fprintln(stderr, "Skipped — run gg init any time to install them.")
	}
}

// userSetUp reports whether a target the user owns — a home-scoped agent or a
// remembered custom (--to) path — already has the skills.
func userSetUp(dets []agentinit.Detection) bool {
	for _, d := range checkedDefaults(dets) {
		if d.Agent.ID == "custom" || strings.HasPrefix(d.Agent.Detect, "~") {
			return true
		}
	}
	return false
}

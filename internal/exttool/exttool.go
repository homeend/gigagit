// Package exttool is the hardcoded catalog of external tools/AI agents gg can
// run per task category (conflict resolution now; commit-message and review in
// later stages), plus their detection. Supporting a new tool is a code change
// (one Builtins entry), never a runtime definition — the agentinit philosophy.
// The catalog's command TEMPLATES never execute directly: the Settings wizard
// materializes them as editable [[tools.command] ] blocks in the gg config, and
// only config content runs.
package exttool

import (
	"os"
	"strings"
)

// Category is a task category a command belongs to.
type Category string

const (
	CatConflict      Category = "conflict"
	CatCommitMessage Category = "commit_message"
	CatReview        Category = "review"
)

// Mode is how a command runs: terminal = suspend the TUI and hand over the
// real terminal (interactive agents, GUI mergetools); capture = run headless
// and capture stdout (stage 2+).
type Mode string

const (
	ModeTerminal Mode = "terminal"
	ModeCapture  Mode = "capture"
)

// CommandTemplate is one catalog default command. Command contains <bin>
// (replaced at generation time by GenerateCommand) plus runtime tokens
// resolved by template.ResolveCommand.
type CommandTemplate struct {
	Category Category
	Name     string // menu label; unique per category across the catalog
	Mode     Mode
	PerFile  bool   // true = runs once per conflicted file (mergetools)
	WhenOp   string // "" = any paused op; else merge|rebase|cherry-pick|revert
	Command  string
}

// Tool is one catalog entry. Bins are candidate binary names probed via
// LookPath; ExtraProbes are absolute paths probed via Stat for installs that
// are typically off PATH (Meld on Windows).
type Tool struct {
	ID          string
	Label       string
	Bins        []string
	ExtraProbes []string
	Commands    []CommandTemplate
}

const claudeConflictCommand = `<bin> --permission-mode acceptEdits \
  --allowedTools "Read" "Edit" "Bash(git status)" "Bash(git diff *)" "Bash(git log *)" "Bash(git add *)" \
  --disallowedTools "Bash(git commit *)" "Bash(git merge *)" "Bash(git rebase *)" "Bash(git push *)" \
  "A git <op> (bringing <source> into <target>) is paused with conflicts in: <conflicted-files>.
   Inspect both sides' history to understand intent, resolve each conflict by editing the files,
   then run git add on each resolved file. Do NOT run git commit or any --continue command --
   stop when everything is staged and summarize what you chose and why."`

// Builtins is the hardcoded catalog. Stage 1 ships conflict templates only;
// commit_message/review defaults land with their stages (recorded in the spec).
func Builtins() []Tool {
	return []Tool{
		{
			ID: "claude", Label: "Claude Code", Bins: []string{"claude"},
			Commands: []CommandTemplate{
				{Category: CatConflict, Name: "Claude", Mode: ModeTerminal, Command: claudeConflictCommand},
			},
		},
		{
			ID: "junie", Label: "JetBrains Junie", Bins: []string{"junie"},
			Commands: []CommandTemplate{
				// Empirical note (spec): whether --merge/--rebase adopt an
				// already-paused op is verified live before merge; the fallback
				// is a --prompt task (see the spec's Junie entry).
				{Category: CatConflict, Name: "Junie (merge)", Mode: ModeTerminal, WhenOp: "merge", Command: "<bin> --merge <source>"},
				{Category: CatConflict, Name: "Junie (rebase)", Mode: ModeTerminal, WhenOp: "rebase", Command: "<bin> --rebase <source>"},
			},
		},
		{
			ID: "meld", Label: "Meld", Bins: []string{"meld"},
			ExtraProbes: []string{`C:\Program Files\Meld\Meld.exe`},
			Commands: []CommandTemplate{
				{Category: CatConflict, Name: "Meld", Mode: ModeTerminal, PerFile: true,
					Command: "<bin> --auto-merge --output=<merged> <local> <base> <remote>"},
			},
		},
	}
}

// Detection is one detected tool. Bin is argv-ready: the bare binary name for
// a PATH hit (portable config), the absolute path for an ExtraProbes hit.
type Detection struct {
	Tool Tool
	Bin  string
}

// Detect probes the catalog with injected lookups (exec.LookPath / os.Stat in
// production — the clipboard nativeArgv seam pattern) so tests never touch the
// developer's machine. First Bins hit wins; ExtraProbes are consulted only
// when no Bins name resolves.
func Detect(look func(string) (string, error), stat func(string) (os.FileInfo, error)) []Detection {
	var out []Detection
	for _, tl := range Builtins() {
		bin := ""
		for _, b := range tl.Bins {
			if _, err := look(b); err == nil {
				bin = b
				break
			}
		}
		if bin == "" {
			for _, p := range tl.ExtraProbes {
				if _, err := stat(p); err == nil {
					bin = p
					break
				}
			}
		}
		if bin != "" {
			out = append(out, Detection{Tool: tl, Bin: bin})
		}
	}
	return out
}

// GenerateCommand materializes a template for a detected binary: <bin> is
// replaced with bin, double-quoted when it contains whitespace (a Windows
// install path). Runtime tokens pass through untouched.
func GenerateCommand(tmpl CommandTemplate, bin string) string {
	if strings.ContainsAny(bin, " \t") {
		bin = `"` + bin + `"`
	}
	return strings.ReplaceAll(tmpl.Command, "<bin>", bin)
}

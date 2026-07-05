// Package exttool is the hardcoded catalog of external tools/AI agents gg can
// run per task category (conflict resolution now; commit-message and review in
// later stages), plus their detection. Supporting a new tool is a code change
// (one Builtins entry), never a runtime definition — the agentinit philosophy.
// The catalog's command TEMPLATES never execute directly: the Settings wizard
// materializes them as editable [[tools.command]] blocks in the gg config, and
// only config content runs.
package exttool

import (
	"os"
	"regexp"
	"runtime"
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
// and may contain <env:NAME> (both replaced at generation time by
// GenerateCommand/GenerateCommandFor) plus runtime tokens resolved by
// template.ResolveCommand. Defaults use only <bin>/<env:...>/path/enum
// tokens for dynamic content — never a raw prose token — per the injection
// posture in the design spec.
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

// claudeConflictCommand uses only generation-time tokens (<bin>, <env:...>)
// for its dynamic content — no raw prose token — per the injection-posture
// amendment: the prompt reads the paused op and the context file (op/
// source/target/conflicted paths) from GG_* env vars rather than having gg
// substitute untrusted values into the prompt text itself.
const claudeConflictCommand = `<bin> --permission-mode acceptEdits \
  --allowedTools "Read" "Edit" "Bash(git status)" "Bash(git diff *)" "Bash(git log *)" "Bash(git add *)" \
  --disallowedTools "Bash(git commit *)" "Bash(git merge *)" "Bash(git rebase *)" "Bash(git push *)" \
  "A git <env:GG_OP> operation is paused with conflicts in this repository.
   Read the context file at <env:GG_CONTEXT_FILE> for the operation's parties and the conflicted paths.
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
				{Category: CatConflict, Name: "Junie (merge)", Mode: ModeTerminal, WhenOp: "merge", Command: "<bin> --merge <env:GG_SOURCE>"},
				{Category: CatConflict, Name: "Junie (rebase)", Mode: ModeTerminal, WhenOp: "rebase", Command: "<bin> --rebase <env:GG_SOURCE>"},
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

// envTokRe matches a generation-time <env:NAME> token in a catalog template.
// NAME follows shell env-var naming: an uppercase letter/underscore start,
// then uppercase letters/digits/underscores.
var envTokRe = regexp.MustCompile(`<env:([A-Z_][A-Z0-9_]*)>`)

// GenerateCommand materializes a template for a detected binary, for the
// running OS. See GenerateCommandFor for what "materialize" means.
func GenerateCommand(tmpl CommandTemplate, bin string) string {
	return GenerateCommandFor(tmpl, bin, runtime.GOOS)
}

// GenerateCommandFor is GenerateCommand with the OS as a parameter (a test
// seam for exercising both renderings from one process). <bin> is replaced
// with bin, double-quoted when it contains whitespace (a Windows install
// path). Every <env:NAME> generation token becomes a per-OS reference to the
// GG_* environment variable gg always sets when it runs the command —
// `${NAME}` on POSIX, `%NAME%` on Windows — so one catalog template
// generates a correct command on either platform without gg ever
// substituting the underlying value (and needing to escape it) itself. The
// POSIX rendering is deliberately unquoted `${NAME}`, not `"$NAME"`: it
// nests inside a template's own double-quoted prompt strings as one word,
// where `"$NAME"` would alternate quotes and word-split the value when it
// contains spaces (e.g. a TMPDIR with a space); shell variable expansion is
// never re-parsed for command substitution, so the expanded value remains
// data either way. Runtime tokens (<op>, <source>, quartet paths,
// <context-file>, ...) pass through untouched for template.ResolveCommand.
func GenerateCommandFor(tmpl CommandTemplate, bin, goos string) string {
	if strings.ContainsAny(bin, " \t") {
		bin = `"` + bin + `"`
	}
	out := strings.ReplaceAll(tmpl.Command, "<bin>", bin)
	out = envTokRe.ReplaceAllStringFunc(out, func(tok string) string {
		name := envTokRe.FindStringSubmatch(tok)[1]
		if goos == "windows" {
			return "%" + name + "%"
		}
		return "${" + name + "}"
	})
	return out
}

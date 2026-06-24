package cli

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/agentinit"
)

// cmdInit implements `gg init`: detect AI agents, ask which to set up, and
// install/refresh the embedded using-gg skill. Pure file I/O — no git, no
// engine, works outside a repository.
func cmdInit(workdir string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	all := fs.Bool("all", false, "install for every detected agent")
	update := fs.Bool("update", false, "refresh every already-installed target (the checked defaults)")
	agents := fs.String("agents", "", "comma-separated agent IDs to install for")
	list := fs.Bool("list", false, "print detected agents and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	dets := agentinit.Detect(workdir, InitHomeDir)
	if len(dets) == 0 {
		fmt.Fprintln(stdout, "no supported agents detected here")
		return 0
	}

	printList(stdout, dets)
	if *list {
		return 0
	}

	var chosen []agentinit.Detection
	switch {
	case *all:
		chosen = dets
	case *update:
		chosen = checkedDefaults(dets)
	case *agents != "":
		for _, id := range strings.FieldsFunc(*agents, func(r rune) bool { return r == ',' || r == ' ' }) {
			d, ok := byAgentID(dets, id)
			if !ok {
				fmt.Fprintf(stderr, "init: unknown or undetected agent ID %q\n", id)
				return 2
			}
			chosen = append(chosen, d)
		}
	default:
		// Interactive: one selection line. EOF with no input = non-interactive
		// invocation without a selection flag — never guess, never hang.
		fmt.Fprint(stderr, "Apply? [enter]=checked / a=all / numbers (e.g. 1,3) / [q]uit: ")
		line, err := bufio.NewReader(stdin).ReadString('\n')
		if err != nil && line == "" {
			fmt.Fprintln(stderr, "init: no selection (non-interactive?); use --all, --update, or --agents")
			return 1
		}
		sel := strings.TrimSpace(line)
		switch {
		case sel == "q":
			return 0
		case sel == "a":
			chosen = dets
		case sel == "":
			chosen = checkedDefaults(dets)
			if len(chosen) == 0 {
				fmt.Fprintln(stdout, "nothing installed yet — nothing to refresh (pick numbers or use a/--all)")
				return 0
			}
		default:
			for _, tok := range strings.FieldsFunc(sel, func(r rune) bool { return r == ',' || r == ' ' }) {
				n, convErr := strconv.Atoi(tok)
				if convErr != nil || n < 1 || n > len(dets) {
					fmt.Fprintf(stderr, "init: invalid selection %q\n", tok)
					return 2
				}
				chosen = append(chosen, dets[n-1])
			}
		}
	}

	for _, d := range chosen {
		if err := agentinit.Install(d); err != nil {
			fmt.Fprintf(stderr, "init: %s: %v\n", d.Agent.Label, err)
			return 1
		}
		if d.Status == agentinit.StatusNew {
			fmt.Fprintf(stdout, "✓ installed %s → %s\n", d.Agent.Label, d.Target)
		} else {
			fmt.Fprintf(stdout, "✓ refreshed %s → %s\n", d.Agent.Label, d.Target)
		}
	}
	return 0
}

// printList renders the numbered checkbox listing.
func printList(w io.Writer, dets []agentinit.Detection) {
	fmt.Fprintln(w, "Detected agents:")
	for i, d := range dets {
		box := "[ ]"
		if d.Status.Checked() {
			box = "[x]"
		}
		fmt.Fprintf(w, "  %d. %s %-26s %s  %s\n", i+1, box, d.Agent.Label, d.Target, d.Status)
	}
}

// checkedDefaults returns the targets that already have the skill installed.
func checkedDefaults(dets []agentinit.Detection) []agentinit.Detection {
	var out []agentinit.Detection
	for _, d := range dets {
		if d.Status.Checked() {
			out = append(out, d)
		}
	}
	return out
}

func byAgentID(dets []agentinit.Detection, id string) (agentinit.Detection, bool) {
	for _, d := range dets {
		if d.Agent.ID == id {
			return d, true
		}
	}
	return agentinit.Detection{}, false
}

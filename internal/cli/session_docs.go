package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

const docsUsage = "usage: gg session docs path\n" +
	"       gg session docs ls [--json]"

// sessionDocs is `gg session docs …`: the worktree's session-docs
// directory itself (spec 2026-10-18-gg-session-docs §6.2) — path for an
// agent that writes the files directly, ls for everything in it.
func sessionDocs(dir sessDir, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, docsUsage)
		return 2
	}
	switch args[0] {
	case "path":
		if len(args) != 1 {
			fmt.Fprintln(stderr, docsUsage)
			return 2
		}
		if dir.inbox == "" {
			fmt.Fprintln(stderr, "session docs path: no state directory for the session docs")
			return 1
		}
		fmt.Fprintln(stdout, steer.DocsDir(dir.inbox)) // creates nothing
		return 0
	case "ls":
		return sessionDocsLs(dir, svc, args[1:], stdout, stderr)
	}
	fmt.Fprintln(stderr, docsUsage)
	return 2
}

func sessionDocsLs(dir sessDir, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session docs ls", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print the directory as JSON")
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 0 {
		fmt.Fprintln(stderr, docsUsage)
		return 2
	}
	d, ok := openDocs("docs ls", dir, svc, stderr)
	if !ok {
		return 1
	}
	defer d.Close()
	ovs := overviewsNewestFirst(d)
	notes := listedNotes(d, "")
	if *asJSON {
		data, _ := json.MarshalIndent(struct {
			Dir       string           `json:"dir"`
			Overviews []steer.Overview `json:"overviews"`
			Notes     []steer.FileNote `json:"notes"`
		}{d.dir, ovs, notes}, "", "  ")
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	for _, o := range ovs {
		fmt.Fprintf(stdout, "overview\t%s\t%d anchors\t%s\n", o.ID, o.Anchors, o.Title)
	}
	for _, n := range notes {
		sum := n.Summary
		if n.Outdated {
			sum += " (outdated)"
		}
		fmt.Fprintf(stdout, "note\t%s\t%s\t%d-%d\t%s\n", n.ID, n.Path, n.Start, n.End, sum)
	}
	return 0
}

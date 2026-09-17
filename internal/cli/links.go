package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/homeend/gigagit/internal/domain"
)

// linksUsage is printed for every usage error of `gg links`.
const linksUsage = "usage: gg links [--json]"

// wireLinkHistEntry is `gg links --json`'s payload, one per row — the same
// convention as `gg link resolve --json`'s wireResolvedLink.
type wireLinkHistEntry struct {
	Link    string `json:"link"`
	Desc    string `json:"desc"`
	Created string `json:"created"`
}

// cmdLinks is `gg links`: this repo's copied-link history (every "copy gg
// link" action recorded by `gg link` or `gg compare`), newest-first, one
// "<desc>\t<link>" row per line, or --json for the structured form.
//
// History disabled (no resolvable state dir) and history empty both print
// nothing at exit 0 — LinkHistory returns nil for either, and neither is a
// usage or runtime error, matching RecordLink's best-effort posture on the
// write side.
func cmdLinks(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("links", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print the ring as JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if len(fs.Args()) > 0 {
		fmt.Fprintln(stderr, linksUsage)
		return 2
	}
	hist := svc.LinkHistory(context.Background())
	if *asJSON {
		wires := make([]wireLinkHistEntry, 0, len(hist))
		for _, e := range hist {
			wires = append(wires, wireLinkHistEntry{Link: e.Link, Desc: e.Desc, Created: e.Created})
		}
		if err := json.NewEncoder(stdout).Encode(wires); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 1
		}
		return 0
	}
	for _, e := range hist {
		fmt.Fprintf(stdout, "%s\t%s\n", e.Desc, e.Link)
	}
	return 0
}

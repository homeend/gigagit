package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/linknav"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// LaunchTUI launches the TUI for a checkout, positioned at a link. cmd/gg
// installs it; internal/cli must NOT import internal/tui (archtest guards the
// layering), so the launcher arrives as a seam. nil — the value every test
// sees — means no launcher is available, and `gg open` says so rather than
// pretending it opened something.
var LaunchTUI func(checkout string, at model.Link) int

const openUsage = "usage: gg open <gg://…> [--no-wait]\n" +
	"quote links that carry #<hunk> — an unquoted # starts a shell comment"

// cmdOpen is `gg open <link>`: show the link to the USER. A live gg session in
// the link's checkout (TUI or web page) is steered; otherwise the TUI is
// launched there, positioned on the link. A bare repository link (no place in
// it) always launches the TUI in that checkout.
func cmdOpen(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("open", flag.ContinueOnError)
	fs.SetOutput(stderr)
	noWait := fs.Bool("no-wait", false, "steer without waiting for the session's answer")
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 1 {
		fmt.Fprintln(stderr, openUsage)
		return 2
	}
	if !isLinkArg(pos[0]) {
		fmt.Fprintf(stderr, "open: %q is not a gg link (it must start with %s)\n", pos[0], model.LinkScheme)
		return 2
	}
	ctx := context.Background()
	res, err := resolveLinkArg(ctx, svc, pos[0])
	if err != nil {
		return linkExit("open", err, stderr)
	}
	if linknav.RepoOnly(res) {
		// A bare repository link names no place to steer a session to; showing
		// it to the user means opening gg in that checkout. (`gg session
		// navigate` keeps refusing it: a live session has nowhere to go.)
		if LaunchTUI == nil {
			fmt.Fprintf(stderr, "open: %s names a repository, not a place in it, and the TUI launcher is unavailable\n", res.Checkout)
			return 1
		}
		return LaunchTUI(res.Checkout, model.Link{})
	}
	// linkSteerDir, not a fresh GitCommonDir/SessionSteerDir pair: the common
	// checkout case (the link names wherever the caller already is) must reuse
	// THIS service's own inbox, exactly as `gg session navigate <link>` does —
	// two independently computed dirs for the same worktree would each watch
	// their own liveness file and never see the other's presence.
	dir, target, err := linkSteerDir(ctx, steerDirFor(svc), svc, res)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	// Every git question below is asked of the LINK's checkout, wherever the
	// caller ran (the headline case is `gg open` from /tmp).
	c, err := navigateCommandFor(ctx, target, res)
	if err != nil {
		return navExit("open", err, stderr)
	}
	if r := routeFor(dir); r.tuiOK || r.webOK {
		// sendSteer is the whole routing table — a live web page is reached over
		// HTTP exactly as `gg session navigate` reaches it.
		code := sendSteer(dir, c, *noWait, stdout, stderr)
		if code == 0 {
			fmt.Fprintln(stdout, "steered: "+res.Checkout)
		}
		return code
	}
	if LaunchTUI == nil {
		fmt.Fprintf(stderr, "open: no live gg session in %s and the TUI launcher is unavailable\n", res.Checkout)
		return 1
	}
	return LaunchTUI(res.Checkout, openAtLink(res, c))
}

// openAtLink is the link handed to the launcher (linknav.AtLink): the same
// place, fully resolved, with any #<hunk> ALREADY lowered to the line
// navigateCommandFor computed.
func openAtLink(res domain.Resolved, c steer.Command) model.Link { return linknav.AtLink(res, c) }

package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"

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

// LaunchWeb starts `gg web` in a checkout with the browser opened, positioned
// at a command the first page to boot lands on (a zero Command = nowhere in
// particular: a bare repository link). Installed by cmd/gg like LaunchTUI —
// internal/cli must not import internal/web either; nil = unavailable.
var LaunchWeb func(checkout string, at steer.Command) int

const openUsage = "usage: gg open <gg://…> [--web] [--no-wait]\n" +
	"quote links that carry #<hunk> — an unquoted # starts a shell comment"

// cmdOpen is `gg open <link>`: show the link to the USER. A live gg session in
// the link's checkout (TUI or web page) is steered; otherwise the TUI is
// launched there, positioned on the link. A bare repository link (no place in
// it) always launches the TUI in that checkout. --web names the browser as
// the frontend: a live web page is steered (and only the page — a TUI live
// beside it is left alone), otherwise `gg web` is started in that checkout
// with the browser opened, landing on the link.
func cmdOpen(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("open", flag.ContinueOnError)
	fs.SetOutput(stderr)
	noWait := fs.Bool("no-wait", false, "steer without waiting for the session's answer")
	web := fs.Bool("web", false, "show it in the browser: steer a live gg web page or start one")
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
		if *web {
			return openWebBare(ctx, svc, res, stdout, stderr)
		}
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
	if *web {
		return openWeb(dir, res, c, stdout, stderr)
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

// openWeb is the --web arm for a link that names a place: a live page in the
// link's checkout gets the command over HTTP — the page alone, never the TUI
// inbox, so a TUI live beside it does not jump too — else `gg web` is
// started there with c as the page's start-at.
func openWeb(dir string, res domain.Resolved, c steer.Command, stdout, stderr io.Writer) int {
	if r := routeFor(dir); r.webOK {
		if c.ID == "" {
			c.ID = steer.NewID()
		}
		err := postWebSteer(r.web.URL, c)
		if err == nil {
			fmt.Fprintln(stdout, "web: sent")
			fmt.Fprintln(stdout, "steered: "+res.Checkout)
			return 0
		}
		// A presence stays "live" for up to steer.LiveWindow after its server
		// died; only a TRANSPORT failure means nobody is listening, and then a
		// fresh server is the right answer. A page that answered (400, 409
		// "operation in flight") is alive — starting a second one beside it
		// would be worse than the error.
		var ue *url.Error
		if !errors.As(err, &ue) || LaunchWeb == nil {
			fmt.Fprintln(stderr, "web:", err)
			return 1
		}
		fmt.Fprintln(stderr, "web: the recorded page is gone ("+err.Error()+"); starting one")
	}
	if LaunchWeb == nil {
		fmt.Fprintf(stderr, "open: no live gg web page in %s and the web launcher is unavailable\n", res.Checkout)
		return 1
	}
	return LaunchWeb(res.Checkout, c)
}

// openWebBare is the --web arm for a bare repository link: a page already
// live in that checkout IS the answer (its URL is printed; nothing to steer
// it to), otherwise `gg web` is started there with no start-at.
func openWebBare(ctx context.Context, svc *domain.Service, res domain.Resolved, stdout, stderr io.Writer) int {
	if dir, _, err := linkSteerDir(ctx, steerDirFor(svc), svc, res); err == nil {
		if r := routeFor(dir); r.webOK {
			fmt.Fprintln(stdout, "web: "+r.web.URL)
			return 0
		}
	}
	if LaunchWeb == nil {
		fmt.Fprintf(stderr, "open: %s names a repository, not a place in it, and the web launcher is unavailable\n", res.Checkout)
		return 1
	}
	return LaunchWeb(res.Checkout, steer.Command{})
}

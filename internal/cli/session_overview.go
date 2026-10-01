package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

const overviewUsage = "usage: gg session overview add --title \"…\" [--file <path>] [--background] [--json]   (text from --file, else stdin)\n" +
	"       gg session overview set <id> [--title \"…\"] [--file <path>] [--json]\n" +
	"       gg session overview list [--json]\n" +
	"       gg session overview show <id> [--json]\n" +
	"       gg session overview rm <id>"

// overviewMaxBytes mirrors the TUI's limit on an overview's text.
const overviewMaxBytes = 64 << 10

// sessionOverview is `gg session overview …`: an agent's in-memory markdown
// overview whose links are anchors to files, lines and notes. It lives in a
// live gg TUI's memory — so only a TUI can answer.
func sessionOverview(dir string, svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, overviewUsage)
		return 2
	}
	switch args[0] {
	case "add", "set":
		return sessionOverviewWrite(dir, svc, args[0], args[1:], stdin, stdout, stderr)
	case "list":
		return sessionOverviewList(dir, args[1:], stdout, stderr)
	case "show":
		return sessionOverviewShow(dir, args[1:], stdout, stderr)
	case "rm":
		return sessionOverviewRm(dir, args[1:], stdout, stderr)
	}
	fmt.Fprintln(stderr, overviewUsage)
	return 2
}

// steerOverviews posts an overview command to the live session and waits for
// its answer: the TUI when one is live, else the gg web page.
func steerOverviews(dir string, c steer.Command, stdout, stderr io.Writer) (steer.Reply, int, bool) {
	rep, code, ok := steerLive(dir, c, false, false, stdout, stderr)
	if ok && !rep.OK {
		fmt.Fprintln(stderr, rep.Error)
		return rep, 1, false
	}
	return rep, code, ok
}

// sessionOverviewWrite is add and set: the text from --file, else stdin.
func sessionOverviewWrite(dir string, svc *domain.Service, verb string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session overview "+verb, flag.ContinueOnError)
	fs.SetOutput(stderr)
	title := fs.String("title", "", "the overview's name, one line")
	file := fs.String("file", "", "read the markdown from this file (default: stdin)")
	background := false
	if verb == "add" {
		fs.BoolVar(&background, "background", false, "add it without showing it")
	}
	asJSON := fs.Bool("json", false, "print the overview as JSON")
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	misuse := func(msg string) int {
		fmt.Fprintln(stderr, "session overview "+verb+": "+msg)
		return 2
	}
	if (verb == "add" && len(pos) != 0) || (verb == "set" && (len(pos) != 1 || !fileIDPattern.MatchString(pos[0]))) {
		fmt.Fprintln(stderr, overviewUsage)
		return 2
	}
	if verb == "add" && strings.TrimSpace(*title) == "" {
		return misuse("--title is required")
	}
	if strings.ContainsAny(*title, "\r\n") || utf8.RuneCountInString(*title) > 200 {
		return misuse("the title must be one line of at most 200 characters")
	}
	var src io.Reader = stdin
	if *file != "" {
		f, err := os.Open(*file)
		if err != nil {
			return misuse(err.Error())
		}
		defer f.Close()
		src = f
	}
	if src == nil {
		return misuse("give the text with --file or on stdin")
	}
	data, err := io.ReadAll(io.LimitReader(src, overviewMaxBytes+1))
	switch {
	case err != nil:
		return misuse(err.Error())
	case len(data) > overviewMaxBytes:
		return misuse("the text is over 64 KiB")
	case strings.TrimSpace(string(data)) == "":
		return misuse("the text is empty")
	}
	c := steer.Command{Cmd: "overview_" + verb, Title: *title, Text: string(data), Background: background}
	if verb == "set" {
		c.FileID = pos[0]
	}
	if svc != nil {
		c.Worktree = callerWorktree(svc)
	}
	r, code, ok := steerOverviews(dir, c, stdout, stderr)
	if !ok {
		return code
	}
	if len(r.Overviews) != 1 {
		fmt.Fprintln(stdout, r.Detail)
		return 0
	}
	o := r.Overviews[0]
	if *asJSON {
		data, _ := json.MarshalIndent(o, "", "  ")
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	fmt.Fprintln(stdout, o.ID)
	for _, u := range o.Unresolved {
		fmt.Fprintln(stdout, "unresolved: "+u)
	}
	// The detail is news when it says more than the id already does: the
	// overview waits in the background, or a file was pushed out for it.
	if d := r.Detail; d != "" && d != "showing "+o.ID && d != "set "+o.ID {
		fmt.Fprintln(stdout, d)
	}
	return 0
}

func sessionOverviewList(dir string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session overview list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print the overviews as JSON")
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 0 {
		fmt.Fprintln(stderr, overviewUsage)
		return 2
	}
	r, code, ok := steerOverviews(dir, steer.Command{Cmd: "overview_list"}, stdout, stderr)
	if !ok {
		return code
	}
	if *asJSON {
		list := r.Overviews
		if list == nil {
			list = []steer.Overview{}
		}
		data, _ := json.MarshalIndent(list, "", "  ")
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	for _, o := range r.Overviews {
		fmt.Fprintf(stdout, "%s\t%s\t%d anchors\t%s\n", o.ID, o.State, o.Anchors, o.Title)
	}
	return 0
}

func sessionOverviewShow(dir string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session overview show", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print the overview as JSON")
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 1 || !fileIDPattern.MatchString(pos[0]) {
		fmt.Fprintln(stderr, overviewUsage)
		return 2
	}
	r, code, ok := steerOverviews(dir, steer.Command{Cmd: "overview_show", FileID: pos[0]}, stdout, stderr)
	if !ok {
		return code
	}
	if len(r.Overviews) != 1 {
		fmt.Fprintln(stderr, "no overview "+pos[0])
		return 1
	}
	o := r.Overviews[0]
	if *asJSON {
		data, _ := json.MarshalIndent(o, "", "  ")
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	fmt.Fprintf(stdout, "%s\t%s\n\n%s\n", o.ID, o.Title, strings.TrimRight(o.Text, "\n"))
	return 0
}

func sessionOverviewRm(dir string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session overview rm", flag.ContinueOnError)
	fs.SetOutput(stderr)
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 1 || !fileIDPattern.MatchString(pos[0]) {
		fmt.Fprintln(stderr, overviewUsage)
		return 2
	}
	r, code, ok := steerOverviews(dir, steer.Command{Cmd: "overview_rm", FileID: pos[0]}, stdout, stderr)
	if !ok {
		return code
	}
	fmt.Fprintln(stdout, r.Detail)
	return 0
}

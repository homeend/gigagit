package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

const overviewUsage = "usage: gg session overview add --title \"…\" [--file <path>] [--background] [--json]   (text from --file, else stdin)\n" +
	"       gg session overview set <id> [--title \"…\"] [--file <path>] [--background] [--json]\n" +
	"       gg session overview list [--json]\n" +
	"       gg session overview show <id> [--json]\n" +
	"       gg session overview rm <id>\n" +
	"       gg session overview export <id> [--out <path>|-] [--force]   (one self-contained HTML file)"

// overviewMaxBytes is the store's limit on an overview's text.
const overviewMaxBytes = agentdocs.MaxOverviewBytes

// sessionOverview is `gg session overview …`: an agent's markdown overview
// whose links are anchors, as a FILE in the worktree's session-docs
// directory (spec 2026-10-18-gg-session-docs §6.2). add and set end by
// asking a live window to show it (file_focus) unless --background.
func sessionOverview(dir sessDir, svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, overviewUsage)
		return 2
	}
	switch args[0] {
	case "add", "set":
		return sessionOverviewWrite(dir, svc, args[0], args[1:], stdin, stdout, stderr)
	case "list":
		return sessionOverviewList(dir, svc, args[1:], stdout, stderr)
	case "show":
		return sessionOverviewShow(dir, svc, args[1:], stdout, stderr)
	case "rm":
		return sessionOverviewRm(dir, svc, args[1:], stdout, stderr)
	case "export":
		return sessionOverviewExport(dir, svc, args[1:], stdout, stderr)
	}
	fmt.Fprintln(stderr, overviewUsage)
	return 2
}

// overviewWire is an overview as the CLI prints it (anchors checked by the
// caller; unresolved = the destinations marked missing).
func overviewWire(o agentdocs.Overview, text bool) steer.Overview {
	w := steer.Overview{ID: o.ID, Title: o.Title, Anchors: len(o.Anchors), Author: o.Author}
	for _, a := range o.Anchors {
		if a.Missing {
			w.Unresolved = append(w.Unresolved, a.Dest)
		}
	}
	if !o.Created.IsZero() {
		w.Created = o.Created.UTC().Format(time.RFC3339)
	}
	if text {
		w.Text = o.Text
	}
	return w
}

// checkedOverview is the store's overview id with its anchors checked
// against the worktree and the directory.
func checkedOverview(d *docsSession, id string) (agentdocs.Overview, bool) {
	if _, ok := d.store.OverviewIn(d.root, id); !ok {
		return agentdocs.Overview{}, false
	}
	d.store.CheckAnchorsIn(d.root, id)
	return d.store.OverviewIn(d.root, id)
}

// sessionOverviewWrite is add and set: the text from --file, else stdin.
func sessionOverviewWrite(dir sessDir, svc *domain.Service, verb string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session overview "+verb, flag.ContinueOnError)
	fs.SetOutput(stderr)
	title := fs.String("title", "", "the overview's name, one line")
	file := fs.String("file", "", "read the markdown from this file (default: stdin)")
	background := fs.Bool("background", false, "write it without showing it")
	asJSON := fs.Bool("json", false, "print the overview as JSON")
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	misuse := func(msg string) int {
		fmt.Fprintln(stderr, "session overview "+verb+": "+msg)
		return 2
	}
	if (verb == "add" && len(pos) != 0) || (verb == "set" && (len(pos) != 1 || !agentdocs.ValidID(pos[0]))) {
		fmt.Fprintln(stderr, overviewUsage)
		return 2
	}
	if verb == "add" && strings.TrimSpace(*title) == "" {
		return misuse("--title is required")
	}
	if strings.ContainsAny(*title, "\r\n") || utf8.RuneCountInString(*title) > agentdocs.MaxOverviewTitle {
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
	// A terminal on stdin means nothing was piped: waiting there would hang
	// an agent that forgot --file.
	if src == nil || readerIsTerminal(src) {
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
	d, ok := openDocs("overview "+verb, dir, svc, stderr)
	if !ok {
		return 1
	}
	defer d.Close()
	var o agentdocs.Overview
	if verb == "add" {
		o, err = d.store.AddOverview(d.root, d.top, *title, string(data))
	} else {
		o, err = d.store.SetOverviewIn(d.root, pos[0], *title, string(data))
	}
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	o, _ = checkedOverview(d, o.ID)
	w := overviewWire(o, false)
	if *asJSON {
		data, _ := json.MarshalIndent(w, "", "  ")
		fmt.Fprintln(stdout, string(data))
	} else {
		fmt.Fprintln(stdout, w.ID)
		for _, u := range w.Unresolved {
			fmt.Fprintln(stdout, "unresolved: "+u)
		}
		// Checked here, not by a window: the file is written whatever a
		// window would say.
		backticked, anchors := domain.OverviewTextProblems(string(data))
		for _, b := range backticked {
			fmt.Fprintln(stdout, "in backticks: "+b)
		}
		if anchors == 0 {
			fmt.Fprintln(stdout, noAnchorWarning)
		}
	}
	if *background {
		return 0
	}
	focusAfterWrite(dir, w.ID, stdout, stderr)
	return 0
}

// focusAfterWrite asks the live window to show the document just written
// (spec §6.2 "Showing it"): a file_focus routed as any bare verb, its reply
// printed. No live window: nothing is printed, the file waits for the next
// reader. A refusal is printed but never fails the write (the file is there).
func focusAfterWrite(dir sessDir, id string, stdout, stderr io.Writer) {
	if _, _, why := dir.target(); why != "" {
		return
	}
	rep, _, ok := steerLive(dir, steer.Command{Cmd: "file_focus", FileID: id}, false, false, stdout, stderr)
	switch {
	case !ok:
	case !rep.OK:
		fmt.Fprintln(stderr, rep.Error)
	case rep.Detail != "":
		fmt.Fprintln(stdout, rep.Detail)
	}
}

func sessionOverviewList(dir sessDir, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
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
	d, ok := openDocs("overview list", dir, svc, stderr)
	if !ok {
		return 1
	}
	defer d.Close()
	list := overviewsNewestFirst(d)
	if *asJSON {
		data, _ := json.MarshalIndent(list, "", "  ")
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	for _, o := range list {
		fmt.Fprintf(stdout, "%s\t%d anchors\t%s\n", o.ID, o.Anchors, o.Title)
	}
	return 0
}

// overviewsNewestFirst is the directory's overviews, anchors checked, newest
// first (Overviews lists oldest first).
func overviewsNewestFirst(d *docsSession) []steer.Overview {
	ovs := d.store.Overviews(d.root)
	list := make([]steer.Overview, 0, len(ovs))
	for i := len(ovs) - 1; i >= 0; i-- {
		if o, ok := checkedOverview(d, ovs[i].ID); ok {
			list = append(list, overviewWire(o, false))
		}
	}
	return list
}

func sessionOverviewShow(dir sessDir, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session overview show", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print the overview as JSON")
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 1 || !agentdocs.ValidID(pos[0]) {
		fmt.Fprintln(stderr, overviewUsage)
		return 2
	}
	d, ok := openDocs("overview show", dir, svc, stderr)
	if !ok {
		return 1
	}
	defer d.Close()
	o, found := checkedOverview(d, pos[0])
	if !found {
		fmt.Fprintln(stderr, "no overview "+pos[0])
		return 1
	}
	w := overviewWire(o, true)
	if *asJSON {
		data, _ := json.MarshalIndent(w, "", "  ")
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n\n%s\n", w.ID, w.Title, w.Author, w.Created, strings.TrimRight(w.Text, "\n"))
	return 0
}

func sessionOverviewRm(dir sessDir, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session overview rm", flag.ContinueOnError)
	fs.SetOutput(stderr)
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 1 || !agentdocs.ValidID(pos[0]) {
		fmt.Fprintln(stderr, overviewUsage)
		return 2
	}
	d, ok := openDocs("overview rm", dir, svc, stderr)
	if !ok {
		return 1
	}
	defer d.Close()
	if !d.store.RemoveOverviewIn(d.root, pos[0]) {
		fmt.Fprintln(stderr, "no overview "+pos[0])
		return 1
	}
	fmt.Fprintln(stdout, "removed "+pos[0])
	return 0
}

// sessionOverviewExport writes an overview as one self-contained HTML file:
// its text and its note anchors' spans from the directory, the files read
// from this worktree.
func sessionOverviewExport(dir sessDir, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session overview export", flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := fs.String("out", "", "write here (default: the export dir); - = stdout")
	force := fs.Bool("force", false, "overwrite an existing file")
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 1 || !agentdocs.ValidID(pos[0]) || svc == nil {
		fmt.Fprintln(stderr, overviewUsage)
		return 2
	}
	d, ok := openDocs("overview export", dir, svc, stderr)
	if !ok {
		return 1
	}
	defer d.Close()
	o, found := d.store.OverviewIn(d.root, pos[0])
	if !found {
		fmt.Fprintln(stderr, "no overview "+pos[0])
		return 1
	}
	src := domain.OverviewSource{Title: o.Title, Text: o.Text, NoteSpans: map[string]domain.NoteSpan{}}
	for _, n := range listedNotes(d, "") {
		src.NoteSpans[n.ID] = domain.NoteSpan{Path: n.Path, Start: n.Start, End: n.End}
	}
	ctx := context.Background()
	page, name, err := svc.ExportOverviewHTML(ctx, src)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return writeExport(ctx, svc, "session overview export", page, name, *out, *force, stdout, stderr)
}

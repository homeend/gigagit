package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

const noteUsage = "usage: gg session note add <path>:<start>[-<end>] --summary \"…\" [--rationale \"…\"] [--author <name>] [--meta k=v]… [--display <json>|@file] [--json]\n" +
	"       gg session note list [<path>] [--json]\n" +
	"       gg session note show <id> [--json]\n" +
	"       gg session note rm <id>\n" +
	"       gg session note clear <path>"

var noteRange = regexp.MustCompile(`^(.+):([0-9]+)(?:-([0-9]+))?$`)

// sessionNote is `gg session note …`: an agent's temporary notes on
// working-tree files, as FILES in the worktree's session-docs directory
// (spec 2026-10-18-gg-session-docs §6.2). Every gg reader of the worktree
// shows them; no window needs to be live.
func sessionNote(dir sessDir, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, noteUsage)
		return 2
	}
	switch args[0] {
	case "add":
		return sessionNoteAdd(dir, svc, args[1:], stdout, stderr)
	case "list":
		return sessionNoteList(dir, svc, args[1:], stdout, stderr)
	case "show":
		return sessionNoteShow(dir, svc, args[1:], stdout, stderr)
	case "rm":
		return sessionNoteRm(dir, svc, args[1:], stdout, stderr)
	case "clear":
		return sessionNoteClear(dir, svc, args[1:], stdout, stderr)
	}
	fmt.Fprintln(stderr, noteUsage)
	return 2
}

func sessionNoteAdd(dir sessDir, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session note add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	summary := fs.String("summary", "", "the remark, one short paragraph")
	rationale := fs.String("rationale", "", "the longer explanation")
	author := fs.String("author", "", "who is speaking (default: agent)")
	asJSON := fs.Bool("json", false, "print the new note as JSON")
	var meta metaFlag
	fs.Var(&meta, "meta", "meta key=value (repeatable)")
	display := fs.String("display", "", "display declaration: JSON or @file")
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 1 {
		fmt.Fprintln(stderr, noteUsage)
		return 2
	}
	mt := noteRange.FindStringSubmatch(pos[0])
	if mt == nil {
		fmt.Fprintln(stderr, "session note add: name the lines — <path>:<start>[-<end>]")
		return 2
	}
	start, _ := strconv.Atoi(mt[2])
	end := start
	if mt[3] != "" {
		end, _ = strconv.Atoi(mt[3])
	}
	switch {
	case start < 1:
		fmt.Fprintln(stderr, "session note add: a line number is 1-based")
		return 2
	case end < start:
		fmt.Fprintln(stderr, "session note add: the range ends before it starts")
		return 2
	case strings.TrimSpace(*summary) == "":
		fmt.Fprintln(stderr, "session note add: --summary is required")
		return 2
	}
	kv, ok := checkedMeta("session note add", meta, stderr)
	if !ok {
		return 2
	}
	disp, ok := displayArg("session note add", *display, stderr)
	if !ok {
		return 2
	}
	d, ok := openDocs("note add", dir, svc, stderr)
	if !ok {
		return 1
	}
	defer d.Close()
	path := mt[1]
	t := agentdocs.NoteText{Summary: *summary, Rationale: *rationale, Author: *author, Meta: kv, Display: disp}
	n, err := d.store.AddNoteMeta(d.root, path, d.readLines(path), start, end, t)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if *asJSON {
		data, _ := json.MarshalIndent(d.noteWire(n, nil), "", "  ")
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	fmt.Fprintln(stdout, agentdocs.NotedDetail(n, ""))
	return 0
}

// listedNotes is the directory's notes (one path, or every noted path),
// placed on the files as they are now, by path then line.
func listedNotes(d *docsSession, only string) []steer.FileNote {
	paths := d.store.NotedPaths(d.root)
	if only != "" {
		paths = []string{only}
	}
	sort.Strings(paths)
	notes := []steer.FileNote{}
	for _, p := range paths {
		for _, n := range d.alignedNotes(p) {
			notes = append(notes, d.noteWire(n, nil))
		}
	}
	return notes
}

func printNoteRow(stdout io.Writer, n steer.FileNote) {
	sum := n.Summary
	if n.Outdated {
		sum += " (outdated)"
	}
	fmt.Fprintf(stdout, "%s\t%s\t%d-%d\t%s\n", n.ID, n.Path, n.Start, n.End, sum)
}

func sessionNoteList(dir sessDir, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session note list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print the notes as JSON")
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) > 1 {
		fmt.Fprintln(stderr, noteUsage)
		return 2
	}
	d, ok := openDocs("note list", dir, svc, stderr)
	if !ok {
		return 1
	}
	defer d.Close()
	only := ""
	if len(pos) == 1 {
		only = pos[0]
	}
	notes := listedNotes(d, only)
	if *asJSON {
		data, _ := json.MarshalIndent(notes, "", "  ")
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	for _, n := range notes {
		printNoteRow(stdout, n)
	}
	return 0
}

func sessionNoteShow(dir sessDir, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session note show", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print the note as JSON")
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 1 || !agentdocs.ValidID(pos[0]) {
		fmt.Fprintln(stderr, noteUsage)
		return 2
	}
	d, ok := openDocs("note show", dir, svc, stderr)
	if !ok {
		return 1
	}
	defer d.Close()
	n, found := d.store.FindNoteIn(d.root, pos[0])
	if !found {
		fmt.Fprintln(stderr, "no note "+pos[0])
		return 1
	}
	if lines := d.readLines(n.Path); len(lines) > 0 {
		d.store.Align(d.root, n.Path, lines) // place it on the file as it is now
		n, _ = d.store.FindNoteIn(d.root, pos[0])
	}
	w := d.noteWire(n, d.store.NoteTextIn(d.root, pos[0]))
	if *asJSON {
		data, _ := json.MarshalIndent(w, "", "  ")
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	head := fmt.Sprintf("%s\t%s:%d-%d\t%s", w.ID, w.Path, w.Start, w.End, w.Author)
	if w.Outdated {
		head += "\t(outdated)"
	}
	fmt.Fprintln(stdout, head)
	fmt.Fprintln(stdout, w.Summary)
	if w.Rationale != "" {
		fmt.Fprintf(stdout, "\n%s\n", w.Rationale)
	}
	if len(w.Text) > 0 {
		fmt.Fprintln(stdout)
		for i, l := range w.Text {
			fmt.Fprintf(stdout, "%d\t%s\n", w.Start+i, l)
		}
	}
	return 0
}

func sessionNoteRm(dir sessDir, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session note rm", flag.ContinueOnError)
	fs.SetOutput(stderr)
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 1 || !agentdocs.ValidID(pos[0]) {
		fmt.Fprintln(stderr, noteUsage)
		return 2
	}
	d, ok := openDocs("note rm", dir, svc, stderr)
	if !ok {
		return 1
	}
	defer d.Close()
	if !d.store.RemoveNoteIn(d.root, pos[0]) {
		fmt.Fprintln(stderr, "no note "+pos[0])
		return 1
	}
	fmt.Fprintln(stdout, "removed "+pos[0])
	return 0
}

// sessionNoteClear unlinks every note on one path.
func sessionNoteClear(dir sessDir, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session note clear", flag.ContinueOnError)
	fs.SetOutput(stderr)
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 1 {
		fmt.Fprintln(stderr, noteUsage)
		return 2
	}
	d, ok := openDocs("note clear", dir, svc, stderr)
	if !ok {
		return 1
	}
	defer d.Close()
	n := d.store.ClearPath(d.root, pos[0])
	if n == 0 {
		fmt.Fprintln(stderr, "no notes on "+pos[0])
		return 1
	}
	fmt.Fprintf(stdout, "removed %d notes on %s\n", n, pos[0])
	return 0
}

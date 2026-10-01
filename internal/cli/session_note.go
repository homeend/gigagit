package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

const noteUsage = "usage: gg session note add <path|file-id>:<start>[-<end>] --summary \"…\" [--rationale \"…\"] [--author <name>] [--json]\n" +
	"       gg session note list [<path>|<file-id>] [--json]\n" +
	"       gg session note show <note-id> [--json]\n" +
	"       gg session note rm <note-id>\n" +
	"       gg session note clear <path>|<file-id>"

var (
	noteIDPattern = regexp.MustCompile(`^t[0-9]+$`)
	noteRange     = regexp.MustCompile(`^(.+):([0-9]+)(?:-([0-9]+))?$`)
)

// sessionNote is `gg session note …`: temporary notes on the files open in a
// live gg TUI. They live in that TUI's memory — so only a TUI can answer.
func sessionNote(dir sessDir, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, noteUsage)
		return 2
	}
	switch args[0] {
	case "add":
		return sessionNoteAdd(dir, svc, args[1:], stdout, stderr)
	case "list":
		return sessionNoteList(dir, args[1:], stdout, stderr)
	case "show":
		return sessionNoteShow(dir, args[1:], stdout, stderr)
	case "rm":
		return sessionNoteRm(dir, args[1:], false, stdout, stderr)
	case "clear":
		return sessionNoteRm(dir, args[1:], true, stdout, stderr)
	}
	fmt.Fprintln(stderr, noteUsage)
	return 2
}

// steerNotes posts a note command to the live session and waits for its
// answer: the TUI when one is live, else gg web, which answers from its own
// store (steerLive's routing).
func steerNotes(dir sessDir, c steer.Command, stdout, stderr io.Writer) (steer.Reply, int, bool) {
	rep, code, ok := steerLive(dir, c, false, false, stdout, stderr)
	if ok && !rep.OK {
		fmt.Fprintln(stderr, rep.Error)
		return rep, 1, false
	}
	return rep, code, ok
}

// fileTarget splits a <path>|<file-id> argument.
func fileTarget(s string) (id, path string) {
	if fileIDPattern.MatchString(s) {
		return s, ""
	}
	return "", s
}

func sessionNoteAdd(dir sessDir, svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session note add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	summary := fs.String("summary", "", "the remark, one short paragraph")
	rationale := fs.String("rationale", "", "the longer explanation")
	author := fs.String("author", "", "who is speaking (default: agent)")
	asJSON := fs.Bool("json", false, "print the new note as JSON")
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
	id, path := fileTarget(mt[1])
	c := steer.Command{Cmd: "note_add", FileID: id, File: path, Start: start, End: end,
		Summary: *summary, Rationale: *rationale, Author: *author}
	if svc != nil {
		c.Worktree = callerWorktree(svc)
	}
	r, code, ok := steerNotes(dir, c, stdout, stderr)
	if !ok {
		return code
	}
	if *asJSON && len(r.Notes) == 1 {
		data, _ := json.MarshalIndent(r.Notes[0], "", "  ")
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	fmt.Fprintln(stdout, r.Detail)
	return 0
}

func sessionNoteList(dir sessDir, args []string, stdout, stderr io.Writer) int {
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
	c := steer.Command{Cmd: "note_list"}
	if len(pos) == 1 {
		c.FileID, c.File = fileTarget(pos[0])
	}
	r, code, ok := steerNotes(dir, c, stdout, stderr)
	if !ok {
		return code
	}
	if *asJSON {
		notes := r.Notes
		if notes == nil {
			notes = []steer.FileNote{}
		}
		data, _ := json.MarshalIndent(notes, "", "  ")
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	for _, n := range r.Notes {
		sum := n.Summary
		if n.Outdated {
			sum += " (outdated)"
		}
		fmt.Fprintf(stdout, "%s\t%s\t%d-%d\t%s\n", n.ID, n.Path, n.Start, n.End, sum)
	}
	return 0
}

func sessionNoteShow(dir sessDir, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session note show", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print the note as JSON")
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 1 || !noteIDPattern.MatchString(pos[0]) {
		fmt.Fprintln(stderr, noteUsage)
		return 2
	}
	r, code, ok := steerNotes(dir, steer.Command{Cmd: "note_show", NoteID: pos[0]}, stdout, stderr)
	if !ok {
		return code
	}
	if len(r.Notes) != 1 {
		fmt.Fprintln(stderr, "no note "+pos[0])
		return 1
	}
	n := r.Notes[0]
	if *asJSON {
		data, _ := json.MarshalIndent(n, "", "  ")
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	head := fmt.Sprintf("%s\t%s:%d-%d\t%s", n.ID, n.Path, n.Start, n.End, n.Author)
	if n.Outdated {
		head += "\t(outdated)"
	}
	fmt.Fprintln(stdout, head)
	fmt.Fprintln(stdout, n.Summary)
	if n.Rationale != "" {
		fmt.Fprintf(stdout, "\n%s\n", n.Rationale)
	}
	if len(n.Text) > 0 {
		fmt.Fprintln(stdout)
		for i, l := range n.Text {
			fmt.Fprintf(stdout, "%d\t%s\n", n.Start+i, l)
		}
	}
	return 0
}

// sessionNoteRm is `note rm <note-id>` and, with file set, `note clear
// <path>|<file-id>` — every note of one open file.
func sessionNoteRm(dir sessDir, args []string, file bool, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session note rm", flag.ContinueOnError)
	fs.SetOutput(stderr)
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 1 || (!file && !noteIDPattern.MatchString(pos[0])) {
		fmt.Fprintln(stderr, noteUsage)
		return 2
	}
	c := steer.Command{Cmd: "note_rm", NoteID: pos[0]}
	if file {
		c.NoteID = ""
		c.FileID, c.File = fileTarget(pos[0])
	}
	r, code, ok := steerNotes(dir, c, stdout, stderr)
	if !ok {
		return code
	}
	fmt.Fprintln(stdout, r.Detail)
	return 0
}

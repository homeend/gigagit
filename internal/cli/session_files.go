package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"regexp"
	"strconv"

	"github.com/homeend/gigagit/internal/steer"
)

const filesUsage = "usage: gg session files [--json]\n" +
	"       gg session files focus <id|path>[:<line>] [--no-wait]"

var (
	fileIDPattern = regexp.MustCompile(`^f[0-9]+$`)
	trailingLine  = regexp.MustCompile(`^(.+):([0-9]+)$`)
)

// parseFileTarget splits `files focus`'s argument: a trailing :<digits> is
// the line; f<digits> is an open file's id, anything else a path (the TUI
// also tries an id that matches nothing as a path).
func parseFileTarget(s string) (id, path string, line int) {
	if m := trailingLine.FindStringSubmatch(s); m != nil {
		s = m[1]
		line, _ = strconv.Atoi(m[2])
	}
	if fileIDPattern.MatchString(s) {
		return s, "", line
	}
	return "", s, line
}

// sessionFiles is `gg session files [--json]` — the live TUI's open files for
// the worktree it shows — and `gg session files focus`.
func sessionFiles(dir string, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "focus" {
		return sessionFilesFocus(dir, args[1:], stdout, stderr)
	}
	fs := flag.NewFlagSet("session files", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print the list as JSON")
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 0 {
		fmt.Fprintln(stderr, filesUsage)
		return 2
	}
	r, code, ok := steerTUI(dir, steer.Command{Cmd: "files"}, false, stdout, stderr)
	if !ok {
		return code
	}
	if !r.OK {
		fmt.Fprintln(stderr, r.Error)
		return 1
	}
	if *asJSON {
		files := r.Files
		if files == nil {
			files = []steer.OpenFile{}
		}
		data, _ := json.MarshalIndent(files, "", "  ")
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	for _, f := range r.Files {
		src := f.Source
		switch f.Source {
		case "commit":
			src = "commit " + f.Rev[:min(7, len(f.Rev))]
		case "shelf":
			src = "shelf " + f.Rev
		}
		line := "-"
		if f.Line > 0 {
			line = ":" + strconv.Itoa(f.Line)
		}
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\t%s\n", f.ID, f.Path, src, line, f.State)
	}
	return 0
}

// sessionFilesFocus is `gg session files focus <id|path>[:<line>]`: bring an
// open file to the front, optionally at a line.
func sessionFilesFocus(dir string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("session files focus", flag.ContinueOnError)
	fs.SetOutput(stderr)
	noWait := fs.Bool("no-wait", false, "post the command and exit without waiting for an answer")
	pos, err := parseSteerFlags(fs, args)
	if err != nil {
		return 2
	}
	if len(pos) != 1 {
		fmt.Fprintln(stderr, filesUsage)
		return 2
	}
	id, path, line := parseFileTarget(pos[0])
	if trailingLine.MatchString(pos[0]) && line < 1 {
		fmt.Fprintln(stderr, "session files focus: a line number is 1-based")
		return 2
	}
	c := steer.Command{Cmd: "file_focus", FileID: id, File: path}
	if line > 0 {
		c.Line = &steer.Line{No: line}
	}
	r, code, ok := steerTUI(dir, c, *noWait, stdout, stderr)
	if !ok {
		return code
	}
	return printSteerReply(r, stdout, stderr)
}

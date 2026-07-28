package template

import "strings"

// FlattenForCmd repairs a command line for cmd.exe, which gg feeds by writing
// it to a temp .bat. Two things a POSIX shell accepts cannot be expressed in a
// batch file at all, and both appear in gg's own tool templates:
//
//   - a line ending in a backslash (POSIX line continuation). cmd.exe has no
//     continuation: it passes the backslash on as an argument and every
//     following line runs as a separate command, so the flags are lost.
//   - a double-quoted string spanning lines. cmd.exe ends the argument at the
//     newline, so the command runs with a truncated argument and, again, the
//     rest of the lines run as separate commands.
//
// The second one is what made "Claude (yolo)" launch WITHOUT
// --dangerously-skip-permissions on Windows: the flag sits at the end of the
// prompt's fifth line, and cmd.exe had already run claude at the end of the
// first, with a truncated prompt and no flags at all.
//
// Only those two cases are joined, so a genuine multi-line batch script (a
// user's post-create hook) still runs line by line. Output is CRLF-separated,
// which is what a .bat wants.
//
// The trailing-backslash rule does misread a line that legitimately ends in
// one (`dir C:\foo\`), which is why it is scoped to line ENDS in a command gg
// itself assembles from templates.
func FlattenForCmd(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	var out []string
	var cur strings.Builder
	open := false // inside an unterminated double quote
	for i, line := range lines {
		cont := false
		trimmed := strings.TrimRight(line, " \t")
		if strings.HasSuffix(trimmed, `\`) {
			line = strings.TrimSuffix(trimmed, `\`)
			cont = true
		}
		if cur.Len() > 0 {
			cur.WriteString(" ")
		}
		cur.WriteString(strings.TrimRight(line, " \t"))
		if strings.Count(line, `"`)%2 == 1 {
			open = !open
		}
		if (cont || open) && i < len(lines)-1 {
			continue // the next line belongs to this command
		}
		out = append(out, cur.String())
		cur.Reset()
		open = false
	}
	return strings.Join(out, "\r\n")
}

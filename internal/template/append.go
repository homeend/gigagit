package template

import (
	"runtime"
	"strings"
)

// AppendBlocker reports what would keep an argument appended to cmd from
// reaching cmd's first program ("" = nothing): a list or pipeline operator
// (`;`, `|`, `||`, `&`, `&&`), a line break that starts a new command, or a
// comment (`#`). Quoted text and escaped characters are skipped. Trailing
// blanks are ignored — the caller trims them before appending.
func AppendBlocker(cmd string) string { return AppendBlockerFor(cmd, runtime.GOOS) }

// AppendBlockerFor is AppendBlocker with the OS as a parameter (a test seam
// for both renderings of the catalog templates). It scans cmd as goos's
// shell would split it: POSIX sh (single and double quotes, backslash
// escapes, `\`+newline continues the line, `#` at a word start comments) or
// cmd.exe (double quotes, `^` escapes, no comments).
func AppendBlockerFor(cmd, goos string) string {
	windows := goos == "windows"
	if windows {
		cmd = FlattenForCmd(cmd) // what cmd.exe runs: continuations joined
	}
	cmd = strings.TrimRight(cmd, " \t\r\n")
	var quote rune // 0, '\'' or '"'
	wordStart := true
	rs := []rune(cmd)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case quote == '\'':
			if r == '\'' {
				quote = 0
			}
			continue
		case quote == '"':
			if r == '"' {
				quote = 0
			} else if r == '\\' && !windows {
				i++
			}
			continue
		}
		switch {
		case r == '"' || r == '\'' && !windows:
			quote = r
		case r == '\\' && !windows, r == '^' && windows:
			i++ // the next character is literal; `\`+newline continues the line
		case r == ';' && !windows:
			return ";"
		case r == '&' && (i > 0 && (rs[i-1] == '>' || rs[i-1] == '<') || i+1 < len(rs) && rs[i+1] == '>'):
			// a redirection (2>&1, &> file), not a list operator
		case r == '|' || r == '&':
			if i+1 < len(rs) && rs[i+1] == r {
				return string([]rune{r, r})
			}
			return string(r)
		case r == '\n' || r == '\r':
			return "a line break"
		case r == '#' && wordStart && !windows:
			return "#"
		}
		wordStart = r == ' ' || r == '\t' || r == '\r'
	}
	return ""
}

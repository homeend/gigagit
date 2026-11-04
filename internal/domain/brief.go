package domain

import "strings"

// workerOnlySections are the brief sections the worker follows and the user
// never reads: how to do the task, not the task (gg-delegate's brief
// template). Matched on the `## ` heading, case-insensitive, a trailing
// colon allowed.
var workerOnlySections = map[string]bool{"rules": true, "do not": true, "done when": true, "files": true}

// SplitBrief cuts a worker's brief into the task the user reads (the worker's
// console prints it, the Brief tour shows it) and the rules only the worker
// obeys. A worker-only section runs from its `## ` heading to the next `# `
// or `## ` heading; headings inside a code fence are text. A brief with no
// such section is all task. Both parts end in one newline (or are empty).
func SplitBrief(brief string) (task, rules string) {
	var t, r strings.Builder
	inRules, fence := false, ""
	for _, line := range strings.SplitAfter(brief, "\n") {
		trim := strings.TrimSpace(line)
		switch {
		case fence != "":
			if strings.HasPrefix(trim, fence) {
				fence = ""
			}
		case strings.HasPrefix(trim, "```"), strings.HasPrefix(trim, "~~~"):
			fence = trim[:3]
		case strings.HasPrefix(line, "## "):
			name := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(line[3:]), ":"))
			inRules = workerOnlySections[strings.TrimSpace(name)]
		case strings.HasPrefix(line, "# "):
			inRules = false
		}
		if inRules {
			r.WriteString(line)
		} else {
			t.WriteString(line)
		}
	}
	return oneTrailingNewline(t.String()), oneTrailingNewline(r.String())
}

func oneTrailingNewline(s string) string {
	s = strings.TrimRight(s, " \t\r\n")
	if s == "" {
		return ""
	}
	return s + "\n"
}

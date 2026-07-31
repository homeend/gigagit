// Package agentskill carries the "using-gg" skill that teaches AI coding
// agents to drive git through the gg CLI. The content is compiled into the
// binary (go:embed); installed copies are derived artifacts that change only
// when a newer binary's init runs.
package agentskill

import (
	_ "embed"
	"fmt"
	"regexp"
	"strconv"
)

//go:embed using-gg.md
var body string

// Version is bumped whenever using-gg.md (or the rendered wrappers) change.
// Installed copies carry it so init can tell new/outdated/up-to-date apart.
const Version = 57

// Body is the canonical markdown body — no frontmatter, no markers.
func Body() string { return body }

// marker is the version stamp embedded in every rendered form.
func marker() string { return fmt.Sprintf("<!-- gg:using-gg:v%d -->", Version) }

// SkillFile renders the Claude Code SKILL.md form: YAML frontmatter + version
// marker + body. The whole file is gg-owned and safe to overwrite.
func SkillFile() string {
	return "---\n" +
		"name: using-gg\n" +
		"description: Use when performing git operations (status, commit, pull, push, branch switch, stash, worktrees) in a repository where the gg CLI is available.\n" +
		"---\n\n" +
		marker() + "\n\n" + body
}

// PlainFile renders a frontmatter-free whole file (e.g. Cursor rules).
func PlainFile() string { return marker() + "\n\n" + body }

// Block renders the managed-block form for shared files (AGENTS.md, …): the
// body wrapped in begin/end markers so init can replace it without touching
// surrounding content.
func Block() string {
	return fmt.Sprintf("<!-- gg:using-gg:v%d:begin -->\n\n%s\n<!-- gg:using-gg:end -->", Version, body)
}

var versionRe = regexp.MustCompile(`gg:using-gg:v(\d+)`)

// HasMarker reports whether content carries any gg using-gg marker (any
// version, any rendered form).
func HasMarker(content []byte) bool { return versionRe.Match(content) }

// InstalledVersion extracts the version stamped into previously installed
// content (any rendered form). 0 means no gg marker present.
func InstalledVersion(content []byte) int {
	m := versionRe.FindSubmatch(content)
	if m == nil {
		return 0
	}
	n, err := strconv.Atoi(string(m[1]))
	if err != nil {
		return 0
	}
	return n
}

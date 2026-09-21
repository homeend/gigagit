// Package agentskill carries the skills that teach AI coding agents to drive gg:
// "using-gg" (the git CLI surface) and "reviewing-with-gg" (the review-notes
// lane). The content is compiled into the binary (go:embed); installed copies
// are derived artifacts that change only when a newer binary's init runs.
package agentskill

import (
	_ "embed"
	"fmt"
	"regexp"
	"strconv"
)

//go:embed using-gg.md
var usingBody string

//go:embed reviewing-with-gg.md
var reviewBody string

// Version is bumped whenever using-gg.md (or the rendered wrappers) change.
// Installed copies carry it so init can tell new/outdated/up-to-date apart.
const Version = 91

// ReviewVersion is the same counter for reviewing-with-gg, which starts at 1
// and moves independently of Version.
const ReviewVersion = 8

// Skill is one embedded skill: its identity, its own version counter, and the
// rendered forms init installs. Markers are per-skill ("gg:<name>:v<N>"), so
// two skills in one shared file never overwrite each other and each is
// recognised only by itself.
type Skill struct {
	Name        string
	Description string
	Version     int

	body    string
	verRe   *regexp.Regexp
	blockRe *regexp.Regexp
}

func newSkill(name, description string, version int, body string) Skill {
	return Skill{
		Name: name, Description: description, Version: version, body: body,
		verRe:   regexp.MustCompile(`gg:` + regexp.QuoteMeta(name) + `:v(\d+)`),
		blockRe: regexp.MustCompile(`(?s)<!-- gg:` + regexp.QuoteMeta(name) + `:v\d+:begin -->.*?<!-- gg:` + regexp.QuoteMeta(name) + `:end -->`),
	}
}

// UsingGG teaches the gg CLI. Its marker text is unchanged from before the
// two-skill split so copies installed by older binaries stay recognised.
var UsingGG = newSkill("using-gg",
	"Use when performing git operations (status, commit, pull, push, branch switch, stash, worktrees) in a repository where the gg CLI is available.",
	Version, usingBody)

// ReviewingWithGG teaches the review-notes lane (gg diff --hunks, gg note …).
//
// The description is rendered as a PLAIN (unquoted) YAML scalar, so it must
// not contain ": " — that is a mapping indicator and would make the whole
// frontmatter unparseable, which strict skill loaders treat as no skill at
// all. TestRenderedFrontmatterIsPlainScalarSafe enforces it for every skill.
var ReviewingWithGG = newSkill("reviewing-with-gg",
	"Use when reviewing code changes in a repository where the gg CLI is available — inspect diffs and leave anchored review notes with gg note.",
	ReviewVersion, reviewBody)

// All is the install set, in a stable order.
func All() []Skill { return []Skill{UsingGG, ReviewingWithGG} }

// Body is the canonical markdown body — no frontmatter, no markers.
func (s Skill) Body() string { return s.body }

// Marker is the version stamp embedded in every rendered form.
func (s Skill) Marker() string { return fmt.Sprintf("<!-- gg:%s:v%d -->", s.Name, s.Version) }

// SkillFile renders the Claude Code SKILL.md form: YAML frontmatter + version
// marker + body. The whole file is gg-owned and safe to overwrite.
func (s Skill) SkillFile() string {
	return "---\n" +
		"name: " + s.Name + "\n" +
		"description: " + s.Description + "\n" +
		"---\n\n" +
		s.Marker() + "\n\n" + s.body
}

// PlainFile renders a frontmatter-free whole file (e.g. Cursor rules).
func (s Skill) PlainFile() string { return s.Marker() + "\n\n" + s.body }

// Block renders the managed-block form for shared files (AGENTS.md, …): the
// body wrapped in begin/end markers so init can replace it without touching
// surrounding content — including a second skill's block in the same file.
func (s Skill) Block() string {
	return fmt.Sprintf("<!-- gg:%s:v%d:begin -->\n\n%s\n<!-- gg:%s:end -->", s.Name, s.Version, s.body, s.Name)
}

// BlockRe matches a previously installed block of THIS skill, any version.
func (s Skill) BlockRe() *regexp.Regexp { return s.blockRe }

// HasMarker reports whether content carries this skill's marker (any version,
// any rendered form).
func (s Skill) HasMarker(content []byte) bool { return s.verRe.Match(content) }

// InstalledVersion extracts the version stamped into previously installed
// content of this skill. 0 means no marker present.
func (s Skill) InstalledVersion(content []byte) int {
	m := s.verRe.FindSubmatch(content)
	if m == nil {
		return 0
	}
	n, err := strconv.Atoi(string(m[1]))
	if err != nil {
		return 0
	}
	return n
}

// The package-level forms are using-gg's, kept so existing callers and tests
// keep compiling unchanged.
func Body() string                        { return UsingGG.Body() }
func SkillFile() string                   { return UsingGG.SkillFile() }
func PlainFile() string                   { return UsingGG.PlainFile() }
func Block() string                       { return UsingGG.Block() }
func HasMarker(content []byte) bool       { return UsingGG.HasMarker(content) }
func InstalledVersion(content []byte) int { return UsingGG.InstalledVersion(content) }

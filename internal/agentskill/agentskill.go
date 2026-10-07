// Package agentskill carries the skills that teach AI coding agents to drive gg:
// "using-gg" (the git CLI surface), "reviewing-with-gg" (the review-notes
// lane), "delegate" (overseeing worker agents; the worker protocol) and
// "gg-review" (the user's /gg-review <gg-link> command) and "gg-cross-review"
// (/gg-cross-review: several models review, the agent merges and settles). The content is compiled into the binary (go:embed); installed copies
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

//go:embed delegate.md
var delegateBody string

//go:embed gg-review.md
var ggReviewBody string

//go:embed gg-cross-review.md
var ggCrossReviewBody string

// Version is bumped whenever using-gg.md (or the rendered wrappers) change.
// Installed copies carry it so init can tell new/outdated/up-to-date apart.
const Version = 145

// ReviewVersion is the same counter for reviewing-with-gg, which starts at 1
// and moves independently of Version.
const ReviewVersion = 15

// DelegateVersion is the counter for the delegate skill.
const DelegateVersion = 3

// GGReviewVersion is the counter for the gg-review skill.
const GGReviewVersion = 1

// GGCrossReviewVersion is the counter for the gg-cross-review skill.
const GGCrossReviewVersion = 1

// Skill is one embedded skill: its identity, its own version counter, and the
// rendered forms init installs. Markers are per-skill ("gg:<name>:v<N>"), so
// two skills in one shared file never overwrite each other and each is
// recognised only by itself.
type Skill struct {
	Name        string
	Description string
	Version     int

	// front is extra frontmatter for the SKILL.md form, each line ending in
	// "\n" (gg-review's argument hint and user-only invocation).
	front   string
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

// Delegate is the playbook for handing work to worker agents through gg's
// agent tools (the overseer's loop) and the protocol a worker follows; gg's
// kickoff line points every spawned worker at it. Short name on purpose:
// users type it (/delegate <task>).
var Delegate = newSkill("delegate",
	"Use when the user asks you to delegate a task to worker agents through gg — start workers in their own worktrees, brief them, wait for their reports and check the result; also the protocol a worker started by gg follows.",
	DelegateVersion, delegateBody)

// GGReview is the user-invoked /gg-review <gg-link> [focus]: review the change
// a link names and store ONE review document with `gg review save`. Never
// loaded on the model's own initiative (disable-model-invocation); installed
// wherever delegate is, and its body reads as plain instructions where there
// are no slash commands.
var GGReview = func() Skill {
	s := newSkill("gg-review",
		"Review the change a gg:// link names and store the review in gg — an overview plus per-file remarks — with gg review save.",
		GGReviewVersion, ggReviewBody)
	s.front = "argument-hint: \"<gg-link> [what to focus on]\"\n" + "disable-model-invocation: true\n"
	return s
}()

// GGCrossReview is the user-invoked /gg-cross-review <gg-link> [2|3] [focus]:
// run 2–3 headless copies of yourself on different models (gg review
// --model … --no-save), merge their reviews, rule on every disagreement and
// store ONE review. User-invoked like gg-review and installed beside it.
var GGCrossReview = func() Skill {
	s := newSkill("gg-cross-review",
		"Review the change a gg:// link names with 2–3 copies of yourself on different models, settle their disagreements, and store one merged review in gg.",
		GGCrossReviewVersion, ggCrossReviewBody)
	s.front = "argument-hint: \"<gg-link> [2|3] [what to focus on]\"\n" + "disable-model-invocation: true\n"
	return s
}()

// All is the install set, in a stable order.
func All() []Skill { return []Skill{UsingGG, ReviewingWithGG, Delegate, GGReview, GGCrossReview} }

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
		s.front +
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

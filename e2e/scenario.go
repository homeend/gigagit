// Package e2e runs gigagit's declarative end-to-end scenarios: each
// scenarios/*.toml builds a real git repository, runs gg CLI commands against
// it in-process, and asserts the user-visible outcome (files, branches,
// stashes, sync state, history shape) — never git internals.
package e2e

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Scenario is one declarative e2e test.
type Scenario struct {
	Name   string `toml:"name"`
	Input  Input  `toml:"input"`
	Runs   []Run  `toml:"run"`
	Expect Expect `toml:"expect"`
}

// Input declares the repository state a scenario starts from.
type Input struct {
	Steps  []Step  `toml:"steps"`
	Origin *Origin `toml:"origin"`
}

// Origin declares upstream history for clone/pull/push scenarios.
type Origin struct {
	Transport string `toml:"transport"` // "" or "http" (default) | "path"
	Steps     []Step `toml:"steps"`     // upstream history before the clone
	After     []Step `toml:"after"`     // upstream changes after the clone
}

// Step is one repo-building action; exactly one action field is set.
// Cwd retargets the step to another directory (sandbox-root-relative).
type Step struct {
	Write   string `toml:"write"`
	Content string `toml:"content"`
	Rm      string `toml:"rm"`
	Commit  string `toml:"commit"`
	Branch  string `toml:"branch"`
	Switch  string `toml:"switch"`
	// Stash is the stash message; the step runs `git stash push -u -m <msg>`.
	Stash        string `toml:"stash"`
	Worktree     string `toml:"worktree"`      // sandbox-root-relative path; Branch holds the branch
	BranchDelete string `toml:"branch_delete"` // `git branch -D <name>` (used to delete an origin branch)
	Tag          string `toml:"tag"`           // tag name; lightweight, or annotated when TagMessage is set
	TagMessage   string `toml:"tag_message"`   // when set, the tag is annotated (`git tag -a -m`)
	Cwd          string `toml:"cwd"`
}

// kind returns the step's single action name, or an error when the step is
// ambiguous or empty.
func (s Step) kind() (string, error) {
	var kinds []string
	if s.Write != "" {
		kinds = append(kinds, "write")
	}
	if s.Rm != "" {
		kinds = append(kinds, "rm")
	}
	if s.Commit != "" {
		kinds = append(kinds, "commit")
	}
	if s.Switch != "" {
		kinds = append(kinds, "switch")
	}
	if s.Stash != "" {
		kinds = append(kinds, "stash")
	}
	if s.Worktree != "" {
		kinds = append(kinds, "worktree")
	} else if s.Branch != "" {
		kinds = append(kinds, "branch") // bare branch creation
	}
	if s.BranchDelete != "" {
		kinds = append(kinds, "branch_delete")
	}
	if s.Tag != "" {
		kinds = append(kinds, "tag")
	}
	if len(kinds) != 1 {
		return "", fmt.Errorf("step %+v: want exactly one action, got %v", s, kinds)
	}
	k := kinds[0]
	if s.Worktree != "" && s.Branch == "" {
		return "", fmt.Errorf("step %+v: worktree requires branch", s)
	}
	if s.Content != "" && k != "write" {
		return "", fmt.Errorf("step %+v: content is only valid with write", s)
	}
	if s.TagMessage != "" && k != "tag" {
		return "", fmt.Errorf("step %+v: tag_message is only valid with tag", s)
	}
	return k, nil
}

// Run is one gg CLI invocation and its required exit code.
type Run struct {
	Cmd            []string `toml:"cmd"`
	Cwd            string   `toml:"cwd"`   // sandbox-root-relative; default "local"
	Stdin          string   `toml:"stdin"` // fed to the command's stdin; "" = empty reader
	Exit           *int     `toml:"exit"`
	StdoutContains []string `toml:"stdout_contains"` // substrings the run's stdout must contain
	StdoutExcludes []string `toml:"stdout_excludes"` // substrings the run's stdout must NOT contain
}

// MissingStdout returns the StdoutContains substrings absent from out.
func (r Run) MissingStdout(out string) []string {
	var miss []string
	for _, want := range r.StdoutContains {
		if !strings.Contains(out, want) {
			miss = append(miss, want)
		}
	}
	return miss
}

// PresentExcluded returns the StdoutExcludes substrings that wrongly appear in out.
func (r Run) PresentExcluded(out string) []string {
	var present []string
	for _, bad := range r.StdoutExcludes {
		if strings.Contains(out, bad) {
			present = append(present, bad)
		}
	}
	return present
}

// FileExpect is the normalized form of one file expectation.
type FileExpect struct {
	Content    string // literal content (when HasContent)
	HasContent bool
	SHA256     string
	Unchanged  bool
	Absent     bool
}

// StatusExpect asserts exact sets per index state; a nil slice = not asserted.
type StatusExpect struct {
	Staged     []string `toml:"staged"`
	Unstaged   []string `toml:"unstaged"`
	Untracked  []string `toml:"untracked"`
	Conflicted []string `toml:"conflicted"`
}

// StashExpect asserts one stash entry's content (entry N = [[expect.stash]] N,
// newest first). Names and dates are deliberately not assertable.
type StashExpect struct {
	Contains map[string]string `toml:"contains"`
}

// SubjectMatcher matches one commit subject: literal or regexp.
type SubjectMatcher struct {
	Literal string
	Pattern *regexp.Regexp
}

func (m SubjectMatcher) match(s string) bool {
	if m.Pattern != nil {
		return m.Pattern.MatchString(s)
	}
	return m.Literal == s
}

func (m SubjectMatcher) String() string {
	if m.Pattern != nil {
		return "matches(" + m.Pattern.String() + ")"
	}
	return fmt.Sprintf("%q", m.Literal)
}

// LogExpect asserts the full subject list of a branch, newest first.
type LogExpect struct {
	Branch   string `toml:"branch"` // default: current branch (HEAD)
	Subjects []any  `toml:"subjects"`

	SubjectsN []SubjectMatcher `toml:"-"`
}

// ScopedExpect carries files/status assertions for a linked worktree.
type ScopedExpect struct {
	Files  map[string]any `toml:"files"`
	Status *StatusExpect  `toml:"status"`

	FilesN map[string]FileExpect `toml:"-"`
}

// OriginExpect asserts origin-side refs (never origin's working tree).
type OriginExpect struct {
	Branches []string    `toml:"branches"`
	Tags     []string    `toml:"tags"`
	Log      []LogExpect `toml:"log"`
}

// Expect is the scenario's final-state contract.
type Expect struct {
	Branch     string                   `toml:"branch"`
	Branches   []string                 `toml:"branches"`
	Clean      *bool                    `toml:"clean"`
	Ahead      *int                     `toml:"ahead"`
	Behind     *int                     `toml:"behind"`
	InProgress string                   `toml:"in_progress"` // none|rebase|merge|cherry-pick|revert
	Stashes    *int                     `toml:"stashes"`
	Worktrees  []string                 `toml:"worktrees"` // sandbox-root-relative
	Files      map[string]any           `toml:"files"`
	Status     *StatusExpect            `toml:"status"`
	Stash      []StashExpect            `toml:"stash"`
	Log        []LogExpect              `toml:"log"`
	Worktree   map[string]*ScopedExpect `toml:"worktree"`
	Origin     *OriginExpect            `toml:"origin"`

	FilesN map[string]FileExpect `toml:"-"`
}

// LoadScenario parses and validates one scenario file. The loader is strict:
// unknown keys, unknown step kinds, or malformed values are errors — an
// agent's typo must never silently assert nothing.
func LoadScenario(path string) (*Scenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	dec := toml.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var s Scenario
	if err := dec.Decode(&s); err != nil {
		// StrictMissingError.Error() omits key names; use String() which includes them.
		if se, ok := err.(*toml.StrictMissingError); ok {
			return nil, fmt.Errorf("%s: strict mode: unknown fields: %s", path, se.String())
		}
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := s.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &s, nil
}

func (s *Scenario) validate() error {
	if s.Name == "" {
		return fmt.Errorf("name is required")
	}
	hasCommitIn := func(steps []Step) (bool, error) {
		found := false
		for _, st := range steps {
			k, err := st.kind()
			if err != nil {
				return false, err
			}
			if k == "commit" {
				found = true
			}
		}
		return found, nil
	}
	inputHasCommit, err := hasCommitIn(s.Input.Steps)
	if err != nil {
		return err
	}
	if o := s.Input.Origin; o != nil {
		if o.Transport != "" && o.Transport != "http" && o.Transport != "path" {
			return fmt.Errorf("origin.transport %q: want http or path", o.Transport)
		}
		originHasCommit, err := hasCommitIn(o.Steps)
		if err != nil {
			return err
		}
		if _, err := hasCommitIn(o.After); err != nil {
			return err
		}
		if !originHasCommit {
			return fmt.Errorf("origin.steps needs at least one commit step (the clone needs history)")
		}
	} else {
		if !inputHasCommit {
			return fmt.Errorf("input.steps needs at least one commit step (the harness-injected .gg.toml must be committed)")
		}
	}
	if len(s.Runs) == 0 {
		return fmt.Errorf("at least one [[run]] is required")
	}
	for i, r := range s.Runs {
		if len(r.Cmd) == 0 {
			return fmt.Errorf("run[%d]: cmd is required", i)
		}
		if r.Exit == nil {
			return fmt.Errorf("run[%d] %v: exit is required", i, r.Cmd)
		}
	}
	return s.Expect.normalize(s.Input.Origin != nil)
}

func (e *Expect) normalize(hasOrigin bool) error {
	switch e.InProgress {
	case "", "none", "rebase", "merge", "cherry-pick", "revert":
	default:
		return fmt.Errorf("in_progress %q: want none, rebase, merge, cherry-pick or revert", e.InProgress)
	}
	if (e.Ahead != nil || e.Behind != nil) && !hasOrigin {
		return fmt.Errorf("ahead/behind require [input.origin]")
	}
	if e.Origin != nil && !hasOrigin {
		return fmt.Errorf("[expect.origin] requires [input.origin]")
	}
	if e.Clean != nil && *e.Clean && e.Status != nil {
		return fmt.Errorf("clean = true and [expect.status] are mutually exclusive")
	}
	var err error
	if e.FilesN, err = normalizeFiles(e.Files); err != nil {
		return err
	}
	for path, se := range e.Worktree {
		if se == nil {
			continue
		}
		if se.FilesN, err = normalizeFiles(se.Files); err != nil {
			return fmt.Errorf("worktree %q: %w", path, err)
		}
	}
	for i := range e.Log {
		if err := e.Log[i].normalize(); err != nil {
			return err
		}
	}
	if e.Origin != nil {
		for i := range e.Origin.Log {
			if err := e.Origin.Log[i].normalize(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (l *LogExpect) normalize() error {
	for _, v := range l.Subjects {
		switch sv := v.(type) {
		case string:
			l.SubjectsN = append(l.SubjectsN, SubjectMatcher{Literal: sv})
		case map[string]any:
			pat, ok := sv["matches"].(string)
			if !ok || len(sv) != 1 {
				return fmt.Errorf("log subject %v: want a string or { matches = \"re\" }", v)
			}
			re, err := regexp.Compile(pat)
			if err != nil {
				return fmt.Errorf("log subject pattern %q: %w", pat, err)
			}
			l.SubjectsN = append(l.SubjectsN, SubjectMatcher{Pattern: re})
		default:
			return fmt.Errorf("log subject %v: want a string or { matches = \"re\" }", v)
		}
	}
	return nil
}

func normalizeFiles(raw map[string]any) (map[string]FileExpect, error) {
	if raw == nil {
		return nil, nil
	}
	out := make(map[string]FileExpect, len(raw))
	for path, v := range raw {
		switch fv := v.(type) {
		case string:
			out[path] = FileExpect{Content: fv, HasContent: true}
		case map[string]any:
			fe := FileExpect{}
			for k, kv := range fv {
				switch k {
				case "sha256":
					s, ok := kv.(string)
					if !ok || s == "" {
						return nil, fmt.Errorf("file %q: sha256 must be a non-empty string", path)
					}
					fe.SHA256 = s
				case "unchanged":
					b, ok := kv.(bool)
					if !ok || !b {
						return nil, fmt.Errorf("file %q: unchanged must be true", path)
					}
					fe.Unchanged = true
				case "absent":
					b, ok := kv.(bool)
					if !ok || !b {
						return nil, fmt.Errorf("file %q: absent must be true", path)
					}
					fe.Absent = true
				default:
					return nil, fmt.Errorf("file %q: unknown key %q", path, k)
				}
			}
			n := 0
			if fe.SHA256 != "" {
				n++
			}
			if fe.Unchanged {
				n++
			}
			if fe.Absent {
				n++
			}
			if n != 1 {
				return nil, fmt.Errorf("file %q: want exactly one of sha256/unchanged/absent", path)
			}
			out[path] = fe
		default:
			return nil, fmt.Errorf("file %q: want a string or a table", path)
		}
	}
	return out, nil
}

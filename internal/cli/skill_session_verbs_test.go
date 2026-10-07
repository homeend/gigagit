package cli

import (
	"bytes"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/agentskill"
	"github.com/homeend/gigagit/internal/domain"
)

// A command a skill tells an agent to run: `gg session <verb> [<sub>]` in
// inline code or at the start of a code line. Prose ("no gg session for this
// worktree") does not start a code span with gg.
var skillSessionVerb = regexp.MustCompile("(?m)(?:`|^\\s*)gg session ([a-z][a-z-]*)(?: ([a-z][a-z-]*))?")

// An inline code span may wrap between "gg" and "session" or after
// "session"; join it back before matching.
var skillSessionWrap = regexp.MustCompile(`\bgg(?:\s*\n\s*| )session\s*\n\s*|\bgg\s*\n\s*session\b`)

// sessionSubVerbs are the verbs that dispatch on a second word; for them the
// second word a skill names is checked too.
var sessionSubVerbs = map[string]bool{"note": true, "overview": true, "highlight": true, "files": true}

// Every `gg session <verb> [<sub>]` a skill names is one gg has: an agent
// following the skill must not hit an unknown verb (gg-review once said
// `gg session list`). A known sub-verb answers --help with its own flag
// usage ("Usage of session note add:" — an alias names its verb: `note
// clear` prints "note rm"); an unknown one prints the verb's usage or an
// error instead.
func TestSkillsNameOnlyRealSessionVerbs(t *testing.T) {
	t.Parallel()
	named := map[string][]string{} // "verb" or "verb sub" → skills
	for _, s := range agentskill.All() {
		body := skillSessionWrap.ReplaceAllStringFunc(s.Body(), func(m string) string {
			if strings.HasSuffix(strings.TrimSpace(m), "session") {
				return "gg session"
			}
			return "gg session "
		})
		for _, m := range skillSessionVerb.FindAllStringSubmatch(body, -1) {
			key := m[1]
			if sessionSubVerbs[m[1]] && m[2] != "" {
				key += " " + m[2]
			}
			named[key] = append(named[key], s.Name)
		}
	}
	if len(named) == 0 {
		t.Fatal("no `gg session <verb>` found in any skill — the pattern is broken")
	}
	keys := make([]string, 0, len(named))
	for k := range named {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	svc := domain.Open(newCLIRepo(t))
	for _, k := range keys {
		args := strings.Fields(k)
		var out, errb bytes.Buffer
		runSession(t.TempDir(), svc, append(args, "--help"), &out, &errb)
		got := errb.String()
		switch {
		case strings.HasPrefix(got, "session: unknown subcommand"):
			t.Errorf("skills %v name `gg session %s`, which gg does not have", named[k], args[0])
		case len(args) == 2 && !strings.HasPrefix(got, "Usage of session "+args[0]+" "):
			t.Errorf("skills %v name `gg session %s`, which gg does not have:\n%s", named[k], k, got)
		}
	}
}

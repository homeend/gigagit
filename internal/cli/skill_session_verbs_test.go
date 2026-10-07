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

// A command a skill tells an agent to run: `gg session <verb>` in inline code
// or at the start of a code line. Prose ("no gg session for this worktree")
// does not start a code span with gg.
var skillSessionVerb = regexp.MustCompile("(?m)(?:`|^\\s*)gg session ([a-z][a-z-]*)")

// Every `gg session <verb>` a skill names is one gg has: an agent following
// the skill must not hit "unknown subcommand" (gg-review once said
// `gg session list`).
func TestSkillsNameOnlyRealSessionVerbs(t *testing.T) {
	t.Parallel()
	verbs := map[string][]string{}
	for _, s := range agentskill.All() {
		for _, m := range skillSessionVerb.FindAllStringSubmatch(s.Body(), -1) {
			verbs[m[1]] = append(verbs[m[1]], s.Name)
		}
	}
	if len(verbs) == 0 {
		t.Fatal("no `gg session <verb>` found in any skill — the pattern is broken")
	}
	names := make([]string, 0, len(verbs))
	for v := range verbs {
		names = append(names, v)
	}
	sort.Strings(names)
	svc := domain.Open(newCLIRepo(t))
	for _, v := range names {
		var out, errb bytes.Buffer
		runSession(t.TempDir(), svc, []string{v, "--help"}, &out, &errb)
		if strings.HasPrefix(errb.String(), "session: unknown subcommand") {
			t.Errorf("skills %v name `gg session %s`, which gg does not have", verbs[v], v)
		}
	}
}

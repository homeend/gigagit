package domain

import (
	"strings"
	"testing"
)

// The user reads a brief's task; the rules are for the worker alone. A
// worker-only section runs from its `## ` heading to the next `#`/`## `
// heading, wherever it stands.
func TestSplitBriefKeepsRulesFromTheTask(t *testing.T) {
	t.Parallel()
	brief := strings.Join([]string{
		"# Fix the parser",
		"Source: X-1    Kind: fix",
		"",
		"## Context",
		"The parser drops the last line.",
		"",
		"## Rules",
		"- Do not touch the lexer.",
		"",
		"## Task",
		"- Find why [parse](a.go:4) drops it.",
		"",
		"## Do not:",
		"- push",
		"### still a rule",
		"",
		"## done WHEN",
		"- the test passes",
		"",
		"## Files",
		"- report: /tmp/r.md",
	}, "\n")
	task, rules := SplitBrief(brief)
	wantTask := "# Fix the parser\nSource: X-1    Kind: fix\n\n## Context\nThe parser drops the last line.\n\n## Task\n- Find why [parse](a.go:4) drops it.\n"
	if task != wantTask {
		t.Fatalf("task =\n%s\nwant\n%s", task, wantTask)
	}
	for _, want := range []string{"## Rules\n- Do not touch the lexer.", "## Do not:\n- push\n### still a rule", "## done WHEN\n- the test passes", "## Files\n- report: /tmp/r.md"} {
		if !strings.Contains(rules, want) {
			t.Errorf("rules lack %q:\n%s", want, rules)
		}
	}
	for _, leak := range []string{"lexer", "push", "test passes", "/tmp/r.md"} {
		if strings.Contains(task, leak) {
			t.Errorf("task shows the rule text %q", leak)
		}
	}
}

// A heading inside a code fence is text, and a brief with no worker-only
// section is all task.
func TestSplitBriefIgnoresFencesAndPlainBriefs(t *testing.T) {
	t.Parallel()
	brief := "# Goal\n```\n## Rules\nnot a heading\n```\nmore\n"
	if task, rules := SplitBrief(brief); task != brief || rules != "" {
		t.Fatalf("task %q rules %q", task, rules)
	}
	if task, rules := SplitBrief("## Rules\n- only rules\n"); task != "" || !strings.Contains(rules, "only rules") {
		t.Fatalf("task %q rules %q", task, rules)
	}
}

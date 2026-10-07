package agentskill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDogfoodSkillCopyInSync guards the repo's committed dogfood copy against
// drifting from the embedded skill: any using-gg.md or renderer change must be
// followed by `gg init --update` + committing the refreshed SKILL.md.
func TestDogfoodSkillCopyInSync(t *testing.T) {
	path := filepath.Join("..", "..", ".claude", "skills", "using-gg", "SKILL.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("dogfood copy not present (non-repo checkout?): %v", err)
	}
	if string(data) != SkillFile() {
		t.Error(".claude/skills/using-gg/SKILL.md is out of sync with the embedded skill — run `gg init --update` and commit the result")
	}
}

func TestBodyCoversTheCLISurface(t *testing.T) {
	b := Body()
	for _, want := range []string{
		"gg status", "gg commit", "gg pull", "gg push", "gg switch",
		"gg stash", "gg undo", "gg worktree", "gg repo", "gg inspect",
		"gg branch create", "gg branch delete",
		"--on-conflict", "--with-branch", "--force", "--branch",
		"non-interactive", "exit 1", "stderr",
		"--time-track",
		"gg diff --hunks", "gg note add", "gg note apply", "gg note list",
		"gg review --notes", "gg skill path", "gg_notes_list", "gg_note_add",
		"gg session navigate", "gg session status",
		"gg link", "gg link resolve", "gg://", "cursor.link",
	} {
		if !strings.Contains(b, want) {
			t.Errorf("body missing %q", want)
		}
	}
	if strings.Contains(b, "gg:using-gg") {
		t.Error("body must not contain markers (renderers add them)")
	}
}

func TestSkillFileHasFrontmatterAndMarker(t *testing.T) {
	s := SkillFile()
	if !strings.HasPrefix(s, "---\n") {
		t.Fatal("SkillFile must start with YAML frontmatter")
	}
	for _, want := range []string{"name: using-gg", "description: Use when",
		fmt.Sprintf("gg:using-gg:v%d", Version)} {
		if !strings.Contains(s, want) {
			t.Errorf("SkillFile missing %q", want)
		}
	}
	if !strings.Contains(s, Body()) {
		t.Error("SkillFile must contain the body verbatim")
	}
}

func TestPlainFileHasMarkerNoFrontmatter(t *testing.T) {
	s := PlainFile()
	if strings.HasPrefix(s, "---\n") {
		t.Fatal("PlainFile must not have YAML frontmatter")
	}
	if !strings.Contains(s, fmt.Sprintf("gg:using-gg:v%d", Version)) {
		t.Error("PlainFile missing version marker")
	}
}

func TestBlockIsDelimited(t *testing.T) {
	b := Block()
	if !strings.HasPrefix(b, fmt.Sprintf("<!-- gg:using-gg:v%d:begin -->", Version)) {
		t.Errorf("block begin marker wrong:\n%s", b[:80])
	}
	if !strings.HasSuffix(strings.TrimRight(b, "\n"), "<!-- gg:using-gg:end -->") {
		t.Error("block end marker missing")
	}
}

func TestInstalledVersionParsesAllForms(t *testing.T) {
	if got := InstalledVersion([]byte(SkillFile())); got != Version {
		t.Errorf("SkillFile version = %d, want %d", got, Version)
	}
	if got := InstalledVersion([]byte("x\n" + Block() + "\ny")); got != Version {
		t.Errorf("Block version = %d, want %d", got, Version)
	}
	if got := InstalledVersion([]byte("<!-- gg:using-gg:v3:begin -->")); got != 3 {
		t.Errorf("explicit v3 = %d, want 3", got)
	}
	if got := InstalledVersion([]byte("no marker here")); got != 0 {
		t.Errorf("no marker should be 0, got %d", got)
	}
}

func TestReviewSkillIdentityAndMarker(t *testing.T) {
	sk := ReviewingWithGG
	if sk.Name != "reviewing-with-gg" || sk.Version != ReviewVersion {
		t.Fatalf("skill identity = %q v%d", sk.Name, sk.Version)
	}
	file := sk.SkillFile()
	for _, want := range []string{
		"---\n", "name: reviewing-with-gg", "description: ",
		fmt.Sprintf("gg:reviewing-with-gg:v%d", sk.Version),
	} {
		if !strings.Contains(file, want) {
			t.Errorf("SkillFile missing %q", want)
		}
	}
	if !strings.Contains(file, sk.Body()) {
		t.Error("SkillFile must contain the body verbatim")
	}
	// The two skills must never recognise each other's markers, or init would
	// report one installed when the other is.
	if sk.HasMarker([]byte(UsingGG.Marker())) || UsingGG.HasMarker([]byte(sk.Marker())) {
		t.Error("each skill's marker must be recognised only by that skill")
	}
	if UsingGG.Name != "using-gg" || !strings.Contains(UsingGG.SkillFile(), "gg:using-gg:v") {
		t.Error("the using-gg marker text must not change — installed copies carry it")
	}
}

func TestReviewSkillBodyCoversTheNoteSurface(t *testing.T) {
	b := ReviewingWithGG.Body()
	for _, want := range []string{
		"gg diff --hunks", "gg note add", "gg note reply", "gg note apply --stdin",
		"gg note list", "gg note rm", "gg note clear",
		"--cached", "--rev", "--hunk", "--new-line", "--old-line",
		`"comments"`, `"newRange"`, "do NOT launch",
		"gg link", "gg://",
	} {
		if !strings.Contains(b, want) {
			t.Errorf("review skill body missing %q", want)
		}
	}
	if strings.Contains(b, "gg:reviewing-with-gg") {
		t.Error("body must not contain markers (renderers add them)")
	}
}

func TestAllReturnsEverySkill(t *testing.T) {
	got := All()
	if len(got) != 5 || got[0].Name != "using-gg" || got[1].Name != "reviewing-with-gg" || got[2].Name != "delegate" || got[3].Name != "gg-review" || got[4].Name != "gg-cross-review" {
		t.Fatalf("All() = %+v, want using-gg, reviewing-with-gg, delegate, gg-review, gg-cross-review", got)
	}
}

func TestGGReviewFrontmatter(t *testing.T) {
	t.Parallel()
	f := GGReview.SkillFile()
	for _, want := range []string{"name: gg-review\n", "argument-hint: \"<gg-link> [what to focus on]\"\n", "disable-model-invocation: true\n", "gg review save", "--dry-run"} {
		if !strings.Contains(f, want) {
			t.Fatalf("gg-review SKILL.md lacks %q", want)
		}
	}
	if strings.Contains(UsingGG.SkillFile(), "disable-model-invocation") {
		t.Fatal("only gg-review is user-invoked")
	}
}

func TestDogfoodDelegateSkillCopyInSync(t *testing.T) {
	path := filepath.Join("..", "..", ".claude", "skills", "delegate", "SKILL.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("dogfood copy missing — run `gg init --update` and commit it: %v", err)
	}
	if string(data) != Delegate.SkillFile() {
		t.Error(".claude/skills/delegate/SKILL.md is out of sync — run `gg init --update` and commit the result")
	}
}

// The delegate skill is the playbook both roles follow: the overseer's loop
// over the agent tools, and the worker protocol the kickoff line points at.
func TestDelegateSkillCoversBothRoles(t *testing.T) {
	b := Delegate.Body()
	for _, want := range []string{
		"## Overseer", "## Worker protocol",
		"agent_start", "agent_wait", "agent_screen", "agent_send", "agent_kill", "agent_task", "agent_report",
		"gg worktree list --free", "timed_out", "final",
	} {
		if !strings.Contains(b, want) {
			t.Errorf("delegate skill lacks %q", want)
		}
	}
}

func TestDogfoodReviewSkillCopyInSync(t *testing.T) {
	path := filepath.Join("..", "..", ".claude", "skills", "reviewing-with-gg", "SKILL.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("dogfood copy not present (non-repo checkout?): %v", err)
	}
	if string(data) != ReviewingWithGG.SkillFile() {
		t.Error(".claude/skills/reviewing-with-gg/SKILL.md is out of sync — run `gg init --agents claude-project` and commit the result")
	}
}

// TestRenderedFrontmatterIsPlainScalarSafe guards the one defect that makes a
// SKILL.md unparseable rather than merely ugly: SkillFile renders `name:` and
// `description:` as PLAIN (unquoted) YAML scalars. A plain scalar may not
// contain ": " (a mapping indicator — PyYAML reports "mapping values are not
// allowed here"), may not carry " #" (a comment indicator), and may not begin
// with a YAML indicator character. A skill whose frontmatter does not parse is
// dropped entirely by strict loaders (Claude Code, Junie, Kimi, Antigravity),
// so this must hold for EVERY skill, not just today's two.
func TestRenderedFrontmatterIsPlainScalarSafe(t *testing.T) {
	const indicators = `-?:,[]{}#&*!|>'"%@` + "`"
	for _, sk := range All() {
		for _, f := range []struct{ label, value string }{
			{"name", sk.Name},
			{"description", sk.Description},
		} {
			v := f.value
			switch {
			case v == "":
				t.Errorf("%s: %s is empty", sk.Name, f.label)
				continue
			case strings.ContainsRune(indicators, rune(v[0])):
				t.Errorf("%s: %s starts with the YAML indicator %q: %q", sk.Name, f.label, v[0], v)
			}
			if strings.Contains(v, ": ") || strings.HasSuffix(v, ":") {
				t.Errorf("%s: %s contains a YAML mapping indicator (\": \"): %q", sk.Name, f.label, v)
			}
			if strings.Contains(v, " #") {
				t.Errorf("%s: %s contains a YAML comment indicator (\" #\"): %q", sk.Name, f.label, v)
			}
			if strings.ContainsAny(v, "\n\r") {
				t.Errorf("%s: %s spans more than one line: %q", sk.Name, f.label, v)
			}
		}
		// Structural: exactly the opening ---, the two key lines plus the
		// skill's own extra lines (gg-review's), and the closing ---.
		head := strings.SplitN(sk.SkillFile(), "---\n", 3)
		if len(head) != 3 {
			t.Fatalf("%s: SkillFile has no closed frontmatter block", sk.Name)
		}
		want := "name: " + sk.Name + "\ndescription: " + sk.Description + "\n" + sk.front
		if head[1] != want {
			t.Errorf("%s: frontmatter body = %q, want %q", sk.Name, head[1], want)
		}
	}
}

// The review document is the one shape a gg review agent replies in: the
// skill teaches it (the engine's context document asks for the same).
func TestReviewSkillTeachesTheReviewDocument(t *testing.T) {
	b := ReviewingWithGG.Body()
	for _, want := range []string{"## Review document", `"summary"`, `"annotations"`, `"meta"`, `"oldRange"`, "$GG_MESSAGE_FILE"} {
		if !strings.Contains(b, want) {
			t.Errorf("review skill body missing %q", want)
		}
	}
	if strings.Contains(b, "sidecar") || strings.Contains(b, "GG_NOTES_FILE") {
		t.Error("the notes sidecar is gone")
	}
}

// Since agent docs reached gg web, overviews (like notes) work with only a
// gg web page live: the skill must not tell an agent they need a TUI.
func TestUsingGGOverviewsDoNotNeedATUI(t *testing.T) {
	for _, stale := range []string{"overviews need a gg TUI", "need a live gg TUI. When"} {
		if strings.Contains(UsingGG.Body(), stale) {
			t.Errorf("using-gg.md still says %q", stale)
		}
	}
}

func TestGGCrossReviewFrontmatter(t *testing.T) {
	t.Parallel()
	f := GGCrossReview.SkillFile()
	for _, want := range []string{"name: gg-cross-review\n", "argument-hint: \"<gg-link> [2|3] [what to focus on]\"\n", "disable-model-invocation: true\n",
		"gg review --tools --json", "--no-save --json", "--link", "--model", "## Disagreements resolved", "## Reviewers", "gg review save", "raised_by", "--dry-run"} {
		if !strings.Contains(f, want) {
			t.Fatalf("gg-cross-review SKILL.md lacks %q", want)
		}
	}
}

func TestDogfoodGGCrossReviewCopyInSync(t *testing.T) {
	path := filepath.Join("..", "..", ".claude", "skills", "gg-cross-review", "SKILL.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("dogfood copy missing — render it and commit: %v", err)
	}
	if string(data) != GGCrossReview.SkillFile() {
		t.Error(".claude/skills/gg-cross-review/SKILL.md is out of sync — render it and commit the result")
	}
}

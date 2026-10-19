package agentskill

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/notemeta"
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
	if len(got) != 6 || got[0].Name != "using-gg" || got[1].Name != "reviewing-with-gg" || got[2].Name != "gg-delegate" || got[3].Name != "gg-review" || got[4].Name != "gg-cross-review" || got[5].Name != "gg-overview" {
		t.Fatalf("All() = %+v, want using-gg, reviewing-with-gg, gg-delegate, gg-review, gg-cross-review, gg-overview", got)
	}
}

// delegate was renamed gg-delegate: init must still recognise the old name's
// copies to remove them, and a retired name never collides with a live one.
func TestRetiredNames(t *testing.T) {
	got := Retired()
	if len(got) != 1 || got[0].Name != "delegate" {
		t.Fatalf("Retired() = %+v, want delegate", got)
	}
	if !got[0].HasMarker([]byte("<!-- gg:delegate:v3 -->")) || got[0].HasMarker([]byte(GGDelegate.Marker())) {
		t.Fatal("the retired marker must match only the old name")
	}
	for _, r := range got {
		for _, s := range All() {
			if r.Name == s.Name {
				t.Fatalf("%s is both retired and installed", r.Name)
			}
		}
	}
}

// gg-overview is the name the user says ("use gg-overview to visualise the
// result"): the model may load it on its own, and its description carries
// the phrase so the agent connects it.
func TestGGOverviewSkill(t *testing.T) {
	t.Parallel()
	f := GGOverview.SkillFile()
	for _, want := range []string{"name: gg-overview\n", "argument-hint:", "gg-overview",
		"gg session note add", "gg session overview add", "gg session docs path", "agent_report"} {
		if !strings.Contains(f, want) {
			t.Fatalf("gg-overview SKILL.md lacks %q", want)
		}
	}
	if strings.Contains(f, "disable-model-invocation") {
		t.Fatal("gg-overview must be loadable on the model's own initiative")
	}
	if !strings.Contains(GGOverview.Description, "gg-overview") {
		t.Fatal("the description must name the phrase the user says")
	}
}

func TestDogfoodGGOverviewCopyInSync(t *testing.T) {
	path := filepath.Join("..", "..", ".claude", "skills", "gg-overview", "SKILL.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("dogfood copy missing — run `gg init --update` and commit it: %v", err)
	}
	if string(data) != GGOverview.SkillFile() {
		t.Error(".claude/skills/gg-overview/SKILL.md is out of sync — run `gg init --update` and commit the result")
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
	path := filepath.Join("..", "..", ".claude", "skills", "gg-delegate", "SKILL.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("dogfood copy missing — run `gg init --update` and commit it: %v", err)
	}
	if string(data) != GGDelegate.SkillFile() {
		t.Error(".claude/skills/gg-delegate/SKILL.md is out of sync — run `gg init --update` and commit the result")
	}
}

// The delegate skill is the playbook both roles follow: the overseer's loop
// over the agent tools, and the worker protocol the kickoff line points at.
func TestDelegateSkillCoversBothRoles(t *testing.T) {
	b := GGDelegate.Body()
	for _, want := range []string{
		"## Overseer", "## Worker protocol",
		"agent_start", "agent_wait", "agent_screen", "agent_send", "agent_kill", "agent_task", "agent_report",
		"gg worktree list --free", "timed_out", "final",
	} {
		if !strings.Contains(b, want) {
			t.Errorf("gg-delegate skill lacks %q", want)
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
	for _, sk := range append(All(), Experimental()...) {
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
		"gg review --tools --json", "--no-save --json", "--link", "--model", "## Disagreements resolved", "## Reviewers", "gg review save", "raised_by", "--dry-run", `--model "<model>"`, "<dir>/r1.json"} {
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

// The default display block a skill teaches must be one gg accepts whole
// (spec §9: "A test parses the default block from the skill text") and the
// same block the engine's review prompt carries.
func TestDefaultDisplayBlockInSkillsIsValid(t *testing.T) {
	var want any
	if err := json.Unmarshal([]byte(engine.DefaultReviewDisplay), &want); err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, s := range All() {
		for _, block := range jsonBlocks(s.Body()) {
			var doc struct {
				Display json.RawMessage `json:"display"`
			}
			if json.Unmarshal([]byte(block), &doc) != nil || len(doc.Display) == 0 || strings.HasPrefix(strings.TrimSpace(block), "{\n  \"version\"") {
				continue
			}
			found++
			if _, probs, err := notemeta.ParseDisplay(doc.Display); err != nil || len(probs) > 0 {
				t.Errorf("%s: the display block is not clean: %v %v", s.Name, err, probs)
			}
			var got any
			_ = json.Unmarshal(doc.Display, &got)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("%s: the display block differs from engine.DefaultReviewDisplay", s.Name)
			}
		}
	}
	if found == 0 {
		t.Fatal("no skill teaches the default display block")
	}
	for _, s := range All() {
		if strings.Contains(s.Body(), "bug | risk | design | nit") || strings.Contains(s.Body(), "bug > risk") {
			t.Errorf("%s still teaches the old severity vocabulary", s.Name)
		}
	}
}

// jsonBlocks are the bodies of the ```json fences in md.
func jsonBlocks(md string) []string {
	var out []string
	for _, part := range strings.Split(md, "```json\n")[1:] {
		if body, _, ok := strings.Cut(part, "\n```"); ok {
			out = append(out, body)
		}
	}
	return out
}

// Every skill that has the agent write text the user reads (notes, reviews,
// overviews, briefs, reports) names the writing standard for that text.
func TestUserTextSkillsNameSTE100(t *testing.T) {
	for _, sk := range []Skill{UsingGG, ReviewingWithGG, GGDelegate, GGReview, GGCrossReview, GGOverview} {
		if !strings.Contains(sk.Body(), "ASD-STE100 (80% compatibility when possible)") {
			t.Errorf("%s: missing the ASD-STE100 writing rule", sk.Name)
		}
	}
}

// The experimental set is installed only by `gg init --experimental`: v2
// copies of the overview and review skills the user compares with v1. Each
// is user-invoked (a v2 must never be picked instead of its v1 on the
// model's own initiative) and names never collide with a live or retired one.
func TestExperimentalSkills(t *testing.T) {
	got := Experimental()
	want := []string{"gg-overview-v2", "gg-review-v2", "gg-cross-review-v2"}
	if len(got) != len(want) {
		t.Fatalf("Experimental() = %d skills, want %v", len(got), want)
	}
	taken := map[string]bool{}
	for _, s := range append(All(), Retired()...) {
		taken[s.Name] = true
	}
	for i, sk := range got {
		if sk.Name != want[i] {
			t.Fatalf("Experimental()[%d] = %s, want %s", i, sk.Name, want[i])
		}
		if taken[sk.Name] {
			t.Fatalf("%s collides with a live or retired skill", sk.Name)
		}
		f := sk.SkillFile()
		for _, w := range []string{"name: " + sk.Name + "\n", "disable-model-invocation: true\n", "argument-hint:", "EXPERIMENTAL", "/" + sk.Name, "gg init --experimental"} {
			if !strings.Contains(f, w) {
				t.Errorf("%s SKILL.md lacks %q", sk.Name, w)
			}
		}
		if !strings.Contains(sk.Body(), "ASD-STE100 (80% compatibility when possible)") {
			t.Errorf("%s: missing the ASD-STE100 writing rule", sk.Name)
		}
		if sk.Version < 1 {
			t.Errorf("%s: version %d", sk.Name, sk.Version)
		}
	}
}

// Every command an experimental skill teaches is one this gg has: the
// v1 gg-overview's `gg show <sha>:<path>` is refused by gg show.
func TestExperimentalSkillsTeachRealCommands(t *testing.T) {
	for _, sk := range append(All(), Experimental()...) {
		if strings.Contains(sk.Body(), "gg show <sha>:<path>") {
			t.Errorf("%s teaches `gg show <sha>:<path>`, which gg show refuses", sk.Name)
		}
	}
	for _, w := range []string{"## Read before you write", "**Before → after**", "**New terms**", "**Changed meaning**", "**Contracts**", "(inferred)", "**Scale it to the change.**", "gg link text", ":old:"} {
		if !strings.Contains(GGOverviewV2.Body(), w) {
			t.Errorf("gg-overview-v2 lacks %q", w)
		}
	}
	for _, sk := range []Skill{GGReviewV2, GGCrossReviewV2} {
		for _, w := range []string{"gg-overview-v2", "gg skill path gg-overview-v2", "Mechanical: <files>", "before → after", "gg log -n 50 <diff>", "gg pr view <n>"} {
			if !strings.Contains(sk.Body(), w) {
				t.Errorf("%s lacks %q", sk.Name, w)
			}
		}
	}
}

func TestDogfoodExperimentalCopiesInSync(t *testing.T) {
	for _, sk := range Experimental() {
		path := filepath.Join("..", "..", ".claude", "skills", sk.Name, "SKILL.md")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("dogfood copy missing — run `gg init --experimental --agents claude-project` and commit it: %v", err)
		}
		if string(data) != sk.SkillFile() {
			t.Errorf(".claude/skills/%s/SKILL.md is out of sync — run `gg init --update` and commit the result", sk.Name)
		}
	}
}

// An agent copies an example's backticks: a link written in backticks is a
// code span, so the overview it writes opens nothing (seen with Junie). No
// skill shows an anchor in backticks outside a fenced block, and every skill
// that teaches anchors says to write them bare.
func TestSkillsWriteAnchorsBare(t *testing.T) {
	backticked := regexp.MustCompile("`\\[[^\\]`\\n]*\\]\\([^)`\\s]+\\)`")
	for _, sk := range append(All(), Experimental()...) {
		fence := false
		for i, line := range strings.Split(sk.Body(), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				fence = !fence
			}
			if !fence && backticked.MatchString(line) {
				t.Errorf("%s:%d shows an anchor in backticks: %s", sk.Name, i+1, line)
			}
		}
		if strings.Contains(sk.Body(), "](") && !strings.Contains(sk.Body(), "Backticks AROUND a link make it plain text") {
			t.Errorf("%s teaches anchors but not the bare-link rule", sk.Name)
		}
	}
}

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

func TestTemplateAddListShowRenderRemove(t *testing.T) {
	dir := prefixRepo(t)

	code, out, errb := runCLIStdin(t, dir, "Hi <user:name>,\n<br> on <branch>\n", "template", "add", "--title", "Greeting", "-F", "-")
	if code != 0 || strings.TrimSpace(out) != "greeting" {
		t.Fatalf("add exit %d out %q err %s", code, out, errb)
	}
	if _, out, _ = runCLI(t, dir, "template", "list"); !strings.Contains(out, "greeting\tglobal\tGreeting") {
		t.Fatalf("list = %q", out)
	}
	if _, out, _ = runCLI(t, dir, "templates"); !strings.Contains(out, "greeting") {
		t.Fatalf("bare templates = %q", out)
	}
	if _, out, _ = runCLI(t, dir, "template", "show", "greet"); !strings.Contains(out, "Hi <user:name>,") || !strings.Contains(out, "variables: name") || !strings.Contains(out, "automatic: <branch>") {
		t.Fatalf("show = %q", out)
	}
	code, _, errb = runCLI(t, dir, "template", "render", "greeting")
	if code != 2 || !strings.Contains(errb, "--set name=") {
		t.Fatalf("render without --set: exit %d err %q", code, errb)
	}
	code, out, errb = runCLI(t, dir, "template", "render", "greeting", "--set", "name=Ann")
	if code != 0 || !strings.HasPrefix(out, "Hi Ann,\n<br> on ") || !strings.HasSuffix(out, "\n") || strings.HasSuffix(out, "\n\n") {
		t.Fatalf("render exit %d out %q err %s", code, out, errb)
	}
	if code, _, _ = runCLI(t, dir, "template", "rm", "greeting"); code != 0 {
		t.Fatalf("rm exit %d", code)
	}
	if _, out, _ = runCLI(t, dir, "template", "list"); strings.Contains(out, "greeting") {
		t.Fatalf("still listed: %q", out)
	}
}

func TestTemplateEditAndFile(t *testing.T) {
	dir := prefixRepo(t)
	f := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(f, []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, errb := runCLI(t, dir, "template", "add", "--global", "--title", "Note", "-F", f); code != 0 {
		t.Fatalf("add: %s", errb)
	}
	if _, out, _ := runCLI(t, dir, "template", "list"); !strings.Contains(out, "note\tglobal\tNote") {
		t.Fatalf("list = %q", out)
	}
	if code, _, errb := runCLIStdin(t, dir, "two\n", "template", "edit", "note", "--global", "-F", "-"); code != 0 {
		t.Fatalf("edit body: %s", errb)
	}
	code, out, errb := runCLI(t, dir, "template", "edit", "note", "--title", "Memo")
	if code != 0 || strings.TrimSpace(out) != "memo" {
		t.Fatalf("rename exit %d out %q err %s", code, out, errb)
	}
	if _, out, _ = runCLI(t, dir, "template", "render", "memo"); out != "two\n" {
		t.Fatalf("render = %q", out)
	}
}

func TestTemplateSeqPeekAndTake(t *testing.T) {
	dir := prefixRepo(t)
	runCLIStdin(t, dir, "#<seq:cli:2>", "template", "add", "--title", "Seq", "-F", "-")
	for i := 0; i < 2; i++ {
		if _, out, _ := runCLI(t, dir, "template", "render", "seq", "--peek"); out != "#01\n" {
			t.Fatalf("peek %d = %q", i, out)
		}
	}
	if _, out, _ := runCLI(t, dir, "template", "render", "seq"); out != "#01\n" {
		t.Fatalf("take = %q", out)
	}
	if _, out, _ := runCLI(t, dir, "template", "render", "seq", "--peek"); out != "#02\n" {
		t.Fatalf("after take = %q", out)
	}
}

func TestTemplateUsageErrors(t *testing.T) {
	dir := prefixRepo(t)
	for _, args := range [][]string{
		{"template"}, {"template", "nope"}, {"template", "add"}, {"template", "add", "--title", "x"},
		{"template", "show"}, {"template", "render", "missing"}, {"template", "edit", "x"},
		{"template", "rm"}, {"template", "rm", "missing"},
	} {
		if code, _, _ := runCLI(t, dir, args...); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
	if code, _, errb := runCLIStdin(t, dir, "x <seq> y", "template", "add", "--title", "Bad", "-F", "-"); code != 1 || !strings.Contains(errb, "seq") {
		t.Fatalf("malformed token: exit %d err %q", code, errb)
	}
	if code, _, _ := runCLI(t, dir, "template", "add", "--title", "Gone", "-F", filepath.Join(t.TempDir(), "absent.md")); code != 1 {
		t.Fatalf("unreadable body file: exit %d, want 1", code)
	}
}

// A relative -F path is taken from the directory the command runs against.
func TestTemplateAddRelativeFile(t *testing.T) {
	dir := prefixRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "body.md"), []byte("rel\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, errb := runCLI(t, dir, "template", "add", "--title", "Rel", "-F", "body.md"); code != 0 {
		t.Fatalf("add: %s", errb)
	}
	if _, out, _ := runCLI(t, dir, "template", "render", "rel"); out != "rel\n" {
		t.Fatalf("render = %q", out)
	}
}

// A new template is global unless --repo: the TUI's and the web's default.
func TestTemplateAddDefaultsToGlobal(t *testing.T) {
	dir := prefixRepo(t)
	if code, _, errb := runCLIStdin(t, dir, "g", "template", "add", "--title", "Everywhere", "-F", "-"); code != 0 {
		t.Fatalf("add: %s", errb)
	}
	if code, _, errb := runCLIStdin(t, dir, "g", "template", "add", "--global", "--title", "Also global", "-F", "-"); code != 0 {
		t.Fatalf("add --global: %s", errb)
	}
	if code, _, errb := runCLIStdin(t, dir, "r", "template", "add", "--repo", "--title", "Here only", "-F", "-"); code != 0 {
		t.Fatalf("add --repo: %s", errb)
	}
	_, out, _ := runCLI(t, dir, "template", "list")
	for _, want := range []string{"everywhere\tglobal\t", "also-global\tglobal\t", "here-only\trepo\t"} {
		if !strings.Contains(out, want) {
			t.Errorf("list misses %q:\n%s", want, out)
		}
	}
	if code, _, _ := runCLIStdin(t, dir, "x", "template", "add", "--repo", "--global", "--title", "Both", "-F", "-"); code != 2 {
		t.Fatalf("--repo with --global: exit %d, want 2", code)
	}
}

func TestTemplateListRefusesArguments(t *testing.T) {
	dir := prefixRepo(t)
	for _, args := range [][]string{{"template", "list", "junk"}, {"templates", "junk"}, {"template", "ls", "--nope"}} {
		code, out, errb := runCLI(t, dir, args...)
		if code != 2 || out != "" || !strings.Contains(errb, "usage: gg template list") {
			t.Errorf("%v: exit %d out %q err %q", args, code, out, errb)
		}
	}
}

// A --set label the template does not ask for is a typo, not a no-op.
func TestTemplateRenderRefusesUnknownSetLabel(t *testing.T) {
	dir := prefixRepo(t)
	runCLIStdin(t, dir, "Hi <user:name> #<seq:unk>", "template", "add", "--title", "Hi", "-F", "-")
	code, out, errb := runCLI(t, dir, "template", "render", "hi", "--set", "name=Ann", "--set", "nmae=Bob")
	if code != 2 || out != "" || !strings.Contains(errb, "nmae") || !strings.Contains(errb, "name") {
		t.Fatalf("exit %d out %q err %q", code, out, errb)
	}
	// The refused render consumed no counter.
	if _, out, _ := runCLI(t, dir, "template", "render", "hi", "--set", "name=Ann", "--peek"); out != "Hi Ann #1\n" {
		t.Fatalf("after the refusal = %q", out)
	}
}

func TestTemplateTitleWithTabRefused(t *testing.T) {
	dir := prefixRepo(t)
	code, _, errb := runCLIStdin(t, dir, "x", "template", "add", "--title", "a\tb", "-F", "-")
	if code == 0 || !strings.Contains(errb, "tab") {
		t.Fatalf("exit %d err %q", code, errb)
	}
}

// -F stops reading once the input cannot be a valid text: an endless stdin
// ends in the size error instead of filling memory.
func TestTemplateReadBodyIsBounded(t *testing.T) {
	body, err := readBody("", "-", endless{})
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != domain.MaxTextTemplateBody+1 {
		t.Fatalf("read %d bytes, want the cap + 1", len(body))
	}
	f := filepath.Join(t.TempDir(), "big.md")
	if err := os.WriteFile(f, []byte(strings.Repeat("x", domain.MaxTextTemplateBody*3)), 0o600); err != nil {
		t.Fatal(err)
	}
	if body, err = readBody("", f, nil); err != nil || len(body) != domain.MaxTextTemplateBody+1 {
		t.Fatalf("file: read %d bytes, %v", len(body), err)
	}
	dir := prefixRepo(t)
	code, _, errb := runCLI(t, dir, "template", "add", "--title", "Big", "-F", f)
	if code == 0 || !strings.Contains(errb, "larger than") {
		t.Fatalf("oversized add: exit %d err %q", code, errb)
	}
}

// endless is a reader that never ends.
type endless struct{}

func (endless) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

// The id hint is the CLI's to give: an unknown id names the listing command.
func TestTemplateUnknownIDHintsAtList(t *testing.T) {
	dir := prefixRepo(t)
	code, _, errb := runCLI(t, dir, "template", "show", "nope")
	if code != 2 || !strings.Contains(errb, "gg template list") {
		t.Fatalf("exit %d err %q", code, errb)
	}
}

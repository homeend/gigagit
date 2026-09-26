package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

func TestLinkContentPrintsTheContentLink(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	code, out, errb := runLinkCLI(t, dir, "--content", "README.md")
	if code != 0 {
		t.Fatalf("exit = %d (stderr %q)", code, errb)
	}
	got := strings.TrimSpace(out)
	if !strings.HasSuffix(got, "/README.md?view=content") {
		t.Fatalf("stdout = %q, want …/README.md?view=content", got)
	}
	l, err := model.ParseLink(got)
	if err != nil || !l.IsContent() {
		t.Fatalf("ParseLink(%q) = %+v, %v — want a content link", got, l, err)
	}
}

func TestLinkContentMissingFileExits1(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	if err := os.Remove(filepath.Join(dir, "README.md")); err != nil {
		t.Fatal(err)
	}
	code, out, errb := runLinkCLI(t, dir, "--content", "README.md")
	if code != 1 || out != "" || !strings.Contains(errb, "README.md is not in the working tree") {
		t.Fatalf("exit=%d stdout=%q stderr=%q, want 1 and the not-in-working-tree message", code, out, errb)
	}
}

func TestLinkContentUsageErrors(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	for _, args := range [][]string{
		{"--content"},                    // no path
		{"--content", "README.md:old:1"}, // a content link has no old side
		{"--content", "README.md#1"},     // never a hunk
		{"--content", "--cached", "README.md"},
		{"--content", "--rev", "HEAD", "README.md"},
		{"--content", "--ref", "main", "README.md"},
		{"--content", "--pair", "HEAD..HEAD", "README.md"},
		{"--content", "--bookmark", "b1", "README.md"}, // one landing
	} {
		if code, _, errb := runLinkCLI(t, dir, args...); code != 2 {
			t.Errorf("gg link %v: exit = %d (stderr %q), want 2", args, code, errb)
		}
	}
}

// Every link-taking verb that is not a navigate refuses a content link: it
// names a file's content, and diffing or anchoring on it would silently mean
// the working-tree diff instead.
func TestContentLinkRefusedByNonNavigateVerbs(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	link := "gg://" + filepath.ToSlash(dir) + "/README.md?view=content"
	svc := domain.Open(dir)
	for _, verb := range []string{"diff", "show", "anchor", "highlight"} {
		_, err := resolveLinkArg(t.Context(), svc, link, linkShapes{Ref: true, Pair: true}, verb)
		if err == nil || !strings.Contains(err.Error(), "content link") {
			t.Errorf("%s: resolveLinkArg = %v, want the content-link refusal", verb, err)
		}
	}
	if _, err := resolveLinkArg(t.Context(), svc, link, linkShapes{Ref: true, Pair: true, Content: true}, "open"); err != nil {
		t.Errorf("open: resolveLinkArg = %v, want the content link accepted", err)
	}
}

// gg web has a viewer now: --web no longer refuses a content link. With no
// live page and no launcher the open says so instead.
func TestOpenWebAcceptsAContentLink(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	var out, errb bytes.Buffer
	link := "gg://" + filepath.ToSlash(dir) + "/README.md?view=content"
	code := cmdOpen(domain.Open(dir), []string{"--web", link}, &out, &errb)
	if code != 1 || strings.Contains(errb.String(), "not supported in gg web") || !strings.Contains(errb.String(), "no live gg web page") {
		t.Fatalf("exit=%d stderr=%q, want 1 and no refusal", code, errb.String())
	}
}

func TestLinkContentWithLine(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	code, out, errb := runLinkCLI(t, dir, "--content", "README.md:1")
	if code != 0 {
		t.Fatalf("exit = %d (stderr %q)", code, errb)
	}
	got := strings.TrimSpace(out)
	if !strings.HasSuffix(got, "/README.md:1?view=content") {
		t.Fatalf("stdout = %q, want …/README.md:1?view=content", got)
	}
	l, err := model.ParseLink(got)
	if err != nil || !l.IsContent() || l.Line != 1 {
		t.Fatalf("ParseLink(%q) = %+v, %v — want a content link at line 1", got, l, err)
	}
}

// The file may be read back by an agent: a line the file does not have is a
// link to nowhere, refused before it is printed.
func TestLinkContentLinePastEOFExits1(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "x.txt"), []byte("one\ntwo\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errb := runLinkCLI(t, dir, "--content", "x.txt:3")
	if code != 1 || out != "" || !strings.Contains(errb, "x.txt has 2 lines") {
		t.Fatalf("exit=%d stdout=%q stderr=%q, want 1 and \"x.txt has 2 lines\"", code, out, errb)
	}
	if code, _, errb := runLinkCLI(t, dir, "--content", "x.txt:2"); code != 0 {
		t.Fatalf("x.txt:2 exit = %d (stderr %q), want 0", code, errb)
	}
}

// contentLineCount counts the way the viewer splits: trailing newlines are
// not lines, CRLF and a lone CR are one break each.
func TestContentLineCount(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]int{
		"": 0, "a": 1, "a\n": 1, "a\n\n\n": 1, "a\r\nb\r\n": 2, "a\rb": 2, "a\n\nb": 3,
	} {
		if got := contentLineCount([]byte(in)); got != want {
			t.Errorf("contentLineCount(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestOpenBackgroundRefusals(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	content := "gg://" + filepath.ToSlash(dir) + "/README.md?view=content"
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--background", "gg://" + filepath.ToSlash(dir) + "/README.md"}, "--background needs a content link"},
		{[]string{"--background", "--web", content}, "--background and --web do not mix"},
	} {
		var out, errb bytes.Buffer
		if code := cmdOpen(domain.Open(dir), tc.args, &out, &errb); code != 2 || !strings.Contains(errb.String(), tc.want) {
			t.Errorf("%v: exit=%d stderr=%q, want 2 and %q", tc.args, code, errb.String(), tc.want)
		}
	}
}

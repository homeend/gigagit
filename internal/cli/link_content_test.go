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
		{"--content"},                // no path
		{"--content", "README.md:3"}, // a line is v2
		{"--content", "README.md#1"}, // never a hunk
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

func TestOpenWebRefusesAContentLink(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	var out, errb bytes.Buffer
	link := "gg://" + filepath.ToSlash(dir) + "/README.md?view=content"
	code := cmdOpen(domain.Open(dir), []string{"--web", link}, &out, &errb)
	if code != 2 || !strings.Contains(errb.String(), "content links are not supported in gg web yet") {
		t.Fatalf("exit=%d stderr=%q, want 2 and the web refusal", code, errb.String())
	}
}

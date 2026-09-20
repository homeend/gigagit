package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

type linkBase struct{ Kind, Base, Why, Link, Error string }

func baseURL(link, base string) string {
	v := url.Values{"link": {link}}
	if base != "" {
		v.Set("base", base)
	}
	return "/api/link-base?" + v.Encode()
}

func refTarget(name string) model.LinkTarget {
	return model.LinkTarget{State: model.StateCommitted, Ref: name}
}

// A ref and a commit, on one fixture, must rewrite DIFFERENTLY — and a ref
// TARGET FIRST. The assertions are on the link TEXT.
func TestLinkBaseRewritesTargetFirst(t *testing.T) {
	dir, mainSha, featSha := linkRepo(t)
	ts := linkServe(t, dir)

	ref := localLink(dir, "", refTarget("feat/x"))
	var sug linkBase
	if code := getAny(t, ts, baseURL(ref, ""), &sug); code != http.StatusOK || sug.Kind != "ref" || sug.Base != "main" || sug.Why != "trunk" {
		t.Fatalf("ref suggestion = %+v (%d)", sug, code)
	}
	var out linkBase
	getAny(t, ts, baseURL(ref, "main"), &out)
	want := localLink(dir, "", model.LinkTarget{State: model.StateCommitted, Preview: &model.LinkPreview{Source: "feat/x", Target: "main"}})
	if out.Link != want || !strings.HasSuffix(out.Link, "@main...feat/x") {
		t.Fatalf("ref rewrite = %q, want %q", out.Link, want)
	}

	// The commit arm, typed ABBREVIATED: the full sha must come from the server.
	commit := localLink(dir, "", commitTarget(featSha[:9]))
	sug = linkBase{}
	if getAny(t, ts, baseURL(commit, ""), &sug); sug.Kind != "commit" || sug.Base != mainSha || sug.Why != "parent" {
		t.Fatalf("commit suggestion = %+v, want parent %s", sug, mainSha)
	}
	out = linkBase{}
	getAny(t, ts, baseURL(commit, sug.Base), &out)
	if !strings.HasSuffix(out.Link, "@"+mainSha+".."+featSha) {
		t.Fatalf("commit rewrite = %q, want …@%s..%s", out.Link, mainSha, featSha)
	}
}

// The pure BoundKind says "ref" for this link; only a LOCATE knows it names
// a file, which no base can bound.
func TestLinkBaseUsesTheLocatedKind(t *testing.T) {
	dir, _, _ := linkRepo(t)
	ts := linkServe(t, dir)
	text := localLink(dir, "a.txt", refTarget("feat/x"))
	l, err := model.ParseLink(text)
	if err != nil || l.BoundKind() != model.LinkBoundRef {
		t.Fatalf("fixture: the pure kind must DISAGREE (%v, %v), or this test sees nothing", l.BoundKind(), err)
	}
	var sug linkBase
	if code := getAny(t, ts, baseURL(text, ""), &sug); code != http.StatusOK || sug.Kind != "none" {
		t.Fatalf("kind = %q (%d), want none", sug.Kind, code)
	}
	var out linkBase
	if code := getAny(t, ts, baseURL(text, "main"), &out); code != http.StatusUnprocessableEntity || out.Link != "" {
		t.Fatalf("a file link must not be rewritten: %d %+v", code, out)
	}
}

func TestLinkBaseRefusals(t *testing.T) {
	dir, mainSha, featSha := linkRepo(t)
	ts := linkServe(t, dir)
	ref := localLink(dir, "", refTarget("feat/x"))
	commit := localLink(dir, "", commitTarget(featSha))
	for _, c := range []struct {
		name, url string
		code      int
		kind      string
	}{
		{"no link", "/api/link-base", http.StatusBadRequest, ""},
		{"unparseable, still typing", baseURL("gg://r@not a target", ""), http.StatusOK, "none"},
		{"a sha that does not resolve yet", baseURL(localLink(dir, "", commitTarget("deadbeef1")), ""), http.StatusOK, "none"},
		{"the working tree has no base", baseURL(localLink(dir, "", model.LinkTarget{}), ""), http.StatusOK, "none"},
		{"unparseable, rewrite asked", baseURL("gg://r@not a target", "main"), http.StatusUnprocessableEntity, ""},
		{"a ref bounded by itself", baseURL(ref, "feat/x"), http.StatusUnprocessableEntity, ""},
		{"a commit with a SHORT base", baseURL(commit, mainSha[:9]), http.StatusUnprocessableEntity, ""},
		{"a commit with a ref base", baseURL(commit, "main"), http.StatusUnprocessableEntity, ""},
	} {
		var got linkBase
		if code := getAny(t, ts, c.url, &got); code != c.code || got.Kind != c.kind || got.Link != "" {
			t.Errorf("%s: code=%d kind=%q link=%q err=%q, want %d %q", c.name, code, got.Kind, got.Link, got.Error, c.code, c.kind)
		}
	}
}

package domain

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// A ?preview= hint describes as the SAVED entry's label when this store holds
// it, and falls through to the address when it does not — never a bare id
// nobody can read. The two outcomes are made to disagree on ONE link text:
// the same link, before and after the entry is removed.
func TestDescribeLinkPreviewHint(t *testing.T) {
	t.Parallel()
	dir, svc := previewRepo(t)
	ctx := context.Background()
	pv, err := svc.PreviewAdd(ctx, "feat/x", "main", "login rework")
	if err != nil {
		t.Fatal(err)
	}
	pr, err := svc.PairAdd(ctx, "main", "feat/x", "the frozen pair")
	if err != nil {
		t.Fatal(err)
	}
	a, b := revOf(t, dir, "main"), revOf(t, dir, "feat/x")
	abs := localLinkRoot(t, svc)
	for _, c := range []struct{ name, text, saved string }{
		{"a merge preview, name form", "gg://r@main...feat/x?preview=" + pv.ID, "preview: login rework"},
		{"a merge preview, local form", "gg://" + abs + "@main...feat/x?preview=" + pv.ID, "preview: login rework"},
		{"a commit pair", "gg://r@" + a + ".." + b + "?preview=" + pr.ID, "pair: the frozen pair"},
	} {
		l, err := model.ParseLink(c.text)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := svc.DescribeLink(ctx, l); got != c.saved {
			t.Errorf("%s: saved desc = %q, want %q", c.name, got, c.saved)
		}
	}
	if err := svc.PreviewRemove(ctx, pv.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.PairRemove(ctx, pr.ID); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, text, gone string }{
		{"a merge preview", "gg://r@main...feat/x?preview=" + pv.ID, "preview: main...feat/x"},
		{"a commit pair", "gg://r@" + a + ".." + b + "?preview=" + pr.ID, "link: "},
	} {
		l, _ := model.ParseLink(c.text)
		if got := svc.DescribeLink(ctx, l); !strings.HasPrefix(got, c.gone) {
			t.Errorf("%s: desc once the entry is gone = %q, want the address form %q…", c.name, got, c.gone)
		}
	}
}

// An address-less ?preview= link names nothing: the hint carries no content
// of its own (unlike a shelved working-tree file), so it is refused outright.
func TestResolveLinkRefusesAnAddresslessPreviewHint(t *testing.T) {
	t.Parallel()
	_, svc := previewRepo(t)
	ctx := context.Background()
	pv, err := svc.PreviewAdd(ctx, "feat/x", "main", "login rework")
	if err != nil {
		t.Fatal(err)
	}
	l, err := model.ParseLink("gg://" + localLinkRoot(t, svc) + "?preview=" + pv.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ResolveLink(ctx, l, ResolveOpts{Cwd: svc})
	if !errors.Is(err, model.ErrLink) || !strings.Contains(err.Error(), "a preview hint needs the set it names") {
		t.Fatalf("err = %v, want a refusal that says what is missing", err)
	}
}

// localLinkRoot is the checkout spelled the way a local-form link spells it
// (a Windows drive path renders as gg:///C:/…).
func localLinkRoot(t *testing.T, svc *Service) string {
	t.Helper()
	top, err := svc.TopLevel(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	abs := filepath.ToSlash(top)
	if !strings.HasPrefix(abs, "/") {
		abs = "/" + abs
	}
	return abs
}

package domain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/agentdocs"
	"github.com/homeend/gigagit/internal/model"
)

const sessOvID = "20261028T150405-ab12"

// sessOvRepo is a committed repo (its real path) and a service on it.
func sessOvRepo(t *testing.T) (string, *Service) {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("1\n2\n3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "a")
	return dir, Open(dir)
}

// writeSessOv lands overview id (title) in docs dir.
func writeSessOv(t *testing.T, dir, id, title string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := agentdocs.EncodeOverviewFile(agentdocs.OverviewFile{Format: agentdocs.DocFormat, Title: title, Created: time.Now(), Text: "See [a](a.go:2).\n"})
	if err != nil {
		t.Fatal(err)
	}
	if err := agentdocs.WriteDoc(dir, id, agentdocs.KindOverview, data, false); err != nil {
		t.Fatal(err)
	}
}

// docsByCheckout is a SessionDocs seam: one docs dir per checkout, under base.
func docsByCheckout(base string) func(commonDir, checkout string) string {
	return func(_, checkout string) string {
		return filepath.Join(base, filepath.Base(checkout))
	}
}

func resolveText(t *testing.T, text string, opts ResolveOpts) (Resolved, error) {
	t.Helper()
	l, err := model.ParseLink(text)
	if err != nil {
		t.Fatalf("parse %q: %v", text, err)
	}
	return ResolveLink(context.Background(), l, opts)
}

func TestSessionOverviewLinkRoundTrip(t *testing.T) {
	t.Parallel()
	top, svc := sessOvRepo(t)
	docs := t.TempDir()
	writeSessOv(t, filepath.Join(docs, filepath.Base(top)), sessOvID, "T")
	text, err := SessionOverviewLink(top, sessOvID)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && text != "gg://"+top+"?overview="+sessOvID {
		t.Fatalf("link = %q", text)
	}
	l, _ := model.ParseLink(text)
	if !IsSessionOverviewLink(l) {
		t.Fatal("not a session overview link")
	}
	res, err := resolveText(t, text, ResolveOpts{Cwd: svc, SessionDocs: docsByCheckout(docs)})
	if err != nil {
		t.Fatal(err)
	}
	if !SamePath(res.Checkout, top) || !res.SessionOverview() || res.Hint.ID != sessOvID {
		t.Fatalf("res = %+v", res)
	}
}

func TestSessionOverviewLinkPicksTheNamedWorktree(t *testing.T) {
	t.Parallel()
	top, svc := sessOvRepo(t)
	wt2 := filepath.Join(filepath.Dir(top), filepath.Base(top)+"-wt2")
	gitRun(t, top, "worktree", "add", "-q", wt2)
	t.Cleanup(func() { os.RemoveAll(wt2) })
	docs := t.TempDir()
	writeSessOv(t, filepath.Join(docs, filepath.Base(top)), sessOvID, "main's")
	writeSessOv(t, filepath.Join(docs, filepath.Base(wt2)), sessOvID, "wt2's")
	text, err := SessionOverviewLink(wt2, sessOvID)
	if err != nil {
		t.Fatal(err)
	}
	res, err := resolveText(t, text, ResolveOpts{Cwd: svc, SessionDocs: docsByCheckout(docs)})
	if err != nil {
		t.Fatal(err)
	}
	if !SamePath(res.Checkout, wt2) {
		t.Fatalf("checkout = %q, want %q", res.Checkout, wt2)
	}
}

func TestSessionOverviewLinkGone(t *testing.T) {
	t.Parallel()
	top, svc := sessOvRepo(t)
	text, _ := SessionOverviewLink(top, sessOvID)
	_, err := resolveText(t, text, ResolveOpts{Cwd: svc, SessionDocs: docsByCheckout(t.TempDir())})
	if !errors.Is(err, ErrSessionOverviewGone) || !strings.Contains(err.Error(), "overview "+sessOvID+" no longer exists") {
		t.Fatalf("err = %v", err)
	}
}

func TestSessionOverviewLinkRefusedWithoutSeam(t *testing.T) {
	t.Parallel()
	top, svc := sessOvRepo(t)
	text, _ := SessionOverviewLink(top, sessOvID)
	_, err := resolveText(t, text, ResolveOpts{Cwd: svc})
	if !errors.Is(err, ErrOverviewNamesNoFiles) || !errors.Is(err, model.ErrLink) || !strings.Contains(err.Error(), "gg overview show") {
		t.Fatalf("err = %v", err)
	}
}

func TestSessionOverviewLinkRemovedWorktree(t *testing.T) {
	t.Parallel()
	gone := filepath.ToSlash(filepath.Join(t.TempDir(), "gone"))
	text, err := SessionOverviewLink(gone, sessOvID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = resolveText(t, text, ResolveOpts{SessionDocs: docsByCheckout(t.TempDir())})
	if !errors.Is(err, ErrLinkUnknownRepo) {
		t.Fatalf("err = %v", err)
	}
}

func TestSessionOverviewLinkNotATopLevel(t *testing.T) {
	t.Parallel()
	top, svc := sessOvRepo(t)
	sub := filepath.Join(top, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	text, _ := SessionOverviewLink(sub, sessOvID)
	_, err := resolveText(t, text, ResolveOpts{Cwd: svc, SessionDocs: docsByCheckout(t.TempDir())})
	if !errors.Is(err, ErrLinkUnknownRepo) {
		t.Fatalf("err = %v", err)
	}
}

func TestSessionOverviewLinkThroughSymlink(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	top, svc := sessOvRepo(t)
	link := filepath.Join(t.TempDir(), "via")
	if err := os.Symlink(top, link); err != nil {
		t.Fatal(err)
	}
	docs := t.TempDir()
	writeSessOv(t, filepath.Join(docs, filepath.Base(top)), sessOvID, "T")
	text, _ := SessionOverviewLink(link, sessOvID)
	res, err := resolveText(t, text, ResolveOpts{Cwd: svc, SessionDocs: docsByCheckout(docs)})
	if err != nil || !SamePath(res.Checkout, top) {
		t.Fatalf("res = %+v err = %v", res, err)
	}
}

func TestSessionOverviewLinkTourID(t *testing.T) {
	t.Parallel()
	top, svc := sessOvRepo(t)
	docs := t.TempDir()
	writeSessOv(t, filepath.Join(docs, filepath.Base(top)), "tour-brief", "Brief")
	text, _ := SessionOverviewLink(top, "tour-brief")
	if res, err := resolveText(t, text, ResolveOpts{Cwd: svc, SessionDocs: docsByCheckout(docs)}); err != nil || res.Hint.ID != "tour-brief" {
		t.Fatalf("res = %+v err = %v", res, err)
	}
}

func TestSessionOverviewLinkBadPath(t *testing.T) {
	t.Parallel()
	if _, err := SessionOverviewLink("/x/a@b", sessOvID); !errors.Is(err, model.ErrLink) {
		t.Fatalf("err = %v", err)
	}
	if _, err := SessionOverviewLink("/x/a", "../up"); err == nil {
		t.Fatal("a bad id must be refused")
	}
}

// A session overview link nested inside the cwd's checkout (a worktree under
// .claude/worktrees) still describes as the overview, not as a path.
func TestLinkDescSessionOverview(t *testing.T) {
	t.Parallel()
	top, svc := sessOvRepo(t)
	text, _ := SessionOverviewLink(filepath.Join(top, "nested"), sessOvID)
	l, _ := model.ParseLink(text)
	if got := svc.DescribeLink(context.Background(), l); got != "overview: "+sessOvID {
		t.Fatalf("desc = %q", got)
	}
}

// LocateLink is the compare/base-suggestion entry: it locates FILES, so a
// session overview link is refused there too.
func TestLocateLinkRefusesSessionOverview(t *testing.T) {
	t.Parallel()
	top, svc := sessOvRepo(t)
	text, _ := SessionOverviewLink(top, sessOvID)
	l, _ := model.ParseLink(text)
	if _, _, err := LocateLink(context.Background(), l, ResolveOpts{Cwd: svc, SessionDocs: docsByCheckout(t.TempDir())}); !errors.Is(err, ErrOverviewNamesNoFiles) {
		t.Fatalf("err = %v", err)
	}
}

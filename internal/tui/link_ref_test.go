package tui

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// refRowsModel has ONE row per ref panel and a DIFFERENT name in each, so an
// arm reading the wrong slice cannot produce the right text.
func refRowsModel(focus panel) Model {
	m := footerModel()
	m.linkRepoName = "r"
	m.branches = []model.Branch{{Name: "feat/x", Hash: "aaaa111"}}
	m.remoteBranches = []model.RemoteBranch{{Name: "origin/main", Hash: "bbbb222"}}
	m.tags = []model.Tag{{Name: "v1", Target: "cccc333"}}
	m.focus = focus
	return m
}

func TestRefRowsCopyTheNameNotTheSha(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		focus panel
		want  string
	}{
		{panelBranches, "gg://r@ref:feat/x"},
		{panelRemotes, "gg://r@ref:origin/main"},
		{panelTags, "gg://r@ref:v1"},
	} {
		m := refRowsModel(c.focus)
		got, ok := m.contextLinkText()
		if !ok || got != c.want {
			t.Errorf("panel %v: contextLinkText = %q, %v; want %q", c.focus, got, ok, c.want)
		}
		l, err := model.ParseLink(got)
		if err != nil || l.Target.Ref == "" {
			t.Errorf("panel %v: %q does not reparse as a ref link: %+v, %v", c.focus, got, l.Target, err)
		}
		// The `.` menu offers the SAME text, placed beside the other copy rows.
		rows := availableActions(m)
		r, ok := findRow(rows, "copy-link")
		if !ok || r.copyText != c.want {
			t.Errorf("panel %v: menu copy-link row = %+v, %v", c.focus, r.copyText, ok)
		}
		for i, row := range rows {
			if row.id == "copy-link" && (i == 0 || rows[i-1].id != "copy-commit-id") {
				t.Errorf("panel %v: copy-link must follow copy-commit-id; rows = %v", c.focus, rowIDs(rows))
			}
		}
	}
}

func rowIDs(rows []actionRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.id
	}
	return out
}

func TestRefLinkRefusesANameTheGrammarCannotHold(t *testing.T) {
	t.Parallel()
	m := refRowsModel(panelBranches)
	for _, bad := range []string{"a@b", "a:b", "a#b", "a?b", "a..b", "a b", ""} {
		if s, ok := m.refLinkFor(bad); ok {
			t.Errorf("refLinkFor(%q) = %q, want a refusal", bad, s)
		}
	}
}

// A files view opened over the Branches panel is not "the branch".
func TestRefArmIsGatedOnTheContentWindow(t *testing.T) {
	t.Parallel()
	m := refRowsModel(panelBranches)
	m.filesView = &contentPopup{}
	if got, ok := m.contextLinkText(); ok && strings.Contains(got, "@ref:") {
		t.Fatalf("a content window answered with the branch link %q", got)
	}
}

// Emission is half the contract. An ANNOTATED tag's name resolves to the tag
// OBJECT unless it is peeled (TestGotoCommitAnnotatedTagLoadsFiles exists
// because `#` once got that wrong), so the copied link is EVALUATED too — for
// a lightweight and an annotated tag on one commit, which must agree.
func TestACopiedTagLinkEvaluatesToItsCommit(t *testing.T) {
	t.Parallel()
	dir, repo := newRepoDir(t)
	for _, args := range [][]string{{"tag", "light"}, {"-c", "user.name=t", "-c", "user.email=t@t", "tag", "-a", "ann", "-m", "x"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	svc := domain.New(repo)
	ctx := context.Background()
	head, ok, err := svc.ResolveRev(ctx, "HEAD")
	if err != nil || !ok {
		t.Fatal(err)
	}
	m := New(svc)
	m.linkRepoName = "r"
	for _, name := range []string{"light", "ann"} {
		text, ok := m.refLinkFor(name)
		if !ok {
			t.Fatalf("%s: refused", name)
		}
		l, err := model.ParseLink(text)
		if err != nil {
			t.Fatal(err)
		}
		fs, err := svc.EvalLink(ctx, l)
		if err != nil || fs.Bounded() || fs.Endpoint().Hash() != head {
			t.Errorf("%s: endpoint %q bounded=%v err=%v, want the commit %s", name, fs.Endpoint().Hash(), fs.Bounded(), err, head)
		}
	}
}

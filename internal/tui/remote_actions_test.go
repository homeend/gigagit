package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/model"
)

func TestCopyRowsBranchesHaveIdAndSha(t *testing.T) {
	m := footerModel()
	m.focus = panelBranches
	m.branches = []model.Branch{{Name: "main", Hash: "abc1234"}}
	rows := m.contextCopyRows()
	if r, ok := findRow(rows, "copy-branch-name"); !ok || r.copyText != "main" {
		t.Fatalf("missing copy-branch-name=main; rows=%v", rows)
	}
	if r, ok := findRow(rows, "copy-commit-id"); !ok || r.copyText != "abc1234" {
		t.Fatalf("missing copy-commit-id=abc1234; rows=%v", rows)
	}
	if _, ok := findRow(rows, "copy-commit-sha"); !ok {
		t.Fatalf("missing copy-commit-sha; rows=%v", rows)
	}
}

func TestCopyRowsRemotesHaveIdAndSha(t *testing.T) {
	m := footerModel()
	m.focus = panelRemotes
	m.remoteBranches = []model.RemoteBranch{{Name: "origin/foo", Remote: "origin", Branch: "foo", Hash: "dead111"}}
	rows := m.contextCopyRows()
	if r, ok := findRow(rows, "copy-branch-name"); !ok || r.copyText != "origin/foo" {
		t.Fatalf("missing copy-branch-name=origin/foo; rows=%v", rows)
	}
	if r, ok := findRow(rows, "copy-commit-id"); !ok || r.copyText != "dead111" {
		t.Fatalf("missing copy-commit-id=dead111; rows=%v", rows)
	}
	if _, ok := findRow(rows, "copy-commit-sha"); !ok {
		t.Fatalf("missing copy-commit-sha; rows=%v", rows)
	}
}

func TestCopyShaRowFallsBackWithoutService(t *testing.T) {
	m := footerModel() // no svc set
	row := m.copyShaRow("origin/foo", "dead111")
	if row.run == nil {
		t.Fatal("copyShaRow must carry a run handler")
	}
	if _, cmd := row.run(m); cmd == nil {
		t.Fatal("copyShaRow run returned nil cmd")
	}
}

func TestCopyShaRowResolvesFullViaService(t *testing.T) {
	fr := gitexec.NewFakeRunner()
	fr.SetResponse("git rev-parse", gitexec.Result{Stdout: "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef\n"})
	m := footerModel()
	m.svc = domain.New(&git.Repo{Runner: fr})
	row := m.copyShaRow("origin/foo", "dead111")
	if _, cmd := row.run(m); cmd == nil {
		t.Fatal("expected a copy cmd")
	}
}

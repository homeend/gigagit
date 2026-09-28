package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

const (
	tvBase = "1111111111111111111111111111111111111111"
	tvOurs = "2222222222222222222222222222222222222222"
)

// twoBranchVersion is a rebase record with usable endpoints — the row a
// preview link is copied from.
func twoBranchVersion() model.BranchVersion {
	return model.BranchVersion{Ref: "refs/gg/versions/main/1753100000-rebase", Hash: tvOurs, Subject: "did a rebase", Op: "rebase", Unix: 1753100000, Base: tvBase, Ours: tvOurs}
}

func TestVersionLinkForIsThePreviewPairPlusTheHint(t *testing.T) {
	t.Parallel()
	m := Model{linkRepoName: "gigagit"}
	got, ok := m.versionLinkFor(twoBranchVersion())
	if !ok || got != "gg://gigagit@"+tvBase+".."+tvOurs+"?version=1753100000-rebase" {
		t.Fatalf("versionLinkFor = %q ok=%v", got, ok)
	}
}

func TestVersionLinkForRefusesAOneBranchRecord(t *testing.T) {
	t.Parallel()
	m := Model{linkRepoName: "gigagit"}
	v := twoBranchVersion()
	v.Base, v.Ours = "", ""
	if got, ok := m.versionLinkFor(v); ok || got != "" {
		t.Fatalf("one-branch record: got %q ok=%v, want none", got, ok)
	}
}

// The trailer is split on whitespace and never sha-checked: a crafted or
// old record can hold anything, and the producer must decline, not emit.
func TestVersionLinkForRefusesAnUnusableRecord(t *testing.T) {
	t.Parallel()
	m := Model{linkRepoName: "gigagit"}
	v := twoBranchVersion()
	v.Ours = "not-a-sha"
	if got, ok := m.versionLinkFor(v); ok || strings.Contains(got, "not-a-sha") {
		t.Fatalf("unusable record: got %q ok=%v", got, ok)
	}
}

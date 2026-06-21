package domain

import (
	"context"
	"testing"

	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/model"
)

func TestCompareFilesGatedQuery(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git diff (compare files)", gitexec.Result{Stdout: "M\tREADME.md\nA\tb.txt\n"})
	svc := New(&git.Repo{Runner: f})

	files, err := svc.CompareFiles(context.Background(),
		model.Endpoint{Kind: model.EndpointCommit, Hash: "abc123"},
		model.Endpoint{Kind: model.EndpointWorkTree})
	if err != nil {
		t.Fatalf("CompareFiles err: %v", err)
	}
	if len(files) != 2 || files[0].Path != "README.md" || files[0].Status != "M" || files[1].Path != "b.txt" {
		t.Fatalf("CompareFiles = %+v", files)
	}
}

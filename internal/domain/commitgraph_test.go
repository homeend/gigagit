package domain

import (
	"context"
	"testing"
)

func TestHasCommitGraphFalseThenTrue(t *testing.T) {
	svc := realFeedRepo(t) // real-repo helper from commitpager_test.go
	ctx := context.Background()
	if has, err := svc.HasCommitGraph(ctx); err != nil || has {
		t.Fatalf("fresh repo: has=%v err=%v, want false", has, err)
	}
	if err := svc.WriteCommitGraph(ctx); err != nil {
		t.Fatal(err)
	}
	if has, err := svc.HasCommitGraph(ctx); err != nil || !has {
		t.Fatalf("after write: has=%v err=%v, want true", has, err)
	}
}

package git

import (
	"context"
	"testing"
)

func TestRefVerbs(t *testing.T) {
	t.Parallel()
	_, runner := newTestRepo(t)
	r := &Repo{Runner: runner}
	head, err := r.RevParse(context.Background(), "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ref := "refs/gg/versions/main/1753100000-merge"
	if err := r.UpdateRef(ctx, ref, head); err != nil {
		t.Fatal(err)
	}
	infos, err := r.ForEachRef(ctx, "refs/gg/versions")
	if err != nil || len(infos) != 1 {
		t.Fatalf("ForEachRef = %v, %v; want 1 row", infos, err)
	}
	if infos[0].Ref != ref || infos[0].Hash != head || infos[0].Subject == "" {
		t.Fatalf("row = %+v", infos[0])
	}
	if err := r.DeleteRef(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if infos, _ = r.ForEachRef(ctx, "refs/gg/versions"); len(infos) != 0 {
		t.Fatalf("after delete: %v", infos)
	}
}

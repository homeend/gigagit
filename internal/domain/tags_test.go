package domain

import (
	"context"
	"testing"
)

func TestServiceTags(t *testing.T) {
	t.Parallel()
	dir := cleanDir(t)
	svc := svcAt(dir)
	gitRunDir(t, dir, "", "tag", "v1.0.0")

	tags, err := svc.Tags(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 1 || tags[0].Name != "v1.0.0" {
		t.Fatalf("tags = %+v", tags)
	}
}

func TestSnapshotIncludesTags(t *testing.T) {
	t.Parallel()
	dir := cleanDir(t)
	svc := svcAt(dir)
	gitRunDir(t, dir, "", "tag", "v1.0.0")

	snap, err := svc.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Tags) != 1 || snap.Tags[0].Name != "v1.0.0" {
		t.Fatalf("snap.Tags = %+v", snap.Tags)
	}
}

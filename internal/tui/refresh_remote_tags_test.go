package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/config"
)

func TestRemoteTagsScheduledItem(t *testing.T) {
	t.Parallel()
	if refreshTomlKey(remoteTagsItem) != "remote_tags" {
		t.Fatalf("toml key = %q, want remote_tags", refreshTomlKey(remoteTagsItem))
	}
	cfg := config.RefreshConfig{Enabled: true, RemoteTags: 30, MinSeconds: 10}
	secs, on := scheduledInterval(cfg, remoteTagsItem)
	if !on || secs != 30 {
		t.Fatalf("scheduledInterval = (%d,%v), want (30,true)", secs, on)
	}
	// default 0 → off
	if _, on := scheduledInterval(config.RefreshConfig{Enabled: true}, remoteTagsItem); on {
		t.Fatal("remote_tags default 0 must be off")
	}
	// floors at min_seconds (interval < min → clamped up)
	cfg2 := config.RefreshConfig{Enabled: true, RemoteTags: 5, MinSeconds: 10}
	secs2, on2 := scheduledInterval(cfg2, remoteTagsItem)
	if !on2 || secs2 != 10 {
		t.Fatalf("scheduledInterval with floor = (%d,%v), want (10,true)", secs2, on2)
	}
	// present in the scheduled set
	found := false
	for _, it := range scheduledItems {
		if it.isRemoteTags {
			found = true
		}
	}
	if !found {
		t.Fatal("remoteTagsItem must be in scheduledItems")
	}
}

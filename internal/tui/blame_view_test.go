package tui

import (
	"testing"
	"time"

	"github.com/gigagit/gg/internal/model"
)

func TestGroupBlameCollapsesRuns(t *testing.T) {
	lines := []model.BlameLine{
		{Hash: "aaa"}, {Hash: "aaa"}, {Hash: "bbb"}, {Hash: "aaa"},
	}
	blocks := groupBlame(lines)
	if len(blocks) != 3 {
		t.Fatalf("want 3 blocks (aaa,aaa | bbb | aaa), got %d: %+v", len(blocks), blocks)
	}
	if blocks[0].start != 0 || blocks[0].end != 1 || blocks[0].hash != "aaa" {
		t.Errorf("block 0 wrong: %+v", blocks[0])
	}
	if blocks[1].start != 2 || blocks[1].end != 2 || blocks[1].hash != "bbb" {
		t.Errorf("block 1 wrong: %+v", blocks[1])
	}
	if blocks[2].start != 3 || blocks[2].end != 3 {
		t.Errorf("block 2 wrong: %+v", blocks[2])
	}
}

func TestGroupBlameEdges(t *testing.T) {
	if got := groupBlame(nil); len(got) != 0 {
		t.Errorf("empty input → no blocks, got %+v", got)
	}
	all := groupBlame([]model.BlameLine{{Hash: "x"}, {Hash: "x"}, {Hash: "x"}})
	if len(all) != 1 || all[0].start != 0 || all[0].end != 2 {
		t.Errorf("all-same → one block, got %+v", all)
	}
}

func TestBlockAt(t *testing.T) {
	blocks := []blameBlock{{start: 0, end: 1, hash: "aaa"}, {start: 2, end: 2, hash: "bbb"}}
	if b, ok := blockAt(blocks, 1); !ok || b.hash != "aaa" {
		t.Errorf("line 1 should be in block aaa, got %+v ok=%v", b, ok)
	}
	if b, ok := blockAt(blocks, 2); !ok || b.hash != "bbb" {
		t.Errorf("line 2 should be in block bbb, got %+v ok=%v", b, ok)
	}
	if _, ok := blockAt(blocks, 9); ok {
		t.Error("out-of-range line should not match a block")
	}
}

func TestBlameAge(t *testing.T) {
	now := time.Unix(1_000_000_000, 0)
	cases := []struct {
		ago  time.Duration
		want string
	}{
		{30 * time.Second, "now"},
		{5 * time.Minute, "5m"},
		{3 * time.Hour, "3h"},
		{2 * 24 * time.Hour, "2d"},
		{90 * 24 * time.Hour, "3mo"},
		{800 * 24 * time.Hour, "2y"},
	}
	for _, c := range cases {
		if got := blameAge(now, now.Add(-c.ago)); got != c.want {
			t.Errorf("blameAge(-%s) = %q, want %q", c.ago, got, c.want)
		}
	}
}

package domain

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/exttool"
)

func TestRepoConfigActions(t *testing.T) {
	t.Parallel()
	all := []RepoConfigAction{CopyToPrivate, MoveToPrivate, CopyToCommitted, MoveToCommitted}
	cases := []struct {
		name                             string
		comEx, privEx, haveCom, havePriv bool
		want                             []RepoConfigAction
	}{
		{"committed only", true, false, true, true, all[:2]},
		{"private only", false, true, true, true, all[2:]},
		{"both", true, true, true, true, all},
		{"neither", false, false, true, true, nil},
		{"no private path", true, false, true, false, nil},
		{"no committed path", false, true, false, true, nil},
	}
	for _, c := range cases {
		if got := RepoConfigActions(c.comEx, c.privEx, c.haveCom, c.havePriv); !slices.Equal(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestRepoConfigEndpoints(t *testing.T) {
	t.Parallel()
	cases := []struct {
		act      RepoConfigAction
		src, dst string
		move     bool
	}{
		{CopyToPrivate, "c", "p", false},
		{MoveToPrivate, "c", "p", true},
		{CopyToCommitted, "p", "c", false},
		{MoveToCommitted, "p", "c", true},
		{"bogus", "", "", false},
	}
	for _, c := range cases {
		src, dst, move := RepoConfigEndpoints(c.act, "c", "p")
		if src != c.src || dst != c.dst || move != c.move {
			t.Errorf("%s: got %q %q %v", c.act, src, dst, move)
		}
	}
	if got := RepoConfigActionLabel(MoveToCommitted); got != "Move to committed (.gg.toml)" {
		t.Errorf("label = %q", got)
	}
}

func TestRepoConfigPaths(t *testing.T) {
	t.Parallel()
	dir := cleanDir(t)
	committed, private, err := svcAt(dir).RepoConfigPaths(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, ".gg.toml"); !samePathT(committed, want) {
		t.Errorf("committed = %q, want %q", committed, want)
	}
	if private == "" || private != config.PrivateRepoPath(filepath.Dir(committed)) {
		t.Errorf("private = %q", private)
	}
}

func samePathT(a, b string) bool {
	ea, _ := filepath.EvalSymlinks(filepath.Dir(a))
	eb, _ := filepath.EvalSymlinks(filepath.Dir(b))
	return ea == eb && filepath.Base(a) == filepath.Base(b)
}

func TestDefaultToolChecked(t *testing.T) {
	t.Parallel()
	plain := exttool.CommandTemplate{Category: exttool.CatConflict, Name: "Claude"}
	yolo := exttool.CommandTemplate{Category: exttool.CatConflict, Name: "Claude (yolo)", OptIn: true}
	complete := exttool.CommandTemplate{Category: exttool.CatConflictComplete, Name: "Kimi — resolve & complete"}
	if !DefaultToolChecked(plain, false) {
		t.Error("a plain new row starts checked")
	}
	if DefaultToolChecked(yolo, false) || DefaultToolChecked(complete, false) {
		t.Error("an opt-in row and every conflict_complete row start unchecked")
	}
	if !DefaultToolChecked(yolo, true) || !DefaultToolChecked(complete, true) {
		t.Error("a configured row shows checked")
	}
}

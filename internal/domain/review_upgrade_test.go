package domain

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/preflight"
)

func verdictOf(t *testing.T, svc *Service, id string) preflight.Verdict {
	t.Helper()
	vs, err := svc.Preflight(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range vs {
		if v.Feature.ID == id {
			return v
		}
	}
	t.Fatalf("no verdict for %s", id)
	return preflight.Verdict{}
}

// Serial: sets XDG_CONFIG_HOME.
func TestStructuredReviewsMigrationUpgradesOldCommands(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	_, svc := newRealRepo(t)
	ctx := context.Background()

	old := exttool.GenerateCommandFor(exttool.CommandTemplate{Command: exttool.SupersededReviewCommands[0].Old}, "claude", runtime.GOOS)
	edited := old + " --model opus"
	global := config.DefaultGlobalPath()
	if err := os.MkdirAll(filepath.Dir(global), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := config.AppendToolCommands(global, []config.ToolCommand{
		{Category: "review", Name: "Claude", Mode: "capture", Command: old},
		{Category: "review", Name: "Mine", Mode: "capture", Command: edited},
	}); err != nil {
		t.Fatal(err)
	}

	if v := verdictOf(t, svc, FeatureStructuredReviews); v.State != preflight.Repairable {
		t.Fatalf("with an old command stored: %v, want Repairable", v.State)
	}
	if err := svc.RunAutoMigrations(ctx); err != nil {
		t.Fatal(err)
	}
	if cmds, _ := config.ToolCommandsIn(global); strings.TrimSpace(cmds[0].Command) != strings.TrimSpace(old) {
		t.Fatal("the migration asks for consent: RunAutoMigrations must not rewrite the config")
	}
	pend, err := svc.PendingMigrations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var mig *PendingMigration
	for i := range pend {
		if pend[i].Feature == FeatureStructuredReviews {
			mig = &pend[i]
		}
	}
	if mig == nil || mig.Consequence == "" {
		t.Fatalf("pending = %+v, want the structured-reviews migration with its consequence", pend)
	}
	if err := svc.RunMigration(ctx, *mig); err != nil {
		t.Fatal(err)
	}
	cmds, err := config.ToolCommandsIn(global)
	if err != nil || len(cmds) != 2 {
		t.Fatalf("%+v %v", cmds, err)
	}
	up := strings.TrimSpace(cmds[0].Command)
	if !strings.HasPrefix(up, "claude -p ") || strings.Contains(up, "/code-review") || !strings.Contains(up, "Review output") {
		t.Fatalf("upgraded command:\n%s", up)
	}
	if strings.TrimSpace(cmds[1].Command) != strings.TrimSpace(edited) {
		t.Fatalf("an edited command must be left alone:\n%s", cmds[1].Command)
	}
	if v := verdictOf(t, svc, FeatureStructuredReviews); v.State != preflight.Satisfied {
		t.Fatalf("after the migration: %v, want Satisfied", v.State)
	}
}

// Serial: sets XDG_CONFIG_HOME.
func TestStructuredReviewsSatisfiedWithoutOldCommands(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	_, svc := newRealRepo(t)
	if v := verdictOf(t, svc, FeatureStructuredReviews); v.State != preflight.Satisfied {
		t.Fatalf("%v, want Satisfied", v.State)
	}
}

package tui

import (
	"bufio"
	"errors"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
)

// preflightServiceFor builds a Service whose `git version` answer is
// gitVer — old enough to fail domain.MinGitVersion when gitVer is below it,
// current otherwise. for-each-ref answers empty for both the meta-marker and
// version-ref probes, since neither store's format matters once FeatureCore
// (Required) has already resolved.
func preflightServiceFor(gitVer string) *domain.Service {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git version", gitexec.Result{Stdout: "git version " + gitVer + "\n"})
	f.SetResponse("git for-each-ref (gg)", gitexec.Result{Stdout: ""})
	return domain.New(&git.Repo{Runner: f})
}

func TestPreflightRequiredUnsatisfiableBlocksLaunch(t *testing.T) {
	t.Parallel()
	svc := preflightServiceFor("2.20.0") // below domain.MinGitVersion (2.30.0): FeatureCore -> Unsatisfiable
	proceed, err := Preflight(svc, strings.NewReader(""), &strings.Builder{})
	if proceed {
		t.Fatalf("want proceed=false for an Unsatisfiable Required feature")
	}
	if err == nil {
		t.Fatalf("want a non-nil error naming the reason")
	}
	if !strings.Contains(err.Error(), "2.20.0") {
		t.Errorf("error %q does not name the reason (git version)", err.Error())
	}
}

func TestPreflightNothingPendingProceedsSilently(t *testing.T) {
	t.Parallel()
	svc := preflightServiceFor("2.45.0") // satisfies MinGitVersion; no stores => no pending migration either
	var out strings.Builder
	proceed, err := Preflight(svc, strings.NewReader(""), &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !proceed {
		t.Fatalf("want proceed=true when nothing is pending")
	}
	if out.String() != "" {
		t.Errorf("want no output when nothing is pending, got %q", out.String())
	}
}

// TestAskMigration exercises the repairable-feature consent path (skip vs.
// migrate) that askMigration factors out of Preflight. It is driven
// directly against a synthetic domain.PendingMigration rather than through
// Preflight end-to-end: on today's build domain.Features() declares no
// feature with a Migrate (format 2 and its migration arrive in a follow-up
// spec — see the comment on FeatureVersions), so svc.PendingMigrations is
// always empty and no real repository can produce a pending migration for
// Preflight to ask about. Faking one into domain.Features() for this test
// would test a feature that does not exist; this instead tests the actual
// decision logic Preflight will run once a real migration is declared.
func TestAskMigrationSkipLeavesRefsUntouchedMigrateRemovesThem(t *testing.T) {
	t.Parallel()

	pending := domain.PendingMigration{
		Feature:     "versions",
		Store:       "versions",
		From:        1,
		To:          2,
		Consequence: "this will rewrite the versions store",
		Refs:        []string{"refs/gg/versions/a", "refs/gg/versions/b"},
	}

	t.Run("skip", func(t *testing.T) {
		refs := append([]string(nil), pending.Refs...)
		ran := false
		var out strings.Builder
		proceed, err := askMigration(pending, bufio.NewReader(strings.NewReader("s\n")), &out, func(domain.PendingMigration) error {
			ran = true
			refs = nil
			return nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !proceed {
			t.Fatalf("want proceed=true on skip")
		}
		if ran {
			t.Errorf("skip must not run the migration")
		}
		if len(refs) != 2 {
			t.Errorf("skip must leave the refs untouched, got %v", refs)
		}
	})

	t.Run("migrate", func(t *testing.T) {
		refs := append([]string(nil), pending.Refs...)
		var out strings.Builder
		proceed, err := askMigration(pending, bufio.NewReader(strings.NewReader("m\n")), &out, func(domain.PendingMigration) error {
			refs = nil
			return nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !proceed {
			t.Fatalf("want proceed=true after a successful migrate")
		}
		if refs != nil {
			t.Errorf("migrate must remove the refs, got %v", refs)
		}
	})

	t.Run("quit", func(t *testing.T) {
		ran := false
		var out strings.Builder
		proceed, err := askMigration(pending, bufio.NewReader(strings.NewReader("q\n")), &out, func(domain.PendingMigration) error {
			ran = true
			return nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if proceed {
			t.Fatalf("want proceed=false on quit")
		}
		if ran {
			t.Errorf("quit must not run the migration")
		}
	})

	t.Run("unreadable input quits, not consents", func(t *testing.T) {
		ran := false
		var out strings.Builder
		proceed, err := askMigration(pending, bufio.NewReader(strings.NewReader("")), &out, func(domain.PendingMigration) error {
			ran = true
			return nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if proceed {
			t.Fatalf("want proceed=false when input is unreadable (EOF)")
		}
		if ran {
			t.Errorf("unreadable input must never be treated as consent")
		}
	})

	t.Run("migration error blocks launch", func(t *testing.T) {
		wantErr := errors.New("boom")
		var out strings.Builder
		proceed, err := askMigration(pending, bufio.NewReader(strings.NewReader("m\n")), &out, func(domain.PendingMigration) error {
			return wantErr
		})
		if proceed {
			t.Fatalf("want proceed=false when the migration itself fails")
		}
		if !errors.Is(err, wantErr) {
			t.Errorf("want the migration error surfaced, got %v", err)
		}
	})
}

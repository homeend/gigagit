package tui

import (
	"bufio"
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

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

// TestAskMigrationLocalizesTheConsequence is the prose guard the spec's Prose
// section asks for on the consent screen. domain pre-renders an English
// Consequence for the CLI and web (English there is by design); the TUI must
// instead put the UNRENDERED (format, args) pair through i18n.T, so the one
// screen that describes irreversible data loss reads in the user's language.
//
// The bundled "xx" language makes the difference observable: an untranslated
// key falls back to English, so a Sprintf-based implementation and a
// translated one would otherwise be indistinguishable.
//
// NOT parallel: withXXLanguage switches the process-global active language.
func TestAskMigrationLocalizesTheConsequence(t *testing.T) {
	withXXLanguage(t, map[string]string{
		"this discards every recorded version of %s": "XX-discards %s",
	})

	m := domain.PendingMigration{
		Feature:           "versions",
		Store:             "versions",
		From:              1,
		To:                2,
		Consequence:       "this discards every recorded version of main",
		ConsequenceFormat: "this discards every recorded version of %s",
		ConsequenceArgs:   []any{"main"},
		Refs:              []string{"refs/gg/versions/a"},
	}

	var out strings.Builder
	if _, err := askMigration(m, bufio.NewReader(strings.NewReader("s\n")), &out, func(domain.PendingMigration) error {
		return nil
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "XX-discards main") {
		t.Errorf("consent screen %q does not carry the LOCALIZED consequence", out.String())
	}
	if strings.Contains(out.String(), "this discards every recorded version of main") {
		t.Errorf("consent screen %q still prints the pre-rendered English consequence", out.String())
	}
}

// TestAskMigrationFallsBackToTheRenderedConsequence covers a Migrate whose
// Describe is nil or whose prose domain could not decompose: the English
// Consequence is still shown rather than an empty line.
func TestAskMigrationFallsBackToTheRenderedConsequence(t *testing.T) {
	t.Parallel()
	m := domain.PendingMigration{Feature: "versions", Consequence: "plain english only"}
	var out strings.Builder
	if _, err := askMigration(m, bufio.NewReader(strings.NewReader("s\n")), &out, func(domain.PendingMigration) error {
		return nil
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "plain english only") {
		t.Errorf("consent screen %q dropped the fallback consequence", out.String())
	}
}

// TestAskMigrationFramesTheMessage guards the consent screen's shape: every
// prose line is framed by the same border and starts at one column, so the
// long consequence paragraph cannot run ragged against the heading.
func TestAskMigrationFramesTheMessage(t *testing.T) {
	t.Parallel()

	m := domain.PendingMigration{
		Feature:     "versions",
		Consequence: strings.Repeat("long consequence prose that must wrap ", 6),
		Refs:        make([]string, 78),
	}
	var out strings.Builder
	if _, err := askMigration(m, bufio.NewReader(strings.NewReader("s\n")), &out, func(domain.PendingMigration) error {
		return nil
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) < 4 {
		t.Fatalf("want a framed block, got %q", out.String())
	}
	top, bottom := lines[0], lines[len(lines)-2] // last line is the prompt
	if !strings.HasPrefix(top, "┌") || !strings.HasSuffix(top, "┐") {
		t.Errorf("want a top border, got %q", top)
	}
	if !strings.HasPrefix(bottom, "└") || !strings.HasSuffix(bottom, "┘") {
		t.Errorf("want a bottom border, got %q", bottom)
	}
	wantW := lipgloss.Width(top)
	for _, l := range lines[1 : len(lines)-2] {
		if !strings.HasPrefix(l, "│ ") || !strings.HasSuffix(l, " │") {
			t.Errorf("line %q is not framed", l)
		}
		if got := lipgloss.Width(l); got != wantW {
			t.Errorf("line %q width %d, want %d", l, got, wantW)
		}
	}
	if last := lines[len(lines)-1]; strings.HasPrefix(last, " ") || strings.HasPrefix(last, "│") {
		t.Errorf("the prompt must sit outside the box, got %q", last)
	}
}

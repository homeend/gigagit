package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// TestLinksPrintsNewestFirstTerse: one "<desc>\t<link>" row per line,
// newest-first.
func TestLinksPrintsNewestFirstTerse(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	svc := linkHistSvc(t, dir)
	svc.RecordLink(context.Background(), "gg://gigagit@ref:feat/x", "branch: feat/x")
	svc.RecordLink(context.Background(), "gg://gigagit@abc123", "commit: abc123 fix auth")

	var out, errb bytes.Buffer
	code := cmdLinks(svc, nil, &out, &errb)
	if code != 0 {
		t.Fatalf("gg links: exit %d (stderr %q)", code, errb.String())
	}
	want := "commit: abc123 fix auth\tgg://gigagit@abc123\n" +
		"branch: feat/x\tgg://gigagit@ref:feat/x\n"
	if out.String() != want {
		t.Errorf("gg links = %q, want %q", out.String(), want)
	}
}

// TestLinksJSON mirrors `gg link resolve --json`'s convention: a JSON array
// of the same rows.
func TestLinksJSON(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	svc := linkHistSvc(t, dir)
	svc.RecordLink(context.Background(), "gg://gigagit@ref:feat/x", "branch: feat/x")

	var out, errb bytes.Buffer
	code := cmdLinks(svc, []string{"--json"}, &out, &errb)
	if code != 0 {
		t.Fatalf("gg links --json: exit %d (stderr %q)", code, errb.String())
	}
	got := strings.TrimSpace(out.String())
	if !strings.Contains(got, `"link":"gg://gigagit@ref:feat/x"`) || !strings.Contains(got, `"desc":"branch: feat/x"`) {
		t.Errorf("gg links --json = %q, missing expected fields", got)
	}
}

// TestLinksEmptyPrintsNothing: an empty ring prints nothing at exit 0 — not
// an error, not "[]", not a header line.
func TestLinksEmptyPrintsNothing(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	svc := linkHistSvc(t, dir)

	var out, errb bytes.Buffer
	code := cmdLinks(svc, nil, &out, &errb)
	if code != 0 {
		t.Fatalf("gg links (empty): exit %d (stderr %q)", code, errb.String())
	}
	if out.String() != "" {
		t.Errorf("gg links (empty) stdout = %q, want empty", out.String())
	}
	if errb.String() != "" {
		t.Errorf("gg links (empty) stderr = %q, want empty", errb.String())
	}
}

// TestLinksDisabledPrintsNothing: no resolvable state dir (history disabled)
// must not crash or print noise — LinkHistory returns nil, and `gg links`
// treats nil exactly like an empty ring.
func TestLinksDisabledPrintsNothing(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("LocalAppData", "")
	dir := newCLIRepo(t)
	svc := openCLIService(t, dir) // no injected store, and no resolvable state dir

	var out, errb bytes.Buffer
	code := cmdLinks(svc, nil, &out, &errb)
	if code != 0 {
		t.Fatalf("gg links (disabled): exit %d (stderr %q)", code, errb.String())
	}
	if out.String() != "" || errb.String() != "" {
		t.Fatalf("gg links (disabled): stdout=%q stderr=%q, want both empty", out.String(), errb.String())
	}
}

// TestLinksUsageErrorOnExtraArgument.
func TestLinksUsageErrorOnExtraArgument(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	svc := linkHistSvc(t, dir)

	var out, errb bytes.Buffer
	code := cmdLinks(svc, []string{"bogus"}, &out, &errb)
	if code != 2 {
		t.Fatalf("gg links bogus: exit %d, want 2 (usage)", code)
	}
	if !strings.Contains(errb.String(), "usage") {
		t.Fatalf("gg links bogus: stderr = %q, want a usage message", errb.String())
	}
}

// TestLinksIsRoutedByCLIRun: `gg links` reaches cmdLinks through the real
// verb router, not just through a direct cmdLinks call — the end-to-end
// wiring `gg link` prints + `gg links` lists.
func TestLinksIsRoutedByCLIRun(t *testing.T) {
	t.Parallel()
	old := RepoStatePath
	RepoStatePath = ""
	defer func() { RepoStatePath = old }()

	dir := newCLIRepo(t)
	svc := linkHistSvc(t, dir)
	svc.RecordLink(context.Background(), "gg://gigagit@ref:feat/x", "branch: feat/x")

	var out, errb bytes.Buffer
	code := runOne(svc, dir, "links", nil, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("gg links (routed): exit %d (stderr %q)", code, errb.String())
	}
	if !strings.Contains(out.String(), "gg://gigagit@ref:feat/x") {
		t.Errorf("gg links (routed) = %q, missing the recorded link", out.String())
	}
	if !IsCommand("links") {
		t.Error("IsCommand(\"links\") = false, want true")
	}
}

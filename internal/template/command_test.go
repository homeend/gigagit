package template

import (
	"strings"
	"testing"
)

func repoCtx() CmdCtx {
	return CmdCtx{
		Op: "merge", Source: "feature", Target: "main",
		Repo:            "/work/my repo",
		ConflictedFiles: []string{"a.go", "dir/b c.go"},
	}
}

func TestResolveCommandProseTokensRaw(t *testing.T) {
	got, err := resolveCommandFor(`agent "resolve <op> of <source> into <target>: <conflicted-files>"`, nil, repoCtx(), "linux")
	if err != nil {
		t.Fatal(err)
	}
	want := `agent "resolve merge of feature into main: a.go dir/b c.go"`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestResolveCommandPathTokensQuoted(t *testing.T) {
	ctx := repoCtx()
	ctx.File = "dir/b c.go"
	ctx.Local, ctx.Base, ctx.Remote = "/tmp/l 1", "/tmp/b", "/tmp/r"
	ctx.Merged = "/work/my repo/dir/b c.go"
	got, err := resolveCommandFor(`meld --output=<merged> <local> <base> <remote>`, nil, ctx, "linux")
	if err != nil {
		t.Fatal(err)
	}
	want := `meld --output='/work/my repo/dir/b c.go' '/tmp/l 1' '/tmp/b' '/tmp/r'`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestResolveCommandPosixQuoteEscapesSingleQuote(t *testing.T) {
	ctx := repoCtx()
	ctx.File = "it's.go"
	ctx.Local, ctx.Base, ctx.Remote, ctx.Merged = "/t/l", "/t/b", "/t/r", "/t/m"
	got, err := resolveCommandFor(`tool <file>`, nil, ctx, "linux")
	if err != nil {
		t.Fatal(err)
	}
	if want := `tool 'it'\''s.go'`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveCommandWindowsQuoting(t *testing.T) {
	ctx := repoCtx()
	got, err := resolveCommandFor(`tool <repo>`, nil, ctx, "windows")
	if err != nil {
		t.Fatal(err)
	}
	if want := `tool "/work/my repo"`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveCommandUserInputs(t *testing.T) {
	got, err := resolveCommandFor(`agent --hint <user:hint>`, map[string]string{"hint": "be careful"}, repoCtx(), "linux")
	if err != nil {
		t.Fatal(err)
	}
	if want := `agent --hint be careful`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if _, err := resolveCommandFor(`agent <user:hint>`, nil, repoCtx(), "linux"); err == nil {
		t.Error("missing user input must error")
	}
}

func TestResolveCommandErrors(t *testing.T) {
	cases := []struct{ name, tmpl string }{
		{"unknown token", `tool <nope>`},
		{"bin at runtime", `<bin> --merge <source>`},
		{"per-file token without file context", `tool <local>`},
	}
	for _, c := range cases {
		if _, err := resolveCommandFor(c.tmpl, nil, repoCtx(), "linux"); err == nil {
			t.Errorf("%s: want error, got none", c.name)
		}
	}
}

func TestResolveCommandEmptyProseIsAllowed(t *testing.T) {
	// Source/Target may be empty (e.g. a revert with no attribution); agents
	// recover via git, so the resolver must not fail.
	ctx := CmdCtx{Op: "revert", Repo: "/r", ConflictedFiles: []string{"a"}}
	got, err := resolveCommandFor(`agent "<op> <source>"`, nil, ctx, "linux")
	if err != nil {
		t.Fatal(err)
	}
	if want := `agent "revert "`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestValidateCommandTokens(t *testing.T) {
	if err := ValidateCommandTokens(`a <op> <source> <target> <conflicted-files> <repo> <user:x>`, false); err != nil {
		t.Errorf("repo-level tokens: %v", err)
	}
	if err := ValidateCommandTokens(`m <file> <local> <base> <remote> <merged>`, true); err != nil {
		t.Errorf("per-file tokens: %v", err)
	}
	if err := ValidateCommandTokens(`m <local>`, false); err == nil {
		t.Error("per-file token in repo-level command must error")
	}
	if err := ValidateCommandTokens(`m <bogus>`, true); err == nil {
		t.Error("unknown token must error")
	}
	if err := ValidateCommandTokens(`<bin> x`, false); err == nil || !strings.Contains(err.Error(), "generated") {
		t.Errorf("<bin> must error mentioning generation, got %v", err)
	}
	if err := ValidateCommandTokens(`x <context-file>`, false); err != nil {
		t.Errorf("<context-file> is a known, non-per-file token: %v", err)
	}
	if err := ValidateCommandTokens(`x <env:GG_OP>`, false); err == nil {
		t.Error("<env:NAME> must error like <bin>")
	}
}

func TestResolveCommandContextFile(t *testing.T) {
	ctx := repoCtx()
	ctx.ContextFile = "/tmp/gg-context-1 2.txt"
	got, err := resolveCommandFor(`agent <context-file>`, nil, ctx, "linux")
	if err != nil {
		t.Fatal(err)
	}
	if want := `agent '/tmp/gg-context-1 2.txt'`; got != want {
		t.Errorf("posix: got %q, want %q", got, want)
	}
	got, err = resolveCommandFor(`agent <context-file>`, nil, ctx, "windows")
	if err != nil {
		t.Fatal(err)
	}
	if want := `agent "/tmp/gg-context-1 2.txt"`; got != want {
		t.Errorf("windows: got %q, want %q", got, want)
	}
	if _, err := resolveCommandFor(`agent <context-file>`, nil, repoCtx(), "linux"); err == nil {
		t.Error("empty ContextFile must error")
	}
}

func TestResolveCommandEnvTokenErrorsAtRuntime(t *testing.T) {
	_, err := resolveCommandFor(`<bin> --merge <env:GG_OP>`, nil, repoCtx(), "linux")
	if err == nil || !strings.Contains(err.Error(), "generated") {
		t.Errorf("<env:NAME> must error mentioning generation, got %v", err)
	}
}

func TestResolveRangeToken(t *testing.T) {
	got, err := ResolveCommand(`review <range>`, nil, CmdCtx{Range: "main..HEAD"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "review main..HEAD" {
		t.Fatalf("got %q, want %q", got, "review main..HEAD")
	}
	// prose token: NOT shell-quoted even though a range can contain '/'
	got, _ = ResolveCommand(`review <range>`, nil, CmdCtx{Range: "feature/x..main"})
	if got != "review feature/x..main" {
		t.Fatalf("range must substitute literally, got %q", got)
	}
	if err := ValidateCommandTokens("x <range>", false); err != nil {
		t.Fatalf("<range> should be a valid token: %v", err)
	}
}

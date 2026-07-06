package template

import (
	"fmt"
	"runtime"
	"strings"
)

// CmdCtx carries the values for external-tool command tokens. Prose fields
// (Op/Source/Target/Range/ConflictedFiles) substitute raw — the default
// commands embed them inside prompt strings, and git refnames/rev-ranges
// cannot contain spaces.
// Path fields (Repo/File/Local/Base/Remote/Merged/ContextFile) substitute
// shell-quoted: they sit in argv positions and may contain spaces. Per-file
// fields are "" for a repo-level command; using their tokens then is an
// error. ContextFile is the per-run context-file path (op/source/target plus
// the conflicted paths, byte-exact — see internal/tui's toolContextFile);
// empty ContextFile makes <context-file> an error too, since every
// conflict-category run creates one.
type CmdCtx struct {
	Op, Source, Target, Range, Repo   string
	ConflictedFiles                   []string
	File, Local, Base, Remote, Merged string
	ContextFile                       string
}

// ResolveCommand substitutes every <...> token in an external-tool command.
// inputs supplies <user:LABEL> values (raw). Unknown tokens, <bin> (a
// generation-time token), a missing user input, or a per-file token with no
// per-file context are errors — never silently passed through.
func ResolveCommand(tmpl string, inputs map[string]string, ctx CmdCtx) (string, error) {
	return resolveCommandFor(tmpl, inputs, ctx, runtime.GOOS)
}

func resolveCommandFor(tmpl string, inputs map[string]string, ctx CmdCtx, goos string) (string, error) {
	var firstErr error
	out := tokenRe.ReplaceAllStringFunc(tmpl, func(tok string) string {
		body := tok[1 : len(tok)-1]
		val, err := resolveCommandToken(body, inputs, ctx, goos)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		return val
	})
	if firstErr != nil {
		return "", firstErr
	}
	return out, nil
}

func resolveCommandToken(body string, inputs map[string]string, ctx CmdCtx, goos string) (string, error) {
	prefix, rest, hasColon := cutColon(body)
	switch prefix {
	case "op":
		return ctx.Op, nil
	case "source":
		return ctx.Source, nil
	case "target":
		return ctx.Target, nil
	case "conflicted-files":
		return strings.Join(ctx.ConflictedFiles, " "), nil
	case "range":
		return ctx.Range, nil
	case "repo":
		return quoteArgFor(ctx.Repo, goos), nil
	case "context-file":
		if ctx.ContextFile == "" {
			return "", fmt.Errorf("template: <context-file> requires a conflict context")
		}
		return quoteArgFor(ctx.ContextFile, goos), nil
	case "file", "local", "base", "remote", "merged":
		v := map[string]string{
			"file": ctx.File, "local": ctx.Local, "base": ctx.Base,
			"remote": ctx.Remote, "merged": ctx.Merged,
		}[prefix]
		if v == "" {
			return "", fmt.Errorf("template: <%s> requires a per-file conflict context", prefix)
		}
		return quoteArgFor(v, goos), nil
	case "user":
		if !hasColon {
			return "", fmt.Errorf("template: <user> requires a label, e.g. <user:hint>")
		}
		v, ok := inputs[rest]
		if !ok {
			return "", fmt.Errorf("template: missing input for <user:%s>", rest)
		}
		return v, nil
	case "bin":
		return "", fmt.Errorf("template: <bin> is resolved when the command is generated — replace it with the tool binary")
	case "env":
		return "", fmt.Errorf("template: <env:NAME> is resolved when the command is generated — write $NAME (or %%NAME%% on Windows) yourself in a config command")
	default:
		return "", fmt.Errorf("template: unknown command token <%s>", body)
	}
}

// commandTokens is the runtime vocabulary; the bool marks per-file-only tokens.
var commandTokens = map[string]bool{
	"op": false, "source": false, "target": false, "range": false, "conflicted-files": false,
	"repo": false, "context-file": false, "user": false,
	"file": true, "local": true, "base": true, "remote": true, "merged": true,
}

// ValidateCommandTokens checks a command template's token set without
// resolving values: unknown tokens, <bin>, <env:NAME>, and per-file tokens in
// a repo-level command are errors. Used to make a bad config block inert
// with a pointed message instead of failing at run time.
func ValidateCommandTokens(tmpl string, perFile bool) error {
	for _, m := range tokenRe.FindAllStringSubmatch(tmpl, -1) {
		prefix, _, _ := cutColon(m[1])
		if prefix == "bin" {
			return fmt.Errorf("template: <bin> is only valid in catalog templates; it is replaced when the command is generated")
		}
		if prefix == "env" {
			return fmt.Errorf("template: <env:NAME> is only valid in catalog templates; it is replaced when the command is generated")
		}
		perFileOnly, known := commandTokens[prefix]
		if !known {
			return fmt.Errorf("template: unknown command token <%s>", m[1])
		}
		if perFileOnly && !perFile {
			return fmt.Errorf("template: <%s> is only valid in a per_file command", prefix)
		}
	}
	return nil
}

// quoteArgFor shell-quotes one argv value: POSIX single-quoting with each
// embedded single quote escaped, or double quotes on Windows (cmd.exe).
func quoteArgFor(s, goos string) string {
	if goos == "windows" {
		return `"` + s + `"`
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

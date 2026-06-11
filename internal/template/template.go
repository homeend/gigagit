package template

import (
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"
)

// Ctx carries everything Resolve needs beyond the <user:> inputs. Now and Rand
// are injected so resolution is deterministic in tests. The resolver never
// mutates any field (notably Seqs).
type Ctx struct {
	ParentBranch string         // <parent-branch>
	Repo         string         // <repo>
	Branch       string         // <branch> (path templates only); "" means unset
	Seqs         map[string]int // current <seq:NAME> values, supplied by the caller
	Now          func() time.Time
	Rand         *rand.Rand
}

// Resolve substitutes every <...> token in tmpl. inputs supplies <user:LABEL>
// values. Unknown tokens, a <branch> token with an unset Ctx.Branch, a missing
// user input, or malformed token arguments are returned as errors (never
// silently passed through).
func Resolve(tmpl string, inputs map[string]string, ctx Ctx) (string, error) {
	var firstErr error
	out := tokenRe.ReplaceAllStringFunc(tmpl, func(tok string) string {
		body := tok[1 : len(tok)-1] // strip < >
		val, err := resolveToken(body, inputs, ctx)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return ""
		}
		return val
	})
	if firstErr != nil {
		return "", firstErr
	}
	return out, nil
}

func resolveToken(body string, inputs map[string]string, ctx Ctx) (string, error) {
	prefix, rest, hasColon := cutColon(body)
	switch prefix {
	case "parent-branch":
		return ctx.ParentBranch, nil
	case "repo":
		return ctx.Repo, nil
	case "branch":
		if ctx.Branch == "" {
			return "", fmt.Errorf("template: <branch> is only valid in path templates")
		}
		return sanitizeSegment(ctx.Branch), nil
	case "date":
		if !hasColon {
			return "", fmt.Errorf("template: <date> requires a format, e.g. <date:yyyy-MM-dd>")
		}
		return ctx.Now().Format(goLayout(rest)), nil
	case "seq":
		return resolveSeq(rest, ctx)
	case "user":
		if !hasColon {
			return "", fmt.Errorf("template: <user> requires a label, e.g. <user:issue-id>")
		}
		v, ok := inputs[rest]
		if !ok {
			return "", fmt.Errorf("template: missing input for <user:%s>", rest)
		}
		return v, nil
	case "random-alpha", "random-num":
		return resolveRandom(prefix, rest, hasColon, ctx)
	default:
		return "", fmt.Errorf("template: unknown token <%s>", body)
	}
}

// resolveSeq handles <seq:NAME> and <seq:NAME:N>. The value comes from ctx.Seqs
// (0 if absent); N zero-pads.
func resolveSeq(rest string, ctx Ctx) (string, error) {
	name, padStr, hasPad := cutColon(rest)
	if name == "" {
		return "", fmt.Errorf("template: <seq> requires a name, e.g. <seq:issue>")
	}
	n := ctx.Seqs[name]
	if !hasPad {
		return strconv.Itoa(n), nil
	}
	pad, err := strconv.Atoi(padStr)
	if err != nil || pad < 0 {
		return "", fmt.Errorf("template: <seq:%s:%s> padding must be a non-negative integer", name, padStr)
	}
	return fmt.Sprintf("%0*d", pad, n), nil
}

// sanitizeSegment makes a branch name safe as a single path segment ('/' -> '-').
func sanitizeSegment(branch string) string {
	return strings.ReplaceAll(branch, "/", "-")
}

// resolveRandom is fully implemented in the next task.
func resolveRandom(prefix, rest string, hasColon bool, ctx Ctx) (string, error) {
	return "", fmt.Errorf("template: %s not yet implemented", prefix)
}

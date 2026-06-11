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
// silently passed through). A <date:...> token requires Ctx.Now and a
// <random-*> token requires Ctx.Rand; if the corresponding field is nil,
// Resolve returns an error rather than panicking.
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
		if ctx.Now == nil {
			return "", fmt.Errorf("template: <date> requires Ctx.Now to be set")
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

const lowerAlpha = "abcdefghijklmnopqrstuvwxyz"
const digits = "0123456789"

// resolveRandom handles <random-alpha:N> (N lowercase letters) and
// <random-num:N> (N digits), drawing from ctx.Rand so seeded runs are
// reproducible. N must be a positive integer.
func resolveRandom(prefix, rest string, hasColon bool, ctx Ctx) (string, error) {
	if !hasColon {
		return "", fmt.Errorf("template: <%s> requires a length, e.g. <%s:4>", prefix, prefix)
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n <= 0 {
		return "", fmt.Errorf("template: <%s:%s> length must be a positive integer", prefix, rest)
	}
	if ctx.Rand == nil {
		return "", fmt.Errorf("template: <%s> requires Ctx.Rand to be set", prefix)
	}
	alphabet := lowerAlpha
	if prefix == "random-num" {
		alphabet = digits
	}
	b := make([]byte, n)
	for i := range b {
		b[i] = alphabet[ctx.Rand.IntN(len(alphabet))]
	}
	return string(b), nil
}

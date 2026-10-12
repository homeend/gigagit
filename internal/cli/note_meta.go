package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/notemeta"
)

// metaFlag collects repeated --meta k=v.
type metaFlag []notemeta.KV

func (m *metaFlag) String() string { return "" }

func (m *metaFlag) Set(v string) error {
	k, val, ok := strings.Cut(v, "=")
	if !ok || strings.TrimSpace(k) == "" {
		return fmt.Errorf("want key=value, got %q", v)
	}
	*m = append(*m, notemeta.KV{Key: strings.TrimSpace(k), Value: val})
	return nil
}

// checkedMeta is the CLI's strict limit check: any dropped entry is a usage
// error (exit 2), named on stderr.
func checkedMeta(sub string, m metaFlag, stderr io.Writer) ([]notemeta.KV, bool) {
	kv, probs := notemeta.CheckMeta(m)
	for _, p := range probs {
		fmt.Fprintf(stderr, "%s: --meta: %s\n", sub, p)
	}
	return kv, len(probs) == 0
}

// readDisplayArg reads --display: inline JSON, or @<file>.
func readDisplayArg(v string) (notemeta.Display, []notemeta.Problem, error) {
	if v == "" {
		return nil, nil, nil
	}
	data := []byte(v)
	if path, ok := strings.CutPrefix(v, "@"); ok {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, err
		}
		data = b
	}
	return notemeta.ParseDisplay(data)
}

// displayArg reads --display for sub: malformed = usage error, an unknown
// field = a warning on stderr.
func displayArg(sub, v string, stderr io.Writer) (notemeta.Display, bool) {
	d, probs, err := readDisplayArg(v)
	if err != nil {
		fmt.Fprintf(stderr, "%s: --display: %v\n", sub, err)
		return nil, false
	}
	for _, p := range probs {
		fmt.Fprintf(stderr, "%s: --display: %s (ignored)\n", sub, p)
	}
	return d, true
}

// noteMetaCmd is `gg note meta <id> set k=v… | unset k… | display <json|@file>`.
func noteMetaCmd(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	const usage = "usage: gg note meta [<repo-link>] <note-id|review:<id>:<n>> set <k=v>... | unset <k>... | display <json|@file> [--author <label>]"
	if len(args) < 2 {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	id, verb := latestRemarkID(svc, args[0]), args[1]
	fs := flag.NewFlagSet("note meta", flag.ContinueOnError)
	fs.SetOutput(stderr)
	author := fs.String("author", "", "who writes (default: $GG_AGENT, else agent)")
	pos, err := parseSteerFlags(fs, args[2:]) // flags may sit between the values
	if err != nil {
		return 2
	}
	by := noteAuthorDefault(*author)
	ctx := context.Background()
	switch verb {
	case "set":
		var m metaFlag
		for _, p := range pos {
			if serr := m.Set(p); serr != nil {
				fmt.Fprintln(stderr, "note meta set:", serr)
				return 2
			}
		}
		kv, ok := checkedMeta("note meta set", m, stderr)
		if !ok || len(kv) == 0 {
			fmt.Fprintln(stderr, usage)
			return 2
		}
		_, err = svc.SetNoteMeta(ctx, id, by, kv)
	case "unset":
		if len(pos) == 0 {
			fmt.Fprintln(stderr, usage)
			return 2
		}
		err = svc.UnsetNoteMeta(ctx, id, by, pos)
	case "display":
		if len(pos) != 1 {
			fmt.Fprintln(stderr, usage)
			return 2
		}
		d, ok := displayArg("note meta display", pos[0], stderr)
		if !ok {
			return 2
		}
		err = svc.SetNoteDisplay(ctx, id, d)
	default:
		fmt.Fprintln(stderr, usage)
		return 2
	}
	switch {
	case errors.Is(err, domain.ErrRemarkDisplay):
		fmt.Fprintf(stderr, "note meta: %s — set it in the review document\n", err)
		return 1
	case err != nil:
		return noteIDExit("note meta", id, svc, err, stderr)
	}
	fmt.Fprintf(stdout, "%s %s\n", verb, id)
	return 0
}

// noteMetaText is the resolved display as plain text lines (gg note show/list):
// shown keys, stored keys, added values with their writer, problems.
func noteMetaText(r notemeta.Resolved) []string {
	var shown []string
	for _, v := range r.Values {
		shown = append(shown, v.Key+": "+v.Value)
	}
	var out []string
	if len(shown) > 0 {
		out = append(out, "meta: "+strings.Join(shown, " · "))
	}
	for _, c := range r.Added {
		line := "added: " + c.Key + ": " + c.To
		if c.By != "" {
			line += " (" + c.By + ")"
		}
		if c.From != "" {
			line += ", was " + c.From
		}
		out = append(out, line)
	}
	for _, p := range r.Problems {
		out = append(out, "problem: "+string(p))
	}
	return out
}

package domain

import (
	"context"
	"errors"
	"fmt"

	"github.com/homeend/gigagit/internal/model"
)

// ErrLinkNoLines means a link names no line or range, so there is no text to
// hand out: a file, a commit, or a #<hunk>.
var ErrLinkNoLines = errors.New("the link names no line or range")

// LinkText is the text a line or range link names (`gg link text`).
type LinkText struct {
	Path   string
	Target string // "working tree", "index", <sha7>, <a7>..<b7>, <target>...<source>, ref:<name>
	Side   model.NoteSide
	Start  int
	End    int // == Start for a single line
	Lines  []string
}

// LinkText reads the lines res names from the version and side it names: the
// working file, the index or HEAD for an uncommitted link (linkSideLines, the
// resolver's own reader), otherwise the file at the commit — the parent for a
// plain commit's old side, commit a for a pair's. s must be the service of
// res.Checkout.
func (s *Service) LinkText(ctx context.Context, res Resolved) (LinkText, error) {
	if res.Addr.Path == "" || res.Line <= 0 {
		return LinkText{}, ErrLinkNoLines
	}
	out := LinkText{Path: res.Addr.Path, Side: res.Side, Start: res.Line, End: max(res.End, res.Line)}
	var lines []string
	ok := false
	switch {
	case res.Addr.State != model.StateCommitted:
		out.Target = "working tree"
		state := model.StateUnstaged
		if res.Addr.State == model.StateStaged {
			out.Target, state = "index", model.StateStaged
		}
		l := model.Link{Side: res.Side, Target: model.LinkTarget{State: state}}
		lines, ok = linkSideLines(ctx, s, l, res.Addr.Path)
	default:
		rev := res.Commit
		out.Target = shortSHA(res.Commit)
		switch {
		case res.Pair != nil:
			out.Target = shortSHA(res.Pair.A) + ".." + shortSHA(res.Pair.B)
			if res.Side == model.NoteSideOld {
				rev = res.Pair.A
			}
		case res.Preview != nil:
			out.Target = res.Preview.Target + "..." + res.Preview.Source
		case res.Ref != "":
			out.Target = "ref:" + res.Ref
			fallthrough
		default:
			if res.Side == model.NoteSideOld {
				rev += "^"
			}
		}
		data, err := s.ResolveBytes(ctx, model.FileRef{Source: model.SourceCommit, Locator: rev, Path: res.Addr.Path})
		if err != nil {
			return LinkText{}, err
		}
		lines, ok = splitTextLines(data)
	}
	if !ok {
		return LinkText{}, fmt.Errorf("%s cannot be read as text (missing, binary or too large)", res.Addr.Path)
	}
	if out.End > len(lines) {
		return LinkText{}, fmt.Errorf("%s has %d lines; the link names %s", res.Addr.Path, len(lines), lineSpan(out.Start, out.End))
	}
	out.Lines = lines[out.Start-1 : out.End]
	return out, nil
}

func lineSpan(a, b int) string {
	if b > a {
		return fmt.Sprintf("lines %d-%d", a, b)
	}
	return fmt.Sprintf("line %d", a)
}

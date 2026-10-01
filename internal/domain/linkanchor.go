package domain

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/textdiff"
)

// The states of a fingerprinted line link once resolved.
const (
	AnchorSame    = "same"    // the line still holds the text
	AnchorMoved   = "moved"   // the text is on another line now
	AnchorChanged = "changed" // the text is nowhere in the file
)

// LineAnchor says what became of a fingerprinted link's line. State "" means
// the link carried no fingerprint; Resolved.Line is then the link's own line.
type LineAnchor struct {
	Asked   int    // the line the link named
	State   string // "", AnchorSame, AnchorMoved, AnchorChanged
	Matches int    // lines carrying the fingerprint (0 when changed)
}

// anchorLine finds where the fingerprinted line is now. The asked line wins
// when it still matches; otherwise the nearest match (a tie: the lower line);
// with no match the asked line stands and the state is "changed".
func anchorLine(lines []string, asked int, fp string) (line int, state string, matches int) {
	best := 0
	for i, l := range lines {
		if model.LineFingerprint(l) != fp {
			continue
		}
		matches++
		n := i + 1
		if best == 0 || abs(n-asked) < abs(best-asked) {
			best = n
		}
	}
	switch {
	case matches == 0:
		return asked, AnchorChanged, 0
	case asked >= 1 && asked <= len(lines) && model.LineFingerprint(lines[asked-1]) == fp:
		return asked, AnchorSame, matches
	}
	return best, AnchorMoved, matches
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// AnchorNote is the one sentence every consumer says about a re-anchored
// link ("" when there is nothing to say). English: it is agent-facing.
func AnchorNote(asked, line int, state string, matches int) string {
	switch state {
	case AnchorMoved:
		if matches > 1 {
			return fmt.Sprintf("line %d moved to %d (nearest of %d matching lines)", asked, line, matches)
		}
		return fmt.Sprintf("line %d moved to %d", asked, line)
	case AnchorChanged:
		return fmt.Sprintf("line %d has changed since this link was copied", asked)
	}
	return ""
}

// AnchorNote is AnchorNote for this resolution.
func (r Resolved) AnchorNote() string {
	return AnchorNote(r.Anchor.Asked, r.Line, r.Anchor.State, r.Anchor.Matches)
}

// linkSideLines reads the text an uncommitted line link's side names, split
// into lines: the working file (new side, or a content link), the index (the
// unstaged diff's old side, the staged diff's new side) or HEAD (the staged
// diff's old side). false when it cannot be read — a deleted, binary or
// over-cap file.
func linkSideLines(ctx context.Context, svc *Service, l model.Link, path string) ([]string, bool) {
	ref := model.FileRef{Source: model.SourceUnstaged, Path: path}
	staged, old := l.Target.State == model.StateStaged, l.Side == model.NoteSideOld
	switch {
	case staged && old:
		ref = model.FileRef{Source: model.SourceCommit, Locator: "HEAD", Path: path}
	case staged || old:
		ref.Source = model.SourceStaged
	}
	data, err := svc.ResolveBytes(ctx, ref)
	if err != nil && staged && old {
		// A staged RENAME: the diff's old side is HEAD's text at the OLD path,
		// while the link names the new one.
		if orig := stagedRenameSource(ctx, svc, path); orig != "" {
			ref.Path = orig
			data, err = svc.ResolveBytes(ctx, ref)
		}
	}
	if err != nil {
		return nil, false
	}
	return splitTextLines(data)
}

// splitTextLines splits file bytes into the lines a link numbers, by the
// diff's own two rules, so the resolver never disagrees with the view the
// link was copied from: a side over MaxDiffBytes is not aligned (and not
// scanned here), and "binary" is a NUL in git's first 8000 bytes — a stray
// NUL further down is still text. false for either.
func splitTextLines(data []byte) ([]string, bool) {
	if len(data) > MaxDiffBytes || textdiff.IsBinary(data) {
		return nil, false
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.TrimRight(strings.ReplaceAll(text, "\r", "\n"), "\n")
	if text == "" {
		return nil, true
	}
	return strings.Split(text, "\n"), true
}

// stagedRenameSource is the path a staged rename moved path FROM ("" when
// path is not one).
func stagedRenameSource(ctx context.Context, svc *Service, path string) string {
	st, err := svc.Status(ctx)
	if err != nil {
		return ""
	}
	for _, f := range st.Files {
		if f.Path == path && f.OrigPath != "" {
			return f.OrigPath
		}
	}
	return ""
}

// LinkLineFingerprint is the fingerprint a PRODUCER puts on an uncommitted
// line link: that of the line l names, read from the side l names. "" for a
// link with no line, a committed target, an unreadable file, a line past the
// end or a blank line — the plain form is then the link.
func (s *Service) LinkLineFingerprint(ctx context.Context, l model.Link) string {
	if l.Line <= 0 || l.Path == "" || l.Target.State == model.StateCommitted {
		return ""
	}
	lines, ok := linkSideLines(ctx, s, l, l.Path)
	if !ok || l.Line > len(lines) {
		return ""
	}
	return model.LineFingerprint(lines[l.Line-1])
}

// ErrLinkStale means a RANGE link's block no longer holds the text it was
// copied from. A range is never re-found: the link is refused.
var ErrLinkStale = errors.New("the link is no longer valid")

// LinkBlockFingerprint is the fingerprint a PRODUCER puts on an uncommitted
// range link: that of lines l.Line..l.End, read from the side l names. "" for
// a link with no range, a committed target, an unreadable file, a range past
// the end or an all-blank block — the plain form is then the link.
func (s *Service) LinkBlockFingerprint(ctx context.Context, l model.Link) string {
	if l.Line <= 0 || l.End <= l.Line || l.Path == "" || l.Target.State == model.StateCommitted {
		return ""
	}
	lines, ok := linkSideLines(ctx, s, l, l.Path)
	if !ok || l.End > len(lines) {
		return ""
	}
	return model.BlockFingerprint(lines[l.Line-1 : l.End])
}

// anchorLink re-finds a fingerprinted link's line in the text its side names
// (linkSideLines); text that cannot be read is "changed". A RANGE is only
// checked, never re-found: a block that is not byte-for-trimmed-byte where
// the link says is ErrLinkStale (the user's ruling: do not guess).
func anchorLink(ctx context.Context, svc *Service, l model.Link, res *Resolved) error {
	if l.Fingerprint == "" || l.Line <= 0 || res.Addr.Path == "" || l.Target.State == model.StateCommitted {
		return nil
	}
	lines, ok := linkSideLines(ctx, svc, l, res.Addr.Path)
	if l.End > l.Line {
		if !ok || l.End > len(lines) || model.BlockFingerprint(lines[l.Line-1:l.End]) != l.Fingerprint {
			return fmt.Errorf("%w: lines %d-%d of %s have changed since it was copied", ErrLinkStale, l.Line, l.End, res.Addr.Path)
		}
		res.Anchor = LineAnchor{Asked: l.Line, State: AnchorSame, Matches: 1}
		return nil
	}
	res.Anchor = LineAnchor{Asked: l.Line, State: AnchorChanged}
	if !ok {
		return nil
	}
	res.Line, res.Anchor.State, res.Anchor.Matches = anchorLine(lines, l.Line, l.Fingerprint)
	return nil
}

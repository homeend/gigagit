package domain

import (
	"context"
	"fmt"
	"strings"

	"github.com/homeend/gigagit/internal/model"
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
// diff's old side). false when it cannot be read — a deleted or binary file.
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
	if err != nil || strings.IndexByte(string(data), 0) >= 0 {
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

// anchorLink re-finds a fingerprinted link's line in the text its side names
// (linkSideLines). Text that cannot be read is "changed".
func anchorLink(ctx context.Context, svc *Service, l model.Link, res *Resolved) {
	if l.Fingerprint == "" || l.Line <= 0 || res.Addr.Path == "" || l.Target.State == model.StateCommitted {
		return
	}
	res.Anchor = LineAnchor{Asked: l.Line, State: AnchorChanged}
	lines, ok := linkSideLines(ctx, svc, l, res.Addr.Path)
	if !ok {
		return
	}
	res.Line, res.Anchor.State, res.Anchor.Matches = anchorLine(lines, l.Line, l.Fingerprint)
}

package model

// LinkBoundKind says whether a link is an UNBOUNDED POINT that a base could
// bound, and which kind of base fits it (spec §5.1). It is pure so that the
// web's JS twin can be gated against it.
type LinkBoundKind int

const (
	// LinkBoundNone: nothing to bound — the link already is a finite set (a
	// path, a change-set, a merge preview), or it is a live state (the working
	// tree, the index) that has no base to be relative to.
	LinkBoundNone LinkBoundKind = iota
	// LinkBoundRef: a branch or tag TIP. Its base is another ref, and the
	// bounded form is the merge preview `@<base>...<ref>`.
	LinkBoundRef
	// LinkBoundCommit: one commit's whole tree. Its base is its parent, and
	// the bounded form is the change-set `@<parent>..<sha>`.
	LinkBoundCommit
)

// BoundKind classifies a PARSED link.
//
// One thing it cannot see: a parsed LOCAL-form link (gg:///abs/dir/f.go@…)
// holds its checkout and its file undivided in Repo.Abs with Path empty, so a
// local-form FILE link reads here as a whole tree. None is therefore certain
// and the other two are "unless this is a local-form file link" — a caller
// that must know locates the link first (domain.SuggestBase does).
func (l Link) BoundKind() LinkBoundKind {
	if l.Path != "" {
		return LinkBoundNone
	}
	// Ref and Commit are each set ONLY for their own target form (LinkTarget's
	// field docs): a change-set, a merge preview, the working tree and the
	// index all leave both empty, so they fall through to None with no test
	// of their own.
	switch {
	case l.Target.Ref != "":
		return LinkBoundRef
	case l.Target.Commit != "":
		return LinkBoundCommit
	}
	return LinkBoundNone
}

// WithBase rewrites an unbounded point into the bounded link a base makes of
// it, or refuses. It is the ONE place the rewrite lives — the result is a
// Link, rendered by String like every other, never assembled from text.
//
//   - a ref `A` with base ref `B`  →  `@B...A`  — TARGET FIRST, git's own
//     three-dot order and the order LinkPreview's fields spell out;
//   - a commit with its parent     →  `@<base>..<self>`, where self is the
//     commit's FULL sha. The link's own Commit may be abbreviated (a user
//     typed it); a pair half never is, so the caller supplies the full one.
//
// The repository, side and hint carry over. Anything else is refused: a base
// the grammar cannot hold, a ref bounded by itself, a short sha.
func (l Link) WithBase(base, self string) (Link, bool) {
	out := l
	switch l.BoundKind() {
	case LinkBoundRef:
		if !LinkRefOK(base) || base == l.Target.Ref {
			return Link{}, false
		}
		out.Target = LinkTarget{State: StateCommitted, Preview: &LinkPreview{Source: l.Target.Ref, Target: base}}
	case LinkBoundCommit:
		if !fullShaLink(base) || !fullShaLink(self) {
			return Link{}, false
		}
		out.Target = LinkTarget{State: StateCommitted, Pair: &LinkPair{A: base, B: self}}
	default:
		return Link{}, false
	}
	return out, true
}

// fullShaLink reports whether s is a FULL object id: 40 hex characters, or 64
// in a sha-256 repository.
func fullShaLink(s string) bool {
	return (len(s) == 40 || len(s) == 64) && isHexLink(s)
}

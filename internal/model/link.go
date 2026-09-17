package model

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// LinkScheme prefixes every gg link.
const LinkScheme = "gg://"

// ErrLink wraps every parse refusal, so a frontend can tell a malformed link
// from a resolution failure without matching on prose.
var ErrLink = errors.New("bad gg link")

// LinkRepo names the repository half of a link. Exactly one field is set.
type LinkRepo struct {
	// Name is the remote-named form: the last path segment of the chosen
	// remote's URL with a trailing ".git" stripped ("gigagit").
	Name string
	// Abs is the local form: an absolute checkout path in slash form.
	//
	// A PARSED local link carries the checkout AND the file path undivided
	// here — the grammar puts no delimiter between them, and splitting needs
	// this machine's repository registry — so Link.Path is empty for one. A
	// link BUILT by a producer sets Abs to the checkout alone and puts the
	// file in Link.Path. Both render to the same string; domain.ResolveLink
	// performs the split.
	Abs string
}

// LinkPreview is the merge-preview half of a link target: git's three-dot
// pair, spelled exactly as `git diff <target>...<source>` reads it. The NAMES
// travel between machines; the machine-local preview id does not (spec §2.1).
type LinkPreview struct {
	Source, Target string
}

// LinkPair is the two-dot half of a link target: a CHANGE-SET, git's
// `<a>..<b>`. Unlike LinkPreview (which is deliberately a pair of branch
// NAMES so it travels), each half here may be a sha or a refname: the common
// producer resolves a commit's first parent and emits two shas, while a
// human may reasonably type `@main..feat/x`.
//
// A pair is BOUNDED (spec §3.1): it evaluates to the files that differ
// between its two halves, not to a whole tree.
type LinkPair struct{ A, B string }

// LinkTarget is which pair of texts the link addresses: the working tree
// (index → file), the index (HEAD → index), a commit (parent → commit), a
// merge preview (merge-base → source tip), a branch/tag tip, or a change-set
// pair.
type LinkTarget struct {
	State FileState // StateUnstaged (default), StateStaged or StateCommitted
	// Commit is set iff State == StateCommitted AND Preview, Ref and Pair are
	// all nil/empty; 7..64 hex characters — 64, not 40, because a sha-256
	// repository's commit ids are 64 hex characters and every producer writes
	// the FULL sha.
	Commit string
	// Preview is set iff the target text carried git's three-dot pair
	// (<target>...<source>). State is StateCommitted and Commit is EMPTY: which
	// commit the preview addresses (the source tip) is a per-machine question
	// only domain.ResolveLink can answer. Link.Address() therefore yields a
	// meaningless zero-sha address for a preview link — callers use
	// domain.ResolveLink and read Resolved.Addr / Resolved.Preview instead.
	Preview *LinkPreview
	// Ref is set iff the target read "ref:<name>" — a branch or tag TIP.
	// State is StateCommitted and Commit is EMPTY: which commit the tip is
	// today is a per-machine, per-moment question only domain can answer.
	// A ref target is UNBOUNDED: it is a point, the whole tree at that tip.
	Ref string
	// Pair is set iff the target carried git's two-dot form (<a>..<b>).
	// State is StateCommitted and Commit is EMPTY. A pair is BOUNDED.
	Pair *LinkPair
}

// LinkHint is the HTML-anchor half of a link: WHICH UI surface it was copied
// from (spec §3.3). Three rules, in priority order:
//
//  1. Compare IGNORES the hint. A bookmarked commit and the same commit
//     picked off the log are one endpoint — there is no
//     bookmark × shelf × commit matrix, only the 2×2 of §3.5.
//  2. Navigate HONOURS it: same address, different landing.
//  3. It DEGRADES, never fails: on another machine the address still
//     resolves and the hint is dropped with a notice.
//
// The one exception is a shelved WORKING-TREE file, whose bytes were never in
// git: there the hint is the only content source, and the link has no address
// at all. That link cannot travel between machines, by construction.
type LinkHint struct {
	Kind string // "bookmark", "shelf" or "stash"; "" = no hint
	ID   string // the machine-local id; never empty when Kind is set
}

// String renders "kind=id", or "" for the zero value.
func (h LinkHint) String() string {
	if h.Kind == "" {
		return ""
	}
	return h.Kind + "=" + h.ID
}

// linkHintKinds is the closed set. A hint whose kind is not here is refused
// rather than carried: an unknown landing is a link this build cannot honour,
// and silently dropping it would make the link mean something else.
var linkHintKinds = map[string]bool{"bookmark": true, "shelf": true, "stash": true}

// Link is one place in one repository: a file, a line on one side of one
// diff, a hunk, or a commit.
type Link struct {
	Repo   LinkRepo
	Path   string // repo-relative git slash path; "" = the repo or a commit
	Target LinkTarget
	Side   NoteSide // NoteSideNew unless the link said "old:"
	Line   int      // 1-based; 0 = none
	Hunk   int      // 1-based; 0 = none
	Hint   LinkHint // "" Kind = no hint; the UI surface a link was copied from
}

// IsLocal reports whether the link names its repository by absolute path.
func (l Link) IsLocal() bool { return l.Repo.Abs != "" }

// LinkPathOK reports whether p can be expressed inside a link. A path holding
// '@', ':', '#' or '?' cannot: those are the grammar's own separators.
// Producers call this and refuse to copy rather than emit something that
// reparses as a different place.
func LinkPathOK(p string) bool { return !strings.ContainsAny(p, "@:#?") }

// LinkAbsOK reports whether an absolute CHECKOUT path can be expressed in the
// local link form. It is the checkout-half twin of LinkPathOK, and it is
// deliberately laxer: the grammar reads the first '@' as the target separator,
// the first '#' as the hunk separator and the first '?' as the hint
// separator, so none of the three may appear anywhere in the path — but a ':'
// is fine (a Windows drive colon is the whole reason splitLinkLine only
// accepts a NUMBER after the last ':', and a POSIX directory may legitimately
// contain one). A leading "X:" drive prefix is skipped before the scan for
// exactly that reason.
//
// Every local-form producer (tui.linkFor, `gg link`, web's repoSegment) calls
// it and refuses rather than emit a link ParseLink would reject or, worse,
// reparse as a different place.
func LinkAbsOK(abs string) bool {
	s := abs
	if len(s) >= 2 && isAlphaLink(s[0]) && s[1] == ':' {
		s = s[2:]
	} else if len(s) >= 3 && s[0] == '/' && isAlphaLink(s[1]) && s[2] == ':' {
		// The "gg:///C:/src" spelling: one leading separator, then the drive.
		s = s[3:]
	}
	return !strings.ContainsAny(s, "@#?")
}

// Address builds the FileAddress the link points at. Worktree is filled by
// the resolver, which is also the only thing that can fill Path for a PARSED
// local link (see LinkRepo.Abs).
//
// It is NOT meaningful for a PREVIEW, REF or PAIR link: in each case the
// commit is a per-machine (and, for a ref, per-moment) resolution, so the
// returned address carries an empty commit. domain.ResolveLink (the only
// caller in the tree) branches on Target.Preview/Ref/Pair before reaching it.
func (l Link) Address() FileAddress {
	return FileAddress{Path: l.Path, State: l.Target.State, Commit: l.Target.Commit}
}

// String renders the canonical text form. It is the exact inverse of
// ParseLink for the remote-named form; for the local form it is inverse up to
// the checkout/path split, which no pure function can make (String(Parse(s))
// == s always holds).
func (l Link) String() string {
	var b strings.Builder
	b.WriteString(LinkScheme)
	if l.Repo.Abs != "" {
		// POSIX: "gg://" + "/mnt/x" already yields three slashes. Windows:
		// "C:/src" needs the separator added.
		if !strings.HasPrefix(l.Repo.Abs, "/") {
			b.WriteByte('/')
		}
		b.WriteString(l.Repo.Abs)
	} else {
		b.WriteString(l.Repo.Name)
	}
	if l.Path != "" {
		b.WriteByte('/')
		b.WriteString(l.Path)
	}
	switch l.Target.State {
	case StateStaged:
		b.WriteString("@staged")
	case StateCommitted:
		if p := l.Target.Preview; p != nil {
			b.WriteByte('@')
			b.WriteString(p.Target)
			b.WriteString("...")
			b.WriteString(p.Source)
			break
		}
		if r := l.Target.Ref; r != "" {
			b.WriteString("@ref:")
			b.WriteString(r)
			break
		}
		if p := l.Target.Pair; p != nil {
			b.WriteByte('@')
			b.WriteString(p.A)
			b.WriteString("..")
			b.WriteString(p.B)
			break
		}
		// StateCommitted is FileState's ZERO value, so a Link nobody filled in
		// would otherwise render a bare "@". Only a real sha earns the target.
		if l.Target.Commit != "" {
			b.WriteByte('@')
			b.WriteString(l.Target.Commit)
		}
	}
	switch {
	case l.Hunk > 0:
		b.WriteByte('#')
		b.WriteString(strconv.Itoa(l.Hunk))
	case l.Line > 0:
		b.WriteByte(':')
		// A preview has no old side (steerCommandForLink and ParseLink both
		// treat it as new-only): a hand-built Link with Side == NoteSideOld
		// here would render an "old:" ParseLink rejects for a preview
		// target. Force the new side rather than emit a string this
		// function's own inverse cannot read back.
		if l.Side == NoteSideOld && l.Target.Preview == nil {
			b.WriteString("old:")
		}
		b.WriteString(strconv.Itoa(l.Line))
	}
	if h := l.Hint.String(); h != "" {
		b.WriteByte('?')
		b.WriteString(h)
	}
	return b.String()
}

// ParseLink reads the strict grammar:
//
//	gg://<repo>/<path>[@<target>][:<line>][?<hint>]   file / line
//	gg://<repo>/<path>[@<target>]#<hunk>[?<hint>]     hunk (git @@ order, 1-based)
//	gg://<repo>@<commit>[?<hint>]                     a commit, no path
//	gg://<repo>[/<path>]@<target>...<source>[:<line>|#<hunk>][?<hint>]  a merge preview
//	gg://<repo>[/<path>]@ref:<name>[:<line>|#<hunk>][?<hint>]           a branch/tag tip
//	gg://<repo>[/<path>]@<a>..<b>[?<hint>]            a change-set
//	gg://<repo>                                       the repository itself
//
// <repo> is a remote repository name, or "/" + an absolute checkout path.
// <target> is "staged", 7..64 hex (64 covers a sha-256 repository, whose
// commit ids every producer writes in full), "ref:<name>" (a branch or tag
// tip) or "<a>..<b>" (a change-set, each half a sha or a refname); absent
// means the working tree. <line> is "<n>" (new side) or "old:<n>". <hint> is
// "<kind>=<id>" naming the UI surface the link was copied from ("bookmark",
// "shelf" or "stash"); it never changes what the link addresses. Errors are
// English and wrap ErrLink.
func ParseLink(s string) (Link, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, LinkScheme) {
		return linkErr("a link starts with %s", LinkScheme)
	}
	body := s[len(LinkScheme):]
	l := Link{Side: NoteSideNew}

	// ?<hint> is the LAST element of the grammar, so it is stripped FIRST:
	// everything before it is an ordinary link, and nothing else in the
	// grammar may contain '?' (LinkPathOK / LinkAbsOK / LinkRefOK all reject
	// it), which is what makes the FIRST '?' unambiguously the separator.
	if i := strings.IndexByte(body, '?'); i >= 0 {
		h, err := parseLinkHint(body[i+1:])
		if err != nil {
			return Link{}, err
		}
		l.Hint = h
		body = body[:i]
	}

	// #<hunk> first: a path may not contain '#', so the first one is ours.
	if i := strings.IndexByte(body, '#'); i >= 0 {
		n, err := strconv.Atoi(body[i+1:])
		if err != nil || n < 1 {
			return linkErr("hunk must be a positive number, got %q", body[i+1:])
		}
		l.Hunk = n
		body = body[:i]
	}
	if body == "" {
		return linkErr("no repository")
	}

	// The FIRST '@' separates the target, so a path holding '@' can never be
	// mistaken for a path plus a target — it fails the target check instead.
	head, tail := body, ""
	hasTarget := false
	if i := strings.IndexByte(body, '@'); i >= 0 {
		head, tail, hasTarget = body[:i], body[i+1:], true
	}

	// The line rides the tail when there is a target, the head otherwise.
	var side NoteSide
	var line int
	var err error
	if hasTarget {
		tail, side, line, err = splitLinkLine(tail)
	} else {
		head, side, line, err = splitLinkLine(head)
	}
	if err != nil {
		return Link{}, err
	}
	if line > 0 {
		l.Side, l.Line = side, line
	}
	if l.Line > 0 && l.Hunk > 0 {
		return linkErr("a link carries a line or a hunk, not both")
	}

	switch {
	case !hasTarget:
		l.Target = LinkTarget{State: StateUnstaged}
	case tail == "staged":
		l.Target = LinkTarget{State: StateStaged}
	default:
		if i := strings.Index(tail, "..."); i >= 0 {
			tgt, src := tail[:i], tail[i+3:]
			if tgt == "" || src == "" {
				return linkErr("a merge preview names <target>...<source>, got %q", tail)
			}
			// Both halves hex and sha-shaped means the caller pasted two commit
			// ids. A preview is a pair of BRANCH names (spec §2.1): resolving a
			// sha pair would silently address a different thing on every machine.
			if isShaLink(tgt) && isShaLink(src) {
				return linkErr("a merge preview names two branches, not two shas, got %q", tail)
			}
			if !LinkRefOK(tgt) || !LinkRefOK(src) {
				return linkErr("%q is not a pair of branch names", tail)
			}
			// The preview's old side is the merge base, which no stored address
			// names (spec §1.1) — there is nothing for "old:" to point at.
			if l.Side == NoteSideOld {
				return linkErr("a merge preview addresses the new side only; drop \"old:\"")
			}
			l.Target = LinkTarget{State: StateCommitted, Preview: &LinkPreview{Source: src, Target: tgt}}
			break
		}
		if name, ok := strings.CutPrefix(tail, "ref:"); ok {
			if !LinkRefOK(name) {
				return linkErr("%q is not a branch or tag name", name)
			}
			l.Target = LinkTarget{State: StateCommitted, Ref: name}
			break
		}
		if i := strings.Index(tail, ".."); i >= 0 {
			a, bb := tail[:i], tail[i+2:]
			if a == "" || bb == "" {
				return linkErr("a change-set names <a>..<b>, got %q", tail)
			}
			// Each half is a sha or a refname. LinkRefOK already rejects the
			// grammar's separators AND ".." (Task 1 tightened it), so a half
			// that would reparse as a different pair cannot get through.
			okHalf := func(s string) bool { return isShaLink(s) || LinkRefOK(s) }
			if !okHalf(a) || !okHalf(bb) {
				return linkErr("%q is not a pair of commits or branch names", tail)
			}
			l.Target = LinkTarget{State: StateCommitted, Pair: &LinkPair{A: a, B: bb}}
			break
		}
		if !isHexLink(tail) || len(tail) < 7 || len(tail) > 64 {
			return linkErr("target must be \"staged\", a commit sha of 7 to 64 hex characters, \"ref:<name>\", <a>..<b> or <target>...<source>, got %q", tail)
		}
		l.Target = LinkTarget{State: StateCommitted, Commit: tail}
	}

	if strings.HasPrefix(head, "/") {
		abs := head
		// gg:///C:/src → the leading separator is the scheme's, not the
		// path's: strip it when a drive letter follows.
		if len(abs) >= 3 && isAlphaLink(abs[1]) && abs[2] == ':' {
			abs = abs[1:]
		}
		if abs == "" || abs == "/" {
			return linkErr("no repository")
		}
		l.Repo.Abs = abs
		return l, nil
	}

	name, path := head, ""
	if i := strings.IndexByte(head, '/'); i >= 0 {
		name, path = head[:i], head[i+1:]
	}
	if name == "" {
		return linkErr("no repository")
	}
	if strings.HasSuffix(path, "/") || strings.Contains(path, "//") {
		return linkErr("%q is not a git path", path)
	}
	// A remote-named link's path is the grammar's own <path>: it may not hold a
	// separator. (":" is the one that can still get this far — the target and
	// hunk separators were consumed above — and "gg://x/a.go:42:99" would
	// otherwise silently yield the path "a.go:42".) The LOCAL form is exempt:
	// its Repo.Abs legitimately carries a drive colon, and the checkout/path
	// split has not happened yet.
	if !LinkPathOK(path) {
		return linkErr("%q is not a git path: a path cannot contain @, : or #", path)
	}
	l.Repo.Name, l.Path = name, path
	if l.Path == "" && (l.Line > 0 || l.Hunk > 0) {
		return linkErr("a line or a hunk needs a file path")
	}
	return l, nil
}

// splitLinkLine strips a trailing ":<n>" or ":old:<n>" from t. A colon whose
// tail is not a number is left alone — that is how a Windows drive letter
// ("/C:/src/repo/f.go") survives.
func splitLinkLine(t string) (rest string, side NoteSide, line int, err error) {
	i := strings.LastIndexByte(t, ':')
	if i < 0 {
		return t, NoteSideNew, 0, nil
	}
	num := t[i+1:]
	n, cerr := strconv.Atoi(num)
	if cerr != nil {
		if num == "" {
			return "", "", 0, fmt.Errorf("%w: a line number is missing after \":\"", ErrLink)
		}
		return t, NoteSideNew, 0, nil
	}
	if n < 1 {
		return "", "", 0, fmt.Errorf("%w: a line must be a 1-based number, got %q", ErrLink, num)
	}
	rest, side = t[:i], NoteSideNew
	if j := strings.LastIndexByte(rest, ':'); j >= 0 && rest[j+1:] == "old" {
		rest, side = rest[:j], NoteSideOld
	}
	return rest, side, n, nil
}

// RepoNameFromURL takes the repository name out of a git remote URL: the last
// path segment with a trailing ".git" stripped. It handles scp-like syntax
// (git@github.com:homeend/gigagit.git), any scheme, backslash separators, a
// trailing slash and a plain local path. "" when nothing usable is left.
func RepoNameFromURL(url string) string {
	u := strings.TrimSpace(url)
	u = strings.TrimRight(u, "/\\")
	if u == "" {
		return ""
	}
	// scp-like URLs separate the path from the host with ':' and may carry no
	// '/' at all, so both separators (and Windows's '\') end a segment.
	if i := strings.LastIndexAny(u, `/:\`); i >= 0 {
		u = u[i+1:]
	}
	return strings.TrimSuffix(u, ".git")
}

func isHexLink(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// isShaLink reports whether s has the shape ParseLink accepts as a commit id.
func isShaLink(s string) bool { return isHexLink(s) && len(s) >= 7 && len(s) <= 64 }

// LinkRefOK reports whether a branch name can ride a preview link. The
// grammar's own separators ('@', ':', '#', '?'), whitespace and two
// consecutive dots are not expressible — every PRODUCER calls this and
// refuses to emit rather than print something ParseLink would reject or
// reparse as a different place. Two dots are refused (not just three)
// because git itself forbids ".." anywhere in a refname (git
// check-ref-format), and leaving it legal would let "main..feat" parse as a
// literal refname instead of failing.
func LinkRefOK(s string) bool {
	if s == "" || strings.Contains(s, "..") {
		return false
	}
	return !strings.ContainsAny(s, "@:#? \t")
}

// parseLinkHint reads "<kind>=<id>". Both halves are mandatory, the kind must
// be one linkHintKinds knows, and the id may not contain a grammar separator
// — a hint that cannot round-trip is refused at parse time rather than
// silently reshaped.
func parseLinkHint(s string) (LinkHint, error) {
	i := strings.IndexByte(s, '=')
	if i < 0 {
		return LinkHint{}, fmt.Errorf("%w: a hint reads <kind>=<id>, got %q", ErrLink, s)
	}
	kind, id := s[:i], s[i+1:]
	if !linkHintKinds[kind] {
		return LinkHint{}, fmt.Errorf("%w: unknown hint kind %q (want bookmark, shelf or stash)", ErrLink, kind)
	}
	if id == "" {
		return LinkHint{}, fmt.Errorf("%w: hint %q has no id", ErrLink, kind)
	}
	if strings.ContainsAny(id, "@:#?/ \t") {
		return LinkHint{}, fmt.Errorf("%w: %q is not a hint id", ErrLink, id)
	}
	return LinkHint{Kind: kind, ID: id}, nil
}

func isAlphaLink(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func linkErr(format string, args ...any) (Link, error) {
	return Link{}, fmt.Errorf("%w: "+format, append([]any{ErrLink}, args...)...)
}

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

// LinkTarget is which pair of texts the link addresses: the working tree
// (index → file), the index (HEAD → index), a commit (parent → commit), or a
// merge preview (merge-base → source tip).
type LinkTarget struct {
	State FileState // StateUnstaged (default), StateStaged or StateCommitted
	// Commit is set iff State == StateCommitted AND Preview is nil; 7..64 hex
	// characters — 64, not 40, because a sha-256 repository's commit ids are 64
	// hex characters and every producer writes the FULL sha.
	Commit string
	// Preview is set iff the target text carried git's three-dot pair
	// (<target>...<source>). State is StateCommitted and Commit is EMPTY: which
	// commit the preview addresses (the source tip) is a per-machine question
	// only domain.ResolveLink can answer. Link.Address() therefore yields a
	// meaningless zero-sha address for a preview link — callers use
	// domain.ResolveLink and read Resolved.Addr / Resolved.Preview instead.
	Preview *LinkPreview
}

// Link is one place in one repository: a file, a line on one side of one
// diff, a hunk, or a commit.
type Link struct {
	Repo   LinkRepo
	Path   string // repo-relative git slash path; "" = the repo or a commit
	Target LinkTarget
	Side   NoteSide // NoteSideNew unless the link said "old:"
	Line   int      // 1-based; 0 = none
	Hunk   int      // 1-based; 0 = none
}

// IsLocal reports whether the link names its repository by absolute path.
func (l Link) IsLocal() bool { return l.Repo.Abs != "" }

// LinkPathOK reports whether p can be expressed inside a link. A path holding
// '@', ':' or '#' cannot: those are the grammar's own separators. Producers
// call this and refuse to copy rather than emit something that reparses as a
// different place.
func LinkPathOK(p string) bool { return !strings.ContainsAny(p, "@:#") }

// LinkAbsOK reports whether an absolute CHECKOUT path can be expressed in the
// local link form. It is the checkout-half twin of LinkPathOK, and it is
// deliberately laxer: the grammar reads the first '@' as the target separator
// and the first '#' as the hunk separator, so neither may appear anywhere in
// the path — but a ':' is fine (a Windows drive colon is the whole reason
// splitLinkLine only accepts a NUMBER after the last ':', and a POSIX
// directory may legitimately contain one). A leading "X:" drive prefix is
// skipped before the scan for exactly that reason.
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
	return !strings.ContainsAny(s, "@#")
}

// Address builds the FileAddress the link points at. Worktree is filled by
// the resolver, which is also the only thing that can fill Path for a PARSED
// local link (see LinkRepo.Abs).
//
// It is NOT meaningful for a PREVIEW link: the source tip is resolved per
// machine, so the returned address carries an empty commit. domain.ResolveLink
// (the only caller in the tree) branches on Target.Preview before reaching it.
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
		if l.Side == NoteSideOld {
			b.WriteString("old:")
		}
		b.WriteString(strconv.Itoa(l.Line))
	}
	return b.String()
}

// ParseLink reads the strict grammar:
//
//	gg://<repo>/<path>[@<target>][:<line>]   file / line
//	gg://<repo>/<path>[@<target>]#<hunk>     hunk (git @@ order, 1-based)
//	gg://<repo>@<commit>                     a commit, no path
//	gg://<repo>[/<path>]@<target>...<source>[:<line>|#<hunk>]  a merge preview
//	gg://<repo>                              the repository itself
//
// <repo> is a remote repository name, or "/" + an absolute checkout path.
// <target> is "staged" or 7..64 hex (64 covers a sha-256 repository, whose
// commit ids every producer writes in full); absent means the working tree. <line> is
// "<n>" (new side) or "old:<n>". Errors are English and wrap ErrLink.
func ParseLink(s string) (Link, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, LinkScheme) {
		return linkErr("a link starts with %s", LinkScheme)
	}
	body := s[len(LinkScheme):]
	l := Link{Side: NoteSideNew}

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
		if !isHexLink(tail) || len(tail) < 7 || len(tail) > 64 {
			return linkErr("target must be \"staged\", a commit sha of 7 to 64 hex characters, or <target>...<source>, got %q", tail)
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
// grammar's own separators ('@', ':', '#'), whitespace and a second "..." are
// not expressible — every PRODUCER calls this and refuses to emit rather than
// print something ParseLink would reject or reparse as a different place.
func LinkRefOK(s string) bool {
	if s == "" || strings.Contains(s, "...") {
		return false
	}
	return !strings.ContainsAny(s, "@:# \t")
}

func isAlphaLink(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func linkErr(format string, args ...any) (Link, error) {
	return Link{}, fmt.Errorf("%w: "+format, append([]any{ErrLink}, args...)...)
}

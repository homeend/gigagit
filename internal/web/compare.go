package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// Branch ↔ branch comparison: the whole tip-to-tip changed-file list, each
// file tagged with which side actually touched it since the two diverged.
// Each side may also name a remote-tracking branch or a tag (compareNamedEndpoint) —
// not an arbitrary rev.
//
// Both names are resolved to TIP HASHES here, and the client's per-file diffs
// then run against those hashes rather than the names. That is the TUI's rule
// (openBranchCompare): a branch NAME in a diff endpoint poisons the
// session-lived diff cache, which treats a commit↔commit pair as immutable —
// commit to the branch, re-open the same compare, and the stale diff comes
// back.
//
// The names are resolved against the server's own branch/tag/remote lists
// rather than merely sanitized (the solo / remove-worktree allowlist
// precedent): isGitArgSafe covers argv, but an unknown name would yield an
// empty compare indistinguishable from "these two sides are identical".

// compareFile is one changed path in a branch comparison.
type compareFile struct {
	Path    string `json:"path"`
	Status  string `json:"status"`
	OldPath string `json:"old_path,omitempty"`
	// Origin says which side changed this path since the merge base: "a",
	// "b" or "both". Empty when the origin sets are unavailable (unrelated
	// histories), which the response reports separately.
	Origin string `json:"origin,omitempty"`
}

// mustCommitEndpoint and mustShelfEndpoint build an Endpoint from a hash/id
// this package has already validated (isHexSha, or an id read back from a gg
// store) — a constructor refusal here would mean the guard and the
// constructor disagree, which is a programming error, not bad request input.
//
// A value whose WIDTH depends on repo config must never reach mustCommitEndpoint:
// git's short-hash formats (`%(objectname:short)` behind model.Branch.Hash,
// `%h` behind model.LogLine.Hash) honour core.abbrev, whose legal minimum is 4
// — below CommitEndpoint's 7..64 floor. A must* there turns a repo setting
// into a panicking handler (see branchTipEndpoint, and the TUI's
// openBranchCompare). isHexSha already enforces 7..64, which is why the hex
// lanes below may use it.
func mustCommitEndpoint(hash string) model.Endpoint {
	e, err := model.CommitEndpoint(hash)
	if err != nil {
		panic(err)
	}
	return e
}

func mustShelfEndpoint(id string) model.Endpoint {
	e, err := model.ShelfEndpoint(id)
	if err != nil {
		panic(err)
	}
	return e
}

// branchTip returns name's tip hash, or "" when no such local branch exists.
// The hash is `%(objectname:short)` — a PRESENCE test's worth of value, not
// something to build an Endpoint from (see branchTipEndpoint).
func branchTip(bs []model.Branch, name string) string {
	for _, b := range bs {
		if b.Name == name {
			return b.Hash
		}
	}
	return ""
}

// branchTipEndpoint resolves an already-allowlisted local branch name to its
// FULL tip sha and that sha's commit endpoint. It returns the HTTP status for
// the error case.
//
// ResolveRev, not model.Branch.Hash: internal/git/repo.go reads a branch tip as
// `%(objectname:short)`, whose width honours core.abbrev (legal down to 4),
// below model.CommitEndpoint's 7..64 floor — so the tip made a legal repo
// config a dead request. This is the CLI's pattern (internal/cli/compare.go).
// The full sha is also the better cache key: Endpoint.CacheTag() is the
// session diff-cache key, and the response hands these hashes to the client
// for its per-file diffs.
//
// The name is resolved as `refs/heads/<name>` rather than bare: a tag of the
// same name outranks a branch in git's rev-parse disambiguation, the same
// collision internal/git/repo.go's Branches comment guards against.
func branchTipEndpoint(ctx context.Context, svc *domain.Service, name string) (model.Endpoint, string, int, error) {
	sha, ok, err := svc.ResolveRev(ctx, "refs/heads/"+name)
	if err != nil {
		return model.Endpoint{}, "", http.StatusInternalServerError, err
	}
	if !ok {
		return model.Endpoint{}, "", http.StatusNotFound, errors.New("unknown branch")
	}
	ep, err := model.CommitEndpoint(sha)
	if err != nil {
		return model.Endpoint{}, "", http.StatusInternalServerError, err
	}
	return ep, sha, 0, nil
}

// remoteBranchTip returns name's tip hash from a remote-tracking list, or ""
// if no such row exists — the RemoteBranch twin of branchTip.
func remoteBranchTip(rs []model.RemoteBranch, name string) string {
	for _, r := range rs {
		if r.Name == name {
			return r.Hash
		}
	}
	return ""
}

// tagNamed reports whether name is one of the repo's tags — the allowlist
// check for the tag arm of compareNamedEndpoint, mirroring branchTip's role
// for branches.
func tagNamed(tags []model.Tag, name string) bool {
	for _, t := range tags {
		if t.Name == name {
			return true
		}
	}
	return false
}

// compareNamedEndpoint resolves one already-allowlisted NAME — a local
// branch, a remote-tracking branch, or a tag — to its FULL commit sha and
// endpoint, for the plain (non-revs) compare lane. The allowlist stays
// name-specific (an unknown name must not yield an empty compare
// indistinguishable from "these two sides are identical" — the same
// rationale as knownRefName's), checked against the three lists FIRST;
// only a name found in one of them is resolved. A local or remote branch
// resolves via its own ref directly (it already points straight at a
// commit, branchTipEndpoint's rule); a tag resolves via
// refs/tags/<name>^{commit} — the peel an ANNOTATED tag needs to reach its
// commit (a lightweight tag's ref already IS the commit, and ^{commit} on
// a commit is a no-op, so one form covers both). Every resolution goes
// through ResolveRev, never the caller's own row: the client only ever
// carries %h / %(objectname:short) values (core.abbrev, legal down to 4),
// well under CommitEndpoint's 7..64 floor.
//
// The switch checks local branches, then remotes, then tags — DELIBERATELY
// not git's own tags-before-heads disambiguation order. A local branch
// always wins a same-named tag here, continuing branchTipEndpoint's own
// rule (its doc comment: "a tag of the same name outranks a branch in
// git's rev-parse disambiguation"); a name found nowhere in these three
// lists is refused rather than silently falling through to some other
// git-visible ref.
func compareNamedEndpoint(ctx context.Context, svc *domain.Service, branches []model.Branch, tags []model.Tag, remotes []model.RemoteBranch, name string) (model.Endpoint, string, int, error) {
	switch {
	case branchTip(branches, name) != "":
		return branchTipEndpoint(ctx, svc, name)
	case remoteBranchTip(remotes, name) != "":
		return resolveRefEndpoint(ctx, svc, "refs/remotes/"+name)
	case tagNamed(tags, name):
		return resolveRefEndpoint(ctx, svc, "refs/tags/"+name+"^{commit}")
	}
	return model.Endpoint{}, "", http.StatusNotFound, errors.New("unknown branch")
}

// resolveRefEndpoint resolves ref — an exact git ref/rev EXPRESSION built
// entirely from an already-allowlisted name, never a raw client value — to
// its full sha and commit endpoint.
func resolveRefEndpoint(ctx context.Context, svc *domain.Service, ref string) (model.Endpoint, string, int, error) {
	sha, ok, err := svc.ResolveRev(ctx, ref)
	if err != nil {
		return model.Endpoint{}, "", http.StatusInternalServerError, err
	}
	if !ok {
		return model.Endpoint{}, "", http.StatusNotFound, errors.New("unknown ref")
	}
	hash := strings.TrimSpace(sha)
	ep, err := model.CommitEndpoint(hash)
	if err != nil {
		return model.Endpoint{}, "", http.StatusInternalServerError, err
	}
	return ep, hash, 0, nil
}

// pathOrigin classifies a changed path against the two origin sets. A rename
// counts on either of its paths, matching the TUI's filterCompareFiles.
func pathOrigin(o model.CompareOrigins, f model.CommitFile) string {
	in := func(set map[string]bool) bool {
		return set[f.Path] || (f.OldPath != "" && set[f.OldPath])
	}
	a, b := in(o.APaths), in(o.BPaths)
	switch {
	case a && b:
		return "both"
	case a:
		return "a"
	case b:
		return "b"
	}
	return ""
}

func (s *Server) handleCompare(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	q := r.URL.Query()
	a, b := q.Get("a"), q.Get("b")
	if a == "" || b == "" {
		writeErr(w, http.StatusBadRequest, errors.New("a and b are required"))
		return
	}
	var aHash, bHash string
	var aEP, bEP model.Endpoint
	if q.Get("revs") == "1" {
		// The rev form: both sides are plain hex object ids (the commit-edit
		// isHexSha guard), used directly — the version ↔ tip compare's
		// transport. The name allowlist below deliberately does NOT apply:
		// its rationale is name-specific (an unknown name yields an empty
		// compare indistinguishable from "identical"), while an unknown hash
		// makes CompareFiles fail loudly. Hex-only is also a stricter argv
		// gate than isGitArgSafe — a hash cannot be a flag or a range.
		if !isHexSha(a) || !isHexSha(b) {
			writeErr(w, http.StatusBadRequest, errors.New("revs must be hex commit ids"))
			return
		}
		aHash, bHash = a, b
		aEP, bEP = mustCommitEndpoint(aHash), mustCommitEndpoint(bHash) // isHexSha: 7..64 hex
	} else {
		if !isGitArgSafe(a) || !isGitArgSafe(b) {
			writeErr(w, http.StatusBadRequest, errors.New("invalid branch"))
			return
		}
		// The lists are the allowlist check; the endpoints are built from
		// the names' FULL shas (compareNamedEndpoint), never a client-carried
		// abbreviated row. A local branch, a remote-tracking branch or a tag
		// may name either side — NOT an arbitrary rev: the allowlist stays
		// name-specific for the reason the doc comment above gives.
		branches, err := svc.Branches(r.Context())
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		// Tags and remotes are fetched LAZILY, only when BOTH sides are not
		// already local branches: Tags() "forces git to read every tag
		// object — seconds on a huge pack" (its own doc comment; this
		// project targets 100GB monorepos), and RemoteBranches is a
		// known-fallible read (domain's TestSnapshotRemoteBranchesBestEffort
		// exists because of it). An ordinary branch/branch compare — the
		// sidebar's "compare src ↔ dst" row, and every pair before this
		// review round — must keep the exact cost and failure surface it
		// always had, never pay for or fail on a list it does not need.
		var tags []model.Tag
		var remotes []model.RemoteBranch
		if branchTip(branches, a) == "" || branchTip(branches, b) == "" {
			if tags, err = svc.Tags(r.Context()); err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
			if remotes, err = svc.RemoteBranches(r.Context()); err != nil {
				writeErr(w, http.StatusInternalServerError, err)
				return
			}
		}
		var code int
		if aEP, aHash, code, err = compareNamedEndpoint(r.Context(), svc, branches, tags, remotes, a); err != nil {
			writeErr(w, code, err)
			return
		}
		if bEP, bHash, code, err = compareNamedEndpoint(r.Context(), svc, branches, tags, remotes, b); err != nil {
			writeErr(w, code, err)
			return
		}
	}
	files, err := svc.CompareFiles(r.Context(), aEP, bEP)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	origins, originsErr := svc.CompareOrigins(r.Context(), aHash, bHash)
	out := make([]compareFile, len(files))
	for i, f := range files {
		out[i] = compareFile{Path: f.Path, Status: f.Status, OldPath: f.OldPath}
		if originsErr == nil {
			out[i].Origin = pathOrigin(origins, f)
		}
	}
	payload := map[string]any{
		"a": a, "b": b,
		"a_hash": aHash, "b_hash": bHash,
		"files": out,
	}
	// The comparison itself stands without the origin sets — only the
	// per-side filter goes away, so the failure is reported alongside the
	// files rather than instead of them (the TUI keeps the view and makes f
	// inert the same way).
	if originsErr != nil {
		payload["origins_error"] = originsErrText(originsErr)
	}
	writeJSON(w, payload)
}

// originsErrText is the client-facing reason the per-side filter is off.
func originsErrText(err error) string {
	if errors.Is(err, domain.ErrNoMergeBase) {
		return "no common ancestor"
	}
	return err.Error()
}

// --- comparing against a stored entry ---------------------------------------
//
// gg's two stores already hold things worth comparing against: a bookmark
// points at a commit or a file as it was addressed, a shelf entry freezes the
// bytes. Until now the compare view could only ever hold two live commits.
//
// Two lanes, because the two questions are different:
//
//	/api/compare-entry   a stored COMMIT entry against a live commit —
//	                     the whole changed-file list, the branch-compare view
//	/api/entry-diff      ONE file, between any two addressable sides —
//	                     what a stored copy and the file here differ by
//
// Both resolve their sides SERVER-side from a store name and an id checked
// against an allowlist. The wire never carries a path into gg's state
// directory, and a client cannot ask for bytes by naming a file in it.
func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/compare-entry", s.handleCompareEntry)
		mux.HandleFunc("GET /api/entry-diff", s.handleEntryDiff)
	})
}

// entrySide is one resolved side of an entry comparison, ready for both the
// wire (spec/label) and the diff (bytes).
type entrySide struct {
	spec  string // what the client sends back to /api/entry-diff for this side
	label string // what the header shows
	// bytes resolves this side's content for path. A side that needs no path
	// (a shelved file's blob) ignores it.
	bytes func(ctx context.Context, path string) ([]byte, error)
	// tag is the cache-key fragment; live is true when the content can change
	// under us (the working tree, the index, a bookmark that resolves live),
	// in which case the diff must not be cached at all.
	tag  string
	live bool
}

// parseEntrySide resolves one wire spec. The vocabulary is closed:
//
//	worktree | staged | commit:<hex> | bookmark:<id> | shelf:<id>
//
// An unknown form is refused rather than guessed at. The int is the HTTP
// status for the error.
func parseEntrySide(ctx context.Context, svc *domain.Service, spec string) (entrySide, int, error) {
	switch {
	case spec == "worktree":
		return entrySide{spec: spec, label: "working tree", live: true, tag: "worktree",
			bytes: func(ctx context.Context, path string) ([]byte, error) {
				return svc.ResolveBytes(ctx, model.FileRef{Source: model.SourceUnstaged, Path: path})
			}}, 0, nil
	case spec == "staged":
		return entrySide{spec: spec, label: "staged", live: true, tag: "index",
			bytes: func(ctx context.Context, path string) ([]byte, error) {
				return svc.ResolveBytes(ctx, model.FileRef{Source: model.SourceStaged, Path: path})
			}}, 0, nil
	case strings.HasPrefix(spec, "commit:"):
		hash := strings.TrimPrefix(spec, "commit:")
		if !isHexSha(hash) {
			return entrySide{}, http.StatusBadRequest, errors.New("commit: must be a hex commit id")
		}
		return commitEntrySide(svc, mustCommitEndpoint(hash), shortSha(hash))
	case strings.HasPrefix(spec, "bookmark:"):
		return bookmarkEntrySide(ctx, svc, strings.TrimPrefix(spec, "bookmark:"))
	case strings.HasPrefix(spec, "shelf:"):
		return shelfEntrySide(ctx, svc, strings.TrimPrefix(spec, "shelf:"))
	}
	return entrySide{}, http.StatusBadRequest, fmt.Errorf("unknown compare side %q", spec)
}

// commitEntrySide serves a commit or a frozen shelf endpoint through the
// endpoint's own FileRef mapping, so the shelf lane cannot drift from the one
// domain uses for the file list.
// The wire spec is decided per KIND, with no default arm: this used to read
// "shelf when the kind is shelf, otherwise commit:" + ep.Hash(), which wired a
// ref or a pair endpoint to the browser as `commit:` with an EMPTY hash — a
// side the client then asks about and gets nothing for, with no error anywhere.
// The endpoint comes from domain, never from the request, so a kind this wire
// cannot spell is a SERVER bug and reports as one (500).
func commitEntrySide(svc *domain.Service, ep model.Endpoint, label string) (entrySide, int, error) {
	var spec string
	switch ep.Kind() {
	case model.EndpointShelf:
		spec = "shelf:" + ep.ShelfID()
	case model.EndpointCommit:
		spec = "commit:" + ep.Hash()
	case model.EndpointWorkTree, model.EndpointIndex, model.EndpointRef, model.EndpointPair, model.EndpointInvalid:
		// The live states have their own specs ("worktree"/"staged") built in
		// parseEntrySide, and a ref or a pair has no spec in this lane's
		// vocabulary at all — the whole-tree entry compare is defined over
		// stored commit entries.
		//
		// %d, NOT ep.Display(): Display has no EndpointInvalid arm and PANICS on
		// the zero endpoint, and internal/web has no recover() middleware — so
		// the one kind this arm exists to report would have dropped the client's
		// connection instead of answering 500.
		return entrySide{}, http.StatusInternalServerError,
			fmt.Errorf("compare side: endpoint kind %d cannot be addressed as a stored commit entry", ep.Kind())
	default:
		return entrySide{}, http.StatusInternalServerError,
			fmt.Errorf("compare side: unknown endpoint kind %d", ep.Kind())
	}
	return entrySide{spec: spec, label: label, tag: ep.CacheTag(),
		bytes: func(ctx context.Context, path string) ([]byte, error) {
			return svc.ResolveBytes(ctx, ep.FileRef(path))
		}}, 0, nil
}

// bookmarkEntrySide resolves a bookmark. A COMMIT bookmark is its commit —
// including the case where that commit is gone, which surfaces as the store's
// own "commit … no longer exists" rather than an empty diff (a bookmark keeps
// no blobs, so there is nothing to fall back to). A FILE bookmark resolves
// however it is addressed, which is the point of a bookmark: it says what is
// there NOW at that address. Only an entry that cannot move (a permanent blob
// checksum, or a shelf-backed address) is cacheable.
func bookmarkEntrySide(ctx context.Context, svc *domain.Service, id string) (entrySide, int, error) {
	b, err := svc.BookmarkGet(ctx, id)
	if err != nil {
		return entrySide{}, http.StatusNotFound, err
	}
	label := b.Label
	if label == "" {
		label = b.Address().Display()
	}
	if b.IsCommit() {
		ep, rerr := svc.ResolveCommitEntryEndpoint(ctx, b.Commit, "")
		if rerr != nil {
			return entrySide{}, http.StatusUnprocessableEntity, rerr
		}
		return commitEntrySide(svc, ep, label)
	}
	frozen := b.SHA != "" || b.State == model.StateShelf
	return entrySide{spec: "bookmark:" + b.ID, label: label, live: !frozen, tag: "bookmark:" + b.ID,
		bytes: func(ctx context.Context, _ string) ([]byte, error) {
			return svc.BookmarkBytes(ctx, b)
		}}, 0, nil
}

// shelfEntrySide resolves a shelf entry. A shelved COMMIT holds a tar of the
// commit's changed files, so it is addressed per path; a shelved FILE is one
// blob and ignores the path it is asked for — asking the tar lane for a file
// entry's bytes would simply fail.
func shelfEntrySide(ctx context.Context, svc *domain.Service, id string) (entrySide, int, error) {
	e, err := svc.ShelfFind(ctx, id)
	if err != nil {
		return entrySide{}, http.StatusNotFound, err
	}
	label := e.Label
	if label == "" {
		label = e.Origin.Display()
	}
	if e.IsCommit() {
		return commitEntrySide(svc, mustShelfEndpoint(e.ID), label)
	}
	return entrySide{spec: "shelf:" + e.ID, label: label, tag: "shelf:" + e.ID,
		bytes: func(ctx context.Context, _ string) ([]byte, error) {
			return svc.ShelfBlob(ctx, e.ID)
		}}, 0, nil
}

func shortSha(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// handleEntryDiff renders ONE file's side-by-side diff between two entry
// sides. It is /api/diff's shape (writeDiffJSON) over a wider vocabulary: the
// existing endpoint can only address git objects, and half of what is worth
// comparing here lives in gg's own stores.
//
// status has the same meaning as everywhere else, relative to the right side:
// A = absent on the left, D = absent on the right. Live sides resolve
// leniently (a file that is simply not there reads as empty, the working-tree
// diff's rule); a frozen side's failure is real and surfaces.
func (s *Server) handleEntryDiff(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	q := r.URL.Query()
	path := q.Get("path")
	if path == "" || !isGitArgSafe(path) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid path"))
		return
	}
	// A rename's LEFT side lives at its OLD path. Without this the left cell
	// of an R row read the new path at the old revision: absent, so the whole
	// file looked added.
	leftPath := leftPathOf(q.Get("old_path"), path)
	if !isGitArgSafe(leftPath) {
		writeErr(w, http.StatusBadRequest, errors.New("invalid old_path"))
		return
	}
	ctx := r.Context()
	left, code, err := parseEntrySide(ctx, svc, q.Get("left"))
	if err != nil {
		writeErr(w, code, err)
		return
	}
	right, code, err := parseEntrySide(ctx, svc, q.Get("right"))
	if err != nil {
		writeErr(w, code, err)
		return
	}
	status := q.Get("status")
	var oldSrc, newSrc domain.ByteSource
	if status != "A" {
		oldSrc = maybeLenient(left, func(ctx context.Context) ([]byte, error) { return left.bytes(ctx, leftPath) })
	}
	if status != "D" {
		newSrc = maybeLenient(right, func(ctx context.Context) ([]byte, error) { return right.bytes(ctx, path) })
	}
	// Cache key: never a name that can be re-pointed. For commit↔commit it is
	// deliberately byte-for-byte handleRevDiff's key, so the two endpoints
	// share cache entries for the same pair instead of each paying for its
	// own. A live side disables caching entirely (Key "").
	key := ""
	if !left.live && !right.live {
		key = left.tag + ".." + right.tag + ":" + path
		if leftPath != path {
			// Its own entry: the same pair and path without old_path is a
			// different (and wrong) diff, and must never be served for this one.
			key += "<" + leftPath
		}
	}
	d, derr := svc.Differ().Diff(ctx, domain.Request{Key: key, Path: path, Old: oldSrc, New: newSrc})
	if derr != nil {
		writeErr(w, http.StatusInternalServerError, derr)
		return
	}
	writeDiffJSON(w, d, nil)
}

// maybeLenient softens only the LIVE sides: an absent working-tree file is an
// ordinary "added/deleted here" answer, while a stored side that cannot be
// read is a genuine failure the user has to see (domain's ComparePatch draws
// the same line).
func maybeLenient(side entrySide, src domain.ByteSource) domain.ByteSource {
	if side.live {
		return lenient(src)
	}
	return src
}

// entryCompareSide is one side of the whole-tree lane on the wire.
type entryCompareSide struct {
	Spec   string `json:"spec"`
	Label  string `json:"label"`
	Hash   string `json:"hash,omitempty"`
	Frozen bool   `json:"frozen,omitempty"`
}

// compareSideWire renders a resolved side for the client, plus the sentence
// naming the fallback when that side is standing on frozen bytes. The note is
// per SIDE because both of them can fall back independently, and "one of
// these is a snapshot" is not the same warning as "both are".
//
// Per KIND, with no default arm, for the same reason commitEntrySide is: the
// old "shelf, else commit:" + ep.Hash()" shape sent a ref or pair endpoint to
// the browser as a commit with an EMPTY hash, and the client's per-file diff
// requests would then quietly describe nothing. The endpoint is domain's, not
// the request's, so an inexpressible kind is a server bug (500).
func compareSideWire(ep model.Endpoint, spec commitEntrySpec) (entryCompareSide, string, error) {
	switch ep.Kind() {
	case model.EndpointShelf:
		return entryCompareSide{Spec: "shelf:" + ep.ShelfID(), Label: spec.label, Frozen: true},
			"frozen copy — commit " + shortSha(spec.sha) + " no longer exists", nil
	case model.EndpointCommit:
		return entryCompareSide{Spec: "commit:" + ep.Hash(), Label: spec.label, Hash: ep.Hash()}, "", nil
	case model.EndpointWorkTree, model.EndpointIndex, model.EndpointRef, model.EndpointPair, model.EndpointInvalid:
		// This lane compares two STORED COMMIT ENTRIES, so only a commit or its
		// frozen shelf stand-in can appear. A live state, a moving tip or a
		// change-set has no spelling here. %d, not Display(), for the reason
		// commitEntrySide gives: Display panics on the zero endpoint.
		return entryCompareSide{}, "", fmt.Errorf("compare side: endpoint kind %d cannot be addressed as a stored commit entry", ep.Kind())
	default:
		return entryCompareSide{}, "", fmt.Errorf("compare side: unknown endpoint kind %d", ep.Kind())
	}
}

// commitEntrySpec is one requested side of the whole-tree lane: a stored
// commit entry, or a live commit named by hash.
type commitEntrySpec struct {
	sha     string // the FULL sha the side records
	shelfID string // shelf entry backing it; "" for a bookmark or a bare commit
	label   string
}

// resolveEntrySideSpec reads a stored entry named by a store/id pair.
func resolveEntrySideSpec(ctx context.Context, svc *domain.Service, store, id string) (commitEntrySpec, int, error) {
	sha, shelfID, label, code, err := commitEntryAddress(ctx, svc, store, id)
	if err != nil {
		return commitEntrySpec{}, code, err
	}
	return commitEntrySpec{sha: sha, shelfID: shelfID, label: label}, 0, nil
}

// resolveCompareRight reads the RIGHT side, which accepts either form: `sha`
// for a live commit (a commit row in the browser) or `right_store`/`right_id`
// for a second stored entry. One endpoint therefore serves both "this commit
// against a bookmark" and "these two entries against each other" — and the
// LEFT side is always an entry (store/id), so `sha` keeps meaning exactly what
// it meant before this second form existed.
func resolveCompareRight(ctx context.Context, svc *domain.Service, q url.Values) (commitEntrySpec, int, error) {
	if sha := q.Get("sha"); sha != "" {
		if !isHexSha(sha) {
			return commitEntrySpec{}, http.StatusBadRequest, errors.New("sha must be a hex commit id")
		}
		return commitEntrySpec{sha: sha, label: shortSha(sha)}, 0, nil
	}
	if q.Get("right_store") == "" && q.Get("right_id") == "" {
		return commitEntrySpec{}, http.StatusBadRequest,
			errors.New("a right side is required: sha=<hex>, or right_store + right_id")
	}
	return resolveEntrySideSpec(ctx, svc, q.Get("right_store"), q.Get("right_id"))
}

// handleCompareEntry compares two commit-shaped sides, at least one of them a
// stored entry: entry ↔ a live commit (a commit row's menu), or entry ↔ entry
// (two bookmarks, two shelf entries, or one of each). FIRST side = left/older
// — the TUI's convention (compareCommitBookmark, startEntryCompare), which is
// what makes the A/D statuses read the way the file list shows them.
//
// Each side resolves HYBRID and INDEPENDENTLY: the live commit while it still
// exists, the shelf entry's frozen tar once it does not. Mixed states
// therefore compose — a shelf↔shelf pair with one gc'd sha becomes frozen↔live
// and lands in domain's shelf↔commit lane. Any fallback is reported rather
// than smoothed over: a comparison against a snapshot of a commit that no
// longer exists is a different statement from one against the commit itself,
// and the client puts it in the title.
//
// format=patch adds the unified diff for the whole comparison (domain renders
// a frozen side through temp files, which git cannot do by itself).
func (s *Server) handleCompareEntry(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	q := r.URL.Query()
	ctx := r.Context()
	l, code, err := resolveEntrySideSpec(ctx, svc, q.Get("store"), q.Get("id"))
	if err != nil {
		writeErr(w, code, err)
		return
	}
	rr, code, err := resolveCompareRight(ctx, svc, q)
	if err != nil {
		writeErr(w, code, err)
		return
	}
	// Two entries recording the SAME commit are a non-comparison — except two
	// DIFFERENT shelf entries of it, whose frozen sets were taken at different
	// moments and may legitimately differ (the TUI's distinctShelves rule).
	distinctShelves := l.shelfID != "" && rr.shelfID != "" && l.shelfID != rr.shelfID
	if l.sha == rr.sha && !distinctShelves {
		writeErr(w, http.StatusUnprocessableEntity, errors.New("that is the same commit — pick a different one to compare against"))
		return
	}
	left, err := svc.ResolveCommitEntryEndpoint(ctx, l.sha, l.shelfID)
	if err != nil {
		// A bookmark whose commit is gone has nothing frozen to fall back on.
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}
	right, err := svc.ResolveCommitEntryEndpoint(ctx, rr.sha, rr.shelfID)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}
	files, err := svc.CompareFiles(ctx, left, right)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]compareFile, len(files))
	for i, f := range files {
		out[i] = compareFile{Path: f.Path, Status: f.Status, OldPath: f.OldPath}
	}
	leftSide, leftNote, err := compareSideWire(left, l)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	rightSide, rightNote, err := compareSideWire(right, rr)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	frozen := leftSide.Frozen || rightSide.Frozen
	payload := map[string]any{
		"left":   leftSide,
		"right":  rightSide,
		"files":  out,
		"frozen": frozen,
	}
	if note := strings.TrimSpace(leftNote + " " + rightNote); note != "" {
		payload["frozen_note"] = note
	}
	if q.Get("format") == "patch" {
		patch, perr := svc.ComparePatch(ctx, left, right)
		if perr != nil {
			writeErr(w, http.StatusInternalServerError, perr)
			return
		}
		payload["patch"] = patch
	}
	writeJSON(w, payload)
}

// commitEntryAddress reads a stored entry's commit address: the full sha it
// records, the shelf id backing it (empty for a bookmark, which stores no
// blobs), and its human label. store is an ALLOWLIST — the two names the
// stores answer to, nothing derived from the request.
func commitEntryAddress(ctx context.Context, svc *domain.Service, store, id string) (sha, shelfID, label string, code int, err error) {
	if id == "" {
		return "", "", "", http.StatusBadRequest, errors.New("id required")
	}
	switch store {
	case "bookmarks":
		b, gerr := svc.BookmarkGet(ctx, id)
		if gerr != nil {
			return "", "", "", http.StatusNotFound, gerr
		}
		if !b.IsCommit() {
			return "", "", "", http.StatusUnprocessableEntity, errors.New("that bookmark points at a file, not a commit")
		}
		label = b.Label
		if label == "" {
			label = b.Address().Display()
		}
		return b.Commit, "", label, 0, nil
	case "shelf":
		e, ferr := svc.ShelfFind(ctx, id)
		if ferr != nil {
			return "", "", "", http.StatusNotFound, ferr
		}
		if !e.IsCommit() {
			return "", "", "", http.StatusUnprocessableEntity, errors.New("that shelf entry holds a file, not a commit")
		}
		label = e.Label
		if label == "" {
			label = e.Origin.Display()
		}
		return e.Origin.Commit, e.ID, label, 0, nil
	}
	return "", "", "", http.StatusBadRequest, fmt.Errorf("unknown store %q", store)
}

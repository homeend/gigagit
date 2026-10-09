package tui

import (
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// windowState is the part of the Model a WORKTREE owns: its window pile
// (full-screen views and centred popups), the files and stash views, the
// preview beside them, what an open diff still owes its reader, the steer
// leftovers waiting on this tree's loads, and the generations that say
// "is this result for the window that is open". The Model embeds it, so
// every reader keeps spelling m.filesView, m.layers, m.diffTag; the slot
// (worktreeView.windows) holds the sleeping copy and switchView swaps the
// two with ONE assignment — a window field is either here, per worktree,
// or on the Model, process-wide. There is no third place.
//
// The checklist for "does a field belong here" is closeFilesView
// (files_view.go): everything it resets is a window field. What stays on
// the Model and why: focus, lastLeftPanel, activeLeftTab and the ctrl+t
// pin (alt+w's first-hit rule and showConsole's Commits-column invariants
// read them across a swap); eager (it walks the SHARED commit feed);
// startAt* (fires within one Update, never outlives a swap); the modal,
// the process, the console, notices, the action menu and the typing flags
// (the operation's and the keyboard's surfaces, never parked); the slot-
// data generations loadGen/srcGen/watchGen (sleepView bumps them so a
// read for the leaving tree cannot land in the arriving one); the session
// diff preferences (diffPartial, diffLong, diffImgLayout, diffCursor,
// diffStacked); and the commit attention bands (a commit is the repo's —
// the working-file bands travel in workingAttention).
type windowState struct {
	pendingSteer    *pendingSteer   // parked navigate (steer_nav.go); drained by the load it waits on
	pendingHint     *pendingHint    // navigate whose hint (steer_nav.go) is being revealed; drained by bookmarksLoadedMsg/shelfLoadedMsg
	hintGen         int             // generation guard for pendingHint (fix F3): bumped on every stage, stamped into the hint's OWN load so an unrelated bookmark/shelf load in flight can never be mistaken for it
	entryCompareGen int             // drops stale commit-entry compare resolves (the pickGen pattern)
	gitConfigGen    int             // stale-drop guard for explorer row loads
	versionsGen     int             // stale-drop guard for the branch-versions popup's loads
	pendingCompare  *pendingCompare // focused file awaiting the compare-mode picker; nil = none
	stashView       *stashView      // stash list in the right column (over Commits); nil = closed
	wtFiles         *worktreeFiles  // F's working-tree mode of the files view (nil otherwise)
	filesFull       bool            // ctrl+t: the files view spans the whole body
	previewFull     bool            // ctrl+t on a focused preview: it spans the whole body
	wtPreviewGen    int             // bumped per cursor move in F's window: drops a superseded preview settle

	filesMode         filesMode              // authoritative source mode (changed/fullTree/compare/stash)
	filesView         *contentPopup          // commit files tree replacing the left column; nil = closed
	filesTitle        string                 // "Files <short-hash> <subject>", updated with the content — rendered/localized display text; NEVER parsed
	filesContext      string                 // diff-view context payload (ref/subject or compare label) mirroring filesTitle's content sans any "Files "/panel framing; the diff view's "@ <context>" header reads THIS, not filesTitle
	filesCommit       model.Commit           // the RESOLVED commit the view is showing (date/author/subject), incl. the ones fetched for a bare sha; backs the date line and filesViewCommit's fallback. Zero UnixTime = unknown: no date line is drawn and no row is spent
	filesHash         string                 // commit the view wants; gates stale async results
	filesLeft         model.Endpoint         // compare mode: older side
	filesRight        model.Endpoint         // compare mode: newer side
	compareTag        string                 // gates stale compareFilesMsg results
	comparePair       *comparePairState      // branch-pair compare extension (origin filter); nil for every other compare
	filesSets         *domain.LinkComparison // link compare: the two file sets, for per-member byte sources; nil for every endpoint compare
	linkCompareWant   string                 // tag of the link compare in flight; "" = none (a stale or cancelled load is dropped)
	filesStashTag     string                 // when the files tree is showing a stash: its ref (gates stash-file loads)
	filesShelfID      string                 // shelf mode: the shelved-commit entry id (gates shelf-file loads, keys member refs)
	filesShelfLabel   string                 // shelf mode: "shelf #<short>" display label for diff contexts
	filesShelfNotes   []domain.ResolvedNote  // shelf mode: the entry's own notes, listed above its members (a Notes section) and read by enter on their rows
	filesReturnFocus  panel                  // panel that opened the files view; esc/l restore focus here (the view itself runs on panelCommits)
	filesReturnLayers []layer                // layer stack parked by a popup that handed off to the files view (handOffToFilesView); esc/l restore it, every other teardown drops it (closeFilesView zeroes it)
	filesTreeFocused  bool                   // true = the tree side owns vertical movement (←/→/tab)
	filesReadInflight bool                   // a per-commit files-view CommitFiles read is outstanding; drop further nav reads until it lands (pure-drop pacing on large repos)
	filesPreview      *openFile              // full-tree mode: the file shown in the right column (nil = none)

	diffTag    string      // request key of the wanted diff; gates stale async results
	diffNav    diffNavKind // which list the open diff was opened from (Home/End file-stepping)
	diffNotice string      // transient bottom-left diff-view notice (file arrival / no-file); cleared on the next key

	// noteLand parks the landing a }/{ FILE step owes the user: the step opens
	// the next noted file asynchronously, so the note to sit on is not known
	// until that file's notes arrive (notesLoadedMsg). nil = nothing parked.
	noteLand *noteLanding
	// hunkReload parks the re-read a staging round owes a SINGLE-file
	// working-tree diff: a stack reconciles itself on every status write, one
	// file does not (diff_stack_hunks.go). nil = nothing parked.
	hunkReload *hunkReload
	// diffLand parks the LINE a re-opened single diff owes the reader: leaving
	// a stack with S re-opens the file asynchronously, and a fresh view lands
	// on its first change block, not on the line being read. nil = nothing.
	diffLand *lineLanding

	// filesPreviewSet / filesPreviewCounts are the open preview's note scope
	// and its per-path badge counts; nil/empty when the files view is not
	// showing a preview. Stamped onto each diff the view opens.
	filesPreviewSet    *domain.PreviewNoteSet
	filesPreviewCounts map[string]int
	filesPreviewGroups map[string][]string // a PR's per-path note groups: the badges' colour bars
	// filesPreviewReviews are the open preview's (or pair's) AI reviews: the
	// Reviews block on top of its file list. filesPairLabel is an open saved
	// pair's label, kept so a review opened from it can re-open it.
	filesPreviewReviews []domain.ReviewHead
	filesPairLabel      string
	// filesReview is the files view's REVIEW mode (review_view.go): set
	// after the view opens on a structured review; nil otherwise.
	filesReview *reviewViewState
	// filesLandNote is the review whose row the next commit file list puts
	// the cursor on (esc from a review opened from that list); "" = none.
	filesLandNote string
	// filesLandScope is its twin for a Range review row (the scope it names).
	filesLandScope string
	// filesBack is set while a range opened from a commit's Range review row
	// shows: esc returns to that commit's files, the cursor on the row.
	filesBack *scopeBack
	// reviewsFollowGen numbers follow-live list landings: only the latest
	// one's pause reads the commit's reviews (reviewsFollowMsg).
	reviewsFollowGen int
	// reviewOpenGen numbers review opens: a read answers only the loading
	// box of its own open (reviewLoadingPopup).
	reviewOpenGen int

	previewOpen *previewOpenState // the merge preview the compare view is showing; nil = none (pointer: survives the value copy)
	previewGen  int               // files-view generation; gates stale previewOpenMsg results (closeFilesView bumps it)
	layers      *layerStack       // top-of-everything window pile: full-screen surfaces + centered popups; nil/empty = none

	// tour is an agent tour (overview id) to show once this worktree's
	// status has loaded (agent_tours_open.go); "" = none. Parked with the
	// worktree it was asked for: another worktree's status must not show it.
	tour string

	// workingAttention holds the `gg session highlight` bands on this
	// worktree's WORKING files (attentionKey.commit == "") while the
	// worktree sleeps; saveView moves them out of m.attention and loadView
	// merges them back. A commit's bands stay in m.attention, repo-wide.
	workingAttention map[attentionKey][]steerMark
}

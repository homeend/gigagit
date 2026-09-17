// live.js — the page's half of live refresh: one EventSource on
// /api/events, coalesced into a single re-fetch of just the sources the
// server named. The server decides WHEN (file watch, intervals, the
// [refresh] config); this module only decides WHAT to reload for a name.
//
// Rules: never while an op is running (its own post-op refresh covers
// everything); never two refreshes at once (the runOnce("refresh") gate
// manualRefresh uses too — an `r` press and a push coalesce); a reconnect
// after a dropped stream reloads everything, since events were missed.
import { attnKey, getJSON, runOnce, state } from "./core.js";
import { fetchStatus, wtCount } from "./status.js";
import { fetchNotes, markDiffRow, openCompare, openFile, openWorkingTree, reconcileStatusView, refreshNoteCounts, renderDiff, revealDiffRow, setLayout, stepNote } from "./files.js";
import { fetchBranches, revealHintEntry } from "./sidebar.js";
import { fetchPreviews, openPreviewForPair, reopenPreviewIfMoved } from "./previews.js";
import { loadCommits, openCommitByHash, renderCommits } from "./commits.js";
import { focusPane } from "./keys.js";
import { loadRepo, opLine } from "./ops.js";

const COALESCE_MS = 150; // one burst of watcher events → one refresh
const RETRY_MS = 500; // a refresh is already running → try again after it

// Sidebar-family sources all reload through fetchBranches (one Promise.all
// over every list); the header (loadRepo) rides along for the branch name
// and ahead/behind counts.
const SIDEBAR = new Set(["branches", "remotes", "worktrees", "tags", "reflog"]);

// "previews" is not a ticker source (only a preview mutation emits it), and
// it reloads on its OWN — a rename must not drag the whole sidebar (tags
// alone cost seconds on a big repo) behind it. A moved tip, on the other
// hand, is a SIDEBAR event: the saved pairs are recomputed from the tips, so
// every sidebar refresh reloads them too.

const pending = new Set();
let timer = null;
let connected = false; // a second hello is a RECONNECT → full refresh

function connectLive() {
  const es = new EventSource("/api/events");
  es.onmessage = (m) => {
    let msg;
    try {
      msg = JSON.parse(m.data);
    } catch {
      return;
    }
    if (msg.reason === "hello") {
      state.live = { enabled: !!msg.live, watch: !!msg.watch };
      // A hello means a NEW hub: the server replaces it wholesale on re-root
      // (and on a refresh-settings write), which ends every stream and brings
      // the tabs back here. Attention bands are addressed by (state, rev,
      // path) against the repo they were painted on, so they must not outlive
      // it — a sha from the old root would key marks onto a file of the new
      // one. A plain reconnect drops them too, which is right: the agent's
      // marks are a live conversation, not stored state.
      state.attention.clear();
      if (connected) scheduleFull(); // reconnect: events were missed meanwhile
      connected = true;
      return;
    }
    // A steer is a one-off instruction an agent posted, not a change
    // notification: it is applied AT ONCE and never joins the coalescing set.
    if (msg.reason === "steer" && msg.steer) {
      applySteer(msg.steer);
      return;
    }
    for (const src of msg.changed || []) pending.add(src);
    arm(COALESCE_MS);
  };
  // EventSource reconnects on its own; the re-hello then reloads in full.
  es.onerror = () => {};
  return es;
}

function arm(ms) {
  if (timer) return;
  timer = setTimeout(() => {
    timer = null;
    flush();
  }, ms);
}

function scheduleFull() {
  pending.add("status");
  pending.add("branches");
  pending.add("feed");
  arm(COALESCE_MS);
}

function flush() {
  if (!pending.size) return;
  if (state.op) {
    // An op owns the data: its refreshAfterOp reloads everything, so the
    // pending names are moot — drop them rather than replay stale ones.
    pending.clear();
    return;
  }
  const want = new Set(pending);
  pending.clear();
  const run = runOnce("refresh", () => refreshSources(want));
  if (!run) {
    // a manual `r` or an earlier flush is mid-flight — queue behind it
    for (const s of want) pending.add(s);
    arm(RETRY_MS);
    return;
  }
  run.catch(() => {});
}

// refreshSources reloads exactly what changed. It mirrors refreshAfterOp's
// cursor discipline: read the anchor BEFORE status (the working-tree row
// can appear or vanish and shift every index), reload the feed in
// RECONCILE mode (paged history and scroll survive), re-anchor after.
async function refreshSources(want) {
  const at = state.rows[state.cursor - wtCount()];
  const keep = at && at.hash;
  const jobs = [];
  if (want.has("status")) jobs.push(fetchStatus());
  // "notes" is not a ticker source: only a note mutation emits it, and both
  // halves (the open diff's rows, the ◆N badges) reload from it.
  if (want.has("notes")) jobs.push(fetchNotes(), refreshNoteCounts());
  let sidebar = false;
  for (const s of want) if (SIDEBAR.has(s)) sidebar = true;
  if (sidebar) jobs.push(fetchBranches(), loadRepo());
  // "notes" pulls the previews too: a preview row carries the pair's note
  // total, so a note write changes the LIST as well as the open diff (the same
  // reason the TUI chains its previews read off the notes source).
  if (sidebar || want.has("previews") || want.has("notes")) jobs.push(fetchPreviews());
  await Promise.all(jobs);
  // After the previews list lands: an open preview whose tips moved re-opens
  // itself, one whose pair vanished closes with a notice.
  await reopenPreviewIfMoved();
  if (want.has("status")) reconcileStatusView();
  if (want.has("feed")) {
    await loadCommits(false, false);
    const last = state.rows.length + wtCount() - 1;
    const i = keep ? state.rows.findIndex((r) => r.hash === keep) : -1;
    if (i >= 0) state.cursor = i + wtCount();
    else if (state.cursor > last) state.cursor = Math.max(0, last);
  }
  renderCommits();
}

// --- live steering ----------------------------------------------------------
// One `gg session …` command, posted to POST /api/session/steer and fanned out
// on this same stream. Every value arrived through the server's allowlists, so
// the openers can be handed it directly.

// applySteer runs one command an agent posted. Failures are silent: the
// endpoint answered 202 long ago and the CLI has already printed "web: sent" —
// there is nothing left to report a failure to.
async function applySteer(s) {
  try {
    switch (s.cmd) {
      case "reload":
        return await steerReload(s);
      case "focus":
        return steerFocus(s.panel);
      case "highlight":
        return steerHighlight(s);
      case "highlight_clear":
        return steerHighlightClear(s);
      case "navigate":
        return await steerNavigate(s);
    }
  } catch {
    // a page that moved on under us is not an error worth surfacing
  }
}

// steerReload goes through the same single-flight gate manual `r` and the
// watcher use: the server serialises reads under the per-repo gate, so
// overlapping fans stack. A null return means one is already running, which is
// a perfectly good answer to "reload". A `status`/`all` reload also DROPS the
// attention marks (the TUI's steerReload does the same): those rebuild the diff
// geometry the bands are anchored against, and a range that drifted under an
// edit is the agent's to re-post. A `notes`-only reload KEEPS them — every note
// mutation auto-posts one, so wiping there would erase a band the agent had
// just painted. The clear goes before the fetch so a notes re-render already
// reflects it.
async function steerReload(s) {
  const want = new Set(s.sources); // the server fills the default ["notes"]
  if (want.has("all")) {
    // "all" is the TUI's hard full reload; refreshSources knows only the
    // individual names, so expand it here rather than have it fall through
    // as a silent no-op.
    want.delete("all");
    for (const n of ["status", "notes", "branches", "feed"]) want.add(n);
  }
  if (want.has("status")) state.attention.clear();
  await runOnce("refresh", () => refreshSources(want));
  // A cleared band is still painted in the DOM until something re-renders; a
  // notes reload does that on its own, anything else needs the nudge.
  if (want.has("status") && !want.has("notes") && state.lastDiff) renderDiff(state.lastDiff);
}

// steerFocus maps the protocol panel names onto the web's two panes. The page
// has `commits` and `files`, not the TUI's nine panels; a name it has no pane
// for is a no-op (the endpoint validated it against the full protocol
// vocabulary, and the CLI is told "web: sent", not what the page did with it).
//
// Focusing must SHOW the pane, not merely record it: each layout hides one of
// the two, so a focus onto the hidden one steps the layout just far enough to
// reveal it (never further — an open diff survives, unlike drillOut, which
// clears it). focusPane then moves the `.focused` ring, exactly as every
// keyboard entry point does.
function steerFocus(panel) {
  if (panel === "commits") {
    state.pane = "commits";
    // The diff layout replaces the commit list; "files" is the nearest stage
    // that shows it again.
    if (state.layout === "diff") setLayout("files");
  } else if (panel === "files" || panel === "staged") {
    state.pane = "files";
    // The file column has no place in the full-width list layout.
    if (state.layout === "list") setLayout("files");
  } else {
    return; // a panel this page has no pane for
  }
  focusPane();
}

// steerAttnKey builds the state.attention key from a wire command — the same
// builder the renderer reads with, so the two always agree.
function steerAttnKey(s) {
  return attnKey({ state: s.state, rev: s.commit, path: s.file });
}

function steerHighlight(s) {
  const k = steerAttnKey(s);
  const marks = state.attention.get(k) || [];
  marks.push({ side: s.side, start: s.start, end: s.end, tone: s.tone }); // server fills side and end
  state.attention.set(k, marks);
  if (state.lastDiff) renderDiff(state.lastDiff);
}

function steerHighlightClear(s) {
  if (!s.file) state.attention.clear();
  else state.attention.delete(steerAttnKey(s));
  if (state.lastDiff) renderDiff(state.lastDiff);
}

// resolveRefTip resolves a `@ref:<name>` navigate's branch/tag NAME to its
// tip hash — no new endpoint: fetchBranches already loads both branches and
// tags in one round trip (the sidebar's own refresh), so a lookup against it
// is exactly what previews.js's tipOf does for a saved pair. It is called
// HERE, at apply time, rather than reading whatever the sidebar last cached
// (ruling R2): a stale cache would land a branch that moved a moment ago on
// its OLD tip, precisely the bug the name-on-the-wire rule exists to avoid.
// "" means the freshly fetched lists still have no row for it (a deleted
// branch, or a tag past the sidebar's row cap), and the caller no-ops rather
// than opening the wrong thing.
async function resolveRefTip(name) {
  await fetchBranches();
  const b = (state.branches || []).find((x) => x.name === name);
  if (b) return b.hash || "";
  // Remotes too: @ref:<name> is NOT restricted to a local branch or a tag.
  // model.LinkRefOK only forbids the grammar's separators and whitespace, and
  // domain.finishLink's ref arm resolves the name with a bare ResolveRev — so
  // `@ref:origin/main` is a legal, resolvable link end to end through the CLI,
  // the domain and the TUI. Without this lookup it silently did nothing HERE
  // alone, which is the worst of the three outcomes. fetchBranches already
  // populates state.remotes in the same round trip.
  const r = (state.remotes || []).find((x) => x.name === name);
  if (r) return r.hash || "";
  const t = (state.tags || []).find((x) => x.name === name);
  return t ? t.target || "" : "";
}

// isHexLike reports whether name already looks like a bare commit sha (the
// pair grammar's OTHER half shape, model.LinkPair — a NAME is the common
// case, a raw sha is legal too) rather than a ref name.
function isHexLike(name) {
  return /^[0-9a-f]{7,64}$/.test(name);
}

// openCompareForPair opens a `@<a>..<b>` navigate's two-dot change-set — the
// SAME renderer a saved preview's three-dot pair uses (files.js's
// openCompare). A NAME half — a local branch, a remote-tracking branch, or
// a tag — is sent straight through the plain openCompare(a, b) lane: the
// server resolves it to a FULL sha itself (compare.go's
// compareNamedEndpoint, review round 1's fix), so there is no client-side
// resolution left to do and no abbreviated-hash risk (an earlier version of
// this function DID resolve names client-side, off the sidebar's own
// %(objectname:short) rows, and a short core.abbrev made the hex lane 400).
// Only when BOTH halves already look like bare shas does this ask through
// revs=1 instead — the one shape the plain lane cannot express at all,
// since it resolves NAMES, not raw hex ids.
async function openCompareForPair(a, b) {
  if (isHexLike(a) && isHexLike(b)) {
    await openCompare(a, b, { revs: 1, aLabel: a, bLabel: b });
    return;
  }
  await openCompare(a, b);
}

// steerNavigate is the whole navigate verb: it lands where the command
// names (steerNavigateLand), then reveals the bookmark/shelf row its hint
// (spec §3.3) named — hint_kind/hint_id, the closed set the server's
// toSteerWire already validated. The reveal is called HERE, unconditionally
// after the land, rather than appended inside steerNavigateLand: that
// function has SEVERAL early `return`s (the file-less preview/ref/pair
// reveals, the `else if (!s.file)` hint-only fall-through, the shared
// `if (!s.line) return;` tail) — a reveal placed after any one of them would
// silently skip every other shape (the same trap ruling S2 named one file
// over, in finishLink's arms).
async function steerNavigate(s) {
  await steerNavigateLand(s);
  if (s.hint_kind) revealHintEntry(s.hint_kind, s.hint_id);
}

// steerNavigateLand opens what the command names and marks the landed row.
// It reuses the very openers the .-menu rows use — openFile does the layout
// switch and routes a working-tree entry to openStatusDiff itself — so a
// steered landing is indistinguishable from a clicked one. A hint-only
// command (no state, no file, no commit, no step) falls through to the
// `else if (!s.file)` branch below and lands on nothing — the reveal above
// IS its landing (ruling S13).
async function steerNavigateLand(s) {
  if (s.step) {
    stepNote(s.step === "next_note" ? 1 : -1);
    return;
  }
  if (s.state === "preview") {
    // The pair, never a sha: the tip is resolved here, so a tip that moved
    // between post and apply is honoured (the TUI consumer does the same).
    await openPreviewForPair(s.source, s.target);
    if (!s.file) return; // a file-less preview navigate only reveals the stage
    const i = state.files.findIndex((f) => f.path === s.file);
    if (i < 0) return;
    await openFile(i);
  } else if (s.state === "ref") {
    // The NAME, never a sha: the tip is resolved here, so a branch that moved
    // between post and apply is honoured (the TUI consumer does the same).
    const sha = await resolveRefTip(s.ref);
    if (!sha) {
      // SAY SO. A ref this page cannot place — a name past the sidebar's row
      // cap, a rev expression like HEAD~2, a branch deleted since the link was
      // made — used to return here in silence, leaving a page that looked as
      // though nothing had been asked of it. A swallowed failure that renders
      // a plausible screen is this feature's signature bug (ruling S7).
      opLine("gg link: cannot place " + s.ref + " in this repository", true);
      return;
    }
    await openCommitByHash(sha, s.ref);
    if (!s.file) return; // a file-less ref navigate only reveals the tree
    const i = state.files.findIndex((f) => f.path === s.file);
    if (i < 0) return;
    await openFile(i);
  } else if (s.state === "pair") {
    // A change-set is BOUNDED, so it is opened as a comparison — never as a
    // commit's tree, which is what the ref arm above opens instead.
    await openCompareForPair(s.a, s.b);
    if (!s.file) return; // a file-less pair navigate only reveals the compare
    const i = state.files.findIndex((f) => f.path === s.file);
    if (i < 0) return;
    await openFile(i);
  } else if (!s.file) {
    if (s.commit) await openCommitByHash(s.commit, s.commit.slice(0, 8));
    return;
  } else if (s.state === "commit") {
    await openCommitByHash(s.commit, s.commit.slice(0, 8));
    const i = state.files.findIndex((f) => f.path === s.file);
    if (i < 0) return;
    await openFile(i);
  } else {
    // The working-tree stage, entered the way the palette enters it (0 = the
    // WT row): it re-reads status — the agent may have written the file a
    // moment ago — and puts the file list on screen before the diff opens.
    await openWorkingTree(0);
    const i = state.statusEntries.findIndex((f) => f.path === s.file);
    if (i < 0) return;
    await openFile(i);
  }
  if (!s.line) return;
  const side = s.side; // the server fills it whenever a line is present
  // A side-by-side `change` row anchors on its NEW side, so an old-side
  // landing falls back to the row carrying that left number; a line the
  // changes-only view folded away is unfolded first.
  const tr = revealDiffRow(side, s.line);
  if (!tr) return;
  markDiffRow(tr, side, s.line);
  tr.scrollIntoView({ block: "center" });
}

// applyStartAt lands the page where `gg open --web <link>` started this server
// — the web twin of the TUI's --at. The server hands the command out ONCE, so
// a reload or a second tab gets nothing and stays put. boot() calls this only
// after its first FULL load (status, branches, previews included), so a
// preview landing finds its saved row in state.previews instead of opening a
// show-once twin — the same readiness the TUI's startAtReady waits for.
// Failures are silent, as for any applied steer.
async function applyStartAt() {
  let body;
  try {
    body = await getJSON("/api/session/start-at");
  } catch {
    return;
  }
  if (body && body.steer) await applySteer(body.steer);
}

export { applyStartAt, connectLive, refreshSources };

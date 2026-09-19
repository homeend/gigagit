// previews.js — saved merge previews: the GitHub-PR "files changed" view for
// a (source → target) pair, recomputed from the live tips. This module owns
// the sidebar list, its menus, the add flow, and the open page (the existing
// compare screen over the two hashes /api/preview/open resolves).
//
// It deliberately does NOT import sidebar.js: files.js and ops.js already
// import it, so a sidebar → previews → files edge would close an import cycle
// with top-level code on both ends. previews.js therefore owns its own fetch,
// and live.js / ops.js / app.js call it beside fetchBranches.
import { $, esc, getJSON, postJSON, state } from "./core.js";
import { openPrompt, showCtxMenu } from "./layers.js";
import { opLine, showLocalConfirm } from "./ops.js";
import { applyCompareFilter, drillOut, noteBadgeHTML, openCompare, renderFiles } from "./files.js";
import { extraRows, registerHelp, registerRows } from "./menus.js";

// fetchPreviews loads the list and renders it. A failure leaves an EMPTY list
// rather than the previous one: a stale row invites a click that opens a pair
// the server no longer knows.
export async function fetchPreviews() {
  try {
    const body = await getJSON("/api/preview");
    state.previews = body.entries || [];
    state.previewsDisabled = !!body.disabled;
    state.previewsStale = false;
  } catch (e) {
    // A failed request is NOT "the previews are gone". Blanking the list here
    // would make reopenPreviewIfMoved read the open preview's missing row as a
    // removal and close the screen on one transient error, so the rows stand
    // and the pass is marked stale instead; the next good fetch clears it.
    state.previewsStale = true;
    return;
  }
  renderPreviews();
}
const refresh = () => fetchPreviews();


// stateText is the row's right-hand cell: the counts when the pair is
// previewable, else why it is not (the TUI's previewStateText).
function stateText(e) {
  switch (e.state) {
    case "merged": return "merged";
    case "missing-source": return "missing: " + e.source;
    case "missing-target": return "missing: " + e.target;
    case "no-base": return "no common base";
    case "error": return "error: " + (e.error || "");
  }
  // Singular for one file, as the TUI's previewStateText renders it.
  return (e.files === 1 ? "1 file" : e.files + " files") + " ↑" + e.ahead;
}


function renderPreviews() {
  if (state.previewsDisabled) {
    $("previews-list").innerHTML = `<li class="none">previews are unavailable here</li>`;
    return;
  }
  $("previews-list").innerHTML = (state.previews || [])
    .map(
      (e) =>
        `<li data-id="${esc(e.id)}" title="${esc(e.source + " → " + e.target)}"><span class="mk"></span>` +
        `${esc(e.label)}<span class="psub">${esc(e.source + " → " + e.target)}</span>` +
        `<span class="psub">${esc(stateText(e))}</span>` +
        // The pair's review-note total, the ◆N the TUI paints on the row. Its
        // own field, not part of stateText: the counts cell says what the
        // preview IS, the badge what has been said about it. The markup is
        // files.js's noteBadgeHTML — one badge painter for every list, so a
        // preview row can never drift from a file row.
        noteBadgeHTML(e.notes) +
        `</li>`
    )
    .join("");
}


// previewShowing reports whether the compare screen is still the one an open
// preview put there. state.filesMode alone is not enough: a branch ↔ branch
// compare or a shelf comparison opened afterwards is also "compare" mode, and
// a moved tip must never replace what the user is looking at now. The
// preview's own compare has the SOURCE tip on the b side (openPreviewBody
// opens merge-base → source). Nor is it enough that a comparison is LOADED:
// drillOut (esc) returns to the commit list without clearing filesMode or
// state.compare, and a tip that moves afterwards must not drag the user back
// into a screen they closed — hence the layout check.
function previewShowing() {
  const po = state.previewOpen;
  if (!po || state.layout === "list") return false; // esc'd back to the commit list
  return state.filesMode === "compare" && !!state.compare && state.compare.bHash === po.sourceHash;
}


// closePreviewView drops the compare screen the way esc does — an open diff
// first, then the file list — so a preview that stopped being previewable
// does not leave a stale diff standing under a one-line notice (the TUI's
// closePreviewView).
function closePreviewView() {
  state.previewOpen = null;
  drillOut();
  if (state.layout !== "list") drillOut();
}


// openPreviewBody opens the compare page over the resolved hashes and
// remembers what is showing, so a later refresh can tell whether the tips
// moved. `moved` is the ref whose tip moved on a re-open, and it is announced
// only when the screen really changes: a target commit off the fork point
// moves neither the merge base nor the source tip, so the diff is identical
// and saying "updated" would be noise (TUI parity).
// armPreview records the pair the compare screen is showing, so the next
// refresh can tell whether its tips moved.
function armPreview(body) {
  const was = state.previewOpen;
  const samePair = !!was && was.source === body.source && was.target === body.target;
  state.previewOpen = {
    id: body.id || "",
    source: body.source,
    target: body.target,
    sourceHash: body.source_hash,
    targetHash: body.target_hash,
    // The source tip IS the note write target: a preview note is an ordinary
    // committed note on it (spec §1.1). The page never computes this itself.
    tip: body.source_hash,
    // A pull request's diff (prs.js) rides this same screen. Its source and
    // target are DISPLAY names — the head may live in a fork — so nothing may
    // send them back to the server as refs.
    pr: body.pr || 0,
  };
  // The previous PAIR's per-file numbers must not survive onto this one's file
  // list; loadPreviewCounts fills them in again a moment later. Re-arming the
  // same pair (a tip that moved) keeps the numbers standing meanwhile, so the
  // badges do not blink off on every refresh.
  if (!samePair) state.previewCounts = null;
  // /api/preview/notes resolves branch NAMES; a PR's would 404 (a fork) or,
  // worse, read a same-named local pair's notes.
  if (state.previewOpen.pr) state.previewCounts = {};
  else loadPreviewCounts(body.source, body.target);
}


// loadPreviewCounts fetches the pair's per-file note totals with NO path, so
// the file list carries its ◆N badges from the first paint rather than only
// after a file has been opened (openFile's own fetch refreshes them).
async function loadPreviewCounts(source, target) {
  let d;
  try {
    d = await getJSON(
      "/api/preview/notes?source=" + encodeURIComponent(source) + "&target=" + encodeURIComponent(target)
    );
  } catch {
    return; // decoration: no badge beats a wrong badge
  }
  const po = state.previewOpen;
  if (!po || po.source !== source || po.target !== target) return; // superseded
  state.previewCounts = d.counts || {};
  renderFiles();
}


function previewTitle(body) {
  if (body.pr) return body.label + " (" + body.source + " → " + body.target + ")";
  return "merge preview: " + body.source + " → " + body.target;
}


export async function openPreviewBody(body, moved) {
  const po = state.previewOpen;
  if (body.state !== "ok") {
    // A pull request names itself; a merge preview is named by its pair.
    const what = body.pr ? body.label : "merge preview " + body.source + " → " + body.target;
    opLine(what + ": " + stateText(body), true);
    // Only the pair that is actually on screen closes: a notice about another
    // row must not take someone else's view down.
    if (po && po.source === body.source && po.target === body.target && previewShowing()) closePreviewView();
    return;
  }
  const same =
    previewShowing() && state.compare.aHash === body.left && state.compare.bHash === body.right;
  if (same) {
    // The endpoints did not change: keep the screen (and the file cursor) and
    // just reconcile the hashes, or every later refresh would announce the
    // same movement again. The title is still rewritten — two pairs can
    // resolve to the same two commits (main and origin/main in step), and the
    // header must name the one that is showing.
    $("files-title").textContent = previewTitle(body);
    armPreview(body);
    return;
  }
  await openCompare(body.left, body.right, {
    revs: 1,
    aLabel: "merge-base(" + body.target + ")",
    bLabel: body.source,
  });
  // openCompare bails on a failed fetch or a superseded open, leaving
  // state.compare as it was — nothing below may claim that screen then.
  if (state.filesMode !== "compare" || !state.compare || state.compare.bHash !== body.right) return;
  $("files-title").textContent = previewTitle(body);
  // The per-side origin filter is meaningless over merge-base → tip ("only
  // merge-base(x)" is empty by construction), so it goes off the same way a
  // missing merge base turns it off — with the reason on the buttons.
  state.compare.originsError = "a merge preview is already merge-base → tip";
  state.compare.previewBar = body.pr
    ? body.source + " → " + body.target + " · read-only"
    : "merge-base(" + body.target + ") → " + body.source;
  applyCompareFilter();
  armPreview(body);
  if (moved) opLine("preview updated: " + moved + " moved");
}


export async function openPreviewEntry(e, moved) {
  try {
    const body = await getJSON("/api/preview/open?id=" + encodeURIComponent(e.id));
    body.id = e.id;
    await openPreviewBody(body, moved);
  } catch (err) {
    opLine("preview: " + (err.message || err), true);
  }
}


// openPreviewForPair opens the preview a steering command named. A saved row
// for the pair takes the record path (so the page shows the label and the row
// stays selected); an unsaved pair opens transiently, exactly as the sidebar's
// "show once" does. Nothing here is saved — a steer must not write to the
// user's preview list.
export async function openPreviewForPair(source, target) {
  const e = (state.previews || []).find((p) => p.source === source && p.target === target);
  if (e) {
    await openPreviewEntry(e, "");
    return;
  }
  await openOnce(source, target, "");
}


// openOnce is the transient preview: no record, resolved by NAME every time.
async function openOnce(source, target, moved) {
  try {
    await openPreviewBody(
      await getJSON(
        "/api/preview/diff?source=" + encodeURIComponent(source) + "&target=" + encodeURIComponent(target)
      ),
      moved
    );
  } catch (err) {
    opLine("preview: " + (err.message || err), true);
  }
}


async function savePreview(source, target, label, open) {
  let entry;
  try {
    entry = (await postJSON("/api/preview", { source, target, label: label || "" })).entry;
  } catch (err) {
    // 409: this pair is already saved — open the record that exists rather
    // than making the user hunt for it.
    if (err.data && err.data.id) entry = { id: err.data.id };
    else {
      opLine("preview: " + (err.message || err), true);
      return;
    }
  }
  refresh();
  if (open && entry && entry.id) openPreviewEntry(entry);
}


// openPreviewPair is the once/save dialog (the TUI's branch pair picker row).
export function openPreviewPair(source, target) {
  showLocalConfirm(
    "Preview merging " + source + " into " + target + "?",
    ["show once", "show and save", "swap direction", "abort"],
    (o) => {
      if (o === "show once") openOnce(source, target, "");
      else if (o === "show and save") savePreview(source, target, "", true);
      else if (o === "swap direction") openPreviewPair(target, source);
    }
  );
}


// The two hash forms this module has to reconcile: the sidebar's branch and
// remote rows carry ABBREVIATED ids (for-each-ref's %(objectname:short)),
// while a preview's source_hash/target_hash come from rev-parse and are the
// full 40 characters. Comparing them with === is always false, so a
// "show once" preview would re-resolve on every refresh and always claim its
// source moved. sameHash compares the shorter one as a prefix instead.
function sameHash(a, b) {
  if (!a || !b) return false;
  return a.length < b.length ? b.startsWith(a) : a.startsWith(b);
}


// tipOf is the hash the sidebar last saw for a ref name — SHORT, see
// sameHash. "" when this page has no row for it (a deleted branch, or a
// remote list capped at 100).
function tipOf(name) {
  const b = (state.branches || []).find((x) => x.name === name);
  if (b) return b.hash || "";
  const r = (state.remotes || []).find((x) => x.name === name);
  return r ? r.hash || "" : "";
}


// knownName gates the add flow against the lists this page has loaded, so a
// typo is refused here rather than as a 404. The remotes list is capped
// server-side, so an unknown name is let through when it is truncated — the
// server resolves it against the full list either way.
function knownName(n) {
  if (!n) return false;
  if (tipOf(n)) return true;
  return !!state.remotesTruncated;
}


// addPreviewFlow: two prompts (source, then the branch it would be merged
// into), each validated before the next opens.
function addPreviewFlow() {
  openPrompt({
    title: "Merge preview — source branch:",
    value: "",
    onSubmit: (source) => {
      if (!knownName(source)) {
        opLine("unknown branch " + source, true);
        return;
      }
      const cur = (state.branches || []).find((b) => b.is_head);
      openPrompt({
        title: "Merge preview — merge " + source + " into:",
        value: cur && cur.name !== source ? cur.name : "main",
        onSubmit: (target) => {
          if (!knownName(target)) {
            opLine("unknown branch " + target, true);
            return;
          }
          if (target === source) {
            opLine("source and target are the same branch", true);
            return;
          }
          savePreview(source, target, "", true);
        },
      });
    },
  });
}
// The sidebar header's + control starts the flow. sidebar.js cannot import
// this module (the cycle above), so the handle goes through the window.
window.__ggAddPreview = addPreviewFlow;


async function removePreview(e) {
  try {
    // DELETE goes through the server's writeGuard, which requires the JSON
    // content type on every mutating request — body or no body.
    const resp = await fetch("/api/preview?id=" + encodeURIComponent(e.id), {
      method: "DELETE",
      headers: { "Content-Type": "application/json" },
    });
    if (!resp.ok) {
      const body = await resp.json().catch(() => ({}));
      opLine("preview: " + (body.error || resp.statusText), true);
      return;
    }
  } catch (err) {
    opLine("preview: " + (err.message || err), true);
    return;
  }
  if (state.previewOpen && state.previewOpen.id === e.id) state.previewOpen = null;
  refresh();
}


function showPreviewMenu(e, x, y) {
  const items = [
    { label: "open merge preview", act: () => openPreviewEntry(e) },
    {
      label: "rename…",
      act: () =>
        openPrompt({
          title: "Rename " + e.label + " to:",
          value: e.label,
          onSubmit: async (label) => {
            try {
              await postJSON("/api/preview/rename", { id: e.id, label });
            } catch (err) {
              opLine("preview: " + (err.message || err), true);
              return;
            }
            refresh();
          },
        }),
    },
    { label: "save reversed (" + e.target + " → " + e.source + ")", act: () => savePreview(e.target, e.source, "", false) },
  ];
  items.push(...extraRows("preview", e));
  items.push({ sep: true });
  items.push({
    label: "remove preview",
    danger: true,
    act: () =>
      showLocalConfirm("Remove the preview " + e.label + "?", ["remove", "abort"], (o) => {
        if (o === "remove") removePreview(e);
      }),
  });
  showCtxMenu(items, x, y);
}


function rowEntry(li) {
  return (state.previews || []).find((x) => x.id === li.dataset.id);
}

$("previews-list").addEventListener("click", (ev) => {
  const li = ev.target.closest("li");
  if (!li || !li.dataset.id) return;
  const e = rowEntry(li);
  if (e) openPreviewEntry(e);
});

$("previews-list").addEventListener("contextmenu", (ev) => {
  const li = ev.target.closest("li");
  if (!li || !li.dataset.id) return;
  ev.preventDefault();
  const e = rowEntry(li);
  if (e) showPreviewMenu(e, ev.clientX, ev.clientY);
});


// Branch row: preview merging THIS branch into the checked-out one.
registerRows("branch", (b) => {
  const cur = (state.branches || []).find((x) => x.is_head);
  if (!b || !b.name || !cur || b.is_head) return [];
  return [
    { sep: true },
    { label: "merge preview " + b.name + " → " + cur.name + "…", act: () => openPreviewPair(b.name, cur.name) },
  ];
});

registerRows("menu", () => [{ label: "new merge preview…", act: addPreviewFlow }]);


// reopenPreviewIfMoved runs after a sidebar refresh: an open preview whose
// tips moved re-opens itself, one whose pair vanished or stopped being
// previewable closes with a notice, and an unchanged one costs nothing.
export async function reopenPreviewIfMoved() {
  const po = state.previewOpen;
  if (!po) return;
  if (state.previewsStale) return; // the list this pass would reason from never arrived
  // A pull request's head moves only when the user fetches it again, and its
  // names are not branches: there is no tip here to watch.
  if (po.pr) return;
  if (!previewShowing()) {
    state.previewOpen = null; // something else owns the screen now
    return;
  }
  if (po.id) {
    const row = (state.previews || []).find((e) => e.id === po.id);
    if (!row) {
      closePreviewView();
      opLine("preview removed");
      return;
    }
    // "error" is not a fact about the pair — it is one summary call that
    // failed (a slow merge-base). The list degrades that row instead of
    // blanking itself; the open screen does the same, by asking the server
    // again rather than tearing the diff down (TUI parity: a summary error
    // leaves the zero State, so its refresh re-resolves too).
    if (row.state === "error") {
      await openPreviewEntry(row, "");
      return;
    }
    if (row.state !== "ok") {
      closePreviewView();
      opLine("merge preview " + row.source + " → " + row.target + ": " + stateText(row), true);
      return;
    }
    // Both sides are rev-parse output here (the row comes from the server),
    // so this is a plain comparison — sameHash only matters for tipOf.
    if (sameHash(row.source_hash, po.sourceHash) && sameHash(row.target_hash, po.targetHash)) return;
    await openPreviewEntry(row, sameHash(row.source_hash, po.sourceHash) ? po.target : po.source);
    return;
  }
  // A "show once" preview has no row: the tips come from the lists this
  // refresh just reloaded — abbreviated, hence sameHash. Unknown names (a
  // deleted branch, a capped remotes list) fall through to the server, which
  // answers with the real state.
  const s = tipOf(po.source);
  const t = tipOf(po.target);
  if (s && t && sameHash(s, po.sourceHash) && sameHash(t, po.targetHash)) return;
  // Name the moved ref only when both tips are actually known here; otherwise
  // the re-open speaks for itself (and stays silent if nothing changed).
  const moved = s && t ? (sameHash(s, po.sourceHash) ? po.target : po.source) : "";
  await openOnce(po.source, po.target, moved);
}


registerHelp({
  key: "previews",
  html:
    "saved <b>merge previews</b> — what a source branch would bring into a target (GitHub's " +
    "files-changed diff). <b>+</b> on the section header adds one; right-click a row for " +
    "rename / save reversed / remove",
});

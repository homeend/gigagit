// patch.js — part of gg's web client. A feature module: it adds its own menu
// rows and help row through the registries (menus.js) instead of editing
// commits.js / files.js / index.html.
//
// Patches are how a change leaves this frontend and comes back:
//
//   export   a commit — or one file's change inside it — as a .patch the
//            browser saves, so it can be mailed, attached to a ticket, or
//            applied somewhere with no access to this repository.
//   import   a .patch file the user names, landed either in the working tree
//            or replayed as real commits.
//   copy     a bookmark's or shelf entry's files into a directory outside the
//            repository, for handing to another tool.
//
// The import path takes a SERVER-side path rather than an upload. A browser
// never learns the path behind <input type=file> — only the bytes — and this
// server is loopback-only, so the file the user means is already reachable by
// name. That is the TUI's lane too.
import { $, getJSON } from "./core.js";
import { openPrompt, showCtxMenu } from "./layers.js";
import { registerHelp, registerRows } from "./menus.js";
import { opLine, startOp } from "./ops.js";

// --- export ----------------------------------------------------------------

// downloadPatch navigates to the patch endpoint. The response carries
// Content-Disposition: attachment, so the browser saves the file and the SPA
// on this page is never replaced — assigning window.location is the whole
// download, and it keeps the browser's own "where do you want this?" flow
// instead of gg inventing one.
//
// A refusal (a merge commit, an unknown sha) answers with JSON and no
// attachment header, which the browser would render as a bare page. So the
// request is made ONCE with fetch first: on 2xx that same URL is handed to the
// location assignment (the response is cached or cheaply re-read), and on a
// refusal the server's sentence lands on the op line where every other refusal
// in this UI appears.
async function downloadPatch(url, what) {
  let resp;
  try {
    resp = await fetch(url);
  } catch (e) {
    opLine("error: " + (e.message || e), true);
    return;
  }
  if (!resp.ok) {
    const body = await resp.json().catch(() => null);
    opLine((body && body.error) || "could not export " + what, true);
    return;
  }
  window.location = url;
  opLine("exported " + what);
}


function commitPatchURL(sha, path) {
  let u = "/api/commit-patch?sha=" + encodeURIComponent(sha);
  if (path) u += "&path=" + encodeURIComponent(path);
  return u;
}


// A merge commit has no single patch — git format-patch -1 silently emits a
// different commit's — so the row is not offered on one. The server refuses it
// as well; this only keeps a row that cannot work off the menu.
registerRows("commit", (c) =>
  c && c.hash && c.parents === 1
    ? [{
        label: "export as patch…",
        act: () => downloadPatch(commitPatchURL(c.hash, ""), (c.short || c.hash.slice(0, 8)) + ".patch"),
      }]
    : []
);


// The per-file export only means something for a file being viewed AT a
// commit: in the working-tree sections there is no commit to take the file's
// change from. Same gate the TUI's diff view uses.
registerRows("file", (f) =>
  f && f.path && f.sha && f.section === "commit"
    ? [{
        label: "export this file's diff as a patch…",
        act: () => downloadPatch(commitPatchURL(f.sha, f.path), f.path + ".patch"),
      }]
    : []
);


// --- import ----------------------------------------------------------------

// applyPatchPrompt asks for the file and starts the op. No mode is sent: the
// engine detects the format itself, sends a plain diff to the working tree,
// and — for a format-patch mailbox — parks its own working-tree/commits/abort
// question in the modal. Deciding that here would mean reading the file over
// HTTP to ask a question the engine already asks better.
//
// The typed path is sent verbatim. It resolves relative to where `gg web` is
// running, which is what the user is looking at when they type it.
function applyPatchPrompt() {
  openPrompt({
    title: "Apply patch — path to the .patch file:",
    value: "",
    placeholder: "/path/to/0001-something.patch",
    onSubmit: (path) => startOp({ op: "apply-patch", path }, "applying " + path),
  });
}


// --- copy an entry to a temp dir -------------------------------------------

// entryLabel names an entry the way the sidebar's own lists do, so the picker
// and the list you read the entry off show the same words. (Copied rather than
// imported: sidebar.js is not this feature's to touch, and a feature module
// importing it would tie the two load orders together.)
function entryLabel(e) {
  return e.label || e.display || e.id;
}


// copyEntryPrompt asks WHERE, prefilled with `<main-worktree>.tmp/<name>` —
// the same destination the TUI offers. The prefill comes from the server
// because it is anchored on the MAIN worktree: computing it in the browser
// from /api/repo would put the copies beside a linked worktree whenever gg web
// is served from one.
//
// An existing directory is the engine's overwrite decision and parks in the
// modal; nothing is asked twice here.
async function copyEntryPrompt(store, e) {
  const got = await getJSON(
    "/api/export-dest?store=" + encodeURIComponent(store) + "&id=" + encodeURIComponent(e.id)
  ).catch((err) => {
    opLine(err.message || String(err), true);
    return null;
  });
  if (!got) return;
  openPrompt({
    title: "Copy " + entryLabel(e) + " (" + got.files + " file(s)) to:",
    value: got.dir,
    placeholder: "/path/to/a/directory",
    onSubmit: (dest) => startOp({ op: "export-to-dir", store, id: e.id, dest }, "copying to " + dest),
  });
}


// pickEntryToCopy lists what is in both stores and copies the one picked.
//
// The TUI hangs this off a bookmark's / shelf entry's own row. The sidebar's
// two menus are not extensible yet (menus.js reaches the other eight), so it
// is reached from the ☰ menu instead and names the entry in the picker. See
// the note in CHANGELOG.
async function pickEntryToCopy(x, y) {
  const [bm, sh] = await Promise.all([
    getJSON("/api/bookmarks").catch(() => null),
    getJSON("/api/shelf").catch(() => null),
  ]);
  const rows = [];
  const bookmarks = (bm && bm.entries) || [];
  const shelf = (sh && sh.entries) || [];
  if (bookmarks.length) {
    rows.push({ header: "bookmarks" });
    for (const e of bookmarks) rows.push({ label: entryLabel(e), act: () => copyEntryPrompt("bookmarks", e) });
  }
  if (shelf.length) {
    rows.push({ header: "shelf" });
    for (const e of shelf) rows.push({ label: entryLabel(e), act: () => copyEntryPrompt("shelf", e) });
  }
  if (!rows.length) {
    opLine("nothing is bookmarked or shelved yet", true);
    return;
  }
  showCtxMenu(rows, x, y);
}


registerRows("menu", () => [
  { label: "apply a patch…", act: () => applyPatchPrompt() },
  {
    label: "copy a bookmark or shelf entry to a directory…",
    // The ☰ button is the anchor the menu that contained this row opened
    // from, so the picker lands under it rather than at the pointer, which is
    // wherever the click happened to be.
    act: () => {
      const r = $("menu-btn").getBoundingClientRect();
      pickEntryToCopy(r.left, r.bottom + 4);
    },
  },
]);


registerHelp({
  key: "patches",
  html:
    "right-click a commit → <b>export as patch…</b> (or a file in a commit → its diff alone); " +
    "☰ → <b>apply a patch…</b> to read one back in, <b>copy a bookmark or shelf entry…</b> to write its files outside the repo",
});

export { applyPatchPrompt, commitPatchURL, copyEntryPrompt, pickEntryToCopy };

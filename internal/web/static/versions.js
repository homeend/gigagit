// versions.js — part of gg's web client. Split from the original app.js;
// see app.js (the entry module) for the load order.
import { $, esc, getJSON, state } from "./core.js";
import { closeLayer, copyText, pushLayer, showCtxMenu } from "./layers.js";
import { opLine, showLocalConfirm, startOp } from "./ops.js";
import { openCompare } from "./files.js";

// --- branch versions (the operations history) ---
//
// Every destructive operation snapshots the branch tip before it runs, so
// this list is "what this branch pointed at before each of the last N
// operations" — the undo of last resort. Restoring is itself snapshotted, so
// nothing here is a one-way door.

// versionWhen renders a snapshot's age. Absolute dates read as history;
// what you actually want to know is "how far back do I have to go".
function versionWhen(unix) {
  const secs = Math.max(0, Math.floor(Date.now() / 1000) - unix);
  const units = [
    [86400 * 7, "w"],
    [86400, "d"],
    [3600, "h"],
    [60, "m"],
  ];
  for (const [size, tag] of units) {
    if (secs >= size) return Math.floor(secs / size) + tag + " ago";
  }
  return "just now";
}


async function openVersions(branch, opts) {
  let body;
  try {
    body = await getJSON("/api/versions?branch=" + encodeURIComponent(branch));
  } catch (e) {
    opLine("versions failed: " + (e.message || e), true);
    return;
  }
  const rows = body.versions || [];
  // The deleted tag matters here: it says the restore row will RECREATE the
  // branch, and why there is no compare row (nothing live to compare with).
  $("versions-title").textContent =
    branch + " — previous versions" + (opts && opts.deleted ? " (branch deleted)" : "");
  $("versions-list").innerHTML = rows.length
    ? rows
        .map(
          (v, i) =>
            `<li data-i="${i}"><span class="vwhen">${esc(versionWhen(v.unix))}</span>` +
            `<span class="vop">${esc(v.op)}</span>` +
            `<span class="vsha">${esc(v.short)}</span>` +
            `<span class="vsub">${esc(v.subject)}</span></li>`
        )
        .join("")
    : `<li class="empty">no recorded versions — they are written as operations run</li>`;
  $("versions-list")._rows = rows;
  $("versions-list")._branch = branch;
  pushLayer("versions", $("versions"));
}


function closeVersions() {
  closeLayer("versions");
}


// The live tip hash of a local branch, or "" when no such branch exists.
// This is the version-compare gate: a deleted branch has no tip to compare
// against (the TUI's "restore it to compare" rule). The value is git's
// abbreviated sha (what /api/branches serves) — fine for the rev form,
// which takes any plain hex id: it stays immutable for the object, and an
// ambiguous abbreviation fails loudly server-side.
function branchTipHash(name) {
  const b = (state.branches || []).find((x) => x.name === name);
  return b ? b.hash : "";
}


// Per-row actions go through the shared ctx-menu rather than inline buttons:
// same interaction language as the sidebar, and the row stays readable.
function showVersionMenu(branch, v, x, y) {
  const items = [];
  const tip = branchTipHash(branch);
  if (tip) {
    items.push({
      label: "compare against current tip",
      act: () => {
        closeVersions();
        closeVersionBranches(); // reached via the picker: the compare replaces it
        openCompare(v.hash, tip, {
          revs: true,
          aLabel: branch + "@" + v.short,
          bLabel: branch + " (tip)",
        });
      },
    });
  }
  showCtxMenu(
    items.concat([
      {
        label: "restore " + branch + " to this version",
        act: () =>
          // The engine only asks when the CURRENT branch is dirty; every
          // other lane moves the ref with no prompt at all. So the confirm
          // is the client's, as with delete-tag and discard.
          showLocalConfirm(
            "Move " + branch + " back to " + v.short + " (" + v.subject + ")?",
            ["restore", "abort"],
            (o) => {
              if (o !== "restore") return;
              closeVersions();
              closeVersionBranches(); // reached via the picker: its rows are now stale
              startOp(
                { op: "restore-version", branch, ref: v.ref },
                "restoring " + branch + " to " + v.short
              );
            }
          ),
      },
      { label: "copy commit id", act: () => copyText(v.hash, "commit id " + v.short) },
      {
        label: "delete this snapshot",
        danger: true,
        act: () =>
          showLocalConfirm("Delete the " + v.op + " snapshot at " + v.short + "?", ["delete", "abort"], (o) => {
            if (o !== "delete") return;
            closeVersions();
            closeVersionBranches(); // reached via the picker: its rows are now stale
            startOp({ op: "delete-version", ref: v.ref }, "deleting snapshot " + v.short);
          }),
      },
    ]),
    x,
    y
  );
}


// Both buttons open the row menu. LEFT click must stopPropagation: the
// document-level outside-click closer would otherwise see this very click
// bubble up and shut the menu in the same event, so the row would look dead
// (the ☰ button's lesson). RIGHT click must preventDefault or the browser's
// own context menu covers ours.
function versionRowMenu(e) {
  const li = e.target.closest("li[data-i]");
  if (!li) return;
  e.preventDefault();
  e.stopPropagation();
  const list = $("versions-list");
  const v = list._rows[Number(li.dataset.i)];
  if (v) showVersionMenu(list._branch, v, e.clientX, e.clientY);
}

$("versions-list").addEventListener("click", versionRowMenu);

$("versions-list").addEventListener("contextmenu", versionRowMenu);

$("versions").addEventListener("click", (e) => {
  if (e.target.id === "versions") closeVersions(); // backdrop
});


// --- all-branches versions picker (the deleted-branch recovery path) ---
//
// Lists every branch with recorded versions, deleted ones tagged — the only
// route to a DELETED branch's snapshots, whose restore recreates the ref.
// Clicking a row opens the versions overlay ON TOP (it sits at z-index 21,
// this picker at 20), so esc drills back out to the picker for free.
async function openVersionBranches() {
  let body;
  try {
    body = await getJSON("/api/version-branches");
  } catch (e) {
    opLine("branch versions failed: " + (e.message || e), true);
    return;
  }
  const rows = body.branches || [];
  $("vbranches-list").innerHTML = rows.length
    ? rows
        .map(
          (b, i) =>
            `<li data-i="${i}"><span class="vbname">${esc(b.branch)}</span>` +
            (b.deleted ? `<span class="vbdel">deleted</span>` : "") +
            `<span class="vbcount">${b.count} snapshot${b.count === 1 ? "" : "s"}</span>` +
            `<span class="vbwhen">${esc(versionWhen(b.latest_unix))}</span></li>`
        )
        .join("")
    : `<li class="empty">no recorded versions anywhere — they are written as operations run</li>`;
  $("vbranches-list")._rows = rows;
  pushLayer("vbranches", $("vbranches"));
}


function closeVersionBranches() {
  closeLayer("vbranches");
}


$("vbranches-list").addEventListener("click", (e) => {
  const li = e.target.closest("li[data-i]");
  if (!li) return;
  const b = $("vbranches-list")._rows[Number(li.dataset.i)];
  if (b) openVersions(b.branch, { deleted: b.deleted });
});

$("vbranches").addEventListener("click", (e) => {
  if (e.target.id === "vbranches") closeVersionBranches(); // backdrop
});

export { branchTipHash, closeVersionBranches, closeVersions, openVersionBranches, openVersions, showVersionMenu, versionRowMenu, versionWhen };

// Mouse-first close buttons (testing feedback): a pointer-only user must
// never need a key to leave an overlay.
$("versions-close").addEventListener("click", () => closeLayer("versions"));
$("vbranches-close").addEventListener("click", () => closeLayer("vbranches"));

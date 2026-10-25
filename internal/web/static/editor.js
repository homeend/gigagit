// editor.js — open in external editor (the TUI's Edit in editor, e, I): the
// server runs its $VISUAL / $EDITOR in a console session, on the working-tree
// file itself or on a read-only copy (POST /api/editor-open). The console is
// the terminal the TUI hands over; when the editor exits its session leaves
// the list and the op line says what the TUI's status line says.
import { $, postJSON, state } from "./core.js";
import { openConsole } from "./console.js";
import { opBusy, opLine } from "./ops.js";
import { fetchStatus, wtCount } from "./status.js";
import { registerHelp, registerRows } from "./menus.js";
import { activeDiff, noteScope } from "./stackview.js";
import { diffBodyEl } from "./files.js";

// --- editor model (pure; guarded against Go) ---
// pickEditLine is the diff line the editor opens at (the TUI's editLine):
// the clicked row's new-side number, else the first row in view that has
// one; 0 = none. rows: [{rno, visible, cur}] in display order.
function pickEditLine(rows) {
  const cur = rows.find((r) => r.cur && r.rno > 0);
  if (cur) return cur.rno;
  const seen = rows.find((r) => r.visible && r.rno > 0);
  return seen ? seen.rno : 0;
}

// diffEditReq is what `e` opens for the diff on screen (the TUI's
// diffEditRow): a working-tree diff — the staged one too, its line
// approximate — edits the file itself; a commit's diff opens a copy of that
// version; a two-sided compare (or the all-files tree diff) has no single
// file and offers nothing.
function diffEditReq(ctx, line) {
  if (!ctx || !ctx.path || ctx.compare || ctx.cmpSides) return null;
  if (ctx.rev === "") return { source: "worktree", path: ctx.path, line };
  if (!/^[0-9a-f]{7,64}$/.test(ctx.rev || "")) return null;
  return { source: "commit", path: ctx.path, rev: ctx.rev, line };
}

// finishedEditors: the editor sessions this page started that have left the
// session list (their editor exited).
function finishedEditors(pending, list) {
  return [...pending.keys()].filter((id) => !(list || []).some((s) => s.id === id));
}
// --- end editor model ---

const pending = new Map(); // session id -> {name, live}

// openInEditor starts the editor and shows its console, bound. An editor
// that has already exited (a GUI one returns at once) gets no console.
// gate: the TUI offers the live edit, the staged copy and the diff's row only
// while no operation runs (a rebase rewriting the file under the editor).
async function openInEditor(req, gate = false) {
  if (gate && opBusy()) return;
  let r;
  try {
    r = await postJSON("/api/editor-open", req);
  } catch (e) {
    return opLine((req.source === "worktree" ? "edit: " : "view: ") + (e.message || e), true);
  }
  pending.set(r.id, { name: r.name, live: !!r.live });
  if (!r.session) return finishEditor(r.id);
  openConsole(r.id, { editor: () => finishEditor(r.id) });
}

// finishEditor is the TUI's editorFinishedMsg / editorViewFinishedMsg: the
// status line, and for a live edit the status reload.
function finishEditor(id) {
  const p = pending.get(id);
  if (!p) return;
  pending.delete(id);
  opLine((p.live ? "edited " : "viewed ") + p.name);
  if (p.live) fetchStatus();
}

// editorsChanged runs on every sessions event (live.js).
function editorsChanged(list) {
  for (const id of finishedEditors(pending, list)) finishEditor(id);
}

// rowNewNo is a diff row's new-side line: its right number cell, which every
// layout draws (split, unified, a pure add or delete) — data-rno is the
// split layout's alone, and only with notes on.
function rowNewNo(tr) {
  const td = tr && tr.querySelector("td.no.r");
  return (td && Number(td.textContent)) || 0;
}

// diffFile is the diff on screen that `e` acts on: the anchored file's own
// section in a stack (the TUI looks only inside the cursor's file), else the
// pane; null when it draws no line table (a binary or too-large file).
function diffFile() {
  if (state.layout !== "diff") return null;
  const scope = noteScope();
  return scope && scope.querySelector("table.diff") ? scope : null;
}

// diffEditLine reads pickEditLine's rows off that file.
function diffEditLine(scope) {
  const top = diffBodyEl().getBoundingClientRect().top;
  const rows = [...scope.querySelectorAll("table.diff tr[data-i]")].map((tr) => ({
    rno: rowNewNo(tr),
    cur: tr.classList.contains("cur"),
    visible: tr.getBoundingClientRect().bottom > top,
  }));
  return pickEditLine(rows);
}

// diffEditOpen is the diff's `e` (and its menu row): false when the diff on
// screen offers no editor (a compare, a binary or too-large file, none).
function diffEditOpen() {
  const scope = diffFile();
  const req = scope && diffEditReq(activeDiff().ctx, diffEditLine(scope));
  if (!req) return false;
  openInEditor(req, true);
  return true;
}

// diffEditRow is the diff menu's "open in editor at line N" for the row
// right-clicked (its own new-side line, else the diff's), or null.
function diffEditRow(tr) {
  const scope = diffFile();
  if (!scope) return null;
  const line = (tr && scope.contains(tr) && rowNewNo(tr)) || diffEditLine(scope);
  const req = diffEditReq(activeDiff().ctx, line);
  return req ? { label: "open in editor at line " + line, act: () => openInEditor(req, true) } : null;
}

// commitMessageInEditor is the commit list's `I`: the cursor's commit.
function commitMessageInEditor() {
  const c = state.rows[state.cursor - wtCount()];
  if (c && c.hash) openInEditor({ source: "message", rev: c.hash });
}

// The file rows (the working-tree list, a commit's file list): edit the
// working file, open the staged copy, or open the commit's version.
registerRows("fileview", (ctx) => {
  if (!ctx.path || ctx.deleted) return [];
  if (ctx.section === "commit") return ctx.sha ? [{ label: "open in external editor", act: () => openInEditor({ source: "commit", path: ctx.path, rev: ctx.sha }) }] : [];
  if (ctx.section === "staged") return [{ label: "open staged version in external editor", act: () => openInEditor({ source: "staged", path: ctx.path }, true) }];
  return [{ label: "edit in editor", act: () => openInEditor({ source: "worktree", path: ctx.path }, true) }];
});

registerRows("commit", (c) => (c && c.hash ? [{ label: "open message in editor", hint: "I", act: () => openInEditor({ source: "message", rev: c.hash }) }] : []));

registerRows("bookmark", (e) => (e && e.id && e.path && e.kind !== "commit" ? [{ label: "open in external editor", act: () => openInEditor({ source: "bookmark", id: e.id }) }] : []));

registerRows("shelf", (e) =>
  e && e.id && e.path && e.kind !== "commit" && e.kind !== "files" ? [{ label: "open in external editor", act: () => openInEditor({ source: "shelf", id: e.id }) }] : []
);

registerHelp({
  key: "external editor",
  html:
    "the server's <b>$VISUAL</b> / <b>$EDITOR</b> (vi, or notepad on Windows) runs in a console over the panes, as the terminal's editor takes the terminal: " +
    "a working-tree file's <b>edit in editor</b> edits the file itself (the list reloads when the editor exits — <i>edited f.txt</i>); " +
    "<b>open staged version in external editor</b>, a commit file's <b>open in external editor</b>, a bookmarked or shelved file's, and <b>I</b> on a commit (its message) open a read-only copy, removed when the editor exits (<i>viewed f.txt</i>). " +
    "In a diff <b>e</b> (or the menu's <b>open in editor at line N</b>) opens it at the clicked line: the working file for a working-tree diff, a copy of the shown version for a commit's — not on a two-sided compare. " +
    "<b>e</b> in the file history opens the selected commit's version, in blame the blamed one. The editor gets the line as <code>+N</code> (vi, nano, emacs…), <code>--goto path:N</code> (code, cursor) or <code>path:N</code> (hx, subl, zed)",
});

export { commitMessageInEditor, diffEditOpen, diffEditReq, diffEditRow, editorsChanged, finishedEditors, openInEditor, pickEditLine };

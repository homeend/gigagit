// allnotes.js — "view all notes…" (the TUI command palette's View all notes):
// every note this checkout can see, as the TUI's tree — group (working tree /
// commits / other) → state, commit or shelf entry → directory → file → note —
// each note row laid out in fixed columns (status, who, where, when, note).
// Enter or a click on a note opens its diff and lands on it, on a shelf
// entry's own note (a recycle's) that note's text, on a review the
// review view; esc on that diff (or out of the review) comes back here.
// ctrl+d deletes the thread or review under the cursor after asking. One read of GET /api/notes/overview; deletes reuse
// POST /api/notes/remove (the same domain call as the TUI's).
import { $, charWidth, elideNoteSummary, esc, getJSON, postJSON, state } from "./core.js";
import { closeLayer, mountOverlay, pushLayer } from "./layers.js";
import { registerHelp } from "./menus.js";
import { opLine, showLocalConfirm } from "./ops.js";
import { openCommitByHash } from "./commits.js";
import { landNote, openFile, openWorkingTree, refreshNoteCounts, setDiffBack } from "./files.js";
import { openReview } from "./reviews.js";
import { openShelfNotes } from "./shelfnotes.js";

// This module builds its own DOM: index.html's `hidden` class has NO global
// rule — the overlay ships its own `#allnotes.hidden` selector. z-index 21:
// below #modal (30), which the ctrl+d confirm raises over it (style.css's
// overlay-stacking note).
const CSS = `
#allnotes { position: fixed; inset: 0; background: rgba(0,0,0,0.45); display: flex; align-items: flex-start; justify-content: center; z-index: 21; }
#allnotes.hidden { display: none; }
#allnotes-box { margin-top: 6vh; width: min(1000px, 94vw); background: var(--bg-alt); border: 1px solid var(--border); border-radius: 6px; box-shadow: 0 8px 30px rgba(0,0,0,0.5); overflow: hidden; display: flex; flex-direction: column; max-height: 84vh; }
#allnotes-head { padding: 8px 10px; font-weight: 600; display: flex; gap: 10px; align-items: baseline; }
#allnotes-input { flex: 1; min-width: 0; background: var(--bg); color: var(--fg); border: 1px solid var(--border); border-radius: 4px; padding: 3px 8px; font: inherit; font-weight: normal; }
#allnotes-input:focus { outline: none; border-color: var(--accent); }
#allnotes-cols, #allnotes-list li.an-note, #allnotes-list li.an-review { display: flex; white-space: nowrap; }
#allnotes-cols { color: var(--dim); padding: 2px 10px 2px calc(10px + 10ch); border-bottom: 1px solid var(--border); }
#allnotes-list { list-style: none; margin: 0; padding: 0; overflow-y: auto; flex: 1; }
#allnotes-list li { padding: 2px 10px; cursor: pointer; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
#allnotes-list li.sel { background: var(--sel); }
#allnotes-list li.an-head { font-weight: 600; }
#allnotes-list li.empty { color: var(--dim); cursor: default; }
#allnotes-list li.an-note, #allnotes-list li.an-review { padding-left: calc(10px + 10ch); }
.an-c { flex: none; overflow: hidden; text-overflow: ellipsis; padding-right: 1ch; box-sizing: border-box; }
.an-status { width: 9ch; } .an-who { width: 10ch; } .an-where { width: 10ch; } .an-when { width: 8ch; }
.an-sum { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; }
.an-who.user { color: var(--accent); } .an-who.agent { color: #b48bff; }
.an-status.dim { color: var(--dim); }
#allnotes-notice { color: var(--dim); padding: 4px 10px; border-top: 1px solid var(--border); }
#allnotes-notice:empty { display: none; }
#allnotes-hints { color: var(--dim); font-size: 11px; padding: 6px 10px; border-top: 1px solid var(--border); }
`;

const styleEl = document.createElement("style");
styleEl.textContent = CSS;
document.head.append(styleEl);

const box = mountOverlay("allnotes");
box.innerHTML =
  `<div id="allnotes-box">` +
  `<div id="allnotes-head"><span id="allnotes-title">All notes</span>` +
  `<input id="allnotes-input" type="text" autocomplete="off" spellcheck="false" placeholder="type to filter"></div>` +
  `<div id="allnotes-cols"><span class="an-c an-status">STATUS</span><span class="an-c an-who">WHO</span>` +
  `<span class="an-c an-where">WHERE</span><span class="an-c an-when">WHEN</span><span class="an-sum">NOTE</span></div>` +
  `<ul id="allnotes-list"></ul>` +
  `<div id="allnotes-notice"></div>` +
  `<div id="allnotes-hints"></div></div>`;

// an is the open popup: the tree rows, folds, cursor and the notice under the
// list (why the last open could not go anywhere). It survives a diff opened
// from it (kept), so esc on that diff reopens it where it was.
let an = null;
let anGen = 0;

// --- the tree (pure: the TUI's buildAllNotesRows / visible) -----------------

// anBuildRows flattens the overview into the tree, depth-first, and stamps
// each row's span (one past its last descendant) so folding and filtering can
// skip a subtree.
function anBuildRows(ov) {
  const rows = [];
  const files = (depth, fs, t) => {
    let lastDir = "";
    for (const f of fs) {
      const cut = f.path.lastIndexOf("/");
      const dir = cut < 0 ? "" : f.path.slice(0, cut);
      if (dir !== "" && dir !== lastDir) rows.push({ kind: "dir", depth, text: dir + "/" });
      lastDir = dir;
      const ft = { ...t, state: f.state, path: f.path, change: f.status || "", oldPath: f.old_path || "" };
      rows.push({ kind: "file", depth: depth + 1, text: f.path.slice(cut + 1), target: ft });
      for (const n of f.notes || []) {
        rows.push({ kind: "note", depth: depth + 2, note: n, status: t.missing ? "missing" : n.status, target: ft,
          filter: (n.summary + "\0" + (n.author || "") + "\0" + f.path).toLowerCase() });
      }
    }
  };
  const group = (key, text) => rows.push({ kind: "group", depth: 0, text, key });
  const sub = (key, text) => rows.push({ kind: "sub", depth: 1, text, key });

  const unstaged = ov.unstaged || [], staged = ov.staged || [], untracked = ov.untracked || [];
  if (unstaged.length + staged.length + untracked.length > 0) {
    let label = "Working tree";
    if (ov.worktree && ov.worktree !== ".") label += "  (" + ov.worktree + ")";
    group("wt", label);
    for (const s of [["wt:unstaged", "Unstaged", unstaged], ["wt:staged", "Staged", staged], ["wt:untracked", "Untracked", untracked]]) {
      if (!s[2].length) continue;
      sub(s[0], s[1]);
      files(2, s[2], {});
    }
  }
  const commits = ov.commits || [];
  if (commits.length) {
    group("commits", "Commits");
    for (const c of commits) {
      let label = c.hash.slice(0, 7) + "  ";
      if (c.missing) {
        label += "(missing — rewritten or deleted)";
      } else {
        label += c.subject;
        if (c.time > 0) {
          // The TUI's CommitDateLayout ("2006-01-02 15:04"), local time.
          const d = new Date(c.time * 1000);
          const p = (n) => String(n).padStart(2, "0");
          label += "  · " + d.getFullYear() + "-" + p(d.getMonth() + 1) + "-" + p(d.getDate()) + " " + p(d.getHours()) + ":" + p(d.getMinutes());
        }
      }
      sub("c:" + c.hash, label);
      if ((c.reviews || []).length) {
        rows.push({ kind: "dir", depth: 2, text: "Reviews" });
        for (const r of c.reviews) {
          rows.push({ kind: "review", depth: 3, review: r,
            filter: (r.summary + "\0" + (r.agent || "") + "\0" + (r.branch || "")).toLowerCase() });
        }
      }
      files(2, c.files || [], { commit: c.hash, subject: c.hash.slice(0, 7) + " " + c.subject, missing: c.missing });
    }
  }
  const shelves = ov.shelves || [];
  if (shelves.length) {
    group("other", "Other");
    for (const s of shelves) {
      const name = s.label || s.id;
      sub("s:" + s.id, "shelf  " + name);
      // The entry's own notes (a recycle's "deleted …", "renamed …") head
      // its files, oldest first; enter reads one.
      for (const n of s.entry || []) {
        rows.push({ kind: "note", depth: 2, note: n, status: s.missing ? "missing" : n.status,
          target: { shelf: s.id, label: name, missing: s.missing },
          filter: (n.summary + "\0" + (n.author || "") + "\0" + name).toLowerCase() });
      }
      files(2, s.files || [], { missing: s.missing });
    }
  }
  for (let i = 0; i < rows.length; i++) {
    let j = i + 1;
    while (j < rows.length && rows[j].depth > rows[i].depth) j++;
    rows[i].span = j;
  }
  return rows;
}

// anVisible is the rows on screen: with a query, the matching notes and
// reviews and their ancestors (folds ignored — a match must be seen); without
// one, every row not under a folded heading.
function anVisible(rows, query, folded) {
  const out = [];
  if (query) {
    const q = query.toLowerCase();
    const leaf = (r) => r.kind === "note" || r.kind === "review";
    const match = rows.map((r) => leaf(r) && r.filter.includes(q));
    rows.forEach((r, i) => {
      let keep = match[i];
      if (!leaf(r)) for (let j = i + 1; j < r.span && !keep; j++) keep = match[j];
      if (keep) out.push(r);
    });
    return out;
  }
  for (let i = 0; i < rows.length; i++) {
    const r = rows[i];
    out.push(r);
    if (r.key && folded[r.key]) i = r.span - 1;
  }
  return out;
}

// anAgo is the TUI's coarseAgo: the largest whole unit, one letter.
function anAgo(ms) {
  if (ms >= 86400000) return Math.floor(ms / 86400000) + "d";
  if (ms >= 3600000) return Math.floor(ms / 3600000) + "h";
  if (ms >= 60000) return Math.floor(ms / 60000) + "m";
  return Math.max(Math.floor(ms / 1000), 0) + "s";
}

// --- rendering ----------------------------------------------------------------

const STATUS_WORD = { active: "active", stale: "stale", orphaned: "orphaned", missing: "missing" };

function noteCells(r, now) {
  const n = r.note;
  const who = n.author || (n.source === "agent" ? "agent" : "you");
  const range = n.range || [n.line, n.line];
  let where = (n.side || "new") + ":" + range[0];
  if (range[1] !== range[0]) where += "-" + range[1];
  if (n.file_level) where = "file";
  if (r.target && r.target.shelf) where = "shelf";
  const when = n.created ? anAgo(now - Date.parse(n.created)) : "";
  const replies = (n.replies || []).length;
  return { status: STATUS_WORD[r.status] || r.status, who, where, when, summary: n.summary, tail: replies ? "  ↩" + replies : "" };
}

function reviewCells(r, now) {
  const v = r.review;
  let where = "commit";
  if (v.kind === "branch") where = "branch " + v.branch;
  else if (v.kind === "was_tip") where = "was tip " + v.branch;
  const when = v.created ? anAgo(now - Date.parse(v.created)) : "";
  return { status: "review", who: v.agent || "agent", where, when, summary: v.summary, tail: "" };
}

function columnsHTML(c, whoCls, dimStatus) {
  const full = [c.status, c.who, c.where, c.when, c.summary + c.tail].filter(Boolean).join(" · ");
  return {
    title: full,
    html:
      `<span class="an-c an-status${dimStatus ? " dim" : ""}">${esc(c.status)}</span>` +
      `<span class="an-c an-who ${whoCls}">${esc(c.who)}</span>` +
      `<span class="an-c an-where">${esc(c.where)}</span>` +
      `<span class="an-c an-when">${esc(c.when)}</span>` +
      `<span class="an-sum">${esc(c.summary)}${esc(c.tail)}</span>`,
  };
}

function visible() {
  return anVisible(an.rows, an.query, an.folded);
}

function render() {
  const list = $("allnotes-list");
  const count = an.rows.filter((r) => r.kind === "note" || r.kind === "review").length;
  $("allnotes-title").textContent = count ? "All notes  " + count : "All notes";
  $("allnotes-notice").textContent = an.notice ? "▸ " + an.notice : "";
  $("allnotes-cols").style.display = an.loading || an.err || !an.rows.length ? "none" : "";
  const vis = an.loading || an.err ? [] : visible();
  const hints = ["↑/↓ move", "enter open / fold", "←/→ fold"];
  const cur = vis[an.sel];
  if (cur && (cur.kind === "note" || cur.kind === "review")) hints.push("ctrl+d delete");
  hints.push("type to filter", "esc close");
  $("allnotes-hints").textContent = hints.join("   ");
  if (an.loading) return void (list.innerHTML = `<li class="empty">(loading…)</li>`);
  if (an.err) return void (list.innerHTML = `<li class="empty">${esc(an.err)}</li>`);
  if (!an.rows.length) return void (list.innerHTML = `<li class="empty">No notes in this repository.</li>`);
  if (!vis.length) return void (list.innerHTML = `<li class="empty">(no matching notes)</li>`);
  const now = Date.now();
  // The NOTE column's width in characters: the row less its padding (10px
  // each side + the 10ch indent) and the 37ch of fixed columns.
  const sumCols = Math.floor((list.clientWidth - 20) / charWidth()) - 10 - 37 - 1;
  list.innerHTML = vis
    .map((r, i) => {
      const sel = i === an.sel ? " sel" : "";
      if (r.kind === "note" || r.kind === "review") {
        const c = r.kind === "note" ? noteCells(r, now) : reviewCells(r, now);
        if (r.kind === "note" && r.target.shelf && sumCols - c.tail.length > 4) {
          // A recycle's "Recycled from <dir> (<branch>)": the path loses its
          // middle, the branch stays (the TUI's elideNoteSummary).
          c.summary = elideNoteSummary(c.summary, sumCols - c.tail.length);
        }
        const whoCls = r.kind === "review" || r.note.source === "agent" ? "agent" : "user";
        const { html, title } = columnsHTML(c, whoCls, r.kind === "note" && r.status !== "active");
        return `<li class="an-${r.kind}${sel}" data-i="${i}" title="${esc(title)}">${html}</li>`;
      }
      const indent = `style="padding-left: calc(10px + ${2 * (r.kind === "group" || r.kind === "sub" ? r.depth : r.depth + 1)}ch)"`;
      if (r.kind === "group" || r.kind === "sub") {
        const mark = an.folded[r.key] && !an.query ? "▸ " : "▾ ";
        return `<li class="an-head${sel}" data-i="${i}" ${indent} title="${esc(r.text)}">${esc(mark + r.text)}</li>`;
      }
      const head = r.kind === "dir" ? " an-head" : "";
      return `<li class="an-${r.kind}${head}${sel}" data-i="${i}" ${indent} title="${esc(r.text)}">${esc(r.text)}</li>`;
    })
    .join("");
  const el = list.querySelector("li.sel");
  if (el) el.scrollIntoView({ block: "nearest" });
}

// --- open / close -------------------------------------------------------------

async function openAllNotes() {
  if (an && !an.hidden) return; // already open
  an = { rows: [], folded: {}, query: "", sel: 0, loading: true, err: "", notice: "", hidden: false };
  show();
  load();
}

// show (re)puts the kept popup on screen: a fresh open, or the return from a
// diff it opened.
function show() {
  an.hidden = false;
  $("allnotes-input").value = an.query;
  pushLayer("allnotes", box, { onKey: anKey });
  render();
  $("allnotes-input").focus();
}

function closeAllNotes() {
  an = null;
  anGen++; // an in-flight read is stale now
  $("allnotes-input").blur(); // a focused input would swallow every global key
  closeLayer("allnotes");
}

// hide takes the popup off screen but keeps it, for the diff it opens.
function hide() {
  an.hidden = true;
  $("allnotes-input").blur();
  closeLayer("allnotes");
}

async function load() {
  const gen = ++anGen;
  let ov;
  try {
    ov = await getJSON("/api/notes/overview");
  } catch (e) {
    if (!an || gen !== anGen) return;
    an.loading = false;
    an.err = e.message || String(e);
    render();
    return;
  }
  if (!an || gen !== anGen) return; // closed, or re-read meanwhile
  an.loading = false;
  an.err = "";
  an.rows = anBuildRows(ov);
  // A re-read after a delete can be shorter than the cursor: keep it on a row.
  const n = visible().length;
  if (an.sel >= n) an.sel = Math.max(n - 1, 0);
  render();
}

function move(d) {
  const n = visible().length;
  an.sel = n ? Math.min(Math.max(an.sel + d, 0), n - 1) : 0;
  an.notice = "";
  render();
}

// setFold folds or unfolds the heading under the cursor. On a row that is not
// a heading, ← goes to its parent heading instead.
function setFold(fold) {
  const vis = visible();
  const r = vis[an.sel];
  if (!r || an.query) return;
  if (r.key) {
    an.folded[r.key] = fold;
    move(0);
    return;
  }
  if (!fold) return;
  for (let i = an.sel - 1; i >= 0; i--) {
    if (vis[i].key && vis[i].depth < r.depth) {
      an.sel = i;
      break;
    }
  }
  render();
}

function activate(i) {
  const r = visible()[i];
  if (!r) return;
  an.sel = i;
  switch (r.kind) {
    case "group":
    case "sub":
      if (!an.query) an.folded[r.key] = !an.folded[r.key];
      render();
      return;
    case "file":
      openTarget(r.target, "");
      return;
    case "note":
      if (r.target.shelf) {
        // The note is its own text: the read-only window reads it even when
        // the entry is gone; esc comes back here.
        openShelfNotes({ id: r.target.shelf }, r.target.label, r.note.id);
        return;
      }
      openTarget(r.target, r.note.id);
      return;
    case "review":
      // The review lives in the note: it opens even when the commit is gone.
      // esc from the review view comes back here.
      hide();
      openReview(r.review.id, { kind: "popup", run: reshow });
      return;
  }
  render();
}

// notice puts why an open went nowhere under the list, as the TUI does.
function notice(text) {
  an.notice = text;
  render();
}

// openTarget opens t's diff with the popup hidden and, when id is set, lands
// on that note once the diff's notes have painted; esc on the diff reopens
// the popup.
async function openTarget(t, id) {
  an.notice = "";
  if (t.state === "commit") {
    if (t.missing) return notice("That commit no longer exists.");
    hide();
    if (!(await openCommitByHash(t.commit, t.subject))) return show();
    const i = state.files.findIndex((f) => f.path === t.path);
    if (i < 0) {
      show();
      return notice("The commit does not change " + t.path + ".");
    }
    await openFile(i);
  } else if (t.state === "unstaged" || t.state === "staged" || t.state === "untracked") {
    const section = t.state === "unstaged" ? "changes" : t.state;
    hide();
    await openWorkingTree(0); // re-reads status: the file may have moved on
    const i = state.statusEntries.findIndex((f) => f.path === t.path && f.section === section);
    if (i < 0) {
      show();
      return notice(
        t.state === "staged" ? "The file has no staged changes now."
          : t.state === "untracked" ? "The file is no longer untracked."
          : "The file has no unstaged changes now.",
      );
    }
    await openFile(i);
  } else {
    return notice("Shelf notes open from gg note list.");
  }
  setDiffBack(reshow);
  if (id && !(await landNote(id))) opLine("the note is not in this diff now", true);
}

// reshow brings the popup back from a diff or review it opened, re-read: the
// notes may have changed meanwhile.
function reshow() {
  if (an) {
    show();
    load();
  }
}

// deleteSelected is ctrl+d: the review or thread under the cursor, after a
// confirm whose default is cancel.
function deleteSelected() {
  const r = visible()[an.sel];
  if (!r || (r.kind !== "note" && r.kind !== "review")) return;
  let id, prompt, done;
  if (r.kind === "review") {
    id = r.review.id;
    prompt = "Delete this review?\n◆ " + (r.review.agent || "agent") + " · " + r.review.summary;
    done = "deleted the review";
  } else {
    id = r.note.id;
    const n = (r.note.replies || []).length;
    prompt = (n ? "Delete this note and its " + n + " replies?" : "Delete this note?") +
      "\n◆ " + (r.note.author || r.note.source) + ": " + r.note.summary;
    done = "deleted the note";
  }
  showLocalConfirm(prompt, ["cancel", "delete"], async (opt) => {
    if (opt !== "delete") return;
    try {
      await postJSON("/api/notes/remove", { id });
    } catch (e) {
      opLine("note: " + (e.message || e), true);
      return;
    }
    opLine(done);
    refreshNoteCounts();
    if (an && !an.hidden) load();
  });
}

function anKey(e) {
  if (e.key === "Escape") {
    closeAllNotes();
    return true;
  }
  if (e.key === "ArrowDown" || (e.key === "n" && e.ctrlKey)) {
    e.preventDefault();
    move(1);
    return true;
  }
  if (e.key === "ArrowUp" || (e.key === "p" && e.ctrlKey)) {
    e.preventDefault();
    move(-1);
    return true;
  }
  if (e.key === "PageDown" || e.key === "PageUp") {
    e.preventDefault();
    move(e.key === "PageDown" ? 10 : -10);
    return true;
  }
  if (e.key === "Home" || e.key === "End") {
    e.preventDefault();
    move(e.key === "Home" ? -an.sel : visible().length);
    return true;
  }
  if (e.key === "ArrowLeft" || e.key === "ArrowRight") {
    // An empty query has no caret to move: the arrows fold, as in the TUI.
    if (an.query) return false;
    e.preventDefault();
    setFold(e.key === "ArrowLeft");
    return true;
  }
  if (e.key === "Enter") {
    e.preventDefault();
    activate(an.sel);
    return true;
  }
  if (e.key === "d" && e.ctrlKey) {
    e.preventDefault();
    deleteSelected();
    return true;
  }
  return false; // everything else is typing
}

$("allnotes-input").addEventListener("input", () => {
  if (!an) return;
  an.query = $("allnotes-input").value;
  an.sel = 0;
  an.notice = "";
  render();
});

$("allnotes-list").addEventListener("click", (e) => {
  const li = e.target.closest("li[data-i]");
  if (li) activate(Number(li.dataset.i));
});

box.addEventListener("click", (e) => {
  if (e.target === box) closeAllNotes(); // a click on the dim closes it
});

registerHelp({
  key: "all notes",
  html:
    "☰ / command palette → <b>view all notes…</b>: every note this checkout can see, as a tree — " +
    "working tree, commits (with their AI reviews), other (shelf entries) → directory → file → note, " +
    "each note with its status, author, place and age. Type to filter, ←/→ fold, enter or a click opens " +
    "a note's diff on it, or a review in the review view (esc comes back), <b>ctrl+d</b> deletes the thread or review under the cursor after asking",
});

export { openAllNotes };

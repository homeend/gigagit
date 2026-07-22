const ROW_H = 22;

const state = {
  rows: [],
  canLoadMore: false,
  loadingMore: false,
  cursor: 0,
  files: [],
  fileCursor: 0,
  fileSha: null,
  pane: "commits", // commits | files
  layout: "list", // list (commits full-width) | detail (files+diff, list hidden)
  // svg is the default: browser rows (22px) are taller than the 13px font
  // box, so text-mode │ glyphs leave vertical gaps and lanes look broken —
  // SVG strokes span the full row height. g toggles back to text.
  graphMode: "svg", // svg | text
  wt: null, // /api/status payload while the tree is dirty, else null
  filesMode: "commit", // commit | status
  statusEntries: [],
};

const $ = (id) => document.getElementById(id);

async function getJSON(url) {
  const resp = await fetch(url);
  const body = await resp.json();
  if (!resp.ok) throw new Error(body.error || resp.statusText);
  return body;
}

async function postJSON(url, body) {
  const resp = await fetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  const data = await resp.json();
  if (!resp.ok) throw new Error(data.error || resp.statusText);
  return data;
}

function esc(s) {
  return String(s).replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));
}

function runes(s) {
  return Array.from(s);
}

// --- working-tree row + status state ---

function wtCount() {
  return state.wt ? 1 : 0;
}

function applyStatus(st) {
  state.wt = st.files && st.files.length ? st : null;
  buildStatusEntries();
}

async function fetchStatus() {
  applyStatus(await getJSON("/api/status"));
}

// A partially-staged file appears twice: once under Staged (unstage
// control), once under Changes (stage control) — the git-status model.
function buildStatusEntries() {
  const es = [];
  for (const f of state.wt ? state.wt.files : []) {
    if (f.kind === "conflicted") es.push({ ...f, section: "conflicts" });
    else if (f.kind === "untracked") es.push({ ...f, section: "untracked" });
    else {
      if (f.staged !== ".") es.push({ ...f, section: "staged" });
      if (f.unstaged !== ".") es.push({ ...f, section: "changes" });
    }
  }
  const order = { staged: 0, changes: 1, untracked: 2, conflicts: 3 };
  es.sort((a, b) => order[a.section] - order[b.section] || (a.path < b.path ? -1 : a.path > b.path ? 1 : 0));
  state.statusEntries = es;
}

function wtRowHTML(i) {
  const sel = i === state.cursor ? " sel" : "";
  const c = state.wt.counts;
  const parts = [];
  if (c.staged) parts.push(c.staged + " staged");
  if (c.unstaged) parts.push(c.unstaged + " changed");
  if (c.untracked) parts.push(c.untracked + " untracked");
  if (c.conflicted) parts.push(c.conflicted + " conflicted");
  return (
    `<div class="crow wt${sel}" data-i="${i}">` +
    `<span class="graph">●</span>` +
    `<span class="subj">Working tree</span>` +
    `<span class="meta">${esc(parts.join(" · "))}</span></div>`
  );
}

// --- commits pane (virtualized: only visible rows exist in the DOM) ---

function renderCommits() {
  const scroll = $("commits-scroll");
  const total = state.rows.length + wtCount();
  $("commits-spacer").style.height = total * ROW_H + "px";
  const first = Math.max(0, Math.floor(scroll.scrollTop / ROW_H) - 10);
  const last = Math.min(total, Math.ceil((scroll.scrollTop + scroll.clientHeight) / ROW_H) + 10);
  const win = $("commits-window");
  win.style.top = first * ROW_H + "px";
  let html = "";
  for (let i = first; i < last; i++) {
    html += state.wt && i === 0 ? wtRowHTML(i) : rowHTML(state.rows[i - wtCount()], i);
  }
  win.innerHTML = html;
  maybeLoadMore(last - wtCount());
}

function rowHTML(row, i) {
  const sel = i === state.cursor ? " sel" : "";
  const refs = (row.refs || [])
    .map((r) => `<span class="ref ${r.kind}${r.head ? " head" : ""}">${esc(r.name)}</span>`)
    .join("");
  const when = new Date(row.time * 1000).toISOString().slice(0, 10);
  return (
    `<div class="crow${sel}" data-i="${i}">` +
    `<span class="graph">${graphHTML(row, i - wtCount())}</span>` +
    `<span class="subj">${refs}${esc(row.subject)}</span>` +
    `<span class="meta">${esc(row.author)} · ${row.short} · ${when}</span></div>`
  );
}

function graphHTML(row, feedIdx) {
  if (state.graphMode === "svg") return graphSVG(row, feedIdx);
  let html = "";
  let col = 0;
  for (const ch of row.cells || "") {
    html += `<span class="lane-${(col >> 1) % 8}">${esc(ch)}</span>`;
    col += 1;
  }
  return html;
}

function toggleGraphMode() {
  state.graphMode = state.graphMode === "text" ? "svg" : "text";
  renderCommits();
}

const CELL_W = 14;
const HALF = CELL_W / 2;
const MID = ROW_H / 2;

const laneColors = [];
function laneColor(i) {
  if (!laneColors.length) {
    const cs = getComputedStyle(document.documentElement);
    for (let k = 0; k < 8; k++) laneColors.push(cs.getPropertyValue(`--lane${k}`).trim());
  }
  return laneColors[i % 8];
}

// Each glyph maps to stroke path(s) inside its CELL_W x ROW_H box, keyed by
// which cell edges the glyph connects (top/bottom at center-x, left/right at
// center-y).
const GLYPH_PATHS = {
  "│": (x) => `M${x + HALF},0 V${ROW_H}`,
  "─": (x) => `M${x},${MID} H${x + CELL_W}`,
  "╭": (x) => `M${x + CELL_W},${MID} Q${x + HALF},${MID} ${x + HALF},${ROW_H}`,
  "╮": (x) => `M${x},${MID} Q${x + HALF},${MID} ${x + HALF},${ROW_H}`,
  "╰": (x) => `M${x + HALF},0 Q${x + HALF},${MID} ${x + CELL_W},${MID}`,
  "╯": (x) => `M${x + HALF},0 Q${x + HALF},${MID} ${x},${MID}`,
  "┬": (x) => `M${x},${MID} H${x + CELL_W} M${x + HALF},${MID} V${ROW_H}`,
  "┴": (x) => `M${x},${MID} H${x + CELL_W} M${x + HALF},0 V${MID}`,
  "┼": (x) => `M${x + HALF},0 V${ROW_H} M${x},${MID} H${x + CELL_W}`,
};

// Node-cell continuity: Lay emits a bare ● on a commit's own row — in a
// terminal the lane's continuation is implied by cell adjacency, but at
// 22px web rows the gap shows, so the node draws up/down stubs whenever the
// neighboring feed row's SAME column carries ink touching the shared edge.
const TOP_TOUCH = new Set(["│", "╰", "╯", "┴", "┼", "●"]); // ink touches its row's top edge
const BOT_TOUCH = new Set(["│", "╭", "╮", "┬", "┼", "●"]); // ink touches its row's bottom edge

function graphSVG(row, feedIdx) {
  const cells = runes(row.cells || "");
  const prev = runes((state.rows[feedIdx - 1] || {}).cells || "");
  const next = runes((state.rows[feedIdx + 1] || {}).cells || "");
  const w = cells.length * CELL_W;
  let parts = `<svg width="${w}" height="${ROW_H}" viewBox="0 0 ${w} ${ROW_H}">`;
  cells.forEach((ch, col) => {
    const x = col * CELL_W;
    const color = laneColor(col >> 1);
    if (ch === "●") {
      if (BOT_TOUCH.has(prev[col]))
        parts += `<path d="M${x + HALF},0 V${MID}" stroke="${color}" stroke-width="2" fill="none"/>`;
      if (TOP_TOUCH.has(next[col]))
        parts += `<path d="M${x + HALF},${MID} V${ROW_H}" stroke="${color}" stroke-width="2" fill="none"/>`;
      parts += `<circle cx="${x + HALF}" cy="${MID}" r="4" fill="${color}"/>`;
    } else if (GLYPH_PATHS[ch]) {
      parts += `<path d="${GLYPH_PATHS[ch](x)}" stroke="${color}" stroke-width="2" fill="none" stroke-linecap="round"/>`;
    } else if (ch !== " ") {
      parts += `<text x="${x}" y="${ROW_H - 6}" fill="${color}" font-size="12">${esc(ch)}</text>`;
    }
  });
  return parts + "</svg>";
}

async function loadCommits(more) {
  const body = await getJSON(more ? "/api/commits?more=1" : "/api/commits");
  state.rows = body.rows || [];
  state.canLoadMore = body.can_load_more;
  renderCommits();
}

function maybeLoadMore(lastVisible) {
  if (!state.canLoadMore || state.loadingMore) return;
  if (lastVisible < state.rows.length - 30) return;
  state.loadingMore = true;
  loadCommits(true).finally(() => {
    state.loadingMore = false;
  });
}

// --- files + diff panes ---

// Layout mirrors the TUI's drill-in: "list" = the commit list alone, full
// width; "detail" = the opened commit's files + diff, list hidden (esc
// returns). Never both crammed on one screen.
function setLayout(mode) {
  state.layout = mode;
  const p = $("panes");
  p.classList.toggle("solo", mode === "list");
  p.classList.toggle("detail", mode === "detail");
  if (mode === "list") moveCursor(0); // re-render + rescroll: display:none dropped the scroll position
}

async function openCommit(i) {
  if (state.wt && i === 0) return openWorkingTree(i);
  state.cursor = i;
  renderCommits();
  const row = state.rows[i - wtCount()];
  const body = await getJSON("/api/commit/" + row.hash);
  state.files = body.files || [];
  state.fileCursor = 0;
  state.fileSha = row.hash;
  state.pane = "files";
  state.filesMode = "commit";
  setLayout("detail");
  $("files-header").textContent = row.short + " " + row.subject;
  renderFiles();
  focusPane();
  if (state.files.length) openFile(0);
}

async function openWorkingTree(i) {
  state.cursor = i;
  renderCommits();
  await fetchStatus(); // refresh on open — external changes since boot
  if (!state.wt) {
    renderCommits();
    return;
  }
  state.filesMode = "status";
  state.fileCursor = 0;
  state.pane = "files";
  setLayout("detail");
  $("files-header").textContent = "Working tree";
  renderFiles();
  focusPane();
  if (state.statusEntries.length) openFile(0);
}

const SECTION_LABELS = { staged: "Staged", changes: "Changes", untracked: "Untracked", conflicts: "Conflicts" };

function renderFiles() {
  if (state.filesMode !== "status") {
    $("files-actions").classList.add("hidden");
    $("files-list").innerHTML = state.files
      .map(
        (f, i) =>
          `<li class="${i === state.fileCursor ? "sel" : ""}" data-i="${i}">` +
          `<span class="st ${esc(f.status)}">${esc(f.status)}</span>${esc(f.path)}</li>`
      )
      .join("");
    return;
  }
  $("files-actions").classList.remove("hidden");
  let html = "";
  let lastSection = "";
  state.statusEntries.forEach((f, i) => {
    if (f.section !== lastSection) {
      html += `<li class="sect">${SECTION_LABELS[f.section]}</li>`;
      lastSection = f.section;
    }
    const badge = f.section === "staged" ? f.staged : f.unstaged;
    const btn =
      f.section === "conflicts"
        ? ""
        : f.section === "staged"
          ? `<button class="act" data-i="${i}" data-un="1">u</button>`
          : `<button class="act" data-i="${i}">s</button>`;
    html +=
      `<li class="${i === state.fileCursor ? "sel" : ""} ${f.section}" data-i="${i}">` +
      `<span class="st">${esc(badge)}</span>${esc(f.path)}${btn}</li>`;
  });
  $("files-list").innerHTML = html;
}

async function openFile(i) {
  state.fileCursor = i;
  renderFiles();
  if (state.filesMode === "status") return openStatusDiff(i);
  const f = state.files[i];
  const q = new URLSearchParams({ sha: state.fileSha, path: f.path, status: f.status });
  if (f.old_path) q.set("old", f.old_path);
  $("diff-header").textContent = f.path;
  $("diff-body").innerHTML = `<div class="notice">loading…</div>`;
  try {
    renderDiff(await getJSON("/api/diff?" + q));
  } catch (e) {
    $("diff-body").innerHTML = `<div class="notice">error: ${esc(e.message || e)}</div>`;
  }
}

async function openStatusDiff(i) {
  const f = state.statusEntries[i];
  $("diff-header").textContent = f.path;
  if (f.section === "conflicts") {
    $("diff-body").innerHTML = `<div class="notice">conflicted — resolve in the TUI</div>`;
    return;
  }
  const q = new URLSearchParams({ wt: f.section === "staged" ? "staged" : "unstaged", path: f.path });
  if (f.orig_path) q.set("old", f.orig_path);
  $("diff-body").innerHTML = `<div class="notice">loading…</div>`;
  try {
    renderDiff(await getJSON("/api/diff?" + q));
  } catch (e) {
    $("diff-body").innerHTML = `<div class="notice">error: ${esc(e.message || e)}</div>`;
  }
}

async function stage(body) {
  try {
    applyStatus(await postJSON("/api/stage", body));
  } catch (e) {
    $("files-header").textContent = "error: " + (e.message || e);
    return;
  }
  if (!state.wt) {
    // tree went clean: leave status mode, drop the synthetic row, and give
    // the commit list its full width back (nothing is on display anymore)
    state.filesMode = "commit";
    state.pane = "commits";
    state.cursor = 0;
    $("files-list").innerHTML = "";
    $("files-actions").classList.add("hidden");
    $("files-header").textContent = "";
    $("diff-header").textContent = "";
    $("diff-body").innerHTML = "";
    state.lastDiff = null; // a resize must not resurrect the cleared diff
    setLayout("list");
    focusPane();
    return;
  }
  state.fileCursor = Math.min(state.fileCursor, state.statusEntries.length - 1);
  renderFiles();
  renderCommits(); // badge counts changed
}

function markSpans(text, spans, side) {
  if (!spans || !spans.length) return esc(text);
  const rs = runes(text);
  let out = "";
  let pos = 0;
  for (const [a, b] of spans) {
    out += esc(rs.slice(pos, a).join(""));
    out += `<mark class="${side}">` + esc(rs.slice(a, b).join("")) + "</mark>";
    pos = b;
  }
  return out + esc(rs.slice(pos).join(""));
}

function renderDiff(d) {
  state.lastDiff = d; // re-rendered on window resize (layout is width-dependent)
  if (d.binary) {
    $("diff-body").innerHTML = `<div class="notice">binary file</div>`;
    return;
  }
  if (d.too_large) {
    $("diff-body").innerHTML = `<div class="notice">diff too large</div>`;
    return;
  }
  const rows = d.rows || [];
  // An all-new or all-deleted file renders single-column: a side-by-side
  // with one permanently empty side wastes half the pane and forces harsh
  // wrapping on the populated half.
  const pureAdd = rows.length > 0 && rows.every((r) => !r.left_no);
  const pureDel = rows.length > 0 && rows.every((r) => !r.right_no);
  let html = `<table class="diff">`;
  if (pureAdd || pureDel) {
    const side = pureAdd ? "r" : "l";
    for (const r of rows) {
      const no = pureAdd ? r.right_no : r.left_no;
      const text = pureAdd ? r.right : r.left;
      const spans = pureAdd ? r.right_spans : r.left_spans;
      html +=
        `<tr class="${r.kind}">` +
        `<td class="no ${side}">${no || ""}</td>` +
        `<td class="side ${side}">${markSpans(text, spans, side)}</td></tr>`;
    }
  } else if ($("diff-pane").clientWidth < 950) {
    // Unified: below ~950px each side-by-side half is too narrow to read
    // (heavy wrapping, context text duplicated on both sides). One
    // full-width column; a changed pair becomes a del row then an add row,
    // keeping the intraline marks of each side.
    for (const r of rows) {
      if (r.kind === "same") {
        html +=
          `<tr class="same"><td class="no l">${r.left_no || ""}</td>` +
          `<td class="no r">${r.right_no || ""}</td>` +
          `<td class="side">${esc(r.right)}</td></tr>`;
      } else {
        if (r.kind !== "add")
          html +=
            `<tr class="del"><td class="no l">${r.left_no || ""}</td><td class="no r"></td>` +
            `<td class="side l">${markSpans(r.left, r.left_spans, "l")}</td></tr>`;
        if (r.kind !== "del")
          html +=
            `<tr class="add"><td class="no l"></td><td class="no r">${r.right_no || ""}</td>` +
            `<td class="side r">${markSpans(r.right, r.right_spans, "r")}</td></tr>`;
      }
    }
  } else {
    for (const r of rows) {
      html +=
        `<tr class="${r.kind}">` +
        `<td class="no l">${r.left_no || ""}</td>` +
        `<td class="side l">${markSpans(r.left, r.left_spans, "l")}</td>` +
        `<td class="no r">${r.right_no || ""}</td>` +
        `<td class="side r">${markSpans(r.right, r.right_spans, "r")}</td></tr>`;
    }
  }
  html += "</table>";
  if (d.truncated) html += `<div class="notice">alignment truncated (size guard)</div>`;
  $("diff-body").innerHTML = html;
}

// --- focus + keyboard ---

function focusPane() {
  document.querySelectorAll(".pane").forEach((p) => p.classList.remove("focused"));
  $(state.pane === "commits" ? "commits-pane" : "files-pane").classList.add("focused");
}

function moveCursor(delta) {
  if (state.pane === "commits") {
    const total = state.rows.length + wtCount();
    if (!total) return;
    state.cursor = Math.max(0, Math.min(total - 1, state.cursor + delta));
    const scroll = $("commits-scroll");
    const top = state.cursor * ROW_H;
    if (top < scroll.scrollTop) scroll.scrollTop = top;
    else if (top + ROW_H > scroll.scrollTop + scroll.clientHeight)
      scroll.scrollTop = top + ROW_H - scroll.clientHeight;
    renderCommits();
  } else {
    const list = state.filesMode === "status" ? state.statusEntries : state.files;
    if (!list.length) return;
    state.fileCursor = Math.max(0, Math.min(list.length - 1, state.fileCursor + delta));
    renderFiles();
  }
}

document.addEventListener("keydown", (e) => {
  if (e.key === "j" || e.key === "ArrowDown") {
    e.preventDefault();
    moveCursor(1);
  } else if (e.key === "k" || e.key === "ArrowUp") {
    e.preventDefault();
    moveCursor(-1);
  } else if (e.key === "Enter") {
    if (state.pane === "commits") openCommit(state.cursor);
    else if (state.filesMode === "status" ? state.statusEntries.length : state.files.length) openFile(state.fileCursor);
  } else if (e.key === "Escape") {
    // drill back out: detail (files+diff) → the full-width commit list
    if (state.layout === "detail") {
      state.pane = "commits";
      setLayout("list");
      focusPane();
    }
  } else if (e.key === "g") {
    toggleGraphMode();
  } else if ((e.key === "s" || e.key === "u") && state.pane === "files" && state.filesMode === "status") {
    const f = state.statusEntries[state.fileCursor];
    if (f && f.section !== "conflicts") {
      if (e.key === "s" && f.section !== "staged") stage({ paths: [f.path] });
      else if (e.key === "u" && f.section === "staged") stage({ paths: [f.path], unstage: true });
    }
  }
});

$("commits-scroll").addEventListener("scroll", renderCommits);
$("commits-window").addEventListener("click", (e) => {
  const row = e.target.closest(".crow");
  if (row) openCommit(Number(row.dataset.i));
});
$("files-list").addEventListener("click", (e) => {
  const btn = e.target.closest("button.act");
  if (btn && state.filesMode === "status") {
    const f = state.statusEntries[Number(btn.dataset.i)];
    stage(btn.dataset.un ? { paths: [f.path], unstage: true } : { paths: [f.path] });
    return;
  }
  const li = e.target.closest("li");
  if (li && li.dataset.i !== undefined) {
    state.pane = "files";
    focusPane();
    openFile(Number(li.dataset.i));
  }
});
$("stage-all").addEventListener("click", () => stage({ all: true }));
$("unstage-all").addEventListener("click", () => {
  const paths = state.statusEntries.filter((f) => f.section === "staged").map((f) => f.path);
  if (paths.length) stage({ paths, unstage: true }); // engine.Stage{All} can't unstage
});
window.addEventListener("resize", () => {
  renderCommits();
  if (state.lastDiff) renderDiff(state.lastDiff); // unified↔side-by-side is width-dependent
});

async function boot() {
  const repo = await getJSON("/api/repo");
  $("repo-name").textContent = repo.name;
  $("repo-branch").textContent = repo.branch;
  $("repo-worktree").textContent = repo.worktree;
  document.title = "gg web — " + repo.name;
  await fetchStatus().catch(() => {}); // status failing must not block browse
  await loadCommits(false);
  focusPane();
}
boot().catch((e) => {
  $("repo-name").textContent = "error: " + (e.message || e);
});

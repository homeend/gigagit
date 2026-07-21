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
  graphMode: "text", // text | svg (svg wired in the graph-upgrade task)
};

const $ = (id) => document.getElementById(id);

async function getJSON(url) {
  const resp = await fetch(url);
  const body = await resp.json();
  if (!resp.ok) throw new Error(body.error || resp.statusText);
  return body;
}

function esc(s) {
  return String(s).replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));
}

function runes(s) {
  return Array.from(s);
}

// --- commits pane (virtualized: only visible rows exist in the DOM) ---

function renderCommits() {
  const scroll = $("commits-scroll");
  $("commits-spacer").style.height = state.rows.length * ROW_H + "px";
  const first = Math.max(0, Math.floor(scroll.scrollTop / ROW_H) - 10);
  const last = Math.min(state.rows.length, Math.ceil((scroll.scrollTop + scroll.clientHeight) / ROW_H) + 10);
  const win = $("commits-window");
  win.style.top = first * ROW_H + "px";
  let html = "";
  for (let i = first; i < last; i++) html += rowHTML(state.rows[i], i);
  win.innerHTML = html;
  maybeLoadMore(last);
}

function rowHTML(row, i) {
  const sel = i === state.cursor ? " sel" : "";
  const refs = (row.refs || [])
    .map((r) => `<span class="ref ${r.kind}${r.head ? " head" : ""}">${esc(r.name)}</span>`)
    .join("");
  const when = new Date(row.time * 1000).toISOString().slice(0, 10);
  return (
    `<div class="crow${sel}" data-i="${i}">` +
    `<span class="graph">${graphHTML(row)}</span>` +
    `<span class="subj">${refs}${esc(row.subject)}</span>` +
    `<span class="meta">${esc(row.author)} · ${row.short} · ${when}</span></div>`
  );
}

// text mode: color each glyph by its lane (two display columns per lane).
function graphHTML(row) {
  let html = "";
  let col = 0;
  for (const ch of row.cells || "") {
    html += `<span class="lane-${(col >> 1) % 8}">${esc(ch)}</span>`;
    col += 1;
  }
  return html;
}

function toggleGraphMode() {} // wired in the graph-upgrade task

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

async function openCommit(i) {
  state.cursor = i;
  renderCommits();
  const row = state.rows[i];
  const body = await getJSON("/api/commit/" + row.hash);
  state.files = body.files || [];
  state.fileCursor = 0;
  state.fileSha = row.hash;
  state.pane = "files";
  $("files-header").textContent = row.short + " " + row.subject;
  renderFiles();
  focusPane();
  if (state.files.length) openFile(0);
}

function renderFiles() {
  $("files-list").innerHTML = state.files
    .map(
      (f, i) =>
        `<li class="${i === state.fileCursor ? "sel" : ""}" data-i="${i}">` +
        `<span class="st ${esc(f.status)}">${esc(f.status)}</span>${esc(f.path)}</li>`
    )
    .join("");
}

async function openFile(i) {
  state.fileCursor = i;
  renderFiles();
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
  if (d.binary) {
    $("diff-body").innerHTML = `<div class="notice">binary file</div>`;
    return;
  }
  if (d.too_large) {
    $("diff-body").innerHTML = `<div class="notice">diff too large</div>`;
    return;
  }
  let html = `<table class="diff">`;
  for (const r of d.rows || []) {
    html +=
      `<tr class="${r.kind}">` +
      `<td class="no l">${r.left_no || ""}</td>` +
      `<td class="side l">${markSpans(r.left, r.left_spans, "l")}</td>` +
      `<td class="no r">${r.right_no || ""}</td>` +
      `<td class="side r">${markSpans(r.right, r.right_spans, "r")}</td></tr>`;
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
    if (!state.rows.length) return;
    state.cursor = Math.max(0, Math.min(state.rows.length - 1, state.cursor + delta));
    const scroll = $("commits-scroll");
    const top = state.cursor * ROW_H;
    if (top < scroll.scrollTop) scroll.scrollTop = top;
    else if (top + ROW_H > scroll.scrollTop + scroll.clientHeight)
      scroll.scrollTop = top + ROW_H - scroll.clientHeight;
    renderCommits();
  } else {
    if (!state.files.length) return;
    state.fileCursor = Math.max(0, Math.min(state.files.length - 1, state.fileCursor + delta));
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
    else if (state.files.length) openFile(state.fileCursor);
  } else if (e.key === "Tab") {
    e.preventDefault();
    state.pane = state.pane === "commits" ? "files" : "commits";
    focusPane();
  } else if (e.key === "Escape") {
    state.pane = "commits";
    focusPane();
  } else if (e.key === "g") {
    toggleGraphMode();
  }
});

$("commits-scroll").addEventListener("scroll", renderCommits);
$("commits-window").addEventListener("click", (e) => {
  const row = e.target.closest(".crow");
  if (row) openCommit(Number(row.dataset.i));
});
$("files-list").addEventListener("click", (e) => {
  const li = e.target.closest("li");
  if (li) {
    state.pane = "files";
    focusPane();
    openFile(Number(li.dataset.i));
  }
});
window.addEventListener("resize", renderCommits);

async function boot() {
  const repo = await getJSON("/api/repo");
  $("repo-name").textContent = repo.name;
  $("repo-branch").textContent = repo.branch;
  $("repo-worktree").textContent = repo.worktree;
  document.title = "gg web — " + repo.name;
  await loadCommits(false);
  focusPane();
}
boot().catch((e) => {
  $("repo-name").textContent = "error: " + (e.message || e);
});

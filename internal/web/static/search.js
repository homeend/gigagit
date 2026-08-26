// search.js — finding things in a repo the browser cannot hold.
//
// Two surfaces live here, both talking to endpoints that answer in one page:
//
//   \  the FEED FILTER — path / author / message / since / until, applied by
//      git during the walk, so a narrowed list is drawn from the whole of
//      history instead of from the pages that happen to be loaded.
//   F  the FILE FINDER — a palette over the repo's tracked paths, ranked
//      server-side, opening a file's history on enter.
//
// The eager ctrl+f search and the commit marks live in commits.js, with the
// feed they page. This module imports from there; nothing there imports back.
import { $, esc, getJSON, state } from "./core.js";
import { closeLayer, mountOverlay, pushLayer, topLayer } from "./layers.js";
import { registerHelp } from "./menus.js";
import { opLine } from "./ops.js";
import { refilterFeed, searchDeeper } from "./commits.js";
import { openFileHistory } from "./filehist.js";

// This module builds its own DOM, so it brings its own rules. index.html's
// `hidden` class has NO global rule — every surface is hidden by its own
// `#id.hidden` selector — so an element created with class="hidden" and no
// matching rule is plainly visible. That has shipped as a bug here before.
const CSS = `
#ffilter { display: flex; gap: 6px; align-items: center; padding: 4px 8px; border-bottom: 1px solid var(--border); }
#ffilter.hidden { display: none; }
#ffilter input { flex: 1; min-width: 0; background: var(--bg-alt); color: var(--fg); border: 1px solid var(--border); border-radius: 4px; padding: 3px 6px; font: inherit; }
#ffilter input:focus { border-color: var(--accent); outline: none; }
#ffilter button { background: none; color: var(--dim); border: 1px solid var(--border); border-radius: 4px; padding: 2px 8px; font: inherit; cursor: pointer; white-space: nowrap; }
#ffilter button:hover { color: var(--fg); border-color: var(--accent); }
#ff-note { color: var(--dim); font-size: 11px; white-space: nowrap; }
#finder { position: fixed; inset: 0; background: rgba(0,0,0,0.45); display: flex; align-items: flex-start; justify-content: center; z-index: 60; }
#finder.hidden { display: none; }
#finder-box { margin-top: 8vh; width: min(760px, 92vw); background: var(--bg-alt); border: 1px solid var(--border); border-radius: 6px; box-shadow: 0 8px 30px rgba(0,0,0,0.5); overflow: hidden; }
#finder-input { width: 100%; box-sizing: border-box; background: var(--bg); color: var(--fg); border: none; border-bottom: 1px solid var(--border); padding: 8px 10px; font: inherit; }
#finder-input:focus { outline: none; }
#finder-list { list-style: none; margin: 0; padding: 0; max-height: 50vh; overflow-y: auto; }
#finder-list li { padding: 3px 10px; cursor: pointer; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
#finder-list li.sel { background: var(--sel); }
#finder-list li.empty { color: var(--dim); cursor: default; }
#finder-note { color: var(--dim); font-size: 11px; padding: 4px 10px; border-top: 1px solid var(--border); }
`;

const styleEl = document.createElement("style");
styleEl.textContent = CSS;
document.head.append(styleEl);


// --- the feed filter --------------------------------------------------------

// The five fields git's own log takes, in the order the TUI's `\` popup lists
// them. `id` is the input's element id, `key` the /api/commits query parameter.
const FIELDS = [
  { key: "path", label: "path", placeholder: "path…" },
  { key: "author", label: "author", placeholder: "author…" },
  { key: "grep", label: "message", placeholder: "message…" },
  { key: "since", label: "since", placeholder: "since… (2 weeks ago)" },
  { key: "until", label: "until", placeholder: "until… (2026-01-01)" },
];

// The bar is built once, into the commits pane above the list. It is not a
// layer: it stays visible while you navigate the results, which is the whole
// point of a filter you can see.
const bar = document.createElement("div");
bar.id = "ffilter";
bar.className = "hidden";
bar.innerHTML =
  FIELDS.map(
    (f) =>
      `<input id="ff-${f.key}" type="text" autocomplete="off" spellcheck="false" ` +
      `placeholder="${esc(f.placeholder)}" title="${esc(f.label)}">`
  ).join("") +
  `<span id="ff-note"></span><button id="ff-clear" title="clear the filter and show every commit">✕ clear</button>`;
$("commits-scroll").before(bar);

// state.feedFilter is the shape commits.js turns into query parameters. It
// lives on the shared state rather than here so the feed module can read it
// without importing this one (the import runs the other way).
state.feedFilter = {};


function filterValues() {
  const f = {};
  for (const { key } of FIELDS) {
    // Control characters are stripped rather than sent: the server refuses
    // them (they would collide two filters onto one cache key), and a pasted
    // stray byte must not leave the feed answering 400 to every later request.
    const v = $("ff-" + key).value.replace(/[\x00-\x1f\x7f]/g, "").trim();
    if (v) f[key] = v;
  }
  return f;
}


function filterActive() {
  return Object.keys(state.feedFilter || {}).length > 0;
}


// Typing must not re-walk history on every keystroke: the request is debounced,
// and commits.js drops any page that arrives after a newer one was asked for.
let applyTimer = null;

function scheduleApply() {
  clearTimeout(applyTimer);
  applyTimer = setTimeout(applyFilter, 300);
}


async function applyFilter() {
  clearTimeout(applyTimer);
  const next = filterValues();
  state.feedFilter = next;
  $("ff-note").textContent = "…";
  try {
    await refilterFeed();
  } catch (e) {
    $("ff-note").textContent = "";
    opLine("filter failed: " + (e.message || e), true);
    return;
  }
  $("ff-note").textContent = filterActive()
    ? state.rows.length + (state.canLoadMore ? "+" : "") + " match" + (state.rows.length === 1 ? "" : "es")
    : "";
}


function openFeedFilter() {
  if (state.layout === "diff" || state.layout === "detail") {
    opLine("the filter narrows the commit list — press esc to it first", false);
    return;
  }
  bar.classList.remove("hidden");
  $("ff-path").focus();
  $("ff-path").select();
}


// clearFeedFilter is the one control the task asks for: it empties every field,
// drops the scope, and closes the bar. Clearing is cheap — the feed remembers
// the accumulation it walked before the filter, so the unfiltered list comes
// back without a git call.
async function clearFeedFilter(keepOpen) {
  for (const { key } of FIELDS) $("ff-" + key).value = "";
  const had = filterActive();
  state.feedFilter = {};
  $("ff-note").textContent = "";
  if (!keepOpen) bar.classList.add("hidden");
  if (had) await refilterFeed();
}


bar.addEventListener("input", scheduleApply);

// The bar's fields own the keyboard while they are focused (the global router
// steps aside for any input), so every key that means something here has to be
// answered here — including ctrl+f, which otherwise reaches the BROWSER and
// opens its find dialog over a page whose own search is the thing you were
// reaching for.
bar.addEventListener("keydown", (e) => {
  if (e.key === "Enter") {
    e.preventDefault();
    applyFilter(); // commit now rather than waiting out the debounce
  } else if (e.key === "Escape") {
    e.preventDefault();
    clearFeedFilter(false);
  } else if (e.key === "f" && (e.ctrlKey || e.metaKey)) {
    e.preventDefault();
    searchDeeper(); // same key, same meaning as everywhere else in the page
  }
});

$("ff-clear").addEventListener("click", () => clearFeedFilter(true));


// --- the fuzzy file finder --------------------------------------------------

const finder = mountOverlay("finder");
finder.innerHTML =
  `<div id="finder-box">` +
  `<input id="finder-input" type="text" autocomplete="off" spellcheck="false" placeholder="find a tracked file…">` +
  `<ul id="finder-list"></ul>` +
  `<div id="finder-note"></div></div>`;

// find holds the open finder: its rows, the cursor, and the generation of the
// last request. Ranking happens on the server, so every keystroke is a request
// — and a slow one for an early prefix must never overwrite a later answer.
let find = null;

let findGen = 0;

let findTimer = null;


function openFinder() {
  find = { rows: [], sel: 0 };
  $("finder-input").value = "";
  $("finder-note").textContent = "";
  $("finder-list").innerHTML = `<li class="empty">loading…</li>`;
  pushLayer("finder", finder, { onKey: finderKey });
  $("finder-input").focus();
  rankFiles("");
}


function closeFinder() {
  find = null;
  clearTimeout(findTimer);
  $("finder-input").blur(); // a focused input would swallow every global key
  closeLayer("finder");
}


async function rankFiles(q) {
  const gen = ++findGen;
  let body;
  try {
    body = await getJSON("/api/files?q=" + encodeURIComponent(q));
  } catch (e) {
    if (find && gen === findGen) $("finder-list").innerHTML = `<li class="empty">error: ${esc(e.message || e)}</li>`;
    return;
  }
  if (!find || gen !== findGen) return; // closed, or a later query already answered
  find.rows = body.files || [];
  find.sel = 0;
  renderFinder();
  $("finder-note").textContent = body.limited
    ? "showing the best " + find.rows.length + " of " + body.total + " tracked files — keep typing"
    : find.rows.length + " of " + body.total + " tracked files";
}


function renderFinder() {
  if (!find.rows.length) {
    $("finder-list").innerHTML = `<li class="empty">(no tracked file matches)</li>`;
    return;
  }
  $("finder-list").innerHTML = find.rows
    .map((p, i) => `<li data-i="${i}"${i === find.sel ? ' class="sel"' : ""}>${esc(p)}</li>`)
    .join("");
  const sel = $("finder-list").querySelector("li.sel");
  if (sel) sel.scrollIntoView({ block: "nearest" });
}


function moveFinder(d) {
  if (!find.rows.length) return;
  find.sel = Math.max(0, Math.min(find.rows.length - 1, find.sel + d));
  renderFinder();
}


// openFinderRow opens the picked file's history — the finder's whole purpose
// (the TUI's F). The layer closes FIRST: the history overlay pushes a layer of
// its own, and esc must land back in the list, not on a finder underneath it.
function openFinderRow(i) {
  const path = find.rows[i];
  if (!path) return;
  closeFinder();
  openFileHistory(path, "");
}


function finderKey(e) {
  if (e.key === "Escape") {
    closeFinder();
    return true;
  }
  if (e.key === "ArrowDown" || (e.key === "n" && e.ctrlKey)) {
    e.preventDefault();
    moveFinder(1);
    return true;
  }
  if (e.key === "ArrowUp" || (e.key === "p" && e.ctrlKey)) {
    e.preventDefault();
    moveFinder(-1);
    return true;
  }
  if (e.key === "Enter") {
    e.preventDefault();
    openFinderRow(find.sel);
    return true;
  }
  return false; // everything else is typing
}


$("finder-input").addEventListener("input", () => {
  clearTimeout(findTimer);
  const q = $("finder-input").value;
  findTimer = setTimeout(() => rankFiles(q), 120);
});

$("finder-list").addEventListener("click", (e) => {
  const li = e.target.closest("li[data-i]");
  if (li) openFinderRow(Number(li.dataset.i));
});

finder.addEventListener("click", (e) => {
  if (e.target === finder) closeFinder(); // a click on the dim closes it
});


// --- keys -------------------------------------------------------------------
// Registered here rather than in keys.js so this feature owns its own file.
// The rules the shared router applies are applied here too: an open layer owns
// the keyboard, and a focused field owns every key it can type.
document.addEventListener("keydown", (e) => {
  if (topLayer()) return;
  if (e.target.closest && e.target.closest("input,textarea")) return;
  if (e.key === "\\") {
    e.preventDefault();
    openFeedFilter();
  } else if (e.key === "F") {
    e.preventDefault();
    openFinder();
  } else if (e.key === "f" && (e.ctrlKey || e.metaKey)) {
    e.preventDefault(); // the browser's own find would take it
    searchDeeper();
  }
});


registerHelp({ key: "\\", html: "<b>filter the commit list</b> by path, author, message or date — applied by git over ALL history, not just the loaded pages" });
registerHelp({ key: "ctrl+f", html: "<b>search deeper</b>: page unloaded history for the next match of the / query; press again to dig past the hit" });
registerHelp({ key: "F", html: "<b>find a file</b> — fuzzy over every tracked path; enter opens its history" });
registerHelp({ key: "ctrl+click", html: "<b>mark a commit</b> — two marks compare, two or more squash (right-click menu)" });

export { clearFeedFilter, openFeedFilter, openFinder };

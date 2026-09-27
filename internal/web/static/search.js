// search.js — finding things in a repo the browser cannot hold.
//
// The FEED FILTER lives here (\) — path / author / message / since / until,
// applied by git during the walk, so a narrowed list is drawn from the whole
// of history instead of from the pages that happen to be loaded. (F, the
// working tree's files, is wtfinder.js.)
//
// The eager ctrl+f search and the commit marks live in commits.js, with the
// feed they page. This module imports from there; nothing there imports back.
import { $, esc, state } from "./core.js";
import { topLayer } from "./layers.js";
import { registerHelp } from "./menus.js";
import { opLine } from "./ops.js";
import { refilterFeed, searchDeeper } from "./commits.js";

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
  } else if (e.key === "f" && (e.ctrlKey || e.metaKey)) {
    e.preventDefault(); // the browser's own find would take it
    searchDeeper();
  }
});


registerHelp({ key: "\\", html: "<b>filter the commit list</b> by path, author, message or date — applied by git over ALL history, not just the loaded pages" });
registerHelp({ key: "ctrl+f", html: "<b>search deeper</b>: page unloaded history for the next match of the / query; press again to dig past the hit" });
registerHelp({ key: "ctrl+click", html: "<b>mark a commit</b> — two marks compare, two or more squash (right-click menu)" });

export { clearFeedFilter, openFeedFilter };

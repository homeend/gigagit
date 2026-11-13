// sidesearch.js — a search box under every sidebar section's header.
//
// Each section (pull requests keep their own forge search, prs.js) gets one
// text box between its header and its list: typing narrows the list to the
// rows whose NAME contains the text (case-insensitive). The match key is the
// section's own — a branch's name, a worktree's branch and path, a stash's
// "<ref>  <subject>" (the TUI stash window's / filter) — never the rendered
// row text, so ages, marks and counts never match; sub-rows (sessions,
// reviews, overviews) follow their parent row.
//
// Lists the browser holds in full (branches, worktrees, previews, stashes,
// bookmarks, shelf) filter at render time through sideMatch. Lists the server
// caps or pages (remotes, tags, reflog) send the text as ?q= so a row past the
// first page is still found; their modules re-fetch on change.
//
// The box follows its section: hidden while the section is folded or empty
// (and nothing is typed). Escape clears the text, a second Escape (or Enter)
// leaves the box; "/" on a focused sidebar row puts the caret in its
// section's box (panefocus.js).
//
// The query lives in state.sideSearch (a module-level let would be
// reassigned only here; other modules read it through sideQuery).
import { $, state } from "./core.js";

// The sections that carry a box, and the word each one's placeholder uses.
const LISTS = {
  branches: "branches",
  remotes: "remote branches",
  worktrees: "worktrees",
  previews: "previews",
  tags: "tags",
  stashes: "stashes",
  reflog: "the reflog",
  bookmarks: "bookmarks",
  shelf: "the shelf",
};

state.sideSearch = {};
const handlers = {};
const timers = {};

// sideQuery is section name's search text, trimmed and lower-cased ("" = none).
function sideQuery(name) {
  return String(state.sideSearch[name] || "").trim().toLowerCase();
}

// sideMatch reports whether any of keys contains name's search text (true
// when nothing is typed).
function sideMatch(name, ...keys) {
  const q = sideQuery(name);
  if (!q) return true;
  return keys.some((k) => String(k || "").toLowerCase().includes(q));
}

// noMatchRow is the row a searched list shows when nothing matched.
function noMatchRow(name, shown) {
  return sideQuery(name) && !shown ? `<li class="none">no match</li>` : "";
}

// onSideSearch registers what name's box runs when its text changes: a
// client-side list re-renders at once, a server-searched one waits delay ms
// of quiet typing before it asks.
function onSideSearch(name, fn, delay = 0) {
  handlers[name] = { fn, delay };
}

function fire(name, now) {
  const h = handlers[name];
  syncBox(name);
  if (!h) return;
  clearTimeout(timers[name]);
  if (now || !h.delay) h.fn();
  else timers[name] = setTimeout(h.fn, h.delay);
}

// setSideSearch replaces name's text (the box shows it) and re-runs the list.
function setSideSearch(name, text) {
  state.sideSearch[name] = text;
  const inp = $(name + "-search");
  if (inp && inp.value !== text) inp.value = text;
  fire(name, true);
}

// focusSideSearch unfolds name's section if needed and puts the caret in its
// box; false when the section has none.
function focusSideSearch(name) {
  const box = $(name + "-search-box");
  if (!box) return false;
  const list = $(name + "-list");
  if (list && list.classList.contains("collapsed")) $(name + "-header").click();
  box.classList.remove("empty");
  const inp = $(name + "-search");
  inp.focus();
  inp.select();
  return true;
}

// syncBox hides an empty section's box: no rows and nothing typed means
// nothing to search. A box with the caret in it stays.
function syncBox(name) {
  const box = $(name + "-search-box");
  const list = $(name + "-list");
  if (!box || !list) return;
  const inp = $(name + "-search");
  const empty = !sideQuery(name) && !list.querySelector("li") && document.activeElement !== inp;
  box.classList.toggle("empty", empty);
}

function mount(name, word) {
  const header = $(name + "-header");
  const list = $(name + "-list");
  if (!header || !list) return;
  const box = document.createElement("div");
  box.id = name + "-search-box";
  box.className = "side-search empty";
  const inp = document.createElement("input");
  inp.id = name + "-search";
  inp.type = "text";
  inp.spellcheck = false;
  inp.autocomplete = "off";
  inp.placeholder = "search " + word + "…";
  inp.title = "type to narrow the list · esc clears · / on a sidebar row lands here";
  box.append(inp);
  // Directly before the list: the CSS reads the list's fold through "+".
  list.before(box);
  inp.addEventListener("input", () => {
    state.sideSearch[name] = inp.value;
    fire(name, false);
  });
  inp.addEventListener("keydown", (e) => {
    if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      if (inp.value) setSideSearch(name, "");
      else inp.blur();
    } else if (e.key === "Enter") {
      e.preventDefault();
      e.stopPropagation();
      fire(name, true); // a server-searched list asks now, not after the pause
      inp.blur();
    }
  });
  inp.addEventListener("blur", () => syncBox(name));
  // Every re-render of the list re-decides whether the box is worth showing.
  new MutationObserver(() => syncBox(name)).observe(list, { childList: true });
}

// (Guarded: the page's pure helpers are also loaded under node by the tests.)
if (typeof document !== "undefined") for (const [name, word] of Object.entries(LISTS)) mount(name, word);

export { focusSideSearch, noMatchRow, onSideSearch, setSideSearch, sideMatch, sideQuery };

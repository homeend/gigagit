// searchhist.js — search recall (the TUI's alt+↑/↓ in a filter or a search
// field): the per-repo rings the server keeps (GET/POST /api/search-history,
// shared with the TUI). alt+↓ steps to an older phrase, alt+↑ to a newer one,
// and above the newest the field holds what was being typed again. A phrase
// is recorded when it is confirmed (enter, a deep search).
import { getJSON, postJSON } from "./core.js";
import { registerHelp } from "./menus.js";

let rings = null; // scope → phrases, newest first (read once, kept current)

async function loadRings() {
  if (!rings) {
    try {
      rings = (await getJSON("/api/search-history")).rings || {};
    } catch {
      rings = {};
    }
  }
  return rings;
}

// recordSearch keeps a confirmed phrase at the top of its scope's ring — in
// the cache too once it has loaded, so the next recall sees it at once.
function recordSearch(scope, phrase) {
  const p = (phrase || "").trim();
  if (!p) return;
  postJSON("/api/search-history", { scope, phrase: p }).catch(() => {});
  loadRings().then((r) => {
    r[scope] = [p, ...(r[scope] || []).filter((x) => x !== p)];
  });
}

// recallStep is the ring index after an alt+arrow from i (-1 = nothing
// recalled, the typed draft): alt+↓ opens at the newest and goes older,
// alt+↑ goes newer and past the newest back to the draft.
function recallStep(i, n, key) {
  if (key === "ArrowDown") return Math.min(n - 1, i + 1);
  return Math.max(-1, i - 1);
}

// recallKey is alt+↑ / alt+↓ in `input`: true when it took the key (the
// default is prevented at once, the ring may still be loading). set(value)
// puts the recalled phrase in effect (re-filter, re-search). Any edit since
// the last recall (typing, backspace, a field cleared in code) starts a new
// walk from the newest phrase.
function recallKey(e, input, scope, set) {
  if (!e.altKey || e.ctrlKey || e.metaKey || (e.key !== "ArrowUp" && e.key !== "ArrowDown")) return false;
  e.preventDefault();
  const before = input.value;
  loadRings().then((r) => {
    if (input.value !== before) return; // typed on while the rings loaded
    const ring = r[scope] || [];
    if (!ring.length) return;
    if (input._recall !== undefined && input.value !== input._recallSet) input._recall = undefined;
    const from = input._recall === undefined ? -1 : input._recall;
    if (from < 0) input._recallDraft = input.value; // what was being typed
    const i = recallStep(from, ring.length, e.key);
    if (i === from) return;
    input._recall = i;
    input._recallSet = i < 0 ? input._recallDraft || "" : ring[i];
    set(input._recallSet);
  });
  return true;
}

// recallReset forgets the walk (the next alt+↓ starts from the newest).
function recallReset(input) {
  input._recall = undefined;
}

// recallOpen is a field opening for typing: a fresh walk, and the rings
// fetched now so the first alt+↓ is not a round trip.
function recallOpen(input) {
  recallReset(input);
  loadRings();
}

registerHelp({
  key: "alt+↓ · alt+↑ · search history",
  html:
    "in the commit filter (<b>/</b>, <b>@</b>), an in-view search and the file finder: <b>alt+↓</b> puts the last " +
    "confirmed query back, again for older ones, <b>alt+↑</b> for newer ones and, past the newest, what you were " +
    "typing. A query is remembered when you press <b>enter</b> — per repository, shared with the TUI",
});

export { recallKey, recallOpen, recallStep, recordSearch };

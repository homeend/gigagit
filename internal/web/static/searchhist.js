// searchhist.js — search recall (the TUI's alt+↑/↓ in a filter or a search
// field): the per-repo rings the server keeps (GET/POST /api/search-history,
// shared with the TUI). alt+↑ steps to an older phrase, alt+↓ to a newer one,
// and past the newest the field holds what was being typed again. A phrase
// is recorded when it is confirmed (enter, a deep search).
import { getJSON, postJSON } from "./core.js";

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

// recordSearch keeps a confirmed phrase at the top of its scope's ring.
function recordSearch(scope, phrase) {
  const p = (phrase || "").trim();
  if (!p) return;
  postJSON("/api/search-history", { scope, phrase: p }).catch(() => {});
  if (rings) rings[scope] = [p, ...(rings[scope] || []).filter((x) => x !== p)];
}

// recallKey is alt+↑ / alt+↓ in `input`: true when it took the key (the
// default is prevented at once, the ring may still be loading). set(value)
// puts the recalled phrase in effect (re-filter, re-search).
function recallKey(e, input, scope, set) {
  if (!e.altKey || e.ctrlKey || e.metaKey || (e.key !== "ArrowUp" && e.key !== "ArrowDown")) return false;
  e.preventDefault();
  const older = e.key === "ArrowUp";
  loadRings().then((r) => {
    const ring = r[scope] || [];
    if (!ring.length) return;
    let i = input._recall === undefined ? -1 : input._recall;
    if (i < 0) input._recallDraft = input.value; // what was being typed
    i = older ? Math.min(ring.length - 1, i + 1) : Math.max(-1, i - 1);
    input._recall = i;
    set(i < 0 ? input._recallDraft || "" : ring[i]);
  });
  return true;
}

// recallReset forgets the walk once the user types (the next alt+↑ starts
// from the newest phrase again).
function recallReset(input) {
  input._recall = undefined;
}

export { recallKey, recallReset, recordSearch };

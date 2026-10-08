// prkept.js — the body a send's box keeps until that send reaches GitHub
// (pure: node-tested by prkeptjs_test.go). kept = {pr, group, text}; group
// "verdict" is Verdict…, "mine" my draft review, "review:<id>" an AI review.

// keptText is the kept text for PR pr's box group, or null.
export function keptText(kept, pr, group) {
  return kept && kept.pr === pr && kept.group === group ? kept.text : null;
}

// keptAfterSend is what a finished send of PR n (its request body, its done
// event) leaves kept: only a send that changed GitHub FROM THAT BOX clears
// it — a resolve, a reply or another PR's send leaves the typed text.
export function keptAfterSend(kept, n, body, ev) {
  if (!kept || !ev.ok || !ev.changed || kept.pr !== n || !body) return kept;
  const group = body.kind === "verdict" ? "verdict" : body.kind === "group" ? body.group : null;
  return group === kept.group ? null : kept;
}

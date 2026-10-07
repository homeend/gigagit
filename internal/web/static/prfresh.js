// prfresh.js — the open PR's freshness word (spec 2026-10-07 §2.4), the
// TUI's prSeen/prUpdated rules. Pure and import-free: prfreshjs_test.go runs
// it under node.
//
//   - "refreshing…" while the forge is asked;
//   - "updated" when a refresh found new comments or commits — never on a
//     PR's first read (that read fills the view) and never for the change
//     the user's own send made;
//   - "offline · read <age>" when the forge could not be reached;
//   - nothing once a refresh finds nothing new.
//
// st = {seen, updated, ownSend, text}; ev = {n, kind: start|ok|fail|sent,
// changed, age}.
export function nextFresh(st, ev) {
  const s = { seen: 0, updated: 0, ownSend: false, ...st };
  switch (ev.kind) {
    case "start":
      return { ...s, text: "refreshing…" };
    case "fail":
      return { ...s, text: ev.age ? "offline · read " + ev.age : "offline" };
    case "sent":
      return { ...s, ownSend: true, updated: 0, text: "" };
  }
  if (s.seen !== ev.n) return { seen: ev.n, updated: 0, ownSend: false, text: "" };
  if (ev.changed && !s.ownSend) return { ...s, updated: ev.n, text: "updated" };
  if (ev.changed) return { ...s, ownSend: false, updated: 0, text: "" };
  return { ...s, updated: 0, text: "" };
}

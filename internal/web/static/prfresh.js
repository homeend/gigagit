// prfresh.js — the open PR's freshness word (spec 2026-10-07 §2.4), the
// TUI's prSeen/prUpdated rules. Pure and import-free: prfreshjs_test.go runs
// it under node.
//
//   - "refreshing…" while the forge is asked;
//   - "updated" when a refresh found new comments or commits — never on a
//     PR's first read (that read fills the view) and never for the change
//     the user's own send made: the first read that STARTED after that send
//     (ev.seq > the seq "sent" carried) absorbs it, changed or not; a read
//     already in flight when the send ended leaves it armed;
//   - "offline · read <age>" when the forge could not be reached;
//   - nothing once a refresh finds nothing new.
//
// st = {seen, updated, ownSend (null, or the last read seq the send
// ended after), text}; ev = {n, kind: start|ok|fail|sent, changed, age, seq}.
export function nextFresh(st, ev) {
  const s = { seen: 0, updated: 0, ownSend: null, ...st };
  switch (ev.kind) {
    case "start":
      return { ...s, text: "refreshing…" };
    case "fail":
      return { ...s, text: ev.age ? "offline · read " + ev.age : "offline" };
    case "sent":
      return { ...s, ownSend: ev.seq === undefined ? 0 : ev.seq, updated: 0, text: "" };
  }
  if (s.seen !== ev.n) return { seen: ev.n, updated: 0, ownSend: null, text: "" };
  const after = s.ownSend !== null && s.ownSend !== false && (ev.seq === undefined || ev.seq > s.ownSend);
  if (after) return { ...s, ownSend: null, updated: 0, text: "" };
  if (ev.changed) return { ...s, updated: ev.n, text: "updated" };
  return { ...s, updated: 0, text: "" };
}

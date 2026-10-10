// prsendrows.js — which GitHub rows a note's right-click menu offers in a
// PR's own diff (the TUI's forgeNoteRows, row for row). Pure and
// import-free: prsendjs_test.go runs it under node.
//
//   - a local root (a note, an AI review's remark) not on GitHub and not in
//     flight: Send as GitHub comment (Retry … after a failure) — the group
//     sends live in the Send to GitHub… panel (prsendpanel.js, R9);
//   - a GitHub thread: Reply & send…, Resolve / Reopen on GitHub, and Send
//     draft reply / Send N draft replies when local drafts hang under it.
export function sendRows(n, pr) {
  if (!pr || !n) return [];
  const rows = [];
  if (n.source === "forge" && !n.parent_id) {
    rows.push({ id: "reply-send", label: "Reply & send…" });
    rows.push({ id: "resolve", label: n.resolved ? "Reopen on GitHub" : "Resolve on GitHub" });
    const drafts = (n.replies || []).filter((r) => r.source !== "forge").length;
    if (drafts) rows.push({ id: "send-drafts", label: drafts === 1 ? "Send draft reply" : "Send " + drafts + " draft replies" });
    return rows;
  }
  if (n.source === "forge" || n.parent_id || n.sync === "sending" || n.sync === "github") return rows;
  rows.push({ id: "send", label: n.sync === "failed" ? "Retry sending as GitHub comment" : "Send as GitHub comment" });
  return rows;
}

// --- the send panel (spec §5.4; the TUI's send_panel.go row for row) ---
// panelRows lays the panel's list: a header per group (its colour slot, the
// agent and date for a review, the all/none state of its tickable rows),
// then its rows — a skip row (the planner would refuse it) is listed with
// the reason and is not tickable. ticked is a Set of ids; an id the
// candidates no longer list is ignored (a stale tick after a refresh).
export function panelRows(cands, ticked) {
  const out = [];
  for (const g of cands.groups || []) {
    const rows = g.rows || [];
    const tickable = rows.filter((r) => !r.skip);
    const on = tickable.filter((r) => ticked.has(r.id)).length;
    out.push({ kind: "group", id: g.id, title: g.title || "", agent: g.agent || "", created: g.created || "", slot: g.slot || 0,
      n: rows.length, ticked: on > 0, all: tickable.length > 0 && on === tickable.length, gkind: g.kind });
    for (const r of rows) {
      out.push({ kind: "row", id: r.id, group: g.id, slot: g.slot || 0, tickable: !r.skip, ticked: !r.skip && ticked.has(r.id),
        severity: r.severity || "", where: whereOf(r), summary: r.summary || "", rationale: r.rationale || "",
        code: r.code || [], skip: r.skip || "", sync: r.sync || "" });
    }
  }
  return out;
}

function whereOf(r) {
  const [a, b] = r.range || [0, 0];
  return r.path + ":" + a + (b > a ? "-" + b : "");
}

// bodyOptions is the body <select>: no body, each TICKED AI review's text
// (newest first — the groups' order), then a typed body.
export function bodyOptions(cands, ticked) {
  const opts = [{ value: "none", label: "no body" }];
  for (const g of cands.groups || []) {
    if (g.kind !== "review" || !(g.rows || []).some((r) => !r.skip && ticked.has(r.id))) continue;
    opts.push({ value: g.id, label: "review text (" + (g.agent || "AI") + ")" });
  }
  opts.push({ value: "typed", label: "typed" });
  return opts;
}

// panelRequest is what Send posts: the ticked ids in list order, a
// verdict, and the body — a review's summary (body_from), typed text
// (body + body_set) or none. null with nothing ticked.
export function panelRequest(cands, ticked, body) {
  const ids = [];
  for (const g of cands.groups || []) for (const r of g.rows || []) if (!r.skip && ticked.has(r.id)) ids.push(r.id);
  if (!ids.length) return null;
  const req = { kind: "notes", ids, verdict: true };
  if (body && body.kind === "review" && body.from) req.body_from = body.from;
  else if (body && body.kind === "typed") Object.assign(req, { body: body.typed || "", body_set: true });
  return req;
}

// tickAllInGroup is the header's checkbox (the TUI's a): every tickable row
// of the group ticked — or, all of them ticked already, none.
export function tickAllInGroup(cands, ticked, groupId) {
  const g = (cands.groups || []).find((x) => x.id === groupId);
  const next = new Set(ticked);
  if (!g) return next;
  const tickable = (g.rows || []).filter((r) => !r.skip);
  const all = tickable.length > 0 && tickable.every((r) => next.has(r.id));
  for (const r of tickable) all ? next.delete(r.id) : next.add(r.id);
  return next;
}

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

// serialReads runs the open PR's forge reads one at a time through gate
// (prs.js: runOnce("pr-comments")). run(fn) starts fn or answers null when a
// read is running; soon(key, fn) starts it, or runs it once the running read
// ends — one waiting read per key, the newest wins. No give-up timer: a read
// always ends (the server bounds it).
export function serialReads(gate) {
  const waiting = new Map();
  const run = (fn) => {
    const p = gate(fn);
    if (p) p.then(next, next);
    return p;
  };
  function next() {
    const first = waiting.entries().next();
    if (first.done) return;
    const [key, fn] = first.value;
    waiting.delete(key);
    soon(key, fn);
  }
  function soon(key, fn) {
    if (run(fn) === null) waiting.set(key, fn);
  }
  return { run, soon };
}

// coveredReads is a single-flight read whose late callers are not dropped:
// a call while a read runs queues ONE more read, and its promise settles
// when that read ends — so the caller sees an answer newer than its call
// (prs.js fetchPRs: a cold page's link landing must see the listing).
export function coveredReads(gate, read) {
  let again = false;
  let tail = null;
  function call() {
    const run = gate(read);
    if (!run) {
      again = true;
      return tail || Promise.resolve();
    }
    tail = run.then(() => {
      if (!again) return;
      again = false;
      return call();
    });
    return tail;
  }
  return call;
}

// readyLatch: wait(ms) resolves true once open() ran (at once if it did),
// false after ms.
export function readyLatch() {
  let ready = false;
  const waiters = [];
  return {
    open() {
      if (ready) return;
      ready = true;
      waiters.splice(0).forEach((f) => f(true));
    },
    wait(ms) {
      if (ready) return Promise.resolve(true);
      return new Promise((res) => {
        waiters.push(res);
        setTimeout(() => res(false), ms);
      });
    },
  };
}

// exclusive runs one task at a time: try(fn) starts fn or answers null while
// one runs; idle() settles once none runs (prs.js: a PR link landing waits
// for a row's open in flight instead of failing in silence).
export function exclusive() {
  let running = null;
  return {
    try(fn) {
      if (running) return null;
      const p = Promise.resolve().then(fn);
      running = p.finally(() => (running = null));
      return p;
    },
    async idle() {
      while (running) await running.catch(() => {});
    },
  };
}

// stickyFlag is soon() for reads that carry a flag a newer waiting read must
// not drop (prs.js: the moved-head read's "updated"): soon(key, flag, make)
// queues make(flag) — the flag ORed over every read that replaced another
// while waiting, cleared when one starts.
export function stickyFlag(reads) {
  const raised = new Set();
  return (key, flag, make) => {
    if (flag) raised.add(key);
    reads.soon(key, () => make(raised.delete(key))());
  };
}

// oncePerKey wraps an async fn so one call per key runs at a time: a call
// for a key whose earlier call has not settled answers null (prs.js: the
// moved-head follow — two reads seeing the same move must fetch once).
export function oncePerKey(fn) {
  const pending = new Set();
  return (key, ...args) => {
    if (pending.has(key)) return null;
    pending.add(key);
    const done = () => pending.delete(key);
    const p = Promise.resolve().then(() => fn(key, ...args));
    p.then(done, done);
    return p;
  };
}

// sentEvent is the freshness event a finished send makes (F1): only a send
// that changed GitHub is my own change; seq = the last read started.
export function sentEvent(ev, seq) {
  return ev.ok && ev.changed ? { kind: "sent", seq } : null;
}

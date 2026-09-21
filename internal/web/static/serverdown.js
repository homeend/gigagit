// serverdown.js — the page notices that `gg web` is gone. A dead server
// leaves a page that LOOKS alive: every list is still painted, and each click
// fails on its own with an unrelated error. So the page watches for it, and
// says so with a veil over everything until the server answers again.
//
// Import-free on purpose: core.js and toast.js both call in here, and the
// pure section below runs under node (serverdownjs_test.go).

// --- server liveness (pure; guarded against Go) ---
// Three states. up → probing on a suspicion (a dropped event stream, a
// failed API call); probing → down on the SECOND failed probe, or back to up
// without a word on a success. One failure is never a verdict: the server
// ends every stream itself on a re-root, and a probe can race its restart.
// A "shutdown" message from the server is a verdict at once — but its first
// probe waits, since the exiting process still answers for a moment.
const LIVENESS_RETRY_MS = 1500; // between the two probes of a suspicion
const LIVENESS_POLL_MS = 2000; // while down: is it back yet?
const LIVENESS_HINT_MS = 10000; // down this long → the "new port?" hint

function createLivenessMonitor({ probe, setTimeout, clearTimeout, now, onChange }) {
  let state = "up";
  let fails = 0;
  let downSince = 0;
  let hinted = false;
  let timer = null;
  let gen = 0; // a probe answered after alive()/shutdown() is stale

  const later = (ms) => {
    if (timer !== null) clearTimeout(timer);
    timer = setTimeout(() => {
      timer = null;
      run();
    }, ms);
  };
  const goDown = () => {
    state = "down";
    downSince = now();
    hinted = false;
    onChange({ state: "down", hint: false });
  };
  const goUp = () => {
    const was = state;
    state = "up";
    fails = 0;
    gen++;
    if (timer !== null) clearTimeout(timer);
    timer = null;
    if (was === "down") onChange({ state: "up", hint: false });
  };
  function run() {
    const mine = ++gen;
    probe().then((ok) => {
      if (mine !== gen || state === "up") return;
      if (ok) return goUp();
      if (state === "probing") {
        if (++fails < 2) return later(LIVENESS_RETRY_MS);
        goDown();
      } else if (!hinted && now() - downSince >= LIVENESS_HINT_MS) {
        hinted = true;
        onChange({ state: "down", hint: true });
      }
      later(LIVENESS_POLL_MS);
    });
  }
  return {
    state: () => state,
    // suspect: something failed the way a dead server would make it fail.
    suspect() {
      if (state !== "up") return; // already on it — never restart the count
      state = "probing";
      fails = 0;
      run();
    },
    // shutdown: the server said goodbye on the event stream.
    shutdown() {
      if (state === "down") return;
      gen++;
      goDown();
      later(LIVENESS_POLL_MS);
    },
    // alive: the server was heard from (a hello on a reconnected stream).
    alive() {
      if (state !== "up") goUp();
    },
  };
}
// --- end server liveness ---

// Any answer from OUR endpoint is life; only a rejection (refused, reset,
// timed out) is not. /api/ping never touches the repository, so it cannot
// queue behind a long operation and time out against a healthy server.
function probePing() {
  return fetch("/api/ping", { cache: "no-store", signal: AbortSignal.timeout(2000) }).then(
    (r) => r.ok,
    () => false,
  );
}

const upHooks = [];
const monitor = createLivenessMonitor({
  probe: probePing,
  setTimeout: (fn, ms) => window.setTimeout(fn, ms),
  clearTimeout: (id) => window.clearTimeout(id),
  now: () => Date.now(),
  onChange: (s) => {
    const bar = document.getElementById("server-down");
    const hint = document.getElementById("server-down-hint");
    if (bar) bar.classList.toggle("hidden", s.state !== "down");
    if (hint) hint.classList.toggle("hidden", !s.hint);
    if (s.state === "up") for (const fn of upHooks) fn();
  },
});

function suspectServerDown() { monitor.suspect(); }
function serverShutdown() { monitor.shutdown(); }
function serverSeen() { monitor.alive(); }
function isServerDown() { return monitor.state() === "down"; }
// onServerUp: run fn each time the page comes back from down.
function onServerUp(fn) { upHooks.push(fn); }

// Keys must not drive a dead page. The window's capture phase runs before
// every document handler; the default is left alone so F5 / ctrl+R — the way
// out — still work. (core.js is also imported under node by the JS-port
// guards, where there is no window — hence the typeof.)
if (typeof window !== "undefined") {
  window.addEventListener("keydown", (e) => { if (isServerDown()) e.stopImmediatePropagation(); }, true);
}

export { suspectServerDown, serverShutdown, serverSeen, isServerDown, onServerUp };

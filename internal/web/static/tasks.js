// tasks.js — what the AI tasks tab and the Worktrees ◆ rows do with a task:
// show its result (the TUI's openTask: a review's note, the result text, a
// failure's output), cancel it, remove an ended record, retry a review save.
// The switcher (openfiles.js) and the sidebar both use it; it imports
// neither, so the three never form a cycle.
import { $, getJSON, postJSON } from "./core.js";
import { closeLayer, copyText, mountOverlay, pushLayer } from "./layers.js";
import { opLine, showLocalConfirm } from "./ops.js";
import { openReview } from "./reviews.js";

const box = mountOverlay("taskresult");
box.innerHTML =
  `<div id="taskresult-box"><div id="taskresult-title"></div><pre id="taskresult-body"></pre>` +
  `<div id="taskresult-btns"><button id="taskresult-apply" class="hidden">apply</button>` +
  `<button id="taskresult-copy">copy</button><button id="taskresult-close">close</button></div></div>`;
let shown = null; // { title, text, apply }

box.addEventListener("click", (e) => {
  if (e.target === box || e.target.id === "taskresult-close") return closeLayer("taskresult");
  if (e.target.id === "taskresult-copy" && shown) return copyText(shown.text, "result");
  if (e.target.id === "taskresult-apply" && shown) return applyMessage(shown.text);
});

// openTaskResult is enter on a task that has something to show (a live
// task's console is the caller's: it holds the row's session).
async function openTaskResult(t) {
  let got;
  try {
    got = await getJSON("/api/tasks/result?id=" + encodeURIComponent(t.id));
  } catch (e) {
    opLine(e.message || String(e), true);
    return;
  }
  if (got.note_id) return openReview(got.note_id, { kind: "list" });
  shown = { title: got.title, text: got.text || "", apply: !!got.apply };
  $("taskresult-title").textContent = got.title;
  $("taskresult-body").textContent = shown.text;
  $("taskresult-body").classList.toggle("log", got.ext === ".log");
  $("taskresult-apply").classList.toggle("hidden", !shown.apply);
  $("taskresult-body").scrollTop = 0;
  pushLayer("taskresult", box, {
    onKey: (e) => {
      if (e.key === "Escape") closeLayer("taskresult");
      else if (e.key === "y") copyText(shown.text, "result");
      else if (e.key === "a" && shown.apply) applyMessage(shown.text);
      else return true;
      e.preventDefault();
      return true;
    },
  });
}

// applyMessage puts a commit-message result in the commit box — the TUI's
// [a] apply; a message already typed there is replaced only after asking.
function applyMessage(text) {
  const put = () => {
    closeLayer("taskresult");
    const ta = $("commit-msg");
    ta.value = text.trim();
    ta.focus();
    opLine("commit message applied");
  };
  if ($("commit-msg").value.trim() === "") return put();
  showLocalConfirm("Replace the commit message you typed?", ["replace", "keep mine"], (o) => {
    if (o === "replace") put();
  });
}

// taskPost runs one of the task actions and reports a refusal.
async function taskPost(path, id, done) {
  try {
    await postJSON("/api/tasks/" + path, { id });
    if (done) opLine(done);
    return true;
  } catch (e) {
    opLine(e.message || String(e), true);
    return false;
  }
}

const cancelTask = (t) => taskPost("cancel", t.id, "cancelled " + t.key);
const removeTask = (t) => taskPost("remove", t.id, "");
const retryTaskSave = (t) => taskPost("retry-save", t.id, "review saved");

export { cancelTask, openTaskResult, removeTask, retryTaskSave };

"use strict";
/* mini-loop web UI (R1: sessions, ledger, composer, approvals).
   Safety contract (test_webui.py): every dynamic value reaches the DOM
   through textContent; no markup-injecting sink; no external resources. */

const $ = (id) => document.getElementById(id);

// ---- auth ---------------------------------------------------------------
const tokenInput = $("token");
tokenInput.value = localStorage.getItem("miniloop_token") || "";
tokenInput.addEventListener("change", () => {
  localStorage.setItem("miniloop_token", tokenInput.value.trim());
  loadSessions();
});
function authHeaders(extra) {
  const h = Object.assign({}, extra);
  const t = tokenInput.value.trim();
  if (t) h["Authorization"] = "Bearer " + t;
  return h;
}
function tokenQuery() {
  const t = tokenInput.value.trim();
  return t ? "&access_token=" + encodeURIComponent(t) : "";
}
async function api(path, options) {
  const opts = options || {};
  opts.headers = authHeaders(opts.headers || {});
  const response = await fetch(path, opts);
  if (!response.ok) {
    let detail = response.status + " " + response.statusText;
    try { detail = (await response.json()).detail || detail; } catch (e) {}
    throw new Error(detail);
  }
  return response.json();
}

// ---- DOM helpers (textContent only) -------------------------------------
function el(tag, cls, text) {
  const node = document.createElement(tag);
  if (cls) node.className = cls;
  if (text !== undefined) node.textContent = text;
  return node;
}

// ---- health -------------------------------------------------------------
async function loadHealth() {
  try {
    const h = await api("/healthz");
    $("health").textContent =
      h.model + (h.fake_llm ? " (fake)" : "") +
      " · " + (h.authenticated ? "authenticated" : "open") +
      " · " + h.sessions + " session(s)";
  } catch (err) {
    $("health").textContent = "health: " + err.message;
  }
}

// ---- session rail -------------------------------------------------------
let currentSid = null;
async function loadSessions() {
  let sessions = [];
  try { sessions = await api("/sessions?limit=100"); }
  catch (err) { $("health").textContent = "sessions: " + err.message; return; }
  const list = $("session-list");
  list.textContent = "";
  for (const s of sessions) {
    const item = el("li");
    item.dataset.sid = s.id;
    if (s.id === currentSid) item.className = "active";
    const sid = el("span", "sid", s.id);
    const badge = el("span", "badge", s.activity || s.status);
    badge.dataset.a = s.activity || s.status;
    item.append(sid, badge);
    item.addEventListener("click", () => selectSession(s.id));
    list.append(item);
    if (s.id === currentSid) {
      $("sess-activity").textContent = s.activity || s.status;
      $("sess-activity").dataset.a = s.activity || s.status;
    }
  }
}

$("new-session").addEventListener("click", () => { $("new-form").hidden = false; });
$("create-cancel").addEventListener("click", () => { $("new-form").hidden = true; });
$("create-confirm").addEventListener("click", async () => {
  try {
    const body = { mode: $("new-mode").value, permission: $("new-perm").value };
    const system = $("new-system").value.trim();
    if (system) body.system = system;
    const created = await api("/sessions", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    $("new-form").hidden = true;
    $("new-system").value = "";
    await loadSessions();
    selectSession(created.id);
  } catch (err) { alertRow("create failed: " + err.message); }
});

// ---- ledger -------------------------------------------------------------
let stream = null;
const openSpans = new Map();   // span_id -> row state
const streams = new Map();     // stream_id -> body element
let requestNo = 0;

function ledgerRow(kind, glyph, label, content, depth) {
  const row = el("div", "lrow");
  row.dataset.kind = kind;
  if (depth) {
    const pad = el("span", "depth-pad");
    pad.style.width = (depth * 18) + "px";
    row.append(pad);
  }
  row.append(el("span", "glyph", glyph));
  const body = el("span", "body");
  if (label) body.append(el("span", "label", label));
  const contentSpan = el("span", "content", content || "");
  body.append(contentSpan);
  row.append(body);
  const dur = el("span", "dur", "");
  row.append(dur);
  $("ledger").append(row);
  $("ledger").scrollTop = $("ledger").scrollHeight;
  return { row, contentSpan, dur };
}

function alertRow(text) {
  const r = ledgerRow("ref", "!", "ui", text, 0);
  r.row.dataset.error = "1";
}

function fmtDur(ms) {
  if (typeof ms !== "number") return "";
  return ms >= 1000 ? (ms / 1000).toFixed(1) + "s" : Math.round(ms) + "ms";
}

function onEvent(event) {
  const depth = event.depth || 0;
  const type = event.type;
  if (type === "status" && event.detail === "session_created") return;
  if (type === "model_start") {
    requestNo += 1;
    const r = ledgerRow("model", "#" + requestNo,
      event.model || "model",
      "purpose=" + (event.purpose || "?") + " messages=" + (event.message_count || "?"),
      depth);
    if (event.span_id) openSpans.set(event.span_id, r);
    r.row.dataset.state = "inflight";
    return;
  }
  if (type === "model_end") {
    const r = openSpans.get(event.span_id);
    if (!r) return;
    openSpans.delete(event.span_id);
    r.dur.textContent = fmtDur(event.duration_ms);
    if (event.error) { r.row.dataset.error = "1"; r.contentSpan.textContent = String(event.error); }
    else if (event.served_model && event.served_model !== event.model) {
      r.contentSpan.textContent += " · served by " + event.served_model;
    }
    return;
  }
  if (type === "tool_use") {
    const r = ledgerRow("tool", "⚙", event.name || "tool",
      JSON.stringify(event.input || {}), depth);
    if (event.span_id) openSpans.set(event.span_id, r);
    return;
  }
  if (type === "tool_result") {
    const r = openSpans.get(event.span_id);
    if (!r) return;
    openSpans.delete(event.span_id);
    r.dur.textContent = fmtDur(event.duration_ms);
    if (event.denied || event.error) r.row.dataset.error = "1";
    const out = String(event.output || "");
    r.contentSpan.textContent += "\n→ " + (out.length > 400 ? out.slice(0, 400) + "…" : out);
    return;
  }
  if (type === "stream_start") {
    const r = ledgerRow("answer", "…", "", "", depth);
    streams.set(event.stream_id, r.contentSpan);
    return;
  }
  if (type === "assistant_delta") {
    const span = streams.get(event.stream_id);
    if (span) span.textContent += event.text || "";
    return;
  }
  if (type === "assistant_text") {
    for (const [sid2, span] of streams) { span.textContent = ""; streams.delete(sid2); }
    const kind = event.phase === "final_answer" ? "answer" : "ref";
    ledgerRow(kind, kind === "answer" ? "✓" : "·", "assistant", event.text || "", depth);
    return;
  }
  if (type === "subagent_start") {
    ledgerRow("ref", "▶", "subagent:" + (event.agent_type || "?"), event.prompt || "", depth);
    return;
  }
  if (type === "subagent_end") {
    ledgerRow("ref", "◀", "subagent done", event.summary || "", depth);
    return;
  }
  if (type === "subagent_refused") {
    alertRow("delegation refused at depth " + event.child_depth);
    return;
  }
  if (type === "approval_request" || type === "approval_resolved" ||
      type === "approval_auto_reviewed") {
    refreshApprovals();
    ledgerRow("ref", "⚑", type, event.tool || "", depth);
    return;
  }
  if (type === "steering_delivered") {
    ledgerRow("user", "»", "steer", event.text || "", depth);
    return;
  }
  if (type === "turn_queued") { ledgerRow("ref", "…", "queued", "", depth); return; }
  if (type === "done") {
    ledgerRow("ref", "■", "turn done", "", depth);
    loadSessions();
    loadGoal();
    syncPosture();
    return;
  }
  if (type === "error") {
    alertRow(String(event.error || event.detail || "error"));
    return;
  }
  if (type === "compact" || type === "tool_catalog" || type === "system_prompt" ||
      type === "capability_plan" || type === "stuck") {
    ledgerRow("ref", "·", type, event.pattern || event.kind || "", depth);
    return;
  }
  // Unknown types still render: a new path inherits the need to be visible.
  ledgerRow("ref", "·", type, "", depth);
}

function openStream(sid) {
  if (stream) { stream.close(); stream = null; }
  openSpans.clear(); streams.clear(); requestNo = 0;
  $("ledger").textContent = "";
  stream = new EventSource(
    "/sessions/" + encodeURIComponent(sid) + "/events?envelope=true" + tokenQuery());
  stream.addEventListener("agent_event", (message) => {
    try { onEvent(JSON.parse(message.data)); } catch (e) {}
  });
  stream.onerror = () => { $("sess-activity").textContent = "reconnecting"; };
}

// ---- approvals ----------------------------------------------------------
async function refreshApprovals() {
  if (!currentSid) return;
  let payload;
  try { payload = await api("/sessions/" + encodeURIComponent(currentSid) + "/approvals"); }
  catch (err) { return; }
  const approvals = payload.approvals || [];
  $("approvals").hidden = approvals.length === 0;
  const list = $("approval-list");
  list.textContent = "";
  for (const a of approvals) {
    const item = el("li");
    const what = el("span", "what",
      (a.tool || "?") + " — " + (a.message || "") + " " + (a.input_preview || ""));
    const allow = el("button", "", "Allow");
    const deny = el("button", "sec danger", "Deny");
    allow.addEventListener("click", () => resolveApproval(a.approval_id, "allow"));
    deny.addEventListener("click", () => resolveApproval(a.approval_id, "deny"));
    item.append(what, allow, deny);
    list.append(item);
  }
}
async function resolveApproval(approvalId, decision) {
  try {
    await api("/sessions/" + encodeURIComponent(currentSid) +
      "/approvals/" + encodeURIComponent(approvalId), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ decision }),
    });
  } catch (err) { alertRow("approval failed: " + err.message); }
  refreshApprovals();
}

// ---- session selection & actions ---------------------------------------
async function selectSession(sid) {
  currentSid = sid;
  $("placeholder").hidden = true;
  $("session-view").hidden = false;
  $("sess-id").textContent = sid;
  showPane("ledger");
  openStream(sid);
  loadGoal();
  refreshApprovals();
  loadSessions();
  syncPosture();
}

// Both axes come from session info: the interaction mode (agent/plan/ask)
// and the permission posture. Re-synced on turn done, so a model that
// leaves plan mode via exit_plan_mode is reflected without a reload.
async function syncPosture() {
  if (!currentSid) return;
  try {
    const info = await api("/sessions/" + encodeURIComponent(currentSid));
    if (info.mode) $("mode-select").value = info.mode;
    if (info.permission_mode) $("perm-select").value = info.permission_mode;
  } catch (e) {}
}

$("mode-select").addEventListener("change", async () => {
  try {
    await api("/sessions/" + encodeURIComponent(currentSid) + "/mode", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ mode: $("mode-select").value }),
    });
    ledgerRow("ref", "·", "mode", $("mode-select").value, 0);
  } catch (err) { alertRow("mode change failed: " + err.message); }
});

$("perm-select").addEventListener("change", async () => {
  try {
    await api("/sessions/" + encodeURIComponent(currentSid) + "/mode", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ permission: $("perm-select").value }),
    });
    ledgerRow("ref", "·", "permission", $("perm-select").value, 0);
  } catch (err) { alertRow("permission change failed: " + err.message); }
});

$("fork-btn").addEventListener("click", async () => {
  try {
    const child = await api("/sessions/" + encodeURIComponent(currentSid) + "/fork",
      { method: "POST" });
    await loadSessions();
    selectSession(child.id);
  } catch (err) { alertRow("fork failed: " + err.message); }
});

$("cancel-btn").addEventListener("click", async () => {
  try {
    await api("/sessions/" + encodeURIComponent(currentSid) + "/cancel",
      { method: "POST" });
  } catch (err) { alertRow("cancel failed: " + err.message); }
});

$("send-btn").addEventListener("click", sendMessage);
$("msg").addEventListener("keydown", (keyEvent) => {
  if (keyEvent.key === "Enter" && (keyEvent.metaKey || keyEvent.ctrlKey)) sendMessage();
});
async function sendMessage() {
  const text = $("msg").value.trim();
  if (!text || !currentSid) return;
  $("msg").value = "";
  ledgerRow("user", "›", "you", text, 0);
  const sid = currentSid;
  try {
    await api("/sessions/" + encodeURIComponent(sid) + "/messages", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ message: text }),
    });
  } catch (err) {
    if (String(err.message).indexOf("running a turn") !== -1) {
      // Busy: steering is what a second message means mid-turn.
      try {
        await api("/sessions/" + encodeURIComponent(sid) + "/steer", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ message: text }),
        });
        ledgerRow("ref", "»", "steered", "delivered to the running turn", 0);
      } catch (steerErr) { alertRow("steer failed: " + steerErr.message); }
    } else {
      alertRow("send failed: " + err.message);
    }
  }
  loadSessions();
}

// ---- tabs ---------------------------------------------------------------
const PANES = ["ledger", "tasks", "team", "trajectories", "transcript",
               "cron", "skills", "memory", "improve", "benchmark"];
function showPane(name) {
  for (const paneId of PANES.concat(["audit-pane"])) {
    $(paneId).hidden = paneId !== name;
  }
  for (const tab of document.querySelectorAll(".tab")) {
    tab.classList.toggle("active", tab.dataset.tab === name);
  }
  if (name === "tasks") loadTasks();
  if (name === "team") loadTeam();
  if (name === "trajectories") loadTrajectories();
  if (name === "transcript") loadTranscript();
  if (name === "cron") loadCron();
  if (name === "skills") loadSkills();
  if (name === "memory") loadMemory();
}
for (const tab of document.querySelectorAll(".tab")) {
  tab.addEventListener("click", () => showPane(tab.dataset.tab));
}

// ---- task board (R7) ----------------------------------------------------
async function loadTasks() {
  if (!currentSid) return;
  const list = $("task-list");
  list.textContent = "";
  try {
    const payload = await api("/sessions/" + encodeURIComponent(currentSid) + "/tasks");
    for (const task of payload.tasks || []) {
      const item = el("li");
      const badge = el("span", "badge", task.status);
      badge.dataset.a = task.status === "completed" ? "idle"
        : (task.status === "in_progress" ? "running" : "stuck");
      const grow = el("span", "grow",
        task.id + " · " + task.subject +
        (task.owner ? " · owner " + task.owner : "") +
        (task.blockedBy && task.blockedBy.length
          ? " · blocked by " + task.blockedBy.join(", ") : "") +
        (task.worktree ? " · worktree " + task.worktree : ""));
      item.append(badge, grow);
      list.append(item);
    }
    if (!list.childNodes.length) list.append(el("li", "", "No tasks on this board."));
  } catch (err) { list.append(el("li", "", "tasks: " + err.message)); }
}
$("tasks-refresh").addEventListener("click", loadTasks);

// ---- team inbox (R7, peek-only) ----------------------------------------
async function loadTeam() {
  if (!currentSid) return;
  const list = $("team-inbox");
  list.textContent = "";
  try {
    const payload = await api("/sessions/" + encodeURIComponent(currentSid) + "/team");
    if (!payload.team) {
      list.append(el("li", "", "This session is not on a team."));
      return;
    }
    for (const message of payload.inbox || []) {
      const item = el("li");
      item.append(el("span", "grow",
        "[" + (message.type || "message") + "] " +
        (message.from || "?") + ": " + (message.content || "")));
      list.append(item);
    }
    if (!list.childNodes.length) {
      list.append(el("li", "",
        payload.team + "/" + payload.name + ": inbox empty (delivered "
        + "messages are consumed by the agent's own injector)"));
    }
  } catch (err) { list.append(el("li", "", "team: " + err.message)); }
}
$("team-refresh").addEventListener("click", loadTeam);

// ---- goal & plan mode (R7) ----------------------------------------------
async function loadGoal() {
  if (!currentSid) return;
  try {
    const payload = await api("/sessions/" + encodeURIComponent(currentSid) + "/goal");
    const goalBadge = $("sess-goal");
    const goal = payload.goal;
    if (goal && goal.objective) {
      goalBadge.textContent = "goal[" + goal.phase + "]: " +
        String(goal.objective).slice(0, 40) +
        " · " + goal.rounds_started + "/" + goal.max_rounds + " rounds";
      goalBadge.dataset.a = goal.phase === "blocked" ? "stuck"
        : (payload.goal_armed ? "running" : "idle");
      goalBadge.hidden = false;
    } else {
      goalBadge.hidden = true;
    }
    $("sess-plan").hidden = !payload.plan_mode;
  } catch (err) { /* the head stays quiet; the panes report their own errors */ }
}

// ---- trajectories (R2) --------------------------------------------------
async function loadTrajectories() {
  if (!currentSid) return;
  const list = $("traj-list");
  list.textContent = "";
  let payload;
  try {
    payload = await api("/sessions/" + encodeURIComponent(currentSid) + "/trajectories");
  } catch (err) { list.append(el("li", "", "error: " + err.message)); return; }
  for (const t of (payload.trajectories || payload || [])) {
    const item = el("li");
    const grow = el("span", "grow",
      t.id + " · " + (t.status || "?") +
      (typeof t.duration_ms === "number" ? " · " + fmtDur(t.duration_ms) : ""));
    const view = el("button", "sec", "Ledger view");
    view.addEventListener("click", () =>
      openAuthorized("/trajectories/" + encodeURIComponent(t.id) + "/view", "text/html"));
    const exportJson = el("button", "sec", "JSON");
    exportJson.addEventListener("click", () =>
      openAuthorized("/trajectories/" + encodeURIComponent(t.id) + "/export?format=json",
                     "application/json"));
    item.append(grow, view, exportJson);
    list.append(item);
  }
  if (!list.childNodes.length) list.append(el("li", "", "No recordings yet."));
}
$("traj-refresh").addEventListener("click", loadTrajectories);

// ---- transcript (R2) ----------------------------------------------------
async function loadTranscript() {
  if (!currentSid) return;
  const body = $("transcript-body");
  body.textContent = "";
  let payload;
  const epoch = $("epoch-select").value;
  const suffix = epoch ? "?epoch=" + encodeURIComponent(epoch) : "";
  try {
    payload = await api("/sessions/" + encodeURIComponent(currentSid) + "/transcript" + suffix);
  } catch (err) { body.textContent = "transcript: " + err.message; return; }
  const select = $("epoch-select");
  select.textContent = "";
  for (let i = 1; i <= (payload.epochs || 1); i++) {
    const option = el("option", "", "epoch " + i + (i === payload.epochs ? " (current)" : ""));
    option.value = String(i);
    if (i === payload.epoch) option.selected = true;
    select.append(option);
  }
  for (const message of payload.messages || []) {
    const role = el("div", "lrow");
    role.dataset.kind = message.role === "user" ? "user" : "answer";
    role.append(el("span", "glyph", message.role === "user" ? "›" : "«"));
    const content = typeof message.content === "string"
      ? message.content : JSON.stringify(message.content);
    role.append(el("span", "body", content));
    body.append(role);
  }
}
$("transcript-refresh").addEventListener("click", loadTranscript);
$("epoch-select").addEventListener("change", loadTranscript);

// ---- cron (R3) ----------------------------------------------------------
async function loadCron() {
  if (!currentSid) return;
  const list = $("cron-list");
  list.textContent = "";
  let payload;
  try { payload = await api("/sessions/" + encodeURIComponent(currentSid) + "/cron"); }
  catch (err) { list.append(el("li", "", "cron: " + err.message)); return; }
  for (const job of payload.jobs || []) {
    const item = el("li");
    const grow = el("span", "grow",
      job.id + " · " + job.cron + " · " + job.prompt.slice(0, 80) +
      (job.recurring ? " · recurring" : " · one-shot"));
    item.append(grow);
    if (!job.armed) {
      const badge = el("span", "badge", "DISARMED");
      badge.dataset.a = "stuck";
      const arm = el("button", "sec", "Arm");
      arm.addEventListener("click", async () => {
        try {
          await api("/sessions/" + encodeURIComponent(currentSid) +
            "/cron/" + encodeURIComponent(job.id) + "/arm", { method: "POST" });
        } catch (err) { alertRow("arm failed: " + err.message); }
        loadCron();
      });
      item.append(badge, arm);
    }
    const cancel = el("button", "sec danger", "Cancel");
    cancel.addEventListener("click", async () => {
      try {
        await api("/sessions/" + encodeURIComponent(currentSid) +
          "/cron/" + encodeURIComponent(job.id), { method: "DELETE" });
      } catch (err) { alertRow("cancel failed: " + err.message); }
      loadCron();
    });
    item.append(cancel);
    list.append(item);
  }
  if (!list.childNodes.length) list.append(el("li", "", "No scheduled jobs."));
}
$("cron-add").addEventListener("click", async () => {
  try {
    await api("/sessions/" + encodeURIComponent(currentSid) + "/cron", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ cron: $("cron-expr").value.trim(),
                            prompt: $("cron-prompt").value.trim() }),
    });
    $("cron-expr").value = ""; $("cron-prompt").value = "";
  } catch (err) { alertRow("schedule failed: " + err.message); }
  loadCron();
});

// ---- authorized open (R6) -----------------------------------------------
// window.open cannot carry the Authorization header, and the token must not
// ride a URL (history, logs). Fetch with the header, open the bytes as a
// same-process blob document instead: the server sees an authenticated
// request, the address bar never sees the token.
async function openAuthorized(path, contentType) {
  try {
    const t = tokenInput.value.trim();
    const response = await fetch(path,
      t ? { headers: { Authorization: "Bearer " + t } } : {});
    if (!response.ok) throw new Error(response.status + " " + response.statusText);
    const blob = await response.blob();
    const typed = new Blob([blob], { type: contentType });
    window.open(URL.createObjectURL(typed), "_blank");
  } catch (err) { alertRow("open failed: " + err.message); }
}

// ---- skills & memory (R4/R6) --------------------------------------------
async function loadSkills() {
  if (!currentSid) return;
  try {
    const payload = await api("/sessions/" + encodeURIComponent(currentSid) + "/skills");
    $("skills-catalogue").textContent = payload.catalogue || "(no skills catalogued)";
  } catch (err) { $("skills-catalogue").textContent = "skills: " + err.message; }
}

let pendingDraft = null;
$("ps-preview").addEventListener("click", async () => {
  const name = $("ps-name").value.trim();
  if (!name || !currentSid) return;
  $("ps-draft").textContent = "capturing…";
  $("ps-commit").hidden = true;
  try {
    const draft = await api("/sessions/" + encodeURIComponent(currentSid) +
      "/personal-skills/preview", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name, focus: $("ps-focus").value.trim() }),
    });
    pendingDraft = draft;
    $("ps-draft").textContent = JSON.stringify(draft, null, 1);
    $("ps-commit").hidden = !(draft.draft_id && draft.digest);
  } catch (err) { $("ps-draft").textContent = "preview failed: " + err.message; }
});
$("ps-commit").addEventListener("click", async () => {
  if (!pendingDraft) return;
  try {
    const published = await api("/sessions/" + encodeURIComponent(currentSid) +
      "/personal-skills/" + encodeURIComponent(pendingDraft.draft_id) + "/commit", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ digest: pendingDraft.digest }),
    });
    $("ps-draft").textContent = "published: " + JSON.stringify(published, null, 1);
    $("ps-commit").hidden = true;
    pendingDraft = null;
    loadSkills();
  } catch (err) { $("ps-draft").textContent = "commit failed: " + err.message; }
});
async function loadMemory() {
  if (!currentSid) return;
  const list = $("memory-list");
  list.textContent = "";
  $("memory-body").textContent = "";
  try {
    const payload = await api("/sessions/" + encodeURIComponent(currentSid) + "/memory");
    for (const memory of payload.memories || []) {
      const item = el("li");
      item.append(el("span", "grow",
        (memory.name || "?") + " [" + (memory.type || "?") + "] — " +
        (memory.description || "")));
      const read = el("button", "sec", "Read");
      read.addEventListener("click", async () => {
        try {
          const full = await api("/sessions/" + encodeURIComponent(currentSid) +
            "/memory/" + encodeURIComponent(memory.name));
          $("memory-body").textContent =
            "# " + full.name + " [" + full.type + "]\n" + full.body;
        } catch (err) { $("memory-body").textContent = "read failed: " + err.message; }
      });
      item.append(read);
      list.append(item);
    }
    if (!list.childNodes.length) list.append(el("li", "", "No memories."));
  } catch (err) { list.append(el("li", "", "memory: " + err.message)); }
}

// ---- benchmark (R6) -----------------------------------------------------
$("bench-run").addEventListener("click", async () => {
  const result = $("bench-result");
  result.textContent = "running the fake pairing…";
  try {
    const report = await api("/benchmark", { method: "POST" });
    const c = report.comparison;
    const lines = ["verdict: " + c.verdict,
      "baseline " + c.baseline_passed + "/" + c.tasks +
      " · candidate " + c.candidate_passed + "/" + c.tasks];
    for (const row of report.baseline.concat(report.candidate)) {
      lines.push(row.arm + " · " + row.task + " · " +
        (row.passed ? "pass" : "FAIL") + " · " + fmtDur(row.duration_ms));
    }
    lines.push("", report.note);
    result.textContent = lines.join("\n");
  } catch (err) { result.textContent = "benchmark failed: " + err.message; }
});

// ---- self-evolution (R5) ------------------------------------------------
$("improve-run").addEventListener("click", async () => {
  const objective = $("improve-objective").value.trim();
  const accept = $("improve-accept").value.trim();
  const result = $("improve-result");
  if (!objective || !accept) { result.textContent = "objective and acceptance command are required"; return; }
  result.textContent = "running the verified loop…";
  try {
    const proposal = await api("/sessions/" + encodeURIComponent(currentSid) +
      "/propose-improvement", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ objective, acceptance_command: accept }),
    });
    result.textContent =
      (proposal.verified ? "VERIFIED" : "UNVERIFIED") +
      " · branch " + proposal.branch + "\n\n" +
      (proposal.diff_stat || "(no diff)") + "\n\n" +
      (proposal.summary || "") + "\n\n" + proposal.next;
  } catch (err) { result.textContent = "proposal failed: " + err.message; }
});

// ---- self-audit (global, R3) --------------------------------------------
$("audit-btn").addEventListener("click", async () => {
  if (currentSid) showPane("audit-pane");
  else { $("placeholder").hidden = true; $("session-view").hidden = false; showPane("audit-pane"); }
  const pane = $("audit-pane");
  pane.textContent = "loading…";
  try {
    const t = tokenInput.value.trim();
    const response = await fetch("/self-audit",
      t ? { headers: { Authorization: "Bearer " + t } } : {});
    pane.textContent = await response.text();
  } catch (err) { pane.textContent = "self-audit: " + err.message; }
});

// ---- delete (R2) --------------------------------------------------------
$("delete-btn").addEventListener("click", async () => {
  if (!currentSid) return;
  const sure = window.confirm(
    "Delete session " + currentSid + "? The workspace is removed; recorded " +
    "trajectories are retained (the owner can still read them).");
  if (!sure) return;
  try {
    await api("/sessions/" + encodeURIComponent(currentSid), { method: "DELETE" });
    currentSid = null;
    $("session-view").hidden = true;
    $("placeholder").hidden = false;
    if (stream) { stream.close(); stream = null; }
  } catch (err) { alertRow("delete failed: " + err.message); }
  loadSessions();
});

// ---- boot ---------------------------------------------------------------
loadHealth();
loadSessions();
setInterval(loadSessions, 5000);
setInterval(loadHealth, 30000);
setInterval(refreshApprovals, 7000);

"use strict";
/* mini-loop web UI: sessions, conversation, tool disclosures, inspector.
   Safety contract (test_webui.py): every dynamic value reaches the DOM
   through textContent; no markup-injecting sink; no external resources. */

const $ = (id) => document.getElementById(id);

// Static, locally drawn icons. Untrusted content never supplies SVG markup.
const ICONS = {
  loop: "M19 8a8 8 0 1 0 1 7M19 3v5h-5",
  plus: "M12 5v14M5 12h14",
  search: "M21 21l-5-5M18 10a8 8 0 1 1-16 0 8 8 0 0 1 16 0",
  "panel-left": "M3 4h18v16H3zM9 4v16",
  "panel-right": "M3 4h18v16H3zM15 4v16",
  settings: "M4 7h16M4 17h16M8 4v6M16 14v6",
  moon: "M20 15a8 8 0 0 1-11-11A8.5 8.5 0 1 0 20 15",
  sun: "M16 12a4 4 0 1 1-8 0 4 4 0 0 1 8 0M12 2v2M12 20v2M2 12h2M20 12h2M5 5l1 1M18 18l1 1M5 19l1-1M18 6l1-1",
  more: "M5 12h.01M12 12h.01M19 12h.01",
  branch: "M6 3v12a4 4 0 0 0 4 4h8M6 7h7a5 5 0 0 0 5-5M15 16l3 3-3 3",
  stop: "M6 6h12v12H6z",
  trash: "M4 6h16M9 6V3h6v3M6 6l1 15h10l1-15M10 10v7M14 10v7",
  close: "M6 6l12 12M6 18L18 6",
  shield: "M12 3l8 3v6c0 5-8 9-8 9s-8-4-8-9V6zM9 12l2 2 4-4",
  "arrow-up": "M12 19V5M6 11l6-6 6 6",
  "arrow-right": "M5 12h14M13 6l6 6-6 6",
  "chevron-left": "M15 5l-7 7 7 7",
  "chevron-right": "M9 5l7 7-7 7",
  command: "M9 9V5a3 3 0 1 0-3 3h12a3 3 0 1 0-3-3v14a3 3 0 1 0 3-3H6a3 3 0 1 0 3 3V9",
  folder: "M3 6h7l2 3h9v11H3z",
  code: "M8 6l-6 6 6 6M16 6l6 6-6 6M14 4l-4 16",
  list: "M9 6h12M9 12h12M9 18h12M3 6h1M3 12h1M3 18h1",
  terminal: "M4 6l6 6-6 6M13 18h7",
};
function icon(name) {
  const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  for (const [key, value] of Object.entries({
    viewBox: "0 0 24 24", fill: "none", stroke: "currentColor",
    "stroke-width": "1.7", "stroke-linecap": "round", "stroke-linejoin": "round",
    "aria-hidden": "true", focusable: "false", class: "icon",
  })) svg.setAttribute(key, value);
  const path = document.createElementNS("http://www.w3.org/2000/svg", "path");
  path.setAttribute("d", ICONS[name] || ICONS.loop);
  svg.append(path);
  return svg;
}
for (const node of document.querySelectorAll("[data-icon]")) node.append(icon(node.dataset.icon));

function readPreference(key) {
  try { return localStorage.getItem(key) || ""; } catch (err) { return ""; }
}
function writePreference(key, value) {
  try { localStorage.setItem(key, value); return true; } catch (err) { return false; }
}
function setTheme(theme) {
  const dark = theme === "dark";
  document.documentElement.dataset.theme = dark ? "dark" : "light";
  $("theme-icon").replaceChildren(icon(dark ? "sun" : "moon"));
  const label = "Switch to " + (dark ? "light" : "dark") + " theme";
  $("theme-toggle").setAttribute("aria-label", label);
  $("theme-toggle").title = label;
}
$("theme-toggle").addEventListener("click", () => {
  const theme = document.documentElement.dataset.theme === "dark" ? "light" : "dark";
  setTheme(theme);
  writePreference("miniloop_theme", theme);
});

const mobileViewport = window.matchMedia("(max-width: 760px)");
const overlayViewport = window.matchMedia("(max-width: 1200px)");
function syncOverlayAccess() {
  const overlay = overlayViewport.matches && !$("inspector").hidden;
  $("conversation").inert = overlay;
  $("inspector").setAttribute("role", overlay ? "dialog" : "complementary");
  if (overlay) $("inspector").setAttribute("aria-modal", "true");
  else $("inspector").removeAttribute("aria-modal");
}
function setSidebarOpen(open, focus) {
  document.body.dataset.sidebarOpen = String(open);
  $("rail").hidden = !open;
  $("sidebar-toggle").setAttribute("aria-expanded", String(open));
  $("sidebar-backdrop").hidden = !(mobileViewport.matches && open);
  $("workspace").inert = mobileViewport.matches && open;
  if (focus) (open ? $("session-search") : $("sidebar-toggle")).focus();
}
$("sidebar-toggle").addEventListener("click", () => setSidebarOpen($("rail").hidden, true));
$("rail-close").addEventListener("click", () => setSidebarOpen(false, true));
$("sidebar-backdrop").addEventListener("click", () => setSidebarOpen(false, true));
mobileViewport.addEventListener("change", () => setSidebarOpen(!mobileViewport.matches));
overlayViewport.addEventListener("change", syncOverlayAccess);
$("settings-btn").addEventListener("click", () => $("settings-dialog").showModal());
$("settings-close").addEventListener("click", () => $("settings-dialog").close());
$("notice-dismiss").addEventListener("click", () => { $("ui-notice").hidden = true; });
document.addEventListener("keydown", (event) => {
  if (event.defaultPrevented || event.isComposing || event.keyCode === 229 || event.repeat) return;
  const dialog = document.querySelector("dialog[open]");
  if (dialog) {
    if (dialog.id === "shortcut-dialog" && recordingShortcut) {
      recordShortcut(event);
      return;
    }
    if (event.key === "Escape") {
      event.preventDefault();
      if (dialog.id === "command-dialog") closeCommandPalette();
      else if (dialog.id === "shortcut-dialog") closeShortcuts();
      else dialog.close();
    }
    return;
  }
  if (handleShortcut(event)) return;
  if (event.key === "Escape") {
    if (mobileViewport.matches && !$("rail").hidden) setSidebarOpen(false, true);
    else if (!$("inspector").hidden) closeInspector();
    else $("session-actions").open = false;
  }
  // An overlay must not leave keyboard focus on the covered conversation.
  const overlay = mobileViewport.matches && !$("rail").hidden ? $("rail")
    : (overlayViewport.matches && !$("inspector").hidden ? $("inspector") : null);
  if (event.key === "Tab" && overlay) {
    const controls = Array.from(overlay.querySelectorAll("button, input, select, textarea, a[href], summary"))
      .filter((node) => !node.disabled && node.getClientRects().length);
    const first = controls[0], last = controls[controls.length - 1];
    if (event.shiftKey && (document.activeElement === first || !overlay.contains(document.activeElement))) {
      event.preventDefault(); last?.focus();
    } else if (!event.shiftKey && (document.activeElement === last || !overlay.contains(document.activeElement))) {
      event.preventDefault(); first?.focus();
    }
  }
});

// ---- auth ---------------------------------------------------------------
const tokenInput = $("token");
tokenInput.value = readPreference("miniloop_token");
tokenInput.addEventListener("change", () => {
  writePreference("miniloop_token", tokenInput.value.trim());
  clearSession();
  sessionsCache = [];
  renderSessions();
  loadHealth();
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
    const error = new Error(detail);
    error.status = response.status;
    throw error;
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
    $("health").textContent = (h.fake_llm ? "Fake model · " : "Connected · ") +
      (h.authenticated ? "Authenticated" : "Open server");
    $("health").title = h.model + " · " + h.sessions + " session(s)";
    $("connection-dot").dataset.a = "idle";
    $("composer-model").textContent = h.model + (h.fake_llm ? " (fake)" : "");
    $("composer-model").title = $("composer-model").textContent;
  } catch (err) {
    $("health").textContent = "health: " + err.message;
    $("connection-dot").dataset.a = "error";
  }
}

// ---- session rail -------------------------------------------------------
let currentSid = null;
let sessionsCache = [];
let sessionsRenderKey = "";
let creatingSession = false;
let selectionVersion = 0;
function activityLabel(activity) {
  const label = String(activity || "idle").replaceAll("_", " ");
  return label.charAt(0).toUpperCase() + label.slice(1);
}
async function loadSessions() {
  const credential = tokenInput.value.trim();
  let sessions;
  try { sessions = await api("/sessions?limit=100"); }
  catch (err) { $("health").textContent = "sessions: " + err.message; return; }
  if (credential !== tokenInput.value.trim()) return;
  sessionsCache = sessions;
  renderSessions();
  const selected = sessions.find((s) => s.id === currentSid);
  if (selected) {
    $("sess-activity").textContent = activityLabel(selected.activity || selected.status);
    $("sess-activity").dataset.a = selected.activity || selected.status;
    $("cancel-btn").disabled = !selected.busy;
  }
  if ($("command-dialog").open) renderCommands();
}
function renderSessions() {
  const query = $("session-search").value.trim().toLowerCase();
  const key = JSON.stringify([currentSid, query, sessionsCache.map((s) =>
    [s.id, s.activity, s.status, s.run_count, s.permission_mode, s.workspace])]);
  if (key === sessionsRenderKey) return;
  sessionsRenderKey = key;
  const list = $("session-list");
  const focusedSid = list.contains(document.activeElement) ? document.activeElement.dataset.sid : null;
  list.textContent = "";
  $("session-count").textContent = String(sessionsCache.length);
  for (const s of sessionsCache) {
    if (query && !(s.id + " " + (s.workspace || "")).toLowerCase().includes(query)) continue;
    const item = el("li");
    const button = el("button", "session-button");
    button.type = "button";
    button.dataset.sid = s.id;
    button.title = s.id + (s.workspace ? "\n" + s.workspace : "");
    button.setAttribute("aria-current", String(s.id === currentSid));
    const copy = el("span", "session-copy");
    const title = el("span", "session-title", "Session " + s.id.slice(0, 8));
    const activity = s.activity || s.status;
    const count = s.run_count || 0;
    copy.append(title, el("span", "session-meta",
      activityLabel(activity) + " · " + count + (count === 1 ? " turn" : " turns")));
    const dot = el("span", "status-dot");
    dot.dataset.a = activity;
    dot.setAttribute("aria-hidden", "true");
    button.append(copy, dot);
    button.addEventListener("click", () => selectSession(s.id));
    item.append(button);
    list.append(item);
    if (s.id === focusedSid) button.focus({ preventScroll: true });
  }
  if (!list.childNodes.length) list.append(el("li", "session-empty",
    query ? "No matching sessions." : "Your sessions will appear here."));
}
$("session-search").addEventListener("input", renderSessions);

$("new-session").addEventListener("click", () => {
  $("create-error").hidden = true;
  $("new-form").showModal();
});
$("create-cancel").addEventListener("click", () => $("new-form").close());
async function createSession(mode, system) {
  if (creatingSession) return null;
  creatingSession = true;
  $("create-confirm").disabled = true;
  $("new-session").disabled = true;
  updateComposer();
  const version = selectionVersion;
  try {
    const body = { mode };
    if (system) body.system = system;
    const created = await api("/sessions", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    if (version === selectionVersion) await selectSession(created.id);
    return created.id;
  } finally {
    creatingSession = false;
    $("create-confirm").disabled = false;
    $("new-session").disabled = false;
    updateComposer();
  }
}
$("create-confirm").addEventListener("click", async () => {
  try {
    await createSession($("new-mode").value, $("new-system").value.trim());
    $("new-form").close();
    $("new-system").value = "";
    $("msg").focus();
  } catch (err) {
    $("create-error").textContent = "Could not create session: " + err.message;
    $("create-error").hidden = false;
  }
});

// ---- ledger -------------------------------------------------------------
let stream = null;
const openSpans = new Map();   // span_id -> row state
const streams = new Map();     // stream_id -> ephemeral row
// activity_id -> { body } phase groups (WEBUI_PLAN R8-3). Grouping is the
// event's own recorded association: a tool_use lands in a group only when
// it carries that group's activity_id -- never "the latest title".
const activities = new Map();
let requestNo = 0;
let traceGroup = null;

// R8-2 tense projection: the verb stem renders as-is while a call is only
// Requested/failed; only a successful tool_result may conjugate to past.
const VERB_STEM = { read: "Read", search: "Search", list: "List",
  write: "Write", edit: "Edit", run: "Run", call: "Call" };
const VERB_PAST = { read: "Read", search: "Searched", list: "Listed",
  write: "Wrote", edit: "Edited", run: "Ran", call: "Called" };

function displayLabel(display, table) {
  if (!display || !table[display.verb]) return null;
  const object = display.object ? " " + display.object : "";
  return table[display.verb] + object;
}

function diagnosticRow(event) {
  if (!traceGroup) {
    const row = el("details", "trace-group");
    const summary = el("summary", "tool-summary");
    const count = el("span", "dur");
    summary.append(icon("list"), el("span", "label", "Run details"), count);
    const body = el("div", "trace-events");
    row.append(summary, body);
    $("ledger").append(row);
    traceGroup = { body, count, size: 0 };
  }
  const detail = el("details", "trace-event");
  detail.append(el("summary", "", event.type), el("pre", "", JSON.stringify(event, null, 2)));
  traceGroup.body.append(detail);
  traceGroup.size += 1;
  traceGroup.count.textContent = traceGroup.size + " events";
}

function ledgerRow(kind, glyph, label, content, depth, container) {
  const disclosure = kind === "tool" || kind === "model";
  const row = el(disclosure ? "details" : "div", "lrow");
  row.dataset.kind = kind;
  const head = disclosure ? el("summary", "tool-summary") : row;
  if (depth && !disclosure) {
    const pad = el("span", "depth-pad");
    pad.style.width = (depth * 18) + "px";
    row.append(pad);
  }
  const mark = el("span", "glyph");
  mark.setAttribute("aria-hidden", "true");
  if (kind === "tool" || kind === "answer") mark.append(icon(kind === "tool" ? "terminal" : "loop"));
  else mark.textContent = glyph;
  head.append(mark);
  const dur = el("span", "dur", "");
  const contentSpan = el(disclosure ? "pre" : "span", "content", content || "");
  let labelEl = null;
  if (disclosure) {
    labelEl = el("span", "label", label);
    head.append(labelEl, dur);
    row.append(head, contentSpan);
  } else {
    const body = el("span", "body");
    if (label) { labelEl = el("span", "label", label); body.append(labelEl); }
    body.append(contentSpan);
    row.append(body, dur);
  }
  const ledger = $("ledger");
  const following = ledger.scrollHeight - ledger.scrollTop - ledger.clientHeight < 100;
  (container || ledger).append(row);
  if (following || kind === "user") ledger.scrollTop = ledger.scrollHeight;
  return { row, contentSpan, dur, labelEl };
}

function alertRow(text) {
  $("ui-notice-text").textContent = text;
  $("ui-notice").hidden = false;
  if (currentSid) {
    const r = ledgerRow("ref", "!", "ui", text, 0);
    r.row.dataset.error = "1";
  }
}

function fmtDur(ms) {
  if (typeof ms !== "number") return "";
  return ms >= 1000 ? (ms / 1000).toFixed(1) + "s" : Math.round(ms) + "ms";
}

function onEvent(event) {
  const depth = event.depth || 0;
  const type = event.type;
  if (type === "status" && event.detail === "session_created") return;
  if (["trajectory_start", "trajectory_end", "status", "tool_catalog", "capability_plan", "system_prompt"].includes(type)) {
    if (type === "trajectory_start") traceGroup = null;
    diagnosticRow(event);
    if (type === "status" && event.status) {
      $("sess-activity").textContent = activityLabel(event.status);
      $("sess-activity").dataset.a = event.status;
      $("cancel-btn").disabled = event.status !== "running";
    }
    return;
  }
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
    r.row.dataset.state = "complete";
    r.dur.textContent = fmtDur(event.duration_ms);
    if (event.error) { r.row.dataset.error = "1"; r.row.open = true; r.contentSpan.textContent = String(event.error); }
    else if (event.served_model && event.served_model !== event.model) {
      r.contentSpan.textContent += " · served by " + event.served_model;
    }
    return;
  }
  if (type === "activity_update") {
    // A phase header (R8-1/R8-3). Idempotent by id: a replayed event reuses
    // the existing group, and existing groups are never renamed.
    if (!event.activity_id || activities.has(event.activity_id)) return;
    const group = el("details", "lrow activity-group");
    group.open = true;
    group.dataset.kind = "activity";
    const summary = el("summary", "tool-summary");
    summary.append(el("span", "glyph", "§"),
      el("span", "label", event.title || ""), el("span", "dur", ""));
    const body = el("div", "activity-body");
    group.append(summary, body);
    $("ledger").append(group);
    activities.set(event.activity_id, { body });
    return;
  }
  if (type === "tool_use") {
    // Semantic label (R8-2) when the event carries a display projection;
    // older events keep the tool name. The raw name and masked params stay
    // in the details body either way -- the label never replaces them.
    const semantic = displayLabel(event.display, VERB_STEM);
    const group = event.activity_id && activities.get(event.activity_id);
    const r = ledgerRow("tool", "⚙", semantic || event.name || "tool",
      (event.name || "tool") + " " + JSON.stringify(event.input || {}), depth,
      group ? group.body : undefined);
    r.row.dataset.state = "requested";
    r.dur.textContent = "Requested";
    r.display = event.display;
    if (event.span_id) openSpans.set(event.span_id, r);
    return;
  }
  if (type === "tool_result") {
    const r = openSpans.get(event.span_id);
    if (!r) return;
    openSpans.delete(event.span_id);
    r.row.dataset.state = "complete";
    r.dur.textContent = fmtDur(event.duration_ms);
    if (event.denied || event.error) { r.row.dataset.error = "1"; r.row.open = true; }
    else {
      // Only a real success may conjugate to past tense (R8-2): a denied
      // or failed call keeps its requested-form label.
      const past = displayLabel(r.display, VERB_PAST);
      if (past && r.labelEl) r.labelEl.textContent = past;
    }
    const out = String(event.output || "");
    r.contentSpan.textContent += "\n→ " + (out.length > 400 ? out.slice(0, 400) + "…" : out);
    return;
  }
  if (type === "stream_start") {
    const r = ledgerRow("answer", "…", "mini-loop", "", depth);
    streams.set(event.stream_id, r);
    return;
  }
  if (type === "assistant_delta") {
    const r = streams.get(event.stream_id);
    if (r) {
      const ledger = $("ledger");
      const following = ledger.scrollHeight - ledger.scrollTop - ledger.clientHeight < 100;
      r.contentSpan.textContent += event.text || "";
      if (following) ledger.scrollTop = ledger.scrollHeight;
    }
    return;
  }
  if (type === "assistant_text") {
    for (const r of streams.values()) r.row.remove();
    streams.clear();
    const kind = event.phase === "final_answer" ? "answer" : "ref";
    const r = ledgerRow(kind, "·", "mini-loop", event.text || "", depth);
    r.row.dataset.phase = event.phase || "commentary";
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
  if (type === "approval_request" || type === "approval_required" || type === "approval_resolved" ||
      type === "approval_auto_reviewed") {
    refreshApprovals();
    loadSessions();
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
    traceGroup = null;
    loadSessions();
    loadGoal();
    return;
  }
  if (type === "error") {
    alertRow(String(event.error || event.detail || "error"));
    return;
  }
  if (type === "compact" || type === "stuck") {
    ledgerRow("ref", "·", type, event.pattern || event.kind || "", depth);
    return;
  }
  // Unknown types still render: a new path inherits the need to be visible.
  ledgerRow("ref", "·", type, "", depth);
}

function openStream(sid) {
  if (stream) { stream.close(); stream = null; }
  openSpans.clear(); streams.clear(); activities.clear();
  requestNo = 0; traceGroup = null;
  $("ledger").textContent = "";
  stream = new EventSource(
    "/sessions/" + encodeURIComponent(sid) + "/events?envelope=true" + tokenQuery());
  stream.addEventListener("agent_event", (message) => {
    if (sid !== currentSid) return;
    try { onEvent(JSON.parse(message.data)); } catch (e) {}
  });
  stream.onerror = () => {
    if (sid === currentSid) $("sess-activity").textContent = "Reconnecting";
  };
}

// ---- approvals ----------------------------------------------------------
async function refreshApprovals() {
  if (!currentSid) return;
  const sid = currentSid;
  let payload;
  try { payload = await api("/sessions/" + encodeURIComponent(sid) + "/approvals"); }
  catch (err) { return; }
  if (sid !== currentSid) return;
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
    allow.addEventListener("click", () => resolveApproval(a.approval_id, "allow", sid));
    deny.addEventListener("click", () => resolveApproval(a.approval_id, "deny", sid));
    const actions = el("div", "approval-actions");
    actions.append(deny, allow);
    if (a.grant_candidate && a.grant_candidate.length) {
      // Session-scoped learning: an informed yes may generalize to the
      // shown prefix (approvals.py grant machinery). Label carries the
      // exact grant; provenance marks a model-proposed prefix.
      const scope = a.grant_candidate.length > 1
        ? a.grant_candidate.slice(1).join(" ")
        : a.grant_candidate[0];
      const origin = a.grant_proposed ? " (model-proposed)" : "";
      const remember = el("button", "sec",
        "Allow + remember: " + scope + origin);
      remember.addEventListener("click",
        () => resolveApproval(a.approval_id, "allow", sid, true));
      actions.append(remember);
    }
    item.append(what, actions);
    list.append(item);
  }
}
async function resolveApproval(approvalId, decision, sid, remember) {
  if (sid !== currentSid) return;
  try {
    await api("/sessions/" + encodeURIComponent(sid) +
      "/approvals/" + encodeURIComponent(approvalId), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ decision, remember: !!remember }),
    });
  } catch (err) { alertRow("approval failed: " + err.message); }
  refreshApprovals();
}

// ---- session selection & actions ---------------------------------------
function clearSession() {
  selectionVersion += 1;
  currentSid = null;
  visitHistory = [];
  visitIndex = -1;
  invalidateSessionNavigation();
  closeCommandPalette(false);
  closeShortcuts(false);
  if (stream) { stream.close(); stream = null; }
  openSpans.clear(); streams.clear(); traceGroup = null;
  $("ledger").textContent = "";
  $("session-view").hidden = true;
  $("placeholder").hidden = false;
  $("suggestions").hidden = false;
  $("conversation").classList.add("is-empty");
  $("session-actions").hidden = true;
  $("session-actions").open = false;
  $("sess-activity").hidden = true;
  $("sess-goal").hidden = true;
  $("sess-plan").hidden = true;
  $("approvals").hidden = true;
  $("sess-id").textContent = "New session";
  $("sess-id").title = "";
  $("workspace-path").textContent = "New isolated workspace";
  $("workspace-path").title = "";
  $("mode-select").value = "interactive";
  $("tools-btn").disabled = true;
  $("msg").value = "";
  pendingDraft = null;
  showPane("ledger");
  updateComposer();
}
async function selectSession(sid, options = {}) {
  if (options.history !== false) invalidateSessionNavigation();
  if (sid === currentSid) {
    if (mobileViewport.matches) setSidebarOpen(false);
    return;
  }
  selectionVersion += 1;
  const version = selectionVersion;
  currentSid = sid;
  if (options.history !== false) recordVisit(sid);
  updateNavigation();
  $("placeholder").hidden = true;
  $("suggestions").hidden = true;
  $("conversation").classList.remove("is-empty");
  $("session-view").hidden = false;
  $("sess-id").textContent = "Session " + sid.slice(0, 8);
  $("sess-id").title = sid;
  $("sess-activity").hidden = false;
  $("sess-activity").textContent = "Connecting";
  $("session-actions").hidden = false;
  $("session-actions").open = false;
  $("tools-btn").disabled = false;
  $("sess-goal").hidden = true;
  $("sess-plan").hidden = true;
  $("approvals").hidden = true;
  $("epoch-select").textContent = "";
  $("ps-draft").textContent = "";
  $("ps-commit").hidden = true;
  pendingDraft = null;
  $("ui-notice").hidden = true;
  if (mobileViewport.matches) setSidebarOpen(false);
  showPane("ledger");
  openStream(sid);
  loadGoal();
  refreshApprovals();
  loadSessions();
  try {
    const info = options.info || await api("/sessions/" + encodeURIComponent(sid));
    if (sid !== currentSid || version !== selectionVersion) return;
    if (info.permission_mode) $("mode-select").value = info.permission_mode;
    const path = info.workspace || "";
    $("workspace-path").textContent = path ? path.split(/[\\/]/).filter(Boolean).slice(-2).join("/") : "Isolated workspace";
    $("workspace-path").title = path;
  } catch (err) { if (sid === currentSid && version === selectionVersion) alertRow("session: " + err.message); }
}

$("mode-select").addEventListener("change", async () => {
  if (!currentSid) return; // This is the permission choice for the first message.
  const sid = currentSid;
  const mode = $("mode-select").value;
  try {
    await api("/sessions/" + encodeURIComponent(sid) + "/mode", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ mode }),
    });
    if (sid === currentSid) ledgerRow("ref", "·", "mode", mode, 0);
  } catch (err) { alertRow("mode change failed: " + err.message); }
});

$("fork-btn").addEventListener("click", async () => {
  if (!currentSid) return;
  $("session-actions").open = false;
  try {
    const child = await api("/sessions/" + encodeURIComponent(currentSid) + "/fork",
      { method: "POST" });
    await loadSessions();
    selectSession(child.id);
  } catch (err) { alertRow("fork failed: " + err.message); }
});

$("cancel-btn").addEventListener("click", async () => {
  if (!currentSid) return;
  $("session-actions").open = false;
  try {
    await api("/sessions/" + encodeURIComponent(currentSid) + "/cancel",
      { method: "POST" });
  } catch (err) { alertRow("cancel failed: " + err.message); }
});

$("send-btn").addEventListener("click", sendMessage);
$("msg").addEventListener("keydown", (keyEvent) => {
  if (!keyEvent.isComposing && keyEvent.keyCode !== 229 && keyEvent.key === "Enter" && (keyEvent.metaKey || keyEvent.ctrlKey)) {
    keyEvent.preventDefault();
    sendMessage();
  }
});
function updateComposer() {
  $("send-btn").disabled = creatingSession || !$("msg").value.trim();
  $("msg").style.height = "auto";
  $("msg").style.height = Math.min(220, Math.max(76, $("msg").scrollHeight)) + "px";
}
$("msg").addEventListener("input", updateComposer);
for (const suggestion of document.querySelectorAll("[data-prompt]")) {
  suggestion.addEventListener("click", () => {
    $("msg").value = suggestion.dataset.prompt;
    updateComposer();
    $("msg").focus();
  });
}
async function sendMessage() {
  const text = $("msg").value.trim();
  if (!text || creatingSession) return;
  let sid = currentSid;
  if (!sid) {
    try { sid = await createSession($("mode-select").value); }
    catch (err) { alertRow("create failed: " + err.message); return; }
  }
  if (!sid) return;
  if ($("msg").value.trim() === text) $("msg").value = "";
  updateComposer();
  if (sid === currentSid) ledgerRow("user", "›", "you", text, 0);
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
        if (sid === currentSid) ledgerRow("ref", "»", "steered", "delivered to the running turn", 0);
      } catch (steerErr) { alertRow("steer failed: " + steerErr.message); }
    } else {
      alertRow("send failed: " + err.message);
    }
  }
  loadSessions();
}

// ---- tabs ---------------------------------------------------------------
const PANES = ["ledger", "tasks", "team", "trajectories", "transcript",
               "cron", "workflows", "skills", "memory", "improve", "benchmark"];
const PANE_TITLES = { tasks: "Tasks", team: "Team inbox", trajectories: "Trajectories",
  transcript: "Transcript", cron: "Scheduled prompts", workflows: "Workflows",
  skills: "Skills", memory: "Memory",
  improve: "Propose an improvement", benchmark: "Benchmark", "audit-pane": "Self-audit" };
let lastInspectorPane = "tasks";
function showPane(name) {
  if (!PANES.includes(name) && name !== "audit-pane") return;
  if (name !== "ledger" && name !== "audit-pane" && !currentSid) return;
  const wasClosed = $("inspector").hidden;
  for (const paneId of PANES.filter((id) => id !== "ledger").concat(["audit-pane"])) {
    $(paneId).hidden = paneId !== name;
  }
  $("inspector").hidden = name === "ledger";
  $("tools-btn").setAttribute("aria-expanded", String(name !== "ledger"));
  $("tabs").hidden = !currentSid;
  if (name !== "ledger") {
    $("pane-title").textContent = PANE_TITLES[name];
    if (name !== "audit-pane") lastInspectorPane = name;
  }
  syncOverlayAccess();
  if (wasClosed && name !== "ledger") $("inspector-close").focus();
  for (const tab of document.querySelectorAll(".tab")) {
    tab.classList.toggle("active", tab.dataset.tab === name);
    tab.setAttribute("aria-pressed", String(tab.dataset.tab === name));
  }
  if (name === "tasks") loadTasks();
  if (name === "team") loadTeam();
  if (name === "trajectories") loadTrajectories();
  if (name === "transcript") loadTranscript();
  if (name === "cron") loadCron();
  if (name === "workflows") loadWorkflows();
  if (name === "skills") loadSkills();
  if (name === "memory") loadMemory();
  if (name === "improve") loadImprovementLineage();
}
function closeInspector() {
  showPane("ledger");
  ($("tools-btn").disabled ? $("msg") : $("tools-btn")).focus();
}
$("tools-btn").addEventListener("click", () => showPane($("inspector").hidden ? lastInspectorPane : "ledger"));
$("inspector-close").addEventListener("click", closeInspector);
for (const tab of document.querySelectorAll(".tab")) {
  tab.addEventListener("click", () => showPane(tab.dataset.tab));
}

// ---- workspace commands, local shortcuts and visited-session navigation ----
// UI-only adapters: no new API, tool dispatch, model request or server settings.
const SHORTCUT_DEFAULTS = Object.freeze({
  "palette.open": "Mod+K", "session.back": "Mod+[", "session.forward": "Mod+]",
  "composer.focus": null, "sessions.search": null, "session.new": null,
  "sidebar.toggle": null, "tools.toggle": null, "settings.open": null, "shortcuts.open": null,
});
const RESERVED_SHORTCUTS = new Set([
  "Mod+A", "Mod+C", "Mod+X", "Mod+V", "Mod+Z", "Mod+Y", "Mod+F", "Mod+G",
  "Mod+H", "Mod+J", "Mod+L", "Mod+N", "Mod+O", "Mod+P", "Mod+Q", "Mod+R",
  "Mod+S", "Mod+T", "Mod+W", "Mod+U", "Mod+Enter",
  "Mod+Shift+A", "Mod+Shift+C", "Mod+Shift+X", "Mod+Shift+V", "Mod+Shift+Z",
  "Mod+Shift+F", "Mod+Shift+G", "Mod+Shift+I", "Mod+Shift+J", "Mod+Shift+N",
  "Mod+Shift+P", "Mod+Shift+Q", "Mod+Shift+R", "Mod+Shift+T", "Mod+Shift+W",
]);
let shortcutBindings = loadShortcutBindings();
let recordingShortcut = null;
let shortcutReturnFocus = null;
let commandReturnFocus = null;
let selectedCommandId = null;
let visibleCommands = [];
let visitHistory = [];
let visitIndex = -1;
let navigatingSession = false;
let navigationVersion = 0;

function invalidateSessionNavigation() {
  navigationVersion += 1;
  navigatingSession = false;
  updateNavigation();
}

function recordVisit(sid) {
  if (visitHistory[visitIndex] === sid) return;
  visitHistory = visitHistory.slice(0, visitIndex + 1);
  visitHistory.push(sid);
  if (visitHistory.length > 100) visitHistory = visitHistory.slice(-100);
  visitIndex = visitHistory.length - 1;
}
function forgetSession(sid) {
  const before = visitHistory.slice(0, visitIndex + 1).filter((id) => id !== sid).length;
  visitHistory = visitHistory.filter((id) => id !== sid);
  visitIndex = Math.min(before - 1, visitHistory.length - 1);
  updateNavigation();
}
function updateNavigation() {
  $("session-back").disabled = navigatingSession || visitIndex <= 0;
  $("session-forward").disabled = navigatingSession || visitIndex < 0 || visitIndex >= visitHistory.length - 1;
}
async function navigateSession(direction) {
  if (navigatingSession || ![-1, 1].includes(direction)) return false;
  const index = visitIndex + direction;
  if (index < 0 || index >= visitHistory.length) return false;
  const target = visitHistory[index];
  const version = selectionVersion;
  const navigation = ++navigationVersion;
  navigatingSession = true;
  updateNavigation();
  try {
    // Check authorization/existence before discarding the visible conversation.
    const info = await api("/sessions/" + encodeURIComponent(target));
    if (version !== selectionVersion || navigation !== navigationVersion) return false;
    visitIndex = index;
    await selectSession(target, { history: false, info });
    return true;
  } catch (err) {
    if (version === selectionVersion && navigation === navigationVersion) {
      if (err.status === 404) forgetSession(target);
      alertRow("Could not open visited session: " + err.message);
    }
    return false;
  } finally {
    if (navigation === navigationVersion) {
      navigatingSession = false;
      updateNavigation();
      if ($("command-dialog").open) renderCommands();
    }
  }
}
$("session-back").addEventListener("click", () => navigateSession(-1));
$("session-forward").addEventListener("click", () => navigateSession(1));

function focusComposer() {
  if (mobileViewport.matches) setSidebarOpen(false);
  showPane("ledger");
  $("msg").focus({ preventScroll: true });
}
function searchSessions() {
  showPane("ledger");
  setSidebarOpen(true, true);
}
function sessionReason() { return currentSid ? "" : "Choose a session first."; }
function getCommands() {
  const commands = [
    { id: "palette.open", label: "Open commands", group: "Navigation", detail: "Search actions and sessions", run: openCommandPalette },
    { id: "composer.focus", label: "Focus message input", group: "Navigation", detail: "Continue your draft without sending it", run: focusComposer },
    { id: "sessions.search", label: "Search sessions", group: "Navigation", detail: "Find a session by its ID or workspace", run: searchSessions },
    { id: "session.back", label: "Previous session", group: "Navigation", detail: "Back through sessions visited on this page",
      disabledReason: $("session-back").disabled ? "No previous session available." : "", run: () => navigateSession(-1) },
    { id: "session.forward", label: "Next session", group: "Navigation", detail: "Forward through sessions visited on this page",
      disabledReason: $("session-forward").disabled ? "No next session available." : "", run: () => navigateSession(1) },
    { id: "sidebar.toggle", label: "Toggle session sidebar", group: "Navigation", detail: "Show or hide recent sessions",
      run: () => setSidebarOpen($("rail").hidden, true) },
    { id: "tools.toggle", label: "Toggle session tools", group: "Navigation", detail: "Inspect the current session",
      disabledReason: sessionReason(), run: () => {
        if (mobileViewport.matches) setSidebarOpen(false);
        if ($("inspector").hidden) showPane(lastInspectorPane);
        else closeInspector();
      } },
    { id: "session.new", label: "New session", group: "Session actions", detail: "Choose permissions and optional instructions",
      disabledReason: creatingSession ? "A session is being created." : "", run: () => $("new-session").click() },
    { id: "session.fork", label: "Fork session", group: "Session actions", detail: "Use the existing fork operation",
      disabledReason: sessionReason(), run: () => $("fork-btn").click() },
    { id: "session.cancel", label: "Cancel turn", group: "Session actions", detail: "Stop the current session's running turn",
      disabledReason: sessionReason() || ($("cancel-btn").disabled ? "No running turn." : ""), run: () => $("cancel-btn").click() },
    { id: "session.delete", label: "Delete session", group: "Session actions", detail: "Requires the existing confirmation",
      disabledReason: sessionReason(), run: () => $("delete-btn").click() },
    ...PANES.filter((name) => name !== "ledger").map((name) => ({
      id: "pane." + name, label: "Open " + PANE_TITLES[name], group: "Session tools",
      detail: "Inspect the current session", disabledReason: sessionReason(),
      run: () => { if (mobileViewport.matches) setSidebarOpen(false); showPane(name); },
    })),
    { id: "settings.open", label: "Open settings", group: "Preferences", detail: "API token and local preferences", run: () => $("settings-dialog").showModal() },
    { id: "shortcuts.open", label: "Keyboard shortcuts", group: "Preferences", detail: "Record, disable or reset local key bindings", run: openShortcuts },
    { id: "theme.toggle", label: "Switch to " + (document.documentElement.dataset.theme === "dark" ? "light" : "dark") + " theme",
      group: "Preferences", detail: "Saved in this browser", run: () => $("theme-toggle").click() },
    { id: "audit.open", label: "Open self-audit", group: "Diagnostics", detail: "Read the existing owner-scoped report", run: () => $("audit-btn").click() },
    ...sessionsCache.map((session) => ({
      id: "session.select." + session.id, label: "Session " + session.id.slice(0, 8), group: "Sessions",
      detail: session.id + (session.workspace ? " · " + session.workspace : ""),
      disabledReason: session.id === currentSid ? "Current session." : "",
      run: () => selectSession(session.id),
    })),
  ];
  return commands.map((command) => ({ disabledReason: "", ...command, binding: shortcutBindings[command.id] || null }));
}
async function runCommand(id) {
  const command = getCommands().find((item) => item.id === id);
  if (!command || command.disabledReason) return false;
  if (id !== "palette.open") closeCommandPalette(false);
  try { await command.run(); return true; }
  catch (err) { alertRow("Command failed: " + err.message); return false; }
}
function returnFocus(node) {
  if (node?.isConnected && !node.disabled && node.getClientRects().length && !node.closest("[inert]")) node.focus({ preventScroll: true });
  else $("commands-btn").focus({ preventScroll: true });
}
function openCommandPalette() {
  if (document.querySelector("dialog[open]")) return false;
  commandReturnFocus = document.activeElement;
  $("command-search").value = "";
  selectedCommandId = null;
  renderCommands();
  $("command-dialog").showModal();
  $("command-search").focus();
  return true;
}
function closeCommandPalette(restore = true) {
  if (!$("command-dialog").open) return;
  $("command-dialog").close();
  if (restore) returnFocus(commandReturnFocus);
}
function renderCommands() {
  const query = $("command-search").value.trim().toLowerCase();
  visibleCommands = getCommands().filter((command) => command.id !== "palette.open" &&
    [command.label, command.detail, command.group].join(" ").toLowerCase().includes(query));
  if (!visibleCommands.some((command) => command.id === selectedCommandId && !command.disabledReason)) {
    selectedCommandId = visibleCommands.find((command) => !command.disabledReason)?.id || null;
  }
  const list = $("command-list");
  list.textContent = "";
  let group = "";
  visibleCommands.forEach((command, index) => {
    if (command.group !== group) {
      group = command.group;
      const heading = el("div", "command-group", group);
      heading.setAttribute("role", "presentation");
      list.append(heading);
    }
    const option = el("button", "command-option");
    option.type = "button";
    option.setAttribute("id", "command-option-" + index);
    option.setAttribute("role", "option");
    option.setAttribute("tabindex", "-1");
    option.setAttribute("data-command-id", command.id);
    option.setAttribute("aria-disabled", String(!!command.disabledReason));
    option.setAttribute("aria-selected", String(command.id === selectedCommandId));
    const copy = el("span", "command-copy");
    copy.append(el("span", "command-name", command.label), el("span", "command-detail", command.disabledReason || command.detail));
    option.append(copy);
    if (command.binding) option.append(el("kbd", "command-key", formatShortcut(command.binding)));
    option.addEventListener("click", () => runCommand(command.id));
    list.append(option);
  });
  $("command-status").textContent = visibleCommands.length
    ? visibleCommands.length + " results · unavailable actions explain why"
    : "No matching commands or sessions.";
  updateCommandSelection();
}
function updateCommandSelection() {
  let active = null;
  for (const option of $("command-list").querySelectorAll("[data-command-id]")) {
    const selected = option.dataset.commandId === selectedCommandId;
    option.setAttribute("aria-selected", String(selected));
    if (selected) active = option;
  }
  if (active) $("command-search").setAttribute("aria-activedescendant", active.id);
  else $("command-search").removeAttribute("aria-activedescendant");
  return active;
}
$("commands-btn").addEventListener("click", openCommandPalette);
$("command-close").addEventListener("click", () => closeCommandPalette());
$("command-search").addEventListener("input", () => { selectedCommandId = null; renderCommands(); });
$("command-search").addEventListener("keydown", (event) => {
  if (event.isComposing || event.keyCode === 229 || event.repeat || event.defaultPrevented) return;
  if (event.key === "ArrowDown" || event.key === "ArrowUp") {
    event.preventDefault();
    const enabled = visibleCommands.filter((command) => !command.disabledReason);
    if (!enabled.length) return;
    const index = enabled.findIndex((command) => command.id === selectedCommandId);
    selectedCommandId = enabled[(index + (event.key === "ArrowDown" ? 1 : -1) + enabled.length) % enabled.length].id;
    updateCommandSelection()?.scrollIntoView({ block: "nearest" });
  } else if (event.key === "Enter") {
    event.preventDefault();
    if (selectedCommandId) return runCommand(selectedCommandId);
  }
});
$("command-shortcuts").addEventListener("click", () => { closeCommandPalette(false); openShortcuts(); });

function normalizeShortcut(binding) {
  if (binding === null) return null;
  if (typeof binding !== "string") return undefined;
  const parts = binding.split("+");
  const key = parts.pop();
  if (!parts.includes("Mod") || parts.some((part) => !["Mod", "Alt", "Shift"].includes(part)) ||
      new Set(parts).size !== parts.length || !/^([A-Za-z0-9[\],./]|Enter)$/.test(key || "")) return undefined;
  return ["Mod", ...(parts.includes("Alt") ? ["Alt"] : []), ...(parts.includes("Shift") ? ["Shift"] : []),
    key === "Enter" ? key : key.toUpperCase()].join("+");
}
function shortcutReserved(binding) {
  // The composer already sends on every Ctrl/Meta+Enter variant.
  return RESERVED_SHORTCUTS.has(binding) || Boolean(binding?.endsWith("+Enter")) || /^Mod\+(?:Shift\+)?[0-9]$/.test(binding);
}
function loadShortcutBindings() {
  const bindings = { ...SHORTCUT_DEFAULTS };
  try {
    const saved = JSON.parse(readPreference("miniloop_shortcuts") || "{}");
    if (!saved || Array.isArray(saved) || typeof saved !== "object") return bindings;
    for (const id of Object.keys(SHORTCUT_DEFAULTS)) {
      if (!Object.hasOwn(saved, id)) continue;
      const binding = normalizeShortcut(saved[id]);
      if (binding !== undefined && !shortcutReserved(binding)) bindings[id] = binding;
    }
    const enabled = Object.values(bindings).filter(Boolean);
    if (new Set(enabled).size !== enabled.length) return { ...SHORTCUT_DEFAULTS };
  } catch (err) { /* damaged or unavailable storage falls back to usable defaults */ }
  return bindings;
}
function formatShortcut(binding) {
  return binding ? binding.replace("Mod", "Ctrl/⌘").replaceAll("+", " + ") : "Not set";
}
function updateShortcutHints() {
  $("command-hint").textContent = formatShortcut(shortcutBindings["palette.open"]);
  for (const [id, command] of [["commands-btn", "palette.open"], ["session-back", "session.back"], ["session-forward", "session.forward"]]) {
    const label = $(id).getAttribute("aria-label");
    $(id).title = label + (shortcutBindings[command] ? " (" + formatShortcut(shortcutBindings[command]) + ")" : "");
  }
}
function setShortcutBinding(id, value) {
  if (!Object.hasOwn(SHORTCUT_DEFAULTS, id)) return { ok: false, error: "Unknown action." };
  const binding = normalizeShortcut(value);
  if (binding === undefined) return { ok: false, error: "Use Ctrl or ⌘ with a letter, number or punctuation key." };
  if (shortcutReserved(binding)) return { ok: false, error: "That shortcut is reserved for the browser, editing or sending." };
  const conflict = Object.keys(shortcutBindings).find((other) => other !== id && binding && shortcutBindings[other] === binding);
  if (conflict) {
    const label = getCommands().find((command) => command.id === conflict)?.label || conflict;
    return { ok: false, error: "Already assigned to " + label + ". Choose another shortcut." };
  }
  shortcutBindings[id] = binding;
  const persisted = writePreference("miniloop_shortcuts", JSON.stringify(shortcutBindings));
  updateShortcutHints();
  return { ok: true, persisted };
}
function shortcutFromEvent(event) {
  if (event.isComposing || event.keyCode === 229 || event.repeat || event.getModifierState?.("AltGraph") || (!event.metaKey && !event.ctrlKey)) return null;
  let key = event.key;
  if (/^Key[A-Z]$/.test(event.code || "")) key = event.code.slice(3);
  else if (/^Digit[0-9]$/.test(event.code || "")) key = event.code.slice(5);
  else key = ({ BracketLeft: "[", BracketRight: "]", Comma: ",", Period: ".", Slash: "/" })[event.code] || key;
  return normalizeShortcut(["Mod", ...(event.altKey ? ["Alt"] : []), ...(event.shiftKey ? ["Shift"] : []), key].join("+")) || null;
}
function handleShortcut(event) {
  const binding = shortcutFromEvent(event);
  if (!binding) return false;
  const matches = Object.keys(shortcutBindings).filter((id) => shortcutBindings[id] === binding);
  if (!matches.length) return false;
  event.preventDefault();
  if (matches.length === 1) runCommand(matches[0]);
  return true;
}
function openShortcuts() {
  if ($("settings-dialog").open) $("settings-dialog").close();
  if (document.querySelector("dialog[open]")) return false;
  shortcutReturnFocus = document.activeElement;
  recordingShortcut = null;
  $("shortcut-feedback").textContent = "";
  $("shortcut-feedback").dataset.error = "false";
  renderShortcuts();
  $("shortcut-dialog").showModal();
  $("shortcut-close").focus();
  return true;
}
function closeShortcuts(restore = true) {
  recordingShortcut = null;
  if (!$("shortcut-dialog").open) return;
  $("shortcut-dialog").close();
  if (restore) returnFocus(shortcutReturnFocus);
}
function renderShortcuts() {
  const list = $("shortcut-list");
  list.textContent = "";
  for (const command of getCommands().filter((item) => Object.hasOwn(SHORTCUT_DEFAULTS, item.id))) {
    const row = el("div", "shortcut-row");
    row.append(el("span", "shortcut-name", command.label));
    const record = el("button", "shortcut-binding", recordingShortcut === command.id ? "Press keys…" : formatShortcut(shortcutBindings[command.id]));
    record.type = "button";
    record.setAttribute("id", "shortcut-record-" + command.id);
    record.setAttribute("aria-label", "Record shortcut for " + command.label);
    record.setAttribute("aria-pressed", String(recordingShortcut === command.id));
    record.addEventListener("click", () => startShortcutRecording(command.id));
    const reset = el("button", "icon-button shortcut-reset-one");
    reset.type = "button";
    reset.append(icon("loop"));
    reset.setAttribute("aria-label", "Reset shortcut for " + command.label);
    reset.title = "Restore default";
    reset.addEventListener("click", () => finishShortcutChange(command.id, SHORTCUT_DEFAULTS[command.id]));
    row.append(record, reset);
    list.append(row);
  }
}
function startShortcutRecording(id) {
  if (!Object.hasOwn(SHORTCUT_DEFAULTS, id)) return;
  recordingShortcut = id;
  $("shortcut-feedback").textContent = "Press a shortcut. Escape cancels; Delete or Backspace disables.";
  $("shortcut-feedback").dataset.error = "false";
  renderShortcuts();
  $("shortcut-record-" + id).focus();
}
function finishShortcutChange(id, binding) {
  const result = setShortcutBinding(id, binding);
  $("shortcut-feedback").dataset.error = String(!result.ok);
  if (!result.ok) { $("shortcut-feedback").textContent = result.error; return; }
  recordingShortcut = null;
  $("shortcut-feedback").textContent = result.persisted ? "Shortcut saved in this browser."
    : "Shortcut works for this page. Browser storage is unavailable.";
  renderShortcuts();
  $("shortcut-record-" + id).focus();
}
function recordShortcut(event) {
  if (event.isComposing || event.keyCode === 229 || event.repeat || event.defaultPrevented || event.getModifierState?.("AltGraph")) return;
  if (event.key === "Escape" || event.key === "Tab") {
    if (event.key === "Escape") event.preventDefault();
    const id = recordingShortcut;
    recordingShortcut = null;
    $("shortcut-feedback").textContent = "Recording cancelled.";
    renderShortcuts();
    $("shortcut-record-" + id).focus();
    return;
  }
  if (["Shift", "Control", "Meta", "Alt"].includes(event.key)) return;
  event.preventDefault();
  if (["Delete", "Backspace"].includes(event.key) && !event.metaKey && !event.ctrlKey && !event.altKey && !event.shiftKey) {
    finishShortcutChange(recordingShortcut, null);
    return;
  }
  const binding = shortcutFromEvent(event);
  if (binding) finishShortcutChange(recordingShortcut, binding);
  else {
    $("shortcut-feedback").dataset.error = "true";
    $("shortcut-feedback").textContent = "Use Ctrl or ⌘ together with a letter or punctuation key.";
  }
}
$("shortcuts-btn").addEventListener("click", openShortcuts);
$("shortcut-close").addEventListener("click", () => closeShortcuts());
$("shortcut-reset").addEventListener("click", () => {
  recordingShortcut = null;
  shortcutBindings = { ...SHORTCUT_DEFAULTS };
  const persisted = writePreference("miniloop_shortcuts", JSON.stringify(shortcutBindings));
  updateShortcutHints();
  renderShortcuts();
  $("shortcut-feedback").dataset.error = "false";
  $("shortcut-feedback").textContent = persisted ? "Default shortcuts restored." : "Defaults restored for this page; browser storage is unavailable.";
});

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
  const sid = currentSid;
  try {
    const payload = await api("/sessions/" + encodeURIComponent(sid) + "/goal");
    if (sid !== currentSid) return;
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
async function loadWorkflows() {
  if (!currentSid) return;
  const list = $("wf-list");
  list.textContent = "";
  $("wf-detail").textContent = "";
  let payload;
  try { payload = await api("/sessions/" + encodeURIComponent(currentSid) + "/workflows"); }
  catch (err) { list.append(el("li", "", "workflows: " + err.message)); return; }
  $("wf-disabled").hidden = payload.enabled !== false;
  $("wf-launch").hidden = payload.enabled === false;
  if (payload.enabled === false) return;
  if (!(payload.runs || []).length) {
    list.append(el("li", "muted", "No workflow runs in this session yet."));
  }
  for (const run of payload.runs || []) {
    const item = el("li");
    const grow = el("span", "grow",
      run.workflow_name + " · " + run.run_id + " · attempts " + run.attempts_used);
    const badge = el("span", "badge", run.status);
    badge.dataset.a = run.status === "RUNNING" ? "running" : "idle";
    item.append(grow, badge);
    const inspect = el("button", "sec", "Detail");
    inspect.addEventListener("click", () => loadWorkflowDetail(run.run_id));
    item.append(inspect);
    // Terminal statuses per workflows/models.py RunStatus.is_terminal.
    if (!["COMPLETED", "FAILED", "CANCELLED", "REJECTED"].includes(run.status)) {
      const cancel = el("button", "sec danger", "Cancel");
      cancel.addEventListener("click", async () => {
        try {
          await api("/sessions/" + encodeURIComponent(currentSid) +
            "/workflows/" + encodeURIComponent(run.run_id) + "/cancel", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({}),
          });
        } catch (err) { alertRow("workflow cancel failed: " + err.message); }
        loadWorkflows();
      });
      item.append(cancel);
    }
    list.append(item);
  }
}

async function loadWorkflowDetail(runId) {
  const detail = $("wf-detail");
  detail.textContent = "";
  let run;
  try {
    run = await api("/sessions/" + encodeURIComponent(currentSid) +
      "/workflows/" + encodeURIComponent(runId));
  } catch (err) { detail.textContent = "detail: " + err.message; return; }
  const head = el("p", "", run.workflow_name + " (rev " + run.definition_revision +
    ") · " + run.status + (run.error ? " · error: " + run.error : "") +
    (run.cancel_reason ? " · " + run.cancel_reason : ""));
  detail.append(head);
  const nodes = el("ul", "rows");
  for (const node of run.nodes || []) {
    nodes.append(el("li", "",
      node.node_id + " · " + node.status +
      " · attempts " + (node.attempt_ids || []).length +
      (node.error ? " · " + node.error : "")));
  }
  detail.append(nodes);
  if (run.result !== null && run.result !== undefined) {
    const result = el("pre", "", JSON.stringify(run.result, null, 2).slice(0, 2000));
    detail.append(el("p", "muted", "Result"), result);
  }
}

$("wf-refresh").addEventListener("click", loadWorkflows);
$("wf-launch-btn").addEventListener("click", async () => {
  let definition, args;
  try {
    definition = JSON.parse($("wf-def").value);
    args = JSON.parse($("wf-args").value || "{}");
  } catch (err) { alertRow("workflow launch: invalid JSON: " + err.message); return; }
  try {
    const result = await api("/sessions/" + encodeURIComponent(currentSid) + "/workflows", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ definition, args }),
    });
    ledgerRow("ref", "·", "workflow", result.workflow_name + " " + result.run_id +
      " " + result.status, 0);
  } catch (err) { alertRow("workflow launch failed: " + err.message); }
  loadWorkflows();
});

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
  const sid = currentSid;
  $("ps-draft").textContent = "capturing…";
  $("ps-commit").hidden = true;
  try {
    const draft = await api("/sessions/" + encodeURIComponent(sid) +
      "/personal-skills/preview", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name, focus: $("ps-focus").value.trim() }),
    });
    if (sid !== currentSid) return;
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
$("improve-suggest").addEventListener("click", async () => {
  const list = $("improve-suggestions");
  list.textContent = "";
  let payload;
  try { payload = await api("/self-audit/suggestions"); }
  catch (err) { list.append(el("li", "", "suggestions: " + err.message)); return; }
  if (!(payload.suggestions || []).length) {
    list.append(el("li", "muted", "No recurring problems in the ledgers — nothing to suggest."));
  }
  for (const s of payload.suggestions || []) {
    const item = el("li");
    const grow = el("span", "grow", "[" + s.source + "] " + s.problem);
    // Suggestion is not authorization: clicking only fills the objective
    // box; the human still reviews, edits, and submits.
    const use = el("button", "sec", "Use");
    use.addEventListener("click", () => { $("improve-objective").value = s.objective; });
    item.append(grow, use);
    list.append(item);
  }
});

async function loadImprovementLineage() {
  const list = $("improve-lineage");
  list.textContent = "";
  let payload;
  try { payload = await api("/improvements"); }
  catch (err) { list.append(el("li", "", "lineage: " + err.message)); return; }
  for (const p of (payload.proposals || []).slice(0, 20)) {
    const flags = (p.touches_verifiers && p.touches_verifiers.length)
      ? " · TOUCHES VERIFIERS" : "";
    const item = el("li");
    item.append(el("span", "grow",
      p.proposal_id + (p.parent_id ? " ← " + p.parent_id : "") +
      " · " + (p.verified ? "verified" : "unverified") + flags +
      " · " + String(p.objective || "").slice(0, 80)));
    list.append(item);
  }
}

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
      // The self-weakening tell rides ahead of everything else: a proposal
      // that changed the acceptance instruments may have passed BECAUSE of
      // that change (see self_improve.py).
      (proposal.touches_verifiers && proposal.touches_verifiers.length
        ? " · TOUCHES VERIFIERS: " + proposal.touches_verifiers.join(", ")
        : "") +
      " · branch " + proposal.branch + "\n\n" +
      (proposal.diff_stat || "(no diff)") + "\n\n" +
      (proposal.summary || "") + "\n\n" + proposal.next;
    loadImprovementLineage();
  } catch (err) { result.textContent = "proposal failed: " + err.message; }
});

// ---- self-audit (global, R3) --------------------------------------------
$("audit-btn").addEventListener("click", async () => {
  $("settings-dialog").close();
  if (mobileViewport.matches) setSidebarOpen(false);
  showPane("audit-pane");
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
  $("session-actions").open = false;
  const sid = currentSid;
  const sure = window.confirm(
    "Delete session " + currentSid + "? The workspace is removed; recorded " +
    "trajectories are retained (the owner can still read them).");
  if (!sure) return;
  try {
    await api("/sessions/" + encodeURIComponent(sid), { method: "DELETE" });
    if (sid === currentSid) clearSession();
  } catch (err) { alertRow("delete failed: " + err.message); }
  loadSessions();
});

// ---- boot ---------------------------------------------------------------
setTheme(readPreference("miniloop_theme"));
setSidebarOpen(!mobileViewport.matches);
updateComposer();
updateNavigation();
updateShortcutHints();
loadHealth();
loadSessions();
setInterval(loadSessions, 5000);
setInterval(loadHealth, 30000);
setInterval(refreshApprovals, 7000);

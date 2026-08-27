/* Dependency-free regressions for the real app.js. This DOM double does not
   model layout; CSS and native browser behavior still need browser QA. */
const assert = require("node:assert/strict");
const { readFileSync } = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const { test } = require("node:test");
const uiRoot = path.join(__dirname, "..", "mini_loop", "webui");
const html = readFileSync(path.join(uiRoot, "index.html"), "utf8");
const script = readFileSync(path.join(uiRoot, "app.js"), "utf8");

class Element {
  constructor(tag, document) {
    this.tagName = tag.toUpperCase(); this.document = document;
    this.children = []; this.dataset = {}; this.attributes = {}; this.listeners = {};
    this.style = {}; this.className = ""; this.value = ""; this._text = "";
    this.hidden = false; this.disabled = false; this.open = false; this.inert = false;
    this.scrollTop = 0; this.scrollHeight = 100; this.clientHeight = 400;
  }
  get childNodes() { return this.children; }
  get textContent() { return this._text + this.children.map((node) => node.textContent).join(""); }
  set textContent(value) {
    for (const child of this.children) child.parent = null;
    this.children = []; this._text = String(value);
  }
  append(...nodes) {
    for (const node of nodes) { node.remove(); node.parent = this; this.children.push(node); }
  }
  remove() {
    if (this.parent) this.parent.children = this.parent.children.filter((node) => node !== this);
    this.parent = null;
  }
  replaceChildren(...nodes) { this.textContent = ""; this.append(...nodes); }
  setAttribute(key, value) {
    this.attributes[key] = String(value);
    if (key === "id") { this.id = value; this.document.nodes.set(value, this); }
    if (key === "class") this.className = value;
    if (key === "value") this.value = value;
    if (["hidden", "disabled", "open"].includes(key)) this[key] = true;
    if (key.startsWith("data-")) this.dataset[key.slice(5).replace(/-([a-z])/g, (_, c) => c.toUpperCase())] = value;
  }
  getAttribute(key) { return this.attributes[key] ?? null; }
  removeAttribute(key) { delete this.attributes[key]; }
  get classList() {
    const toggle = (name, force) => {
      const classes = new Set(this.className.split(/\s+/).filter(Boolean));
      const enabled = force === undefined ? !classes.has(name) : force;
      if (enabled) classes.add(name); else classes.delete(name);
      this.className = [...classes].join(" ");
    };
    return { toggle, add: (name) => toggle(name, true), remove: (name) => toggle(name, false) };
  }
  contains(node) { return node === this || this.children.some((child) => child.contains(node)); }
  matches(selector) {
    const match = selector.trim().match(/^([\w-]+)?(?:\.([\w-]+))?(?:\[([\w-]+)(?:="([^"]*)")?\])?$/);
    if (!match) throw new Error("Unsupported test selector: " + selector);
    const [, tag, cls, attr, value] = match;
    return (!tag || this.tagName === tag.toUpperCase()) &&
      (!cls || this.className.split(/\s+/).includes(cls)) &&
      (!attr || (attr === "open" ? this.open : this.getAttribute(attr) !== null) &&
        (value === undefined || this.getAttribute(attr) === value));
  }
  querySelectorAll(selector) {
    const selectors = selector.split(",");
    const result = [];
    const walk = (parent) => {
      for (const child of parent.children) {
        if (selectors.some((part) => child.matches(part))) result.push(child);
        walk(child);
      }
    };
    walk(this);
    return result;
  }
  getClientRects() {
    for (let node = this; node; node = node.parent) if (node.hidden) return [];
    return [{}];
  }
  focus() { this.document.activeElement = this; }
  showModal() { this.open = true; }
  close() { this.open = false; }
  addEventListener(type, listener) { (this.listeners[type] ||= []).push(listener); }
  async emit(type, event = {}) {
    event.preventDefault ||= () => { event.prevented = true; };
    await Promise.all((this.listeners[type] || []).map((listener) => listener(event)));
  }
}

function makeDocument() {
  const doc = { nodes: new Map() };
  const root = new Element("document", doc);
  const stack = [root];
  const tags = /<(\/?)([a-z][a-z0-9-]*)([^>]*)>/gi;
  const voidTags = new Set(["meta", "link", "input", "br", "hr", "img"]);
  for (const match of html.matchAll(tags)) {
    const [, closing, tag, attrs] = match;
    if (closing) { stack.pop(); continue; }
    const node = new Element(tag, doc);
    for (const attr of attrs.matchAll(/([\w:-]+)(?:\s*=\s*"([^"]*)")?/g)) {
      node.setAttribute(attr[1], attr[2] || "");
    }
    stack[stack.length - 1].append(node);
    if (!voidTags.has(tag)) stack.push(node);
  }
  for (const select of root.querySelectorAll("select")) {
    select.value = select.querySelectorAll("option")[0]?.value || "";
  }
  doc.documentElement = root.querySelectorAll("html")[0];
  doc.body = root.querySelectorAll("body")[0];
  doc.activeElement = doc.body;
  doc.getElementById = (id) => doc.nodes.get(id) || null;
  doc.createElement = (tag) => new Element(tag, doc);
  doc.createElementNS = (_, tag) => new Element(tag, doc);
  doc.querySelectorAll = (selector) => root.querySelectorAll(selector);
  doc.querySelector = (selector) => doc.querySelectorAll(selector)[0] || null;
  doc.listeners = {};
  doc.addEventListener = Element.prototype.addEventListener;
  return doc;
}

async function app(options = {}) {
  const document = makeDocument();
  const requests = [], streams = [], sessions = [];
  const preferences = new Map();
  const response = (body, status = 200) => ({ ok: status < 400, status,
    statusText: "fixture", json: async () => body, text: async () => JSON.stringify(body) });
  const context = vm.createContext({
    document, console, setInterval() {},
    window: { matchMedia: (query) => ({
      matches: query.includes("760") ? !!options.mobile : !!(options.mobile || options.overlay),
      addEventListener() {},
    }), confirm: () => true },
    localStorage: {
      getItem(key) { if (options.storageBlocked) throw new Error("blocked"); return preferences.get(key); },
      setItem(key, value) { if (options.storageBlocked) throw new Error("blocked"); preferences.set(key, value); },
    },
    EventSource: class {
      constructor(url) { this.url = url; this.listeners = {}; streams.push(this); }
      addEventListener(type, fn) { this.listeners[type] = fn; }
      close() { this.closed = true; }
    },
    fetch: async (url, opts = {}) => {
      requests.push({ url, ...opts });
      const override = await options.respond?.(url, opts);
      if (override) return response(override.body, override.status);
      if (url === "/healthz") return response({ model: "test-model", fake_llm: true, sessions: sessions.length });
      if (url.startsWith("/sessions?")) return response(sessions);
      if (url === "/sessions" && opts.method === "POST") {
        const body = JSON.parse(opts.body);
        const created = { id: "created-session", status: "idle", run_count: 0,
          permission_mode: body.mode, workspace: "/tmp/created-session" };
        sessions.push(created);
        return response(created);
      }
      if (/^\/sessions\/[^/]+$/.test(url)) return response(
        sessions.find((s) => s.id === url.split("/").pop()) || {
          id: url.split("/").pop(), permission_mode: "interactive", workspace: "/tmp/" + url.split("/").pop(),
        });
      if (url.endsWith("/goal")) return response({ goal: null });
      if (url.endsWith("/approvals")) return response({ approvals: [] });
      return response({ tasks: [], jobs: [], memories: [], messages: [] });
    },
  });
  const run = (code) => vm.runInContext(code, context);
  run(script);
  await new Promise(setImmediate);
  return { document, requests, streams, preferences, run, get: (id) => document.getElementById(id) };
}

test("light/dark toggle persists without making browser storage mandatory", async () => {
  const a = await app();
  await a.get("theme-toggle").emit("click");
  assert.equal(a.document.documentElement.dataset.theme, "dark");
  assert.equal(a.preferences.get("miniloop_theme"), "dark");
  assert.equal(a.get("theme-toggle").getAttribute("aria-label"), "Switch to light theme");
  const blocked = await app({ storageBlocked: true });
  await blocked.get("theme-toggle").emit("click");
  assert.equal(blocked.document.documentElement.dataset.theme, "dark");
});

test("mobile drawer closes by default and isolates the covered workspace", async () => {
  const a = await app({ mobile: true });
  assert.equal(a.get("rail").hidden, true);
  assert.equal(a.get("tools-btn").getAttribute("aria-label"), "Session tools");
  a.run("setSidebarOpen(true, true)");
  assert.equal(a.get("workspace").inert, true);
  assert.equal(a.get("sidebar-backdrop").hidden, false);
  assert.equal(a.document.activeElement, a.get("session-search"));
  await a.get("rail-close").emit("click");
  assert.equal(a.get("workspace").inert, false);
  assert.equal(a.document.activeElement, a.get("sidebar-toggle"));
});

test("Escape closes the active native dialog without closing its underlying drawer", async () => {
  const a = await app({ mobile: true });
  a.run("setSidebarOpen(true, true)");
  for (const [trigger, dialog] of [["settings-btn", "settings-dialog"], ["new-session", "new-form"]]) {
    await a.get(trigger).emit("click");
    assert.equal(a.get(dialog).open, true);
    const escape = { key: "Escape" };
    await Element.prototype.emit.call(a.document, "keydown", escape);
    assert.equal(a.get(dialog).open, false);
    assert.equal(escape.prevented, true);
    assert.equal(a.get("rail").hidden, false);
    assert.equal(a.get("workspace").inert, true);
  }
});

test("inspector preserves the ledger and returns focus when closed", async () => {
  const a = await app({ overlay: true });
  await a.run("selectSession('alpha')");
  await a.get("tools-btn").emit("click");
  assert.equal(a.get("ledger").hidden, false);
  assert.equal(a.get("tasks").hidden, false);
  assert.equal(a.get("conversation").inert, true);
  assert.equal(a.get("inspector").getAttribute("role"), "dialog");
  await a.get("inspector-close").emit("click");
  assert.equal(a.get("inspector").hidden, true);
  assert.equal(a.get("conversation").inert, false);
  assert.equal(a.document.activeElement, a.get("tools-btn"));
});

test("self-audit is reachable without a session; session-only panels are not", async () => {
  const a = await app();
  a.run("showPane('tasks')");
  assert.equal(a.get("inspector").hidden, true);
  await a.get("audit-btn").emit("click");
  assert.equal(a.get("audit-pane").hidden, false);
  assert.equal(a.get("tabs").hidden, true);
  assert.equal(a.get("session-view").hidden, true);
  assert.ok(a.requests.some((r) => r.url === "/self-audit"));
  await a.get("inspector-close").emit("click");
  assert.equal(a.document.activeElement, a.get("msg"));
});

test("first-message creation uses the displayed permission mode and sends once", async () => {
  const a = await app();
  a.get("mode-select").value = "readonly";
  await a.get("mode-select").emit("change");
  assert.ok(!a.requests.some((r) => r.url.includes("/null/")));
  a.get("msg").value = "A literal <b>draft</b>";
  await a.run("sendMessage()");
  const posts = a.requests.filter((r) => r.method === "POST");
  assert.deepEqual(posts.map((r) => r.url), ["/sessions", "/sessions/created-session/messages"]);
  assert.equal(JSON.parse(posts[0].body).mode, "readonly");
  assert.equal(JSON.parse(posts[1].body).message, "A literal <b>draft</b>");
  assert.equal(a.get("ledger").querySelectorAll("b").length, 0);
  assert.equal(a.get("msg").value, "");
});

test("creation errors keep the draft and show an error before any session exists", async () => {
  const a = await app({ respond: (url) => url === "/sessions"
    ? { status: 503, body: { detail: "temporarily unavailable" } } : undefined });
  a.get("msg").value = "Keep this draft";
  await a.run("sendMessage()");
  assert.equal(a.get("msg").value, "Keep this draft");
  assert.equal(a.get("ui-notice").hidden, false);
  assert.match(a.get("ui-notice-text").textContent, /temporarily unavailable/);
  assert.equal(a.get("send-btn").disabled, false);
});

test("double-send during session creation cannot create two sessions", async () => {
  let release;
  const pending = new Promise((resolve) => { release = resolve; });
  const a = await app({ respond: (url) => url === "/sessions" ? pending : undefined });
  a.get("msg").value = "Once only";
  const first = a.run("sendMessage()");
  await a.run("sendMessage()");
  assert.equal(a.requests.filter((r) => r.url === "/sessions").length, 1);
  release({ body: { id: "created-session" } });
  await first;
  assert.equal(a.requests.filter((r) => r.url.endsWith("/messages")).length, 1);
});

test("busy sends still become steering, and a pending turn does not lock the composer", async () => {
  let release, attempts = 0;
  const pending = new Promise((resolve) => { release = resolve; });
  const a = await app({ respond: (url) => {
    if (!url.endsWith("/messages")) return;
    return ++attempts === 1 ? pending : { status: 409, body: { detail: "already running a turn" } };
  } });
  await a.run("selectSession('alpha')");
  a.get("msg").value = "First task";
  const first = a.run("sendMessage()");
  a.get("msg").value = "Steer this task";
  a.run("updateComposer()");
  assert.equal(a.get("send-btn").disabled, false);
  await a.run("sendMessage()");
  const steer = a.requests.find((r) => r.url === "/sessions/alpha/steer");
  assert.equal(JSON.parse(steer.body).message, "Steer this task");
  release({ body: {} });
  await first;
});

test("IME confirmation does not send; Ctrl+Enter does", async () => {
  const a = await app();
  await a.run("selectSession('alpha')");
  a.get("msg").value = "输入法";
  await a.get("msg").emit("keydown", { key: "Enter", ctrlKey: true, isComposing: true });
  assert.equal(a.requests.filter((r) => r.url.endsWith("/messages")).length, 0);
  await a.get("msg").emit("keydown", { key: "Enter", ctrlKey: true, isComposing: false });
  assert.equal(a.requests.filter((r) => r.url.endsWith("/messages")).length, 1);
});

test("final text replaces the ephemeral streaming row without leaving blank bubbles", async () => {
  const a = await app();
  a.run("onEvent({type:'stream_start', stream_id:'s'}); onEvent({type:'assistant_delta', stream_id:'s', text:'Draft'});");
  a.run("onEvent({type:'assistant_text', phase:'final_answer', text:'Final answer'});");
  assert.equal(a.get("ledger").children.length, 1);
  assert.equal(a.get("ledger").children[0].dataset.phase, "final_answer");
  assert.match(a.get("ledger").textContent, /Final answer/);
  assert.doesNotMatch(a.get("ledger").textContent, /Draft/);
});

test("tool errors expand the disclosure and retain attacker-controlled output as text", async () => {
  const a = await app();
  a.run("onEvent({type:'tool_use', name:'bash', span_id:'t', input:{command:'test'}})");
  const row = a.get("ledger").children[0];
  assert.equal(row.tagName, "DETAILS");
  assert.equal(row.open, false);
  assert.match(row.textContent, /Requested/); // tool_use precedes permission, not execution
  a.run("onEvent({type:'tool_result', span_id:'t', denied:true, output:'<img src=x onerror=alert(1)>', duration_ms:12})");
  assert.equal(row.open, true);
  assert.equal(row.dataset.error, "1");
  assert.match(row.textContent, /<img src=x/);
  assert.equal(row.querySelectorAll("img").length, 0);
});

test("diagnostic events are inspectable inside one quiet disclosure", async () => {
  const a = await app();
  a.run("onEvent({type:'status', status:'running'}); onEvent({type:'system_prompt', text:'<script>not markup</script>'});");
  assert.equal(a.get("ledger").children.length, 1);
  assert.match(a.get("ledger").textContent, /2 events/);
  assert.match(a.get("ledger").textContent, /not markup/);
  assert.equal(a.get("ledger").querySelectorAll("script").length, 0);
  assert.equal(a.get("cancel-btn").disabled, false);
});

test("session rows filter by real workspace data and preserve keyboard focus on refresh", async () => {
  const a = await app();
  a.run("sessionsCache = [{id:'alpha', workspace:'/tmp/project-a', status:'idle', run_count:0}, {id:'beta', workspace:'/tmp/project-b', status:'running'}]; renderSessions()");
  const button = a.get("session-list").querySelectorAll("button")[0];
  button.focus();
  a.run("renderSessions()");
  assert.equal(a.document.activeElement, button);
  a.run("sessionsCache[0].run_count = 1; renderSessions()");
  assert.equal(a.document.activeElement.dataset.sid, "alpha");
  a.get("session-search").value = "project-b";
  await a.get("session-search").emit("input");
  assert.equal(a.get("session-list").querySelectorAll("button").length, 1);
  assert.equal(a.get("session-list").querySelectorAll("button")[0].dataset.sid, "beta");
});

test("late session details cannot overwrite the newly selected workspace or mode", async () => {
  let release;
  const pending = new Promise((resolve) => { release = resolve; });
  const a = await app({ respond: (url) => url === "/sessions/slow" ? pending : undefined });
  const slow = a.run("selectSession('slow')");
  await a.run("selectSession('fast')");
  release({ body: { permission_mode: "auto", workspace: "/wrong/workspace" } });
  await slow;
  assert.equal(a.get("workspace-path").title, "/tmp/fast");
  assert.equal(a.get("mode-select").value, "interactive");
  assert.equal(a.streams[0].closed, true);
});

test("late approvals from a previous session do not surface on the current session", async () => {
  let release;
  const pending = new Promise((resolve) => { release = resolve; });
  const a = await app({ respond: (url) => url === "/sessions/slow/approvals" ? pending : undefined });
  a.run("currentSid = 'slow'");
  const slow = a.run("refreshApprovals()");
  a.run("currentSid = 'fast'");
  await a.run("refreshApprovals()");
  release({ body: { approvals: [{ approval_id: "old", tool: "bash" }] } });
  await slow;
  assert.equal(a.get("approvals").hidden, true);
  assert.equal(a.get("approval-list").children.length, 0);
});

test("both approval controls resolve only the displayed session and approval", async () => {
  for (const decision of ["deny", "allow"]) {
    const a = await app({ respond: (url) => url.endsWith("/approvals")
      ? { body: { approvals: [{ approval_id: "approval-1", tool: "preview" }] } } : undefined });
    a.run("currentSid = 'alpha'");
    await a.run("refreshApprovals()");
    const label = decision === "allow" ? "Allow" : "Deny";
    const button = a.get("approval-list").querySelectorAll("button").find((node) => node.textContent === label);
    await button.emit("click");
    const request = a.requests.find((r) => r.method === "POST");
    assert.equal(request.url, "/sessions/alpha/approvals/approval-1");
    assert.deepEqual(JSON.parse(request.body), { decision });
  }
});

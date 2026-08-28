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
  get id() { return this.attributes.id || ""; }
  set id(value) { this.attributes.id = String(value); this.document.nodes.set(String(value), this); }
  get childNodes() { return this.children; }
  get isConnected() { return !!this.document.documentElement?.contains(this); }
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
  getAttribute(key) {
    if (key.startsWith("data-")) {
      const name = key.slice(5).replace(/-([a-z])/g, (_, c) => c.toUpperCase());
      return this.dataset[name] ?? null;
    }
    return this.attributes[key] ?? null;
  }
  removeAttribute(key) {
    delete this.attributes[key];
    if (["hidden", "disabled", "open"].includes(key)) this[key] = false;
  }
  get classList() {
    const toggle = (name, force) => {
      const classes = new Set(this.className.split(/\s+/).filter(Boolean));
      const enabled = force === undefined ? !classes.has(name) : force;
      if (enabled) classes.add(name); else classes.delete(name);
      this.className = [...classes].join(" ");
      return enabled;
    };
    return { toggle, add: (name) => toggle(name, true), remove: (name) => toggle(name, false),
      contains: (name) => this.className.split(/\s+/).includes(name) };
  }
  contains(node) { return node === this || this.children.some((child) => child.contains(node)); }
  matches(selector) {
    if (selector.includes(",")) return selector.split(",").some((part) => this.matches(part));
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
  querySelector(selector) { return this.querySelectorAll(selector)[0] || null; }
  closest(selector) {
    for (let node = this; node; node = node.parent) if (node.matches(selector)) return node;
    return null;
  }
  getClientRects() {
    for (let node = this; node; node = node.parent) if (node.hidden) return [];
    return [{}];
  }
  focus() { this.document.activeElement = this; }
  click() { if (!this.disabled) return this.emit("click"); }
  scrollIntoView(options) { this.lastScrollIntoView = options; }
  showModal() { this._previousFocus = this.document.activeElement; this.open = true; }
  close() { this.open = false; if (this._previousFocus?.isConnected) this._previousFocus.focus(); }
  addEventListener(type, listener) { (this.listeners[type] ||= []).push(listener); }
  async emit(type, event = {}) {
    event.type ||= type;
    event.target ||= this.document ? this : this.activeElement;
    event.currentTarget = this;
    event.preventDefault ||= () => { event.prevented = true; event.defaultPrevented = true; };
    event.stopPropagation ||= () => { event.cancelBubble = true; };
    await Promise.all((this.listeners[type] || []).map((listener) => listener(event)));
    return event;
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
  doc.emit = Element.prototype.emit;
  return doc;
}

async function app(options = {}) {
  const document = makeDocument();
  const requests = [], streams = [], sessions = [];
  const preferences = new Map(Object.entries(options.preferences || {}));
  const navigator = { platform: options.platform || "MacIntel" };
  const response = (body, status = 200) => ({ ok: status < 400, status,
    statusText: "fixture", json: async () => body, text: async () => JSON.stringify(body) });
  const context = vm.createContext({
    document, navigator, console, setInterval() {},
    window: { navigator, matchMedia: (query) => ({
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

// Dispatch keyboard events through their ordinary bubbling path. Real focus
// trapping and native platform accelerators remain browser acceptance gates.
async function keydown(a, target, key, modifiers = {}) {
  const event = { key, target, ...modifiers };
  for (let node = target; node; node = node.parent) {
    await node.emit("keydown", event);
    if (event.cancelBubble) return event;
  }
  await a.document.emit("keydown", event);
  await new Promise(setImmediate);
  return event;
}

function paletteSelection(a) {
  const list = a.get("command-list");
  const activeId = a.get("command-search").getAttribute("aria-activedescendant");
  if (activeId) return a.document.getElementById(activeId);
  return list.querySelectorAll('[aria-selected="true"]')[0] ||
    (list.contains(a.document.activeElement) ? a.document.activeElement : null);
}

function commandIdentity(node) { return node?.dataset.command || node?.id || node?.textContent; }

function historyOf(a) { return JSON.parse(a.run("JSON.stringify(visitHistory)")); }

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
  await a.run("runCommand('tools.toggle')");
  a.get("tasks-refresh").focus();
  await a.run("runCommand('tools.toggle')");
  assert.equal(a.get("inspector").hidden, true);
  assert.equal(a.document.activeElement, a.get("tools-btn"), "command close must not strand focus inside the hidden inspector");
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


test("command palette filters available actions without inventing sessions or sending tasks", async () => {
  const a = await app();
  await a.get("commands-btn").click();
  assert.equal(a.get("command-dialog").open, true);
  assert.equal(a.document.activeElement, a.get("command-search"));
  assert.ok(a.run("getCommands().find((command) => command.id === 'pane.tasks').disabledReason"));
  await a.run("runCommand('pane.tasks')");
  assert.equal(a.get("inspector").hidden, true);
  a.run("openCommandPalette()");
  a.get("command-search").value = "settings";
  await a.get("command-search").emit("input");
  assert.match(a.get("command-list").textContent, /settings/i);
  assert.doesNotMatch(a.get("command-list").textContent, /fork session/i);
  a.get("command-search").value = "a-command-that-does-not-exist";
  await a.get("command-search").emit("input");
  assert.equal(a.get("command-list").querySelectorAll("button").length, 0);
  await a.run("runCommand('session.new')");
  await new Promise(setImmediate);
  assert.equal(a.get("new-form").open, true);
  assert.equal(a.requests.filter((request) => request.method === "POST").length, 0);
});

test("command palette arrow selection, Enter dispatch and Escape preserve focus and modal ownership", async () => {
  const a = await app();
  a.get("commands-btn").focus();
  a.run("openCommandPalette()");
  const first = paletteSelection(a);
  assert.ok(first, "the active palette item is exposed to assistive technology");
  await keydown(a, a.get("command-search"), "ArrowDown");
  const second = paletteSelection(a);
  assert.ok(second);
  assert.notEqual(commandIdentity(second), commandIdentity(first));
  await keydown(a, a.get("command-search"), "ArrowUp");
  assert.equal(commandIdentity(paletteSelection(a)), commandIdentity(first));
  await keydown(a, a.get("command-search"), "Escape");
  assert.equal(a.get("command-dialog").open, false);
  assert.equal(a.document.activeElement, a.get("commands-btn"));

  a.run("openCommandPalette()");
  a.get("command-search").value = a.run("getCommands().find((command) => command.id === 'session.new').label");
  await a.get("command-search").emit("input");
  await keydown(a, a.get("command-search"), "Enter");
  assert.equal(a.get("command-dialog").open, false);
  assert.equal(a.get("new-form").open, true);
  a.run("openCommandPalette()");
  assert.equal(a.get("command-dialog").open, false, "a palette cannot stack over another modal");
  assert.equal(a.requests.filter((request) => request.method === "POST").length, 0);
});

test("session back and forward reuse selection without appending visits or reopening the current stream", async () => {
  const a = await app();
  await a.run("selectSession('alpha')");
  await a.run("selectSession('beta')");
  await a.run("selectSession('gamma')");
  const streamCount = a.streams.length;
  await a.run("selectSession('gamma')");
  assert.equal(a.streams.length, streamCount);
  assert.deepEqual(historyOf(a), ["alpha", "beta", "gamma"]);
  await a.run("navigateSession(-1)");
  assert.equal(a.run("currentSid"), "beta");
  assert.deepEqual(historyOf(a), ["alpha", "beta", "gamma"]);
  await a.run("navigateSession(1)");
  assert.equal(a.run("currentSid"), "gamma");
  await a.run("navigateSession(-1)");
  await a.run("selectSession('delta')");
  assert.deepEqual(historyOf(a), ["alpha", "beta", "delta"]);
  assert.equal(a.run("visitIndex"), 2);
  await a.run("navigateSession(1)");
  assert.equal(a.run("currentSid"), "delta");
});

test("visit history is bounded while retaining a usable back path and the current session", async () => {
  const a = await app();
  for (let index = 0; index < 112; index++) await a.run("selectSession('visit-" + index + "')");
  assert.equal(historyOf(a).length, 100);
  assert.equal(historyOf(a)[0], "visit-12");
  assert.equal(historyOf(a).at(-1), "visit-111");
  assert.equal(a.run("visitIndex"), 99);
  await a.run("navigateSession(-1)");
  assert.equal(a.run("currentSid"), "visit-110");
  assert.equal(historyOf(a).length, 100);
});

test("forgetting a session removes all its visits and preserves navigation among survivors", async () => {
  const a = await app();
  for (const sid of ["alpha", "beta", "alpha", "gamma"]) await a.run("selectSession('" + sid + "')");
  a.run("forgetSession('alpha')");
  assert.deepEqual(historyOf(a), ["beta", "gamma"]);
  assert.equal(a.run("visitIndex"), 1);
  assert.equal(a.run("currentSid"), "gamma");
  await a.run("navigateSession(-1)");
  assert.equal(a.run("currentSid"), "beta");
});

test("session history forgets a missing target but never treats a transient failure as deletion", async () => {
  for (const failureStatus of [404, 503]) {
    let fail = false;
    const a = await app({ respond: (url) => fail && url === "/sessions/beta"
      ? { status: failureStatus, body: { detail: failureStatus === 404 ? "No session" : "temporarily unavailable" } }
      : undefined });
    for (const sid of ["alpha", "beta", "gamma"]) await a.run("selectSession('" + sid + "')");
    fail = true;
    await a.run("navigateSession(-1)");
    assert.notEqual(a.run("currentSid"), "beta", "failed navigation does not select an unavailable session");
    assert.equal(historyOf(a).includes("beta"), failureStatus !== 404);
  }
});

test("authentication changes clear session visits, streams and open commands but preserve shortcut preferences", async () => {
  const a = await app();
  await a.run("selectSession('alpha')");
  await a.run("selectSession('beta')");
  assert.equal(a.run("setShortcutBinding('composer.focus', 'Mod+Shift+K')").ok, true);
  a.run("openCommandPalette()");
  a.get("token").value = "another-owner";
  await a.get("token").emit("change");
  await new Promise(setImmediate);
  assert.equal(a.run("currentSid"), null);
  assert.deepEqual(historyOf(a), []);
  assert.equal(a.run("visitIndex"), -1);
  assert.equal(a.get("command-dialog").open, false);
  assert.equal(a.get("ledger").children.length, 0);
  assert.ok(a.streams.every((stream) => stream.closed));
  assert.equal(a.run("shortcutBindings['composer.focus']"), "Mod+Shift+K");

  a.run("openShortcuts()");
  a.get("token").value = "third-owner";
  await a.get("token").emit("change");
  assert.equal(a.get("shortcut-dialog").open, false);
  assert.equal(a.run("shortcutBindings['composer.focus']"), "Mod+Shift+K");
});

test("shortcut changes reject conflicts and send accelerators without replacing a valid override", async () => {
  const a = await app();
  assert.equal(a.run("setShortcutBinding('composer.focus', 'Mod+Shift+K')").ok, true);
  for (const binding of ["Mod+K", "Mod+C", "Mod+Enter", "Mod+Shift+Enter", "Mod+Alt+Enter",
    "Mod+Alt+Shift+Enter", "Mod+Shift+Alt+Enter", "K"]) {
    const result = a.run("setShortcutBinding('composer.focus', " + JSON.stringify(binding) + ")");
    assert.equal(result.ok, false, binding + " must remain unavailable");
    assert.ok(result.error);
    assert.equal(a.run("shortcutBindings['composer.focus']"), "Mod+Shift+K");
  }
  assert.ok(a.preferences.has("miniloop_shortcuts"));
  const restored = await app({ preferences: Object.fromEntries(a.preferences) });
  assert.equal(restored.run("shortcutBindings['composer.focus']"), "Mod+Shift+K");
  assert.equal(restored.run("shortcutBindings['palette.open']"), "Mod+K");
  await keydown(restored, restored.document.body, "K", { metaKey: true, shiftKey: true });
  assert.equal(restored.document.activeElement, restored.get("msg"));
});

test("shortcut recording supports cancel, assignment, disable and restoring defaults", async () => {
  const a = await app();
  a.run("openShortcuts(); startShortcutRecording('composer.focus')");
  await keydown(a, a.document.activeElement, "Escape");
  assert.equal(a.get("shortcut-dialog").open, true);
  assert.equal(a.run("shortcutBindings['composer.focus']"), null);
  a.run("startShortcutRecording('composer.focus')");
  await keydown(a, a.document.activeElement, "k", { metaKey: true });
  assert.equal(a.run("shortcutBindings['composer.focus']"), null, "recording cannot steal an occupied chord");
  await keydown(a, a.document.activeElement, "K", { metaKey: true, shiftKey: true });
  assert.equal(a.run("shortcutBindings['composer.focus']"), "Mod+Shift+K");
  a.get("msg").value = "Keep this draft until I explicitly send it";
  for (const modifiers of [{}, { shiftKey: true }, { altKey: true }, { altKey: true, shiftKey: true }]) {
    a.run("startShortcutRecording('composer.focus')");
    await keydown(a, a.document.activeElement, "Enter", { metaKey: true, ...modifiers });
    assert.equal(a.run("shortcutBindings['composer.focus']"), "Mod+Shift+K");
    assert.equal(a.get("msg").value, "Keep this draft until I explicitly send it");
    assert.equal(a.requests.filter((request) => request.method === "POST").length, 0);
  }
  a.run("startShortcutRecording('composer.focus')");
  await keydown(a, a.document.activeElement, "Delete");
  assert.equal(a.run("shortcutBindings['composer.focus']"), null);
  assert.equal(a.run("setShortcutBinding('palette.open', null)").ok, true);
  await a.get("shortcut-close").click();
  await keydown(a, a.document.body, "k", { metaKey: true });
  assert.equal(a.get("command-dialog").open, false);
  a.run("openShortcuts()");
  await a.get("shortcut-reset").click();
  assert.equal(a.run("shortcutBindings['palette.open']"), "Mod+K");
  assert.equal(a.run("shortcutBindings['session.back']"), "Mod+[");
  assert.equal(a.run("shortcutBindings['session.forward']"), "Mod+]");
  assert.equal(a.run("shortcutBindings['composer.focus']"), null);
  await a.get("shortcut-close").click();
  await keydown(a, a.document.body, "k", { metaKey: true });
  assert.equal(a.get("command-dialog").open, true);
});

test("shortcut dispatch and recording ignore IME and repeat events without sending a model task", async () => {
  const a = await app();
  for (const modifiers of [{ isComposing: true }, { repeat: true }, { keyCode: 229 }]) {
    await keydown(a, a.get("msg"), "k", { metaKey: true, ...modifiers });
    assert.equal(a.get("command-dialog").open, false);
  }
  await keydown(a, a.get("msg"), "k", { metaKey: true });
  assert.equal(a.get("command-dialog").open, true);
  await keydown(a, a.get("command-search"), "Escape");
  a.run("openShortcuts(); startShortcutRecording('composer.focus')");
  for (const modifiers of [{ isComposing: true }, { repeat: true }]) {
    await keydown(a, a.document.activeElement, "K", { metaKey: true, shiftKey: true, ...modifiers });
    assert.equal(a.run("shortcutBindings['composer.focus']"), null);
  }
  await keydown(a, a.document.activeElement, "Escape");
  assert.equal(a.requests.filter((request) => request.method === "POST").length, 0);
});

test("damaged shortcut preferences cannot crash startup or occupy protected keys", async () => {
  for (const serialized of ["{broken", "null", "[]", '"not an object"',
    '{"palette.open":"Mod+C","session.back":"Mod+K","unknown.action":"Mod+Shift+P"}']) {
    const a = await app({ preferences: { miniloop_shortcuts: serialized } });
    const bindings = JSON.parse(a.run("JSON.stringify(shortcutBindings)"));
    assert.equal(Object.values(bindings).includes("Mod+C"), false);
    assert.equal(Object.hasOwn(bindings, "unknown.action"), false);
    assert.ok(Object.values(bindings).filter((binding) => binding === "Mod+K").length <= 1);
    a.run("openCommandPalette()");
    assert.equal(a.get("command-dialog").open, true);
  }
  const blocked = await app({ storageBlocked: true });
  assert.equal(blocked.run("setShortcutBinding('composer.focus', 'Mod+Shift+K')").ok, true);
  assert.equal(blocked.run("shortcutBindings['composer.focus']"), "Mod+Shift+K");
});


test("stale navigation cannot block a new context or unlock its newer pending navigation", async () => {
  for (const change of ["selection", "authentication", "clear"]) {
    let checking = false, nextTarget = null, releaseOld, releaseNew;
    const oldPending = new Promise((resolve) => { releaseOld = resolve; });
    const newPending = new Promise((resolve) => { releaseNew = resolve; });
    const oldResponse = { body: { id: "alpha", permission_mode: "auto", workspace: "/stale-owner/workspace" } };
    const newResponse = () => ({ body: { id: nextTarget, permission_mode: "interactive", workspace: "/tmp/" + nextTarget } });
    const a = await app({ respond: (url) => {
      if (checking && url === "/sessions/alpha") return oldPending;
      if (nextTarget && url === "/sessions/" + nextTarget) return newPending;
    } });
    try {
      await a.run("selectSession('alpha')");
      await a.run("selectSession('beta')");
      checking = true;
      const oldNavigation = a.run("navigateSession(-1)");
      if (change === "selection") await a.run("selectSession('gamma')");
      else {
        if (change === "authentication") {
          a.get("token").value = "different-owner";
          await a.get("token").emit("change");
        } else a.run("clearSession()");
        assert.equal(a.run("currentSid"), null);
        assert.deepEqual(historyOf(a), []);
        await a.run("selectSession('delta')");
        await a.run("selectSession('epsilon')");
      }
      const selected = change === "selection" ? "gamma" : "epsilon";
      nextTarget = change === "selection" ? "beta" : "delta";
      assert.equal(a.get("session-back").disabled, false, change + " must allow navigation before the old request settles");
      const newNavigation = a.run("navigateSession(-1)");
      assert.equal(a.get("session-back").disabled, true);
      const visits = historyOf(a);
      const streamCount = a.streams.length;
      releaseOld(oldResponse);
      assert.equal(await oldNavigation, false);
      assert.equal(a.run("currentSid"), selected);
      assert.deepEqual(historyOf(a), visits);
      assert.equal(a.streams.length, streamCount);
      assert.notEqual(a.get("workspace-path").title, "/stale-owner/workspace");
      assert.equal(a.get("session-back").disabled, true, "the old finally cannot unlock a newer request");
      const requestCount = a.requests.filter((request) => request.url === "/sessions/" + nextTarget).length;
      const duplicate = a.run("navigateSession(-1)");
      await new Promise(setImmediate);
      assert.equal(a.requests.filter((request) => request.url === "/sessions/" + nextTarget).length, requestCount);
      releaseNew(newResponse());
      assert.equal(await newNavigation, true);
      assert.equal(await duplicate, false);
      assert.equal(a.run("currentSid"), nextTarget);
    } finally {
      // Keep a failed assertion from leaving fixture requests unresolved.
      releaseOld(oldResponse);
      releaseNew(newResponse());
    }
  }
});

test("palette session search renders remote text literally and selects the real session without running it", async () => {
  const session = { id: "alpha-session", workspace: "/tmp/project-<img src=x onerror=alert(1)>",
    status: "idle", permission_mode: "interactive", run_count: 0 };
  const a = await app({ respond: (url) => url.startsWith("/sessions?")
    ? { body: [session] } : url === "/sessions/alpha-session" ? { body: session } : undefined });
  a.run("openCommandPalette()");
  a.get("command-search").value = "project-<img";
  await a.get("command-search").emit("input");
  assert.match(a.get("command-list").textContent, /project-<img/);
  assert.equal(a.get("command-list").querySelectorAll("img").length, 0);
  const target = a.get("command-list").querySelectorAll("button")
    .find((button) => button.getAttribute("data-command-id") === "session.select.alpha-session");
  assert.ok(target);
  await target.click();
  await new Promise(setImmediate);
  assert.equal(a.run("currentSid"), session.id);
  assert.deepEqual(historyOf(a), [session.id]);
  assert.equal(a.get("command-dialog").open, false);
  assert.equal(a.get("workspace-path").title, session.workspace);
  assert.equal(a.requests.filter((request) => request.method === "POST").length, 0);
});

# Web UI

The `/ui` workspace presents the existing mini-loop REST/SSE APIs. It is a
single self-contained response assembled from `index.html`, `app.css`, and
`app.js`; no frontend build, remote assets, or new dependencies are required.
The classic console remains at `/`.

## Design reference

Reviewed on 2026-08-27 against
[DeepSeek Harness at `b150a551b8d465e31e418e1b2eaf5e79bbb7d28e`](https://github.com/deepseek-ai/deepseek-harness/tree/b150a551b8d465e31e418e1b2eaf5e79bbb7d28e).
The reference was its application UI, not the DeepSeek chat product or landing
page. Relevant upstream components:

- `packages/client/ui-sidebar/src/client/SidebarRoot.module.css`: neutral
  sidebar, outlined new-session action, quiet session rows, footer settings.
- `packages/client/ui-conversation/src/client/skeleton/ConversationRoot.module.css`
  and `InputBar.module.css`: centered conversation width, spacious rounded
  composer, permission and model information beside a circular send action.
- `packages/client/ui-conversation/src/client/chat/MessageItem.module.css`:
  right-aligned user bubbles and open assistant prose.
- `packages/client/ui-theme/src/styles/design-platform.css`: light/dark neutral
  surfaces, restrained blue accents, subtle borders.

This is an original HTML/CSS/JavaScript implementation of those visual
patterns. No upstream component code, branding, logo, plugin system, or
runtime dependency is incorporated.

On 2026-08-28, the UI also adopted Minke's global action palette, shortcut
recorder, and visited-session navigation patterns. The
[fixed-source comparison and adoption boundaries](MINKE_UI_ADOPTION.md)
compare Minke `c156b73` with DeepSeek `b150a55` and current DeepSeek `cd5ef81`.
Message outlines and general log export already exist upstream; Minke's
native file, PTY, browser, and remote-host capabilities are outside this change.

### Commands, shortcuts, and session navigation

| Entry | Default shortcut | Behavior |
| --- | --- | --- |
| Commands in the top bar | Ctrl / Command + K | Search grouped actions and fetched sessions; arrows select, Enter opens, Escape closes and restores focus. |
| Previous / Next session | Ctrl / Command + [ / ] | Move through sessions visited on this page, without creating a new visit. |
| Settings → Keyboard shortcuts, or Commands → Edit shortcuts | Unassigned | Record, disable, reset, and save bindings in this browser. |

Commands reuse existing controls and APIs, including the delete confirmation;
they never ask the model to execute an action name. Unavailable actions explain
why. Search only covers the currently fetched session list, not hidden or
unloaded sessions. Selecting the current session does not reopen its SSE stream.

Shortcuts accept Ctrl or Command with a supported key, optionally Shift/Alt.
Duplicate assignments and common browser/editing accelerators are rejected;
all Ctrl/Command + Enter variants remain reserved for the existing composer
send behavior. The three defaults above intentionally own K, [ and ] when
their key events reach this page; they do not operate the browser's URL history.
Other actions start unassigned. Recording ignores IME and repeats; Escape
cancels, Tab leaves recording, and unmodified Delete/Backspace disables a binding.
Invalid stored preferences fall back safely; unavailable storage keeps changes
usable for this page and reports that they were not saved.

Visit history is capped at 100 entries and is not persisted across reloads.
Selecting a different session after going back discards the forward branch.
Authentication changes and clearing the active session clear the history.
Navigation checks the target before replacing the visible conversation: 404
removes a missing visit, while transient failures keep it available to retry.
New selections invalidate pending navigation, so old responses cannot block or
replace a newer selection. No server-side history or transcript store is added.

## Interaction and boundaries

- Sending the first message creates a session with the displayed permission
  mode. New session also offers the existing optional system prompt. Creation
  failures leave the draft available; sending while busy still uses steering.
- Session rows are keyboard-operable buttons. Search filters the fetched
  session IDs and workspace paths (up to the existing 100-session API limit).
  Displayed IDs and activity are server data, not invented session titles.
- The conversation stays visible beside Session tools on wide screens. On
  smaller screens, the tools panel takes over the available area; Escape and
  the close button return to the conversation. The mobile sidebar is a drawer.
- Tool/model records and run metadata expand using native disclosures. Errors
  open the affected tool record; approvals remain explicit Allow/Deny actions.
  Final answers remain distinct from continuing commentary. The event ledger
  is still the existing SSE projection; the Transcript panel is the authority
  for recorded message history, including user messages across reloads, when a
  durable state store is configured. The default Null store has no saved epochs.
- Light is the initial appearance; the theme choice and existing API token
  preference are local to the browser. Storage being unavailable does not
  prevent use of the page. Model and workspace labels describe server state;
  they do not claim to be model-switching or host-directory-selection controls.
- Dynamic prose uses text nodes only. SVG paths are fixed local constants;
  there are no external scripts, fonts, images, markup sinks, or CSP changes.
  No feature bundle, approval permission, durable store, or auth default changes.

## Verification

Run the focused safety/API and interaction regression tests:

```sh
.venv/bin/python -m pytest -q tests/test_webui.py tests/test_webui_routes.py tests/test_console_safety.py
node --check mini_loop/webui/app.js
node --test tests/webui_dom.test.cjs
```

The dependency-free Node tests execute the real application script against a
small DOM test double; they do not validate CSS or replace browser checks.
Pytest runs them when Node is available and explicitly skips that check otherwise.
Before delivery, also inspect the real `/ui` served with its CSP at desktop and
mobile widths, both themes, and exercise create/send, disclosures, inspector
navigation, settings, and approvals using an isolated fake-model server. Real
provider/model quality is outside this UI acceptance gate.

### Acceptance record — 2026-08-28

| Gate | Result |
| --- | --- |
| `.venv/bin/python -m pytest -q` | 1,883 passed, 18 skipped, 24 subtests passed; 3 dependency deprecation warnings |
| Focused pytest command above | 26 passed; 1 dependency deprecation warning |
| `node --check mini_loop/webui/app.js` | Passed |
| `node --test tests/webui_dom.test.cjs` | 30 passed |
| `.venv/bin/python tools/verify_invariants.py` | 71 modules passed |
| `.venv/bin/python tools/verify_scans.py` | 19 scanning guards anchored |
| `.venv/bin/python tools/verify_guards.py -k webui` | The targeted missing-script mutation was caught |
| Static page checks | 117 unique IDs; all 96 literal JavaScript ID references and 13 label targets resolve; all 16 CSS variables are defined |
| Architecture and diff checks | Canonical README Mermaid unchanged; review baseline updated; `git diff --check` passed |

The new interactions were exercised against the CSP-protected `/ui` in the
in-app browser, using an isolated fake-model server and real temporary sessions.
Checks covered command search/Enter dispatch, session lookup, back/forward,
shortcut conflicts and reserved send keys, recording cancellation, disable/reset,
save across reload, and focus return after closing session tools. Existing
session creation and fake-model sending also worked. No browser error logs were
reported. Page, top-bar controls, and both new dialogs were checked at 375, 768,
1,024, and 1,440 pixels without horizontal overflow. Light and dark themes were
inspected; shortcut feedback and reset stay visible while the list scrolls.

The Node regressions additionally cover IME/repeat handling, malformed or
unavailable local storage, stale requests after authentication/selection changes,
100-entry history limits, forward-branch truncation, and 404 versus transient
navigation failures. These tests use a DOM double, not real input-method engines.
Real provider calls, native Safari/Firefox, and the full mutation-guard sweep
were not part of this gate. The backend API and runtime defaults are unchanged.

### Acceptance record — 2026-08-27

| Gate | Result |
| --- | --- |
| `.venv/bin/python -m pytest -q` | 1,883 passed, 18 skipped, 24 subtests passed; 3 dependency deprecation warnings |
| Focused pytest command above | 26 passed; 1 dependency deprecation warning |
| `node --check mini_loop/webui/app.js` | Passed |
| `node --test tests/webui_dom.test.cjs` | 17 passed |
| `.venv/bin/python tools/verify_invariants.py` | 71 modules passed |
| `.venv/bin/python tools/verify_scans.py` | 19 scanning guards anchored |
| `.venv/bin/python tools/verify_guards.py -k webui` | The targeted Web UI mutation was caught |
| `git diff --check` | Passed |

The served page was also checked in the Codex in-app browser at 375, 768,
1,024, and 1,440 pixels: no horizontal page overflow or clipped interactive
controls were found. Both themes, session search, first-message creation,
optional system prompts, tool disclosures, all nine inspector panels,
keyboard focus/Escape, and settings were exercised. The final reload and
keyboard checks produced no browser log errors.

Approval Allow/Deny and saved Transcript records were checked separately with
an isolated fake model, a no-op tool, and a temporary SQLite state store. The
default Null store's missing transcript is expected and is not concealed.
These checks made no real provider calls. Native Safari/Firefox, real-model
behavior, and the full mutation-guard sweep were not part of this UI gate.

# User-scoped skills and memory

Status: implementation contract for the second local-filesystem version.

## 1. Outcome

mini-loop exposes two explicit knowledge sources:

- `agent`: deployment-managed, read-only skills shared by the manager;
- `user`: skills and durable memory resolved from the authenticated session
  owner.

The owner is bound before an `Agent` is constructed. A model cannot select or
override that owner through a tool argument. Source labels are model-visible
provenance; they do not grant authority. Tool catalogues, hooks, permission
mode, and the sandbox remain the enforcement boundary.

## 2. Baseline gap addressed by V1

Before V1, `SkillLoader` was one manager-wide directory with one namespace, so
every user saw the same catalogue and a skill had no source provenance. Memory
had an owner-filtered `ScopedMemory` view, but all owners shared one directory
and a filename derived only from the memory name. Two owners writing the same
name could therefore overwrite one another before the read filter ran.

Ownership was also bound too late on several paths: the HTTP route set it after
`SessionManager.create()` had built the agent and recorded the session; restore
set it after agent construction; teammates did not inherit it at all.

## 3. Versioned scope

V1 adds:

1. `SessionManager.create(owner=...)`, restore-before-build owner binding, and
   teammate inheritance.
2. A deployment-configured user-resource root. Each owner maps to a digest-only
   directory containing `skills/` and `memory/`.
3. A layered skill catalogue with `agent` and `user` sources, a shared bounded
   catalogue budget, explicit source selection, and ambiguity errors for
   cross-source name collisions.
4. Per-owner `MemoryStore` instances and lifecycle locks when the user-resource
   root is enabled. The legacy shared store remains the compatibility fallback.
5. Owner-derived memory filenames even in the legacy shared store, so equal
   names cannot collide across owners. New records carry `scope=user`, an
   opaque owner key, and `origin` provenance.
6. Clean automatic extraction that excludes injected memory context and tool
   result bodies, plus no automatic writes in `readonly` mode.

V2 additionally lets an authenticated user deliberately turn the current live
session into a new personal skill. It is a two-phase operation:

1. `preview` projects a bounded ledger of completed authenticated HTTP turns,
   asks the model for a typed draft, validates and masks it, and stores only a
   short-lived process-local draft. It does not touch the skill directory.
2. `commit` is a separate authenticated caller action over the exact draft ID
   and digest. It creates one `SKILL.md` in the bound user's directory and
   never accepts an owner, path, or replacement body from the model.

Existing-skill edit/replace, entitlement tiers such as free/pro/admin, remote
memory providers, agent-owned writable memory, multi-skill transactions, and
cross-process catalogue refresh are not part of V2. If product tiers are later
needed, they must come from trusted authentication claims and a separate policy
resolver, not directory names or model input.

## 4. Identity and filesystem contract

The session owner is the sole user-scope authority. A safe namespace is
`u-` plus a SHA-256 digest of the exact owner identifier; raw principal IDs are
never used as path components. This keeps `/`, newlines, Unicode, and long IDs
from escaping the configured root or changing frontmatter structure.
HTTP authentication is resolved once at request admission and that same frozen
`Principal` drives the handler, ownership check, and capture `RunContext`.

With `MINILOOP_USER_RESOURCES_ROOT=/srv/mini-loop/users`, one owner resolves to:

```text
/srv/mini-loop/users/u-<owner-digest>/
  skills/<name>/SKILL.md
  memory/<owner-keyed-name>.md
```

The setting is opt-in. Without it, agent skills continue to come from
`MINILOOP_SKILLS_DIR`, user skills are absent, and memory uses the existing
manager-wide root with owner-filtered reads plus the new collision-safe keys.
Legacy files without an owner key remain readable by their recorded owner;
legacy files without any owner remain in the `anonymous` scope. Previously
overwritten data cannot be reconstructed.

## 5. Skill resolution contract

Within each source, `SkillLoader` keeps its existing sorted first-wins rule and
problem reporting. Across sources there is no shadowing:

- `load_skill(name="deploy", scope="agent")` selects the agent skill;
- `load_skill(name="deploy", scope="user")` selects the current user's skill;
- `agent:deploy` and `user:deploy` are equivalent qualified names;
- an unqualified unique name remains compatible;
- an unqualified name present in both sources returns an ambiguity error.

The catalogue groups `Agent-provided skills` and `User-scoped skills`. Entries
and loaded wrappers include their source and a content digest. Agent entries
are rendered first, but the combined output has one total
`MAX_SKILL_CATALOGUE` budget; omission is named and reported.

A user skill is instruction-like input and may influence the model, but it
cannot add tools, bypass permissions, widen a child role's catalogue, or change
the bound memory owner.

### Session capture and publication

The authoritative write edge is the authenticated `commit` route, not a
model-callable write tool. The API preview can only create a bounded pending
draft; any future assistant-facing helper must stop at the same non-publishing
boundary. `readonly` sessions may preview but cannot commit.

The source projection is not a copy or blacklist over `agent.messages`.
Authenticated `/messages` and `/messages/stream` calls stamp a capture-only
capability without widening tool authority. After a successful run, the session
records only the original HTTP user text and the final answer returned to that
caller, and only after terminal trajectory/event persistence succeeds. Tool
calls/results, system instructions, loaded skills, recalled
memory, runtime facts, steering, background/team/workflow/cron notifications,
goal/stop-hook continuations, and custom injector messages never enter this
ledger. Restored history is deliberately not reconstructed into it; a receipt
names omitted and compacted history. The ledger is masked before storage and
again before synthesis. If a registered secret is short or unavailable and
therefore cannot be screened, capture and publication fail closed. Registered
secret screening is not general PII detection, which is why the draft remains
visible before the separate commit.

The synthesis side query has no tools and uses a dedicated purpose so it does
not replace the live conversation meter. Its admitted message list is immutable
through request optimization, cache annotation, and recovery dispatch. It must
return a strict versioned JSON object. The runtime, not the model, constructs
the canonical file: lower-kebab name, single-line bounded description,
normalized LF newlines, non-empty bounded Markdown body, and only
`name`/`description` frontmatter. Malformed, over-limit, secret-bearing, or
wrapper-forging output fails with zero publication.

The source ledger is capped at 64 messages and 40,000 serialized characters.
Pending drafts are bound to the session and resource owner, expire after 15
minutes in process, and are capped at 64 globally, 16 per owner, and 4 per
session. Global pressure refuses a new owner instead of evicting another
owner's reviewed draft. Commit accepts only `draft_id` plus the canonical
document digest; a caller cannot use it to submit arbitrary Markdown. The same
digest is returned in preview and commit receipts, while `content_digest`
identifies the body. Publication is create-only: the same name and document
digest is an idempotent replay, while the same name with different content is a
conflict. An agent/user name collision is allowed and continues to require
`user:<name>` when loading. The file is created without replacement, with no
caller-controlled path and no symlink following.

Before the no-replace hard link, the publisher constructs the canonical file
and a complete future-session snapshot with the same normalization and
path-sorted catalogue order a restart will produce. The hard link is the
irreversible commit point; no fallible validation or rollback follows it, so an
idempotent concurrent publisher cannot receive success and later lose the
file. After a
commit, the resolver replaces that owner's cached resource bundle while
retaining the same `MemoryStore`. Existing live sessions keep their old
snapshot and stable prompt prefix. A teammate spawned from an old parent
inherits that parent's pinned snapshot; an independently created new session
uses the new snapshot. The
commit receipt says `activation=next_session`; live rebinding and
replacement/version rollback are separate future designs.

## 6. Memory contract

`MEMORY_TYPES` remains a content taxonomy; its `user` value is not an access
scope. Access scope is independently fixed to the session owner.

Every scoped operation rejects an explicit different owner. New memory files
use an owner digest in the physical key and carry:

```yaml
scope: user
owner_key: <sha256>
owner: <single-line display value>
origin: explicit | auto_extracted | consolidated | imported
```

When the existing memory feature is enabled, `remember`, automatic extraction,
and consolidation write only the current user store. `recall`, selected
context, and the runtime index read only that store. The resource root scopes
those paths but does not itself install memory tools. Rendered memory blocks say
`scope="user"` and include `origin`; memory is reference data, not agent policy.

Automatic extraction runs after a normal final answer, not at true session
close. It consumes a cleaned transcript projection: injected
`<memory_context>` prefixes and tool-result bodies (including loaded skill
bodies) are excluded. `readonly` sessions may recall memory but never perform
the automatic write.

## 7. Inheritance matrix

| Runtime | Skills | Memory |
|---|---|---|
| Main session | agent + current user snapshot | current user |
| Teammate | parent's resolved skill snapshot | parent's user scope |
| In-process subagent | parent's resolved skill snapshot, subject to role tools | no implicit memory tools; cannot choose another scope |
| Workflow worker | manager agent skills only | no user memory |
| Restored/cron session | resolve after persisted owner is restored | persisted owner |
| Unauthenticated local session | agent + anonymous user | anonymous compatibility scope |

A newly committed skill is visible to independently resolved sessions built
after publication. It is not injected into the turn that authored or committed
it, existing live sessions are not silently rebound, and a later teammate keeps
the live parent's pinned resource snapshot.

## 8. Failure and observability

Unreadable, rejected, shadowed, truncated, or omitted user skill files are
reported through the user-resource problem log without exposing another
owner's catalogue to the model. Unknown/ambiguous load errors list only the
current agent and current-user candidates.

Runtime preview/publication errors return stable error codes without echoing
the source transcript or rejected raw model output; framework body-validation
errors retain FastAPI's ordinary 422 shape. A missing, expired, wrong-session,
wrong-owner, or
wrong-digest draft cannot be committed, and a commit queued behind a run is
rejected if the session is deleted before it obtains the lock. Failures before
the publication commit point leave no accepted half-file and do not replace the
last known-good resolver bundle; after the hard link, retries are idempotent.

The runtime posture reports whether owner-scoped resource roots are active.
This is application-level isolation, not host tenancy: without a real sandbox
or container, shell access can still read host files outside the workspace.

## 9. Acceptance gates

- Alice and Bob can keep equal skill names and equal memory names without
  shadowing, overwrite, catalogue leakage, or recall leakage.
- Agent/user skill collisions require a source; unique legacy names still load.
- Malicious owner identifiers never appear in a path and cannot inject
  frontmatter.
- Owner is correct before first record/build and survives ordinary and cron
  restore; teammates inherit it.
- User catalogue flooding cannot remove agent provenance or exceed the combined
  catalogue bound.
- `readonly` blocks automatic memory writes; extraction excludes injected
  memory, skill, and raw tool-result bodies.
- Existing anonymous single-user behavior and legacy memory reads remain
  compatible.
- Preview writes no skill file; commit requires an authenticated owner, exact
  pending draft and digest, and is refused in `readonly` mode.
- Only original authenticated HTTP user input and its returned final answer
  enter the source ledger; tool results, loaded skills, injected
  memory/runtime/coordination text, internal continuations, restored history,
  and registered secrets never enter synthesis or the committed canonical file.
- Invalid model JSON, expired/cross-session drafts, wrong digests, symlink
  targets, and different-content existing user names fail without overwrite;
  same-content and concurrent identical retries are idempotent.
- A successful commit keeps existing live catalogues stable, refreshes future
  sessions for only that owner, reuses the owner's memory store, and reports
  `activation=next_session`.
- Relevant tests, invariant/scanner/guard checks, the full pytest suite, and
  `git diff --check` pass before delivery.

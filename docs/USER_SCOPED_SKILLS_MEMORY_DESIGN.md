# User-scoped skills and memory

Status: implementation contract for the first local-filesystem version.

## 1. Outcome

mini-loop will expose two explicit knowledge sources:

- `agent`: deployment-managed, read-only skills shared by the manager;
- `user`: skills and durable memory resolved from the authenticated session
  owner.

The owner is bound before an `Agent` is constructed. A model cannot select or
override that owner through a tool argument. Source labels are model-visible
provenance; they do not grant authority. Tool catalogues, hooks, permission
mode, and the sandbox remain the enforcement boundary.

## 2. Current gap

The existing `SkillLoader` is one manager-wide directory with one namespace,
so every user sees the same catalogue and a skill has no source provenance.
Memory has an owner-filtered `ScopedMemory` view, but all owners share one
directory and a filename derived only from the memory name. Two owners writing
the same name therefore overwrite one another before the read filter runs.

Ownership is also bound too late on several paths: the HTTP route sets it after
`SessionManager.create()` has built the agent and recorded the session; restore
sets it after agent construction; teammates do not inherit it at all.

## 3. V1 scope

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

User skill upload/edit APIs, entitlement tiers such as free/pro/admin, remote
memory providers, agent-owned writable memory, and cross-process locking are
not part of V1. User skills are provisioned by the deployment. The first
resolution of an owner creates one process-local snapshot that is reused by
that manager's sessions; restart the manager or inject a new resolver to
refresh it. If product tiers are later needed, they must come from trusted
authentication claims and a separate policy resolver, not directory names or
model input.

## 4. Identity and filesystem contract

The session owner is the sole user-scope authority. A safe namespace is
`u-` plus a SHA-256 digest of the exact owner identifier; raw principal IDs are
never used as path components. This keeps `/`, newlines, Unicode, and long IDs
from escaping the configured root or changing frontmatter structure.

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
| Main session | agent + current user | current user |
| Teammate | parent's resolved sources | parent's user scope |
| In-process subagent | parent's resolved skill snapshot, subject to role tools | no implicit memory tools; cannot choose another scope |
| Workflow worker | manager agent skills only | no user memory |
| Restored/cron session | resolve after persisted owner is restored | persisted owner |
| Unauthenticated local session | agent + anonymous user | anonymous compatibility scope |

## 8. Failure and observability

Unreadable, rejected, shadowed, truncated, or omitted user skill files are
reported through the user-resource problem log without exposing another
owner's catalogue to the model. Unknown/ambiguous load errors list only the
current agent and current-user candidates.

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
- Relevant tests, invariant/scanner/guard checks, the full pytest suite, and
  `git diff --check` pass before delivery.

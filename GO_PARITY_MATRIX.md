# Python to Go parity matrix

Baseline: Python `main` at `ad71e05`, checked on 2026-10-02. This is an
implementation inventory, not evidence that any Go behavior exists. Update
each row with a fixture or test result as the port proceeds.
Python module names in the table are relative to `python/mini_loop/`; test
names are relative to `python/tests/`.

The versioned contract snapshot is under [`go/testdata/`](go/testdata/). Run
`.venv/bin/python python/tools/export_go_contracts.py --check` to detect drift;
the generator captures `default_registry()`, `create_app().openapi()`, the
SQLite v7 schema, default-tool risk/capability metadata, Python's stop-reason
constants, serialized fake replies, and actual file-tool outputs and file
digests, glob searches and filename matching, todo transitions, textual question
outputs, deployment skill catalogue contracts, request/context contracts, role
selection, run provenance, child loops, action identities, transitions and
actual replay paths, grant candidates and parked/reviewer approval outcomes from
the current implementation (104 generated snapshots), plus registry masking,
environment selection and typed recording projections, plus real foreground commands
and command-result rendering recipes. The loop snapshot adds cache wire/token
projections, stuck signals/hashes and actual nudge/halt paths. The lifecycle
snapshot runs nine actual managed turns, event-bus bounds, activity titles and
tool labels. The scheduling snapshot exercises six execution modes, prompt nil/empty
rewrites, adverse batch completion with a barrier, ordered stuck steps and four
Todo-nag turns. The manager snapshot compares actual creation/defaults, ten
workspace binding outcomes, shared services, deletion and stopping. The HTTP
snapshot adds actual authenticated/anonymous responses,
finite SSE framing and the admitted authority stamp. The controls snapshot adds
six actual managed scenarios and six HTTP mode/steer responses, including
Unicode/count bounds and a real readonly-refused file effect. The fork snapshot
adds completed/empty history copies, fresh child state, lineage/source events
and five HTTP outcomes. The provider snapshot captures 33 actual pinned SDK
HTTP scenarios, requests, consumed replies, error classes and retry waits. The streaming
snapshot adds nine actual SDK/StreamingTransport SSE calls, replies, raw deltas,
masked coalesced progress, retry headers, partial state and body closure. The recovery snapshot captures 24 actual Python
policy trajectories: every request, event, wait, final response and mirrored history.
The progress snapshot adds nine actual configurable Python transport cases and ten
stateful direct/stream fake calls, including thinking signatures, usage and identities.
The configuration snapshot adds 64 actual Settings outcomes covering all 49 fields
and the exact default skill asset/descriptions/load contract. The private spill snapshot
adds fourteen store cases, eight actual string-Bash commands, three projection
recipes, collision/symlink refusal, a managed structured default tool round and
three best-effort manager construction cases. The thirtieth snapshot executes per-run JSONL store/privacy/restart cases, managed
recording faults, owned HTTP reads/exports and binary-float duration rounding.

Unicode casing tables are generated separately by
`python/tools/export_go_unicode.py --check`.

| Slice | Python source of truth | Required Go contract | Go evidence |
|---|---|---|---|
| Session lifecycle, ownership, workspace binding and concurrent isolation | `mini_loop/manager.py`, `session.py`, `auth.py`; `tests/test_concurrent_turns.py`, `test_auth.py`, `test_workspace_binding.py` | Typed owner/session IDs, serialized turns per session, concurrent separate sessions, identical refusal status | Library sessions serialize turns and bind resolved workspaces. Caller-stamped typed RunContext and peer derivation are implemented, with five Python snapshots and detached data. Shared subagent providers retain session isolation under race testing. NewManagedSession adds process-local admission/status/run counts, active-turn cancellation, closure rechecks and nonblocking Info activity. Tests cover queue cancellation, approval waits, handler panic repair and reentry after faults. NewSessionManager owns ordered handles, shared eight-slot model/tool pools, default in-memory approvals/actions, explicit owner lookup and bounded remembered owners. The twentieth Python snapshot matches creation/defaults, ten binding status categories, deletion and stop. Go tests cover simultaneous creation/stop, closed queued admission, retiring shared directories, factory faults and symlink-only cleanup. Bound directories are retained; scratch reclaim waits for active holders. Go library requires explicit owners and returns a typed missing/foreign error; Python raw manager defaults omitted owners to anonymous and delete returns false. HTTP authentication and owner-scoped routes are now implemented. Bounded library steering, live modes and owner-scoped HTTP wakeup are now implemented. Typed environment settings, standalone HTTP ownership and per-run trajectory recording are implemented; session restore remains pending. |
| Model/tool loop and transcript protocol | `agent.py`, `blocks.py`, `fake_llm.py`; `tests/test_transcript_contract.py`, `test_tool_batch_invariants.py`, `test_provider_fidelity.py`, `test_stop_reasons.py` | Text, thinking, tool use and result variants; ordered batches and strict transcript validation | `go/protocol` decodes Python fake replies into typed content, caller, usage and stop reasons; transcript pairing is strict. `go/agent` tests pause, refusal, unknown-stop behavior and cancelled-batch repair in memory. Todo, stop, stuck, error, approval and subagent events share a typed, sequenced 200-event backlog with label/depth/run provenance. Round exhaustion and stuck halts report a stop marker before partial output. Sixteen Python detector cases, six input/output hash cases and five actual loops match Go, including nudge reset, denied calls, monologue continuation, zero nudge budget and null opt-out. Tests cover the 20-step bound, per-user reset, final rewritten inputs, paired detector errors, child inheritance and masked/detached stuck events. Nine actual Python managed turns match core/status/done/cancelled event fields/order and history, including provider failure and model/tool cancellation. The bus matches 200-event replay and 2,000-event drop-oldest subscriptions; ephemeral deltas do not replay. Event framing tracks transcript rewrites by epoch. Error labels/random IDs/timing/numeric spelling are normalized; an empty refusal [] becomes a valid Go empty string. Failure/denial is recorded in telemetry while paired model results omit is_error, as in source. Absent caller and explicit null remain distinct. Typed prompt hooks, named per-round injectors, Todo nagging, shared model/tool limiters and consecutive parallel batches are implemented. The nineteenth Python snapshot matches six classifier cases, nil/empty prompt rewrites, adverse completion/barrier order, result/step order and four cross-turn nag counters. Go tests cover cancelled admission, shared caps, exclusive pool bypass, completed-sibling repair, worker joins/panics, invalid injector batches and fresh-child seam/pool inheritance. Bounded steering injects after custom injectors before compaction at the next model round; named posture updates distinguish operator facts from user text. Modes refresh at permission evaluation, with child control isolation. Explicit typed SSE, provisional progress and shown-text cancellation repair are now implemented; DefaultRecovery is implemented; durable steering and mid-batch injection remain pending. |
| Registry and execution boundary | `registry.py`, `builtins.py`, `permissions.py`, `approvals.py`, `actions.py`, `tools.py`, `durable.py`; `tests/test_tool_pipeline.py`, `test_permission_modes.py`, `test_durable_approvals.py`, `test_external_cancellation.py` | Pinned catalogue; before, monotonic guard, permission, execute, after, observer order; denied and unknown-effect outcomes | All ten default inputs are typed. Go has an immutable executable catalog, one ordered gate, readonly/interactive/auto policy, in-process approval and cancellation repair. `NewWorkspaceSession` registers Bash plus read/write/edit/glob; `go/workspace` matches 34 Python output/file-effect cases and tests atomic failure and symlink-parent resolution. Go agent tests workspace binding, readonly calls and a path changed after approval. Glob matches 38 Python searches and 182 filename patterns; tests cover enumeration budgets, cancellation and deep directories under a low descriptor limit. `NewRuntimeSession` adds TodoWrite/load_skill/ask_user/compress/task (all ten executable tools), with 10 Python todo transitions, four question variants and 19 skill catalogue/load scenarios. Runtime binding, readonly behavior and question cancellation are tested. `compress` defers summary generation until the full batch is paired. Task crosses the execution-risk gate and delegates through a bound provider with depth quotas. Optional journals hash final rewritten inputs, replay terminal results and reconcile unknown actions through typed verifier verdicts; only proven non-landing permits retry. Settlement precedes observers. Twelve input/identity cases and ten actual Python replay paths match Go. Explicit optional JSON null remains distinct from an absent field through cloning, masking and identity hashing. An optional bound approval broker matches 38 Python grant candidates and 21 actual approval/question/reviewer outcomes. Tests cover parked actions, same-session grants, reviewer ordering, foreign binding, context cancellation, concurrent cancellation and fresh child isolation. Preview and stored-answer redaction are optional typed seams. Injected-store restore-time expiry is implemented; SQLite approval backend and future sink coverage remain pending. Per-call execution classifiers fail to exclusive barriers; every parallel call retains the same gate and holds one permit through hooks, execution and observers. Tool use/result spans, activity labels and reconciliation telemetry are implemented; unknown-action reconciliation emits before effects/settlement. The real host shell is implemented with typed result/failure metadata, aggregate capture, full-stream masks and process-group cancellation. Eight Python command cases and seven render recipes match; real process tests cover exited-parent pipe holders, detached-pipe cleanup, cancellation, overflow, environment selection, runtime binding and replay. No Go Seatbelt backend ships; process evidence is macOS, Linux untested and other hosts rejected. |
| Default REST and SSE | `server.py`; `tests/test_server.py`, `test_streaming.py`, `test_webui_routes.py` | Health, session CRUD/message/cancel, approval, event stream, transcript, trajectory, UI response shapes | Partial: go/httpapi is an embeddable handler with thirty-four method/path operations. The twenty-first snapshot compares authenticated/anonymous CRUD, completed replay, cancellation, approvals, Null-store transcript, basic health and finite SSE frames; eight header cases, principals, six bind cases and HTTP authority stamps match. Targeted tests cover ten-MiB ingress before auth, one admitted principal, foreign endpoints, busy admission, bounded cache/rate/list work, owner rate windows, remembered approvals, SSE cursor/envelope/query auth, disconnect cleanup/cancellation and optional JSON masking without history mutation. Cache lookup/rate/claim is atomic through result publication. Streams queue; disconnect cancels their own active turn or wait, matching a separate real Python HTTP probe. Full health posture/source build fingerprint, optional UI data routes, native SQL restart and complete FastAPI validation/coercion remain pending; injected-store bounded event catch-up and transcript epoch reads are implemented. The twenty-second snapshot compares six owner-scoped mode/steer HTTP responses and model-visible wrappers; Go concurrency tests prove one idle holder, own-manager shutdown joins and rate spending. The twenty-third snapshot compares paired fork history, model/system inheritance, fresh child state, lineage/source events and five actual HTTP responses; Go tests cover atomic source admission, cancellation repair, construction failure and shutdown. Fork persistence remains G5. Snapshot 32 compares sixteen real Python shell/auth/method/static-path outcomes and four source asset identities. The embedded JS passes all 37 source DOM interaction cases; real built-binary browser tests cover create/send/tool/final/trajectory flows. Optional pane APIs remain open. The standalone cmd/miniloop launcher owns settings composition, requested/actual bind refusal, listening and signal shutdown; real TCP and built-binary tests cover auth, tool effects and active/queued cancellation. |
| Provider and recovery | `providers.py`, `transport.py`, `recovery.py`; `tests/test_provider_surface.py`, `test_streaming_failures.py`, `test_recovery_backoff.py` | Fake and Anthropic-compatible transports; served-model identity, bounded retries, safe interrupted streams | Go fake consumes typed requests, validates transcript/schema/budgets, reports the requested model and counts the complete payload. Explicit `go/provider.Client` implements direct Anthropic-compatible HTTP with typed ingress, cache wire payloads, signed thinking, served-model/usage identity and SDK-level retries. The twenty-fourth snapshot pins SDK 0.107.1/version/source hashes and 33 real mock-HTTP cases: status and connection/body/timeout retries, headers, waits, budget preflight and descriptions. Tests cover real httptest HTTP tool execution, pairing, served aliases, context-owned timeout/cancellation/backoff, limits, credential-free errors/debug identity, rejected redirects, malformed/unsupported content and shared-client concurrency. Go adds bounded wire sizes, no automatic redirects, explicit credentials and a whole-attempt deadline; SDK has per-operation timeouts. Typed SSE assembly, coalesced provisional progress, fresh stream IDs and shown-text cancellation repair are implemented; the twenty-fifth snapshot compares nine actual SDK/StreamingTransport outcomes. Local real HTTP tests cover complete tool rounds, signed history/usage, context-owned read timeout/cancellation and 32 shared-client calls. Go requires stopped validated blocks and final delta/stop, stricter than SDK unchecked partial EOF. Captured stream progress thresholds reach manager/fork/child construction; zero/negative values and elapsed-time arrival flush match actual Python transport cases. Stateful fake clients now own atomic message IDs, signed thinking and typed responder/ceiling/delay settings, with explicit direct/stream views sharing the same sequence. Ten actual Python calls pin final content/signatures/usage and deltas; concurrent Go calls retain unique IDs, delay cancellation consumes only an admitted ordinal, and stream views lift the direct ceiling. Non-null citation/server-tool/search variants and optional request settings/auth modes remain pending. The launcher captures fake delay from the environment; direct library construction remains explicit. DefaultRecovery now matches 24 actual Python trajectories, including bounded transient waits/fallback, escalation refusal retaining the partial, whole-answer continuation, truncated tools, and one reactive shrink mirrored into paired live history. Tests verify fresh stream regeneration without splicing, permits released during outer waits, persistent session fallback and fresh child override state. Internal/protected request projections and durable recovery state remain pending. Nullable text citations are omitted explicitly by the fixture projection. No live production endpoint/cache benefit is claimed. |
| Context and model budget | `prompts.py`, `skills.py`, `compaction.py`, `caching.py`, `metering.py`, `token_efficiency.py`; relevant `tests/test_*` files | Bounded request construction, skills, cache and compaction, usage accounting | The standalone binary embeds the exact Python default skill, independent of checkout/cwd; explicit filesystem catalogues retain source rechecks. `go/skills` preserves bounded deployment catalogue/body, sorted first-wins discovery, strict UTF-8/newlines and source-digest verification against 19 Python scenarios. Diagnostic occurrence/eviction counts and concurrent shared reads are tested. Invalid-name prefixes in diagnostics are bounded/marked at 2,048 characters in Go. `protocol.ModelRequest` carries fitted typed schemas, a stable system prompt, skill descriptions, changed runtime facts and output budgets to the provider. Six catalogues, three Unicode/wire budget cases, four pair-safe snip/micro histories, six meter steps, result spill and transcript/summary artifacts match Python. Usage includes cached input, calibrates same-envelope growth and observes signed shrink; summaries do not anchor the live meter. Workspace-backed sessions use all four default compaction layers, with typed receipts and guarded deferred `compress`. Tests cover empty/failed/cancelled summaries, whole-batch ordering, cheap shrink avoiding a summary and workspace archive containment. The Bash-only constructor uses in-memory compaction for an unbound injected executor; a real bound executor selects workspace compaction. Optional RuntimeConfig.Secrets masks default spill/archives/summary without changing live arguments or the summary request. Default typed cache annotations project up to four ephemeral breakpoints into a detached wire request, including summaries; live history stays raw. Twelve Python wire/position/token cases match Go. The fake rejects more than four points. Policies and stop hooks inherit into fresh children. Existing system block-list input, real provider cache reuse/savings, user-skill layering, masking for future provider/optional-feature sinks, optional prompt sections and token-efficiency policies remain pending. |
| Private Bash preservation | `spill.py`, `tools.py`, `builtins.py`, `manager.py`; `tests/test_spill.py` | Private bounded artifacts, masked content, opaque retrieval and failure-preserving preview | Typed `go/spill.Store`, Request, Ref and LocalStore match fourteen source cases: exact UTF-8, 8,000,000-byte bounds, safe names/hash groups and private/exclusive files. Eight actual string-Bash cases and three projection recipes compare preview/file digests; collisions, planted leaf symlinks, concurrent unique saves, masks and save faults are tested. Typed RuntimeConfig/ManagerServices seams retain credentials/process tracking without mutating caller executors; forks get fresh namespace names. Launcher matches three best-effort source manager cases and retains artifacts after stop. **Actual source gap:** default Python Bash uses the structured `run_bash_result`, which bypasses string preservation; Go structured default does the same (actual managed source tool round, zero artifacts). Only the string compatibility interface appends preservation notes. No owner ACL, parent-race sandbox, unbounded capture or automatic purge is claimed. Go additionally checks UTF-8/context/short writes and attempts failed-leaf cleanup after an inode check; check/unlink is not atomic against host tampering. |
| Storage and evidence | `storage.py`, `trajectory.py`, `actions.py`, `session.py`; `tests/test_storage.py`, `test_crash_windows.py`, `test_trajectory_ownership.py` | Optional SQLite state, lease and restart behavior, append-only evidence, ownership checks | Named action records/statuses, detached snapshots, workflow binding, memory result retention and a typed ActionStore/StoredActionJournal transition adapter are implemented. Six transitions per Python memory/SQLite journal, conflict/invalid-settlement behavior and Unicode result bounds match source snapshots. Tests cover cancellation, store errors and scoped unknown marking through a typed test backing. ApprovalRecord/statuses and a typed ApprovalStore write seam preserve pending, terminal, cancellation and reviewer rows; faults are bounded diagnostics and do not rewrite human decisions. Remembered grants remain process-local. Process-local detached event subscriptions, sink fault containment and transcript-epoch framing are implemented. No shipped Go SQLite store, native-database restart evidence or cross-process dispatch fencing yet; configured-store bounded event catch-up is implemented. Explicit injected-store manager restoration is implemented. File-backed per-run trajectory persistence is implemented: typed writer/reader interfaces, masked full model/tool/child fields outside the live backlog, ordered finish receipts, source privacy redaction, retained owner reads and explicit drained purge. Snapshot 30 compares eight source store cases, seven managed cases, sixteen HTTP outcomes and ten rounding boundaries. Go adds 64 MiB per-record limits; JSON reads cap eight MiB and raw JSONL streams without append locks across client waits. Typed HTML ledger and filtered record visitor are implemented. Snapshot 31 compares eleven complete Python HTML pages, file assembly, nine iterator cases, four HTTP outcomes and source asset hashes. The independent traceview CLI renders exports, IDs or a session chronology to 0600 HTML. Snapshot 44 captures actual Python SQLite epochs, rollback, exact-expiry leases, two-connection appends, identity-preserving upserts, explicit unknown recovery, audit-preserving deletion and v1/future/corrupt schema cases. Go now has concrete SessionRecord and separate session/transcript/event/lease/approval-read consumer contracts, plus a bounded current-variant archival event decoder. Tests compare all session/message/audit projection shapes and current writer variants, including a live tool loop and untrusted archival provenance. These contract/decoder tests do not run SQL in Go. Driver approval, backend execution and native restart validation remain pending. Configured RuntimeConfig/ManagerServices.StateStore now composes live session projection, request guard, prefix rewrite epochs, masked event-first capture and process leases. Default journal/approval storage uses the injected backend; explicit overrides remain caller-owned. Snapshot 45 compares actual Python AgentSession/SQLite guard, masking, event epoch/ordinal and confirmed/unconfirmed renewal recipes with typed Go test backing. Ordinary write faults degrade; guard-query/count errors stop; confirmed lease loss cancels the turn. Go claims after admission, persists queued steering before acknowledgment and disables writes before deletion to prevent late resurrection; stop releases after drain and cleanup faults are reported. Renewal/metadata require transcript growth, so stored status may remain running after completion. The global default unknown sweep follows source policy without proving foreign process death. Native Go SQLite, real database restart validation and state-backend launcher activation remain pending. Snapshot 46 runs actual Python manager/SQLite restores for seven histories. Go restores owner/binding/system/run/status, highest epoch references, Todo/steering and sequence, expires parked approvals and repairs crash tails before the first resumed model request. Go explicitly preserves repair-time metadata, avoids payload sequence reuse and claims before repair. Foreign-held handles remain pending; admission reloads under claim. Read/repair faults refuse publication and release leases without deleting historical data; stop joins restore. This is injected-store behavior through a test backing; native SQL restart and state-backend launcher activation remain pending; typed goal snapshots/disarmed restoration are implemented; configured-store bounded SSE, transcript epochs and plan-mode events/folding are implemented. Snapshot 47 compares seven actual scheduled restore/request cases. Lazy cron restores bound or freshly provisioned scratch, preserves saved owner/state and starts a fresh untrusted turn only after arm. Missing SQL rows cannot claim; no-store anonymous fallback is ephemeral. Stop cancels cooperative restore reads. |
| Optional orchestration | `background.py`, `cron.py`, `tasks.py`, `teams.py`, `subagents.py`, `worktrees.py`, `workflows/`; matching test families | Each default and authority rule preserved; typed states and bounded queues | Default in-process task delegation is implemented: capability-selected tools, fresh child state, peer provenance, Explore readonly/worker interactive modes, depth 2 and round budget 30. Four role selections and three actual child loops match Python, including exhaustion. Custom providers cannot bypass the depth quota; tests cover nested task rebinding, cancellation repair and concurrent isolation. Children inherit the exact optional model limiter and parallel-tool limiter, plus prompt hooks/injectors and stop hooks. Explicit library TaskTools now adds the five persistent graph tools through the same gate, with typed task records and masked atomic files. Snapshot 33 covers 53 source store steps, six actual tool outputs, five tool schemas/traits and five HTTP results. Dependencies, bounded rendering, corrupt-file reporting, retained crash markers and one successful cross-process claimer are verified. Manager boards isolate workspaces and forks start fresh. Task API is active independently of tools. Full MINILOOP_FEATURES activation remains unsupported. The explicit worktrees operator library ports creation/task binding, changes/keep/remove/list, audited effects and source-compatible workspace-factory fallback. Snapshot 34 compares nine real Python/Git scenarios, and real Go tests cover Git second guards, retained partial effects and owned bound-session writes/retention. Explicit WorktreeTools now installs five closed input variants through the same risk/permission gate. Snapshot 35 compares three actual source tool flows (25 steps), five schemas/traits and eight canonical input identities, including unconfigured service, linked/pinned task board and lazy first board admission after entry. enter_worktree is an exclusive barrier even with a custom parallel classifier: files, shell/sandbox, catalogue and broker surfaces prepare before execution scope publication; model context/hooks and fresh children follow the new root. Original lifecycle workspace remains authoritative for Info/task HTTP/trajectory attribution/scratch deletion. Go tests cover failed/nil/unbound/wrong-root preparation, ordered parallel groups, retained journal/observer settlement, approval/question scopes, fresh child-state isolation (selected worktree tools remain unconfigured), fresh forks and entered work surviving scratch deletion. Built-in rebind retains secrets/spill/deadline/capture/interrupt ownership; custom executors need an explicit workspace Bash factory returning WorkspaceBashExecutor for the requested root. Empty source capabilities keep these optional tools out of default child roles. Snapshot 36 compares seven actual managed factory/Git scenarios: clean/dirty delete removes only directories, preserve/stop retains work, and nonrepo/unborn/branch-conflict allocation falls back to plain directories. A concrete WorktreeWorkspaceFactory adapts the explicit service to ManagerConfig without tool activation. Shared-holder and failed-admission tests pin Go cleanup: the last holder loses scratch while Git registration/branch remains; unpublished failure cleanup is an existing Go addition, whereas Python leaves the allocation. Actual source has no Git-aware manager reclaimer. Explicit background operator service now ports typed tasks, independent contexts/groups, merged byte capture, status/rendering, retention/listing/drain, original-root ledger and orphan reporting. Snapshot 37 captures twelve actual native commands, unrecorded execution, five retained completions, started cancellation, five orphan seeds/Unicode counter reuse, 53 orphan notifications through an actual Agent injector and ten heuristics. Python Unicode 14 decimal counter inputs are pinned across every digit block. Go additionally cleans pre-start cancellation where source leaves Running/ledger; it validates bounded typed ledger reads, retains admitted executor bindings and isolates foreground interruption. Explicit RuntimeConfig.BackgroundTools now installs two closed variants through the common gate, adapts conditional Bash dispatch/classification while retaining foreground metadata, shares denial/prefix approval logic, lazily boots per-session ledger state, injects newest-50 task notifications with typed background_result events and names live survivors in interruption markers. Workspace entry publishes the prepared native shell to both paths. Snapshot 38 compares two schemas/traits, eleven canonical/null/grant variants, six activation-dependent classifier inputs, fifteen actual source-gate calls, disabled Bash behavior and the source live-task interruption marker. Native tests cover actual model request injection, 53-orphan bounded boot without tool use, mask/readonly/destructive/foreign authority, rebind, operator cancellation and default child foreground scope. ManagerServices.BackgroundTools now composes fresh session/fork state with delete/stop joins after turn drain and before reclamation. Snapshot 39 compares five actual source manager delete/preserve/bound/stop/fork cases; Go additionally checks a draining turn, late lazy creation and real launcher HTTP shutdown with a reaped native PID. launcher.Options.BackgroundTools and --background-tools select the individual service; MINILOOP_FEATURES remains unsupported. Explicit selected child background scopes now retain independent queues/counters, derive qualified IDs from peer message identity and remain reachable by parent recursive lifetime cleanup after return. Default children omit background state. Snapshot 40 captures six actual source cases and proves inherited harness injectors can orphan/unlink a still-live parent ledger. Go initializes root adoption before child admission, never child-adopts parent records and reports qualified child orphans through root restart adoption. Native tests cover check-only and Explore policies, completion requests, preserved parent/child ledgers, returned/nested child lifetime and manager cleanup. Child metadata/queues remain unbounded; no cross-session/process ledger authority is claimed. Explicit go/cron operator service now matches typed five-field expressions, per-session controls, bounded problems, masked atomic stores, disarmed restoration, mark-before-dispatch and exclusive minute claims. Snapshot 41 captures 27 expressions, 14 operator states, one-shot/pair/loss/masking/bound cases and actual source untrusted dispatch. Go adds typed scalar/read bounds and explicit Start, error/panic reporting, cancellable joins and stopped-generation suppression; two independent native processes exercise claim arbitration. SessionManager now owns cron by default, binds fresh untrusted scheduled turns and owner-scoped library operations, removes future jobs on delete and drains/joins runs on stop. Snapshot 42 compares real source authority, five lifecycle/ownership states and a live scheduled cancellation. Go adds queued-turn suppression, continued cleanup with reported cron-save failure and native foreground-shell reaping; standalone Serve starts restored jobs without arming them. Explicit CronTools installs three closed schedule/list/cancel variants through the common gate and a typed owner-bound CronControl; model Arm is absent. Snapshot 43 compares three actual source tool contexts (24 calls), six canonical/grant inputs, schemas/traits and 29 HTTP requests. Four owner-scoped HTTP list/schedule/cancel/arm operations work without model-tool activation; HTTP preserves source boolean coercions and validation-before-owner ordering. Go adds strict schema booleans for model inputs, replay/masking/foreign-authority tests, fresh activated forks and unconfigured selected children, restored operator authorization without persisted activation and actual local TCP launcher composition. --cron-tools selects the individual service. Lazy configured-store stable-ID cron restoration is implemented; native SQLite restart evidence, exact 422 validation details, teams and workflows remain pending. |
| Optional goals | `goals.py`, `permissions.py`, `session.py`, `server.py`; `tests/test_goals.py` | Revision CAS, human arming, bounded default stop continuation and disarmed restore | Five closed goal tool inputs and typed records/events are selected by GoalTools/--goal-tools; default ten tools stay unchanged. Nil StopHooks installs GoalContinuation; explicit lists replace it, including empty lists. Default consumer is inert without an armed active goal. Create/resume require explicit human provenance, whereas ordinary HTTP/cron/children remain untrusted/peer. Complete/block do not arm. Only requested stop continuations consume budget, before stuck/global-round decisions. Refusals preserve source failed=false. Whole snapshots/clear tombstones fold under restore and pending claim reload, always disarmed; forks/children start fresh. Owned GET goal projects masked detached facts. Snapshot 51 compares 46 real source gate calls, six stop calls, default/custom loops, actual Python SQLite restore/rearm/requests and four owned HTTP views. Native tests cover parallel CAS, overflow, capture-before-request, authenticated HTTP refusal, masking, pending reload, forks/children and TCP launcher. Go revisions are bounded int64 and strict JSON rejects Python coercions; corrupt/unknown archival variants fail closed. No edit/clear/pause tools ship; source cap text still mentions nonexistent goal_edit. No native Go SQL durability is claimed. |
| Optional plan mode | `plan_mode.py`, `prompts.py`, `session.py`; `tests/test_plan_mode.py` | Stable catalog, soft guidance, review refusal with feedback and last logged whole boolean | Explicit typed RuntimeConfig/ManagerServices/launcher PlanModeTools plus --plan-mode-tools install two closed variants through the common gate. Nil uses source headless approval; typed PlanApprover receives bound authority. Fault/panic/cancel keeps planning. Default/custom builders receive boolean state; fixed prompts remain fixed. Readonly/Auto permissions stay independent. Boolean events flow through existing capture/SSE/trajectory paths and restore fold, including pending reload. Forks start off; selected children bind fresh state; default role profiles omit source capability-free tools. Snapshot 50 matches 35 actual source gate calls, source schemas/traits/canonical grants and two Python SQLite restores plus next requests. Go tests three actual model rounds, persistence-before-request, faults/cancel/masking/foreign identity, forks/children and real TCP launcher. Go archival active is strict boolean, whereas Python's generic fold uses truthiness; source emits booleans. Restore folds all logged scopes, as source. Native Go SQLite and approval UI wiring remain pending. Plan refusals now preserve source text with failed=false/completed journal status; callback/hook faults remain failed=true. Snapshot 50 additionally pins 35 failed/denied outcomes. Snapshot 52 compares five real managed model loops, settled observers, memory journal rows, stuck-step flags, live/stored events, real recording error metrics and same-action replay. Model results omit is_error. This specific telemetry gap is closed; full release audit remains open. |
| Optional user resources | `user_resources.py`, `memory.py`, `skill_capture.py`; `tests/test_user_resource_lifecycle.py` | Owner snapshots and bounded publication | Canonical fields, exact private directories, layered catalogues, scoped Markdown memory, immutable owner snapshots and operator create-only publication are implemented as typed libraries. Snapshot 63 compares 16 actual Python publication scenarios and 27 calls; explicit trusted manager/runtime snapshot binding is implemented; explicit owner memory tools, automatic selection/extraction/consolidation and contained source-endpoint capture are implemented; snapshots 68/69 compare origins, owner isolation and actual endpoint calls. Shared manager Memory fallback now binds exact admitted owners across create/fork/ordinary and scheduled restoration; native tests reopen Markdown files and preserve resource-resolver precedence. Launcher/configuration roots and individual memory tool/auto selection are now implemented; snapshot 70 compares seven actual source constructor cases. Trusted successful-turn skill capture is now implemented; typed candidate parsing and preview business flow are implemented; standalone native-session model binding is implemented; manager binding and HTTP publication remain pending. |
| Optional typed decisions | `decisions.py`, `decision_tools.py`, `decision_llm.py`; `tests/test_decisions.py`, `test_decision_tools.py`, `test_decision_llm.py`, `test_decision_replay.py` | Explicit choice/score/noul judgments, actual model/usage, default-off and no execution authority | The go/decisions operator library has closed JSON Value variants and named Request/Question/Answer/Result/TokenUsage/Provider contracts. Snapshots detach every mutable surface. Strict request/result validation preserves IDs, full distributions, maximum choice, weighted score/legend, noul probability and optional whole usage. Fixed-endpoint Jev transport disables redirects, ignores proxies for owned clients, closes response bodies, bounds total deadlines/read bytes/retries and sanitizes transport feedback. Borrowed transports stay caller-owned; concurrent calls are tested. Snapshot 53 compares 28 actual source requests, 22 results, four UTF-8 bounds and 21 offline HTTP cases, including retry-after clamp/fallback, non-retried status, malformed/duplicate payload and transport faults. Native tests additionally cover local TLS, cancellation/read caps and exact large-integer legend equality. Go counts are signed64; nonstandard JSON/duplicate local keys and excessive wire nesting fail closed. Canonical map order is not exact source insertion order. Explicit RuntimeConfig/ManagerServices DecisionTools now installs the closed external-risk tool; backend injection alone leaves it off. State and member names are masked/revalidated before custom/Jev or fresh isolated LLM queries. Shared model limits, complete-response checks before recovery, typed decision/custom-model metadata and private trajectory inputs are implemented. Snapshot 54 compares 31 actual Python LLM recipes and eight gate outcomes. Native tests cover child/manager composition, fallback/parent isolation, timeout/cancellation/panic, recording projections and exact >4 KiB result replay under current guards. Default-off remains. Launcher/environment activation now selects llm/jev, with explicit backend precedence and credential-free offline inspection. Snapshot 55 compares actual source maximum-byte memory/SQLite-reopen replay, aggregate shedding and Unicode bounds/reconciliation; native common-gate replay and recreated stored adapters match result hashes/retention. Snapshot 56 compares cooperative cancellation, journal cancellation and permit release, and pins a source escaped-model negative case. Go masks result structures before escaping; full managed tests inspect decoded live/SSE, sink callbacks, private trajectories, stored events/messages and action results. This deliberately closes the source escaped-output masking gap. Native SQL/restart and live-provider audit remain pending. Native replies require complete model/usage, surrogate refusal occurs earlier, unknown backend failures normalize to RuntimeError and derived floats use 1e-12 tolerance. No paid service or native SQL decision evidence is claimed. |
| Configuration and standalone startup | `config.py`, `__main__.py`, `server.py`, `skills.py`; configuration/startup tests | Named settings, loud validation, explicit service wiring, bind guard, shutdown and redacted diagnostics | `go/config` has all 49 source fields; 64 real Python outcomes compare accepted values and rejected configurations. Go has explicit int/duration bounds, no `.env` discovery or reload, and pure loading without Python Settings workspace mkdir. `launcher.New` wires supported services, `App.Serve` owns TCP listener/shutdown, and cmd/miniloop handles SIGINT/SIGTERM. Default trajectory recording is active; its configured root failure refuses startup. Enabled unavailable services still refuse activation. The private spill root is now best-effort constructed by default; an empty path disables it, and root failure retains startup. Builtin skill bytes/descriptions/load match Python. `--dump-config` reports settings-and-availability without runtime/probe, redacts credential fields and URL secrets, and includes Go VCS build info; it is not full effective posture. Local HTTP provider and real shell timeout tests prove option wiring. Positive subnanosecond durations and nonfinite values refuse; fake-delay validation also runs for a real provider. Public console/UI shells are implemented; missing optional services and full posture remain G3/G5/G6. |
| Audit and extension seams | `config.py`, `harness.py`, `identity.py`, `audit.py`, `EXTENDING.md`; `tests/test_extension_contracts.py`, `test_effective_posture.py` | Explicit constructor dependencies, truthful default posture and credential-free reporting | Typed UserPromptHook and named MessageInjector seams receive detached history/todo/authority views; full injector batches validate before append, before runtime facts and compaction. Optional typed ApprovalReviewer and ApprovalRedactor seams, default CachePolicy/StuckDetector, typed StopHook continuation and optional ordered EventSink are implemented. Sink faults/panics are contained; callbacks may inspect Info/events but cannot recursively emit or block on live Messages. Go constructors reject invalid cache strides and detector thresholds; valid configured cases match Python. Reviewer faults/invalid verdicts abstain; remembered human grants precede review, and readonly/final denial stays authoritative. Guardian implementation, full effective-posture reporting and future provider/SQLite/optional-feature sink masking remain pending. The typed go/secrets Registry preserves named injection APIs, lazy cached values, retry/reporting, ANSI matching, Unicode casing and the default Null posture; 15 mask recipes, four environments and ten typed previews match actual Python. Runtime integration tests cover journal-before-observer order, raw execution arguments, masked model results, scoped shared-broker registries, compaction artifacts and child event isolation. The real shell consumes the selected environment and masks full/split streams before projection. Runtime registries bind independent credential configurations without mutating a shared executor. |

Snapshot 48 invokes the actual Python events endpoint iterator over SQLite/Null
for 17 window/header cases, including subscription-before-read and a concurrent
boundary emission. Go separates physical EventOrdinal from SSE EventSequence,
reads the newest 2,000 stored rows and filters by sequence. The source drops 50
stored status events when ephemeral sequences move its ordinal query past the
physical head, and raises SQL OverflowError on a huge positive header; Go closes
these measured gaps. Fresh/invalid/negative IDs use the 200-event backlog; the
pinned Python 3.11 default 4,300-digit conversion bound is retained. Owned TCP
tests prove de-duplication, live writes during catch-up, fresh replay, disconnect
cancellation and opaque pre-SSE 503 read failures. No native SQL/reopen proof is
claimed; the Go runtime tests use synchronized injected memory backings.

## HTTP inventory

Personal-skill request boundary (2026-10-07): snapshot 80 compares 43 actual
Pydantic preview/commit validation outcomes and normalized values with the native
typed decoders. Unknown fields, exact keys, null/type rejection, source patterns,
Unicode code-point limits and duplicate-key normalization are covered. This is
request decoding evidence. Snapshot 81 adds 11 actual HTTP outcomes for both
routes, including authenticated capture, preview/receipt fields, policy/draft
errors, and failure retention followed by success. Snapshot 82 compares 41 actual
FastAPI validation lists with ordered errors, locations, context and input echo.
Snapshot 83 adds 63 actual syntax/media/UTF-8 responses, including code-point
offsets, strict missing Content-Type, application +json, UTF-8 BOM and illegal
encoding. Snapshot 84 adds 88 scalar UTF-8/16/32 HTTP outcomes with BOM/NUL byte
detection, source Unicode offsets and invalid unit handling. Its six historical
surrogate counterexamples are now included in snapshot 85's 90 passing comparisons.
Raw/escaped surrogate handling, code-point syntax offsets, last-key retention and
safe Unicode refusals are covered. Snapshot 86 adds 288 actual numeric HTTP
outcomes: source double rounding, arbitrary precision integers, the pinned
4300-digit parse limit, nonfinite refusal and last-key retention. Snapshot 87 adds
144 live Uvicorn HTTP depth outcomes, compared both without and with registered
masking (288 native comparisons). Its pinned recursion-1000 profile permits 985
containers at parsing and 978 in echoed values; TestClient's stack has different
thresholds. Alternate runtime-stack profiles remain unverified. Snapshot 88 adds
48 actual owned catalogue HTTP outcomes across empty, legacy, layered and anonymous
scenarios. Retained versus future/fork publication visibility and owner admission
match; native tests additionally verify registered-secret masking and safe custom
catalogue failures.

`python/mini_loop/server.py` currently declares 44 routes. The default Go slice should
start with `/healthz`, `/sessions`, session detail/deletion,
`/sessions/{session_id}/messages`, `/sessions/{session_id}/cancel`,
`/sessions/{session_id}/events`, approvals, transcript, trajectories, `/`, and
`/ui`. Optional route groups cover skills/memory, workflows, cron, tasks/team,
improvement, audit, and benchmark. Every route requires a response/error/event
fixture before its row can be marked covered; route presence alone is weak
evidence. The current Go handler implements these thirty-eight method/path operations:

| Method | Path | Current boundary |
|---|---|---|
| GET | / | Public embedded development console; protected data APIs |
| GET | /ui | Public embedded full source shell; optional data routes remain open |
| GET | /healthz | Basic fields; full posture deferred |
| POST | /benchmark | Authenticated/rate-limited fake-only visible and heldout comparison; fresh supported config/deployment catalogue; joined temporary cleanup |
| GET | /self-audit | Admitted-owner live report under auth; fleet operator view when open; projected plain text and source character cap |
| GET | /self-audit/suggestions | Ledger-only inert typed objectives; source default limit; no recording IO or rate spending |
| GET | /self-audit/bench-task-drafts | Ledger-only typed draft tasks with fixed null expectation; no admission or work launch |
| POST, GET | /sessions | Create and bounded recent listing |
| GET, DELETE | /sessions/{session_id} | Owner-scoped lookup and draining deletion |
| POST | /sessions/{session_id}/messages | Atomic busy admission, replay and rate bounds |
| POST | /sessions/{session_id}/messages/stream | Queued turn with own disconnect cancellation |
| POST | /sessions/{session_id}/cancel | Active-turn cancellation |
| POST | /sessions/{session_id}/mode | Current permission mode and next-round posture note |
| POST | /sessions/{session_id}/steer | Owned idle wakeup or bounded busy steering |
| POST | /sessions/{session_id}/fork | Completed transcript copy, fresh scratch, typed lineage and source event |
| POST | /sessions/{session_id}/personal-skills/preview | Owned evidence preview; typed policy/validation/syntax errors and strict media; pinned byte, Unicode, numeric and live HTTP depth semantics; alternate stack profiles unverified |
| POST | /sessions/{session_id}/personal-skills/{draft_id}/commit | Owned reviewed publication; exact cleanup; typed validation/syntax errors and strict media; pinned byte, Unicode, numeric and live HTTP depth semantics; alternate stack profiles unverified |
| GET | /sessions/{session_id}/skills | Owned retained model-facing catalogue; typed masked description response; publication affects future sessions/forks |
| GET | /sessions/{session_id}/memory | Owned latest metadata from the fixed scoped store; typed masked response |
| GET | /sessions/{session_id}/memory/{name} | Owned exact-name body; source missing-name quoting and encoded-slash refusal |
| GET | /sessions/{session_id}/approvals | Scoped pending approvals |
| POST | /sessions/{session_id}/approvals/{approval_id} | Bound allow/deny/answer/remember |
| GET | /sessions/{session_id}/events | Bounded replay/live SSE; configured event-store catch-up (2,000 stored rows), owner admission before read and sequence deduplication; native SQL pending |
| GET | /sessions/{session_id}/transcript | Owned configured-store epoch reads; concrete persisted messages, historical/gap/crash-tail views and pinned query validation; Null-store 404 remains |
| GET, POST | /sessions/{session_id}/cron | Owned structured jobs and schedule; fresh process authorization |
| DELETE | /sessions/{session_id}/cron/{job_id} | Owned cancel; foreign job reads like missing |
| POST | /sessions/{session_id}/cron/{job_id}/arm | Operator-only process authorization for restored jobs |
| GET | /sessions/{session_id}/goal | Owned detached goal/arming/plan facts; no HTTP arming or mutation |
| GET | /improvements | Manager-rooted newest-first lineage; auth-bound owner, open operator view, source legacy data and private serialization failures |
| GET | /sessions/{session_id}/tasks | Fresh workspace board after owner admission; non-consuming structured rows |
| GET | /sessions/{session_id}/trajectories | Live owned session recording list |
| GET | /trajectories | Recorded-owner filtered file summaries |
| GET | /trajectories/{trajectory_id} | Owned JSON document; eight-MiB source cap |
| GET | /trajectories/{trajectory_id}/export | Owned JSON or unlocked JSONL stream |
| GET | /trajectories/{trajectory_id}/view | Owned HTML ledger; eight-MiB source cap |


## Defaults to preserve

The README reports core loop, workspace tools, REST/SSE, caching, stuck
detection, and local trajectory recording as on by default. Comprehensive
features, authentication on loopback, owner resources, token efficiency,
guardian, typed decisions, and workflows are off unless configured. SQLite,
sandbox, and secrets are null boundaries by default. The verified loop is
library-only. Go documentation and tests must state the actual Go posture,
even while it differs from Python.

## Owner resource contract progress

Snapshot 57 invokes Python user_resources canonicalization and owner hashing.
Go userresources now has closed SkillFields, immutable CanonicalSkill, named safe
ValidationError/Code and exact DirectoryKey derivation. Character/line bounds,
Unicode wrapper matching, newline/whitespace normalization and content hashes
match 42 source cases; ten owner keys preserve exact identifiers. Native UTF-8
refusal is stronger. No directory/owner binding, layered loader, memory store,
publication/activation or HTTP route ships in this library slice.

Owner directory progress: snapshot 58 compares ten actual Python root/child path
recipes. Native DirectoryResolver/DirectoryBinding now pin resolved trusted roots,
tighten 0700 permissions, reject planted child symlinks/files and cache immutable
exact owner paths. Cancellation/concurrent reuse are tested. The source's stores
and layered catalogue are not constructed natively yet; session/HTTP/configuration
activation remains pending. Source path checks are not external-process fencing.

Layered skill progress: snapshot 59 compares 16 actual Python source pairs,
qualified/scope selection, collision/invalid/unknown-name refusals, Unicode
combined budgets and per-source changed/missing/identical/newline-equivalent
file verification. Native LayeredCatalog is concrete, uses the same verifier as
legacy Catalog and preserves separate problem ownership. Builtin/cancellation
and detached diagnostics are native tests. Owner/session composition is pending.

Memory storage progress: snapshot 60 compares 13 actual Python MemoryStore/
ScopedMemory scenarios, 118 operations and exact file hashes. Typed native file
storage implements cache/deferred index/search/owner replacement/legacy migration
and masking. Context cancellation, private temporary modes and closed scoped API
are native additions; imported bodies retain source full reads and mtime/size
cache behavior. LLM selection/extraction/consolidation, tools and trusted runtime
resource composition remain pending.

Owner resource composition progress: snapshot 61 compares actual Python frozen
bundles, exact owner caches, fixed skill snapshots, independent user logs,
operator diagnostics, masked memory and owner isolation. Native Resources uses
private binding fields and returns ScopedStore directly; completed cache values
are shared, failed builds are uncached and retries recheck links. Four source
scenarios and native concurrency/cancellation/retry checks pass. Publication,
manager/restore/child composition, configuration and routes remain pending.

Publication file-boundary progress: snapshot 62 compares 12 actual Python
atomic_create_bytes/read_bytes_no_follow scenarios. Native durable uses anchored
component-wise O_NOFOLLOW descriptors, bounded regular reads, private fsynced
scratch and no-replace hard-link commits; typed device/inode identity is returned.
Darwin native concurrency/rename/cancellation/FIFO tests pass. Linux/Intel-Darwin
compile checks are separate from runtime evidence. Full skill publication remains
pending; no path-only replacement is used as an equivalent.

Pre-commit catalogue progress: native Catalog.WithSourceDocument prepares a
detached snapshot with the existing parser before filesystem commit, refusing
name/path conflicts and malformed/truncated sources. Existing snapshot 57's valid
actual Python canonical cases compare full-source/body hashes, prepared entries,
restart descriptions and loaded output. Native tests cover absent/changed-file
refusal, path ordering, independent concurrent diagnostics, bounds and cancelled
preparation. This is a publisher prerequisite; full publication and managed
activation remain pending. No new source snapshot or coverage measurement.

Create-only publisher progress: snapshot 63 exercises 16 actual Python scenarios,
27 calls, safe receipt/error projections, collision warnings, normalization, raw
secret screening, short/unresolved registry refusal, owner isolation and exact
committed bytes/modes. Native PublishSkill retains live snapshots and memory,
prepares before hard-link commit, verifies bounded exact canonical retries and
handles independent resolver races. Native secret-surface/panic/cancellation tests
fail closed; source digest retention is stronger than synthetic Python entries.
Capture/preview, manager/configuration/routes and cross-process cache refresh
remain pending. No vague domain payloads or new dependencies were introduced.

Managed resource progress: snapshot 64 runs actual Python manager create/fork,
teammate binding, SQLite restoration, missing scheduled restoration and disabled
legacy layering: nine frames and 36 actual tool calls include domain-refusal
flags/output hashes. Native explicit ManagerServices.UserResources resolves owners
for new/forked/restored handles; RuntimeConfig.UserResources validates and copies
a complete matching bundle. Live snapshots remain fixed and selected children
inherit. Native tests compare the source teammate's inherited binding through the
runtime seam; they do not implement a teammate scheduler. Native restored-state
evidence uses the injected test backing, not Go SQLite. Complete native child
bundle retention also pins scoped memory, while Python in-process subagents
explicitly inherit skills. Memory lifecycle/tools, launcher root selection,
capture/routes, native SQL, teams and remaining groups are still pending.

Memory tool progress: snapshot 65 captures 14 actual Python common-gate calls
over a shared memory store, including owner isolation, overwrite, defaults/nulls,
unknown string type normalization, recall provenance/escaping and readonly
denial. Native closed input variants compare schema/traits/replay identity;
explicit runtime/managed tools retain bound owner stores and selected-child
sharing. Native malformed non-string payloads are refused at the typed boundary.
Default tools/launcher activation, automatic memory selection/extraction/
consolidation, context index and capture/routes remain pending.

Memory context progress: snapshot 66 executes 12 actual source selection side
queries and change-only runtime-facts cases. Native optional memory tools now
default automatic selection on, with detached MemoryAuto=false override.
Prepared input, request tail/budget/absence of tools/system, model fault fallback,
ordered duplicate/bool indices, owner isolation, unchanged live meter and typed
load events match. Native JSON rejects nonfinite values into lexical fallback;
transcript/lease authority failures propagate. Runtime index requires recall,
independently of the auto flag. Full native turn and cancellation tests exercise
request integration. Extraction/consolidation, healthy-endpoint capture and
launcher/default activation remain pending.

Memory lifecycle progress (2026-10-07): Store-owned process-local lifecycle
serialization is shared by every scoped binding and separate from the ordinary
operation lock. Explicit remember holds it through Write, matching
python/mini_loop/memory.py install_memory; automatic extraction/consolidation
remain pending. Native tests exercise shared bindings, cancellable waiting,
error release, independent stores, and scoped operations inside a callback.
A native bypass mutation is caught by the gated remember cancellation test.

Memory extraction stage progress (2026-10-07): snapshot 67 executes actual
extract_memories through Agent._create in 13 cases, comparing the prompt,
1,500-token request, source agent_turn purpose, incremental count/files/origins,
owner isolation, ASCII Unicode tail and greedy context stripping. Go retains
supported typed thinking/tool-use variants while removing tool results and
runtime/recalled context. Side requests now carry explicit history ownership;
normal recovery/cache/provider/event behavior remains, without live meter/history.
The native decoder rejects non-string/null entry fields and nonfinite JSON;
source may coerce malformed header scalars. Cancellation and native transcript/
lease failures propagate. The stage awaits consolidation and healthy-endpoint
capture wiring; default runtime end behavior has not been activated yet.

Memory consolidation/capture progress (2026-10-07): snapshot 68 compares ten actual
Python consolidation cases, and snapshot 69 compares eleven real Agent endpoint
paths. Automatic capture holds lifecycle across extraction and owner-scoped
replacement, requires the tool pair/auto/writable posture, preserves unchanged
origins and contains ordinary lifecycle faults. Normal final, stop-hook stuck halt
and exhaustion capture; source tool-batch stuck halt omission is measured/preserved.
Readonly, disabled, missing pair, provider error and cancellation produce no
capture writes. Native authority loss propagates; capture error class labels differ
from Python. Typed extract/error events detach, mask and archive without authority.
Root/default launcher selection is delivered below; user-skill capture/preview/routes remain pending.


Launcher memory progress (2026-10-07): snapshot 70 records seven actual source
manager construction cases: default/configured shared storage, owner-local with
shared storage retained, and shared/owner roots blocked by regular files. Native
startup compares outcomes, side effects and owner-root permissions. The launcher
always constructs the shared Store, then optional UserResources Resolver; exact
owner binding stays in the common manager composition. Native HTTP provider tests
exercise all selected storage modes, Alice/Bob/anonymous isolation, owner-local
precedence and copied MemoryAuto=false. Default startup installs no memory tools;
explicit Options.MemoryTools/--memory-tools installs the pair. Automatic behavior
defaults on only when that pair is present, with --memory-auto=false opt-out.
Dump/Inspect reports selected memory backend/tool/auto choices without effects.
Comprehensive feature activation stays unavailable. Root errors refuse startup;
no memory tool can change owner or roots. Native SQLite and user-skill capture/
preview/routes remain open.


Personal-skill draft storage progress (2026-10-07): snapshot 71 compares 40 actual
Python operations and source defaults. The native explicit DraftStore uses typed
coverage, identifiers, immutable private handles and detached public previews.
FIFO session/owner/global quotas preserve other owners; wrong authority is 404
before expiry/digest checks, expired access is 410, digest mismatch is 409 and
capacity refusal is 429. Get/Peek do not consume; atomic Consume is one-shot;
DiscardCommitted requires exact identity and ignores TTL after publication.
Evidence remains signed/duplicate integers at this storage layer, matching source.
Native tests add 32 concurrent consumers, detached evidence and hidden authority.
Configuration uses typed durations and safe errors (Python uses ValueError);
initial entropy failure occurs before quota mutation. Model preview/projection,
manager injection and authenticated skill routes are still pending.


Skill evidence projection progress (2026-10-07): snapshot 72 compares 38 actual
source legacy/admitted-text cases, full compact Unicode JSON hashes and metadata.
The native pure helpers preserve greedy recalled-memory stripping, malformed
wrapper refusal, whole interjection handling, injected marker/compaction flags,
assistant text-only blocks and complete user-array exclusion. Already-admitted
text is preserved verbatim. Source whitespace/IGNORECASE and Unicode 14 word
boundaries are pinned, including a Unicode 15 classification difference. Fixed
field strings/keys mask before whole-message suffix budgeting, including key
collisions and role-label redaction; labels establish no provider role. Exact
brackets/commas/escapes/Unicode cost, 40k cap, omissions and coverage match. Native
limits/UTF-8/typed protocol validation are stricter at malformed boundaries.
Model preview and authenticated skill routes remain
pending; legacy filtering does not grant admission.

Admitted-turn skill capture progress (2026-10-07): snapshot 73 compares 20 actual
Python recorder recipes and their state/projection hashes. ManagedSession owns
a fresh process-local typed ledger, populated after successful terminal flush
only with trusted capture capability. HTTP normal/streaming calls record once;
idempotent replay, foreign requests, ordinary/peer turns and cancellation do not.
Source masking, Unicode trim/JSON costs, 64-message/40k bounds, single-message
eviction, omission counts and plain-history compaction flags match. Registered
short/unresolved or unavailable screening latches failure; healthy recovery does
not clear it. Projection preserves masked field/key/collision semantics and
refuses capture errors. Native snapshots detach and serialize concurrent readers;
typed storage removes malformed dynamic-state cases and faults use safe latches.
No ledger is reconstructed from restored/forked history. Candidate parsing is
implemented below; model preview, draft-manager injection and authenticated routes
remain pending.

Skill candidate parser progress (2026-10-07): snapshot 74 compares 63 actual
source outcomes and accepted-result hashes. Typed immutable create/skip results
preserve original fields and detached evidence. Exact schema, recursive masking
and subsequent validation order, empty skip, strict integer/unique/in-range
evidence and canonical skill validation match. Transient JSON boundary records
preserve last-wins duplicate keys and nonfinite-type rejection outcomes, without
retained dynamic/nonfinite payload. Source integer digit limits are pinned. Native
UTF-8/lone-surrogate refusals prevent lossy decoding; safe masking_unavailable
contains native masker panics. Preview health checks/model retries, manager draft
injection and authenticated routes remain pending; no runtime activation changed.

Skill preview business flow progress (2026-10-07): snapshot 75 compares 29
actual Python flows, exact request hashes and retained draft fields/digest. The
complete preview function is ported through typed model/store seams: strict
ledger presence, projection/refusal ordering, masked bounded focus, fixed system/
output tokens, two sanitized attempts, safe repair reason, provider/cancellation
outcomes, post-parse health, skip and bound retention. Closed JSON maps are
transient boundaries; no arbitrary payload enters service state. Failed previews
retain no draft. Native panic/deadline handling is explicit. The native-session
model adapter, manager admission/lease binding and authenticated routes remain
pending; this library grants no publication authority or runtime activation.

Standalone native skill preview progress (2026-10-07): snapshot 76 compares six
actual Agent model calls, repair/refusal/provider outcomes and model events.
Session.PreviewPersonalSkill serializes with core turns and binds configured
owner/session to process-local drafts. Normal cache/recovery/shared limiter and
telemetry are retained; tools are empty and only text response blocks are joined.
Recovery receives no live history and the live token meter is unchanged. Manager
admission/lease/cancellation, draft injection and authenticated routes remain open.

Manager skill draft storage (2026-10-07): snapshot 77 compares seven actual
Python create/fork/ordinary and scheduled restore frames with the shared pool,
including resources-disabled and anonymous construction. Managers now inject
one process-local typed DraftStore through the common runtime factory. New
managers start empty; SQL source evidence does not prove a native SQL backend.
Native checks verify adapter retention/owner absence and global 64-item capacity
across sixteen sessions without foreign eviction. Manager preview admission/lease
and authenticated routes remain pending.

Owned manager preview (2026-10-07): snapshot 78 compares eight actual source
policy/preview outcomes. Native owner/configuration, admission identity recheck,
lease requirement and strict ledger supply are implemented. Readonly can preview.
Native before/during request lease loss maps to safe 409; turns retain idle status
and count, with separate private cancellation/join on deletion/stop. This join
extends source turn-only cleanup. Commit/publication HTTP routes remain open.

Manager reviewed commit (2026-10-07): snapshot 79 compares six actual source
owner/readonly/digest/publication/retention/idempotency cases. Commit now shares
preview admission/lease/lifetime, publishes through the typed create-only resolver
and discards exact identity only after success. Failure retains drafts; receipts
activate in future sessions and preserve source status/message classification.
HTTP preview/commit routes and final lifecycle audit remain open.

### HTTP resource boundary — current checkpoint

Owned preview/commit routes are implemented; their body admission now covers
source media, byte encodings, syntax, retained Unicode and numeric values, ordered
schema diagnostics and pinned live HTTP depth. Earlier resource paragraphs above
record chronological implementation checkpoints and their then-open work. They
are historical; current resource status is summarized in the matrix and HTTP
inventory. Native SQLite, complete teams
and other remaining groups still require their planned implementation and audit.


## Benchmark statistics progress — 2026-10-07

The native benchmark result/measurement types, median aggregation, strict pass
votes, paired conservative verdict and transcript motion metrics are implemented
as an operator library. Snapshot 90 compares 10 actual source aggregations,
15 paired comparisons (including task-set errors), six typed-transcript metric
cases and 20 decimal-rounding cases. Exact large integers, bool-as-number,
integer/float median identity, duplicate task pairing/all-row totals, ordered
warnings, zero-base deltas and identical read windows are explicit. Nonfinite
native arithmetic refuses with a typed error; malformed arbitrary source rows are
not representable by the concrete native result struct. The JSON integer limit
is pinned to Python's current 4300 digits. An immutable runtime/manager-create
tool selection now matches source subset order, duplicate/unknown/empty semantics
and reduces both model schemas and actual gate handlers; forced excluded write/Bash
calls cannot reach their handlers, and child roles inherit the reduced catalogue.
This transient construction profile is not persisted across fork/restore.
Five visible and three heldout task specifications, trusted typed judge/setup
callbacks and exact seeded log bytes are now implemented. Snapshot 91 compares
eight source specs, 50 actual filesystem/text judgments and the 324000-byte log
digest. Unicode splitlines, strict UTF-8 faults, permissive substring/existence
behavior and symlink semantics are pinned. Nil native judges/zero tasks refuse
explicitly; malformed arbitrary callback objects are unrepresentable.
Native RunArm now owns/joins a manager, executes fresh anonymous interactive
sessions, captures task storage, applies whitelists and measures real run time and
final transcript cost/motion. Setup/create/cancellation abort; ordinary run/judge
faults score failed rows. Snapshot 92 captures eight actual source arm recipes,
including visible/heldout default-fake effects, empty sets and fault/recovery
boundaries. The eight default task effects/final texts/deterministic rows match.
Native failure diagnostic spelling remains different: the recovered fault fixture
costs 38 versus source 32. FakeObjectProvider models source non-SDK object transcript
projection without changing raw client/real-provider absent/null semantics; the
launcher selects it. Empty native history now costs zero. POST /benchmark now
composes four fresh fake clients and owned arms, with transient roots removed after
all joins. Snapshot 93 compares five actual HTTP profiles: default, ignored body,
malformed JSON body, real-main configuration and one-round fresh environment.
Only duration and its derived warning are normalized; other results/metrics match.
Native tests cover authentication/methods, shared per-owner rate budget, cancellation
and construction cleanup, fresh deployment catalogues and private configuration
failures. The handler now has 34 operations/31 patterns. Source create is not
rate-limited; benchmark shares the message/fork/steer expensive-route budget.
Unsupported activated runtime profiles remain explicit failures, not parity.
No G7 acceptance or migration completion is claimed.


## Typed self-audit observation core

`go/selfaudit` renders explicit observation snapshots for the five source report
sections, with typed optional fields and safe failure classes. Activity uses the
100 most recent sessions; owner totals preserve the source's bounded count.
Problems retain ledger ordering/counts/churn; trends use 50 global summaries or
10 per recent owned session (at most 20 sessions/50 summaries). Skill usage scans
200 tool-use events per summary and labels correlation. Cron exposes sorted
disarmed IDs only when global inclusion is selected. Reports cap at 8000 Unicode
characters with the source truncation marker.

Suggestions inspect the last three distinct entries per source, strip Python
whitespace, cap at 300 characters and deduplicate after truncation. Limits clamp
to at least one; the default is eight. Draft names use the source SHA-256 prefix
and ledger- name. Expect is a concrete null-only type, never a callable; native
benchmark.NewTask still rejects a draft without a human-authored judge.
Snapshot 94 compares 26 actual source processing profiles, including scope,
empty/missing seams, Unicode/deduplication, scan/report caps and independent
collection/inspection/event failures. No report fields or timings are normalized.
Native tests also refuse non-null draft expectations and check input immutability.
Snapshot 94 proves the processing library; manager collection is now implemented
as described below. Runtime tool installation remains pending; the HTTP routes are implemented below. It does not synthesize empty
reports for unimplemented services. Source parity evidence covers scalar UTF-8 and finite numeric observation
profiles; arbitrary malformed Python namespace values are not claimed covered. No HTTP inventory/G7 acceptance is added by these core checks.


## Exact problem ledger and holder diagnostics

`go/problems.Log` ports source ProblemLog append/extend/clear, bounded distinct
FIFO retention, repeat counts, lifetime totals, eviction counts and churn. Snapshot
95 compares nine actual source sequences, including 400 appends across a churning
four-message/three-slot log, returning evictions, exact Unicode/whitespace and
nonpositive capacities. Source invalid-capacity append increments total before
IndexError; native ErrEmptyEviction preserves that state and unwrap-compatible
error identity. Native ingress accepts rendered strings; arbitrary Python object
str() callbacks/list mutation are outside this typed API. Zero Log uses default
50; explicit New(0) preserves zero and its append failure.

Counters use immutable exact nonnegative integers and detached big.Int views.
Ledger.Total now accepts that Counter instead of a machine-sized int, retaining
all prior snapshot-94 report outputs and JSON integer shape. Source header counts
are rendered without float conversion. Optional ProblemSource exposes detached
SelfAuditProblems. ApprovalBroker and InMemoryActionJournal record every diagnostic
occurrence before their legacy deduplication; existing Problems() remains unchanged.
Native tests trigger repeated real reviewer faults and result shedding, checking
counted snapshots, private panic omission, old API behavior and replay authority.
Manager collection and native holder adapters are now implemented below. Stored
journals without a diagnostic seam and model tool remain pending. HTTP binding
is implemented below; inventory is now 37 operations/34 patterns.


## Live self-audit observation

SessionManager.ObserveSelfAudit captures owner-admitted handles before any injected
service IO, sorts actual immutable creation times and inspects at most 100 handles.
TotalSessions retains fleet size without inspecting older handles and is scoped
to the bounded owned set in owner observations. No manager/session lock spans IO.
Native cron, trajectory, skill, task, approval, action and gate holders supply
detached typed ProblemSource ledgers; task diagnostics use the already-created
runtime store through an atomic pointer, never initializing a second board. Cron
jobs and arming are read atomically without retaining prompts or targets.

Fleet recordings use list(50); owner reads query only admitted recent 20 sessions
with per-session list(10), cap 50 before event IO, verify returned session/owner
metadata and request tool_use events with a 200-event budget. All other tool uses
consume that budget. A total 64 MiB event-byte budget bounds wire decoding and
returns ObservationLimitError; raw JSON is transient ingress only. List/event
faults and panics retain class-only failures; event faults preserve trend results.

Memory raw stores retain exact full diagnostics. Fixed-owner bindings retain only
known-owner write/replacement diagnostics in separate bounded logs. Unreadable
shared files cannot establish an owner and are fleet-only. This intentionally
corrects source ScopedMemory's shared problems delegation; bindings do not share
observations merely because they have the same owner. Unattributed errors from
per-owner resource stores are not yet available in fleet collection. Existing
cron/trajectory/skill/task counters retain their native machine-sized storage;
the adapter does not claim to remove those overflow limits.

Snapshot 96 constructs actual Python SessionManager/AgentSession instances for six
empty, mixed activity/owner, unknown-owner and 100-session cap profiles. Reports
match exactly with no normalization. Native tests cover actual JSONL turns, owner
admission before event IO, 20/10/50/200 budgets, cancellation, panic/error isolation,
concurrent snapshots, lazy task faults and unattributed-memory filename omission.
This observer slice alone adds no HTTP acceptance; the route binding below has
its own corpus. Model-tool and other migration rows, including stored persistence,
teams and workflows, remain open.


## Self-audit HTTP composition

The three GETs now bind actual manager observation and the existing pure report,
objective and draft processors. Configured authentication selects the already
admitted principal and excludes fleet ledgers/cron; open deployments use the
unscoped operator view. Query/body fields do not override owner, global inclusion
or the fixed default curation limit. GETs consume no rate budget and do not launch
turns, schedule jobs or admit benchmark tasks. Existing ingress caps and global
authentication precede handlers; query tokens remain event-route-only.

ObserveSelfAuditProblems uses a private finite collection mode to skip activity/
Info, cron overview and trajectory list/visitor IO. It reads only existing ledgers
and treats a failed collection as an error, not an empty suggestion set. Report
mode retains independent visible class-only section failures. Curation failures
return a private plain 500; typed JSON responses use the shared fail-closed
projection. Native plain report masking is applied before writing, with the source
Unicode cap/marker reapplied after replacements that can expand text. This extends
source self-audit masking while preserving the native recording boundary.

Snapshot 97 compares 72 actual source HTTP responses (open, authenticated, private
owner-resource memory): scope, headers, report strings, objective/draft structures,
ignored malformed bodies/query overrides, bearer/query authentication and wrong
methods. Only generated session IDs are replaced with labels. Native tests cover
real no-rate semantics before/after exhausted turn budget, body cap before auth,
no recording IO/launches, shared-memory filename omission, projection panic/private
errors, Unicode report cap and mask expansion. HTTP inventory is 37 operations/
34 patterns versus source 44; seven improvement/team/workflow operations remain
missing. Source shared-memory diagnostic delegation remains intentionally corrected
as documented above, and model-tool installation remains open.

## Self-audit model-input contract

Snapshot 98 captures the actual installed Python tool's schema, read risk,
readonly/exclusive traits, absence from the default registry and five keyword
admission outcomes. The native closed input union now recognizes self_audit,
with an empty concrete payload and detached schema. Unknown fields, including
owner/global/limit overrides, cannot survive decoding. Canonical replay input,
masked recording copies and provider tool-use blocks retain its discriminator.
The schema matches source exactly; it does not install the tool. Runtime handler,
manager visibility policy, launcher selection and child propagation remain open.
This protocol step does not change the HTTP inventory or enable comprehensive
features. The Python model handler's unscoped manager report must be reconciled
with native owner authority at trusted composition, never via model arguments.

## Bound self-audit model tool

The optional SelfAuditTools runtime/manager flag now installs the source schema
and read-risk/readonly/exclusive traits through the common gate. A typed observer
and finite trusted view bind collection to the session owner by default; explicit
operator construction preserves the source unscoped manager report. Standalone
--self-audit-tools selects owner view under configured auth and operator view when
open. Authenticated model-tool scope intentionally corrects Python's unscoped
handler. The model cannot override scope. The shared manager construction map
covers new/restored/forked runtimes; selected children inherit the bound view,
while default role capabilities remain unchanged. Bare activated sessions retain
the source no-manager notice. Cancellation and private panic containment surround
collection, which does not acquire the active turn lock. Existing result hooks,
masking and journal semantics remain in order.

Snapshot 98 now includes the actual source no-manager result. Native model loops
compare it and the source empty-manager report from snapshot 96, then verify
owned/operator diagnostics during an active turn, default-off/selection removal,
readonly traits, child binding, guard-before-observation, result masking and local
HTTP-provider launcher execution under both auth modes. No HTTP inventory change;
G0–G7, comprehensive features, persistence and other gaps remain open.

## Improvement acceptance-instrument checks

Snapshot 99 captures three actual source classifier profiles and ten actual
fingerprints: empty/missing/NUL roots, changed bytes, renaming, all four globs,
Unicode/binary data, hidden files, directory omission, file/broken/looping symlinks
and source read-failure marker. Go improvement.VerifierTouches preserves case,
order, duplicates and substring rules. VerifierFingerprint returns a concrete
comparable 16-byte value with the source 32-hex JSON/text projection. It hashes
relative paths and full bytes without separators, preserving pattern order and
sorting within each glob. Literal workspace metacharacters remain literal.
Permission errors in glob enumeration are ignored, stat permission errors abort,
absent/loop paths are skipped and failed reads contribute <unreadable>. Other
scan IO failures abort. Native tests exercise mutation/restoration, detached touch
results and explicit filesystem faults; malformed filename encoding is tested via
a detached entry because APFS refuses creating such filenames.

These root-relative globs cover only tools/verify_*, .github/workflows/*,
conftest.py and tests/conftest.py; they do not scan every path flagged by substring
classification or arbitrary nested verifier directories. Fingerprint sampling
at acceptance judgment remains required in the future verified loop. This
library installs no runtime/model/HTTP feature. HTTP inventory remains 37/44;
verified loop, proposal flow, both improvement operations and
G0–G7 remain open.

## Improvement archive append

Snapshot 100 runs the actual Python ImprovementArchive.record with fixed UUID
and clock: missing/null fields, populated Unicode fields, false/zero/empty values,
explicit empty and default anonymous owners, parent lineage, secret masking of
values and keys with last-key collisions, and best-effort root/open failures.
Go Record preserves the twelve-field JSON schema and allocated raw ID even if
its stored value is masked. ProposalFields admits the known producer schema;
summary/next/unknown inputs are omitted. JSON formatting is semantically compatible,
not byte-identical to Python's ASCII/spaced output. Native tests exercise concurrent
complete appends, prior-row preservation, detached projection, failure before IO,
private masking panic refusal and ID shape. Constructor performs no filesystem IO.
The lock serializes one instance only; this index is not a transactional artifact.
Legacy reading and owner filtering are now implemented as the next slice below;
runtime/HTTP binding remains pending. HTTP inventory stays 37/44 and G0–G7 remains open.

## Improvement archive compatibility reads

Snapshot 101 compares 27 actual Python ImprovementArchive.list profiles: unknown
fields, non-object rows, source newest-first order, malformed/blank lines, exact
owner and empty owner, accepted-row limit accounting, zero/negative and default
200 limits, duplicate-key last value with first-position order, Python Unicode
splitlines, BOM skipping, arbitrary-width integers up to 4300 digits, rounded
floats and nonfinite values, lone-surrogate escapes, >64 KiB rows, missing/IO
failures, strict UTF-8 failure and 500/1005-depth samples. Canonical per-row digests
compare complete source values, including large rows and unknown nested fields.
Go ArchiveQuery and private-field ArchiveValue variants contain no any or raw JSON
service state. Accessors detach slices and expose explicit text/integer/float/
boolean variants. New proposal admission remains ProposalFields. Historical rows
are not remasked; the source reads the whole file, with no imposed byte cap.
Scoped non-object rows abort the whole query, like source AttributeError, unless
the accepted-row limit was already satisfied. IO failures yield [] while UTF-8
and conversion failures propagate. Nonfinite values survive library reads but
MarshalJSON refuses them, matching the future standard HTTP serialization boundary.
Go accepts 1000 nested containers; Python's precise call-stack-dependent recursion
cutoff is not established by the 500/1005 samples. Runtime/HTTP binding remains
pending; HTTP inventory stays 37/44 and G0–G7 remains open.

## Improvement lineage HTTP reads

Snapshot 102 compares 46 actual source responses across open and authenticated
deployments: alice/bob/fleet lineage, query/body overrides ignored, missing/bad/
query-only credentials, wrong methods, malformed line skipping, missing/directory
IO results, invalid UTF-8, arbitrary legacy scalar rows, owner type mismatches,
duplicate owner keys, nonfinite/lone-surrogate response failures and filtering
foreign malformed-response values before serialization. Finite float/bigint values
and source default 200 rows also match. The cap fixture retains a complete response
digest rather than expanding 400 repetitive rows. Historical strings are returned
without remasking, including a value registered in the current secret registry.

Manager construction owns a fixed .improvements archive without startup archive
IO. ListImprovements takes a trusted optional OwnerID; HTTP derives it solely from
Authenticator.Configured and the admitted principal. Serialization finishes before
headers; read and response faults use the source private plain-text 500. Typed
ArchiveValue still retains lone surrogates through read accessors, but MarshalJSON
now refuses them as well as nonfinite numbers at the source HTTP boundary. Native
tests also verify request byte admission, GET rate neutrality, cancellation, no
provider/session work and constructor absence. Inventory is now 38 operations/
35 patterns versus source 44. Six POST-improvement/team/workflow operations, native
persistence and G0–G7 remain open.

## Pure verified-loop contracts and folds

Snapshot 103 compares 32 actual verified_loop.py folds and refusals: empty patch
revision increments, pending/blocked/untrusted changes, nine verdict/integrity
pairs with matching/nonmatching coverage, one usable receipt among others, stale
CAS, contract revision mismatch, unused foreign receipts, unknown status and
missing IDs, typed artifacts/facts, duplicate blocker first removal, atomic refusal,
and duplicate/extra checkpoint requirements. Hash and canonical identities match
source byte-for-byte, including sorted keys, spaced JSON, Unicode/HTML characters
and tuple projections. Hash deliberately excludes allowed surfaces, persistence
and contamination rules; it is not a signature. Source permits extra checkpoint
IDs and duplicate state IDs; first StatusOf and last-value fold behavior survive.

Go TaskContract/Checkpoint/Receipt/RoundPlan retain private snapshots with detached
constructors/accessors. Five Operation constructors form a closed union, replacing
open patch tuples. Native tests also prove replay, input/result detachment, source
defaults, Unicode whitespace refusal, invalid constructors and no revision wrap.
Native revisions are signed 64-bit, and canonical identity refuses non-scalar
UTF-8 text; arbitrary-width counters/non-scalar Python canonical text are outside
this native profile. ApplyPatch is pure; executor, acceptance command, integrity
probe and session service remain pending. HTTP stays 38/44 and G0–G7 remains open.

## Verified-loop execute / accept / fold coordination

Snapshot 104 runs the actual Python VerifiedLoopService over explicit worker,
CommandResult, probe and event seams in 22 scenarios: first success, repair after
feedback, prose-only failure, None exit, timeout, source overflow/error-plus-zero
exit behavior, tampered/restored instruments, nil baseline/current samples, zero/
negative/default round counts, Unicode prefix/tail limits and effect/event errors.
Outcomes, canonical checkpoints, full receipts, objective feedback and exact effect/
event ordering match. The source success predicate is exit zero, not timed out and
not tampered; overflow/error flags do not independently change that predicate.
Successful later restoration yields clean final integrity while retaining earlier
suspect receipts. A nil initial sample disables later probes, matching source None.

Go Service uses typed Worker/AcceptanceRunner/IntegrityProbe/VerifiedEventSink seams
and a closed round/receipt/checkpoint telemetry union. ShellAcceptance and
WorkspaceIntegrity reuse actual workspace shell and instrument effects. Native
tests prove actual command execution, tamper-before-acceptance refusal, restored
second-round success, restoration during acceptance still refused, cancellation
without late acceptance, private panic refusal and detached exit-code telemetry.
Command/sink/probe errors abort without a successful outcome. Rune limits match
source characters. Source-bound 64-bit/scalar text profile remains explicit.
Runtime session/worker/approval/event binding, proposal Git/POST, native persistence
and G0–G7 remain open; HTTP stays 38/44 (35 patterns). This is an explicit operator
coordinator, not a registered runtime feature or a claim of live-model convergence.

## Managed verified task binding

RunVerifiedWithContext composes the coordinator with actual managed admission,
cancellation, worker role/subagent derivation, structured command execution and
typed session events. The operator acceptance command uses the bound executor
directly, matching Python; worker tools retain role/permission/approval hooks.
The existing snapshot 104's event payloads round-trip through the archival adapter
without wire changes. Native integration tests run a real child write and shell
acceptance, inspect peer authority and owner/workspace binding, deny the child
write, cancel a parked child before acceptance, refuse concurrent managed turns,
reuse the session after cancellation, and stop on foreign leases at round, worker
end and checkpoint boundaries. Stored events preserve masking and typed values.
The lease check at effect/event/return boundaries is an explicit native embedding
guard; it is not a heartbeat or proof of process fencing. Parent transcript does
not acquire child messages. Source live-model convergence is still untested.
HTTP remains 38/44 (35 patterns); Git proposal composition/POST, team/workflows,
native persistence and G0–G7 remain open.

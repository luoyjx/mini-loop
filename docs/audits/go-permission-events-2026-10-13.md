# Decision-time permission events — 2026-10-13

Based on `1b8e3e8` on `codex/go-permission-events`. This slice addresses the
permission-event gap recorded in the [preceding source review](go-port-remaining-boundaries-2026-10-13.md).
The legacy recovery-model default remains separate and pending.

## Implementation and evidence

- A named per-evaluation publisher emits each permission decision before the
  next rule, approval or tool effect. It is never retained on a shared policy.
- Session events carry the closed `permission` variant with `decision`, `rule`,
  `tool` and `reason`, together with the existing session/turn identity.
  Rule, tool and reason pass through the existing masking path before live
  delivery, SQLite storage and trajectory recording.
- Stored decoding accepts only `allow` and `deny`; restored events are historical
  observations and grant no execution or human-approval authority.
- The readonly reason matches Source (`risk=exec`, or `risk=None` for an
  unclassified tool). The separate denial reply remains unchanged.

The new producer runs the actual Python managed session, permission hook and
approval broker with a scripted provider and safe tool handlers. Fourteen cases
cover readonly/auto/interactive behavior, immutable refusal, two-rule approval
order, later approval failure/cancellation and secret-bearing tool identity.
Only session and message identity are normalized. The native managed comparison
checks ordered observations, all permission fields, identities, tool outcomes,
handler effects, masking and archive round trips. Native approval choices use a
typed callback; this comparison does not establish complete broker equivalence.

The existing twenty policy profiles now compare their twelve events. A retained
overlay of production code at `1b8e3e8` fails eight readonly reason assertions;
the implementation passes them. The new managed corpus additionally pins
`risk=None`. The initial native test setup omitted the constructor's required
Bash executor and failed before execution; that setup was corrected and its log
is retained.

Native integration separately verifies actual authenticated HTTP/SSE ordering
and owner isolation, plus a managed run followed by SQLite close/reopen and
physical trajectory JSONL inspection. A cancellation sink confirms that the
first published permission is retained while the second approval and handler
never execute. These integrations are native controls, not additional captured
Source profiles.

## Validation

The final machine-readable receipt is
[`go-permission-events-2026-10-13.json`](go-permission-events-2026-10-13.json).
Raw attempts, process samples and frozen-input hashes are retained under
`var/go-permission-events-2026-10-13/`.

Validation uses one heavy stage at a time: `nice -n 10`, `GOMAXPROCS=2`,
`GOFLAGS=-p=1`, `go test -parallel=2`, numerical-library threads set to one and
serial pytest. `GOMEMLIMIT=512MiB` is a soft Go target. The sampled owned-process
tree budget is 1 GiB; host memory pressure, sustained CPU above 250% for 30
seconds, a 30-minute stage limit or monitor failure stops only this task's tree.
One-second samples can miss short peaks and are not a hard OS allocation cap.

Complete frozen gates passed: Source export 254, four type audits, ordinary
Go (49 passing packages, two without tests; 1,417 passing top-level tests),
`go vet`, full race, and Python **2255 passed / 28 skipped / 96 warnings /
24 subtests passed**. Invariants cover 77 modules; all 19 scanning guards and
two selected permission mutation guards pass. The other mutation guards were
not rerun. The frozen 1,756 inputs were rechecked unchanged before committing.

Consumer audit first refused launch on host memory pressure. After a 30-second
normal-pressure window, one resumed run observed 253 of 255 passing candidates;
its strict exit 1 is retained for the existing IDNA and default-parser gaps.
The permission-event fixture has an observed passing native consumer.

The cohort sampled RSS peak was **552.7 MiB**. Python briefly reached **299.3%**
CPU; only three samples exceeded 250%, without a continuous 30-second overrun.
No unrelated processes were terminated. The original prelaunch refusal remains
recorded alongside the successful resumption.

[Package-local coverage](go-coverage-permission-events-2026-10-13.json) is app
**85.28%**, all Go **85.08%**, Provider **82.74%**, from the complete successful
race profile. Unlike the prior `-coverpkg=./...` profile, this run does not credit
cross-package test hits. The percentage difference is not a measured regression.

Source capture: `34576e0`; runtime, tests and canonical architecture: `9a20803`.
The separate documentation commit carries this receipt and the iteration image.

## Architecture and remaining scope

The README Mermaid and interactive JSON add the ToolGate-to-Session permission
event flow. The generated HTML is produced from that specification. The base
layered diagram retains its components; the
[iteration image](go-permission-events-infographic-2026-10-13.png) highlights the
changed execution and evidence paths and has a
[verbatim prompt](go-permission-events-infographic-2026-10-13.prompt.txt).
The built-in imagegen output was visually inspected. Archify passes 9/9 showcase
checks with zero errors/warnings after two focused label-placement repairs.
Rendered HTML inspection is unavailable after the earlier browser denial;
opening the file returned only a queued status, not a visual-review pass.

No dependency or feature-default change is included. Default CPython parser
approval and its native consumer, strict IDNA dependencies/consumers, the legacy
recovery fallback, fixed-environment Linux acceptance and the other original
P0/P1/G0–G7 requirements remain open. This slice does not complete migration.

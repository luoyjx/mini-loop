# Caller IDs as finite SDK observations

Review baseline: local main `0b83339`; implementation branch
`codex/go-caller-id-observations`. This is one G4/G7 provider contract slice.
Complete Python-to-Go migration acceptance remains open.

## Actual Source capture

The installed Anthropic SDK **0.107.1** was exercised through real
`messages.create` and `messages.stream`, with offline HTTP MockTransport.
The producer records raw messages/SSE and actual normalized content.
The original **48 cases remain identical**; **112 additional captures** cover
four caller-bearing block kinds, two known code-execution tags and seven ID
profiles in direct/SSE mode: empty text, missing, null, integer 513, boolean,
list and object. Total: **160**. Missing `tool_id` normalizes to null.

The fixed streaming SDK uses `ParsedTextBlock` as its web-fetch fallback class;
its dumped web-fetch tag, fields and caller observations remain intact. Tests
record that actual class rather than assuming the direct-mode concrete class.
This corpus does not establish unknown caller-kind or nonfinite-ID parity.

Before the native fix, the expanded consumer passed the original 48 captures
and failed all 112 additions. The retained `before-go-caller` stage exited 1
normally; this was a contract mismatch, not a resource stop.

## Native representation and boundaries

The shared `protocol.ToolCaller` stores known code caller IDs as the existing
named immutable `SDKMetadataText`. `ToolID()` returns text only;
`ToolIDMetadata()` exposes the closed JSON observation. Explicit
`CodeExecutionToolCaller` construction still requires nonempty text.
Direct caller `tool_id` remains an extension with no typed execution identity.
Unknown kinds, malformed discriminators, nonfinite IDs and invalid Unicode
remain refused; failed decoding preserves the previous receiver.

The decoder reads the immutable object directly so historical decoding does
not accidentally reapply the network byte cap. Four block codecs preserve
oversized finite historical IDs while bounded network admission rejects them.
All Source captures pass forwarding/archive, clone, detached-accessor and
masking comparisons. Existing SQLite tests now append all 160 rows, close the
database, reopen it and compare loaded messages with the captured content.

Eight Source client-tool ID profiles run through a real Session and ToolGate
with a safe test handler; the object profile also runs in readonly mode.
The handler receives the outer tool-use ID and trusted owner/session/run
context; action IDs, tool events, result pairing and next-request transcripts
retain that identity. Readonly permission denies the handler. These checks
create no operating-system shell effects or new authority from metadata.

## Validation and resource policy

The user's relaxed profile remains serial: `GOMAXPROCS=2`, build `-p=1`,
test `-parallel=2`, soft `GOMEMLIMIT=512MiB`, `GOGC=75`, nice 10, sampled
owned-tree RSS limit 1.5 GiB, sustained CPU limit 300% for 45 seconds and a
1,800-second stage timeout. Observed host pressure values 1/2 are admitted;
other or unreadable values stop the owned workload. No unrelated process was
terminated. Sampled RSS can miss short-lived children and is not a hard peak.

The first focused Go attempt caught the numeric getter projection and an old
SSE missing-ID rejection assertion. Both were corrected. The first Python
attempt caught the streaming web-fetch class assumption; the fixture and
producer were unchanged. Failed attempts remain in the receipt.

<!-- validation-results:start -->
| Gate | Result | Scope |
| --- | --- | --- |
| Complete Go tests + atomic coverage | passed | 51 package results; 1,438 top-level passes, 6 skips |
| Go vet | passed | `./...` |
| Focused caller Go | passed | 4 packages; 30 top-level passes, 571 subtests |
| Focused Source Python | passed | 1 test; 8 SDK serializer warnings |
| Complete Python | passed | 2,256 passed, 28 skipped, 104 warnings, 24 subtests |
| Source export | passed | 255 current files; fresh manifest |
| Invariants / scans | passed | 77 modules / 19 anchored scan guards |
| Four type audits | passed | syntax, public, private and variables; zero findings |
| Consumer inventory | failed, retained baseline gaps | 254/256 candidate passing-test associations |

Current atomic statement coverage: application **89.4334%** (33,652/37,628),
all Go **89.1854%** (34,018/38,143), provider **86.7882%** (1,524/1,756).
The 91,181,340-byte profile was summarized by streaming 1,305,423 rows and
merging 26,640 unique spans. Coverage includes CLI and excludes `/tools/` in the
application number. It does not measure branches, Python, C or subprocess
interiors. This is a new complete coverage run, not reused baseline coverage.
Current race was not rerun for this immutable codec change; the earlier full
race pass remains scoped to `f913d20`.

All complete gate input snapshots match. The Go tests took 242.475 seconds,
with sampled owned-tree RSS peak 565.08 MiB; Python took 94.719 seconds / 443.28
MiB, Source export 101.663 seconds / 403.14 MiB, vet 9.580 seconds / 537.05 MiB.
Short-stage sampling may miss the actual process peak. No budget stop occurred.

Consumer failure remains exactly `python-provider-idna.json` and
`python-syntax-parser.json`, both unchanged baseline prerequisites. Candidate
passing-test associations do not certify full field or migration parity.
Default parser/IDNA dependency choices, broader SDK fields and original
P0/P1/G0–G7 acceptance remain open.
<!-- validation-results:end -->

## Architecture and delivery evidence

README Mermaid remains canonical; its provider label and boundary explanation
now describe inert caller ID observations. The JSON specification was updated
and HTML regenerated with Archify: **9/9 showcase checks**, zero errors/warnings.
Rendered HTML visual inspection was not performed because the earlier browser
approval refusal remains in force. The brief renderer completed despite its
sandbox-denied nice request; monitored tests/audits use actual nice 10.

The built-in imagegen edit preserves the five-layer base and highlights the
L3 provider/L5 history scope. No architecture component was added or removed,
so the base PNG and its prompt remain unchanged. The iteration PNG was visually
inspected; its text and highlighted scope match this slice.

- [Iteration image](go-caller-id-observations-infographic-2026-10-14.png)
- [Exact built-in imagegen prompt](go-caller-id-observations-infographic-2026-10-14.prompt.txt)
- [Machine-readable receipt](go-caller-id-observations-2026-10-14.json)

Raw logs, sampled processes, before/after input hashes, failed attempts, coverage
profile and evidence scripts remain under
`var/go-caller-id-observations-2026-10-14/`. Receipt artifacts include byte counts
and SHA-256. No new dependency, default feature, component or remote push.


## Local main integration

Implementation commit `fa18e5a9585d50013fe8395137c9e45eced112d7` was
fast-forwarded into local main from `0b83339`. All 1,770 frozen runtime,
Source and canonical architecture inputs still match the complete tested
snapshot after integration. This final delivery update changes only audit and
planning documents. Work is local; no remote refresh or push occurred.

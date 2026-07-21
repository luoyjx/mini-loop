# Agent trajectories

mini-loop records every `session.run()` as one durable trajectory. A trajectory
is an append-only account of what the agent observed and did: model calls,
assistant output, tool actions and results, subagent activity, compaction,
recovery, and the final outcome.

## Data model

| Concept | mini-loop field | Meaning |
|---|---|---|
| Conversation/thread | `group_id` / `session` | Links multiple user runs in one session |
| Run/trace | `trajectory_id` / `trace_id` | One end-to-end user request |
| Ordered step | `seq` + `ts` | Stable session order and timestamp |
| Model span | `model_*` `span_id` | One provider call, including purpose, duration, stop reason, and usage when available |
| Tool span | `tool_*` `span_id` | Correlated `tool_use` and `tool_result`; `parent_span_id` points to the model call that requested it |

Recorded model events include the complete provider input/output payload, and
recorded tool results keep the complete observation. The live SSE/card payload
remains capped for browser performance; trajectory-only fields are removed
before an event is published to subscribers or custom `event_sink` consumers.

Each file uses the `mini-loop.trajectory.v1` schema and contains three JSONL
record kinds:

1. `trajectory_start` — identity, input, model, system prompt, tools, workspace;
2. `event` — the same canonical events delivered over SSE;
3. `trajectory_end` — status, duration, final output/error, and aggregate counts.

The recorder flushes every appended line. If the process stops before the end
record is written, the file remains readable and is returned as
`status: "interrupted"`, `partial: true`.

## Inspect and export

The web console lists trajectories for the active session. **View recording**
replays the recorded events into the event panel; it does not execute tools or
call the model again. JSON gives a convenient assembled object, while JSONL
preserves the append-only source records.

```sh
# list one session's runs
curl -s localhost:8000/sessions/$SID/trajectories | jq

# inspect one assembled trajectory
curl -s localhost:8000/trajectories/$TRAJECTORY_ID | jq

# export the canonical append-only file
curl -s "localhost:8000/trajectories/$TRAJECTORY_ID/export?format=jsonl" \
  -o "$TRAJECTORY_ID.jsonl"
```

`GET /trajectories?session_id=...&limit=...` can list recordings even after an
active session and its workspace have been deleted. By default, files live in
`MINILOOP_WORKSPACE_ROOT/.trajectories`.

## Privacy and retention

Trajectories are local, but they contain user prompts, system prompts, tool
inputs/outputs, and final responses by default. Newly created files use mode
`0600` and a newly created trajectory directory uses `0700`; configure
recording explicitly for the environment where mini-loop runs:

```sh
# move recordings to a dedicated volume
MINILOOP_TRAJECTORY_ROOT=/var/lib/mini-loop/trajectories

# retain timings and structure while replacing content-bearing fields
MINILOOP_TRAJECTORY_CAPTURE_CONTENT=0

# disable the built-in recorder entirely (SSE events still work)
MINILOOP_TRAJECTORIES=0
```

There is deliberately no automatic retention or deletion policy. Removing a
session workspace does not remove its trajectory; delete or archive the
trajectory directory according to your own audit policy.

## Design references and boundary

The schema borrows the trace/group/span split and sensitive-data control from
the [OpenAI Agents SDK tracing model](https://openai.github.io/openai-agents-python/tracing/),
the inspectable chronological steps and replay vocabulary from
[smolagents memory](https://huggingface.co/docs/smolagents/main/tutorials/memory),
and the durable thread/run distinction from
[LangGraph persistence](https://docs.langchain.com/oss/python/langgraph/persistence).

LangGraph checkpoints include executable state and can resume or fork a graph.
mini-loop trajectories currently record events for inspection and dataset/audit
export only. Viewing a recording never claims checkpoint recovery or
re-executes side effects.

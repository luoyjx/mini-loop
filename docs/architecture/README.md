# Focused architecture views

The canonical whole-system model remains the Mermaid in the repository
[README](../../README.md) and the generated
[interactive system map](../mini-loop-system.architecture.html). The large
[layered base image](../mini-loop-layered-architecture.png) is an overview and
navigation aid, not the preferred surface for reading individual labels.

These focused views split that overview by runtime or deployment path. They do
not introduce new components, connections, defaults, persistence claims, or
authority boundaries.

| View | Scope | Interactive | PNG preview | Editable source |
| --- | --- | --- | --- | --- |
| Python core | Entry, authority, session, agent, context, tools and state | [HTML](python-core.html) | [PNG](python-core.png) | [JSON](python-core.architecture.json) |
| Go runtime | Managed runtime, context, tool gate, child agents and evidence | [HTML](go-runtime.html) | [PNG](go-runtime.png) | [JSON](go-runtime.architecture.json) |
| Deep Agents server | Authenticated API, queue, worker, graph and persistence | [HTML](deepagents-server.html) | [PNG](deepagents-server.png) | [JSON](deepagents-server.architecture.json) |
| Temporal prototype | Default-off prototype workflow, activities and fixture | [HTML](temporal-prototype.html) | [PNG](temporal-prototype.png) | [JSON](temporal-prototype.architecture.json) |
| Current Task server | Production API, Temporal History, Workflow, Activity and trace | [HTML](task-server.html) | [PNG](task-server.png) | [JSON](task-server.architecture.json) |
| Go personal assistant | Channel, store, routing, workers, tools and connector | [HTML](personal-assistant.html) | [PNG](personal-assistant.png) | [JSON](personal-assistant.architecture.json) |

Use the HTML artifacts for zooming, path tracing, light/dark themes and export.
Use the PNGs for quick previews in Markdown or review tools. Regenerate each
HTML artifact from its adjacent `*.architecture.json`; do not hand-edit the
generated HTML.

All six specifications were validated with Archify's `showcase` profile against
repository revision `16ee882d9761d9b2c81e17214821da97e6e443b8`. Each passed
all nine layout checks with zero errors and zero warnings. Visual inspection used
the committed 1800 px PNG renders.

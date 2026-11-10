# P1.5/P1.6 architecture image audit

- Built-in imagegen edit, using `docs/mini-loop-layered-architecture.png` as the edit target.
- Canonical base SHA-256: `c219a78df123c75da7ce008b6999e47049ea49fcaea84930225c1988b1904503`.
- Iteration PNG SHA-256: `7eaad24468348d8bd7a3184d3a19e33c884fbb412ccda72db50cbd7925ff385b`.
- Output dimensions: 1375 × 1144, equal to the canonical base.
- Visual inspection: the five primary layers and the existing Deep Agents Server,
  Temporal prototype, Task Server and Go demo bands remain present. The only intended
  semantic addition is an orange, horizontally separated repository-only callout above
  the Task Server band.
- The callout states the exact bounded fault/protocol results and is not connected by an
  arrow to the runtime. It introduces no proxy, fixture, provider, service or persistence
  node and therefore does not change the canonical README Mermaid or architecture JSON.
- Existing small raster text retains the base image's stylized rendering. The new English
  callout is readable and does not cover the Task Server topology.

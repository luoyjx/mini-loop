# Architecture split evidence — 2026-11-11

The whole-system architecture was too dense to remain legible as one raster
image. This iteration preserves the canonical Mermaid, interactive system map,
topology and runtime behavior, while adding six focused Archify views under
`docs/architecture/`.

The views cover Python core, Go runtime, the standard Deep Agents server,
the default-off Temporal prototype, the current production Task server and the
default-off Go personal assistant. Each view keeps one primary execution path
and only the branches needed to explain that path.

The [iteration image](architecture-split-2026-11-11.png) edits the canonical
base diagram only to add a numbered focused-view navigation overlay. The exact
built-in ImageGen prompt is saved in
[architecture-split-2026-11-11.prompt.txt](architecture-split-2026-11-11.prompt.txt).
The canonical base image itself is unchanged because no architecture component
was added or removed.

Validation evidence:

- Six Archify specifications passed the `showcase` profile.
- Each artifact passed all nine layout checks with zero errors and zero warnings.
- The canonical interactive specification was regenerated after moving two
  Task-server trace labels/routes into clear corridors; this is a layout-only
  repair, and it also passes the `showcase` profile with zero errors or warnings.
- Repository evidence was resolved against revision
  `16ee882d9761d9b2c81e17214821da97e6e443b8`.
- Six 1800 px PNG previews were rendered and inspected as a contact sheet.
- The README architecture outline and repository whitespace checks are part of
  the final validation gates for this documentation-only iteration.

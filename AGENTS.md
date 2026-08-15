# Repository instructions

## Scope

These instructions apply to the entire repository unless a deeper `AGENTS.md`
overrides them.

## Read first

- Read the `README.md` architecture map before changing runtime structure,
  module ownership, request/tool flow, trust, persistence, or default behavior.
- Read `EXTENDING.md` before changing an injection seam and
  `docs/HARDENING_NOTES.md` before changing a load-bearing guard or invariant.
- Use `ast-outline` before full reads of supported source and Markdown files;
  narrow to the relevant symbol whenever possible.

## Architecture maintenance

- Review the README architecture map in every implementation iteration and
  update its visible review baseline in the same commit.
- When code changes affect topology, ownership, control/data flow, authority,
  persistence, public entry points, or feature defaults, update the Mermaid,
  its boundary explanation, and the interactive architecture specification in
  the same commit.
- Treat the README Mermaid as canonical. Regenerate
  `docs/mini-loop-system.architecture.html` from
  `docs/mini-loop-system.architecture.json`; never hand-edit the generated HTML.
- Preserve truthful status labels. Do not describe a default-off or
  process-local path as default-on or durable.

## Validation

- Run the narrowest relevant tests while iterating and
  `.venv/bin/python -m pytest -q` before merging or pushing implementation work.
- Run `.venv/bin/python tools/verify_invariants.py` after package-module changes.
- Run `.venv/bin/python tools/verify_scans.py` after changing source scanners,
  inventories, or their targets, and `.venv/bin/python tools/verify_guards.py`
  after changing guarded behavior or mutation anchors.
- Run `git diff --check` for every change. For architecture-only changes, also
  verify the README outline and regenerate/visually inspect the interactive map
  when the Archify tooling is available.

## Boundaries and delivery

- Preserve unrelated worktree files and stage only task-owned paths.
- Do not add dependencies without explicit user approval.
- After validation, commit repository changes and report the exact gates run;
  keep environment-blocked or skipped checks explicit.

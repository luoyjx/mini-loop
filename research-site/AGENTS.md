# Research site instructions

## Scope

These instructions apply to `research-site/`.

## Content authority

- Treat top-level `../docs/*.md` files as the only research-content authority.
- Read `README.md` and `scripts/build-research.mjs` before changing discovery,
  metadata, routes, or Markdown rendering.
- Do not hand-edit `app/data/research.generated.json`; run `npm run content`.
- Keep root cards and detail metadata derived from the same generated record.

## Boundaries

- Keep the site read-only and separate from the mini-loop runtime.
- Do not add third-party dependencies without explicit user approval.
- Preserve the vinext/Sites build and Cloudflare Worker-compatible output.
- Store only `project_id` plus optional logical `d1` and `r2` declarations in
  `.openai/hosting.json`.

## Validation

- Run `npm run content:check` after content-pipeline changes.
- Run `npm test` and `git diff --check` before committing site changes.

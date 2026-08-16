# mini-loop Research Atlas

Research Atlas is the browsable reading layer for the repository's maintained
research and design notes. The Markdown files remain authoritative; the site
is a generated projection that adds search, categories, reading metadata,
table-of-contents navigation, and related-document links.

## Content pipeline

`scripts/build-research.mjs` discovers every top-level `../docs/*.md` file,
except `AGENTS.md` and `README.md`. It derives the title, early conclusion,
category, tags, Git update date, reading time, sections, search text, and raw
Markdown into `app/data/research.generated.json`.

There is no manual document registry. To publish new research:

1. Add a top-level Markdown report under `docs/` with one H1 title.
2. Put a useful `结论先行`, `摘要`, `Outcome`, or equivalent paragraph near
   the beginning so the generated card has a meaningful summary.
3. Run `npm run content`, then validate with `npm test`.

Do not hand-edit `app/data/research.generated.json`; regeneration must be able
to reproduce it from repository documents.

## Local workflow

```bash
npm run dev
npm run content:check
npm test
```

`npm run dev` and `npm run build` regenerate the content index first. The site
uses the bundled vinext/Sites stack and intentionally adds no Markdown runtime
dependency: repository text is rendered with a constrained React renderer, so
raw HTML from research files is never executed.

## Deployment boundary

The site is a separate read-only Sites deployment. It does not import or call
the mini-loop runtime, does not write repository content, and has no database,
uploads, or app-owned authentication. `.openai/hosting.json` contains only the
Sites project identifier and optional logical binding declarations.

# Research document instructions

## Scope

These instructions apply to repository documents under `docs/`.

## Research publication

- Put maintained research reports at the top level as `docs/*.md` so Research
  Atlas discovers them automatically.
- Give each report one H1 title and an early `结论先行`, `摘要`, `Outcome`, or
  equivalent paragraph that can stand alone as its index summary.
- Keep facts, judgments, recommendations, source versions, and evidence
  boundaries explicit in research reports.
- After adding, renaming, or removing a top-level Markdown report, run
  `cd research-site && npm run content && npm test`.
- Do not edit `research-site/app/data/research.generated.json` directly.

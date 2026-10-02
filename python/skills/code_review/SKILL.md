---
name: code_review
description: Checklist and conventions for reviewing a code change before approval.
---

# Code Review

When asked to review a change, work through these in order and report findings
grouped by severity (blocker / should-fix / nit):

1. **Correctness** — does it do what it claims? Look for off-by-one, wrong
   conditionals, unhandled error paths, and resource leaks.
2. **Tests** — is the new behavior covered? Would the tests fail if the change
   were reverted?
3. **Security** — untrusted input reaching shell/SQL/filesystem; secrets in
   code; path traversal; missing authz checks.
4. **Clarity** — names match intent, no dead code, comments explain *why* not
   *what*.
5. **Scope** — the diff does one thing; unrelated churn is split out.

End with a one-line verdict: APPROVE / REQUEST CHANGES, and the single most
important thing to fix.

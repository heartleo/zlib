Pull requests here come from the maintainer and are squash-merged once the
required checks pass. The review is advice for that decision. Also check the
rules in .github/CONTRIBUTING.md: the title is a Conventional Commit of at most
50 characters, and the PR holds one logical change with its tests and docs.

Review correctness, regressions and credential boundaries in the changed code.
Read surrounding call sites and tests; report actionable findings introduced by
the change with file/line evidence, a concrete trigger and user-visible impact.
Prioritize credential routing, redirects, download confinement, overwrite
behavior, terminal escape handling, update verification, and Windows/macOS/Linux
differences when those paths change. For Bubble Tea screens inspect selection,
resize behavior and keyboard navigation. For CLI/MCP changes check output and
read-only behavior. Avoid style-only findings and speculative vulnerabilities.

Re-reviews cover new commits and whether earlier findings still apply. State
unrun checks. Leave comments only; maintainers own approvals and integration.

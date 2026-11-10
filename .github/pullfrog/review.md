This repository does not merge pull requests. Begin the review by saying so in
one sentence: the findings are advice for the maintainer, who may reimplement
the change directly. Do not imply the PR will be merged.

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

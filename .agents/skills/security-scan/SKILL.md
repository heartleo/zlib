---
name: security-scan
description: Scan zlib for security problems before a release or after a dependency, network, credential, or file-handling change — govulncheck, secret scanning, GitHub alert review, and a credential-boundary code review — and handle a private vulnerability report through a GitHub security advisory.
---

# Security scanning

Run every scan below before tagging a release, after changing `go.mod`, and
after touching cookies, EAPI headers, redirects, domain policy, downloads,
`zlib update`, or anything under `~/.config/zlib/`. Report each finding with
its ID, the affected package or file, whether the code reaches it, and the
fixed version. Do not drop findings you judge harmless; mark them instead.

Every commit already runs govulncheck and a staged gitleaks scan through the
mandatory pre-commit hook (`scripts/check.sh`, see `.github/CONTRIBUTING.md`). The
steps below go further: full history, GitHub alerts and the manual review.

## 1. Go vulnerabilities (govulncheck)

```sh
go install golang.org/x/vuln/cmd/govulncheck@v1.9.0
govulncheck ./...
go version   # must match the go line in go.mod
```

govulncheck checks the standard library of whatever `go` it runs. Check
`go version` against the `go` line in `go.mod`. A `go run …@latest` that
prints `switching to go1.x.y` scans that version instead, which can hide
vulnerabilities in the toolchain releases are actually built with. To check a
shipped binary, run `go version <path-to-zlib>` and
`govulncheck -mode=binary <path-to-zlib>`.

Fix by layer:

- Standard library: raise the `go` line to the latest patch release
  (`go mod edit -go=1.x.y`). CI and releases install exactly that version
  through `setup-go`'s `go-version-file`, and the README badge reads the same
  line. Do not rely on a `toolchain` line for this.
- Module: `go get <module>@<fixed version>`.
- Then run `go mod tidy`, `go vet ./...`, `go test ./...`, and scan again.
  CI fails when `go mod tidy` changes `go.sum`.

A "your code doesn't appear to call" result still needs the upgrade when one
exists, but it does not block a release.

CI runs the same scan on pushes and pull requests (`govulncheck` job in
`.github/workflows/ci.yml`) and daily on main (`govulncheck.yml`). Keep the
pinned govulncheck version the same in both and in this file.

## 2. Secrets

`gitleaks.yml` scans every push and PR, and the whole history weekly. Both
gitleaks commands read `.gitleaks.toml` (default rules plus `remix_userkey`
values and real home-directory paths) and `.gitleaksignore` (reviewed
historical findings) from the repository root. Locally, scan the history and
the working tree, including untracked files that might be committed next, then
the documentation:

```sh
gitleaks git --redact -v .
gitleaks dir --redact -v .     # also reports gitignored local files; check each path
node scripts/check-docs.mjs
```

`check-docs.mjs` finds what secret scanners do not: local paths, the current
OS username, emails and private IPs in Markdown, skills and local notes. Rules
and the leak procedure: `AGENTS.md#documentation-privacy`.

Tests use fake values only (`fake-user`, `FAKE-KEY`). Real `remix_userid` or
`remix_userkey` values, passwords, SMTP credentials, or proxy addresses must
never appear in code, fixtures, docs, or commit messages. A leaked secret stays
in history: rotate it first. Rewriting history needs the maintainer's
approval.

## 3. GitHub alerts

These read-only checks need a `gh` login with access to the repository:

```sh
gh api repos/heartleo/zlib/private-vulnerability-reporting      # enabled: true
gh api repos/heartleo/zlib/security-advisories --jq '.[] | [.ghsa_id, .state, .severity, .summary] | @tsv'
gh api repos/heartleo/zlib/dependabot/alerts --jq '.[] | select(.state=="open")'
gh api repos/heartleo/zlib/secret-scanning/alerts --jq '.[] | select(.state=="open")'
gh api repos/heartleo/zlib/code-scanning/alerts --jq '.[] | select(.state=="open")'
```

A 404 or 403 means the feature is off or the token lacks scope; report that
instead of reading it as "no alerts".

## 4. Credential-boundary review

Scanners do not find logic flaws such as credentials crossing an origin
boundary; those pass every tool above. Read the changed code against these
rules:

- Account cookies and `remix-*` headers go only to the configured mirror's
  origin, with matching scheme and host. Find every place that attaches
  them: `grep -rn -e AddCookie -e remix-user -e cookieSnapshot --include=*.go .`.
  Each request built from a server-supplied URL, such as an EAPI file link,
  needs an explicit origin check before credentials are added.
- Every redirect hop is checked again (`checkRedirect` and any client that
  sends credentials), including HTTPS-to-HTTP downgrades on the same host.
  net/http strips only `Authorization`, `Cookie`, and `WWW-Authenticate`, and
  only when the domain changes.
- A new mirror needs `ParseAllowedDomain` and a measured entry in
  `allowedDomainSuffixes`. Never trust a host just because an API returned it.
- File writes stay inside the chosen directory (`sanitizeFilename`), go through
  a temporary file, and never replace an existing file before the transfer
  completes.
- `zlib update` installs only official release assets through the detected
  channel.
- `--json`, logs, and error messages never print cookie values or passwords.

Add a regression test for each fixed boundary that records which credentials
each host received. Use an in-memory `RoundTripper` or `httptest`, never a
live mirror. Confirm that the test fails without the fix.

## 5. Handling a private report

1. Reply only in the advisory thread. Keep details out of issues, commits on
   public branches, and PRs until the advisory is published.
2. Reproduce with fake credentials and confirm the affected version range
   with `git show <tag>:<file>`. Releases older than the reported one are often
   affected too.
3. Fix in the advisory's temporary private fork. A commit pushed to public
   `main` discloses the issue before users can upgrade.
4. Set the CVSS vector and CWE, release, then publish the advisory and credit
   the reporter. Pushing, publishing, and requesting a CVE all need the
   maintainer's explicit approval.

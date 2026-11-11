# Contributing

zlib is maintained by one person. Every change reaches `main` through a pull
request from the maintainer; only collaborators can open pull requests. To
report a bug, propose a feature or offer a fix, open an issue. A fix you
describe or attach there is reviewed and committed by the maintainer, with a
`Co-authored-by` trailer when your design is used.

## Development workflow

The workflow is kept small: one short-lived branch per change, one pull
request per branch, one squashed commit on `main` per pull request.

### Branches

- `main` is always releasable. Releases are tags on `main`; there are no
  long-lived development or release branches.
- Branch from the latest `main` and name the branch `<type>/<topic>`, using
  the commit types below: `fix/download-percent`, `feat/weread-import`,
  `docs/pr-workflow`.
- Keep a branch to days, not weeks. Rebase it on `main` when it falls behind;
  do not merge `main` into it.
- The branch is deleted when its pull request merges.

### Commits

- Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/):
  `type(scope): description`, with the subject at most 50 characters and no
  body unless it explains something the diff cannot. Types: `feat`, `fix`,
  `perf`, `refactor`, `test`, `docs`, `build`, `ci`, `chore`.
- Each commit on a branch passes the pre-commit checks below. Commits on a
  branch are working history; squash merging decides what lands on `main`.

### Pull requests

- **One logical change per pull request.** A change and its tests, docs and
  site updates go together. Unrelated changes, even small ones, get their own
  pull requests. Changes that would be wrong on `main` without each other go
  together.
- **The title is the commit.** It becomes the subject of the squashed commit
  and a line in the release notes, so it follows the commit rules above,
  including the 50-character limit (the `commit-msg` hook does not see it).
  `feat`, `fix`, `perf` and `build` titles appear in the release notes;
  `docs`, `test`, `ci`, `chore` and `refactor` do not (`.goreleaser.yml`).
- Fill in the template: what changed, why, and how it was verified. Link the
  issue it resolves with `Fixes #123`.
- Open a draft while work is in progress; mark it ready when it is.

### Review

- Read the full diff on GitHub before merging, as a reviewer would: scope,
  naming, error paths, tests, docs and leftover debugging.
- Pullfrog's review comments are advice. Resolve each one with a change or a
  reply saying why not.
- Changes listed under [Security changes](#security-changes) also need the
  credential boundary review before merging.

### Testing

- `make check` passes locally (the pre-commit hook runs it on every commit).
- A bug fix starts with a test that fails without the fix.
- Tests do not depend on the machine: no network, real credentials, fixed
  terminal sizes from the environment, or assumptions about the length or
  location of the checkout or temporary directory.
- Terminal UI changes: also run the walkthrough in
  [docs/tui.md](../docs/tui.md#verification) and look at the changed screens
  at 80x24.
- Site changes: the checks under [Website](#website).

### Merging

- Merge with **Squash and merge** only, after the required checks pass:
  `ci-ok` and `gitleaks`. `ci-ok` succeeds only when every `ci` job it needs
  succeeded; it is the one to require, because a skipped matrix job reports
  as `test` rather than `test (ubuntu-latest)` and would never satisfy a rule
  that names the platform jobs.
- The squashed commit is the title alone, with no ` (#123)` suffix (GitHub
  adds one by default, which breaks the 50-character limit) and no body:
  `gh pr merge 123 --squash --subject "<title>" --body ""`, or edit both
  fields in the web form. The pull request number is still linked from the
  commit on GitHub. The one exception is credit: when a contributor's design
  is used, the body holds their `Co-authored-by: Name <email>` trailer and
  nothing else.
- Before merging, rebase on `main` if it moved, so the checks ran on what
  will land.
- Dependabot pull requests follow the same rules; adjust the title to the
  commit rules when needed.
- A broken `main` is fixed or reverted through a pull request first, before
  other work merges.

### Releasing

Tag the squashed commit on `main` as described in
[docs/versioning.md](../docs/versioning.md#releasing), check the draft
release, then publish it. Publishing redeploys the site, which shows the
latest published release version (`.github/workflows/pages.yml`).

## Pre-commit checks

Security scanning and cross-platform testing are mandatory before every
commit. Enable the hook once per clone:

```sh
make hooks   # git config core.hooksPath .githooks
```

`.githooks/pre-commit` runs `scripts/check.sh`, which you can also start with
`make check`. A commit is blocked unless every step passes:

| Step | What it checks |
| --- | --- |
| Toolchain | `go` matches the `go` line in `go.mod`, so scans cover the shipped standard library |
| `gofmt -l .` | Formatting |
| `go mod tidy -diff` | `go.mod` and `go.sum` are tidy |
| `sync-plugin-skills.mjs --check` | Plugin skills match `.agents/skills` |
| `check-docs.mjs` | `docs/` is English; docs and skills contain no local paths, usernames, emails or credentials ([rules](../AGENTS.md#documentation-privacy)) |
| `node --test scripts/*.test.mjs` | The node scripts: the Pullfrog settings script, and the rule that decides which changes need the Go jobs in CI |
| `go vet` × 6 | Type-checks linux, darwin and windows on amd64 and arm64, including build-tagged files and tests |
| `go test -race ./...` | Tests on your OS (without `-race` when cgo is off) |
| `govulncheck ./...` | Known vulnerabilities in reachable code, standard library included |
| `gitleaks git --pre-commit --staged` | Secrets in the staged changes, with the rules in `.gitleaks.toml` |

Required tools: Go (the `go.mod` release), Node.js 22+,
`go install golang.org/x/vuln/cmd/govulncheck@v1.9.0`, and
[gitleaks](https://github.com/gitleaks/gitleaks#installing) 8.19 or later.

`.githooks/commit-msg` also rejects a commit subject longer than 50 characters.

Do not bypass the hook with `git commit --no-verify`. If a step fails, fix the
cause. When govulncheck reports a vulnerability, follow
[the security-scan skill](../.agents/skills/security-scan/SKILL.md). Network
errors from govulncheck are not a pass: retry.

CI repeats these checks on pull requests and on `main`. It also runs the tests
natively on Linux, macOS and Windows, because path handling, file names and
terminal detection differ between them. Code that touches one of those must be
correct on all three. Branch on `runtime.GOOS` behind a parameter, as
`sanitizeFilenameFor` does, so every platform's branch is tested on any OS.

The Go jobs (`build`, `test`, `govulncheck`) run only when a change can affect
the Go build or tests. `scripts/ci-changes.mjs` decides: they are skipped only
if every changed file is on its list of paths that no Go code or test reads
(docs, the site, skills, plugin manifests, the other workflows, the node
tooling). Anything else, including a new directory or file, runs them. When a
Go test starts reading a file, take that path off the list. The `checks` job
(skills, docs privacy, script tests) runs on every change, and
`govulncheck.yml` still scans `main` daily. A new push to a pull request
cancels its older run; runs on `main` are never cancelled.

## Issue forms

Issue forms follow [GitHub's form syntax](https://docs.github.com/en/communities/using-templates-to-encourage-useful-issues-and-pull-requests/syntax-for-issue-forms).
Their version/reproduction fields draw on the
[GitHub CLI bug template](https://github.com/cli/cli/blob/trunk/.github/ISSUE_TEMPLATE/bug_report.md)
and terminal context on the
[Bubble Tea bug form](https://github.com/charmbracelet/bubbletea/blob/main/.github/ISSUE_TEMPLATE/bug.yml).
Feature requests ask for the user problem, proposed behavior and alternatives;
usage questions have their own form. Security reports go to private advisories.

## Pullfrog automation

Issue triage and PR review comments come from Pullfrog. Setup, settings and
activation are in [docs/pullfrog.md](../docs/pullfrog.md).

## Security changes

After changing cookies, EAPI headers, redirects, domain policy, downloads,
`zlib update`, or files under `~/.config/zlib/`, also run the credential
boundary review in the security-scan skill. Treat all text from a mirror as
untrusted: it must not reach the terminal with control characters (see
`internal/zlib/text.go`) or leave the download directory.

## Website

`site/` is static and outside `make check`, so check it by hand before
committing a change to its copy, type, spacing or layout:

- Follow the [text layout rules](../docs/site.md#text-layout): breaks fall
  between sentences, then between words (Chinese words included), never
  inside one, and no line is left with one or two words.
- Run `node scripts/build-zh.mjs`, serve `site/`, and run
  `scripts/check-site-text.mjs` (it needs `PUPPETEER_CORE` and `CHROME`;
  see `docs/site.md#verifying-a-change`). It must report nothing.
- Look at the changed blocks in a browser at 375, 768 and 1440px, in English
  and Chinese.

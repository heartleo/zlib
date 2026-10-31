# Contributing

zlib takes no pull requests. Changes are committed directly, so every commit
must pass the checks below on the committer's machine first.

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
| `go vet` × 6 | Type-checks linux, darwin and windows on amd64 and arm64, including build-tagged files and tests |
| `go test -race ./...` | Tests on your OS (without `-race` when cgo is off) |
| `govulncheck ./...` | Known vulnerabilities in reachable code, standard library included |
| `gitleaks git --pre-commit --staged` | Secrets in the staged changes, with the rules in `.gitleaks.toml` |

Required tools: Go (the `go.mod` release), Node.js 22+,
`go install golang.org/x/vuln/cmd/govulncheck@v1.9.0`, and
[gitleaks](https://github.com/gitleaks/gitleaks#installing) 8.19 or later.

Do not bypass the hook with `git commit --no-verify`. If a step fails, fix the
cause. When govulncheck reports a vulnerability, follow
[the security-scan skill](../.agents/skills/security-scan/SKILL.md). Network
errors from govulncheck are not a pass: retry.

CI repeats these checks on every push. It also runs the tests natively on
Linux, macOS and Windows, because path handling, file names and terminal
detection differ between them. Code that touches one of those must be correct on
all three. Branch on `runtime.GOOS` behind a parameter, as
`sanitizeFilenameFor` does, so every platform's branch is tested on any OS.

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

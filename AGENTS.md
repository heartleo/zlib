# AGENTS.md

Instructions for AI coding agents working in this repository. Contribution
rules and the mandatory pre-commit checks are in
[.github/CONTRIBUTING.md](.github/CONTRIBUTING.md).

## Documentation privacy

These rules apply to every file in the repository and to issue, PR and commit
text. Treat anything committed as public forever: deleting it later does not
remove it from history, forks or tags.

### Layers

| Layer | What it catches | Where |
| --- | --- | --- |
| `node scripts/check-docs.mjs` | Local paths, the current OS username, emails, private IPs and credential values in every Markdown, skill, tape and example file (plus `docs/handoff.md` and `CLAUDE.md`); non-English text in `docs/` | `scripts/check.sh`, CI |
| gitleaks with `.gitleaks.toml` | Built-in secret rules, plus `remix_userkey` values and paths into a real home directory, in every file type | `scripts/check.sh` (staged), `gitleaks.yml` (pushes, PRs, weekly full history) |
| GitHub secret scanning and push protection | Provider tokens, server side | Repository settings |
| Review | Names in prose, screenshots, GIF and PNG content | You, before every commit |

Never weaken a check, add a broad allowlist or use `--no-verify` to get a file
through. Fix the file.

### Rules

- **English only in `docs/`.** Research notes included. The only Chinese
  documents are `README.zh.md`, the generated `site/zh/` and the `repo-readme`
  skill templates. Quoting non-English strings as test data needs an entry in
  `cjkAllowed` in `scripts/check-docs.mjs` with the reason.
- **No absolute local paths.** Never write a path from your machine: no drive
  letters (`<drive>:\...`), no `/Users/<name>` or `/home/<name>`, no Git Bash or
  WSL drive paths (`/d/...`, `/mnt/d/...`), no temp or config directories. Use a
  repository-relative path (`internal/zlib/eapi.go`), a link to the upstream
  project, or a placeholder such as `<temp-dir>`, `<jadx-dir>`,
  `<crush-checkout>` or `<claude-config-dir>`. Generic forms such as
  `~/.config/zlib/` and `C:\Users\<name>` are fine.
- **Reserved names in examples.** Domains from RFC 2606/6761 (`example.com`,
  `.example`, `.test`, `.invalid`), IPs from RFC 5737 (`192.0.2.0/24`,
  `198.51.100.0/24`, `203.0.113.0/24`) or `127.0.0.1`, and placeholder accounts
  (`you`, `user`, `test`). Kindle examples may use `reader@kindle.com`.
- **No personal or environment details.** No OS usernames, real names, personal
  email addresses, hostnames, internal IPs, machine-specific environment
  variable values or local tool setups.
- **No credentials.** Never record `remix_userid`/`remix_userkey` values,
  passwords, SMTP passwords, Cookie headers, session files, tokens, signed
  download URLs or constants extracted from third-party apps. Show the field
  name and a placeholder (`remix_userkey=<key>`). Test fixtures use obviously
  fake values (`FAKE-KEY`, `fixture`), never real ones.
- **Research docs carry technical content only.** No analysis dates, session
  narrative ("this round", "the user provided", "at the time"), branch history
  or tool chatter. Identify sources by version, revision or checksum, and keep
  statements about zlib true for the current code.
- **Measurements describe shape, not content.** When a doc records live
  responses, keep status codes, key names and counts; leave out account data,
  book titles, raw bodies and download links.

### When something leaks

1. **Credential** (session key, password, token): treat it as compromised.
   Ask the owner to revoke it (log out of Z-Library sessions, change the
   password) first; removing it from the repository comes second.
2. **Fix the current files** and run both checks.
3. **History** is rewritten only with the maintainer's explicit approval
   (`git filter-repo`, then a force push that also moves tags). For a
   non-credential finding the maintainer chooses not to rewrite, add its
   fingerprint to `.gitleaksignore` with a comment. Never ignore a credential.

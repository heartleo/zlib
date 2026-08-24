# Handoff

Single rolling handoff document. Overwrite this file rather than creating dated copies.

**Last updated:** 2026-08-24 (branch head `d283e72`)

---

## Current state

| | |
| --- | --- |
| Working branch | `fix/diamwall-detection-and-challenge` @ `d283e72` |
| `main` | `b07bc99` — identical to `origin/main`, untouched |
| Pushed? | **No.** Nothing has left the machine. The branch is local-only. |
| Working tree | clean |
| Verification | `gofmt -l .` clean · `go vet ./...` clean · `go test -race ./...` → **179 pass** (as of `89416cc`; the two commits added since are docs/dependency-only, no source changed) |

Commit stack on top of `main`:

```
d283e72  chore: bump dependencies to latest patch releases
e1a30d9  docs: streamline Quick Start EAPI login
89416cc  docs: document the challenged domain status
b8fd539  fix: report challenged domains as usable in doctor
0481fcb  fix: narrow DiamWall detection and unblock the JS challenge
---------- main / origin/main ----------
b07bc99  docs: update README.md
d0a7e8b  feat: add EAPI mode and domain diagnostics
```

## What this branch fixes

It is the follow-up to a review of `d0a7e8b` (the EAPI + doctor feature commit). That commit's anti-bot check misfired in both directions and left the HTML transport unusable against the primary mirror.

### Availability blockers (all three were live bugs)

- **HTTP 503 challenge was swallowed.** `validateResponse` ran before `isChallengePage`, and Z-Library serves its "Checking your browser" proof-of-work with **503** — so `solveChallenge` was unreachable and every HTML request failed as `unexpected HTTP status 503`. Split `validateStatus` out of `validateResponse`; the order in `fetchOnce` is now bot-protection → challenge → login page → status.
- **Unbounded challenge recursion.** `get()` ended in a bare `return c.get(rawURL)`. Each retry restarts the HTTP timeout, so a stubborn endpoint became a command that printed nothing and never returned. Now a bounded loop: `maxChallengeAttempts = 10`, exponential backoff, `ErrChallengeFailed` sentinel.
- **`fatal error: concurrent map writes`.** `FetchBookDetails` fanned out one goroutine per id over a shared `Client` while `get()` wrote `c_token`. Added `cookiesMu` with `cookieSnapshot()`/`setCookie()`; the fan-out now warms up on the first id alone (so the rest share its token), runs the remainder through a `detailFetchConcurrency`-wide pool, and skips them if the warm-up fails.

### DiamWall detection, narrowed

The old detector was `strings.Contains(lower(body), "diamwall")` over every response, plus a `__diamwall` `Set-Cookie` check in the probe. Both were wrong:

- `__diamwall` is a **clearance** cookie — DiamWall issues it on requests it *allows*. Keying on it reported every reachable mirror as `diamwall_blocked`. It now counts only at the redirect-loop exits, where the chain never resolves.
- A bare body substring matches legitimate content: a search echoes the query into the results page, and a book title can contain the word. Detection now anchors on the interstitial `<title>` and skips the body entirely for non-HTML `Content-Type` — which is what keeps the JSON EAPI path from ever tripping it.
- Added status **513** alongside 517. Without it a genuinely blocked mirror burned the redirect budget and was misreported as `ErrRedirectLoop`.

### `doctor` was calling the *working* mirrors broken

The challenge interstitial is 503, so `doctor` classified the mirrors that serve it as `http_error`. Added `DomainStatusChallenged`, checked before the status classification and coloured as success.

Measured 2026-08-24 against the live list (`curl` confirmed the 503 bodies carry `<title>Checking your browser ...</title>` and `c_token`):

| Mirror | Before | After |
| --- | --- | --- |
| `z-lib.gd` | `http_error` 503 | **`challenged`** — usable |
| `z-lib.gl` | `http_error` 503 | **`challenged`** — usable |
| `zliba.ru` | `network_error` | **`challenged`** — usable |
| `1lib.sk`, `z-lib.fm`, `z-lib.sk` | `diamwall_blocked` 517 | unchanged — genuine block |
| `z-library.gs` | `network_error` EOF | unchanged |
| `z-library.ec` | `http_error` 403 | unchanged |

### EAPI

- Non-2xx responses carrying `{"success":0,"error":"..."}` now reach the caller, so a real message ("daily limit reached") survives instead of collapsing into a bare status error. Guarded by `isEAPIEnvelope` so a non-JSON 502 still fails on status.
- `searchEAPI` falls back to the requested page when `pagination.current` is absent — a zero `Page` froze the interactive pager (`←` dead via the `page > 1` guard, `→` refetching page 1).
- `resolveEAPIFileURL` no longer discards the `url.Parse` error, which returns a **nil** `*URL` and panicked on the next line.

### CLI

- `--cookies` rejects a file carrying no `remix_userid`/`remix_userkey` instead of saving a session that fails much later as an opaque "session expired".
- `zlib login --password X` no longer signs into the remembered account without showing it. The remembered address is only a form prefill; the prompt decision is made from the flags *before* the prefill is applied.

### Docs / deps (e1a30d9, d283e72)

Two follow-up commits, orthogonal to the fixes above — no source under test changed:

- **`e1a30d9`** — Quick Start's EAPI login now uses `zlib login --eapi --domain https://z-lib.gd` as the single copy-pasteable path; the `ZLIB_DOMAIN` export form is kept as a commented-out alternative instead of a second, literally-executable `zlib login --eapi` line (the original edit had both lines live, so a top-to-bottom copy-paste logged in twice).
- **`d283e72`** — `go.mod`/`go.sum` dependency bump (go-isatty, go-runewidth, charmbracelet/x/ansi, go-colorful, xo/terminfo, golang.org/x/{net,sys,text}), all to their latest patch releases. This was **not** a deliberate `go mod tidy` — it's drift from a machine-wide `GOFLAGS=-mod=mod`, which lets every `go build`/`go test`/`go vet` run this session silently rewrite `go.mod`. Kept intentionally as a formal bump rather than reverted; if `GOFLAGS=-mod=mod` isn't wanted globally, future sessions on this machine will keep tripping this.

Three minor README review findings were **left as-is per instruction**, not fixed: an English comment inside `README.zh.md`'s Quick Start block that should be Chinese to match the rest of the file, dropped inline comments on the two `zlib search` examples, and an EN/ZH content drift ("web domains" vs "domains" in the Quick Start intro sentence).

## How it was verified

- **179 tests pass under `-race`.** Every fix above has a regression test.
- The two concurrency tests were **confirmed to fail first** — locks were temporarily removed and both reported `DATA RACE` — then confirmed passing with the locks restored. A green race test that was never seen red proves nothing.
- **Live end-to-end:** a temporary test drove `Client.get()` against `https://z-lib.gd` and cleared the real challenge **3 runs out of 3** (CLAUDE.md warns against concluding from a single run on this endpoint). The temp file was deleted afterwards. A real EAPI `search` returned live results.

## Open items

1. **Nothing is pushed.** Decide between pushing `fix/diamwall-detection-and-challenge` and opening a PR, or continuing locally. Per standing instruction, a push needs explicit approval each time.
2. **`CLAUDE.md` is gitignored** (`.gitignore:41`) and untracked. It was substantially expanded this session — the EAPI dual-transport architecture, authentication paths, `doctor` status semantics, and the `.env` precedence trap were all undocumented. **All of that lives only on this machine.** Decide whether to un-ignore it so the documentation reaches the repo.
3. **A review finding was rejected as a false positive, deliberately.** The reviewer flagged the domain allowlist as breaking the `ZLIB_DOMAIN` escape hatch, claiming the README uses `z-library.sk`. It does not — `README.md:253-260` publishes the suffix table, and the allowlist is intentional hardening that stops a stolen session shipping `remix_userkey` to an attacker-controlled host. No code change made; CLAUDE.md now records that a new mirror legitimately requires a code change.

## Traps found the hard way

- **CLAUDE.md documented code that had never been committed.** `maxChallengeAttempts`, `challengeRetryBase`, `ErrChallengeFailed`, `cookiesMu`, `cookieSnapshot()`, `detailFetchConcurrency` — `git log -S` across all history returned zero hits for every one of them. The doc described an intended design as though it shipped. This branch makes the code match the doc. Treat unfamiliar symbols in CLAUDE.md as claims to verify, not facts.
- **A repo-local `.env` silently overrides the saved session domain.** `.env` is loaded from the working directory *and* `~/.config/zlib/`, and `ZLIB_DOMAIN` there beats `session.json`. A command that unexpectedly targets the wrong mirror is usually this — it sent a search to the blocked `z-lib.sk` during this session even though the session file said `z-lib.gd`.
- **Two `statusline-command.sh` files existed and only one ran.** `CLAUDE_CONFIG_DIR` is `D:\Claude\.claude` (so that `settings.json` is the live one), but its command string was `bash ~/.claude/statusline-command.sh`, and bash expands `~` from `$HOME` = `C:\Users\liyaoxin`. Edits to the D: copy had no effect. Both `settings.json` files now use the absolute path `bash D:/Claude/.claude/statusline-command.sh`. The orphaned copy at `C:\Users\liyaoxin\.claude\statusline-command.sh` still needs deleting (a permission rule blocked it); nothing references it.

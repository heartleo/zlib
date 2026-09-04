# zlib

A CLI for Z-Library.

![Go version](https://img.shields.io/badge/go-1.25%2B-blue)
[![CI](https://img.shields.io/github/actions/workflow/status/heartleo/zlib/release.yml)](https://github.com/heartleo/zlib/actions)
[![Release](https://img.shields.io/github/v/release/heartleo/zlib)](https://github.com/heartleo/zlib/releases)
[![Downloads](https://img.shields.io/github/downloads/heartleo/zlib/total)](https://github.com/heartleo/zlib/releases)
![License](https://img.shields.io/badge/license-MIT-green)

English | [中文](README.zh.md)

![search demo](docs/demo-search.gif)

## Features

🔍 **Interactive search** — browse results with `↑/↓`, switch pages with `←/→`  
📥 **Book download** — by book ID, from search results, with live progress  
📚 **Download history** — paginated history browser with download support  
📖 **Send to Kindle** — deliver files to your Kindle address  
🕒 **Usage profile** — view daily download quota  
🎨 **Themes** — auto, mocha, latte, dracula, tokyo, nord, gruvbox  
🌐 **Proxy & custom domain** support for restricted networks  

## Install

**Homebrew** (macOS / Linux):

```bash
brew install heartleo/tap/zlib
```

**winget** (Windows):

```powershell
winget install heartleo.zlib
```

**curl** (macOS / Linux):

```bash
curl -fsSL https://raw.githubusercontent.com/heartleo/zlib/main/install.sh | sh
```

**Go install** (requires Go 1.25+):

```bash
go install github.com/heartleo/zlib/cmd/zlib@latest
```

**Build from source:**

```bash
git clone https://github.com/heartleo/zlib
cd zlib
go build -o zlib ./cmd/zlib
```

### Claude Code plugin

zlib ships a [Claude Code](https://claude.com/claude-code) plugin, so Claude can search and download books for you.

```
/plugin marketplace add heartleo/zlib
/plugin install zlib@zlib-plugin-cc
```

`/zlib "the pragmatic programmer"` to search and download.
`/zlib:kindle` sends a book to your Kindle.

### Codex plugin

zlib also ships a skills-only plugin for Codex:

```bash
codex plugin marketplace add heartleo/zlib
codex plugin add zlib@zlib
```

Ask Codex to search or download a book with zlib. Use the bundled Kindle skill
when you want to send a local book file to your Kindle.

## Quick Start

> **Set `ZLIB_DOMAIN` first.** Z-Library mirrors change often and most of them
> are behind DiamWall, so there is no default that stays correct. Every command
> resolves its mirror from `ZLIB_DOMAIN` before anything else, and a wrong or
> stale value is the single most common cause of a login that succeeds and a
> next command that fails.

Most current Z-Library web domains put automated requests behind DiamWall, so
the recommended setup uses EAPI. Probe the candidates first, then set
`ZLIB_DOMAIN` to one reported as `healthy` or `challenged`:

```bash
zlib doctor --eapi
export ZLIB_DOMAIN=https://z-lib.gd     # pick one doctor reported usable
zlib login --eapi
zlib search "dune"
```

A successful login records that domain as `ZLIB_DOMAIN` in
`~/.config/zlib/.env`, so the setting survives a new shell and you do not have
to remember it. Other keys in that file are left untouched. Two things still
outrank it, and a stale value in either shadows what the login wrote: a real
environment variable, and a `.env` in the working directory.

`challenged` means the domain answered with Z-Library's own JS
proof-of-work page. That domain is usable: the client solves the challenge
automatically, so pick it as readily as a `healthy` one. Only
`diamwall_blocked`, `http_error`, `redirect_loop` and `network_error` are
unusable.

EAPI login checks the selected domain again before asking for account
credentials. If no candidate is usable, see
[Troubleshooting DiamWall](#troubleshooting-diamwall-issue-16).

## Commands

### login

![login demo](docs/demo-login.gif)

```bash
zlib login
zlib login --email you@example.com --password secret
zlib login --cookies ~/Downloads/cookies.txt --domain https://z-lib.gd
zlib login --eapi --domain https://z-lib.gd
# or set ZLIB_DOMAIN=https://z-lib.gd, then:
zlib login --eapi
```

Saves session to `~/.config/zlib/session.json`, and records the domain it used
as `ZLIB_DOMAIN` in `~/.config/zlib/.env` so later commands reach the same
mirror without you setting anything.
The interactive login remembers and prefills the last successful email address,
but never stores the password. `--cookies` imports a user-supplied
Netscape/Mozilla-format cookie file; only unexpired root cookies matching
`--domain` are saved to the session. Cookie files contain reusable credentials,
so keep the exported file private.

If you omit `--domain`, both HTML and EAPI login read `ZLIB_DOMAIN`. When neither
is set, the command prints the required setting and exits. EAPI login also checks
the selected domain before asking for account credentials. If the check fails,
it tells you to choose another domain. Cookie imports follow the same rule.

`--eapi` selects the unofficial mobile app API and saves that choice in the
session. Search, profile, history, book details, downloads, and Kindle delivery
then use the EAPI path where needed. Book references use `id:hash`, for example
`zlib download 115066162:e9f13b --send-to-kindle`. See
[EAPI mode](docs/eapi.md) for the endpoint list and stability notes.

If `/eapi/user/login` refuses to issue credentials, `--eapi` login falls back to
the HTML login form and continues in EAPI mode. The EAPI authenticates on
nothing but `remix_userid`/`remix_userkey`, and the HTML form hands out the same
pair, so the fallback session is not degraded. See
[EAPI login says `Authorization failed`](#eapi-login-says-authorization-failed).

### logout

```bash
zlib logout
```

### search

![search demo static](docs/demo-search-static.gif)

Without arguments, opens an interactive picker:

- type a query and confirm
- browse results with `↑/↓`
- switch pages with `←/→`
- press `Enter` to download

```bash
zlib search # interactive mode
zlib search "dune" --page 2 # static table
zlib search "dune" --json   # machine-readable output for scripts and plugins
```

Filter by file format with `--ext` (or its alias `--format`), repeatable.

```bash
zlib search "python crash course" --ext epub --ext pdf
```

Use `--full-title` to disable title truncation

```bash
zlib search "civilized to death" --full-title
```

### download

```bash
zlib download Gz31nyAV5E
zlib download Gz31nyAV5E --dir ./books --send-to-kindle
zlib download Gz31nyAV5E --dir "~/Downloads"
```

`--dir` expands a leading `~` itself, so it works whether or not the shell expanded it.

Press `Ctrl+C` to cancel.
Incomplete files are removed automatically.

### history

![history demo](docs/demo-history.gif)

Without flags, opens an interactive history browser:

- browse with `↑/↓`, switch pages with `←/→`
- press `Enter` to re-download

```bash
zlib history
zlib history --download Gz31nyAV5E --dir ./books
zlib history --format epub
zlib history --json   # machine-readable, always non-interactive
```

### profile

![profile demo](docs/demo-profile.gif)

```bash
zlib profile
zlib profile --json
```

### doctor

Checks all known domains without logging in or changing the active domain.
`--proxy` overrides `ZLIB_PROXY` for this check only.

```bash
zlib doctor
zlib doctor --eapi
zlib doctor --proxy http://127.0.0.1:7890
zlib doctor --proxy socks5://127.0.0.1:9050 --json
```

`--eapi` checks `/eapi/info/domains` instead of the home page. A domain may block
the website while leaving EAPI reachable, so the two checks can disagree.

### Troubleshooting DiamWall (issue #16)

In [issue #16](https://github.com/heartleo/zlib/issues/16), the mirror serves a
DiamWall challenge where the CLI expects Z-Library HTML or JSON. You may see an
HTTP `307` redirect loop, HTTP `517`, or `Access Denied | DiamWall`. Older CLI
versions may instead fail with `searchResultBox not found`, `dstats-info not
found`, or `invalid character '<'` while decoding login JSON.

1. Check both website and EAPI connectivity:

   ```bash
   zlib doctor --json
   zlib doctor --eapi --json
   ```

2. Set `ZLIB_DOMAIN` to a domain reported as `healthy` or `challenged`, then log
   in with EAPI:

   ```bash
   export ZLIB_DOMAIN=https://z-lib.gd
   zlib login --eapi
   zlib search "golang"
   ```

3. If your network requires a proxy, run the check through that proxy before
   setting it for other commands:

   ```bash
   zlib doctor --eapi --proxy http://127.0.0.1:7890
   export ZLIB_PROXY=http://127.0.0.1:7890
   ```

`challenged` means the domain served Z-Library's own JS proof-of-work page; the
client clears it automatically, so the domain is usable. `diamwall_blocked` means
DiamWall intercepted the request. `redirect_loop` means the redirects never
reached content. `http_error` covers responses such as `403`; `network_error`
covers DNS, TLS, timeout, reset, and EOF failures. Changing the User-Agent or
importing login cookies does not solve a DiamWall JS/TLS challenge. Use a
`healthy` or `challenged` EAPI domain, or a different network route.

### EAPI login says `Authorization failed`

```text
Error: login failed: zlibrary: login failed: Authorization failed
```

Z-Library gates `/eapi/user/login` separately from the rest of the EAPI. An
account it refuses this way still drives `/eapi/user/profile`,
`/eapi/book/search` and `/eapi/user/book/downloaded` normally. The message is
also not a wrong password: a wrong one comes back `Incorrect email or password`
instead.

The CLI handles this on its own by logging in through the HTML form and
continuing in EAPI mode, so on a current build the login simply succeeds. If you
see the message, you are on a build older than that fallback — upgrade, or
import a browser cookie file instead:

```bash
zlib login --cookies ~/Downloads/cookies.txt --domain https://z-lib.gd
```

`Incorrect email or password` really is a credential failure. Note that a
password containing shell metacharacters can be mangled by `--password`; omit
the flag and let the prompt take it.

### Commands fail after a successful login

```text
Error: zlibrary: blocked by anti-bot protection (HTTP 517) at https://z-lib.sk/...
Error: zlibrary: unexpected HTTP status 503 at https://z-lib.gd/users/downloads
```

A `ZLIB_DOMAIN` left over from an earlier setup overrides the domain the login
just saved, so `zlib login --domain https://z-lib.gd` can succeed and the very
next command still target the old mirror. Check the effective value before
suspecting anything else — `.env` files in the working directory and in
`~/.config/zlib/` both feed it:

```bash
echo "$ZLIB_DOMAIN"
grep -r ZLIB_DOMAIN .env ~/.config/zlib/.env 2>/dev/null
```

Point it at a domain `zlib doctor --eapi` reports as `healthy` or `challenged`,
or remove it entirely to fall back to the domain saved in the session.

A bare `503` from a `healthy` or `challenged` domain means the build predates
the challenge fix and is treating Z-Library's own proof-of-work page as an
error. Upgrade.

### Recommended access domains

Measured 2026-09-04 by probing `/eapi/user/login` on each host. The list is
maintained from measurement rather than from Z-Library's own
`/eapi/info/domains`, which advertises hosts that are DiamWall-fronted and
omits several that work.

| Allowed domain | EAPI (measured 2026-09-04) |
| -------------- | -------------------------- |
| `z-lib.gd`     | works (EAPI default)       |
| `z-lib.gl`     | works                      |
| `z-library.ec` | works (EAPI only; the HTML site is Cloudflare-blocked) |
| `zlib.bz`      | works                      |
| `article.sk`   | works                      |
| `articles.sk`  | works                      |
| `zliba.ru`     | redirects to `zlib.bz`; `login --eapi` follows it |
| `1lib.sk`      | DiamWall-blocked           |
| `z-lib.fm`     | DiamWall-blocked           |
| `z-lib.sk`     | DiamWall-blocked           |

`z-library.gs` was removed: its TLS handshake fails. Run `zlib doctor --eapi`
for the current picture rather than trusting this table, which ages.

### kindle

![kindle demo](docs/demo-kindle.gif)

Configure Kindle delivery settings:

- recipient Kindle address
- sender address
- SMTP host and port

SMTP password is never stored on disk — set `ZLIB_SMTP_PWD` instead.

```bash
zlib kindle                  # configure
zlib kindle send             # pick a file interactively
zlib kindle send ./dune.epub # send a local file
```

Supported formats: `EPUB` `PDF` `MOBI` `TXT` `DOC` `DOCX` `RTF` `HTML`

### theme

```bash
zlib theme           # show current
zlib theme auto      # follow terminal background
zlib theme nord      # set globally
```

Available: `auto` · `mocha` · `latte` · `dracula` · `tokyo` · `nord` · `gruvbox`

## Configuration

Keep secrets like `ZLIB_SMTP_PWD` out of your shell history — put them in an `.env` file instead of exporting them inline. zlib reads env vars with this precedence: **real environment > working-directory `.env` > `~/.config/zlib/.env`**. The global file is the recommended home for machine-wide values.

HTML and EAPI both read `ZLIB_DOMAIN`. It overrides the domain saved in `~/.config/zlib/session.json`; if you unset it, the CLI uses the saved domain. A new login without `--domain` requires `ZLIB_DOMAIN`. EAPI login checks that domain before requesting account credentials.

A successful login writes `ZLIB_DOMAIN` into `~/.config/zlib/.env`, replacing the assignment already there and leaving every other line alone. That is the lowest-precedence of the three sources, so an exported variable or a working-directory `.env` still wins over it — check those first when a command targets a mirror you did not choose.

| Variable                | Description                                                     |
| ----------------------- | --------------------------------------------------------------- |
| `ZLIB_DOMAIN`           | Domain for HTML and EAPI; used when login omits `--domain`      |
| `ZLIB_PROXY`            | Proxy URL, e.g. `http://127.0.0.1:7890`                         |
| `ZLIB_SMTP_PWD`         | SMTP password for Kindle delivery                               |
| `ZLIB_THEME`            | Override theme without changing config                          |
| `ZLIB_DOWNLOAD_RETRIES` | Retries for dropped connections and `502`/`503`/`504` responses |

`ZLIB_DOWNLOAD_RETRIES` defaults to `3`.

**Edit the global env file** (`~/.config/zlib/.env`, one `KEY=value` per line):

```bash
mkdir -p ~/.config/zlib && touch ~/.config/zlib/.env && chmod 600 ~/.config/zlib/.env
# open it in your editor:
notepad "$env:USERPROFILE\.config\zlib\.env"   # Windows (PowerShell)
open -t ~/.config/zlib/.env                       # macOS
${EDITOR:-nano} ~/.config/zlib/.env               # Linux
```

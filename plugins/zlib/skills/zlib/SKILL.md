---
name: zlib
description: Search and download books from Z-Library. Wraps the zlib CLI to search by title/author, download by book ID, browse download history, and check daily download limits. Use when the user wants to find, download, or browse books from Z-Library. For sending books to Kindle, use the zlib:kindle skill instead.
argument-hint: "<query-or-command> [book-id]"
allowed-tools: Bash, Read, AskUserQuestion
homepage: https://github.com/heartleo/zlib
repository: https://github.com/heartleo/zlib
author: heartleo
license: MIT
user-invocable: true
---

# /zlib

This skill lets you drive [Z-Library](https://github.com/heartleo/zlib) through the `zlib` CLI. `zlib` is a compiled binary the user already has on their PATH — you call it with Bash and read its plain-text output. Several `zlib` subcommands open an interactive prompt that waits for keyboard input when run bare; **you can't answer those and they will hang**, so this skill uses only the non-interactive command forms documented below.

## Before anything: two preflight checks

Run both once at the start of a session, in a single command:

```bash
command -v zlib >/dev/null 2>&1 && echo "BIN_OK" || echo "BIN_MISSING"
zlib profile 2>&1 | head -5
```

1. **Binary present?** If you see `BIN_MISSING`, stop and tell the user to install it, then wait:
   > `zlib` is not on your PATH. Install it with `go install github.com/heartleo/zlib/cmd/zlib@latest` (or `brew install heartleo/tap/zlib`), then ask me again.

2. **Logged in?** `zlib profile` prints the user's download limits when authenticated. If instead you see `Not logged in. Run: zlib login` or `session expired, run \`zlib login\``, stop and give the user this friendly handoff, then wait:
   > 🔐 Looks like you're not signed in to Z-Library yet. Open a **separate terminal window** (any shell — not this chat), run `zlib login`, and complete the prompts it shows. Once it says login succeeded, come back here and ask me again — I'll pick up where we left off.

   **Never run `zlib login` yourself.** Its interactive prompts need a real terminal — inside this chat there is no TTY, so it cannot work here (same pattern as `gh auth login`: authentication happens in the user's own terminal, and the CLI persists the session to `~/.config/zlib/session.json` for later commands). Do not pass credentials as flags either — that leaks them into shell history. Your job is only the friendly nudge above. Reuse this same handoff whenever any command (search, history, profile, download) reports not-logged-in / session-expired mid-session — don't invent a new message each time. (An email/SMTP password exists too, but that belongs solely to the `zlib:kindle` skill — never bring it up around login.)

## Golden rule: never run a bare interactive command

These forms open an interactive prompt and will hang waiting for input. **Do not run them:**

- `zlib search` (no query)
- `zlib history` (no `--page`, `--format`, or `--download`)
- `zlib kindle` / `zlib kindle send` (no file)

Always pass the arguments below that force plain-text / non-interactive output.

## Commands

### Search — `zlib search "<query>" --json`

Prefer `--json`: one JSON document on stdout (`{"books":[…],"page":n,"total_pages":n}`), each book with `id` (the value `zlib download` accepts), `name`, `authors`, `year`, `extension`, `size`, `rating`. No table to parse, no truncated titles.

**Always pass `--count 10`.** Each result carries long URL/cover fields (~1 KB per book), so the default 50 bloats the JSON to ~50 KB. Ten at a time keeps it small; use `--page 2` etc. when the user wants more.

```bash
zlib search "the pragmatic programmer" --json --count 10
zlib search "dune" --json --count 10 --page 2
zlib search "python crash course" --json --count 10 --ext epub --ext pdf   # --ext repeatable; --format is an alias
```

**Parse, never truncate.** Structured output must be parsed and projected down to the fields you need — never piped through `head -c` (a truncated JSON document loses data *and* won't parse). One call handles success, not-logged-in, and malformed output alike:

```bash
zlib search "<query>" --json --count 10 | python -c "
import json, sys
raw = sys.stdin.read()
try:
    d = json.loads(raw)
    print(f\"page {d['page']}/{d['total_pages']}\")
    for b in d['books']:
        print('|'.join([b['id'], b['name'][:70], ';'.join(b.get('authors', []))[:40],
                        str(b.get('year', '')), b.get('extension', ''), b.get('size', ''), str(b.get('rating', ''))]))
except Exception:
    print(raw[:500])  # not JSON (not logged in / error page) -> show the message as-is
"
```

If `python` is missing on the user's machine, just read the raw JSON directly — at `--count 10` it is small enough. The same parse-first rule applies to `history --json` and `profile --json`.

Flags: `--json`, `-p/--page` (default 1), `-n/--count` (default 50 — always override to 10), `--ext`/`--format` (repeatable extension filter), `--full-title` (table only).

**Always relay results to the user as a markdown table** built from the JSON (columns: #, ID, Title, Authors, Year, Format, Size, Rating) — never dump raw JSON at the user. Include every row; if an entry looks suspect (spam collection, placeholder metadata), keep it and add a note rather than dropping it. If `--json` is not recognized (older binary), fall back to the plain table form — a query argument alone already forces static output — and read IDs from the table's ID column.

To go from a search result straight to a file, take the ID from the table and call `download` below. (`zlib search` also accepts `--dir`/`--send-to-kindle`, but prefer the explicit `download` command so the ID is unambiguous.)

### Download — `zlib download <book-id>`

**Ask where to save first.** Unless the user already named a folder, use `AskUserQuestion` before downloading — offer the common landing spots and let "Other" take a custom path. Resolve each option to a **platform-correct absolute path** and show it in the option description so the user sees exactly where the file will land:

| Option | Windows | macOS | Linux |
|--------|---------|-------|-------|
| Downloads (recommended) | `%USERPROFILE%\Downloads` | `~/Downloads` | `~/Downloads` (or `xdg-user-dir DOWNLOAD` if available) |
| `./books` in current dir | same everywhere — resolve to absolute | | |
| Desktop | `%USERPROFILE%\Desktop` | `~/Desktop` | `~/Desktop` — **may not exist on server/minimal installs; offer only if the directory exists** |
| *Other* | user types any path | | |

Notes:
- Detect the platform first (`uname -s` in bash: `Linux` / `Darwin` / `MSYS`/`MINGW` = Windows Git Bash).
- `--dir` expands a leading `~` itself, so `--dir "~/Downloads"` is safe; expand `%USERPROFILE%` yourself. `--dir` does not create the folder — `mkdir -p` it first.
- On headless Linux (no Desktop dir, SSH session), drop the Desktop option rather than offering a path that doesn't exist.
- Remember the chosen folder for the rest of the session — don't re-ask on every download; re-confirm only if the user switches context.

Downloads to the current directory unless `--dir` is given. When it detects no terminal (i.e. you running it), it prints a plain `Downloading…` / `Saved to: <path> (<n> bytes)` and exits cleanly with code 0. Just run it and read the printed path:

```bash
zlib download Gz31nyAV5E --dir ./books
ls -la ./books/                       # optional: confirm the file landed
```

Flags: `-d/--dir` (default `.`), `--send-to-kindle`.

> Compatibility: older zlib builds (before the no-TTY plain path) render an interactive progress display and can hang without a terminal. If a `download` ever fails to return, the user is on an old binary — wrap it defensively and verify by the file instead of the exit code: `timeout 180 zlib download <id> --dir <dir> </dev/null >/dev/null 2>&1` then `ls` the dir. A complete file of the size shown in the search table is a success. Suggest they upgrade (`go install github.com/heartleo/zlib/cmd/zlib@latest`).

### History — `zlib history --json`

Prefer `--json`: it always forces the static path (never the interactive browser, even bare) and prints `{"items":[…],"page":n,"total_pages":n}` with each item's `id`, `name`, `extension`, `size`, `date`. Relay it as a markdown table (same rule as search — complete rows, never raw JSON).

```bash
zlib history --json                          # list past downloads as JSON
zlib history --json --page 2 --format epub   # paginate / filter
zlib history --download Gz31nyAV5E --dir ./books   # re-download a book from history
```

`--download` uses the same download path as `zlib download` — it exits cleanly with no TTY, same as above.

Flags: `--json`, `-p/--page`, `-f/--format`, `-D/--download <book-id>`, `-d/--dir`, `--send-to-kindle`.

Older binary without `--json`: bare `zlib history` opens an interactive browser — always pass `--page`/`--format` to force the static table.

### Profile — `zlib profile --json`

Shows the daily download quota. `--json` prints `{"daily_amount":n,"daily_allowed":n,"daily_remaining":n,"daily_reset":"…"}`; without it, a styled card. Also doubles as the login check in preflight.

### Kindle delivery → the `/zlib:kindle` skill

Sending a downloaded file to Kindle (and the SMTP/Amazon setup it needs) lives in a separate skill, `zlib:kindle`. When the user wants a book on their Kindle, hand off to it rather than documenting SMTP here. `zlib download --send-to-kindle` also delivers in one step once Kindle is configured.

## Typical flow

1. Preflight (binary + login).
2. `zlib search "<what the user wants>"` → show the table.
3. If several matches, use `AskUserQuestion` to let the user pick, or infer the best ID from title/author/format/size.
4. `zlib download <id> --dir <dir>` → read the printed `Saved to:` path and report it.
5. If the user wants it on their Kindle, use the `zlib:kindle` skill.

## Output & errors

- `zlib` output is plain text / Unicode tables — just relay the relevant rows; don't dump raw ANSI.
- On `session expired` / `Not logged in` mid-session, the session lapsed — reuse the **same friendly separate-terminal `zlib login` handoff** from preflight #2 (never pass credentials as flags), then wait for the user.
- Network/domain errors: Z-Library mirrors change often. If requests fail with a domain/connection error, have the user set `ZLIB_DOMAIN` (a working mirror) or `ZLIB_PROXY` by editing their env file — never on the command line. Create/point them at it:

  ```bash
  mkdir -p ~/.config/zlib; touch ~/.config/zlib/.env; chmod 600 ~/.config/zlib/.env
  echo "Edit: $(cd ~/.config/zlib && pwd)/.env  (add ZLIB_DOMAIN=… or ZLIB_PROXY=…, one per line)"
  ```

  Open helpers: Windows `! notepad "$USERPROFILE\.config\zlib\.env"` · macOS `! open -t ~/.config/zlib/.env` · Linux `! ${EDITOR:-nano} ~/.config/zlib/.env`. Then retry.
- Respect the user's daily download limit shown by `zlib profile`; don't loop downloads past it.

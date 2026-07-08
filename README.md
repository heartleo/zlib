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

- 🔍 **Interactive search** — browse results with `↑/↓`, switch pages with `←/→`
- 📥 **Book download** — by book ID, from search results, with live progress
- 📚 **Download history** — paginated history browser with download support
- 📖 **Send to Kindle** — deliver files to your Kindle address
- 🕒 **Usage profile** — view daily download quota
- 🎨 **Themes** — auto, mocha, latte, dracula, tokyo, nord, gruvbox
- 🌐 **Proxy & custom domain** support for restricted networks

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

**Prebuilt binaries** — download from [GitHub Releases](https://github.com/heartleo/zlib/releases)

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

## Quick Start

```bash
zlib login
zlib search        # interactive mode
zlib search "dune" # static table
```

## Commands

### login

![login demo](docs/demo-login.gif)

```bash
zlib login
zlib login --email you@example.com --password secret
```

Saves session to `~/.config/zlib/session.json`.

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
```

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

| Variable        | Description                             |
| --------------- | --------------------------------------- |
| `ZLIB_DOMAIN`   | Override the default Z-Library domain   |
| `ZLIB_PROXY`    | Proxy URL, e.g. `http://127.0.0.1:7890` |
| `ZLIB_SMTP_PWD` | SMTP password for Kindle delivery       |
| `ZLIB_THEME`    | Override theme without changing config  |

**Edit the global env file** (`~/.config/zlib/.env`, one `KEY=value` per line):

```bash
mkdir -p ~/.config/zlib && touch ~/.config/zlib/.env && chmod 600 ~/.config/zlib/.env
# open it in your editor:
notepad "$env:USERPROFILE\.config\zlib\.env"   # Windows (PowerShell)
open -t ~/.config/zlib/.env                       # macOS
${EDITOR:-nano} ~/.config/zlib/.env               # Linux
```

## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=heartleo/zlib&type=Date)](https://star-history.com/#heartleo/zlib&Date)

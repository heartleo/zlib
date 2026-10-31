<div align="center">

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/logo-dark-theme.svg">
  <source media="(prefers-color-scheme: light)" srcset="docs/logo-light-theme.svg">
  <img src="docs/logo-light-theme.svg" alt="zlib" width="260">
</picture>

A Z-Library CLI for you and your AI agents.

<a href="go.mod"><img src="https://img.shields.io/github/go-mod/go-version/heartleo/zlib" alt="Go version" height="20"/></a> <a href="https://github.com/heartleo/zlib/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/heartleo/zlib/ci.yml" alt="CI" height="20"/></a> <a href="https://github.com/heartleo/zlib/releases/latest"><img src="https://img.shields.io/github/v/release/heartleo/zlib" alt="Release" height="20"/></a> <a href="https://github.com/heartleo/zlib/releases"><img src="https://img.shields.io/github/downloads/heartleo/zlib/total" alt="Downloads" height="20"/></a> <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-green" alt="License" height="20"/></a>

<a href="https://trendshift.io/repositories/135168?utm_source=repository-badge&amp;utm_medium=badge&amp;utm_campaign=badge-repository-135168" target="_blank" rel="noopener noreferrer"><img src="https://trendshift.io/api/badge/repositories/135168" alt="heartleo%2Fzlib | Trendshift" width="250" height="55"/></a>

English | [简体中文](README.zh.md)

[Install](#install) · [Quick start](#quick-start) · [Terminal interface](#terminal-interface) · [Commands](#commands) · [Agent setup](#agent-setup) · [Docs](#documentation)

</div>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/search.gif">
  <source media="(prefers-color-scheme: light)" srcset="docs/search-light.gif">
  <img src="docs/search.gif" width="100%" alt="Book search in the terminal">
</picture>

## Features

🔍 **Search** — filter by year, language, and format; sort results\
📚 **Browse** — favorites, recommendations, popular books, and download history\
📥 **Download** — view book details and choose a download directory\
📖 **Send to Kindle** — deliver files to your [Kindle address](docs/kindle.md)\
🕒 **Profile** — check your daily download quota\
🎨 **Themes** — switch color themes with `/theme`\
🌐 **Network** — find working mirrors with `zlib doctor`, trust [your own mirror](docs/configuration.md#your-own-mirrors), or use a proxy\
🤖 **AI Agent Integration** — integrate with Claude Code and Codex through [Agent plugins](#agent-setup)

## Install

macOS / Linux with Homebrew:

```bash
brew install heartleo/tap/zlib
```

macOS / Linux with the install script (installs to `/usr/local/bin`, may ask for `sudo`):

```bash
curl -fsSL https://raw.githubusercontent.com/heartleo/zlib/main/install.sh | sh
```

Windows with winget:

```powershell
winget install --id heartleo.zlib --exact
```

Or download a binary from [Releases](https://github.com/heartleo/zlib/releases/latest)
and put it on your `PATH`. See [Installation](docs/installation.md) for Go
installation, source builds, and upgrades.

## Quick start

You need a Z-Library account. First, find a domain reachable from your network:

```sh
zlib doctor
```

Log in with a domain from the results. Replace the example URL below with one
from your results; enter your email and password in the prompt:

```sh
zlib login --domain https://z-lib.gd
zlib tui
```

Press `/`, choose `/search`, enter a title, author, or ISBN, and press `Ctrl+S`.
You can also type `/search dune` and press `Enter`. Select a book and press `Enter`
to confirm the download.

Downloads default to the current directory. You can choose another folder before
downloading. Login is saved locally; subsequent sessions only need `zlib tui`.

If no domain works or login fails, see [Troubleshooting](docs/troubleshooting.md).
Existing browser sessions can be [imported as cookies](docs/troubleshooting.md#login-returns-authorization-failed).

## Terminal interface

`zlib tui` opens one persistent shell. Every screen lists its keys at the bottom;
these cover a first session:

| Key            | Action                                                                       |
| -------------- | ---------------------------------------------------------------------------- |
| `/`            | Open the command list: search, favorites, history, profile, settings, themes |
| `↑/↓` or `j/k` | Select a book                                                                |
| `←/→`          | Previous or next page of results                                             |
| `Enter`        | Download the selected book, after a confirmation                             |
| `Ctrl+F`       | Change the query and filters of the current search                           |
| `Esc`          | Go back                                                                      |
| `/quit`        | Exit                                                                         |

Settings live under `/config` (network, downloads, Kindle). See
[Terminal interface](docs/tui.md) for every screen and key.

## Commands

Pass a query for table output, or add `--json` for scripts:

```sh
zlib search "dune" --ext epub
zlib search "dune" --ext epub --json --count 10
```

| Task                             | Command                        |
| -------------------------------- | ------------------------------ |
| Browse favorites                 | `zlib favorites`               |
| Browse recommendations           | `zlib recommended`             |
| Browse popular books             | `zlib most-popular`            |
| Browse download history          | `zlib history`                 |
| Check account and quota          | `zlib profile`                 |
| Send to [Kindle](docs/kindle.md) | `zlib kindle send ./book.epub` |

To download, replace `id:hash` with a book's complete `id` from
the results:

```sh
zlib download "id:hash"
```

Run `zlib <command> --help` for flags. [Usage](docs/usage.md) covers filters,
pagination, and download options.

## Agent setup

Install the plugin, or register the MCP server (`zlib mcp`). If `zlib` is
missing, the plugin installs it on first use. Log in yourself with `zlib login`
so your password stays out of chat.

### Claude Code

Plugin:

```text
/plugin marketplace add heartleo/zlib
/plugin install zlib@zlib-plugin-cc
```

### Codex

Plugin:

```sh
codex plugin marketplace add heartleo/zlib
codex plugin add zlib@zlib
```

### MCP server

Register `zlib mcp` as a local stdio server. `zlib` must be on `PATH` and signed in:

```sh
# Claude Code
claude mcp add -s user zlib -- zlib mcp

# Codex
codex mcp add zlib -- zlib mcp
```

Add `--read-only` after `mcp` to hide download, history deletion, and Kindle
delivery. See [MCP server](docs/mcp.md) for other clients, flags, and tools.

## Documentation

| Guide                                      | Covers                                         |
| ------------------------------------------ | ---------------------------------------------- |
| [Installation](docs/installation.md)       | Install methods, source builds, `zlib update`  |
| [Usage](docs/usage.md)                     | Search, downloads, favorites, history          |
| [Terminal interface](docs/tui.md)          | Commands and keyboard controls                 |
| [Configuration](docs/configuration.md)     | Domains, proxies, folders, configuration paths |
| [Themes](docs/themes.md)                   | Available palettes and preview controls        |
| [Kindle](docs/kindle.md)                   | SMTP setup and delivery                        |
| [MCP server](docs/mcp.md)                  | Client setup, flags, and tools for `zlib mcp`  |
| [Troubleshooting](docs/troubleshooting.md) | Login and network problems                     |

Report bugs in [Issues](https://github.com/heartleo/zlib/issues) with your
`zlib version`, OS, command, and error. Omit passwords, cookies, and session files.
Report security vulnerabilities privately as described in [SECURITY.md](.github/SECURITY.md).

## Star History

<a href="https://www.star-history.com/?repos=heartleo%2Fzlib&type=date&logscale=&legend=top-left">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/chart?repos=heartleo/zlib&type=date&theme=dark&logscale&legend=top-left" />
   <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/chart?repos=heartleo/zlib&type=date&logscale&legend=top-left" />
   <img alt="Star History Chart" src="https://api.star-history.com/chart?repos=heartleo/zlib&type=date&logscale&legend=top-left" />
 </picture>
</a>

# MCP server

[README](../README.md) · [Agents and scripts](agents.md)

`zlib mcp` runs a local stdio [Model Context Protocol](https://modelcontextprotocol.io)
server. It exposes search, book details, feeds, history, quota, domain checks,
downloads and Kindle delivery as tools with structured inputs and outputs. It
uses the same session (`~/.config/zlib/session.json`) and `.env` settings as the
CLI.

## Requirements

- `zlib` on your `PATH` ([Installation](installation.md)).
- A session created in your terminal with `zlib login`. Keep the password out of
  the agent chat.
- An MCP client that runs local stdio servers.

Check both before registering:

```sh
zlib version
zlib profile --json
```

## Install

Most clients accept this standard config:

```json
{
  "mcpServers": {
    "zlib": {
      "command": "zlib",
      "args": ["mcp"]
    }
  }
}
```

Desktop apps may not see your shell `PATH`. If a client cannot start `zlib`, set
`command` to the full path from `command -v zlib` (`where zlib` on Windows).

### Claude Code

```sh
claude mcp add -s user zlib -- zlib mcp
claude mcp get zlib
```

`-s user` registers the server for every project; omit it to register it for the
current project only.

### Codex

```sh
codex mcp add zlib -- zlib mcp
codex mcp list
```

Or add to `~/.codex/config.toml`:

```toml
[mcp_servers.zlib]
command = "zlib"
args = ["mcp"]
```

### Gemini CLI

```sh
gemini mcp add -s user zlib zlib mcp
gemini mcp list
```

### VS Code

```sh
code --add-mcp '{"name":"zlib","command":"zlib","args":["mcp"]}'
```

Or add to `.vscode/mcp.json`:

```json
{
  "servers": {
    "zlib": {
      "command": "zlib",
      "args": ["mcp"]
    }
  }
}
```

### Cursor

Add the standard config to `~/.cursor/mcp.json`, or to `.cursor/mcp.json` for
one project.

### Claude Desktop

Add the standard config to `claude_desktop_config.json`, with the full path to
`zlib` as `command`, then restart the app:

- macOS: `~/Library/Application Support/Claude/claude_desktop_config.json`
- Windows: `%APPDATA%\Claude\claude_desktop_config.json`

## First prompt

> Use zlib to show my remaining daily downloads, then search for "Dune" in EPUB
> and list the first 5 results.

The agent should call `get_profile` and `search_books`. If it reports that you
are not logged in, run `zlib login` and ask again.

Headless runs:

```sh
codex exec "Use zlib MCP to search for 'Dune' in EPUB and list the top 3 IDs."
claude -p "Use mcp__zlib__search_books to find 'Dune' in EPUB and list the top 3 IDs." --allowedTools "mcp__zlib__search_books"
```

## Configuration

Add flags after `mcp`, for example `"args": ["mcp", "--read-only"]`:

| Flag | Effect |
| --- | --- |
| `--read-only` | Expose only read tools: no download, history deletion, Kindle or WeRead delivery |
| `--disable-download` | Hide `download_book` |
| `--disable-history-delete` | Hide `delete_download_history` |
| `--disable-kindle` | Hide `send_to_kindle` and block delivery after a download |
| `--disable-weread` | Hide `import_to_weread` and block WeRead import after a download |
| `--allowed-dir <dir>` | Keep downloads, Kindle attachments and WeRead imports inside one folder |
| `--log-level <level>` | `off`, `error`, `warn` (default), `info` or `debug`, to stderr |

`--read-only` overrides the individual `--disable-*` flags. `--allowed-dir` also
rejects paths that leave the folder through symlinks or Windows junctions.
Downloads without a `dir` go to `ZLIB_DOWNLOAD_DIR` when it lies inside that
folder, otherwise to the folder itself. `--log-level` logs tool names, durations
and failures, never arguments.

## Permissions and results

Downloads can replace same-named files. Kindle delivery reads a local file and
emails it to the configured recipient. WeRead import uploads a local file to the
user's WeRead shelf and counts toward WeRead's import limits; the user
logs in with `zlib weread login` in a terminal, never through the agent. Require
client approval for these actions
and for history deletion; tool annotations are hints, not authorization.
Read-only mode still exposes account details and history. Without
`--allowed-dir`, the server can write and read wherever the local user can.
`check_domains` takes no proxy argument; it uses `ZLIB_PROXY` when set. It also
probes the user's own mirrors. MCP cannot add or remove mirrors: the user runs
`zlib mirrors add` in a terminal, then restarts the server.

MCP search accepts `count` from 1 to 50 (default 10) and pages starting at 1.
Year filters accept 1000 through 9999, or 0 to leave a bound unset. Invalid
arguments are tool errors that name the argument and its allowed values.
History metadata failures return an error instead of an incomplete filtered
list. Restart the server after changing environment or proxy settings; account
sessions are reread on each call.

Book lists return `id`, `name`, `authors`, `publisher`, `year`, `language`,
`extension`, `size` and `rating`; `get_book` adds ISBNs, edition, pages,
categories and description. Error messages show only the scheme and host of any
URL, so signed download links and proxy credentials stay out of transcripts.

Cancelling a tool stops its HTTP and SMTP work. A completed file remains saved,
and mail already accepted by the SMTP server cannot be recalled. A failed Kindle
step after a download retains the file path in the result; do not download again
just to retry delivery. A WeRead error saying the import is unconfirmed means the
upload finished; ask the user to check the shelf instead of importing again.
`already_existed: true` means WeRead already had the file and returned the
existing book.

## Tools

| Tool | Mode | Description |
| --- | --- | --- |
| `search_books` | Read | Search books with format, language, year, and order filters |
| `get_book` | Read | Fetch book metadata and description by `id:hash` |
| `get_recommended_books` | Read | Personalized recommendations for the signed-in account |
| `get_most_popular_books` | Read | Most popular books on Z-Library |
| `get_saved_books` | Read | Paged favorite books |
| `get_download_history` | Read | Paged download history with optional format filter |
| `get_profile` | Read | Account profile and daily quota |
| `check_domains` | Read | Probe known mirrors to find reachable domains |
| `download_book` | Write | Download a book by `id:hash` with MCP progress notifications |
| `delete_download_history` | Write | Remove an entry from remote history by ID or `id:hash` |
| `send_to_kindle` | Write | Send a local ebook file to Kindle via SMTP |
| `import_to_weread` | Write | Import a local EPUB, PDF, MOBI, AZW3 or TXT file into WeRead ([guide](weread.md)) |

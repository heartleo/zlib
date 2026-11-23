# Privacy

[README](../README.md) · [MCP server](mcp.md)

zlib runs on your computer. It has no telemetry, no analytics and no account with
the project: nothing is sent to the maintainers. It makes network requests only
when you ask for something, and only to the services that task needs.

## What is sent, and where

| Task | Sent | To |
| --- | --- | --- |
| Search, book details, feeds, history, profile, download | Your search text, the book reference and your Z-Library session | The Z-Library mirror you use, or the one `zlib doctor` finds |
| Metadata lookup | The title, authors and ISBN of the book | Open Library and Google Books by default for a book that is not Chinese (Google Books only with your API key), Douban for a Chinese one, and Goodreads or WeRead only if you turn them on |
| AI Match (only when you set up a model and press `a`) | The title, authors, ISBN, language, publisher and year being searched, the file's name without its path, and the catalogue rows found (title, authors, publisher, date, ISBN, languages and the start of the description) | The AI endpoint you configured, under your own API key; its web search, if you turned it on, runs on that provider's side |
| Cover download | A request for the cover image | The catalogue's image host |
| Send to Kindle | The e-book file and your Kindle address | Your own SMTP server |
| Import to WeRead, WeRead lookup | The e-book file, or the title | Tencent's WeRead, under your WeRead account or API key |
| `zlib update` | A request for the latest release | GitHub |

Metadata lookups never send the file itself, only its title, authors and ISBN.
AI Match is off by default and sends nothing until you configure a model, pass
its connection test and start a match yourself; the other catalogue lookups are
the same with or without it. What the AI provider does with the request is up
to that provider. zlib never sends the file, its path or your Z-Library session
to it. Your Google Books, WeRead and AI API keys, and any custom AI request
headers, are sent only to their own services, and the AI endpoint is never
followed through a redirect to another host.

## What is kept on your computer

Your Z-Library session, settings, API keys (the AI key is in `ai.json`) and SMTP
password stay in files and environment variables on your machine:
`~/.config/zlib/` and `.env` files ([Configuration](configuration.md)). zlib never prints a key: settings screens
and `--json` show it as hidden.

## Through an AI agent

When an agent uses zlib through the [MCP server](mcp.md) or the plugins, the
agent sees the results of the tools it calls: book lists, the metadata of a local
file it reads, and the paths it works with. Your password and session cookies are
never part of a result. `--allowed-dir` limits the folders the tools touch, and
`--read-only` removes the tools that change anything. What the agent's own
service does with the conversation is up to that service.

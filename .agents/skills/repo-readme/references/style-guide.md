# README style

## Header

Start with the logo, name, and a one-sentence purpose. Keep the maintainer's badge
set: Go version (from `go.mod`), CI (`ci.yml`), release, downloads, license, and
the Trendshift badge below them. Do not drop or add badges without asking. Then
the language switch and links to the main sections.
Follow with one short paragraph: what zlib is, what a user does with it, and
where their data lives. Then show the demo, then a few task bullets.

## Order

Install → Quick start → Terminal interface → Commands → Agent setup →
Documentation. A reader should reach a working search before any reference
material. Keep required onboarding outside `<details>` blocks; collapse only
optional recipes.

## Terminal interface versus commands

Say which entry point opens what: `zlib tui` opens the shell; `zlib search` with
a query prints a table, without a query it opens a form; `--json` is for scripts.
Give the TUI a short key table for a first session: open commands, select, page,
download, refine, back, exit. Every key in it must match `docs/tui.md` and the
code. The full key list stays in `docs/tui.md`.

## Agent setup

The README's Agent setup covers the plugin skills users install through the
Claude Code or Codex marketplace (`zlib`, `kindle`), and a short MCP server
subsection: the `claude mcp add` and `codex mcp add` commands, `--read-only`,
and a link to `docs/mcp.md`. Other MCP clients, the flag table and the tool
list live in `docs/mcp.md`. Do not link `.agents/skills/` or `docs/agents.md`,
and do not mention repository-level skills such as `repo-readme` or
`zlib-tui`; those are maintainer tooling, not part of what a user installs.

## Wording

Keep feature bullets about what zlib does, without commands or keys (see
[the copy rules](copy.md#feature-bullets)). Describe observed behavior; omit
speed, quality, popularity, and completeness claims without evidence. Use named
commands and concrete keys in the other sections. A prompt that runs on Enter
is not live search.
Do not state counts that drift (number of themes, mirrors, formats).

Use short headings and fenced command blocks. Label Bash and PowerShell blocks
separately where their syntax differs. Tables suit short key-to-action,
task-to-command, and guide-link mappings. No emoji headings, sponsor banners, or
badge walls beyond the set above.

## Media

Use a real recording of the current interface. Before keeping a demo, check its
recording script (`docs/*.tape`) against the current flow. If it shows an older
interface, say so in the review notes and do not describe it as current.

## Two languages

Use the same examples, keys, and links in English and Chinese. Translate
explanations, not flags, paths, keys, JSON keys, plugin selectors, or command
names. Prefer natural Chinese over a literal translation of English sentence
structure.

Apply [the copy rules](copy.md) to the final pass. Cut repeated introductions, promotional
adjectives, decorative headings, and recap paragraphs. Preserve technical limits
that affect whether a command works.

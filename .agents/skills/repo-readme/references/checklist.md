# README checks

- The tagline and description match the locked hero copy in `copy.md`, or the
  maintainer approved the change. Bullets pass the line gate.
- Feature bullets say what zlib does and contain no command, flag, key, or menu
  path.
- Both languages show a purpose, a short description, installation, first-use
  commands, the terminal key table, commands, and agent setup.
- The first-use sequence includes account and domain prerequisites.
- Each command and flag exists in current help or code; example placeholders are explained.
- Every key in the terminal table matches `docs/tui.md` and the shell code. The
  TUI home has no search input; search starts from `/`.
- Query search, interactive search, and JSON output are described accurately.
- Agent setup names only plugin skills; no `.agents/skills/` or `docs/agents.md`
  link, no repository-level skills. Its MCP subsection shows only Claude Code and
  Codex and links `docs/mcp.md` for other clients.
- Plugin selectors match the marketplace manifests. The CLI/session prerequisite is visible.
- Commands in corresponding fenced blocks, and keys in the key tables, are
  identical between languages.
- Local images and links exist. Heading anchors and inbound links resolve.
- The demo's recording script matches the current flow, or the review notes say it does not.
- Both languages carry the same badges: Go version, CI (`ci.yml`), release,
  downloads, license, Trendshift. Each points to the intended target.
- The issues paragraph links `.github/SECURITY.md` for private vulnerability reports.
- Detailed configuration and installation alternatives remain reachable in `docs/`.
- Research notes distinguish observations from zlib-specific editorial decisions.
- Skill references agree with the README structure and avoid maintaining duplicate full drafts.
- If a plugin skill (`zlib`, `kindle`) changed, `node scripts/sync-plugin-skills.mjs --check` passes.
- `git diff --check` passes.

Use the smallest checks that prove the change. Documentation edits do not require
installing packages, making authenticated requests, or sending messages.

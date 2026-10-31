# CLI/TUI README research

These notes describe the linked READMEs at review time. They are observations,
not universal rules or a popularity ranking. Recheck the sources before a later
redesign; record the date, links, observations, and decisions here.

## Review of 2026-10-17

Raw README files fetched from the default branch of each repository.

| Source | Observed structure | Pattern zlib takes |
| --- | --- | --- |
| [Crush](https://github.com/charmbracelet/crush/blob/main/README.md) | Logo, two badges, one-line pitch, demo, then Features → Installation → Getting Started. Long install recipes and module setup sit in `<details>` blocks. Configuration follows in depth. | Demo before install; Getting started right after install; keep optional recipes out of the main path. |
| [Glow](https://github.com/charmbracelet/glow/blob/master/README.md) | "What is it?", Installation, then separate **The TUI** and **The CLI** sections: running with no arguments opens the TUI; arguments and stdin use the CLI. | Separate a Terminal interface section from Commands, and say which entry point opens which. |
| [Lazygit](https://github.com/jesseduffield/lazygit/blob/master/README.md) | Sponsors, an opinionated elevator pitch, table of contents, features as headed GIFs, tutorials, then a long per-distro install list. | Describe features through concrete actions; skip the long distro list and the rant tone. |
| [Posting](https://github.com/darrenburns/posting/blob/main/README.md) | Centered title, bold one-liner, a paragraph naming the category and what sets it apart (SSH, keyboard workflow, local YAML), screenshot, three links, a feature list. | Add a short paragraph after the header: what zlib is, what it does, where data lives. |
| [Atuin](https://github.com/atuinsh/atuin/blob/main/README.md) | Light/dark `<picture>` logo, badges, demo, features, docs link, then a four-command Quickstart block. | Keep first use as one short, copyable command sequence. |
| [superfile](https://github.com/yorukot/superfile/blob/main/README.md) | Light/dark logo, demo, contents list, per-OS install one-liners, a "Start superfile" section with the bare command, then hotkeys, themes, troubleshooting, uninstall. | Name the command that starts the TUI; put full hotkeys in a guide. |
| [k9s](https://github.com/derailed/k9s/blob/master/README.md) | Very long single-page manual: screenshots, install, command line, a key-binding table, configuration. | Use a key table, but only for first-session keys; the full table belongs in `docs/tui.md`. |
| [GitUI](https://github.com/gitui-org/gitui/blob/master/README.md) | Numbered sections with "Top ▲" links, motivation, benchmarks, roadmap, known limitations. | Not adopted: zlib's README is short enough not to need a numbered index. |
| [Yazi](https://github.com/sxyazi/yazi/blob/main/README.md) | Minimal: description, demo, project status, links to an external docs site. | Keep the README a doorway; detailed guides live in `docs/`. |
| [btop](https://github.com/aristocratos/btop/blob/main/README.md) | Repology badge, many platform badges, news, per-OS compilation guides, full config file. | Not adopted: badge walls and compile guides push the quick start down. |

### Decisions

- Header: logo, name, one-line purpose, the maintainer's badge set (restored
  2026-10-18 at the maintainer's request), language switch,
  section links; then one paragraph saying what zlib is (Posting pattern).
- Demo before installation (Crush, Atuin, superfile).
- Install: one route per main platform; alternatives in `docs/installation.md`.
- Quick start: account prerequisite → `zlib doctor` → `zlib login --domain` →
  `zlib tui` → the exact keys for a first search and download.
- **Terminal interface** and **Commands** are separate sections (Glow). The
  terminal section has a short first-session key table (k9s, smaller) and links
  to `docs/tui.md` for the rest.
- Agent setup stays visible after Commands (GitHub CLI pattern from the
  2026-10-15 review).
- Not adopted: sponsor banners, tables of contents for a short page, benchmark
  claims, per-distro install lists, emoji headings.

### Findings fixed in this review

- The README said to type a query on the TUI home and press Enter. The home has
  no search input (`shell_home.go`, `TestShellHomeRequiresSlashForCommands`);
  search starts from `/`. Both READMEs now give the `/search` path.
- `docs/search.gif` was recorded on 2026-04-10 from `docs/search.tape`,
  which drives the older single-command search form, not the current shell.
  Re-recording needs VHS and a live login, so it was not done. Treat the GIF as
  illustrative until it is re-recorded. (Since re-recorded in `3a1e3d3` from
  fixture data through the TUI shell, in a dark and a light version; the
  unused April recordings were removed.)

## Review of 2026-10-15

Sources: Crush, Lazygit, fzf, GitHub CLI. Decisions kept from that review:
distinguish table output, JSON, and interactive commands (fzf); keep source
builds out of the README (fzf); keep an agent-skills entry point (GitHub CLI).
That review removed stale claims ("over 20" themes, Windows configuration under
`%APPDATA%`, "live search") and pointed the CI badge at `ci.yml`.

## Writing skill review of 2026-10-18

SKILL.md files fetched from each repository's default branch. Selected for
README copy: the marketingskills pair, because the task was product copy for a
README that already had a voice.

| Source | What it offers | Used for zlib |
| --- | --- | --- |
| [marketingskills `copywriting`](https://github.com/coreyhaines31/marketingskills/tree/main/skills/copywriting) (MIT) | Diagnose blank / real voice / point edit before drafting; three-question line gate; leave-it-alone test; AI-tell blacklist | Edit modes, line gate, English checks |
| [marketingskills `copy-editing`](https://github.com/coreyhaines31/marketingskills/tree/main/skills/copy-editing) (MIT) | Seven sweeps; word-level cut and replace lists | Word lists only; the sweeps and expert scoring suit landing pages, not a README |
| [agent-toolkit `crafting-effective-readmes`](https://github.com/softaworks/agent-toolkit/tree/main/skills/crafting-effective-readmes) (MIT) | README tasks (create, add, update, review) and audience-first questions | Already covered by the workflow; not adopted separately |
| [agent-toolkit `writing-clearly-and-concisely`](https://github.com/softaworks/agent-toolkit/tree/main/skills/writing-clearly-and-concisely) (MIT) | Strunk's composition rules; AI vocabulary to avoid | Active voice, positive form, needless words |
| [anthropics `doc-coauthoring`](https://github.com/anthropics/skills/tree/main/skills/doc-coauthoring) | Long-form document co-writing workflow | Not adopted: built for specs and proposals |
| Local `humanizer-zh` (user skill) | Chinese template patterns: 不是而是, 三段式, 层叠的“的”, 进行＋动词 | Chinese checks |

Applied on 2026-10-18: hero copy locked as chosen by the maintainer; feature
bullets split to one task each, dropped "in one list layout" and "everything",
unified 筛选 over 过滤. The `/theme` mention left the bullets; themes stay in
the Documentation table.

## Verification sources

- Command behavior: `internal/cli/login.go`, `search.go`, `shell.go`,
  `shell_palette.go`, and `zlib <command> --help`.
- TUI keys: `docs/tui.md` (the interaction contract) and the shell tests it names.
- Configuration paths: `internal/config/config.go`; theme names: `internal/cli/theme.go`.
- Claude marketplace: `.claude-plugin/marketplace.json`.
- Codex marketplace: `.agents/plugins/marketplace.json`; syntax checked with
  `codex plugin add --help` and `codex plugin marketplace add --help`.
- [Homebrew tap](https://github.com/heartleo/homebrew-tap/blob/main/zlib.rb) and
  [winget manifests](https://github.com/microsoft/winget-pkgs/tree/master/manifests/h/heartleo/zlib)
  identify the package routes. Package installation was not run during these reviews.

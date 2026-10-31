---
name: repo-readme
description: Edit or review zlib's English and Chinese READMEs using patterns from popular TUI project READMEs, research new examples, and verify onboarding commands, TUI keys, and agent installation.
license: MIT
---

# Repository README

Keep installation, quick start, the terminal key table, and agent setup in both
READMEs. Use `docs/` for detailed workflows and CLI help for exact flags.
The layout follows patterns observed in Crush, Glow, Posting, Atuin, superfile,
and k9s; see [the research notes](references/research.md).
Follow [the copy rules](references/copy.md) for every line of prose.

## Workflow

1. Read both READMEs and the docs for the workflows being changed. Inspect the
   relevant command help, implementation, and plugin manifests. Preserve existing
   worktree changes. Finish with evidence for each command, prerequisite, and
   behavior you plan to describe.
2. For structural changes, read [the research notes](references/research.md).
   If new research is requested, inspect current primary README sources and record
   the date, links, observations, and decisions. Borrow useful patterns; choose
   sections for zlib's users.
3. Follow [the style guide](references/style-guide.md): header, one-paragraph
   description, demo, task bullets; one installation route per main platform;
   a quick start that keeps domain selection, prompted login, opening the TUI,
   and the first search keys together; then separate Terminal interface and
   Commands sections.
4. Draft both languages together. Keep commands, flags, destinations, and code
   blocks identical. Translate prose naturally. Use the language outlines below
   when restructuring; they describe content rather than duplicate the README.
   For wording, pick the edit mode in [the copy rules](references/copy.md),
   keep the locked hero copy, and run the line gate on what you change.
5. Run [the checklist](references/checklist.md). Check local links and heading
   anchors, compare code blocks, and verify examples with command help. Use
   fixtures or help output for validation that would otherwise authenticate,
   download a book, or send mail.
6. This skill is repository-level and is not synced into the plugin. If a
   plugin skill (`zlib`, `kindle`) changed, run
   `node scripts/sync-plugin-skills.mjs`, then
   `node scripts/sync-plugin-skills.mjs --check`. Finish with
   `git diff --check`. Report what was checked and any unverified claim.

## Evidence to check

- Login needs a configured domain or `--domain`. An example domain is not a
  guarantee of availability; direct readers to `zlib doctor`.
- Search with a query prints a table; search without a query opens a form.
  JSON examples need an explicit query and a complete `id:hash` for downloading.
- The TUI home has no search input. Search starts from `/` (choose `/search`,
  or type `/search <query>` and press Enter); the form submits with Ctrl+S.
- A TUI download starts with confirmation. Distinguish selecting from downloading.
- Take every key in the README key table from `docs/tui.md` and the shell code.
- Plugins require the installed CLI and a session in the agent's environment.
  Verify marketplace names and syntax from manifests and installed CLI help.
- Read configuration paths and theme names from code. Put the maintained detail
  in the corresponding guide, rather than another README table.
- Use the CI workflow for the CI badge. Verify local demo assets exist; compare
  their `.tape` scripts with the current flow before claiming they show the
  current UI. The README demo is the `docs/search.gif` /
  `docs/search-light.gif` pair, recorded from fixtures
  (`internal/cli/demo_test.go`) with the steps in `docs/search-steps.tape`.

## References

- [Research](references/research.md): sources and decisions behind the layout.
- [Style](references/style-guide.md): presentation and wording.
- [Copy](references/copy.md): edit modes, locked hero copy, line gate, English and Chinese checks.
- [English outline](references/template-en.md): English section responsibilities.
- [Chinese outline](references/template-zh.md): Chinese wording and section mapping.
- [Checklist](references/checklist.md): completion checks.

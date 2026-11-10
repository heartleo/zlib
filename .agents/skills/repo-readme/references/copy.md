# README copy

Rules for the tagline, description paragraph, feature bullets, and any other
prose in the READMEs. Adapted from the MIT-licensed `copywriting` and
`copy-editing` skills in
[coreyhaines31/marketingskills](https://github.com/coreyhaines31/marketingskills)
and `writing-clearly-and-concisely` in
[softaworks/agent-toolkit](https://github.com/softaworks/agent-toolkit); see
[the research notes](research.md#writing-skill-review-of-2026-10-18).

## Pick the edit mode first

| Mode | When | What may change |
| --- | --- | --- |
| Rebuild | Section is missing, stale, or generic | Write from verified facts; never invent numbers or claims |
| Restore | Copy has a clear angle but rough lines | Keep the angle and vocabulary; fix buried leads, vague lines, AI tells |
| Point edit | The maintainer asks to change one line or phrase | Change only that, plus the minimum around it; leave the rest as written |

State the mode in one line before editing. Lines the maintainer wrote or chose
are locked: suggest changes, do not apply them unasked.

## Locked hero copy

Chosen by the maintainer on 2026-10-18. Keep both languages in step.

| Part | English | Chinese |
| --- | --- | --- |
| Tagline | A Z-Library CLI to search, download, and send books to Kindle, for you and AI agents like Claude Code and Codex. | Z-Library 命令行工具，搜索、下载图书并发送到 Kindle，你和 Claude Code、Codex 等 AI Agent 都能用。 |

There is no separate description paragraph: the tagline names the core tasks
(search, download, Kindle), that it is a CLI, and the agent integration. It deliberately leaves out
search filters, JSON output, local session storage, and the full-screen
interface; those belong in the bullets or later sections.

## Line gate

Run every line you write or keep through three questions:

1. Can the reader picture it? A command, key, or concrete result beats an abstraction.
2. Can it be checked? "Sends the file to Kindle" can; "seamless" cannot.
3. Could another Z-Library client say exactly this? If so, make it specific.

Then check each change: is it better for a reason you can state, or only
different? Revert changes you cannot justify.

## Feature bullets

The Features section keeps the v0.0.8 format the maintainer chose: one line
per feature, `emoji **Label** — description`, joined with a trailing backslash
(not two spaces, which fail `git diff --check`). Keep both languages to the
same features in the same order.

- One user task per bullet, starting with a verb. Do not pair unrelated
  features in one bullet.
- Say what zlib does, not how to use it. A bullet has no command, flag, key,
  or menu path (`zlib doctor`, `/theme`, `s`, `/config`) and no UI layout
  detail. Operating steps belong in Quick start, Terminal interface, Commands,
  and `docs/`.
- No unprovable scope words ("everything", "all", "any").
- Do not repeat the hero sentence; a bullet adds detail the hero left out.

## English checks

- Active voice, positive form, plain words: "use" not "leverage", "help" not "facilitate".
- Cut intensifiers and filler: very, really, just, actually, basically, in order to.
- Replace puffery with the fact it stands for: seamless, robust, powerful,
  blazing fast, cutting-edge, unlock, streamline, effortless.
- No contrast reveals ("not X, but Y"), negation lists, self-answered
  questions, colon reveals, or stock openers ("Whether you're…", "Say goodbye to…").
- No em dashes or exclamation marks in the tagline, description, or bullets.
- Sentences under about 25 words; one idea each.

## Chinese checks

Write natural Chinese rather than translating English sentence structure.

- 不用“不是……而是……”、强凑三段式、句尾拔高和宣传语。
- 少用层叠的“的”、“进行＋动词”和被字句。
- 术语统一：筛选（不写“过滤”）、下载目录、镜像、Agent 插件。
- 中文标点；命令、参数、按键、路径保持原文。

## Before finishing

Read the hero and bullets aloud in both languages. Every claim must match the
code or docs; flag anything unverified as `[NEED: …]` instead of writing it.

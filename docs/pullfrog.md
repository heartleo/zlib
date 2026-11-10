# Pullfrog automation

Pullfrog needs its GitHub App, repository settings in the
[console](https://pullfrog.com/console/heartleo?repo=zlib), and
`.github/workflows/pullfrog.yml` on the default branch. The App dispatches the
workflow for configured events; separate issue or PR event workflows are not
needed.

## Repository settings

With GitHub CLI authentication available, apply the checked-in configuration:

```sh
node scripts/configure-pullfrog.mjs --apply
```

This writes settings for `heartleo/zlib` only. It does not publish files, enable
the repository, or start runs. Settings are applied sequentially; a failure stops
the script and leaves earlier writes in place. Re-running applies the same
configuration. Changes to `.github/pullfrog/` prompts require reapplying it;
Pullfrog stores their text in the console rather than reading these files on
each run.

The configuration uses the OSS-funded DeepSeek Flash alias with High reasoning.
Model and effort live only in the console settings; the workflow sets neither,
so the script is the single source. No provider key or personal model
subscription is required for a repository with an active OSS grant. Keep the
grant enabled and select only funded models to use the program.

New issues, including those from non-collaborators, receive analysis, related
issue checks and plans when actionable. Automatic labeling is off; the issue
forms already apply the `bug`, `enhancement` and `question` labels.
New PRs receive comment-only reviews, with re-reviews on new commits. Maintainer
mentions can request additional analysis; outside mentions cannot start runs.
Automatic implementation, CI fixes, approvals and merging are disabled. Code
pushes are disabled in the repository settings, consistent with the
repository's contribution policy. Reviews state that the repository
does not merge pull requests.

The script also unsets keys it does not manage (environment allowlist, hooks,
mention and label instructions, and the build, plan, address-reviews and fix-ci
prompts), so re-running it removes console edits to them.

To see whether the console has drifted from the checked-in configuration without
changing anything:

```sh
node scripts/configure-pullfrog.mjs --check
```

It exits 1 and lists each differing key. `config get` prints the bare value, or
`unset`; the comparison ignores whitespace and quotes. On Windows the CLI's Node
process can abort at exit after a complete answer, so the script trusts any
non-empty output.

The workflow is the unmodified template from the
[setup guide](https://docs.pullfrog.com/getting-started). Push, shell, model and
effort come only from the console settings above, and `--check` reports drift.
The workflow installs no toolchain and sets no cache, so a run that executes
code from an external pull request cannot write a cache that `ci.yml` or
`release.yml` would restore. The agent has no Go toolchain pinned to `go.mod`,
so checks that need one may not run; reviews state which checks ran.

## Activation

1. Review and publish the workflow and supporting files to the default branch.
2. In the console, use **Verify manual installation**. Do not use **Add workflow
   directly** or **Add workflow via PR** when managing this file locally.
3. Check that OSS funding is active, the model is DeepSeek Flash, reasoning is
   High, and the automation settings match the script. Enable the repository if
   verification has not enabled it.
4. Send a read-only console prompt to summarize the repository. Check the
   Actions log for the resolved model and effort, the funding route and a
   successful run. Then verify issue triage with a maintainer-approved test issue.

Until the workflow is published and verified, local configuration is not proof
of a working automation. OSS model funding does not cover GitHub Actions runner
usage; GitHub's runner policies apply independently.

## References

- [Pullfrog setup and workflow contract](https://docs.pullfrog.com/getting-started)
- [CLI settings](https://docs.pullfrog.com/cli-configuration)
- [OSS-funded models](https://docs.pullfrog.com/models#pullfrog-for-oss)
- [Issue enrichment](https://docs.pullfrog.com/issue-enrichment)
- [PR reviews](https://docs.pullfrog.com/pr-reviews)

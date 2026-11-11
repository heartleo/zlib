# Pullfrog automation

Pullfrog needs its GitHub App, repository settings in the
[console](https://pullfrog.com/console/heartleo?repo=zlib), and
`.github/workflows/pullfrog.yml` on the default branch. The App dispatches the
workflow for configured events; separate issue or PR event workflows are not
needed.

## Repository settings

Pullfrog keeps settings in its console. It has no configuration file, bulk
apply or export, so `scripts/configure-pullfrog.mjs` is the single source of
them in this repository: one `pullfrog config` call per key, with prompt text
read from `.github/pullfrog/`. Pullfrog stores that text in the console rather
than reading these files on each run, so a changed prompt must be applied.

```sh
node scripts/configure-pullfrog.mjs --print   # settings as text; no network, no login
node scripts/configure-pullfrog.mjs --check   # read the console, report differences
node scripts/configure-pullfrog.mjs --apply   # write only the differences
```

`--apply` writes settings for `heartleo/zlib` only. It does not publish files,
enable the repository, or start runs. It reads every key first, writes only
those that differ, and retries a failed call up to five times with a growing
wait, so a dropped connection costs one call rather than the run. If it still
stops, it says how many settings were written; run it again and only the
remainder is written.

Behind an HTTP proxy the script sets `NODE_USE_ENV_PROXY=1` for the CLI. Node's
`fetch` otherwise ignores `HTTPS_PROXY`, and direct connections to Pullfrog
timed out in bursts (21 of 40 requests in one measurement, none through the
proxy). The variable exists from Node 24.0.0 and 22.21.0; on any other version
the script prints a note and the CLI still connects directly, relying on the
retries. Set the variable yourself to override the script's choice.

### Authentication and permissions

The script holds no credential and never reads one. The `pullfrog` CLI it starts
acts as the GitHub user signed in to the GitHub CLI (`gh auth login`, scopes
`repo` and `read:org`), so nothing can be applied without that login and the
repository's admin role: `push`, `shell`, `auto-merge`, signing and OSS funding
need admin; reading needs write access. Someone who forks the repository gets
the script but not these permissions, and `--print` and the tests need none.
The CLI is pinned in the script because it runs with your token; the API
rejects a CLI whose contract version it no longer supports, in which case
raise the pin after reading the release notes.

`--apply` refuses to run unless `HEAD` is `origin/main` and `.github/pullfrog/`
and the script have no uncommitted changes. The prompts steer an agent that
reads public issues, so only merged, reviewed text may reach the console. Run
`--check` anywhere; it only reads.

### Without the script

Run `--print` and enter the values in the
[console](https://pullfrog.com/console/heartleo?repo=zlib), pasting each text
block into its field. Then run `--check`, which reads the values back through
the CLI and reports any that differ.

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
repository's contribution policy. Reviews are advice; the maintainer
squash-merges pull requests after the required checks pass.

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

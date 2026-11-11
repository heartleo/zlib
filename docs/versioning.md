# Versioning

zlib follows [Semantic Versioning](https://semver.org/) and writes every zlib
version as **`vX.Y.Z`**, with a lowercase `v` prefix: `v0.0.10`, not `0.0.10`.

## Where the rule applies

| Place | Example |
| --- | --- |
| Git tags and GitHub release names | `v0.0.10` |
| `zlib version` and `zlib --version` | `zlib version v0.0.10` |
| `zlib update` messages | `Installed zlib v0.0.10`, ` + zlib v0.0.10` |
| Changelogs, advisories, issues, docs and commit messages | `Fixed in v0.0.10.` |

Builds between tags report a Go pseudo-version, such as
`v0.0.11-0.20261019110558-9e05d988daa5`. Builds without version metadata
report `dev`. Both forms keep the rule.

## How the build enforces it

- `.goreleaser.yml` injects `v{{.Version}}` through `-ldflags`. GoReleaser's
  `{{.Version}}` is the tag without its `v`.
- `go install` and source builds take the version from Go build info, which
  already has the `v`.
- `internal/zlib/version.go` (`canonicalVersion`) adds a missing `v` to any numeric version
  injected by another packager, so `-X github.com/heartleo/zlib/internal/zlib.Version=0.0.10`
  still reports `v0.0.10`.

`TestCanonicalVersion` covers the normalization, and
`TestGoReleaserLdflagsTargetThisPackage` fails when the ldflags package path
no longer matches the package that declares `Version`. To check a release binary,
run `zlib version`.

## Exceptions

Some formats are owned by other tools, so the rule does not apply to them.

| Place | Format | Reason |
| --- | --- | --- |
| Release asset names | `zlib_0.0.10_linux_x86_64.tar.gz` | GoReleaser's default name. `zlib update` in every released version builds this name from the tag, so renaming the assets would break updates from those versions. |
| winget and Homebrew manifests | `0.0.10` | These package managers require bare versions. GoReleaser writes them. |
| Plugin manifests (`plugins/zlib/.claude-plugin/plugin.json`, `.codex-plugin/plugin.json`) | `0.2.1` | The plugin has its own version, separate from the CLI. The manifest schemas expect bare SemVer. |
| `go.mod` `go` line, `@version` module queries and third-party versions | `go 1.26.11`, `govulncheck@v1.9.0` | These follow the upstream tool's syntax. |

## Releasing

1. Tag the release commit on `main` with `git tag vX.Y.Z`.
2. Push the tag with `git push origin vX.Y.Z`. The release workflow runs
   GoReleaser.
3. Download one archive and run `zlib version`. It must print
   `zlib version vX.Y.Z`.

# Publishing to the MCP Registry

[README](../README.md) · [MCP server](mcp.md) · [Versioning](versioning.md)

For maintainers. The [official MCP Registry](https://registry.modelcontextprotocol.io/)
lists MCP servers so clients can find and install them. It is in preview and
stores metadata only, never code: an entry points at a package the maintainer
hosts. This page says how zlib is set up for it, and the steps to submit it.
**Nothing is submitted until a maintainer runs the publish workflow with
`dry_run` off.**

## What the registry requires

| Area | Rule | zlib |
| --- | --- | --- |
| Name | Reverse-DNS, one slash, `^[a-zA-Z0-9.-]+/[a-zA-Z0-9._-]+$`, up to 200 characters. With GitHub authentication it must start with `io.github.<owner>/` | `io.github.heartleo/zlib` |
| Description | 1 to 100 characters | `Search and download Z-Library books, fix e-book metadata, and send books to Kindle or WeRead.` (93) |
| Version | One specific version, unique per publication and immutable; semantic versioning recommended; ranges (`^1.2.3`, `1.x`) refused | The release tag without its `v`: `0.0.15` |
| Package | `npm`, `pypi`, `nuget`, `cargo`, `oci` or `mcpb`. The registry stores no artifacts | `mcpb`, hosted on the GitHub release |
| Ownership | npm `mcpName`, PyPI and NuGet `mcp-name:` in the README, OCI image label; nothing for `mcpb` beyond the name and the URL | The GitHub OIDC login proves `io.github.heartleo` |
| MCPB package | `identifier` is the download URL on a GitHub or GitLab release and contains `mcp`; `fileSha256` is the file's SHA-256; clients verify it, the registry does not | `…/releases/download/vX.Y.Z/zlib_X.Y.Z_<os>_<arch>.mcpb` |
| Extras | `title`, `websiteUrl`, `icons`, `repository`, `environmentVariables`, all optional | Set, in `packaging/mcp/server.json` |

zlib is a Go program distributed as release binaries, Homebrew and winget, which
the registry has no package type for. An MCP Bundle (`.mcpb`) is the match: a zip
holding the executable and a `manifest.json`, which Claude Desktop and other
clients install with one click. The alternatives are an OCI image (the server
needs your Z-Library session and your files, so it would run with volume mounts
and the session in the container's home) and an npm wrapper that downloads the
binary on install (a second package to keep in step). Both are possible later.

## What is in the repository

| File | Purpose |
| --- | --- |
| `packaging/mcp/server.json` | The registry entry. Its version and packages are placeholders; the workflow renders the real ones |
| `packaging/mcpb/manifest.json` | The bundle's manifest: tools, settings, privacy policy |
| `scripts/mcpb/` | Builds a bundle per platform and renders `server.json` from a release's bundles, with the schema's checks |
| `.goreleaser.yml` | A post-build hook makes the bundles; they are uploaded with the release and listed in `checksums.txt` |
| `.github/workflows/mcp-registry.yml` | Manual workflow: checks the bundles against `checksums.txt`, validates their manifests with the pinned MCPB validator, renders `server.json`, runs `mcp-publisher validate` and, only when asked, publishes |
| `plugins/zlib/.mcp.json` | Registers `zlib mcp` in the Claude Code, Codex and Grok plugin, the install route for clients that do not read bundles |
| `docs/privacy.md` | The privacy policy the manifest links to, required because the server talks to external services |

`TestRegistryFilesMatchTheServer` keeps the descriptions, the tool list and the
settings in these files equal to what the server registers and reads, and
`scripts/mcpb` tests cover the bundle and the registry's rules.

The server reads each flag from `ZLIB_MCP_<FLAG>` too (`ZLIB_MCP_READ_ONLY=true`,
`ZLIB_MCP_ALLOWED_DIR=/books`), because a registry entry or a bundle configures a
server with environment variables, not arguments. A flag on the command line
wins and an empty variable counts as unset.

## Which clients use what

The registry lists packages; a client installs the types it understands. The
`mcpb` entry reaches clients that install MCP Bundles, such as Claude Desktop.
Codex and Grok install neither a bundle nor a registry entry, so for them the
install route is the plugin (`plugins/zlib`, which carries `.mcp.json`) or one
registration command:

| Client | Route | Checked |
| --- | --- | --- |
| Claude Desktop and other MCPB clients | The `.mcpb` bundle, from the registry or the release | Bundle runs and passes the schema checks here; the MCPB validator runs in the workflow |
| Claude Code | Plugin, or `claude mcp add` | `claude plugin validate` shows the server |
| Codex | Plugin (`mcpServers` in its manifest), or `codex mcp add` | The plugin installed from the local marketplace listed `zlib mcp` in `codex mcp list` |
| Grok | Plugin with `--trust`, or `grok mcp add` | The plugin installed from the local marketplace; `grok mcp doctor` started the server and listed its tools |

The plugin runs the `zlib` on `PATH`, so a CLI older than a tool does not offer
it: update the CLI to get new tools.

## What popular listed servers do

Read from the registry's API and the projects' repositories. Go servers are the
closest to zlib.

| Practice | Seen in | zlib |
| --- | --- | --- |
| One server, several channels. A native binary plus a thin npm and PyPI launcher, so `npx -y` and `uvx` clients work with no install | `kubernetes-mcp-server` (npm, PyPI, OCI); Azure MCP (NuGet, npm, PyPI, six MCPB) | MCPB only. The launchers are the main gap, and the next step below |
| A container image as the one package, run with `docker run -i --rm` through `runtimeArguments` | GitHub's server, `mcp-grafana` | Not used: the server needs your session and your files |
| MCPB added next to other types, one package per platform with a `fileSha256` | Azure MCP | Done; the other types are missing |
| The package version equals the server version | all of them | Done: one tag gives both |
| `environmentVariables` with `isRequired`, `isSecret` and a description | Grafana, Terraform servers | Done for the two switches. API keys are left out on purpose: see below |
| Safety switches as settings: read-only and destructive-tool limits, an audit log | Terraform servers, Kubernetes | `--read-only`, `--disable-*` and `--allowed-dir`, with `ZLIB_MCP_*` |
| Guardrails described as best effort, the real control being the account's own permissions | `kubernetes-mcp-server` | Said in [MCP server](mcp.md#permissions-and-results) |
| A per-client install section, with deeplink buttons for VS Code and Cursor | `kubernetes-mcp-server` | A section per client; no buttons |
| A repository with `server.json`, a bundle manifest, `AGENTS.md` and `SECURITY.md` | `kubernetes-mcp-server` | All present |

API keys stay out of `environmentVariables` and the bundle's settings. A client
may pass a blank value for a setting the user left empty, and zlib treats a set
but empty variable as an override, so a blank `ZLIB_GOOGLE_BOOKS_KEY` would hide
the key saved with `zlib config`. The bundle uses the same saved settings as the
CLI, so keys need no second place. The two `ZLIB_MCP_*` switches are safe:
a blank one counts as unset.

### The next step: launchers

The pattern in the table that zlib lacks is an `npx` and `uvx` route. Sketch,
following `kubernetes-mcp-server` and the usual native-binary launchers:

- An npm package whose `bin` script runs `zlib mcp` from a per-platform optional
  dependency (`…-linux-x64`, `…-darwin-arm64`, and so on, each holding one
  binary), as esbuild does. `package.json` carries `mcpName`
  (`io.github.heartleo/zlib`), and the package version is the release version.
- The release workflow publishes the packages with npm provenance, then lists
  an `npm` package in `server.json` next to the six bundles, with `runtimeHint`
  `npx`. A PyPI wheel per platform works the same way with `mcp-name:` in its
  README, if `uvx` matters.
- It needs a name and scope on npm (and PyPI), and a publishing token or
  trusted publishing. Both are decisions for the maintainer: the names are
  public and hard to take back.

## Versioning

Release `vX.Y.Z` gives the server version `X.Y.Z` and bundles named
`zlib_X.Y.Z_<os>_<arch>.mcpb`. A published version cannot change. To fix only the
entry's metadata (a description, an icon) without a new binary, the registry
needs a version it sorts higher.

The registry marks the highest semantic version as `latest`, and sorts a
prerelease such as `X.Y.Z-1` before `X.Y.Z`. A `X.Y.Z-1` published after `X.Y.Z`
is therefore never `latest`, and clients that read `latest` keep the old entry.
Use a prerelease of the same version only to correct the metadata before `X.Y.Z`
is published. Afterwards, publish a higher version: the next patch release, or a
prerelease of it such as `X.Y.(Z+1)-0`, which sorts after `X.Y.Z`. Each needs its
own `v` release, because the workflow renders `server.json` from that release's
bundles, with new names, URLs and hashes.

## Known limits

- The registry's package schema has no platform or architecture field. Each
  platform is its own `mcpb` package (six of them), and only the file name says
  which. MCPB names the operating system in `compatibility.platforms` but has no
  architecture field. A client that picks by operating system alone may choose
  the wrong architecture.
- Only clients that know MCP Bundles install a `.mcpb` and show its two
  settings. Codex (0.170.0) and Grok (1.0.77) do not: they read neither the
  manifest nor `${user_config.*}`. A bundle unpacked by hand and registered with
  its command (`server/zlib mcp`) and the two `ZLIB_MCP_*` environment variables
  works in both: Codex called `read_metadata` through it and Grok's `mcp doctor`
  found all 15 tools and called it too. Their users run `zlib mcp` from the CLI
  as before ([MCP server](mcp.md)).
- A bundle holds the `zlib` executable, so a user who installs it still needs the
  CLI on their `PATH` to run `zlib login` (the session is shared). The server
  cannot log in through the agent.
- Registry entries are made by hand for now. Automating them on `release:
  published` is a change to make after a first successful manual publication.

## Before the first submission

These steps need a release that contains the MCP bundles, so this change is
merged and released first.

1. Release as usual ([Versioning](versioning.md#releasing)) and publish the draft.
   Check that `zlib_X.Y.Z_<os>_<arch>.mcpb` exists for all six platforms and that
   `checksums.txt` lists them.
2. Install one bundle in Claude Desktop through its extensions settings. Check
   the tools list, the two settings and a search after `zlib login`. The
   workflow also runs `npx @anthropic-ai/mcpb validate` on every manifest; read
   the result of that step in the dry run, as it could not be run beforehand.
3. Run the MCP Registry workflow with the tag and `dry_run` on (the default). It
   renders `server.json`, runs `mcp-publisher validate` and stops. Read the
   rendered file in the log.
4. Ask for approval to publish, as below.

## Submit

Only after approval, run the workflow again with `dry_run` off. It signs in with
GitHub OIDC (`id-token: write`, no secret needed) and runs `mcp-publisher
publish`. Then check the entry:

```sh
curl "https://registry.modelcontextprotocol.io/v0.1/servers?search=io.github.heartleo/zlib"
```

It should show the version, `latest` set, and six packages. Failures and their
fixes:

| Message | Fix |
| --- | --- |
| `Registry validation failed for package` | The package does not match the entry: check the bundle URL and that its release is published |
| `invalid audience` | Bump `MCP_PUBLISHER_VERSION` in the workflow |
| `You do not have permission to publish this server` | The name must start with `io.github.heartleo/` and the workflow must run in `heartleo/zlib` |
| `fileSha256` pattern error | A bundle was missing or renamed: the workflow renders one package per platform and fails if one is absent |

## Each release afterwards

Run the workflow for the new tag. Versions are immutable, so a mistake means a
new release, or a prerelease for metadata only. To take a version down, mark it
deleted with `mcp-publisher status`; nothing is erased, and `status` can restore
it.

## Open decisions

- **Launchers.** Add an npm package (and a PyPI wheel) that runs the binary, as
  the popular Go servers do, so `npx` and `uvx` clients work and Codex and Grok
  users have a one-line route that needs no CLI install. It needs the package
  names and scopes, and a publishing setup ([above](#the-next-step-launchers)).
- **Architecture.** Six per-platform MCPB packages, as above, or only the most
  common (macOS Apple silicon, Windows x86_64, Linux x86_64) to make a wrong pick
  less likely. Launchers make this less important, since they pick the platform
  themselves.
- **When to automate.** Keep the manual workflow, or run it on `release:
  published` once a first publication has gone well.

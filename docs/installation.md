# Installation

[README](../README.md) · [Quick start](../README.md#quick-start)

Choose one installation method. Prebuilt binaries are available for macOS, Linux
and Windows on x86-64 and ARM64; they do not require Go.

## Package managers

macOS / Linux:

```bash
brew install heartleo/tap/zlib-cli
```

The formula is named `zlib-cli` because Homebrew's own `zlib` library already
uses the name `zlib`; the command it installs is still `zlib`. If you installed
the earlier `heartleo/tap/zlib` formula, switch to the new one:

```bash
brew uninstall heartleo/tap/zlib
brew install heartleo/tap/zlib-cli
```

Windows (PowerShell):

```powershell
winget install --id heartleo.zlib --exact
```

## Install script or binary download

On macOS / Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/heartleo/zlib/main/install.sh | sh
```

The script installs `zlib` to `/usr/local/bin`, creating the directory if it is
missing and asking for `sudo` when your user cannot write there. Set
`ZLIB_INSTALL_DIR` to install somewhere else, for example
`curl -fsSL https://raw.githubusercontent.com/heartleo/zlib/main/install.sh | ZLIB_INSTALL_DIR="$HOME/.local/bin" sh`;
that directory must be on your `PATH`.

For a manual install, download the archive for your OS and architecture from
[Releases](https://github.com/heartleo/zlib/releases/latest). Extract `zlib`
(`zlib.exe` on Windows) into a directory on your `PATH`.

## Go install

Requires Go 1.26 or later:

```sh
go install github.com/heartleo/zlib/cmd/zlib@latest
```

Add Go's binary directory to your `PATH`: `$GOBIN` if set, otherwise the `bin`
directory under `go env GOPATH`.

## Build from source

Requires the Go version declared in [go.mod](../go.mod).
To build a development branch, check out that branch before running the build command.

```sh
git clone https://github.com/heartleo/zlib
cd zlib
```

macOS / Linux:

```bash
go build -o zlib ./cmd/zlib
./zlib --help
```

Windows (PowerShell):

```powershell
go build -o zlib.exe ./cmd/zlib
.\zlib.exe --help
```

The binary stays in the repository directory. Put it on `PATH` to run it as `zlib`
from other directories or from an agent. To check code changes, run `go test ./...`
and `go vet ./...`.

## Verify and upgrade

```sh
zlib version
zlib update
```

`zlib version` prints the version as `vX.Y.Z`, the same form as the release
tag, for example `zlib version v0.0.10`. See [Versioning](versioning.md).

`zlib update` uses Homebrew, winget or Go when it can identify that installation.
For a standalone binary, it downloads the latest GitHub release for the current
OS and architecture, verifies the release checksum, then replaces the executable.
The previous executable remains next to it as `zlib.previous` (or
`zlib.exe.previous` on Windows). Restart zlib after updating. A source build with
an unrecognized version must be rebuilt or replaced with a release.

In a terminal, each finished step stays on screen with its timing while the
download shows a live progress bar, size and transfer rate:

```text
Checked for updates in 301ms
Downloaded zlib_0.0.13_linux_x86_64.tar.gz (9.0 MiB) in 4.01s
Verified checksum (sha256)
Installed zlib v0.0.13 in 7ms
 - zlib v0.0.12
 + zlib v0.0.13
Restart zlib to use the new version.
```

Ctrl+C during the download cancels the update and leaves the executable
unchanged. Without a terminal (a pipe or CI), the same step lines print without
the progress bar.

If the update cannot identify your package manager, use its normal upgrade
command: `brew upgrade heartleo/tap/zlib-cli`,
`winget upgrade --id heartleo.zlib --exact`, or
`go install github.com/heartleo/zlib/cmd/zlib@latest`.

If a command in these docs is missing, check your installed version. Docs on a
development branch may describe changes that have not reached a release yet;
build that branch from source to try them.

Continue with the [quick start](../README.md#quick-start).

#!/bin/sh
# Mandatory pre-commit checks for zlib. .githooks/pre-commit runs this before
# every commit; `make check` runs it by hand. Every step must pass. See
# .github/CONTRIBUTING.md#pre-commit-checks.
set -eu

cd "$(git rev-parse --show-toplevel)"
# A global GOFLAGS=-mod=mod would let go vet and go test rewrite go.sum.
export GOFLAGS=

step() { printf '\n==> %s\n' "$1"; }
fail() {
	printf 'check: %s\n' "$1" >&2
	exit 1
}
need() { command -v "$1" >/dev/null 2>&1 || fail "$1 not found. $2"; }

need go "Install the Go release on the go line of go.mod."
need node "Install Node.js 22 or later (skill sync check)."
need govulncheck "Run: go install golang.org/x/vuln/cmd/govulncheck@v1.9.0"
need gitleaks "Install gitleaks 8.19 or later: https://github.com/gitleaks/gitleaks#installing"

# govulncheck checks the standard library of the go that runs it, so a
# different local toolchain would scan the wrong release.
want="go$(awk '$1 == "go" { print $2; exit }' go.mod)"
have=$(go env GOVERSION)
[ "$have" = "$want" ] || fail "go is $have but go.mod requires $want; scans would check the wrong standard library."

step "gofmt"
unformatted=$(gofmt -l .)
[ -z "$unformatted" ] || fail "unformatted files (run gofmt -w .):
$unformatted"

step "go mod tidy"
go mod tidy -diff || fail "go.mod or go.sum is not tidy (run go mod tidy)"

step "plugin skills"
node scripts/sync-plugin-skills.mjs --check

step "docs privacy"
node scripts/check-docs.mjs

# Cross-platform: type-check every release target, including files behind
# build tags and tests. Tests themselves run natively here and on Linux,
# macOS and Windows in CI.
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do
	step "go vet $target"
	GOOS=${target%/*} GOARCH=${target#*/} CGO_ENABLED=0 go vet ./... || fail "go vet failed for $target"
done

step "go test"
if [ "$(go env CGO_ENABLED)" = "1" ]; then
	go test -race ./...
else
	printf 'cgo is off, running without -race\n'
	go test ./...
fi

step "govulncheck"
govulncheck ./...

step "gitleaks (staged changes)"
gitleaks git --pre-commit --staged --redact --no-banner .

printf '\nAll pre-commit checks passed.\n'

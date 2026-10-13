#!/usr/bin/env bash
# Portable CI entrypoint shared by GitHub Actions and Forgejo Actions.
#
# Runs formatting, vet, build, race tests and shell lint against the repository.
# Toolchain expectations: Go (>= the version in go.mod) and bash. shellcheck and
# golangci-lint are used when present but are not required.
set -euo pipefail

cd "$(dirname "$0")/.."

group() { printf '::group::%s\n' "$1"; }
endgroup() { printf '::endgroup::\n'; }

group "Toolchain"
go version
go env GOOS GOARCH
endgroup

group "go mod verify"
go mod verify
endgroup

group "gofmt"
unformatted="$(gofmt -l . | grep -v '^vendor/' || true)"
if [ -n "$unformatted" ]; then
  echo "The following Go files are not gofmt-formatted:" >&2
  echo "$unformatted" >&2
  echo "Run 'gofmt -w .' to fix." >&2
  exit 1
fi
echo "all Go files are gofmt-formatted"
endgroup

group "go vet"
go vet ./...
endgroup

group "go build"
go build ./...
endgroup

group "go test -race"
go test -race ./...
endgroup

group "shellcheck"
if command -v shellcheck >/dev/null 2>&1; then
  shellcheck -S warning install.sh scripts/*.sh
else
  echo "shellcheck not installed; skipping"
fi
endgroup

group "golangci-lint"
if command -v golangci-lint >/dev/null 2>&1; then
  # Non-blocking: the linter version may lag the Go toolchain.
  golangci-lint run || echo "golangci-lint reported issues (non-blocking)"
else
  echo "golangci-lint not installed; skipping"
fi
endgroup

echo "CI passed."

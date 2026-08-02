#!/usr/bin/env bash
# Build lancastd with the version stamped from git, so a local build is labelled
# the same way a release is (ADR 0016). goreleaser owns real releases; this is the
# convenience build for development.
#
#   ./build.sh            → ./lancastd  (or lancastd.exe on Windows)
#
# The version is `git describe` (nearest tag + commits + dirty flag), falling back
# to "dev" outside a git checkout.
set -euo pipefail

version="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
out="lancastd"
[ "${OS:-}" = "Windows_NT" ] && out="lancastd.exe"

go build -ldflags "-X lancast/internal/api.Version=${version}" -o "${out}" ./cmd/lancastd
echo "built ${out} (version ${version})"

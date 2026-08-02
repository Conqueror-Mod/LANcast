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

ldflags="-X lancast/internal/api.Version=${version}"
# WINDOWLESS=1 links the Windows GUI subsystem so a double-click shows no console
# (the tray/desktop mode, ADR 0022). Default keeps the console so `-version` and
# logs are visible during development; goreleaser applies windowsgui for releases.
if [ "${WINDOWLESS:-}" = "1" ] && [ "${OS:-}" = "Windows_NT" ]; then
	ldflags="${ldflags} -H=windowsgui"
fi

go build -ldflags "${ldflags}" -o "${out}" ./cmd/lancastd
echo "built ${out} (version ${version})"

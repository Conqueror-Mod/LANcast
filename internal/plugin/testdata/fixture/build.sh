#!/usr/bin/env bash
# Rebuild the committed test fixture module. Run from this directory.
# The .wasm is committed so CI needs no wasm toolchain; regenerate it here when
# fixture.go changes.
set -euo pipefail
GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o ../fixture.wasm .
echo "built ../fixture.wasm"

#!/usr/bin/env bash
# Rebuild the committed OMDb plugin module. Run from this directory.
# The .wasm is committed so CI needs no wasm toolchain; regenerate it here when
# omdb.go or the sdk changes.
set -euo pipefail
GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o plugin.wasm .
echo "built plugin.wasm"

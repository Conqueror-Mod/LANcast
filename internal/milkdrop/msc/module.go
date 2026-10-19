package msc

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"io"
)

/*
 * The vendored module (ADR 0091, step g1): msc.wasm, milkdrop-shader-converter
 * 0.0.8 built for WASI by build/build.sh from pinned sources, gzipped. It is
 * embedded gzipped — 340 KB in the binary rather than 1.2 MB — and inflated
 * only when the converter is first used. README.md is the record of what it
 * is, how it was reproduced, and what it can reach.
 */

//go:embed msc.wasm.gz
var moduleGz []byte

// ModuleSHA256 is the hash of msc.wasm, the module inflated. README.md says
// how it was reproduced; module_test.go holds the embedded file to it.
const ModuleSHA256 = "91babcc93ad5b5f90231087f966c6f5e36d182af886e1a6dde6c7a4b5aad1cf9"

// inflate is the module's bytes.
func inflate() ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(moduleGz))
	if err != nil {
		return nil, err
	}
	return io.ReadAll(zr)
}

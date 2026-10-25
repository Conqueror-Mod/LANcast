// Package msc runs milkdrop-shader-converter, compiled to WebAssembly, under
// wazero: one MilkDrop 2 HLSL shader in, GLSL ES 3.00 out (ADR 0091, the
// amendment of 2026-10-19, steps g1 and g2).
//
// The converter is 2014-era C++ (Unity's hlsl2glslfork and glsl-optimizer),
// and the spike found memory-safety bugs in it: two of 3,000 mutated shaders
// called through a corrupted function pointer, and one never returned. So it
// never runs natively in the server. It runs here, as msc.wasm.gz beside this
// file, built from pinned sources by build/build.sh (see README.md), with:
//
//   - no filesystem, no environment, no arguments beyond its name, and no host
//     functions at all. The module's whole reach is 13 WASI imports — the
//     standard streams, a clock, and exit — and it imports no path_open, so it
//     could not name a file even if a directory were mounted;
//   - a fresh instance for every shader, so one shader's corruption cannot
//     reach the next;
//   - a deadline (3 s, seven times the slowest real shader) and a memory
//     ceiling (32 MiB, five times the largest instance);
//   - one conversion at a time. Uploads are an administrator's, and rare;
//     the ceiling is per instance, so this is what makes it a ceiling on the
//     server too.
//
// The module is compiled on first use rather than at start-up: compiling it
// costs about half a second and, with the compiled code, some 65 MiB of
// memory, which a server that never sees a .milk with shaders should not pay.
//
// This package knows nothing about presets. internal/milkdrop does the string
// work either side (taking the shader out of the .milk, wrapping it in
// MilkDrop's header, trimming the result back to a shader_body) and calls this
// through an interface, so its handler tests never need the module.
package msc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"
)

// The limits ADR 0091 sets for one conversion.
const (
	// Deadline is about seven times the slowest real shader the spike
	// converted with one worker (434 ms).
	Deadline = 3 * time.Second
	// MemoryLimitBytes is five times the largest instance the spike saw
	// (5.9 MiB).
	MemoryLimitBytes = 32 << 20
	// MaxOutputBytes bounds what the module may write to stdout. The largest
	// converted shader in the corpus is under 24 KB; anything near this is
	// not a shader butterchurn will be handed.
	MaxOutputBytes = 1 << 20
	// maxLogBytes bounds stderr, of which only the first line is kept.
	maxLogBytes = 64 << 10
)

// The converter's exit codes (build/convert.cpp).
const (
	exitParse     = 1
	exitTranslate = 2
	exitNonASCII  = 3
	exitOptimizer = 4
	exitEmpty     = 5
)

// Failure is a shader the converter would not convert, with the reason it
// gave. It is what an upload records; anything else Convert returns is the
// server's own fault.
type Failure struct {
	// Reason is the first line of what the converter said, or what stopped
	// it: a trap, the deadline, the memory ceiling.
	Reason string
}

func (f *Failure) Error() string { return f.Reason }

// Converter converts shaders with the embedded module. The zero value is not
// usable; call New.
type Converter struct {
	wasm     func() ([]byte, error)
	deadline time.Duration
	memPages uint32
	maxOut   int

	once     sync.Once
	rt       wazero.Runtime
	compiled wazero.CompiledModule
	initErr  error

	// sem holds one token: one conversion at a time.
	sem chan struct{}

	// inflight and maxInflight are for the test that holds the one-at-a-time
	// rule; nothing else reads them.
	mu                    sync.Mutex
	inflight, maxInflight int
}

// New is a converter for the embedded module. Nothing is compiled until the
// first Convert.
func New() *Converter {
	return newConverter(inflate, Deadline, MemoryLimitBytes)
}

func newConverter(wasm func() ([]byte, error), deadline time.Duration, memBytes int) *Converter {
	return &Converter{
		wasm:     wasm,
		deadline: deadline,
		memPages: uint32(memBytes / 65536),
		maxOut:   MaxOutputBytes,
		sem:      make(chan struct{}, 1),
	}
}

/*
 * compile builds the runtime and compiles the module, once.
 *
 * wazero's compiler, not its interpreter: the spike measured the interpreter
 * at 11 times slower (1.1 s a shader), and every release target is amd64 or
 * arm64, where the compiler runs. On anything else wazero has only the
 * interpreter, and that is still correct, only slower.
 *
 * The context is not the request's. The compiled module outlives the request
 * that happened to need it first, and a client that hangs up mid-compile must
 * not leave the server with a half-made runtime it then keeps for good.
 */
func (c *Converter) compile() error {
	c.once.Do(func() {
		ctx := context.Background()
		cfg := wazero.NewRuntimeConfigInterpreter()
		if runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64" {
			cfg = wazero.NewRuntimeConfigCompiler()
		}
		cfg = cfg.WithMemoryLimitPages(c.memPages).WithCloseOnContextDone(true)
		rt := wazero.NewRuntimeWithConfig(ctx, cfg)
		if _, err := wasi_snapshot_preview1.Instantiate(ctx, rt); err != nil {
			rt.Close(ctx)
			c.initErr = fmt.Errorf("shader converter: wasi: %w", err)
			return
		}
		wasm, err := c.wasm()
		if err != nil {
			rt.Close(ctx)
			c.initErr = fmt.Errorf("shader converter: read module: %w", err)
			return
		}
		compiled, err := rt.CompileModule(ctx, wasm)
		if err != nil {
			rt.Close(ctx)
			c.initErr = fmt.Errorf("shader converter: compile module: %w", err)
			return
		}
		c.mu.Lock()
		c.rt, c.compiled = rt, compiled
		c.mu.Unlock()
	})
	return c.initErr
}

// Compiled reports whether the module has been compiled yet.
func (c *Converter) Compiled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.rt != nil
}

/*
 * Convert turns one prepared HLSL shader — already wrapped in MilkDrop's
 * header by internal/milkdrop — into the GLSL glsl-optimizer printed.
 *
 * A shader the converter will not convert is a *Failure. Any other error is
 * the server's: the module would not compile, or ctx ended while this call
 * waited its turn.
 */
func (c *Converter) Convert(ctx context.Context, hlsl string) (string, error) {
	select {
	case c.sem <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { <-c.sem }()

	if err := c.compile(); err != nil {
		return "", err
	}
	c.mu.Lock()
	c.inflight++
	if c.inflight > c.maxInflight {
		c.maxInflight = c.inflight
	}
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.inflight--
		c.mu.Unlock()
	}()

	stdout := &capped{max: c.maxOut}
	stderr := &capped{max: maxLogBytes}
	// No WithFS, no WithEnv, no WithSysWalltime: the instance sees no files,
	// no environment and wazero's fake clock. Arguments are its name and
	// nothing else.
	cfg := wazero.NewModuleConfig().
		WithName("").
		WithArgs("msc").
		WithStdin(strings.NewReader(hlsl)).
		WithStdout(stdout).
		WithStderr(stderr)

	// The deadline runs from the start of this conversion, not from when the
	// request arrived: waiting for the one slot is not the converter's time.
	// It is derived from ctx all the same, so a request that is cancelled
	// stops its conversion too.
	rctx, cancel := context.WithTimeout(ctx, c.deadline)
	defer cancel()
	mod, err := c.rt.InstantiateModule(rctx, c.compiled, cfg)
	if mod != nil {
		mod.Close(context.Background())
	}
	if err == nil || isExit(err, 0) {
		if stdout.over {
			return "", &Failure{Reason: fmt.Sprintf("the converted shader is over %d KB", c.maxOut>>10)}
		}
		return stdout.String(), nil
	}
	return "", c.failure(ctx, err, stderr.String())
}

func isExit(err error, code uint32) bool {
	var exit *sys.ExitError
	return errors.As(err, &exit) && exit.ExitCode() == code
}

// failure says why a conversion stopped, as a person uploading a preset would
// want it said.
func (c *Converter) failure(ctx context.Context, err error, log string) error {
	// The request ending is not the shader's fault, and is not recorded as
	// though it were.
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var exit *sys.ExitError
	if errors.As(err, &exit) {
		switch exit.ExitCode() {
		case sys.ExitCodeDeadlineExceeded:
			return &Failure{Reason: fmt.Sprintf("the converter did not finish within %v", c.deadline)}
		case exitEmpty:
			return &Failure{Reason: "the shader is empty"}
		case exitParse, exitTranslate, exitNonASCII, exitOptimizer:
			if line := firstLine(log); line != "" {
				return &Failure{Reason: line}
			}
		}
		return &Failure{Reason: fmt.Sprintf("the converter exited with code %d", exit.ExitCode())}
	}
	// Anything else from a running instance is a trap: the converter's own
	// memory-safety bug, caught by the sandbox. wazero names it ("wasm error:
	// invalid table access"); the stack trace after it is not for a person.
	// Running out of memory looks the same from here: past the ceiling,
	// memory.grow fails, malloc returns null, and operator new — built
	// without exceptions — aborts, which is a trap.
	msg := firstLine(err.Error())
	return &Failure{Reason: "the converter crashed: " + strings.TrimPrefix(msg, "module[] function[_start] failed: ")}
}

// firstLine is the first line of s with anything in it, trimmed.
func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return ""
}

// capped keeps at most max bytes and notes that more was written. It never
// returns an error: a module whose write failed would print its reason to
// the stream that just failed.
type capped struct {
	buf  bytes.Buffer
	max  int
	over bool
}

func (w *capped) Write(p []byte) (int, error) {
	if room := w.max - w.buf.Len(); room < len(p) {
		w.over = true
		if room > 0 {
			w.buf.Write(p[:room])
		}
		return len(p), nil
	}
	return w.buf.Write(p)
}

func (w *capped) String() string { return w.buf.String() }

// Close releases the runtime and the compiled module, if there are any.
func (c *Converter) Close(ctx context.Context) error {
	c.sem <- struct{}{}
	defer func() { <-c.sem }()
	if c.rt == nil {
		return nil
	}
	return c.rt.Close(ctx)
}

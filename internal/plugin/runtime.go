package plugin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// HTTPGetter performs the one outbound call a plugin is allowed, host-mediated.
// Injected so tests never touch the network and so the host — not the module —
// owns egress.
type HTTPGetter func(ctx context.Context, url string) ([]byte, error)

// SecretResolver returns a configured secret by name, or "" if unset. The
// manifest gate runs before this is ever called, so it only sees names a plugin
// was granted.
type SecretResolver func(name string) string

// Runtime hosts compiled plugins. One per process is enough; it holds the
// wazero runtime and the host-function module every plugin shares.
type Runtime struct {
	wz     wazero.Runtime
	log    *slog.Logger
	httpc  HTTPGetter
	secret SecretResolver
}

// Option customizes a Runtime.
type Option func(*Runtime)

// WithHTTPGetter overrides the outbound fetch (tests inject a fake).
func WithHTTPGetter(g HTTPGetter) Option { return func(r *Runtime) { r.httpc = g } }

// WithSecretResolver supplies the secret lookup (the host wires this to config).
func WithSecretResolver(s SecretResolver) Option { return func(r *Runtime) { r.secret = s } }

// pluginKey carries the calling plugin into host functions, so each call's
// capability checks run against the right manifest without a shared registry.
type pluginKey struct{}

// NewRuntime builds the wazero runtime and instantiates the shared host module.
// The three host functions are always present; each enforces the calling
// plugin's manifest internally, so an ungranted capability fails at the call
// rather than by a missing import the module cannot link against.
func NewRuntime(ctx context.Context, log *slog.Logger, opts ...Option) (*Runtime, error) {
	rt := &Runtime{
		log:    log,
		httpc:  defaultHTTPGet,
		secret: func(string) string { return "" },
	}
	for _, o := range opts {
		o(rt)
	}

	rt.wz = wazero.NewRuntime(ctx)
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, rt.wz); err != nil {
		return nil, fmt.Errorf("plugin runtime: wasi: %w", err)
	}
	_, err := rt.wz.NewHostModuleBuilder("env").
		NewFunctionBuilder().WithFunc(rt.hostLog).Export("host_log").
		NewFunctionBuilder().WithFunc(rt.hostHTTPGet).Export("host_http_get").
		NewFunctionBuilder().WithFunc(rt.hostSecret).Export("host_secret").
		Instantiate(ctx)
	if err != nil {
		return nil, fmt.Errorf("plugin runtime: host module: %w", err)
	}
	return rt, nil
}

// Close releases the runtime and every compiled module.
func (rt *Runtime) Close(ctx context.Context) error { return rt.wz.Close(ctx) }

// Plugin is a compiled, ready-to-call module and its manifest.
type Plugin struct {
	Manifest Manifest
	rt       *Runtime
	compiled wazero.CompiledModule
}

// Load compiles a module from bytes under a validated manifest.
func (rt *Runtime) Load(ctx context.Context, m Manifest, wasm []byte) (*Plugin, error) {
	compiled, err := rt.wz.CompileModule(ctx, wasm)
	if err != nil {
		return nil, fmt.Errorf("compile plugin %q: %w", m.Name, err)
	}
	return &Plugin{Manifest: m, rt: rt, compiled: compiled}, nil
}

// LoadDir loads a plugin from a directory holding plugin.json and plugin.wasm.
func (rt *Runtime) LoadDir(ctx context.Context, dir string) (*Plugin, error) {
	manifestBytes, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	m, err := ParseManifest(manifestBytes)
	if err != nil {
		return nil, err
	}
	wasm, err := os.ReadFile(filepath.Join(dir, "plugin.wasm"))
	if err != nil {
		return nil, fmt.Errorf("read module: %w", err)
	}
	return rt.Load(ctx, m, wasm)
}

// Call invokes an exported function with input bytes and returns its output.
// A fresh module instance is used per call — the simple, isolation-safe baseline;
// pooling is a later optimisation. The calling plugin rides in the context so
// the host functions can check its capabilities.
func (p *Plugin) Call(ctx context.Context, fn string, input []byte) ([]byte, error) {
	ctx = context.WithValue(ctx, pluginKey{}, p)

	// Anonymous name so the same compiled module can be instantiated repeatedly;
	// _initialize (not _start) runs the Go runtime setup for a reactor module.
	cfg := wazero.NewModuleConfig().WithName("").WithStartFunctions("_initialize")
	mod, err := p.rt.wz.InstantiateModule(ctx, p.compiled, cfg)
	if err != nil {
		return nil, fmt.Errorf("instantiate %q: %w", p.Manifest.Name, err)
	}
	defer mod.Close(ctx)

	var inPtr, inLen uint32
	if len(input) > 0 {
		inPtr, err = guestAlloc(ctx, mod, input)
		if err != nil {
			return nil, err
		}
		inLen = uint32(len(input))
	}

	f := mod.ExportedFunction(fn)
	if f == nil {
		return nil, fmt.Errorf("plugin %q has no export %q", p.Manifest.Name, fn)
	}
	res, err := f.Call(ctx, uint64(inPtr), uint64(inLen))
	if err != nil {
		return nil, fmt.Errorf("plugin %q: %s: %w", p.Manifest.Name, fn, err)
	}
	if len(res) == 0 {
		return nil, nil
	}
	out, ok := readPacked(mod, res[0])
	if !ok {
		return nil, errors.New("plugin returned an unreadable result")
	}
	// Copy out before the module (and its memory) is closed.
	cp := make([]byte, len(out))
	copy(cp, out)
	return cp, nil
}

func defaultHTTPGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func pluginFromCtx(ctx context.Context) *Plugin {
	p, _ := ctx.Value(pluginKey{}).(*Plugin)
	return p
}

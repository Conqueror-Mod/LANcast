package plugin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"lancast/internal/meta"
	"lancast/internal/netguard"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// HTTPGetter performs the one outbound call a plugin is allowed, host-mediated.
// Injected so tests never touch the network and so the host — not the module —
// owns egress.
type HTTPGetter func(ctx context.Context, url string) ([]byte, error)

/*
 * SecretResolver returns a configured secret, or "" if unset. The manifest gate
 * runs before this is ever called, so it only sees names a plugin was granted.
 *
 * It takes the plugin's name as well as the secret's, and that is the whole
 * point: a credential is obtained *for* a plugin. Two plugins asking for
 * "api_key" are asking for two different things, and a resolver that could not
 * tell them apart would have to answer both from one value or answer neither.
 * Before this the host resolved from a fixed list of first-party names and
 * answered "" for everything else — so a third-party plugin could be granted a
 * secret it could never read.
 */
type SecretResolver func(plugin, name string) string

// Runtime hosts compiled plugins. One per process is enough; it holds the
// wazero runtime and the host-function module every plugin shares.
type Runtime struct {
	wz     wazero.Runtime
	log    *slog.Logger
	httpc  HTTPGetter
	secret SecretResolver

	// The fetch policy every plugin gets and none can decline — see
	// fetchpolicy.go.
	cache      ResponseCache
	cacheTTL   time.Duration
	ratePerSec float64
	limMu      sync.Mutex
	limiters   map[string]*meta.Limiter
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
		secret: func(string, string) string { return "" },
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

	// exports is what the module actually exports, read once at compile time.
	// It is what makes an *optional* export possible (ADR 0064): the host can
	// ask before calling, so adding one does not break a module built before
	// it existed.
	exports map[string]bool
}

// HasExport reports whether the module exports a function. Used for exports the
// host calls only when they are present — adding one of those is a non-breaking
// change, and this is what makes that true rather than aspirational.
func (p *Plugin) HasExport(name string) bool { return p.exports[name] }

// Load compiles a module from bytes under a validated manifest.
func (rt *Runtime) Load(ctx context.Context, m Manifest, wasm []byte) (*Plugin, error) {
	compiled, err := rt.wz.CompileModule(ctx, wasm)
	if err != nil {
		return nil, fmt.Errorf("compile plugin %q: %w", m.Name, err)
	}
	exports := make(map[string]bool)
	for name := range compiled.ExportedFunctions() {
		exports[name] = true
	}
	/*
	 * Refuse a module that cannot answer for the kind it claims, here rather
	 * than at the first call.
	 *
	 * Without this a provider missing `search` installed cleanly, listed as
	 * enabled, and failed the first time the enricher asked it anything — with
	 * a message that reaches a log rather than the person who just installed
	 * it. The exports are known at compile time, so the honest moment to say so
	 * is now.
	 */
	for _, fn := range append([]string{allocEntrypoint}, requiredExports[m.Kind]...) {
		if !exports[fn] {
			compiled.Close(ctx)
			return nil, fmt.Errorf("plugin %q is kind %q but exports no %q",
				m.Name, m.Kind, fn)
		}
	}
	return &Plugin{Manifest: m, rt: rt, compiled: compiled, exports: exports}, nil
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
	/*
	 * Guarded, because the manifest allowlist checks a *name* and a name is not
	 * a destination.
	 *
	 * A plugin lists `api.example.com`, the person installing it sees that in
	 * the grant dialog and reasonably agrees — and whoever owns the name also
	 * owns what it resolves to, and can point it at 127.0.0.1 whenever they
	 * like, including after the grant. The allowlist would still match the
	 * string.
	 *
	 * So the address is checked after resolution and before connect. The
	 * allowlist decides which service a plugin may talk to; this decides which
	 * addresses anything may be reached at. A plugin needs both.
	 */
	client := netguard.Client(15 * time.Second)
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

package plugin

import (
	"context"
	"log/slog"

	"github.com/tetratelabs/wazero/api"
)

// The host functions are the only authority a module has. Each reads the calling
// plugin from the context and checks it against that plugin's manifest before
// doing anything — deny-by-default, enforced here rather than by which imports a
// module happens to link.

// hostLog lets a module emit diagnostics. Level maps to slog; anything a plugin
// logs is attributed to it, never trusted as host state.
func (rt *Runtime) hostLog(ctx context.Context, mod api.Module, level, ptr, length uint32) {
	msg, ok := mod.Memory().Read(ptr, length)
	if !ok {
		return
	}
	p := pluginFromCtx(ctx)
	name := "?"
	if p != nil {
		name = p.Manifest.Name
	}
	rt.log.Log(ctx, slogLevel(level), "plugin log", "plugin", name, "msg", string(msg))
}

// hostHTTPGet performs the one outbound call a plugin may make, and only to a
// host its manifest declared. The host owns the connection; the module gets
// bytes back, never a socket. An undeclared host or a failed fetch returns an
// empty span, which the guest reads as "no data".
func (rt *Runtime) hostHTTPGet(ctx context.Context, mod api.Module, urlPtr, urlLen uint32) uint64 {
	raw, ok := mod.Memory().Read(urlPtr, urlLen)
	if !ok {
		return 0
	}
	url := string(raw)
	p := pluginFromCtx(ctx)
	if p == nil || !p.Manifest.allowsURL(url) {
		name := "?"
		if p != nil {
			name = p.Manifest.Name
		}
		rt.log.Warn("plugin http denied", "plugin", name, "url", url)
		return 0
	}
	body, err := rt.httpc(ctx, url)
	if err != nil {
		rt.log.Debug("plugin http failed", "plugin", p.Manifest.Name, "url", url, "error", err)
		return 0
	}
	return writeToGuest(ctx, mod, body)
}

// hostSecret returns a configured secret, but only one the plugin's manifest
// granted by name. An ungranted or unset name returns an empty span.
func (rt *Runtime) hostSecret(ctx context.Context, mod api.Module, namePtr, nameLen uint32) uint64 {
	raw, ok := mod.Memory().Read(namePtr, nameLen)
	if !ok {
		return 0
	}
	name := string(raw)
	p := pluginFromCtx(ctx)
	if p == nil || !p.Manifest.allowsSecret(name) {
		pn := "?"
		if p != nil {
			pn = p.Manifest.Name
		}
		rt.log.Warn("plugin secret denied", "plugin", pn, "secret", name)
		return 0
	}
	val := rt.secret(name)
	if val == "" {
		return 0
	}
	return writeToGuest(ctx, mod, []byte(val))
}

// slogLevel maps the guest's numeric level to slog's. Unknown values are info.
func slogLevel(level uint32) slog.Level {
	switch level {
	case 0:
		return slog.LevelDebug
	case 2:
		return slog.LevelWarn
	case 3:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

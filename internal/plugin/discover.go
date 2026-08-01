package plugin

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"lancast/internal/meta"
)

// LoadAll loads every plugin under root — one subdirectory per plugin, each with
// a plugin.json and plugin.wasm. A missing root is not an error (plugins are
// optional); a single malformed or unloadable plugin is logged and skipped so
// one bad plugin never takes down startup.
func (rt *Runtime) LoadAll(ctx context.Context, root string) []*Plugin {
	entries, err := os.ReadDir(root)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			rt.log.Warn("plugin dir unreadable", "dir", root, "error", err)
		}
		return nil
	}

	var out []*Plugin
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		p, err := rt.LoadDir(ctx, dir)
		if err != nil {
			rt.log.Warn("skipping plugin", "dir", dir, "error", err)
			continue
		}
		rt.log.Info("loaded plugin", "name", p.Manifest.Name, "kind", p.Manifest.Kind, "version", p.Manifest.Version)
		out = append(out, p)
	}
	return out
}

// RegisterInto adds each plugin to a registry by its kind — today only
// rating_source sources. A plugin whose kind has no registration path yet is
// skipped with a log, not an error: the manifest already validated the kind, so
// this is "the host does not wire this kind in yet", a forward-compatible state.
func RegisterInto(reg *meta.Registry, plugins []*Plugin, log logger) {
	for _, p := range plugins {
		switch p.Manifest.Kind {
		case KindRatingSource:
			rs, err := NewRatingSource(p)
			if err != nil {
				log.Warn("plugin not registered", "name", p.Manifest.Name, "error", err)
				continue
			}
			reg.AddRatingSource(rs)
		default:
			log.Warn("plugin kind has no registration path", "name", p.Manifest.Name, "kind", p.Manifest.Kind)
		}
	}
}

// logger is the slice of *slog.Logger RegisterInto needs, kept small so callers
// are not forced to thread the whole logger type when a nil-safe subset will do.
type logger interface {
	Warn(msg string, args ...any)
}

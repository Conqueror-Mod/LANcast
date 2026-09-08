package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// InstalledPlugin is one installed plugin and the capability grant the operator
// approved for it (ADR 0021). GrantedHTTP and GrantedSecrets are the record of
// consent — the effective authority, independent of what the manifest now asks.
type InstalledPlugin struct {
	Name           string   `json:"name"`
	Version        string   `json:"version"`
	Kind           string   `json:"kind"`
	Digest         string   `json:"digest"`
	Signer         string   `json:"signer"`
	Enabled        bool     `json:"enabled"`
	GrantedHTTP    []string `json:"granted_http"`
	GrantedSecrets []string `json:"granted_secrets"`
	InstalledAt    int64    `json:"installed_at"`
}

// InstallPlugin records (or replaces) an installed plugin and its grant. Keyed by
// name, so re-installing a plugin overwrites the prior row — including its digest
// and grant, which is how a changed manifest requires a fresh approval.
func (s *Store) InstallPlugin(ctx context.Context, p InstalledPlugin) error {
	httpJSON, err := json.Marshal(nonNilStrings(p.GrantedHTTP))
	if err != nil {
		return fmt.Errorf("install plugin: %w", err)
	}
	secretsJSON, err := json.Marshal(nonNilStrings(p.GrantedSecrets))
	if err != nil {
		return fmt.Errorf("install plugin: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO installed_plugin
			(name, version, kind, digest, signer, enabled, granted_http, granted_secrets, installed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			version = excluded.version, kind = excluded.kind, digest = excluded.digest,
			signer = excluded.signer, enabled = excluded.enabled,
			granted_http = excluded.granted_http, granted_secrets = excluded.granted_secrets,
			installed_at = excluded.installed_at`,
		p.Name, p.Version, p.Kind, p.Digest, p.Signer, boolToInt(p.Enabled),
		string(httpJSON), string(secretsJSON), time.Now().Unix())
	if err != nil {
		return fmt.Errorf("install plugin: %w", err)
	}
	return nil
}

// ListInstalledPlugins returns every installed plugin, name order.
func (s *Store) ListInstalledPlugins(ctx context.Context) ([]InstalledPlugin, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT name, version, kind, digest, signer, enabled, granted_http, granted_secrets, installed_at
		FROM installed_plugin ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list plugins: %w", err)
	}
	defer rows.Close()

	out := []InstalledPlugin{}
	for rows.Next() {
		var p InstalledPlugin
		var enabled int
		var httpJSON, secretsJSON string
		if err := rows.Scan(&p.Name, &p.Version, &p.Kind, &p.Digest, &p.Signer,
			&enabled, &httpJSON, &secretsJSON, &p.InstalledAt); err != nil {
			return nil, fmt.Errorf("list plugins: %w", err)
		}
		p.Enabled = enabled != 0
		if err := json.Unmarshal([]byte(httpJSON), &p.GrantedHTTP); err != nil {
			return nil, fmt.Errorf("list plugins: granted_http: %w", err)
		}
		if err := json.Unmarshal([]byte(secretsJSON), &p.GrantedSecrets); err != nil {
			return nil, fmt.Errorf("list plugins: granted_secrets: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetInstalledPlugin returns one plugin by name, or ErrNotFound.
func (s *Store) GetInstalledPlugin(ctx context.Context, name string) (InstalledPlugin, error) {
	var p InstalledPlugin
	var enabled int
	var httpJSON, secretsJSON string
	err := s.db.QueryRowContext(ctx, `
		SELECT name, version, kind, digest, signer, enabled, granted_http, granted_secrets, installed_at
		FROM installed_plugin WHERE name = ?`, name).Scan(
		&p.Name, &p.Version, &p.Kind, &p.Digest, &p.Signer,
		&enabled, &httpJSON, &secretsJSON, &p.InstalledAt)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	if err != nil {
		return p, fmt.Errorf("get plugin: %w", err)
	}
	p.Enabled = enabled != 0
	_ = json.Unmarshal([]byte(httpJSON), &p.GrantedHTTP)
	_ = json.Unmarshal([]byte(secretsJSON), &p.GrantedSecrets)
	return p, nil
}

// SetPluginEnabled flips a plugin on or off. The change takes effect on the next
// registry rebuild.
func (s *Store) SetPluginEnabled(ctx context.Context, name string, enabled bool) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE installed_plugin SET enabled = ? WHERE name = ?`, boolToInt(enabled), name)
	if err != nil {
		return fmt.Errorf("set plugin enabled: %w", err)
	}
	return notFoundIfZero(res)
}

// RemovePlugin forgets an installed plugin. The caller removes its unpacked
// files; this drops the record.
func (s *Store) RemovePlugin(ctx context.Context, name string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM installed_plugin WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("remove plugin: %w", err)
	}
	// Its credentials go with it. A stored key outliving the plugin it was
	// obtained for is a credential nobody can see, in a table nobody would
	// think to look in, waiting to be handed to whatever installs under that
	// name next.
	if _, err := s.db.ExecContext(ctx, `DELETE FROM plugin_secret WHERE plugin = ?`, name); err != nil {
		return fmt.Errorf("remove plugin secrets: %w", err)
	}
	return notFoundIfZero(res)
}

// notFoundIfZero maps "no rows affected" to ErrNotFound, so callers can tell a
// missing plugin from a real failure.
func notFoundIfZero(res interface{ RowsAffected() (int64, error) }) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

/*
 * A plugin's own credentials.
 *
 * The API never reads a value back — SetPluginSecret writes one and
 * ConfiguredPluginSecrets reports which names have one. A value leaves this
 * package in exactly one direction, to the host function that hands it to the
 * guest that was granted it.
 */

// SetPluginSecret stores a plugin's credential, replacing any previous value.
// An empty value deletes the row rather than storing emptiness, because "" is
// what an unset secret already reads as and two spellings of unset is one
// spelling too many.
func (s *Store) SetPluginSecret(ctx context.Context, plugin, name, value string) error {
	if value == "" {
		return s.DeletePluginSecret(ctx, plugin, name)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO plugin_secret (plugin, name, value, set_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(plugin, name) DO UPDATE SET value = excluded.value, set_at = excluded.set_at`,
		plugin, name, value, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("set plugin secret: %w", err)
	}
	return nil
}

// DeletePluginSecret forgets one credential. Deleting one that was never set is
// not an error: the caller asked for it to be gone and it is.
func (s *Store) DeletePluginSecret(ctx context.Context, plugin, name string) error {
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM plugin_secret WHERE plugin = ? AND name = ?`, plugin, name); err != nil {
		return fmt.Errorf("delete plugin secret: %w", err)
	}
	return nil
}

// PluginSecret returns one plugin's stored credential. The second result
// distinguishes "no row" from "a row holding empty", though the setter makes
// the latter unreachable.
func (s *Store) PluginSecret(ctx context.Context, plugin, name string) (string, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx,
		`SELECT value FROM plugin_secret WHERE plugin = ? AND name = ?`, plugin, name).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("plugin secret: %w", err)
	}
	return v, true, nil
}

// ConfiguredPluginSecrets lists the names this plugin has a stored value for.
// Names only — this is what lets the Add-ons page say "granted, but you have
// not given it one yet" without the value ever leaving the database.
func (s *Store) ConfiguredPluginSecrets(ctx context.Context, plugin string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT name FROM plugin_secret WHERE plugin = ? ORDER BY name`, plugin)
	if err != nil {
		return nil, fmt.Errorf("configured plugin secrets: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("configured plugin secrets: %w", err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

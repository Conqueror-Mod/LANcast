// Package plugin is the WebAssembly plugin runtime (ADR 0020).
//
// A plugin is a .wasm module plus a manifest declaring the capabilities it
// needs. The host instantiates the module with only the host functions those
// capabilities map to — deny-by-default — so a module has no ambient access to
// the filesystem, the network, secrets, or the database. It returns data; the
// host owns all persistence. This is the isolation boundary M4 is built on, and
// the interfaces it adapts to (meta.RatingSource first) are unchanged (ADR 0007).
package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ABIVersion is the host↔module contract this build implements. A module
// declaring a different major is refused rather than run against a boundary it
// was not built for. It is deliberately separate from the HTTP API version
// (ADR 0018) — a different contract with a different audience.
const ABIVersion = 1

// Kind is what a plugin extends. The set is intentionally narrow to start: the
// first contract is "a new source for an existing capability", not "a new
// capability". Widening it waits for a real plugin that needs it.
type Kind string

const KindRatingSource Kind = "rating_source"

var supportedKinds = map[Kind]bool{
	KindRatingSource: true,
}

// Capabilities is the authority a plugin asks for. Anything not listed here is
// denied; the host grants exactly these and nothing more.
type Capabilities struct {
	// HTTP is the set of hosts the module may reach, and only via the
	// host-mediated fetch — never a raw socket.
	HTTP []string `json:"http"`
	// Secrets is the set of configured secret names the module may read.
	Secrets []string `json:"secrets"`
}

// Manifest is the plugin.json beside a module's .wasm.
type Manifest struct {
	Name         string       `json:"name"`
	Version      string       `json:"version"`
	ABI          int          `json:"abi"`
	Kind         Kind         `json:"kind"`
	Capabilities Capabilities `json:"capabilities"`
}

// ParseManifest decodes and validates a manifest. An unknown kind or an
// unsupported ABI is refused at load, not discovered mid-call.
func ParseManifest(data []byte) (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return m, fmt.Errorf("parse manifest: %w", err)
	}
	if m.Name == "" {
		return m, errors.New("manifest: name is required")
	}
	if m.ABI != ABIVersion {
		return m, fmt.Errorf("manifest: abi %d unsupported (host implements %d)", m.ABI, ABIVersion)
	}
	if !supportedKinds[m.Kind] {
		return m, fmt.Errorf("manifest: unknown kind %q", m.Kind)
	}
	return m, nil
}

// allowsHost reports whether a bare hostname is in the HTTP capability.
func (m Manifest) allowsHost(host string) bool {
	for _, h := range m.Capabilities.HTTP {
		if strings.EqualFold(h, host) {
			return true
		}
	}
	return false
}

// allowsURL parses a URL and reports whether its host is granted. A URL that
// does not parse, or names no host, is denied.
func (m Manifest) allowsURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return false
	}
	return m.allowsHost(u.Hostname())
}

// allowsSecret reports whether a secret name is granted.
func (m Manifest) allowsSecret(name string) bool {
	for _, s := range m.Capabilities.Secrets {
		if s == name {
			return true
		}
	}
	return false
}

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
//
// The version gates the **whole** contract, not each function: a module
// declaring 2 gets all of ABI 2 and none of ABI 1. That is the cheap answer
// while the only implementations are ours, and it is the honest one — a
// per-function negotiation would mean the host supporting every combination
// anybody ever shipped.
//
// 2 added the response envelope (ADR 0063). Before it a guest had no way to say
// a call failed: an empty span meant "nothing", so an upstream that was down
// reported itself exactly as one that found nothing. Breaking a contract with
// one implementation, all of it ours, cost a rebuild; the same change after
// publication would have cost everybody else's.
const ABIVersion = 2

// Kind is what a plugin extends. The set is intentionally narrow to start: the
// first contract is "a new source for an existing capability", not "a new
// capability". Widening it waits for a real plugin that needs it.
type Kind string

const (
	KindRatingSource Kind = "rating_source"
	// KindProvider is a searchable metadata source: `search` and `fetch`,
	// adapted to meta.Provider. It is the second shape, and building it is what
	// found the two holes ADR 0063 records.
	KindProvider Kind = "provider"
)

var supportedKinds = map[Kind]bool{
	KindRatingSource: true,
	KindProvider:     true,
}

/*
 * Caps is what a provider plugin says it can answer for, declared in the
 * manifest rather than exported by the module (ADR 0063).
 *
 * The host needs this before deciding whether to load the plugin at all, and an
 * export would mean instantiating a WASM module to ask a question about whether
 * to instantiate it. The manifest is signed and readable without starting
 * anything.
 *
 * A plugin can misdeclare in either direction — claiming a kind it cannot answer
 * for, or hiding one it can. Neither is a safety question: the first costs a
 * wasted call that returns nothing, the second costs a source nobody asked. So
 * this trades no trust, only work.
 */
type Caps struct {
	Movie   bool `json:"movie"`
	Show    bool `json:"show"`
	Episode bool `json:"episode"`
	Artwork bool `json:"artwork"`
}

// any reports whether the caps name at least one kind this plugin could answer.
func (c Caps) any() bool { return c.Movie || c.Show || c.Episode }

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
	// Caps is meaningful only for KindProvider, and is what the host consults
	// before asking this plugin about a movie or an episode.
	Caps Caps `json:"caps"`
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
	// A provider that answers for no kind is never consulted about anything.
	// Refusing at load says so once, where somebody is reading an error; loading
	// it would mean a plugin that appears installed and is silent for ever.
	if m.Kind == KindProvider && !m.Caps.any() {
		return m, errors.New("manifest: a provider must declare at least one of caps.movie, caps.show, caps.episode")
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

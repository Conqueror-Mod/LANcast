package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
)

/*
 * The response envelope (ABI 2, ADR 0063).
 *
 * ABI 1 had no way for a module to say a call went wrong. A guest returned its
 * payload, and an empty span meant "nothing" — so an upstream API that was down,
 * rate-limited, or answering nonsense had exactly the same way to report itself
 * as one that looked and genuinely found nothing.
 *
 * For a rating source that is survivable: no scores today, scores tomorrow, and
 * the item is no worse off. For a provider it is not, and that is what forced
 * this. "No candidates" means *this item is unmatched* — a conclusion the
 * enricher records and stops re-asking — while "the API is down" means ask
 * again later. Collapsing the two writes a permanent answer from a temporary
 * failure, which is the same shape of mistake as a scan deleting a file it
 * could not read.
 *
 * The envelope is deliberately not a status code. A module returning `error`
 * says what went wrong in words, and those words reach the log with the
 * plugin's name attached — which is the difference between "the plugin failed"
 * and "the plugin said its key was rejected".
 */
type envelope struct {
	// Result is the call's payload, whatever shape the entry point defines.
	// Present and possibly empty on success.
	Result json.RawMessage `json:"result,omitempty"`
	// Error is a message from the guest. Non-empty means the call failed and
	// Result means nothing.
	Error string `json:"error,omitempty"`
}

// ErrPluginRefused is what a guest-reported failure unwraps to, so a caller can
// tell "the plugin says it could not" from "the plugin is broken" without
// matching on message text.
var ErrPluginRefused = errors.New("plugin reported a failure")

/*
 * decodeEnvelope unpacks a guest response into the payload it carries.
 *
 * An empty response is an empty *success*, not a failure. A module that has
 * nothing to say returns nothing, and requiring it to spell out `{"result":[]}`
 * would make the common case the wordy one — and would break every guest that
 * simply returned early.
 */
func decodeEnvelope(name string, out []byte) (json.RawMessage, error) {
	if len(out) == 0 {
		return nil, nil
	}
	var env envelope
	if err := json.Unmarshal(out, &env); err != nil {
		return nil, fmt.Errorf("plugin %q returned a malformed response: %w", name, err)
	}
	if env.Error != "" {
		return nil, fmt.Errorf("plugin %q: %s: %w", name, env.Error, ErrPluginRefused)
	}
	return env.Result, nil
}

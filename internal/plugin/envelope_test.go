package plugin

import (
	"errors"
	"strings"
	"testing"
)

/*
 * The distinction ABI 2 exists to carry (ADR 0063).
 *
 * Under ABI 1 a guest said "nothing" and "it went wrong" the same way: an empty
 * span. Survivable for a rating source — no scores until tomorrow — and not for
 * a provider, where "no candidates" means *this item is unmatched*, which the
 * enricher writes down and stops re-asking, while "the API is down" means try
 * later. A permanent conclusion from a temporary failure.
 */

func TestAnEmptyResponseIsAnEmptySuccess(t *testing.T) {
	payload, err := decodeEnvelope("x", nil)
	if err != nil {
		t.Fatalf("err = %v; a guest with nothing to say returns nothing, and "+
			"requiring it to spell out an empty result would make the common "+
			"case the wordy one", err)
	}
	if payload != nil {
		t.Errorf("payload = %s, want nothing", payload)
	}
}

func TestAResultIsUnwrapped(t *testing.T) {
	payload, err := decodeEnvelope("x", []byte(`{"result":[{"source":"imdb"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), "imdb") {
		t.Errorf("payload = %s, want the inner result", payload)
	}
}

/*
 * A reported failure is an error, and it says what the guest said.
 *
 * The message is the point. "The plugin failed" sends somebody to the plugin;
 * "omdb rejected the key" sends them to the setting that is actually wrong.
 */
func TestAReportedFailureIsAnErrorAndKeepsItsWords(t *testing.T) {
	_, err := decodeEnvelope("omdb", []byte(`{"error":"invalid api key"}`))
	if err == nil {
		t.Fatal("a guest-reported failure came back as success")
	}
	if !errors.Is(err, ErrPluginRefused) {
		t.Errorf("err = %v, want it to unwrap to ErrPluginRefused so callers "+
			"need not match on message text", err)
	}
	for _, want := range []string{"omdb", "invalid api key"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %q, missing %q", err.Error(), want)
		}
	}
}

// A failure is not an empty result, which is the whole distinction stated as
// one assertion.
func TestAFailureIsNotAnEmptyResult(t *testing.T) {
	empty, errEmpty := decodeEnvelope("x", []byte(`{"result":[]}`))
	_, errFail := decodeEnvelope("x", []byte(`{"error":"upstream 503"}`))

	if errEmpty != nil {
		t.Fatalf("an explicit empty result was an error: %v", errEmpty)
	}
	if string(empty) != "[]" {
		t.Errorf("empty result = %s, want []", empty)
	}
	if errFail == nil {
		t.Fatal("a failure and an empty result are still the same answer")
	}
}

// Malformed is neither, and says so as itself rather than as a guest refusal.
func TestMalformedIsNotAGuestRefusal(t *testing.T) {
	_, err := decodeEnvelope("x", []byte(`not json`))
	if err == nil {
		t.Fatal("a malformed response was accepted")
	}
	if errors.Is(err, ErrPluginRefused) {
		t.Error("a broken plugin was reported as a plugin that refused; they " +
			"are different problems with different fixes")
	}
}

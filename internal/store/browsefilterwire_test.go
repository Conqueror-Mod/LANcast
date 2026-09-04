package store

import (
	"encoding/json"
	"testing"
)

/*
 * The bytes a client actually receives, not the struct the server holds.
 *
 * ResolutionBucket shipped without json tags. Facets is written straight to the
 * response, so the wire carried `Key`, `Label`, `MinWidth`, `MaxWidth` while
 * docs/api.md documented `key`, `label`, `min_width`, `max_width` — and the
 * client read the documented names. Every resolution chip rendered with an
 * undefined label, and pressing one sent `resolution=undefined`, which the
 * contract says is ignored rather than rejected. The filter was a silent no-op
 * for as long as it existed.
 *
 * Two suites had a chance and neither took it. browsefilter_test.go asserts
 * ordering through `b.MinWidth`, which is the Go field and says nothing about
 * JSON. browseFilters.test.ts builds its fixture in snake_case, which is what
 * the client believed rather than what the server sent. Each was right about
 * its own half.
 *
 * So this asserts the serialized form. A field name is part of the contract
 * (ADR 0018) and cannot be verified by reading either side alone.
 */
func TestResolutionBucketWireShape(t *testing.T) {
	b, err := json.Marshal(ResolutionBucket{
		Key: "uhd", Label: "4K", MinWidth: 3000, MaxWidth: 0,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	const want = `{"key":"uhd","label":"4K","min_width":3000,"max_width":0}`
	if string(b) != want {
		t.Errorf("resolution bucket serializes as\n  %s\nwant\n  %s\n\n"+
			"These names are the browse filter's contract: the client keys its "+
			"chips on `key` and labels them from `label`. Renaming one silently "+
			"disables the filter rather than failing.", b, want)
	}
}

// Facets carries the buckets, and it is Facets that is written to the response —
// so the nesting is part of the same claim.
func TestFacetsCarriesBucketsUnderSnakeCaseNames(t *testing.T) {
	b, err := json.Marshal(Facets{
		Resolutions: []ResolutionBucket{{Key: "sd", Label: "SD", MinWidth: 1, MaxWidth: 1099}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got struct {
		Resolutions []struct {
			Key      string `json:"key"`
			Label    string `json:"label"`
			MinWidth int    `json:"min_width"`
			MaxWidth int    `json:"max_width"`
		} `json:"resolutions"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Resolutions) != 1 {
		t.Fatalf("resolutions did not survive the round trip: %s", b)
	}
	if got.Resolutions[0].Key != "sd" || got.Resolutions[0].Label != "SD" ||
		got.Resolutions[0].MinWidth != 1 || got.Resolutions[0].MaxWidth != 1099 {
		t.Errorf("bucket fields did not round-trip under the documented names: %s", b)
	}
}

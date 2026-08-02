package plugin_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"

	"lancast/internal/meta/omdb"
	"lancast/internal/plugin"
)

func quietTestLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// The single OMDb payload both paths parse. Covers all three sources, the
// percentage and /100 scales, and grouped vote counts.
const omdbPayload = `{
	"Response": "True",
	"imdbVotes": "1,234,567",
	"Ratings": [
		{"Source": "Internet Movie Database", "Value": "7.9/10"},
		{"Source": "Rotten Tomatoes", "Value": "94%"},
		{"Source": "Metacritic", "Value": "81/100"}
	]
}`

// The acceptance criterion for the whole plugin runtime: the first-party OMDb
// plugin, run across the WASM boundary, produces ratings byte-identical to the
// native source on the same payload — ADR 0007's promise proven end to end.
func TestOMDbPluginMatchesNativeSource(t *testing.T) {
	ctx := context.Background()
	const imdbID = "tt2543164"

	// Native source, fed the payload via an httptest server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(omdbPayload))
	}))
	defer srv.Close()
	native := omdb.New("test-key", omdb.WithBaseURL(srv.URL), omdb.WithHTTPClient(srv.Client()))
	nativeRatings, err := native.Ratings(ctx, imdbID)
	if err != nil {
		t.Fatalf("native Ratings: %v", err)
	}
	if len(nativeRatings) != 3 {
		t.Fatalf("native produced %d ratings, want 3 — fix the test payload", len(nativeRatings))
	}

	// The plugin, fed the same payload via the host getter and the same key via
	// the host secret. Nothing touches the network.
	rt, err := plugin.NewRuntime(ctx, quietTestLog(),
		plugin.WithHTTPGetter(func(ctx context.Context, url string) ([]byte, error) {
			return []byte(omdbPayload), nil
		}),
		plugin.WithSecretResolver(func(name string) string {
			if name == "omdb_key" {
				return "test-key"
			}
			return ""
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(ctx)

	manifestBytes, err := os.ReadFile("../../plugins/omdb/plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	m, err := plugin.ParseManifest(manifestBytes)
	if err != nil {
		t.Fatalf("parse shipped manifest: %v", err)
	}
	wasm, err := os.ReadFile("../../plugins/omdb/plugin.wasm")
	if err != nil {
		t.Fatalf("read shipped plugin: %v — run plugins/omdb/build.sh", err)
	}
	p, err := rt.Load(ctx, m, wasm)
	if err != nil {
		t.Fatal(err)
	}
	rs, err := plugin.NewRatingSource(p)
	if err != nil {
		t.Fatal(err)
	}
	pluginRatings, err := rs.Ratings(ctx, imdbID)
	if err != nil {
		t.Fatalf("plugin Ratings: %v", err)
	}

	if !reflect.DeepEqual(nativeRatings, pluginRatings) {
		t.Errorf("plugin and native disagree:\n native = %+v\n plugin = %+v", nativeRatings, pluginRatings)
	}
}

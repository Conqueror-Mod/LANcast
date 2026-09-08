package plugin_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"lancast/internal/meta"
	"lancast/internal/meta/tmdb"
	"lancast/internal/plugin"
)

/*
 * The acceptance criterion for the provider shape.
 *
 * OMDb proved a plugin could be a rating source. It proved nothing about a
 * provider, because a rating source is the narrowest thing an extension point
 * can be: one id in, some scores out, no identity, no images. A provider
 * decides what a file *is*.
 *
 * So the same test, at the harder end: the first-party TMDB plugin, run across
 * the WASM boundary, produces candidates and records identical to the native
 * source on identical payloads. Everything that has ever been a bug in this
 * mapping is in the fixtures below — the numeric character names, the
 * untranslated collection block, the tv imdb id living somewhere else, a
 * season's own poster rather than its show's.
 */

// The payloads both paths parse, keyed by the path they answer.
var tmdbFixtures = map[string]string{
	"/search/movie": `{"results":[
		{"id":329865,"title":"Arrival","release_date":"2016-11-10","overview":"Linguists and physicists.","popularity":41.7,"poster_path":"/x.jpg"},
		{"id":1,"title":"Arrival, The","release_date":"","overview":"","popularity":0.4,"poster_path":""}
	]}`,
	"/movie/329865": `{
		"title":"Arrival","overview":"Linguists and physicists.",
		"release_date":"2016-11-10","runtime":116,"vote_average":7.6,
		"genres":[{"name":"Drama"},{"name":""},{"name":"Science Fiction"}],
		"poster_path":"/p.jpg","backdrop_path":"/b.jpg",
		"imdb_id":"tt2543164",
		"belongs_to_collection":{"id":448150,"name":"Ankunft Filmreihe","poster_path":"/cp.jpg","backdrop_path":""},
		"keywords":{"keywords":[{"id":180547,"name":"marvel cinematic universe"}]},
		"credits":{
			"cast":[
				{"name":"Amy Adams","character":"Louise Banks","profile_path":"/a.jpg","order":0},
				{"name":"Jeremy Renner","character":"81","profile_path":"","order":1}
			],
			"crew":[
				{"name":"Denis Villeneuve","job":"Director"},
				{"name":"Eric Heisserer","job":"Screenplay"},
				{"name":"Somebody Else","job":"Gaffer"}
			]
		}
	}`,
	// The translated name the embedded block did not carry.
	"/collection/448150": `{"id":448150,"name":"Arrival Collection"}`,
	"/search/tv": `{"results":[
		{"id":1396,"name":"Breaking Bad","first_air_date":"2008-01-20","overview":"A teacher.","popularity":99.9,"poster_path":"/bb.jpg"}
	]}`,
	"/tv/1396": `{
		"name":"Breaking Bad","overview":"A teacher.","first_air_date":"2008-01-20",
		"vote_average":8.9,"genres":[{"name":"Drama"}],
		"poster_path":"/bb.jpg","backdrop_path":"/bbb.jpg",
		"external_ids":{"imdb_id":"tt0903747"},
		"credits":{"cast":[{"name":"Bryan Cranston","character":"Walter White","profile_path":"/bc.jpg","order":0}],"crew":[]}
	}`,
	"/tv/1396/season/2":           `{"name":"Season 2","overview":"The second one.","air_date":"2009-03-08","vote_average":8.3,"poster_path":"/s2.jpg"}`,
	"/tv/1396/season/2/episode/4": `{"name":"Down","overview":"An episode.","air_date":"2009-03-29","vote_average":7.9,"still_path":"/e4.jpg"}`,
}

// payloadFor picks the fixture a request path asks for, longest match first so
// /tv/1396/season/2/episode/4 does not answer as /tv/1396.
func payloadFor(t *testing.T, path string) string {
	t.Helper()
	best := ""
	for p := range tmdbFixtures {
		if strings.HasPrefix(path, p) && len(p) > len(best) {
			best = p
		}
	}
	if best == "" {
		t.Fatalf("no TMDB fixture for %q — add one", path)
	}
	return tmdbFixtures[best]
}

func TestTMDBPluginMatchesNativeProvider(t *testing.T) {
	ctx := context.Background()

	// Native, fed the fixtures over httptest.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(payloadFor(t, r.URL.Path)))
	}))
	defer srv.Close()
	native := tmdb.New("test-key", tmdb.WithBaseURL(srv.URL), tmdb.WithHTTPClient(srv.Client()))

	// The plugin, fed the same fixtures through the host getter and the same
	// key through the host secret. Nothing touches the network.
	rt, err := plugin.NewRuntime(ctx, quietTestLog(),
		plugin.WithHTTPGetter(func(ctx context.Context, raw string) ([]byte, error) {
			path := raw
			if i := strings.Index(path, "/3/"); i >= 0 {
				path = path[i+2:]
			}
			if i := strings.IndexByte(path, '?'); i >= 0 {
				path = path[:i]
			}
			return []byte(payloadFor(t, path)), nil
		}),
		plugin.WithSecretResolver(func(name string) string {
			if name == "tmdb_key" {
				return "test-key"
			}
			return ""
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(ctx)

	manifestBytes, err := os.ReadFile("../../plugins/tmdb/plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	m, err := plugin.ParseManifest(manifestBytes)
	if err != nil {
		t.Fatalf("parse shipped manifest: %v", err)
	}
	wasm, err := os.ReadFile("../../plugins/tmdb/plugin.wasm")
	if err != nil {
		t.Fatalf("read shipped plugin: %v — run plugins/tmdb/build.sh", err)
	}
	p, err := rt.Load(ctx, m, wasm)
	if err != nil {
		t.Fatal(err)
	}
	pv, err := plugin.NewProvider(p)
	if err != nil {
		t.Fatal(err)
	}

	// The manifest's caps must say what the native provider says it can do,
	// or the plugin is a provider the enricher would never consult about half
	// its library.
	if pv.Caps() != native.Caps() {
		t.Errorf("caps = %+v, native = %+v", pv.Caps(), native.Caps())
	}
	if pv.ID() != native.ID() {
		t.Errorf("id = %q, native = %q", pv.ID(), native.ID())
	}

	searches := []struct {
		name string
		q    meta.Query
	}{
		{"movie", meta.Query{Kind: meta.KindMovie, Title: "Arrival", Year: 2016}},
		{"show", meta.Query{Kind: meta.KindShow, Title: "Breaking Bad"}},
		{"episode uses the series name", meta.Query{Kind: meta.KindEpisode, Title: "Down", Series: "Breaking Bad", Season: 2, Episode: 4}},
		{"a season is never searched", meta.Query{Kind: meta.KindSeason, Title: "Season 2"}},
		{"an empty title asks nothing", meta.Query{Kind: meta.KindMovie, Title: "   "}},
	}
	for _, tc := range searches {
		t.Run("search/"+tc.name, func(t *testing.T) {
			want, err := native.Search(ctx, tc.q)
			if err != nil {
				t.Fatalf("native Search: %v", err)
			}
			got, err := pv.Search(ctx, tc.q)
			if err != nil {
				t.Fatalf("plugin Search: %v", err)
			}
			if !reflect.DeepEqual(want, got) {
				t.Errorf("plugin and native disagree:\n native = %+v\n plugin = %+v", want, got)
			}
		})
	}

	fetches := []struct {
		name string
		ref  meta.Ref
	}{
		{"movie", meta.Ref{Kind: meta.KindMovie, ExternalID: "329865"}},
		{"show", meta.Ref{Kind: meta.KindShow, ExternalID: "1396"}},
		{"season", meta.Ref{Kind: meta.KindSeason, ExternalID: "1396", Season: 2}},
		{"episode", meta.Ref{Kind: meta.KindEpisode, ExternalID: "1396", Season: 2, Episode: 4}},
	}
	for _, tc := range fetches {
		t.Run("fetch/"+tc.name, func(t *testing.T) {
			want, err := native.Fetch(ctx, tc.ref)
			if err != nil {
				t.Fatalf("native Fetch: %v", err)
			}
			got, err := pv.Fetch(ctx, tc.ref)
			if err != nil {
				t.Fatalf("plugin Fetch: %v", err)
			}
			if !reflect.DeepEqual(want, got) {
				t.Errorf("plugin and native disagree:\n native = %s\n plugin = %s", showRecord(want), showRecord(got))
			}
		})
	}
}

// A payload TMDB serves and the mapping has been wrong about before: the
// collection name in the embedded block is not translated, so a movie fetch
// must go and ask the dedicated endpoint. Asserted on the merged result rather
// than by counting requests, because what matters is the name that reaches the
// library.
func TestTMDBPluginResolvesTheTranslatedCollectionName(t *testing.T) {
	ctx := context.Background()
	rt, p := loadTMDBPlugin(t)
	defer rt.Close(ctx)
	pv, err := plugin.NewProvider(p)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := pv.Fetch(ctx, meta.Ref{Kind: meta.KindMovie, ExternalID: "329865"})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Collection == nil || rec.Collection.Name != "Arrival Collection" {
		t.Errorf("collection = %+v, want the translated name", rec.Collection)
	}
}

/*
 * A fetch the host refused or could not make is an error, not an empty record.
 *
 * This is the whole reason the provider forced ABI 2. If it ever regresses,
 * a TMDB outage stops being a delay and becomes the enricher writing "there is
 * no such film" into somebody's library, permanently, for everything it tried
 * to match while the API was down.
 */
func TestTMDBPluginReportsAFailedFetchAsAFailure(t *testing.T) {
	ctx := context.Background()
	rt, err := plugin.NewRuntime(ctx, quietTestLog(),
		plugin.WithHTTPGetter(func(ctx context.Context, raw string) ([]byte, error) {
			return nil, nil // the host refused it, or it failed
		}),
		plugin.WithSecretResolver(func(string) string { return "test-key" }),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(ctx)
	pv, err := plugin.NewProvider(loadTMDBInto(t, ctx, rt))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := pv.Search(ctx, meta.Query{Kind: meta.KindMovie, Title: "Arrival"}); err == nil {
		t.Error("a failed search reported success")
	}
	if _, err := pv.Fetch(ctx, meta.Ref{Kind: meta.KindMovie, ExternalID: "329865"}); err == nil {
		t.Error("a failed fetch reported success")
	}
}

// With no key configured the plugin says so rather than reporting an empty
// library. A server with no TMDB key is a working server; it is just one the
// enricher has nothing to ask.
func TestTMDBPluginWithoutAKey(t *testing.T) {
	ctx := context.Background()
	rt, err := plugin.NewRuntime(ctx, quietTestLog(),
		plugin.WithHTTPGetter(func(ctx context.Context, raw string) ([]byte, error) {
			t.Error("fetched with no key configured")
			return nil, nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close(ctx)
	pv, err := plugin.NewProvider(loadTMDBInto(t, ctx, rt))
	if err != nil {
		t.Fatal(err)
	}
	_, err = pv.Search(ctx, meta.Query{Kind: meta.KindMovie, Title: "Arrival"})
	if err == nil || !strings.Contains(err.Error(), "no API key") {
		t.Errorf("error = %v, want it to name the missing key", err)
	}
}

func loadTMDBPlugin(t *testing.T) (*plugin.Runtime, *plugin.Plugin) {
	t.Helper()
	ctx := context.Background()
	rt, err := plugin.NewRuntime(ctx, quietTestLog(),
		plugin.WithHTTPGetter(func(ctx context.Context, raw string) ([]byte, error) {
			path := raw
			if i := strings.Index(path, "/3/"); i >= 0 {
				path = path[i+2:]
			}
			if i := strings.IndexByte(path, '?'); i >= 0 {
				path = path[:i]
			}
			return []byte(payloadFor(t, path)), nil
		}),
		plugin.WithSecretResolver(func(name string) string { return "test-key" }),
	)
	if err != nil {
		t.Fatal(err)
	}
	return rt, loadTMDBInto(t, ctx, rt)
}

func loadTMDBInto(t *testing.T, ctx context.Context, rt *plugin.Runtime) *plugin.Plugin {
	t.Helper()
	manifestBytes, err := os.ReadFile("../../plugins/tmdb/plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	m, err := plugin.ParseManifest(manifestBytes)
	if err != nil {
		t.Fatal(err)
	}
	wasm, err := os.ReadFile("../../plugins/tmdb/plugin.wasm")
	if err != nil {
		t.Fatalf("read shipped plugin: %v — run plugins/tmdb/build.sh", err)
	}
	p, err := rt.Load(ctx, m, wasm)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// showRecord renders a record with its pointer fields dereferenced, so a
// failure names the field that differs instead of printing addresses.
func showRecord(r *meta.Record) string {
	if r == nil {
		return "<nil>"
	}
	var b strings.Builder
	b.WriteString(r.Source + "/" + r.ExternalID + " kind=" + string(r.Kind) + " imdb=" + r.IMDbID)
	f := r.Fields
	writeStr := func(name string, v *string) {
		if v != nil {
			b.WriteString(" " + name + "=" + *v)
		}
	}
	writeStr("title", f.Title)
	writeStr("overview", f.Overview)
	writeStr("series", f.Series)
	if f.Year != nil {
		b.WriteString(" year=" + itoa(*f.Year))
	}
	if f.Season != nil {
		b.WriteString(" season=" + itoa(*f.Season))
	}
	if f.Episode != nil {
		b.WriteString(" episode=" + itoa(*f.Episode))
	}
	if f.Rating != nil {
		b.WriteString(" rating=" + ftoa(*f.Rating))
	}
	if f.DurationMS != nil {
		b.WriteString(" duration=" + i64toa(*f.DurationMS))
	}
	if f.ReleasedAt != nil {
		b.WriteString(" released=" + i64toa(*f.ReleasedAt))
	}
	b.WriteString(" genres=" + strings.Join(r.Genres, ","))
	for _, c := range r.Credits {
		b.WriteString(" credit[" + c.Role + ":" + c.Name + ":" + c.Character + ":" + c.Image + "]")
	}
	for _, a := range r.Artwork {
		b.WriteString(" art[" + string(a.Kind) + ":" + a.URL + "]")
	}
	if r.Collection != nil {
		b.WriteString(" collection[" + r.Collection.ExternalID + ":" + r.Collection.Name + "]")
	}
	for _, k := range r.Keywords {
		b.WriteString(" kw[" + itoa(k.ID) + ":" + k.Name + "]")
	}
	return b.String()
}

func itoa(v int) string     { return strconv.Itoa(v) }
func i64toa(v int64) string { return strconv.FormatInt(v, 10) }
func ftoa(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

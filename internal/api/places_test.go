package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"lancast/internal/photo"
	"lancast/internal/store"
)

// placesFixture is a picture library holding two photos filed under a place,
// one with a position and no town, and one that carries nothing.
type placesFixture struct {
	lib   int64
	place int64
}

func newPlacesFixture(t *testing.T, h *harness) placesFixture {
	t.Helper()
	ctx := context.Background()
	lib, err := h.st.CreateLibrary(ctx, "Photographs", "picture", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	place := &store.PhotoPlace{ID: 9000001, Name: "Alphaville", Region: "Somewhere", CountryCode: "ZZ", Country: "Testland"}
	at := &store.LatLon{Lat: 10.5, Lon: -20.25}
	for i, p := range []*store.PhotoPlace{place, place, nil, nil} {
		name := "p" + strconv.Itoa(i) + ".jpg"
		id, err := h.st.UpsertItem(ctx, store.ScanFile{LibraryID: lib.ID, Path: lib.Path + "/" + name,
			Kind: "photo", Title: name, SortTitle: name, Container: "jpg", SizeBytes: 1, MTime: 1})
		if err != nil {
			t.Fatal(err)
		}
		pos := at
		if i == 3 {
			pos = nil
		}
		if err := h.st.RecordPhotoLocation(ctx, id, pos, p); err != nil {
			t.Fatal(err)
		}
	}
	return placesFixture{lib: lib.ID, place: place.ID}
}

type placesBody struct {
	Enabled bool `json:"enabled"`
	Reading bool `json:"reading"`
	Places  []struct {
		ID      int64  `json:"id"`
		Name    string `json:"name"`
		Region  string `json:"region"`
		Country string `json:"country"`
		Count   int    `json:"count"`
	} `json:"places"`
	Elsewhere int `json:"elsewhere"`
	Unlocated int `json:"unlocated"`
	Unread    int `json:"unread"`
}

func TestPlacesListANameAndACountAndNeverACoordinate(t *testing.T) {
	h := newHarness(t)
	fx := newPlacesFixture(t, h)

	res := h.do(t, "GET", "/api/libraries/"+strconv.FormatInt(fx.lib, 10)+"/places", nil)
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, raw)
	}
	// Asserted on the bytes, not the struct: a coordinate the struct below
	// does not declare would decode away silently and pass.
	for _, leak := range []string{"lat", "lon", "10.5", "-20.25"} {
		if strings.Contains(string(raw), leak) {
			t.Errorf("the places response contains %q: %s", leak, raw)
		}
	}
	var got placesBody
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Enabled {
		t.Error("enabled before anybody turned it on")
	}
	if len(got.Places) != 1 || got.Places[0].Name != "Alphaville" || got.Places[0].Count != 2 {
		t.Errorf("places = %+v", got.Places)
	}
	if got.Elsewhere != 1 || got.Unlocated != 1 || got.Unread != 0 {
		t.Errorf("elsewhere %d unlocated %d unread %d", got.Elsewhere, got.Unlocated, got.Unread)
	}
}

func TestAPlaceOpensOntoItsCount(t *testing.T) {
	h := newHarness(t)
	fx := newPlacesFixture(t, h)
	base := "/api/libraries/" + strconv.FormatInt(fx.lib, 10) + "/places/"

	var page struct {
		Total int          `json:"total"`
		Items []store.Item `json:"items"`
	}
	decode(t, h.do(t, "GET", base+strconv.FormatInt(fx.place, 10), nil), &page)
	if page.Total != 2 || len(page.Items) != 2 {
		t.Errorf("Alphaville opened onto %d (%d items), want 2", page.Total, len(page.Items))
	}
	decode(t, h.do(t, "GET", base+"elsewhere", nil), &page)
	if page.Total != 1 || len(page.Items) != 1 {
		t.Errorf("elsewhere opened onto %d (%d items), want 1", page.Total, len(page.Items))
	}
	for _, bad := range []string{"0", "-3", "paris"} {
		res := h.do(t, "GET", base+bad, nil)
		res.Body.Close()
		if res.StatusCode != http.StatusBadRequest {
			t.Errorf("place %q: status %d, want 400", bad, res.StatusCode)
		}
	}
}

// Places of a film library would be a question about filming locations.
func TestPlacesAreAPictureLibraryView(t *testing.T) {
	h := newHarness(t)
	res := h.do(t, "GET", "/api/libraries/"+strconv.FormatInt(h.lib.ID, 10)+"/places", nil)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("a film library's places: status %d, want 400 wrong_kind", res.StatusCode)
	}
}

/*
 * Off deletes. The setting is the only way a coordinate gets into the
 * database, and turning it off has to take every one out again — otherwise
 * "off" means "hidden", and hidden data is still in every backup.
 */
func TestTurningPlacesOffForgetsEveryLocation(t *testing.T) {
	h := newHarness(t)
	fx := newPlacesFixture(t, h)
	h.srvAPI.locations = photo.NewLocationWorker(h.st,
		func() bool { return h.settings.Get().PhotoPlaces }, slog.New(slog.NewTextHandler(io.Discard, nil)))
	kicked := 0
	h.srvAPI.placesSoon = func() { kicked++ }

	var settings map[string]any
	decode(t, h.do(t, "PUT", "/api/settings", map[string]any{"photo_places": true}), &settings)
	if settings["photo_places"] != true || kicked != 1 {
		t.Fatalf("turning it on: setting %v, pass kicked %d times", settings["photo_places"], kicked)
	}
	decode(t, h.do(t, "PUT", "/api/settings", map[string]any{"photo_places": false}), &settings)
	if settings["photo_places"] != false {
		t.Fatalf("turning it off: setting %v", settings["photo_places"])
	}

	var got placesBody
	decode(t, h.do(t, "GET", "/api/libraries/"+strconv.FormatInt(fx.lib, 10)+"/places", nil), &got)
	if len(got.Places) != 0 || got.Elsewhere != 0 || got.Unlocated != 0 || got.Unread != 4 {
		t.Errorf("after turning it off: %+v, want every photo unread again", got)
	}
}

package retrodb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// The shape of thumbnails.libretro.com's directory index, as read 2026-10-08:
// an nginx-style listing of percent-escaped hrefs.
const sampleListing = `<html><head><title>Index of /Sega - Mega Drive - Genesis/Named_Boxarts/</title></head><body>
<h1>Index of /Sega - Mega Drive - Genesis/Named_Boxarts/</h1><hr><pre><a href="../">../</a>
<a href="Road%20Rash%20II%20%28USA%2C%20Europe%29%20%28RR205%29.png">Road Rash II (USA, Europe) (RR205).png</a>   08-Mar-2024 10:12  512344
<a href="Road%20Rash%20II%20%28Japan%29.png">Road Rash II (Japan).png</a>   08-Mar-2024 10:12  498120
<a href="Sonic%20_%20Knuckles%20%28World%29.png">Sonic _ Knuckles (World).png</a>  08-Mar-2024 10:12  1
<a href="?C=N;O=D">Name</a>
<a href="subdir/">subdir/</a>
</pre><hr></body></html>`

func TestParseListing(t *testing.T) {
	got := ParseListing(sampleListing)
	want := []string{
		"Road Rash II (USA, Europe) (RR205)",
		"Road Rash II (Japan)",
		"Sonic _ Knuckles (World)",
	}
	if len(got) != len(want) {
		t.Fatalf("got %q", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	// A listed name round-trips to the URL it came from: names in a listing
	// have already been through the set's unsafe-character rule.
	u := ThumbnailURL("genesis", Boxart, got[2])
	if u != "https://thumbnails.libretro.com/Sega%20-%20Mega%20Drive%20-%20Genesis/Named_Boxarts/Sonic%20_%20Knuckles%20%28World%29.png" {
		t.Errorf("round trip = %s", u)
	}
}

// A listing is fetched once per process; a failure is asked again.
func TestListingsRememberSuccessNotFailure(t *testing.T) {
	var hits, fail atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if fail.Load() == 1 {
			http.Error(w, "busy", http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(sampleListing))
	}))
	defer srv.Close()
	l := &Listings{Client: srv.Client()}
	ctx := context.Background()

	fail.Store(1)
	if _, err := l.Get(ctx, srv.URL+"/a/"); err == nil {
		t.Fatal("a 503 was not an error")
	}
	fail.Store(0)
	for i := 0; i < 3; i++ {
		names, err := l.Get(ctx, srv.URL+"/a/")
		if err != nil || len(names) != 3 {
			t.Fatalf("got %v, %v", names, err)
		}
	}
	if hits.Load() != 2 {
		t.Errorf("server asked %d times, want the failure and then one success", hits.Load())
	}
}

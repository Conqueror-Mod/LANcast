//go:build mbmeasure

package musicbrainz

/*
 * The measurement ADR 0115 requires before any screen is built: run the
 * matcher over a sample of a real library and report how it splits.
 *
 * Behind a build tag so it never runs in CI and never ships: it talks to the
 * real MusicBrainz, at its rate, for minutes. Point it at a COPY of a
 * database (copy lancast.db with its -wal and -shm):
 *
 *	MB_MEASURE_DB=<copy>/lancast.db MB_MEASURE_LIBRARY=19 MB_MEASURE_N=150 \
 *	  go test -tags mbmeasure ./internal/meta/musicbrainz/ -run TestMeasure -v -timeout 30m
 */

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"testing"

	"lancast/internal/meta"
	"lancast/internal/store"
)

func TestMeasure(t *testing.T) {
	path := os.Getenv("MB_MEASURE_DB")
	if path == "" {
		t.Skip("MB_MEASURE_DB not set")
	}
	lib, _ := strconv.ParseInt(os.Getenv("MB_MEASURE_LIBRARY"), 10, 64)
	n, _ := strconv.Atoi(os.Getenv("MB_MEASURE_N"))
	if n <= 0 {
		n = 100
	}
	ctx := context.Background()
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// Every album, a page at a time: a listing is capped, and sampling one
	// page samples one stretch of the alphabet.
	var albums []store.Item
	for off := 0; ; {
		page, _, err := st.ListItems(ctx, store.ItemFilter{LibraryID: lib, Kind: "album", Limit: 500, Offset: off})
		if err != nil {
			t.Fatal(err)
		}
		albums = append(albums, page...)
		off += len(page)
		if len(page) == 0 {
			break
		}
	}
	t.Logf("%d albums in the library", len(albums))
	rand.New(rand.NewSource(3)).Shuffle(len(albums), func(i, j int) { albums[i], albums[j] = albums[j], albums[i] })
	if len(albums) > n {
		albums = albums[:n]
	}

	c := New("measure")
	counts := map[string]int{}
	type row struct {
		state, album, artist, best, bestArtist string
		year, bestYear                         int
		score                                  float64
	}
	var rows []row
	for i, al := range albums {
		artist := ""
		if al.ParentID != nil {
			if p, err := st.GetItem(ctx, *al.ParentID, ""); err == nil && p.Kind == "artist" {
				artist = p.Title
			}
		}
		year := 0
		if al.Year != nil {
			year = *al.Year
		}
		q := meta.AlbumQuery(al.Title, artist, year)
		cands, err := c.Search(ctx, q)
		if errors.Is(err, ErrBusy) {
			counts["busy"]++
			continue
		}
		if err != nil {
			t.Fatalf("album %d: %v", al.ID, err)
		}
		meta.Rank(q, cands)
		best, state := meta.Decide(cands)
		counts[state]++
		rows = append(rows, row{state, al.Title, artist, best.Title, best.Artist, year, best.Year, best.Score})
		if (i+1)%25 == 0 {
			t.Logf("%d/%d %v", i+1, len(albums), counts)
		}
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].state != rows[j].state {
			return rows[i].state < rows[j].state
		}
		return rows[i].score > rows[j].score
	})
	for _, r := range rows {
		if r.state == meta.StateMatched && r.score >= 0.95 {
			continue
		}
		fmt.Printf("%-9s %.2f  %q / %q (%d)  ->  %q / %q (%d)\n",
			r.state, r.score, r.album, r.artist, r.year, r.best, r.bestArtist, r.bestYear)
	}
	t.Logf("FINAL %d albums: %v", len(albums), counts)
}

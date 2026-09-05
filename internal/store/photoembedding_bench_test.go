package store

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"testing"
)

/*
 * How long a search actually takes, at a library size worth worrying about.
 *
 * ADR 0060 chose brute force over a vector index and said the arithmetic is
 * cheap while the *read* may not be — and then said to measure before believing
 * either half. This is that measurement, and it exists because the alternative
 * is a cache built on an intuition: 40,000 photographs is about 82MB of vectors,
 * which sounds like a lot right up until you divide it by what a disk does.
 *
 * Run it with:
 *
 *	go test ./internal/store/ -run xxx -bench SearchPhotos -benchtime 10x
 *
 * It is a benchmark rather than a test on purpose. There is no threshold here
 * that could fail honestly — a number that is fine on this machine and awful on
 * a Raspberry Pi is not a regression, and a test asserting milliseconds would
 * fail on whichever machine happened to be busy.
 */
func benchmarkSearch(b *testing.B, n int) {
	b.Helper()
	st := openTestStore(b)
	ctx := context.Background()

	lib, err := st.CreateLibrary(ctx, "Photographs", "picture", b.TempDir())
	if err != nil {
		b.Fatal(err)
	}

	// A fixed seed, so two runs of this benchmark are comparable. The vectors
	// are noise, which is the right shape: cosine over random unit vectors
	// costs exactly what cosine over real ones costs, and the read is the same
	// bytes either way.
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < n; i++ {
		id, err := st.UpsertItem(ctx, ScanFile{
			LibraryID: lib.ID, Path: fmt.Sprintf("p%06d.jpg", i), Kind: "photo",
			Title: "p", SortTitle: "p", Container: "jpg", SizeBytes: 1, MTime: 1,
		})
		if err != nil {
			b.Fatal(err)
		}
		if err := st.SavePhotoEmbedding(ctx, id, "bench", unitVector(rng, 512)); err != nil {
			b.Fatal(err)
		}
	}

	query := unitVector(rng, 512)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hits, err := st.SearchPhotosByVector(ctx, lib.ID, "bench", query, 60, 0)
		if err != nil {
			b.Fatal(err)
		}
		if len(hits) != 60 {
			b.Fatalf("got %d hits, want 60", len(hits))
		}
	}
}

func unitVector(rng *rand.Rand, dims int) []float32 {
	v := make([]float32, dims)
	var sum float64
	for i := range v {
		v[i] = float32(rng.NormFloat64())
		sum += float64(v[i]) * float64(v[i])
	}
	norm := float32(math.Sqrt(sum))
	for i := range v {
		v[i] /= norm
	}
	return v
}

func BenchmarkSearchPhotos1k(b *testing.B)  { benchmarkSearch(b, 1_000) }
func BenchmarkSearchPhotos10k(b *testing.B) { benchmarkSearch(b, 10_000) }
func BenchmarkSearchPhotos40k(b *testing.B) { benchmarkSearch(b, 40_000) }

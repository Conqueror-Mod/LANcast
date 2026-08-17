package enrich

import (
	"context"
	"testing"

	"lancast/internal/meta"
)

/*
 * The activity panel read **"682 of 449"**.
 *
 * Total was sized once when the run began and never revised, so anything that
 * joined the queue mid-run marched progress past it. That is not an edge case:
 * a scan adds rows while enrichment is already going, `refresh` clears the stamp
 * on a whole library, and `reparse` requeues everything it corrected.
 */
func TestGrowTotalNeverTrailsProgress(t *testing.T) {
	// The reported bug, in numbers: sized at 449, then 682 enriched with more
	// still queued.
	if got := growTotal(449, 682, 120); got < 682 {
		t.Errorf("total = %d with 682 done — progress is past its own total", got)
	}
	if got := growTotal(449, 682, 120); got != 802 {
		t.Errorf("total = %d, want 802 (done plus outstanding)", got)
	}
}

// Monotonic: a bar that jumps backwards reads as a fault in the thing it is
// measuring, so a shrinking queue must not shrink the total.
func TestGrowTotalNeverGoesBackwards(t *testing.T) {
	total := growTotal(0, 0, 500) // sized at the start
	if total != 500 {
		t.Fatalf("initial total = %d, want 500", total)
	}
	for _, done := range []int{100, 200, 300, 400, 500} {
		next := growTotal(total, done, 500-done)
		if next < total {
			t.Errorf("total shrank from %d to %d at %d done", total, next, done)
		}
		total = next
	}
	if total != 500 {
		t.Errorf("total = %d after a clean run of 500, want it unchanged", total)
	}
}

/*
 * A failed item stays pending and is retried, so it is already inside
 * `remaining`. Adding failures on top would inflate the total by every transient
 * provider error — a library with a flaky key would grow a total that never
 * stops climbing.
 */
func TestGrowTotalDoesNotCountFailuresTwice(t *testing.T) {
	// 10 done, 5 still queued (3 of which failed once and will be retried).
	if got := growTotal(15, 10, 5); got != 15 {
		t.Errorf("total = %d, want 15 — failures are inside remaining, not added to it", got)
	}
}

// The ordinary case: nothing joined the queue, so the estimate stands.
func TestGrowTotalLeavesAGoodEstimateAlone(t *testing.T) {
	if got := growTotal(1000, 250, 750); got != 1000 {
		t.Errorf("total = %d, want 1000 left alone", got)
	}
}

/*
 * The invariant, at the worker rather than at the helper.
 *
 * growTotal passes its own tests whether or not anything calls it, which is the
 * failure this project has now met twice — a rule written and not wired. So this
 * runs a real pass and asserts the reported numbers agree with each other.
 */
func TestReportedTotalNeverTrailsEnriched(t *testing.T) {
	ctx := context.Background()
	st, lib := harness(t)
	for i, name := range []string{"a", "b", "c", "d", "e"} {
		addItem(t, st, lib, `C:\m\`+name+`.mkv`, "Film "+name, 2000+i)
	}

	p := &fakeProvider{
		id: "fake",
		cands: []meta.Candidate{{
			Provider: "fake", ExternalID: "1", Kind: meta.KindMovie,
			Title: "Film a", Year: 2000, Popularity: 40,
		}},
		record: arrivalRecord(),
	}
	reg := meta.NewRegistry()
	reg.AddProvider(p)

	w := New(st, reg, &fakeArt{}, quietLog())
	if err := w.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}

	s := w.Stats()
	if s.Total < s.Enriched {
		t.Errorf("Stats reported %d of %d — progress past its own total",
			s.Enriched, s.Total)
	}
	if s.Enriched == 0 {
		t.Fatal("nothing was enriched; the assertion above would pass vacuously")
	}
}

package enrich

import (
	"context"
	"fmt"
	"testing"

	"lancast/internal/meta"
	"lancast/internal/store"
)

/*
 * T-3: Halloween II (2009) held duration_ms 960000, a 16-minute short's TMDB
 * runtime, against a 7,145 s file. Enrichment read the row before the probe
 * had saved, filled the empty duration from the provider, and wrote it over
 * what the probe had measured by then. Only the probe sets a duration.
 */
func TestEnrichmentNeverWritesDuration(t *testing.T) {
	ctx := context.Background()
	st, lib := harness(t)
	id := addItem(t, st, lib, `C:\films\Halloween 2 (2009).mkv`, "Halloween 2", 2009)

	p := &fakeProvider{id: "fake", record: &meta.Record{
		Source: "fake", ExternalID: "1213455", Kind: meta.KindMovie,
		Fields: meta.Fields{Title: meta.S("Halloween Horror 2"), DurationMS: meta.I64(960_000)},
	}}
	reg := meta.NewRegistry()
	reg.AddProvider(p)
	w := New(st, reg, &fakeArt{}, quietLog())

	// The snapshot enrichment works from, taken before the probe finished.
	stale, err := st.GetItem(ctx, id, "local")
	if err != nil {
		t.Fatal(err)
	}
	if stale.DurationMS != nil {
		t.Fatalf("fixture already has a duration: %d", *stale.DurationMS)
	}
	// The probe lands in between.
	if err := st.SaveProbe(ctx, id, store.ProbeResult{DurationMS: 7_145_000, Container: "mkv"}); err != nil {
		t.Fatal(err)
	}

	if err := w.ApplyMatch(ctx, *stale, "fake", "1213455", meta.KindMovie); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetItem(ctx, id, "local")
	if got.DurationMS == nil || *got.DurationMS != 7_145_000 {
		t.Errorf("duration_ms = %s, want the probe's 7145000", ms(got.DurationMS))
	}

	// And with no probe at all, a provider runtime does not become the duration.
	id2 := addItem(t, st, lib, `C:\films\Unprobed.mkv`, "Unprobed", 2009)
	it2, _ := st.GetItem(ctx, id2, "local")
	if err := w.ApplyMatch(ctx, *it2, "fake", "1213455", meta.KindMovie); err != nil {
		t.Fatal(err)
	}
	got2, _ := st.GetItem(ctx, id2, "local")
	if got2.DurationMS != nil {
		t.Errorf("duration_ms = %d on an unprobed file, want none", *got2.DurationMS)
	}
}

func ms(p *int64) string {
	if p == nil {
		return "none"
	}
	return fmt.Sprint(*p)
}

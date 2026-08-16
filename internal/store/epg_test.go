package store

import (
	"context"
	"testing"
	"time"
)

func seedChannels(t *testing.T, st *Store, sourceName string, tvgIDs ...string) (int64, []int64) {
	t.Helper()
	ctx := context.Background()
	src, err := st.CreateChannelSource(ctx, sourceName, "http://example.invalid/list.m3u", "http://example.invalid/guide.xml")
	if err != nil {
		t.Fatal(err)
	}
	chans := make([]Channel, 0, len(tvgIDs))
	for _, id := range tvgIDs {
		c := Channel{Name: "Channel " + id, URL: "http://example.invalid/s" + id}
		if id != "" {
			v := id
			c.TvgID = &v
		}
		chans = append(chans, c)
	}
	if err := st.ReplaceChannels(ctx, src.ID, chans); err != nil {
		t.Fatal(err)
	}
	stored, err := st.ListChannels(ctx, src.ID)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]int64, len(stored))
	for i, c := range stored {
		ids[i] = c.ID
	}
	return src.ID, ids
}

func TestChannelKeepsTvgID(t *testing.T) {
	st := openTestStore(t)
	_, ids := seedChannels(t, st, "Provider", "bbcone.uk", "")

	got, err := st.GetChannel(context.Background(), ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if got.TvgID == nil || *got.TvgID != "bbcone.uk" {
		t.Errorf("tvg_id = %v", got.TvgID)
	}
	untagged, err := st.GetChannel(context.Background(), ids[1])
	if err != nil {
		t.Fatal(err)
	}
	if untagged.TvgID != nil {
		t.Errorf("a channel with no tvg-id got %q", *untagged.TvgID)
	}
}

// The join is case-insensitive because providers publish bbcone.uk in the
// playlist and BBCOne.uk in the guide, and a case-sensitive match loses whole
// lineups to a shift key.
func TestChannelTvgIDsAreLowercased(t *testing.T) {
	st := openTestStore(t)
	srcID, ids := seedChannels(t, st, "Provider", "BBCOne.uk", "")

	m, err := st.ChannelTvgIDs(context.Background(), srcID)
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 1 {
		t.Fatalf("got %d entries, want 1 — the untagged channel must be absent", len(m))
	}
	if m["bbcone.uk"] != ids[0] {
		t.Errorf("lookup by lowercase id failed: %v", m)
	}
}

func TestNowNextReturnsCurrentAndFollowing(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	srcID, ids := seedChannels(t, st, "Provider", "a", "b")

	now := time.Now()
	progs := []Program{
		// Finished: must not be reported as on now.
		{ChannelID: ids[0], StartAt: now.Add(-3 * time.Hour).Unix(), StopAt: now.Add(-2 * time.Hour).Unix(), Title: "Earlier"},
		{ChannelID: ids[0], StartAt: now.Add(-30 * time.Minute).Unix(), StopAt: now.Add(30 * time.Minute).Unix(), Title: "On now"},
		{ChannelID: ids[0], StartAt: now.Add(30 * time.Minute).Unix(), StopAt: now.Add(90 * time.Minute).Unix(), Title: "Next"},
		{ChannelID: ids[0], StartAt: now.Add(90 * time.Minute).Unix(), StopAt: now.Add(150 * time.Minute).Unix(), Title: "After that"},
		{ChannelID: ids[1], StartAt: now.Add(-10 * time.Minute).Unix(), StopAt: now.Add(50 * time.Minute).Unix(), Title: "Other channel"},
	}
	if _, err := st.ReplaceProgramsForSource(ctx, srcID, progs); err != nil {
		t.Fatal(err)
	}

	got, err := st.NowNext(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	first := got[ids[0]]
	if len(first) != 2 {
		t.Fatalf("got %d entries for channel 1, want exactly now and next", len(first))
	}
	if first[0].Title != "On now" || first[1].Title != "Next" {
		t.Errorf("got %q then %q", first[0].Title, first[1].Title)
	}
	if len(got[ids[1]]) != 1 || got[ids[1]][0].Title != "Other channel" {
		t.Errorf("second channel: %+v", got[ids[1]])
	}
}

// A channel whose listings ran out is absent rather than present-and-empty, so
// a client can tell "no guide" from "nothing on".
func TestNowNextOmitsChannelsWithNothingOn(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	srcID, ids := seedChannels(t, st, "Provider", "a")

	now := time.Now()
	if _, err := st.ReplaceProgramsForSource(ctx, srcID, []Program{
		{ChannelID: ids[0], StartAt: now.Add(-3 * time.Hour).Unix(), StopAt: now.Add(-2 * time.Hour).Unix(), Title: "Over"},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := st.NowNext(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, present := got[ids[0]]; present {
		t.Errorf("a channel with only finished listings appeared: %+v", got[ids[0]])
	}
}

// The programme that started before the window opened is the one being watched.
// A schedule that omits it begins with a hole.
func TestChannelScheduleIncludesTheOverlappingProgramme(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	srcID, ids := seedChannels(t, st, "Provider", "a")

	now := time.Now()
	if _, err := st.ReplaceProgramsForSource(ctx, srcID, []Program{
		{ChannelID: ids[0], StartAt: now.Add(-40 * time.Minute).Unix(), StopAt: now.Add(20 * time.Minute).Unix(), Title: "Straddles"},
		{ChannelID: ids[0], StartAt: now.Add(20 * time.Minute).Unix(), StopAt: now.Add(80 * time.Minute).Unix(), Title: "Inside"},
		{ChannelID: ids[0], StartAt: now.Add(300 * time.Minute).Unix(), StopAt: now.Add(360 * time.Minute).Unix(), Title: "Beyond"},
	}); err != nil {
		t.Fatal(err)
	}

	got, err := st.ChannelSchedule(ctx, ids[0], now, now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Title != "Straddles" || got[1].Title != "Inside" {
		t.Fatalf("got %+v", got)
	}
}

// Replacing one source's guide must not blank another's — two providers can
// each publish one.
func TestReplaceProgramsIsScopedToItsSource(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	srcA, idsA := seedChannels(t, st, "A", "a")
	srcB, idsB := seedChannels(t, st, "B", "b")

	now := time.Now()
	mk := func(id int64, title string) []Program {
		return []Program{{ChannelID: id, StartAt: now.Unix(), StopAt: now.Add(time.Hour).Unix(), Title: title}}
	}
	if _, err := st.ReplaceProgramsForSource(ctx, srcA, mk(idsA[0], "From A")); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ReplaceProgramsForSource(ctx, srcB, mk(idsB[0], "From B")); err != nil {
		t.Fatal(err)
	}
	// Refreshing A again must leave B alone.
	if _, err := st.ReplaceProgramsForSource(ctx, srcA, mk(idsA[0], "From A again")); err != nil {
		t.Fatal(err)
	}

	got, err := st.NowNext(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(got[idsB[0]]) != 1 || got[idsB[0]][0].Title != "From B" {
		t.Errorf("B's guide was disturbed by A's refresh: %+v", got[idsB[0]])
	}
	if len(got[idsA[0]]) != 1 || got[idsA[0]][0].Title != "From A again" {
		t.Errorf("A's guide did not replace: %+v", got[idsA[0]])
	}
}

/*
 * Replacing channels discards their listings, by cascade.
 *
 * This is the ordering constraint the whole feature rests on: a refresh must
 * import the channel list first and the guide second. Doing it the other way
 * round produces an empty guide and no error anywhere, so it is asserted rather
 * than trusted to the comment that says so.
 */
func TestReplacingChannelsCascadesTheGuideAway(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	srcID, ids := seedChannels(t, st, "Provider", "a")

	now := time.Now()
	if _, err := st.ReplaceProgramsForSource(ctx, srcID, []Program{
		{ChannelID: ids[0], StartAt: now.Unix(), StopAt: now.Add(time.Hour).Unix(), Title: "On now"},
	}); err != nil {
		t.Fatal(err)
	}

	// A channel-list refresh: same channels, new rows.
	tvg := "a"
	if err := st.ReplaceChannels(ctx, srcID, []Channel{
		{Name: "Channel a", URL: "http://example.invalid/sa", TvgID: &tvg},
	}); err != nil {
		t.Fatal(err)
	}

	got, err := st.NowNext(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("listings survived a channel replace attached to dead ids: %+v", got)
	}
}

func TestPruneExpiredPrograms(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	srcID, ids := seedChannels(t, st, "Provider", "a")

	now := time.Now()
	if _, err := st.ReplaceProgramsForSource(ctx, srcID, []Program{
		{ChannelID: ids[0], StartAt: now.Add(-72 * time.Hour).Unix(), StopAt: now.Add(-71 * time.Hour).Unix(), Title: "Last week"},
		{ChannelID: ids[0], StartAt: now.Unix(), StopAt: now.Add(time.Hour).Unix(), Title: "On now"},
	}); err != nil {
		t.Fatal(err)
	}

	n, err := st.PruneExpiredPrograms(ctx, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("pruned %d, want 1", n)
	}
	got, err := st.NowNext(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(got[ids[0]]) != 1 || got[ids[0]][0].Title != "On now" {
		t.Errorf("the prune took what is on now: %+v", got[ids[0]])
	}
}

// program_count and epg_refreshed_at are what the Settings pane reports, and a
// source that imported a guide but says "never" reads as a failed import.
func TestReplaceProgramsRecordsTheRefresh(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	srcID, ids := seedChannels(t, st, "Provider", "a")

	now := time.Now()
	if _, err := st.ReplaceProgramsForSource(ctx, srcID, []Program{
		{ChannelID: ids[0], StartAt: now.Unix(), StopAt: now.Add(time.Hour).Unix(), Title: "T"},
	}); err != nil {
		t.Fatal(err)
	}

	src, err := st.GetChannelSource(ctx, srcID)
	if err != nil {
		t.Fatal(err)
	}
	if src.ProgramCount != 1 {
		t.Errorf("program_count = %d, want 1", src.ProgramCount)
	}
	if src.EPGRefreshedAt == nil {
		t.Error("epg_refreshed_at was not recorded")
	}
	if src.EPGURL == nil || *src.EPGURL != "http://example.invalid/guide.xml" {
		t.Errorf("epg_url = %v", src.EPGURL)
	}
}

func TestSetChannelSourceEPGURL(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	srcID, _ := seedChannels(t, st, "Provider", "a")

	if err := st.SetChannelSourceEPGURL(ctx, srcID, ""); err != nil {
		t.Fatal(err)
	}
	src, err := st.GetChannelSource(ctx, srcID)
	if err != nil {
		t.Fatal(err)
	}
	if src.EPGURL != nil {
		t.Errorf("clearing left %q", *src.EPGURL)
	}

	if err := st.SetChannelSourceEPGURL(ctx, 999999, "http://example.invalid/g.xml"); err != ErrNotFound {
		t.Errorf("unknown source: %v, want ErrNotFound", err)
	}
}

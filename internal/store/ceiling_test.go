package store

import (
	"context"
	"path/filepath"
	"testing"
)

/*
 * The ceiling, asserted at the layer that enforces it (ADR 0015).
 *
 * These are the tests that matter in this package, for the same reason the
 * sharing ones next door do: the failure is silent. A listing that forgets the
 * predicate shows a child something the household said no to, and nothing about
 * the response looks wrong.
 *
 * The two halves are tested separately on purpose. A listing that hides a tile
 * is a convenience; MayPlay is the check that stands between a hand-written
 * request and a file, and it has to hold on its own — a client that never asks
 * for the grid can still ask for the stream.
 */

type ceilingFixture struct {
	st      *Store
	lib     *Library
	film    int64 // rated R
	kids    int64 // rated G
	homeVid int64 // rated nothing at all
	show    int64 // rated TV-MA
	season  int64 // rated nothing
	episode int64 // rated nothing; must inherit the show's
}

func seedForCeiling(t *testing.T) ceilingFixture {
	t.Helper()
	ctx := context.Background()
	st := openTestStore(t)
	root := t.TempDir()

	lib, err := st.CreateLibrary(ctx, "Films", "movie", root)
	if err != nil {
		t.Fatal(err)
	}

	add := func(name, kind, title string, parent *int64) int64 {
		t.Helper()
		f := ScanFile{
			LibraryID: lib.ID, Path: filepath.Join(root, name), Kind: kind,
			Title: title, SortTitle: title, Container: "mkv", SizeBytes: 1, MTime: 1,
		}
		id, err := st.UpsertItem(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		if parent != nil {
			if err := st.SetParent(ctx, id, parent); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}
	rate := func(id int64, label string) {
		t.Helper()
		if err := st.UpdateItemMetadata(ctx, id, ItemMetadata{ContentRating: &label}); err != nil {
			t.Fatal(err)
		}
	}

	f := ceilingFixture{st: st, lib: lib}
	f.film = add("scream.mkv", "movie", "Scream", nil)
	rate(f.film, "R")
	f.kids = add("kids.mkv", "movie", "Paddington", nil)
	rate(f.kids, "G")
	// No rating at all. This is home video, anything a provider never matched,
	// and most of what somebody added by hand — the case the rule turns on.
	f.homeVid = add("holiday.mkv", "movie", "Holiday 2004", nil)

	f.show = add("show", "show", "Some Programme", nil)
	rate(f.show, "TV-MA")
	f.season = add("show-s1", "season", "Season 1", &f.show)
	f.episode = add("show-s1e1.mkv", "episode", "Pilot", &f.season)
	return f
}

func ceilingUser(t *testing.T, st *Store, label string) *User {
	t.Helper()
	ctx := context.Background()
	u, err := st.CreateUser(ctx, "", "child-"+label, "hash", RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetMaxContentRating(ctx, u.ID, label); err != nil {
		t.Fatal(err)
	}
	return u
}

func listed(t *testing.T, st *Store, lib int64, ceiling string) map[string]bool {
	t.Helper()
	items, total, err := st.ListItems(context.Background(), ItemFilter{
		LibraryID: lib, MaxContentRating: ceiling, Limit: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, it := range items {
		out[it.Title] = true
	}
	// The count and the page are built from one WHERE, and a total that
	// described a larger library than the page would be its own kind of leak.
	if total != len(items) {
		t.Errorf("total = %d but %d items came back", total, len(items))
	}
	return out
}

func TestAListingUnderACeilingShowsOnlyWhatIsPermitted(t *testing.T) {
	f := seedForCeiling(t)
	got := listed(t, f.st, f.lib.ID, "PG")

	if !got["Paddington"] {
		t.Error("a G film was hidden by a PG ceiling")
	}
	if got["Scream"] {
		t.Error("an R film was listed under a PG ceiling")
	}
}

func TestAnUnratedItemIsNotQuietlyPermitted(t *testing.T) {
	/*
	 * The uncomfortable half. A limit that stops at the catalogued and waves
	 * everything else past is a filter that looks like a limit — and in a real
	 * library the uncatalogued is where the unlabelled home video sits.
	 *
	 * In SQL this falls out of NULL not being IN anything, which is a fact
	 * about three-valued logic and not an intention. Asserted so it stays an
	 * intention.
	 */
	f := seedForCeiling(t)
	if listed(t, f.st, f.lib.ID, "PG")["Holiday 2004"] {
		t.Error("an unrated item was listed under a PG ceiling")
	}
}

func TestNoCeilingChangesNothing(t *testing.T) {
	// The ordinary case, and the one an upgrade lands everybody in.
	f := seedForCeiling(t)
	got := listed(t, f.st, f.lib.ID, "")
	for _, title := range []string{"Scream", "Paddington", "Holiday 2004"} {
		if !got[title] {
			t.Errorf("%q was hidden from an account with no ceiling", title)
		}
	}
}

func TestAnEpisodeIsJudgedByItsShow(t *testing.T) {
	/*
	 * The rule that decides whether this feature is usable at all.
	 *
	 * An episode almost never carries a certificate of its own; its show does.
	 * Without inheritance a ceiling hides every episode in the library while
	 * leaving films visible, which is not the limit anybody asked for — it is
	 * how a feature gets switched off and called broken.
	 */
	f := seedForCeiling(t)
	ctx := context.Background()
	child := ceilingUser(t, f.st, "PG")

	ok, err := f.st.MayPlay(ctx, child.ID, f.episode)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("an episode of a TV-MA show played under a PG ceiling, two levels up from its rating")
	}

	// And the other direction: the inheritance must not block what it permits.
	teen := ceilingUser(t, f.st, "TV-MA")
	if ok, _ := f.st.MayPlay(ctx, teen.ID, f.episode); !ok {
		t.Error("the same episode was refused to an account whose ceiling allows it")
	}
}

func TestPlaybackIsRefusedEvenWhenNobodyAskedForAListing(t *testing.T) {
	/*
	 * The half that matters. A client that never fetches the grid can still ask
	 * for the stream, and a limit enforced only in the listing is a suggestion.
	 */
	f := seedForCeiling(t)
	ctx := context.Background()
	child := ceilingUser(t, f.st, "PG")

	if ok, _ := f.st.MayPlay(ctx, child.ID, f.film); ok {
		t.Error("an R film was authorised for a PG account")
	}
	if ok, _ := f.st.MayPlay(ctx, child.ID, f.kids); !ok {
		t.Error("a G film was refused to a PG account")
	}
	if ok, _ := f.st.MayPlay(ctx, child.ID, f.homeVid); ok {
		t.Error("an unrated film was authorised for a PG account")
	}
}

func TestAnAccountWithNoCeilingPlaysAnything(t *testing.T) {
	f := seedForCeiling(t)
	ctx := context.Background()
	adult, err := f.st.CreateUser(ctx, "", "adult", "hash", RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{f.film, f.homeVid, f.episode} {
		if ok, _ := f.st.MayPlay(ctx, adult.ID, id); !ok {
			t.Errorf("item %d was refused to an account with no ceiling", id)
		}
	}
}

func TestAnIdWithNoAccountHasNoCeiling(t *testing.T) {
	/*
	 * This was written the other way round first — an id naming no account
	 * looks like a stale session, so refuse it — and it is wrong.
	 *
	 * An unsecured loopback server has no accounts at all and reads every
	 * request as store.LocalUserID, so refusing the unknown emptied the whole
	 * library for the one configuration that works out of the box. Deciding
	 * whether a session is real belongs to the session layer, which does it on
	 * every request; this answers what a ceiling permits, and an account with
	 * none is not restricted.
	 */
	f := seedForCeiling(t)
	if ok, _ := f.st.MayPlay(context.Background(), LocalUserID, f.film); !ok {
		t.Error("the local user of an unsecured server was refused its own library")
	}
}

func TestACeilingNothingCanPlaceIsRefusedRatherThanStored(t *testing.T) {
	/*
	 * The only failure worse than being too strict: a household believing a
	 * limit is in force that is not. An unplaceable label would silently mean
	 * "no limit", so it does not get written.
	 */
	ctx := context.Background()
	st := openTestStore(t)
	u, err := st.CreateUser(ctx, "", "child", "hash", RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetMaxContentRating(ctx, u.ID, "Certificate 27"); err == nil {
		t.Fatal("an unplaceable ceiling was accepted")
	}
	got, err := st.MaxContentRating(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("ceiling = %q, want nothing stored", got)
	}
}

func TestTheCeilingSurvivesBeingReadBack(t *testing.T) {
	// It rides on the account row, so every path that loads a user carries it —
	// including the one a session uses on every request.
	ctx := context.Background()
	st := openTestStore(t)
	u, err := st.CreateUser(ctx, "", "child", "hash", RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetMaxContentRating(ctx, u.ID, "PG-13"); err != nil {
		t.Fatal(err)
	}
	back, err := st.UserByID(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.MaxContentRating != "PG-13" {
		t.Errorf("ceiling = %q, want PG-13", back.MaxContentRating)
	}
}

func TestAChosenFilterAndACeilingIntersect(t *testing.T) {
	/*
	 * They sit next to each other in ItemFilter and mean different things: one
	 * is a filter a person chose and can remove, the other a limit set for them
	 * that they cannot. Asking for R under a PG ceiling shows nothing, which is
	 * the honest answer — and must not show R.
	 */
	f := seedForCeiling(t)
	items, _, err := f.st.ListItems(context.Background(), ItemFilter{
		LibraryID: f.lib.ID, ContentRatings: []string{"R"},
		MaxContentRating: "PG", Limit: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Errorf("got %d items, want none: a chosen filter must not widen a ceiling", len(items))
	}
}

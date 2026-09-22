package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

// shareFixture gives a paired peer and two libraries — the minimum for a grant
// to be possible and for "shared" to be distinguishable from "everything".
func shareFixture(t *testing.T) (*Store, context.Context, *Library, *Library) {
	t.Helper()
	st := newStore(t)
	ctx := context.Background()

	if err := st.AddPeer(ctx, samplePeer(fpA)); err != nil {
		t.Fatal(err)
	}
	films, err := st.CreateLibrary(ctx, "Films", "movie", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	music, err := st.CreateLibrary(ctx, "Music", "music", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return st, ctx, films, music
}

// Pairing grants nothing (ADR 0071 §1). The absence of a row is the default,
// so this is the state a newly paired server is in and it must see nothing.
func TestPairingSharesNothing(t *testing.T) {
	st, ctx, _, _ := shareFixture(t)

	ids, err := st.SharedLibraries(ctx, fpA)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Errorf("a freshly paired peer sees %v, want nothing", ids)
	}
}

/*
 * The distinction the whole ceiling rests on.
 *
 * "Shared with no ceiling" and "not shared at all" both answer with an empty
 * string, and a caller that cannot tell them apart will read one as the other.
 * Whichever way it guesses is wrong: treat not-shared as no-ceiling and every
 * limit is bypassed; treat no-ceiling as not-shared and sharing does not work.
 */
func TestCeilingDistinguishesNoLimitFromNoAccess(t *testing.T) {
	st, ctx, films, music := shareFixture(t)

	if err := st.ShareLibrary(ctx, fpA, films.ID, "", time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}

	ceiling, err := st.CeilingFor(ctx, fpA, films.ID)
	if err != nil {
		t.Fatalf("shared library: %v, want no error", err)
	}
	if ceiling != "" {
		t.Errorf("ceiling = %q, want empty for a share with no limit", ceiling)
	}

	if _, err := st.CeilingFor(ctx, fpA, music.ID); !errors.Is(err, ErrNotShared) {
		t.Errorf("unshared library: %v, want ErrNotShared", err)
	}
}

// A peer this server has never granted anything must not resolve to "no
// ceiling" — the failure ADR 0071 §6 describes, where a friend is admitted
// past every limit and it looks like it worked.
func TestCeilingFailsClosedForAnUnknownPeer(t *testing.T) {
	st, ctx, films, _ := shareFixture(t)

	if _, err := st.CeilingFor(ctx, "NOTAPEER", films.ID); !errors.Is(err, ErrNotShared) {
		t.Errorf("unknown peer: %v, want ErrNotShared", err)
	}
}

// Unpairing takes everything away with one action and nothing per-library to
// clean up (ADR 0071 §1). The cascade is the revocation mechanism, so it is
// tested rather than assumed.
func TestUnpairingRevokesEveryShare(t *testing.T) {
	st, ctx, films, music := shareFixture(t)
	for _, l := range []*Library{films, music} {
		if err := st.ShareLibrary(ctx, fpA, l.ID, "", time.Unix(1000, 0)); err != nil {
			t.Fatal(err)
		}
	}

	if err := st.RemovePeer(ctx, fpA); err != nil {
		t.Fatal(err)
	}

	ids, err := st.SharedLibraries(ctx, fpA)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Errorf("after unpairing the peer still sees %v", ids)
	}
}

// Deleting a library must not leave a grant naming something that is gone.
func TestDeletingALibraryDropsItsShares(t *testing.T) {
	st, ctx, films, music := shareFixture(t)
	for _, l := range []*Library{films, music} {
		if err := st.ShareLibrary(ctx, fpA, l.ID, "", time.Unix(1000, 0)); err != nil {
			t.Fatal(err)
		}
	}

	if err := st.DeleteLibrary(ctx, films.ID); err != nil {
		t.Fatal(err)
	}

	ids, err := st.SharedLibraries(ctx, fpA)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != music.ID {
		t.Errorf("shared = %v, want only the surviving library %d", ids, music.ID)
	}
}

// Adjusting a limit is not re-deciding to share, so the date must not move —
// it is what the host is shown to remember when they agreed.
func TestChangingACeilingKeepsTheOriginalDate(t *testing.T) {
	st, ctx, films, _ := shareFixture(t)

	if err := st.ShareLibrary(ctx, fpA, films.ID, "", time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}
	if err := st.ShareLibrary(ctx, fpA, films.ID, "PG-13", time.Unix(9999, 0)); err != nil {
		t.Fatal(err)
	}

	shares, err := st.SharesTo(ctx, fpA)
	if err != nil {
		t.Fatal(err)
	}
	if len(shares) != 1 {
		t.Fatalf("%d shares, want 1", len(shares))
	}
	if shares[0].Ceiling != "PG-13" {
		t.Errorf("ceiling = %q, want the new limit", shares[0].Ceiling)
	}
	if shares[0].SharedAt != 1000 {
		t.Errorf("shared_at = %d, want the original 1000", shares[0].SharedAt)
	}
}

/*
 * A ceiling this server does not recognise is refused at the door.
 *
 * Stored, it would resolve to "no ceiling" at read time by way of
 * rating.Known — a limit that silently is not one. Refusing the write is the
 * only place this can be caught where somebody is there to be told.
 */
func TestAnUnknownCeilingIsRefused(t *testing.T) {
	st, ctx, films, _ := shareFixture(t)

	err := st.ShareLibrary(ctx, fpA, films.ID, "NC-42", time.Unix(1000, 0))
	if err == nil {
		t.Fatal("stored a ceiling this server cannot read back")
	}

	if _, err := st.CeilingFor(ctx, fpA, films.ID); !errors.Is(err, ErrNotShared) {
		t.Errorf("a refused share left a row: %v", err)
	}
}

// Un-sharing something that was never shared is the state the caller asked
// for, not an error.
func TestUnsharingWhatWasNeverSharedSucceeds(t *testing.T) {
	st, ctx, films, _ := shareFixture(t)

	if err := st.UnshareLibrary(ctx, fpA, films.ID); err != nil {
		t.Errorf("unshare of a non-share: %v", err)
	}
}

/*
 * The number the host is shown before they choose a ceiling (ADR 0071 §6).
 *
 * An unrated item is blocked, and those items then vanish with no explanation
 * — the same discomfort the local rule carries. The mitigation is telling the
 * host what it costs, which is only useful if the number is right.
 *
 * Uses the ceiling fixture next door because it already holds the awkward
 * cases: an item rated nothing, and an episode that is rated nothing itself
 * but inherits its show's certificate two levels up. An inherited rating is
 * not an unrated item, and counting it as one would warn about a cost that
 * does not exist.
 */
func TestUnratedCountSeesInheritedRatings(t *testing.T) {
	f := seedForCeiling(t)
	ctx := context.Background()

	unrated, total, err := f.st.UnratedInShare(ctx, f.lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if total == 0 {
		t.Fatal("counted nothing at all; the fixture should hold several items")
	}
	// homeVid is the only item with no certificate anywhere above it. The
	// episode has none of its own and must not be counted: it inherits TV-MA.
	if unrated != 1 {
		t.Errorf("unrated = %d of %d, want exactly the one item rated nothing "+
			"(an inherited rating is not unrated)", unrated, total)
	}
}

/*
 * A ceiling on a music or picture share is a control that does nothing, so the
 * count must not report the whole library as unrated — that would read as a
 * warning about a cost, when the truth is that the limit does not apply.
 */
func TestUnratedCountIgnoresKindsWithNoCertificate(t *testing.T) {
	st, ctx, _, music := shareFixture(t)

	unrated, total, err := st.UnratedInShare(ctx, music.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unrated != 0 || total != 0 {
		t.Errorf("music library counted %d unrated of %d, want 0 of 0: no "+
			"certificate exists for a track", unrated, total)
	}
}

/*
 * The test ADR 0071 §6 asks for by name: one that fails loudly if a friend
 * ever reaches the account-resolving path.
 *
 * Both principals are strings underneath, and the two paths fail in opposite
 * directions — an account with no row resolves to no ceiling, a friend with no
 * share is refused. Confuse them and every ceiling a host set is bypassed
 * silently, which is the defect the split exists to prevent.
 *
 * So an account is planted whose id *is* the fingerprint, with no ceiling of
 * its own. If Friend(fp) ever resolved through the user table it would find
 * that row, read "no ceiling", and permit. The only way this passes is if the
 * friend path never looks there.
 */
func TestAFriendNeverResolvesThroughTheUserTable(t *testing.T) {
	f := seedForCeiling(t)
	ctx := context.Background()

	if err := f.st.AddPeer(ctx, samplePeer(fpA)); err != nil {
		t.Fatal(err)
	}
	// An account whose id collides with the fingerprint, with no ceiling —
	// the row that would fail open if the paths were confused.
	if _, err := f.st.db.ExecContext(ctx,
		`INSERT INTO user (id, name, password_hash, role, created_at)
		 VALUES (?, 'collision', 'x', 'member', 0)`, fpA); err != nil {
		t.Fatal(err)
	}

	// Nothing has been shared with this peer.
	if ok, err := f.st.MayPlay(ctx, Friend(fpA), f.kids); err != nil || ok {
		t.Errorf("friend with no share: allowed=%v err=%v — want refused. "+
			"An account row with the same id must not be consulted", ok, err)
	}
}

// With a share and no ceiling, a friend sees what the library holds — the
// ordinary case, and the one that would break if the fix were "refuse friends".
func TestASharedLibraryWithNoCeilingPermits(t *testing.T) {
	f := seedForCeiling(t)
	ctx := context.Background()
	if err := f.st.AddPeer(ctx, samplePeer(fpA)); err != nil {
		t.Fatal(err)
	}
	if err := f.st.ShareLibrary(ctx, fpA, f.lib.ID, "", time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}

	for _, id := range []int64{f.film, f.kids, f.episode} {
		if ok, err := f.st.MayPlay(ctx, Friend(fpA), id); err != nil || !ok {
			t.Errorf("item %d: allowed=%v err=%v, want permitted", id, ok, err)
		}
	}
}

// The share's ceiling applies to the friend, including the rule that an
// unrated item is not shown (ADR 0071 §6).
func TestAShareCeilingLimitsTheFriend(t *testing.T) {
	f := seedForCeiling(t)
	ctx := context.Background()
	if err := f.st.AddPeer(ctx, samplePeer(fpA)); err != nil {
		t.Fatal(err)
	}
	if err := f.st.ShareLibrary(ctx, fpA, f.lib.ID, "PG", time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}

	if ok, _ := f.st.MayPlay(ctx, Friend(fpA), f.kids); !ok {
		t.Error("a G film is under a PG ceiling and must be permitted")
	}
	if ok, _ := f.st.MayPlay(ctx, Friend(fpA), f.film); ok {
		t.Error("an R film is over a PG ceiling and must be refused")
	}
	if ok, _ := f.st.MayPlay(ctx, Friend(fpA), f.homeVid); ok {
		t.Error("an unrated item must not be shown to a friend under a ceiling")
	}
}

// Un-sharing takes effect on the next request (ADR 0071 §6), so the same
// principal that was permitted a moment ago is refused now.
func TestUnsharingRefusesTheNextRequest(t *testing.T) {
	f := seedForCeiling(t)
	ctx := context.Background()
	if err := f.st.AddPeer(ctx, samplePeer(fpA)); err != nil {
		t.Fatal(err)
	}
	if err := f.st.ShareLibrary(ctx, fpA, f.lib.ID, "", time.Unix(1000, 0)); err != nil {
		t.Fatal(err)
	}
	if ok, _ := f.st.MayPlay(ctx, Friend(fpA), f.kids); !ok {
		t.Fatal("fixture is wrong: the share should permit before it is removed")
	}

	if err := f.st.UnshareLibrary(ctx, fpA, f.lib.ID); err != nil {
		t.Fatal(err)
	}
	if ok, _ := f.st.MayPlay(ctx, Friend(fpA), f.kids); ok {
		t.Error("still permitted after the share was removed")
	}
}

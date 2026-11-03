package enrich

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"lancast/internal/meta"
	"lancast/internal/store"
)

/*
 * Albums and artists (ADR 0115).
 *
 * Their own path rather than the shared one, which would get three things
 * wrong. The shared merge puts a provider above what an item holds, and an
 * album's tags are the owner's statement. It writes NFO sidecars, collections
 * and ratings, none of which an album has. And it searches every item by its
 * own title, where an artist is identified through its albums instead.
 *
 * Every "no answer" here is checked against "no answer yet". A provider that
 * is busy, unreachable or absent leaves the item pending; only a search that
 * ran and found nothing marks it unmatched. A busy afternoon at MusicBrainz
 * must not become a library of albums marked "no match".
 */

// MusicStore is what matching music needs beyond the shared surface. The
// real store has all of it; a test fake may leave music out.
type MusicStore interface {
	GetItem(ctx context.Context, id int64, userID string) (*store.Item, error)
	ApplyAlbumMatch(ctx context.Context, itemID int64, m store.AlbumMatch) error
	ApplyArtistMatch(ctx context.Context, itemID int64, m store.ArtistMatch) error
	MarkMusicUnmatched(ctx context.Context, itemID int64, score float64) error
	ArtistCreditVotes(ctx context.Context, artistID int64) (map[string]int, int, error)
	RequeueArtistOf(ctx context.Context, albumID int64) error
	SelectedArtworkSource(ctx context.Context, itemID int64, kind string) (string, bool, error)
}

func isMusic(kind meta.Kind) bool { return kind == meta.KindAlbum || kind == meta.KindArtist }

func (w *Worker) enrichMusic(ctx context.Context, item store.Item) (bool, error) {
	ms, ok := w.st.(MusicStore)
	if !ok || len(w.reg.Providers(meta.Kind(item.Kind))) == 0 {
		return false, nil
	}
	if item.Kind == string(meta.KindArtist) {
		return w.enrichArtist(ctx, ms, item)
	}
	return w.enrichAlbum(ctx, ms, item)
}

func (w *Worker) enrichAlbum(ctx context.Context, ms MusicStore, item store.Item) (bool, error) {
	artist := ""
	if item.ParentID != nil {
		if p, err := ms.GetItem(ctx, *item.ParentID, ""); err == nil && p.Kind == string(meta.KindArtist) {
			artist = p.Title
		}
	}
	q := meta.AlbumQuery(item.Title, artist, derefInt(item.Year))
	cands, err := w.reg.Search(ctx, q)
	if err != nil {
		w.log.Debug("album search failed; left pending", "item", item.ID, "error", err)
		return false, nil
	}
	best, state := meta.Decide(cands)
	if state == meta.StateUnmatched {
		return true, ms.MarkMusicUnmatched(ctx, item.ID, best.Score)
	}
	provider, ok := w.reg.Provider(best.Provider)
	if !ok {
		return false, nil
	}
	rec, err := provider.Fetch(ctx, meta.Ref{Kind: meta.KindAlbum, ExternalID: best.ExternalID})
	if err != nil || rec == nil {
		w.log.Debug("album fetch failed; left pending", "item", item.ID, "error", err)
		return false, nil
	}
	m := store.AlbumMatch{
		Provider:   best.Provider,
		ExternalID: best.ExternalID,
		State:      state,
		Score:      best.Score,
		Genres:     rec.Genres,
	}
	if rec.Fields.Year != nil {
		m.Year = *rec.Fields.Year
	}
	if rec.Music != nil {
		m.ReleaseType = rec.Music.ReleaseType
		if len(rec.Music.Artists) > 0 {
			m.ArtistID = rec.Music.Artists[0].ID
		}
	}
	if err := ms.ApplyAlbumMatch(ctx, item.ID, m); err != nil {
		return true, err
	}
	// Only a confident match brings a cover: a wrong picture on a guess is
	// the most visible mistake this could make.
	if state == meta.StateMatched {
		w.albumCover(ctx, ms, item.ID, rec)
	}
	return true, nil
}

/*
 * albumCover gives a matched album its record's front cover from the Cover
 * Art Archive (ADR 0115, step 4), only where the album has none of its own.
 *
 * Art read off the disk has no source URL and is never replaced; nor is a
 * locked one. A cover that came from the archive is replaced, because it
 * belonged to the match, and a corrected match should not keep the wrong
 * record's picture. The local cover pass writes over this one whenever it
 * finds art in the files, so the tags win here too.
 */
func (w *Worker) albumCover(ctx context.Context, ms MusicStore, albumID int64, rec *meta.Record) {
	if len(rec.Artwork) == 0 {
		return
	}
	src, has, err := ms.SelectedArtworkSource(ctx, albumID, string(meta.ArtPoster))
	if err != nil {
		w.log.Warn("album cover check failed", "album", albumID, "error", err)
		return
	}
	if has && (!isCoverArchive(src) || src == rec.Artwork[0].URL) {
		return
	}
	locked, err := w.st.LockedFields(ctx, albumID)
	if err != nil || meta.LockedSet(locked)[meta.FieldArtwork] {
		return
	}
	w.storeArtwork(ctx, albumID, rec.Artwork)
}

func isCoverArchive(url string) bool { return strings.Contains(url, "coverartarchive.org/") }

/*
 * enrichArtist takes the artist its albums agree on.
 *
 * It waits while any of its albums is still unseen, since the next one may
 * change the count. With none voting it is unmatched. A tie between two
 * artists is unmatched too: two albums crediting two different artists say
 * the folder holds two acts, not which one this row is. A majority is
 * matched; a plurality short of half is applied and marked for review.
 */
func (w *Worker) enrichArtist(ctx context.Context, ms MusicStore, item store.Item) (bool, error) {
	votes, pending, err := ms.ArtistCreditVotes(ctx, item.ID)
	if err != nil {
		return false, err
	}
	if pending > 0 {
		return false, nil
	}
	id, share, tied := winner(votes)
	if id == "" || tied {
		return true, ms.MarkMusicUnmatched(ctx, item.ID, share)
	}
	providers := w.reg.Providers(meta.KindArtist)
	rec, err := providers[0].Fetch(ctx, meta.Ref{Kind: meta.KindArtist, ExternalID: id})
	if err != nil || rec == nil {
		w.log.Debug("artist fetch failed; left pending", "item", item.ID, "error", err)
		return false, nil
	}
	state := meta.StateMatched
	if share < 0.5 {
		state = meta.StateReview
	}
	return true, ms.ApplyArtistMatch(ctx, item.ID, store.ArtistMatch{
		Provider:   providers[0].ID(),
		ExternalID: id,
		State:      state,
		Score:      share,
		Genres:     rec.Genres,
	})
}

// winner is the most-voted id, its share of the votes, and whether another
// id has as many.
func winner(votes map[string]int) (id string, share float64, tied bool) {
	total := 0
	type kv struct {
		id string
		n  int
	}
	var all []kv
	for k, n := range votes {
		total += n
		all = append(all, kv{k, n})
	}
	if total == 0 {
		return "", 0, false
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].n != all[j].n {
			return all[i].n > all[j].n
		}
		return all[i].id < all[j].id
	})
	tied = len(all) > 1 && all[1].n == all[0].n
	return all[0].id, float64(all[0].n) / float64(total), tied
}

/*
 * ApplyMusicMatch applies the record a person chose in Fix match and locks
 * the identity, so no later pass re-decides it. The fields are still filled
 * only where the tags left a gap: choosing a record says what the album is,
 * not that its tags are wrong. Correcting an album sends its artist back to
 * be voted again, since the vote may have changed.
 */
func (w *Worker) ApplyMusicMatch(ctx context.Context, item store.Item, providerID, externalID string) error {
	ms, ok := w.st.(MusicStore)
	if !ok {
		return fmt.Errorf("this store does not hold music matches")
	}
	provider, ok := w.reg.Provider(providerID)
	if !ok {
		return fmt.Errorf("unknown provider %q", providerID)
	}
	kind := meta.Kind(item.Kind)
	rec, err := provider.Fetch(ctx, meta.Ref{Kind: kind, ExternalID: externalID})
	if err != nil {
		return fmt.Errorf("fetch confirmed match: %w", err)
	}
	if rec == nil {
		return fmt.Errorf("provider %q returned no record for %q", providerID, externalID)
	}
	if kind == meta.KindArtist {
		return ms.ApplyArtistMatch(ctx, item.ID, store.ArtistMatch{
			Provider: providerID, ExternalID: externalID, Score: 1,
			Genres: rec.Genres, Confirmed: true,
		})
	}
	m := store.AlbumMatch{
		Provider: providerID, ExternalID: externalID, Score: 1,
		Genres: rec.Genres, Confirmed: true,
	}
	if rec.Fields.Year != nil {
		m.Year = *rec.Fields.Year
	}
	if rec.Music != nil {
		m.ReleaseType = rec.Music.ReleaseType
		if len(rec.Music.Artists) > 0 {
			m.ArtistID = rec.Music.Artists[0].ID
		}
	}
	if err := ms.ApplyAlbumMatch(ctx, item.ID, m); err != nil {
		return err
	}
	w.albumCover(ctx, ms, item.ID, rec)
	return ms.RequeueArtistOf(ctx, item.ID)
}

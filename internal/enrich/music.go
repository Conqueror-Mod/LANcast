package enrich

import (
	"context"
	"sort"

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
	return true, ms.ApplyAlbumMatch(ctx, item.ID, m)
}

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

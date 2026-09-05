package store

import (
	"context"
	"fmt"
	"sort"
	"time"
)

/*
 * What a photograph looks like, as a vector (ADR 0060).
 *
 * The search is a brute-force cosine scan and there is no index, which is a
 * decision the ADR argues from arithmetic rather than an omission: 40,000
 * photographs is 82MB of vectors and about 20 million multiply-adds, and
 * ADR 0057 measured 40,000 at roughly twice the largest real library here.
 * Immich needs a vector extension because Immich scales past what a household
 * owns; buying one here would cost the pure-Go SQLite driver, which is the same
 * trade ADR 0052 refused for cgo.
 *
 * The encoding is `face.go`'s, unchanged, and so is `cosine`. Two vector
 * formats in one package would be two things to get wrong.
 */

// SearchHit is one photograph and how well it matched. It is serialized as a
// type rather than unpacked into a map, so the tags are the wire.
type SearchHit struct {
	Item Item `json:"item"`
	// Score is a cosine between unit vectors: bounded, comparable within one
	// answer, and close to meaningless across two. It travels so a client can
	// tell an obvious first from a distant fifth.
	Score float64 `json:"score"`
}

/*
 * SavePhotoEmbedding records what one photograph looks like.
 *
 * Refuses a photograph a mark covers, rather than storing it and filtering on
 * read. That is ADR 0051's rule and the same one RecordFaces enforces: an
 * embedding is derived from the photograph and is not less private than it, so
 * a stored one would sit in the database and in every backup taken afterwards.
 * A CLIP vector is the sharper case — a face embedding says *who*, this says
 * *what the picture is of*, so a covered folder reachable by searching its
 * contents would be a cover that lifts for anyone who guesses.
 */
func (s *Store) SavePhotoEmbedding(ctx context.Context, itemID int64, model string, v []float32) error {
	if len(v) == 0 {
		return fmt.Errorf("save photo embedding %d: empty vector", itemID)
	}

	var covered int
	if err := s.db.QueryRowContext(ctx,
		`SELECT sensitive_effective FROM media_item WHERE id = ?`, itemID).Scan(&covered); err != nil {
		return fmt.Errorf("save photo embedding %d: %w", itemID, err)
	}
	if covered != 0 {
		return fmt.Errorf("save photo embedding %d: photograph is covered by a mark", itemID)
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO photo_embedding (item_id, model, dims, embedding, embedded_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(item_id) DO UPDATE SET
			model = excluded.model, dims = excluded.dims,
			embedding = excluded.embedding, embedded_at = excluded.embedded_at`,
		itemID, model, len(v), encodeEmbedding(v), time.Now().Unix()); err != nil {
		return fmt.Errorf("save photo embedding %d: %w", itemID, err)
	}
	return nil
}

/*
 * DeletePhotoEmbeddingsUnderSensitive removes vectors for anything a mark now
 * covers, and answers how many went.
 *
 * The counterpart to saving refusing them: marking a folder that has already
 * been indexed has to delete what is there, not merely stop it being returned.
 * Unconditional rather than only-when-marking, the way the faces version is —
 * unmarking cannot resurrect what was deleted, and running it after a clear
 * costs one query that matches nothing, which is cheaper than a rule with an
 * exception in it.
 */
func (s *Store) DeletePhotoEmbeddingsUnderSensitive(ctx context.Context, libraryID int64) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM photo_embedding
		 WHERE item_id IN (
			SELECT id FROM media_item
			 WHERE library_id = ? AND sensitive_effective = 1)`, libraryID)
	if err != nil {
		return 0, fmt.Errorf("delete covered photo embeddings: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

/*
 * PhotosPendingEmbedding lists photographs this model has not seen.
 *
 * Keyed on the model rather than on presence, because a vector from another
 * model is not a stale vector — it is a vector in a different coordinate
 * system, and ranking a library against a mixture of two is worse than ranking
 * it against one. Swapping the model is meant to be a file swap; this is what
 * makes the pass notice.
 *
 * Marked folders are absent, so the pass never asks the sidecar to open one.
 */
func (s *Store) PhotosPendingEmbedding(ctx context.Context, libraryID int64, model string, limit int) ([]Item, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+itemCols+`
		  FROM media_item
		 WHERE library_id = ? AND kind = 'photo' AND missing = 0
		   AND sensitive_effective = 0
		   AND NOT EXISTS (
			SELECT 1 FROM photo_embedding e
			 WHERE e.item_id = media_item.id AND e.model = ?)
		 ORDER BY id
		 LIMIT ?`, libraryID, model, limit)
	if err != nil {
		return nil, fmt.Errorf("photos pending embedding: %w", err)
	}
	defer rows.Close()
	return scanItems(rows)
}

/*
 * SearchPhotosByVector ranks a library's photographs against one vector.
 *
 * Brute force, and the ADR's arithmetic says why that is enough. What it is
 * *not* is a similarity threshold: cosine scores from a contrastive model are
 * not calibrated to anything a person would recognise, so a cutoff would be a
 * number invented here and applied to every query. The caller asks for as many
 * as it wants to show, ordered, and decides what to do with the tail.
 *
 * `minScore` exists for the caller that has measured one. Zero means no floor.
 *
 * Marked folders cannot appear, and there are two independent reasons they
 * cannot: no vector is stored for them, and the join excludes them anyway. The
 * second is the one that does not depend on the first having worked.
 */
func (s *Store) SearchPhotosByVector(
	ctx context.Context, libraryID int64, model string, query []float32,
	limit int, minScore float64,
) ([]SearchHit, error) {
	if len(query) == 0 {
		return nil, fmt.Errorf("search photos: empty query vector")
	}
	if limit <= 0 || limit > 500 {
		limit = 60
	}

	/*
	 * Two passes on purpose: score narrow rows, then fetch the few that won.
	 *
	 * The obvious single query joins media_item and reads every column of every
	 * candidate — 40,000 wide rows, of which sixty survive. The ADR's warning is
	 * that the cost here is the read rather than the arithmetic, and reading
	 * `itemCols` for the whole library to throw away 99.85% of it is exactly the
	 * read it was warning about. This one carries an id and a blob.
	 */
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.item_id, e.embedding
		  FROM photo_embedding e
		  JOIN media_item mi ON mi.id = e.item_id
		 WHERE mi.library_id = ? AND e.model = ?
		   AND mi.missing = 0 AND mi.sensitive_effective = 0`, libraryID, model)
	if err != nil {
		return nil, fmt.Errorf("search photos: %w", err)
	}
	defer rows.Close()

	type scored struct {
		id    int64
		score float64
	}
	var hits []scored
	for rows.Next() {
		var id int64
		var blob []byte
		if err := rows.Scan(&id, &blob); err != nil {
			return nil, fmt.Errorf("search photos: %w", err)
		}
		v := decodeEmbedding(blob)
		// A row whose width does not match the query is from a different model
		// or a truncated write. Skipping it is honest; comparing it would be
		// arithmetic across two different coordinate systems.
		if len(v) != len(query) {
			continue
		}
		score := cosine(v, query)
		if score < minScore {
			continue
		}
		hits = append(hits, scored{id: id, score: score})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search photos: %w", err)
	}

	// Best first, then by id so a tie does not reorder between two identical
	// searches — the same total-order rule the cast search already follows.
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].id < hits[j].id
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	if len(hits) == 0 {
		return []SearchHit{}, nil
	}

	ids := make([]any, 0, len(hits))
	for _, h := range hits {
		ids = append(ids, h.id)
	}
	itemRows, err := s.db.QueryContext(ctx,
		`SELECT `+itemCols+` FROM media_item WHERE id IN (`+placeholders(len(ids))+`)`, ids...)
	if err != nil {
		return nil, fmt.Errorf("search photos: %w", err)
	}
	defer itemRows.Close()
	items, err := scanItems(itemRows)
	if err != nil {
		return nil, fmt.Errorf("search photos: %w", err)
	}
	/*
	 * The artwork has to be attached, and forgetting it is invisible from here.
	 *
	 * `itemCols` carries no images — every grid in this project gets them from
	 * AttachArtwork afterwards — so a search that skipped this returned rows
	 * that were correct in every field the ranking uses and rendered as a page
	 * of blank tiles with filenames on them. Nothing fails: the ids are right,
	 * the scores are right, the order is right, and the answer is unusable.
	 *
	 * Found by looking at it. The tests assert which photographs come back and
	 * in what order, which is exactly what was never wrong.
	 */
	if err := s.AttachArtwork(ctx, items); err != nil {
		return nil, fmt.Errorf("search photos: %w", err)
	}

	byID := make(map[int64]Item, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}

	// Re-imposed rather than relied upon: `IN` does not promise an order, and
	// the ranking is the whole answer here.
	out := make([]SearchHit, 0, len(hits))
	for _, h := range hits {
		if it, ok := byID[h.id]; ok {
			out = append(out, SearchHit{Item: it, Score: h.score})
		}
	}
	return out, nil
}

// EmbeddedPhotoCount is how many photographs this model has seen in a library,
// for progress reporting.
func (s *Store) EmbeddedPhotoCount(ctx context.Context, libraryID int64, model string) (int, error) {
	var n int
	/*
	 * Eligibility here must match the search's, exactly.
	 *
	 * Three queries decide what a library holds for this feature — this one,
	 * the pending count, and the search itself — and for a while this one
	 * disagreed with the other two: it counted every stored vector, including
	 * photographs on an unmounted drive that the search excludes.
	 *
	 * That is not a rounding error, because of what the number is used for. The
	 * screen says "N of M photographs are indexed, so a search only looks at
	 * those", and if N counts rows the search will not return then the sentence
	 * is false in the direction that matters: somebody is told more of their
	 * library was searched than actually was.
	 *
	 * A marked folder is already handled by deleting its embeddings (ADR 0051),
	 * so this clause is belt and braces there — and it is the missing ones that
	 * made it necessary.
	 */
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		  FROM photo_embedding e
		  JOIN media_item mi ON mi.id = e.item_id
		 WHERE mi.library_id = ? AND e.model = ?
		   AND mi.missing = 0 AND mi.sensitive_effective = 0`, libraryID, model).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("embedded photo count: %w", err)
	}
	return n, nil
}

// PhotosPendingEmbeddingCount is the same question as a number, for progress
// reporting. Re-asked between batches rather than counted down from a total
// measured once: a scan can add photographs while a pass runs, and marking a
// folder removes some from the queue entirely, so a counter that only fell
// would drift from the truth in both directions. The activity view has been
// bitten once already by a total that was measured at the start and never
// revised.
func (s *Store) PhotosPendingEmbeddingCount(ctx context.Context, libraryID int64, model string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		  FROM media_item
		 WHERE library_id = ? AND kind = 'photo' AND missing = 0
		   AND sensitive_effective = 0
		   AND NOT EXISTS (
			SELECT 1 FROM photo_embedding e
			 WHERE e.item_id = media_item.id AND e.model = ?)`, libraryID, model).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("photos pending embedding count: %w", err)
	}
	return n, nil
}

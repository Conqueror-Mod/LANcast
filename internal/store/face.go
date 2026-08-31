package store

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

/*
 * Faces, and grouping them into people (ADR 0052).
 *
 * Everything here is independent of which model produced the embeddings. The
 * worker hands over vectors; this decides what is the same person, and that
 * decision has to survive the model being replaced — which it will be, since
 * ADR 0052 chose ONNX precisely so improving quality is a file swap.
 *
 * Nothing in this file touches a sensitive folder. Faces are never recorded for
 * one, and marking a folder deletes the faces already under it: an embedding is
 * derived from a photograph and is not less private than it.
 */

// Face is one detection, as stored.
type Face struct {
	ID         int64
	ItemID     int64
	ClusterID  *int64
	X, Y, W, H int
	Score      float64
	Embedding  []float32
}

// FaceCluster is a group of faces believed to be one person.
type FaceCluster struct {
	ID         int64   `json:"id"`
	Name       *string `json:"name"`
	NameLocked bool    `json:"name_locked"`
	Count      int     `json:"count"`
	// Cover is a face to show for the group — the highest-scoring one, so an
	// unnamed cluster is represented by its clearest example rather than by
	// whichever row came back first.
	CoverFaceID *int64 `json:"cover_face_id,omitempty"`
	CoverItemID *int64 `json:"cover_item_id,omitempty"`
}

/*
 * SameFaceCosine is the similarity above which two faces are one person.
 *
 * 0.363 is the threshold OpenCV publishes for SFace, the embedder ADR 0052
 * chose, and it is used rather than a rounder number somebody liked the look
 * of. It belongs here as a named constant because it is a property of the
 * *model*: swapping the model means revisiting this line, and a magic number
 * buried in a comparison is a line nobody revisits.
 *
 * Erring low merges two people into one group; erring high splits one person
 * across several. The second is visibly annoying and trivially fixed by naming
 * both groups the same thing. The first quietly attaches somebody's face to
 * somebody else's name, which is the failure worth avoiding — so if this is
 * ever wrong, it should be wrong high.
 */
const SameFaceCosine = 0.363

// encodeEmbedding stores a vector as little-endian float32s. Explicit rather
// than gob or JSON: this blob is written once per face and read on every
// re-cluster, and a self-describing format would cost size and speed for a
// shape that is fixed by the model.
func encodeEmbedding(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(f))
	}
	return b
}

func decodeEmbedding(b []byte) []float32 {
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return v
}

/*
 * Cosine similarity of two embeddings.
 *
 * Not normalised in advance, because a model that returns unit vectors and one
 * that does not would then disagree silently — and "which models normalise" is
 * exactly the kind of fact that is true until the day it is not.
 */
func cosine(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

/*
 * RecordFaces replaces everything detected in one photograph.
 *
 * Replace rather than append: the worker is re-runnable, and running it twice
 * over a photograph must leave one set of faces rather than two. The cluster
 * assignments of the old rows go with them, which is why ClusterLibrary is
 * expected to run after a detection pass rather than before.
 *
 * Refused outright for an item inside a marked folder. The worker is not
 * supposed to be handed one, and refusing here rather than trusting the caller
 * is what makes that a property of the system instead of a habit.
 */
func (s *Store) RecordFaces(ctx context.Context, itemID int64, faces []Face) error {
	var sensitive int
	if err := s.db.QueryRowContext(ctx,
		`SELECT sensitive_effective FROM media_item WHERE id = ?`, itemID).Scan(&sensitive); err != nil {
		return fmt.Errorf("record faces for item %d: %w", itemID, err)
	}
	if sensitive != 0 {
		return fmt.Errorf("record faces for item %d: item is in a folder marked sensitive", itemID)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("record faces for item %d: %w", itemID, err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM face WHERE item_id = ?`, itemID); err != nil {
		return fmt.Errorf("record faces for item %d: %w", itemID, err)
	}
	now := time.Now().Unix()
	for _, f := range faces {
		if len(f.Embedding) == 0 {
			// A detection with no embedding cannot be grouped and would sit in
			// the table for ever as a face belonging to nobody.
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO face (item_id, x, y, w, h, score, embedding, detected_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			itemID, f.X, f.Y, f.W, f.H, f.Score, encodeEmbedding(f.Embedding), now); err != nil {
			return fmt.Errorf("record faces for item %d: %w", itemID, err)
		}
	}
	return tx.Commit()
}

// DeleteFacesUnderSensitive removes every face belonging to a marked folder.
//
// Called after a mark is applied, and it is a deletion rather than a filter on
// purpose: hiding an embedding still leaves it in the database and in every
// backup taken afterwards.
func (s *Store) DeleteFacesUnderSensitive(ctx context.Context, libraryID int64) (int, error) {
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM face
		 WHERE item_id IN (
			SELECT id FROM media_item
			 WHERE library_id = ? AND sensitive_effective = 1)`, libraryID)
	if err != nil {
		return 0, fmt.Errorf("delete faces under sensitive folders: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

/*
 * ClusterLibrary groups a library's faces into people.
 *
 * Greedy single-pass agglomeration against cluster centroids: for each face,
 * find the most similar cluster and join it if it is close enough, otherwise
 * start a new one. Not the most sophisticated clustering available, and chosen
 * because the alternative is worse in the way that matters — a method that
 * re-partitions everything from scratch produces different groups each run, and
 * groups that move under a name are groups somebody has to re-name.
 *
 * **Named clusters are seeded first and are never renamed, merged away, or
 * deleted.** That is the locked-fields rule applied to identity: a re-cluster
 * may decide that a face belongs to a named person, and may never decide that a
 * named person is somebody else. Faces are drawn to a named cluster before an
 * unnamed one at equal similarity, so naming a group makes it *more* stable
 * rather than freezing it.
 */
func (s *Store) ClusterLibrary(ctx context.Context, libraryID int64) error {
	type cluster struct {
		id       int64
		named    bool
		centroid []float32
		n        int
	}

	// Named clusters first, with their centroids, so they act as anchors.
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.name IS NOT NULL AND c.name <> '', f.embedding
		  FROM face_cluster c
		  JOIN face f ON f.cluster_id = c.id
		  JOIN media_item m ON m.id = f.item_id
		 WHERE c.library_id = ? AND m.sensitive_effective = 0
		 ORDER BY (c.name IS NULL), c.id`, libraryID)
	if err != nil {
		return fmt.Errorf("cluster library %d: %w", libraryID, err)
	}
	byID := map[int64]*cluster{}
	var clusters []*cluster
	for rows.Next() {
		var id int64
		var named bool
		var blob []byte
		if err := rows.Scan(&id, &named, &blob); err != nil {
			rows.Close()
			return fmt.Errorf("cluster library %d: %w", libraryID, err)
		}
		c := byID[id]
		if c == nil {
			c = &cluster{id: id, named: named}
			byID[id] = c
			clusters = append(clusters, c)
		}
		accumulate(c.centroid, decodeEmbedding(blob), &c.centroid, &c.n)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("cluster library %d: %w", libraryID, err)
	}

	// Only unnamed clusters are rebuilt; a named one keeps every face it has.
	unassigned, err := s.unassignedFaces(ctx, libraryID)
	if err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("cluster library %d: %w", libraryID, err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().Unix()
	for _, f := range unassigned {
		best, bestSim := (*cluster)(nil), -1.0
		for _, c := range clusters {
			sim := cosine(f.Embedding, c.centroid)
			/*
			 * A named cluster wins a tie. The comparison is `>` for unnamed and
			 * `>=` for named, which is the whole of "naming a group makes it
			 * more stable": an equally good unnamed candidate does not take a
			 * face away from a person who has been identified.
			 */
			if sim > bestSim || (c.named && sim == bestSim) {
				best, bestSim = c, sim
			}
		}
		if best == nil || bestSim < SameFaceCosine {
			res, err := tx.ExecContext(ctx,
				`INSERT INTO face_cluster (library_id, created_at) VALUES (?, ?)`,
				libraryID, now)
			if err != nil {
				return fmt.Errorf("cluster library %d: %w", libraryID, err)
			}
			id, _ := res.LastInsertId()
			best = &cluster{id: id}
			byID[id] = best
			clusters = append(clusters, best)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE face SET cluster_id = ? WHERE id = ?`, best.id, f.ID); err != nil {
			return fmt.Errorf("cluster library %d: %w", libraryID, err)
		}
		accumulate(best.centroid, f.Embedding, &best.centroid, &best.n)
	}

	/*
	 * Empty unnamed clusters are swept; empty *named* ones are kept.
	 *
	 * A named cluster whose faces have all gone — the photographs deleted, or
	 * the folder marked sensitive — is still somebody the person named, and
	 * deleting it would silently lose that name. It costs one row.
	 */
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM face_cluster
		 WHERE library_id = ?
		   AND (name IS NULL OR name = '')
		   AND NOT EXISTS (SELECT 1 FROM face WHERE face.cluster_id = face_cluster.id)`,
		libraryID); err != nil {
		return fmt.Errorf("cluster library %d: %w", libraryID, err)
	}
	return tx.Commit()
}

// accumulate folds one embedding into a running mean.
func accumulate(cur []float32, add []float32, out *[]float32, n *int) {
	if len(cur) == 0 {
		c := make([]float32, len(add))
		copy(c, add)
		*out, *n = c, 1
		return
	}
	if len(add) != len(cur) {
		// A model change mid-library. Ignoring the odd one out is better than
		// producing a centroid of two different vector spaces.
		return
	}
	k := float32(*n)
	for i := range cur {
		cur[i] = (cur[i]*k + add[i]) / (k + 1)
	}
	*out, *n = cur, *n+1
}

func (s *Store) unassignedFaces(ctx context.Context, libraryID int64) ([]Face, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT f.id, f.embedding
		  FROM face f
		  JOIN media_item m ON m.id = f.item_id
		 WHERE m.library_id = ? AND m.sensitive_effective = 0
		   AND (f.cluster_id IS NULL
		        OR f.cluster_id IN (SELECT id FROM face_cluster
		                             WHERE library_id = ? AND (name IS NULL OR name = '')))
		 ORDER BY f.score DESC, f.id`, libraryID, libraryID)
	if err != nil {
		return nil, fmt.Errorf("unassigned faces: %w", err)
	}
	defer rows.Close()
	out := []Face{}
	for rows.Next() {
		var f Face
		var blob []byte
		if err := rows.Scan(&f.ID, &blob); err != nil {
			return nil, fmt.Errorf("unassigned faces: %w", err)
		}
		f.Embedding = decodeEmbedding(blob)
		out = append(out, f)
	}
	return out, rows.Err()
}

/*
 * NameCluster records what a person called a group, and locks it.
 *
 * An empty name clears both, which is how somebody undoes a name they typed by
 * mistake — the group goes back to being an unnamed cluster and re-clustering
 * may absorb it again.
 */
func (s *Store) NameCluster(ctx context.Context, clusterID int64, name string) error {
	if name == "" {
		_, err := s.db.ExecContext(ctx,
			`UPDATE face_cluster SET name = NULL, name_locked = 0 WHERE id = ?`, clusterID)
		if err != nil {
			return fmt.Errorf("clear cluster %d name: %w", clusterID, err)
		}
		return nil
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE face_cluster SET name = ?, name_locked = 1 WHERE id = ?`, name, clusterID)
	if err != nil {
		return fmt.Errorf("name cluster %d: %w", clusterID, err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return fmt.Errorf("name cluster %d: no such cluster", clusterID)
	}
	return nil
}

// FaceClusters lists a library's people, largest first — an unnamed group of
// forty faces is worth naming before an unnamed group of one.
func (s *Store) FaceClusters(ctx context.Context, libraryID int64) ([]FaceCluster, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.name, c.name_locked, COUNT(f.id) AS n,
		       (SELECT f2.id FROM face f2 WHERE f2.cluster_id = c.id
		         ORDER BY f2.score DESC LIMIT 1),
		       (SELECT f2.item_id FROM face f2 WHERE f2.cluster_id = c.id
		         ORDER BY f2.score DESC LIMIT 1)
		  FROM face_cluster c
		  LEFT JOIN face f ON f.cluster_id = c.id
		 WHERE c.library_id = ?
		 GROUP BY c.id, c.name, c.name_locked
		 ORDER BY n DESC, c.id`, libraryID)
	if err != nil {
		return nil, fmt.Errorf("face clusters: %w", err)
	}
	defer rows.Close()
	out := []FaceCluster{}
	for rows.Next() {
		var c FaceCluster
		var locked int
		if err := rows.Scan(&c.ID, &c.Name, &locked, &c.Count, &c.CoverFaceID, &c.CoverItemID); err != nil {
			return nil, fmt.Errorf("face clusters: %w", err)
		}
		c.NameLocked = locked != 0
		out = append(out, c)
	}
	return out, rows.Err()
}

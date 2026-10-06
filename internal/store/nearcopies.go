package store

import (
	"context"
	"fmt"
	"math/bits"
	"sort"
)

/*
 * Near copies: the same picture resized or re-saved (ADR 0075, 2026-10-06
 * amendment).
 *
 * Not duplicates. A duplicate is the same bytes, and stays the only thing the
 * Duplicates list calls one. This is "probably the same picture", shown apart,
 * and nothing acts on it automatically.
 *
 * The rule is the measured one, on a real library of 3,073 photos where every
 * pair it selected was judged by eye. Two photos are near copies when all four
 * hold:
 *
 * - their search embeddings are close (cosine >= NearCopyMinCosine);
 * - their display copies' difference hashes are close (<= NearCopyMaxDHash);
 * - they were not taken at different moments. A burst, shots a second apart,
 *   sits as close in both of the above as a re-save does, and on that library
 *   bursts were most of the close pairs. The capture time is what separates
 *   them: a re-save keeps the same time or has none;
 * - they are not both screenshots. Two different screenshots of one dark
 *   interface sat closer than real copies did.
 *
 * That selected 32 pairs: 30 plainly the same picture, one uncertain, none a
 * different picture. Crops and edits fall outside it by design.
 */

const (
	// NearCopyMinCosine is the lowest embedding similarity for a near copy.
	// Edited copies (names painted on, a colour filter) sit below it.
	NearCopyMinCosine = 0.93
	// NearCopyMaxDHash is the most bits two display-copy hashes may differ by.
	// A crop of the same picture measured 14 or more.
	NearCopyMaxDHash = 8
)

// screenSizes are the resolutions a screenshot comes out at. Two photos both
// exactly one of these are left out: the measured hard case.
var screenSizes = map[[2]int]bool{
	{1280, 720}: true, {1366, 768}: true, {1600, 900}: true, {1920, 1080}: true,
	{1920, 1200}: true, {2560, 1080}: true, {2560, 1440}: true, {2560, 1600}: true,
	{3440, 1440}: true, {3840, 1600}: true, {3840, 2160}: true, {7680, 2160}: true,
}

// NearCopyGroup is two or more photos that are probably the same picture.
type NearCopyGroup struct {
	// Copies come largest picture first.
	Copies []DuplicateCopy `json:"copies"`
	// Keep is the copy with the most pixels: the one to keep if any is
	// removed. A suggestion; nothing removes anything.
	Keep int64 `json:"keep"`
}

// NearCopies is a picture library's near copies, and how much of it was
// compared.
type NearCopies struct {
	Groups      []NearCopyGroup `json:"groups"`
	ExtraCopies int             `json:"extra_copies"`
	// Pending is how many photos could not be compared yet: no search
	// embedding (the library has not been indexed) or no hash (the photo
	// worker has not read it since the hash was added).
	Pending int `json:"pending"`
}

// nearCandidate is one photo, as the rule reads it.
type nearCandidate struct {
	id    int64
	w, h  int
	taken int64
	size  int64
	path  string
	sha   string
	dhash uint64
	emb   []float32
	album *string
}

// PhotoNearCopies returns a picture library's near copies. Marked and missing
// photos take no part, as in PhotoDuplicates.
func (s *Store) PhotoNearCopies(ctx context.Context, libraryID int64) (NearCopies, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT mi.id, COALESCE(mi.width, 0), COALESCE(mi.height, 0), COALESCE(mi.taken_at, 0),
		       COALESCE(mi.size_bytes, 0), mi.path, COALESCE(h.sha256, ''), h.dhash, e.embedding, g.title
		  FROM media_item mi
		  LEFT JOIN photo_hash h ON h.item_id = mi.id
		  LEFT JOIN photo_embedding e ON e.item_id = mi.id
		  LEFT JOIN media_item g ON g.id = mi.parent_id AND g.kind = 'gallery'
		 WHERE mi.library_id = ? AND mi.kind = 'photo'
		   AND mi.missing = 0 AND mi.sensitive_effective = 0`, libraryID)
	if err != nil {
		return NearCopies{}, fmt.Errorf("photo near copies: %w", err)
	}
	var cs []nearCandidate
	pending := 0
	for rows.Next() {
		var c nearCandidate
		var dh *int64
		var blob []byte
		if err := rows.Scan(&c.id, &c.w, &c.h, &c.taken, &c.size, &c.path, &c.sha, &dh, &blob, &c.album); err != nil {
			rows.Close()
			return NearCopies{}, fmt.Errorf("photo near copies: %w", err)
		}
		if dh == nil || len(blob) == 0 {
			pending++
			continue
		}
		c.dhash = uint64(*dh)
		c.emb = decodeEmbedding(blob)
		cs = append(cs, c)
	}
	if err := rows.Close(); err != nil {
		return NearCopies{}, fmt.Errorf("photo near copies: %w", err)
	}

	groups := nearCopyGroups(cs)
	out := NearCopies{Groups: []NearCopyGroup{}, Pending: pending}
	if len(groups) == 0 {
		return out, nil
	}

	ids := []any{}
	for _, g := range groups {
		for _, c := range g {
			ids = append(ids, c.id)
		}
	}
	itemRows, err := s.db.QueryContext(ctx,
		`SELECT `+itemCols+` FROM media_item WHERE id IN (`+placeholders(len(ids))+`)`, ids...)
	if err != nil {
		return NearCopies{}, fmt.Errorf("photo near copies: %w", err)
	}
	items, err := scanItems(itemRows)
	itemRows.Close()
	if err != nil {
		return NearCopies{}, fmt.Errorf("photo near copies: %w", err)
	}
	byID := make(map[int64]Item, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}
	for _, g := range groups {
		ng := NearCopyGroup{Keep: g[0].id}
		for _, c := range g {
			ng.Copies = append(ng.Copies, DuplicateCopy{Item: byID[c.id], Album: c.album})
		}
		out.Groups = append(out.Groups, ng)
		out.ExtraCopies += len(g) - 1
	}
	return out, nil
}

/*
 * nearCopyGroups applies the rule to every pair and joins the pairs into
 * groups. Pure, so the rule is tested without a database.
 *
 * The hash is compared first because it is a single popcount, and on a real
 * library it rules out all but a few hundred of several million pairs; the
 * embedding's cosine, 512 multiplications, runs only on those.
 *
 * Exact copies (the same SHA-256) are not paired with each other: they are the
 * Duplicates list's. A resized copy of a photo that also has an exact copy
 * joins both, and the group shows all three.
 */
func nearCopyGroups(cs []nearCandidate) [][]nearCandidate {
	parent := make([]int, len(cs))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	paired := make([]bool, len(cs))
	for i := range cs {
		for j := i + 1; j < len(cs); j++ {
			if nearCopy(&cs[i], &cs[j]) {
				parent[find(i)] = find(j)
				paired[i], paired[j] = true, true
			}
		}
	}

	byRoot := map[int][]nearCandidate{}
	for i := range cs {
		if paired[i] {
			r := find(i)
			byRoot[r] = append(byRoot[r], cs[i])
		}
	}
	groups := make([][]nearCandidate, 0, len(byRoot))
	for _, g := range byRoot {
		// Largest picture first: that is the one to keep. Ties go to the
		// larger file, then the path, so the order is the same every time.
		sort.Slice(g, func(a, b int) bool {
			pa, pb := g[a].w*g[a].h, g[b].w*g[b].h
			if pa != pb {
				return pa > pb
			}
			if g[a].size != g[b].size {
				return g[a].size > g[b].size
			}
			return g[a].path < g[b].path
		})
		groups = append(groups, g)
	}
	sort.Slice(groups, func(a, b int) bool {
		if len(groups[a]) != len(groups[b]) {
			return len(groups[a]) > len(groups[b])
		}
		return groups[a][0].path < groups[b][0].path
	})
	return groups
}

// nearCopy is the measured rule for one pair.
func nearCopy(a, b *nearCandidate) bool {
	if a.sha != "" && a.sha == b.sha {
		return false
	}
	if bits.OnesCount64(a.dhash^b.dhash) > NearCopyMaxDHash {
		return false
	}
	if a.taken > 0 && b.taken > 0 && a.taken != b.taken {
		return false
	}
	if a.w == b.w && a.h == b.h && screenSizes[[2]int{a.w, a.h}] {
		return false
	}
	return cosine(a.emb, b.emb) >= NearCopyMinCosine
}

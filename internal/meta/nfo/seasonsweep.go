package nfo

import (
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"lancast/internal/meta"
)

/*
 * RemoveStraySeasonSidecar deletes the `tvshow.nfo` that an earlier build
 * wrote into a season folder (R08-5), and nothing else.
 *
 * It answers true only when it removed a file. Every test below has to pass
 * before anything is deleted, because this reaches into the user's library and
 * a sidecar is their data unless it can be shown to be ours:
 *
 *   - the root is `<movie>`: rootElement had no season case, so that is what
 *     LANcast wrote, and a real show's file says `<tvshow>`;
 *   - it carries LANcast's marker;
 *   - every element in it is one applyRecord writes, so no other tool has put
 *     anything there that deleting the file would lose;
 *   - the marker does not prove a person edited it since, by the same test
 *     Read uses to tell our mirror from a human's change.
 *
 * Anything that fails one of them is left exactly as it is.
 */
func RemoveStraySeasonSidecar(seasonDir string) (bool, error) {
	if seasonDir == "" || IsSyntheticPath(seasonDir) {
		return false, nil
	}
	target := filepath.Join(seasonDir, "tvshow.nfo")
	raw, err := os.ReadFile(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("nfo: read %s: %w", target, err)
	}
	var root node
	if xml.Unmarshal(raw, &root) != nil {
		return false, nil
	}
	if !isOwnSeasonSidecar(&root) {
		return false, nil
	}
	if err := os.Remove(target); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("nfo: remove %s: %w", target, err)
	}
	return true, nil
}

// ownElements is everything applyRecord and Write put into a file they create.
var ownElements = map[string]bool{
	"title": true, "sorttitle": true, "plot": true, "mpaa": true, "year": true,
	"rating": true, "runtime": true, "premiered": true, "aired": true,
	"showtitle": true, "season": true, "episode": true,
	"genre": true, "actor": true, "director": true,
	MarkerElement: true,
}

func isOwnSeasonSidecar(root *node) bool {
	if !strings.EqualFold(root.XMLName.Local, "movie") {
		return false
	}
	marker, found := root.child(MarkerElement)
	if !found {
		return false
	}
	for _, c := range root.Nodes {
		if !ownElements[strings.ToLower(c.XMLName.Local)] {
			return false
		}
	}
	rec := recordFrom(root, meta.KindSeason)
	if rec == nil {
		// Nothing in it but the marker: ours, and empty.
		return true
	}
	if !provesUserEdit(marker.attr("hash"), rec) {
		return true
	}
	// The whole-record hash differs. Per-field digests can still show that
	// nothing a person would change was changed; without them, keep the file.
	written := decodeDigests(marker.attr("fields"))
	return len(written) > 0 && editedFields(rec, written) == nil
}

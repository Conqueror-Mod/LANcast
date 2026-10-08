// Package saves keeps a person's game saves as files under the data
// directory (ADR 0073).
//
// Files rather than blobs in SQLite, as the ADR decided: a save state can be
// tens of megabytes, a backup of the database should not grow with every
// game somebody plays, and a file can be replaced atomically by a rename.
//
// The rule this package exists to keep: **a save is never overwritten without
// the previous copy being kept.** Two machines saving the same slot, or a
// core writing a corrupt state, must cost at most one step back.
package saves

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
)

// ErrTooLarge is returned when a save exceeds MaxBytes.
var ErrTooLarge = errors.New("save is larger than any core writes")

// MaxBytes bounds one save. An N64 save state with the expansion pak is about
// 16MB and a PlayStation one about 4MB; this leaves room for a core that
// compresses nothing without letting a client fill the disk with one request.
const MaxBytes = 64 << 20

/*
 * Slots.
 *
 * `sram` is the game's own save — battery RAM, a memory card — which every
 * core version can read. `auto` is the state written when a game is closed,
 * so it can be resumed where it was left. `state-0` to `state-9` are the
 * numbered save states a person chooses. States are tied to the core that
 * wrote them; save RAM is not.
 */
var slotRE = regexp.MustCompile(`^(sram|auto|state-[0-9])$`)

// ValidSlot reports whether slot names a slot.
func ValidSlot(slot string) bool { return slotRE.MatchString(slot) }

// IsState reports whether a slot holds a save state, which records the core
// and version that wrote it and is refused by any other.
func IsState(slot string) bool { return slot != "sram" }

// Files is the save directory.
type Files struct {
	Dir string // <data dir>/saves
}

/*
 * userDir is where one person's saves live.
 *
 * Named by a hash of the account id rather than the id itself: the id is a
 * string the store chose ("u_3f9", "local"), and a path built from one is a
 * path built from data — hashing makes every one of them a fixed-shape name
 * with no separators in it, whatever an id ever comes to contain.
 */
func (f Files) userDir(userID string) string {
	sum := sha256.Sum256([]byte(userID))
	return filepath.Join(f.Dir, hex.EncodeToString(sum[:8]))
}

func (f Files) path(userID string, itemID int64, slot string, previous bool) string {
	name := slot + ".sav"
	if previous {
		name = slot + ".prev"
	}
	return filepath.Join(f.userDir(userID), strconv.FormatInt(itemID, 10), name)
}

// Written is what a write produced.
type Written struct {
	Size   int64
	SHA256 string
}

/*
 * Write stores a save, keeping the one it replaces as the previous copy.
 *
 * The new bytes go to a temporary file in the same directory, are synced, and
 * only then does the current file become the previous one and the new one
 * become current — two renames, so at no point is there no current save and
 * at no point is a half-written file under a real name. A write that fails or
 * is too large leaves both copies exactly as they were.
 */
func (f Files) Write(userID string, itemID int64, slot string, r io.Reader) (Written, error) {
	if !ValidSlot(slot) {
		return Written{}, fmt.Errorf("invalid slot %q", slot)
	}
	cur := f.path(userID, itemID, slot, false)
	dir := filepath.Dir(cur)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Written{}, err
	}
	tmp, err := os.CreateTemp(dir, slot+"-*.part")
	if err != nil {
		return Written{}, err
	}
	ok := false
	defer func() {
		if !ok {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()

	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(r, MaxBytes+1))
	if err != nil {
		return Written{}, err
	}
	if n > MaxBytes {
		return Written{}, ErrTooLarge
	}
	if err := tmp.Sync(); err != nil {
		return Written{}, err
	}
	if err := tmp.Close(); err != nil {
		return Written{}, err
	}

	prev := f.path(userID, itemID, slot, true)
	if _, err := os.Stat(cur); err == nil {
		if err := os.Rename(cur, prev); err != nil {
			return Written{}, err
		}
	}
	if err := os.Rename(tmp.Name(), cur); err != nil {
		// Put the previous save back rather than leave the slot empty.
		os.Rename(prev, cur)
		return Written{}, err
	}
	ok = true
	return Written{Size: n, SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}

// Open opens a save, or its previous copy. A missing save is os.ErrNotExist.
func (f Files) Open(userID string, itemID int64, slot string, previous bool) (*os.File, error) {
	if !ValidSlot(slot) {
		return nil, os.ErrNotExist
	}
	return os.Open(f.path(userID, itemID, slot, previous))
}

// RemoveUser deletes every save one person has, when their account goes.
func (f Files) RemoveUser(userID string) error {
	return os.RemoveAll(f.userDir(userID))
}

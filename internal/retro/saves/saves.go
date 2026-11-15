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
	"sync"
)

// ErrTooLarge is returned when a save exceeds MaxBytes.
var ErrTooLarge = errors.New("save is larger than any core writes")

/*
 * ErrConflict is returned by WriteIf when the slot no longer holds the save
 * the writer started from (R12-4).
 *
 * A player that could not fetch the save, or that loaded one before another
 * machine replaced it, holds save RAM that is not a continuation of what is on
 * the server. Many cartridges write save RAM at boot, so "the newer write wins"
 * let a blank game replace a real save within seconds of starting, and a second
 * write rotated the real one out of `.prev` too.
 */
var ErrConflict = errors.New("the save changed since it was loaded")

// ErrNoSave is what a client reports for an empty slot, so a caller can tell
// "there is no save" from "the save could not be fetched": the first is safe to
// write over, the second is not.
var ErrNoSave = errors.New("no save in this slot")

// NoSave is the based-on value of a writer that loaded an empty slot. It is
// not a hash, so it can never match a save that exists.
const NoSave = "none"

// ValidBasedOn reports whether s can be a WriteIf precondition: empty (none),
// NoSave, or a lower-case hex SHA-256 as Written and the ETag give it.
func ValidBasedOn(s string) bool {
	if s == "" || s == NoSave {
		return true
	}
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// swap serialises the compare and the renames, so two writers based on the
// same save cannot both pass the check. Held for a hash and two renames, never
// across a request body.
var swap sync.Mutex

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
	return f.WriteIf(userID, itemID, slot, "", r)
}

/*
 * WriteIf is Write with a precondition: basedOn is the SHA-256 of the save the
 * writer loaded, or NoSave when it loaded an empty slot. When the slot holds
 * anything else by the time the new bytes are ready, nothing changes and the
 * error is ErrConflict. An empty basedOn is no precondition, as Write.
 *
 * Compared with the file rather than the database's record of it, because the
 * file is what the write would rotate away.
 */
func (f Files) WriteIf(userID string, itemID int64, slot, basedOn string, r io.Reader) (Written, error) {
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

	swap.Lock()
	defer swap.Unlock()
	if basedOn != "" {
		now, err := fileSHA256(cur)
		if err != nil {
			return Written{}, err
		}
		if now != basedOn {
			return Written{}, ErrConflict
		}
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

// fileSHA256 is the hex SHA-256 of the file at p, or NoSave when there is none.
func fileSHA256(p string) (string, error) {
	fh, err := os.Open(p)
	if errors.Is(err, os.ErrNotExist) {
		return NoSave, nil
	}
	if err != nil {
		return "", err
	}
	defer fh.Close()
	h := sha256.New()
	if _, err := io.Copy(h, fh); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
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

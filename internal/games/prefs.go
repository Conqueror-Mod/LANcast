package games

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

/*
 * Hidden and favourite games, per client.
 *
 * Here rather than in server settings for the reason ADR 0066 gives: the server
 * has no games, so it can have no opinion about which of them somebody likes.
 * This is a fact about one person's desktop, exactly like desktopprefs, and it
 * is written the same way — whole values, atomic replace, a missing file
 * meaning defaults.
 *
 * Stored as ids rather than as a flag on the game, because the game is not ours
 * to write to: the manifest belongs to Steam, and an uninstall must not lose
 * the fact that this title was hidden when it comes back.
 */

// PrefsFileName is the file inside the client's directory.
const PrefsFileName = "games.json"

// Prefs is the per-client games state.
type Prefs struct {
	Hidden     []string `json:"hidden"`
	Favourites []string `json:"favourites"`
	/*
	 * Displays is which screen each game was asked to open on, keyed on app id
	 * and holding a display *device name* (ADR 0066's amendment).
	 *
	 * Omitted when empty so that a file written before this existed, and a
	 * desk with one monitor, both stay the two lines they were.
	 */
	Displays map[string]string `json:"displays,omitempty"`
}

// IsHidden reports whether an appid is hidden.
func (p Prefs) IsHidden(id string) bool { return contains(p.Hidden, id) }

// IsFavourite reports whether an appid is a favourite.
func (p Prefs) IsFavourite(id string) bool { return contains(p.Favourites, id) }

// Set records both flags for one game. Both at once rather than two setters,
// so the page cannot half-apply a change — the same reasoning as
// lancastDesktopSet taking every preference it writes.
func (p *Prefs) Set(id string, hidden, favourite bool) {
	p.Hidden = toggle(p.Hidden, id, hidden)
	p.Favourites = toggle(p.Favourites, id, favourite)
}

func contains(list []string, id string) bool {
	for _, v := range list {
		if v == id {
			return true
		}
	}
	return false
}

// toggle adds or removes id, keeping the list sorted and free of duplicates so
// the file on disk is stable between writes and diffable by a human.
func toggle(list []string, id string, want bool) []string {
	out := make([]string, 0, len(list)+1)
	for _, v := range list {
		if v != id {
			out = append(out, v)
		}
	}
	if want {
		out = append(out, id)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

// LoadPrefs reads the games preferences from dir.
//
// A missing file is the first run and means no hidden and no favourites. A
// malformed file is recovered from the same way desktopprefs recovers: defaults
// returned and the caller told, because refusing to list anybody's games over
// an unparsable list of hidden ones trades the feature for a footnote.
func LoadPrefs(dir string) (Prefs, error) {
	if dir == "" {
		return Prefs{}, nil
	}
	raw, err := os.ReadFile(filepath.Join(dir, PrefsFileName))
	if os.IsNotExist(err) {
		return Prefs{}, nil
	}
	if err != nil {
		return Prefs{}, fmt.Errorf("games preferences: %w", err)
	}
	var p Prefs
	if err := json.Unmarshal(raw, &p); err != nil {
		return Prefs{}, fmt.Errorf("games preferences: %s is not readable: %w", PrefsFileName, err)
	}
	return p, nil
}

// SavePrefs writes the games preferences to dir, creating it if needed.
func SavePrefs(dir string, p Prefs) error {
	if dir == "" {
		return fmt.Errorf("games preferences: no directory to write to")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("games preferences: %w", err)
	}
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("games preferences: %w", err)
	}
	raw = append(raw, '\n')

	final := filepath.Join(dir, PrefsFileName)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return fmt.Errorf("games preferences: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("games preferences: %w", err)
	}
	return nil
}

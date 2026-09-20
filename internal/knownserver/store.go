package knownserver

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// FileName is the record's name in the client's own data directory.
//
// The client's directory and not the server's, because this is a fact about
// this installation of the client: which servers *it* has been pointed at. A
// server's data directory belongs to a server and may not even be on this
// machine.
const FileName = "servers.json"

/*
 * Load reads the record from dir.
 *
 * A missing file is the first run and returns an empty list. A malformed file
 * returns an empty list *and* an error, the same bargain desktopprefs strikes:
 * the caller is told so the failure has a voice, but an unreadable record does
 * not stop the app opening. What it does mean is that every server is unknown
 * again, so the next connection asks -- which is the safe direction for this
 * particular file to fail in, since the alternative to asking is trusting
 * something on the strength of bytes that would not parse.
 */
func Load(dir string) (List, error) {
	if dir == "" {
		return List{}, nil
	}
	raw, err := os.ReadFile(filepath.Join(dir, FileName))
	if os.IsNotExist(err) {
		return List{}, nil
	}
	if err != nil {
		return List{}, fmt.Errorf("known servers: %w", err)
	}
	var l List
	if err := json.Unmarshal(raw, &l); err != nil {
		return List{}, fmt.Errorf("known servers: %s is not readable: %w", FileName, err)
	}
	return l, nil
}

// Save writes the record whole and replaces it atomically.
//
// Whole, because a half-written trust store that then fails to parse is a
// prompt to re-trust every server -- and a person met with a fingerprint
// prompt they did not expect has no way to tell that from the thing the prompt
// is for.
func Save(dir string, l List) error {
	if dir == "" {
		return fmt.Errorf("known servers: no directory to write to")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("known servers: %w", err)
	}
	raw, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return fmt.Errorf("known servers: %w", err)
	}
	raw = append(raw, '\n')

	final := filepath.Join(dir, FileName)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("known servers: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("known servers: %w", err)
	}
	return nil
}

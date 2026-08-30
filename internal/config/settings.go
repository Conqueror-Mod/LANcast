package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Settings is the user-editable runtime configuration.
//
// The TMDB key lives here rather than in the database because it is a secret:
// the file is written 0600, and the API never echoes the value back.
type Settings struct {
	TMDBKey string `json:"tmdb_key,omitempty"`
	// OpenSubtitlesKey enables subtitle search. Optional, like the TMDB key:
	// without it LANcast still serves embedded and sidecar subtitles.
	OpenSubtitlesKey string `json:"opensubtitles_key,omitempty"`
	// OMDbKey enables external ratings (Rotten Tomatoes / Metacritic / IMDb via
	// OMDb, ADR 0019). Optional and write-only like the other keys; without it
	// the rating pass never runs and nothing leaves the machine.
	OMDbKey    string  `json:"omdb_key,omitempty"`
	RatePerSec float64 `json:"rate_per_sec,omitempty"`
	WriteNFO   bool    `json:"write_nfo"`
	AutoEnrich bool    `json:"auto_enrich"`
	// UpdateCheck asks the project's releases endpoint whether a newer version
	// exists. On by default: an update nobody hears about is one nobody
	// installs, and the check is a plain GET carrying no identifier. Off stops
	// it entirely — nothing else changes.
	UpdateCheck bool `json:"update_check"`

	// ---- library and playback rules -------------------------------------
	//
	// These four are the server's opinion about what a client shows, and they
	// live here rather than in the client for the reason every rule in LANcast
	// does: the server owns truth, and a household with a phone, a browser and
	// a TV must not have three answers to "have I watched this".

	// WatchedThreshold is the percentage of an item's duration past which it
	// counts as watched. Plex's equivalent exists because credits are not the
	// film: stopping at 96% is finishing it, and a shelf that keeps offering
	// the last ninety seconds back is a shelf nobody clears.
	//
	// Applied server-side on every progress write, so a client that never
	// bothers to send `watched` still gets correct state, and a client that
	// sends it early cannot un-finish something.
	WatchedThreshold int `json:"watched_threshold,omitempty"`

	// ContinueWeeks drops anything untouched for this many weeks off the
	// Continue Watching shelf. Zero means never drop anything.
	//
	// A shelf is a promise that these are the things you are in the middle of.
	// The half-hour of a documentary you abandoned in March is not that, and it
	// pushes out the thing you paused last night.
	ContinueWeeks int `json:"continue_weeks,omitempty"`

	// ContinueLimit caps how many items that shelf holds.
	ContinueLimit int `json:"continue_limit,omitempty"`

	// AllowMediaDeletion permits deleting media *files from disk* through the
	// API. Off makes `DELETE /api/items/{id}?mode=delete` a 403; removing a
	// title from the library (mode=ignore) is unaffected, because that touches
	// no file.
	//
	// On by default, matching the behaviour that already shipped — turning it
	// off is an operator saying "this server does not delete my media", which
	// is a thing a person should be able to say and could not before.
	AllowMediaDeletion bool `json:"allow_media_deletion"`

	/*
	 * EmptyTrashOnScan removes rows whose files are gone, after a scan that was
	 * in a position to know they are gone.
	 *
	 * Off by default, and that is the decision rather than caution. "Scanning
	 * marks missing, never deletes" exists because an unmounted drive must not
	 * destroy library data, and a missing row is not junk — it is the record of
	 * a film somebody watched, with its position, rating and history. Somebody
	 * who wants that tidied can ask; nobody gets it by not reading a page.
	 *
	 * The conditions under which a scan may act on it live in
	 * `scan.MayEmptyTrash`, which is the half that knows whether the walk could
	 * see the library at all.
	 */
	EmptyTrashOnScan bool `json:"empty_trash_on_scan"`

	// ScanIntervalHours rescans every library on a timer. Zero is off, which is
	// the default: LANcast scans when asked and when a library is created, and
	// a periodic scan is for a server whose media arrives by other means —
	// a downloader, a sync job, another machine's writes.
	ScanIntervalHours int `json:"scan_interval_hours,omitempty"`

	// AuditRetentionDays drops audit events older than this many days. Zero
	// keeps them for ever, which is a real answer for somebody running this
	// where the audit trail is the point.
	//
	// It exists because audit_event was append-only with no ceiling: a server
	// left running for a year kept every row, and nothing ever looked at the
	// age of one again. This is the same judgement ContinueWeeks makes one
	// field up — age is relevance for some records and not for others, and
	// saying which is the operator's call.
	//
	// Ninety days by default. Long enough that "what happened to that library
	// last month" is still answerable, short enough that the table stops being
	// a permanent record of every scan the server has ever run.
	AuditRetentionDays int `json:"audit_retention_days,omitempty"`

	// DebugLogging raises the server's log level to debug, at runtime and
	// across restarts. Persisted rather than a one-shot toggle because the
	// faults worth debug logging for are usually the intermittent ones: turning
	// it on, restarting, and losing the setting is how a person ends up
	// reproducing a bug three times.
	DebugLogging bool `json:"debug_logging"`

	// HardwareEncoder is "auto", "off", or a specific ffmpeg encoder name.
	// Auto takes the fastest encoder that passed a real test encode.
	HardwareEncoder string `json:"hardware_encoder,omitempty"`

	// FFmpegDir is the directory holding ffmpeg and ffprobe. It exists because a
	// service account's PATH does not include a per-user ffmpeg install, which
	// otherwise leaves every item unprobed and every file direct-played (ADR
	// 0016). `service install` records what it can see; empty means "find them on
	// PATH", which is the normal interactive case.
	FFmpegDir string `json:"ffmpeg_dir,omitempty"`

	// PasswordHash is the bcrypt hash guarding the instance. Empty means the
	// server is unconfigured and will bind to loopback only.
	PasswordHash string `json:"password_hash,omitempty"`

	// TLSCertFile and TLSKeyFile point at a PEM certificate and private key the
	// operator supplies (bring-your-own-cert). When both are set, LANcast serves
	// HTTPS with them. When empty and the server binds beyond loopback, a
	// self-signed certificate is generated and persisted instead (ADR 0014).
	TLSCertFile string `json:"tls_cert_file,omitempty"`
	TLSKeyFile  string `json:"tls_key_file,omitempty"`
}

// CustomTLS reports whether the operator supplied their own certificate and key.
func (s Settings) CustomTLS() bool { return s.TLSCertFile != "" && s.TLSKeyFile != "" }

// Secured reports whether a password has been set.
func (s Settings) Secured() bool { return s.PasswordHash != "" }

// Defaults returns the settings a fresh install starts with.
//
// AutoEnrich is on because metadata arriving by itself is the expected
// behavior; it is a no-op without a key. WriteNFO is off because writing into
// someone's media folders is not something to do unasked.
func Defaults() Settings {
	return Settings{RatePerSec: 5, WriteNFO: false, AutoEnrich: true,
		UpdateCheck: true, HardwareEncoder: "auto",
		// 90% is the long-standing convention and the value Plex ships; the
		// last tenth of a film is credits often enough that finishing it is the
		// safer default. 16 weeks and 40 items match it too — not out of
		// deference, but because they are the numbers a decade of people have
		// found unsurprising, and an unsurprising default is the whole job of a
		// default.
		WatchedThreshold: 90, ContinueWeeks: 16, ContinueLimit: 40,
		AllowMediaDeletion: true, ScanIntervalHours: 0,
		AuditRetentionDays: 90}
}

// SettingsStore reads and writes the settings file.
type SettingsStore struct {
	path string
	mu   sync.RWMutex
	cur  Settings
}

// LoadSettings opens (or initializes) the settings file in dir.
func LoadSettings(dir string) (*SettingsStore, error) {
	s := &SettingsStore{
		path: filepath.Join(dir, "config.json"),
		cur:  Defaults(),
	}

	raw, err := os.ReadFile(s.path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return s, nil // defaults; nothing written until something is set
	case err != nil:
		return nil, fmt.Errorf("read settings: %w", err)
	}

	loaded := Defaults()
	if err := json.Unmarshal(raw, &loaded); err != nil {
		return nil, fmt.Errorf("parse settings %s: %w", s.path, err)
	}
	if loaded.RatePerSec <= 0 {
		loaded.RatePerSec = Defaults().RatePerSec
	}
	clamp(&loaded)
	s.cur = loaded
	return s, nil
}

// Get returns a copy of the current settings.
func (s *SettingsStore) Get() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cur
}

// Set replaces the settings and persists them with 0600 permissions.
func (s *SettingsStore) Set(next Settings) error {
	if next.RatePerSec <= 0 {
		next.RatePerSec = Defaults().RatePerSec
	}
	clamp(&next)

	s.mu.Lock()
	defer s.mu.Unlock()

	body, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return fmt.Errorf("encode settings: %w", err)
	}

	// Write via a temp file so a crash never leaves a truncated config, and
	// create it 0600 from the outset rather than chmod-ing after the secret is
	// already on disk.
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("replace settings: %w", err)
	}

	s.cur = next
	return nil
}

// clamp replaces out-of-range numbers with their defaults.
//
// Here rather than only in the API handler, because the file is editable by
// hand and a hand-edited zero must not become a rule. A WatchedThreshold of 0
// would mark everything watched the instant it started playing — the setting
// failing open in the most destructive direction available to it, against
// state a person cannot easily reconstruct.
//
// Zero is meaningful for ContinueWeeks (never expire), ScanIntervalHours (off)
// and AuditRetentionDays (keep for ever), so those are floors rather than
// replacements. AuditRetentionDays matters most here: reading a hand-edited
// negative as a cutoff would delete the whole audit trail on the next pass,
// and there is nowhere to recover it from.
func clamp(s *Settings) {
	d := Defaults()
	if s.WatchedThreshold < 50 || s.WatchedThreshold > 100 {
		s.WatchedThreshold = d.WatchedThreshold
	}
	if s.ContinueWeeks < 0 {
		s.ContinueWeeks = d.ContinueWeeks
	}
	if s.ContinueLimit <= 0 || s.ContinueLimit > 100 {
		s.ContinueLimit = d.ContinueLimit
	}
	if s.ScanIntervalHours < 0 {
		s.ScanIntervalHours = 0
	}
	if s.AuditRetentionDays < 0 {
		s.AuditRetentionDays = 0
	}
}

// ContinueCutoff is the unix time before which a paused item leaves the
// Continue Watching shelf, or 0 when nothing ever expires.
func (s Settings) ContinueCutoff(now time.Time) int64 {
	if s.ContinueWeeks <= 0 {
		return 0
	}
	return now.AddDate(0, 0, -7*s.ContinueWeeks).Unix()
}

// Watched reports whether this position finishes an item of this duration.
//
// False when the duration is unknown — an unprobed file, or a live stream —
// because a percentage of nothing is not a fact about anything, and guessing
// here marks things watched that were never played.
func (s Settings) Watched(positionMS, durationMS int64) bool {
	if durationMS <= 0 || positionMS <= 0 {
		return false
	}
	return positionMS*100 >= durationMS*int64(s.WatchedThreshold)
}

// Path is the settings file location, for diagnostics.
func (s *SettingsStore) Path() string { return s.path }

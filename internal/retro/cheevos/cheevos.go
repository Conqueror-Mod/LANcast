// Package cheevos is the desktop player's RetroAchievements runtime
// (ADR 0111): rcheevos, RetroAchievements' own library, loaded as
// rcheevos.dll, evaluating a game's achievements against its memory every
// frame.
//
// rcheevos builds every request; this package sends none of them itself. It
// hands each to a Sender, which posts it to the person's LANcast server with
// the game's ticket, and the server sends it on with the person's token.
// The player signs in as ra.Placeholder and never holds the token.
//
// Softcore only. Every call into the DLL is made on the core's thread, and
// every answer from the network is handed back on it, in Frame or Idle.
package cheevos

import (
	"context"
	"strings"

	"lancast/internal/media"
	"lancast/internal/retro/ra"
)

// Sender posts one rcheevos request to the LANcast server and returns
// RetroAchievements' status and answer. An error means it should be tried
// again later (nothing usable came back); a refusal is an answer, with a
// status and a body in RetroAchievements' own shape.
type Sender func(ctx context.Context, body, userAgentClause string) (status int, answer []byte, err error)

// Event is something the person should see while playing.
type Event struct {
	// Kind is one of: ready (achievements loaded: Title, Total, Unlocked),
	// none (this game has no achievements, or is not recognised), off
	// (earning is off or not connected: Text says why), unlocked, mastered,
	// progress, progress-hide, offline (an unlock is waiting to be sent),
	// online (it went), problem (RetroAchievements refused something).
	Kind        string
	ID          uint32
	Title       string
	Description string
	Badge       string // LANcast's own URL for the picture, or ""
	Points      uint32
	Progress    string // "3/10", for progress
	Text        string
	Total       uint32
	Unlocked    uint32
}

// Achievement is one of the loaded game's achievements, for the menu.
type Achievement struct {
	ID          uint32 `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Badge       string `json:"badge,omitempty"`
	Points      uint32 `json:"points"`
	// State is locked, unlocked or unsupported (the set uses something this
	// core does not provide).
	State    string `json:"state"`
	Progress string `json:"progress,omitempty"`
}

// Summary is the loaded game's set, for the menu's Achievements page.
type Summary struct {
	GameID         uint32        `json:"game_id"`
	Title          string        `json:"title"`
	Badge          string        `json:"badge,omitempty"`
	Total          uint32        `json:"total"`
	Unlocked       uint32        `json:"unlocked"`
	Points         uint32        `json:"points"`
	PointsUnlocked uint32        `json:"points_unlocked"`
	RichPresence   string        `json:"rich_presence,omitempty"`
	Achievements   []Achievement `json:"achievements"`
}

// Console is RetroAchievements' console id for a LANcast platform, or 0 for
// one it has none for. rc_consoles.h, v12.5.
func Console(platform string) uint32 {
	switch platform {
	case media.PlatformGenesis:
		return 1
	case media.PlatformN64:
		return 2
	case media.PlatformSNES:
		return 3
	case media.PlatformGB:
		return 4
	case media.PlatformGBA:
		return 5
	case media.PlatformGBC:
		return 6
	case media.PlatformNES:
		return 7
	case media.PlatformSMS:
		return 11
	case media.PlatformPS1:
		return 12
	}
	return 0
}

// mediaURL turns a RetroAchievements picture URL into LANcast's own route
// for it (ADR 0090 §5), so the page never fetches from RetroAchievements.
// Anything else is dropped.
func mediaURL(u string) string {
	p, ok := strings.CutPrefix(u, ra.MediaURL)
	if !ok || !ra.MediaPath(p) {
		return ""
	}
	return "/api/retroachievements/media" + p
}

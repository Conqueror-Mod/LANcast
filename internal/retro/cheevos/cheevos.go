// Package cheevos is the desktop player's binding to rcheevos,
// RetroAchievements' C library, built with LANcast's shim into
// lancast-cheevos.dll (ADR 0112; third_party/rcheevos).
//
// The DLL is loaded by full path through syscall, with no cgo, as libmpv and
// the emulator cores are. rcheevos reads the game's memory many times a frame,
// so those reads stay in C, against regions registered once per game
// (SetMemory). Only rare things cross into Go: an HTTP request rcheevos wants
// made, an event, a log line, and the end of a login or a game load.
//
// This package makes no HTTP calls. Every request goes to Handler.Request,
// and the desktop player sends it through the LANcast server (ADR 0112): the
// server holds the RetroAchievements token and forwards only the calls it
// allows.
//
// One client per process, because the shim keeps one. The desktop player
// runs one game at a time.
package cheevos

import (
	"errors"
	"os"
	"path/filepath"
)

// DLLName is the file the release places beside LANcast.exe.
const DLLName = "lancast-cheevos.dll"

// DLLPath is the one place the DLL may be loaded from: beside the running
// executable, by full path. A bare name would be found by the DLL search
// order, which includes the working directory, so a planted DLL beside a
// game would run inside the player (the same rule internal/mpv keeps).
// "" when the executable cannot be found.
func DLLPath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(exe), DLLName)
}

// InterfaceVersion is the shim's LC_INTERFACE_VERSION. Open refuses a DLL
// that reports another, so a mismatched DLL fails at load, not mid-game.
const InterfaceVersion = 2

// ErrUnsupported is Open's answer where the DLL cannot be loaded at all.
var ErrUnsupported = errors.New("cheevos: achievements run on Windows only")

// Console is a RetroAchievements console id (rc_consoles.h).
type Console uint32

// The consoles LANcast's desktop cores play (ADR 0112).
const (
	MegaDrive    Console = 1
	Nintendo64   Console = 2
	SNES         Console = 3
	GameBoy      Console = 4
	GameBoyAdv   Console = 5
	GameBoyColor Console = 6
	NES          Console = 7
	MasterSystem Console = 11
	PlayStation  Console = 12
	GameGear     Console = 15
	WonderSwan   Console = 53 // and the WonderSwan Color: one console to RA
)

// EventType is an RC_CLIENT_EVENT_* value.
type EventType uint32

const (
	EventAchievementTriggered    EventType = 1
	EventLeaderboardStarted      EventType = 2
	EventLeaderboardFailed       EventType = 3
	EventLeaderboardSubmitted    EventType = 4
	EventChallengeIndicatorShow  EventType = 5
	EventChallengeIndicatorHide  EventType = 6
	EventProgressIndicatorShow   EventType = 7
	EventProgressIndicatorHide   EventType = 8
	EventProgressIndicatorUpdate EventType = 9
	EventTrackerShow             EventType = 10
	EventTrackerHide             EventType = 11
	EventTrackerUpdate           EventType = 12
	EventLeaderboardScoreboard   EventType = 13
	EventReset                   EventType = 14
	EventGameCompleted           EventType = 15
	EventServerError             EventType = 16
	EventDisconnected            EventType = 17
	EventReconnected             EventType = 18
	EventSubsetCompleted         EventType = 19
)

// Event is something that happened in play, flattened from rcheevos' event.
// Which fields mean anything depends on Type: an achievement fills all of
// them; a tracker only ID and Text.
type Event struct {
	Type    EventType
	ID      uint32
	Points  uint32
	Percent float32
	Title   string
	// Description is an achievement's or leaderboard's.
	Description string
	// Badge is an achievement's badge name, "12345".
	Badge string
	// Text is measured progress, a tracker's display, a submitted score, or
	// a server error's message, by Type.
	Text string
}

// GameInfo is the loaded game, for the game menu.
type GameInfo struct {
	ID    uint32
	Title string
	// Badge is the game's badge name, "112233".
	Badge                  string
	Total, Unlocked        uint32 // core achievements
	Points, PointsUnlocked uint32
}

// ListedState is where one achievement stands.
type ListedState uint32

const (
	Locked      ListedState = 0
	Unlocked    ListedState = 1
	Unsupported ListedState = 2 // the set uses something this core does not provide
)

// Listed is one of the loaded game's achievements, unlocked first.
type Listed struct {
	ID, Points  uint32
	State       ListedState
	Percent     float32
	Title       string
	Description string
	Badge       string // badge name, "12345"
	Progress    string // "3/10", or ""
}

// Op names what a Done call is the end of.
type Op uint32

const (
	OpLogin    Op = 1
	OpLoadGame Op = 2
)

// Result codes rcheevos reports through Done (rc_error.h). Zero is success.
const (
	OK                 = 0 // RC_OK
	InvalidState       = -25
	APIFailure         = -27
	LoginRequired      = -28
	NoGameLoaded       = -29
	Aborted            = -31
	NoResponse         = -32
	InvalidCredentials = -34
	ExpiredToken       = -35
	InsufficientBuffer = -36
)

// Response statuses for Respond, besides an HTTP status code.
const (
	// StatusRetry is a failure worth retrying: no connection, a timeout.
	// rcheevos keeps an unlock and tries it again.
	StatusRetry = -2
	// StatusFail is a failure that is not.
	StatusFail = -1
)

// Request is an HTTP call rcheevos wants made. PostData is empty for a GET.
type Request struct {
	ID          uint32
	URL         string
	PostData    string
	ContentType string
}

// Handler receives what crosses from rcheevos into Go. Every method is
// called on the goroutine that was in the DLL at the time — inside Login,
// LoadGame, DoFrame, Idle or Respond — so each must return promptly and must
// not call back into the Client. Request in particular must hand the call
// off and answer it later, with Respond, from anywhere.
type Handler interface {
	Request(Request)
	Event(Event)
	Log(string)
	Done(op Op, result int, err string)
}

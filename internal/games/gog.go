package games

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

/*
 * GOG games.
 *
 * GOG's installer writes one registry key per game under `GOG.com\Games\<id>`,
 * naming the folder, whether it installs through Galaxy or not. In that folder
 * sits `goggame-<id>.info`: the game's name and its play tasks, the list Galaxy
 * itself reads to know what "Play" runs. Both are files and keys on this disk,
 * so nothing here signs in or asks GOG anything (ADR 0066).
 *
 * DLC registers the same way and says which game it belongs to (`dependsOn`).
 * It is not a game to put on the grid, so it is skipped: a tile for a DLC
 * would start the base game, which is already there.
 */

// GOGInstall is one registry entry, in the shape the rules below need. A
// plain struct so ScanGOG is tested against fixtures on any operating system.
type GOGInstall struct {
	GameID    string
	Path      string
	DependsOn string
}

// gogInfo is the part of goggame-<id>.info this reads.
type gogInfo struct {
	GameID    string `json:"gameId"`
	Name      string `json:"name"`
	PlayTasks []struct {
		Category  string `json:"category"`
		IsPrimary bool   `json:"isPrimary"`
		Path      string `json:"path"`
		Type      string `json:"type"`
	} `json:"playTasks"`
}

// ScanGOG turns registry entries into games. A missing info file means the
// entry is stale (the folder was deleted without uninstalling) and is skipped
// rather than shown as a game that cannot start.
func ScanGOG(installs []GOGInstall) Result {
	if len(installs) == 0 {
		return Result{Status: StatusNotInstalled}
	}
	out := Result{Status: StatusOK, Games: []Game{}}
	for _, in := range installs {
		if in.DependsOn != "" || !isDigits(in.GameID) || in.Path == "" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(in.Path, "goggame-"+in.GameID+".info"))
		if err != nil {
			continue
		}
		var info gogInfo
		if err := json.Unmarshal(raw, &info); err != nil || info.Name == "" {
			continue
		}
		g := Game{
			ID:          GOGID(in.GameID),
			Name:        info.Name,
			Source:      SourceGOG,
			InstallPath: in.Path,
			SizeBytes:   dirSize(in.Path),
		}
		if task := gogPrimaryTask(info); task != "" {
			g.Executable = filepath.Join(in.Path, filepath.FromSlash(task))
		}
		// GOG ships the game's icon beside it; the executable's is the
		// fallback, and usually the same picture.
		g.IconSource = g.Executable
		if ico := filepath.Join(in.Path, "goggame-"+in.GameID+".ico"); fileExists(ico) {
			g.IconSource = ico
		}
		out.Games = append(out.Games, g)
	}
	return out
}

/*
 * gogPrimaryTask is the path of what "Play" runs: the task marked primary,
 * else the first game task that is a file. Tasks of other categories are
 * manuals, settings tools and websites, which are not the game.
 */
func gogPrimaryTask(info gogInfo) string {
	first := ""
	for _, t := range info.PlayTasks {
		if t.Category != "game" || t.Type != "FileTask" || t.Path == "" {
			continue
		}
		if t.IsPrimary {
			return t.Path
		}
		if first == "" {
			first = t.Path
		}
	}
	return first
}

/*
 * GalaxyArgs is GOG Galaxy's own way of starting an installed game, the one
 * its Start-menu shortcut uses: `/command=runGame /gameId=<id> /path="<dir>"`.
 * Through Galaxy rather than the executable when Galaxy is installed, so the
 * game gets what Galaxy gives it there — play time, cloud saves, the overlay.
 *
 * Validated here, the last step before a process: the id must be digits and
 * the path must not contain a quote, which a Windows path cannot hold anyway
 * and which is the one character that could end the argument early.
 */
func GalaxyArgs(id, installPath string) ([]string, error) {
	if !isDigits(id) {
		return nil, fmt.Errorf("not a GOG game id: %q", id)
	}
	if installPath == "" || strings.ContainsRune(installPath, '"') {
		return nil, fmt.Errorf("not a usable GOG install path: %q", installPath)
	}
	return []string{"/command=runGame", "/gameId=" + id, `/path="` + installPath + `"`}, nil
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

/*
 * commandExecutable is the program a registered command line runs:
 * `"C:\Program Files\GOG Galaxy\GalaxyClient.exe" /urlProtocol="%1"` gives the
 * quoted path; an unquoted command gives its first word.
 */
func commandExecutable(cmd string) string {
	s := strings.TrimSpace(cmd)
	if strings.HasPrefix(s, `"`) {
		if end := strings.Index(s[1:], `"`); end >= 0 {
			return s[1 : 1+end]
		}
		return ""
	}
	if i := strings.IndexByte(s, ' '); i >= 0 {
		return s[:i]
	}
	return s
}

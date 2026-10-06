package games

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
)

/*
 * EA games.
 *
 * The EA app's installer writes an ordinary uninstall entry for each game,
 * published by Electronic Arts, with the folder and, in DisplayIcon, the
 * executable EA's own desktop shortcut runs. In that folder EA writes
 * `__Installer\installerdata.xml`, the manifest the EA app reads: the game's
 * title and its content id. An entry is a game when both are there, which is
 * also what keeps the EA app itself off the grid: it registers under the same
 * publisher with no install folder and no manifest.
 *
 * Started by that executable, not by a `link2ea://` URI. The URI wants an
 * offer id that nothing on this disk records in a form this can trust, the
 * Battle.net problem again; the executable is what EA's shortcut runs, and an
 * EA game started that way hands itself to the EA app for its sign-in and
 * anti-cheat as it would from the desktop.
 */

// eaManifest is the part of installerdata.xml this reads.
type eaManifest struct {
	ContentIDs []string `xml:"contentIDs>contentID"`
	Titles     []struct {
		Locale string `xml:"locale,attr"`
		Name   string `xml:",chardata"`
	} `xml:"gameTitles>gameTitle"`
}

// EAGames picks EA's games out of the uninstall entries.
func EAGames(programs []InstalledProgram) Result {
	out := Result{Status: StatusNotInstalled, Games: []Game{}}
	for _, p := range programs {
		// EA writes the folder with a trailing separator; trimmed before it is
		// used for anything, not only for display.
		dir := strings.TrimRight(p.InstallLocation, `\/`)
		if !strings.Contains(p.Publisher, "Electronic Arts") || dir == "" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, "__Installer", "installerdata.xml"))
		if err != nil {
			continue
		}
		var m eaManifest
		if err := xml.Unmarshal(raw, &m); err != nil || len(m.ContentIDs) == 0 {
			continue
		}
		id := strings.TrimSpace(m.ContentIDs[0])
		if !isDigits(id) {
			continue
		}
		g := Game{
			ID:          EAID(id),
			Name:        eaTitle(m, p.Name),
			Source:      SourceEA,
			InstallPath: dir,
			Executable:  iconExecutable(p.DisplayIcon),
		}
		if p.EstimatedSizeKB > 0 {
			g.SizeBytes = p.EstimatedSizeKB * 1024
		} else {
			g.SizeBytes = dirSize(g.InstallPath)
		}
		out.Status = StatusOK
		out.Games = append(out.Games, g)
	}
	return out
}

// eaTitle is the manifest's English title, else its first, else the name the
// uninstall entry shows.
func eaTitle(m eaManifest, fallback string) string {
	for _, t := range m.Titles {
		if t.Locale == "en_US" && strings.TrimSpace(t.Name) != "" {
			return strings.TrimSpace(t.Name)
		}
	}
	for _, t := range m.Titles {
		if strings.TrimSpace(t.Name) != "" {
			return strings.TrimSpace(t.Name)
		}
	}
	return fallback
}

/*
 * iconExecutable reads a DisplayIcon value as a path: `"D:\Game\Game.exe"`,
 * or `D:\Game\Game.exe,0` with an icon index. Anything that is not an .exe
 * after that is not something to start, and is returned empty. Containment is
 * checked at launch, against the install folder (ExecutableTarget).
 */
func iconExecutable(icon string) string {
	s := strings.TrimSpace(icon)
	if i := strings.LastIndex(s, ","); i > 0 && isDigits(strings.TrimSpace(s[i+1:])) {
		s = strings.TrimSpace(s[:i])
	}
	s = strings.Trim(s, `"`)
	if !strings.EqualFold(filepath.Ext(s), ".exe") {
		return ""
	}
	return s
}

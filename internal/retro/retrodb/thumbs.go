package retrodb

import (
	"net/url"
	"strings"
)

/*
 * Box art from libretro-thumbnails (ADR 0073), addressed by the DAT name a
 * ROM matched.
 *
 * Verified against the project's own README and a live request: images live
 * at thumbnails.libretro.com/<system>/<set>/<name>.png, where <name> is the
 * DAT name with each of these replaced by an underscore: & * / : ` < > ? \ |
 *
 * A network fetch, so it is off until the setting is turned on; nothing here
 * runs during a scan.
 */

const thumbnailHost = "https://thumbnails.libretro.com/"

// systemNames is each platform's libretro system name, which is also the
// thumbnail repository's top-level folder.
var systemNames = map[string]string{
	"nes":     "Nintendo - Nintendo Entertainment System",
	"snes":    "Nintendo - Super Nintendo Entertainment System",
	"n64":     "Nintendo - Nintendo 64",
	"gb":      "Nintendo - Game Boy",
	"gbc":     "Nintendo - Game Boy Color",
	"gba":     "Nintendo - Game Boy Advance",
	"sms":     "Sega - Master System - Mark III",
	"genesis": "Sega - Mega Drive - Genesis",
	"ps1":     "Sony - PlayStation",
}

// Thumbnail sets.
const (
	Boxart = "Named_Boxarts"
	Snap   = "Named_Snaps"
	Title  = "Named_Titles"
)

var thumbUnsafe = strings.NewReplacer(
	"&", "_", "*", "_", "/", "_", ":", "_", "`", "_",
	"<", "_", ">", "_", "?", "_", `\`, "_", "|", "_",
)

// ThumbnailURL is where a matched game's image of one set lives, or "" for a
// platform with no thumbnail system.
func ThumbnailURL(platform, set, datName string) string {
	sys, ok := systemNames[platform]
	if !ok || datName == "" {
		return ""
	}
	return thumbnailHost + url.PathEscape(sys) + "/" + set + "/" +
		url.PathEscape(thumbUnsafe.Replace(datName)) + ".png"
}

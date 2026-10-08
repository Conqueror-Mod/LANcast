package retrodb

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
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

/*
 * ListingURL is the directory index of one console's set, for when the exact
 * name is not there.
 *
 * The thumbnail repository and the DATs are maintained apart and their names
 * drift. Road Rash II is "(USA, Europe) (Rev 1)" in the DAT and has boxes only
 * as "(USA, Europe) (RR205)" and "(RR206)"; a fan translation has no DAT name
 * at all, yet "Fire Emblem - The Binding Blade (USA)" is in the set. The
 * server's own index is the one place that says what is really there: a few
 * hundred kilobytes per console and set, read only after an exact name missed.
 */
func ListingURL(platform, set string) string {
	sys, ok := systemNames[platform]
	if !ok {
		return ""
	}
	return thumbnailHost + url.PathEscape(sys) + "/" + set + "/"
}

var listingHref = regexp.MustCompile(`href="([^"?/][^"]*\.png)"`)

// ParseListing reads image names, without ".png", from a directory index.
// They come back as the files are named — already through thumbUnsafe — so
// ThumbnailURL of one finds it again.
func ParseListing(html string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range listingHref.FindAllStringSubmatch(html, -1) {
		name, err := url.PathUnescape(m[1])
		if err != nil || strings.Contains(name, "/") {
			continue
		}
		name = strings.TrimSuffix(name, ".png")
		if name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

// Listings fetches and remembers directory indexes for the life of the
// process. A failure is not remembered, so a network that comes back is
// asked again.
type Listings struct {
	Client *http.Client
	mu     sync.Mutex
	cache  map[string][]string
}

func (l *Listings) Get(ctx context.Context, u string) ([]string, error) {
	l.mu.Lock()
	if names, ok := l.cache[u]; ok {
		l.mu.Unlock()
		return names, nil
	}
	l.mu.Unlock()
	c := l.Client
	if c == nil {
		c = &http.Client{Timeout: 60 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("thumbnail listing: %s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	names := ParseListing(string(b))
	l.mu.Lock()
	if l.cache == nil {
		l.cache = map[string][]string{}
	}
	l.cache[u] = names
	l.mu.Unlock()
	return names, nil
}

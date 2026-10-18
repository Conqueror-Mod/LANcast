// Package ra reads a person's progress from RetroAchievements' Web API
// (ADR 0090). Read-only: it never earns or submits anything.
//
// Every request carries the person's own Web API key, passed as y, and is
// made only on their behalf. Nothing here runs by itself; the API layer asks
// when the person opens their profile.
package ra

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// BaseURL and MediaURL are RetroAchievements' API and picture hosts.
const (
	BaseURL  = "https://retroachievements.org"
	MediaURL = "https://media.retroachievements.org"
)

// ErrRefused is RetroAchievements turning the key or the name down.
var ErrRefused = errors.New("RetroAchievements did not accept that username and API key")

// Client asks RetroAchievements on someone's behalf.
type Client struct {
	HTTP      *http.Client
	Base      string // BaseURL unless a test says otherwise
	UserAgent string
}

// Profile is what the profile page heads with.
type Profile struct {
	User                string `json:"User"`
	ULID                string `json:"ULID"`
	UserPic             string `json:"UserPic"`
	MemberSince         string `json:"MemberSince"`
	RichPresenceMsg     string `json:"RichPresenceMsg"`
	TotalPoints         int    `json:"TotalPoints"`
	TotalSoftcorePoints int    `json:"TotalSoftcorePoints"`
	TotalTruePoints     int    `json:"TotalTruePoints"`
	Motto               string `json:"Motto"`
}

// Unlock is one recently unlocked achievement.
type Unlock struct {
	Date          string `json:"Date"`
	HardcoreMode  flex   `json:"HardcoreMode"`
	AchievementID int    `json:"AchievementID"`
	Title         string `json:"Title"`
	Description   string `json:"Description"`
	BadgeURL      string `json:"BadgeURL"`
	Points        int    `json:"Points"`
	GameTitle     string `json:"GameTitle"`
	GameIcon      string `json:"GameIcon"`
	GameID        int    `json:"GameID"`
	ConsoleName   string `json:"ConsoleName"`
}

// GameProgress is one game's completion.
type GameProgress struct {
	GameID                int    `json:"GameID"`
	Title                 string `json:"Title"`
	ImageIcon             string `json:"ImageIcon"`
	ConsoleName           string `json:"ConsoleName"`
	MaxPossible           int    `json:"MaxPossible"`
	NumAwarded            int    `json:"NumAwarded"`
	NumAwardedHardcore    int    `json:"NumAwardedHardcore"`
	MostRecentAwardedDate string `json:"MostRecentAwardedDate"`
	HighestAwardKind      string `json:"HighestAwardKind"`
}

// flex reads a JSON bool or 0/1, which this API uses interchangeably.
type flex bool

func (f *flex) UnmarshalJSON(b []byte) error {
	switch strings.Trim(string(b), `"`) {
	case "true", "1":
		*f = true
	default:
		*f = false
	}
	return nil
}

// Profile reads user's profile with key. user is a name or a ULID.
func (c Client) Profile(ctx context.Context, user, key string) (Profile, error) {
	var p Profile
	err := c.get(ctx, "API_GetUserProfile.php", url.Values{"u": {user}, "y": {key}}, &p)
	if err == nil && p.User == "" {
		// An unknown user is answered with an empty object, not an error.
		return p, ErrRefused
	}
	return p, err
}

// Recent reads what user unlocked in the last since.
func (c Client) Recent(ctx context.Context, user, key string, since time.Duration) ([]Unlock, error) {
	var out []Unlock
	m := strconv.Itoa(int(since.Minutes()))
	err := c.get(ctx, "API_GetUserRecentAchievements.php", url.Values{"u": {user}, "y": {key}, "m": {m}}, &out)
	return out, err
}

// Completion reads the first count games of user's completion progress.
func (c Client) Completion(ctx context.Context, user, key string, count int) ([]GameProgress, error) {
	var page struct {
		Results []GameProgress `json:"Results"`
	}
	err := c.get(ctx, "API_GetUserCompletionProgress.php",
		url.Values{"u": {user}, "y": {key}, "c": {strconv.Itoa(count)}}, &page)
	return page.Results, err
}

func (c Client) get(ctx context.Context, endpoint string, q url.Values, into any) error {
	base := c.Base
	if base == "" {
		base = BaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/API/"+endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		// The URL carries the key: report the endpoint, never the request.
		return fmt.Errorf("RetroAchievements could not be reached (%s)", endpoint)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("RetroAchievements' answer could not be read (%s)", endpoint)
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return ErrRefused
	case resp.StatusCode == http.StatusTooManyRequests:
		return errors.New("RetroAchievements asked LANcast to slow down; try again in a minute")
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("RetroAchievements answered %d (%s)", resp.StatusCode, endpoint)
	}
	if err := json.Unmarshal(body, into); err != nil {
		return fmt.Errorf("RetroAchievements' answer was not understood (%s)", endpoint)
	}
	return nil
}

// MediaPath reports whether p is a picture path LANcast may fetch from
// MediaURL: under /UserPic/, /Badge/ or /Images/, a plain file name, .png.
func MediaPath(p string) bool {
	for _, dir := range []string{"/UserPic/", "/Badge/", "/Images/"} {
		name, ok := strings.CutPrefix(p, dir)
		if !ok {
			continue
		}
		if !strings.HasSuffix(name, ".png") || len(name) > 128 {
			return false
		}
		for _, r := range strings.TrimSuffix(name, ".png") {
			ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-'
			if !ok {
				return false
			}
		}
		return true
	}
	return false
}

package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"lancast/internal/retro/ra"
	"lancast/internal/secret"
	"lancast/internal/store"
)

/*
 * RetroAchievements on the profile page (ADR 0090).
 *
 * Opt-in per account and visible to that account alone. Nothing contacts
 * RetroAchievements until a person connects their own account; after that it
 * is contacted for them, when they open their profile, and cached. Their API
 * key is sealed under a key kept outside the database and is never returned
 * or logged. Pictures come through LANcast, so a viewer's browser never
 * contacts RetroAchievements itself.
 */

// RetroAchievements is what the routes need: the sealer for keys, the client,
// and where pictures are cached.
type RetroAchievements struct {
	Sealer   *secret.Sealer
	Client   ra.Client
	MediaDir string
	MediaURL string // ra.MediaURL unless a test says otherwise
}

// SetRetroAchievements turns the feature on. Without it the routes report it
// unavailable.
func (s *Server) SetRetroAchievements(r *RetroAchievements) {
	s.ra = r
	s.raCache = &raCache{views: map[string]raCached{}}
}

const (
	raCacheFor     = 10 * time.Minute
	raRefreshEvery = time.Minute
	raRecentWindow = 14 * 24 * time.Hour
	raGames        = 100
)

type raCache struct {
	mu    sync.Mutex
	views map[string]raCached
}

type raCached struct {
	at   time.Time
	view raView
}

func (c *raCache) forget(user string) {
	c.mu.Lock()
	delete(c.views, user)
	c.mu.Unlock()
}

type raView struct {
	Available      bool       `json:"available"`
	Connected      bool       `json:"connected"`
	User           string     `json:"user,omitempty"`
	Problem        string     `json:"problem,omitempty"`
	Avatar         string     `json:"avatar,omitempty"`
	Points         int        `json:"points"`
	SoftcorePoints int        `json:"softcore_points"`
	TruePoints     int        `json:"true_points"`
	RichPresence   string     `json:"rich_presence,omitempty"`
	Recent         []raUnlock `json:"recent"`
	Games          []raGame   `json:"games"`
	FetchedAt      int64      `json:"fetched_at,omitempty"`
}

type raUnlock struct {
	AchievementID int    `json:"achievement_id"`
	Title         string `json:"title"`
	Description   string `json:"description"`
	Points        int    `json:"points"`
	Hardcore      bool   `json:"hardcore"`
	Date          string `json:"date"`
	Badge         string `json:"badge,omitempty"`
	GameID        int    `json:"game_id"`
	GameTitle     string `json:"game_title"`
	Console       string `json:"console"`
}

type raGame struct {
	GameID          int    `json:"game_id"`
	Title           string `json:"title"`
	Console         string `json:"console"`
	Icon            string `json:"icon,omitempty"`
	Awarded         int    `json:"awarded"`
	AwardedHardcore int    `json:"awarded_hardcore"`
	Max             int    `json:"max"`
	Award           string `json:"award,omitempty"`
	LastUnlock      string `json:"last_unlock,omitempty"`
}

// raMedia turns a RetroAchievements picture path into LANcast's own URL for
// it, or "" when it is not one LANcast fetches.
func raMedia(p string) string {
	if !ra.MediaPath(p) {
		return ""
	}
	return "/api/retroachievements/media" + p
}

func (s *Server) getRetroAchievements(w http.ResponseWriter, r *http.Request) {
	if s.ra == nil {
		writeJSON(w, http.StatusOK, raView{Recent: []raUnlock{}, Games: []raGame{}})
		return
	}
	user := s.userID(r)
	acct, err := s.st.GetRetroAccount(r.Context(), user)
	if err != nil {
		s.writeInternal(w, err, "read retroachievements account")
		return
	}
	if acct == nil {
		writeJSON(w, http.StatusOK, raView{Available: true, Recent: []raUnlock{}, Games: []raGame{}})
		return
	}

	s.raCache.mu.Lock()
	cached, ok := s.raCache.views[user]
	s.raCache.mu.Unlock()
	age := time.Since(cached.at)
	refresh := r.URL.Query().Get("refresh") == "1" && age > raRefreshEvery
	if ok && age < raCacheFor && !refresh {
		writeJSON(w, http.StatusOK, cached.view)
		return
	}

	view := raView{Available: true, Connected: true, User: acct.RAUser, Recent: []raUnlock{}, Games: []raGame{}}
	key, err := s.ra.Sealer.Open(acct.APIKeySealed)
	if err != nil {
		view.Problem = "This connection was made on another server or before a restore, so its key can't be read here. Connect again to carry on."
		writeJSON(w, http.StatusOK, view)
		return
	}
	if err := s.fetchRA(r.Context(), acct, string(key), &view); err != nil {
		// What was shown last stays, with the reason it is not newer.
		if ok {
			stale := cached.view
			stale.Problem = err.Error()
			writeJSON(w, http.StatusOK, stale)
			return
		}
		view.Problem = err.Error()
		writeJSON(w, http.StatusOK, view)
		return
	}
	s.raCache.mu.Lock()
	s.raCache.views[user] = raCached{at: time.Now(), view: view}
	s.raCache.mu.Unlock()
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) fetchRA(ctx context.Context, acct *store.RetroAccount, key string, view *raView) error {
	c := s.ra.Client
	p, err := c.Profile(ctx, acct.RAULID, key)
	if err != nil {
		if errors.Is(err, ra.ErrRefused) {
			return errors.New("RetroAchievements no longer accepts this API key. Connect again with your current one.")
		}
		return err
	}
	view.User = p.User
	view.Avatar = raMedia(p.UserPic)
	view.Points, view.SoftcorePoints, view.TruePoints = p.TotalPoints, p.TotalSoftcorePoints, p.TotalTruePoints
	view.RichPresence = p.RichPresenceMsg
	recent, err := c.Recent(ctx, acct.RAULID, key, raRecentWindow)
	if err != nil {
		return err
	}
	for _, u := range recent {
		view.Recent = append(view.Recent, raUnlock{
			AchievementID: u.AchievementID, Title: u.Title, Description: u.Description,
			Points: u.Points, Hardcore: bool(u.HardcoreMode), Date: u.Date,
			Badge: raMedia(u.BadgeURL), GameID: u.GameID, GameTitle: u.GameTitle, Console: u.ConsoleName,
		})
	}
	games, err := c.Completion(ctx, acct.RAULID, key, raGames)
	if err != nil {
		return err
	}
	for _, g := range games {
		view.Games = append(view.Games, raGame{
			GameID: g.GameID, Title: g.Title, Console: g.ConsoleName, Icon: raMedia(g.ImageIcon),
			Awarded: g.NumAwarded, AwardedHardcore: g.NumAwardedHardcore, Max: g.MaxPossible,
			Award: g.HighestAwardKind, LastUnlock: g.MostRecentAwardedDate,
		})
	}
	view.FetchedAt = time.Now().Unix()
	return nil
}

// putRetroAchievements connects: the name and key are checked with
// RetroAchievements before anything is kept.
func (s *Server) putRetroAchievements(w http.ResponseWriter, r *http.Request) {
	if s.ra == nil {
		writeError(w, http.StatusConflict, "conflict", "RetroAchievements is not available on this server")
		return
	}
	var req struct {
		Username string `json:"username"`
		APIKey   string `json:"api_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}
	name, key := strings.TrimSpace(req.Username), strings.TrimSpace(req.APIKey)
	if name == "" || key == "" || len(name) > 64 || len(key) > 128 {
		writeError(w, http.StatusBadRequest, "bad_request", "both your RetroAchievements username and your Web API key are needed")
		return
	}
	p, err := s.ra.Client.Profile(r.Context(), name, key)
	if errors.Is(err, ra.ErrRefused) {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, "upstream", err.Error())
		return
	}
	if p.ULID == "" {
		writeError(w, http.StatusBadGateway, "upstream", "RetroAchievements did not say which account that is")
		return
	}
	sealed, err := s.ra.Sealer.Seal([]byte(key))
	if err != nil {
		s.writeInternal(w, err, "seal retroachievements key")
		return
	}
	user := s.userID(r)
	if err := s.st.SetRetroAccount(r.Context(), store.RetroAccount{
		UserID: user, RAUser: p.User, RAULID: p.ULID, APIKeySealed: sealed,
	}); err != nil {
		s.writeInternal(w, err, "save retroachievements account")
		return
	}
	s.raCache.forget(user)
	s.getRetroAchievements(w, r)
}

func (s *Server) deleteRetroAchievements(w http.ResponseWriter, r *http.Request) {
	user := s.userID(r)
	if err := s.st.DeleteRetroAccount(r.Context(), user); err != nil {
		s.writeInternal(w, err, "disconnect retroachievements")
		return
	}
	if s.raCache != nil {
		s.raCache.forget(user)
	}
	w.WriteHeader(http.StatusNoContent)
}

/*
 * raMediaRoute serves a RetroAchievements picture from LANcast's own cache,
 * fetching it once. Only picture paths (ra.MediaPath), only PNG, at most
 * 2 MB: this is not a general proxy.
 */
func (s *Server) raMediaRoute(w http.ResponseWriter, r *http.Request) {
	if s.ra == nil {
		http.NotFound(w, r)
		return
	}
	p := "/" + r.PathValue("dir") + "/" + r.PathValue("file")
	if !ra.MediaPath(p) {
		http.NotFound(w, r)
		return
	}
	local := filepath.Join(s.ra.MediaDir, filepath.FromSlash(strings.TrimPrefix(p, "/")))
	if _, err := os.Stat(local); err != nil {
		if err := s.fetchRAMedia(r.Context(), p, local); err != nil {
			http.NotFound(w, r)
			return
		}
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("Content-Type", "image/png")
	http.ServeFile(w, r, local)
}

func (s *Server) fetchRAMedia(ctx context.Context, p, local string) error {
	base := s.ra.MediaURL
	if base == "" {
		base = ra.MediaURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+p, nil)
	if err != nil {
		return err
	}
	if s.ra.Client.UserAgent != "" {
		req.Header.Set("User-Agent", s.ra.Client.UserAgent)
	}
	hc := s.ra.Client.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("picture not found")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20+1))
	if err != nil || len(body) > 2<<20 || !strings.HasPrefix(string(body), "\x89PNG") {
		return errors.New("not a picture LANcast keeps")
	}
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		return err
	}
	tmp := local + ".part"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, local)
}

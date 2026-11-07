package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"lancast/internal/remote"
)

/*
 * Play on another of your own devices
 * ([ADR 0117](../../docs/adr/0117-play-on-another-device.md)).
 *
 * A window that offers itself reports here every few seconds; another window of
 * the same account sends it commands, and the player collects them when the
 * event stream says `remote`.
 *
 * Every route is scoped to the caller's own account and nothing wider. There is
 * no admin view and no way to address another account's player: a command to
 * one is a 404, the same answer as a player that does not exist, so an id
 * cannot even be confirmed by guessing it. Taking over somebody else's screen
 * is not a feature this server offers.
 *
 * The server never judges the item in a play command. The player plays it
 * through its own session, which is this same account, so library access and
 * the rating ceiling apply exactly as if it had been pressed there.
 */

// validPlayerID accepts the random ids windows make: 8 to 64 of [A-Za-z0-9_-].
func validPlayerID(id string) bool {
	if len(id) < 8 || len(id) > 64 {
		return false
	}
	for _, c := range id {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

// playerID reads and checks the path's player id, answering 400 when it is not
// one a window could have made.
func playerID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	if !validPlayerID(id) {
		writeError(w, http.StatusBadRequest, "bad_request", "player id must be 8 to 64 letters, digits, - or _")
		return "", false
	}
	return id, true
}

func (s *Server) listPlayers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"players": s.remote.List(s.userID(r))})
}

func (s *Server) reportPlayer(w http.ResponseWriter, r *http.Request) {
	id, ok := playerID(w, r)
	if !ok {
		return
	}
	var req struct {
		Name       string  `json:"name"`
		Kind       string  `json:"kind"`
		ItemID     int64   `json:"item_id"`
		PositionMS int64   `json:"position_ms"`
		DurationMS int64   `json:"duration_ms"`
		Paused     bool    `json:"paused"`
		Volume     float64 `json:"volume"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}
	user := s.userID(r)
	p, changed := s.remote.Report(user, id, remote.Report{
		Name: req.Name, Kind: req.Kind,
		ItemID: req.ItemID, PositionMS: req.PositionMS, DurationMS: req.DurationMS,
		Paused: req.Paused, Volume: req.Volume,
	})
	if changed {
		s.events.PublishTo(user, TopicPlayers)
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) removePlayer(w http.ResponseWriter, r *http.Request) {
	id, ok := playerID(w, r)
	if !ok {
		return
	}
	user := s.userID(r)
	if s.remote.Remove(user, id) {
		s.events.PublishTo(user, TopicPlayers)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) sendPlayerCommand(w http.ResponseWriter, r *http.Request) {
	id, ok := playerID(w, r)
	if !ok {
		return
	}
	var req struct {
		remote.Command
		// FromPlayer is the sending window's own player id, if it has one,
		// so the receiving window can say where the film came from.
		FromPlayer string `json:"from_player"`
	}
	// A play command's queue can be a whole randomized library.
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}
	user := s.userID(r)
	cmd := req.Command
	cmd.From = ""
	if validPlayerID(req.FromPlayer) {
		cmd.From = s.remote.Name(user, req.FromPlayer)
	}
	if err := s.remote.Send(user, id, cmd); err != nil {
		switch {
		case errors.Is(err, remote.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "that device is not available")
		case errors.Is(err, remote.ErrBadCommand):
			writeError(w, http.StatusBadRequest, "bad_request", badCommandMessage(err))
		default:
			s.writeInternal(w, err, "remote command")
		}
		return
	}
	s.events.PublishTo(user, TopicRemote)
	// Pause and seek are pressed many times a minute from a remote; only a
	// play says something worth a line at Info.
	level := slog.LevelDebug
	if cmd.Action == remote.ActionPlay {
		level = slog.LevelInfo
	}
	s.log.Log(r.Context(), level, "remote: command sent", "player", s.remote.Name(user, id),
		"action", cmd.Action, "item", cmd.ItemID, "from", cmd.From)
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) takePlayerCommands(w http.ResponseWriter, r *http.Request) {
	id, ok := playerID(w, r)
	if !ok {
		return
	}
	cmds, err := s.remote.Take(s.userID(r), id)
	if errors.Is(err, remote.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "that device is not available")
		return
	}
	if err != nil {
		s.writeInternal(w, err, "remote commands")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"commands": cmds})
}

// badCommandMessage is the reason after the sentinel, for the error envelope.
func badCommandMessage(err error) string {
	msg := err.Error()
	if i := strings.LastIndex(msg, "\n"); i >= 0 {
		msg = msg[i+1:]
	}
	return msg
}

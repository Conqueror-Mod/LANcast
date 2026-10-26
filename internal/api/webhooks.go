package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"lancast/internal/config"
	"lancast/internal/presence"
	"lancast/internal/scan"
	"lancast/internal/store"
	"lancast/internal/webhook"
)

/*
 * Webhooks (ADR 0097): administrator-only, none by default, and playback is
 * reported only for accounts that said yes themselves.
 *
 * The webhooks live in settings.json and the dispatcher reads them through
 * settings on every event, so a change here applies to the next event with
 * nothing to restart. The secret is returned once, by POST, and never again.
 */

// webhookView is a webhook as the API shows it: everything but the secret,
// and what happened the last time it was sent something.
type webhookView struct {
	ID        string          `json:"id"`
	URL       string          `json:"url"`
	Events    []string        `json:"events"`
	Enabled   bool            `json:"enabled"`
	CreatedAt int64           `json:"created_at"`
	Last      *webhook.Status `json:"last"`
}

func (s *Server) viewWebhook(h config.Webhook, last map[string]webhook.Status) webhookView {
	v := webhookView{ID: h.ID, URL: h.URL, Events: nonNil(h.Events), Enabled: h.Enabled, CreatedAt: h.CreatedAt}
	if st, ok := last[h.ID]; ok {
		v.Last = &st
	}
	return v
}

func (s *Server) webhookStatus() map[string]webhook.Status {
	if s.webhooks == nil {
		return nil
	}
	return s.webhooks.Last()
}

// listWebhooks answers GET /api/webhooks. Admin only.
func (s *Server) listWebhooks(w http.ResponseWriter, r *http.Request) {
	last := s.webhookStatus()
	hooks := s.settings.Get().Webhooks
	out := make([]webhookView, 0, len(hooks))
	for _, h := range hooks {
		out = append(out, s.viewWebhook(h, last))
	}
	var dropped int64
	if s.webhooks != nil {
		dropped = s.webhooks.Dropped()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"webhooks": out,
		"events":   webhook.Events,
		"dropped":  dropped,
	})
}

func webhookError(w http.ResponseWriter, err error) {
	writeError(w, http.StatusBadRequest, "bad_request", err.Error())
}

// createWebhook answers POST /api/webhooks. Admin only.
func (s *Server) createWebhook(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL     string   `json:"url"`
		Events  []string `json:"events"`
		Enabled *bool    `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}
	u, err := webhook.ValidateURL(req.URL)
	if err != nil {
		webhookError(w, err)
		return
	}
	events, err := webhook.ValidateEvents(req.Events)
	if err != nil {
		webhookError(w, err)
		return
	}
	id, secret, err := webhook.NewSecret()
	if err != nil {
		s.writeInternal(w, err, "make webhook secret")
		return
	}
	h := config.Webhook{ID: id, URL: u, Events: events, Secret: secret, Enabled: true, CreatedAt: time.Now().Unix()}
	if req.Enabled != nil {
		h.Enabled = *req.Enabled
	}

	s.webhookMu.Lock()
	cur := s.settings.Get()
	if len(cur.Webhooks) >= webhook.MaxHooks {
		s.webhookMu.Unlock()
		writeError(w, http.StatusConflict, "too_many", webhook.ErrTooMany.Error())
		return
	}
	cur.Webhooks = append(append([]config.Webhook(nil), cur.Webhooks...), h)
	err = s.settings.Set(cur)
	s.webhookMu.Unlock()
	if err != nil {
		s.writeInternal(w, err, "save webhook")
		return
	}
	// The URL is in the audit detail because an administrator pointing the
	// server somewhere is exactly the act worth finding later. Not the secret.
	s.audit(r, "webhook.create", "webhook", id, "Added a webhook to "+u,
		map[string]any{"url": u, "events": events})
	writeJSON(w, http.StatusCreated, map[string]any{
		"webhook": s.viewWebhook(h, nil),
		"secret":  secret,
	})
}

// patchWebhook answers PATCH /api/webhooks/{id}. Admin only.
func (s *Server) patchWebhook(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL     *string   `json:"url"`
		Events  *[]string `json:"events"`
		Enabled *bool     `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}
	var newURL string
	var newEvents []string
	var err error
	if req.URL != nil {
		if newURL, err = webhook.ValidateURL(*req.URL); err != nil {
			webhookError(w, err)
			return
		}
	}
	if req.Events != nil {
		if newEvents, err = webhook.ValidateEvents(*req.Events); err != nil {
			webhookError(w, err)
			return
		}
	}

	id := r.PathValue("id")
	s.webhookMu.Lock()
	cur := s.settings.Get()
	hooks := append([]config.Webhook(nil), cur.Webhooks...)
	i := webhookIndex(hooks, id)
	if i < 0 {
		s.webhookMu.Unlock()
		writeError(w, http.StatusNotFound, "not_found", webhook.ErrNotFound.Error())
		return
	}
	if req.URL != nil {
		hooks[i].URL = newURL
	}
	if req.Events != nil {
		hooks[i].Events = newEvents
	}
	if req.Enabled != nil {
		hooks[i].Enabled = *req.Enabled
	}
	cur.Webhooks = hooks
	err = s.settings.Set(cur)
	updated := hooks[i]
	s.webhookMu.Unlock()
	if err != nil {
		s.writeInternal(w, err, "save webhook")
		return
	}
	s.audit(r, "webhook.update", "webhook", id, "Changed the webhook to "+updated.URL,
		map[string]any{"url": updated.URL, "events": updated.Events, "enabled": updated.Enabled})
	writeJSON(w, http.StatusOK, s.viewWebhook(updated, s.webhookStatus()))
}

// deleteWebhook answers DELETE /api/webhooks/{id}. Admin only.
func (s *Server) deleteWebhook(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.webhookMu.Lock()
	cur := s.settings.Get()
	i := webhookIndex(cur.Webhooks, id)
	if i < 0 {
		s.webhookMu.Unlock()
		writeError(w, http.StatusNotFound, "not_found", webhook.ErrNotFound.Error())
		return
	}
	gone := cur.Webhooks[i]
	hooks := append(append([]config.Webhook(nil), cur.Webhooks[:i]...), cur.Webhooks[i+1:]...)
	cur.Webhooks = hooks
	err := s.settings.Set(cur)
	s.webhookMu.Unlock()
	if err != nil {
		s.writeInternal(w, err, "remove webhook")
		return
	}
	s.audit(r, "webhook.delete", "webhook", id, "Removed the webhook to "+gone.URL, nil)
	w.WriteHeader(http.StatusNoContent)
}

// testWebhook answers POST /api/webhooks/{id}/test: ping, once, and wait.
func (s *Server) testWebhook(w http.ResponseWriter, r *http.Request) {
	cur := s.settings.Get()
	i := webhookIndex(cur.Webhooks, r.PathValue("id"))
	if i < 0 {
		writeError(w, http.StatusNotFound, "not_found", webhook.ErrNotFound.Error())
		return
	}
	if s.webhooks == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "webhooks are not running on this server")
		return
	}
	writeJSON(w, http.StatusOK, s.webhooks.Test(r.Context(), cur.Webhooks[i]))
}

func webhookIndex(hooks []config.Webhook, id string) int {
	for i, h := range hooks {
		if h.ID == id {
			return i
		}
	}
	return -1
}

/*
 * putPlaybackWebhooks is the account's own switch: may this server's webhooks
 * report what I watch. PUT /api/profile/webhooks. As with activity sharing,
 * there is no administrator route to it (ADR 0097, ADR 0035).
 */
func (s *Server) putPlaybackWebhooks(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFromContext(r)
	if !ok {
		writeError(w, http.StatusConflict, "no_account", "this server has no accounts yet; create one first")
		return
	}
	var req struct {
		Share bool `json:"share"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "malformed JSON body")
		return
	}
	if err := s.st.SetSharePlaybackWebhooks(r.Context(), sess.UserID, req.Share); err != nil {
		s.writeInternal(w, err, "set playback webhooks")
		return
	}
	verb := "stopped letting webhooks report what they watch"
	if req.Share {
		verb = "let webhooks report what they watch"
	}
	s.audit(r, "profile.webhooks", "user", sess.UserID, verb, nil)
	writeJSON(w, http.StatusOK, map[string]any{"share": req.Share})
}

// ---------------------------------------------------------------- emitters

/*
 * playbackChanged turns a presence change into webhook events, for accounts
 * that agreed. A change of title is the old one stopping and the new one
 * starting. Run on its own goroutine: it reads the database, and presence
 * calls it from request handlers.
 */
func (s *Server) playbackChanged(c presence.Change) {
	if s.webhooks == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		on, err := s.st.SharesPlaybackWebhooks(ctx, c.UserID)
		if err != nil || !on {
			return
		}
		u, err := s.st.UserByID(ctx, c.UserID)
		if err != nil || u == nil {
			return
		}
		account := map[string]any{"id": u.ID, "name": u.Name}
		if c.From != "" {
			s.webhooks.Emit(webhook.PlaybackStopped, map[string]any{"account": account, "title": c.From})
		}
		if c.To != "" {
			s.webhooks.Emit(webhook.PlaybackStarted, map[string]any{"account": account, "title": c.To})
		}
	}()
}

// scanFinished tells webhooks how a library's scan ended.
func (s *Server) scanFinished(lib store.Library, p scan.Progress) {
	if s.webhooks == nil {
		return
	}
	outcome := "completed"
	switch p.State {
	case scan.StateFailed:
		outcome = "failed"
	case scan.StateCancelled:
		outcome = "cancelled"
	}
	data := map[string]any{
		"library":       map[string]any{"id": lib.ID, "name": lib.Name, "kind": lib.Kind},
		"outcome":       outcome,
		"files_seen":    p.FilesSeen,
		"items_changed": p.ItemsChanged,
		"items_missing": p.ItemsMissing,
		"duration_ms":   p.ElapsedMS,
	}
	if p.Error != "" && outcome == "failed" {
		data["error"] = p.Error
	}
	s.webhooks.Emit(webhook.ScanFinished, data)
}

// backupTaken tells webhooks about a backup a person or the schedule took.
func (s *Server) backupTaken(name string, bytes int64, schema int, scheduled bool) {
	if s.webhooks == nil {
		return
	}
	by := "person"
	if scheduled {
		by = "schedule"
	}
	s.webhooks.Emit(webhook.BackupTaken, map[string]any{
		"name": name, "bytes": bytes, "schema_version": schema, "taken_by": by,
	})
}

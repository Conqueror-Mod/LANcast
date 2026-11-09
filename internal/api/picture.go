package api

import (
	"errors"
	"net/http"
	"regexp"
	"strings"

	"lancast/internal/picture"
	"lancast/internal/store"
)

/*
 * A picture of your own (ADR 0121).
 *
 * The upload goes through internal/picture, which keeps none of it: what is
 * stored and served is two JPEGs this server drew. Served only to signed-in
 * accounts on this server. A federated guest is refused by the guest gate
 * (the route is not on its list), and nothing here is sent to a paired server.
 */

/*
 * putPicture sets the caller's own picture from a raw image body.
 *
 * The Content-Type is not trusted or required: the decoder decides what the
 * bytes are. A member needs the member_pictures setting on; an admin does not.
 */
func (s *Server) putPicture(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFromContext(r)
	if !ok {
		writeError(w, http.StatusConflict, "no_account",
			"this server has no accounts yet; create one first")
		return
	}
	u, err := s.st.UserByID(r.Context(), sess.UserID)
	if err != nil {
		s.writeInternal(w, err, "get user")
		return
	}
	if u.Role != store.RoleAdmin && !s.settings.Get().MemberPictures {
		writeError(w, http.StatusForbidden, "forbidden",
			"uploading a picture is turned off on this server")
		return
	}

	res, err := picture.Make(r.Body)
	if errors.Is(err, picture.ErrRefused) {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err != nil {
		s.writeInternal(w, err, "make picture")
		return
	}
	hash, err := s.st.SetPicture(r.Context(), sess.UserID, res.Large, res.Small)
	if err != nil {
		s.writeInternal(w, err, "set picture")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"picture": hash})
}

// deletePicture clears the caller's own picture. Their chosen animal, if any,
// shows again.
func (s *Server) deletePicture(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionFromContext(r)
	if !ok {
		writeError(w, http.StatusConflict, "no_account",
			"this server has no accounts yet; create one first")
		return
	}
	if _, err := s.st.ClearPicture(r.Context(), sess.UserID); err != nil {
		s.writeInternal(w, err, "clear picture")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

/*
 * clearUserPicture is an admin taking down somebody's picture: the whole of
 * the moderation ADR 0121 §5 sizes for a household. Audited, because it is one
 * person acting on another's account.
 */
func (s *Server) clearUserPicture(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	target, err := s.st.UserByID(r.Context(), id)
	if s.notFoundOr(w, err, "get user", "no such user") {
		return
	}
	had, err := s.st.ClearPicture(r.Context(), id)
	if err != nil {
		s.writeInternal(w, err, "clear picture")
		return
	}
	if had {
		s.audit(r, "user.picture.clear", "user", id,
			"cleared "+target.Name+"'s picture", nil)
	}
	w.WriteHeader(http.StatusNoContent)
}

var pictureHash = regexp.MustCompile(`^[0-9a-f]{64}$`)

/*
 * servePicture answers GET /api/pictures/{hash}?size=64|256 (256 by default).
 *
 * By hash rather than by account, so the URL changes when the picture does
 * and the response can be cached for ever. "private" because it is a person's
 * face behind a sign-in, and no shared cache should hold one.
 */
func (s *Server) servePicture(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	if !pictureHash.MatchString(hash) {
		writeError(w, http.StatusNotFound, "not_found", "no such picture")
		return
	}
	size := store.PictureLarge
	switch strings.TrimSpace(r.URL.Query().Get("size")) {
	case "", "256":
	case "64":
		size = store.PictureSmall
	default:
		writeError(w, http.StatusBadRequest, "bad_request", "size must be 64 or 256")
		return
	}
	b, err := s.st.PictureByHash(r.Context(), hash, size)
	if s.notFoundOr(w, err, "get picture", "no such picture") {
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(b)
}

// pictureOrNil is the picture field as clients read it: the hash, or null.
func pictureOrNil(hash string) any {
	if hash == "" {
		return nil
	}
	return hash
}

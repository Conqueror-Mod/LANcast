package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"lancast/internal/milkdrop"
)

/*
 * MilkDrop presets (ADR 0091, steps b and g3).
 *
 * An administrator uploads a preset, the server reads and checks it, and
 * every signed-in window can then list it and fetch it to play. The checks are
 * the whole of the ADR's security argument on the server side: a .milk or
 * butterchurn JSON either becomes numbers, EEL that internal/milkdrop/eel has
 * parsed, and GLSL — or it is refused with the line and the reason. Nothing in
 * a preset is ever run here as code. A .milk's HLSL shaders are converted to
 * that GLSL by a converter that runs inside wazero's sandbox, with no files
 * and a deadline (internal/milkdrop/msc); one that will not convert is stored
 * with butterchurn's defaults and the converter's reason, never refused.
 *
 * The body of an upload is the file itself, as a plugin bundle's is, and the
 * filename rides in `?filename=` because it is only ever a display name. It
 * never becomes a path: the stored file is named by the preset's hash.
 */

type milkdropList struct {
	Presets []milkdrop.Summary `json:"presets"`
}

func (s *Server) listMilkdrop(w http.ResponseWriter, r *http.Request) {
	list, err := s.presets.List()
	if err != nil {
		s.writeInternal(w, err, "list milkdrop presets")
		return
	}
	writeJSON(w, http.StatusOK, milkdropList{Presets: list})
}

func (s *Server) getMilkdrop(w http.ResponseWriter, r *http.Request) {
	e, err := s.presets.Get(r.PathValue("id"))
	if errors.Is(err, milkdrop.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "no such preset")
		return
	}
	if err != nil {
		s.writeInternal(w, err, "read milkdrop preset")
		return
	}
	// The same preset never changes under its id, so a window may keep it.
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	writeJSON(w, http.StatusOK, e)
}

/*
 * uploadMilkdrop stores one preset. 201 for a new one, 200 for a preset that
 * was already here (the id is its hash, so uploading a pack twice adds
 * nothing and renames nothing).
 *
 * A refusal is 400 `invalid_preset` with the reason as the message — "per_frame_12,
 * column 7: unknown function invsqrt()" — because the person uploading is the
 * only one who can do anything about it, and it is far better said here than
 * shown as a black screen in every window that picks the preset.
 */
func (s *Server) uploadMilkdrop(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, milkdrop.MaxUploadBytes))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "too_large",
				fmt.Sprintf("a preset is at most %d KB", milkdrop.MaxUploadBytes>>10))
			return
		}
		writeError(w, http.StatusBadRequest, "bad_request", "could not read the upload")
		return
	}
	u, err := milkdrop.Parse(body, r.URL.Query().Get("filename"))
	if milkdrop.IsInvalid(err) {
		writeError(w, http.StatusBadRequest, "invalid_preset", err.Error())
		return
	}
	if err != nil {
		s.writeInternal(w, err, "read milkdrop preset")
		return
	}
	// Parse has accepted it; now its shaders, if it has any. A shader that
	// will not convert is not a refusal — the preset is stored as step 1
	// stored it, with the converter's reason — so the only errors here are a
	// request that ended, which stores nothing, and a converter that is
	// itself broken, which is logged and stored past.
	t0 := time.Now()
	if err := milkdrop.ConvertShaders(r.Context(), s.shaders, &u); err != nil {
		if r.Context().Err() != nil {
			return
		}
		s.log.Error("milkdrop shader converter failed", "name", u.Name, "error", err)
	}
	if u.HLSL != nil {
		s.log.Info("milkdrop shaders", "name", u.Name, "shaders", u.Shaders,
			"reason", u.ShadersReason, "ms", time.Since(t0).Milliseconds())
	}
	sm, created, replaced, err := s.presets.Put(u)
	if err != nil {
		s.writeInternal(w, err, "store milkdrop preset")
		return
	}
	if !created {
		writeJSON(w, http.StatusOK, sm)
		return
	}
	details := map[string]any{"name": sm.Name, "source": sm.Source, "shaders": sm.Shaders}
	if sm.ShadersReason != "" {
		details["shaders_reason"] = sm.ShadersReason
	}
	if replaced != "" {
		details["replaces"] = replaced
	}
	s.audit(r, "visualizer.preset.upload", "milkdrop_preset", sm.ID,
		fmt.Sprintf("Uploaded MilkDrop preset %q", sm.Name), details)
	writeJSON(w, http.StatusCreated, sm)
}

func (s *Server) deleteMilkdrop(w http.ResponseWriter, r *http.Request) {
	sm, err := s.presets.Delete(r.PathValue("id"))
	if errors.Is(err, milkdrop.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "no such preset")
		return
	}
	if err != nil {
		s.writeInternal(w, err, "delete milkdrop preset")
		return
	}
	s.audit(r, "visualizer.preset.delete", "milkdrop_preset", sm.ID,
		fmt.Sprintf("Removed MilkDrop preset %q", sm.Name), map[string]any{"name": sm.Name})
	w.WriteHeader(http.StatusNoContent)
}

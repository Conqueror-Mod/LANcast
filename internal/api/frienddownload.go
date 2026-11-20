package api

import (
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
)

/*
 * A friend downloading from a share (ADR 0071, amended 2026-11-20).
 *
 * Watching and downloading are different grants. A share lets a friend's
 * household watch; whether they may also take the original file away is a
 * second switch on the same share, off unless the host turns it on. A friend
 * had a Downloads page and nothing on a host's titles to put in it.
 *
 * Two hops, like every other federation route: the friend's client asks its
 * own server (peerDownload), which asks the host (federationDownload).
 */

/*
 * federationDownload serves one shared item's original file to a paired
 * server, as an attachment.
 *
 * MayDownload is MayPlay plus the share's switch, so a download never reaches
 * further than a stream: the same library scope, the same ceiling. Unlike the
 * playback routes there is no Watch Together exception — being in a room with
 * somebody lets you watch their film with them, not keep it. A refusal is 404,
 * indistinguishable from an item that does not exist.
 */
func (s *Server) federationDownload(w http.ResponseWriter, r *http.Request) {
	fingerprint, ok := s.federationPeer(w, r)
	if !ok {
		return
	}
	itemID, err := strconv.ParseInt(r.URL.Query().Get("item"), 10, 64)
	if err != nil || itemID <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "which item")
		return
	}
	allowed, err := s.st.MayDownload(r.Context(), fingerprint, itemID)
	if err != nil || !allowed {
		writeError(w, http.StatusNotFound, "not_found", "no such item")
		return
	}
	// No account, for the reason writeSharedStream gives.
	it, err := s.st.GetItem(r.Context(), itemID, "")
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such item")
		return
	}
	s.log.Info("friend download", "peer", fingerprint, "item", itemID, "title", it.Title)
	s.serveDownload(w, r, it)
}

/*
 * peerDownload fetches a file a paired server lets us download, and hands it
 * to our client as an attachment.
 *
 * The filename is rebuilt here rather than forwarded. The far server proposes
 * one; this server reads the name out of it, sanitises it again and writes its
 * own header, so nothing another household sends becomes a header verbatim.
 * The type is always application/octet-stream: the bytes are for saving, and
 * nothing here should be rendered.
 */
func (s *Server) peerDownload(w http.ResponseWriter, r *http.Request) {
	p, ok := s.peerForBrowse(w, r)
	if !ok {
		return
	}
	item := r.URL.Query().Get("item")
	if item == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "which item")
		return
	}

	resp, err := s.openPeerStream(r, p, "/api/federation/download?item="+url.QueryEscape(item))
	if err != nil {
		s.peerUnreachable(w, p, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		if resp.StatusCode == http.StatusNotFound {
			writeError(w, http.StatusNotFound, "not_found", "that server does not offer this download")
			return
		}
		writeError(w, http.StatusBadGateway, "peer_error", "that server could not send the file")
		return
	}

	copyStreamHeaders(w, resp, relayMedia)
	w.Header().Set("Content-Type", "application/octet-stream")
	setAttachment(w, relayedFilename(resp.Header.Get("Content-Disposition")))
	w.WriteHeader(resp.StatusCode)
	//nolint:errcheck // A copy that stops early is a download somebody cancelled.
	io.Copy(w, resp.Body)
}

// relayedFilename takes the filename out of a far server's
// Content-Disposition and makes it safe to propose again.
func relayedFilename(header string) string {
	_, params, err := mime.ParseMediaType(header)
	if err != nil {
		return "download"
	}
	// ParseMediaType decodes filename* (RFC 2231) into "filename".
	return sanitizeFilename(params["filename"])
}

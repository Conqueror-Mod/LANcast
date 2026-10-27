package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"

	"lancast/internal/artwork"
	"lancast/internal/dlna"
	"lancast/internal/store"
)

/*
 * DLNA, the HTTP half (ADR 0086).
 *
 * Served on its own plain-HTTP listener, never the HTTPS one, and the handler
 * built here serves the DLNA routes and nothing else: no LANcast API is
 * reachable through it.
 *
 * Every lookup is made **as the chosen account** (DLNAConfig.Account), so a TV
 * sees exactly what that account sees: GetItem refuses what it cannot open,
 * and listings carry its ceiling and hidden libraries (ADR 0084). That is the
 * whole of DLNA's permission model, and it is the existing one.
 */

// DLNAConfig is what the handler needs to know on every request. A function
// rather than a value, so a change in Settings reaches the next request.
type DLNAConfig struct {
	UUID    string
	Name    string
	Account string
}

// dlnaLAN refuses anything not on the local network (ADR 0086 §4). Checked
// on the connection, not trusted from a header: port forwarding or a public
// IPv6 address must not turn this into an open media server.
func dlnaLAN(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		ip := net.ParseIP(host)
		if err != nil || ip == nil || !(ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast()) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// DLNAHandler is everything the DLNA listener serves.
func (s *Server) DLNAHandler(cfg func() DLNAConfig) http.Handler {
	mux := http.NewServeMux()
	xmlOut := func(w http.ResponseWriter, body string) {
		w.Header().Set("Content-Type", `text/xml; charset="utf-8"`)
		_, _ = w.Write([]byte(body))
	}
	mux.HandleFunc("GET /dlna/device.xml", func(w http.ResponseWriter, r *http.Request) {
		c := cfg()
		xmlOut(w, dlna.DeviceDescription(c.Name, c.UUID))
	})
	mux.HandleFunc("GET /dlna/cds.xml", func(w http.ResponseWriter, r *http.Request) {
		xmlOut(w, dlna.ContentDirectorySCPD)
	})
	mux.HandleFunc("GET /dlna/cms.xml", func(w http.ResponseWriter, r *http.Request) {
		xmlOut(w, dlna.ConnectionManagerSCPD)
	})
	// Eventing is not offered, but several control points subscribe anyway
	// and give up on the server if the subscription is refused outright.
	mux.HandleFunc("/dlna/event/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("SID", "uuid:"+cfg().UUID+"-events")
		w.Header().Set("TIMEOUT", "Second-1800")
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("POST /dlna/control/cms", func(w http.ResponseWriter, r *http.Request) {
		action, _, err := dlna.ParseAction(r.Body)
		switch {
		case err != nil:
			xmlFault(w, 401, "invalid action")
		case action == "GetProtocolInfo":
			xmlOut(w, dlna.Response(dlna.ConnectionManager, action, [][2]string{
				{"Source", dlnaSourceProtocols}, {"Sink", ""},
			}))
		case action == "GetCurrentConnectionIDs":
			xmlOut(w, dlna.Response(dlna.ConnectionManager, action, [][2]string{{"ConnectionIDs", "0"}}))
		default:
			xmlFault(w, 401, "invalid action")
		}
	})
	mux.HandleFunc("POST /dlna/control/cds", func(w http.ResponseWriter, r *http.Request) {
		s.dlnaContentDirectory(w, r, cfg().Account)
	})
	mux.HandleFunc("GET /dlna/media/{id}", func(w http.ResponseWriter, r *http.Request) {
		s.dlnaMedia(w, r, cfg().Account)
	})
	mux.HandleFunc("HEAD /dlna/media/{id}", func(w http.ResponseWriter, r *http.Request) {
		s.dlnaMedia(w, r, cfg().Account)
	})
	mux.HandleFunc("GET /dlna/art/{id}", func(w http.ResponseWriter, r *http.Request) {
		s.dlnaArt(w, r, cfg().Account)
	})
	return dlnaLAN(mux)
}

// dlnaSourceProtocols is what this server can send, for GetProtocolInfo.
const dlnaSourceProtocols = "http-get:*:video/x-matroska:*,http-get:*:video/mp4:*," +
	"http-get:*:video/x-msvideo:*,http-get:*:video/mp2t:*,http-get:*:video/webm:*," +
	"http-get:*:audio/mpeg:*,http-get:*:audio/flac:*,http-get:*:audio/mp4:*," +
	"http-get:*:audio/ogg:*,http-get:*:audio/wav:*,http-get:*:image/jpeg:*,http-get:*:image/png:*"

func xmlFault(w http.ResponseWriter, code int, desc string) {
	w.Header().Set("Content-Type", `text/xml; charset="utf-8"`)
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = w.Write([]byte(dlna.Fault(code, desc)))
}

func (s *Server) dlnaContentDirectory(w http.ResponseWriter, r *http.Request, account string) {
	action, args, err := dlna.ParseAction(r.Body)
	if err != nil {
		xmlFault(w, 401, "invalid action")
		return
	}
	out := func(pairs [][2]string) {
		w.Header().Set("Content-Type", `text/xml; charset="utf-8"`)
		_, _ = w.Write([]byte(dlna.Response(dlna.ContentDirectory, action, pairs)))
	}
	switch action {
	case "GetSystemUpdateID":
		out([][2]string{{"Id", "1"}})
	case "GetSearchCapabilities":
		out([][2]string{{"SearchCaps", ""}})
	case "GetSortCapabilities":
		out([][2]string{{"SortCaps", ""}})
	case "Browse":
		b := dlna.ParseBrowse(args)
		objs, total, ok := s.dlnaBrowse(r, account, b)
		if !ok {
			xmlFault(w, 701, "no such object")
			return
		}
		out([][2]string{
			{"Result", dlna.DIDL(objs)},
			{"NumberReturned", strconv.Itoa(len(objs))},
			{"TotalMatches", strconv.Itoa(total)},
			{"UpdateID", "1"},
		})
	default:
		xmlFault(w, 401, "invalid action")
	}
}

// dlnaBrowse answers one Browse as the account. ok is false for an object
// that does not exist or that the account cannot see, which are the same
// answer by design.
func (s *Server) dlnaBrowse(r *http.Request, account string, b dlna.Browse) ([]dlna.Object, int, bool) {
	ctx := r.Context()
	base := "http://" + r.Host
	hidden, err := s.st.HiddenLibraries(ctx, account)
	if err != nil {
		return nil, 0, false
	}
	ceiling, _ := s.st.MaxContentRating(ctx, account)
	limit := b.Count
	if limit <= 0 || limit > 500 {
		limit = 500
	}

	switch {
	case b.ObjectID == "0":
		if b.Metadata {
			return []dlna.Object{{ID: "0", ParentID: "-1", Title: "LANcast", Class: "object.container", Container: true}}, 1, true
		}
		libs, err := s.st.ListLibraries(ctx)
		if err != nil {
			return nil, 0, false
		}
		var all []dlna.Object
		for _, l := range libs {
			if l.Kind == "retro" || l.Kind == "comics" || containsID(hidden, l.ID) {
				continue // ROMs and comics are LANcast's own to show, not a TV's
			}
			all = append(all, libraryObject(l))
		}
		return page(all, b.Start, limit), len(all), true

	case strings.HasPrefix(b.ObjectID, "lib-"):
		id, err := strconv.ParseInt(strings.TrimPrefix(b.ObjectID, "lib-"), 10, 64)
		if err != nil || containsID(hidden, id) {
			return nil, 0, false
		}
		lib, err := s.st.GetLibrary(ctx, id)
		if err != nil || lib.Kind == "retro" || lib.Kind == "comics" {
			return nil, 0, false
		}
		if b.Metadata {
			return []dlna.Object{libraryObject(*lib)}, 1, true
		}
		items, total, err := s.st.ListItems(ctx, store.ItemFilter{
			LibraryID: id, TopLevel: true, ExcludeKinds: []string{"collection", "playlist"},
			MaxContentRating: ceiling, HiddenLibraries: hidden, Limit: limit, Offset: b.Start,
		})
		if err != nil {
			return nil, 0, false
		}
		objs := s.dlnaObjects(ctx, items, b.ObjectID, base)
		return objs, total - (len(items) - len(objs)), true

	case strings.HasPrefix(b.ObjectID, "item-"):
		id, err := strconv.ParseInt(strings.TrimPrefix(b.ObjectID, "item-"), 10, 64)
		if err != nil {
			return nil, 0, false
		}
		it, err := s.st.GetItem(ctx, id, account)
		if err != nil {
			return nil, 0, false
		}
		if b.Metadata {
			parent := "lib-" + strconv.FormatInt(it.LibraryID, 10)
			if it.ParentID != nil {
				parent = "item-" + strconv.FormatInt(*it.ParentID, 10)
			}
			objs := s.dlnaObjects(ctx, []store.Item{*it}, parent, base)
			return objs, len(objs), len(objs) == 1
		}
		items, total, err := s.st.ListItems(ctx, store.ItemFilter{
			ParentID: &id, MaxContentRating: ceiling, HiddenLibraries: hidden, Limit: limit, Offset: b.Start,
		})
		if err != nil {
			return nil, 0, false
		}
		objs := s.dlnaObjects(ctx, items, b.ObjectID, base)
		return objs, total - (len(items) - len(objs)), true
	}
	return nil, 0, false
}

func containsID(ids []int64, id int64) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func page(all []dlna.Object, start, limit int) []dlna.Object {
	if start >= len(all) {
		return []dlna.Object{}
	}
	end := start + limit
	if end > len(all) {
		end = len(all)
	}
	return all[start:end]
}

func libraryObject(l store.Library) dlna.Object {
	return dlna.Object{
		ID: "lib-" + strconv.FormatInt(l.ID, 10), ParentID: "0",
		Title: l.Name, Class: "object.container.storageFolder", Container: true,
	}
}

// dlnaContainerClass is the UPnP class of each kind that holds others.
var dlnaContainerClass = map[string]string{
	"show":    "object.container.storageFolder",
	"season":  "object.container.storageFolder",
	"artist":  "object.container.person.musicArtist",
	"album":   "object.container.album.musicAlbum",
	"gallery": "object.container.album.photoAlbum",
}

// dlnaItemClass is the UPnP class of each kind a TV can play.
var dlnaItemClass = map[string]string{
	"movie":   "object.item.videoItem.movie",
	"episode": "object.item.videoItem",
	"video":   "object.item.videoItem",
	"track":   "object.item.audioItem.musicTrack",
	"photo":   "object.item.imageItem.photo",
}

// dlnaObjects turns items into Browse entries, leaving out what a TV has no
// use for (a ROM, a missing file) rather than listing things that fail.
func (s *Server) dlnaObjects(ctx context.Context, items []store.Item, parent, base string) []dlna.Object {
	out := make([]dlna.Object, 0, len(items))
	if err := s.st.AttachArtwork(ctx, items); err != nil {
		s.log.Debug("dlna: artwork", "error", err)
	}
	for _, it := range items {
		if it.Missing {
			continue
		}
		o := dlna.Object{
			ID: "item-" + strconv.FormatInt(it.ID, 10), ParentID: parent, Title: it.Title,
		}
		if it.Year != nil {
			o.Year = *it.Year
		}
		if it.Artwork != nil && it.Artwork.Poster != "" {
			o.ArtURL = base + "/dlna/art/" + strconv.FormatInt(it.ID, 10)
		}
		if class, ok := dlnaContainerClass[it.Kind]; ok {
			o.Class, o.Container = class, true
		} else if class, ok := dlnaItemClass[it.Kind]; ok {
			o.Class = class
			res := &dlna.Resource{URL: base + "/dlna/media/" + strconv.FormatInt(it.ID, 10)}
			container := ""
			if it.Container != nil {
				container = *it.Container
			}
			res.MIME = dlna.MIME(it.Kind, container)
			if it.SizeBytes != nil {
				res.Size = *it.SizeBytes
			}
			if it.DurationMS != nil {
				res.Duration = dlna.Duration(*it.DurationMS)
			}
			o.Res = res
		} else {
			continue
		}
		out = append(out, o)
	}
	return out
}

// dlnaMedia sends a file, as the account, through the same containment check
// every file route makes.
func (s *Server) dlnaMedia(w http.ResponseWriter, r *http.Request, account string) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	it, err := s.st.GetItem(r.Context(), id, account)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, playable := dlnaItemClass[it.Kind]; !playable || it.Missing {
		http.NotFound(w, r)
		return
	}
	path, err := s.itemFilePath(r, it)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	container := ""
	if it.Container != nil {
		container = *it.Container
	}
	w.Header().Set("Content-Type", dlna.MIME(it.Kind, container))
	// What a TV checks before it will seek: streaming transfer, byte ranges.
	w.Header().Set("transferMode.dlna.org", "Streaming")
	w.Header().Set("contentFeatures.dlna.org", "DLNA.ORG_OP=01;DLNA.ORG_CI=0;DLNA.ORG_FLAGS=01700000000000000000000000000000")
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}

// dlnaArt sends an item's poster, as the account.
func (s *Server) dlnaArt(w http.ResponseWriter, r *http.Request, account string) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	it, err := s.st.GetItem(r.Context(), id, account)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	items := []store.Item{*it}
	if err := s.st.AttachArtwork(r.Context(), items); err != nil || items[0].Artwork == nil || items[0].Artwork.Poster == "" {
		http.NotFound(w, r)
		return
	}
	f, err := s.art.Open(items[0].Artwork.Poster, artwork.SizePoster)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	http.ServeContent(w, r, fmt.Sprintf("%d.jpg", id), info.ModTime(), f)
}

// DLNAStatus is whether DLNA is running and, when it is not, why — the line
// Server › DLNA leads with, so a TV that finds nothing has an answer.
type DLNAStatus struct {
	Running bool   `json:"running"`
	Reason  string `json:"reason"`
	Port    int    `json:"port"`
}

// SetDLNAStatus is how the supervisor in serverapp reports to the pane.
func (s *Server) SetDLNAStatus(f func() DLNAStatus) { s.dlnaStatus = f }

// dlnaStatusRoute answers GET /api/dlna. Admin only.
func (s *Server) dlnaStatusRoute(w http.ResponseWriter, r *http.Request) {
	st := DLNAStatus{Reason: "off", Port: 8200}
	if s.dlnaStatus != nil {
		st = s.dlnaStatus()
	}
	writeJSON(w, http.StatusOK, st)
}

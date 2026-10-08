// Package remote is the desktop player's side of the server (ADR 0073): a
// game's files and the person's saves, reached with the stream ticket the
// page minted for the game (ADR 0068, amended by 0073).
//
// The ticket never leaves this process and never appears in a URL. TLS is
// pinned to the server's public key through certpin, the one copy of that
// check every native client path shares.
package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"lancast/internal/certpin"
)

// Game is one game on one server, reached with one ticket.
type Game struct {
	origin *url.URL
	client *http.Client
	itemID int64
	ticket string
}

// New prepares a client for one game. pin is required for https and refused
// for http, as the libmpv relay requires, so the two cannot be mixed up into
// an unpinned TLS connection.
func New(origin, pin string, itemID int64, ticket string) (*Game, error) {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("retro remote: bad server origin %q", origin)
	}
	if itemID <= 0 || ticket == "" {
		return nil, errors.New("retro remote: need a game and a ticket")
	}
	tr := &http.Transport{Proxy: nil, IdleConnTimeout: 90 * time.Second}
	switch u.Scheme {
	case "https":
		if pin == "" {
			return nil, errors.New("retro remote: https server with no certificate pin")
		}
		tr.TLSClientConfig = certpin.TLSConfig(pin)
	case "http":
		if pin != "" {
			return nil, errors.New("retro remote: a pin was given for a plain-http server")
		}
	default:
		return nil, fmt.Errorf("retro remote: unsupported scheme %q", u.Scheme)
	}
	return &Game{
		origin: &url.URL{Scheme: u.Scheme, Host: u.Host},
		client: &http.Client{Transport: tr},
		itemID: itemID, ticket: ticket,
	}, nil
}

func (g *Game) url(path string, q url.Values) string {
	u := *g.origin
	u.Path = path
	u.RawQuery = q.Encode()
	return u.String()
}

func (g *Game) do(ctx context.Context, method, path string, q url.Values, body []byte) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, g.url(path, q), r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Ticket "+g.ticket)
	req.Header.Set("User-Agent", "LANcast-Client (retro)")
	if body != nil {
		req.Header.Set("Content-Type", "application/octet-stream")
	}
	return g.client.Do(req)
}

// statusError reads the server's error envelope into an error.
func statusError(resp *http.Response) error {
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if json.Unmarshal(b, &env) == nil && env.Error.Message != "" {
		return fmt.Errorf("server: %s (%d)", env.Error.Message, resp.StatusCode)
	}
	return fmt.Errorf("server returned %s", resp.Status)
}

// File is one file of the game.
type File struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
	Present   bool   `json:"present"`
}

// Files lists the game's files, entry first.
func (g *Game) Files(ctx context.Context) ([]File, error) {
	resp, err := g.do(ctx, http.MethodGet, "/api/items/"+g.id()+"/files", nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, statusError(resp)
	}
	var out struct {
		Files []File `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out.Files, nil
}

func (g *Game) id() string { return strconv.FormatInt(g.itemID, 10) }

// ItemID is the game this client was made for.
func (g *Game) ItemID() int64 { return g.itemID }

/*
 * Download fetches every file of the game into dir and returns the entry
 * file's local path.
 *
 * A file already there at the listed size is not fetched again: a disc image
 * is hundreds of megabytes, and playing the same game tomorrow should not
 * cost them twice. Each name is checked again here before it becomes a path —
 * the server keeps names inside the game's folder, and this keeps them inside
 * the cache whatever a server says.
 */
func (g *Game) Download(ctx context.Context, dir string, progress func(done, total int64)) (string, error) {
	files, err := g.Files(ctx)
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", errors.New("the game lists no files")
	}
	var total, done int64
	for _, f := range files {
		total += f.SizeBytes
	}
	var entry string
	for i, f := range files {
		if !f.Present {
			return "", fmt.Errorf("%s is missing from the server's disk", f.Name)
		}
		local, err := LocalPath(dir, f.Name)
		if err != nil {
			return "", err
		}
		if i == 0 {
			entry = local
		}
		if st, err := os.Stat(local); err == nil && st.Size() == f.SizeBytes {
			done += f.SizeBytes
			if progress != nil {
				progress(done, total)
			}
			continue
		}
		if err := g.fetch(ctx, f.Name, local, func(n int64) {
			if progress != nil {
				progress(done+n, total)
			}
		}); err != nil {
			return "", fmt.Errorf("%s: %w", f.Name, err)
		}
		done += f.SizeBytes
	}
	return entry, nil
}

// LocalPath turns a listed name into a path under dir, refusing any name
// that would leave it.
func LocalPath(dir, name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	// A leading separator is refused outright. On Windows "/etc/passwd" has
	// no drive and would join to a path inside dir — harmless, but a server
	// sending one is not sending a name the listing could have produced.
	if name == "" || strings.HasPrefix(name, "/") || strings.HasPrefix(name, `\`) ||
		filepath.IsAbs(clean) || clean == ".." ||
		strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.VolumeName(clean) != "" {
		return "", fmt.Errorf("refusing file name %q", name)
	}
	return filepath.Join(dir, clean), nil
}

func (g *Game) fetch(ctx context.Context, name, dst string, progress func(int64)) error {
	resp, err := g.do(ctx, http.MethodGet, "/api/stream/"+g.id()+"/files", url.Values{"name": {name}}, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return statusError(resp)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".part-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	var n int64
	buf := make([]byte, 256<<10)
	for {
		k, rerr := resp.Body.Read(buf)
		if k > 0 {
			if _, err := tmp.Write(buf[:k]); err != nil {
				tmp.Close()
				return err
			}
			n += int64(k)
			progress(n)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			tmp.Close()
			return rerr
		}
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dst)
}

// ---- host.SaveStore ----

var errNoSave = errors.New("no save in this slot")

func (g *Game) savePath(slot string) string { return "/api/items/" + g.id() + "/saves/" + slot }

// requestTimeout bounds a save call. Saves run in the background, but one
// that never returns would hold a game's close for ever.
const requestTimeout = 30 * time.Second

func (g *Game) get(slot string) ([]byte, http.Header, error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	resp, err := g.do(ctx, http.MethodGet, g.savePath(slot), nil, nil)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil, errNoSave
	}
	if resp.StatusCode != http.StatusOK {
		return nil, nil, statusError(resp)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20+1))
	return b, resp.Header, err
}

func (g *Game) put(slot string, data []byte, q url.Values) error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	resp, err := g.do(ctx, http.MethodPut, g.savePath(slot), q, data)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return statusError(resp)
	}
	return nil
}

// LoadSRAM returns the saved game, or nil when there is none yet.
func (g *Game) LoadSRAM() ([]byte, error) {
	b, _, err := g.get("sram")
	if errors.Is(err, errNoSave) {
		return nil, nil
	}
	return b, err
}

func (g *Game) StoreSRAM(data []byte) error { return g.put("sram", data, nil) }

func (g *Game) StoreState(slot string, data []byte, core, version string) error {
	return g.put(slot, data, url.Values{"core": {core}, "core_version": {version}})
}

func (g *Game) LoadState(slot string) ([]byte, string, string, error) {
	b, h, err := g.get(slot)
	if err != nil {
		return nil, "", "", err
	}
	return b, h.Get("X-LANcast-Core"), h.Get("X-LANcast-Core-Version"), nil
}

// Package artwork stores images in a content-addressed cache and generates
// display sizes on demand.
//
// The SHA-256 of the source bytes is the image's identity, so a backdrop shared
// by two items is stored once and an upstream URL change orphans nothing. The
// whole cache is rebuildable: delete the directory and it heals on next access.
package artwork

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/image/draw"
)

// Size names a generated variant.
type Size string

const (
	SizeThumb    Size = "thumb"
	SizePoster   Size = "poster"
	SizePoster2x Size = "poster2x"
	SizeFanart   Size = "fanart"
	SizeOriginal Size = "original"
)

// widths are the generated variants. Serving full-size fanart into a poster
// grid is what makes a library feel slow over LAN and unusable remotely.
var widths = map[Size]int{
	SizeThumb:    185,
	SizePoster:   342,
	SizePoster2x: 500,
	SizeFanart:   1280,
}

// ValidSize reports whether s names a known variant.
func ValidSize(s Size) bool {
	_, ok := widths[s]
	return ok || s == SizeOriginal
}

// maxDownload bounds a single image fetch. Artwork is never this large, and an
// unbounded read from a remote host is not something to leave open.
const maxDownload = 32 << 20 // 32 MiB

// Cache stores and serves artwork under a root directory.
type Cache struct {
	root string
	http *http.Client

	// mu guards per-hash derivation so concurrent requests for the same
	// missing size do not both decode and encode it.
	mu       sync.Mutex
	deriving map[string]*sync.Mutex
}

// New builds a cache rooted at dir.
func New(dir string) *Cache {
	return &Cache{
		root:     dir,
		http:     &http.Client{Timeout: 30 * time.Second},
		deriving: map[string]*sync.Mutex{},
	}
}

// Stored reports whether the original for hash is already on disk.
func (c *Cache) Stored(hash string) bool {
	_, err := os.Stat(c.pathFor(hash, SizeOriginal))
	return err == nil
}

// Download fetches an image, stores it by content hash, and returns the hash
// with the decoded dimensions.
func (c *Cache) Download(ctx context.Context, url string) (hash string, w, h int, size int64, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", 0, 0, 0, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", 0, 0, 0, fmt.Errorf("artwork: fetch %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", 0, 0, 0, fmt.Errorf("artwork: fetch %s: status %d", url, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDownload))
	if err != nil {
		return "", 0, 0, 0, fmt.Errorf("artwork: read %s: %w", url, err)
	}

	return c.Put(body)
}

// Put stores raw image bytes, returning the content hash and dimensions.
// Storing the same bytes twice is a no-op.
func (c *Cache) Put(body []byte) (hash string, w, h int, size int64, err error) {
	cfg, _, err := image.DecodeConfig(strings.NewReader(string(body)))
	if err != nil {
		return "", 0, 0, 0, fmt.Errorf("artwork: not a decodable image: %w", err)
	}

	sum := sha256.Sum256(body)
	hash = hex.EncodeToString(sum[:])

	target := c.pathFor(hash, SizeOriginal)
	if _, statErr := os.Stat(target); statErr == nil {
		return hash, cfg.Width, cfg.Height, int64(len(body)), nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", 0, 0, 0, fmt.Errorf("artwork: mkdir: %w", err)
	}
	if err := writeAtomic(target, body); err != nil {
		return "", 0, 0, 0, err
	}
	return hash, cfg.Width, cfg.Height, int64(len(body)), nil
}

// Open returns a reader for a hash at a size, generating the variant on first
// request. It returns os.ErrNotExist if the original is not cached.
func (c *Cache) Open(hash string, size Size) (*os.File, error) {
	if !validHash(hash) {
		return nil, os.ErrNotExist
	}
	if !ValidSize(size) {
		size = SizePoster
	}

	if f, err := os.Open(c.pathFor(hash, size)); err == nil {
		return f, nil
	}

	original := c.pathFor(hash, SizeOriginal)
	if _, err := os.Stat(original); err != nil {
		return nil, os.ErrNotExist
	}
	if size == SizeOriginal {
		return os.Open(original)
	}

	if err := c.derive(hash, size); err != nil {
		// Falling back to the original keeps the page working even if
		// resizing fails; it is slower, not broken.
		return os.Open(original)
	}
	return os.Open(c.pathFor(hash, size))
}

// derive generates one size from the stored original, once.
func (c *Cache) derive(hash string, size Size) error {
	lock := c.lockFor(hash + string(size))
	lock.Lock()
	defer lock.Unlock()

	target := c.pathFor(hash, size)
	if _, err := os.Stat(target); err == nil {
		return nil // another goroutine got there first
	}

	src, err := os.Open(c.pathFor(hash, SizeOriginal))
	if err != nil {
		return err
	}
	defer src.Close()

	img, _, err := image.Decode(src)
	if err != nil {
		return fmt.Errorf("artwork: decode %s: %w", hash, err)
	}

	width := widths[size]
	bounds := img.Bounds()
	if bounds.Dx() <= width {
		// Never upscale — it costs bytes and looks worse than the original.
		width = bounds.Dx()
	}
	height := bounds.Dy() * width / bounds.Dx()
	if width <= 0 || height <= 0 {
		return fmt.Errorf("artwork: degenerate dimensions for %s", hash)
	}

	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, bounds, draw.Over, nil)

	var buf strings.Builder
	if err := jpeg.Encode(&stringWriter{&buf}, dst, &jpeg.Options{Quality: 85}); err != nil {
		return fmt.Errorf("artwork: encode %s: %w", hash, err)
	}
	return writeAtomic(target, []byte(buf.String()))
}

func (c *Cache) lockFor(key string) *sync.Mutex {
	c.mu.Lock()
	defer c.mu.Unlock()
	if l, ok := c.deriving[key]; ok {
		return l
	}
	l := &sync.Mutex{}
	c.deriving[key] = l
	return l
}

// pathFor shards by the first two hash characters so no directory accumulates
// tens of thousands of entries.
func (c *Cache) pathFor(hash string, size Size) string {
	return filepath.Join(c.root, hash[:2], hash, string(size)+".jpg")
}

func validHash(h string) bool {
	if len(h) != 64 {
		return false
	}
	for _, r := range h {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func writeAtomic(target string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("artwork: mkdir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".tmp-*")
	if err != nil {
		return fmt.Errorf("artwork: temp: %w", err)
	}
	name := tmp.Name()
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		os.Remove(name)
		return fmt.Errorf("artwork: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, target); err != nil {
		os.Remove(name)
		return fmt.Errorf("artwork: rename: %w", err)
	}
	return nil
}

// stringWriter adapts strings.Builder to io.Writer for jpeg.Encode.
type stringWriter struct{ b *strings.Builder }

func (w *stringWriter) Write(p []byte) (int, error) { return w.b.Write(p) }

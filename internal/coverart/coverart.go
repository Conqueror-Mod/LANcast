// Package coverart sources album artwork from the files a music library
// already contains.
//
// Two sources, in the order ADR 0024 sets: the picture embedded in a track,
// then a `cover.jpg` or `folder.jpg` sitting beside it. Embedded wins because
// it travels with the record — it was attached by whoever tagged the files and
// cannot be about a different album, where a loose image in a directory can be
// anything a file manager left there.
//
// Choosing a sidecar is separated from running ffmpeg, for the reason
// `probe.ParseJSON` is separated from `probe.Probe`: the selection rules are
// where the fiddly judgement lives, and they are worth testing against a list
// of filenames rather than against a directory of real albums.
package coverart

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"lancast/internal/childproc"
	"lancast/internal/media"
	"os/exec"

	// JPEG and PNG only, matching exactly what internal/artwork can store.
	//
	// Not an arbitrary limit, and not one to widen casually: Go's image
	// registry is global to the binary, so importing a decoder here silently
	// changes what every other package's image.DecodeConfig accepts. Accepting
	// a format the cache cannot store would be worse — this package would find
	// a cover, hand it over, and the cache would reject it, which reads as an
	// album that mysteriously has no artwork. If WebP covers turn out to be
	// common, both packages widen together.
	_ "image/jpeg"
	_ "image/png"
)

// ErrNoArt means nothing was found. It is the ordinary outcome for a great many
// albums, not a failure, and callers are expected to record the attempt and
// move on rather than retry.
var ErrNoArt = errors.New("no cover art found")

// ErrNotInstalled is returned when ffmpeg cannot be found.
var ErrNotInstalled = errors.New("ffmpeg not found on PATH")

// maxImage bounds what will be read into memory. Embedded pictures are
// occasionally enormous — a scanned gatefold at print resolution — and an album
// cover does not need to be, so a file past this is rejected rather than
// resized. The cache derives display sizes from the original, and an original
// nobody can afford to hold is not an original worth keeping.
const maxImage = 24 << 20 // 24 MB

// Source names where an image came from.
//
// Recorded because "why does this album have the wrong cover" is answerable if
// the answer is "the folder.jpg beside it" and unanswerable otherwise.
type Source string

const (
	SourceEmbedded Source = "embedded"
	SourceSidecar  Source = "sidecar"
)

// Image is one found cover.
type Image struct {
	Bytes  []byte
	Source Source
	// From is the file the bytes came out of — the track for an embedded
	// picture, the image itself for a sidecar.
	From string
}

// sidecarNames are the base filenames worth treating as an album cover, best
// first. Taken from what real libraries contain rather than from a standard:
// `cover` and `folder` are what Picard and Windows write, `front` is what disc
// rippers write, and `albumart` is what older media players left behind.
//
// Order is preference, and it is load-bearing: an album directory holding both
// `cover.jpg` and `back.jpg` must not get the back of the sleeve, which is why
// this is a ranked list and not a set.
var sidecarNames = []string{"cover", "folder", "front", "album", "albumart", "albumartsmall"}

// sidecarExts are the image formats worth trying, best first — and the same
// set the artwork cache can store, for the reason the imports above give.
var sidecarExts = []string{".jpg", ".jpeg", ".png"}

// PickSidecar chooses the best cover image from a directory's filenames, or
// returns "" if none qualifies.
//
// Pure, and takes names rather than a path, so the ranking is testable without
// a directory of fixtures. Matching is case-insensitive because Windows and
// Linux disagree about whether `Cover.jpg` and `cover.jpg` are the same file,
// and a music library is routinely written by both.
func PickSidecar(names []string) string {
	// Index by lowercase name so the ranked search below is a lookup rather
	// than a scan per candidate, and sort first so a directory listing in any
	// order resolves the same way on every run.
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)

	byLower := make(map[string]string, len(sorted))
	for _, n := range sorted {
		l := strings.ToLower(n)
		if _, seen := byLower[l]; !seen {
			byLower[l] = n
		}
	}

	for _, base := range sidecarNames {
		for _, ext := range sidecarExts {
			if actual, ok := byLower[base+ext]; ok {
				return actual
			}
		}
	}
	return ""
}

// Extractor pulls the embedded picture out of a media file with ffmpeg.
//
// Process execution is confined here so the rest of the package stays testable
// without ffmpeg installed, the same split probing already uses.
type Extractor struct {
	// Path to the ffmpeg binary. Empty means look it up on PATH.
	Path string
	// Timeout bounds one extraction. A damaged file can otherwise hang.
	Timeout time.Duration
}

func NewExtractor() *Extractor { return &Extractor{Timeout: 30 * time.Second} }

// Available reports whether ffmpeg can be found.
func (e *Extractor) Available() bool {
	_, err := e.binary()
	return err == nil
}

func (e *Extractor) binary() (string, error) {
	if e.Path != "" {
		return e.Path, nil
	}
	found, err := exec.LookPath("ffmpeg")
	if err != nil {
		return "", ErrNotInstalled
	}
	return found, nil
}

// Embedded returns the attached picture from a media file.
//
// A file with no picture is not an error worth logging: ffmpeg exits non-zero
// because the stream it was asked to map does not exist, which is the ordinary
// case for an untagged rip. That becomes ErrNoArt.
func (e *Extractor) Embedded(ctx context.Context, path string) ([]byte, error) {
	bin, err := e.binary()
	if err != nil {
		return nil, err
	}

	timeout := e.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin,
		"-v", "error",
		"-nostdin",
		// The file path is a separate argument, never interpolated into a
		// shell string — music filenames contain quotes and worse.
		"-i", path,
		// Map the first video stream, which for an audio file is the attached
		// picture. -an drops the audio explicitly: without it ffmpeg can still
		// decide the output format wants a sound track and refuse.
		"-an",
		"-map", "0:v:0",
		// Copy rather than re-encode. The embedded picture is already a JPEG or
		// PNG, and decoding and re-encoding it would cost a generation of
		// quality to produce the same image.
		"-c:v", "copy",
		"-frames:v", "1",
		"-f", "image2pipe",
		"pipe:1",
	)
	// Without this every extraction flashes a console window on Windows — the
	// v0.4.1 lesson, which applies to any child process this project spawns.
	childproc.Hide(cmd)

	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// No video stream to map is the "no art" case, not a fault.
			return nil, ErrNoArt
		}
		return nil, fmt.Errorf("extract cover %s: %w", path, err)
	}
	if len(out) == 0 {
		return nil, ErrNoArt
	}
	if len(out) > maxImage {
		return nil, fmt.Errorf("extract cover %s: picture is %d bytes, over the %d limit",
			path, len(out), maxImage)
	}
	return out, nil
}

// Resolver finds an album's cover, trying each source in turn.
type Resolver struct {
	ext *Extractor

	// TracksToTry bounds how many tracks are opened looking for an embedded
	// picture. Albums tag their art on every track or on none, so the first
	// track almost always settles it; the allowance exists for the album whose
	// first file is the odd one out. Trying all twenty of a well-tagged-but-
	// artless album would be twenty ffmpeg spawns to learn what two proved.
	TracksToTry int
}

func NewResolver(ext *Extractor) *Resolver {
	return &Resolver{ext: ext, TracksToTry: 3}
}

// Available reports whether embedded extraction can run. Sidecar discovery
// works regardless — it is a file read — so a server with no ffmpeg still gets
// covers for libraries that keep them beside the music.
func (r *Resolver) Available() bool { return r.ext.Available() }

// ForAlbum finds a cover for an album, given its tracks in order.
//
// Embedded first, then a sidecar beside the tracks, per ADR 0024. Returns
// ErrNoArt when neither source has anything, which is an ordinary outcome.
func (r *Resolver) ForAlbum(ctx context.Context, trackPaths []string) (*Image, error) {
	if len(trackPaths) == 0 {
		return nil, ErrNoArt
	}

	if r.ext.Available() {
		limit := r.TracksToTry
		if limit <= 0 || limit > len(trackPaths) {
			limit = len(trackPaths)
		}
		for _, p := range trackPaths[:limit] {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			body, err := r.ext.Embedded(ctx, p)
			if err != nil {
				// Including ErrNoArt: this track has no picture, the next
				// might. Anything worse is also not fatal to the album.
				continue
			}
			if !decodable(body) {
				// A tagger wrote something that is not an image, or wrote a
				// format Go cannot read. Either way it must not reach the
				// cache, which would store bytes nothing can ever render.
				continue
			}
			return &Image{Bytes: body, Source: SourceEmbedded, From: p}, nil
		}
	}

	// Sidecars live beside the tracks. Every track of an album is normally in
	// one directory, but a multi-disc rip splits into subdirectories, so the
	// album's own directory is the parent of a disc folder. Both are searched,
	// nearest first.
	for _, dir := range sidecarDirs(trackPaths) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if !e.IsDir() {
				names = append(names, e.Name())
			}
		}
		if sharedWithOtherAlbums(names, dir, trackPaths) {
			// The image here is not this album's. Found against a real library:
			// loose tracks dropped into a letter-bucket folder each become
			// their own album, and a single Folder.jpg in that bucket was
			// handed to every one of them — five unrelated records wearing the
			// same cover, which looks exactly like a scanner bug.
			continue
		}
		name := PickSidecar(names)
		if name == "" {
			continue
		}
		full := filepath.Join(dir, name)
		body, err := readImage(full)
		if err != nil {
			continue
		}
		return &Image{Bytes: body, Source: SourceSidecar, From: full}, nil
	}

	return nil, ErrNoArt
}

// sharedWithOtherAlbums reports whether a directory holds audio that does not
// belong to this album — which makes any image in it a directory's picture
// rather than this record's cover.
//
// The case that forces this is a library organised into letter buckets, or any
// folder of loose singles: each file groups into its own album by tag, every
// one of them names that folder as its directory, and a single `folder.jpg`
// there would otherwise be adopted by all of them. Being wrong here is worse
// than finding nothing — a grid of unrelated albums sharing one cover reads as
// a broken scanner, where a missing cover reads as a missing cover.
//
// A multi-disc album is deliberately unaffected: its parent directory holds the
// disc folders and no audio of its own, so the cover beside them is still
// found.
func sharedWithOtherAlbums(names []string, dir string, trackPaths []string) bool {
	ours := 0
	for _, p := range trackPaths {
		if filepath.Dir(p) == dir {
			ours++
		}
	}
	for _, n := range names {
		if !media.IsAudio(n) {
			continue
		}
		ours--
		if ours < 0 {
			// An audio file in this directory that is not one of ours.
			return true
		}
	}
	return false
}

// sidecarDirs lists the directories worth searching for an album, nearest
// first and without duplicates. A single-directory album yields one; a
// multi-disc rip yields each disc folder and their shared parent.
func sidecarDirs(trackPaths []string) []string {
	var dirs []string
	seen := map[string]bool{}
	add := func(d string) {
		if d == "" || seen[d] {
			return
		}
		seen[d] = true
		dirs = append(dirs, d)
	}
	for _, p := range trackPaths {
		add(filepath.Dir(p))
	}
	// Parents second, so a cover in the disc folder beats one in the album
	// folder rather than the other way round.
	for _, p := range trackPaths {
		add(filepath.Dir(filepath.Dir(p)))
	}
	return dirs
}

// readImage reads a sidecar, refusing anything too large or not decodable.
func readImage(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxImage {
		return nil, fmt.Errorf("cover %s is %d bytes, over the %d limit",
			path, info.Size(), maxImage)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !decodable(body) {
		return nil, fmt.Errorf("cover %s is not a decodable image", path)
	}
	return body, nil
}

// decodable reports whether Go can read the image's header.
//
// Checked before storing rather than after, because the artwork cache is
// content-addressed: bytes that reach it are keyed by hash and derived into
// display sizes on demand, and something undecodable would be stored once and
// then fail every single time it was asked for.
func decodable(body []byte) bool {
	_, _, err := image.DecodeConfig(bytes.NewReader(body))
	return err == nil
}

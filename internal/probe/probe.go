// Package probe inspects media files with ffprobe.
//
// Until now LANcast knew a file's name and size and nothing about its
// contents. That is enough to serve bytes, and not enough to decide whether a
// given client can play them — which is the question the whole of M3 turns on.
//
// Parsing is separated from execution: ParseJSON is a pure function over
// ffprobe's output, so the interesting logic is testable against fixtures
// without ffmpeg installed or any media on disk.
package probe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"lancast/internal/childproc"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ErrNotInstalled is returned when ffprobe cannot be found.
var ErrNotInstalled = errors.New("ffprobe not found on PATH")

// StreamKind distinguishes the tracks in a container.
type StreamKind string

const (
	KindVideo    StreamKind = "video"
	KindAudio    StreamKind = "audio"
	KindSubtitle StreamKind = "subtitle"
)

// Stream is one track.
type Stream struct {
	Index    int        `json:"index"`
	Kind     StreamKind `json:"kind"`
	Codec    string     `json:"codec"`
	Profile  string     `json:"profile,omitempty"`
	Language string     `json:"language,omitempty"`
	Title    string     `json:"title,omitempty"`
	Default  bool       `json:"default"`
	Forced   bool       `json:"forced"`

	// Video only.
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
	PixFmt    string `json:"pix_fmt,omitempty"`
	FrameRate string `json:"frame_rate,omitempty"`

	// Colour, for telling HDR from SDR (ADR 0033).
	//
	// Bit depth cannot do it. `yuv420p10le` is what HDR10 reports and it is
	// also what 10-bit SDR reports, so without these the server has no way to
	// know which files need tone mapping — and converts both by dropping bits,
	// which on an HDR source produces a washed-out picture *and* an output
	// still carrying the source's PQ tags.
	ColorTransfer  string `json:"color_transfer,omitempty"`
	ColorPrimaries string `json:"color_primaries,omitempty"`
	ColorSpace     string `json:"color_space,omitempty"`

	// Audio only.
	Channels   int `json:"channels,omitempty"`
	SampleRate int `json:"sample_rate,omitempty"`

	BitRate int64 `json:"bit_rate,omitempty"`
}

// Tags is the embedded metadata a music file carries — ID3v2 on MP3, Vorbis
// comments on FLAC and Ogg, atoms on MP4.
//
// For music these are the authority, not a guess (ADR 0024): they are written
// by the tagger rather than inferred from a filename by whoever ripped the
// disc.
//
// Only these fields are read. A real FLAC in the test library carries a
// multi-kilobyte LYRICS tag, and pulling every tag into memory and then into
// the database to find four of them is not a trade worth making.
type Tags struct {
	Title       string
	Artist      string
	Album       string
	AlbumArtist string
	Genre       string
	Track       int
	Disc        int
	Year        int
}

// Empty reports whether nothing useful was tagged, which is what sends the
// scanner back to folder and filename guessing.
func (t Tags) Empty() bool {
	return t.Title == "" && t.Artist == "" && t.Album == "" && t.AlbumArtist == ""
}

// Result is everything probing learned about one file.
type Result struct {
	Container  string   `json:"container"`
	DurationMS int64    `json:"duration_ms"`
	BitRate    int64    `json:"bit_rate"`
	SizeBytes  int64    `json:"size_bytes"`
	Streams    []Stream `json:"streams"`
	Tags       Tags     `json:"tags"`
}

// Video returns the first video stream, which is the one that matters for
// playback decisions. Cover art is skipped: it is stored as a video stream and
// would otherwise be mistaken for the picture.
func (r *Result) Video() *Stream {
	for i := range r.Streams {
		s := &r.Streams[i]
		if s.Kind == KindVideo && !isCoverArt(s) {
			return s
		}
	}
	return nil
}

// Audio returns the default audio stream, or the first if none is marked.
func (r *Result) Audio() *Stream {
	var first *Stream
	for i := range r.Streams {
		s := &r.Streams[i]
		if s.Kind != KindAudio {
			continue
		}
		if s.Default {
			return s
		}
		if first == nil {
			first = s
		}
	}
	return first
}

// AudioByIndex returns the audio stream at an absolute stream index, or nil if
// there is no audio stream there.
//
// The index is the one ffmpeg's `-map 0:N` uses, so a caller that selects a
// track and a caller that decides how to deliver it are talking about the same
// stream. Returning nil rather than falling back is deliberate: a request for
// a track that does not exist is a bad request, not a reason to silently play
// a different one.
func (r *Result) AudioByIndex(index int) *Stream {
	for i := range r.Streams {
		s := &r.Streams[i]
		if s.Kind == KindAudio && s.Index == index {
			return s
		}
	}
	return nil
}

// Subtitles returns every subtitle track.
func (r *Result) Subtitles() []Stream {
	var out []Stream
	for _, s := range r.Streams {
		if s.Kind == KindSubtitle {
			out = append(out, s)
		}
	}
	return out
}

// isCoverArt reports whether a video stream is embedded artwork rather than
// motion picture. A single-frame mjpeg or png track is a poster, and treating
// it as the video stream would make an audio file look like a film.
func isCoverArt(s *Stream) bool {
	switch s.Codec {
	case "mjpeg", "png", "bmp", "gif", "webp":
		return true
	}
	return false
}

// Prober runs ffprobe.
type Prober struct {
	// Path to the ffprobe binary. Empty means look it up on PATH.
	Path string
	// Timeout bounds a single probe. A damaged file can otherwise hang.
	Timeout time.Duration
}

func New() *Prober { return &Prober{Timeout: 30 * time.Second} }

// Available reports whether ffprobe can be found.
func (p *Prober) Available() bool {
	_, err := p.binary()
	return err == nil
}

func (p *Prober) binary() (string, error) {
	if p.Path != "" {
		return p.Path, nil
	}
	found, err := exec.LookPath("ffprobe")
	if err != nil {
		return "", ErrNotInstalled
	}
	return found, nil
}

// Probe inspects a file.
func (p *Prober) Probe(ctx context.Context, path string) (*Result, error) {
	bin, err := p.binary()
	if err != nil {
		return nil, err
	}

	timeout := p.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin,
		"-v", "error",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		// The file path is passed as a separate argument, never interpolated
		// into a shell string — media filenames contain quotes, semicolons,
		// and worse.
		path,
	)
	childproc.Hide(cmd)

	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			msg := strings.TrimSpace(string(exitErr.Stderr))
			if msg == "" {
				msg = exitErr.String()
			}
			return nil, fmt.Errorf("probe %s: %s", path, msg)
		}
		return nil, fmt.Errorf("probe %s: %w", path, err)
	}

	return ParseJSON(out)
}

// ReadTags reads only a file's embedded tags.
//
// Separate from Probe because the scanner needs tags for every music file to
// group it (ADR 0024), and it needs them *during* the walk — long before the
// probe worker gets to codecs. Asking only for the container skips decoding
// every stream header, which is the expensive half.
//
// An untagged file is not an error: it returns empty tags, and the caller falls
// back to folder and filename.
func (p *Prober) ReadTags(ctx context.Context, path string) (Tags, error) {
	bin, err := p.binary()
	if err != nil {
		return Tags{}, err
	}

	timeout := p.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin,
		"-v", "error",
		"-print_format", "json",
		"-show_format",
		path,
	)
	childproc.Hide(cmd)

	out, err := cmd.Output()
	if err != nil {
		return Tags{}, fmt.Errorf("read tags %s: %w", path, err)
	}

	var doc ffprobeDoc
	if err := json.Unmarshal(out, &doc); err != nil {
		return Tags{}, fmt.Errorf("parse tags %s: %w", path, err)
	}
	return parseTags(doc.Format.Tags), nil
}

// ParseJSON converts ffprobe output into a Result. Kept separate from process
// execution so the parsing — where the real complexity lives — is testable
// against fixtures without ffmpeg installed.
func ParseJSON(raw []byte) (*Result, error) {
	var doc ffprobeDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse ffprobe output: %w", err)
	}

	res := &Result{
		Container:  primaryFormat(doc.Format.FormatName),
		DurationMS: secondsToMS(doc.Format.Duration),
		BitRate:    atoi64(doc.Format.BitRate),
		SizeBytes:  atoi64(doc.Format.Size),
		Tags:       parseTags(doc.Format.Tags),
	}

	for _, s := range doc.Streams {
		kind := StreamKind(s.CodecType)
		switch kind {
		case KindVideo, KindAudio, KindSubtitle:
		default:
			// Data and attachment streams are not playable tracks.
			continue
		}

		out := Stream{
			Index:    s.Index,
			Kind:     kind,
			Codec:    s.CodecName,
			Profile:  s.Profile,
			Language: strings.TrimSpace(s.Tags.Language),
			Title:    strings.TrimSpace(s.Tags.Title),
			Default:  s.Disposition.Default == 1,
			Forced:   s.Disposition.Forced == 1,
			Width:    s.Width,
			Height:   s.Height,
			PixFmt:   s.PixFmt,
			// Lower-cased because ffprobe's spelling varies with the container
			// and the rule below is an exact match on a handful of names.
			ColorTransfer:  strings.ToLower(strings.TrimSpace(s.ColorTransfer)),
			ColorPrimaries: strings.ToLower(strings.TrimSpace(s.ColorPrimaries)),
			ColorSpace:     strings.ToLower(strings.TrimSpace(s.ColorSpace)),
			FrameRate:      normalizeRate(s.AvgFrameRate),
			Channels:       s.Channels,
			SampleRate:     atoi(s.SampleRate),
			BitRate:        atoi64(s.BitRate),
		}
		// "und" is ffprobe's placeholder for unknown and carries no more
		// information than an empty string, but reads as a real language.
		if out.Language == "und" {
			out.Language = ""
		}
		res.Streams = append(res.Streams, out)
	}

	// Duration occasionally lives on the video stream rather than the format,
	// notably for MPEG-TS.
	if res.DurationMS == 0 {
		for _, s := range doc.Streams {
			if ms := secondsToMS(s.Duration); ms > 0 {
				res.DurationMS = ms
				break
			}
		}
	}

	return res, nil
}

// ---------------------------------------------------------------- decoding

type ffprobeDoc struct {
	Format struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
		Size       string `json:"size"`
		BitRate    string `json:"bit_rate"`
		// Tags are container-level: for music, the ID3v2 / Vorbis / MP4 fields.
		// Read as raw strings because every value here needs interpreting —
		// "11/15" is a track number, "/0" is a malformed disc, and "2016-04-29"
		// is a year with extra.
		Tags map[string]string `json:"tags"`
	} `json:"format"`
	Streams []struct {
		Index          int    `json:"index"`
		CodecType      string `json:"codec_type"`
		CodecName      string `json:"codec_name"`
		Profile        string `json:"profile"`
		Width          int    `json:"width"`
		Height         int    `json:"height"`
		PixFmt         string `json:"pix_fmt"`
		ColorTransfer  string `json:"color_transfer"`
		ColorPrimaries string `json:"color_primaries"`
		ColorSpace     string `json:"color_space"`
		AvgFrameRate   string `json:"avg_frame_rate"`
		Channels       int    `json:"channels"`
		SampleRate     string `json:"sample_rate"`
		BitRate        string `json:"bit_rate"`
		Duration       string `json:"duration"`
		Disposition    struct {
			Default int `json:"default"`
			Forced  int `json:"forced"`
		} `json:"disposition"`
		Tags struct {
			Language string `json:"language"`
			Title    string `json:"title"`
		} `json:"tags"`
	} `json:"streams"`
}

// primaryFormat picks one name from ffprobe's comma-separated list.
// "mov,mp4,m4a,3gp,3g2,mj2" is not a useful value to store.
func primaryFormat(name string) string {
	if i := strings.IndexByte(name, ','); i >= 0 {
		return name[:i]
	}
	return name
}

// normalizeRate turns ffprobe's "24000/1001" into "23.976", and drops the
// meaningless "0/0" that appears on streams with no frame rate.
func normalizeRate(rate string) string {
	if rate == "" || rate == "0/0" {
		return ""
	}
	num, den, ok := strings.Cut(rate, "/")
	if !ok {
		return rate
	}
	n, err1 := strconv.ParseFloat(num, 64)
	d, err2 := strconv.ParseFloat(den, 64)
	if err1 != nil || err2 != nil || d == 0 {
		return ""
	}
	return strconv.FormatFloat(n/d, 'f', -1, 64)
}

func secondsToMS(s string) int64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || f <= 0 {
		return 0
	}
	return int64(f * 1000)
}

func atoi(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}

func atoi64(s string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// ---------------------------------------------------------------------- tags

// parseTags pulls the music fields out of ffprobe's container tags.
//
// The shapes here are all taken from real files rather than a specification,
// because taggers do not agree with each other and the specification does not
// bind them:
//
//   - Case varies by container. MP3 reports "artist", FLAC reports "ARTIST".
//   - Separators vary. A FLAC carried both "ALBUM ARTIST" (with a space) and
//     "album_artist", so keys are matched with spaces, underscores and hyphens
//     removed rather than by exact name.
//   - Numbers arrive as fractions. "11/15" is track 11 of 15; "1" is track 1.
//   - Fractions arrive malformed. A real file reported disc "/0", which has no
//     numerator at all and means "not set".
//   - Dates are not years. "1989" and "2016-04-29" both appear; only the
//     leading year is wanted.
func parseTags(raw map[string]string) Tags {
	if len(raw) == 0 {
		return Tags{}
	}

	// Normalise once, and iterate in sorted order so two spellings of the same
	// field resolve the same way on every run rather than however the map
	// happened to be walked.
	norm := make(map[string]string, len(raw))
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := strings.TrimSpace(raw[k])
		if v == "" {
			continue
		}
		n := normalizeTagKey(k)
		if _, seen := norm[n]; !seen {
			norm[n] = v
		}
	}

	first := func(names ...string) string {
		for _, n := range names {
			if v := norm[n]; v != "" {
				return v
			}
		}
		return ""
	}

	t := Tags{
		Title:       first("title"),
		Artist:      first("artist", "performer"),
		Album:       first("album"),
		AlbumArtist: first("albumartist", "ensemble", "band"),
		Genre:       first("genre"),
	}
	t.Track = leadingNumber(first("track", "tracknumber"))
	t.Disc = leadingNumber(first("disc", "discnumber", "disk"))
	t.Year = leadingYear(first("date", "year", "originaldate", "originalyear"))
	return t
}

// normalizeTagKey lowercases and drops the separators taggers disagree about,
// so "ALBUM ARTIST", "album_artist" and "Album-Artist" are one field.
func normalizeTagKey(k string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(k) {
		switch r {
		case ' ', '_', '-':
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// leadingNumber reads the numerator of a "N/M" tag, or a bare number. A value
// with no leading digits — the observed "/0" — is not set, and returns 0.
func leadingNumber(v string) int {
	if i := strings.IndexByte(v, '/'); i >= 0 {
		v = v[:i]
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// leadingYear takes the year off a date tag, which may be a bare "1989" or a
// full "2016-04-29".
func leadingYear(v string) int {
	v = strings.TrimSpace(v)
	if len(v) < 4 {
		return 0
	}
	n, err := strconv.Atoi(v[:4])
	if err != nil || n < 1000 || n > 9999 {
		return 0
	}
	return n
}

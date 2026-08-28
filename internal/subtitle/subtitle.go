// Package subtitle discovers, converts, and serves subtitle tracks.
//
// Browsers render exactly one subtitle format: WebVTT. Everything here exists
// to get text into that format, or to say clearly why it cannot.
package subtitle

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"lancast/internal/media"
)

// Kind separates what can become WebVTT from what cannot.
type Kind string

const (
	// Text formats carry characters and convert cleanly.
	Text Kind = "text"
	// Bitmap formats are images of text. PGS and VOBSUB store rendered
	// pictures, so there is nothing to convert without OCR — they can only be
	// burned into the video, which forces a full re-encode.
	Bitmap Kind = "bitmap"
	// Unknown is anything unrecognized; treated as unusable rather than
	// attempted, since a failed conversion mid-playback is worse than an
	// honest "unavailable".
	Unknown Kind = "unknown"
)

// textCodecs are the ffmpeg codec names that carry text.
var textCodecs = map[string]bool{
	"subrip": true, "srt": true, "ass": true, "ssa": true,
	"webvtt": true, "vtt": true, "mov_text": true, "text": true,
	"microdvd": true, "subviewer": true, "subviewer1": true, "sami": true,
}

// bitmapCodecs store pictures rather than characters.
var bitmapCodecs = map[string]bool{
	"hdmv_pgs_subtitle": true, "pgssub": true, "pgs": true,
	"dvd_subtitle": true, "dvdsub": true, "vobsub": true,
	"dvb_subtitle": true, "xsub": true,
}

// ClassifyCodec reports whether a subtitle codec can become WebVTT.
func ClassifyCodec(codec string) Kind {
	c := strings.ToLower(strings.TrimSpace(codec))
	switch {
	case textCodecs[c]:
		return Text
	case bitmapCodecs[c]:
		return Bitmap
	default:
		return Unknown
	}
}

// UnsupportedReason explains, in words a person can act on, why a track cannot
// be displayed.
func UnsupportedReason(codec string) string {
	switch ClassifyCodec(codec) {
	case Bitmap:
		return "image-based subtitles (" + strings.ToUpper(codec) +
			") cannot be shown as text — search for a subtitle file instead"
	case Unknown:
		return "unrecognized subtitle format (" + codec + ")"
	default:
		return ""
	}
}

// sidecarExtensions are the external subtitle files worth indexing.
var sidecarExtensions = map[string]bool{
	".srt": true, ".vtt": true, ".ass": true, ".ssa": true, ".sub": true,
}

// IsSidecar reports whether a path looks like an external subtitle file.
func IsSidecar(path string) bool {
	return sidecarExtensions[strings.ToLower(filepath.Ext(path))]
}

// Sidecar is an external subtitle file found beside a video.
type Sidecar struct {
	Path     string
	Language string
	Title    string
	Forced   bool
	Format   string // extension without the dot
}

// FindSidecars locates subtitle files belonging to a video.
//
// Looks beside the file and in the conventional Subs/ and Subtitles/
// subdirectories, since ripping tools commonly put them there.
//
// The trap a media server must not fall into is the shared subtitle folder.
// When several videos share a directory — a flat movie library, or a season of
// episodes — a subtitle named only for its language ("Subs/English.srt") names
// no particular film, and handing it to every video is exactly how one movie
// ends up showing another's subtitles. So the language-only shortcut applies
// only when this is the sole video in its directory. Otherwise every candidate
// must name the video: in the filename ("Subs/Film.en.srt") or via a per-video
// subfolder ("Subs/Film (2020)/English.srt").
/*
 * DirReader lists a directory. It exists so a scan can read each folder once
 * instead of once per file in it.
 *
 * Sidecar discovery is inherently per-directory — "what else is sitting next to
 * this video" — but it was being asked per *file*, twice: once here and once in
 * isSoleVideo. A season folder of twenty episodes therefore read the same
 * directory forty times, and a music library read one per track for subtitle
 * formats that cannot apply to audio at all.
 */
type DirReader func(string) ([]fs.DirEntry, error)

// FindSidecars finds subtitle files beside a video, reading the filesystem
// directly. Callers walking many files in one directory should use
// FindSidecarsWith and share a reader.
func FindSidecars(videoPath string) []Sidecar {
	return FindSidecarsWith(videoPath, os.ReadDir)
}

// FindSidecarsWith is FindSidecars against a caller-supplied directory reader.
func FindSidecarsWith(videoPath string, read DirReader) []Sidecar {
	base := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	dir := filepath.Dir(videoPath)

	// One read, used for both questions. Sharing a reader across files is only
	// half the saving if a single call still asks twice.
	entries, readErr := read(dir)
	sole := readErr == nil && isSoleVideo(entries, videoPath)

	var out []Sidecar
	seen := map[string]bool{}
	add := func(path, stem string) {
		lp := strings.ToLower(path)
		if seen[lp] {
			return
		}
		seen[lp] = true
		s := Sidecar{
			Path:   path,
			Format: strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."),
		}
		s.Language, s.Forced, s.Title = parseSidecarName(stem, base)
		out = append(out, s)
	}

	// Beside the video: always require the filename to name the video. A loose
	// "English.srt" next to two films belongs to neither.
	if readErr == nil {
		for _, e := range entries {
			if e.IsDir() || !IsSidecar(e.Name()) {
				continue
			}
			stem := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
			if !nameMatchesVideo(stem, base) {
				continue
			}
			add(filepath.Join(dir, e.Name()), stem)
		}
	}

	// Conventional subtitle subdirectories.
	for _, sub := range []string{"Subs", "subs", "Subtitles", "subtitles"} {
		subDir := filepath.Join(dir, sub)
		if st, err := os.Stat(subDir); err != nil || !st.IsDir() {
			continue
		}
		collectFromSubsDir(subDir, base, sole, add)
	}
	return out
}

// collectFromSubsDir gathers subtitles from a Subs/ or Subtitles/ folder,
// applying the language-only shortcut only when the video is alone in its
// directory. A per-video subfolder is honored regardless, since its name is an
// explicit statement of ownership.
func collectFromSubsDir(subDir, base string, sole bool, add func(path, stem string)) {
	entries, err := os.ReadDir(subDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			if nameMatchesVideo(name, base) {
				collectLangFiles(filepath.Join(subDir, name), base, add)
			}
			continue
		}
		if !IsSidecar(name) {
			continue
		}
		stem := strings.TrimSuffix(name, filepath.Ext(name))
		// A file that names the video always belongs to it; a language-only
		// file does only when there is no other video to confuse it with.
		if sole || nameMatchesVideo(stem, base) {
			add(filepath.Join(subDir, name), stem)
		}
	}
}

// collectLangFiles claims every subtitle in a per-video subfolder, where the
// folder name has already established ownership.
func collectLangFiles(dir, base string, add func(path, stem string)) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !IsSidecar(e.Name()) {
			continue
		}
		stem := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		add(filepath.Join(dir, e.Name()), stem)
	}
}

// nameMatchesVideo reports whether a subtitle stem (or subfolder name) names
// the video: an exact match, or the video name followed by a separator and
// language/flag tokens ("Film.en.forced").
func nameMatchesVideo(stem, base string) bool {
	if strings.EqualFold(stem, base) {
		return true
	}
	lower, lb := strings.ToLower(stem), strings.ToLower(base)
	if !strings.HasPrefix(lower, lb) {
		return false
	}
	rest := lower[len(lb):]
	return rest != "" && strings.ContainsRune(".-_ ", rune(rest[0]))
}

// isSoleVideo reports whether videoPath is the only video file in dir. When it
// is, a language-only subtitle in a Subs/ folder can safely be assumed to
// belong to it; when it is not, that assumption links movies to the wrong file.
func isSoleVideo(entries []fs.DirEntry, videoPath string) bool {
	self := filepath.Base(videoPath)
	for _, e := range entries {
		if e.IsDir() || !media.IsVideo(e.Name()) {
			continue
		}
		if !strings.EqualFold(e.Name(), self) {
			return false
		}
	}
	return true
}

// parseSidecarName pulls language and flags out of the part of the filename
// that follows the video's own name: "Film.en.forced.srt" → en, forced.
func parseSidecarName(stem, base string) (language string, forced bool, title string) {
	suffix := stem
	if strings.HasPrefix(strings.ToLower(stem), strings.ToLower(base)) {
		suffix = stem[len(base):]
	}
	suffix = strings.Trim(suffix, " .-_")

	if suffix == "" {
		return "", false, ""
	}

	parts := strings.FieldsFunc(suffix, func(r rune) bool {
		return r == '.' || r == '-' || r == '_' || r == ' '
	})

	var leftovers []string
	for _, p := range parts {
		lower := strings.ToLower(p)
		switch {
		case lower == "forced":
			forced = true
		case lower == "sdh" || lower == "cc" || lower == "hi":
			// Hearing-impaired markers are worth keeping as a label rather
			// than discarding: people choose tracks on exactly this.
			leftovers = append(leftovers, strings.ToUpper(p))
		case language == "" && isLanguageCode(lower):
			language = normalizeLanguage(lower)
		default:
			leftovers = append(leftovers, p)
		}
	}
	return language, forced, strings.Join(leftovers, " ")
}

// languageNames maps the spellings seen in real filenames to ISO 639-1.
var languageNames = map[string]string{
	"en": "en", "eng": "en", "english": "en",
	"fr": "fr", "fre": "fr", "fra": "fr", "french": "fr",
	"es": "es", "spa": "es", "esp": "es", "spanish": "es",
	"de": "de", "ger": "de", "deu": "de", "german": "de",
	"it": "it", "ita": "it", "italian": "it",
	"pt": "pt", "por": "pt", "portuguese": "pt",
	"nl": "nl", "dut": "nl", "nld": "nl", "dutch": "nl",
	"ja": "ja", "jpn": "ja", "japanese": "ja",
	"ko": "ko", "kor": "ko", "korean": "ko",
	"zh": "zh", "chi": "zh", "zho": "zh", "chinese": "zh",
	"ru": "ru", "rus": "ru", "russian": "ru",
	"pl": "pl", "pol": "pl", "polish": "pl",
	"sv": "sv", "swe": "sv", "swedish": "sv",
	"da": "da", "dan": "da", "danish": "da",
	"no": "no", "nor": "no", "norwegian": "no",
	"fi": "fi", "fin": "fi", "finnish": "fi",
	"ar": "ar", "ara": "ar", "arabic": "ar",
	"he": "he", "heb": "he", "hebrew": "he",
	"hi": "hi", "hin": "hi", "hindi": "hi",
	"tr": "tr", "tur": "tr", "turkish": "tr",
	"cs": "cs", "cze": "cs", "ces": "cs", "czech": "cs",
	"el": "el", "gre": "el", "ell": "el", "greek": "el",
}

func isLanguageCode(s string) bool {
	_, ok := languageNames[s]
	return ok
}

// NormalizeLanguage maps any recognized spelling to an ISO 639-1 code.
func NormalizeLanguage(s string) string {
	return normalizeLanguage(strings.ToLower(strings.TrimSpace(s)))
}

func normalizeLanguage(s string) string {
	if code, ok := languageNames[s]; ok {
		return code
	}
	return s
}

// DisplayLanguage turns a code into something readable in a picker.
func DisplayLanguage(code string) string {
	names := map[string]string{
		"en": "English", "fr": "French", "es": "Spanish", "de": "German",
		"it": "Italian", "pt": "Portuguese", "nl": "Dutch", "ja": "Japanese",
		"ko": "Korean", "zh": "Chinese", "ru": "Russian", "pl": "Polish",
		"sv": "Swedish", "da": "Danish", "no": "Norwegian", "fi": "Finnish",
		"ar": "Arabic", "he": "Hebrew", "hi": "Hindi", "tr": "Turkish",
		"cs": "Czech", "el": "Greek",
	}
	c := NormalizeLanguage(code)
	if name, ok := names[c]; ok {
		return name
	}
	if c == "" {
		return "Unknown"
	}
	return strings.ToUpper(c)
}

// Package subtitle discovers, converts, and serves subtitle tracks.
//
// Browsers render exactly one subtitle format: WebVTT. Everything here exists
// to get text into that format, or to say clearly why it cannot.
package subtitle

import (
	"os"
	"path/filepath"
	"strings"
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
func FindSidecars(videoPath string) []Sidecar {
	base := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	dir := filepath.Dir(videoPath)

	dirs := []string{dir}
	for _, sub := range []string{"Subs", "subs", "Subtitles", "subtitles"} {
		candidate := filepath.Join(dir, sub)
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			dirs = append(dirs, candidate)
		}
	}

	var out []Sidecar
	seen := map[string]bool{}

	for _, d := range dirs {
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if !IsSidecar(name) {
				continue
			}

			stem := strings.TrimSuffix(name, filepath.Ext(name))
			// A file in a Subs/ directory belongs to the only video there;
			// requiring a name match would miss the common "Subs/English.srt".
			if d == dir && !strings.EqualFold(stem, base) &&
				!strings.HasPrefix(strings.ToLower(stem), strings.ToLower(base)+".") {
				continue
			}

			path := filepath.Join(d, name)
			if seen[strings.ToLower(path)] {
				continue
			}
			seen[strings.ToLower(path)] = true

			s := Sidecar{
				Path:   path,
				Format: strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), "."),
			}
			s.Language, s.Forced, s.Title = parseSidecarName(stem, base)
			out = append(out, s)
		}
	}
	return out
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

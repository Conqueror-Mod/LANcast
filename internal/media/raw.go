package media

import (
	"os"
	"path/filepath"
	"strings"
)

/*
 * Camera RAW files in a picture library (ADR 0107).
 *
 * Indexed like any other picture. What they show is the full-size JPEG the
 * camera stores inside every RAW file, which is read by internal/photo; the
 * sensor data itself is never developed.
 *
 * A RAW kept beside a JPEG of the same name is one photograph shot twice,
 * and is skipped so it shows once: that is how cameras set to RAW+JPEG
 * write them, and how every photo manager pairs them.
 */
var rawExts = map[string]bool{
	".cr2": true, ".cr3": true, ".crw": true, // Canon
	".nef": true, ".nrw": true, // Nikon
	".arw": true, ".srf": true, ".sr2": true, // Sony
	".dng": true, // Adobe, Leica, Pentax, phones
	".raf": true, // Fujifilm
	".orf": true, // Olympus, OM System
	".rw2": true, // Panasonic
	".pef": true, // Pentax
	".srw": true, // Samsung
}

func init() {
	for ext := range rawExts {
		imageExts[ext] = true
	}
}

// IsRAW reports whether a path is a camera RAW file.
func IsRAW(path string) bool {
	return rawExts[strings.ToLower(filepath.Ext(path))]
}

// developedExts are what a camera writes beside a RAW in RAW+JPEG mode, and
// what a phone writes beside a DNG.
var developedExts = []string{".jpg", ".jpeg", ".JPG", ".JPEG", ".heic", ".HEIC", ".heif", ".HEIF"}

// HasDevelopedTwin reports whether a RAW file has a JPEG or HEIC of the same
// name in the same folder: the same photograph, already developed.
func HasDevelopedTwin(path string) bool {
	if !IsRAW(path) {
		return false
	}
	stem := strings.TrimSuffix(path, filepath.Ext(path))
	for _, ext := range developedExts {
		if fi, err := os.Stat(stem + ext); err == nil && !fi.IsDir() {
			return true
		}
	}
	return false
}

package playlist

import (
	"path/filepath"
	"strings"
	"testing"
)

/*
 * FuzzParse feeds the .m3u reader whatever a library folder holds under that
 * name (docs/hardening-plan.md, item 3). A scan reads every one, so a file
 * that panics the parser stops the scan that found it.
 *
 * Beyond not panicking: every entry it returns has a path with no surrounding
 * space, and every path Resolve accepts is absolute, because the importer
 * checks containment against an absolute library root.
 */
func FuzzParse(f *testing.F) {
	for _, s := range []string{
		"#EXTM3U\n#EXTINF:123,Artist - Title\nmusic/a.mp3\n",
		"\ufeff#EXTM3U\r\n#EXTINF:-1 tvg-id=\"x\",A, with, commas\r\nC:\\Music\\b.flac\r\n",
		"#EXTINF:abc\n../../etc/passwd\n",
		"#EXTM3U\n#EXT-X-VERSION:3\nseg0.ts\n",
		"http://example.invalid/stream\n",
		"#EXTINF:\n\n\n",
		"",
		"#",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, in []byte) {
		entries, err := Parse(strings.NewReader(string(in)))
		if err != nil {
			return
		}
		base := filepath.Join(t.TempDir(), "lists")
		for _, e := range entries {
			if e.Path == "" || e.Path != strings.TrimSpace(e.Path) {
				t.Fatalf("entry path %q is empty or untrimmed", e.Path)
			}
			// A Windows drive path read on Linux is kept as a foreign absolute
			// path (TestResolveKeepsForeignAbsolutePath), which IsAbs there
			// does not count.
			if p, ok := Resolve(base, e.Path); ok && !filepath.IsAbs(p) && !hasDriveLetter(p) {
				t.Fatalf("Resolve(%q) = %q, not absolute", e.Path, p)
			}
		}
	})
}

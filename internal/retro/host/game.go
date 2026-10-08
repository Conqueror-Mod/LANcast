package host

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"lancast/internal/retro/libretro"
)

/*
 * GameFile turns the file the server sent into the file a core is given.
 *
 * Dumps are usually kept zipped, and most cores cannot open a zip: mGBA,
 * Mesen, BlastEm and Mupen64Plus-Next all declare only their own extensions.
 * RetroArch extracts for them, and so must we. A core that lists "zip" itself
 * (bsnes does) gets the archive as it is, and so does one that sets
 * block_extract, which is a core saying it wants the archive whole.
 *
 * This was missed for a whole stage because the real-core test unzipped each
 * game itself before loading it, so the one step the client skipped was the
 * one step the test did for it. The test now goes through here.
 *
 * The extension check at the end is not tidiness. A core handed a file it
 * never claimed does not fail cleanly: BlastEm given a zip took the client's
 * whole process down. Refusing here turns that into a sentence.
 *
 * dir is where an extracted file goes. Extraction is skipped when a file of
 * the right size is already there, so playing again costs nothing.
 */
func GameFile(path string, info libretro.SystemInfo, dir string) (string, error) {
	exts := extensions(info.ValidExtensions)
	ext := extOf(path)
	if ext == "zip" && !exts["zip"] && !info.BlockExtract {
		return extract(path, exts, dir)
	}
	if len(exts) > 0 && !exts[ext] {
		return "", fmt.Errorf("%s cannot open a .%s file (it opens %s)", coreName(info), ext, readable(info.ValidExtensions))
	}
	return path, nil
}

func extract(zp string, exts map[string]bool, dir string) (string, error) {
	zr, err := zip.OpenReader(zp)
	if err != nil {
		return "", fmt.Errorf("the game's zip could not be opened: %w", err)
	}
	defer zr.Close()
	var pick *zip.File
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if exts[extOf(f.Name)] {
			pick = f
			break
		}
	}
	if pick == nil {
		return "", errors.New("the game's zip holds nothing this console's core can open")
	}
	// Base name only: a name in an archive is not a path to trust.
	name := filepath.Base(filepath.FromSlash(strings.ReplaceAll(pick.Name, `\`, "/")))
	if name == "." || name == ".." || name == string(filepath.Separator) {
		return "", errors.New("the game's zip names its file oddly")
	}
	out := filepath.Join(dir, name)
	if st, err := os.Stat(out); err == nil && uint64(st.Size()) == pick.UncompressedSize64 {
		return out, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	src, err := pick.Open()
	if err != nil {
		return "", fmt.Errorf("the game's zip could not be read: %w", err)
	}
	defer src.Close()
	tmp, err := os.CreateTemp(dir, ".extract-*")
	if err != nil {
		return "", err
	}
	_, err = io.Copy(tmp, src)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("the game's zip could not be read: %w", err)
	}
	if err := os.Rename(tmp.Name(), out); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	return out, nil
}

func extensions(list string) map[string]bool {
	m := map[string]bool{}
	for _, e := range strings.Split(strings.ToLower(list), "|") {
		if e = strings.TrimSpace(strings.TrimPrefix(e, ".")); e != "" {
			m[e] = true
		}
	}
	return m
}

func extOf(p string) string {
	return strings.ToLower(strings.TrimPrefix(filepath.Ext(p), "."))
}

func coreName(info libretro.SystemInfo) string {
	if info.LibraryName != "" {
		return info.LibraryName
	}
	return "This core"
}

func readable(list string) string {
	var out []string
	for e := range strings.SplitSeq(list, "|") {
		if e != "" {
			out = append(out, "."+e)
		}
	}
	return strings.Join(out, ", ")
}

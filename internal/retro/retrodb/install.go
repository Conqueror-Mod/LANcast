package retrodb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

/*
 * Fetching the DAT files on request (ADR 0073, following ADR 0043).
 *
 * Never automatic: a media server that reaches the internet without being
 * asked has broken no phone-home. Somebody presses a button, having been told
 * what is about to be downloaded, how large it is and under which licence.
 * After that, identifying a library is entirely offline.
 *
 * Pinned to a **commit** of libretro-database, with a SHA-256 per file. A
 * moving ref would let the bytes change under a pinned checksum, and every new
 * install would then refuse to verify while existing ones carried on — a
 * fault nobody could reproduce. Newer DATs are a code change here, on purpose.
 */

// Commit is the libretro-database revision the files below were taken from.
const Commit = "fbeefcb46c2e1b20a7e2945f34a694a41b2d6f90"

// Licence of libretro-database, shown before anything is fetched.
const (
	Licence    = "CC BY-SA 4.0"
	LicenceURL = "https://creativecommons.org/licenses/by-sa/4.0/"
)

var (
	ErrChecksumMismatch = errors.New("a download did not match its expected checksum")
)

// File is one pinned DAT. URL is built from Commit and Source and is never
// caller-supplied.
type File struct {
	Name      string // the name it is stored under in the install directory
	Source    string // its path in libretro-database
	SHA256    string
	SizeBytes int64
}

// URL is where the file is fetched from.
func (f File) URL() string {
	parts := strings.Split(f.Source, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return "https://raw.githubusercontent.com/libretro/libretro-database/" + Commit + "/" + strings.Join(parts, "/")
}

// platformSet is one platform's game DAT and the metadata DATs joined to it.
type platformSet struct {
	Platform string
	Games    File
	Metadata []File
}

func set(platform, libretroName, games, gamesSHA string, gamesSize int64, meta ...File) platformSet {
	return platformSet{
		Platform: platform,
		Games: File{Name: platform + ".dat", Source: "metadat/" + games + "/" + libretroName + ".dat",
			SHA256: gamesSHA, SizeBytes: gamesSize},
		Metadata: meta,
	}
}

func meta(platform, kind, libretroName, sha string, size int64) File {
	return File{Name: platform + "-" + kind + ".dat", Source: "metadat/" + kind + "/" + libretroName + ".dat",
		SHA256: sha, SizeBytes: size}
}

/*
 * platformFiles is the pinned set: No-Intro for cartridges, Redump for the
 * PlayStation, and libretro's release-year, genre and ESRB DATs beside each.
 * PS1 has no year or genre DAT at this commit; Redump carries its years
 * inline. The ESRB DATs are uneven — thousands of 8- and 16-bit games, one
 * N64 game — and are pinned anyway, because a rating is what lets an account
 * with a content ceiling see a game at all.
 */
var platformFiles = []platformSet{
	set("nes", "Nintendo - Nintendo Entertainment System", "no-intro",
		"853ffc356c664d72ca6c17a8382a2d8dd533dfd8a7e4644b1438ec6979e621e1", 3308190,
		meta("nes", "releaseyear", "Nintendo - Nintendo Entertainment System", "75f3c80a816fee115920eb9d2c63c97f6cbc15f50e8563f1aa982f9a1877311c", 218336),
		meta("nes", "genre", "Nintendo - Nintendo Entertainment System", "931e910cc48b43861c7e7250273d2015350e38f23f8d4d33748737a8830fb2f6", 319291),
		meta("nes", "esrb", "Nintendo - Nintendo Entertainment System", "88c2c4371b56d5c9ab9e43834e7f024321ff1456a35b8f3504ab42ddaf4e705a", 54234)),
	set("snes", "Nintendo - Super Nintendo Entertainment System", "no-intro",
		"175c69cc8b50dc60c84828ad570e581a7c03602518776540733636235aa0ff8a", 987445,
		meta("snes", "releaseyear", "Nintendo - Super Nintendo Entertainment System", "a38acd1af1d9f2aa9ebe9efaa74d2335fdb40aefff8512c9fd1dedf8577d3998", 322971),
		meta("snes", "genre", "Nintendo - Super Nintendo Entertainment System", "92f0ed22dfecf2bb8338f5680dd0b5556c70e6a63ab9fe36285d8c945649f67e", 370035),
		meta("snes", "esrb", "Nintendo - Super Nintendo Entertainment System", "b9d92c83115325ef0c631ad53ac785c73f0e4427b147e25772b969a4f0dc1e65", 107619)),
	set("n64", "Nintendo - Nintendo 64", "no-intro",
		"eb20ef6164e86eee57b049a5f45bac21cb36a40e5edab26c44db19e4ec47ac8b", 369835,
		meta("n64", "releaseyear", "Nintendo - Nintendo 64", "20b37b55fbfd9a382118dbebb3570885e1521920dad056bc7d8a93623e124ecf", 182568),
		meta("n64", "genre", "Nintendo - Nintendo 64", "ed12f13a9fbc8d4debe449fccc92b6a64803873b0461fdd3caef2780013af143", 196745),
		meta("n64", "esrb", "Nintendo - Nintendo 64", "5f271ecdb010563d4e746614d562968e010d1c7fae8dbf9d34d6ad67011170e7", 170)),
	set("gb", "Nintendo - Game Boy", "no-intro",
		"6fa0187a63666d9cdf5af9bb50baeca392f9f2195bcb2371e56c0a00e62989de", 530791,
		meta("gb", "releaseyear", "Nintendo - Game Boy", "3b36730fd4a3974a12d7406327883d7d42bc1a93f62e62b2334ecdd51680dec3", 92230),
		meta("gb", "genre", "Nintendo - Game Boy", "3d88c5579fdb2f9c6d828170defe34b89cd855792fbb937e3c6f50a51c4b63c2", 166649),
		meta("gb", "esrb", "Nintendo - Game Boy", "a28156a52a4c40f2509eee3012ba4c93d0181ed020dbf0c0fd4dd14528f44023", 36086)),
	set("gbc", "Nintendo - Game Boy Color", "no-intro",
		"f884fb411b10cee473f5e5cd0aff7e3b485d1f19dc40d893b5c2d0a6d52d6a7e", 667128,
		meta("gbc", "releaseyear", "Nintendo - Game Boy Color", "a89f8dedb4d231c473a3189b27a1f0073ec03cb6917842c44772370d66f67e56", 112137),
		meta("gbc", "genre", "Nintendo - Game Boy Color", "affde021868fa8042e360cd910e85e320953d9ab6c49ed0d565bbadd581a30a0", 170421),
		meta("gbc", "esrb", "Nintendo - Game Boy Color", "716d562477ea2d203dd567257332d504ea4f218c712916c61d65709466cdb087", 95910)),
	set("gba", "Nintendo - Game Boy Advance", "no-intro",
		"90188f6e4e481cb2be98eda6271844b7f56813575497bebaaeddf7b76a6353db", 1014501,
		meta("gba", "releaseyear", "Nintendo - Game Boy Advance", "80a10952a808809cb998953eeecac45c4f10c2a12fe4d355a18858a681967022", 225711),
		meta("gba", "genre", "Nintendo - Game Boy Advance", "d819fa478a9376076df91bd3762a3c0ce84ba28cf0f5d6f5bdcde09f5923fbc3", 330976),
		meta("gba", "esrb", "Nintendo - Game Boy Advance", "889365e1a4add6eeddec32d9d3ac0da38df22a30a8dedba517958945b20b6029", 191084)),
	set("sms", "Sega - Master System - Mark III", "no-intro",
		"f383714cf27e699eb6fa1df2b5bfa137dc036da0267f1b3eaa2eaf7a0d2f1a4a", 266518,
		meta("sms", "releaseyear", "Sega - Master System - Mark III", "c05ec368088ddba3b1717b8328ddfe236cdd31ebbcefea7b0403f9234a99fd8b", 22884),
		meta("sms", "genre", "Sega - Master System - Mark III", "45f2ffd52f355a47cc4bd3121ec1bf24c5cd9882174e0afaeafd6d6682191ecb", 60713),
		meta("sms", "esrb", "Sega - Master System - Mark III", "319ed000931739ad306dd9c2470aad820814a06ebbd67663683972e087f6570f", 10347)),
	set("genesis", "Sega - Mega Drive - Genesis", "no-intro",
		"d858f1ddffbc82eda9d7295fb790d1e87d156dc10385a02a2c343c01cafde349", 913828,
		meta("genesis", "releaseyear", "Sega - Mega Drive - Genesis", "2f8b2a9c58eb2b75e48b09192f3526dcd79c70444810ae3c0f61dd7cbf70ec70", 175858),
		meta("genesis", "genre", "Sega - Mega Drive - Genesis", "efcba66ce8ee19d2049f6c61df3430f2d636d1a4aed8b43f821c2f90eb74ba6e", 258365),
		meta("genesis", "esrb", "Sega - Mega Drive - Genesis", "db702120eee175797097feae8fc122b55ea51e611d3ca74fef8996cea3687849", 77257)),
	set("ps1", "Sony - PlayStation", "redump",
		"55453a532d8a659b3723eaf818bce83afea2ba926b3a0021adc904d54f46ab11", 3929602,
		meta("ps1", "esrb", "Sony - PlayStation", "2669857e0e7d6fc502f434d1dae5f148db6e0dabb7e5808ff52ad25a715280d4", 645)),
}

// Files is every pinned file, in download order.
func Files() []File {
	var out []File
	for _, p := range platformFiles {
		out = append(out, p.Games)
		out = append(out, p.Metadata...)
	}
	return out
}

// TotalBytes is what the UI shows before the first byte arrives.
func TotalBytes() int64 {
	var n int64
	for _, f := range Files() {
		n += f.SizeBytes
	}
	return n
}

// manifestName is written last, holding Commit. Its presence is what
// "installed" means, so a partial install counts as absent (ADR 0043).
const manifestName = "INSTALLED"

// Installed reports whether a complete install of this build's pinned set is
// in dir. An install of a different commit is not this one: it is offered
// again, and replacing it re-identifies the library.
func Installed(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, manifestName))
	return err == nil && strings.TrimSpace(string(b)) == Commit
}

// Stage names the step in progress.
type Stage string

const (
	StageDownloading Stage = "downloading"
	StageVerifying   Stage = "verifying"
	StageInstalling  Stage = "installing"
)

// Progress is how far an install has got, across every file.
type Progress struct {
	Stage      Stage
	File       string
	BytesDone  int64
	BytesTotal int64
}

var client = &http.Client{Timeout: 0}

// StallTimeout abandons a connection that has delivered nothing for this long.
const StallTimeout = 60 * time.Second

// fileURL is replaced in tests. Production fetches only Commit-pinned
// URLs on raw.githubusercontent.com.
var fileURL = func(f File) string { return f.URL() }

/*
 * Install fetches every pinned file into dir.
 *
 * Everything is downloaded into a staging directory beside dir and verified
 * there; only when every file has checked out is the staging directory
 * swapped in and the manifest written. A failure at file 20 of 26 leaves the
 * previous install, if any, exactly as it was.
 */
func Install(ctx context.Context, dir string, report func(Progress)) error {
	staging := dir + ".part"
	if err := os.RemoveAll(staging); err != nil {
		return err
	}
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			os.RemoveAll(staging)
		}
	}()

	total := TotalBytes()
	var done int64
	for _, f := range Files() {
		if err := fetch(ctx, f, staging, done, total, report); err != nil {
			return fmt.Errorf("%s: %w", f.Source, err)
		}
		done += f.SizeBytes
	}
	if report != nil {
		report(Progress{Stage: StageInstalling, BytesDone: done, BytesTotal: total})
	}

	old := dir + ".old"
	os.RemoveAll(old)
	if _, err := os.Stat(dir); err == nil {
		if err := os.Rename(dir, old); err != nil {
			return err
		}
	}
	if err := os.Rename(staging, dir); err != nil {
		// Put the previous install back rather than leave nothing.
		os.Rename(old, dir)
		return err
	}
	os.RemoveAll(old)
	if err := os.WriteFile(filepath.Join(dir, manifestName), []byte(Commit+"\n"), 0o644); err != nil {
		return err
	}
	ok = true
	return nil
}

func fetch(ctx context.Context, f File, dir string, done, total int64, report func(Progress)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL(f), nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned %s", resp.Status)
	}

	out, err := os.Create(filepath.Join(dir, f.Name))
	if err != nil {
		return err
	}
	defer out.Close()

	// A stalled connection is abandoned; a slow one is not.
	stall := time.AfterFunc(StallTimeout, cancel)
	defer stall.Stop()

	sum := sha256.New()
	var read int64
	buf := make([]byte, 128<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			stall.Reset(StallTimeout)
			if _, err := out.Write(buf[:n]); err != nil {
				return err
			}
			sum.Write(buf[:n])
			read += int64(n)
			if read > f.SizeBytes {
				return fmt.Errorf("%w: longer than pinned", ErrChecksumMismatch)
			}
			if report != nil {
				report(Progress{Stage: StageDownloading, File: f.Name, BytesDone: done + read, BytesTotal: total})
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			if ctx.Err() != nil && ctx.Err() != context.Canceled {
				return ctx.Err()
			}
			return rerr
		}
	}
	if report != nil {
		report(Progress{Stage: StageVerifying, File: f.Name, BytesDone: done + read, BytesTotal: total})
	}
	if got := hex.EncodeToString(sum.Sum(nil)); got != f.SHA256 {
		return fmt.Errorf("%w: got %s", ErrChecksumMismatch, got)
	}
	return nil
}

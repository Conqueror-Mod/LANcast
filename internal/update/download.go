package update

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"lancast/internal/release"
	"lancast/internal/selfupdate"
)

// maxArtifact bounds what will be read into memory. The Windows archive is
// about 15 MB; this is generous enough to survive growth and small enough that
// a redirect to something enormous cannot exhaust the machine.
const maxArtifact = 256 << 20

// installFiles are the names taken out of the archive and staged. Named rather
// than "everything in the zip": the archive also carries a README and a licence,
// and an updater that overwrites files nobody asked it to touch is an updater
// nobody trusts.
var installFiles = map[string]bool{
	"LANcast-Server.exe": true,
	"LANcast-Client.exe": true,
	"WebView2Loader.dll": true,
	"lancastd":           true,
	"lancast":            true,
}

// Progress is what the activity panel renders while a download runs.
type Progress struct {
	Active bool   `json:"active"`
	Done   int64  `json:"done"`
	Total  int64  `json:"total"`
	Stage  string `json:"stage,omitempty"`
}

// Progress returns the current download state.
func (c *Checker) Progress() Progress {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.progress
}

func (c *Checker) setProgress(p Progress) {
	c.mu.Lock()
	c.progress = p
	c.mu.Unlock()
}

// DownloadAndStage fetches the newest release, proves it, and stages it for the
// next restart.
//
// The order matters and is the whole security argument: the signature is
// checked before the archive is trusted, the archive is checked against the
// signed digest before it is opened, and nothing is written into the install
// directory at all — staging goes to the data directory, and the swap happens
// on shutdown (internal/selfupdate).
func (c *Checker) DownloadAndStage(ctx context.Context, dataDir string) error {
	return c.downloadFrom(ctx, dataDir, releasesURL)
}

// downloadFrom is DownloadAndStage against a given endpoint, so tests can serve
// their own release. The exported path always uses the constant — an endpoint a
// caller could choose would be a way to hand someone a malicious update, and
// the signature check narrows that risk rather than removing it.
func (c *Checker) downloadFrom(ctx context.Context, dataDir, endpoint string) error {
	if !release.Signable() {
		// Refusing is the point. A build that cannot verify a release must not
		// install one, no matter how the request arrived.
		return fmt.Errorf("this build cannot verify a release signature, so it will not install one")
	}

	c.setProgress(Progress{Active: true, Stage: "looking up the release"})
	defer func() { c.setProgress(Progress{}) }()

	rel, err := c.fetchRelease(ctx, endpoint)
	if err != nil {
		return err
	}
	if !Newer(c.current, rel.TagName) {
		return fmt.Errorf("no newer release than %s", c.current)
	}

	archive := archiveName(rel.TagName)
	urls := map[string]string{}
	for _, a := range rel.Assets {
		urls[a.Name] = a.BrowserDownloadURL
	}
	for _, need := range []string{"checksums.txt", "checksums.txt.sig", archive} {
		if urls[need] == "" {
			if need == "checksums.txt.sig" {
				return fmt.Errorf("release %s is not signed, so it will not be installed automatically", rel.TagName)
			}
			return fmt.Errorf("release %s has no %s", rel.TagName, need)
		}
	}

	c.setProgress(Progress{Active: true, Stage: "verifying the release"})
	checksums, err := c.get(ctx, urls["checksums.txt"], nil)
	if err != nil {
		return err
	}
	sig, err := c.get(ctx, urls["checksums.txt.sig"], nil)
	if err != nil {
		return err
	}
	verified, err := release.VerifyChecksums(checksums, sig)
	if err != nil {
		return fmt.Errorf("release %s: %w", rel.TagName, err)
	}

	c.setProgress(Progress{Active: true, Stage: "downloading " + rel.TagName})
	var done atomic.Int64
	body, err := c.get(ctx, urls[archive], func(n int64, total int64) {
		done.Store(n)
		c.setProgress(Progress{Active: true, Done: n, Total: total,
			Stage: "downloading " + rel.TagName})
	})
	if err != nil {
		return err
	}
	if err := verified.CheckArtifact(archive, body); err != nil {
		return fmt.Errorf("release %s: %w", rel.TagName, err)
	}

	c.setProgress(Progress{Active: true, Stage: "staging " + rel.TagName})
	files, err := extract(body)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("release %s contained none of the files this install uses", rel.TagName)
	}
	return selfupdate.Stage(dataDir, rel.TagName, files, time.Now().Unix())
}

// archiveName is the asset this platform installs from. goreleaser's template
// is lancast_<version>_<os>_<arch>, with the leading v stripped from the tag.
func archiveName(tag string) string {
	v := strings.TrimPrefix(tag, "v")
	if runtime.GOOS == "windows" {
		return fmt.Sprintf("lancast_%s_windows_%s.zip", v, runtime.GOARCH)
	}
	return fmt.Sprintf("lancast_%s_%s_%s.tar.gz", v, runtime.GOOS, runtime.GOARCH)
}

// extract pulls the install files out of a zip.
//
// Only names in installFiles, and only bare names: a zip entry with a path is
// refused rather than flattened, because an archive is an untrusted container
// even when its digest is signed — the signature says the project built it, not
// that every entry is sane.
func extract(body []byte) (map[string][]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, fmt.Errorf("release archive: %w", err)
	}
	out := map[string][]byte{}
	for _, f := range zr.File {
		name := f.Name
		if name != path.Base(name) || !installFiles[name] {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("release archive: %s: %w", name, err)
		}
		data, err := io.ReadAll(io.LimitReader(rc, maxArtifact))
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("release archive: %s: %w", name, err)
		}
		out[name] = data
	}
	return out, nil
}

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func (c *Checker) fetchRelease(ctx context.Context, endpoint string) (*ghReleaseFull, error) {
	body, err := c.get(ctx, endpoint, nil)
	if err != nil {
		return nil, err
	}
	var rel ghReleaseFull
	if err := json.Unmarshal(body, &rel); err != nil {
		return nil, err
	}
	if rel.Draft || rel.Prerelease {
		return nil, fmt.Errorf("releases: latest is a draft or prerelease")
	}
	return &rel, nil
}

type ghReleaseFull struct {
	TagName    string    `json:"tag_name"`
	Draft      bool      `json:"draft"`
	Prerelease bool      `json:"prerelease"`
	Assets     []ghAsset `json:"assets"`
}

// get fetches a URL, optionally reporting progress. Bounded by maxArtifact so a
// redirect to something enormous cannot exhaust memory.
func (c *Checker) get(ctx context.Context, url string, onProgress func(done, total int64)) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/octet-stream")

	// A download needs longer than a status check; the client's own short
	// timeout would cut a 15 MB archive off on a slow line.
	client := &http.Client{Timeout: 15 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", path.Base(url), resp.Status)
	}

	var r io.Reader = io.LimitReader(resp.Body, maxArtifact)
	if onProgress != nil {
		r = &progressReader{r: r, total: resp.ContentLength, report: onProgress}
	}
	return io.ReadAll(r)
}

type progressReader struct {
	r      io.Reader
	total  int64
	done   int64
	report func(done, total int64)
	last   time.Time
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.done += int64(n)
	// Throttled: a progress callback per read would be thousands of updates for
	// one download, and the panel polls far slower than that anyway.
	if time.Since(p.last) > 250*time.Millisecond {
		p.last = time.Now()
		p.report(p.done, p.total)
	}
	return n, err
}

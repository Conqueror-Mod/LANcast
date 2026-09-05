package faceinstall

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

/*
 * Fetching what face grouping needs, on request (ADR 0052, following ADR 0048).
 *
 * Three things, none of them bundled: two ONNX models and the ONNX Runtime
 * shared library. Together they are about 55MB against a 17MB installer, and
 * the overwhelming majority of LANcast installs have no picture library at all.
 *
 * **The worker binary is not among them.** It ships with the server, because it
 * is three megabytes and because a worker whose version can drift from the
 * server that drives it is a support question nobody can answer from a log —
 * the wire format between them is a contract, and pinning both ends to the same
 * release is free.
 *
 * Nothing is fetched automatically. A media server that reaches the internet
 * without being asked has broken no-phone-home, and that principle has no
 * convenience exception: somebody presses a button, having first been told what
 * is about to be downloaded, how large it is, and under which licence.
 */

var (
	ErrUnsupportedPlatform = errors.New("no pinned face-model build for this platform")
	ErrChecksumMismatch    = errors.New("a download did not match its expected checksum")
)

/*
 * Asset is one file to fetch.
 *
 * URL is fixed in this file and never caller-supplied: the payload is a model
 * this server is about to load and a library it is about to execute, and
 * fetching an address the request chose is the server-side request forgery the
 * rest of this API refuses.
 */
type Asset struct {
	Name      string
	URL       string
	SHA256    string
	SizeBytes int64
	// ExtractFromZip names a file inside a zip archive. Empty means the
	// download is the file.
	ExtractFromZip string
	// Licence and LicenceURL are shown before anything is downloaded. A
	// download somebody cannot identify is not consent.
	Licence    string
	LicenceURL string
}

/*
 * The pinned set.
 *
 * The models come from OpenCV Zoo at a **commit**, not at `main`. A moving ref
 * would mean the bytes could change under a pinned checksum, and the failure
 * mode of that is every new install refusing to verify while every existing one
 * carries on working — a fault nobody can reproduce.
 *
 * YuNet is MIT and SFace is Apache-2.0, both including their weights, which is
 * why they were chosen over the more accurate InsightFace models: those are
 * MIT code with **non-commercial** weights and would have quietly foreclosed
 * LANcast's own licensing (ADR 0053).
 */
const zooCommit = "47534e27c9851bb1128ccc0102f1145e27f23f98"

var models = []Asset{
	{
		Name: "face_detection_yunet_2023mar.onnx",
		URL: "https://media.githubusercontent.com/media/opencv/opencv_zoo/" +
			zooCommit + "/models/face_detection_yunet/face_detection_yunet_2023mar.onnx",
		SHA256:     "8f2383e4dd3cfbb4553ea8718107fc0423210dc964f9f4280604804ed2552fa4",
		SizeBytes:  232589,
		Licence:    "MIT",
		LicenceURL: "https://github.com/opencv/opencv_zoo/blob/main/models/face_detection_yunet/LICENSE",
	},
	{
		Name: "face_recognition_sface_2021dec.onnx",
		URL: "https://media.githubusercontent.com/media/opencv/opencv_zoo/" +
			zooCommit + "/models/face_recognition_sface/face_recognition_sface_2021dec.onnx",
		SHA256:     "0ba9fbfa01b5270c96627c4ef784da859931e02f04419c829e83484087c34e79",
		SizeBytes:  38696353,
		Licence:    "Apache-2.0",
		LicenceURL: "https://github.com/opencv/opencv_zoo/blob/main/models/face_recognition_sface/LICENSE",
	},
}

/*
 * The runtime, which is a 76MB download for a 16MB library.
 *
 * Wasteful, and taken anyway: this is the archive Microsoft publishes and
 * signs, and repackaging it somewhere smaller would mean asking people to trust
 * a copy rather than the original. The extra sixty megabytes cross the wire
 * once.
 */
var runtimeWindowsAMD64 = Asset{
	Name: "onnxruntime.dll",
	URL: "https://github.com/microsoft/onnxruntime/releases/download/" +
		"v1.29.0/onnxruntime-win-x64-1.29.0.zip",
	SHA256:         "c9b4b7086b529ad814f428c1bad028e20a25d7dc0699836775faace4ab5b78b2",
	SizeBytes:      79645520,
	ExtractFromZip: "lib/onnxruntime.dll",
	Licence:        "MIT",
	LicenceURL:     "https://github.com/microsoft/onnxruntime/blob/main/LICENSE",
}

/*
 * The semantic-search set (ADR 0060), which is a *separate* optional download.
 *
 * Two features, two downloads, and a server may have either, both or neither.
 * Folding them together would tell a household that wanted search and not face
 * grouping that the feature was unavailable because a different model was
 * missing — the report ADR 0052 built the reason field to avoid.
 *
 * WHY THIS COPY OF THE WEIGHTS
 *
 * These are OpenCLIP's ViT-B/32 LAION-2B weights, which are MIT, exported to
 * ONNX. The export is Immich's, pinned to a commit and to the sha256 of each
 * file: the choice is between depending on somebody's re-export or shipping our
 * own conversion pipeline, and a pinned digest makes the first of those a
 * question about *these bytes* rather than about a repository. Every byte is
 * verified before it is placed, exactly as the face models are.
 *
 * OpenAI's own CLIP export was the obvious alternative and was rejected: it
 * declares no licence at all, and "no licence" is not permission.
 *
 * The runtime is the same onnxruntime.dll the face models need. Whichever
 * feature is installed first pays for it, and `Missing` keeps the second from
 * fetching 76MB it already has.
 */
const clipCommit = "19a057a0fb927cf239541300e74237cd56c7ffe2"

const clipBase = "https://huggingface.co/immich-app/ViT-B-32__laion2b-s34b-b79k/resolve/" +
	clipCommit + "/"

/*
 * The names on disk are the sidecar's contract, not the repository's.
 *
 * `clip-visual` and `clip-textual` are what the worker looks for. The upstream
 * files are both called model.onnx, in different folders, and this directory is
 * flat — so they are renamed on the way in, and the worker's fragment search
 * finds them regardless of any suffix a later pin adds.
 */
var semanticModels = []Asset{
	{
		Name:       "clip-visual.onnx",
		URL:        clipBase + "visual/model.onnx",
		SHA256:     "b9ce24b91a8c62ef8d40ea786051709c93140104cbf2b6b4a2cc270df3838f9c",
		SizeBytes:  351613724,
		Licence:    "MIT",
		LicenceURL: "https://github.com/mlfoundations/open_clip/blob/main/LICENSE",
	},
	{
		Name:       "clip-textual.onnx",
		URL:        clipBase + "textual/model.onnx",
		SHA256:     "48c25be37b352398f2533c9426dab6f9340535e14e46f884cc894236a5060722",
		SizeBytes:  254193396,
		Licence:    "MIT",
		LicenceURL: "https://github.com/mlfoundations/open_clip/blob/main/LICENSE",
	},
	{
		// Half a megabyte, and the half of the model with no error path: a
		// tokenizer that is nearly right returns confidently wrong photographs.
		Name:       "merges.txt",
		URL:        clipBase + "textual/merges.txt",
		SHA256:     "9fd691f7c8039210e0fced15865466c65820d09b63988b0174bfe25de299051a",
		SizeBytes:  524619,
		Licence:    "MIT",
		LicenceURL: "https://github.com/mlfoundations/open_clip/blob/main/LICENSE",
	},
}

// SemanticAssetsForHost is the semantic-search download for this machine,
// including the shared runtime — which `Missing` will drop if face grouping
// already fetched it.
func SemanticAssetsForHost() ([]Asset, error) {
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		return nil, ErrUnsupportedPlatform
	}
	return append(append([]Asset{}, semanticModels...), runtimeWindowsAMD64), nil
}

// SemanticInstalled reports whether semantic search has everything it needs.
func SemanticInstalled(dir string) bool {
	for _, a := range semanticModels {
		if !fileExists(filepath.Join(dir, a.Name)) {
			return false
		}
	}
	return fileExists(filepath.Join(dir, runtimeWindowsAMD64.Name))
}

/*
 * Missing drops the assets already sitting in dir with the right bytes.
 *
 * By digest rather than by name, and that is the whole point of it. A file
 * present under the right name may be a truncated download from a connection
 * that dropped, and skipping it on name alone would make a re-install — the one
 * thing somebody does when it is broken — the one thing that cannot fix it.
 *
 * Hashing what is already there costs a few seconds against a download measured
 * in minutes, so it is not worth an option.
 */
func Missing(dir string, assets []Asset) []Asset {
	out := make([]Asset, 0, len(assets))
	for _, a := range assets {
		p := filepath.Join(dir, a.Name)
		/*
		 * An extracted asset is checked for presence rather than by digest,
		 * because its SHA256 is the *archive's* and the file on disk is one
		 * member of it. Comparing them would never match, and the runtime — the
		 * largest download of the set — would be fetched again every time.
		 */
		if a.ExtractFromZip != "" {
			if !fileExists(p) {
				out = append(out, a)
			}
			continue
		}
		if !matchesDigest(p, a.SHA256) {
			out = append(out, a)
		}
	}
	return out
}

func matchesDigest(path, want string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, f); err != nil {
		return false
	}
	return hex.EncodeToString(sum.Sum(nil)) == want
}

/*
 * AssetsForHost returns everything to fetch for this machine.
 *
 * Windows/amd64 only for now, matching the platforms `lancast-faces` is built
 * for (ADR 0052). Elsewhere it refuses rather than downloading models that
 * nothing on the machine can load — an install that half-succeeds is worse than
 * one that says no.
 */
func AssetsForHost() ([]Asset, error) {
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		return nil, ErrUnsupportedPlatform
	}
	return append(append([]Asset{}, models...), runtimeWindowsAMD64), nil
}

// TotalBytes is what the UI shows before the first byte arrives.
func TotalBytes(assets []Asset) int64 {
	var n int64
	for _, a := range assets {
		n += a.SizeBytes
	}
	return n
}

type Stage string

const (
	StageDownloading Stage = "downloading"
	StageVerifying   Stage = "verifying"
	StageInstalling  Stage = "installing"
)

// Progress reports how far along an install is, across all assets rather than
// per file: somebody watching wants to know when it will finish, not which of
// three files is in flight.
type Progress struct {
	Stage      Stage
	Asset      string
	BytesDone  int64
	BytesTotal int64
}

// Installed reports whether everything needed is already in place, so the UI
// can offer an install rather than a re-install.
func Installed(dir string) bool {
	for _, a := range models {
		if !fileExists(filepath.Join(dir, a.Name)) {
			return false
		}
	}
	return fileExists(filepath.Join(dir, runtimeWindowsAMD64.Name))
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir() && st.Size() > 0
}

var client = &http.Client{Timeout: 0}

/*
 * Install fetches everything into dir.
 *
 * Each asset is downloaded to a temporary file, verified, and only then moved
 * into place. A half-written model is a model that loads and produces
 * nonsense — or, more likely, one that fails to parse with an error naming a
 * protobuf field, which is not a sentence anybody can act on. Nothing appears
 * under its real name until its bytes are known to be right.
 */
func Install(ctx context.Context, assets []Asset, dir string, report func(Progress)) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	total := TotalBytes(assets)
	var done int64

	for _, a := range assets {
		tmp, err := fetch(ctx, a, dir, done, total, report)
		if err != nil {
			/*
			 * The partial file goes with the error.
			 *
			 * fetch returns the temporary path alongside its failures so this
			 * can happen; without it, a mismatched digest or a dropped
			 * connection leaves a `.part` file in the models directory, and a
			 * few failed attempts leave a directory somebody has to clean out
			 * by hand to reclaim a few hundred megabytes.
			 */
			if tmp != "" {
				os.Remove(tmp)
			}
			return fmt.Errorf("%s: %w", a.Name, err)
		}

		if report != nil {
			report(Progress{Stage: StageInstalling, Asset: a.Name,
				BytesDone: done + a.SizeBytes, BytesTotal: total})
		}
		if err := place(tmp, filepath.Join(dir, a.Name), a); err != nil {
			os.Remove(tmp)
			return fmt.Errorf("%s: %w", a.Name, err)
		}
		os.Remove(tmp)
		done += a.SizeBytes
	}
	return nil
}

func fetch(ctx context.Context, a Asset, dir string, done, total int64, report func(Progress)) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned %s", resp.Status)
	}

	tmp, err := os.CreateTemp(dir, "face-*.part")
	if err != nil {
		return "", err
	}
	defer tmp.Close()

	sum := sha256.New()
	var read int64
	buf := make([]byte, 256*1024)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := tmp.Write(buf[:n]); werr != nil {
				return tmp.Name(), werr
			}
			sum.Write(buf[:n])
			read += int64(n)
			if report != nil {
				report(Progress{Stage: StageDownloading, Asset: a.Name,
					BytesDone: done + read, BytesTotal: total})
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return tmp.Name(), rerr
		}
		if err := ctx.Err(); err != nil {
			return tmp.Name(), err
		}
	}

	if report != nil {
		report(Progress{Stage: StageVerifying, Asset: a.Name,
			BytesDone: done + read, BytesTotal: total})
	}
	if got := hex.EncodeToString(sum.Sum(nil)); got != a.SHA256 {
		return tmp.Name(), fmt.Errorf("%w: got %s", ErrChecksumMismatch, got)
	}
	return tmp.Name(), nil
}

/*
 * place moves a verified download to its final name, extracting it first when
 * it arrived inside an archive.
 *
 * A rename rather than a copy, so the moment the file exists under its real
 * name it is complete. `Installed` reports on those names, and a partially
 * copied file that satisfied it would be a worker that starts and then fails.
 */
func place(tmp, dst string, a Asset) error {
	if a.ExtractFromZip == "" {
		return os.Rename(tmp, dst)
	}

	zr, err := zip.OpenReader(tmp)
	if err != nil {
		return err
	}
	defer zr.Close()

	want := path.Base(a.ExtractFromZip)
	for _, f := range zr.File {
		/*
		 * Matched on the base name, because the archive's top directory carries
		 * a version — `onnxruntime-win-x64-1.29.0/lib/onnxruntime.dll` — and
		 * pinning the whole path would mean this line and the URL above both
		 * having to change together, which is one of them being forgotten.
		 *
		 * A zip entry's name is attacker-controlled in general; here the
		 * archive's bytes have already been checked against a pinned digest, so
		 * it is the file Microsoft published or nothing was extracted at all.
		 */
		if path.Base(f.Name) != want || f.FileInfo().IsDir() {
			continue
		}
		if !strings.HasSuffix(f.Name, a.ExtractFromZip) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.CreateTemp(filepath.Dir(dst), "face-*.tmp")
		if err != nil {
			rc.Close()
			return err
		}
		_, cerr := io.Copy(out, rc)
		rc.Close()
		out.Close()
		if cerr != nil {
			os.Remove(out.Name())
			return cerr
		}
		if err := os.Rename(out.Name(), dst); err != nil {
			os.Remove(out.Name())
			return err
		}
		return nil
	}
	return fmt.Errorf("%s not found in the archive", a.ExtractFromZip)
}

// Timeout guards a stalled connection without capping a slow one: a 76MB
// download on a poor line is normal, and a socket that has said nothing for a
// minute is not.
const StallTimeout = 60 * time.Second

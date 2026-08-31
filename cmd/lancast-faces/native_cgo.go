//go:build cgo

package main

import (
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

/*
 * The cgo path: native inference (ADR 0052).
 *
 * The C symbol below is not decoration. This binary exists to carry native
 * inference, and the thing that had to be proven first was that a cgo binary
 * cross-compiles and publishes from a Linux runner. A Go file that merely
 * *could* use cgo proves nothing: with no C symbol the toolchain is never
 * invoked and mingw is never exercised. It reports the compiler the artefact
 * was built with, which is the one fact a cross-compiled binary cannot fake.
 */

// #include <stdlib.h>
//
// static const char* lancast_native_id(void) {
// #if defined(__clang__)
//   return "clang " __clang_version__;
// #elif defined(__GNUC__)
//   return "gcc " __VERSION__;
// #else
//   return "unknown C toolchain";
// #endif
// }
import "C"

const hasCGO = true

func nativeInfo() string {
	return C.GoString(C.lancast_native_id())
}

/*
 * The model geometry, read from the models themselves rather than assumed.
 *
 * `GetInputOutputInfo` on the real files reports YuNet taking a fixed
 * [1 3 640 640] and emitting twelve heads — cls/obj/bbox/kps at strides 8, 16
 * and 32, with 6400, 1600 and 400 cells, which are exactly 80², 40² and 20².
 * SFace takes [1 3 112 112] and returns [1 128].
 *
 * These are constants rather than discovered at runtime because they are fixed
 * in the model file: discovering them would be code that reads a number it
 * already knows and cannot act on if it differs. What *is* checked at runtime
 * is that the tensors are the length this implies — see DecodeStride, which
 * returns nothing rather than indexing past the end.
 */
const (
	detectSize = 640
	embedSize  = 112
	embedDims  = 128
)

var strides = []int{8, 16, 32}

/*
 * Detector confidence, and the reason it is low.
 *
 * 0.6 on the product of the two heads. A face in a holiday photograph is often
 * small, half-turned and badly lit, and a high bar quietly drops exactly the
 * pictures a person most wants found — the candid ones. A false positive costs
 * a cluster somebody ignores; a false negative costs a photograph that can
 * never be found by the person in it.
 */
const minDetectScore = 0.6

// NMS threshold, matching OpenCV's for this detector.
const nmsIoU = 0.3

/*
 * The models, loaded once.
 *
 * A session is expensive to build and cheap to reuse, and this binary is handed
 * a whole library on stdin — building a session per photograph would spend more
 * time loading a 38MB file than looking at pictures.
 */
type engine struct {
	detector *ort.DynamicAdvancedSession
	embedder *ort.DynamicAdvancedSession
}

var (
	engineOnce sync.Once
	loaded     *engine
	loadErr    error
)

// findModel locates a model by a distinguishing fragment of its name, so a
// newer release of the same model (yunet_2026may, say) is picked up without an
// edit here — the version is the model's business, not this file's.
func findModel(dir, fragment string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() && filepath.Ext(n) == ".onnx" &&
			len(n) > len(fragment) && contains(n, fragment) {
			return filepath.Join(dir, n), nil
		}
	}
	return "", fmt.Errorf("no %s model in %s", fragment, dir)
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func load(modelsDir string) (*engine, error) {
	engineOnce.Do(func() {
		if lib := os.Getenv("LANCAST_ONNXRUNTIME"); lib != "" {
			// The runtime is an optional download beside the models, exactly as
			// ffmpeg is (ADR 0048). Naming it explicitly beats relying on it
			// being wherever the loader happens to look.
			ort.SetSharedLibraryPath(lib)
		}
		if err := ort.InitializeEnvironment(); err != nil {
			loadErr = fmt.Errorf("start onnxruntime: %w", err)
			return
		}
		det, err := findModel(modelsDir, "yunet")
		if err != nil {
			loadErr = err
			return
		}
		emb, err := findModel(modelsDir, "sface")
		if err != nil {
			loadErr = err
			return
		}

		outputs := make([]string, 0, len(strides)*4)
		for _, head := range []string{"cls", "obj", "bbox", "kps"} {
			for _, s := range strides {
				outputs = append(outputs, fmt.Sprintf("%s_%d", head, s))
			}
		}
		d, err := ort.NewDynamicAdvancedSession(det, []string{"input"}, outputs, nil)
		if err != nil {
			loadErr = fmt.Errorf("load detector: %w", err)
			return
		}
		e, err := ort.NewDynamicAdvancedSession(emb, []string{"data"}, []string{"fc1"}, nil)
		if err != nil {
			loadErr = fmt.Errorf("load embedder: %w", err)
			return
		}
		loaded = &engine{detector: d, embedder: e}
	})
	return loaded, loadErr
}

// detectOne runs the whole pipeline over one photograph.
func detectOne(path, modelsDir string) ([]Face, error) {
	if modelsDir == "" {
		return nil, errNoModel
	}
	eng, err := load(modelsDir)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	src, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		// Not fatal to the batch: a library contains a truncated JPEG
		// eventually, and stopping there means the pass never finishes.
		return nil, fmt.Errorf("decode: %w", err)
	}

	boxed, scale, padX, padY := Letterbox(src, detectSize)
	inTensor, err := ort.NewTensor(ort.NewShape(1, 3, detectSize, detectSize), Tensor(boxed))
	if err != nil {
		return nil, err
	}
	defer inTensor.Destroy()

	outs := make([]ort.Value, len(strides)*4)
	defer func() {
		for _, o := range outs {
			if o != nil {
				o.Destroy()
			}
		}
	}()
	if err := eng.detector.Run([]ort.Value{inTensor}, outs); err != nil {
		return nil, fmt.Errorf("detect: %w", err)
	}

	// outs is ordered as the output names were: cls×3, obj×3, bbox×3, kps×3.
	data := func(i int) []float32 {
		t, ok := outs[i].(*ort.Tensor[float32])
		if !ok {
			return nil
		}
		return t.GetData()
	}

	cands := []Detection{}
	for si, stride := range strides {
		grid := detectSize / stride
		cands = append(cands, DecodeStride(stride, grid, grid,
			data(0*len(strides)+si), data(1*len(strides)+si),
			data(2*len(strides)+si), data(3*len(strides)+si),
			minDetectScore)...)
	}
	kept := NMS(cands, nmsIoU)

	faces := make([]Face, 0, len(kept))
	for _, d := range kept {
		onPhoto := Unletterbox(d, scale, padX, padY)
		emb, err := eng.embed(src, onPhoto)
		if err != nil {
			// One face failing to embed is not the photograph failing.
			continue
		}
		faces = append(faces, Face{
			Path:  path,
			X:     int(onPhoto.X),
			Y:     int(onPhoto.Y),
			W:     int(onPhoto.W),
			H:     int(onPhoto.H),
			Score: onPhoto.Score,
			// Trimmed to the model's own output length rather than trusted,
			// so a model swap that changes the dimension is a short vector
			// rather than a panic.
			Embedding: emb,
		})
	}
	return faces, nil
}

func (e *engine) embed(src image.Image, d Detection) ([]float32, error) {
	crop := AlignedCrop(src, d, embedSize)
	in, err := ort.NewTensor(ort.NewShape(1, 3, embedSize, embedSize), Tensor(crop))
	if err != nil {
		return nil, err
	}
	defer in.Destroy()

	out := make([]ort.Value, 1)
	if err := e.embedder.Run([]ort.Value{in}, out); err != nil {
		return nil, err
	}
	defer out[0].Destroy()

	t, ok := out[0].(*ort.Tensor[float32])
	if !ok {
		return nil, fmt.Errorf("embedder returned %T, want float32", out[0])
	}
	src32 := t.GetData()
	v := make([]float32, len(src32))
	copy(v, src32)
	return v, nil
}

// probeModels reports whether the two models are present and loadable, which
// is what `capabilities` answers. Loading them is the only honest check: a
// present file can still be truncated, and a worker that reported ready and
// then failed on the first photograph would be worse than one that said no.
func probeModels(dir string) error {
	if _, err := load(dir); err != nil {
		return err
	}
	return nil
}

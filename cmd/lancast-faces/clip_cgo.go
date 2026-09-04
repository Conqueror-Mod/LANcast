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
 * The CLIP half of the sidecar (ADR 0060).
 *
 * LOADED SEPARATELY FROM THE FACE MODELS, ON PURPOSE
 *
 * They are two optional downloads and a server may have either, both, or
 * neither. Sharing one `sync.Once` with `load()` would mean a library that only
 * wanted face grouping failed to start it because no CLIP model was present,
 * and the reverse — which is a feature breaking because a *different* feature
 * is not installed, reported as neither.
 *
 * So this has its own Once, its own probe and its own line in `capabilities`.
 * The cost is a second copy of the "is the runtime up" dance; the alternative
 * costs a support question nobody can answer from a log.
 *
 * THE MODEL'S INPUT AND OUTPUT NAMES ARE A CONTRACT
 *
 * They are written here rather than discovered, exactly as YuNet's "input" and
 * SFace's "data"/"fc1" are. An export naming them differently is a different
 * model as far as this file is concerned, and finding that out at load time
 * with a clear error beats discovering names at runtime and hoping the one
 * called something-like-embeddings is the right one.
 */

const (
	/*
	 * The names the pinned export actually uses, read out of the graph rather
	 * than assumed.
	 *
	 * They were written here as "pixel_values"/"image_embeds" and
	 * "input_ids"/"attention_mask"/"text_embeds" first, because that is what a
	 * transformers-exported CLIP is called everywhere it is written about. This
	 * export is OpenCLIP's own, and it names them "image", "text" and
	 * "embedding" — and its text tower takes **one** input, with no attention
	 * mask at all.
	 *
	 * The install flow places these files; a hand-placed export from elsewhere
	 * may disagree, and will say so at load rather than at the first
	 * photograph.
	 */
	clipVisualInput  = "image"
	clipVisualOutput = "embedding"

	clipTextInput  = "text"
	clipTextOutput = "embedding"

	// ClipModelName identifies the coordinate system a stored vector belongs
	// to. It travels with every embedding, because two models are two spaces
	// and a cosine between them is a number with no meaning that still sorts.
	ClipModelName = "openclip-vit-b-32"
)

type clipEngine struct {
	visual *ort.DynamicAdvancedSession
	text   *ort.DynamicAdvancedSession
	tok    *Tokenizer
}

var (
	clipOnce   sync.Once
	clipLoaded *clipEngine
	clipErr    error
)

// findFile locates a non-model asset — the merges table — by a fragment of its
// name, so a newer vocabulary lands without an edit here.
func findFile(dir, fragment, ext string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() && filepath.Ext(n) == ext && contains(n, fragment) {
			return filepath.Join(dir, n), nil
		}
	}
	return "", fmt.Errorf("no %s%s in %s", fragment, ext, dir)
}

func loadClip(modelsDir string) (*clipEngine, error) {
	clipOnce.Do(func() {
		// Named, never discovered — the same rule and the same reason as the
		// face engine's. See native_cgo.go: an empty path binds to whichever
		// runtime the loader reaches first, which on Windows is Microsoft's own
		// and is a different answer on every machine.
		if lib := runtimePath(os.Getenv("LANCAST_ONNXRUNTIME"), modelsDir); lib != "" {
			ort.SetSharedLibraryPath(lib)
		}
		if err := ort.InitializeEnvironment(); err != nil {
			clipErr = fmt.Errorf("start onnxruntime: %w", err)
			return
		}

		visualPath, err := findModel(modelsDir, "clip-visual")
		if err != nil {
			clipErr = err
			return
		}
		textPath, err := findModel(modelsDir, "clip-textual")
		if err != nil {
			clipErr = err
			return
		}
		mergesPath, err := findFile(modelsDir, "merges", ".txt")
		if err != nil {
			clipErr = err
			return
		}

		/*
		 * The tokenizer is built before the sessions, because it is the cheap
		 * failure. A truncated merges file is a vocabulary that is quietly
		 * short and produces ids the embedding matrix indexes out of range —
		 * better to refuse here than to load 300MB first and then discover it.
		 */
		f, err := os.Open(mergesPath)
		if err != nil {
			clipErr = fmt.Errorf("open merges: %w", err)
			return
		}
		tok, err := NewTokenizer(f)
		_ = f.Close()
		if err != nil {
			clipErr = err
			return
		}

		v, err := ort.NewDynamicAdvancedSession(visualPath,
			[]string{clipVisualInput}, []string{clipVisualOutput}, nil)
		if err != nil {
			clipErr = fmt.Errorf("load clip visual: %w", err)
			return
		}
		txt, err := ort.NewDynamicAdvancedSession(textPath,
			[]string{clipTextInput}, []string{clipTextOutput}, nil)
		if err != nil {
			clipErr = fmt.Errorf("load clip textual: %w", err)
			return
		}
		clipLoaded = &clipEngine{visual: v, text: txt, tok: tok}
	})
	return clipLoaded, clipErr
}

// clipModels reports whether the semantic-search models are present and
// loadable. Loading is the only honest check: a present file can still be
// truncated, and a worker that said ready and then failed on the first
// photograph would be worse than one that said no.
func clipModels(dir string) error {
	if dir == "" {
		return errNoClipModel
	}
	if _, err := loadClip(dir); err != nil {
		return err
	}
	return nil
}

/*
 * embedImageFile turns one photograph into a unit vector.
 *
 * A file that cannot be decoded is this photograph's error and not the batch's,
 * the way `detect` already treats one: a library contains a truncated JPEG
 * eventually, and stopping the pass at it means the pass never finishes.
 */
func embedImageFile(path, modelsDir string) ([]float32, error) {
	eng, err := loadClip(modelsDir)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	src, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	in, err := ort.NewTensor(
		ort.NewShape(1, 3, ClipImageSize, ClipImageSize),
		ClipTensor(ClipCrop(src)))
	if err != nil {
		return nil, err
	}
	defer in.Destroy()

	out := make([]ort.Value, 1)
	if err := eng.visual.Run([]ort.Value{in}, out); err != nil {
		return nil, err
	}
	defer out[0].Destroy()

	t, ok := out[0].(*ort.Tensor[float32])
	if !ok {
		return nil, fmt.Errorf("clip visual returned %T, want float32", out[0])
	}
	v := append([]float32(nil), t.GetData()...)
	if !Normalize(v) {
		return nil, fmt.Errorf("clip visual returned a zero vector")
	}
	return v, nil
}

/*
 * embedQuery turns a typed sentence into a unit vector in the same space.
 *
 * Same space is the whole point and the thing nothing checks: the two towers
 * only agree because they came from one export and the text went through that
 * export's tokenizer. There is no runtime assertion that could catch a mismatch
 * — both halves produce 512 plausible numbers either way.
 */
func embedQuery(query, modelsDir string) ([]float32, error) {
	eng, err := loadClip(modelsDir)
	if err != nil {
		return nil, err
	}

	/*
	 * The mask is computed and not sent.
	 *
	 * This export's text tower takes ids alone: it finds the end-of-text token
	 * by argmax over the ids themselves and reads the sequence there, so
	 * padding is already excluded and a mask would have nowhere to go. Encode
	 * still returns one because it is the honest description of what it
	 * produced, and because an export that does want a mask is a plausible
	 * future pin.
	 */
	ids, _, err := eng.tok.Encode(query)
	if err != nil {
		return nil, err
	}

	idsT, err := ort.NewTensor(ort.NewShape(1, ContextLength), ids)
	if err != nil {
		return nil, err
	}
	defer idsT.Destroy()

	out := make([]ort.Value, 1)
	if err := eng.text.Run([]ort.Value{idsT}, out); err != nil {
		return nil, err
	}
	defer out[0].Destroy()

	t, ok := out[0].(*ort.Tensor[float32])
	if !ok {
		return nil, fmt.Errorf("clip textual returned %T, want float32", out[0])
	}
	v := append([]float32(nil), t.GetData()...)
	if !Normalize(v) {
		return nil, fmt.Errorf("clip textual returned a zero vector")
	}
	return v, nil
}

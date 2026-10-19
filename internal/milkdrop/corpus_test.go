package milkdrop

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"lancast/internal/milkdrop/eel"
)

/*
 * The corpus: real presets, kept outside the repository (ADR 0091: their
 * licences are uncertain, and LANcast redistributes none).
 *
 *	LANCAST_MILKDROP_CORPUS=<dir>[;<dir>…]   folders searched for *.milk
 *	LANCAST_MILKDROP_DIFF=<file.jsonl>       also write every block, for
 *	                                         spikes/milkdrop/godiff.mjs
 *
 * The spike used three projectM collections, pinned by commit:
 * presets-milkdrop-original @ e03b83e, presets-cream-of-the-crop @ 0180df2,
 * presets-projectm-classic @ 14a6244 — 9,487 presets in all.
 *
 * Skipped when unset, so `go test ./...` needs nothing outside the tree. What
 * it asserts is the floor the spike measured: eel-wasm compiled 99.9% of
 * these, so a validator that accepts much less is refusing working presets.
 * godiff.mjs is the stronger check — it compares this package's verdict with
 * eel-wasm's own on every block, one by one.
 */
func TestCorpus(t *testing.T) {
	roots := os.Getenv("LANCAST_MILKDROP_CORPUS")
	if roots == "" {
		t.Skip("LANCAST_MILKDROP_CORPUS not set")
	}
	var diff *json.Encoder
	if out := os.Getenv("LANCAST_MILKDROP_DIFF"); out != "" {
		f, err := os.Create(out)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		diff = json.NewEncoder(f)
	}

	var files []string
	for _, root := range filepath.SplitList(roots) {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.EqualFold(filepath.Ext(p), ".milk") {
				files = append(files, p)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Strings(files)

	accepted := 0
	reasons := map[string]int{}
	example := map[string]string{}
	var most eel.Stats
	shaders := map[string]int{}
	norm := regexp.MustCompile(`"[^"]*"|\d+`)
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		u, blocks, err := readMilk(body)
		if err != nil {
			t.Errorf("%s: %v", f, err)
			continue
		}
		for _, b := range blocks(&u.Preset) {
			if !compiled(&u.Preset, b.name) {
				continue
			}
			st, verr := eel.Measure(*b.src)
			if verr == nil {
				most.Bytes = max(most.Bytes, st.Bytes)
				most.Depth = max(most.Depth, st.Depth)
				most.LoopNesting = max(most.LoopNesting, st.LoopNesting)
			}
			if diff != nil {
				msg := ""
				if verr != nil {
					msg = verr.Error()
				}
				_ = diff.Encode(map[string]any{"file": f, "block": b.name, "src": *b.src, "ok": verr == nil, "msg": msg})
			}
		}
		if _, err := Parse(body, filepath.Base(f)); err != nil {
			k := norm.ReplaceAllString(err.Error(), "N")
			if i := strings.Index(k, ": "); i >= 0 {
				k = k[i+2:]
			}
			reasons[k]++
			if example[k] == "" {
				example[k] = filepath.Base(f) + ": " + err.Error()
			}
			continue
		}
		accepted++
		shaders[u.Shaders]++
	}

	t.Logf("%d presets, %d accepted (%.2f%%)", len(files), accepted, 100*float64(accepted)/float64(len(files)))
	t.Logf("shaders: %v", shaders)
	t.Logf("largest accepted block %d bytes, deepest nesting %d, deepest loop nesting %d",
		most.Bytes, most.Depth, most.LoopNesting)
	keys := make([]string, 0, len(reasons))
	for k := range reasons {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return reasons[keys[i]] > reasons[keys[j]] })
	for _, k := range keys {
		t.Logf("  %4d × %s   e.g. %s", reasons[k], k, example[k])
	}
	if len(files) > 0 && float64(accepted) < 0.995*float64(len(files)) {
		t.Errorf("accepted %d of %d; eel-wasm compiles 99.9%% of this corpus", accepted, len(files))
	}
}

/*
 * TestFuzzBlocks writes random strings of EEL tokens, with this package's
 * verdict on each, for godiff.mjs to compare with eel-wasm's.
 *
 *	LANCAST_MILKDROP_FUZZ=<file.jsonl>
 *
 * The corpus is nearly all working code, so it says little about the edges
 * of the grammar: where a ';' may go, what may be assigned, how comments
 * nest. Random strings are mostly wrong, which is the point — they show
 * whether the two refuse the same wrong things.
 */
func TestFuzzBlocks(t *testing.T) {
	out := os.Getenv("LANCAST_MILKDROP_FUZZ")
	if out == "" {
		t.Skip("LANCAST_MILKDROP_FUZZ not set")
	}
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	pieces := []string{
		"a", "b", "x.y", "sin", "min", "if", "loop", "while", "assign", "megabuf", "gmegabuf", "exec2", "fmod", "_x",
		"1", "12", ".", "e", ";", ";", ",", "(", "(", ")", ")", "=", "+=", "%=", "^=", "==", "!=", "<", "<=", ">",
		"&&", "||", "&", "|", "^", "+", "-", "!", "*", "/", "%", "?", ":", "[", "]", "$", "~",
		" ", " ", "\n", "\r\n", "//", "/*", "*/", `\\`, `\`, "\t",
	}
	seed := uint64(0x9e3779b97f4a7c15)
	next := func(n int) int {
		seed ^= seed << 13
		seed ^= seed >> 7
		seed ^= seed << 17
		return int(seed % uint64(n))
	}
	for i := 0; i < 200000; i++ {
		var b strings.Builder
		for j, n := 0, 1+next(12); j < n; j++ {
			b.WriteString(pieces[next(len(pieces))])
		}
		src := b.String()
		err := eel.Check(src, eel.DefaultLimits)
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		_ = enc.Encode(map[string]any{"file": "fuzz", "block": "fuzz", "src": src, "ok": err == nil, "msg": msg})
	}
}

// compiled reports whether butterchurn compiles a block of p: a shape's or
// a wave's only when it is enabled.
func compiled(p *Preset, name string) bool {
	for i := range p.Shapes {
		if strings.HasPrefix(name, "shape"+string(rune('0'+i))+".") {
			return enabled(p.Shapes[i].BaseVals)
		}
	}
	for i := range p.Waves {
		if strings.HasPrefix(name, "wave"+string(rune('0'+i))+".") {
			return enabled(p.Waves[i].BaseVals)
		}
	}
	return true
}

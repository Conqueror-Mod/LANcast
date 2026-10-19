//go:build spike

// SPIKE (ADR 0091 step g). Builds the browser test set from what the server
// itself would store.
//
//	go run -tags spike ./spikes/milkdrop/shaders/bundle -conv converted.json -every 8 -out presets.json <corpus pack dir>...
//
// For a sample of the presets whose shaders all converted: read the .milk
// with internal/milkdrop (as an upload would be read today, shaders dropped),
// put the converted GLSL in, then hand the result back to milkdrop.Parse as
// butterchurn JSON, so the server's own validator decides whether it would be
// accepted. Each entry also carries a twin with no shaders, the picture
// step 1 shows, so the page can tell whether a shader changed anything.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"lancast/internal/milkdrop"
)

type entry struct {
	ID     string           `json:"id"`
	Preset milkdrop.Preset  `json:"preset"`
	Bare   *milkdrop.Preset `json:"bare,omitempty"`
}

func main() {
	convPath := flag.String("conv", "", "converted.json from post.mjs")
	every := flag.Int("every", 1, "take every Nth preset, sorted by id")
	out := flag.String("out", "", "presets.json")
	flag.Parse()

	var conv map[string]struct{ Warp, Comp string }
	b, err := os.ReadFile(*convPath)
	check(err)
	check(json.Unmarshal(b, &conv))
	roots := map[string]string{}
	for _, r := range flag.Args() {
		roots[filepath.Base(r)] = r
	}

	ids := make([]string, 0, len(conv))
	for id := range conv {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var set []entry
	refused := map[string]int{}
	for i, id := range ids {
		if i%*every != 0 {
			continue
		}
		pack, rel, _ := strings.Cut(id, "/")
		body, err := os.ReadFile(filepath.Join(roots[pack], filepath.FromSlash(rel)))
		check(err)
		u, err := milkdrop.Parse(body, filepath.Base(rel))
		if err != nil {
			refused["milk: "+err.Error()]++
			continue
		}
		bare := u.Preset
		p := u.Preset
		p.Warp, p.Comp = conv[id].Warp, conv[id].Comp
		j, err := json.Marshal(p)
		check(err)
		back, err := milkdrop.Parse(j, filepath.Base(rel))
		if err != nil {
			refused["json: "+err.Error()]++
			continue
		}
		if back.Shaders != milkdrop.ShadersConverted {
			refused["json: shaders="+back.Shaders]++
		}
		set = append(set, entry{ID: id, Preset: back.Preset, Bare: &bare})
	}
	j, err := json.Marshal(set)
	check(err)
	check(os.WriteFile(*out, j, 0o644))
	fmt.Fprintf(os.Stderr, "%d presets written, refused: %v\n", len(set), refused)
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

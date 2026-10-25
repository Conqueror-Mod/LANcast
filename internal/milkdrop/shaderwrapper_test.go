package milkdrop

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

/*
 * butterchurnDeclares against the build LANcast ships (web/vendor/butterchurn).
 *
 * The list decides which of a preset's declarations the server keeps: a name
 * butterchurn declares and the server also kept would be declared twice, and
 * the preset would fail to compile in every browser. So it is read back out of
 * the vendored engine's two wrappers here, and an upgrade that adds a name
 * fails this test instead of quietly breaking presets that use it.
 */
func TestButterchurnDeclaresMatchesTheVendoredWrappers(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "web", "vendor", "butterchurn", "butterchurn.min.js"))
	if err != nil {
		t.Fatal(err)
	}
	js := string(src)
	starts := regexp.MustCompile(`createShader\(t=""\)\{`).FindAllStringIndex(js, -1)
	if len(starts) != 2 {
		t.Fatalf("found %d preset-shader wrappers, want warp and comp", len(starts))
	}

	decl := []*regexp.Regexp{
		regexp.MustCompile(`^uniform\s+\w+\s+(\w+)\s*;`),
		regexp.MustCompile(`^(?:in|out)\s+\w+\s+(\w+)\s*;`),
		regexp.MustCompile(`^(?:float|int|vec\d|mat\d)\s+(\w+)\s*(?:=|;)`),
		regexp.MustCompile(`^(?:float|int|vec\d|mat\d)\s+(\w+)\s*\(`),
	}
	found := map[string]bool{}
	for _, s := range starts {
		rest := js[s[0]:]
		frag := strings.Index(rest, "FRAGMENT_SHADER")
		end := strings.Index(rest[frag:], "compileShader")
		if frag < 0 || end < 0 {
			t.Fatal("could not find a wrapper's fragment template")
		}
		tpl := strings.ReplaceAll(rest[frag:frag+end], `\n`, "\n")
		for _, line := range strings.Split(tpl, "\n") {
			line = strings.TrimSpace(line)
			for _, re := range decl {
				if m := re.FindStringSubmatch(line); m != nil {
					found[m[1]] = true
					break
				}
			}
		}
	}

	var missing, extra []string
	for n := range found {
		if !butterchurnDeclares[n] {
			missing = append(missing, n)
		}
	}
	for n := range butterchurnDeclares {
		if !found[n] {
			extra = append(extra, n)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 {
		t.Errorf("the vendored wrappers declare names butterchurnDeclares lacks (a preset declaring one would compile twice): %v", missing)
	}
	if len(extra) > 0 {
		t.Errorf("butterchurnDeclares has names the vendored wrappers no longer declare: %v", extra)
	}
}

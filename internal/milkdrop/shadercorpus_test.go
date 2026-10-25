package milkdrop

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"lancast/internal/milkdrop/msc"
)

/*
 * The shader port against the spike's run over the corpus (ADR 0091, g2).
 *
 *	LANCAST_MILKDROP_CORPUS=<dir>[;<dir>…]   the three projectM packs (corpus_test.go)
 *	LANCAST_MILKDROP_SPIKE_RUN=<dir>         the spike's run/: jobs.jsonl (pipeline.mjs's
 *	                                         prepared HLSL), results.jsonl (the module's
 *	                                         output) and converted.json (post.mjs's
 *	                                         processOptimizedShader)
 *	LANCAST_MILKDROP_CONVERT=<n>             also convert every nth shader with this
 *	                                         package's own converter, under its real
 *	                                         limits, and compare with results.jsonl
 *
 * Both kept outside the repository, like the corpus itself, and skipped when
 * unset. It compares each string step on its own, so a difference says which
 * step it is in:
 *
 *   - splitShaders + prepareShader against jobs.jsonl, byte for byte;
 *   - processOptimizedShader on the module's output against converted.json;
 *   - and, sampled, the converter here — 3 s, 32 MiB, one at a time — against
 *     the spike's host, which ran with 10 s and 256 MiB.
 *
 * The spike read each .milk as UTF-8, replacing what was not; the server reads
 * such a file as Latin-1 (decodeText), which only changes what a comment
 * becomes. The first comparison reads the file as the spike did, so it is
 * about the port and not about that choice.
 */
func TestShaderCorpusAgainstPipelineMjs(t *testing.T) {
	roots, run := os.Getenv("LANCAST_MILKDROP_CORPUS"), os.Getenv("LANCAST_MILKDROP_SPIKE_RUN")
	if roots == "" || run == "" {
		t.Skip("LANCAST_MILKDROP_CORPUS or LANCAST_MILKDROP_SPIKE_RUN not set")
	}

	jobs := map[string]string{}
	readJSONL(t, filepath.Join(run, "jobs.jsonl"), func(b []byte) {
		var j struct{ ID, HLSL string }
		if err := json.Unmarshal(b, &j); err != nil {
			t.Fatal(err)
		}
		jobs[j.ID] = j.HLSL
	})
	type result struct {
		ID   string
		RC   int
		GLSL string
		Log  string
	}
	results := map[string]result{}
	readJSONL(t, filepath.Join(run, "results.jsonl"), func(b []byte) {
		var r result
		if err := json.Unmarshal(b, &r); err != nil {
			t.Fatal(err)
		}
		results[r.ID] = r
	})
	var converted map[string]map[string]string
	if b, err := os.ReadFile(filepath.Join(run, "converted.json")); err != nil {
		t.Fatal(err)
	} else if err := json.Unmarshal(b, &converted); err != nil {
		t.Fatal(err)
	}

	var (
		presets, shaders, prepSame, prepDiff, missing int
		procSame, procDiff                            int
		firstDiffs                                    []string
	)
	seen := map[string]bool{}
	for _, root := range filepath.SplitList(roots) {
		pack := filepath.Base(root)
		var files []string
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.EqualFold(filepath.Ext(p), ".milk") {
				files = append(files, p)
			}
			return err
		})
		sort.Strings(files)
		for _, f := range files {
			presets++
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			rel, _ := filepath.Rel(root, f)
			id := pack + "/" + filepath.ToSlash(rel)
			src := splitShaders(asNodeReadsUTF8(b))
			for _, k := range []struct {
				kind, text string
			}{{"warp", src.Warp}, {"comp", src.Comp}} {
				if k.text == "" {
					continue
				}
				shaders++
				jid := id + "#" + k.kind
				seen[jid] = true
				want, ok := jobs[jid]
				if !ok {
					missing++
					continue
				}
				if prepareShader(k.text) == want {
					prepSame++
				} else {
					prepDiff++
					if len(firstDiffs) < 5 {
						firstDiffs = append(firstDiffs, "prepare "+jid)
					}
				}
				r := results[jid]
				if g, ok := converted[id][k.kind]; ok && r.RC == 0 {
					if processOptimizedShaderAsPacks(r.GLSL) == g {
						procSame++
					} else {
						procDiff++
						if len(firstDiffs) < 10 {
							firstDiffs = append(firstDiffs, "process "+jid)
						}
					}
				}
			}
		}
	}
	extra := 0
	for id := range jobs {
		if !seen[id] {
			extra++
		}
	}
	t.Logf("%d presets, %d shaders; prepareShader: %d identical, %d differ, %d the spike lacks, %d only the spike has",
		presets, shaders, prepSame, prepDiff, missing, extra)
	t.Logf("processOptimizedShader: %d identical, %d differ", procSame, procDiff)
	for _, d := range firstDiffs {
		t.Log("  differs: " + d)
	}
	if prepDiff+missing+extra+procDiff > 0 {
		t.Error("the port and pipeline.mjs disagree")
	}

	every, _ := strconv.Atoi(os.Getenv("LANCAST_MILKDROP_CONVERT"))
	if every <= 0 {
		return
	}
	ids := make([]string, 0, len(jobs))
	for id := range jobs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	c := msc.New()
	defer c.Close(context.Background())
	var same, diff int
	codes := map[string]int{}
	for i := 0; i < len(ids); i += every {
		id := ids[i]
		glsl, err := c.Convert(context.Background(), jobs[id])
		want := results[id]
		var f *msc.Failure
		switch {
		case err == nil && want.RC == 0 && glsl == want.GLSL:
			same++
			codes["converted"]++
		case err != nil && errors.As(err, &f) && want.RC != 0 && f.Reason == strings.TrimSpace(strings.SplitN(strings.TrimLeft(want.Log, "\n"), "\n", 2)[0]):
			same++
			codes["refused alike"]++
		default:
			diff++
			if diff <= 5 {
				t.Logf("  converter differs on %s: rc %d, err %v", id, want.RC, err)
			}
		}
	}
	t.Logf("converter, every %d: %d agree with the spike (%v), %d differ", every, same, codes, diff)
	if diff > 0 {
		t.Error("the converter here disagrees with the spike's host")
	}
}

// asNodeReadsUTF8 is fs.readFileSync(f, "utf8"): each byte that is not part of
// valid UTF-8 becomes U+FFFD.
func asNodeReadsUTF8(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	var sb strings.Builder
	for len(b) > 0 {
		r, n := utf8.DecodeRune(b)
		sb.WriteRune(r)
		b = b[n:]
	}
	return sb.String()
}

func readJSONL(t *testing.T, path string, each func([]byte)) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		each(sc.Bytes())
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
}

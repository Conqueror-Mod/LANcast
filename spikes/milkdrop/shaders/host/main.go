//go:build spike

// SPIKE (ADR 0091 step g). Runs msc.wasm, the WASI build of
// milkdrop-shader-converter, under wazero, the way the server would at upload.
//
//	go run -tags spike ./spikes/milkdrop/shaders/host -wasm msc.wasm -in jobs.jsonl -out results.jsonl
//
// jobs.jsonl: one {"id":…, "hlsl":…} per line (pipeline.mjs's prepareShader
// output). results.jsonl: {"id", "rc", "glsl", "log", "us", "pages"}.
//
// Each conversion gets a fresh instance of one compiled module, with no
// filesystem, no environment, no clock beyond WASI's defaults, stdin and
// stdout as byte buffers, a memory ceiling and a deadline. That is the
// whole capability set; there are no host functions at all.
//
// The tag keeps it out of `go build ./...`: it is evidence, not product.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"
)

type job struct {
	ID   string `json:"id"`
	HLSL string `json:"hlsl"`
}

type result struct {
	ID    string `json:"id"`
	RC    int    `json:"rc"` // exit code; -1 trap, -2 deadline
	GLSL  string `json:"glsl,omitempty"`
	Log   string `json:"log,omitempty"`
	US    int64  `json:"us"`    // wall time of instantiate + run
	Pages uint32 `json:"pages"` // linear memory at exit, 64 KiB pages
}

func main() {
	wasmPath := flag.String("wasm", "", "msc.wasm")
	in := flag.String("in", "", "jobs.jsonl")
	out := flag.String("out", "", "results.jsonl")
	mem := flag.Uint("mempages", 4096, "memory ceiling in 64 KiB pages (4096 = 256 MiB)")
	timeout := flag.Duration("timeout", 10*time.Second, "per conversion")
	jobs := flag.Int("j", runtime.NumCPU(), "parallel conversions")
	interp := flag.Bool("interp", false, "use wazero's interpreter instead of the compiler")
	flag.Parse()

	ctx := context.Background()
	wasm, err := os.ReadFile(*wasmPath)
	check(err)

	cfg := wazero.NewRuntimeConfigCompiler()
	if *interp {
		cfg = wazero.NewRuntimeConfigInterpreter()
	}
	cfg = cfg.WithMemoryLimitPages(uint32(*mem)).WithCloseOnContextDone(true)
	rt := wazero.NewRuntimeWithConfig(ctx, cfg)
	defer rt.Close(ctx)
	wasi_snapshot_preview1.MustInstantiate(ctx, rt)

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	before := ms.Sys
	t0 := time.Now()
	compiled, err := rt.CompileModule(ctx, wasm)
	check(err)
	fmt.Fprintf(os.Stderr, "compile: %v (%d bytes of wasm)\n", time.Since(t0), len(wasm))
	for _, imp := range compiled.ImportedFunctions() {
		mod, name, _ := imp.Import()
		fmt.Fprintf(os.Stderr, "import: %s.%s\n", mod, name)
	}

	f, err := os.Open(*in)
	check(err)
	var all []job
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var j job
		check(json.Unmarshal(sc.Bytes(), &j))
		all = append(all, j)
	}
	check(sc.Err())

	results := make([]result, len(all))
	var wg sync.WaitGroup
	ch := make(chan int)
	var peakSys uint64
	var peakMu sync.Mutex
	for w := 0; w < *jobs; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range ch {
				results[i] = convert(ctx, rt, compiled, all[i], *timeout)
				if i%200 == 0 {
					var m runtime.MemStats
					runtime.ReadMemStats(&m)
					peakMu.Lock()
					if m.Sys > peakSys {
						peakSys = m.Sys
					}
					peakMu.Unlock()
				}
			}
		}()
	}
	t1 := time.Now()
	for i := range all {
		ch <- i
	}
	close(ch)
	wg.Wait()
	wall := time.Since(t1)

	o, err := os.Create(*out)
	check(err)
	w := bufio.NewWriter(o)
	enc := json.NewEncoder(w)
	var us []int64
	var pages []int
	rcs := map[int]int{}
	for _, r := range results {
		check(enc.Encode(r))
		us = append(us, r.US)
		pages = append(pages, int(r.Pages))
		rcs[r.RC]++
	}
	check(w.Flush())
	check(o.Close())

	sort.Slice(us, func(a, b int) bool { return us[a] < us[b] })
	sort.Ints(pages)
	q := func(p float64) int { return int(float64(len(us)-1) * p) }
	fmt.Fprintf(os.Stderr, "%d conversions in %v with %d workers; exit codes %v\n", len(all), wall, *jobs, rcs)
	if len(us) > 0 {
		fmt.Fprintf(os.Stderr, "per conversion: p50 %.1f ms, p90 %.1f ms, p99 %.1f ms, max %.1f ms\n",
			ms1(us[q(.5)]), ms1(us[q(.9)]), ms1(us[q(.99)]), ms1(us[len(us)-1]))
		fmt.Fprintf(os.Stderr, "linear memory: p50 %d KiB, p99 %d KiB, max %d KiB\n",
			pages[q(.5)]*64, pages[q(.99)]*64, pages[len(pages)-1]*64)
	}
	fmt.Fprintf(os.Stderr, "go runtime Sys: %d MiB before compile, peak sampled %d MiB\n", before>>20, peakSys>>20)
}

func convert(ctx context.Context, rt wazero.Runtime, c wazero.CompiledModule, j job, d time.Duration) result {
	var stdout, stderr bytes.Buffer
	cfg := wazero.NewModuleConfig().
		WithName("").
		WithArgs("msc").
		WithStdin(bytes.NewReader([]byte(j.HLSL))).
		WithStdout(&stdout).
		WithStderr(&stderr)
	cctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	t := time.Now()
	mod, err := rt.InstantiateModule(cctx, c, cfg)
	r := result{ID: j.ID, US: time.Since(t).Microseconds()}
	if mod != nil {
		if m := mod.Memory(); m != nil {
			r.Pages = m.Size() / 65536
		}
		mod.Close(ctx)
	}
	var exit *sys.ExitError
	switch {
	case err == nil:
		r.RC = 0
	case errors.As(err, &exit):
		r.RC = int(exit.ExitCode())
		if exit.ExitCode() == sys.ExitCodeDeadlineExceeded {
			r.RC = -2
		}
	default:
		r.RC = -1
		r.Log = err.Error()
	}
	if r.RC == 0 {
		r.GLSL = stdout.String()
	}
	if r.Log == "" {
		r.Log = stderr.String()
	}
	return r
}

func ms1(us int64) float64 { return float64(us) / 1000 }

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

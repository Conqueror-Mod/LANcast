/*
lancast-faces is the native side of face grouping (ADR 0052).

It exists as its own binary rather than as a package inside lancastd, and that
placement is the whole decision. Every LANcast release build is CGO_ENABLED=0
and every one of them is cross-compiled from a single Linux runner; native
inference inside the server would mean a mingw toolchain, an aarch64 toolchain,
the vision library built for both, a glibc floor on Linux, and DLLs beside a
Windows service that the installer must place and the updater must swap
atomically. That is a permanent tax on every release for a feature only picture
libraries use.

So the cgo lives here, in a binary the server launches the way it already
launches ffmpeg — optional, replaceable, and unable to take the media server
down with it when a vision library segfaults.

# What exists today

The transport and the contract. **Not the inference**: no model is chosen or
bundled yet, so `detect` reports honestly that it cannot run rather than
returning an empty result that would read as "no faces in any of your
photographs".

That order is deliberate. The last two times this project shipped something
whose path only executes during a release, it broke there — a signing step that
had never run, and an update swap that reached staging and stopped. So the
build, the cross-compile and the publish are proven before anything depends on
them.
*/
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"
)

// Version is injected at build time, exactly as lancastd's is, so the server
// can refuse a worker it does not recognise rather than guessing at its
// output.
var Version = "dev"

/*
Capabilities is what the server reads to decide whether face grouping can run
at all, and it is deliberately shaped like the media-tools report the settings
page already shows (ADR 0048): a thing that is absent says so, and the UI can
explain rather than fail silently.

Ready is false while no model is present. A server seeing that must not queue
work — the failure this avoids is a worker that accepts every photograph,
returns nothing, and leaves somebody believing their family albums contain no
faces.
*/
type Capabilities struct {
	Version string `json:"version"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	// Native reports what the binary is actually linked against. It is the one
	// fact a cross-compiled artefact cannot fake: a build that silently lost
	// its cgo would say so here rather than at the first photograph.
	Native string `json:"native"`
	Ready  bool   `json:"ready"`
	Reason string `json:"reason,omitempty"`
}

// Face is one detection. The embedding is carried as raw float32s rather than
// anything cleverer because the only consumer stores it as a blob and compares
// it with a dot product.
type Face struct {
	Path      string    `json:"path"`
	X         int       `json:"x"`
	Y         int       `json:"y"`
	W         int       `json:"w"`
	H         int       `json:"h"`
	Score     float32   `json:"score"`
	Embedding []float32 `json:"embedding"`
}

// Result is one line of `detect` output: one input path, its faces, and its
// error if it had one. A photograph that cannot be read is not a failure of the
// batch — a library will contain a truncated JPEG eventually, and stopping the
// pass at it would mean the pass never finishes.
type Result struct {
	Path  string `json:"path"`
	Faces []Face `json:"faces,omitempty"`
	Error string `json:"error,omitempty"`
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "capabilities":
		capabilities(os.Args[2:])
	case "detect":
		detect(os.Args[2:])
	case "version", "--version", "-version":
		fmt.Println(Version)
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "lancast-faces: unknown command %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `lancast-faces — native face detection for LANcast (ADR 0052)

  capabilities [-models DIR]
                          report what this build can do, as JSON
  detect [-models DIR]    read image paths on stdin, one per line;
                          write one JSON Result per line to stdout
  version                 print the version

Paths arrive on stdin rather than as arguments because a library is tens of
thousands of them and every platform has a command-line length limit.
`)
}

func caps(modelsDir string) Capabilities {
	c := Capabilities{
		Version: Version,
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
		Native:  nativeInfo(),
	}
	/*
	 * Not ready, and specific about why.
	 *
	 * "Ready: false" with no reason is the kind of report that costs an
	 * afternoon: it cannot be told apart from a worker that is missing, one
	 * that is the wrong version, and one whose model file was deleted.
	 */
	if !hasCGO {
		c.Reason = "built without cgo, so no inference backend is linked"
		return c
	}
	if modelsDir == "" {
		c.Reason = "no model directory given — pass -models"
		return c
	}
	if err := probeModels(modelsDir); err != nil {
		c.Reason = err.Error()
		return c
	}
	c.Ready = true
	return c
}

func capabilities(args []string) {
	fs := flag.NewFlagSet("capabilities", flag.ExitOnError)
	models := fs.String("models", "", "directory holding the detector and embedder models")
	_ = fs.Parse(args)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(caps(*models)); err != nil {
		fmt.Fprintln(os.Stderr, "lancast-faces:", err)
		os.Exit(1)
	}
}

func detect(args []string) {
	fs := flag.NewFlagSet("detect", flag.ExitOnError)
	models := fs.String("models", "", "directory holding the detector and embedder models")
	_ = fs.Parse(args)

	c := caps(*models)
	if !c.Ready {
		/*
		 * Refused, loudly, rather than returning an empty result.
		 *
		 * An empty result is indistinguishable from "there are no faces in any
		 * of these photographs", and a caller that believed it would mark the
		 * library indexed and never ask again. Exit code 3 is reserved for
		 * "this worker cannot run", so a caller can tell it apart from a crash.
		 */
		fmt.Fprintf(os.Stderr, "lancast-faces: cannot detect: %s\n", c.Reason)
		os.Exit(3)
	}

	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	out := json.NewEncoder(os.Stdout)
	for in.Scan() {
		path := strings.TrimSpace(in.Text())
		if path == "" {
			continue
		}
		faces, err := detectOne(path, *models)
		r := Result{Path: path, Faces: faces}
		if err != nil {
			r.Error = err.Error()
		}
		if err := out.Encode(r); err != nil {
			fmt.Fprintln(os.Stderr, "lancast-faces:", err)
			os.Exit(1)
		}
	}
	if err := in.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "lancast-faces:", err)
		os.Exit(1)
	}
}

// errNoModel is what both backends return until a model is chosen and bundled.
// Named rather than inline so the two build variants cannot drift into
// describing the same situation two ways.
var errNoModel = fmt.Errorf("no face model bundled (ADR 0052)")

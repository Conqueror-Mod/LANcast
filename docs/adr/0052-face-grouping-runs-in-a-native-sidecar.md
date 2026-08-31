# ADR 0052 — Face grouping runs in a native sidecar

**Status:** proposed
**Date:** 2026-08-31

Face grouping is the feature Google Photos used to justify reading every family
album ever uploaded, which makes doing it **entirely on the box** the sharpest
available statement of what LANcast is for. Nothing leaves the machine, no
account is required, and the index is a table in the same database as everything
else.

Chris has chosen **cgo** — native inference rather than a pure-Go approximation.
This records what that costs, and where the cgo has to live for it not to cost
that.

## The constraint, measured rather than assumed

Every release build in `.goreleaser.yaml` is `CGO_ENABLED=0`:

| build | target | cgo |
|---|---|---|
| `lancastd-windows` | windows/amd64 | off |
| `lancastd-unix` | linux/amd64, linux/arm64 | off |
| `lancast-windows` | windows/amd64 | off |
| `lancast-unix` | linux/amd64, linux/arm64 | off |

CI and the release both run on a **single `ubuntu-latest` runner**, and all four
targets are produced by cross-compilation from it. That is not an accident of
configuration — it is the reason a four-target matrix is one cheap job, the
reason the Linux binaries are static and run on anything, and the reason
`modernc.org/sqlite` (a pure-Go SQLite) is the driver instead of the faster cgo
one.

**Turning cgo on inside `lancastd` breaks all of that at once.** Windows/amd64
would need a mingw cross-toolchain plus the inference library built for Windows;
linux/arm64 would need an aarch64 cross-toolchain plus the library built for
aarch64; the Linux binaries would acquire a glibc floor unless every dependency
is static-linked by hand; and the Windows service, today a single `.exe`, would
likely grow DLLs that the installer has to place and the updater has to swap
atomically alongside it.

That is a permanent tax on every release, paid by every user, for a feature only
picture libraries use.

## The decision

**Use cgo. Do not put it in `lancastd`.**

Face detection and embedding run in a **separate native binary** —
`lancast-faces` — which the server launches as a child process, hands a list of
image paths, and reads results back from. `lancastd` stays `CGO_ENABLED=0` and
the release matrix is untouched.

This is not a compromise on the choice. The inference is native, compiled, and
as fast as the hardware allows; cgo simply lives in the one binary that needs
it, built by its own job, for the platforms it can serve.

### Why this is the obvious shape here

**LANcast already does exactly this, twice.** `ffmpeg` and `ffprobe` are
external native tools the server locates, manages, and functions without — and
[ADR 0048](0048-media-tools-install-themselves-on-first-run.md) already built
the flow for fetching such a tool on first run and reporting its absence
honestly. A face worker is the same shape as a tool that is already proven in
production, rather than a new kind of thing.

It also means:

- **The feature is optional.** A model is tens of megabytes; somebody with no
  picture library downloads none of it.
- **The blast radius is a subprocess.** A segfault in a native vision library
  kills the worker, and the server reports a failed pass. In-process it would
  take the media server down mid-film.
- **The matrix can be narrower.** `lancast-faces` can ship for windows/amd64 and
  linux/amd64 on day one and add linux/arm64 when somebody wants it, without
  holding up a release of anything else.

### Which library, and which models

**ONNX Runtime, with OpenCV Zoo's YuNet and SFace.**

ONNX Runtime takes two interchangeable model files — a detector and an embedder
— each replaceable without touching the code that runs them, so improving
quality later is a file swap rather than a rebuild. That matters for a feature
whose quality *is* its model.

**The model licences decide this, not the accuracy figures**, and the first
draft of this ADR got it wrong: it named SCRFD and ArcFace, which is what
everybody reaches for and is the wrong answer here.

| model | code licence | **weights licence** | usable |
|---|---|---|---|
| YuNet (OpenCV Zoo) | MIT | **MIT** | yes |
| SFace (OpenCV Zoo) | Apache-2.0 | **Apache-2.0** | yes |
| InsightFace SCRFD / ArcFace / `buffalo_l` | MIT | **non-commercial research only** | **no** |
| dlib `mmod_human_face_detector` | Boost | public domain | yes |
| dlib `shape_predictor_5_face_landmarks` | Boost | unrestricted | yes |
| dlib `dlib_face_recognition_resnet_model_v1` | Boost | public domain | yes |
| dlib `shape_predictor_68_face_landmarks` | Boost | **non-commercial (iBUG 300-W)** | **no** |

The trap is that a permissive *code* licence says nothing about the *weights*.
InsightFace's library is MIT and its pretrained models are explicitly
non-commercial; dlib is Boost throughout and one of its four models is
non-commercial because of the dataset it was trained on. Both look fine to
anybody who checks the repository licence and stops there.

**dlib remains a viable second choice** and is worth studying — a working
reference implementation exists at `AndriyKalashnykov/go-face-recognition` (MIT)
over `go-face` (CC0-1.0), including a fully static `libdlib.a` link with no
runtime `.so` dependencies, which is directly relevant to shipping a single
portable worker. Its model stack is usable within our terms **provided the
68-point predictor is avoided** — and most dlib examples use exactly that one.
Against it: the models are a decade old, detection and embedding are coupled,
and the Windows build is genuinely painful.

**Whichever is chosen, the weights licence is a release-blocking property** and
belongs in a check rather than in somebody's memory. A model whose terms forbid
commercial use would silently foreclose a licensing decision this project has
not made yet — see ADR 0053.

## What gets stored

Three additions, all additive columns and tables, no reshaping of `media_item`:

- **`face`** — one row per detected face: `item_id`, bounding box, the embedding
  as a blob, and a nullable `cluster_id`.
- **`face_cluster`** — one row per group: an id, a nullable `name`, and whether
  the name is locked.
- The worker is **re-runnable**, following the scan → enrich → probe shape
  already in the codebase.

**A name a person typed is an edit and is locked.** A re-cluster may move faces
between clusters and may create new ones; it may never overwrite a name. This is
the locked-fields rule, and it gets the same standing and the same permanent
test as the rest of them — the thing LANcast exists to not be is software that
re-litigates your corrections.

## Marked folders are not indexed

A sensitive folder's photographs are **not detected, not embedded, and not
clustered** (ADR 0051, amended).

This follows the timeline's rule and for a stronger reason. A people view is
built out of face thumbnails cropped from photographs; if a marked folder were
indexed, its contents would appear — cropped, out of context, on a screen that
is not the folder — which is precisely what the mark exists to prevent. Worse,
a named cluster would then link a person to that folder by inference even if no
thumbnail from it were ever shown.

Marking a folder after it has been indexed must therefore **delete** the faces
belonging to it, not merely hide them. An embedding is derived from the
photograph and is not less private than it.

## The naming UI is the product

Worth stating because it is the part most likely to be under-built: a cluster of
unnamed faces is a curiosity and a named one is how you find a photograph. The
model and the worker are the large part by effort and the smaller part by value.

The screen has to make it fast to name a large cluster, easy to say "this is not
the same person", and obvious when the software is unsure. None of that is
inference work.

## Cost, honestly

The worker is the smaller half of the effort. The build and release plumbing for
a second, native, per-platform binary is real work that produces nothing a user
can see, and it comes first — there is no way to demonstrate the feature without
it. The naming UI is where the remaining time goes.

An estimate worth writing down so it can be wrong out loud: on the reporting
library — 3,676 photographs, of which 3,670 sit in folders — a first pass is
tens of minutes of CPU, once, in the background. That is acceptable for a
one-off index and would not be acceptable per scan, which is why the pass must
be incremental on `taken_at`/`mtime` the way the music tag pass should be.

## What this does not decide

- **GPS and places.** Reading EXIF GPS is the one photo-metadata question that is
  a privacy decision rather than a parsing chore, and it is not bundled in here.
- **Duplicate detection and RAW.** Both still open, both independent of this.
- **Whether the model ships with the installer or is fetched on first use.**
  ADR 0048's flow exists and probably applies. With YuNet and SFace the terms
  permit redistribution either way, so this became a size and update question
  rather than a legal one.

## The risk worth stating

The first native cross-compiled artefact this project ships will fail in a way
the tests do not see, because that is what happened the last two times a path
only executed during a release — the signing step that had never run, and the
update swap that reached staging and stopped. `lancast-faces` should therefore
be **built and published by CI before anything depends on it**, and exercised on
a real install, before a line of the naming UI is written.

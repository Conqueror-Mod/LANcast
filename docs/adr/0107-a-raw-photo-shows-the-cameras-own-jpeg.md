# ADR 0107 — A RAW photo shows the camera's own JPEG

Date: 2026-10-28 · Status: **accepted** · Builds on [ADR 0028](0028-pictures-library.md)

## Context

A picture library indexed JPEG, PNG, WebP, GIF, BMP, TIFF and HEIC, and
ignored camera RAW files entirely: `.cr2`, `.nef`, `.arw`, `.dng` and the
rest. For anyone who shoots RAW, that is most of their photographs.

Developing a RAW, meaning demosaicing the sensor data, applying the camera's
colour profile, white balance and a tone curve, is what Lightroom and darktable
are for. It is a large dependency (LibRaw) or a large piece of code, and its
result is a matter of taste. ffmpeg does not read RAW either.

But every RAW file also holds a JPEG the camera rendered for its own screen.
It is full size in Canon's CR2/CR3, Nikon's NEF, Fujifilm's RAF and most
DNGs, and at least a couple of thousand pixels wide in Sony's ARW. It is the
photograph as the camera saw it.

## Decision

**RAW files are pictures, and what they show is the JPEG inside them.** Their
extensions join the picture library's list. The photo decoder (`photo.Read`)
never hands a RAW to `image.Decode`. The TIFF decoder would read the first
image in the file and report success, and in a NEF that first image is a
160 × 120 thumbnail. Instead it finds every embedded JPEG by its `FF D8 FF`
start and keeps **the largest that Go's decoder accepts**. That one rule
covers TIFF-based RAWs, RAF and CR3 without parsing any maker's container:

- an EXIF thumbnail loses on size;
- sensor data stored as lossless JPEG (SOF3) is a process Go does not decode,
  so it is never chosen;
- a preview cut short gives way to the next largest.

Capture time and rotation come from the file's EXIF, which `readEXIF` already
reads, since most RAWs are TIFF inside.

**Everything else follows from the existing picture path.** A browser cannot
show a RAW, so full-screen viewing serves the 1600 px rendition the thumbnail
worker makes, as for HEIC. Faces, near-copies and places read that rendition.
Downloading gives the original RAW.

**A RAW beside a JPEG or HEIC of the same name is skipped** at scan, so a
photograph taken in RAW+JPEG shows once, as the JPEG. That is how cameras
write them and how every photo manager pairs them. A RAW indexed before its
JPEG appeared is marked missing, like any file no longer seen.

**No dependency, and nothing developed.** A RAW with no decodable JPEG inside
reports that. It is not shown as a thumbnail, and it is not shown wrong.

## Consequences

- Photographers' RAW folders appear in a picture library without converting
  anything.
- What is seen is the camera's rendering, not an edit. A RAW edited in
  Lightroom still shows the camera's JPEG until it is exported. Showing an edit
  would mean reading a sidecar's development settings and developing the RAW,
  which is a different decision.
- Tested against constructed files holding every trap above. No camera RAW
  was on the build machine, so a real one from each maker is owed.

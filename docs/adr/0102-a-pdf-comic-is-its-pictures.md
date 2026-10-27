# ADR 0102 — A PDF comic is its pictures

Date: 2026-10-27 · Status: **accepted** · Amends [ADR 0101](0101-a-comic-is-read-out-of-its-archive.md)

## Context

ADR 0101 read `.cbz` only. The first library tested against it was all PDFs
(five issues of a current series, as sold), and the scan reported "0 items"
with nothing to say why. PDF is how a good share of comics are bought and
kept, so a comics library that skips them is wrong where people start.

A PDF is a page-description language, and showing an arbitrary one means
rendering it: fonts, vector paths, transparency, colour management. No
renderer is in the standard library, and pdfium or MuPDF would be the largest
native dependency LANcast has ever taken, for a reader.

But a comic PDF is not an arbitrary one. The five tested files were 154 pages
holding 154 images: each page is one JPEG covering the page, drawn and nothing
else. That is how comic PDFs are made, by publishers and by scanners alike.

## Decision

**A page of a PDF comic is the picture it shows, served as it is in the file.**
Nothing is rendered. `internal/comic/pdf` reads the file's cross-reference
(tables, streams, object streams, the `/Prev` chain of an edited file), walks
the page tree, and for each page finds the image its resources hold. A JPEG
(`/DCTDecode`) is served byte for byte; a lossless 8-bit grey or RGB picture
(`/FlateDecode`, with or without a PNG predictor) is made into a PNG. Page
order is the page tree's, so it is the reading order the file declares.

**Indexing reads no image data.** It reads the index and the page objects —
1–2 ms for a 50 MB issue — so a page request opens the file, indexes it and
reads one stream, with no cache to go stale.

**What it does not read is refused, page by page, in words.** A page that is
text or drawing, a page cut into several pictures, a JPEG 2000 page (which
browsers cannot show), a masked or CMYK lossless page: each is a page in its
place whose image route answers `503` with the reason, and the reader says the
reason where the page would be. The pages either side still read. A small
second image beside the main one — a publisher's mark, a scanner's watermark —
is ignored when the main picture is at least 95% of the page's picture area.
A PDF where no page is a picture is a book of words and is not opened at all;
an encrypted one is refused.

**The cover is the first page that is a picture**, within the first five, so a
PDF opening on a notice page still has a cover.

**A file from anywhere is input to a parser, so it is bounded.** Nesting depth,
decompressed size, page count, page size, pixel count (36 MP for a lossless
page, about a 600 dpi scan) and the number of `/Prev` hops are all capped. A
lossless page whose data is shorter than its declared size is refused rather
than padded, because padding let a few-byte file make the server allocate for a
page it did not contain. A broken cross-reference — common, and the reason a
PDF "that opens everywhere" can still have a wrong table — is rebuilt by
reading every `n g obj` in the file, as every viewer does. The parser is fuzzed.

**The scan says what it skipped.** A comics library counts the comic files it
does not read (`.cbr`, `.cb7`, `.cbt`, `.cba`, ebooks) in `skipped_kind`, and
Settings › Libraries says how many and which formats it reads, so a library of
them no longer looks empty for no reason.

## Consequences

- No dependency. The reader is about a thousand lines of Go, standard library
  only, cross-checked against an independent parser (pypdf) on the five real
  issues: every page identical byte for byte.
- A PDF whose pages are drawn — a graphic novel exported from a layout program
  with live text — shows its pages that are pictures and names the rest. That
  is a real gap and the honest one; closing it is a renderer, and a renderer is
  a separate decision.
- `.cbr` and `.cb7` remain unread, as ADR 0101 decided; they are now counted
  instead of silent.

# Icon artifacts

Two generated files are committed so a build needs no image toolchain (the same
approach as `internal/web/dist`):

- **`lancast.ico`** — a multi-size (256/48/32/16) icon built from
  `LANcast_icon.png`. Embedded for the system tray (`internal/branding`).
- **`cmd/lancastd/lancastd_windows.syso`** — a Windows resource carrying the
  icon, so `lancastd.exe` shows the LANcast icon in Explorer. Linked only for
  `GOOS=windows` (the `_windows.syso` suffix).

## Regenerate

`lancast.ico` — a tiny Go program using `golang.org/x/image/draw` (CatmullRom)
downscales the master app icon **`web/public/icon-512.png`** to the four sizes and
wraps them as PNG-in-ICO. (The PWA icon set in `web/public` is the source of
truth for the app icon; the older art in this folder is not used for the exe.)

The `.syso` — from the `.ico`, with no image work:

```bash
go run github.com/akavel/rsrc@latest -ico assets/lancast.ico -arch amd64 -o cmd/lancastd/lancastd_windows.syso
```

Neither tool is a module dependency; both run via `go run`.

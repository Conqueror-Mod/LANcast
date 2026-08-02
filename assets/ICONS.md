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
downscales `LANcast_icon.png` to the four sizes and wraps them as PNG-in-ICO.

The `.syso` — from the `.ico`, with no image work:

```bash
go run github.com/akavel/rsrc@latest -ico assets/lancast.ico -arch amd64 -o cmd/lancastd/lancastd_windows.syso
```

Neither tool is a module dependency; both run via `go run`.

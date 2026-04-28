# pidisp

[![Go Reference](https://pkg.go.dev/badge/github.com/amnonbc/pidisp.svg)](https://pkg.go.dev/github.com/amnonbc/pidisp)

A Go module for opening a display on a Raspberry Pi and blitting `image.RGBA` frames onto it.

No CGO, no libdrm, no X11 — just raw Linux ioctls and mmap.

## Backends

| Backend | Path | Speed (800×480, Pi 2) | Requirements |
|---|---|---|---|
| DRM/KMS | `/dev/dri/card*` | ~1.66 ms/frame | DRM master (console, no display server) |
| fbdev | `/dev/fb0` | ~53 ms/frame (16 bpp) | Read/write on `/dev/fb0` |

`Open` tries DRM first (card0, card1, card2) and falls back to fbdev automatically.

## Usage

```go
import "github.com/amnonbc/pidisp"

d, err := pidisp.Open(pidisp.Options{Rotate: true})
if err != nil {
    log.Fatal(err)
}
defer d.Close()

img := image.NewRGBA(image.Rect(0, 0, d.Width(), d.Height()))
// draw into img ...
d.Blit(img)
```

## Cross-compilation

```
# Pi 2/3 (ARMv7)
GOOS=linux GOARCH=arm GOARM=7 go build ./...

# Pi 1 (ARMv6)
GOOS=linux GOARCH=arm GOARM=6 go build ./...

# Pi 4/5 (ARM64)
GOOS=linux GOARCH=arm64 go build ./...
```

## DRM/KMS setup (Pi OS)

Add to `/boot/config.txt`:
```
dtoverlay=vc4-kms-v3d
```

Run the process on the console with no display server active (DRM requires DRM master).

## Comparison with other libraries

Other Go framebuffer libraries exist, but pidisp fills a specific niche:

| Feature | pidisp | Others (gonutz/different55/etc) |
|---|---|---|
| DRM/KMS support | ✓ (with hardware rotation) | ✗ fbdev only |
| Auto-fallback | ✓ tries DRM, falls back to fbdev | ✗ single backend |
| Hardware acceleration | ✓ ABGR8888 zero-copy, plane rotation | ✗ per-pixel software operations |
| Performance (Pi 2) | 1.66 ms/frame | ~53 ms/frame |
| CGO-free | ✓ raw ioctls only | varies |
| Modern Pi OS support | ✓ (DRM default) | ⚠️ (fbdev deprecated) |

**When to use pidisp:**
- Raspberry Pi graphics without a display server
- Any Linux system with DRM/KMS display hardware
- High-performance graphics (~32× faster than fbdev on DRM-capable hardware)
- Simple API that abstracts away backend selection

**Limitations:**
- Linux only (DRM/fbdev not available on other platforms)
- Requires DRM master for DRM backend (console-only, no display server)
- `image.RGBA` format only (no color space conversion)

## Sub-packages

- [`drm`](./drm) — DRM/KMS backend; ABGR8888 + hardware rotation for maximum performance
- [`fb`](./fb) — fbdev fallback; supports 16 bpp (RGB565 with Bayer dithering) and 32 bpp

## Alternatives
- https://github.com/gonutz/framebuffer - cgo based library, without DRM acceleration.

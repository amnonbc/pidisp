# pidisp

[![Go Reference](https://pkg.go.dev/badge/github.com/amnonbc/pidisp.svg)](https://pkg.go.dev/github.com/amnonbc/pidisp)

A Go module for opening a display on a Raspberry Pi and rendering `image.RGBA` frames with zero-copy double-buffering.

No CGO, no libdrm, no X11 — just raw Linux ioctls and mmap.

## Backends

| Backend | Path | Speed (sync) | Speed (async) | Requirements |
|---|---|---|---|---|
| DRM/KMS | `/dev/dri/card*` | ~17 ms/frame | ~1 ms/frame | DRM master (console, no display server) |
| fbdev | `/dev/fb0` | ~40 ms/frame (16 bpp) | N/A | Read/write on `/dev/fb0` |

`Open` tries DRM first (card0, card1, card2) and falls back to fbdev automatically.

**Async mode:** DRM supports `AsyncFlip: true` to skip vsync synchronization, reducing latency ~17×. Trade-off: may cause screen tearing.

## Usage

### Zero-copy rendering with tear-free display

Render directly into hardware-mapped memory on DRM (no per-frame memcopy). `Flip()` waits for vsync to prevent tearing:

```go
import "github.com/amnonbc/pidisp"

d, err := pidisp.Open(pidisp.Options{Rotate: true})
if err != nil {
    log.Fatal(err)
}
defer d.Close()

// Render loop
for {
    // Get the back buffer (not currently displayed)
    buf := d.BackBuffer()
    // Draw your frame into buf...
    
    // Flip to display the rendered frame
    if err := d.Flip(); err != nil {
        log.Printf("flip failed: %v", err)
    }
}
```

### Low-latency rendering with async flip

Skip vsync synchronization for ~17× lower latency. Trade-off: may cause screen tearing.

```go
import (
    "errors"
    "syscall"
    "github.com/amnonbc/pidisp"
)

d, err := pidisp.Open(pidisp.Options{AsyncFlip: true})
if err != nil {
    log.Fatal(err)
}
defer d.Close()

for {
    buf := d.BackBuffer()
    // Draw...
    if err := d.Flip(); err != nil {
        // EBUSY: previous flip still pending (frame dropped) — expected, keep going
        // Other errors: log them
        if !errors.Is(err, syscall.EBUSY) {
            log.Printf("flip failed: %v", err)
        }
    }
}
```

On DRM, rendering is directly into hardware-mapped memory. On fbdev, `Flip()` copies the back buffer to the hardware framebuffer. `Flip()` now returns an error if the operation fails.

### Legacy API (Blit)

For backward compatibility, the old `Blit()` method is still available:

```go
img := image.NewRGBA(image.Rect(0, 0, d.Width(), d.Height()))
// draw into img ...
d.Blit(img)  // deprecated; use BackBuffer/Flip instead
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

## Benchmarks

Measured on Raspberry Pi 2 (ARMv7) with 800×480 display:

| Operation | Time | Notes |
|-----------|------|-------|
| `BackBuffer()` | 62 ns | Zero-cost buffer access |
| `Flip()` with vsync | 17.2 ms | **Waits for display refresh** |
| `Flip()` without vsync | 1.01 ms | **Returns immediately** |
| Drawing 100×100 pixels | 970 µs | Typical partial update |
| fbdev full copy | 40 ms | Software framebuffer conversion |

### Understanding Vsync

**With Vsync (sync flip):**
- `Flip()` blocks until the next vertical refresh (~60 Hz = 16.6 ms interval on Pi)
- Display shows frame exactly at refresh boundary → **no tearing**
- Latency: ~17 ms per frame
- Best for: UI apps, video playback, anything not requiring sub-frame latency

**Without Vsync (async flip):**
- `Flip()` returns immediately (~1 ms)
- Display may switch mid-frame if refresh happens during update → **potential tearing**
- Latency: ~1 ms per frame
- Best for: games, real-time apps, streaming (where low latency > visual perfection)

**The tradeoff:** Async flip is ~17× faster but may show visible horizontal tearing lines. Choose based on your app's priorities.

Run benchmarks:
```bash
go test -bench=. -benchtime=100ms
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
| Double-buffering | ✓ (zero-copy on DRM) | ✗ single buffer |
| Async flip | ✓ (low-latency mode) | ✗ sync only |
| Auto-fallback | ✓ tries DRM, falls back to fbdev | ✗ single backend |
| Hardware acceleration | ✓ ABGR8888 zero-copy, plane rotation | ✗ per-pixel software operations |
| Error handling | ✓ Flip() returns error | ✗ silent failures |
| Performance (Pi 2, sync) | 17 ms/frame (DRM), 40 ms/frame (fbdev) | ~53 ms/frame |
| Performance (Pi 2, async) | 1 ms/frame (DRM only) | N/A |
| CGO-free | ✓ raw ioctls only | varies |
| Modern Pi OS support | ✓ (DRM default) | ⚠️ (fbdev deprecated) |

**When to use pidisp:**
- Raspberry Pi graphics without a display server
- Any Linux system with DRM/KMS display hardware
- High-performance graphics (17× faster than fbdev on DRM-capable hardware)
- Low-latency rendering (async flip mode: 1ms/frame for games, streaming)
- Simple API that abstracts away backend selection and error handling

**Limitations:**
- Linux only (DRM/fbdev not available on other platforms)
- Requires DRM master for DRM backend (console-only, no display server)
- `image.RGBA` format only (no color space conversion)

## Sub-packages

- [`drm`](./drm) — DRM/KMS backend; ABGR8888 + hardware rotation for maximum performance
- [`fb`](./fb) — fbdev fallback; supports 16 bpp (RGB565 with Bayer dithering) and 32 bpp

## Alternatives
- https://github.com/gonutz/framebuffer - cgo based library, without DRM acceleration.
- https://github.com/gogpu/gg - enterprise grade 2D graphics library for Go.

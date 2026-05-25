# pidisp Demo

A colorful animated pattern demo for pidisp, showcasing zero-copy rendering on Raspberry Pi displays.

## Patterns

The demo alternates between two animated patterns every 50 frames:

### Gradient Sweep
A rotating HSV color gradient that cycles through the full color spectrum horizontally, with saturation increasing vertically. Creates a smooth, continuous color animation.

### Bouncing Rectangles
Six colored rectangles bouncing around the screen with sinusoidal motion. Each rectangle moves independently with different phase offsets, on a dark background.

## Building

```bash
go build -o demo ./cmd/demo
```

## Usage

### Basic
```bash
./demo
```
Runs indefinitely with tear-free vsync-locked rendering (~17 ms/frame on DRM).

### Low-Latency Animation
```bash
./demo -async
```
Skips vsync synchronization for ~17× lower latency (~1 ms/frame on DRM). May produce visible screen tearing but provides smoother motion for games or high-performance apps.

### Time-Limited
```bash
./demo -duration 30s
```
Runs for 30 seconds, then exits with FPS report.

```bash
./demo -frames 1000
```
Renders exactly 1000 frames, then exits with FPS report.

## Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-frames` | int | 0 (infinite) | Number of frames to render before exiting |
| `-duration` | duration | 0 (infinite) | How long to run (e.g., "10s", "5m") |
| `-async` | bool | false | Use async flip mode (low-latency, may tear) |

## Performance

On Raspberry Pi 2 (ARMv7, 800×480 display):

| Mode | Latency | Tearing | Best For |
|------|---------|---------|----------|
| Sync (default) | ~17 ms/frame | None | UI, smooth visuals |
| Async (`-async`) | ~1 ms/frame | Possible | Games, real-time, streaming |

## Notes

- Patterns are generated in real-time (not pre-computed)
- Drawing uses direct pixel writes to `BackBuffer()` for maximum efficiency
- Color conversion (HSV → RGB) happens in software
- EBUSY errors in async mode (previous flip still pending) are silently handled—frames are dropped as expected
- Exit with Ctrl+C (or let `-frames` or `-duration` terminate it)

## See Also

- [pidisp README](../../README.md) for library documentation
- [cmd/diagnose](../diagnose) for hardware testing

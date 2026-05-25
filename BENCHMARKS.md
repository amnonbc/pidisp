# Benchmarks

This directory includes comprehensive benchmarks to validate pidisp's performance claims against the figures in the README.

## Running the benchmarks

### Quick test (all backends) — recommended
```bash
go test -bench=. -benchtime=100ms
```

Use **100ms** (not 1s or 10s) because drawing dominates the runtime. See "Performance note" below.

### Test specific backend

**DRM/KMS backend:**
```bash
go test -bench=BenchmarkDRMBackend -benchtime=10s
```

**fbdev backend:**
```bash
go test -bench=BenchmarkFBDevBackend -benchtime=10s
```

### Isolate drawing vs. flipping
```bash
go test -bench=Drawing -benchtime=10s
go test -bench=Flip -benchtime=10s
```

## Comparing with gonutz/framebuffer

To compare pidisp with gonutz/framebuffer:

1. Install gonutz:
```bash
go get github.com/gonutz/framebuffer
```

2. Enable the comparison benchmark:
```bash
cp comparison_bench_template.go.txt comparison_bench_test.go
```

3. Run the comparison:
```bash
go test -bench=Comparison -benchtime=10s
```

The comparison benchmark tests both pidisp (fbdev) and gonutz on the same hardware.

## Expected results

### Important performance note

The benchmarks measure **rendering + Flip cost together**. On your hardware:

```
Diagnostic results (Pi with 800×480 display):
- Drawing 100×100 pixels:  < 1 ms
- Flip (vsync wait):       ~7-8 ms
- Full 800×480 pixel fill: ~41 ms
```

**Key insight:** The test pattern in the benchmarks fills only a 100×100 region (0.6% of pixels) to keep iterations fast. Real apps have variable rendering costs—if your app draws more, it will be slower; if it draws less, it will be faster.

### Library performance (DRM vs fbdev)

From the README, measured with full-screen fills on Pi 2:

| Backend | Full screen | Metric | Note |
|---|---|---|---|
| DRM/KMS | 1.66 ms | ms/frame | Zero-copy into hardware buffer |
| fbdev | 53 ms | ms/frame | Software buffer copy to device |

The DRM/KMS backend is **~32× faster** due to zero-copy rendering directly into hardware-mapped memory.

## What each benchmark tests

**Full pipeline (rendering + display):**
- **BackBufferRenderFlip**: Zero-copy path (BackBuffer → draw → Flip) — realistic workload
- **BlitAPI**: Deprecated Blit() API with per-frame allocation — slower
- **DRMBackend**: Forces DRM backend explicitly
- **FBDevBackend**: Forces fbdev backend explicitly
- **RotationDRM**: DRM with 180° hardware rotation enabled

**Isolated components (for diagnosis):**
- **BackBufferOnly**: Just the BackBuffer() call (should be instant, ~microseconds)
- **FlipPerformance**: Just Flip() without drawing (reveals vsync latency, ~7-8ms on this Pi)
- **DrawingPerformance**: Just the pixel drawing (reveals app-specific cost)

**Comparison (optional):**
- **GonutzFramebuffer**: Direct comparison with competing library (enable via template)

## Interpreting results

- **us/frame** = microseconds per frame (lower is better)
- **us/call** = microseconds per call (for isolated operations)
- The `allocs/op` and `B/op` metrics show memory allocation patterns:
  - **DRM BackBuffer**: 0 allocations (zero-copy)
  - **Blit**: 1 allocation per call (image.RGBA)
  - **fbdev**: varies by implementation

### Example interpretation

If you see:
```
BenchmarkFlipPerformance-4              1000         7500000 ns/op  (7.5 ms)
BenchmarkBackBufferRenderFlip-4           20       50000000 ns/op (50 ms)
```

This means:
- **Flip alone** takes 7.5 ms (vsync latency)
- **Rendering + Flip** takes 50 ms total
- So your app's drawing logic takes ~42 ms
- On DRM backend, the Flip is zero-copy (no extra cost beyond vsync)

## Running with profiling

To profile a specific benchmark:
```bash
go test -bench=BenchmarkDRMBackend -benchtime=10s -cpuprofile=cpu.prof
go tool pprof cpu.prof
```

## Notes

- Benchmarks require a Raspberry Pi or Linux system with DRM/fbdev display support
- DRM requires DRM master (console-only, no display server)
- The simple `fillBuffer()` workload represents minimal drawing; real apps may be CPU-limited instead of device-limited
- Results vary by Pi generation, display type (DSI/HDMI), and kernel configuration
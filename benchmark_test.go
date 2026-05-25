package pidisp

import (
	"image"
	"image/color"
	"log/slog"
	"os"
	"testing"
)

func init() {
	// Suppress INFO logs during benchmarks for cleaner output
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	})))
}


// BenchmarkBackBufferRenderFlip benchmarks the zero-copy rendering path.
// This tests the optimized BackBuffer() -> draw -> Flip() workflow.
func BenchmarkBackBufferRenderFlip(b *testing.B) {
	d, err := Open(Options{})
	if err != nil {
		b.Skipf("Display unavailable: %v", err)
	}
	defer d.Close()

	w, h := d.Width(), d.Height()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := d.BackBuffer()
		fillBuffer(buf, w, h, uint32(i%256))
		if err := d.Flip(); err != nil {
			b.Fatalf("Flip failed: %v", err)
		}
	}
	b.ReportMetric(float64(b.Elapsed().Microseconds())/float64(b.N), "us/frame")
}

// BenchmarkBlitAPI benchmarks the deprecated Blit() API with per-frame allocation.
func BenchmarkBlitAPI(b *testing.B) {
	d, err := Open(Options{})
	if err != nil {
		b.Skipf("Display unavailable: %v", err)
	}
	defer d.Close()


	w, h := d.Width(), d.Height()
	img := image.NewRGBA(image.Rect(0, 0, w, h))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fillBuffer(img, w, h, uint32(i%256))
		d.Blit(img)
	}
	b.ReportMetric(float64(b.Elapsed().Microseconds())/float64(b.N), "us/frame")
}

// BenchmarkDRMBackend forces DRM backend and measures full-screen render time.
func BenchmarkDRMBackend(b *testing.B) {
	d, err := Open(Options{ForceFB: false})
	if err != nil {
		b.Skipf("DRM unavailable: %v", err)
	}
	defer d.Close()

	w, h := d.Width(), d.Height()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := d.BackBuffer()
		fillBuffer(buf, w, h, uint32(i%256))
		if err := d.Flip(); err != nil {
			b.Fatalf("Flip failed: %v", err)
		}
	}
	b.ReportMetric(float64(b.Elapsed().Microseconds())/float64(b.N), "us/frame")
}

// BenchmarkFBDevBackend forces fbdev backend and measures full-screen render time.
func BenchmarkFBDevBackend(b *testing.B) {
	d, err := Open(Options{ForceFB: true})
	if err != nil {
		b.Skipf("fbdev unavailable: %v", err)
	}
	defer d.Close()

	w, h := d.Width(), d.Height()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := d.BackBuffer()
		fillBuffer(buf, w, h, uint32(i%256))
		if err := d.Flip(); err != nil {
			b.Fatalf("Flip failed: %v", err)
		}
	}
	b.ReportMetric(float64(b.Elapsed().Microseconds())/float64(b.N), "us/frame")
}

// BenchmarkRotationDRM measures rendering with hardware rotation enabled.
func BenchmarkRotationDRM(b *testing.B) {
	d, err := Open(Options{Rotate: true, ForceFB: false})
	if err != nil {
		b.Skipf("DRM unavailable: %v", err)
	}
	defer d.Close()

	w, h := d.Width(), d.Height()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := d.BackBuffer()
		fillBuffer(buf, w, h, uint32(i%256))
		if err := d.Flip(); err != nil {
			b.Fatalf("Flip failed: %v", err)
		}
	}
	b.ReportMetric(float64(b.Elapsed().Microseconds())/float64(b.N), "us/frame")
}

// BenchmarkDrawingPerformance isolates just the drawing operation
// to separate rendering cost from Flip cost.
func BenchmarkDrawingPerformance(b *testing.B) {
	d, err := Open(Options{})
	if err != nil {
		b.Skipf("Display unavailable: %v", err)
	}
	defer d.Close()

	w, h := d.Width(), d.Height()
	buf := d.BackBuffer()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fillBuffer(buf, w, h, uint32(i%256))
	}
	b.ReportMetric(float64(b.Elapsed().Microseconds())/float64(b.N), "us/frame")
}

// BenchmarkFlipPerformance isolates just the Flip operation cost.
func BenchmarkFlipPerformance(b *testing.B) {
	d, err := Open(Options{})
	if err != nil {
		b.Skipf("Display unavailable: %v", err)
	}
	defer d.Close()

	w, h := d.Width(), d.Height()
	buf := d.BackBuffer()
	fillBuffer(buf, w, h, 0xFF)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := d.Flip(); err != nil {
			b.Fatalf("Flip failed: %v", err)
		}
	}
	b.ReportMetric(float64(b.Elapsed().Microseconds())/float64(b.N), "us/frame")
}

// BenchmarkBackBufferOnly measures just the BackBuffer() call (should be instant).
func BenchmarkBackBufferOnly(b *testing.B) {
	d, err := Open(Options{})
	if err != nil {
		b.Skipf("Display unavailable: %v", err)
	}
	defer d.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = d.BackBuffer()
	}
	b.ReportMetric(float64(b.Elapsed().Microseconds())/float64(b.N), "us/call")
}

// BenchmarkAsyncFlip measures page flip with async mode enabled (no vsync wait).
// Only available on DRM backend; falls back to fbdev if DRM unavailable.
func BenchmarkAsyncFlip(b *testing.B) {
	d, err := Open(Options{AsyncFlip: true})
	if err != nil {
		b.Skipf("Display unavailable: %v", err)
	}
	defer d.Close()

	w, h := d.Width(), d.Height()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := d.BackBuffer()
		fillBuffer(buf, w, h, uint32(i%256))
		if err := d.Flip(); err != nil {
			b.Fatalf("Flip failed: %v", err)
		}
	}
	b.ReportMetric(float64(b.Elapsed().Microseconds())/float64(b.N), "us/frame")
}

// fillBuffer fills a small region of the buffer (realistic app workload).
// Real apps don't redraw the entire screen—they draw to specific regions.
func fillBuffer(buf *image.RGBA, w, h int, color32 uint32) {
	if buf == nil {
		return
	}
	c := color.RGBA{
		R: uint8((color32 >> 16) & 0xFF),
		G: uint8((color32 >> 8) & 0xFF),
		B: uint8(color32 & 0xFF),
		A: 0xFF,
	}
	// Fill only a 100x100 region (realistic partial update)
	size := 100
	if w < size {
		size = w
	}
	if h < size {
		size = h
	}
	for x := 0; x < size; x++ {
		for y := 0; y < size; y++ {
			buf.SetRGBA(x, y, c)
		}
	}
}

// Command demo renders animated colorful patterns on a Raspberry Pi display.
package main

import (
	"errors"
	"flag"
	"image"
	"image/color"
	"log"
	"math"
	"syscall"
	"time"

	"github.com/amnonbc/pidisp"
)

func main() {
	var (
		frames   = flag.Int("frames", 0, "number of frames to render (0 = infinite)")
		async    = flag.Bool("async", false, "use async flip for lower latency (may tear)")
		duration = flag.Duration("duration", 0, "run for this duration (0 = use -frames)")
	)
	flag.Parse()

	d, err := pidisp.Open(pidisp.Options{
		AsyncFlip: *async,
	})
	if err != nil {
		log.Fatal(err)
	}
	defer d.Close()

	log.Printf("Display: %dx%d", d.Width(), d.Height())
	if *async {
		log.Println("Mode: async flip (low-latency, may tear)")
	} else {
		log.Println("Mode: sync flip (tear-free)")
	}

	frame := 0
	start := time.Now()

	for {
		// Check duration limit
		if *duration > 0 && time.Since(start) >= *duration {
			break
		}

		// Check frame limit
		if *frames > 0 && frame >= *frames {
			break
		}

		// Get back buffer for rendering
		buf := d.BackBuffer()

		// Render animated pattern
		if frame%100 < 50 {
			renderGradientSweep(buf, d.Width(), d.Height(), frame)
		} else {
			renderBouncingRects(buf, d.Width(), d.Height(), frame)
		}

		// Flip to display
		if err := d.Flip(); err != nil {
			// In async mode, EBUSY means the previous flip is still pending.
			// This is normal behavior—just drop the frame and continue.
			if !errors.Is(err, syscall.EBUSY) {
				log.Printf("flip failed: %v", err)
			}
		}

		frame++

		// Rough 60 FPS pacing (not critical for demo)
		time.Sleep(time.Millisecond * 16)
	}

	elapsed := time.Since(start)
	fps := float64(frame) / elapsed.Seconds()
	log.Printf("Rendered %d frames in %v (%.1f fps)", frame, elapsed, fps)
}

// renderGradientSweep fills the display with a rotating color gradient.
func renderGradientSweep(buf *image.RGBA, width, height int, frame int) {
	hueOffset := float64(frame%360) / 360
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			hue := math.Mod(float64(x)/float64(width)+hueOffset, 1.0)
			sat := float64(y) / float64(height)
			val := 1.0
			r, g, b := hsv2rgb(hue, sat, val)
			buf.SetRGBA(x, y, color.RGBA{R: r, G: g, B: b, A: 0xFF})
		}
	}
}

// renderBouncingRects draws animated colored rectangles bouncing around.
func renderBouncingRects(buf *image.RGBA, width, height int, frame int) {
	// Fill background
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			buf.SetRGBA(x, y, color.RGBA{R: 20, G: 20, B: 30, A: 0xFF})
		}
	}

	// Draw bouncing rectangles
	rectSize := 40
	colors := []color.RGBA{
		{R: 255, G: 0, B: 0, A: 0xFF},     // Red
		{R: 0, G: 255, B: 0, A: 0xFF},     // Green
		{R: 0, G: 0, B: 255, A: 0xFF},     // Blue
		{R: 255, G: 255, B: 0, A: 0xFF},   // Yellow
		{R: 255, G: 0, B: 255, A: 0xFF},   // Magenta
		{R: 0, G: 255, B: 255, A: 0xFF},   // Cyan
	}

	for i, col := range colors {
		phase := float64((frame + i*50) % 200)
		x := int(float64(width-rectSize) * (0.5 + 0.5*math.Sin(phase*math.Pi/100)))
		y := int(float64(height-rectSize) * (0.5 + 0.5*math.Cos((phase+float64(i)*60)*math.Pi/100)))

		drawFilledRect(buf, x, y, rectSize, rectSize, col)
	}
}

// drawFilledRect draws a filled rectangle.
func drawFilledRect(buf *image.RGBA, x, y, w, h int, col color.RGBA) {
	for dy := 0; dy < h; dy++ {
		for dx := 0; dx < w; dx++ {
			px := x + dx
			py := y + dy
			if px >= 0 && px < buf.Bounds().Max.X && py >= 0 && py < buf.Bounds().Max.Y {
				buf.SetRGBA(px, py, col)
			}
		}
	}
}

// hsv2rgb converts HSV (hue [0,1], saturation [0,1], value [0,1]) to RGB.
func hsv2rgb(h, s, v float64) (uint8, uint8, uint8) {
	c := v * s
	hp := math.Mod(h*6, 6)
	x := c * (1 - math.Abs(math.Mod(hp, 2)-1))

	var r, g, b float64
	switch int(hp) {
	case 0:
		r, g, b = c, x, 0
	case 1:
		r, g, b = x, c, 0
	case 2:
		r, g, b = 0, c, x
	case 3:
		r, g, b = 0, x, c
	case 4:
		r, g, b = x, 0, c
	case 5:
		r, g, b = c, 0, x
	}

	m := v - c
	r, g, b = r+m, g+m, b+m
	return uint8(r * 255), uint8(g * 255), uint8(b * 255)
}

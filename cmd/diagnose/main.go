package main

import (
	"fmt"
	"image/color"
	"log"
	"time"

	"github.com/amnonbc/pidisp"
)

func main() {
	fmt.Println("=== pidisp Hang Diagnosis ===\n")

	// Test 1: Open
	fmt.Print("1. Testing Open()... ")
	start := time.Now()
	d, err := pidisp.Open(pidisp.Options{})
	elapsed := time.Since(start)
	if err != nil {
		fmt.Printf("FAILED after %v\n", elapsed)
		log.Fatalf("   Error: %v\n", err)
	}
	fmt.Printf("OK (%v)\n", elapsed)
	defer d.Close()

	// Test 2: Get dimensions
	fmt.Print("2. Testing Width/Height... ")
	start = time.Now()
	w, h := d.Width(), d.Height()
	elapsed = time.Since(start)
	fmt.Printf("OK (%v) - %dx%d\n", elapsed, w, h)

	// Test 3: BackBuffer
	fmt.Print("3. Testing BackBuffer()... ")
	start = time.Now()
	buf := d.BackBuffer()
	elapsed = time.Since(start)
	if buf == nil {
		fmt.Printf("FAILED - returned nil\n")
		return
	}
	fmt.Printf("OK (%v)\n", elapsed)

	// Test 4: Draw into buffer (solid color)
	fmt.Print("4. Testing drawing into buffer... ")
	start = time.Now()
	red := color.RGBA{R: 255, G: 0, B: 0, A: 255}
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			buf.SetRGBA(x, y, red)
		}
	}
	elapsed = time.Since(start)
	fmt.Printf("OK (%v)\n", elapsed)

	// Test 5: Flip (this is where it likely hangs)
	fmt.Print("5. Testing Flip() - waiting for vsync... ")
	start = time.Now()
	d.Flip()
	elapsed = time.Since(start)
	fmt.Printf("OK (%v)\n", elapsed)

	// Test 6: Second flip
	fmt.Print("6. Testing second Flip()... ")
	buf = d.BackBuffer()
	green := color.RGBA{R: 0, G: 255, B: 0, A: 255}
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			buf.SetRGBA(x, y, green)
		}
	}
	start = time.Now()
	d.Flip()
	elapsed = time.Since(start)
	fmt.Printf("OK (%v)\n", elapsed)

	fmt.Println("\n✓ All steps completed successfully!")
	fmt.Println("\nIf Flip() takes 16-60ms, that's normal (waiting for vsync).")
	fmt.Println("If Flip() takes >2 seconds or hangs forever, there's an issue.")
}

package encoder

import (
	"bytes"
	"image/jpeg"
	"testing"

	"rcupc/pkg/capture"
)

// makeFrame builds a deterministic top-down BGRA frame. stride 0 means
// width*4. fill returns RGB (A is set opaque).
func makeFrame(width, height, stride int, fill func(x, y int) (r, g, b byte)) *capture.Frame {
	if stride == 0 {
		stride = width * 4
	}
	pixels := make([]byte, stride*height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r, g, b := fill(x, y)
			i := y*stride + x*4
			pixels[i+0] = b
			pixels[i+1] = g
			pixels[i+2] = r
			pixels[i+3] = 0xff
		}
	}
	return &capture.Frame{
		Width:  width,
		Height: height,
		Stride: stride,
		Format: capture.FormatBGRA,
		Pixels: pixels,
	}
}

func gradient(x, y int) (byte, byte, byte) {
	return byte(x * 8), byte(y * 8), byte((x + y) * 4)
}

func TestNewJPEGQualityValidation(t *testing.T) {
	for _, q := range []int{1, 50, 80, 100} {
		enc, err := NewJPEG(q)
		if err != nil {
			t.Errorf("NewJPEG(%d) = %v, want nil", q, err)
			continue
		}
		if enc.Quality() != q {
			t.Errorf("Quality() = %d, want %d", enc.Quality(), q)
		}
	}
	for _, q := range []int{-1, 0, 101, 1000} {
		if _, err := NewJPEG(q); err == nil {
			t.Errorf("NewJPEG(%d) = nil error, want error", q)
		}
	}
}

func TestDefaultQualityInRange(t *testing.T) {
	if DefaultQuality < MinQuality || DefaultQuality > MaxQuality {
		t.Fatalf("DefaultQuality = %d, want within [%d, %d]", DefaultQuality, MinQuality, MaxQuality)
	}
}

func TestEncodeValidFrame(t *testing.T) {
	enc, err := NewJPEG(DefaultQuality)
	if err != nil {
		t.Fatal(err)
	}

	frame := makeFrame(16, 8, 0, gradient)
	ef, err := enc.Encode(frame)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	if ef.Format != FormatJPEG {
		t.Errorf("Format = %v, want %v", ef.Format, FormatJPEG)
	}
	if ef.Width != frame.Width || ef.Height != frame.Height {
		t.Errorf("dimensions = %dx%d, want %dx%d", ef.Width, ef.Height, frame.Width, frame.Height)
	}
	if len(ef.Data) == 0 {
		t.Fatal("encoded data is empty")
	}
	// JPEG SOI marker.
	if ef.Data[0] != 0xFF || ef.Data[1] != 0xD8 {
		t.Fatalf("data does not start with a JPEG SOI marker: %x", ef.Data[:2])
	}
}

func TestRoundTrip(t *testing.T) {
	enc, err := NewJPEG(90)
	if err != nil {
		t.Fatal(err)
	}

	frame := makeFrame(24, 16, 0, gradient)
	ef, err := enc.Encode(frame)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	img, err := jpeg.Decode(bytes.NewReader(ef.Data))
	if err != nil {
		t.Fatalf("jpeg.Decode: %v", err)
	}
	b := img.Bounds()
	if b.Dx() != frame.Width || b.Dy() != frame.Height {
		t.Fatalf("decoded dimensions = %dx%d, want %dx%d", b.Dx(), b.Dy(), frame.Width, frame.Height)
	}

	// JPEG is lossy, so require content rather than exact pixels.
	nonZero := false
	for y := b.Min.Y; y < b.Max.Y && !nonZero; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			if r != 0 || g != 0 || bl != 0 {
				nonZero = true
				break
			}
		}
	}
	if !nonZero {
		t.Fatal("decoded image is entirely black")
	}
}

func TestEncodePreservesColor(t *testing.T) {
	enc, err := NewJPEG(95)
	if err != nil {
		t.Fatal(err)
	}

	// Solid red. BGRA stores B,G,R,A.
	frame := makeFrame(16, 16, 0, func(int, int) (byte, byte, byte) { return 255, 0, 0 })
	ef, err := enc.Encode(frame)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	img, err := jpeg.Decode(bytes.NewReader(ef.Data))
	if err != nil {
		t.Fatalf("jpeg.Decode: %v", err)
	}
	r, g, b, _ := img.At(8, 8).RGBA()
	R, G, B := r>>8, g>>8, b>>8
	if R < 200 {
		t.Errorf("red channel = %d, want >= 200", R)
	}
	if G > 60 || B > 60 {
		t.Errorf("green/blue = %d/%d, want low (channel order swapped?)", G, B)
	}
}

func TestEncodePaddedStride(t *testing.T) {
	enc, err := NewJPEG(DefaultQuality)
	if err != nil {
		t.Fatal(err)
	}

	// width*4 = 12, but rows are padded to 20 bytes.
	frame := makeFrame(3, 4, 20, gradient)
	ef, err := enc.Encode(frame)
	if err != nil {
		t.Fatalf("Encode with padded stride: %v", err)
	}
	if ef.Width != 3 || ef.Height != 4 {
		t.Fatalf("dimensions = %dx%d, want 3x4", ef.Width, ef.Height)
	}

	img, err := jpeg.Decode(bytes.NewReader(ef.Data))
	if err != nil {
		t.Fatalf("jpeg.Decode: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 3 || b.Dy() != 4 {
		t.Fatalf("decoded dimensions = %dx%d, want 3x4", b.Dx(), b.Dy())
	}
}

func TestEncodeDoesNotMutateSource(t *testing.T) {
	enc, err := NewJPEG(DefaultQuality)
	if err != nil {
		t.Fatal(err)
	}

	frame := makeFrame(16, 8, 0, gradient)
	before := append([]byte(nil), frame.Pixels...)

	if _, err := enc.Encode(frame); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Equal(before, frame.Pixels) {
		t.Fatal("Encode mutated the source frame pixels")
	}
}

func TestEncodeRejectsInvalidFrames(t *testing.T) {
	enc, err := NewJPEG(DefaultQuality)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		frame *capture.Frame
	}{
		{"nil", nil},
		{"zero width", &capture.Frame{Width: 0, Height: 2, Stride: 8, Format: capture.FormatBGRA, Pixels: make([]byte, 16)}},
		{"negative height", &capture.Frame{Width: 2, Height: -1, Stride: 8, Format: capture.FormatBGRA, Pixels: make([]byte, 16)}},
		{"short stride", &capture.Frame{Width: 2, Height: 2, Stride: 4, Format: capture.FormatBGRA, Pixels: make([]byte, 8)}},
		{"wrong buffer size", &capture.Frame{Width: 2, Height: 2, Stride: 8, Format: capture.FormatBGRA, Pixels: make([]byte, 15)}},
		{"unsupported format", &capture.Frame{Width: 2, Height: 2, Stride: 8, Format: 0, Pixels: make([]byte, 16)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := enc.Encode(tt.frame); err == nil {
				t.Fatal("Encode = nil error, want error")
			}
		})
	}
}

func TestEncodeMultipleFramesIndependent(t *testing.T) {
	enc, err := NewJPEG(90)
	if err != nil {
		t.Fatal(err)
	}

	red := makeFrame(8, 8, 0, func(int, int) (byte, byte, byte) { return 255, 0, 0 })
	green := makeFrame(8, 8, 0, func(int, int) (byte, byte, byte) { return 0, 255, 0 })

	redEnc, err := enc.Encode(red)
	if err != nil {
		t.Fatalf("Encode red: %v", err)
	}
	greenEnc, err := enc.Encode(green)
	if err != nil {
		t.Fatalf("Encode green: %v", err)
	}

	if bytes.Equal(redEnc.Data, greenEnc.Data) {
		t.Fatal("different frames produced identical encoded data")
	}
	for _, ef := range []*EncodedFrame{redEnc, greenEnc} {
		if _, err := jpeg.Decode(bytes.NewReader(ef.Data)); err != nil {
			t.Fatalf("decode %v: %v", ef.Format, err)
		}
	}

	// The two encoded buffers must not alias: mutating one leaves the other.
	probe := greenEnc.Data[0]
	redEnc.Data[0] ^= 0xFF
	if greenEnc.Data[0] != probe {
		t.Fatal("encoded buffers alias between frames")
	}
}

// Package encoder turns a raw capture.Frame into a compressed image frame.
//
// Phase 9 only implements MJPEG: every frame is independently encoded as a
// JPEG. There is no inter-frame dependency, no video container and no network
// framing. The package knows nothing about TCP, sessions, the RCUPC protocol,
// logging, configuration or the frame pipeline; it may depend on pkg/capture
// because that is where frames come from.
package encoder

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"

	"rcupc/pkg/capture"
)

// Format identifies the compression format of an EncodedFrame.
type Format uint8

const (
	// FormatJPEG is a single, independently decodable JPEG image. A stream of
	// these frames is MJPEG.
	FormatJPEG Format = iota + 1
)

func (f Format) String() string {
	switch f {
	case FormatJPEG:
		return "JPEG"
	default:
		return fmt.Sprintf("Format(%d)", uint8(f))
	}
}

// EncodedFrame is one compressed frame. Data is owned by the caller and is
// never reused by the encoder.
type EncodedFrame struct {
	Format Format
	Width  int
	Height int
	Data   []byte
}

// Encoder compresses a raw capture frame. Implementations must not mutate the
// input frame.
type Encoder interface {
	Encode(*capture.Frame) (*EncodedFrame, error)
}

// JPEG quality range accepted by image/jpeg.
const (
	MinQuality     = 1
	MaxQuality     = 100
	DefaultQuality = 80
)

// JPEGEncoder encodes frames as independent JPEG images using image/jpeg.
type JPEGEncoder struct {
	quality int
}

var _ Encoder = (*JPEGEncoder)(nil)

// NewJPEG returns a JPEG encoder with the given quality. Quality must be in
// [MinQuality, MaxQuality]; anything else is rejected rather than clamped.
func NewJPEG(quality int) (*JPEGEncoder, error) {
	if quality < MinQuality || quality > MaxQuality {
		return nil, fmt.Errorf("encoder: JPEG quality %d out of range [%d, %d]", quality, MinQuality, MaxQuality)
	}
	return &JPEGEncoder{quality: quality}, nil
}

// Quality returns the configured JPEG quality.
func (e *JPEGEncoder) Quality() int {
	return e.quality
}

// Encode converts one BGRA frame into a JPEG. The input frame is read-only and
// is not modified. Every call returns a fresh encoded buffer.
func (e *JPEGEncoder) Encode(frame *capture.Frame) (*EncodedFrame, error) {
	if err := frame.Validate(); err != nil {
		return nil, fmt.Errorf("encoder: invalid frame: %w", err)
	}
	if frame.Format != capture.FormatBGRA {
		return nil, fmt.Errorf("encoder: unsupported pixel format %v", frame.Format)
	}

	img := bgraToRGBA(frame)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: e.quality}); err != nil {
		return nil, fmt.Errorf("encoder: jpeg encode: %w", err)
	}

	return &EncodedFrame{
		Format: FormatJPEG,
		Width:  frame.Width,
		Height: frame.Height,
		Data:   buf.Bytes(),
	}, nil
}

// bgraToRGBA copies a top-down BGRA frame into a tightly packed RGBA image.
// It reads each source row using the frame's stride (which may be larger than
// width*4) and swaps the blue and red channels. JPEG has no alpha, so alpha is
// set to opaque.
func bgraToRGBA(frame *capture.Frame) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, frame.Width, frame.Height))
	rowBytes := frame.Width * 4

	for y := 0; y < frame.Height; y++ {
		src := frame.Pixels[y*frame.Stride : y*frame.Stride+rowBytes]
		dstRow := dst.Pix[y*dst.Stride : y*dst.Stride+rowBytes]
		for x := 0; x < frame.Width; x++ {
			srcPix := src[x*4:]
			dstPix := dstRow[x*4:]
			dstPix[0] = srcPix[2] // R
			dstPix[1] = srcPix[1] // G
			dstPix[2] = srcPix[0] // B
			dstPix[3] = 0xff      // A
		}
	}
	return dst
}

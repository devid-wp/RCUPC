// Package capture provides a minimal abstraction for obtaining raw desktop
// frames.
//
// It knows nothing about TCP, sessions, the RCUPC protocol, clients, servers
// or the logging implementation. A backend captures pixels; callers decide
// what to do with them.
//
// Lifecycle: Start, then one or more Capture calls, then Close. The windows
// backend captures the primary desktop only.
package capture

import (
	"errors"
	"fmt"
)

// ErrUnsupported is reported by New on platforms without a capture backend.
var ErrUnsupported = errors.New("desktop capture is unsupported on this platform")

// PixelFormat describes the layout of Frame.Pixels.
type PixelFormat uint8

const (
	// FormatBGRA is 32-bit blue-green-red-alpha, one row after another.
	// GDI's alpha byte is not meaningful and is typically zero.
	FormatBGRA PixelFormat = iota + 1
)

// BytesPerPixel returns the number of bytes per pixel, or 0 if unknown.
func (p PixelFormat) BytesPerPixel() int {
	switch p {
	case FormatBGRA:
		return 4
	default:
		return 0
	}
}

func (p PixelFormat) String() string {
	switch p {
	case FormatBGRA:
		return "BGRA"
	default:
		return fmt.Sprintf("PixelFormat(%d)", uint8(p))
	}
}

// Frame is one raw desktop frame. Pixels is top-down and owned by the caller.
type Frame struct {
	Width  int
	Height int
	Stride int
	Format PixelFormat
	Pixels []byte
}

// Validate reports whether the frame metadata is internally consistent.
func (f *Frame) Validate() error {
	if f == nil {
		return errors.New("capture: nil frame")
	}
	if f.Width <= 0 || f.Height <= 0 {
		return fmt.Errorf("capture: invalid frame size %dx%d", f.Width, f.Height)
	}
	bpp := f.Format.BytesPerPixel()
	if bpp == 0 {
		return fmt.Errorf("capture: unknown pixel format %v", f.Format)
	}
	if f.Stride < f.Width*bpp {
		return fmt.Errorf("capture: stride %d too small for width %d and format %v", f.Stride, f.Width, f.Format)
	}
	if want := f.Stride * f.Height; len(f.Pixels) != want {
		return fmt.Errorf("capture: pixel buffer is %d bytes, want %d", len(f.Pixels), want)
	}
	return nil
}

// Source is a desktop capture source.
//
// Start initializes the backend, Capture returns one frame, and Close releases
// all resources. Close is idempotent. Callers must serialize access.
type Source interface {
	Start() error
	Capture() (*Frame, error)
	Close() error
}

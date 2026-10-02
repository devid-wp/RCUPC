//go:build windows

// Windows backdrop for the capture package.
//
// This backend uses GDI through the standard library's syscall package, so no
// third-party dependency is required. It captures the primary desktop with
// BitBlt and returns a top-down BGRA frame.
package capture

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

var (
	user32 = syscall.NewLazyDLL("user32.dll")
	gdi32  = syscall.NewLazyDLL("gdi32.dll")

	procGetDC            = user32.NewProc("GetDC")
	procReleaseDC        = user32.NewProc("ReleaseDC")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")

	procCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject           = gdi32.NewProc("SelectObject")
	procBitBlt                 = gdi32.NewProc("BitBlt")
	procGetDIBits              = gdi32.NewProc("GetDIBits")
	procDeleteObject           = gdi32.NewProc("DeleteObject")
	procDeleteDC               = gdi32.NewProc("DeleteDC")
)

const (
	smCXScreen = 0 // primary monitor width
	smCYScreen = 1 // primary monitor height

	srcCopy    = 0x00CC0020
	captureBLT = 0x40000000 // include layered windows

	dibRGBColors = 0
	biRGB        = 0
)

// bitmapInfoHeader mirrors the Win32 BITMAPINFOHEADER. GetDIBits fills the
// pixel buffer according to this description.
type bitmapInfoHeader struct {
	size          uint32
	width         int32
	height        int32
	planes        uint16
	bitCount      uint16
	compression   uint32
	sizeImage     uint32
	xPelsPerMeter int32
	yPelsPerMeter int32
	clrUsed       uint32
	clrImportant  uint32
}

type rgbQuad struct {
	blue     byte
	green    byte
	red      byte
	reserved byte
}

type bitmapInfo struct {
	header bitmapInfoHeader
	colors [1]rgbQuad
}

// gdiSource owns a screen DC, a memory DC and a compatible bitmap. All three
// are created in Start and released in Close.
type gdiSource struct {
	mu sync.Mutex

	width  int
	height int
	stride int

	screenDC uintptr
	memDC    uintptr
	bitmap   uintptr

	started bool
}

// New returns a GDI-backed capture source for the primary desktop.
func New() (Source, error) {
	return &gdiSource{}, nil
}

var _ Source = (*gdiSource)(nil)

func (s *gdiSource) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return nil
	}

	w, _, _ := procGetSystemMetrics.Call(uintptr(smCXScreen))
	h, _, _ := procGetSystemMetrics.Call(uintptr(smCYScreen))
	width, height := int(int32(w)), int(int32(h))
	if width <= 0 || height <= 0 {
		return fmt.Errorf("capture: invalid primary desktop size %dx%d", width, height)
	}

	screenDC, _, callErr := procGetDC.Call(0)
	if screenDC == 0 {
		return fmt.Errorf("capture: GetDC failed: %v", callErr)
	}

	memDC, _, callErr := procCreateCompatibleDC.Call(screenDC)
	if memDC == 0 {
		procReleaseDC.Call(0, screenDC)
		return fmt.Errorf("capture: CreateCompatibleDC failed: %v", callErr)
	}

	bitmap, _, callErr := procCreateCompatibleBitmap.Call(screenDC, uintptr(width), uintptr(height))
	if bitmap == 0 {
		procDeleteDC.Call(memDC)
		procReleaseDC.Call(0, screenDC)
		return fmt.Errorf("capture: CreateCompatibleBitmap failed: %v", callErr)
	}

	s.width, s.height = width, height
	s.stride = width * 4
	s.screenDC, s.memDC, s.bitmap = screenDC, memDC, bitmap
	s.started = true
	return nil
}

func (s *gdiSource) Capture() (*Frame, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return nil, fmt.Errorf("capture: not started")
	}

	// BitBlt needs the bitmap selected into the memory DC, but GetDIBits
	// requires it to be deselected. Select around the copy only.
	old, _, callErr := procSelectObject.Call(s.memDC, s.bitmap)
	if old == 0 {
		return nil, fmt.Errorf("capture: SelectObject failed: %v", callErr)
	}

	ret, _, callErr := procBitBlt.Call(
		s.memDC, 0, 0, uintptr(s.width), uintptr(s.height),
		s.screenDC, 0, 0, uintptr(srcCopy|captureBLT),
	)
	procSelectObject.Call(s.memDC, old) // always restore
	if ret == 0 {
		return nil, fmt.Errorf("capture: BitBlt failed: %v", callErr)
	}

	buf := make([]byte, s.stride*s.height)
	info := bitmapInfo{
		header: bitmapInfoHeader{
			size:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
			width:       int32(s.width),
			height:      int32(s.height), // positive: rows are bottom-up
			planes:      1,
			bitCount:    32,
			compression: biRGB,
		},
	}

	bufPtr := unsafe.Pointer(&buf[0])
	infoPtr := unsafe.Pointer(&info)
	lines, _, callErr := procGetDIBits.Call(
		s.memDC,
		s.bitmap,
		0,
		uintptr(s.height),
		uintptr(bufPtr),
		uintptr(infoPtr),
		uintptr(dibRGBColors),
	)
	runtime.KeepAlive(buf)
	runtime.KeepAlive(info)

	if lines == 0 {
		return nil, fmt.Errorf("capture: GetDIBits failed: %v", callErr)
	}
	if int(lines) != s.height {
		return nil, fmt.Errorf("capture: GetDIBits copied %d of %d rows", int(lines), s.height)
	}

	flipVertical(buf, s.stride, s.height)

	return &Frame{
		Width:  s.width,
		Height: s.height,
		Stride: s.stride,
		Format: FormatBGRA,
		Pixels: buf,
	}, nil
}

func (s *gdiSource) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return nil
	}
	if s.bitmap != 0 {
		procDeleteObject.Call(s.bitmap)
	}
	if s.memDC != 0 {
		procDeleteDC.Call(s.memDC)
	}
	if s.screenDC != 0 {
		procReleaseDC.Call(0, s.screenDC)
	}

	s.screenDC, s.memDC, s.bitmap = 0, 0, 0
	s.width, s.height, s.stride = 0, 0, 0
	s.started = false
	return nil
}

// flipVertical turns GDI's bottom-up rows into the top-down layout promised by
// Frame.
func flipVertical(buf []byte, stride, height int) {
	row := make([]byte, stride)
	for y := 0; y < height/2; y++ {
		top := buf[y*stride : y*stride+stride]
		bottom := buf[(height-1-y)*stride : (height-y)*stride]
		copy(row, top)
		copy(top, bottom)
		copy(bottom, row)
	}
}

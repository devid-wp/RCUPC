//go:build windows

package capture

import "testing"

// TestCaptureOneFrame initializes GDI capture, grabs one real frame and
// releases the resources. It requires an interactive desktop session; run it
// with:
//
//	go test -run TestCaptureOneFrame -v ./pkg/capture
func TestCaptureOneFrame(t *testing.T) {
	src, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := src.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Close is idempotent, so a deferred cleanup plus the explicit close below
	// are both safe.
	defer func() { _ = src.Close() }()

	frame, err := src.Capture()
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if err := frame.Validate(); err != nil {
		t.Fatalf("frame invalid: %v", err)
	}
	if frame.Width <= 0 || frame.Height <= 0 {
		t.Fatalf("dimensions = %dx%d, want positive", frame.Width, frame.Height)
	}
	if len(frame.Pixels) == 0 {
		t.Fatal("pixel buffer is empty")
	}
	if !hasNonZero(frame.Pixels) {
		t.Fatal("pixel buffer is all zero; capture returned placeholder data")
	}

	t.Logf("captured %dx%d %s stride=%d bytes=%d",
		frame.Width, frame.Height, frame.Format, frame.Stride, len(frame.Pixels))

	if err := src.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := src.Capture(); err == nil {
		t.Fatal("Capture after Close succeeded, want error")
	}
}

func hasNonZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return true
		}
	}
	return false
}

package capture

import "testing"

func TestPixelFormat(t *testing.T) {
	if got := FormatBGRA.BytesPerPixel(); got != 4 {
		t.Errorf("FormatBGRA.BytesPerPixel() = %d, want 4", got)
	}
	if got := FormatBGRA.String(); got != "BGRA" {
		t.Errorf("FormatBGRA.String() = %q, want BGRA", got)
	}
	if got := PixelFormat(0).BytesPerPixel(); got != 0 {
		t.Errorf("unknown BytesPerPixel() = %d, want 0", got)
	}
}

func TestFrameValidate(t *testing.T) {
	tests := []struct {
		name    string
		frame   *Frame
		wantErr bool
	}{
		{"nil", nil, true},
		{"valid", &Frame{Width: 2, Height: 2, Stride: 8, Format: FormatBGRA, Pixels: make([]byte, 16)}, false},
		{"zero width", &Frame{Width: 0, Height: 2, Stride: 8, Format: FormatBGRA, Pixels: make([]byte, 16)}, true},
		{"negative height", &Frame{Width: 2, Height: -1, Stride: 8, Format: FormatBGRA, Pixels: make([]byte, 16)}, true},
		{"unknown format", &Frame{Width: 2, Height: 2, Stride: 8, Format: 0, Pixels: make([]byte, 16)}, true},
		{"short stride", &Frame{Width: 2, Height: 2, Stride: 4, Format: FormatBGRA, Pixels: make([]byte, 8)}, true},
		{"wrong buffer size", &Frame{Width: 2, Height: 2, Stride: 8, Format: FormatBGRA, Pixels: make([]byte, 15)}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.frame.Validate()
			if tt.wantErr && err == nil {
				t.Fatal("Validate() = nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
		})
	}
}

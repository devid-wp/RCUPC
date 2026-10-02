//go:build !windows

package capture

import (
	"errors"
	"testing"
)

func TestNewUnsupported(t *testing.T) {
	src, err := New()
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("New() error = %v, want ErrUnsupported", err)
	}
	if src != nil {
		t.Fatalf("New() source = %v, want nil on an unsupported platform", src)
	}
}

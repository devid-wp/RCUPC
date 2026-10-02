//go:build !windows

package capture

// New reports that desktop capture is not supported on this platform. It never
// returns a fake source: there is no capture backend outside Windows.
func New() (Source, error) {
	return nil, ErrUnsupported
}

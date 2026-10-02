package framepipe

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"rcupc/pkg/capture"
)

var errBoom = errors.New("boom")

// fakeSource is a deterministic capture.Source for tests.
type fakeSource struct {
	mu sync.Mutex

	startErr error
	closeErr error

	startCount   int
	closeCount   int
	captureCount int

	captureErr   error
	captureErrAt int // fail on this 1-based capture; 0 = never
	frame        func(n int) *capture.Frame
}

func (f *fakeSource) Start() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startCount++
	return f.startErr
}

func (f *fakeSource) Capture() (*capture.Frame, error) {
	f.mu.Lock()
	f.captureCount++
	n := f.captureCount
	errAt := f.captureErrAt
	captureErr := f.captureErr
	frameFn := f.frame
	f.mu.Unlock()

	if errAt != 0 && n == errAt {
		return nil, captureErr
	}
	if frameFn != nil {
		return frameFn(n), nil
	}
	return testFrame(n), nil
}

func (f *fakeSource) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closeCount++
	return f.closeErr
}

func (f *fakeSource) counts() (start, captured, closed int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.startCount, f.captureCount, f.closeCount
}

// recordingConsumer records frames and can cancel or fail on a chosen frame.
type recordingConsumer struct {
	mu sync.Mutex

	frames  []*capture.Frame
	err     error
	errAt   int // fail on this 1-based frame; 0 = never
	onFrame func(n int)
}

func (c *recordingConsumer) Consume(frame *capture.Frame) error {
	c.mu.Lock()
	c.frames = append(c.frames, frame)
	n := len(c.frames)
	errAt := c.errAt
	consumeErr := c.err
	onFrame := c.onFrame
	c.mu.Unlock()

	if onFrame != nil {
		onFrame(n)
	}
	if errAt != 0 && n == errAt {
		return consumeErr
	}
	return nil
}

func (c *recordingConsumer) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.frames)
}

func testFrame(n int) *capture.Frame {
	const width, height = 2, 2
	stride := width * 4
	pixels := make([]byte, stride*height)
	pixels[0] = byte(n) // deterministic content
	return &capture.Frame{
		Width:  width,
		Height: height,
		Stride: stride,
		Format: capture.FormatBGRA,
		Pixels: pixels,
	}
}

func TestNewValidation(t *testing.T) {
	src := &fakeSource{}
	cons := &recordingConsumer{}

	if _, err := New(nil, cons, time.Second); err == nil {
		t.Error("New(nil source) = nil error, want error")
	}
	if _, err := New(src, nil, time.Second); err == nil {
		t.Error("New(nil consumer) = nil error, want error")
	}
	if _, err := New(src, cons, 0); err == nil {
		t.Error("New(zero interval) = nil error, want error")
	}
	if _, err := New(src, cons, -time.Second); err == nil {
		t.Error("New(negative interval) = nil error, want error")
	}
}

func TestRunStartError(t *testing.T) {
	src := &fakeSource{startErr: errBoom}
	p, err := New(src, &recordingConsumer{}, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}

	if err := p.Run(context.Background()); !errors.Is(err, errBoom) {
		t.Fatalf("Run = %v, want %v", err, errBoom)
	}
	if start, captured, closed := src.counts(); start != 1 || captured != 0 || closed != 0 {
		t.Fatalf("start=%d captured=%d closed=%d, want 1/0/0", start, captured, closed)
	}
}

func TestRunCapturesMultipleFrames(t *testing.T) {
	src := &fakeSource{}
	ctx, cancel := context.WithCancel(context.Background())
	cons := &recordingConsumer{onFrame: func(n int) {
		if n == 3 {
			cancel()
		}
	}}

	p, err := New(src, cons, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}

	if err := p.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v, want context.Canceled", err)
	}

	if start, captured, closed := src.counts(); start != 1 || captured != 3 || closed != 1 {
		t.Fatalf("start=%d captured=%d closed=%d, want 1/3/1", start, captured, closed)
	}
	if got := cons.count(); got != 3 {
		t.Fatalf("consumed %d frames, want 3", got)
	}
}

func TestRunRespectsInterval(t *testing.T) {
	const interval = 20 * time.Millisecond

	src := &fakeSource{}
	ctx, cancel := context.WithCancel(context.Background())

	var mu sync.Mutex
	var times []time.Time
	cons := &recordingConsumer{onFrame: func(n int) {
		mu.Lock()
		times = append(times, time.Now())
		mu.Unlock()
		if n == 3 {
			cancel()
		}
	}}

	p, err := New(src, cons, interval)
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	if err := p.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v, want context.Canceled", err)
	}
	elapsed := time.Since(start)
	if elapsed < interval {
		t.Fatalf("Run returned after %s, want at least one interval (%s)", elapsed, interval)
	}

	mu.Lock()
	got := append([]time.Time(nil), times...)
	mu.Unlock()
	if len(got) != 3 {
		t.Fatalf("consumed %d frames, want 3", len(got))
	}
	for i := 1; i < len(got); i++ {
		if gap := got[i].Sub(got[i-1]); gap < interval/2 {
			t.Fatalf("gap %s between frame %d and %d, want >= %s", gap, i, i+1, interval/2)
		}
	}
}

func TestRunStopsWhenContextAlreadyCancelled(t *testing.T) {
	src := &fakeSource{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	p, err := New(src, &recordingConsumer{}, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}

	if err := p.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v, want context.Canceled", err)
	}
	if _, captured, closed := src.counts(); captured != 0 || closed != 1 {
		t.Fatalf("captured=%d closed=%d, want 0/1", captured, closed)
	}
}

func TestRunNoCapturesAfterCancel(t *testing.T) {
	src := &fakeSource{}
	ctx, cancel := context.WithCancel(context.Background())
	cons := &recordingConsumer{onFrame: func(int) { cancel() }}

	p, err := New(src, cons, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}

	if err := p.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v, want context.Canceled", err)
	}
	if _, captured, closed := src.counts(); captured != 1 || closed != 1 {
		t.Fatalf("captured=%d closed=%d, want 1/1", captured, closed)
	}
	if got := cons.count(); got != 1 {
		t.Fatalf("consumed %d frames, want 1", got)
	}
}

func TestRunCaptureErrorStops(t *testing.T) {
	src := &fakeSource{captureErrAt: 2, captureErr: errBoom}
	cons := &recordingConsumer{}

	p, err := New(src, cons, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}

	if err := p.Run(context.Background()); !errors.Is(err, errBoom) {
		t.Fatalf("Run = %v, want %v", err, errBoom)
	}
	if _, captured, closed := src.counts(); captured != 2 || closed != 1 {
		t.Fatalf("captured=%d closed=%d, want 2/1", captured, closed)
	}
	if got := cons.count(); got != 1 {
		t.Fatalf("consumed %d frames, want 1", got)
	}
}

func TestRunConsumerErrorStops(t *testing.T) {
	src := &fakeSource{}
	cons := &recordingConsumer{errAt: 1, err: errBoom}

	p, err := New(src, cons, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}

	if err := p.Run(context.Background()); !errors.Is(err, errBoom) {
		t.Fatalf("Run = %v, want %v", err, errBoom)
	}
	if _, captured, closed := src.counts(); captured != 1 || closed != 1 {
		t.Fatalf("captured=%d closed=%d, want 1/1", captured, closed)
	}
}

func TestRunRejectsNilFrame(t *testing.T) {
	src := &fakeSource{frame: func(int) *capture.Frame { return nil }}

	p, err := New(src, &recordingConsumer{}, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}

	if err := p.Run(context.Background()); err == nil {
		t.Fatal("Run = nil, want nil-frame error")
	}
	if _, captured, closed := src.counts(); captured != 1 || closed != 1 {
		t.Fatalf("captured=%d closed=%d, want 1/1", captured, closed)
	}
}

func TestRunRejectsInvalidFrame(t *testing.T) {
	src := &fakeSource{frame: func(int) *capture.Frame {
		return &capture.Frame{Width: 0, Height: 0, Format: capture.FormatBGRA}
	}}

	p, err := New(src, &recordingConsumer{}, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}

	if err := p.Run(context.Background()); err == nil {
		t.Fatal("Run = nil, want invalid-frame error")
	}
	if _, captured, closed := src.counts(); captured != 1 || closed != 1 {
		t.Fatalf("captured=%d closed=%d, want 1/1", captured, closed)
	}
}

func TestRunReturnsCloseError(t *testing.T) {
	src := &fakeSource{closeErr: errBoom}
	ctx, cancel := context.WithCancel(context.Background())
	cons := &recordingConsumer{onFrame: func(int) { cancel() }}

	p, err := New(src, cons, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}

	err = p.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run = %v, want context.Canceled", err)
	}
	if !errors.Is(err, errBoom) {
		t.Fatalf("Run = %v, want joined close error", err)
	}
}

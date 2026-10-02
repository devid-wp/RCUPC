// Package framepipe moves frames from a capture.Source to a consumer.
//
// It is intentionally small and independent: it knows nothing about TCP,
// sessions, the RCUPC protocol, encoders or GUIs. It may depend on
// pkg/capture because frames originate there.
//
// Ownership: the source creates a fresh frame for every Capture call and the
// pipeline passes that frame to the consumer. The pipeline never reuses or
// pools pixel buffers in this phase.
package framepipe

import (
	"context"
	"errors"
	"fmt"
	"time"

	"rcupc/pkg/capture"
)

// Consumer receives frames from a Pipeline. Consume is called synchronously
// from the pipeline goroutine.
//
// The frame and its pixel buffer are valid during the Consume call. The
// pipeline does not reuse or mutate them afterwards, but it also does not
// retain them; consumers must not keep a frame beyond returning unless that
// becomes an explicit contract later.
type Consumer interface {
	Consume(*capture.Frame) error
}

// Pipeline repeatedly captures frames from a Source and delivers them to a
// Consumer.
type Pipeline struct {
	src      capture.Source
	consumer Consumer
	interval time.Duration
}

// New returns a Pipeline that waits interval between frames. interval must be
// positive; the caller decides the value (no frame-rate setting is derived
// from it yet).
func New(src capture.Source, consumer Consumer, interval time.Duration) (*Pipeline, error) {
	if src == nil {
		return nil, errors.New("framepipe: nil capture source")
	}
	if consumer == nil {
		return nil, errors.New("framepipe: nil consumer")
	}
	if interval <= 0 {
		return nil, fmt.Errorf("framepipe: interval must be positive, got %s", interval)
	}
	return &Pipeline{src: src, consumer: consumer, interval: interval}, nil
}

// Run starts the source, captures frames until ctx is cancelled, then closes
// the source exactly once. It returns ctx.Err() on cancellation, a capture
// error if Capture fails, the consumer's error if Consume fails, or an invalid
// frame error. If closing the source fails, that error is joined with the
// primary result.
func (p *Pipeline) Run(ctx context.Context) error {
	if err := p.src.Start(); err != nil {
		return fmt.Errorf("framepipe: start capture: %w", err)
	}

	runErr := p.loop(ctx)

	if cerr := p.src.Close(); cerr != nil {
		return errors.Join(runErr, fmt.Errorf("framepipe: close capture: %w", cerr))
	}
	return runErr
}

func (p *Pipeline) loop(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		frame, err := p.src.Capture()
		if err != nil {
			return fmt.Errorf("framepipe: capture: %w", err)
		}
		if err := validateFrame(frame); err != nil {
			return err
		}
		// Do not deliver a frame captured after cancellation.
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := p.consumer.Consume(frame); err != nil {
			return fmt.Errorf("framepipe: consume: %w", err)
		}

		timer := time.NewTimer(p.interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func validateFrame(frame *capture.Frame) error {
	if frame == nil {
		return errors.New("framepipe: capture returned a nil frame")
	}
	if err := frame.Validate(); err != nil {
		return fmt.Errorf("framepipe: invalid frame: %w", err)
	}
	return nil
}

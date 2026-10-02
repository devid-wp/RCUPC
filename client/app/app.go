// Package app wires the client components for the Phase 1 CLI.
package app

import (
	"context"
	"log/slog"

	"rcupc/client/session"
)

// Run connects to addr, completes the handshake and keeps the session open.
func Run(ctx context.Context, addr string, log *slog.Logger) error {
	log.Info("connecting", "addr", addr)

	sess, err := session.Connect(ctx, addr, log)
	if err != nil {
		return err
	}
	log.Info("connection established", "addr", addr)

	if err := sess.Wait(ctx); err != nil {
		return err
	}
	log.Info("connection closed", "addr", addr)
	return nil
}

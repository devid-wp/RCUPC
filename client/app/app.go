// Package app wires the client components for the CLI.
package app

import (
	"context"
	"errors"

	"rcupc/client/session"
	"rcupc/pkg/config"
	"rcupc/pkg/logging"
	"rcupc/pkg/protocol"
)

// Run connects using cfg, completes the handshake, sends the sample commands
// of the message phase and keeps the session open. A cancelled context is a
// clean shutdown, not an error. cfg is read-only.
func Run(ctx context.Context, cfg config.ClientConfig, log logging.Logger) error {
	log.Info("connecting",
		"addr", cfg.HostAddr,
		"handshake_timeout", cfg.HandshakeTimeout,
		"max_line_len", cfg.MaxLineLen,
	)

	sess, err := session.Connect(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer func() { _ = sess.Close() }()
	log.Info("connected", "addr", cfg.HostAddr)

	for _, cmd := range []protocol.Command{protocol.Info, protocol.Ping} {
		log.Info("command sent", "command", string(cmd))
		resp, err := sess.Send(cmd)
		if err != nil {
			return err
		}
		log.Info("response received", "command", string(cmd), "response", resp)
	}

	err = sess.Wait(ctx)
	if err == nil {
		log.Info("connection closed", "addr", cfg.HostAddr)
		return nil
	}
	if errors.Is(err, context.Canceled) {
		log.Info("shutdown", "addr", cfg.HostAddr)
		return nil
	}
	return err
}

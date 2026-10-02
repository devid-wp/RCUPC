// Package server accepts incoming RCUPC connections and hands each one to a
// session.
package server

import (
	"context"
	"sync"

	"rcupc/host/session"
	"rcupc/pkg/config"
	"rcupc/pkg/logging"
	"rcupc/pkg/transport"
)

// Server listens for RCUPC clients using a validated host configuration.
type Server struct {
	cfg config.HostConfig
	log logging.Logger
}

// New returns a Server that listens on cfg.ListenAddr. cfg must be validated.
func New(cfg config.HostConfig, log logging.Logger) *Server {
	return &Server{cfg: cfg, log: log}
}

// ListenAndServe blocks until ctx is cancelled or the listener fails. On
// cancellation it stops accepting, closes the listener, waits for active
// sessions to finish and returns nil.
func (s *Server) ListenAndServe(ctx context.Context) error {
	ln, err := transport.Listen(ctx, s.cfg.ListenAddr)
	if err != nil {
		return err
	}
	defer ln.Close()

	return s.serve(ctx, ln)
}

// serve runs the accept loop on ln until ctx is cancelled. It is separate from
// ListenAndServe so tests can supply a listener on an ephemeral port.
func (s *Server) serve(ctx context.Context, ln *transport.Listener) error {
	s.log.Info("listening",
		"addr", ln.Addr(),
		"handshake_timeout", s.cfg.HandshakeTimeout,
		"max_line_len", s.cfg.MaxLineLen,
	)

	// Closing the listener unblocks Accept when ctx is cancelled.
	closerDone := make(chan struct{})
	go func() {
		defer close(closerDone)
		<-ctx.Done()
		ln.Close()
	}()

	var sessions sync.WaitGroup
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				<-closerDone
				sessions.Wait()
				s.log.Info("server shutdown")
				return nil
			}
			s.log.Error("accept failed", "err", err)
			continue
		}
		s.log.Info("client connected", "remote", conn.RemoteAddr())
		sessions.Add(1)
		go s.handle(ctx, conn, &sessions)
	}
}

func (s *Server) handle(ctx context.Context, conn transport.Conn, sessions *sync.WaitGroup) {
	defer sessions.Done()

	remote := conn.RemoteAddr()
	// The session logs any specific failure (invalid handshake, oversized
	// message, ...); the server records the lifecycle event only.
	_ = session.Run(ctx, conn, s.cfg, s.log)
	s.log.Info("session closed", "remote", remote)
}

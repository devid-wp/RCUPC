// Package server accepts incoming RCUPC connections and hands each one to a
// session.
package server

import (
	"context"
	"log/slog"
	"net"

	"rcupc/host/session"
)

// Server listens for RCUPC clients on a TCP address.
type Server struct {
	addr string
	log  *slog.Logger
}

// New returns a Server that will listen on addr.
func New(addr string, log *slog.Logger) *Server {
	return &Server{addr: addr, log: log}
}

// ListenAndServe blocks until ctx is cancelled or the listener fails.
func (s *Server) ListenAndServe(ctx context.Context) error {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", s.addr)
	if err != nil {
		return err
	}
	defer ln.Close()

	s.log.Info("listening", "addr", ln.Addr().String())

	// Closing the listener unblocks Accept when ctx is cancelled.
	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			s.log.Error("accept failed", "err", err)
			continue
		}
		s.log.Info("client connected", "remote", conn.RemoteAddr().String())
		go s.handle(ctx, conn)
	}
}

func (s *Server) handle(ctx context.Context, conn net.Conn) {
	if err := session.Run(ctx, conn, s.log); err != nil {
		s.log.Warn("session ended with error", "remote", conn.RemoteAddr().String(), "err", err)
		return
	}
	s.log.Info("session closed", "remote", conn.RemoteAddr().String())
}

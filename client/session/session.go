// Package session implements the client side of a single RCUPC connection.
package session

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"rcupc/pkg/config"
	"rcupc/pkg/logging"
	"rcupc/pkg/protocol"
	"rcupc/pkg/transport"
)

// Session is an established connection to a host.
type Session struct {
	conn       transport.Conn
	r          *bufio.Reader
	log        logging.Logger
	maxLineLen int
}

// Connect dials cfg.HostAddr through the transport boundary and performs the
// handshake using cfg.HandshakeTimeout and cfg.MaxLineLen. cfg is read-only.
func Connect(ctx context.Context, cfg config.ClientConfig, log logging.Logger) (*Session, error) {
	conn, err := transport.Dial(ctx, cfg.HostAddr)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", cfg.HostAddr, err)
	}

	if err := conn.SetDeadline(time.Now().Add(cfg.HandshakeTimeout)); err != nil {
		conn.Close()
		return nil, err
	}
	if err := protocol.WriteLine(conn, protocol.Hello); err != nil {
		conn.Close()
		return nil, fmt.Errorf("write handshake: %w", err)
	}

	r := bufio.NewReader(conn)
	line, err := protocol.ReadLineLimit(r, cfg.MaxLineLen)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("read handshake response: %w", err)
	}
	msg, reason, err := protocol.Parse(line)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if msg != protocol.OK {
		conn.Close()
		if msg == protocol.Error {
			return nil, fmt.Errorf("host rejected handshake: %s", reason)
		}
		return nil, fmt.Errorf("unexpected handshake response %q", msg)
	}

	// A failed clear means the peer closed immediately after OK; not fatal.
	_ = conn.SetDeadline(time.Time{})
	log.Info("handshake complete", "addr", cfg.HostAddr, "protocol", protocol.Version)
	return &Session{conn: conn, r: r, log: log, maxLineLen: cfg.MaxLineLen}, nil
}

// Send writes one command, reads the host response and validates it. The
// session stays open and can be reused for further commands.
func (s *Session) Send(cmd protocol.Command) (string, error) {
	if err := protocol.WriteMessage(s.conn, string(cmd)); err != nil {
		return "", fmt.Errorf("write %s: %w", cmd, err)
	}

	resp, err := protocol.ReadMessageLimit(s.r, s.maxLineLen)
	if err != nil {
		return "", fmt.Errorf("read response to %s: %w", cmd, err)
	}
	if err := protocol.Validate(cmd, resp); err != nil {
		return resp, err
	}
	return resp, nil
}

// Close closes the underlying connection.
func (s *Session) Close() error {
	return s.conn.Close()
}

// Wait keeps the session open until the host closes it or ctx is cancelled. On
// cancellation it closes the connection (unblocking the read) and returns
// ctx.Err(); a host-initiated close returns nil.
func (s *Session) Wait(ctx context.Context) error {
	stop := make(chan struct{})
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		select {
		case <-ctx.Done():
			s.conn.Close()
		case <-stop:
		}
	}()
	defer func() {
		close(stop)
		<-watcherDone
	}()

	buf := make([]byte, 256)
	for {
		n, err := s.r.Read(buf)
		if n > 0 {
			s.log.Debug("unexpected data", "bytes", n)
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

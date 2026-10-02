// Package session implements the client side of a single RCUPC connection.
package session

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"time"

	"rcupc/pkg/protocol"
)

const handshakeTimeout = 5 * time.Second

// Session is an established connection to a host.
type Session struct {
	conn net.Conn
	r    *bufio.Reader
	log  *slog.Logger
}

// Connect dials addr and performs the Phase 1 handshake.
func Connect(ctx context.Context, addr string, log *slog.Logger) (*Session, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}

	if err := conn.SetDeadline(time.Now().Add(handshakeTimeout)); err != nil {
		conn.Close()
		return nil, err
	}
	if err := protocol.WriteLine(conn, protocol.Hello); err != nil {
		conn.Close()
		return nil, fmt.Errorf("write handshake: %w", err)
	}

	r := bufio.NewReader(conn)
	line, err := protocol.ReadLine(r)
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
	return &Session{conn: conn, r: r, log: log}, nil
}

// Wait keeps the session open until the host closes it or ctx is cancelled.
func (s *Session) Wait(ctx context.Context) error {
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			s.conn.Close()
		case <-stop:
		}
	}()

	buf := make([]byte, 256)
	for {
		n, err := s.r.Read(buf)
		if n > 0 {
			s.log.Info("ignoring data (unused in Phase 1)", "bytes", n)
		}
		if err != nil {
			if errors.Is(err, io.EOF) || ctx.Err() != nil {
				return nil
			}
			return err
		}
	}
}

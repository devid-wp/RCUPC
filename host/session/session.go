// Package session implements the host side of a single RCUPC connection.
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

// handshakeTimeout bounds how long a client may take to complete the handshake.
const handshakeTimeout = 5 * time.Second

// Run performs the Phase 1 handshake and then keeps the connection open until
// the client disconnects or ctx is cancelled.
func Run(ctx context.Context, conn net.Conn, log *slog.Logger) error {
	defer conn.Close()

	remote := conn.RemoteAddr()
	if err := conn.SetDeadline(time.Now().Add(handshakeTimeout)); err != nil {
		return err
	}

	r := bufio.NewReader(conn)
	msg, err := readHandshake(r)
	if err != nil {
		reject(conn, log, remote, err.Error())
		return err
	}
	if msg != protocol.Hello {
		err = fmt.Errorf("expected %s, got %s", protocol.Hello, msg)
		reject(conn, log, remote, err.Error())
		return err
	}

	if err := protocol.WriteLine(conn, protocol.OK); err != nil {
		return fmt.Errorf("write handshake response: %w", err)
	}
	// The client may close as soon as it reads OK; a failed clear is not fatal.
	_ = conn.SetDeadline(time.Time{})

	log.Info("handshake complete", "remote", remote, "protocol", protocol.Version)
	return keepOpen(ctx, r, conn, log)
}

func readHandshake(r *bufio.Reader) (protocol.Message, error) {
	line, err := protocol.ReadLine(r)
	if err != nil {
		return "", err
	}
	msg, _, err := protocol.Parse(line)
	return msg, err
}

func reject(conn net.Conn, log *slog.Logger, remote net.Addr, reason string) {
	_ = protocol.WriteError(conn, reason)
	log.Warn("handshake rejected", "remote", remote, "reason", reason)
}

// keepOpen reads from the connection until it is closed, so the host can
// detect and log client disconnects.
func keepOpen(ctx context.Context, r *bufio.Reader, conn net.Conn, log *slog.Logger) error {
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-stop:
		}
	}()

	buf := make([]byte, 256)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			log.Info("ignoring data (unused in Phase 1)", "bytes", n)
		}
		if err != nil {
			if errors.Is(err, io.EOF) || ctx.Err() != nil {
				return nil
			}
			return err
		}
	}
}

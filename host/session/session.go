// Package session implements the host side of a single RCUPC connection.
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

// Run performs the handshake and then serves client commands until the client
// disconnects or ctx is cancelled. Cancellation closes the connection, which
// unblocks any pending read, and Run returns nil. cfg is read-only.
func Run(ctx context.Context, conn transport.Conn, cfg config.HostConfig, log logging.Logger) error {
	defer conn.Close()

	// Closing the connection on cancellation unblocks any blocking Read. The
	// watcher covers the whole session (handshake and command loop) and is
	// joined before Run returns.
	stop := make(chan struct{})
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		select {
		case <-ctx.Done():
			conn.Close()
		case <-stop:
		}
	}()
	defer func() {
		close(stop)
		<-watcherDone
	}()

	remote := conn.RemoteAddr()
	if err := conn.SetDeadline(time.Now().Add(cfg.HandshakeTimeout)); err != nil {
		log.Error("handshake deadline failed", "remote", remote, "err", err)
		return err
	}

	r := bufio.NewReader(conn)
	msg, err := readHandshake(r, cfg.MaxLineLen)
	if err != nil {
		reject(conn, log, err.Error())
		return err
	}
	if msg != protocol.Hello {
		err = fmt.Errorf("expected %s, got %s", protocol.Hello, msg)
		reject(conn, log, err.Error())
		return err
	}

	if err := protocol.WriteLine(conn, protocol.OK); err != nil {
		log.Error("handshake response failed", "remote", remote, "err", err)
		return fmt.Errorf("write handshake response: %w", err)
	}
	// The client may close as soon as it reads OK; a failed clear is not fatal.
	_ = conn.SetDeadline(time.Time{})

	log.Info("handshake complete", "remote", remote, "protocol", protocol.Version)
	return serve(ctx, r, conn, cfg.MaxLineLen, log)
}

func readHandshake(r *bufio.Reader, maxLineLen int) (protocol.Message, error) {
	line, err := protocol.ReadLineLimit(r, maxLineLen)
	if err != nil {
		return "", err
	}
	msg, _, err := protocol.Parse(line)
	return msg, err
}

func reject(conn transport.Conn, log logging.Logger, reason string) {
	_ = protocol.WriteError(conn, reason)
	log.Warn("invalid handshake", "remote", conn.RemoteAddr(), "reason", reason)
}

// serve reads one command per line and writes the matching response, keeping
// the connection alive between commands.
func serve(ctx context.Context, r *bufio.Reader, conn transport.Conn, maxLineLen int, log logging.Logger) error {
	for {
		line, err := protocol.ReadMessageLimit(r, maxLineLen)
		if err != nil {
			if errors.Is(err, protocol.ErrLineTooLong) {
				// The rest of the oversized line cannot be resynced safely,
				// so report the error and end the session.
				_ = protocol.WriteMessage(conn, protocol.LineTooLong)
				log.Warn("oversized message", "remote", conn.RemoteAddr())
				return err
			}
			if errors.Is(err, io.EOF) || ctx.Err() != nil {
				return nil
			}
			log.Error("read failed", "remote", conn.RemoteAddr(), "err", err)
			return err
		}

		resp := protocol.Reply(protocol.Command(line))
		if resp == protocol.UnknownCommand {
			log.Warn("invalid command", "remote", conn.RemoteAddr(), "command", line)
		} else {
			log.Info("command handled", "remote", conn.RemoteAddr(), "command", line, "response", resp)
		}
		if err := protocol.WriteMessage(conn, resp); err != nil {
			log.Error("write failed", "remote", conn.RemoteAddr(), "err", err)
			return fmt.Errorf("write response: %w", err)
		}
	}
}

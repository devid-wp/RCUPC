package session

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	hostsession "rcupc/host/session"
	"rcupc/pkg/config"
	"rcupc/pkg/logging"
	"rcupc/pkg/protocol"
)

func newLogger() logging.Logger {
	return logging.New(io.Discard, logging.LevelInfo)
}

func clientConfig(addr string) config.ClientConfig {
	cfg := config.DefaultClient()
	cfg.HostAddr = addr
	return cfg
}

// startHost runs host sessions on a loopback listener and returns the address
// the client should dial. It accepts until the listener is closed.
func startHost(t *testing.T, ctx context.Context) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { _ = hostsession.Run(ctx, conn, config.DefaultHost(), newLogger()) }()
		}
	}()

	return ln.Addr().String()
}

func TestConnectOK(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sess, err := Connect(ctx, clientConfig(startHost(t, ctx)), newLogger())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// The host keeps the connection open, so Wait must block until cancelled.
	waitErr := make(chan error, 1)
	go func() { waitErr <- sess.Wait(ctx) }()

	select {
	case err := <-waitErr:
		t.Fatalf("Wait returned early: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	cancel()
	select {
	case err := <-waitErr:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Wait after cancel = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Wait did not return after cancel")
	}
}

func TestConnectRejected(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Consume HELLO, then reject with a reason.
		_, _ = bufio.NewReader(conn).ReadString('\n')
		_ = protocol.WriteError(conn, "version mismatch")
	}()

	_, err = Connect(context.Background(), clientConfig(ln.Addr().String()), newLogger())
	if err == nil || !strings.Contains(err.Error(), "version mismatch") {
		t.Fatalf("Connect error = %v, want rejection reason", err)
	}
}

func TestSend(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sess, err := Connect(ctx, clientConfig(startHost(t, ctx)), newLogger())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = sess.Close() }()

	resp, err := sess.Send(protocol.Info)
	if err != nil {
		t.Fatalf("Send INFO: %v", err)
	}
	if resp != protocol.HostInfo {
		t.Fatalf("INFO -> %q, want %q", resp, protocol.HostInfo)
	}

	// A second command must reuse the same connection.
	resp, err = sess.Send(protocol.Ping)
	if err != nil {
		t.Fatalf("Send PING: %v", err)
	}
	if resp != protocol.Pong {
		t.Fatalf("PING -> %q, want %q", resp, protocol.Pong)
	}
}

func TestSendUnknownCommand(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sess, err := Connect(ctx, clientConfig(startHost(t, ctx)), newLogger())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = sess.Close() }()

	resp, err := sess.Send(protocol.Command("BOGUS"))
	if err == nil {
		t.Fatalf("Send BOGUS = (%q, nil), want error", resp)
	}
	if resp != protocol.UnknownCommand {
		t.Fatalf("BOGUS raw response = %q, want %q", resp, protocol.UnknownCommand)
	}
}

func TestWaitHostClose(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Complete the handshake, then close the connection.
		_, _ = bufio.NewReader(conn).ReadString('\n')
		_ = protocol.WriteLine(conn, protocol.OK)
	}()

	sess, err := Connect(context.Background(), clientConfig(ln.Addr().String()), newLogger())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	if err := sess.Wait(context.Background()); err != nil {
		t.Fatalf("Wait after host close = %v, want nil", err)
	}
}

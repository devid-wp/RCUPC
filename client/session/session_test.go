package session

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	hostsession "rcupc/host/session"
	"rcupc/pkg/protocol"
)

func newLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// startHost runs a Phase 1 host session on a loopback listener and returns the
// address the client should dial.
func startHost(t *testing.T, ctx context.Context) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		_ = hostsession.Run(ctx, conn, newLogger())
	}()

	return ln.Addr().String()
}

func TestConnectOK(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sess, err := Connect(ctx, startHost(t, ctx), newLogger())
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
		if err != nil {
			t.Fatalf("Wait after cancel: %v", err)
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

	_, err = Connect(context.Background(), ln.Addr().String(), newLogger())
	if err == nil || !strings.Contains(err.Error(), "version mismatch") {
		t.Fatalf("Connect error = %v, want rejection reason", err)
	}
}

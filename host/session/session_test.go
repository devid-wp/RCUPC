package session

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"rcupc/pkg/protocol"
)

func newLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRunHandshakeOK(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- Run(ctx, server, newLogger()) }()

	if err := protocol.WriteLine(client, protocol.Hello); err != nil {
		t.Fatalf("write HELLO: %v", err)
	}

	line, err := protocol.ReadLine(bufio.NewReader(client))
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	msg, _, err := protocol.Parse(line)
	if err != nil || msg != protocol.OK {
		t.Fatalf("response = %q (%v), want OK", line, err)
	}

	client.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return after client close")
	}
}

func TestRunRejectsWrongVersion(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- Run(ctx, server, newLogger()) }()

	if _, err := io.WriteString(client, "OTHER/9.9 HELLO\n"); err != nil {
		t.Fatalf("write bad handshake: %v", err)
	}

	line, err := protocol.ReadLine(bufio.NewReader(client))
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	msg, reason, err := protocol.Parse(line)
	if err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if msg != protocol.Error || reason == "" {
		t.Fatalf("response = (%q, %q), want ERROR with reason", msg, reason)
	}

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Run returned nil for rejected handshake")
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return after rejection")
	}
}

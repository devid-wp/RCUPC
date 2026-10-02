package transport

import (
	"context"
	"testing"
)

func mustListen(t *testing.T) *Listener {
	t.Helper()

	ln, err := Listen(context.Background(), "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

func TestListenAcceptDialExchange(t *testing.T) {
	ln := mustListen(t)

	serverErr := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.Close()

		buf := make([]byte, 64)
		n, err := conn.Read(buf)
		if err != nil {
			serverErr <- err
			return
		}
		_, err = conn.Write(buf[:n])
		serverErr <- err
	}()

	client, err := Dial(context.Background(), ln.Addr())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	if _, err := client.Write([]byte("hello")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	buf := make([]byte, 64)
	n, err := client.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got := string(buf[:n]); got != "hello" {
		t.Fatalf("echo = %q, want %q", got, "hello")
	}

	if err := <-serverErr; err != nil {
		t.Fatalf("server side: %v", err)
	}
}

func TestConnRemoteAddr(t *testing.T) {
	ln := mustListen(t)

	go func() {
		if conn, err := ln.Accept(); err == nil {
			_ = conn.Close()
		}
	}()

	conn, err := Dial(context.Background(), ln.Addr())
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	if addr := conn.RemoteAddr(); addr == nil || addr.String() == "" {
		t.Fatalf("RemoteAddr = %v, want non-empty", addr)
	}
}

func TestClosingOneConnectionKeepsListener(t *testing.T) {
	ln := mustListen(t)

	first, err := Dial(context.Background(), ln.Addr())
	if err != nil {
		t.Fatalf("Dial first: %v", err)
	}
	firstServer, err := ln.Accept()
	if err != nil {
		t.Fatalf("Accept first: %v", err)
	}
	_ = firstServer.Close()
	_ = first.Close()

	// The listener must still accept a second connection.
	second, err := Dial(context.Background(), ln.Addr())
	if err != nil {
		t.Fatalf("Dial second: %v", err)
	}
	defer second.Close()

	secondServer, err := ln.Accept()
	if err != nil {
		t.Fatalf("Accept second after first closed: %v", err)
	}
	defer secondServer.Close()

	if _, err := secondServer.Write([]byte("ok")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	buf := make([]byte, 8)
	n, err := second.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got := string(buf[:n]); got != "ok" {
		t.Fatalf("got %q, want %q", got, "ok")
	}
}

func TestListenerCloseStopsAccept(t *testing.T) {
	ln := mustListen(t)
	addr := ln.Addr()

	if err := ln.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := ln.Accept(); err == nil {
		t.Fatal("Accept after Close returned nil error")
	}

	conn, err := Dial(context.Background(), addr)
	if err == nil {
		_ = conn.Close()
		t.Fatal("Dial to closed listener succeeded, want error")
	}
}

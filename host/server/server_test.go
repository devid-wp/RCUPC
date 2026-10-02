package server

import (
	"bufio"
	"context"
	"io"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	clientsession "rcupc/client/session"
	"rcupc/pkg/config"
	"rcupc/pkg/logging"
	"rcupc/pkg/protocol"
	"rcupc/pkg/transport"
)

func testLogger() logging.Logger {
	return logging.New(io.Discard, logging.LevelInfo)
}

// startServe listens on an ephemeral local port and runs serve with the
// default configuration.
func startServe(t *testing.T, ctx context.Context) (*transport.Listener, <-chan error) {
	return startServeCfg(t, ctx, config.DefaultHost())
}

// startServeCfg runs serve with a custom configuration on an ephemeral port.
func startServeCfg(t *testing.T, ctx context.Context, cfg config.HostConfig) (*transport.Listener, <-chan error) {
	t.Helper()

	ln, err := transport.Listen(context.Background(), "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	srv := &Server{cfg: cfg, log: testLogger()}
	done := make(chan error, 1)
	go func() { done <- srv.serve(ctx, ln) }()
	return ln, done
}

// handshake dials addr and completes the handshake as a raw client.
func handshake(t *testing.T, addr string) (transport.Conn, *bufio.Reader) {
	t.Helper()

	conn, err := transport.Dial(context.Background(), addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	if err := protocol.WriteLine(conn, protocol.Hello); err != nil {
		t.Fatalf("write HELLO: %v", err)
	}
	r := bufio.NewReader(conn)
	line, err := protocol.ReadLine(r)
	if err != nil {
		t.Fatalf("read handshake: %v", err)
	}
	msg, _, err := protocol.Parse(line)
	if err != nil || msg != protocol.OK {
		t.Fatalf("handshake response = %q (%v), want OK", line, err)
	}
	return conn, r
}

func TestServerShutdownClosesListenerAndSessions(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ln, done := startServe(t, ctx)
	addr := ln.Addr()

	conn, r := handshake(t, addr)
	defer conn.Close()

	// The session works before shutdown.
	if err := protocol.WriteMessage(conn, string(protocol.Ping)); err != nil {
		t.Fatalf("write PING: %v", err)
	}
	resp, err := protocol.ReadMessage(r)
	if err != nil || resp != protocol.Pong {
		t.Fatalf("PING -> %q (%v), want PONG", resp, err)
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not return after cancel")
	}

	// The listener is closed and no longer accepts.
	if c, err := ln.Accept(); err == nil {
		_ = c.Close()
		t.Fatal("listener still accepting after shutdown")
	}

	// New dials are refused.
	if c, err := transport.Dial(context.Background(), addr); err == nil {
		_ = c.Close()
		t.Fatal("dial to shut down server succeeded")
	}

	// The active connection was closed.
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	if _, err := r.ReadByte(); err == nil {
		t.Fatal("active connection still open after shutdown")
	}
}

func TestServerNormalCommandsStillWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ln, done := startServe(t, ctx)

	conn, r := handshake(t, ln.Addr())
	defer conn.Close()

	steps := []struct {
		cmd  protocol.Command
		want string
	}{
		{protocol.Info, protocol.HostInfo},
		{protocol.Ping, protocol.Pong},
		{protocol.Command("BOGUS"), protocol.UnknownCommand},
	}
	for _, step := range steps {
		if err := protocol.WriteMessage(conn, string(step.cmd)); err != nil {
			t.Fatalf("write %s: %v", step.cmd, err)
		}
		resp, err := protocol.ReadMessage(r)
		if err != nil || resp != step.want {
			t.Fatalf("%s -> %q (%v), want %q", step.cmd, resp, err, step.want)
		}
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not return")
	}
}

func TestShutdownNoGoroutineLeak(t *testing.T) {
	warmUpRuntime()
	baseline := runtime.NumGoroutine()

	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		ln, done := startServe(t, ctx)

		conn, r := handshake(t, ln.Addr())
		if err := protocol.WriteMessage(conn, string(protocol.Info)); err != nil {
			t.Fatalf("write INFO: %v", err)
		}
		if resp, err := protocol.ReadMessage(r); err != nil || resp != protocol.HostInfo {
			t.Fatalf("INFO -> %q (%v)", resp, err)
		}

		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("serve: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("serve did not return")
		}
		_ = conn.Close()
	}

	waitForGoroutines(t, baseline)
}

// warmUpRuntime exercises the transport so that runtime/network goroutines
// exist before the leak baseline is taken.
func warmUpRuntime() {
	ln, err := transport.Listen(context.Background(), "127.0.0.1:0")
	if err != nil {
		return
	}
	conn, err := transport.Dial(context.Background(), ln.Addr())
	if err != nil {
		_ = ln.Close()
		return
	}
	_ = ln.Close()
	_ = conn.Close()
}

// waitForGoroutines waits until the goroutine count drops back to want. All
// RCUPC goroutines are joined deterministically before this is called, so the
// only reason to wait is unrelated runtime scheduling.
func waitForGoroutines(t *testing.T, want int) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("goroutine leak: %d goroutines, want <= %d", runtime.NumGoroutine(), want)
}

// recorder is a logging.Logger test double that records entries.
type recorder struct {
	mu      sync.Mutex
	entries []entry
}

type entry struct {
	level string
	msg   string
}

func (r *recorder) add(level, msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, entry{level: level, msg: msg})
}

func (r *recorder) Debug(msg string, _ ...any) { r.add("DEBUG", msg) }
func (r *recorder) Info(msg string, _ ...any)  { r.add("INFO", msg) }
func (r *recorder) Warn(msg string, _ ...any)  { r.add("WARN", msg) }
func (r *recorder) Error(msg string, _ ...any) { r.add("ERROR", msg) }

func (r *recorder) has(level, msg string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.entries {
		if e.level == level && e.msg == msg {
			return true
		}
	}
	return false
}

func TestServerLogsLifecycle(t *testing.T) {
	rec := &recorder{}
	ctx, cancel := context.WithCancel(context.Background())

	ln, err := transport.Listen(context.Background(), "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &Server{cfg: config.DefaultHost(), log: rec}

	done := make(chan error, 1)
	go func() { done <- srv.serve(ctx, ln) }()

	conn, r := handshake(t, ln.Addr())
	if err := protocol.WriteMessage(conn, string(protocol.Ping)); err != nil {
		t.Fatalf("write PING: %v", err)
	}
	if resp, err := protocol.ReadMessage(r); err != nil || resp != protocol.Pong {
		t.Fatalf("PING -> %q (%v), want PONG", resp, err)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not return")
	}
	_ = conn.Close()

	for _, want := range [][2]string{
		{"INFO", "listening"},
		{"INFO", "client connected"},
		{"INFO", "command handled"},
		{"INFO", "session closed"},
		{"INFO", "server shutdown"},
	} {
		if !rec.has(want[0], want[1]) {
			t.Errorf("missing log %s %q", want[0], want[1])
		}
	}
}

func TestCustomMaxLineLen(t *testing.T) {
	cfg := config.DefaultHost()
	cfg.MaxLineLen = 24 // fits the 17-byte handshake, rejects a 40-byte command

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ln, done := startServeCfg(t, ctx, cfg)

	conn, r := handshake(t, ln.Addr())
	defer conn.Close()

	if _, err := conn.Write([]byte(strings.Repeat("A", 40) + "\n")); err != nil {
		t.Fatalf("write oversized: %v", err)
	}
	resp, err := protocol.ReadMessage(r)
	if err != nil || resp != protocol.LineTooLong {
		t.Fatalf("oversized -> %q (%v), want %q", resp, err, protocol.LineTooLong)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not return")
	}
}

func TestConfiguredClientConnect(t *testing.T) {
	hostCfg := config.DefaultHost()

	ln, err := transport.Listen(context.Background(), "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	hostCfg.ListenAddr = ln.Addr()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := &Server{cfg: hostCfg, log: testLogger()}
	done := make(chan error, 1)
	go func() { done <- srv.serve(ctx, ln) }()

	clientCfg := config.DefaultClient()
	clientCfg.HostAddr = ln.Addr()

	sess, err := clientsession.Connect(ctx, clientCfg, testLogger())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = sess.Close() }()

	if resp, err := sess.Send(protocol.Info); err != nil || resp != protocol.HostInfo {
		t.Fatalf("INFO -> %q (%v), want %q", resp, err, protocol.HostInfo)
	}
	if resp, err := sess.Send(protocol.Ping); err != nil || resp != protocol.Pong {
		t.Fatalf("PING -> %q (%v), want %q", resp, err, protocol.Pong)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not return")
	}
}

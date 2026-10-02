package session

import (
	"bufio"
	"context"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"rcupc/pkg/config"
	"rcupc/pkg/logging"
	"rcupc/pkg/protocol"
)

func newLogger() logging.Logger {
	return logging.New(io.Discard, logging.LevelInfo)
}

func testConfig() config.HostConfig {
	return config.DefaultHost()
}

func TestRunHandshakeOK(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- Run(ctx, server, testConfig(), newLogger()) }()

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
	go func() { done <- Run(ctx, server, testConfig(), newLogger()) }()

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

// startSession runs Run on one end of a pipe, completes the handshake and
// returns the client end plus a reader positioned at the message phase.
func startSession(t *testing.T, ctx context.Context) (net.Conn, *bufio.Reader, chan error) {
	t.Helper()

	server, client := net.Pipe()
	done := make(chan error, 1)
	go func() { done <- Run(ctx, server, testConfig(), newLogger()) }()

	if err := protocol.WriteLine(client, protocol.Hello); err != nil {
		t.Fatalf("write HELLO: %v", err)
	}
	r := bufio.NewReader(client)
	line, err := protocol.ReadLine(r)
	if err != nil {
		t.Fatalf("read handshake response: %v", err)
	}
	msg, _, err := protocol.Parse(line)
	if err != nil || msg != protocol.OK {
		t.Fatalf("handshake response = %q (%v), want OK", line, err)
	}
	return client, r, done
}

func writeCommand(t *testing.T, w io.Writer, cmd protocol.Command) {
	t.Helper()
	if err := protocol.WriteMessage(w, string(cmd)); err != nil {
		t.Fatalf("write %s: %v", cmd, err)
	}
}

func readResponse(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	resp, err := protocol.ReadMessage(r)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	return resp
}

func waitRun(t *testing.T, done chan error, wantErr bool) {
	t.Helper()
	select {
	case err := <-done:
		if wantErr && err == nil {
			t.Fatal("Run returned nil, want error")
		}
		if !wantErr && err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return")
	}
}

func TestMessagePing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client, r, done := startSession(t, ctx)
	defer client.Close()

	writeCommand(t, client, protocol.Ping)
	if resp := readResponse(t, r); resp != protocol.Pong {
		t.Fatalf("PING -> %q, want %q", resp, protocol.Pong)
	}

	client.Close()
	waitRun(t, done, false)
}

func TestMessageInfo(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client, r, done := startSession(t, ctx)
	defer client.Close()

	writeCommand(t, client, protocol.Info)
	if resp := readResponse(t, r); resp != protocol.HostInfo {
		t.Fatalf("INFO -> %q, want %q", resp, protocol.HostInfo)
	}

	client.Close()
	waitRun(t, done, false)
}

func TestMessageUnknown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client, r, done := startSession(t, ctx)
	defer client.Close()

	writeCommand(t, client, protocol.Command("BOGUS"))
	if resp := readResponse(t, r); resp != protocol.UnknownCommand {
		t.Fatalf("BOGUS -> %q, want %q", resp, protocol.UnknownCommand)
	}

	client.Close()
	waitRun(t, done, false)
}

func TestMessageMultiple(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client, r, done := startSession(t, ctx)
	defer client.Close()

	steps := []struct {
		cmd  protocol.Command
		want string
	}{
		{protocol.Ping, protocol.Pong},
		{protocol.Info, protocol.HostInfo},
		{protocol.Command("BOGUS"), protocol.UnknownCommand},
		{protocol.Ping, protocol.Pong},
	}
	for _, step := range steps {
		writeCommand(t, client, step.cmd)
		if resp := readResponse(t, r); resp != step.want {
			t.Fatalf("%s -> %q, want %q", step.cmd, resp, step.want)
		}
	}

	client.Close()
	waitRun(t, done, false)
}

func TestMessageOversized(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client, r, done := startSession(t, ctx)
	defer client.Close()

	oversized := strings.Repeat("A", protocol.MaxLineLen+1) + "\n"
	if _, err := io.WriteString(client, oversized); err != nil {
		t.Fatalf("write oversized line: %v", err)
	}
	if resp := readResponse(t, r); resp != protocol.LineTooLong {
		t.Fatalf("oversized -> %q, want %q", resp, protocol.LineTooLong)
	}
	waitRun(t, done, true)
}

func TestSessionsIndependent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c1, _, d1 := startSession(t, ctx)
	c2, r2, d2 := startSession(t, ctx)
	defer c2.Close()

	// Closing the first session must not affect the second one.
	c1.Close()
	waitRun(t, d1, false)

	writeCommand(t, c2, protocol.Ping)
	if resp := readResponse(t, r2); resp != protocol.Pong {
		t.Fatalf("second session PING -> %q, want %q", resp, protocol.Pong)
	}

	c2.Close()
	waitRun(t, d2, false)
}

func TestSessionCancellationIndependent(t *testing.T) {
	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()

	c1, _, d1 := startSession(t, ctx1)
	c2, r2, d2 := startSession(t, ctx2)
	defer c1.Close()
	defer c2.Close()

	// Cancelling one session must not affect the other.
	cancel1()
	waitRun(t, d1, false)

	writeCommand(t, c2, protocol.Ping)
	if resp := readResponse(t, r2); resp != protocol.Pong {
		t.Fatalf("second session PING -> %q, want %q", resp, protocol.Pong)
	}

	c2.Close()
	waitRun(t, d2, false)
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

func TestSessionLogsInvalidHandshake(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()

	rec := &recorder{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- Run(ctx, server, testConfig(), rec) }()

	if _, err := io.WriteString(client, "OTHER/9.9 HELLO\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, _ = protocol.ReadLine(bufio.NewReader(client))

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return")
	}

	if !rec.has("WARN", "invalid handshake") {
		t.Fatalf("missing WARN invalid handshake log")
	}
}

func TestHandshakeTimeout(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()

	cfg := config.DefaultHost()
	cfg.HandshakeTimeout = 50 * time.Millisecond

	done := make(chan error, 1)
	go func() { done <- Run(context.Background(), server, cfg, newLogger()) }()

	// The client never sends HELLO, so Run must give up after the timeout.
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Run returned nil, want handshake timeout error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not time out")
	}
}

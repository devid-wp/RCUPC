package logging

import (
	"bytes"
	"strings"
	"sync"
	"testing"
)

func TestLevels(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf, LevelInfo)

	log.Debug("debug message")
	log.Info("info message")
	log.Warn("warn message")
	log.Error("error message")

	out := buf.String()
	if strings.Contains(out, "debug message") {
		t.Errorf("debug entry emitted at LevelInfo: %q", out)
	}
	for _, want := range []string{
		`level=INFO msg="info message"`,
		`level=WARN msg="warn message"`,
		`level=ERROR msg="error message"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestDebugEnabledAtDebugLevel(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf, LevelDebug)
	log.Debug("debug message")

	if !strings.Contains(buf.String(), "debug message") {
		t.Fatalf("debug entry not emitted at LevelDebug: %q", buf.String())
	}
}

func TestStructuredFields(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf, LevelInfo)
	log.Info("command handled", "command", "PING", "response", "PONG")

	out := buf.String()
	if !strings.Contains(out, `msg="command handled"`) {
		t.Errorf("missing message: %q", out)
	}
	if !strings.Contains(out, "command=PING") {
		t.Errorf("missing command field: %q", out)
	}
	if !strings.Contains(out, "response=PONG") {
		t.Errorf("missing response field: %q", out)
	}
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestConcurrentLogging(t *testing.T) {
	const goroutines = 16
	const perGoroutine = 50

	var buf syncBuffer
	log := New(&buf, LevelInfo)

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				log.Info("entry", "worker", i, "n", j)
			}
		}(i)
	}
	wg.Wait()

	if got, want := strings.Count(buf.String(), "msg=entry"), goroutines*perGoroutine; got != want {
		t.Fatalf("logged %d entries, want %d", got, want)
	}
}

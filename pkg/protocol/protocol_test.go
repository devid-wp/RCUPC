package protocol

import (
	"bufio"
	"errors"
	"strings"
	"testing"
)

func TestLine(t *testing.T) {
	if got, want := Line(Hello), "MIRROR/0.1 HELLO\n"; got != want {
		t.Fatalf("Line(Hello) = %q, want %q", got, want)
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		name       string
		line       string
		wantMsg    Message
		wantReason string
		wantErr    bool
	}{
		{"hello", "MIRROR/0.1 HELLO\n", Hello, "", false},
		{"ok", "MIRROR/0.1 OK\n", OK, "", false},
		{"error with reason", "MIRROR/0.1 ERROR bad version\n", Error, "bad version", false},
		{"crlf", "MIRROR/0.1 OK\r\n", OK, "", false},
		{"bad version", "OTHER/1.0 HELLO\n", "", "", true},
		{"malformed", "HELLO\n", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, reason, err := Parse(tt.line)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Parse(%q) error = nil, want error", tt.line)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q) unexpected error: %v", tt.line, err)
			}
			if msg != tt.wantMsg || reason != tt.wantReason {
				t.Fatalf("Parse(%q) = (%q, %q), want (%q, %q)",
					tt.line, msg, reason, tt.wantMsg, tt.wantReason)
			}
		})
	}
}

func TestReadLineTooLong(t *testing.T) {
	long := strings.Repeat("A", MaxLineLen+1) + "\n"
	_, err := ReadLine(bufio.NewReader(strings.NewReader(long)))
	if !errors.Is(err, ErrLineTooLong) {
		t.Fatalf("ReadLine error = %v, want ErrLineTooLong", err)
	}
}

func TestReadWriteRoundTrip(t *testing.T) {
	var sb strings.Builder
	if err := WriteLine(&sb, Hello); err != nil {
		t.Fatal(err)
	}

	msg, _, err := Parse(sb.String())
	if err != nil || msg != Hello {
		t.Fatalf("Parse round trip = (%q, %v), want HELLO", msg, err)
	}

	line, err := ReadLine(bufio.NewReader(strings.NewReader(sb.String())))
	if err != nil {
		t.Fatal(err)
	}
	if line != "MIRROR/0.1 HELLO\n" {
		t.Fatalf("ReadLine = %q", line)
	}
}

package protocol

import (
	"bufio"
	"errors"
	"strings"
	"testing"
)

func TestReply(t *testing.T) {
	tests := []struct {
		cmd  Command
		want string
	}{
		{Ping, Pong},
		{Info, HostInfo},
		{Command("NOPE"), UnknownCommand},
		{Command(""), UnknownCommand},
	}
	for _, tt := range tests {
		if got := Reply(tt.cmd); got != tt.want {
			t.Errorf("Reply(%q) = %q, want %q", tt.cmd, got, tt.want)
		}
	}
}

func TestValidate(t *testing.T) {
	if err := Validate(Ping, Pong); err != nil {
		t.Errorf("Validate(PING, PONG) = %v, want nil", err)
	}
	if err := Validate(Info, HostInfo); err != nil {
		t.Errorf("Validate(INFO, HOST) = %v, want nil", err)
	}
	if err := Validate(Ping, "NOPE"); err == nil {
		t.Error("Validate(PING, NOPE) = nil, want error")
	}
	if err := Validate(Command("NOPE"), UnknownCommand); err == nil {
		t.Error("Validate(unknown) = nil, want error")
	}
}

func TestWriteReadMessage(t *testing.T) {
	var sb strings.Builder
	if err := WriteMessage(&sb, Pong); err != nil {
		t.Fatal(err)
	}
	if sb.String() != "PONG\n" {
		t.Fatalf("WriteMessage = %q, want %q", sb.String(), "PONG\n")
	}

	got, err := ReadMessage(bufio.NewReader(strings.NewReader("PING\n")))
	if err != nil || got != "PING" {
		t.Fatalf("ReadMessage = (%q, %v), want PING", got, err)
	}

	got, err = ReadMessage(bufio.NewReader(strings.NewReader("PING\r\n")))
	if err != nil || got != "PING" {
		t.Fatalf("ReadMessage CRLF = (%q, %v), want PING", got, err)
	}
}

func TestReadMessageTooLong(t *testing.T) {
	long := strings.Repeat("A", MaxLineLen+1) + "\n"
	_, err := ReadMessage(bufio.NewReader(strings.NewReader(long)))
	if !errors.Is(err, ErrLineTooLong) {
		t.Fatalf("ReadMessage error = %v, want ErrLineTooLong", err)
	}
}

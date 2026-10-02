package protocol

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// Command is a message-phase command sent by the client to the host.
type Command string

const (
	Ping Command = "PING"
	Info Command = "INFO"
)

// Message-phase responses. Unlike the handshake these are raw lines without the
// protocol-version prefix.
const (
	Pong           = "PONG"
	HostInfo       = "RCUPC/0.1 HOST"
	UnknownCommand = "ERROR unknown command"
	LineTooLong    = "ERROR line too long"
)

// Reply returns the host response for a command. Unknown commands get an error
// response instead of closing the connection.
func Reply(cmd Command) string {
	switch cmd {
	case Ping:
		return Pong
	case Info:
		return HostInfo
	default:
		return UnknownCommand
	}
}

// Validate checks that resp is the response expected for cmd.
func Validate(cmd Command, resp string) error {
	switch cmd {
	case Ping:
		if resp != Pong {
			return fmt.Errorf("protocol: %s answered with %q, want %q", cmd, resp, Pong)
		}
	case Info:
		if resp != HostInfo {
			return fmt.Errorf("protocol: %s answered with %q, want %q", cmd, resp, HostInfo)
		}
	default:
		return fmt.Errorf("protocol: unknown command %q", cmd)
	}
	return nil
}

// WriteMessage writes a raw newline-terminated message line.
func WriteMessage(w io.Writer, s string) error {
	_, err := io.WriteString(w, s+"\n")
	return err
}

// ReadMessage reads one raw newline-terminated line and strips the terminator,
// enforcing the default MaxLineLen. Overlong lines are rejected with
// ErrLineTooLong.
func ReadMessage(r *bufio.Reader) (string, error) {
	return ReadMessageLimit(r, MaxLineLen)
}

// ReadMessageLimit reads one raw message line, enforcing at most max bytes.
func ReadMessageLimit(r *bufio.Reader, max int) (string, error) {
	line, err := ReadLineLimit(r, max)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// Package protocol defines the RCUPC wire format.
//
// The connection starts with a single line-based handshake:
//
//	client -> host   MIRROR/0.1 HELLO\n
//	host   -> client MIRROR/0.1 OK\n
//
// On rejection the host answers MIRROR/0.1 ERROR <reason>\n and closes the
// connection. After a successful handshake the connection enters the message
// phase, where raw newline-delimited commands and responses are exchanged
// (see message.go).
//
// This package knows only bytes and framing; it has no idea about sockets,
// sessions or the meaning of a connection.
package protocol

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Version is the protocol version implemented by this build.
const Version = "MIRROR/0.1"

// MaxLineLen bounds one newline-terminated line (handshake or message phase)
// so a misbehaving peer cannot make us buffer unbounded data.
const MaxLineLen = 256

// ErrLineTooLong is returned when a line exceeds MaxLineLen.
var ErrLineTooLong = errors.New("protocol: line too long")

// Message is a handshake token.
type Message string

const (
	Hello Message = "HELLO"
	OK    Message = "OK"
	Error Message = "ERROR"
)

// Line renders a message as a wire line, e.g. "MIRROR/0.1 OK\n".
func Line(m Message) string {
	return Version + " " + string(m) + "\n"
}

// WriteLine writes one message line to w.
func WriteLine(w io.Writer, m Message) error {
	_, err := io.WriteString(w, Line(m))
	return err
}

// WriteError writes a rejection line carrying a human-readable reason.
// Newlines are collapsed so the reason stays on a single line.
func WriteError(w io.Writer, reason string) error {
	reason = strings.ReplaceAll(reason, "\n", " ")
	_, err := io.WriteString(w, Version+" "+string(Error)+" "+reason+"\n")
	return err
}

// Parse validates a wire line and returns its message and, for ERROR, the
// reason. It fails on a malformed line or an unsupported protocol version.
func Parse(line string) (Message, string, error) {
	line = strings.TrimRight(line, "\r\n")
	parts := strings.SplitN(line, " ", 3)
	if len(parts) < 2 {
		return "", "", fmt.Errorf("protocol: malformed line %q", line)
	}
	if parts[0] != Version {
		return "", "", fmt.Errorf("protocol: unsupported version %q (want %q)", parts[0], Version)
	}
	var reason string
	if len(parts) == 3 {
		reason = parts[2]
	}
	return Message(parts[1]), reason, nil
}

// ReadLine reads one newline-terminated line, enforcing the default MaxLineLen.
func ReadLine(r *bufio.Reader) (string, error) {
	return ReadLineLimit(r, MaxLineLen)
}

// ReadLineLimit reads one newline-terminated line, enforcing at most max bytes.
func ReadLineLimit(r *bufio.Reader, max int) (string, error) {
	var buf []byte
	for {
		part, err := r.ReadSlice('\n')
		buf = append(buf, part...)
		if len(buf) > max {
			return "", ErrLineTooLong
		}
		switch {
		case err == nil:
			return string(buf), nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		default:
			return "", err
		}
	}
}

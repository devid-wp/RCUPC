// Package transport is the RCUPC networking boundary.
//
// It wraps the standard library TCP types so that session and application code
// never touch net.Listener or net.Conn directly. Only the operations the host
// and client need are exposed: listen, accept, dial, read, write, set a
// deadline, report the peer address and close.
package transport

import (
	"context"
	"net"
	"time"
)

// Conn is an established, bidirectional byte stream.
//
// It is an interface so that sessions can be unit-tested with in-memory pipes
// while production uses TCP. Both *net.TCPConn and net.Pipe connections satisfy
// it. This is the single seam between the transport package and the sessions.
type Conn interface {
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	Close() error

	// SetDeadline sets the read and write deadline; used for the handshake timeout.
	SetDeadline(t time.Time) error
	// RemoteAddr returns the peer address, for logging.
	RemoteAddr() net.Addr
}

// Listener accepts incoming TCP connections.
type Listener struct {
	ln net.Listener
}

// Listen starts a TCP listener on addr, for example "127.0.0.1:0" or ":9000".
func Listen(ctx context.Context, addr string) (*Listener, error) {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	return &Listener{ln: ln}, nil
}

// Accept waits for and returns the next incoming connection.
func (l *Listener) Accept() (Conn, error) {
	conn, err := l.ln.Accept()
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// Addr returns the address the listener is bound to.
func (l *Listener) Addr() string {
	return l.ln.Addr().String()
}

// Close stops the listener. Pending and future Accept calls return an error.
func (l *Listener) Close() error {
	return l.ln.Close()
}

// Dial connects to a TCP address such as "192.168.1.42:9000".
func Dial(ctx context.Context, addr string) (Conn, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

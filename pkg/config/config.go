// Package config defines the runtime settings of the RCUPC host and client.
//
// Configuration is plain data: it is built from defaults and command-line
// flags, validated before startup and then treated as immutable. It is also
// where the flag definitions live, so entrypoints do not hand-roll them and
// sessions never parse flags.
package config

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"strconv"
	"time"

	"rcupc/pkg/protocol"
)

const (
	// DefaultHostAddr is the host listen address used when none is configured.
	DefaultHostAddr = ":9000"
	// DefaultHandshakeTimeout bounds the handshake exchange.
	DefaultHandshakeTimeout = 5 * time.Second
	// DefaultMaxLineLen matches the protocol's default line limit.
	DefaultMaxLineLen = protocol.MaxLineLen
)

// HostConfig holds the validated runtime settings of the host.
type HostConfig struct {
	ListenAddr       string
	HandshakeTimeout time.Duration
	MaxLineLen       int
}

// ClientConfig holds the validated runtime settings of the client.
type ClientConfig struct {
	HostAddr         string
	HandshakeTimeout time.Duration
	MaxLineLen       int
}

// DefaultHost returns the host configuration used when no flags are given.
func DefaultHost() HostConfig {
	return HostConfig{
		ListenAddr:       DefaultHostAddr,
		HandshakeTimeout: DefaultHandshakeTimeout,
		MaxLineLen:       DefaultMaxLineLen,
	}
}

// DefaultClient returns the client configuration before the target address is
// supplied on the command line.
func DefaultClient() ClientConfig {
	return ClientConfig{
		HandshakeTimeout: DefaultHandshakeTimeout,
		MaxLineLen:       DefaultMaxLineLen,
	}
}

// BindHost registers the host flags on fs. Current values are the defaults.
func BindHost(fs *flag.FlagSet, cfg *HostConfig) {
	fs.StringVar(&cfg.ListenAddr, "addr", cfg.ListenAddr, "TCP address to listen on")
	fs.DurationVar(&cfg.HandshakeTimeout, "handshake-timeout", cfg.HandshakeTimeout, "maximum time for the handshake")
	fs.IntVar(&cfg.MaxLineLen, "max-line-len", cfg.MaxLineLen, "maximum message line length in bytes")
}

// BindClient registers the client flags on fs. The target address is a
// positional argument; current values are the defaults.
func BindClient(fs *flag.FlagSet, cfg *ClientConfig) {
	fs.DurationVar(&cfg.HandshakeTimeout, "handshake-timeout", cfg.HandshakeTimeout, "maximum time for the handshake")
	fs.IntVar(&cfg.MaxLineLen, "max-line-len", cfg.MaxLineLen, "maximum message line length in bytes")
}

// Validate reports whether the host configuration is usable.
func (c HostConfig) Validate() error {
	if err := validateAddr(c.ListenAddr, false); err != nil {
		return err
	}
	if err := validatePositive("handshake timeout", c.HandshakeTimeout); err != nil {
		return err
	}
	return validateMaxLineLen(c.MaxLineLen)
}

// Validate reports whether the client configuration is usable.
func (c ClientConfig) Validate() error {
	if err := validateAddr(c.HostAddr, true); err != nil {
		return err
	}
	if err := validatePositive("handshake timeout", c.HandshakeTimeout); err != nil {
		return err
	}
	return validateMaxLineLen(c.MaxLineLen)
}

func validateAddr(addr string, requireHost bool) error {
	if addr == "" {
		return errors.New("address must not be empty")
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid address %q: %w", addr, err)
	}
	if requireHost && host == "" {
		return fmt.Errorf("invalid address %q: host must not be empty", addr)
	}
	if port == "" {
		return fmt.Errorf("invalid address %q: port must not be empty", addr)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 0 || n > 65535 {
		return fmt.Errorf("invalid address %q: port must be a number between 0 and 65535", addr)
	}
	return nil
}

func validatePositive(name string, d time.Duration) error {
	if d <= 0 {
		return fmt.Errorf("%s must be positive, got %s", name, d)
	}
	return nil
}

func validateMaxLineLen(n int) error {
	if n <= 0 {
		return fmt.Errorf("max line length must be positive, got %d", n)
	}
	return nil
}

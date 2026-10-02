package config

import (
	"testing"
	"time"

	"rcupc/pkg/protocol"
)

func TestDefaultHost(t *testing.T) {
	cfg := DefaultHost()

	if cfg.ListenAddr != ":9000" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, ":9000")
	}
	if cfg.HandshakeTimeout != 5*time.Second {
		t.Errorf("HandshakeTimeout = %s, want 5s", cfg.HandshakeTimeout)
	}
	if cfg.MaxLineLen != protocol.MaxLineLen {
		t.Errorf("MaxLineLen = %d, want %d", cfg.MaxLineLen, protocol.MaxLineLen)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}

func TestDefaultClient(t *testing.T) {
	cfg := DefaultClient()

	if cfg.HandshakeTimeout != 5*time.Second {
		t.Errorf("HandshakeTimeout = %s, want 5s", cfg.HandshakeTimeout)
	}
	if cfg.MaxLineLen != protocol.MaxLineLen {
		t.Errorf("MaxLineLen = %d, want %d", cfg.MaxLineLen, protocol.MaxLineLen)
	}
	// No target address is known until it is given on the command line.
	if err := cfg.Validate(); err == nil {
		t.Error("Validate() without an address = nil, want error")
	}
	cfg.HostAddr = "127.0.0.1:9000"
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}

func TestHostValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*HostConfig)
		wantErr bool
	}{
		{"default", func(*HostConfig) {}, false},
		{"valid custom", func(c *HostConfig) {
			c.ListenAddr = "192.168.1.5:1234"
			c.HandshakeTimeout = 2 * time.Second
			c.MaxLineLen = 1024
		}, false},
		{"ephemeral port", func(c *HostConfig) { c.ListenAddr = "127.0.0.1:0" }, false},
		{"empty address", func(c *HostConfig) { c.ListenAddr = "" }, true},
		{"address without port", func(c *HostConfig) { c.ListenAddr = "localhost" }, true},
		{"non-numeric port", func(c *HostConfig) { c.ListenAddr = "localhost:abc" }, true},
		{"port out of range", func(c *HostConfig) { c.ListenAddr = "localhost:70000" }, true},
		{"zero timeout", func(c *HostConfig) { c.HandshakeTimeout = 0 }, true},
		{"negative timeout", func(c *HostConfig) { c.HandshakeTimeout = -time.Second }, true},
		{"zero max line len", func(c *HostConfig) { c.MaxLineLen = 0 }, true},
		{"negative max line len", func(c *HostConfig) { c.MaxLineLen = -1 }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultHost()
			tt.mutate(&cfg)
			err := cfg.Validate()
			if tt.wantErr && err == nil {
				t.Fatalf("Validate() = nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
		})
	}
}

func TestClientValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*ClientConfig)
		wantErr bool
	}{
		{"valid", func(c *ClientConfig) { c.HostAddr = "192.168.1.42:9000" }, false},
		{"valid hostname", func(c *ClientConfig) { c.HostAddr = "localhost:9000" }, false},
		{"empty address", func(c *ClientConfig) { c.HostAddr = "" }, true},
		{"missing host", func(c *ClientConfig) { c.HostAddr = ":9000" }, true},
		{"missing port", func(c *ClientConfig) { c.HostAddr = "localhost" }, true},
		{"non-numeric port", func(c *ClientConfig) { c.HostAddr = "localhost:abc" }, true},
		{"zero timeout", func(c *ClientConfig) { c.HandshakeTimeout = 0 }, true},
		{"zero max line len", func(c *ClientConfig) { c.MaxLineLen = 0 }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultClient()
			cfg.HostAddr = "127.0.0.1:9000"
			tt.mutate(&cfg)
			err := cfg.Validate()
			if tt.wantErr && err == nil {
				t.Fatalf("Validate() = nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
		})
	}
}

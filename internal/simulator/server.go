package simulator

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"time"

	"github.com/simonvetter/modbus"
)

// Server serves a Device over Modbus TCP and can be stopped and restarted on the same address to simulate an
// outage, such as the real device's Modbus server going away while it has no cloud connection.
type Server struct {
	addr string
	srv  *modbus.ModbusServer
}

// Listen starts serving dev on addr (host:port). Port 0 picks a free port; Addr reports the one chosen. A nil
// logger discards the Modbus library's log output.
func Listen(ctx context.Context, dev *Device, addr string, logger *log.Logger) (*Server, error) {
	resolved, err := resolvePort(ctx, addr)
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	srv, err := modbus.NewServer(&modbus.ServerConfiguration{
		URL:        "tcp://" + resolved,
		Timeout:    time.Minute,
		MaxClients: 8,
		Logger:     logger,
	}, dev)
	if err != nil {
		return nil, fmt.Errorf("create modbus server: %w", err)
	}
	s := &Server{addr: resolved, srv: srv}
	if err := s.Start(); err != nil {
		return nil, err
	}
	return s, nil
}

// Addr returns the host:port the server listens on.
func (s *Server) Addr() string {
	return s.addr
}

// Start resumes serving on the same address after Stop.
func (s *Server) Start() error {
	if err := s.srv.Start(); err != nil {
		return fmt.Errorf("start modbus server on %s: %w", s.addr, err)
	}
	return nil
}

// Stop stops listening and closes every open client session.
func (s *Server) Stop() error {
	if err := s.srv.Stop(); err != nil {
		return fmt.Errorf("stop modbus server on %s: %w", s.addr, err)
	}
	return nil
}

// The Modbus library doesn't expose its listener's address, so a free port is reserved and released up front.
func resolvePort(ctx context.Context, addr string) (string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("listen address %q: %w", addr, err)
	}
	if port != "0" {
		return addr, nil
	}
	var lc net.ListenConfig
	l, err := lc.Listen(ctx, "tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		return "", fmt.Errorf("reserve a free port on %s: %w", host, err)
	}
	resolved := l.Addr().String()
	if err := l.Close(); err != nil {
		return "", fmt.Errorf("release reserved port %s: %w", resolved, err)
	}
	return resolved, nil
}

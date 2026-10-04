//go:build integration

package testbroker

import (
	"context"
	"io"
	"net"
	"sync"
	"testing"
)

// Proxy forwards TCP connections to a broker. Cut drops every connection without an MQTT DISCONNECT, which makes
// the broker publish the clients' Last Wills; Restore accepts connections again on the same address.
type Proxy struct {
	target string
	addr   string

	mu    sync.Mutex
	ln    net.Listener
	conns []net.Conn
}

// NewProxy starts a proxy to target (host:port) on a free loopback port for the duration of the test.
func NewProxy(t *testing.T, target string) *Proxy {
	t.Helper()
	p := &Proxy{target: target, addr: "127.0.0.1:0"}
	if err := p.listen(t.Context()); err != nil {
		t.Fatalf("start proxy: %v", err)
	}
	t.Cleanup(p.Cut)
	return p
}

// URL returns the proxy's address as an mqtt:// broker URL.
func (p *Proxy) URL() string {
	return "mqtt://" + p.addr
}

// Cut closes the listener and every forwarded connection.
func (p *Proxy) Cut() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ln != nil {
		_ = p.ln.Close()
		p.ln = nil
	}
	for _, c := range p.conns {
		_ = c.Close()
	}
	p.conns = nil
}

// Restore listens again on the address used before Cut.
func (p *Proxy) Restore(t *testing.T) {
	t.Helper()
	if err := p.listen(t.Context()); err != nil {
		t.Fatalf("restore proxy: %v", err)
	}
}

func (p *Proxy) listen(ctx context.Context) error {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", p.addr)
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.ln, p.addr = ln, ln.Addr().String()
	p.mu.Unlock()
	go p.accept(ctx, ln)
	return nil
}

func (p *Proxy) accept(ctx context.Context, ln net.Listener) {
	for {
		client, err := ln.Accept()
		if err != nil {
			return
		}
		var d net.Dialer
		upstream, err := d.DialContext(ctx, "tcp", p.target)
		if err != nil {
			_ = client.Close()
			continue
		}
		p.mu.Lock()
		p.conns = append(p.conns, client, upstream)
		p.mu.Unlock()
		go pipe(client, upstream)
		go pipe(upstream, client)
	}
}

func pipe(dst, src net.Conn) {
	_, _ = io.Copy(dst, src)
	_ = dst.Close()
}

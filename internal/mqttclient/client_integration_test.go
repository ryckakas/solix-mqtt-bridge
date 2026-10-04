//go:build integration

package mqttclient_test

import (
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/ryckakas/solix-mqtt-bridge/internal/mqttclient"
	"github.com/ryckakas/solix-mqtt-bridge/internal/testbroker"
)

func TestPublishDeliversRetainedMessages(t *testing.T) {
	b := testbroker.Start(t, testbroker.Options{})
	sub := testbroker.Subscribe(t, b.URL, "t/#")
	c := connect(t, mqttclient.Config{URL: b.URL, ClientID: "pub"}, mqttclient.Will{Topic: "t/will"}, nil)

	if err := c.Publish(t.Context(), "t/value", []byte("42"), true); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if _, ok := sub.WaitFor(5*time.Second, topicPayload("t/value", "42")); !ok {
		t.Fatalf("t/value=42 not delivered; got %v", sub.Messages())
	}
	late := testbroker.Subscribe(t, b.URL, "t/value")
	if m, ok := late.WaitFor(5*time.Second, topicPayload("t/value", "42")); !ok || !m.Retained {
		t.Errorf("late subscriber: got %+v (found %v), want a retained 42", m, ok)
	}
}

func TestPublishWhileDisconnectedIsDroppedNotQueued(t *testing.T) {
	b := testbroker.Start(t, testbroker.Options{})
	proxy := testbroker.NewProxy(t, testbroker.HostPort(b.URL))
	sub := testbroker.Subscribe(t, b.URL, "t/#")
	reconnected := make(chan struct{}, 4)
	c := connect(t, mqttclient.Config{URL: proxy.URL(), ClientID: "pub"}, mqttclient.Will{Topic: "t/will"},
		func() { reconnected <- struct{}{} })
	<-reconnected

	proxy.Cut()
	waitFor(t, "connection loss", func() bool { return !c.Connected() })
	if err := c.Publish(t.Context(), "t/stale", []byte("old"), false); !errors.Is(err, mqttclient.ErrNotConnected) {
		t.Fatalf("Publish while disconnected: err = %v, want ErrNotConnected", err)
	}

	proxy.Restore(t)
	select {
	case <-reconnected:
	case <-time.After(30 * time.Second):
		t.Fatal("no reconnect after the proxy came back")
	}
	if err := c.Publish(t.Context(), "t/fresh", []byte("new"), false); err != nil {
		t.Fatalf("Publish after reconnect: %v", err)
	}
	if _, ok := sub.WaitFor(5*time.Second, topicPayload("t/fresh", "new")); !ok {
		t.Fatal("t/fresh not delivered after reconnect")
	}
	if m, ok := sub.Last("t/stale"); ok {
		t.Errorf("message published while disconnected was replayed: %+v", m)
	}
}

func TestWillIsPublishedWhenTheNetworkFails(t *testing.T) {
	b := testbroker.Start(t, testbroker.Options{})
	proxy := testbroker.NewProxy(t, testbroker.HostPort(b.URL))
	sub := testbroker.Subscribe(t, b.URL, "t/#")
	connected := make(chan struct{}, 1)
	connect(t, mqttclient.Config{URL: proxy.URL(), ClientID: "pub"},
		mqttclient.Will{Topic: "t/availability", Payload: []byte("offline"), Retained: true},
		func() { connected <- struct{}{} })
	<-connected

	proxy.Cut()
	if _, ok := sub.WaitFor(10*time.Second, topicPayload("t/availability", "offline")); !ok {
		t.Fatalf("will not published after a network failure; got %v", sub.Messages())
	}
}

func TestCleanDisconnectSuppressesTheWill(t *testing.T) {
	b := testbroker.Start(t, testbroker.Options{})
	sub := testbroker.Subscribe(t, b.URL, "t/#")
	c := connect(t, mqttclient.Config{URL: b.URL, ClientID: "pub"},
		mqttclient.Will{Topic: "t/availability", Payload: []byte("offline")}, nil)
	if err := c.Publish(t.Context(), "t/marker", []byte("1"), false); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if _, ok := sub.WaitFor(5*time.Second, topicPayload("t/marker", "1")); !ok {
		t.Fatal("marker not delivered")
	}

	c.Disconnect()
	if m, ok := sub.WaitFor(2*time.Second, topicPayload("t/availability", "offline")); ok {
		t.Errorf("will published after a clean disconnect: %+v", m)
	}
}

func TestTLSVerifiesAgainstTheCAFileOrSkipsWhenInsecure(t *testing.T) {
	b := testbroker.Start(t, testbroker.Options{TLS: true})
	sub := testbroker.Subscribe(t, b.URL, "t/#")

	for name, cfg := range map[string]mqttclient.Config{
		"ca-file":  {URL: b.TLSURL, ClientID: "tls-ca", CAFile: b.CAFile},
		"insecure": {URL: b.TLSURL, ClientID: "tls-insecure", TLSInsecure: true},
	} {
		t.Run(name, func(t *testing.T) {
			c := connect(t, cfg, mqttclient.Will{Topic: "t/will"}, nil)
			if err := c.Publish(t.Context(), "t/"+name, []byte("ok"), false); err != nil {
				t.Fatalf("Publish over TLS: %v", err)
			}
			if _, ok := sub.WaitFor(5*time.Second, topicPayload("t/"+name, "ok")); !ok {
				t.Fatal("message over TLS not delivered")
			}
		})
	}

	t.Run("unverified", func(t *testing.T) {
		c, err := mqttclient.Connect(mqttclient.Config{URL: b.TLSURL, ClientID: "tls-none", PublishTimeout: time.Second},
			mqttclient.Will{Topic: "t/will"}, nil, discard())
		if err != nil {
			t.Fatalf("Connect: %v", err)
		}
		t.Cleanup(c.Disconnect)
		time.Sleep(3 * time.Second)
		if c.Connected() {
			t.Fatal("connected to a self-signed broker without its CA or TLSInsecure")
		}
	})
}

func connect(t *testing.T, cfg mqttclient.Config, will mqttclient.Will, onConnect func()) *mqttclient.Client {
	t.Helper()
	cfg.PublishTimeout = 5 * time.Second
	c, err := mqttclient.Connect(cfg, will, onConnect, discard())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(c.Disconnect)
	waitFor(t, "connection", c.Connected)
	return c
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func topicPayload(topic, payload string) func(testbroker.Message) bool {
	return func(m testbroker.Message) bool { return m.Topic == topic && m.Payload == payload }
}

func discard() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

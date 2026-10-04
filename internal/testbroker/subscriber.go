//go:build integration

package testbroker

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

var subscribers atomic.Uint64

// Message is one message a Subscriber received.
type Message struct {
	// Topic is the topic it arrived on.
	Topic string
	// Payload is its body.
	Payload string
	// Retained reports whether the broker delivered it from its retained store.
	Retained bool
}

// Subscriber records every message on a topic filter.
type Subscriber struct {
	url  string
	mu   sync.Mutex
	msgs []Message
	news chan struct{}
}

// Subscribe connects a fresh client to brokerURL and subscribes to filter, failing the test if either step fails.
func Subscribe(t *testing.T, brokerURL, filter string) *Subscriber {
	t.Helper()
	s := &Subscriber{url: brokerURL, news: make(chan struct{}, 1)}
	// A shared client id would make the broker kick the older subscriber off.
	id := fmt.Sprintf("test-subscriber-%d", subscribers.Add(1))
	c := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(brokerURL).SetClientID(id))
	if tok := c.Connect(); !tok.WaitTimeout(10*time.Second) || tok.Error() != nil {
		t.Fatalf("subscriber connect: %v", tok.Error())
	}
	t.Cleanup(func() { c.Disconnect(100) })
	tok := c.Subscribe(filter, 1, func(_ mqtt.Client, m mqtt.Message) {
		s.mu.Lock()
		s.msgs = append(s.msgs, Message{Topic: m.Topic(), Payload: string(m.Payload()), Retained: m.Retained()})
		s.mu.Unlock()
		select {
		case s.news <- struct{}{}:
		default:
		}
	})
	if !tok.WaitTimeout(10*time.Second) || tok.Error() != nil {
		t.Fatalf("subscribe %s: %v", filter, tok.Error())
	}
	return s
}

// URL returns the broker the subscriber is connected to.
func (s *Subscriber) URL() string {
	return s.url
}

// Messages returns everything received so far.
func (s *Subscriber) Messages() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Message(nil), s.msgs...)
}

// WaitFor returns the first message that satisfies match, waiting up to timeout for one to arrive.
func (s *Subscriber) WaitFor(timeout time.Duration, match func(Message) bool) (Message, bool) {
	deadline := time.After(timeout)
	for {
		for _, m := range s.Messages() {
			if match(m) {
				return m, true
			}
		}
		select {
		case <-s.news:
		case <-deadline:
			return Message{}, false
		}
	}
}

// Last returns the most recent message on topic, if any.
func (s *Subscriber) Last(topic string) (Message, bool) {
	msgs := s.Messages()
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Topic == topic {
			return msgs[i], true
		}
	}
	return Message{}, false
}

// Reset forgets every message received so far.
func (s *Subscriber) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msgs = nil
}

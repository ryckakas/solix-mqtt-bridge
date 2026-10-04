package mqttout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/ryckakas/solix-mqtt-bridge/internal/bridge"
	"github.com/ryckakas/solix-mqtt-bridge/internal/mqttclient"
)

// Config configures the generic output.
type Config struct {
	// MQTT is the broker connection; the output owns it exclusively.
	MQTT mqttclient.Config
	// BaseTopic prefixes every topic, e.g. "solix-mqtt-bridge" or "others/solix-mqtt-bridge" on an openWB broker.
	BaseTopic string
}

// Output is the generic MQTT output. Its Last Will marks the availability topic "offline".
type Output struct {
	base   string
	client *mqttclient.Client
	logger *slog.Logger

	mu        sync.Mutex
	last      bridge.State
	published bool
}

// New connects to the broker in the background and returns the output; nothing is published before the first
// Publish.
func New(cfg Config, logger *slog.Logger) (*Output, error) {
	o := &Output{base: cfg.BaseTopic, logger: logger}
	will := mqttclient.Will{Topic: o.topic("availability"), Payload: []byte(availabilityOffline), Retained: true}
	client, err := mqttclient.Connect(cfg.MQTT, will, o.republish, logger)
	if err != nil {
		return nil, fmt.Errorf("generic mqtt output: %w", err)
	}
	o.client = client
	return o, nil
}

// Publish sends availability, the state document and, while fresh, the per-field topics, all retained. While the
// broker connection is down it drops the update; the next (re)connect republishes the latest state.
func (o *Output) Publish(ctx context.Context, s bridge.State) error {
	o.mu.Lock()
	o.last, o.published = s, true
	o.mu.Unlock()
	return o.send(ctx, s)
}

// Close marks the output offline and disconnects cleanly.
func (o *Output) Close(ctx context.Context) error {
	err := o.client.Publish(ctx, o.topic("availability"), []byte(availabilityOffline), true)
	o.client.Disconnect()
	if err != nil && !errors.Is(err, mqttclient.ErrNotConnected) {
		return fmt.Errorf("generic mqtt output: mark offline: %w", err)
	}
	return nil
}

func (o *Output) send(ctx context.Context, s bridge.State) error {
	doc, err := json.Marshal(buildStateDoc(s))
	if err != nil {
		return fmt.Errorf("generic mqtt output: encode state: %w", err)
	}
	fields := fieldValues(s)
	msgs := make([]field, 0, 2+len(fields))
	msgs = append(msgs, field{"availability", availability(s)}, field{"state", string(doc)})
	msgs = append(msgs, fields...)
	for _, m := range msgs {
		err := o.client.Publish(ctx, o.topic(m.name), []byte(m.value), true)
		if errors.Is(err, mqttclient.ErrNotConnected) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("generic mqtt output: %w", err)
		}
	}
	return nil
}

// Connected reports whether the broker connection is currently up.
func (o *Output) Connected() bool {
	return o.client.Connected()
}

func (o *Output) republish() {
	o.mu.Lock()
	s, published := o.last, o.published
	o.mu.Unlock()
	if !published {
		return
	}
	if err := o.send(context.Background(), s); err != nil {
		o.logger.Warn("republish after reconnect failed", "err", err)
	}
}

func (o *Output) topic(name string) string {
	return o.base + "/" + name
}

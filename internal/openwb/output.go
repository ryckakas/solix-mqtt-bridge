// Package openwb is the openWB 2.x output: it feeds openWB's generic MQTT battery module. openWB keeps the last
// battery value forever, so this output neutralizes it itself with power 0 when the data goes stale, as its Last
// Will, and on shutdown.
package openwb

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"

	"github.com/ryckakas/solix-mqtt-bridge/internal/bridge"
	"github.com/ryckakas/solix-mqtt-bridge/internal/mqttclient"
)

// Config configures the openWB output.
type Config struct {
	// MQTT is the connection to openWB's broker; the output owns it exclusively.
	MQTT mqttclient.Config
	// BatteryID is the component id of the generic MQTT battery configured in openWB.
	BatteryID int
	// PublishCounters adds lifetime imported/exported energy. openWB's peak filter rejects the device's 100 Wh
	// steps while "Maximale Leistung des Speichers" is set, so leave it off unless that setting is 0.
	PublishCounters bool
}

// Output is the openWB output. Values are published non-retained: openWB validates them and stores them retained
// itself.
type Output struct {
	cfg    Config
	client *mqttclient.Client
	logger *slog.Logger

	mu        sync.Mutex
	last      bridge.State
	published bool
}

type message struct {
	field string
	value string
}

// New connects to openWB's broker in the background and returns the output.
func New(cfg Config, logger *slog.Logger) (*Output, error) {
	o := &Output{cfg: cfg, logger: logger}
	will := mqttclient.Will{Topic: o.topic("power"), Payload: []byte("0")}
	client, err := mqttclient.Connect(cfg.MQTT, will, o.republish, logger)
	if err != nil {
		return nil, fmt.Errorf("openWB output: %w", err)
	}
	o.client = client
	return o, nil
}

// Publish sends power and state of charge while the data is fresh, and power 0 otherwise. While the broker
// connection is down it drops the update; the next (re)connect republishes the latest state.
func (o *Output) Publish(ctx context.Context, s bridge.State) error {
	o.mu.Lock()
	o.last, o.published = s, true
	o.mu.Unlock()
	return o.send(ctx, messages(s, o.cfg.PublishCounters))
}

// Close publishes power 0, so openWB stops counting the battery, and disconnects cleanly.
func (o *Output) Close(ctx context.Context) error {
	err := o.client.Publish(ctx, o.topic("power"), []byte("0"), false)
	o.client.Disconnect()
	if err != nil && !errors.Is(err, mqttclient.ErrNotConnected) {
		return fmt.Errorf("openWB output: neutralize power: %w", err)
	}
	return nil
}

// Connected reports whether the broker connection is currently up.
func (o *Output) Connected() bool {
	return o.client.Connected()
}

func messages(s bridge.State, counters bool) []message {
	if !s.Fresh {
		return []message{{"power", "0"}}
	}
	msgs := []message{
		{"power", strconv.FormatInt(s.Reading.ChargePowerW, 10)},
		{"soc", strconv.FormatUint(uint64(s.Reading.SoCPercent), 10)},
	}
	if counters && s.Reading.CountersValid {
		msgs = append(msgs,
			message{"imported", strconv.FormatUint(s.Reading.ChargedTotalWh, 10)},
			message{"exported", strconv.FormatUint(s.Reading.DischargedTotalWh, 10)})
	}
	return msgs
}

func (o *Output) send(ctx context.Context, msgs []message) error {
	for _, m := range msgs {
		err := o.client.Publish(ctx, o.topic(m.field), []byte(m.value), false)
		if errors.Is(err, mqttclient.ErrNotConnected) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("openWB output: %w", err)
		}
	}
	return nil
}

func (o *Output) republish() {
	o.mu.Lock()
	s, published := o.last, o.published
	o.mu.Unlock()
	if !published {
		return
	}
	if err := o.send(context.Background(), messages(s, o.cfg.PublishCounters)); err != nil {
		o.logger.Warn("republish after reconnect failed", "err", err)
	}
}

func (o *Output) topic(field string) string {
	return fmt.Sprintf("openWB/set/mqtt/bat/%d/get/%s", o.cfg.BatteryID, field)
}

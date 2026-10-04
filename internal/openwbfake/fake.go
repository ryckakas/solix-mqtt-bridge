package openwbfake

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// Fake subscribes to a broker and judges every generic MQTT battery message with Validate. Unlike openWB it doesn't
// store accepted values: openWB does that internally, with rights its public ACL doesn't grant.
type Fake struct {
	client mqtt.Client
	logger *slog.Logger

	mu       sync.Mutex
	verdicts []Verdict
}

// Start connects to brokerURL and subscribes to openWB/set/mqtt/bat/+/get/+.
func Start(brokerURL, clientID string, logger *slog.Logger) (*Fake, error) {
	f := &Fake{logger: logger}
	f.client = mqtt.NewClient(mqtt.NewClientOptions().AddBroker(brokerURL).SetClientID(clientID).
		SetAutoReconnect(true).SetConnectRetry(true).SetOnConnectHandler(f.subscribe))
	t := f.client.Connect()
	if !t.WaitTimeout(15 * time.Second) {
		return nil, fmt.Errorf("fake openWB connect to %s: timed out", brokerURL)
	}
	if err := t.Error(); err != nil {
		return nil, fmt.Errorf("fake openWB connect to %s: %w", brokerURL, err)
	}
	return f, nil
}

// Verdicts returns every verdict so far, oldest first.
func (f *Fake) Verdicts() []Verdict {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Verdict(nil), f.verdicts...)
}

// Close disconnects from the broker.
func (f *Fake) Close() {
	f.client.Disconnect(100)
}

func (f *Fake) subscribe(c mqtt.Client) {
	t := c.Subscribe("openWB/set/mqtt/bat/+/get/+", 1, f.handle)
	if !t.WaitTimeout(10*time.Second) || t.Error() != nil {
		f.logger.Error("fake openWB subscribe failed", "err", t.Error())
	}
}

func (f *Fake) handle(_ mqtt.Client, m mqtt.Message) {
	v := Validate(m.Topic(), m.Payload(), m.Retained())
	f.mu.Lock()
	f.verdicts = append(f.verdicts, v)
	f.mu.Unlock()
	if !v.Accepted {
		f.logger.Warn("openWB would reject", "topic", v.Topic, "payload", v.Payload, "reason", v.Reason)
		return
	}
	f.logger.Info("openWB accepts", "battery", v.BatteryID, "field", v.Field, "value", v.Payload, "retained", v.Retained)
}

//go:build integration

package mqttout_test

import (
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/ryckakas/solix-mqtt-bridge/internal/bridge"
	"github.com/ryckakas/solix-mqtt-bridge/internal/mqttclient"
	"github.com/ryckakas/solix-mqtt-bridge/internal/mqttout"
	"github.com/ryckakas/solix-mqtt-bridge/internal/plausibility"
	"github.com/ryckakas/solix-mqtt-bridge/internal/solarbank"
	"github.com/ryckakas/solix-mqtt-bridge/internal/testbroker"
)

const wait = 10 * time.Second

func TestFreshThenStaleThenClose(t *testing.T) {
	b := testbroker.Start(t, testbroker.Options{})
	sub := testbroker.Subscribe(t, b.URL, "solix/#")
	out := newOutput(t, b.URL, "solix")

	publish(t, out, state(true))
	expect(t, sub, "solix/availability", "online", true)
	expect(t, sub, "solix/charge_power_w", "1500", true)
	expect(t, sub, "solix/soc_percent", "63", true)
	if doc := stateDoc(t, sub); doc["status"] != "fresh" {
		t.Errorf("state.status = %v, want fresh", doc["status"])
	}

	sub.Reset()
	publish(t, out, state(false))
	expect(t, sub, "solix/availability", "offline", true)
	if doc := stateDoc(t, sub); doc["status"] != "stale" {
		t.Errorf("state.status = %v, want stale", doc["status"])
	}
	if m, ok := sub.Last("solix/charge_power_w"); ok {
		t.Errorf("per-field topic updated while stale: %+v", m)
	}

	sub.Reset()
	if err := out.Close(t.Context()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	expect(t, sub, "solix/availability", "offline", true)
}

func TestWillMarksOfflineWhenTheNetworkFails(t *testing.T) {
	b := testbroker.Start(t, testbroker.Options{})
	proxy := testbroker.NewProxy(t, testbroker.HostPort(b.URL))
	sub := testbroker.Subscribe(t, b.URL, "solix/#")
	out := newOutput(t, proxy.URL(), "solix")
	publish(t, out, state(true))
	expect(t, sub, "solix/availability", "online", true)

	sub.Reset()
	proxy.Cut()
	expect(t, sub, "solix/availability", "offline", true)
}

func TestReconnectRepublishesTheLatestState(t *testing.T) {
	b := testbroker.Start(t, testbroker.Options{})
	proxy := testbroker.NewProxy(t, testbroker.HostPort(b.URL))
	sub := testbroker.Subscribe(t, b.URL, "solix/#")
	out := newOutput(t, proxy.URL(), "solix")
	publish(t, out, state(true))
	expect(t, sub, "solix/availability", "online", true)

	proxy.Cut()
	expect(t, sub, "solix/availability", "offline", true)
	sub.Reset()
	proxy.Restore(t)
	if _, ok := sub.WaitFor(40*time.Second, match("solix/availability", "online")); !ok {
		t.Fatalf("availability not republished as online after reconnect; got %v", sub.Messages())
	}
}

func TestWorksUnderTheOpenWBACLWithAnOthersBaseTopic(t *testing.T) {
	b := testbroker.Start(t, testbroker.Options{ACL: testbroker.OpenWBACL})
	sub := testbroker.Subscribe(t, b.URL, "others/#")
	out := newOutput(t, b.URL, "others/solix-mqtt-bridge")
	publish(t, out, state(true))
	expect(t, sub, "others/solix-mqtt-bridge/availability", "online", true)
	expect(t, sub, "others/solix-mqtt-bridge/charge_power_w", "1500", true)
}

func state(fresh bool) bridge.State {
	at := time.Now()
	return bridge.State{
		Identity:     solarbank.Identity{Model: "A17E2", Serial: "SN1", Firmware: "v1"},
		Fresh:        fresh,
		LastGoodRead: at,
		Raw:          solarbank.Snapshot{At: at, Status: solarbank.StatusCharging, BatteryPowerW: -1500, SoCPercent: 63},
		Reading:      plausibility.Reading{At: at, ChargePowerW: 1500, SoCPercent: 63},
	}
}

func newOutput(t *testing.T, url, base string) *mqttout.Output {
	t.Helper()
	out, err := mqttout.New(mqttout.Config{
		MQTT:      mqttclient.Config{URL: url, ClientID: "generic-" + t.Name(), PublishTimeout: 5 * time.Second},
		BaseTopic: base,
	}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = out.Close(t.Context()) })
	deadline := time.Now().Add(wait)
	for !out.Connected() {
		if time.Now().After(deadline) {
			t.Fatal("output never connected")
		}
		time.Sleep(50 * time.Millisecond)
	}
	return out
}

func publish(t *testing.T, out *mqttout.Output, s bridge.State) {
	t.Helper()
	if err := out.Publish(t.Context(), s); err != nil {
		t.Fatalf("Publish: %v", err)
	}
}

func expect(t *testing.T, sub *testbroker.Subscriber, topic, payload string, retained bool) {
	t.Helper()
	if _, ok := sub.WaitFor(wait, match(topic, payload)); !ok {
		t.Fatalf("%s=%s not received; got %v", topic, payload, sub.Messages())
	}
	late := testbroker.Subscribe(t, sub.URL(), topic)
	if lm, ok := late.WaitFor(wait, func(testbroker.Message) bool { return true }); !ok || lm.Retained != retained {
		t.Errorf("%s: late subscriber got %+v (found %v), want retained=%v", topic, lm, ok, retained)
	}
}

func stateDoc(t *testing.T, sub *testbroker.Subscriber) map[string]any {
	t.Helper()
	m, ok := sub.WaitFor(wait, func(m testbroker.Message) bool { return m.Topic == "solix/state" })
	if !ok {
		t.Fatalf("no state document; got %v", sub.Messages())
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(m.Payload), &doc); err != nil {
		t.Fatalf("state document is not JSON: %v (%q)", err, m.Payload)
	}
	return doc
}

func match(topic, payload string) func(testbroker.Message) bool {
	return func(m testbroker.Message) bool { return m.Topic == topic && m.Payload == payload }
}

//go:build integration

package openwb_test

import (
	"log/slog"
	"testing"
	"time"

	"github.com/ryckakas/solix-mqtt-bridge/internal/bridge"
	"github.com/ryckakas/solix-mqtt-bridge/internal/mqttclient"
	"github.com/ryckakas/solix-mqtt-bridge/internal/openwb"
	"github.com/ryckakas/solix-mqtt-bridge/internal/openwbfake"
	"github.com/ryckakas/solix-mqtt-bridge/internal/plausibility"
	"github.com/ryckakas/solix-mqtt-bridge/internal/testbroker"
)

const wait = 10 * time.Second

func TestFreshStaleAndCloseAsOpenWBSeesThem(t *testing.T) {
	b := testbroker.Start(t, testbroker.Options{ACL: testbroker.OpenWBACL})
	fake := startFake(t, b.URL)
	out := newOutput(t, b.URL, false)

	publish(t, out, fresh())
	expectVerdict(t, fake, "power", "1500")
	expectVerdict(t, fake, "soc", "63")

	publish(t, out, bridge.State{})
	expectVerdict(t, fake, "power", "0")

	if err := out.Close(t.Context()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	waitVerdicts(t, fake, 4)
	assertAllAcceptedAndNotRetained(t, fake)
	for _, v := range fake.Verdicts() {
		if v.Field == "imported" || v.Field == "exported" {
			t.Errorf("counter published without PublishCounters: %+v", v)
		}
	}
}

func TestCountersWhenEnabled(t *testing.T) {
	b := testbroker.Start(t, testbroker.Options{ACL: testbroker.OpenWBACL})
	fake := startFake(t, b.URL)
	out := newOutput(t, b.URL, true)

	publish(t, out, fresh())
	expectVerdict(t, fake, "imported", "1234500")
	expectVerdict(t, fake, "exported", "1100000")
	assertAllAcceptedAndNotRetained(t, fake)
}

func TestWillNeutralizesPowerWhenTheNetworkFails(t *testing.T) {
	b := testbroker.Start(t, testbroker.Options{ACL: testbroker.OpenWBACL})
	proxy := testbroker.NewProxy(t, testbroker.HostPort(b.URL))
	fake := startFake(t, b.URL)
	out := newOutput(t, proxy.URL(), false)
	publish(t, out, fresh())
	expectVerdict(t, fake, "power", "1500")

	proxy.Cut()
	expectVerdict(t, fake, "power", "0")
	assertAllAcceptedAndNotRetained(t, fake)
}

func fresh() bridge.State {
	return bridge.State{Fresh: true, LastGoodRead: time.Now(), Reading: plausibility.Reading{
		ChargePowerW: 1500, SoCPercent: 63, CountersValid: true, ChargedTotalWh: 1_234_500, DischargedTotalWh: 1_100_000,
	}}
}

func startFake(t *testing.T, url string) *openwbfake.Fake {
	t.Helper()
	f, err := openwbfake.Start(url, "fake-openwb-"+t.Name(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("start fake openWB: %v", err)
	}
	t.Cleanup(f.Close)
	return f
}

func newOutput(t *testing.T, url string, counters bool) *openwb.Output {
	t.Helper()
	out, err := openwb.New(openwb.Config{
		MQTT:            mqttclient.Config{URL: url, ClientID: "openwb-" + t.Name(), PublishTimeout: 5 * time.Second},
		BatteryID:       3,
		PublishCounters: counters,
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

func publish(t *testing.T, out *openwb.Output, s bridge.State) {
	t.Helper()
	if err := out.Publish(t.Context(), s); err != nil {
		t.Fatalf("Publish: %v", err)
	}
}

func expectVerdict(t *testing.T, fake *openwbfake.Fake, field, payload string) {
	t.Helper()
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		for _, v := range fake.Verdicts() {
			if v.Field == field && v.Payload == payload {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("fake openWB never saw %s=%s; verdicts %+v", field, payload, fake.Verdicts())
}

func waitVerdicts(t *testing.T, fake *openwbfake.Fake, n int) {
	t.Helper()
	deadline := time.Now().Add(wait)
	for len(fake.Verdicts()) < n {
		if time.Now().After(deadline) {
			t.Fatalf("got %d verdicts, want at least %d", len(fake.Verdicts()), n)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func assertAllAcceptedAndNotRetained(t *testing.T, fake *openwbfake.Fake) {
	t.Helper()
	for _, v := range fake.Verdicts() {
		if !v.Accepted || v.Retained || v.BatteryID != 3 {
			t.Errorf("verdict %+v: want accepted, not retained, battery 3", v)
		}
	}
}

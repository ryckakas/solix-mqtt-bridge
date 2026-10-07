//go:build integration

package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ryckakas/solix-mqtt-bridge/internal/config"
	"github.com/ryckakas/solix-mqtt-bridge/internal/openwbfake"
	"github.com/ryckakas/solix-mqtt-bridge/internal/simulator"
	"github.com/ryckakas/solix-mqtt-bridge/internal/solarbank"
	"github.com/ryckakas/solix-mqtt-bridge/internal/testbroker"
)

const wait = 15 * time.Second

// The whole pipeline against stand-ins: simulator -> bridge -> mosquitto with openWB's ACL -> fake openWB and a
// generic subscriber, through a device outage, its recovery and a graceful shutdown, with file logging on.
func TestEndToEnd(t *testing.T) {
	dev := simulator.NewDevice(1, simulator.DefaultState())
	dev.Update(func(s *simulator.State) { s.Status, s.BatteryPowerW, s.SoCPercent = 1, -1500, 63 })
	sim, err := simulator.Listen(t.Context(), dev, "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("start simulator: %v", err)
	}
	t.Cleanup(func() { _ = sim.Stop() })

	b := testbroker.Start(t, testbroker.Options{ACL: testbroker.OpenWBACL})
	fake, err := openwbfake.Start(b.URL, "fake-openwb", slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("start fake openWB: %v", err)
	}
	t.Cleanup(fake.Close)
	generic := testbroker.Subscribe(t, b.URL, "others/#")

	logDir := t.TempDir()
	cfg, err := config.Load(env(map[string]string{
		"SOLARBANK_ADDR":  sim.Addr(),
		"POLL_INTERVAL":   "200ms",
		"STALE_AFTER":     "1s",
		"MQTT_URL":        b.URL,
		"MQTT_BASE_TOPIC": "others/solix-mqtt-bridge",
		"OPENWB_MQTT_URL": b.URL,
		"OPENWB_BAT_ID":   "7",
		"LOG_DIR":         logDir,
	}), false)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan int, 1)
	logger, closeLog := newLogger(cfg, io.Discard, true)
	go func() { done <- serve(ctx, cfg, solarbank.NewReader(cfg.Solarbank), logger) }()

	expectOpenWB(t, fake, "power", "1500")
	expectOpenWB(t, fake, "soc", "63")
	expectGeneric(t, generic, "availability", "online")
	expectGeneric(t, generic, "charge_power_w", "1500")

	if err := sim.Stop(); err != nil {
		t.Fatalf("stop simulator: %v", err)
	}
	expectOpenWB(t, fake, "power", "0")
	expectGeneric(t, generic, "availability", "offline")

	generic.Reset()
	if err := sim.Start(); err != nil {
		t.Fatalf("restart simulator: %v", err)
	}
	expectGeneric(t, generic, "availability", "online")

	generic.Reset()
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("serve exit code = %d, want 0", code)
		}
	case <-time.After(wait):
		t.Fatal("serve did not stop after cancel")
	}
	expectGeneric(t, generic, "availability", "offline")

	for _, v := range fake.Verdicts() {
		if !v.Accepted || v.Retained || v.BatteryID != 7 {
			t.Errorf("openWB verdict %+v: want accepted, not retained, battery 7", v)
		}
	}
	if got := dev.UnexpectedAccesses(); len(got) != 0 {
		t.Errorf("the bridge sent non-FC04 requests to the Solarbank: %v", got)
	}

	closeLog()
	expectLogged(t, logDir, `msg="solix-mqtt-bridge starting"`, "msg=poll ", `msg="solix-mqtt-bridge stopped"`)
}

// Reads every day's file, so a run across midnight still passes.
func expectLogged(t *testing.T, dir string, wants ...string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "solix-mqtt-bridge-*.log"))
	if err != nil || len(files) == 0 {
		t.Fatalf("log files in %s: %v (err %v), want at least one", dir, files, err)
	}
	var logs strings.Builder
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		logs.Write(b)
	}
	for _, want := range wants {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("log files lack %s:\n%s", want, logs.String())
		}
	}
}

func expectOpenWB(t *testing.T, fake *openwbfake.Fake, field, payload string) {
	t.Helper()
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		vs := fake.Verdicts()
		for i := len(vs) - 1; i >= 0; i-- {
			if vs[i].Field == field {
				if vs[i].Payload == payload {
					return
				}
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("openWB never got %s=%s; verdicts %+v", field, payload, fake.Verdicts())
}

func expectGeneric(t *testing.T, sub *testbroker.Subscriber, field, payload string) {
	t.Helper()
	topic := "others/solix-mqtt-bridge/" + field
	if _, ok := sub.WaitFor(wait, func(m testbroker.Message) bool { return m.Topic == topic && m.Payload == payload }); !ok {
		t.Fatalf("generic output never published %s=%s; got %v", topic, payload, sub.Messages())
	}
}

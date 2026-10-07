package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ryckakas/solix-mqtt-bridge/internal/config"
	"github.com/ryckakas/solix-mqtt-bridge/internal/simulator"
)

func env(kv map[string]string) config.LookupFunc {
	return func(k string) (string, bool) {
		v, ok := kv[k]
		return v, ok
	}
}

func TestVersionFlag(t *testing.T) {
	var out bytes.Buffer
	if code := run([]string{"-version"}, env(nil), &out, &bytes.Buffer{}); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.HasPrefix(out.String(), "solix-mqtt-bridge dev") {
		t.Errorf("output = %q, want the version line", out.String())
	}
}

func TestInvalidConfigurationExitsWithTwo(t *testing.T) {
	var stderr bytes.Buffer
	if code := run(nil, env(nil), &bytes.Buffer{}, &stderr); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "SOLARBANK_ADDR is required") {
		t.Errorf("stderr = %q, want the missing setting named", stderr.String())
	}
}

func TestUnknownFlagExitsWithTwo(t *testing.T) {
	if code := run([]string{"-bogus"}, env(nil), &bytes.Buffer{}, &bytes.Buffer{}); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}

func TestProbePrintsOneSnapshotAsJSON(t *testing.T) {
	dev := simulator.NewDevice(1, simulator.DefaultState())
	dev.Update(func(s *simulator.State) { s.Status, s.BatteryPowerW, s.SoCPercent = 1, -1500, 63 })
	srv, err := simulator.Listen(t.Context(), dev, "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("start simulator: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })

	var stdout, stderr bytes.Buffer
	code := run([]string{"-probe"}, env(map[string]string{"SOLARBANK_ADDR": srv.Addr()}), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr: %s", code, stderr.String())
	}
	var doc map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("probe output is not JSON: %v\n%s", err, stdout.String())
	}
	for key, want := range map[string]any{
		"model": "A17E2", "status": "charging", "battery_power_w": -1500.0, "charge_power_w": 1500.0, "soc_percent": 63.0,
	} {
		if doc[key] != want {
			t.Errorf("%s = %v, want %v", key, doc[key], want)
		}
	}
	if got := dev.UnexpectedAccesses(); len(got) != 0 {
		t.Errorf("probe sent non-FC04 requests: %v", got)
	}
}

func TestProbeWritesNoLogFile(t *testing.T) {
	srv, err := simulator.Listen(t.Context(), simulator.NewDevice(1, simulator.DefaultState()), "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("start simulator: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })

	dir := filepath.Join(t.TempDir(), "logs")
	var stderr bytes.Buffer
	code := run([]string{"-probe"}, env(map[string]string{"SOLARBANK_ADDR": srv.Addr(), "LOG_DIR": dir}),
		&bytes.Buffer{}, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr: %s", code, stderr.String())
	}
	if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("stat %s: err = %v, want the log dir never created in probe mode", dir, err)
	}
}

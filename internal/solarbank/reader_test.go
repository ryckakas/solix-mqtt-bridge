package solarbank_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ryckakas/solix-mqtt-bridge/internal/simulator"
	"github.com/ryckakas/solix-mqtt-bridge/internal/solarbank"
)

func TestReaderReadsIdentityAndSnapshot(t *testing.T) {
	dev, srv := startDevice(t, simulator.DefaultState())
	dev.Update(func(s *simulator.State) {
		s.Status = 1
		s.BatteryPowerW = -1500
		s.SoCPercent = 63
		s.GridPowerW = -120
		s.MaxChargePowerW = -2400
		s.ChargedTotalWh = 1_234_567
		s.DischargedTotalWh = 1_100_099
	})
	r := newReader(t, srv, solarbank.Config{})

	id, err := r.Connect(t.Context())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	want := solarbank.Identity{Model: "A17E2", Serial: "APZ1DMWH0000000001", Firmware: "v1.0.1.14"}
	if id != want {
		t.Errorf("identity = %+v, want %+v", id, want)
	}

	snap, err := r.Read(t.Context())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if snap.Status != solarbank.StatusCharging || snap.BatteryPowerW != -1500 || snap.ChargePowerW() != 1500 {
		t.Errorf("status/power = %v/%d/%d, want charging/-1500/1500", snap.Status, snap.BatteryPowerW, snap.ChargePowerW())
	}
	if snap.SoCPercent != 63 || snap.GridPowerW != -120 {
		t.Errorf("soc/grid = %d/%d, want 63/-120", snap.SoCPercent, snap.GridPowerW)
	}
	if snap.MaxChargePowerW != 2400 || snap.MaxDischargePowerW != 2500 || snap.CapacityWh != 5000 {
		t.Errorf("limits/capacity = %d/%d/%d, want 2400/2500/5000",
			snap.MaxChargePowerW, snap.MaxDischargePowerW, snap.CapacityWh)
	}
	if snap.ChargedTotalWh != 1_234_500 || snap.DischargedTotalWh != 1_100_000 {
		t.Errorf("totals = %d/%d, want 1234500/1100000 (100 Wh steps)", snap.ChargedTotalWh, snap.DischargedTotalWh)
	}
	if time.Since(snap.At) > time.Minute {
		t.Errorf("snapshot stamped %v, want about now", snap.At)
	}
}

func TestConnectRefusesOtherModelsUnlessAllowed(t *testing.T) {
	other := simulator.DefaultState()
	other.Model = "A17X8"
	_, srv := startDevice(t, other)

	_, err := newReader(t, srv, solarbank.Config{}).Connect(t.Context())
	if !errors.Is(err, solarbank.ErrUnsupportedModel) {
		t.Fatalf("Connect to an A17X8: err = %v, want ErrUnsupportedModel", err)
	}

	id, err := newReader(t, srv, solarbank.Config{AllowAnyModel: true}).Connect(t.Context())
	if err != nil {
		t.Fatalf("Connect with AllowAnyModel: %v", err)
	}
	if id.Model != "A17X8" {
		t.Errorf("model = %q, want A17X8", id.Model)
	}
}

func TestConnectUsesTheConfiguredUnitID(t *testing.T) {
	_, srv := startDevice(t, simulator.DefaultState())
	if _, err := newReader(t, srv, solarbank.Config{UnitID: 7}).Connect(t.Context()); err == nil {
		t.Fatal("Connect with unit id 7 to a unit-1 device: want error, got nil")
	}
}

func TestReaderRecoversAfterAnOutage(t *testing.T) {
	_, srv := startDevice(t, simulator.DefaultState())
	r := newReader(t, srv, solarbank.Config{})
	if _, err := r.Connect(t.Context()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if _, err := r.Read(t.Context()); err != nil {
		t.Fatalf("Read before outage: %v", err)
	}

	if err := srv.Stop(); err != nil {
		t.Fatalf("stop simulator: %v", err)
	}
	if _, err := r.Read(t.Context()); err == nil {
		t.Fatal("Read during outage: want error, got nil")
	}
	_ = r.Close()

	if err := srv.Start(); err != nil {
		t.Fatalf("restart simulator: %v", err)
	}
	if _, err := r.Connect(t.Context()); err != nil {
		t.Fatalf("Connect after outage: %v", err)
	}
	if _, err := r.Read(t.Context()); err != nil {
		t.Fatalf("Read after outage: %v", err)
	}
}

func TestConnectFailsFastWhenNothingListens(t *testing.T) {
	_, srv := startDevice(t, simulator.DefaultState())
	if err := srv.Stop(); err != nil {
		t.Fatalf("stop simulator: %v", err)
	}
	start := time.Now()
	if _, err := newReader(t, srv, solarbank.Config{}).Connect(t.Context()); err == nil {
		t.Fatal("Connect with nothing listening: want error, got nil")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("Connect took %v, want it bounded by the 1 s timeout", elapsed)
	}
}

// Every test's device must have seen nothing but FC04 reads: the bridge is read-only towards the Solarbank.
func startDevice(t *testing.T, initial simulator.State) (*simulator.Device, *simulator.Server) {
	t.Helper()
	dev := simulator.NewDevice(1, initial)
	srv, err := simulator.Listen(t.Context(), dev, "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("start simulator: %v", err)
	}
	t.Cleanup(func() {
		_ = srv.Stop()
		if got := dev.UnexpectedAccesses(); len(got) != 0 {
			t.Errorf("the reader sent non-FC04 requests: %v", got)
		}
	})
	return dev, srv
}

func newReader(t *testing.T, srv *simulator.Server, cfg solarbank.Config) *solarbank.Reader {
	t.Helper()
	cfg.Addr = srv.Addr()
	if cfg.UnitID == 0 {
		cfg.UnitID = 1
	}
	cfg.Timeout = time.Second
	r := solarbank.NewReader(cfg)
	t.Cleanup(func() { _ = r.Close() })
	return r
}

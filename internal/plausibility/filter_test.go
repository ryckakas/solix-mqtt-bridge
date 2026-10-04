package plausibility_test

import (
	"testing"
	"time"

	"github.com/ryckakas/solix-mqtt-bridge/internal/plausibility"
	"github.com/ryckakas/solix-mqtt-bridge/internal/solarbank"
)

var t0 = time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)

func defaultFilter() *plausibility.Filter {
	return plausibility.New(plausibility.Config{SoCJumpConfirm: 10 * time.Minute, ZeroPowerHoldPolls: 2})
}

func snap(sec int, mutate func(*solarbank.Snapshot)) solarbank.Snapshot {
	s := solarbank.Snapshot{
		At:                 t0.Add(time.Duration(sec) * time.Second),
		Status:             solarbank.StatusCharging,
		BatteryPowerW:      -1000,
		SoCPercent:         50,
		MaxChargePowerW:    2500,
		MaxDischargePowerW: 2500,
		CapacityWh:         5000,
		ChargedTotalWh:     1_000_000,
		DischargedTotalWh:  900_000,
	}
	if mutate != nil {
		mutate(&s)
	}
	return s
}

func withSoC(v uint16) func(*solarbank.Snapshot) {
	return func(s *solarbank.Snapshot) { s.SoCPercent = v }
}

func TestFirstReadingPassesThrough(t *testing.T) {
	r := defaultFilter().Apply(snap(0, nil))
	if r.ChargePowerW != 1000 || r.SoCPercent != 50 {
		t.Errorf("power/soc = %d/%d, want 1000/50", r.ChargePowerW, r.SoCPercent)
	}
	if !r.CountersValid || r.ChargedTotalWh != 1_000_000 || r.DischargedTotalWh != 900_000 {
		t.Errorf("counters = %v %d/%d, want valid 1000000/900000", r.CountersValid, r.ChargedTotalWh, r.DischargedTotalWh)
	}
	if len(r.Rejections) != 0 {
		t.Errorf("rejections = %v, want none", r.Rejections)
	}
}

func TestZeroPowerWhileActiveIsHeldForConfiguredPolls(t *testing.T) {
	f := defaultFilter()
	f.Apply(snap(0, nil))
	zero := func(s *solarbank.Snapshot) { s.BatteryPowerW = 0 }

	for i, want := range []int64{1000, 1000, 0} {
		r := f.Apply(snap(5*(i+1), zero))
		if r.ChargePowerW != want {
			t.Errorf("zero reading %d: power = %d, want %d", i+1, r.ChargePowerW, want)
		}
	}
	if got := f.Counts()[plausibility.ZeroPower]; got != 2 {
		t.Errorf("zero-power count = %d, want 2", got)
	}
}

func TestZeroPowerHoldResetsAfterARealReading(t *testing.T) {
	f := defaultFilter()
	f.Apply(snap(0, nil))
	zero := func(s *solarbank.Snapshot) { s.BatteryPowerW = 0 }
	f.Apply(snap(5, zero))
	f.Apply(snap(10, zero))
	f.Apply(snap(15, func(s *solarbank.Snapshot) { s.BatteryPowerW = -1200 }))
	if r := f.Apply(snap(20, zero)); r.ChargePowerW != 1200 {
		t.Errorf("zero after a real reading: power = %d, want the held 1200", r.ChargePowerW)
	}
}

func TestZeroPowerAcceptedWhenStatusIsNotActive(t *testing.T) {
	for _, status := range []solarbank.BatteryStatus{solarbank.StatusStandby, solarbank.StatusSleep} {
		f := defaultFilter()
		f.Apply(snap(0, nil))
		r := f.Apply(snap(5, func(s *solarbank.Snapshot) { s.BatteryPowerW, s.Status = 0, status }))
		if r.ChargePowerW != 0 || len(r.Rejections) != 0 {
			t.Errorf("status %v: power = %d rejections %v, want 0 and none", status, r.ChargePowerW, r.Rejections)
		}
	}
}

func TestSoCChangeWithinPhysicsIsAccepted(t *testing.T) {
	f := defaultFilter()
	f.Apply(snap(0, withSoC(50)))
	if r := f.Apply(snap(5, withSoC(51))); r.SoCPercent != 51 {
		t.Errorf("soc = %d, want 51", r.SoCPercent)
	}
}

// Replays the reported glitch: 85 % for five minutes while the battery really sat at 16 %.
func TestSoCGlitchShorterThanConfirmWindowIsHeld(t *testing.T) {
	f := defaultFilter()
	f.Apply(snap(0, withSoC(16)))
	for sec := 5; sec <= 300; sec += 5 {
		if r := f.Apply(snap(sec, withSoC(85))); r.SoCPercent != 16 {
			t.Fatalf("t=%ds: soc = %d, want the held 16", sec, r.SoCPercent)
		}
	}
	r := f.Apply(snap(305, withSoC(16)))
	if r.SoCPercent != 16 || len(r.Rejections) != 0 {
		t.Errorf("after the glitch: soc = %d rejections %v, want 16 and none", r.SoCPercent, r.Rejections)
	}
	if got := f.Counts()[plausibility.SoCJump]; got != 60 {
		t.Errorf("soc-jump count = %d, want 60", got)
	}
}

func TestSoCJumpPersistingForConfirmWindowIsAccepted(t *testing.T) {
	f := defaultFilter()
	f.Apply(snap(0, withSoC(50)))
	for sec := 5; sec < 605; sec += 5 {
		if r := f.Apply(snap(sec, withSoC(80))); r.SoCPercent != 50 {
			t.Fatalf("t=%ds: soc = %d, want the held 50", sec, r.SoCPercent)
		}
	}
	if r := f.Apply(snap(605, withSoC(80))); r.SoCPercent != 80 {
		t.Errorf("after 10 min at 80: soc = %d, want 80", r.SoCPercent)
	}
}

func TestSoCConfirmationRestartsOnADifferentLevel(t *testing.T) {
	f := defaultFilter()
	f.Apply(snap(0, withSoC(50)))
	f.Apply(snap(5, withSoC(80)))
	for sec := 10; sec <= 610; sec += 5 {
		level := uint16(20)
		if sec%10 == 0 {
			level = 80
		}
		if r := f.Apply(snap(sec, withSoC(level))); r.SoCPercent != 50 {
			t.Fatalf("t=%ds alternating 80/20: soc = %d, want the held 50", sec, r.SoCPercent)
		}
	}
}

func TestLongGapAllowsALargeSoCChange(t *testing.T) {
	f := defaultFilter()
	f.Apply(snap(0, withSoC(50)))
	if r := f.Apply(snap(2*3600, withSoC(90))); r.SoCPercent != 90 {
		t.Errorf("after a 2 h gap: soc = %d, want 90", r.SoCPercent)
	}
}

func TestSoCAbove100IsReplaced(t *testing.T) {
	f := defaultFilter()
	if r := f.Apply(snap(0, withSoC(120))); r.SoCPercent != 100 {
		t.Errorf("first reading 120: soc = %d, want 100", r.SoCPercent)
	}
	f = defaultFilter()
	f.Apply(snap(0, withSoC(60)))
	if r := f.Apply(snap(5, withSoC(120))); r.SoCPercent != 60 {
		t.Errorf("120 after 60: soc = %d, want 60", r.SoCPercent)
	}
	if got := f.Counts()[plausibility.SoCRange]; got != 1 {
		t.Errorf("soc-range count = %d, want 1", got)
	}
}

func TestSoCFallbackLimitWhenCapacityIsUnknown(t *testing.T) {
	unknown := func(v uint16) func(*solarbank.Snapshot) {
		return func(s *solarbank.Snapshot) { s.SoCPercent, s.CapacityWh = v, 0 }
	}
	f := defaultFilter()
	f.Apply(snap(0, unknown(50)))
	if r := f.Apply(snap(5, unknown(52))); r.SoCPercent != 52 {
		t.Errorf("+2 pp in 5 s: soc = %d, want 52", r.SoCPercent)
	}
	if r := f.Apply(snap(10, unknown(62))); r.SoCPercent != 52 {
		t.Errorf("+10 pp in 5 s: soc = %d, want the held 52", r.SoCPercent)
	}
}

func TestCountersRejectZeroAndBackwardsReadings(t *testing.T) {
	f := defaultFilter()
	f.Apply(snap(0, nil))
	tests := []struct {
		name                   string
		charged, discharged    uint64
		wantCharged, wantDisch uint64
		wantRejectedCharged    bool
		wantRejectedDischarged bool
	}{
		{"zero during firmware update", 0, 0, 1_000_000, 900_000, true, true},
		{"backwards", 999_900, 900_000, 1_000_000, 900_000, true, false},
		{"forwards", 1_000_100, 900_100, 1_000_100, 900_100, false, false},
	}
	for i, tt := range tests {
		r := f.Apply(snap(5*(i+1), func(s *solarbank.Snapshot) { s.ChargedTotalWh, s.DischargedTotalWh = tt.charged, tt.discharged }))
		if r.ChargedTotalWh != tt.wantCharged || r.DischargedTotalWh != tt.wantDisch || !r.CountersValid {
			t.Errorf("%s: counters = %v %d/%d, want valid %d/%d",
				tt.name, r.CountersValid, r.ChargedTotalWh, r.DischargedTotalWh, tt.wantCharged, tt.wantDisch)
		}
		if got := rejected(r, plausibility.ChargedTotal); got != tt.wantRejectedCharged {
			t.Errorf("%s: charged rejected = %v, want %v", tt.name, got, tt.wantRejectedCharged)
		}
		if got := rejected(r, plausibility.DischargedTotal); got != tt.wantRejectedDischarged {
			t.Errorf("%s: discharged rejected = %v, want %v", tt.name, got, tt.wantRejectedDischarged)
		}
	}
}

func TestCountersInvalidUntilBothHaveAValue(t *testing.T) {
	f := defaultFilter()
	r := f.Apply(snap(0, func(s *solarbank.Snapshot) { s.ChargedTotalWh = 0 }))
	if r.CountersValid {
		t.Error("charged total 0 on the first reading: CountersValid = true, want false")
	}
	if r := f.Apply(snap(5, nil)); !r.CountersValid {
		t.Error("both totals present: CountersValid = false, want true")
	}
}

func rejected(r plausibility.Reading, name plausibility.Name) bool {
	for _, rej := range r.Rejections {
		if rej.Filter == name {
			return true
		}
	}
	return false
}

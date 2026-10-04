package plausibility

import (
	"time"

	"github.com/ryckakas/solix-mqtt-bridge/internal/solarbank"
)

const (
	socMarginPP = 1.0
	// Used when the device reports no capacity or power limit: 2 pp per 5 s poll, generous for a home battery.
	fallbackPPPerHour = 1440.0
)

func (f *Filter) filterSoC(s solarbank.Snapshot, r *Reading) uint16 {
	raw := s.SoCPercent
	if raw > 100 {
		kept := uint16(100)
		if f.soc.ok {
			kept = f.soc.value
		}
		r.Rejections = append(r.Rejections, Rejection{Filter: SoCRange, Raw: int64(raw), Kept: int64(kept)})
		return kept
	}
	if !f.soc.ok || within(raw, f.soc.value, maxSoCDeltaPP(s, s.At.Sub(f.soc.at))) {
		f.acceptSoC(raw, s.At)
		return raw
	}
	if f.confirmed(raw, s) {
		f.acceptSoC(raw, s.At)
		return raw
	}
	r.Rejections = append(r.Rejections, Rejection{Filter: SoCJump, Raw: int64(raw), Kept: int64(f.soc.value)})
	return f.soc.value
}

// A jump is accepted once readings have stayed near the new level for SoCJumpConfirm; a different level restarts it.
func (f *Filter) confirmed(raw uint16, s solarbank.Snapshot) bool {
	if !f.pending.ok || !within(raw, f.pending.value, maxSoCDeltaPP(s, s.At.Sub(f.pending.at))) {
		f.pending = sample[uint16]{value: raw, at: s.At, ok: true}
		return false
	}
	return s.At.Sub(f.pending.at) >= f.cfg.SoCJumpConfirm
}

func (f *Filter) acceptSoC(v uint16, at time.Time) {
	f.soc = sample[uint16]{value: v, at: at, ok: true}
	f.pending = sample[uint16]{}
}

func maxSoCDeltaPP(s solarbank.Snapshot, dt time.Duration) float64 {
	hours := max(dt.Hours(), 0)
	maxPowerW := max(s.MaxChargePowerW, s.MaxDischargePowerW)
	if s.CapacityWh == 0 || maxPowerW == 0 {
		return fallbackPPPerHour*hours + socMarginPP
	}
	return float64(maxPowerW)*hours/float64(s.CapacityWh)*100 + socMarginPP
}

func within(a, b uint16, limit float64) bool {
	d := float64(a) - float64(b)
	return d <= limit && -d <= limit
}

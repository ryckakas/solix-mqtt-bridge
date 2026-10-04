// Package plausibility filters Solarbank readings for known firmware glitches before they are published: a battery
// power of exactly 0 W for one poll, a state of charge that jumps further than physics allows, and energy totals
// that read zero or run backwards.
package plausibility

import (
	"maps"
	"time"

	"github.com/ryckakas/solix-mqtt-bridge/internal/solarbank"
)

// Name identifies one filter rule in a Rejection and in Counts.
type Name string

const (
	// ZeroPower replaces a 0 W reading while the status says charging or discharging.
	ZeroPower Name = "zero-power"
	// SoCJump holds the last state of charge when a reading moves further than the battery could have.
	SoCJump Name = "soc-jump"
	// SoCRange replaces a state of charge above 100 %.
	SoCRange Name = "soc-range"
	// ChargedTotal rejects a lifetime charged-energy reading that is zero or lower than the last accepted one.
	ChargedTotal Name = "charged-total"
	// DischargedTotal rejects a lifetime discharged-energy reading that is zero or lower than the last accepted one.
	DischargedTotal Name = "discharged-total"
)

// Config tunes the filters.
type Config struct {
	// SoCJumpConfirm is how long a state of charge beyond the physical limit must persist before it is accepted.
	SoCJumpConfirm time.Duration
	// ZeroPowerHoldPolls is how many consecutive polls a contradictory 0 W reading is replaced by the last power.
	ZeroPowerHoldPolls int
}

// Rejection describes one raw value a filter replaced.
type Rejection struct {
	// Filter is the rule that fired.
	Filter Name
	// Raw is the value the device reported.
	Raw int64
	// Kept is the value used instead.
	Kept int64
}

// Reading is the filtered view of one snapshot.
type Reading struct {
	// At is the snapshot's poll time.
	At time.Time
	// ChargePowerW is the battery power in W, positive while charging.
	ChargePowerW int64
	// SoCPercent is the state of charge, 0 to 100.
	SoCPercent uint16
	// CountersValid reports whether both lifetime totals have an accepted value yet.
	CountersValid bool
	// ChargedTotalWh is the last accepted lifetime charged energy; meaningful only when CountersValid.
	ChargedTotalWh uint64
	// DischargedTotalWh is the last accepted lifetime discharged energy; meaningful only when CountersValid.
	DischargedTotalWh uint64
	// Rejections lists the raw values replaced in this reading.
	Rejections []Rejection
}

// Filter carries the state the rules need between polls. It is not safe for concurrent use.
type Filter struct {
	cfg    Config
	counts map[Name]uint64

	power      sample[int64]
	heldPolls  int
	soc        sample[uint16]
	pending    sample[uint16]
	charged    sample[uint64]
	discharged sample[uint64]
}

type sample[T any] struct {
	value T
	at    time.Time
	ok    bool
}

// New returns a Filter with no history; the first reading's power and state of charge are accepted as they are.
func New(cfg Config) *Filter {
	return &Filter{cfg: cfg, counts: map[Name]uint64{}}
}

// Apply filters one snapshot and updates the history.
func (f *Filter) Apply(s solarbank.Snapshot) Reading {
	r := Reading{At: s.At}
	r.ChargePowerW = f.filterPower(s, &r)
	r.SoCPercent = f.filterSoC(s, &r)
	f.filterCounter(ChargedTotal, &f.charged, s.ChargedTotalWh, s.At, &r)
	f.filterCounter(DischargedTotal, &f.discharged, s.DischargedTotalWh, s.At, &r)
	r.CountersValid = f.charged.ok && f.discharged.ok
	r.ChargedTotalWh, r.DischargedTotalWh = f.charged.value, f.discharged.value
	for _, rej := range r.Rejections {
		f.counts[rej.Filter]++
	}
	return r
}

// Counts returns how often each rule has fired since New.
func (f *Filter) Counts() map[Name]uint64 {
	return maps.Clone(f.counts)
}

func (f *Filter) filterPower(s solarbank.Snapshot, r *Reading) int64 {
	raw := s.ChargePowerW()
	active := s.Status == solarbank.StatusCharging || s.Status == solarbank.StatusDischarging
	if raw == 0 && active && f.power.ok && f.heldPolls < f.cfg.ZeroPowerHoldPolls {
		f.heldPolls++
		r.Rejections = append(r.Rejections, Rejection{Filter: ZeroPower, Raw: raw, Kept: f.power.value})
		return f.power.value
	}
	f.heldPolls = 0
	f.power = sample[int64]{value: raw, at: s.At, ok: true}
	return raw
}

func (f *Filter) filterCounter(name Name, last *sample[uint64], raw uint64, at time.Time, r *Reading) {
	if raw == 0 || (last.ok && raw < last.value) {
		r.Rejections = append(r.Rejections, Rejection{Filter: name, Raw: int64(raw), Kept: int64(last.value)}) //nolint:gosec // Wh totals stay far below 2^63
		return
	}
	*last = sample[uint64]{value: raw, at: at, ok: true}
}

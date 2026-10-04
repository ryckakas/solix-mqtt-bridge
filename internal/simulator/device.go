// Package simulator is a Modbus TCP stand-in for an Anker SOLIX Solarbank Max AC, serving its input registers to
// tests and the local dev stack. It is a test stand-in only and must never be linked into the bridge.
package simulator

import (
	"maps"
	"math"
	"sync"
	"time"
)

// State is the simulated device state the register image is rendered from, in the device's own conventions.
type State struct {
	// Model is served at register 32768.
	Model string
	// Serial is served at register 10100.
	Serial string
	// Firmware is served at register 10112.
	Firmware string
	// Status is served at register 10001: 0 standby, 1 charging, 2 discharging, 3 sleep.
	Status uint16
	// BatteryPowerW is served at register 10008: positive while discharging, negative while charging.
	BatteryPowerW int32
	// PVPowerW is served at register 10002.
	PVPowerW int32
	// ThirdPartyPVPowerW is served at register 10004.
	ThirdPartyPVPowerW int32
	// HomeLoadW is served at register 10010.
	HomeLoadW int32
	// GridPowerW is served at register 10012: positive while importing.
	GridPowerW int32
	// SoCPercent is served at register 10014.
	SoCPercent uint16
	// MaxChargePowerW is served verbatim at register 10036; the real device may report it with either sign.
	MaxChargePowerW int32
	// MaxDischargePowerW is served verbatim at register 10038.
	MaxDischargePowerW int32
	// CapacityWh is served at register 10250, truncated to 100 Wh steps.
	CapacityWh uint32
	// SectionStatus is served at register 10252.
	SectionStatus uint16
	// SectionSoCPercent is served at register 10256.
	SectionSoCPercent uint16
	// EnergyWh is the stored energy Step integrates; it drives SoCPercent and is not served directly.
	EnergyWh float64
	// ChargedTotalWh is served at register 10262, truncated to 100 Wh steps.
	ChargedTotalWh float64
	// DischargedTotalWh is served at register 10264, truncated to 100 Wh steps.
	DischargedTotalWh float64
}

// DefaultState returns a plausible idle Max AC at 50 % state of charge.
func DefaultState() State {
	return State{
		Model:              "A17E2",
		Serial:             "APZ1DMWH0000000001",
		Firmware:           "v1.0.1.14",
		MaxChargePowerW:    2500,
		MaxDischargePowerW: 2500,
		CapacityWh:         5000,
		SoCPercent:         50,
		SectionSoCPercent:  50,
		EnergyWh:           2500,
		ChargedTotalWh:     1_234_500,
		DischargedTotalWh:  1_100_000,
	}
}

// Access is one kind of Modbus request the simulator has received, named after its function codes.
type Access string

const (
	// AccessReadInputRegisters is FC04, the only access the bridge is allowed to make.
	AccessReadInputRegisters Access = "read input registers (FC04)"
	// AccessReadHoldingRegisters is FC03.
	AccessReadHoldingRegisters Access = "read holding registers (FC03)"
	// AccessWriteHoldingRegisters is FC06 or FC16.
	AccessWriteHoldingRegisters Access = "write holding registers (FC06/FC16)"
	// AccessReadCoils is FC01.
	AccessReadCoils Access = "read coils (FC01)"
	// AccessWriteCoils is FC05 or FC15.
	AccessWriteCoils Access = "write coils (FC05/FC15)"
	// AccessReadDiscreteInputs is FC02.
	AccessReadDiscreteInputs Access = "read discrete inputs (FC02)"
)

// Device is a simulated Solarbank: its state, a function-code recorder, and the request handling around them.
// It is safe for concurrent use.
type Device struct {
	unitID uint8

	mu       sync.Mutex
	state    State
	accesses map[Access]int
}

// NewDevice returns a device that answers only requests addressed to unitID.
func NewDevice(unitID uint8, initial State) *Device {
	return &Device{unitID: unitID, state: initial, accesses: map[Access]int{}}
}

// State returns a copy of the current state.
func (d *Device) State() State {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.state
}

// Update applies fn to the state atomically; scenarios use it to inject glitches between polls.
func (d *Device) Update(fn func(*State)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	fn(&d.state)
}

// Step advances the physics by dt: stored energy, state of charge, lifetime totals and status follow BatteryPowerW.
func (d *Device) Step(dt time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := &d.state
	dischargedWh := float64(s.BatteryPowerW) * dt.Hours()
	s.EnergyWh = math.Max(0, math.Min(float64(s.CapacityWh), s.EnergyWh-dischargedWh))
	if dischargedWh < 0 {
		s.ChargedTotalWh -= dischargedWh
	} else {
		s.DischargedTotalWh += dischargedWh
	}
	if s.CapacityWh > 0 {
		s.SoCPercent = uint16(math.Round(s.EnergyWh / float64(s.CapacityWh) * 100))
		s.SectionSoCPercent = s.SoCPercent
	}
	s.Status = statusFor(s.BatteryPowerW)
}

// Accesses returns how many requests of each kind the device has received, including rejected ones.
func (d *Device) Accesses() map[Access]int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return maps.Clone(d.accesses)
}

// UnexpectedAccesses returns every recorded access other than FC04 reads; the bridge must never cause one.
func (d *Device) UnexpectedAccesses() map[Access]int {
	got := d.Accesses()
	delete(got, AccessReadInputRegisters)
	return got
}

func (d *Device) record(a Access) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.accesses[a]++
}

func statusFor(batteryPowerW int32) uint16 {
	switch {
	case batteryPowerW < 0:
		return 1
	case batteryPowerW > 0:
		return 2
	default:
		return 0
	}
}

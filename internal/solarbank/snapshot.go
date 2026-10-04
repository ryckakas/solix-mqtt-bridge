package solarbank

import (
	"fmt"
	"time"
)

// ModelMaxAC is the model string the Solarbank Max AC reports in register 32768.
const ModelMaxAC = "A17E2"

// BatteryStatus is the battery state the device reports in register 10001.
type BatteryStatus uint16

const (
	// StatusStandby is register value 0: idle, ready to charge or discharge.
	StatusStandby BatteryStatus = 0
	// StatusCharging is register value 1.
	StatusCharging BatteryStatus = 1
	// StatusDischarging is register value 2.
	StatusDischarging BatteryStatus = 2
	// StatusSleep is register value 3: the battery is in low-power sleep.
	StatusSleep BatteryStatus = 3
)

// String returns the status name, or "unknown(n)" for a value the register map doesn't define.
func (s BatteryStatus) String() string {
	switch s {
	case StatusStandby:
		return "standby"
	case StatusCharging:
		return "charging"
	case StatusDischarging:
		return "discharging"
	case StatusSleep:
		return "sleep"
	default:
		return fmt.Sprintf("unknown(%d)", uint16(s))
	}
}

// Identity is the device's self-description, read once per connection.
type Identity struct {
	// Model comes from register 32768, e.g. ModelMaxAC.
	Model string
	// Serial comes from register 10100.
	Serial string
	// Firmware comes from register 10112.
	Firmware string
}

// Snapshot is one decoded poll of the live and battery registers, unfiltered and in the device's own sign conventions.
type Snapshot struct {
	// At is when the poll completed.
	At time.Time
	// Status comes from register 10001.
	Status BatteryStatus
	// BatteryPowerW comes from register 10008: positive while discharging, negative while charging.
	BatteryPowerW int32
	// PVPowerW comes from register 10002, the Solarbank's own PV input.
	PVPowerW int32
	// ThirdPartyPVPowerW comes from register 10004.
	ThirdPartyPVPowerW int32
	// HomeLoadW comes from register 10010.
	HomeLoadW int32
	// GridPowerW comes from register 10012, measured at the Solarbank's own CT: positive while importing.
	GridPowerW int32
	// SoCPercent comes from register 10014, nominally 0 to 100.
	SoCPercent uint16
	// MaxChargePowerW is the magnitude of register 10036, the device's live charge limit.
	MaxChargePowerW int64
	// MaxDischargePowerW is the magnitude of register 10038, the device's live discharge limit.
	MaxDischargePowerW int64
	// CapacityWh comes from register 10250 in 100 Wh steps; zero if the firmware doesn't implement it.
	CapacityWh uint64
	// SectionStatus is register 10252, whose meaning on this model is unconfirmed: diagnostics only.
	SectionStatus uint16
	// SectionSoCPercent is register 10256, whose meaning on this model is unconfirmed: diagnostics only.
	SectionSoCPercent uint16
	// ChargedTotalWh is the lifetime charged energy from register 10262, in 100 Wh steps.
	ChargedTotalWh uint64
	// DischargedTotalWh is the lifetime discharged energy from register 10264, in 100 Wh steps.
	DischargedTotalWh uint64
}

// ChargePowerW returns the battery power with charging positive and discharging negative, the inverse of the
// device's convention; every output publishes this one.
func (s Snapshot) ChargePowerW() int64 {
	return -int64(s.BatteryPowerW)
}

package solarbank

import (
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

type words struct {
	start uint16
	regs  []uint16
}

func wordsFromBytes(b block, raw []byte) (words, error) {
	if len(raw) != 2*int(b.count) {
		return words{}, fmt.Errorf("register block %d: got %d bytes, want %d", b.start, len(raw), 2*int(b.count))
	}
	regs := make([]uint16, b.count)
	for i := range regs {
		regs[i] = binary.BigEndian.Uint16(raw[2*i:])
	}
	return words{start: b.start, regs: regs}, nil
}

func (w words) u16(addr uint16) uint16 {
	return w.regs[addr-w.start]
}

func (w words) u32(addr uint16) uint32 {
	return uint32(w.u16(addr))<<16 | uint32(w.u16(addr+1))
}

func (w words) s32(addr uint16) int32 {
	return int32(w.u32(addr)) //nolint:gosec // the device encodes signed values as two's complement; wrapping is the decode
}

func (w words) str(addr, count uint16) string {
	buf := make([]byte, 0, 2*int(count))
	for i := range count {
		buf = binary.BigEndian.AppendUint16(buf, w.u16(addr+i))
	}
	return strings.TrimSpace(strings.TrimRight(string(buf), "\x00"))
}

func decodeSnapshot(live, battery words, at time.Time) Snapshot {
	return Snapshot{
		At:                 at,
		Status:             BatteryStatus(live.u16(regStatus)),
		BatteryPowerW:      live.s32(regBatteryPower),
		PVPowerW:           live.s32(regPVPower),
		ThirdPartyPVPowerW: live.s32(regThirdPartyPVPower),
		HomeLoadW:          live.s32(regHomeLoad),
		GridPowerW:         live.s32(regGridPower),
		SoCPercent:         live.u16(regSoC),
		MaxChargePowerW:    magnitude(live.s32(regMaxChargePower)),
		MaxDischargePowerW: magnitude(live.s32(regMaxDischargePower)),
		CapacityWh:         energyWh(battery.u32(regCapacity)),
		SectionStatus:      battery.u16(regSectionStatus),
		SectionSoCPercent:  battery.u16(regSectionSoC),
		ChargedTotalWh:     energyWh(battery.u32(regChargedTotal)),
		DischargedTotalWh:  energyWh(battery.u32(regDischargedTotal)),
	}
}

func decodeIdentity(identity, model words) Identity {
	return Identity{
		Model:    model.str(regModel, modelWords),
		Serial:   identity.str(regSerial, serialWords),
		Firmware: identity.str(regFirmware, firmwareWords),
	}
}

func magnitude(v int32) int64 {
	if v < 0 {
		return -int64(v)
	}
	return int64(v)
}

func energyWh(raw uint32) uint64 {
	return uint64(raw) * whPerEnergyUnit
}

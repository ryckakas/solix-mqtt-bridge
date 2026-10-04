package simulator

import (
	"github.com/simonvetter/modbus"
)

type addrRange struct {
	first, last uint16
}

// The ranges Anker's integration polls; inside them the real firmware answers unimplemented addresses with 0.
var servedRanges = []addrRange{
	{first: 10000, last: 10050},
	{first: 10090, last: 10156},
	{first: 10208, last: 10265},
	{first: 32768, last: 32774},
}

// HandleInputRegisters answers FC04 reads from the rendered register image; it rejects other unit ids and
// addresses outside the served ranges, like the real device.
func (d *Device) HandleInputRegisters(req *modbus.InputRegistersRequest) ([]uint16, error) {
	d.record(AccessReadInputRegisters)
	if req.UnitId != d.unitID {
		return nil, modbus.ErrGWTargetFailedToRespond
	}
	if !served(req.Addr, req.Quantity) {
		return nil, modbus.ErrIllegalDataAddress
	}
	img := render(d.State())
	res := make([]uint16, req.Quantity)
	for i := range res {
		res[i] = img[req.Addr+uint16(i)]
	}
	return res, nil
}

// HandleHoldingRegisters records and rejects FC03/FC06/FC16 requests; the bridge must never send them.
func (d *Device) HandleHoldingRegisters(req *modbus.HoldingRegistersRequest) ([]uint16, error) {
	if req.IsWrite {
		d.record(AccessWriteHoldingRegisters)
	} else {
		d.record(AccessReadHoldingRegisters)
	}
	return nil, modbus.ErrIllegalFunction
}

// HandleCoils records and rejects FC01/FC05/FC15 requests.
func (d *Device) HandleCoils(req *modbus.CoilsRequest) ([]bool, error) {
	if req.IsWrite {
		d.record(AccessWriteCoils)
	} else {
		d.record(AccessReadCoils)
	}
	return nil, modbus.ErrIllegalFunction
}

// HandleDiscreteInputs records and rejects FC02 requests.
func (d *Device) HandleDiscreteInputs(_ *modbus.DiscreteInputsRequest) ([]bool, error) {
	d.record(AccessReadDiscreteInputs)
	return nil, modbus.ErrIllegalFunction
}

func served(addr, quantity uint16) bool {
	last := int(addr) + int(quantity) - 1
	for _, r := range servedRanges {
		if addr >= r.first && last <= int(r.last) {
			return true
		}
	}
	return false
}

// Addresses are literals on purpose: the simulator is an independent oracle for the bridge's register map.
func render(s State) map[uint16]uint16 {
	img := make(map[uint16]uint16, 64)
	put32 := func(addr uint16, v uint32) {
		img[addr], img[addr+1] = uint16(v>>16), uint16(v) //nolint:gosec // splitting into the two 16-bit words
	}
	putStr := func(addr uint16, words int, str string) {
		b := make([]byte, 2*words)
		copy(b, str)
		for i := range words {
			img[addr+uint16(i)] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
		}
	}

	img[10001] = s.Status
	put32(10002, twosComplement(s.PVPowerW))
	put32(10004, twosComplement(s.ThirdPartyPVPowerW))
	put32(10008, twosComplement(s.BatteryPowerW))
	put32(10010, twosComplement(s.HomeLoadW))
	put32(10012, twosComplement(s.GridPowerW))
	img[10014] = s.SoCPercent
	put32(10036, twosComplement(s.MaxChargePowerW))
	put32(10038, twosComplement(s.MaxDischargePowerW))
	putStr(10100, 12, s.Serial)
	putStr(10112, 6, s.Firmware)
	put32(10250, s.CapacityWh/100)
	img[10252] = s.SectionStatus
	img[10256] = s.SectionSoCPercent
	put32(10262, tenthKWh(s.ChargedTotalWh))
	put32(10264, tenthKWh(s.DischargedTotalWh))
	putStr(32768, 5, s.Model)
	return img
}

func twosComplement(v int32) uint32 {
	return uint32(v) //nolint:gosec // the device encodes signed values as two's complement
}

func tenthKWh(wh float64) uint32 {
	if wh <= 0 {
		return 0
	}
	return uint32(wh / 100)
}

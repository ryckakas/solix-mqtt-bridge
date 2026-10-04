package solarbank

import (
	"encoding/binary"
	"testing"
	"time"
)

func TestWordsFromBytesDecodesBigEndian(t *testing.T) {
	w, err := wordsFromBytes(block{start: 100, count: 2}, []byte{0x12, 0x34, 0xAB, 0xCD})
	if err != nil {
		t.Fatalf("wordsFromBytes: %v", err)
	}
	if got := w.u16(100); got != 0x1234 {
		t.Errorf("u16(100) = %#04x, want 0x1234", got)
	}
	if got := w.u16(101); got != 0xABCD {
		t.Errorf("u16(101) = %#04x, want 0xabcd", got)
	}
}

func TestWordsFromBytesRejectsWrongLength(t *testing.T) {
	for _, n := range []int{0, 3, 6} {
		if _, err := wordsFromBytes(block{start: 100, count: 2}, make([]byte, n)); err == nil {
			t.Errorf("%d bytes for a 2-register block: want error, got nil", n)
		}
	}
}

func TestS32IsHighWordFirstTwosComplement(t *testing.T) {
	tests := []struct {
		name string
		regs []uint16
		want int32
	}{
		{"positive", []uint16{0x0000, 0x03E8}, 1000},
		{"negative", []uint16{0xFFFF, 0xFC18}, -1000},
		{"minus one", []uint16{0xFFFF, 0xFFFF}, -1},
		{"high word carries value", []uint16{0x0001, 0x0000}, 65536},
		{"most negative", []uint16{0x8000, 0x0000}, -2147483648},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := words{start: 0, regs: tt.regs}
			if got := w.s32(0); got != tt.want {
				t.Errorf("s32 = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestU32IsHighWordFirst(t *testing.T) {
	w := words{start: 0, regs: []uint16{0x0001, 0x0002}}
	if got := w.u32(0); got != 65538 {
		t.Errorf("u32 = %d, want 65538", got)
	}
}

func TestStrTrimsNULPaddingAndSpaces(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"odd length NUL padded", "A17E2", "A17E2"},
		{"even length", "AB12", "AB12"},
		{"trailing spaces", "v1.0 ", "v1.0"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			regs := make([]uint16, 4)
			putStr(regs, 0, tt.in)
			w := words{start: 0, regs: regs}
			if got := w.str(0, 4); got != tt.want {
				t.Errorf("str = %q, want %q", got, tt.want)
			}
		})
	}
}

// Offsets are written as literals, not derived from the reg* constants, so a wrong address can't pass.
func TestDecodeSnapshotGolden(t *testing.T) {
	live := make([]uint16, 51)
	live[1] = 1
	putS32(live, 2, 1800)
	putS32(live, 4, 2400)
	putS32(live, 8, -1500)
	putS32(live, 10, 450)
	putS32(live, 12, -120)
	live[14] = 63
	putS32(live, 36, 2500)
	putS32(live, 38, -2400)

	battery := make([]uint16, 58)
	putU32(battery, 42, 50)
	battery[44] = 1
	battery[48] = 62
	putU32(battery, 54, 123456)
	putU32(battery, 56, 0x0001_0002)

	at := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	got := decodeSnapshot(mustWords(t, liveBlock, live), mustWords(t, batteryBlock, battery), at)

	want := Snapshot{
		At:                 at,
		Status:             StatusCharging,
		BatteryPowerW:      -1500,
		PVPowerW:           1800,
		ThirdPartyPVPowerW: 2400,
		HomeLoadW:          450,
		GridPowerW:         -120,
		SoCPercent:         63,
		MaxChargePowerW:    2500,
		MaxDischargePowerW: 2400,
		CapacityWh:         5000,
		SectionStatus:      1,
		SectionSoCPercent:  62,
		ChargedTotalWh:     12345600,
		DischargedTotalWh:  6553800,
	}
	if got != want {
		t.Errorf("decodeSnapshot:\n got  %+v\n want %+v", got, want)
	}
}

func TestDecodeIdentityGolden(t *testing.T) {
	identity := make([]uint16, 67)
	putStr(identity, 10, "APZ1DMWH0123456789AB")
	putStr(identity, 22, "v1.0.1.14")
	model := make([]uint16, 7)
	putStr(model, 0, "A17E2")

	got := decodeIdentity(mustWords(t, identityBlock, identity), mustWords(t, modelBlock, model))

	want := Identity{Model: ModelMaxAC, Serial: "APZ1DMWH0123456789AB", Firmware: "v1.0.1.14"}
	if got != want {
		t.Errorf("decodeIdentity = %+v, want %+v", got, want)
	}
}

func TestChargePowerWInvertsDeviceSign(t *testing.T) {
	tests := []struct {
		name   string
		device int32
		want   int64
	}{
		{"charging", -1500, 1500},
		{"discharging", 800, -800},
		{"idle", 0, 0},
		{"most negative does not overflow", -2147483648, 2147483648},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := (Snapshot{BatteryPowerW: tt.device}).ChargePowerW(); got != tt.want {
				t.Errorf("ChargePowerW = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestBatteryStatusString(t *testing.T) {
	tests := map[BatteryStatus]string{
		StatusStandby:     "standby",
		StatusCharging:    "charging",
		StatusDischarging: "discharging",
		StatusSleep:       "sleep",
		BatteryStatus(7):  "unknown(7)",
	}
	for status, want := range tests {
		if got := status.String(); got != want {
			t.Errorf("BatteryStatus(%d).String() = %q, want %q", uint16(status), got, want)
		}
	}
}

func TestRegistersLieInsideTheirBlocks(t *testing.T) {
	tests := []struct {
		name  string
		addr  uint16
		width uint16
		in    block
	}{
		{"status", regStatus, 1, liveBlock},
		{"pv power", regPVPower, 2, liveBlock},
		{"third-party pv power", regThirdPartyPVPower, 2, liveBlock},
		{"battery power", regBatteryPower, 2, liveBlock},
		{"home load", regHomeLoad, 2, liveBlock},
		{"grid power", regGridPower, 2, liveBlock},
		{"soc", regSoC, 1, liveBlock},
		{"max charge power", regMaxChargePower, 2, liveBlock},
		{"max discharge power", regMaxDischargePower, 2, liveBlock},
		{"capacity", regCapacity, 2, batteryBlock},
		{"section status", regSectionStatus, 1, batteryBlock},
		{"section soc", regSectionSoC, 1, batteryBlock},
		{"charged total", regChargedTotal, 2, batteryBlock},
		{"discharged total", regDischargedTotal, 2, batteryBlock},
		{"serial", regSerial, serialWords, identityBlock},
		{"firmware", regFirmware, firmwareWords, identityBlock},
		{"model", regModel, modelWords, modelBlock},
	}
	for _, tt := range tests {
		if tt.addr < tt.in.start || int(tt.addr)+int(tt.width) > int(tt.in.start)+int(tt.in.count) {
			t.Errorf("%s (%d, %d words) lies outside block %d+%d", tt.name, tt.addr, tt.width, tt.in.start, tt.in.count)
		}
	}
}

// Anker's integration reads at most 100 registers per request; larger reads are untested against the device.
func TestBlocksStayWithinReadLimit(t *testing.T) {
	for _, b := range []block{liveBlock, batteryBlock, identityBlock, modelBlock} {
		if b.count == 0 || b.count > 100 {
			t.Errorf("block %d has %d registers, want 1..100", b.start, b.count)
		}
	}
}

func mustWords(t *testing.T, b block, regs []uint16) words {
	t.Helper()
	raw := make([]byte, 0, 2*len(regs))
	for _, r := range regs {
		raw = binary.BigEndian.AppendUint16(raw, r)
	}
	w, err := wordsFromBytes(b, raw)
	if err != nil {
		t.Fatalf("wordsFromBytes: %v", err)
	}
	return w
}

func putS32(regs []uint16, idx int, v int32) {
	putU32(regs, idx, uint32(v))
}

func putU32(regs []uint16, idx int, v uint32) {
	regs[idx], regs[idx+1] = uint16(v>>16), uint16(v)
}

func putStr(regs []uint16, idx int, s string) {
	if len(s)%2 == 1 {
		s += "\x00"
	}
	for i := range len(s) / 2 {
		regs[idx+i] = uint16(s[2*i])<<8 | uint16(s[2*i+1])
	}
}

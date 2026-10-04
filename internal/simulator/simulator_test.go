package simulator_test

import (
	"math"
	"testing"
	"time"

	"github.com/simonvetter/modbus"

	"github.com/ryckakas/solix-mqtt-bridge/internal/simulator"
)

func TestServesRegisterImageAtDocumentedAddresses(t *testing.T) {
	dev := simulator.NewDevice(1, simulator.DefaultState())
	dev.Update(func(s *simulator.State) {
		s.Status = 2
		s.BatteryPowerW = -1500
		s.SoCPercent = 63
		s.ChargedTotalWh = 12_345_678
	})
	c := dial(t, listen(t, dev), 1)

	live, err := c.ReadRegisters(10000, 51, modbus.INPUT_REGISTER)
	if err != nil {
		t.Fatalf("read live block: %v", err)
	}
	if live[1] != 2 {
		t.Errorf("status word = %d, want 2", live[1])
	}
	if live[8] != 0xFFFF || live[9] != 0xFA24 {
		t.Errorf("battery power words = %#04x %#04x, want 0xffff 0xfa24 (-1500)", live[8], live[9])
	}
	if live[14] != 63 {
		t.Errorf("soc word = %d, want 63", live[14])
	}

	battery, err := c.ReadRegisters(10208, 58, modbus.INPUT_REGISTER)
	if err != nil {
		t.Fatalf("read battery block: %v", err)
	}
	if got := uint32(battery[54])<<16 | uint32(battery[55]); got != 123456 {
		t.Errorf("charged total = %d tenth-kWh, want 123456 (truncated to 100 Wh steps)", got)
	}

	model, err := c.ReadBytes(32768, 10, modbus.INPUT_REGISTER)
	if err != nil {
		t.Fatalf("read model: %v", err)
	}
	if string(model[:5]) != "A17E2" {
		t.Errorf("model = %q, want A17E2", model)
	}
}

func TestRejectsOtherUnitIDs(t *testing.T) {
	dev := simulator.NewDevice(1, simulator.DefaultState())
	c := dial(t, listen(t, dev), 7)
	if _, err := c.ReadRegisters(10000, 1, modbus.INPUT_REGISTER); err == nil {
		t.Fatal("read with unit id 7: want error, got nil")
	}
}

func TestRejectsAddressesOutsideServedRanges(t *testing.T) {
	dev := simulator.NewDevice(1, simulator.DefaultState())
	c := dial(t, listen(t, dev), 1)
	for _, tt := range []struct {
		addr, quantity uint16
	}{{9999, 1}, {10050, 2}, {10051, 1}, {40000, 1}} {
		if _, err := c.ReadRegisters(tt.addr, tt.quantity, modbus.INPUT_REGISTER); err == nil {
			t.Errorf("read %d+%d: want illegal data address, got nil", tt.addr, tt.quantity)
		}
	}
}

func TestRecordsAndRejectsEverythingButFC04(t *testing.T) {
	dev := simulator.NewDevice(1, simulator.DefaultState())
	c := dial(t, listen(t, dev), 1)

	if _, err := c.ReadRegisters(10000, 1, modbus.INPUT_REGISTER); err != nil {
		t.Fatalf("FC04 read: %v", err)
	}
	if len(dev.UnexpectedAccesses()) != 0 {
		t.Fatalf("after an FC04 read: unexpected accesses %v", dev.UnexpectedAccesses())
	}

	if _, err := c.ReadRegisters(10064, 1, modbus.HOLDING_REGISTER); err == nil {
		t.Error("FC03 read: want illegal function, got nil")
	}
	if err := c.WriteRegister(10064, 3); err == nil {
		t.Error("FC06 write: want illegal function, got nil")
	}
	if err := c.WriteCoil(1, true); err == nil {
		t.Error("FC05 write: want illegal function, got nil")
	}

	got := dev.UnexpectedAccesses()
	for _, a := range []simulator.Access{
		simulator.AccessReadHoldingRegisters,
		simulator.AccessWriteHoldingRegisters,
		simulator.AccessWriteCoils,
	} {
		if got[a] != 1 {
			t.Errorf("accesses[%q] = %d, want 1 (all: %v)", a, got[a], got)
		}
	}
	if dev.Accesses()[simulator.AccessReadInputRegisters] != 1 {
		t.Errorf("FC04 count = %d, want 1", dev.Accesses()[simulator.AccessReadInputRegisters])
	}
}

func TestStepIntegratesBatteryPower(t *testing.T) {
	dev := simulator.NewDevice(1, simulator.DefaultState())
	start := dev.State()

	dev.Update(func(s *simulator.State) { s.BatteryPowerW = -1000 })
	dev.Step(time.Hour)
	charged := dev.State()
	if math.Abs(charged.EnergyWh-(start.EnergyWh+1000)) > 1e-9 {
		t.Errorf("energy after 1 h at -1000 W = %v, want %v", charged.EnergyWh, start.EnergyWh+1000)
	}
	if charged.SoCPercent != 70 || charged.Status != 1 {
		t.Errorf("after charging: soc %d status %d, want 70 and 1", charged.SoCPercent, charged.Status)
	}
	if charged.ChargedTotalWh-start.ChargedTotalWh != 1000 {
		t.Errorf("charged total grew by %v, want 1000", charged.ChargedTotalWh-start.ChargedTotalWh)
	}

	dev.Update(func(s *simulator.State) { s.BatteryPowerW = 500 })
	dev.Step(2 * time.Hour)
	discharged := dev.State()
	if discharged.SoCPercent != 50 || discharged.Status != 2 {
		t.Errorf("after discharging: soc %d status %d, want 50 and 2", discharged.SoCPercent, discharged.Status)
	}
	if discharged.DischargedTotalWh-start.DischargedTotalWh != 1000 {
		t.Errorf("discharged total grew by %v, want 1000", discharged.DischargedTotalWh-start.DischargedTotalWh)
	}
}

func TestStepClampsStoredEnergyToCapacity(t *testing.T) {
	dev := simulator.NewDevice(1, simulator.DefaultState())
	dev.Update(func(s *simulator.State) { s.BatteryPowerW = -5000 })
	dev.Step(10 * time.Hour)
	if got := dev.State(); got.SoCPercent != 100 || got.EnergyWh != float64(got.CapacityWh) {
		t.Errorf("overcharged: soc %d energy %v, want 100 and %d", got.SoCPercent, got.EnergyWh, got.CapacityWh)
	}
}

func TestStopAndRestartOnTheSameAddress(t *testing.T) {
	dev := simulator.NewDevice(1, simulator.DefaultState())
	srv := listen(t, dev)
	addr := srv.Addr()
	c := dial(t, srv, 1)

	if _, err := c.ReadRegisters(10000, 1, modbus.INPUT_REGISTER); err != nil {
		t.Fatalf("read before outage: %v", err)
	}
	if err := srv.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if _, err := c.ReadRegisters(10000, 1, modbus.INPUT_REGISTER); err == nil {
		t.Fatal("read during outage: want error, got nil")
	}
	if err := srv.Start(); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if srv.Addr() != addr {
		t.Fatalf("address changed across restart: %s -> %s", addr, srv.Addr())
	}
	if _, err := dial(t, srv, 1).ReadRegisters(10000, 1, modbus.INPUT_REGISTER); err != nil {
		t.Fatalf("read after restart: %v", err)
	}
}

func listen(t *testing.T, dev *simulator.Device) *simulator.Server {
	t.Helper()
	srv, err := simulator.Listen(t.Context(), dev, "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })
	return srv
}

func dial(t *testing.T, srv *simulator.Server, unitID uint8) *modbus.ModbusClient {
	t.Helper()
	c, err := modbus.NewClient(&modbus.ClientConfiguration{URL: "tcp://" + srv.Addr(), Timeout: time.Second})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if err := c.Open(); err != nil {
		t.Fatalf("open client: %v", err)
	}
	if err := c.SetUnitId(unitID); err != nil {
		t.Fatalf("set unit id: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

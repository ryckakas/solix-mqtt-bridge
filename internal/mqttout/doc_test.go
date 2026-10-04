package mqttout

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ryckakas/solix-mqtt-bridge/internal/bridge"
	"github.com/ryckakas/solix-mqtt-bridge/internal/plausibility"
	"github.com/ryckakas/solix-mqtt-bridge/internal/solarbank"
)

var at = time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)

func freshState() bridge.State {
	return bridge.State{
		Identity:     solarbank.Identity{Model: "A17E2", Serial: "SN1", Firmware: "v1"},
		Fresh:        true,
		LastGoodRead: at,
		Raw: solarbank.Snapshot{
			At: at, Status: solarbank.StatusCharging, BatteryPowerW: -1500, PVPowerW: 2000, HomeLoadW: 500,
			GridPowerW: -10, SoCPercent: 64, ChargedTotalWh: 1_000_000, DischargedTotalWh: 900_000,
		},
		Reading: plausibility.Reading{
			At: at, ChargePowerW: 1500, SoCPercent: 63, CountersValid: true,
			ChargedTotalWh: 1_000_000, DischargedTotalWh: 900_000,
		},
		FilterCounts: map[plausibility.Name]uint64{plausibility.SoCJump: 2},
	}
}

func TestStateDocCarriesFilteredAndRawValues(t *testing.T) {
	raw, err := json.Marshal(buildStateDoc(freshState()))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	battery := doc["battery"].(map[string]any)
	rawBlock := doc["raw"].(map[string]any)
	checks := []struct {
		name string
		got  any
		want any
	}{
		{"status", doc["status"], "fresh"},
		{"last_good_read", doc["last_good_read"], "2026-10-04T12:00:00Z"},
		{"device.model", doc["device"].(map[string]any)["model"], "A17E2"},
		{"battery.charge_power_w (charging positive)", battery["charge_power_w"], 1500.0},
		{"battery.soc_percent (filtered)", battery["soc_percent"], 63.0},
		{"battery.status", battery["status"], "charging"},
		{"battery.charged_total_wh", battery["charged_total_wh"], 1_000_000.0},
		{"raw.battery_power_w (device convention)", rawBlock["battery_power_w"], -1500.0},
		{"raw.soc_percent (unfiltered)", rawBlock["soc_percent"], 64.0},
		{"filter_rejections.soc-jump", doc["filter_rejections"].(map[string]any)["soc-jump"], 2.0},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestStateDocBeforeFirstRead(t *testing.T) {
	raw, err := json.Marshal(buildStateDoc(bridge.State{}))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"status":"waiting","last_good_read":null,"device":{"model":"","serial":"","firmware":""},` +
		`"battery":null,"raw":null,"filter_rejections":{}}`
	if string(raw) != want {
		t.Errorf("doc =\n%s\nwant\n%s", raw, want)
	}
}

func TestStateDocNullsCountersUntilValid(t *testing.T) {
	s := freshState()
	s.Reading.CountersValid = false
	doc := buildStateDoc(s)
	if doc.Battery.ChargedTotalWh != nil || doc.Battery.DischargedTotalWh != nil {
		t.Errorf("counters = %v/%v, want nil until valid", doc.Battery.ChargedTotalWh, doc.Battery.DischargedTotalWh)
	}
}

func TestStatusAndAvailability(t *testing.T) {
	stale := freshState()
	stale.Fresh = false
	tests := []struct {
		name                      string
		s                         bridge.State
		wantStatus, wantAvailable string
	}{
		{"waiting", bridge.State{}, "waiting", "offline"},
		{"fresh", freshState(), "fresh", "online"},
		{"stale", stale, "stale", "offline"},
	}
	for _, tt := range tests {
		if got := status(tt.s); got != tt.wantStatus {
			t.Errorf("%s: status = %q, want %q", tt.name, got, tt.wantStatus)
		}
		if got := availability(tt.s); got != tt.wantAvailable {
			t.Errorf("%s: availability = %q, want %q", tt.name, got, tt.wantAvailable)
		}
	}
}

func TestFieldValuesOnlyWhileFresh(t *testing.T) {
	got := map[string]string{}
	for _, f := range fieldValues(freshState()) {
		got[f.name] = f.value
	}
	want := map[string]string{
		"charge_power_w": "1500", "soc_percent": "63", "battery_status": "charging", "pv_power_w": "2000",
		"third_party_pv_power_w": "0", "home_load_w": "500", "grid_power_w": "-10",
		"charged_total_wh": "1000000", "discharged_total_wh": "900000",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("field %s = %q, want %q", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("fields = %v, want exactly %v", got, want)
	}

	stale := freshState()
	stale.Fresh = false
	if f := fieldValues(stale); len(f) != 0 {
		t.Errorf("stale: fields = %v, want none", f)
	}
}

func TestFieldValuesOmitCountersUntilValid(t *testing.T) {
	s := freshState()
	s.Reading.CountersValid = false
	for _, f := range fieldValues(s) {
		if f.name == "charged_total_wh" || f.name == "discharged_total_wh" {
			t.Errorf("counter field %s published before counters are valid", f.name)
		}
	}
}

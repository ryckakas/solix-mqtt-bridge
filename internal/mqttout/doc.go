// Package mqttout is the generic MQTT output: under a base topic it publishes a retained JSON state document,
// retained per-field topics for simple consumers, and an availability topic that reads "online" only while the data
// is fresh.
package mqttout

import (
	"strconv"
	"time"

	"github.com/ryckakas/solix-mqtt-bridge/internal/bridge"
)

const (
	availabilityOnline  = "online"
	availabilityOffline = "offline"
)

type stateDoc struct {
	Status           string            `json:"status"`
	LastGoodRead     *time.Time        `json:"last_good_read"`
	Device           deviceDoc         `json:"device"`
	Battery          *batteryDoc       `json:"battery"`
	Raw              *rawDoc           `json:"raw"`
	FilterRejections map[string]uint64 `json:"filter_rejections"`
}

type deviceDoc struct {
	Model    string `json:"model"`
	Serial   string `json:"serial"`
	Firmware string `json:"firmware"`
}

type batteryDoc struct {
	ChargePowerW      int64   `json:"charge_power_w"`
	SoCPercent        uint16  `json:"soc_percent"`
	Status            string  `json:"status"`
	ChargedTotalWh    *uint64 `json:"charged_total_wh"`
	DischargedTotalWh *uint64 `json:"discharged_total_wh"`
}

type rawDoc struct {
	BatteryPowerW      int32  `json:"battery_power_w"`
	PVPowerW           int32  `json:"pv_power_w"`
	ThirdPartyPVPowerW int32  `json:"third_party_pv_power_w"`
	HomeLoadW          int32  `json:"home_load_w"`
	GridPowerW         int32  `json:"grid_power_w"`
	SoCPercent         uint16 `json:"soc_percent"`
	Status             uint16 `json:"status"`
	MaxChargePowerW    int64  `json:"max_charge_power_w"`
	MaxDischargePowerW int64  `json:"max_discharge_power_w"`
	CapacityWh         uint64 `json:"capacity_wh"`
	SectionStatus      uint16 `json:"section_status"`
	SectionSoCPercent  uint16 `json:"section_soc_percent"`
	ChargedTotalWh     uint64 `json:"charged_total_wh"`
	DischargedTotalWh  uint64 `json:"discharged_total_wh"`
}

func status(s bridge.State) string {
	switch {
	case !s.HasData():
		return "waiting"
	case s.Fresh:
		return "fresh"
	default:
		return "stale"
	}
}

func availability(s bridge.State) string {
	if s.Fresh {
		return availabilityOnline
	}
	return availabilityOffline
}

func buildStateDoc(s bridge.State) stateDoc {
	doc := stateDoc{
		Status:           status(s),
		Device:           deviceDoc{Model: s.Identity.Model, Serial: s.Identity.Serial, Firmware: s.Identity.Firmware},
		FilterRejections: map[string]uint64{},
	}
	for name, n := range s.FilterCounts {
		doc.FilterRejections[string(name)] = n
	}
	if !s.HasData() {
		return doc
	}
	at := s.LastGoodRead.UTC()
	doc.LastGoodRead = &at
	doc.Battery = &batteryDoc{
		ChargePowerW: s.Reading.ChargePowerW,
		SoCPercent:   s.Reading.SoCPercent,
		Status:       s.Raw.Status.String(),
	}
	if s.Reading.CountersValid {
		charged, discharged := s.Reading.ChargedTotalWh, s.Reading.DischargedTotalWh
		doc.Battery.ChargedTotalWh, doc.Battery.DischargedTotalWh = &charged, &discharged
	}
	r := s.Raw
	doc.Raw = &rawDoc{
		BatteryPowerW: r.BatteryPowerW, PVPowerW: r.PVPowerW, ThirdPartyPVPowerW: r.ThirdPartyPVPowerW,
		HomeLoadW: r.HomeLoadW, GridPowerW: r.GridPowerW, SoCPercent: r.SoCPercent, Status: uint16(r.Status),
		MaxChargePowerW: r.MaxChargePowerW, MaxDischargePowerW: r.MaxDischargePowerW, CapacityWh: r.CapacityWh,
		SectionStatus: r.SectionStatus, SectionSoCPercent: r.SectionSoCPercent,
		ChargedTotalWh: r.ChargedTotalWh, DischargedTotalWh: r.DischargedTotalWh,
	}
	return doc
}

type field struct {
	name  string
	value string
}

// Only fresh data reaches the per-field topics; while stale they keep their last retained value.
func fieldValues(s bridge.State) []field {
	if !s.Fresh || !s.HasData() {
		return nil
	}
	r := s.Raw
	fields := []field{
		{"charge_power_w", strconv.FormatInt(s.Reading.ChargePowerW, 10)},
		{"soc_percent", strconv.FormatUint(uint64(s.Reading.SoCPercent), 10)},
		{"battery_status", r.Status.String()},
		{"pv_power_w", strconv.FormatInt(int64(r.PVPowerW), 10)},
		{"third_party_pv_power_w", strconv.FormatInt(int64(r.ThirdPartyPVPowerW), 10)},
		{"home_load_w", strconv.FormatInt(int64(r.HomeLoadW), 10)},
		{"grid_power_w", strconv.FormatInt(int64(r.GridPowerW), 10)},
	}
	if s.Reading.CountersValid {
		fields = append(fields,
			field{"charged_total_wh", strconv.FormatUint(s.Reading.ChargedTotalWh, 10)},
			field{"discharged_total_wh", strconv.FormatUint(s.Reading.DischargedTotalWh, 10)})
	}
	return fields
}

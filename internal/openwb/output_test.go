package openwb

import (
	"testing"

	"github.com/ryckakas/solix-mqtt-bridge/internal/bridge"
	"github.com/ryckakas/solix-mqtt-bridge/internal/plausibility"
)

func TestMessages(t *testing.T) {
	fresh := bridge.State{Fresh: true, Reading: plausibility.Reading{
		ChargePowerW: -800, SoCPercent: 41, CountersValid: true, ChargedTotalWh: 1_234_500, DischargedTotalWh: 1_100_000,
	}}
	invalidCounters := fresh
	invalidCounters.Reading.CountersValid = false
	stale := fresh
	stale.Fresh = false

	tests := []struct {
		name     string
		s        bridge.State
		counters bool
		want     []message
	}{
		{"fresh: power (charging positive) and soc", fresh, false, []message{{"power", "-800"}, {"soc", "41"}}},
		{"fresh with counters", fresh, true, []message{
			{"power", "-800"}, {"soc", "41"}, {"imported", "1234500"}, {"exported", "1100000"},
		}},
		{"counters not yet valid", invalidCounters, true, []message{{"power", "-800"}, {"soc", "41"}}},
		{"stale: power 0 only", stale, true, []message{{"power", "0"}}},
		{"waiting: power 0 only", bridge.State{}, true, []message{{"power", "0"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := messages(tt.s, tt.counters)
			if len(got) != len(tt.want) {
				t.Fatalf("messages = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("message %d = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

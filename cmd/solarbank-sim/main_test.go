package main

import "testing"

func TestPhaseAtWalksTheCycle(t *testing.T) {
	tests := []struct {
		tick int
		want string
	}{
		{0, "charging"},
		{119, "charging"},
		{120, "idle"},
		{180, "discharging"},
		{299, "discharging"},
		{300, "charging"},
	}
	for _, tt := range tests {
		if got, _ := phaseAt(tt.tick); got.name != tt.want {
			t.Errorf("phaseAt(%d) = %q, want %q", tt.tick, got.name, tt.want)
		}
	}
}

func TestGlitchesLandInTheirPhases(t *testing.T) {
	if p, _ := phaseAt(30); p.name != "charging" {
		t.Errorf("SoC-jump glitch starts in %q, want charging", p.name)
	}
	if p, _ := phaseAt(240); p.name != "discharging" {
		t.Errorf("zero-power glitch lands in %q, want discharging", p.name)
	}
}

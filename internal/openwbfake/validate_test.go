package openwbfake

import "testing"

func TestValidate(t *testing.T) {
	tests := []struct {
		name     string
		topic    string
		payload  string
		accepted bool
	}{
		{"power positive int", "openWB/set/mqtt/bat/2/get/power", "1500", true},
		{"power negative float", "openWB/set/mqtt/bat/2/get/power", "-812.5", true},
		{"power zero", "openWB/set/mqtt/bat/2/get/power", "0", true},
		{"power quoted string", "openWB/set/mqtt/bat/2/get/power", `"1500"`, false},
		{"power not JSON", "openWB/set/mqtt/bat/2/get/power", "1500W", false},
		{"power boolean", "openWB/set/mqtt/bat/2/get/power", "true", false},
		{"power null", "openWB/set/mqtt/bat/2/get/power", "null", false},
		{"power with whitespace", "openWB/set/mqtt/bat/2/get/power", " 1500\n", true},
		{"two values", "openWB/set/mqtt/bat/2/get/power", "1 2", false},
		{"soc in range", "openWB/set/mqtt/bat/2/get/soc", "63", true},
		{"soc above 100", "openWB/set/mqtt/bat/2/get/soc", "101", false},
		{"soc negative", "openWB/set/mqtt/bat/2/get/soc", "-1", false},
		{"imported", "openWB/set/mqtt/bat/2/get/imported", "1234500", true},
		{"exported negative", "openWB/set/mqtt/bat/2/get/exported", "-5", false},
		{"unknown field", "openWB/set/mqtt/bat/2/get/voltage", "230", false},
		{"non-numeric id", "openWB/set/mqtt/bat/x/get/power", "1", false},
		{"wrong prefix", "openWB/mqtt/bat/2/get/power", "1", false},
		{"nested field", "openWB/set/mqtt/bat/2/get/power/extra", "1", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := Validate(tt.topic, []byte(tt.payload), false)
			if v.Accepted != tt.accepted {
				t.Errorf("Accepted = %v (reason %q), want %v", v.Accepted, v.Reason, tt.accepted)
			}
		})
	}
}

func TestValidateParsesIDAndField(t *testing.T) {
	v := Validate("openWB/set/mqtt/bat/12/get/soc", []byte("50"), true)
	if v.BatteryID != 12 || v.Field != "soc" || !v.Retained {
		t.Errorf("verdict = %+v, want battery 12, field soc, retained", v)
	}
}

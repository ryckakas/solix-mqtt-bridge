package config_test

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/ryckakas/solix-mqtt-bridge/internal/config"
)

func env(kv map[string]string) config.LookupFunc {
	return func(k string) (string, bool) {
		v, ok := kv[k]
		return v, ok
	}
}

func TestDefaultsWithTheGenericOutput(t *testing.T) {
	cfg, err := config.Load(env(map[string]string{"SOLARBANK_ADDR": "solarbank", "MQTT_URL": "tcp://broker:1883"}), false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Solarbank.Addr != "solarbank:502" || cfg.Solarbank.UnitID != 1 || cfg.Solarbank.Timeout != 3*time.Second ||
		cfg.Solarbank.AllowAnyModel {
		t.Errorf("solarbank = %+v, want solarbank:502, unit 1, 3s, model check on", cfg.Solarbank)
	}
	if cfg.PollInterval != 5*time.Second || cfg.StaleAfter != 30*time.Second {
		t.Errorf("poll/stale = %s/%s, want 5s/30s", cfg.PollInterval, cfg.StaleAfter)
	}
	if cfg.Filter.SoCJumpConfirm != 10*time.Minute || cfg.Filter.ZeroPowerHoldPolls != 2 {
		t.Errorf("filter = %+v, want 10m confirm, 2 hold polls", cfg.Filter)
	}
	if cfg.Generic == nil || cfg.Generic.BaseTopic != "solix-mqtt-bridge" ||
		cfg.Generic.MQTT.ClientID != "solix-mqtt-bridge" || cfg.Generic.MQTT.PublishTimeout <= 0 {
		t.Errorf("generic = %+v, want default base topic, client id and a publish timeout", cfg.Generic)
	}
	if cfg.OpenWB != nil {
		t.Errorf("openWB = %+v, want nil without OPENWB_MQTT_URL", cfg.OpenWB)
	}
	if cfg.LogLevel != slog.LevelInfo || cfg.LogJSON {
		t.Errorf("log = %v json=%v, want info text", cfg.LogLevel, cfg.LogJSON)
	}
}

func TestOpenWBOutputOnly(t *testing.T) {
	cfg, err := config.Load(env(map[string]string{
		"SOLARBANK_ADDR":           "10.0.0.5:1502",
		"OPENWB_MQTT_URL":          "ssl://openwb:8883",
		"OPENWB_BAT_ID":            "4",
		"OPENWB_MQTT_USERNAME":     "bridge",
		"OPENWB_MQTT_PASSWORD":     "secret",
		"OPENWB_MQTT_TLS_INSECURE": "true",
		"OPENWB_PUBLISH_COUNTERS":  "true",
	}), false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Generic != nil {
		t.Errorf("generic = %+v, want nil without MQTT_URL", cfg.Generic)
	}
	o := cfg.OpenWB
	if o == nil || o.BatteryID != 4 || !o.PublishCounters || o.MQTT.ClientID != "solix-mqtt-bridge-openwb" ||
		o.MQTT.Username != "bridge" || o.MQTT.Password != "secret" || !o.MQTT.TLSInsecure {
		t.Errorf("openWB = %+v", o)
	}
	if cfg.Solarbank.Addr != "10.0.0.5:1502" {
		t.Errorf("addr = %q, want the given host:port kept", cfg.Solarbank.Addr)
	}
}

func TestIPv6HostGetsTheDefaultPort(t *testing.T) {
	cfg, err := config.Load(env(map[string]string{"SOLARBANK_ADDR": "fe80::1", "MQTT_URL": "tcp://b:1883"}), false)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Solarbank.Addr != "[fe80::1]:502" {
		t.Errorf("addr = %q, want [fe80::1]:502", cfg.Solarbank.Addr)
	}
}

func TestProbeNeedsNoOutput(t *testing.T) {
	if _, err := config.Load(env(map[string]string{"SOLARBANK_ADDR": "solarbank"}), true); err != nil {
		t.Errorf("probe without outputs: %v", err)
	}
}

func TestReportsEveryProblemAtOnce(t *testing.T) {
	_, err := config.Load(env(map[string]string{}), false)
	if err == nil {
		t.Fatal("empty environment: want error")
	}
	for _, want := range []string{"SOLARBANK_ADDR is required", "no output configured"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestRejectsInvalidValues(t *testing.T) {
	base := map[string]string{"SOLARBANK_ADDR": "sb", "MQTT_URL": "tcp://b:1883"}
	tests := []struct {
		key, value, want string
	}{
		{"POLL_INTERVAL", "often", "POLL_INTERVAL"},
		{"POLL_INTERVAL", "-5s", "POLL_INTERVAL"},
		{"STALE_AFTER", "5s", "must be longer than POLL_INTERVAL"},
		{"SOLARBANK_UNIT_ID", "0", "SOLARBANK_UNIT_ID"},
		{"SOLARBANK_UNIT_ID", "300", "SOLARBANK_UNIT_ID"},
		{"SOLARBANK_ALLOW_ANY_MODEL", "maybe", "SOLARBANK_ALLOW_ANY_MODEL"},
		{"LOG_LEVEL", "loud", "LOG_LEVEL"},
		{"LOG_FORMAT", "xml", "LOG_FORMAT"},
		{"MQTT_BASE_TOPIC", "solix/#", "MQTT_BASE_TOPIC"},
		{"MQTT_BASE_TOPIC", "solix/", "MQTT_BASE_TOPIC"},
		{"OPENWB_MQTT_URL", "tcp://openwb:1883", "OPENWB_BAT_ID is required"},
	}
	for _, tt := range tests {
		kv := map[string]string{tt.key: tt.value}
		for k, v := range base {
			kv[k] = v
		}
		_, err := config.Load(env(kv), false)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s=%q: err = %v, want it to mention %q", tt.key, tt.value, err, tt.want)
		}
	}
}

func TestRejectsANegativeBatteryID(t *testing.T) {
	_, err := config.Load(env(map[string]string{
		"SOLARBANK_ADDR": "sb", "OPENWB_MQTT_URL": "tcp://openwb:1883", "OPENWB_BAT_ID": "-1",
	}), false)
	if err == nil || !strings.Contains(err.Error(), "OPENWB_BAT_ID") {
		t.Errorf("err = %v, want an OPENWB_BAT_ID error", err)
	}
}

func TestRejectsSharedClientIDOnTheSameBroker(t *testing.T) {
	_, err := config.Load(env(map[string]string{
		"SOLARBANK_ADDR":        "sb",
		"MQTT_URL":              "tcp://openwb:1883",
		"MQTT_BASE_TOPIC":       "others/solix",
		"OPENWB_MQTT_URL":       "tcp://openwb:1883",
		"OPENWB_BAT_ID":         "1",
		"OPENWB_MQTT_CLIENT_ID": "solix-mqtt-bridge",
	}), false)
	if err == nil || !strings.Contains(err.Error(), "must differ") {
		t.Errorf("err = %v, want a client id collision error", err)
	}
}

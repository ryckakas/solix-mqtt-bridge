// Package config reads the bridge's settings from environment variables and validates them, reporting every problem
// at once. No device or broker address has a default, so nothing can reach a real device by accident.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/ryckakas/solix-mqtt-bridge/internal/mqttclient"
	"github.com/ryckakas/solix-mqtt-bridge/internal/mqttout"
	"github.com/ryckakas/solix-mqtt-bridge/internal/openwb"
	"github.com/ryckakas/solix-mqtt-bridge/internal/plausibility"
	"github.com/ryckakas/solix-mqtt-bridge/internal/solarbank"
)

const (
	publishTimeout     = 5 * time.Second
	zeroPowerHoldPolls = 2
)

// LookupFunc reads one environment variable; os.LookupEnv satisfies it.
type LookupFunc func(key string) (string, bool)

// Config is the validated configuration.
type Config struct {
	// Solarbank is the Modbus connection to the device.
	Solarbank solarbank.Config
	// PollInterval is how often the device is read.
	PollInterval time.Duration
	// StaleAfter is how long after the last good read the data counts as stale.
	StaleAfter time.Duration
	// Filter tunes the plausibility filters.
	Filter plausibility.Config
	// Generic is the generic MQTT output; nil when MQTT_URL is unset.
	Generic *mqttout.Config
	// OpenWB is the openWB output; nil when OPENWB_MQTT_URL is unset.
	OpenWB *openwb.Config
	// LogLevel is the minimum level logged.
	LogLevel slog.Level
	// LogJSON selects JSON log lines instead of text, on the console and in the log files.
	LogJSON bool
	// LogDir is the directory for daily log files; empty disables file logging.
	LogDir string
	// LogRetentionDays is how many full days of log files are kept before today's, 1 to 365.
	LogRetentionDays int
	// LogFileLevel is the minimum level written to the log files; the default debug includes one line per poll.
	LogFileLevel slog.Level
}

type reader struct {
	lookup LookupFunc
	errs   []error
}

// Load reads and validates the configuration. With probe set, no output is required, because probe mode only reads
// the device once.
func Load(lookup LookupFunc, probe bool) (Config, error) {
	r := &reader{lookup: lookup}
	cfg := Config{
		Solarbank: solarbank.Config{
			Addr:          r.solarbankAddr(),
			UnitID:        r.unitID("SOLARBANK_UNIT_ID", 1),
			Timeout:       r.duration("MODBUS_TIMEOUT", 3*time.Second),
			AllowAnyModel: r.boolean("SOLARBANK_ALLOW_ANY_MODEL", false),
		},
		PollInterval: r.duration("POLL_INTERVAL", 5*time.Second),
		StaleAfter:   r.duration("STALE_AFTER", 30*time.Second),
		Filter: plausibility.Config{
			SoCJumpConfirm:     r.duration("SOC_JUMP_CONFIRM", 10*time.Minute),
			ZeroPowerHoldPolls: zeroPowerHoldPolls,
		},
		Generic:          r.generic(),
		OpenWB:           r.openWB(),
		LogLevel:         r.logLevel("LOG_LEVEL", "info"),
		LogJSON:          r.logJSON(),
		LogDir:           r.str("LOG_DIR", ""),
		LogRetentionDays: r.retentionDays("LOG_RETENTION_DAYS", 7),
		LogFileLevel:     r.logLevel("LOG_FILE_LEVEL", "debug"),
	}
	if cfg.StaleAfter <= cfg.PollInterval {
		r.fail("STALE_AFTER (%s) must be longer than POLL_INTERVAL (%s)", cfg.StaleAfter, cfg.PollInterval)
	}
	if !probe && cfg.Generic == nil && cfg.OpenWB == nil {
		r.fail("no output configured: set MQTT_URL (generic MQTT) and/or OPENWB_MQTT_URL (openWB)")
	}
	if cfg.Generic != nil && cfg.OpenWB != nil && cfg.Generic.MQTT.URL == cfg.OpenWB.MQTT.URL &&
		cfg.Generic.MQTT.ClientID == cfg.OpenWB.MQTT.ClientID {
		r.fail("MQTT_CLIENT_ID and OPENWB_MQTT_CLIENT_ID must differ on the same broker")
	}
	return cfg, errors.Join(r.errs...)
}

func (r *reader) solarbankAddr() string {
	addr := r.str("SOLARBANK_ADDR", "")
	if addr == "" {
		r.fail("SOLARBANK_ADDR is required (host or host:port of the Solarbank's Modbus TCP interface)")
		return ""
	}
	if _, _, err := net.SplitHostPort(addr); err != nil {
		return net.JoinHostPort(addr, "502")
	}
	return addr
}

func (r *reader) generic() *mqttout.Config {
	conn, ok := r.mqtt("MQTT_", "solix-mqtt-bridge")
	if !ok {
		return nil
	}
	base := r.str("MQTT_BASE_TOPIC", "solix-mqtt-bridge")
	if base == "" || strings.ContainsAny(base, "#+") || strings.HasPrefix(base, "/") || strings.HasSuffix(base, "/") {
		r.fail("MQTT_BASE_TOPIC %q: must be non-empty, without wildcards or leading/trailing slashes", base)
	}
	return &mqttout.Config{MQTT: conn, BaseTopic: base}
}

func (r *reader) openWB() *openwb.Config {
	conn, ok := r.mqtt("OPENWB_MQTT_", "solix-mqtt-bridge-openwb")
	if !ok {
		return nil
	}
	id := -1
	if raw, set := r.lookup("OPENWB_BAT_ID"); !set || raw == "" {
		r.fail("OPENWB_BAT_ID is required with OPENWB_MQTT_URL (the component id of openWB's MQTT battery)")
	} else if n, err := strconv.Atoi(raw); err != nil || n < 0 {
		r.fail("OPENWB_BAT_ID %q: must be a non-negative integer", raw)
	} else {
		id = n
	}
	return &openwb.Config{MQTT: conn, BatteryID: id, PublishCounters: r.boolean("OPENWB_PUBLISH_COUNTERS", false)}
}

func (r *reader) mqtt(prefix, defaultClientID string) (mqttclient.Config, bool) {
	url := r.str(prefix+"URL", "")
	if url == "" {
		return mqttclient.Config{}, false
	}
	return mqttclient.Config{
		URL:            url,
		ClientID:       r.str(prefix+"CLIENT_ID", defaultClientID),
		Username:       r.str(prefix+"USERNAME", ""),
		Password:       r.str(prefix+"PASSWORD", ""),
		CAFile:         r.str(prefix+"CA_FILE", ""),
		TLSInsecure:    r.boolean(prefix+"TLS_INSECURE", false),
		PublishTimeout: publishTimeout,
	}, true
}

func (r *reader) logLevel(key, fallback string) slog.Level {
	var level slog.Level
	raw := r.str(key, fallback)
	if err := level.UnmarshalText([]byte(raw)); err != nil {
		r.fail("%s %q: must be debug, info, warn or error", key, raw)
	}
	return level
}

func (r *reader) logJSON() bool {
	switch raw := r.str("LOG_FORMAT", "text"); raw {
	case "text":
		return false
	case "json":
		return true
	default:
		r.fail("LOG_FORMAT %q: must be text or json", raw)
		return false
	}
}

func (r *reader) str(key, fallback string) string {
	if v, ok := r.lookup(key); ok && v != "" {
		return v
	}
	return fallback
}

func (r *reader) duration(key string, fallback time.Duration) time.Duration {
	raw, ok := r.lookup(key)
	if !ok || raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		r.fail("%s %q: must be a positive duration such as 5s or 10m", key, raw)
		return fallback
	}
	return d
}

func (r *reader) boolean(key string, fallback bool) bool {
	raw, ok := r.lookup(key)
	if !ok || raw == "" {
		return fallback
	}
	b, err := strconv.ParseBool(raw)
	if err != nil {
		r.fail("%s %q: must be true or false", key, raw)
		return fallback
	}
	return b
}

func (r *reader) unitID(key string, fallback uint8) uint8 {
	raw, ok := r.lookup(key)
	if !ok || raw == "" {
		return fallback
	}
	n, err := strconv.ParseUint(raw, 10, 8)
	if err != nil || n == 0 || n > 247 {
		r.fail("%s %q: must be a Modbus unit id between 1 and 247", key, raw)
		return fallback
	}
	return uint8(n)
}

func (r *reader) retentionDays(key string, fallback int) int {
	raw, ok := r.lookup(key)
	if !ok || raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > 365 {
		r.fail("%s %q: must be a number of days between 1 and 365", key, raw)
		return fallback
	}
	return n
}

func (r *reader) fail(format string, args ...any) {
	r.errs = append(r.errs, fmt.Errorf(format, args...))
}

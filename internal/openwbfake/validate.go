// Package openwbfake stands in for openWB 2.x's handling of the generic MQTT battery module: it validates what
// arrives on openWB/set/mqtt/bat/<id>/get/<field> the way openWB does. It is a test stand-in only and must never be
// linked into the bridge.
package openwbfake

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Verdict is the outcome of validating one message.
type Verdict struct {
	// Topic is the topic the message arrived on.
	Topic string
	// BatteryID is the component id parsed from the topic; -1 if the topic didn't parse.
	BatteryID int
	// Field is the last topic segment, e.g. "power".
	Field string
	// Payload is the raw message body.
	Payload string
	// Retained reports whether the message was published retained; openWB expects non-retained set messages.
	Retained bool
	// Accepted reports whether openWB would take the value.
	Accepted bool
	// Reason explains a rejection.
	Reason string
}

// Validate checks one message the way openWB 2.x validates generic MQTT battery input: the payload must be a bare
// JSON number, soc must lie in 0 to 100, and imported/exported must not be negative.
func Validate(topic string, payload []byte, retained bool) Verdict {
	v := Verdict{Topic: topic, BatteryID: -1, Payload: string(payload), Retained: retained}
	id, field, ok := parseTopic(topic)
	if !ok {
		v.Reason = "not a generic MQTT battery topic"
		return v
	}
	v.BatteryID, v.Field = id, field
	value, err := parseNumber(payload)
	if err != nil {
		v.Reason = err.Error()
		return v
	}
	v.Accepted, v.Reason = checkRange(field, value)
	return v
}

// Mirrors openWB's json.loads: exactly one JSON value, no trailing data, and it must be a number, not a string.
func parseNumber(payload []byte) (float64, error) {
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.UseNumber()
	var val any
	if err := dec.Decode(&val); err != nil {
		return 0, fmt.Errorf("payload is not JSON: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return 0, errors.New("payload has trailing data after the JSON value")
	}
	num, ok := val.(json.Number)
	if !ok {
		return 0, fmt.Errorf("payload is a JSON %T, not a number", val)
	}
	return num.Float64()
}

func parseTopic(topic string) (int, string, bool) {
	rest, ok := strings.CutPrefix(topic, "openWB/set/mqtt/bat/")
	if !ok {
		return 0, "", false
	}
	idPart, field, ok := strings.Cut(rest, "/get/")
	if !ok || strings.Contains(field, "/") {
		return 0, "", false
	}
	id, err := strconv.Atoi(idPart)
	if err != nil || id < 0 {
		return 0, "", false
	}
	return id, field, true
}

func checkRange(field string, value float64) (bool, string) {
	switch field {
	case "power":
		return true, ""
	case "soc":
		if value < 0 || value > 100 {
			return false, fmt.Sprintf("soc %v outside 0..100", value)
		}
		return true, ""
	case "imported", "exported":
		if value < 0 {
			return false, fmt.Sprintf("%s %v is negative", field, value)
		}
		return true, ""
	default:
		return false, fmt.Sprintf("unknown field %q", field)
	}
}

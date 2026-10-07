package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/ryckakas/solix-mqtt-bridge/internal/plausibility"
	"github.com/ryckakas/solix-mqtt-bridge/internal/solarbank"
)

var t0 = time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)

type fakeSource struct {
	clock      *time.Time
	connectErr error
	readErr    error
	snap       solarbank.Snapshot
	connects   int
	closes     int
}

func (f *fakeSource) Connect(context.Context) (solarbank.Identity, error) {
	f.connects++
	if f.connectErr != nil {
		return solarbank.Identity{}, f.connectErr
	}
	return solarbank.Identity{Model: solarbank.ModelMaxAC, Serial: "SN1", Firmware: "v1"}, nil
}

func (f *fakeSource) Read(context.Context) (solarbank.Snapshot, error) {
	if f.readErr != nil {
		return solarbank.Snapshot{}, f.readErr
	}
	s := f.snap
	s.At = *f.clock
	return s, nil
}

func (f *fakeSource) Close() error {
	f.closes++
	return nil
}

type recordingOutput struct {
	mu     sync.Mutex
	states []State
	closed bool
}

func (o *recordingOutput) Publish(_ context.Context, s State) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.states = append(o.states, s)
	return nil
}

func (o *recordingOutput) Close(context.Context) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.closed = true
	return nil
}

func (o *recordingOutput) last(t *testing.T) State {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.states) == 0 {
		t.Fatal("nothing published")
	}
	return o.states[len(o.states)-1]
}

func setup() (*Bridge, *fakeSource, *recordingOutput, *time.Time) {
	clock := t0
	src := &fakeSource{clock: &clock, snap: solarbank.Snapshot{
		Status: solarbank.StatusCharging, BatteryPowerW: -1500, SoCPercent: 63,
		MaxChargePowerW: 2500, MaxDischargePowerW: 2500, CapacityWh: 5000,
	}}
	out := &recordingOutput{}
	b := New(Config{
		PollInterval: 5 * time.Second,
		StaleAfter:   30 * time.Second,
		Filter:       plausibility.Config{SoCJumpConfirm: 10 * time.Minute, ZeroPowerHoldPolls: 2},
	}, src, []Output{out}, slog.New(slog.DiscardHandler))
	b.now = func() time.Time { return clock }
	return b, src, out, &clock
}

func TestFirstGoodReadIsFresh(t *testing.T) {
	b, _, out, _ := setup()
	if err := b.poll(t.Context()); err != nil {
		t.Fatalf("poll: %v", err)
	}
	s := out.last(t)
	if !s.Fresh || !s.HasData() || s.Identity.Model != solarbank.ModelMaxAC {
		t.Errorf("state = fresh %v, data %v, model %q; want fresh with identity", s.Fresh, s.HasData(), s.Identity.Model)
	}
	if s.Reading.ChargePowerW != 1500 || s.Raw.BatteryPowerW != -1500 {
		t.Errorf("power filtered/raw = %d/%d, want 1500/-1500", s.Reading.ChargePowerW, s.Raw.BatteryPowerW)
	}
}

func TestGoesStaleAfterTheThresholdAndRecovers(t *testing.T) {
	b, src, out, clock := setup()
	mustPoll(t, b)

	src.readErr = errors.New("connection reset")
	for _, sec := range []int{5, 10, 25} {
		*clock = t0.Add(time.Duration(sec) * time.Second)
		mustPoll(t, b)
		if !out.last(t).Fresh {
			t.Fatalf("t=%ds: stale before STALE_AFTER", sec)
		}
	}
	*clock = t0.Add(30 * time.Second)
	mustPoll(t, b)
	if s := out.last(t); s.Fresh || !s.HasData() {
		t.Fatalf("t=30s: fresh %v data %v, want stale with old data", s.Fresh, s.HasData())
	}

	src.readErr = nil
	*clock = t0.Add(35 * time.Second)
	mustPoll(t, b)
	if s := out.last(t); !s.Fresh || !s.LastGoodRead.Equal(*clock) {
		t.Errorf("after recovery: fresh %v last read %v, want fresh at %v", s.Fresh, s.LastGoodRead, *clock)
	}
}

func TestReconnectsAfterAFailedRead(t *testing.T) {
	b, src, _, _ := setup()
	mustPoll(t, b)
	src.readErr = errors.New("broken pipe")
	mustPoll(t, b)
	if src.closes != 1 {
		t.Errorf("closes after a failed read = %d, want 1", src.closes)
	}
	src.readErr = nil
	mustPoll(t, b)
	if src.connects != 2 {
		t.Errorf("connects = %d, want 2 (initial + reconnect)", src.connects)
	}
}

func TestKeepsPublishingWhileTheDeviceIsUnreachable(t *testing.T) {
	b, src, out, clock := setup()
	src.connectErr = errors.New("connection refused")
	for i := range 3 {
		*clock = t0.Add(time.Duration(5*i) * time.Second)
		mustPoll(t, b)
	}
	if len(out.states) != 3 {
		t.Fatalf("published %d states, want one per poll", len(out.states))
	}
	if s := out.last(t); s.Fresh || s.HasData() {
		t.Errorf("state = fresh %v data %v, want waiting", s.Fresh, s.HasData())
	}
	if src.connects != 3 {
		t.Errorf("connects = %d, want one attempt per poll", src.connects)
	}
}

func TestFilterCountsReachTheOutputs(t *testing.T) {
	b, src, out, clock := setup()
	mustPoll(t, b)
	src.snap.SoCPercent = 95
	*clock = t0.Add(5 * time.Second)
	mustPoll(t, b)
	s := out.last(t)
	if s.Reading.SoCPercent != 63 || s.FilterCounts[plausibility.SoCJump] != 1 {
		t.Errorf("soc %d, soc-jump count %d; want the held 63 and 1", s.Reading.SoCPercent, s.FilterCounts[plausibility.SoCJump])
	}
}

func TestUnsupportedModelIsFatalAndClosesOutputs(t *testing.T) {
	b, src, out, _ := setup()
	src.connectErr = fmt.Errorf("%w: device reports %q", solarbank.ErrUnsupportedModel, "A17X8")
	err := b.Run(t.Context())
	if !errors.Is(err, solarbank.ErrUnsupportedModel) {
		t.Fatalf("Run: err = %v, want ErrUnsupportedModel", err)
	}
	if !out.closed {
		t.Error("outputs not closed after a fatal error")
	}
}

func TestRunClosesOutputsAndSourceOnCancel(t *testing.T) {
	b, src, out, _ := setup()
	b.cfg.PollInterval = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- b.Run(ctx) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	if !out.closed || src.closes != 1 {
		t.Errorf("closed output %v, source closes %d; want true and 1", out.closed, src.closes)
	}
}

func TestEveryGoodReadLogsAPollLine(t *testing.T) {
	b, src, _, clock := setup()
	var logs bytes.Buffer
	b.logger = slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	mustPoll(t, b)
	src.readErr = errors.New("connection reset")
	*clock = t0.Add(5 * time.Second)
	mustPoll(t, b)

	var polls []map[string]any
	for line := range bytes.Lines(logs.Bytes()) {
		var rec map[string]any
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}
		if rec["msg"] == "poll" {
			polls = append(polls, rec)
		}
	}
	if len(polls) != 1 {
		t.Fatalf("poll lines = %d, want 1 (one good read, one failed)", len(polls))
	}
	want := map[string]any{
		"status": "charging", "raw_battery_power_w": -1500.0, "charge_power_w": 1500.0,
		"raw_soc_percent": 63.0, "soc_percent": 63.0, "pv_power_w": 0.0, "home_load_w": 0.0, "grid_power_w": 0.0,
	}
	for k, v := range want {
		if polls[0][k] != v {
			t.Errorf("poll %s = %v, want %v", k, polls[0][k], v)
		}
	}
}

func mustPoll(t *testing.T, b *Bridge) {
	t.Helper()
	if err := b.poll(t.Context()); err != nil {
		t.Fatalf("poll: %v", err)
	}
}

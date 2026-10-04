// Command solarbank-sim serves a simulated Solarbank Max AC over Modbus TCP for the local dev stack. It is a test
// stand-in and never talks to a real device.
//
// Environment: SIM_LISTEN (host:port, default 0.0.0.0:5502), SIM_UNIT_ID (default 1), SIM_GLITCHES (true injects
// the device quirks the bridge filters: a 30 s SoC jump and a one-poll 0 W reading while discharging).
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/ryckakas/solix-mqtt-bridge/internal/simulator"
)

type phase struct {
	name          string
	seconds       int
	batteryPowerW int32
	pvPowerW      int32
	homeLoadW     int32
}

var cycle = []phase{
	{name: "charging", seconds: 120, batteryPowerW: -1500, pvPowerW: 2000, homeLoadW: 500},
	{name: "idle", seconds: 60},
	{name: "discharging", seconds: 120, batteryPowerW: 800, homeLoadW: 800},
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(logger); err != nil {
		logger.Error("solarbank-sim failed", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	unitID, err := strconv.ParseUint(envOr("SIM_UNIT_ID", "1"), 10, 8)
	if err != nil {
		return fmt.Errorf("SIM_UNIT_ID: %w", err)
	}
	glitches := os.Getenv("SIM_GLITCHES") == "true"

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dev := simulator.NewDevice(uint8(unitID), simulator.DefaultState())
	libLog := slog.NewLogLogger(logger.Handler(), slog.LevelDebug)
	srv, err := simulator.Listen(ctx, dev, envOr("SIM_LISTEN", "0.0.0.0:5502"), libLog)
	if err != nil {
		return err
	}
	logger.Info("serving simulated Solarbank", "addr", srv.Addr(), "unit_id", unitID, "glitches", glitches)
	runScenario(ctx, dev, logger, glitches)
	return srv.Stop()
}

func runScenario(ctx context.Context, dev *simulator.Device, logger *slog.Logger, glitches bool) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	current := ""
	for tick := 0; ; tick++ {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		p, pos := phaseAt(tick)
		if p.name != current {
			current = p.name
			logger.Info("phase", "name", p.name, "battery_power_w", p.batteryPowerW)
		}
		dev.Update(func(s *simulator.State) {
			s.BatteryPowerW, s.PVPowerW, s.HomeLoadW = p.batteryPowerW, p.pvPowerW, p.homeLoadW
		})
		dev.Step(time.Second)
		if glitches {
			injectGlitch(dev, pos)
		}
	}
}

func phaseAt(tick int) (phase, int) {
	total := 0
	for _, p := range cycle {
		total += p.seconds
	}
	pos := tick % total
	offset := pos
	for _, p := range cycle {
		if offset < p.seconds {
			return p, pos
		}
		offset -= p.seconds
	}
	return cycle[len(cycle)-1], pos
}

// Positions are seconds into the cycle: the SoC jump lands while charging, the 0 W reading while discharging.
func injectGlitch(dev *simulator.Device, pos int) {
	switch {
	case pos >= 30 && pos < 60:
		dev.Update(func(s *simulator.State) { s.SoCPercent = min(100, s.SoCPercent+35) })
	case pos == 240:
		dev.Update(func(s *simulator.State) { s.BatteryPowerW = 0 })
	}
}

func envOr(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

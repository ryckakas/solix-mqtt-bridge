// Command solix-mqtt-bridge reads an Anker SOLIX Solarbank over local Modbus TCP and publishes its battery data over
// MQTT: as generic topics for any consumer and, optionally, to an openWB 2.x wallbox.
//
// It is configured through environment variables (see the README). With -probe it reads the device once, prints
// the identity and snapshot as JSON, and exits without touching MQTT.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ryckakas/solix-mqtt-bridge/internal/bridge"
	"github.com/ryckakas/solix-mqtt-bridge/internal/config"
	"github.com/ryckakas/solix-mqtt-bridge/internal/mqttout"
	"github.com/ryckakas/solix-mqtt-bridge/internal/openwb"
	"github.com/ryckakas/solix-mqtt-bridge/internal/solarbank"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	os.Exit(run(os.Args[1:], os.LookupEnv, os.Stdout, os.Stderr))
}

func run(args []string, lookup config.LookupFunc, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("solix-mqtt-bridge", flag.ContinueOnError)
	fs.SetOutput(stderr)
	probe := fs.Bool("probe", false, "read the Solarbank once, print identity and snapshot as JSON, and exit (no MQTT)")
	showVersion := fs.Bool("version", false, "print the version and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		_, _ = fmt.Fprintf(stdout, "solix-mqtt-bridge %s (%s, %s)\n", version, commit, date)
		return 0
	}
	cfg, err := config.Load(lookup, *probe)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "solix-mqtt-bridge: invalid configuration:\n%v\n", err)
		return 2
	}
	logger := newLogger(cfg, stderr)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	reader := solarbank.NewReader(cfg.Solarbank)
	if *probe {
		return probeOnce(ctx, reader, stdout, logger)
	}
	return serve(ctx, cfg, reader, logger)
}

func serve(ctx context.Context, cfg config.Config, src bridge.Source, logger *slog.Logger) int {
	outputs, err := openOutputs(cfg, logger)
	if err != nil {
		logger.Error("cannot start outputs", "err", err)
		return 1
	}
	logger.Info("solix-mqtt-bridge starting", "version", version, "solarbank", cfg.Solarbank.Addr,
		"generic_output", cfg.Generic != nil, "openwb_output", cfg.OpenWB != nil)
	b := bridge.New(bridge.Config{PollInterval: cfg.PollInterval, StaleAfter: cfg.StaleAfter, Filter: cfg.Filter},
		src, outputs, logger)
	if err := b.Run(ctx); err != nil {
		logger.Error("solix-mqtt-bridge stopped", "err", err)
		return 1
	}
	logger.Info("solix-mqtt-bridge stopped")
	return 0
}

func openOutputs(cfg config.Config, logger *slog.Logger) ([]bridge.Output, error) {
	var outputs []bridge.Output
	if cfg.Generic != nil {
		out, err := mqttout.New(*cfg.Generic, logger.With("output", "mqtt"))
		if err != nil {
			return nil, err
		}
		outputs = append(outputs, out)
	}
	if cfg.OpenWB != nil {
		out, err := openwb.New(*cfg.OpenWB, logger.With("output", "openwb"))
		if err != nil {
			return nil, err
		}
		outputs = append(outputs, out)
	}
	return outputs, nil
}

type probeDoc struct {
	Model              string    `json:"model"`
	Serial             string    `json:"serial"`
	Firmware           string    `json:"firmware"`
	ReadAt             time.Time `json:"read_at"`
	Status             string    `json:"status"`
	BatteryPowerW      int32     `json:"battery_power_w"`
	ChargePowerW       int64     `json:"charge_power_w"`
	SoCPercent         uint16    `json:"soc_percent"`
	PVPowerW           int32     `json:"pv_power_w"`
	ThirdPartyPVPowerW int32     `json:"third_party_pv_power_w"`
	HomeLoadW          int32     `json:"home_load_w"`
	GridPowerW         int32     `json:"grid_power_w"`
	MaxChargePowerW    int64     `json:"max_charge_power_w"`
	MaxDischargePowerW int64     `json:"max_discharge_power_w"`
	CapacityWh         uint64    `json:"capacity_wh"`
	SectionStatus      uint16    `json:"section_status"`
	SectionSoCPercent  uint16    `json:"section_soc_percent"`
	ChargedTotalWh     uint64    `json:"charged_total_wh"`
	DischargedTotalWh  uint64    `json:"discharged_total_wh"`
}

func probeOnce(ctx context.Context, r *solarbank.Reader, stdout io.Writer, logger *slog.Logger) int {
	id, err := r.Connect(ctx)
	if err != nil {
		logger.Error("probe: connect failed", "err", err)
		return 1
	}
	defer func() { _ = r.Close() }()
	s, err := r.Read(ctx)
	if err != nil {
		logger.Error("probe: read failed", "err", err)
		return 1
	}
	doc := probeDoc{
		Model: id.Model, Serial: id.Serial, Firmware: id.Firmware, ReadAt: s.At.UTC(), Status: s.Status.String(),
		BatteryPowerW: s.BatteryPowerW, ChargePowerW: s.ChargePowerW(), SoCPercent: s.SoCPercent,
		PVPowerW: s.PVPowerW, ThirdPartyPVPowerW: s.ThirdPartyPVPowerW, HomeLoadW: s.HomeLoadW, GridPowerW: s.GridPowerW,
		MaxChargePowerW: s.MaxChargePowerW, MaxDischargePowerW: s.MaxDischargePowerW, CapacityWh: s.CapacityWh,
		SectionStatus: s.SectionStatus, SectionSoCPercent: s.SectionSoCPercent,
		ChargedTotalWh: s.ChargedTotalWh, DischargedTotalWh: s.DischargedTotalWh,
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		logger.Error("probe: write output", "err", err)
		return 1
	}
	return 0
}

func newLogger(cfg config.Config, w io.Writer) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.LogLevel}
	if cfg.LogJSON {
		return slog.New(slog.NewJSONHandler(w, opts))
	}
	return slog.New(slog.NewTextHandler(w, opts))
}

package bridge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ryckakas/solix-mqtt-bridge/internal/plausibility"
	"github.com/ryckakas/solix-mqtt-bridge/internal/solarbank"
)

const closeTimeout = 5 * time.Second

// Source is where the loop reads from; *solarbank.Reader satisfies it.
type Source interface {
	Connect(ctx context.Context) (solarbank.Identity, error)
	Read(ctx context.Context) (solarbank.Snapshot, error)
	Close() error
}

// Config tunes the loop.
type Config struct {
	// PollInterval is how often the device is read.
	PollInterval time.Duration
	// StaleAfter is how long after the last good read the data counts as stale.
	StaleAfter time.Duration
	// Filter tunes the plausibility filters.
	Filter plausibility.Config
}

// Bridge polls a Source and fans every poll's State out to the outputs. It is not safe for concurrent use.
type Bridge struct {
	cfg     Config
	src     Source
	outputs []Output
	logger  *slog.Logger
	now     func() time.Time

	filter    *plausibility.Filter
	state     State
	connected bool
	failing   bool
	active    map[plausibility.Name]bool
}

// New returns a Bridge; nothing happens until Run.
func New(cfg Config, src Source, outputs []Output, logger *slog.Logger) *Bridge {
	return &Bridge{
		cfg:     cfg,
		src:     src,
		outputs: outputs,
		logger:  logger,
		now:     time.Now,
		filter:  plausibility.New(cfg.Filter),
		active:  map[plausibility.Name]bool{},
	}
}

// Run polls until ctx is canceled, then closes every output (which neutralizes what it published) and the source.
// It returns early only on a fatal error, such as solarbank.ErrUnsupportedModel.
func (b *Bridge) Run(ctx context.Context) error {
	ticker := time.NewTicker(b.cfg.PollInterval)
	defer ticker.Stop()
	for {
		if err := b.poll(ctx); err != nil {
			return errors.Join(err, b.shutdown(ctx))
		}
		select {
		case <-ctx.Done():
			return b.shutdown(ctx)
		case <-ticker.C:
		}
	}
}

func (b *Bridge) poll(ctx context.Context) error {
	if err := b.read(ctx); err != nil {
		return err
	}
	b.updateFreshness()
	for _, out := range b.outputs {
		if err := out.Publish(ctx, b.state); err != nil {
			b.logger.Warn("publish failed", "err", err)
		}
	}
	return nil
}

func (b *Bridge) read(ctx context.Context) error {
	if !b.connected {
		id, err := b.src.Connect(ctx)
		if errors.Is(err, solarbank.ErrUnsupportedModel) {
			return err
		}
		if err != nil {
			b.readFailed("connect", err)
			return nil
		}
		b.connected, b.state.Identity = true, id
		b.logger.Info("connected to Solarbank", "model", id.Model, "serial", id.Serial, "firmware", id.Firmware)
	}
	snap, err := b.src.Read(ctx)
	if err != nil {
		b.readFailed("read", err)
		b.connected = false
		if cerr := b.src.Close(); cerr != nil {
			b.logger.Debug("close after failed read", "err", cerr)
		}
		return nil
	}
	if b.failing {
		b.failing = false
		b.logger.Info("Solarbank reads recovered")
	}
	reading := b.filter.Apply(snap)
	b.logger.Debug("poll", "status", snap.Status.String(),
		"raw_battery_power_w", snap.BatteryPowerW, "charge_power_w", reading.ChargePowerW,
		"raw_soc_percent", snap.SoCPercent, "soc_percent", reading.SoCPercent,
		"pv_power_w", snap.PVPowerW, "home_load_w", snap.HomeLoadW, "grid_power_w", snap.GridPowerW)
	b.logRejections(ctx, reading, snap.Status)
	b.state.Raw, b.state.Reading, b.state.LastGoodRead = snap, reading, snap.At
	b.state.FilterCounts = b.filter.Counts()
	return nil
}

// The first failure of a streak is a warning; repeats are debug noise until reads recover.
func (b *Bridge) readFailed(op string, err error) {
	if b.failing {
		b.logger.Debug("Solarbank "+op+" failed", "err", err)
		return
	}
	b.failing = true
	b.logger.Warn("Solarbank "+op+" failed; retrying every poll", "err", err)
}

func (b *Bridge) updateFreshness() {
	fresh := b.state.HasData() && b.now().Sub(b.state.LastGoodRead) < b.cfg.StaleAfter
	switch {
	case b.state.Fresh && !fresh:
		b.logger.Warn("Solarbank data is stale; outputs report it unavailable", "last_good_read", b.state.LastGoodRead)
	case !b.state.Fresh && fresh:
		b.logger.Info("Solarbank data is fresh")
	}
	b.state.Fresh = fresh
}

func (b *Bridge) logRejections(ctx context.Context, r plausibility.Reading, status solarbank.BatteryStatus) {
	firing := map[plausibility.Name]bool{}
	for _, rej := range r.Rejections {
		firing[rej.Filter] = true
		level := slog.LevelDebug
		if !b.active[rej.Filter] {
			level = slog.LevelWarn
		}
		b.logger.Log(ctx, level, "plausibility filter replaced a reading",
			"filter", rej.Filter, "raw", rej.Raw, "kept", rej.Kept, "status", status)
	}
	b.active = firing
}

func (b *Bridge) shutdown(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), closeTimeout)
	defer cancel()
	var errs []error
	for _, out := range b.outputs {
		if err := out.Close(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if b.connected {
		if err := b.src.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close Solarbank connection: %w", err))
		}
	}
	return errors.Join(errs...)
}

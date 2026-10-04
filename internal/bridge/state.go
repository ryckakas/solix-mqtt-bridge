// Package bridge runs the poll loop: it reads the Solarbank, filters each reading, tracks whether the data is fresh,
// and hands every poll's State to the configured outputs.
package bridge

import (
	"context"
	"time"

	"github.com/ryckakas/solix-mqtt-bridge/internal/plausibility"
	"github.com/ryckakas/solix-mqtt-bridge/internal/solarbank"
)

// State is everything an output needs to know after one poll.
type State struct {
	// Identity is the device identity from the current connection; zero before the first successful connect.
	Identity solarbank.Identity
	// Fresh reports whether the last good read is younger than the stale threshold.
	Fresh bool
	// LastGoodRead is when the last snapshot was read; zero until the first one.
	LastGoodRead time.Time
	// Raw is the last snapshot read, unfiltered and in the device's sign convention.
	Raw solarbank.Snapshot
	// Reading is the filtered view of Raw.
	Reading plausibility.Reading
	// FilterCounts is how often each plausibility rule has fired since start.
	FilterCounts map[plausibility.Name]uint64
}

// HasData reports whether at least one snapshot has been read since start.
func (s State) HasData() bool {
	return !s.LastGoodRead.IsZero()
}

// Output publishes States to one consumer.
type Output interface {
	// Publish is called once per poll with the current state; it must not block much longer than one publish.
	Publish(ctx context.Context, s State) error
	// Close neutralizes what the output published (for example availability offline, or power 0) and disconnects
	// cleanly.
	Close(ctx context.Context) error
}

// Command openwb-fake stands in for openWB 2.x in the local dev stack: it validates generic MQTT battery messages
// the way openWB does and logs every verdict. It is a test stand-in and never talks to a real openWB.
//
// Environment: MQTT_URL (required, e.g. tcp://mosquitto:1883).
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ryckakas/solix-mqtt-bridge/internal/openwbfake"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	url := os.Getenv("MQTT_URL")
	if url == "" {
		logger.Error("MQTT_URL is required")
		os.Exit(2)
	}
	fake, err := openwbfake.Start(url, "openwb-fake", logger)
	if err != nil {
		logger.Error("openwb-fake failed", "err", err)
		os.Exit(1)
	}
	defer fake.Close()
	logger.Info("fake openWB listening", "broker", url, "topics", "openWB/set/mqtt/bat/+/get/+")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
}

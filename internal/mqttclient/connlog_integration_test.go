//go:build integration

package mqttclient

import (
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/ryckakas/solix-mqtt-bridge/internal/testbroker"
)

func TestUnreachableBrokerWarnsOnceThenRecovers(t *testing.T) {
	b := testbroker.Start(t, testbroker.Options{})
	proxy := testbroker.NewProxy(t, testbroker.HostPort(b.URL))
	proxy.Cut()
	rec := &recorder{}
	c, err := Connect(Config{URL: proxy.URL(), ClientID: "bridge", PublishTimeout: 5 * time.Second},
		Will{Topic: "w"}, nil, slog.New(rec))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(c.Disconnect)

	eventually(t, "a connect warning", func() bool { return len(rec.records(failedMsg)) > 0 })
	proxy.Restore(t)
	eventually(t, "mqtt connected", func() bool { return len(rec.records("mqtt connected")) > 0 })
	if err := c.Publish(t.Context(), "solix/test", []byte("ok"), false); err != nil {
		t.Errorf("publish after recovery: %v", err)
	}
	if got := rec.levels(failedMsg); got[0] != slog.LevelWarn || slices.Contains(got[1:], slog.LevelWarn) {
		t.Errorf("failure levels = %v, want exactly one WARN, first", got)
	}
}

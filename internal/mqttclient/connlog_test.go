package mqttclient

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

const failedMsg = "mqtt connect failed; retrying"

var testCfg = Config{URL: "tcp://broker:1883", ClientID: "bridge"}

type recorder struct {
	mu   sync.Mutex
	recs []slog.Record
}

func (r *recorder) Enabled(context.Context, slog.Level) bool { return true }

func (r *recorder) Handle(_ context.Context, rec slog.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recs = append(r.recs, rec.Clone())
	return nil
}

func (r *recorder) WithAttrs([]slog.Attr) slog.Handler { return r }

func (r *recorder) WithGroup(string) slog.Handler { return r }

func (r *recorder) records(msg string) []slog.Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []slog.Record
	for _, rec := range r.recs {
		if msg == "" || rec.Message == msg {
			out = append(out, rec)
		}
	}
	return out
}

func (r *recorder) levels(msg string) []slog.Level {
	recs := r.records(msg)
	out := make([]slog.Level, 0, len(recs))
	for _, rec := range recs {
		out = append(out, rec.Level)
	}
	return out
}

func attr(rec slog.Record, key string) string {
	var v string
	rec.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			v = a.Value.String()
			return false
		}
		return true
	})
	return v
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestOnlyTheFirstFailureOfAStreakWarns(t *testing.T) {
	rec := &recorder{}
	cl := newConnLog(testCfg, slog.New(rec))
	fail := func() { cl.notify(nil, mqtt.ConnectionNotificationFailed{Reason: errors.New("connection refused")}) }
	fail()
	fail()
	fail()
	cl.connected()
	fail()
	fail()

	want := []slog.Level{slog.LevelWarn, slog.LevelDebug, slog.LevelDebug, slog.LevelWarn, slog.LevelDebug}
	if got := rec.levels(failedMsg); !slices.Equal(got, want) {
		t.Errorf("failure levels = %v, want %v", got, want)
	}
	if got := rec.levels("mqtt connected"); !slices.Equal(got, []slog.Level{slog.LevelInfo}) {
		t.Errorf("connected levels = %v, want one INFO", got)
	}
	failures := rec.records(failedMsg)
	if len(failures) == 0 {
		t.Fatal("no connect failure logged")
	}
	first := failures[0]
	for key, want := range map[string]string{"broker": testCfg.URL, "client_id": testCfg.ClientID, "err": "connection refused"} {
		if got := attr(first, key); got != want {
			t.Errorf("warning %s = %q, want %q", key, got, want)
		}
	}
}

// Only the attempt's overall outcome counts; per-broker and progress notifications would repeat it.
func TestOtherConnectionNotificationsLogNothing(t *testing.T) {
	rec := &recorder{}
	cl := newConnLog(testCfg, slog.New(rec))
	for _, n := range []mqtt.ConnectionNotification{
		mqtt.ConnectionNotificationConnecting{},
		mqtt.ConnectionNotificationBroker{},
		mqtt.ConnectionNotificationBrokerFailed{Reason: errors.New("connection refused")},
		mqtt.ConnectionNotificationConnected{},
		mqtt.ConnectionNotificationLost{},
	} {
		cl.notify(nil, n)
	}
	if got := rec.records(""); len(got) != 0 {
		t.Errorf("logged %d records, want none", len(got))
	}
}

func TestARefusedConnectionIsLogged(t *testing.T) {
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	c, err := Connect(Config{URL: "tcp://" + addr, ClientID: "bridge", PublishTimeout: time.Second},
		Will{Topic: "w"}, nil, slog.New(rec))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(c.Disconnect)

	eventually(t, "a connect warning", func() bool { return len(rec.records(failedMsg)) > 0 })
	first := rec.records(failedMsg)[0]
	if first.Level != slog.LevelWarn || !strings.Contains(attr(first, "err"), "refused") {
		t.Errorf("first failure = %v %q, want a WARN naming the refused connection", first.Level, attr(first, "err"))
	}
}

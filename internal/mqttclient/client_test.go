package mqttclient

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ryckakas/solix-mqtt-bridge/internal/testcert"
)

func TestOptionsAcceptsSupportedSchemes(t *testing.T) {
	for _, u := range []string{"tcp://broker:1883", "mqtt://broker:1883", "ssl://broker:8883", "mqtts://broker:8883"} {
		if _, err := options(Config{URL: u, ClientID: "c"}, Will{Topic: "w"}); err != nil {
			t.Errorf("options(%s): %v", u, err)
		}
	}
}

func TestOptionsRejectsUnsupportedSchemes(t *testing.T) {
	for _, u := range []string{"ws://broker:9001", "http://broker", "broker:1883", "://"} {
		if _, err := options(Config{URL: u, ClientID: "c"}, Will{Topic: "w"}); err == nil {
			t.Errorf("options(%s): want error, got nil", u)
		}
	}
}

func TestOptionsRejectsTLSSettingsOnPlainTCP(t *testing.T) {
	for _, cfg := range []Config{
		{URL: "tcp://broker:1883", TLSInsecure: true},
		{URL: "mqtt://broker:1883", CAFile: "/ca.pem"},
	} {
		if _, err := options(cfg, Will{Topic: "w"}); err == nil || !strings.Contains(err.Error(), "TLS") {
			t.Errorf("options(%+v): err = %v, want a TLS error", cfg, err)
		}
	}
}

func TestTLSConfigLoadsCAFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, testcert.New(t).CA, 0o600); err != nil {
		t.Fatal(err)
	}
	tc, err := tlsConfig(Config{CAFile: path})
	if err != nil {
		t.Fatalf("tlsConfig: %v", err)
	}
	if tc.RootCAs == nil || tc.InsecureSkipVerify {
		t.Errorf("RootCAs set = %v, InsecureSkipVerify = %v; want true, false", tc.RootCAs != nil, tc.InsecureSkipVerify)
	}
}

func TestTLSConfigRejectsBadCAFiles(t *testing.T) {
	dir := t.TempDir()
	garbage := filepath.Join(dir, "garbage.pem")
	if err := os.WriteFile(garbage, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(dir, "missing.pem"), garbage} {
		if _, err := tlsConfig(Config{CAFile: path}); err == nil {
			t.Errorf("tlsConfig(%s): want error, got nil", filepath.Base(path))
		}
	}
}

func TestTLSConfigInsecureIsOptIn(t *testing.T) {
	tc, err := tlsConfig(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if tc.InsecureSkipVerify {
		t.Error("InsecureSkipVerify = true without TLSInsecure")
	}
	if tc, _ := tlsConfig(Config{TLSInsecure: true}); !tc.InsecureSkipVerify {
		t.Error("InsecureSkipVerify = false with TLSInsecure")
	}
}

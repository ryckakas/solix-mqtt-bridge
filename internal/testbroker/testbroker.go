//go:build integration

// Package testbroker runs throwaway MQTT infrastructure for integration tests: a mosquitto container (optionally
// with an ACL and a TLS listener), a subscriber, and a TCP proxy whose connections can be cut to simulate a network
// failure. It is a test helper only and must never be linked into the bridge.
package testbroker

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mosquitto"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/ryckakas/solix-mqtt-bridge/internal/testcert"
)

// Image is the broker image the tests run: the 2.0 line openWB ships, with TLS support.
const Image = "eclipse-mosquitto:2.0.22-openssl"

// OpenWBACL mirrors the permissions openWB 2.x grants anonymous clients on its public listener.
const OpenWBACL = `topic write openWB/set/#
topic read openWB/#
topic readwrite others/#
`

// Options configures a broker.
type Options struct {
	// ACL is the content of a mosquitto acl_file; empty allows everything.
	ACL string
	// TLS adds a listener on 8883 with a certificate signed by a throwaway CA.
	TLS bool
}

// Broker is a running mosquitto container.
type Broker struct {
	// URL is the plain listener as mqtt://host:port.
	URL string
	// TLSURL is the TLS listener as mqtts://host:port; empty unless Options.TLS.
	TLSURL string
	// CAFile is a PEM file with the CA that signed the TLS listener's certificate; empty unless Options.TLS.
	CAFile string
}

// Start runs a broker for the duration of the test.
func Start(t *testing.T, opts Options) Broker {
	t.Helper()
	conf := []string{"listener 1883", "allow_anonymous true"}
	files := []testcontainers.ContainerFile{}
	if opts.ACL != "" {
		conf = append(conf, "acl_file /mosquitto/config/acl")
		files = append(files, file("/mosquitto/config/acl", []byte(opts.ACL)))
	}
	var ca []byte
	if opts.TLS {
		b := testcert.New(t)
		ca = b.CA
		conf = append(conf, "listener 8883", "certfile /mosquitto/config/server.pem", "keyfile /mosquitto/config/server.key")
		files = append(files, file("/mosquitto/config/server.pem", b.Cert), file("/mosquitto/config/server.key", b.Key))
	}
	files = append(files, file("/mosquitto/config/mosquitto.conf", []byte(strings.Join(conf, "\n")+"\n")))

	customizers := []testcontainers.ContainerCustomizer{testcontainers.WithFiles(files...)}
	if opts.TLS {
		customizers = append(customizers,
			testcontainers.WithExposedPorts("8883/tcp"),
			testcontainers.WithAdditionalWaitStrategy(wait.ForListeningPort("8883/tcp")))
	}
	ctr, err := mosquitto.Run(t.Context(), Image, customizers...)
	testcontainers.CleanupContainer(t, ctr)
	if err != nil {
		t.Fatalf("start mosquitto: %v", err)
	}

	b := Broker{}
	if b.URL, err = ctr.BrokerURL(t.Context()); err != nil {
		t.Fatalf("broker url: %v", err)
	}
	if opts.TLS {
		if b.TLSURL, err = ctr.PortEndpoint(t.Context(), "8883/tcp", "mqtts"); err != nil {
			t.Fatalf("tls broker url: %v", err)
		}
		b.CAFile = filepath.Join(t.TempDir(), "ca.pem")
		if err := os.WriteFile(b.CAFile, ca, 0o600); err != nil {
			t.Fatalf("write CA file: %v", err)
		}
	}
	return b
}

// HostPort strips the scheme from a broker URL.
func HostPort(brokerURL string) string {
	_, hostPort, found := strings.Cut(brokerURL, "://")
	if !found {
		panic(fmt.Sprintf("broker url without scheme: %q", brokerURL))
	}
	return hostPort
}

func file(path string, content []byte) testcontainers.ContainerFile {
	return testcontainers.ContainerFile{Reader: bytes.NewReader(content), ContainerFilePath: path, FileMode: 0o644}
}

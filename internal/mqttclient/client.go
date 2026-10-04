// Package mqttclient builds the MQTT connections the outputs use: broker URL, credentials, TLS and a Last Will, with
// publishing that is skipped rather than queued while the connection is down.
package mqttclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// ErrNotConnected is returned by Publish while the connection is down; the message is dropped, not queued.
var ErrNotConnected = errors.New("mqtt: not connected")

// Config describes one broker connection.
type Config struct {
	// URL is the broker, as tcp://host:1883, mqtt://…, ssl://host:8883 or mqtts://….
	URL string
	// ClientID must be unique per broker; each output uses its own.
	ClientID string
	// Username and Password are optional broker credentials.
	Username string
	// Password is sent only together with Username.
	Password string
	// CAFile is a PEM bundle to verify the broker's certificate against, for ssl:// or mqtts:// only.
	CAFile string
	// TLSInsecure skips verification of the broker's certificate.
	TLSInsecure bool
	// PublishTimeout bounds how long Publish waits for the broker's acknowledgement.
	PublishTimeout time.Duration
}

// Will is the message the broker publishes for a client whose connection drops without a clean disconnect.
type Will struct {
	// Topic is where the broker publishes the will.
	Topic string
	// Payload is the will's body.
	Payload []byte
	// Retained marks the will as retained.
	Retained bool
}

// Client is an MQTT connection that reconnects in the background.
type Client struct {
	c       mqtt.Client
	timeout time.Duration
}

// Connect starts connecting to the broker and keeps retrying in the background; it doesn't wait for the first
// connection. onConnect runs on its own goroutine after every successful (re)connect.
func Connect(cfg Config, will Will, onConnect func(), logger *slog.Logger) (*Client, error) {
	opts, err := options(cfg, will)
	if err != nil {
		return nil, err
	}
	opts.SetOnConnectHandler(func(mqtt.Client) {
		logger.Info("mqtt connected", "broker", cfg.URL, "client_id", cfg.ClientID)
		if onConnect != nil {
			onConnect()
		}
	})
	opts.SetConnectionLostHandler(func(_ mqtt.Client, err error) {
		logger.Warn("mqtt connection lost", "broker", cfg.URL, "client_id", cfg.ClientID, "err", err)
	})
	c := mqtt.NewClient(opts)
	c.Connect()
	return &Client{c: c, timeout: cfg.PublishTimeout}, nil
}

// Publish sends one message at QoS 1 and waits for the broker's acknowledgement. While the connection is down it
// returns ErrNotConnected immediately instead of queuing.
func (c *Client) Publish(ctx context.Context, topic string, payload []byte, retained bool) error {
	// paho would store a message published while reconnecting and replay it later; stale data must not be replayed.
	if !c.c.IsConnectionOpen() {
		return ErrNotConnected
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	t := c.c.Publish(topic, 1, retained, payload)
	select {
	case <-t.Done():
		if err := t.Error(); err != nil {
			return fmt.Errorf("publish %s: %w", topic, err)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("publish %s: %w", topic, ctx.Err())
	}
}

// Connected reports whether the connection is currently up.
func (c *Client) Connected() bool {
	return c.c.IsConnectionOpen()
}

// Disconnect closes the connection cleanly, so the broker discards the Will instead of publishing it.
func (c *Client) Disconnect() {
	c.c.Disconnect(250)
}

func options(cfg Config, will Will) (*mqtt.ClientOptions, error) {
	u, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("mqtt url %q: %w", cfg.URL, err)
	}
	opts := mqtt.NewClientOptions().
		AddBroker(cfg.URL).
		SetClientID(cfg.ClientID).
		SetCleanSession(true).
		SetKeepAlive(30*time.Second).
		SetConnectTimeout(10*time.Second).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(5*time.Second).
		SetMaxReconnectInterval(30*time.Second).
		SetBinaryWill(will.Topic, will.Payload, 1, will.Retained)
	if cfg.Username != "" {
		opts.SetUsername(cfg.Username).SetPassword(cfg.Password)
	}
	switch u.Scheme {
	case "tcp", "mqtt":
		if cfg.CAFile != "" || cfg.TLSInsecure {
			return nil, fmt.Errorf("mqtt url %q: TLS options need an ssl:// or mqtts:// broker", cfg.URL)
		}
	case "ssl", "mqtts":
		tc, err := tlsConfig(cfg)
		if err != nil {
			return nil, err
		}
		opts.SetTLSConfig(tc)
	default:
		return nil, fmt.Errorf("mqtt url %q: scheme must be tcp, mqtt, ssl or mqtts", cfg.URL)
	}
	return opts, nil
}

func tlsConfig(cfg Config) (*tls.Config, error) {
	tc := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: cfg.TLSInsecure, //nolint:gosec // opt-in for brokers with self-signed certs, e.g. openWB's
	}
	if cfg.CAFile == "" {
		return tc, nil
	}
	pem, err := os.ReadFile(cfg.CAFile)
	if err != nil {
		return nil, fmt.Errorf("read mqtt CA file: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("mqtt CA file %s: no PEM certificates found", cfg.CAFile)
	}
	tc.RootCAs = pool
	return tc, nil
}

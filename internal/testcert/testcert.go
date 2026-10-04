// Package testcert generates throwaway TLS material for tests: a self-signed CA and a server certificate it signs.
// It is a test helper only and must never be linked into the bridge.
package testcert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"testing"
	"time"
)

// Bundle is PEM-encoded TLS material.
type Bundle struct {
	// CA is the self-signed CA certificate clients verify against.
	CA []byte
	// Cert is the server certificate, valid for localhost, 127.0.0.1 and ::1.
	Cert []byte
	// Key is the server certificate's private key.
	Key []byte
}

// New generates a fresh CA and a server certificate signed by it, failing the test on any error.
func New(tb testing.TB) Bundle {
	tb.Helper()
	caKey := newKey(tb)
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "solix-mqtt-bridge test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	caDER := sign(tb, caTmpl, caTmpl, caKey, caKey)

	srvKey := newKey(tb)
	srvTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		tb.Fatalf("parse CA certificate: %v", err)
	}
	srvDER := sign(tb, srvTmpl, caCert, srvKey, caKey)

	keyDER, err := x509.MarshalECPrivateKey(srvKey)
	if err != nil {
		tb.Fatalf("marshal server key: %v", err)
	}
	return Bundle{
		CA:   pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}),
		Cert: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srvDER}),
		Key:  pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
	}
}

func newKey(tb testing.TB) *ecdsa.PrivateKey {
	tb.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		tb.Fatalf("generate key: %v", err)
	}
	return k
}

func sign(tb testing.TB, tmpl, parent *x509.Certificate, key, parentKey *ecdsa.PrivateKey) []byte {
	tb.Helper()
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, parentKey)
	if err != nil {
		tb.Fatalf("create certificate %q: %v", tmpl.Subject.CommonName, err)
	}
	return der
}

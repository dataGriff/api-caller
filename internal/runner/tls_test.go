package runner

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// pki is a throwaway certificate authority with one server and one client
// certificate, written as PEM under dir/certs.
type pki struct {
	caPEM        []byte
	serverCert   tls.Certificate
	pool         *x509.CertPool
	caFile       string
	clientCert   string
	clientKey    string
	strangerCert string
	strangerKey  string
}

func newPKI(t *testing.T, dir string) *pki {
	t.Helper()
	newKey := func() *ecdsa.PrivateKey {
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		return k
	}
	sign := func(tmpl, parent *x509.Certificate, key, parentKey *ecdsa.PrivateKey) ([]byte, *x509.Certificate) {
		der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, parentKey)
		if err != nil {
			t.Fatal(err)
		}
		cert, _ := x509.ParseCertificate(der)
		return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), cert
	}
	keyPEM := func(k *ecdsa.PrivateKey) []byte {
		der, _ := x509.MarshalECPrivateKey(k)
		return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
	}
	now := time.Now()
	caTmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "apic test CA"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	caKey := newKey()
	caPEM, ca := sign(caTmpl, caTmpl, caKey, caKey)

	serverKey := newKey()
	serverPEM, _ := sign(&x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "localhost"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, DNSNames: []string{"localhost"}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}, ca, serverKey, caKey)
	serverCert, err := tls.X509KeyPair(serverPEM, keyPEM(serverKey))
	if err != nil {
		t.Fatal(err)
	}
	clientKey := newKey()
	clientPEM, _ := sign(&x509.Certificate{SerialNumber: big.NewInt(3), Subject: pkix.Name{CommonName: "alice"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}, ca, clientKey, caKey)

	// A self-signed stranger the server does not trust.
	strangerKey := newKey()
	strangerTmpl := &x509.Certificate{SerialNumber: big.NewInt(4), Subject: pkix.Name{CommonName: "mallory"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	strangerPEM, _ := sign(strangerTmpl, strangerTmpl, strangerKey, strangerKey)

	pool := x509.NewCertPool()
	pool.AddCert(ca)
	p := &pki{caPEM: caPEM, serverCert: serverCert, pool: pool}
	certs := filepath.Join(dir, "certs")
	if err := os.MkdirAll(certs, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name string, data []byte, mode os.FileMode) string {
		path := filepath.Join(certs, name)
		if err := os.WriteFile(path, data, mode); err != nil {
			t.Fatal(err)
		}
		return path
	}
	p.caFile = write("ca.pem", caPEM, 0o644)
	p.clientCert = write("client.pem", clientPEM, 0o644)
	p.clientKey = write("client-key.pem", keyPEM(clientKey), 0o600)
	p.strangerCert = write("stranger.pem", strangerPEM, 0o644)
	p.strangerKey = write("stranger-key.pem", keyPEM(strangerKey), 0o600)
	return p
}

// mtlsServer requires a client certificate signed by the test CA.
func mtlsServer(t *testing.T, p *pki) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"user":"` + r.TLS.PeerCertificates[0].Subject.CommonName + `"}`))
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{p.serverCert}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: p.pool, MinVersion: tls.VersionTLS12}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

func TestClientCertificatesAndPrivateCA(t *testing.T) {
	dir := writeProject(t, map[string]string{
		"api.http":             "### me\n# @name me\n# @assert status == 200\n# @assert body.$.user == alice\nGET {{baseUrl}}/me\n",
		"http-client.env.json": `{"dev": {}}`,
	})
	p := newPKI(t, dir)
	srv := mtlsServer(t, p)
	env := `{"dev": {"baseUrl": "` + srv.URL + `"}}`
	if err := os.WriteFile(filepath.Join(dir, "http-client.env.json"), []byte(env), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(opts Options) (*Result, error) {
		t.Helper()
		opts.Env, opts.NoSession = "dev", true
		r := newRunner(t, dir, opts)
		return r.Run(context.Background(), r.Project.Requests()[0])
	}

	// Nothing configured: the private CA is unknown, so the handshake fails
	// as a transport error.
	if _, err := run(Options{}); ExitCode(err) != ExitTransport {
		t.Fatalf("without a CA: want exit 3, got %v", err)
	}
	// --insecure skips verification, but the server still wants a client cert.
	if _, err := run(Options{Insecure: true}); ExitCode(err) != ExitTransport {
		t.Fatalf("insecure without a cert: want exit 3, got %v", err)
	}
	// Flags: CA trusted, certificate presented.
	res, err := run(Options{CACert: p.caFile, Cert: p.clientCert, Key: p.clientKey})
	if err != nil || !res.OK {
		t.Fatalf("with flags: %v %+v", err, res)
	}
	if res.Request.TLS == nil || res.Request.TLS.CertFile != p.clientCert || res.Request.TLS.CAFile != p.caFile || res.Request.TLS.Insecure {
		t.Errorf("tls info = %+v", res.Request.TLS)
	}
	// A certificate the server does not trust is a transport error too.
	if _, err := run(Options{CACert: p.caFile, Cert: p.strangerCert, Key: p.strangerKey}); ExitCode(err) != ExitTransport {
		t.Fatalf("stranger cert: want exit 3, got %v", err)
	}

	// apic.yaml, with project-relative paths.
	yaml := "tls:\n  caFile: certs/ca.pem\n  certFile: certs/client.pem\n  keyFile: certs/client-key.pem\n"
	if err := os.WriteFile(filepath.Join(dir, "apic.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err = run(Options{})
	if err != nil || !res.OK {
		t.Fatalf("with apic.yaml: %v %+v", err, res)
	}
	if res.Request.TLS.CertFile != "certs/client.pem" || res.Request.TLS.KeyFile != "certs/client-key.pem" {
		t.Errorf("tls info = %+v", res.Request.TLS)
	}
	r := newRunner(t, dir, Options{Env: "dev", NoSession: true})
	if d := r.Describe(r.Project.Requests()[0]); d.TLS == nil || d.TLS.String() != "client cert certs/client.pem (key certs/client-key.pem) · ca certs/ca.pem" {
		t.Errorf("describe tls = %+v", d.TLS)
	}

	// A per-host override wins over the defaults: the stranger for this host.
	hostYAML := yaml + "  hosts:\n    127.0.0.1: {certFile: certs/stranger.pem, keyFile: certs/stranger-key.pem}\n    '*.example.com': {verifyHost: false}\n"
	if err := os.WriteFile(filepath.Join(dir, "apic.yaml"), []byte(hostYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := run(Options{}); ExitCode(err) != ExitTransport {
		t.Fatalf("host override should present the stranger: %v", err)
	}
	r = newRunner(t, dir, Options{Env: "dev", NoSession: true})
	if _, ok := hostOverride(r.Project.Config.TLS.Hosts, "API.example.com:8443"); !ok {
		t.Error("wildcard host override should match a subdomain with a port")
	}
	if _, ok := hostOverride(r.Project.Config.TLS.Hosts, "example.com"); ok {
		t.Error("*.example.com must not match the apex")
	}

	// JetBrains: the env file's SSLConfiguration block.
	if err := os.Remove(filepath.Join(dir, "apic.yaml")); err != nil {
		t.Fatal(err)
	}
	private := `{"dev": {"SSLConfiguration": {"clientCertificate": {"path": "certs/client.pem", "keyPath": "certs/client-key.pem"}, "verifyHostCertificate": false}}}`
	if err := os.WriteFile(filepath.Join(dir, "http-client.private.env.json"), []byte(private), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err = run(Options{})
	if err != nil || !res.OK || !res.Request.TLS.Insecure || res.Request.TLS.CertFile != "certs/client.pem" {
		t.Fatalf("JetBrains block: %v %+v", err, res)
	}
	r = newRunner(t, dir, Options{Env: "dev", NoSession: true})
	if d := r.Describe(r.Project.Requests()[0]); d.Variables != nil && len(d.Variables) != 1 {
		t.Errorf("SSLConfiguration must not be a variable: %+v", d.Variables)
	}
	// A key with a passphrase is refused up front.
	private = `{"dev": {"SSLConfiguration": {"clientCertificate": "certs/client.pem", "hasCertificatePassphrase": true}}}`
	if err := os.WriteFile(filepath.Join(dir, "http-client.private.env.json"), []byte(private), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := run(Options{CACert: p.caFile}); ExitCode(err) != ExitUsage || !strings.Contains(err.Error(), "passphrase") {
		t.Fatalf("passphrase: want a usage error, got %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "http-client.private.env.json")); err != nil {
		t.Fatal(err)
	}

	// Configuration mistakes are usage errors, not network ones.
	if _, err := run(Options{CACert: filepath.Join(dir, "api.http")}); ExitCode(err) != ExitUsage || !strings.Contains(err.Error(), "no PEM certificates") {
		t.Errorf("bad CA file: %v", err)
	}
	if _, err := run(Options{CACert: p.caFile, Cert: filepath.Join(dir, "certs", "missing.pem")}); ExitCode(err) != ExitUsage {
		t.Errorf("missing cert: %v", err)
	}
	escape := "tls:\n  caFile: ../outside.pem\n"
	if err := os.WriteFile(filepath.Join(dir, "apic.yaml"), []byte(escape), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := run(Options{}); ExitCode(err) != ExitUsage || !strings.Contains(err.Error(), "outside the project root") {
		t.Errorf("escaping path: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "apic.yaml")); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(p.clientKey, 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := run(Options{CACert: p.caFile, Cert: p.clientCert, Key: p.clientKey})
		if ExitCode(err) != ExitUsage || !strings.Contains(err.Error(), "world-readable (mode 0644)") {
			t.Errorf("world-readable key: %v", err)
		}
	}
}

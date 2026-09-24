package runner

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"

	"github.com/dataGriff/api-caller/internal/project"
)

// tlsSettings is what applies to one host once apic.yaml's `tls:`, the
// environment's JetBrains SSLConfiguration block, a per-host override and
// the command-line flags are merged, in that order. Paths from the flags
// are absolute (the user named them); the others are relative to the
// project root and confined to it.
type tlsSettings struct {
	CAFile   string
	CertFile string
	KeyFile  string
	Verify   bool
	// Passphrase marks a key the env file says is encrypted, which apic
	// cannot use.
	Passphrase bool
	// caFromFlag and certFromFlag mark paths the user typed on the
	// command line, the only ones not confined to the project root.
	caFromFlag   bool
	certFromFlag bool
}

func (s tlsSettings) isDefault() bool {
	return s.CAFile == "" && s.CertFile == "" && s.Verify
}

// TLSInfo is the TLS setup of a request as shown in output and mapped by
// `apic curl`. Paths are as configured: relative to the project root, or
// absolute when they came from a flag.
type TLSInfo struct {
	CAFile   string `json:"ca_file,omitempty"`
	CertFile string `json:"cert_file,omitempty"`
	KeyFile  string `json:"key_file,omitempty"`
	Insecure bool   `json:"insecure,omitempty"` // server certificate not verified
}

// String is the one-line description `describe` and verbose runs print.
func (t *TLSInfo) String() string {
	if t == nil {
		return ""
	}
	var parts []string
	if t.CertFile != "" {
		s := "client cert " + t.CertFile
		if t.KeyFile != "" && t.KeyFile != t.CertFile {
			s += " (key " + t.KeyFile + ")"
		}
		parts = append(parts, s)
	}
	if t.CAFile != "" {
		parts = append(parts, "ca "+t.CAFile)
	}
	if t.Insecure {
		parts = append(parts, "server certificate not verified")
	}
	return strings.Join(parts, " · ")
}

// tlsFor merges the TLS settings that apply to host (empty for none in
// particular, as for a token endpoint).
func (r *Runner) tlsFor(host string) tlsSettings {
	cfg := r.Project.Config.TLS
	s := tlsSettings{CAFile: cfg.CAFile, CertFile: cfg.CertFile, KeyFile: cfg.KeyFile, Verify: true}
	if cfg.VerifyHost != nil {
		s.Verify = *cfg.VerifyHost
	}
	if r.Envs != nil {
		if ssl := r.Envs.SSL(r.Opts.Env); ssl != nil {
			if ssl.CertFile != "" {
				s.CertFile, s.KeyFile, s.Passphrase = ssl.CertFile, ssl.KeyFile, ssl.HasPassphrase
			}
			if ssl.VerifyHost != nil {
				s.Verify = *ssl.VerifyHost
			}
		}
	}
	if h, ok := hostOverride(cfg.Hosts, host); ok {
		if h.CAFile != "" {
			s.CAFile = h.CAFile
		}
		if h.CertFile != "" {
			s.CertFile, s.KeyFile, s.Passphrase = h.CertFile, h.KeyFile, false
		}
		if h.VerifyHost != nil {
			s.Verify = *h.VerifyHost
		}
	}
	if r.Opts.CACert != "" {
		s.CAFile, s.caFromFlag = r.Opts.CACert, true
	}
	if r.Opts.Cert != "" {
		s.CertFile, s.KeyFile, s.Passphrase, s.certFromFlag = r.Opts.Cert, r.Opts.Key, false, true
	}
	if s.CertFile != "" && s.KeyFile == "" {
		s.KeyFile = s.CertFile
	}
	if r.Opts.Insecure {
		s.Verify = false
	}
	return s
}

// hostOverride finds the `tls.hosts` entry for host: an exact name, a
// name with the port, or a `*.suffix` wildcard; the most specific wins.
func hostOverride(hosts map[string]project.TLSHost, host string) (project.TLSHost, bool) {
	if host == "" || len(hosts) == 0 {
		return project.TLSHost{}, false
	}
	host = strings.ToLower(host)
	bare := host
	if i := strings.LastIndexByte(host, ':'); i > 0 && !strings.Contains(host[i:], "]") {
		bare = host[:i]
	}
	for _, candidate := range []string{host, bare} {
		if h, ok := hosts[candidate]; ok {
			return h, true
		}
	}
	best, found := project.TLSHost{}, false
	longest := -1
	for name, h := range hosts {
		name = strings.ToLower(name)
		if !strings.HasPrefix(name, "*.") {
			continue
		}
		suffix := name[1:]
		if strings.HasSuffix(bare, suffix) && len(bare) > len(suffix) && len(suffix) > longest {
			best, found, longest = h, true, len(suffix)
		}
	}
	return best, found
}

// info reports the settings for output, or nil when they are the default.
func (s tlsSettings) info() *TLSInfo {
	if s.isDefault() {
		return nil
	}
	return &TLSInfo{CAFile: s.CAFile, CertFile: s.CertFile, KeyFile: s.KeyFile, Insecure: !s.Verify}
}

// key identifies a set of settings for the caches.
func (s tlsSettings) key() string {
	if s.isDefault() {
		return "default"
	}
	return fmt.Sprintf("%s|%s|%s|%v|%v|%v", s.CAFile, s.CertFile, s.KeyFile, s.Verify, s.caFromFlag, s.certFromFlag)
}

// tlsConfig builds (and caches) the tls.Config for a set of settings.
// Every problem is a usage error: it is the project's configuration, not
// the network, that is wrong.
func (r *Runner) tlsConfig(s tlsSettings) (*tls.Config, error) {
	if s.isDefault() {
		return nil, nil
	}
	if s.CertFile != "" && s.Passphrase {
		return nil, usagef("tls: the client key for %s needs a passphrase (hasCertificatePassphrase), which apic cannot supply; store a decrypted key instead", s.CertFile)
	}
	key := s.key()
	r.tlsMu.Lock()
	defer r.tlsMu.Unlock()
	if r.tlsCache == nil {
		r.tlsCache = map[string]*tls.Config{}
	}
	if c, ok := r.tlsCache[key]; ok {
		return c, nil
	}
	c := &tls.Config{MinVersion: tls.VersionTLS12}
	if !s.Verify {
		c.InsecureSkipVerify = true //nolint:gosec // explicit --insecure or verifyHost: false
	}
	if s.CAFile != "" {
		path, err := r.tlsPath(s.CAFile, "ca file", s.caFromFlag)
		if err != nil {
			return nil, err
		}
		pem, err := os.ReadFile(path) //nolint:gosec // confined to the project root, or named on the command line
		if err != nil {
			return nil, usagef("tls: ca file %s: %v", s.CAFile, err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, usagef("tls: ca file %s: no PEM certificates found", s.CAFile)
		}
		c.RootCAs = pool
	}
	if s.CertFile != "" {
		certPath, err := r.tlsPath(s.CertFile, "client certificate", s.certFromFlag)
		if err != nil {
			return nil, err
		}
		keyPath, err := r.tlsPath(s.KeyFile, "client key", s.certFromFlag)
		if err != nil {
			return nil, err
		}
		if err := refuseWorldReadable(keyPath, s.KeyFile); err != nil {
			return nil, err
		}
		cert, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			return nil, usagef("tls: client certificate %s: %v", s.CertFile, err)
		}
		c.Certificates = []tls.Certificate{cert}
	}
	r.tlsCache[key] = c
	return c, nil
}

// tlsPath resolves a certificate path: one from a flag is the user's own,
// taken as it is; one from apic.yaml or an env file is relative to the
// project root and confined to it, absolute or not.
func (r *Runner) tlsPath(p, what string, fromFlag bool) (string, error) {
	if fromFlag {
		return p, nil
	}
	real, err := project.Confine(r.Project.Root, r.Project.Root, p)
	if errors.Is(err, project.ErrOutsideRoot) {
		return "", usagef("tls: %s %s: resolves outside the project root", what, p)
	}
	if err != nil {
		return "", usagef("tls: %s %s: %v", what, p, err)
	}
	return real, nil
}

// refuseWorldReadable keeps a private key that anyone on the machine can
// read from being used silently, the way ssh does. File modes are not
// meaningful on Windows.
func refuseWorldReadable(path, shown string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return usagef("tls: client key %s: %v", shown, err)
	}
	if perm := info.Mode().Perm(); perm&0o004 != 0 {
		return usagef("tls: client key %s is world-readable (mode %04o); run `chmod 600 %s`", shown, perm, shown)
	}
	return nil
}

// transport builds the HTTP transport for a request to host, with the TLS
// settings that apply to it.
//
// One transport serves every request with the same TLS settings in an
// invocation, so a flow's requests reuse their connections (which the
// timings report as reused).
func (r *Runner) transport(host string) (*http.Transport, error) {
	s := r.tlsFor(host)
	cfg, err := r.tlsConfig(s)
	if err != nil {
		return nil, err
	}
	r.tlsMu.Lock()
	defer r.tlsMu.Unlock()
	if tr, ok := r.transports[s.key()]; ok {
		return tr, nil
	}
	tr := cloneDefaultTransport()
	tr.Proxy = r.proxyFunc()
	if cfg != nil {
		tr.TLSClientConfig = cfg
	}
	if r.transports == nil {
		r.transports = map[string]*http.Transport{}
	}
	r.transports[s.key()] = tr
	return tr, nil
}

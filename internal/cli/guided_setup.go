package cli

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/store"
	urfave "github.com/urfave/cli/v3"
	"golang.org/x/sys/unix"
)

const guidedCertificateValidity = 365 * 24 * time.Hour

type guidedSetupResult struct {
	ConfigPath      string
	CertificatePath string
	KeyPath         string
	Endpoint        string
	Fingerprint     string
	CertificateEnd  time.Time
	Warning         string
	JournalPath     string
	setupLock       *os.File
	created         []string
}

type guidedSetupOptions struct {
	listen, endpoint, transport string
	prefixes                    []string
	certPath, keyPath           string
	confirmLAN, acknowledgeHTTP bool
}

func prepareGuidedSetup(errWriter io.Writer, cmd *urfave.Command, configPath, invitePath string) (*guidedSetupResult, error) {
	// --guided is retained as a compatibility alias. A missing configuration
	// always uses the same setup ladder, whether or not the alias is present.
	if _, err := os.Lstat(configPath); err == nil {
		return nil, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	opts := guidedSetupOptions{
		listen:          strings.TrimSpace(cmd.String("listen")),
		endpoint:        strings.TrimSpace(cmd.String("endpoint")),
		transport:       strings.ToLower(strings.TrimSpace(cmd.String("transport"))),
		prefixes:        cleanPrefixes(cmd.StringSlice("admitted-prefix")),
		certPath:        strings.TrimSpace(cmd.String("tls-cert")),
		keyPath:         strings.TrimSpace(cmd.String("tls-key")),
		confirmLAN:      cmd.Bool("confirm-non-loopback"),
		acknowledgeHTTP: cmd.Bool("acknowledge-cleartext-credentials"),
	}
	if opts.listen == "" {
		opts.listen = "127.0.0.1:8443"
	}
	if opts.transport == "" {
		opts.transport = "tls"
	}
	if opts.endpoint == "" {
		opts.endpoint = defaultEndpointForTransport(opts.listen, opts.transport)
	}
	if len(opts.prefixes) == 0 {
		opts.prefixes = []string{"coordination:"}
	}
	// Defaults are resolved before validation for both the historical alias and
	// its canonical zero-flag form. Setup never prompts or reads stdin.
	endpointURL, endpointHost, err := validateGuidedChoices(opts)
	if err != nil {
		return nil, err
	}
	loopback, err := loopbackListener(opts.listen)
	if err != nil {
		return nil, err
	}
	if !loopback && !opts.confirmLAN {
		return nil, reason.Invalid("non-loopback --listen requires --confirm-non-loopback")
	}
	if opts.transport == "http" && !opts.acknowledgeHTTP {
		return nil, reason.Invalid("cleartext --transport http requires --acknowledge-cleartext-credentials")
	}

	result := &guidedSetupResult{ConfigPath: configPath, Endpoint: endpointURL.String()}
	var certPEM, keyPEM []byte
	if opts.transport == "tls" {
		if (opts.certPath == "") != (opts.keyPath == "") {
			return nil, reason.Invalid("--tls-cert and --tls-key must be supplied together")
		}
		if opts.certPath == "" {
			result.CertificatePath = filepath.Join(filepath.Dir(configPath), "server.crt")
			result.KeyPath = filepath.Join(filepath.Dir(configPath), "server.key")
			certPEM, keyPEM, result.CertificateEnd, err = generateGuidedCertificate(endpointHost)
		} else {
			result.CertificatePath, err = filepath.Abs(filepath.Clean(opts.certPath))
			if err == nil {
				result.KeyPath, err = filepath.Abs(filepath.Clean(opts.keyPath))
			}
			if err == nil {
				result.Fingerprint, result.CertificateEnd, result.Warning, err = inspectGuidedCertificate(result.CertificatePath, result.KeyPath, endpointHost)
			}
		}
		if err != nil {
			return nil, err
		}
		if len(certPEM) > 0 {
			leaf, parseErr := firstCertificate(certPEM)
			if parseErr != nil {
				return nil, parseErr
			}
			result.Fingerprint = certificateFingerprint(leaf)
		}
	}

	home := defaultServerHome()
	setupLock, err := acquireGuidedSetupLock(configPath)
	if err != nil {
		return nil, err
	}
	keepLock := false
	defer func() {
		if !keepLock {
			releaseGuidedSetupLock(setupLock)
		}
	}()
	if err := recoverGuidedSetup(configPath); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(configPath); err == nil {
		return nil, reason.Invalid("guided setup refuses to overwrite --server-config; choose a new path")
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if entries, readErr := os.ReadDir(home); readErr == nil && len(entries) > 0 {
		return nil, reason.Invalid(fmt.Sprintf("server configuration %s does not match non-empty server home %s; use the configuration that initialized this home or choose a fresh XDG_STATE_HOME", configPath, home))
	} else if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return nil, reason.Invalid("guided setup cannot inspect the server home; choose a readable owner-private XDG_STATE_HOME")
	}
	contents := guidedConfig(home, opts, result)
	if err := preflightGuidedTargets(configPath, invitePath, result, len(certPEM) > 0); err != nil {
		return nil, err
	}
	result.JournalPath = configPath + ".guided-incomplete"
	result.setupLock = setupLock
	manifest := guidedSetupManifest{Home: home, ConfigPath: configPath, CertificatePath: result.CertificatePath, KeyPath: result.KeyPath, InvitePath: invitePath, GeneratedTLS: len(certPEM) > 0, Targets: map[string]string{configPath: contentSHA256([]byte(contents))}}
	if len(certPEM) > 0 {
		manifest.Targets[result.CertificatePath] = contentSHA256(certPEM)
		manifest.Targets[result.KeyPath] = contentSHA256(keyPEM)
	}
	encodedManifest, err := json.Marshal(manifest)
	if err != nil {
		return nil, err
	}
	if err := writeGuidedExclusive(result.JournalPath, append(encodedManifest, '\n')); err != nil {
		return nil, reason.New(reason.ReasonStorageFailure, "guided setup could not write its recovery record; correct the filesystem error and rerun")
	}
	result.created = append(result.created, result.JournalPath)
	if len(certPEM) > 0 {
		if err := writeGuidedExclusive(result.CertificatePath, certPEM); err != nil {
			return nil, reason.New(reason.ReasonStorageFailure, "guided setup could not write its TLS certificate; correct the filesystem error and rerun")
		}
		result.created = append(result.created, result.CertificatePath)
		if err := writeGuidedExclusive(result.KeyPath, keyPEM); err != nil {
			rollbackGuided(result.created)
			return nil, reason.New(reason.ReasonStorageFailure, "guided setup could not finish writing TLS files; correct the filesystem error and rerun")
		}
		result.created = append(result.created, result.KeyPath)
	}
	if err := writeGuidedExclusive(configPath, []byte(contents)); err != nil {
		rollbackGuided(result.created)
		return nil, reason.New(reason.ReasonStorageFailure, "guided setup could not write the server configuration; correct the filesystem error and rerun")
	}
	result.created = append(result.created, configPath)
	keepLock = true
	return result, nil
}

func cleanPrefixes(values []string) []string {
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func defaultEndpointForTransport(listen, transport string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil || host == "" || host == "0.0.0.0" || host == "::" {
		return ""
	}
	scheme := "https"
	if strings.EqualFold(strings.TrimSpace(transport), "http") {
		scheme = "http"
	}
	return scheme + "://" + net.JoinHostPort(host, port)
}

func validateGuidedChoices(opts guidedSetupOptions) (*url.URL, string, error) {
	_, portText, err := net.SplitHostPort(opts.listen)
	if err != nil {
		return nil, "", reason.Invalid("--listen must be HOST:PORT, for example 0.0.0.0:8443")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return nil, "", reason.Invalid("--listen port must be a number from 1 through 65535; for example 0.0.0.0:8443")
	}
	if opts.transport != "tls" && opts.transport != "http" {
		return nil, "", reason.Invalid("--transport must be tls or http")
	}
	parsed, err := url.Parse(opts.endpoint)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" {
		return nil, "", reason.Invalid("--endpoint must be an origin URL such as https://worklease.example.com:8443")
	}
	expectedScheme := map[string]string{"tls": "https", "http": "http"}[opts.transport]
	if parsed.Scheme != expectedScheme {
		return nil, "", reason.Invalid("--endpoint scheme must be " + expectedScheme + " for --transport " + opts.transport)
	}
	if parsed.Hostname() == "" {
		return nil, "", reason.Invalid("--endpoint must include a hostname or IP address")
	}
	if ip := net.ParseIP(parsed.Hostname()); ip != nil && ip.IsUnspecified() {
		return nil, "", reason.Invalid("--endpoint must not use a wildcard address; provide a client-facing hostname or IP")
	}
	if endpointPort := parsed.Port(); endpointPort != "" {
		port, portErr := strconv.Atoi(endpointPort)
		if portErr != nil || port < 1 || port > 65535 {
			return nil, "", reason.Invalid("--endpoint port must be a number from 1 through 65535; for example https://worklease.example.com:8443")
		}
	}
	seen := map[string]bool{}
	for _, prefix := range opts.prefixes {
		if !utf8.ValidString(prefix) || strings.TrimSpace(prefix) != prefix || !strings.HasSuffix(prefix, ":") {
			return nil, "", reason.Invalid("--admitted-prefix must be delimiter-terminated UTF-8; for example --admitted-prefix coordination:")
		}
		if strings.HasPrefix(prefix, "path:") || strings.HasPrefix(prefix, "backlog-md:") || strings.HasPrefix(prefix, "markdown:") {
			return nil, "", reason.Invalid("--admitted-prefix cannot admit host-local path:, backlog-md:, or markdown: resources")
		}
		if seen[prefix] {
			return nil, "", reason.Invalid("--admitted-prefix values must be unique")
		}
		seen[prefix] = true
	}
	return parsed, parsed.Hostname(), nil
}

func loopbackListener(listen string) (bool, error) {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return false, reason.Invalid("--listen must be HOST:PORT, for example 0.0.0.0:8443")
	}
	if strings.EqualFold(host, "localhost") {
		return true, nil
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback(), nil
}

func generateGuidedCertificate(host string) ([]byte, []byte, time.Time, error) {
	return generateGuidedCertificateAt(host, time.Now().UTC(), guidedCertificateValidity)
}

func generateGuidedCertificateAt(host string, now time.Time, validity time.Duration) ([]byte, []byte, time.Time, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	now = now.UTC()
	notAfter := now.Add(validity)
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: host}, NotBefore: now.Add(-5 * time.Minute), NotAfter: notAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	if ip := net.ParseIP(host); ip != nil {
		template.IPAddresses = []net.IP{ip}
	} else {
		template.DNSNames = []string{host}
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), notAfter, nil
}

func inspectGuidedCertificate(certPath, keyPath, host string) (string, time.Time, string, error) {
	if err := store.ValidateHostedPrivateFile(certPath); err != nil {
		return "", time.Time{}, "", reason.Invalid("--tls-cert must be a readable owner-private regular file")
	}
	if err := store.ValidateHostedPrivateFile(keyPath); err != nil {
		return "", time.Time{}, "", reason.Invalid("--tls-key must be a readable owner-private regular file")
	}
	pair, err := os.ReadFile(certPath)
	if err != nil {
		return "", time.Time{}, "", reason.Invalid("--tls-cert cannot be read")
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return "", time.Time{}, "", reason.Invalid("--tls-key cannot be read")
	}
	if _, err := tls.X509KeyPair(pair, key); err != nil {
		return "", time.Time{}, "", reason.Invalid("--tls-cert and --tls-key are not a matching certificate pair")
	}
	certificates, err := certificatesFromPEM(pair)
	if err != nil {
		return "", time.Time{}, "", reason.Invalid("--tls-cert contains no valid certificate chain")
	}
	leaf := certificates[0]
	now := time.Now()
	for _, certificate := range certificates {
		if now.Before(certificate.NotBefore) || !now.Before(certificate.NotAfter) {
			return "", time.Time{}, "", reason.Invalid("--tls-cert chain is not currently valid; supply unexpired certificate material")
		}
	}
	warning := ""
	if err := leaf.VerifyHostname(host); err != nil {
		warning = fmt.Sprintf("warning: supplied TLS certificate SAN does not cover advertised endpoint host %s", host)
	}
	return certificateFingerprint(leaf), leaf.NotAfter, warning, nil
}

func certificatesFromPEM(contents []byte) ([]*x509.Certificate, error) {
	var certificates []*x509.Certificate
	for len(contents) > 0 {
		block, rest := pem.Decode(contents)
		if block == nil {
			break
		}
		contents = rest
		if block.Type != "CERTIFICATE" {
			continue
		}
		certificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, err
		}
		certificates = append(certificates, certificate)
	}
	if len(certificates) == 0 {
		return nil, errors.New("certificate PEM is invalid")
	}
	return certificates, nil
}

func firstCertificate(contents []byte) (*x509.Certificate, error) {
	certificates, err := certificatesFromPEM(contents)
	if err != nil {
		return nil, err
	}
	return certificates[0], nil
}

func certificateFingerprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(sum[:])
}

func guidedConfig(home string, opts guidedSetupOptions, result *guidedSetupResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "home: %q\nlisten: %q\nadvertisedEndpoint: %q\n", home, opts.listen, result.Endpoint)
	if opts.transport == "tls" {
		fmt.Fprintf(&b, "tlsCert: %q\ntlsKey: %q\n", result.CertificatePath, result.KeyPath)
	} else {
		b.WriteString("allowInsecureHTTP: true\n")
	}
	b.WriteString("admittedPrefixes:\n")
	for _, prefix := range opts.prefixes {
		fmt.Fprintf(&b, "  - %q\n", prefix)
	}
	b.WriteString("maxTTL: 1h\nmaxHold: 24h\nshutdownTimeout: 5s\nhealthRate: 60\nmetadataRate: 60\nenrollmentRate: 20\n")
	return b.String()
}

type guidedSetupManifest struct {
	Home            string            `json:"home"`
	ConfigPath      string            `json:"configPath"`
	CertificatePath string            `json:"certificatePath,omitempty"`
	KeyPath         string            `json:"keyPath,omitempty"`
	InvitePath      string            `json:"invitePath"`
	GeneratedTLS    bool              `json:"generatedTls"`
	Targets         map[string]string `json:"targets"`
}

func contentSHA256(contents []byte) string {
	sum := sha256.Sum256(contents)
	return hex.EncodeToString(sum[:])
}

func acquireGuidedSetupLock(configPath string) (*os.File, error) {
	parent := filepath.Dir(configPath)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return nil, err
	}
	file, err := os.Open(parent)
	if err != nil {
		return nil, reason.New(reason.ReasonStorageFailure, "server setup directory lock cannot be opened")
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, reason.Invalid("another server setup is in progress for --server-config " + shellQuote(configPath))
		}
		return nil, err
	}
	return file, nil
}

func releaseGuidedSetupLock(file *os.File) {
	if file == nil {
		return
	}
	_ = unix.Flock(int(file.Fd()), unix.LOCK_UN)
	_ = file.Close()
}

func recoverGuidedSetup(configPath string) error {
	journalPath := configPath + ".guided-incomplete"
	if _, err := os.Lstat(journalPath); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err := store.ValidateHostedPrivateFile(journalPath); err != nil {
		return reason.Invalid("guided setup recovery record is unsafe; inspect " + journalPath)
	}
	data, err := os.ReadFile(journalPath)
	if err != nil {
		return reason.Invalid("guided setup recovery record cannot be read; inspect " + journalPath)
	}
	var manifest guidedSetupManifest
	if err := json.Unmarshal(data, &manifest); err != nil || !validGuidedManifest(configPath, manifest) {
		return reason.Invalid("guided setup recovery record is invalid or out of scope; inspect " + journalPath)
	}
	if _, err := os.Lstat(filepath.Join(manifest.Home, store.HostedMarkerFileName)); err == nil {
		return reason.Invalid("guided setup was interrupted after authority initialization began; rerun server init without --guided using --server-config " + shellQuote(configPath) + " --bootstrap-invite-file " + shellQuote(manifest.InvitePath))
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var verified []string
	for path, expected := range manifest.Targets {
		if _, statErr := os.Lstat(path); errors.Is(statErr, os.ErrNotExist) {
			continue
		} else if statErr != nil {
			return statErr
		}
		if err := store.ValidateHostedPrivateFile(path); err != nil {
			return reason.Invalid("guided setup found an unsafe partial file; inspect " + path + " and recovery record " + journalPath)
		}
		contents, readErr := os.ReadFile(path)
		if readErr != nil || contentSHA256(contents) != expected {
			return reason.Invalid("guided setup found a changed partial file; inspect " + path + " and recovery record " + journalPath)
		}
		verified = append(verified, path)
	}
	for _, path := range verified {
		if err := os.Remove(path); err != nil {
			return reason.New(reason.ReasonStorageFailure, "guided setup could not remove its verified partial file "+path+"; correct the filesystem error and rerun")
		}
	}
	if err := os.Remove(journalPath); err != nil {
		return reason.New(reason.ReasonStorageFailure, "guided setup could not clear its recovery record "+journalPath+"; correct the filesystem error and rerun")
	}
	return nil
}

func validGuidedManifest(configPath string, manifest guidedSetupManifest) bool {
	if filepath.Clean(manifest.ConfigPath) != filepath.Clean(configPath) || filepath.Clean(manifest.Home) != filepath.Clean(defaultServerHome()) || strings.TrimSpace(manifest.InvitePath) == "" {
		return false
	}
	expected := map[string]bool{filepath.Clean(configPath): true}
	if manifest.GeneratedTLS {
		certPath := filepath.Join(filepath.Dir(configPath), "server.crt")
		keyPath := filepath.Join(filepath.Dir(configPath), "server.key")
		if filepath.Clean(manifest.CertificatePath) != certPath || filepath.Clean(manifest.KeyPath) != keyPath {
			return false
		}
		expected[certPath], expected[keyPath] = true, true
	}
	if len(manifest.Targets) != len(expected) {
		return false
	}
	for path, digest := range manifest.Targets {
		if !expected[filepath.Clean(path)] || len(digest) != sha256.Size*2 {
			return false
		}
	}
	return true
}

func completeGuidedSetup(result *guidedSetupResult) error {
	if result == nil || result.JournalPath == "" {
		return nil
	}
	if err := removeDurable(result.JournalPath); err != nil {
		return reason.New(reason.ReasonStorageFailure, "guided setup initialized the authority but could not clear its recovery record; rerun server init without --guided using --server-config "+shellQuote(result.ConfigPath))
	}
	return nil
}

func completeRecoveredGuidedSetup(configPath, home string) error {
	journalPath := configPath + ".guided-incomplete"
	data, err := os.ReadFile(journalPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return reason.Invalid("guided setup recovery record cannot be read; inspect " + journalPath)
	}
	var manifest guidedSetupManifest
	if err := json.Unmarshal(data, &manifest); err != nil || !validGuidedManifest(configPath, manifest) || filepath.Clean(manifest.Home) != filepath.Clean(home) {
		return reason.Invalid("guided setup recovery record does not match this server configuration; inspect " + journalPath)
	}
	if err := removeDurable(journalPath); err != nil {
		return reason.New(reason.ReasonStorageFailure, "server initialization completed but could not clear guided recovery record "+journalPath)
	}
	return nil
}

func preflightGuidedTargets(configPath, invitePath string, result *guidedSetupResult, generated bool) error {
	// Guided setup always initializes a fresh authority, so a staged bootstrap
	// secret from another run must block it before any state is committed.
	paths := []string{configPath, invitePath, invitePath + ".legacy-secret", configPath + ".guided-incomplete"}
	collisionPaths := append([]string(nil), paths...)
	if result.CertificatePath != "" {
		collisionPaths = append(collisionPaths, result.CertificatePath, result.KeyPath)
	}
	if generated {
		paths = append(paths, result.CertificatePath, result.KeyPath)
	}
	seen := map[string]bool{}
	for _, path := range collisionPaths {
		path = filepath.Clean(path)
		if seen[path] {
			return reason.Invalid("guided setup output paths must be distinct; choose a different --bootstrap-invite-file, --server-config, --tls-cert, or --tls-key")
		}
		seen[path] = true
	}
	for _, path := range paths {
		if _, err := os.Lstat(path); err == nil {
			return reason.Invalid("guided setup refuses to overwrite existing file " + path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		parent := filepath.Dir(path)
		if err := os.MkdirAll(parent, 0o700); err != nil {
			return err
		}
		if err := store.ValidateHostedHome(parent); err != nil {
			return reason.Invalid("guided setup output directory must be owner-private: " + parent)
		}
	}
	return nil
}

func writeGuidedExclusive(path string, contents []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(contents); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	// Sync the parent so the new directory entry survives a power failure
	// alongside the already-synced contents.
	if err := syncDir(filepath.Dir(path)); err != nil {
		return err
	}
	ok = true
	return nil
}

func syncDir(dir string) error {
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

// removeDurable deletes path and commits the unlink to stable storage so a
// cleared recovery record cannot reappear after a crash.
func removeDurable(path string) error {
	if err := os.Remove(path); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	return syncDir(filepath.Dir(path))
}

func (result *guidedSetupResult) release() {
	if result == nil {
		return
	}
	releaseGuidedSetupLock(result.setupLock)
	result.setupLock = nil
}

func rollbackGuided(paths []string) {
	for index := len(paths) - 1; index >= 0; index-- {
		_ = os.Remove(paths[index])
	}
}

func shellQuote(value string) string {
	if value != "" && strings.IndexFunc(value, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_@%+=:,./-", r))
	}) == -1 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

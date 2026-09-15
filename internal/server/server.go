// Package server implements the frozen worklease-http/1 authority transport.
package server

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/store"
	"github.com/brettinternet/worklease/internal/watch"
	"gopkg.in/yaml.v3"
)

const ProtocolVersion = "worklease-http/1"
const maxRequestBody = 1 << 20
const maxResponseBody = 4 << 20

type Config struct {
	Home            string   `yaml:"home"`
	Listen          string   `yaml:"listen"`
	TLSCert         string   `yaml:"tlsCert"`
	TLSKey          string   `yaml:"tlsKey"`
	Prefixes        []string `yaml:"admittedPrefixes"`
	MaxTTL          string   `yaml:"maxTTL"`
	MaxTTLMicros    int64    `yaml:"maxTTLMicros"`
	MaxHold         string   `yaml:"maxHold"`
	MaxHoldMicros   int64    `yaml:"maxHoldMicros"`
	ShutdownTimeout string   `yaml:"shutdownTimeout"`
	HealthRate      int      `yaml:"healthRate"`
	MetadataRate    int      `yaml:"metadataRate"`
	EnrollmentRate  int      `yaml:"enrollmentRate"`
	RateLimits      struct {
		Health     int `yaml:"health"`
		Metadata   int `yaml:"metadata"`
		Enrollment int `yaml:"enrollment"`
	} `yaml:"rateLimits"`
}

func LoadConfig(path string) (Config, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" {
		return Config{}, reason.New(reason.ReasonConfigMissing, "server configuration file is required")
	}
	if err := store.ValidateHostedPrivateFile(path); err != nil {
		return Config{}, reason.New(reason.ReasonConfigInvalid, "server configuration is unsafe")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, reason.New(reason.ReasonConfigInvalid, "cannot read server configuration")
	}
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return Config{}, reason.New(reason.ReasonConfigInvalid, "server configuration is invalid")
	}
	if len(node.Content) == 0 {
		return Config{}, reason.New(reason.ReasonConfigInvalid, "server configuration must be an object")
	}
	root := node.Content[0]
	if root.Kind != yaml.MappingNode {
		return Config{}, reason.New(reason.ReasonConfigInvalid, "server configuration must be an object")
	}
	known := map[string]bool{"home": true, "listen": true, "listenAddress": true, "tlsCert": true, "tlsKey": true, "tlsCertFile": true, "tlsKeyFile": true, "admittedPrefixes": true, "prefixes": true, "maxTTL": true, "maxTTLMicros": true, "maxHold": true, "maxHoldMicros": true, "shutdownTimeout": true, "healthRate": true, "metadataRate": true, "enrollmentRate": true, "rateLimits": true}
	seen := map[string]bool{}
	for i := 0; i < len(root.Content); i += 2 {
		name := root.Content[i].Value
		if name == "" || !known[name] {
			return Config{}, reason.New(reason.ReasonConfigInvalid, "unknown server configuration field")
		}
		if seen[name] {
			return Config{}, reason.New(reason.ReasonConfigInvalid, "duplicate server configuration field")
		}
		seen[name] = true
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, reason.New(reason.ReasonConfigInvalid, "server configuration is invalid")
	}
	// Accept the deployment-facing aliases while keeping the parser strict.
	var raw map[string]any
	_ = yaml.Unmarshal(data, &raw)
	for _, aliases := range [][2]string{{"listen", "listenAddress"}, {"tlsCert", "tlsCertFile"}, {"tlsKey", "tlsKeyFile"}, {"admittedPrefixes", "prefixes"}} {
		if _, left := raw[aliases[0]]; left {
			if _, right := raw[aliases[1]]; right {
				return Config{}, reason.New(reason.ReasonConfigInvalid, "conflicting server configuration aliases")
			}
		}
	}
	if cfg.Listen == "" {
		if v, ok := raw["listenAddress"].(string); ok {
			cfg.Listen = v
		}
	}
	if cfg.TLSCert == "" {
		if v, ok := raw["tlsCertFile"].(string); ok {
			cfg.TLSCert = v
		}
	}
	if cfg.TLSKey == "" {
		if v, ok := raw["tlsKeyFile"].(string); ok {
			cfg.TLSKey = v
		}
	}
	if rawLimits, present := raw["rateLimits"]; present {
		limits, ok := rawLimits.(map[string]any)
		if !ok {
			return Config{}, reason.New(reason.ReasonConfigInvalid, "rateLimits must be an object")
		}
		for name := range limits {
			if name != "health" && name != "metadata" && name != "enrollment" {
				return Config{}, reason.New(reason.ReasonConfigInvalid, "unknown rateLimits field")
			}
		}
		for _, pair := range [][2]string{{"healthRate", "health"}, {"metadataRate", "metadata"}, {"enrollmentRate", "enrollment"}} {
			if _, direct := raw[pair[0]]; direct {
				if _, nested := limits[pair[1]]; nested {
					return Config{}, reason.New(reason.ReasonConfigInvalid, "conflicting rate limit fields")
				}
			}
		}
	}
	if cfg.HealthRate == 0 {
		cfg.HealthRate = cfg.RateLimits.Health
	}
	if cfg.MetadataRate == 0 {
		cfg.MetadataRate = cfg.RateLimits.Metadata
	}
	if cfg.EnrollmentRate == 0 {
		cfg.EnrollmentRate = cfg.RateLimits.Enrollment
	}
	if len(cfg.Prefixes) == 0 {
		if v, ok := raw["prefixes"].([]any); ok {
			for _, x := range v {
				if s, ok := x.(string); ok {
					cfg.Prefixes = append(cfg.Prefixes, s)
				}
			}
		}
	}
	return cfg, validateConfig(cfg, true)
}

func parseDuration(v string, micros int64, name string) (time.Duration, error) {
	if micros != 0 {
		if micros < 0 {
			return 0, reason.New(reason.ReasonConfigInvalid, name+" must be positive")
		}
		return time.Duration(micros) * time.Microsecond, nil
	}
	if strings.TrimSpace(v) == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(strings.TrimSpace(v))
	if err != nil || d <= 0 {
		return 0, reason.New(reason.ReasonConfigInvalid, name+" is invalid")
	}
	return d, nil
}
func validateConfig(c Config, allowInsecureHTTP bool) error {
	if strings.TrimSpace(c.Home) == "" {
		return reason.New(reason.ReasonConfigInvalid, "home is required")
	}
	if strings.TrimSpace(c.Listen) == "" {
		return reason.New(reason.ReasonConfigInvalid, "listen is required")
	}
	if len(c.Prefixes) == 0 {
		return reason.New(reason.ReasonConfigInvalid, "admittedPrefixes is required")
	}
	ttl, e := parseDuration(c.MaxTTL, c.MaxTTLMicros, "maxTTL")
	if e != nil || ttl == 0 {
		if e != nil {
			return e
		}
		return reason.New(reason.ReasonConfigInvalid, "maxTTL is required")
	}
	hold, e := parseDuration(c.MaxHold, c.MaxHoldMicros, "maxHold")
	if e != nil || hold == 0 {
		if e != nil {
			return e
		}
		return reason.New(reason.ReasonConfigInvalid, "maxHold is required")
	}
	if ttl < time.Second || ttl > time.Hour || hold < time.Second || hold > 24*time.Hour {
		return reason.New(reason.ReasonConfigInvalid, "TTL and hold bounds are invalid")
	}
	if c.HealthRate <= 0 || c.MetadataRate <= 0 || c.EnrollmentRate <= 0 {
		return reason.New(reason.ReasonConfigInvalid, "health, metadata, and enrollment rate limits must be positive")
	}
	if !allowInsecureHTTP && (c.TLSCert == "" || c.TLSKey == "") {
		return reason.New(reason.ReasonConfigInvalid, "TLS certificate and key are required")
	}
	if c.TLSCert != "" && c.TLSKey != "" {
		if err := store.ValidateHostedPrivateFile(c.TLSCert); err != nil {
			return reason.New(reason.ReasonConfigInvalid, "TLS certificate is unsafe")
		}
		if err := store.ValidateHostedPrivateFile(c.TLSKey); err != nil {
			return reason.New(reason.ReasonConfigInvalid, "TLS key is unsafe")
		}
	}
	return nil
}

type RateConfig struct{ Health, Metadata, Enrollment int }
type Server struct {
	cfg      Config
	service  *lease.Service
	store    *store.Store
	lock     *store.HostedLock
	http     *http.Server
	cancel   context.CancelFunc
	rates    *limiter
	logger   *log.Logger
	shutdown time.Duration
}

func New(ctx context.Context, cfg Config, allowInsecureHTTP bool, logger *log.Logger) (*Server, error) {
	if err := validateConfig(cfg, allowInsecureHTTP); err != nil {
		return nil, err
	}
	if err := store.ValidateHostedHome(cfg.Home); err != nil {
		return nil, err
	}
	lock, err := store.AcquireHostedLock(ctx, cfg.Home)
	if err != nil {
		return nil, err
	}
	st, err := store.Open(ctx, cfg.Home, store.Options{HostedWriter: true, RequireHostedReady: true, HostedLock: lock})
	if err != nil {
		_ = lock.Close()
		return nil, err
	}
	ttl, _ := parseDuration(cfg.MaxTTL, cfg.MaxTTLMicros, "maxTTL")
	hold, _ := parseDuration(cfg.MaxHold, cfg.MaxHoldMicros, "maxHold")
	svc, err := lease.NewRemote(st, nil, nil, lease.Defaults{TTL: ttl, PollInterval: watch.DefaultPoll}, lease.RemotePolicy{Prefixes: cfg.Prefixes, MaxTTL: ttl, MaxHold: hold})
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	shutdown := 5 * time.Second
	if d, e := parseDuration(cfg.ShutdownTimeout, 0, "shutdownTimeout"); e == nil && d > 0 {
		shutdown = d
	}
	if shutdown > 30*time.Second {
		shutdown = 30 * time.Second
	}
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	s := &Server{cfg: cfg, service: svc, store: st, lock: lock, rates: &limiter{limits: RateConfig{cfg.HealthRate, cfg.MetadataRate, cfg.EnrollmentRate}}, logger: logger, shutdown: shutdown}
	s.http = &http.Server{Addr: cfg.Listen, Handler: s.handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 35 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	return s, nil
}

// Handler exposes the protocol handler for embedding and transport tests.
func (s *Server) Handler() http.Handler { return s.handler() }

func (s *Server) Serve(ctx context.Context, allowInsecureHTTP bool) error {
	defer func() { _ = s.store.Close() }()
	if ctx == nil {
		ctx = context.Background()
	}
	requestCtx, cancelRequests := context.WithCancel(context.Background())
	s.cancel = cancelRequests
	s.http.BaseContext = func(net.Listener) context.Context { return requestCtx }
	defer cancelRequests()
	errc := make(chan error, 1)
	go func() {
		if allowInsecureHTTP {
			errc <- s.http.ListenAndServe()
		} else {
			errc <- s.http.ListenAndServeTLS(s.cfg.TLSCert, s.cfg.TLSKey)
		}
	}()
	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		// First stop admission and allow ordinary in-flight requests to drain.
		// At the bounded deadline, cancel long polls and force-close stragglers.
		shutdownCtx, stopShutdown := context.WithTimeout(context.Background(), s.shutdown)
		err := s.http.Shutdown(shutdownCtx)
		stopShutdown()
		if err != nil {
			cancelRequests()
			_ = s.http.Close()
		}
		select {
		case serveErr := <-errc:
			if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
				return serveErr
			}
		case <-time.After(s.shutdown):
			cancelRequests()
			_ = s.http.Close()
		}
		return nil
	}
}
func (s *Server) Close() error {
	if s.cancel != nil {
		s.cancel()
	}
	if s.http != nil {
		_ = s.http.Close()
	}
	if s.store != nil {
		return s.store.Close()
	}
	if s.lock != nil {
		return s.lock.Close()
	}
	return nil
}

type limiter struct {
	mu      sync.Mutex
	limits  RateConfig
	windows map[string]window
}
type window struct {
	at time.Time
	n  int
}

func (l *limiter) allow(bucket, source string) (bool, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.windows == nil {
		l.windows = map[string]window{}
	}
	limit := 0
	switch bucket {
	case "health":
		limit = l.limits.Health
	case "metadata":
		limit = l.limits.Metadata
	case "enroll":
		limit = l.limits.Enrollment
	}
	if limit <= 0 {
		return true, 0
	}
	key := bucket + "\x00" + source
	now := time.Now()
	w := l.windows[key]
	if now.Sub(w.at) >= time.Minute {
		w = window{at: now}
	}
	if w.n >= limit {
		return false, int((time.Minute - now.Sub(w.at) + time.Second - 1) / time.Second)
	}
	w.n++
	l.windows[key] = w
	return true, 0
}

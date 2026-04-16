package vault

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/cuigh/auxo/log"
	"github.com/cuigh/swirl/misc"
)

const (
	AuthToken   = "token"
	AuthAppRole = "approle"

	StorageTmpfs = "tmpfs"
	StorageVol   = "volume"
	StorageInit  = "init"

	defaultTimeout     = 10 * time.Second
	defaultKVMount     = "secret"
	defaultAppRolePath = "approle"
	// Renew tokens when they have less than this fraction of their TTL left.
	renewThreshold = 0.25
)

// ErrDisabled signals that Vault integration is not enabled in Settings.
var ErrDisabled = errors.New("vault integration is not enabled")

// Client is a thin HTTP client for the Vault HTTP API. It caches the
// authenticated token and reacquires it lazily when it has expired or when
// configuration changes.
//
// Concurrency: Client is safe for concurrent use by multiple goroutines; the
// authentication cache is protected by a mutex.
type Client struct {
	settingLoader func() *misc.Setting

	mu           sync.Mutex
	http         *http.Client
	cfgHash      string
	token        string
	tokenExpires time.Time
	logger       log.Logger
}

// NewClient returns a new Vault client bound to the given setting loader.
// The loader is called on every operation so that Settings changes are
// picked up without restarting Swirl.
func NewClient(loader func() *misc.Setting) *Client {
	return &Client{settingLoader: loader, logger: log.Get(PkgName)}
}

// IsEnabled reports whether Vault is configured and enabled.
func (c *Client) IsEnabled() bool {
	s := c.settingLoader()
	return s != nil && s.Vault.Enabled && s.Vault.Address != ""
}

// Health returns (sealed, initialized, version, error). It always performs a
// fresh unauthenticated request — useful for the "Test connection" button.
func (c *Client) Health(ctx context.Context) (sealed bool, initialized bool, version string, err error) {
	s := c.settingLoader()
	if s == nil || s.Vault.Address == "" {
		err = errors.New("vault address is not configured")
		return
	}
	cli, err := c.httpClient(s)
	if err != nil {
		return
	}
	// standbyok=true and sealedcode=200 make /sys/health return 200 even when
	// the node is standby or sealed; we want an HTTP success in all those
	// cases and will look at the body for the actual status.
	u, err := buildURL(s.Vault.Address, "v1/sys/health", "standbyok=true&sealedcode=200&uninitcode=200")
	if err != nil {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return
	}
	if s.Vault.Namespace != "" {
		req.Header.Set("X-Vault-Namespace", s.Vault.Namespace)
	}
	resp, err := cli.Do(req)
	if err != nil {
		// Log transport-level failures in full: DNS issues, connection
		// timeouts, TLS errors, proxy misbehaviour — we've been burned
		// by all of them at least once.
		c.logger.Warnf("vault health %s failed: %v", u, err)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 500 {
		err = fmt.Errorf("vault health %s %d: %s", resp.Proto, resp.StatusCode, truncate(string(body), 200))
		c.logger.Warnf("vault health: %v", err)
		return
	}
	var h struct {
		Sealed      bool   `json:"sealed"`
		Initialized bool   `json:"initialized"`
		Version     string `json:"version"`
	}
	_ = json.Unmarshal(body, &h)
	return h.Sealed, h.Initialized, h.Version, nil
}

// TestAuth runs a full auth round-trip against the current settings. It
// bypasses the cached token so it really exercises the credentials.
func (c *Client) TestAuth(ctx context.Context) error {
	s := c.settingLoader()
	if s == nil || !s.Vault.Enabled {
		return ErrDisabled
	}
	cli, err := c.httpClient(s)
	if err != nil {
		return err
	}
	tok, _, err := c.authenticate(ctx, cli, s)
	if err != nil {
		return err
	}
	// Validate the token by calling /sys/auth/token/lookup-self.
	u, err := buildURL(s.Vault.Address, "v1/auth/token/lookup-self", "")
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Vault-Token", tok)
	if s.Vault.Namespace != "" {
		req.Header.Set("X-Vault-Namespace", s.Vault.Namespace)
	}
	resp, err := cli.Do(req)
	if err != nil {
		c.logger.Warnf("vault token lookup transport error: %v", err)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		wrapped := fmt.Errorf("token lookup failed: %s %d: %s", resp.Proto, resp.StatusCode, truncate(string(body), 200))
		c.logger.Warnf("vault %v", wrapped)
		return wrapped
	}
	return nil
}

// ReadKVv2 reads a secret from a KVv2 mount. The returned map contains the
// latest version of the "data" payload. "path" is the logical path *inside*
// the mount (no "data/" prefix — this helper adds it).
func (c *Client) ReadKVv2(ctx context.Context, path string) (map[string]any, error) {
	s := c.settingLoader()
	if s == nil || !s.Vault.Enabled {
		return nil, ErrDisabled
	}
	mount := strings.Trim(s.Vault.KVMount, "/ ")
	if mount == "" {
		mount = defaultKVMount
	}
	clean := strings.TrimLeft(path, "/")
	apiPath := fmt.Sprintf("v1/%s/data/%s", mount, clean)

	body, err := c.doAuthed(ctx, http.MethodGet, apiPath, "", nil)
	if err != nil {
		return nil, err
	}
	var env struct {
		Data struct {
			Data     map[string]any `json:"data"`
			Metadata map[string]any `json:"metadata"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("decode kv response: %w", err)
	}
	if env.Data.Data == nil {
		return map[string]any{}, nil
	}
	return env.Data.Data, nil
}

// WriteKVv2 writes a new version of a KVv2 secret. "path" is the logical
// path inside the mount (no "data/" prefix). The caller's token needs
// `create` + `update` capabilities on `<mount>/data/<path>`.
func (c *Client) WriteKVv2(ctx context.Context, path string, data map[string]any) error {
	s := c.settingLoader()
	if s == nil || !s.Vault.Enabled {
		return ErrDisabled
	}
	mount := strings.Trim(s.Vault.KVMount, "/ ")
	if mount == "" {
		mount = defaultKVMount
	}
	clean := strings.TrimLeft(path, "/")
	apiPath := fmt.Sprintf("v1/%s/data/%s", mount, clean)
	payload := map[string]any{"data": data}
	_, err := c.doAuthed(ctx, http.MethodPost, apiPath, "", payload)
	return err
}

// DeleteKVv2 permanently removes a KVv2 secret AND all its version history
// (DELETE on the `metadata/` sub-path, not `data/`). The caller's token
// needs `delete` on `<mount>/metadata/<path>`.
func (c *Client) DeleteKVv2(ctx context.Context, path string) error {
	s := c.settingLoader()
	if s == nil || !s.Vault.Enabled {
		return ErrDisabled
	}
	mount := strings.Trim(s.Vault.KVMount, "/ ")
	if mount == "" {
		mount = defaultKVMount
	}
	clean := strings.TrimLeft(path, "/")
	apiPath := fmt.Sprintf("v1/%s/metadata/%s", mount, clean)
	_, err := c.doAuthed(ctx, http.MethodDelete, apiPath, "", nil)
	return err
}

// KVv2Metadata describes the version history of a KVv2 secret, returned
// by ReadMetadataKVv2. Enough for version-count badges + a stale check.
type KVv2Metadata struct {
	CurrentVersion int       `json:"current_version"`
	OldestVersion  int       `json:"oldest_version"`
	CreatedTime    time.Time `json:"created_time"`
	UpdatedTime    time.Time `json:"updated_time"`
	Versions       map[string]struct {
		CreatedTime time.Time `json:"created_time"`
		Destroyed   bool      `json:"destroyed"`
	} `json:"versions"`
}

// ReadMetadataKVv2 fetches version metadata for a KVv2 secret. Returns a
// nil metadata + no error if the secret does not exist (404) so the
// caller can distinguish "missing" from "transport error".
func (c *Client) ReadMetadataKVv2(ctx context.Context, path string) (*KVv2Metadata, error) {
	s := c.settingLoader()
	if s == nil || !s.Vault.Enabled {
		return nil, ErrDisabled
	}
	mount := strings.Trim(s.Vault.KVMount, "/ ")
	if mount == "" {
		mount = defaultKVMount
	}
	clean := strings.TrimLeft(path, "/")
	apiPath := fmt.Sprintf("v1/%s/metadata/%s", mount, clean)
	body, err := c.doAuthed(ctx, http.MethodGet, apiPath, "", nil)
	if err != nil {
		// doAuthed formats the HTTP status inside the error message. A
		// 404 is not a real error here — it just means the entry has
		// not been written yet. Match on " 404 " (with surrounding
		// spaces) to avoid false positives on IPs/ports.
		if strings.Contains(err.Error(), " 404 ") {
			return nil, nil
		}
		return nil, err
	}
	var env struct {
		Data KVv2Metadata `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("decode kv metadata response: %w", err)
	}
	return &env.Data, nil
}

// ReadMetadataSummary is a typed projection of ReadMetadataKVv2 that
// returns only the primitives needed by the biz layer — kept here to
// avoid the cross-package tangle of exporting KVv2Metadata through the
// biz `vaultReader` interface (biz can't import `vault`).
//
// `exists` is false when the entry does not exist (404); both other
// return values are meaningless in that case. `currentVersion` is the
// latest version number (1-based), `totalVersions` is the number of
// versions still on record (destroyed ones included).
func (c *Client) ReadMetadataSummary(ctx context.Context, path string) (currentVersion, totalVersions int, exists bool, err error) {
	meta, err := c.ReadMetadataKVv2(ctx, path)
	if err != nil {
		return 0, 0, false, err
	}
	if meta == nil {
		return 0, 0, false, nil
	}
	return meta.CurrentVersion, len(meta.Versions), true, nil
}

// ResolvePrefixed joins the configured prefix with a secret name and returns
// the full KVv2 logical path.
func ResolvePrefixed(s *misc.Setting, name string) string {
	prefix := strings.Trim(s.Vault.KVPrefix, "/ ")
	name = strings.Trim(name, "/ ")
	if prefix == "" {
		return name
	}
	return prefix + "/" + name
}

// ---- internals ----------------------------------------------------------

func (c *Client) doAuthed(ctx context.Context, method, apiPath, query string, payload any) ([]byte, error) {
	s := c.settingLoader()
	if s == nil || !s.Vault.Enabled {
		return nil, ErrDisabled
	}
	cli, err := c.httpClient(s)
	if err != nil {
		return nil, err
	}
	tok, err := c.getToken(ctx, cli, s)
	if err != nil {
		return nil, err
	}
	u, err := buildURL(s.Vault.Address, apiPath, query)
	if err != nil {
		return nil, err
	}
	var body io.Reader
	if payload != nil {
		buf, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Vault-Token", tok)
	if s.Vault.Namespace != "" {
		req.Header.Set("X-Vault-Namespace", s.Vault.Namespace)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == 403 {
		// Invalidate cached token so the next call re-auths with fresh creds.
		c.mu.Lock()
		c.token = ""
		c.tokenExpires = time.Time{}
		c.mu.Unlock()
	}
	if resp.StatusCode >= 400 {
		// Include resp.Proto + Server/Via so we can tell at a glance
		// what proxy is in front of Vault (Traefik, Caddy, nginx, ...)
		// and what HTTP version was actually used. Helps when chasing
		// "Misdirected Request" or other reverse-proxy edge cases.
		wrapped := fmt.Errorf("vault %s %s: %s %d (server=%q via=%q): %s",
			method, apiPath, resp.Proto, resp.StatusCode,
			resp.Header.Get("Server"), resp.Header.Get("Via"),
			truncate(string(data), 600))
		// 4xx/5xx are interesting for ops — log at Warn so they show
		// up without flipping levels. 404s are intentionally logged
		// too: they may be "entry not yet written" (benign) or
		// "policy mismatch" (symptomatic).
		c.logger.Warnf("%v", wrapped)
		return nil, wrapped
	}
	return data, nil
}

func (c *Client) getToken(ctx context.Context, cli *http.Client, s *misc.Setting) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	newHash := configHash(s)
	stillFresh := c.token != "" && c.cfgHash == newHash &&
		(c.tokenExpires.IsZero() || time.Until(c.tokenExpires) > 30*time.Second)
	if stillFresh {
		return c.token, nil
	}

	tok, ttl, err := c.authenticate(ctx, cli, s)
	if err != nil {
		return "", err
	}
	c.cfgHash = newHash
	c.token = tok
	if ttl > 0 {
		c.tokenExpires = time.Now().Add(ttl)
	} else {
		c.tokenExpires = time.Time{} // root / non-expiring
	}
	return tok, nil
}

func (c *Client) authenticate(ctx context.Context, cli *http.Client, s *misc.Setting) (token string, ttl time.Duration, err error) {
	method := strings.ToLower(strings.TrimSpace(s.Vault.AuthMethod))
	if method == "" {
		method = AuthToken
	}
	switch method {
	case AuthToken:
		// Trim whitespace defensively: pasted Vault tokens often arrive
		// with a trailing newline that the HTTP layer would happily send,
		// causing Vault to reject them as malformed.
		tok := strings.TrimSpace(s.Vault.Token)
		if tok == "" {
			return "", 0, errors.New("vault token is empty")
		}
		return tok, 0, nil
	case AuthAppRole:
		return loginAppRole(ctx, cli, s)
	default:
		return "", 0, fmt.Errorf("unsupported vault auth method: %q", method)
	}
}

func loginAppRole(ctx context.Context, cli *http.Client, s *misc.Setting) (string, time.Duration, error) {
	if s.Vault.RoleID == "" {
		return "", 0, errors.New("vault approle role_id is empty")
	}
	mount := strings.Trim(s.Vault.AppRolePath, "/ ")
	if mount == "" {
		mount = defaultAppRolePath
	}
	u, err := buildURL(s.Vault.Address, "v1/auth/"+mount+"/login", "")
	if err != nil {
		return "", 0, err
	}
	body, err := json.Marshal(map[string]string{
		"role_id":   s.Vault.RoleID,
		"secret_id": s.Vault.SecretID,
	})
	if err != nil {
		return "", 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.Vault.Namespace != "" {
		req.Header.Set("X-Vault-Namespace", s.Vault.Namespace)
	}
	lg := log.Get("vault")
	resp, err := cli.Do(req)
	if err != nil {
		lg.Warnf("vault approle login transport error: %v", err)
		return "", 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		wrapped := fmt.Errorf("approle login: %s %d: %s", resp.Proto, resp.StatusCode, truncate(string(raw), 300))
		lg.Warnf("vault %v", wrapped)
		return "", 0, wrapped
	}
	var env struct {
		Auth struct {
			ClientToken   string `json:"client_token"`
			LeaseDuration int    `json:"lease_duration"`
		} `json:"auth"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return "", 0, fmt.Errorf("decode approle response: %w", err)
	}
	if env.Auth.ClientToken == "" {
		return "", 0, errors.New("approle login returned empty client_token")
	}
	return env.Auth.ClientToken, time.Duration(env.Auth.LeaseDuration) * time.Second, nil
}

func (c *Client) httpClient(s *misc.Setting) (*http.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// We rebuild the http.Client on every call only if the TLS config changed.
	// In practice rebuilding is cheap; we keep it simple and just cache the
	// *http.Client itself, re-creating it when cfgHash moves. But to avoid
	// coupling with getToken (which also locks), we recompute on first use
	// only and store it.
	if c.http != nil && c.cfgHash == configHash(s) {
		return c.http, nil
	}
	tlsCfg := &tls.Config{InsecureSkipVerify: s.Vault.TLSSkipVerify}
	if strings.TrimSpace(s.Vault.CACert) != "" {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(s.Vault.CACert)) {
			return nil, errors.New("vault ca_cert is not a valid PEM")
		}
		tlsCfg.RootCAs = pool
	}
	tr := &http.Transport{
		TLSClientConfig:       tlsCfg,
		MaxIdleConns:          10,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: timeoutOrDefault(s) + 2*time.Second,
		// DisableKeepAlives: fresh TCP connection per request. Observed
		// in the wild: Vault sends `Connection: close` on every reply
		// (both HTTP and HTTPS backends), which under Go's default
		// keep-alive behaviour can leave an internal pool entry in a
		// half-closed state; the next request then hangs until the
		// client-level timeout. A new TCP connection per call adds no
		// noticeable latency for our tiny call volume and sidesteps
		// the whole class of issue.
		DisableKeepAlives: true,
	}
	c.http = &http.Client{Transport: tr, Timeout: timeoutOrDefault(s)}
	return c.http, nil
}

func timeoutOrDefault(s *misc.Setting) time.Duration {
	if s.Vault.RequestTimeout > 0 {
		return time.Duration(s.Vault.RequestTimeout) * time.Second
	}
	return defaultTimeout
}

func configHash(s *misc.Setting) string {
	// Cheap fingerprint of the connection-relevant fields. Not a security
	// hash — just used to detect Settings changes that require reauth or
	// rebuilding the http.Client.
	return strings.Join([]string{
		s.Vault.Address, s.Vault.Namespace, s.Vault.AuthMethod,
		s.Vault.Token, s.Vault.AppRolePath, s.Vault.RoleID, s.Vault.SecretID,
		s.Vault.CACert, fmt.Sprintf("%t/%d", s.Vault.TLSSkipVerify, s.Vault.RequestTimeout),
	}, "|")
}

func buildURL(addr, path, query string) (string, error) {
	addr = strings.TrimRight(addr, "/")
	u, err := url.Parse(addr + "/" + strings.TrimLeft(path, "/"))
	if err != nil {
		return "", err
	}
	if query != "" {
		u.RawQuery = query
	}
	return u.String(), nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

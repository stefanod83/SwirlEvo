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
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 500 {
		err = fmt.Errorf("vault health http %d: %s", resp.StatusCode, truncate(string(body), 200))
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
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("token lookup failed: http %d: %s", resp.StatusCode, truncate(string(body), 200))
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
		return nil, fmt.Errorf("vault %s %s: http %d: %s", method, apiPath, resp.StatusCode, truncate(string(data), 300))
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
	resp, err := cli.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", 0, fmt.Errorf("approle login: http %d: %s", resp.StatusCode, truncate(string(raw), 300))
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

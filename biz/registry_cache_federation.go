package biz

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/cuigh/auxo/net/web"
	"github.com/cuigh/swirl/dao"
)

// Registry Cache federation delegation (Phase 4).
//
// Standalone portals cannot reach the daemon.json of swarm-mode peers
// (the federation model forbids direct Docker socket access to the
// peer's managers). Instead, the portal MIRRORS its Setting.RegistryCache
// to the peer via POST /api/federation/registry-cache/receive. The peer
// then runs its own host addon bootstrap flow internally.
//
// Credentials (Password) are intentionally STRIPPED from the payload —
// the peer admin configures them separately via their own Settings UI.
// The CA cert is included so the peer can distribute it to its nodes.

// registryCacheSyncPayload is the JSON body sent to a peer's
// `/api/federation/registry-cache/receive` endpoint. Mirrors the fields
// of misc.Setting.RegistryCache that are SAFE to cross trust boundaries
// (CA cert + config flags). The receiver calls SettingBiz.Save which
// in turn preserves any existing password on the peer side via the
// standard secret-preservation contract (empty-string → keep).
type registryCacheSyncPayload struct {
	Enabled           bool   `json:"enabled"`
	Hostname          string `json:"hostname"`
	Port              int    `json:"port"`
	CACertPEM         string `json:"ca_cert_pem"`
	Username          string `json:"username,omitempty"`
	Password          string `json:"password"` // always empty — preserves peer-local value
	UseUpstreamPrefix bool   `json:"use_upstream_prefix"`
	RewriteMode       string `json:"rewrite_mode"`
	PreserveDigests   bool   `json:"preserve_digests"`
}

// federationHTTPClient is reused across outbound syncs. Single
// instance keeps TLS handshakes hot; 30s timeout bounds any single
// sync call.
var federationHTTPClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        8,
		MaxIdleConnsPerHost: 2,
		IdleConnTimeout:     90 * time.Second,
		TLSClientConfig:     &tls.Config{},
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	},
}

// SyncRegistryCacheToPeer pushes the portal's live Setting.RegistryCache
// to a swarm_via_swirl peer. Returns an error when the peer is
// unreachable, rejects the token, or fails to apply the payload.
// Password is intentionally blanked — the peer's SettingBiz.Save
// preserves whatever password the peer admin configured locally.
func SyncRegistryCacheToPeer(ctx context.Context, host *dao.Host) error {
	if host == nil {
		return errors.New("registry cache sync: nil host")
	}
	if host.Type != "swarm_via_swirl" {
		return fmt.Errorf("registry cache sync: host %q is not a swarm_via_swirl peer", host.Name)
	}
	base := strings.TrimRight(host.SwirlURL, "/")
	if base == "" {
		return fmt.Errorf("registry cache sync: host %q has no SwirlURL", host.Name)
	}
	if host.SwirlToken == "" {
		return fmt.Errorf("registry cache sync: host %q has no SwirlToken", host.Name)
	}
	live := LiveSettingsSnapshot()
	if live == nil || !live.RegistryCache.Enabled {
		return errors.New("registry cache sync: local Setting.RegistryCache is not enabled")
	}
	rc := live.RegistryCache
	payload := registryCacheSyncPayload{
		Enabled:           rc.Enabled,
		Hostname:          rc.Hostname,
		Port:              rc.Port,
		CACertPEM:         rc.CACertPEM,
		Username:          rc.Username,
		Password:          "", // never cross the trust boundary
		UseUpstreamPrefix: rc.UseUpstreamPrefix,
		RewriteMode:       rc.RewriteMode,
		PreserveDigests:   rc.PreserveDigests,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("registry cache sync: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		base+"/api/federation/registry-cache/receive",
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("registry cache sync: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+host.SwirlToken)
	req.Header.Set("X-Swirl-Federation-Version", "1")

	resp, err := federationHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("registry cache sync: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Read a best-effort diagnostic from the response body (size-
		// capped so a misbehaving peer cannot flood our logs).
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("registry cache sync: peer returned %d: %s", resp.StatusCode, strings.TrimSpace(string(buf)))
	}
	return nil
}

// ApplyReceivedRegistryCache is the peer-side counterpart of
// SyncRegistryCacheToPeer. Called by the federation receive handler
// after authenticating the portal's bearer token. Uses the regular
// SettingBiz.Save contract so the in-memory snapshot refresh +
// validation hooks all run.
//
// Accepts a decoded payload rather than raw bytes so the API handler
// can enforce struct-tag validation before we persist anything.
func ApplyReceivedRegistryCache(ctx context.Context, sb SettingBiz, payload map[string]interface{}, user web.User) error {
	if sb == nil {
		return errors.New("registry cache receive: SettingBiz not wired")
	}
	if payload == nil {
		return errors.New("registry cache receive: empty payload")
	}
	// Defensive: drop any `password` the portal might have leaked.
	// We NEVER trust a federation push to rotate our local credential —
	// the peer admin types it into their own Settings UI.
	delete(payload, "password")
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("registry cache receive: marshal: %w", err)
	}
	// Save takes json.RawMessage or []byte; json.RawMessage is cleaner.
	return sb.Save(ctx, "registry_cache", json.RawMessage(raw), user)
}

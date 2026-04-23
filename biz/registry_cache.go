package biz

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cuigh/swirl/dao"
)

// Registry Cache lives as a global Setting keyed by "registry_cache" and is
// consumed through three code paths:
//   1. Phase 1 (this file) — Setting save hook normalizes + validates the
//      payload. Helper GenerateCAPair / ComputeCAFingerprint support the
//      optional "generate self-signed CA" flow in the UI.
//   2. Phase 2 — Host addon bootstrap script + daemon.json snippet builders.
//   3. Phase 3 — compose image ref rewriter at deploy time.
//
// Only the mirror's connection info and upstream mapping live here; Swirl
// does NOT manage the mirror's container lifecycle. The operator deploys
// registry:2 / Harbor / Nexus themselves and points Swirl at it.

// validRewriteModes enumerates the RewriteMode values accepted by the
// deploy-time image rewriter in Phase 3. Anything else is rejected at
// Save time.
var validRewriteModes = map[string]bool{
	"off":      true,
	"per-host": true,
	"always":   true,
}

// normalizeRegistryCache fills in derived fields (CA fingerprint, default
// port, default RewriteMode, default UseUpstreamPrefix) and trims
// whitespace so follow-up validation operates on a clean payload. Input/
// output is the generic map[string]interface{} SettingBiz.Save decodes
// from the incoming JSON RawMessage.
func normalizeRegistryCache(v interface{}) interface{} {
	m, ok := v.(map[string]interface{})
	if !ok {
		return v
	}
	// String trims.
	for _, k := range []string{"hostname", "username", "rewrite_mode"} {
		if s, ok := m[k].(string); ok {
			m[k] = strings.TrimSpace(s)
		}
	}
	if ca, ok := m["ca_cert_pem"].(string); ok {
		m["ca_cert_pem"] = strings.TrimSpace(ca)
		// Derive fingerprint from the trimmed cert. If parsing fails
		// (bad PEM / operator still editing), blank the fingerprint so
		// the UI does not surface a stale value.
		if fp, err := ComputeCAFingerprint(m["ca_cert_pem"].(string)); err == nil {
			m["ca_fingerprint"] = fp
		} else {
			m["ca_fingerprint"] = ""
		}
	}
	// Default port 5000 — the registry:2 convention.
	if enabled, _ := m["enabled"].(bool); enabled {
		if _, present := m["port"]; !present {
			m["port"] = float64(5000)
		} else if p, ok := m["port"].(float64); ok && p == 0 {
			m["port"] = float64(5000)
		}
	}
	// Default rewrite mode = per-host.
	if rm, _ := m["rewrite_mode"].(string); rm == "" {
		m["rewrite_mode"] = "per-host"
	}
	// Default UseUpstreamPrefix = true (multi-upstream layout, matches
	// the typical Harbor/Nexus project convention). Legacy blobs that
	// lacked this field take the same default on first read.
	if _, present := m["use_upstream_prefix"]; !present {
		m["use_upstream_prefix"] = true
	}
	// Drop legacy per-upstream mapping table if still present in the
	// stored blob — the flag supersedes it. Removing the key avoids
	// the Go unmarshal silently carrying it onto an unknown field.
	delete(m, "upstreams")
	return m
}

// overlayRegistryCacheFromRegistry applies the "linked Registry"
// semantics. When `registry_id` is set in the incoming blob, load the
// referenced Registry and OVERLAY Hostname/Port/Username/Password/
// CACertPEM/CAFingerprint on the blob — any value the UI submitted
// for those fields is discarded. When `registry_id` is empty, return
// the blob unchanged.
//
// Runs after normalizeRegistryCache so the CA fingerprint derivation
// (for the inline case) has already finalised.
func overlayRegistryCacheFromRegistry(ctx context.Context, di dao.Interface, v interface{}) interface{} {
	m, ok := v.(map[string]interface{})
	if !ok {
		return v
	}
	id, _ := m["registry_id"].(string)
	id = strings.TrimSpace(id)
	if id == "" {
		return m
	}
	r, err := di.RegistryGet(ctx, id)
	if err != nil || r == nil {
		// Unknown RegistryID → downgrade silently to inline values;
		// validation will still refuse an empty hostname if enabled.
		// Also blank the stale fields so the UI cannot silently keep
		// referencing a deleted Registry.
		m["registry_id"] = ""
		return m
	}
	host, port := splitMirrorURL(r.URL)
	if host != "" {
		m["hostname"] = host
	}
	if port > 0 {
		m["port"] = float64(port)
	}
	m["username"] = r.Username
	if r.Password != "" {
		m["password"] = r.Password
	}
	// When the Registry is linked, its CA is authoritative — overwrite
	// whatever inline value the UI sent.
	m["ca_cert_pem"] = r.CACertPEM
	if r.CAFingerprint != "" {
		m["ca_fingerprint"] = r.CAFingerprint
	} else if fp, fpErr := ComputeCAFingerprint(r.CACertPEM); fpErr == nil {
		m["ca_fingerprint"] = fp
	} else {
		m["ca_fingerprint"] = ""
	}
	return m
}

// splitMirrorURL parses the Registry URL (e.g. https://mirror.lan:5000
// or https://mirror.lan) and returns its host + port. Empty + 0 when
// parsing fails. Ports are inferred from scheme when absent: 443 for
// https, 80 for http.
func splitMirrorURL(raw string) (string, int) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", 0
	}
	// url.Parse requires a scheme to populate Host — assume https when
	// missing, which matches the Registry.URL convention.
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", 0
	}
	host := u.Hostname()
	portStr := u.Port()
	if portStr == "" {
		if u.Scheme == "http" {
			return host, 80
		}
		return host, 443
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return host, 0
	}
	return host, port
}

// validateRegistryCache enforces the invariants the downstream consumers
// rely on: non-empty hostname + port when enabled. Disabled settings are
// allowed to contain partial/empty values so operators can save
// work-in-progress.
func validateRegistryCache(v interface{}) error {
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}
	enabled, _ := m["enabled"].(bool)
	if !enabled {
		return nil
	}
	host, _ := m["hostname"].(string)
	if host == "" {
		return errors.New("registry_cache: hostname is required when enabled")
	}
	portF, _ := m["port"].(float64)
	port := int(portF)
	if port < 1 || port > 65535 {
		return fmt.Errorf("registry_cache: port must be between 1 and 65535 (got %d)", port)
	}
	if rm, _ := m["rewrite_mode"].(string); !validRewriteModes[rm] {
		return fmt.Errorf("registry_cache: invalid rewrite_mode %q (expected off|per-host|always)", rm)
	}
	return nil
}

// ComputeCAFingerprint returns the hex-encoded SHA-256 of the certificate's
// DER bytes (the same fingerprint shown by `openssl x509 -noout -fingerprint
// -sha256`). Returns an error when the PEM is empty or cannot be parsed —
// callers that want a best-effort value (e.g. normalize on Save) should
// blank the fingerprint field on error instead of refusing the save.
func ComputeCAFingerprint(pemData string) (string, error) {
	if strings.TrimSpace(pemData) == "" {
		return "", errors.New("empty PEM")
	}
	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		return "", errors.New("invalid PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("parse certificate: %w", err)
	}
	sum := sha256.Sum256(cert.Raw)
	return strings.ToUpper(hex.EncodeToString(sum[:])), nil
}

// GenerateCAPair creates a self-signed CA certificate + ECDSA P-256 key
// suitable for signing the operator's mirror TLS cert. The public cert is
// persisted in Setting.RegistryCache.CACertPEM so Swirl can distribute it
// to hosts via the bootstrap script. The private key is returned ONCE to
// the UI (downloaded by the operator) and never stored in Swirl — the
// operator uses it to sign the mirror's server cert offline.
//
// 10-year validity matches typical internal-CA practice; the operator can
// regenerate + rotate anytime from the Settings page.
func GenerateCAPair(subjectCN string) (certPEM, keyPEM string, err error) {
	if subjectCN == "" {
		subjectCN = "Swirl Registry Cache CA"
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("generate key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", "", fmt.Errorf("generate serial: %w", err)
	}
	now := time.Now().UTC()
	tpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   subjectCN,
			Organization: []string{"Swirl"},
		},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return "", "", fmt.Errorf("sign certificate: %w", err)
	}
	certPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", "", fmt.Errorf("marshal key: %w", err)
	}
	keyPEM = string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
	return certPEM, keyPEM, nil
}

// RegistryCacheParams collapses the live Setting.RegistryCache subtree
// into the subset of fields the snippet + script builders actually need.
// Keeps the helpers independent from the misc.Setting struct layout so
// unit tests can pass ad-hoc values without building a full Setting.
type RegistryCacheParams struct {
	Enabled     bool
	Hostname    string
	Port        int
	CACertPEM   string
	// Derived; included so callers do not have to recompute.
	Fingerprint string
}

// LiveRegistryCacheParams returns a snapshot of the currently-enabled
// Registry Cache mirror configuration, or nil when the feature is not
// enabled (or no live *misc.Setting has been installed — happens in
// tests). Callers must treat the returned value as read-only.
//
// The returned struct carries CACertPEM verbatim (needed to generate
// the bootstrap script's heredoc) but is only consumed inside the biz
// + API layers; the API handler strips secrets before returning the
// DTO to the UI.
func LiveRegistryCacheParams() *RegistryCacheParams {
	liveSettingsMu.RLock()
	defer liveSettingsMu.RUnlock()
	if liveSettings == nil || !liveSettings.RegistryCache.Enabled {
		return nil
	}
	rc := liveSettings.RegistryCache
	return &RegistryCacheParams{
		Enabled:     rc.Enabled,
		Hostname:    rc.Hostname,
		Port:        rc.Port,
		CACertPEM:   rc.CACertPEM,
		Fingerprint: rc.CAFingerprint,
	}
}

// BuildMirrorURL returns the canonical https://<hostname>:<port> URL
// the daemon points at via `registry-mirrors`. Empty hostname returns
// an empty string — callers must guard.
func BuildMirrorURL(p *RegistryCacheParams) string {
	if p == nil || p.Hostname == "" {
		return ""
	}
	port := p.Port
	if port == 0 {
		port = 5000
	}
	return fmt.Sprintf("https://%s:%d", p.Hostname, port)
}

// BuildDaemonSnippet produces the fragment of /etc/docker/daemon.json
// the operator must merge into the remote host's existing config.
// Returns valid JSON (pretty-printed) so operators can also diff /
// merge visually. The output is intentionally a FRAGMENT — the
// bootstrap script merges it into whatever already lives on the host.
//
// insecure=true uses the `insecure-registries` route (no cert
// distribution). insecure=false adds only `registry-mirrors` and
// relies on the CA being placed in /etc/docker/certs.d/.
func BuildDaemonSnippet(p *RegistryCacheParams, insecure bool) string {
	url := BuildMirrorURL(p)
	if url == "" {
		return ""
	}
	if insecure {
		endpoint := fmt.Sprintf("%s:%d", p.Hostname, portOrDefault(p.Port))
		return fmt.Sprintf(`{
  "registry-mirrors": [%q],
  "insecure-registries": [%q]
}
`, url, endpoint)
	}
	return fmt.Sprintf(`{
  "registry-mirrors": [%q]
}
`, url)
}

// BuildBootstrapScript produces a shell script the operator copy-pastes
// onto the target host. Merges the snippet into /etc/docker/daemon.json,
// installs the CA (when not insecure), and reloads dockerd so the new
// mirror takes effect. Uses `jq` for the merge — the script guards
// against missing jq with a clear error.
//
// The script is intentionally conservative:
//   - Never overwrites an existing daemon.json blindly; always merges.
//   - Backs up the current daemon.json to .bak before writing.
//   - Uses `systemctl reload docker` with a SIGHUP fallback.
//   - Idempotent: re-running it repeats the merge without harm.
func BuildBootstrapScript(p *RegistryCacheParams, insecure bool) string {
	url := BuildMirrorURL(p)
	if url == "" {
		return ""
	}
	endpoint := fmt.Sprintf("%s:%d", p.Hostname, portOrDefault(p.Port))
	certBlock := ""
	if !insecure && strings.TrimSpace(p.CACertPEM) != "" {
		// Heredoc with explicit delimiter + trimmed body so the
		// certificate lands as-is on the remote filesystem.
		certBlock = fmt.Sprintf(`
# --- Trust the Swirl Registry Cache CA ------------------------------
sudo mkdir -p /etc/docker/certs.d/%s
sudo tee /etc/docker/certs.d/%s/ca.crt >/dev/null <<'SWIRL_CA_PEM_EOF'
%s
SWIRL_CA_PEM_EOF
sudo chmod 0644 /etc/docker/certs.d/%s/ca.crt
`,
			endpoint, endpoint, strings.TrimSpace(p.CACertPEM), endpoint)
	}
	snippet := strings.TrimSpace(BuildDaemonSnippet(p, insecure))
	return fmt.Sprintf(`#!/usr/bin/env bash
# Swirl Registry Cache — host bootstrap
# Generated for mirror: %s
# Mode: %s
#
# What this does:
#   1. Merges registry-mirrors (+ insecure-registries on demand) into
#      /etc/docker/daemon.json, preserving unrelated keys.
#   2. (TLS mode) Installs the Swirl-issued CA into
#      /etc/docker/certs.d/<host>:<port>/ca.crt so docker trusts the
#      mirror without flagging it as insecure.
#   3. Reloads dockerd so the change takes effect without a full
#      restart when possible (SIGHUP → registry-mirrors hot-reload).
set -euo pipefail

if ! command -v jq >/dev/null 2>&1; then
  echo "swirl: this script requires jq — install it and retry" >&2
  exit 2
fi

SNIPPET=$(cat <<'SWIRL_SNIPPET_EOF'
%s
SWIRL_SNIPPET_EOF
)

DAEMON=/etc/docker/daemon.json
if [ -f "$DAEMON" ]; then
  sudo cp -a "$DAEMON" "${DAEMON}.bak.$(date +%%Y%%m%%d%%H%%M%%S)"
  MERGED=$(sudo jq -s '.[0] * .[1]' "$DAEMON" <(printf '%%s' "$SNIPPET"))
else
  MERGED="$SNIPPET"
fi
printf '%%s\n' "$MERGED" | sudo tee "$DAEMON" >/dev/null
sudo chmod 0644 "$DAEMON"
%s
# Reload dockerd: SIGHUP is enough for registry-mirrors; fall back to
# reload/restart if the daemon does not honor it.
if sudo systemctl is-active --quiet docker; then
  sudo systemctl reload docker 2>/dev/null || sudo systemctl restart docker
elif command -v pidof >/dev/null 2>&1; then
  sudo kill -HUP "$(pidof dockerd || true)" 2>/dev/null || true
fi

echo "swirl: Registry Cache bootstrap applied — %s → %s"
`,
		url,
		modeLabel(insecure),
		snippet,
		certBlock,
		endpoint,
		url,
	)
}

func portOrDefault(p int) int {
	if p == 0 {
		return 5000
	}
	return p
}

// RegistryCachePingResult reports whether the configured mirror is
// reachable. `Status` is the HTTP status returned by /v2/: 200 means
// anonymous access OK, 401 means the mirror requires auth (still a
// healthy signal since TLS + routing worked). Anything else → treat as
// error. `LatencyMs` measures the full round-trip (DNS + TLS + HTTP).
type RegistryCachePingResult struct {
	OK        bool   `json:"ok"`
	Status    int    `json:"status,omitempty"`
	Error     string `json:"error,omitempty"`
	LatencyMs int64  `json:"latencyMs,omitempty"`
	MirrorURL string `json:"mirrorUrl,omitempty"`
}

// PingRegistryCache performs a best-effort probe of the configured
// pull-through mirror by issuing GET /v2/ over HTTPS. Honours the
// operator-uploaded CA (rc.CACertPEM) via a temporary root pool so
// self-signed mirrors validate without tripping the system trust
// store. A missing CA falls back to the system trust; this matches
// what Docker does when daemon.json carries a mirror URL without a
// dedicated cert in /etc/docker/certs.d.
func PingRegistryCache(ctx context.Context) *RegistryCachePingResult {
	live := LiveRegistryCacheParams()
	if live == nil {
		return &RegistryCachePingResult{Error: "registry_cache: feature is not enabled"}
	}
	url := BuildMirrorURL(&RegistryCacheParams{Hostname: live.Hostname, Port: live.Port})
	if url == "" {
		return &RegistryCachePingResult{Error: "registry_cache: mirror hostname is empty"}
	}
	target := url + "/v2/"

	tlsCfg := &tls.Config{}
	if strings.TrimSpace(live.CACertPEM) != "" {
		pool := x509.NewCertPool()
		if pool.AppendCertsFromPEM([]byte(live.CACertPEM)) {
			tlsCfg.RootCAs = pool
		}
	}
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: tlsCfg,
		},
	}

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return &RegistryCachePingResult{Error: fmt.Sprintf("build request: %v", err), MirrorURL: url}
	}
	resp, err := client.Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return &RegistryCachePingResult{Error: err.Error(), LatencyMs: latency, MirrorURL: url}
	}
	defer resp.Body.Close()
	// 200 (anonymous access) and 401 (auth required) both prove the
	// mirror is alive + TLS is fine. Anything else is degraded.
	ok := resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusUnauthorized
	return &RegistryCachePingResult{
		OK:        ok,
		Status:    resp.StatusCode,
		LatencyMs: latency,
		MirrorURL: url,
	}
}

func modeLabel(insecure bool) string {
	if insecure {
		return "insecure-registries (HTTP, lab/dev only)"
	}
	return "TLS with distributed CA"
}

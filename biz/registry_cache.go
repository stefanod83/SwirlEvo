package biz

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"time"
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

// RegistryCachePrefixPattern is the allowed character set for mirror path
// prefixes. Slug-style keeps the resulting URL clean and unambiguous
// (`<hostname>:<port>/<prefix>/<repo>:<tag>`).
var RegistryCachePrefixPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// validRewriteModes enumerates the RewriteMode values accepted by the
// deploy-time image rewriter in Phase 3. Anything else is rejected at
// Save time.
var validRewriteModes = map[string]bool{
	"off":      true,
	"per-host": true,
	"always":   true,
}

// normalizeRegistryCache fills in derived fields (CA fingerprint, default
// port, default RewriteMode) and trims whitespace so follow-up validation
// operates on a clean payload. The input/output type is the generic
// map[string]interface{} that SettingBiz.Save decodes from the incoming
// JSON RawMessage.
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
	// Normalize upstream mappings — trim + lower-case host, trim prefix.
	if raw, ok := m["upstreams"].([]interface{}); ok {
		out := make([]interface{}, 0, len(raw))
		for _, item := range raw {
			entry, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if u, ok := entry["upstream"].(string); ok {
				entry["upstream"] = strings.ToLower(strings.TrimSpace(u))
			}
			if p, ok := entry["prefix"].(string); ok {
				entry["prefix"] = strings.ToLower(strings.TrimSpace(p))
			}
			out = append(out, entry)
		}
		m["upstreams"] = out
	}
	return m
}

// validateRegistryCache enforces the invariants the downstream consumers
// rely on: non-empty hostname + port when enabled, unique prefixes,
// well-formed prefix slugs. Disabled settings are allowed to contain
// partial/empty values so operators can save work-in-progress.
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
	ups, ok := m["upstreams"].([]interface{})
	if !ok || len(ups) == 0 {
		return errors.New("registry_cache: at least one upstream mapping is required when enabled")
	}
	seenPrefix := make(map[string]bool, len(ups))
	seenUpstream := make(map[string]bool, len(ups))
	for i, item := range ups {
		entry, ok := item.(map[string]interface{})
		if !ok {
			return fmt.Errorf("registry_cache: upstreams[%d] is not an object", i)
		}
		up, _ := entry["upstream"].(string)
		pref, _ := entry["prefix"].(string)
		if up == "" {
			return fmt.Errorf("registry_cache: upstreams[%d].upstream is required", i)
		}
		if pref == "" {
			return fmt.Errorf("registry_cache: upstreams[%d].prefix is required", i)
		}
		if !RegistryCachePrefixPattern.MatchString(pref) {
			return fmt.Errorf("registry_cache: upstreams[%d].prefix %q is not a valid slug (allowed: [a-z0-9][a-z0-9-]{0,31})", i, pref)
		}
		if seenPrefix[pref] {
			return fmt.Errorf("registry_cache: duplicate prefix %q", pref)
		}
		if seenUpstream[up] {
			return fmt.Errorf("registry_cache: duplicate upstream %q", up)
		}
		seenPrefix[pref] = true
		seenUpstream[up] = true
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

func modeLabel(insecure bool) string {
	if insecure {
		return "insecure-registries (HTTP, lab/dev only)"
	}
	return "TLS with distributed CA"
}

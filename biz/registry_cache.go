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

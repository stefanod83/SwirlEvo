package deploy_agent

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/cuigh/auxo/log"
)

// parseCIDRs converts a slice of CIDR strings to *net.IPNet. Empty
// entries and comment lines (starting with '#') are skipped; malformed
// entries return an explicit error so the operator knows which line to
// fix.
//
// If the result would be empty (no valid entries after trimming), the
// caller gets an empty slice — the wrapping serveRecovery applies the
// 127.0.0.1/32 default BEFORE calling parseCIDRs so a degenerate list
// never reaches the middleware.
func parseCIDRs(list []string) ([]*net.IPNet, error) {
	out := make([]*net.IPNet, 0, len(list))
	for _, raw := range list {
		entry := strings.TrimSpace(raw)
		if entry == "" || strings.HasPrefix(entry, "#") {
			continue
		}
		_, ipNet, err := net.ParseCIDR(entry)
		if err != nil {
			// Allow bare-IP entries by promoting them to /32 or /128 so
			// operators don't have to spell out the mask for a single
			// host. Everything else is a hard error.
			ip := net.ParseIP(entry)
			if ip == nil {
				return nil, fmt.Errorf("deploy-agent: invalid CIDR %q: %w", entry, err)
			}
			if ip.To4() != nil {
				_, ipNet, _ = net.ParseCIDR(entry + "/32")
			} else {
				_, ipNet, _ = net.ParseCIDR(entry + "/128")
			}
		}
		out = append(out, ipNet)
	}
	return out, nil
}

// clientIP extracts the caller IP from the request. When trustProxy is
// true and an X-Forwarded-For header is present, the first element of
// the comma-separated list wins. Otherwise the RemoteAddr host part is
// used (strips the ":port" suffix).
//
// Returns nil when neither source yields a parseable IP. Callers should
// treat nil as "deny".
func clientIP(r *http.Request, trustProxy bool) net.IP {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			first := strings.TrimSpace(strings.SplitN(xff, ",", 2)[0])
			if ip := net.ParseIP(first); ip != nil {
				return ip
			}
		}
	}
	host := r.RemoteAddr
	// RemoteAddr is "IP:port" for TCP listeners; SplitHostPort safely
	// handles both IPv4 "1.2.3.4:8080" and IPv6 "[::1]:8080".
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return net.ParseIP(host)
}

// ipAllowMiddleware wraps next so requests whose caller IP is not
// covered by any entry in allow are rejected with 403. The IP is
// resolved via clientIP. When trustProxy is true the X-Forwarded-For
// header is honoured (see clientIP).
//
// When the allow slice is nil or empty the middleware denies
// everything — the serveRecovery helper injects a 127.0.0.1/32 default
// before calling this, so reaching the middleware with an empty list
// means something upstream is misconfigured.
func ipAllowMiddleware(allow []*net.IPNet, trustProxy bool, next http.Handler) http.Handler {
	logger := log.Get("deploy-agent")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r, trustProxy)
		if ip == nil {
			logger.Warnf("recovery: rejecting request with unparseable remote %q", r.RemoteAddr)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		for _, n := range allow {
			if n.Contains(ip) {
				next.ServeHTTP(w, r)
				return
			}
		}
		logger.Warnf("recovery: blocked request from %s (not in allow-list)", ip.String())
		http.Error(w, "forbidden", http.StatusForbidden)
	})
}

// csrfMiddleware enforces a static session token for unsafe HTTP
// methods (POST, PUT, DELETE, PATCH). The token is looked up in the
// X-CSRF-Token header first, then the `_csrf` form value so the bare
// HTML form works without JavaScript.
//
// GET/HEAD/OPTIONS pass through unconditionally — those methods are
// assumed safe (they are for this sidekick: GET / and GET /status.json
// have no side effects).
func csrfMiddleware(token string, next http.Handler) http.Handler {
	if token == "" {
		// Guard against a caller forgetting to generate the token.
		// Rather than silently accept every POST, refuse to start.
		panic("deploy-agent: csrfMiddleware requires a non-empty token")
	}
	logger := log.Get("deploy-agent")
	tokenBytes := []byte(token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isUnsafeMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		supplied := r.Header.Get("X-CSRF-Token")
		if supplied == "" {
			// Parse form only if needed; ParseForm is idempotent so a
			// downstream handler re-parsing is cheap.
			if err := r.ParseForm(); err == nil {
				supplied = r.Form.Get("_csrf")
			}
		}
		if subtle.ConstantTimeCompare([]byte(supplied), tokenBytes) != 1 {
			logger.Warnf("recovery: CSRF mismatch on %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
			http.Error(w, "forbidden: CSRF token missing or invalid", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isUnsafeMethod reports whether the HTTP method is expected to carry
// a CSRF token. Matches the RFC 7231 "unsafe" set.
func isUnsafeMethod(method string) bool {
	switch strings.ToUpper(method) {
	case http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch:
		return true
	default:
		return false
	}
}

// errNoCIDRs is returned when parseCIDRs yields zero valid nets. The
// caller (serveRecovery) converts this into a fall-back to
// 127.0.0.1/32 so a misconfigured job file never ends up binding a
// fully-open recovery UI.
var errNoCIDRs = errors.New("deploy-agent: no CIDRs parsed")

// resolveAllowList produces a validated *net.IPNet slice from the
// operator's configuration. Never returns an empty result — the
// 127.0.0.1/32 safety net kicks in both for empty inputs and for inputs
// that parse to zero entries.
func resolveAllowList(raw []string) ([]*net.IPNet, error) {
	nets, err := parseCIDRs(raw)
	if err != nil {
		return nil, err
	}
	if len(nets) == 0 {
		_, loopback, _ := net.ParseCIDR("127.0.0.1/32")
		return []*net.IPNet{loopback}, nil
	}
	return nets, nil
}

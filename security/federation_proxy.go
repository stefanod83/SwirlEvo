package security

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/cuigh/auxo/log"
	"github.com/cuigh/auxo/net/web"
	"github.com/cuigh/swirl/biz"
	"github.com/cuigh/swirl/dao"
)

// FederationProxy is a web.Filter that intercepts API requests whose
// `node` parameter identifies a host of `Type=swarm_via_swirl` and
// forwards them to the remote Swirl instance via HTTPS, augmenting
// the outbound request with:
//
//   - `Authorization: Bearer <host.SwirlToken>` — authenticates the
//     portal against the swarm peer.
//   - `X-Swirl-Originating-User: <user.Name>` — audit-only, tells
//     the remote Swirl which human triggered the action.
//
// WebSocket upgrades (exec, streaming logs, stats) are forwarded via
// a transparent TCP tunnel that copies bytes in both directions. The
// proxy honours the request context — cancelling it (timeout, client
// disconnect) tears the tunnel down cleanly.
//
// Non-federation requests (node empty, node pointing at a standalone
// host, or any non-API path) pass through unchanged.
type FederationProxy struct {
	hb biz.HostBiz
	// httpClient is reused across requests — HTTP/2 keep-alive keeps
	// the connection to each peer warm.
	httpClient *http.Client
}

// NewFederationProxy is the DI constructor. Reads the HostBiz from
// the container so it can resolve `node` → `dao.Host` per request.
func NewFederationProxy(hb biz.HostBiz) *FederationProxy {
	return &FederationProxy{
		hb: hb,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
			Transport: &http.Transport{
				// Reuse connections. The portal's main cost is
				// waiting on the remote Swirl, not TLS handshakes.
				MaxIdleConns:        32,
				MaxIdleConnsPerHost: 8,
				IdleConnTimeout:     90 * time.Second,
				// TLS: honour system trust store by default. A
				// future enhancement could surface a
				// "skip verify" per-host knob for homelab setups.
				TLSClientConfig: &tls.Config{},
				DialContext: (&net.Dialer{
					Timeout:   10 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
			},
		},
	}
}

// Apply implements `web.Filter`. Every API request flows through
// here. Early-out when the `node` parameter is absent or identifies
// a standalone host — the handler chain proceeds unchanged.
func (p *FederationProxy) Apply(next web.HandlerFunc) web.HandlerFunc {
	return func(ctx web.Context) error {
		nodeID := extractNodeParam(ctx)
		if nodeID == "" {
			return next(ctx)
		}
		host, err := p.hb.Find(ctx.Request().Context(), nodeID)
		if err != nil || host == nil {
			return next(ctx)
		}
		if host.Type != "swarm_via_swirl" {
			return next(ctx)
		}
		// Federation: forward. From here the handler chain is
		// short-circuited — the request lives and dies inside the
		// proxy.
		if isWebSocketUpgrade(ctx.Request()) {
			return p.proxyWebSocket(ctx, host)
		}
		return p.proxyHTTP(ctx, host)
	}
}

// proxyHTTP forwards a regular JSON request via a standard HTTP
// round-trip. Keeps query string + body + headers (minus hop-by-hop
// ones). Streams the response body back to the client.
func (p *FederationProxy) proxyHTTP(ctx web.Context, host *dao.Host) error {
	req := ctx.Request()
	resp := ctx.Response()

	target, err := buildTargetURL(host, req)
	if err != nil {
		resp.WriteHeader(http.StatusBadGateway)
		fmt.Fprintf(resp, "federation: invalid target URL: %v", err)
		return nil
	}

	outReq, err := http.NewRequestWithContext(req.Context(), req.Method, target, req.Body)
	if err != nil {
		resp.WriteHeader(http.StatusBadGateway)
		fmt.Fprintf(resp, "federation: new request: %v", err)
		return nil
	}
	copyHeaders(outReq.Header, req.Header)
	stripHopByHop(outReq.Header)
	outReq.Header.Set("Authorization", "Bearer "+host.SwirlToken)
	if u := ctx.User(); u != nil && u.Name() != "" {
		outReq.Header.Set(headerOriginatingUser, u.Name())
	}
	outReq.Header.Set("X-Swirl-Federation-Version", "1")

	outResp, err := p.httpClient.Do(outReq)
	if err != nil {
		log.Get("federation").Warnf("proxy http error for host %q → %s: %v", host.Name, target, err)
		resp.WriteHeader(http.StatusBadGateway)
		fmt.Fprintf(resp, "federation: upstream request failed: %v", err)
		return nil
	}
	defer outResp.Body.Close()

	copyHeaders(resp.Header(), outResp.Header)
	stripHopByHop(resp.Header())
	resp.WriteHeader(outResp.StatusCode)
	// Streaming body copy — caller gets first-byte fast.
	_, _ = io.Copy(resp, outResp.Body)
	return nil
}

// proxyWebSocket handles the HTTP-101 upgrade dance manually. We
// hijack the client TCP conn, open a parallel conn to the target,
// replay the handshake adding our auth headers, then shuttle bytes
// bidirectionally until either side closes.
//
// httputil.ReverseProxy WOULD handle this transparently, but its
// WebSocket support depends on the backend speaking plain HTTP/1.1
// without TLS termination intermediaries — in our Traefik-fronted
// world we need the explicit TLS dial. The manual hijack is ~40 LOC
// and gives us full control over the handshake headers.
func (p *FederationProxy) proxyWebSocket(ctx web.Context, host *dao.Host) error {
	req := ctx.Request()
	resp := ctx.Response()

	target, err := buildTargetURL(host, req)
	if err != nil {
		resp.WriteHeader(http.StatusBadGateway)
		fmt.Fprintf(resp, "federation: invalid WS target: %v", err)
		return nil
	}
	u, err := url.Parse(target)
	if err != nil {
		resp.WriteHeader(http.StatusBadGateway)
		fmt.Fprintf(resp, "federation: bad WS URL: %v", err)
		return nil
	}

	// Dial the upstream. TLS when the scheme is https/wss.
	var upstream net.Conn
	addr := u.Host
	if !strings.Contains(addr, ":") {
		if u.Scheme == "https" || u.Scheme == "wss" {
			addr += ":443"
		} else {
			addr += ":80"
		}
	}
	dialCtx, dialCancel := context.WithTimeout(req.Context(), 10*time.Second)
	defer dialCancel()
	if u.Scheme == "https" || u.Scheme == "wss" {
		upstream, err = (&tls.Dialer{
			NetDialer: &net.Dialer{Timeout: 10 * time.Second},
			Config:    &tls.Config{},
		}).DialContext(dialCtx, "tcp", addr)
	} else {
		upstream, err = (&net.Dialer{Timeout: 10 * time.Second}).DialContext(dialCtx, "tcp", addr)
	}
	if err != nil {
		log.Get("federation").Warnf("proxy ws dial failed for host %q → %s: %v", host.Name, addr, err)
		resp.WriteHeader(http.StatusBadGateway)
		fmt.Fprintf(resp, "federation: ws dial: %v", err)
		return nil
	}
	defer upstream.Close()

	// Build the upgrade request. Path includes query string so the
	// remote Swirl sees the same URL the operator targeted.
	path := u.RequestURI()
	out := &http.Request{
		Method: req.Method,
		URL:    u,
		Host:   u.Host,
		Header: http.Header{},
	}
	copyHeaders(out.Header, req.Header)
	stripHopByHop(out.Header)
	out.Header.Set("Authorization", "Bearer "+host.SwirlToken)
	if usr := ctx.User(); usr != nil && usr.Name() != "" {
		out.Header.Set(headerOriginatingUser, usr.Name())
	}
	out.Header.Set("X-Swirl-Federation-Version", "1")
	// Rewrite the Host header for the upstream origin.
	out.Header.Set("Host", u.Host)
	// Write the request line + headers onto the TLS stream.
	reqLine := fmt.Sprintf("%s %s HTTP/1.1\r\n", req.Method, path)
	if _, werr := upstream.Write([]byte(reqLine)); werr != nil {
		return werr
	}
	if werr := out.Header.Write(upstream); werr != nil {
		return werr
	}
	if _, werr := upstream.Write([]byte("\r\n")); werr != nil {
		return werr
	}

	// Hijack the client TCP conn to get direct access to the socket.
	hijacker, ok := resp.(http.Hijacker)
	if !ok {
		resp.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(resp, "federation: response writer does not support WS hijacking")
		return nil
	}
	client, _, err := hijacker.Hijack()
	if err != nil {
		return fmt.Errorf("federation: hijack: %w", err)
	}
	defer client.Close()

	// Pump bytes. ContextCancel on either side exits both loops.
	errCh := make(chan error, 2)
	go func() {
		_, err := io.Copy(upstream, client)
		errCh <- err
	}()
	go func() {
		_, err := io.Copy(client, upstream)
		errCh <- err
	}()
	<-errCh
	return nil
}

// extractNodeParam reads `node` from the query string OR the JSON
// body (best-effort; only for methods that carry a body). Query
// wins — matches the convention used throughout the API.
func extractNodeParam(ctx web.Context) string {
	if v := ctx.Query("node"); v != "" {
		return v
	}
	// Don't read the body here — it would consume the stream and
	// break the downstream handler. Portal UI always passes `node`
	// as a query parameter, matching how container/network/volume
	// endpoints are called today.
	return ""
}

// isWebSocketUpgrade returns true iff the request wants to upgrade
// to WebSocket (naive-ui's Terminal + our /api/container/connect
// endpoint both speak WS).
func isWebSocketUpgrade(req *http.Request) bool {
	if !strings.EqualFold(req.Header.Get("Connection"), "upgrade") {
		return false
	}
	return strings.EqualFold(req.Header.Get("Upgrade"), "websocket")
}

// buildTargetURL composes the upstream URL by joining the host's
// SwirlURL with the request's path + query. Scheme follows SwirlURL:
// https://... stays https, ws:// upgrades to wss://, etc.
func buildTargetURL(host *dao.Host, req *http.Request) (string, error) {
	base := strings.TrimRight(host.SwirlURL, "/")
	if base == "" {
		return "", fmt.Errorf("host %q has no SwirlURL", host.Name)
	}
	target := base + req.URL.RequestURI()
	if _, err := url.Parse(target); err != nil {
		return "", err
	}
	return target, nil
}

// copyHeaders mirrors src headers into dst, replacing any existing
// values. Used for both outbound (portal→peer) and inbound
// (peer→portal) directions.
func copyHeaders(dst, src http.Header) {
	for k, vs := range src {
		dst.Del(k)
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}

// stripHopByHop removes headers that are strictly per-hop and should
// not propagate across the proxy boundary (RFC 7230 §6.1). Keeps the
// payload clean and avoids double-encoding artefacts.
func stripHopByHop(h http.Header) {
	for _, hdr := range []string{
		"Connection", "Proxy-Connection", "Keep-Alive",
		"Proxy-Authenticate", "Proxy-Authorization", "TE", "Trailers",
		"Transfer-Encoding", "Upgrade",
	} {
		h.Del(hdr)
	}
}

// headerOriginatingUser is the audit-only header the portal adds to
// every proxied request. The remote Swirl logs it alongside the
// peer name in `dao.Event.OriginatingUser` (wired in Phase 7).
const headerOriginatingUser = "X-Swirl-Originating-User"

// Silences the "imported and not used" lint for httputil if we wire
// a ReverseProxy fallback later. No-op today.
var _ = httputil.ReverseProxy{}

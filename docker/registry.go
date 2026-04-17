package docker

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/cuigh/swirl/dao"
)

// RegistryClient is a minimal Docker Registry v2 HTTP client used by the
// Swirl "Browse registry" feature. It authenticates with HTTP Basic using
// the credentials stored on the `dao.Registry` record. Not a substitute
// for the Docker SDK — the daemon still does the actual pull/push; this
// client is only for catalog/tag listing.
type RegistryClient struct {
	mu      sync.Mutex
	clients map[string]*http.Client
	hash    map[string]string
}

// NewRegistryClient returns a client with an empty cache.
func NewRegistryClient() *RegistryClient {
	return &RegistryClient{
		clients: map[string]*http.Client{},
		hash:    map[string]string{},
	}
}

// Ping checks if the registry is reachable and optionally authenticated.
// Returns nil on success; the error message on failure is user-visible.
func (rc *RegistryClient) Ping(ctx context.Context, r *dao.Registry) error {
	cli, err := rc.clientFor(r)
	if err != nil {
		return err
	}
	base := strings.TrimRight(r.URL, "/")
	resp, _, err := rc.doRequest(ctx, cli, r, http.MethodGet, base+"/v2/", "")
	if err != nil {
		return err
	}
	// 401 with Bearer challenge means reachable but may need token auth
	// for actual operations — that's fine for a ping.
	if resp.StatusCode == http.StatusUnauthorized {
		challenge := resp.Header.Get("Www-Authenticate")
		if strings.HasPrefix(challenge, "Bearer ") {
			token, terr := rc.fetchBearerToken(ctx, cli, r, challenge)
			if terr != nil {
				return fmt.Errorf("auth: %w", terr)
			}
			resp2, _, err2 := rc.doRequest(ctx, cli, r, http.MethodGet, base+"/v2/", token)
			if err2 != nil {
				return err2
			}
			if resp2.StatusCode == http.StatusUnauthorized {
				return fmt.Errorf("authentication failed after token exchange")
			}
			if resp2.StatusCode >= 400 {
				return fmt.Errorf("http %d", resp2.StatusCode)
			}
			return nil
		}
		return fmt.Errorf("authentication failed (check username/password)")
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("http %d", resp.StatusCode)
	}
	return nil
}

// CatalogList returns a page of repositories from the registry catalog.
// `pageSize` caps the returned slice (registry min is usually 1, max is
// server-defined — 100 is a safe sweet spot). `last` resumes a prior
// page; pass "" for the first call. Returned `next` is the `last` value
// to use on the follow-up call, or "" when the catalog is exhausted.
func (rc *RegistryClient) CatalogList(ctx context.Context, r *dao.Registry, pageSize int, last string) (repos []string, next string, err error) {
	if pageSize <= 0 {
		pageSize = 100
	}
	q := url.Values{}
	q.Set("n", fmt.Sprintf("%d", pageSize))
	if last != "" {
		q.Set("last", last)
	}
	var env struct {
		Repositories []string `json:"repositories"`
	}
	resp, body, err := rc.doJSON(ctx, r, http.MethodGet, "/v2/_catalog", q, &env)
	if err != nil {
		return nil, "", err
	}
	// Link header "</v2/_catalog?last=foo&n=100>; rel=\"next\"" → use "foo" for next call.
	next = parseNextLast(resp.Header.Get("Link"))
	_ = body
	return env.Repositories, next, nil
}

// TagsList returns all tags for a given repository. Docker registries
// typically return them all in one response; pagination exists but is
// rarely needed here.
func (rc *RegistryClient) TagsList(ctx context.Context, r *dao.Registry, repo string) ([]string, error) {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return nil, fmt.Errorf("repository name is required")
	}
	var env struct {
		Tags []string `json:"tags"`
	}
	_, _, err := rc.doJSON(ctx, r, http.MethodGet, "/v2/"+repo+"/tags/list", nil, &env)
	if err != nil {
		return nil, err
	}
	return env.Tags, nil
}

// ---- internals ----------------------------------------------------------

func (rc *RegistryClient) doJSON(ctx context.Context, r *dao.Registry, method, path string, q url.Values, out any) (*http.Response, []byte, error) {
	cli, err := rc.clientFor(r)
	if err != nil {
		return nil, nil, err
	}
	base := strings.TrimRight(r.URL, "/")
	u := base + path
	if len(q) > 0 {
		u = u + "?" + q.Encode()
	}

	// First attempt — try Basic auth if credentials are present.
	resp, body, err := rc.doRequest(ctx, cli, r, method, u, "")
	if err != nil {
		return nil, nil, err
	}

	// If the registry responds with 401 + a Bearer challenge, fetch a
	// token from the realm and retry with Bearer auth. This is the
	// standard Docker Registry v2 token authentication flow used by
	// Docker Hub, Harbor, GitLab, and most hosted registries.
	if resp.StatusCode == http.StatusUnauthorized {
		challenge := resp.Header.Get("Www-Authenticate")
		if strings.HasPrefix(challenge, "Bearer ") {
			token, terr := rc.fetchBearerToken(ctx, cli, r, challenge)
			if terr != nil {
				return nil, body, fmt.Errorf("registry %s: token auth failed: %w", r.URL, terr)
			}
			resp, body, err = rc.doRequest(ctx, cli, r, method, u, token)
			if err != nil {
				return nil, nil, err
			}
		}
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, body, fmt.Errorf("registry %s: authentication failed (check username/password)", r.URL)
	}
	if resp.StatusCode >= 400 {
		return nil, body, fmt.Errorf("registry %s %s: http %d: %s", r.URL, path, resp.StatusCode, truncate(string(body), 300))
	}
	if out != nil && len(body) > 0 {
		if err := json.Unmarshal(body, out); err != nil {
			return nil, body, fmt.Errorf("decode %s: %w", path, err)
		}
	}
	return resp, body, nil
}

func (rc *RegistryClient) doRequest(ctx context.Context, cli *http.Client, r *dao.Registry, method, u, bearerToken string) (*http.Response, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return nil, nil, err
	}
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	} else if r.Username != "" || r.Password != "" {
		req.SetBasicAuth(r.Username, r.Password)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := cli.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, body, nil
}

// fetchBearerToken implements the token exchange described in the
// Docker Registry v2 Token Authentication spec. It parses the
// `Www-Authenticate: Bearer realm="...",service="...",scope="..."`
// challenge and fetches a short-lived token from the realm.
func (rc *RegistryClient) fetchBearerToken(ctx context.Context, cli *http.Client, r *dao.Registry, challenge string) (string, error) {
	params := parseBearerChallenge(challenge)
	realm := params["realm"]
	if realm == "" {
		return "", fmt.Errorf("bearer challenge missing realm: %s", challenge)
	}
	q := url.Values{}
	if svc := params["service"]; svc != "" {
		q.Set("service", svc)
	}
	if scope := params["scope"]; scope != "" {
		q.Set("scope", scope)
	}
	tokenURL := realm
	if len(q) > 0 {
		tokenURL += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tokenURL, nil)
	if err != nil {
		return "", err
	}
	if r.Username != "" {
		req.SetBasicAuth(r.Username, r.Password)
	}
	resp, err := cli.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("token endpoint http %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	var env struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}
	tok := env.Token
	if tok == "" {
		tok = env.AccessToken
	}
	if tok == "" {
		return "", fmt.Errorf("token endpoint returned empty token")
	}
	return tok, nil
}

// parseBearerChallenge extracts key=value pairs from a
// `Bearer realm="...",service="...",scope="..."` header.
func parseBearerChallenge(header string) map[string]string {
	out := map[string]string{}
	s := strings.TrimPrefix(header, "Bearer ")
	for _, part := range strings.Split(s, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) == 2 {
			out[kv[0]] = strings.Trim(kv[1], `"`)
		}
	}
	return out
}

// clientFor returns an HTTP client with TLS settings matching the
// registry's SkipTLSVerify. The client is cached per registry.ID and
// rebuilt when `SkipTLSVerify` or `URL` changes.
func (rc *RegistryClient) clientFor(r *dao.Registry) (*http.Client, error) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	key := r.ID
	h := fmt.Sprintf("%s|%t", r.URL, r.SkipTLSVerify)
	if cached, ok := rc.clients[key]; ok && rc.hash[key] == h {
		return cached, nil
	}
	tr := &http.Transport{
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: r.SkipTLSVerify, MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout: 5 * time.Second,
	}
	cli := &http.Client{Transport: tr, Timeout: 30 * time.Second}
	rc.clients[key] = cli
	rc.hash[key] = h
	return cli, nil
}

// parseNextLast extracts the `last=X` query param from a Link header.
// The header shape is `</v2/_catalog?last=foo&n=100>; rel="next"`.
// Returns "" when absent.
var nextLastRe = regexp.MustCompile(`last=([^&>]+)`)

func parseNextLast(link string) string {
	if link == "" {
		return ""
	}
	m := nextLastRe.FindStringSubmatch(link)
	if len(m) < 2 {
		return ""
	}
	// The registry URL-encodes `last`; decode.
	if dec, err := url.QueryUnescape(m[1]); err == nil {
		return dec
	}
	return m[1]
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

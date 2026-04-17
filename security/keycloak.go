package security

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/cuigh/auxo/log"
	"github.com/cuigh/swirl/biz"
	"github.com/cuigh/swirl/dao"
	"github.com/cuigh/swirl/misc"
	"golang.org/x/oauth2"
)

// oidcHTTPClient is a shared HTTP client for OIDC/OAuth2 calls (discovery,
// token exchange, JWKS fetch). DisableKeepAlives avoids "Misdirected
// Request" issues from reverse proxies that close idle connections.
var oidcHTTPClient = &http.Client{
	Transport: &http.Transport{DisableKeepAlives: true},
	Timeout:   30 * time.Second,
}

// oidcCtx injects the custom HTTP client into the context so the oauth2
// and oidc libraries use it instead of http.DefaultClient.
func oidcCtx(ctx context.Context) context.Context {
	return context.WithValue(ctx, oauth2.HTTPClient, oidcHTTPClient)
}

// KeycloakClient is a lazy-initialised OIDC helper. The provider is cached for
// 1 hour; on config change (issuer / client id / secret) it is rebuilt on
// next access. NOT thread-safe on config mutation — that's fine because
// settings live behind the Save endpoint which serialises changes.
type KeycloakClient struct {
	settingLoader func() *misc.Setting
	ub            biz.UserBiz
	rb            biz.RoleBiz

	mu          sync.Mutex
	provider    *oidc.Provider
	verifier    *oidc.IDTokenVerifier
	cfg         *oauth2.Config
	builtAt     time.Time
	builtIssuer string
	builtClient string
	builtSecret string
	builtScopes string
	logger      log.Logger
}

func NewKeycloakClient(loader func() *misc.Setting, ub biz.UserBiz, rb biz.RoleBiz) *KeycloakClient {
	return &KeycloakClient{settingLoader: loader, ub: ub, rb: rb, logger: log.Get(PkgName)}
}

// IsEnabled tells whether Keycloak authentication is active.
func (k *KeycloakClient) IsEnabled() bool {
	s := k.settingLoader()
	return s != nil && s.Keycloak.Enabled && s.Keycloak.IssuerURL != "" && s.Keycloak.ClientID != ""
}

// ensure builds/refreshes the OIDC provider if needed.
func (k *KeycloakClient) ensure(ctx context.Context) error {
	s := k.settingLoader()
	if s == nil || !s.Keycloak.Enabled {
		return errors.New("keycloak is not enabled")
	}
	k.mu.Lock()
	defer k.mu.Unlock()

	configChanged := s.Keycloak.IssuerURL != k.builtIssuer ||
		s.Keycloak.ClientID != k.builtClient ||
		s.Keycloak.ClientSecret != k.builtSecret ||
		s.Keycloak.Scopes != k.builtScopes
	expired := time.Since(k.builtAt) > time.Hour
	if k.provider != nil && !configChanged && !expired {
		return nil
	}

	p, err := oidc.NewProvider(oidcCtx(ctx), s.Keycloak.IssuerURL)
	if err != nil {
		return fmt.Errorf("oidc provider: %w", err)
	}
	scopes := strings.Fields(s.Keycloak.Scopes)
	if len(scopes) == 0 {
		scopes = []string{oidc.ScopeOpenID, "profile", "email"}
	}
	k.provider = p
	k.verifier = p.Verifier(&oidc.Config{ClientID: s.Keycloak.ClientID})
	k.cfg = &oauth2.Config{
		ClientID:     s.Keycloak.ClientID,
		ClientSecret: s.Keycloak.ClientSecret,
		Endpoint:     p.Endpoint(),
		RedirectURL:  s.Keycloak.RedirectURI,
		Scopes:       scopes,
	}
	k.builtAt = time.Now()
	k.builtIssuer = s.Keycloak.IssuerURL
	k.builtClient = s.Keycloak.ClientID
	k.builtSecret = s.Keycloak.ClientSecret
	k.builtScopes = s.Keycloak.Scopes
	return nil
}

// Diagnose runs a series of checks on the Keycloak OIDC configuration
// and returns structured results so the Settings UI can tell the operator
// exactly what's wrong. Each check is independent: a failure in one
// does not abort the rest.
func (k *KeycloakClient) Diagnose(ctx context.Context) map[string]interface{} {
	result := map[string]interface{}{}
	s := k.settingLoader()
	if s == nil {
		result["config"] = map[string]interface{}{"ok": false, "error": "settings not loaded"}
		return result
	}

	// Check 1: config completeness
	configOK := s.Keycloak.Enabled && s.Keycloak.IssuerURL != "" && s.Keycloak.ClientID != ""
	configResult := map[string]interface{}{
		"ok":        configOK,
		"enabled":   s.Keycloak.Enabled,
		"issuerUrl": s.Keycloak.IssuerURL,
		"clientId":  s.Keycloak.ClientID,
		"secretSet": s.Keycloak.ClientSecret != "",
	}
	if !configOK {
		configResult["error"] = "enabled, issuer_url, and client_id are all required"
	}
	result["config"] = configResult

	// Check 2: OIDC discovery
	discoveryResult := map[string]interface{}{"ok": false}
	if s.Keycloak.IssuerURL != "" {
		p, err := oidc.NewProvider(oidcCtx(ctx), s.Keycloak.IssuerURL)
		if err != nil {
			discoveryResult["error"] = err.Error()
		} else {
			ep := p.Endpoint()
			discoveryResult["ok"] = true
			discoveryResult["authEndpoint"] = ep.AuthURL
			discoveryResult["tokenEndpoint"] = ep.TokenURL
		}
	} else {
		discoveryResult["error"] = "issuer_url is empty"
	}
	result["discovery"] = discoveryResult

	// Check 3: redirect_uri
	redirectResult := map[string]interface{}{
		"configured": s.Keycloak.RedirectURI,
	}
	if s.Keycloak.RedirectURI == "" {
		redirectResult["ok"] = false
		redirectResult["error"] = "redirect_uri is empty — it should be <your-origin>/api/auth/keycloak/callback"
	} else {
		redirectResult["ok"] = true
		redirectResult["hint"] = "make sure this EXACT URL is in Keycloak's 'Valid Redirect URIs' for this client"
	}
	result["redirect_uri"] = redirectResult

	// Check 4: try building an auth URL (validates the full pipeline)
	authURLResult := map[string]interface{}{"ok": false}
	if configOK {
		if err := k.ensure(ctx); err != nil {
			authURLResult["error"] = err.Error()
		} else {
			authURL, err := k.AuthCodeURL(ctx, "test-state")
			if err != nil {
				authURLResult["error"] = err.Error()
			} else {
				authURLResult["ok"] = true
				authURLResult["authUrl"] = authURL
			}
		}
	} else {
		authURLResult["error"] = "config incomplete — fix config check first"
	}
	result["auth_url"] = authURLResult

	return result
}

// AuthCodeURL returns the redirect URL for the initial login step.
func (k *KeycloakClient) AuthCodeURL(ctx context.Context, state string) (string, error) {
	if err := k.ensure(ctx); err != nil {
		return "", err
	}
	return k.cfg.AuthCodeURL(state), nil
}

// Claims extracted from the Keycloak ID token.
type KeycloakClaims struct {
	Subject  string   `json:"sub"`
	Username string   `json:"preferred_username"`
	Name     string   `json:"name"`
	Email    string   `json:"email"`
	Groups   []string `json:"groups"`
	Extra    map[string]any
	IDToken  string
}

// Exchange swaps a code for tokens, verifies the ID token and extracts claims.
func (k *KeycloakClient) Exchange(ctx context.Context, code string) (*KeycloakClaims, error) {
	if err := k.ensure(ctx); err != nil {
		return nil, err
	}
	tok, err := k.cfg.Exchange(oidcCtx(ctx), code)
	if err != nil {
		return nil, fmt.Errorf("code exchange: %w", err)
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok || rawID == "" {
		return nil, errors.New("id_token missing from token response")
	}
	idToken, err := k.verifier.Verify(oidcCtx(ctx), rawID)
	if err != nil {
		return nil, fmt.Errorf("verify id_token: %w", err)
	}

	s := k.settingLoader()
	usernameClaim := defaultString(s.Keycloak.UsernameClaim, "preferred_username")
	emailClaim := defaultString(s.Keycloak.EmailClaim, "email")
	groupsClaim := defaultString(s.Keycloak.GroupsClaim, "groups")

	var raw map[string]any
	if err := idToken.Claims(&raw); err != nil {
		return nil, fmt.Errorf("parse claims: %w", err)
	}
	c := &KeycloakClaims{IDToken: rawID, Extra: raw}
	c.Subject, _ = raw["sub"].(string)
	c.Username, _ = raw[usernameClaim].(string)
	c.Name, _ = raw["name"].(string)
	c.Email, _ = raw[emailClaim].(string)
	if v, ok := raw[groupsClaim]; ok {
		switch arr := v.(type) {
		case []any:
			for _, it := range arr {
				if str, ok := it.(string); ok {
					c.Groups = append(c.Groups, strings.TrimPrefix(str, "/"))
				}
			}
		case []string:
			for _, str := range arr {
				c.Groups = append(c.Groups, strings.TrimPrefix(str, "/"))
			}
		}
	}
	if c.Username == "" {
		c.Username = c.Subject
	}
	if c.Name == "" {
		c.Name = c.Username
	}
	return c, nil
}

// LogoutURL returns the upstream logout URL (RP-initiated). If EnableLogout is
// false or the endpoint is not discoverable, returns an empty string.
func (k *KeycloakClient) LogoutURL(ctx context.Context, idToken, postLogoutRedirect string) (string, error) {
	if err := k.ensure(ctx); err != nil {
		return "", err
	}
	s := k.settingLoader()
	if !s.Keycloak.EnableLogout {
		return "", nil
	}
	var endCfg struct {
		EndSessionURL string `json:"end_session_endpoint"`
	}
	if err := k.provider.Claims(&endCfg); err != nil || endCfg.EndSessionURL == "" {
		return "", err
	}
	u := endCfg.EndSessionURL
	sep := "?"
	if strings.Contains(u, "?") {
		sep = "&"
	}
	u += sep + "id_token_hint=" + idToken
	if postLogoutRedirect != "" {
		u += "&post_logout_redirect_uri=" + postLogoutRedirect
	}
	return u, nil
}

// ResolveUser upserts a Swirl user from Keycloak claims and returns the local
// user id. Applies group→role mapping from settings. Returns an error if the
// claimed login name collides with an existing user of a different type.
func (k *KeycloakClient) ResolveUser(ctx context.Context, claims *KeycloakClaims) (string, error) {
	s := k.settingLoader()
	existing, err := k.ub.FindByName(ctx, claims.Username)
	if err != nil {
		return "", err
	}
	roles := k.resolveRoles(ctx, claims.Groups, s.Keycloak.GroupRoleMap)
	k.logger.Infof("keycloak resolve: user=%s groups=%d matched_roles=%v", claims.Username, len(claims.Groups), roles)

	if existing != nil {
		if existing.Type != biz.UserTypeKeycloak {
			return "", fmt.Errorf("loginname %q already exists with type=%s — cannot reuse for Keycloak", claims.Username, existing.Type)
		}
		// Update profile from the latest token.
		existing.Name = claims.Name
		existing.Email = claims.Email
		if len(roles) > 0 {
			existing.Roles = roles
		}
		if err := k.ub.Update(ctx, existing, nil); err != nil {
			return "", err
		}
		return existing.ID, nil
	}
	if !s.Keycloak.AutoCreateUser {
		return "", errors.New("user not provisioned and auto-create-user is disabled")
	}
	user := &dao.User{
		Type:      biz.UserTypeKeycloak,
		LoginName: claims.Username,
		Name:      claims.Name,
		Email:     claims.Email,
		Roles:     roles,
	}
	return k.ub.Create(ctx, user, nil)
}

// resolveRoles maps Keycloak group names to Swirl role IDs. The mapping
// value can be either a role ID (old format) or a role NAME (new format
// that survives backup/restore across instances). If the value is not a
// valid role ID, it's looked up by name. This makes the mapping portable.
func (k *KeycloakClient) resolveRoles(ctx context.Context, groups []string, mapping map[string]string) []string {
	if len(mapping) == 0 || len(groups) == 0 {
		return nil
	}
	// Cache roles by name for this resolution pass (avoid N DB hits).
	var allRoles []*dao.Role
	rolesLoaded := false

	seen := map[string]struct{}{}
	var out []string
	for _, g := range groups {
		// Try both "/group" and "group" formats — Keycloak sends
		// "/appFoo" (Full group path ON) but TrimPrefix strips the
		// slash when parsing claims. The UI mapping may have either.
		val, ok := mapping[g]
		if !ok {
			val, ok = mapping["/"+g]
		}
		if !ok {
			val, ok = mapping[strings.TrimPrefix(g, "/")]
		}
		if !ok || val == "" {
			continue
		}
		roleID := val
		// Try to find the role by ID first. If not found, treat val
		// as a role name and look up the ID.
		if k.rb != nil {
			role, _ := k.rb.Find(ctx, val)
			if role == nil {
				// Not found by ID — try by name
				if !rolesLoaded {
					allRoles, _ = k.rb.Search(ctx, "")
					rolesLoaded = true
				}
				for _, r := range allRoles {
					if strings.EqualFold(r.Name, val) {
						roleID = r.ID
						break
					}
				}
			}
		}
		if _, dup := seen[roleID]; !dup {
			out = append(out, roleID)
			seen[roleID] = struct{}{}
		}
	}
	return out
}

func defaultString(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/url"

	"github.com/cuigh/auxo/app/container"
	"github.com/cuigh/auxo/data"
	"github.com/cuigh/auxo/log"
	"github.com/cuigh/auxo/net/web"
	"github.com/cuigh/swirl/misc"
	"github.com/cuigh/swirl/security"
)

// AuthHandler exposes OIDC / external identity-provider endpoints.
// Mounted at /api/auth by main.go (container name "api.auth"), so the
// path tags below are RELATIVE to /api/auth — e.g. "/keycloak/login"
// becomes /api/auth/keycloak/login.
type AuthHandler struct {
	KeycloakLogin     web.HandlerFunc `path:"/keycloak/login" auth:"*" desc:"start Keycloak OIDC flow"`
	KeycloakCallback  web.HandlerFunc `path:"/keycloak/callback" auth:"*" desc:"Keycloak OIDC callback"`
	KeycloakLogoutURL web.HandlerFunc `path:"/keycloak/logout-url" auth:"?" desc:"RP-initiated logout URL"`
}

// NewAuth is registered in api.init.
func NewAuth(kc *security.KeycloakClient, idn *security.Identifier) *AuthHandler {
	return &AuthHandler{
		KeycloakLogin:     keycloakLogin(kc),
		KeycloakCallback:  keycloakCallback(kc, idn),
		KeycloakLogoutURL: keycloakLogoutURL(kc),
	}
}

const (
	kcStateCookie    = "kc_oauth_state"
	kcRedirectCookie = "kc_oauth_redirect"
)

func randomState() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func keycloakLogin(kc *security.KeycloakClient) web.HandlerFunc {
	return func(c web.Context) error {
		if !kc.IsEnabled() {
			return web.NewError(http.StatusNotFound)
		}
		state, err := randomState()
		if err != nil {
			return err
		}
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()

		authURL, err := kc.AuthCodeURL(ctx, state)
		if err != nil {
			return err
		}
		// state cookie for CSRF; post-login redirect cookie for final hop
		redir := c.Query("redirect")
		if redir == "" {
			redir = "/"
		}
		setKCCookie(c, kcStateCookie, state, 10*60)
		setKCCookie(c, kcRedirectCookie, redir, 10*60)
		return c.Redirect(authURL)
	}
}

func keycloakCallback(kc *security.KeycloakClient, idn *security.Identifier) web.HandlerFunc {
	lg := log.Get("keycloak")
	return func(c web.Context) error {
		if !kc.IsEnabled() {
			lg.Warn("keycloak callback: not enabled → 404")
			return web.NewError(http.StatusNotFound)
		}
		code := c.Query("code")
		state := c.Query("state")
		errParam := c.Query("error")
		errDesc := c.Query("error_description")
		// Keycloak may redirect back with ?error=... instead of ?code=...
		// when the user denies consent or the client is misconfigured.
		if errParam != "" {
			lg.Warnf("keycloak callback: Keycloak returned error=%s desc=%s", errParam, errDesc)
			return web.NewError(http.StatusBadRequest, "keycloak: "+errParam+" — "+errDesc)
		}
		if code == "" || state == "" {
			lg.Warnf("keycloak callback: missing code=%t state=%t", code != "", state != "")
			return web.NewError(http.StatusBadRequest, "missing code or state")
		}
		stateCookie, _ := c.Request().Cookie(kcStateCookie)
		if stateCookie == nil || stateCookie.Value != state {
			lg.Warnf("keycloak callback: invalid state (cookie=%v, query=%s)", stateCookie != nil, state)
			return web.NewError(http.StatusBadRequest, "invalid state — cookies may have expired (>10 min) or SameSite blocked them")
		}
		redirCookie, _ := c.Request().Cookie(kcRedirectCookie)
		postLogin := "/"
		if redirCookie != nil && redirCookie.Value != "" {
			postLogin = redirCookie.Value
		}
		clearKCCookie(c, kcStateCookie)
		clearKCCookie(c, kcRedirectCookie)

		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()

		lg.Infof("keycloak callback: exchanging code (len=%d)", len(code))
		claims, err := kc.Exchange(ctx, code)
		if err != nil {
			lg.Warnf("keycloak callback: exchange failed: %v", err)
			return err
		}
		lg.Infof("keycloak callback: resolving user %s", claims.Username)
		userID, err := kc.ResolveUser(ctx, claims)
		if err != nil {
			lg.Warnf("keycloak callback: resolve user failed: %v", err)
			return err
		}
		identity, err := idn.IdentifyExternal(ctx, userID, defaultName(claims))
		if err != nil {
			lg.Warnf("keycloak callback: identify failed: %v", err)
			return err
		}

		frag := url.Values{}
		frag.Set("token", identity.Token())
		frag.Set("name", identity.Name())
		frag.Set("perms", joinPerms(identity.Perms()))
		frag.Set("redirect", postLogin)
		frag.Set("idToken", claims.IDToken)
		dest := "/oauth-complete#" + frag.Encode()
		lg.Infof("keycloak callback: success, redirect to %s (postLogin=%s)", "/oauth-complete#...", postLogin)
		return c.Redirect(dest)
	}
}

func keycloakLogoutURL(kc *security.KeycloakClient) web.HandlerFunc {
	return func(c web.Context) error {
		if !kc.IsEnabled() {
			return success(c, data.Map{"url": ""})
		}
		ctx, cancel := misc.Context(defaultTimeout)
		defer cancel()

		idToken := c.Query("idToken")
		redirect := c.Query("redirect")
		u, err := kc.LogoutURL(ctx, idToken, redirect)
		if err != nil {
			return err
		}
		return success(c, data.Map{"url": u})
	}
}

func defaultName(c *security.KeycloakClaims) string {
	if c.Name != "" {
		return c.Name
	}
	return c.Username
}

func joinPerms(perms []string) string {
	out := ""
	for i, p := range perms {
		if i > 0 {
			out += ","
		}
		out += p
	}
	return out
}

func setKCCookie(c web.Context, name, value string, maxAge int) {
	cookie := &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	}
	http.SetCookie(c.Response(), cookie)
}

func clearKCCookie(c web.Context, name string) {
	setKCCookie(c, name, "", -1)
}

func init() {
	container.Put(NewAuth, container.Name("api.auth"))
}

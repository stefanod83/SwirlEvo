package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/url"

	"github.com/cuigh/auxo/app/container"
	"github.com/cuigh/auxo/data"
	"github.com/cuigh/auxo/net/web"
	"github.com/cuigh/swirl/misc"
	"github.com/cuigh/swirl/security"
)

// AuthHandler exposes OIDC / external identity-provider endpoints.
type AuthHandler struct {
	KeycloakLogin    web.HandlerFunc `path:"/auth/keycloak/login" auth:"*" desc:"start Keycloak OIDC flow"`
	KeycloakCallback web.HandlerFunc `path:"/auth/keycloak/callback" auth:"*" desc:"Keycloak OIDC callback"`
	KeycloakLogoutURL web.HandlerFunc `path:"/auth/keycloak/logout-url" auth:"?" desc:"RP-initiated logout URL"`
}

// NewAuth is registered in api.init.
func NewAuth(kc *security.KeycloakClient, idn *security.Identifier) *AuthHandler {
	return &AuthHandler{
		KeycloakLogin:    keycloakLogin(kc),
		KeycloakCallback: keycloakCallback(kc, idn),
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
	return func(c web.Context) error {
		if !kc.IsEnabled() {
			return web.NewError(http.StatusNotFound)
		}
		code := c.Query("code")
		state := c.Query("state")
		if code == "" || state == "" {
			return web.NewError(http.StatusBadRequest, "missing code or state")
		}
		stateCookie, _ := c.Request().Cookie(kcStateCookie)
		if stateCookie == nil || stateCookie.Value != state {
			return web.NewError(http.StatusBadRequest, "invalid state")
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

		claims, err := kc.Exchange(ctx, code)
		if err != nil {
			return err
		}
		userID, err := kc.ResolveUser(ctx, claims)
		if err != nil {
			return err
		}
		identity, err := idn.IdentifyExternal(ctx, userID, defaultName(claims))
		if err != nil {
			return err
		}

		// Redirect to a client-side bridge page that reads the data from the
		// URL hash, commits it to the store and then navigates to the final
		// destination. Hash is chosen over query to avoid referer/server logs.
		frag := url.Values{}
		frag.Set("token", identity.Token())
		frag.Set("name", identity.Name())
		frag.Set("perms", joinPerms(identity.Perms()))
		frag.Set("redirect", postLogin)
		frag.Set("idToken", claims.IDToken)
		return c.Redirect("/oauth-complete#" + frag.Encode())
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

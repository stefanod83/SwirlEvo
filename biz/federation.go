package biz

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cuigh/auxo/app"
	"github.com/cuigh/auxo/data"
	"github.com/cuigh/auxo/net/web"
	"github.com/cuigh/swirl/dao"
	"github.com/cuigh/swirl/misc"
)

// FederationBiz exposes federation-peer management. A "federation
// peer" is a non-human user account (`User.Type=federation`) whose
// long-lived bearer token authenticates a portal Swirl when it
// proxies calls into this Swirl swarm instance.
//
// Every peer carries a single token entry in `User.Tokens` named
// `"federation-active"` (so the lookup is deterministic). Rotation
// generates a fresh random token and replaces the existing entry —
// no grace-period overlap is attempted intentionally: federated
// portals refresh on rotation via the `/api/federation/renew-token`
// endpoint rather than via local knowledge of the previous token.
type FederationBiz interface {
	// CreatePeer mints a new federation peer user + token. The
	// plaintext token is returned ONCE in the response and never
	// persisted in cleartext outside `User.Tokens` (which is
	// indexed for the FindByToken lookup).
	CreatePeer(ctx context.Context, name string, ttlDays int, creator web.User) (*PeerResult, error)
	// ListPeers returns every federation user with metadata about
	// the token expiry — never the token value itself.
	ListPeers(ctx context.Context) ([]*PeerSummary, error)
	// RotateToken issues a fresh token for the existing peer. The
	// old token is invalidated immediately.
	RotateToken(ctx context.Context, peerID string, ttlDays int, rotator web.User) (*PeerResult, error)
	// Revoke deletes a federation peer entirely. Subsequent requests
	// from that token return 401.
	Revoke(ctx context.Context, peerID string, rotator web.User) error
	// RotateSelf is the self-service rotation a federation peer
	// calls on its OWN token. Authenticates via the peer's current
	// bearer (which resolves to the peer user in `caller`). No admin
	// permission needed — rotating your own credential is inherent.
	// The old token is invalidated immediately; caller must capture
	// the new one from the response.
	RotateSelf(ctx context.Context, ttlDays int, caller web.User) (*PeerResult, error)
	// Capabilities returns the metadata the portal needs to classify
	// this instance after a token-based handshake.
	Capabilities(ctx context.Context) *Capabilities
}

// PeerResult carries the ONE-TIME plaintext token returned to the
// operator at creation/rotation. The API encodes this struct into
// the response body and the caller is expected to copy it out
// immediately — it will not be retrievable again.
type PeerResult struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	LoginName  string    `json:"loginName"`
	Token      string    `json:"token"` // PLAINTEXT — shown once
	ExpiresAt  dao.Time  `json:"expiresAt"`
	CreatedAt  dao.Time  `json:"createdAt"`
}

type PeerSummary struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	LoginName  string    `json:"loginName"`
	ExpiresAt  dao.Time  `json:"expiresAt"`
	CreatedAt  dao.Time  `json:"createdAt"`
	// Expired is computed server-side so the UI doesn't need to
	// parse the date. Soft-expiry (operations keep working) — the
	// banner uses this to warn the operator.
	Expired    bool      `json:"expired"`
}

// Capabilities is the struct returned by GET /api/federation/capabilities.
// Future versions bump `APIVersion`; the portal refuses hosts with
// an API version it does not understand.
type Capabilities struct {
	APIVersion int      `json:"apiVersion"` // bumped on incompatible handshake changes
	Mode       string   `json:"mode"`       // "swarm" | "standalone"
	Version    string   `json:"version"`    // Swirl semver
	Features   []string `json:"features"`   // extensible list of optional capabilities
	Nodes      int      `json:"nodes"`      // cluster nodes for swarm; 1 for standalone
	PeerName   string   `json:"peerName"`   // the peer this token resolved to (audit aid)
}

const (
	// federationAPIVersion is the handshake version. Bumped on
	// incompatible API changes the portal needs to know about.
	federationAPIVersion = 1
	// federationTokenName is the fixed entry name inside
	// User.Tokens for the federation peer's active token. Single
	// active token per peer — rotation replaces it in-place.
	federationTokenName = "federation-active"
	// federationLoginPrefix is prepended to peer login names so they
	// never collide with human accounts.
	federationLoginPrefix = "federation-peer-"
	// federationTokenBytes controls the entropy of the token: 32
	// random bytes → 64-char hex string.
	federationTokenBytes = 32
)

// NewFederation is the DI constructor.
func NewFederation(ub UserBiz, eb EventBiz, di dao.Interface) FederationBiz {
	return &federationBiz{ub: ub, eb: eb, di: di}
}

type federationBiz struct {
	ub UserBiz
	eb EventBiz
	di dao.Interface
}

func (b *federationBiz) CreatePeer(ctx context.Context, name string, ttlDays int, creator web.User) (*PeerResult, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("federation: peer name is required")
	}
	token, err := generateFederationToken()
	if err != nil {
		return nil, err
	}
	expiresAt := computeExpiry(ttlDays)
	loginName := federationLoginPrefix + slugify(name)
	now := now()

	u := &dao.User{
		Name:      name,
		LoginName: loginName,
		Email:     loginName + "@federation.local",
		Type:      UserTypeFederation,
		Status:    UserStatusActive,
		Tokens: data.Options{
			data.Option{Name: federationTokenName, Value: token},
		},
		CreatedAt: now,
		UpdatedAt: now,
		CreatedBy: newOperator(creator),
		UpdatedBy: newOperator(creator),
	}
	id, err := b.ub.Create(ctx, u, creator)
	if err != nil {
		return nil, err
	}

	// Persist expiry on a dedicated field? The User schema has no
	// token-metadata slot, so we encode expiry as a second entry in
	// `Tokens` named "federation-expires-at=<rfc3339>". This keeps
	// everything in the existing BSON document without schema churn.
	u.ID = id
	u.Tokens = data.Options{
		data.Option{Name: federationTokenName, Value: token},
		data.Option{Name: federationExpiresAtMeta, Value: time.Time(expiresAt).Format(time.RFC3339)},
	}
	if uerr := b.di.UserUpdate(ctx, u); uerr != nil {
		return nil, uerr
	}

	if b.eb != nil {
		b.eb.CreateUser(EventActionCreate, id, loginName, creator)
	}
	return &PeerResult{
		ID:        id,
		Name:      name,
		LoginName: loginName,
		Token:     token,
		ExpiresAt: expiresAt,
		CreatedAt: now,
	}, nil
}

func (b *federationBiz) ListPeers(ctx context.Context) ([]*PeerSummary, error) {
	users, _, err := b.ub.Search(ctx, "", "", "", 1, 10000)
	if err != nil {
		return nil, err
	}
	out := make([]*PeerSummary, 0)
	for _, u := range users {
		if u.Type != UserTypeFederation {
			continue
		}
		expiresAt := extractExpiry(u.Tokens)
		out = append(out, &PeerSummary{
			ID:        u.ID,
			Name:      u.Name,
			LoginName: u.LoginName,
			ExpiresAt: expiresAt,
			CreatedAt: u.CreatedAt,
			Expired:   isExpired(expiresAt),
		})
	}
	return out, nil
}

func (b *federationBiz) RotateToken(ctx context.Context, peerID string, ttlDays int, rotator web.User) (*PeerResult, error) {
	existing, err := b.ub.FindByID(ctx, peerID)
	if err != nil {
		return nil, err
	}
	if existing == nil || existing.Type != UserTypeFederation {
		return nil, errors.New("federation: peer not found")
	}
	token, err := generateFederationToken()
	if err != nil {
		return nil, err
	}
	expiresAt := computeExpiry(ttlDays)
	existing.Tokens = data.Options{
		data.Option{Name: federationTokenName, Value: token},
		data.Option{Name: federationExpiresAtMeta, Value: time.Time(expiresAt).Format(time.RFC3339)},
	}
	existing.UpdatedAt = now()
	existing.UpdatedBy = newOperator(rotator)
	if uerr := b.di.UserUpdate(ctx, existing); uerr != nil {
		return nil, uerr
	}
	if b.eb != nil {
		b.eb.CreateUser(EventActionUpdate, existing.ID, existing.LoginName, rotator)
	}
	return &PeerResult{
		ID:        existing.ID,
		Name:      existing.Name,
		LoginName: existing.LoginName,
		Token:     token,
		ExpiresAt: expiresAt,
		CreatedAt: existing.CreatedAt,
	}, nil
}

func (b *federationBiz) Revoke(ctx context.Context, peerID string, rotator web.User) error {
	existing, err := b.ub.FindByID(ctx, peerID)
	if err != nil {
		return err
	}
	if existing == nil || existing.Type != UserTypeFederation {
		return errors.New("federation: peer not found")
	}
	if derr := b.di.UserDelete(ctx, peerID); derr != nil {
		return derr
	}
	if b.eb != nil {
		b.eb.CreateUser(EventActionDelete, existing.ID, existing.LoginName, rotator)
	}
	return nil
}

func (b *federationBiz) RotateSelf(ctx context.Context, ttlDays int, caller web.User) (*PeerResult, error) {
	if caller == nil || caller.Anonymous() {
		return nil, errors.New("federation: RotateSelf requires an authenticated peer")
	}
	existing, err := b.ub.FindByID(ctx, caller.ID())
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, errors.New("federation: peer not found")
	}
	if existing.Type != UserTypeFederation {
		// Guard rails: only federation peers can self-rotate. Keeps
		// a compromised human token from triggering this path to
		// bypass regular password policy.
		return nil, errors.New("federation: RotateSelf is only available to federation peers")
	}
	token, err := generateFederationToken()
	if err != nil {
		return nil, err
	}
	expiresAt := computeExpiry(ttlDays)
	existing.Tokens = data.Options{
		data.Option{Name: federationTokenName, Value: token},
		data.Option{Name: federationExpiresAtMeta, Value: time.Time(expiresAt).Format(time.RFC3339)},
	}
	existing.UpdatedAt = now()
	existing.UpdatedBy = newOperator(caller)
	if uerr := b.di.UserUpdate(ctx, existing); uerr != nil {
		return nil, uerr
	}
	if b.eb != nil {
		b.eb.CreateUser(EventActionUpdate, existing.ID, existing.LoginName, caller)
	}
	return &PeerResult{
		ID:        existing.ID,
		Name:      existing.Name,
		LoginName: existing.LoginName,
		Token:     token,
		ExpiresAt: expiresAt,
		CreatedAt: existing.CreatedAt,
	}, nil
}

func (b *federationBiz) Capabilities(ctx context.Context) *Capabilities {
	mode := "swarm"
	if misc.IsStandalone() {
		mode = "standalone"
	}
	peerName := ""
	if cu := getContextUser(ctx); cu != nil {
		peerName = cu.Name()
	}
	return &Capabilities{
		APIVersion: federationAPIVersion,
		Mode:       mode,
		Version:    app.Version,
		Features:   []string{"proxy-v1"},
		Nodes:      1,
		PeerName:   peerName,
	}
}

// federationExpiresAtMeta is the `Tokens` entry that encodes the
// absolute expiry time of the active federation token. Kept as
// metadata alongside the token itself so a single DB read fetches
// both. The lookup `tokens.value=<token>` ignores it because the
// expiry entry's value is a timestamp, not a token hash.
const federationExpiresAtMeta = "federation-expires-at"

// generateFederationToken returns a 64-char hex string (32 bytes of
// crypto-random entropy). Using crypto/rand rather than the `misc`
// helpers because federation tokens replace passwords — a weaker PRNG
// would be a credential-rotation footgun.
func generateFederationToken() (string, error) {
	buf := make([]byte, federationTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("federation: random: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// computeExpiry returns a `dao.Time` N days in the future. TTL<=0
// means "no expiry" (MaxInt-ish, 100 years from now).
func computeExpiry(ttlDays int) dao.Time {
	if ttlDays <= 0 {
		return dao.Time(time.Now().AddDate(100, 0, 0))
	}
	return dao.Time(time.Now().AddDate(0, 0, ttlDays))
}

// extractExpiry locates the expiry-metadata entry and parses it.
// Returns a zero `dao.Time` when missing/malformed — the UI
// interprets that as "unknown".
func extractExpiry(tokens data.Options) dao.Time {
	for _, t := range tokens {
		if t.Name == federationExpiresAtMeta {
			if parsed, err := time.Parse(time.RFC3339, t.Value); err == nil {
				return dao.Time(parsed)
			}
		}
	}
	return dao.Time(time.Time{})
}

func isExpired(expiresAt dao.Time) bool {
	t := time.Time(expiresAt)
	if t.IsZero() {
		return false
	}
	return t.Before(time.Now())
}

// slugify normalises a peer name into a safe loginName suffix.
// Lowercases, replaces whitespace + non-alphanumeric with `-`, trims
// consecutive separators.
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var sb strings.Builder
	lastDash := false
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			sb.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && sb.Len() > 0 {
				sb.WriteRune('-')
				lastDash = true
			}
		}
	}
	out := strings.TrimRight(sb.String(), "-")
	if out == "" {
		return "peer"
	}
	return out
}

// getContextUser pulls the authenticated web.User out of a request
// context. Returns nil if the key is absent or the cast fails.
func getContextUser(ctx context.Context) web.User {
	v := ctx.Value(contextUserKey{})
	if v == nil {
		return nil
	}
	if u, ok := v.(web.User); ok {
		return u
	}
	return nil
}

// contextUserKey is the type-safe key the federation handler uses to
// propagate the authenticated user into the Capabilities call. The
// middleware (or HandlerFunc) injects it; biz layer reads it here.
type contextUserKey struct{}

// WithContextUser returns a derived context carrying the user. Kept
// in biz so it survives refactors of the security package without
// import-cycle issues.
func WithContextUser(ctx context.Context, u web.User) context.Context {
	return context.WithValue(ctx, contextUserKey{}, u)
}

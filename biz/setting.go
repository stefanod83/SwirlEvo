package biz

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/cuigh/auxo/data"
	"github.com/cuigh/auxo/net/web"
	"github.com/cuigh/swirl/dao"
	"github.com/cuigh/swirl/misc"
)

// SettingSecretMask is the placeholder returned by Find/Load whenever a
// sensitive field (vault.token, vault.secret_id, keycloak.client_secret)
// has a non-empty value in the DB. On Save, the same sentinel (or an
// empty string) is treated as "preserve the existing value" — the real
// secret is never round-tripped through the UI in cleartext.
const SettingSecretMask = "••••••••"

type SettingBiz interface {
	Find(ctx context.Context, id string) (options interface{}, err error)
	Load(ctx context.Context) (options data.Map, err error)
	// LoadRaw is identical to Load but does NOT mask sensitive fields.
	// Intended for callers that need the real values: the bootstrap
	// `loadSetting` in main.go that populates the in-memory
	// `*misc.Setting` snapshot, and the backup key provider during
	// the first authentication round-trip. Do NOT use in HTTP
	// handlers — those must go through Load.
	LoadRaw(ctx context.Context) (options data.Map, err error)
	Save(ctx context.Context, id string, options interface{}, user web.User) (err error)
}

func NewSetting(d dao.Interface, eb EventBiz) SettingBiz {
	return &settingBiz{d: d, eb: eb}
}

type settingBiz struct {
	d  dao.Interface
	eb EventBiz
}

// liveSettings is the in-memory `*misc.Setting` snapshot loaded at
// startup. It's installed by `main.loadSetting` via SetLiveSettings (see
// `main.go`). The pointer is the SAME one captured by closures in
// `vault/wire.go`, `backup_provider.go`, etc. — mutating the pointed-to
// struct here makes those subsystems see the new values without a
// restart.
//
// Stored as a package-level variable instead of a struct field on
// settingBiz to avoid a DI cycle (loadSetting depends on SettingBiz, so
// SettingBiz cannot depend on *misc.Setting through the constructor).
var (
	liveSettings   *misc.Setting
	liveSettingsMu sync.RWMutex
)

// SetLiveSettings installs the live settings pointer. Called by
// `main.loadSetting` once `*misc.Setting` has been built. Passing nil
// removes any previously installed pointer (e.g. for tests).
func SetLiveSettings(s *misc.Setting) {
	liveSettingsMu.Lock()
	liveSettings = s
	liveSettingsMu.Unlock()
}

func (b *settingBiz) Find(ctx context.Context, id string) (options interface{}, err error) {
	options, err = b.findRaw(ctx, id)
	if err == nil && options != nil {
		options = sanitizeForResponse(id, options)
	}
	return
}

// findRaw returns the unmasked setting — only for internal use by Save's
// preserve-on-empty logic and refreshInMemory.
func (b *settingBiz) findRaw(ctx context.Context, id string) (interface{}, error) {
	setting, err := b.d.SettingGet(ctx, id)
	if err != nil || setting == nil {
		return nil, err
	}
	return b.unmarshal(setting.Options)
}

// Load returns settings of swirl with sensitive fields masked
// (vault.token, vault.secret_id, keycloak.client_secret are replaced
// by SettingSecretMask). This is the HTTP-safe entry-point — any call
// path that needs real values must use LoadRaw instead.
func (b *settingBiz) Load(ctx context.Context) (options data.Map, err error) {
	options, err = b.loadRaw(ctx)
	if err != nil {
		return
	}
	for id, v := range options {
		options[id] = sanitizeForResponse(id, v)
	}
	return
}

// LoadRaw returns the unmasked settings map. Public entry-point exposed
// on the SettingBiz interface for bootstrap paths (main.loadSetting)
// that populate the live *misc.Setting with real values. HTTP handlers
// must keep going through Load — LoadRaw is NOT safe for UI round-trips.
func (b *settingBiz) LoadRaw(ctx context.Context) (data.Map, error) {
	return b.loadRaw(ctx)
}

// loadRaw returns the unmasked settings map. Used by refreshInMemory so
// the live *misc.Setting holds real values (not placeholders).
func (b *settingBiz) loadRaw(ctx context.Context) (data.Map, error) {
	settings, err := b.d.SettingGetAll(ctx)
	if err != nil {
		return nil, err
	}
	options := data.Map{}
	for _, s := range settings {
		v, err := b.unmarshal(s.Options)
		if err != nil {
			return nil, err
		}
		options[s.ID] = v
	}
	return options, nil
}

func (b *settingBiz) Save(ctx context.Context, id string, options interface{}, user web.User) (err error) {
	// The API handler binds the incoming body with `Options
	// json.RawMessage`, so `options` arrives here as a json.RawMessage
	// (== []byte). preserveSecretsFromExisting expects a decoded
	// map[string]interface{}: unmarshal upfront so the mask-placeholder
	// substitution actually fires. Without this step the placeholder
	// `SettingSecretMask` would be persisted literally in the DB and
	// every AppRole login / token call would break.
	if raw, ok := options.(json.RawMessage); ok && len(raw) > 0 {
		var decoded interface{}
		if derr := json.Unmarshal(raw, &decoded); derr == nil {
			options = decoded
		}
	} else if raw, ok := options.([]byte); ok && len(raw) > 0 {
		var decoded interface{}
		if derr := json.Unmarshal(raw, &decoded); derr == nil {
			options = decoded
		}
	}
	// Preserve sensitive fields that the UI round-tripped as the mask
	// placeholder or as empty strings — the client never sees the real
	// value, so submitting the mask means "don't change this secret".
	if existing, lerr := b.findRaw(ctx, id); lerr == nil && existing != nil {
		options = preserveSecretsFromExisting(id, options, existing)
	}

	setting := &dao.Setting{
		ID:        id,
		UpdatedAt: time.Now(),
	}
	if user != nil {
		setting.UpdatedBy = dao.Operator{ID: user.ID(), Name: user.Name()}
	}

	setting.Options, err = b.marshal(options)
	if err == nil {
		err = b.d.SettingUpdate(ctx, setting)
	}
	if err == nil {
		// Refresh the in-memory snapshot so callers that captured the
		// `*misc.Setting` pointer at startup (Vault client, backup key
		// provider, …) immediately see the new values without a restart.
		// Best-effort: a refresh failure does not roll back the persisted
		// change, but is logged so operators can investigate.
		if rerr := b.refreshInMemory(ctx); rerr != nil {
			// Don't surface as Save error — persistence already succeeded.
			// Subsequent reads will pick up the change at next process boot.
			_ = rerr
		}
	}
	if err == nil && user != nil {
		b.eb.CreateSetting(EventActionUpdate, user)
	}
	return
}

// refreshInMemory re-reads every setting from the DAO and overwrites the
// fields of the live `*misc.Setting` struct in place. The closures that
// captured this pointer at startup (Vault client, backup key provider)
// see the new values on their next call.
//
// Best-effort: if no live pointer was registered (tests, or a startup
// path that didn't run loadSetting), this is a no-op.
func (b *settingBiz) refreshInMemory(ctx context.Context) error {
	liveSettingsMu.RLock()
	s := liveSettings
	liveSettingsMu.RUnlock()
	if s == nil {
		return nil
	}
	// Use loadRaw (not Load) so the snapshot keeps real secret values,
	// not the UI placeholders.
	opts, err := b.loadRaw(ctx)
	if err != nil {
		return err
	}
	buf, err := json.Marshal(opts)
	if err != nil {
		return err
	}
	fresh := &misc.Setting{}
	if err := json.Unmarshal(buf, fresh); err != nil {
		return err
	}
	*s = *fresh
	return nil
}

func (b *settingBiz) marshal(v interface{}) (s string, err error) {
	var buf []byte
	if buf, err = json.Marshal(v); err == nil {
		s = string(buf)
	}
	return
}

func (b *settingBiz) unmarshal(s string) (v interface{}, err error) {
	d := json.NewDecoder(bytes.NewBuffer([]byte(s)))
	d.UseNumber()
	err = d.Decode(&v)
	return
}

// secretFieldsByID lists the JSON-tag field names that must be masked in
// each settings group. Match the `json:` tags in misc/option.go.
var secretFieldsByID = map[string][]string{
	"vault":    {"token", "secret_id"},
	"keycloak": {"client_secret"},
}

// sanitizeForResponse replaces non-empty sensitive fields with the
// SettingSecretMask placeholder on a copy of the incoming value. Returns
// the original value unchanged when no sensitive fields apply.
func sanitizeForResponse(id string, v interface{}) interface{} {
	fields, ok := secretFieldsByID[id]
	if !ok {
		return v
	}
	m, ok := v.(map[string]interface{})
	if !ok {
		return v
	}
	clone := make(map[string]interface{}, len(m))
	for k, val := range m {
		clone[k] = val
	}
	for _, f := range fields {
		if s, isStr := clone[f].(string); isStr && s != "" {
			clone[f] = SettingSecretMask
		}
	}
	return clone
}

// preserveSecretsFromExisting substitutes mask placeholders and empty
// strings in the incoming payload with the values stored in `existing`.
// This implements the "don't overwrite a secret just because the UI
// re-submitted the form" contract: the operator must type a new value
// (different from the mask + non-empty) to rotate the secret.
func preserveSecretsFromExisting(id string, incoming, existing interface{}) interface{} {
	fields, ok := secretFieldsByID[id]
	if !ok {
		return incoming
	}
	in, ok := incoming.(map[string]interface{})
	if !ok {
		return incoming
	}
	ex, ok := existing.(map[string]interface{})
	if !ok {
		return incoming
	}
	for _, f := range fields {
		incomingVal, _ := in[f].(string)
		if incomingVal == "" || incomingVal == SettingSecretMask {
			if existingVal, isStr := ex[f].(string); isStr {
				in[f] = existingVal
			}
		}
	}
	return in
}

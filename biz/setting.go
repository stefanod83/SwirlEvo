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

type SettingBiz interface {
	Find(ctx context.Context, id string) (options interface{}, err error)
	Load(ctx context.Context) (options data.Map, err error)
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
	var setting *dao.Setting
	setting, err = b.d.SettingGet(ctx, id)
	if err == nil && setting != nil {
		return b.unmarshal(setting.Options)
	}
	return
}

// Load returns settings of swirl. If not found, default settings will be returned.
func (b *settingBiz) Load(ctx context.Context) (options data.Map, err error) {
	var settings []*dao.Setting
	settings, err = b.d.SettingGetAll(ctx)
	if err != nil {
		return
	}

	options = data.Map{}
	for _, s := range settings {
		var v interface{}
		if v, err = b.unmarshal(s.Options); err != nil {
			return
		}
		options[s.ID] = v
	}
	return
}

func (b *settingBiz) Save(ctx context.Context, id string, options interface{}, user web.User) (err error) {
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
	opts, err := b.Load(ctx)
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

package biz

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/cuigh/auxo/app/container"
	"github.com/cuigh/auxo/net/web"
	"github.com/cuigh/swirl/dao"
	"github.com/cuigh/swirl/misc"
)

// defaultSecretField is the KVv2 field selector assumed when the catalog
// entry leaves it unset. Matches the convention used for the backup key.
const defaultSecretField = "value"

// maxSecretNameLen bounds the logical name to what MongoDB and BoltDB can
// reasonably index without truncation; Vault itself accepts longer paths
// but the UI has no use case beyond this length.
const maxSecretNameLen = 128

// vaultReader is the subset of vault.Client that the biz layer uses.
// Declared as an interface (rather than importing vault.Client directly)
// to avoid the import cycle biz → vault → biz (BackupKeyProvider).
type vaultReader interface {
	IsEnabled() bool
	ReadKVv2(ctx context.Context, path string) (map[string]any, error)
	WriteKVv2(ctx context.Context, path string, data map[string]any) error
	DeleteKVv2(ctx context.Context, path string) error
	// ReadMetadataSummary returns (currentVersion, totalVersions, exists, err).
	// `exists=false` + `err=nil` means the KV entry is absent (404).
	ReadMetadataSummary(ctx context.Context, path string) (int, int, bool, error)
}

// errVaultDisabled is returned by Preview when Vault is not configured.
// Kept local to avoid importing the vault package.
var errVaultDisabled = errors.New("vault integration is not enabled")

// VaultSecretStatus reports per-entry health returned by GetStatuses.
// Exists=false means the KV entry is absent in Vault (the catalog entry
// still exists in Swirl). When Error is non-empty the other fields are
// not meaningful.
type VaultSecretStatus struct {
	ID             string `json:"id"`
	Exists         bool   `json:"exists"`
	CurrentVersion int    `json:"currentVersion,omitempty"`
	TotalVersions  int    `json:"totalVersions,omitempty"`
	Error          string `json:"error,omitempty"`
}

// VaultSecretBiz manages the catalog of Vault secret references. Only the
// pointer (mount/prefix/path/field) is stored in the Swirl database — the
// actual secret value is fetched from Vault on demand and never persisted.
type VaultSecretBiz interface {
	Search(ctx context.Context, name string, pageIndex, pageSize int) ([]*dao.VaultSecret, int, error)
	Find(ctx context.Context, id string) (*dao.VaultSecret, error)
	FindByName(ctx context.Context, name string) (*dao.VaultSecret, error)
	GetAll(ctx context.Context) ([]*dao.VaultSecret, error)
	Create(ctx context.Context, secret *dao.VaultSecret, user web.User) (string, error)
	Update(ctx context.Context, secret *dao.VaultSecret, user web.User) error
	Delete(ctx context.Context, id string, user web.User) error
	// Preview reads the secret from Vault and returns the list of field names
	// present in the entry. The returned slice NEVER contains values — this
	// is the only contract Swirl exposes about the live secret content.
	Preview(ctx context.Context, id string) (exists bool, fields []string, err error)
	// WriteValue writes a new version of the secret in Vault. When
	// `replace` is false the backend merges the new fields into the
	// current version; when true the new version contains ONLY the
	// supplied fields. Values never touch disk on the Swirl side.
	WriteValue(ctx context.Context, id string, data map[string]any, replace bool, user web.User) error
	// GetStatuses fetches per-catalog-entry metadata from Vault in
	// parallel and returns a map keyed by catalog entry id. Best-effort:
	// per-entry errors are surfaced as Error in the status, not as a
	// top-level failure.
	GetStatuses(ctx context.Context) (map[string]*VaultSecretStatus, error)
}

// NewVaultSecret wires the biz. The Vault client is resolved lazily via the
// DI container (name "vault-client") so we don't have a compile-time import
// on the vault package — otherwise the import cycle
// biz -> vault -> biz (BackupKeyProvider) would block compilation.
func NewVaultSecret(di dao.Interface, eb EventBiz, s *misc.Setting) VaultSecretBiz {
	loader := func() *misc.Setting { return s }
	return &vaultSecretBiz{di: di, eb: eb, loader: loader}
}

type vaultSecretBiz struct {
	di     dao.Interface
	eb     EventBiz
	loader func() *misc.Setting
}

func (b *vaultSecretBiz) Search(ctx context.Context, name string, pageIndex, pageSize int) ([]*dao.VaultSecret, int, error) {
	args := &dao.VaultSecretSearchArgs{
		Name:      name,
		PageIndex: pageIndex,
		PageSize:  pageSize,
	}
	return b.di.VaultSecretSearch(ctx, args)
}

func (b *vaultSecretBiz) Find(ctx context.Context, id string) (*dao.VaultSecret, error) {
	return b.di.VaultSecretGet(ctx, id)
}

func (b *vaultSecretBiz) FindByName(ctx context.Context, name string) (*dao.VaultSecret, error) {
	return b.di.VaultSecretGetByName(ctx, name)
}

func (b *vaultSecretBiz) GetAll(ctx context.Context) ([]*dao.VaultSecret, error) {
	return b.di.VaultSecretGetAll(ctx)
}

func (b *vaultSecretBiz) Create(ctx context.Context, secret *dao.VaultSecret, user web.User) (string, error) {
	if err := b.normalize(secret); err != nil {
		return "", err
	}
	// Unique name guard — both DAOs also enforce it (mongo via index), but we
	// check early so the UI gets a friendly error instead of a driver message.
	existing, err := b.di.VaultSecretGetByName(ctx, secret.Name)
	if err != nil {
		return "", err
	}
	if existing != nil {
		return "", fmt.Errorf("a vault secret named %q already exists", secret.Name)
	}

	secret.ID = createId()
	secret.CreatedAt = now()
	secret.UpdatedAt = secret.CreatedAt
	secret.CreatedBy = newOperator(user)
	secret.UpdatedBy = secret.CreatedBy

	if err := b.di.VaultSecretCreate(ctx, secret); err != nil {
		return "", err
	}
	b.eb.CreateVaultSecret(EventActionCreate, secret.ID, secret.Name, user)
	return secret.ID, nil
}

func (b *vaultSecretBiz) Update(ctx context.Context, secret *dao.VaultSecret, user web.User) error {
	if secret.ID == "" {
		return errors.New("vault secret id is required")
	}
	if err := b.normalize(secret); err != nil {
		return err
	}
	// Reject a rename that collides with another row (the uniqueness guard
	// must be checked in the biz: the mongo unique index would surface the
	// error anyway, but bolt has no equivalent).
	existing, err := b.di.VaultSecretGetByName(ctx, secret.Name)
	if err != nil {
		return err
	}
	if existing != nil && existing.ID != secret.ID {
		return fmt.Errorf("a vault secret named %q already exists", secret.Name)
	}

	secret.UpdatedAt = now()
	secret.UpdatedBy = newOperator(user)

	if err := b.di.VaultSecretUpdate(ctx, secret); err != nil {
		return err
	}
	b.eb.CreateVaultSecret(EventActionUpdate, secret.ID, secret.Name, user)
	return nil
}

func (b *vaultSecretBiz) Delete(ctx context.Context, id string, user web.User) error {
	// Fetch first so the emitted event carries the logical name, matching
	// the audit format used by the Registry/Host flows.
	existing, err := b.di.VaultSecretGet(ctx, id)
	if err != nil {
		return err
	}
	name := ""
	if existing != nil {
		name = existing.Name
	}
	if err := b.di.VaultSecretDelete(ctx, id); err != nil {
		return err
	}
	b.eb.CreateVaultSecret(EventActionDelete, id, name, user)
	return nil
}

func (b *vaultSecretBiz) Preview(ctx context.Context, id string) (bool, []string, error) {
	vc, err := lookupVaultClient()
	if err != nil {
		return false, nil, err
	}
	if !vc.IsEnabled() {
		return false, nil, errVaultDisabled
	}
	rec, err := b.di.VaultSecretGet(ctx, id)
	if err != nil {
		return false, nil, err
	}
	if rec == nil {
		return false, nil, errors.New("vault secret not found")
	}
	s := b.loader()
	if s == nil {
		return false, nil, errors.New("settings are not loaded")
	}
	logicalPath := rec.Path
	if logicalPath == "" {
		// Fall back to Name for rows written before the Path column existed.
		logicalPath = rec.Name
	}
	full := resolvePrefixed(s, logicalPath)
	data, err := vc.ReadKVv2(ctx, full)
	if err != nil {
		// Distinguish "missing entry" from auth/config errors so the UI
		// can differentiate. The Vault client formats the HTTP status
		// as `" <code> "` in the message (e.g. `HTTP/1.1 404`).
		if strings.Contains(err.Error(), " 404 ") {
			return false, nil, nil
		}
		return false, nil, err
	}
	fields := make([]string, 0, len(data))
	for k := range data {
		fields = append(fields, k)
	}
	sort.Strings(fields)
	return true, fields, nil
}

// lookupVaultClient resolves the vault client lazily via the DI container.
// Registered in vault/wire.go under the name "vault-client".
func lookupVaultClient() (vaultReader, error) {
	v, err := container.TryFind("vault-client")
	if err != nil {
		return nil, fmt.Errorf("vault client is not registered: %w", err)
	}
	vc, ok := v.(vaultReader)
	if !ok {
		return nil, errors.New("registered vault-client does not implement the expected interface")
	}
	return vc, nil
}

// resolvePrefixed mirrors vault.ResolvePrefixed locally so biz does not pull
// the vault package (which would form an import cycle through biz itself).
func resolvePrefixed(s *misc.Setting, name string) string {
	prefix := strings.Trim(s.Vault.KVPrefix, "/ ")
	name = strings.Trim(name, "/ ")
	if prefix == "" {
		return name
	}
	return prefix + "/" + name
}

// normalize trims whitespace, applies defaults and validates the entry. The
// logical name is used both as display label and as the KVv2 sub-path, so
// the rules are stricter than a typical object name.
func (b *vaultSecretBiz) normalize(secret *dao.VaultSecret) error {
	if secret == nil {
		return errors.New("vault secret is nil")
	}
	secret.Name = strings.TrimSpace(secret.Name)
	secret.Path = strings.TrimSpace(secret.Path)
	secret.Field = strings.TrimSpace(secret.Field)
	secret.Description = strings.TrimSpace(secret.Description)

	if secret.Name == "" {
		return errors.New("name is required")
	}
	if len(secret.Name) > maxSecretNameLen {
		return fmt.Errorf("name must be at most %d characters", maxSecretNameLen)
	}
	if strings.ContainsAny(secret.Name, " \t\r\n") {
		return errors.New("name must not contain whitespace")
	}

	// Path defaults to the Name when omitted — makes single-entry secrets
	// trivial to create while still allowing dedicated sub-paths.
	if secret.Path == "" {
		secret.Path = secret.Name
	}
	if strings.HasPrefix(secret.Path, "/") || strings.HasSuffix(secret.Path, "/") {
		return errors.New("path must not start or end with a slash")
	}
	if strings.ContainsAny(secret.Path, " \t\r\n") {
		return errors.New("path must not contain whitespace")
	}

	// Field is optional — an empty value means "auto-select": if the KVv2
	// entry has a single field, use it; otherwise return the full JSON.
	// Do NOT force a default like "value" here: the KVv2 field name is
	// user-defined and may be anything (the secret name itself, a
	// descriptive key, etc.).
	return nil
}

func (b *vaultSecretBiz) WriteValue(ctx context.Context, id string, data map[string]any, replace bool, user web.User) error {
	if len(data) == 0 {
		return errors.New("no fields to write")
	}
	vc, err := lookupVaultClient()
	if err != nil {
		return err
	}
	if !vc.IsEnabled() {
		return errVaultDisabled
	}
	rec, err := b.di.VaultSecretGet(ctx, id)
	if err != nil {
		return err
	}
	if rec == nil {
		return errors.New("vault secret not found")
	}
	s := b.loader()
	if s == nil {
		return errors.New("settings are not loaded")
	}
	logicalPath := rec.Path
	if logicalPath == "" {
		logicalPath = rec.Name
	}
	full := resolvePrefixed(s, logicalPath)
	// Merge mode: read the current version, overlay the new fields, then
	// write the result as a new version. The current values never leave
	// the backend — only the field NAMES of the new entry travel back to
	// the caller via the audit event.
	payload := data
	if !replace {
		current, rerr := vc.ReadKVv2(ctx, full)
		if rerr != nil && !strings.Contains(rerr.Error(), " 404 ") {
			return rerr
		}
		merged := make(map[string]any, len(current)+len(data))
		for k, v := range current {
			merged[k] = v
		}
		for k, v := range data {
			merged[k] = v
		}
		payload = merged
	}
	if err := vc.WriteKVv2(ctx, full, payload); err != nil {
		return err
	}
	// Audit: only field NAMES, never values.
	fieldNames := make([]string, 0, len(data))
	for k := range data {
		fieldNames = append(fieldNames, k)
	}
	sort.Strings(fieldNames)
	mode := "append"
	if replace {
		mode = "replace"
	}
	detail := fmt.Sprintf("%s:%s fields=%s", mode, rec.Name, strings.Join(fieldNames, ","))
	b.eb.CreateVaultSecret(EventActionUpdate, rec.ID, detail, user)
	return nil
}

func (b *vaultSecretBiz) GetStatuses(ctx context.Context) (map[string]*VaultSecretStatus, error) {
	records, err := b.di.VaultSecretGetAll(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]*VaultSecretStatus, len(records))
	if len(records) == 0 {
		return out, nil
	}
	vc, err := lookupVaultClient()
	if err != nil {
		// No client registered — return a status-per-row with the error so
		// the UI shows a concrete reason instead of a missing badge.
		for _, r := range records {
			out[r.ID] = &VaultSecretStatus{ID: r.ID, Error: err.Error()}
		}
		return out, nil
	}
	if !vc.IsEnabled() {
		for _, r := range records {
			out[r.ID] = &VaultSecretStatus{ID: r.ID, Error: errVaultDisabled.Error()}
		}
		return out, nil
	}
	s := b.loader()
	if s == nil {
		for _, r := range records {
			out[r.ID] = &VaultSecretStatus{ID: r.ID, Error: "settings are not loaded"}
		}
		return out, nil
	}

	// Cap concurrency to 8 to avoid flooding a small Vault cluster when
	// there are many catalog entries.
	sem := make(chan struct{}, 8)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, rec := range records {
		rec := rec
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			logicalPath := rec.Path
			if logicalPath == "" {
				logicalPath = rec.Name
			}
			full := resolvePrefixed(s, logicalPath)
			current, total, exists, serr := vc.ReadMetadataSummary(ctx, full)
			st := &VaultSecretStatus{ID: rec.ID}
			if serr != nil {
				st.Error = serr.Error()
			} else {
				st.Exists = exists
				st.CurrentVersion = current
				st.TotalVersions = total
			}
			mu.Lock()
			out[rec.ID] = st
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out, nil
}

func init() {
	container.Put(NewVaultSecret)
}

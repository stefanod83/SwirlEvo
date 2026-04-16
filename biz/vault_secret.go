package biz

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

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

// vaultReader is the subset of vault.Client that Preview uses. We accept
// an interface instead of the concrete type so the biz layer doesn't have
// to import the vault package — which would create an import cycle
// (vault already depends on biz for BackupKeyProvider).
type vaultReader interface {
	IsEnabled() bool
	ReadKVv2(ctx context.Context, path string) (map[string]any, error)
}

// errVaultDisabled is returned by Preview when Vault is not configured.
// Kept local to avoid importing the vault package.
var errVaultDisabled = errors.New("vault integration is not enabled")

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
		// Distinguish "missing entry" from auth/config errors so the UI can
		// differentiate. The Vault client wraps the HTTP status into the
		// error string; 404 contains "http 404".
		msg := err.Error()
		if strings.Contains(msg, "http 404") {
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

	// Field is optional in storage; the biz layer treats an empty value as
	// "return the whole KV object" at resolve time. We still pin a default
	// for display so the UI can show something consistent.
	if secret.Field == "" {
		secret.Field = defaultSecretField
	}
	return nil
}

func init() {
	container.Put(NewVaultSecret)
}

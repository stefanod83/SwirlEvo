package vault

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cuigh/swirl/biz"
	"github.com/cuigh/swirl/misc"
)

// backupKeyProvider is the concrete biz.BackupKeyProvider that reads the
// SWIRL_BACKUP_KEY passphrase from Vault's KVv2 engine. Installed via
// NewBackupKeyProvider and wired in main.
type backupKeyProvider struct {
	client   *Client
	settings func() *misc.Setting
}

// NewBackupKeyProvider returns a biz.BackupKeyProvider that resolves the
// backup passphrase through the given Vault client. The provider re-reads
// Settings on every Lookup so that path/field changes in the admin UI take
// effect without a Swirl restart.
func NewBackupKeyProvider(c *Client, settings func() *misc.Setting) biz.BackupKeyProvider {
	return &backupKeyProvider{client: c, settings: settings}
}

// Lookup implements biz.BackupKeyProvider. Returns an error when Vault is
// disabled or the configured path/field resolves to an empty string.
func (p *backupKeyProvider) Lookup(ctx context.Context) (string, string, error) {
	s := p.settings()
	if s == nil {
		return "", "vault", errors.New("vault provider: settings are nil")
	}
	if !p.client.IsEnabled() {
		return "", "vault", ErrDisabled
	}
	name := strings.TrimSpace(s.Vault.BackupKeyPath)
	if name == "" {
		name = "backup-key"
	}
	field := strings.TrimSpace(s.Vault.BackupKeyField)
	if field == "" {
		field = "value"
	}
	path := ResolvePrefixed(s, name)
	data, err := p.client.ReadKVv2(ctx, path)
	if err != nil {
		return "", "vault", fmt.Errorf("read %s: %w", path, err)
	}
	raw, ok := data[field]
	if !ok {
		return "", "vault", fmt.Errorf("field %q not found at %s", field, path)
	}
	str, ok := raw.(string)
	if !ok {
		return "", "vault", fmt.Errorf("field %q at %s is not a string", field, path)
	}
	if str == "" {
		return "", "vault", fmt.Errorf("field %q at %s is empty", field, path)
	}
	return str, "vault", nil
}

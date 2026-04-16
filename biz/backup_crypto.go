package biz

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"golang.org/x/crypto/scrypt"
)

const (
	backupKeyEnv     = "SWIRL_BACKUP_KEY"
	backupKeyMinLen  = 16
	backupSaltAtRest = "swirl-backup-at-rest"
	scryptN          = 1 << 15 // 32768
	scryptR          = 8
	scryptP          = 1
	aesKeyLen        = 32
	nonceLen         = 12
	portableSaltLen  = 16
)

var (
	magicAtRest   = []byte{'S', 'W', 'B', 'R'}
	magicPortable = []byte{'S', 'W', 'B', 'P'}

	errMissingKey    = errors.New("SWIRL_BACKUP_KEY is not configured")
	errInvalidFormat = errors.New("backup archive format is not recognized")
	errDecrypt       = errors.New("backup decryption failed — wrong key or corrupted data")
)

// BackupKeyProvider lets an external subsystem (e.g. Vault) supply the
// backup passphrase when the SWIRL_BACKUP_KEY env var is unset. The
// provider is consulted only when env is empty; env always wins.
//
// Lookup returns the passphrase as a string and a logical source tag for
// logs/diagnostics ("vault", "env", ...). Errors are surfaced verbatim so
// operators can see why the fallback failed.
type BackupKeyProvider interface {
	Lookup(ctx context.Context) (passphrase string, source string, err error)
}

var (
	backupKeyProvider   BackupKeyProvider
	backupKeyProviderMu sync.RWMutex

	backupKeyCache        string
	backupKeyCacheExpires time.Time
	backupKeyCacheMu      sync.RWMutex
)

// SetBackupKeyProvider wires a fallback provider (typically the Vault
// client) to be used when SWIRL_BACKUP_KEY is absent from the environment.
// Passing nil removes any previously installed provider.
func SetBackupKeyProvider(p BackupKeyProvider) {
	backupKeyProviderMu.Lock()
	backupKeyProvider = p
	backupKeyProviderMu.Unlock()
	// Invalidate any cached passphrase so the new provider is consulted
	// on the next operation.
	backupKeyCacheMu.Lock()
	backupKeyCache = ""
	backupKeyCacheExpires = time.Time{}
	backupKeyCacheMu.Unlock()
}

// backupKeyConfigured reports whether a master key is currently available,
// either via environment or via a cached/fetchable provider response.
// A negative result here is the same "skip scheduled backups" signal used
// historically — but now it also returns true when a Vault provider can
// supply the key, without requiring an operator env var.
func backupKeyConfigured() bool {
	if len(os.Getenv(backupKeyEnv)) >= backupKeyMinLen {
		return true
	}
	// Check cache first — cheap, avoids hammering Vault on every scheduler tick.
	backupKeyCacheMu.RLock()
	if backupKeyCache != "" && (backupKeyCacheExpires.IsZero() || time.Now().Before(backupKeyCacheExpires)) {
		backupKeyCacheMu.RUnlock()
		return len(backupKeyCache) >= backupKeyMinLen
	}
	backupKeyCacheMu.RUnlock()
	// Attempt a provider fetch (best-effort). If this succeeds we also
	// populate the cache for the actual encrypt/decrypt calls.
	pw, err := fetchFromProvider()
	if err != nil {
		return false
	}
	return len(pw) >= backupKeyMinLen
}

// deriveKey runs scrypt over a passphrase with the given salt.
func deriveKey(passphrase, salt []byte) ([]byte, error) {
	return scrypt.Key(passphrase, salt, scryptN, scryptR, scryptP, aesKeyLen)
}

// masterKey returns the derived AES key used for at-rest backup encryption.
// Source order: (1) SWIRL_BACKUP_KEY env, (2) in-memory cache, (3) Vault
// provider. Once fetched from Vault, the plaintext passphrase is cached
// for a short TTL so the scheduler can keep running even if Vault blips.
func masterKey() ([]byte, error) {
	if pw := os.Getenv(backupKeyEnv); len(pw) >= backupKeyMinLen {
		return deriveKey([]byte(pw), []byte(backupSaltAtRest))
	}
	// Try cache.
	backupKeyCacheMu.RLock()
	cached := backupKeyCache
	fresh := !backupKeyCacheExpires.IsZero() && time.Now().Before(backupKeyCacheExpires)
	backupKeyCacheMu.RUnlock()
	if cached != "" && fresh {
		return deriveKey([]byte(cached), []byte(backupSaltAtRest))
	}
	// Fetch from provider.
	pw, err := fetchFromProvider()
	if err != nil {
		return nil, err
	}
	if len(pw) < backupKeyMinLen {
		return nil, fmt.Errorf("%w: provider returned a passphrase shorter than %d bytes", errMissingKey, backupKeyMinLen)
	}
	return deriveKey([]byte(pw), []byte(backupSaltAtRest))
}

// fetchFromProvider runs the installed provider (if any), stores the
// result in cache, and returns the passphrase.
func fetchFromProvider() (string, error) {
	backupKeyProviderMu.RLock()
	p := backupKeyProvider
	backupKeyProviderMu.RUnlock()
	if p == nil {
		return "", errMissingKey
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pw, _, err := p.Lookup(ctx)
	if err != nil {
		return "", fmt.Errorf("backup key provider: %w", err)
	}
	// Cache for 5 minutes; short enough to pick up rotation reasonably fast,
	// long enough to keep the scheduler working across transient Vault outages.
	backupKeyCacheMu.Lock()
	backupKeyCache = pw
	backupKeyCacheExpires = time.Now().Add(5 * time.Minute)
	backupKeyCacheMu.Unlock()
	return pw, nil
}

func aesGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// encryptAtRest encrypts plaintext with the master key and returns the full archive bytes
// (magic + nonce + ciphertext+tag).
func encryptAtRest(plaintext []byte) ([]byte, error) {
	key, err := masterKey()
	if err != nil {
		return nil, err
	}
	aead, err := aesGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, nonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	ct := aead.Seal(nil, nonce, plaintext, nil)

	out := make([]byte, 0, len(magicAtRest)+nonceLen+len(ct))
	out = append(out, magicAtRest...)
	out = append(out, nonce...)
	out = append(out, ct...)
	return out, nil
}

// decryptAtRest reverses encryptAtRest.
func decryptAtRest(archive []byte) ([]byte, error) {
	if len(archive) < len(magicAtRest)+nonceLen || !bytes.Equal(archive[:len(magicAtRest)], magicAtRest) {
		return nil, errInvalidFormat
	}
	key, err := masterKey()
	if err != nil {
		return nil, err
	}
	aead, err := aesGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := archive[len(magicAtRest) : len(magicAtRest)+nonceLen]
	ct := archive[len(magicAtRest)+nonceLen:]
	pt, err := aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, errDecrypt
	}
	return pt, nil
}

// encryptPortable encrypts plaintext with a passphrase-derived key.
// The returned bytes contain magic + random salt + nonce + ciphertext+tag,
// so the same payload can be decrypted on another instance armed only with the password.
func encryptPortable(plaintext []byte, passphrase string) ([]byte, error) {
	if passphrase == "" {
		return nil, errors.New("passphrase is required for portable export")
	}
	salt := make([]byte, portableSaltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, err
	}
	key, err := deriveKey([]byte(passphrase), salt)
	if err != nil {
		return nil, err
	}
	aead, err := aesGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, nonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	ct := aead.Seal(nil, nonce, plaintext, nil)

	out := make([]byte, 0, len(magicPortable)+portableSaltLen+nonceLen+len(ct))
	out = append(out, magicPortable...)
	out = append(out, salt...)
	out = append(out, nonce...)
	out = append(out, ct...)
	return out, nil
}

// decryptPortable reverses encryptPortable.
func decryptPortable(archive []byte, passphrase string) ([]byte, error) {
	head := len(magicPortable)
	if len(archive) < head+portableSaltLen+nonceLen || !bytes.Equal(archive[:head], magicPortable) {
		return nil, errInvalidFormat
	}
	if passphrase == "" {
		return nil, errors.New("passphrase is required for portable archive")
	}
	salt := archive[head : head+portableSaltLen]
	nonce := archive[head+portableSaltLen : head+portableSaltLen+nonceLen]
	ct := archive[head+portableSaltLen+nonceLen:]

	key, err := deriveKey([]byte(passphrase), salt)
	if err != nil {
		return nil, err
	}
	aead, err := aesGCM(key)
	if err != nil {
		return nil, err
	}
	pt, err := aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, errDecrypt
	}
	return pt, nil
}

// decryptAny auto-detects at-rest vs portable by inspecting the leading magic bytes,
// and returns the plaintext.
func decryptAny(archive []byte, passphrase string) ([]byte, error) {
	switch {
	case len(archive) >= len(magicAtRest) && bytes.Equal(archive[:len(magicAtRest)], magicAtRest):
		return decryptAtRest(archive)
	case len(archive) >= len(magicPortable) && bytes.Equal(archive[:len(magicPortable)], magicPortable):
		return decryptPortable(archive, passphrase)
	default:
		return nil, errInvalidFormat
	}
}

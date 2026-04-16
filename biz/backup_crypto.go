package biz

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
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
	ok, _, _ := backupKeyStatus()
	return ok
}

// backupKeyStatus is the diagnostic-rich version of backupKeyConfigured:
// it returns the source ("env" / "cache" / "vault" / "") and the actual
// error if a provider lookup failed, so the API can surface the real
// reason instead of an opaque false. Order of resolution mirrors
// masterKey() so the two stay consistent.
func backupKeyStatus() (configured bool, source string, err error) {
	if len(os.Getenv(backupKeyEnv)) >= backupKeyMinLen {
		return true, "env", nil
	}
	// Check cache first — cheap, avoids hammering Vault on every scheduler tick.
	backupKeyCacheMu.RLock()
	if backupKeyCache != "" && (backupKeyCacheExpires.IsZero() || time.Now().Before(backupKeyCacheExpires)) {
		ok := len(backupKeyCache) >= backupKeyMinLen
		backupKeyCacheMu.RUnlock()
		return ok, "cache", nil
	}
	backupKeyCacheMu.RUnlock()
	// Attempt a provider fetch (best-effort). If this succeeds we also
	// populate the cache for the actual encrypt/decrypt calls.
	pw, perr := fetchFromProvider()
	if perr != nil {
		return false, "", perr
	}
	if len(pw) >= backupKeyMinLen {
		return true, "vault", nil
	}
	return false, "vault", fmt.Errorf("provider returned a passphrase shorter than %d bytes", backupKeyMinLen)
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

// decryptAtRest reverses encryptAtRest using the current master key.
func decryptAtRest(archive []byte) ([]byte, error) {
	key, err := masterKey()
	if err != nil {
		return nil, err
	}
	return openAtRest(archive, key)
}

// decryptAtRestWithKey reverses encryptAtRest using an explicit AES key
// instead of the current master key. Used by recovery flows that need to
// decrypt with the OLD key (derived from a passphrase the operator
// supplies) before re-encrypting with the new master key.
func decryptAtRestWithKey(archive, key []byte) ([]byte, error) {
	return openAtRest(archive, key)
}

// openAtRest holds the shared cipher logic for both decrypt entry-points.
// Validates magic + length, then runs AES-GCM Open. On AEAD failure returns
// errDecrypt so callers can distinguish "wrong key / corrupted" from
// "malformed archive".
func openAtRest(archive, key []byte) ([]byte, error) {
	if len(archive) < len(magicAtRest)+nonceLen || !bytes.Equal(archive[:len(magicAtRest)], magicAtRest) {
		return nil, errInvalidFormat
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

// keyFingerprint returns a stable, key-derived 16-byte tag (hex-encoded)
// suitable for storing alongside an archive. The label includes a version
// suffix so the scheme can be rotated without false-negatives on existing
// fingerprints. HMAC over a fixed label gives indistinguishability without
// leaking the key.
func keyFingerprint(key []byte) string {
	h := hmac.New(sha256.New, key)
	_, _ = h.Write([]byte("swirl-backup-key-fp/v1"))
	sum := h.Sum(nil)
	return hex.EncodeToString(sum[:16])
}

// currentKeyFingerprint computes the fingerprint of the current master
// key. Returns "" (no error) when no master key is configured — callers
// use the empty string to mean "key unknown / unconfigured".
func currentKeyFingerprint() string {
	key, err := masterKey()
	if err != nil {
		return ""
	}
	return keyFingerprint(key)
}

// deriveKeyFromPassphrase wraps deriveKey with the fixed at-rest salt so
// callers (e.g. the Recover handler) can hand in a raw passphrase without
// having to know about salt management.
func deriveKeyFromPassphrase(passphrase string) ([]byte, error) {
	if len(passphrase) == 0 {
		return nil, errors.New("passphrase is required")
	}
	return deriveKey([]byte(passphrase), []byte(backupSaltAtRest))
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

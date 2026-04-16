package biz

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"
	"os"

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

// backupKeyConfigured reports whether the master key env var is set and long enough.
func backupKeyConfigured() bool {
	return len(os.Getenv(backupKeyEnv)) >= backupKeyMinLen
}

// deriveKey runs scrypt over a passphrase with the given salt.
func deriveKey(passphrase, salt []byte) ([]byte, error) {
	return scrypt.Key(passphrase, salt, scryptN, scryptR, scryptP, aesKeyLen)
}

func masterKey() ([]byte, error) {
	pw := os.Getenv(backupKeyEnv)
	if len(pw) < backupKeyMinLen {
		return nil, errMissingKey
	}
	return deriveKey([]byte(pw), []byte(backupSaltAtRest))
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

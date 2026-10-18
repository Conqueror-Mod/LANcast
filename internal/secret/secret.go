// Package secret seals small secrets for storage in the database (ADR 0090).
//
// AES-256-GCM, with a fresh random nonce for every sealing, under a key kept
// in its own file in the data folder rather than in the database. That
// separation is the point: LANcast's backups are the database, so a backup on
// its own carries sealed values nobody can open, and a backup restored on
// another machine simply asks people to enter their secrets again.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// KeyFile is the key's name in the data folder.
const KeyFile = "secrets.key"

const keySize = 32

// ErrCannotOpen is a sealed value this key cannot open: sealed under another
// key (a database restored onto another machine) or damaged.
var ErrCannotOpen = errors.New("secret: this value was sealed with a different key")

// Sealer seals and opens values with one key.
type Sealer struct {
	aead cipher.AEAD
}

// Load reads the key in dir, making one if there is none. A key file of the
// wrong size is an error, not replaced: replacing it would make every value
// sealed so far unreadable without a word.
func Load(dir string) (*Sealer, error) {
	path := filepath.Join(dir, KeyFile)
	key, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		key = make([]byte, keySize)
		if _, err := io.ReadFull(rand.Reader, key); err != nil {
			return nil, fmt.Errorf("secret: make key: %w", err)
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("secret: %w", err)
		}
		// O_EXCL: two processes starting at once must not each write a key.
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			return Load(dir)
		}
		if err != nil {
			return nil, fmt.Errorf("secret: write key: %w", err)
		}
		if _, err := f.Write(key); err != nil {
			f.Close()
			return nil, fmt.Errorf("secret: write key: %w", err)
		}
		if err := f.Close(); err != nil {
			return nil, fmt.Errorf("secret: write key: %w", err)
		}
	case err != nil:
		return nil, fmt.Errorf("secret: read key: %w", err)
	case len(key) != keySize:
		return nil, fmt.Errorf("secret: %s is %d bytes, not %d; leaving it alone", path, len(key), keySize)
	}
	return New(key)
}

// New makes a Sealer from a 32-byte key.
func New(key []byte) (*Sealer, error) {
	if len(key) != keySize {
		return nil, fmt.Errorf("secret: key is %d bytes, not %d", len(key), keySize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Sealer{aead: aead}, nil
}

// Seal returns the nonce followed by the sealed value.
func (s *Sealer) Seal(plain []byte) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("secret: nonce: %w", err)
	}
	return s.aead.Seal(nonce, nonce, plain, nil), nil
}

// Open returns what Seal sealed, or ErrCannotOpen.
func (s *Sealer) Open(sealed []byte) ([]byte, error) {
	n := s.aead.NonceSize()
	if len(sealed) < n+s.aead.Overhead() {
		return nil, ErrCannotOpen
	}
	plain, err := s.aead.Open(nil, sealed[:n], sealed[n:], nil)
	if err != nil {
		return nil, ErrCannotOpen
	}
	return plain, nil
}

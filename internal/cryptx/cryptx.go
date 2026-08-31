package cryptx

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrBadHex     = errors.New("crypt key must be 32 bytes hex-encoded")
	ErrBadKeyFile = errors.New("invalid key file")
	ErrCiphertext = errors.New("invalid ciphertext")
	ErrKeyLength  = errors.New("key must be 32 bytes")
)

func LoadOrGenerateKey(explicit, keyFile string) ([]byte, error) {
	if explicit != "" {
		key, err := hex.DecodeString(explicit)
		if err != nil || len(key) != 32 {
			return nil, ErrBadHex
		}
		return key, nil
	}
	if data, err := os.ReadFile(keyFile); err == nil {
		key, err := hex.DecodeString(strings.TrimSpace(string(data)))
		if err != nil || len(key) != 32 {
			return nil, ErrBadKeyFile
		}
		return key, nil
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(keyFile), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyFile, []byte(hex.EncodeToString(key)+"\n"), 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

type Cryptx struct {
	aead cipher.AEAD
}

func New(key []byte) (*Cryptx, error) {
	if len(key) != 32 {
		return nil, ErrKeyLength
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cryptx{aead: aead}, nil
}

func (c *Cryptx) Encrypt(plain string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := c.aead.Seal(nil, nonce, []byte(plain), nil)
	return base64.RawStdEncoding.EncodeToString(append(nonce, sealed...)), nil
}

func (c *Cryptx) Decrypt(enc string) (string, error) {
	raw, err := base64.RawStdEncoding.DecodeString(enc)
	if err != nil {
		return "", ErrCiphertext
	}
	ns := c.aead.NonceSize()
	if len(raw) <= ns {
		return "", ErrCiphertext
	}
	plain, err := c.aead.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

package cryptx

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	return bytes.Repeat([]byte{0x42}, 32)
}

func TestRoundTrip(t *testing.T) {
	c, err := New(testKey(t))
	if err != nil {
		t.Fatal(err)
	}
	enc, err := c.Encrypt("s3cret-p@ss")
	if err != nil {
		t.Fatal(err)
	}
	if enc == "s3cret-p@ss" {
		t.Fatal("ciphertext equals plaintext")
	}
	dec, err := c.Decrypt(enc)
	if err != nil {
		t.Fatal(err)
	}
	if dec != "s3cret-p@ss" {
		t.Fatalf("dec = %q", dec)
	}
}

func TestEncryptNonDeterministic(t *testing.T) {
	c, _ := New(testKey(t))
	a, _ := c.Encrypt("same")
	b, _ := c.Encrypt("same")
	if a == b {
		t.Fatal("nonce reused")
	}
}

func TestTamperFails(t *testing.T) {
	c, _ := New(testKey(t))
	enc, _ := c.Encrypt("data")
	tampered := enc[:len(enc)-2] + "AA"
	if _, err := c.Decrypt(tampered); err == nil {
		t.Fatal("tampered ciphertext accepted")
	}
}

func TestWrongKeyFails(t *testing.T) {
	c1, _ := New(testKey(t))
	enc, _ := c1.Encrypt("data")
	c2, _ := New(bytes.Repeat([]byte{0x43}, 32))
	if _, err := c2.Decrypt(enc); err == nil {
		t.Fatal("decrypted with wrong key")
	}
}

func TestNewRejectsBadKeyLength(t *testing.T) {
	if _, err := New([]byte("short")); err == nil {
		t.Fatal("short key accepted")
	}
}

func TestLoadOrGenerateKeyExplicit(t *testing.T) {
	key, err := LoadOrGenerateKey("00"+"112233445566778899aabbccddeeff", "")
	if err == nil || key != nil {
		t.Fatalf("31-byte hex accepted: %v", err)
	}
	good := "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"
	key, err = LoadOrGenerateKey(good, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(key) != 32 {
		t.Fatalf("len = %d", len(key))
	}
}

func TestLoadOrGenerateKeyFile(t *testing.T) {
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "sub", ".ku-dump-key")
	k1, err := LoadOrGenerateKey("", keyFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(k1) != 32 {
		t.Fatalf("len = %d", len(k1))
	}
	info, err := os.Stat(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode = %v", info.Mode().Perm())
	}
	k2, err := LoadOrGenerateKey("", keyFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(k1, k2) {
		t.Fatal("key not stable across loads")
	}
	if _, err := LoadOrGenerateKey("zz", keyFile); !errors.Is(err, ErrBadHex) && err == nil {
		t.Fatal("invalid hex accepted")
	}
}

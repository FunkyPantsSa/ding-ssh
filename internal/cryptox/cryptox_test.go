package cryptox

import (
	"bytes"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, 32)
	plain := "s3cret/with-unicode-密钥"
	enc, err := Encrypt(key, plain)
	if err != nil {
		t.Fatal(err)
	}
	if !IsEncrypted(enc) {
		t.Fatalf("expected prefix, got %q", enc)
	}
	got, err := Decrypt(key, enc)
	if err != nil {
		t.Fatal(err)
	}
	if got != plain {
		t.Fatalf("got %q want %q", got, plain)
	}
}

func TestEncryptEmpty(t *testing.T) {
	key := bytes.Repeat([]byte{0x7}, 32)
	enc, err := Encrypt(key, "")
	if err != nil {
		t.Fatal(err)
	}
	if enc != "" {
		t.Fatalf("empty plaintext should stay empty, got %q", enc)
	}
}

func TestDecryptRejectsPlaintext(t *testing.T) {
	key := bytes.Repeat([]byte{0x9}, 32)
	_, err := Decrypt(key, "not-encrypted")
	if err != ErrNotEncrypted {
		t.Fatalf("got %v want ErrNotEncrypted", err)
	}
}

func TestDeriveKeyDeterministic(t *testing.T) {
	salt := bytes.Repeat([]byte{0x11}, saltLen)
	a := DeriveKey("passphrase", salt)
	b := DeriveKey("passphrase", salt)
	if !bytes.Equal(a, b) {
		t.Fatal("DeriveKey should be deterministic")
	}
	if len(a) != 32 {
		t.Fatalf("key len %d", len(a))
	}
	c := DeriveKey("other", salt)
	if bytes.Equal(a, c) {
		t.Fatal("different password should not yield same key")
	}
}
